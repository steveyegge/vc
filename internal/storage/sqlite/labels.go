package sqlite

import (
	"context"
	"fmt"

	"github.com/steveyegge/vc/internal/types"
)

// AddLabel adds a label to an issue
func (s *SQLiteStorage) AddLabel(ctx context.Context, issueID, label, actor string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	result, err := tx.ExecContext(ctx, `
		INSERT OR IGNORE INTO labels (issue_id, label)
		VALUES (?, ?)
	`, issueID, label)
	if err != nil {
		return fmt.Errorf("failed to add label: %w", err)
	}

	// Only record event if a row was actually inserted
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to check rows affected: %w", err)
	}

	if rowsAffected > 0 {
		_, err = tx.ExecContext(ctx, `
			INSERT INTO events (issue_id, event_type, actor, comment)
			VALUES (?, ?, ?, ?)
		`, issueID, types.EventLabelAdded, actor, fmt.Sprintf("Added label: %s", label))
		if err != nil {
			return fmt.Errorf("failed to record event: %w", err)
		}
	}

	return tx.Commit()
}

// RemoveLabel removes a label from an issue
func (s *SQLiteStorage) RemoveLabel(ctx context.Context, issueID, label, actor string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	result, err := tx.ExecContext(ctx, `
		DELETE FROM labels WHERE issue_id = ? AND label = ?
	`, issueID, label)
	if err != nil {
		return fmt.Errorf("failed to remove label: %w", err)
	}

	// Only record event if a row was actually deleted
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to check rows affected: %w", err)
	}

	if rowsAffected > 0 {
		_, err = tx.ExecContext(ctx, `
			INSERT INTO events (issue_id, event_type, actor, comment)
			VALUES (?, ?, ?, ?)
		`, issueID, types.EventLabelRemoved, actor, fmt.Sprintf("Removed label: %s", label))
		if err != nil {
			return fmt.Errorf("failed to record event: %w", err)
		}
	}

	return tx.Commit()
}

// GetLabels returns all labels for an issue
func (s *SQLiteStorage) GetLabels(ctx context.Context, issueID string) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT label FROM labels WHERE issue_id = ? ORDER BY label
	`, issueID)
	if err != nil {
		return nil, fmt.Errorf("failed to get labels: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var labels []string
	for rows.Next() {
		var label string
		if err := rows.Scan(&label); err != nil {
			return nil, err
		}
		labels = append(labels, label)
	}

	return labels, nil
}

// GetIssuesByLabel returns issues with a specific label
func (s *SQLiteStorage) GetIssuesByLabel(ctx context.Context, label string) ([]*types.Issue, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT i.id, i.title, i.description, i.design, i.acceptance_criteria, i.notes,
		       i.status, i.priority, i.issue_type, i.assignee, i.estimated_minutes,
		       i.created_at, i.updated_at, i.closed_at
		FROM issues i
		JOIN labels l ON i.id = l.issue_id
		WHERE l.label = ?
		ORDER BY i.priority ASC, i.created_at DESC
	`, label)
	if err != nil {
		return nil, fmt.Errorf("failed to get issues by label: %w", err)
	}
	defer func() { _ = rows.Close() }()

	return scanIssues(rows)
}
