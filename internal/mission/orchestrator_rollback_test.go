package mission

import (
	"context"
	"testing"
	"time"

	"github.com/steveyegge/vc/internal/types"
)

// TestCreatePhasesFromPlan_Rollback tests that phase creation rolls back on failure
func TestCreatePhasesFromPlan_Rollback(t *testing.T) {
	ctx := context.Background()

	// Create mock storage that will fail on the 3rd issue
	store := NewMockStorage()
	store.failOnIssueID = "test-3" // Fail when creating 3rd phase

	// Add mission to store (required for priority inheritance)
	mission := &types.Issue{
		ID:        "mission-1",
		Title:     "Test Mission",
		IssueType: types.TypeEpic,
		Status:    types.StatusOpen,
		Priority:  0,
	}
	store.issues["mission-1"] = mission

	planner := &MockPlanner{}
	orchestrator, err := NewOrchestrator(&Config{
		Store:   store,
		Planner: planner,
	})
	if err != nil {
		t.Fatalf("Failed to create orchestrator: %v", err)
	}

	// Create a plan with 3 phases - 3rd will fail
	now := time.Now()
	plan := &types.MissionPlan{
		MissionID: "mission-1",
		Phases: []types.PlannedPhase{
			{
				PhaseNumber:     1,
				Title:           "Phase 1",
				Description:     "First phase",
				Strategy:        "Strategy 1",
				Tasks:           []string{"Task 1"},
				EstimatedEffort: "1 week",
			},
			{
				PhaseNumber:     2,
				Title:           "Phase 2",
				Description:     "Second phase",
				Strategy:        "Strategy 2",
				Tasks:           []string{"Task 2"},
				EstimatedEffort: "1 week",
			},
			{
				PhaseNumber:     3,
				Title:           "Phase 3",
				Description:     "Third phase (will fail)",
				Strategy:        "Strategy 3",
				Tasks:           []string{"Task 3"},
				EstimatedEffort: "1 week",
			},
		},
		Strategy:        "Phased approach",
		EstimatedEffort: "3 weeks",
		Confidence:      0.8,
		GeneratedAt:     now,
	}

	// Attempt to create phases - should fail on phase 3
	phaseIDs, err := orchestrator.CreatePhasesFromPlan(ctx, "mission-1", plan, "test-user")

	// Verify creation failed
	if err == nil {
		t.Fatal("Expected error when phase 3 creation fails, got nil")
	}

	// Verify no phase IDs returned
	if len(phaseIDs) != 0 {
		t.Errorf("Expected 0 phaseIDs on rollback, got %d: %v", len(phaseIDs), phaseIDs)
	}

	// Verify previously created phases were closed (rollback)
	if len(store.closedIssues) != 2 {
		t.Errorf("Expected 2 phases to be closed during rollback, got %d: %v", len(store.closedIssues), store.closedIssues)
	}

	// Verify no open phases remain (only mission should be left)
	if len(store.issues) != 1 {
		t.Errorf("Expected 1 issue after rollback (mission only), got %d", len(store.issues))
	}
}

// TestCreatePhasesFromPlan_RollbackOnDependencyFailure tests rollback when dependency creation fails
func TestCreatePhasesFromPlan_RollbackOnDependencyFailure(t *testing.T) {
	ctx := context.Background()

	// Create mock storage that will fail on 3rd AddDependency call
	store := NewMockStorage()
	store.failOnDepCount = 3 // Fail after 3 dependency calls

	// Add mission to store (required for priority inheritance)
	mission := &types.Issue{
		ID:        "mission-1",
		Title:     "Test Mission",
		IssueType: types.TypeEpic,
		Status:    types.StatusOpen,
		Priority:  0,
	}
	store.issues["mission-1"] = mission

	planner := &MockPlanner{}
	orchestrator, err := NewOrchestrator(&Config{
		Store:   store,
		Planner: planner,
	})
	if err != nil {
		t.Fatalf("Failed to create orchestrator: %v", err)
	}

	// Create a plan with 2 phases
	plan := &types.MissionPlan{
		MissionID: "mission-1",
		Phases: []types.PlannedPhase{
			{
				PhaseNumber:     1,
				Title:           "Phase 1",
				Description:     "First phase",
				Strategy:        "Strategy 1",
				Tasks:           []string{"Task 1"},
				EstimatedEffort: "1 week",
			},
			{
				PhaseNumber:     2,
				Title:           "Phase 2",
				Description:     "Second phase",
				Strategy:        "Strategy 2",
				Tasks:           []string{"Task 2"},
				Dependencies:    []int{1},
				EstimatedEffort: "1 week",
			},
		},
		Strategy:        "Phased approach",
		EstimatedEffort: "2 weeks",
		Confidence:      0.8,
		GeneratedAt:     time.Now(),
	}

	// Attempt to create phases - should fail when adding dependency
	phaseIDs, err := orchestrator.CreatePhasesFromPlan(ctx, "mission-1", plan, "test-user")

	// Verify creation failed
	if err == nil {
		t.Fatal("Expected error when dependency creation fails, got nil")
	}

	// Verify no phase IDs returned
	if len(phaseIDs) != 0 {
		t.Errorf("Expected 0 phaseIDs on rollback, got %d", len(phaseIDs))
	}

	// Verify created phases were closed (rollback)
	if len(store.closedIssues) == 0 {
		t.Error("Expected phases to be closed during rollback, got 0")
	}
}
