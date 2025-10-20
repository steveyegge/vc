package sqlite

import (
	"context"
	"fmt"
	"time"
)

// EventCounts holds event count statistics for monitoring
type EventCounts struct {
	TotalEvents      int
	EventsByIssue    map[string]int
	EventsBySeverity map[string]int
	EventsByType     map[string]int
}

// CleanupEventsByAge deletes events older than the retention period
// Regular events are deleted after retentionDays, critical events after criticalRetentionDays
// Deletions are batched for performance (batchSize events per transaction)
func (s *SQLiteStorage) CleanupEventsByAge(ctx context.Context, retentionDays, criticalRetentionDays, batchSize int) (int, error) {
	if retentionDays < 0 || criticalRetentionDays < 0 {
		return 0, fmt.Errorf("retention days cannot be negative")
	}
	if batchSize < 1 {
		return 0, fmt.Errorf("batch size must be at least 1")
	}

	totalDeleted := 0

	// Step 1: Delete old regular events (severity = info or warning)
	regularCutoff := time.Now().AddDate(0, 0, -retentionDays)
	deleted, err := s.deleteOldEventsBatch(ctx, regularCutoff, []string{"info", "warning"}, batchSize)
	if err != nil {
		return totalDeleted, fmt.Errorf("failed to delete old regular events: %w", err)
	}
	totalDeleted += deleted

	// Step 2: Delete old critical events (severity = error or critical)
	// Only if critical retention is different from regular retention
	if criticalRetentionDays != retentionDays {
		criticalCutoff := time.Now().AddDate(0, 0, -criticalRetentionDays)
		deleted, err = s.deleteOldEventsBatch(ctx, criticalCutoff, []string{"error", "critical"}, batchSize)
		if err != nil {
			return totalDeleted, fmt.Errorf("failed to delete old critical events: %w", err)
		}
		totalDeleted += deleted
	}

	return totalDeleted, nil
}

// deleteOldEventsBatch deletes events older than cutoff with specified severities in batches
func (s *SQLiteStorage) deleteOldEventsBatch(ctx context.Context, cutoff time.Time, severities []string, batchSize int) (int, error) {
	totalDeleted := 0

	for {
		// Check context cancellation
		select {
		case <-ctx.Done():
			return totalDeleted, ctx.Err()
		default:
		}

		// Build severity IN clause
		severityPlaceholders := ""
		args := []interface{}{cutoff}
		for i, sev := range severities {
			if i > 0 {
				severityPlaceholders += ", "
			}
			severityPlaceholders += "?"
			args = append(args, sev)
		}
		args = append(args, batchSize)

		// Delete a batch
		query := fmt.Sprintf(`
			DELETE FROM agent_events
			WHERE id IN (
				SELECT id FROM agent_events
				WHERE timestamp < ?
				AND severity IN (%s)
				ORDER BY timestamp ASC
				LIMIT ?
			)
		`, severityPlaceholders)

		result, err := s.db.ExecContext(ctx, query, args...)
		if err != nil {
			return totalDeleted, fmt.Errorf("failed to execute delete: %w", err)
		}

		rowsAffected, err := result.RowsAffected()
		if err != nil {
			return totalDeleted, fmt.Errorf("failed to get rows affected: %w", err)
		}

		totalDeleted += int(rowsAffected)

		// If we deleted fewer than batchSize, we're done
		if rowsAffected < int64(batchSize) {
			break
		}
	}

	return totalDeleted, nil
}

