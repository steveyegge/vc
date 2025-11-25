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
//  5. Updates mission approval metadata
//  6. Cleans up ephemeral plan from storage
//
// Atomicity Guarantee:
// All Beads operations (issue creation, dependencies, labels) occur in a single
// database transaction. If ANY step fails, the entire operation rolls back,
// leaving no partial state in the issue tracker.
//
// Note: Mission state update and plan deletion are VC-specific operations that
// run after the transaction commits. These are "finalization" steps that occur
// after the core atomic work is complete.
//
// Parameters:
//   - ctx: Context for cancellation and timeouts
//   - store: Storage interface with transaction support
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
//
// vc-3hjg: Added transaction support for atomic issue creation
func ApproveAndCreateIssues(ctx context.Context, store storage.Storage, plan *MissionPlan, actor string) (*ApprovalResult, error) {
	// ==========================================================================
	// Phase 1: Pre-validation (outside transaction)
	// ==========================================================================

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

	// Verify mission exists and is an epic (pre-validation before transaction)
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
	if mission.ApprovedAt != nil {
		return nil, fmt.Errorf("plan for mission %s was already approved at %v", plan.MissionID, mission.ApprovedAt)
	}

	// ==========================================================================
	// Phase 2: Atomic issue creation (inside transaction)
	// ==========================================================================

	// Initialize result
	result := &ApprovalResult{
		MissionID: plan.MissionID,
		TaskIDs:   make(map[string][]string),
	}

	// Run all Beads operations in a transaction
	// If any operation fails, the entire transaction is rolled back
	err = store.RunInVCTransaction(ctx, func(tx *storage.VCTransaction) error {
		// Create phases and tasks atomically
		for phaseIdx, phase := range plan.Phases {
			// Create phase issue
			estimatedMinutes := int(phase.EstimatedHours * 60) // Convert hours to minutes
			phaseIssue := &types.Issue{
				Title:              phase.Title,
				Description:        phase.Description,
				Design:             phase.Strategy, // Map strategy to design field
				AcceptanceCriteria: "",             // Phases don't require acceptance criteria (they're chores)
				Status:             types.StatusOpen,
				Priority:           phase.Priority,
				IssueType:          types.TypeChore, // Phases are chores (group tasks without requiring AC)
				IssueSubtype:       types.SubtypeNormal,
				EstimatedMinutes:   &estimatedMinutes,
				CreatedAt:          time.Now(),
				UpdatedAt:          time.Now(),
			}

			// Create phase in database (generates ID)
			if err := tx.CreateIssue(ctx, phaseIssue, actor); err != nil {
				return fmt.Errorf("failed to create phase %d: %w", phaseIdx+1, err)
			}

			result.PhaseIDs = append(result.PhaseIDs, phaseIssue.ID)
			result.TotalIssues++

			// Apply label: generated:plan
			if err := tx.AddLabel(ctx, phaseIssue.ID, "generated:plan", actor); err != nil {
				return fmt.Errorf("failed to label phase %s: %w", phaseIssue.ID, err)
			}

			// Create dependency: phase blocks mission
			phaseDep := &types.Dependency{
				IssueID:     plan.MissionID,
				DependsOnID: phaseIssue.ID,
				Type:        types.DepBlocks,
				CreatedAt:   time.Now(),
			}
			if err := tx.AddDependency(ctx, phaseDep, actor); err != nil {
				return fmt.Errorf("failed to add phase dependency: %w", err)
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
				if err := tx.CreateIssue(ctx, taskIssue, actor); err != nil {
					return fmt.Errorf("failed to create task %d in phase %d: %w", taskIdx+1, phaseIdx+1, err)
				}

				taskIDs = append(taskIDs, taskIssue.ID)
				result.TotalIssues++

				// Apply label: generated:plan
				if err := tx.AddLabel(ctx, taskIssue.ID, "generated:plan", actor); err != nil {
					return fmt.Errorf("failed to label task %s: %w", taskIssue.ID, err)
				}

				// Create dependency: task blocks phase
				taskDep := &types.Dependency{
					IssueID:     phaseIssue.ID,
					DependsOnID: taskIssue.ID,
					Type:        types.DepBlocks,
					CreatedAt:   time.Now(),
				}
				if err := tx.AddDependency(ctx, taskDep, actor); err != nil {
					return fmt.Errorf("failed to add task dependency: %w", err)
				}
			}

			result.TaskIDs[phaseIssue.ID] = taskIDs
		}

		return nil // Commit transaction
	})

	if err != nil {
		// Transaction rolled back - no issues were created
		return nil, fmt.Errorf("transaction failed: %w", err)
	}

	// ==========================================================================
	// Phase 3: Finalization (outside transaction, after commit)
	// ==========================================================================

	// Update mission approval metadata (VC-specific operation)
	// This runs after the transaction commits, so issues are already created
	now := time.Now()
	missionUpdates := map[string]interface{}{
		"approved_at": now,
		"approved_by": actor,
	}
	if err := store.UpdateMission(ctx, plan.MissionID, missionUpdates, actor); err != nil {
		// Note: Issues were already created. This is a "finalization" failure.
		// The caller should be aware that issues exist but mission state wasn't updated.
		return result, fmt.Errorf("issues created but failed to update mission: %w", err)
	}

	// Delete ephemeral plan from storage (VC-specific operation)
	if err := store.DeletePlan(ctx, plan.MissionID); err != nil {
		// Note: Issues were created and mission was updated. Only plan cleanup failed.
		// This is non-critical - the plan is ephemeral and can be cleaned up later.
		return result, fmt.Errorf("issues created but failed to delete ephemeral plan: %w", err)
	}

	return result, nil
}
