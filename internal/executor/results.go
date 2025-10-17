package executor

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/steveyegge/vc/internal/ai"
	"github.com/steveyegge/vc/internal/events"
	"github.com/steveyegge/vc/internal/gates"
	"github.com/steveyegge/vc/internal/git"
	"github.com/steveyegge/vc/internal/storage"
	"github.com/steveyegge/vc/internal/types"
)

// Code review decision thresholds
const (
	// minCodeReviewConfidence is the minimum confidence threshold for skipping code review.
	// If AI confidence is below this threshold, we request review as a safety measure.
	minCodeReviewConfidence = 0.70
)

// ResultsProcessor handles post-execution results collection and tracker updates
type ResultsProcessor struct {
	store              storage.Storage
	supervisor         *ai.Supervisor
	gitOps             git.GitOperations
	messageGen         *git.MessageGenerator
	enableQualityGates bool
	enableAutoCommit   bool
	workingDir         string
	actor              string // The actor performing the update (e.g., "repl", "executor-instance-id")
}

// ResultsProcessorConfig holds configuration for the results processor
type ResultsProcessorConfig struct {
	Store              storage.Storage
	Supervisor         *ai.Supervisor      // Can be nil to disable AI analysis
	GitOps             git.GitOperations   // Can be nil to disable auto-commit
	MessageGen         *git.MessageGenerator // Can be nil to disable auto-commit
	EnableQualityGates bool
	EnableAutoCommit   bool
	WorkingDir         string
	Actor              string // Actor ID for tracking who made the changes
}

// ProcessingResult contains the outcome of processing agent results
type ProcessingResult struct {
	Completed        bool     // Was the issue marked as completed?
	DiscoveredIssues []string // IDs of discovered issues created
	GatesPassed      bool     // Did quality gates pass?
	CommitHash       string   // Git commit hash (if auto-commit succeeded)
	Summary          string   // Human-readable summary
	AIAnalysis       *ai.Analysis // The AI analysis result (if available)
}

// NewResultsProcessor creates a new results processor
func NewResultsProcessor(cfg *ResultsProcessorConfig) (*ResultsProcessor, error) {
	if cfg.Store == nil {
		return nil, fmt.Errorf("storage is required")
	}
	if cfg.WorkingDir == "" {
		cfg.WorkingDir = "."
	}
	if cfg.Actor == "" {
		cfg.Actor = "unknown"
	}

	return &ResultsProcessor{
		store:              cfg.Store,
		supervisor:         cfg.Supervisor,
		gitOps:             cfg.GitOps,
		messageGen:         cfg.MessageGen,
		enableQualityGates: cfg.EnableQualityGates,
		enableAutoCommit:   cfg.EnableAutoCommit,
		workingDir:         cfg.WorkingDir,
		actor:              cfg.Actor,
	}, nil
}

