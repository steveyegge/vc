package events

import (
	"context"
	"time"
)

// EventType represents the type of event that occurred during agent execution.
type EventType string

const (
	// EventTypeFileModified indicates a file was created, modified, or deleted
	EventTypeFileModified EventType = "file_modified"
	// EventTypeTestRun indicates a test suite was executed
	EventTypeTestRun EventType = "test_run"
	// EventTypeGitOperation indicates a git command was executed
	EventTypeGitOperation EventType = "git_operation"
	// EventTypeBuildOutput indicates build output was produced
	EventTypeBuildOutput EventType = "build_output"
	// EventTypeLintOutput indicates linter output was produced
	EventTypeLintOutput EventType = "lint_output"
	// EventTypeProgress indicates progress update from the agent
	EventTypeProgress EventType = "progress"
	// EventTypeError indicates an error occurred
	EventTypeError EventType = "error"
	// EventTypeWatchdog indicates a watchdog alert was triggered
	EventTypeWatchdog EventType = "watchdog_alert"

	// Executor-level events
	// EventTypeIssueClaimed indicates an executor claimed an issue for processing
	EventTypeIssueClaimed EventType = "issue_claimed"
	// EventTypeAssessmentStarted indicates AI assessment phase started
	EventTypeAssessmentStarted EventType = "assessment_started"
	// EventTypeAssessmentCompleted indicates AI assessment phase completed
	EventTypeAssessmentCompleted EventType = "assessment_completed"
	// EventTypeAgentSpawned indicates a coding agent was spawned
	EventTypeAgentSpawned EventType = "agent_spawned"
	// EventTypeAgentCompleted indicates a coding agent completed execution
	EventTypeAgentCompleted EventType = "agent_completed"
	// EventTypeResultsProcessingStarted indicates results processing phase started
	EventTypeResultsProcessingStarted EventType = "results_processing_started"
	// EventTypeResultsProcessingCompleted indicates results processing phase completed
	EventTypeResultsProcessingCompleted EventType = "results_processing_completed"
	// EventTypeAnalysisStarted indicates AI analysis phase started
	EventTypeAnalysisStarted EventType = "analysis_started"
	// EventTypeAnalysisCompleted indicates AI analysis phase completed
	EventTypeAnalysisCompleted EventType = "analysis_completed"
	// EventTypeQualityGatesStarted indicates quality gates evaluation started
	EventTypeQualityGatesStarted EventType = "quality_gates_started"
	// EventTypeQualityGatesProgress indicates progress during quality gates evaluation
	EventTypeQualityGatesProgress EventType = "quality_gates_progress"
	// EventTypeQualityGatesCompleted indicates quality gates evaluation completed
	EventTypeQualityGatesCompleted EventType = "quality_gates_completed"
	// EventTypeQualityGatesSkipped indicates quality gates evaluation was skipped
	EventTypeQualityGatesSkipped EventType = "quality_gates_skipped"
)

// EventSeverity represents the severity level of an event.
type EventSeverity string

const (
	// SeverityInfo indicates informational events
	SeverityInfo EventSeverity = "info"
	// SeverityWarning indicates potentially problematic events
	SeverityWarning EventSeverity = "warning"
	// SeverityError indicates error events
	SeverityError EventSeverity = "error"
	// SeverityCritical indicates critical events requiring immediate attention
	SeverityCritical EventSeverity = "critical"
)

// AgentEvent represents an event that occurred during agent execution.
// Events are extracted from agent output and stored for analysis and review.
type AgentEvent struct {
	// ID is the unique identifier for this event
	ID string `json:"id"`
	// Type is the type of event
	Type EventType `json:"type"`
	// Timestamp is when the event occurred
	Timestamp time.Time `json:"timestamp"`
	// IssueID is the issue being worked on when this event occurred
	IssueID string `json:"issue_id"`
	// ExecutorID is the executor instance that was running
	ExecutorID string `json:"executor_id"`
	// AgentID is the specific agent that produced this event
	AgentID string `json:"agent_id"`
	// Severity is the severity level of this event
	Severity EventSeverity `json:"severity"`
	// Message is a human-readable description of the event
	Message string `json:"message"`
	// Data contains structured, type-specific data (must be JSON-serializable)
	Data map[string]interface{} `json:"data"`
	// SourceLine is the line number in the agent output where this event was extracted
	SourceLine int `json:"source_line"`
}

// FileModifiedData contains structured data for file modification events.
type FileModifiedData struct {
	// FilePath is the path to the file that was modified
	FilePath string `json:"file_path"`
	// Operation is the type of modification: "created", "modified", "deleted"
	Operation string `json:"operation"`
}

// TestRunData contains structured data for test execution events.
type TestRunData struct {
	// TestName is the name of the test that was run
	TestName string `json:"test_name"`
	// Passed indicates whether the test passed
	Passed bool `json:"passed"`
	// Duration is how long the test took to run
	Duration time.Duration `json:"duration"`
	// Output is the output from the test execution
	Output string `json:"output"`
}

// GitOperationData contains structured data for git operation events.
type GitOperationData struct {
	// Command is the git command that was executed
	Command string `json:"command"`
	// Args are the arguments passed to the git command
	Args []string `json:"args"`
	// Success indicates whether the operation succeeded
	Success bool `json:"success"`
}

// EventStore defines the interface for storing and retrieving agent events.
type EventStore interface {
	// StoreEvent stores a new event in the event store
	StoreEvent(ctx context.Context, event *AgentEvent) error

	// GetEvents retrieves events matching the given filter
	GetEvents(ctx context.Context, filter EventFilter) ([]*AgentEvent, error)

	// GetEventsByIssue retrieves all events for a specific issue
	GetEventsByIssue(ctx context.Context, issueID string) ([]*AgentEvent, error)

	// GetRecentEvents retrieves the most recent events up to the specified limit
	GetRecentEvents(ctx context.Context, limit int) ([]*AgentEvent, error)
}

// EventFilter defines criteria for filtering events.
type EventFilter struct {
	// IssueID filters events by issue ID
	IssueID string
	// Type filters events by event type
	Type EventType
	// Severity filters events by severity level
	Severity EventSeverity
	// AfterTime filters events that occurred after this time
	AfterTime time.Time
	// BeforeTime filters events that occurred before this time
	BeforeTime time.Time
	// Limit limits the number of events returned
	Limit int
}

// Helper methods for type-safe data access
