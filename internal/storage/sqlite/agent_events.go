package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/steveyegge/vc/internal/events"
)

// StoreAgentEvent stores a new agent event in the database
func (s *SQLiteStorage) StoreAgentEvent(ctx context.Context, event *events.AgentEvent) error {
	// Marshal the Data field to JSON
	dataJSON, err := json.Marshal(event.Data)
	if err != nil {
		return fmt.Errorf("failed to marshal event data: %w", err)
	}

	query := `
		INSERT INTO agent_events (
			id, type, timestamp, issue_id, executor_id, agent_id,
			severity, message, data, source_line
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`

	_, err = s.db.ExecContext(ctx, query,
		event.ID,
		event.Type,
		event.Timestamp,
		event.IssueID,
		event.ExecutorID,
		event.AgentID,
		event.Severity,
		event.Message,
		string(dataJSON),
		event.SourceLine,
	)
	if err != nil {
		return fmt.Errorf("failed to store agent event (type=%s, issue=%s): %w", event.Type, event.IssueID, err)
	}

	return nil
}

// GetAgentEvents retrieves events matching the given filter
func (s *SQLiteStorage) GetAgentEvents(ctx context.Context, filter events.EventFilter) ([]*events.AgentEvent, error) {
	query := `
		SELECT id, type, timestamp, issue_id, executor_id, agent_id,
		       severity, message, data, source_line
		FROM agent_events
		WHERE 1=1
	`
	args := []interface{}{}

	// Apply filters
	if filter.IssueID != "" {
		query += " AND issue_id = ?"
		args = append(args, filter.IssueID)
	}
	if filter.Type != "" {
		query += " AND type = ?"
		args = append(args, filter.Type)
	}
	if filter.Severity != "" {
		query += " AND severity = ?"
		args = append(args, filter.Severity)
	}
	if !filter.AfterTime.IsZero() {
		query += " AND timestamp > ?"
		args = append(args, filter.AfterTime)
	}
	if !filter.BeforeTime.IsZero() {
		query += " AND timestamp < ?"
		args = append(args, filter.BeforeTime)
	}

	// Order by timestamp descending (most recent first)
	query += " ORDER BY timestamp DESC"

	// Apply limit
	if filter.Limit > 0 {
		query += " LIMIT ?"
		args = append(args, filter.Limit)
	}

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query agent events: %w", err)
	}
	defer rows.Close()

	return s.scanEvents(rows)
}

// GetAgentEventsByIssue retrieves all events for a specific issue
func (s *SQLiteStorage) GetAgentEventsByIssue(ctx context.Context, issueID string) ([]*events.AgentEvent, error) {
	query := `
		SELECT id, type, timestamp, issue_id, executor_id, agent_id,
		       severity, message, data, source_line
		FROM agent_events
		WHERE issue_id = ?
		ORDER BY timestamp ASC
	`

	rows, err := s.db.QueryContext(ctx, query, issueID)
	if err != nil {
		return nil, fmt.Errorf("failed to query agent events by issue: %w", err)
	}
	defer rows.Close()

	return s.scanEvents(rows)
}

// GetRecentAgentEvents retrieves the most recent events up to the specified limit
func (s *SQLiteStorage) GetRecentAgentEvents(ctx context.Context, limit int) ([]*events.AgentEvent, error) {
	query := `
		SELECT id, type, timestamp, issue_id, executor_id, agent_id,
		       severity, message, data, source_line
		FROM agent_events
		ORDER BY timestamp DESC
		LIMIT ?
	`

	rows, err := s.db.QueryContext(ctx, query, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to query recent agent events: %w", err)
	}
	defer rows.Close()

	return s.scanEvents(rows)
}

// scanEvents is a helper function to scan rows into AgentEvent structs
func (s *SQLiteStorage) scanEvents(rows *sql.Rows) ([]*events.AgentEvent, error) {
	var result []*events.AgentEvent

	for rows.Next() {
		var event events.AgentEvent
		var dataJSON string
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
		if dataJSON != "" && dataJSON != "{}" {
			if err := json.Unmarshal([]byte(dataJSON), &event.Data); err != nil {
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