// ProcessAgentResult processes the result from an agent execution and updates the tracker
//
// This is the core of vc-76: collect results, run AI analysis, update issue status,
// create follow-on issues, and close the loop from execution back to tracker.
//
// Steps performed:
// 1. Extract and summarize agent output
// 2. Run AI analysis (if supervisor available)
// 3. Run quality gates (if enabled and agent succeeded)
// 4. Update issue status based on analysis
// 5. Create discovered issues from AI analysis
// 6. Add comments with summary and analysis
// 7. Check parent epic completion
// 8. Return detailed processing result
func (rp *ResultsProcessor) ProcessAgentResult(ctx context.Context, issue *types.Issue, agentResult *AgentResult) (*ProcessingResult, error) {
	result := &ProcessingResult{
		Completed:        false,
		DiscoveredIssues: []string{},
		GatesPassed:      true,
		Summary:          "",
	}

	// Step 1: Extract agent output summary
	agentOutput, err := rp.extractSummary(ctx, issue, agentResult)
	if err != nil {
		// AI summarization failed - this is a critical error (ZFC requires AI)
		// Mark the issue as blocked and require human intervention
		fmt.Fprintf(os.Stderr, "\n✗ AI summarization failed: %v\n", err)
		fmt.Fprintf(os.Stderr, "Marking issue %s as blocked - human intervention required\n", issue.ID)

		// Add error comment explaining the failure
		// Include last 100 lines of raw output so human can review it
		outputSample := getOutputSample(agentResult.Output, 100)

		errorComment := fmt.Sprintf("**AI Summarization Failed**\n\n"+
			"The AI supervisor failed to summarize agent output after multiple retries.\n\n"+
			"Error: %v\n\n"+
			"This violates ZFC principles (no heuristic fallbacks). The issue has been marked as blocked.\n\n"+
			"**Human Action Required:**\n"+
			"1. Check ANTHROPIC_API_KEY is set and valid\n"+
			"2. Check network connectivity to Anthropic API\n"+
			"3. Review agent output below\n"+
			"4. Resolve the underlying issue and retry\n\n"+
			"**Agent Execution Details:**\n"+
			"- Success: %v\n"+
			"- Exit Code: %d\n"+
			"- Duration: %v\n"+
			"- Output Lines: %d\n\n"+
			"**Raw Agent Output (last %d lines):**\n```\n%s\n```",
			err, agentResult.Success, agentResult.ExitCode, agentResult.Duration, len(agentResult.Output),
			len(outputSample), strings.Join(outputSample, "\n"))

		if addErr := rp.store.AddComment(ctx, issue.ID, rp.actor, errorComment); addErr != nil {
			fmt.Fprintf(os.Stderr, "warning: failed to add error comment: %v\n", addErr)
		}

		// Update issue to blocked status
		updates := map[string]interface{}{
			"status": types.StatusBlocked,
		}
		if updateErr := rp.store.UpdateIssue(ctx, issue.ID, updates, rp.actor); updateErr != nil {
			fmt.Fprintf(os.Stderr, "warning: failed to update issue to blocked: %v\n", updateErr)
		}

		// Log the event
		rp.logEvent(ctx, events.EventTypeError, events.SeverityError, issue.ID,
			fmt.Sprintf("AI summarization failed for issue %s - marked as blocked", issue.ID),
			map[string]interface{}{
				"error":        err.Error(),
				"error_type":   "ai_summarization_failed",
				"exit_code":    agentResult.ExitCode,
				"output_lines": len(agentResult.Output),
			})

		// Release the execution state
		if releaseErr := rp.store.ReleaseIssue(ctx, issue.ID); releaseErr != nil {
			return nil, fmt.Errorf("failed to release issue after summarization failure: %w", releaseErr)
		}

		result.Summary = fmt.Sprintf("AI summarization failed - issue blocked (ZFC compliance): %v", err)
		return result, nil
	}

	fmt.Printf("\n=== Agent Execution Complete ===\n")
	fmt.Printf("Success: %v\n", agentResult.Success)
	fmt.Printf("Exit Code: %d\n", agentResult.ExitCode)
	fmt.Printf("Duration: %v\n", agentResult.Duration)

	// Step 2: AI Analysis (if supervisor available)
	var analysis *ai.Analysis
	if rp.supervisor != nil {
		// Update execution state to analyzing
		if err := rp.store.UpdateExecutionState(ctx, issue.ID, types.ExecutionStateAnalyzing); err != nil {
			fmt.Fprintf(os.Stderr, "warning: failed to update execution state: %v\n", err)
		}

		// Log analysis started
		rp.logEvent(ctx, events.EventTypeAnalysisStarted, events.SeverityInfo, issue.ID,
			fmt.Sprintf("Starting AI analysis for issue %s", issue.ID),
			map[string]interface{}{})

		var err error
		analysis, err = rp.supervisor.AnalyzeExecutionResult(ctx, issue, agentOutput, agentResult.Success)
		if err != nil {
			// Don't fail - just log and continue
			fmt.Fprintf(os.Stderr, "Warning: AI analysis failed: %v (continuing without analysis)\n", err)
			// Log analysis failure
			rp.logEvent(ctx, events.EventTypeAnalysisCompleted, events.SeverityError, issue.ID,
				fmt.Sprintf("AI analysis failed: %v", err),
				map[string]interface{}{
					"success": false,
					"error":   err.Error(),
				})
		} else {
			result.AIAnalysis = analysis
			fmt.Printf("\n=== AI Analysis ===\n")
			fmt.Printf("Completed: %v\n", analysis.Completed)
			fmt.Printf("Discovered Issues: %d\n", len(analysis.DiscoveredIssues))
			fmt.Printf("Quality Issues: %d\n", len(analysis.QualityIssues))
			fmt.Printf("Summary: %s\n", analysis.Summary)
			// Log analysis success
			rp.logEvent(ctx, events.EventTypeAnalysisCompleted, events.SeverityInfo, issue.ID,
				fmt.Sprintf("AI analysis completed for issue %s", issue.ID),
				map[string]interface{}{
					"success":          true,
					"completed":        analysis.Completed,
					"discovered_count": len(analysis.DiscoveredIssues),
					"quality_issues":   len(analysis.QualityIssues),
				})
		}
	}

	// Step 3: Quality Gates (if enabled and agent succeeded)
	if agentResult.Success && rp.enableQualityGates {
		// Update execution state to gates
		if err := rp.store.UpdateExecutionState(ctx, issue.ID, types.ExecutionStateGates); err != nil {
			fmt.Fprintf(os.Stderr, "warning: failed to update execution state: %v\n", err)
		}

		// Log quality gates started
		rp.logEvent(ctx, events.EventTypeQualityGatesStarted, events.SeverityInfo, issue.ID,
			fmt.Sprintf("Starting quality gates evaluation for issue %s", issue.ID),
			map[string]interface{}{})

		gateRunner, err := gates.NewRunner(&gates.Config{
			Store:      rp.store,
			WorkingDir: rp.workingDir,
		})
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to create quality gate runner: %v (skipping gates)\n", err)
			// Set GatesPassed to false to prevent issue from completing without gates
			result.GatesPassed = false
			// Log quality gates error
			rp.logEvent(ctx, events.EventTypeQualityGatesCompleted, events.SeverityError, issue.ID,
				fmt.Sprintf("Quality gate runner creation failed: %v", err),
				map[string]interface{}{
					"success": false,
					"error":   err.Error(),
				})
		} else {
			gateResults, allPassed := gateRunner.RunAll(ctx)
			result.GatesPassed = allPassed

			// Handle gate results (creates blocking issues on failure)
			if err := gateRunner.HandleGateResults(ctx, issue, gateResults, allPassed); err != nil {
				fmt.Fprintf(os.Stderr, "Warning: failed to handle gate results: %v\n", err)
			}

			// Log quality gates completed
			gateData := map[string]interface{}{
				"all_passed": allPassed,
				"gates_run":  len(gateResults),
			}

			// Count passed and failed gates
			passedCount := 0
			failedCount := 0
			for _, gateResult := range gateResults {
				if gateResult.Passed {
					passedCount++
				} else {
					failedCount++
				}
			}
			gateData["passed_count"] = passedCount
			gateData["failed_count"] = failedCount

			severity := events.SeverityInfo
			if !allPassed {
				severity = events.SeverityWarning
			}

			rp.logEvent(ctx, events.EventTypeQualityGatesCompleted, severity, issue.ID,
				fmt.Sprintf("Quality gates evaluation completed for issue %s (passed: %v)", issue.ID, allPassed),
				gateData)

			if !allPassed {
				fmt.Printf("\n=== Quality Gates Failed ===\n")
				fmt.Printf("Issue %s marked as blocked due to failing quality gates\n", issue.ID)

				// Update issue to blocked status
				updates := map[string]interface{}{
					"status": types.StatusBlocked,
				}
				if err := rp.store.UpdateIssue(ctx, issue.ID, updates, rp.actor); err != nil {
					fmt.Fprintf(os.Stderr, "warning: failed to update issue to blocked: %v\n", err)
				}

				// Release the execution state
				if err := rp.store.ReleaseIssue(ctx, issue.ID); err != nil {
					return nil, fmt.Errorf("failed to release blocked issue: %w", err)
				}

				result.Summary = "Quality gates failed - issue blocked"
				return result, nil
			}
		}
	} else {
		// Quality gates skipped - log why
		var reason string
		if !agentResult.Success {
			reason = "agent execution failed"
		} else {
			reason = "quality gates disabled"
		}
		rp.logEvent(ctx, events.EventTypeQualityGatesSkipped, events.SeverityInfo, issue.ID,
			fmt.Sprintf("Quality gates skipped for issue %s: %s", issue.ID, reason),
			map[string]interface{}{
				"reason": reason,
			})
	}

	// Step 3.5: Auto-commit changes (if enabled, agent succeeded, and gates passed)
	if agentResult.Success && result.GatesPassed && rp.enableAutoCommit && rp.gitOps != nil && rp.messageGen != nil {
		// Update execution state to committing
		if err := rp.store.UpdateExecutionState(ctx, issue.ID, types.ExecutionStateCommitting); err != nil {
			fmt.Fprintf(os.Stderr, "warning: failed to update execution state to committing: %v\n", err)
		}

		commitHash, err := rp.autoCommit(ctx, issue)
		if err != nil {
			// Don't fail - just log and continue
			fmt.Fprintf(os.Stderr, "Warning: auto-commit failed: %v (continuing without commit)\n", err)
		} else if commitHash != "" {
			result.CommitHash = commitHash
			fmt.Printf("\n✓ Changes committed: %s\n", commitHash[:8])

			// Add comment with commit hash
			commitComment := fmt.Sprintf("Auto-committed changes: %s", commitHash)
			if err := rp.store.AddComment(ctx, issue.ID, rp.actor, commitComment); err != nil {
				fmt.Fprintf(os.Stderr, "warning: failed to add commit comment: %v\n", err)
			}

			// Step 3.6: AI-based code review decision (ZFC principle - no heuristics)
			if rp.supervisor != nil {
				fmt.Printf("\n=== Code Review Decision ===\n")

				// Get the diff for this commit using git directly
				// Note: We can't use gitOps.GetDiff() because it doesn't support commit refs
				diff, err := rp.getCommitDiff(ctx, commitHash)
				if err != nil {
					// Don't fail - just log and continue
					fmt.Fprintf(os.Stderr, "Warning: failed to get diff for code review decision: %v\n", err)
				} else {
					// Use Haiku to decide if review is needed (fast and cheap)
					decision, err := rp.supervisor.AnalyzeCodeReviewNeed(ctx, issue, diff)
					if err != nil {
						// Don't fail - just log and continue
						fmt.Fprintf(os.Stderr, "Warning: code review decision failed: %v\n", err)
					} else {
						// Log the decision
						decisionComment := fmt.Sprintf("**Code Review Decision**\n\nNeeds Review: %v\n\nReasoning: %s\n\nConfidence: %.0f%%",
							decision.NeedsReview, decision.Reasoning, decision.Confidence*100)
						if err := rp.store.AddComment(ctx, issue.ID, "ai-supervisor", decisionComment); err != nil {
							fmt.Fprintf(os.Stderr, "warning: failed to add code review decision comment: %v\n", err)
						}

						// Determine if review is needed considering both AI decision and confidence
						needsReview := decision.NeedsReview
						reviewReason := decision.Reasoning

						// Safety measure: require review if confidence is too low
						if !needsReview && decision.Confidence < minCodeReviewConfidence {
							needsReview = true
							reviewReason = fmt.Sprintf("Low confidence decision (%.0f%% < %.0f%% threshold). "+
								"Requesting review as safety measure.\n\nOriginal reasoning: %s",
								decision.Confidence*100, minCodeReviewConfidence*100, decision.Reasoning)
							fmt.Printf("⚠️  Low confidence (%.0f%%), requesting review as safety measure\n",
								decision.Confidence*100)
						}

						// If review is needed, create a blocking code review issue
						if needsReview {
							if decision.NeedsReview {
								fmt.Printf("Code review recommended (confidence: %.0f%%)\n", decision.Confidence*100)
							}
							reviewIssueID, err := rp.createCodeReviewIssue(ctx, issue, commitHash, reviewReason)
							if err != nil {
								fmt.Fprintf(os.Stderr, "Warning: failed to create code review issue: %v\n", err)
							} else {
								fmt.Printf("✓ Created code review issue: %s\n", reviewIssueID)
								result.DiscoveredIssues = append(result.DiscoveredIssues, reviewIssueID)
							}
						} else {
							fmt.Printf("No code review needed (confidence: %.0f%%)\n", decision.Confidence*100)
						}
					}
				}
			}
		}
	}

	// Step 4: Update issue status
	if agentResult.Success && result.GatesPassed {
		// Determine if we should close the issue based on AI analysis
		shouldClose := true
		if analysis != nil && !analysis.Completed {
			shouldClose = false
			fmt.Printf("\nAI analysis indicates issue is not fully complete - leaving open\n")
		}

		result.Completed = shouldClose

		// Update issue status
		if shouldClose {
			updates := map[string]interface{}{
				"status": types.StatusClosed,
			}
			if err := rp.store.UpdateIssue(ctx, issue.ID, updates, rp.actor); err != nil {
				fmt.Fprintf(os.Stderr, "warning: failed to close issue: %v\n", err)
			} else {
				fmt.Printf("\n✓ Issue %s marked as closed\n", issue.ID)
			}
		}

		// Step 5: Add completion comment
		if err := rp.store.AddComment(ctx, issue.ID, rp.actor, agentOutput); err != nil {
			fmt.Fprintf(os.Stderr, "warning: failed to add comment: %v\n", err)
		}

		// Step 6: Add AI analysis comment and create discovered issues
		if analysis != nil {
			analysisComment := rp.buildAnalysisComment(analysis)
			if err := rp.store.AddComment(ctx, issue.ID, "ai-supervisor", analysisComment); err != nil {
				fmt.Fprintf(os.Stderr, "warning: failed to add analysis comment: %v\n", err)
			}

			// Create discovered issues
			if len(analysis.DiscoveredIssues) > 0 {
				createdIDs, err := rp.supervisor.CreateDiscoveredIssues(ctx, issue, analysis.DiscoveredIssues)
				if err != nil {
					fmt.Fprintf(os.Stderr, "warning: failed to create discovered issues: %v\n", err)
				} else {
					result.DiscoveredIssues = createdIDs
					if len(createdIDs) > 0 {
						fmt.Printf("\n✓ Created %d discovered issues: %v\n", len(createdIDs), createdIDs)
						discoveredComment := fmt.Sprintf("Discovered %d new issues: %v", len(createdIDs), createdIDs)
						if err := rp.store.AddComment(ctx, issue.ID, "ai-supervisor", discoveredComment); err != nil {
							fmt.Fprintf(os.Stderr, "warning: failed to add discovered issues comment: %v\n", err)
						}
					}
				}
			}
		}

		// Step 7: Check if parent epic is now complete
		if shouldClose {
			if err := checkEpicCompletion(ctx, rp.store, rp.supervisor, issue.ID); err != nil {
				fmt.Fprintf(os.Stderr, "warning: failed to check epic completion: %v\n", err)
			}
		}

		// Update execution state to completed
		if err := rp.store.UpdateExecutionState(ctx, issue.ID, types.ExecutionStateCompleted); err != nil {
			fmt.Fprintf(os.Stderr, "warning: failed to update execution state: %v\n", err)
		}

		// Release the execution state
		if err := rp.store.ReleaseIssue(ctx, issue.ID); err != nil {
			return nil, fmt.Errorf("failed to release issue: %w", err)
		}

		// Build summary
		result.Summary = rp.buildSummary(issue, agentResult, analysis, result)

	} else {
		// Agent failed or gates failed
		fmt.Printf("\n✗ Agent execution failed (exit code: %d)\n", agentResult.ExitCode)

		errMsg := fmt.Sprintf("Agent failed with exit code %d\n\nLast output:\n%s",
			agentResult.ExitCode, agentOutput)

		// Add error comment
		if err := rp.store.AddComment(ctx, issue.ID, rp.actor, errMsg); err != nil {
			fmt.Fprintf(os.Stderr, "warning: failed to add error comment: %v\n", err)
		}

		// Leave issue in in_progress state but release execution lock
		if err := rp.store.ReleaseIssue(ctx, issue.ID); err != nil {
			return nil, fmt.Errorf("failed to release issue: %w", err)
		}

		result.Summary = fmt.Sprintf("Agent execution failed with exit code %d", agentResult.ExitCode)
	}

	return result, nil
}

