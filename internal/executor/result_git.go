package executor

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/steveyegge/vc/internal/gates"
	"github.com/steveyegge/vc/internal/git"
	"github.com/steveyegge/vc/internal/types"
)

// getUncommittedDiff gets the git diff for uncommitted changes
func (rp *ResultsProcessor) getUncommittedDiff(ctx context.Context) (string, error) {
	cmd := exec.CommandContext(ctx, "git", "-C", rp.workingDir, "diff", "HEAD")
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git diff failed: %w", err)
	}
	return string(output), nil
}

// getCommitDiff gets the git diff for a specific commit using git directly
func (rp *ResultsProcessor) getCommitDiff(ctx context.Context, commitHash string) (string, error) {
	// Validate commit hash format to prevent command injection
	if !isValidGitRef(commitHash) {
		return "", fmt.Errorf("invalid commit hash format: %s", commitHash)
	}

	// Check if this commit has a parent (handles first commit case)
	checkParentCmd := exec.CommandContext(ctx, "git", "-C", rp.workingDir,
		"rev-parse", "--verify", "--quiet", commitHash+"^")
	hasParent := checkParentCmd.Run() == nil

	var cmd *exec.Cmd
	if !hasParent {
		// First commit - use git show instead of diff
		cmd = exec.CommandContext(ctx, "git", "-C", rp.workingDir,
			"show", "--format=", commitHash)
	} else {
		// Normal case - diff against parent
		// Use exec.Command with separate args to prevent command injection
		cmd = exec.CommandContext(ctx, "git", "-C", rp.workingDir,
			"diff", commitHash+"^", commitHash)
	}

	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git diff failed: %w", err)
	}

	return string(output), nil
}

// isValidGitRef validates that a git reference is safe to use in commands
// Accepts: commit SHAs (40 hex chars for SHA-1, 64 for SHA-256), short forms (7-40 chars),
// and special refs like HEAD, HEAD~1, etc.
func isValidGitRef(ref string) bool {
	if len(ref) == 0 || len(ref) > 64 {
		return false
	}

	// Allow alphanumeric, -, ~, ^, / (for refs/heads/branch-name)
	// Reject shell metacharacters: ; & | $ ` \ " ' < > ( ) { } [ ] * ? !
	for _, c := range ref {
		if (c < '0' || c > '9') &&
			(c < 'a' || c > 'z') &&
			(c < 'A' || c > 'Z') &&
			c != '-' && c != '_' && c != '/' && c != '~' && c != '^' && c != '.' {
			return false
		}
	}

	return true
}

// getExistingTests finds and reads existing test files to understand test patterns
func (rp *ResultsProcessor) getExistingTests(ctx context.Context) (string, error) {
	// Use git to find test files (handles .gitignore automatically)
	// Look for common test file patterns: *_test.go, *.test.*, test_*.*
	cmd := exec.CommandContext(ctx, "git", "-C", rp.workingDir,
		"ls-files", "*_test.go", "*.test.*", "test_*.*")
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to find test files: %w", err)
	}

	testFiles := strings.Split(strings.TrimSpace(string(output)), "\n")
	if len(testFiles) == 0 || (len(testFiles) == 1 && testFiles[0] == "") {
		return "", fmt.Errorf("no test files found")
	}

	// Read a sample of test files (up to 5 files, max 10KB total)
	var testContent strings.Builder
	const maxFiles = 5
	const maxTotalSize = 10000
	totalSize := 0

	for i, testFile := range testFiles {
		if i >= maxFiles {
			testContent.WriteString(fmt.Sprintf("\n... [%d more test files omitted]\n", len(testFiles)-i))
			break
		}
		if totalSize >= maxTotalSize {
			testContent.WriteString(fmt.Sprintf("\n... [content limit reached, %d files omitted]\n", len(testFiles)-i))
			break
		}

		filePath := filepath.Join(rp.workingDir, testFile)
		content, err := os.ReadFile(filePath)
		if err != nil {
			continue // Skip files we can't read
		}

		// Add this file's content
		testContent.WriteString(fmt.Sprintf("\n=== %s ===\n", testFile))
		contentStr := string(content)
		if len(contentStr)+totalSize > maxTotalSize {
			// Truncate this file to fit within limit (safely, preserving UTF-8)
			remaining := maxTotalSize - totalSize
			if remaining > 0 {
				contentStr = safeTruncateUTF8(contentStr, remaining) + "\n... [truncated]\n"
			} else {
				contentStr = "\n... [truncated]\n"
			}
		}
		testContent.WriteString(contentStr)
		totalSize += len(contentStr)
	}

	return testContent.String(), nil
}

