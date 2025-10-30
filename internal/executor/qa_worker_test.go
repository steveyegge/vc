package executor

import (
	"context"
	"testing"
	"time"

	"github.com/steveyegge/vc/internal/gates"
	"github.com/steveyegge/vc/internal/labels"
	"github.com/steveyegge/vc/internal/storage"
	"github.com/steveyegge/vc/internal/types"
)

// mockGatesProvider is a mock implementation of GateProvider for testing
type mockGatesProvider struct {
	allPassed bool
	results   []*gates.Result
}

func (m *mockGatesProvider) RunAll(ctx context.Context) ([]*gates.Result, bool) {
	return m.results, m.allPassed
}

// newMockGatesRunner creates a mock gates runner for testing
func newMockGatesRunner(store storage.Storage, allPassed bool) *gates.Runner {
	provider := &mockGatesProvider{
		allPassed: allPassed,
		results: []*gates.Result{
			{Gate: gates.GateTest, Passed: allPassed, Output: "mock test output"},
			{Gate: gates.GateBuild, Passed: allPassed, Output: "mock build output"},
		},
	}
	runner, _ := gates.NewRunner(&gates.Config{
		Store:      store,
		WorkingDir: ".",
		Provider:   provider,
	})
	return runner
}

// TestQAWorkerClaimReadyWork tests the basic claiming logic for QualityGateWorker
func TestQAWorkerClaimReadyWork(t *testing.T) {
	ctx := context.Background()
	store := setupQATestStorage(t, ctx)
	defer store.Close()

	// Create a mission epic
	now := time.Now()
	mission := &types.Mission{
		Issue: types.Issue{
			ID:            "vc-100",
			Title:         "Test Mission",
			Description:   "A test mission for QA worker",
			IssueType:     types.TypeEpic,
			IssueSubtype:  types.SubtypeMission,
			Status:        types.StatusOpen, // Mission epic is open, with needs-quality-gates label
			Priority:      1,
			CreatedAt:     now,
			UpdatedAt:     now,
		},
		Goal:        "Test mission goal",
		Context:     "Test context",
		SandboxPath: "/tmp/test-sandbox",
		BranchName:  "mission/vc-100",
	}

	if err := store.CreateMission(ctx, mission, "test"); err != nil {
		t.Fatalf("Failed to create mission: %v", err)
	}

	// Add needs-quality-gates label
	if err := store.AddLabel(ctx, mission.ID, labels.LabelNeedsQualityGates, "test"); err != nil {
		t.Fatalf("Failed to add label: %v", err)
	}

	// Register executor instance (required for foreign key constraint)
	instance := &types.ExecutorInstance{
		InstanceID:    "test-worker-1",
		Hostname:      "test-host",
		PID:           1234,
		Status:        types.ExecutorStatusRunning,
		StartedAt:     now,
		LastHeartbeat: now,
		Version:       "test",
		Metadata:      "{}",
	}
	if err := store.RegisterInstance(ctx, instance); err != nil {
		t.Fatalf("Failed to register executor instance: %v", err)
	}

	// Create QA worker
	worker, err := NewQualityGateWorker(&QualityGateWorkerConfig{
		Store:       store,
		InstanceID:  "test-worker-1",
		WorkingDir:  ".",
		GatesRunner: newMockGatesRunner(store, true),
	})
	if err != nil {
		t.Fatalf("Failed to create QA worker: %v", err)
	}

	// Claim ready work
	claimed, err := worker.ClaimReadyWork(ctx)
	if err != nil {
		t.Fatalf("Failed to claim work: %v", err)
	}
	if claimed == nil {
		t.Fatal("Expected to claim a mission, got nil")
	}
	if claimed.ID != mission.ID {
		t.Fatalf("Expected to claim mission %s, got %s", mission.ID, claimed.ID)
	}

	// Verify gates-running label was added
	hasLabel, err := labels.HasLabel(ctx, store, mission.ID, labels.LabelGatesRunning)
	if err != nil {
		t.Fatalf("Failed to check label: %v", err)
	}
	if !hasLabel {
		t.Error("Expected gates-running label to be added")
	}

	// Verify issue status is in_progress
	issue, err := store.GetIssue(ctx, mission.ID)
	if err != nil {
		t.Fatalf("Failed to get issue: %v", err)
	}
	if issue.Status != types.StatusInProgress {
		t.Errorf("Expected status in_progress, got %s", issue.Status)
	}

	// Verify execution state was created
	execState, err := store.GetExecutionState(ctx, mission.ID)
	if err != nil {
		t.Fatalf("Failed to get execution state: %v", err)
	}
	if execState == nil {
		t.Fatal("Expected execution state to be created")
	}
	if execState.ExecutorInstanceID != "test-worker-1" {
		t.Errorf("Expected executor instance 'test-worker-1', got %s", execState.ExecutorInstanceID)
	}
}

