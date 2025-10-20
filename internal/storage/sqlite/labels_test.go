package sqlite

import (
	"context"
	"testing"

	"github.com/steveyegge/vc/internal/types"
)

// TestAddLabelSkipsEventWhenLabelExists tests that AddLabel doesn't record
// an event when the label already exists (vc-27)
func TestAddLabelSkipsEventWhenLabelExists(t *testing.T) {
	store := setupTestDB(t)
	defer func() { _ = store.Close() }()

	ctx := context.Background()

	// Create a test issue
	issue := &types.Issue{
		Title:       "Test issue for labels",
		Description: "Testing label events",
		IssueType:   types.TypeTask,
		Status:      types.StatusOpen,
		Priority:    1,
	}
	err := store.CreateIssue(ctx, issue, "test-actor")
	if err != nil {
		t.Fatalf("Failed to create issue: %v", err)
	}

	// Add label for the first time - should record event
	err = store.AddLabel(ctx, issue.ID, "test-label", "test-actor")
	if err != nil {
		t.Fatalf("Failed to add label: %v", err)
	}

	// Check events - should have 2 events (created + label_added)
	events, err := store.GetEvents(ctx, issue.ID, 100)
	if err != nil {
		t.Fatalf("Failed to get events: %v", err)
	}
	if len(events) != 2 {
		t.Errorf("Expected 2 events after first add, got %d", len(events))
	}
	// Verify the second event is label_added
	if events[1].EventType != types.EventLabelAdded {
		t.Errorf("Expected EventLabelAdded, got %s", events[1].EventType)
	}

	// Add the same label again - should NOT record event
	err = store.AddLabel(ctx, issue.ID, "test-label", "test-actor")
	if err != nil {
		t.Fatalf("Failed to add duplicate label: %v", err)
	}

	// Check events - should still have only 2 events
	events, err = store.GetEvents(ctx, issue.ID, 100)
	if err != nil {
		t.Fatalf("Failed to get events: %v", err)
	}
	if len(events) != 2 {
		t.Errorf("Expected 2 events after duplicate add (no new event), got %d", len(events))
		for i, e := range events {
			comment := ""
			if e.Comment != nil {
				comment = *e.Comment
			}
			t.Logf("Event %d: %s - %s", i, e.EventType, comment)
		}
	}

	// Verify labels - should have exactly one instance
	labels, err := store.GetLabels(ctx, issue.ID)
	if err != nil {
		t.Fatalf("Failed to get labels: %v", err)
	}
	if len(labels) != 1 {
		t.Errorf("Expected 1 label, got %d", len(labels))
	}
	if labels[0] != "test-label" {
		t.Errorf("Expected 'test-label', got %s", labels[0])
	}
}

