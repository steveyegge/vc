package gates

import (
	"context"
	"fmt"
	"os/exec"
	"strings"

	"github.com/steveyegge/vc/internal/storage"
	"github.com/steveyegge/vc/internal/types"
)

// GateType identifies different quality gates
type GateType string

const (
	GateTest   GateType = "test"
	GateLint   GateType = "lint"
	GateBuild  GateType = "build"
)

// Result represents the outcome of a quality gate check
type Result struct {
	Gate    GateType
	Passed  bool
	Output  string
	Error   error
}

// Runner executes quality gates for an issue
type Runner struct {
	store      storage.Storage
	workingDir string
}

// Config holds quality gate runner configuration
type Config struct {
	Store      storage.Storage
	WorkingDir string // Directory where gate commands are executed
}

// NewRunner creates a new quality gate runner
func NewRunner(cfg *Config) (*Runner, error) {
	if cfg.Store == nil {
		return nil, fmt.Errorf("storage is required")
	}
	if cfg.WorkingDir == "" {
		cfg.WorkingDir = "."
	}

	return &Runner{
		store:      cfg.Store,
		workingDir: cfg.WorkingDir,
	}, nil
}

// RunAll executes all quality gates in sequence
// Returns the results and whether all gates passed
func (r *Runner) RunAll(ctx context.Context) ([]*Result, bool) {
	var results []*Result
	allPassed := true

	// Run gates in order: test -> lint -> build
	gates := []struct {
		gateType GateType
		runFunc  func(context.Context) *Result
	}{
		{GateTest, r.runTestGate},
		{GateLint, r.runLintGate},
		{GateBuild, r.runBuildGate},
	}

	for _, gate := range gates {
		result := gate.runFunc(ctx)
		results = append(results, result)

		if !result.Passed {
			allPassed = false
			// Continue running remaining gates even if one fails
			// This gives comprehensive feedback about all quality issues
		}
	}

	return results, allPassed
}

// runTestGate executes go test
func (r *Runner) runTestGate(ctx context.Context) *Result {
	result := &Result{Gate: GateTest}

	cmd := exec.CommandContext(ctx, "go", "test", "./...")
	cmd.Dir = r.workingDir

	output, err := cmd.CombinedOutput()
	result.Output = string(output)

	if err != nil {
		result.Passed = false
		result.Error = fmt.Errorf("go test failed: %w", err)
		return result
	}

	result.Passed = true
	return result
}

// runLintGate executes golangci-lint
func (r *Runner) runLintGate(ctx context.Context) *Result {
	result := &Result{Gate: GateLint}

	// Check if golangci-lint is available
	if _, err := exec.LookPath("golangci-lint"); err != nil {
		result.Passed = false
		result.Error = fmt.Errorf("golangci-lint not found in PATH")
		result.Output = "golangci-lint is not installed or not in PATH"
		return result
	}

	cmd := exec.CommandContext(ctx, "golangci-lint", "run", "./...")
	cmd.Dir = r.workingDir

	output, err := cmd.CombinedOutput()
	result.Output = string(output)

	if err != nil {
		result.Passed = false
		result.Error = fmt.Errorf("golangci-lint failed: %w", err)
		return result
	}

	result.Passed = true
	return result
}

// runBuildGate executes go build
func (r *Runner) runBuildGate(ctx context.Context) *Result {
	result := &Result{Gate: GateBuild}

	cmd := exec.CommandContext(ctx, "go", "build", "./...")
	cmd.Dir = r.workingDir

	output, err := cmd.CombinedOutput()
	result.Output = string(output)

	if err != nil {
		result.Passed = false
		result.Error = fmt.Errorf("go build failed: %w", err)
		return result
	}

	result.Passed = true
	return result
}

