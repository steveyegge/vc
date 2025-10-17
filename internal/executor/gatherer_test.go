package executor

import (
	"context"
	"testing"
	"time"

	"github.com/steveyegge/vc/internal/storage"
	"github.com/steveyegge/vc/internal/types"
)

func TestNewContextGatherer(t *testing.T) {
	ctx := context.Background()
	cfg := storage.DefaultConfig()
	cfg.Path = ":memory:"
	store, err := storage.NewStorage(ctx, cfg)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer store.Close()

	gatherer := NewContextGatherer(store)
	if gatherer == nil {
		t.Fatal("NewContextGatherer returned nil")
	}
}

func TestGetParentMission(t *testing.T) {
	ctx := context.Background()
	cfg := storage.DefaultConfig()
	cfg.Path = ":memory:"
	store, err := storage.NewStorage(ctx, cfg)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer store.Close()

	gatherer := NewContextGatherer(store)

	// Create parent issue
	parent := &types.Issue{
		Title:    "Parent Mission",
		Status:   types.StatusOpen,
		Priority: 1,
		IssueType: types.TypeEpic,
	}
	if err := store.CreateIssue(ctx, parent, "test"); err != nil {
		t.Fatalf("Failed to create parent issue: %v", err)
	}

	// Create child issue
	child := &types.Issue{
		Title:    "Child Task",
		Status:   types.StatusOpen,
		Priority: 2,
		IssueType: types.TypeTask,
	}
	if err := store.CreateIssue(ctx, child, "test"); err != nil {
		t.Fatalf("Failed to create child issue: %v", err)
	}

	// Add parent-child dependency
	dep := &types.Dependency{
		IssueID:     child.ID,
		DependsOnID: parent.ID,
		Type:        types.DepParentChild,
	}
	if err := store.AddDependency(ctx, dep, "test"); err != nil {
		t.Fatalf("Failed to add dependency: %v", err)
	}

	// Test GetParentMission
	foundParent, err := gatherer.GetParentMission(ctx, child)
	if err != nil {
		t.Fatalf("GetParentMission failed: %v", err)
	}

	if foundParent == nil {
		t.Fatal("Expected to find parent, got nil")
	}

	if foundParent.ID != parent.ID {
		t.Errorf("Expected parent ID %s, got %s", parent.ID, foundParent.ID)
	}

	// Test with issue that has no parent
	orphan := &types.Issue{
		Title:    "Orphan Issue",
		Status:   types.StatusOpen,
		Priority: 1,
		IssueType: types.TypeTask,
	}
	if err := store.CreateIssue(ctx, orphan, "test"); err != nil {
		t.Fatalf("Failed to create orphan issue: %v", err)
	}

	foundParent, err = gatherer.GetParentMission(ctx, orphan)
	if err != nil {
		t.Fatalf("GetParentMission failed for orphan: %v", err)
	}

	if foundParent != nil {
		t.Errorf("Expected nil parent for orphan, got %v", foundParent)
	}
}

