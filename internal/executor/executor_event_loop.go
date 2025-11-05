package executor

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/steveyegge/vc/internal/events"
	"github.com/steveyegge/vc/internal/types"
)

// eventLoop is the main event loop that processes issues
func (e *Executor) eventLoop(ctx context.Context) {
	defer close(e.doneCh)

	ticker := time.NewTicker(e.pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-e.stopCh:
			return
		case <-ticker.C:
			// Process one code work issue (regular tasks)
			// Note: Heartbeat updates now happen in dedicated heartbeatLoop() goroutine (vc-m4od)
			if err := e.processNextIssue(ctx); err != nil {
				// Log error but continue
				fmt.Fprintf(os.Stderr, "error processing issue: %v\n", err)
			}

			// Process one QA work issue (quality gates for missions) (vc-254)
			if e.enableQualityGateWorker && e.qaWorker != nil {
				if err := e.processNextQAWork(ctx); err != nil {
					// Log error but continue
					fmt.Fprintf(os.Stderr, "error processing QA work: %v\n", err)
				}
			}

			// Check health monitors after completing an issue (if enabled)
			if e.enableHealthMonitoring && e.healthRegistry != nil {
				if err := e.checkHealthMonitors(ctx); err != nil {
					// Log error but continue
					fmt.Fprintf(os.Stderr, "error running health monitors: %v\n", err)
				}
			}
		}
	}
}

// processNextQAWork attempts to claim and process one mission that needs quality gates (vc-254)
func (e *Executor) processNextQAWork(ctx context.Context) error {
	// Try to claim a mission needing quality gates
	mission, err := e.qaWorker.ClaimReadyWork(ctx)
	if err != nil {
		return fmt.Errorf("failed to claim QA work: %w", err)
	}

	// No QA work available
	if mission == nil {
		return nil
	}

	// Execute quality gates in background goroutine to enable parallelism
	// This allows code workers to continue working while gates run
	e.qaWorkersWg.Add(1) // Track goroutine for graceful shutdown (vc-0d58)
	go func() {
		defer e.qaWorkersWg.Done() // Release goroutine tracker (vc-0d58)
		if err := e.qaWorker.Execute(ctx, mission); err != nil {
			// Log error - QA worker handles state transitions internally
			fmt.Fprintf(os.Stderr, "QA worker execution failed for %s: %v\n", mission.ID, err)
		}
	}()

	return nil
}

// checkMissionConvergence checks if completing this issue causes a mission to converge.
// If the issue is a discovered:blocker and its parent mission has now converged, logs the event.
func (e *Executor) checkMissionConvergence(ctx context.Context, issue *types.Issue) error {
	// Check if this issue has the discovered:blocker label
	labels, err := e.store.GetLabels(ctx, issue.ID)
	if err != nil {
		return fmt.Errorf("failed to get labels for %s: %w", issue.ID, err)
	}

	hasBlockerLabel := false
	for _, label := range labels {
		if label == "discovered:blocker" {
			hasBlockerLabel = true
			break
		}
	}

	if !hasBlockerLabel {
		// Not a blocker, no need to check convergence
		return nil
	}

	// Find the mission root
	missionRoot, err := GetMissionRoot(ctx, e.store, issue.ID)
	if err != nil {
		return fmt.Errorf("failed to get mission root: %w", err)
	}

	// Check if mission has converged
	converged, err := HasMissionConverged(ctx, e.store, missionRoot.ID)
	if err != nil {
		return fmt.Errorf("failed to check mission convergence: %w", err)
	}

	if converged {
		fmt.Printf("\n✓ Mission %s (%s) has converged - all discovered work complete!\n",
			missionRoot.ID, missionRoot.Title)

		// Log convergence event
		e.logEvent(ctx, events.EventTypeProgress, events.SeverityInfo, missionRoot.ID,
			fmt.Sprintf("Mission %s converged after completing blocker %s", missionRoot.ID, issue.ID),
			map[string]interface{}{
				"event_subtype":     "mission_converged",
				"mission_id":        missionRoot.ID,
				"mission_title":     missionRoot.Title,
				"completed_blocker": issue.ID,
			})
	}

	return nil
}

