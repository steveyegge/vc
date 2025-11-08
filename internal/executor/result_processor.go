package executor

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/steveyegge/vc/internal/ai"
	"github.com/steveyegge/vc/internal/events"
	"github.com/steveyegge/vc/internal/gates"
	"github.com/steveyegge/vc/internal/labels"
	"github.com/steveyegge/vc/internal/sandbox"
	"github.com/steveyegge/vc/internal/types"
)

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

	// Set default gates timeout if not specified (vc-xcfw)
	gatesTimeout := cfg.GatesTimeout
	if gatesTimeout == 0 {
		gatesTimeout = 5 * time.Minute
	}

	// Set default dedup batch size if not specified (vc-a80e)
	dedupBatchSize := cfg.DedupBatchSize
	if dedupBatchSize == 0 {
		dedupBatchSize = 100
	}

	return &ResultsProcessor{
		store:              cfg.Store,
		supervisor:         cfg.Supervisor,
		deduplicator:       cfg.Deduplicator,
		gitOps:             cfg.GitOps,
		messageGen:         cfg.MessageGen,
		enableQualityGates: cfg.EnableQualityGates,
		enableAutoCommit:   cfg.EnableAutoCommit,
		enableAutoPR:       cfg.EnableAutoPR,
		workingDir:         cfg.WorkingDir,
		actor:              cfg.Actor,
		sandbox:            cfg.Sandbox,
		sandboxManager:     cfg.SandboxManager,
		executor:           cfg.Executor,
		watchdogConfig:     cfg.WatchdogConfig,
		gatesTimeout:       gatesTimeout,
		dedupBatchSize:     dedupBatchSize,
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

	// Declare gateResults at function scope so approval gate can access them (vc-145)
	var gateResults []*gates.Result

	// Step 1: Extract agent output summary
	agentOutput := rp.extractSummary(ctx, issue, agentResult)

	fmt.Printf("\n=== Agent Execution Complete ===\n")
	fmt.Printf("Success: %v\n", agentResult.Success)
	fmt.Printf("Exit Code: %d\n", agentResult.ExitCode)
	fmt.Printf("Duration: %v\n", agentResult.Duration)

	// Step 1.5 & 2: Handle structured report and AI analysis
	shouldReturn, analysis := rp.handleStructuredReportAndAnalysis(ctx, issue, agentResult, agentOutput, result)
	if shouldReturn {
		return result, nil
	}

	// Step 2.5: Mission Gate Delegation (vc-251)
	// For missions (epics with subtype=mission), defer quality gates to QA workers
	// instead of running them inline (which blocks the executor)
	if delegated, delegatedResult := rp.handleMissionGateDelegation(ctx, issue, agentResult, result); delegated {
		return delegatedResult, nil
	}

	// Step 3: Quality Gates (if enabled and agent succeeded)
	shouldReturn, gateResults = rp.handleQualityGates(ctx, issue, agentResult, result)
	if shouldReturn {
		return result, nil
	}
	// Step 3.4: Human Approval Gate (vc-145)
	// If sandboxes are enabled and quality gates passed, require human approval before merging
	if agentResult.Success && result.GatesPassed && rp.sandbox != nil {
		fmt.Printf("\n=== Human Approval Gate ===\n")

		approvalGate, err := gates.NewApprovalGate(&gates.ApprovalConfig{
			Store:   rp.store,
			Sandbox: rp.sandbox,
			Issue:   issue,
			Results: gateResults,
		})
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to create approval gate: %v (skipping approval)\n", err)
			// Log approval gate error
			rp.logEvent(ctx, events.EventTypeError, events.SeverityWarning, issue.ID,
				fmt.Sprintf("Approval gate creation failed: %v", err),
				map[string]interface{}{
					"error": err.Error(),
				})
		} else {
			// Run approval gate
			approvalResult := approvalGate.Run(ctx)

			// Log approval result
			severity := events.SeverityInfo
			if !approvalResult.Passed {
				severity = events.SeverityWarning
			}
			rp.logEvent(ctx, events.EventTypeProgress, severity, issue.ID,
				fmt.Sprintf("Approval gate: %s", approvalResult.Output),
				map[string]interface{}{
					"passed": approvalResult.Passed,
					"output": approvalResult.Output,
				})

			// Update sandbox approval status
			if approvalResult.Passed {
				rp.sandbox.ApprovalStatus = "approved"
				fmt.Printf("✓ Approved - changes will be merged to main\n")
			} else {
				rp.sandbox.ApprovalStatus = "rejected"
				fmt.Printf("✗ Rejected - changes will not be merged\n")

				// Add comment to issue
				comment := fmt.Sprintf("**Human Review: Rejected**\n\n%s\n\nSandbox branch %s preserved for debugging.",
					approvalResult.Output, rp.sandbox.GitBranch)
				if err := rp.store.AddComment(ctx, issue.ID, rp.actor, comment); err != nil {
					fmt.Fprintf(os.Stderr, "warning: failed to add approval rejection comment: %v\n", err)
				}

				// Mark issue as needs-review
				if err := rp.store.AddLabel(ctx, issue.ID, "needs-review", rp.actor); err != nil {
					fmt.Fprintf(os.Stderr, "warning: failed to add needs-review label: %v\n", err)
				}

				// Update to blocked status
				updates := map[string]interface{}{
					"status": string(types.StatusBlocked),
				}

				// Log status change for audit trail (vc-n4lx)
				rp.store.LogStatusChangeFromUpdates(ctx, issue.ID, updates, rp.actor,
					"human approval rejected - blocked for review")

				if err := rp.store.UpdateIssue(ctx, issue.ID, updates, rp.actor); err != nil {
					fmt.Fprintf(os.Stderr, "warning: failed to update issue to blocked: %v\n", err)
				}

				// Release the execution state
				if err := rp.releaseExecutionState(ctx, issue.ID); err != nil {
					return nil, fmt.Errorf("failed to release rejected issue: %w", err)
				}

				result.Summary = "Human approval rejected - issue blocked for review"
				return result, nil
			}
		}
	}

	// Step 3.5: Transition to committing state (vc-129)
	// After quality gates pass, always transition to committing state
	// This must happen before auto-commit to maintain valid state transitions
	if agentResult.Success && result.GatesPassed {
		if err := rp.store.UpdateExecutionState(ctx, issue.ID, types.ExecutionStateCommitting); err != nil {
			fmt.Fprintf(os.Stderr, "warning: failed to update execution state to committing: %v\n", err)
		}
	}

	// Step 3.6: Test Coverage Analysis (vc-217)
	// After quality gates pass, analyze test coverage and file test improvement issues
	if err := rp.handleTestCoverageAnalysis(ctx, issue, agentResult, result); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: test coverage analysis failed: %v\n", err)
	}

	// Step 3.7: Auto-commit and code review (if enabled, agent succeeded, and gates passed)
	if err := rp.handleAutoCommitAndCodeReview(ctx, issue, agentResult, result, gateResults); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: auto-commit or code review failed: %v\n", err)
	}

	// Step 4: Update issue status
	if agentResult.Success && result.GatesPassed {
		// Handle success path
		if err := rp.handleSuccessPath(ctx, issue, agentResult, analysis, gateResults, agentOutput, result); err != nil {
			return nil, err
		}
	} else {
		// Handle failure path
		if err := rp.handleFailurePath(ctx, issue, agentResult, agentOutput, result); err != nil {
			return nil, err
		}
	}

	return result, nil
}

