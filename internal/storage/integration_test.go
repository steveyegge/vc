package storage

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/steveyegge/vc/internal/types"
)

// TestMultiExecutorClaiming tests that multiple executors can claim different issues concurrently
func TestMultiExecutorClaiming(t *testing.T) {
	backends := []string{"sqlite"}

	for _, backend := range backends {
		t.Run(backend, func(t *testing.T) {

			ctx := context.Background()
			store := setupStorage(t, backend)
			defer store.Close()

			// Create 3 executors
			executors := createExecutors(t, ctx, store, 3)

			// Create 10 issues to claim
			issues := createTestIssues(t, ctx, store, 10)

			// Each executor tries to claim issues concurrently
			var wg sync.WaitGroup
			claimCounts := make([]atomic.Int32, len(executors))

			for i, executor := range executors {
				wg.Add(1)
				go func(execIdx int, execID string) {
					defer wg.Done()

					for _, issue := range issues {
						err := store.ClaimIssue(ctx, issue.ID, execID)
						if err == nil {
							claimCounts[execIdx].Add(1)
						}
						// Expect some failures due to race conditions - that's OK
					}
				}(i, executor.InstanceID)
			}

			wg.Wait()

			// Verify:
			// 1. Count how many issues were actually claimed
			totalClaimed := int32(0)
			for i := range claimCounts {
				count := claimCounts[i].Load()
				t.Logf("Executor %d claimed %d issues", i, count)
				totalClaimed += count
			}

			// 2. Each issue should have execution state from exactly one executor
			claimedIssues := 0
			executorClaimMap := make(map[string]int)

			for _, issue := range issues {
				state, err := store.GetExecutionState(ctx, issue.ID)
				if err != nil {
					t.Errorf("Failed to get execution state for issue %s: %v", issue.ID, err)
					continue
				}
				if state != nil {
					claimedIssues++
					executorClaimMap[state.ExecutorInstanceID]++
					if state.State != types.ExecutionStateClaimed {
						t.Errorf("Issue %s has wrong state: %s (expected claimed)", issue.ID, state.State)
					}
				}
			}

			// Verify the total claimed count matches actual claimed issues
			if totalClaimed != int32(claimedIssues) {
				t.Errorf("Executor claim count (%d) doesn't match actual claimed issues (%d)", totalClaimed, claimedIssues)
			}

			// Verify each executor's claim count matches reality
			for i, executor := range executors {
				reportedCount := claimCounts[i].Load()
				actualCount := executorClaimMap[executor.InstanceID]
				if reportedCount != int32(actualCount) {
					t.Errorf("Executor %d reported %d claims but actually has %d", i, reportedCount, actualCount)
				}
			}

			// Note: Not all issues may be claimed if there are race conditions
			// The important thing is that no issue is double-claimed
			t.Logf("Successfully claimed %d out of %d issues with no double-claims", claimedIssues, len(issues))
		})
	}
}

