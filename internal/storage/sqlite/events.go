package sqlite

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/steveyegge/vc/internal/types"
)

// AddComment adds a comment to an issue
func (s *SQLiteStorage) AddComment(ctx context.Context, issueID, actor, comment string) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO events (issue_id, event_type, actor, comment)
		VALUES (?, ?, ?, ?)
	`, issueID, types.EventCommented, actor, comment)
	if err != nil {
		return fmt.Errorf("failed to add comment: %w", err)
	}

	// Update issue updated_at timestamp
	_, err = s.db.ExecContext(ctx, `
		UPDATE issues SET updated_at = CURRENT_TIMESTAMP WHERE id = ?
	`, issueID)
	if err != nil {
		return fmt.Errorf("failed to update timestamp: %w", err)
	}

	return nil
}

// GetEvents returns the event history for an issue
func (s *SQLiteStorage) GetEvents(ctx context.Context, issueID string, limit int) ([]*types.Event, error) {
	limitSQL := ""
	if limit > 0 {
		limitSQL = fmt.Sprintf(" LIMIT %d", limit)
	}

	query := fmt.Sprintf(`
		SELECT id, issue_id, event_type, actor, old_value, new_value, comment, created_at
		FROM events
		WHERE issue_id = ?
		ORDER BY created_at DESC
		%s
	`, limitSQL)

	rows, err := s.db.QueryContext(ctx, query, issueID)
	if err != nil {
		return nil, fmt.Errorf("failed to get events: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var events []*types.Event
	for rows.Next() {
		var event types.Event
		var oldValue, newValue, comment sql.NullString

		err := rows.Scan(
			&event.ID, &event.IssueID, &event.EventType, &event.Actor,
			&oldValue, &newValue, &comment, &event.CreatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan event: %w", err)
		}

		if oldValue.Valid {
			event.OldValue = &oldValue.String
		}
		if newValue.Valid {
			event.NewValue = &newValue.String
		}
		if comment.Valid {
			event.Comment = &comment.String
		}

		events = append(events, &event)
	}

	return events, nil
}

// GetStatistics returns aggregate statistics
func (s *SQLiteStorage) GetStatistics(ctx context.Context) (*types.Statistics, error) {
	var stats types.Statistics

	// Get counts
	// Note: COALESCE is needed because SUM() returns NULL when there are no rows
	err := s.db.QueryRowContext(ctx, `
		SELECT
			COUNT(*) as total,
			COALESCE(SUM(CASE WHEN status = 'open' THEN 1 ELSE 0 END), 0) as open,
			COALESCE(SUM(CASE WHEN status = 'in_progress' THEN 1 ELSE 0 END), 0) as in_progress,
			COALESCE(SUM(CASE WHEN status = 'closed' THEN 1 ELSE 0 END), 0) as closed
		FROM issues
	`).Scan(&stats.TotalIssues, &stats.OpenIssues, &stats.InProgressIssues, &stats.ClosedIssues)
	if err != nil {
		return nil, fmt.Errorf("failed to get issue counts: %w", err)
	}

	// Get blocked count
	err = s.db.QueryRowContext(ctx, `
		SELECT COUNT(DISTINCT i.id)
		FROM issues i
		JOIN dependencies d ON i.id = d.issue_id
		JOIN issues blocker ON d.depends_on_id = blocker.id
		WHERE i.status IN ('open', 'in_progress', 'blocked')
		  AND d.type = 'blocks'
		  AND blocker.status IN ('open', 'in_progress', 'blocked')
	`).Scan(&stats.BlockedIssues)
	if err != nil {
		return nil, fmt.Errorf("failed to get blocked count: %w", err)
	}

	// Get ready count
	err = s.db.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM issues i
		WHERE i.status = 'open'
		  AND NOT EXISTS (
		    SELECT 1 FROM dependencies d
		    JOIN issues blocked ON d.depends_on_id = blocked.id
		    WHERE d.issue_id = i.id
		      AND d.type = 'blocks'
		      AND blocked.status IN ('open', 'in_progress', 'blocked')
		  )
	`).Scan(&stats.ReadyIssues)
	if err != nil {
		return nil, fmt.Errorf("failed to get ready count: %w", err)
	}

	// Get average lead time (hours from created to closed)
	var avgLeadTime sql.NullFloat64
	err = s.db.QueryRowContext(ctx, `
		SELECT AVG(
			(julianday(closed_at) - julianday(created_at)) * 24
		)
		FROM issues
		WHERE closed_at IS NOT NULL
	`).Scan(&avgLeadTime)
	if err != nil && err != sql.ErrNoRows {
		return nil, fmt.Errorf("failed to get lead time: %w", err)
	}
	if avgLeadTime.Valid {
		stats.AverageLeadTime = avgLeadTime.Float64
	}

	return &stats, nil
}
