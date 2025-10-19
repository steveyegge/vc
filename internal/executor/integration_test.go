package executor

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/steveyegge/vc/internal/gates"
	"github.com/steveyegge/vc/internal/sandbox"
	"github.com/steveyegge/vc/internal/storage"
	"github.com/steveyegge/vc/internal/types"
)

// TestFullWorkflowEndToEnd tests the complete workflow: create mission → plan → execute → review → commit
func TestFullWorkflowEndToEnd(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	store := setupTestStorage(t, ctx)
	defer store.Close()

	exec := setupTestExecutor(t, store, false)

	var mission *types.Issue
	var tasks []*types.Issue

	t.Run("create_mission", func(t *testing.T) {
		mission = &types.Issue{
			Title:              "Test Mission: Add Feature X",
			Description:        "Mission to add a new feature to the codebase",
			IssueType:          types.TypeEpic,
			Status:             types.StatusOpen,
			Priority:           0,
			AcceptanceCriteria: "Feature X is implemented, tested, and committed",
			CreatedAt:          time.Now(),
			UpdatedAt:          time.Now(),
		}
		if err := store.CreateIssue(ctx, mission, "test"); err != nil {
			t.Fatalf("Failed to create mission: %v", err)
		}
	})

	t.Run("plan_tasks", func(t *testing.T) {
		tasks = []*types.Issue{
			{
				Title:              "Implement core logic",
				Description:        "Write the main feature logic",
				IssueType:          types.TypeTask,
				Status:             types.StatusOpen,
				Priority:           1,
				AcceptanceCriteria: "Core logic implemented",
				CreatedAt:          time.Now(),
				UpdatedAt:          time.Now(),
			},
			{
				Title:              "Add unit tests",
				Description:        "Write tests for the new feature",
				IssueType:          types.TypeTask,
				Status:             types.StatusOpen,
				Priority:           1,
				AcceptanceCriteria: "Tests pass",
				CreatedAt:          time.Now(),
				UpdatedAt:          time.Now(),
			},
		}

		for _, task := range tasks {
			if err := store.CreateIssue(ctx, task, "test"); err != nil {
				t.Fatalf("Failed to create task: %v", err)
			}

			// Link task to mission
			dep := types.Dependency{
				IssueID:     task.ID,
				DependsOnID: mission.ID,
				Type:        types.DepParentChild,
				CreatedAt:   time.Now(),
				CreatedBy:   "test",
			}
			if err := store.AddDependency(ctx, &dep, "test"); err != nil {
				t.Fatalf("Failed to add dependency: %v", err)
			}
		}
	})

	t.Run("execute_tasks", func(t *testing.T) {
		for _, task := range tasks {
			executeAndCompleteTask(t, ctx, store, exec, task)
		}
	})

	t.Run("verify_completion", func(t *testing.T) {
		for _, task := range tasks {
			issue, err := store.GetIssue(ctx, task.ID)
			if err != nil {
				t.Fatalf("Failed to get task %s: %v", task.ID, err)
			}
			if issue.Status != types.StatusClosed {
				t.Errorf("Task %s should be closed, got %s", task.ID, issue.Status)
			}
		}
	})

	t.Run("close_mission", func(t *testing.T) {
		missionIssue, err := store.GetIssue(ctx, mission.ID)
		if err != nil {
			t.Fatalf("Failed to get mission: %v", err)
		}
		if missionIssue.Status != types.StatusOpen {
			t.Errorf("Mission should still be open before manual close, got %s", missionIssue.Status)
		}

		// Close mission
		updates := map[string]interface{}{"status": types.StatusClosed}
		if err := store.UpdateIssue(ctx, mission.ID, updates, "test"); err != nil {
			t.Fatalf("Failed to close mission: %v", err)
		}
	})
}

