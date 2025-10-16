package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/steveyegge/vc/internal/types"
)

// PostgresStorage implements the Storage interface using PostgreSQL
type PostgresStorage struct {
	pool *pgxpool.Pool
}

// Config holds PostgreSQL connection configuration
type Config struct {
	Host            string
	Port            int
	Database        string
	User            string
	Password        string
	SSLMode         string
	MaxConns        int32
	MinConns        int32
	MaxConnLifetime time.Duration
	MaxConnIdleTime time.Duration
	HealthCheck     time.Duration
}

// DefaultConfig returns a config with sensible defaults
func DefaultConfig() *Config {
	return &Config{
		Host:            "localhost",
		Port:            5432,
		Database:        "vc",
		User:            "vc",
		SSLMode:         "prefer",
		MaxConns:        25,
		MinConns:        5,
		MaxConnLifetime: 1 * time.Hour,
		MaxConnIdleTime: 30 * time.Minute,
		HealthCheck:     1 * time.Minute,
	}
}

// New creates a new PostgreSQL storage backend with connection pooling
func New(ctx context.Context, cfg *Config) (*PostgresStorage, error) {
	if cfg == nil {
		cfg = DefaultConfig()
	}

	// Build connection string
	connString := fmt.Sprintf(
		"postgres://%s:%s@%s:%d/%s?sslmode=%s",
		cfg.User,
		cfg.Password,
		cfg.Host,
		cfg.Port,
		cfg.Database,
		cfg.SSLMode,
	)

	// Configure connection pool
	poolConfig, err := pgxpool.ParseConfig(connString)
	if err != nil {
		return nil, fmt.Errorf("failed to parse connection string: %w", err)
	}

	// Apply pool configuration
	poolConfig.MaxConns = cfg.MaxConns
	poolConfig.MinConns = cfg.MinConns
	poolConfig.MaxConnLifetime = cfg.MaxConnLifetime
	poolConfig.MaxConnIdleTime = cfg.MaxConnIdleTime
	poolConfig.HealthCheckPeriod = cfg.HealthCheck

	// Create connection pool
	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create connection pool: %w", err)
	}

	// Test connection with ping
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	// Initialize schema
	if err := initializeSchema(ctx, pool); err != nil {
		pool.Close()
		return nil, fmt.Errorf("failed to initialize schema: %w", err)
	}

	return &PostgresStorage{
		pool: pool,
	}, nil
}

// initializeSchema creates all tables, indexes, and views if they don't exist
func initializeSchema(ctx context.Context, pool *pgxpool.Pool) error {
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("failed to acquire connection: %w", err)
	}
	defer conn.Release()

	if _, err := conn.Exec(ctx, schema); err != nil {
		return fmt.Errorf("failed to execute schema: %w", err)
	}

	return nil
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

// MaxEventsLimit is the maximum number of events that can be returned by GetEvents
// to prevent DoS attacks from excessively large queries
const MaxEventsLimit = 10000

// Close closes the connection pool and releases all resources
func (s *PostgresStorage) Close() error {
	if s.pool != nil {
		s.pool.Close()
	}
	return nil
}

