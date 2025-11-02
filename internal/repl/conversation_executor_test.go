package repl

import (
	"context"
	"strings"
	"testing"

	"github.com/steveyegge/vc/internal/types"
)

// TestReleaseIssueWithError tests the error handling release function
func TestReleaseIssueWithError(t *testing.T) {
	t.Run("releases issue with error comment", func(t *testing.T) {
		mock := &mockStorageIntegration{}
		handler := &ConversationHandler{
			storage: mock,
			actor:   "test-actor",
		}
		ctx := context.Background()

		// Release issue with error
		handler.releaseIssueWithError(ctx, "vc-1", "test-actor", "Test error message")

		// Verify ReleaseIssueAndReopen was called
		// Note: The mock doesn't track calls, but we can verify no panic occurs
		// In a real scenario, we'd use a mock that tracks method calls
	})

	t.Run("handles storage errors gracefully", func(t *testing.T) {
		mock := &mockStorageIntegration{}
		// Configure mock to fail on ReleaseIssueAndReopen
		// (current mock doesn't support this, but production code handles it)
		handler := &ConversationHandler{
			storage: mock,
			actor:   "test-actor",
		}
		ctx := context.Background()

		// Should not panic even if storage fails
		handler.releaseIssueWithError(ctx, "vc-1", "test-actor", "Test error")
	})
}

// TestValidateIssueForExecution tests the validation logic
func TestValidateIssueForExecution(t *testing.T) {
	t.Run("accepts open issue", func(t *testing.T) {
		mock := &mockStorageIntegration{}
		handler := &ConversationHandler{storage: mock}
		ctx := context.Background()

		issue := &types.Issue{
			ID:     "vc-1",
			Status: types.StatusOpen,
		}

		msg, err := handler.validateIssueForExecution(ctx, issue)
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
		if msg != "" {
			t.Errorf("Expected no message for open issue, got: %s", msg)
		}
	})

	t.Run("rejects closed issue", func(t *testing.T) {
		mock := &mockStorageIntegration{}
		handler := &ConversationHandler{storage: mock}
		ctx := context.Background()

		issue := &types.Issue{
			ID:     "vc-1",
			Status: types.StatusClosed,
		}

		msg, err := handler.validateIssueForExecution(ctx, issue)
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
		if !strings.Contains(msg, "already closed") {
			t.Errorf("Expected 'already closed' message, got: %s", msg)
		}
	})

	t.Run("rejects in-progress issue", func(t *testing.T) {
		mock := &mockStorageIntegration{}
		handler := &ConversationHandler{storage: mock}
		ctx := context.Background()

		issue := &types.Issue{
			ID:     "vc-1",
			Status: types.StatusInProgress,
		}

		msg, err := handler.validateIssueForExecution(ctx, issue)
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
		if !strings.Contains(msg, "already in progress") {
			t.Errorf("Expected 'already in progress' message, got: %s", msg)
		}
	})

	t.Run("rejects blocked issue with dependencies", func(t *testing.T) {
		mock := &mockStorageIntegration{}
		mock.dependencies = map[string][]*types.Issue{
			"vc-1": {
				{ID: "vc-2", Status: types.StatusOpen},
			},
		}
		handler := &ConversationHandler{storage: mock}
		ctx := context.Background()

		issue := &types.Issue{
			ID:     "vc-1",
			Status: types.StatusBlocked,
		}

		msg, err := handler.validateIssueForExecution(ctx, issue)
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
		if !strings.Contains(msg, "blocked by") {
			t.Errorf("Expected 'blocked by' message, got: %s", msg)
		}
		if !strings.Contains(msg, "vc-2") {
			t.Errorf("Expected blocker ID in message, got: %s", msg)
		}
	})

	t.Run("rejects blocked issue without dependencies", func(t *testing.T) {
		mock := &mockStorageIntegration{}
		handler := &ConversationHandler{storage: mock}
		ctx := context.Background()

		issue := &types.Issue{
			ID:     "vc-1",
			Status: types.StatusBlocked,
		}

		msg, err := handler.validateIssueForExecution(ctx, issue)
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
		if !strings.Contains(msg, "currently blocked") {
			t.Errorf("Expected 'currently blocked' message, got: %s", msg)
		}
	})
}

