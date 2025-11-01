package repl

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/steveyegge/vc/internal/ai"
	"github.com/steveyegge/vc/internal/executor"
	"github.com/steveyegge/vc/internal/types"
)

// toolContinueExecution executes work on an issue (THE VIBECODER PRIMITIVE).
// Spawns a coding agent, processes results, runs quality gates, and creates follow-on issues.
// This is the core operation of VibeCoder - AI-supervised execution of coding work.
// Input: issue_id (optional - picks next ready if not provided), async (not yet implemented)
// Returns: Execution status, completion details, and any discovered follow-on work
func (c *ConversationHandler) toolContinueExecution(ctx context.Context, input map[string]interface{}) (string, error) {
	issueID, _ := input["issue_id"].(string)
	async := false
	if a, ok := input["async"].(bool); ok {
		async = a
	}

	// Note: async execution is not yet implemented
	if async {
		return "", fmt.Errorf("async execution not yet implemented")
	}

	var issue *types.Issue
	var err error

	// Get issue to execute
	if issueID != "" {
		// Execute specific issue
		issue, err = c.storage.GetIssue(ctx, issueID)
		if err != nil {
			return "", fmt.Errorf("failed to get issue %s: %w", issueID, err)
		}

		// Validate issue can be executed
		if errMsg, err := c.validateIssueForExecution(ctx, issue); err != nil {
			return "", err
		} else if errMsg != "" {
			return errMsg, nil
		}
	} else {
		// Get next ready work
		issues, err := c.storage.GetReadyWork(ctx, types.WorkFilter{
			Status: types.StatusOpen,
			Limit:  1,
		})
		if err != nil {
			return "", fmt.Errorf("failed to get ready work: %w", err)
		}

		if len(issues) == 0 {
			return "No ready work found. All issues are either completed or blocked.", nil
		}

		issue = issues[0]
	}

	// Execute the issue using shared execution logic
	result, err := c.executeIssue(ctx, issue)
	if err != nil {
		return "", err
	}

	// Build response (format differs from batch execution in continue_until_blocked)
	var response string
	if result.Completed {
		response = fmt.Sprintf("✓ Issue %s completed successfully!\n", issue.ID)
	} else if !result.GatesPassed {
		response = fmt.Sprintf("✗ Issue %s blocked by quality gates\n", issue.ID)
	} else {
		response = fmt.Sprintf("⚡ Issue %s partially complete (left open)\n", issue.ID)
	}

	if len(result.DiscoveredIssues) > 0 {
		response += fmt.Sprintf("\nCreated %d follow-on issues: %v\n", len(result.DiscoveredIssues), result.DiscoveredIssues)
	}

	return response, nil
}

// toolContinueUntilBlocked autonomously executes ready issues in a loop until blocked.
// This is the autonomous execution mode that enables VC to work through multiple issues
// without manual intervention. It includes watchdog monitoring, error tracking, and
// graceful shutdown capabilities.
// Input: max_iterations (default: 10), timeout_minutes (default: 120), error_threshold (default: 3)
// Returns: Summary of execution including issues completed, errors, and stop reason
func (c *ConversationHandler) toolContinueUntilBlocked(ctx context.Context, input map[string]interface{}) (string, error) {
	// Parse parameters with defaults
	maxIterations := 10
	if mi, ok := input["max_iterations"].(float64); ok {
		maxIterations = int(mi)
	}

	timeoutMinutes := 120
	if tm, ok := input["timeout_minutes"].(float64); ok {
		timeoutMinutes = int(tm)
	}

	errorThreshold := 3
	if et, ok := input["error_threshold"].(float64); ok {
		errorThreshold = int(et)
	}

	// Create timeout context
	timeoutDuration := time.Duration(timeoutMinutes) * time.Minute
	ctx, cancel := context.WithTimeout(ctx, timeoutDuration)
	defer cancel()

	// Track execution state with three categories
	var (
		completedIssues   []string
		partialIssues     []string
		failedIssues      []string
		consecutiveErrors int
		iteration         int
		startTime         = time.Now()
	)

	// Execution loop
	for iteration = 0; iteration < maxIterations; iteration++ {
		// Check for context cancellation (timeout or Ctrl+C)
		select {
		case <-ctx.Done():
			elapsed := time.Since(startTime)
			return c.formatContinueLoopResult(completedIssues, partialIssues, failedIssues, iteration, "timeout or interruption", elapsed), nil
		default:
		}

		// Check for ready work
		readyIssues, err := c.storage.GetReadyWork(ctx, types.WorkFilter{
			Status: types.StatusOpen,
			Limit:  1,
		})
		if err != nil {
			return "", fmt.Errorf("failed to check for ready work: %w", err)
		}

		if len(readyIssues) == 0 {
			// No more work - successful completion
			elapsed := time.Since(startTime)
			return c.formatContinueLoopResult(completedIssues, partialIssues, failedIssues, iteration, "no more ready work", elapsed), nil
		}

		issue := readyIssues[0]

		// Execute the issue using the same logic as continue_execution
		executionResult, execErr := c.executeIssue(ctx, issue)

		if execErr != nil {
			// Execution error (spawn failed, agent crashed, etc.)
			failedIssues = append(failedIssues, issue.ID)
			consecutiveErrors++

			// Check error threshold
			if consecutiveErrors >= errorThreshold {
				elapsed := time.Since(startTime)
				return c.formatContinueLoopResult(completedIssues, partialIssues, failedIssues, iteration+1, fmt.Sprintf("error threshold exceeded (%d consecutive errors)", consecutiveErrors), elapsed), nil
			}
		} else if !executionResult.GatesPassed {
			// Quality gates failed - actual failure
			failedIssues = append(failedIssues, issue.ID)
			consecutiveErrors++

			// Check error threshold
			if consecutiveErrors >= errorThreshold {
				elapsed := time.Since(startTime)
				return c.formatContinueLoopResult(completedIssues, partialIssues, failedIssues, iteration+1, fmt.Sprintf("error threshold exceeded (%d consecutive errors)", consecutiveErrors), elapsed), nil
			}
		} else if executionResult.Completed {
			// Issue closed successfully
			completedIssues = append(completedIssues, issue.ID)
			consecutiveErrors = 0 // Reset error counter on success
		} else {
			// Issue left open but work was done (partial completion)
			partialIssues = append(partialIssues, issue.ID)
			consecutiveErrors = 0 // Reset error counter on partial success
		}

		// Progress update (will be visible in the conversation)
		// The AI will see this in the tool result
	}

	// Reached max iterations
	elapsed := time.Since(startTime)
	return c.formatContinueLoopResult(completedIssues, partialIssues, failedIssues, iteration, "max iterations reached", elapsed), nil
}

