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
	// EventTypeIssueDecomposed indicates an issue was decomposed into child issues (vc-rzqe)
	EventTypeIssueDecomposed EventType = "issue_decomposed"
	// EventTypeQualityGatesStarted indicates quality gates evaluation started
	EventTypeQualityGatesStarted EventType = "quality_gates_started"
	// EventTypeQualityGatesProgress indicates progress during quality gates evaluation
	EventTypeQualityGatesProgress EventType = "quality_gates_progress"
	// EventTypeQualityGatesCompleted indicates quality gates evaluation completed
	EventTypeQualityGatesCompleted EventType = "quality_gates_completed"
	// EventTypeQualityGatesSkipped indicates quality gates evaluation was skipped
	EventTypeQualityGatesSkipped EventType = "quality_gates_skipped"
	// EventTypeQualityGatesDeferred indicates quality gates were deferred to QA worker (vc-251)
	EventTypeQualityGatesDeferred EventType = "quality_gates_deferred"
	// EventTypeQualityGatesRollback indicates changes were rolled back after quality gate failure (vc-16fe)
	EventTypeQualityGatesRollback EventType = "quality_gates_rollback"

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

	// Instance cleanup events (vc-32)
	// EventTypeInstanceCleanupCompleted indicates executor instance cleanup cycle completed
	EventTypeInstanceCleanupCompleted EventType = "instance_cleanup_completed"

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
	// EventTypeExecutorSelfHealingMode indicates executor entered self-healing mode (baseline failed)
	EventTypeExecutorSelfHealingMode EventType = "executor_self_healing_mode"

	// Self-healing events (vc-210)
	// EventTypeBaselineTestFixStarted indicates self-healing started for a baseline test failure
	EventTypeBaselineTestFixStarted EventType = "baseline_test_fix_started"
	// EventTypeBaselineTestFixCompleted indicates self-healing completed for a baseline test failure
	EventTypeBaselineTestFixCompleted EventType = "baseline_test_fix_completed"
	// EventTypeTestFailureDiagnosis indicates AI diagnosed a test failure
	EventTypeTestFailureDiagnosis EventType = "test_failure_diagnosis"

	// Label-driven state machine events (vc-218)
	// EventTypeLabelStateTransition indicates a label-driven state transition occurred
	EventTypeLabelStateTransition EventType = "label_state_transition"

	// Circuit breaker events (vc-182)
	// EventTypeCircuitBreakerStateChange indicates circuit breaker state transition
	EventTypeCircuitBreakerStateChange EventType = "circuit_breaker_state_change"

	// Quality gate worker events (vc-252, vc-253)
	// EventTypeQualityGatePass indicates all quality gates passed for a mission
	EventTypeQualityGatePass EventType = "quality_gate_pass"
	// EventTypeQualityGateFail indicates one or more quality gates failed for a mission
	EventTypeQualityGateFail EventType = "quality_gate_fail"

	// Mission sandbox lifecycle events (vc-265)
	// EventTypeSandboxCreationStarted indicates sandbox creation began
	EventTypeSandboxCreationStarted EventType = "sandbox_creation_started"
	// EventTypeGitWorktreeCreated indicates git worktree was successfully created
	EventTypeGitWorktreeCreated EventType = "git_worktree_created"
	// EventTypeGitBranchCreated indicates git branch was successfully created
	EventTypeGitBranchCreated EventType = "git_branch_created"
	// EventTypeSandboxCreationCompleted indicates sandbox creation completed
	EventTypeSandboxCreationCompleted EventType = "sandbox_creation_completed"
	// EventTypeSandboxCleanupStarted indicates sandbox cleanup began
	EventTypeSandboxCleanupStarted EventType = "sandbox_cleanup_started"
	// EventTypeGitBranchDeleted indicates git branch was deleted
	EventTypeGitBranchDeleted EventType = "git_branch_deleted"
	// EventTypeGitWorktreeRemoved indicates git worktree was removed
	EventTypeGitWorktreeRemoved EventType = "git_worktree_removed"
	// EventTypeSandboxCleanupCompleted indicates sandbox cleanup completed
	EventTypeSandboxCleanupCompleted EventType = "sandbox_cleanup_completed"

	// Mission phase transition events (vc-266)
	// EventTypeMissionCreated indicates a new mission was created
	EventTypeMissionCreated EventType = "mission_created"
	// EventTypeMissionMetadataUpdated indicates mission metadata was updated
	EventTypeMissionMetadataUpdated EventType = "mission_metadata_updated"

	// Bootstrap mode events (vc-b027)
	// EventTypeBootstrapModeActivated indicates executor entered bootstrap mode (quota crisis)
	EventTypeBootstrapModeActivated EventType = "bootstrap_mode_activated"

	// Epic lifecycle events (vc-268)
	// EventTypeEpicCompleted indicates an epic was completed (all children done)
	EventTypeEpicCompleted EventType = "epic_completed"
	// EventTypeEpicCleanupStarted indicates epic sandbox cleanup began
	EventTypeEpicCleanupStarted EventType = "epic_cleanup_started"
	// EventTypeEpicCleanupCompleted indicates epic sandbox cleanup completed
	EventTypeEpicCleanupCompleted EventType = "epic_cleanup_completed"

	// Code review sweep events (vc-1)
	// EventTypeCodeReviewDecision indicates AI decided whether code review sweep is needed
	EventTypeCodeReviewDecision EventType = "code_review_decision"
	// EventTypeCodeReviewCreated indicates a code review sweep issue was created
	EventTypeCodeReviewCreated EventType = "code_review_created"

	// Cost budgeting events (vc-e3s7)
	// EventTypeAICost indicates AI API usage and associated cost
	EventTypeAICost EventType = "ai_cost"
	// EventTypeBudgetAlert indicates budget warning or exceeded alert
	EventTypeBudgetAlert EventType = "budget_alert"

	// Quota monitoring events (vc-7e21)
	// EventTypeQuotaAlert indicates predictive quota alert (YELLOW/ORANGE/RED)
	EventTypeQuotaAlert EventType = "quota_alert"
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

