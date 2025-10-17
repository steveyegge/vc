package sqlite

import (
	"context"
	"testing"
	"time"

	"github.com/steveyegge/vc/internal/types"
)

func TestExecutionHistory(t *testing.T) {
	ctx := context.Background()
	store := setupTestDB(t)

	// Create a test issue
	issue := &types.Issue{
		ID:          "test-1",
		Title:       "Test Issue",
		Description: "Test Description",
		Status:      types.StatusOpen,
		Priority:    1,
		IssueType:   types.TypeTask,
	}
	if err := store.CreateIssue(ctx, issue, "test-actor"); err != nil {
		t.Fatalf("Failed to create issue: %v", err)
	}

	// Create a test executor instance
	instance := &types.ExecutorInstance{
		InstanceID: "test-executor-1",
		Hostname:   "localhost",
		PID:        12345,
		Status:     types.ExecutorStatusRunning,
		Version:    "1.0.0",
		Metadata:   "{}",
	}
	if err := store.RegisterInstance(ctx, instance); err != nil {
		t.Fatalf("Failed to register instance: %v", err)
	}

	t.Run("RecordNewAttempt", func(t *testing.T) {
		attempt := &types.ExecutionAttempt{
			IssueID:            "test-1",
			ExecutorInstanceID: "test-executor-1",
			Summary:            "First attempt",
		}

		err := store.RecordExecutionAttempt(ctx, attempt)
		if err != nil {
			t.Fatalf("Failed to record attempt: %v", err)
		}

		// Should have auto-assigned attempt number
		if attempt.AttemptNumber != 1 {
			t.Errorf("Expected attempt number 1, got %d", attempt.AttemptNumber)
		}

		// Should have populated ID
		if attempt.ID == 0 {
			t.Error("Expected ID to be populated")
		}

		// Should have set started_at
		if attempt.StartedAt.IsZero() {
			t.Error("Expected StartedAt to be set")
		}
	})

	t.Run("GetExecutionHistory", func(t *testing.T) {
		attempts, err := store.GetExecutionHistory(ctx, "test-1")
		if err != nil {
			t.Fatalf("Failed to get history: %v", err)
		}

		if len(attempts) != 1 {
			t.Fatalf("Expected 1 attempt, got %d", len(attempts))
		}

		attempt := attempts[0]
		if attempt.IssueID != "test-1" {
			t.Errorf("Expected issue_id test-1, got %s", attempt.IssueID)
		}
		if attempt.AttemptNumber != 1 {
			t.Errorf("Expected attempt number 1, got %d", attempt.AttemptNumber)
		}
		if attempt.Summary != "First attempt" {
			t.Errorf("Expected summary 'First attempt', got %s", attempt.Summary)
		}
	})

	t.Run("UpdateAttempt", func(t *testing.T) {
		// Get the first attempt
		attempts, err := store.GetExecutionHistory(ctx, "test-1")
		if err != nil {
			t.Fatalf("Failed to get history: %v", err)
		}
		attempt := attempts[0]

		// Update it
		now := time.Now()
		success := true
		exitCode := 0
		attempt.CompletedAt = &now
		attempt.Success = &success
		attempt.ExitCode = &exitCode
		attempt.Summary = "Completed successfully"
		attempt.OutputSample = "Sample output"
		attempt.ErrorSample = "No errors"

		err = store.RecordExecutionAttempt(ctx, attempt)
		if err != nil {
			t.Fatalf("Failed to update attempt: %v", err)
		}

		// Verify update
		attempts, err = store.GetExecutionHistory(ctx, "test-1")
		if err != nil {
			t.Fatalf("Failed to get history: %v", err)
		}
		updated := attempts[0]

		if updated.CompletedAt == nil {
			t.Error("Expected CompletedAt to be set")
		}
		if updated.Success == nil || !*updated.Success {
			t.Error("Expected Success to be true")
		}
		if updated.ExitCode == nil || *updated.ExitCode != 0 {
			t.Error("Expected ExitCode to be 0")
		}
		if updated.Summary != "Completed successfully" {
			t.Errorf("Expected updated summary, got %s", updated.Summary)
		}
	})

	t.Run("MultipleAttempts", func(t *testing.T) {
		// Add a second attempt
		attempt2 := &types.ExecutionAttempt{
			IssueID:            "test-1",
			ExecutorInstanceID: "test-executor-1",
			Summary:            "Second attempt",
		}
		if err := store.RecordExecutionAttempt(ctx, attempt2); err != nil {
			t.Fatalf("Failed to record second attempt: %v", err)
		}

		// Add a third attempt
		attempt3 := &types.ExecutionAttempt{
			IssueID:            "test-1",
			ExecutorInstanceID: "test-executor-1",
			Summary:            "Third attempt",
		}
		if err := store.RecordExecutionAttempt(ctx, attempt3); err != nil {
			t.Fatalf("Failed to record third attempt: %v", err)
		}

		// Get all attempts
		attempts, err := store.GetExecutionHistory(ctx, "test-1")
		if err != nil {
			t.Fatalf("Failed to get history: %v", err)
		}

		if len(attempts) != 3 {
			t.Fatalf("Expected 3 attempts, got %d", len(attempts))
		}

		// Verify attempt numbers are sequential
		for i, attempt := range attempts {
			expectedNumber := i + 1
			if attempt.AttemptNumber != expectedNumber {
				t.Errorf("Expected attempt %d to have number %d, got %d",
					i, expectedNumber, attempt.AttemptNumber)
			}
		}

		// Verify chronological order
		if !attempts[0].StartedAt.Before(attempts[1].StartedAt) {
			t.Error("Expected attempts to be ordered chronologically")
		}
	})

	t.Run("EmptyHistory", func(t *testing.T) {
		attempts, err := store.GetExecutionHistory(ctx, "nonexistent")
		if err != nil {
			t.Fatalf("Failed to get history: %v", err)
		}

		if len(attempts) != 0 {
			t.Errorf("Expected 0 attempts for nonexistent issue, got %d", len(attempts))
		}
	})

	t.Run("ValidationErrors", func(t *testing.T) {
		// Missing issue_id
		attempt := &types.ExecutionAttempt{
			ExecutorInstanceID: "test-executor-1",
		}
		err := store.RecordExecutionAttempt(ctx, attempt)
		if err == nil {
			t.Error("Expected validation error for missing issue_id")
		}

		// Missing executor_instance_id
		attempt = &types.ExecutionAttempt{
			IssueID: "test-1",
		}
		err = store.RecordExecutionAttempt(ctx, attempt)
		if err == nil {
			t.Error("Expected validation error for missing executor_instance_id")
		}
	})

	t.Run("TruncatedOutput", func(t *testing.T) {
		// Create an attempt with large output samples
		largeOutput := ""
		for i := 0; i < 2000; i++ {
			largeOutput += "Line " + string(rune(i)) + "\n"
		}

		attempt := &types.ExecutionAttempt{
			IssueID:            "test-1",
			ExecutorInstanceID: "test-executor-1",
			Summary:            "Attempt with large output",
			OutputSample:       largeOutput,
			ErrorSample:        largeOutput,
		}

		err := store.RecordExecutionAttempt(ctx, attempt)
		if err != nil {
			t.Fatalf("Failed to record attempt: %v", err)
		}

		// Verify it was stored (truncation would happen at application level)
		attempts, err := store.GetExecutionHistory(ctx, "test-1")
		if err != nil {
			t.Fatalf("Failed to get history: %v", err)
		}

		// Find the attempt we just created
		var found *types.ExecutionAttempt
		for _, a := range attempts {
			if a.Summary == "Attempt with large output" {
				found = a
				break
			}
		}

		if found == nil {
			t.Fatal("Could not find attempt with large output")
		}

		// Output should be stored (even if large)
		if len(found.OutputSample) == 0 {
			t.Error("Expected output sample to be stored")
		}
	})
}

