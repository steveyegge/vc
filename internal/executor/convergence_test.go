package executor

import (
	"context"
	"testing"

	"github.com/steveyegge/vc/internal/storage"
	"github.com/steveyegge/vc/internal/types"
)

func setupConvergenceTestStorage(t *testing.T) (context.Context, storage.Storage) {
	ctx := context.Background()
	cfg := storage.DefaultConfig()
	cfg.Path = t.TempDir() + "/test.db"
	store, err := storage.NewStorage(ctx, cfg)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	return ctx, store
}

func createTestIssue(t *testing.T, ctx context.Context, store storage.Storage, title string, status types.Status) *types.Issue {
	issue := &types.Issue{
		Title:     title,
		Status:    types.StatusOpen,
		Priority:  2,
		IssueType: types.TypeTask,
	}
	if err := store.CreateIssue(ctx, issue, "test"); err != nil {
		t.Fatalf("Failed to create issue %s: %v", title, err)
	}

	// If status is closed, close it properly
	if status == types.StatusClosed {
		if err := store.CloseIssue(ctx, issue.ID, "test closed", "test"); err != nil {
			t.Fatalf("Failed to close issue %s: %v", title, err)
		}
		// Fetch updated issue to get closed_at timestamp
		issue, err := store.GetIssue(ctx, issue.ID)
		if err != nil {
			t.Fatalf("Failed to fetch closed issue %s: %v", title, err)
		}
		return issue
	}

	return issue
}

func addDiscoveredFromDep(t *testing.T, ctx context.Context, store storage.Storage, childID, parentID string) {
	dep := &types.Dependency{
		IssueID:     childID,
		DependsOnID: parentID,
		Type:        types.DepDiscoveredFrom,
	}
	if err := store.AddDependency(ctx, dep, "test"); err != nil {
		t.Fatalf("Failed to add discovered-from dependency: %v", err)
	}
}

func TestGetMissionRoot_NoParent(t *testing.T) {
	ctx, store := setupConvergenceTestStorage(t)
	defer store.Close()

	// Create a root issue with no discovered-from parent
	root := createTestIssue(t, ctx, store, "Root Issue", types.StatusOpen)

	// Test: GetMissionRoot should return the issue itself
	foundRoot, err := GetMissionRoot(ctx, store, root.ID)
	if err != nil {
		t.Fatalf("GetMissionRoot failed: %v", err)
	}

	if foundRoot.ID != root.ID {
		t.Errorf("Expected root ID %s, got %s", root.ID, foundRoot.ID)
	}
}

func TestGetMissionRoot_SingleParent(t *testing.T) {
	ctx, store := setupConvergenceTestStorage(t)
	defer store.Close()

	// Create chain: root ← child
	root := createTestIssue(t, ctx, store, "Root Issue", types.StatusOpen)
	child := createTestIssue(t, ctx, store, "Child Issue", types.StatusOpen)
	addDiscoveredFromDep(t, ctx, store, child.ID, root.ID)

	// Test: GetMissionRoot from child should return root
	foundRoot, err := GetMissionRoot(ctx, store, child.ID)
	if err != nil {
		t.Fatalf("GetMissionRoot failed: %v", err)
	}

	if foundRoot.ID != root.ID {
		t.Errorf("Expected root ID %s, got %s", root.ID, foundRoot.ID)
	}
}

func TestGetMissionRoot_DeepChain(t *testing.T) {
	ctx, store := setupConvergenceTestStorage(t)
	defer store.Close()

	// Create chain of depth 3+: root ← child1 ← child2 ← child3
	root := createTestIssue(t, ctx, store, "Root Issue", types.StatusOpen)
	child1 := createTestIssue(t, ctx, store, "Child 1", types.StatusOpen)
	child2 := createTestIssue(t, ctx, store, "Child 2", types.StatusOpen)
	child3 := createTestIssue(t, ctx, store, "Child 3", types.StatusOpen)

	addDiscoveredFromDep(t, ctx, store, child1.ID, root.ID)
	addDiscoveredFromDep(t, ctx, store, child2.ID, child1.ID)
	addDiscoveredFromDep(t, ctx, store, child3.ID, child2.ID)

	// Test: GetMissionRoot from deepest child should return root
	foundRoot, err := GetMissionRoot(ctx, store, child3.ID)
	if err != nil {
		t.Fatalf("GetMissionRoot failed: %v", err)
	}

	if foundRoot.ID != root.ID {
		t.Errorf("Expected root ID %s, got %s", root.ID, foundRoot.ID)
	}

	// Test from middle of chain
	foundRoot, err = GetMissionRoot(ctx, store, child2.ID)
	if err != nil {
		t.Fatalf("GetMissionRoot failed from middle: %v", err)
	}

	if foundRoot.ID != root.ID {
		t.Errorf("Expected root ID %s from middle, got %s", root.ID, foundRoot.ID)
	}
}