// checkEpicCompletion checks if completing this task causes its parent epic to complete.
// If the epic is now complete (all children are closed/deferred), adds the 'needs-quality-gates' label.
// Handles nested epic hierarchies by checking all parent epics up to the mission root.
// vc-235: Epic-centric workflow integration after task completion
func (e *Executor) checkEpicCompletion(ctx context.Context, issue *types.Issue) error {
	// Walk up the parent-child dependency chain to check all parent epics
	parentEpics, err := e.store.GetDependencies(ctx, issue.ID)
	if err != nil {
		return fmt.Errorf("failed to get parent dependencies for %s: %w", issue.ID, err)
	}

	// Check each parent epic for completion
	for _, parentEpic := range parentEpics {
		// Only check epic types (not regular tasks)
		if parentEpic.IssueType != types.TypeEpic {
			continue
		}

		// Check if this epic is now complete
		isComplete, err := e.store.IsEpicComplete(ctx, parentEpic.ID)
		if err != nil {
			return fmt.Errorf("failed to check if epic %s is complete: %w", parentEpic.ID, err)
		}

		if isComplete {
			fmt.Printf("\n✓ Epic %s (%s) is now complete - all tasks finished!\n",
				parentEpic.ID, parentEpic.Title)

			// Add 'needs-quality-gates' label to trigger next workflow phase
			if err := e.store.AddLabel(ctx, parentEpic.ID, "needs-quality-gates", "executor"); err != nil {
				return fmt.Errorf("failed to add needs-quality-gates label to epic %s: %w", parentEpic.ID, err)
			}

			// Note: Epic completion event is emitted by checkAndCloseEpicIfComplete() in epic.go
			// (called via result processor) using EventTypeEpicCompleted (vc-268, vc-274).
			// Removed duplicate old-style progress event that was previously emitted here.

			// Recursively check if the parent epic completing causes its parent to complete
			// This handles nested epic hierarchies (e.g., phase → mission)
			if err := e.checkEpicCompletion(ctx, parentEpic); err != nil {
				// Log but don't fail - we've already marked this epic as complete
				fmt.Fprintf(os.Stderr, "warning: failed to check parent epic completion: %v\n", err)
			}
		}
	}

	return nil
}

// getNextReadyBlocker finds the highest priority discovered:blocker issue that is ready to execute.
// Returns nil if no ready blockers are found.
// vc-156: Optimized to use single SQL query instead of N+1 queries
func (e *Executor) getNextReadyBlocker(ctx context.Context) (*types.Issue, error) {
	// Use optimized storage method that does filtering in SQL (vc-156)
	// This replaces the old approach of fetching all blockers then checking dependencies one by one
	// Performance: O(1) query instead of O(N) queries where N = number of blockers
	blockers, err := e.store.GetReadyBlockers(ctx, 1)
	if err != nil {
		return nil, fmt.Errorf("failed to get ready blockers: %w", err)
	}

	if len(blockers) == 0 {
		fmt.Printf("No ready blockers found, falling back to regular work\n")
		return nil, nil
	}

	bestBlocker := blockers[0]
	fmt.Printf("Found ready blocker: %s (P%d) - %s\n", bestBlocker.ID, bestBlocker.Priority, bestBlocker.Title)
	return bestBlocker, nil
}

