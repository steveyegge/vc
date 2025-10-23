package beads

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/steveyegge/vc/internal/types"
)

// TestBeadsIntegration validates that VC storage wraps Beads correctly
func TestBeadsIntegration(t *testing.T) {
	ctx := context.Background()

	// Create temporary database
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	// Create VC storage (wraps Beads)
	store, err := NewVCStorage(ctx, dbPath)
	if err != nil {
		t.Fatalf("Failed to create VC storage: %v", err)
	}
	defer store.Close()

	// Verify database file was created
	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		t.Fatalf("Database file was not created: %s", dbPath)
	}

	t.Run("create regular issue", func(t *testing.T) {
		issue := &types.Issue{
			Title:       "Test issue",
			Description: "Test description",
			Status:      types.StatusOpen,
			Priority:    2,
			IssueType:   types.TypeTask,
		}

		err := store.CreateIssue(ctx, issue, "test")
		if err != nil {
			t.Fatalf("Failed to create issue: %v", err)
		}

		if issue.ID == "" {
			t.Fatal("Issue ID was not generated")
		}

		t.Logf("Created issue: %s", issue.ID)
	})

	t.Run("create mission issue", func(t *testing.T) {
		mission := &types.Issue{
			Title:        "Test mission",
			Description:  "Mission description",
			Status:       types.StatusOpen,
			Priority:     0,
			IssueType:    types.TypeEpic,
			IssueSubtype: types.SubtypeMission,
		}

		err := store.CreateIssue(ctx, mission, "test")
		if err != nil {
			t.Fatalf("Failed to create mission: %v", err)
		}

		if mission.ID == "" {
			t.Fatal("Mission ID was not generated")
		}

		// Verify mission was created in extension table
		retrieved, err := store.GetIssue(ctx, mission.ID)
		if err != nil {
			t.Fatalf("Failed to retrieve mission: %v", err)
		}

		if retrieved.IssueSubtype != types.SubtypeMission {
			t.Errorf("Expected subtype 'mission', got '%s'", retrieved.IssueSubtype)
		}

		t.Logf("Created mission: %s with subtype %s", mission.ID, retrieved.IssueSubtype)
	})

	t.Run("add and retrieve labels", func(t *testing.T) {
		issue := &types.Issue{
			Title:      "Labeled issue",
			Status:     types.StatusOpen,
			Priority:   2,
			IssueType:  types.TypeTask,
		}

		err := store.CreateIssue(ctx, issue, "test")
		if err != nil {
			t.Fatalf("Failed to create issue: %v", err)
		}

		// Add labels (via Beads)
		err = store.AddLabel(ctx, issue.ID, "mission", "test")
		if err != nil {
			t.Fatalf("Failed to add label: %v", err)
		}

		err = store.AddLabel(ctx, issue.ID, "sandbox:mission-100", "test")
		if err != nil {
			t.Fatalf("Failed to add sandbox label: %v", err)
		}

		// Retrieve labels
		labels, err := store.GetLabels(ctx, issue.ID)
		if err != nil {
			t.Fatalf("Failed to get labels: %v", err)
		}

		if len(labels) != 2 {
			t.Errorf("Expected 2 labels, got %d", len(labels))
		}

		t.Logf("Issue %s has labels: %v", issue.ID, labels)
	})

	t.Run("get ready work", func(t *testing.T) {
		// Create a task
		task := &types.Issue{
			Title:      "Ready task",
			Status:     types.StatusOpen,
			Priority:   1,
			IssueType:  types.TypeTask,
		}

		err := store.CreateIssue(ctx, task, "test")
		if err != nil {
			t.Fatalf("Failed to create task: %v", err)
		}

		// Query ready work
		ready, err := store.GetReadyWork(ctx, types.WorkFilter{
			Status: types.StatusOpen,
			Limit:  10,
		})
		if err != nil {
			t.Fatalf("Failed to get ready work: %v", err)
		}

		if len(ready) == 0 {
			t.Error("Expected at least one ready issue")
		}

		t.Logf("Found %d ready issues", len(ready))
	})

	t.Run("executor instance registration", func(t *testing.T) {
		instance := &types.ExecutorInstance{
			InstanceID:    "test-executor-1",
			Hostname:      "localhost",
			PID:           12345,
			Version:       "0.1.0",
			StartedAt:     time.Now(),
			LastHeartbeat: time.Now(),
			Status:        "running",
		}

		err := store.RegisterInstance(ctx, instance)
		if err != nil {
			t.Fatalf("Failed to register instance: %v", err)
		}

		// Retrieve active instances
		instances, err := store.GetActiveInstances(ctx)
		if err != nil {
			t.Fatalf("Failed to get active instances: %v", err)
		}

		if len(instances) != 1 {
			t.Errorf("Expected 1 active instance, got %d", len(instances))
		}

		if instances[0].InstanceID != "test-executor-1" {
			t.Errorf("Expected instance ID 'test-executor-1', got '%s'", instances[0].InstanceID)
		}

		t.Logf("Registered executor instance: %s", instance.InstanceID)
	})

	t.Run("claim and release issue", func(t *testing.T) {
		// Create issue
		issue := &types.Issue{
			Title:      "Claimable task",
			Status:     types.StatusOpen,
			Priority:   2,
			IssueType:  types.TypeTask,
		}

		err := store.CreateIssue(ctx, issue, "test")
		if err != nil {
			t.Fatalf("Failed to create issue: %v", err)
		}

		// Claim issue
		err = store.ClaimIssue(ctx, issue.ID, "test-executor-1")
		if err != nil {
			t.Fatalf("Failed to claim issue: %v", err)
		}

		// Verify execution state
		state, err := store.GetExecutionState(ctx, issue.ID)
		if err != nil {
			t.Fatalf("Failed to get execution state: %v", err)
		}

		if state.State != "claimed" {
			t.Errorf("Expected state 'claimed', got '%s'", state.State)
		}

		// Release issue
		err = store.ReleaseIssue(ctx, issue.ID)
		if err != nil {
			t.Fatalf("Failed to release issue: %v", err)
		}

		t.Logf("Successfully claimed and released issue: %s", issue.ID)
	})
}