// CreateBlockingIssue creates a blocking issue when a gate fails
func (r *Runner) CreateBlockingIssue(ctx context.Context, originalIssue *types.Issue, result *Result) (string, error) {
	// Generate issue ID
	issueID := fmt.Sprintf("%s-gate-%s", originalIssue.ID, result.Gate)

	// Truncate output if too long (keep first 1000 chars)
	output := result.Output
	if len(output) > 1000 {
		output = output[:1000] + "\n... (truncated)"
	}

	// Create the blocking issue
	issue := &types.Issue{
		ID:          issueID,
		Title:       fmt.Sprintf("Quality gate failure: %s for %s", result.Gate, originalIssue.ID),
		Description: fmt.Sprintf("The %s quality gate failed when processing issue %s.\n\nError: %v\n\nOutput:\n```\n%s\n```",
			result.Gate, originalIssue.ID, result.Error, output),
		Status:      types.StatusOpen,
		Priority:    originalIssue.Priority, // Inherit priority from original issue
		IssueType:   types.TypeBug,
		Design:      fmt.Sprintf("Fix the %s failures reported above and ensure the gate passes.", result.Gate),
		AcceptanceCriteria: fmt.Sprintf("- %s gate passes with zero errors\n- Original issue %s can proceed",
			result.Gate, originalIssue.ID),
	}

	if err := r.store.CreateIssue(ctx, issue, "quality-gates"); err != nil {
		return "", fmt.Errorf("failed to create blocking issue: %w", err)
	}

	// Add gate type label
	gateLabel := fmt.Sprintf("gate:%s", result.Gate)
	if err := r.store.AddLabel(ctx, issueID, gateLabel, "quality-gates"); err != nil {
		return "", fmt.Errorf("failed to add gate label: %w", err)
	}

	// Create dependency: originalIssue depends on (is blocked by) the gate issue
	dep := &types.Dependency{
		IssueID:     originalIssue.ID,
		DependsOnID: issueID,
		Type:        types.DepBlocks,
	}
	if err := r.store.AddDependency(ctx, dep, "quality-gates"); err != nil {
		return "", fmt.Errorf("failed to create blocking dependency: %w", err)
	}

	return issueID, nil
}

// HandleGateResults processes gate results, creating blocking issues and updating original issue
func (r *Runner) HandleGateResults(ctx context.Context, originalIssue *types.Issue, results []*Result, allPassed bool) error {
	// Log all gate results as events
	for _, result := range results {
		eventComment := r.formatGateResult(result)
		if err := r.store.AddComment(ctx, originalIssue.ID, "quality-gates", eventComment); err != nil {
			// Don't fail on logging errors
			fmt.Printf("warning: failed to log gate result: %v\n", err)
		}
	}

	// If all gates passed, nothing else to do
	if allPassed {
		successComment := "All quality gates passed:\n- ✓ go test\n- ✓ golangci-lint\n- ✓ go build"
		if err := r.store.AddComment(ctx, originalIssue.ID, "quality-gates", successComment); err != nil {
			fmt.Printf("warning: failed to add success comment: %v\n", err)
		}
		return nil
	}

	// Create blocking issues for each failed gate
	var createdIssues []string
	for _, result := range results {
		if !result.Passed {
			issueID, err := r.CreateBlockingIssue(ctx, originalIssue, result)
			if err != nil {
				return fmt.Errorf("failed to create blocking issue for %s gate: %w", result.Gate, err)
			}
			createdIssues = append(createdIssues, issueID)
		}
	}

	// Update original issue status to blocked
	updates := map[string]interface{}{
		"status": types.StatusBlocked,
	}
	if err := r.store.UpdateIssue(ctx, originalIssue.ID, updates, "quality-gates"); err != nil {
		return fmt.Errorf("failed to update issue to blocked: %w", err)
	}

	// Add summary comment
	summaryComment := fmt.Sprintf("Quality gates failed. Created %d blocking issue(s): %s",
		len(createdIssues), strings.Join(createdIssues, ", "))
	if err := r.store.AddComment(ctx, originalIssue.ID, "quality-gates", summaryComment); err != nil {
		fmt.Printf("warning: failed to add summary comment: %v\n", err)
	}

	return nil
}

// formatGateResult formats a gate result for display
func (r *Runner) formatGateResult(result *Result) string {
	status := "✓ PASSED"
	if !result.Passed {
		status = "✗ FAILED"
	}

	output := result.Output
	if len(output) > 500 {
		output = output[:500] + "\n... (truncated, see blocking issue for full output)"
	}

	comment := fmt.Sprintf("**Quality Gate: %s** - %s\n", result.Gate, status)
	if !result.Passed && result.Error != nil {
		comment += fmt.Sprintf("\nError: %v\n", result.Error)
	}
	if output != "" {
		comment += fmt.Sprintf("\nOutput:\n```\n%s\n```\n", output)
	}

	return comment
}
