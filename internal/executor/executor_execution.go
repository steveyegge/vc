package executor

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/google/uuid"
	"github.com/steveyegge/vc/internal/ai"
	"github.com/steveyegge/vc/internal/events"
	"github.com/steveyegge/vc/internal/sandbox"
	"github.com/steveyegge/vc/internal/types"
)

// executeIssue executes a single issue by spawning a coding agent
func (e *Executor) executeIssue(ctx context.Context, issue *types.Issue) error {
	fmt.Printf("Executing issue %s: %s\n", issue.ID, issue.Title)

	// Start telemetry collection for this execution
	e.monitor.StartExecution(issue.ID, e.instanceID)

	// Log issue claimed event
	e.logEvent(ctx, events.EventTypeIssueClaimed, events.SeverityInfo, issue.ID,
		fmt.Sprintf("Issue %s claimed by executor %s", issue.ID, e.instanceID),
		map[string]interface{}{
			"issue_title": issue.Title,
		})
	e.monitor.RecordEvent(string(events.EventTypeIssueClaimed))

	// Phase 1: AI Assessment (if enabled)
	// Always transition to assessing state for state machine consistency (vc-110)
	if err := e.store.UpdateExecutionState(ctx, issue.ID, types.ExecutionStateAssessing); err != nil {
		// Check if context was canceled (shutdown initiated)
		if ctx.Err() != nil {
			// Use background context for cleanup since main context is canceled
			cleanupCtx := context.Background()
			e.releaseIssueWithError(cleanupCtx, issue.ID, fmt.Sprintf("Execution canceled during state transition: %v", ctx.Err()))
			e.monitor.EndExecution(false, false)
			return ctx.Err()
		}
		fmt.Fprintf(os.Stderr, "warning: failed to update execution state: %v\n", err)
	}
	e.monitor.RecordStateTransition(types.ExecutionStateClaimed, types.ExecutionStateAssessing)

	var assessment *ai.Assessment
	if e.enableAISupervision && e.supervisor != nil {
		// Log assessment started
		e.logEvent(ctx, events.EventTypeAssessmentStarted, events.SeverityInfo, issue.ID,
			fmt.Sprintf("Starting AI assessment for issue %s", issue.ID),
			map[string]interface{}{})

		var err error
		assessment, err = e.supervisor.AssessIssueState(ctx, issue)
		if err != nil {
			// Check if context was canceled (shutdown initiated)
			if ctx.Err() != nil {
				fmt.Fprintf(os.Stderr, "Assessment canceled due to executor shutdown\n")
				// Use background context for cleanup since main context is canceled
				cleanupCtx := context.Background()
				e.releaseIssueWithError(cleanupCtx, issue.ID, "Execution canceled during assessment")
				e.monitor.EndExecution(false, false)
				return ctx.Err()
			}
			// Real error (not cancellation) - log and continue without assessment
			fmt.Fprintf(os.Stderr, "Warning: AI assessment failed: %v (continuing without assessment)\n", err)
			// Log assessment failure
			e.logEvent(ctx, events.EventTypeAssessmentCompleted, events.SeverityError, issue.ID,
				fmt.Sprintf("AI assessment failed: %v", err),
				map[string]interface{}{
					"success": false,
					"error":   err.Error(),
				})
		} else {
			// Log the assessment as a comment
			assessmentComment := fmt.Sprintf("**AI Assessment**\n\nStrategy: %s\n\nConfidence: %.0f%%\n\nSteps:\n",
				assessment.Strategy, assessment.Confidence*100)
			for i, step := range assessment.Steps {
				assessmentComment += fmt.Sprintf("%d. %s\n", i+1, step)
			}
			if len(assessment.Risks) > 0 {
				assessmentComment += "\nRisks:\n"
				for _, risk := range assessment.Risks {
					assessmentComment += fmt.Sprintf("- %s\n", risk)
				}
			}
			if err := e.store.AddComment(ctx, issue.ID, "ai-supervisor", assessmentComment); err != nil {
				fmt.Fprintf(os.Stderr, "warning: failed to add assessment comment: %v\n", err)
			}

			// Log assessment success
			e.logEvent(ctx, events.EventTypeAssessmentCompleted, events.SeverityInfo, issue.ID,
				fmt.Sprintf("AI assessment completed for issue %s", issue.ID),
				map[string]interface{}{
					"success":     true,
					"strategy":    assessment.Strategy,
					"confidence":  assessment.Confidence,
					"steps_count": len(assessment.Steps),
					"risks_count": len(assessment.Risks),
				})
		}
	} else {
		// AI supervision disabled - assessing state is a no-op
		fmt.Printf("Skipping AI assessment (supervision disabled)\n")
	}

	// Phase 2: Create sandbox if enabled
	var sb *sandbox.Sandbox
	workingDir := e.workingDir
	if e.enableSandboxes && e.sandboxMgr != nil {
		fmt.Printf("Creating sandbox for issue %s...\n", issue.ID)

		// Get parent repo from config (will be set by manager if not specified)
		parentRepo := "."
		if e.config != nil && e.config.ParentRepo != "" {
			parentRepo = e.config.ParentRepo
		}

		// Get base branch from config
		baseBranch := "main"
		if e.config != nil && e.config.DefaultBranch != "" {
			baseBranch = e.config.DefaultBranch
		}

		sandboxCfg := sandbox.SandboxConfig{
			MissionID:  issue.ID,
			ParentRepo: parentRepo,
			BaseBranch: baseBranch,
		}

		var err error
		sb, err = e.sandboxMgr.Create(ctx, sandboxCfg)
		if err != nil {
			// Don't fail execution - just log and continue without sandbox
			fmt.Fprintf(os.Stderr, "Warning: failed to create sandbox: %v (continuing in main workspace)\n", err)
		} else {
			// Set working directory to sandbox path
			workingDir = sb.Path
			fmt.Printf("Sandbox created: %s (branch: %s)\n", sb.Path, sb.GitBranch)

			// Ensure cleanup happens
			defer func() {
				if sb != nil {
					fmt.Printf("Cleaning up sandbox %s...\n", sb.ID)
					if err := e.sandboxMgr.Cleanup(ctx, sb); err != nil {
						fmt.Fprintf(os.Stderr, "warning: failed to cleanup sandbox: %v\n", err)
					}
				}
			}()
		}
	}

	// Phase 3: Spawn the coding agent
	// Check if context was canceled before starting execution (vc-101)
	if ctx.Err() != nil {
		fmt.Fprintf(os.Stderr, "Execution canceled before spawning agent\n")
		// Use background context for cleanup since main context is canceled
		cleanupCtx := context.Background()
		e.releaseIssueWithError(cleanupCtx, issue.ID, "Execution canceled before spawning agent")
		e.monitor.EndExecution(false, false)
		return ctx.Err()
	}

	// Update execution state to executing
	if err := e.store.UpdateExecutionState(ctx, issue.ID, types.ExecutionStateExecuting); err != nil {
		// Check if context was canceled (shutdown initiated)
		if ctx.Err() != nil {
			// Use background context for cleanup since main context is canceled
			cleanupCtx := context.Background()
			e.releaseIssueWithError(cleanupCtx, issue.ID, fmt.Sprintf("Execution canceled during state transition: %v", ctx.Err()))
			e.monitor.EndExecution(false, false)
			return ctx.Err()
		}
		fmt.Fprintf(os.Stderr, "warning: failed to update execution state: %v\n", err)
	}
	// Always transition from assessing→executing (vc-110)
	e.monitor.RecordStateTransition(types.ExecutionStateAssessing, types.ExecutionStateExecuting)

	// Create a cancelable context for the agent so watchdog can intervene
	agentCtx, agentCancel := context.WithCancel(ctx)
	defer func() {
		fmt.Printf("[DEBUG vc-177] agentCancel called for issue %s\n", issue.ID)
		agentCancel() // Always cancel when we're done
	}()

	// Register agent context with intervention controller for watchdog
	if e.intervention != nil {
		e.intervention.SetAgentContext(issue.ID, agentCancel)
		defer e.intervention.ClearAgentContext()
	}

	// Gather context for comprehensive prompt
	gatherer := NewContextGatherer(e.store)
	promptCtx, err := gatherer.GatherContext(ctx, issue, nil)
	if err != nil {
		e.logEvent(ctx, events.EventTypeAgentSpawned, events.SeverityError, issue.ID,
			fmt.Sprintf("Failed to gather context: %v", err),
			map[string]interface{}{
				"success": false,
				"error":   err.Error(),
			})
		e.releaseIssueWithError(ctx, issue.ID, fmt.Sprintf("Failed to gather context: %v", err))
		e.monitor.EndExecution(false, false)
		return fmt.Errorf("failed to gather context: %w", err)
	}

	// Build comprehensive prompt using PromptBuilder
	builder, err := NewPromptBuilder()
	if err != nil {
		e.logEvent(ctx, events.EventTypeAgentSpawned, events.SeverityError, issue.ID,
			fmt.Sprintf("Failed to create prompt builder: %v", err),
			map[string]interface{}{
				"success": false,
				"error":   err.Error(),
			})
		e.releaseIssueWithError(ctx, issue.ID, fmt.Sprintf("Failed to create prompt builder: %v", err))
		e.monitor.EndExecution(false, false)
		return fmt.Errorf("failed to create prompt builder: %w", err)
	}

	prompt, err := builder.BuildPrompt(promptCtx)
	if err != nil {
		e.logEvent(ctx, events.EventTypeAgentSpawned, events.SeverityError, issue.ID,
			fmt.Sprintf("Failed to build prompt: %v", err),
			map[string]interface{}{
				"success": false,
				"error":   err.Error(),
			})
		e.releaseIssueWithError(ctx, issue.ID, fmt.Sprintf("Failed to build prompt: %v", err))
		e.monitor.EndExecution(false, false)
		return fmt.Errorf("failed to build prompt: %w", err)
	}

	// Log prompt for debugging if VC_DEBUG_PROMPTS is set
	if os.Getenv("VC_DEBUG_PROMPTS") != "" {
		fmt.Fprintf(os.Stderr, "\n=== AGENT PROMPT ===\n%s\n=== END PROMPT ===\n\n", prompt)
	}

	// Generate a unique agent ID for this execution
	agentID := uuid.New().String()

	agentCfg := AgentConfig{
		Type:       AgentTypeAmp, // Use Amp for structured JSON events (vc-236)
		WorkingDir: workingDir,
		Issue:      issue,
		StreamJSON: true, // Enable --stream-json for structured events (vc-236)
		Timeout:    30 * time.Minute,
		// Enable event parsing and storage
		Store:      e.store,
		ExecutorID: e.instanceID,
		AgentID:    agentID,
		Monitor:    e.monitor, // Pass monitor for watchdog visibility (vc-118)
		Sandbox:    sb,
	}

	agent, err := SpawnAgent(agentCtx, agentCfg, prompt)
	if err != nil {
		// Log agent spawn failure BEFORE releasing issue
		e.logEvent(ctx, events.EventTypeAgentSpawned, events.SeverityError, issue.ID,
			fmt.Sprintf("Failed to spawn agent: %v", err),
			map[string]interface{}{
				"success":    false,
				"agent_type": agentCfg.Type,
				"error":      err.Error(),
			})
		e.releaseIssueWithError(ctx, issue.ID, fmt.Sprintf("Failed to spawn agent: %v", err))
		// End telemetry collection on failure
		e.monitor.EndExecution(false, false)
		return fmt.Errorf("failed to spawn agent: %w", err)
	}

	// Log agent spawned successfully
	e.logEvent(ctx, events.EventTypeAgentSpawned, events.SeverityInfo, issue.ID,
		fmt.Sprintf("Agent spawned for issue %s", issue.ID),
		map[string]interface{}{
			"success":    true,
			"agent_type": agentCfg.Type,
		})

	// Wait for agent to complete
	result, err := agent.Wait(agentCtx)
	if err != nil {
		// Log agent execution failure BEFORE releasing issue
		e.logEvent(ctx, events.EventTypeAgentCompleted, events.SeverityError, issue.ID,
			fmt.Sprintf("Agent execution failed: %v", err),
			map[string]interface{}{
				"success": false,
				"error":   err.Error(),
			})
		e.releaseIssueWithError(ctx, issue.ID, fmt.Sprintf("Agent execution failed: %v", err))
		// End telemetry collection on failure
		e.monitor.EndExecution(false, false)
		return fmt.Errorf("agent execution failed: %w", err)
	}

	// Log agent execution success
	e.logEvent(ctx, events.EventTypeAgentCompleted, events.SeverityInfo, issue.ID,
		fmt.Sprintf("Agent completed execution for issue %s", issue.ID),
		map[string]interface{}{
			"success":      true,
			"exit_code":    result.ExitCode,
			"duration_ms":  result.Duration.Milliseconds(),
			"output_lines": len(result.Output),
		})

	// Phase 3: Process results using ResultsProcessor
	// This handles AI analysis, quality gates, discovered issues, and tracker updates

	// Log results processing started
	e.logEvent(ctx, events.EventTypeResultsProcessingStarted, events.SeverityInfo, issue.ID,
		fmt.Sprintf("Starting results processing for issue %s", issue.ID),
		map[string]interface{}{})

	// Use shared deduplicator instance (vc-137)
	// Created once in New() and reused for both sandbox manager and results processor
	processor, err := NewResultsProcessor(&ResultsProcessorConfig{
		Store:              e.store,
		Supervisor:         e.supervisor,
		Deduplicator:       e.deduplicator, // Use shared instance (vc-137)
		GitOps:             e.gitOps,       // Git operations for auto-commit (vc-136)
		MessageGen:         e.messageGen,   // Commit message generator (vc-136)
		EnableQualityGates: e.enableQualityGates,
		EnableAutoCommit:   e.config.EnableAutoCommit, // Auto-commit configuration (vc-142)
		WorkingDir:         workingDir,                // Use sandbox path if sandboxing is enabled (vc-117)
		Actor:              e.instanceID,
		Sandbox:            sb, // Pass sandbox for status tracking (vc-134)
	})
	if err != nil {
		// Log results processing failure BEFORE releasing issue
		e.logEvent(ctx, events.EventTypeResultsProcessingCompleted, events.SeverityError, issue.ID,
			fmt.Sprintf("Results processor creation failed: %v", err),
			map[string]interface{}{
				"success": false,
				"error":   err.Error(),
			})
		e.releaseIssueWithError(ctx, issue.ID, fmt.Sprintf("Failed to create results processor: %v", err))
		// End telemetry collection on failure
		e.monitor.EndExecution(false, false)
		return fmt.Errorf("failed to create results processor: %w", err)
	}

	procResult, err := processor.ProcessAgentResult(ctx, issue, result)
	if err != nil {
		// Log results processing failure BEFORE releasing issue
		e.logEvent(ctx, events.EventTypeResultsProcessingCompleted, events.SeverityError, issue.ID,
			fmt.Sprintf("Results processing failed: %v", err),
			map[string]interface{}{
				"success": false,
				"error":   err.Error(),
			})
		e.releaseIssueWithError(ctx, issue.ID, fmt.Sprintf("Failed to process results: %v", err))
		// End telemetry collection on failure
		e.monitor.EndExecution(false, false)
		return fmt.Errorf("failed to process agent result: %w", err)
	}

	// Log results processing success
	e.logEvent(ctx, events.EventTypeResultsProcessingCompleted, events.SeverityInfo, issue.ID,
		fmt.Sprintf("Results processing completed for issue %s", issue.ID),
		map[string]interface{}{
			"success":           true,
			"completed":         procResult.Completed,
			"gates_passed":      procResult.GatesPassed,
			"discovered_issues": len(procResult.DiscoveredIssues),
			"commit_hash":       procResult.CommitHash,
		})

	// Print summary
	fmt.Println(procResult.Summary)

	// vc-154: Check mission convergence if this was a blocker and completed successfully
	if procResult.Completed && result.Success {
		if err := e.checkMissionConvergence(ctx, issue); err != nil {
			// Log error but don't fail execution
			fmt.Fprintf(os.Stderr, "warning: failed to check mission convergence: %v\n", err)
		}
	}

	// vc-235: Check epic completion if this task completed successfully
	if procResult.Completed && result.Success {
		if err := e.checkEpicCompletion(ctx, issue); err != nil {
			// Log error but don't fail execution
			fmt.Fprintf(os.Stderr, "warning: failed to check epic completion: %v\n", err)
		}
	}

	// End telemetry collection
	e.monitor.EndExecution(procResult.Completed && result.Success, procResult.GatesPassed)

	return nil
}

