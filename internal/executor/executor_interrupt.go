package executor

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sync/atomic"
	"time"

	"github.com/steveyegge/vc/internal/events"
	"github.com/steveyegge/vc/internal/types"
)

// InterruptManager manages task interruption and resume for the executor
type InterruptManager struct {
	executor      *Executor
	interruptFlag atomic.Bool // Set to true when interrupt is requested
	currentIssue  atomic.Value  // *types.Issue - currently executing issue
}

// NewInterruptManager creates a new interrupt manager
func NewInterruptManager(e *Executor) *InterruptManager {
	return &InterruptManager{
		executor: e,
	}
}

// SetCurrentIssue sets the currently executing issue
func (im *InterruptManager) SetCurrentIssue(issue *types.Issue) {
	if issue == nil {
		im.currentIssue.Store((*types.Issue)(nil))
	} else {
		im.currentIssue.Store(issue)
	}
}

// GetCurrentIssue returns the currently executing issue
func (im *InterruptManager) GetCurrentIssue() *types.Issue {
	val := im.currentIssue.Load()
	if val == nil {
		return nil
	}
	return val.(*types.Issue)
}

// RequestInterrupt signals that the current task should be interrupted
func (im *InterruptManager) RequestInterrupt() {
	im.interruptFlag.Store(true)
}

// ClearInterrupt clears the interrupt flag
func (im *InterruptManager) ClearInterrupt() {
	im.interruptFlag.Store(false)
}

// IsInterruptRequested checks if an interrupt has been requested
func (im *InterruptManager) IsInterruptRequested() bool {
	return im.interruptFlag.Load()
}

// HandlePauseCommand handles a pause request from the control CLI
// Returns response data and error
func (im *InterruptManager) HandlePauseCommand(ctx context.Context, issueID string, reason string) (map[string]interface{}, error) {
	// Check if this issue is currently executing
	currentIssue := im.GetCurrentIssue()
	if currentIssue == nil {
		return nil, fmt.Errorf("no task currently executing")
	}

	if currentIssue.ID != issueID {
		return nil, fmt.Errorf("issue %s is not currently executing (current: %s)", issueID, currentIssue.ID)
	}

	// Request interrupt
	im.RequestInterrupt()

	// Log the pause request
	im.executor.logEvent(ctx, events.EventTypeProgress, events.SeverityInfo, issueID,
		fmt.Sprintf("Pause requested for %s: %s", issueID, reason),
		map[string]interface{}{
			"reason": reason,
			"timestamp": time.Now().Format(time.RFC3339),
		})

	fmt.Printf("⏸️  Pause requested for %s (reason: %s)\n", issueID, reason)
	fmt.Printf("   Waiting for agent to reach safe checkpoint...\n")

	// Return response data
	return map[string]interface{}{
		"issue_id":     issueID,
		"requested_at": time.Now().Format(time.RFC3339),
		"reason":       reason,
		"status":       "interrupt_requested",
	}, nil
}

// SaveInterruptContext saves the current agent context for resume
func (im *InterruptManager) SaveInterruptContext(ctx context.Context, issue *types.Issue, interruptedBy string, reason string, executionState string) error {
	// Check for existing interrupt metadata to preserve resume count
	existingMetadata, err := im.executor.store.GetInterruptMetadata(ctx, issue.ID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: failed to get existing interrupt metadata: %v\n", err)
	}

	resumeCount := 0
	if existingMetadata != nil {
		// Preserve existing resume count when re-pausing
		resumeCount = existingMetadata.ResumeCount
	}

	// Build interrupt metadata
	metadata := &types.InterruptMetadata{
		IssueID:            issue.ID,
		InterruptedAt:      time.Now(),
		InterruptedBy:      interruptedBy,
		Reason:             reason,
		ExecutorInstanceID: im.executor.instanceID,
		ExecutionState:     executionState,
		ResumeCount:        resumeCount,
	}

	// TODO: Extract agent context from running agent
	// For now, we'll save basic metadata
	// In a full implementation, we would:
	// 1. Get agent's current todo list state
	// 2. Get agent's working notes
	// 3. Get last tool used
	// 4. Build full context snapshot

	// Create basic context
	agentContext := types.AgentContext{
		InterruptedAt:  time.Now(),
		WorkingNotes:   fmt.Sprintf("Task interrupted at %s", executionState),
		CurrentPhase:   executionState,
	}

	// Serialize context
	contextJSON, err := json.Marshal(agentContext)
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: failed to serialize agent context: %v\n", err)
	} else {
		metadata.ContextSnapshot = string(contextJSON)
	}

	// Save to database
	if err := im.executor.store.SaveInterruptMetadata(ctx, metadata); err != nil {
		return fmt.Errorf("failed to save interrupt metadata: %w", err)
	}

	// Add 'interrupted' label to issue
	if err := im.executor.store.AddLabel(ctx, issue.ID, "interrupted", "executor"); err != nil {
		fmt.Fprintf(os.Stderr, "warning: failed to add interrupted label: %v\n", err)
	}

	// Log event
	im.executor.logEvent(ctx, events.EventTypeProgress, events.SeverityInfo, issue.ID,
		fmt.Sprintf("Saved interrupt context for %s", issue.ID),
		map[string]interface{}{
			"interrupted_by": interruptedBy,
			"reason":         reason,
			"state":          executionState,
		})

	fmt.Printf("✓ Saved interrupt context for %s\n", issue.ID)

	return nil
}

