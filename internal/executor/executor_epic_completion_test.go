package executor

import (
	"testing"

	"github.com/steveyegge/vc/internal/types"
)

// TestCheckEpicCompletion_NoParentEpic tests that checkEpicCompletion handles tasks without parent epics
func TestCheckEpicCompletion_NoParentEpic(t *testing.T) {
	ctx, store, exec := setupExecutorTest(t)
	defer store.Close()

	// Create a standalone task with no parent epic
	task := &types.Issue{
		Title:     "Standalone task",
		Status:    types.StatusOpen,
		Priority:  1,
		IssueType: types.TypeTask,
	}
	if err := store.CreateIssue(ctx, task, "test"); err != nil {
		t.Fatalf("Failed to create task: %v", err)
	}

	// Close the task
	if err := store.CloseIssue(ctx, task.ID, "completed", "test"); err != nil {
		t.Fatalf("Failed to close task: %v", err)
	}

	// Reload the task to get updated status
	task, err := store.GetIssue(ctx, task.ID)
	if err != nil {
		t.Fatalf("Failed to get task: %v", err)
	}

	// Check epic completion - should not error, just return early
	err = exec.checkEpicCompletion(ctx, task)
	if err != nil {
		t.Errorf("checkEpicCompletion failed for task without parent: %v", err)
	}

	t.Log("✓ Gracefully handled task without parent epic")
}

// TestCheckEpicCompletion_EpicNotComplete tests that incomplete epics are not marked complete
func TestCheckEpicCompletion_EpicNotComplete(t *testing.T) {
	ctx, store, exec := setupExecutorTest(t)
	defer store.Close()

	// Create a mission epic
	mission := &types.Mission{
		Issue: types.Issue{
			Title:        "Implement authentication",
			Status:       types.StatusOpen,
			Priority:     1,
			IssueType:    types.TypeEpic,
			IssueSubtype: "mission",
		},
	}
	if err := store.CreateMission(ctx, mission, "test"); err != nil {
		t.Fatalf("Failed to create mission: %v", err)
	}

	// Create two child tasks
	task1 := &types.Issue{
		Title:     "Task 1",
		Status:    types.StatusOpen,
		Priority:  1,
		IssueType: types.TypeTask,
	}
	if err := store.CreateIssue(ctx, task1, "test"); err != nil {
		t.Fatalf("Failed to create task1: %v", err)
	}

	// Close task1
	if err := store.CloseIssue(ctx, task1.ID, "completed", "test"); err != nil {
		t.Fatalf("Failed to close task1: %v", err)
	}

	// Reload task1
	task1, err := store.GetIssue(ctx, task1.ID)
	if err != nil {
		t.Fatalf("Failed to get task1: %v", err)
	}

	task2 := &types.Issue{
		Title:     "Task 2",
		Status:    types.StatusOpen, // Still open - epic not complete
		Priority:  1,
		IssueType: types.TypeTask,
	}
	if err := store.CreateIssue(ctx, task2, "test"); err != nil {
		t.Fatalf("Failed to create task2: %v", err)
	}

	// Link tasks to mission via parent-child dependencies
	dep1 := &types.Dependency{
		IssueID:     task1.ID,
		DependsOnID: mission.ID,
		Type:        types.DepParentChild,
	}
	if err := store.AddDependency(ctx, dep1, "test"); err != nil {
		t.Fatalf("Failed to add dependency: %v", err)
	}

	dep2 := &types.Dependency{
		IssueID:     task2.ID,
		DependsOnID: mission.ID,
		Type:        types.DepParentChild,
	}
	if err := store.AddDependency(ctx, dep2, "test"); err != nil {
		t.Fatalf("Failed to add dependency: %v", err)
	}

	// Check epic completion after task1 completes
	if err := exec.checkEpicCompletion(ctx, task1); err != nil {
		t.Errorf("checkEpicCompletion failed: %v", err)
	}

	// Verify 'needs-quality-gates' label was NOT added
	labels, err2 := store.GetLabels(ctx, mission.ID)
	if err2 != nil {
		t.Fatalf("Failed to get labels: %v", err2)
	}

	for _, label := range labels {
		if label == "needs-quality-gates" {
			t.Error("Epic should not have needs-quality-gates label (task2 still open)")
		}
	}

	t.Log("✓ Incomplete epic not marked as complete")
}

