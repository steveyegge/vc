package git

import (
	"bufio"
	"context"
	"fmt"
	"os"
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

// Rebase performs a git rebase operation.
// SECURITY: repoPath must be a validated, trusted path. This function
// does not perform path validation or sandboxing.
func (g *Git) Rebase(ctx context.Context, repoPath string, opts RebaseOptions) (*RebaseResult, error) {
	result := &RebaseResult{}

	// Validate mutually exclusive options
	exclusiveCount := 0
	if opts.BaseBranch != "" {
		exclusiveCount++
	}
	if opts.Abort {
		exclusiveCount++
	}
	if opts.Continue {
		exclusiveCount++
	}
	if exclusiveCount != 1 {
		return nil, fmt.Errorf("exactly one of BaseBranch, Abort, or Continue must be specified")
	}

	// Get current branch name for result tracking
	branchCmd := exec.CommandContext(ctx, g.gitPath, "-C", repoPath, "rev-parse", "--abbrev-ref", "HEAD")
	branchOutput, err := branchCmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to get current branch: %w", err)
	}
	result.CurrentBranch = strings.TrimSpace(string(branchOutput))

	// Handle abort case
	if opts.Abort {
		abortCmd := exec.CommandContext(ctx, g.gitPath, "-C", repoPath, "rebase", "--abort")
		if err := abortCmd.Run(); err != nil {
			result.ErrorMessage = fmt.Sprintf("rebase --abort failed: %v", err)
			result.AbortedSuccessfully = false
			return result, fmt.Errorf("git rebase --abort failed in %s: %w", repoPath, err)
		}
		result.Success = true
		result.AbortedSuccessfully = true
		return result, nil
	}

	// Handle continue case
	if opts.Continue {
		continueCmd := exec.CommandContext(ctx, g.gitPath, "-C", repoPath, "rebase", "--continue")
		output, err := continueCmd.CombinedOutput()
		if err != nil {
			outputStr := string(output)

			// Check if there's no rebase in progress
			if strings.Contains(outputStr, "No rebase in progress") {
				result.ErrorMessage = "No rebase in progress"
				return result, fmt.Errorf("no rebase in progress in %s", repoPath)
			}

			// Check if there are still conflicts
			hasConflicts, conflictErr := g.hasConflicts(ctx, repoPath)
			if conflictErr == nil && hasConflicts {
				result.HasConflicts = true
				result.ConflictedFiles = g.getConflictedFiles(ctx, repoPath)
				result.ErrorMessage = "Still has unresolved conflicts"
				return result, nil // Not an error - expected state
			}

			// Some other error occurred
			result.ErrorMessage = fmt.Sprintf("rebase --continue failed: %v\nOutput: %s", err, outputStr)
			return result, fmt.Errorf("git rebase --continue failed in %s: %w", repoPath, err)
		}
		result.Success = true
		return result, nil
	}

	// Handle normal rebase case
	result.BaseBranch = opts.BaseBranch

	// Perform the rebase
	rebaseCmd := exec.CommandContext(ctx, g.gitPath, "-C", repoPath, "rebase", opts.BaseBranch)
	output, err := rebaseCmd.CombinedOutput()

	if err != nil {
		// Rebase failed - check if it's due to conflicts
		hasConflicts, conflictErr := g.hasConflicts(ctx, repoPath)
		if conflictErr != nil {
			result.ErrorMessage = fmt.Sprintf("rebase failed and conflict check failed: %v\nRebase output: %s", conflictErr, string(output))
			return result, fmt.Errorf("git rebase failed in %s and conflict check failed: %w", repoPath, err)
		}

		// Check for merge conflicts
		if hasConflicts {
			result.HasConflicts = true
			result.ConflictedFiles = g.getConflictedFiles(ctx, repoPath)
			result.ErrorMessage = fmt.Sprintf("rebase failed with conflicts: %s", string(output))
			return result, nil // Return nil error since conflicts are expected and handled
		}

		// Some other error occurred
		result.ErrorMessage = fmt.Sprintf("rebase failed: %v\nOutput: %s", err, string(output))
		return result, fmt.Errorf("git rebase failed in %s: %w", repoPath, err)
	}

	// Rebase succeeded
	result.Success = true
	return result, nil
}

// hasConflicts checks if there are unmerged files (merge conflicts).
// This uses git diff --diff-filter=U which specifically checks for unmerged paths.
func (g *Git) hasConflicts(ctx context.Context, repoPath string) (bool, error) {
	// Use git diff to check for unmerged paths (conflicts)
	cmd := exec.CommandContext(ctx, g.gitPath, "-C", repoPath, "diff", "--name-only", "--diff-filter=U")
	output, err := cmd.Output()
	if err != nil {
		// If the command fails, it might be because we're not in a rebase
		// In that case, there are no conflicts
		return false, nil
	}
	return len(strings.TrimSpace(string(output))) > 0, nil
}

