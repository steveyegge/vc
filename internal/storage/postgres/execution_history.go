package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/steveyegge/vc/internal/types"
)

// GetExecutionHistory retrieves all execution attempts for an issue,
// ordered chronologically (oldest first).
func (s *PostgresStorage) GetExecutionHistory(ctx context.Context, issueID string) ([]*types.ExecutionAttempt, error) {
	query := `
		SELECT id, issue_id, executor_instance_id, attempt_number,
		       started_at, completed_at, success, exit_code,
		       summary, output_sample, error_sample
		FROM execution_history
		WHERE issue_id = $1
		ORDER BY started_at ASC
	`

	rows, err := s.pool.Query(ctx, query, issueID)
	if err != nil {
		return nil, fmt.Errorf("failed to query execution history: %w", err)
	}
	defer rows.Close()

	var attempts []*types.ExecutionAttempt
	for rows.Next() {
		attempt := &types.ExecutionAttempt{}
		var completedAt *time.Time
		var success *bool
		var exitCode *int

		err := rows.Scan(
			&attempt.ID,
			&attempt.IssueID,
			&attempt.ExecutorInstanceID,
			&attempt.AttemptNumber,
			&attempt.StartedAt,
			&completedAt,
			&success,
			&exitCode,
			&attempt.Summary,
			&attempt.OutputSample,
			&attempt.ErrorSample,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan execution attempt: %w", err)
		}

		// Assign nullable fields
		attempt.CompletedAt = completedAt
		attempt.Success = success
		attempt.ExitCode = exitCode

		attempts = append(attempts, attempt)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating execution history rows: %w", err)
	}

	return attempts, nil
}

// RecordExecutionAttempt stores a new execution attempt or updates an existing one.
// If attempt.ID is 0, a new record is created and the ID is populated.
// If attempt.ID is > 0, the existing record is updated.
func (s *PostgresStorage) RecordExecutionAttempt(ctx context.Context, attempt *types.ExecutionAttempt) error {
	// If ID is 0, this is a new attempt - insert it (will validate after auto-assigning attempt_number)
	if attempt.ID == 0 {
		return s.insertExecutionAttempt(ctx, attempt)
	}

	// For updates, validate first
	if err := attempt.Validate(); err != nil {
		return fmt.Errorf("invalid execution attempt: %w", err)
	}

	// Update the existing attempt
	return s.updateExecutionAttempt(ctx, attempt)
}

// insertExecutionAttempt inserts a new execution attempt record.
// The attempt.ID field is populated with the new record's ID.
func (s *PostgresStorage) insertExecutionAttempt(ctx context.Context, attempt *types.ExecutionAttempt) error {
	// Auto-assign attempt number if not set
	if attempt.AttemptNumber == 0 {
		maxAttempt, err := s.getMaxAttemptNumber(ctx, attempt.IssueID)
		if err != nil {
			return fmt.Errorf("failed to get max attempt number: %w", err)
		}
		attempt.AttemptNumber = maxAttempt + 1
	}

	// Set default timestamps if not set
	if attempt.StartedAt.IsZero() {
		attempt.StartedAt = time.Now()
	}

	// Validate after auto-assigning fields
	if err := attempt.Validate(); err != nil {
		return fmt.Errorf("invalid execution attempt: %w", err)
	}

	query := `
		INSERT INTO execution_history (
			issue_id, executor_instance_id, attempt_number,
			started_at, completed_at, success, exit_code,
			summary, output_sample, error_sample
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		RETURNING id
	`

	err := s.pool.QueryRow(ctx, query,
		attempt.IssueID,
		attempt.ExecutorInstanceID,
		attempt.AttemptNumber,
		attempt.StartedAt,
		attempt.CompletedAt,
		attempt.Success,
		attempt.ExitCode,
		attempt.Summary,
		attempt.OutputSample,
		attempt.ErrorSample,
	).Scan(&attempt.ID)

	if err != nil {
		return fmt.Errorf("failed to insert execution attempt: %w", err)
	}

	return nil
}

// updateExecutionAttempt updates an existing execution attempt record.
func (s *PostgresStorage) updateExecutionAttempt(ctx context.Context, attempt *types.ExecutionAttempt) error {
	query := `
		UPDATE execution_history
		SET completed_at = $1,
		    success = $2,
		    exit_code = $3,
		    summary = $4,
		    output_sample = $5,
		    error_sample = $6
		WHERE id = $7
	`

	cmdTag, err := s.pool.Exec(ctx, query,
		attempt.CompletedAt,
		attempt.Success,
		attempt.ExitCode,
		attempt.Summary,
		attempt.OutputSample,
		attempt.ErrorSample,
		attempt.ID,
	)
	if err != nil {
		return fmt.Errorf("failed to update execution attempt: %w", err)
	}

	if cmdTag.RowsAffected() == 0 {
		return fmt.Errorf("execution attempt %d not found", attempt.ID)
	}

	return nil
}

// getMaxAttemptNumber returns the highest attempt number for an issue.
// Returns 0 if no attempts exist.
func (s *PostgresStorage) getMaxAttemptNumber(ctx context.Context, issueID string) (int, error) {
	query := `
		SELECT COALESCE(MAX(attempt_number), 0)
		FROM execution_history
		WHERE issue_id = $1
	`

	var maxAttempt int
	err := s.pool.QueryRow(ctx, query, issueID).Scan(&maxAttempt)
	if err != nil && err != pgx.ErrNoRows {
		return 0, fmt.Errorf("failed to query max attempt number: %w", err)
	}

	return maxAttempt, nil
}