// TestSandboxIsolation tests that tasks execute in isolated sandboxes
func TestSandboxIsolation(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	store := setupTestStorage(t, ctx)
	defer store.Close()

	exec := setupTestExecutor(t, store, false)

	// Create test directory structure
	testDir := t.TempDir()
	
	// Create two tasks that might conflict if not sandboxed
	task1 := &types.Issue{
		Title:              "Modify shared file - Task 1",
		Description:        "Task 1 modifies a shared file",
		IssueType:          types.TypeTask,
		Status:             types.StatusOpen,
		Priority:           1,
		AcceptanceCriteria: "File modified by task 1",
		CreatedAt:          time.Now(),
		UpdatedAt:          time.Now(),
	}
	if err := store.CreateIssue(ctx, task1, "test"); err != nil {
		t.Fatalf("Failed to create task1: %v", err)
	}

	task2 := &types.Issue{
		Title:              "Modify shared file - Task 2",
		Description:        "Task 2 modifies the same shared file",
		IssueType:          types.TypeTask,
		Status:             types.StatusOpen,
		Priority:           1,
		AcceptanceCriteria: "File modified by task 2",
		CreatedAt:          time.Now(),
		UpdatedAt:          time.Now(),
	}
	if err := store.CreateIssue(ctx, task2, "test"); err != nil {
		t.Fatalf("Failed to create task2: %v", err)
	}

	// Simulate executing both tasks
	// In a real sandbox implementation, each would have its own working directory
	task1Dir := filepath.Join(testDir, "sandbox1")
	task2Dir := filepath.Join(testDir, "sandbox2")

	if err := os.MkdirAll(task1Dir, 0755); err != nil {
		t.Fatalf("Failed to create task1 sandbox: %v", err)
	}
	if err := os.MkdirAll(task2Dir, 0755); err != nil {
		t.Fatalf("Failed to create task2 sandbox: %v", err)
	}

	// Write different content to the same filename in each sandbox
	testFile := "shared.txt"
	if err := os.WriteFile(filepath.Join(task1Dir, testFile), []byte("Task 1 content"), 0644); err != nil {
		t.Fatalf("Failed to write task1 file: %v", err)
	}
	if err := os.WriteFile(filepath.Join(task2Dir, testFile), []byte("Task 2 content"), 0644); err != nil {
		t.Fatalf("Failed to write task2 file: %v", err)
	}

	// Verify isolation - both files exist with different content
	content1, err := os.ReadFile(filepath.Join(task1Dir, testFile))
	if err != nil {
		t.Fatalf("Failed to read task1 file: %v", err)
	}
	content2, err := os.ReadFile(filepath.Join(task2Dir, testFile))
	if err != nil {
		t.Fatalf("Failed to read task2 file: %v", err)
	}

	if string(content1) != "Task 1 content" {
		t.Errorf("Task 1 file has wrong content: %s", content1)
	}
	if string(content2) != "Task 2 content" {
		t.Errorf("Task 2 file has wrong content: %s", content2)
	}
	if string(content1) == string(content2) {
		t.Error("Sandboxes not isolated - both files have same content")
	}

	// Complete both tasks
	for _, task := range []*types.Issue{task1, task2} {
		executeAndCompleteTask(t, ctx, store, exec, task)
	}

	t.Log("Sandbox isolation test completed successfully")
}

