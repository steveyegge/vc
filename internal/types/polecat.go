package types

// Polecat Mode Types (vc-plr3, vc-sqz4)
//
// These types support VC's polecat mode for Gastown integration.
// In polecat mode, VC executes a single task and outputs JSON to stdout.
// See docs/design/GASTOWN_INTEGRATION.md for full specification.

// PolecatTaskSource identifies where a polecat task came from
type PolecatTaskSource string

const (
	// TaskSourceCLI indicates task came from --task CLI argument
	TaskSourceCLI PolecatTaskSource = "cli"

	// TaskSourceStdin indicates task came from --stdin
	TaskSourceStdin PolecatTaskSource = "stdin"

	// TaskSourceIssue indicates task came from --issue (beads issue)
	TaskSourceIssue PolecatTaskSource = "issue"
)

// IsValid checks if the task source value is valid
func (s PolecatTaskSource) IsValid() bool {
	switch s {
	case TaskSourceCLI, TaskSourceStdin, TaskSourceIssue:
		return true
	}
	return false
}

// PolecatTask represents a task to execute in polecat mode
type PolecatTask struct {
	// Description is the natural language task description
	Description string `json:"description"`

	// Source indicates where the task came from (cli, stdin, issue)
	Source PolecatTaskSource `json:"source"`

	// IssueID is the beads issue ID if Source is TaskSourceIssue
	IssueID string `json:"issue_id,omitempty"`

	// AcceptanceCriteria from the issue (if Source is TaskSourceIssue)
	AcceptanceCriteria string `json:"acceptance_criteria,omitempty"`
}

// PolecatStatus represents the outcome status of polecat execution
type PolecatStatus string

const (
	// PolecatStatusCompleted indicates task is done and gates passed
	PolecatStatusCompleted PolecatStatus = "completed"

	// PolecatStatusPartial indicates some work done but incomplete
	PolecatStatusPartial PolecatStatus = "partial"

	// PolecatStatusBlocked indicates cannot proceed (dependency, unclear requirements)
	PolecatStatusBlocked PolecatStatus = "blocked"

	// PolecatStatusFailed indicates execution failed (gates failed, agent crashed)
	PolecatStatusFailed PolecatStatus = "failed"

	// PolecatStatusDecomposed indicates task was too large, subtasks created
	PolecatStatusDecomposed PolecatStatus = "decomposed"
)

// IsValid checks if the status value is valid
func (s PolecatStatus) IsValid() bool {
	switch s {
	case PolecatStatusCompleted, PolecatStatusPartial, PolecatStatusBlocked, PolecatStatusFailed, PolecatStatusDecomposed:
		return true
	}
	return false
}

// IsSuccess returns true if status indicates successful completion
func (s PolecatStatus) IsSuccess() bool {
	return s == PolecatStatusCompleted
}

// PolecatGateResult represents the result of a single quality gate
type PolecatGateResult struct {
	Passed bool   `json:"passed"`
	Output string `json:"output"`
	Error  string `json:"error,omitempty"`
}

// PolecatDiscoveredIssue represents an issue discovered during execution
type PolecatDiscoveredIssue struct {
	Title       string `json:"title"`
	Description string `json:"description"`
	Type        string `json:"type"` // task, bug, feature
	Priority    int    `json:"priority"`
}

// PolecatDecomposition represents a task decomposition result
type PolecatDecomposition struct {
	Reasoning string            `json:"reasoning"`
	Subtasks  []PolecatSubtask `json:"subtasks"`
}

// PolecatSubtask represents a subtask from decomposition
type PolecatSubtask struct {
	Title    string `json:"title"`
	Priority int    `json:"priority"`
}