// CheckAndLoadInterruptContext checks if an issue has interrupt metadata and returns resume context
func (im *InterruptManager) CheckAndLoadInterruptContext(ctx context.Context, issueID string) (string, error) {
	// Check for interrupt metadata
	metadata, err := im.executor.store.GetInterruptMetadata(ctx, issueID)
	if err != nil {
		return "", fmt.Errorf("failed to get interrupt metadata: %w", err)
	}

	if metadata == nil {
		// No interrupt metadata - this is a fresh start
		return "", nil
	}

	// Build resume context brief for agent
	resumeBrief := buildAgentResumeContext(metadata)

	// Mark as resumed in database
	if err := im.executor.store.MarkInterruptResumed(ctx, issueID); err != nil {
		fmt.Fprintf(os.Stderr, "warning: failed to mark interrupt as resumed: %v\n", err)
	}

	// Remove 'interrupted' label
	if err := im.executor.store.RemoveLabel(ctx, issueID, "interrupted", "executor"); err != nil {
		fmt.Fprintf(os.Stderr, "warning: failed to remove interrupted label: %v\n", err)
	}

	// Log resume event
	im.executor.logEvent(ctx, events.EventTypeProgress, events.SeverityInfo, issueID,
		fmt.Sprintf("Resuming %s from interrupted state", issueID),
		map[string]interface{}{
			"interrupted_at": metadata.InterruptedAt.Format(time.RFC3339),
			"resume_count":   metadata.ResumeCount + 1,
		})

	fmt.Printf("🔄 Resuming %s from interrupted state (interrupted %s ago)\n",
		issueID, time.Since(metadata.InterruptedAt).Round(time.Second))

	return resumeBrief, nil
}

// buildAgentResumeContext creates a resume brief for the agent
func buildAgentResumeContext(metadata *types.InterruptMetadata) string {
	if metadata == nil {
		return ""
	}

	// Parse context snapshot if available
	var context types.AgentContext
	if metadata.ContextSnapshot != "" {
		if err := json.Unmarshal([]byte(metadata.ContextSnapshot), &context); err == nil {
			return buildFullResumeContext(metadata, &context)
		}
	}

	// Fall back to basic context
	return buildBasicResumeContext(metadata)
}

func buildFullResumeContext(metadata *types.InterruptMetadata, context *types.AgentContext) string {
	brief := fmt.Sprintf(`**Task Resumed from Interrupt**

This task was interrupted at %s.

**Reason**: %s
**Interrupted by**: %s
**Execution phase**: %s

`, metadata.InterruptedAt.Format("2006-01-02 15:04:05"),
		metadata.Reason,
		metadata.InterruptedBy,
		metadata.ExecutionState)

	if context.WorkingNotes != "" {
		brief += fmt.Sprintf("**Your working notes**:\n%s\n\n", context.WorkingNotes)
	}

	if context.ProgressSummary != "" {
		brief += fmt.Sprintf("**Progress so far**: %s\n\n", context.ProgressSummary)
	}

	brief += "**Please continue from where you left off.**\n"

	return brief
}

func buildBasicResumeContext(metadata *types.InterruptMetadata) string {
	return fmt.Sprintf(`**Task Resumed from Interrupt**

This task was interrupted at %s.

**Reason**: %s
**Interrupted by**: %s
**Execution phase**: %s

**Please assess the current state and continue working on this task.**
`, metadata.InterruptedAt.Format("2006-01-02 15:04:05"),
		metadata.Reason,
		metadata.InterruptedBy,
		metadata.ExecutionState)
}