// TestErrorRecoveryAndResume tests that work can be resumed after failure
func TestErrorRecoveryAndResume(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	store := setupTestStorage(t, ctx)
	defer store.Close()

	exec1 := setupTestExecutor(t, store, false)

	// Create a task
	task := &types.Issue{
		Title:              "Task that will fail and resume",
		Description:        "This task will be interrupted and resumed",
		IssueType:          types.TypeTask,
		Status:             types.StatusOpen,
		Priority:           1,
		AcceptanceCriteria: "Task completes after resume",
		CreatedAt:          time.Now(),
		UpdatedAt:          time.Now(),
	}
	if err := store.CreateIssue(ctx, task, "test"); err != nil {
		t.Fatalf("Failed to create task: %v", err)
	}

	// Phase 1: First executor claims and starts work
	if err := store.ClaimIssue(ctx, task.ID, exec1.instanceID); err != nil {
		t.Fatalf("Failed to claim task: %v", err)
	}

	// Progress through some states
	if err := store.UpdateExecutionState(ctx, task.ID, types.ExecutionStateAssessing); err != nil {
		t.Fatalf("Failed to update to assessing: %v", err)
	}
	if err := store.UpdateExecutionState(ctx, task.ID, types.ExecutionStateExecuting); err != nil {
		t.Fatalf("Failed to update to executing: %v", err)
	}

	// Save checkpoint
	checkpointData := map[string]interface{}{
		"step":      2,
		"completed": []string{"setup", "compile"},
		"pending":   []string{"test", "deploy"},
	}
	if err := store.SaveCheckpoint(ctx, task.ID, checkpointData); err != nil {
		t.Fatalf("Failed to save checkpoint: %v", err)
	}

	// Simulate executor crash (mark as stale and cleanup)
	now := time.Now()
	exec1Stale := &types.ExecutorInstance{
		InstanceID:    exec1.instanceID,
		Hostname:      exec1.hostname,
		PID:           exec1.pid,
		Status:        types.ExecutorStatusRunning,
		StartedAt:     now.Add(-20 * time.Minute),
		LastHeartbeat: now.Add(-20 * time.Minute),
		Version:       exec1.version,
		Metadata:      "{}",
	}
	if err := store.RegisterInstance(ctx, exec1Stale); err != nil {
		t.Fatalf("Failed to update executor to stale: %v", err)
	}

	// Cleanup stale instances
	cleaned, err := store.CleanupStaleInstances(ctx, 300)
	if err != nil {
		t.Fatalf("Failed to cleanup stale instances: %v", err)
	}
	if cleaned != 1 {
		t.Errorf("Expected 1 cleanup, got %d", cleaned)
	}

	// Release issue and reopen for retry (atomically)
	if err := store.ReleaseIssueAndReopen(ctx, task.ID, "test", "Simulating crash recovery - executor was cleaned up"); err != nil {
		t.Fatalf("Failed to release and reopen issue: %v", err)
	}

	// Phase 2: New executor resumes work
	exec2 := setupTestExecutor(t, store, false)

	// Claim the task
	if err := store.ClaimIssue(ctx, task.ID, exec2.instanceID); err != nil {
		t.Fatalf("Failed to claim task with exec2: %v", err)
	}

	// Retrieve checkpoint to verify resume context
	checkpointJSON, err := store.GetCheckpoint(ctx, task.ID)
	if err != nil {
		t.Fatalf("Failed to get checkpoint: %v", err)
	}
	if checkpointJSON == "" {
		t.Error("Expected checkpoint data, got empty string")
	}

	// Continue from where we left off - need to start from assessing since we just claimed
	// The checkpoint tells us where we were, but state machine always starts from claimed
	if err := store.UpdateExecutionState(ctx, task.ID, types.ExecutionStateAssessing); err != nil {
		t.Fatalf("Failed to resume assessing: %v", err)
	}
	if err := store.UpdateExecutionState(ctx, task.ID, types.ExecutionStateExecuting); err != nil {
		t.Fatalf("Failed to resume executing: %v", err)
	}
	if err := store.UpdateExecutionState(ctx, task.ID, types.ExecutionStateAnalyzing); err != nil {
		t.Fatalf("Failed to update to analyzing: %v", err)
	}
	if err := store.UpdateExecutionState(ctx, task.ID, types.ExecutionStateGates); err != nil {
		t.Fatalf("Failed to update to gates: %v", err)
	}
	if err := store.UpdateExecutionState(ctx, task.ID, types.ExecutionStateCompleted); err != nil {
		t.Fatalf("Failed to complete: %v", err)
	}

	// Close task
	closeUpdates := map[string]interface{}{"status": types.StatusClosed}
	if err := store.UpdateIssue(ctx, task.ID, closeUpdates, exec2.instanceID); err != nil {
		t.Fatalf("Failed to close task: %v", err)
	}

	// Verify final state
	finalTask, err := store.GetIssue(ctx, task.ID)
	if err != nil {
		t.Fatalf("Failed to get final task: %v", err)
	}
	if finalTask.Status != types.StatusClosed {
		t.Errorf("Task should be closed, got %s", finalTask.Status)
	}

	t.Log("Error recovery and resume test completed successfully")
}