// TestRemoveLabelSkipsEventWhenLabelDoesntExist tests that RemoveLabel doesn't
// record an event when the label doesn't exist (vc-27)
func TestRemoveLabelSkipsEventWhenLabelDoesntExist(t *testing.T) {
	store := setupTestDB(t)
	defer func() { _ = store.Close() }()

	ctx := context.Background()

	// Create a test issue
	issue := &types.Issue{
		Title:       "Test issue for label removal",
		Description: "Testing label removal events",
		IssueType:   types.TypeTask,
		Status:      types.StatusOpen,
		Priority:    1,
	}
	err := store.CreateIssue(ctx, issue, "test-actor")
	if err != nil {
		t.Fatalf("Failed to create issue: %v", err)
	}

	// Try to remove a label that doesn't exist - should NOT record event
	err = store.RemoveLabel(ctx, issue.ID, "nonexistent-label", "test-actor")
	if err != nil {
		t.Fatalf("Failed to remove nonexistent label: %v", err)
	}

	// Check events - should have only 1 event (created)
	events, err := store.GetEvents(ctx, issue.ID, 100)
	if err != nil {
		t.Fatalf("Failed to get events: %v", err)
	}
	if len(events) != 1 {
		t.Errorf("Expected 1 event after removing nonexistent label, got %d", len(events))
		for i, e := range events {
			comment := ""
			if e.Comment != nil {
				comment = *e.Comment
			}
			t.Logf("Event %d: %s - %s", i, e.EventType, comment)
		}
	}

	// Now add a label and remove it - should record both events
	err = store.AddLabel(ctx, issue.ID, "temp-label", "test-actor")
	if err != nil {
		t.Fatalf("Failed to add label: %v", err)
	}

	err = store.RemoveLabel(ctx, issue.ID, "temp-label", "test-actor")
	if err != nil {
		t.Fatalf("Failed to remove label: %v", err)
	}

	// Check events - should have 3 events (created + label_added + label_removed)
	events, err = store.GetEvents(ctx, issue.ID, 100)
	if err != nil {
		t.Fatalf("Failed to get events: %v", err)
	}
	if len(events) != 3 {
		t.Errorf("Expected 3 events after add+remove, got %d", len(events))
		for i, e := range events {
			comment := ""
			if e.Comment != nil {
				comment = *e.Comment
			}
			t.Logf("Event %d: %s - %s", i, e.EventType, comment)
		}
	}

	// Verify the events are in correct order
	if events[1].EventType != types.EventLabelAdded {
		t.Errorf("Expected second event to be EventLabelAdded, got %s", events[1].EventType)
	}
	if events[2].EventType != types.EventLabelRemoved {
		t.Errorf("Expected third event to be EventLabelRemoved, got %s", events[2].EventType)
	}

	// Verify no labels remain
	labels, err := store.GetLabels(ctx, issue.ID)
	if err != nil {
		t.Fatalf("Failed to get labels: %v", err)
	}
	if len(labels) != 0 {
		t.Errorf("Expected 0 labels after removal, got %d", len(labels))
	}
}

// TestLabelOperationsAuditTrail tests the complete audit trail for label operations
func TestLabelOperationsAuditTrail(t *testing.T) {
	store := setupTestDB(t)
	defer func() { _ = store.Close() }()

	ctx := context.Background()

	// Create a test issue
	issue := &types.Issue{
		Title:       "Test issue for audit trail",
		Description: "Testing complete label audit trail",
		IssueType:   types.TypeTask,
		Status:      types.StatusOpen,
		Priority:    1,
	}
	err := store.CreateIssue(ctx, issue, "test-actor")
	if err != nil {
		t.Fatalf("Failed to create issue: %v", err)
	}

	// Sequence of operations:
	// 1. Add label-1 (should record)
	// 2. Add label-1 again (should NOT record)
	// 3. Add label-2 (should record)
	// 4. Remove label-1 (should record)
	// 5. Remove label-1 again (should NOT record)
	// 6. Remove nonexistent label-3 (should NOT record)

	operations := []struct {
		action      string
		label       string
		shouldEvent bool
	}{
		{"add", "label-1", true},
		{"add", "label-1", false}, // duplicate
		{"add", "label-2", true},
		{"remove", "label-1", true},
		{"remove", "label-1", false}, // already removed
		{"remove", "label-3", false}, // never existed
	}

	expectedEvents := 1 // Start with created event

	for i, op := range operations {
		if op.action == "add" {
			err = store.AddLabel(ctx, issue.ID, op.label, "test-actor")
		} else {
			err = store.RemoveLabel(ctx, issue.ID, op.label, "test-actor")
		}
		if err != nil {
			t.Fatalf("Operation %d failed: %v", i, err)
		}

		if op.shouldEvent {
			expectedEvents++
		}

		// Verify event count after each operation
		events, err := store.GetEvents(ctx, issue.ID, 100)
		if err != nil {
			t.Fatalf("Failed to get events after operation %d: %v", i, err)
		}
		if len(events) != expectedEvents {
			t.Errorf("After operation %d (%s %s), expected %d events, got %d",
				i, op.action, op.label, expectedEvents, len(events))
			for j, e := range events {
				comment := ""
				if e.Comment != nil {
					comment = *e.Comment
				}
				t.Logf("  Event %d: %s - %s", j, e.EventType, comment)
			}
		}
	}

	// Final state: only label-2 should remain
	labels, err := store.GetLabels(ctx, issue.ID)
	if err != nil {
		t.Fatalf("Failed to get final labels: %v", err)
	}
	if len(labels) != 1 {
		t.Errorf("Expected 1 label in final state, got %d", len(labels))
	}
	if len(labels) > 0 && labels[0] != "label-2" {
		t.Errorf("Expected 'label-2', got %s", labels[0])
	}
}

