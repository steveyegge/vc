package sandbox

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// createWorktree creates a git worktree for the sandbox.
// It creates the worktree in detached HEAD state, which allows for flexible branch management.
// The branch should be created separately using createBranch after the worktree is set up.
//
// Returns the absolute path to the created worktree, or an error if creation fails.
func createWorktree(ctx context.Context, cfg SandboxConfig, _ string) (string, error) {
	// Validate parent repo is a git repository
	if err := validateGitRepo(cfg.ParentRepo); err != nil {
		return "", fmt.Errorf("parent repo validation failed: %w", err)
	}

	// Create sandbox directory path
	worktreePath := filepath.Join(cfg.SandboxRoot, "mission-"+cfg.MissionID)

	// Ensure parent directory exists
	if err := os.MkdirAll(cfg.SandboxRoot, 0755); err != nil {
		return "", fmt.Errorf("failed to create sandbox root directory: %w", err)
	}

	// Check if worktree path already exists
	if _, err := os.Stat(worktreePath); err == nil {
		return "", fmt.Errorf("worktree path already exists: %s", worktreePath)
	}

	// Create worktree in detached HEAD state
	// We use --detach to create without automatically creating a branch
	// The branch will be created separately for better control
	cmd := exec.CommandContext(ctx, "git", "worktree", "add", "--detach", worktreePath, cfg.BaseBranch)
	cmd.Dir = cfg.ParentRepo

	output, err := cmd.CombinedOutput()
	if err != nil {
		// Clean up if worktree creation failed but directory was created
		_ = os.RemoveAll(worktreePath) // Best-effort cleanup
		return "", fmt.Errorf("git worktree add failed: %w (output: %s)", err, string(output))
	}

	// Convert to absolute path
	absPath, err := filepath.Abs(worktreePath)
	if err != nil {
		// Try to clean up the worktree we just created
		_ = removeWorktree(ctx, cfg.ParentRepo, worktreePath) // Best-effort cleanup
		return "", fmt.Errorf("failed to get absolute path: %w", err)
	}

	return absPath, nil
}

// removeWorktree removes a git worktree.
// This should be called during sandbox cleanup to remove the isolated workspace.
// It removes both the worktree and prunes the git worktree list.
// The parentRepo parameter is optional - if empty, commands will run without explicit directory context.
func removeWorktree(ctx context.Context, parentRepo, worktreePath string) error {
	// Check if path exists
	if _, err := os.Stat(worktreePath); os.IsNotExist(err) {
		// Path doesn't exist, nothing to remove
		return nil
	}

	// First, try to remove the worktree using git command
	// Run from parent repo if provided for more reliable operation
	cmd := exec.CommandContext(ctx, "git", "worktree", "remove", worktreePath, "--force")
	if parentRepo != "" {
		cmd.Dir = parentRepo
	}

	_, err := cmd.CombinedOutput()
	if err != nil {
		// If git command fails, fall back to manual removal
		// This can happen if the worktree is already broken
		if err := os.RemoveAll(worktreePath); err != nil {
			return fmt.Errorf("failed to remove worktree directory: %w", err)
		}

		// Try to prune the worktree list
		// This may fail if we can't find the parent repo, but that's okay
		pruneCmd := exec.CommandContext(ctx, "git", "worktree", "prune")
		if parentRepo != "" {
			pruneCmd.Dir = parentRepo
		}
		_ = pruneCmd.Run() // Ignore errors from prune

		return nil
	}

	// Successfully removed via git command
	return nil
}

// getGitStatus returns the current git status in the worktree.
// Uses 'git status --porcelain' for machine-readable output.
// Returns empty string if there are no changes.
func getGitStatus(ctx context.Context, worktreePath string) (string, error) {
	// Validate worktree is a git repository
	if err := validateGitRepo(worktreePath); err != nil {
		return "", fmt.Errorf("worktree validation failed: %w", err)
	}

	// Run git status with porcelain format for machine-readable output
	cmd := exec.CommandContext(ctx, "git", "status", "--porcelain")
	cmd.Dir = worktreePath

	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git status failed: %w (output: %s)", err, string(output))
	}

	return strings.TrimSpace(string(output)), nil
}

