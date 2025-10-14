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
	defer rows.Close()

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
// is older than staleThreshold seconds. Returns the number of instances cleaned up.
func (s *SQLiteStorage) CleanupStaleInstances(ctx context.Context, staleThreshold int) (int, error) {
	// Calculate the cutoff time in Go, then compare
	cutoffTime := time.Now().Add(-time.Duration(staleThreshold) * time.Second)

	query := `
		UPDATE executor_instances
		SET status = 'stopped'
		WHERE status = 'running'
		  AND last_heartbeat < ?
	`

	result, err := s.db.ExecContext(ctx, query, cutoffTime)
	if err != nil {
		return 0, fmt.Errorf("failed to cleanup stale instances: %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("failed to get rows affected: %w", err)
	}

	return int(rows), nil
}
