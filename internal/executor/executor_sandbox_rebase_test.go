package executor

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/steveyegge/vc/internal/storage"
	"github.com/steveyegge/vc/internal/types"
)

// TestRebaseAllSandboxes_NoSandboxes verifies behavior when no sandboxes exist
func TestRebaseAllSandboxes_NoSandboxes(t *testing.T) {
	ctx := context.Background()

	// Create temporary database
	tmpDB := t.TempDir() + "/test.db"
	storeCfg := &storage.Config{Path: tmpDB}
	store, err := storage.NewStorage(ctx, storeCfg)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer func() { _ = store.Close() }()

	// Create executor
	cfg := DefaultConfig()
	cfg.Store = store
	cfg.EnableSandboxes = true
	cfg.EnableAISupervision = false // Disable for simpler test
	cfg.EnableQualityGates = false
	cfg.EnableQualityGateWorker = false // Must be disabled when gates are disabled
	executor, err := New(cfg)
	if err != nil {
		t.Fatalf("Failed to create executor: %v", err)
	}

	// Call rebaseAllSandboxes (should complete successfully with no work)
	anySucceeded, err := executor.rebaseAllSandboxes(ctx)
	if err != nil {
		t.Errorf("Expected no error when no sandboxes exist, got: %v", err)
	}
	if anySucceeded {
		t.Errorf("Expected no successful rebases, got anySucceeded=true")
	}
}

// TestRebaseAllSandboxes_MissionWithoutSandbox verifies missions without sandbox metadata are skipped
func TestRebaseAllSandboxes_MissionWithoutSandbox(t *testing.T) {
	ctx := context.Background()

	// Create temporary database
	tmpDB := t.TempDir() + "/test.db"
	storeCfg := &storage.Config{Path: tmpDB}
	store, err := storage.NewStorage(ctx, storeCfg)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer func() { _ = store.Close() }()

	// Create a mission without sandbox metadata
	mission := &types.Mission{
		Issue: types.Issue{
			ID:          "vc-test1",
			Title:       "Test Mission",
			Description: "Test",
			IssueType:   types.TypeEpic,
			Status:      types.StatusOpen,
			Priority:    1,
		},
		Goal: "Test mission without sandbox",
	}
	if err := store.CreateMission(ctx, mission, "test"); err != nil {
		t.Fatalf("Failed to create mission: %v", err)
	}

	// Create executor
	cfg := DefaultConfig()
	cfg.Store = store
	cfg.EnableSandboxes = true
	cfg.EnableAISupervision = false
	cfg.EnableQualityGates = false
	cfg.EnableQualityGateWorker = false
	executor, err := New(cfg)
	if err != nil {
		t.Fatalf("Failed to create executor: %v", err)
	}

	// Call rebaseAllSandboxes (should skip mission without sandbox)
	anySucceeded, err := executor.rebaseAllSandboxes(ctx)
	if err != nil {
		t.Errorf("Expected no error for mission without sandbox, got: %v", err)
	}
	if anySucceeded {
		t.Errorf("Expected no successful rebases for mission without sandbox, got anySucceeded=true")
	}
}

// TestRebaseSandbox_NonexistentPath verifies graceful handling of missing sandbox paths
func TestRebaseSandbox_NonexistentPath(t *testing.T) {
	ctx := context.Background()

	// Create temporary database
	tmpDB := t.TempDir() + "/test.db"
	storeCfg := &storage.Config{Path: tmpDB}
	store, err := storage.NewStorage(ctx, storeCfg)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer func() { _ = store.Close() }()

	// Create a mission with nonexistent sandbox path
	nonexistentPath := t.TempDir() + "/nonexistent-sandbox"
	mission := &types.Mission{
		Issue: types.Issue{
			ID:          "vc-test2",
			Title:       "Test Mission",
			Description: "Test",
			IssueType:   types.TypeEpic,
			Status:      types.StatusOpen,
			Priority:    1,
		},
		Goal:        "Test mission with invalid sandbox",
		SandboxPath: nonexistentPath,
		BranchName:  "mission/test",
	}
	if err := store.CreateMission(ctx, mission, "test"); err != nil {
		t.Fatalf("Failed to create mission: %v", err)
	}

	// Create executor
	cfg := DefaultConfig()
	cfg.Store = store
	cfg.EnableSandboxes = true
	cfg.EnableAISupervision = false
	cfg.EnableQualityGates = false
	cfg.EnableQualityGateWorker = false
	executor, err := New(cfg)
	if err != nil {
		t.Fatalf("Failed to create executor: %v", err)
	}

	// Call rebaseAllSandboxes (should skip nonexistent sandbox)
	anySucceeded, err := executor.rebaseAllSandboxes(ctx)
	if err != nil {
		t.Errorf("Expected no error for nonexistent sandbox, got: %v", err)
	}
	if anySucceeded {
		t.Errorf("Expected no successful rebases for nonexistent sandbox, got anySucceeded=true")
	}
}