// CreateIssue creates a new issue
func (s *PostgresStorage) CreateIssue(ctx context.Context, issue *types.Issue, actor string) error {
	// Validate issue before creating
	if err := issue.Validate(); err != nil {
		return fmt.Errorf("validation failed: %w", err)
	}

	// Generate ID if not set using PostgreSQL sequence
	if issue.ID == "" {
		err := s.pool.QueryRow(ctx, "SELECT next_issue_id()").Scan(&issue.ID)
		if err != nil {
			return fmt.Errorf("failed to generate issue ID: %w", err)
		}
	}

	// Set timestamps
	now := time.Now()
	issue.CreatedAt = now
	issue.UpdatedAt = now

	// Start transaction
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	// Insert issue
	_, err = tx.Exec(ctx, `
		INSERT INTO issues (
			id, title, description, design, acceptance_criteria, notes,
			status, priority, issue_type, assignee, estimated_minutes,
			created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
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
	_, err = tx.Exec(ctx, `
		INSERT INTO events (issue_id, event_type, actor, new_value)
		VALUES ($1, $2, $3, $4)
	`, issue.ID, types.EventCreated, actor, eventDataStr)
	if err != nil {
		return fmt.Errorf("failed to record event: %w", err)
	}

	return tx.Commit(ctx)
}

// GetIssue retrieves an issue by ID
func (s *PostgresStorage) GetIssue(ctx context.Context, id string) (*types.Issue, error) {
	var issue types.Issue
	var closedAt *time.Time
	var estimatedMinutes *int
	var assignee *string

	err := s.pool.QueryRow(ctx, `
		SELECT id, title, description, design, acceptance_criteria, notes,
		       status, priority, issue_type, assignee, estimated_minutes,
		       created_at, updated_at, closed_at
		FROM issues
		WHERE id = $1
	`, id).Scan(
		&issue.ID, &issue.Title, &issue.Description, &issue.Design,
		&issue.AcceptanceCriteria, &issue.Notes, &issue.Status,
		&issue.Priority, &issue.IssueType, &assignee, &estimatedMinutes,
		&issue.CreatedAt, &issue.UpdatedAt, &closedAt,
	)

	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get issue: %w", err)
	}

	issue.ClosedAt = closedAt
	issue.EstimatedMinutes = estimatedMinutes
	if assignee != nil {
		issue.Assignee = *assignee
	}

	return &issue, nil
}

// UpdateIssue updates fields on an issue
func (s *PostgresStorage) UpdateIssue(ctx context.Context, id string, updates map[string]interface{}, actor string) error {
	// Get old issue for event
	oldIssue, err := s.GetIssue(ctx, id)
	if err != nil {
		return err
	}
	if oldIssue == nil {
		return fmt.Errorf("issue %s not found", id)
	}

	// Build update query with validated field names
	setClauses := []string{"updated_at = $1"}
	args := []interface{}{time.Now()}
	paramIndex := 2

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

		setClauses = append(setClauses, fmt.Sprintf("%s = $%d", key, paramIndex))
		args = append(args, value)
		paramIndex++
	}
	args = append(args, id)

	// Start transaction
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	// Update issue
	query := fmt.Sprintf("UPDATE issues SET %s WHERE id = $%d", strings.Join(setClauses, ", "), paramIndex)
	_, err = tx.Exec(ctx, query, args...)
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

	_, err = tx.Exec(ctx, `
		INSERT INTO events (issue_id, event_type, actor, old_value, new_value)
		VALUES ($1, $2, $3, $4, $5)
	`, id, eventType, actor, oldDataStr, newDataStr)
	if err != nil {
		return fmt.Errorf("failed to record event: %w", err)
	}

	return tx.Commit(ctx)
}

// CloseIssue closes an issue with a reason
func (s *PostgresStorage) CloseIssue(ctx context.Context, id string, reason string, actor string) error {
	now := time.Now()

	// Update with special event handling
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	_, err = tx.Exec(ctx, `
		UPDATE issues SET status = $1, closed_at = $2, updated_at = $3
		WHERE id = $4
	`, types.StatusClosed, now, now, id)
	if err != nil {
		return fmt.Errorf("failed to close issue: %w", err)
	}

	_, err = tx.Exec(ctx, `
		INSERT INTO events (issue_id, event_type, actor, comment)
		VALUES ($1, $2, $3, $4)
	`, id, types.EventClosed, actor, reason)
	if err != nil {
		return fmt.Errorf("failed to record event: %w", err)
	}

	return tx.Commit(ctx)
}

// SearchIssues finds issues matching query and filters
func (s *PostgresStorage) SearchIssues(ctx context.Context, query string, filter types.IssueFilter) ([]*types.Issue, error) {
	whereClauses := []string{}
	args := []interface{}{}
	paramIndex := 1

	if query != "" {
		whereClauses = append(whereClauses, fmt.Sprintf("(title ILIKE $%d OR description ILIKE $%d OR id ILIKE $%d)", paramIndex, paramIndex+1, paramIndex+2))
		pattern := "%" + query + "%"
		args = append(args, pattern, pattern, pattern)
		paramIndex += 3
	}

	if filter.Status != nil {
		whereClauses = append(whereClauses, fmt.Sprintf("status = $%d", paramIndex))
		args = append(args, *filter.Status)
		paramIndex++
	}

	if filter.Priority != nil {
		whereClauses = append(whereClauses, fmt.Sprintf("priority = $%d", paramIndex))
		args = append(args, *filter.Priority)
		paramIndex++
	}

	if filter.IssueType != nil {
		whereClauses = append(whereClauses, fmt.Sprintf("issue_type = $%d", paramIndex))
		args = append(args, *filter.IssueType)
		paramIndex++
	}

	if filter.Assignee != nil {
		whereClauses = append(whereClauses, fmt.Sprintf("assignee = $%d", paramIndex))
		args = append(args, *filter.Assignee)
		paramIndex++
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

	rows, err := s.pool.Query(ctx, querySQL, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to search issues: %w", err)
	}
	defer rows.Close()

	var issues []*types.Issue
	for rows.Next() {
		var issue types.Issue
		var closedAt *time.Time
		var estimatedMinutes *int
		var assignee *string

		err := rows.Scan(
			&issue.ID, &issue.Title, &issue.Description, &issue.Design,
			&issue.AcceptanceCriteria, &issue.Notes, &issue.Status,
			&issue.Priority, &issue.IssueType, &assignee, &estimatedMinutes,
			&issue.CreatedAt, &issue.UpdatedAt, &closedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan issue: %w", err)
		}

		issue.ClosedAt = closedAt
		issue.EstimatedMinutes = estimatedMinutes
		if assignee != nil {
			issue.Assignee = *assignee
		}

		issues = append(issues, &issue)
	}

	return issues, nil
}

// AddDependency adds a dependency between issues with cycle prevention
func (s *PostgresStorage) AddDependency(ctx context.Context, dep *types.Dependency, actor string) error {
	// Validate issue IDs are not empty (check first for more specific error messages)
	if dep.IssueID == "" {
		return fmt.Errorf("issue ID cannot be empty")
	}
	if dep.DependsOnID == "" {
		return fmt.Errorf("dependency target ID cannot be empty")
	}

	// Prevent self-dependency
	if dep.IssueID == dep.DependsOnID {
		return fmt.Errorf("issue cannot depend on itself")
	}

	dep.CreatedAt = time.Now()
	dep.CreatedBy = actor

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	// Insert dependency - foreign key constraints will validate issue existence
	_, err = tx.Exec(ctx, `
		INSERT INTO dependencies (issue_id, depends_on_id, type, created_at, created_by)
		VALUES ($1, $2, $3, $4, $5)
	`, dep.IssueID, dep.DependsOnID, dep.Type, dep.CreatedAt, dep.CreatedBy)
	if err != nil {
		// Check if it's a foreign key violation using pgx error codes
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23503" {
			// FK violation - check which constraint was violated
			if strings.Contains(pgErr.ConstraintName, "issue_id") {
				return fmt.Errorf("issue %s not found", dep.IssueID)
			}
			if strings.Contains(pgErr.ConstraintName, "depends_on") {
				return fmt.Errorf("dependency target %s not found", dep.DependsOnID)
			}
			return fmt.Errorf("one or both issues not found")
		}
		return fmt.Errorf("failed to add dependency: %w", err)
	}

	// Check if this creates a cycle (only for 'blocks' type dependencies)
	// We need to check if we can reach IssueID from DependsOnID
	// If yes, adding "IssueID depends on DependsOnID" would create a cycle
	if dep.Type == types.DepBlocks {
		var cycleExists bool
		err = tx.QueryRow(ctx, `
			WITH RECURSIVE paths AS (
				SELECT
					issue_id,
					depends_on_id,
					1 as depth
				FROM dependencies
				WHERE type = 'blocks'
				  AND issue_id = $1

				UNION ALL

				SELECT
					d.issue_id,
					d.depends_on_id,
					p.depth + 1
				FROM dependencies d
				JOIN paths p ON d.issue_id = p.depends_on_id
				WHERE d.type = 'blocks'
				  AND p.depth < 100
			)
			SELECT EXISTS(
				SELECT 1 FROM paths
				WHERE depends_on_id = $2
			)
		`, dep.DependsOnID, dep.IssueID).Scan(&cycleExists)

		if err != nil {
			return fmt.Errorf("failed to check for cycles: %w", err)
		}

		if cycleExists {
			return fmt.Errorf("cannot add dependency: would create a cycle (%s → %s → ... → %s)",
				dep.IssueID, dep.DependsOnID, dep.IssueID)
		}
	}

	// Record event
	_, err = tx.Exec(ctx, `
		INSERT INTO events (issue_id, event_type, actor, comment)
		VALUES ($1, $2, $3, $4)
	`, dep.IssueID, types.EventDependencyAdded, actor,
		fmt.Sprintf("Added dependency: %s %s %s", dep.IssueID, dep.Type, dep.DependsOnID))
	if err != nil {
		return fmt.Errorf("failed to record event: %w", err)
	}

	return tx.Commit(ctx)
}

// RemoveDependency removes a dependency
func (s *PostgresStorage) RemoveDependency(ctx context.Context, issueID, dependsOnID string, actor string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	_, err = tx.Exec(ctx, `
		DELETE FROM dependencies WHERE issue_id = $1 AND depends_on_id = $2
	`, issueID, dependsOnID)
	if err != nil {
		return fmt.Errorf("failed to remove dependency: %w", err)
	}

	_, err = tx.Exec(ctx, `
		INSERT INTO events (issue_id, event_type, actor, comment)
		VALUES ($1, $2, $3, $4)
	`, issueID, types.EventDependencyRemoved, actor,
		fmt.Sprintf("Removed dependency on %s", dependsOnID))
	if err != nil {
		return fmt.Errorf("failed to record event: %w", err)
	}

	return tx.Commit(ctx)
}

// GetDependencies returns issues that this issue depends on
func (s *PostgresStorage) GetDependencies(ctx context.Context, issueID string) ([]*types.Issue, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT i.id, i.title, i.description, i.design, i.acceptance_criteria, i.notes,
		       i.status, i.priority, i.issue_type, i.assignee, i.estimated_minutes,
		       i.created_at, i.updated_at, i.closed_at
		FROM issues i
		JOIN dependencies d ON i.id = d.depends_on_id
		WHERE d.issue_id = $1
		ORDER BY i.priority ASC
	`, issueID)
	if err != nil {
		return nil, fmt.Errorf("failed to get dependencies: %w", err)
	}
	defer rows.Close()

	return scanIssues(rows)
}