// extractSummary extracts a summary from agent output using AI.
// When AI supervisor is not available, returns a simple data summary (not a heuristic).
func (rp *ResultsProcessor) extractSummary(ctx context.Context, issue *types.Issue, result *AgentResult) (string, error) {
	if len(result.Output) == 0 {
		return "Agent completed with no output", nil
	}

	// Join output lines into full text
	fullOutput := strings.Join(result.Output, "\n")

	// When no AI supervisor is available, return basic data summary
	// This is NOT a heuristic - just raw data formatting
	if rp.supervisor == nil {
		// Get last 50 lines of output for basic visibility
		sample := getOutputSample(result.Output, 50)
		basicSummary := fmt.Sprintf("Agent completed with exit code %d\n\nLast %d lines of output:\n%s",
			result.ExitCode, len(sample), strings.Join(sample, "\n"))
		return basicSummary, nil
	}

	// Target summary length: aim for ~2000 chars (enough for meaningful summary)
	const maxSummaryLength = 2000

	summary, err := rp.supervisor.SummarizeAgentOutput(ctx, issue, fullOutput, maxSummaryLength)
	if err != nil {
		// AI summarization failed - return basic data summary as fallback
		// This maintains system functionality while logging the AI failure
		fmt.Fprintf(os.Stderr, "Warning: AI summarization failed: %v (using basic summary)\n", err)
		sample := getOutputSample(result.Output, 50)
		basicSummary := fmt.Sprintf("Agent completed with exit code %d\n\n(AI summarization failed: %v)\n\nLast %d lines of output:\n%s",
			result.ExitCode, err, len(sample), strings.Join(sample, "\n"))
		return basicSummary, nil
	}

	return summary, nil
}

