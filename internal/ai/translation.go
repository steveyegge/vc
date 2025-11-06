package ai

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/steveyegge/vc/internal/priorities"
	"github.com/steveyegge/vc/internal/types"
)

// DiscoveredIssue represents a new issue discovered during execution
type DiscoveredIssue struct {
	Title              string   `json:"title"`
	Description        string   `json:"description"`
	Type               string   `json:"type"`               // bug, task, enhancement, etc.
	Priority           string   `json:"priority"`           // P0, P1, P2, P3
	DiscoveryType      string   `json:"discovery_type"`     // blocker, related, background (vc-151)
	AcceptanceCriteria string   `json:"acceptance_criteria,omitempty"` // vc-4vot: Required for meta-issues
	Labels             []string `json:"labels,omitempty"`   // vc-4vot: AI-set labels (e.g., "meta-issue")
}

// No heuristic pattern matching - we'll rely on AI to set labels

// getBlockerDepth calculates the depth of discovered:blocker issues in the dependency chain
// Returns the depth (0 = root task, 1 = first blocker, 2 = blocker of blocker, etc.)
func (s *Supervisor) getBlockerDepth(ctx context.Context, parentIssue *types.Issue) (int, error) {
	depth := 0
	currentID := parentIssue.ID
	maxDepth := 10 // Safety limit to prevent infinite loops

	for depth < maxDepth {
		// Check if this issue has discovered:blocker label
		hasBlockerLabel := false
		labels, err := s.store.GetLabels(ctx, currentID)
		if err != nil {
			return depth, fmt.Errorf("failed to get labels for %s: %w", currentID, err)
		}

		for _, label := range labels {
			if label == "discovered:blocker" {
				hasBlockerLabel = true
				break
			}
		}

		if !hasBlockerLabel {
			// This is not a blocker - we've reached the root
			return depth, nil
		}

		// This is a blocker - increment depth and look for its parent
		depth++

		// Find the parent (discovered_from dependency)
		deps, err := s.store.GetDependencyRecords(ctx, currentID)
		if err != nil {
			return depth, fmt.Errorf("failed to get dependencies for %s: %w", currentID, err)
		}

		found := false
		for _, dep := range deps {
			if dep.Type == types.DepDiscoveredFrom && dep.IssueID == currentID {
				// This issue was discovered from dep.DependsOnID
				currentID = dep.DependsOnID
				found = true
				break
			}
		}

		if !found {
			// No parent found - this is the root
			return depth, nil
		}
	}

	return depth, fmt.Errorf("dependency chain too deep (>%d), possible circular dependency", maxDepth)
}

// hasLabel checks if a label exists in a slice of label names
func hasLabel(labels []string, label string) bool {
	for _, l := range labels {
		if l == label {
			return true
		}
	}
	return false
}

// isCircularMetaIssue checks if creating this meta-issue would create a circular dependency
// Example: vc-hpcl needs criteria → vc-9yhu adds criteria to vc-hpcl → vc-qo2u adds criteria to vc-9yhu
// vc-4vot: Uses labels set by AI, not heuristic pattern matching
func (s *Supervisor) isCircularMetaIssue(ctx context.Context, parentIssue *types.Issue, discoveredIssue DiscoveredIssue) (bool, error) {
	// Check if parent has meta-issue label
	parentLabels, err := s.store.GetLabels(ctx, parentIssue.ID)
	if err != nil {
		return false, fmt.Errorf("failed to get parent labels: %w", err)
	}

	hasParentMetaLabel := false
	for _, label := range parentLabels {
		if label == "meta-issue" {
			hasParentMetaLabel = true
			break
		}
	}

	// Check if discovered issue has meta-issue label (set by AI)
	hasDiscoveredMetaLabel := hasLabel(discoveredIssue.Labels, "meta-issue")

	// If both parent and child are meta-issues, it's circular
	if hasParentMetaLabel && hasDiscoveredMetaLabel {
		return true, nil
	}

	return false, nil
}

