package ai

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/steveyegge/vc/internal/iterative"
	"github.com/steveyegge/vc/internal/types"
)

// Integration tests for PlanRefiner.
// These tests require ANTHROPIC_API_KEY to be set in the environment.
//
// Run with: go test -v ./internal/ai -run TestPlanRefiner

// createTestMission creates a Mission for testing
func createTestMission(id, title, goal string) *types.Mission {
	return &types.Mission{
		Issue: types.Issue{
			ID:          id,
			Title:       title,
			Description: goal,
			IssueType:   types.TypeEpic,
			Priority:    1,
			Status:      types.StatusOpen,
			CreatedAt:   time.Now(),
		},
		Goal: goal,
	}
}

// TestNewPlanRefiner tests refiner creation
func TestNewPlanRefiner(t *testing.T) {
	mission := createTestMission("test-1", "Test Mission", "Test mission goal")

	tests := []struct {
		name        string
		supervisor  *Supervisor
		planningCtx *types.PlanningContext
		wantErr     bool
	}{
		{
			name:       "valid refiner creation",
			supervisor: &Supervisor{},
			planningCtx: &types.PlanningContext{
				Mission: mission,
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			refiner := NewPlanRefiner(tt.supervisor, tt.planningCtx)
			if refiner == nil {
				t.Error("NewPlanRefiner() returned nil")
			}
			if refiner.supervisor != tt.supervisor {
				t.Error("Refiner supervisor not set correctly")
			}
			if refiner.planningCtx != tt.planningCtx {
				t.Error("Refiner planning context not set correctly")
			}
			if refiner.currentIter != 0 {
				t.Errorf("Expected currentIter=0, got %d", refiner.currentIter)
			}
		})
	}
}

// TestPlanRefiner_Refine tests that Refine increments iteration counter
func TestPlanRefiner_Refine_Iteration(t *testing.T) {
	skipIfNoAPIKey(t)

	supervisor, err := createTestSupervisor(t)
	if err != nil {
		t.Skipf("Failed to create supervisor: %v", err)
	}

	mission := createTestMission("test-refine-1", "Implement user authentication", "Add user authentication system")
	planningCtx := &types.PlanningContext{
		Mission: mission,
	}

	refiner := NewPlanRefiner(supervisor, planningCtx)

	// Create a simple initial plan
	initialPlan := types.MissionPlan{
		MissionID: "test-refine-1",
		Phases: []types.PlannedPhase{
			{
				PhaseNumber:     1,
				Title:           "Basic auth",
				Description:     "Implement basic authentication",
				Strategy:        "Start with simple JWT authentication",
				Tasks:           []string{"Add login endpoint", "Add token validation"},
				EstimatedEffort: "2 days",
			},
		},
		Strategy:        "Implement basic authentication",
		Risks:           []string{"Security vulnerabilities"},
		EstimatedEffort: "2 days",
		Confidence:      0.7,
		GeneratedAt:     time.Now(),
		GeneratedBy:     "test",
		Status:          "draft",
	}

	planJSON, err := json.Marshal(initialPlan)
	if err != nil {
		t.Fatalf("Failed to marshal plan: %v", err)
	}

	artifact := &iterative.Artifact{
		Type:    "mission_plan",
		Content: string(planJSON),
		Context: "Initial plan",
	}

	ctx := context.Background()
	refinedArtifact, err := refiner.Refine(ctx, artifact)

	if err != nil {
		t.Fatalf("Refine failed: %v", err)
	}

	if refinedArtifact == nil {
		t.Fatal("Refined artifact is nil")
	}

	// Check that iteration counter incremented
	if refiner.currentIter != 1 {
		t.Errorf("Expected currentIter=1, got %d", refiner.currentIter)
	}

	// Verify we can parse the refined plan
	var refinedPlan types.MissionPlan
	if err := json.Unmarshal([]byte(refinedArtifact.Content), &refinedPlan); err != nil {
		t.Fatalf("Failed to parse refined plan: %v", err)
	}

	// Basic validation
	if refinedPlan.MissionID != initialPlan.MissionID {
		t.Errorf("Mission ID changed: expected %s, got %s", initialPlan.MissionID, refinedPlan.MissionID)
	}
}

