package beads

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	beadsLib "github.com/steveyegge/beads"
	"github.com/steveyegge/vc/internal/events"
	"github.com/steveyegge/vc/internal/types"
)

// TestDatabaseTableMissingDetection tests detection of missing database tables at startup
func TestDatabaseTableMissingDetection(t *testing.T) {
	ctx := context.Background()

	t.Run("all required tables exist after initialization", func(t *testing.T) {
		// Create temporary database
		tmpDir := t.TempDir()
		dbPath := filepath.Join(tmpDir, "test.db")

		// Initialize VC storage (should create all tables)
		store, err := NewVCStorage(ctx, dbPath)
		if err != nil {
			t.Fatalf("Failed to create VC storage: %v", err)
		}
		defer func() { _ = store.Close() }()

		// Verify all Beads core tables exist
		beadsTables := []string{
			"issues",
			"dependencies",
			"labels",
			"events",
			"config",
		}

		db := store.db
		for _, tableName := range beadsTables {
			exists, err := tableExists(ctx, db, tableName)
			if err != nil {
				t.Fatalf("Failed to check if table %s exists: %v", tableName, err)
			}
			if !exists {
				t.Errorf("Required Beads table %s does not exist", tableName)
			}
		}

		// Verify all VC extension tables exist
		vcTables := []string{
			"vc_mission_state",
			"vc_agent_events",
			"vc_executor_instances",
			"vc_issue_execution_state",
			"vc_execution_history",
			"vc_gate_baselines",
			"vc_review_checkpoints",
			"vc_mission_plans",
		}

		for _, tableName := range vcTables {
			exists, err := tableExists(ctx, db, tableName)
			if err != nil {
				t.Fatalf("Failed to check if table %s exists: %v", tableName, err)
			}
			if !exists {
				t.Errorf("Required VC extension table %s does not exist", tableName)
			}
		}
	})

	t.Run("detect missing VC extension tables", func(t *testing.T) {
		// Create temporary database
		tmpDir := t.TempDir()
		dbPath := filepath.Join(tmpDir, "partial.db")

		// Create a database with only Beads core tables (no VC extensions)
		// by using Beads storage directly
		beadsStore, err := beadsLib.NewSQLiteStorage(context.Background(), dbPath)
		if err != nil {
			t.Fatalf("Failed to create Beads storage: %v", err)
		}
		// Close Beads storage to simulate a database with only Beads tables
		beadsStore.Close()

		// Now try to open with VCStorage - it should detect missing tables
		// and create them (current behavior is auto-creation, not error)
		store, err := NewVCStorage(ctx, dbPath)
		if err != nil {
			t.Fatalf("Failed to create VC storage: %v", err)
		}
		defer func() { _ = store.Close() }()

		// Verify that VC extension tables were created
		vcTables := []string{
			"vc_mission_state",
			"vc_agent_events",
			"vc_executor_instances",
		}

		for _, tableName := range vcTables {
			exists, err := tableExists(ctx, store.db, tableName)
			if err != nil {
				t.Fatalf("Failed to check if table %s exists: %v", tableName, err)
			}
			if !exists {
				t.Errorf("VC extension table %s was not created during initialization", tableName)
			}
		}
	})

	t.Run("handle completely empty database", func(t *testing.T) {
		// Create temporary database
		tmpDir := t.TempDir()
		dbPath := filepath.Join(tmpDir, "empty.db")

		// NewVCStorage should initialize all tables from scratch
		store, err := NewVCStorage(ctx, dbPath)
		if err != nil {
			t.Fatalf("Failed to create VC storage on empty database: %v", err)
		}
		defer func() { _ = store.Close() }()

		// Verify all critical tables exist
		criticalTables := []string{
			"issues",
			"vc_agent_events",
			"vc_executor_instances",
			"vc_issue_execution_state",
		}

		for _, tableName := range criticalTables {
			exists, err := tableExists(ctx, store.db, tableName)
			if err != nil {
				t.Fatalf("Failed to check if table %s exists: %v", tableName, err)
			}
			if !exists {
				t.Errorf("Critical table %s does not exist after initialization", tableName)
			}
		}
	})

	t.Run("verify required columns exist in critical tables", func(t *testing.T) {
		// Create temporary database
		tmpDir := t.TempDir()
		dbPath := filepath.Join(tmpDir, "test.db")

		// Initialize VC storage
		store, err := NewVCStorage(ctx, dbPath)
		if err != nil {
			t.Fatalf("Failed to create VC storage: %v", err)
		}
		defer func() { _ = store.Close() }()

		// Verify critical columns exist in vc_agent_events
		agentEventsColumns := []string{
			"id",
			"timestamp",
			"issue_id",
			"executor_id",
			"agent_id",
			"type",
			"severity",
			"message",
			"data",
			"source_line",
		}

		for _, col := range agentEventsColumns {
			exists, err := columnExists(ctx, store.db, "vc_agent_events", col)
			if err != nil {
				t.Fatalf("Failed to check column %s: %v", col, err)
			}
			if !exists {
				t.Errorf("Required column %s missing from vc_agent_events", col)
			}
		}

		// Verify critical columns exist in vc_issue_execution_state
		executionStateColumns := []string{
			"issue_id",
			"executor_instance_id",
			"claimed_at",
			"state",
			"intervention_count",
			"last_intervention_time",
		}

		for _, col := range executionStateColumns {
			exists, err := columnExists(ctx, store.db, "vc_issue_execution_state", col)
			if err != nil {
				t.Fatalf("Failed to check column %s: %v", col, err)
			}
			if !exists {
				t.Errorf("Required column %s missing from vc_issue_execution_state", col)
			}
		}
	})

	t.Run("verify indexes exist for performance", func(t *testing.T) {
		// Create temporary database
		tmpDir := t.TempDir()
		dbPath := filepath.Join(tmpDir, "test.db")

		// Initialize VC storage
		store, err := NewVCStorage(ctx, dbPath)
		if err != nil {
			t.Fatalf("Failed to create VC storage: %v", err)
		}
		defer func() { _ = store.Close() }()

		// Verify critical indexes exist
		criticalIndexes := []string{
			"idx_vc_agent_events_issue",
			"idx_vc_agent_events_executor",
			"idx_vc_agent_events_timestamp",
			"idx_vc_execution_state",
			"idx_vc_execution_executor",
		}

		for _, idxName := range criticalIndexes {
			exists, err := indexExists(ctx, store.db, idxName)
			if err != nil {
				t.Fatalf("Failed to check index %s: %v", idxName, err)
			}
			if !exists {
				t.Errorf("Required index %s does not exist", idxName)
			}
		}
	})
}