func TestGetMissionDiscoveries_NoDiscoveries(t *testing.T) {
	ctx, store := setupConvergenceTestStorage(t)
	defer store.Close()

	// Create a mission with no discoveries
	mission := createTestIssue(t, ctx, store, "Mission", types.StatusOpen)

	// Test: GetMissionDiscoveries should return empty list
	discoveries, err := GetMissionDiscoveries(ctx, store, mission.ID)
	if err != nil {
		t.Fatalf("GetMissionDiscoveries failed: %v", err)
	}

	if len(discoveries) != 0 {
		t.Errorf("Expected 0 discoveries, got %d", len(discoveries))
	}
}

func TestGetMissionDiscoveries_DirectDiscoveries(t *testing.T) {
	ctx, store := setupConvergenceTestStorage(t)
	defer store.Close()

	// Create mission with 2 direct discoveries
	mission := createTestIssue(t, ctx, store, "Mission", types.StatusOpen)
	disc1 := createTestIssue(t, ctx, store, "Discovery 1", types.StatusOpen)
	disc2 := createTestIssue(t, ctx, store, "Discovery 2", types.StatusOpen)

	addDiscoveredFromDep(t, ctx, store, disc1.ID, mission.ID)
	addDiscoveredFromDep(t, ctx, store, disc2.ID, mission.ID)

	// Test: Should return both discoveries
	discoveries, err := GetMissionDiscoveries(ctx, store, mission.ID)
	if err != nil {
		t.Fatalf("GetMissionDiscoveries failed: %v", err)
	}

	if len(discoveries) != 2 {
		t.Errorf("Expected 2 discoveries, got %d", len(discoveries))
	}

	// Verify IDs are in the result
	foundIDs := make(map[string]bool)
	for _, d := range discoveries {
		foundIDs[d.ID] = true
	}
	if !foundIDs[disc1.ID] || !foundIDs[disc2.ID] {
		t.Errorf("Expected to find disc1 and disc2 in discoveries")
	}
}

func TestGetMissionDiscoveries_TransitiveDiscoveries(t *testing.T) {
	ctx, store := setupConvergenceTestStorage(t)
	defer store.Close()

	// Create mission with transitive discoveries
	// mission → disc1 → disc2
	//        → disc3
	mission := createTestIssue(t, ctx, store, "Mission", types.StatusOpen)
	disc1 := createTestIssue(t, ctx, store, "Discovery 1", types.StatusOpen)
	disc2 := createTestIssue(t, ctx, store, "Discovery 2", types.StatusOpen)
	disc3 := createTestIssue(t, ctx, store, "Discovery 3", types.StatusOpen)

	addDiscoveredFromDep(t, ctx, store, disc1.ID, mission.ID)
	addDiscoveredFromDep(t, ctx, store, disc2.ID, disc1.ID)
	addDiscoveredFromDep(t, ctx, store, disc3.ID, mission.ID)

	// Test: Should return all 3 discoveries
	discoveries, err := GetMissionDiscoveries(ctx, store, mission.ID)
	if err != nil {
		t.Fatalf("GetMissionDiscoveries failed: %v", err)
	}

	if len(discoveries) != 3 {
		t.Errorf("Expected 3 discoveries, got %d", len(discoveries))
	}

	// Verify all IDs are in the result
	foundIDs := make(map[string]bool)
	for _, d := range discoveries {
		foundIDs[d.ID] = true
	}
	if !foundIDs[disc1.ID] || !foundIDs[disc2.ID] || !foundIDs[disc3.ID] {
		t.Errorf("Expected to find all discoveries in result")
	}
}