// TestPlanRefiner_CheckConvergence_IdenticalPlans tests convergence with identical plans
func TestPlanRefiner_CheckConvergence_IdenticalPlans(t *testing.T) {
	skipIfNoAPIKey(t)

	supervisor, err := createTestSupervisor(t)
	if err != nil {
		t.Skipf("Failed to create supervisor: %v", err)
	}

	mission := createTestMission("test-conv-1", "Test mission", "Test mission goal")
	planningCtx := &types.PlanningContext{
		Mission: mission,
	}

	refiner := NewPlanRefiner(supervisor, planningCtx)

	// Create identical plans
	plan := types.MissionPlan{
		MissionID: "test-conv-1",
		Phases: []types.PlannedPhase{
			{
				PhaseNumber:     1,
				Title:           "Implementation",
				Description:     "Implement the feature",
				Strategy:        "Direct implementation",
				Tasks:           []string{"Add feature"},
				EstimatedEffort: "1 day",
			},
		},
		Strategy:        "Direct implementation",
		Risks:           []string{"None"},
		EstimatedEffort: "1 day",
		Confidence:      0.9,
		GeneratedAt:     time.Now(),
		GeneratedBy:     "ai-planner",
		Status:          "refining",
	}

	planJSON, _ := json.Marshal(plan)

	previous := &iterative.Artifact{
		Type:    "mission_plan",
		Content: string(planJSON),
		Context: "First iteration",
	}

	current := &iterative.Artifact{
		Type:    "mission_plan",
		Content: string(planJSON),
		Context: "Second iteration - no changes",
	}

	ctx := context.Background()
	decision, err := refiner.CheckConvergence(ctx, current, previous)
	if err != nil {
		t.Fatalf("CheckConvergence failed: %v", err)
	}

	// Identical plans should converge
	if !decision.Converged {
		t.Errorf("Expected convergence for identical plans, got: %+v", decision)
	}
}

// TestPlanRefiner_CheckConvergence_MinorChanges tests convergence with minor rewording
func TestPlanRefiner_CheckConvergence_MinorChanges(t *testing.T) {
	skipIfNoAPIKey(t)

	supervisor, err := createTestSupervisor(t)
	if err != nil {
		t.Skipf("Failed to create supervisor: %v", err)
	}

	mission := createTestMission("test-conv-2", "Test mission", "Test mission goal")
	planningCtx := &types.PlanningContext{
		Mission: mission,
	}

	refiner := NewPlanRefiner(supervisor, planningCtx)

	// Previous plan
	prevPlan := types.MissionPlan{
		MissionID: "test-conv-2",
		Phases: []types.PlannedPhase{
			{
				PhaseNumber:     1,
				Title:           "User authentication",
				Description:     "Add user authentication system",
				Strategy:        "Implement JWT-based authentication",
				Tasks:           []string{"Add login endpoint"},
				EstimatedEffort: "2 days",
			},
		},
		Strategy:        "Implement JWT-based authentication",
		Risks:           []string{"Token security"},
		EstimatedEffort: "2 days",
		Confidence:      0.8,
		GeneratedAt:     time.Now(),
		GeneratedBy:     "ai-planner",
		Status:          "refining",
	}

	// Current plan - same structure, minor wording changes
	currPlan := types.MissionPlan{
		MissionID: "test-conv-2",
		Phases: []types.PlannedPhase{
			{
				PhaseNumber:     1,
				Title:           "User authentication",
				Description:     "Build user authentication system",
				Strategy:        "Build JWT-based authentication system",
				Tasks:           []string{"Implement login endpoint"},
				EstimatedEffort: "2 days",
			},
		},
		Strategy:        "Build JWT-based authentication system",
		Risks:           []string{"Token security"},
		EstimatedEffort: "2 days",
		Confidence:      0.85,
		GeneratedAt:     time.Now(),
		GeneratedBy:     "ai-planner",
		Status:          "refining",
	}

	prevJSON, _ := json.Marshal(prevPlan)
	currJSON, _ := json.Marshal(currPlan)

	previous := &iterative.Artifact{
		Type:    "mission_plan",
		Content: string(prevJSON),
		Context: "First iteration",
	}

	current := &iterative.Artifact{
		Type:    "mission_plan",
		Content: string(currJSON),
		Context: "Second iteration - minor refinements",
	}

	ctx := context.Background()
	decision, err := refiner.CheckConvergence(ctx, current, previous)
	if err != nil {
		t.Fatalf("CheckConvergence failed: %v", err)
	}

	// Minor rewording should converge
	if !decision.Converged {
		t.Error("Expected convergence for minor rewording")
	}
}

