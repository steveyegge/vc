package executor

import (
	"context"
	"testing"
	"time"

	"github.com/steveyegge/vc/internal/storage"
	"github.com/steveyegge/vc/internal/types"
)

func TestCheckEpicCompletion(t *testing.T) {
	ctx := context.Background()

	// Create in-memory storage
	cfg := storage.DefaultConfig()
	cfg.Backend = "sqlite"
	cfg.Path = ":memory:"
	store, err := storage.NewStorage(ctx, cfg)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer store.Close()

	now := time.Now()

	// Create an epic
	epic := &types.Issue{
		ID:          "test-epic-1",
		Title:       "Test Epic",
		IssueType:   types.TypeEpic,
		Status:      types.StatusOpen,
		Priority:    1,
		Description: "Test epic description",
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if err := store.CreateIssue(ctx, epic, "test"); err != nil {
		t.Fatalf("Failed to create epic: %v", err)
	}

	// Create child issues
	child1 := &types.Issue{
		ID:          "test-child-1",
		Title:       "Child 1",
		IssueType:   types.TypeTask,
		Status:      types.StatusOpen,
		Priority:    1,
		Description: "Test child 1",
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	child2 := &types.Issue{
		ID:          "test-child-2",
		Title:       "Child 2",
		IssueType:   types.TypeTask,
		Status:      types.StatusOpen,
		Priority:    1,
		Description: "Test child 2",
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	child3 := &types.Issue{
		ID:          "test-child-3",
		Title:       "Child 3",
		IssueType:   types.TypeTask,
		Status:      types.StatusOpen,
		Priority:    1,
		Description: "Test child 3",
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	if err := store.CreateIssue(ctx, child1, "test"); err != nil {
		t.Fatalf("Failed to create child1: %v", err)
	}
	if err := store.CreateIssue(ctx, child2, "test"); err != nil {
		t.Fatalf("Failed to create child2: %v", err)
	}
	if err := store.CreateIssue(ctx, child3, "test"); err != nil {
		t.Fatalf("Failed to create child3: %v", err)
	}

	// Add dependencies: epic depends on children (epic can't close until children are done)
	// This means GetDependents(child) will return the epic
	dep1 := &types.Dependency{
		IssueID:     epic.ID,
		DependsOnID: child1.ID,
		Type:        types.DepBlocks,
		CreatedAt:   now,
		CreatedBy:   "test",
	}
	if err := store.AddDependency(ctx, dep1, "test"); err != nil {
		t.Fatalf("Failed to add dependency for child1: %v", err)
	}
	dep2 := &types.Dependency{
		IssueID:     epic.ID,
		DependsOnID: child2.ID,
		Type:        types.DepBlocks,
		CreatedAt:   now,
		CreatedBy:   "test",
	}
	if err := store.AddDependency(ctx, dep2, "test"); err != nil {
		t.Fatalf("Failed to add dependency for child2: %v", err)
	}
	dep3 := &types.Dependency{
		IssueID:     epic.ID,
		DependsOnID: child3.ID,
		Type:        types.DepBlocks,
		CreatedAt:   now,
		CreatedBy:   "test",
	}
	if err := store.AddDependency(ctx, dep3, "test"); err != nil {
		t.Fatalf("Failed to add dependency for child3: %v", err)
	}

	// Close first child - epic should remain open
	if err := store.CloseIssue(ctx, child1.ID, "Done", "test"); err != nil {
		t.Fatalf("Failed to close child1: %v", err)
	}
	if err := checkEpicCompletion(ctx, store, child1.ID); err != nil {
		t.Fatalf("checkEpicCompletion failed: %v", err)
	}

	// Verify epic is still open
	epicCheck, err := store.GetIssue(ctx, epic.ID)
	if err != nil {
		t.Fatalf("Failed to get epic: %v", err)
	}
	if epicCheck.Status == types.StatusClosed {
		t.Error("Epic should not be closed when only 1 of 3 children complete")
	}

	// Close second child - epic should still remain open
	if err := store.CloseIssue(ctx, child2.ID, "Done", "test"); err != nil {
		t.Fatalf("Failed to close child2: %v", err)
	}
	if err := checkEpicCompletion(ctx, store, child2.ID); err != nil {
		t.Fatalf("checkEpicCompletion failed: %v", err)
	}

	// Verify epic is still open
	epicCheck, err = store.GetIssue(ctx, epic.ID)
	if err != nil {
		t.Fatalf("Failed to get epic: %v", err)
	}
	if epicCheck.Status == types.StatusClosed {
		t.Error("Epic should not be closed when only 2 of 3 children complete")
	}

	// Close third child - epic should now auto-close
	if err := store.CloseIssue(ctx, child3.ID, "Done", "test"); err != nil {
		t.Fatalf("Failed to close child3: %v", err)
	}
	if err := checkEpicCompletion(ctx, store, child3.ID); err != nil {
		t.Fatalf("checkEpicCompletion failed: %v", err)
	}

	// Verify epic is now closed
	epicCheck, err = store.GetIssue(ctx, epic.ID)
	if err != nil {
		t.Fatalf("Failed to get epic: %v", err)
	}
	if epicCheck.Status != types.StatusClosed {
		t.Error("Epic should be closed when all 3 children are complete")
	}
}

func TestCheckEpicCompletionWithMultipleEpics(t *testing.T) {
	ctx := context.Background()

	// Create in-memory storage
	cfg := storage.DefaultConfig()
	cfg.Backend = "sqlite"
	cfg.Path = ":memory:"
	store, err := storage.NewStorage(ctx, cfg)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer store.Close()

	now := time.Now()

	// Create two epics
	epic1 := &types.Issue{
		ID:          "test-epic-1",
		Title:       "Test Epic 1",
		IssueType:   types.TypeEpic,
		Status:      types.StatusOpen,
		Priority:    1,
		Description: "Test epic 1",
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	epic2 := &types.Issue{
		ID:          "test-epic-2",
		Title:       "Test Epic 2",
		IssueType:   types.TypeEpic,
		Status:      types.StatusOpen,
		Priority:    1,
		Description: "Test epic 2",
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	if err := store.CreateIssue(ctx, epic1, "test"); err != nil {
		t.Fatalf("Failed to create epic1: %v", err)
	}
	if err := store.CreateIssue(ctx, epic2, "test"); err != nil {
		t.Fatalf("Failed to create epic2: %v", err)
	}

	// Create a shared child task
	child := &types.Issue{
		ID:          "test-child-1",
		Title:       "Shared Child",
		IssueType:   types.TypeTask,
		Status:      types.StatusOpen,
		Priority:    1,
		Description: "Shared child task",
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	if err := store.CreateIssue(ctx, child, "test"); err != nil {
		t.Fatalf("Failed to create child: %v", err)
	}

	// Add dependencies: both epics depend on the child (epics can't close until child is done)
	dep1 := &types.Dependency{
		IssueID:     epic1.ID,
		DependsOnID: child.ID,
		Type:        types.DepBlocks,
		CreatedAt:   now,
		CreatedBy:   "test",
	}
	if err := store.AddDependency(ctx, dep1, "test"); err != nil {
		t.Fatalf("Failed to add dependency for epic1: %v", err)
	}
	dep2 := &types.Dependency{
		IssueID:     epic2.ID,
		DependsOnID: child.ID,
		Type:        types.DepBlocks,
		CreatedAt:   now,
		CreatedBy:   "test",
	}
	if err := store.AddDependency(ctx, dep2, "test"); err != nil {
		t.Fatalf("Failed to add dependency for epic2: %v", err)
	}

	// Close child - both epics should auto-close since they have no other children
	if err := store.CloseIssue(ctx, child.ID, "Done", "test"); err != nil {
		t.Fatalf("Failed to close child: %v", err)
	}
	if err := checkEpicCompletion(ctx, store, child.ID); err != nil {
		t.Fatalf("checkEpicCompletion failed: %v", err)
	}

	// Verify both epics are closed
	epic1Check, err := store.GetIssue(ctx, epic1.ID)
	if err != nil {
		t.Fatalf("Failed to get epic1: %v", err)
	}
	if epic1Check.Status != types.StatusClosed {
		t.Error("Epic1 should be closed when its only child is complete")
	}

	epic2Check, err := store.GetIssue(ctx, epic2.ID)
	if err != nil {
		t.Fatalf("Failed to get epic2: %v", err)
	}
	if epic2Check.Status != types.StatusClosed {
		t.Error("Epic2 should be closed when its only child is complete")
	}
}
