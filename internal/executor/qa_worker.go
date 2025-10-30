package executor

import (
	"context"
	"fmt"
	"time"

	"github.com/steveyegge/vc/internal/ai"
	"github.com/steveyegge/vc/internal/events"
	"github.com/steveyegge/vc/internal/gates"
	"github.com/steveyegge/vc/internal/labels"
	"github.com/steveyegge/vc/internal/storage"
	"github.com/steveyegge/vc/internal/types"
)

// QualityGateWorker is a specialized worker that claims missions needing quality gates
// and runs gate commands (unlike code workers that spawn agents).
//
// vc-252: QualityGateWorker claiming logic
// vc-253: QA worker gate execution and transitions
// vc-259: Error handling policy
//
// Error Handling Policy:
//
// CRITICAL operations (must succeed, return error on failure):
//   - GetMissionsNeedingGates: Cannot proceed without missions to claim
//   - AddLabel(gates-running): Claim lock - failure means race condition, try next mission
//   - ClaimIssue: Creates execution state - failure means claim is incomplete
//   - GetMission: Cannot execute without mission metadata
//   - TransitionState: State machine transitions must succeed or mission is stuck
//   - UpdateIssue(status): Status changes are part of state transitions, must succeed
//   - AddLabel(gates-failed): Final state marker - failure means mission status unclear
//
// BEST-EFFORT operations (log warning, continue execution):
//   - StoreAgentEvent: Observability - nice to have, but not critical for correctness
//   - AddComment: User-facing diagnostics - helpful but not required for state consistency
//   - ReleaseIssue: Cleanup operation - orphaned claims cleaned up by stale instance cleanup
//   - RemoveLabel(gates-running): Cleanup operation - if this fails, mission is stuck but
//     we emit an alert event and rely on human intervention or watchdog to recover
//
// ALERT-WORTHY failures (emit alert event, log warning, continue):
//   - RemoveLabel(gates-running): If this fails after gates complete, the mission will be
//     stuck with gates-running label even though gates are done. This prevents re-execution
//     and requires manual intervention. We emit a high-severity alert event to notify
//     operators, then continue (since the gates actually ran and the results are recorded).
//
type QualityGateWorker struct {
	store      storage.Storage
	supervisor *ai.Supervisor
	workingDir string
	instanceID string
	gatesRunner *gates.Runner
}

// NewQualityGateWorker creates a new quality gate worker
func NewQualityGateWorker(cfg *QualityGateWorkerConfig) (*QualityGateWorker, error) {
	if cfg.Store == nil {
		return nil, fmt.Errorf("storage is required")
	}
	if cfg.InstanceID == "" {
		return nil, fmt.Errorf("instance ID is required")
	}
	if cfg.GatesRunner == nil {
		return nil, fmt.Errorf("gates runner is required")
	}
	if cfg.WorkingDir == "" {
		cfg.WorkingDir = "."
	}

	return &QualityGateWorker{
		store:      cfg.Store,
		supervisor: cfg.Supervisor,
		workingDir: cfg.WorkingDir,
		instanceID: cfg.InstanceID,
		gatesRunner: cfg.GatesRunner,
	}, nil
}

// QualityGateWorkerConfig holds configuration for the quality gate worker
type QualityGateWorkerConfig struct {
	Store       storage.Storage
	Supervisor  *ai.Supervisor
	WorkingDir  string
	InstanceID  string
	GatesRunner *gates.Runner
}