// TestRebaseSandbox_CleanRebase verifies successful rebase operation
func TestRebaseSandbox_CleanRebase(t *testing.T) {
	// This test requires git to be available
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available, skipping integration test")
	}

	ctx := context.Background()

	// Create temporary git repository
	tmpDir := t.TempDir()
	repoDir := filepath.Join(tmpDir, "repo")
	if err := os.MkdirAll(repoDir, 0755); err != nil {
		t.Fatalf("Failed to create repo directory: %v", err)
	}

	// Initialize git repo
	runGit := func(args ...string) error {
		cmd := exec.Command("git", args...)
		cmd.Dir = repoDir
		output, err := cmd.CombinedOutput()
		if err != nil {
			return &testError{err: err, output: string(output)}
		}
		return nil
	}

	// Set up repo
	if err := runGit("init"); err != nil {
		t.Fatalf("Failed to init repo: %v", err)
	}
	// Explicitly set initial branch to 'main' for consistency across git versions
	if err := runGit("config", "init.defaultBranch", "main"); err != nil {
		t.Fatalf("Failed to configure default branch: %v", err)
	}
	if err := runGit("symbolic-ref", "HEAD", "refs/heads/main"); err != nil {
		t.Fatalf("Failed to set HEAD to main: %v", err)
	}
	if err := runGit("config", "user.email", "test@example.com"); err != nil {
		t.Fatalf("Failed to configure git: %v", err)
	}
	if err := runGit("config", "user.name", "Test User"); err != nil {
		t.Fatalf("Failed to configure git: %v", err)
	}

	// Create initial commit on main
	testFile := filepath.Join(repoDir, "file.txt")
	if err := os.WriteFile(testFile, []byte("initial content\n"), 0644); err != nil {
		t.Fatalf("Failed to write test file: %v", err)
	}
	if err := runGit("add", "."); err != nil {
		t.Fatalf("Failed to add files: %v", err)
	}
	if err := runGit("commit", "-m", "Initial commit"); err != nil {
		t.Fatalf("Failed to commit: %v", err)
	}

	// Create feature branch
	if err := runGit("checkout", "-b", "mission/test"); err != nil {
		t.Fatalf("Failed to create branch: %v", err)
	}
	if err := os.WriteFile(testFile, []byte("initial content\nfeature work\n"), 0644); err != nil {
		t.Fatalf("Failed to write test file: %v", err)
	}
	if err := runGit("add", "."); err != nil {
		t.Fatalf("Failed to add files: %v", err)
	}
	if err := runGit("commit", "-m", "Feature work"); err != nil {
		t.Fatalf("Failed to commit: %v", err)
	}

	// Add new commit to main (simulating upstream changes)
	if err := runGit("checkout", "main"); err != nil {
		t.Fatalf("Failed to checkout main: %v", err)
	}
	mainFile := filepath.Join(repoDir, "main.txt")
	if err := os.WriteFile(mainFile, []byte("main content\n"), 0644); err != nil {
		t.Fatalf("Failed to write main file: %v", err)
	}
	if err := runGit("add", "."); err != nil {
		t.Fatalf("Failed to add files: %v", err)
	}
	if err := runGit("commit", "-m", "Main branch progress"); err != nil {
		t.Fatalf("Failed to commit: %v", err)
	}

	// Set up origin remote pointing to the same repo (simulates cloned sandbox)
	// In a real scenario, sandboxes are clones with origin pointing to parent
	if err := runGit("remote", "add", "origin", repoDir); err != nil {
		t.Fatalf("Failed to add origin remote: %v", err)
	}

	// Checkout feature branch again (simulating sandbox state)
	if err := runGit("checkout", "mission/test"); err != nil {
		t.Fatalf("Failed to checkout feature branch: %v", err)
	}

	// Create executor and test rebase
	tmpDB := t.TempDir() + "/test.db"
	storeCfg := &storage.Config{Path: tmpDB}
	store, err := storage.NewStorage(ctx, storeCfg)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer func() { _ = store.Close() }()

	cfg := DefaultConfig()
	cfg.Store = store
	cfg.EnableSandboxes = true
	cfg.EnableAISupervision = false
	cfg.EnableQualityGates = false
	cfg.EnableQualityGateWorker = false
	executor, err := New(cfg)
	if err != nil {
		t.Fatalf("Failed to create executor: %v", err)
	}

	// Perform rebase
	result := executor.rebaseSandbox(ctx, repoDir, "mission/test", "main")

	if !result.Success {
		t.Errorf("Expected successful rebase, got: success=%v, error=%v, output=%s",
			result.Success, result.Error, result.Output)
	}
	if result.HasConflict {
		t.Errorf("Expected no conflicts, got conflict=true")
	}

	// Verify feature branch now includes main's changes
	if err := runGit("log", "--oneline"); err != nil {
		t.Logf("Git log output: %v", err)
	}

	// Check that main.txt exists (from main branch)
	if _, err := os.Stat(filepath.Join(repoDir, "main.txt")); os.IsNotExist(err) {
		t.Errorf("Expected main.txt to exist after rebase")
	}
}

