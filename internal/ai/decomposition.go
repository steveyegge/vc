package ai

import (
	"context"
	"fmt"
	"os"

	"github.com/steveyegge/vc/internal/types"
)

// DecomposeIssue creates child issues from an assessment's decomposition plan (vc-rzqe)
// Returns the IDs of the created child issues, or an error if creation fails
func (s *Supervisor) DecomposeIssue(ctx context.Context, store IssueStore, parentIssue *types.Issue, plan *DecompositionPlan) ([]string, error) {
	if plan == nil || len(plan.ChildIssues) == 0 {
		return nil, fmt.Errorf("decomposition plan is empty")
	}

	fmt.Printf("🔄 Decomposing %s into %d child issues: %s\n", parentIssue.ID, len(plan.ChildIssues), plan.Reasoning)

	var childIDs []string
	for i, childSpec := range plan.ChildIssues {
		// Create child issue
		childIssue := &types.Issue{
			Title:              childSpec.Title,
			Description:        childSpec.Description,
			AcceptanceCriteria: childSpec.AcceptanceCriteria,
			IssueType:          types.TypeTask, // Decomposed issues are always tasks
			Status:             types.StatusOpen,
			Priority:           childSpec.Priority,
		}

		// Set estimated minutes if provided
		if childSpec.EstimatedMinutes > 0 {
			childIssue.EstimatedMinutes = &childSpec.EstimatedMinutes
		}

		// Create the issue
		if err := store.CreateIssue(ctx, childIssue, "ai-supervisor"); err != nil {
			return childIDs, fmt.Errorf("failed to create child issue %d/%d: %w", i+1, len(plan.ChildIssues), err)
		}

		childID := childIssue.ID
		childIDs = append(childIDs, childID)

		// Add dependency: parent depends on child (child blocks parent)
		dep := &types.Dependency{
			IssueID:     parentIssue.ID,
			DependsOnID: childID,
			Type:        types.DepBlocks,
		}
		if err := store.AddDependency(ctx, dep, "ai-supervisor"); err != nil {
			fmt.Fprintf(os.Stderr, "warning: failed to add dependency %s -> %s: %v\n", parentIssue.ID, childID, err)
		}

		// Add discovered:decomposed label to child to track origin (vc-rzqe)
		if err := store.AddLabel(ctx, childID, types.LabelDiscoveredDecomposed, "ai-supervisor"); err != nil {
			fmt.Fprintf(os.Stderr, "warning: failed to add %s label to %s: %v\n", types.LabelDiscoveredDecomposed, childID, err)
		}

		fmt.Printf("  ✓ Created child issue %s: %s\n", childID, childSpec.Title)
	}

	// Add decomposed label to parent to mark it as a coordinator (vc-rzqe)
	if err := store.AddLabel(ctx, parentIssue.ID, types.LabelDecomposed, "ai-supervisor"); err != nil {
		fmt.Fprintf(os.Stderr, "warning: failed to add %s label to %s: %v\n", types.LabelDecomposed, parentIssue.ID, err)
	}

	// Update parent issue notes to explain decomposition
	notesUpdate := fmt.Sprintf("Decomposed into %d child issues by AI assessment.\n\nReasoning: %s\n\nChild issues: %v",
		len(childIDs), plan.Reasoning, childIDs)
	if err := store.UpdateIssue(ctx, parentIssue.ID, map[string]interface{}{
		"notes": notesUpdate,
	}, "ai-supervisor"); err != nil {
		fmt.Fprintf(os.Stderr, "warning: failed to update parent notes: %v\n", err)
	}

	fmt.Printf("✓ Decomposition complete: %s -> %v\n", parentIssue.ID, childIDs)
	return childIDs, nil
}

// IssueStore defines the storage operations needed for decomposition (vc-rzqe)
// This interface allows decomposition to work with any storage implementation
type IssueStore interface {
	CreateIssue(ctx context.Context, issue *types.Issue, actor string) error
	AddDependency(ctx context.Context, dep *types.Dependency, actor string) error
	AddLabel(ctx context.Context, issueID, label, actor string) error
	UpdateIssue(ctx context.Context, issueID string, updates map[string]interface{}, actor string) error
}
