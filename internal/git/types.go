package git

import (
	"context"
)

// GitOperations provides git operations for the executor.
// This interface is designed to be implementation-agnostic,
// allowing for testing with mock implementations.
type GitOperations interface {
	// HasUncommittedChanges checks if there are uncommitted changes in the repository.
	// Returns true if there are staged or unstaged changes, false otherwise.
	HasUncommittedChanges(ctx context.Context, repoPath string) (bool, error)

	// GetStatus returns detailed git status information.
	GetStatus(ctx context.Context, repoPath string) (*Status, error)

	// CommitChanges creates a commit with the given message.
	// Returns the commit hash if successful.
	CommitChanges(ctx context.Context, repoPath string, opts CommitOptions) (string, error)

	// Rebase performs a git rebase operation.
	// Returns the result including conflict detection and status.
	Rebase(ctx context.Context, repoPath string, opts RebaseOptions) (*RebaseResult, error)
}

// Status represents the git status of a repository.
type Status struct {
	// Modified files (staged or unstaged)
	Modified []string

	// Untracked files
	Untracked []string

	// Deleted files
	Deleted []string

	// Added files (staged)
	Added []string

	// Renamed files
	Renamed []string

	// HasChanges is true if any changes exist
	HasChanges bool
}

// CommitOptions configures a git commit operation.
type CommitOptions struct {
	// Message is the commit message
	Message string

	// Author specifies the author (optional, uses git config if empty)
	Author string

	// CoAuthors is a list of co-authors to add to the commit message
	CoAuthors []string

	// AddAll stages all changes before committing (git add -A)
	AddAll bool

	// AllowEmpty allows creating an empty commit
	AllowEmpty bool
}

// CommitMessageRequest contains information for generating a commit message.
type CommitMessageRequest struct {
	// IssueID is the issue being worked on
	IssueID string

	// IssueTitle is the title of the issue
	IssueTitle string

	// IssueDescription provides context about the issue
	IssueDescription string

	// ChangedFiles lists the files that were modified
	ChangedFiles []string

	// Diff is the git diff output (optional, can be large)
	Diff string
}

// CommitMessageResponse contains the AI-generated commit message.
type CommitMessageResponse struct {
	// Subject is the commit subject line (50 chars or less)
	Subject string `json:"subject"`

	// Body is the detailed commit message body
	Body string `json:"body"`

	// Reasoning explains why this message was chosen
	Reasoning string `json:"reasoning"`
}

// RebaseOptions configures a git rebase operation.
type RebaseOptions struct {
	// BaseBranch is the branch to rebase onto (e.g., "main", "origin/main")
	BaseBranch string

	// Abort will abort an in-progress rebase if true
	// This is mutually exclusive with BaseBranch
	Abort bool

	// Continue will continue a rebase after resolving conflicts
	// This is mutually exclusive with BaseBranch and Abort
	Continue bool
}

// RebaseResult contains the outcome of a rebase operation.
type RebaseResult struct {
	// Success indicates whether the rebase completed successfully
	Success bool

	// HasConflicts indicates whether merge conflicts were detected
	HasConflicts bool

	// ConflictedFiles lists files with merge conflicts
	ConflictedFiles []string

	// CurrentBranch is the branch that was being rebased
	CurrentBranch string

	// BaseBranch is the branch being rebased onto
	BaseBranch string

	// ErrorMessage contains any error message from the rebase operation
	ErrorMessage string

	// AbortedSuccessfully indicates if an abort operation succeeded
	AbortedSuccessfully bool
}

// RebaseCheckpoint stores rebase state for resuming interrupted operations.
// This can be stored in IssueExecutionState.CheckpointData as JSON.
type RebaseCheckpoint struct {
	// InProgress indicates if a rebase is currently in progress
	InProgress bool `json:"in_progress"`

	// BaseBranch is the branch being rebased onto
	BaseBranch string `json:"base_branch,omitempty"`

	// CurrentBranch is the branch being rebased
	CurrentBranch string `json:"current_branch,omitempty"`

	// ConflictedFiles lists files with unresolved conflicts
	ConflictedFiles []string `json:"conflicted_files,omitempty"`

	// StartedAt records when the rebase started
	StartedAt string `json:"started_at,omitempty"` // RFC3339 timestamp

	// ErrorMessage contains any error from the rebase
	ErrorMessage string `json:"error_message,omitempty"`
}
