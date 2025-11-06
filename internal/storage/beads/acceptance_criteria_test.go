package beads

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/steveyegge/vc/internal/types"
)

// TestAcceptanceCriteriaValidation validates that acceptance_criteria is required
// for task and bug issues during creation (vc-ilf1)
func TestAcceptanceCriteriaValidation(t *testing.T) {
	ctx := context.Background()

	// Create temporary database
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	// Create VC storage
	store, err := NewVCStorage(ctx, dbPath)
	if err != nil {
		t.Fatalf("Failed to create VC storage: %v", err)
	}
	defer func() { _ = store.Close() }()

	t.Run("task with empty acceptance_criteria should be rejected", func(t *testing.T) {
		issue := &types.Issue{
			Title:              "Test task",
			Description:        "Task description",
			Status:             types.StatusOpen,
			Priority:           2,
			IssueType:          types.TypeTask,
			AcceptanceCriteria: "", // Empty
		}

		err := store.CreateIssue(ctx, issue, "test")
		if err == nil {
			t.Fatal("Expected error when creating task with empty acceptance_criteria, got nil")
		}

		if !strings.Contains(err.Error(), "acceptance_criteria is required") {
			t.Errorf("Expected error message about acceptance_criteria, got: %v", err)
		}
	})

	t.Run("bug with empty acceptance_criteria should be rejected", func(t *testing.T) {
		issue := &types.Issue{
			Title:              "Test bug",
			Description:        "Bug description",
			Status:             types.StatusOpen,
			Priority:           1,
			IssueType:          types.TypeBug,
			AcceptanceCriteria: "", // Empty
		}

		err := store.CreateIssue(ctx, issue, "test")
		if err == nil {
			t.Fatal("Expected error when creating bug with empty acceptance_criteria, got nil")
		}

		if !strings.Contains(err.Error(), "acceptance_criteria is required") {
			t.Errorf("Expected error message about acceptance_criteria, got: %v", err)
		}
	})

	t.Run("task with whitespace-only acceptance_criteria should be rejected", func(t *testing.T) {
		issue := &types.Issue{
			Title:              "Test task",
			Description:        "Task description",
			Status:             types.StatusOpen,
			Priority:           2,
			IssueType:          types.TypeTask,
			AcceptanceCriteria: "   \n\t  ", // Whitespace only
		}

		err := store.CreateIssue(ctx, issue, "test")
		if err == nil {
			t.Fatal("Expected error when creating task with whitespace-only acceptance_criteria, got nil")
		}

		if !strings.Contains(err.Error(), "acceptance_criteria is required") {
			t.Errorf("Expected error message about acceptance_criteria, got: %v", err)
		}
	})

	t.Run("bug with whitespace-only acceptance_criteria should be rejected", func(t *testing.T) {
		issue := &types.Issue{
			Title:              "Test bug",
			Description:        "Bug description",
			Status:             types.StatusOpen,
			Priority:           1,
			IssueType:          types.TypeBug,
			AcceptanceCriteria: "  \n  ", // Whitespace only
		}

		err := store.CreateIssue(ctx, issue, "test")
		if err == nil {
			t.Fatal("Expected error when creating bug with whitespace-only acceptance_criteria, got nil")
		}

		if !strings.Contains(err.Error(), "acceptance_criteria is required") {
			t.Errorf("Expected error message about acceptance_criteria, got: %v", err)
		}
	})

	t.Run("task with valid acceptance_criteria should succeed", func(t *testing.T) {
		issue := &types.Issue{
			Title:              "Test task with criteria",
			Description:        "Task description",
			Status:             types.StatusOpen,
			Priority:           2,
			IssueType:          types.TypeTask,
			AcceptanceCriteria: "Valid acceptance criteria",
		}

		err := store.CreateIssue(ctx, issue, "test")
		if err != nil {
			t.Fatalf("Expected success when creating task with valid acceptance_criteria, got: %v", err)
		}

		if issue.ID == "" {
			t.Fatal("Issue ID was not generated")
		}

		// Verify it was stored correctly
		retrieved, err := store.GetIssue(ctx, issue.ID)
		if err != nil {
			t.Fatalf("Failed to retrieve issue: %v", err)
		}

		if retrieved.AcceptanceCriteria != "Valid acceptance criteria" {
			t.Errorf("Expected acceptance criteria to be preserved, got: %s", retrieved.AcceptanceCriteria)
		}
	})

	t.Run("bug with valid acceptance_criteria should succeed", func(t *testing.T) {
		issue := &types.Issue{
			Title:              "Test bug with criteria",
			Description:        "Bug description",
			Status:             types.StatusOpen,
			Priority:           1,
			IssueType:          types.TypeBug,
			AcceptanceCriteria: "Bug is fixed when X works",
		}

		err := store.CreateIssue(ctx, issue, "test")
		if err != nil {
			t.Fatalf("Expected success when creating bug with valid acceptance_criteria, got: %v", err)
		}

		if issue.ID == "" {
			t.Fatal("Issue ID was not generated")
		}

		// Verify it was stored correctly
		retrieved, err := store.GetIssue(ctx, issue.ID)
		if err != nil {
			t.Fatalf("Failed to retrieve issue: %v", err)
		}

		if retrieved.AcceptanceCriteria != "Bug is fixed when X works" {
			t.Errorf("Expected acceptance criteria to be preserved, got: %s", retrieved.AcceptanceCriteria)
		}
	})

	t.Run("epic without acceptance_criteria should succeed", func(t *testing.T) {
		// Epics don't require acceptance criteria
		issue := &types.Issue{
			Title:              "Test epic",
			Description:        "Epic description",
			Status:             types.StatusOpen,
			Priority:           0,
			IssueType:          types.TypeEpic,
			AcceptanceCriteria: "", // Empty is OK for epics
		}

		err := store.CreateIssue(ctx, issue, "test")
		if err != nil {
			t.Fatalf("Expected success when creating epic without acceptance_criteria, got: %v", err)
		}

		if issue.ID == "" {
			t.Fatal("Issue ID was not generated")
		}
	})

	t.Run("chore without acceptance_criteria should succeed", func(t *testing.T) {
		// Chores don't require acceptance criteria
		issue := &types.Issue{
			Title:              "Test chore",
			Description:        "Chore description",
			Status:             types.StatusOpen,
			Priority:           2,
			IssueType:          types.TypeChore,
			AcceptanceCriteria: "", // Empty is OK for chores
		}

		err := store.CreateIssue(ctx, issue, "test")
		if err != nil {
			t.Fatalf("Expected success when creating chore without acceptance_criteria, got: %v", err)
		}

		if issue.ID == "" {
			t.Fatal("Issue ID was not generated")
		}
	})
}