// handleSuccessPath handles the success path when agent execution and quality gates both pass.
// This includes:
// - Checking for incomplete work and potentially retrying
// - Closing the issue with proper reason
// - Emitting baseline events if applicable
// - Adding completion comments
// - Creating discovered issues from analysis
// - Checking epic completion
// - Updating execution state
// - Releasing execution lock
func (rp *ResultsProcessor) handleSuccessPath(ctx context.Context, issue *types.Issue, agentResult *AgentResult, analysis *ai.Analysis, gateResults []*gates.Result, agentOutput string, result *ProcessingResult) error {
	// Determine if we should close the issue based on AI analysis
	shouldClose := true
	if analysis != nil && !analysis.Completed {
		shouldClose = false
		fmt.Printf("\nAI analysis indicates issue is not fully complete - handling incomplete work\n")

		// vc-1ows: Handle incomplete work (agent didn't complete the task)
		// Check execution history to determine retry strategy
		if err := rp.handleIncompleteWork(ctx, issue, analysis); err != nil {
			fmt.Fprintf(os.Stderr, "warning: failed to handle incomplete work: %v\n", err)
		}
	}

	result.Completed = shouldClose

	// Update issue status
	if shouldClose {
		// Build close reason from quality gates results
		closeReason := "Completed: agent succeeded, quality gates passed"
		if len(gateResults) > 0 {
			passedGates := []string{}
			for _, gr := range gateResults {
				if gr.Passed {
					passedGates = append(passedGates, string(gr.Gate))
				}
			}
			if len(passedGates) > 0 {
				closeReason = fmt.Sprintf("Completed: gates passed (%s)", strings.Join(passedGates, ", "))
			}
		}

		// vc-0d49: Use CloseIssue instead of UpdateIssue to properly set closed_at and close reason
		if err := rp.store.CloseIssue(ctx, issue.ID, closeReason, rp.actor); err != nil {
			fmt.Fprintf(os.Stderr, "warning: failed to close issue: %v\n", err)
		} else {
			fmt.Printf("\n✓ Issue %s closed: %s\n", issue.ID, closeReason)

			// vc-an5o: Record progress to reset watchdog backoff after successful completion
			if rp.watchdogConfig != nil {
				rp.watchdogConfig.RecordProgress()
				fmt.Printf("✓ Watchdog: Progress recorded, backoff reset to base interval\n")
			}

			// vc-230: Emit baseline_test_fix_completed event if this was a baseline issue
			// vc-261: Use IsBaselineIssue() and get fix_type from diagnosis (not string matching)
			if IsBaselineIssue(issue.ID) {
				// Extract gate type
				gateType := GetGateType(issue.ID)

				// Get diagnosis to extract fix type (vc-261: No more ZFC violation!)
				fixType := "unknown"
				diagnosis := rp.getDiagnosisFromComments(ctx, issue.ID)
				if diagnosis != nil {
					fixType = string(diagnosis.FailureType)
				}

				// Count tests fixed from gate results
				testsFixed := 0
				if len(gateResults) > 0 {
					// Count how many tests passed
					for _, gateResult := range gateResults {
						if gateResult.Passed && gateResult.Gate == gates.GateTest {
							testsFixed = 1 // At least one test passed
							break
						}
					}
				}

				// vc-261: Fix event data to match BaselineTestFixCompletedData struct
				rp.logEvent(ctx, events.EventTypeBaselineTestFixCompleted, events.SeverityInfo, issue.ID,
					fmt.Sprintf("Self-healing completed for baseline issue %s", issue.ID),
					map[string]interface{}{
						"baseline_issue_id":  issue.ID,
						"gate_type":          gateType,
						"success":            true,
						"fix_type":           fixType,
						"tests_fixed":        testsFixed,
						"commit_hash":        result.CommitHash,
						"processing_time_ms": agentResult.Duration.Milliseconds(),
					})

				fmt.Printf("✓ Baseline self-healing completed successfully\n")
			}
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

	// Step 7: Check if parent epic is now complete (and auto-cleanup mission sandbox if needed)
	if shouldClose {
		if err := checkEpicCompletion(ctx, rp.store, rp.supervisor, rp.sandboxManager, rp.actor, issue.ID); err != nil {
			fmt.Fprintf(os.Stderr, "warning: failed to check epic completion: %v\n", err)
		}
	}

	// Step 8: Check if code review sweep is needed (vc-1)
	// This runs after successful issue completion to check accumulated changes
	if rp.executor != nil {
		if err := rp.executor.checkCodeReviewSweep(ctx); err != nil {
			// Log warning but don't fail - review checks are non-critical
			fmt.Fprintf(os.Stderr, "warning: failed to check code review sweep: %v\n", err)
		}
	}

	// Update execution state to completed
	if err := rp.store.UpdateExecutionState(ctx, issue.ID, types.ExecutionStateCompleted); err != nil {
		fmt.Fprintf(os.Stderr, "warning: failed to update execution state: %v\n", err)
	}

	// Release the execution state
	if err := rp.releaseExecutionState(ctx, issue.ID); err != nil {
		return fmt.Errorf("failed to release issue: %w", err)
	}

	// Build summary
	result.Summary = rp.buildSummary(issue, agentResult, analysis, result)

	return nil
}

// handleFailurePath handles the failure path when agent execution or quality gates fail.
// This includes:
// - Adding error comment
// - Emitting baseline failure events if applicable
// - Releasing execution lock
func (rp *ResultsProcessor) handleFailurePath(ctx context.Context, issue *types.Issue, agentResult *AgentResult, agentOutput string, result *ProcessingResult) error {
	// Agent failed or gates failed
	fmt.Printf("\n✗ Agent execution failed (exit code: %d)\n", agentResult.ExitCode)

	errMsg := fmt.Sprintf("Agent failed with exit code %d\n\nLast output:\n%s",
		agentResult.ExitCode, agentOutput)

	// Add error comment
	if err := rp.store.AddComment(ctx, issue.ID, rp.actor, errMsg); err != nil {
		fmt.Fprintf(os.Stderr, "warning: failed to add error comment: %v\n", err)
	}

	// vc-230: Emit baseline_test_fix_completed event with success=false if this was a baseline issue
	// vc-261: Use IsBaselineIssue() helper and match event data to struct
	if IsBaselineIssue(issue.ID) {
		// Extract gate type
		gateType := GetGateType(issue.ID)

		// Extract failure reason
		failureReason := "unknown"
		if !agentResult.Success {
			failureReason = "agent_execution_failed"
		} else if !result.GatesPassed {
			failureReason = "quality_gates_failed"
		}

		// vc-261: Fix event data to match BaselineTestFixCompletedData struct
		rp.logEvent(ctx, events.EventTypeBaselineTestFixCompleted, events.SeverityError, issue.ID,
			fmt.Sprintf("Self-healing failed for baseline issue %s", issue.ID),
			map[string]interface{}{
				"baseline_issue_id":  issue.ID,
				"gate_type":          gateType,
				"success":            false,
				"error":              failureReason,
				"processing_time_ms": agentResult.Duration.Milliseconds(),
			})

		fmt.Printf("✗ Baseline self-healing failed\n")
	}

	// Leave issue in in_progress state but release execution lock
	if err := rp.releaseExecutionState(ctx, issue.ID); err != nil {
		return fmt.Errorf("failed to release issue: %w", err)
	}

	result.Summary = fmt.Sprintf("Agent execution failed with exit code %d", agentResult.ExitCode)

	return nil
}

// handleStructuredReportAndAnalysis handles structured agent reports and AI analysis.
// Returns (true, analysis) if caller should return early.
// Returns (false, analysis) if caller should continue.
func (rp *ResultsProcessor) handleStructuredReportAndAnalysis(ctx context.Context, issue *types.Issue, agentResult *AgentResult, agentOutput string, result *ProcessingResult) (bool, *ai.Analysis) {
	// Try to parse structured agent report (vc-257)
	fullOutput := strings.Join(agentResult.Output, "\n")
	agentReport, hasReport := ParseAgentReport(fullOutput)

	// Track whether the structured report was successfully handled (vc-138)
	reportHandled := false

	if hasReport {
		fmt.Printf("\n✓ Found structured agent report (status: %s)\n", agentReport.Status)

		reportHandler := NewAgentReportHandler(rp.store, rp.actor)
		completed, err := reportHandler.HandleReport(ctx, issue, agentReport)
		if err != nil {
			fmt.Fprintf(os.Stderr, "warning: failed to handle agent report: %v (falling back to AI analysis)\n", err)
			rp.logEvent(ctx, events.EventTypeError, events.SeverityWarning, issue.ID,
				fmt.Sprintf("Structured report handling failed: %v", err),
				map[string]interface{}{
					"report_status":  agentReport.Status,
					"report_summary": agentReport.Summary,
					"error":          err.Error(),
				})
		} else {
			reportHandled = true
			result.Completed = completed

			// For certain statuses, we can skip quality gates and AI analysis
			switch agentReport.Status {
			case AgentStatusBlocked:
				fmt.Printf("Issue blocked by agent - skipping quality gates\n")
				if err := rp.releaseExecutionState(ctx, issue.ID); err != nil {
					fmt.Fprintf(os.Stderr, "failed to release blocked issue: %v\n", err)
				}
				result.Summary = fmt.Sprintf("Agent blocked: %s", agentReport.Summary)
				result.GatesPassed = false
				return true, nil

			case AgentStatusDecomposed:
				fmt.Printf("Task decomposed into epic - executor will pick up children\n")
				if err := rp.releaseExecutionState(ctx, issue.ID); err != nil {
					fmt.Fprintf(os.Stderr, "failed to release decomposed issue: %v\n", err)
				}
				result.Summary = fmt.Sprintf("Task decomposed: %s", agentReport.Summary)
				result.Completed = false
				return true, nil

			case AgentStatusPartial:
				fmt.Printf("Partial completion - continuing to quality gates\n")

			case AgentStatusCompleted:
				fmt.Printf("Agent reports completion - continuing to quality gates\n")
			}
		}
	} else {
		fmt.Printf("\nℹ No structured agent report found - will use AI analysis\n")
	}

	// Step 2: AI Analysis (vc-138: skip if structured report was successfully handled)
	if err := rp.store.UpdateExecutionState(ctx, issue.ID, types.ExecutionStateAnalyzing); err != nil {
		fmt.Fprintf(os.Stderr, "warning: failed to update execution state: %v\n", err)
	}

	var analysis *ai.Analysis
	if reportHandled {
		fmt.Printf("Using structured agent report - skipping AI analysis\n")
	} else if rp.supervisor != nil {
		rp.logEvent(ctx, events.EventTypeAnalysisStarted, events.SeverityInfo, issue.ID,
			fmt.Sprintf("Starting AI analysis for issue %s", issue.ID),
			map[string]interface{}{})

		analysisStart := time.Now()
		var err error
		analysis, err = rp.supervisor.AnalyzeExecutionResult(ctx, issue, agentOutput, agentResult.Success)
		if rp.executor != nil && rp.executor.monitor != nil {
			rp.executor.monitor.RecordPhaseDuration("analyze", time.Since(analysisStart))
		}
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: AI analysis failed: %v (continuing without analysis)\n", err)
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

			if len(analysis.DiscoveredIssues) > 0 {
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
					if rp.executor != nil && rp.executor.monitor != nil {
						rp.executor.monitor.RecordDiscoveredIssues(len(createdIDs))
					}
				}
			}

			rp.logEvent(ctx, events.EventTypeAnalysisCompleted, events.SeverityInfo, issue.ID,
				fmt.Sprintf("AI analysis completed for issue %s", issue.ID),
				map[string]interface{}{
					"success":          true,
					"completed":        analysis.Completed,
					"discovered_count": len(analysis.DiscoveredIssues),
					"quality_issues":   len(analysis.QualityIssues),
				})
		}
	} else {
		fmt.Printf("AI supervision disabled - skipping analysis (state transition maintained)\n")
	}

	return false, analysis
}