// TestRaceConditionPrevention tests that the database prevents double-claiming via race conditions
func TestRaceConditionPrevention(t *testing.T) {
	backends := []string{"sqlite"}

	for _, backend := range backends {
		t.Run(backend, func(t *testing.T) {

			ctx := context.Background()
			store := setupStorage(t, backend)
			defer store.Close()

			// Create 2 executors
			executors := createExecutors(t, ctx, store, 2)

			// Create a single issue
			issue := &types.Issue{
				Title:              "Race Condition Test",
				Description:        "Single issue for race testing",
				IssueType:          types.TypeTask,
				Status:             types.StatusOpen,
				Priority:           1,
				AcceptanceCriteria: "Only one executor claims it",
				CreatedAt:          time.Now(),
				UpdatedAt:          time.Now(),
			}
			if err := store.CreateIssue(ctx, issue, "test"); err != nil {
				t.Fatalf("Failed to create issue: %v", err)
			}

			// Both executors try to claim the same issue simultaneously
			var wg sync.WaitGroup
			errors := make([]error, len(executors))

			for i, executor := range executors {
				wg.Add(1)
				go func(idx int, execID string) {
					defer wg.Done()
					errors[idx] = store.ClaimIssue(ctx, issue.ID, execID)
				}(i, executor.InstanceID)
			}

			wg.Wait()

			// Verify: Exactly one executor succeeded
			successCount := 0
			failureCount := 0
			var winnerID string

			for i, err := range errors {
				if err == nil {
					successCount++
					winnerID = executors[i].InstanceID
				} else {
					failureCount++
				}
			}

			if successCount != 1 {
				t.Errorf("Expected exactly 1 successful claim, got %d", successCount)
			}
			if failureCount != 1 {
				t.Errorf("Expected exactly 1 failed claim, got %d", failureCount)
			}

			// Verify the winning executor has the claim
			state, err := store.GetExecutionState(ctx, issue.ID)
			if err != nil {
				t.Fatalf("Failed to get execution state: %v", err)
			}
			if state.ExecutorInstanceID != winnerID {
				t.Errorf("Expected executor %s to have claim, got %s", winnerID, state.ExecutorInstanceID)
			}
		})
	}
}

// TestCheckpointSaveAndRestore tests checkpoint save and restore functionality
func TestCheckpointSaveAndRestore(t *testing.T) {
	backends := []string{"sqlite"}

	for _, backend := range backends {
		t.Run(backend, func(t *testing.T) {

			ctx := context.Background()
			store := setupStorage(t, backend)
			defer store.Close()

			// Create executor and issue
			executors := createExecutors(t, ctx, store, 1)
			executor := executors[0]

			issues := createTestIssues(t, ctx, store, 1)
			issue := issues[0]

			// Claim the issue
			if err := store.ClaimIssue(ctx, issue.ID, executor.InstanceID); err != nil {
				t.Fatalf("Failed to claim issue: %v", err)
			}

			// Create checkpoint data
			checkpointData := map[string]interface{}{
				"step":      3,
				"completed": []string{"task1", "task2"},
				"pending":   []string{"task3", "task4"},
				"metadata": map[string]string{
					"agent_version": "1.0",
					"start_time":    time.Now().Format(time.RFC3339),
				},
			}

			// Save checkpoint
			if err := store.SaveCheckpoint(ctx, issue.ID, checkpointData); err != nil {
				t.Fatalf("Failed to save checkpoint: %v", err)
			}

			// Retrieve checkpoint
			checkpointJSON, err := store.GetCheckpoint(ctx, issue.ID)
			if err != nil {
				t.Fatalf("Failed to get checkpoint: %v", err)
			}

			// Verify checkpoint data
			var restored map[string]interface{}
			if err := json.Unmarshal([]byte(checkpointJSON), &restored); err != nil {
				t.Fatalf("Failed to unmarshal checkpoint: %v", err)
			}

			// Check specific fields
			if restored["step"].(float64) != 3 {
				t.Errorf("Expected step 3, got %v", restored["step"])
			}

			completed := restored["completed"].([]interface{})
			if len(completed) != 2 {
				t.Errorf("Expected 2 completed tasks, got %d", len(completed))
			}

			pending := restored["pending"].([]interface{})
			if len(pending) != 2 {
				t.Errorf("Expected 2 pending tasks, got %d", len(pending))
			}

			metadata := restored["metadata"].(map[string]interface{})
			if metadata["agent_version"] != "1.0" {
				t.Errorf("Expected agent_version 1.0, got %v", metadata["agent_version"])
			}
		})
	}
}

