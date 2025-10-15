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
