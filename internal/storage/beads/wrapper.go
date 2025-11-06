// Package beads provides a wrapper around the Beads storage library
// with VC-specific extensions.
//
// Architecture:
// - Beads provides core issue tracking (issues, dependencies, labels, events)
// - VC adds extension tables for workflow engine state (vc_mission_state, vc_agent_events)
// - Both use the same SQLite database (.beads/vc.db)
// - Foreign keys connect VC extension tables to Beads core tables
//
// This follows the IntelliJ/Android Studio model:
// - Beads is the general-purpose platform (no VC-specific code)
// - VC is the extension (adds own tables, doesn't modify Beads schema)
package beads

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"

	beadsLib "github.com/steveyegge/beads"
	"github.com/steveyegge/vc/internal/events"
	"github.com/steveyegge/vc/internal/types"
)

// VCStorage wraps Beads storage and adds VC-specific extensions
type VCStorage struct {
	beadsLib.Storage       // Embedded - all Beads operations available
	db               *sql.DB  // Direct DB access for VC extension tables
	dbPath           string   // Path to database file
}

// NewVCStorage creates a VC storage instance using Beads as the underlying storage
func NewVCStorage(ctx context.Context, dbPath string) (*VCStorage, error) {
	// 1. Open Beads storage (creates core tables: issues, dependencies, labels, etc.)
	beadsStore, err := beadsLib.NewSQLiteStorage(dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open Beads storage: %w", err)
	}

	// 1.5. Initialize issue_prefix config if not already set (required by Beads for ID generation)
	if prefix, err := beadsStore.GetConfig(ctx, "issue_prefix"); err != nil || prefix == "" {
		// Set default prefix "vc" for VC project
		if err := beadsStore.SetConfig(ctx, "issue_prefix", "vc"); err != nil {
			beadsStore.Close()
			return nil, fmt.Errorf("failed to set issue_prefix config: %w", err)
		}
	}

	// 2. Get underlying DB connection pool for regular queries (cached)
	db := beadsStore.UnderlyingDB()
	if db == nil {
		return nil, fmt.Errorf("beads storage did not provide underlying DB")
	}

	// 3. Create VC extension tables using scoped connection for DDL
	// Use UnderlyingConn(ctx) for DDL operations as recommended by Beads
	conn, err := beadsStore.UnderlyingConn(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get connection for DDL: %w", err)
	}
	defer conn.Close()

	if err := createVCExtensionTables(ctx, conn); err != nil {
		return nil, fmt.Errorf("failed to create VC extension tables: %w", err)
	}

	return &VCStorage{
		Storage: beadsStore,
		db:      db,
		dbPath:  dbPath,
	}, nil
}

// Close closes the storage connection and releases resources.
// This delegates to the embedded Beads storage which owns the database connection.
// After Close() is called, all subsequent operations will fail.
func (s *VCStorage) Close() error {
	// Beads owns the DB connection (s.db is the same underlying connection)
	// so we just delegate to Beads.Storage.Close() which closes the DB
	return s.Storage.Close()
}

// GetDB returns the underlying database connection for advanced operations.
// This is primarily used by CLI commands that need direct SQL access.
func (s *VCStorage) GetDB() interface{} {
	return s.db
}

// createVCExtensionTables creates VC-specific tables in the Beads database
// These tables extend Beads with mission workflow metadata
// Uses a scoped connection (*sql.Conn) for DDL operations as recommended by Beads
func createVCExtensionTables(ctx context.Context, conn *sql.Conn) error {
	// Step 1: Create tables (without indexes that depend on columns that might not exist)
	_, err := conn.ExecContext(ctx, vcExtensionTableSchema)
	if err != nil {
		return fmt.Errorf("failed to create VC extension tables: %w", err)
	}

	// Step 2: Run migrations to add missing columns to existing tables
	// This must run BEFORE creating indexes that depend on those columns
	if err := migrateAgentEventsTable(ctx, conn); err != nil {
		return fmt.Errorf("failed to migrate agent_events table: %w", err)
	}

	// vc-165b: Migrate execution state table for intervention tracking
	if err := migrateExecutionStateTable(ctx, conn); err != nil {
		return fmt.Errorf("failed to migrate execution_state table: %w", err)
	}

	// Step 3: Create indexes (now that all columns exist)
	_, err = conn.ExecContext(ctx, vcExtensionIndexSchema)
	if err != nil {
		return fmt.Errorf("failed to create VC extension indexes: %w", err)
	}

	return nil
}

