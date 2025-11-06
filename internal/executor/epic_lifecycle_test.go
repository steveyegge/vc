package executor

import (
	"testing"

	"github.com/steveyegge/vc/internal/events"
	"github.com/steveyegge/vc/internal/types"
)

// TestEpicLifecycleEvents_RegularEpic tests that epic_completed event is emitted for regular epics (vc-278).
// This is an integration test verifying the full flow from checkAndCloseEpicIfComplete through the event system.
func TestEpicLifecycleEvents_RegularEpic(t *testing.T) {
	ctx, store, exec := setupExecutorTest(t)
	defer store.Close()

	// Create a regular epic (not a mission)
	epic := &types.Issue{
		Title:        "Regular epic",
		Status:       types.StatusOpen,
		Priority:     1,
		IssueType:    types.TypeEpic,
		IssueSubtype: "", // Not a mission
	}
	if err := store.CreateIssue(ctx, epic, "test"); err != nil {
		t.Fatalf("Failed to create epic: %v", err)
	}

	// Create a child task
	task := &types.Issue{
		Title:     "Child task",
		Status:    types.StatusOpen,
		Priority:  1,
		IssueType: types.TypeTask,
		AcceptanceCriteria: "Test completes successfully",
	}
	if err := store.CreateIssue(ctx, task, "test"); err != nil {
		t.Fatalf("Failed to create task: %v", err)
	}

	// Link task to epic
	dep := &types.Dependency{
		IssueID:     task.ID,
		DependsOnID: epic.ID,
		Type:        types.DepParentChild,
	}
	if err := store.AddDependency(ctx, dep, "test"); err != nil {
		t.Fatalf("Failed to add dependency: %v", err)
	}

	// Close the task
	if err := store.CloseIssue(ctx, task.ID, "completed", "test"); err != nil {
		t.Fatalf("Failed to close task: %v", err)
	}

	// Call checkAndCloseEpicIfComplete directly
	closed, err := checkAndCloseEpicIfComplete(ctx, store, exec.supervisor, exec.instanceID, epic.ID)
	if err != nil {
		t.Fatalf("checkAndCloseEpicIfComplete failed: %v", err)
	}

	if !closed {
		t.Error("Expected epic to be closed, but it was not")
	}

	// Query stored events to verify epic_completed event was created
	allEvents, err := store.GetAgentEventsByIssue(ctx, epic.ID)
	if err != nil {
		t.Fatalf("Failed to get events: %v", err)
	}

	// Find epic_completed event
	var epicCompletedEvent *events.AgentEvent
	for _, evt := range allEvents {
		if evt.Type == events.EventTypeEpicCompleted {
			epicCompletedEvent = evt
			break
		}
	}

	if epicCompletedEvent == nil {
		t.Fatal("epic_completed event was not found")
	}

	// Validate event fields
	if epicCompletedEvent.IssueID != epic.ID {
		t.Errorf("Expected event IssueID %s, got %s", epic.ID, epicCompletedEvent.IssueID)
	}

	if epicCompletedEvent.ExecutorID != exec.instanceID {
		t.Errorf("Expected event ExecutorID %s, got %s", exec.instanceID, epicCompletedEvent.ExecutorID)
	}

	// Validate event data using typed getter
	data, err := epicCompletedEvent.GetEpicCompletedData()
	if err != nil {
		t.Fatalf("Failed to get epic completed data: %v", err)
	}

	if data.EpicID != epic.ID {
		t.Errorf("Expected EpicID %s, got %s", epic.ID, data.EpicID)
	}

	if data.EpicTitle != epic.Title {
		t.Errorf("Expected EpicTitle %s, got %s", epic.Title, data.EpicTitle)
	}

	if data.ChildrenCompleted != 1 {
		t.Errorf("Expected ChildrenCompleted 1, got %d", data.ChildrenCompleted)
	}

	if data.CompletionMethod != "all_children_closed" {
		t.Errorf("Expected CompletionMethod 'all_children_closed', got %s", data.CompletionMethod)
	}

	if data.IsMission {
		t.Error("Expected IsMission to be false for regular epic")
	}

	// Verify no cleanup events for regular epic (only missions have cleanup)
	for _, evt := range allEvents {
		if evt.Type == events.EventTypeEpicCleanupStarted || evt.Type == events.EventTypeEpicCleanupCompleted {
			t.Errorf("Unexpected cleanup event %s for regular epic", evt.Type)
		}
	}

	t.Log("✓ epic_completed event verified for regular epic")
}

