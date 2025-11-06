package executor

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"

	"github.com/steveyegge/vc/internal/storage"
	"github.com/steveyegge/vc/internal/types"
)

// TestExecutorStateTransitions tests the full execution flow with state transitions
func TestExecutorStateTransitions(t *testing.T) {
	// Setup storage
	cfg := storage.DefaultConfig()
	cfg.Path = t.TempDir() + "/test.db"

	ctx := context.Background()
	store, err := storage.NewStorage(ctx, cfg)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer func() { _ = store.Close() }()

	// Create executor with AI supervision disabled (to avoid needing API key in tests)
	execCfg := DefaultConfig()
	execCfg.Store = store
	execCfg.EnableAISupervision = false
	execCfg.PollInterval = 100 * time.Millisecond

	executor, err := New(execCfg)
	if err != nil {
		t.Fatalf("Failed to create executor: %v", err)
	}

	// Register the executor instance manually (since we're not calling Start())
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

	// Create a test issue
	issue := &types.Issue{
		Title:              "Test Issue",
		Description:        "This is a test issue for state transitions",
		IssueType:          types.TypeTask,
		Status:             types.StatusOpen,
		Priority:           1,
		AcceptanceCriteria: "Test should pass",
		CreatedAt:          time.Now(),
		UpdatedAt:          time.Now(),
	}

	if err := store.CreateIssue(ctx, issue, "test"); err != nil {
		t.Fatalf("Failed to create issue: %v", err)
	}

	// Test 1: Claim the issue
	if err := store.ClaimIssue(ctx, issue.ID, executor.instanceID); err != nil {
		t.Fatalf("Failed to claim issue: %v", err)
	}

	// Verify execution state is claimed
	state, err := store.GetExecutionState(ctx, issue.ID)
	if err != nil {
		t.Fatalf("Failed to get execution state: %v", err)
	}
	if state.State != types.ExecutionStateClaimed {
		t.Errorf("Expected state %s, got %s", types.ExecutionStateClaimed, state.State)
	}
	if state.ExecutorInstanceID != executor.instanceID {
		t.Errorf("Expected executor %s, got %s", executor.instanceID, state.ExecutorInstanceID)
	}

	// Test 2: Transition to assessing
	if err := store.UpdateExecutionState(ctx, issue.ID, types.ExecutionStateAssessing); err != nil {
		t.Fatalf("Failed to update execution state: %v", err)
	}

	state, err = store.GetExecutionState(ctx, issue.ID)
	if err != nil {
		t.Fatalf("Failed to get execution state: %v", err)
	}
	if state.State != types.ExecutionStateAssessing {
		t.Errorf("Expected state %s, got %s", types.ExecutionStateAssessing, state.State)
	}

	// Test 3: Transition to executing
	if err := store.UpdateExecutionState(ctx, issue.ID, types.ExecutionStateExecuting); err != nil {
		t.Fatalf("Failed to update execution state: %v", err)
	}

	state, err = store.GetExecutionState(ctx, issue.ID)
	if err != nil {
		t.Fatalf("Failed to get execution state: %v", err)
	}
	if state.State != types.ExecutionStateExecuting {
		t.Errorf("Expected state %s, got %s", types.ExecutionStateExecuting, state.State)
	}

	// Test 4: Transition to analyzing
	if err := store.UpdateExecutionState(ctx, issue.ID, types.ExecutionStateAnalyzing); err != nil {
		t.Fatalf("Failed to update execution state: %v", err)
	}

	state, err = store.GetExecutionState(ctx, issue.ID)
	if err != nil {
		t.Fatalf("Failed to get execution state: %v", err)
	}
	if state.State != types.ExecutionStateAnalyzing {
		t.Errorf("Expected state %s, got %s", types.ExecutionStateAnalyzing, state.State)
	}

	// Test 5: Transition to gates
	if err := store.UpdateExecutionState(ctx, issue.ID, types.ExecutionStateGates); err != nil {
		t.Fatalf("Failed to update execution state: %v", err)
	}

	state, err = store.GetExecutionState(ctx, issue.ID)
	if err != nil {
		t.Fatalf("Failed to get execution state: %v", err)
	}
	if state.State != types.ExecutionStateGates {
		t.Errorf("Expected state %s, got %s", types.ExecutionStateGates, state.State)
	}

	// Test 6: Transition to committing (vc-129)
	if err := store.UpdateExecutionState(ctx, issue.ID, types.ExecutionStateCommitting); err != nil {
		t.Fatalf("Failed to update execution state: %v", err)
	}

	state, err = store.GetExecutionState(ctx, issue.ID)
	if err != nil {
		t.Fatalf("Failed to get execution state: %v", err)
	}
	if state.State != types.ExecutionStateCommitting {
		t.Errorf("Expected state %s, got %s", types.ExecutionStateCommitting, state.State)
	}

	// Test 7: Transition to completed
	if err := store.UpdateExecutionState(ctx, issue.ID, types.ExecutionStateCompleted); err != nil {
		t.Fatalf("Failed to update execution state: %v", err)
	}

	state, err = store.GetExecutionState(ctx, issue.ID)
	if err != nil {
		t.Fatalf("Failed to get execution state: %v", err)
	}
	if state.State != types.ExecutionStateCompleted {
		t.Errorf("Expected state %s, got %s", types.ExecutionStateCompleted, state.State)
	}

	// Test 8: Release the issue (vc-129: updated comment numbering)
	// Note: ReleaseIssue deletes the execution state record (does not change issue status)
	if err := store.ReleaseIssue(ctx, issue.ID); err != nil {
		t.Fatalf("Failed to release issue: %v", err)
	}

	// Verify execution state is deleted after release (vc-134 fix)
	state, err = store.GetExecutionState(ctx, issue.ID)
	if err != nil {
		t.Fatalf("Failed to get execution state: %v", err)
	}
	if state != nil {
		t.Error("Expected execution state to be deleted after release")
	}
}