// migrateAgentEventsTable adds missing columns to existing vc_agent_events tables
// Uses a scoped connection (*sql.Conn) for DDL operations as recommended by Beads
func migrateAgentEventsTable(ctx context.Context, conn *sql.Conn) error {
	// Check if executor_id column exists
	var hasExecutorID bool
	err := conn.QueryRowContext(ctx, `
		SELECT COUNT(*) > 0
		FROM pragma_table_info('vc_agent_events')
		WHERE name = 'executor_id'
	`).Scan(&hasExecutorID)
	if err != nil {
		return fmt.Errorf("failed to check for executor_id column: %w", err)
	}

	if !hasExecutorID {
		// Add executor_id column
		_, err = conn.ExecContext(ctx, `
			ALTER TABLE vc_agent_events ADD COLUMN executor_id TEXT
		`)
		if err != nil {
			return fmt.Errorf("failed to add executor_id column: %w", err)
		}

		// Create index
		_, err = conn.ExecContext(ctx, `
			CREATE INDEX IF NOT EXISTS idx_vc_agent_events_executor ON vc_agent_events(executor_id)
		`)
		if err != nil {
			return fmt.Errorf("failed to create executor_id index: %w", err)
		}
	}

	// Check if agent_id column exists
	var hasAgentID bool
	err = conn.QueryRowContext(ctx, `
		SELECT COUNT(*) > 0
		FROM pragma_table_info('vc_agent_events')
		WHERE name = 'agent_id'
	`).Scan(&hasAgentID)
	if err != nil {
		return fmt.Errorf("failed to check for agent_id column: %w", err)
	}

	if !hasAgentID {
		// Add agent_id column
		_, err = conn.ExecContext(ctx, `
			ALTER TABLE vc_agent_events ADD COLUMN agent_id TEXT
		`)
		if err != nil {
			return fmt.Errorf("failed to add agent_id column: %w", err)
		}
	}

	// Check if source_line column exists
	var hasSourceLine bool
	err = conn.QueryRowContext(ctx, `
		SELECT COUNT(*) > 0
		FROM pragma_table_info('vc_agent_events')
		WHERE name = 'source_line'
	`).Scan(&hasSourceLine)
	if err != nil {
		return fmt.Errorf("failed to check for source_line column: %w", err)
	}

	if !hasSourceLine {
		// Add source_line column
		_, err = conn.ExecContext(ctx, `
			ALTER TABLE vc_agent_events ADD COLUMN source_line INTEGER DEFAULT 0
		`)
		if err != nil {
			return fmt.Errorf("failed to add source_line column: %w", err)
		}
	}

	return nil
}

// migrateExecutionStateTable adds intervention tracking columns to vc_issue_execution_state (vc-165b)
// Uses a scoped connection (*sql.Conn) for DDL operations as recommended by Beads
func migrateExecutionStateTable(ctx context.Context, conn *sql.Conn) error {
	// Check if intervention_count column exists
	var hasInterventionCount bool
	err := conn.QueryRowContext(ctx, `
		SELECT COUNT(*) > 0
		FROM pragma_table_info('vc_issue_execution_state')
		WHERE name = 'intervention_count'
	`).Scan(&hasInterventionCount)
	if err != nil {
		return fmt.Errorf("failed to check for intervention_count column: %w", err)
	}

	if !hasInterventionCount {
		// Add intervention_count column
		_, err = conn.ExecContext(ctx, `
			ALTER TABLE vc_issue_execution_state ADD COLUMN intervention_count INTEGER DEFAULT 0
		`)
		if err != nil {
			return fmt.Errorf("failed to add intervention_count column: %w", err)
		}
	}

	// Check if last_intervention_time column exists
	var hasLastInterventionTime bool
	err = conn.QueryRowContext(ctx, `
		SELECT COUNT(*) > 0
		FROM pragma_table_info('vc_issue_execution_state')
		WHERE name = 'last_intervention_time'
	`).Scan(&hasLastInterventionTime)
	if err != nil {
		return fmt.Errorf("failed to check for last_intervention_time column: %w", err)
	}

	if !hasLastInterventionTime {
		// Add last_intervention_time column
		_, err = conn.ExecContext(ctx, `
			ALTER TABLE vc_issue_execution_state ADD COLUMN last_intervention_time DATETIME
		`)
		if err != nil {
			return fmt.Errorf("failed to add last_intervention_time column: %w", err)
		}

		// Create index for efficient backoff queries
		_, err = conn.ExecContext(ctx, `
			CREATE INDEX IF NOT EXISTS idx_vc_execution_intervention ON vc_issue_execution_state(intervention_count, last_intervention_time)
		`)
		if err != nil {
			return fmt.Errorf("failed to create intervention index: %w", err)
		}
	}

	return nil
}

