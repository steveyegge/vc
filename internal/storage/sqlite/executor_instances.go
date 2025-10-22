package sqlite

import (
	"context"
	"fmt"
	"time"

	"github.com/steveyegge/vc/internal/types"
)

// RegisterInstance registers a new executor instance
func (s *SQLiteStorage) RegisterInstance(ctx context.Context, instance *types.ExecutorInstance) error {
	// Validate the instance before inserting
	if err := instance.Validate(); err != nil {
		return fmt.Errorf("invalid executor instance: %w", err)
	}

	query := `
		INSERT INTO executor_instances (
			instance_id, hostname, pid, status, started_at, last_heartbeat, version, metadata
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(instance_id) DO UPDATE SET
			hostname = excluded.hostname,
			pid = excluded.pid,
			status = excluded.status,
			last_heartbeat = excluded.last_heartbeat,
			version = excluded.version,
			metadata = excluded.metadata
	`

	_, err := s.db.ExecContext(ctx, query,
		instance.InstanceID,
		instance.Hostname,
		instance.PID,
		instance.Status,
		instance.StartedAt,
		instance.LastHeartbeat,
		instance.Version,
		instance.Metadata,
	)

	if err != nil {
		return fmt.Errorf("failed to register executor instance: %w", err)
	}

	return nil
}