// TestStaleInstanceCleanup tests cleanup of stale executor instances
func TestStaleInstanceCleanup(t *testing.T) {
	backends := []string{"sqlite"}

	for _, backend := range backends {
		t.Run(backend, func(t *testing.T) {

			ctx := context.Background()
			store := setupStorage(t, backend)
			defer store.Close()

			now := time.Now()

			// Create 3 instances with different heartbeat times
			instances := []*types.ExecutorInstance{
				{
					InstanceID:    "fresh-1",
					Hostname:      "host-1",
					PID:           1001,
					Status:        types.ExecutorStatusRunning,
					StartedAt:     now,
					LastHeartbeat: now, // Fresh (just now)
					Version:       "0.1.0",
					Metadata:      "{}",
				},
				{
					InstanceID:    "stale-1",
					Hostname:      "host-2",
					PID:           1002,
					Status:        types.ExecutorStatusRunning,
					StartedAt:     now.Add(-10 * time.Minute),
					LastHeartbeat: now.Add(-10 * time.Minute), // Stale (10 min old)
					Version:       "0.1.0",
					Metadata:      "{}",
				},
				{
					InstanceID:    "stale-2",
					Hostname:      "host-3",
					PID:           1003,
					Status:        types.ExecutorStatusRunning,
					StartedAt:     now.Add(-20 * time.Minute),
					LastHeartbeat: now.Add(-20 * time.Minute), // Very stale (20 min old)
					Version:       "0.1.0",
					Metadata:      "{}",
				},
			}

			for _, instance := range instances {
				if err := store.RegisterInstance(ctx, instance); err != nil {
					t.Fatalf("Failed to register instance %s: %v", instance.InstanceID, err)
				}
			}

			// Verify all are initially active
			active, err := store.GetActiveInstances(ctx)
			if err != nil {
				t.Fatalf("Failed to get active instances: %v", err)
			}
			if len(active) != 3 {
				t.Errorf("Expected 3 active instances before cleanup, got %d", len(active))
			}

			// Cleanup instances stale by more than 5 minutes (300 seconds)
			cleaned, err := store.CleanupStaleInstances(ctx, 300)
			if err != nil {
				t.Fatalf("Failed to cleanup stale instances: %v", err)
			}

			if cleaned != 2 {
				t.Errorf("Expected to cleanup 2 stale instances, cleaned %d", cleaned)
			}

			// Verify only fresh instance is still active
			active, err = store.GetActiveInstances(ctx)
			if err != nil {
				t.Fatalf("Failed to get active instances after cleanup: %v", err)
			}

			if len(active) != 1 {
				t.Errorf("Expected 1 active instance after cleanup, got %d", len(active))
			}

			if len(active) > 0 && active[0].InstanceID != "fresh-1" {
				t.Errorf("Expected fresh-1 to remain active, got %s", active[0].InstanceID)
			}
		})
	}
}

