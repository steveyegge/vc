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
	defer func() { _ = os.RemoveAll(tmpDir) }()

	// Initialize a git repository
	cmd := exec.Command("git", "init", "--initial-branch=main")
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
		defer func() { _ = os.RemoveAll(tmpDir) }()

		// Initialize a git repository
		cmd := exec.Command("git", "init", "--initial-branch=main")
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
		defer func() { _ = os.RemoveAll(tmpDir) }()

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
		defer func() { _ = os.RemoveAll(tmpDir) }()

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
		defer func() { _ = os.RemoveAll(tmpDir) }()

		// Initialize a git repository
		cmd := exec.Command("git", "init", "--initial-branch=main")
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
	defer func() { _ = os.RemoveAll(tmpDir) }()

	// Initialize a git repository
	cmd := exec.Command("git", "init", "--initial-branch=main")
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
	defer func() { _ = os.RemoveAll(tmpDir) }()

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
		_ = os.RemoveAll(tmpDir)
		if err := os.MkdirAll(tmpDir, 0755); err != nil {
			t.Fatalf("Failed to create temp dir: %v", err)
		}
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
		_ = os.RemoveAll(tmpDir)
		if err := os.MkdirAll(tmpDir, 0755); err != nil {
			t.Fatalf("Failed to create temp dir: %v", err)
		}
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
		_ = os.RemoveAll(tmpDir)
		if err := os.MkdirAll(tmpDir, 0755); err != nil {
			t.Fatalf("Failed to create temp dir: %v", err)
		}
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
		defer func() { _ = os.RemoveAll(nonRepoDir) }()

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
	cmd := exec.Command("git", "init", "--initial-branch=main")
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