// TestUpdateIssuePreservesAcceptanceCriteria verifies that UpdateIssue
// preserves acceptance_criteria when updating other fields (vc-ilf1)
func TestUpdateIssuePreservesAcceptanceCriteria(t *testing.T) {
	ctx := context.Background()

	// Create temporary database
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	// Create VC storage
	store, err := NewVCStorage(ctx, dbPath)
	if err != nil {
		t.Fatalf("Failed to create VC storage: %v", err)
	}
	defer func() { _ = store.Close() }()

	// Create a task with acceptance criteria
	issue := &types.Issue{
		Title:              "Test task",
		Description:        "Task description",
		Status:             types.StatusOpen,
		Priority:           2,
		IssueType:          types.TypeTask,
		AcceptanceCriteria: "Original acceptance criteria",
		Notes:              "Original notes",
	}

	err = store.CreateIssue(ctx, issue, "test")
	if err != nil {
		t.Fatalf("Failed to create issue: %v", err)
	}

	// Update notes (but not acceptance criteria)
	updates := map[string]interface{}{
		"notes": "Updated notes",
	}

	err = store.UpdateIssue(ctx, issue.ID, updates, "test")
	if err != nil {
		t.Fatalf("Failed to update issue: %v", err)
	}

	// Verify acceptance criteria was preserved
	retrieved, err := store.GetIssue(ctx, issue.ID)
	if err != nil {
		t.Fatalf("Failed to retrieve issue: %v", err)
	}

	if retrieved.AcceptanceCriteria != "Original acceptance criteria" {
		t.Errorf("Expected acceptance criteria to be preserved, got: %s", retrieved.AcceptanceCriteria)
	}

	if retrieved.Notes != "Updated notes" {
		t.Errorf("Expected notes to be updated, got: %s", retrieved.Notes)
	}

	// Update acceptance criteria explicitly
	updates = map[string]interface{}{
		"acceptance_criteria": "Updated acceptance criteria",
	}

	err = store.UpdateIssue(ctx, issue.ID, updates, "test")
	if err != nil {
		t.Fatalf("Failed to update acceptance criteria: %v", err)
	}

	// Verify acceptance criteria was updated
	retrieved, err = store.GetIssue(ctx, issue.ID)
	if err != nil {
		t.Fatalf("Failed to retrieve issue: %v", err)
	}

	if retrieved.AcceptanceCriteria != "Updated acceptance criteria" {
		t.Errorf("Expected acceptance criteria to be updated, got: %s", retrieved.AcceptanceCriteria)
	}
}

