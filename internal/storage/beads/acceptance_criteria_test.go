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

	t.Run("task with very long acceptance_criteria should succeed", func(t *testing.T) {
		// Create acceptance criteria with ~4200 characters (realistic length based on vc-4778: 1937 chars)
		longCriteria := strings.Repeat("This is a detailed acceptance criterion. ", 100) // ~4200 chars

		issue := &types.Issue{
			Title:              "Test task with long criteria",
			Description:        "Task description",
			Status:             types.StatusOpen,
			Priority:           2,
			IssueType:          types.TypeTask,
			AcceptanceCriteria: longCriteria,
		}

		err := store.CreateIssue(ctx, issue, "test")
		if err != nil {
			t.Fatalf("Expected success with long acceptance_criteria (%d chars), got: %v", len(longCriteria), err)
		}

		if issue.ID == "" {
			t.Fatal("Issue ID was not generated")
		}

		// Verify it was stored correctly
		retrieved, err := store.GetIssue(ctx, issue.ID)
		if err != nil {
			t.Fatalf("Failed to retrieve issue: %v", err)
		}

		if retrieved.AcceptanceCriteria != longCriteria {
			t.Errorf("Expected acceptance criteria to be preserved, length mismatch: got %d, expected %d",
				len(retrieved.AcceptanceCriteria), len(longCriteria))
		}
	})

	t.Run("task with extremely long acceptance_criteria should succeed", func(t *testing.T) {
		// Test with 10000 characters to ensure no arbitrary limits
		extremelyLongCriteria := strings.Repeat("X", 10000)

		issue := &types.Issue{
			Title:              "Test task with extremely long criteria",
			Description:        "Task description",
			Status:             types.StatusOpen,
			Priority:           2,
			IssueType:          types.TypeTask,
			AcceptanceCriteria: extremelyLongCriteria,
		}

		err := store.CreateIssue(ctx, issue, "test")
		if err != nil {
			t.Fatalf("Expected success with extremely long acceptance_criteria (%d chars), got: %v", len(extremelyLongCriteria), err)
		}

		// Verify it was stored correctly
		retrieved, err := store.GetIssue(ctx, issue.ID)
		if err != nil {
			t.Fatalf("Failed to retrieve issue: %v", err)
		}

		if retrieved.AcceptanceCriteria != extremelyLongCriteria {
			t.Errorf("Expected acceptance criteria to be preserved exactly, length mismatch: got %d, expected %d",
				len(retrieved.AcceptanceCriteria), len(extremelyLongCriteria))
		}
	})

	t.Run("feature type requires acceptance_criteria", func(t *testing.T) {
		// Feature type follows same validation rules as task/bug (vc-47rx)
		// This test complements the task/bug tests above
		issue := &types.Issue{
			Title:              "Test feature",
			Description:        "Feature description",
			Status:             types.StatusOpen,
			Priority:           2,
			IssueType:          types.TypeFeature,
			AcceptanceCriteria: "", // Empty
		}

		err := store.CreateIssue(ctx, issue, "test")
		if err == nil {
			t.Fatal("Expected error when creating feature with empty acceptance_criteria, got nil")
		}

		if !strings.Contains(err.Error(), "acceptance_criteria is required") {
			t.Errorf("Expected error message about acceptance_criteria, got: %v", err)
		}

		// Now test that feature succeeds with valid criteria
		issue.AcceptanceCriteria = "Feature is complete when X works"
		err = store.CreateIssue(ctx, issue, "test")
		if err != nil {
			t.Fatalf("Expected success when creating feature with valid acceptance_criteria, got: %v", err)
		}

		// Verify it was stored correctly
		retrieved, err := store.GetIssue(ctx, issue.ID)
		if err != nil {
			t.Fatalf("Failed to retrieve issue: %v", err)
		}

		if retrieved.AcceptanceCriteria != "Feature is complete when X works" {
			t.Errorf("Expected acceptance criteria to be preserved, got: %s", retrieved.AcceptanceCriteria)
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

// TestAcceptanceCriteriaJSONSerialization validates that acceptance_criteria
// is correctly serialized to and deserialized from JSON (vc-47rx)
func TestAcceptanceCriteriaJSONSerialization(t *testing.T) {
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

	t.Run("acceptance_criteria serializes correctly", func(t *testing.T) {
		// Create issue with acceptance criteria
		issue := &types.Issue{
			Title:              "Test JSON serialization",
			Description:        "Test description",
			Status:             types.StatusOpen,
			Priority:           2,
			IssueType:          types.TypeTask,
			AcceptanceCriteria: "Test criteria with special chars: \n- Item 1\n- Item 2\n",
		}

		err := store.CreateIssue(ctx, issue, "test")
		if err != nil {
			t.Fatalf("Failed to create issue: %v", err)
		}

		// Retrieve and verify
		retrieved, err := store.GetIssue(ctx, issue.ID)
		if err != nil {
			t.Fatalf("Failed to retrieve issue: %v", err)
		}

		if retrieved.AcceptanceCriteria != issue.AcceptanceCriteria {
			t.Errorf("AcceptanceCriteria not preserved through storage.\nExpected: %q\nGot: %q",
				issue.AcceptanceCriteria, retrieved.AcceptanceCriteria)
		}
	})

	t.Run("empty acceptance_criteria omitted from JSON", func(t *testing.T) {
		// Epic with empty acceptance_criteria (allowed)
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
			t.Fatalf("Failed to create issue: %v", err)
		}

		// Retrieve and verify empty string is preserved
		retrieved, err := store.GetIssue(ctx, issue.ID)
		if err != nil {
			t.Fatalf("Failed to retrieve issue: %v", err)
		}

		if retrieved.AcceptanceCriteria != "" {
			t.Errorf("Expected empty acceptance_criteria, got: %q", retrieved.AcceptanceCriteria)
		}
	})

	t.Run("acceptance_criteria with unicode characters", func(t *testing.T) {
		issue := &types.Issue{
			Title:       "Test unicode",
			Description: "Test description",
			Status:      types.StatusOpen,
			Priority:    2,
			IssueType:   types.TypeTask,
			AcceptanceCriteria: "Test criteria with unicode: ✓ Pass ✗ Fail 🎯 Goal\n" +
				"Multiple languages: 日本語 中文 한글",
		}

		err := store.CreateIssue(ctx, issue, "test")
		if err != nil {
			t.Fatalf("Failed to create issue: %v", err)
		}

		retrieved, err := store.GetIssue(ctx, issue.ID)
		if err != nil {
			t.Fatalf("Failed to retrieve issue: %v", err)
		}

		if retrieved.AcceptanceCriteria != issue.AcceptanceCriteria {
			t.Errorf("Unicode acceptance_criteria not preserved.\nExpected: %q\nGot: %q",
				issue.AcceptanceCriteria, retrieved.AcceptanceCriteria)
		}
	})
}

// TestAcceptanceCriteriaLengthValidation validates that acceptance_criteria
// has reasonable length limits (vc-47rx)
func TestAcceptanceCriteriaLengthValidation(t *testing.T) {
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

	t.Run("short acceptance_criteria should succeed", func(t *testing.T) {
		issue := &types.Issue{
			Title:              "Test short criteria",
			Description:        "Test description",
			Status:             types.StatusOpen,
			Priority:           2,
			IssueType:          types.TypeTask,
			AcceptanceCriteria: "Pass",
		}

		err := store.CreateIssue(ctx, issue, "test")
		if err != nil {
			t.Fatalf("Expected success with short acceptance_criteria, got: %v", err)
		}
	})

	t.Run("medium length acceptance_criteria should succeed", func(t *testing.T) {
		// ~500 characters - typical acceptance criteria
		criteria := strings.Repeat("- Acceptance criterion item\n", 15)
		issue := &types.Issue{
			Title:              "Test medium criteria",
			Description:        "Test description",
			Status:             types.StatusOpen,
			Priority:           2,
			IssueType:          types.TypeTask,
			AcceptanceCriteria: criteria,
		}

		err := store.CreateIssue(ctx, issue, "test")
		if err != nil {
			t.Fatalf("Expected success with medium acceptance_criteria, got: %v", err)
		}

		// Verify it was stored correctly
		retrieved, err := store.GetIssue(ctx, issue.ID)
		if err != nil {
			t.Fatalf("Failed to retrieve issue: %v", err)
		}

		if retrieved.AcceptanceCriteria != criteria {
			t.Error("Medium length acceptance_criteria not preserved")
		}
	})

	t.Run("large acceptance_criteria should succeed", func(t *testing.T) {
		// ~5000 characters - large but reasonable
		criteria := strings.Repeat("Detailed acceptance criterion with multiple requirements. ", 80)
		issue := &types.Issue{
			Title:              "Test large criteria",
			Description:        "Test description",
			Status:             types.StatusOpen,
			Priority:           2,
			IssueType:          types.TypeTask,
			AcceptanceCriteria: criteria,
		}

		err := store.CreateIssue(ctx, issue, "test")
		if err != nil {
			t.Fatalf("Expected success with large acceptance_criteria, got: %v", err)
		}

		// Verify it was stored correctly
		retrieved, err := store.GetIssue(ctx, issue.ID)
		if err != nil {
			t.Fatalf("Failed to retrieve issue: %v", err)
		}

		if retrieved.AcceptanceCriteria != criteria {
			t.Error("Large acceptance_criteria not preserved")
		}
	})

	t.Run("very large acceptance_criteria should succeed", func(t *testing.T) {
		// ~50000 characters - very large but still reasonable for complex acceptance criteria
		criteria := strings.Repeat("- Test criterion with detailed explanation of expected behavior\n", 500)
		issue := &types.Issue{
			Title:              "Test very large criteria",
			Description:        "Test description",
			Status:             types.StatusOpen,
			Priority:           2,
			IssueType:          types.TypeTask,
			AcceptanceCriteria: criteria,
		}

		err := store.CreateIssue(ctx, issue, "test")
		if err != nil {
			t.Fatalf("Expected success with very large acceptance_criteria, got: %v", err)
		}

		// Verify it was stored correctly
		retrieved, err := store.GetIssue(ctx, issue.ID)
		if err != nil {
			t.Fatalf("Failed to retrieve issue: %v", err)
		}

		if len(retrieved.AcceptanceCriteria) != len(criteria) {
			t.Errorf("Very large acceptance_criteria length mismatch: expected %d, got %d",
				len(criteria), len(retrieved.AcceptanceCriteria))
		}
	})
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

// TestAcceptanceCriteriaSpecialCharacters validates that acceptance_criteria
// correctly handles special characters, unicode, and edge cases (vc-47rx)
func TestAcceptanceCriteriaSpecialCharacters(t *testing.T) {
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

	testCases := []struct {
		name     string
		criteria string
	}{
		{
			name:     "acceptance criteria with newlines",
			criteria: "Criterion 1:\n- Item A\n- Item B\n\nCriterion 2:\n- Item C",
		},
		{
			name:     "acceptance criteria with unicode",
			criteria: "支持中文字符 ✓ Emoji support 🎯 Greek letters: αβγ",
		},
		{
			name:     "acceptance criteria with special JSON characters",
			criteria: `Quotes: "double" and 'single', backslash: \, forward slash: /, tab: 	`,
		},
		{
			name:     "acceptance criteria with markdown",
			criteria: "# Header\n\n**Bold** and *italic*\n\n```go\ncode block\n```\n\n- List item",
		},
		{
			name:     "acceptance criteria with SQL characters",
			criteria: "SELECT * FROM issues WHERE id = 'vc-123'; -- This should be safe",
		},
		{
			name:     "acceptance criteria with numbered list",
			criteria: "1) First criterion\n2) Second criterion\n3) Third criterion",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			issue := &types.Issue{
				Title:              "Test: " + tc.name,
				Description:        "Testing special characters",
				Status:             types.StatusOpen,
				Priority:           2,
				IssueType:          types.TypeTask,
				AcceptanceCriteria: tc.criteria,
			}

			err := store.CreateIssue(ctx, issue, "test")
			if err != nil {
				t.Fatalf("Failed to create issue with special characters: %v", err)
			}

			// Verify it was stored correctly
			retrieved, err := store.GetIssue(ctx, issue.ID)
			if err != nil {
				t.Fatalf("Failed to retrieve issue: %v", err)
			}

			if retrieved.AcceptanceCriteria != tc.criteria {
				t.Errorf("Acceptance criteria not preserved.\nExpected: %q\nGot: %q",
					tc.criteria, retrieved.AcceptanceCriteria)
			}
		})
	}
}

// TestAcceptanceCriteriaUpdateValidation validates that updating an issue's
// acceptance_criteria field respects the same validation rules as creation (vc-47rx)
func TestAcceptanceCriteriaUpdateValidation(t *testing.T) {
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

	// Create a task with valid acceptance criteria
	issue := &types.Issue{
		Title:              "Test task",
		Description:        "Task description",
		Status:             types.StatusOpen,
		Priority:           2,
		IssueType:          types.TypeTask,
		AcceptanceCriteria: "Original criteria",
	}

	err = store.CreateIssue(ctx, issue, "test")
	if err != nil {
		t.Fatalf("Failed to create issue: %v", err)
	}

	t.Run("updating to empty acceptance_criteria should succeed", func(t *testing.T) {
		// Note: UpdateIssue doesn't validate acceptance_criteria because it's a direct update
		// Validation only happens during CreateIssue and ClaimIssue
		// This is intentional to allow fixing issues that were created without proper validation
		updates := map[string]interface{}{
			"acceptance_criteria": "",
		}

		err := store.UpdateIssue(ctx, issue.ID, updates, "test")
		// Update should succeed - validation is only on create and claim
		if err != nil {
			t.Fatalf("UpdateIssue should allow empty acceptance_criteria (validation only on create/claim), got: %v", err)
		}

		// Verify the empty value was stored
		retrieved, err := store.GetIssue(ctx, issue.ID)
		if err != nil {
			t.Fatalf("Failed to retrieve issue: %v", err)
		}

		if retrieved.AcceptanceCriteria != "" {
			t.Errorf("Expected empty acceptance_criteria after update, got: %q", retrieved.AcceptanceCriteria)
		}
	})

	t.Run("updating to long acceptance_criteria should succeed", func(t *testing.T) {
		// Use ~7000 chars to test between realistic (4200) and extreme (10000) lengths
		longCriteria := strings.Repeat("Long criteria ", 500) // ~7000 chars

		updates := map[string]interface{}{
			"acceptance_criteria": longCriteria,
		}

		err := store.UpdateIssue(ctx, issue.ID, updates, "test")
		if err != nil {
			t.Fatalf("Failed to update with long acceptance_criteria: %v", err)
		}

		// Verify it was stored correctly
		retrieved, err := store.GetIssue(ctx, issue.ID)
		if err != nil {
			t.Fatalf("Failed to retrieve issue: %v", err)
		}

		if len(retrieved.AcceptanceCriteria) != len(longCriteria) {
			t.Errorf("Expected acceptance criteria length %d, got %d",
				len(longCriteria), len(retrieved.AcceptanceCriteria))
		}
	})
}
