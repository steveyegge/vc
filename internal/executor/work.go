package executor

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/steveyegge/vc/internal/events"
	"github.com/steveyegge/vc/internal/types"
)

// GetReadyWork implements smart work selection with fallback chain based on self-healing mode.
// Returns the next issue to work on, or nil if no work is available.
//
// When in SELF_HEALING mode, uses fallback chain:
// 1. Find baseline-failure labeled issues (ready)
// 2. Investigate blocked baseline and claim ready dependents
// 3. Find discovered:blocker issues (ready)
// 4. Log diagnostics if no work found
// 5. Check escalation threshold
// 6. Fall through to regular work
//
// When in HEALTHY or ESCALATED mode, claims regular work.
func (e *Executor) GetReadyWork(ctx context.Context) (*types.Issue, error) {
	mode := e.getSelfHealingMode()

	switch mode {
	case ModeHealthy:
		return e.getNormalWork(ctx)

	case ModeSelfHealing:
		// Try fallback chain
		if work := e.findBaselineIssues(ctx); work != nil {
			e.recordSelfHealingProgress()
			return work, nil
		}

		if work := e.investigateBlockedBaseline(ctx); work != nil {
			e.recordSelfHealingProgress()
			return work, nil
		}

		if work := e.findDiscoveredBlockers(ctx); work != nil {
			e.recordSelfHealingProgress()
			return work, nil
		}

		// No work found - increment counter and check for deadlock (vc-ipoj)
		e.recordSelfHealingNoWork()

		// Check if we're deadlocked (all baselines blocked indefinitely)
		if e.isSelfHealingDeadlocked() {
			// Create diagnostic issue and exit self-healing mode
			if err := e.escalateSelfHealingDeadlock(ctx); err != nil {
				fmt.Fprintf(os.Stderr, "Warning: failed to escalate deadlock: %v\n", err)
			}
			// Exit self-healing mode and allow regular work
			e.transitionToEscalated(ctx, "Self-healing deadlock: all baselines blocked indefinitely")
			return e.getNormalWork(ctx)
		}

		// Log diagnostics periodically
		e.logBlockageDiagnostics(ctx)

		// Check per-issue escalation thresholds (attempts/duration)
		if e.shouldEscalate(ctx) {
			reason := "Self-healing mode exhausted all fallback options without making progress"
			e.transitionToEscalated(ctx, reason)
		}

		// Fall through to regular work
		return e.getNormalWork(ctx)

	case ModeEscalated:
		// In escalated mode, just claim regular work
		// Human intervention is needed to fix the baseline
		return e.getNormalWork(ctx)

	default:
		return nil, fmt.Errorf("unknown self-healing mode: %v", mode)
	}
}

// findBaselineIssues finds open baseline-failure issues that are ready to execute.
// Returns the first ready baseline issue, or nil if none are ready.
// vc-1nks: Now uses optimized SQL query to reduce N+1 query problem
func (e *Executor) findBaselineIssues(ctx context.Context) *types.Issue {
	// Use optimized storage method that does filtering in SQL (vc-1nks)
	// This replaces the old approach of fetching all baseline issues then checking dependencies one by one
	// Performance: O(1) query instead of O(N) queries where N = number of baseline issues
	baselineIssues, err := e.store.GetReadyBaselineIssues(ctx, 1)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to get ready baseline issues: %v\n", err)
		return nil
	}

	if len(baselineIssues) == 0 {
		return nil
	}

	issue := baselineIssues[0]
	fmt.Printf("Found ready baseline issue: %s - %s\n", issue.ID, issue.Title)

	// Track escalation attempt for this baseline issue (vc-h8b8)
	e.incrementAttempt(issue.ID)

	e.logEvent(ctx, events.EventTypeProgress, events.SeverityInfo, issue.ID,
		fmt.Sprintf("Self-healing: found ready baseline issue %s", issue.ID),
		map[string]interface{}{
			"event_subtype": "baseline_issue_selected",
			"issue_id":      issue.ID,
			"mode":          "SELF_HEALING",
		})
	return issue
}