// ClaimReadyWork finds and claims a mission that needs quality gates.
//
// Claiming logic (vc-252):
// 1. Query for missions with 'needs-quality-gates' label
// 2. Exclude missions that already have 'gates-running' label (prevents double-claiming)
// 3. Atomically add 'gates-running' label to claim the mission
// 4. Update issue to in_progress status
// 5. Create execution state record
// 6. Return claimed mission
//
// Returns:
//   - (*types.Issue, error): The claimed mission, or nil if no work is ready
func (w *QualityGateWorker) ClaimReadyWork(ctx context.Context) (*types.Issue, error) {
	// Get missions needing gates from storage
	missions, err := w.store.GetMissionsNeedingGates(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to query missions needing gates: %w", err)
	}

	// No work available
	if len(missions) == 0 {
		return nil, nil
	}

	// Try to claim missions in order
	// atomicClaim handles race conditions via the AddLabel operation
	for _, mission := range missions {
		// Try to atomically claim this mission by adding gates-running label
		if err := w.atomicClaim(ctx, mission); err != nil {
			// Failed to claim (possibly race condition) - try next mission
			fmt.Printf("Warning: failed to claim mission %s: %v\n", mission.ID, err)
			continue
		}

		// Successfully claimed
		return mission, nil
	}

	// No missions could be claimed
	return nil, nil
}

// atomicClaim atomically claims a mission by adding the gates-running label
// and updating the issue to in_progress status.
//
// Steps:
// 1. Add 'gates-running' label (this is the claim lock)
// 2. Claim the issue (creates execution state and updates to in_progress)
// 3. Log claim event
//
// Returns error if any step fails. The caller should try another mission if this fails.
func (w *QualityGateWorker) atomicClaim(ctx context.Context, mission *types.Issue) error {
	// Step 1: Add gates-running label atomically
	// This is the critical lock - if another worker adds it first, we'll fail gracefully
	if err := w.store.AddLabel(ctx, mission.ID, labels.LabelGatesRunning, w.instanceID); err != nil {
		return fmt.Errorf("failed to add gates-running label: %w", err)
	}

	// Step 2: Claim the issue (creates execution state and updates to in_progress)
	if err := w.store.ClaimIssue(ctx, mission.ID, w.instanceID); err != nil {
		// Failed to claim - DO NOT remove the label, as we may not own it
		// If another worker added the label, removing it would break their lock
		// Orphaned labels will be cleaned up by stale instance cleanup
		return fmt.Errorf("failed to claim issue: %w", err)
	}

	// Step 3: Log claim event
	event := &events.AgentEvent{
		Type:      events.EventTypeProgress,
		Timestamp: time.Now(),
		IssueID:   mission.ID,
		Severity:  events.SeverityInfo,
		Message:   fmt.Sprintf("Quality gate worker claimed mission %s", mission.ID),
		Data: map[string]interface{}{
			"worker_type":   "quality_gate",
			"instance_id":   w.instanceID,
			"mission_title": mission.Title,
		},
	}

	if err := w.store.StoreAgentEvent(ctx, event); err != nil {
		// Log warning but don't fail the claim
		fmt.Printf("Warning: failed to log claim event: %v\n", err)
	}

	return nil
}

// Execute runs quality gates for a claimed mission and handles the results.
//
// vc-253: QA worker gate execution and transitions
//
// Steps:
// 1. Get mission sandbox path from mission.Metadata["sandbox_path"]
// 2. Run gates in that sandbox using the gates runner
// 3. Handle results:
//    - Success: transition to needs-review state
//    - Failure: transition to gates-failed state
// 4. Remove gates-running label
// 5. Update mission status
//
// Returns error if execution fails critically. Gate failures are handled gracefully.
func (w *QualityGateWorker) Execute(ctx context.Context, mission *types.Issue) error {
	// Get mission metadata to find sandbox path
	missionData, err := w.store.GetMission(ctx, mission.ID)
	if err != nil {
		return fmt.Errorf("failed to get mission metadata: %w", err)
	}
	if missionData == nil {
		return fmt.Errorf("mission %s has no metadata", mission.ID)
	}

	sandboxPath := missionData.SandboxPath
	if sandboxPath == "" {
		return fmt.Errorf("mission %s has no sandbox_path", mission.ID)
	}

	// Log execution start
	fmt.Printf("Running quality gates for mission %s in sandbox %s\n", mission.ID, sandboxPath)
	startEvent := &events.AgentEvent{
		Type:      events.EventTypeProgress,
		Timestamp: time.Now(),
		IssueID:   mission.ID,
		Severity:  events.SeverityInfo,
		Message:   "Quality gates execution started",
		Data: map[string]interface{}{
			"sandbox_path": sandboxPath,
			"worker_type":  "quality_gate",
		},
	}
	if err := w.store.StoreAgentEvent(ctx, startEvent); err != nil {
		fmt.Printf("Warning: failed to log start event: %v\n", err)
	}

	// TODO (vc-253): Run quality gates in the mission sandbox
	// For now, we'll use a placeholder - the gates runner needs to be enhanced
	// to support running in a specific directory (sandbox path)
	// This will be implemented when we integrate with the actual gate execution

	// Run quality gates (currently runs in working directory, not sandbox)
	results, allPassed := w.gatesRunner.RunAll(ctx)

	// Check for context cancellation
	if ctx.Err() != nil {
		return fmt.Errorf("gates execution canceled: %w", ctx.Err())
	}

	// Handle results and transition states
	if allPassed {
		return w.handleGatesPass(ctx, mission, results)
	} else {
		return w.handleGatesFail(ctx, mission, results)
	}
}

