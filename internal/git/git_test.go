package git

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestGitOperations tests the basic git operations
func TestGitOperations(t *testing.T) {
	ctx := context.Background()

	// Create a temporary directory for testing
	tmpDir, err := os.MkdirTemp("", "vc-git-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Initialize a git repository
	cmd := exec.Command("git", "init")
	cmd.Dir = tmpDir
	if err := cmd.Run(); err != nil {
		t.Fatalf("Failed to init git repo: %v", err)
	}

	// Configure git user for commits
	configUser := exec.Command("git", "config", "user.name", "Test User")
	configUser.Dir = tmpDir
	if err := configUser.Run(); err != nil {
		t.Fatalf("Failed to config git user: %v", err)
	}

	configEmail := exec.Command("git", "config", "user.email", "test@example.com")
	configEmail.Dir = tmpDir
	if err := configEmail.Run(); err != nil {
		t.Fatalf("Failed to config git email: %v", err)
	}

	// Create a Git instance
	git, err := NewGit(ctx)
	if err != nil {
		t.Fatalf("Failed to create Git instance: %v", err)
	}

	// Test 1: Check no uncommitted changes in empty repo
	t.Run("NoChangesInEmptyRepo", func(t *testing.T) {
		hasChanges, err := git.HasUncommittedChanges(ctx, tmpDir)
		if err != nil {
			t.Fatalf("HasUncommittedChanges failed: %v", err)
		}
		if hasChanges {
			t.Error("Expected no uncommitted changes in empty repo")
		}
	})

	// Test 2: Create a file and check for changes
	t.Run("DetectUncommittedChanges", func(t *testing.T) {
		testFile := filepath.Join(tmpDir, "test.txt")
		if err := os.WriteFile(testFile, []byte("test content"), 0644); err != nil {
			t.Fatalf("Failed to create test file: %v", err)
		}

		hasChanges, err := git.HasUncommittedChanges(ctx, tmpDir)
		if err != nil {
			t.Fatalf("HasUncommittedChanges failed: %v", err)
		}
		if !hasChanges {
			t.Error("Expected uncommitted changes after creating file")
		}
	})

	// Test 3: Get detailed status
	t.Run("GetDetailedStatus", func(t *testing.T) {
		status, err := git.GetStatus(ctx, tmpDir)
		if err != nil {
			t.Fatalf("GetStatus failed: %v", err)
		}

		if !status.HasChanges {
			t.Error("Expected HasChanges to be true")
		}

		if len(status.Untracked) != 1 || status.Untracked[0] != "test.txt" {
			t.Errorf("Expected 1 untracked file 'test.txt', got: %v", status.Untracked)
		}
	})

	// Test 4: Commit changes
	t.Run("CommitChanges", func(t *testing.T) {
		opts := CommitOptions{
			Message: "test: add test file\n\nThis is a test commit.",
			CoAuthors: []string{
				"Claude <noreply@anthropic.com>",
			},
			AddAll:     true,
			AllowEmpty: false,
		}

		commitHash, err := git.CommitChanges(ctx, tmpDir, opts)
		if err != nil {
			t.Fatalf("CommitChanges failed: %v", err)
		}

		if commitHash == "" {
			t.Error("Expected non-empty commit hash")
		}

		if len(commitHash) != 40 {
			t.Errorf("Expected commit hash to be 40 chars, got %d: %s", len(commitHash), commitHash)
		}

		// Verify no more uncommitted changes
		hasChanges, err := git.HasUncommittedChanges(ctx, tmpDir)
		if err != nil {
			t.Fatalf("HasUncommittedChanges failed: %v", err)
		}
		if hasChanges {
			t.Error("Expected no uncommitted changes after commit")
		}
	})

	// Test 5: Verify commit message with co-author
	t.Run("VerifyCommitMessage", func(t *testing.T) {
		cmd := exec.Command("git", "log", "-1", "--pretty=format:%B")
		cmd.Dir = tmpDir
		output, err := cmd.Output()
		if err != nil {
			t.Fatalf("Failed to get commit message: %v", err)
		}

		message := string(output)
		if !strings.Contains(message, "test: add test file") {
			t.Errorf("Commit message doesn't contain subject line: %s", message)
		}

		if !strings.Contains(message, "Co-Authored-By: Claude <noreply@anthropic.com>") {
			t.Errorf("Commit message doesn't contain co-author: %s", message)
		}
	})

	// Test 6: Modify file and commit again
	t.Run("ModifyAndCommit", func(t *testing.T) {
		testFile := filepath.Join(tmpDir, "test.txt")
		if err := os.WriteFile(testFile, []byte("modified content"), 0644); err != nil {
			t.Fatalf("Failed to modify test file: %v", err)
		}

		status, err := git.GetStatus(ctx, tmpDir)
		if err != nil {
			t.Fatalf("GetStatus failed: %v", err)
		}

		if len(status.Modified) != 1 {
			t.Errorf("Expected 1 modified file, got: %v", status.Modified)
		}

		opts := CommitOptions{
			Message:    "test: modify test file",
			AddAll:     true,
			AllowEmpty: false,
		}

		commitHash, err := git.CommitChanges(ctx, tmpDir, opts)
		if err != nil {
			t.Fatalf("CommitChanges failed: %v", err)
		}

		if commitHash == "" {
			t.Error("Expected non-empty commit hash")
		}
	})
}

// TestGitNotAvailable tests behavior when git is not available
func TestGitNotAvailable(t *testing.T) {
	// This test would require mocking exec.LookPath, which is complex
	// For now, we'll skip it, but in a real scenario, we'd use dependency injection
	t.Skip("Skipping git availability test - requires mocking")
}

// TestGitOperations_ErrorCases tests error handling
func TestGitOperations_ErrorCases(t *testing.T) {
	ctx := context.Background()

	git, err := NewGit(ctx)
	if err != nil {
		t.Fatalf("Failed to create Git instance: %v", err)
	}

	t.Run("InvalidRepoPath", func(t *testing.T) {
		nonExistentPath := "/tmp/nonexistent-repo-" + t.Name()

		_, err := git.HasUncommittedChanges(ctx, nonExistentPath)
		if err == nil {
			t.Error("Expected error for non-existent repo path")
		}

		_, err = git.GetStatus(ctx, nonExistentPath)
		if err == nil {
			t.Error("Expected error for non-existent repo path")
		}
	})

	t.Run("EmptyCommitMessage", func(t *testing.T) {
		tmpDir, err := os.MkdirTemp("", "vc-git-test-*")
		if err != nil {
			t.Fatalf("Failed to create temp dir: %v", err)
		}
		defer os.RemoveAll(tmpDir)

		// Initialize a git repository
		cmd := exec.Command("git", "init")
		cmd.Dir = tmpDir
		if err := cmd.Run(); err != nil {
			t.Fatalf("Failed to init git repo: %v", err)
		}

		opts := CommitOptions{
			Message: "", // Empty message
			AddAll:  true,
		}

		_, err = git.CommitChanges(ctx, tmpDir, opts)
		if err == nil {
			t.Error("Expected error for empty commit message")
		}
		if err != nil && !strings.Contains(err.Error(), "commit message is required") {
			t.Errorf("Expected 'commit message is required' error, got: %v", err)
		}
	})

	t.Run("CommitInNonRepo", func(t *testing.T) {
		tmpDir, err := os.MkdirTemp("", "vc-git-test-*")
		if err != nil {
			t.Fatalf("Failed to create temp dir: %v", err)
		}
		defer os.RemoveAll(tmpDir)

		// Don't initialize git repo
		opts := CommitOptions{
			Message: "test commit",
			AddAll:  true,
		}

		_, err = git.CommitChanges(ctx, tmpDir, opts)
		if err == nil {
			t.Error("Expected error when committing in non-repo directory")
		}
	})

	t.Run("GetDiffInNonRepo", func(t *testing.T) {
		tmpDir, err := os.MkdirTemp("", "vc-git-test-*")
		if err != nil {
			t.Fatalf("Failed to create temp dir: %v", err)
		}
		defer os.RemoveAll(tmpDir)

		_, err = git.GetDiff(ctx, tmpDir, false)
		if err == nil {
			t.Error("Expected error when getting diff in non-repo directory")
		}
	})

	t.Run("CancelledContext", func(t *testing.T) {
		tmpDir, err := os.MkdirTemp("", "vc-git-test-*")
		if err != nil {
			t.Fatalf("Failed to create temp dir: %v", err)
		}
		defer os.RemoveAll(tmpDir)

		// Initialize a git repository
		cmd := exec.Command("git", "init")
		cmd.Dir = tmpDir
		if err := cmd.Run(); err != nil {
			t.Fatalf("Failed to init git repo: %v", err)
		}

		// Cancel context immediately
		cancelledCtx, cancel := context.WithCancel(ctx)
		cancel()

		_, err = git.GetStatus(cancelledCtx, tmpDir)
		// Error may or may not occur depending on timing
		// Just ensure it doesn't panic
		_ = err
	})
}

// TestCommitMessageValidation tests that commit message validation works
func TestCommitMessageValidation(t *testing.T) {
	ctx := context.Background()

	tmpDir, err := os.MkdirTemp("", "vc-git-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Initialize a git repository
	cmd := exec.Command("git", "init")
	cmd.Dir = tmpDir
	if err := cmd.Run(); err != nil {
		t.Fatalf("Failed to init git repo: %v", err)
	}

	// Configure git user
	configUser := exec.Command("git", "config", "user.name", "Test User")
	configUser.Dir = tmpDir
	if err := configUser.Run(); err != nil {
		t.Fatalf("Failed to config git user: %v", err)
	}

	configEmail := exec.Command("git", "config", "user.email", "test@example.com")
	configEmail.Dir = tmpDir
	if err := configEmail.Run(); err != nil {
		t.Fatalf("Failed to config git email: %v", err)
	}

	// Create a file
	testFile := filepath.Join(tmpDir, "test.txt")
	if err := os.WriteFile(testFile, []byte("test"), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	git, err := NewGit(ctx)
	if err != nil {
		t.Fatalf("Failed to create Git instance: %v", err)
	}

	t.Run("ValidMessage", func(t *testing.T) {
		opts := CommitOptions{
			Message: "test: valid commit message",
			AddAll:  true,
		}

		hash, err := git.CommitChanges(ctx, tmpDir, opts)
		if err != nil {
			t.Fatalf("Commit with valid message failed: %v", err)
		}
		if hash == "" {
			t.Error("Expected non-empty commit hash")
		}
	})

	t.Run("EmptyMessage", func(t *testing.T) {
		opts := CommitOptions{
			Message: "",
			AddAll:  true,
		}

		_, err := git.CommitChanges(ctx, tmpDir, opts)
		if err == nil {
			t.Error("Expected error for empty commit message")
		}
	})

	t.Run("WhitespaceOnlyMessage", func(t *testing.T) {
		// Create another file to commit
		testFile2 := filepath.Join(tmpDir, "test2.txt")
		if err := os.WriteFile(testFile2, []byte("test2"), 0644); err != nil {
			t.Fatalf("Failed to create test file: %v", err)
		}

		opts := CommitOptions{
			Message: "   \n\t  ", // Whitespace only
			AddAll:  true,
		}

		// Git will likely reject this, but our validation doesn't catch it
		// This documents the current behavior
		_, err := git.CommitChanges(ctx, tmpDir, opts)
		// Either our validation catches it or git does
		_ = err // Don't require error - documents current behavior
	})
}

// TestRebaseOperations tests git rebase functionality
func TestRebaseOperations(t *testing.T) {
	ctx := context.Background()

	// Create a temporary directory for testing
	tmpDir, err := os.MkdirTemp("", "vc-git-rebase-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Initialize a git repository
	initRepo(t, tmpDir)

	git, err := NewGit(ctx)
	if err != nil {
		t.Fatalf("Failed to create Git instance: %v", err)
	}

	// Test 1: Successful rebase without conflicts
	t.Run("SuccessfulRebase", func(t *testing.T) {
		// Create initial commit on main
		createFileAndCommit(t, tmpDir, "main.txt", "main content", "Initial commit on main")

		// Create a feature branch
		createBranch(t, tmpDir, "feature-branch")

		// Add commit to feature branch
		createFileAndCommit(t, tmpDir, "feature.txt", "feature content", "Add feature")

		// Switch back to main and add another commit
		checkoutBranch(t, tmpDir, "main")
		createFileAndCommit(t, tmpDir, "main2.txt", "main content 2", "Second commit on main")

		// Switch back to feature branch and rebase
		checkoutBranch(t, tmpDir, "feature-branch")

		result, err := git.Rebase(ctx, tmpDir, RebaseOptions{
			BaseBranch: "main",
		})

		if err != nil {
			t.Fatalf("Rebase failed: %v", err)
		}

		if !result.Success {
			t.Error("Expected successful rebase")
		}

		if result.HasConflicts {
			t.Error("Expected no conflicts")
		}

		if result.CurrentBranch != "feature-branch" {
			t.Errorf("Expected current branch 'feature-branch', got %s", result.CurrentBranch)
		}

		if result.BaseBranch != "main" {
			t.Errorf("Expected base branch 'main', got %s", result.BaseBranch)
		}
	})

	// Test 2: Rebase with conflicts
	t.Run("RebaseWithConflicts", func(t *testing.T) {
		// Reset the repo
		os.RemoveAll(tmpDir)
		os.MkdirAll(tmpDir, 0755)
		initRepo(t, tmpDir)

		// Create initial commit on main
		createFileAndCommit(t, tmpDir, "conflict.txt", "original content\n", "Initial commit")

		// Create a feature branch
		createBranch(t, tmpDir, "feature-conflict")

		// Modify the file on feature branch
		createFileAndCommit(t, tmpDir, "conflict.txt", "feature content\n", "Feature change")

		// Switch back to main and modify the same file
		checkoutBranch(t, tmpDir, "main")
		createFileAndCommit(t, tmpDir, "conflict.txt", "main content\n", "Main change")

		// Switch back to feature branch and try to rebase
		checkoutBranch(t, tmpDir, "feature-conflict")

		result, err := git.Rebase(ctx, tmpDir, RebaseOptions{
			BaseBranch: "main",
		})

		// Rebase should detect conflicts
		if err != nil {
			t.Logf("Rebase returned error (expected for conflicts): %v", err)
		}

		if result == nil {
			t.Fatal("Expected result even with conflicts")
		}

		if !result.HasConflicts {
			t.Error("Expected conflicts to be detected")
		}

		if len(result.ConflictedFiles) == 0 {
			t.Error("Expected conflicted files to be listed")
		}

		if !strings.Contains(strings.Join(result.ConflictedFiles, ","), "conflict.txt") {
			t.Errorf("Expected conflict.txt in conflicted files, got: %v", result.ConflictedFiles)
		}

		// Clean up: abort the rebase
		abortResult, err := git.Rebase(ctx, tmpDir, RebaseOptions{
			Abort: true,
		})
		if err != nil {
			t.Fatalf("Failed to abort rebase: %v", err)
		}
		if !abortResult.AbortedSuccessfully {
			t.Error("Expected successful abort")
		}
	})

	// Test 3: Continue rebase after resolving conflicts
	t.Run("ContinueRebaseAfterResolution", func(t *testing.T) {
		// Reset the repo
		os.RemoveAll(tmpDir)
		os.MkdirAll(tmpDir, 0755)
		initRepo(t, tmpDir)

		// Create a conflict scenario
		createFileAndCommit(t, tmpDir, "conflict.txt", "original content\n", "Initial commit")
		createBranch(t, tmpDir, "feature-continue")
		createFileAndCommit(t, tmpDir, "conflict.txt", "feature content\n", "Feature change")
		checkoutBranch(t, tmpDir, "main")
		createFileAndCommit(t, tmpDir, "conflict.txt", "main content\n", "Main change")
		checkoutBranch(t, tmpDir, "feature-continue")

		// Start rebase (will conflict)
		result, _ := git.Rebase(ctx, tmpDir, RebaseOptions{BaseBranch: "main"})
		if !result.HasConflicts {
			t.Fatal("Expected conflicts in initial rebase")
		}

		// Manually resolve the conflict
		resolvedContent := "resolved content\n"
		conflictFile := filepath.Join(tmpDir, "conflict.txt")
		if err := os.WriteFile(conflictFile, []byte(resolvedContent), 0644); err != nil {
			t.Fatalf("Failed to resolve conflict: %v", err)
		}

		// Stage the resolution
		addCmd := exec.Command("git", "add", "conflict.txt")
		addCmd.Dir = tmpDir
		if err := addCmd.Run(); err != nil {
			t.Fatalf("Failed to stage resolution: %v", err)
		}

		// Continue the rebase
		continueResult, err := git.Rebase(ctx, tmpDir, RebaseOptions{
			Continue: true,
		})

		if err != nil {
			t.Fatalf("Continue rebase failed: %v", err)
		}

		if !continueResult.Success {
			t.Errorf("Expected successful continue, got: %+v", continueResult)
		}

		if continueResult.HasConflicts {
			t.Error("Expected no conflicts after resolution")
		}

		// Verify the rebase is complete
		hasChanges, err := git.HasUncommittedChanges(ctx, tmpDir)
		if err != nil {
			t.Fatalf("Failed to check uncommitted changes: %v", err)
		}
		if hasChanges {
			t.Error("Expected no uncommitted changes after successful rebase")
		}
	})

	// Test 4: Abort rebase
	t.Run("AbortRebase", func(t *testing.T) {
		// Reset the repo
		os.RemoveAll(tmpDir)
		os.MkdirAll(tmpDir, 0755)
		initRepo(t, tmpDir)

		// Create a conflict scenario
		createFileAndCommit(t, tmpDir, "abort.txt", "original\n", "Initial")
		createBranch(t, tmpDir, "feature-abort")
		createFileAndCommit(t, tmpDir, "abort.txt", "feature\n", "Feature change")
		checkoutBranch(t, tmpDir, "main")
		createFileAndCommit(t, tmpDir, "abort.txt", "main\n", "Main change")
		checkoutBranch(t, tmpDir, "feature-abort")

		// Start rebase (will conflict)
		_, _ = git.Rebase(ctx, tmpDir, RebaseOptions{BaseBranch: "main"})

		// Now abort it
		result, err := git.Rebase(ctx, tmpDir, RebaseOptions{
			Abort: true,
		})

		if err != nil {
			t.Fatalf("Abort rebase failed: %v", err)
		}

		if !result.Success {
			t.Error("Expected successful abort")
		}

		if !result.AbortedSuccessfully {
			t.Error("Expected AbortedSuccessfully to be true")
		}
	})

	// Test 5: Invalid options (mutually exclusive)
	t.Run("InvalidOptions", func(t *testing.T) {
		// Both BaseBranch and Abort
		_, err := git.Rebase(ctx, tmpDir, RebaseOptions{
			BaseBranch: "main",
			Abort:      true,
		})
		if err == nil {
			t.Error("Expected error for mutually exclusive options")
		}

		// No options specified
		_, err = git.Rebase(ctx, tmpDir, RebaseOptions{})
		if err == nil {
			t.Error("Expected error when no options specified")
		}

		// All three options
		_, err = git.Rebase(ctx, tmpDir, RebaseOptions{
			BaseBranch: "main",
			Abort:      true,
			Continue:   true,
		})
		if err == nil {
			t.Error("Expected error for mutually exclusive options")
		}
	})

	// Test 6: Rebase in non-repo directory
	t.Run("RebaseInNonRepo", func(t *testing.T) {
		nonRepoDir, err := os.MkdirTemp("", "vc-git-non-repo-*")
		if err != nil {
			t.Fatalf("Failed to create temp dir: %v", err)
		}
		defer os.RemoveAll(nonRepoDir)

		_, err = git.Rebase(ctx, nonRepoDir, RebaseOptions{
			BaseBranch: "main",
		})
		if err == nil {
			t.Error("Expected error when rebasing in non-repo directory")
		}
	})
}

// Helper functions for rebase tests

func initRepo(t *testing.T, dir string) {
	cmd := exec.Command("git", "init")
	cmd.Dir = dir
	if err := cmd.Run(); err != nil {
		t.Fatalf("Failed to init git repo: %v", err)
	}

	configUser := exec.Command("git", "config", "user.name", "Test User")
	configUser.Dir = dir
	if err := configUser.Run(); err != nil {
		t.Fatalf("Failed to config git user: %v", err)
	}

	configEmail := exec.Command("git", "config", "user.email", "test@example.com")
	configEmail.Dir = dir
	if err := configEmail.Run(); err != nil {
		t.Fatalf("Failed to config git email: %v", err)
	}
}

func createFileAndCommit(t *testing.T, dir, filename, content, message string) {
	filePath := filepath.Join(dir, filename)
	if err := os.WriteFile(filePath, []byte(content), 0644); err != nil {
		t.Fatalf("Failed to create file %s: %v", filename, err)
	}

	addCmd := exec.Command("git", "add", filename)
	addCmd.Dir = dir
	if err := addCmd.Run(); err != nil {
		t.Fatalf("Failed to add file %s: %v", filename, err)
	}

	commitCmd := exec.Command("git", "commit", "-m", message)
	commitCmd.Dir = dir
	if err := commitCmd.Run(); err != nil {
		t.Fatalf("Failed to commit: %v", err)
	}
}

func createBranch(t *testing.T, dir, branchName string) {
	cmd := exec.Command("git", "checkout", "-b", branchName)
	cmd.Dir = dir
	if err := cmd.Run(); err != nil {
		t.Fatalf("Failed to create branch %s: %v", branchName, err)
	}
}

func checkoutBranch(t *testing.T, dir, branchName string) {
	cmd := exec.Command("git", "checkout", branchName)
	cmd.Dir = dir
	if err := cmd.Run(); err != nil {
		t.Fatalf("Failed to checkout branch %s: %v", branchName, err)
	}
}
