package executor

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/google/uuid"
	"github.com/steveyegge/vc/internal/events"
)

// logEvent creates and stores an agent event for observability
func (e *Executor) logEvent(ctx context.Context, eventType events.EventType, severity events.EventSeverity, issueID, message string, data map[string]interface{}) {
	// Skip logging if context is canceled (e.g., during shutdown)
	if ctx.Err() != nil {
		return
	}

	event := &events.AgentEvent{
		ID:         uuid.New().String(),
		Type:       eventType,
		Timestamp:  time.Now(),
		IssueID:    issueID,
		ExecutorID: e.instanceID,
		AgentID:    "", // Empty for executor-level events (not produced by coding agents)
		Severity:   severity,
		Message:    message,
		Data:       data,
		SourceLine: 0, // Not applicable for executor-level events
	}

	if err := e.store.StoreAgentEvent(ctx, event); err != nil {
		// Log error but don't fail execution
		fmt.Fprintf(os.Stderr, "warning: failed to store agent event: %v\n", err)
	}
}

// logCleanupEvent creates and stores a structured event for cleanup metrics (vc-196)
func (e *Executor) logCleanupEvent(ctx context.Context, totalDeleted, timeBasedDeleted, perIssueDeleted, globalLimitDeleted int, processingTimeMs int64, vacuumRan bool, eventsRemaining int, success bool, errorMsg string) {
	// Skip logging if context is canceled (e.g., during shutdown)
	if ctx.Err() != nil {
		return
	}

	data := map[string]interface{}{
		"events_deleted":       totalDeleted,
		"time_based_deleted":   timeBasedDeleted,
		"per_issue_deleted":    perIssueDeleted,
		"global_limit_deleted": globalLimitDeleted,
		"processing_time_ms":   processingTimeMs,
		"vacuum_ran":           vacuumRan,
		"events_remaining":     eventsRemaining,
		"success":              success,
	}

	if errorMsg != "" {
		data["error"] = errorMsg
	}

	message := fmt.Sprintf("Event cleanup completed: deleted %d events in %dms", totalDeleted, processingTimeMs)
	if !success {
		message = fmt.Sprintf("Event cleanup failed: %s", errorMsg)
	}

	event := &events.AgentEvent{
		ID:         uuid.New().String(),
		Type:       events.EventTypeEventCleanupCompleted,
		Timestamp:  time.Now(),
		IssueID:    "SYSTEM", // System-level event (requires SYSTEM pseudo-issue to exist)
		ExecutorID: e.instanceID,
		AgentID:    "", // Not produced by a coding agent
		Severity:   events.SeverityInfo,
		Message:    message,
		Data:       data,
		SourceLine: 0, // Not applicable for executor-level events
	}

	if !success {
		event.Severity = events.SeverityError
	}

	if err := e.store.StoreAgentEvent(ctx, event); err != nil {
		// Log error but don't fail cleanup
		fmt.Fprintf(os.Stderr, "warning: failed to store cleanup event: %v\n", err)
	}
}

// logInstanceCleanupEvent creates and stores a structured event for instance cleanup metrics (vc-32)
// This follows the same pattern as logCleanupEvent for consistency.
func (e *Executor) logInstanceCleanupEvent(ctx context.Context, instancesDeleted, instancesRemaining int, processingTimeMs int64, cleanupAgeSeconds, maxToKeep int, success bool, errorMsg string) {
	// Skip logging if context is canceled (e.g., during shutdown)
	if ctx.Err() != nil {
		return
	}

	data := map[string]interface{}{
		"instances_deleted":   instancesDeleted,
		"instances_remaining": instancesRemaining,
		"processing_time_ms":  processingTimeMs,
		"cleanup_age_seconds": cleanupAgeSeconds,
		"max_to_keep":         maxToKeep,
		"success":             success,
	}

	if errorMsg != "" {
		data["error"] = errorMsg
	}

	message := fmt.Sprintf("Instance cleanup completed: deleted %d stopped instances in %dms", instancesDeleted, processingTimeMs)
	if !success {
		message = fmt.Sprintf("Instance cleanup failed: %s", errorMsg)
	}

	event := &events.AgentEvent{
		ID:         uuid.New().String(),
		Type:       events.EventTypeInstanceCleanupCompleted,
		Timestamp:  time.Now(),
		IssueID:    "SYSTEM", // System-level event (requires SYSTEM pseudo-issue to exist)
		ExecutorID: e.instanceID,
		AgentID:    "", // Not produced by a coding agent
		Severity:   events.SeverityInfo,
		Message:    message,
		Data:       data,
		SourceLine: 0, // Not applicable for executor-level events
	}

	if !success {
		event.Severity = events.SeverityError
	}

	if err := e.store.StoreAgentEvent(ctx, event); err != nil {
		// Log error but don't fail cleanup
		fmt.Fprintf(os.Stderr, "warning: failed to store instance cleanup event: %v\n", err)
	}
}