// CreateDiscoveredIssues creates issues from the AI analysis
// vc-4vot: Added validation to prevent infinite meta-issue recursion
func (s *Supervisor) CreateDiscoveredIssues(ctx context.Context, parentIssue *types.Issue, discovered []DiscoveredIssue) ([]string, error) {
	var createdIDs []string
	var skipped []string

	// vc-4vot: Circuit breaker - if more than 5 discovered blockers, something is wrong
	blockerCount := 0
	for _, disc := range discovered {
		if disc.DiscoveryType == "blocker" {
			blockerCount++
		}
	}

	if blockerCount > 5 {
		fmt.Fprintf(os.Stderr, "⚠️  WARNING: Excessive blocker discovery detected (%d blockers)\n", blockerCount)
		fmt.Fprintf(os.Stderr, "   This may indicate infinite recursion. Escalating to human review.\n")

		// Create a single escalation issue instead of creating all blockers
		escalationIssue := &types.Issue{
			Title:       fmt.Sprintf("Excessive blocker discovery in %s - needs human review", parentIssue.ID),
			Description: fmt.Sprintf("The AI analysis discovered %d blocking issues for %s, which suggests a systemic problem or infinite recursion.\n\nParent Issue: %s\nParent Title: %s\n\nPlease review the parent issue and address the root cause.\n\n_Discovered during execution of %s_", blockerCount, parentIssue.ID, parentIssue.ID, parentIssue.Title, parentIssue.ID),
			IssueType:   types.TypeTask,
			Status:      types.StatusOpen,
			Priority:    0, // P0 - critical
			Assignee:    "ai-supervisor",
			AcceptanceCriteria: "1. Review parent issue and discovered blockers list\n2. Identify root cause of excessive blocker discovery\n3. Resolve underlying issue or reconfigure AI analysis\n4. Ensure parent issue has clear acceptance criteria",
		}

		if err := s.store.CreateIssue(ctx, escalationIssue, "ai-supervisor"); err != nil {
			return nil, fmt.Errorf("failed to create escalation issue: %w", err)
		}

		if err := s.store.AddLabel(ctx, escalationIssue.ID, "escalated", "ai-supervisor"); err != nil {
			fmt.Fprintf(os.Stderr, "warning: failed to add escalated label: %v\n", err)
		}

		if err := s.store.AddComment(ctx, escalationIssue.ID, "ai-supervisor", fmt.Sprintf("Discovered blockers:\n%s", formatDiscoveredIssues(discovered))); err != nil {
			fmt.Fprintf(os.Stderr, "warning: failed to add comment with blocker details: %v\n", err)
		}

		fmt.Printf("✓ Created escalation issue %s instead of %d blockers\n", escalationIssue.ID, blockerCount)
		return []string{escalationIssue.ID}, nil
	}

	for _, disc := range discovered {
		// vc-4vot: Check for circular meta-issue pattern (uses AI-set labels)
		isCircular, err := s.isCircularMetaIssue(ctx, parentIssue, disc)
		if err != nil {
			fmt.Fprintf(os.Stderr, "warning: failed to check circular meta-issue: %v (skipping issue)\n", err)
			skipped = append(skipped, disc.Title)
			continue
		}

		if isCircular {
			fmt.Fprintf(os.Stderr, "⚠️  Skipping circular meta-issue: %s\n", disc.Title)
			fmt.Fprintf(os.Stderr, "   Parent %s is also a meta-issue - would create infinite recursion\n", parentIssue.ID)
			skipped = append(skipped, disc.Title)
			continue
		}

		// vc-4vot: Meta-issue validation - meta-issues MUST have acceptance criteria
		if hasLabel(disc.Labels, "meta-issue") {
			if disc.AcceptanceCriteria == "" {
				fmt.Fprintf(os.Stderr, "⚠️  Skipping meta-issue without acceptance criteria: %s\n", disc.Title)
				fmt.Fprintf(os.Stderr, "   Meta-issues must have criteria to avoid recursion\n")
				skipped = append(skipped, disc.Title)
				continue
			}
			fmt.Printf("ℹ️  Meta-issue detected with criteria: %s\n", disc.Title)
		}

		// vc-4vot: Check blocker depth limit - max 2 levels of discovered blockers
		if disc.DiscoveryType == "blocker" {
			depth, err := s.getBlockerDepth(ctx, parentIssue)
			if err != nil {
				fmt.Fprintf(os.Stderr, "warning: failed to check blocker depth: %v (allowing issue)\n", err)
				// Continue creating the issue - err on the side of progress
			} else if depth >= 2 {
				fmt.Fprintf(os.Stderr, "⚠️  Skipping blocker at depth %d: %s\n", depth+1, disc.Title)
				fmt.Fprintf(os.Stderr, "   Maximum blocker depth is 2 (current parent is at depth %d)\n", depth)
				skipped = append(skipped, disc.Title)
				continue
			}
		}

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
			Title:              disc.Title,
			Description:        disc.Description + fmt.Sprintf("\n\n_Discovered during execution of %s_", parentIssue.ID),
			IssueType:          issueType,
			Status:             types.StatusOpen,
			Priority:           priority, // Use calculated priority (vc-152)
			Assignee:           "ai-supervisor",
			AcceptanceCriteria: disc.AcceptanceCriteria, // vc-4vot: Include acceptance criteria from AI
		}

		if err := s.store.CreateIssue(ctx, newIssue, "ai-supervisor"); err != nil {
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

		// vc-4vot: Add AI-specified labels (e.g., "meta-issue")
		for _, label := range disc.Labels {
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

	// vc-4vot: Log skipped issues for debugging
	if len(skipped) > 0 {
		fmt.Printf("⚠️  Skipped %d issues due to recursion prevention:\n", len(skipped))
		for _, title := range skipped {
			fmt.Printf("  - %s\n", title)
		}
	}

	return createdIDs, nil
}

// formatDiscoveredIssues formats a list of discovered issues for display
func formatDiscoveredIssues(issues []DiscoveredIssue) string {
	var sb strings.Builder
	for i, issue := range issues {
		sb.WriteString(fmt.Sprintf("%d. [%s] %s\n", i+1, issue.DiscoveryType, issue.Title))
		if issue.Description != "" {
			// Show first 100 chars of description
			desc := issue.Description
			if len(desc) > 100 {
				desc = desc[:100] + "..."
			}
			sb.WriteString(fmt.Sprintf("   %s\n", desc))
		}
	}
	return sb.String()
}
