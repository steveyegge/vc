package beads

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/steveyegge/vc/internal/types"
)

// ======================================================================
// EXECUTOR INSTANCE MANAGEMENT (VC extension table: vc_executor_instances)
// ======================================================================

// RegisterInstance registers a new executor instance
func (s *VCStorage) RegisterInstance(ctx context.Context, instance *types.ExecutorInstance) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO vc_executor_instances (id, hostname, pid, version, started_at, last_heartbeat, status)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`, instance.InstanceID, instance.Hostname, instance.PID, instance.Version,
		instance.StartedAt, instance.LastHeartbeat, instance.Status)

	if err != nil {
		return fmt.Errorf("failed to register executor instance: %w", err)
	}

	return nil
}

// UpdateHeartbeat updates the last heartbeat time for an executor instance
func (s *VCStorage) UpdateHeartbeat(ctx context.Context, instanceID string) error {
	result, err := s.db.ExecContext(ctx, `
		UPDATE vc_executor_instances
		SET last_heartbeat = ?
		WHERE id = ?
	`, time.Now(), instanceID)

	if err != nil {
		return fmt.Errorf("failed to update heartbeat: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to check rows affected: %w", err)
	}

	if rowsAffected == 0 {
		return fmt.Errorf("executor instance %s not found", instanceID)
	}

	return nil
}

// GetActiveInstances retrieves all active executor instances
func (s *VCStorage) GetActiveInstances(ctx context.Context) ([]*types.ExecutorInstance, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, hostname, pid, version, started_at, last_heartbeat, status
		FROM vc_executor_instances
		WHERE status = 'running'
		ORDER BY started_at
	`)
	if err != nil {
		return nil, fmt.Errorf("failed to query active instances: %w", err)
	}
	defer rows.Close()

	var instances []*types.ExecutorInstance
	for rows.Next() {
		var inst types.ExecutorInstance
		if err := rows.Scan(&inst.InstanceID, &inst.Hostname, &inst.PID, &inst.Version,
			&inst.StartedAt, &inst.LastHeartbeat, &inst.Status); err != nil {
			return nil, fmt.Errorf("failed to scan instance: %w", err)
		}
		instances = append(instances, &inst)
	}

	return instances, rows.Err()
}

// CleanupStaleInstances marks instances as crashed if they haven't sent heartbeat
func (s *VCStorage) CleanupStaleInstances(ctx context.Context, staleThresholdSeconds int) (int, error) {
	staleTime := time.Now().Add(-time.Duration(staleThresholdSeconds) * time.Second)

	result, err := s.db.ExecContext(ctx, `
		UPDATE vc_executor_instances
		SET status = 'crashed'
		WHERE status = 'running'
		  AND last_heartbeat < ?
	`, staleTime)

	if err != nil {
		return 0, fmt.Errorf("failed to cleanup stale instances: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("failed to get rows affected: %w", err)
	}

	return int(rowsAffected), nil
}

// DeleteOldStoppedInstances deletes old stopped/crashed instances
func (s *VCStorage) DeleteOldStoppedInstances(ctx context.Context, olderThanSeconds int, maxToKeep int) (int, error) {
	cutoffTime := time.Now().Add(-time.Duration(olderThanSeconds) * time.Second)

	// Delete old stopped/crashed instances, keeping the most recent maxToKeep
	result, err := s.db.ExecContext(ctx, `
		DELETE FROM vc_executor_instances
		WHERE status IN ('stopped', 'crashed')
		  AND started_at < ?
		  AND id NOT IN (
		    SELECT id FROM vc_executor_instances
		    WHERE status IN ('stopped', 'crashed')
		    ORDER BY started_at DESC
		    LIMIT ?
		  )
	`, cutoffTime, maxToKeep)

	if err != nil {
		return 0, fmt.Errorf("failed to delete old instances: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("failed to get rows affected: %w", err)
	}

	return int(rowsAffected), nil
}

// ======================================================================
// ISSUE EXECUTION STATE (VC extension table: vc_issue_execution_state)
// ======================================================================

// ClaimIssue atomically claims an issue for execution
func (s *VCStorage) ClaimIssue(ctx context.Context, issueID, executorInstanceID string) error {
	// Begin transaction to ensure atomicity
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback() // Rollback if not committed

	// First, check if issue is already claimed or being executed
	var existingClaim string
	err = tx.QueryRowContext(ctx, `
		SELECT executor_instance_id
		FROM vc_issue_execution_state
		WHERE issue_id = ? AND state IN ('claimed', 'assessing', 'executing', 'analyzing', 'gates', 'committing')
	`, issueID).Scan(&existingClaim)

	if err == nil {
		// Already claimed or being executed
		return fmt.Errorf("issue %s already claimed by %s", issueID, existingClaim)
	} else if err != sql.ErrNoRows {
		return fmt.Errorf("failed to check existing claim: %w", err)
	}

	// Insert or update claim
	_, err = tx.ExecContext(ctx, `
		INSERT INTO vc_issue_execution_state (issue_id, executor_instance_id, claimed_at, state, updated_at)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(issue_id) DO UPDATE SET
			executor_instance_id = excluded.executor_instance_id,
			claimed_at = excluded.claimed_at,
			state = ?,
			updated_at = excluded.updated_at
	`, issueID, executorInstanceID, time.Now(), types.ExecutionStateClaimed, time.Now(), types.ExecutionStateClaimed)

	if err != nil {
		return fmt.Errorf("failed to claim issue: %w", err)
	}

	// Update issue status to in_progress in Beads (through transaction)
	_, err = tx.ExecContext(ctx, `
		UPDATE issues SET status = ?, updated_at = ? WHERE id = ?
	`, "in_progress", time.Now(), issueID)

	if err != nil {
		return fmt.Errorf("failed to update issue status: %w", err)
	}

	// Commit transaction
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	return nil
}

// GetExecutionState retrieves execution state for an issue
func (s *VCStorage) GetExecutionState(ctx context.Context, issueID string) (*types.IssueExecutionState, error) {
	var state types.IssueExecutionState
	var executorInstanceID sql.NullString
	var claimedAt sql.NullTime
	var checkpointData sql.NullString
	var errorMessage sql.NullString

	err := s.db.QueryRowContext(ctx, `
		SELECT issue_id, executor_instance_id, claimed_at, state, checkpoint_data, error_message, updated_at
		FROM vc_issue_execution_state
		WHERE issue_id = ?
	`, issueID).Scan(
		&state.IssueID,
		&executorInstanceID,
		&claimedAt,
		&state.State,
		&checkpointData,
		&errorMessage,
		&state.UpdatedAt,
	)

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("no execution state for issue %s", issueID)
		}
		return nil, fmt.Errorf("failed to query execution state: %w", err)
	}

	if executorInstanceID.Valid {
		state.ExecutorInstanceID = executorInstanceID.String
	}
	if claimedAt.Valid {
		state.ClaimedAt = claimedAt.Time
	}
	if checkpointData.Valid {
		state.CheckpointData = checkpointData.String
	}
	if errorMessage.Valid {
		state.ErrorMessage = errorMessage.String
	}

	return &state, nil
}