func TestHasMissionConverged_NoDiscoveries(t *testing.T) {
	ctx, store := setupConvergenceTestStorage(t)
	defer store.Close()

	// Create mission with no discoveries
	mission := createTestIssue(t, ctx, store, "Mission", types.StatusOpen)

	// Test: Should return false (no discoveries to converge)
	converged, err := HasMissionConverged(ctx, store, mission.ID)
	if err != nil {
		t.Fatalf("HasMissionConverged failed: %v", err)
	}

	if converged {
		t.Error("Expected false for mission with no discoveries")
	}
}

func TestHasMissionConverged_AllClosed(t *testing.T) {
	ctx, store := setupConvergenceTestStorage(t)
	defer store.Close()

	// Create mission with 2 closed discoveries
	mission := createTestIssue(t, ctx, store, "Mission", types.StatusOpen)
	disc1 := createTestIssue(t, ctx, store, "Discovery 1", types.StatusClosed)
	disc2 := createTestIssue(t, ctx, store, "Discovery 2", types.StatusClosed)

	addDiscoveredFromDep(t, ctx, store, disc1.ID, mission.ID)
	addDiscoveredFromDep(t, ctx, store, disc2.ID, mission.ID)

	// Test: Should return true (all closed)
	converged, err := HasMissionConverged(ctx, store, mission.ID)
	if err != nil {
		t.Fatalf("HasMissionConverged failed: %v", err)
	}

	if !converged {
		t.Error("Expected true when all discoveries are closed")
	}
}

func TestHasMissionConverged_SomeOpen(t *testing.T) {
	ctx, store := setupConvergenceTestStorage(t)
	defer store.Close()

	// Create mission with 1 closed, 1 open discovery
	mission := createTestIssue(t, ctx, store, "Mission", types.StatusOpen)
	disc1 := createTestIssue(t, ctx, store, "Discovery 1", types.StatusClosed)
	disc2 := createTestIssue(t, ctx, store, "Discovery 2", types.StatusOpen)

	addDiscoveredFromDep(t, ctx, store, disc1.ID, mission.ID)
	addDiscoveredFromDep(t, ctx, store, disc2.ID, mission.ID)

	// Test: Should return false (not all closed)
	converged, err := HasMissionConverged(ctx, store, mission.ID)
	if err != nil {
		t.Fatalf("HasMissionConverged failed: %v", err)
	}

	if converged {
		t.Error("Expected false when some discoveries are open")
	}
}

func TestHasMissionConverged_TransitiveOpen(t *testing.T) {
	ctx, store := setupConvergenceTestStorage(t)
	defer store.Close()

	// Create mission → disc1 (closed) → disc2 (open)
	mission := createTestIssue(t, ctx, store, "Mission", types.StatusOpen)
	disc1 := createTestIssue(t, ctx, store, "Discovery 1", types.StatusClosed)
	disc2 := createTestIssue(t, ctx, store, "Discovery 2", types.StatusOpen)

	addDiscoveredFromDep(t, ctx, store, disc1.ID, mission.ID)
	addDiscoveredFromDep(t, ctx, store, disc2.ID, disc1.ID)

	// Test: Should return false (transitive discovery is open)
	converged, err := HasMissionConverged(ctx, store, mission.ID)
	if err != nil {
		t.Fatalf("HasMissionConverged failed: %v", err)
	}

	if converged {
		t.Error("Expected false when transitive discovery is open")
	}
}

func TestCheckMissionExplosion_BelowThreshold(t *testing.T) {
	ctx, store := setupConvergenceTestStorage(t)
	defer store.Close()

	// Create mission with 10 discoveries (below threshold of 20)
	mission := createTestIssue(t, ctx, store, "Mission", types.StatusOpen)
	for i := 0; i < 10; i++ {
		disc := createTestIssue(t, ctx, store, "Discovery", types.StatusOpen)
		addDiscoveredFromDep(t, ctx, store, disc.ID, mission.ID)
	}

	// Test: Should return false (below threshold)
	exploded, err := CheckMissionExplosion(ctx, store, mission.ID)
	if err != nil {
		t.Fatalf("CheckMissionExplosion failed: %v", err)
	}

	if exploded {
		t.Error("Expected false for mission with 10 discoveries")
	}
}