// TestDatabaseRecoveryAfterTableCreation tests that the system can recover
// after database tables are created
func TestDatabaseRecoveryAfterTableCreation(t *testing.T) {
	ctx := context.Background()

	t.Run("operations work after table creation", func(t *testing.T) {
		// Create temporary database
		tmpDir := t.TempDir()
		dbPath := filepath.Join(tmpDir, "recovery.db")

		// Create an empty database
		db, err := sql.Open("sqlite3", dbPath)
		if err != nil {
			t.Fatalf("Failed to create database: %v", err)
		}
		db.Close()

		// Open with VCStorage (creates all tables)
		store, err := NewVCStorage(ctx, dbPath)
		if err != nil {
			t.Fatalf("Failed to create VC storage: %v", err)
		}
		defer func() { _ = store.Close() }()

		// Verify we can perform basic operations
		issue := &types.Issue{
			Title:              "Test recovery",
			Status:             types.StatusOpen,
			Priority:           2,
			IssueType:          types.TypeTask,
			AcceptanceCriteria: "Test acceptance criteria",
		}

		err = store.CreateIssue(ctx, issue, "test")
		if err != nil {
			t.Fatalf("Failed to create issue after table creation: %v", err)
		}

		if issue.ID == "" {
			t.Error("Issue ID was not generated")
		}

		// Verify we can retrieve the issue
		retrieved, err := store.GetIssue(ctx, issue.ID)
		if err != nil {
			t.Fatalf("Failed to retrieve issue: %v", err)
		}

		if retrieved.Title != issue.Title {
			t.Errorf("Expected title '%s', got '%s'", issue.Title, retrieved.Title)
		}
	})
}

