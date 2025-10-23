package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/steveyegge/vc/internal/types"
)

// ClaimIssue atomically claims an issue for execution by an executor instance
// This prevents double-claiming by using INSERT which will fail if the issue is already claimed
func (s *SQLiteStorage) ClaimIssue(ctx context.Context, issueID, executorInstanceID string) error {
	// Start transaction for atomic claim + issue status update
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	// Check if issue is already claimed
	var existingExecutor string
	err = tx.QueryRowContext(ctx, "SELECT executor_instance_id FROM issue_execution_state WHERE issue_id = ?", issueID).Scan(&existingExecutor)
	if err != sql.ErrNoRows {
		if err != nil {
			return fmt.Errorf("failed to check execution state: %w", err)
		}
		return fmt.Errorf("issue %s is already claimed by another executor", issueID)
	}

	// Verify the issue exists and is in 'open' status
	var issueStatus string
	err = tx.QueryRowContext(ctx, "SELECT status FROM issues WHERE id = ?", issueID).Scan(&issueStatus)
	if err == sql.ErrNoRows {
		return fmt.Errorf("issue not found: %s", issueID)
	}
	if err != nil {
		return fmt.Errorf("failed to check issue status: %w", err)
	}
	if issueStatus != string(types.StatusOpen) {
		return fmt.Errorf("issue %s is not open (status: %s)", issueID, issueStatus)
	}

	// Verify executor instance exists
	var instanceExists bool
	err = tx.QueryRowContext(ctx, "SELECT 1 FROM executor_instances WHERE instance_id = ?", executorInstanceID).Scan(&instanceExists)
	if err == sql.ErrNoRows {
		return fmt.Errorf("executor instance not found: %s", executorInstanceID)
	}
	if err != nil {
		return fmt.Errorf("failed to check executor instance: %w", err)
	}

	// Attempt to insert execution state (will fail if already claimed)
	now := time.Now()
	_, err = tx.ExecContext(ctx, `
		INSERT INTO issue_execution_state (
			issue_id, executor_instance_id, state, checkpoint_data, started_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?)
	`, issueID, executorInstanceID, types.ExecutionStateClaimed, "{}", now, now)
	if err != nil {
		// Check if it's a unique constraint violation (issue already claimed)
		if isUniqueConstraintError(err) {
			return fmt.Errorf("issue %s is already claimed by another executor", issueID)
		}
		return fmt.Errorf("failed to claim issue: %w", err)
	}

	// Update issue status to in_progress
	_, err = tx.ExecContext(ctx, `
		UPDATE issues SET status = ?, updated_at = ?
		WHERE id = ?
	`, types.StatusInProgress, now, issueID)
	if err != nil {
		return fmt.Errorf("failed to update issue status: %w", err)
	}

	// Record event
	_, err = tx.ExecContext(ctx, `
		INSERT INTO events (issue_id, event_type, actor, comment)
		VALUES (?, ?, ?, ?)
	`, issueID, types.EventStatusChanged, executorInstanceID, "Issue claimed by executor")
	if err != nil {
		return fmt.Errorf("failed to record event: %w", err)
	}

	return tx.Commit()
}

// GetExecutionState retrieves the execution state for an issue
func (s *SQLiteStorage) GetExecutionState(ctx context.Context, issueID string) (*types.IssueExecutionState, error) {
	query := `
		SELECT issue_id, executor_instance_id, state, checkpoint_data, started_at, updated_at
		FROM issue_execution_state
		WHERE issue_id = ?
	`

	var state types.IssueExecutionState
	err := s.db.QueryRowContext(ctx, query, issueID).Scan(
		&state.IssueID,
		&state.ExecutorInstanceID,
		&state.State,
		&state.CheckpointData,
		&state.StartedAt,
		&state.UpdatedAt,
	)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get execution state: %w", err)
	}

	return &state, nil
}

