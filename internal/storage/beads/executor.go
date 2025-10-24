package beads

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/steveyegge/vc/internal/types"
)

// ======================================================================
// EXECUTOR INSTANCE MANAGEMENT (VC extension table: vc_executor_instances)
// ======================================================================

// RegisterInstance registers a new executor instance
func (s *VCStorage) RegisterInstance(ctx context.Context, instance *types.ExecutorInstance) error {
	// Use INSERT ... ON CONFLICT DO UPDATE to handle re-registration (vc-130)
	// This allows executors to restart with the same ID
	// IMPORTANT: We use ON CONFLICT DO UPDATE instead of INSERT OR REPLACE because
	// REPLACE triggers DELETE, which cascades to execution_state.executor_instance_id (ON DELETE SET NULL)
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO vc_executor_instances (id, hostname, pid, version, started_at, last_heartbeat, status)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			hostname = excluded.hostname,
			pid = excluded.pid,
			version = excluded.version,
			started_at = excluded.started_at,
			last_heartbeat = excluded.last_heartbeat,
			status = excluded.status
	`, instance.InstanceID, instance.Hostname, instance.PID, instance.Version,
		instance.StartedAt, instance.LastHeartbeat, instance.Status)

	if err != nil {
		return fmt.Errorf("failed to register executor instance: %w", err)
	}

	return nil
}

// MarkInstanceStopped marks an executor instance as stopped
func (s *VCStorage) MarkInstanceStopped(ctx context.Context, instanceID string) error {
	result, err := s.db.ExecContext(ctx, `
		UPDATE vc_executor_instances
		SET status = 'stopped'
		WHERE id = ?
	`, instanceID)

	if err != nil {
		return fmt.Errorf("failed to mark instance as stopped: %w", err)
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
	defer func() { _ = rows.Close() }()

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

// CleanupStaleInstances marks instances as crashed and releases their claimed issues
func (s *VCStorage) CleanupStaleInstances(ctx context.Context, staleThresholdSeconds int) (int, error) {
	staleTime := time.Now().Add(-time.Duration(staleThresholdSeconds) * time.Second)

	// Start a transaction to ensure atomic cleanup of instances and their claims
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	// First, find all stale instances (running but heartbeat is old)
	staleQuery := `
		SELECT id
		FROM vc_executor_instances
		WHERE status = 'running'
		  AND last_heartbeat < ?
	`

	rows, err := tx.QueryContext(ctx, staleQuery, staleTime)
	if err != nil {
		return 0, fmt.Errorf("failed to query stale instances: %w", err)
	}

	var staleInstanceIDs []string
	for rows.Next() {
		var instanceID string
		if err := rows.Scan(&instanceID); err != nil {
			_ = rows.Close()
			return 0, fmt.Errorf("failed to scan instance ID: %w", err)
		}
		staleInstanceIDs = append(staleInstanceIDs, instanceID)
	}
	_ = rows.Close()

	if err = rows.Err(); err != nil {
		return 0, fmt.Errorf("error iterating stale instances: %w", err)
	}

	// Also find instances that are already stopped but still have claims (orphaned claims)
	orphanedQuery := `
		SELECT DISTINCT executor_instance_id
		FROM vc_issue_execution_state
		WHERE executor_instance_id IN (
			SELECT id FROM vc_executor_instances WHERE status = 'stopped'
		)
	`

	orphanedRows, err := tx.QueryContext(ctx, orphanedQuery)
	if err != nil {
		return 0, fmt.Errorf("failed to query orphaned claims: %w", err)
	}

	var orphanedInstanceIDs []string
	for orphanedRows.Next() {
		var instanceID string
		if err := orphanedRows.Scan(&instanceID); err != nil {
			_ = orphanedRows.Close()
			return 0, fmt.Errorf("failed to scan orphaned instance ID: %w", err)
		}
		orphanedInstanceIDs = append(orphanedInstanceIDs, instanceID)
	}
	_ = orphanedRows.Close()

	if err = orphanedRows.Err(); err != nil {
		return 0, fmt.Errorf("error iterating orphaned instances: %w", err)
	}

	// Combine both lists (stale and orphaned)
	allInstanceIDs := append(staleInstanceIDs, orphanedInstanceIDs...)

	// If no instances to clean up, return early
	if len(allInstanceIDs) == 0 {
		return 0, nil
	}

	// For each instance (stale or orphaned), find and release all claimed issues
	for _, instanceID := range allInstanceIDs {
		// Find all issues claimed by this instance
		claimedIssuesQuery := `
			SELECT issue_id
			FROM vc_issue_execution_state
			WHERE executor_instance_id = ?
		`

		issueRows, err := tx.QueryContext(ctx, claimedIssuesQuery, instanceID)
		if err != nil {
			return 0, fmt.Errorf("failed to query claimed issues for instance %s: %w", instanceID, err)
		}

		var issueIDs []string
		for issueRows.Next() {
			var issueID string
			if err := issueRows.Scan(&issueID); err != nil {
				_ = issueRows.Close()
				return 0, fmt.Errorf("failed to scan issue ID: %w", err)
			}
			issueIDs = append(issueIDs, issueID)
		}
		_ = issueRows.Close()

		if err = issueRows.Err(); err != nil {
			return 0, fmt.Errorf("error iterating claimed issues: %w", err)
		}

		// Release each claimed issue
		for _, issueID := range issueIDs {
			// Clear the executor claim but preserve checkpoint data
			// This allows recovery/resume after cleanup
			_, err = tx.ExecContext(ctx, `
				UPDATE vc_issue_execution_state
				SET executor_instance_id = NULL,
				    state = ?,
				    updated_at = ?
				WHERE issue_id = ?
			`, types.ExecutionStatePending, time.Now(), issueID)
			if err != nil {
				return 0, fmt.Errorf("failed to release execution state for issue %s: %w", issueID, err)
			}

			// Reset issue status to 'open' and clear closed_at
			_, err = tx.ExecContext(ctx, `
				UPDATE issues
				SET status = ?, updated_at = ?, closed_at = NULL
				WHERE id = ?
			`, "open", time.Now(), issueID)
			if err != nil {
				return 0, fmt.Errorf("failed to reset issue status for %s: %w", issueID, err)
			}

			// Store event explaining why the issue was released
			var message string
			isStale := false
			for _, staleID := range staleInstanceIDs {
				if staleID == instanceID {
					isStale = true
					break
				}
			}
			if isStale {
				message = fmt.Sprintf("Issue automatically released - executor instance %s became stale (no heartbeat for %d seconds)", instanceID, staleThresholdSeconds)
			} else {
				message = fmt.Sprintf("Issue automatically released - executor instance %s was already stopped but claim remained (orphaned)", instanceID)
			}

			// Store as agent event
			eventData := map[string]interface{}{
				"instance_id": instanceID,
				"reason":      message,
			}
			eventDataJSON, _ := json.Marshal(eventData)

			_, err = tx.ExecContext(ctx, `
				INSERT INTO vc_agent_events (issue_id, type, message, data, timestamp)
				VALUES (?, ?, ?, ?, ?)
			`, issueID, "issue_released", message, string(eventDataJSON), time.Now())
			if err != nil {
				// Don't fail cleanup if event storage fails
				fmt.Fprintf(os.Stderr, "warning: failed to store release event for issue %s: %v\n", issueID, err)
			}
		}
	}

	// Mark all stale instances as 'crashed'
	if len(staleInstanceIDs) > 0 {
		updateQuery := `
			UPDATE vc_executor_instances
			SET status = 'crashed'
			WHERE status = 'running'
			  AND last_heartbeat < ?
		`

		_, err = tx.ExecContext(ctx, updateQuery, staleTime)
		if err != nil {
			return 0, fmt.Errorf("failed to mark stale instances as crashed: %w", err)
		}
	}

	// Commit the transaction
	if err = tx.Commit(); err != nil {
		return 0, fmt.Errorf("failed to commit transaction: %w", err)
	}

	// Return total number of instances cleaned (stale + orphaned)
	return len(allInstanceIDs), nil
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
	defer func() { _ = tx.Rollback() }() // Rollback if not committed

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
// Returns (nil, nil) if no execution state exists (not an error condition)
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
			// No execution state exists - this is not an error condition
			return nil, nil
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
		return fmt.Errorf("failed to get current execution state: %w", err)
	}
	if currentExecState == nil {
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

// ReleaseIssue releases an issue claim (deletes execution state)
func (s *VCStorage) ReleaseIssue(ctx context.Context, issueID string) error {
	// Check if execution state exists first
	state, err := s.GetExecutionState(ctx, issueID)
	if err != nil {
		return fmt.Errorf("failed to check execution state for issue %s: %w", issueID, err)
	}
	if state == nil {
		return fmt.Errorf("execution state not found for issue %s", issueID)
	}

	// Delete the execution state
	result, err := s.db.ExecContext(ctx, `
		DELETE FROM vc_issue_execution_state
		WHERE issue_id = ?
	`, issueID)

	if err != nil {
		return fmt.Errorf("failed to delete execution state: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to check rows affected: %w", err)
	}

	if rowsAffected == 0 {
		return fmt.Errorf("execution state not found for issue %s", issueID)
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
		ORDER BY started_at ASC
	`, issueID)
	if err != nil {
		return nil, fmt.Errorf("failed to query execution history: %w", err)
	}
	defer func() { _ = rows.Close() }()

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