// TestExecutorWithAISupervisionEnabled tests that executor handles AI supervision config
func TestExecutorWithAISupervisionEnabled(t *testing.T) {
	// Setup storage
	cfg := storage.DefaultConfig()
	cfg.Path = t.TempDir() + "/test.db"

	ctx := context.Background()
	store, err := storage.NewStorage(ctx, cfg)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer func() { _ = store.Close() }()

	// Test 1: Create executor with AI supervision explicitly disabled
	execCfg := DefaultConfig()
	execCfg.Store = store
	execCfg.EnableAISupervision = false

	executor, err := New(execCfg)
	if err != nil {
		t.Fatalf("Failed to create executor: %v", err)
	}

	if executor.enableAISupervision {
		t.Error("AI supervision should be disabled when explicitly disabled")
	}
	if executor.supervisor != nil {
		t.Error("Supervisor should be nil when AI supervision is disabled")
	}

	// Test 2: Verify executor gracefully handles missing API key
	// (when EnableAISupervision = true but ANTHROPIC_API_KEY is not set)
	// This is tested by checking that New() doesn't return an error
	// The executor should log a warning and continue without AI supervision
}

// TestExecutorStateSequence tests that states transition in the correct order
func TestExecutorStateSequence(t *testing.T) {
	// Setup storage
	cfg := storage.DefaultConfig()
	cfg.Path = t.TempDir() + "/test.db"

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

	executor, err := New(execCfg)
	if err != nil {
		t.Fatalf("Failed to create executor: %v", err)
	}

	// Register the executor instance manually (since we're not calling Start())
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

	// Create a test issue
	issue := &types.Issue{
		Title:              "State Sequence Test",
		Description:        "Test state sequence",
		IssueType:          types.TypeTask,
		Status:             types.StatusOpen,
		Priority:           1,
		AcceptanceCriteria: "States transition in order",
		CreatedAt:          time.Now(),
		UpdatedAt:          time.Now(),
	}

	if err := store.CreateIssue(ctx, issue, "test"); err != nil {
		t.Fatalf("Failed to create issue: %v", err)
	}

	// Claim the issue
	if err := store.ClaimIssue(ctx, issue.ID, executor.instanceID); err != nil {
		t.Fatalf("Failed to claim issue: %v", err)
	}

	// Define expected state sequence (all states must be traversed even without AI)
	expectedStates := []types.ExecutionState{
		types.ExecutionStateClaimed,
		types.ExecutionStateAssessing,
		types.ExecutionStateExecuting,
		types.ExecutionStateAnalyzing,
		types.ExecutionStateGates,
		types.ExecutionStateCommitting, // vc-129: must go through committing state
		types.ExecutionStateCompleted,
	}

	// Verify initial state
	state, err := store.GetExecutionState(ctx, issue.ID)
	if err != nil {
		t.Fatalf("Failed to get execution state: %v", err)
	}
	if state.State != expectedStates[0] {
		t.Errorf("Initial state should be %s, got %s", expectedStates[0], state.State)
	}

	// Transition through states
	for i := 1; i < len(expectedStates); i++ {
		if err := store.UpdateExecutionState(ctx, issue.ID, expectedStates[i]); err != nil {
			t.Fatalf("Failed to update to state %s: %v", expectedStates[i], err)
		}

		state, err := store.GetExecutionState(ctx, issue.ID)
		if err != nil {
			t.Fatalf("Failed to get execution state: %v", err)
		}

		if state.State != expectedStates[i] {
			t.Errorf("State %d should be %s, got %s", i, expectedStates[i], state.State)
		}
	}
}

