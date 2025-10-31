package executor

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/steveyegge/vc/internal/ai"
	"github.com/steveyegge/vc/internal/events"
	"github.com/steveyegge/vc/internal/labels"
	"github.com/steveyegge/vc/internal/sandbox"
	"github.com/steveyegge/vc/internal/storage"
	"github.com/steveyegge/vc/internal/types"
)

// checkEpicCompletion checks if an issue's parent epic is now complete
// Uses AI to assess completion based on objectives, not just counting closed children
// If the epic is closed and is a mission, automatically cleans up the mission sandbox (vc-245)
func checkEpicCompletion(ctx context.Context, store storage.Storage, supervisor *ai.Supervisor, sandboxMgr sandbox.Manager, issueID string) error {
	// Get the issue to check its parent
	issue, err := store.GetIssue(ctx, issueID)
	if err != nil {
		return fmt.Errorf("failed to get issue: %w", err)
	}
	if issue == nil {
		return fmt.Errorf("issue not found: %s", issueID)
	}

	// Get parent issues (issues this one depends on via parent-child relationship)
	deps, err := store.GetDependencies(ctx, issueID)
	if err != nil {
		return fmt.Errorf("failed to get dependencies: %w", err)
	}

	// Find parent epic(s)
	for _, dep := range deps {
		if dep.IssueType == types.TypeEpic {
			// Check if all children of this epic are closed
			closed, err := checkAndCloseEpicIfComplete(ctx, store, supervisor, dep.ID)
			if err != nil {
				// Log but don't fail - this is a best-effort check
				fmt.Printf("Warning: failed to check epic completion for %s: %v\n", dep.ID, err)
				continue
			}

			// If epic was closed, check if it's a mission and clean up sandbox (vc-245)
			if closed && sandboxMgr != nil {
				if err := cleanupMissionSandboxIfComplete(ctx, store, sandboxMgr, dep.ID); err != nil {
					// Log but don't fail - this is best-effort
					fmt.Printf("Warning: failed to cleanup mission sandbox for %s: %v\n", dep.ID, err)
				}
			}
		}
	}

	return nil
}

