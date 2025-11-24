package types

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// Issue represents a trackable work item
type Issue struct {
	ID                 string           `json:"id"`
	Title              string           `json:"title"`
	Description        string           `json:"description"`
	Design             string           `json:"design,omitempty"`
	AcceptanceCriteria string           `json:"acceptance_criteria,omitempty"`
	Notes              string           `json:"notes,omitempty"`
	Status             Status           `json:"status"`
	Priority           int              `json:"priority"`
	IssueType          IssueType        `json:"issue_type"`
	IssueSubtype       IssueSubtype     `json:"issue_subtype,omitempty"` // mission or empty for normal issues
	Assignee           string           `json:"assignee,omitempty"`
	EstimatedMinutes   *int             `json:"estimated_minutes,omitempty"`
	CreatedAt          time.Time        `json:"created_at"`
	UpdatedAt          time.Time        `json:"updated_at"`
	ClosedAt           *time.Time       `json:"closed_at,omitempty"`
	MissionContext     *MissionContext  `json:"mission_context,omitempty"` // vc-234: Populated by GetReadyWork
}

// Validate checks if the issue has valid field values
func (i *Issue) Validate() error {
	if len(i.Title) == 0 {
		return fmt.Errorf("title is required")
	}
	if len(i.Title) > 500 {
		return fmt.Errorf("title must be 500 characters or less (got %d)", len(i.Title))
	}
	if i.Priority < 0 || i.Priority > 4 {
		return fmt.Errorf("priority must be between 0 and 4 (got %d)", i.Priority)
	}
	if !i.Status.IsValid() {
		return fmt.Errorf("invalid status: %s", i.Status)
	}
	if !i.IssueType.IsValid() {
		return fmt.Errorf("invalid issue type: %s", i.IssueType)
	}
	if !i.IssueSubtype.IsValid() {
		return fmt.Errorf("invalid issue subtype: %s", i.IssueSubtype)
	}
	if i.EstimatedMinutes != nil && *i.EstimatedMinutes < 0 {
		return fmt.Errorf("estimated_minutes cannot be negative")
	}

	// Validate acceptance criteria requirements based on issue type (vc-e3j2)
	// Policy: task, bug, and feature types require acceptance criteria
	// Epic and chore types do not require acceptance criteria
	if i.IssueType == TypeTask || i.IssueType == TypeBug || i.IssueType == TypeFeature {
		trimmed := strings.TrimSpace(i.AcceptanceCriteria)
		if trimmed == "" {
			return fmt.Errorf("acceptance_criteria is required for %s issues", i.IssueType)
		}
	}

	return nil
}

// Status represents the current state of an issue
type Status string

const (
	StatusOpen       Status = "open"
	StatusInProgress Status = "in_progress"
	StatusBlocked    Status = "blocked"
	StatusClosed     Status = "closed"
)

// IsValid checks if the status value is valid
func (s Status) IsValid() bool {
	switch s {
	case StatusOpen, StatusInProgress, StatusBlocked, StatusClosed:
		return true
	}
	return false
}

// IssueType categorizes the kind of work
//
// Acceptance Criteria Policy (vc-e3j2):
// - task, bug, feature: REQUIRE non-empty acceptance_criteria
// - epic, chore: acceptance_criteria is OPTIONAL
//
// This policy ensures actionable work items have clear success criteria
// while allowing high-level containers and maintenance work to be flexible.
type IssueType string

const (
	TypeBug     IssueType = "bug"     // Requires acceptance_criteria
	TypeFeature IssueType = "feature" // Requires acceptance_criteria
	TypeTask    IssueType = "task"    // Requires acceptance_criteria
	TypeEpic    IssueType = "epic"    // acceptance_criteria is optional
	TypeChore   IssueType = "chore"   // acceptance_criteria is optional
)

// IsValid checks if the issue type value is valid
func (t IssueType) IsValid() bool {
	switch t {
	case TypeBug, TypeFeature, TypeTask, TypeEpic, TypeChore:
		return true
	}
	return false
}

// IssueSubtype provides additional categorization for epics
type IssueSubtype string

const (
	SubtypeMission IssueSubtype = "mission" // Top-level epic for missions
	SubtypeNormal  IssueSubtype = ""        // Normal issue (not a mission)
)

// IsValid checks if the issue subtype value is valid
func (s IssueSubtype) IsValid() bool {
	switch s {
	case SubtypeMission, SubtypeNormal:
		return true
	}
	return false
}

// Dependency represents a relationship between issues
type Dependency struct {
	IssueID     string         `json:"issue_id"`
	DependsOnID string         `json:"depends_on_id"`
	Type        DependencyType `json:"type"`
	CreatedAt   time.Time      `json:"created_at"`
	CreatedBy   string         `json:"created_by"`
}

