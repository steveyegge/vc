package executor

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/steveyegge/vc/internal/events"
	"github.com/steveyegge/vc/internal/types"
)

// escalationTracker tracks escalation state for a single baseline issue (vc-h8b8).
// This is used to determine when to escalate a baseline issue that fails repeatedly
// or remains unresolved for too long.
type escalationTracker struct {
	IssueID       string    // The baseline issue being tracked
	AttemptCount  int       // Number of times we've attempted to fix this issue
	FirstSeen     time.Time // When we first saw this baseline issue (entered self-healing mode)
	LastAttempted time.Time // Last time we attempted work on this issue
}

// getOrCreateTracker gets or creates an escalation tracker for a baseline issue.
// This is thread-safe via the escalationMutex.
func (e *Executor) getOrCreateTracker(issueID string) *escalationTracker {
	e.escalationMutex.Lock()
	defer e.escalationMutex.Unlock()

	tracker, exists := e.escalationTrackers[issueID]
	if !exists {
		tracker = &escalationTracker{
			IssueID:       issueID,
			AttemptCount:  0,
			FirstSeen:     time.Now(),
			LastAttempted: time.Time{},
		}
		e.escalationTrackers[issueID] = tracker
	}
	return tracker
}

// incrementAttempt increments the attempt count for a baseline issue.
// Call this when the executor claims or attempts work on a baseline issue.
func (e *Executor) incrementAttempt(issueID string) {
	e.escalationMutex.Lock()
	defer e.escalationMutex.Unlock()

	tracker, exists := e.escalationTrackers[issueID]
	if !exists {
		tracker = &escalationTracker{
			IssueID:       issueID,
			AttemptCount:  1,
			FirstSeen:     time.Now(),
			LastAttempted: time.Now(),
		}
		e.escalationTrackers[issueID] = tracker
	} else {
		tracker.AttemptCount++
		tracker.LastAttempted = time.Now()
	}

	fmt.Printf("Escalation tracking: %s attempt #%d (first seen: %v ago)\n",
		issueID, tracker.AttemptCount, time.Since(tracker.FirstSeen).Round(time.Second))
}

// checkEscalationThresholds checks if any baseline issue has exceeded escalation thresholds.
// Returns the issue ID and reason if escalation is needed, or empty strings if not.
// This checks BOTH attempt count and duration thresholds.
func (e *Executor) checkEscalationThresholds(ctx context.Context) (string, string) {
	e.escalationMutex.RLock()
	defer e.escalationMutex.RUnlock()

	// Check all tracked baseline issues
	for issueID, tracker := range e.escalationTrackers {
		// Check attempt threshold
		if e.config.MaxEscalationAttempts > 0 && tracker.AttemptCount >= e.config.MaxEscalationAttempts {
			reason := fmt.Sprintf("Baseline issue %s failed %d times (threshold: %d attempts)",
				issueID, tracker.AttemptCount, e.config.MaxEscalationAttempts)
			return issueID, reason
		}

		// Check duration threshold
		duration := time.Since(tracker.FirstSeen)
		if e.config.MaxEscalationDuration > 0 && duration >= e.config.MaxEscalationDuration {
			reason := fmt.Sprintf("Baseline issue %s unresolved for %v (threshold: %v)",
				issueID, duration.Round(time.Minute), e.config.MaxEscalationDuration)
			return issueID, reason
		}
	}

	return "", ""
}

// clearTracker removes escalation tracking for a baseline issue.
// Call this when a baseline issue is successfully resolved (closed).
func (e *Executor) clearTracker(issueID string) {
	e.escalationMutex.Lock()
	defer e.escalationMutex.Unlock()
	delete(e.escalationTrackers, issueID)
	fmt.Printf("Escalation tracking: cleared tracker for resolved issue %s\n", issueID)
}

