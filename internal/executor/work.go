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
			return work, nil
		}

		if work := e.investigateBlockedBaseline(ctx); work != nil {
			return work, nil
		}

		if work := e.findDiscoveredBlockers(ctx); work != nil {
			return work, nil
		}

		// No work found - check escalation
		e.logBlockageDiagnostics(ctx)

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
func (e *Executor) findBaselineIssues(ctx context.Context) *types.Issue {
	baselineIssues, err := e.store.GetIssuesByLabel(ctx, "baseline-failure")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to get baseline issues: %v\n", err)
		return nil
	}

	for _, issue := range baselineIssues {
		if issue.Status == types.StatusOpen {
			// Check if it has blocking dependencies
			depRecords, err := e.store.GetDependencyRecords(ctx, issue.ID)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Warning: failed to get dependency records for %s: %v\n", issue.ID, err)
				continue
			}

			// Check if any dependencies are blocking (not closed)
			hasBlockingDeps := false
			for _, dep := range depRecords {
				// Skip metadata dependencies
				if dep.Type == "discovered-from" {
					continue
				}

				parent, err := e.store.GetIssue(ctx, dep.DependsOnID)
				if err != nil {
					fmt.Fprintf(os.Stderr, "Warning: failed to get parent %s: %v\n", dep.DependsOnID, err)
					hasBlockingDeps = true
					break
				}
				if parent.Status != types.StatusClosed {
					hasBlockingDeps = true
					break
				}
			}

			if !hasBlockingDeps {
				fmt.Printf("Found ready baseline issue: %s - %s\n", issue.ID, issue.Title)
				e.logEvent(ctx, events.EventTypeProgress, events.SeverityInfo, issue.ID,
					fmt.Sprintf("Self-healing: found ready baseline issue %s", issue.ID),
					map[string]interface{}{
						"event_subtype": "baseline_issue_selected",
						"issue_id":      issue.ID,
						"mode":          "SELF_HEALING",
					})
				return issue
			}
		}
	}

	return nil
}

// investigateBlockedBaseline checks if baseline-failure issues are blocked by dependencies.
// If found, returns the first ready dependent that could unblock the baseline.
// Returns nil if no dependencies found or all are also blocked.
func (e *Executor) investigateBlockedBaseline(ctx context.Context) *types.Issue {
	baselineIssues, err := e.store.GetIssuesByLabel(ctx, "baseline-failure")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to get baseline issues: %v\n", err)
		return nil
	}

	// For each blocked baseline issue, check its dependencies
	for _, baseline := range baselineIssues {
		if baseline.Status != types.StatusOpen {
			continue
		}

		depRecords, err := e.store.GetDependencyRecords(ctx, baseline.ID)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to get dependency records for %s: %v\n", baseline.ID, err)
			continue
		}

		// Check each dependency to find ready work
		for _, dep := range depRecords {
			// Skip metadata dependencies
			if dep.Type == "discovered-from" {
				continue
			}

			parent, err := e.store.GetIssue(ctx, dep.DependsOnID)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Warning: failed to get parent %s: %v\n", dep.DependsOnID, err)
				continue
			}

			// If this dependency is open and ready, work on it
			if parent.Status == types.StatusOpen {
				// Check if the parent itself is ready (no blocking dependencies)
				parentDeps, err := e.store.GetDependencyRecords(ctx, parent.ID)
				if err != nil {
					fmt.Fprintf(os.Stderr, "Warning: failed to get dependency records for %s: %v\n", parent.ID, err)
					continue
				}

				hasBlockingDeps := false
				for _, parentDep := range parentDeps {
					if parentDep.Type == "discovered-from" {
						continue
					}
					grandparent, err := e.store.GetIssue(ctx, parentDep.DependsOnID)
					if err != nil {
						fmt.Fprintf(os.Stderr, "Warning: failed to get grandparent %s: %v\n", parentDep.DependsOnID, err)
						hasBlockingDeps = true
						break
					}
					if grandparent.Status != types.StatusClosed {
						hasBlockingDeps = true
						break
					}
				}

				if !hasBlockingDeps {
					fmt.Printf("Found blocker of baseline issue: %s - %s (blocks %s)\n", parent.ID, parent.Title, baseline.ID)
					e.logEvent(ctx, events.EventTypeProgress, events.SeverityInfo, parent.ID,
						fmt.Sprintf("Self-healing: found blocker %s that blocks baseline %s", parent.ID, baseline.ID),
						map[string]interface{}{
							"event_subtype":  "baseline_blocker_selected",
							"issue_id":       parent.ID,
							"baseline_issue": baseline.ID,
							"mode":           "SELF_HEALING",
						})
					return parent
				}
			}
		}
	}

	return nil
}

// findDiscoveredBlockers finds discovered:blocker issues that are ready to execute.
// Returns the first ready blocker, or nil if none are ready.
func (e *Executor) findDiscoveredBlockers(ctx context.Context) *types.Issue {
	blockerIssues, err := e.store.GetIssuesByLabel(ctx, "discovered:blocker")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to get discovered blocker issues: %v\n", err)
		return nil
	}

	for _, issue := range blockerIssues {
		if issue.Status == types.StatusOpen {
			// Check if it has blocking dependencies
			depRecords, err := e.store.GetDependencyRecords(ctx, issue.ID)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Warning: failed to get dependency records for %s: %v\n", issue.ID, err)
				continue
			}

			// Check if any dependencies are blocking (not closed)
			hasBlockingDeps := false
			for _, dep := range depRecords {
				// Skip metadata dependencies
				if dep.Type == "discovered-from" {
					continue
				}

				parent, err := e.store.GetIssue(ctx, dep.DependsOnID)
				if err != nil {
					fmt.Fprintf(os.Stderr, "Warning: failed to get parent %s: %v\n", dep.DependsOnID, err)
					hasBlockingDeps = true
					break
				}
				if parent.Status != types.StatusClosed {
					hasBlockingDeps = true
					break
				}
			}

			if !hasBlockingDeps {
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
		}
	}

	return nil
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
// Returns true if the executor has been stuck in self-healing mode for too long.
func (e *Executor) shouldEscalate(ctx context.Context) bool {
	// Get how long we've been in self-healing mode
	modeChangedAt := e.getModeChangedAt()
	duration := time.Since(modeChangedAt)

	// Escalate if stuck for more than 30 minutes
	escalationThreshold := 30 * time.Minute
	if duration > escalationThreshold {
		fmt.Printf("⚠️  Self-healing mode has been active for %v (threshold: %v)\n", duration, escalationThreshold)
		return true
	}

	return false
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
