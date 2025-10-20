package watchdog

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/steveyegge/vc/internal/storage"
	"github.com/steveyegge/vc/internal/types"
)

// InterventionType categorizes the type of intervention taken
type InterventionType string

const (
	InterventionPauseAgent       InterventionType = "pause_agent"
	InterventionKillAgent        InterventionType = "kill_agent"
	InterventionPauseExecutor    InterventionType = "pause_executor"
	InterventionRequestCheckpoint InterventionType = "request_checkpoint"
)

// InterventionResult represents the outcome of an intervention
type InterventionResult struct {
	// Success indicates whether the intervention was successful
	Success bool
	// InterventionType indicates what action was taken
	InterventionType InterventionType
	// AnomalyReport contains the anomaly that triggered intervention
	AnomalyReport *AnomalyReport
	// EscalationIssueID is the ID of the issue created for human review
	EscalationIssueID string
	// Message provides details about what happened
	Message string
	// Timestamp is when the intervention occurred
	Timestamp time.Time
}

// InterventionController manages watchdog interventions when anomalies are detected
// It can pause/kill agents and manage executor state
type InterventionController struct {
	mu    sync.RWMutex
	store storage.Storage

	// cancelFunc is the cancel function for the current agent execution
	// When an intervention occurs, this function is called to stop the agent
	cancelFunc context.CancelFunc

	// currentIssueID tracks which issue is currently being executed
	currentIssueID string

	// executorInstanceID identifies this executor instance
	executorInstanceID string

	// interventionHistory tracks recent interventions for reporting
	interventionHistory []InterventionResult
	maxHistorySize      int
}

// InterventionControllerConfig holds configuration for the intervention controller
type InterventionControllerConfig struct {
	Store              storage.Storage
	ExecutorInstanceID string
	MaxHistorySize     int // Maximum number of interventions to keep in memory (default: 100)
}

// NewInterventionController creates a new intervention controller
func NewInterventionController(cfg *InterventionControllerConfig) (*InterventionController, error) {
	if cfg.Store == nil {
		return nil, fmt.Errorf("store is required")
	}
	if cfg.ExecutorInstanceID == "" {
		return nil, fmt.Errorf("executor_instance_id is required")
	}

	maxHistorySize := cfg.MaxHistorySize
	if maxHistorySize <= 0 {
		maxHistorySize = 100
	}

	return &InterventionController{
		store:               cfg.Store,
		executorInstanceID:  cfg.ExecutorInstanceID,
		interventionHistory: make([]InterventionResult, 0, maxHistorySize),
		maxHistorySize:      maxHistorySize,
	}, nil
}

// SetAgentContext registers the cancel function for the current agent execution
// This should be called when an agent starts executing an issue
// The cancelFunc will be invoked if intervention is needed
func (ic *InterventionController) SetAgentContext(issueID string, cancelFunc context.CancelFunc) {
	ic.mu.Lock()
	defer ic.mu.Unlock()

	ic.currentIssueID = issueID
	ic.cancelFunc = cancelFunc
}

// ClearAgentContext clears the current agent context
// This should be called when an agent completes execution (success or failure)
func (ic *InterventionController) ClearAgentContext() {
	ic.mu.Lock()
	defer ic.mu.Unlock()

	ic.currentIssueID = ""
	ic.cancelFunc = nil
}

// PauseAgent pauses the currently executing agent by canceling its context
// This triggers a graceful shutdown where the agent should clean up and stop
func (ic *InterventionController) PauseAgent(ctx context.Context, report *AnomalyReport) (*InterventionResult, error) {
	ic.mu.Lock()
	defer ic.mu.Unlock()

	if ic.cancelFunc == nil {
		return nil, fmt.Errorf("no active agent to pause")
	}

	result := &InterventionResult{
		Success:          true,
		InterventionType: InterventionPauseAgent,
		AnomalyReport:    report,
		Message:          fmt.Sprintf("Paused agent executing %s due to %s anomaly", ic.currentIssueID, report.AnomalyType),
		Timestamp:        time.Now(),
	}

	// Cancel the agent's context to trigger graceful shutdown
	ic.cancelFunc()

	// Create escalation issue for human review
	// Pass currentIssueID to avoid reading without lock
	escalationID, err := ic.createEscalationIssue(ctx, report, InterventionPauseAgent, ic.currentIssueID)
	if err != nil {
		result.Message += fmt.Sprintf(" (warning: failed to create escalation issue: %v)", err)
	} else {
		result.EscalationIssueID = escalationID
	}

	// Emit watchdog event
	if err := ic.emitWatchdogEvent(ctx, result, ic.currentIssueID); err != nil {
		result.Message += fmt.Sprintf(" (warning: failed to emit event: %v)", err)
	}

	// Add to history
	ic.addToHistoryLocked(result)

	fmt.Printf("Watchdog: Paused agent for issue %s (escalation: %s)\n", ic.currentIssueID, escalationID)

	return result, nil
}