// TestResumeAfterInterruption tests that work can be resumed after executor crash/restart
func TestResumeAfterInterruption(t *testing.T) {
	backends := []string{"sqlite"}

	for _, backend := range backends {
		t.Run(backend, func(t *testing.T) {

			ctx := context.Background()
			store := setupStorage(t, backend)
			defer store.Close()

			// Phase 1: Executor 1 starts work
			executors := createExecutors(t, ctx, store, 1)
			executor1 := executors[0]

			issues := createTestIssues(t, ctx, store, 1)
			issue := issues[0]

			// Claim issue
			if err := store.ClaimIssue(ctx, issue.ID, executor1.InstanceID); err != nil {
				t.Fatalf("Failed to claim issue: %v", err)
			}

			// Progress through some states
			if err := store.UpdateExecutionState(ctx, issue.ID, types.ExecutionStateAssessing); err != nil {
				t.Fatalf("Failed to update state to assessing: %v", err)
			}

			if err := store.UpdateExecutionState(ctx, issue.ID, types.ExecutionStateExecuting); err != nil {
				t.Fatalf("Failed to update state to executing: %v", err)
			}

			// Save checkpoint mid-execution
			checkpointData := map[string]interface{}{
				"step":      2,
				"completed": []string{"setup", "build"},
				"pending":   []string{"test", "deploy"},
			}
			if err := store.SaveCheckpoint(ctx, issue.ID, checkpointData); err != nil {
				t.Fatalf("Failed to save checkpoint: %v", err)
			}

			// Simulate crash: Mark executor 1 as stale
			// (In real scenario, heartbeat would stop updating)
			executor1.LastHeartbeat = time.Now().Add(-10 * time.Minute)
			if err := store.RegisterInstance(ctx, executor1); err != nil {
				t.Fatalf("Failed to update executor1 heartbeat: %v", err)
			}

			// Cleanup stale instances
			cleaned, err := store.CleanupStaleInstances(ctx, 300)
			if err != nil {
				t.Fatalf("Failed to cleanup stale instances: %v", err)
			}
			if cleaned != 1 {
				t.Errorf("Expected to cleanup 1 stale instance, got %d", cleaned)
			}

			// Phase 2: New executor takes over
			// In a real scenario, the watchdog would detect the stale executor
			// and retrieve checkpoint data before releasing the issue

			// Retrieve checkpoint BEFORE releasing (this is what the watchdog would do)
			checkpointJSON, err := store.GetCheckpoint(ctx, issue.ID)
			if err != nil {
				t.Fatalf("Failed to get checkpoint before release: %v", err)
			}

			var savedCheckpoint map[string]interface{}
			if err := json.Unmarshal([]byte(checkpointJSON), &savedCheckpoint); err != nil {
				t.Fatalf("Failed to unmarshal checkpoint: %v", err)
			}

			// Verify we captured the checkpoint
			if savedCheckpoint["step"].(float64) != 2 {
				t.Errorf("Expected saved checkpoint step 2, got %v", savedCheckpoint["step"])
			}

			// Now release the issue
			if err := store.ReleaseIssue(ctx, issue.ID); err != nil {
				t.Fatalf("Failed to release issue: %v", err)
			}

			// Update issue status back to open so it appears in ready work
			updates := map[string]interface{}{"status": types.StatusOpen}
			if err := store.UpdateIssue(ctx, issue.ID, updates, "test"); err != nil {
				t.Fatalf("Failed to update issue status to open: %v", err)
			}

			// Create new executor (simulating restart or different instance)
			executors2 := createExecutors(t, ctx, store, 1)
			executor2 := executors2[0]

			// Executor 2 discovers the work and claims it
			readyWork, err := store.GetReadyWork(ctx, types.WorkFilter{
				Status: types.StatusOpen,
				Limit:  10,
			})
			if err != nil {
				t.Fatalf("Failed to get ready work: %v", err)
			}

			found := false
			for _, w := range readyWork {
				if w.ID == issue.ID {
					found = true
					break
				}
			}
			if !found {
				t.Fatal("Issue not found in ready work after release")
			}

			// Claim the issue
			if err := store.ClaimIssue(ctx, issue.ID, executor2.InstanceID); err != nil {
				t.Fatalf("Failed to claim issue with executor2: %v", err)
			}

			// In a real implementation, the executor would restore from the saved checkpoint
			// For this test, we verify the saved checkpoint data is intact
			completed := savedCheckpoint["completed"].([]interface{})
			if len(completed) != 2 {
				t.Errorf("Expected 2 completed tasks when resuming, got %d", len(completed))
			}

			pending := savedCheckpoint["pending"].([]interface{})
			if len(pending) != 2 {
				t.Errorf("Expected 2 pending tasks when resuming, got %d", len(pending))
			}

			// Now executor2 would resume work and complete it
			// Must go through state transitions properly
			if err := store.UpdateExecutionState(ctx, issue.ID, types.ExecutionStateAssessing); err != nil {
				t.Fatalf("Failed to update state to assessing: %v", err)
			}
			if err := store.UpdateExecutionState(ctx, issue.ID, types.ExecutionStateExecuting); err != nil {
				t.Fatalf("Failed to update state to executing: %v", err)
			}
			if err := store.UpdateExecutionState(ctx, issue.ID, types.ExecutionStateAnalyzing); err != nil {
				t.Fatalf("Failed to update state to analyzing: %v", err)
			}
			if err := store.UpdateExecutionState(ctx, issue.ID, types.ExecutionStateGates); err != nil {
				t.Fatalf("Failed to update state to gates: %v", err)
			}
			if err := store.UpdateExecutionState(ctx, issue.ID, types.ExecutionStateCompleted); err != nil {
				t.Fatalf("Failed to update state to completed: %v", err)
			}

			// Close the issue
			closeUpdates := map[string]interface{}{"status": types.StatusClosed}
			if err := store.UpdateIssue(ctx, issue.ID, closeUpdates, executor2.InstanceID); err != nil {
				t.Fatalf("Failed to close issue: %v", err)
			}

			// Release execution state
			if err := store.ReleaseIssue(ctx, issue.ID); err != nil {
				t.Fatalf("Failed to release issue: %v", err)
			}

			// Verify issue is now closed
			finalIssue, err := store.GetIssue(ctx, issue.ID)
			if err != nil {
				t.Fatalf("Failed to get final issue: %v", err)
			}
			if finalIssue.Status != types.StatusClosed {
				t.Errorf("Expected issue to be closed, got %s", finalIssue.Status)
			}
		})
	}
}