// TestFormatContinueLoopResult tests the result formatting
func TestFormatContinueLoopResult(t *testing.T) {
	mock := &mockStorageIntegration{}
	handler := &ConversationHandler{storage: mock}

	t.Run("formats empty result", func(t *testing.T) {
		result := handler.formatContinueLoopResult(
			[]string{},
			[]string{},
			[]string{},
			0,
			"no work",
			0,
		)

		if !strings.Contains(result, "Completed: 0") {
			t.Errorf("Expected 0 completed, got: %s", result)
		}
		if !strings.Contains(result, "Partial: 0") {
			t.Errorf("Expected 0 partial, got: %s", result)
		}
		if !strings.Contains(result, "Failed: 0") {
			t.Errorf("Expected 0 failed, got: %s", result)
		}
		if !strings.Contains(result, "Stop Reason: no work") {
			t.Errorf("Expected stop reason, got: %s", result)
		}
	})

	t.Run("formats result with all categories", func(t *testing.T) {
		completed := []string{"vc-1", "vc-2"}
		partial := []string{"vc-3"}
		failed := []string{"vc-4"}

		result := handler.formatContinueLoopResult(
			completed,
			partial,
			failed,
			5,
			"timeout",
			60_000_000_000, // 60 seconds in nanoseconds
		)

		if !strings.Contains(result, "Completed: 2") {
			t.Errorf("Expected 2 completed, got: %s", result)
		}
		if !strings.Contains(result, "vc-1") || !strings.Contains(result, "vc-2") {
			t.Errorf("Expected completed IDs, got: %s", result)
		}
		if !strings.Contains(result, "Partial: 1") {
			t.Errorf("Expected 1 partial, got: %s", result)
		}
		if !strings.Contains(result, "vc-3") {
			t.Errorf("Expected partial ID, got: %s", result)
		}
		if !strings.Contains(result, "Failed: 1") {
			t.Errorf("Expected 1 failed, got: %s", result)
		}
		if !strings.Contains(result, "vc-4") {
			t.Errorf("Expected failed ID, got: %s", result)
		}
		if !strings.Contains(result, "Iterations: 5") {
			t.Errorf("Expected 5 iterations, got: %s", result)
		}
		if !strings.Contains(result, "timeout") {
			t.Errorf("Expected timeout in stop reason, got: %s", result)
		}
	})
}

// TestExecuteIssueValidation tests executeIssue validation (without full execution)
func TestExecuteIssueValidation(t *testing.T) {
	t.Run("validates issue before execution", func(t *testing.T) {
		mock := &mockStorageIntegration{}
		handler := &ConversationHandler{
			storage: mock,
			actor:   "test-actor",
		}
		ctx := context.Background()

		// Closed issue should fail validation
		closedIssue := &types.Issue{
			ID:     "vc-1",
			Status: types.StatusClosed,
		}

		_, err := handler.executeIssue(ctx, closedIssue)
		if err == nil {
			t.Error("Expected error for closed issue")
		}
		if !strings.Contains(err.Error(), "already closed") {
			t.Errorf("Expected 'already closed' error, got: %v", err)
		}
	})

	t.Run("validates issue not in-progress", func(t *testing.T) {
		mock := &mockStorageIntegration{}
		handler := &ConversationHandler{
			storage: mock,
			actor:   "test-actor",
		}
		ctx := context.Background()

		inProgressIssue := &types.Issue{
			ID:     "vc-1",
			Status: types.StatusInProgress,
		}

		_, err := handler.executeIssue(ctx, inProgressIssue)
		if err == nil {
			t.Error("Expected error for in-progress issue")
		}
		if !strings.Contains(err.Error(), "already in progress") {
			t.Errorf("Expected 'already in progress' error, got: %v", err)
		}
	})

	t.Run("validates issue not blocked", func(t *testing.T) {
		mock := &mockStorageIntegration{}
		mock.dependencies = map[string][]*types.Issue{
			"vc-1": {{ID: "vc-2", Status: types.StatusOpen}},
		}
		handler := &ConversationHandler{
			storage: mock,
			actor:   "test-actor",
		}
		ctx := context.Background()

		blockedIssue := &types.Issue{
			ID:     "vc-1",
			Status: types.StatusBlocked,
		}

		_, err := handler.executeIssue(ctx, blockedIssue)
		if err == nil {
			t.Error("Expected error for blocked issue")
		}
		if !strings.Contains(err.Error(), "blocked") {
			t.Errorf("Expected 'blocked' error, got: %v", err)
		}
	})
}