// TestBeadsExtensionTablesCreated verifies extension tables are created
func TestBeadsExtensionTablesCreated(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	store, err := NewVCStorage(ctx, dbPath)
	if err != nil {
		t.Fatalf("Failed to create VC storage: %v", err)
	}
	defer store.Close()

	// Query each extension table to verify it exists
	tables := []string{
		"vc_mission_state",
		"vc_agent_events",
		"vc_executor_instances",
		"vc_issue_execution_state",
		"vc_execution_history",
	}

	for _, table := range tables {
		var count int
		err := store.db.QueryRowContext(ctx,
			"SELECT COUNT(*) FROM "+table,
		).Scan(&count)

		if err != nil {
			t.Errorf("Extension table '%s' does not exist or is not accessible: %v", table, err)
		} else {
			t.Logf("✓ Extension table '%s' exists (count=%d)", table, count)
		}
	}
}

// TestBeadsCoreTables verifies that Beads core tables exist
func TestBeadsCoreTables(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	store, err := NewVCStorage(ctx, dbPath)
	if err != nil {
		t.Fatalf("Failed to create VC storage: %v", err)
	}
	defer store.Close()

	// Query Beads core tables
	beadsTables := []string{
		"issues",
		"dependencies",
		"labels",
		"comments",
		"events",
	}

	for _, table := range beadsTables {
		var count int
		err := store.db.QueryRowContext(ctx,
			"SELECT COUNT(*) FROM "+table,
		).Scan(&count)

		if err != nil {
			t.Errorf("Beads core table '%s' does not exist: %v", table, err)
		} else {
			t.Logf("✓ Beads table '%s' exists (count=%d)", table, count)
		}
	}
}