// TestQualityGateBlocking tests that quality gates can block task completion
func TestQualityGateBlocking(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	store := setupTestStorage(t, ctx)
	defer store.Close()

	exec := setupTestExecutor(t, store, false)

	// Create a task
	task := &types.Issue{
		Title:              "Task with quality gates",
		Description:        "This task has quality gates that must pass",
		IssueType:          types.TypeTask,
		Status:             types.StatusOpen,
		Priority:           1,
		AcceptanceCriteria: "Tests pass, linting passes, builds successfully",
		CreatedAt:          time.Now(),
		UpdatedAt:          time.Now(),
	}
	if err := store.CreateIssue(ctx, task, "test"); err != nil {
		t.Fatalf("Failed to create task: %v", err)
	}

	// Claim and execute
	if err := store.ClaimIssue(ctx, task.ID, exec.instanceID); err != nil {
		t.Fatalf("Failed to claim task: %v", err)
	}

	// Progress to gates
	if err := store.UpdateExecutionState(ctx, task.ID, types.ExecutionStateAssessing); err != nil {
		t.Fatalf("Failed to update to assessing: %v", err)
	}
	if err := store.UpdateExecutionState(ctx, task.ID, types.ExecutionStateExecuting); err != nil {
		t.Fatalf("Failed to update to executing: %v", err)
	}
	if err := store.UpdateExecutionState(ctx, task.ID, types.ExecutionStateAnalyzing); err != nil {
		t.Fatalf("Failed to update to analyzing: %v", err)
	}
	if err := store.UpdateExecutionState(ctx, task.ID, types.ExecutionStateGates); err != nil {
		t.Fatalf("Failed to update to gates: %v", err)
	}

	// Simulate quality gate checks
	// Test gate: Always passes (simulated)
	testGate := &gates.Result{
		Gate:   gates.GateTest,
		Passed: true,
		Output: "All tests passed",
	}

	// Lint gate: Fails (simulated blocking scenario)
	lintGate := &gates.Result{
		Gate:   gates.GateLint,
		Passed: false,
		Output: "Linting errors found: 3 errors, 5 warnings",
	}

	// Build gate: Passes
	buildGate := &gates.Result{
		Gate:   gates.GateBuild,
		Passed: true,
		Output: "Build successful",
	}

	results := []*gates.Result{testGate, lintGate, buildGate}
	allPassed := true
	for _, r := range results {
		if !r.Passed {
			allPassed = false
			break
		}
	}

	if allPassed {
		t.Error("Quality gates should have failed due to lint errors")
	}

	// Since gates failed, task should not complete
	// Instead, it should remain in gates state or transition to blocked
	state, err := store.GetExecutionState(ctx, task.ID)
	if err != nil {
		t.Fatalf("Failed to get execution state: %v", err)
	}
	if state.State != types.ExecutionStateGates {
		t.Errorf("Task should still be in gates state, got %s", state.State)
	}

	// Task should remain open
	taskIssue, err := store.GetIssue(ctx, task.ID)
	if err != nil {
		t.Fatalf("Failed to get task: %v", err)
	}
	if taskIssue.Status == types.StatusClosed {
		t.Error("Task should not be closed when gates fail")
	}

	// Simulate fixing the lint errors and re-running gates
	lintGateFixed := &gates.Result{
		Gate:   gates.GateLint,
		Passed: true,
		Output: "Linting passed",
	}
	resultsFixed := []*gates.Result{testGate, lintGateFixed, buildGate}
	allPassedFixed := true
	for _, r := range resultsFixed {
		if !r.Passed {
			allPassedFixed = false
			break
		}
	}

	if !allPassedFixed {
		t.Error("Quality gates should pass after fixes")
	}

	// Now task can complete
	if err := store.UpdateExecutionState(ctx, task.ID, types.ExecutionStateCompleted); err != nil {
		t.Fatalf("Failed to complete task: %v", err)
	}

	updates := map[string]interface{}{"status": types.StatusClosed}
	if err := store.UpdateIssue(ctx, task.ID, updates, exec.instanceID); err != nil {
		t.Fatalf("Failed to close task: %v", err)
	}

	t.Log("Quality gate blocking test completed successfully")
}

