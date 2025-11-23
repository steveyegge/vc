package beads

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/steveyegge/vc/internal/types"
)

// SaveInterruptMetadata saves interrupt context for a paused task
func (s *VCStorage) SaveInterruptMetadata(ctx context.Context, metadata *types.InterruptMetadata) error {
	query := `
		INSERT INTO vc_interrupt_metadata (
			issue_id, interrupted_at, interrupted_by, reason,
			executor_instance_id, agent_id, execution_state,
			last_tool, working_notes, todos_json,
			progress_summary, context_snapshot, resume_count
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(issue_id) DO UPDATE SET
			interrupted_at = excluded.interrupted_at,
			interrupted_by = excluded.interrupted_by,
			reason = excluded.reason,
			executor_instance_id = excluded.executor_instance_id,
			agent_id = excluded.agent_id,
			execution_state = excluded.execution_state,
			last_tool = excluded.last_tool,
			working_notes = excluded.working_notes,
			todos_json = excluded.todos_json,
			progress_summary = excluded.progress_summary,
			context_snapshot = excluded.context_snapshot,
			resume_count = excluded.resume_count
	`

	_, err := s.db.ExecContext(ctx, query,
		metadata.IssueID,
		metadata.InterruptedAt,
		metadata.InterruptedBy,
		metadata.Reason,
		metadata.ExecutorInstanceID,
		metadata.AgentID,
		metadata.ExecutionState,
		metadata.LastTool,
		metadata.WorkingNotes,
		metadata.TodosJSON,
		metadata.ProgressSummary,
		metadata.ContextSnapshot,
		metadata.ResumeCount,
	)

	if err != nil {
		return fmt.Errorf("failed to save interrupt metadata: %w", err)
	}

	return nil
}

// GetInterruptMetadata retrieves interrupt context for an issue
func (s *VCStorage) GetInterruptMetadata(ctx context.Context, issueID string) (*types.InterruptMetadata, error) {
	query := `
		SELECT
			issue_id, interrupted_at, interrupted_by, reason,
			executor_instance_id, agent_id, execution_state,
			last_tool, working_notes, todos_json,
			progress_summary, context_snapshot, resumed_at, resume_count
		FROM vc_interrupt_metadata
		WHERE issue_id = ?
	`

	var metadata types.InterruptMetadata
	var resumedAt sql.NullTime

	err := s.db.QueryRowContext(ctx, query, issueID).Scan(
		&metadata.IssueID,
		&metadata.InterruptedAt,
		&metadata.InterruptedBy,
		&metadata.Reason,
		&metadata.ExecutorInstanceID,
		&metadata.AgentID,
		&metadata.ExecutionState,
		&metadata.LastTool,
		&metadata.WorkingNotes,
		&metadata.TodosJSON,
		&metadata.ProgressSummary,
		&metadata.ContextSnapshot,
		&resumedAt,
		&metadata.ResumeCount,
	)

	if err == sql.ErrNoRows {
		return nil, nil // No interrupt metadata for this issue
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get interrupt metadata: %w", err)
	}

	if resumedAt.Valid {
		metadata.ResumedAt = &resumedAt.Time
	}

	return &metadata, nil
}

// MarkInterruptResumed marks an interrupt as resumed
func (s *VCStorage) MarkInterruptResumed(ctx context.Context, issueID string) error {
	query := `
		UPDATE vc_interrupt_metadata
		SET resumed_at = ?, resume_count = resume_count + 1
		WHERE issue_id = ?
	`

	_, err := s.db.ExecContext(ctx, query, time.Now(), issueID)
	if err != nil {
		return fmt.Errorf("failed to mark interrupt as resumed: %w", err)
	}

	return nil
}

// DeleteInterruptMetadata removes interrupt metadata (after successful resume or manual cleanup)
func (s *VCStorage) DeleteInterruptMetadata(ctx context.Context, issueID string) error {
	query := `DELETE FROM vc_interrupt_metadata WHERE issue_id = ?`

	_, err := s.db.ExecContext(ctx, query, issueID)
	if err != nil {
		return fmt.Errorf("failed to delete interrupt metadata: %w", err)
	}

	return nil
}

