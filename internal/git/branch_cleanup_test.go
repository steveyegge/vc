package git

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// TestFindOrphanedMissionBranches tests the orphaned branch detection logic
func TestFindOrphanedMissionBranches(t *testing.T) {
	// Create a temporary directory for test repo
	tmpDir, err := os.MkdirTemp("", "git-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Initialize git repo
	ctx := context.Background()
	if err := exec.CommandContext(ctx, "git", "init", tmpDir).Run(); err != nil {
		t.Fatalf("failed to init git repo: %v", err)
	}

	// Configure git
	exec.CommandContext(ctx, "git", "-C", tmpDir, "config", "user.name", "Test User").Run()
	exec.CommandContext(ctx, "git", "-C", tmpDir, "config", "user.email", "test@example.com").Run()

	// Create initial commit
	readmePath := filepath.Join(tmpDir, "README.md")
	if err := os.WriteFile(readmePath, []byte("# Test Repo\n"), 0644); err != nil {
		t.Fatalf("failed to write README: %v", err)
	}
	if err := exec.CommandContext(ctx, "git", "-C", tmpDir, "add", "README.md").Run(); err != nil {
		t.Fatalf("failed to add README: %v", err)
	}
	if err := exec.CommandContext(ctx, "git", "-C", tmpDir, "commit", "-m", "Initial commit").Run(); err != nil {
		t.Fatalf("failed to commit: %v", err)
	}

	// Create a mission branch (orphaned - no worktree)
	if err := exec.CommandContext(ctx, "git", "-C", tmpDir, "branch", "mission/vc-123/1234567890").Run(); err != nil {
		t.Fatalf("failed to create mission branch: %v", err)
	}

	// Create another non-mission branch (should be ignored)
	if err := exec.CommandContext(ctx, "git", "-C", tmpDir, "branch", "feature/test").Run(); err != nil {
		t.Fatalf("failed to create feature branch: %v", err)
	}

	// Initialize git operations
	gitOps, err := NewGit(ctx)
	if err != nil {
		t.Fatalf("failed to create git ops: %v", err)
	}

	// Find orphaned branches
	orphaned, err := gitOps.FindOrphanedMissionBranches(ctx, tmpDir)
	if err != nil {
		t.Fatalf("failed to find orphaned branches: %v", err)
	}

	// Should find exactly 1 orphaned mission branch
	if len(orphaned) != 1 {
		t.Errorf("expected 1 orphaned branch, got %d", len(orphaned))
		for _, b := range orphaned {
			t.Logf("  - %s (age: %v)", b.Name, b.Age)
		}
	}

	// Verify it's the right branch
	if len(orphaned) > 0 && orphaned[0].Name != "mission/vc-123/1234567890" {
		t.Errorf("expected branch 'mission/vc-123/1234567890', got '%s'", orphaned[0].Name)
	}
}

// TestCleanupOrphanedBranches tests branch cleanup with retention policy
func TestCleanupOrphanedBranches(t *testing.T) {
	// Create a temporary directory for test repo
	tmpDir, err := os.MkdirTemp("", "git-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Initialize git repo
	ctx := context.Background()
	if err := exec.CommandContext(ctx, "git", "init", tmpDir).Run(); err != nil {
		t.Fatalf("failed to init git repo: %v", err)
	}

	// Configure git
	exec.CommandContext(ctx, "git", "-C", tmpDir, "config", "user.name", "Test User").Run()
	exec.CommandContext(ctx, "git", "-C", tmpDir, "config", "user.email", "test@example.com").Run()

	// Create initial commit
	readmePath := filepath.Join(tmpDir, "README.md")
	if err := os.WriteFile(readmePath, []byte("# Test Repo\n"), 0644); err != nil {
		t.Fatalf("failed to write README: %v", err)
	}
	if err := exec.CommandContext(ctx, "git", "-C", tmpDir, "add", "README.md").Run(); err != nil {
		t.Fatalf("failed to add README: %v", err)
	}
	if err := exec.CommandContext(ctx, "git", "-C", tmpDir, "commit", "-m", "Initial commit").Run(); err != nil {
		t.Fatalf("failed to commit: %v", err)
	}

	// Create a mission branch
	if err := exec.CommandContext(ctx, "git", "-C", tmpDir, "branch", "mission/vc-456/9876543210").Run(); err != nil {
		t.Fatalf("failed to create mission branch: %v", err)
	}

	// Initialize git operations
	gitOps, err := NewGit(ctx)
	if err != nil {
		t.Fatalf("failed to create git ops: %v", err)
	}

	// Test with retention = 0 days (should delete)
	deleted, err := gitOps.CleanupOrphanedBranches(ctx, tmpDir, 0, true) // dry-run
	if err != nil {
		t.Fatalf("failed to cleanup branches: %v", err)
	}

	if deleted != 1 {
		t.Errorf("expected to delete 1 branch (dry-run), got %d", deleted)
	}

	// Test with retention = 100 days (should NOT delete)
	deleted, err = gitOps.CleanupOrphanedBranches(ctx, tmpDir, 100, true) // dry-run
	if err != nil {
		t.Fatalf("failed to cleanup branches: %v", err)
	}

	if deleted != 0 {
		t.Errorf("expected to delete 0 branches (too recent), got %d", deleted)
	}

	// Actual deletion test (retention = 0)
	deleted, err = gitOps.CleanupOrphanedBranches(ctx, tmpDir, 0, false) // NOT dry-run
	if err != nil {
		t.Fatalf("failed to cleanup branches: %v", err)
	}

	if deleted != 1 {
		t.Errorf("expected to delete 1 branch, got %d", deleted)
	}

	// Verify branch is actually deleted
	orphaned, err := gitOps.FindOrphanedMissionBranches(ctx, tmpDir)
	if err != nil {
		t.Fatalf("failed to find orphaned branches: %v", err)
	}

	if len(orphaned) != 0 {
		t.Errorf("expected 0 orphaned branches after cleanup, got %d", len(orphaned))
	}
}

// TestListWorktrees tests worktree listing functionality
func TestListWorktrees(t *testing.T) {
	// Create a temporary directory for test repo
	tmpDir, err := os.MkdirTemp("", "git-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Initialize git repo
	ctx := context.Background()
	if err := exec.CommandContext(ctx, "git", "init", tmpDir).Run(); err != nil {
		t.Fatalf("failed to init git repo: %v", err)
	}

	// Configure git
	exec.CommandContext(ctx, "git", "-C", tmpDir, "config", "user.name", "Test User").Run()
	exec.CommandContext(ctx, "git", "-C", tmpDir, "config", "user.email", "test@example.com").Run()

	// Create initial commit
	readmePath := filepath.Join(tmpDir, "README.md")
	if err := os.WriteFile(readmePath, []byte("# Test Repo\n"), 0644); err != nil {
		t.Fatalf("failed to write README: %v", err)
	}
	if err := exec.CommandContext(ctx, "git", "-C", tmpDir, "add", "README.md").Run(); err != nil {
		t.Fatalf("failed to add README: %v", err)
	}
	if err := exec.CommandContext(ctx, "git", "-C", tmpDir, "commit", "-m", "Initial commit").Run(); err != nil {
		t.Fatalf("failed to commit: %v", err)
	}

	// Initialize git operations
	gitOps, err := NewGit(ctx)
	if err != nil {
		t.Fatalf("failed to create git ops: %v", err)
	}

	// List worktrees (should have only the main worktree)
	worktrees, err := gitOps.ListWorktrees(ctx, tmpDir)
	if err != nil {
		t.Fatalf("failed to list worktrees: %v", err)
	}

	// Main worktree should be present
	if len(worktrees) != 1 {
		t.Errorf("expected 1 worktree (main), got %d", len(worktrees))
		for path, branch := range worktrees {
			t.Logf("  - %s -> %s", path, branch)
		}
	}
}

// TestGetBranchTimestamp tests getting branch commit timestamp
func TestGetBranchTimestamp(t *testing.T) {
	// Create a temporary directory for test repo
	tmpDir, err := os.MkdirTemp("", "git-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Initialize git repo
	ctx := context.Background()
	if err := exec.CommandContext(ctx, "git", "init", tmpDir).Run(); err != nil {
		t.Fatalf("failed to init git repo: %v", err)
	}

	// Configure git
	exec.CommandContext(ctx, "git", "-C", tmpDir, "config", "user.name", "Test User").Run()
	exec.CommandContext(ctx, "git", "-C", tmpDir, "config", "user.email", "test@example.com").Run()

	// Create initial commit
	readmePath := filepath.Join(tmpDir, "README.md")
	if err := os.WriteFile(readmePath, []byte("# Test Repo\n"), 0644); err != nil {
		t.Fatalf("failed to write README: %v", err)
	}
	if err := exec.CommandContext(ctx, "git", "-C", tmpDir, "add", "README.md").Run(); err != nil {
		t.Fatalf("failed to add README: %v", err)
	}
	if err := exec.CommandContext(ctx, "git", "-C", tmpDir, "commit", "-m", "Initial commit").Run(); err != nil {
		t.Fatalf("failed to commit: %v", err)
	}

	// Initialize git operations
	gitOps, err := NewGit(ctx)
	if err != nil {
		t.Fatalf("failed to create git ops: %v", err)
	}

	// Get timestamp for HEAD (current branch)
	timestamp, err := gitOps.GetBranchTimestamp(ctx, tmpDir, "HEAD")
	if err != nil {
		t.Fatalf("failed to get branch timestamp: %v", err)
	}

	// Timestamp should be recent (within last minute)
	now := time.Now()
	diff := now.Sub(timestamp)
	if diff < 0 || diff > time.Minute {
		t.Errorf("timestamp seems wrong: %v (diff from now: %v)", timestamp, diff)
	}
}