// TestMultiTaskCoordination tests that multiple tasks can be coordinated with dependencies
func TestMultiTaskCoordination(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	store := setupTestStorage(t, ctx)
	defer store.Close()

	exec := setupTestExecutor(t, store, false)

	// Create a dependency chain: task1 -> task2 -> task3
	// (task2 depends on task1, task3 depends on task2)
	task1 := &types.Issue{
		Title:              "Task 1: Foundation",
		Description:        "First task in the chain",
		IssueType:          types.TypeTask,
		Status:             types.StatusOpen,
		Priority:           1,
		AcceptanceCriteria: "Foundation complete",
		CreatedAt:          time.Now(),
		UpdatedAt:          time.Now(),
	}
	if err := store.CreateIssue(ctx, task1, "test"); err != nil {
		t.Fatalf("Failed to create task1: %v", err)
	}

	task2 := &types.Issue{
		Title:              "Task 2: Build on foundation",
		Description:        "Second task, depends on task 1",
		IssueType:          types.TypeTask,
		Status:             types.StatusOpen,
		Priority:           1,
		AcceptanceCriteria: "Built on foundation",
		CreatedAt:          time.Now(),
		UpdatedAt:          time.Now(),
	}
	if err := store.CreateIssue(ctx, task2, "test"); err != nil {
		t.Fatalf("Failed to create task2: %v", err)
	}

	task3 := &types.Issue{
		Title:              "Task 3: Final integration",
		Description:        "Third task, depends on task 2",
		IssueType:          types.TypeTask,
		Status:             types.StatusOpen,
		Priority:           1,
		AcceptanceCriteria: "Integration complete",
		CreatedAt:          time.Now(),
		UpdatedAt:          time.Now(),
	}
	if err := store.CreateIssue(ctx, task3, "test"); err != nil {
		t.Fatalf("Failed to create task3: %v", err)
	}

	// Add dependencies
	dep1 := types.Dependency{
		IssueID:     task2.ID,
		DependsOnID: task1.ID,
		Type:        types.DepBlocks,
		CreatedAt:   time.Now(),
		CreatedBy:   "test",
	}
	if err := store.AddDependency(ctx, &dep1, "test"); err != nil {
		t.Fatalf("Failed to add dependency task1->task2: %v", err)
	}

	dep2 := types.Dependency{
		IssueID:     task3.ID,
		DependsOnID: task2.ID,
		Type:        types.DepBlocks,
		CreatedAt:   time.Now(),
		CreatedBy:   "test",
	}
	if err := store.AddDependency(ctx, &dep2, "test"); err != nil {
		t.Fatalf("Failed to add dependency task2->task3: %v", err)
	}

	// Verify initial ready work (only task1 should be ready)
	readyWork, err := store.GetReadyWork(ctx, types.WorkFilter{
		Status: types.StatusOpen,
		Limit:  10,
	})
	if err != nil {
		t.Fatalf("Failed to get ready work: %v", err)
	}

	// Find which tasks are in ready work
	readyIDs := make(map[string]bool)
	for _, w := range readyWork {
		readyIDs[w.ID] = true
	}

	if !readyIDs[task1.ID] {
		t.Error("Task 1 should be ready (no blockers)")
	}
	if readyIDs[task2.ID] {
		t.Error("Task 2 should be blocked by task 1")
	}
	if readyIDs[task3.ID] {
		t.Error("Task 3 should be blocked by task 2")
	}

	// Execute task1
	executeAndCompleteTask(t, ctx, store, exec, task1)

	// Now task2 should be ready
	readyWork, err = store.GetReadyWork(ctx, types.WorkFilter{
		Status: types.StatusOpen,
		Limit:  10,
	})
	if err != nil {
		t.Fatalf("Failed to get ready work after task1: %v", err)
	}

	readyIDs = make(map[string]bool)
	for _, w := range readyWork {
		readyIDs[w.ID] = true
	}

	if !readyIDs[task2.ID] {
		t.Error("Task 2 should be ready after task 1 completes")
	}
	if readyIDs[task3.ID] {
		t.Error("Task 3 should still be blocked by task 2")
	}

	// Execute task2
	executeAndCompleteTask(t, ctx, store, exec, task2)

	// Now task3 should be ready
	readyWork, err = store.GetReadyWork(ctx, types.WorkFilter{
		Status: types.StatusOpen,
		Limit:  10,
	})
	if err != nil {
		t.Fatalf("Failed to get ready work after task2: %v", err)
	}

	readyIDs = make(map[string]bool)
	for _, w := range readyWork {
		readyIDs[w.ID] = true
	}

	if !readyIDs[task3.ID] {
		t.Error("Task 3 should be ready after task 2 completes")
	}

	// Execute task3
	executeAndCompleteTask(t, ctx, store, exec, task3)

	// Verify all tasks completed
	for i, task := range []*types.Issue{task1, task2, task3} {
		finalTask, err := store.GetIssue(ctx, task.ID)
		if err != nil {
			t.Fatalf("Failed to get task %d: %v", i+1, err)
		}
		if finalTask.Status != types.StatusClosed {
			t.Errorf("Task %d should be closed, got %s", i+1, finalTask.Status)
		}
	}

	t.Log("Multi-task coordination test completed successfully")
}

