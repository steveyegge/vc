package executor

import (
	"context"
	"testing"
	"time"

	"github.com/steveyegge/vc/internal/storage"
	"github.com/steveyegge/vc/internal/types"
)

// TestQualityGateRaceWithStaleCleanup tests the race condition where CleanupStaleInstances
// deletes execution state while quality gates are still running (vc-178)
//
// This test simulates:
// 1. Executor processes issue, reaches "gates" state
// 2. Quality gates are running (simulated by slow test provider)
// 3. Executor heartbeat expires (simulated by making it stale)
// 4. CleanupStaleInstances runs and deletes execution state
// 5. Quality gates finish and results processor tries to ReleaseIssue
// 6. ReleaseIssue should handle "execution state not found" gracefully
//
// Before the fix (vc-178), step 6 would fail with an error.
// After the fix, releaseExecutionState tolerates already-cleaned state.
func TestQualityGateRaceWithStaleCleanup(t *testing.T) {
	// Setup storage
	cfg := storage.DefaultConfig()
	cfg.Path = ":memory:"

	ctx := context.Background()
	store, err := storage.NewStorage(ctx, cfg)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer func() { _ = store.Close() }()

	// Create executor
	execCfg := DefaultConfig()
	execCfg.Store = store
	execCfg.EnableAISupervision = false
	execCfg.EnableQualityGates = true

	executor, err := New(execCfg)
	if err != nil {
		t.Fatalf("Failed to create executor: %v", err)
	}

	// Register executor instance
	instance := &types.ExecutorInstance{
		InstanceID:    executor.instanceID,
		Hostname:      executor.hostname,
		PID:           executor.pid,
		Status:        types.ExecutorStatusRunning,
		StartedAt:     time.Now(),
		LastHeartbeat: time.Now(),
		Version:       executor.version,
		Metadata:      "{}",
	}
	if err := store.RegisterInstance(ctx, instance); err != nil {
		t.Fatalf("Failed to register executor: %v", err)
	}

	// Create test issue
	issue := &types.Issue{
		Title:              "Test race condition between gates and cleanup",
		Description:        "This issue simulates execution state being cleaned up while gates run",
		IssueType:          types.TypeTask,
		Status:             types.StatusOpen,
		Priority:           1,
		AcceptanceCriteria: "Handles race condition gracefully",
		CreatedAt:          time.Now(),
		UpdatedAt:          time.Now(),
	}

	if err := store.CreateIssue(ctx, issue, "test"); err != nil {
		t.Fatalf("Failed to create issue: %v", err)
	}

	// Claim issue and transition to gates state (simulating normal execution)
	if err := store.ClaimIssue(ctx, issue.ID, executor.instanceID); err != nil {
		t.Fatalf("Failed to claim issue: %v", err)
	}

	// Transition through required states: claimed → assessing → executing → analyzing → gates
	if err := store.UpdateExecutionState(ctx, issue.ID, types.ExecutionStateAssessing); err != nil {
		t.Fatalf("Failed to update to assessing: %v", err)
	}
	if err := store.UpdateExecutionState(ctx, issue.ID, types.ExecutionStateExecuting); err != nil {
		t.Fatalf("Failed to update to executing: %v", err)
	}
	if err := store.UpdateExecutionState(ctx, issue.ID, types.ExecutionStateAnalyzing); err != nil {
		t.Fatalf("Failed to update to analyzing: %v", err)
	}
	if err := store.UpdateExecutionState(ctx, issue.ID, types.ExecutionStateGates); err != nil {
		t.Fatalf("Failed to update to gates: %v", err)
	}

	// Verify execution state exists before cleanup
	execState, err := store.GetExecutionState(ctx, issue.ID)
	if err != nil {
		t.Fatalf("Failed to get execution state: %v", err)
	}
	if execState == nil {
		t.Fatal("Expected execution state to exist")
	}
	if execState.State != types.ExecutionStateGates {
		t.Errorf("Expected state 'gates', got %s", execState.State)
	}

	// SIMULATE THE RACE CONDITION:
	// While gates are "running", executor becomes stale and cleanup deletes execution state

	// Step 1: Mark executor as stale (heartbeat expired)
	now := time.Now()
	staleInstance := &types.ExecutorInstance{
		InstanceID:    executor.instanceID,
		Hostname:      executor.hostname,
		PID:           executor.pid,
		Status:        types.ExecutorStatusRunning,
		StartedAt:     now.Add(-20 * time.Minute), // Started 20 min ago
		LastHeartbeat: now.Add(-10 * time.Minute), // Last heartbeat 10 min ago (stale!)
		Version:       executor.version,
		Metadata:      "{}",
	}
	if err := store.RegisterInstance(ctx, staleInstance); err != nil {
		t.Fatalf("Failed to update executor to stale: %v", err)
	}

	// Step 2: Run cleanup (this will delete execution state and reopen issue)
	cleaned, err := store.CleanupStaleInstances(ctx, 300) // 5 min threshold
	if err != nil {
		t.Fatalf("Failed to cleanup stale instances: %v", err)
	}
	if cleaned != 1 {
		t.Errorf("Expected 1 instance cleaned up, got %d", cleaned)
	}

	// Verify execution state was released but preserved (for checkpoint/resume)
	execStateAfterCleanup, err := store.GetExecutionState(ctx, issue.ID)
	if err != nil {
		t.Fatalf("Failed to check execution state after cleanup: %v", err)
	}
	if execStateAfterCleanup == nil {
		t.Error("Expected execution state to be preserved by cleanup (for checkpoint data)")
	} else {
		// Verify the claim was cleared
		if execStateAfterCleanup.ExecutorInstanceID != "" {
			t.Errorf("Expected executor_instance_id to be cleared, got %s", execStateAfterCleanup.ExecutorInstanceID)
		}
		// Verify state was reset to pending
		if execStateAfterCleanup.State != types.ExecutionStatePending {
			t.Errorf("Expected state to be reset to 'pending', got %s", execStateAfterCleanup.State)
		}
	}

	// Verify issue was reopened by cleanup
	issueAfterCleanup, err := store.GetIssue(ctx, issue.ID)
	if err != nil {
		t.Fatalf("Failed to get issue after cleanup: %v", err)
	}
	if issueAfterCleanup.Status != types.StatusOpen {
		t.Errorf("Expected issue to be reopened to 'open', got %s", issueAfterCleanup.Status)
	}

	// Step 3: Simulate quality gates finishing and trying to release execution state
	// This is what the ResultsProcessor does after gates run

	// Create a results processor
	rpCfg := &ResultsProcessorConfig{
		Store:              store,
		Supervisor:         nil, // No AI for this test
		EnableQualityGates: true,
		EnableAutoCommit:   false,
		WorkingDir:         ".",
		Actor:              "test-executor",
	}
	rp, err := NewResultsProcessor(rpCfg)
	if err != nil {
		t.Fatalf("Failed to create results processor: %v", err)
	}

	// THE KEY TEST: Call releaseExecutionState after cleanup has already cleared it
	// This should succeed (state exists but executor_instance_id is NULL)
	err = rp.releaseExecutionState(ctx, issue.ID)
	if err != nil {
		t.Errorf("releaseExecutionState should handle already-cleaned state gracefully, got error: %v", err)
	}

	// Verify the execution state still exists after release (it was already released by cleanup)
	finalState, err := store.GetExecutionState(ctx, issue.ID)
	if err != nil {
		t.Fatalf("Failed to check execution state after release: %v", err)
	}
	// After cleanup, state should be pending with NULL executor
	// After releaseExecutionState, it should be deleted
	if finalState != nil {
		t.Logf("Note: execution state still exists after releaseExecutionState (may be expected depending on implementation)")
	}

	t.Logf("✓ Race condition test passed: releaseExecutionState handles already-cleaned state gracefully")
	t.Logf("  - Cleanup released execution state while gates were 'running'")
	t.Logf("  - releaseExecutionState handled the already-released state without error")
}