// validateIssueForExecution validates that an issue can be executed.
// Returns a user-facing error message if the issue cannot be executed, or empty string if valid.
// Returns a system error if validation itself fails (e.g., database error).
func (c *ConversationHandler) validateIssueForExecution(ctx context.Context, issue *types.Issue) (string, error) {
	switch issue.Status {
	case types.StatusClosed:
		return fmt.Sprintf("Cannot execute issue %s: already closed", issue.ID), nil
	case types.StatusInProgress:
		return fmt.Sprintf("Cannot execute issue %s: already in progress (may be claimed by another executor)", issue.ID), nil
	case types.StatusBlocked:
		// Get dependencies to show what's blocking it
		deps, err := c.storage.GetDependencies(ctx, issue.ID)
		if err != nil {
			return "", fmt.Errorf("failed to get dependencies for issue %s: %w", issue.ID, err)
		}

		if len(deps) > 0 {
			var blockingIDs []string
			for _, dep := range deps {
				if dep.Status != types.StatusClosed {
					blockingIDs = append(blockingIDs, dep.ID)
				}
			}
			if len(blockingIDs) > 0 {
				return fmt.Sprintf("Cannot execute issue %s: blocked by %v", issue.ID, blockingIDs), nil
			}
		}
		return fmt.Sprintf("Cannot execute issue %s: currently blocked", issue.ID), nil
	}

	return "", nil
}

// issueExecutionResult captures the result of executing a single issue.
type issueExecutionResult struct {
	Completed        bool
	GatesPassed      bool
	DiscoveredIssues []string
}