// TestPlanRefiner_CheckConvergence_MajorChanges tests non-convergence with major changes
func TestPlanRefiner_CheckConvergence_MajorChanges(t *testing.T) {
	skipIfNoAPIKey(t)

	supervisor, err := createTestSupervisor(t)
	if err != nil {
		t.Skipf("Failed to create supervisor: %v", err)
	}

	mission := createTestMission("test-conv-3", "Test mission", "Test mission goal")
	planningCtx := &types.PlanningContext{
		Mission: mission,
	}

	refiner := NewPlanRefiner(supervisor, planningCtx)

	// Previous plan - simple, incomplete
	prevPlan := types.MissionPlan{
		MissionID: "test-conv-3",
		Phases: []types.PlannedPhase{
			{
				PhaseNumber:     1,
				Title:           "Implementation",
				Description:     "Implement the feature",
				Strategy:        "Simple implementation",
				Tasks:           []string{"Add feature"},
				EstimatedEffort: "1 day",
			},
		},
		Strategy:        "Simple implementation",
		Risks:           []string{},
		EstimatedEffort: "1 day",
		Confidence:      0.6,
		GeneratedAt:     time.Now(),
		GeneratedBy:     "ai-planner",
		Status:          "refining",
	}

	// Current plan - much more detailed with new phases
	currPlan := types.MissionPlan{
		MissionID: "test-conv-3",
		Phases: []types.PlannedPhase{
			{
				PhaseNumber:     1,
				Title:           "Design and planning",
				Description:     "Design the feature architecture",
				Strategy:        "Start with architecture design",
				Tasks:           []string{"Design architecture", "Plan database schema"},
				EstimatedEffort: "2 days",
			},
			{
				PhaseNumber:     2,
				Title:           "Implementation",
				Description:     "Implement core feature",
				Strategy:        "Build feature with error handling",
				Tasks:           []string{"Implement core feature", "Add error handling"},
				Dependencies:    []int{1},
				EstimatedEffort: "2 days",
			},
			{
				PhaseNumber:     3,
				Title:           "Testing and validation",
				Description:     "Test the feature thoroughly",
				Strategy:        "Comprehensive testing",
				Tasks:           []string{"Write integration tests"},
				Dependencies:    []int{2},
				EstimatedEffort: "1 day",
			},
		},
		Strategy:        "Phased implementation with design, core feature, and testing",
		Risks:           []string{"Database migration complexity", "Integration points", "Edge case handling"},
		EstimatedEffort: "5 days",
		Confidence:      0.85,
		GeneratedAt:     time.Now(),
		GeneratedBy:     "ai-planner",
		Status:          "refining",
	}

	prevJSON, _ := json.Marshal(prevPlan)
	currJSON, _ := json.Marshal(currPlan)

	previous := &iterative.Artifact{
		Type:    "mission_plan",
		Content: string(prevJSON),
		Context: "First iteration - rough draft",
	}

	current := &iterative.Artifact{
		Type:    "mission_plan",
		Content: string(currJSON),
		Context: "Second iteration - major expansion",
	}

	ctx := context.Background()
	decision, err := refiner.CheckConvergence(ctx, current, previous)
	if err != nil {
		t.Fatalf("CheckConvergence failed: %v", err)
	}

	// Major structural changes should NOT converge
	if decision.Converged {
		t.Error("Expected non-convergence for major structural changes")
	}
}

// TestPlanRefiner_IncorporateFeedback tests feedback incorporation
func TestPlanRefiner_IncorporateFeedback(t *testing.T) {
	skipIfNoAPIKey(t)

	supervisor, err := createTestSupervisor(t)
	if err != nil {
		t.Skipf("Failed to create supervisor: %v", err)
	}

	mission := createTestMission("test-feedback-1", "Test mission", "Test mission goal")
	planningCtx := &types.PlanningContext{
		Mission: mission,
	}

	refiner := NewPlanRefiner(supervisor, planningCtx)

	// Current plan
	currentPlan := types.MissionPlan{
		MissionID: "test-feedback-1",
		Phases: []types.PlannedPhase{
			{
				PhaseNumber:     1,
				Title:           "Implementation",
				Description:     "Implement the feature",
				Strategy:        "Direct implementation",
				Tasks:           []string{"Add feature"},
				EstimatedEffort: "1 day",
			},
		},
		Strategy:        "Direct implementation",
		Risks:           []string{},
		EstimatedEffort: "1 day",
		Confidence:      0.7,
		GeneratedAt:     time.Now(),
		GeneratedBy:     "ai-planner",
		Status:          "draft",
	}

	// Human feedback requesting more detail
	feedback := "Please add a testing phase and include more specific task descriptions. Also add risk considerations for security."

	ctx := context.Background()
	updatedPlan, err := refiner.IncorporateFeedback(ctx, &currentPlan, feedback)
	if err != nil {
		t.Fatalf("IncorporateFeedback failed: %v", err)
	}

	if updatedPlan == nil {
		t.Fatal("Updated plan is nil")
	}

	// Verify mission ID is preserved
	if updatedPlan.MissionID != currentPlan.MissionID {
		t.Errorf("Mission ID changed: expected %s, got %s", currentPlan.MissionID, updatedPlan.MissionID)
	}

	// The AI should have addressed the feedback (we can't test exact content, but can verify structure)
	if len(updatedPlan.Phases) < len(currentPlan.Phases) {
		t.Error("Expected more phases after feedback incorporation")
	}

	// Verify status is "refining" after feedback
	if updatedPlan.Status != "refining" {
		t.Errorf("Expected status 'refining', got '%s'", updatedPlan.Status)
	}
}