// KillAgent immediately kills the currently executing agent by canceling its context
// This is a more aggressive intervention than PauseAgent
func (ic *InterventionController) KillAgent(ctx context.Context, report *AnomalyReport) (*InterventionResult, error) {
	ic.mu.Lock()
	defer ic.mu.Unlock()

	if ic.cancelFunc == nil {
		return nil, fmt.Errorf("no active agent to kill")
	}

	result := &InterventionResult{
		Success:          true,
		InterventionType: InterventionKillAgent,
		AnomalyReport:    report,
		Message:          fmt.Sprintf("Killed agent executing %s due to %s anomaly (severity: %s)", ic.currentIssueID, report.AnomalyType, report.Severity),
		Timestamp:        time.Now(),
	}

	// Cancel the agent's context immediately
	ic.cancelFunc()

	// Create escalation issue for human review
	// Pass currentIssueID to avoid reading without lock
	escalationID, err := ic.createEscalationIssue(ctx, report, InterventionKillAgent, ic.currentIssueID)
	if err != nil {
		result.Message += fmt.Sprintf(" (warning: failed to create escalation issue: %v)", err)
	} else {
		result.EscalationIssueID = escalationID
	}

	// Emit watchdog event
	if err := ic.emitWatchdogEvent(ctx, result, ic.currentIssueID); err != nil {
		result.Message += fmt.Sprintf(" (warning: failed to emit event: %v)", err)
	}

	// Add to history
	ic.addToHistoryLocked(result)

	fmt.Printf("Watchdog: Killed agent for issue %s (escalation: %s)\n", ic.currentIssueID, escalationID)

	return result, nil
}

// PauseExecutor creates an escalation issue for critical system-wide issues
//
// NOTE: This method does NOT actually pause the executor - it only creates an escalation issue.
// Full executor pause requires coordination with the executor's main loop, which needs:
//   - A signal mechanism (channel, atomic flag, or context cancellation)
//   - Graceful shutdown of the current agent
//   - Cleanup of executor resources
//   - State persistence for resumption
//
// TODO(vc-executor): Implement actual executor pause mechanism
// For now, this creates a high-priority escalation issue that requires human intervention.
func (ic *InterventionController) PauseExecutor(ctx context.Context, report *AnomalyReport) (*InterventionResult, error) {
	ic.mu.Lock()
	defer ic.mu.Unlock()

	result := &InterventionResult{
		Success:          true,
		InterventionType: InterventionPauseExecutor,
		AnomalyReport:    report,
		Message:          fmt.Sprintf("Created escalation for executor %s pause due to %s anomaly (severity: %s)", ic.executorInstanceID, report.AnomalyType, report.Severity),
		Timestamp:        time.Now(),
	}

	// Create escalation issue for human review
	// Pass currentIssueID to avoid reading without lock
	escalationID, err := ic.createEscalationIssue(ctx, report, InterventionPauseExecutor, ic.currentIssueID)
	if err != nil {
		result.Message += fmt.Sprintf(" (warning: failed to create escalation issue: %v)", err)
	} else {
		result.EscalationIssueID = escalationID
	}

	// Emit watchdog event
	if err := ic.emitWatchdogEvent(ctx, result, ic.currentIssueID); err != nil {
		result.Message += fmt.Sprintf(" (warning: failed to emit event: %v)", err)
	}

	// Add to history
	ic.addToHistoryLocked(result)

	// NOTE: This does not actually pause the executor - just creates an escalation issue.
	// The executor implementation needs to check for pause signals/escalations in its main loop.
	// This is intentionally incomplete until the executor infrastructure is built.

	fmt.Printf("Watchdog: Created executor pause escalation %s for %s\n", escalationID, ic.executorInstanceID)

	return result, nil
}

