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
	// EventTypeContextUsage indicates context usage measurement from agent output
	EventTypeContextUsage EventType = "context_usage"

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

	// Deduplication events (vc-151)
	// EventTypeDeduplicationBatchStarted indicates batch deduplication processing started
	EventTypeDeduplicationBatchStarted EventType = "deduplication_batch_started"
	// EventTypeDeduplicationBatchCompleted indicates batch deduplication processing completed
	EventTypeDeduplicationBatchCompleted EventType = "deduplication_batch_completed"
	// EventTypeDeduplicationDecision indicates an individual duplicate decision was made
	EventTypeDeduplicationDecision EventType = "deduplication_decision"

	// Event retention and cleanup events (vc-196)
	// EventTypeEventCleanupCompleted indicates event cleanup cycle completed
	EventTypeEventCleanupCompleted EventType = "event_cleanup_completed"

	// Health monitoring events (vc-205)
	// EventTypeHealthCheckCompleted indicates a health monitor completed execution
	EventTypeHealthCheckCompleted EventType = "health_check_completed"
	// EventTypeHealthCheckFailed indicates a health monitor failed to execute
	EventTypeHealthCheckFailed EventType = "health_check_failed"

	// Agent progress events (vc-129)
	// EventTypeAgentToolUse indicates an agent invoked a tool (Read, Edit, Write, Bash, etc.)
	EventTypeAgentToolUse EventType = "agent_tool_use"
	// EventTypeAgentHeartbeat indicates periodic progress heartbeat from agent
	EventTypeAgentHeartbeat EventType = "agent_heartbeat"
	// EventTypeAgentStateChange indicates agent state change (thinking, planning, executing)
	EventTypeAgentStateChange EventType = "agent_state_change"

	// Preflight quality gates events (vc-196, vc-201)
	// EventTypePreFlightCheckStarted indicates preflight baseline check started
	EventTypePreFlightCheckStarted EventType = "pre_flight_check_started"
	// EventTypePreFlightCheckCompleted indicates preflight baseline check completed
	EventTypePreFlightCheckCompleted EventType = "pre_flight_check_completed"
	// EventTypeBaselineCacheHit indicates baseline was retrieved from cache
	EventTypeBaselineCacheHit EventType = "baseline_cache_hit"
	// EventTypeBaselineCacheMiss indicates baseline was not in cache (will run gates)
	EventTypeBaselineCacheMiss EventType = "baseline_cache_miss"
	// EventTypeExecutorDegradedMode indicates executor entered degraded mode (baseline failed)
	EventTypeExecutorDegradedMode EventType = "executor_degraded_mode"
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

// DeduplicationBatchStartedData contains structured data for deduplication batch start events (vc-151).
type DeduplicationBatchStartedData struct {
	// CandidateCount is the number of candidate issues being deduplicated
	CandidateCount int `json:"candidate_count"`
	// ParentIssueID is the issue that discovered these candidates (if applicable)
	ParentIssueID string `json:"parent_issue_id,omitempty"`
}

// DeduplicationBatchCompletedData contains structured data for deduplication batch completion events (vc-151).
type DeduplicationBatchCompletedData struct {
	// TotalCandidates is the total number of candidates processed
	TotalCandidates int `json:"total_candidates"`
	// UniqueCount is the number of unique issues (to be filed)
	UniqueCount int `json:"unique_count"`
	// DuplicateCount is the number of duplicates against existing issues
	DuplicateCount int `json:"duplicate_count"`
	// WithinBatchDuplicateCount is the number of duplicates within the batch
	WithinBatchDuplicateCount int `json:"within_batch_duplicate_count"`
	// ComparisonsMade is the total number of pairwise comparisons
	ComparisonsMade int `json:"comparisons_made"`
	// AICallsMade is the number of AI API calls made
	AICallsMade int `json:"ai_calls_made"`
	// ProcessingTimeMs is the time taken for deduplication in milliseconds
	ProcessingTimeMs int64 `json:"processing_time_ms"`
	// Success indicates whether deduplication succeeded
	Success bool `json:"success"`
	// Error contains the error message if deduplication failed
	Error string `json:"error,omitempty"`
}

// DeduplicationDecisionData contains structured data for individual duplicate decisions (vc-151).
type DeduplicationDecisionData struct {
	// CandidateTitle is the title of the candidate issue
	CandidateTitle string `json:"candidate_title"`
	// IsDuplicate indicates whether the candidate was marked as a duplicate
	IsDuplicate bool `json:"is_duplicate"`
	// DuplicateOf is the ID of the existing issue (if duplicate)
	DuplicateOf string `json:"duplicate_of,omitempty"`
	// Confidence is the AI's confidence score (0.0 to 1.0)
	Confidence float64 `json:"confidence"`
	// Reasoning is the AI's explanation for the decision
	Reasoning string `json:"reasoning,omitempty"`
	// WithinBatchDuplicate indicates if this is a within-batch duplicate
	WithinBatchDuplicate bool `json:"within_batch_duplicate,omitempty"`
	// WithinBatchOriginal is the title of the original issue (for within-batch duplicates)
	WithinBatchOriginal string `json:"within_batch_original,omitempty"`
}

// EventCleanupCompletedData contains structured data for event cleanup completion events (vc-196).
type EventCleanupCompletedData struct {
	// EventsDeleted is the total number of events deleted
	EventsDeleted int `json:"events_deleted"`
	// TimeBasedDeleted is the number of events deleted by time-based retention
	TimeBasedDeleted int `json:"time_based_deleted"`
	// PerIssueDeleted is the number of events deleted by per-issue limit
	PerIssueDeleted int `json:"per_issue_deleted"`
	// GlobalLimitDeleted is the number of events deleted by global safety limit
	GlobalLimitDeleted int `json:"global_limit_deleted"`
	// ProcessingTimeMs is the time taken for cleanup in milliseconds
	ProcessingTimeMs int64 `json:"processing_time_ms"`
	// VacuumRan indicates whether VACUUM was executed
	VacuumRan bool `json:"vacuum_ran"`
	// EventsRemaining is the total number of events remaining after cleanup
	EventsRemaining int `json:"events_remaining"`
	// Success indicates whether cleanup succeeded
	Success bool `json:"success"`
	// Error contains the error message if cleanup failed
	Error string `json:"error,omitempty"`
}

// AgentToolUseData contains structured data for agent tool usage events (vc-129).
type AgentToolUseData struct {
	// ToolName is the name of the tool invoked (Read, Edit, Write, Bash, etc.)
	ToolName string `json:"tool_name"`
	// ToolDescription is a brief description of what the tool is doing
	ToolDescription string `json:"tool_description,omitempty"`
	// TargetFile is the file being operated on (if applicable)
	TargetFile string `json:"target_file,omitempty"`
	// Command is the command being executed (for Bash tool)
	Command string `json:"command,omitempty"`
}

// AgentHeartbeatData contains structured data for agent heartbeat events (vc-129).
type AgentHeartbeatData struct {
	// CurrentAction is a description of what the agent is currently doing
	CurrentAction string `json:"current_action"`
	// ElapsedSeconds is the time elapsed since agent started (in seconds)
	ElapsedSeconds int64 `json:"elapsed_seconds"`
}

// AgentStateChangeData contains structured data for agent state change events (vc-129).
type AgentStateChangeData struct {
	// FromState is the previous state (if applicable)
	FromState string `json:"from_state,omitempty"`
	// ToState is the new state (thinking, planning, executing, waiting, etc.)
	ToState string `json:"to_state"`
	// Description is additional context about the state change
	Description string `json:"description,omitempty"`
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