// DependencyType categorizes the relationship between issues
type DependencyType string

const (
	// DepBlocks indicates the issue is blocked by another issue
	DepBlocks DependencyType = "blocks"
	// DepRelated indicates the issue is related to another issue
	DepRelated DependencyType = "related"
	// DepParentChild indicates a parent-child relationship (epic to tasks)
	DepParentChild DependencyType = "parent-child"
	// DepDiscoveredFrom indicates the issue was discovered during work on another issue
	// Used by AI analysis to track punted work, discovered bugs, and quality issues
	DepDiscoveredFrom DependencyType = "discovered-from"
)

// IsValid checks if the dependency type value is valid
func (d DependencyType) IsValid() bool {
	switch d {
	case DepBlocks, DepRelated, DepParentChild, DepDiscoveredFrom:
		return true
	}
	return false
}

// Label represents a tag on an issue
type Label struct {
	IssueID string `json:"issue_id"`
	Label   string `json:"label"`
}

// Event represents an audit trail entry
type Event struct {
	ID        int64      `json:"id"`
	IssueID   string     `json:"issue_id"`
	EventType EventType  `json:"event_type"`
	Actor     string     `json:"actor"`
	OldValue  *string    `json:"old_value,omitempty"`
	NewValue  *string    `json:"new_value,omitempty"`
	Comment   *string    `json:"comment,omitempty"`
	CreatedAt time.Time  `json:"created_at"`
}

// EventCounts holds event count statistics for monitoring
type EventCounts struct {
	TotalEvents      int
	EventsByIssue    map[string]int
	EventsBySeverity map[string]int
	EventsByType     map[string]int
}

// EventType categorizes audit trail events
type EventType string

const (
	EventCreated           EventType = "created"
	EventUpdated           EventType = "updated"
	EventStatusChanged     EventType = "status_changed"
	EventCommented         EventType = "commented"
	EventClosed            EventType = "closed"
	EventReopened          EventType = "reopened"
	EventDependencyAdded   EventType = "dependency_added"
	EventDependencyRemoved EventType = "dependency_removed"
	EventLabelAdded        EventType = "label_added"
	EventLabelRemoved      EventType = "label_removed"
	EventWatchdog          EventType = "watchdog"
)

// BlockedIssue extends Issue with blocking information
type BlockedIssue struct {
	Issue
	BlockedByCount int      `json:"blocked_by_count"`
	BlockedBy      []string `json:"blocked_by"`
}

// TreeNode represents a node in a dependency tree
type TreeNode struct {
	Issue
	Depth     int  `json:"depth"`
	Truncated bool `json:"truncated"`
}

// Statistics provides aggregate metrics
type Statistics struct {
	TotalIssues      int     `json:"total_issues"`
	OpenIssues       int     `json:"open_issues"`
	InProgressIssues int     `json:"in_progress_issues"`
	ClosedIssues     int     `json:"closed_issues"`
	BlockedIssues    int     `json:"blocked_issues"`
	ReadyIssues      int     `json:"ready_issues"`
	AverageLeadTime  float64 `json:"average_lead_time_hours"`
}

// IssueFilter is used to filter issue queries
type IssueFilter struct {
	Status    *Status
	Priority  *int
	IssueType *IssueType
	Type      *IssueType // Alias for IssueType (for compatibility)
	Assignee  *string
	Labels    []string
	Limit     int
}

// WorkFilter is used to filter ready work queries
// SortPolicy determines how ready work is ordered (from Beads)
type SortPolicy string

// Sort policy constants (must match Beads library)
const (
	// SortPolicyHybrid prioritizes recent issues by priority, older by age (default)
	SortPolicyHybrid SortPolicy = "hybrid"

	// SortPolicyPriority always sorts by priority first, then creation date
	// Use for autonomous execution - VC's default
	SortPolicyPriority SortPolicy = "priority"

	// SortPolicyOldest always sorts by creation date (oldest first)
	SortPolicyOldest SortPolicy = "oldest"
)

type WorkFilter struct {
	Status     Status
	Priority   *int
	Assignee   *string
	Limit      int
	SortPolicy SortPolicy
}

// ExecutorStatus represents the state of an executor instance
type ExecutorStatus string

const (
	ExecutorStatusRunning ExecutorStatus = "running"
	ExecutorStatusStopped ExecutorStatus = "stopped"
)

// IsValid checks if the executor status value is valid
func (s ExecutorStatus) IsValid() bool {
	return s == ExecutorStatusRunning || s == ExecutorStatusStopped
}