// TestSearchIssuesWithLabels tests that SearchIssues correctly filters by labels (vc-118)
// This test verifies the fix for the issue_labels → labels table name mismatch
func TestSearchIssuesWithLabels(t *testing.T) {
	store := setupTestDB(t)
	defer func() { _ = store.Close() }()

	ctx := context.Background()

	// Create test issues
	issue1 := &types.Issue{
		Title:       "Issue with label A",
		Description: "Has label A",
		IssueType:   types.TypeTask,
		Status:      types.StatusOpen,
		Priority:    1,
	}
	err := store.CreateIssue(ctx, issue1, "test-actor")
	if err != nil {
		t.Fatalf("Failed to create issue1: %v", err)
	}

	issue2 := &types.Issue{
		Title:       "Issue with label B",
		Description: "Has label B",
		IssueType:   types.TypeTask,
		Status:      types.StatusOpen,
		Priority:    1,
	}
	err = store.CreateIssue(ctx, issue2, "test-actor")
	if err != nil {
		t.Fatalf("Failed to create issue2: %v", err)
	}

	issue3 := &types.Issue{
		Title:       "Issue with both labels",
		Description: "Has labels A and B",
		IssueType:   types.TypeTask,
		Status:      types.StatusOpen,
		Priority:    1,
	}
	err = store.CreateIssue(ctx, issue3, "test-actor")
	if err != nil {
		t.Fatalf("Failed to create issue3: %v", err)
	}

	// Add labels
	err = store.AddLabel(ctx, issue1.ID, "label-a", "test-actor")
	if err != nil {
		t.Fatalf("Failed to add label-a to issue1: %v", err)
	}

	err = store.AddLabel(ctx, issue2.ID, "label-b", "test-actor")
	if err != nil {
		t.Fatalf("Failed to add label-b to issue2: %v", err)
	}

	err = store.AddLabel(ctx, issue3.ID, "label-a", "test-actor")
	if err != nil {
		t.Fatalf("Failed to add label-a to issue3: %v", err)
	}

	err = store.AddLabel(ctx, issue3.ID, "label-b", "test-actor")
	if err != nil {
		t.Fatalf("Failed to add label-b to issue3: %v", err)
	}

	// Test 1: Search for issues with label-a
	openStatus := types.StatusOpen
	filter := types.IssueFilter{
		Status: &openStatus,
		Labels: []string{"label-a"},
	}

	results, err := store.SearchIssues(ctx, "", filter)
	if err != nil {
		t.Fatalf("SearchIssues with label-a failed: %v", err)
	}

	if len(results) != 2 {
		t.Errorf("Expected 2 issues with label-a, got %d", len(results))
	}

	// Verify we got issue1 and issue3
	foundIssue1 := false
	foundIssue3 := false
	for _, issue := range results {
		if issue.ID == issue1.ID {
			foundIssue1 = true
		}
		if issue.ID == issue3.ID {
			foundIssue3 = true
		}
	}
	if !foundIssue1 || !foundIssue3 {
		t.Errorf("Expected to find issue1 and issue3, found1=%v found3=%v", foundIssue1, foundIssue3)
	}

	// Test 2: Search for issues with both label-a AND label-b (should only return issue3)
	filter.Labels = []string{"label-a", "label-b"}

	results, err = store.SearchIssues(ctx, "", filter)
	if err != nil {
		t.Fatalf("SearchIssues with label-a AND label-b failed: %v", err)
	}

	if len(results) != 1 {
		t.Errorf("Expected 1 issue with both labels, got %d", len(results))
	}

	if len(results) > 0 && results[0].ID != issue3.ID {
		t.Errorf("Expected to find issue3, got %s", results[0].ID)
	}

	// Test 3: Search for issues with nonexistent label
	filter.Labels = []string{"nonexistent-label"}

	results, err = store.SearchIssues(ctx, "", filter)
	if err != nil {
		t.Fatalf("SearchIssues with nonexistent label failed: %v", err)
	}

	if len(results) != 0 {
		t.Errorf("Expected 0 issues with nonexistent label, got %d", len(results))
	}
}
