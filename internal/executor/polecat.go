package executor

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/steveyegge/vc/internal/ai"
	"github.com/steveyegge/vc/internal/storage"
	"github.com/steveyegge/vc/internal/types"
)

// PolecatExecutor handles single-task execution in polecat mode.
//
// This executor is designed for Gastown integration where VC runs inside
// a polecat (isolated clone). Key differences from the regular Executor:
//
// - No issue claiming or polling - task is provided directly
// - No sandbox creation - uses current directory (polecat's clone)
// - Single execution then exit - no continuous loop
// - JSON output to stdout - structured result for polecat wrapper
// - No database writes - polecat wrapper handles beads updates
//
// See docs/design/GASTOWN_INTEGRATION.md for full specification.
type PolecatExecutor struct {
	store      storage.Storage    // Optional: for loading issues, not for writes
	supervisor *ai.Supervisor     // AI supervisor for assessment and analysis
	config     *PolecatConfig
}

// PolecatConfig holds configuration for polecat mode execution
type PolecatConfig struct {
	// Store is optional - used for loading issues via --issue flag
	Store storage.Storage

	// WorkingDir is the directory where execution happens (default: current dir)
	WorkingDir string

	// EnablePreflight enables baseline health check before starting (default: true)
	EnablePreflight bool

	// EnableAssessment enables AI assessment phase (default: true)
	EnableAssessment bool

	// EnableQualityGates enables quality gates after execution (default: true)
	EnableQualityGates bool

	// GatesTimeout is the timeout for quality gate execution (default: 5m)
	GatesTimeout time.Duration

	// MaxIterations is the maximum refinement iterations (default: 7)
	MaxIterations int

	// MinIterations is the minimum iterations before checking convergence (default: 1)
	MinIterations int

	// AgentTimeout is the timeout for agent execution (default: 30m)
	AgentTimeout time.Duration
}

// DefaultPolecatConfig returns sensible defaults for polecat mode
func DefaultPolecatConfig() *PolecatConfig {
	return &PolecatConfig{
		WorkingDir:         ".",
		EnablePreflight:    true,
		EnableAssessment:   true,
		EnableQualityGates: true,
		GatesTimeout:       5 * time.Minute,
		MaxIterations:      7,
		MinIterations:      1,
		AgentTimeout:       30 * time.Minute,
	}
}

// NewPolecatExecutor creates a new polecat mode executor
func NewPolecatExecutor(cfg *PolecatConfig) (*PolecatExecutor, error) {
	if cfg == nil {
		cfg = DefaultPolecatConfig()
	}

	// Set defaults for unspecified values
	if cfg.WorkingDir == "" {
		cfg.WorkingDir = "."
	}
	if cfg.GatesTimeout == 0 {
		cfg.GatesTimeout = 5 * time.Minute
	}
	if cfg.MaxIterations == 0 {
		cfg.MaxIterations = 7
	}
	if cfg.AgentTimeout == 0 {
		cfg.AgentTimeout = 30 * time.Minute
	}

	pe := &PolecatExecutor{
		store:  cfg.Store,
		config: cfg,
	}

	// Initialize AI supervisor if API key is available
	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey != "" {
		supervisor, err := ai.NewSupervisor(&ai.Config{
			Store: cfg.Store, // Can be nil in polecat mode
		})
		if err != nil {
			// Log warning but continue without supervision
			fmt.Fprintf(os.Stderr, "Warning: failed to initialize AI supervisor: %v (continuing without supervision)\n", err)
		} else {
			pe.supervisor = supervisor
		}
	}

	return pe, nil
}

