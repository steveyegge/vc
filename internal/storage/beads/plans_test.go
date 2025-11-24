package beads

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/steveyegge/vc/internal/types"
)

// setupTestStorage creates a temporary in-memory storage for testing
func setupTestStorage(t *testing.T) (*VCStorage, func()) {
	t.Helper()
	ctx := context.Background()

	// Create temporary database
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	// Create VC storage
	store, err := NewVCStorage(ctx, dbPath)
	if err != nil {
		t.Fatalf("Failed to create VC storage: %v", err)
	}

	cleanup := func() {
		_ = store.Close()
	}

	return store, cleanup
}

// TestStorePlan_CreateNew tests creating a new plan
func TestStorePlan_CreateNew(t *testing.T) {
	ctx := context.Background()
	store, cleanup := setupTestStorage(t)
	defer cleanup()

	// Create a mission first
	mission := &types.Mission{
		Issue: types.Issue{
			Title:       "Test Mission",
			Description: "Test mission description",
			IssueType:   types.TypeEpic,
			Status:      types.StatusOpen,
			Priority:    1,
		},
		Goal: "Test goal",
	}
	if err := store.CreateMission(ctx, mission, "test"); err != nil {
		t.Fatalf("Failed to create mission: %v", err)
	}

	// Create a plan
	plan := &types.MissionPlan{
		MissionID: mission.ID,
		Phases: []types.PlannedPhase{
			{
				PhaseNumber:     1,
				Title:           "Phase 1",
				Description:     "First phase",
				Strategy:        "Test strategy",
				Tasks:           []string{"task1", "task2"},
				EstimatedEffort: "1 week",
			},
		},
		Strategy:        "Overall strategy",
		Risks:           []string{"risk1"},
		EstimatedEffort: "2 weeks",
		Confidence:      0.85,
		GeneratedAt:     time.Now(),
		GeneratedBy:     "test",
		Status:          "draft",
	}

	// Store with expectedIteration=0 (create new)
	iteration, err := store.StorePlan(ctx, plan, 0)
	if err != nil {
		t.Fatalf("StorePlan failed: %v", err)
	}
	if iteration != 1 {
		t.Errorf("Expected iteration=1, got %d", iteration)
	}

	// Verify we can retrieve it
	retrieved, retrievedIteration, err := store.GetPlan(ctx, mission.ID)
	if err != nil {
		t.Fatalf("GetPlan failed: %v", err)
	}
	if retrieved == nil {
		t.Fatal("GetPlan returned nil")
	}
	if retrievedIteration != 1 {
		t.Errorf("Expected iteration=1, got %d", retrievedIteration)
	}
	if retrieved.MissionID != mission.ID {
		t.Errorf("MissionID mismatch: got %s, want %s", retrieved.MissionID, mission.ID)
	}
	if retrieved.Status != "draft" {
		t.Errorf("Status mismatch: got %s, want draft", retrieved.Status)
	}
}

// TestStorePlan_OptimisticLockingSuccess tests successful update with correct iteration
func TestStorePlan_OptimisticLockingSuccess(t *testing.T) {
	ctx := context.Background()
	store, cleanup := setupTestStorage(t)
	defer cleanup()

	mission := &types.Mission{
		Issue: types.Issue{
			Title:       "Test Mission",
			Description: "Test",
			IssueType:   types.TypeEpic,
			Status:      types.StatusOpen,
			Priority:    1,
		},
		Goal: "Test",
	}
	if err := store.CreateMission(ctx, mission, "test"); err != nil {
		t.Fatalf("Failed to create mission: %v", err)
	}

	// Create initial plan
	plan := &types.MissionPlan{
		MissionID: mission.ID,
		Phases: []types.PlannedPhase{
			{
				PhaseNumber:     1,
				Title:           "Phase 1",
				Description:     "First phase",
				Strategy:        "Strategy",
				Tasks:           []string{"task1"},
				EstimatedEffort: "1 week",
			},
		},
		Strategy:        "Strategy",
		Risks:           []string{},
		EstimatedEffort: "1 week",
		Confidence:      0.8,
		GeneratedAt:     time.Now(),
		GeneratedBy:     "test",
		Status:          "draft",
	}

	iteration1, err := store.StorePlan(ctx, plan, 0)
	if err != nil {
		t.Fatalf("Initial StorePlan failed: %v", err)
	}
	if iteration1 != 1 {
		t.Fatalf("Expected iteration=1, got %d", iteration1)
	}

	// Update with correct iteration
	plan.Strategy = "Updated strategy"
	plan.Status = "refining"
	iteration2, err := store.StorePlan(ctx, plan, 1)
	if err != nil {
		t.Fatalf("Update StorePlan failed: %v", err)
	}
	if iteration2 != 2 {
		t.Errorf("Expected iteration=2, got %d", iteration2)
	}

	// Verify update
	retrieved, retrievedIteration, err := store.GetPlan(ctx, mission.ID)
	if err != nil {
		t.Fatalf("GetPlan failed: %v", err)
	}
	if retrievedIteration != 2 {
		t.Errorf("Expected iteration=2, got %d", retrievedIteration)
	}
	if retrieved.Strategy != "Updated strategy" {
		t.Errorf("Strategy not updated: got %s", retrieved.Strategy)
	}
	if retrieved.Status != "refining" {
		t.Errorf("Status not updated: got %s", retrieved.Status)
	}
}

