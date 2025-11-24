package types

import "time"

// InterruptMetadata represents saved context when a task is paused
type InterruptMetadata struct {
	IssueID            string    `json:"issue_id"`
	InterruptedAt      time.Time `json:"interrupted_at"`
	InterruptedBy      string    `json:"interrupted_by"` // "user" | "budget" | "system"
	Reason             string    `json:"reason,omitempty"`
	ExecutorInstanceID string    `json:"executor_instance_id,omitempty"`
	AgentID            string    `json:"agent_id,omitempty"`
	ExecutionState     string    `json:"execution_state,omitempty"` // "assessing" | "executing" | "analyzing"
	LastTool           string    `json:"last_tool,omitempty"`
	WorkingNotes       string    `json:"working_notes,omitempty"`
	TodosJSON          string    `json:"todos_json,omitempty"`       // JSON array
	ProgressSummary    string    `json:"progress_summary,omitempty"`
	ContextSnapshot    string    `json:"context_snapshot,omitempty"` // Full JSON context
	ResumedAt          *time.Time `json:"resumed_at,omitempty"`
	ResumeCount        int       `json:"resume_count"`
}

// AgentContext represents the full context that can be saved/restored
type AgentContext struct {
	// Work state
	Todos           []string          `json:"todos,omitempty"`
	CompletedTodos  []string          `json:"completed_todos,omitempty"`
	WorkingNotes    string            `json:"working_notes,omitempty"`
	ProgressSummary string            `json:"progress_summary,omitempty"`

	// Execution state
	LastTool        string            `json:"last_tool,omitempty"`
	LastToolResult  string            `json:"last_tool_result,omitempty"`

	// Metadata
	InterruptedAt   time.Time         `json:"interrupted_at"`
	SessionDuration time.Duration     `json:"session_duration,omitempty"`
	ToolsUsed       int               `json:"tools_used,omitempty"`

	// Additional context
	Observations    []string          `json:"observations,omitempty"`
	Decisions       []string          `json:"decisions,omitempty"`
	CustomData      map[string]interface{} `json:"custom_data,omitempty"`
}