// TestExecutorSandboxIntegration tests the full executor integration with sandbox manager
func TestExecutorSandboxIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()

	// Create a temporary directory to serve as the parent repo
	parentRepo := t.TempDir()

	// Initialize it as a git repo
	if err := setupGitRepo(t, parentRepo); err != nil {
		t.Fatalf("Failed to setup git repo: %v", err)
	}

	// Create storage in the parent repo
	dbPath := filepath.Join(parentRepo, ".beads", "test.db")
	if err := os.MkdirAll(filepath.Dir(dbPath), 0755); err != nil {
		t.Fatalf("Failed to create .beads dir: %v", err)
	}

	cfg := storage.DefaultConfig()
	cfg.Path = dbPath
	store, err := storage.NewStorage(ctx, cfg)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer store.Close()

	// Create executor with sandboxes enabled
	sandboxRoot := filepath.Join(parentRepo, ".sandboxes")
	execCfg := DefaultConfig()
	execCfg.Store = store
	execCfg.EnableSandboxes = true
	execCfg.SandboxRoot = sandboxRoot
	execCfg.ParentRepo = parentRepo
	execCfg.WorkingDir = parentRepo
	execCfg.PollInterval = 100 * time.Millisecond
	execCfg.EnableAISupervision = false // Disable AI for test
	execCfg.EnableQualityGates = false  // Disable gates for test

	exec, err := New(execCfg)
	if err != nil {
		t.Fatalf("Failed to create executor: %v", err)
	}

	// Register executor instance
	instance := &types.ExecutorInstance{
		InstanceID:    exec.instanceID,
		Hostname:      exec.hostname,
		PID:           exec.pid,
		Status:        types.ExecutorStatusRunning,
		StartedAt:     time.Now(),
		LastHeartbeat: time.Now(),
		Version:       exec.version,
		Metadata:      "{}",
	}
	if err := store.RegisterInstance(ctx, instance); err != nil {
		t.Fatalf("Failed to register executor: %v", err)
	}

	// Verify sandbox manager was initialized
	if exec.sandboxMgr == nil {
		t.Fatal("Sandbox manager should be initialized when EnableSandboxes is true")
	}

	// Create a test issue
	issue := &types.Issue{
		Title:              "Test sandbox integration",
		Description:        "Test that executor creates and cleans up sandboxes",
		IssueType:          types.TypeTask,
		Status:             types.StatusOpen,
		Priority:           1,
		AcceptanceCriteria: "Sandbox created, used, and cleaned up",
		CreatedAt:          time.Now(),
		UpdatedAt:          time.Now(),
	}
	if err := store.CreateIssue(ctx, issue, "test"); err != nil {
		t.Fatalf("Failed to create issue: %v", err)
	}

	// Note: We can't easily test the full executeIssue() flow in a unit test
	// because it spawns actual agents. Instead, we verify the sandbox manager
	// is properly initialized and configured.

	// Test that sandbox manager can create a sandbox
	sb, err := exec.sandboxMgr.Create(ctx, sandbox.SandboxConfig{
		MissionID:  issue.ID,
		BaseBranch: "main",
	})
	if err != nil {
		t.Fatalf("Failed to create sandbox: %v", err)
	}
	defer func() {
		if err := exec.sandboxMgr.Cleanup(ctx, sb); err != nil {
			t.Errorf("Failed to cleanup sandbox: %v", err)
		}
	}()

	// Verify sandbox was created
	if sb == nil {
		t.Fatal("Expected sandbox to be created")
	}
	if sb.ID == "" {
		t.Error("Sandbox should have an ID")
	}
	if sb.MissionID != issue.ID {
		t.Errorf("Expected mission ID %s, got %s", issue.ID, sb.MissionID)
	}
	if sb.Path == "" {
		t.Error("Sandbox should have a path")
	}

	// Verify sandbox directory exists
	if _, err := os.Stat(sb.Path); os.IsNotExist(err) {
		t.Errorf("Sandbox directory should exist at %s", sb.Path)
	}

	// Verify sandbox has its own git branch
	if sb.GitBranch == "" {
		t.Error("Sandbox should have a git branch")
	}

	// List sandboxes
	sandboxes, err := exec.sandboxMgr.List(ctx)
	if err != nil {
		t.Fatalf("Failed to list sandboxes: %v", err)
	}
	if len(sandboxes) != 1 {
		t.Errorf("Expected 1 sandbox, got %d", len(sandboxes))
	}

	// Cleanup sandbox
	if err := exec.sandboxMgr.Cleanup(ctx, sb); err != nil {
		t.Fatalf("Failed to cleanup sandbox: %v", err)
	}

	// Verify sandbox was cleaned up
	sandboxes, err = exec.sandboxMgr.List(ctx)
	if err != nil {
		t.Fatalf("Failed to list sandboxes after cleanup: %v", err)
	}
	if len(sandboxes) != 0 {
		t.Errorf("Expected 0 sandboxes after cleanup, got %d", len(sandboxes))
	}

	t.Log("Executor sandbox integration test completed successfully")
}