// ExecutorInstance represents a running executor instance
type ExecutorInstance struct {
	InstanceID       string         `json:"instance_id"`
	Hostname         string         `json:"hostname"`
	PID              int            `json:"pid"`
	Status           ExecutorStatus `json:"status"`
	StartedAt        time.Time      `json:"started_at"`
	LastHeartbeat    time.Time      `json:"last_heartbeat"`
	Version          string         `json:"version"`
	Metadata         string         `json:"metadata"` // JSON string (must be valid JSON)
	SelfHealingMode  string         `json:"self_healing_mode"` // vc-556f: HEALTHY, SELF_HEALING, or ESCALATED
}

// Validate checks if the executor instance has valid field values
func (e *ExecutorInstance) Validate() error {
	if e.InstanceID == "" {
		return fmt.Errorf("instance_id is required")
	}
	if e.Hostname == "" {
		return fmt.Errorf("hostname is required")
	}
	if e.PID <= 0 {
		return fmt.Errorf("pid must be positive (got %d)", e.PID)
	}
	if !e.Status.IsValid() {
		return fmt.Errorf("invalid status: %s", e.Status)
	}
	// Validate metadata is valid JSON
	if e.Metadata != "" {
		var v interface{}
		if err := json.Unmarshal([]byte(e.Metadata), &v); err != nil {
			return fmt.Errorf("metadata must be valid JSON: %w", err)
		}
	}
	return nil
}

// ExecutionState represents the state of issue execution
type ExecutionState string

const (
	ExecutionStatePending    ExecutionState = "pending"    // Initial state (not yet claimed)
	ExecutionStateClaimed    ExecutionState = "claimed"    // Claimed by executor
	ExecutionStateAssessing  ExecutionState = "assessing"  // AI is assessing the task
	ExecutionStateExecuting  ExecutionState = "executing"  // Agent is executing the work
	ExecutionStateAnalyzing  ExecutionState = "analyzing"  // AI is analyzing the result
	ExecutionStateGates      ExecutionState = "gates"      // Running quality gates
	ExecutionStateCommitting ExecutionState = "committing" // Committing changes
	ExecutionStateCompleted  ExecutionState = "completed"  // Successfully completed
	ExecutionStateFailed     ExecutionState = "failed"     // Failed (terminal state)
)

// IsValid checks if the execution state value is valid
func (s ExecutionState) IsValid() bool {
	switch s {
	case ExecutionStatePending, ExecutionStateClaimed, ExecutionStateAssessing, ExecutionStateExecuting,
		ExecutionStateAnalyzing, ExecutionStateGates, ExecutionStateCommitting,
		ExecutionStateCompleted, ExecutionStateFailed:
		return true
	}
	return false
}

// ValidTransitions defines the valid state transitions for the execution state machine.
// The state machine ensures predictable progression through the execution lifecycle.
//
// State Machine Diagram:
//
//	pending → claimed → assessing → executing → analyzing → gates → committing → completed
//	    ↓         ↓         ↓           ↓           ↓         ↓          ↓
//	  failed    failed    failed      failed      failed    failed     failed
//
// Valid transitions:
//   - pending → claimed (executor claims the issue)
//   - claimed → assessing (start AI assessment)
//   - assessing → executing (assessment complete, start work)
//   - executing → analyzing (work complete, start AI analysis)
//   - analyzing → gates (analysis complete, run quality gates)
//   - gates → committing (gates passed, commit changes)
//   - committing → completed (changes committed successfully)
//   - any state → failed (error occurred at any stage)
func (s ExecutionState) ValidTransitions() []ExecutionState {
	switch s {
	case ExecutionStatePending:
		return []ExecutionState{ExecutionStateClaimed, ExecutionStateFailed}
	case ExecutionStateClaimed:
		return []ExecutionState{ExecutionStateAssessing, ExecutionStateFailed}
	case ExecutionStateAssessing:
		return []ExecutionState{ExecutionStateExecuting, ExecutionStateFailed}
	case ExecutionStateExecuting:
		return []ExecutionState{ExecutionStateAnalyzing, ExecutionStateFailed}
	case ExecutionStateAnalyzing:
		return []ExecutionState{ExecutionStateGates, ExecutionStateFailed}
	case ExecutionStateGates:
		return []ExecutionState{ExecutionStateCommitting, ExecutionStateFailed}
	case ExecutionStateCommitting:
		return []ExecutionState{ExecutionStateCompleted, ExecutionStateFailed}
	case ExecutionStateCompleted:
		return []ExecutionState{} // Terminal state
	case ExecutionStateFailed:
		return []ExecutionState{} // Terminal state
	default:
		return []ExecutionState{}
	}
}