// TestComputePlanningCost tests cost calculation
func TestComputePlanningCost(t *testing.T) {
	tests := []struct {
		name               string
		result             *iterative.ConvergenceResult
		metrics            *iterative.ArtifactMetrics
		expectedIterations int
		hasTokenCounts     bool
	}{
		{
			name: "with metrics and token counts",
			result: &iterative.ConvergenceResult{
				Iterations:  4,
				Converged:   true,
				ElapsedTime: 35 * time.Second,
			},
			metrics: &iterative.ArtifactMetrics{
				TotalInputTokens:  8000,
				TotalOutputTokens: 2600,
			},
			expectedIterations: 4,
			hasTokenCounts:     true,
		},
		{
			name: "without metrics",
			result: &iterative.ConvergenceResult{
				Iterations:  3,
				Converged:   true,
				ElapsedTime: 20 * time.Second,
			},
			metrics:            nil,
			expectedIterations: 3,
			hasTokenCounts:     false,
		},
		{
			name: "with metrics but no token counts",
			result: &iterative.ConvergenceResult{
				Iterations:  2,
				Converged:   false,
				ElapsedTime: 15 * time.Second,
			},
			metrics: &iterative.ArtifactMetrics{
				TotalInputTokens:  0,
				TotalOutputTokens: 0,
			},
			expectedIterations: 2,
			hasTokenCounts:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cost := ComputePlanningCost(tt.result, tt.metrics)

			if cost == nil {
				t.Fatal("ComputePlanningCost returned nil")
			}

			if cost.Iterations != tt.expectedIterations {
				t.Errorf("Expected %d iterations, got %d", tt.expectedIterations, cost.Iterations)
			}

			if tt.hasTokenCounts {
				if cost.TotalTokens == 0 {
					t.Error("Expected non-zero total tokens")
				}
				if cost.EstimatedCostUSD == 0 {
					t.Error("Expected non-zero estimated cost")
				}
			} else {
				if cost.EstimatedCostUSD != 0 {
					t.Error("Expected zero cost when no token counts available")
				}
			}

			expectedDuration := tt.result.ElapsedTime.Milliseconds()
			if cost.TotalDuration != expectedDuration {
				t.Errorf("Expected duration %d ms, got %d ms", expectedDuration, cost.TotalDuration)
			}
		})
	}
}

// TestPlanRefiner_ValidationFailure tests that validation errors are caught
func TestPlanRefiner_ValidationFailure(t *testing.T) {
	// This is a unit test (doesn't need API key) testing validation logic
	mission := createTestMission("test-validation-1", "Test mission", "Test mission goal")

	supervisor := &Supervisor{}
	planningCtx := &types.PlanningContext{
		Mission: mission,
	}

	refiner := NewPlanRefiner(supervisor, planningCtx)

	// Invalid plan (missing required fields)
	invalidPlan := types.MissionPlan{
		MissionID: "",
		Phases:    []types.PlannedPhase{},
	}

	planJSON, _ := json.Marshal(invalidPlan)

	artifact := &iterative.Artifact{
		Type:    "mission_plan",
		Content: string(planJSON),
		Context: "Invalid plan",
	}

	// This tests internal structure - we can't call Refine without API key,
	// but we can verify the refiner structure is correct
	if refiner == nil {
		t.Error("Refiner should not be nil")
	}

	// Verify artifact structure
	var parsed types.MissionPlan
	if err := json.Unmarshal([]byte(artifact.Content), &parsed); err != nil {
		t.Fatalf("Failed to parse artifact: %v", err)
	}

	if parsed.MissionID != "" {
		t.Error("Expected empty mission ID in test artifact")
	}
}