// UpdateExecutionState updates the execution state with validation
// Validates that the state transition is valid according to the execution state machine
func (s *VCStorage) UpdateExecutionState(ctx context.Context, issueID string, newState types.ExecutionState) error {
	// Validate that the new state is valid
	if !newState.IsValid() {
		return fmt.Errorf("invalid execution state: %s", newState)
	}

	// Get current state to validate transition
	currentExecState, err := s.GetExecutionState(ctx, issueID)
	if err != nil {
		// If no execution state exists, only allow transition to pending or claimed
		if newState == types.ExecutionStatePending || newState == types.ExecutionStateClaimed {
			// Create new execution state record (use ON CONFLICT in case of race)
			_, err := s.db.ExecContext(ctx, `
				INSERT INTO vc_issue_execution_state (issue_id, state, updated_at)
				VALUES (?, ?, ?)
				ON CONFLICT(issue_id) DO UPDATE SET
					state = excluded.state,
					updated_at = excluded.updated_at
			`, issueID, newState, time.Now())
			if err != nil {
				return fmt.Errorf("failed to create execution state: %w", err)
			}
			return nil
		}
		return fmt.Errorf("cannot transition to %s without existing execution state", newState)
	}

	// Validate state transition
	if !currentExecState.State.CanTransitionTo(newState) {
		return fmt.Errorf("invalid state transition: cannot transition from %s to %s (valid transitions: %v)",
			currentExecState.State, newState, currentExecState.State.ValidTransitions())
	}

	// Update state
	_, err = s.db.ExecContext(ctx, `
		UPDATE vc_issue_execution_state
		SET state = ?, updated_at = ?
		WHERE issue_id = ?
	`, newState, time.Now(), issueID)

	if err != nil {
		return fmt.Errorf("failed to update execution state: %w", err)
	}

	return nil
}

// SaveCheckpoint saves checkpoint data for an issue
func (s *VCStorage) SaveCheckpoint(ctx context.Context, issueID string, checkpointData interface{}) error {
	// Marshal checkpoint data to JSON
	dataJSON, err := json.Marshal(checkpointData)
	if err != nil {
		return fmt.Errorf("failed to marshal checkpoint data: %w", err)
	}

	_, err = s.db.ExecContext(ctx, `
		UPDATE vc_issue_execution_state
		SET checkpoint_data = ?, updated_at = ?
		WHERE issue_id = ?
	`, string(dataJSON), time.Now(), issueID)

	if err != nil {
		return fmt.Errorf("failed to save checkpoint: %w", err)
	}

	return nil
}

