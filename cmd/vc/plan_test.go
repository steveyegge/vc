package main

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/steveyegge/vc/internal/iterative"
	"github.com/steveyegge/vc/internal/storage/beads"
	"github.com/steveyegge/vc/internal/types"
)

type testMissionPlanRefiner struct {
	refineFunc           func(plan *types.MissionPlan) (*types.MissionPlan, error)
	checkConvergenceFunc func(call int, current, previous *types.MissionPlan) (*iterative.ConvergenceDecision, error)
	refineCalls          int
}

func (r *testMissionPlanRefiner) Refine(ctx context.Context, artifact *iterative.Artifact) (*iterative.Artifact, error) {
	r.refineCalls++

	var plan types.MissionPlan
	if err := json.Unmarshal([]byte(artifact.Content), &plan); err != nil {
		return nil, err
	}

	nextPlan, err := r.refineFunc(&plan)
	if err != nil {
		return nil, err
	}

	content, err := json.Marshal(nextPlan)
	if err != nil {
		return nil, err
	}

	return &iterative.Artifact{
		Type:    artifact.Type,
		Content: string(content),
		Context: artifact.Context,
	}, nil
}

func (r *testMissionPlanRefiner) CheckConvergence(ctx context.Context, current, previous *iterative.Artifact) (*iterative.ConvergenceDecision, error) {
	var currentPlan types.MissionPlan
	if err := json.Unmarshal([]byte(current.Content), &currentPlan); err != nil {
		return nil, err
	}

	var previousPlan types.MissionPlan
	if err := json.Unmarshal([]byte(previous.Content), &previousPlan); err != nil {
		return nil, err
	}

	return r.checkConvergenceFunc(r.refineCalls, &currentPlan, &previousPlan)
}

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

// TestGenerateShortUUID tests the UUID generation helper (vc-26hh)
func TestGenerateShortUUID(t *testing.T) {
	// Test that UUID is generated
	uuid1 := generateShortUUID()
	if uuid1 == "" {
		t.Error("Expected non-empty UUID")
	}

	// Test that UUID is 8 hex characters
	if len(uuid1) != 8 {
		t.Errorf("Expected UUID length 8, got %d", len(uuid1))
	}

	// Test that UUID is hexadecimal
	for _, c := range uuid1 {
		if !strings.ContainsRune("0123456789abcdef", c) {
			t.Errorf("UUID contains non-hex character: %c", c)
		}
	}

	// Test that subsequent UUIDs are different
	uuid2 := generateShortUUID()
	if uuid1 == uuid2 {
		t.Error("Expected different UUIDs, got same value")
	}

	// Test many UUIDs to ensure randomness
	seen := make(map[string]bool)
	for i := 0; i < 100; i++ {
		uuid := generateShortUUID()
		if seen[uuid] {
			t.Errorf("Duplicate UUID detected: %s", uuid)
		}
		seen[uuid] = true
	}
}

// TestGetStatusColor tests the status color helper (vc-26hh)
func TestGetStatusColor(t *testing.T) {
	tests := []struct {
		status string
		want   string // Expected color name (for debugging)
	}{
		{"draft", "yellow"},
		{"refining", "cyan"},
		{"validated", "green"},
		{"approved", "blue"},
		{"unknown", "white"},
	}

	for _, tt := range tests {
		t.Run(tt.status, func(t *testing.T) {
			colorFn := getStatusColor(tt.status)
			if colorFn == nil {
				t.Error("Expected non-nil color function")
			}

			// Test that it can format a string
			result := colorFn(tt.status)
			if result == "" {
				t.Error("Expected non-empty colored string")
			}
		})
	}
}