// TestQAWorkerNoDoubleClaimWithGatesRunning tests that missions with gates-running label are not claimed
func TestQAWorkerNoDoubleClaimWithGatesRunning(t *testing.T) {
	ctx := context.Background()
	store := setupQATestStorage(t, ctx)
	defer store.Close()

	// Create a mission epic
	now := time.Now()
	mission := &types.Mission{
		Issue: types.Issue{
			ID:            "vc-101",
			Title:         "Test Mission 2",
			Description:   "A test mission already claimed",
			IssueType:     types.TypeEpic,
			IssueSubtype:  types.SubtypeMission,
			Status:        types.StatusOpen,
			Priority:      1,
			CreatedAt:     now,
			UpdatedAt:     now,
		},
		Goal:        "Test mission goal",
		Context:     "Test context",
		SandboxPath: "/tmp/test-sandbox-2",
		BranchName:  "mission/vc-101",
	}

	if err := store.CreateMission(ctx, mission, "test"); err != nil {
		t.Fatalf("Failed to create mission: %v", err)
	}

	// Add both labels (needs-quality-gates AND gates-running)
	if err := store.AddLabel(ctx, mission.ID, labels.LabelNeedsQualityGates, "test"); err != nil {
		t.Fatalf("Failed to add needs-quality-gates label: %v", err)
	}
	if err := store.AddLabel(ctx, mission.ID, labels.LabelGatesRunning, "other-worker"); err != nil {
		t.Fatalf("Failed to add gates-running label: %v", err)
	}

	// Create QA worker
	worker, err := NewQualityGateWorker(&QualityGateWorkerConfig{
		Store:       store,
		InstanceID:  "test-worker-2",
		WorkingDir:  ".",
		GatesRunner: newMockGatesRunner(store, true),
	})
	if err != nil {
		t.Fatalf("Failed to create QA worker: %v", err)
	}

	// Attempt to claim ready work - should get nothing
	claimed, err := worker.ClaimReadyWork(ctx)
	if err != nil {
		t.Fatalf("Failed to claim work: %v", err)
	}
	if claimed != nil {
		t.Errorf("Expected no mission to be claimed (already has gates-running), got %s", claimed.ID)
	}
}