// checkAndCloseEpicIfComplete checks if an epic is complete and closes it if so
// Uses AI assessment instead of hardcoded "all children closed" logic (ZFC compliance)
// Returns (closed bool, error) indicating whether the epic was closed
func checkAndCloseEpicIfComplete(ctx context.Context, store storage.Storage, supervisor *ai.Supervisor, epicID string) (bool, error) {
	// Get the epic
	epic, err := store.GetIssue(ctx, epicID)
	if err != nil {
		return false, fmt.Errorf("failed to get epic: %w", err)
	}
	if epic == nil {
		return false, fmt.Errorf("epic not found: %s", epicID)
	}

	// Skip if already closed
	if epic.Status == types.StatusClosed {
		return false, nil // Already closed, not closed by us
	}

	// Get all issues that depend on this epic (its children)
	children, err := store.GetDependents(ctx, epicID)
	if err != nil {
		return false, fmt.Errorf("failed to get epic children: %w", err)
	}

	// If no children, don't auto-close (epics should have children)
	if len(children) == 0 {
		return false, nil
	}

	// Use AI to assess completion if supervisor is available
	if supervisor != nil {
		assessment, err := supervisor.AssessCompletion(ctx, epic, children)
		if err != nil {
			// If AI assessment fails, log but don't fail the check
			// This maintains backward compatibility if AI is unavailable
			fmt.Printf("Warning: AI completion assessment failed for %s: %v (skipping auto-close)\n", epicID, err)
			return false, nil
		}

		// Log assessment reasoning
		reasoningComment := fmt.Sprintf("**AI Completion Assessment**\n\n"+
			"Should Close: %v\n"+
			"Confidence: %.2f\n\n"+
			"Reasoning: %s\n",
			assessment.ShouldClose, assessment.Confidence, assessment.Reasoning)

		if len(assessment.Caveats) > 0 {
			reasoningComment += "\nCaveats:\n"
			for _, caveat := range assessment.Caveats {
				reasoningComment += fmt.Sprintf("- %s\n", caveat)
			}
		}

		if err := store.AddComment(ctx, epicID, "ai-supervisor", reasoningComment); err != nil {
			fmt.Printf("Warning: failed to add AI assessment comment: %v\n", err)
		}

		// Close epic if AI recommends it
		if assessment.ShouldClose {
			fmt.Printf("AI recommends closing epic %s (confidence: %.2f)\n", epicID, assessment.Confidence)

			reason := fmt.Sprintf("AI assessment: objectives met (confidence: %.2f)", assessment.Confidence)
			if err := store.CloseIssue(ctx, epicID, reason, "ai-supervisor"); err != nil {
				return false, fmt.Errorf("failed to close epic: %w", err)
			}

			fmt.Printf("✓ Closed epic %s: %s\n", epicID, epic.Title)

			// vc-268: Emit epic_completed event
			eventData := map[string]interface{}{
				"epic_id":            epicID,
				"epic_title":         epic.Title,
				"children_completed": len(children),
				"completion_method":  "ai_assessment",
				"confidence":         assessment.Confidence,
				"is_mission":         epic.IssueSubtype == types.SubtypeMission,
				"actor":              "ai-supervisor",
			}
			message := fmt.Sprintf("Epic %s completed: %s (AI assessment, confidence: %.2f)", epicID, epic.Title, assessment.Confidence)
			logEpicEvent(ctx, store, events.EventTypeEpicCompleted, events.SeverityInfo, epicID, "executor", message, eventData)

			// vc-218: If this is a mission epic, transition to needs-quality-gates state
			if epic.IssueSubtype == types.SubtypeMission {
				if err := labels.TransitionState(ctx, store, epicID, "", labels.LabelNeedsQualityGates, labels.TriggerEpicCompleted, "ai-supervisor"); err != nil {
					// Log warning but don't fail - state transition is best-effort
					fmt.Printf("Warning: failed to transition mission %s to needs-quality-gates: %v\n", epicID, err)
				} else {
					fmt.Printf("✓ Mission %s transitioned to needs-quality-gates state\n", epicID)
				}
			}

			return true, nil // Successfully closed
		} else {
			fmt.Printf("AI recommends keeping epic %s open: %s\n", epicID, assessment.Reasoning)
		}

		return false, nil
	}

	// Fallback: No AI supervisor available, use simple heuristic
	// (This path should rarely be taken in production)
	fmt.Printf("Warning: No AI supervisor available for epic %s, using fallback logic\n", epicID)

	// Check if all children are closed
	allClosed := true
	for _, child := range children {
		if child.Status != types.StatusClosed {
			allClosed = false
			break
		}
	}

	// If all children are closed, close the epic
	if allClosed {
		fmt.Printf("All children of epic %s are complete, closing epic\n", epicID)

		reason := fmt.Sprintf("All %d child issues completed (fallback logic)", len(children))
		if err := store.CloseIssue(ctx, epicID, reason, "executor"); err != nil {
			return false, fmt.Errorf("failed to close epic: %w", err)
		}

		fmt.Printf("✓ Closed epic %s: %s\n", epicID, epic.Title)

		// vc-268: Emit epic_completed event
		eventData := map[string]interface{}{
			"epic_id":            epicID,
			"epic_title":         epic.Title,
			"children_completed": len(children),
			"completion_method":  "all_children_closed",
			"confidence":         1.0, // Fallback logic is deterministic
			"is_mission":         epic.IssueSubtype == types.SubtypeMission,
			"actor":              "executor",
		}
		message := fmt.Sprintf("Epic %s completed: %s (all %d children closed)", epicID, epic.Title, len(children))
		logEpicEvent(ctx, store, events.EventTypeEpicCompleted, events.SeverityInfo, epicID, "executor", message, eventData)

		// vc-218: If this is a mission epic, transition to needs-quality-gates state
		if epic.IssueSubtype == types.SubtypeMission {
			if err := labels.TransitionState(ctx, store, epicID, "", labels.LabelNeedsQualityGates, labels.TriggerEpicCompleted, "executor"); err != nil {
				// Log warning but don't fail - state transition is best-effort
				fmt.Printf("Warning: failed to transition mission %s to needs-quality-gates: %v\n", epicID, err)
			} else {
				fmt.Printf("✓ Mission %s transitioned to needs-quality-gates state\n", epicID)
			}
		}

		return true, nil // Successfully closed
	}

	return false, nil
}