// TestCompleteExecutorWorkflow tests the full workflow from claim to completion
func TestCompleteExecutorWorkflow(t *testing.T) {
	backends := []string{"sqlite"}

	for _, backend := range backends {
		t.Run(backend, func(t *testing.T) {

			ctx := context.Background()
			store := setupStorage(t, backend)
			defer store.Close()

			// Create executor
			executors := createExecutors(t, ctx, store, 1)
			executor := executors[0]

			// Create issue
			issues := createTestIssues(t, ctx, store, 1)
			issue := issues[0]

			// Test the complete state sequence
			expectedStates := []types.ExecutionState{
				types.ExecutionStateClaimed,
				types.ExecutionStateAssessing,
				types.ExecutionStateExecuting,
				types.ExecutionStateAnalyzing,
				types.ExecutionStateGates,
				types.ExecutionStateCompleted,
			}

			// Claim issue
			if err := store.ClaimIssue(ctx, issue.ID, executor.InstanceID); err != nil {
				t.Fatalf("Failed to claim issue: %v", err)
			}

			// Verify claimed state
			state, err := store.GetExecutionState(ctx, issue.ID)
			if err != nil {
				t.Fatalf("Failed to get execution state: %v", err)
			}
			if state.State != expectedStates[0] {
				t.Errorf("Expected state %s, got %s", expectedStates[0], state.State)
			}

			// Transition through all states
			for i := 1; i < len(expectedStates); i++ {
				if err := store.UpdateExecutionState(ctx, issue.ID, expectedStates[i]); err != nil {
					t.Fatalf("Failed to update to state %s: %v", expectedStates[i], err)
				}

				state, err := store.GetExecutionState(ctx, issue.ID)
				if err != nil {
					t.Fatalf("Failed to get execution state at step %d: %v", i, err)
				}
				if state.State != expectedStates[i] {
					t.Errorf("At step %d: expected state %s, got %s", i, expectedStates[i], state.State)
				}

				// Save checkpoint at each step
				checkpointData := map[string]interface{}{
					"state": string(expectedStates[i]),
					"step":  i,
				}
				if err := store.SaveCheckpoint(ctx, issue.ID, checkpointData); err != nil {
					t.Fatalf("Failed to save checkpoint at step %d: %v", i, err)
				}
			}

			// Release issue
			if err := store.ReleaseIssue(ctx, issue.ID); err != nil {
				t.Fatalf("Failed to release issue: %v", err)
			}

			// Verify execution state is cleared
			state, err = store.GetExecutionState(ctx, issue.ID)
			if err != nil {
				t.Fatalf("Failed to get execution state after release: %v", err)
			}
			if state != nil {
				t.Error("Expected execution state to be nil after release")
			}
		})
	}
}

