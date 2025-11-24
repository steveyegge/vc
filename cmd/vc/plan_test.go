package main

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/steveyegge/vc/internal/storage/beads"
	"github.com/steveyegge/vc/internal/types"
)

// TestPlanShowCommand tests that the plan show command can display a plan (vc-25zn)
func TestPlanShowCommand(t *testing.T) {
	// Create temporary database
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	ctx := context.Background()
	testStore, err := beads.NewVCStorage(ctx, dbPath)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer testStore.Close()

	// Create a test mission
	mission := &types.Mission{
		Issue: types.Issue{
			Title:       "Test Mission",
			Description: "Test mission for plan",
			IssueType:   types.TypeEpic,
			Status:      types.StatusOpen,
			Priority:    1,
		},
		Goal: "Test mission goal",
	}
	if err := testStore.CreateMission(ctx, mission, "test"); err != nil {
		t.Fatalf("Failed to create mission: %v", err)
	}

	// Create a test plan
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
			{
				PhaseNumber:     2,
				Title:           "Phase 2",
				Description:     "Second phase",
				Strategy:        "Another strategy",
				Tasks:           []string{"task3"},
				EstimatedEffort: "3 days",
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

	iteration, err := testStore.StorePlan(ctx, plan, 0)
	if err != nil {
		t.Fatalf("Failed to store plan: %v", err)
	}
	if iteration != 1 {
		t.Errorf("Expected iteration=1, got %d", iteration)
	}

	// Verify we can retrieve the plan
	retrieved, retrievedIteration, err := testStore.GetPlan(ctx, mission.ID)
	if err != nil {
		t.Fatalf("Failed to get plan: %v", err)
	}
	if retrieved == nil {
		t.Fatal("GetPlan returned nil")
	}
	if retrievedIteration != 1 {
		t.Errorf("Expected iteration=1, got %d", retrievedIteration)
	}
	if len(retrieved.Phases) != 2 {
		t.Errorf("Expected 2 phases, got %d", len(retrieved.Phases))
	}
}

// TestPlanListCommand tests that the plan list command can list draft plans (vc-25zn)
func TestPlanListCommand(t *testing.T) {
	// Create temporary database
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	ctx := context.Background()
	testStore, err := beads.NewVCStorage(ctx, dbPath)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer testStore.Close()

	// Create test missions with different statuses
	statuses := []string{"draft", "refining", "validated"}
	for _, status := range statuses {
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
		if err := testStore.CreateMission(ctx, mission, "test"); err != nil {
			t.Fatalf("Failed to create mission: %v", err)
		}

		plan := &types.MissionPlan{
			MissionID: mission.ID,
			Phases: []types.PlannedPhase{
				{
					PhaseNumber:     1,
					Title:           "Phase 1",
					Description:     "Test phase",
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
		if _, err := testStore.StorePlan(ctx, plan, 0); err != nil {
			t.Fatalf("Failed to store plan: %v", err)
		}
	}

	// List draft plans
	plans, err := testStore.ListDraftPlans(ctx)
	if err != nil {
		t.Fatalf("Failed to list draft plans: %v", err)
	}

	if len(plans) != 3 {
		t.Errorf("Expected 3 draft plans, got %d", len(plans))
	}

	// Verify all are non-approved
	for _, plan := range plans {
		if plan.Status == "approved" {
			t.Errorf("ListDraftPlans returned approved plan: %s", plan.MissionID)
		}
	}
}