// VC-specific extension schema - TABLE DEFINITIONS ONLY
// These tables coexist with Beads core tables in the same database
// Following the IntelliJ/Android Studio extensibility model
// vc-126: Split into two parts to allow migration between table and index creation
const vcExtensionTableSchema = `
-- VC Extension Tables
-- These tables extend Beads issues with mission workflow metadata

-- Mission state (maps issue_id → mission metadata)
CREATE TABLE IF NOT EXISTS vc_mission_state (
    issue_id TEXT PRIMARY KEY,
    subtype TEXT NOT NULL CHECK(subtype IN ('mission', 'phase', 'review')),
    sandbox_path TEXT,           -- '.sandboxes/mission-300/'
    branch_name TEXT,            -- 'mission/vc-300-user-auth'
    iteration_count INTEGER DEFAULT 0,
    last_gates_run DATETIME,
    gates_status TEXT CHECK(gates_status IN ('pending', 'running', 'passed', 'failed')),
    goal TEXT,                   -- High-level mission goal
    context TEXT,                -- Additional planning context
    phase_count INTEGER DEFAULT 0,       -- Number of phases in plan
    current_phase INTEGER DEFAULT 0,     -- Current phase being executed (0-indexed)
    approval_required BOOLEAN DEFAULT FALSE,  -- Requires human approval before execution
    approved_at DATETIME,        -- When plan was approved
    approved_by TEXT,            -- Who approved the plan
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (issue_id) REFERENCES issues(id) ON DELETE CASCADE
);

-- Agent events (activity feed for VC execution)
-- Separate from Beads 'events' table which tracks issue lifecycle
CREATE TABLE IF NOT EXISTS vc_agent_events (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    timestamp DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    issue_id TEXT,     -- Issue reference (no FK constraint to allow system-level events, vc-128)
    executor_id TEXT,  -- Executor instance that created this event (no FK constraint for flexibility)
    agent_id TEXT,     -- Agent that created this event (if applicable)
    type TEXT NOT NULL,
    severity TEXT CHECK(severity IN ('info', 'warning', 'error')),
    message TEXT NOT NULL,
    data TEXT,  -- JSON blob with event-specific details
    source_line INTEGER DEFAULT 0  -- Line number in agent output (if applicable)
    -- No FK constraints: events are logs/metrics, system-level events use NULL issue_id
);

-- Executor instances (for tracking active VC executors)
CREATE TABLE IF NOT EXISTS vc_executor_instances (
    id TEXT PRIMARY KEY,
    hostname TEXT NOT NULL,
    pid INTEGER NOT NULL,
    version TEXT NOT NULL,
    started_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    last_heartbeat DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    status TEXT NOT NULL DEFAULT 'running' CHECK(status IN ('running', 'stopped', 'crashed'))
);

-- Issue execution state (checkpoint/resume for long-running tasks)
CREATE TABLE IF NOT EXISTS vc_issue_execution_state (
    issue_id TEXT PRIMARY KEY,
    executor_instance_id TEXT,
    claimed_at DATETIME,
    state TEXT NOT NULL DEFAULT 'pending' CHECK(state IN ('pending', 'claimed', 'assessing', 'executing', 'analyzing', 'gates', 'committing', 'completed', 'failed')),
    checkpoint_data TEXT,  -- JSON blob for agent state
    error_message TEXT,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (issue_id) REFERENCES issues(id) ON DELETE CASCADE,
    FOREIGN KEY (executor_instance_id) REFERENCES vc_executor_instances(id) ON DELETE SET NULL
);

-- Execution history (audit trail of execution attempts)
CREATE TABLE IF NOT EXISTS vc_execution_history (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    issue_id TEXT NOT NULL,
    executor_instance_id TEXT,
    attempt_number INTEGER NOT NULL,
    started_at DATETIME NOT NULL,
    completed_at DATETIME,
    success BOOLEAN,
    exit_code INTEGER,
    summary TEXT,
    output_sample TEXT,
    error_sample TEXT,
    FOREIGN KEY (issue_id) REFERENCES issues(id) ON DELETE CASCADE,
    FOREIGN KEY (executor_instance_id) REFERENCES vc_executor_instances(id) ON DELETE SET NULL
);

-- Gate baselines (cache of preflight gate results by commit hash)
-- vc-198: Pre-flight quality gates to prevent work on broken baseline
CREATE TABLE IF NOT EXISTS vc_gate_baselines (
    commit_hash TEXT PRIMARY KEY,
    branch_name TEXT NOT NULL,
    timestamp DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    all_passed BOOLEAN NOT NULL,
    results_json TEXT NOT NULL,  -- JSON map of gate results: {"test": {"passed": true, "output": "..."}, ...}
    sandbox_path TEXT            -- Optional: for future Phase 3 sandbox reuse
);

-- Code review checkpoints (tracks when code review sweeps were performed)
-- vc-1: Activity-based AI code review sweep
CREATE TABLE IF NOT EXISTS vc_review_checkpoints (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    commit_sha TEXT NOT NULL,
    timestamp DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    review_scope TEXT NOT NULL,  -- "quick" | "thorough" | "targeted:path/to/dir"
    review_issue_id TEXT         -- Reference to the review issue that was created (if any)
);
`

