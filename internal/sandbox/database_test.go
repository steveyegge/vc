package sandbox

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/steveyegge/vc/internal/storage"
	"github.com/steveyegge/vc/internal/types"
)

func TestInitSandboxDB(t *testing.T) {
	ctx := context.Background()

	// Create temp directory for sandbox
	tmpDir, err := os.MkdirTemp("", "sandbox-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Initialize sandbox DB
	parentDBPath := filepath.Join(t.TempDir(), "parent.db")
	dbPath, err := initSandboxDB(ctx, tmpDir, "vc-123", parentDBPath)
	if err != nil {
		t.Fatalf("initSandboxDB failed: %v", err)
	}

	// Verify database file exists
	expectedPath := filepath.Join(tmpDir, ".beads", "mission.db")
	if dbPath != expectedPath {
		t.Errorf("expected db path %s, got %s", expectedPath, dbPath)
	}

	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		t.Errorf("database file was not created at %s", dbPath)
	}

	// Verify we can open the database
	cfg := &storage.Config{Path: dbPath}
	store, err := storage.NewStorage(ctx, cfg)
	if err != nil {
		t.Fatalf("failed to open created database: %v", err)
	}
	defer store.Close()

	// Verify metadata table exists and has data
	// Note: We can't easily test this without exposing the metadata retrieval,
	// but the fact that the database opens successfully is a good sign
}

func TestCopyCoreIssues(t *testing.T) {
	ctx := context.Background()

	// Create main database
	mainDBPath := filepath.Join(t.TempDir(), "main.db")
	mainDB, err := storage.NewStorage(ctx, &storage.Config{Path: mainDBPath})
	if err != nil {
		t.Fatalf("failed to create main DB: %v", err)
	}
	defer mainDB.Close()

	// Create sandbox database
	sandboxDBPath := filepath.Join(t.TempDir(), "sandbox.db")
	sandboxDB, err := storage.NewStorage(ctx, &storage.Config{Path: sandboxDBPath})
	if err != nil {
		t.Fatalf("failed to create sandbox DB: %v", err)
	}
	defer sandboxDB.Close()

	// Create a mission with dependencies
	mission := &types.Issue{
		ID:          "vc-100",
		Title:       "Test Mission",
		Description: "Mission for testing",
		Status:      types.StatusOpen,
		Priority:    1,
		IssueType:   types.TypeEpic,
	}
	if err := mainDB.CreateIssue(ctx, mission, "test"); err != nil {
		t.Fatalf("failed to create mission: %v", err)
	}

	// Create a blocking dependency
	blocker := &types.Issue{
		ID:          "vc-101",
		Title:       "Blocking Issue",
		Description: "Blocks the mission",
		Status:      types.StatusOpen,
		Priority:    1,
		IssueType:   types.TypeTask,
	}
	if err := mainDB.CreateIssue(ctx, blocker, "test"); err != nil {
		t.Fatalf("failed to create blocker: %v", err)
	}

	// Add dependency (mission depends on blocker)
	dep := &types.Dependency{
		IssueID:     mission.ID,
		DependsOnID: blocker.ID,
		Type:        types.DepBlocks,
		CreatedBy:   "test",
	}
	if err := mainDB.AddDependency(ctx, dep, "test"); err != nil {
		t.Fatalf("failed to add dependency: %v", err)
	}

	// Create a child issue
	child := &types.Issue{
		ID:          "vc-102",
		Title:       "Child Task",
		Description: "Child of mission",
		Status:      types.StatusOpen,
		Priority:    2,
		IssueType:   types.TypeTask,
	}
	if err := mainDB.CreateIssue(ctx, child, "test"); err != nil {
		t.Fatalf("failed to create child: %v", err)
	}

	// Add parent-child dependency
	childDep := &types.Dependency{
		IssueID:     child.ID,
		DependsOnID: mission.ID,
		Type:        types.DepParentChild,
		CreatedBy:   "test",
	}
	if err := mainDB.AddDependency(ctx, childDep, "test"); err != nil {
		t.Fatalf("failed to add child dependency: %v", err)
	}

	// Add labels
	if err := mainDB.AddLabel(ctx, mission.ID, "test-label", "test"); err != nil {
		t.Fatalf("failed to add label: %v", err)
	}

	// Copy core issues
	if err := copyCoreIssues(ctx, mainDB, sandboxDB, mission.ID); err != nil {
		t.Fatalf("copyCoreIssues failed: %v", err)
	}

	// Verify mission was copied
	copiedMission, err := sandboxDB.GetIssue(ctx, mission.ID)
	if err != nil {
		t.Fatalf("failed to get copied mission: %v", err)
	}
	if copiedMission == nil {
		t.Fatal("mission was not copied")
	}
	if copiedMission.Title != mission.Title {
		t.Errorf("mission title mismatch: expected %s, got %s", mission.Title, copiedMission.Title)
	}

	// Verify blocker was copied
	copiedBlocker, err := sandboxDB.GetIssue(ctx, blocker.ID)
	if err != nil {
		t.Fatalf("failed to get copied blocker: %v", err)
	}
	if copiedBlocker == nil {
		t.Error("blocker was not copied")
	}

	// Verify child was copied
	copiedChild, err := sandboxDB.GetIssue(ctx, child.ID)
	if err != nil {
		t.Fatalf("failed to get copied child: %v", err)
	}
	if copiedChild == nil {
		t.Error("child was not copied")
	}

	// Verify dependencies were copied
	deps, err := sandboxDB.GetDependencies(ctx, mission.ID)
	if err != nil {
		t.Fatalf("failed to get dependencies: %v", err)
	}
	if len(deps) == 0 {
		t.Error("dependencies were not copied")
	}

	// Verify labels were copied
	labels, err := sandboxDB.GetLabels(ctx, mission.ID)
	if err != nil {
		t.Fatalf("failed to get labels: %v", err)
	}
	if len(labels) == 0 {
		t.Error("labels were not copied")
	}
	if labels[0] != "test-label" {
		t.Errorf("expected label 'test-label', got %s", labels[0])
	}
}