// Execute runs a single task in polecat mode and returns the result.
//
// The execution flow is:
// 1. Preflight: Check if baseline is healthy (optional)
// 2. Assess: AI evaluation of task complexity (optional)
// 3. Execute: Spawn coding agent to do the work
// 4. Iterate: Refine until convergence (single pass for lite mode)
// 5. Gates: Run quality gates (test, lint, build)
//
// Returns a PolecatResult suitable for JSON serialization to stdout.
func (pe *PolecatExecutor) Execute(ctx context.Context, task *types.PolecatTask) *types.PolecatResult {
	startTime := time.Now()
	result := types.NewPolecatResult()

	// Validate task
	if task == nil {
		result.SetFailed("task is required")
		return result
	}
	if task.Description == "" {
		result.SetFailed("task description is required")
		return result
	}

	// Log to stderr (not stdout - that's for JSON output)
	fmt.Fprintf(os.Stderr, "\n=== Polecat Mode Execution ===\n")
	fmt.Fprintf(os.Stderr, "Task: %s\n", truncateForLog(task.Description, 100))
	fmt.Fprintf(os.Stderr, "Source: %s\n", task.Source)
	if task.IssueID != "" {
		fmt.Fprintf(os.Stderr, "Issue: %s\n", task.IssueID)
	}

	// Step 1: Preflight check (if enabled)
	if pe.config.EnablePreflight {
		fmt.Fprintf(os.Stderr, "\n--- Preflight Check ---\n")
		preflightPassed, preflightResults := pe.runPreflight(ctx)
		if !preflightPassed {
			result.SetBlocked("baseline quality gates failing", "Fix baseline failures before running VC")
			result.PreflightResult = preflightResults
			result.DurationSeconds = time.Since(startTime).Seconds()
			return result
		}
		fmt.Fprintf(os.Stderr, "✓ Baseline healthy\n")
	}

	// Step 2: Assessment (if enabled and supervisor available)
	var assessment *ai.Assessment
	if pe.config.EnableAssessment && pe.supervisor != nil {
		fmt.Fprintf(os.Stderr, "\n--- AI Assessment ---\n")
		var err error
		assessment, err = pe.assessTask(ctx, task)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: assessment failed: %v (continuing without assessment)\n", err)
		} else if assessment != nil {
			fmt.Fprintf(os.Stderr, "Strategy: %s\n", assessment.Strategy)
			fmt.Fprintf(os.Stderr, "Confidence: %.0f%%\n", assessment.Confidence*100)

			// Check for decomposition recommendation
			if assessment.ShouldDecompose && assessment.DecompositionPlan != nil {
				fmt.Fprintf(os.Stderr, "Task decomposition recommended\n")
				result.SetDecomposed(
					assessment.DecompositionPlan.Reasoning,
					convertDecompositionToSubtasks(assessment.DecompositionPlan),
				)
				result.DurationSeconds = time.Since(startTime).Seconds()
				result.Message = fmt.Sprintf("Task decomposed into %d subtasks", len(result.Decomposition.Subtasks))
				return result
			}
		}
	}

	// Step 3: Execute agent
	fmt.Fprintf(os.Stderr, "\n--- Agent Execution ---\n")
	agentResult, filesModified, err := pe.executeAgent(ctx, task, assessment)
	if err != nil {
		result.SetFailed(fmt.Sprintf("agent execution failed: %v", err))
		result.DurationSeconds = time.Since(startTime).Seconds()
		return result
	}

	result.FilesModified = filesModified
	result.Iterations = 1 // Initial execution counts as iteration 1

	// Check if agent succeeded
	if !agentResult.Success {
		result.Status = types.PolecatStatusPartial
		result.Message = "Agent execution completed with errors"
		result.Summary = summarizeAgentOutput(agentResult)
	}

	// Step 4: AI Analysis (if supervisor available)
	var analysis *ai.Analysis
	if pe.supervisor != nil && agentResult.Success {
		fmt.Fprintf(os.Stderr, "\n--- AI Analysis ---\n")
		analysis, err = pe.analyzeResult(ctx, task, agentResult)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: analysis failed: %v\n", err)
		} else if analysis != nil {
			// Extract discovered issues
			for _, di := range analysis.DiscoveredIssues {
				// Parse priority string to int (P0=0, P1=1, etc.)
				priority := parsePriorityString(di.Priority)
				result.AddDiscoveredIssue(di.Title, di.Description, di.Type, priority)
			}
			// Extract punted items
			result.PuntedItems = analysis.PuntedItems
			result.Summary = analysis.Summary
			// Note: Analysis doesn't have Converged field, we determine convergence from Completed
			result.Converged = analysis.Completed

			fmt.Fprintf(os.Stderr, "Completed: %v\n", analysis.Completed)
			fmt.Fprintf(os.Stderr, "Discovered issues: %d\n", len(analysis.DiscoveredIssues))
		}
	}

	// Step 5: Quality Gates (if enabled)
	if pe.config.EnableQualityGates {
		fmt.Fprintf(os.Stderr, "\n--- Quality Gates ---\n")
		gatesPassed, gateResults := pe.runQualityGates(ctx)
		result.QualityGates = gateResults

		if !gatesPassed {
			result.Status = types.PolecatStatusFailed
			result.Success = false
			result.Message = "Quality gates failed"
			result.DurationSeconds = time.Since(startTime).Seconds()
			return result
		}
		fmt.Fprintf(os.Stderr, "✓ All quality gates passed\n")
	}

	// Success path
	result.DurationSeconds = time.Since(startTime).Seconds()

	if result.Converged && result.AllGatesPassed() {
		result.SetCompleted(result.Summary)
		if result.Summary == "" {
			result.Summary = "Task completed successfully"
		}
	} else if agentResult.Success {
		result.Status = types.PolecatStatusPartial
		result.Success = false
		if result.Message == "" {
			result.Message = "Task completed but may need review"
		}
	}

	fmt.Fprintf(os.Stderr, "\n=== Execution Complete ===\n")
	fmt.Fprintf(os.Stderr, "Status: %s\n", result.Status)
	fmt.Fprintf(os.Stderr, "Duration: %.1fs\n", result.DurationSeconds)

	return result
}