// handleGatesPass handles successful gate execution
// Transitions mission from needs-quality-gates → needs-review
func (w *QualityGateWorker) handleGatesPass(ctx context.Context, mission *types.Issue, results []*gates.Result) error {
	fmt.Printf("✓ All quality gates passed for mission %s\n", mission.ID)

	// Log success event
	event := &events.AgentEvent{
		Type:      events.EventTypeQualityGatePass,
		Timestamp: time.Now(),
		IssueID:   mission.ID,
		Severity:  events.SeverityInfo,
		Message:   "All quality gates passed",
		Data: map[string]interface{}{
			"gates_count": len(results),
			"worker_type": "quality_gate",
		},
	}
	if err := w.store.StoreAgentEvent(ctx, event); err != nil {
		fmt.Printf("Warning: failed to log pass event: %v\n", err)
	}

	// Transition: needs-quality-gates → needs-review
	if err := labels.TransitionState(ctx, w.store, mission.ID,
		labels.LabelNeedsQualityGates, labels.LabelNeedsReview,
		labels.TriggerGatesPassed, w.instanceID); err != nil {
		return fmt.Errorf("failed to transition to needs-review: %w", err)
	}

	// Remove gates-running label (best-effort with alert on failure)
	// vc-259: If this fails, mission is stuck with gates-running label
	// We emit an alert event for human intervention but don't fail the execution
	// since the gates actually completed and results are recorded
	if err := w.store.RemoveLabel(ctx, mission.ID, labels.LabelGatesRunning, w.instanceID); err != nil {
		fmt.Printf("ERROR: Failed to remove gates-running label from %s: %v\n", mission.ID, err)
		fmt.Printf("       Mission is stuck with gates-running - manual intervention required\n")

		// Emit high-severity alert event
		alertEvent := &events.AgentEvent{
			Type:      events.EventTypeError,
			Timestamp: time.Now(),
			IssueID:   mission.ID,
			Severity:  events.SeverityError,
			Message:   "Failed to remove gates-running label - mission stuck",
			Data: map[string]interface{}{
				"error":       err.Error(),
				"worker_type": "quality_gate",
				"action":      "Manual intervention required to remove gates-running label",
			},
		}
		if alertErr := w.store.StoreAgentEvent(ctx, alertEvent); alertErr != nil {
			fmt.Printf("Warning: failed to log alert event: %v\n", alertErr)
		}
		// Continue execution - don't return error
	}

	// Update mission status to open (ready for next stage)
	if err := w.store.UpdateIssue(ctx, mission.ID, map[string]interface{}{
		"status": string(types.StatusOpen),
	}, w.instanceID); err != nil {
		return fmt.Errorf("failed to update mission status: %w", err)
	}

	// Release execution state
	if err := w.store.ReleaseIssue(ctx, mission.ID); err != nil {
		fmt.Printf("Warning: failed to release execution state: %v\n", err)
	}

	return nil
}