// TestExecutorRefusesIssueWithoutAcceptanceCriteria validates the vc-hpcl regression
// (vc-kmgv). This test specifically reproduces the scenario where vc-hpcl was created
// with empty acceptance criteria, making it impossible to validate completion.
//
// The test verifies that:
// 1. Tasks/bugs cannot be created without acceptance criteria (validated above)
// 2. If an issue somehow exists without criteria, the executor refuses to claim it
// 3. The error message clearly explains WHY acceptance criteria are needed
func TestExecutorRefusesIssueWithoutAcceptanceCriteria(t *testing.T) {
	ctx := context.Background()

	// Create temporary database
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	// Create VC storage
	store, err := NewVCStorage(ctx, dbPath)
	if err != nil {
		t.Fatalf("Failed to create VC storage: %v", err)
	}
	defer func() { _ = store.Close() }()

	// Simulate the vc-hpcl scenario: Create an issue that somehow got created
	// without acceptance criteria (e.g., migrated from old system, manual DB edit, etc.)
	// We'll use a chore type to bypass creation validation, then update to task
	issue := &types.Issue{
		Title:              "Fix missing database tables",
		Description:        "Some database tables are missing and need to be created",
		Status:             types.StatusOpen,
		Priority:           1,
		IssueType:          types.TypeChore, // Chores don't require acceptance criteria
		AcceptanceCriteria: "",
	}

	err = store.CreateIssue(ctx, issue, "test")
	if err != nil {
		t.Fatalf("Failed to create chore issue: %v", err)
	}

	// Now simulate the issue being changed to a task type without acceptance criteria
	// (This mimics data corruption or migration issues)
	_, err = store.db.ExecContext(ctx, `
		UPDATE issues
		SET issue_type = ?
		WHERE id = ?
	`, string(types.TypeTask), issue.ID)
	if err != nil {
		t.Fatalf("Failed to manually update issue type: %v", err)
	}

	// Verify the issue now has task type but no acceptance criteria
	retrieved, err := store.GetIssue(ctx, issue.ID)
	if err != nil {
		t.Fatalf("Failed to retrieve issue: %v", err)
	}
	if retrieved.IssueType != types.TypeTask {
		t.Fatalf("Expected issue type to be task, got: %v", retrieved.IssueType)
	}
	if strings.TrimSpace(retrieved.AcceptanceCriteria) != "" {
		t.Fatalf("Expected empty acceptance criteria, got: %s", retrieved.AcceptanceCriteria)
	}

	// Register a test executor instance
	executorID := "test-executor-123"
	instance := &types.ExecutorInstance{
		InstanceID:    executorID,
		Hostname:      "test-host",
		PID:           12345,
		Version:       "1.0.0",
		StartedAt:     time.Now(),
		LastHeartbeat: time.Now(),
		Status:        "running",
	}
	err = store.RegisterInstance(ctx, instance)
	if err != nil {
		t.Fatalf("Failed to register executor instance: %v", err)
	}

	// The critical test: Executor should refuse to claim this issue
	err = store.ClaimIssue(ctx, issue.ID, executorID)

	// Verify claim was rejected
	if err == nil {
		t.Fatal("Expected error when executor tries to claim task without acceptance criteria, got nil")
	}

	// Verify error message explains WHY acceptance criteria are needed
	errorMsg := err.Error()
	expectedPhrases := []string{
		"acceptance",
		"criteria",
		"required",
	}

	for _, phrase := range expectedPhrases {
		if !strings.Contains(strings.ToLower(errorMsg), phrase) {
			t.Errorf("Error message should mention '%s', got: %s", phrase, errorMsg)
		}
	}

	// Verify the issue was NOT claimed
	execState, err := store.GetExecutionState(ctx, issue.ID)
	if err != nil {
		t.Fatalf("Failed to get execution state: %v", err)
	}
	if execState != nil && execState.ExecutorInstanceID == executorID {
		t.Error("Issue should not be claimed by executor after validation failure")
	}

	// Verify issue status is still 'open' (not 'in_progress')
	retrieved, err = store.GetIssue(ctx, issue.ID)
	if err != nil {
		t.Fatalf("Failed to retrieve issue after claim attempt: %v", err)
	}
	if retrieved.Status != types.StatusOpen {
		t.Errorf("Issue status should remain 'open' after failed claim, got: %v", retrieved.Status)
	}
}