// TestCheckEpicCompletion_EpicComplete tests that complete epics get needs-quality-gates label
func TestCheckEpicCompletion_EpicComplete(t *testing.T) {
	ctx, store, exec := setupExecutorTest(t)
	defer store.Close()

	// Create a mission epic
	mission := &types.Mission{
		Issue: types.Issue{
			Title:        "Implement authentication",
			Status:       types.StatusOpen,
			Priority:     1,
			IssueType:    types.TypeEpic,
			IssueSubtype: "mission",
		},
	}
	if err := store.CreateMission(ctx, mission, "test"); err != nil {
		t.Fatalf("Failed to create mission: %v", err)
	}

	// Create two child tasks
	task1 := &types.Issue{
		Title:     "Task 1",
		Status:    types.StatusOpen,
		Priority:  1,
		IssueType: types.TypeTask,
	}
	if err := store.CreateIssue(ctx, task1, "test"); err != nil {
		t.Fatalf("Failed to create task1: %v", err)
	}

	task2 := &types.Issue{
		Title:     "Task 2",
		Status:    types.StatusOpen,
		Priority:  1,
		IssueType: types.TypeTask,
	}
	if err := store.CreateIssue(ctx, task2, "test"); err != nil {
		t.Fatalf("Failed to create task2: %v", err)
	}

	// Link tasks to mission via parent-child dependencies
	dep1 := &types.Dependency{
		IssueID:     task1.ID,
		DependsOnID: mission.ID,
		Type:        types.DepParentChild,
	}
	if err := store.AddDependency(ctx, dep1, "test"); err != nil {
		t.Fatalf("Failed to add dependency: %v", err)
	}

	dep2 := &types.Dependency{
		IssueID:     task2.ID,
		DependsOnID: mission.ID,
		Type:        types.DepParentChild,
	}
	if err := store.AddDependency(ctx, dep2, "test"); err != nil {
		t.Fatalf("Failed to add dependency: %v", err)
	}

	// Close both tasks
	if err := store.CloseIssue(ctx, task1.ID, "completed", "test"); err != nil {
		t.Fatalf("Failed to close task1: %v", err)
	}
	if err := store.CloseIssue(ctx, task2.ID, "completed", "test"); err != nil {
		t.Fatalf("Failed to close task2: %v", err)
	}

	// Reload task2 after closing
	task2, err := store.GetIssue(ctx, task2.ID)
	if err != nil {
		t.Fatalf("Failed to get task2: %v", err)
	}

	// Check epic completion after task2 completes (the last task)
	err = exec.checkEpicCompletion(ctx, task2)
	if err != nil {
		t.Errorf("checkEpicCompletion failed: %v", err)
	}

	// Verify 'needs-quality-gates' label was added
	labels, err := store.GetLabels(ctx, mission.ID)
	if err != nil {
		t.Fatalf("Failed to get labels: %v", err)
	}

	hasLabel := false
	for _, label := range labels {
		if label == "needs-quality-gates" {
			hasLabel = true
			break
		}
	}

	if !hasLabel {
		t.Error("Epic should have needs-quality-gates label (all tasks complete)")
	}

	t.Log("✓ Complete epic marked with needs-quality-gates label")
}