// TestEpicChildDependencyDirection tests that epic-child dependencies use the standard (child, parent) direction
func TestEpicChildDependencyDirection(t *testing.T) {
	backends := []string{"sqlite"}

	for _, backend := range backends {
		t.Run(backend, func(t *testing.T) {

			ctx := context.Background()
			store := setupStorage(t, backend)
			defer store.Close()

			// Create an epic
			epic := &types.Issue{
				Title:              "Test Epic",
				Description:        "Epic for testing child dependencies",
				IssueType:          types.TypeEpic,
				Status:             types.StatusOpen,
				Priority:           0,
				AcceptanceCriteria: "All children complete",
				CreatedAt:          time.Now(),
				UpdatedAt:          time.Now(),
			}
			if err := store.CreateIssue(ctx, epic, "test"); err != nil {
				t.Fatalf("Failed to create epic: %v", err)
			}

			// Create 3 child tasks
			children := make([]*types.Issue, 3)
			for i := 0; i < 3; i++ {
				child := &types.Issue{
					Title:              fmt.Sprintf("Child Task %d", i+1),
					Description:        fmt.Sprintf("Child task %d of epic", i+1),
					IssueType:          types.TypeTask,
					Status:             types.StatusOpen,
					Priority:           1,
					AcceptanceCriteria: fmt.Sprintf("Task %d complete", i+1),
					CreatedAt:          time.Now(),
					UpdatedAt:          time.Now(),
				}
				if err := store.CreateIssue(ctx, child, "test"); err != nil {
					t.Fatalf("Failed to create child %d: %v", i, err)
				}
				children[i] = child

				// Add dependency: child -> epic (standard direction)
				dep := types.Dependency{
					IssueID:     child.ID,
					DependsOnID: epic.ID,
					Type:        types.DepParentChild,
					CreatedAt:   time.Now(),
					CreatedBy:   "test",
				}
				if err := store.AddDependency(ctx, &dep, "test"); err != nil {
					t.Fatalf("Failed to add dependency for child %d: %v", i, err)
				}
			}

			// Test 1: GetDependencies(child) should return the epic parent
			// This verifies child depends ON epic (child.ID -> epic.ID)
			for i, child := range children {
				deps, err := store.GetDependencies(ctx, child.ID)
				if err != nil {
					t.Fatalf("Failed to get dependencies for child %d: %v", i, err)
				}
				if len(deps) != 1 {
					t.Errorf("Child %d: expected 1 dependency (the epic), got %d", i, len(deps))
					continue
				}
				// deps[0] is the Issue that child depends on (should be the epic)
				if deps[0].ID != epic.ID {
					t.Errorf("Child %d: expected to depend on epic %s, got %s", i, epic.ID, deps[0].ID)
				}
				if deps[0].IssueType != types.TypeEpic {
					t.Errorf("Child %d: dependency should be an epic, got %s", i, deps[0].IssueType)
				}
			}

			// Test 2: GetDependents(epic) should return all children
			// This verifies the query looks for issues that depend ON the epic
			dependents, err := store.GetDependents(ctx, epic.ID)
			if err != nil {
				t.Fatalf("Failed to get dependents for epic: %v", err)
			}
			if len(dependents) != 3 {
				t.Errorf("Epic: expected 3 dependents (children), got %d", len(dependents))
			}

			// Verify all children are in the dependents list
			childIDs := make(map[string]bool)
			for _, child := range children {
				childIDs[child.ID] = true
			}
			for _, dependent := range dependents {
				if !childIDs[dependent.ID] {
					t.Errorf("Epic: unexpected dependent %s", dependent.ID)
				}
				if dependent.IssueType != types.TypeTask {
					t.Errorf("Epic: dependent %s should be a task, got %s", dependent.ID, dependent.IssueType)
				}
			}

			// Test 3: Verify there are NO old-style (epic, child) dependencies
			// Old style would be: epic.ID as issue_id, child.ID as depends_on_id
			// In old style, GetDependencies(epic) would return children
			// In new style, GetDependencies(epic) should return nothing (or non-parent-child deps)
			epicDeps, err := store.GetDependencies(ctx, epic.ID)
			if err != nil {
				t.Fatalf("Failed to get dependencies for epic: %v", err)
			}
			// Epic should not depend on its children - children depend on epic
			for _, dep := range epicDeps {
				if dep.IssueType == types.TypeTask {
					// If epic "depends on" any of its children, that's old-style and wrong
					for _, child := range children {
						if dep.ID == child.ID {
							t.Errorf("Found old-style dependency: epic depends on child %s (should be child -> epic)", child.ID)
						}
					}
				}
			}
		})
	}
}

