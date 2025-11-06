package ai

import (
	"context"
	"testing"

	"github.com/steveyegge/vc/internal/storage"
	"github.com/steveyegge/vc/internal/types"
)

// TestMetaIssueRecursionPrevention tests vc-4vot: preventing infinite meta-issue recursion
func TestMetaIssueRecursionPrevention(t *testing.T) {
	ctx := context.Background()

	// Create in-memory storage
	cfg := storage.DefaultConfig()
	cfg.Path = ":memory:"
	store, err := storage.NewStorage(ctx, cfg)
	if err != nil {
		t.Fatalf("failed to create storage: %v", err)
	}
	defer store.Close()

	// Create supervisor with storage
	supervisor := &Supervisor{
		store: store,
		model: "claude-sonnet-4-20250514", // Not used in this test
	}

	// Test Case 1: Circular meta-issue detection (vc-hpcl scenario)
	t.Run("circular_meta_issue_detection", func(t *testing.T) {
		// Create parent meta-issue (represents vc-9yhu: adds criteria to vc-hpcl)
		parentIssue := &types.Issue{
			Title:       "Add acceptance criteria to vc-hpcl",
			Description: "vc-hpcl needs acceptance criteria",
			IssueType:   types.TypeTask,
			Status:      types.StatusOpen,
			Priority:    1,
			AcceptanceCriteria: "1. Add criteria to vc-hpcl\n2. Ensure criteria are clear",
		}
		if err := store.CreateIssue(ctx, parentIssue, "test"); err != nil {
			t.Fatalf("failed to create parent issue: %v", err)
		}

		// Add meta-issue label to parent
		if err := store.AddLabel(ctx, parentIssue.ID, "meta-issue", "test"); err != nil {
			t.Fatalf("failed to add meta-issue label: %v", err)
		}

		// Try to create a child meta-issue (represents vc-qo2u: adds criteria to vc-9yhu)
		// This should be BLOCKED because parent is also a meta-issue
		discoveredIssues := []DiscoveredIssue{
			{
				Title:              "Add acceptance criteria to " + parentIssue.ID,
				Description:        "This meta-issue needs acceptance criteria",
				Type:               "task",
				Priority:           "P1",
				DiscoveryType:      "blocker",
				AcceptanceCriteria: "1. Add criteria\n2. Verify criteria",
				Labels:             []string{"meta-issue"}, // AI marks this as meta-issue
			},
		}

		createdIDs, err := supervisor.CreateDiscoveredIssues(ctx, parentIssue, discoveredIssues)
		if err != nil {
			t.Fatalf("CreateDiscoveredIssues failed: %v", err)
		}

		// Should be EMPTY - circular meta-issue should be skipped
		if len(createdIDs) != 0 {
			t.Errorf("expected 0 created issues (circular meta-issue should be blocked), got %d", len(createdIDs))
		}
	})

	// Test Case 2: Meta-issue without acceptance criteria should be rejected
	t.Run("meta_issue_without_criteria_rejected", func(t *testing.T) {
		// Create normal parent issue
		parentIssue := &types.Issue{
			Title:              "Implement feature X",
			Description:        "Add feature X to the system",
			IssueType:          types.TypeTask,
			Status:             types.StatusOpen,
			Priority:           2,
			AcceptanceCriteria: "1. Feature works\n2. Tests pass",
		}
		if err := store.CreateIssue(ctx, parentIssue, "test"); err != nil {
			t.Fatalf("failed to create parent issue: %v", err)
		}

		// Try to create meta-issue WITHOUT acceptance criteria - should be rejected
		discoveredIssues := []DiscoveredIssue{
			{
				Title:              "Add design to " + parentIssue.ID,
				Description:        "This issue needs a design",
				Type:               "task",
				Priority:           "P1",
				DiscoveryType:      "blocker",
				AcceptanceCriteria: "", // MISSING - should be rejected
				Labels:             []string{"meta-issue"},
			},
		}

		createdIDs, err := supervisor.CreateDiscoveredIssues(ctx, parentIssue, discoveredIssues)
		if err != nil {
			t.Fatalf("CreateDiscoveredIssues failed: %v", err)
		}

		// Should be EMPTY - meta-issue without criteria should be skipped
		if len(createdIDs) != 0 {
			t.Errorf("expected 0 created issues (meta-issue without criteria should be blocked), got %d", len(createdIDs))
		}
	})

	// Test Case 3: Valid meta-issue with criteria should be allowed
	t.Run("valid_meta_issue_with_criteria_allowed", func(t *testing.T) {
		// Create normal parent issue
		parentIssue := &types.Issue{
			Title:              "Implement feature Y",
			Description:        "Add feature Y to the system",
			IssueType:          types.TypeTask,
			Status:             types.StatusOpen,
			Priority:           2,
			AcceptanceCriteria: "1. Feature works\n2. Tests pass",
		}
		if err := store.CreateIssue(ctx, parentIssue, "test"); err != nil {
			t.Fatalf("failed to create parent issue: %v", err)
		}

		// Create meta-issue WITH acceptance criteria - should be allowed
		discoveredIssues := []DiscoveredIssue{
			{
				Title:              "Add design to " + parentIssue.ID,
				Description:        "This issue needs a design document",
				Type:               "task",
				Priority:           "P1",
				DiscoveryType:      "blocker",
				AcceptanceCriteria: "1. Create design doc\n2. Review with team\n3. Add to issue", // PRESENT - should be allowed
				Labels:             []string{"meta-issue"},
			},
		}

		createdIDs, err := supervisor.CreateDiscoveredIssues(ctx, parentIssue, discoveredIssues)
		if err != nil {
			t.Fatalf("CreateDiscoveredIssues failed: %v", err)
		}

		// Should have ONE issue created
		if len(createdIDs) != 1 {
			t.Errorf("expected 1 created issue, got %d", len(createdIDs))
		}

		// Verify the issue has the meta-issue label
		if len(createdIDs) > 0 {
			labels, err := store.GetLabels(ctx, createdIDs[0])
			if err != nil {
				t.Fatalf("failed to get labels: %v", err)
			}

			hasMetaLabel := false
			for _, label := range labels {
				if label == "meta-issue" {
					hasMetaLabel = true
					break
				}
			}

			if !hasMetaLabel {
				t.Errorf("created issue should have meta-issue label")
			}

			// Verify acceptance criteria was set
			issue, err := store.GetIssue(ctx, createdIDs[0])
			if err != nil {
				t.Fatalf("failed to get issue: %v", err)
			}
			if issue.AcceptanceCriteria == "" {
				t.Errorf("meta-issue should have acceptance criteria")
			}
		}
	})

	// Test Case 4: Blocker depth limit (max 2 levels)
	t.Run("blocker_depth_limit", func(t *testing.T) {
		// Create chain: root -> blocker1 -> blocker2
		// blocker3 should be rejected (depth 3)

		// Root task
		rootIssue := &types.Issue{
			Title:              "Root task",
			Description:        "The original task",
			IssueType:          types.TypeTask,
			Status:             types.StatusOpen,
			Priority:           2,
			AcceptanceCriteria: "1. Complete the task",
		}
		if err := store.CreateIssue(ctx, rootIssue, "test"); err != nil {
			t.Fatalf("failed to create root issue: %v", err)
		}

		// Blocker level 1
		blocker1 := &types.Issue{
			Title:              "Blocker 1",
			Description:        "First blocker",
			IssueType:          types.TypeBug,
			Status:             types.StatusOpen,
			Priority:           1,
			AcceptanceCriteria: "1. Fix blocker",
		}
		if err := store.CreateIssue(ctx, blocker1, "test"); err != nil {
			t.Fatalf("failed to create blocker1: %v", err)
		}
		if err := store.AddLabel(ctx, blocker1.ID, "discovered:blocker", "test"); err != nil {
			t.Fatalf("failed to add blocker label: %v", err)
		}
		dep1 := &types.Dependency{
			IssueID:     blocker1.ID,
			DependsOnID: rootIssue.ID,
			Type:        types.DepDiscoveredFrom,
		}
		if err := store.AddDependency(ctx, dep1, "test"); err != nil {
			t.Fatalf("failed to add dependency: %v", err)
		}

		// Blocker level 2
		blocker2 := &types.Issue{
			Title:              "Blocker 2",
			Description:        "Second level blocker",
			IssueType:          types.TypeBug,
			Status:             types.StatusOpen,
			Priority:           1,
			AcceptanceCriteria: "1. Fix this too",
		}
		if err := store.CreateIssue(ctx, blocker2, "test"); err != nil {
			t.Fatalf("failed to create blocker2: %v", err)
		}
		if err := store.AddLabel(ctx, blocker2.ID, "discovered:blocker", "test"); err != nil {
			t.Fatalf("failed to add blocker label: %v", err)
		}
		dep2 := &types.Dependency{
			IssueID:     blocker2.ID,
			DependsOnID: blocker1.ID,
			Type:        types.DepDiscoveredFrom,
		}
		if err := store.AddDependency(ctx, dep2, "test"); err != nil {
			t.Fatalf("failed to add dependency: %v", err)
		}

		// Try to create blocker level 3 - should be rejected
		discoveredIssues := []DiscoveredIssue{
			{
				Title:              "Blocker 3",
				Description:        "Third level blocker - should be rejected",
				Type:               "bug",
				Priority:           "P1",
				DiscoveryType:      "blocker", // This is a blocker
				AcceptanceCriteria: "1. Fix this",
			},
		}

		createdIDs, err := supervisor.CreateDiscoveredIssues(ctx, blocker2, discoveredIssues)
		if err != nil {
			t.Fatalf("CreateDiscoveredIssues failed: %v", err)
		}

		// Should be EMPTY - depth 3 blocker should be skipped
		if len(createdIDs) != 0 {
			t.Errorf("expected 0 created issues (depth 3 blocker should be blocked), got %d", len(createdIDs))
		}
	})

	// Test Case 5: Circuit breaker for excessive blockers
	t.Run("circuit_breaker_excessive_blockers", func(t *testing.T) {
		parentIssue := &types.Issue{
			Title:              "Task with many blockers",
			Description:        "This will discover too many blockers",
			IssueType:          types.TypeTask,
			Status:             types.StatusOpen,
			Priority:           2,
			AcceptanceCriteria: "1. Complete task",
		}
		if err := store.CreateIssue(ctx, parentIssue, "test"); err != nil {
			t.Fatalf("failed to create parent issue: %v", err)
		}

		// Create 10 blocker issues (circuit breaker triggers at >5)
		discoveredIssues := []DiscoveredIssue{}
		for i := 1; i <= 10; i++ {
			discoveredIssues = append(discoveredIssues, DiscoveredIssue{
				Title:              "Blocker " + string(rune('A'+i-1)),
				Description:        "Blocker description",
				Type:               "bug",
				Priority:           "P1",
				DiscoveryType:      "blocker",
				AcceptanceCriteria: "1. Fix it",
			})
		}

		createdIDs, err := supervisor.CreateDiscoveredIssues(ctx, parentIssue, discoveredIssues)
		if err != nil {
			t.Fatalf("CreateDiscoveredIssues failed: %v", err)
		}

		// Should create exactly 1 escalation issue (not 10 individual blockers)
		if len(createdIDs) != 1 {
			t.Errorf("expected 1 escalation issue (circuit breaker), got %d", len(createdIDs))
		}

		// Verify the escalation issue has the "escalated" label
		if len(createdIDs) > 0 {
			labels, err := store.GetLabels(ctx, createdIDs[0])
			if err != nil {
				t.Fatalf("failed to get labels: %v", err)
			}

			hasEscalatedLabel := false
			for _, label := range labels {
				if label == "escalated" {
					hasEscalatedLabel = true
					break
				}
			}

			if !hasEscalatedLabel {
				t.Errorf("escalation issue should have 'escalated' label")
			}
		}
	})
}