func TestExecutionAttemptNumbering(t *testing.T) {
	ctx := context.Background()
	store := setupTestDB(t)

	// Create test issue and executor
	issue := &types.Issue{
		ID:          "test-numbering",
		Title:       "Test Numbering",
		Description: "Test",
		Status:      types.StatusOpen,
		Priority:    1,
		IssueType:   types.TypeTask,
	}
	if err := store.CreateIssue(ctx, issue, "test-actor"); err != nil {
		t.Fatalf("Failed to create issue: %v", err)
	}

	instance := &types.ExecutorInstance{
		InstanceID: "test-executor-numbering",
		Hostname:   "localhost",
		PID:        99999,
		Status:     types.ExecutorStatusRunning,
		Version:    "1.0.0",
		Metadata:   "{}",
	}
	if err := store.RegisterInstance(ctx, instance); err != nil {
		t.Fatalf("Failed to register instance: %v", err)
	}

	t.Run("AutoIncrementAttemptNumber", func(t *testing.T) {
		// Create 5 attempts without specifying attempt_number
		for i := 1; i <= 5; i++ {
			attempt := &types.ExecutionAttempt{
				IssueID:            "test-numbering",
				ExecutorInstanceID: "test-executor-numbering",
				Summary:            "Auto attempt",
				// AttemptNumber not set - should auto-increment
			}

			err := store.RecordExecutionAttempt(ctx, attempt)
			if err != nil {
				t.Fatalf("Failed to record attempt %d: %v", i, err)
			}

			if attempt.AttemptNumber != i {
				t.Errorf("Expected auto-incremented number %d, got %d", i, attempt.AttemptNumber)
			}
		}

		// Verify all attempts
		attempts, err := store.GetExecutionHistory(ctx, "test-numbering")
		if err != nil {
			t.Fatalf("Failed to get history: %v", err)
		}

		if len(attempts) != 5 {
			t.Fatalf("Expected 5 attempts, got %d", len(attempts))
		}

		for i, attempt := range attempts {
			expected := i + 1
			if attempt.AttemptNumber != expected {
				t.Errorf("Attempt %d: expected number %d, got %d",
					i, expected, attempt.AttemptNumber)
			}
		}
	})
}