func TestCheckMissionExplosion_AtThreshold(t *testing.T) {
	ctx, store := setupConvergenceTestStorage(t)
	defer store.Close()

	// Create mission with exactly 20 discoveries (at threshold)
	mission := createTestIssue(t, ctx, store, "Mission", types.StatusOpen)
	for i := 0; i < 20; i++ {
		disc := createTestIssue(t, ctx, store, "Discovery", types.StatusOpen)
		addDiscoveredFromDep(t, ctx, store, disc.ID, mission.ID)
	}

	// Test: Should return false (at threshold, not above)
	exploded, err := CheckMissionExplosion(ctx, store, mission.ID)
	if err != nil {
		t.Fatalf("CheckMissionExplosion failed: %v", err)
	}

	if exploded {
		t.Error("Expected false for mission with exactly 20 discoveries")
	}
}

func TestCheckMissionExplosion_AboveThreshold(t *testing.T) {
	ctx, store := setupConvergenceTestStorage(t)
	defer store.Close()

	// Create mission with 25 discoveries (above threshold)
	mission := createTestIssue(t, ctx, store, "Mission", types.StatusOpen)
	for i := 0; i < 25; i++ {
		disc := createTestIssue(t, ctx, store, "Discovery", types.StatusOpen)
		addDiscoveredFromDep(t, ctx, store, disc.ID, mission.ID)
	}

	// Test: Should return true (above threshold)
	exploded, err := CheckMissionExplosion(ctx, store, mission.ID)
	if err != nil {
		t.Fatalf("CheckMissionExplosion failed: %v", err)
	}

	if !exploded {
		t.Error("Expected true for mission with 25 discoveries")
	}
}

func TestCheckMissionExplosion_TransitiveDiscoveries(t *testing.T) {
	ctx, store := setupConvergenceTestStorage(t)
	defer store.Close()

	// Create mission with 15 direct + 10 transitive discoveries (25 total)
	mission := createTestIssue(t, ctx, store, "Mission", types.StatusOpen)

	// Create 15 direct discoveries
	var directDiscoveries []*types.Issue
	for i := 0; i < 15; i++ {
		disc := createTestIssue(t, ctx, store, "Direct Discovery", types.StatusOpen)
		addDiscoveredFromDep(t, ctx, store, disc.ID, mission.ID)
		directDiscoveries = append(directDiscoveries, disc)
	}

	// Create 10 transitive discoveries from first direct discovery
	for i := 0; i < 10; i++ {
		disc := createTestIssue(t, ctx, store, "Transitive Discovery", types.StatusOpen)
		addDiscoveredFromDep(t, ctx, store, disc.ID, directDiscoveries[0].ID)
	}

	// Test: Should return true (25 total discoveries)
	exploded, err := CheckMissionExplosion(ctx, store, mission.ID)
	if err != nil {
		t.Fatalf("CheckMissionExplosion failed: %v", err)
	}

	if !exploded {
		t.Error("Expected true for mission with 25 total discoveries")
	}
}

