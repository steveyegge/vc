package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"github.com/steveyegge/vc/internal/types"
)

// SQLiteStorage implements the Storage interface using SQLite
type SQLiteStorage struct {
	db          *sql.DB
	issuePrefix string // Prefix for issue IDs (e.g., "vc-", "bd-")
}

// New creates a new SQLite storage backend
func New(path string) (*SQLiteStorage, error) {
	// Ensure directory exists
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create directory: %w", err)
	}

	// Extract issue prefix from database filename (vc-247)
	// e.g., ".beads/vc.db" → "vc-", ".beads/bd.db" → "bd-"
	filename := filepath.Base(path)
	prefix := strings.TrimSuffix(filename, filepath.Ext(filename))
	issuePrefix := prefix + "-"

	// Open database with WAL mode for better concurrency
	db, err := sql.Open("sqlite3", path+"?_journal_mode=WAL&_foreign_keys=ON")
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Test connection
	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	// Initialize schema
	if _, err := db.Exec(schema); err != nil {
		return nil, fmt.Errorf("failed to initialize schema: %w", err)
	}

	// Migrate existing databases to add issue_counters table if missing
	if err := migrateIssueCountersTable(db); err != nil {
		return nil, fmt.Errorf("failed to migrate issue_counters table: %w", err)
	}

	// Check config table for issue_prefix (takes precedence over filename-based prefix)
	// This allows sandboxes and other databases to override the prefix
	var configPrefix string
	err = db.QueryRow("SELECT value FROM config WHERE key = ?", "issue_prefix").Scan(&configPrefix)
	if err == nil && configPrefix != "" {
		// Use config table value if present
		issuePrefix = configPrefix + "-"
	} else if err != nil && err != sql.ErrNoRows {
		// Propagate unexpected errors (not "no rows")
		return nil, fmt.Errorf("failed to read issue_prefix from config: %w", err)
	}
	// Otherwise use the filename-based prefix set above

	return &SQLiteStorage{
		db:          db,
		issuePrefix: issuePrefix,
	}, nil
}

// getNextID determines the next issue ID to use (DEPRECATED - kept for backwards compatibility)
// New code should rely on the atomic counter in issue_counters table
func getNextID(db *sql.DB) (int, error) {
	var maxID sql.NullString
	err := db.QueryRow("SELECT MAX(id) FROM issues").Scan(&maxID)
	if err != nil {
		// Propagate actual errors (network, permissions, etc.)
		return 0, fmt.Errorf("failed to query max issue ID: %w", err)
	}

	// Empty table or NULL result - start from 1
	if !maxID.Valid || maxID.String == "" {
		return 1, nil
	}

	// Parse "vc-123" or "bd-123" to get 123
	parts := strings.Split(maxID.String, "-")
	if len(parts) != 2 {
		return 0, fmt.Errorf("invalid issue ID format: %s (expected prefix-number)", maxID.String)
	}

	var num int
	if _, err := fmt.Sscanf(parts[1], "%d", &num); err != nil {
		return 0, fmt.Errorf("failed to parse issue number from %s: %w", maxID.String, err)
	}

	return num + 1, nil
}

// migrateIssueCountersTable checks if the issue_counters table needs initialization.
// This ensures existing databases created before the atomic counter feature get migrated automatically.
// The table may already exist (created by schema), but be empty - in that case we still need to sync.
func migrateIssueCountersTable(db *sql.DB) error {
	// Check if the table exists (it should, created by schema)
	var tableName string
	err := db.QueryRow(`
		SELECT name FROM sqlite_master
		WHERE type='table' AND name='issue_counters'
	`).Scan(&tableName)

	tableExists := err == nil

	if !tableExists {
		if err != sql.ErrNoRows {
			return fmt.Errorf("failed to check for issue_counters table: %w", err)
		}
		// Table doesn't exist, create it (shouldn't happen with schema, but handle it)
		_, err := db.Exec(`
			CREATE TABLE issue_counters (
				prefix TEXT PRIMARY KEY,
				last_id INTEGER NOT NULL DEFAULT 0
			)
		`)
		if err != nil {
			return fmt.Errorf("failed to create issue_counters table: %w", err)
		}
	}

	// Check if table is empty - if so, we need to sync from existing issues
	var count int
	err = db.QueryRow(`SELECT COUNT(*) FROM issue_counters`).Scan(&count)
	if err != nil {
		return fmt.Errorf("failed to count issue_counters: %w", err)
	}

	if count == 0 {
		// Table is empty, sync counters from existing issues to prevent ID collisions
		// This is safe to do during migration since it's a one-time operation
		_, err = db.Exec(`
			INSERT INTO issue_counters (prefix, last_id)
			SELECT
				substr(id, 1, instr(id, '-') - 1) as prefix,
				MAX(CAST(substr(id, instr(id, '-') + 1) AS INTEGER)) as max_id
			FROM issues
			WHERE instr(id, '-') > 0
			  AND substr(id, instr(id, '-') + 1) GLOB '[0-9]*'
			GROUP BY prefix
			ON CONFLICT(prefix) DO UPDATE SET
				last_id = MAX(last_id, excluded.last_id)
		`)
		if err != nil {
			return fmt.Errorf("failed to sync counters during migration: %w", err)
		}
	}

	// Table exists and is initialized (either was already populated, or we just synced it)
	return nil
}

