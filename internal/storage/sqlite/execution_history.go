package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/steveyegge/vc/internal/types"
)

// GetExecutionHistory retrieves all execution attempts for an issue,
// ordered chronologically (oldest first).
func (s *SQLiteStorage) GetExecutionHistory(ctx context.Context, issueID string) ([]*types.ExecutionAttempt, error) {
	query := `
		SELECT id, issue_id, executor_instance_id, attempt_number,
		       started_at, completed_at, success, exit_code,
		       summary, output_sample, error_sample
		FROM execution_history
		WHERE issue_id = ?
		ORDER BY started_at ASC
	`

	rows, err := s.db.QueryContext(ctx, query, issueID)
	if err != nil {
		return nil, fmt.Errorf("failed to query execution history: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var attempts []*types.ExecutionAttempt
	for rows.Next() {
		attempt := &types.ExecutionAttempt{}
		var completedAt sql.NullTime
		var success sql.NullBool
		var exitCode sql.NullInt64

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

		// Convert nullable fields
		if completedAt.Valid {
			attempt.CompletedAt = &completedAt.Time
		}
		if success.Valid {
			attempt.Success = &success.Bool
		}
		if exitCode.Valid {
			exitCodeInt := int(exitCode.Int64)
			attempt.ExitCode = &exitCodeInt
		}

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
func (s *SQLiteStorage) RecordExecutionAttempt(ctx context.Context, attempt *types.ExecutionAttempt) error {
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
func (s *SQLiteStorage) insertExecutionAttempt(ctx context.Context, attempt *types.ExecutionAttempt) error {
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
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`

	result, err := s.db.ExecContext(ctx, query,
		attempt.IssueID,
		attempt.ExecutorInstanceID,
		attempt.AttemptNumber,
		attempt.StartedAt,
		sqlNullTime(attempt.CompletedAt),
		sqlNullBool(attempt.Success),
		sqlNullInt(attempt.ExitCode),
		attempt.Summary,
		attempt.OutputSample,
		attempt.ErrorSample,
	)
	if err != nil {
		return fmt.Errorf("failed to insert execution attempt: %w", err)
	}

	// Populate the ID field
	id, err := result.LastInsertId()
	if err != nil {
		return fmt.Errorf("failed to get last insert ID: %w", err)
	}
	attempt.ID = id

	return nil
}

// updateExecutionAttempt updates an existing execution attempt record.
func (s *SQLiteStorage) updateExecutionAttempt(ctx context.Context, attempt *types.ExecutionAttempt) error {
	query := `
		UPDATE execution_history
		SET completed_at = ?,
		    success = ?,
		    exit_code = ?,
		    summary = ?,
		    output_sample = ?,
		    error_sample = ?
		WHERE id = ?
	`

	result, err := s.db.ExecContext(ctx, query,
		sqlNullTime(attempt.CompletedAt),
		sqlNullBool(attempt.Success),
		sqlNullInt(attempt.ExitCode),
		attempt.Summary,
		attempt.OutputSample,
		attempt.ErrorSample,
		attempt.ID,
	)
	if err != nil {
		return fmt.Errorf("failed to update execution attempt: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}
	if rowsAffected == 0 {
		return fmt.Errorf("execution attempt %d not found", attempt.ID)
	}

	return nil
}

// getMaxAttemptNumber returns the highest attempt number for an issue.
// Returns 0 if no attempts exist.
func (s *SQLiteStorage) getMaxAttemptNumber(ctx context.Context, issueID string) (int, error) {
	query := `
		SELECT COALESCE(MAX(attempt_number), 0)
		FROM execution_history
		WHERE issue_id = ?
	`

	var maxAttempt int
	err := s.db.QueryRowContext(ctx, query, issueID).Scan(&maxAttempt)
	if err != nil {
		return 0, fmt.Errorf("failed to query max attempt number: %w", err)
	}

	return maxAttempt, nil
}

// Helper functions for nullable types

func sqlNullTime(t *time.Time) sql.NullTime {
	if t == nil {
		return sql.NullTime{Valid: false}
	}
	return sql.NullTime{Time: *t, Valid: true}
}

func sqlNullBool(b *bool) sql.NullBool {
	if b == nil {
		return sql.NullBool{Valid: false}
	}
	return sql.NullBool{Bool: *b, Valid: true}
}

func sqlNullInt(i *int) sql.NullInt64 {
	if i == nil {
		return sql.NullInt64{Valid: false}
	}
	return sql.NullInt64{Int64: int64(*i), Valid: true}
}
