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
		}
	}

	return nil
}

// investigateBlockedBaseline checks if baseline-failure issues are blocked.
// If a blocked baseline has ready dependents (children), returns the first ready dependent.
// This allows working on child issues even when the parent baseline is blocked.
// Returns nil if no dependents found or all are also blocked.
func (e *Executor) investigateBlockedBaseline(ctx context.Context) *types.Issue {
	baselineIssues, err := e.store.GetIssuesByLabel(ctx, "baseline-failure")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to get baseline issues: %v\n", err)
		return nil
	}

	// For each baseline issue
	for _, baseline := range baselineIssues {
		// Only investigate if baseline is open
		if baseline.Status != types.StatusOpen {
			continue
		}

		// Check if baseline is blocked by checking its dependencies
		depRecords, err := e.store.GetDependencyRecords(ctx, baseline.ID)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to get dependency records for %s: %v\n", baseline.ID, err)
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

		// If baseline is not blocked, skip it (handled by findBaselineIssues)
		if !hasBlockingDeps {
			continue
		}

		// Baseline is blocked - investigate dependents (children)
		fmt.Printf("Baseline issue %s is blocked, investigating dependents\n", baseline.ID)
		e.logEvent(ctx, events.EventTypeProgress, events.SeverityInfo, baseline.ID,
			fmt.Sprintf("Investigating dependents of blocked baseline %s", baseline.ID),
			map[string]interface{}{
				"event_subtype": "baseline_investigation",
				"baseline_id":   baseline.ID,
				"mode":          "SELF_HEALING",
			})

		// Get all dependents (issues that depend on this baseline)
		dependents, err := e.store.GetDependents(ctx, baseline.ID)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to get dependents for %s: %v\n", baseline.ID, err)
			continue
		}

		if len(dependents) == 0 {
			fmt.Printf("  No dependents found for blocked baseline %s\n", baseline.ID)
			continue
		}

		// Filter for ready dependents
		var ready []*types.Issue
		for _, dep := range dependents {
			if e.isReady(ctx, dep) {
				ready = append(ready, dep)
			}
		}

		if len(ready) == 0 {
			e.logBlockageReasons(ctx, baseline, dependents)
			continue
		}

		// Found ready dependent - work on it
		fmt.Printf("Found ready dependent of blocked baseline: %s - %s (child of %s)\n",
			ready[0].ID, ready[0].Title, baseline.ID)

		// Track escalation attempt for the parent baseline issue (vc-h8b8)
		// We're working on a child to unblock the baseline, so this counts as an attempt
		e.incrementAttempt(baseline.ID)

		e.logEvent(ctx, events.EventTypeProgress, events.SeverityInfo, ready[0].ID,
			fmt.Sprintf("Self-healing: found ready dependent %s of blocked baseline %s", ready[0].ID, baseline.ID),
			map[string]interface{}{
				"event_subtype":  "baseline_dependent_selected",
				"issue_id":       ready[0].ID,
				"baseline_issue": baseline.ID,
				"ready_count":    len(ready),
				"mode":           "SELF_HEALING",
			})
		return ready[0]
	}

	return nil
}

// isReady checks if an issue is ready to be worked on (open status, no blocking dependencies).
func (e *Executor) isReady(ctx context.Context, issue *types.Issue) bool {
	// Must be open
	if issue.Status != types.StatusOpen {
		return false
	}

	// Check for blocking dependencies
	depRecords, err := e.store.GetDependencyRecords(ctx, issue.ID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to get dependency records for %s: %v\n", issue.ID, err)
		return false
	}

	// Check if any dependencies are blocking (not closed)
	for _, dep := range depRecords {
		// Skip metadata dependencies
		if dep.Type == "discovered-from" {
			continue
		}

		parent, err := e.store.GetIssue(ctx, dep.DependsOnID)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to get parent %s: %v\n", dep.DependsOnID, err)
			return false
		}
		if parent.Status != types.StatusClosed {
			return false
		}
	}

	return true
}

// logBlockageReasons logs why dependents of a blocked baseline are themselves blocked.
func (e *Executor) logBlockageReasons(ctx context.Context, baseline *types.Issue, dependents []*types.Issue) {
	fmt.Printf("  Found %d dependents of blocked baseline %s, but none are ready:\n", len(dependents), baseline.ID)

	for _, dep := range dependents {
		if dep.Status != types.StatusOpen {
			fmt.Printf("    - %s: status=%s\n", dep.ID, dep.Status)
			continue
		}

		// Check why it's blocked
		depRecords, err := e.store.GetDependencyRecords(ctx, dep.ID)
		if err != nil {
			fmt.Printf("    - %s: error getting dependencies: %v\n", dep.ID, err)
			continue
		}

		blockedBy := []string{}
		for _, depRec := range depRecords {
			if depRec.Type == "discovered-from" {
				continue
			}
			parent, err := e.store.GetIssue(ctx, depRec.DependsOnID)
			if err != nil {
				blockedBy = append(blockedBy, depRec.DependsOnID+"(error)")
				continue
			}
			if parent.Status != types.StatusClosed {
				blockedBy = append(blockedBy, parent.ID)
			}
		}

		if len(blockedBy) > 0 {
			fmt.Printf("    - %s: blocked by %v\n", dep.ID, blockedBy)
		}
	}
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
// Returns true if any baseline issue has exceeded escalation thresholds (attempts or duration).
// This is now per-baseline-issue tracking rather than overall self-healing mode duration (vc-h8b8).
func (e *Executor) shouldEscalate(ctx context.Context) bool {
	// Check if any baseline issue has exceeded thresholds
	issueID, reason := e.checkEscalationThresholds(ctx)
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