// TestPartialSchemaHandling tests handling of databases with some tables missing
func TestPartialSchemaHandling(t *testing.T) {
	ctx := context.Background()

	t.Run("missing agent events table is created", func(t *testing.T) {
		// Create temporary database
		tmpDir := t.TempDir()
		dbPath := filepath.Join(tmpDir, "partial.db")

		// Create database with only core Beads tables
		beadsStore, err := beadsLib.NewSQLiteStorage(context.Background(), dbPath)
		if err != nil {
			t.Fatalf("Failed to create Beads storage: %v", err)
		}
		beadsStore.Close()

		// Open with VCStorage - should create missing VC extension tables
		store, err := NewVCStorage(ctx, dbPath)
		if err != nil {
			t.Fatalf("Failed to open VC storage: %v", err)
		}
		defer func() { _ = store.Close() }()

		// Verify vc_agent_events table was created
		exists, err := tableExists(ctx, store.db, "vc_agent_events")
		if err != nil {
			t.Fatalf("Failed to check vc_agent_events table: %v", err)
		}
		if !exists {
			t.Error("vc_agent_events table was not created")
		}

		// Verify we can insert events
		event := &events.AgentEvent{
			IssueID:  "vc-test",
			Type:     "test",
			Severity: events.SeverityInfo,
			Message:  "Test event",
		}

		err = store.StoreAgentEvent(ctx, event)
		if err != nil {
			t.Fatalf("Failed to store event after table creation: %v", err)
		}
	})

	t.Run("missing executor instances table is created", func(t *testing.T) {
		// Create temporary database
		tmpDir := t.TempDir()
		dbPath := filepath.Join(tmpDir, "partial2.db")

		// Create database with only Beads tables
		beadsStore, err := beadsLib.NewSQLiteStorage(context.Background(), dbPath)
		if err != nil {
			t.Fatalf("Failed to create Beads storage: %v", err)
		}
		beadsStore.Close()

		// Open with VCStorage
		store, err := NewVCStorage(ctx, dbPath)
		if err != nil {
			t.Fatalf("Failed to open VC storage: %v", err)
		}
		defer func() { _ = store.Close() }()

		// Verify vc_executor_instances table was created
		exists, err := tableExists(ctx, store.db, "vc_executor_instances")
		if err != nil {
			t.Fatalf("Failed to check vc_executor_instances table: %v", err)
		}
		if !exists {
			t.Error("vc_executor_instances table was not created")
		}
	})
}

// Helper functions

// tableExists checks if a table exists in the database
func tableExists(ctx context.Context, db *sql.DB, tableName string) (bool, error) {
	var count int
	err := db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?
	`, tableName).Scan(&count)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

// columnExists checks if a column exists in a table
func columnExists(ctx context.Context, db *sql.DB, tableName, columnName string) (bool, error) {
	var count int
	err := db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM pragma_table_info(?) WHERE name = ?
	`, tableName, columnName).Scan(&count)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