// buildAnalysisComment creates a formatted comment from AI analysis
func (rp *ResultsProcessor) buildAnalysisComment(analysis *ai.Analysis) string {
	var comment strings.Builder

	comment.WriteString("**AI Analysis**\n\n")
	comment.WriteString(fmt.Sprintf("Completed: %v\n\n", analysis.Completed))
	comment.WriteString(fmt.Sprintf("Summary: %s\n\n", analysis.Summary))

	if len(analysis.PuntedItems) > 0 {
		comment.WriteString("Punted Work:\n")
		for _, item := range analysis.PuntedItems {
			comment.WriteString(fmt.Sprintf("- %s\n", item))
		}
		comment.WriteString("\n")
	}

	if len(analysis.QualityIssues) > 0 {
		comment.WriteString("Quality Issues:\n")
		for _, issue := range analysis.QualityIssues {
			comment.WriteString(fmt.Sprintf("- %s\n", issue))
		}
		comment.WriteString("\n")
	}

	return comment.String()
}

// buildSummary creates a human-readable summary of the processing result
func (rp *ResultsProcessor) buildSummary(issue *types.Issue, agentResult *AgentResult, analysis *ai.Analysis, procResult *ProcessingResult) string {
	var summary strings.Builder

	summary.WriteString(fmt.Sprintf("=== Processing Complete for %s ===\n\n", issue.ID))
	summary.WriteString(fmt.Sprintf("Title: %s\n", issue.Title))
	summary.WriteString(fmt.Sprintf("Duration: %v\n", agentResult.Duration))
	summary.WriteString(fmt.Sprintf("Success: %v\n", agentResult.Success))

	if procResult.Completed {
		summary.WriteString(fmt.Sprintf("Status: ✓ Closed\n"))
	} else {
		summary.WriteString(fmt.Sprintf("Status: Still open (incomplete)\n"))
	}

	if analysis != nil {
		summary.WriteString(fmt.Sprintf("\nAI Analysis Summary: %s\n", analysis.Summary))

		if len(analysis.PuntedItems) > 0 {
			summary.WriteString(fmt.Sprintf("Punted items: %d\n", len(analysis.PuntedItems)))
		}

		if len(analysis.QualityIssues) > 0 {
			summary.WriteString(fmt.Sprintf("Quality issues found: %d\n", len(analysis.QualityIssues)))
		}
	}

	if len(procResult.DiscoveredIssues) > 0 {
		summary.WriteString(fmt.Sprintf("\n✓ Created %d discovered issues: %v\n",
			len(procResult.DiscoveredIssues), procResult.DiscoveredIssues))
	}

	if !procResult.GatesPassed {
		summary.WriteString("\n✗ Quality gates failed - issue blocked\n")
	}

	if procResult.CommitHash != "" {
		summary.WriteString(fmt.Sprintf("\n✓ Auto-committed: %s\n", procResult.CommitHash[:8]))
	}

	return summary.String()
}