// executeIssue executes a single issue and returns the result.
// This is extracted from toolContinueExecution to enable reuse in the autonomous loop.
func (c *ConversationHandler) executeIssue(ctx context.Context, issue *types.Issue) (*issueExecutionResult, error) {
	// Validate issue can be executed (prevent race conditions)
	if errMsg, err := c.validateIssueForExecution(ctx, issue); err != nil {
		return nil, err
	} else if errMsg != "" {
		return nil, fmt.Errorf("%s", errMsg)
	}

	// Claim the issue
	instanceID := fmt.Sprintf("conversation-%s", c.actor)
	if err := c.storage.ClaimIssue(ctx, issue.ID, instanceID); err != nil {
		return nil, fmt.Errorf("failed to claim issue %s: %w", issue.ID, err)
	}

	// Note: Conversational mode doesn't use the full state machine (assess/analyze/gates)
	// The issue is claimed (in_progress) which is sufficient for tracking

	// Gather context for comprehensive prompt
	gatherer := executor.NewContextGatherer(c.storage)
	promptCtx, err := gatherer.GatherContext(ctx, issue, nil)
	if err != nil {
		c.releaseIssueWithError(ctx, issue.ID, instanceID, fmt.Sprintf("Failed to gather context: %v", err))
		return nil, fmt.Errorf("failed to gather context: %w", err)
	}

	// Build comprehensive prompt using PromptBuilder
	builder, err := executor.NewPromptBuilder()
	if err != nil {
		c.releaseIssueWithError(ctx, issue.ID, instanceID, fmt.Sprintf("Failed to create prompt builder: %v", err))
		return nil, fmt.Errorf("failed to create prompt builder: %w", err)
	}

	prompt, err := builder.BuildPrompt(promptCtx)
	if err != nil {
		c.releaseIssueWithError(ctx, issue.ID, instanceID, fmt.Sprintf("Failed to build prompt: %v", err))
		return nil, fmt.Errorf("failed to build prompt: %w", err)
	}

	// Log prompt for debugging if VC_DEBUG_PROMPTS is set
	if os.Getenv("VC_DEBUG_PROMPTS") != "" {
		fmt.Fprintf(os.Stderr, "\n=== AGENT PROMPT ===\n%s\n=== END PROMPT ===\n\n", prompt)
	}

	// Spawn agent
	agentCfg := executor.AgentConfig{
		Type:       executor.AgentTypeClaudeCode,
		WorkingDir: ".",
		Issue:      issue,
		StreamJSON: false,
		Timeout:    30 * time.Minute,
	}

	agent, err := executor.SpawnAgent(ctx, agentCfg, prompt)
	if err != nil {
		c.releaseIssueWithError(ctx, issue.ID, instanceID, fmt.Sprintf("Failed to spawn agent: %v", err))
		return nil, fmt.Errorf("failed to spawn agent: %w", err)
	}

	// Wait for completion
	result, err := agent.Wait(ctx)
	if err != nil {
		c.releaseIssueWithError(ctx, issue.ID, instanceID, fmt.Sprintf("Agent execution failed: %v", err))
		return nil, fmt.Errorf("agent execution failed: %w", err)
	}

	// Process results using ResultsProcessor
	supervisor, err := ai.NewSupervisor(&ai.Config{
		Store: c.storage,
	})
	if err != nil {
		// Continue without AI supervision
		fmt.Fprintf(os.Stderr, "Warning: AI supervisor not available: %v (continuing without AI analysis)\n", err)
		supervisor = nil
	}

	processor, err := executor.NewResultsProcessor(&executor.ResultsProcessorConfig{
		Store:              c.storage,
		Supervisor:         supervisor,
		EnableQualityGates: true,
		WorkingDir:         ".",
		Actor:              instanceID,
	})
	if err != nil {
		c.releaseIssueWithError(ctx, issue.ID, instanceID, fmt.Sprintf("Failed to create results processor: %v", err))
		return nil, fmt.Errorf("failed to create results processor: %w", err)
	}

	procResult, err := processor.ProcessAgentResult(ctx, issue, result)
	if err != nil {
		c.releaseIssueWithError(ctx, issue.ID, instanceID, fmt.Sprintf("Failed to process results: %v", err))
		return nil, fmt.Errorf("failed to process results: %w", err)
	}

	return &issueExecutionResult{
		Completed:        procResult.Completed,
		GatesPassed:      procResult.GatesPassed,
		DiscoveredIssues: procResult.DiscoveredIssues,
	}, nil
}

// formatContinueLoopResult formats the result of a continue_until_blocked execution.
// Displays three categories: completed (closed), partial (work done, left open), and failed (errors).
func (c *ConversationHandler) formatContinueLoopResult(completed, partial, failed []string, iterations int, stopReason string, elapsed time.Duration) string {
	var result string

	result += "⚡ Autonomous Execution Complete\n\n"
	result += fmt.Sprintf("Stop Reason: %s\n", stopReason)
	result += fmt.Sprintf("Iterations: %d\n", iterations)
	result += fmt.Sprintf("Elapsed Time: %s\n", elapsed.Round(time.Second))
	result += "\n"

	// Completed issues (fully closed)
	result += fmt.Sprintf("Completed: %d issues\n", len(completed))
	if len(completed) > 0 {
		result += fmt.Sprintf("  %v\n", completed)
	}

	// Partial completion (work done, left open for more)
	result += fmt.Sprintf("Partial: %d issues (work done, left open)\n", len(partial))
	if len(partial) > 0 {
		result += fmt.Sprintf("  %v\n", partial)
	}

	// Failed issues (execution errors or quality gates failed)
	result += fmt.Sprintf("Failed: %d issues\n", len(failed))
	if len(failed) > 0 {
		result += fmt.Sprintf("  %v\n", failed)
	}

	return result
}

// releaseIssueWithError releases an issue and adds an error comment
func (c *ConversationHandler) releaseIssueWithError(ctx context.Context, issueID, actor, errMsg string) {
	// Use atomic ReleaseIssueAndReopen to ensure issue returns to 'open' status
	// This allows the issue to be retried instead of getting stuck in 'in_progress'
	if err := c.storage.ReleaseIssueAndReopen(ctx, issueID, actor, errMsg); err != nil {
		fmt.Fprintf(os.Stderr, "warning: failed to release and reopen issue: %v\n", err)
	}
}
