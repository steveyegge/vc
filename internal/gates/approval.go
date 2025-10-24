package gates

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/steveyegge/vc/internal/sandbox"
	"github.com/steveyegge/vc/internal/storage"
	"github.com/steveyegge/vc/internal/types"
)

// ApprovalGate presents sandbox execution results to a human for review
// before allowing code changes to be merged to main.
type ApprovalGate struct {
	store   storage.Storage
	sandbox *sandbox.Sandbox
	issue   *types.Issue
	results []*Result // Quality gate results to display
}

// ApprovalConfig holds configuration for the approval gate
type ApprovalConfig struct {
	Store   storage.Storage
	Sandbox *sandbox.Sandbox
	Issue   *types.Issue
	Results []*Result // Quality gate results from prior gates
}

// NewApprovalGate creates a new approval gate
func NewApprovalGate(cfg *ApprovalConfig) (*ApprovalGate, error) {
	if cfg.Store == nil {
		return nil, fmt.Errorf("storage is required")
	}
	if cfg.Sandbox == nil {
		return nil, fmt.Errorf("sandbox is required")
	}
	if cfg.Issue == nil {
		return nil, fmt.Errorf("issue is required")
	}

	return &ApprovalGate{
		store:   cfg.Store,
		sandbox: cfg.Sandbox,
		issue:   cfg.Issue,
		results: cfg.Results,
	}, nil
}

// Run presents the approval prompt and returns the result
func (g *ApprovalGate) Run(ctx context.Context) *Result {
	result := &Result{
		Gate:   GateType("approval"),
		Passed: false,
	}

	// Check for auto-approve environment variable
	if os.Getenv("VC_AUTO_APPROVE") == "true" {
		result.Passed = true
		result.Output = "Auto-approved via VC_AUTO_APPROVE environment variable"
		return result
	}

	// Build and display summary
	summary, err := g.buildSummary(ctx)
	if err != nil {
		result.Error = fmt.Errorf("failed to build summary: %w", err)
		result.Output = "Error building summary for approval"
		return result
	}

	fmt.Println("\n" + strings.Repeat("=", 80))
	fmt.Println(summary)
	fmt.Println(strings.Repeat("=", 80))

	// Prompt for decision
	for {
		decision, err := g.promptUser("\nApprove merge to main? [y/n/d=show diff]: ")
		if err != nil {
			result.Error = fmt.Errorf("failed to get user input: %w", err)
			result.Output = "Error reading user input"
			return result
		}

		decision = strings.TrimSpace(strings.ToLower(decision))

		switch decision {
		case "y", "yes":
			result.Passed = true
			result.Output = "Approved by user"
			return result

		case "n", "no":
			result.Passed = false
			result.Output = "Rejected by user"
			return result

		case "d", "diff":
			// Show full diff and prompt again
			if err := g.showDiff(ctx); err != nil {
				fmt.Printf("Error showing diff: %v\n", err)
			}
			// Continue loop to prompt again

		default:
			fmt.Printf("Invalid input '%s'. Please enter y, n, or d.\n", decision)
			// Continue loop to prompt again
		}
	}
}

// buildSummary creates a comprehensive summary of the sandbox execution results
func (g *ApprovalGate) buildSummary(ctx context.Context) (string, error) {
	var sb strings.Builder

	// Header
	sb.WriteString(fmt.Sprintf("=== Sandbox Execution Results: %s ===\n\n", g.issue.ID))
	sb.WriteString(fmt.Sprintf("Mission: %s\n", g.issue.Title))
	sb.WriteString(fmt.Sprintf("Branch: %s\n", g.sandbox.GitBranch))
	sb.WriteString(fmt.Sprintf("Status: %s\n\n", g.sandbox.Status))

	// Quality gates results
	if len(g.results) > 0 {
		sb.WriteString("Quality Gates:\n")
		allPassed := true
		for _, r := range g.results {
			status := "✓ PASS"
			if !r.Passed {
				status = "✗ FAIL"
				allPassed = false
			}
			sb.WriteString(fmt.Sprintf("  %s: %s\n", status, r.Gate))
		}
		if allPassed {
			sb.WriteString("\nAll quality gates PASSED ✓\n\n")
		} else {
			sb.WriteString("\n⚠️  Some quality gates FAILED\n\n")
		}
	}

	// Changed files
	changedFiles, diffStats, err := g.getChangedFiles(ctx)
	if err != nil {
		return "", fmt.Errorf("failed to get changed files: %w", err)
	}

	if len(changedFiles) > 0 {
		sb.WriteString(fmt.Sprintf("Changed Files (%d):\n", len(changedFiles)))
		for _, file := range changedFiles {
			sb.WriteString(fmt.Sprintf("  %s\n", file))
		}
		sb.WriteString("\n")
	} else {
		sb.WriteString("Changed Files: None\n\n")
	}

	// Diff stats
	if diffStats != "" {
		sb.WriteString(fmt.Sprintf("Diff Stats:\n%s\n\n", diffStats))
	}

	// Commits
	commits, err := g.getCommits(ctx)
	if err != nil {
		return "", fmt.Errorf("failed to get commits: %w", err)
	}

	if len(commits) > 0 {
		sb.WriteString(fmt.Sprintf("Commits (%d):\n", len(commits)))
		for _, commit := range commits {
			sb.WriteString(fmt.Sprintf("  %s\n", commit))
		}
		sb.WriteString("\n")
	}

	// TODO: Add AI analysis summary once we have a way to query comments
	// For now, skip this section

	return sb.String(), nil
}

// getChangedFiles returns the list of changed files and diff stats
func (g *ApprovalGate) getChangedFiles(ctx context.Context) ([]string, string, error) {
	// Get list of changed files using git diff
	cmd := exec.CommandContext(ctx, "git", "diff", "--name-only", "HEAD")
	cmd.Dir = g.sandbox.Path

	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, "", fmt.Errorf("git diff --name-only failed: %w", err)
	}

	files := []string{}
	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line != "" {
			files = append(files, line)
		}
	}

	// Get diff stats
	cmd = exec.CommandContext(ctx, "git", "diff", "--stat", "HEAD")
	cmd.Dir = g.sandbox.Path

	output, err = cmd.CombinedOutput()
	if err != nil {
		return files, "", fmt.Errorf("git diff --stat failed: %w", err)
	}

	return files, strings.TrimSpace(string(output)), nil
}

// getCommits returns the list of commits on the mission branch
func (g *ApprovalGate) getCommits(ctx context.Context) ([]string, error) {
	// Get commits that are on mission branch but not on main
	cmd := exec.CommandContext(ctx, "git", "log", "--oneline", "main..HEAD")
	cmd.Dir = g.sandbox.Path

	output, err := cmd.CombinedOutput()
	if err != nil {
		// If there are no commits, this might error
		return []string{}, nil
	}

	commits := []string{}
	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line != "" {
			commits = append(commits, line)
		}
	}

	return commits, nil
}

// showDiff displays the full git diff
func (g *ApprovalGate) showDiff(ctx context.Context) error {
	cmd := exec.CommandContext(ctx, "git", "diff", "main..HEAD")
	cmd.Dir = g.sandbox.Path
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	return cmd.Run()
}

// promptUser prompts the user for input and returns their response
func (g *ApprovalGate) promptUser(prompt string) (string, error) {
	fmt.Print(prompt)
	reader := bufio.NewReader(os.Stdin)
	response, err := reader.ReadString('\n')
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(response), nil
}