// getModifiedFiles returns a list of modified files in the worktree.
// This includes both staged and unstaged changes.
// Returns an empty slice if there are no modifications.
func getModifiedFiles(ctx context.Context, worktreePath string) ([]string, error) {
	// Get git status first
	status, err := getGitStatus(ctx, worktreePath)
	if err != nil {
		return nil, err
	}

	// If no changes, return empty slice
	if status == "" {
		return []string{}, nil
	}

	// Parse porcelain output
	// Format is: XY filename
	// Where X is staged status, Y is unstaged status
	// Note: Git quotes filenames with special characters (spaces, quotes, etc.)
	var files []string
	lines := strings.Split(status, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		// Skip the status codes (first 3 characters: "XY ")
		if len(line) < 3 {
			continue
		}

		filename := strings.TrimSpace(line[3:])

		// Remove quotes if present (git quotes filenames with special chars)
		filename = strings.Trim(filename, `"`)

		// Handle renames (format: "old -> new")
		// For renames, return only the new filename
		if strings.Contains(filename, " -> ") {
			parts := strings.Split(filename, " -> ")
			if len(parts) == 2 {
				filename = strings.TrimSpace(parts[1])
			}
		}

		if filename != "" {
			files = append(files, filename)
		}
	}

	return files, nil
}

// createBranch creates a new branch in the worktree.
// This should be called after createWorktree to set up the dedicated mission branch.
// The worktree should already exist and be in detached HEAD state.
func createBranch(ctx context.Context, worktreePath, branchName, baseBranch string) error {
	// Validate worktree is a git repository
	if err := validateGitRepo(worktreePath); err != nil {
		return fmt.Errorf("worktree validation failed: %w", err)
	}

	// Validate branch name
	if err := validateGitRefName(branchName); err != nil {
		return fmt.Errorf("invalid branch name: %w", err)
	}

	// Check if branch already exists (locally)
	checkCmd := exec.CommandContext(ctx, "git", "rev-parse", "--verify", branchName)
	checkCmd.Dir = worktreePath
	if err := checkCmd.Run(); err == nil {
		return fmt.Errorf("branch %s already exists", branchName)
	}

	// Create new branch from base branch
	// We use checkout -b to create and switch to the branch
	cmd := exec.CommandContext(ctx, "git", "checkout", "-b", branchName, baseBranch)
	cmd.Dir = worktreePath

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git checkout -b failed: %w (output: %s)", err, string(output))
	}

	return nil
}

// deleteBranch deletes a branch in the repository.
// This is used to clean up mission branches after sandbox cleanup.
// The branch must not be currently checked out.
func deleteBranch(ctx context.Context, repoPath, branchName string) error {
	// Validate repo is a git repository
	if err := validateGitRepo(repoPath); err != nil {
		return fmt.Errorf("repo validation failed: %w", err)
	}

	// Validate branch name
	if err := validateGitRefName(branchName); err != nil {
		return fmt.Errorf("invalid branch name: %w", err)
	}

	// Check if branch exists
	checkCmd := exec.CommandContext(ctx, "git", "rev-parse", "--verify", branchName)
	checkCmd.Dir = repoPath
	if err := checkCmd.Run(); err != nil {
		// Branch doesn't exist - not an error, just return
		return nil
	}

	// Delete the branch (use -D to force delete even if not fully merged)
	cmd := exec.CommandContext(ctx, "git", "branch", "-D", branchName)
	cmd.Dir = repoPath

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git branch -D failed: %w (output: %s)", err, string(output))
	}

	return nil
}

// validateGitRefName validates that a string is a valid git reference name.
// Git ref names cannot contain certain characters or patterns.
// See git-check-ref-format(1) for full specification.
func validateGitRefName(name string) error {
	if name == "" {
		return fmt.Errorf("ref name cannot be empty")
	}

	// Check for invalid characters and patterns
	invalidChars := []string{" ", "~", "^", ":", "?", "*", "[", "\\", "..", "@{", "//"}
	for _, char := range invalidChars {
		if strings.Contains(name, char) {
			return fmt.Errorf("ref name contains invalid character or pattern: %s", char)
		}
	}

	// Cannot start or end with dot
	if strings.HasPrefix(name, ".") || strings.HasSuffix(name, ".") {
		return fmt.Errorf("ref name cannot start or end with '.'")
	}

	// Cannot end with .lock
	if strings.HasSuffix(name, ".lock") {
		return fmt.Errorf("ref name cannot end with '.lock'")
	}

	// Cannot start with slash
	if strings.HasPrefix(name, "/") {
		return fmt.Errorf("ref name cannot start with '/'")
	}

	// Cannot end with slash
	if strings.HasSuffix(name, "/") {
		return fmt.Errorf("ref name cannot end with '/'")
	}

	return nil
}

