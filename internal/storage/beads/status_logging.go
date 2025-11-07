package beads

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/steveyegge/vc/internal/types"
)

// LogStatusChange logs status changes with full audit trail.
// This is called BEFORE UpdateIssue to capture the old status.
//
// Addresses vc-n4lx: During dogfood runs, baseline issues were found with
// status='blocked' but there was no audit trail showing when/why this happened.
//
// Log format:
//   [STATUS CHANGE] 2025-11-06T21:30:00Z vc-123 open → blocked (actor: preflight-self-healing, reason: quality gates failed)
//   [BASELINE STATUS] 2025-11-06T21:30:00Z vc-baseline-test open → blocked (actor: quality-gates, reason: 3 test failures)
func (s *VCStorage) LogStatusChange(ctx context.Context, issueID string, newStatus types.Status, actor, reason string) {
	// Fetch current issue to get old status
	issue, err := s.GetIssue(ctx, issueID)
	if err != nil {
		// Issue doesn't exist yet (create operation) or fetch failed
		// Log what we can
		fmt.Fprintf(os.Stderr, "[STATUS CHANGE] %s %s <unknown> → %s (actor: %s, reason: %s)\n",
			time.Now().Format(time.RFC3339),
			issueID,
			newStatus,
			actor,
			reason)
		return
	}

	oldStatus := issue.Status

	// No change? Still log it for audit purposes
	if oldStatus == newStatus {
		fmt.Fprintf(os.Stderr, "[STATUS NO-OP] %s %s %s (actor: %s, reason: %s)\n",
			time.Now().Format(time.RFC3339),
			issueID,
			newStatus,
			actor,
			reason)
		return
	}

	// Check if this is a baseline issue - make it highly visible
	isBaseline := strings.HasPrefix(issueID, "vc-baseline-")

	logPrefix := "[STATUS CHANGE]"
	if isBaseline {
		logPrefix = "🚨 [BASELINE STATUS]"
	}

	// Log to stderr so it's captured in executor logs
	fmt.Fprintf(os.Stderr, "%s %s %s %s → %s (actor: %s, reason: %s)\n",
		logPrefix,
		time.Now().Format(time.RFC3339),
		issueID,
		oldStatus,
		newStatus,
		actor,
		reason)
}

// LogStatusChangeFromUpdates is a convenience wrapper that extracts the status
// from the updates map and calls LogStatusChange if status is present.
//
// Usage:
//   s.LogStatusChangeFromUpdates(ctx, issueID, updates, actor, "quality gates failed")
//   err := s.UpdateIssue(ctx, issueID, updates, actor)
func (s *VCStorage) LogStatusChangeFromUpdates(ctx context.Context, issueID string, updates map[string]interface{}, actor, reason string) {
	// Check if status is in the updates
	statusVal, hasStatus := updates["status"]
	if !hasStatus {
		return // No status change, nothing to log
	}

	// Convert to types.Status
	var newStatus types.Status
	switch v := statusVal.(type) {
	case types.Status:
		newStatus = v
	case string:
		newStatus = types.Status(v)
	default:
		// Unknown status type, log what we can
		fmt.Fprintf(os.Stderr, "[STATUS CHANGE] %s %s <unknown-type:%T> (actor: %s, reason: %s)\n",
			time.Now().Format(time.RFC3339),
			issueID,
			statusVal,
			actor,
			reason)
		return
	}

	s.LogStatusChange(ctx, issueID, newStatus, actor, reason)
}
