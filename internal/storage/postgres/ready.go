package postgres

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/steveyegge/vc/internal/types"
)

// GetReadyWork returns issues with no open blockers
func (s *PostgresStorage) GetReadyWork(ctx context.Context, filter types.WorkFilter) ([]*types.Issue, error) {
	whereClauses := []string{}
	args := []interface{}{}
	paramIndex := 1

	// Default to open status if not specified
	if filter.Status == "" {
		filter.Status = types.StatusOpen
	}

	whereClauses = append(whereClauses, fmt.Sprintf("i.status = $%d", paramIndex))
	args = append(args, filter.Status)
	paramIndex++

	if filter.Priority != nil {
		whereClauses = append(whereClauses, fmt.Sprintf("i.priority = $%d", paramIndex))
		args = append(args, *filter.Priority)
		paramIndex++
	}

	if filter.Assignee != nil {
		whereClauses = append(whereClauses, fmt.Sprintf("i.assignee = $%d", paramIndex))
		args = append(args, *filter.Assignee)
		paramIndex++
	}

	// Build WHERE clause properly
	whereSQL := strings.Join(whereClauses, " AND ")

	// Build LIMIT clause using parameter
	limitSQL := ""
	if filter.Limit > 0 {
		limitSQL = fmt.Sprintf(" LIMIT $%d", paramIndex)
		args = append(args, filter.Limit)
	}

	// Single query template
	query := fmt.Sprintf(`
		SELECT i.id, i.title, i.description, i.design, i.acceptance_criteria, i.notes,
		       i.status, i.priority, i.issue_type, i.assignee, i.estimated_minutes,
		       i.created_at, i.updated_at, i.closed_at
		FROM issues i
		WHERE %s
		  AND NOT EXISTS (
		    SELECT 1 FROM dependencies d
		    JOIN issues blocked ON d.depends_on_id = blocked.id
		    WHERE d.issue_id = i.id
		      AND d.type = 'blocks'
		      AND blocked.status IN ('open', 'in_progress', 'blocked')
		  )
		ORDER BY i.priority ASC, i.created_at DESC
		%s
	`, whereSQL, limitSQL)

	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to get ready work: %w", err)
	}
	defer rows.Close()

	return scanIssues(rows)
}

// GetBlockedIssues returns issues that are blocked by dependencies
func (s *PostgresStorage) GetBlockedIssues(ctx context.Context) ([]*types.BlockedIssue, error) {
	// Use PostgreSQL's array_agg to get all blocker IDs in a single query (no N+1)
	rows, err := s.pool.Query(ctx, `
		SELECT
		    i.id, i.title, i.description, i.design, i.acceptance_criteria, i.notes,
		    i.status, i.priority, i.issue_type, i.assignee, i.estimated_minutes,
		    i.created_at, i.updated_at, i.closed_at,
		    COUNT(d.depends_on_id) as blocked_by_count,
		    array_agg(d.depends_on_id) as blocker_ids
		FROM issues i
		JOIN dependencies d ON i.id = d.issue_id
		JOIN issues blocker ON d.depends_on_id = blocker.id
		WHERE i.status IN ('open', 'in_progress', 'blocked')
		  AND d.type = 'blocks'
		  AND blocker.status IN ('open', 'in_progress', 'blocked')
		GROUP BY i.id
		ORDER BY i.priority ASC
	`)
	if err != nil {
		return nil, fmt.Errorf("failed to get blocked issues: %w", err)
	}
	defer rows.Close()

	var blocked []*types.BlockedIssue
	for rows.Next() {
		var issue types.BlockedIssue
		var closedAt *time.Time
		var estimatedMinutes *int
		var assignee *string
		var blockerIDs []string

		err := rows.Scan(
			&issue.ID, &issue.Title, &issue.Description, &issue.Design,
			&issue.AcceptanceCriteria, &issue.Notes, &issue.Status,
			&issue.Priority, &issue.IssueType, &assignee, &estimatedMinutes,
			&issue.CreatedAt, &issue.UpdatedAt, &closedAt, &issue.BlockedByCount,
			&blockerIDs,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan blocked issue: %w", err)
		}

		if closedAt != nil {
			issue.ClosedAt = closedAt
		}
		if estimatedMinutes != nil {
			issue.EstimatedMinutes = estimatedMinutes
		}
		if assignee != nil {
			issue.Assignee = *assignee
		}

		issue.BlockedBy = blockerIDs

		blocked = append(blocked, &issue)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating blocked issues: %w", err)
	}

	return blocked, nil
}

// GetStatistics returns aggregate statistics
func (s *PostgresStorage) GetStatistics(ctx context.Context) (*types.Statistics, error) {
	var stats types.Statistics

	// Get counts
	err := s.pool.QueryRow(ctx, `
		SELECT
			COUNT(*) as total,
			COUNT(*) FILTER (WHERE status = 'open') as open,
			COUNT(*) FILTER (WHERE status = 'in_progress') as in_progress,
			COUNT(*) FILTER (WHERE status = 'closed') as closed
		FROM issues
	`).Scan(&stats.TotalIssues, &stats.OpenIssues, &stats.InProgressIssues, &stats.ClosedIssues)
	if err != nil {
		return nil, fmt.Errorf("failed to get issue counts: %w", err)
	}

	// Get blocked count
	err = s.pool.QueryRow(ctx, `
		SELECT COUNT(DISTINCT i.id)
		FROM issues i
		JOIN dependencies d ON i.id = d.issue_id
		JOIN issues blocker ON d.depends_on_id = blocker.id
		WHERE i.status IN ('open', 'in_progress', 'blocked')
		  AND d.type = 'blocks'
		  AND blocker.status IN ('open', 'in_progress', 'blocked')
	`).Scan(&stats.BlockedIssues)
	if err != nil && err != pgx.ErrNoRows {
		return nil, fmt.Errorf("failed to get blocked count: %w", err)
	}

	// Get ready count
	err = s.pool.QueryRow(ctx, `
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
	// PostgreSQL uses EXTRACT(EPOCH FROM interval) to get seconds, then convert to hours
	var avgLeadTime *float64
	err = s.pool.QueryRow(ctx, `
		SELECT AVG(
			EXTRACT(EPOCH FROM (closed_at - created_at)) / 3600.0
		)
		FROM issues
		WHERE closed_at IS NOT NULL
	`).Scan(&avgLeadTime)
	if err != nil && err != pgx.ErrNoRows {
		return nil, fmt.Errorf("failed to get lead time: %w", err)
	}
	if avgLeadTime != nil {
		stats.AverageLeadTime = *avgLeadTime
	}

	return &stats, nil
}
