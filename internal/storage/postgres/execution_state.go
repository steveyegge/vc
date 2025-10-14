package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/steveyegge/vc/internal/types"
)

// ClaimIssue atomically claims an issue for execution by an executor instance
// This prevents double-claiming by using INSERT which will fail if the issue is already claimed
func (s *PostgresStorage) ClaimIssue(ctx context.Context, issueID, executorInstanceID string) error {
	// Start transaction for atomic claim + issue status update
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	// Verify the issue exists and is in 'open' status
	var issueStatus string
	err = tx.QueryRow(ctx, "SELECT status FROM issues WHERE id = $1", issueID).Scan(&issueStatus)
	if err == pgx.ErrNoRows {
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
	err = tx.QueryRow(ctx, "SELECT 1 FROM executor_instances WHERE instance_id = $1", executorInstanceID).Scan(&instanceExists)
	if err == pgx.ErrNoRows {
		return fmt.Errorf("executor instance not found: %s", executorInstanceID)
	}
	if err != nil {
		return fmt.Errorf("failed to check executor instance: %w", err)
	}

	// Attempt to insert execution state (will fail if already claimed via unique constraint)
	now := time.Now()
	_, err = tx.Exec(ctx, `
		INSERT INTO issue_execution_state (
			issue_id, executor_instance_id, state, checkpoint_data, started_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6)
	`, issueID, executorInstanceID, types.ExecutionStateClaimed, json.RawMessage("{}"), now, now)
	if err != nil {
		// Check if it's a unique constraint violation (issue already claimed)
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return fmt.Errorf("issue %s is already claimed by another executor", issueID)
		}
		return fmt.Errorf("failed to claim issue: %w", err)
	}

	// Update issue status to in_progress
	_, err = tx.Exec(ctx, `
		UPDATE issues SET status = $1, updated_at = $2
		WHERE id = $3
	`, types.StatusInProgress, now, issueID)
	if err != nil {
		return fmt.Errorf("failed to update issue status: %w", err)
	}

	// Record event
	_, err = tx.Exec(ctx, `
		INSERT INTO events (issue_id, event_type, actor, comment)
		VALUES ($1, $2, $3, $4)
	`, issueID, types.EventStatusChanged, executorInstanceID, "Issue claimed by executor")
	if err != nil {
		return fmt.Errorf("failed to record event: %w", err)
	}

	return tx.Commit(ctx)
}

// GetExecutionState retrieves the execution state for an issue
func (s *PostgresStorage) GetExecutionState(ctx context.Context, issueID string) (*types.IssueExecutionState, error) {
	query := `
		SELECT issue_id, executor_instance_id, state, checkpoint_data, started_at, updated_at
		FROM issue_execution_state
		WHERE issue_id = $1
	`

	var state types.IssueExecutionState
	var checkpointData json.RawMessage
	err := s.pool.QueryRow(ctx, query, issueID).Scan(
		&state.IssueID,
		&state.ExecutorInstanceID,
		&state.State,
		&checkpointData,
		&state.StartedAt,
		&state.UpdatedAt,
	)

	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get execution state: %w", err)
	}

	// Convert JSONB to string for compatibility with existing interface
	state.CheckpointData = string(checkpointData)

	return &state, nil
}

// UpdateExecutionState updates the state field of an execution state
// This enforces state transitions in the state machine atomically
func (s *PostgresStorage) UpdateExecutionState(ctx context.Context, issueID string, newState types.ExecutionState) error {
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
		SET state = $1, updated_at = $2
		WHERE issue_id = $3 AND state = $4
	`

	result, err := s.pool.Exec(ctx, query, newState, time.Now(), issueID, currentState.State)
	if err != nil {
		return fmt.Errorf("failed to update execution state: %w", err)
	}

	rows := result.RowsAffected()
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
func (s *PostgresStorage) SaveCheckpoint(ctx context.Context, issueID string, checkpointData interface{}) error {
	// Marshal checkpoint data to JSON
	jsonData, err := json.Marshal(checkpointData)
	if err != nil {
		return fmt.Errorf("failed to marshal checkpoint data: %w", err)
	}

	query := `
		UPDATE issue_execution_state
		SET checkpoint_data = $1, updated_at = $2
		WHERE issue_id = $3
	`

	result, err := s.pool.Exec(ctx, query, json.RawMessage(jsonData), time.Now(), issueID)
	if err != nil {
		return fmt.Errorf("failed to save checkpoint: %w", err)
	}

	rows := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("execution state not found for issue: %s", issueID)
	}

	return nil
}

// GetCheckpoint retrieves the checkpoint data for an issue
func (s *PostgresStorage) GetCheckpoint(ctx context.Context, issueID string) (string, error) {
	query := `
		SELECT checkpoint_data
		FROM issue_execution_state
		WHERE issue_id = $1
	`

	var checkpointData json.RawMessage
	err := s.pool.QueryRow(ctx, query, issueID).Scan(&checkpointData)

	if err == pgx.ErrNoRows {
		return "", fmt.Errorf("execution state not found for issue: %s", issueID)
	}
	if err != nil {
		return "", fmt.Errorf("failed to get checkpoint: %w", err)
	}

	return string(checkpointData), nil
}

// ReleaseIssue releases an issue from execution, removing the execution state
func (s *PostgresStorage) ReleaseIssue(ctx context.Context, issueID string) error {
	query := `
		DELETE FROM issue_execution_state
		WHERE issue_id = $1
	`

	result, err := s.pool.Exec(ctx, query, issueID)
	if err != nil {
		return fmt.Errorf("failed to release issue: %w", err)
	}

	rows := result.RowsAffected()
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