// TestStorePlan_OptimisticLockingFailure tests that stale iteration is rejected
func TestStorePlan_OptimisticLockingFailure(t *testing.T) {
	ctx := context.Background()
	store, cleanup := setupTestStorage(t)
	defer cleanup()

	mission := &types.Mission{
		Issue: types.Issue{
			Title:       "Test Mission",
			Description: "Test",
			IssueType:   types.TypeEpic,
			Status:      types.StatusOpen,
			Priority:    1,
		},
		Goal: "Test",
	}
	if err := store.CreateMission(ctx, mission, "test"); err != nil {
		t.Fatalf("Failed to create mission: %v", err)
	}

	// Create initial plan
	plan := &types.MissionPlan{
		MissionID: mission.ID,
		Phases: []types.PlannedPhase{
			{
				PhaseNumber:     1,
				Title:           "Phase 1",
				Description:     "First phase",
				Strategy:        "Strategy",
				Tasks:           []string{"task1"},
				EstimatedEffort: "1 week",
			},
		},
		Strategy:        "Strategy",
		Risks:           []string{},
		EstimatedEffort: "1 week",
		Confidence:      0.8,
		GeneratedAt:     time.Now(),
		GeneratedBy:     "test",
		Status:          "draft",
	}

	if _, err := store.StorePlan(ctx, plan, 0); err != nil {
		t.Fatalf("Initial StorePlan failed: %v", err)
	}

	// Simulate concurrent modification: update to iteration 2
	plan.Strategy = "Concurrent update"
	if _, err := store.StorePlan(ctx, plan, 1); err != nil {
		t.Fatalf("Concurrent StorePlan failed: %v", err)
	}

	// Now try to update with stale iteration=1 (should fail)
	plan.Strategy = "Stale update"
	_, err := store.StorePlan(ctx, plan, 1)
	if !errors.Is(err, ErrStaleIteration) {
		t.Errorf("Expected ErrStaleIteration, got: %v", err)
	}

	// Verify the concurrent update was preserved
	retrieved, _, err := store.GetPlan(ctx, mission.ID)
	if err != nil {
		t.Fatalf("GetPlan failed: %v", err)
	}
	if retrieved.Strategy != "Concurrent update" {
		t.Errorf("Expected 'Concurrent update', got %s", retrieved.Strategy)
	}
}