// TestEpicLifecycleEvents_MissionEpic tests that epic_completed event is emitted for mission epics (vc-278).
// This test verifies the epic_completed event has IsMission=true for mission epics.
// Note: Sandbox cleanup events are tested separately in epic_sandbox_cleanup_test.go
func TestEpicLifecycleEvents_MissionEpic(t *testing.T) {
	ctx, store, exec := setupExecutorTest(t)
	defer store.Close()

	// Create a mission epic
	mission := &types.Mission{
		Issue: types.Issue{
			Title:        "Mission epic",
			Status:       types.StatusOpen,
			Priority:     1,
			IssueType:    types.TypeEpic,
			IssueSubtype: types.SubtypeMission,
		},
		SandboxPath: "/tmp/test-sandbox",
	}
	if err := store.CreateMission(ctx, mission, "test"); err != nil {
		t.Fatalf("Failed to create mission: %v", err)
	}

	// Create a child task
	task := &types.Issue{
		Title:     "Child task",
		Status:    types.StatusOpen,
		Priority:  1,
		IssueType: types.TypeTask,
		AcceptanceCriteria: "Test completes successfully",
	}
	if err := store.CreateIssue(ctx, task, "test"); err != nil {
		t.Fatalf("Failed to create task: %v", err)
	}

	// Link task to mission
	dep := &types.Dependency{
		IssueID:     task.ID,
		DependsOnID: mission.ID,
		Type:        types.DepParentChild,
	}
	if err := store.AddDependency(ctx, dep, "test"); err != nil {
		t.Fatalf("Failed to add dependency: %v", err)
	}

	// Close the task
	if err := store.CloseIssue(ctx, task.ID, "completed", "test"); err != nil {
		t.Fatalf("Failed to close task: %v", err)
	}

	// Call checkAndCloseEpicIfComplete directly
	closed, err := checkAndCloseEpicIfComplete(ctx, store, exec.supervisor, exec.instanceID, mission.ID)
	if err != nil {
		t.Fatalf("checkAndCloseEpicIfComplete failed: %v", err)
	}

	if !closed {
		t.Error("Expected mission epic to be closed, but it was not")
	}

	// Query stored events to verify epic_completed event was created
	allEvents, err := store.GetAgentEventsByIssue(ctx, mission.ID)
	if err != nil {
		t.Fatalf("Failed to get events: %v", err)
	}

	// Find epic_completed event
	var epicCompletedEvent *events.AgentEvent
	for _, evt := range allEvents {
		if evt.Type == events.EventTypeEpicCompleted {
			epicCompletedEvent = evt
			break
		}
	}

	// Verify epic_completed event exists
	if epicCompletedEvent == nil {
		t.Fatal("epic_completed event was not found")
	}

	completedData, err := epicCompletedEvent.GetEpicCompletedData()
	if err != nil {
		t.Fatalf("Failed to get epic completed data: %v", err)
	}

	if completedData.EpicID != mission.ID {
		t.Errorf("Expected EpicID %s, got %s", mission.ID, completedData.EpicID)
	}

	if !completedData.IsMission {
		t.Error("Expected IsMission to be true for mission epic")
	}

	if epicCompletedEvent.ExecutorID != exec.instanceID {
		t.Errorf("Expected ExecutorID %s, got %s", exec.instanceID, epicCompletedEvent.ExecutorID)
	}

	t.Log("✓ epic_completed event verified for mission epic with IsMission=true")
}

// TestEpicLifecycleEvents_ExecutorInstanceID tests that epic_completed events have the correct executor instance ID (vc-278).
func TestEpicLifecycleEvents_ExecutorInstanceID(t *testing.T) {
	ctx, store, exec := setupExecutorTest(t)
	defer store.Close()

	// Create a regular epic
	epic := &types.Issue{
		Title:        "Epic for instance ID test",
		Status:       types.StatusOpen,
		Priority:     1,
		IssueType:    types.TypeEpic,
		IssueSubtype: "", // Regular epic
	}
	if err := store.CreateIssue(ctx, epic, "test"); err != nil {
		t.Fatalf("Failed to create epic: %v", err)
	}

	// Create and close a child task
	task := &types.Issue{
		Title:     "Child task",
		Status:    types.StatusOpen,
		Priority:  1,
		IssueType: types.TypeTask,
		AcceptanceCriteria: "Test completes successfully",
	}
	if err := store.CreateIssue(ctx, task, "test"); err != nil {
		t.Fatalf("Failed to create task: %v", err)
	}

	dep := &types.Dependency{
		IssueID:     task.ID,
		DependsOnID: epic.ID,
		Type:        types.DepParentChild,
	}
	if err := store.AddDependency(ctx, dep, "test"); err != nil {
		t.Fatalf("Failed to add dependency: %v", err)
	}

	if err := store.CloseIssue(ctx, task.ID, "completed", "test"); err != nil {
		t.Fatalf("Failed to close task: %v", err)
	}

	// Close epic
	_, err := checkAndCloseEpicIfComplete(ctx, store, exec.supervisor, exec.instanceID, epic.ID)
	if err != nil {
		t.Fatalf("checkAndCloseEpicIfComplete failed: %v", err)
	}

	// Get all events and verify they have the correct executor instance ID
	allEvents, err := store.GetAgentEventsByIssue(ctx, epic.ID)
	if err != nil {
		t.Fatalf("Failed to get events: %v", err)
	}

	epicEventCount := 0
	for _, evt := range allEvents {
		// Only check epic_completed event
		if evt.Type == events.EventTypeEpicCompleted {
			epicEventCount++

			if evt.ExecutorID != exec.instanceID {
				t.Errorf("Event %s has incorrect ExecutorID: expected %s, got %s",
					evt.Type, exec.instanceID, evt.ExecutorID)
			}
		}
	}

	if epicEventCount != 1 {
		t.Errorf("Expected 1 epic_completed event, got %d", epicEventCount)
	}

	t.Log("✓ epic_completed event has correct executor instance ID")
}