// runPreflight checks if the baseline (main branch) passes quality gates
func (pe *PolecatExecutor) runPreflight(ctx context.Context) (bool, map[string]types.PolecatGateResult) {
	results := make(map[string]types.PolecatGateResult)

	// Run quick health check - just build and lint, skip full test suite for speed
	gates := []struct {
		name    string
		command string
		args    []string
	}{
		{"build", "go", []string{"build", "./..."}},
		{"lint", "golangci-lint", []string{"run", "--timeout", "2m", "./..."}},
	}

	allPassed := true
	for _, gate := range gates {
		cmd := exec.CommandContext(ctx, gate.command, gate.args...)
		cmd.Dir = pe.config.WorkingDir
		output, err := cmd.CombinedOutput()

		passed := err == nil
		errStr := ""
		if err != nil {
			errStr = err.Error()
			allPassed = false
		}

		results[gate.name] = types.PolecatGateResult{
			Passed: passed,
			Output: string(output),
			Error:  errStr,
		}

		if passed {
			fmt.Fprintf(os.Stderr, "  ✓ %s passed\n", gate.name)
		} else {
			fmt.Fprintf(os.Stderr, "  ✗ %s failed\n", gate.name)
		}
	}

	return allPassed, results
}

// assessTask uses AI to evaluate task complexity and suggest approach
func (pe *PolecatExecutor) assessTask(ctx context.Context, task *types.PolecatTask) (*ai.Assessment, error) {
	// Create a synthetic issue for assessment
	issue := &types.Issue{
		ID:                 task.IssueID,
		Title:              extractTitle(task.Description),
		Description:        task.Description,
		AcceptanceCriteria: task.AcceptanceCriteria,
		IssueType:          types.TypeTask,
		Priority:           2, // Medium priority (P2)
	}

	return pe.supervisor.AssessIssueState(ctx, issue)
}