// RequestCheckpoint requests that the agent checkpoint its progress and terminate gracefully
// This is used when context usage is approaching exhaustion (vc-121)
// The agent should:
//  1. Record current progress as issues
//  2. Save checkpoint state for handoff
//  3. Gracefully terminate
// The next worker can then pick up the checkpoint and continue
func (ic *InterventionController) RequestCheckpoint(ctx context.Context, report *AnomalyReport) (*InterventionResult, error) {
	ic.mu.Lock()
	defer ic.mu.Unlock()

	if ic.cancelFunc == nil {
		return nil, fmt.Errorf("no active agent to checkpoint")
	}

	result := &InterventionResult{
		Success:          true,
		InterventionType: InterventionRequestCheckpoint,
		AnomalyReport:    report,
		Message:          fmt.Sprintf("Requested checkpoint for issue %s due to context exhaustion (%.1f%% usage)", ic.currentIssueID, report.Confidence*100),
		Timestamp:        time.Now(),
	}

	// Cancel the agent's context to trigger graceful shutdown
	// The agent should detect this and checkpoint its state
	ic.cancelFunc()

	// Create escalation issue documenting the checkpoint request
	// Pass currentIssueID to avoid reading without lock
	escalationID, err := ic.createEscalationIssue(ctx, report, InterventionRequestCheckpoint, ic.currentIssueID)
	if err != nil {
		result.Message += fmt.Sprintf(" (warning: failed to create escalation issue: %v)", err)
	} else {
		result.EscalationIssueID = escalationID
	}

	// Emit watchdog event
	if err := ic.emitWatchdogEvent(ctx, result, ic.currentIssueID); err != nil {
		result.Message += fmt.Sprintf(" (warning: failed to emit event: %v)", err)
	}

	// Add to history
	ic.addToHistoryLocked(result)

	fmt.Printf("Watchdog: Requested checkpoint for issue %s (escalation: %s)\n", ic.currentIssueID, escalationID)

	return result, nil
}

// notifyHuman creates an escalation issue without stopping execution
// This is used for anomalies that need human attention but aren't critical
func (ic *InterventionController) notifyHuman(ctx context.Context, report *AnomalyReport) (*InterventionResult, error) {
	ic.mu.Lock()
	defer ic.mu.Unlock()

	result := &InterventionResult{
		Success:          true,
		InterventionType: InterventionPauseAgent, // Use pause as the type for notification
		AnomalyReport:    report,
		Message:          fmt.Sprintf("Notified human about %s anomaly in %s", report.AnomalyType, ic.currentIssueID),
		Timestamp:        time.Now(),
	}

	currentIssueID := ic.currentIssueID
	escalationID, err := ic.createEscalationIssue(ctx, report, InterventionPauseAgent, currentIssueID)
	if err != nil {
		result.Success = false
		result.Message = fmt.Sprintf("Failed to create escalation issue: %v", err)
		return result, err
	}
	result.EscalationIssueID = escalationID

	if err := ic.emitWatchdogEvent(ctx, result, currentIssueID); err != nil {
		result.Message += fmt.Sprintf(" (warning: failed to emit event: %v)", err)
	}

	ic.addToHistoryLocked(result)
	return result, nil
}

// flagForInvestigation creates an escalation issue for lower-priority anomalies
// These are logged for investigation but don't require immediate intervention
func (ic *InterventionController) flagForInvestigation(ctx context.Context, report *AnomalyReport) (*InterventionResult, error) {
	ic.mu.Lock()
	defer ic.mu.Unlock()

	result := &InterventionResult{
		Success:          true,
		InterventionType: InterventionPauseAgent,
		AnomalyReport:    report,
		Message:          fmt.Sprintf("Flagged %s anomaly for investigation", report.AnomalyType),
		Timestamp:        time.Now(),
	}

	currentIssueID := ic.currentIssueID
	escalationID, err := ic.createEscalationIssue(ctx, report, InterventionPauseAgent, currentIssueID)
	if err != nil {
		result.Success = false
		result.Message = fmt.Sprintf("Failed to create escalation issue: %v", err)
		return result, err
	}
	result.EscalationIssueID = escalationID

	if err := ic.emitWatchdogEvent(ctx, result, currentIssueID); err != nil {
		result.Message += fmt.Sprintf(" (warning: failed to emit event: %v)", err)
	}

	ic.addToHistoryLocked(result)
	return result, nil
}