// testError is a helper type for test error reporting
type testError struct {
	err    error
	output string
}

func (e *testError) Error() string {
	return e.err.Error() + "\nOutput: " + e.output
}

// TestHandleRebaseConflicts_CreatesBlockingTask verifies conflict resolution task creation
func TestHandleRebaseConflicts_CreatesBlockingTask(t *testing.T) {
	ctx := context.Background()

	// Create temporary database
	tmpDB := t.TempDir() + "/test.db"
	storeCfg := &storage.Config{Path: tmpDB}
	store, err := storage.NewStorage(ctx, storeCfg)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer func() { _ = store.Close() }()

	// Create a mission
	mission := &types.Mission{
		Issue: types.Issue{
			ID:          "vc-test3",
			Title:       "Test Mission",
			Description: "Test",
			IssueType:   types.TypeEpic,
			Status:      types.StatusOpen,
			Priority:    1,
		},
		Goal:        "Test mission with conflicts",
		SandboxPath: ".sandboxes/mission-test",
		BranchName:  "mission/test",
	}
	if err := store.CreateMission(ctx, mission, "test"); err != nil {
		t.Fatalf("Failed to create mission: %v", err)
	}

	// Create executor
	cfg := DefaultConfig()
	cfg.Store = store
	cfg.EnableSandboxes = true
	cfg.EnableAISupervision = false
	cfg.EnableQualityGates = false
	cfg.EnableQualityGateWorker = false
	executor, err := New(cfg)
	if err != nil {
		t.Fatalf("Failed to create executor: %v", err)
	}

	// Create conflict result
	result := RebaseResult{
		SandboxPath: ".sandboxes/mission-test",
		BranchName:  "mission/test",
		Success:     false,
		HasConflict: true,
		Output:      "CONFLICT (content): Merge conflict in file.txt",
	}

	// Handle conflicts
	err = executor.handleRebaseConflicts(ctx, mission, result)
	if err != nil {
		t.Fatalf("Failed to handle conflicts: %v", err)
	}

	// Verify conflict resolution task was created
	status := types.StatusOpen
	filter := types.IssueFilter{
		Labels: []string{"rebase-conflict"},
		Status: &status,
	}
	issues, err := store.SearchIssues(ctx, "", filter)
	if err != nil {
		t.Fatalf("Failed to search for conflict tasks: %v", err)
	}

	if len(issues) != 1 {
		t.Errorf("Expected 1 conflict resolution task, got %d", len(issues))
	} else {
		task := issues[0]
		if task.Priority != 0 {
			t.Errorf("Expected P0 priority, got %d", task.Priority)
		}

		// Verify no-auto-claim label exists
		labels, err := store.GetLabels(ctx, task.ID)
		if err != nil {
			t.Fatalf("Failed to get labels: %v", err)
		}
		hasNoAutoClaim := false
		for _, label := range labels {
			if label == "no-auto-claim" {
				hasNoAutoClaim = true
				break
			}
		}
		if !hasNoAutoClaim {
			t.Errorf("Expected no-auto-claim label on conflict task")
		}

		// Verify dependency exists
		deps, err := store.GetDependencies(ctx, mission.ID)
		if err != nil {
			t.Fatalf("Failed to get dependencies: %v", err)
		}
		if len(deps) != 1 {
			t.Errorf("Expected 1 dependency, got %d", len(deps))
		}
	}
}