// investigateBlockedBaseline checks if baseline-failure issues are blocked.
// If a blocked baseline has ready dependents (children), returns the first ready dependent.
// This allows working on child issues even when the parent baseline is blocked.
// Returns nil if no dependents found or all are also blocked.
// vc-1nks: Now uses optimized SQL query to reduce N+1 query problem
func (e *Executor) investigateBlockedBaseline(ctx context.Context) *types.Issue {
	// Use optimized storage method that does all filtering in SQL (vc-1nks)
	// This replaces the old approach of:
	// 1. Fetching all baseline issues
	// 2. Checking each one's dependencies (N queries)
	// 3. Fetching dependents for each blocked baseline (N queries)
	// 4. Checking if each dependent is ready (N*M queries)
	// Performance: O(1) query instead of O(N + N + N*M) queries
	dependents, baselineMap, err := e.store.GetReadyDependentsOfBlockedBaselines(ctx, 1)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to get ready dependents of blocked baselines: %v\n", err)
		return nil
	}

	if len(dependents) == 0 {
		return nil
	}

	dependent := dependents[0]
	baselineID := baselineMap[dependent.ID]

	fmt.Printf("Baseline issue %s is blocked, found ready dependent: %s - %s\n",
		baselineID, dependent.ID, dependent.Title)

	// Track escalation attempt for the parent baseline issue (vc-h8b8)
	// We're working on a child to unblock the baseline, so this counts as an attempt
	e.incrementAttempt(baselineID)

	e.logEvent(ctx, events.EventTypeProgress, events.SeverityInfo, dependent.ID,
		fmt.Sprintf("Self-healing: found ready dependent %s of blocked baseline %s", dependent.ID, baselineID),
		map[string]interface{}{
			"event_subtype":  "baseline_dependent_selected",
			"issue_id":       dependent.ID,
			"baseline_issue": baselineID,
			"mode":           "SELF_HEALING",
		})

	return dependent
}

// findDiscoveredBlockers finds discovered:blocker issues that are ready to execute.
// Returns the first ready blocker, or nil if none are ready.
// vc-1nks: Now uses optimized SQL query (same as vc-156)
func (e *Executor) findDiscoveredBlockers(ctx context.Context) *types.Issue {
	// Use optimized storage method that does filtering in SQL (vc-156, vc-1nks)
	// This replaces the old approach of fetching all blocker issues then checking dependencies one by one
	// Performance: O(1) query instead of O(N) queries where N = number of blocker issues
	blockerIssues, err := e.store.GetReadyBlockers(ctx, 1)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to get ready blocker issues: %v\n", err)
		return nil
	}

	if len(blockerIssues) == 0 {
		return nil
	}

	issue := blockerIssues[0]
	fmt.Printf("Found ready discovered blocker: %s - %s\n", issue.ID, issue.Title)
	e.logEvent(ctx, events.EventTypeProgress, events.SeverityInfo, issue.ID,
		fmt.Sprintf("Self-healing: found ready discovered blocker %s", issue.ID),
		map[string]interface{}{
			"event_subtype": "discovered_blocker_selected",
			"issue_id":      issue.ID,
			"mode":          "SELF_HEALING",
		})
	return issue
}