// clearAllTrackers removes all escalation trackers.
// Call this when transitioning back to HEALTHY state (all baseline issues resolved).
func (e *Executor) clearAllTrackers() {
	e.escalationMutex.Lock()
	defer e.escalationMutex.Unlock()

	if len(e.escalationTrackers) > 0 {
		fmt.Printf("Escalation tracking: clearing %d tracker(s) (system healthy)\n", len(e.escalationTrackers))
		e.escalationTrackers = make(map[string]*escalationTracker)
	}
}

// escalateBaseline performs escalation actions for a baseline issue that has exceeded thresholds.
// This includes:
// 1. Adding no-auto-claim label to baseline issue
// 2. Creating escalation issue (P0, urgent, no-auto-claim)
// 3. Logging detailed diagnostics
// 4. Emitting escalation event to activity feed
func (e *Executor) escalateBaseline(ctx context.Context, issueID string, reason string) error {
	tracker := e.getOrCreateTracker(issueID)

	fmt.Fprintf(os.Stderr, "\n🚨 ESCALATING BASELINE ISSUE: %s\n", issueID)
	fmt.Fprintf(os.Stderr, "   Reason: %s\n", reason)
	fmt.Fprintf(os.Stderr, "   Attempts: %d\n", tracker.AttemptCount)
	fmt.Fprintf(os.Stderr, "   Duration: %v\n", time.Since(tracker.FirstSeen).Round(time.Minute))
	if !tracker.LastAttempted.IsZero() {
		fmt.Fprintf(os.Stderr, "   Last attempt: %v ago\n", time.Since(tracker.LastAttempted).Round(time.Minute))
	}

	// Get the baseline issue to extract details
	issue, err := e.store.GetIssue(ctx, issueID)
	if err != nil {
		return fmt.Errorf("failed to get baseline issue %s: %w", issueID, err)
	}

	// Step 1: Add no-auto-claim label to baseline issue
	if err := e.store.AddLabel(ctx, issueID, "no-auto-claim", "executor-escalation"); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to add no-auto-claim label to %s: %v\n", issueID, err)
		// Don't fail - continue with other escalation steps
	} else {
		fmt.Printf("✓ Added no-auto-claim label to baseline issue %s\n", issueID)
	}

	// Step 2: Create escalation issue
	escalationIssue := &types.Issue{
		Title:              fmt.Sprintf("ESCALATED: Baseline %s needs human intervention", GetGateType(issueID)),
		IssueType:          types.TypeTask,
		Priority:           0, // P0 - highest priority
		Status:             types.StatusOpen,
		AcceptanceCriteria: "Baseline issue resolved and quality gates passing",
		Description: fmt.Sprintf(`# Baseline Issue Escalation

The baseline issue **%s** has exceeded escalation thresholds and requires human intervention.

## Escalation Details
- **Baseline Issue**: %s
- **Gate Type**: %s
- **Reason**: %s
- **Attempts**: %d
- **Duration**: %v
- **First Seen**: %s
- **Last Attempted**: %s

## What Happened
The VC executor attempted to fix this baseline issue multiple times but was unable to resolve it within the configured thresholds. The issue has been marked with the no-auto-claim label to prevent the executor from continuing to work on it.

## Next Steps
1. Review the baseline issue (%s) to understand why it's failing
2. Check recent commits that may have broken the baseline
3. Manually fix the underlying problem
4. Verify the quality gate passes: ` + "`make %s`" + `
5. Close this escalation issue once resolved
6. Remove the no-auto-claim label from %s if you want VC to resume work

## Diagnostics
%s
`, issueID, issueID, GetGateType(issueID), reason, tracker.AttemptCount,
			time.Since(tracker.FirstSeen).Round(time.Minute),
			tracker.FirstSeen.Format(time.RFC3339),
			tracker.LastAttempted.Format(time.RFC3339),
			issueID, GetGateType(issueID), issueID,
			e.formatDiagnostics(ctx, issue)),
	}

	if err := e.store.CreateIssue(ctx, escalationIssue, "executor-escalation"); err != nil {
		return fmt.Errorf("failed to create escalation issue: %w", err)
	}

	fmt.Printf("✓ Created escalation issue: %s\n", escalationIssue.ID)

	// Add no-auto-claim label to escalation issue (human-only)
	if err := e.store.AddLabel(ctx, escalationIssue.ID, "no-auto-claim", "executor-escalation"); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to add no-auto-claim label to escalation issue %s: %v\n", escalationIssue.ID, err)
	}

	// Add escalation label for visibility
	if err := e.store.AddLabel(ctx, escalationIssue.ID, "escalation", "executor-escalation"); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to add escalation label to %s: %v\n", escalationIssue.ID, err)
	}

	// Add discovered-from dependency to track relationship
	dep := &types.Dependency{
		IssueID:     escalationIssue.ID,
		DependsOnID: issueID,
		Type:        "discovered-from",
	}
	if err := e.store.AddDependency(ctx, dep, "executor-escalation"); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to add discovered-from dependency: %v\n", err)
	}

	// Step 3: Emit escalation event to activity feed
	e.logEvent(ctx, events.EventTypeExecutorSelfHealingMode, events.SeverityError, issueID,
		fmt.Sprintf("ESCALATED: Baseline issue %s requires human intervention", issueID),
		map[string]interface{}{
			"event_subtype":      "baseline_escalated",
			"baseline_issue":     issueID,
			"escalation_issue":   escalationIssue.ID,
			"reason":             reason,
			"attempt_count":      tracker.AttemptCount,
			"duration_minutes":   int(time.Since(tracker.FirstSeen).Minutes()),
			"first_seen":         tracker.FirstSeen.Format(time.RFC3339),
			"last_attempted":     tracker.LastAttempted.Format(time.RFC3339),
			"gate_type":          GetGateType(issueID),
			"threshold_attempts": e.config.MaxEscalationAttempts,
			"threshold_duration": e.config.MaxEscalationDuration.String(),
		})

	fmt.Printf("\n🚨 Escalation complete. Human intervention required.\n")
	fmt.Printf("   Escalation issue: %s\n", escalationIssue.ID)
	fmt.Printf("   Baseline issue: %s (marked no-auto-claim)\n\n", issueID)

	return nil
}