// TestMissionThreadIntegration tests a realistic mission thread scenario
// This simulates what happens during actual mission execution with discoveries
func TestMissionThreadIntegration(t *testing.T) {
	ctx, store := setupConvergenceTestStorage(t)
	defer store.Close()

	// Create a realistic mission thread:
	// mission (vc-100)
	//   ├─ bug1 (vc-101) [open]
	//   │   └─ bug1a (vc-102) [closed] - discovered while fixing bug1
	//   ├─ refactor (vc-103) [open]
	//   │   ├─ test1 (vc-104) [closed]
	//   │   └─ test2 (vc-105) [closed]
	//   └─ docs (vc-106) [closed]

	mission := createTestIssue(t, ctx, store, "Implement user authentication", types.StatusOpen)
	bug1 := createTestIssue(t, ctx, store, "Fix password validation bug", types.StatusOpen)
	bug1a := createTestIssue(t, ctx, store, "Fix regex escaping in validator", types.StatusClosed)
	refactor := createTestIssue(t, ctx, store, "Refactor auth middleware", types.StatusOpen)
	test1 := createTestIssue(t, ctx, store, "Add unit tests for middleware", types.StatusClosed)
	test2 := createTestIssue(t, ctx, store, "Add integration tests", types.StatusClosed)
	docs := createTestIssue(t, ctx, store, "Document auth API", types.StatusClosed)

	// Build discovered-from chain
	addDiscoveredFromDep(t, ctx, store, bug1.ID, mission.ID)
	addDiscoveredFromDep(t, ctx, store, bug1a.ID, bug1.ID)
	addDiscoveredFromDep(t, ctx, store, refactor.ID, mission.ID)
	addDiscoveredFromDep(t, ctx, store, test1.ID, refactor.ID)
	addDiscoveredFromDep(t, ctx, store, test2.ID, refactor.ID)
	addDiscoveredFromDep(t, ctx, store, docs.ID, mission.ID)

	// Test 1: GetMissionRoot from deepest node should find mission
	root, err := GetMissionRoot(ctx, store, bug1a.ID)
	if err != nil {
		t.Fatalf("GetMissionRoot failed: %v", err)
	}
	if root.ID != mission.ID {
		t.Errorf("Expected root to be mission %s, got %s", mission.ID, root.ID)
	}

	// Test 2: GetMissionRoot from middle node should find mission
	root, err = GetMissionRoot(ctx, store, test1.ID)
	if err != nil {
		t.Fatalf("GetMissionRoot failed: %v", err)
	}
	if root.ID != mission.ID {
		t.Errorf("Expected root to be mission %s, got %s", mission.ID, root.ID)
	}

	// Test 3: GetMissionDiscoveries should find all 6 discoveries
	discoveries, err := GetMissionDiscoveries(ctx, store, mission.ID)
	if err != nil {
		t.Fatalf("GetMissionDiscoveries failed: %v", err)
	}
	if len(discoveries) != 6 {
		t.Errorf("Expected 6 discoveries, got %d", len(discoveries))
	}

	// Test 4: Mission should not have converged (some issues still open)
	converged, err := HasMissionConverged(ctx, store, mission.ID)
	if err != nil {
		t.Fatalf("HasMissionConverged failed: %v", err)
	}
	if converged {
		t.Error("Expected mission not to have converged (some issues still open)")
	}

	// Test 5: Close all open issues and check convergence
	if err := store.CloseIssue(ctx, bug1.ID, "fixed", "test"); err != nil {
		t.Fatalf("Failed to close bug1: %v", err)
	}
	if err := store.CloseIssue(ctx, refactor.ID, "completed", "test"); err != nil {
		t.Fatalf("Failed to close refactor: %v", err)
	}

	converged, err = HasMissionConverged(ctx, store, mission.ID)
	if err != nil {
		t.Fatalf("HasMissionConverged failed after closing: %v", err)
	}
	if !converged {
		t.Error("Expected mission to have converged (all discoveries closed)")
	}

	// Test 6: Mission should not explode (only 6 discoveries)
	exploded, err := CheckMissionExplosion(ctx, store, mission.ID)
	if err != nil {
		t.Fatalf("CheckMissionExplosion failed: %v", err)
	}
	if exploded {
		t.Error("Expected mission not to explode (only 6 discoveries)")
	}

	// Verify discovery chain integrity
	// Each discovered issue should trace back to the mission root
	allDiscoveredIDs := []string{bug1.ID, bug1a.ID, refactor.ID, test1.ID, test2.ID, docs.ID}
	for _, discID := range allDiscoveredIDs {
		root, err := GetMissionRoot(ctx, store, discID)
		if err != nil {
			t.Fatalf("GetMissionRoot failed for %s: %v", discID, err)
		}
		if root.ID != mission.ID {
			t.Errorf("Expected %s to trace back to mission %s, got %s", discID, mission.ID, root.ID)
		}
	}
}