// PolecatResult is the JSON output structure for polecat mode execution
//
// This struct is serialized to JSON and written to stdout when VC runs in
// polecat mode. The polecat wrapper (or Gastown) parses this to determine
// success, create discovered issues, and handle results.
//
// Status values:
//   - "completed": Task done, gates passed
//   - "partial": Some work done, but incomplete
//   - "blocked": Cannot proceed (dependency, unclear requirements)
//   - "failed": Execution failed (gates failed, agent crashed)
//   - "decomposed": Task was too large, subtasks created
type PolecatResult struct {
	// Status is the outcome of execution (completed, partial, blocked, failed, decomposed)
	Status PolecatStatus `json:"status"`

	// Success indicates whether the task completed successfully
	Success bool `json:"success"`

	// Iterations is the number of refinement iterations performed
	Iterations int `json:"iterations"`

	// Converged indicates whether AI determined work is complete
	Converged bool `json:"converged"`

	// DurationSeconds is the total execution time
	DurationSeconds float64 `json:"duration_seconds"`

	// FilesModified lists paths of files changed during execution
	FilesModified []string `json:"files_modified"`

	// QualityGates contains results for each quality gate (test, lint, build)
	QualityGates map[string]PolecatGateResult `json:"quality_gates"`

	// DiscoveredIssues contains issues found during execution
	DiscoveredIssues []PolecatDiscoveredIssue `json:"discovered_issues"`

	// PuntedItems lists work explicitly deferred for later
	PuntedItems []string `json:"punted_items"`

	// Summary is a human-readable description of what was done
	Summary string `json:"summary"`

	// Decomposition contains decomposition details if Status is "decomposed"
	Decomposition *PolecatDecomposition `json:"decomposition,omitempty"`

	// Error contains error details if Status is "failed" or "blocked"
	Error *string `json:"error,omitempty"`

	// PreflightResult contains preflight check results if baseline was unhealthy
	PreflightResult map[string]PolecatGateResult `json:"preflight_result,omitempty"`

	// Message is a human-readable status message
	Message string `json:"message,omitempty"`

	// SuggestedAction is a recommended next step for the user/wrapper
	SuggestedAction string `json:"suggested_action,omitempty"`
}

// NewPolecatResult creates a new PolecatResult with sensible defaults
func NewPolecatResult() *PolecatResult {
	return &PolecatResult{
		Status:           PolecatStatusFailed, // Default to failed, set to completed on success
		Success:          false,
		Iterations:       0,
		Converged:        false,
		FilesModified:    []string{},
		QualityGates:     make(map[string]PolecatGateResult),
		DiscoveredIssues: []PolecatDiscoveredIssue{},
		PuntedItems:      []string{},
	}
}

// SetCompleted marks the result as successfully completed
func (r *PolecatResult) SetCompleted(summary string) {
	r.Status = PolecatStatusCompleted
	r.Success = true
	r.Converged = true
	r.Summary = summary
}

// SetFailed marks the result as failed with an error message
func (r *PolecatResult) SetFailed(err string) {
	r.Status = PolecatStatusFailed
	r.Success = false
	r.Error = &err
}

// SetBlocked marks the result as blocked with a reason
func (r *PolecatResult) SetBlocked(reason string, suggestedAction string) {
	r.Status = PolecatStatusBlocked
	r.Success = false
	r.Error = &reason
	r.SuggestedAction = suggestedAction
}

// SetDecomposed marks the result as decomposed with subtasks
func (r *PolecatResult) SetDecomposed(reasoning string, subtasks []PolecatSubtask) {
	r.Status = PolecatStatusDecomposed
	r.Success = true // Decomposition is a valid outcome
	r.Decomposition = &PolecatDecomposition{
		Reasoning: reasoning,
		Subtasks:  subtasks,
	}
}

// AddGateResult adds a quality gate result
func (r *PolecatResult) AddGateResult(gate string, passed bool, output string, err string) {
	r.QualityGates[gate] = PolecatGateResult{
		Passed: passed,
		Output: output,
		Error:  err,
	}
}

// AddDiscoveredIssue adds an issue discovered during execution
func (r *PolecatResult) AddDiscoveredIssue(title, description, issueType string, priority int) {
	r.DiscoveredIssues = append(r.DiscoveredIssues, PolecatDiscoveredIssue{
		Title:       title,
		Description: description,
		Type:        issueType,
		Priority:    priority,
	})
}

// AllGatesPassed returns true if all quality gates passed
func (r *PolecatResult) AllGatesPassed() bool {
	for _, gate := range r.QualityGates {
		if !gate.Passed {
			return false
		}
	}
	return true
}