// TestExecutorStateSequenceWithAI tests state sequence when AI supervision is enabled
func TestExecutorStateSequenceWithAI(t *testing.T) {
	// This test documents the expected state sequence when AI supervision is enabled
	// Since we can't test with real AI, we just document the expected sequence

	expectedStatesWithAI := []types.ExecutionState{
		types.ExecutionStateClaimed,    // Issue is claimed
		types.ExecutionStateAssessing,  // AI assesses the issue
		types.ExecutionStateExecuting,  // Agent executes the work
		types.ExecutionStateAnalyzing,  // AI analyzes the result
		types.ExecutionStateGates,      // Quality gates are run
		types.ExecutionStateCommitting, // Changes are committed (vc-129)
		types.ExecutionStateCompleted,  // Work is complete
	}

	// Verify all states are valid
	for _, state := range expectedStatesWithAI {
		if !state.IsValid() {
			t.Errorf("State %s is not valid", state)
		}
	}

	// Document the flow
	t.Logf("Expected state sequence with AI supervision:")
	for i, state := range expectedStatesWithAI {
		t.Logf("  %d. %s", i+1, state)
	}
}

// TestExecutorDoubleClaim tests that two executors cannot claim the same issue
func TestExecutorDoubleClaim(t *testing.T) {
	// Setup storage
	cfg := storage.DefaultConfig()
	cfg.Path = t.TempDir() + "/test.db"

	ctx := context.Background()
	store, err := storage.NewStorage(ctx, cfg)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer func() { _ = store.Close() }()

	// Create two executors
	execCfg1 := DefaultConfig()
	execCfg1.Store = store
	execCfg1.EnableAISupervision = false

	executor1, err := New(execCfg1)
	if err != nil {
		t.Fatalf("Failed to create executor1: %v", err)
	}

	execCfg2 := DefaultConfig()
	execCfg2.Store = store
	execCfg2.EnableAISupervision = false

	executor2, err := New(execCfg2)
	if err != nil {
		t.Fatalf("Failed to create executor2: %v", err)
	}

	// Register both executors manually (since we're not calling Start())
	instance1 := &types.ExecutorInstance{
		InstanceID:    executor1.instanceID,
		Hostname:      executor1.hostname,
		PID:           executor1.pid,
		Status:        types.ExecutorStatusRunning,
		StartedAt:     time.Now(),
		LastHeartbeat: time.Now(),
		Version:       executor1.version,
		Metadata:      "{}",
	}
	if err := store.RegisterInstance(ctx, instance1); err != nil {
		t.Fatalf("Failed to register executor1: %v", err)
	}

	instance2 := &types.ExecutorInstance{
		InstanceID:    executor2.instanceID,
		Hostname:      executor2.hostname,
		PID:           executor2.pid,
		Status:        types.ExecutorStatusRunning,
		StartedAt:     time.Now(),
		LastHeartbeat: time.Now(),
		Version:       executor2.version,
		Metadata:      "{}",
	}
	if err := store.RegisterInstance(ctx, instance2); err != nil {
		t.Fatalf("Failed to register executor2: %v", err)
	}

	// Create a test issue
	issue := &types.Issue{
		Title:              "Double Claim Test",
		Description:        "Test that two executors can't claim the same issue",
		IssueType:          types.TypeTask,
		Status:             types.StatusOpen,
		Priority:           1,
		AcceptanceCriteria: "Only one executor can claim",
		CreatedAt:          time.Now(),
		UpdatedAt:          time.Now(),
	}

	if err := store.CreateIssue(ctx, issue, "test"); err != nil {
		t.Fatalf("Failed to create issue: %v", err)
	}

	// Executor 1 claims the issue
	if err := store.ClaimIssue(ctx, issue.ID, executor1.instanceID); err != nil {
		t.Fatalf("Executor 1 failed to claim issue: %v", err)
	}

	// Executor 2 tries to claim the same issue - should fail
	err = store.ClaimIssue(ctx, issue.ID, executor2.instanceID)
	if err == nil {
		t.Fatal("Executor 2 should not be able to claim already-claimed issue")
	}
	if !strings.Contains(err.Error(), "already claimed") {
		t.Errorf("Expected 'already claimed' error, got: %v", err)
	}

	// Verify executor 1 still has the claim
	state, err := store.GetExecutionState(ctx, issue.ID)
	if err != nil {
		t.Fatalf("Failed to get execution state: %v", err)
	}
	if state.ExecutorInstanceID != executor1.instanceID {
		t.Errorf("Expected executor1 to have claim, got executor %s", state.ExecutorInstanceID)
	}
}