// VC-specific extension schema - INDEX DEFINITIONS
// vc-126: Indexes created AFTER migrations to ensure columns exist
const vcExtensionIndexSchema = `
-- Indexes for VC Extension Tables

-- Mission state indexes
CREATE INDEX IF NOT EXISTS idx_vc_mission_subtype ON vc_mission_state(subtype);
CREATE INDEX IF NOT EXISTS idx_vc_mission_gates ON vc_mission_state(gates_status);

-- Agent events indexes
CREATE INDEX IF NOT EXISTS idx_vc_agent_events_issue ON vc_agent_events(issue_id);
CREATE INDEX IF NOT EXISTS idx_vc_agent_events_executor ON vc_agent_events(executor_id);
CREATE INDEX IF NOT EXISTS idx_vc_agent_events_timestamp ON vc_agent_events(timestamp);
CREATE INDEX IF NOT EXISTS idx_vc_agent_events_type ON vc_agent_events(type);

-- Executor instances indexes
CREATE INDEX IF NOT EXISTS idx_vc_executor_status ON vc_executor_instances(status);
CREATE INDEX IF NOT EXISTS idx_vc_executor_heartbeat ON vc_executor_instances(last_heartbeat);

-- Issue execution state indexes
CREATE INDEX IF NOT EXISTS idx_vc_execution_state ON vc_issue_execution_state(state);
CREATE INDEX IF NOT EXISTS idx_vc_execution_executor ON vc_issue_execution_state(executor_instance_id);

-- Execution history indexes
CREATE INDEX IF NOT EXISTS idx_vc_history_issue ON vc_execution_history(issue_id);
CREATE INDEX IF NOT EXISTS idx_vc_history_started ON vc_execution_history(started_at);

-- Gate baselines indexes
CREATE INDEX IF NOT EXISTS idx_vc_gate_baselines_timestamp ON vc_gate_baselines(timestamp);
CREATE INDEX IF NOT EXISTS idx_vc_gate_baselines_branch ON vc_gate_baselines(branch_name);

-- Code review checkpoints indexes
CREATE INDEX IF NOT EXISTS idx_vc_review_checkpoints_timestamp ON vc_review_checkpoints(timestamp);
CREATE INDEX IF NOT EXISTS idx_vc_review_checkpoints_commit ON vc_review_checkpoints(commit_sha);
`

// ======================================================================
// VC-SPECIFIC METHODS (Extension Operations)
// ======================================================================