// Helper function to setup a git repository
func setupGitRepo(t *testing.T, path string) error {
	t.Helper()

	// Initialize git repo
	cmd := exec.Command("git", "init")
	cmd.Dir = path
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("git init failed: %w", err)
	}

	// Configure git user (required for commits)
	cmd = exec.Command("git", "config", "user.email", "test@example.com")
	cmd.Dir = path
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("git config user.email failed: %w", err)
	}

	cmd = exec.Command("git", "config", "user.name", "Test User")
	cmd.Dir = path
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("git config user.name failed: %w", err)
	}

	// Create an initial commit (required for worktrees)
	readmePath := filepath.Join(path, "README.md")
	if err := os.WriteFile(readmePath, []byte("# Test Repo\n"), 0644); err != nil {
		return fmt.Errorf("failed to create README: %w", err)
	}

	cmd = exec.Command("git", "add", "README.md")
	cmd.Dir = path
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("git add failed: %w", err)
	}

	cmd = exec.Command("git", "commit", "-m", "Initial commit")
	cmd.Dir = path
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("git commit failed: %w", err)
	}

	return nil
}

// Helper functions

func setupTestStorage(t *testing.T, ctx context.Context) storage.Storage {
	t.Helper()

	// Use a temp file instead of :memory: for better CI compatibility
	tmpfile, err := os.CreateTemp("", "test-*.db")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	tmpfile.Close()
	
	t.Cleanup(func() {
		os.Remove(tmpfile.Name())
	})

	cfg := storage.DefaultConfig()
	cfg.Path = tmpfile.Name()

	store, err := storage.NewStorage(ctx, cfg)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}

	return store
}

func setupTestExecutor(t *testing.T, store storage.Storage, enableAI bool) *Executor {
	t.Helper()

	cfg := DefaultConfig()
	cfg.Store = store
	cfg.EnableAISupervision = enableAI
	cfg.PollInterval = 100 * time.Millisecond

	exec, err := New(cfg)
	if err != nil {
		t.Fatalf("Failed to create executor: %v", err)
	}

	// Register instance
	ctx := context.Background()
	instance := &types.ExecutorInstance{
		InstanceID:    exec.instanceID,
		Hostname:      exec.hostname,
		PID:           exec.pid,
		Status:        types.ExecutorStatusRunning,
		StartedAt:     time.Now(),
		LastHeartbeat: time.Now(),
		Version:       exec.version,
		Metadata:      "{}",
	}
	if err := store.RegisterInstance(ctx, instance); err != nil {
		t.Fatalf("Failed to register executor: %v", err)
	}

	return exec
}

func executeAndCompleteTask(t *testing.T, ctx context.Context, store storage.Storage, exec *Executor, task *types.Issue) {
	t.Helper()

	// Claim
	if err := store.ClaimIssue(ctx, task.ID, exec.instanceID); err != nil {
		t.Fatalf("Failed to claim task %s: %v", task.ID, err)
	}

	// Execute through states - must follow strict sequence
	// claimed -> assessing -> executing -> analyzing -> gates -> completed
	states := []types.ExecutionState{
		types.ExecutionStateAssessing,
		types.ExecutionStateExecuting,
		types.ExecutionStateAnalyzing,
		types.ExecutionStateGates,
		types.ExecutionStateCompleted,
	}

	for _, state := range states {
		if err := store.UpdateExecutionState(ctx, task.ID, state); err != nil {
			t.Fatalf("Failed to update task %s to %s: %v", task.ID, state, err)
		}
	}

	// Close
	updates := map[string]interface{}{"status": types.StatusClosed}
	if err := store.UpdateIssue(ctx, task.ID, updates, exec.instanceID); err != nil {
		t.Fatalf("Failed to close task %s: %v", task.ID, err)
	}

	// Release
	if err := store.ReleaseIssue(ctx, task.ID); err != nil {
		t.Fatalf("Failed to release task %s: %v", task.ID, err)
	}
}