// handleQualityGates runs quality gates if enabled and handles the results.
// Returns (true, gateResults) if caller should return early due to gate failure.
// Returns (false, gateResults) if caller should continue with remaining steps.
// The gateResults are returned so they can be used by later steps (e.g., human approval gate).
func (rp *ResultsProcessor) handleQualityGates(ctx context.Context, issue *types.Issue, agentResult *AgentResult, result *ProcessingResult) (bool, []*gates.Result) {
	var gateResults []*gates.Result

	if !agentResult.Success || !rp.enableQualityGates {
		// Quality gates skipped - log why
		var reason string
		if !agentResult.Success {
			reason = "agent execution failed"
			// Mark sandbox as failed when agent execution fails (vc-134)
			if rp.sandbox != nil {
				rp.sandbox.Status = sandbox.SandboxStatusFailed
			}
		} else {
			reason = "quality gates disabled"
		}
		rp.logEvent(ctx, events.EventTypeQualityGatesSkipped, events.SeverityInfo, issue.ID,
			fmt.Sprintf("Quality gates skipped for issue %s: %s", issue.ID, reason),
			map[string]interface{}{
				"reason": reason,
			})
		return false, gateResults
	}

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
		return false, gateResults
	}

	// Update execution state to gates
	if err := rp.store.UpdateExecutionState(ctx, issue.ID, types.ExecutionStateGates); err != nil {
		fmt.Fprintf(os.Stderr, "warning: failed to update execution state: %v\n", err)
	}

	// Log quality gates started
	gatesStartTime := time.Now()
	rp.logEvent(ctx, events.EventTypeQualityGatesStarted, events.SeverityInfo, issue.ID,
		fmt.Sprintf("Starting quality gates evaluation for issue %s", issue.ID),
		map[string]interface{}{})

	// vc-267: Create progress callback to emit progress events during gate execution
	progressCallback := func(currentGate gates.GateType, gatesCompleted, totalGates int, elapsedSeconds int64) {
		message := ""
		if currentGate != "" {
			message = fmt.Sprintf("Running %s gate (%d/%d completed, %ds elapsed)", currentGate, gatesCompleted, totalGates, elapsedSeconds)
		} else {
			// Heartbeat without specific gate (periodic update)
			message = fmt.Sprintf("Quality gates in progress (%d/%d completed, %ds elapsed)", gatesCompleted, totalGates, elapsedSeconds)
		}

		// vc-273: Use typed constructor for progress events
		progressData := events.QualityGatesProgressData{
			CurrentGate:    string(currentGate),
			GatesCompleted: gatesCompleted,
			TotalGates:     totalGates,
			ElapsedSeconds: elapsedSeconds,
			Message:        message,
		}
		rp.logProgressEvent(ctx, events.SeverityInfo, issue.ID, message, progressData)
	}

	gateRunner, err := gates.NewRunner(&gates.Config{
		Store:            rp.store,
		Supervisor:       rp.supervisor, // Enable AI-driven recovery strategies (ZFC)
		WorkingDir:       rp.workingDir,
		ProgressCallback: progressCallback, // vc-267: Progress reporting
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
		return false, gateResults
	}

	// Create timeout context for quality gates (vc-245, vc-xcfw)
	// Configurable timeout (default: 5 minutes) prevents indefinite hangs
	gateCtx, cancel := context.WithTimeout(ctx, rp.gatesTimeout)
	defer cancel()

	fmt.Printf("Running quality gates (timeout: %v)...\n", rp.gatesTimeout)

	// Run gates with timeout protection
	var allPassed bool
	gateResults, allPassed = gateRunner.RunAll(gateCtx)

	// Record gate results in telemetry
	// Note: gates.Result doesn't track individual gate duration, so we use 0
	// and rely on total gates duration from the overall timing
	if rp.executor != nil && rp.executor.monitor != nil {
		for _, gateResult := range gateResults {
			message := gateResult.Output
			if gateResult.Error != nil {
				message = gateResult.Error.Error()
			}
			rp.executor.monitor.RecordGateResult(
				string(gateResult.Gate),
				gateResult.Passed,
				0, // Duration not available from gates.Result
				message,
			)
		}
	}

	// Log progress for each gate (vc-245)
	for i, gateResult := range gateResults {
		status := "PASS"
		severity := events.SeverityInfo
		if !gateResult.Passed {
			status = "FAIL"
			severity = events.SeverityWarning
		}
		fmt.Printf("  %s: %s\n", gateResult.Gate, status)

		// vc-273: Emit progress event for each gate with typed data
		message := fmt.Sprintf("Quality gate %s: %s", gateResult.Gate, status)
		progressData := events.QualityGatesProgressData{
			CurrentGate:    string(gateResult.Gate),
			GatesCompleted: i + 1,
			TotalGates:     len(gateResults),
			ElapsedSeconds: int64(time.Since(gatesStartTime).Seconds()),
			Message:        message,
		}
		rp.logProgressEvent(ctx, severity, issue.ID, message, progressData)
	}

	// Check if we timed out or were canceled (vc-128)
	timedOut := gateCtx.Err() == context.DeadlineExceeded
	canceled := gateCtx.Err() == context.Canceled || ctx.Err() == context.Canceled

	if timedOut {
		fmt.Fprintf(os.Stderr, "Warning: quality gates timed out after 5 minutes\n")
		result.GatesPassed = false
		allPassed = false // Override allPassed on timeout
	} else if canceled {
		// Executor is shutting down - don't mark as failed, return issue to open
		fmt.Fprintf(os.Stderr, "Warning: quality gates canceled due to executor shutdown\n")
		result.GatesPassed = false
		allPassed = false // Don't pass gates on cancellation

		// vc-140: Log partial results before cleanup
		if len(gateResults) > 0 {
			comment := "**Quality Gates Canceled During Execution**\n\nPartial results:\n"
			for _, gateResult := range gateResults {
				status := "PASS"
				if !gateResult.Passed {
					status = "FAIL"
				}
				comment += fmt.Sprintf("- %s: %s\n", gateResult.Gate, status)
			}
			comment += "\nIssue returned to 'open' status for retry."

			if err := rp.store.AddComment(ctx, issue.ID, "quality-gates", comment); err != nil {
				fmt.Fprintf(os.Stderr, "warning: failed to add partial results comment: %v\n", err)
			}

			// Also log events for each completed gate (vc-140, vc-273)
			for i, gateResult := range gateResults {
				severity := events.SeverityInfo
				if !gateResult.Passed {
					severity = events.SeverityWarning
				}

				status := map[bool]string{true: "PASS", false: "FAIL"}[gateResult.Passed]
				message := fmt.Sprintf("Quality gate %s: %s (canceled)", gateResult.Gate, status)

				// vc-273: Use typed data for canceled progress events
				progressData := events.QualityGatesProgressData{
					CurrentGate:    string(gateResult.Gate),
					GatesCompleted: i + 1,
					TotalGates:     len(gateResults),
					ElapsedSeconds: 0, // Unknown for canceled gates
					Message:        message,
				}
				rp.logProgressEvent(ctx, severity, issue.ID, message, progressData)
			}
		}

		// Don't handle gate results - let the executor release the issue
	} else {
		result.GatesPassed = allPassed
	}

	// Handle gate results (creates blocking issues on failure)
	// Skip this on cancellation - let executor release issue back to open (vc-128)
	if !canceled {
		if err := gateRunner.HandleGateResults(ctx, issue, gateResults, allPassed); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to handle gate results: %v\n", err)
		}
	}

	// Build completion event data
	gateData := map[string]interface{}{
		"all_passed": allPassed,
		"gates_run":  len(gateResults),
		"timeout":    timedOut,
		"canceled":   canceled,
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
	} else if canceled {
		// Cancellation due to shutdown is not an error - it's expected (vc-128)
		severity = events.SeverityInfo
		message = fmt.Sprintf("Quality gates canceled for issue %s due to executor shutdown", issue.ID)
		gateData["error"] = "context canceled"
	} else if !allPassed {
		severity = events.SeverityWarning
	}

	// Always emit completion event (vc-245)
	rp.logEvent(ctx, events.EventTypeQualityGatesCompleted, severity, issue.ID, message, gateData)

	// Update sandbox status based on quality gate results (vc-134)
	if rp.sandbox != nil {
		if allPassed && !canceled && !timedOut {
			rp.sandbox.Status = sandbox.SandboxStatusCompleted
		} else {
			rp.sandbox.Status = sandbox.SandboxStatusFailed
		}
	}

	// vc-218: If this is a mission with needs-quality-gates label and gates passed,
	// transition to needs-review state (for future QA workers)
	if allPassed && !canceled && !timedOut && issue.IssueSubtype == types.SubtypeMission {
		hasLabel, err := labels.HasLabel(ctx, rp.store, issue.ID, labels.LabelNeedsQualityGates)
		if err != nil {
			fmt.Printf("Warning: failed to check for needs-quality-gates label: %v\n", err)
		} else if hasLabel {
			if err := labels.TransitionState(ctx, rp.store, issue.ID, labels.LabelNeedsQualityGates, labels.LabelNeedsReview, labels.TriggerGatesPassed, rp.actor); err != nil {
				fmt.Printf("Warning: failed to transition mission to needs-review: %v\n", err)
			} else {
				fmt.Printf("✓ Mission %s transitioned to needs-review state\n", issue.ID)
			}
		}
	}

	// Skip blocking logic if canceled - executor will release issue (vc-128)
	if !canceled && !allPassed {
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
			"status": string(types.StatusBlocked),
		}

		// Log status change for audit trail (vc-n4lx)
		rp.store.LogStatusChangeFromUpdates(ctx, issue.ID, updates, rp.actor,
			"quality gates failed after agent execution")

		if err := rp.store.UpdateIssue(ctx, issue.ID, updates, rp.actor); err != nil {
			fmt.Fprintf(os.Stderr, "warning: failed to update issue to blocked: %v\n", err)
		}

		// Release the execution state
		if err := rp.releaseExecutionState(ctx, issue.ID); err != nil {
			// Don't return error to maintain backward compatibility
			fmt.Fprintf(os.Stderr, "warning: failed to release blocked issue: %v\n", err)
		}

		result.Summary = "Quality gates failed - issue blocked"
		return true, gateResults // Return early - issue is blocked
	}

	return false, gateResults
}

