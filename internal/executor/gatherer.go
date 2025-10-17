package executor

import (
	"context"
	"fmt"
	"strings"

	"github.com/steveyegge/vc/internal/storage"
	"github.com/steveyegge/vc/internal/types"
)

// contextGatherer implements the ContextGatherer interface
type contextGatherer struct {
	store storage.Storage
}

// NewContextGatherer creates a new context gatherer
func NewContextGatherer(store storage.Storage) ContextGatherer {
	return &contextGatherer{
		store: store,
	}
}

// GatherContext builds complete context for an issue execution
// It collects information from all available sources: issue tree, history,
// dependencies, git state, and quality gates
func (g *contextGatherer) GatherContext(ctx context.Context, issue *types.Issue, sandbox interface{}) (*PromptContext, error) {
	pc := &PromptContext{
		Issue: issue,
	}

	// 1. Get parent mission if this is a child task
	if parent, err := g.GetParentMission(ctx, issue); err == nil && parent != nil {
		pc.ParentMission = parent
	}

	// 2. Get related issues (blockers, dependents, siblings)
	if related, err := g.GetRelatedIssues(ctx, issue); err == nil {
		pc.RelatedIssues = related
	}

	// 3. Get previous execution attempts
	if attempts, err := g.GetPreviousAttempts(ctx, issue.ID); err == nil {
		pc.PreviousAttempts = attempts
	}

	// 4. Get quality gate status if any
	// TODO: Implement quality gate status when gates package is ready
	pc.QualityGateStatus = nil

	// 5. Get sandbox context if available
	// TODO: Implement sandbox inspection when sandbox package is ready
	pc.Sandbox = sandbox
	pc.GitState = nil

	// 6. Analyze resume state if we have previous attempts
	if len(pc.PreviousAttempts) > 0 {
		if hint, err := g.AnalyzeResumeState(ctx, sandbox, pc.PreviousAttempts); err == nil {
			pc.ResumeHint = hint
		}
	}

	return pc, nil
}

// GetParentMission retrieves the parent issue if this is a subtask
// Returns nil if the issue has no parent or if there's an error
func (g *contextGatherer) GetParentMission(ctx context.Context, issue *types.Issue) (*types.Issue, error) {
	// Get dependency records to find parent-child relationships
	deps, err := g.store.GetDependencyRecords(ctx, issue.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to get dependency records: %w", err)
	}

	// Find the first parent-child dependency
	for _, dep := range deps {
		if dep.Type == types.DepParentChild {
			// The parent is the issue this depends on
			parent, err := g.store.GetIssue(ctx, dep.DependsOnID)
			if err != nil {
				return nil, fmt.Errorf("failed to get parent issue %s: %w", dep.DependsOnID, err)
			}
			return parent, nil
		}
	}

	// No parent found
	return nil, nil
}

// GetRelatedIssues retrieves all issues related through dependencies
// Includes blockers, dependents, siblings, and other relationships
func (g *contextGatherer) GetRelatedIssues(ctx context.Context, issue *types.Issue) (*RelatedIssues, error) {
	ri := &RelatedIssues{}

	// Get all dependency records to separate blockers from parent-child
	depRecords, err := g.store.GetDependencyRecords(ctx, issue.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to get dependency records: %w", err)
	}

	// Separate blockers from parent-child dependencies
	var blockerIDs []string
	for _, dep := range depRecords {
		if dep.Type == types.DepBlocks {
			blockerIDs = append(blockerIDs, dep.DependsOnID)
		}
	}

	// Fetch blocker issues
	for _, blockerID := range blockerIDs {
		blocker, err := g.store.GetIssue(ctx, blockerID)
		if err == nil && blocker != nil {
			ri.Blockers = append(ri.Blockers, blocker)
		}
	}

	// Get dependents (issues that depend on this one)
	dependents, err := g.store.GetDependents(ctx, issue.ID)
	if err == nil {
		ri.Dependents = dependents
	}

	// Get siblings (other children of same parent)
	parent, err := g.GetParentMission(ctx, issue)
	if err == nil && parent != nil {
		// Get all children of the parent
		allChildren, err := g.store.GetDependents(ctx, parent.ID)
		if err == nil {
			// Filter out the current issue
			for _, child := range allChildren {
				if child.ID != issue.ID {
					ri.Siblings = append(ri.Siblings, child)
				}
			}
		}
	}

	return ri, nil
}

// GetPreviousAttempts retrieves execution history for an issue
// Returns attempts in chronological order (oldest first)
func (g *contextGatherer) GetPreviousAttempts(ctx context.Context, issueID string) ([]*types.ExecutionAttempt, error) {
	attempts, err := g.store.GetExecutionHistory(ctx, issueID)
	if err != nil {
		return nil, fmt.Errorf("failed to get execution history: %w", err)
	}
	return attempts, nil
}

// AnalyzeResumeState examines sandbox state and previous attempts to determine
// where execution left off. Returns a human-readable hint for the AI.
func (g *contextGatherer) AnalyzeResumeState(ctx context.Context, sandbox interface{}, attempts []*types.ExecutionAttempt) (string, error) {
	if len(attempts) == 0 {
		return "", nil
	}

	// Analyze the most recent attempt
	lastAttempt := attempts[len(attempts)-1]

	var hint strings.Builder
	hint.WriteString(fmt.Sprintf("Previous attempt #%d ", lastAttempt.AttemptNumber))

	// Check if the attempt completed
	if lastAttempt.CompletedAt == nil {
		hint.WriteString("did not complete (may have crashed). ")
	} else if lastAttempt.Success != nil && *lastAttempt.Success {
		hint.WriteString("succeeded but may have punted work. ")
	} else if lastAttempt.ExitCode != nil {
		hint.WriteString(fmt.Sprintf("failed with exit code %d. ", *lastAttempt.ExitCode))
	} else {
		hint.WriteString("completed with unknown status. ")
	}

	// Add summary if available
	if lastAttempt.Summary != "" {
		hint.WriteString(fmt.Sprintf("Summary: %s ", lastAttempt.Summary))
	}

	// Add error information if available
	if lastAttempt.ErrorSample != "" {
		// Truncate error sample if too long
		errorSample := lastAttempt.ErrorSample
		if len(errorSample) > 200 {
			errorSample = errorSample[:200] + "..."
		}
		hint.WriteString(fmt.Sprintf("Error: %s ", errorSample))
	}

	// TODO: Add sandbox/git state analysis when sandbox package is ready
	// if sandbox != nil {
	//     // Check for modified files, uncommitted changes, etc.
	// }

	hint.WriteString("Please assess the current state and continue from where we left off.")

	return hint.String(), nil
}