// CanTransitionTo checks if a transition from this state to the target state is valid
func (s ExecutionState) CanTransitionTo(target ExecutionState) bool {
	validTransitions := s.ValidTransitions()
	for _, valid := range validTransitions {
		if valid == target {
			return true
		}
	}
	return false
}

// IssueExecutionState tracks the execution state of an issue being processed by an executor
type IssueExecutionState struct {
	IssueID               string         `json:"issue_id"`
	ExecutorInstanceID    string         `json:"executor_instance_id"`
	State                 ExecutionState `json:"state"`
	CheckpointData        string         `json:"checkpoint_data"` // JSON string (must be valid JSON)
	ClaimedAt             time.Time      `json:"claimed_at"`
	StartedAt             time.Time      `json:"started_at"`
	UpdatedAt             time.Time      `json:"updated_at"`
	ErrorMessage          string         `json:"error_message,omitempty"`
	InterventionCount     int            `json:"intervention_count"`              // vc-165b: Count of watchdog interventions
	LastInterventionTime  *time.Time     `json:"last_intervention_time,omitempty"` // vc-165b: When last intervention occurred
}

// Validate checks if the issue execution state has valid field values
func (s *IssueExecutionState) Validate() error {
	if s.IssueID == "" {
		return fmt.Errorf("issue_id is required")
	}
	if s.ExecutorInstanceID == "" {
		return fmt.Errorf("executor_instance_id is required")
	}
	if !s.State.IsValid() {
		return fmt.Errorf("invalid state: %s", s.State)
	}
	// Validate checkpoint_data is valid JSON
	if s.CheckpointData != "" {
		var v interface{}
		if err := json.Unmarshal([]byte(s.CheckpointData), &v); err != nil {
			return fmt.Errorf("checkpoint_data must be valid JSON: %w", err)
		}
	}
	return nil
}

// ExecutionAttempt represents a single execution attempt for an issue.
// Multiple attempts may occur due to retries, resumption after failures,
// or iterative refinement.
type ExecutionAttempt struct {
	ID                 int64      `json:"id"`
	IssueID            string     `json:"issue_id"`
	ExecutorInstanceID string     `json:"executor_instance_id"`
	AttemptNumber      int        `json:"attempt_number"`
	StartedAt          time.Time  `json:"started_at"`
	CompletedAt        *time.Time `json:"completed_at,omitempty"`
	Success            *bool      `json:"success,omitempty"` // nil if not completed yet
	ExitCode           *int       `json:"exit_code,omitempty"`
	Summary            string     `json:"summary"`
	OutputSample       string     `json:"output_sample"` // Truncated output (last 1000 lines)
	ErrorSample        string     `json:"error_sample"`  // Truncated errors (last 1000 lines)
}

// Validate checks if the execution attempt has valid field values
func (a *ExecutionAttempt) Validate() error {
	if a.IssueID == "" {
		return fmt.Errorf("issue_id is required")
	}
	if a.ExecutorInstanceID == "" {
		return fmt.Errorf("executor_instance_id is required")
	}
	if a.AttemptNumber < 1 {
		return fmt.Errorf("attempt_number must be positive (got %d)", a.AttemptNumber)
	}
	return nil
}

// GateResult represents the result of a quality gate check
// vc-198: Used in preflight quality gates cache
type GateResult struct {
	Gate   string `json:"gate"`
	Passed bool   `json:"passed"`
	Output string `json:"output"`
	Error  string `json:"error,omitempty"`
}

// FailureType categorizes test failures
// vc-9aa9: Moved from ai package to avoid import cycles
type FailureType string

const (
	FailureTypeFlaky         FailureType = "flaky"         // Intermittent failure (race condition, timing)
	FailureTypeReal          FailureType = "real"          // Actual bug in code
	FailureTypeEnvironmental FailureType = "environmental" // External dependency issue
	FailureTypeUnknown       FailureType = "unknown"       // Cannot determine
)

// TestFailureDiagnosis represents AI diagnosis of a test failure
// vc-210: Self-healing - AI agent can fix baseline test failures
// vc-9aa9: Moved from ai package to avoid import cycles with storage
type TestFailureDiagnosis struct {
	FailureType  FailureType `json:"failure_type"`  // Type of failure: flaky, real, or environmental
	RootCause    string      `json:"root_cause"`    // Detailed explanation of why the test is failing
	ProposedFix  string      `json:"proposed_fix"`  // Proposed fix with rationale
	Confidence   float64     `json:"confidence"`    // Confidence in the diagnosis (0.0-1.0)
	TestNames    []string    `json:"test_names"`    // List of failing test names
	StackTraces  []string    `json:"stack_traces"`  // Relevant stack traces
	Verification []string    `json:"verification"`  // Steps to verify the fix works
}