// InstanceCleanupCompletedData contains structured data for instance cleanup events (vc-32).
// This struct follows the same pattern as EventCleanupCompletedData for consistency.
type InstanceCleanupCompletedData struct {
	// InstancesDeleted is the total number of stopped instances deleted
	InstancesDeleted int `json:"instances_deleted"`
	// InstancesRemaining is the number of stopped instances remaining after cleanup
	InstancesRemaining int `json:"instances_remaining"`
	// ProcessingTimeMs is the time taken for cleanup in milliseconds
	ProcessingTimeMs int64 `json:"processing_time_ms"`
	// CleanupAgeSeconds is the age threshold used (instances older than this were deleted)
	CleanupAgeSeconds int `json:"cleanup_age_seconds"`
	// MaxToKeep is the minimum number of stopped instances to keep
	MaxToKeep int `json:"max_to_keep"`
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

// BaselineTestFixStartedData contains structured data for baseline test fix start events (vc-210).
type BaselineTestFixStartedData struct {
	// BaselineIssueID is the ID of the baseline issue being fixed (e.g., vc-baseline-test)
	BaselineIssueID string `json:"baseline_issue_id"`
	// GateType is the type of gate that failed (test, lint, build)
	GateType string `json:"gate_type"`
	// FailingTests is the list of failing test names
	FailingTests []string `json:"failing_tests,omitempty"`
}

// BaselineTestFixCompletedData contains structured data for baseline test fix completion events (vc-210).
type BaselineTestFixCompletedData struct {
	// BaselineIssueID is the ID of the baseline issue that was fixed
	BaselineIssueID string `json:"baseline_issue_id"`
	// GateType is the type of gate that was fixed
	GateType string `json:"gate_type"`
	// Success indicates whether the fix was successful
	Success bool `json:"success"`
	// FixType is the type of fix applied (flaky, real, environmental)
	FixType string `json:"fix_type,omitempty"`
	// TestsFixed is the number of tests that were fixed
	TestsFixed int `json:"tests_fixed"`
	// CommitHash is the git commit hash of the fix
	CommitHash string `json:"commit_hash,omitempty"`
	// ProcessingTimeMs is the time taken to fix in milliseconds
	ProcessingTimeMs int64 `json:"processing_time_ms"`
	// Error contains the error message if fix failed
	Error string `json:"error,omitempty"`
}

// TestFailureDiagnosisData contains structured data for test failure diagnosis events (vc-210).
type TestFailureDiagnosisData struct {
	// FailureType is the classified type of failure (flaky, real, environmental, unknown)
	// vc-228: Kept as string for JSON flexibility; use IsValidFailureType() for validation
	// Valid values: "flaky", "real", "environmental", "unknown"
	FailureType string `json:"failure_type"`
	// RootCause is the AI's diagnosis of why the test is failing
	RootCause string `json:"root_cause"`
	// ProposedFix is the AI's recommended fix
	ProposedFix string `json:"proposed_fix"`
	// Confidence is the AI's confidence in the diagnosis (0.0 to 1.0)
	Confidence float64 `json:"confidence"`
	// TestNames is the list of failing tests
	TestNames []string `json:"test_names"`
}

// LabelStateTransitionData contains structured data for label state transition events (vc-218).
type LabelStateTransitionData struct {
	// FromLabel is the previous state label (empty if initial state)
	FromLabel string `json:"from_label,omitempty"`
	// ToLabel is the new state label
	ToLabel string `json:"to_label"`
	// Trigger is what triggered the transition (e.g., "task_completed", "gates_passed", "human_approval")
	Trigger string `json:"trigger"`
	// Actor is who/what initiated the transition (executor ID, user, etc.)
	Actor string `json:"actor,omitempty"`
	// MissionID is the mission this transition applies to (if applicable)
	MissionID string `json:"mission_id,omitempty"`
}

// SandboxCreationStartedData contains structured data for sandbox creation start events (vc-265).
type SandboxCreationStartedData struct {
	// MissionID is the mission being worked on
	MissionID string `json:"mission_id"`
	// BaseBranch is the base branch for the mission branch
	BaseBranch string `json:"base_branch,omitempty"`
}

// GitWorktreeCreatedData contains structured data for git worktree creation events (vc-265).
type GitWorktreeCreatedData struct {
	// MissionID is the mission being worked on
	MissionID string `json:"mission_id"`
	// WorktreePath is the path to the created worktree
	WorktreePath string `json:"worktree_path"`
	// BaseBranch is the base branch for the worktree
	BaseBranch string `json:"base_branch,omitempty"`
}

// GitBranchCreatedData contains structured data for git branch creation events (vc-265).
type GitBranchCreatedData struct {
	// MissionID is the mission being worked on
	MissionID string `json:"mission_id"`
	// BranchName is the name of the created branch
	BranchName string `json:"branch_name"`
	// BaseBranch is the base branch for the new branch
	BaseBranch string `json:"base_branch,omitempty"`
	// WorktreePath is the path to the worktree
	WorktreePath string `json:"worktree_path"`
}

// SandboxCreationCompletedData contains structured data for sandbox creation completion events (vc-265).
type SandboxCreationCompletedData struct {
	// MissionID is the mission being worked on
	MissionID string `json:"mission_id"`
	// SandboxID is the unique ID for this sandbox
	SandboxID string `json:"sandbox_id"`
	// SandboxPath is the path to the sandbox directory
	SandboxPath string `json:"sandbox_path"`
	// BranchName is the name of the mission branch
	BranchName string `json:"branch_name"`
	// DurationMs is the time taken to create the sandbox in milliseconds
	DurationMs int64 `json:"duration_ms"`
	// Success indicates whether creation succeeded
	Success bool `json:"success"`
	// Error contains the error message if creation failed
	Error string `json:"error,omitempty"`
}

// SandboxCleanupStartedData contains structured data for sandbox cleanup start events (vc-265).
type SandboxCleanupStartedData struct {
	// MissionID is the mission being worked on
	MissionID string `json:"mission_id"`
	// SandboxID is the unique ID for this sandbox
	SandboxID string `json:"sandbox_id"`
	// SandboxPath is the path to the sandbox directory
	SandboxPath string `json:"sandbox_path"`
	// BranchName is the name of the mission branch
	BranchName string `json:"branch_name"`
}

// GitBranchDeletedData contains structured data for git branch deletion events (vc-265).
type GitBranchDeletedData struct {
	// MissionID is the mission being worked on
	MissionID string `json:"mission_id"`
	// BranchName is the name of the deleted branch
	BranchName string `json:"branch_name"`
	// Success indicates whether deletion succeeded
	Success bool `json:"success"`
	// Error contains the error message if deletion failed
	Error string `json:"error,omitempty"`
}

// GitWorktreeRemovedData contains structured data for git worktree removal events (vc-265).
type GitWorktreeRemovedData struct {
	// MissionID is the mission being worked on
	MissionID string `json:"mission_id"`
	// WorktreePath is the path to the removed worktree
	WorktreePath string `json:"worktree_path"`
	// Success indicates whether removal succeeded
	Success bool `json:"success"`
	// Error contains the error message if removal failed
	Error string `json:"error,omitempty"`
}

// SandboxCleanupCompletedData contains structured data for sandbox cleanup completion events (vc-265).
type SandboxCleanupCompletedData struct {
	// MissionID is the mission being worked on
	MissionID string `json:"mission_id"`
	// SandboxID is the unique ID for this sandbox
	SandboxID string `json:"sandbox_id"`
	// SandboxPath is the path to the sandbox directory
	SandboxPath string `json:"sandbox_path"`
	// BranchName is the name of the mission branch
	BranchName string `json:"branch_name"`
	// DurationMs is the time taken to cleanup the sandbox in milliseconds
	DurationMs int64 `json:"duration_ms"`
	// Success indicates whether cleanup succeeded
	Success bool `json:"success"`
	// Error contains the error message if cleanup failed
	Error string `json:"error,omitempty"`
}

// MissionCreatedData contains structured data for mission creation events (vc-266).
type MissionCreatedData struct {
	// MissionID is the ID of the created mission
	MissionID string `json:"mission_id"`
	// ParentEpicID is the ID of the parent epic (if any)
	ParentEpicID string `json:"parent_epic_id,omitempty"`
	// Goal is the high-level goal for the mission
	Goal string `json:"goal"`
	// PhaseCount is the number of phases planned
	PhaseCount int `json:"phase_count"`
	// ApprovalRequired indicates if human approval is needed
	ApprovalRequired bool `json:"approval_required"`
	// Actor is who created the mission (user, executor ID, etc.)
	Actor string `json:"actor"`
}

// MissionMetadataUpdatedData contains structured data for mission metadata update events (vc-266).
type MissionMetadataUpdatedData struct {
	// MissionID is the ID of the mission
	MissionID string `json:"mission_id"`
	// UpdatedFields is a list of field names that were changed
	UpdatedFields []string `json:"updated_fields"`
	// Changes is a map of field name to old/new values
	Changes map[string]FieldChange `json:"changes,omitempty"`
	// Actor is who updated the mission
	Actor string `json:"actor"`
}

// FieldChange represents a change to a field value
type FieldChange struct {
	// OldValue is the previous value (may be nil for new fields)
	OldValue interface{} `json:"old_value"`
	// NewValue is the new value
	NewValue interface{} `json:"new_value"`
}

// QualityGatesProgressData contains structured data for quality gates progress events (vc-267).
type QualityGatesProgressData struct {
	// CurrentGate is the gate currently being executed (test, lint, build)
	CurrentGate string `json:"current_gate"`
	// GatesCompleted is the number of gates completed so far
	GatesCompleted int `json:"gates_completed"`
	// TotalGates is the total number of gates to run
	TotalGates int `json:"total_gates"`
	// ElapsedSeconds is the time elapsed since gates started (in seconds)
	ElapsedSeconds int64 `json:"elapsed_seconds"`
	// Message is a human-readable progress message
	Message string `json:"message,omitempty"`
}

// EpicCompletedData contains structured data for epic completion events (vc-268).
type EpicCompletedData struct {
	// EpicID is the ID of the completed epic
	EpicID string `json:"epic_id"`
	// EpicTitle is the title of the epic
	EpicTitle string `json:"epic_title"`
	// ChildrenCompleted is the number of child issues that were completed
	ChildrenCompleted int `json:"children_completed"`
	// CompletionMethod indicates how completion was determined (ai_assessment, all_children_closed)
	CompletionMethod string `json:"completion_method"`
	// Confidence is the AI's confidence in the completion decision (0.0 to 1.0, or 1.0 for fallback)
	Confidence float64 `json:"confidence"`
	// IsMission indicates whether this epic is a mission
	IsMission bool `json:"is_mission"`
	// Actor is who/what closed the epic (ai-supervisor, executor, etc.)
	Actor string `json:"actor"`
}

// EpicCleanupStartedData contains structured data for epic cleanup start events (vc-268).
type EpicCleanupStartedData struct {
	// EpicID is the ID of the epic being cleaned up
	EpicID string `json:"epic_id"`
	// IsMission indicates whether this epic is a mission (with sandbox)
	IsMission bool `json:"is_mission"`
	// SandboxPath is the path to the sandbox being cleaned up (if mission)
	SandboxPath string `json:"sandbox_path,omitempty"`
}

// EpicCleanupCompletedData contains structured data for epic cleanup completion events (vc-268).
type EpicCleanupCompletedData struct {
	// EpicID is the ID of the epic that was cleaned up
	EpicID string `json:"epic_id"`
	// IsMission indicates whether this epic was a mission
	IsMission bool `json:"is_mission"`
	// SandboxPath is the path to the sandbox that was cleaned up (if mission)
	SandboxPath string `json:"sandbox_path,omitempty"`
	// Success indicates whether cleanup succeeded
	Success bool `json:"success"`
	// Error contains the error message if cleanup failed
	Error string `json:"error,omitempty"`
	// DurationMs is the time taken to cleanup in milliseconds
	DurationMs int64 `json:"duration_ms,omitempty"`
}

// QuotaAlertData contains structured data for quota alert events (vc-7e21).
type QuotaAlertData struct {
	// AlertLevel is the urgency level (YELLOW, ORANGE, RED)
	AlertLevel string `json:"alert_level"`
	// TokensPerMinute is the current burn rate in tokens/minute
	TokensPerMinute float64 `json:"tokens_per_minute"`
	// CostPerMinute is the current burn rate in $/minute
	CostPerMinute float64 `json:"cost_per_minute"`
	// EstimatedMinutesToLimit is the predicted time until quota exhaustion
	EstimatedMinutesToLimit float64 `json:"estimated_minutes_to_limit"`
	// Confidence is the AI's confidence in the prediction (0.0-1.0)
	Confidence float64 `json:"confidence"`
	// CurrentTokensUsed is the current hourly token usage
	CurrentTokensUsed int64 `json:"current_tokens_used"`
	// TokenLimit is the hourly token limit
	TokenLimit int64 `json:"token_limit"`
	// CurrentCostUsed is the current hourly cost
	CurrentCostUsed float64 `json:"current_cost_used"`
	// CostLimit is the hourly cost limit
	CostLimit float64 `json:"cost_limit"`
	// RecommendedAction is what the user should do
	RecommendedAction string `json:"recommended_action"`
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

// IsValidFailureType validates if a failure type string is valid (vc-228)
// Valid values match the FailureType constants in internal/ai/test_failure.go
func IsValidFailureType(ft string) bool {
	validTypes := map[string]bool{
		"flaky":         true, // Intermittent failure (race condition, timing)
		"real":          true, // Actual bug in code
		"environmental": true, // External dependency issue
		"unknown":       true, // Cannot determine
	}
	return validTypes[ft]
}

// Helper methods for type-safe data access