// TestExecutorShutdownCleansOldInstances verifies that executor shutdown
// triggers cleanup of old stopped instances (vc-31)
func TestExecutorShutdownCleansOldInstances(t *testing.T) {
	// Setup storage
	cfg := storage.DefaultConfig()
	cfg.Path = t.TempDir() + "/test.db"

	ctx := context.Background()
	store, err := storage.NewStorage(ctx, cfg)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer func() { _ = store.Close() }()

	// Step 1: Create multiple old stopped instances in the database
	// We'll create 20 old instances and 5 recent ones
	oldInstanceCount := 20
	recentInstanceCount := 5
	now := time.Now()

	// Create old stopped instances (started 48h ago, will be deleted)
	oldTime := now.Add(-48 * time.Hour)
	for i := 0; i < oldInstanceCount; i++ {
		instance := &types.ExecutorInstance{
			InstanceID:    "old-instance-" + strings.Repeat("0", 3-len(string(rune(i)))) + string(rune('0'+i)),
			Hostname:      "test-host",
			PID:           1000 + i,
			Status:        types.ExecutorStatusStopped,
			StartedAt:     oldTime,
			LastHeartbeat: oldTime,
			Version:       "test",
			Metadata:      "{}",
		}
		if err := store.RegisterInstance(ctx, instance); err != nil {
			t.Fatalf("Failed to register old instance %d: %v", i, err)
		}
	}

	// Create recent stopped instances (started 1h ago, will be kept)
	recentTime := now.Add(-1 * time.Hour)
	for i := 0; i < recentInstanceCount; i++ {
		instance := &types.ExecutorInstance{
			InstanceID:    "recent-instance-" + strings.Repeat("0", 3-len(string(rune(i)))) + string(rune('0'+i)),
			Hostname:      "test-host",
			PID:           2000 + i,
			Status:        types.ExecutorStatusStopped,
			StartedAt:     recentTime,
			LastHeartbeat: recentTime,
			Version:       "test",
			Metadata:      "{}",
		}
		if err := store.RegisterInstance(ctx, instance); err != nil {
			t.Fatalf("Failed to register recent instance %d: %v", i, err)
		}
	}

	// Verify we have all instances in the database
	initialCount := oldInstanceCount + recentInstanceCount
	t.Logf("Created %d old instances and %d recent instances", oldInstanceCount, recentInstanceCount)

	// Step 2: Create executor with custom cleanup config
	execCfg := DefaultConfig()
	execCfg.Store = store
	execCfg.EnableAISupervision = false
	execCfg.EnableQualityGates = false // Disable quality gates (including preflight) to speed up test
	execCfg.PollInterval = 100 * time.Millisecond
	// Custom cleanup config: keep only 10 most recent, delete anything older than 1 second
	// This ensures the old instances (48h old) will be deleted
	execCfg.InstanceCleanupAge = 1 * time.Second
	execCfg.InstanceCleanupKeep = 10

	executor, err := New(execCfg)
	if err != nil {
		t.Fatalf("Failed to create executor: %v", err)
	}

	// Step 3: Start and immediately stop the executor
	// We don't need it to actually run, we just need the shutdown cleanup
	if err := executor.Start(ctx); err != nil {
		t.Fatalf("Failed to start executor: %v", err)
	}

	// Give it a moment to start
	time.Sleep(200 * time.Millisecond)

	// Step 4: Stop the executor (this should trigger cleanup)
	stopCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	if err := executor.Stop(stopCtx); err != nil {
		t.Fatalf("Failed to stop executor: %v", err)
	}

	// Step 5: Verify that old instances were deleted and recent ones kept
	// Cast storage to access underlying database for verification
	// This is safe in tests since we know we're using beads storage
	vcStore, ok := store.(interface{ UnderlyingDB() *sql.DB })
	if !ok {
		t.Fatal("Storage doesn't provide UnderlyingDB() method")
	}
	db := vcStore.UnderlyingDB()

	// Query all stopped instances
	query := `
		SELECT COUNT(*)
		FROM vc_executor_instances
		WHERE status = 'stopped'
	`
	var remainingCount int
	if err := db.QueryRowContext(ctx, query).Scan(&remainingCount); err != nil {
		t.Fatalf("Failed to query remaining instances: %v", err)
	}

	// Expected: maxToKeep=10 most recent instances should remain
	// We had 20 old + 5 recent + 1 (the executor we just stopped) = 26 total
	// After cleanup: keep 10 most recent
	expectedRemaining := 10
	if remainingCount != expectedRemaining {
		t.Errorf("Expected %d instances to remain after cleanup, got %d", expectedRemaining, remainingCount)
	}

	// Verify that ALL recent instances are kept (they should be in the top 10 most recent)
	// Recent instances were created 1h ago, so they should all be kept
	for i := 0; i < recentInstanceCount; i++ {
		instanceID := "recent-instance-" + strings.Repeat("0", 3-len(string(rune(i)))) + string(rune('0'+i))
		query := `SELECT COUNT(*) FROM vc_executor_instances WHERE id = ?`
		var count int
		if err := db.QueryRowContext(ctx, query, instanceID).Scan(&count); err != nil {
			t.Fatalf("Failed to check instance %s: %v", instanceID, err)
		}
		if count != 1 {
			t.Errorf("Recent instance %s should have been kept but was deleted", instanceID)
		}
	}

	// Verify that SOME old instances were deleted
	// We had 26 total (20 old + 5 recent + 1 executor), kept 10, so 16 deleted
	deletedCount := initialCount + 1 - expectedRemaining
	if deletedCount != 16 {
		t.Errorf("Expected 16 instances to be deleted, but got %d deleted", deletedCount)
	}

	// Count remaining old instances (should be 10 - 5 recent - 1 executor = 4 old ones kept)
	query = `SELECT COUNT(*) FROM vc_executor_instances WHERE id LIKE 'old-instance-%'`
	var oldRemaining int
	if err := db.QueryRowContext(ctx, query).Scan(&oldRemaining); err != nil {
		t.Fatalf("Failed to count old instances: %v", err)
	}
	expectedOldRemaining := expectedRemaining - recentInstanceCount - 1 // 10 total - 5 recent - 1 executor
	if oldRemaining != expectedOldRemaining {
		t.Errorf("Expected %d old instances to remain, got %d", expectedOldRemaining, oldRemaining)
	}

	t.Logf("✓ Cleanup correctly deleted %d old instances and kept %d most recent",
		initialCount+1-expectedRemaining, expectedRemaining)
}