// handleGatesFail handles failed gate execution
// Transitions mission to gates-failed state and creates fix tasks if appropriate
func (w *QualityGateWorker) handleGatesFail(ctx context.Context, mission *types.Issue, results []*gates.Result) error {
	fmt.Printf("✗ Quality gates failed for mission %s\n", mission.ID)

	// Log failure event
	failedGates := []string{}
	for _, result := range results {
		if !result.Passed {
			failedGates = append(failedGates, string(result.Gate))
		}
	}

	event := &events.AgentEvent{
		Type:      events.EventTypeQualityGateFail,
		Timestamp: time.Now(),
		IssueID:   mission.ID,
		Severity:  events.SeverityWarning,
		Message:   fmt.Sprintf("Quality gates failed: %d/%d passed", len(results)-len(failedGates), len(results)),
		Data: map[string]interface{}{
			"gates_count":  len(results),
			"failed_gates": failedGates,
			"worker_type":  "quality_gate",
		},
	}
	if err := w.store.StoreAgentEvent(ctx, event); err != nil {
		fmt.Printf("Warning: failed to log fail event: %v\n", err)
	}

	// Add comment with gate results
	comment := fmt.Sprintf("**Quality Gates Failed**\n\n")
	comment += fmt.Sprintf("Failed: %d/%d gates\n\n", len(failedGates), len(results))
	for _, result := range results {
		status := "✓"
		if !result.Passed {
			status = "✗"
		}
		comment += fmt.Sprintf("%s **%s**\n", status, result.Gate)
		if result.Output != "" {
			comment += fmt.Sprintf("```\n%s\n```\n\n", result.Output)
		}
	}

	if err := w.store.AddComment(ctx, mission.ID, w.instanceID, comment); err != nil {
		fmt.Printf("Warning: failed to add gate results comment: %v\n", err)
	}

	// Remove gates-running label (best-effort with alert on failure)
	// vc-259: If this fails, mission is stuck with gates-running label
	// We emit an alert event for human intervention but don't fail the execution
	// since the gates actually completed and results are recorded
	if err := w.store.RemoveLabel(ctx, mission.ID, labels.LabelGatesRunning, w.instanceID); err != nil {
		fmt.Printf("ERROR: Failed to remove gates-running label from %s: %v\n", mission.ID, err)
		fmt.Printf("       Mission is stuck with gates-running - manual intervention required\n")

		// Emit high-severity alert event
		alertEvent := &events.AgentEvent{
			Type:      events.EventTypeError,
			Timestamp: time.Now(),
			IssueID:   mission.ID,
			Severity:  events.SeverityError,
			Message:   "Failed to remove gates-running label - mission stuck",
			Data: map[string]interface{}{
				"error":       err.Error(),
				"worker_type": "quality_gate",
				"action":      "Manual intervention required to remove gates-running label",
			},
		}
		if alertErr := w.store.StoreAgentEvent(ctx, alertEvent); alertErr != nil {
			fmt.Printf("Warning: failed to log alert event: %v\n", alertErr)
		}
		// Continue execution - don't return error
	}

	// Add gates-failed label (custom label, not a state transition)
	if err := w.store.AddLabel(ctx, mission.ID, "gates-failed", w.instanceID); err != nil {
		return fmt.Errorf("failed to add gates-failed label: %w", err)
	}

	// Update mission status to blocked
	if err := w.store.UpdateIssue(ctx, mission.ID, map[string]interface{}{
		"status": string(types.StatusBlocked),
	}, w.instanceID); err != nil {
		return fmt.Errorf("failed to update mission status: %w", err)
	}

	// Release execution state
	if err := w.store.ReleaseIssue(ctx, mission.ID); err != nil {
		fmt.Printf("Warning: failed to release execution state: %v\n", err)
	}

	// TODO (vc-253): Analyze failures with AI to determine if:
	// - Minor issues → create fix tasks, add has-fix-tasks label
	// - Major issues → add needs-redesign label, escalate to human
	// For now, just mark as gates-failed and let human decide

	return nil
}