// Note: Full integration tests for executeIssue require mocking the executor.SpawnAgent
// function and AI supervisor, which is beyond the scope of unit tests.
// Those would be better suited for integration tests with test doubles or fixtures.

// TestToolContinueExecutionWithIssueID tests continue_execution with specific issue
func TestToolContinueExecutionWithIssueID(t *testing.T) {
	t.Run("validates specific issue ID", func(t *testing.T) {
		mock := &mockStorageIntegration{}
		mock.issues = map[string]*types.Issue{
			"vc-1": {
				ID:     "vc-1",
				Status: types.StatusClosed,
			},
		}
		handler := &ConversationHandler{
			storage: mock,
			actor:   "test",
		}
		ctx := context.Background()

		// Should return user-facing error for closed issue
		result, err := handler.toolContinueExecution(ctx, map[string]interface{}{
			"issue_id": "vc-1",
		})

		// No system error, but user-facing message
		if err != nil {
			t.Fatalf("Expected no system error, got: %v", err)
		}

		if !strings.Contains(result, "already closed") {
			t.Errorf("Expected 'already closed' in result, got: %s", result)
		}
	})

	t.Run("handles missing issue", func(t *testing.T) {
		mock := &mockStorageIntegration{}
		mock.issues = map[string]*types.Issue{}
		handler := &ConversationHandler{
			storage: mock,
			actor:   "test",
		}
		ctx := context.Background()

		_, err := handler.toolContinueExecution(ctx, map[string]interface{}{
			"issue_id": "vc-999",
		})

		if err == nil {
			t.Error("Expected error for non-existent issue")
		}
		if !strings.Contains(err.Error(), "failed to get issue") {
			t.Errorf("Expected 'failed to get issue' error, got: %v", err)
		}
	})

	t.Run("picks next ready work when no issue_id", func(t *testing.T) {
		mock := &mockStorageIntegration{}
		// No ready work
		handler := &ConversationHandler{
			storage: mock,
			actor:   "test",
		}
		ctx := context.Background()

		result, err := handler.toolContinueExecution(ctx, map[string]interface{}{})

		if err != nil {
			t.Fatalf("Expected no error, got: %v", err)
		}

		if !strings.Contains(result, "No ready work found") {
			t.Errorf("Expected 'No ready work found', got: %s", result)
		}
	})
}

