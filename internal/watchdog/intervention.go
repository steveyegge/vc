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
	InterventionPauseAgent    InterventionType = "pause_agent"
	InterventionKillAgent     InterventionType = "kill_agent"
	InterventionPauseExecutor InterventionType = "pause_executor"
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
	if err := ic.emitWatchdogEvent(ctx, result); err != nil {
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
	if err := ic.emitWatchdogEvent(ctx, result); err != nil {
		result.Message += fmt.Sprintf(" (warning: failed to emit event: %v)", err)
	}

	// Add to history
	ic.addToHistoryLocked(result)

	fmt.Printf("Watchdog: Killed agent for issue %s (escalation: %s)\n", ic.currentIssueID, escalationID)

	return result, nil
}

// PauseExecutor pauses the entire executor (not just the current agent)
// This is used for critical system-wide issues
func (ic *InterventionController) PauseExecutor(ctx context.Context, report *AnomalyReport) (*InterventionResult, error) {
	ic.mu.Lock()
	defer ic.mu.Unlock()

	result := &InterventionResult{
		Success:          true,
		InterventionType: InterventionPauseExecutor,
		AnomalyReport:    report,
		Message:          fmt.Sprintf("Paused executor %s due to %s anomaly (severity: %s)", ic.executorInstanceID, report.AnomalyType, report.Severity),
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
	if err := ic.emitWatchdogEvent(ctx, result); err != nil {
		result.Message += fmt.Sprintf(" (warning: failed to emit event: %v)", err)
	}

	// Add to history
	ic.addToHistoryLocked(result)

	// NOTE: Pausing the executor requires coordination with the executor's main loop
	// The executor should monitor for escalation issues or other signals to stop
	// For now, we just create the escalation issue and let the executor detect it

	fmt.Printf("Watchdog: Paused executor %s (escalation: %s)\n", ic.executorInstanceID, escalationID)

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
	case ActionNotifyHuman:
		// Just create the escalation issue without stopping execution
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

		if err := ic.emitWatchdogEvent(ctx, result); err != nil {
			result.Message += fmt.Sprintf(" (warning: failed to emit event: %v)", err)
		}

		ic.addToHistoryLocked(result)
		return result, nil

	case ActionInvestigate, ActionMonitor:
		// These are lower-priority actions - just log and create escalation
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

		if err := ic.emitWatchdogEvent(ctx, result); err != nil {
			result.Message += fmt.Sprintf(" (warning: failed to emit event: %v)", err)
		}

		ic.addToHistoryLocked(result)
		return result, nil

	default:
		return nil, fmt.Errorf("unknown recommended action: %s", report.RecommendedAction)
	}
}

// createEscalationIssue creates a new issue for human review of the anomaly
// currentIssueID is passed as parameter to avoid reading ic.currentIssueID without lock
func (ic *InterventionController) createEscalationIssue(ctx context.Context, report *AnomalyReport, interventionType InterventionType, currentIssueID string) (string, error) {
	// Build issue title and description
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
`,
		report.AnomalyType,
		report.Severity,
		report.Confidence,
		interventionType,
		report.AffectedIssues,
		report.Description,
		report.Reasoning,
		report.RecommendedAction,
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

	// If there's a current issue being executed, create a dependency
	if currentIssueID != "" {
		dep := &types.Dependency{
			IssueID:     issue.ID,
			DependsOnID: currentIssueID,
			Type:        types.DepDiscoveredFrom,
			CreatedAt:   time.Now(),
			CreatedBy:   "watchdog",
		}
		if err := ic.store.AddDependency(ctx, dep, "watchdog"); err != nil {
			// Log but don't fail - dependency is nice-to-have
			fmt.Printf("Warning: failed to add dependency from escalation %s to %s: %v\n",
				issue.ID, currentIssueID, err)
		}
	}

	return issue.ID, nil
}

// emitWatchdogEvent emits a watchdog event through the event system
func (ic *InterventionController) emitWatchdogEvent(ctx context.Context, result *InterventionResult) error {
	if ic.currentIssueID == "" {
		// No current issue to attach event to
		return nil
	}

	comment := fmt.Sprintf("Watchdog intervention: %s - %s", result.InterventionType, result.Message)
	actor := fmt.Sprintf("watchdog-%s", ic.executorInstanceID)

	if err := ic.store.AddComment(ctx, ic.currentIssueID, actor, comment); err != nil {
		return fmt.Errorf("failed to create watchdog event: %w", err)
	}

	return nil
}

// addToHistoryLocked adds an intervention result to the history
// MUST be called with ic.mu held (lock requirement enforced by naming convention)
func (ic *InterventionController) addToHistoryLocked(result *InterventionResult) {
	ic.interventionHistory = append(ic.interventionHistory, *result)

	// Enforce max history size
	if len(ic.interventionHistory) > ic.maxHistorySize {
		copy(ic.interventionHistory, ic.interventionHistory[len(ic.interventionHistory)-ic.maxHistorySize:])
		ic.interventionHistory = ic.interventionHistory[:ic.maxHistorySize]
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
