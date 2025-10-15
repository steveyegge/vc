package git

import (
	"bufio"
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// Git implements GitOperations using the git CLI.
type Git struct {
	// gitPath is the path to the git executable
	gitPath string
}

// NewGit creates a new Git instance.
// It verifies that git is available on the system.
func NewGit(ctx context.Context) (*Git, error) {
	gitPath, err := exec.LookPath("git")
	if err != nil {
		return nil, fmt.Errorf("git not found in PATH: %w", err)
	}

	// Verify git works
	cmd := exec.CommandContext(ctx, gitPath, "version")
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("git command failed: %w", err)
	}

	return &Git{gitPath: gitPath}, nil
}

// HasUncommittedChanges checks if there are uncommitted changes.
// SECURITY: repoPath must be a validated, trusted path. This function
// does not perform path validation or sandboxing.
func (g *Git) HasUncommittedChanges(ctx context.Context, repoPath string) (bool, error) {
	status, err := g.GetStatus(ctx, repoPath)
	if err != nil {
		return false, fmt.Errorf("failed to check uncommitted changes in %s: %w", repoPath, err)
	}
	return status.HasChanges, nil
}

// GetStatus returns the git status of the repository.
// SECURITY: repoPath must be a validated, trusted path. This function
// does not perform path validation or sandboxing.
func (g *Git) GetStatus(ctx context.Context, repoPath string) (*Status, error) {
	// Use git status --porcelain for machine-readable output
	cmd := exec.CommandContext(ctx, g.gitPath, "-C", repoPath, "status", "--porcelain")
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git status failed in %s: %w", repoPath, err)
	}

	status := &Status{
		Modified:   []string{},
		Untracked:  []string{},
		Deleted:    []string{},
		Added:      []string{},
		Renamed:    []string{},
		HasChanges: false,
	}

	scanner := bufio.NewScanner(strings.NewReader(string(output)))
	for scanner.Scan() {
		line := scanner.Text()
		if len(line) < 3 {
			continue
		}

		statusCode := line[0:2]
		filePath := line[3:]

		// Parse status codes: XY where X=index, Y=working tree
		// Reference: https://git-scm.com/docs/git-status#_short_format
		switch {
		case strings.HasPrefix(statusCode, "??"):
			status.Untracked = append(status.Untracked, filePath)
		case strings.HasPrefix(statusCode, "A "), strings.HasPrefix(statusCode, "AM"):
			status.Added = append(status.Added, filePath)
		case strings.HasPrefix(statusCode, "M "), strings.HasPrefix(statusCode, " M"), strings.HasPrefix(statusCode, "MM"):
			status.Modified = append(status.Modified, filePath)
		case strings.HasPrefix(statusCode, "D "), strings.HasPrefix(statusCode, " D"):
			status.Deleted = append(status.Deleted, filePath)
		case strings.HasPrefix(statusCode, "R "):
			status.Renamed = append(status.Renamed, filePath)
		default:
			// Other changes (copied, updated but unmerged, etc.)
			status.Modified = append(status.Modified, filePath)
		}

		status.HasChanges = true
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("failed to parse git status: %w", err)
	}

	return status, nil
}

// CommitChanges creates a git commit.
// SECURITY: repoPath must be a validated, trusted path. This function
// does not perform path validation or sandboxing.
func (g *Git) CommitChanges(ctx context.Context, repoPath string, opts CommitOptions) (string, error) {
	if opts.Message == "" {
		return "", fmt.Errorf("commit message is required")
	}

	// Stage changes if requested
	if opts.AddAll {
		addCmd := exec.CommandContext(ctx, g.gitPath, "-C", repoPath, "add", "-A")
		if err := addCmd.Run(); err != nil {
			return "", fmt.Errorf("git add failed in %s: %w", repoPath, err)
		}
	}

	// Build commit message with co-authors
	message := opts.Message
	if len(opts.CoAuthors) > 0 {
		message += "\n"
		for _, coAuthor := range opts.CoAuthors {
			message += fmt.Sprintf("\nCo-Authored-By: %s", coAuthor)
		}
	}

	// Build commit command
	args := []string{"-C", repoPath, "commit", "-m", message}
	if opts.Author != "" {
		args = append(args, "--author", opts.Author)
	}
	if opts.AllowEmpty {
		args = append(args, "--allow-empty")
	}

	commitCmd := exec.CommandContext(ctx, g.gitPath, args...)
	if err := commitCmd.Run(); err != nil {
		return "", fmt.Errorf("git commit failed in %s: %w", repoPath, err)
	}

	// Get the commit hash
	hashCmd := exec.CommandContext(ctx, g.gitPath, "-C", repoPath, "rev-parse", "HEAD")
	hashOutput, err := hashCmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to get commit hash in %s: %w", repoPath, err)
	}

	commitHash := strings.TrimSpace(string(hashOutput))
	return commitHash, nil
}

// GetDiff returns the git diff output for the repository.
// This can be used to provide context to the AI for commit message generation.
// SECURITY: repoPath must be a validated, trusted path. This function
// does not perform path validation or sandboxing.
func (g *Git) GetDiff(ctx context.Context, repoPath string, staged bool) (string, error) {
	args := []string{"-C", repoPath, "diff"}
	if staged {
		args = append(args, "--staged")
	}

	cmd := exec.CommandContext(ctx, g.gitPath, args...)
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git diff failed in %s: %w", repoPath, err)
	}

	return string(output), nil
}
