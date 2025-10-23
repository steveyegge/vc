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
	"fmt"

	"github.com/steveyegge/beads"
	beadsTypes "github.com/steveyegge/beads/internal/types"
	"github.com/steveyegge/vc/internal/events"
	"github.com/steveyegge/vc/internal/types"
)

// VCStorage wraps Beads storage and adds VC-specific extensions
type VCStorage struct {
	beads.Storage          // Embedded - all Beads operations available
	db            *sql.DB  // Direct DB access for VC extension tables
	dbPath        string   // Path to database file
}

// NewVCStorage creates a VC storage instance using Beads as the underlying storage
func NewVCStorage(ctx context.Context, dbPath string) (*VCStorage, error) {
	// 1. Open Beads storage (creates core tables: issues, dependencies, labels, etc.)
	beadsStore, err := beads.NewSQLiteStorage(dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open Beads storage: %w", err)
	}

	// 2. Get underlying DB connection for VC extension tables
	db := beadsStore.UnderlyingDB()
	if db == nil {
		return nil, fmt.Errorf("Beads storage did not provide underlying DB")
	}

	// 3. Create VC extension tables
	if err := createVCExtensionTables(ctx, db); err != nil {
		return nil, fmt.Errorf("failed to create VC extension tables: %w", err)
	}

	return &VCStorage{
		Storage: beadsStore,
		db:      db,
		dbPath:  dbPath,
	}, nil
}

// createVCExtensionTables creates VC-specific tables in the Beads database
// These tables extend Beads with mission workflow metadata
func createVCExtensionTables(ctx context.Context, db *sql.DB) error {
	_, err := db.ExecContext(ctx, vcExtensionSchema)
	if err != nil {
		return fmt.Errorf("failed to create VC extension schema: %w", err)
	}
	return nil
}

// VC-specific extension schema
// These tables coexist with Beads core tables in the same database
const vcExtensionSchema = `
-- VC Extension Tables
-- These tables extend Beads issues with mission workflow metadata
-- Following the IntelliJ/Android Studio extensibility model

-- Mission state (maps issue_id → mission metadata)
CREATE TABLE IF NOT EXISTS vc_mission_state (
    issue_id TEXT PRIMARY KEY,
    subtype TEXT NOT NULL CHECK(subtype IN ('mission', 'phase', 'review')),
    sandbox_path TEXT,           -- '.sandboxes/mission-300/'
    branch_name TEXT,            -- 'mission/vc-300-user-auth'
    iteration_count INTEGER DEFAULT 0,
    last_gates_run DATETIME,
    gates_status TEXT CHECK(gates_status IN ('pending', 'running', 'passed', 'failed')),
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (issue_id) REFERENCES issues(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_vc_mission_subtype ON vc_mission_state(subtype);
CREATE INDEX IF NOT EXISTS idx_vc_mission_gates ON vc_mission_state(gates_status);

-- Agent events (activity feed for VC execution)
-- Separate from Beads 'events' table which tracks issue lifecycle
CREATE TABLE IF NOT EXISTS vc_agent_events (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    timestamp DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    issue_id TEXT,
    type TEXT NOT NULL,
    severity TEXT CHECK(severity IN ('info', 'warning', 'error')),
    message TEXT NOT NULL,
    data TEXT,  -- JSON blob with event-specific details
    FOREIGN KEY (issue_id) REFERENCES issues(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_vc_agent_events_issue ON vc_agent_events(issue_id);
CREATE INDEX IF NOT EXISTS idx_vc_agent_events_timestamp ON vc_agent_events(timestamp);
CREATE INDEX IF NOT EXISTS idx_vc_agent_events_type ON vc_agent_events(type);

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

CREATE INDEX IF NOT EXISTS idx_vc_executor_status ON vc_executor_instances(status);
CREATE INDEX IF NOT EXISTS idx_vc_executor_heartbeat ON vc_executor_instances(last_heartbeat);

-- Issue execution state (checkpoint/resume for long-running tasks)
CREATE TABLE IF NOT EXISTS vc_issue_execution_state (
    issue_id TEXT PRIMARY KEY,
    executor_instance_id TEXT,
    claimed_at DATETIME,
    state TEXT NOT NULL DEFAULT 'pending' CHECK(state IN ('pending', 'claimed', 'executing', 'analyzing', 'completed', 'failed')),
    checkpoint_data TEXT,  -- JSON blob for agent state
    error_message TEXT,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (issue_id) REFERENCES issues(id) ON DELETE CASCADE,
    FOREIGN KEY (executor_instance_id) REFERENCES vc_executor_instances(id) ON DELETE SET NULL
);

CREATE INDEX IF NOT EXISTS idx_vc_execution_state ON vc_issue_execution_state(state);
CREATE INDEX IF NOT EXISTS idx_vc_execution_executor ON vc_issue_execution_state(executor_instance_id);

-- Execution history (audit trail of execution attempts)
CREATE TABLE IF NOT EXISTS vc_execution_history (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    issue_id TEXT NOT NULL,
    executor_instance_id TEXT,
    started_at DATETIME NOT NULL,
    completed_at DATETIME,
    state TEXT NOT NULL,
    error_message TEXT,
    FOREIGN KEY (issue_id) REFERENCES issues(id) ON DELETE CASCADE,
    FOREIGN KEY (executor_instance_id) REFERENCES vc_executor_instances(id) ON DELETE SET NULL
);

CREATE INDEX IF NOT EXISTS idx_vc_history_issue ON vc_execution_history(issue_id);
CREATE INDEX IF NOT EXISTS idx_vc_history_started ON vc_execution_history(started_at);
`

