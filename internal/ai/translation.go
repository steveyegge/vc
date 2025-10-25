package ai

import (
	"context"
	"fmt"
	"os"

	"github.com/steveyegge/vc/internal/priorities"
	"github.com/steveyegge/vc/internal/types"
)

// DiscoveredIssue represents a new issue discovered during execution
type DiscoveredIssue struct {
	Title        string `json:"title"`
	Description  string `json:"description"`
	Type         string `json:"type"`          // bug, task, enhancement, etc.
	Priority     string `json:"priority"`      // P0, P1, P2, P3
	DiscoveryType string `json:"discovery_type"` // blocker, related, background (vc-151)
}

// CreateDiscoveredIssues creates issues from the AI analysis
func (s *Supervisor) CreateDiscoveredIssues(ctx context.Context, parentIssue *types.Issue, discovered []DiscoveredIssue) ([]string, error) {
	var createdIDs []string

	for _, disc := range discovered {
		// Calculate priority based on discovery type and parent priority (vc-152)
		// This overrides the AI-suggested priority string (disc.Priority) for blockers/related/background
		// The AI's priority suggestion is stored but not used (may be useful for future enhancements)
		priority := priorities.CalculateDiscoveredPriority(parentIssue.Priority, disc.DiscoveryType)

		// Map string type to types.IssueType
		issueType := types.TypeTask // default
		switch disc.Type {
		case "bug":
			issueType = types.TypeBug
		case "task":
			issueType = types.TypeTask
		case "feature", "enhancement":
			issueType = types.TypeFeature
		case "epic":
			issueType = types.TypeEpic
		case "chore":
			issueType = types.TypeChore
		}

		// Create the issue
		newIssue := &types.Issue{
			Title:       disc.Title,
			Description: disc.Description + fmt.Sprintf("\n\n_Discovered during execution of %s_", parentIssue.ID),
			IssueType:   issueType,
			Status:      types.StatusOpen,
			Priority:    priority, // Use calculated priority (vc-152)
			Assignee:    "ai-supervisor",
		}

		err := s.store.CreateIssue(ctx, newIssue, "ai-supervisor")
		if err != nil {
			return createdIDs, fmt.Errorf("failed to create discovered issue: %w", err)
		}

		// The ID is set on the issue by CreateIssue
		id := newIssue.ID

		createdIDs = append(createdIDs, id)
		fmt.Printf("Created discovered issue %s: %s\n", id, disc.Title)

		// Add discovery type label (vc-151)
		if disc.DiscoveryType != "" {
			label := fmt.Sprintf("discovered:%s", disc.DiscoveryType)
			if err := s.store.AddLabel(ctx, id, label, "ai-supervisor"); err != nil {
				fmt.Fprintf(os.Stderr, "warning: failed to add label %s to %s: %v\n", label, id, err)
			} else {
				fmt.Printf("  Added label: %s\n", label)
			}
		}

		// Add a dependency: new issue was discovered from parent
		// This ensures discovered work doesn't get lost and is tracked properly
		dep := &types.Dependency{
			IssueID:     id,
			DependsOnID: parentIssue.ID,
			Type:        types.DepDiscoveredFrom,
		}
		if err := s.store.AddDependency(ctx, dep, "ai-supervisor"); err != nil {
			fmt.Fprintf(os.Stderr, "warning: failed to add dependency %s -> %s: %v\n", id, parentIssue.ID, err)
		}
	}

	return createdIDs, nil
}