// executeAgent spawns a coding agent to execute the task
func (pe *PolecatExecutor) executeAgent(ctx context.Context, task *types.PolecatTask, assessment *ai.Assessment) (*AgentResult, []string, error) {
	// Build the prompt
	prompt := pe.buildPrompt(task, assessment)

	// Log prompt for debugging if enabled
	if os.Getenv("VC_DEBUG_PROMPTS") != "" {
		fmt.Fprintf(os.Stderr, "\n=== AGENT PROMPT ===\n%s\n=== END PROMPT ===\n\n", prompt)
	}

	// Create a synthetic issue for agent config
	issue := &types.Issue{
		ID:    task.IssueID,
		Title: extractTitle(task.Description),
	}
	if issue.ID == "" {
		issue.ID = fmt.Sprintf("polecat-%s", uuid.New().String()[:8])
	}

	agentCfg := AgentConfig{
		Type:       AgentTypeClaudeCode,
		WorkingDir: pe.config.WorkingDir,
		Issue:      issue,
		StreamJSON: true,
		Timeout:    pe.config.AgentTimeout,
		Store:      nil, // Explicitly nil - polecat mode makes no database writes (vc-4bql)
		ExecutorID: "polecat",
		AgentID:    uuid.New().String(),
	}

	agent, err := SpawnAgent(ctx, agentCfg, prompt)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to spawn agent: %w", err)
	}

	result, err := agent.Wait(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("agent wait failed: %w", err)
	}

	// Get modified files from git
	filesModified := pe.getModifiedFiles()

	return result, filesModified, nil
}

// buildPrompt constructs the agent prompt from task and optional assessment
func (pe *PolecatExecutor) buildPrompt(task *types.PolecatTask, assessment *ai.Assessment) string {
	var sb strings.Builder

	sb.WriteString("# Task\n\n")
	sb.WriteString(task.Description)
	sb.WriteString("\n\n")

	if task.AcceptanceCriteria != "" {
		sb.WriteString("# Acceptance Criteria\n\n")
		sb.WriteString(task.AcceptanceCriteria)
		sb.WriteString("\n\n")
	}

	if assessment != nil {
		sb.WriteString("# AI Assessment\n\n")
		sb.WriteString(fmt.Sprintf("Strategy: %s\n\n", assessment.Strategy))

		if len(assessment.Steps) > 0 {
			sb.WriteString("Recommended Steps:\n")
			for i, step := range assessment.Steps {
				sb.WriteString(fmt.Sprintf("%d. %s\n", i+1, step))
			}
			sb.WriteString("\n")
		}

		if len(assessment.Risks) > 0 {
			sb.WriteString("Risks to consider:\n")
			for _, risk := range assessment.Risks {
				sb.WriteString(fmt.Sprintf("- %s\n", risk))
			}
			sb.WriteString("\n")
		}
	}

	sb.WriteString("# Instructions\n\n")
	sb.WriteString("Complete this task by making the necessary code changes.\n")
	sb.WriteString("When done, provide a brief summary of what was accomplished.\n")
	sb.WriteString("If you discover related issues or improvements, note them for follow-up.\n")

	return sb.String()
}

// analyzeResult uses AI to analyze agent output and extract insights
func (pe *PolecatExecutor) analyzeResult(ctx context.Context, task *types.PolecatTask, agentResult *AgentResult) (*ai.Analysis, error) {
	// Create synthetic issue for analysis
	issue := &types.Issue{
		ID:          task.IssueID,
		Title:       extractTitle(task.Description),
		Description: task.Description,
	}
	if issue.ID == "" {
		issue.ID = "polecat-task"
	}

	// Combine output for analysis
	output := strings.Join(agentResult.Output, "\n")

	// AnalyzeExecutionResult is the correct method for post-execution analysis
	return pe.supervisor.AnalyzeExecutionResult(ctx, issue, output, agentResult.Success)
}