// ======================================================================
// VC-SPECIFIC METHODS (Extension Operations)
// ======================================================================

// StoreAgentEvent stores a VC agent event in the extension table
func (s *VCStorage) StoreAgentEvent(ctx context.Context, event *events.AgentEvent) error {
	// Convert event data to JSON if present
	var dataJSON string
	if event.Data != nil {
		// TODO: Marshal event.Data to JSON
		// For now, assume Data is already a JSON string
		dataJSON = fmt.Sprintf("%v", event.Data)
	}

	_, err := s.db.ExecContext(ctx, `
		INSERT INTO vc_agent_events (timestamp, issue_id, type, severity, message, data)
		VALUES (?, ?, ?, ?, ?, ?)
	`, event.Timestamp, event.IssueID, event.Type, event.Severity, event.Message, dataJSON)

	if err != nil {
		return fmt.Errorf("failed to store agent event: %w", err)
	}
	return nil
}

// GetAgentEvents retrieves agent events matching the filter
func (s *VCStorage) GetAgentEvents(ctx context.Context, filter events.EventFilter) ([]*events.AgentEvent, error) {
	// TODO: Implement with proper filtering
	return nil, fmt.Errorf("GetAgentEvents not yet implemented")
}

// GetAgentEventsByIssue retrieves all agent events for a specific issue
func (s *VCStorage) GetAgentEventsByIssue(ctx context.Context, issueID string) ([]*events.AgentEvent, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, timestamp, issue_id, type, severity, message, data
		FROM vc_agent_events
		WHERE issue_id = ?
		ORDER BY timestamp
	`, issueID)
	if err != nil {
		return nil, fmt.Errorf("failed to query agent events: %w", err)
	}
	defer rows.Close()

	var result []*events.AgentEvent
	for rows.Next() {
		var e events.AgentEvent
		var dataJSON sql.NullString
		if err := rows.Scan(&e.ID, &e.Timestamp, &e.IssueID, &e.Type, &e.Severity, &e.Message, &dataJSON); err != nil {
			return nil, fmt.Errorf("failed to scan agent event: %w", err)
		}
		// TODO: Unmarshal dataJSON to e.Data
		result = append(result, &e)
	}

	return result, rows.Err()
}

// GetRecentAgentEvents retrieves the most recent N agent events
func (s *VCStorage) GetRecentAgentEvents(ctx context.Context, limit int) ([]*events.AgentEvent, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, timestamp, issue_id, type, severity, message, data
		FROM vc_agent_events
		ORDER BY timestamp DESC
		LIMIT ?
	`, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to query recent agent events: %w", err)
	}
	defer rows.Close()

	var result []*events.AgentEvent
	for rows.Next() {
		var e events.AgentEvent
		var dataJSON sql.NullString
		if err := rows.Scan(&e.ID, &e.Timestamp, &e.IssueID, &e.Type, &e.Severity, &e.Message, &dataJSON); err != nil {
			return nil, fmt.Errorf("failed to scan agent event: %w", err)
		}
		// TODO: Unmarshal dataJSON to e.Data
		result = append(result, &e)
	}

	return result, rows.Err()
}

// ======================================================================
// TYPE CONVERSION HELPERS
// ======================================================================

// Convert Beads types to VC types
func beadsIssueToVC(bi *beadsTypes.Issue) *types.Issue {
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
func vcIssueToBeads(vi *types.Issue) *beadsTypes.Issue {
	if vi == nil {
		return nil
	}
	return &beadsTypes.Issue{
		ID:                 vi.ID,
		Title:              vi.Title,
		Description:        vi.Description,
		Design:             vi.Design,
		AcceptanceCriteria: vi.AcceptanceCriteria,
		Notes:              vi.Notes,
		Status:             beadsTypes.Status(vi.Status),
		Priority:           vi.Priority,
		IssueType:          beadsTypes.IssueType(vi.IssueType),
		// IssueSubtype is VC-specific, stored in extension table
		Assignee:         vi.Assignee,
		EstimatedMinutes: vi.EstimatedMinutes,
		CreatedAt:        vi.CreatedAt,
		UpdatedAt:        vi.UpdatedAt,
		ClosedAt:         vi.ClosedAt,
	}
}