// GetDependents returns issues that depend on this issue
func (s *PostgresStorage) GetDependents(ctx context.Context, issueID string) ([]*types.Issue, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT i.id, i.title, i.description, i.design, i.acceptance_criteria, i.notes,
		       i.status, i.priority, i.issue_type, i.assignee, i.estimated_minutes,
		       i.created_at, i.updated_at, i.closed_at
		FROM issues i
		JOIN dependencies d ON i.id = d.issue_id
		WHERE d.depends_on_id = $1
		ORDER BY i.priority ASC
	`, issueID)
	if err != nil {
		return nil, fmt.Errorf("failed to get dependents: %w", err)
	}
	defer rows.Close()

	return scanIssues(rows)
}

// GetDependencyTree returns the full dependency tree
func (s *PostgresStorage) GetDependencyTree(ctx context.Context, issueID string, maxDepth int) ([]*types.TreeNode, error) {
	if maxDepth <= 0 {
		maxDepth = 50
	}

	// Use recursive CTE to build tree
	rows, err := s.pool.Query(ctx, `
		WITH RECURSIVE tree AS (
			SELECT
				i.id, i.title, i.status, i.priority, i.description, i.design,
				i.acceptance_criteria, i.notes, i.issue_type, i.assignee,
				i.estimated_minutes, i.created_at, i.updated_at, i.closed_at,
				0 as depth
			FROM issues i
			WHERE i.id = $1

			UNION ALL

			SELECT
				i.id, i.title, i.status, i.priority, i.description, i.design,
				i.acceptance_criteria, i.notes, i.issue_type, i.assignee,
				i.estimated_minutes, i.created_at, i.updated_at, i.closed_at,
				t.depth + 1
			FROM issues i
			JOIN dependencies d ON i.id = d.depends_on_id
			JOIN tree t ON d.issue_id = t.id
			WHERE t.depth < $2
		)
		SELECT * FROM tree
		ORDER BY depth, priority
	`, issueID, maxDepth)
	if err != nil {
		return nil, fmt.Errorf("failed to get dependency tree: %w", err)
	}
	defer rows.Close()

	var nodes []*types.TreeNode
	for rows.Next() {
		var node types.TreeNode
		var closedAt *time.Time
		var estimatedMinutes *int
		var assignee *string

		err := rows.Scan(
			&node.ID, &node.Title, &node.Status, &node.Priority,
			&node.Description, &node.Design, &node.AcceptanceCriteria,
			&node.Notes, &node.IssueType, &assignee, &estimatedMinutes,
			&node.CreatedAt, &node.UpdatedAt, &closedAt, &node.Depth,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan tree node: %w", err)
		}

		if closedAt != nil {
			node.ClosedAt = closedAt
		}
		if estimatedMinutes != nil {
			node.EstimatedMinutes = estimatedMinutes
		}
		if assignee != nil {
			node.Assignee = *assignee
		}

		// Nodes at depth maxDepth-1 are the deepest returned, so they're truncated
		// (their children aren't shown due to the WHERE t.depth < maxDepth clause)
		node.Truncated = node.Depth >= maxDepth-1

		nodes = append(nodes, &node)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating dependency tree rows: %w", err)
	}

	return nodes, nil
}

// DetectCycles finds circular dependencies and returns the actual cycle paths
func (s *PostgresStorage) DetectCycles(ctx context.Context) ([][]*types.Issue, error) {
	// Use recursive CTE to find cycles with full paths
	// We track the path as a string to work around the need for arrays
	rows, err := s.pool.Query(ctx, `
		WITH RECURSIVE paths AS (
			SELECT
				issue_id,
				depends_on_id,
				issue_id as start_id,
				issue_id || '→' || depends_on_id as path,
				0 as depth
			FROM dependencies

			UNION ALL

			SELECT
				d.issue_id,
				d.depends_on_id,
				p.start_id,
				p.path || '→' || d.depends_on_id,
				p.depth + 1
			FROM dependencies d
			JOIN paths p ON d.issue_id = p.depends_on_id
			WHERE p.depth < 100
			  AND p.path NOT LIKE '%' || d.depends_on_id || '→%'
		)
		SELECT DISTINCT path || '→' || start_id as cycle_path
		FROM paths
		WHERE depends_on_id = start_id
		ORDER BY cycle_path
	`)
	if err != nil {
		return nil, fmt.Errorf("failed to detect cycles: %w", err)
	}
	defer rows.Close()

	// First pass: collect all cycle paths and unique issue IDs
	type cyclePath struct {
		path     string
		issueIDs []string
	}
	var cyclePaths []cyclePath
	seen := make(map[string]bool)
	uniqueIssueIDs := make(map[string]bool)

	for rows.Next() {
		var pathStr string
		if err := rows.Scan(&pathStr); err != nil {
			return nil, err
		}

		// Skip if we've already seen this cycle (can happen with different entry points)
		if seen[pathStr] {
			continue
		}
		seen[pathStr] = true

		// Parse the path string: "bd-1→bd-2→bd-3→bd-1"
		issueIDs := strings.Split(pathStr, "→")

		// Remove the duplicate last element (cycle closes back to start)
		if len(issueIDs) > 1 && issueIDs[0] == issueIDs[len(issueIDs)-1] {
			issueIDs = issueIDs[:len(issueIDs)-1]
		}

		// Track this cycle path
		cyclePaths = append(cyclePaths, cyclePath{
			path:     pathStr,
			issueIDs: issueIDs,
		})

		// Collect unique issue IDs
		for _, issueID := range issueIDs {
			uniqueIssueIDs[issueID] = true
		}
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating cycle path rows: %w", err)
	}

	// If no cycles found, return early
	if len(cyclePaths) == 0 {
		return nil, nil
	}

	// Second pass: bulk fetch all issues involved in cycles
	// Build the WHERE IN clause with proper parameters
	issueIDList := make([]string, 0, len(uniqueIssueIDs))
	for id := range uniqueIssueIDs {
		issueIDList = append(issueIDList, id)
	}

	// Sort for deterministic ordering (improves debugging and test consistency)
	sort.Strings(issueIDList)

	// Build issue map for fast lookup
	// Use batching to avoid exceeding PostgreSQL's 65535 parameter limit
	const maxBatchSize = 1000
	issueMap := make(map[string]*types.Issue)

	for i := 0; i < len(issueIDList); i += maxBatchSize {
		end := i + maxBatchSize
		if end > len(issueIDList) {
			end = len(issueIDList)
		}
		batch := issueIDList[i:end]

		// Build parameterized query for this batch: SELECT ... WHERE id IN ($1, $2, ..., $N)
		params := make([]interface{}, len(batch))
		placeholders := make([]string, len(batch))
		for j, id := range batch {
			params[j] = id
			placeholders[j] = fmt.Sprintf("$%d", j+1)
		}

		bulkQuery := fmt.Sprintf(`
			SELECT id, title, description, design, acceptance_criteria, notes,
			       status, priority, issue_type, assignee, estimated_minutes,
			       created_at, updated_at, closed_at
			FROM issues
			WHERE id IN (%s)
		`, strings.Join(placeholders, ", "))

		issueRows, err := s.pool.Query(ctx, bulkQuery, params...)
		if err != nil {
			return nil, fmt.Errorf("failed to fetch cycle issues: %w", err)
		}
		defer issueRows.Close()

		// Process this batch and add to issueMap
		for issueRows.Next() {
			var issue types.Issue
			var closedAt *time.Time
			var estimatedMinutes *int
			var assignee *string

			err := issueRows.Scan(
				&issue.ID, &issue.Title, &issue.Description, &issue.Design,
				&issue.AcceptanceCriteria, &issue.Notes, &issue.Status,
				&issue.Priority, &issue.IssueType, &assignee, &estimatedMinutes,
				&issue.CreatedAt, &issue.UpdatedAt, &closedAt,
			)
			if err != nil {
				return nil, fmt.Errorf("failed to scan issue: %w", err)
			}

			issue.ClosedAt = closedAt
			issue.EstimatedMinutes = estimatedMinutes
			if assignee != nil {
				issue.Assignee = *assignee
			}

			issueMap[issue.ID] = &issue
		}

		// Check for errors during iteration
		if err := issueRows.Err(); err != nil {
			return nil, fmt.Errorf("error iterating batch results: %w", err)
		}
	}

	// Third pass: assemble cycles from issue map
	var cycles [][]*types.Issue
	for _, cp := range cyclePaths {
		var cycleIssues []*types.Issue
		for _, issueID := range cp.issueIDs {
			if issue, exists := issueMap[issueID]; exists {
				cycleIssues = append(cycleIssues, issue)
			} else {
				// Data integrity issue: issue in cycle path not found in database
				fmt.Fprintf(os.Stderr, "WARNING: issue %s in cycle path not found in database (path: %s)\n", issueID, cp.path)
			}
		}
		if len(cycleIssues) > 0 {
			cycles = append(cycles, cycleIssues)
		}
	}

	return cycles, nil
}

// AddLabel adds a label to an issue
func (s *PostgresStorage) AddLabel(ctx context.Context, issueID, label, actor string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	result, err := tx.Exec(ctx, `
		INSERT INTO labels (issue_id, label)
		VALUES ($1, $2)
		ON CONFLICT (issue_id, label) DO NOTHING
	`, issueID, label)
	if err != nil {
		return fmt.Errorf("failed to add label: %w", err)
	}

	// Only record event if label was actually added (not a no-op)
	rowsAffected := result.RowsAffected()
	if rowsAffected > 0 {
		_, err = tx.Exec(ctx, `
			INSERT INTO events (issue_id, event_type, actor, comment)
			VALUES ($1, $2, $3, $4)
		`, issueID, types.EventLabelAdded, actor, fmt.Sprintf("Added label: %s", label))
		if err != nil {
			return fmt.Errorf("failed to record event: %w", err)
		}
	}

	return tx.Commit(ctx)
}

// RemoveLabel removes a label from an issue
func (s *PostgresStorage) RemoveLabel(ctx context.Context, issueID, label, actor string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	result, err := tx.Exec(ctx, `
		DELETE FROM labels WHERE issue_id = $1 AND label = $2
	`, issueID, label)
	if err != nil {
		return fmt.Errorf("failed to remove label: %w", err)
	}

	// Only record event if label was actually removed (not a no-op)
	rowsAffected := result.RowsAffected()
	if rowsAffected > 0 {
		_, err = tx.Exec(ctx, `
			INSERT INTO events (issue_id, event_type, actor, comment)
			VALUES ($1, $2, $3, $4)
		`, issueID, types.EventLabelRemoved, actor, fmt.Sprintf("Removed label: %s", label))
		if err != nil {
			return fmt.Errorf("failed to record event: %w", err)
		}
	}

	return tx.Commit(ctx)
}

// GetLabels returns all labels for an issue
func (s *PostgresStorage) GetLabels(ctx context.Context, issueID string) ([]string, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT label FROM labels WHERE issue_id = $1 ORDER BY label
	`, issueID)
	if err != nil {
		return nil, fmt.Errorf("failed to get labels: %w", err)
	}
	defer rows.Close()

	labels := []string{}
	for rows.Next() {
		var label string
		if err := rows.Scan(&label); err != nil {
			return nil, err
		}
		labels = append(labels, label)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating label rows: %w", err)
	}

	return labels, nil
}