// processNextIssue claims and processes the next ready issue with priority order:
// 1. Discovered blockers (label=discovered:blocker, status=open, no blocking dependencies)
// 2. Regular ready work (no dependencies)
// 3. Discovered related work (label=discovered:related, status=open, no blocking dependencies)
//
// Note: Blockers take absolute priority over regular work, regardless of priority numbers.
// This may cause regular work to wait indefinitely if blockers continuously appear.
// This is intentional behavior to ensure mission convergence - discovered blockers must
// be addressed before missions can be considered complete. Work starvation of regular
// tasks is acceptable to maintain mission quality and ensure discovered issues are resolved.
// See vc-160 for monitoring work starvation, and vc-161 for prioritization policy docs.
//
// vc-a6ko: Refactored to use GetReadyWork() with smart fallback chain for self-healing mode
func (e *Executor) processNextIssue(ctx context.Context) error {
	// vc-196: Run preflight quality gates check before claiming work
	if e.preFlightChecker != nil {
		// vc-47e0: When in self-healing mode, invalidate cache to force fresh baseline check
		// This allows the executor to detect when baseline issues are fixed without restart
		if e.getSelfHealingMode() != ModeHealthy {
			e.preFlightChecker.InvalidateAllCache()
		}

		allPassed, commitHash, err := e.preFlightChecker.CheckBaseline(ctx, e.instanceID)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Preflight check failed: %v\n", err)
			// Continue polling but don't claim work
			return nil
		}

		if !allPassed {
			// Baseline failed - enter self-healing mode
			failureMode := e.preFlightChecker.config.FailureMode

			switch failureMode {
			case FailureModeBlock:
				// Get cached gate results from CheckBaseline call above
				results, err := e.preFlightChecker.GetCachedResults(ctx, commitHash)
				if err != nil {
					fmt.Fprintf(os.Stderr, "Failed to get cached gate results: %v\n", err)
					// Continue anyway - we'll try again next poll
					return nil
				}
				if results == nil {
					fmt.Fprintf(os.Stderr, "No cached gate results available for commit %s\n", commitHash)
					// Continue anyway - we'll try again next poll
					return nil
				}

				// Enter self-healing mode - only claim baseline issues until fixed
				if e.getSelfHealingMode() == ModeHealthy {
					// Create baseline blocking issues for failing gates (only on state transition)
					if err := e.preFlightChecker.HandleBaselineFailure(ctx, e.instanceID, commitHash, results); err != nil {
						fmt.Fprintf(os.Stderr, "Failed to handle baseline failure: %v\n", err)
						// Continue anyway - we'll try again next poll
					}

					// Note: transitionToSelfHealing will print the banner
				}
				e.transitionToSelfHealing(ctx)

			case FailureModeWarn:
				// Warn but continue claiming work
				fmt.Printf("⚠️  WARNING: Baseline failed on commit %s but continuing anyway (warn mode)\n", commitHash)
				// Continue to claim work below

			case FailureModeIgnore:
				// Ignore failures completely
				// Continue to claim work below
			}
		} else {
			// vc-1d3d: Baseline passes - exit self-healing mode if we were in it
			if e.getSelfHealingMode() != ModeHealthy {
				// Note: transitionToHealthy will print the banner
				e.transitionToHealthy(ctx)
			}
		}
	}

	// vc-a6ko: Use GetReadyWork() which handles self-healing fallback chain
	// Throttle log message: only print once per minute when in self-healing mode
	if e.getSelfHealingMode() != ModeHealthy {
		if time.Since(e.selfHealingMsgLast) > time.Minute {
			fmt.Printf("⚠️  Self-healing mode: trying fallback chain\n")
			e.selfHealingMsgLast = time.Now()
		}
	}

	// Get next ready work using smart selection
	issue, err := e.GetReadyWork(ctx)
	if err != nil {
		return fmt.Errorf("failed to get ready work: %w", err)
	}

	// No work available
	if issue == nil {
		return nil
	}

	// Attempt to claim the issue
	if err := e.store.ClaimIssue(ctx, issue.ID, e.instanceID); err != nil {
		// Issue may have been claimed by another executor
		// This is expected in multi-executor scenarios
		return nil
	}

	// Successfully claimed - now execute it
	return e.executeIssue(ctx, issue)
}