// mergeBranchToMain merges a mission branch to the main branch.
// This preserves code changes made during sandbox execution.
// The merge is performed in the parent repository (not the worktree).
//
// Returns an error if the merge fails or if there are conflicts.
// The caller should handle merge conflicts appropriately.
func mergeBranchToMain(ctx context.Context, repoPath, branchName, mainBranch string) error {
	// Validate repo is a git repository
	if err := validateGitRepo(repoPath); err != nil {
		return fmt.Errorf("repo validation failed: %w", err)
	}

	// Validate branch name
	if err := validateGitRefName(branchName); err != nil {
		return fmt.Errorf("invalid branch name: %w", err)
	}

	// Check if branch exists
	checkCmd := exec.CommandContext(ctx, "git", "rev-parse", "--verify", branchName)
	checkCmd.Dir = repoPath
	if err := checkCmd.Run(); err != nil {
		return fmt.Errorf("branch %s does not exist", branchName)
	}

	// Save current branch so we can return to it
	getCurrentBranchCmd := exec.CommandContext(ctx, "git", "rev-parse", "--abbrev-ref", "HEAD")
	getCurrentBranchCmd.Dir = repoPath
	currentBranchOutput, err := getCurrentBranchCmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to get current branch: %w (output: %s)", err, string(currentBranchOutput))
	}
	currentBranch := strings.TrimSpace(string(currentBranchOutput))

	// Checkout main branch
	checkoutCmd := exec.CommandContext(ctx, "git", "checkout", mainBranch)
	checkoutCmd.Dir = repoPath
	output, err := checkoutCmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to checkout %s: %w (output: %s)", mainBranch, err, string(output))
	}

	// Attempt the merge
	// Use --no-ff to always create a merge commit (preserves history)
	mergeCmd := exec.CommandContext(ctx, "git", "merge", "--no-ff", "-m",
		fmt.Sprintf("Merge mission branch %s", branchName), branchName)
	mergeCmd.Dir = repoPath
	mergeOutput, mergeErr := mergeCmd.CombinedOutput()

	// If merge succeeded, we're done
	if mergeErr == nil {
		return nil
	}

	// Merge failed - check if it's due to conflicts
	statusCmd := exec.CommandContext(ctx, "git", "status", "--porcelain")
	statusCmd.Dir = repoPath
	statusOutput, statusErr := statusCmd.CombinedOutput()

	// Try to return to original branch before reporting error
	if currentBranch != mainBranch {
		returnCmd := exec.CommandContext(ctx, "git", "checkout", currentBranch)
		returnCmd.Dir = repoPath
		_ = returnCmd.Run() // Best-effort, ignore error
	}

	// Check if we have merge conflicts
	if statusErr == nil && strings.Contains(string(statusOutput), "UU ") {
		// Abort the merge
		abortCmd := exec.CommandContext(ctx, "git", "merge", "--abort")
		abortCmd.Dir = repoPath
		_ = abortCmd.Run() // Best-effort

		return fmt.Errorf("merge conflicts detected when merging %s to %s: %s",
			branchName, mainBranch, string(mergeOutput))
	}

	// Some other merge error
	return fmt.Errorf("git merge failed: %w (output: %s)", mergeErr, string(mergeOutput))
}

// validateGitRepo checks if a directory is a git repository.
// Returns an error if the path doesn't exist or is not a git repo.
func validateGitRepo(path string) error {
	// Check if path exists
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("path does not exist: %s", path)
		}
		return fmt.Errorf("failed to stat path: %w", err)
	}

	// Check if path is a directory
	if !info.IsDir() {
		return fmt.Errorf("path is not a directory: %s", path)
	}

	// Check if .git exists (works for both regular repos and worktrees)
	// For worktrees, .git is a file that points to the parent repo
	gitPath := filepath.Join(path, ".git")
	if _, err := os.Stat(gitPath); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("not a git repository (no .git found): %s", path)
		}
		return fmt.Errorf("failed to check for .git: %w", err)
	}

	return nil
}