// TestListMissionsWithSandboxes_FiltersCorrectly verifies mission filtering logic
func TestListMissionsWithSandboxes_FiltersCorrectly(t *testing.T) {
	ctx := context.Background()

	// Create temporary database
	tmpDB := t.TempDir() + "/test.db"
	storeCfg := &storage.Config{Path: tmpDB}
	store, err := storage.NewStorage(ctx, storeCfg)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer func() { _ = store.Close() }()

	// Create mission with sandbox
	missionWithSandbox := &types.Mission{
		Issue: types.Issue{
			ID:           "vc-test4",
			Title:        "Mission With Sandbox",
			Description:  "Test",
			IssueType:    types.TypeEpic,
			IssueSubtype: types.SubtypeMission,
			Status:       types.StatusOpen,
			Priority:     1,
		},
		Goal:        "Test mission with sandbox",
		SandboxPath: ".sandboxes/mission-test4",
		BranchName:  "mission/test4",
	}
	if err := store.CreateMission(ctx, missionWithSandbox, "test"); err != nil {
		t.Fatalf("Failed to create mission with sandbox: %v", err)
	}

	// Create mission without sandbox
	missionWithoutSandbox := &types.Mission{
		Issue: types.Issue{
			ID:           "vc-test5",
			Title:        "Mission Without Sandbox",
			Description:  "Test",
			IssueType:    types.TypeEpic,
			IssueSubtype: types.SubtypeMission,
			Status:       types.StatusOpen,
			Priority:     1,
		},
		Goal: "Test mission without sandbox",
	}
	if err := store.CreateMission(ctx, missionWithoutSandbox, "test"); err != nil {
		t.Fatalf("Failed to create mission without sandbox: %v", err)
	}

	// Create closed mission with sandbox (should be filtered out)
	closedMission := &types.Mission{
		Issue: types.Issue{
			ID:           "vc-test6",
			Title:        "Closed Mission",
			Description:  "Test",
			IssueType:    types.TypeEpic,
			IssueSubtype: types.SubtypeMission,
			Status:       types.StatusOpen, // Create as open first
			Priority:     1,
		},
		Goal:        "Test closed mission",
		SandboxPath: ".sandboxes/mission-test6",
		BranchName:  "mission/test6",
	}
	if err := store.CreateMission(ctx, closedMission, "test"); err != nil {
		t.Fatalf("Failed to create closed mission: %v", err)
	}
	// Now close it properly (which sets ClosedAt timestamp)
	if err := store.CloseIssue(ctx, closedMission.ID, "test", "test close"); err != nil {
		t.Fatalf("Failed to close mission: %v", err)
	}

	// Create executor
	cfg := DefaultConfig()
	cfg.Store = store
	cfg.EnableSandboxes = true
	cfg.EnableAISupervision = false
	cfg.EnableQualityGates = false
	cfg.EnableQualityGateWorker = false
	executor, err := New(cfg)
	if err != nil {
		t.Fatalf("Failed to create executor: %v", err)
	}

	// List missions with sandboxes
	missions, err := executor.listMissionsWithSandboxes(ctx)
	if err != nil {
		t.Fatalf("Failed to list missions: %v", err)
	}

	// Verify only open mission with sandbox metadata is included
	if len(missions) != 1 {
		t.Errorf("Expected 1 mission, got %d", len(missions))
	} else {
		if missions[0].ID != "vc-test4" {
			t.Errorf("Expected mission vc-test4, got %s", missions[0].ID)
		}
	}
}