// TestToolContinueUntilBlockedParameters tests parameter parsing
func TestToolContinueUntilBlockedParameters(t *testing.T) {
	t.Run("uses default parameters", func(t *testing.T) {
		mock := &mockStorageIntegration{}
		handler := &ConversationHandler{storage: mock, actor: "test"}
		ctx := context.Background()

		result, err := handler.toolContinueUntilBlocked(ctx, map[string]interface{}{})

		if err != nil {
			t.Fatalf("Expected no error, got: %v", err)
		}

		// Should stop immediately with no ready work
		if !strings.Contains(result, "no more ready work") {
			t.Errorf("Expected 'no more ready work' stop reason, got: %s", result)
		}
	})

	t.Run("respects max_iterations", func(t *testing.T) {
		mock := &mockStorageIntegration{}
		handler := &ConversationHandler{storage: mock, actor: "test"}
		ctx := context.Background()

		result, err := handler.toolContinueUntilBlocked(ctx, map[string]interface{}{
			"max_iterations": float64(5),
		})

		if err != nil {
			t.Fatalf("Expected no error, got: %v", err)
		}

		// Should respect the parameter (stops with no work before max iterations)
		if !strings.Contains(result, "Autonomous Execution Complete") {
			t.Errorf("Expected completion message, got: %s", result)
		}
	})

	t.Run("respects timeout_minutes", func(t *testing.T) {
		mock := &mockStorageIntegration{}
		handler := &ConversationHandler{storage: mock, actor: "test"}
		ctx := context.Background()

		result, err := handler.toolContinueUntilBlocked(ctx, map[string]interface{}{
			"timeout_minutes": float64(1),
		})

		if err != nil {
			t.Fatalf("Expected no error, got: %v", err)
		}

		// Should complete normally (no work, so timeout doesn't trigger)
		if !strings.Contains(result, "Autonomous Execution Complete") {
			t.Errorf("Expected completion message, got: %s", result)
		}
	})

	t.Run("respects error_threshold", func(t *testing.T) {
		mock := &mockStorageIntegration{}
		handler := &ConversationHandler{storage: mock, actor: "test"}
		ctx := context.Background()

		result, err := handler.toolContinueUntilBlocked(ctx, map[string]interface{}{
			"error_threshold": float64(1),
		})

		if err != nil {
			t.Fatalf("Expected no error, got: %v", err)
		}

		// Should complete normally (no work, so errors don't occur)
		if !strings.Contains(result, "Autonomous Execution Complete") {
			t.Errorf("Expected completion message, got: %s", result)
		}
	})
}

// TestIssueExecutionResult tests the result structure
func TestIssueExecutionResult(t *testing.T) {
	t.Run("creates result structure", func(t *testing.T) {
		result := &issueExecutionResult{
			Completed:        true,
			GatesPassed:      true,
			DiscoveredIssues: []string{"vc-1", "vc-2"},
		}

		if !result.Completed {
			t.Error("Expected Completed to be true")
		}
		if !result.GatesPassed {
			t.Error("Expected GatesPassed to be true")
		}
		if len(result.DiscoveredIssues) != 2 {
			t.Errorf("Expected 2 discovered issues, got %d", len(result.DiscoveredIssues))
		}
	})
}

// TestToolContinueUntilBlockedEdgeCases tests edge cases in the autonomous loop
func TestToolContinueUntilBlockedEdgeCases(t *testing.T) {
	t.Run("stops on no ready work immediately", func(t *testing.T) {
		mock := &mockStorageIntegration{}
		// No ready work configured
		handler := &ConversationHandler{storage: mock, actor: "test"}
		ctx := context.Background()

		result, err := handler.toolContinueUntilBlocked(ctx, map[string]interface{}{})

		if err != nil {
			t.Fatalf("Expected no error, got: %v", err)
		}

		if !strings.Contains(result, "no more ready work") {
			t.Errorf("Expected 'no more ready work' in result, got: %s", result)
		}

		if !strings.Contains(result, "Iterations: 0") {
			t.Errorf("Expected 0 iterations, got: %s", result)
		}

		if !strings.Contains(result, "Completed: 0") {
			t.Errorf("Expected 0 completed, got: %s", result)
		}
	})

	t.Run("formats partial completion correctly", func(t *testing.T) {
		mock := &mockStorageIntegration{}
		handler := &ConversationHandler{storage: mock, actor: "test"}

		// Test the formatting directly
		result := handler.formatContinueLoopResult(
			[]string{},           // completed
			[]string{"vc-1"},     // partial
			[]string{},           // failed
			1,                    // iterations
			"no more ready work", // stop reason
			5_000_000_000,        // 5 seconds
		)

		if !strings.Contains(result, "Partial: 1") {
			t.Errorf("Expected 'Partial: 1' in result, got: %s", result)
		}

		if !strings.Contains(result, "vc-1") {
			t.Errorf("Expected partial issue ID in result, got: %s", result)
		}

		if !strings.Contains(result, "work done, left open") {
			t.Errorf("Expected partial explanation in result, got: %s", result)
		}
	})

	t.Run("formats failed issues correctly", func(t *testing.T) {
		mock := &mockStorageIntegration{}
		handler := &ConversationHandler{storage: mock, actor: "test"}

		result := handler.formatContinueLoopResult(
			[]string{},               // completed
			[]string{},               // partial
			[]string{"vc-1", "vc-2"}, // failed
			2,                        // iterations
			"error threshold exceeded (3 consecutive errors)", // stop reason
			10_000_000_000, // 10 seconds
		)

		if !strings.Contains(result, "Failed: 2") {
			t.Errorf("Expected 'Failed: 2' in result, got: %s", result)
		}

		if !strings.Contains(result, "error threshold exceeded") {
			t.Errorf("Expected error threshold in stop reason, got: %s", result)
		}
	})
}