// handleTestCoverageAnalysis runs test coverage analysis if preconditions are met (vc-217).
// Returns error if analysis fails, nil otherwise (failure is non-fatal).
func (rp *ResultsProcessor) handleTestCoverageAnalysis(ctx context.Context, issue *types.Issue, agentResult *AgentResult, result *ProcessingResult) error {
	if !agentResult.Success || !result.GatesPassed || rp.supervisor == nil || rp.gitOps == nil {
		return nil // Preconditions not met, skip silently
	}

	fmt.Printf("\n=== Test Coverage Analysis ===\n")

	// Check if there are uncommitted changes to analyze
	hasChanges, err := rp.gitOps.HasUncommittedChanges(ctx, rp.workingDir)
	if err != nil {
		return fmt.Errorf("failed to check for changes: %w", err)
	}

	if !hasChanges {
		fmt.Printf("No uncommitted changes - skipping test coverage analysis\n")
		return nil
	}

	// Get the diff of uncommitted changes
	diff, err := rp.getUncommittedDiff(ctx)
	if err != nil {
		return fmt.Errorf("failed to get diff: %w", err)
	}

	if diff == "" {
		return nil
	}

	// Get existing test files to understand test patterns
	existingTests, err := rp.getExistingTests(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to get existing tests: %v\n", err)
		// Continue with empty existing tests
	}

	// Analyze test coverage
	testAnalysis, err := rp.supervisor.AnalyzeTestCoverage(ctx, issue, diff, existingTests)
	if err != nil {
		return fmt.Errorf("AI analysis failed: %w", err)
	}

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
			return fmt.Errorf("failed to create test issues: %w", err)
		}
		result.DiscoveredIssues = append(result.DiscoveredIssues, createdTestIssues...)
		fmt.Printf("✓ Created %d test improvement issues: %v\n", len(createdTestIssues), createdTestIssues)
	} else {
		fmt.Printf("✓ No test coverage gaps found - coverage looks good\n")
	}

	return nil
}