func TestRefineMissionPlan(t *testing.T) {
	initialPlan := &types.MissionPlan{
		MissionID: "plan-1234",
		Phases: []types.PlannedPhase{
			{
				PhaseNumber:     1,
				Title:           "Draft phase",
				Description:     "Initial draft",
				Strategy:        "Start simple",
				Tasks:           []string{"Add first task"},
				EstimatedEffort: "1 day",
			},
		},
		Strategy:        "Initial strategy",
		Risks:           []string{"unknowns"},
		EstimatedEffort: "2 days",
		Confidence:      0.55,
		GeneratedAt:     time.Now(),
		GeneratedBy:     "test",
		Status:          "draft",
	}

	refiner := &testMissionPlanRefiner{
		refineFunc: func(plan *types.MissionPlan) (*types.MissionPlan, error) {
			next := *plan
			next.Confidence += 0.1
			next.Status = "refining"
			next.Phases = append([]types.PlannedPhase(nil), plan.Phases...)
			next.Phases[0].Tasks = append([]string{}, plan.Phases[0].Tasks...)
			next.Phases[0].Tasks = append(next.Phases[0].Tasks, fmt.Sprintf("Refinement pass %d", len(plan.Phases[0].Tasks)))
			return &next, nil
		},
		checkConvergenceFunc: func(call int, current, previous *types.MissionPlan) (*iterative.ConvergenceDecision, error) {
			return &iterative.ConvergenceDecision{
				Converged:  call >= 2,
				Confidence: 0.9,
				Reasoning:  "test convergence",
				Strategy:   "test",
			}, nil
		},
	}

	refinedPlan, result, err := refineMissionPlan(
		context.Background(),
		initialPlan,
		refiner,
		iterative.RefinementConfig{
			MinIterations: 1,
			MaxIterations: 3,
		},
		"test refinement",
	)
	if err != nil {
		t.Fatalf("refineMissionPlan failed: %v", err)
	}

	if result == nil {
		t.Fatal("Expected convergence result")
	}
	if !result.Converged {
		t.Error("Expected converged=true")
	}
	if result.Iterations != 2 {
		t.Errorf("Expected 2 refinement passes, got %d", result.Iterations)
	}
	if refinedPlan.Status != "refining" {
		t.Errorf("Expected status refining, got %s", refinedPlan.Status)
	}
	if refinedPlan.Confidence <= initialPlan.Confidence {
		t.Errorf("Expected confidence to improve, got %.2f", refinedPlan.Confidence)
	}
	if got := len(refinedPlan.Phases[0].Tasks); got != 3 {
		t.Errorf("Expected 3 tasks after refinement, got %d", got)
	}
}

func TestRefineMissionPlanRejectsInvalidOutput(t *testing.T) {
	initialPlan := &types.MissionPlan{
		MissionID: "plan-bad",
		Phases: []types.PlannedPhase{
			{
				PhaseNumber:     1,
				Title:           "Draft phase",
				Description:     "Initial draft",
				Strategy:        "Start simple",
				Tasks:           []string{"Add first task"},
				EstimatedEffort: "1 day",
			},
		},
		Strategy:        "Initial strategy",
		Risks:           []string{"unknowns"},
		EstimatedEffort: "2 days",
		Confidence:      0.55,
		GeneratedAt:     time.Now(),
		GeneratedBy:     "test",
		Status:          "draft",
	}

	refiner := &testMissionPlanRefiner{
		refineFunc: func(plan *types.MissionPlan) (*types.MissionPlan, error) {
			return &types.MissionPlan{
				MissionID: plan.MissionID,
				Status:    "refining",
			}, nil
		},
		checkConvergenceFunc: func(call int, current, previous *types.MissionPlan) (*iterative.ConvergenceDecision, error) {
			return &iterative.ConvergenceDecision{
				Converged:  true,
				Confidence: 1.0,
				Reasoning:  "invalid output for test",
				Strategy:   "test",
			}, nil
		},
	}

	_, _, err := refineMissionPlan(
		context.Background(),
		initialPlan,
		refiner,
		iterative.RefinementConfig{
			MinIterations: 1,
			MaxIterations: 1,
		},
		"invalid refinement",
	)
	if err == nil {
		t.Fatal("Expected validation error from invalid refined plan")
	}
	if !strings.Contains(err.Error(), "refined plan failed validation") {
		t.Fatalf("Expected validation error, got: %v", err)
	}
}