// autoCommit performs auto-commit with AI-generated message.
// Returns the commit hash if successful, empty string if no changes to commit.
func (rp *ResultsProcessor) autoCommit(ctx context.Context, issue *types.Issue) (string, error) {
	fmt.Printf("\n=== Auto-commit ===\n")

	// Wrap git operations with event tracking
	trackedGit, err := git.NewEventTracker(&git.EventTrackerConfig{
		Git:        rp.gitOps,
		Store:      rp.store,
		IssueID:    issue.ID,
		ExecutorID: rp.actor,
		AgentID:    "results-processor",
	})
	if err != nil {
		// Fallback to regular git ops if event tracker fails
		fmt.Fprintf(os.Stderr, "warning: failed to create git event tracker: %v\n", err)
		trackedGit = nil
	}

	// Use tracked git if available, otherwise use regular git ops
	gitOps := rp.gitOps
	if trackedGit != nil {
		gitOps = trackedGit
	}

	// Check for context cancellation before starting auto-commit (vc-25e5)
	if ctx.Err() != nil {
		return "", ctx.Err()
	}

	// Step 1: Check if there are uncommitted changes
	hasChanges, err := gitOps.HasUncommittedChanges(ctx, rp.workingDir)
	if err != nil {
		return "", fmt.Errorf("failed to check for uncommitted changes: %w", err)
	}

	if !hasChanges {
		fmt.Printf("No uncommitted changes detected - skipping commit\n")
		return "", nil
	}

	// Step 2: Get git status to determine changed files
	status, err := gitOps.GetStatus(ctx, rp.workingDir)
	if err != nil {
		return "", fmt.Errorf("failed to get git status: %w", err)
	}

	// Collect all changed files
	changedFiles := append([]string{}, status.Modified...)
	changedFiles = append(changedFiles, status.Added...)
	changedFiles = append(changedFiles, status.Deleted...)
	changedFiles = append(changedFiles, status.Renamed...)
	changedFiles = append(changedFiles, status.Untracked...)

	fmt.Printf("Found %d changed files\n", len(changedFiles))

	// Check for context cancellation before expensive AI operation (vc-25e5)
	if ctx.Err() != nil {
		return "", ctx.Err()
	}

	// Step 3: Generate commit message using AI
	req := git.CommitMessageRequest{
		IssueID:          issue.ID,
		IssueTitle:       issue.Title,
		IssueDescription: issue.Description,
		ChangedFiles:     changedFiles,
		// Note: We're skipping diff for now to keep prompt size manageable
		// Could add: Diff: getDiff() if needed for better messages
	}

	fmt.Printf("Generating commit message via AI...\n")
	msgResponse, err := rp.messageGen.GenerateCommitMessage(ctx, req)
	if err != nil {
		return "", fmt.Errorf("failed to generate commit message: %w", err)
	}

	// Validate commit message
	if msgResponse.Subject == "" {
		return "", fmt.Errorf("AI generated empty commit subject")
	}

	// Build full commit message
	commitMessage := msgResponse.Subject
	if msgResponse.Body != "" {
		commitMessage += "\n\n" + msgResponse.Body
	}

	fmt.Printf("Generated message:\n  Subject: %s\n", msgResponse.Subject)

	// Check for context cancellation before git commit (vc-25e5)
	if ctx.Err() != nil {
		return "", ctx.Err()
	}

	// Step 4: Commit the changes
	commitOpts := git.CommitOptions{
		Message: commitMessage,
		CoAuthors: []string{
			"Claude <noreply@anthropic.com>",
		},
		AddAll:     true, // Stage all changes
		AllowEmpty: false,
	}

	commitHash, err := gitOps.CommitChanges(ctx, rp.workingDir, commitOpts)
	if err != nil {
		return "", fmt.Errorf("failed to commit changes: %w", err)
	}

	return commitHash, nil
}

