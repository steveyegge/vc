package ai

import (
	"context"
	"strings"
	"testing"

	"github.com/steveyegge/vc/internal/storage"
	"github.com/steveyegge/vc/internal/types"
)

// TestMetaIssueWorkflowIntegration tests the complete workflow of detecting
// and creating meta-issues for missing acceptance criteria.
//
// This test validates vc-bze5: the automated detection logic that identifies
// when an issue lacks acceptance criteria and creates a task to add them,
// while ensuring the meta-issue itself has criteria to prevent infinite recursion.
func TestMetaIssueWorkflowIntegration(t *testing.T) {
	ctx := context.Background()

	// Create in-memory storage
	cfg := storage.DefaultConfig()
	cfg.Path = ":memory:"
	store, err := storage.NewStorage(ctx, cfg)
	if err != nil {
		t.Fatalf("failed to create storage: %v", err)
	}
	defer store.Close()

	// Create supervisor with storage (no real API calls in this test)
	supervisor := &Supervisor{
		store: store,
		model: "claude-sonnet-4-20250514",
	}

	t.Run("detect_missing_acceptance_criteria_and_create_meta_issue", func(t *testing.T) {
		// Step 1: Create an issue WITHOUT acceptance criteria (simulating an epic or poorly-defined task)
		// Note: In vc-e3j2, task/bug/feature issues MUST have acceptance criteria, so this would fail CreateIssue.
		// For this test, we'll use an epic (which doesn't require criteria) or bypass the validation.
		parentIssue := &types.Issue{
			Title:              "Implement new feature XYZ",
			Description:        "Add support for feature XYZ in the system",
			IssueType:          types.TypeEpic, // Epics don't require acceptance criteria
			Status:             types.StatusOpen,
			Priority:           1,
			AcceptanceCriteria: "", // NO CRITERIA - this should trigger meta-issue detection
		}
		if err := store.CreateIssue(ctx, parentIssue, "test"); err != nil {
			t.Fatalf("failed to create parent issue: %v", err)
		}

		// Step 2: Simulate AI analysis discovering that this issue lacks acceptance criteria
		// In a real workflow, this would come from AnalyzeExecutionResult() or assessment
		// The AI would detect: "This issue has no clear completion criteria"
		discoveredIssues := []DiscoveredIssue{
			{
				Title:       "Add acceptance criteria to " + parentIssue.ID,
				Description: "Issue " + parentIssue.ID + " lacks specific acceptance criteria. Without clear criteria, it's impossible to determine when the work is complete.",
				Type:        "task",
				Priority:    "P1", // AI suggests P1, but will be calculated based on discovery_type
				DiscoveryType: "blocker", // This blocks the parent from being properly executed
				AcceptanceCriteria: "1. Add specific, measurable acceptance criteria to " + parentIssue.ID + "\n2. Ensure each criterion is testable\n3. Verify criteria cover all aspects of the feature",
				Labels: []string{"meta-issue"}, // AI marks this as a meta-issue
			},
		}

		// Step 3: Create the discovered meta-issue
		createdIDs, err := supervisor.CreateDiscoveredIssues(ctx, parentIssue, discoveredIssues)
		if err != nil {
			t.Fatalf("CreateDiscoveredIssues failed: %v", err)
		}

		// Step 4: Verify the meta-issue was created
		if len(createdIDs) != 1 {
			t.Fatalf("expected 1 meta-issue to be created, got %d", len(createdIDs))
		}

		metaIssueID := createdIDs[0]

		// Step 5: Verify the meta-issue has all required properties
		metaIssue, err := store.GetIssue(ctx, metaIssueID)
		if err != nil {
			t.Fatalf("failed to get meta-issue: %v", err)
		}

		// Verify title references parent
		if !strings.Contains(metaIssue.Title, parentIssue.ID) {
			t.Errorf("meta-issue title should reference parent issue ID, got: %s", metaIssue.Title)
		}

		// Verify description explains the problem
		if !strings.Contains(metaIssue.Description, "acceptance criteria") {
			t.Errorf("meta-issue description should mention acceptance criteria, got: %s", metaIssue.Description)
		}

		// CRITICAL: Verify meta-issue has acceptance criteria (prevents infinite recursion)
		if metaIssue.AcceptanceCriteria == "" {
			t.Error("meta-issue MUST have acceptance criteria to prevent infinite recursion")
		}
		if !strings.Contains(metaIssue.AcceptanceCriteria, parentIssue.ID) {
			t.Errorf("meta-issue acceptance criteria should reference parent issue, got: %s", metaIssue.AcceptanceCriteria)
		}

		// Verify meta-issue label was applied
		labels, err := store.GetLabels(ctx, metaIssueID)
		if err != nil {
			t.Fatalf("failed to get labels: %v", err)
		}
		hasMetaLabel := false
		hasBlockerLabel := false
		for _, label := range labels {
			if label == "meta-issue" {
				hasMetaLabel = true
			}
			if label == "discovered:blocker" {
				hasBlockerLabel = true
			}
		}
		if !hasMetaLabel {
			t.Error("meta-issue should have 'meta-issue' label")
		}
		if !hasBlockerLabel {
			t.Error("meta-issue should have 'discovered:blocker' label (it blocks parent work)")
		}

		// Step 6: Verify dependency linking meta-issue to parent
		deps, err := store.GetDependencyRecords(ctx, metaIssueID)
		if err != nil {
			t.Fatalf("failed to get dependencies: %v", err)
		}

		foundDiscoveredFrom := false
		for _, dep := range deps {
			if dep.IssueID == metaIssueID && dep.DependsOnID == parentIssue.ID && dep.Type == types.DepDiscoveredFrom {
				foundDiscoveredFrom = true
				break
			}
		}
		if !foundDiscoveredFrom {
			t.Error("meta-issue should have discovered_from dependency to parent")
		}

		// Step 7: Verify priority calculation (blocker from P1 parent = P0)
		if metaIssue.Priority != 0 {
			t.Errorf("meta-issue priority should be P0 (blocker from P1 parent), got P%d", metaIssue.Priority)
		}
	})

	t.Run("prevent_meta_issue_about_meta_issue", func(t *testing.T) {
		// Step 1: Create a meta-issue (represents "Add criteria to issue X")
		firstMetaIssue := &types.Issue{
			Title:              "Add acceptance criteria to vc-xyz",
			Description:        "Issue vc-xyz needs acceptance criteria",
			IssueType:          types.TypeTask,
			Status:             types.StatusOpen,
			Priority:           1,
			AcceptanceCriteria: "1. Add criteria to vc-xyz\n2. Ensure criteria are clear",
		}
		if err := store.CreateIssue(ctx, firstMetaIssue, "test"); err != nil {
			t.Fatalf("failed to create first meta-issue: %v", err)
		}

		// Add meta-issue label
		if err := store.AddLabel(ctx, firstMetaIssue.ID, "meta-issue", "test"); err != nil {
			t.Fatalf("failed to add meta-issue label: %v", err)
		}

		// Step 2: Simulate AI analysis discovering that the meta-issue itself needs better criteria
		// This should be BLOCKED to prevent infinite recursion
		discoveredIssues := []DiscoveredIssue{
			{
				Title:              "Add better acceptance criteria to " + firstMetaIssue.ID,
				Description:        "Meta-issue " + firstMetaIssue.ID + " needs more specific criteria",
				Type:               "task",
				Priority:           "P1",
				DiscoveryType:      "blocker",
				AcceptanceCriteria: "1. Improve criteria for " + firstMetaIssue.ID,
				Labels:             []string{"meta-issue"}, // This is ALSO a meta-issue
			},
		}

		// Step 3: Try to create the meta-meta-issue (should be blocked)
		createdIDs, err := supervisor.CreateDiscoveredIssues(ctx, firstMetaIssue, discoveredIssues)
		if err != nil {
			t.Fatalf("CreateDiscoveredIssues failed: %v", err)
		}

		// Step 4: Verify NO issue was created (circular meta-issue prevented)
		if len(createdIDs) != 0 {
			t.Errorf("expected 0 issues created (circular meta-issue prevention), got %d", len(createdIDs))
		}
	})

	t.Run("state_verification_prevents_stale_meta_issues", func(t *testing.T) {
		// Step 1: Create an issue that starts WITHOUT criteria but will be updated
		issue := &types.Issue{
			Title:              "Implement feature ABC",
			Description:        "Add feature ABC",
			IssueType:          types.TypeEpic, // Epic doesn't require criteria initially
			Status:             types.StatusOpen,
			Priority:           2,
			AcceptanceCriteria: "", // Empty initially
		}
		if err := store.CreateIssue(ctx, issue, "test"); err != nil {
			t.Fatalf("failed to create issue: %v", err)
		}

		// Step 2: Someone updates the issue with criteria BEFORE the meta-issue is created
		// (Simulates a race condition where analysis started before update, but creation happens after)
		updates := map[string]interface{}{
			"acceptance_criteria": "1. Feature works\n2. Tests pass\n3. Docs updated",
		}
		if err := store.UpdateIssue(ctx, issue.ID, updates, "test"); err != nil {
			t.Fatalf("failed to update issue: %v", err)
		}

		// Step 3: AI tries to create meta-issue based on stale observation
		discoveredIssues := []DiscoveredIssue{
			{
				Title:              "Add acceptance criteria to " + issue.ID,
				Description:        "Issue lacks acceptance criteria (stale observation)",
				Type:               "task",
				Priority:           "P1",
				DiscoveryType:      "blocker",
				AcceptanceCriteria: "1. Add criteria to " + issue.ID,
				Labels:             []string{"meta-issue"},
			},
		}

		// Step 4: Try to create the meta-issue (should be blocked by state verification)
		createdIDs, err := supervisor.CreateDiscoveredIssues(ctx, issue, discoveredIssues)
		if err != nil {
			t.Fatalf("CreateDiscoveredIssues failed: %v", err)
		}

		// Step 5: Verify NO issue was created (state verification caught the stale observation)
		if len(createdIDs) != 0 {
			t.Errorf("expected 0 issues created (state verification should detect criteria now exists), got %d", len(createdIDs))
		}
	})

	t.Run("meta_issue_for_missing_description", func(t *testing.T) {
		// Test the workflow for other meta-issue types (missing description, missing design, etc.)
		// This ensures state verification works for different missing field types

		// Step 1: Create issue with minimal description
		issue := &types.Issue{
			Title:              "Fix bug in parser",
			Description:        "", // Empty description
			IssueType:          types.TypeBug,
			Status:             types.StatusOpen,
			Priority:           1,
			AcceptanceCriteria: "Bug is fixed", // Has criteria, but no description
		}
		if err := store.CreateIssue(ctx, issue, "test"); err != nil {
			t.Fatalf("failed to create issue: %v", err)
		}

		// Step 2: AI detects missing description
		discoveredIssues := []DiscoveredIssue{
			{
				Title:              "Add description to " + issue.ID,
				Description:        "Issue " + issue.ID + " has no description explaining what needs to be done",
				Type:               "task",
				Priority:           "P1",
				DiscoveryType:      "blocker",
				AcceptanceCriteria: "1. Add detailed description to " + issue.ID + "\n2. Explain the bug behavior\n3. Describe expected behavior",
				Labels:             []string{"meta-issue"},
			},
		}

		// Step 3: Create the meta-issue
		createdIDs, err := supervisor.CreateDiscoveredIssues(ctx, issue, discoveredIssues)
		if err != nil {
			t.Fatalf("CreateDiscoveredIssues failed: %v", err)
		}

		// Step 4: Verify meta-issue was created
		if len(createdIDs) != 1 {
			t.Fatalf("expected 1 meta-issue for missing description, got %d", len(createdIDs))
		}

		// Verify it has all the required properties
		metaIssue, err := store.GetIssue(ctx, createdIDs[0])
		if err != nil {
			t.Fatalf("failed to get meta-issue: %v", err)
		}

		if metaIssue.AcceptanceCriteria == "" {
			t.Error("meta-issue for missing description must have acceptance criteria")
		}

		// Step 5: Now update parent with description and try again (state verification test)
		updates := map[string]interface{}{
			"description": "Parser fails when encountering nested structures. Need to fix the recursive descent logic.",
		}
		if err := store.UpdateIssue(ctx, issue.ID, updates, "test"); err != nil {
			t.Fatalf("failed to update issue: %v", err)
		}

		// Try to create another meta-issue (should be blocked)
		createdIDs2, err := supervisor.CreateDiscoveredIssues(ctx, issue, discoveredIssues)
		if err != nil {
			t.Fatalf("CreateDiscoveredIssues failed: %v", err)
		}

		if len(createdIDs2) != 0 {
			t.Errorf("expected 0 issues (parent now has description), got %d", len(createdIDs2))
		}
	})

	t.Run("normal_discovered_issues_still_work", func(t *testing.T) {
		// Ensure non-meta-issue discovered issues work normally
		parentIssue := &types.Issue{
			Title:              "Refactor module Y",
			Description:        "Clean up module Y code",
			IssueType:          types.TypeTask,
			Status:             types.StatusOpen,
			Priority:           2,
			AcceptanceCriteria: "Code is refactored and tests pass",
		}
		if err := store.CreateIssue(ctx, parentIssue, "test"); err != nil {
			t.Fatalf("failed to create parent issue: %v", err)
		}

		// Discover normal issues (not meta-issues)
		discoveredIssues := []DiscoveredIssue{
			{
				Title:              "Add unit tests for module Y",
				Description:        "Module Y lacks test coverage",
				Type:               "task",
				Priority:           "P2",
				DiscoveryType:      "related", // Related work, not blocker
				AcceptanceCriteria: "Unit tests cover core functionality",
				// NO meta-issue label
			},
			{
				Title:         "Fix typo in module Y comments",
				Description:   "Found a typo in the header comment",
				Type:          "chore",
				Priority:      "P3",
				DiscoveryType: "background",
				// No acceptance criteria for chore - should get default
			},
		}

		createdIDs, err := supervisor.CreateDiscoveredIssues(ctx, parentIssue, discoveredIssues)
		if err != nil {
			t.Fatalf("CreateDiscoveredIssues failed: %v", err)
		}

		// Both issues should be created
		if len(createdIDs) != 2 {
			t.Fatalf("expected 2 normal discovered issues, got %d", len(createdIDs))
		}

		// Verify first issue (related task)
		issue1, err := store.GetIssue(ctx, createdIDs[0])
		if err != nil {
			t.Fatalf("failed to get issue 1: %v", err)
		}
		if issue1.AcceptanceCriteria == "" {
			t.Error("task issue should have acceptance criteria")
		}

		labels1, _ := store.GetLabels(ctx, createdIDs[0])
		hasMetaLabel := false
		for _, label := range labels1 {
			if label == "meta-issue" {
				hasMetaLabel = true
			}
		}
		if hasMetaLabel {
			t.Error("normal discovered issue should NOT have meta-issue label")
		}

		// Verify second issue (background chore)
		issue2, err := store.GetIssue(ctx, createdIDs[1])
		if err != nil {
			t.Fatalf("failed to get issue 2: %v", err)
		}
		// Chores do NOT require acceptance criteria (vc-e3j2 policy)
		// The AI didn't provide criteria, and that's fine for chores
		// Just verify the issue was created successfully
		if issue2.IssueType != types.TypeChore {
			t.Errorf("expected TypeChore, got %s", issue2.IssueType)
		}
	})
}
