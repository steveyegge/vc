package gates

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/steveyegge/vc/internal/ai"
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

// GateProvider is an interface for running quality gates
// This allows for pluggable gate implementations (e.g., for testing or custom gates)
type GateProvider interface {
	// RunAll executes all quality gates in sequence
	// Returns the results and whether all gates passed
	RunAll(ctx context.Context) ([]*Result, bool)
}

// Runner executes quality gates for an issue
type Runner struct {
	store      storage.Storage
	supervisor *ai.Supervisor // Optional: for AI-driven recovery strategies
	workingDir string
	provider   GateProvider // Optional: pluggable gate provider (defaults to built-in)
}

// Config holds quality gate runner configuration
type Config struct {
	Store      storage.Storage
	Supervisor *ai.Supervisor // Optional: enables AI-driven recovery strategies (ZFC)
	WorkingDir string         // Directory where gate commands are executed
	Provider   GateProvider   // Optional: pluggable gate provider (defaults to built-in)
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
		supervisor: cfg.Supervisor,
		workingDir: cfg.WorkingDir,
		provider:   cfg.Provider, // Can be nil (defaults to built-in implementation)
	}, nil
}

// RunAll executes all quality gates in sequence
// Returns the results and whether all gates passed
func (r *Runner) RunAll(ctx context.Context) ([]*Result, bool) {
	// If a custom provider is configured, use it instead of built-in gates
	if r.provider != nil {
		return r.provider.RunAll(ctx)
	}

	// Default implementation: run built-in gates
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

// HandleGateResults processes gate results using AI-driven recovery strategies (ZFC)
// Falls back to hardcoded behavior if supervisor is unavailable
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

	// ZFC: Use AI to determine recovery strategy
	if r.supervisor != nil {
		return r.handleGateResultsWithAI(ctx, originalIssue, results)
	}

	// Fallback: Use hardcoded behavior (backward compatibility)
	fmt.Printf("warning: No AI supervisor configured for quality gates on %s, using fallback logic\n", originalIssue.ID)
	return r.handleGateResultsFallback(ctx, originalIssue, results)
}

// handleGateResultsWithAI uses AI supervisor to determine recovery strategy (ZFC)
func (r *Runner) handleGateResultsWithAI(ctx context.Context, originalIssue *types.Issue, results []*Result) error {
	// Convert gate results to AI format
	var gateFailures []ai.GateFailure
	for _, result := range results {
		if !result.Passed {
			// Truncate output for AI consumption
			output := result.Output
			if len(output) > 1000 {
				output = output[:1000] + "\n... (truncated)"
			}

			errMsg := ""
			if result.Error != nil {
				errMsg = result.Error.Error()
			}

			gateFailures = append(gateFailures, ai.GateFailure{
				Gate:   string(result.Gate),
				Output: output,
				Error:  errMsg,
			})
		}
	}

	// Ask AI for recovery strategy with timeout protection (vc-225)
	// Prevent hanging on AI API issues - fallback after 2 minutes
	aiCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()

	strategy, err := r.supervisor.GenerateRecoveryStrategy(aiCtx, originalIssue, gateFailures)
	if err != nil {
		// If AI fails, fall back to hardcoded behavior
		fmt.Printf("warning: AI recovery strategy failed for %s: %v (falling back)\n", originalIssue.ID, err)
		return r.handleGateResultsFallback(ctx, originalIssue, results)
	}

	// Log the AI's reasoning
	reasoningComment := fmt.Sprintf("**AI Recovery Strategy**\n\n"+
		"Action: %s\n"+
		"Confidence: %.2f\n\n"+
		"Reasoning: %s\n",
		strategy.Action, strategy.Confidence, strategy.Reasoning)
	if err := r.store.AddComment(ctx, originalIssue.ID, "ai-supervisor", reasoningComment); err != nil {
		fmt.Printf("warning: failed to add AI reasoning comment: %v\n", err)
	}

	// Execute the recommended action
	switch strategy.Action {
	case "fix_in_place":
		return r.executeFixInPlace(ctx, originalIssue, strategy)

	case "acceptable_failure":
		return r.executeAcceptableFailure(ctx, originalIssue, strategy)

	case "split_work":
		return r.executeSplitWork(ctx, originalIssue, strategy)

	case "escalate":
		return r.executeEscalate(ctx, originalIssue, strategy)

	case "retry":
		return r.executeRetry(ctx, originalIssue, strategy)

	default:
		fmt.Printf("warning: unknown recovery action '%s' for %s, falling back\n", strategy.Action, originalIssue.ID)
		return r.handleGateResultsFallback(ctx, originalIssue, results)
	}
}

