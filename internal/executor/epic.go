package executor

import (
	"context"
	"fmt"

	"github.com/steveyegge/vc/internal/storage"
	"github.com/steveyegge/vc/internal/types"
)

// checkEpicCompletion checks if an issue's parent epic is now complete
// An epic is complete when all its child issues are closed
func checkEpicCompletion(ctx context.Context, store storage.Storage, issueID string) error {
	// Get the issue to check its parent
	issue, err := store.GetIssue(ctx, issueID)
	if err != nil {
		return fmt.Errorf("failed to get issue: %w", err)
	}
	if issue == nil {
		return fmt.Errorf("issue not found: %s", issueID)
	}

	// Get parent issues (issues this one depends on via parent-child relationship)
	deps, err := store.GetDependencies(ctx, issueID)
	if err != nil {
		return fmt.Errorf("failed to get dependencies: %w", err)
	}

	// Find parent epic(s)
	for _, dep := range deps {
		if dep.IssueType == types.TypeEpic {
			// Check if all children of this epic are closed
			if err := checkAndCloseEpicIfComplete(ctx, store, dep.ID); err != nil {
				// Log but don't fail - this is a best-effort check
				fmt.Printf("Warning: failed to check epic completion for %s: %v\n", dep.ID, err)
			}
		}
	}

	return nil
}

// checkAndCloseEpicIfComplete checks if an epic is complete and closes it if so
func checkAndCloseEpicIfComplete(ctx context.Context, store storage.Storage, epicID string) error {
	// Get the epic
	epic, err := store.GetIssue(ctx, epicID)
	if err != nil {
		return fmt.Errorf("failed to get epic: %w", err)
	}
	if epic == nil {
		return fmt.Errorf("epic not found: %s", epicID)
	}

	// Skip if already closed
	if epic.Status == types.StatusClosed {
		return nil
	}

	// Get all issues that depend on this epic (its children)
	children, err := store.GetDependents(ctx, epicID)
	if err != nil {
		return fmt.Errorf("failed to get epic children: %w", err)
	}

	// If no children, don't auto-close (epics should have children)
	if len(children) == 0 {
		return nil
	}

	// Check if all children are closed
	allClosed := true
	for _, child := range children {
		if child.Status != types.StatusClosed {
			allClosed = false
			break
		}
	}

	// If all children are closed, close the epic
	if allClosed {
		fmt.Printf("All children of epic %s are complete, closing epic\n", epicID)

		reason := fmt.Sprintf("All %d child issues completed", len(children))
		if err := store.CloseIssue(ctx, epicID, reason, "executor"); err != nil {
			return fmt.Errorf("failed to close epic: %w", err)
		}

		fmt.Printf("✓ Closed epic %s: %s\n", epicID, epic.Title)
	}

	return nil
}
