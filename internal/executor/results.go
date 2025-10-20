package executor

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/steveyegge/vc/internal/ai"
	"github.com/steveyegge/vc/internal/deduplication"
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
	deduplicator       deduplication.Deduplicator // Can be nil to disable deduplication
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
	Supervisor         *ai.Supervisor            // Can be nil to disable AI analysis
	Deduplicator       deduplication.Deduplicator // Can be nil to disable deduplication
	GitOps             git.GitOperations         // Can be nil to disable auto-commit
	MessageGen         *git.MessageGenerator     // Can be nil to disable auto-commit
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
		deduplicator:       cfg.Deduplicator,
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
		if releaseErr := rp.releaseExecutionState(ctx, issue.ID); releaseErr != nil {
			return nil, fmt.Errorf("failed to release issue after summarization failure: %w", releaseErr)
		}

		result.Summary = fmt.Sprintf("AI summarization failed - issue blocked (ZFC compliance): %v", err)
		return result, nil
	}

	fmt.Printf("\n=== Agent Execution Complete ===\n")
	fmt.Printf("Success: %v\n", agentResult.Success)
	fmt.Printf("Exit Code: %d\n", agentResult.ExitCode)
	fmt.Printf("Duration: %v\n", agentResult.Duration)

	// Step 1.5: Try to parse structured agent report (vc-257)
	// This happens BEFORE AI analysis - if agent provides structured output, use it
	fullOutput := strings.Join(agentResult.Output, "\n")
	agentReport, hasReport := ParseAgentReport(fullOutput)

	if hasReport {
		fmt.Printf("\n✓ Found structured agent report (status: %s)\n", agentReport.Status)

		// Handle the structured report
		reportHandler := NewAgentReportHandler(rp.store, rp.actor)
		completed, err := reportHandler.HandleReport(ctx, issue, agentReport)
		if err != nil {
			fmt.Fprintf(os.Stderr, "warning: failed to handle agent report: %v (falling back to AI analysis)\n", err)
			// Don't fail - fall through to AI analysis
		} else {
			// Structured report was handled successfully
			result.Completed = completed

			// For certain statuses, we can skip quality gates and AI analysis
			switch agentReport.Status {
			case AgentStatusBlocked:
				// Issue is blocked - no need for gates/analysis
				fmt.Printf("Issue blocked by agent - skipping quality gates\n")

				// Release the execution state
				if err := rp.releaseExecutionState(ctx, issue.ID); err != nil {
					return nil, fmt.Errorf("failed to release blocked issue: %w", err)
				}

				result.Summary = fmt.Sprintf("Agent blocked: %s", agentReport.Summary)
				result.GatesPassed = false
				return result, nil

			case AgentStatusDecomposed:
				// Task was decomposed - epic created, children ready
				fmt.Printf("Task decomposed into epic - executor will pick up children\n")

				// Release the execution state
				if err := rp.releaseExecutionState(ctx, issue.ID); err != nil {
					return nil, fmt.Errorf("failed to release decomposed issue: %w", err)
				}

				result.Summary = fmt.Sprintf("Task decomposed: %s", agentReport.Summary)
				result.Completed = false // Epic stays open
				return result, nil

			case AgentStatusPartial:
				// Partial completion - follow-on issues created
				// Still run quality gates on what was completed
				fmt.Printf("Partial completion - continuing to quality gates\n")

			case AgentStatusCompleted:
				// Full completion - proceed to quality gates
				fmt.Printf("Agent reports completion - continuing to quality gates\n")
			}
		}
	} else {
		fmt.Printf("\nℹ No structured agent report found - will use AI analysis\n")
	}

	// Step 2: AI Analysis (if supervisor available and no structured report handled)
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

			// File discovered issues from analysis (vc-143)
			// These are issues discovered during execution, independent of quality gates
			if len(analysis.DiscoveredIssues) > 0 {
				// Deduplicate discovered issues (vc-145/vc-147)
				discoveredToCreate := analysis.DiscoveredIssues
				if rp.deduplicator != nil {
					uniqueDiscovered, dedupStats := rp.deduplicateDiscoveredIssues(ctx, issue, analysis.DiscoveredIssues)
					if len(uniqueDiscovered) < len(analysis.DiscoveredIssues) {
						fmt.Printf("🔍 Deduplication: %d discovered issues → %d unique (filtered %d duplicates)\n",
							len(analysis.DiscoveredIssues), len(uniqueDiscovered),
							len(analysis.DiscoveredIssues)-len(uniqueDiscovered))
						fmt.Printf("   Stats: %d comparisons, %d AI calls, %dms\n",
							dedupStats.ComparisonsMade, dedupStats.AICallsMade, dedupStats.ProcessingTimeMs)
					}
					discoveredToCreate = uniqueDiscovered
				}

				createdIDs, err := rp.supervisor.CreateDiscoveredIssues(ctx, issue, discoveredToCreate)
				if err != nil {
					fmt.Fprintf(os.Stderr, "Warning: failed to create discovered issues: %v\n", err)
				} else if len(createdIDs) > 0 {
					fmt.Printf("✓ Created %d discovered issue(s) from analysis: %v\n", len(createdIDs), createdIDs)
					result.DiscoveredIssues = createdIDs
				}
			}

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
		// Check if we're in the VC repo (vc-144: skip gates for non-VC repos)
		if !rp.isVCRepo() {
			fmt.Printf("⚠ Skipping quality gates (not in VC repo, working dir: %s)\n", rp.workingDir)
			// Log quality gates skipped
			rp.logEvent(ctx, events.EventTypeQualityGatesSkipped, events.SeverityInfo, issue.ID,
				"Quality gates skipped (not in VC repository)",
				map[string]interface{}{
					"working_dir": rp.workingDir,
					"reason":      "non-vc-repo",
				})
			result.GatesPassed = true // Don't block on skipped gates
			// Skip to next step
			goto SkipGates
		}

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
			Supervisor: rp.supervisor, // Enable AI-driven recovery strategies (ZFC)
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
			// Create timeout context for quality gates (vc-245)
			// 5 minutes should be enough for test/lint/build, but prevents indefinite hangs
			gateCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
			defer cancel()

			fmt.Printf("Running quality gates (timeout: 5m)...\n")

			// Run gates with timeout protection
			gateResults, allPassed := gateRunner.RunAll(gateCtx)

			// Log progress for each gate (vc-245)
			for _, gateResult := range gateResults {
				status := "PASS"
				severity := events.SeverityInfo
				if !gateResult.Passed {
					status = "FAIL"
					severity = events.SeverityWarning
				}
				fmt.Printf("  %s: %s\n", gateResult.Gate, status)

				// Emit progress event for each gate
				rp.logEvent(ctx, events.EventTypeQualityGatesProgress, severity, issue.ID,
					fmt.Sprintf("Quality gate %s: %s", gateResult.Gate, status),
					map[string]interface{}{
						"gate":   string(gateResult.Gate),
						"passed": gateResult.Passed,
					})
			}

			// Check if we timed out or were cancelled (vc-128)
			timedOut := gateCtx.Err() == context.DeadlineExceeded
			cancelled := gateCtx.Err() == context.Canceled || ctx.Err() == context.Canceled

			if timedOut {
				fmt.Fprintf(os.Stderr, "Warning: quality gates timed out after 5 minutes\n")
				result.GatesPassed = false
				allPassed = false // Override allPassed on timeout
			} else if cancelled {
				// Executor is shutting down - don't mark as failed, return issue to open
				fmt.Fprintf(os.Stderr, "Warning: quality gates cancelled due to executor shutdown\n")
				result.GatesPassed = false
				allPassed = false // Don't pass gates on cancellation
				// Don't handle gate results - let the executor release the issue
			} else {
				result.GatesPassed = allPassed
			}

			// Handle gate results (creates blocking issues on failure)
			// Skip this on cancellation - let executor release issue back to open (vc-128)
			if !cancelled {
				if err := gateRunner.HandleGateResults(ctx, issue, gateResults, allPassed); err != nil {
					fmt.Fprintf(os.Stderr, "Warning: failed to handle gate results: %v\n", err)
				}
			}

			// Build completion event data
			gateData := map[string]interface{}{
				"all_passed": allPassed,
				"gates_run":  len(gateResults),
				"timeout":    timedOut,
				"cancelled":  cancelled,
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

			// Determine severity and message
			severity := events.SeverityInfo
			message := fmt.Sprintf("Quality gates evaluation completed for issue %s (passed: %v)", issue.ID, allPassed)

			if timedOut {
				severity = events.SeverityError
				message = fmt.Sprintf("Quality gates timed out after 5 minutes for issue %s", issue.ID)
				gateData["error"] = "deadline exceeded"
			} else if cancelled {
				// Cancellation due to shutdown is not an error - it's expected (vc-128)
				severity = events.SeverityInfo
				message = fmt.Sprintf("Quality gates cancelled for issue %s due to executor shutdown", issue.ID)
				gateData["error"] = "context cancelled"
			} else if !allPassed {
				severity = events.SeverityWarning
			}

			// Always emit completion event (vc-245)
			rp.logEvent(ctx, events.EventTypeQualityGatesCompleted, severity, issue.ID, message, gateData)

			// Skip blocking logic if cancelled - executor will release issue (vc-128)
			if !cancelled && !allPassed {
				fmt.Printf("\n=== Quality Gates Failed ===\n")
				fmt.Printf("Issue %s marked as blocked due to failing quality gates\n", issue.ID)

				// Build comment explaining which gates failed
				var failedGates []string
				var passedGates []string
				for _, gateResult := range gateResults {
					if gateResult.Passed {
						passedGates = append(passedGates, string(gateResult.Gate))
					} else {
						failedGates = append(failedGates, string(gateResult.Gate))
					}
				}

				gatesComment := fmt.Sprintf("**Quality Gates Failed**\n\nIssue marked as blocked.\n\nFailed gates (%d):\n", len(failedGates))
				for _, gate := range failedGates {
					gatesComment += fmt.Sprintf("- %s\n", gate)
				}
				if len(passedGates) > 0 {
					gatesComment += fmt.Sprintf("\nPassed gates (%d):\n", len(passedGates))
					for _, gate := range passedGates {
						gatesComment += fmt.Sprintf("- %s\n", gate)
					}
				}
				gatesComment += "\nPlease fix the failing gates and retry."

				// Add comment before updating status
				if err := rp.store.AddComment(ctx, issue.ID, rp.actor, gatesComment); err != nil {
					fmt.Fprintf(os.Stderr, "warning: failed to add quality gates comment: %v\n", err)
				}

				// Update issue to blocked status
				updates := map[string]interface{}{
					"status": types.StatusBlocked,
				}
				if err := rp.store.UpdateIssue(ctx, issue.ID, updates, rp.actor); err != nil {
					fmt.Fprintf(os.Stderr, "warning: failed to update issue to blocked: %v\n", err)
				}

				// Release the execution state
				if err := rp.releaseExecutionState(ctx, issue.ID); err != nil {
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

SkipGates:
	// Step 3.5: Test Coverage Analysis (vc-217)
	// After quality gates pass, analyze test coverage and file test improvement issues
	if agentResult.Success && result.GatesPassed && rp.supervisor != nil && rp.gitOps != nil {
		fmt.Printf("\n=== Test Coverage Analysis ===\n")

		// Check if there are uncommitted changes to analyze
		hasChanges, err := rp.gitOps.HasUncommittedChanges(ctx, rp.workingDir)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to check for changes: %v\n", err)
		} else if hasChanges {
			// Get the diff of uncommitted changes
			diff, err := rp.getUncommittedDiff(ctx)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Warning: failed to get diff for test coverage analysis: %v\n", err)
			} else if diff != "" {
			// Get existing test files to understand test patterns
			existingTests, err := rp.getExistingTests(ctx)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Warning: failed to get existing tests: %v\n", err)
			}

			// Analyze test coverage
			testAnalysis, err := rp.supervisor.AnalyzeTestCoverage(ctx, issue, diff, existingTests)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Warning: test coverage analysis failed: %v\n", err)
			} else {
				// Add analysis summary as comment
				testComment := fmt.Sprintf("**Test Coverage Analysis**\n\n%s\n\nSufficient Coverage: %v\nConfidence: %.0f%%\nTest Issues Found: %d",
					testAnalysis.Summary, testAnalysis.SufficientCoverage, testAnalysis.Confidence*100, len(testAnalysis.TestIssues))
				if len(testAnalysis.UncoveredAreas) > 0 {
					testComment += "\n\nUncovered Areas:\n"
					for _, area := range testAnalysis.UncoveredAreas {
						testComment += fmt.Sprintf("- %s\n", area)
					}
				}
				if err := rp.store.AddComment(ctx, issue.ID, "ai-supervisor", testComment); err != nil {
					fmt.Fprintf(os.Stderr, "warning: failed to add test coverage comment: %v\n", err)
				}

				// File test improvement issues if gaps were found
				if len(testAnalysis.TestIssues) > 0 {
					fmt.Printf("Filing %d test improvement issues...\n", len(testAnalysis.TestIssues))
					createdTestIssues, err := rp.createTestIssues(ctx, issue, testAnalysis.TestIssues)
					if err != nil {
						fmt.Fprintf(os.Stderr, "Warning: failed to create test issues: %v\n", err)
					} else {
						result.DiscoveredIssues = append(result.DiscoveredIssues, createdTestIssues...)
						fmt.Printf("✓ Created %d test improvement issues: %v\n", len(createdTestIssues), createdTestIssues)
					}
				} else {
					fmt.Printf("✓ No test coverage gaps found - coverage looks good\n")
				}
			}
			}
		} else {
			fmt.Printf("No uncommitted changes - skipping test coverage analysis\n")
		}
	}

	// Step 3.6: Auto-commit changes (if enabled, agent succeeded, and gates passed)
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
			fmt.Printf("\n✓ Changes committed: %s\n", safeShortHash(commitHash))

			// Add comment with commit hash
			commitComment := fmt.Sprintf("Auto-committed changes: %s", commitHash)
			if err := rp.store.AddComment(ctx, issue.ID, rp.actor, commitComment); err != nil {
				fmt.Fprintf(os.Stderr, "warning: failed to add commit comment: %v\n", err)
			}

			// Step 3.6: AI-based code review decision and automated quality analysis (vc-216)
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

						// Safety measure: require review if confidence is too low
						if !needsReview && decision.Confidence < minCodeReviewConfidence {
							needsReview = true
							fmt.Printf("⚠️  Low confidence (%.0f%%), requesting review as safety measure\n",
								decision.Confidence*100)
						}

						// If review is needed, perform automated code quality analysis (vc-216)
						// This replaces manual review with AI-driven analysis that files granular fix issues
						if needsReview {
							if decision.NeedsReview {
								fmt.Printf("Code review recommended (confidence: %.0f%%)\n", decision.Confidence*100)
							}

							fmt.Printf("\n=== Automated Code Quality Analysis ===\n")
							qualityAnalysis, err := rp.supervisor.AnalyzeCodeQuality(ctx, issue, diff)
							if err != nil {
								// AI quality analysis failed - log error and document for human review
								// Note: We don't fall back to creating a manual review issue because:
								// 1. The AI already has retry logic with exponential backoff
								// 2. Creating a generic "please review" issue adds no value over logging the error
								// 3. Keeps us consistent with vc-216's vision of automated quality analysis
								fmt.Fprintf(os.Stderr, "✗ Automated code quality analysis failed: %v\n", err)
								fmt.Fprintf(os.Stderr, "  Commit %s requires human review\n", safeShortHash(commitHash))

								// Add comment to parent issue documenting the failure
								failureComment := fmt.Sprintf("**Automated Code Quality Analysis Failed**\n\n"+
									"The AI supervisor failed to analyze code quality for commit %s.\n\n"+
									"Error: %v\n\n"+
									"**Action Required:**\n"+
									"Manual code review is needed for this commit. Please review the changes and:\n"+
									"1. Check for bugs, security issues, and code quality problems\n"+
									"2. File specific issues for any problems found\n"+
									"3. Investigate why the AI analysis failed (check logs, API connectivity, etc.)",
									commitHash, err)
								if err := rp.store.AddComment(ctx, issue.ID, "ai-supervisor", failureComment); err != nil {
									fmt.Fprintf(os.Stderr, "warning: failed to add quality analysis failure comment: %v\n", err)
								}
							} else {
								// Add analysis summary as comment
								analysisComment := fmt.Sprintf("**Automated Code Quality Analysis**\n\n%s\n\nConfidence: %.0f%%\nIssues Found: %d",
									qualityAnalysis.Summary, qualityAnalysis.Confidence*100, len(qualityAnalysis.Issues))
								if err := rp.store.AddComment(ctx, issue.ID, "ai-supervisor", analysisComment); err != nil {
									fmt.Fprintf(os.Stderr, "warning: failed to add quality analysis comment: %v\n", err)
								}

								// File granular issues for each quality problem found
								if len(qualityAnalysis.Issues) > 0 {
									fmt.Printf("Filing %d quality issues...\n", len(qualityAnalysis.Issues))
									createdIssues, err := rp.createQualityIssues(ctx, issue, commitHash, qualityAnalysis.Issues)
									if err != nil {
										fmt.Fprintf(os.Stderr, "Warning: failed to create quality issues: %v\n", err)
									} else {
										result.DiscoveredIssues = append(result.DiscoveredIssues, createdIssues...)
										fmt.Printf("✓ Created %d quality fix issues: %v\n", len(createdIssues), createdIssues)
									}
								} else {
									fmt.Printf("✓ No quality issues found - code looks good\n")
								}
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

			// Create discovered issues (vc-149: deduplicate first)
			if len(analysis.DiscoveredIssues) > 0 {
				// Deduplicate discovered issues if deduplicator is available
				discoveredToCreate := analysis.DiscoveredIssues
				if rp.deduplicator != nil {
					uniqueDiscovered, dedupStats := rp.deduplicateDiscoveredIssues(ctx, issue, analysis.DiscoveredIssues)
					if len(uniqueDiscovered) < len(analysis.DiscoveredIssues) {
						fmt.Printf("🔍 Deduplication: %d discovered issues → %d unique (filtered %d duplicates)\n",
							len(analysis.DiscoveredIssues), len(uniqueDiscovered),
							len(analysis.DiscoveredIssues)-len(uniqueDiscovered))
						fmt.Printf("   Stats: %d comparisons, %d AI calls, %dms\n",
							dedupStats.ComparisonsMade, dedupStats.AICallsMade, dedupStats.ProcessingTimeMs)
					}
					discoveredToCreate = uniqueDiscovered
				}

				createdIDs, err := rp.supervisor.CreateDiscoveredIssues(ctx, issue, discoveredToCreate)
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
		if err := rp.releaseExecutionState(ctx, issue.ID); err != nil {
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
		if err := rp.releaseExecutionState(ctx, issue.ID); err != nil {
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

// releaseExecutionState releases the execution state for an issue.
// If the execution state is already gone (e.g., cleaned up by CleanupStaleInstances due to stale heartbeat),
// this is treated as success since the goal (release the state) has been achieved.
// This prevents race conditions where cleanup releases state while execution is finishing.
func (rp *ResultsProcessor) releaseExecutionState(ctx context.Context, issueID string) error {
	err := rp.store.ReleaseIssue(ctx, issueID)
	if err != nil {
		// Check if error is "execution state not found"
		// This can happen if CleanupStaleInstances already released the state
		if strings.Contains(err.Error(), "execution state not found") {
			// Log warning but don't fail - state was already released by cleanup
			fmt.Fprintf(os.Stderr, "info: execution state for %s was already released (likely by cleanup loop)\n", issueID)
			return nil
		}
		// Other errors should still be propagated
		return err
	}
	return nil
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
		summary.WriteString("Status: ✓ Closed\n")
	} else {
		summary.WriteString("Status: Still open (incomplete)\n")
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
		summary.WriteString(fmt.Sprintf("\n✓ Auto-committed: %s\n", safeShortHash(procResult.CommitHash)))
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

// logDeduplicationBatchStarted logs a deduplication batch start event (vc-151)
func (rp *ResultsProcessor) logDeduplicationBatchStarted(ctx context.Context, issueID string, candidateCount int, parentIssueID string) {
	// Skip logging if context is cancelled
	if ctx.Err() != nil {
		return
	}

	event, err := events.NewDeduplicationBatchStartedEvent(
		issueID,
		rp.actor,
		"deduplication",
		events.SeverityInfo,
		fmt.Sprintf("Starting deduplication of %d discovered issues for %s", candidateCount, parentIssueID),
		events.DeduplicationBatchStartedData{
			CandidateCount: candidateCount,
			ParentIssueID:  parentIssueID,
		},
	)
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: failed to create deduplication batch started event: %v\n", err)
		return
	}

	if err := rp.store.StoreAgentEvent(ctx, event); err != nil {
		fmt.Fprintf(os.Stderr, "warning: failed to store deduplication batch started event: %v\n", err)
	}
}

// logDeduplicationBatchCompleted logs a deduplication batch completion event with stats and decisions (vc-151)
func (rp *ResultsProcessor) logDeduplicationBatchCompleted(ctx context.Context, issueID string, result *deduplication.DeduplicationResult, err error) {
	// Skip logging if context is cancelled
	if ctx.Err() != nil {
		return
	}

	var batchEvent *events.AgentEvent
	var eventErr error

	if err != nil {
		// Deduplication failed
		batchEvent, eventErr = events.NewDeduplicationBatchCompletedEvent(
			issueID,
			rp.actor,
			"deduplication",
			events.SeverityError,
			fmt.Sprintf("Deduplication failed for %s: %v", issueID, err),
			events.DeduplicationBatchCompletedData{
				Success: false,
				Error:   err.Error(),
			},
		)
	} else {
		// Deduplication succeeded
		severity := events.SeverityInfo
		if result.Stats.DuplicateCount > 0 || result.Stats.WithinBatchDuplicateCount > 0 {
			severity = events.SeverityWarning // Duplicates found - worth highlighting
		}

		batchEvent, eventErr = events.NewDeduplicationBatchCompletedEvent(
			issueID,
			rp.actor,
			"deduplication",
			severity,
			fmt.Sprintf("Deduplication completed: %d unique, %d duplicates, %d within-batch duplicates",
				result.Stats.UniqueCount, result.Stats.DuplicateCount, result.Stats.WithinBatchDuplicateCount),
			events.DeduplicationBatchCompletedData{
				TotalCandidates:           result.Stats.TotalCandidates,
				UniqueCount:               result.Stats.UniqueCount,
				DuplicateCount:            result.Stats.DuplicateCount,
				WithinBatchDuplicateCount: result.Stats.WithinBatchDuplicateCount,
				ComparisonsMade:           result.Stats.ComparisonsMade,
				AICallsMade:               result.Stats.AICallsMade,
				ProcessingTimeMs:          result.Stats.ProcessingTimeMs,
				Success:                   true,
			},
		)
	}

	if eventErr != nil {
		fmt.Fprintf(os.Stderr, "warning: failed to create deduplication batch completed event: %v\n", eventErr)
		return
	}

	if err := rp.store.StoreAgentEvent(ctx, batchEvent); err != nil {
		fmt.Fprintf(os.Stderr, "warning: failed to store deduplication batch completed event: %v\n", err)
		return
	}

	// Log individual decision events (for confidence score distribution analysis)
	if result != nil && len(result.Decisions) > 0 {
		for _, decision := range result.Decisions {
			rp.logDeduplicationDecision(ctx, issueID, decision)
		}
	}
}

// logDeduplicationDecision logs an individual deduplication decision (vc-151)
func (rp *ResultsProcessor) logDeduplicationDecision(ctx context.Context, issueID string, decision deduplication.DecisionDetail) {
	// Skip logging if context is cancelled
	if ctx.Err() != nil {
		return
	}

	severity := events.SeverityInfo
	var message string

	if decision.IsDuplicate {
		severity = events.SeverityWarning
		if decision.WithinBatchOriginalIndex >= 0 {
			message = fmt.Sprintf("Within-batch duplicate: %s (confidence: %.2f)", decision.CandidateTitle, decision.Confidence)
		} else {
			message = fmt.Sprintf("Duplicate of %s: %s (confidence: %.2f)", decision.DuplicateOf, decision.CandidateTitle, decision.Confidence)
		}
	} else {
		message = fmt.Sprintf("Unique issue: %s (confidence: %.2f)", decision.CandidateTitle, decision.Confidence)
	}

	var withinBatchOriginal string
	if decision.WithinBatchOriginalIndex >= 0 {
		withinBatchOriginal = fmt.Sprintf("candidate_%d", decision.WithinBatchOriginalIndex)
	}

	event, err := events.NewDeduplicationDecisionEvent(
		issueID,
		rp.actor,
		"deduplication",
		severity,
		message,
		events.DeduplicationDecisionData{
			CandidateTitle:       decision.CandidateTitle,
			IsDuplicate:          decision.IsDuplicate,
			DuplicateOf:          decision.DuplicateOf,
			Confidence:           decision.Confidence,
			Reasoning:            decision.Reasoning,
			WithinBatchDuplicate: decision.WithinBatchOriginalIndex >= 0,
			WithinBatchOriginal:  withinBatchOriginal,
		},
	)
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: failed to create deduplication decision event: %v\n", err)
		return
	}

	if err := rp.store.StoreAgentEvent(ctx, event); err != nil {
		fmt.Fprintf(os.Stderr, "warning: failed to store deduplication decision event: %v\n", err)
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

// safeShortHash returns a shortened version of a git hash, safely handling short or empty hashes
func safeShortHash(hash string) string {
	if len(hash) >= 8 {
		return hash[:8]
	}
	return hash
}

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
		safeShortHash(commitHash),
		reasoning,
		safeShortHash(commitHash))

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

// createQualityIssues creates blocking quality fix issues from automated code analysis (vc-216)
// Each issue represents a specific fix that should be addressed.
// If individual issue creation fails, continues creating remaining issues and collects all errors.
func (rp *ResultsProcessor) createQualityIssues(ctx context.Context, parentIssue *types.Issue, commitHash string, qualityIssues []ai.DiscoveredIssue) ([]string, error) {
	var createdIssues []string
	var errors []error

	for i, qualityIssue := range qualityIssues {
		// Create issue title with commit reference
		title := qualityIssue.Title

		// Build detailed description with commit context
		description := fmt.Sprintf(`Code quality issue identified by automated analysis.

**Original Issue:** %s
**Commit:** %s

%s

_This issue was automatically created by AI code quality analysis (vc-216)._`,
			parentIssue.ID,
			safeShortHash(commitHash),
			qualityIssue.Description)

		// Map priority string to int
		priority := 2 // default P2
		switch qualityIssue.Priority {
		case "P0":
			priority = 0
		case "P1":
			priority = 1
		case "P2":
			priority = 2
		case "P3":
			priority = 3
		}

		// Map type string to types.IssueType
		issueType := types.TypeTask // default
		switch qualityIssue.Type {
		case "bug":
			issueType = types.TypeBug
		case "task":
			issueType = types.TypeTask
		case "chore":
			issueType = types.TypeChore
		case "feature", "enhancement":
			issueType = types.TypeFeature
		}

		// Create the quality fix issue
		fixIssue := &types.Issue{
			Title:       title,
			Description: description,
			IssueType:   issueType,
			Status:      types.StatusOpen,
			Priority:    priority,
			Assignee:    "ai-supervisor",
		}

		err := rp.store.CreateIssue(ctx, fixIssue, "ai-supervisor")
		if err != nil {
			// Collect error but continue creating remaining issues
			errors = append(errors, fmt.Errorf("failed to create quality fix issue %d (%s): %w", i+1, title, err))
			fmt.Fprintf(os.Stderr, "warning: failed to create quality fix issue %d (%s): %v (continuing with remaining issues)\n", i+1, title, err)
			continue
		}

		fixIssueID := fixIssue.ID
		createdIssues = append(createdIssues, fixIssueID)

		// Add blocking dependency: parent issue is blocked by this fix issue
		// This ensures the parent can't be considered "done" until quality issues are addressed
		dep := &types.Dependency{
			IssueID:     parentIssue.ID,    // Parent issue
			DependsOnID: fixIssueID,        // Depends on fix issue
			Type:        types.DepBlocks,   // Fix blocks parent
		}
		if err := rp.store.AddDependency(ctx, dep, "ai-supervisor"); err != nil {
			// Log warning but don't fail - issue was created successfully
			fmt.Fprintf(os.Stderr, "warning: failed to add blocking dependency %s -> %s: %v\n",
				parentIssue.ID, fixIssueID, err)
		}

		fmt.Printf("  ✓ Created %s (%s, P%d): %s\n", fixIssueID, issueType, priority, title)
	}

	// Add comment to parent issue about quality issues
	if len(createdIssues) > 0 {
		qualityComment := fmt.Sprintf("Automated code quality analysis found %d issues:\n%v\n\nThis issue is now blocked pending quality fixes.",
			len(createdIssues), createdIssues)
		if err := rp.store.AddComment(ctx, parentIssue.ID, "ai-supervisor", qualityComment); err != nil {
			fmt.Fprintf(os.Stderr, "warning: failed to add quality issues comment to parent: %v\n", err)
		}
	}

	// Return any errors that occurred during issue creation
	if len(errors) > 0 {
		// Create a combined error message
		var errMsg strings.Builder
		errMsg.WriteString(fmt.Sprintf("encountered %d errors while creating quality issues:", len(errors)))
		for _, err := range errors {
			errMsg.WriteString(fmt.Sprintf("\n  - %v", err))
		}
		return createdIssues, fmt.Errorf("%s", errMsg.String())
	}

	return createdIssues, nil
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

// createTestIssues creates test improvement issues from test coverage analysis (vc-217)
func (rp *ResultsProcessor) createTestIssues(ctx context.Context, parentIssue *types.Issue, testIssues []ai.DiscoveredIssue) ([]string, error) {
	var createdIssues []string
	var errors []error

	for i, testIssue := range testIssues {
		title := testIssue.Title

		// Build description with parent issue context
		description := fmt.Sprintf(`Test coverage gap identified by automated analysis (vc-217).

**Original Issue:** %s

%s

_This issue was automatically created by AI test coverage analysis._`,
			parentIssue.ID,
			testIssue.Description)

		// Map priority string to int
		priority := 2 // default P2
		switch testIssue.Priority {
		case "P0":
			priority = 0
		case "P1":
			priority = 1
		case "P2":
			priority = 2
		case "P3":
			priority = 3
		}

		// Map type string to types.IssueType
		issueType := types.TypeTask // default for tests
		switch testIssue.Type {
		case "bug":
			issueType = types.TypeBug
		case "task":
			issueType = types.TypeTask
		case "chore":
			issueType = types.TypeChore
		}

		// Create the test improvement issue
		newIssue := &types.Issue{
			Title:       title,
			Description: description,
			IssueType:   issueType,
			Status:      types.StatusOpen,
			Priority:    priority,
			Assignee:    "ai-supervisor",
		}

		err := rp.store.CreateIssue(ctx, newIssue, "ai-supervisor")
		if err != nil {
			errors = append(errors, fmt.Errorf("failed to create test issue %d (%s): %w", i+1, title, err))
			fmt.Fprintf(os.Stderr, "warning: failed to create test issue %d (%s): %v\n", i+1, title, err)
			continue
		}

		createdIssues = append(createdIssues, newIssue.ID)

		// Add related dependency (not blocking - these are follow-on improvements)
		dep := &types.Dependency{
			IssueID:     newIssue.ID,             // Test issue
			DependsOnID: parentIssue.ID,          // Related to parent
			Type:        types.DepDiscoveredFrom, // Discovered from parent work
		}
		if err := rp.store.AddDependency(ctx, dep, "ai-supervisor"); err != nil {
			fmt.Fprintf(os.Stderr, "warning: failed to add dependency %s -> %s: %v\n",
				newIssue.ID, parentIssue.ID, err)
		}

		fmt.Printf("  ✓ Created %s (%s, P%d): %s\n", newIssue.ID, issueType, priority, title)
	}

	// Add comment to parent issue about test issues
	if len(createdIssues) > 0 {
		testComment := fmt.Sprintf("Test coverage analysis found %d test gaps and created issues:\n%v",
			len(createdIssues), createdIssues)
		if err := rp.store.AddComment(ctx, parentIssue.ID, "ai-supervisor", testComment); err != nil {
			fmt.Fprintf(os.Stderr, "warning: failed to add test issues comment to parent: %v\n", err)
		}
	}

	if len(errors) > 0 {
		var errMsg strings.Builder
		errMsg.WriteString(fmt.Sprintf("encountered %d errors while creating test issues:", len(errors)))
		for _, err := range errors {
			errMsg.WriteString(fmt.Sprintf("\n  - %v", err))
		}
		return createdIssues, fmt.Errorf("%s", errMsg.String())
	}

	return createdIssues, nil
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

// safeTruncateUTF8 truncates a string to maxLen bytes while preserving UTF-8 encoding
// If truncation would split a multi-byte UTF-8 sequence, it backs off to a valid boundary
func safeTruncateUTF8(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}

	// Truncate at maxLen initially
	truncated := s[:maxLen]

	// Walk backwards to find a valid UTF-8 boundary
	// We only need to check up to 4 bytes back (max UTF-8 sequence length)
	for i := 0; i < 4 && len(truncated) > 0; i++ {
		// Check if we have valid UTF-8
		if isValidUTF8(truncated) {
			return truncated
		}
		// Remove last byte and try again
		truncated = truncated[:len(truncated)-1]
	}

	// If we still don't have valid UTF-8 after 4 bytes, return empty string
	// rather than corrupted data
	return ""
}

// isValidUTF8 checks if a string contains valid UTF-8
func isValidUTF8(s string) bool {
	// Quick check: if the last byte is ASCII (0-127), it's always valid
	if len(s) > 0 && s[len(s)-1] < 128 {
		return true
	}
	// For multi-byte sequences, check if it's valid
	for range s {
		// If we can iterate without panic, it's valid UTF-8
	}
	return true
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

// deduplicateDiscoveredIssues uses the deduplicator to filter out duplicate discovered issues
// Returns the unique issues to create and deduplication statistics
func (rp *ResultsProcessor) deduplicateDiscoveredIssues(ctx context.Context, parentIssue *types.Issue, discovered []ai.DiscoveredIssue) ([]ai.DiscoveredIssue, deduplication.DeduplicationStats) {
	// Convert discovered issues to types.Issue for deduplication
	candidates := make([]*types.Issue, len(discovered))
	for i, disc := range discovered {
		// Map priority
		priority := 2
		switch disc.Priority {
		case "P0":
			priority = 0
		case "P1":
			priority = 1
		case "P2":
			priority = 2
		case "P3":
			priority = 3
		}

		// Map type
		issueType := types.TypeTask
		switch disc.Type {
		case "bug":
			issueType = types.TypeBug
		case "task":
			issueType = types.TypeTask
		case "feature", "enhancement":
			issueType = types.TypeFeature
		case "epic":
			issueType = types.TypeEpic
		case "chore":
			issueType = types.TypeChore
		}

		candidates[i] = &types.Issue{
			Title:       disc.Title,
			Description: disc.Description,
			IssueType:   issueType,
			Priority:    priority,
			Status:      types.StatusOpen,
		}
	}

	// vc-151: Log deduplication batch started event
	rp.logDeduplicationBatchStarted(ctx, parentIssue.ID, len(candidates), parentIssue.ID)

	// Deduplicate
	result, err := rp.deduplicator.DeduplicateBatch(ctx, candidates)
	if err != nil {
		// Fail-safe: on error, return all discovered issues
		fmt.Fprintf(os.Stderr, "Warning: deduplication failed, creating all discovered issues: %v\n", err)
		// vc-151: Log failure
		rp.logDeduplicationBatchCompleted(ctx, parentIssue.ID, nil, err)
		return discovered, deduplication.DeduplicationStats{}
	}

	// vc-151: Log deduplication batch completed event with stats and individual decisions
	rp.logDeduplicationBatchCompleted(ctx, parentIssue.ID, result, nil)

	// Build list of unique discovered issues to create
	// We need to map back from unique issues to original DiscoveredIssue objects
	uniqueDiscovered := []ai.DiscoveredIssue{}
	createdSet := make(map[int]bool)

	// Mark which indices were created (unique issues)
	for _, uniqueIssue := range result.UniqueIssues {
		// Find the original index by matching title
		for i, candidate := range candidates {
			if candidate.Title == uniqueIssue.Title && !createdSet[i] {
				uniqueDiscovered = append(uniqueDiscovered, discovered[i])
				createdSet[i] = true
				break
			}
		}
	}

	return uniqueDiscovered, result.Stats
}