// Intervene analyzes an anomaly report and decides what intervention to take
// This delegates the intervention decision to AI (ZFC compliant)
func (ic *InterventionController) Intervene(ctx context.Context, report *AnomalyReport) (*InterventionResult, error) {
	if !report.Detected {
		return nil, fmt.Errorf("no anomaly detected, intervention not needed")
	}

	// The AnomalyReport already contains a RecommendedAction from the AI
	// We follow that recommendation
	switch report.RecommendedAction {
	case ActionStopExecution:
		// Stop execution = kill the agent
		return ic.KillAgent(ctx, report)
	case ActionRestartAgent:
		// Restart = pause (kill) the agent, it will be restarted by the executor
		return ic.PauseAgent(ctx, report)
	case ActionMarkAsBlocked:
		// Mark as blocked = pause agent and create escalation issue
		// The escalation issue will be marked as blocked for human review
		return ic.PauseAgent(ctx, report)
	case ActionCheckpoint:
		// Request checkpoint for context exhaustion
		return ic.RequestCheckpoint(ctx, report)
	case ActionNotifyHuman:
		// Just create the escalation issue without stopping execution
		return ic.notifyHuman(ctx, report)

	case ActionInvestigate, ActionMonitor:
		// These are lower-priority actions - just log and create escalation
		return ic.flagForInvestigation(ctx, report)

	default:
		return nil, fmt.Errorf("unknown recommended action: %s", report.RecommendedAction)
	}
}

// createEscalationIssue creates or updates an escalation issue for human review
// Implements deduplication to prevent spam (vc-243)
// currentIssueID is passed as parameter to avoid reading ic.currentIssueID without lock
func (ic *InterventionController) createEscalationIssue(ctx context.Context, report *AnomalyReport, interventionType InterventionType, currentIssueID string) (string, error) {
	// Check for existing open escalation for this (issue, anomaly type) combination
	anomalyLabel := fmt.Sprintf("anomaly:%s", report.AnomalyType)
	affectedLabel := fmt.Sprintf("affected-issue:%s", currentIssueID)

	openStatus := types.StatusOpen
	filter := types.IssueFilter{
		Status: &openStatus,
		Labels: []string{"watchdog-escalation", anomalyLabel, affectedLabel},
		Limit:  1,
	}

	existing, err := ic.store.SearchIssues(ctx, "", filter)
	if err != nil {
		// Log but continue - we'll create new if search fails
		fmt.Printf("Warning: failed to search for existing escalation: %v\n", err)
	}

	// If existing escalation found, update it instead of creating new
	if len(existing) > 0 {
		return ic.updateEscalationIssue(ctx, existing[0], report, interventionType)
	}

	// No existing escalation - create new one
	title := fmt.Sprintf("Watchdog: %s anomaly detected in %s", report.AnomalyType, currentIssueID)

	description := fmt.Sprintf(`Watchdog detected anomalous behavior and intervened.

**Anomaly Type**: %s
**Severity**: %s
**Confidence**: %.2f
**Intervention**: %s
**Affected Issues**: %v

**Description**:
%s

**Reasoning**:
%s

**Recommended Action**: %s

---
**Detection History**:
- %s: Detected (severity=%s, confidence=%.2f, intervention=%s)
`,
		report.AnomalyType,
		report.Severity,
		report.Confidence,
		interventionType,
		report.AffectedIssues,
		report.Description,
		report.Reasoning,
		report.RecommendedAction,
		time.Now().Format("2006-01-02 15:04:05"),
		report.Severity,
		report.Confidence,
		interventionType,
	)

	// Determine issue priority based on severity
	var priority int
	switch report.Severity {
	case SeverityCritical:
		priority = 0 // P0
	case SeverityHigh:
		priority = 1 // P1
	case SeverityMedium:
		priority = 2 // P2
	default:
		priority = 3 // P3
	}

	// Create the escalation issue
	issue := &types.Issue{
		Title:       title,
		Description: description,
		Status:      types.StatusOpen,
		Priority:    priority,
		IssueType:   types.TypeTask,
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}

	// Store the issue (using "watchdog" as the actor)
	if err := ic.store.CreateIssue(ctx, issue, "watchdog"); err != nil {
		return "", fmt.Errorf("failed to create escalation issue: %w", err)
	}

	// Add labels for deduplication
	labels := []string{"watchdog-escalation", anomalyLabel, affectedLabel}
	for _, label := range labels {
		if err := ic.store.AddLabel(ctx, issue.ID, label, "watchdog"); err != nil {
			// Log but don't fail - labels are for optimization
			fmt.Printf("Warning: failed to add label %s to escalation %s: %v\n", label, issue.ID, err)
		}
	}

	// Note: We do NOT create a dependency on the parent issue (vc-244)
	// Escalation issues are monitoring artifacts that should not block their parent
	// Parent issue ID is already in the title and labels for reference

	return issue.ID, nil
}

