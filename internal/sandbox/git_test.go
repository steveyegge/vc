package sandbox

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// setupTestRepo creates a temporary git repository for testing.
// Returns the path to the repo and a cleanup function.
func setupTestRepo(t *testing.T) (string, func()) {
	t.Helper()

	// Create temporary directory
	tmpDir, err := os.MkdirTemp("", "vc-git-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}

	cleanup := func() {
		os.RemoveAll(tmpDir)
	}

	// Initialize git repo
	cmd := exec.Command("git", "init")
	cmd.Dir = tmpDir
	if err := cmd.Run(); err != nil {
		cleanup()
		t.Fatalf("Failed to init git repo: %v", err)
	}

	// Configure git user (required for commits)
	configEmail := exec.Command("git", "config", "user.email", "test@example.com")
	configEmail.Dir = tmpDir
	if err := configEmail.Run(); err != nil {
		cleanup()
		t.Fatalf("Failed to configure git email: %v", err)
	}

	configName := exec.Command("git", "config", "user.name", "Test User")
	configName.Dir = tmpDir
	if err := configName.Run(); err != nil {
		cleanup()
		t.Fatalf("Failed to configure git name: %v", err)
	}

	// Create initial commit
	testFile := filepath.Join(tmpDir, "README.md")
	if err := os.WriteFile(testFile, []byte("# Test Repo\n"), 0644); err != nil {
		cleanup()
		t.Fatalf("Failed to create test file: %v", err)
	}

	addCmd := exec.Command("git", "add", "README.md")
	addCmd.Dir = tmpDir
	if err := addCmd.Run(); err != nil {
		cleanup()
		t.Fatalf("Failed to git add: %v", err)
	}

	commitCmd := exec.Command("git", "commit", "-m", "Initial commit")
	commitCmd.Dir = tmpDir
	if err := commitCmd.Run(); err != nil {
		cleanup()
		t.Fatalf("Failed to commit: %v", err)
	}

	return tmpDir, cleanup
}

func TestValidateGitRepo(t *testing.T) {
	// Test with valid repo
	repo, cleanup := setupTestRepo(t)
	defer cleanup()

	if err := validateGitRepo(repo); err != nil {
		t.Errorf("validateGitRepo failed for valid repo: %v", err)
	}

	// Test with non-existent path
	if err := validateGitRepo("/nonexistent/path"); err == nil {
		t.Error("validateGitRepo should fail for non-existent path")
	}

	// Test with non-git directory
	tmpDir, err := os.MkdirTemp("", "not-a-git-repo-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	if err := validateGitRepo(tmpDir); err == nil {
		t.Error("validateGitRepo should fail for non-git directory")
	}
}

func TestCreateWorktree(t *testing.T) {
	repo, cleanup := setupTestRepo(t)
	defer cleanup()

	ctx := context.Background()

	// Create sandbox root
	sandboxRoot, err := os.MkdirTemp("", "vc-sandbox-test-*")
	if err != nil {
		t.Fatalf("Failed to create sandbox root: %v", err)
	}
	defer os.RemoveAll(sandboxRoot)

	cfg := SandboxConfig{
		MissionID:   "test-123",
		ParentRepo:  repo,
		BaseBranch:  "HEAD",
		SandboxRoot: sandboxRoot,
	}

	// Create worktree
	worktreePath, err := createWorktree(ctx, cfg, "mission-test-123")
	if err != nil {
		t.Fatalf("createWorktree failed: %v", err)
	}

	// Verify worktree exists
	if _, err := os.Stat(worktreePath); err != nil {
		t.Errorf("Worktree directory doesn't exist: %v", err)
	}

	// Verify it's a git repo
	if err := validateGitRepo(worktreePath); err != nil {
		t.Errorf("Worktree is not a valid git repo: %v", err)
	}

	// Verify README.md exists in worktree
	readmePath := filepath.Join(worktreePath, "README.md")
	if _, err := os.Stat(readmePath); err != nil {
		t.Errorf("README.md not found in worktree: %v", err)
	}

	// Test creating duplicate worktree (should fail)
	_, err = createWorktree(ctx, cfg, "mission-test-123")
	if err == nil {
		t.Error("createWorktree should fail when path already exists")
	}

	// Clean up worktree
	if err := removeWorktree(ctx, repo, worktreePath); err != nil {
		t.Errorf("Failed to remove worktree: %v", err)
	}
}

func TestRemoveWorktree(t *testing.T) {
	repo, cleanup := setupTestRepo(t)
	defer cleanup()

	ctx := context.Background()

	// Create sandbox root
	sandboxRoot, err := os.MkdirTemp("", "vc-sandbox-test-*")
	if err != nil {
		t.Fatalf("Failed to create sandbox root: %v", err)
	}
	defer os.RemoveAll(sandboxRoot)

	cfg := SandboxConfig{
		MissionID:   "test-456",
		ParentRepo:  repo,
		BaseBranch:  "HEAD",
		SandboxRoot: sandboxRoot,
	}

	// Create worktree
	worktreePath, err := createWorktree(ctx, cfg, "mission-test-456")
	if err != nil {
		t.Fatalf("createWorktree failed: %v", err)
	}

	// Remove worktree
	if err := removeWorktree(ctx, repo, worktreePath); err != nil {
		t.Errorf("removeWorktree failed: %v", err)
	}

	// Verify worktree is gone
	if _, err := os.Stat(worktreePath); !os.IsNotExist(err) {
		t.Error("Worktree directory still exists after removal")
	}

	// Test removing non-existent worktree (should succeed)
	if err := removeWorktree(ctx, repo, worktreePath); err != nil {
		t.Errorf("removeWorktree should succeed for non-existent path: %v", err)
	}
}

func TestGetGitStatus(t *testing.T) {
	repo, cleanup := setupTestRepo(t)
	defer cleanup()

	ctx := context.Background()

	// Create sandbox root
	sandboxRoot, err := os.MkdirTemp("", "vc-sandbox-test-*")
	if err != nil {
		t.Fatalf("Failed to create sandbox root: %v", err)
	}
	defer os.RemoveAll(sandboxRoot)

	cfg := SandboxConfig{
		MissionID:   "test-789",
		ParentRepo:  repo,
		BaseBranch:  "HEAD",
		SandboxRoot: sandboxRoot,
	}

	// Create worktree
	worktreePath, err := createWorktree(ctx, cfg, "mission-test-789")
	if err != nil {
		t.Fatalf("createWorktree failed: %v", err)
	}
	defer removeWorktree(ctx, repo, worktreePath)

	// Get status (should be empty initially)
	status, err := getGitStatus(ctx, worktreePath)
	if err != nil {
		t.Fatalf("getGitStatus failed: %v", err)
	}

	if status != "" {
		t.Errorf("Expected empty status, got: %s", status)
	}

	// Create a new file
	testFile := filepath.Join(worktreePath, "test.txt")
	if err := os.WriteFile(testFile, []byte("test content\n"), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	// Get status again (should show untracked file)
	status, err = getGitStatus(ctx, worktreePath)
	if err != nil {
		t.Fatalf("getGitStatus failed: %v", err)
	}

	if status == "" {
		t.Error("Expected non-empty status after creating file")
	}

	if !strings.Contains(status, "test.txt") {
		t.Errorf("Status should mention test.txt, got: %s", status)
	}
}

func TestGetModifiedFiles(t *testing.T) {
	repo, cleanup := setupTestRepo(t)
	defer cleanup()

	ctx := context.Background()

	// Create sandbox root
	sandboxRoot, err := os.MkdirTemp("", "vc-sandbox-test-*")
	if err != nil {
		t.Fatalf("Failed to create sandbox root: %v", err)
	}
	defer os.RemoveAll(sandboxRoot)

	cfg := SandboxConfig{
		MissionID:   "test-101",
		ParentRepo:  repo,
		BaseBranch:  "HEAD",
		SandboxRoot: sandboxRoot,
	}

	// Create worktree
	worktreePath, err := createWorktree(ctx, cfg, "mission-test-101")
	if err != nil {
		t.Fatalf("createWorktree failed: %v", err)
	}
	defer removeWorktree(ctx, repo, worktreePath)

	// Get modified files (should be empty initially)
	files, err := getModifiedFiles(ctx, worktreePath)
	if err != nil {
		t.Fatalf("getModifiedFiles failed: %v", err)
	}

	if len(files) != 0 {
		t.Errorf("Expected 0 modified files, got %d: %v", len(files), files)
	}

	// Create a new file
	testFile := filepath.Join(worktreePath, "test.txt")
	if err := os.WriteFile(testFile, []byte("test content\n"), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	// Get modified files again
	files, err = getModifiedFiles(ctx, worktreePath)
	if err != nil {
		t.Fatalf("getModifiedFiles failed: %v", err)
	}

	if len(files) != 1 {
		t.Errorf("Expected 1 modified file, got %d: %v", len(files), files)
	}

	if len(files) > 0 && files[0] != "test.txt" {
		t.Errorf("Expected test.txt, got: %s", files[0])
	}

	// Modify existing file
	readmePath := filepath.Join(worktreePath, "README.md")
	if err := os.WriteFile(readmePath, []byte("# Modified\n"), 0644); err != nil {
		t.Fatalf("Failed to modify README.md: %v", err)
	}

	// Get modified files again (should show both)
	files, err = getModifiedFiles(ctx, worktreePath)
	if err != nil {
		t.Fatalf("getModifiedFiles failed: %v", err)
	}

	if len(files) != 2 {
		t.Errorf("Expected 2 modified files, got %d: %v", len(files), files)
	}
}

func TestCreateBranch(t *testing.T) {
	repo, cleanup := setupTestRepo(t)
	defer cleanup()

	ctx := context.Background()

	// Create sandbox root
	sandboxRoot, err := os.MkdirTemp("", "vc-sandbox-test-*")
	if err != nil {
		t.Fatalf("Failed to create sandbox root: %v", err)
	}
	defer os.RemoveAll(sandboxRoot)

	cfg := SandboxConfig{
		MissionID:   "test-202",
		ParentRepo:  repo,
		BaseBranch:  "HEAD",
		SandboxRoot: sandboxRoot,
	}

	// Create worktree
	worktreePath, err := createWorktree(ctx, cfg, "mission-test-202")
	if err != nil {
		t.Fatalf("createWorktree failed: %v", err)
	}
	defer removeWorktree(ctx, repo, worktreePath)

	// Create branch
	branchName := "mission-test-202"
	if err := createBranch(ctx, worktreePath, branchName, "HEAD"); err != nil {
		t.Fatalf("createBranch failed: %v", err)
	}

	// Verify branch exists
	cmd := exec.Command("git", "rev-parse", "--verify", branchName)
	cmd.Dir = worktreePath
	if err := cmd.Run(); err != nil {
		t.Errorf("Branch %s doesn't exist after creation", branchName)
	}

	// Verify we're on the branch
	cmd = exec.Command("git", "branch", "--show-current")
	cmd.Dir = worktreePath
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("Failed to get current branch: %v", err)
	}

	currentBranch := strings.TrimSpace(string(output))
	if currentBranch != branchName {
		t.Errorf("Expected to be on branch %s, got: %s", branchName, currentBranch)
	}

	// Test creating duplicate branch (should fail)
	if err := createBranch(ctx, worktreePath, branchName, "HEAD"); err == nil {
		t.Error("createBranch should fail when branch already exists")
	}
}

func TestCreateWorktreeWithInvalidRepo(t *testing.T) {
	ctx := context.Background()

	// Create sandbox root
	sandboxRoot, err := os.MkdirTemp("", "vc-sandbox-test-*")
	if err != nil {
		t.Fatalf("Failed to create sandbox root: %v", err)
	}
	defer os.RemoveAll(sandboxRoot)

	cfg := SandboxConfig{
		MissionID:   "test-invalid",
		ParentRepo:  "/nonexistent/repo",
		BaseBranch:  "HEAD",
		SandboxRoot: sandboxRoot,
	}

	// Attempt to create worktree (should fail)
	_, err = createWorktree(ctx, cfg, "mission-test-invalid")
	if err == nil {
		t.Error("createWorktree should fail with invalid parent repo")
	}
}

func TestIntegrationWorkflow(t *testing.T) {
	// This test simulates a complete sandbox workflow
	repo, cleanup := setupTestRepo(t)
	defer cleanup()

	ctx := context.Background()

	// Create sandbox root
	sandboxRoot, err := os.MkdirTemp("", "vc-sandbox-test-*")
	if err != nil {
		t.Fatalf("Failed to create sandbox root: %v", err)
	}
	defer os.RemoveAll(sandboxRoot)

	cfg := SandboxConfig{
		MissionID:   "integration-test",
		ParentRepo:  repo,
		BaseBranch:  "HEAD",
		SandboxRoot: sandboxRoot,
	}

	// 1. Create worktree
	worktreePath, err := createWorktree(ctx, cfg, "mission-integration-test")
	if err != nil {
		t.Fatalf("Step 1 - createWorktree failed: %v", err)
	}

	// 2. Create branch
	branchName := "mission-integration-test"
	if err := createBranch(ctx, worktreePath, branchName, "HEAD"); err != nil {
		t.Fatalf("Step 2 - createBranch failed: %v", err)
	}

	// 3. Make some changes
	testFile := filepath.Join(worktreePath, "feature.txt")
	if err := os.WriteFile(testFile, []byte("new feature\n"), 0644); err != nil {
		t.Fatalf("Step 3 - Failed to create feature file: %v", err)
	}

	// 4. Check git status
	status, err := getGitStatus(ctx, worktreePath)
	if err != nil {
		t.Fatalf("Step 4 - getGitStatus failed: %v", err)
	}
	if !strings.Contains(status, "feature.txt") {
		t.Errorf("Step 4 - Status should show feature.txt, got: %s", status)
	}

	// 5. Get modified files
	files, err := getModifiedFiles(ctx, worktreePath)
	if err != nil {
		t.Fatalf("Step 5 - getModifiedFiles failed: %v", err)
	}
	if len(files) != 1 || files[0] != "feature.txt" {
		t.Errorf("Step 5 - Expected [feature.txt], got: %v", files)
	}

	// 6. Clean up worktree
	if err := removeWorktree(ctx, repo, worktreePath); err != nil {
		t.Fatalf("Step 6 - removeWorktree failed: %v", err)
	}

	// 7. Verify cleanup
	if _, err := os.Stat(worktreePath); !os.IsNotExist(err) {
		t.Error("Step 7 - Worktree should be removed")
	}
}

func TestGetModifiedFilesWithSpaces(t *testing.T) {
	repo, cleanup := setupTestRepo(t)
	defer cleanup()

	ctx := context.Background()

	// Create sandbox root
	sandboxRoot, err := os.MkdirTemp("", "vc-sandbox-test-*")
	if err != nil {
		t.Fatalf("Failed to create sandbox root: %v", err)
	}
	defer os.RemoveAll(sandboxRoot)

	cfg := SandboxConfig{
		MissionID:   "test-spaces",
		ParentRepo:  repo,
		BaseBranch:  "HEAD",
		SandboxRoot: sandboxRoot,
	}

	// Create worktree
	worktreePath, err := createWorktree(ctx, cfg, "mission-test-spaces")
	if err != nil {
		t.Fatalf("createWorktree failed: %v", err)
	}
	defer removeWorktree(ctx, repo, worktreePath)

	// Create file with spaces in name
	fileWithSpaces := filepath.Join(worktreePath, "file with spaces.txt")
	if err := os.WriteFile(fileWithSpaces, []byte("content\n"), 0644); err != nil {
		t.Fatalf("Failed to create file with spaces: %v", err)
	}

	// Get modified files
	files, err := getModifiedFiles(ctx, worktreePath)
	if err != nil {
		t.Fatalf("getModifiedFiles failed: %v", err)
	}

	if len(files) != 1 {
		t.Fatalf("Expected 1 file, got %d: %v", len(files), files)
	}

	// Verify filename is returned WITHOUT quotes
	if files[0] != "file with spaces.txt" {
		t.Errorf("Expected 'file with spaces.txt', got: '%s'", files[0])
	}

	// Verify no quotes in the returned filename
	if strings.Contains(files[0], `"`) {
		t.Errorf("Filename should not contain quotes, got: %s", files[0])
	}
}

func TestGetModifiedFilesWithRenames(t *testing.T) {
	repo, cleanup := setupTestRepo(t)
	defer cleanup()

	ctx := context.Background()

	// Create sandbox root
	sandboxRoot, err := os.MkdirTemp("", "vc-sandbox-test-*")
	if err != nil {
		t.Fatalf("Failed to create sandbox root: %v", err)
	}
	defer os.RemoveAll(sandboxRoot)

	cfg := SandboxConfig{
		MissionID:   "test-rename",
		ParentRepo:  repo,
		BaseBranch:  "HEAD",
		SandboxRoot: sandboxRoot,
	}

	// Create worktree
	worktreePath, err := createWorktree(ctx, cfg, "mission-test-rename")
	if err != nil {
		t.Fatalf("createWorktree failed: %v", err)
	}
	defer removeWorktree(ctx, repo, worktreePath)

	// Create a branch
	if err := createBranch(ctx, worktreePath, "test-branch", "HEAD"); err != nil {
		t.Fatalf("createBranch failed: %v", err)
	}

	// Rename README.md to GUIDE.md using git
	mvCmd := exec.Command("git", "mv", "README.md", "GUIDE.md")
	mvCmd.Dir = worktreePath
	if err := mvCmd.Run(); err != nil {
		t.Fatalf("Failed to rename file: %v", err)
	}

	// Get modified files
	files, err := getModifiedFiles(ctx, worktreePath)
	if err != nil {
		t.Fatalf("getModifiedFiles failed: %v", err)
	}

	// Should return only the new filename (GUIDE.md)
	if len(files) != 1 {
		t.Fatalf("Expected 1 file, got %d: %v", len(files), files)
	}

	if files[0] != "GUIDE.md" {
		t.Errorf("Expected 'GUIDE.md' (new name), got: '%s'", files[0])
	}
}

func TestCreateBranchWithInvalidNames(t *testing.T) {
	repo, cleanup := setupTestRepo(t)
	defer cleanup()

	ctx := context.Background()

	// Create sandbox root
	sandboxRoot, err := os.MkdirTemp("", "vc-sandbox-test-*")
	if err != nil {
		t.Fatalf("Failed to create sandbox root: %v", err)
	}
	defer os.RemoveAll(sandboxRoot)

	cfg := SandboxConfig{
		MissionID:   "test-invalid-branch",
		ParentRepo:  repo,
		BaseBranch:  "HEAD",
		SandboxRoot: sandboxRoot,
	}

	// Create worktree
	worktreePath, err := createWorktree(ctx, cfg, "mission-test-invalid-branch")
	if err != nil {
		t.Fatalf("createWorktree failed: %v", err)
	}
	defer removeWorktree(ctx, repo, worktreePath)

	// Test invalid branch names
	invalidNames := []string{
		"",                  // Empty
		"branch with space", // Contains space
		"branch~1",          // Contains tilde
		"branch^1",          // Contains caret
		"branch:name",       // Contains colon
		"branch?name",       // Contains question mark
		"branch*name",       // Contains asterisk
		"branch[0]",         // Contains brackets
		"branch\\name",      // Contains backslash
		"branch..name",      // Contains double dot
		"branch@{name}",     // Contains @{
		"branch//name",      // Contains double slash
		".branch",           // Starts with dot
		"branch.",           // Ends with dot
		"branch.lock",       // Ends with .lock
		"/branch",           // Starts with slash
		"branch/",           // Ends with slash
	}

	for _, name := range invalidNames {
		if err := createBranch(ctx, worktreePath, name, "HEAD"); err == nil {
			t.Errorf("createBranch should fail for invalid branch name: '%s'", name)
		}
	}

	// Test valid branch name (should succeed)
	validName := "feature/valid-branch-123"
	if err := createBranch(ctx, worktreePath, validName, "HEAD"); err != nil {
		t.Errorf("createBranch should succeed for valid branch name: '%s', got error: %v", validName, err)
	}
}