// TestQAWorkerIgnoresNonMissions tests that regular tasks with needs-quality-gates are ignored
func TestQAWorkerIgnoresNonMissions(t *testing.T) {
	ctx := context.Background()
	store := setupQATestStorage(t, ctx)
	defer store.Close()

	// Create a regular task (not a mission)
	now := time.Now()
	task := &types.Issue{
		ID:          "vc-102",
		Title:       "Regular task",
		Description: "Not a mission",
		IssueType:   types.TypeTask,
		Status:      types.StatusOpen,
		Priority:    1,
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	if err := store.CreateIssue(ctx, task, "test"); err != nil {
		t.Fatalf("Failed to create task: %v", err)
	}

	// Add needs-quality-gates label (this shouldn't happen in practice, but test it)
	if err := store.AddLabel(ctx, task.ID, labels.LabelNeedsQualityGates, "test"); err != nil {
		t.Fatalf("Failed to add label: %v", err)
	}

	// Create QA worker
	worker, err := NewQualityGateWorker(&QualityGateWorkerConfig{
		Store:       store,
		InstanceID:  "test-worker-3",
		WorkingDir:  ".",
		GatesRunner: newMockGatesRunner(store, true),
	})
	if err != nil {
		t.Fatalf("Failed to create QA worker: %v", err)
	}

	// Attempt to claim ready work - should get nothing (task is not a mission)
	claimed, err := worker.ClaimReadyWork(ctx)
	if err != nil {
		t.Fatalf("Failed to claim work: %v", err)
	}
	if claimed != nil {
		t.Errorf("Expected no mission to be claimed (issue is not a mission), got %s", claimed.ID)
	}
}

// TestQAWorkerClaimPrioritization tests that higher priority missions are claimed first
func TestQAWorkerClaimPrioritization(t *testing.T) {
	ctx := context.Background()
	store := setupQATestStorage(t, ctx)
	defer store.Close()

	// Create low priority mission
	now := time.Now()
	older := now.Add(-2 * time.Hour)
	lowPriorityMission := &types.Mission{
		Issue: types.Issue{
			ID:            "vc-103",
			Title:         "Low Priority Mission",
			Description:   "P2 mission",
			IssueType:     types.TypeEpic,
			IssueSubtype:  types.SubtypeMission,
			Status:        types.StatusOpen, // Mission epic is open, with needs-quality-gates label
			Priority:      2, // Lower priority
			CreatedAt:     older, // Older
			UpdatedAt:     now,
		},
		Goal:        "Low priority goal",
		Context:     "Test context",
		SandboxPath: "/tmp/test-sandbox-low",
		BranchName:  "mission/vc-103",
	}

	// Create high priority mission
	highPriorityMission := &types.Mission{
		Issue: types.Issue{
			ID:            "vc-104",
			Title:         "High Priority Mission",
			Description:   "P0 mission",
			IssueType:     types.TypeEpic,
			IssueSubtype:  types.SubtypeMission,
			Status:        types.StatusOpen, // Mission epic is open, with needs-quality-gates label
			Priority:      0, // Higher priority
			CreatedAt:     now, // Newer
			UpdatedAt:     now,
		},
		Goal:        "High priority goal",
		Context:     "Test context",
		SandboxPath: "/tmp/test-sandbox-high",
		BranchName:  "mission/vc-104",
	}

	// Create missions (low priority first)
	if err := store.CreateMission(ctx, lowPriorityMission, "test"); err != nil {
		t.Fatalf("Failed to create low priority mission: %v", err)
	}
	if err := store.CreateMission(ctx, highPriorityMission, "test"); err != nil {
		t.Fatalf("Failed to create high priority mission: %v", err)
	}

	// Add labels to both
	if err := store.AddLabel(ctx, lowPriorityMission.ID, labels.LabelNeedsQualityGates, "test"); err != nil {
		t.Fatalf("Failed to add label to low priority: %v", err)
	}
	if err := store.AddLabel(ctx, highPriorityMission.ID, labels.LabelNeedsQualityGates, "test"); err != nil {
		t.Fatalf("Failed to add label to high priority: %v", err)
	}

	// Register executor instance (required for foreign key constraint)
	instance := &types.ExecutorInstance{
		InstanceID:    "test-worker-4",
		Hostname:      "test-host",
		PID:           1234,
		Status:        types.ExecutorStatusRunning,
		StartedAt:     now,
		LastHeartbeat: now,
		Version:       "test",
		Metadata:      "{}",
	}
	if err := store.RegisterInstance(ctx, instance); err != nil {
		t.Fatalf("Failed to register executor instance: %v", err)
	}

	// Create QA worker
	worker, err := NewQualityGateWorker(&QualityGateWorkerConfig{
		Store:       store,
		InstanceID:  "test-worker-4",
		WorkingDir:  ".",
		GatesRunner: newMockGatesRunner(store, true),
	})
	if err != nil {
		t.Fatalf("Failed to create QA worker: %v", err)
	}

	// Claim ready work - should get high priority mission first
	claimed, err := worker.ClaimReadyWork(ctx)
	if err != nil {
		t.Fatalf("Failed to claim work: %v", err)
	}
	if claimed == nil {
		t.Fatal("Expected to claim a mission, got nil")
	}
	if claimed.ID != highPriorityMission.ID {
		t.Errorf("Expected to claim high priority mission %s, got %s", highPriorityMission.ID, claimed.ID)
	}
}

// TestQAWorkerRequiresGatesRunner tests that NewQualityGateWorker validates gatesRunner is provided (vc-258)
func TestQAWorkerRequiresGatesRunner(t *testing.T) {
	ctx := context.Background()
	store := setupQATestStorage(t, ctx)
	defer store.Close()

	// Attempt to create QA worker without GatesRunner - should fail
	_, err := NewQualityGateWorker(&QualityGateWorkerConfig{
		Store:      store,
		InstanceID: "test-worker-nil",
		WorkingDir: ".",
		// GatesRunner is intentionally nil
	})

	if err == nil {
		t.Fatal("Expected error when creating QA worker without GatesRunner, got nil")
	}

	expectedError := "gates runner is required"
	if err.Error() != expectedError {
		t.Errorf("Expected error %q, got %q", expectedError, err.Error())
	}
}

// TestQAWorkerExecuteSuccess tests successful gate execution and state transitions (vc-253)
func TestQAWorkerExecuteSuccess(t *testing.T) {
	ctx := context.Background()
	store := setupQATestStorage(t, ctx)
	defer store.Close()

	// Create a mission
	now := time.Now()
	mission := &types.Mission{
		Issue: types.Issue{
			ID:            "vc-200",
			Title:         "Test Mission for Execute",
			Description:   "Test successful gate execution",
			IssueType:     types.TypeEpic,
			IssueSubtype:  types.SubtypeMission,
			Status:        types.StatusOpen, // Must be open to claim
			Priority:      1,
			CreatedAt:     now,
			UpdatedAt:     now,
		},
		Goal:        "Test mission goal",
		Context:     "Test context",
		SandboxPath: "/tmp/test-sandbox-execute",
		BranchName:  "mission/vc-200",
	}

	if err := store.CreateMission(ctx, mission, "test"); err != nil {
		t.Fatalf("Failed to create mission: %v", err)
	}

	// Add needs-quality-gates label
	if err := store.AddLabel(ctx, mission.ID, labels.LabelNeedsQualityGates, "test"); err != nil {
		t.Fatalf("Failed to add needs-quality-gates label: %v", err)
	}

	// Register executor instance
	instance := &types.ExecutorInstance{
		InstanceID:    "test-worker-5",
		Hostname:      "test-host",
		PID:           1234,
		Status:        types.ExecutorStatusRunning,
		StartedAt:     now,
		LastHeartbeat: now,
		Version:       "test",
		Metadata:      "{}",
	}
	if err := store.RegisterInstance(ctx, instance); err != nil {
		t.Fatalf("Failed to register executor instance: %v", err)
	}

	// Claim the issue (this sets status to in_progress and creates execution state)
	if err := store.ClaimIssue(ctx, mission.ID, "test-worker-5"); err != nil {
		t.Fatalf("Failed to claim issue: %v", err)
	}

	// Add gates-running label (simulating claimed state for QA worker)
	if err := store.AddLabel(ctx, mission.ID, labels.LabelGatesRunning, "test-worker-5"); err != nil {
		t.Fatalf("Failed to add gates-running label: %v", err)
	}

	// Create QA worker with passing gates
	worker, err := NewQualityGateWorker(&QualityGateWorkerConfig{
		Store:       store,
		InstanceID:  "test-worker-5",
		WorkingDir:  ".",
		GatesRunner: newMockGatesRunner(store, true), // All gates pass
	})
	if err != nil {
		t.Fatalf("Failed to create QA worker: %v", err)
	}

	// Execute gates
	if err := worker.Execute(ctx, &mission.Issue); err != nil {
		t.Fatalf("Failed to execute gates: %v", err)
	}

	// Verify gates-running label was removed
	hasGatesRunning, err := labels.HasLabel(ctx, store, mission.ID, labels.LabelGatesRunning)
	if err != nil {
		t.Fatalf("Failed to check gates-running label: %v", err)
	}
	if hasGatesRunning {
		t.Error("Expected gates-running label to be removed")
	}

	// Verify needs-quality-gates label was removed
	hasNeedsQG, err := labels.HasLabel(ctx, store, mission.ID, labels.LabelNeedsQualityGates)
	if err != nil {
		t.Fatalf("Failed to check needs-quality-gates label: %v", err)
	}
	if hasNeedsQG {
		t.Error("Expected needs-quality-gates label to be removed")
	}

	// Verify needs-review label was added
	hasNeedsReview, err := labels.HasLabel(ctx, store, mission.ID, labels.LabelNeedsReview)
	if err != nil {
		t.Fatalf("Failed to check needs-review label: %v", err)
	}
	if !hasNeedsReview {
		t.Error("Expected needs-review label to be added")
	}

	// Verify mission status is open (ready for next stage)
	issue, err := store.GetIssue(ctx, mission.ID)
	if err != nil {
		t.Fatalf("Failed to get issue: %v", err)
	}
	if issue.Status != types.StatusOpen {
		t.Errorf("Expected status open, got %s", issue.Status)
	}

	// Verify execution state was released
	execState, err := store.GetExecutionState(ctx, mission.ID)
	if err != nil {
		t.Fatalf("Failed to get execution state: %v", err)
	}
	if execState != nil {
		t.Error("Expected execution state to be released")
	}
}

// TestQAWorkerExecuteFailure tests failed gate execution and blocking issue creation (vc-253)
func TestQAWorkerExecuteFailure(t *testing.T) {
	ctx := context.Background()
	store := setupQATestStorage(t, ctx)
	defer store.Close()

	// Create a mission
	now := time.Now()
	mission := &types.Mission{
		Issue: types.Issue{
			ID:            "vc-201",
			Title:         "Test Mission for Execute Failure",
			Description:   "Test failed gate execution",
			IssueType:     types.TypeEpic,
			IssueSubtype:  types.SubtypeMission,
			Status:        types.StatusOpen, // Must be open to claim
			Priority:      1,
			CreatedAt:     now,
			UpdatedAt:     now,
		},
		Goal:        "Test mission goal",
		Context:     "Test context",
		SandboxPath: "/tmp/test-sandbox-execute-fail",
		BranchName:  "mission/vc-201",
	}

	if err := store.CreateMission(ctx, mission, "test"); err != nil {
		t.Fatalf("Failed to create mission: %v", err)
	}

	// Add needs-quality-gates label
	if err := store.AddLabel(ctx, mission.ID, labels.LabelNeedsQualityGates, "test"); err != nil {
		t.Fatalf("Failed to add needs-quality-gates label: %v", err)
	}

	// Register executor instance
	instance := &types.ExecutorInstance{
		InstanceID:    "test-worker-6",
		Hostname:      "test-host",
		PID:           1234,
		Status:        types.ExecutorStatusRunning,
		StartedAt:     now,
		LastHeartbeat: now,
		Version:       "test",
		Metadata:      "{}",
	}
	if err := store.RegisterInstance(ctx, instance); err != nil {
		t.Fatalf("Failed to register executor instance: %v", err)
	}

	// Claim the issue (this sets status to in_progress and creates execution state)
	if err := store.ClaimIssue(ctx, mission.ID, "test-worker-6"); err != nil {
		t.Fatalf("Failed to claim issue: %v", err)
	}

	// Add gates-running label (simulating claimed state for QA worker)
	if err := store.AddLabel(ctx, mission.ID, labels.LabelGatesRunning, "test-worker-6"); err != nil {
		t.Fatalf("Failed to add gates-running label: %v", err)
	}

	// Create QA worker with failing gates
	worker, err := NewQualityGateWorker(&QualityGateWorkerConfig{
		Store:       store,
		InstanceID:  "test-worker-6",
		WorkingDir:  ".",
		GatesRunner: newMockGatesRunner(store, false), // Gates fail
	})
	if err != nil {
		t.Fatalf("Failed to create QA worker: %v", err)
	}

	// Execute gates
	if err := worker.Execute(ctx, &mission.Issue); err != nil {
		t.Fatalf("Failed to execute gates: %v", err)
	}

	// Verify gates-running label was removed
	hasGatesRunning, err := labels.HasLabel(ctx, store, mission.ID, labels.LabelGatesRunning)
	if err != nil {
		t.Fatalf("Failed to check gates-running label: %v", err)
	}
	if hasGatesRunning {
		t.Error("Expected gates-running label to be removed")
	}

	// Verify needs-quality-gates label is still present (for retry)
	hasNeedsQG, err := labels.HasLabel(ctx, store, mission.ID, labels.LabelNeedsQualityGates)
	if err != nil {
		t.Fatalf("Failed to check needs-quality-gates label: %v", err)
	}
	if !hasNeedsQG {
		t.Error("Expected needs-quality-gates label to remain for retry")
	}

	// Verify gates-failed label was added
	hasGatesFailed, err := labels.HasLabel(ctx, store, mission.ID, labels.LabelGatesFailed)
	if err != nil {
		t.Fatalf("Failed to check gates-failed label: %v", err)
	}
	if !hasGatesFailed {
		t.Error("Expected gates-failed label to be added")
	}

	// Verify mission status is blocked
	issue, err := store.GetIssue(ctx, mission.ID)
	if err != nil {
		t.Fatalf("Failed to get issue: %v", err)
	}
	if issue.Status != types.StatusBlocked {
		t.Errorf("Expected status blocked, got %s", issue.Status)
	}

	// Verify execution state was released
	execState, err := store.GetExecutionState(ctx, mission.ID)
	if err != nil {
		t.Fatalf("Failed to get execution state: %v", err)
	}
	if execState != nil {
		t.Error("Expected execution state to be released")
	}

	// Verify blocking issues were created
	// The mock gates have 2 gates (test and build), both failing
	// So we should have 2 blocking issues
	expectedBlockingIssue1 := mission.ID + "-gate-test"
	expectedBlockingIssue2 := mission.ID + "-gate-build"

	blockingIssue1, err := store.GetIssue(ctx, expectedBlockingIssue1)
	if err != nil {
		t.Fatalf("Failed to get blocking issue 1: %v", err)
	}
	if blockingIssue1 == nil {
		t.Error("Expected blocking issue for test gate to be created")
	}

	blockingIssue2, err := store.GetIssue(ctx, expectedBlockingIssue2)
	if err != nil {
		t.Fatalf("Failed to get blocking issue 2: %v", err)
	}
	if blockingIssue2 == nil {
		t.Error("Expected blocking issue for build gate to be created")
	}

	// Verify blocking dependencies exist
	deps, err := store.GetDependencies(ctx, mission.ID)
	if err != nil {
		t.Fatalf("Failed to get dependencies: %v", err)
	}
	if len(deps) < 2 {
		t.Errorf("Expected at least 2 blocking dependencies, got %d", len(deps))
	}
}

// setupQATestStorage creates an in-memory storage for testing
func setupQATestStorage(t *testing.T, ctx context.Context) storage.Storage {
	t.Helper()

	cfg := storage.DefaultConfig()
	cfg.Path = ":memory:"

	store, err := storage.NewStorage(ctx, cfg)
	if err != nil {
		t.Fatalf("Failed to create test storage: %v", err)
	}

	return store
}