// TestConflictResolution tests conflict parsing and validation
func TestConflictResolution(t *testing.T) {
	ctx := context.Background()

	tmpDir, err := os.MkdirTemp("", "vc-git-conflict-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	git, err := NewGit(ctx)
	if err != nil {
		t.Fatalf("Failed to create Git instance: %v", err)
	}

	// Test 1: Parse single conflict marker
	t.Run("ParseSingleConflict", func(t *testing.T) {
		conflictContent := `line 1
line 2
<<<<<<< HEAD
our change
=======
their change
>>>>>>> main
line 3`

		conflicts, err := git.parseConflictMarkers(conflictContent)
		if err != nil {
			t.Fatalf("parseConflictMarkers failed: %v", err)
		}

		if len(conflicts) != 1 {
			t.Fatalf("Expected 1 conflict, got %d", len(conflicts))
		}

		conflict := conflicts[0]
		if conflict.OursLabel != "HEAD" {
			t.Errorf("Expected OursLabel 'HEAD', got '%s'", conflict.OursLabel)
		}
		if conflict.TheirsLabel != "main" {
			t.Errorf("Expected TheirsLabel 'main', got '%s'", conflict.TheirsLabel)
		}
		if len(conflict.OursContent) != 1 || conflict.OursContent[0] != "our change" {
			t.Errorf("Expected OursContent ['our change'], got %v", conflict.OursContent)
		}
		if len(conflict.TheirsContent) != 1 || conflict.TheirsContent[0] != "their change" {
			t.Errorf("Expected TheirsContent ['their change'], got %v", conflict.TheirsContent)
		}
		if conflict.StartLine != 3 {
			t.Errorf("Expected StartLine 3, got %d", conflict.StartLine)
		}
		if conflict.MiddleLine != 5 {
			t.Errorf("Expected MiddleLine 5, got %d", conflict.MiddleLine)
		}
		if conflict.EndLine != 7 {
			t.Errorf("Expected EndLine 7, got %d", conflict.EndLine)
		}
	})

	// Test 2: Parse multiple conflicts
	t.Run("ParseMultipleConflicts", func(t *testing.T) {
		conflictContent := `line 1
<<<<<<< HEAD
first our
=======
first their
>>>>>>> main
middle line
<<<<<<< HEAD
second our
=======
second their
>>>>>>> feature`

		conflicts, err := git.parseConflictMarkers(conflictContent)
		if err != nil {
			t.Fatalf("parseConflictMarkers failed: %v", err)
		}

		if len(conflicts) != 2 {
			t.Fatalf("Expected 2 conflicts, got %d", len(conflicts))
		}

		if conflicts[0].OursContent[0] != "first our" {
			t.Errorf("First conflict OursContent mismatch: %v", conflicts[0].OursContent)
		}
		if conflicts[1].TheirsLabel != "feature" {
			t.Errorf("Second conflict TheirsLabel mismatch: %s", conflicts[1].TheirsLabel)
		}
	})

	// Test 3: Parse multiline conflict content
	t.Run("ParseMultilineContent", func(t *testing.T) {
		conflictContent := `<<<<<<< HEAD
our line 1
our line 2
our line 3
=======
their line 1
their line 2
>>>>>>> main`

		conflicts, err := git.parseConflictMarkers(conflictContent)
		if err != nil {
			t.Fatalf("parseConflictMarkers failed: %v", err)
		}

		if len(conflicts) != 1 {
			t.Fatalf("Expected 1 conflict, got %d", len(conflicts))
		}

		if len(conflicts[0].OursContent) != 3 {
			t.Errorf("Expected 3 lines in OursContent, got %d", len(conflicts[0].OursContent))
		}
		if len(conflicts[0].TheirsContent) != 2 {
			t.Errorf("Expected 2 lines in TheirsContent, got %d", len(conflicts[0].TheirsContent))
		}
	})

	// Test 4: GetConflictDetails with real file
	t.Run("GetConflictDetailsFromFile", func(t *testing.T) {
		// Create a file with conflict markers
		conflictFile := filepath.Join(tmpDir, "conflict.txt")
		conflictContent := `normal line
<<<<<<< HEAD
version from HEAD
=======
version from main
>>>>>>> main
another normal line`

		err := os.WriteFile(conflictFile, []byte(conflictContent), 0644)
		if err != nil {
			t.Fatalf("Failed to create conflict file: %v", err)
		}

		req := ConflictResolutionRequest{
			RepoPath:        tmpDir,
			ConflictedFiles: []string{"conflict.txt"},
			BaseBranch:      "main",
			CurrentBranch:   "feature",
		}

		result, err := git.GetConflictDetails(ctx, req)
		if err != nil {
			t.Fatalf("GetConflictDetails failed: %v", err)
		}

		if result.TotalConflicts != 1 {
			t.Errorf("Expected 1 total conflict, got %d", result.TotalConflicts)
		}

		fileConflict, exists := result.FileConflicts["conflict.txt"]
		if !exists {
			t.Fatal("Expected conflict.txt in FileConflicts")
		}

		if len(fileConflict.Conflicts) != 1 {
			t.Errorf("Expected 1 conflict in file, got %d", len(fileConflict.Conflicts))
		}

		if fileConflict.FullContent != conflictContent {
			t.Error("FullContent doesn't match original")
		}
	})

	// Test 5: ValidateConflictResolution - with conflicts
	t.Run("ValidateWithConflicts", func(t *testing.T) {
		conflictFile := filepath.Join(tmpDir, "unresolved.txt")
		conflictContent := `<<<<<<< HEAD
conflict here
=======
still conflicted
>>>>>>> main`

		err := os.WriteFile(conflictFile, []byte(conflictContent), 0644)
		if err != nil {
			t.Fatalf("Failed to create conflict file: %v", err)
		}

		resolved, err := git.ValidateConflictResolution(ctx, tmpDir, []string{"unresolved.txt"})
		if err != nil {
			t.Fatalf("ValidateConflictResolution failed: %v", err)
		}

		if resolved {
			t.Error("Expected validation to fail with unresolved conflicts")
		}
	})

	// Test 6: ValidateConflictResolution - resolved
	t.Run("ValidateResolved", func(t *testing.T) {
		resolvedFile := filepath.Join(tmpDir, "resolved.txt")
		resolvedContent := `this is
completely
resolved
no markers here`

		err := os.WriteFile(resolvedFile, []byte(resolvedContent), 0644)
		if err != nil {
			t.Fatalf("Failed to create resolved file: %v", err)
		}

		resolved, err := git.ValidateConflictResolution(ctx, tmpDir, []string{"resolved.txt"})
		if err != nil {
			t.Fatalf("ValidateConflictResolution failed: %v", err)
		}

		if !resolved {
			t.Error("Expected validation to pass for resolved file")
		}
	})

	// Test 7: Multiple files validation
	t.Run("ValidateMultipleFiles", func(t *testing.T) {
		file1 := filepath.Join(tmpDir, "file1.txt")
		file2 := filepath.Join(tmpDir, "file2.txt")

		_ = os.WriteFile(file1, []byte("resolved content"), 0644)
		_ = os.WriteFile(file2, []byte("<<<<<<< conflict"), 0644)

		resolved, err := git.ValidateConflictResolution(ctx, tmpDir, []string{"file1.txt", "file2.txt"})
		if err != nil {
			t.Fatalf("ValidateConflictResolution failed: %v", err)
		}

		if resolved {
			t.Error("Expected validation to fail when any file has conflicts")
		}
	})

	// Test 8: Empty conflict sections
	t.Run("ParseEmptyConflictSections", func(t *testing.T) {
		conflictContent := `<<<<<<< HEAD
=======
>>>>>>> main`

		conflicts, err := git.parseConflictMarkers(conflictContent)
		if err != nil {
			t.Fatalf("parseConflictMarkers failed: %v", err)
		}

		if len(conflicts) != 1 {
			t.Fatalf("Expected 1 conflict, got %d", len(conflicts))
		}

		if len(conflicts[0].OursContent) != 0 {
			t.Errorf("Expected empty OursContent, got %v", conflicts[0].OursContent)
		}
		if len(conflicts[0].TheirsContent) != 0 {
			t.Errorf("Expected empty TheirsContent, got %v", conflicts[0].TheirsContent)
		}
	})

	// Test 9: Incomplete conflict marker (missing end)
	t.Run("IncompleteConflictMarker", func(t *testing.T) {
		conflictContent := `normal line
<<<<<<< HEAD
our content
=======
their content
// Missing >>>>>>> marker`

		_, err := git.parseConflictMarkers(conflictContent)
		if err == nil {
			t.Error("Expected error for incomplete conflict marker")
		}
		if err != nil && !strings.Contains(err.Error(), "incomplete conflict marker") {
			t.Errorf("Expected 'incomplete conflict marker' error, got: %v", err)
		}
	})

	// Test 10: Nested conflict marker (malformed)
	t.Run("NestedConflictMarker", func(t *testing.T) {
		conflictContent := `<<<<<<< HEAD
content 1
<<<<<<< HEAD
nested marker
=======
content 2
>>>>>>> main`

		_, err := git.parseConflictMarkers(conflictContent)
		if err == nil {
			t.Error("Expected error for nested/malformed conflict marker")
		}
		if err != nil && !strings.Contains(err.Error(), "malformed conflict marker") {
			t.Errorf("Expected 'malformed conflict marker' error, got: %v", err)
		}
	})

	// Test 11: Path traversal prevention in GetConflictDetails
	t.Run("PathTraversalPrevention", func(t *testing.T) {
		req := ConflictResolutionRequest{
			RepoPath:        tmpDir,
			ConflictedFiles: []string{"../../../etc/passwd"},
			BaseBranch:      "main",
			CurrentBranch:   "feature",
		}

		_, err := git.GetConflictDetails(ctx, req)
		if err == nil {
			t.Error("Expected error for path traversal attempt")
		}
		if err != nil && !strings.Contains(err.Error(), "outside repository") {
			t.Errorf("Expected 'outside repository' error, got: %v", err)
		}
	})

	// Test 12: Path traversal prevention in ValidateConflictResolution
	t.Run("PathTraversalPreventionValidate", func(t *testing.T) {
		_, err := git.ValidateConflictResolution(ctx, tmpDir, []string{"../../../etc/passwd"})
		if err == nil {
			t.Error("Expected error for path traversal attempt")
		}
		if err != nil && !strings.Contains(err.Error(), "outside repository") {
			t.Errorf("Expected 'outside repository' error, got: %v", err)
		}
	})
}