// releaseIssueWithError releases an issue and adds an error comment
// If there are too many consecutive failures, the issue is marked as blocked instead of reopened
func (e *Executor) releaseIssueWithError(ctx context.Context, issueID, errMsg string) {
	const maxConsecutiveFailures = 3 // Block after 3 consecutive failures

	// Get execution history to check for consecutive failures
	history, err := e.store.GetExecutionHistory(ctx, issueID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: failed to get execution history for %s: %v\n", issueID, err)
		// Fall through to reopen - safer to retry than block on error
	}

	// Count recent consecutive failures
	consecutiveFailures := 0
	for i := len(history) - 1; i >= 0; i-- {
		attempt := history[i]
		// Only count completed attempts
		if attempt.Success == nil {
			continue // Skip incomplete attempts
		}
		if !*attempt.Success {
			consecutiveFailures++
		} else {
			break // Stop counting at first success
		}
	}

	// Check if we should block due to too many failures
	if consecutiveFailures >= maxConsecutiveFailures {
		fmt.Fprintf(os.Stderr, "Issue %s has %d consecutive failures, marking as blocked\n",
			issueID, consecutiveFailures)

		// Mark as blocked instead of reopening
		blockReason := fmt.Sprintf("Blocked after %d consecutive execution failures. Last error: %s",
			consecutiveFailures, errMsg)

		// Release execution state and mark as blocked
		if err := e.store.ReleaseIssue(ctx, issueID); err != nil {
			fmt.Fprintf(os.Stderr, "warning: failed to release issue %s: %v\n", issueID, err)
		}

		if err := e.store.UpdateIssue(ctx, issueID, map[string]interface{}{
			"status": types.StatusBlocked,
		}, "executor"); err != nil {
			fmt.Fprintf(os.Stderr, "warning: failed to mark issue %s as blocked: %v\n", issueID, err)
		}

		if err := e.store.AddComment(ctx, issueID, "executor", blockReason); err != nil {
			fmt.Fprintf(os.Stderr, "warning: failed to add comment to %s: %v\n", issueID, err)
		}
		return
	}

	// Not enough failures yet, reopen for retry
	if consecutiveFailures > 0 {
		fmt.Fprintf(os.Stderr, "Issue %s has %d consecutive failures, reopening for retry\n",
			issueID, consecutiveFailures)
	}

	// Use atomic ReleaseIssueAndReopen to ensure issue returns to 'open' status
	// This allows the issue to be retried instead of getting stuck in 'in_progress'
	if err := e.store.ReleaseIssueAndReopen(ctx, issueID, e.instanceID, errMsg); err != nil {
		fmt.Fprintf(os.Stderr, "warning: failed to release and reopen issue: %v\n", err)
	}
}