func TestGetRelatedIssues(t *testing.T) {
	ctx := context.Background()
	cfg := storage.DefaultConfig()
	cfg.Path = ":memory:"
	store, err := storage.NewStorage(ctx, cfg)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer store.Close()

	gatherer := NewContextGatherer(store)

	// Create parent epic
	parent := &types.Issue{
		Title:    "Parent Epic",
		Status:   types.StatusOpen,
		Priority: 1,
		IssueType: types.TypeEpic,
	}
	if err := store.CreateIssue(ctx, parent, "test"); err != nil {
		t.Fatalf("Failed to create parent: %v", err)
	}

	// Create main task
	task := &types.Issue{
		Title:    "Main Task",
		Status:   types.StatusOpen,
		Priority: 2,
		IssueType: types.TypeTask,
	}
	if err := store.CreateIssue(ctx, task, "test"); err != nil {
		t.Fatalf("Failed to create task: %v", err)
	}

	// Create sibling
	sibling := &types.Issue{
		Title:    "Sibling Task",
		Status:   types.StatusOpen,
		Priority: 2,
		IssueType: types.TypeTask,
	}
	if err := store.CreateIssue(ctx, sibling, "test"); err != nil {
		t.Fatalf("Failed to create sibling: %v", err)
	}

	// Create blocker
	blocker := &types.Issue{
		Title:    "Blocker Issue",
		Status:   types.StatusOpen,
		Priority: 1,
		IssueType: types.TypeTask,
	}
	if err := store.CreateIssue(ctx, blocker, "test"); err != nil {
		t.Fatalf("Failed to create blocker: %v", err)
	}

	// Create dependent
	dependent := &types.Issue{
		Title:    "Dependent Issue",
		Status:   types.StatusOpen,
		Priority: 3,
		IssueType: types.TypeTask,
	}
	if err := store.CreateIssue(ctx, dependent, "test"); err != nil {
		t.Fatalf("Failed to create dependent: %v", err)
	}

	// Set up relationships
	// task -> parent (parent-child)
	if err := store.AddDependency(ctx, &types.Dependency{
		IssueID:     task.ID,
		DependsOnID: parent.ID,
		Type:        types.DepParentChild,
	}, "test"); err != nil {
		t.Fatalf("Failed to add parent-child dependency: %v", err)
	}

	// sibling -> parent (parent-child)
	if err := store.AddDependency(ctx, &types.Dependency{
		IssueID:     sibling.ID,
		DependsOnID: parent.ID,
		Type:        types.DepParentChild,
	}, "test"); err != nil {
		t.Fatalf("Failed to add sibling dependency: %v", err)
	}

	// task -> blocker (blocks)
	if err := store.AddDependency(ctx, &types.Dependency{
		IssueID:     task.ID,
		DependsOnID: blocker.ID,
		Type:        types.DepBlocks,
	}, "test"); err != nil {
		t.Fatalf("Failed to add blocker dependency: %v", err)
	}

	// dependent -> task (blocks)
	if err := store.AddDependency(ctx, &types.Dependency{
		IssueID:     dependent.ID,
		DependsOnID: task.ID,
		Type:        types.DepBlocks,
	}, "test"); err != nil {
		t.Fatalf("Failed to add dependent dependency: %v", err)
	}

	// Test GetRelatedIssues
	related, err := gatherer.GetRelatedIssues(ctx, task)
	if err != nil {
		t.Fatalf("GetRelatedIssues failed: %v", err)
	}

	// Check blockers
	if len(related.Blockers) != 1 {
		t.Errorf("Expected 1 blocker, got %d", len(related.Blockers))
	} else if related.Blockers[0].ID != blocker.ID {
		t.Errorf("Expected blocker %s, got %s", blocker.ID, related.Blockers[0].ID)
	}

	// Check dependents
	if len(related.Dependents) != 1 {
		t.Errorf("Expected 1 dependent, got %d", len(related.Dependents))
	} else if related.Dependents[0].ID != dependent.ID {
		t.Errorf("Expected dependent %s, got %s", dependent.ID, related.Dependents[0].ID)
	}

	// Check siblings
	if len(related.Siblings) != 1 {
		t.Errorf("Expected 1 sibling, got %d", len(related.Siblings))
	} else if related.Siblings[0].ID != sibling.ID {
		t.Errorf("Expected sibling %s, got %s", sibling.ID, related.Siblings[0].ID)
	}
}