// handleAutoCommitAndCodeReview handles auto-committing changes and performing code review analysis.
// This combines Step 3.7 (auto-commit) and Step 3.8 (code review decision) from the original flow.
// Returns error if critical steps fail, nil otherwise (most failures are non-fatal).
func (rp *ResultsProcessor) handleAutoCommitAndCodeReview(ctx context.Context, issue *types.Issue, agentResult *AgentResult, result *ProcessingResult, gateResults []*gates.Result) error {
	if !agentResult.Success || !result.GatesPassed || !rp.enableAutoCommit || rp.gitOps == nil || rp.messageGen == nil {
		return nil // Preconditions not met, skip silently
	}

	commitHash, err := rp.autoCommit(ctx, issue)
	if err != nil {
		// Don't fail - just log and continue
		fmt.Fprintf(os.Stderr, "Warning: auto-commit failed: %v (continuing without commit)\n", err)
		return nil
	}

	if commitHash == "" {
		return nil // No changes to commit
	}

	result.CommitHash = commitHash
	fmt.Printf("\n✓ Changes committed: %s\n", safeShortHash(commitHash))

	// Add comment with commit hash
	commitComment := fmt.Sprintf("Auto-committed changes: %s", commitHash)
	if err := rp.store.AddComment(ctx, issue.ID, rp.actor, commitComment); err != nil {
		fmt.Fprintf(os.Stderr, "warning: failed to add commit comment: %v\n", err)
	}

	// Auto-PR creation (if enabled) (vc-389e)
	if rp.enableAutoPR {
		prURL, err := rp.createAutoPR(ctx, issue, commitHash, gateResults)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: auto-PR failed: %v (continuing without PR)\n", err)
		} else if prURL != "" {
			prComment := fmt.Sprintf("Auto-created PR: %s", prURL)
			if err := rp.store.AddComment(ctx, issue.ID, rp.actor, prComment); err != nil {
				fmt.Fprintf(os.Stderr, "warning: failed to add PR comment: %v\n", err)
			}
		}
	}

	// AI-based code review decision and automated quality analysis (vc-216)
	if rp.supervisor != nil {
		if err := rp.handleCodeReviewDecision(ctx, issue, commitHash, result); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: code review decision failed: %v\n", err)
		}
	}

	return nil
}