// runQualityGates executes test, lint, and build gates
func (pe *PolecatExecutor) runQualityGates(ctx context.Context) (bool, map[string]types.PolecatGateResult) {
	results := make(map[string]types.PolecatGateResult)

	// Create timeout context for gates
	gatesCtx, cancel := context.WithTimeout(ctx, pe.config.GatesTimeout)
	defer cancel()

	// Run gates in order: build -> test -> lint
	gatesList := []struct {
		name    string
		command string
		args    []string
	}{
		{"build", "go", []string{"build", "./..."}},
		{"test", "go", []string{"test", "./..."}},
		{"lint", "golangci-lint", []string{"run", "--timeout", "2m", "./..."}},
	}

	allPassed := true
	for _, gate := range gatesList {
		cmd := exec.CommandContext(gatesCtx, gate.command, gate.args...)
		cmd.Dir = pe.config.WorkingDir
		output, err := cmd.CombinedOutput()

		passed := err == nil
		errStr := ""
		if err != nil {
			errStr = err.Error()
			allPassed = false
		}

		results[gate.name] = types.PolecatGateResult{
			Passed: passed,
			Output: truncateForLog(string(output), 5000), // Limit output size
			Error:  errStr,
		}

		if passed {
			fmt.Fprintf(os.Stderr, "  ✓ %s passed\n", gate.name)
		} else {
			fmt.Fprintf(os.Stderr, "  ✗ %s failed\n", gate.name)
		}
	}

	return allPassed, results
}

// getModifiedFiles returns list of files modified in current working directory
func (pe *PolecatExecutor) getModifiedFiles() []string {
	cmd := exec.Command("git", "diff", "--name-only", "HEAD")
	cmd.Dir = pe.config.WorkingDir
	output, err := cmd.Output()
	if err != nil {
		return []string{}
	}

	files := strings.Split(strings.TrimSpace(string(output)), "\n")
	// Filter out empty strings
	result := make([]string, 0, len(files))
	for _, f := range files {
		if f != "" {
			result = append(result, f)
		}
	}
	return result
}

// OutputJSON writes the result as JSON to stdout
func (pe *PolecatExecutor) OutputJSON(result *types.PolecatResult) error {
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(result)
}

// Helper functions

// truncateForLog truncates a string for log output
func truncateForLog(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

// extractTitle extracts a short title from a task description
func extractTitle(description string) string {
	// Take first line or first 80 characters
	lines := strings.SplitN(description, "\n", 2)
	title := lines[0]
	if len(title) > 80 {
		title = title[:80] + "..."
	}
	return title
}

// summarizeAgentOutput creates a brief summary from agent output
func summarizeAgentOutput(result *AgentResult) string {
	if len(result.Output) == 0 {
		return "No output captured"
	}

	// Take last few lines as they usually contain the summary
	start := len(result.Output) - 10
	if start < 0 {
		start = 0
	}
	return strings.Join(result.Output[start:], "\n")
}

// convertDecompositionToSubtasks converts AI decomposition plan to polecat subtasks
func convertDecompositionToSubtasks(plan *ai.DecompositionPlan) []types.PolecatSubtask {
	subtasks := make([]types.PolecatSubtask, len(plan.ChildIssues))
	for i, child := range plan.ChildIssues {
		subtasks[i] = types.PolecatSubtask{
			Title:    child.Title,
			Priority: child.Priority,
		}
	}
	return subtasks
}

// parsePriorityString converts a priority string like "P0", "P1", etc. to an int
func parsePriorityString(priority string) int {
	// Try to parse as "P0", "P1", etc.
	if len(priority) >= 2 && (priority[0] == 'P' || priority[0] == 'p') {
		if p, err := strconv.Atoi(priority[1:]); err == nil {
			return p
		}
	}
	// Try to parse as plain number
	if p, err := strconv.Atoi(priority); err == nil {
		return p
	}
	// Default to P2 (medium priority)
	return 2
}