// isVCRepo checks if the working directory is the VC repository
// This is used to determine if quality gates should run (vc-144)
func (rp *ResultsProcessor) isVCRepo() bool {
	// Check for VC-specific markers:
	// 1. cmd/vc directory (main package)
	// 2. internal/executor directory
	// 3. go.mod with module path containing "steveyegge/vc"

	// Simple heuristic: check if cmd/vc exists
	cmdVCPath := filepath.Join(rp.workingDir, "cmd", "vc")
	if _, err := os.Stat(cmdVCPath); err == nil {
		return true
	}

	// Also check go.mod for module path
	goModPath := filepath.Join(rp.workingDir, "go.mod")
	if data, err := os.ReadFile(goModPath); err == nil {
		if strings.Contains(string(data), "github.com/steveyegge/vc") {
			return true
		}
	}

	return false
}

// createAutoPR creates a GitHub PR using gh CLI after successful auto-commit (vc-389e)
// Returns the PR URL if successful, empty string if PR creation was skipped or failed
func (rp *ResultsProcessor) createAutoPR(ctx context.Context, issue *types.Issue, commitHash string, gateResults []*gates.Result) (string, error) {
	fmt.Printf("\n=== Auto-PR Creation ===\n")

	// Check for context cancellation before network operations (vc-25e5)
	if ctx.Err() != nil {
		return "", ctx.Err()
	}

	// Check if gh CLI is available
	if _, err := exec.LookPath("gh"); err != nil {
		return "", fmt.Errorf("gh CLI not found: %w (install from https://cli.github.com/)", err)
	}

	// Get current branch name
	branchCmd := exec.CommandContext(ctx, "git", "-C", rp.workingDir, "branch", "--show-current")
	branchOutput, err := branchCmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to get current branch: %w", err)
	}
	branchName := strings.TrimSpace(string(branchOutput))

	if branchName == "" || branchName == "main" || branchName == "master" {
		fmt.Printf("Not creating PR: on branch '%s' (PRs only created from feature branches)\n", branchName)
		return "", nil
	}

	// Build PR title from issue
	prTitle := fmt.Sprintf("[%s] %s", issue.ID, issue.Title)

	// Build PR body with quality gate results
	var bodyBuilder strings.Builder
	bodyBuilder.WriteString("## Summary\n\n")
	bodyBuilder.WriteString(fmt.Sprintf("Automated PR for issue %s\n\n", issue.ID))

	if issue.Description != "" {
		bodyBuilder.WriteString(fmt.Sprintf("%s\n\n", issue.Description))
	}

	bodyBuilder.WriteString("## Quality Gates\n\n")
	if len(gateResults) > 0 {
		for _, gr := range gateResults {
			status := "❌ FAIL"
			if gr.Passed {
				status = "✅ PASS"
			}
			bodyBuilder.WriteString(fmt.Sprintf("- %s **%s**\n", status, gr.Gate))
		}
	} else {
		bodyBuilder.WriteString("No quality gates were run.\n")
	}

	bodyBuilder.WriteString(fmt.Sprintf("\n## Commit\n\n- %s\n\n", commitHash))
	bodyBuilder.WriteString("---\n")
	bodyBuilder.WriteString("🤖 Generated with [Claude Code](https://claude.com/claude-code)\n")

	prBody := bodyBuilder.String()

	// Create PR using gh CLI
	cmd := exec.CommandContext(ctx, "gh", "pr", "create",
		"--title", prTitle,
		"--body", prBody,
		"--head", branchName)
	cmd.Dir = rp.workingDir

	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("gh pr create failed: %w\nOutput: %s", err, string(output))
	}

	// Extract PR URL from output
	prURL := strings.TrimSpace(string(output))
	fmt.Printf("✓ Created PR: %s\n", prURL)

	return prURL, nil
}
