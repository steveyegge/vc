package main

import (
	"context"
	"testing"
	"time"

	"github.com/steveyegge/vc/internal/storage/beads"
	"github.com/steveyegge/vc/internal/types"
)

func TestStaleCommand(t *testing.T) {
	// Create in-memory database for testing
	ctx := context.Background()
	testStore, err := beads.NewVCStorage(ctx, ":memory:")
	if err != nil {
		t.Fatalf("Failed to create test storage: %v", err)
	}
	defer testStore.Close()

	// Override the global store for the test
	originalStore := store
	store = testStore
	defer func() { store = originalStore }()

	// Create a test issue
	issue := &types.Issue{
		Title:       "Test issue for stale detection",
		Description: "This issue will be claimed by a stopped executor",
		Status:      types.StatusOpen,
		Priority:    2,
		IssueType:   types.TypeTask,
	}

	err = testStore.CreateIssue(ctx, issue, "test-user")
	if err != nil {
		t.Fatalf("Failed to create test issue: %v", err)
	}

	// Register a running executor instance
	executorInstance := &types.ExecutorInstance{
		InstanceID:    "test-executor-stopped",
		Hostname:      "test-host",
		PID:           12345,
		Version:       "test-v1",
		Status:        types.ExecutorStatusRunning,
		StartedAt:     time.Now().Add(-1 * time.Hour),
		LastHeartbeat: time.Now().Add(-30 * time.Minute),
	}

	err = testStore.RegisterInstance(ctx, executorInstance)
	if err != nil {
		t.Fatalf("Failed to register executor instance: %v", err)
	}

	// Claim the issue with the executor
	err = testStore.ClaimIssue(ctx, issue.ID, executorInstance.InstanceID)
	if err != nil {
		t.Fatalf("Failed to claim issue: %v", err)
	}

	// Now mark executor as stopped (simulating crash)
	err = testStore.MarkInstanceStopped(ctx, executorInstance.InstanceID)
	if err != nil {
		t.Fatalf("Failed to mark executor as stopped: %v", err)
	}

	// Test 1: getStaleIssues should find the issue
	staleIssues, err := getStaleIssues(ctx, 5*time.Minute)
	if err != nil {
		t.Fatalf("getStaleIssues failed: %v", err)
	}

	if len(staleIssues) != 1 {
		t.Errorf("Expected 1 stale issue, got %d", len(staleIssues))
	}

	if len(staleIssues) > 0 {
		si := staleIssues[0]
		if si.IssueID != issue.ID {
			t.Errorf("Expected issue ID %s, got %s", issue.ID, si.IssueID)
		}
		if si.ExecutorInstanceID != executorInstance.InstanceID {
			t.Errorf("Expected executor ID %s, got %s", executorInstance.InstanceID, si.ExecutorInstanceID)
		}
		if si.ExecutorStatus != "stopped" {
			t.Errorf("Expected executor status 'stopped', got %s", si.ExecutorStatus)
		}
	}

	// Test 2: releaseStaleIssue should release the issue
	if len(staleIssues) > 0 {
		err = releaseStaleIssue(ctx, staleIssues[0])
		if err != nil {
			t.Fatalf("releaseStaleIssue failed: %v", err)
		}

		// Verify issue was released
		updatedIssue, err := testStore.GetIssue(ctx, issue.ID)
		if err != nil {
			t.Fatalf("Failed to get updated issue: %v", err)
		}

		if updatedIssue.Status != types.StatusOpen {
			t.Errorf("Expected issue status to be 'open', got %s", updatedIssue.Status)
		}

		// Verify execution state was updated
		execState, err := testStore.GetExecutionState(ctx, issue.ID)
		if err != nil {
			t.Fatalf("Failed to get execution state: %v", err)
		}

		if execState.ExecutorInstanceID != "" {
			t.Errorf("Expected executor_instance_id to be cleared, got %s", execState.ExecutorInstanceID)
		}

		if execState.State != types.ExecutionStatePending {
			t.Errorf("Expected execution state 'pending', got %s", execState.State)
		}

		// Verify no stale issues remain
		staleIssues, err = getStaleIssues(ctx, 5*time.Minute)
		if err != nil {
			t.Fatalf("getStaleIssues failed: %v", err)
		}

		if len(staleIssues) != 0 {
			t.Errorf("Expected 0 stale issues after release, got %d", len(staleIssues))
		}
	}
}

func TestStaleCommandWithStaleHeartbeat(t *testing.T) {
	// Create in-memory database for testing
	ctx := context.Background()
	testStore, err := beads.NewVCStorage(ctx, ":memory:")
	if err != nil {
		t.Fatalf("Failed to create test storage: %v", err)
	}
	defer testStore.Close()

	// Override the global store for the test
	originalStore := store
	store = testStore
	defer func() { store = originalStore }()

	// Create a test issue
	issue := &types.Issue{
		Title:       "Test issue for stale heartbeat detection",
		Description: "This issue will be claimed by an executor with stale heartbeat",
		Status:      types.StatusOpen,
		Priority:    2,
		IssueType:   types.TypeTask,
	}

	err = testStore.CreateIssue(ctx, issue, "test-user")
	if err != nil {
		t.Fatalf("Failed to create test issue: %v", err)
	}

	// Register an executor instance with stale heartbeat
	executorInstance := &types.ExecutorInstance{
		InstanceID:    "test-executor-stale-heartbeat",
		Hostname:      "test-host",
		PID:           12346,
		Version:       "test-v1",
		Status:        types.ExecutorStatusRunning,
		StartedAt:     time.Now().Add(-1 * time.Hour),
		LastHeartbeat: time.Now().Add(-10 * time.Minute), // Stale (> 5 minutes)
	}

	err = testStore.RegisterInstance(ctx, executorInstance)
	if err != nil {
		t.Fatalf("Failed to register executor instance: %v", err)
	}

	// Claim the issue with the executor
	err = testStore.ClaimIssue(ctx, issue.ID, executorInstance.InstanceID)
	if err != nil {
		t.Fatalf("Failed to claim issue: %v", err)
	}

	// Test: getStaleIssues should find the issue (threshold: 5 minutes)
	staleIssues, err := getStaleIssues(ctx, 5*time.Minute)
	if err != nil {
		t.Fatalf("getStaleIssues failed: %v", err)
	}

	if len(staleIssues) != 1 {
		t.Errorf("Expected 1 stale issue, got %d", len(staleIssues))
	}

	if len(staleIssues) > 0 {
		si := staleIssues[0]
		if si.IssueID != issue.ID {
			t.Errorf("Expected issue ID %s, got %s", issue.ID, si.IssueID)
		}
		if si.ExecutorStatus != "running" {
			t.Errorf("Expected executor status 'running', got %s", si.ExecutorStatus)
		}
	}

	// Test with longer threshold (15 minutes) - should NOT find the issue
	staleIssues, err = getStaleIssues(ctx, 15*time.Minute)
	if err != nil {
		t.Fatalf("getStaleIssues failed: %v", err)
	}

	if len(staleIssues) != 0 {
		t.Errorf("Expected 0 stale issues with 15m threshold, got %d", len(staleIssues))
	}
}
