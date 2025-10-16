package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/steveyegge/vc/internal/events"
)

// StoreAgentEvent stores a new agent event in the database
func (p *PostgresStorage) StoreAgentEvent(ctx context.Context, event *events.AgentEvent) error {
	// Marshal the Data field to JSON
	dataJSON, err := json.Marshal(event.Data)
	if err != nil {
		return fmt.Errorf("failed to marshal event data: %w", err)
	}

	query := `
		INSERT INTO agent_events (
			id, type, timestamp, issue_id, executor_id, agent_id,
			severity, message, data, source_line
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
	`

	_, err = p.pool.Exec(ctx, query,
		event.ID,
		event.Type,
		event.Timestamp,
		event.IssueID,
		event.ExecutorID,
		event.AgentID,
		event.Severity,
		event.Message,
		dataJSON,
		event.SourceLine,
	)
	if err != nil {
		return fmt.Errorf("failed to store agent event: %w", err)
	}

	return nil
}

// GetAgentEvents retrieves events matching the given filter
func (p *PostgresStorage) GetAgentEvents(ctx context.Context, filter events.EventFilter) ([]*events.AgentEvent, error) {
	query := `
		SELECT id, type, timestamp, issue_id, executor_id, agent_id,
		       severity, message, data, source_line
		FROM agent_events
		WHERE 1=1
	`
	args := []interface{}{}
	argNum := 1

	// Apply filters
	if filter.IssueID != "" {
		query += fmt.Sprintf(" AND issue_id = $%d", argNum)
		args = append(args, filter.IssueID)
		argNum++
	}
	if filter.Type != "" {
		query += fmt.Sprintf(" AND type = $%d", argNum)
		args = append(args, filter.Type)
		argNum++
	}
	if filter.Severity != "" {
		query += fmt.Sprintf(" AND severity = $%d", argNum)
		args = append(args, filter.Severity)
		argNum++
	}
	if !filter.AfterTime.IsZero() {
		query += fmt.Sprintf(" AND timestamp > $%d", argNum)
		args = append(args, filter.AfterTime)
		argNum++
	}
	if !filter.BeforeTime.IsZero() {
		query += fmt.Sprintf(" AND timestamp < $%d", argNum)
		args = append(args, filter.BeforeTime)
		argNum++
	}

	// Order by timestamp descending (most recent first)
	query += " ORDER BY timestamp DESC"

	// Apply limit
	if filter.Limit > 0 {
		query += fmt.Sprintf(" LIMIT $%d", argNum)
		args = append(args, filter.Limit)
	}

	rows, err := p.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query agent events: %w", err)
	}
	defer rows.Close()

	return p.scanEvents(rows)
}

// GetAgentEventsByIssue retrieves all events for a specific issue
func (p *PostgresStorage) GetAgentEventsByIssue(ctx context.Context, issueID string) ([]*events.AgentEvent, error) {
	query := `
		SELECT id, type, timestamp, issue_id, executor_id, agent_id,
		       severity, message, data, source_line
		FROM agent_events
		WHERE issue_id = $1
		ORDER BY timestamp ASC
	`

	rows, err := p.pool.Query(ctx, query, issueID)
	if err != nil {
		return nil, fmt.Errorf("failed to query agent events by issue: %w", err)
	}
	defer rows.Close()

	return p.scanEvents(rows)
}

// GetRecentAgentEvents retrieves the most recent events up to the specified limit
func (p *PostgresStorage) GetRecentAgentEvents(ctx context.Context, limit int) ([]*events.AgentEvent, error) {
	query := `
		SELECT id, type, timestamp, issue_id, executor_id, agent_id,
		       severity, message, data, source_line
		FROM agent_events
		ORDER BY timestamp DESC
		LIMIT $1
	`

	rows, err := p.pool.Query(ctx, query, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to query recent agent events: %w", err)
	}
	defer rows.Close()

	return p.scanEvents(rows)
}

// scanEvents is a helper function to scan rows into AgentEvent structs
func (p *PostgresStorage) scanEvents(rows pgx.Rows) ([]*events.AgentEvent, error) {
	var result []*events.AgentEvent

	for rows.Next() {
		var event events.AgentEvent
		var dataJSON []byte
		var timestamp time.Time

		err := rows.Scan(
			&event.ID,
			&event.Type,
			&timestamp,
			&event.IssueID,
			&event.ExecutorID,
			&event.AgentID,
			&event.Severity,
			&event.Message,
			&dataJSON,
			&event.SourceLine,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan agent event: %w", err)
		}

		event.Timestamp = timestamp

		// Unmarshal the JSON data field
		event.Data = make(map[string]interface{})
		if len(dataJSON) > 0 && string(dataJSON) != "{}" {
			if err := json.Unmarshal(dataJSON, &event.Data); err != nil {
				return nil, fmt.Errorf("failed to unmarshal event data: %w", err)
			}
		}

		result = append(result, &event)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating agent event rows: %w", err)
	}

	return result, nil
}
