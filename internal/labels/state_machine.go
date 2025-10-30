// Package labels provides label-driven state machine utilities (vc-218).
//
// State Flow:
// - task-ready → Code Workers claim
// - needs-quality-gates → QA Workers claim
// - needs-review → GitOps Arbiter claims
// - needs-human-approval → Human Approvers claim
// - approved → GitOps Merger claims
package labels

import (
	"context"
	"fmt"
	"time"

	"github.com/steveyegge/vc/internal/events"
)

// State labels used in the mission workflow (vc-218)
const (
	// LabelTaskReady indicates a task is ready for code workers to claim
	LabelTaskReady = "task-ready"
	// LabelNeedsQualityGates indicates a mission needs quality gates to run
	LabelNeedsQualityGates = "needs-quality-gates"
	// LabelGatesRunning indicates quality gates are currently running (claim lock for QA workers)
	LabelGatesRunning = "gates-running"
	// LabelGatesFailed indicates quality gates failed (prevents re-claiming until fixed)
	LabelGatesFailed = "gates-failed"
	// LabelNeedsReview indicates a mission needs GitOps Arbiter review
	LabelNeedsReview = "needs-review"
	// LabelNeedsHumanApproval indicates a mission needs human approval
	LabelNeedsHumanApproval = "needs-human-approval"
	// LabelApproved indicates a mission has been approved for merging
	LabelApproved = "approved"
)

// Trigger types for state transitions (vc-218)
const (
	// TriggerTaskCompleted indicates a task was completed
	TriggerTaskCompleted = "task_completed"
	// TriggerEpicCompleted indicates an epic was completed
	TriggerEpicCompleted = "epic_completed"
	// TriggerGatesPassed indicates quality gates passed
	TriggerGatesPassed = "gates_passed"
	// TriggerReviewCompleted indicates arbiter review completed
	TriggerReviewCompleted = "review_completed"
	// TriggerHumanApproval indicates human approved the mission
	TriggerHumanApproval = "human_approval"
)

// Storage interface for label operations (subset of storage.Storage)
// This allows the state machine to work with minimal dependencies
type Storage interface {
	AddLabel(ctx context.Context, issueID, label, actor string) error
	RemoveLabel(ctx context.Context, issueID, label, actor string) error
	GetLabels(ctx context.Context, issueID string) ([]string, error)
	StoreAgentEvent(ctx context.Context, event *events.AgentEvent) error
}

// TransitionState transitions an issue from one state label to another.
// It removes the old label (if provided), adds the new label, and logs the transition.
//
// Parameters:
//   - ctx: Context for the operation
//   - store: Storage backend for label and event operations
//   - issueID: ID of the issue to transition
//   - fromLabel: Current state label (empty string if initial state)
//   - toLabel: New state label
//   - trigger: What triggered the transition (e.g., "task_completed")
//   - actor: Who/what initiated the transition (e.g., executor ID)
//
// Returns error if label operations or event logging fail.
func TransitionState(ctx context.Context, store Storage, issueID, fromLabel, toLabel, trigger, actor string) error {
	// Remove old label if specified
	if fromLabel != "" {
		if err := store.RemoveLabel(ctx, issueID, fromLabel, actor); err != nil {
			return fmt.Errorf("failed to remove label %s: %w", fromLabel, err)
		}
	}

	// Add new label
	if err := store.AddLabel(ctx, issueID, toLabel, actor); err != nil {
		return fmt.Errorf("failed to add label %s: %w", toLabel, err)
	}

	// Log the transition to agent_events for monitoring
	event := &events.AgentEvent{
		Type:      events.EventTypeLabelStateTransition,
		Timestamp: time.Now(),
		IssueID:   issueID,
		Severity:  events.SeverityInfo,
		Message:   fmt.Sprintf("State transition: %s → %s (trigger: %s)", fromLabel, toLabel, trigger),
		Data: map[string]interface{}{
			"from_label": fromLabel,
			"to_label":   toLabel,
			"trigger":    trigger,
			"actor":      actor,
		},
	}

	if err := store.StoreAgentEvent(ctx, event); err != nil {
		// Don't fail the operation if event logging fails, just log warning
		// The label transition itself succeeded
		return fmt.Errorf("warning: state transition succeeded but failed to log event: %w", err)
	}

	return nil
}

// HasLabel checks if an issue has a specific label.
func HasLabel(ctx context.Context, store Storage, issueID, label string) (bool, error) {
	labels, err := store.GetLabels(ctx, issueID)
	if err != nil {
		return false, fmt.Errorf("failed to get labels: %w", err)
	}

	for _, l := range labels {
		if l == label {
			return true, nil
		}
	}

	return false, nil
}

// GetStateLabel returns the current state label for an issue.
// Returns empty string if no state label is present.
func GetStateLabel(ctx context.Context, store Storage, issueID string) (string, error) {
	labels, err := store.GetLabels(ctx, issueID)
	if err != nil {
		return "", fmt.Errorf("failed to get labels: %w", err)
	}

	// Check for state labels in priority order
	stateLabels := []string{
		LabelApproved,
		LabelNeedsHumanApproval,
		LabelNeedsReview,
		LabelNeedsQualityGates,
		LabelTaskReady,
	}

	for _, stateLabel := range stateLabels {
		for _, l := range labels {
			if l == stateLabel {
				return stateLabel, nil
			}
		}
	}

	return "", nil
}