// logBlockageDiagnostics logs diagnostic information when self-healing mode can't find work.
// This helps understand why the executor is stuck and what's blocking progress.
func (e *Executor) logBlockageDiagnostics(ctx context.Context) {
	fmt.Printf("\n⚠️  Self-healing diagnostics: No ready work found in fallback chain\n")

	// Count baseline issues by status
	baselineIssues, err := e.store.GetIssuesByLabel(ctx, "baseline-failure")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to get baseline issues for diagnostics: %v\n", err)
	} else {
		openCount := 0
		blockedCount := 0
		for _, issue := range baselineIssues {
			if issue.Status == types.StatusOpen {
				openCount++
				// Check if blocked
				depRecords, _ := e.store.GetDependencyRecords(ctx, issue.ID)
				for _, dep := range depRecords {
					if dep.Type != "discovered-from" {
						parent, _ := e.store.GetIssue(ctx, dep.DependsOnID)
						if parent != nil && parent.Status != types.StatusClosed {
							blockedCount++
							fmt.Printf("   - %s blocked by %s (%s)\n", issue.ID, parent.ID, parent.Title)
							break
						}
					}
				}
			}
		}
		fmt.Printf("   Baseline issues: %d total, %d open, %d blocked\n", len(baselineIssues), openCount, blockedCount)
	}

	// Count discovered blockers by status
	blockerIssues, err := e.store.GetIssuesByLabel(ctx, "discovered:blocker")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to get blocker issues for diagnostics: %v\n", err)
	} else {
		openCount := 0
		blockedCount := 0
		for _, issue := range blockerIssues {
			if issue.Status == types.StatusOpen {
				openCount++
				// Check if blocked
				depRecords, _ := e.store.GetDependencyRecords(ctx, issue.ID)
				for _, dep := range depRecords {
					if dep.Type != "discovered-from" {
						parent, _ := e.store.GetIssue(ctx, dep.DependsOnID)
						if parent != nil && parent.Status != types.StatusClosed {
							blockedCount++
							break
						}
					}
				}
			}
		}
		fmt.Printf("   Discovered blockers: %d total, %d open, %d blocked\n", len(blockerIssues), openCount, blockedCount)
	}

	// Log to activity feed
	e.logEvent(ctx, events.EventTypeExecutorSelfHealingMode, events.SeverityWarning, "SYSTEM",
		"Self-healing mode: no ready work found after checking all fallback options",
		map[string]interface{}{
			"event_subtype": "self_healing_no_work",
			"mode":          "SELF_HEALING",
			"timestamp":     time.Now().Format(time.RFC3339),
		})
}

// shouldEscalate checks if self-healing mode should escalate to human intervention.
// Returns true if any baseline issue has exceeded escalation thresholds (attempts or duration).
// This is now per-baseline-issue tracking rather than overall self-healing mode duration (vc-h8b8).
func (e *Executor) shouldEscalate(ctx context.Context) bool {
	// Check if any baseline issue has exceeded thresholds
	issueID, reason := e.checkEscalationThresholds()
	if issueID != "" {
		fmt.Printf("⚠️  Escalation threshold exceeded: %s\n", reason)

		// Perform escalation actions
		if err := e.escalateBaseline(ctx, issueID, reason); err != nil {
			fmt.Fprintf(os.Stderr, "Error escalating baseline issue %s: %v\n", issueID, err)
			// Don't return true on error - we'll retry on next iteration
			return false
		}

		return true
	}

	return false
}

// recordSelfHealingProgress records that we successfully found work in self-healing mode (vc-ipoj).
// This resets the deadlock counter and timer.
func (e *Executor) recordSelfHealingProgress() {
	e.selfHealingProgressMutex.Lock()
	defer e.selfHealingProgressMutex.Unlock()
	e.selfHealingLastProgress = time.Now()
	e.selfHealingNoWorkCount = 0
}

// recordSelfHealingNoWork records that we found no work in self-healing mode (vc-ipoj).
// This increments the deadlock counter.
func (e *Executor) recordSelfHealingNoWork() {
	e.selfHealingProgressMutex.Lock()
	defer e.selfHealingProgressMutex.Unlock()
	e.selfHealingNoWorkCount++
}

// isSelfHealingDeadlocked checks if self-healing mode has been stuck with no progress (vc-ipoj).
// Returns true if:
// 1. We've had zero progress (no claims/completions) for longer than the deadlock timeout
// 2. We haven't already created a deadlock escalation issue
func (e *Executor) isSelfHealingDeadlocked() bool {
	e.selfHealingProgressMutex.RLock()
	defer e.selfHealingProgressMutex.RUnlock()

	// Already escalated for deadlock
	if e.selfHealingDeadlockIssue != "" {
		return false
	}

	// Check timeout
	timeout := e.config.SelfHealingDeadlockTimeout
	if timeout == 0 {
		return false // Deadlock detection disabled
	}

	timeSinceProgress := time.Since(e.selfHealingLastProgress)
	return timeSinceProgress >= timeout
}

