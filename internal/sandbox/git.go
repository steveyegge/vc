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
func createWorktree(ctx context.Context, cfg SandboxConfig, branchName string) (string, error) {
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
		os.RemoveAll(worktreePath)
		return "", fmt.Errorf("git worktree add failed: %w (output: %s)", err, string(output))
	}

	// Convert to absolute path
	absPath, err := filepath.Abs(worktreePath)
	if err != nil {
		// Try to clean up the worktree we just created
		removeWorktree(ctx, worktreePath)
		return "", fmt.Errorf("failed to get absolute path: %w", err)
	}

	return absPath, nil
}

// removeWorktree removes a git worktree.
// This should be called during sandbox cleanup to remove the isolated workspace.
// It removes both the worktree and prunes the git worktree list.
func removeWorktree(ctx context.Context, worktreePath string) error {
	// Check if path exists
	if _, err := os.Stat(worktreePath); os.IsNotExist(err) {
		// Path doesn't exist, nothing to remove
		return nil
	}

	// First, try to remove the worktree using git command
	// We need to find the parent repo to run this command from
	// For now, we'll try to run it from the worktree itself
	cmd := exec.CommandContext(ctx, "git", "worktree", "remove", worktreePath, "--force")

	output, err := cmd.CombinedOutput()
	if err != nil {
		// If git command fails, fall back to manual removal
		// This can happen if the worktree is already broken
		if err := os.RemoveAll(worktreePath); err != nil {
			return fmt.Errorf("failed to remove worktree directory: %w", err)
		}

		// Try to prune the worktree list
		// This may fail if we can't find the parent repo, but that's okay
		pruneCmd := exec.CommandContext(ctx, "git", "worktree", "prune")
		pruneCmd.Run() // Ignore errors from prune

		return nil
	}

	// Successfully removed via git command
	_ = output // Suppress unused warning
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