func TestGetPreviousAttempts(t *testing.T) {
	ctx := context.Background()
	cfg := storage.DefaultConfig()
	cfg.Path = ":memory:"
	store, err := storage.NewStorage(ctx, cfg)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer store.Close()

	gatherer := NewContextGatherer(store)

	// Create issue
	issue := &types.Issue{
		Title:    "Test Issue",
		Status:   types.StatusOpen,
		Priority: 1,
		IssueType: types.TypeTask,
	}
	if err := store.CreateIssue(ctx, issue, "test"); err != nil {
		t.Fatalf("Failed to create issue: %v", err)
	}

	// Create executor instance
	instance := &types.ExecutorInstance{
		InstanceID: "test-instance",
		Hostname:   "test-host",
		PID:        12345,
		Status:     types.ExecutorStatusRunning,
		StartedAt:  time.Now(),
		LastHeartbeat: time.Now(),
		Version:    "1.0.0",
		Metadata:   "{}",
	}
	if err := store.RegisterInstance(ctx, instance); err != nil {
		t.Fatalf("Failed to register instance: %v", err)
	}

	// Create execution attempts
	attempt1 := &types.ExecutionAttempt{
		IssueID:            issue.ID,
		ExecutorInstanceID: instance.InstanceID,
		AttemptNumber:      1,
		StartedAt:          time.Now(),
		Summary:            "First attempt",
	}
	if err := store.RecordExecutionAttempt(ctx, attempt1); err != nil {
		t.Fatalf("Failed to record attempt 1: %v", err)
	}

	attempt2 := &types.ExecutionAttempt{
		IssueID:            issue.ID,
		ExecutorInstanceID: instance.InstanceID,
		AttemptNumber:      2,
		StartedAt:          time.Now(),
		Summary:            "Second attempt",
	}
	if err := store.RecordExecutionAttempt(ctx, attempt2); err != nil {
		t.Fatalf("Failed to record attempt 2: %v", err)
	}

	// Test GetPreviousAttempts
	attempts, err := gatherer.GetPreviousAttempts(ctx, issue.ID)
	if err != nil {
		t.Fatalf("GetPreviousAttempts failed: %v", err)
	}

	if len(attempts) != 2 {
		t.Errorf("Expected 2 attempts, got %d", len(attempts))
	}

	// Verify attempts are in chronological order
	if len(attempts) == 2 {
		if attempts[0].AttemptNumber != 1 {
			t.Errorf("Expected first attempt to be #1, got #%d", attempts[0].AttemptNumber)
		}
		if attempts[1].AttemptNumber != 2 {
			t.Errorf("Expected second attempt to be #2, got #%d", attempts[1].AttemptNumber)
		}
	}
}

func TestAnalyzeResumeState(t *testing.T) {
	ctx := context.Background()
	cfg := storage.DefaultConfig()
	cfg.Path = ":memory:"
	store, err := storage.NewStorage(ctx, cfg)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer store.Close()

	gatherer := NewContextGatherer(store)

	// Test with no attempts
	hint, err := gatherer.AnalyzeResumeState(ctx, nil, nil)
	if err != nil {
		t.Fatalf("AnalyzeResumeState failed with no attempts: %v", err)
	}
	if hint != "" {
		t.Errorf("Expected empty hint for no attempts, got: %s", hint)
	}

	// Test with successful attempt
	now := time.Now()
	success := true
	attempts := []*types.ExecutionAttempt{
		{
			IssueID:            "test-1",
			ExecutorInstanceID: "inst-1",
			AttemptNumber:      1,
			StartedAt:          now,
			CompletedAt:        &now,
			Success:            &success,
			Summary:            "Task completed",
		},
	}

	hint, err = gatherer.AnalyzeResumeState(ctx, nil, attempts)
	if err != nil {
		t.Fatalf("AnalyzeResumeState failed: %v", err)
	}

	if hint == "" {
		t.Error("Expected non-empty hint")
	}

	if !containsString(hint, "attempt #1") {
		t.Errorf("Expected hint to mention attempt #1, got: %s", hint)
	}

	if !containsString(hint, "succeeded") {
		t.Errorf("Expected hint to mention success, got: %s", hint)
	}

	// Test with failed attempt
	failed := false
	exitCode := 1
	attempts = []*types.ExecutionAttempt{
		{
			IssueID:            "test-2",
			ExecutorInstanceID: "inst-1",
			AttemptNumber:      2,
			StartedAt:          now,
			CompletedAt:        &now,
			Success:            &failed,
			ExitCode:           &exitCode,
			Summary:            "Task failed",
			ErrorSample:        "Error: something went wrong",
		},
	}

	hint, err = gatherer.AnalyzeResumeState(ctx, nil, attempts)
	if err != nil {
		t.Fatalf("AnalyzeResumeState failed: %v", err)
	}

	if !containsString(hint, "exit code 1") {
		t.Errorf("Expected hint to mention exit code, got: %s", hint)
	}

	if !containsString(hint, "Error") {
		t.Errorf("Expected hint to include error sample, got: %s", hint)
	}

	// Test with incomplete attempt
	attempts = []*types.ExecutionAttempt{
		{
			IssueID:            "test-3",
			ExecutorInstanceID: "inst-1",
			AttemptNumber:      3,
			StartedAt:          now,
			CompletedAt:        nil, // Not completed
		},
	}

	hint, err = gatherer.AnalyzeResumeState(ctx, nil, attempts)
	if err != nil {
		t.Fatalf("AnalyzeResumeState failed: %v", err)
	}

	if !containsString(hint, "did not complete") {
		t.Errorf("Expected hint to mention incomplete attempt, got: %s", hint)
	}
}