// logEvent creates and stores an agent event for observability
func (rp *ResultsProcessor) logEvent(ctx context.Context, eventType events.EventType, severity events.EventSeverity, issueID, message string, data map[string]interface{}) {
	// Skip logging if context is cancelled (e.g., during shutdown)
	if ctx.Err() != nil {
		return
	}

	event := &events.AgentEvent{
		ID:         uuid.New().String(),
		Type:       eventType,
		Timestamp:  time.Now(),
		IssueID:    issueID,
		ExecutorID: rp.actor,
		AgentID:    "", // Empty for executor-level events (not produced by coding agents)
		Severity:   severity,
		Message:    message,
		Data:       data,
		SourceLine: 0, // Not applicable for executor-level events
	}

	if err := rp.store.StoreAgentEvent(ctx, event); err != nil {
		// Log error but don't fail execution
		fmt.Fprintf(os.Stderr, "warning: failed to store agent event: %v\n", err)
	}
}

// getOutputSample returns the last N lines of output, or all if fewer than N
func getOutputSample(output []string, maxLines int) []string {
	if len(output) == 0 {
		return []string{"(no output)"}
	}

	if len(output) <= maxLines {
		return output
	}

	return output[len(output)-maxLines:]
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
		if !((c >= '0' && c <= '9') ||
		     (c >= 'a' && c <= 'z') ||
		     (c >= 'A' && c <= 'Z') ||
		     c == '-' || c == '_' || c == '/' || c == '~' || c == '^' || c == '.') {
			return false
		}
	}

	return true
}