// cleanupMissionSandboxIfComplete checks if a closed epic is a mission and cleans up its sandbox
// This is called after checkAndCloseEpicIfComplete successfully closes an epic
func cleanupMissionSandboxIfComplete(ctx context.Context, store storage.Storage, sandboxMgr sandbox.Manager, epicID string) error {
	// Check if this epic has mission metadata (is a mission epic)
	mission, err := store.GetMission(ctx, epicID)
	if err != nil {
		// If error indicates "not a mission", that's fine - this epic is not a mission
		if strings.Contains(err.Error(), "is not a mission") || strings.Contains(err.Error(), "not found") {
			return nil
		}
		return fmt.Errorf("failed to check if epic is a mission: %w", err)
	}
	if mission == nil {
		// Not a mission epic
		return nil
	}

	// This is a mission epic - clean up its sandbox
	fmt.Printf("Mission %s completed - cleaning up sandbox\n", epicID)

	// vc-268: Emit epic_cleanup_started event
	startEventData := map[string]interface{}{
		"epic_id":      epicID,
		"is_mission":   true,
		"sandbox_path": mission.SandboxPath,
	}
	logEpicEvent(ctx, store, events.EventTypeEpicCleanupStarted, events.SeverityInfo, epicID, "executor",
		fmt.Sprintf("Starting cleanup for mission epic %s", epicID), startEventData)

	startTime := time.Now()
	cleanupErr := sandbox.CleanupMissionSandbox(ctx, sandboxMgr, store, epicID)
	duration := time.Since(startTime)

	// vc-268: Emit epic_cleanup_completed event
	completeEventData := map[string]interface{}{
		"epic_id":      epicID,
		"is_mission":   true,
		"sandbox_path": mission.SandboxPath,
		"success":      cleanupErr == nil,
		"duration_ms":  duration.Milliseconds(),
	}
	if cleanupErr != nil {
		completeEventData["error"] = cleanupErr.Error()
	}

	message := fmt.Sprintf("Completed cleanup for mission epic %s (duration: %v)", epicID, duration)
	severity := events.SeverityInfo
	if cleanupErr != nil {
		message = fmt.Sprintf("Cleanup failed for mission epic %s: %v", epicID, cleanupErr)
		severity = events.SeverityWarning
		// Log but don't fail - sandbox cleanup is best-effort
		fmt.Printf("Warning: failed to cleanup mission sandbox for %s: %v\n", epicID, cleanupErr)
	} else {
		fmt.Printf("✓ Cleaned up sandbox for mission %s\n", epicID)
	}

	logEpicEvent(ctx, store, events.EventTypeEpicCleanupCompleted, severity, epicID, "executor", message, completeEventData)

	return nil // Always return nil since cleanup is best-effort
}

// logEpicEvent creates and stores an epic lifecycle event (vc-268)
// This follows the same pattern as logEvent in executor_events.go but doesn't require an Executor instance
func logEpicEvent(ctx context.Context, store storage.Storage, eventType events.EventType, severity events.EventSeverity, issueID, executorID, message string, data map[string]interface{}) {
	// Skip logging if context is canceled (e.g., during shutdown)
	if ctx.Err() != nil {
		return
	}

	if data == nil {
		data = make(map[string]interface{})
	}

	event := &events.AgentEvent{
		ID:         uuid.New().String(),
		Type:       eventType,
		Timestamp:  time.Now(),
		IssueID:    issueID,
		ExecutorID: executorID,
		AgentID:    "", // Empty for executor-level epic events
		Severity:   severity,
		Message:    message,
		Data:       data,
		SourceLine: 0, // Not applicable for executor-level events
	}

	if err := store.StoreAgentEvent(ctx, event); err != nil {
		// Log error but don't fail execution
		fmt.Printf("Warning: failed to store epic event: %v\n", err)
	}
}