// handleGateResultsFallback uses hardcoded logic (old behavior)
func (r *Runner) handleGateResultsFallback(ctx context.Context, originalIssue *types.Issue, results []*Result) error {
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

// executeFixInPlace creates blocking issues and marks original as blocked
func (r *Runner) executeFixInPlace(ctx context.Context, originalIssue *types.Issue, strategy *ai.RecoveryStrategy) error {
	// Create issues from AI recommendations
	var createdIssues []string
	for _, discoveredIssue := range strategy.CreateIssues {
		issue := &types.Issue{
			Title:              discoveredIssue.Title,
			Description:        discoveredIssue.Description,
			Status:             types.StatusOpen,
			Priority:           originalIssue.Priority, // Inherit priority
			IssueType:          types.TypeBug,
			Design:             "Fix the quality gate failure described above",
			AcceptanceCriteria: "Gate passes without errors",
		}

		if err := r.store.CreateIssue(ctx, issue, "ai-supervisor"); err != nil {
			return fmt.Errorf("failed to create AI-recommended issue: %w", err)
		}

		createdIssues = append(createdIssues, issue.ID)

		// Add blocking dependency
		dep := &types.Dependency{
			IssueID:     originalIssue.ID,
			DependsOnID: issue.ID,
			Type:        types.DepBlocks,
		}
		if err := r.store.AddDependency(ctx, dep, "ai-supervisor"); err != nil {
			return fmt.Errorf("failed to create blocking dependency: %w", err)
		}
	}

	// Mark as blocked if AI recommends it
	if strategy.MarkAsBlocked {
		updates := map[string]interface{}{
			"status": types.StatusBlocked,
		}
		if err := r.store.UpdateIssue(ctx, originalIssue.ID, updates, "ai-supervisor"); err != nil {
			return fmt.Errorf("failed to mark issue as blocked: %w", err)
		}
	}

	// Add AI's comment if provided
	if strategy.AddComment != "" {
		if err := r.store.AddComment(ctx, originalIssue.ID, "ai-supervisor", strategy.AddComment); err != nil {
			fmt.Printf("warning: failed to add AI comment: %v\n", err)
		}
	}

	fmt.Printf("✓ AI recovery (fix_in_place): created %d issue(s) for %s\n", len(createdIssues), originalIssue.ID)
	return nil
}

// executeAcceptableFailure closes the issue despite gate failures
func (r *Runner) executeAcceptableFailure(ctx context.Context, originalIssue *types.Issue, strategy *ai.RecoveryStrategy) error {
	// Add warning comment about acceptable failure
	warningComment := fmt.Sprintf("⚠️ **Quality gates failed but closing anyway (AI decision)**\n\n%s", strategy.AddComment)
	if err := r.store.AddComment(ctx, originalIssue.ID, "ai-supervisor", warningComment); err != nil {
		fmt.Printf("warning: failed to add acceptable failure comment: %v\n", err)
	}

	// Close if AI recommends it (and if not requiring approval)
	if strategy.CloseOriginal && !strategy.RequiresApproval {
		reason := fmt.Sprintf("AI assessed gate failures as acceptable (confidence: %.2f)", strategy.Confidence)
		if err := r.store.CloseIssue(ctx, originalIssue.ID, reason, "ai-supervisor"); err != nil {
			return fmt.Errorf("failed to close issue: %w", err)
		}
		fmt.Printf("✓ AI recovery (acceptable_failure): closed %s despite gate failures\n", originalIssue.ID)
	} else if strategy.RequiresApproval {
		// Add approval request label
		if err := r.store.AddLabel(ctx, originalIssue.ID, "needs-approval", "ai-supervisor"); err != nil {
			fmt.Printf("warning: failed to add needs-approval label: %v\n", err)
		}
		fmt.Printf("⏳ AI recovery (acceptable_failure): %s requires human approval\n", originalIssue.ID)
	}

	return nil
}

// executeSplitWork creates new issues and closes original
func (r *Runner) executeSplitWork(ctx context.Context, originalIssue *types.Issue, strategy *ai.RecoveryStrategy) error {
	// Create issues from AI recommendations
	var createdIssues []string
	for _, discoveredIssue := range strategy.CreateIssues {
		issue := &types.Issue{
			Title:              discoveredIssue.Title,
			Description:        discoveredIssue.Description,
			Status:             types.StatusOpen,
			Priority:           originalIssue.Priority,
			IssueType:          types.TypeBug,
			Design:             "Fix the quality gate failure described above",
			AcceptanceCriteria: "Issue resolved and gates pass",
		}

		if err := r.store.CreateIssue(ctx, issue, "ai-supervisor"); err != nil {
			return fmt.Errorf("failed to create split work issue: %w", err)
		}

		createdIssues = append(createdIssues, issue.ID)

		// Add discovered-from dependency (not blocking)
		dep := &types.Dependency{
			IssueID:     issue.ID,
			DependsOnID: originalIssue.ID,
			Type:        types.DepDiscoveredFrom,
		}
		if err := r.store.AddDependency(ctx, dep, "ai-supervisor"); err != nil {
			fmt.Printf("warning: failed to add discovered-from dependency: %v\n", err)
		}
	}

	// Add comment explaining split
	if strategy.AddComment != "" {
		if err := r.store.AddComment(ctx, originalIssue.ID, "ai-supervisor", strategy.AddComment); err != nil {
			fmt.Printf("warning: failed to add split work comment: %v\n", err)
		}
	}

	// Close original if AI recommends it
	if strategy.CloseOriginal {
		reason := fmt.Sprintf("Work split into %d new issues: %s", len(createdIssues), strings.Join(createdIssues, ", "))
		if err := r.store.CloseIssue(ctx, originalIssue.ID, reason, "ai-supervisor"); err != nil {
			return fmt.Errorf("failed to close original issue: %w", err)
		}
	}

	fmt.Printf("✓ AI recovery (split_work): created %d issue(s) and closed %s\n", len(createdIssues), originalIssue.ID)
	return nil
}

// executeEscalate flags issue for human review
func (r *Runner) executeEscalate(ctx context.Context, originalIssue *types.Issue, strategy *ai.RecoveryStrategy) error {
	// Add escalation comment
	escalationComment := fmt.Sprintf("🚨 **Escalated for human review**\n\n%s", strategy.AddComment)
	if err := r.store.AddComment(ctx, originalIssue.ID, "ai-supervisor", escalationComment); err != nil {
		fmt.Printf("warning: failed to add escalation comment: %v\n", err)
	}

	// Add escalation label
	if err := r.store.AddLabel(ctx, originalIssue.ID, "escalated", "ai-supervisor"); err != nil {
		fmt.Printf("warning: failed to add escalation label: %v\n", err)
	}

	// Mark as blocked if AI recommends it
	if strategy.MarkAsBlocked {
		updates := map[string]interface{}{
			"status": types.StatusBlocked,
		}
		if err := r.store.UpdateIssue(ctx, originalIssue.ID, updates, "ai-supervisor"); err != nil {
			return fmt.Errorf("failed to mark issue as blocked: %w", err)
		}
	}

	fmt.Printf("🚨 AI recovery (escalate): %s flagged for human review\n", originalIssue.ID)
	return nil
}

// executeRetry suggests retry without creating blocking issues
func (r *Runner) executeRetry(ctx context.Context, originalIssue *types.Issue, strategy *ai.RecoveryStrategy) error {
	// Add retry suggestion comment
	retryComment := fmt.Sprintf("🔄 **Retry suggested**\n\n%s\n\nThe issue remains open for retry.", strategy.AddComment)
	if err := r.store.AddComment(ctx, originalIssue.ID, "ai-supervisor", retryComment); err != nil {
		fmt.Printf("warning: failed to add retry comment: %v\n", err)
	}

	// Don't mark as blocked - leave open for retry
	fmt.Printf("🔄 AI recovery (retry): %s left open for retry\n", originalIssue.ID)
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