// getConflictedFiles returns a list of files with merge conflicts.
func (g *Git) getConflictedFiles(ctx context.Context, repoPath string) []string {
	// Use git diff --name-only --diff-filter=U to find unmerged files
	cmd := exec.CommandContext(ctx, g.gitPath, "-C", repoPath, "diff", "--name-only", "--diff-filter=U")
	output, err := cmd.Output()
	if err != nil {
		return []string{}
	}

	var files []string
	scanner := bufio.NewScanner(strings.NewReader(string(output)))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line != "" {
			files = append(files, line)
		}
	}

	return files
}

// GetConflictDetails parses merge conflicts in files and returns detailed information.
// SECURITY: repoPath in the request must be a validated, trusted path.
func (g *Git) GetConflictDetails(ctx context.Context, req ConflictResolutionRequest) (*ConflictResolutionResult, error) {
	result := &ConflictResolutionResult{
		FileConflicts: make(map[string]*FileConflict),
	}

	for _, filePath := range req.ConflictedFiles {
		fullPath := fmt.Sprintf("%s/%s", req.RepoPath, filePath)

		// Read the file content
		content, err := os.ReadFile(fullPath)
		if err != nil {
			result.ErrorMessage = fmt.Sprintf("failed to read file %s: %v", filePath, err)
			return result, fmt.Errorf("failed to read conflicted file %s: %w", filePath, err)
		}

		// Parse conflicts in the file
		fileConflict := &FileConflict{
			FilePath:    filePath,
			FullContent: string(content),
			Conflicts:   []ConflictMarker{},
		}

		conflicts := g.parseConflictMarkers(string(content))
		fileConflict.Conflicts = conflicts
		result.TotalConflicts += len(conflicts)
		result.FileConflicts[filePath] = fileConflict
	}

	return result, nil
}

// parseConflictMarkers parses conflict markers in file content and returns the conflicts.
func (g *Git) parseConflictMarkers(content string) []ConflictMarker {
	lines := strings.Split(content, "\n")
	var conflicts []ConflictMarker
	var currentConflict *ConflictMarker
	var inOurSection bool

	for lineNum, line := range lines {
		// Check for start of conflict marker
		if strings.HasPrefix(line, "<<<<<<<") {
			if currentConflict != nil {
				// Nested or malformed conflict marker, skip
				continue
			}
			currentConflict = &ConflictMarker{
				StartLine:   lineNum + 1, // 1-indexed
				OursContent: []string{},
				TheirsContent: []string{},
			}
			// Extract label (e.g., "HEAD" from "<<<<<<< HEAD")
			parts := strings.Fields(line)
			if len(parts) > 1 {
				currentConflict.OursLabel = parts[1]
			}
			inOurSection = true
			continue
		}

		// Check for middle separator
		if strings.HasPrefix(line, "=======") && currentConflict != nil {
			currentConflict.MiddleLine = lineNum + 1
			inOurSection = false
			continue
		}

		// Check for end of conflict marker
		if strings.HasPrefix(line, ">>>>>>>") && currentConflict != nil {
			currentConflict.EndLine = lineNum + 1
			// Extract label (e.g., "main" from ">>>>>>> main")
			parts := strings.Fields(line)
			if len(parts) > 1 {
				currentConflict.TheirsLabel = parts[1]
			}
			conflicts = append(conflicts, *currentConflict)
			currentConflict = nil
			inOurSection = false
			continue
		}

		// Add content to the appropriate section
		if currentConflict != nil {
			if inOurSection {
				currentConflict.OursContent = append(currentConflict.OursContent, line)
			} else {
				currentConflict.TheirsContent = append(currentConflict.TheirsContent, line)
			}
		}
	}

	return conflicts
}

// ValidateConflictResolution checks if conflicts have been properly resolved.
// Returns true if no conflict markers remain in the specified files.
// SECURITY: repoPath must be a validated, trusted path.
func (g *Git) ValidateConflictResolution(ctx context.Context, repoPath string, files []string) (bool, error) {
	for _, filePath := range files {
		fullPath := fmt.Sprintf("%s/%s", repoPath, filePath)

		content, err := os.ReadFile(fullPath)
		if err != nil {
			return false, fmt.Errorf("failed to read file %s: %w", filePath, err)
		}

		// Check for any remaining conflict markers
		contentStr := string(content)
		if strings.Contains(contentStr, "<<<<<<<") ||
		   strings.Contains(contentStr, "=======") ||
		   strings.Contains(contentStr, ">>>>>>>") {
			return false, nil
		}
	}

	return true, nil
}