// StoreAgentEvent stores a VC agent event in the extension table
func (s *VCStorage) StoreAgentEvent(ctx context.Context, event *events.AgentEvent) error {
	// Convert event data to JSON if present
	var dataJSON string
	if event.Data != nil {
		jsonBytes, err := json.Marshal(event.Data)
		if err != nil {
			return fmt.Errorf("failed to marshal event data: %w", err)
		}
		dataJSON = string(jsonBytes)
	}

	// Convert empty issue_id to NULL to avoid FK constraint violation for system events (vc-100)
	var issueID interface{}
	if event.IssueID == "" {
		issueID = nil
	} else {
		issueID = event.IssueID
	}

	// Convert empty executor_id and agent_id to NULL
	var executorID interface{}
	if event.ExecutorID == "" {
		executorID = nil
	} else {
		executorID = event.ExecutorID
	}

	var agentID interface{}
	if event.AgentID == "" {
		agentID = nil
	} else {
		agentID = event.AgentID
	}

	_, err := s.db.ExecContext(ctx, `
		INSERT INTO vc_agent_events (timestamp, issue_id, executor_id, agent_id, type, severity, message, data, source_line)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, event.Timestamp, issueID, executorID, agentID, event.Type, event.Severity, event.Message, dataJSON, event.SourceLine)

	if err != nil {
		return fmt.Errorf("failed to store agent event: %w", err)
	}
	return nil
}

// GetAgentEvents retrieves agent events matching the filter
func (s *VCStorage) GetAgentEvents(ctx context.Context, filter events.EventFilter) ([]*events.AgentEvent, error) {
	// Build WHERE clause dynamically based on filter
	var whereClauses []string
	var args []interface{}

	if filter.IssueID != "" {
		whereClauses = append(whereClauses, "issue_id = ?")
		args = append(args, filter.IssueID)
	}

	if filter.Type != "" {
		whereClauses = append(whereClauses, "type = ?")
		args = append(args, filter.Type)
	}

	if filter.Severity != "" {
		whereClauses = append(whereClauses, "severity = ?")
		args = append(args, filter.Severity)
	}

	if !filter.AfterTime.IsZero() {
		whereClauses = append(whereClauses, "timestamp > ?")
		args = append(args, filter.AfterTime)
	}

	if !filter.BeforeTime.IsZero() {
		whereClauses = append(whereClauses, "timestamp <= ?")
		args = append(args, filter.BeforeTime)
	}

	// Build the query
	query := `SELECT id, timestamp, issue_id, executor_id, agent_id, type, severity, message, data, source_line FROM vc_agent_events`
	if len(whereClauses) > 0 {
		query += " WHERE " + strings.Join(whereClauses, " AND ")
	}
	query += " ORDER BY timestamp DESC"

	if filter.Limit > 0 {
		query += " LIMIT ?"
		args = append(args, filter.Limit)
	}

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query agent events: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var result []*events.AgentEvent
	for rows.Next() {
		var e events.AgentEvent
		var issueID, executorID, agentID, severity sql.NullString
		var dataJSON sql.NullString
		var sourceLine sql.NullInt64
		if err := rows.Scan(&e.ID, &e.Timestamp, &issueID, &executorID, &agentID, &e.Type, &severity, &e.Message, &dataJSON, &sourceLine); err != nil {
			return nil, fmt.Errorf("failed to scan agent event: %w", err)
		}
		if issueID.Valid {
			e.IssueID = issueID.String
		}
		if executorID.Valid {
			e.ExecutorID = executorID.String
		}
		if agentID.Valid {
			e.AgentID = agentID.String
		}
		if severity.Valid {
			e.Severity = events.EventSeverity(severity.String)
		}
		if sourceLine.Valid {
			e.SourceLine = int(sourceLine.Int64)
		}
		if dataJSON.Valid && dataJSON.String != "" {
			if err := json.Unmarshal([]byte(dataJSON.String), &e.Data); err != nil {
				return nil, fmt.Errorf("failed to unmarshal event data: %w", err)
			}
		}
		result = append(result, &e)
	}

	return result, rows.Err()
}

// GetAgentEventsByIssue retrieves all agent events for a specific issue
func (s *VCStorage) GetAgentEventsByIssue(ctx context.Context, issueID string) ([]*events.AgentEvent, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, timestamp, issue_id, executor_id, agent_id, type, severity, message, data, source_line
		FROM vc_agent_events
		WHERE issue_id = ?
		ORDER BY timestamp
	`, issueID)
	if err != nil {
		return nil, fmt.Errorf("failed to query agent events: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var result []*events.AgentEvent
	for rows.Next() {
		var e events.AgentEvent
		var issueIDNull, executorID, agentID, severity sql.NullString
		var dataJSON sql.NullString
		var sourceLine sql.NullInt64
		if err := rows.Scan(&e.ID, &e.Timestamp, &issueIDNull, &executorID, &agentID, &e.Type, &severity, &e.Message, &dataJSON, &sourceLine); err != nil {
			return nil, fmt.Errorf("failed to scan agent event: %w", err)
		}
		if issueIDNull.Valid {
			e.IssueID = issueIDNull.String
		}
		if executorID.Valid {
			e.ExecutorID = executorID.String
		}
		if agentID.Valid {
			e.AgentID = agentID.String
		}
		if severity.Valid {
			e.Severity = events.EventSeverity(severity.String)
		}
		if sourceLine.Valid {
			e.SourceLine = int(sourceLine.Int64)
		}
		if dataJSON.Valid && dataJSON.String != "" {
			if err := json.Unmarshal([]byte(dataJSON.String), &e.Data); err != nil {
				return nil, fmt.Errorf("failed to unmarshal event data: %w", err)
			}
		}
		result = append(result, &e)
	}

	return result, rows.Err()
}

// GetRecentAgentEvents retrieves the most recent N agent events
func (s *VCStorage) GetRecentAgentEvents(ctx context.Context, limit int) ([]*events.AgentEvent, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, timestamp, issue_id, executor_id, agent_id, type, severity, message, data, source_line
		FROM vc_agent_events
		ORDER BY timestamp DESC
		LIMIT ?
	`, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to query recent agent events: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var result []*events.AgentEvent
	for rows.Next() {
		var e events.AgentEvent
		var issueIDNull, executorID, agentID, severity sql.NullString
		var dataJSON sql.NullString
		var sourceLine sql.NullInt64
		if err := rows.Scan(&e.ID, &e.Timestamp, &issueIDNull, &executorID, &agentID, &e.Type, &severity, &e.Message, &dataJSON, &sourceLine); err != nil {
			return nil, fmt.Errorf("failed to scan agent event: %w", err)
		}
		if issueIDNull.Valid {
			e.IssueID = issueIDNull.String
		}
		if executorID.Valid {
			e.ExecutorID = executorID.String
		}
		if agentID.Valid {
			e.AgentID = agentID.String
		}
		if severity.Valid {
			e.Severity = events.EventSeverity(severity.String)
		}
		if sourceLine.Valid {
			e.SourceLine = int(sourceLine.Int64)
		}
		if dataJSON.Valid && dataJSON.String != "" {
			if err := json.Unmarshal([]byte(dataJSON.String), &e.Data); err != nil {
				return nil, fmt.Errorf("failed to unmarshal event data: %w", err)
			}
		}
		result = append(result, &e)
	}

	return result, rows.Err()
}

// ======================================================================
// TYPE CONVERSION HELPERS
// ======================================================================

// Convert Beads types to VC types
func beadsIssueToVC(bi *beadsLib.Issue) *types.Issue {
	if bi == nil {
		return nil
	}
	return &types.Issue{
		ID:                 bi.ID,
		Title:              bi.Title,
		Description:        bi.Description,
		Design:             bi.Design,
		AcceptanceCriteria: bi.AcceptanceCriteria,
		Notes:              bi.Notes,
		Status:             types.Status(bi.Status),
		Priority:           bi.Priority,
		IssueType:          types.IssueType(bi.IssueType),
		// IssueSubtype is in VC extension table, not Beads
		Assignee:         bi.Assignee,
		EstimatedMinutes: bi.EstimatedMinutes,
		CreatedAt:        bi.CreatedAt,
		UpdatedAt:        bi.UpdatedAt,
		ClosedAt:         bi.ClosedAt,
	}
}

// Convert VC types to Beads types
func vcIssueToBeads(vi *types.Issue) *beadsLib.Issue {
	if vi == nil {
		return nil
	}
	return &beadsLib.Issue{
		ID:                 vi.ID,
		Title:              vi.Title,
		Description:        vi.Description,
		Design:             vi.Design,
		AcceptanceCriteria: vi.AcceptanceCriteria,
		Notes:              vi.Notes,
		Status:             beadsLib.Status(vi.Status),
		Priority:           vi.Priority,
		IssueType:          beadsLib.IssueType(vi.IssueType),
		// IssueSubtype is VC-specific, stored in extension table
		Assignee:         vi.Assignee,
		EstimatedMinutes: vi.EstimatedMinutes,
		CreatedAt:        vi.CreatedAt,
		UpdatedAt:        vi.UpdatedAt,
		ClosedAt:         vi.ClosedAt,
	}
}

// ======================================================================
// GATE BASELINE METHODS (vc-198: Preflight quality gates cache)
// ======================================================================

// GateBaseline represents a cached baseline of gate results for a specific commit
type GateBaseline struct {
	CommitHash  string
	BranchName  string
	Timestamp   string
	AllPassed   bool
	Results     map[string]*types.GateResult // Map of gate name -> result
	SandboxPath string                       // Optional: for Phase 3 sandbox reuse
}

// GetGateBaseline retrieves a cached baseline for the given commit hash
// Returns nil if no baseline exists for this commit
func (s *VCStorage) GetGateBaseline(ctx context.Context, commitHash string) (*GateBaseline, error) {
	var baseline GateBaseline
	var resultsJSON string
	var sandboxPath sql.NullString

	err := s.db.QueryRowContext(ctx, `
		SELECT commit_hash, branch_name, timestamp, all_passed, results_json, sandbox_path
		FROM vc_gate_baselines
		WHERE commit_hash = ?
	`, commitHash).Scan(
		&baseline.CommitHash,
		&baseline.BranchName,
		&baseline.Timestamp,
		&baseline.AllPassed,
		&resultsJSON,
		&sandboxPath,
	)

	if err == sql.ErrNoRows {
		return nil, nil // No baseline found (not an error)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to query gate baseline: %w", err)
	}

	// Parse results JSON
	if err := json.Unmarshal([]byte(resultsJSON), &baseline.Results); err != nil {
		return nil, fmt.Errorf("failed to unmarshal gate results: %w", err)
	}

	if sandboxPath.Valid {
		baseline.SandboxPath = sandboxPath.String
	}

	return &baseline, nil
}

// SetGateBaseline stores a baseline in the cache (replaces existing if present)
func (s *VCStorage) SetGateBaseline(ctx context.Context, baseline *GateBaseline) error {
	// Marshal results to JSON
	resultsJSON, err := json.Marshal(baseline.Results)
	if err != nil {
		return fmt.Errorf("failed to marshal gate results: %w", err)
	}

	// Use REPLACE to upsert (insert or update)
	_, err = s.db.ExecContext(ctx, `
		REPLACE INTO vc_gate_baselines (commit_hash, branch_name, timestamp, all_passed, results_json, sandbox_path)
		VALUES (?, ?, ?, ?, ?, ?)
	`, baseline.CommitHash, baseline.BranchName, baseline.Timestamp, baseline.AllPassed, string(resultsJSON), baseline.SandboxPath)

	if err != nil {
		return fmt.Errorf("failed to store gate baseline: %w", err)
	}

	return nil
}

// InvalidateGateBaseline removes a baseline from the cache
func (s *VCStorage) InvalidateGateBaseline(ctx context.Context, commitHash string) error {
	_, err := s.db.ExecContext(ctx, `
		DELETE FROM vc_gate_baselines WHERE commit_hash = ?
	`, commitHash)

	if err != nil {
		return fmt.Errorf("failed to invalidate gate baseline: %w", err)
	}

	return nil
}

// CleanupOldBaselines removes baselines older than the given age
// Used for cache cleanup to prevent unbounded growth
func (s *VCStorage) CleanupOldBaselines(ctx context.Context, maxAge string) (int, error) {
	result, err := s.db.ExecContext(ctx, `
		DELETE FROM vc_gate_baselines
		WHERE timestamp < datetime('now', ?)
	`, maxAge)

	if err != nil {
		return 0, fmt.Errorf("failed to cleanup old baselines: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("failed to get rows affected: %w", err)
	}

	return int(rowsAffected), nil
}
