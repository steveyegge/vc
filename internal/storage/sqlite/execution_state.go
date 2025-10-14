package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
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
	defer tx.Rollback()

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
// This enforces state transitions in the state machine
func (s *SQLiteStorage) UpdateExecutionState(ctx context.Context, issueID string, state types.ExecutionState) error {
	// Validate the new state
	if !state.IsValid() {
		return fmt.Errorf("invalid execution state: %s", state)
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
	if !isValidStateTransition(currentState.State, state) {
		return fmt.Errorf("invalid state transition from %s to %s", currentState.State, state)
	}

	// Update the state
	query := `
		UPDATE issue_execution_state
		SET state = ?, updated_at = ?
		WHERE issue_id = ?
	`

	result, err := s.db.ExecContext(ctx, query, state, time.Now(), issueID)
	if err != nil {
		return fmt.Errorf("failed to update execution state: %w", err)
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

// isValidStateTransition validates state machine transitions
func isValidStateTransition(from, to types.ExecutionState) bool {
	// Define valid state transitions
	validTransitions := map[types.ExecutionState][]types.ExecutionState{
		types.ExecutionStateClaimed: {
			types.ExecutionStateAssessing,
		},
		types.ExecutionStateAssessing: {
			types.ExecutionStateExecuting,
		},
		types.ExecutionStateExecuting: {
			types.ExecutionStateAnalyzing,
		},
		types.ExecutionStateAnalyzing: {
			types.ExecutionStateGates,
		},
		types.ExecutionStateGates: {
			types.ExecutionStateCompleted,
		},
		types.ExecutionStateCompleted: {
			// Terminal state - no transitions
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