// handleCodeReviewDecision performs AI-based code review decision and quality analysis (vc-216).
// Returns error if critical steps fail, nil otherwise (most failures are non-fatal).
func (rp *ResultsProcessor) handleCodeReviewDecision(ctx context.Context, issue *types.Issue, commitHash string, result *ProcessingResult) error {
	fmt.Printf("\n=== Code Review Decision ===\n")

	// Check for context cancellation before expensive operations (vc-25e5)
	if ctx.Err() != nil {
		return ctx.Err()
	}

	// Get the diff for this commit using git directly
	diff, err := rp.getCommitDiff(ctx, commitHash)
	if err != nil {
		return fmt.Errorf("failed to get diff: %w", err)
	}

	// Use Haiku to decide if review is needed (fast and cheap)
	decision, err := rp.supervisor.AnalyzeCodeReviewNeed(ctx, issue, diff)
	if err != nil {
		return fmt.Errorf("AI decision failed: %w", err)
	}

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
	if needsReview {
		if decision.NeedsReview {
			fmt.Printf("Code review recommended (confidence: %.0f%%)\n", decision.Confidence*100)
		}

		// Check for context cancellation before AI quality analysis (vc-25e5)
		if ctx.Err() != nil {
			return ctx.Err()
		}

		fmt.Printf("\n=== Automated Code Quality Analysis ===\n")
		qualityAnalysis, err := rp.supervisor.AnalyzeCodeQuality(ctx, issue, diff)
		if err != nil {
			// AI quality analysis failed - log error and document for human review
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
			return nil // Don't fail on quality analysis error
		}

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
	} else {
		fmt.Printf("No code review needed (confidence: %.0f%%)\n", decision.Confidence*100)
	}

	return nil
}

