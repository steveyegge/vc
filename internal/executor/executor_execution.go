package executor

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/google/uuid"
	"github.com/steveyegge/vc/internal/ai"
	"github.com/steveyegge/vc/internal/events"
	"github.com/steveyegge/vc/internal/iterative"
	"github.com/steveyegge/vc/internal/sandbox"
	"github.com/steveyegge/vc/internal/types"
)

// executeIssue executes a single issue by spawning a coding agent
func (e *Executor) executeIssue(ctx context.Context, issue *types.Issue) error {
	fmt.Printf("Executing issue %s: %s\n", issue.ID, issue.Title)

	// Check if bootstrap mode should be activated (vc-b027)
	bootstrapMode, bootstrapReason := e.ShouldUseBootstrapMode(ctx, issue)
	if bootstrapMode {
		e.logBootstrapModeActivation(ctx, issue, bootstrapReason)
	}

	// Start telemetry collection for this execution
	e.getMonitor().StartExecution(issue.ID, e.instanceID)

	// Log issue claimed event
	e.logEvent(ctx, events.EventTypeIssueClaimed, events.SeverityInfo, issue.ID,
		fmt.Sprintf("Issue %s claimed by executor %s", issue.ID, e.instanceID),
		map[string]interface{}{
			"issue_title": issue.Title,
		})
	e.getMonitor().RecordEvent(string(events.EventTypeIssueClaimed))

	// Initialize execution state to claimed (vc-efad)
	// This must happen before any state transitions to maintain state machine integrity
	if err := e.store.UpdateExecutionState(ctx, issue.ID, types.ExecutionStateClaimed); err != nil {
		// Check if context was canceled (shutdown initiated)
		if ctx.Err() != nil {
			// Use background context for cleanup since main context is canceled
			cleanupCtx := context.Background()
			e.releaseIssueWithError(cleanupCtx, issue.ID, fmt.Sprintf("Execution canceled during state initialization: %v", ctx.Err()))
			e.getMonitor().EndExecution(false, false)
			return ctx.Err()
		}
		fmt.Fprintf(os.Stderr, "warning: failed to initialize execution state: %v\n", err)
	}

	// Phase 1: AI Assessment (if enabled)
	// Always transition to assessing state for state machine consistency (vc-110)
	if err := e.store.UpdateExecutionState(ctx, issue.ID, types.ExecutionStateAssessing); err != nil {
		// Check if context was canceled (shutdown initiated)
		if ctx.Err() != nil {
			// Use background context for cleanup since main context is canceled
			cleanupCtx := context.Background()
			e.releaseIssueWithError(cleanupCtx, issue.ID, fmt.Sprintf("Execution canceled during state transition: %v", ctx.Err()))
			e.getMonitor().EndExecution(false, false)
			return ctx.Err()
		}
		fmt.Fprintf(os.Stderr, "warning: failed to update execution state: %v\n", err)
	}
	e.getMonitor().RecordStateTransition(types.ExecutionStateClaimed, types.ExecutionStateAssessing)

	// Pre-flight health check: verify AI supervisor is healthy before proceeding (vc-182)
	if e.enableAISupervision && e.supervisor != nil {
		if err := e.supervisor.HealthCheck(ctx); err != nil {
			// Circuit breaker is open or API is unhealthy - fail fast
			fmt.Fprintf(os.Stderr, "AI supervisor health check failed: %v\n", err)
			e.logEvent(ctx, events.EventTypeAssessmentCompleted, events.SeverityError, issue.ID,
				fmt.Sprintf("AI supervisor health check failed: %v", err),
				map[string]interface{}{
					"success": false,
					"error":   err.Error(),
				})
			// Use background context for cleanup since we're failing execution
			cleanupCtx := context.Background()
			e.releaseIssueWithError(cleanupCtx, issue.ID, fmt.Sprintf("AI supervisor unavailable: %v", err))
			e.getMonitor().EndExecution(false, false)
			return fmt.Errorf("cannot execute issue: AI supervisor health check failed: %w", err)
		}
	}

	var assessment *ai.Assessment
	// vc-b027: Skip AI assessment in bootstrap mode
	if bootstrapMode {
		fmt.Printf("Skipping AI assessment (bootstrap mode active)\n")
	} else if e.enableAISupervision && e.supervisor != nil {
		// Log assessment started
		e.logEvent(ctx, events.EventTypeAssessmentStarted, events.SeverityInfo, issue.ID,
			fmt.Sprintf("Starting AI assessment for issue %s", issue.ID),
			map[string]interface{}{})

		// Track assessment phase duration
		assessStart := time.Now()
		var err error

		// vc-43kd: Use iterative refinement if enabled for complex/high-risk issues
		if e.enableIterativeRefinement {
			// Create metrics collector for assessment refinement (vc-it8m)
			collector := iterative.NewInMemoryMetricsCollector()

			var refinementResult *iterative.ConvergenceResult
			assessment, refinementResult, err = e.supervisor.AssessIssueStateWithRefinement(ctx, issue, collector)

			e.getMonitor().RecordPhaseDuration("assess", time.Since(assessStart))

			// Log refinement metrics if successful
			if err == nil && refinementResult != nil {
				// Log convergence event (vc-43kd: activity feed events)
				e.logEvent(ctx, events.EventTypeProgress, events.SeverityInfo, issue.ID,
					fmt.Sprintf("Assessment refinement: iterations=%d, converged=%v, duration=%v",
						refinementResult.Iterations, refinementResult.Converged, refinementResult.ElapsedTime),
					map[string]interface{}{
						"phase":      "assessment",
						"iterations": refinementResult.Iterations,
						"converged":  refinementResult.Converged,
						"duration":   refinementResult.ElapsedTime.String(),
					})

				// Emit metrics summary (vc-it8m)
				fmt.Printf("📊 Assessment refinement metrics: iterations=%d, duration=%v, converged=%v\n",
					refinementResult.Iterations, refinementResult.ElapsedTime, refinementResult.Converged)
			}
		} else {
			// Fall back to single-pass assessment
			assessment, err = e.supervisor.AssessIssueState(ctx, issue)
			e.getMonitor().RecordPhaseDuration("assess", time.Since(assessStart))
		}
		if err != nil {
			// Check if context was canceled (shutdown initiated)
			if ctx.Err() != nil {
				fmt.Fprintf(os.Stderr, "Assessment canceled due to executor shutdown\n")
				// Use background context for cleanup since main context is canceled
				cleanupCtx := context.Background()
				e.releaseIssueWithError(cleanupCtx, issue.ID, "Execution canceled during assessment")
				e.getMonitor().EndExecution(false, false)
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

			// vc-rzqe: Check if AI recommends decomposition
			if assessment.ShouldDecompose && assessment.DecompositionPlan != nil {
				fmt.Printf("🔄 AI recommends decomposing %s into child issues\n", issue.ID)

				// Decompose the issue into children
				childIDs, err := e.supervisor.DecomposeIssue(ctx, e.store, issue, assessment.DecompositionPlan)
				if err != nil {
					fmt.Fprintf(os.Stderr, "warning: failed to decompose issue: %v (continuing with execution)\n", err)
				} else {
					fmt.Printf("✓ Successfully decomposed %s into %d child issues: %v\n", issue.ID, len(childIDs), childIDs)

					// Release the parent issue since children will be worked on instead
					comment := fmt.Sprintf("Decomposed into %d child issues: %v", len(childIDs), childIDs)
					if err := e.store.ReleaseIssueAndReopen(ctx, issue.ID, "ai-supervisor", comment); err != nil {
						fmt.Fprintf(os.Stderr, "warning: failed to release decomposed parent: %v\n", err)
					}

					// Log decomposition event
					e.logEvent(ctx, events.EventTypeIssueDecomposed, events.SeverityInfo, issue.ID,
						fmt.Sprintf("Decomposed %s into %d child issues", issue.ID, len(childIDs)),
						map[string]interface{}{
							"child_ids":       childIDs,
							"child_count":     len(childIDs),
							"reasoning":       assessment.DecompositionPlan.Reasoning,
						})

					// End execution metrics - issue was decomposed (not failed)
					e.getMonitor().EndExecution(false, false)

					// Return nil to indicate success (no error, but no agent spawned either)
					return nil
				}
			}
		}
	} else {
		// AI supervision disabled - assessing state is a no-op
		fmt.Printf("Skipping AI assessment (supervision disabled)\n")
	}

	// Phase 2: Get or create mission sandbox if enabled
	var sb *sandbox.Sandbox
	workingDir := e.workingDir
	if e.enableSandboxes && e.sandboxMgr != nil {
		// Look up the mission for this task (vc-244)
		missionCtx, err := e.store.GetMissionForTask(ctx, issue.ID)
		if err != nil {
			// Don't fail execution - just log and continue without sandbox
			fmt.Fprintf(os.Stderr, "Warning: failed to get mission for task %s: %v (continuing in main workspace)\n", issue.ID, err)
		} else if missionCtx != nil {
			// Task is part of a mission - use mission sandbox
			fmt.Printf("Task %s is part of mission %s\n", issue.ID, missionCtx.MissionID)

			// Get or create mission sandbox
			sb, err = sandbox.GetMissionSandbox(ctx, e.sandboxMgr, e.store, missionCtx.MissionID)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Warning: failed to get mission sandbox: %v (continuing in main workspace)\n", err)
			} else if sb == nil {
				// No sandbox exists yet - create it (auto-create on first task)
				fmt.Printf("Creating mission sandbox for %s...\n", missionCtx.MissionID)
				sb, err = sandbox.CreateMissionSandbox(ctx, e.sandboxMgr, e.store, missionCtx.MissionID)
				if err != nil {
					fmt.Fprintf(os.Stderr, "Warning: failed to create mission sandbox: %v (continuing in main workspace)\n", err)
					sb = nil // Clear to continue without sandbox
				} else {
					fmt.Printf("Mission sandbox created: %s (branch: %s)\n", sb.Path, sb.GitBranch)
				}
			} else {
				fmt.Printf("Using existing mission sandbox: %s (branch: %s)\n", sb.Path, sb.GitBranch)
			}

			// If we have a sandbox, set working directory
			if sb != nil {
				workingDir = sb.Path
				// NOTE: Do NOT cleanup mission sandbox here - it's shared across all tasks in the mission
				// Cleanup happens when the mission is closed (vc-245)
			}
		} else {
			// Task is not part of a mission - create per-execution sandbox (legacy behavior)
			fmt.Printf("Task %s is not part of a mission, creating per-execution sandbox...\n", issue.ID)

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

			sb, err = e.sandboxMgr.Create(ctx, sandboxCfg)
			if err != nil {
				// Don't fail execution - just log and continue without sandbox
				fmt.Fprintf(os.Stderr, "Warning: failed to create per-execution sandbox: %v (continuing in main workspace)\n", err)
			} else {
				// Set working directory to sandbox path
				workingDir = sb.Path
				fmt.Printf("Per-execution sandbox created: %s (branch: %s)\n", sb.Path, sb.GitBranch)

				// Ensure cleanup happens for per-execution sandboxes
				defer func() {
					if sb != nil {
						fmt.Printf("Cleaning up per-execution sandbox %s...\n", sb.ID)
						if err := e.sandboxMgr.Cleanup(ctx, sb); err != nil {
							fmt.Fprintf(os.Stderr, "warning: failed to cleanup sandbox: %v\n", err)
						}
					}
				}()
			}
		}
	}

	// Phase 2.5: Diagnose baseline test failures (vc-230)
	// If this is a baseline test issue, use AI to diagnose the failure
	// vc-261: Use IsBaselineIssue() helper instead of duplicated map
	if IsBaselineIssue(issue.ID) && e.enableAISupervision && e.supervisor != nil {
		// Extract test output from issue description
		// The description should contain the gate output
		testOutput := issue.Description

		fmt.Printf("Diagnosing baseline test failure for %s...\n", issue.ID)

		diagnosis, err := e.supervisor.DiagnoseTestFailure(ctx, issue, testOutput)
		if err != nil {
			// Log warning but continue - diagnosis is optional
			fmt.Fprintf(os.Stderr, "Warning: failed to diagnose test failure: %v (continuing without diagnosis)\n", err)

			// Emit diagnosis failure event
			e.logEvent(ctx, events.EventTypeTestFailureDiagnosis, events.SeverityWarning, issue.ID,
				fmt.Sprintf("Test failure diagnosis failed: %v", err),
				map[string]interface{}{
					"success": false,
					"error":   err.Error(),
				})
		} else {
			// vc-261: Fix event data to match BaselineTestFixStartedData struct
			gateType := GetGateType(issue.ID) // "test", "lint", or "build"
			e.logEvent(ctx, events.EventTypeBaselineTestFixStarted, events.SeverityInfo, issue.ID,
				fmt.Sprintf("Starting self-healing for baseline issue %s", issue.ID),
				map[string]interface{}{
					"baseline_issue_id": issue.ID,
					"gate_type":         gateType,
					"failing_tests":     diagnosis.TestNames,
				})

			// Add diagnosis as a comment for visibility
			diagnosisComment := fmt.Sprintf("**AI Test Failure Diagnosis**\n\n"+
				"Failure Type: %s\n"+
				"Confidence: %.0f%%\n\n"+
				"Root Cause:\n%s\n\n"+
				"Proposed Fix:\n%s\n\n"+
				"Verification Steps:\n",
				diagnosis.FailureType, diagnosis.Confidence*100,
				diagnosis.RootCause, diagnosis.ProposedFix)
			for i, step := range diagnosis.Verification {
				diagnosisComment += fmt.Sprintf("%d. %s\n", i+1, step)
			}

			if err := e.store.AddComment(ctx, issue.ID, "ai-supervisor", diagnosisComment); err != nil {
				fmt.Fprintf(os.Stderr, "warning: failed to add diagnosis comment: %v\n", err)
			}

			// vc-9aa9: Store diagnosis in dedicated table (replaces HTML comment approach)
			if err := e.store.StoreDiagnosis(ctx, issue.ID, diagnosis); err != nil {
				fmt.Fprintf(os.Stderr, "warning: failed to store diagnosis: %v\n", err)
			}

			fmt.Printf("Diagnosis complete: %s failure with %.0f%% confidence\n",
				diagnosis.FailureType, diagnosis.Confidence*100)
		}
	}

	// Phase 3: Spawn the coding agent
	// Check if context was canceled before starting execution (vc-101)
	if ctx.Err() != nil {
		fmt.Fprintf(os.Stderr, "Execution canceled before spawning agent\n")
		// Use background context for cleanup since main context is canceled
		cleanupCtx := context.Background()
		e.releaseIssueWithError(cleanupCtx, issue.ID, "Execution canceled before spawning agent")
		e.getMonitor().EndExecution(false, false)
		return ctx.Err()
	}

	// Update execution state to executing
	if err := e.store.UpdateExecutionState(ctx, issue.ID, types.ExecutionStateExecuting); err != nil {
		// Check if context was canceled (shutdown initiated)
		if ctx.Err() != nil {
			// Use background context for cleanup since main context is canceled
			cleanupCtx := context.Background()
			e.releaseIssueWithError(cleanupCtx, issue.ID, fmt.Sprintf("Execution canceled during state transition: %v", ctx.Err()))
			e.getMonitor().EndExecution(false, false)
			return ctx.Err()
		}
		fmt.Fprintf(os.Stderr, "warning: failed to update execution state: %v\n", err)
	}
	// Always transition from assessing→executing (vc-110)
	e.getMonitor().RecordStateTransition(types.ExecutionStateAssessing, types.ExecutionStateExecuting)

	// Create a cancelable context for the agent so watchdog can intervene
	agentCtx, agentCancel := context.WithCancel(ctx)
	defer func() {
		fmt.Printf("[DEBUG vc-177] agentCancel called for issue %s\n", issue.ID)
		agentCancel() // Always cancel when we're done
	}()

	// Register agent context with intervention controller for watchdog (vc-mq3c)
	if e.watchdog != nil {
		e.watchdog.SetAgentContext(issue.ID, agentCancel)
		defer e.watchdog.ClearAgentContext()
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
		e.getMonitor().EndExecution(false, false)
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
		e.getMonitor().EndExecution(false, false)
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
		e.getMonitor().EndExecution(false, false)
		return fmt.Errorf("failed to build prompt: %w", err)
	}

	// Log prompt for debugging if VC_DEBUG_PROMPTS is set
	if os.Getenv("VC_DEBUG_PROMPTS") != "" {
		fmt.Fprintf(os.Stderr, "\n=== AGENT PROMPT ===\n%s\n=== END PROMPT ===\n\n", prompt)
	}

	// Generate a unique agent ID for this execution
	agentID := uuid.New().String()

	agentCfg := AgentConfig{
		Type:       AgentTypeClaudeCode, // Use Claude Code as primary agent worker (vc-q788)
		WorkingDir: workingDir,
		Issue:      issue,
		StreamJSON: true, // Enable --output-format stream-json for structured events (vc-q788)
		Timeout:    30 * time.Minute,
		// Enable event parsing and storage
		Store:      e.store,
		ExecutorID: e.instanceID,
		AgentID:    agentID,
		Monitor:    e.getMonitor(), // Pass monitor for watchdog visibility (vc-118)
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
		e.getMonitor().EndExecution(false, false)
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
	// Track execution phase duration
	execStart := time.Now()
	result, err := agent.Wait(agentCtx)
	e.getMonitor().RecordPhaseDuration("execute", time.Since(execStart))
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
		e.getMonitor().EndExecution(false, false)
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
		EnableAutoPR:       e.config.EnableAutoPR,     // Auto-PR configuration (vc-389e)
		WorkingDir:         workingDir,                // Use sandbox path if sandboxing is enabled (vc-117)
		Actor:              e.instanceID,
		Sandbox:            sb,           // Pass sandbox for status tracking (vc-134)
		SandboxManager:     e.sandboxMgr, // Pass manager for auto-cleanup (vc-245)
		Executor:           e,            // Pass executor reference for code review checks (vc-1)
		WatchdogConfig:     e.watchdogConfig, // Watchdog config for backoff reset (vc-an5o)
		GatesTimeout:       e.config.GatesTimeout, // Quality gates timeout (vc-xcfw)
		MaxIncompleteRetries: e.config.MaxIncompleteRetries, // Max incomplete retries (vc-hsfz)
		BootstrapMode:        bootstrapMode, // Bootstrap mode for quota crisis (vc-b027)
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
		e.getMonitor().EndExecution(false, false)
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
		e.getMonitor().EndExecution(false, false)
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
	e.getMonitor().EndExecution(procResult.Completed && result.Success, procResult.GatesPassed)

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

		updates := map[string]interface{}{
			"status": string(types.StatusBlocked),
		}

		// Log status change for audit trail (vc-n4lx)
		e.store.LogStatusChangeFromUpdates(ctx, issueID, updates, "executor",
			fmt.Sprintf("consecutive failures threshold reached: %s", blockReason))

		if err := e.store.UpdateIssue(ctx, issueID, updates, "executor"); err != nil {
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