// Helper functions

func setupStorage(t *testing.T, backend string) Storage {
	t.Helper()

	ctx := context.Background()

	// Create a temporary file for the test database
	// We can't use :memory: because MkdirAll fails on it
	tmpfile, err := os.CreateTemp("", "test-*.db")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	tmpfile.Close()

	// Clean up the temp file after the test
	t.Cleanup(func() {
		os.Remove(tmpfile.Name())
	})

	cfg := DefaultConfig()
	cfg.Path = tmpfile.Name()

	store, err := NewStorage(ctx, cfg)
	if err != nil {
		t.Fatalf("Failed to create SQLite storage: %v", err)
	}
	return store
}

func createExecutors(t *testing.T, ctx context.Context, store Storage, count int) []*types.ExecutorInstance {
	t.Helper()

	executors := make([]*types.ExecutorInstance, count)
	now := time.Now()

	for i := 0; i < count; i++ {
		executor := &types.ExecutorInstance{
			InstanceID:    fmt.Sprintf("test-executor-%d", i),
			Hostname:      fmt.Sprintf("test-host-%d", i),
			PID:           1000 + i,
			Status:        types.ExecutorStatusRunning,
			StartedAt:     now,
			LastHeartbeat: now,
			Version:       "0.1.0-test",
			Metadata:      "{}",
		}

		if err := store.RegisterInstance(ctx, executor); err != nil {
			t.Fatalf("Failed to register executor %d: %v", i, err)
		}

		executors[i] = executor
	}

	return executors
}

func createTestIssues(t *testing.T, ctx context.Context, store Storage, count int) []*types.Issue {
	t.Helper()

	issues := make([]*types.Issue, count)
	now := time.Now()

	for i := 0; i < count; i++ {
		issue := &types.Issue{
			Title:              fmt.Sprintf("Test Issue %d", i),
			Description:        fmt.Sprintf("Integration test issue %d", i),
			IssueType:          types.TypeTask,
			Status:             types.StatusOpen,
			Priority:           1,
			AcceptanceCriteria: fmt.Sprintf("Test %d should pass", i),
			CreatedAt:          now,
			UpdatedAt:          now,
		}

		if err := store.CreateIssue(ctx, issue, "test"); err != nil {
			t.Fatalf("Failed to create issue %d: %v", i, err)
		}

		issues[i] = issue
	}

	return issues
}