// handleMissionGateDelegation checks if this is a mission and delegates quality gates to QA workers (vc-251).
// Returns (true, result) if delegation occurred and caller should return early.
// Returns (false, nil) if this is not a mission or delegation is not needed.
func (rp *ResultsProcessor) handleMissionGateDelegation(ctx context.Context, issue *types.Issue, agentResult *AgentResult, result *ProcessingResult) (bool, *ProcessingResult) {
	if !agentResult.Success || !rp.enableQualityGates ||
		issue.IssueType != types.TypeEpic || issue.IssueSubtype != types.SubtypeMission {
		return false, nil
	}

	fmt.Printf("\n=== Mission Quality Gates Delegation ===\n")
	fmt.Printf("Mission detected - deferring quality gates to QA worker\n")

	// Add needs-quality-gates label to trigger QA worker
	if err := rp.store.AddLabel(ctx, issue.ID, labels.LabelNeedsQualityGates, rp.actor); err != nil {
		fmt.Fprintf(os.Stderr, "warning: failed to add needs-quality-gates label: %v\n", err)
	} else {
		fmt.Printf("✓ Added 'needs-quality-gates' label for QA worker\n")
	}

	// Emit deferred event for observability
	rp.logEvent(ctx, events.EventTypeQualityGatesDeferred, events.SeverityInfo, issue.ID,
		fmt.Sprintf("Quality gates deferred to QA worker for mission %s", issue.ID),
		map[string]interface{}{
			"mission_id": issue.ID,
			"reason":     "delegated-to-qa-worker",
		})

	// Release the execution state
	if err := rp.releaseExecutionState(ctx, issue.ID); err != nil {
		// Return error as a result with error message
		errorResult := &ProcessingResult{
			Completed:        false,
			DiscoveredIssues: []string{},
			GatesPassed:      false,
			Summary:          fmt.Sprintf("Failed to release mission execution state: %v", err),
		}
		return true, errorResult
	}

	// Build result and return early (skip inline gates)
	result.Completed = false // Mission stays open until all tasks complete
	result.GatesPassed = true // Not failed, just deferred
	result.Summary = "Mission execution complete - quality gates deferred to QA worker"
	return true, result
}

// extractSummary extracts a summary from agent output using AI.
// When AI supervisor is not available, returns a simple data summary (not a heuristic).
func (rp *ResultsProcessor) extractSummary(ctx context.Context, issue *types.Issue, result *AgentResult) string {
	if len(result.Output) == 0 {
		return "Agent completed with no output"
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
		return basicSummary
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
		return basicSummary
	}

	return summary
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

	// Add phase duration breakdown if available (vc-b5db)
	if rp.executor != nil && rp.executor.monitor != nil {
		telemetry := rp.executor.monitor.GetCurrentExecution()
		if telemetry != nil && len(telemetry.PhaseDurations) > 0 {
			summary.WriteString("\nPhase Breakdown:\n")
			// Show phases in logical order
			phaseOrder := []string{"assess", "execute", "analyze", "gates"}
			for _, phase := range phaseOrder {
				if duration, ok := telemetry.PhaseDurations[phase]; ok {
					summary.WriteString(fmt.Sprintf("  %s: %v\n", phase, duration.Round(time.Millisecond)))
				}
			}
		}
	}

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

	// Add quality gate details if available (vc-b5db)
	if rp.executor != nil && rp.executor.monitor != nil {
		telemetry := rp.executor.monitor.GetCurrentExecution()
		if telemetry != nil && len(telemetry.GateResults) > 0 {
			summary.WriteString("\nQuality Gates:\n")
			for _, gateResult := range telemetry.GateResults {
				status := "✓ PASS"
				if !gateResult.Passed {
					status = "✗ FAIL"
				}
				// Don't show duration since gates.Result doesn't provide it
				summary.WriteString(fmt.Sprintf("  %s %s\n", status, gateResult.Name))
				if !gateResult.Passed && gateResult.Message != "" {
					// Show first line of error message
					firstLine := strings.Split(gateResult.Message, "\n")[0]
					if len(firstLine) > 80 {
						firstLine = firstLine[:77] + "..."
					}
					summary.WriteString(fmt.Sprintf("    %s\n", firstLine))
				}
			}
		}
	}

	if !procResult.GatesPassed {
		summary.WriteString("\n✗ Quality gates failed - issue blocked\n")
	}

	if procResult.CommitHash != "" {
		summary.WriteString(fmt.Sprintf("\n✓ Auto-committed: %s\n", safeShortHash(procResult.CommitHash)))
	}

	return summary.String()
}

// getDiagnosisFromComments extracts the test failure diagnosis from issue comments (vc-261).
// The diagnosis is stored as a JSON comment in the format: <!--VC-DIAGNOSIS:{json}-->
// Returns nil if no diagnosis is found.
func (rp *ResultsProcessor) getDiagnosisFromComments(ctx context.Context, issueID string) *ai.TestFailureDiagnosis {
	// Get all events for the issue (comments are stored as events)
	events, err := rp.store.GetEvents(ctx, issueID, 0)
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: failed to get events for diagnosis extraction: %v\n", err)
		return nil
	}

	// Look for the diagnosis JSON comment
	const diagnosisPrefix = "<!--VC-DIAGNOSIS:"
	const diagnosisSuffix = "-->"
	for _, event := range events {
		if event.Comment != nil {
			commentText := *event.Comment
			if strings.HasPrefix(commentText, diagnosisPrefix) && strings.HasSuffix(commentText, diagnosisSuffix) {
				// Extract JSON from comment
				jsonStr := strings.TrimPrefix(commentText, diagnosisPrefix)
				jsonStr = strings.TrimSuffix(jsonStr, diagnosisSuffix)

				// Parse JSON
				var diagnosis ai.TestFailureDiagnosis
				if err := json.Unmarshal([]byte(jsonStr), &diagnosis); err != nil {
					fmt.Fprintf(os.Stderr, "warning: failed to parse diagnosis JSON: %v\n", err)
					return nil
				}

				return &diagnosis
			}
		}
	}

	return nil
}

// logEvent creates and stores an agent event for observability
func (rp *ResultsProcessor) logEvent(ctx context.Context, eventType events.EventType, severity events.EventSeverity, issueID, message string, data map[string]interface{}) {
	// Skip logging if context is canceled (e.g., during shutdown)
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

// logProgressEvent creates and stores a quality gates progress event with type-safe data (vc-273)
func (rp *ResultsProcessor) logProgressEvent(ctx context.Context, severity events.EventSeverity, issueID, message string, data events.QualityGatesProgressData) {
	// Skip logging if context is canceled (e.g., during shutdown)
	if ctx.Err() != nil {
		return
	}

	event, err := events.NewQualityGatesProgressEvent(issueID, rp.actor, "", severity, message, data)
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: failed to create quality gates progress event: %v\n", err)
		return
	}

	if err := rp.store.StoreAgentEvent(ctx, event); err != nil {
		// Log error but don't fail execution
		fmt.Fprintf(os.Stderr, "warning: failed to store quality gates progress event: %v\n", err)
	}
}