// UpdateHeartbeat updates the last_heartbeat timestamp for an executor instance
func (s *SQLiteStorage) UpdateHeartbeat(ctx context.Context, instanceID string) error {
	query := `
		UPDATE executor_instances
		SET last_heartbeat = ?
		WHERE instance_id = ?
	`

	result, err := s.db.ExecContext(ctx, query, time.Now(), instanceID)
	if err != nil {
		return fmt.Errorf("failed to update heartbeat: %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rows == 0 {
		return fmt.Errorf("executor instance not found: %s", instanceID)
	}

	return nil
}

// GetActiveInstances returns all executor instances with status='running'
func (s *SQLiteStorage) GetActiveInstances(ctx context.Context) ([]*types.ExecutorInstance, error) {
	query := `
		SELECT instance_id, hostname, pid, status, started_at, last_heartbeat, version, metadata
		FROM executor_instances
		WHERE status = 'running'
		ORDER BY last_heartbeat DESC
	`

	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to query active instances: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var instances []*types.ExecutorInstance
	for rows.Next() {
		instance := &types.ExecutorInstance{}
		err := rows.Scan(
			&instance.InstanceID,
			&instance.Hostname,
			&instance.PID,
			&instance.Status,
			&instance.StartedAt,
			&instance.LastHeartbeat,
			&instance.Version,
			&instance.Metadata,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan executor instance: %w", err)
		}
		instances = append(instances, instance)
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating executor instances: %w", err)
	}

	return instances, nil
}

// CleanupStaleInstances marks instances as 'stopped' if their last_heartbeat
// is older than staleThreshold seconds, and releases all issues claimed by those instances.
// Also releases claims from already-stopped instances (orphaned claims).
// Returns the number of instances cleaned up.
func (s *SQLiteStorage) CleanupStaleInstances(ctx context.Context, staleThreshold int) (int, error) {
	// Calculate the cutoff time in Go, then compare
	cutoffTime := time.Now().Add(-time.Duration(staleThreshold) * time.Second)

	// Start a transaction to ensure atomic cleanup of instances and their claims
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	// First, find all stale instances (running but heartbeat is old)
	staleQuery := `
		SELECT instance_id
		FROM executor_instances
		WHERE status = 'running'
		  AND last_heartbeat < ?
	`

	rows, err := tx.QueryContext(ctx, staleQuery, cutoffTime)
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
		FROM issue_execution_state
		WHERE executor_instance_id IN (
			SELECT instance_id FROM executor_instances WHERE status = 'stopped'
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
		// Find all issues claimed by this stale instance
		claimedIssuesQuery := `
			SELECT issue_id
			FROM issue_execution_state
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
			// Delete execution state
			_, err = tx.ExecContext(ctx, `
				DELETE FROM issue_execution_state
				WHERE issue_id = ?
			`, issueID)
			if err != nil {
				return 0, fmt.Errorf("failed to delete execution state for issue %s: %w", issueID, err)
			}

			// Reset issue status to 'open'
			_, err = tx.ExecContext(ctx, `
				UPDATE issues
				SET status = ?, updated_at = ?
				WHERE id = ?
			`, types.StatusOpen, time.Now(), issueID)
			if err != nil {
				return 0, fmt.Errorf("failed to reset issue status for %s: %w", issueID, err)
			}

			// Add comment explaining why the issue was released
			var comment string
			// Check if this is a stale instance or an orphaned claim
			isStale := false
			for _, staleID := range staleInstanceIDs {
				if staleID == instanceID {
					isStale = true
					break
				}
			}
			if isStale {
				comment = fmt.Sprintf("Issue automatically released - executor instance %s became stale (no heartbeat for %d seconds)", instanceID, staleThreshold)
			} else {
				comment = fmt.Sprintf("Issue automatically released - executor instance %s was already stopped but claim remained (orphaned)", instanceID)
			}
			_, err = tx.ExecContext(ctx, `
				INSERT INTO events (issue_id, event_type, actor, comment)
				VALUES (?, ?, ?, ?)
			`, issueID, types.EventStatusChanged, "system", comment)
			if err != nil {
				return 0, fmt.Errorf("failed to add release comment for issue %s: %w", issueID, err)
			}
		}
	}

	// Mark all stale instances as 'stopped' (orphaned instances are already stopped)
	updateQuery := `
		UPDATE executor_instances
		SET status = 'stopped'
		WHERE status = 'running'
		  AND last_heartbeat < ?
	`

	_, err = tx.ExecContext(ctx, updateQuery, cutoffTime)
	if err != nil {
		return 0, fmt.Errorf("failed to mark stale instances as stopped: %w", err)
	}

	// Commit the transaction
	if err = tx.Commit(); err != nil {
		return 0, fmt.Errorf("failed to commit transaction: %w", err)
	}

	// Return total number of instances cleaned (stale + orphaned)
	return len(allInstanceIDs), nil
}

// DeleteOldStoppedInstances removes old stopped executor instances from the database
// to prevent accumulation of historical instances that are no longer needed.
// It deletes instances with status='stopped' that are older than olderThanSeconds,
// but always keeps at least maxToKeep most recent stopped instances.
//
// Special cases:
//   - maxToKeep=0: Deletes ALL instances older than threshold (keeps nothing)
//   - maxToKeep > total stopped instances: Deletes nothing (all instances are kept)
//
// Returns the number of instances deleted.
func (s *SQLiteStorage) DeleteOldStoppedInstances(ctx context.Context, olderThanSeconds int, maxToKeep int) (int, error) {
	// Validate inputs
	if olderThanSeconds <= 0 {
		return 0, fmt.Errorf("olderThanSeconds must be positive, got: %d", olderThanSeconds)
	}
	if maxToKeep < 0 {
		return 0, fmt.Errorf("maxToKeep must be non-negative, got: %d", maxToKeep)
	}

	// Calculate the cutoff time
	cutoffTime := time.Now().Add(-time.Duration(olderThanSeconds) * time.Second)

	// Start a transaction
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	// Build the delete query:
	// 1. Find stopped instances older than cutoff
	// 2. Exclude the N most recent stopped instances (to always keep some history)
	// We use a subquery to get the instance_ids we want to keep
	deleteQuery := `
		DELETE FROM executor_instances
		WHERE status = 'stopped'
		  AND started_at < ?
		  AND instance_id NOT IN (
		      SELECT instance_id
		      FROM executor_instances
		      WHERE status = 'stopped'
		      ORDER BY started_at DESC
		      LIMIT ?
		  )
	`

	result, err := tx.ExecContext(ctx, deleteQuery, cutoffTime, maxToKeep)
	if err != nil {
		return 0, fmt.Errorf("failed to delete old stopped instances: %w", err)
	}

	rowsDeleted, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("failed to get rows deleted: %w", err)
	}

	// Commit the transaction
	if err = tx.Commit(); err != nil {
		return 0, fmt.Errorf("failed to commit transaction: %w", err)
	}

	return int(rowsDeleted), nil
}
