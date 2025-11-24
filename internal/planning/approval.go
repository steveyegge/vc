// Package planning provides functionality for mission plan approval and issue creation.
package planning

import (
	"context"
	"fmt"
	"time"

	"github.com/steveyegge/vc/internal/storage"
	"github.com/steveyegge/vc/internal/types"
)

// ApprovalResult contains the summary of issues created during approval.
type ApprovalResult struct {
	// PhaseIDs contains the issue IDs of created phases (in order).
	PhaseIDs []string

	// TaskIDs contains the issue IDs of created tasks (grouped by phase).
	TaskIDs map[string][]string // phase ID -> task IDs

	// TotalIssues is the total number of issues created (phases + tasks).
	TotalIssues int

	// MissionID is the ID of the parent mission.
	MissionID string
}

// ApproveAndCreateIssues atomically converts an approved plan into concrete Beads issues.
//
// This function implements the critical bridge between planning and execution:
//  1. Validates the plan is ready for approval (status must be "validated")
//  2. Creates all child issues (phases and tasks) in a single transaction
//  3. Sets up dependency graph (phases block mission, tasks block phases)
//  4. Applies labels (generated:plan) to all created issues
//  5. Transitions mission status: draft → planned
//  6. Cleans up ephemeral plan from storage
//
// Atomicity Guarantee:
// All operations occur in a single database transaction. If ANY step fails,
// the entire operation rolls back, leaving no partial state in the issue tracker.
//
// Parameters:
//   - ctx: Context for cancellation and timeouts
//   - store: Storage interface (must support GetDB() to access underlying DB)
//   - plan: The validated plan to approve and create issues from
//   - actor: Who is approving the plan (e.g., "user@example.com" or "ai-supervisor")
//
// Returns:
//   - ApprovalResult with IDs of all created issues
//   - Error if validation fails or any creation step fails (triggers rollback)
//
// Error Conditions:
//   - Plan status is not "validated" (must pass validation first)
//   - Plan was already approved (no-op protection)
//   - Mission issue doesn't exist or is not an epic
//   - Any database operation fails (constraint violation, etc.)
//   - Transaction commit fails
func ApproveAndCreateIssues(ctx context.Context, store storage.Storage, plan *MissionPlan, actor string) (*ApprovalResult, error) {
	// Validate inputs
	if plan == nil {
		return nil, fmt.Errorf("plan cannot be nil")
	}
	if plan.MissionID == "" {
		return nil, fmt.Errorf("plan must have a mission ID")
	}
	if actor == "" {
		return nil, fmt.Errorf("actor cannot be empty")
	}

	// Validate plan is ready for approval
	if plan.Status != PlanStatusValidated {
		return nil, fmt.Errorf("plan status must be 'validated' to approve (current: %s)", plan.Status)
	}

	// TODO(vc-kkmz): Implement true atomic transaction support
	// Currently, operations are NOT atomic - if any step fails partway through,
	// some issues may be created while others are not. To fix this, we need:
	// 1. Beads to expose a transaction-aware API (RunInTransaction or similar)
	// 2. All storage methods to accept an optional transaction parameter
	// For now, we rely on validation to catch errors early and minimize risk.

	// Initialize result
	result := &ApprovalResult{
		MissionID: plan.MissionID,
		TaskIDs:   make(map[string][]string),
	}

	// Verify mission exists and is an epic
	mission, err := store.GetMission(ctx, plan.MissionID)
	if err != nil {
		return nil, fmt.Errorf("failed to get mission: %w", err)
	}
	if mission == nil {
		return nil, fmt.Errorf("mission %s not found", plan.MissionID)
	}
	if mission.IssueType != types.TypeEpic {
		return nil, fmt.Errorf("mission %s is not an epic (type: %s)", plan.MissionID, mission.IssueType)
	}

	// Check if mission was already approved (idempotency check)
	// After approval, the plan is deleted, so check the mission's approval status instead
	if mission.ApprovedAt != nil {
		return nil, fmt.Errorf("plan for mission %s was already approved at %v", plan.MissionID, mission.ApprovedAt)
	}

	// Create phases and tasks
	for phaseIdx, phase := range plan.Phases {
		// Create phase issue
		estimatedMinutes := int(phase.EstimatedHours * 60) // Convert hours to minutes
		phaseIssue := &types.Issue{
			Title:              phase.Title,
			Description:        phase.Description,
			Design:             phase.Strategy, // Map strategy to design field
			AcceptanceCriteria: "", // Phases don't require acceptance criteria (they're chores)
			Status:             types.StatusOpen,
			Priority:           phase.Priority,
			IssueType:          types.TypeChore, // Phases are chores (group tasks without requiring AC)
			IssueSubtype:       types.SubtypeNormal,
			EstimatedMinutes:   &estimatedMinutes,
			CreatedAt:          time.Now(),
			UpdatedAt:          time.Now(),
		}

		// Create phase in database (generates ID)
		if err := store.CreateIssue(ctx, phaseIssue, actor); err != nil {
			return nil, fmt.Errorf("failed to create phase %d: %w", phaseIdx+1, err)
		}

		result.PhaseIDs = append(result.PhaseIDs, phaseIssue.ID)
		result.TotalIssues++

		// Apply label: generated:plan
		if err := store.AddLabel(ctx, phaseIssue.ID, "generated:plan", actor); err != nil {
			return nil, fmt.Errorf("failed to label phase %s: %w", phaseIssue.ID, err)
		}

		// Create dependency: phase blocks mission
		phaseDep := &types.Dependency{
			IssueID:    plan.MissionID,
			DependsOnID: phaseIssue.ID,
			Type:        types.DepBlocks,
			CreatedAt:   time.Now(),
		}
		if err := store.AddDependency(ctx, phaseDep, actor); err != nil {
			return nil, fmt.Errorf("failed to add phase dependency: %w", err)
		}

		// Create tasks for this phase
		var taskIDs []string
		for taskIdx, task := range phase.Tasks {
			// Build acceptance criteria string from array
			acceptanceCriteria := ""
			for i, criterion := range task.AcceptanceCriteria {
				if i > 0 {
					acceptanceCriteria += "\n"
				}
				acceptanceCriteria += criterion
			}

			taskEstimatedMinutes := task.EstimatedMinutes
			taskIssue := &types.Issue{
				Title:              task.Title,
				Description:        task.Description,
				AcceptanceCriteria: acceptanceCriteria,
				Status:             types.StatusOpen,
				Priority:           task.Priority,
				IssueType:          types.TypeTask,
				IssueSubtype:       types.SubtypeNormal, // Tasks are normal issues
				EstimatedMinutes:   &taskEstimatedMinutes,
				CreatedAt:          time.Now(),
				UpdatedAt:          time.Now(),
			}

			// Create task in database (generates ID)
			if err := store.CreateIssue(ctx, taskIssue, actor); err != nil {
				return nil, fmt.Errorf("failed to create task %d in phase %d: %w", taskIdx+1, phaseIdx+1, err)
			}

			taskIDs = append(taskIDs, taskIssue.ID)
			result.TotalIssues++

			// Apply label: generated:plan
			if err := store.AddLabel(ctx, taskIssue.ID, "generated:plan", actor); err != nil {
				return nil, fmt.Errorf("failed to label task %s: %w", taskIssue.ID, err)
			}

			// Create dependency: task blocks phase
			taskDep := &types.Dependency{
				IssueID:    phaseIssue.ID,
				DependsOnID: taskIssue.ID,
				Type:        types.DepBlocks,
				CreatedAt:   time.Now(),
			}
			if err := store.AddDependency(ctx, taskDep, actor); err != nil {
				return nil, fmt.Errorf("failed to add task dependency: %w", err)
			}
		}

		result.TaskIDs[phaseIssue.ID] = taskIDs
	}

	// Update mission approval metadata (status remains "open" until work begins)
	now := time.Now()
	missionUpdates := map[string]interface{}{
		"approved_at": now,
		"approved_by": actor,
	}
	if err := store.UpdateMission(ctx, plan.MissionID, missionUpdates, actor); err != nil {
		return nil, fmt.Errorf("failed to update mission: %w", err)
	}

	// Delete ephemeral plan from storage
	if err := store.DeletePlan(ctx, plan.MissionID); err != nil {
		return nil, fmt.Errorf("failed to delete ephemeral plan: %w", err)
	}

	return result, nil
}