// TestStorePlan_ConcurrentRefinement simulates the exact race condition from vc-un1o
func TestStorePlan_ConcurrentRefinement(t *testing.T) {
	ctx := context.Background()
	store, cleanup := setupTestStorage(t)
	defer cleanup()

	mission := &types.Mission{
		Issue: types.Issue{
			Title:       "Test Mission",
			Description: "Test",
			IssueType:   types.TypeEpic,
			Status:      types.StatusOpen,
			Priority:    1,
		},
		Goal: "Test",
	}
	if err := store.CreateMission(ctx, mission, "test"); err != nil {
		t.Fatalf("Failed to create mission: %v", err)
	}

	// Create initial plan at iteration 5
	plan := &types.MissionPlan{
		MissionID: mission.ID,
		Phases: []types.PlannedPhase{
			{
				PhaseNumber:     1,
				Title:           "Phase 1",
				Description:     "First phase",
				Strategy:        "Strategy",
				Tasks:           []string{"task1"},
				EstimatedEffort: "1 week",
			},
		},
		Strategy:        "Strategy v1",
		Risks:           []string{},
		EstimatedEffort: "1 week",
		Confidence:      0.8,
		GeneratedAt:     time.Now(),
		GeneratedBy:     "test",
		Status:          "draft",
	}

	// Bootstrap to iteration 5
	for i := 0; i < 5; i++ {
		_, err := store.StorePlan(ctx, plan, i)
		if err != nil {
			t.Fatalf("Bootstrap failed at iteration %d: %v", i, err)
		}
	}

	// Simulate two concurrent refinement processes
	var wg sync.WaitGroup
	var userASuccess, userBSuccess bool
	var userAErr, userBErr error
	var userAIteration, userBIteration int

	// User A refines based on iteration 5
	wg.Add(1)
	go func() {
		defer wg.Done()
		planA := *plan
		planA.Strategy = "User A refinement"
		userAIteration, userAErr = store.StorePlan(ctx, &planA, 5)
		userASuccess = (userAErr == nil)
	}()

	// User B refines based on iteration 5
	wg.Add(1)
	go func() {
		defer wg.Done()
		planB := *plan
		planB.Strategy = "User B refinement"
		userBIteration, userBErr = store.StorePlan(ctx, &planB, 5)
		userBSuccess = (userBErr == nil)
	}()

	wg.Wait()

	// CRITICAL: Exactly ONE should succeed, ONE should fail with ErrStaleIteration
	if userASuccess == userBSuccess {
		t.Fatalf("Race condition not handled correctly: userA success=%v (iter=%d, err=%v), userB success=%v (iter=%d, err=%v)",
			userASuccess, userAIteration, userAErr, userBSuccess, userBIteration, userBErr)
	}

	// Verify the error is ErrStaleIteration
	if userASuccess && !errors.Is(userBErr, ErrStaleIteration) {
		t.Errorf("Expected ErrStaleIteration from user B, got: %v", userBErr)
	}
	if userBSuccess && !errors.Is(userAErr, ErrStaleIteration) {
		t.Errorf("Expected ErrStaleIteration from user A, got: %v", userAErr)
	}

	// Verify final state: exactly one refinement was saved
	retrieved, finalIteration, err := store.GetPlan(ctx, mission.ID)
	if err != nil {
		t.Fatalf("GetPlan failed: %v", err)
	}
	if finalIteration != 6 {
		t.Errorf("Expected final iteration=6, got %d", finalIteration)
	}

	// Verify the saved refinement is from the successful user
	expectedStrategy := "User A refinement"
	if userBSuccess {
		expectedStrategy = "User B refinement"
	}
	if retrieved.Strategy != expectedStrategy {
		t.Errorf("Expected strategy %q, got %q", expectedStrategy, retrieved.Strategy)
	}
}

// TestDeletePlan tests plan deletion
func TestDeletePlan(t *testing.T) {
	ctx := context.Background()
	store, cleanup := setupTestStorage(t)
	defer cleanup()

	mission := &types.Mission{
		Issue: types.Issue{
			Title:       "Test Mission",
			Description: "Test",
			IssueType:   types.TypeEpic,
			Status:      types.StatusOpen,
			Priority:    1,
		},
		Goal: "Test",
	}
	if err := store.CreateMission(ctx, mission, "test"); err != nil {
		t.Fatalf("Failed to create mission: %v", err)
	}

	// Create a plan
	plan := &types.MissionPlan{
		MissionID: mission.ID,
		Phases: []types.PlannedPhase{
			{
				PhaseNumber:     1,
				Title:           "Phase 1",
				Description:     "First phase",
				Strategy:        "Strategy",
				Tasks:           []string{"task1"},
				EstimatedEffort: "1 week",
			},
		},
		Strategy:        "Strategy",
		Risks:           []string{},
		EstimatedEffort: "1 week",
		Confidence:      0.8,
		GeneratedAt:     time.Now(),
		GeneratedBy:     "test",
		Status:          "draft",
	}
	if _, err := store.StorePlan(ctx, plan, 0); err != nil {
		t.Fatalf("StorePlan failed: %v", err)
	}

	// Delete the plan
	if err := store.DeletePlan(ctx, mission.ID); err != nil {
		t.Fatalf("DeletePlan failed: %v", err)
	}

	// Verify it's gone
	retrieved, _, err := store.GetPlan(ctx, mission.ID)
	if err != nil {
		t.Fatalf("GetPlan failed: %v", err)
	}
	if retrieved != nil {
		t.Errorf("Expected nil after deletion, got: %+v", retrieved)
	}
}

