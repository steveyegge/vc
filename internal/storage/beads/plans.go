package beads

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/steveyegge/vc/internal/types"
)

// ErrStaleIteration is returned when attempting to update a plan with a stale iteration number
// This indicates a concurrent modification race - another process updated the plan first
var ErrStaleIteration = errors.New("plan iteration mismatch: concurrent modification detected")

// StorePlan stores or updates a mission plan with optimistic locking (vc-un1o, vc-gxfn)
//
// CONCURRENCY CONTROL:
// - expectedIteration == 0: CREATE new plan or FORCE UPDATE (ignore iteration)
// - expectedIteration > 0: UPDATE only if current iteration matches (optimistic locking)
//   Returns storage.ErrStaleIteration if mismatch (concurrent modification detected)
//
// TRANSACTION SAFETY (vc-gxfn):
// - Entire operation wrapped in transaction
// - Plan JSON marshaling happens before transaction begins
// - On any error, transaction rolls back automatically
//
// EXAMPLE USAGE:
//
//	// First time storing a plan
//	newIteration, err := store.StorePlan(ctx, plan, 0)
//	// newIteration == 1
//
//	// Refining an existing plan (optimistic locking)
//	newIteration, err := store.StorePlan(ctx, refinedPlan, 5)
//	if errors.Is(err, ErrStaleIteration) {
//	    // Someone else updated the plan, refetch and retry
//	}
func (s *VCStorage) StorePlan(ctx context.Context, plan *types.MissionPlan, expectedIteration int) (int, error) {
	// Validate input
	if plan == nil {
		return 0, fmt.Errorf("plan cannot be nil")
	}
	if err := plan.Validate(); err != nil {
		return 0, fmt.Errorf("invalid plan: %w", err)
	}
	if expectedIteration < 0 {
		return 0, fmt.Errorf("expectedIteration must be >= 0 (got %d)", expectedIteration)
	}

	// Marshal plan to JSON before starting transaction
	planJSON, err := json.Marshal(plan)
	if err != nil {
		return 0, fmt.Errorf("failed to marshal plan JSON: %w", err)
	}

	// Begin transaction (vc-gxfn: atomic write with rollback on failure)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback() // Safe to call after commit

	var newIteration int

	// Check if plan exists
	var currentIteration int
	var exists bool
	err = tx.QueryRowContext(ctx, `
		SELECT iteration FROM vc_mission_plans WHERE mission_id = ?
	`, plan.MissionID).Scan(&currentIteration)

	if err == sql.ErrNoRows {
		// Plan doesn't exist - create new
		exists = false
		newIteration = 1
	} else if err != nil {
		return 0, fmt.Errorf("failed to check existing plan: %w", err)
	} else {
		// Plan exists
		exists = true

		// Optimistic locking check (vc-un1o)
		if expectedIteration > 0 && currentIteration != expectedIteration {
			return 0, ErrStaleIteration
		}

		newIteration = currentIteration + 1
	}

	now := time.Now()

	if exists {
		// Update existing plan
		_, err = tx.ExecContext(ctx, `
			UPDATE vc_mission_plans
			SET plan_json = ?,
			    iteration = ?,
			    status = ?,
			    updated_at = ?
			WHERE mission_id = ?
		`, string(planJSON), newIteration, plan.Status, now, plan.MissionID)
		if err != nil {
			return 0, fmt.Errorf("failed to update plan: %w", err)
		}
	} else {
		// Insert new plan
		_, err = tx.ExecContext(ctx, `
			INSERT INTO vc_mission_plans (mission_id, plan_json, iteration, status, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?)
		`, plan.MissionID, string(planJSON), newIteration, plan.Status, now, now)
		if err != nil {
			return 0, fmt.Errorf("failed to insert plan: %w", err)
		}
	}

	// Commit transaction
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("failed to commit transaction: %w", err)
	}

	return newIteration, nil
}

// GetPlan retrieves the latest plan for a mission
// Returns (nil, 0, nil) if no plan exists
func (s *VCStorage) GetPlan(ctx context.Context, missionID string) (*types.MissionPlan, int, error) {
	var planJSON string
	var iteration int
	var status string

	err := s.db.QueryRowContext(ctx, `
		SELECT plan_json, iteration, status
		FROM vc_mission_plans
		WHERE mission_id = ?
	`, missionID).Scan(&planJSON, &iteration, &status)

	if err == sql.ErrNoRows {
		return nil, 0, nil
	}
	if err != nil {
		return nil, 0, fmt.Errorf("failed to query plan: %w", err)
	}

	var plan types.MissionPlan
	if err := json.Unmarshal([]byte(planJSON), &plan); err != nil {
		return nil, 0, fmt.Errorf("failed to unmarshal plan JSON: %w", err)
	}

	// Restore status from table (not stored in JSON)
	plan.Status = status

	return &plan, iteration, nil
}

// GetPlanHistory retrieves all historical iterations of a plan ordered by iteration DESC
// This is useful for reviewing how a plan evolved through refinement
func (s *VCStorage) GetPlanHistory(ctx context.Context, missionID string) ([]*types.MissionPlan, error) {
	// Note: Current design stores only the latest iteration
	// To implement full history, we'd need to either:
	// 1. Add a vc_mission_plan_history table
	// 2. Change primary key to (mission_id, iteration) compound key
	//
	// For now, return single latest version wrapped in array
	plan, _, err := s.GetPlan(ctx, missionID)
	if err != nil {
		return nil, err
	}
	if plan == nil {
		return []*types.MissionPlan{}, nil
	}
	return []*types.MissionPlan{plan}, nil
}

// DeletePlan removes all plan data for a mission
func (s *VCStorage) DeletePlan(ctx context.Context, missionID string) error {
	_, err := s.db.ExecContext(ctx, `
		DELETE FROM vc_mission_plans WHERE mission_id = ?
	`, missionID)
	if err != nil {
		return fmt.Errorf("failed to delete plan: %w", err)
	}
	return nil
}

// ListDraftPlans retrieves all plans with status not 'approved'
// Useful for cleanup operations and monitoring stale draft plans
func (s *VCStorage) ListDraftPlans(ctx context.Context) ([]*types.MissionPlan, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT plan_json, status
		FROM vc_mission_plans
		WHERE status != 'approved'
		ORDER BY updated_at DESC
	`)
	if err != nil {
		return nil, fmt.Errorf("failed to query draft plans: %w", err)
	}
	defer rows.Close()

	var plans []*types.MissionPlan
	for rows.Next() {
		var planJSON string
		var status string
		if err := rows.Scan(&planJSON, &status); err != nil {
			return nil, fmt.Errorf("failed to scan plan row: %w", err)
		}

		var plan types.MissionPlan
		if err := json.Unmarshal([]byte(planJSON), &plan); err != nil {
			return nil, fmt.Errorf("failed to unmarshal plan JSON: %w", err)
		}

		plan.Status = status
		plans = append(plans, &plan)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating plan rows: %w", err)
	}

	return plans, nil
}