// formatDiagnostics generates diagnostic information for the escalation issue description.
func (e *Executor) formatDiagnostics(ctx context.Context, baselineIssue *types.Issue) string {
	diagnostics := "### Issue Details\n"
	diagnostics += fmt.Sprintf("- **Status**: %s\n", baselineIssue.Status)
	diagnostics += fmt.Sprintf("- **Priority**: P%d\n", baselineIssue.Priority)
	diagnostics += fmt.Sprintf("- **Created**: %s\n", baselineIssue.CreatedAt.Format(time.RFC3339))
	diagnostics += fmt.Sprintf("- **Updated**: %s\n", baselineIssue.UpdatedAt.Format(time.RFC3339))

	if baselineIssue.Description != "" {
		diagnostics += fmt.Sprintf("\n### Description\n%s\n", baselineIssue.Description)
	}

	// Check for dependencies
	deps, err := e.store.GetDependencyRecords(ctx, baselineIssue.ID)
	if err == nil && len(deps) > 0 {
		diagnostics += "\n### Dependencies\n"
		for _, dep := range deps {
			if dep.Type != "discovered-from" {
				parent, err := e.store.GetIssue(ctx, dep.DependsOnID)
				if err == nil {
					diagnostics += fmt.Sprintf("- **%s** (%s): %s\n", dep.DependsOnID, parent.Status, parent.Title)
				}
			}
		}
	}

	// Check for dependents
	dependents, err := e.store.GetDependents(ctx, baselineIssue.ID)
	if err == nil && len(dependents) > 0 {
		diagnostics += fmt.Sprintf("\n### Dependents (%d issues)\n", len(dependents))
		for i, dep := range dependents {
			if i < 5 { // Show first 5
				diagnostics += fmt.Sprintf("- **%s** (%s): %s\n", dep.ID, dep.Status, dep.Title)
			}
		}
		if len(dependents) > 5 {
			diagnostics += fmt.Sprintf("- ... and %d more\n", len(dependents)-5)
		}
	}

	return diagnostics
}