// GetCheckpoint retrieves checkpoint data for an issue
func (s *VCStorage) GetCheckpoint(ctx context.Context, issueID string) (string, error) {
	var checkpointData sql.NullString

	err := s.db.QueryRowContext(ctx, `
		SELECT checkpoint_data
		FROM vc_issue_execution_state
		WHERE issue_id = ?
	`, issueID).Scan(&checkpointData)

	if err != nil {
		if err == sql.ErrNoRows {
			return "", nil // No checkpoint
		}
		return "", fmt.Errorf("failed to query checkpoint: %w", err)
	}

	if checkpointData.Valid {
		return checkpointData.String, nil
	}

	return "", nil
}

// ReleaseIssue releases an issue claim (keeps execution state)
func (s *VCStorage) ReleaseIssue(ctx context.Context, issueID string) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE vc_issue_execution_state
		SET state = ?, updated_at = ?
		WHERE issue_id = ?
	`, types.ExecutionStateCompleted, time.Now(), issueID)

	if err != nil {
		return fmt.Errorf("failed to release issue: %w", err)
	}

	return nil
}

// ReleaseIssueAndReopen releases claim and reopens the issue
func (s *VCStorage) ReleaseIssueAndReopen(ctx context.Context, issueID, actor, errorComment string) error {
	// Update execution state to failed
	_, err := s.db.ExecContext(ctx, `
		UPDATE vc_issue_execution_state
		SET state = ?, error_message = ?, updated_at = ?
		WHERE issue_id = ?
	`, types.ExecutionStateFailed, errorComment, time.Now(), issueID)

	if err != nil {
		return fmt.Errorf("failed to update execution state: %w", err)
	}

	// Reopen issue in Beads
	err = s.Storage.UpdateIssue(ctx, issueID, map[string]interface{}{
		"status": "open",
	}, actor)

	if err != nil {
		return fmt.Errorf("failed to reopen issue: %w", err)
	}

	// Add comment explaining the failure
	if errorComment != "" {
		err = s.Storage.AddComment(ctx, issueID, actor, errorComment)
		if err != nil {
			return fmt.Errorf("failed to add error comment: %w", err)
		}
	}

	return nil
}

// ======================================================================
// EXECUTION HISTORY (VC extension table: vc_execution_history)
// ======================================================================

// RecordExecutionAttempt records an execution attempt in history
func (s *VCStorage) RecordExecutionAttempt(ctx context.Context, attempt *types.ExecutionAttempt) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO vc_execution_history (issue_id, executor_instance_id, attempt_number, started_at, completed_at, success, exit_code, summary, output_sample, error_sample)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, attempt.IssueID, attempt.ExecutorInstanceID, attempt.AttemptNumber, attempt.StartedAt, attempt.CompletedAt,
		attempt.Success, attempt.ExitCode, attempt.Summary, attempt.OutputSample, attempt.ErrorSample)

	if err != nil {
		return fmt.Errorf("failed to record execution attempt: %w", err)
	}

	return nil
}

// GetExecutionHistory retrieves execution history for an issue
func (s *VCStorage) GetExecutionHistory(ctx context.Context, issueID string) ([]*types.ExecutionAttempt, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, issue_id, executor_instance_id, attempt_number, started_at, completed_at, success, exit_code, summary, output_sample, error_sample
		FROM vc_execution_history
		WHERE issue_id = ?
		ORDER BY started_at DESC
	`, issueID)
	if err != nil {
		return nil, fmt.Errorf("failed to query execution history: %w", err)
	}
	defer rows.Close()

	var history []*types.ExecutionAttempt
	for rows.Next() {
		var attempt types.ExecutionAttempt
		var completedAt sql.NullTime
		var success sql.NullBool
		var exitCode sql.NullInt64

		if err := rows.Scan(&attempt.ID, &attempt.IssueID, &attempt.ExecutorInstanceID,
			&attempt.AttemptNumber, &attempt.StartedAt, &completedAt, &success, &exitCode,
			&attempt.Summary, &attempt.OutputSample, &attempt.ErrorSample); err != nil {
			return nil, fmt.Errorf("failed to scan execution attempt: %w", err)
		}

		if completedAt.Valid {
			attempt.CompletedAt = &completedAt.Time
		}
		if success.Valid {
			successVal := success.Bool
			attempt.Success = &successVal
		}
		if exitCode.Valid {
			exitCodeVal := int(exitCode.Int64)
			attempt.ExitCode = &exitCodeVal
		}

		history = append(history, &attempt)
	}

	return history, rows.Err()
}

// ======================================================================
// CONFIG (delegate to Beads)
// ======================================================================

// GetConfig, SetConfig delegate to Beads
// (Already available via embedded beads.Storage)