// escalateSelfHealingDeadlock creates a diagnostic issue for self-healing deadlock (vc-ipoj).
// This is called when all baseline issues are blocked and we can't make progress.
func (e *Executor) escalateSelfHealingDeadlock(ctx context.Context) error {
	e.selfHealingProgressMutex.Lock()
	timeSinceProgress := time.Since(e.selfHealingLastProgress)
	noWorkCount := e.selfHealingNoWorkCount
	e.selfHealingProgressMutex.Unlock()

	fmt.Fprintf(os.Stderr, "\n🚨 SELF-HEALING DEADLOCK DETECTED\n")
	fmt.Fprintf(os.Stderr, "   Time since last progress: %v\n", timeSinceProgress.Round(time.Second))
	fmt.Fprintf(os.Stderr, "   Consecutive no-work iterations: %d\n", noWorkCount)

	// Get baseline issues for diagnostics
	baselineIssues, err := e.store.GetIssuesByLabel(ctx, "baseline-failure")
	if err != nil {
		return fmt.Errorf("failed to get baseline issues: %w", err)
	}

	// Build detailed diagnostics
	diagnostics := "## Deadlock Analysis\n\n"
	diagnostics += fmt.Sprintf("Self-healing mode has made **zero progress** for %v (threshold: %v).\n\n",
		timeSinceProgress.Round(time.Minute), e.config.SelfHealingDeadlockTimeout)
	diagnostics += fmt.Sprintf("- **Consecutive no-work iterations**: %d\n", noWorkCount)
	diagnostics += fmt.Sprintf("- **Last progress**: %s\n", e.selfHealingLastProgress.Format(time.RFC3339))
	diagnostics += fmt.Sprintf("- **Current time**: %s\n\n", time.Now().Format(time.RFC3339))

	diagnostics += "## Baseline Issues\n\n"
	openCount := 0
	blockedCount := 0
	for _, issue := range baselineIssues {
		if issue.Status == types.StatusOpen {
			openCount++
			diagnostics += fmt.Sprintf("### %s: %s\n", issue.ID, issue.Title)
			diagnostics += fmt.Sprintf("- **Status**: %s\n", issue.Status)
			diagnostics += fmt.Sprintf("- **Priority**: P%d\n", issue.Priority)

			// Check if blocked
			depRecords, _ := e.store.GetDependencyRecords(ctx, issue.ID)
			isBlocked := false
			for _, dep := range depRecords {
				if dep.Type != "discovered-from" {
					parent, _ := e.store.GetIssue(ctx, dep.DependsOnID)
					if parent != nil && parent.Status != types.StatusClosed {
						isBlocked = true
						blockedCount++
						diagnostics += fmt.Sprintf("- **Blocked by**: %s (%s)\n", parent.ID, parent.Title)
						break
					}
				}
			}
			if !isBlocked {
				diagnostics += "- **Blocked**: No (but may have other issues)\n"
			}
			diagnostics += "\n"
		}
	}
	diagnostics += fmt.Sprintf("**Summary**: %d baseline issues total, %d open, %d blocked\n\n", len(baselineIssues), openCount, blockedCount)

	diagnostics += "## What Happened\n\n"
	diagnostics += "The VC executor entered self-healing mode to fix baseline quality gate failures, but all baseline issues are blocked by dependencies that cannot be resolved. "
	diagnostics += "This creates a deadlock where the executor cannot make progress.\n\n"

	diagnostics += "## Next Steps\n\n"
	diagnostics += "1. Review the blocked baseline issues above\n"
	diagnostics += "2. Manually resolve the blocking dependencies or remove circular dependencies\n"
	diagnostics += "3. Verify quality gates pass after fixing blockers\n"
	diagnostics += "4. Close this escalation issue once the deadlock is resolved\n"
	diagnostics += "5. The executor has exited self-healing mode and will continue with regular work\n"

	// Create escalation issue
	escalationIssue := &types.Issue{
		Title:       "ESCALATED: Self-healing deadlock - all baselines blocked",
		IssueType:   types.TypeTask,
		Priority:    0, // P0 - highest priority
		Status:      types.StatusOpen,
		Description: diagnostics,
	}

	if err := e.store.CreateIssue(ctx, escalationIssue, "executor-deadlock"); err != nil {
		return fmt.Errorf("failed to create deadlock escalation issue: %w", err)
	}

	fmt.Printf("✓ Created deadlock escalation issue: %s\n", escalationIssue.ID)

	// Add labels
	if err := e.store.AddLabel(ctx, escalationIssue.ID, "no-auto-claim", "executor-deadlock"); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to add no-auto-claim label: %v\n", err)
	}
	if err := e.store.AddLabel(ctx, escalationIssue.ID, "escalation", "executor-deadlock"); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to add escalation label: %v\n", err)
	}
	if err := e.store.AddLabel(ctx, escalationIssue.ID, "baseline-stuck", "executor-deadlock"); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to add baseline-stuck label: %v\n", err)
	}

	// Record that we created the escalation issue
	e.selfHealingProgressMutex.Lock()
	e.selfHealingDeadlockIssue = escalationIssue.ID
	e.selfHealingProgressMutex.Unlock()

	// Emit activity feed event
	e.logEvent(ctx, events.EventTypeExecutorSelfHealingMode, events.SeverityCritical, "SYSTEM",
		"Self-healing deadlock: all baselines blocked indefinitely",
		map[string]interface{}{
			"event_subtype":              "self_healing_deadlock",
			"escalation_issue":           escalationIssue.ID,
			"time_since_progress_mins":   int(timeSinceProgress.Minutes()),
			"no_work_count":              noWorkCount,
			"baseline_count":             len(baselineIssues),
			"baseline_open_count":        openCount,
			"baseline_blocked_count":     blockedCount,
			"deadlock_timeout":           e.config.SelfHealingDeadlockTimeout.String(),
		})

	fmt.Printf("\n🚨 Deadlock escalation complete. Exiting self-healing mode.\n")
	fmt.Printf("   Escalation issue: %s\n\n", escalationIssue.ID)

	return nil
}