// TestGetMissionWithApprovalMetadata tests that GetMission properly loads approval fields
func TestGetMissionWithApprovalMetadata(t *testing.T) {
	backends := []string{"sqlite"}

	for _, backend := range backends {
		t.Run(backend, func(t *testing.T) {

			ctx := context.Background()
			store := setupStorage(t, backend)
			defer store.Close()

			// Create a mission (epic issue type)
			mission := &types.Issue{
				ID:                 "", // Will be auto-generated
				Title:              "Test Mission",
				Description:        "Mission for testing approval metadata",
				IssueType:          types.TypeEpic,
				Status:             types.StatusOpen,
				Priority:           0,
				AcceptanceCriteria: "All phases complete",
				CreatedAt:          time.Now(),
				UpdatedAt:          time.Now(),
			}
			if err := store.CreateIssue(ctx, mission, "test"); err != nil {
				t.Fatalf("Failed to create mission: %v", err)
			}

			// Test 1: Mission without approval (ApprovedAt = nil)
			retrieved, err := store.GetMission(ctx, mission.ID)
			if err != nil {
				t.Fatalf("Failed to get mission: %v", err)
			}
			if retrieved == nil {
				t.Fatal("GetMission returned nil")
			}
			if retrieved.ApprovedAt != nil {
				t.Errorf("Expected ApprovedAt to be nil, got %v", retrieved.ApprovedAt)
			}
			if retrieved.ApprovedBy != "" {
				t.Errorf("Expected ApprovedBy to be empty, got %s", retrieved.ApprovedBy)
			}

			// Test 2: Approve the mission
			approvalTime := time.Now()
			approver := "test-user"
			updates := map[string]interface{}{
				"approved_at": approvalTime,
				"approved_by": approver,
			}
			if err := store.UpdateIssue(ctx, mission.ID, updates, "test"); err != nil {
				t.Fatalf("Failed to update mission with approval: %v", err)
			}

			// Test 3: Retrieve mission again and verify approval fields are populated
			approved, err := store.GetMission(ctx, mission.ID)
			if err != nil {
				t.Fatalf("Failed to get approved mission: %v", err)
			}
			if approved == nil {
				t.Fatal("GetMission returned nil for approved mission")
			}
			if approved.ApprovedAt == nil {
				t.Error("Expected ApprovedAt to be set after approval")
			} else {
				// Check that the time is close (within 1 second due to rounding)
				diff := approved.ApprovedAt.Sub(approvalTime)
				if diff < 0 {
					diff = -diff
				}
				if diff > time.Second {
					t.Errorf("ApprovedAt time mismatch: expected %v, got %v (diff: %v)",
						approvalTime, approved.ApprovedAt, diff)
				}
			}
			if approved.ApprovedBy != approver {
				t.Errorf("Expected ApprovedBy to be %s, got %s", approver, approved.ApprovedBy)
			}

			// Test 4: Verify IsApproved() works correctly with database state
			// First test: Mission with ApprovalRequired=false should return true
			approved.ApprovalRequired = false
			if !approved.IsApproved() {
				t.Error("Mission with ApprovalRequired=false should be approved")
			}

			// Second test: Mission with ApprovalRequired=true and ApprovedAt set should return true
			approved.ApprovalRequired = true
			if !approved.IsApproved() {
				t.Error("Mission with ApprovalRequired=true and ApprovedAt set should be approved")
			}

			// Test 5: Create a mission that requires approval but hasn't been approved
			unapprovedMission := &types.Issue{
				Title:              "Unapproved Mission",
				Description:        "Mission requiring approval",
				IssueType:          types.TypeEpic,
				Status:             types.StatusOpen,
				Priority:           0,
				AcceptanceCriteria: "Needs approval",
				CreatedAt:          time.Now(),
				UpdatedAt:          time.Now(),
			}
			if err := store.CreateIssue(ctx, unapprovedMission, "test"); err != nil {
				t.Fatalf("Failed to create unapproved mission: %v", err)
			}

			unapproved, err := store.GetMission(ctx, unapprovedMission.ID)
			if err != nil {
				t.Fatalf("Failed to get unapproved mission: %v", err)
			}
			unapproved.ApprovalRequired = true
			if unapproved.IsApproved() {
				t.Error("Mission with ApprovalRequired=true and ApprovedAt=nil should not be approved")
			}

			// Test 6: Verify GetIssue doesn't populate approval fields (sanity check)
			asIssue, err := store.GetIssue(ctx, mission.ID)
			if err != nil {
				t.Fatalf("Failed to get mission as issue: %v", err)
			}
			// GetIssue returns *types.Issue which doesn't have ApprovedAt/ApprovedBy fields
			// Just verify it doesn't fail
			if asIssue == nil {
				t.Fatal("GetIssue returned nil")
			}
			if asIssue.ID != mission.ID {
				t.Errorf("Expected issue ID %s, got %s", mission.ID, asIssue.ID)
			}
		})
	}
}
