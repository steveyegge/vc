package executor

import (
	"context"

	"github.com/steveyegge/vc/internal/gates"
	"github.com/steveyegge/vc/internal/types"
)

// PromptContext contains comprehensive context for prompt generation and execution.
// It aggregates all information needed for AI-supervised task execution, including
// issue details, execution history, dependencies, git state, and quality gate results.
type PromptContext struct {
	// Issue is the current issue being processed
	Issue *types.Issue

	// Sandbox contains the sandbox environment context (nil if not using sandbox)
	// TODO: Define sandbox.SandboxContext when sandbox package is implemented
	Sandbox interface{} // Placeholder for *sandbox.SandboxContext

	// ParentMission is the parent issue if this is a subtask
	ParentMission *types.Issue

	// RelatedIssues contains all dependency and relationship information
	RelatedIssues *RelatedIssues

	// PreviousAttempts tracks execution history for this issue
	PreviousAttempts []*types.ExecutionAttempt

	// QualityGateStatus contains results from quality gate checks
	QualityGateStatus *GateStatus

	// GitState captures the current git repository state
	GitState *GitState

	// ResumeHint provides AI with context about where execution left off
	// Used for resuming after crashes or partial completion
	ResumeHint string
}

// RelatedIssues contains all issues related to the current issue through various
// dependency relationships. This provides comprehensive context about blockers,
// dependents, and related work.
type RelatedIssues struct {
	// Blockers are issues that must be completed before this one can proceed
	Blockers []*types.Issue

	// Dependents are issues that depend on this one being completed
	Dependents []*types.Issue

	// Siblings are other children of the same parent issue
	Siblings []*types.Issue

	// Related are issues connected through non-blocking relationships
	Related []*types.Issue
}

// GitState captures the current state of the git repository.
// This helps AI understand the working directory context and whether
// changes need to be committed or stashed.
type GitState struct {
	// CurrentBranch is the name of the active git branch
	CurrentBranch string

	// UncommittedChanges indicates if there are uncommitted modifications
	UncommittedChanges bool

	// ModifiedFiles lists files that have been modified but not committed
	ModifiedFiles []string

	// Status contains the raw output from 'git status --porcelain'
	Status string
}

// GateStatus represents the results of quality gate execution.
// This wraps the gates package results in a convenient structure for context.
type GateStatus struct {
	// Results contains individual gate results (test, lint, build)
	Results []*gates.Result

	// AllPassed indicates whether all gates passed
	AllPassed bool
}

// ContextGatherer is responsible for gathering comprehensive context for issue execution.
// It aggregates information from storage, git, previous attempts, and related issues
// to build a complete PromptContext for AI-supervised execution.
type ContextGatherer interface {
	// GatherContext builds complete context for an issue execution
	// Returns nil error if context gathering succeeds, error otherwise
	GatherContext(ctx context.Context, issue *types.Issue, sandbox interface{}) (*PromptContext, error)

	// GetParentMission retrieves the parent issue if this is a subtask
	// Returns nil if the issue has no parent
	GetParentMission(ctx context.Context, issue *types.Issue) (*types.Issue, error)

	// GetRelatedIssues retrieves all issues related through dependencies
	// Includes blockers, dependents, siblings, and other relationships
	GetRelatedIssues(ctx context.Context, issue *types.Issue) (*RelatedIssues, error)

	// GetPreviousAttempts retrieves execution history for an issue
	// Returns attempts in chronological order (oldest first)
	GetPreviousAttempts(ctx context.Context, issueID string) ([]*types.ExecutionAttempt, error)

	// AnalyzeResumeState examines sandbox state and previous attempts to determine
	// where execution left off. Returns a human-readable hint for the AI.
	AnalyzeResumeState(ctx context.Context, sandbox interface{}, attempts []*types.ExecutionAttempt) (string, error)
}