// ListInterruptedIssues returns all issues with interrupt metadata
func (s *VCStorage) ListInterruptedIssues(ctx context.Context) ([]*types.InterruptMetadata, error) {
	query := `
		SELECT
			issue_id, interrupted_at, interrupted_by, reason,
			executor_instance_id, agent_id, execution_state,
			last_tool, working_notes, todos_json,
			progress_summary, context_snapshot, resumed_at, resume_count
		FROM vc_interrupt_metadata
		ORDER BY interrupted_at DESC
	`

	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to list interrupted issues: %w", err)
	}
	defer rows.Close()

	var results []*types.InterruptMetadata
	for rows.Next() {
		var metadata types.InterruptMetadata
		var resumedAt sql.NullTime

		err := rows.Scan(
			&metadata.IssueID,
			&metadata.InterruptedAt,
			&metadata.InterruptedBy,
			&metadata.Reason,
			&metadata.ExecutorInstanceID,
			&metadata.AgentID,
			&metadata.ExecutionState,
			&metadata.LastTool,
			&metadata.WorkingNotes,
			&metadata.TodosJSON,
			&metadata.ProgressSummary,
			&metadata.ContextSnapshot,
			&resumedAt,
			&metadata.ResumeCount,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan interrupt metadata: %w", err)
		}

		if resumedAt.Valid {
			metadata.ResumedAt = &resumedAt.Time
		}

		results = append(results, &metadata)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating interrupt metadata: %w", err)
	}

	return results, nil
}

// BuildAgentResumeContext creates a resume brief for the agent based on interrupt metadata
func BuildAgentResumeContext(metadata *types.InterruptMetadata) string {
	if metadata == nil {
		return ""
	}

	// Parse context snapshot if available
	var context types.AgentContext
	if metadata.ContextSnapshot != "" {
		if err := json.Unmarshal([]byte(metadata.ContextSnapshot), &context); err == nil {
			// Successfully parsed full context
			return buildFullResumeContext(metadata, &context)
		}
	}

	// Fall back to basic context from metadata fields
	return buildBasicResumeContext(metadata)
}

// buildFullResumeContext creates a detailed resume brief from full context
func buildFullResumeContext(metadata *types.InterruptMetadata, context *types.AgentContext) string {
	brief := fmt.Sprintf(`**Task Interrupted and Resumed**

You were interrupted at %s while working on this issue.

**Reason**: %s
**Interrupted by**: %s
**Execution phase**: %s
**Time spent before interrupt**: %s

`, metadata.InterruptedAt.Format("2006-01-02 15:04:05"),
		metadata.Reason,
		metadata.InterruptedBy,
		metadata.ExecutionState,
		context.SessionDuration)

	if len(context.Todos) > 0 {
		brief += "**Your TODO list when interrupted**:\n"
		for i, todo := range context.Todos {
			brief += fmt.Sprintf("%d. %s\n", i+1, todo)
		}
		brief += "\n"
	}

	if len(context.CompletedTodos) > 0 {
		brief += "**Completed tasks**:\n"
		for _, todo := range context.CompletedTodos {
			brief += fmt.Sprintf("- ✓ %s\n", todo)
		}
		brief += "\n"
	}

	if context.ProgressSummary != "" {
		brief += fmt.Sprintf("**Progress summary**: %s\n\n", context.ProgressSummary)
	}

	if context.LastTool != "" {
		brief += fmt.Sprintf("**Last tool used**: %s\n", context.LastTool)
		if context.LastToolResult != "" {
			brief += fmt.Sprintf("**Result**: %s\n", context.LastToolResult)
		}
		brief += "\n"
	}

	if context.WorkingNotes != "" {
		brief += fmt.Sprintf("**Your working notes**:\n%s\n\n", context.WorkingNotes)
	}

	if len(context.Observations) > 0 {
		brief += "**Key observations**:\n"
		for _, obs := range context.Observations {
			brief += fmt.Sprintf("- %s\n", obs)
		}
		brief += "\n"
	}

	brief += "**Please continue from where you left off.**\n"

	return brief
}

// buildBasicResumeContext creates a simple resume brief from basic metadata
func buildBasicResumeContext(metadata *types.InterruptMetadata) string {
	brief := fmt.Sprintf(`**Task Interrupted and Resumed**

You were interrupted at %s while working on this issue.

**Reason**: %s
**Interrupted by**: %s
**Execution phase**: %s

`, metadata.InterruptedAt.Format("2006-01-02 15:04:05"),
		metadata.Reason,
		metadata.InterruptedBy,
		metadata.ExecutionState)

	if metadata.LastTool != "" {
		brief += fmt.Sprintf("**Last tool used**: %s\n\n", metadata.LastTool)
	}

	if metadata.ProgressSummary != "" {
		brief += fmt.Sprintf("**Progress so far**: %s\n\n", metadata.ProgressSummary)
	}

	if metadata.WorkingNotes != "" {
		brief += fmt.Sprintf("**Your working notes**:\n%s\n\n", metadata.WorkingNotes)
	}

	brief += "**Please continue from where you left off.**\n"

	return brief
}