func TestCopyCoreIssuesRecursive(t *testing.T) {
	ctx := context.Background()

	// Create main database
	mainDB, err := storage.NewStorage(ctx, &storage.Config{Path: ":memory:"})
	if err != nil {
		t.Fatalf("failed to create main DB: %v", err)
	}
	defer mainDB.Close()

	// Create sandbox database
	sandboxDB, err := storage.NewStorage(ctx, &storage.Config{Path: ":memory:"})
	if err != nil {
		t.Fatalf("failed to create sandbox DB: %v", err)
	}
	defer sandboxDB.Close()

	// Create a chain of dependencies: mission -> dep1 -> dep2
	mission := &types.Issue{
		ID:          "vc-200",
		Title:       "Mission",
		Description: "Top level",
		Status:      types.StatusOpen,
		Priority:    1,
		IssueType:   types.TypeEpic,
	}
	dep1 := &types.Issue{
		ID:          "vc-201",
		Title:       "Dependency 1",
		Description: "First level dep",
		Status:      types.StatusOpen,
		Priority:    1,
		IssueType:   types.TypeTask,
	}
	dep2 := &types.Issue{
		ID:          "vc-202",
		Title:       "Dependency 2",
		Description: "Second level dep",
		Status:      types.StatusOpen,
		Priority:    1,
		IssueType:   types.TypeTask,
	}

	// Create issues
	for _, issue := range []*types.Issue{mission, dep1, dep2} {
		if err := mainDB.CreateIssue(ctx, issue, "test"); err != nil {
			t.Fatalf("failed to create issue %s: %v", issue.ID, err)
		}
	}

	// Create dependency chain
	if err := mainDB.AddDependency(ctx, &types.Dependency{
		IssueID:     mission.ID,
		DependsOnID: dep1.ID,
		Type:        types.DepBlocks,
		CreatedBy:   "test",
	}, "test"); err != nil {
		t.Fatalf("failed to add dependency: %v", err)
	}

	if err := mainDB.AddDependency(ctx, &types.Dependency{
		IssueID:     dep1.ID,
		DependsOnID: dep2.ID,
		Type:        types.DepBlocks,
		CreatedBy:   "test",
	}, "test"); err != nil {
		t.Fatalf("failed to add dependency: %v", err)
	}

	// Copy core issues
	if err := copyCoreIssues(ctx, mainDB, sandboxDB, mission.ID); err != nil {
		t.Fatalf("copyCoreIssues failed: %v", err)
	}

	// Verify all three issues were copied
	for _, id := range []string{mission.ID, dep1.ID, dep2.ID} {
		issue, err := sandboxDB.GetIssue(ctx, id)
		if err != nil {
			t.Fatalf("failed to get issue %s: %v", id, err)
		}
		if issue == nil {
			t.Errorf("issue %s was not copied", id)
		}
	}
}