// handleIncompleteWork handles the case when AI analysis reports the task is incomplete (vc-1ows).
// This can happen when the agent reads files but doesn't make required edits, or only partially
// completes the work. The function checks execution history to determine retry strategy:
// - If attempts < maxIncompleteRetries: Leave as open for retry with enhanced context
// - If attempts >= maxIncompleteRetries: Escalate with needs-human-review label
func (rp *ResultsProcessor) handleIncompleteWork(ctx context.Context, issue *types.Issue, analysis *ai.Analysis) error {
	const maxIncompleteRetries = 1 // Allow 1 retry before escalation

	// Count how many times this issue has been attempted with incomplete results (vc-rd1z)
	// An incomplete attempt is one where:
	// - Agent succeeded (exit code 0)
	// - But AI analysis reported completed=false
	// - This function added an "Incomplete Work Detected" comment
	//
	// We count by looking for our own "Incomplete Work Detected" comments in the event history,
	// which is more reliable than trying to infer from execution attempts (which don't track
	// whether the issue was closed after each attempt).
	issueEvents, err := rp.store.GetEvents(ctx, issue.ID, 0)
	if err != nil {
		return fmt.Errorf("failed to get events for incomplete attempt counting: %w", err)
	}

	incompleteAttempts := 0
	const incompleteMarker = "**Incomplete Work Detected"
	for _, event := range issueEvents {
		if event.Comment != nil && strings.Contains(*event.Comment, incompleteMarker) {
			incompleteAttempts++
		}
	}

	// Add 1 for the current incomplete attempt (we haven't added the comment yet)
	incompleteAttempts++

	fmt.Printf("\nIncomplete work detected: attempt #%d (max retries: %d)\n", incompleteAttempts, maxIncompleteRetries)

	if incompleteAttempts <= maxIncompleteRetries {
		// Still within retry limit - add comment and leave as open for retry
		retryComment := fmt.Sprintf(`**Incomplete Work Detected (Attempt #%d)**

The AI supervisor determined that this task was not fully completed in the previous execution.

**Analysis Summary**: %s

**What was found**:
- Agent execution succeeded (no errors)
- Quality gates passed
- However, the work does not meet all acceptance criteria

**Next Steps**:
The executor will retry this task. The next agent will receive enhanced instructions emphasizing that code changes must be made to complete the acceptance criteria.

**Acceptance Criteria** (must ALL be met):
%s

If this issue continues to have incomplete attempts after %d total tries, it will be escalated for human review.`,
			incompleteAttempts, analysis.Summary, issue.AcceptanceCriteria, maxIncompleteRetries+1)

		if err := rp.store.AddComment(ctx, issue.ID, "ai-supervisor", retryComment); err != nil {
			return fmt.Errorf("failed to add retry comment: %w", err)
		}

		fmt.Printf("✓ Added retry comment - issue will be retried with enhanced prompt\n")

		// Emit event for observability
		rp.logEvent(ctx, events.EventTypeProgress, events.SeverityWarning, issue.ID,
			fmt.Sprintf("Incomplete work detected - scheduling retry (attempt #%d)", incompleteAttempts),
			map[string]interface{}{
				"incomplete_attempts": incompleteAttempts,
				"max_retries":         maxIncompleteRetries,
				"analysis_summary":    analysis.Summary,
			})

		// Release execution state so the executor can pick up the issue for retry
		if err := rp.releaseExecutionState(ctx, issue.ID); err != nil {
			return fmt.Errorf("failed to release execution state for retry: %w", err)
		}

	} else {
		// Exceeded retry limit - escalate with needs-human-review label
		escalationComment := fmt.Sprintf(`**Incomplete Work Escalated**

This task has been attempted %d times but the agent has not been able to fully complete it.

**Analysis from latest attempt**: %s

**Pattern Detected**:
The agent appears to be reading files and analyzing the code, but not making the necessary code modifications to complete the task.

**Acceptance Criteria** (not all met):
%s

**Action Required**:
This issue needs human intervention to either:
1. Break down the task into smaller, more focused subtasks
2. Clarify the acceptance criteria
3. Complete the work manually

The issue has been marked with the 'needs-human-review' label to prevent further automatic retries.`,
			incompleteAttempts, analysis.Summary, issue.AcceptanceCriteria)

		if err := rp.store.AddComment(ctx, issue.ID, "ai-supervisor", escalationComment); err != nil {
			return fmt.Errorf("failed to add escalation comment: %w", err)
		}

		// Add needs-human-review label
		if err := rp.store.AddLabel(ctx, issue.ID, "needs-human-review", rp.actor); err != nil {
			return fmt.Errorf("failed to add needs-human-review label: %w", err)
		}

		// Update to blocked status
		updates := map[string]interface{}{
			"status": string(types.StatusBlocked),
		}

		// Log status change for audit trail
		rp.store.LogStatusChangeFromUpdates(ctx, issue.ID, updates, rp.actor,
			"incomplete work escalated after multiple attempts")

		if err := rp.store.UpdateIssue(ctx, issue.ID, updates, rp.actor); err != nil {
			return fmt.Errorf("failed to update issue to blocked: %w", err)
		}

		fmt.Printf("🚨 Incomplete work escalated - marked as blocked with needs-human-review label\n")

		// Emit escalation event
		rp.logEvent(ctx, events.EventTypeProgress, events.SeverityError, issue.ID,
			fmt.Sprintf("Incomplete work escalated after %d attempts", incompleteAttempts),
			map[string]interface{}{
				"incomplete_attempts": incompleteAttempts,
				"max_retries":         maxIncompleteRetries,
				"analysis_summary":    analysis.Summary,
				"escalated":           true,
			})

		// Release execution state - issue is now blocked and needs human review
		if err := rp.releaseExecutionState(ctx, issue.ID); err != nil {
			return fmt.Errorf("failed to release execution state after escalation: %w", err)
		}
	}

	return nil
}