// UpdateExecutionState updates the state field of an execution state
// This enforces state transitions in the state machine atomically
func (s *SQLiteStorage) UpdateExecutionState(ctx context.Context, issueID string, newState types.ExecutionState) error {
	// Validate the new state
	if !newState.IsValid() {
		return fmt.Errorf("invalid execution state: %s", newState)
	}

	// Get current state to validate transition
	currentState, err := s.GetExecutionState(ctx, issueID)
	if err != nil {
		return fmt.Errorf("failed to get current state: %w", err)
	}
	if currentState == nil {
		return fmt.Errorf("issue %s has no execution state", issueID)
	}

	// Validate state transition
	if !isValidStateTransition(currentState.State, newState) {
		return fmt.Errorf("invalid state transition from %s to %s", currentState.State, newState)
	}

	// Atomically update the state with WHERE clause checking current state
	// This prevents race conditions where another transaction modifies state between our read and write
	query := `
		UPDATE issue_execution_state
		SET state = ?, updated_at = ?
		WHERE issue_id = ? AND state = ?
	`

	result, err := s.db.ExecContext(ctx, query, newState, time.Now(), issueID, currentState.State)
	if err != nil {
		return fmt.Errorf("failed to update execution state: %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rows == 0 {
		// Either the issue doesn't exist OR the state was modified concurrently
		// Check which case it is by re-reading the state
		checkState, err := s.GetExecutionState(ctx, issueID)
		if err != nil {
			return fmt.Errorf("failed to verify execution state: %w", err)
		}
		if checkState == nil {
			return fmt.Errorf("execution state not found for issue: %s", issueID)
		}
		// State changed concurrently - this is a conflict
		return fmt.Errorf("concurrent state modification detected: expected %s but found %s", currentState.State, checkState.State)
	}

	return nil
}

// SaveCheckpoint saves checkpoint data for an issue
func (s *SQLiteStorage) SaveCheckpoint(ctx context.Context, issueID string, checkpointData interface{}) error {
	// Marshal checkpoint data to JSON
	jsonData, err := json.Marshal(checkpointData)
	if err != nil {
		return fmt.Errorf("failed to marshal checkpoint data: %w", err)
	}

	query := `
		UPDATE issue_execution_state
		SET checkpoint_data = ?, updated_at = ?
		WHERE issue_id = ?
	`

	result, err := s.db.ExecContext(ctx, query, string(jsonData), time.Now(), issueID)
	if err != nil {
		return fmt.Errorf("failed to save checkpoint: %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rows == 0 {
		return fmt.Errorf("execution state not found for issue: %s", issueID)
	}

	return nil
}

// GetCheckpoint retrieves the checkpoint data for an issue
func (s *SQLiteStorage) GetCheckpoint(ctx context.Context, issueID string) (string, error) {
	query := `
		SELECT checkpoint_data
		FROM issue_execution_state
		WHERE issue_id = ?
	`

	var checkpointData string
	err := s.db.QueryRowContext(ctx, query, issueID).Scan(&checkpointData)

	if err == sql.ErrNoRows {
		return "", fmt.Errorf("execution state not found for issue: %s", issueID)
	}
	if err != nil {
		return "", fmt.Errorf("failed to get checkpoint: %w", err)
	}

	return checkpointData, nil
}

// ReleaseIssue releases an issue from execution, removing the execution state
func (s *SQLiteStorage) ReleaseIssue(ctx context.Context, issueID string) error {
	query := `
		DELETE FROM issue_execution_state
		WHERE issue_id = ?
	`

	result, err := s.db.ExecContext(ctx, query, issueID)
	if err != nil {
		return fmt.Errorf("failed to release issue: %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rows == 0 {
		return fmt.Errorf("execution state not found for issue: %s", issueID)
	}

	return nil
}

// ReleaseIssueAndReopen atomically releases an issue from execution and resets its status to open
// This is used when an error occurs during execution - the issue should become available for retry
func (s *SQLiteStorage) ReleaseIssueAndReopen(ctx context.Context, issueID, actor, errorComment string) error {
	// Start transaction for atomic release + status update + comment
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	// Delete execution state
	result, err := tx.ExecContext(ctx, `
		DELETE FROM issue_execution_state
		WHERE issue_id = ?
	`, issueID)
	if err != nil {
		return fmt.Errorf("failed to release issue: %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	// If no execution state existed, still continue to update status and add comment
	// This handles cases where the state might have been cleaned up already
	if rows == 0 {
		// Just log a warning, don't fail - we still want to reset status and add comment
		fmt.Fprintf(os.Stderr, "warning: no execution state found for issue %s during release\n", issueID)
	}

	// Update issue status back to open
	now := time.Now()
	_, err = tx.ExecContext(ctx, `
		UPDATE issues SET status = ?, updated_at = ?
		WHERE id = ?
	`, types.StatusOpen, now, issueID)
	if err != nil {
		return fmt.Errorf("failed to update issue status: %w", err)
	}

	// Add error comment if provided
	if errorComment != "" {
		_, err = tx.ExecContext(ctx, `
			INSERT INTO events (issue_id, event_type, actor, comment, created_at)
			VALUES (?, ?, ?, ?, ?)
		`, issueID, types.EventCommented, actor, errorComment, now)
		if err != nil {
			return fmt.Errorf("failed to add error comment: %w", err)
		}
	}

	// Record status change event
	_, err = tx.ExecContext(ctx, `
		INSERT INTO events (issue_id, event_type, actor, comment, created_at)
		VALUES (?, ?, ?, ?, ?)
	`, issueID, types.EventStatusChanged, actor, "Issue released due to error and reopened for retry", now)
	if err != nil {
		return fmt.Errorf("failed to record status change event: %w", err)
	}

	return tx.Commit()
}

// isValidStateTransition validates state machine transitions
func isValidStateTransition(from, to types.ExecutionState) bool {
	// Any state can transition to 'failed' (error escape hatch)
	if to == types.ExecutionStateFailed {
		return true
	}

	// Define valid state transitions
	// NOTE: This allows skipping optional phases (assessing, analyzing, gates)
	// to support configurations where AI supervision or quality gates are disabled
	validTransitions := map[types.ExecutionState][]types.ExecutionState{
		types.ExecutionStateClaimed: {
			types.ExecutionStateAssessing, // Normal path: assessment enabled
			types.ExecutionStateExecuting, // Skip assessment: AI supervision disabled
			types.ExecutionStateCompleted, // Skip all phases: everything disabled (edge case)
		},
		types.ExecutionStateAssessing: {
			types.ExecutionStateExecuting, // Normal path: proceed to execution
		},
		types.ExecutionStateExecuting: {
			types.ExecutionStateAnalyzing, // Normal path: AI analysis enabled
			types.ExecutionStateGates,     // Skip analysis: AI supervision disabled but gates enabled
			types.ExecutionStateCompleted, // Skip analysis and gates: both disabled
		},
		types.ExecutionStateAnalyzing: {
			types.ExecutionStateGates,     // Normal path: quality gates enabled
			types.ExecutionStateCompleted, // Skip gates: quality gates disabled
		},
		types.ExecutionStateGates: {
			types.ExecutionStateCompleted, // Normal path: gates passed or failed
		},
		types.ExecutionStateCompleted: {
			// Terminal state - no transitions
		},
		types.ExecutionStateFailed: {
			// Terminal error state - no transitions
		},
	}

	allowedStates, ok := validTransitions[from]
	if !ok {
		return false
	}

	for _, allowed := range allowedStates {
		if allowed == to {
			return true
		}
	}

	return false
}

// isUniqueConstraintError checks if an error is a unique constraint violation
func isUniqueConstraintError(err error) bool {
	// SQLite returns "UNIQUE constraint failed" in the error message
	return err != nil && (err.Error() == "UNIQUE constraint failed: issue_execution_state.issue_id" ||
		err.Error() == "PRIMARY KEY must be unique")
}