// GetIssuesByLabel returns issues with a specific label
func (s *PostgresStorage) GetIssuesByLabel(ctx context.Context, label string) ([]*types.Issue, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT i.id, i.title, i.description, i.design, i.acceptance_criteria, i.notes,
		       i.status, i.priority, i.issue_type, i.assignee, i.estimated_minutes,
		       i.created_at, i.updated_at, i.closed_at
		FROM issues i
		JOIN labels l ON i.id = l.issue_id
		WHERE l.label = $1
		ORDER BY i.priority ASC, i.created_at DESC
	`, label)
	if err != nil {
		return nil, fmt.Errorf("failed to get issues by label: %w", err)
	}
	defer rows.Close()

	return scanIssues(rows)
}

// Ready work methods are implemented in ready.go

// AddComment adds a comment to an issue
func (s *PostgresStorage) AddComment(ctx context.Context, issueID, actor, comment string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	_, err = tx.Exec(ctx, `
		INSERT INTO events (issue_id, event_type, actor, comment)
		VALUES ($1, $2, $3, $4)
	`, issueID, types.EventCommented, actor, comment)
	if err != nil {
		return fmt.Errorf("failed to add comment: %w", err)
	}

	// Update issue updated_at timestamp
	_, err = tx.Exec(ctx, `
		UPDATE issues SET updated_at = NOW() WHERE id = $1
	`, issueID)
	if err != nil {
		return fmt.Errorf("failed to update timestamp: %w", err)
	}

	return tx.Commit(ctx)
}

// GetEvents returns the event history for an issue
// limit: 0 means no limit, positive values are capped at MaxEventsLimit, negative values return an error
func (s *PostgresStorage) GetEvents(ctx context.Context, issueID string, limit int) ([]*types.Event, error) {
	// Validate limit parameter
	if limit < 0 {
		return nil, fmt.Errorf("limit must be non-negative (got %d)", limit)
	}

	// Cap maximum limit to prevent DoS
	if limit > MaxEventsLimit {
		limit = MaxEventsLimit
	}

	limitSQL := ""
	if limit > 0 {
		limitSQL = fmt.Sprintf(" LIMIT %d", limit)
	}

	query := fmt.Sprintf(`
		SELECT id, issue_id, event_type, actor, old_value, new_value, comment, created_at
		FROM events
		WHERE issue_id = $1
		ORDER BY created_at DESC
		%s
	`, limitSQL)

	rows, err := s.pool.Query(ctx, query, issueID)
	if err != nil {
		return nil, fmt.Errorf("failed to get events: %w", err)
	}
	defer rows.Close()

	var events []*types.Event
	for rows.Next() {
		var event types.Event
		var oldValue, newValue, comment *string

		err := rows.Scan(
			&event.ID, &event.IssueID, &event.EventType, &event.Actor,
			&oldValue, &newValue, &comment, &event.CreatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan event: %w", err)
		}

		event.OldValue = oldValue
		event.NewValue = newValue
		event.Comment = comment

		events = append(events, &event)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating event rows: %w", err)
	}

	return events, nil
}

// GetStatistics is implemented in ready.go

// RegisterInstance registers a new executor instance using PostgreSQL upsert
func (s *PostgresStorage) RegisterInstance(ctx context.Context, instance *types.ExecutorInstance) error {
	// Validate the instance before inserting
	if err := instance.Validate(); err != nil {
		return fmt.Errorf("invalid executor instance: %w", err)
	}

	query := `
		INSERT INTO executor_instances (
			instance_id, hostname, pid, status, started_at, last_heartbeat, version, metadata
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		ON CONFLICT(instance_id) DO UPDATE SET
			hostname = EXCLUDED.hostname,
			pid = EXCLUDED.pid,
			status = EXCLUDED.status,
			last_heartbeat = EXCLUDED.last_heartbeat,
			version = EXCLUDED.version,
			metadata = EXCLUDED.metadata
	`

	_, err := s.pool.Exec(ctx, query,
		instance.InstanceID,
		instance.Hostname,
		instance.PID,
		instance.Status,
		instance.StartedAt,
		instance.LastHeartbeat,
		instance.Version,
		instance.Metadata,
	)

	if err != nil {
		return fmt.Errorf("failed to register executor instance: %w", err)
	}

	return nil
}

// UpdateHeartbeat updates the last_heartbeat timestamp for an executor instance
func (s *PostgresStorage) UpdateHeartbeat(ctx context.Context, instanceID string) error {
	query := `
		UPDATE executor_instances
		SET last_heartbeat = $1
		WHERE instance_id = $2
	`

	result, err := s.pool.Exec(ctx, query, time.Now(), instanceID)
	if err != nil {
		return fmt.Errorf("failed to update heartbeat: %w", err)
	}

	rows := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("executor instance not found: %s", instanceID)
	}

	return nil
}

// GetActiveInstances returns all executor instances with status='running'
func (s *PostgresStorage) GetActiveInstances(ctx context.Context) ([]*types.ExecutorInstance, error) {
	query := `
		SELECT instance_id, hostname, pid, status, started_at, last_heartbeat, version, metadata
		FROM executor_instances
		WHERE status = 'running'
		ORDER BY last_heartbeat DESC
	`

	rows, err := s.pool.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to query active instances: %w", err)
	}
	defer rows.Close()

	var instances []*types.ExecutorInstance
	for rows.Next() {
		instance := &types.ExecutorInstance{}

		err := rows.Scan(
			&instance.InstanceID,
			&instance.Hostname,
			&instance.PID,
			&instance.Status,
			&instance.StartedAt,
			&instance.LastHeartbeat,
			&instance.Version,
			&instance.Metadata,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan executor instance: %w", err)
		}

		instances = append(instances, instance)
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating executor instances: %w", err)
	}

	return instances, nil
}

// CleanupStaleInstances marks instances as 'stopped' if their last_heartbeat
// is older than staleThreshold seconds. Returns the number of instances cleaned up.
func (s *PostgresStorage) CleanupStaleInstances(ctx context.Context, staleThreshold int) (int, error) {
	// Calculate the cutoff time in Go, then compare
	cutoffTime := time.Now().Add(-time.Duration(staleThreshold) * time.Second)

	query := `
		UPDATE executor_instances
		SET status = 'stopped'
		WHERE status = 'running'
		  AND last_heartbeat < $1
	`

	result, err := s.pool.Exec(ctx, query, cutoffTime)
	if err != nil {
		return 0, fmt.Errorf("failed to cleanup stale instances: %w", err)
	}

	rows := result.RowsAffected()
	return int(rows), nil
}

// Execution state methods are implemented in execution_state.go

// Helper function to scan issues from rows
func scanIssues(rows pgx.Rows) ([]*types.Issue, error) {
	var issues []*types.Issue
	for rows.Next() {
		var issue types.Issue
		var closedAt *time.Time
		var estimatedMinutes *int
		var assignee *string

		err := rows.Scan(
			&issue.ID, &issue.Title, &issue.Description, &issue.Design,
			&issue.AcceptanceCriteria, &issue.Notes, &issue.Status,
			&issue.Priority, &issue.IssueType, &assignee, &estimatedMinutes,
			&issue.CreatedAt, &issue.UpdatedAt, &closedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan issue: %w", err)
		}

		issue.ClosedAt = closedAt
		issue.EstimatedMinutes = estimatedMinutes
		if assignee != nil {
			issue.Assignee = *assignee
		}

		issues = append(issues, &issue)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating issue rows: %w", err)
	}

	return issues, nil
}