// getNormalWork retrieves regular ready work (not in self-healing mode).
// This is the standard work selection path when baseline is healthy.
func (e *Executor) getNormalWork(ctx context.Context) (*types.Issue, error) {
	// Priority 1: Try to get a ready blocker (if blocker priority enabled)
	var issue *types.Issue
	var foundViaBlocker bool
	if e.config.EnableBlockerPriority {
		var err error
		issue, err = e.getNextReadyBlocker(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to get ready blockers: %w", err)
		}

		// Track whether we found work via blocker path
		foundViaBlocker = (issue != nil)
	}

	// Priority 2: Fall back to regular ready work
	if issue == nil {
		// vc-7100: Request multiple issues from storage since VC filters out no-auto-claim
		// If we only request 1 issue and it has no-auto-claim, we'd get nothing
		filter := types.WorkFilter{
			Status:     types.StatusOpen,
			Limit:      10, // vc-7100: Request 10 so filtering doesn't exhaust the queue
			SortPolicy: types.SortPolicyPriority, // vc-190: Always use priority-first sorting
		}

		issues, err := e.store.GetReadyWork(ctx, filter)
		if err != nil {
			return nil, fmt.Errorf("failed to get ready work: %w", err)
		}

		if len(issues) == 0 {
			// No work available
			return nil, nil
		}

		// vc-7100: Take the first issue after filtering
		issue = issues[0]
	}

	// Log when blocker is selected over regular work (vc-159)
	if foundViaBlocker {
		fmt.Printf("Claiming blocker %s (P%d) over regular ready work\n", issue.ID, issue.Priority)

		// Log agent event for blocker prioritization
		e.logEvent(ctx, events.EventTypeProgress, events.SeverityInfo, issue.ID,
			fmt.Sprintf("Blocker %s prioritized over regular work", issue.ID),
			map[string]interface{}{
				"event_subtype": "blocker_prioritized",
				"blocker_id":    issue.ID,
				"priority":      issue.Priority,
			})
	}

	return issue, nil
}