// createCodeReviewIssue creates a blocking code review issue for the given commit
func (rp *ResultsProcessor) createCodeReviewIssue(ctx context.Context, parentIssue *types.Issue, commitHash, reasoning string) (string, error) {
	// Create issue title
	title := fmt.Sprintf("Code review: %s", parentIssue.Title)

	// Build detailed description
	description := fmt.Sprintf(`Code review requested by AI supervisor for changes made in %s.

**Original Issue:** %s
**Commit:** %s

**AI Reasoning:**
%s

**Review Instructions:**
1. Review the changes in commit %s
2. Check for correctness, security issues, and code quality
3. Add review comments to this issue
4. Close this issue when review is complete

_This issue was automatically created by AI code review analysis._`,
		parentIssue.ID,
		parentIssue.ID,
		commitHash[:8],
		reasoning,
		commitHash[:8])

	// Create the code review issue
	reviewIssue := &types.Issue{
		Title:       title,
		Description: description,
		IssueType:   types.TypeTask,
		Status:      types.StatusOpen,
		Priority:    1, // P1 - high priority
		Assignee:    "ai-supervisor",
	}

	err := rp.store.CreateIssue(ctx, reviewIssue, "ai-supervisor")
	if err != nil {
		return "", fmt.Errorf("failed to create code review issue: %w", err)
	}

	reviewIssueID := reviewIssue.ID

	// Add blocking dependency: parent issue is blocked by review issue
	// This ensures the parent can't be considered "done" until review is complete
	dep := &types.Dependency{
		IssueID:     parentIssue.ID,          // Parent issue
		DependsOnID: reviewIssueID,           // Depends on review issue
		Type:        types.DepBlocks,         // Review blocks parent
	}
	if err := rp.store.AddDependency(ctx, dep, "ai-supervisor"); err != nil {
		// Log warning but don't fail - issue was created successfully
		fmt.Fprintf(os.Stderr, "warning: failed to add blocking dependency %s -> %s: %v\n",
			parentIssue.ID, reviewIssueID, err)
	}

	// Add comment to parent issue about code review
	reviewComment := fmt.Sprintf("Code review issue created: %s\n\nThis issue is now blocked pending code review.", reviewIssueID)
	if err := rp.store.AddComment(ctx, parentIssue.ID, "ai-supervisor", reviewComment); err != nil {
		fmt.Fprintf(os.Stderr, "warning: failed to add code review comment to parent: %v\n", err)
	}

	return reviewIssueID, nil
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