// TestCheckEpicCompletion_NestedEpics tests recursive epic completion checking
func TestCheckEpicCompletion_NestedEpics(t *testing.T) {
	ctx, store, exec := setupExecutorTest(t)
	defer store.Close()

	// Create a mission epic (top-level)
	mission := &types.Mission{
		Issue: types.Issue{
			Title:        "Implement authentication",
			Status:       types.StatusOpen,
			Priority:     1,
			IssueType:    types.TypeEpic,
			IssueSubtype: "mission",
		},
	}
	if err := store.CreateMission(ctx, mission, "test"); err != nil {
		t.Fatalf("Failed to create mission: %v", err)
	}

	// Create a phase epic (child of mission)
	phase := &types.Mission{
		Issue: types.Issue{
			Title:        "Phase 1: Basic auth",
			Status:       types.StatusOpen,
			Priority:     1,
			IssueType:    types.TypeEpic,
			IssueSubtype: "phase",
		},
	}
	if err := store.CreateMission(ctx, phase, "test"); err != nil {
		t.Fatalf("Failed to create phase: %v", err)
	}

	// Link phase to mission
	phaseDep := &types.Dependency{
		IssueID:     phase.ID,
		DependsOnID: mission.ID,
		Type:        types.DepParentChild,
	}
	if err := store.AddDependency(ctx, phaseDep, "test"); err != nil {
		t.Fatalf("Failed to add phase dependency: %v", err)
	}

	// Create a task (child of phase)
	task := &types.Issue{
		Title:     "Implement login",
		Status:    types.StatusOpen,
		Priority:  1,
		IssueType: types.TypeTask,
	}
	if err := store.CreateIssue(ctx, task, "test"); err != nil {
		t.Fatalf("Failed to create task: %v", err)
	}

	// Link task to phase
	taskDep := &types.Dependency{
		IssueID:     task.ID,
		DependsOnID: phase.ID,
		Type:        types.DepParentChild,
	}
	if err := store.AddDependency(ctx, taskDep, "test"); err != nil {
		t.Fatalf("Failed to add task dependency: %v", err)
	}

	// Close the task
	if err := store.CloseIssue(ctx, task.ID, "completed", "test"); err != nil {
		t.Fatalf("Failed to close task: %v", err)
	}

	// Reload task after closing
	task, err := store.GetIssue(ctx, task.ID)
	if err != nil {
		t.Fatalf("Failed to get task: %v", err)
	}

	// Check epic completion - should cascade up: phase → mission
	err = exec.checkEpicCompletion(ctx, task)
	if err != nil {
		t.Errorf("checkEpicCompletion failed: %v", err)
	}

	// Verify phase has needs-quality-gates label
	phaseLabels, err := store.GetLabels(ctx, phase.ID)
	if err != nil {
		t.Fatalf("Failed to get phase labels: %v", err)
	}

	hasPhaseLabel := false
	for _, label := range phaseLabels {
		if label == "needs-quality-gates" {
			hasPhaseLabel = true
			break
		}
	}

	if !hasPhaseLabel {
		t.Error("Phase should have needs-quality-gates label (task complete)")
	}

	// Note: The mission does NOT automatically get the label because IsEpicComplete
	// checks if children are StatusClosed, and the phase is still StatusOpen (even though it's complete).
	// This is a known limitation - epic completion checking is one level deep.
	// Future enhancement (vc-236): Make IsEpicComplete recursively check nested epics.

	// For now, verify the phase got the label (which is correct behavior)
	t.Log("✓ Nested epic completion detected for phase epic")
	t.Log("  Note: Mission-level completion requires manual verification (known limitation)")
}

// TestCheckEpicCompletion_EventLogging tests that epic completion is logged to activity feed
func TestCheckEpicCompletion_EventLogging(t *testing.T) {
	ctx, store, exec := setupExecutorTest(t)
	defer store.Close()

	// Create a simple epic with one task
	epic := &types.Mission{
		Issue: types.Issue{
			Title:        "Simple epic",
			Status:       types.StatusOpen,
			Priority:     1,
			IssueType:    types.TypeEpic,
			IssueSubtype: "mission",
		},
	}
	if err := store.CreateMission(ctx, epic, "test"); err != nil {
		t.Fatalf("Failed to create epic: %v", err)
	}

	task := &types.Issue{
		Title:     "Simple task",
		Status:    types.StatusOpen,
		Priority:  1,
		IssueType: types.TypeTask,
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

	// Close the task
	if err := store.CloseIssue(ctx, task.ID, "completed", "test"); err != nil {
		t.Fatalf("Failed to close task: %v", err)
	}

	// Reload task
	task, err := store.GetIssue(ctx, task.ID)
	if err != nil {
		t.Fatalf("Failed to get task: %v", err)
	}

	// Check epic completion
	err = exec.checkEpicCompletion(ctx, task)
	if err != nil {
		t.Errorf("checkEpicCompletion failed: %v", err)
	}

	// Verify event was logged
	events, err := store.GetAgentEventsByIssue(ctx, epic.ID)
	if err != nil {
		t.Fatalf("Failed to get agent events: %v", err)
	}

	foundEvent := false
	for _, event := range events {
		if event.Type == "progress" && event.Data != nil {
			if eventSubtype, ok := event.Data["event_subtype"].(string); ok && eventSubtype == "epic_completed" {
				foundEvent = true
				// Verify event data
				if epicID, ok := event.Data["epic_id"].(string); !ok || epicID != epic.ID {
					t.Error("Event should have correct epic_id")
				}
				if labelAdded, ok := event.Data["label_added"].(string); !ok || labelAdded != "needs-quality-gates" {
					t.Error("Event should indicate needs-quality-gates label was added")
				}
				break
			}
		}
	}

	if !foundEvent {
		t.Error("Epic completion event should be logged to activity feed")
	}

	t.Log("✓ Epic completion event logged correctly")
}