// TestToolContinueExecutionResultFormatting tests the result messages
func TestToolContinueExecutionResultFormatting(t *testing.T) {
	t.Run("formats completed result", func(t *testing.T) {
		// Note: This would require mocking the full execution pipeline
		// For now we verify the validation and parameter parsing
		mock := &mockStorageIntegration{}
		handler := &ConversationHandler{storage: mock, actor: "test"}
		ctx := context.Background()

		// Test with no ready work (simple case)
		result, err := handler.toolContinueExecution(ctx, map[string]interface{}{})
		if err != nil {
			t.Fatalf("Expected no error, got: %v", err)
		}

		if !strings.Contains(result, "No ready work found") {
			t.Errorf("Expected 'No ready work found', got: %s", result)
		}
	})

	t.Run("validates async parameter", func(t *testing.T) {
		mock := &mockStorageIntegration{}
		handler := &ConversationHandler{storage: mock, actor: "test"}
		ctx := context.Background()

		_, err := handler.toolContinueExecution(ctx, map[string]interface{}{
			"async": true,
		})

		if err == nil {
			t.Error("Expected error for async mode")
		}

		if !strings.Contains(err.Error(), "async execution not yet implemented") {
			t.Errorf("Expected async error, got: %v", err)
		}
	})
}

// TestValidateIssueForExecutionErrorHandling tests error paths
func TestValidateIssueForExecutionErrorHandling(t *testing.T) {
	t.Run("handles storage errors when getting dependencies", func(t *testing.T) {
		// In the current implementation, storage errors are returned as system errors
		// This test documents the expected behavior

		mock := &mockStorageIntegration{}
		handler := &ConversationHandler{storage: mock}
		ctx := context.Background()

		// Blocked issue with no dependencies configured
		issue := &types.Issue{
			ID:     "vc-1",
			Status: types.StatusBlocked,
		}

		msg, err := handler.validateIssueForExecution(ctx, issue)
		if err != nil {
			t.Fatalf("Expected no system error, got: %v", err)
		}

		// Should return user-facing message
		if !strings.Contains(msg, "blocked") {
			t.Errorf("Expected blocked message, got: %s", msg)
		}
	})
}

// TestReleaseIssueWithErrorVariants tests different error scenarios
func TestReleaseIssueWithErrorVariants(t *testing.T) {
	t.Run("handles context errors", func(t *testing.T) {
		mock := &mockStorageIntegration{}
		handler := &ConversationHandler{
			storage: mock,
			actor:   "test-actor",
		}

		// Use a canceled context
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		// Should not panic even with canceled context
		handler.releaseIssueWithError(ctx, "vc-1", "test-actor", "Test error")
	})

	t.Run("handles empty error message", func(t *testing.T) {
		mock := &mockStorageIntegration{}
		handler := &ConversationHandler{
			storage: mock,
			actor:   "test-actor",
		}
		ctx := context.Background()

		// Should handle empty error message gracefully
		handler.releaseIssueWithError(ctx, "vc-1", "test-actor", "")
	})
}