// TestListDraftPlans tests listing non-approved plans
func TestListDraftPlans(t *testing.T) {
	ctx := context.Background()
	store, cleanup := setupTestStorage(t)
	defer cleanup()

	// Create missions and plans
	statuses := []string{"draft", "refining", "validated", "approved"}
	missionIDs := make([]string, len(statuses))

	for i, status := range statuses {
		mission := &types.Mission{
			Issue: types.Issue{
				Title:       "Mission " + status,
				Description: "Test",
				IssueType:   types.TypeEpic,
				Status:      types.StatusOpen,
				Priority:    1,
			},
			Goal: "Test",
		}
		if err := store.CreateMission(ctx, mission, "test"); err != nil {
			t.Fatalf("Failed to create mission %d: %v", i, err)
		}
		missionIDs[i] = mission.ID

		plan := &types.MissionPlan{
			MissionID: mission.ID,
			Phases: []types.PlannedPhase{
				{
					PhaseNumber:     1,
					Title:           "Phase 1",
					Description:     "First phase",
					Strategy:        "Strategy",
					Tasks:           []string{"task1"},
					EstimatedEffort: "1 week",
				},
			},
			Strategy:        "Strategy",
			Risks:           []string{},
			EstimatedEffort: "1 week",
			Confidence:      0.8,
			GeneratedAt:     time.Now(),
			GeneratedBy:     "test",
			Status:          status,
		}
		if _, err := store.StorePlan(ctx, plan, 0); err != nil {
			t.Fatalf("StorePlan failed for status %s: %v", status, err)
		}
	}

	// List draft plans (should exclude 'approved')
	draftPlans, err := store.ListDraftPlans(ctx)
	if err != nil {
		t.Fatalf("ListDraftPlans failed: %v", err)
	}

	// Should have 3 plans (draft, refining, validated) but not approved
	if len(draftPlans) != 3 {
		t.Errorf("Expected 3 draft plans, got %d", len(draftPlans))
	}

	// Verify none are approved
	for _, plan := range draftPlans {
		if plan.Status == "approved" {
			t.Errorf("ListDraftPlans returned approved plan: %s", plan.MissionID)
		}
	}
}

// TestStorePlan_TransactionRollback tests that failed operations don't corrupt data
func TestStorePlan_TransactionRollback(t *testing.T) {
	ctx := context.Background()
	store, cleanup := setupTestStorage(t)
	defer cleanup()

	mission := &types.Mission{
		Issue: types.Issue{
			Title:       "Test Mission",
			Description: "Test",
			IssueType:   types.TypeEpic,
			Status:      types.StatusOpen,
			Priority:    1,
		},
		Goal: "Test",
	}
	if err := store.CreateMission(ctx, mission, "test"); err != nil {
		t.Fatalf("Failed to create mission: %v", err)
	}

	// Create valid initial plan
	validPlan := &types.MissionPlan{
		MissionID: mission.ID,
		Phases: []types.PlannedPhase{
			{
				PhaseNumber:     1,
				Title:           "Phase 1",
				Description:     "First phase",
				Strategy:        "Strategy",
				Tasks:           []string{"task1"},
				EstimatedEffort: "1 week",
			},
		},
		Strategy:        "Original strategy",
		Risks:           []string{},
		EstimatedEffort: "1 week",
		Confidence:      0.8,
		GeneratedAt:     time.Now(),
		GeneratedBy:     "test",
		Status:          "draft",
	}

	if _, err := store.StorePlan(ctx, validPlan, 0); err != nil {
		t.Fatalf("Initial StorePlan failed: %v", err)
	}

	// Try to store invalid plan with stale iteration (should fail, transaction should rollback)
	invalidPlan := *validPlan
	invalidPlan.Strategy = "Should not be saved"
	_, err := store.StorePlan(ctx, &invalidPlan, 999) // Wrong iteration
	if !errors.Is(err, ErrStaleIteration) {
		t.Errorf("Expected ErrStaleIteration, got: %v", err)
	}

	// Verify original plan is unchanged
	retrieved, _, err := store.GetPlan(ctx, mission.ID)
	if err != nil {
		t.Fatalf("GetPlan failed: %v", err)
	}
	if retrieved.Strategy != "Original strategy" {
		t.Errorf("Plan was corrupted: got strategy %q", retrieved.Strategy)
	}
}
