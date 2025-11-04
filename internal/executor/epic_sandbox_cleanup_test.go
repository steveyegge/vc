package executor

import (
	"context"
	"os"
	"testing"

	"github.com/steveyegge/vc/internal/sandbox"
	"github.com/steveyegge/vc/internal/storage"
	"github.com/steveyegge/vc/internal/types"
)

// TestMissionSandboxAutoCleanup tests that sandboxes are automatically cleaned up when missions complete
func TestMissionSandboxAutoCleanup(t *testing.T) {
	ctx := context.Background()

	// Create temporary directory for sandboxes
	tmpDir, err := os.MkdirTemp("", "vc-test-sandboxes-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create test repo
	repoDir, err := os.MkdirTemp("", "vc-test-repo-*")
	if err != nil {
		t.Fatalf("Failed to create test repo: %v", err)
	}
	defer os.RemoveAll(repoDir)

	// Initialize git repo
	if err := setupGitRepo(t, repoDir); err != nil {
		t.Fatalf("Failed to setup git repo: %v", err)
	}

	// Create in-memory storage
	cfg := storage.DefaultConfig()
	cfg.Path = t.TempDir() + "/test.db"
	store, err := storage.NewStorage(ctx, cfg)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer store.Close()

	// Create sandbox manager
	mgr, err := sandbox.NewManager(sandbox.Config{
		SandboxRoot: tmpDir,
		ParentRepo:  repoDir,
		MainDB:      store,
	})
	if err != nil {
		t.Fatalf("Failed to create sandbox manager: %v", err)
	}

	// Create a mission epic
	mission := &types.Mission{
		Issue: types.Issue{
			Title:        "Implement feature X",
			Status:       types.StatusOpen,
			Priority:     1,
			IssueType:    types.TypeEpic,
			IssueSubtype: "mission",
		},
	}
	if err := store.CreateMission(ctx, mission, "test"); err != nil {
		t.Fatalf("Failed to create mission: %v", err)
	}

	// Create mission sandbox
	sb, err := sandbox.CreateMissionSandbox(ctx, mgr, store, mission.ID)
	if err != nil {
		t.Fatalf("Failed to create mission sandbox: %v", err)
	}

	// Verify sandbox was created
	if sb.Path == "" {
		t.Fatal("Sandbox path should not be empty")
	}
	if _, err := os.Stat(sb.Path); os.IsNotExist(err) {
		t.Fatalf("Sandbox directory should exist at %s", sb.Path)
	}

	// Create a child task
	task := &types.Issue{
		Title:     "Subtask 1",
		Status:    types.StatusOpen,
		Priority:  1,
		IssueType: types.TypeTask,
	}
	if err := store.CreateIssue(ctx, task, "test"); err != nil {
		t.Fatalf("Failed to create task: %v", err)
	}

	// Link task to mission
	dep := &types.Dependency{
		IssueID:     task.ID,
		DependsOnID: mission.ID,
		Type:        types.DepParentChild,
	}
	if err := store.AddDependency(ctx, dep, "test"); err != nil {
		t.Fatalf("Failed to add dependency: %v", err)
	}

	// Close the task and check epic completion with sandbox manager
	// This should close the mission and cleanup the sandbox
	if err := store.CloseIssue(ctx, task.ID, "completed", "test"); err != nil {
		t.Fatalf("Failed to close task: %v", err)
	}

	// Simulate what happens in result_processor.go after task completion
	// Check epic completion should detect mission is complete and clean up sandbox
	if err := checkEpicCompletion(ctx, store, nil, mgr, "test-instance", task.ID); err != nil {
		t.Fatalf("checkEpicCompletion failed: %v", err)
	}

	// Verify mission was closed (fallback logic when no AI supervisor)
	updatedMission, err := store.GetMission(ctx, mission.ID)
	if err != nil {
		t.Fatalf("Failed to get updated mission: %v", err)
	}
	if updatedMission.Status != types.StatusClosed {
		t.Errorf("Mission should be closed, got status: %s", updatedMission.Status)
	}

	// Verify sandbox metadata was cleared
	if updatedMission.SandboxPath != "" {
		t.Errorf("Mission sandbox_path should be cleared, got: %s", updatedMission.SandboxPath)
	}
	if updatedMission.BranchName != "" {
		t.Errorf("Mission branch_name should be cleared, got: %s", updatedMission.BranchName)
	}

	// Verify sandbox directory was removed
	if _, err := os.Stat(sb.Path); !os.IsNotExist(err) {
		t.Errorf("Sandbox directory should be removed at %s", sb.Path)
	}

	t.Log("✓ Mission sandbox automatically cleaned up after mission completion")
}

// TestMissionSandboxPersistsWhenIncomplete tests that sandboxes are NOT cleaned up when missions are incomplete
func TestMissionSandboxPersistsWhenIncomplete(t *testing.T) {
	ctx := context.Background()

	// Create temporary directory for sandboxes
	tmpDir, err := os.MkdirTemp("", "vc-test-sandboxes-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create test repo
	repoDir, err := os.MkdirTemp("", "vc-test-repo-*")
	if err != nil {
		t.Fatalf("Failed to create test repo: %v", err)
	}
	defer os.RemoveAll(repoDir)

	// Initialize git repo
	if err := setupGitRepo(t, repoDir); err != nil {
		t.Fatalf("Failed to setup git repo: %v", err)
	}

	// Create in-memory storage
	cfg := storage.DefaultConfig()
	cfg.Path = t.TempDir() + "/test.db"
	store, err := storage.NewStorage(ctx, cfg)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer store.Close()

	// Create sandbox manager
	mgr, err := sandbox.NewManager(sandbox.Config{
		SandboxRoot: tmpDir,
		ParentRepo:  repoDir,
		MainDB:      store,
	})
	if err != nil {
		t.Fatalf("Failed to create sandbox manager: %v", err)
	}

	// Create a mission epic
	mission := &types.Mission{
		Issue: types.Issue{
			Title:        "Implement feature X",
			Status:       types.StatusOpen,
			Priority:     1,
			IssueType:    types.TypeEpic,
			IssueSubtype: "mission",
		},
	}
	if err := store.CreateMission(ctx, mission, "test"); err != nil {
		t.Fatalf("Failed to create mission: %v", err)
	}

	// Create mission sandbox
	sb, err := sandbox.CreateMissionSandbox(ctx, mgr, store, mission.ID)
	if err != nil {
		t.Fatalf("Failed to create mission sandbox: %v", err)
	}

	// Verify sandbox was created
	if sb.Path == "" {
		t.Fatal("Sandbox path should not be empty")
	}
	if _, err := os.Stat(sb.Path); os.IsNotExist(err) {
		t.Fatalf("Sandbox directory should exist at %s", sb.Path)
	}

	// Create two child tasks
	task1 := &types.Issue{
		Title:     "Subtask 1",
		Status:    types.StatusOpen,
		Priority:  1,
		IssueType: types.TypeTask,
	}
	if err := store.CreateIssue(ctx, task1, "test"); err != nil {
		t.Fatalf("Failed to create task1: %v", err)
	}

	task2 := &types.Issue{
		Title:     "Subtask 2",
		Status:    types.StatusOpen,
		Priority:  1,
		IssueType: types.TypeTask,
	}
	if err := store.CreateIssue(ctx, task2, "test"); err != nil {
		t.Fatalf("Failed to create task2: %v", err)
	}

	// Link tasks to mission
	dep1 := &types.Dependency{
		IssueID:     task1.ID,
		DependsOnID: mission.ID,
		Type:        types.DepParentChild,
	}
	if err := store.AddDependency(ctx, dep1, "test"); err != nil {
		t.Fatalf("Failed to add dependency 1: %v", err)
	}

	dep2 := &types.Dependency{
		IssueID:     task2.ID,
		DependsOnID: mission.ID,
		Type:        types.DepParentChild,
	}
	if err := store.AddDependency(ctx, dep2, "test"); err != nil {
		t.Fatalf("Failed to add dependency 2: %v", err)
	}

	// Close only task1 (mission still incomplete because task2 is open)
	if err := store.CloseIssue(ctx, task1.ID, "completed", "test"); err != nil {
		t.Fatalf("Failed to close task1: %v", err)
	}

	// Check epic completion - should NOT close mission (task2 still open)
	if err := checkEpicCompletion(ctx, store, nil, mgr, "test-instance", task1.ID); err != nil {
		t.Fatalf("checkEpicCompletion failed: %v", err)
	}

	// Verify mission is still open
	updatedMission, err := store.GetMission(ctx, mission.ID)
	if err != nil {
		t.Fatalf("Failed to get updated mission: %v", err)
	}
	if updatedMission.Status != types.StatusOpen {
		t.Errorf("Mission should still be open, got status: %s", updatedMission.Status)
	}

	// Verify sandbox metadata is still present
	if updatedMission.SandboxPath == "" {
		t.Error("Mission sandbox_path should not be cleared (mission not complete)")
	}
	if updatedMission.BranchName == "" {
		t.Error("Mission branch_name should not be cleared (mission not complete)")
	}

	// Verify sandbox directory still exists
	if _, err := os.Stat(sb.Path); os.IsNotExist(err) {
		t.Errorf("Sandbox directory should still exist at %s (mission not complete)", sb.Path)
	}

	t.Log("✓ Mission sandbox preserved when mission incomplete")
}