// updateEscalationIssue updates an existing escalation with new observation
func (ic *InterventionController) updateEscalationIssue(ctx context.Context, issue *types.Issue, report *AnomalyReport, interventionType InterventionType) (string, error) {
	// Append new detection to history
	now := time.Now()
	newObservation := fmt.Sprintf("\n- %s: Detected (severity=%s, confidence=%.2f, intervention=%s)",
		now.Format("2006-01-02 15:04:05"),
		report.Severity,
		report.Confidence,
		interventionType,
	)

	// Build updates map
	// Note: updated_at is automatically managed by storage layer, don't pass it explicitly
	updates := map[string]interface{}{
		"description": issue.Description + newObservation,
	}

	// Update priority if severity increased
	var newPriority int
	switch report.Severity {
	case SeverityCritical:
		newPriority = 0
	case SeverityHigh:
		newPriority = 1
	case SeverityMedium:
		newPriority = 2
	default:
		newPriority = 3
	}
	if newPriority < issue.Priority {
		updates["priority"] = newPriority
	}

	// Save updated issue
	if err := ic.store.UpdateIssue(ctx, issue.ID, updates, "watchdog"); err != nil {
		return "", fmt.Errorf("failed to update escalation issue: %w", err)
	}

	// Add comment about new detection
	comment := fmt.Sprintf("New %s anomaly detection: severity=%s, confidence=%.2f, intervention=%s",
		report.AnomalyType, report.Severity, report.Confidence, interventionType)
	if err := ic.store.AddComment(ctx, issue.ID, "watchdog", comment); err != nil {
		// Log but don't fail - comment is nice-to-have
		fmt.Printf("Warning: failed to add comment to escalation %s: %v\n", issue.ID, err)
	}

	return issue.ID, nil
}

// emitWatchdogEvent emits a watchdog event through the event system
// currentIssueID is passed as parameter to avoid reading ic.currentIssueID without lock
func (ic *InterventionController) emitWatchdogEvent(ctx context.Context, result *InterventionResult, currentIssueID string) error {
	if currentIssueID == "" {
		// No current issue to attach event to
		return nil
	}

	comment := fmt.Sprintf("Watchdog intervention: %s - %s", result.InterventionType, result.Message)
	actor := fmt.Sprintf("watchdog-%s", ic.executorInstanceID)

	if err := ic.store.AddComment(ctx, currentIssueID, actor, comment); err != nil {
		return fmt.Errorf("failed to create watchdog event: %w", err)
	}

	return nil
}

// addToHistoryLocked adds an intervention result to the history
// MUST be called with ic.mu held (lock requirement enforced by naming convention)
func (ic *InterventionController) addToHistoryLocked(result *InterventionResult) {
	ic.interventionHistory = append(ic.interventionHistory, *result)

	// Enforce max history size - keep only the last maxHistorySize entries
	if len(ic.interventionHistory) > ic.maxHistorySize {
		ic.interventionHistory = ic.interventionHistory[len(ic.interventionHistory)-ic.maxHistorySize:]
	}
}

// GetInterventionHistory returns a copy of recent intervention results
func (ic *InterventionController) GetInterventionHistory() []InterventionResult {
	ic.mu.RLock()
	defer ic.mu.RUnlock()

	result := make([]InterventionResult, len(ic.interventionHistory))
	copy(result, ic.interventionHistory)
	return result
}

// GetCurrentIssueID returns the ID of the currently executing issue
func (ic *InterventionController) GetCurrentIssueID() string {
	ic.mu.RLock()
	defer ic.mu.RUnlock()
	return ic.currentIssueID
}

// HasActiveAgent returns true if an agent is currently executing
func (ic *InterventionController) HasActiveAgent() bool {
	ic.mu.RLock()
	defer ic.mu.RUnlock()
	return ic.cancelFunc != nil
}