// CreateIssue creates a new issue
func (s *SQLiteStorage) CreateIssue(ctx context.Context, issue *types.Issue, actor string) error {
	// Validate issue before creating
	if err := issue.Validate(); err != nil {
		return fmt.Errorf("validation failed: %w", err)
	}

	// Set timestamps
	now := time.Now()
	issue.CreatedAt = now
	issue.UpdatedAt = now

	// Acquire a dedicated connection for the transaction.
	// This is necessary because we need to execute raw SQL ("BEGIN IMMEDIATE", "COMMIT")
	// on the same connection, and database/sql's connection pool would otherwise
	// use different connections for different queries.
	conn, err := s.db.Conn(ctx)
	if err != nil {
		return fmt.Errorf("failed to acquire connection: %w", err)
	}
	defer conn.Close()

	// Start IMMEDIATE transaction to acquire write lock early and prevent race conditions.
	// IMMEDIATE acquires a RESERVED lock immediately, preventing other IMMEDIATE or EXCLUSIVE
	// transactions from starting. This serializes ID generation across concurrent writers.
	//
	// We use raw Exec instead of BeginTx because database/sql doesn't support transaction
	// modes in BeginTx, and the sqlite3 driver's BeginTx always uses DEFERRED mode.
	if _, err := conn.ExecContext(ctx, "BEGIN IMMEDIATE"); err != nil {
		return fmt.Errorf("failed to begin immediate transaction: %w", err)
	}

	// Track commit state for defer cleanup
	// Use context.Background() for ROLLBACK to ensure cleanup happens even if ctx is canceled
	committed := false
	defer func() {
		if !committed {
			_, _ = conn.ExecContext(context.Background(), "ROLLBACK")
		}
	}()

	// Generate ID if not set (inside transaction to prevent race conditions)
	if issue.ID == "" {
		// Get prefix from issuePrefix (already set during initialization)
		// Remove trailing "-" for consistency with config table format
		prefix := strings.TrimSuffix(s.issuePrefix, "-")

		// Atomically initialize counter (if needed) and get next ID (within transaction)
		// This ensures the counter starts from the max existing ID, not 1
		// CRITICAL: We rely on BEGIN IMMEDIATE above to serialize this operation across processes
		//
		// The query works as follows:
		// 1. Try to INSERT with last_id = MAX(existing IDs) or 0 if none exist, then +1
		// 2. ON CONFLICT: update last_id to MAX(existing last_id, new calculated last_id) + 1
		// 3. RETURNING gives us the final incremented value
		//
		// This atomically handles three cases:
		// - Counter doesn't exist: initialize from existing issues and return next ID
		// - Counter exists but lower than max ID: update to max and return next ID
		// - Counter exists and correct: just increment and return next ID
		var nextID int
		err = conn.QueryRowContext(ctx, `
			INSERT INTO issue_counters (prefix, last_id)
			SELECT ?, COALESCE(MAX(CAST(substr(id, LENGTH(?) + 2) AS INTEGER)), 0) + 1
			FROM issues
			WHERE id LIKE ? || '-%'
			  AND substr(id, LENGTH(?) + 2) GLOB '[0-9]*'
			ON CONFLICT(prefix) DO UPDATE SET
				last_id = MAX(
					last_id,
					(SELECT COALESCE(MAX(CAST(substr(id, LENGTH(?) + 2) AS INTEGER)), 0)
					 FROM issues
					 WHERE id LIKE ? || '-%'
					   AND substr(id, LENGTH(?) + 2) GLOB '[0-9]*')
				) + 1
			RETURNING last_id
		`, prefix, prefix, prefix, prefix, prefix, prefix, prefix).Scan(&nextID)
		if err != nil {
			return fmt.Errorf("failed to generate next ID for prefix %s: %w", prefix, err)
		}

		issue.ID = fmt.Sprintf("%s-%d", prefix, nextID)
	}

	// Insert issue
	_, err = conn.ExecContext(ctx, `
		INSERT INTO issues (
			id, title, description, design, acceptance_criteria, notes,
			status, priority, issue_type, assignee, estimated_minutes,
			created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		issue.ID, issue.Title, issue.Description, issue.Design,
		issue.AcceptanceCriteria, issue.Notes, issue.Status,
		issue.Priority, issue.IssueType, issue.Assignee,
		issue.EstimatedMinutes, issue.CreatedAt, issue.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("failed to insert issue: %w", err)
	}

	// Record creation event
	eventData, _ := json.Marshal(issue)
	eventDataStr := string(eventData)
	_, err = conn.ExecContext(ctx, `
		INSERT INTO events (issue_id, event_type, actor, new_value)
		VALUES (?, ?, ?, ?)
	`, issue.ID, types.EventCreated, actor, eventDataStr)
	if err != nil {
		return fmt.Errorf("failed to record event: %w", err)
	}

	// Commit the transaction
	if _, err := conn.ExecContext(ctx, "COMMIT"); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}
	committed = true

	return nil
}

// GetIssue retrieves an issue by ID
func (s *SQLiteStorage) GetIssue(ctx context.Context, id string) (*types.Issue, error) {
	var issue types.Issue
	var closedAt sql.NullTime
	var approvedAt sql.NullTime
	var estimatedMinutes sql.NullInt64
	var assignee sql.NullString
	var approvedBy sql.NullString

	err := s.db.QueryRowContext(ctx, `
		SELECT id, title, description, design, acceptance_criteria, notes,
		       status, priority, issue_type, assignee, estimated_minutes,
		       created_at, updated_at, closed_at, approved_at, approved_by
		FROM issues
		WHERE id = ?
	`, id).Scan(
		&issue.ID, &issue.Title, &issue.Description, &issue.Design,
		&issue.AcceptanceCriteria, &issue.Notes, &issue.Status,
		&issue.Priority, &issue.IssueType, &assignee, &estimatedMinutes,
		&issue.CreatedAt, &issue.UpdatedAt, &closedAt, &approvedAt, &approvedBy,
	)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get issue: %w", err)
	}

	if closedAt.Valid {
		issue.ClosedAt = &closedAt.Time
	}
	if estimatedMinutes.Valid {
		mins := int(estimatedMinutes.Int64)
		issue.EstimatedMinutes = &mins
	}
	if assignee.Valid {
		issue.Assignee = assignee.String
	}

	return &issue, nil
}

// GetMission retrieves a mission by ID with approval metadata
func (s *SQLiteStorage) GetMission(ctx context.Context, id string) (*types.Mission, error) {
	var mission types.Mission
	var closedAt sql.NullTime
	var approvedAt sql.NullTime
	var estimatedMinutes sql.NullInt64
	var assignee sql.NullString
	var approvedBy sql.NullString

	err := s.db.QueryRowContext(ctx, `
		SELECT id, title, description, design, acceptance_criteria, notes,
		       status, priority, issue_type, assignee, estimated_minutes,
		       created_at, updated_at, closed_at, approved_at, approved_by
		FROM issues
		WHERE id = ?
	`, id).Scan(
		&mission.ID, &mission.Title, &mission.Description, &mission.Design,
		&mission.AcceptanceCriteria, &mission.Notes, &mission.Status,
		&mission.Priority, &mission.IssueType, &assignee, &estimatedMinutes,
		&mission.CreatedAt, &mission.UpdatedAt, &closedAt, &approvedAt, &approvedBy,
	)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get mission: %w", err)
	}

	// Set nullable fields
	if closedAt.Valid {
		mission.ClosedAt = &closedAt.Time
	}
	if estimatedMinutes.Valid {
		mins := int(estimatedMinutes.Int64)
		mission.EstimatedMinutes = &mins
	}
	if assignee.Valid {
		mission.Assignee = assignee.String
	}

	// Set mission-specific approval fields
	if approvedAt.Valid {
		mission.ApprovedAt = &approvedAt.Time
	}
	if approvedBy.Valid {
		mission.ApprovedBy = approvedBy.String
	}

	return &mission, nil
}

// Allowed fields for update to prevent SQL injection
var allowedUpdateFields = map[string]bool{
	"status":              true,
	"priority":            true,
	"title":               true,
	"assignee":            true,
	"description":         true,
	"design":              true,
	"acceptance_criteria": true,
	"notes":               true,
	"issue_type":          true,
	"estimated_minutes":   true,
	"approved_at":         true,
	"approved_by":         true,
}

// UpdateIssue updates fields on an issue
func (s *SQLiteStorage) UpdateIssue(ctx context.Context, id string, updates map[string]interface{}, actor string) error {
	// Get old issue for event
	oldIssue, err := s.GetIssue(ctx, id)
	if err != nil {
		return err
	}
	if oldIssue == nil {
		return fmt.Errorf("issue %s not found", id)
	}

	// Build update query with validated field names
	setClauses := []string{"updated_at = ?"}
	args := []interface{}{time.Now()}

	for key, value := range updates {
		// Prevent SQL injection by validating field names
		if !allowedUpdateFields[key] {
			return fmt.Errorf("invalid field for update: %s", key)
		}

		// Validate field values
		switch key {
		case "priority":
			if priority, ok := value.(int); ok {
				if priority < 0 || priority > 4 {
					return fmt.Errorf("priority must be between 0 and 4 (got %d)", priority)
				}
			}
		case "status":
			if status, ok := value.(string); ok {
				if !types.Status(status).IsValid() {
					return fmt.Errorf("invalid status: %s", status)
				}
			}
		case "issue_type":
			if issueType, ok := value.(string); ok {
				if !types.IssueType(issueType).IsValid() {
					return fmt.Errorf("invalid issue type: %s", issueType)
				}
			}
		case "title":
			if title, ok := value.(string); ok {
				if len(title) == 0 || len(title) > 500 {
					return fmt.Errorf("title must be 1-500 characters")
				}
			}
		case "estimated_minutes":
			if mins, ok := value.(int); ok {
				if mins < 0 {
					return fmt.Errorf("estimated_minutes cannot be negative")
				}
			}
		}

		setClauses = append(setClauses, fmt.Sprintf("%s = ?", key))
		args = append(args, value)
	}
	args = append(args, id)

	// Start transaction
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	// Update issue
	query := fmt.Sprintf("UPDATE issues SET %s WHERE id = ?", strings.Join(setClauses, ", "))
	_, err = tx.ExecContext(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("failed to update issue: %w", err)
	}

	// Record event
	oldData, _ := json.Marshal(oldIssue)
	newData, _ := json.Marshal(updates)
	oldDataStr := string(oldData)
	newDataStr := string(newData)

	eventType := types.EventUpdated
	if statusVal, ok := updates["status"]; ok {
		if statusVal == string(types.StatusClosed) {
			eventType = types.EventClosed
		} else {
			eventType = types.EventStatusChanged
		}
	}

	_, err = tx.ExecContext(ctx, `
		INSERT INTO events (issue_id, event_type, actor, old_value, new_value)
		VALUES (?, ?, ?, ?, ?)
	`, id, eventType, actor, oldDataStr, newDataStr)
	if err != nil {
		return fmt.Errorf("failed to record event: %w", err)
	}

	return tx.Commit()
}

// CloseIssue closes an issue with a reason
func (s *SQLiteStorage) CloseIssue(ctx context.Context, id string, reason string, actor string) error {
	now := time.Now()

	// Update with special event handling
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	_, err = tx.ExecContext(ctx, `
		UPDATE issues SET status = ?, closed_at = ?, updated_at = ?
		WHERE id = ?
	`, types.StatusClosed, now, now, id)
	if err != nil {
		return fmt.Errorf("failed to close issue: %w", err)
	}

	_, err = tx.ExecContext(ctx, `
		INSERT INTO events (issue_id, event_type, actor, comment)
		VALUES (?, ?, ?, ?)
	`, id, types.EventClosed, actor, reason)
	if err != nil {
		return fmt.Errorf("failed to record event: %w", err)
	}

	return tx.Commit()
}

// SearchIssues finds issues matching query and filters
func (s *SQLiteStorage) SearchIssues(ctx context.Context, query string, filter types.IssueFilter) ([]*types.Issue, error) {
	whereClauses := []string{}
	args := []interface{}{}

	if query != "" {
		whereClauses = append(whereClauses, "(title LIKE ? OR description LIKE ? OR id LIKE ?)")
		pattern := "%" + query + "%"
		args = append(args, pattern, pattern, pattern)
	}

	if filter.Status != nil {
		whereClauses = append(whereClauses, "status = ?")
		args = append(args, *filter.Status)
	}

	if filter.Priority != nil {
		whereClauses = append(whereClauses, "priority = ?")
		args = append(args, *filter.Priority)
	}

	if filter.IssueType != nil {
		whereClauses = append(whereClauses, "issue_type = ?")
		args = append(args, *filter.IssueType)
	}

	if filter.Assignee != nil {
		whereClauses = append(whereClauses, "assignee = ?")
		args = append(args, *filter.Assignee)
	}

	// Handle label filtering (vc-243)
	// Each label requires an EXISTS subquery to ensure ALL labels match
	if len(filter.Labels) > 0 {
		for _, label := range filter.Labels {
			whereClauses = append(whereClauses, `
				EXISTS (
					SELECT 1 FROM labels l
					WHERE l.issue_id = issues.id AND l.label = ?
				)`)
			args = append(args, label)
		}
	}

	whereSQL := ""
	if len(whereClauses) > 0 {
		whereSQL = "WHERE " + strings.Join(whereClauses, " AND ")
	}

	limitSQL := ""
	if filter.Limit > 0 {
		limitSQL = fmt.Sprintf(" LIMIT %d", filter.Limit)
	}

	querySQL := fmt.Sprintf(`
		SELECT id, title, description, design, acceptance_criteria, notes,
		       status, priority, issue_type, assignee, estimated_minutes,
		       created_at, updated_at, closed_at
		FROM issues
		%s
		ORDER BY priority ASC, created_at DESC
		%s
	`, whereSQL, limitSQL)

	rows, err := s.db.QueryContext(ctx, querySQL, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to search issues: %w", err)
	}
	defer rows.Close()

	var issues []*types.Issue
	for rows.Next() {
		var issue types.Issue
		var closedAt sql.NullTime
		var estimatedMinutes sql.NullInt64
		var assignee sql.NullString

		err := rows.Scan(
			&issue.ID, &issue.Title, &issue.Description, &issue.Design,
			&issue.AcceptanceCriteria, &issue.Notes, &issue.Status,
			&issue.Priority, &issue.IssueType, &assignee, &estimatedMinutes,
			&issue.CreatedAt, &issue.UpdatedAt, &closedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan issue: %w", err)
		}

		if closedAt.Valid {
			issue.ClosedAt = &closedAt.Time
		}
		if estimatedMinutes.Valid {
			mins := int(estimatedMinutes.Int64)
			issue.EstimatedMinutes = &mins
		}
		if assignee.Valid {
			issue.Assignee = assignee.String
		}

		issues = append(issues, &issue)
	}

	return issues, nil
}

// GetConfig gets a configuration value from the config table
func (s *SQLiteStorage) GetConfig(ctx context.Context, key string) (string, error) {
	var value string
	err := s.db.QueryRowContext(ctx, `SELECT value FROM config WHERE key = ?`, key).Scan(&value)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return value, err
}

// SetConfig sets a configuration value in the config table
func (s *SQLiteStorage) SetConfig(ctx context.Context, key, value string) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO config (key, value) VALUES (?, ?)
		ON CONFLICT (key) DO UPDATE SET value = excluded.value
	`, key, value)
	return err
}

// Close closes the database connection
func (s *SQLiteStorage) Close() error {
	return s.db.Close()
}