func TestGatherContext(t *testing.T) {
	ctx := context.Background()
	cfg := storage.DefaultConfig()
	cfg.Path = ":memory:"
	store, err := storage.NewStorage(ctx, cfg)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer store.Close()

	gatherer := NewContextGatherer(store)

	// Create a complete scenario with parent, siblings, blockers, and execution history

	// Create parent
	parent := &types.Issue{
		Title:    "Parent Epic",
		Status:   types.StatusOpen,
		Priority: 1,
		IssueType: types.TypeEpic,
	}
	if err := store.CreateIssue(ctx, parent, "test"); err != nil {
		t.Fatalf("Failed to create parent: %v", err)
	}

	// Create main task
	task := &types.Issue{
		Title:    "Main Task",
		Status:   types.StatusInProgress,
		Priority: 2,
		IssueType: types.TypeTask,
	}
	if err := store.CreateIssue(ctx, task, "test"); err != nil {
		t.Fatalf("Failed to create task: %v", err)
	}

	// Create sibling
	sibling := &types.Issue{
		Title:    "Sibling Task",
		Status:   types.StatusOpen,
		Priority: 2,
		IssueType: types.TypeTask,
	}
	if err := store.CreateIssue(ctx, sibling, "test"); err != nil {
		t.Fatalf("Failed to create sibling: %v", err)
	}

	// Set up dependencies
	if err := store.AddDependency(ctx, &types.Dependency{
		IssueID:     task.ID,
		DependsOnID: parent.ID,
		Type:        types.DepParentChild,
	}, "test"); err != nil {
		t.Fatalf("Failed to add parent dependency: %v", err)
	}

	if err := store.AddDependency(ctx, &types.Dependency{
		IssueID:     sibling.ID,
		DependsOnID: parent.ID,
		Type:        types.DepParentChild,
	}, "test"); err != nil {
		t.Fatalf("Failed to add sibling dependency: %v", err)
	}

	// Create execution instance and attempt
	instance := &types.ExecutorInstance{
		InstanceID: "test-inst",
		Hostname:   "test-host",
		PID:        12345,
		Status:     types.ExecutorStatusRunning,
		StartedAt:  time.Now(),
		LastHeartbeat: time.Now(),
		Version:    "1.0.0",
		Metadata:   "{}",
	}
	if err := store.RegisterInstance(ctx, instance); err != nil {
		t.Fatalf("Failed to register instance: %v", err)
	}

	attempt := &types.ExecutionAttempt{
		IssueID:            task.ID,
		ExecutorInstanceID: instance.InstanceID,
		AttemptNumber:      1,
		StartedAt:          time.Now(),
		Summary:            "First attempt",
	}
	if err := store.RecordExecutionAttempt(ctx, attempt); err != nil {
		t.Fatalf("Failed to record attempt: %v", err)
	}

	// Gather context
	promptCtx, err := gatherer.GatherContext(ctx, task, nil)
	if err != nil {
		t.Fatalf("GatherContext failed: %v", err)
	}

	// Verify context
	if promptCtx.Issue == nil || promptCtx.Issue.ID != task.ID {
		t.Error("Expected issue to be set correctly")
	}

	if promptCtx.ParentMission == nil || promptCtx.ParentMission.ID != parent.ID {
		t.Error("Expected parent mission to be found")
	}

	if promptCtx.RelatedIssues == nil {
		t.Error("Expected related issues to be populated")
	} else {
		if len(promptCtx.RelatedIssues.Siblings) != 1 {
			t.Errorf("Expected 1 sibling, got %d", len(promptCtx.RelatedIssues.Siblings))
		}
	}

	if len(promptCtx.PreviousAttempts) != 1 {
		t.Errorf("Expected 1 previous attempt, got %d", len(promptCtx.PreviousAttempts))
	}

	if promptCtx.ResumeHint == "" {
		t.Error("Expected resume hint to be set")
	}
}

// Helper function
func containsString(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && (
		s[:len(substr)] == substr ||
		s[len(s)-len(substr):] == substr ||
		stringContains(s, substr)))
}

func stringContains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