// indexExists checks if an index exists in the database
func indexExists(ctx context.Context, db *sql.DB, indexName string) (bool, error) {
	var count int
	err := db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM sqlite_master WHERE type='index' AND name=?
	`, indexName).Scan(&count)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

// TestMissionPlansTableSchema validates the vc_mission_plans table structure (vc-680s)
func TestMissionPlansTableSchema(t *testing.T) {
	ctx := context.Background()

	t.Run("mission_plans table has all required columns", func(t *testing.T) {
		// Create temporary database
		tmpDir := t.TempDir()
		dbPath := filepath.Join(tmpDir, "test.db")

		// Initialize VC storage
		store, err := NewVCStorage(ctx, dbPath)
		if err != nil {
			t.Fatalf("Failed to create VC storage: %v", err)
		}
		defer func() { _ = store.Close() }()

		// Verify all required columns exist
		requiredColumns := []string{
			"mission_id",
			"plan_json",
			"iteration",
			"status",
			"created_at",
			"updated_at",
			"approved_at",
		}

		for _, col := range requiredColumns {
			exists, err := columnExists(ctx, store.db, "vc_mission_plans", col)
			if err != nil {
				t.Fatalf("Failed to check column %s: %v", col, err)
			}
			if !exists {
				t.Errorf("Required column %s missing from vc_mission_plans", col)
			}
		}
	})

	t.Run("mission_plans table has required indexes", func(t *testing.T) {
		// Create temporary database
		tmpDir := t.TempDir()
		dbPath := filepath.Join(tmpDir, "test.db")

		// Initialize VC storage
		store, err := NewVCStorage(ctx, dbPath)
		if err != nil {
			t.Fatalf("Failed to create VC storage: %v", err)
		}
		defer func() { _ = store.Close() }()

		// Verify required indexes exist
		requiredIndexes := []string{
			"idx_vc_mission_plans_status",
			"idx_vc_mission_plans_updated",
		}

		for _, idx := range requiredIndexes {
			exists, err := indexExists(ctx, store.db, idx)
			if err != nil {
				t.Fatalf("Failed to check index %s: %v", idx, err)
			}
			if !exists {
				t.Errorf("Required index %s missing from vc_mission_plans", idx)
			}
		}
	})

	t.Run("mission_plans status column has CHECK constraint", func(t *testing.T) {
		// Create temporary database
		tmpDir := t.TempDir()
		dbPath := filepath.Join(tmpDir, "test.db")

		// Initialize VC storage
		store, err := NewVCStorage(ctx, dbPath)
		if err != nil {
			t.Fatalf("Failed to create VC storage: %v", err)
		}
		defer func() { _ = store.Close() }()

		// Try to insert plan with invalid status - should fail
		_, err = store.db.ExecContext(ctx, `
			INSERT INTO vc_mission_plans (mission_id, plan_json, iteration, status, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?)
		`, "test-mission", "{}", 1, "invalid_status", "2024-01-01", "2024-01-01")

		if err == nil {
			t.Error("Expected CHECK constraint violation for invalid status, but insert succeeded")
		}
	})

	t.Run("mission_plans mission_id has unique constraint", func(t *testing.T) {
		// Create temporary database
		tmpDir := t.TempDir()
		dbPath := filepath.Join(tmpDir, "test.db")

		// Initialize VC storage
		store, err := NewVCStorage(ctx, dbPath)
		if err != nil {
			t.Fatalf("Failed to create VC storage: %v", err)
		}
		defer func() { _ = store.Close() }()

		// Create a mission first (required by FK constraint)
		mission := &types.Mission{
			Issue: types.Issue{
				Title:       "Test Mission",
				Description: "Test mission",
				IssueType:   types.TypeEpic,
				Status:      types.StatusOpen,
				Priority:    1,
			},
			Goal: "Test goal",
		}
		if err := store.CreateMission(ctx, mission, "test"); err != nil {
			t.Fatalf("Failed to create mission: %v", err)
		}

		// Insert first plan
		_, err = store.db.ExecContext(ctx, `
			INSERT INTO vc_mission_plans (mission_id, plan_json, iteration, status, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?)
		`, mission.ID, "{}", 1, "draft", "2024-01-01", "2024-01-01")
		if err != nil {
			t.Fatalf("Failed to insert first plan: %v", err)
		}

		// Try to insert duplicate mission_id - should fail
		_, err = store.db.ExecContext(ctx, `
			INSERT INTO vc_mission_plans (mission_id, plan_json, iteration, status, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?)
		`, mission.ID, "{}", 2, "draft", "2024-01-02", "2024-01-02")

		if err == nil {
			t.Error("Expected PRIMARY KEY constraint violation for duplicate mission_id, but insert succeeded")
		}
	})
}