// CleanupEventsByIssueLimit enforces per-issue event limits
// For each issue with more than perIssueLimit events, oldest non-critical events are deleted
// Critical events (severity = error or critical) are exempt from this limit
func (s *SQLiteStorage) CleanupEventsByIssueLimit(ctx context.Context, perIssueLimit, batchSize int) (int, error) {
	if perIssueLimit < 0 {
		return 0, fmt.Errorf("per-issue limit cannot be negative")
	}
	if perIssueLimit == 0 {
		// 0 means unlimited
		return 0, nil
	}
	if batchSize < 1 {
		return 0, fmt.Errorf("batch size must be at least 1")
	}

	totalDeleted := 0

	// Find issues exceeding the limit
	query := `
		SELECT issue_id, COUNT(*) as event_count
		FROM agent_events
		GROUP BY issue_id
		HAVING event_count > ?
	`

	rows, err := s.db.QueryContext(ctx, query, perIssueLimit)
	if err != nil {
		return 0, fmt.Errorf("failed to query issue event counts: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var issues []struct {
		issueID    string
		eventCount int
	}

	for rows.Next() {
		var issueID string
		var count int
		if err := rows.Scan(&issueID, &count); err != nil {
			return totalDeleted, fmt.Errorf("failed to scan issue count: %w", err)
		}
		issues = append(issues, struct {
			issueID    string
			eventCount int
		}{issueID, count})
	}

	if err := rows.Err(); err != nil {
		return totalDeleted, fmt.Errorf("error iterating issue counts: %w", err)
	}

	// For each issue exceeding the limit, delete oldest non-critical events
	for _, issue := range issues {
		// Check context cancellation
		select {
		case <-ctx.Done():
			return totalDeleted, ctx.Err()
		default:
		}

		eventsToDelete := issue.eventCount - perIssueLimit
		if eventsToDelete <= 0 {
			continue
		}

		deleted, err := s.deleteOldestEventsForIssue(ctx, issue.issueID, eventsToDelete, batchSize)
		if err != nil {
			return totalDeleted, fmt.Errorf("failed to delete events for issue %s: %w", issue.issueID, err)
		}
		totalDeleted += deleted
	}

	return totalDeleted, nil
}

// deleteOldestEventsForIssue deletes the oldest non-critical events for a specific issue
func (s *SQLiteStorage) deleteOldestEventsForIssue(ctx context.Context, issueID string, count, batchSize int) (int, error) {
	totalDeleted := 0
	remaining := count

	for remaining > 0 {
		// Check context cancellation
		select {
		case <-ctx.Done():
			return totalDeleted, ctx.Err()
		default:
		}

		// Delete up to batchSize events
		limitThisBatch := batchSize
		if remaining < batchSize {
			limitThisBatch = remaining
		}

		query := `
			DELETE FROM agent_events
			WHERE id IN (
				SELECT id FROM agent_events
				WHERE issue_id = ?
				AND severity NOT IN ('error', 'critical')
				ORDER BY timestamp ASC
				LIMIT ?
			)
		`

		result, err := s.db.ExecContext(ctx, query, issueID, limitThisBatch)
		if err != nil {
			return totalDeleted, fmt.Errorf("failed to execute delete: %w", err)
		}

		rowsAffected, err := result.RowsAffected()
		if err != nil {
			return totalDeleted, fmt.Errorf("failed to get rows affected: %w", err)
		}

		totalDeleted += int(rowsAffected)
		remaining -= int(rowsAffected)

		// If we deleted fewer than requested, no more non-critical events to delete
		if rowsAffected < int64(limitThisBatch) {
			break
		}
	}

	return totalDeleted, nil
}

// CleanupEventsByGlobalLimit enforces a global event count limit
// When the total event count exceeds the limit, oldest non-critical events are deleted
// This is typically triggered at 95% of the configured global limit
func (s *SQLiteStorage) CleanupEventsByGlobalLimit(ctx context.Context, globalLimit, batchSize int) (int, error) {
	if globalLimit < 1 {
		return 0, fmt.Errorf("global limit must be at least 1")
	}
	if batchSize < 1 {
		return 0, fmt.Errorf("batch size must be at least 1")
	}

	// Get current event count
	var currentCount int
	err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM agent_events").Scan(&currentCount)
	if err != nil {
		return 0, fmt.Errorf("failed to get event count: %w", err)
	}

	// If under the limit, nothing to do
	if currentCount <= globalLimit {
		return 0, nil
	}

	eventsToDelete := currentCount - globalLimit
	totalDeleted := 0

	// Delete oldest non-critical events in batches
	for eventsToDelete > 0 {
		// Check context cancellation
		select {
		case <-ctx.Done():
			return totalDeleted, ctx.Err()
		default:
		}

		// Delete up to batchSize events
		limitThisBatch := batchSize
		if eventsToDelete < batchSize {
			limitThisBatch = eventsToDelete
		}

		query := `
			DELETE FROM agent_events
			WHERE id IN (
				SELECT id FROM agent_events
				WHERE severity NOT IN ('error', 'critical')
				ORDER BY timestamp ASC
				LIMIT ?
			)
		`

		result, err := s.db.ExecContext(ctx, query, limitThisBatch)
		if err != nil {
			return totalDeleted, fmt.Errorf("failed to execute delete: %w", err)
		}

		rowsAffected, err := result.RowsAffected()
		if err != nil {
			return totalDeleted, fmt.Errorf("failed to get rows affected: %w", err)
		}

		totalDeleted += int(rowsAffected)
		eventsToDelete -= int(rowsAffected)

		// If we deleted fewer than requested, no more non-critical events to delete
		if rowsAffected < int64(limitThisBatch) {
			break
		}
	}

	return totalDeleted, nil
}

// GetEventCounts returns detailed event count statistics for monitoring
func (s *SQLiteStorage) GetEventCounts(ctx context.Context) (*EventCounts, error) {
	counts := &EventCounts{
		EventsByIssue:    make(map[string]int),
		EventsBySeverity: make(map[string]int),
		EventsByType:     make(map[string]int),
	}

	// Total events
	err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM agent_events").Scan(&counts.TotalEvents)
	if err != nil {
		return nil, fmt.Errorf("failed to get total event count: %w", err)
	}

	// Events by issue
	rows, err := s.db.QueryContext(ctx, `
		SELECT issue_id, COUNT(*)
		FROM agent_events
		GROUP BY issue_id
	`)
	if err != nil {
		return nil, fmt.Errorf("failed to query events by issue: %w", err)
	}
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		var issueID string
		var count int
		if err := rows.Scan(&issueID, &count); err != nil {
			return nil, fmt.Errorf("failed to scan issue count: %w", err)
		}
		counts.EventsByIssue[issueID] = count
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating issue counts: %w", err)
	}

	// Events by severity
	rows, err = s.db.QueryContext(ctx, `
		SELECT severity, COUNT(*)
		FROM agent_events
		GROUP BY severity
	`)
	if err != nil {
		return nil, fmt.Errorf("failed to query events by severity: %w", err)
	}
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		var severity string
		var count int
		if err := rows.Scan(&severity, &count); err != nil {
			return nil, fmt.Errorf("failed to scan severity count: %w", err)
		}
		counts.EventsBySeverity[severity] = count
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating severity counts: %w", err)
	}

	// Events by type
	rows, err = s.db.QueryContext(ctx, `
		SELECT type, COUNT(*)
		FROM agent_events
		GROUP BY type
	`)
	if err != nil {
		return nil, fmt.Errorf("failed to query events by type: %w", err)
	}
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		var eventType string
		var count int
		if err := rows.Scan(&eventType, &count); err != nil {
			return nil, fmt.Errorf("failed to scan type count: %w", err)
		}
		counts.EventsByType[eventType] = count
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating type counts: %w", err)
	}

	return counts, nil
}

// VacuumDatabase runs the VACUUM command to reclaim disk space
// This can be slow and locks the database, so it should be run during maintenance windows
func (s *SQLiteStorage) VacuumDatabase(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, "VACUUM")
	if err != nil {
		return fmt.Errorf("failed to vacuum database: %w", err)
	}
	return nil
}