func TestMergeResults(t *testing.T) {
	ctx := context.Background()

	// Create main database
	mainDB, err := storage.NewStorage(ctx, &storage.Config{Path: ":memory:"})
	if err != nil {
		t.Fatalf("failed to create main DB: %v", err)
	}
	defer mainDB.Close()

	// Create sandbox database
	sandboxDB, err := storage.NewStorage(ctx, &storage.Config{Path: ":memory:"})
	if err != nil {
		t.Fatalf("failed to create sandbox DB: %v", err)
	}
	defer sandboxDB.Close()

	// Create a mission in both databases
	mission := &types.Issue{
		ID:          "vc-300",
		Title:       "Test Mission",
		Description: "Mission for merge testing",
		Status:      types.StatusOpen,
		Priority:    1,
		IssueType:   types.TypeEpic,
	}
	if err := mainDB.CreateIssue(ctx, mission, "test"); err != nil {
		t.Fatalf("failed to create mission in main DB: %v", err)
	}
	if err := sandboxDB.CreateIssue(ctx, mission, "test"); err != nil {
		t.Fatalf("failed to create mission in sandbox DB: %v", err)
	}

	// Update mission status in sandbox
	if err := sandboxDB.UpdateIssue(ctx, mission.ID, map[string]interface{}{
		"status": types.StatusClosed,
	}, "agent"); err != nil {
		t.Fatalf("failed to update mission in sandbox: %v", err)
	}

	// Create a discovered issue in sandbox (doesn't exist in main)
	discovered := &types.Issue{
		ID:          "vc-301",
		Title:       "Discovered Issue",
		Description: "Found during execution",
		Status:      types.StatusOpen,
		Priority:    2,
		IssueType:   types.TypeTask,
	}
	if err := sandboxDB.CreateIssue(ctx, discovered, "agent"); err != nil {
		t.Fatalf("failed to create discovered issue: %v", err)
	}

	// Add a label to discovered issue
	if err := sandboxDB.AddLabel(ctx, discovered.ID, "discovered", "agent"); err != nil {
		t.Fatalf("failed to add label: %v", err)
	}

	// Merge results
	if err := mergeResults(ctx, sandboxDB, mainDB, mission.ID); err != nil {
		t.Fatalf("mergeResults failed: %v", err)
	}

	// Verify mission status was updated in main DB
	mainMission, err := mainDB.GetIssue(ctx, mission.ID)
	if err != nil {
		t.Fatalf("failed to get mission from main DB: %v", err)
	}
	if mainMission.Status != types.StatusClosed {
		t.Errorf("mission status not updated: expected %s, got %s", types.StatusClosed, mainMission.Status)
	}

	// Verify discovered issue was created in main DB
	mainDiscovered, err := mainDB.GetIssue(ctx, discovered.ID)
	if err != nil {
		t.Fatalf("failed to get discovered issue: %v", err)
	}
	if mainDiscovered == nil {
		t.Fatal("discovered issue was not merged")
	}
	if mainDiscovered.Title != discovered.Title {
		t.Errorf("discovered issue title mismatch: expected %s, got %s",
			discovered.Title, mainDiscovered.Title)
	}

	// Verify labels were copied for discovered issue
	labels, err := mainDB.GetLabels(ctx, discovered.ID)
	if err != nil {
		t.Fatalf("failed to get labels: %v", err)
	}
	if len(labels) == 0 {
		t.Error("labels were not copied for discovered issue")
	}
}

func TestMergeResultsWithComments(t *testing.T) {
	ctx := context.Background()

	// Create main database
	mainDB, err := storage.NewStorage(ctx, &storage.Config{Path: ":memory:"})
	if err != nil {
		t.Fatalf("failed to create main DB: %v", err)
	}
	defer mainDB.Close()

	// Create sandbox database
	sandboxDB, err := storage.NewStorage(ctx, &storage.Config{Path: ":memory:"})
	if err != nil {
		t.Fatalf("failed to create sandbox DB: %v", err)
	}
	defer sandboxDB.Close()

	// Create a mission
	mission := &types.Issue{
		ID:          "vc-400",
		Title:       "Test Mission",
		Description: "For comment testing",
		Status:      types.StatusOpen,
		Priority:    1,
		IssueType:   types.TypeEpic,
	}
	if err := mainDB.CreateIssue(ctx, mission, "test"); err != nil {
		t.Fatalf("failed to create mission in main DB: %v", err)
	}
	if err := sandboxDB.CreateIssue(ctx, mission, "test"); err != nil {
		t.Fatalf("failed to create mission in sandbox DB: %v", err)
	}

	// Add comments in sandbox
	if err := sandboxDB.AddComment(ctx, mission.ID, "agent", "Work in progress"); err != nil {
		t.Fatalf("failed to add comment: %v", err)
	}

	// Merge results
	if err := mergeResults(ctx, sandboxDB, mainDB, mission.ID); err != nil {
		t.Fatalf("mergeResults failed: %v", err)
	}

	// Verify comments were merged (as sandbox execution comments)
	events, err := mainDB.GetEvents(ctx, mission.ID, 10)
	if err != nil {
		t.Fatalf("failed to get events: %v", err)
	}

	// Should have at least creation event and merged comment
	if len(events) < 2 {
		t.Errorf("expected at least 2 events, got %d", len(events))
	}

	// Check that at least one event is a sandbox comment
	foundSandboxComment := false
	for _, event := range events {
		if event.EventType == types.EventCommented && event.Comment != nil {
			if event.Actor == "sandbox-merge" {
				foundSandboxComment = true
				break
			}
		}
	}
	if !foundSandboxComment {
		t.Error("sandbox comments were not merged to main DB")
	}
}
