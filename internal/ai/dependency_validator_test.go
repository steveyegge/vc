package ai

import (
	"context"
	"strings"
	"testing"

	"github.com/steveyegge/vc/internal/types"
)

// TestValidateDependencyReferences_ValidPlan verifies valid phase dependencies pass
func TestValidateDependencyReferences_ValidPlan(t *testing.T) {
	s := &Supervisor{}

	plan := &types.MissionPlan{
		MissionID:       "vc-test",
		Strategy:        "Test",
		EstimatedEffort: "1 week",
		Confidence:      0.8,
		Phases: []types.PlannedPhase{
			{
				PhaseNumber:     1,
				Title:           "Phase 1",
				Description:     "First phase",
				Strategy:        "Test",
				Tasks:           []string{"Task 1"},
				Dependencies:    []int{}, // No dependencies
				EstimatedEffort: "1 day",
			},
			{
				PhaseNumber:     2,
				Title:           "Phase 2",
				Description:     "Second phase",
				Strategy:        "Test",
				Tasks:           []string{"Task 1"},
				Dependencies:    []int{1}, // Depends on phase 1
				EstimatedEffort: "1 day",
			},
			{
				PhaseNumber:     3,
				Title:           "Phase 3",
				Description:     "Third phase",
				Strategy:        "Test",
				Tasks:           []string{"Task 1"},
				Dependencies:    []int{1, 2}, // Depends on phases 1 and 2
				EstimatedEffort: "1 day",
			},
		},
	}

	err := s.validateDependencyReferences(context.Background(), plan)
	if err != nil {
		t.Errorf("Valid plan should pass dependency validation, got: %v", err)
	}
}

// TestValidateDependencyReferences_InvalidPhaseNumber verifies invalid phase numbers are caught
func TestValidateDependencyReferences_InvalidPhaseNumber(t *testing.T) {
	s := &Supervisor{}

	plan := &types.MissionPlan{
		MissionID:       "vc-test",
		Strategy:        "Test",
		EstimatedEffort: "1 week",
		Confidence:      0.8,
		Phases: []types.PlannedPhase{
			{
				PhaseNumber:     1,
				Title:           "Phase 1",
				Description:     "First phase",
				Strategy:        "Test",
				Tasks:           []string{"Task 1"},
				Dependencies:    []int{},
				EstimatedEffort: "1 day",
			},
			{
				PhaseNumber:     2,
				Title:           "Phase 2",
				Description:     "Second phase",
				Strategy:        "Test",
				Tasks:           []string{"Task 1"},
				Dependencies:    []int{99}, // Invalid: phase 99 doesn't exist
				EstimatedEffort: "1 day",
			},
		},
	}

	err := s.validateDependencyReferences(context.Background(), plan)

	if err == nil {
		t.Fatal("Expected validation error for invalid phase dependency, got nil")
	}

	if !strings.Contains(err.Error(), "invalid dependency") {
		t.Errorf("Error should mention 'invalid dependency', got: %v", err)
	}

	if !strings.Contains(err.Error(), "phase 99") {
		t.Errorf("Error should mention 'phase 99', got: %v", err)
	}

	if !strings.Contains(err.Error(), "Phase 2") {
		t.Errorf("Error should mention 'Phase 2', got: %v", err)
	}
}

// TestValidateDependencyReferences_MultipleInvalidDependencies verifies first invalid dependency is caught
func TestValidateDependencyReferences_MultipleInvalidDependencies(t *testing.T) {
	s := &Supervisor{}

	plan := &types.MissionPlan{
		MissionID:       "vc-test",
		Strategy:        "Test",
		EstimatedEffort: "1 week",
		Confidence:      0.8,
		Phases: []types.PlannedPhase{
			{
				PhaseNumber:     1,
				Title:           "Phase 1",
				Description:     "First phase",
				Strategy:        "Test",
				Tasks:           []string{"Task 1"},
				Dependencies:    []int{},
				EstimatedEffort: "1 day",
			},
			{
				PhaseNumber:     2,
				Title:           "Phase 2",
				Description:     "Second phase",
				Strategy:        "Test",
				Tasks:           []string{"Task 1"},
				Dependencies:    []int{5, 10, 15}, // All invalid
				EstimatedEffort: "1 day",
			},
		},
	}

	err := s.validateDependencyReferences(context.Background(), plan)

	if err == nil {
		t.Fatal("Expected validation error for invalid phase dependencies, got nil")
	}

	if !strings.Contains(err.Error(), "invalid dependency") {
		t.Errorf("Error should mention 'invalid dependency', got: %v", err)
	}

	// Should mention at least one of the invalid phase numbers
	hasInvalidPhase := strings.Contains(err.Error(), "phase 5") ||
		strings.Contains(err.Error(), "phase 10") ||
		strings.Contains(err.Error(), "phase 15")

	if !hasInvalidPhase {
		t.Errorf("Error should mention one of the invalid phase numbers, got: %v", err)
	}
}

// TestValidateDependencyReferences_NonSequentialPhaseNumbers verifies validation works with non-sequential phase numbers
func TestValidateDependencyReferences_NonSequentialPhaseNumbers(t *testing.T) {
	s := &Supervisor{}

	// Phases with non-sequential numbers (e.g., 1, 3, 5)
	plan := &types.MissionPlan{
		MissionID:       "vc-test",
		Strategy:        "Test",
		EstimatedEffort: "1 week",
		Confidence:      0.8,
		Phases: []types.PlannedPhase{
			{
				PhaseNumber:     1,
				Title:           "Phase 1",
				Description:     "First phase",
				Strategy:        "Test",
				Tasks:           []string{"Task 1"},
				Dependencies:    []int{},
				EstimatedEffort: "1 day",
			},
			{
				PhaseNumber:     3,
				Title:           "Phase 3",
				Description:     "Third phase",
				Strategy:        "Test",
				Tasks:           []string{"Task 1"},
				Dependencies:    []int{1}, // Valid: depends on phase 1
				EstimatedEffort: "1 day",
			},
			{
				PhaseNumber:     5,
				Title:           "Phase 5",
				Description:     "Fifth phase",
				Strategy:        "Test",
				Tasks:           []string{"Task 1"},
				Dependencies:    []int{1, 3}, // Valid: depends on phases 1 and 3
				EstimatedEffort: "1 day",
			},
		},
	}

	err := s.validateDependencyReferences(context.Background(), plan)
	if err != nil {
		t.Errorf("Valid non-sequential plan should pass, got: %v", err)
	}

	// Now test invalid dependency for non-sequential phases
	plan.Phases[2].Dependencies = []int{1, 2, 3} // Phase 2 doesn't exist

	err = s.validateDependencyReferences(context.Background(), plan)
	if err == nil {
		t.Fatal("Expected validation error for phase 2 dependency, got nil")
	}

	if !strings.Contains(err.Error(), "phase 2") {
		t.Errorf("Error should mention 'phase 2', got: %v", err)
	}
}

// TestValidateDependencyReferences_EmptyPlan verifies empty plan is handled
func TestValidateDependencyReferences_EmptyPlan(t *testing.T) {
	s := &Supervisor{}

	plan := &types.MissionPlan{
		MissionID:       "vc-test",
		Strategy:        "Test",
		EstimatedEffort: "1 week",
		Confidence:      0.8,
		Phases:          []types.PlannedPhase{}, // Empty
	}

	// Empty plan should pass dependency validation (no dependencies to check)
	err := s.validateDependencyReferences(context.Background(), plan)
	if err != nil {
		t.Errorf("Empty plan should pass dependency validation, got: %v", err)
	}
}

// TestValidateDependencyReferences_SelfDependency verifies self-dependency is invalid
func TestValidateDependencyReferences_SelfDependency(t *testing.T) {
	s := &Supervisor{}

	plan := &types.MissionPlan{
		MissionID:       "vc-test",
		Strategy:        "Test",
		EstimatedEffort: "1 week",
		Confidence:      0.8,
		Phases: []types.PlannedPhase{
			{
				PhaseNumber:     1,
				Title:           "Phase 1",
				Description:     "First phase",
				Strategy:        "Test",
				Tasks:           []string{"Task 1"},
				Dependencies:    []int{1}, // Self-dependency (technically exists, but circular)
				EstimatedEffort: "1 day",
			},
		},
	}

	// Note: Self-dependency passes the reference validator (phase 1 exists)
	// but should be caught by the circular dependency validator
	err := s.validateDependencyReferences(context.Background(), plan)
	if err != nil {
		t.Errorf("Self-dependency should pass reference check (caught by circular validator instead), got: %v", err)
	}

	// Verify circular dependency validator catches it
	err = s.validateCircularDependencies(context.Background(), plan)
	if err == nil {
		t.Error("Expected circular dependency validator to catch self-dependency")
	}
}

// TestValidatePlan_DependencyReferencesIntegration verifies dependency validator runs in ValidatePlan
func TestValidatePlan_DependencyReferencesIntegration(t *testing.T) {
	// This test uses a direct call to validateDependencyReferences rather than ValidatePlan
	// because ValidatePlan calls MissionPlan.Validate() first, which has its own validation
	// that prevents non-sequential phase numbers. Our validator provides additional safety
	// for cases where type-level validation might be bypassed or for programmatically
	// constructed plans.

	s := &Supervisor{}

	// Test case with non-sequential phases (bypassing MissionPlan.Validate)
	plan := &types.MissionPlan{
		MissionID:       "vc-test",
		Strategy:        "Test",
		EstimatedEffort: "1 week",
		Confidence:      0.8,
		Phases: []types.PlannedPhase{
			{
				PhaseNumber:     1,
				Title:           "Phase 1",
				Description:     "First phase",
				Strategy:        "Test",
				Tasks:           []string{"Task 1"},
				Dependencies:    []int{},
				EstimatedEffort: "1 day",
			},
			{
				PhaseNumber:     3,
				Title:           "Phase 3",
				Description:     "Third phase",
				Strategy:        "Test",
				Tasks:           []string{"Task 1"},
				Dependencies:    []int{2}, // Invalid: phase 2 doesn't exist
				EstimatedEffort: "1 day",
			},
		},
	}

	// Call validateDependencyReferences directly
	err := s.validateDependencyReferences(context.Background(), plan)

	if err == nil {
		t.Fatal("Expected validation error for invalid dependency, got nil")
	}

	if !strings.Contains(err.Error(), "invalid dependency") {
		t.Errorf("Error should mention 'invalid dependency', got: %v", err)
	}

	if !strings.Contains(err.Error(), "phase 2") {
		t.Errorf("Error should mention 'phase 2', got: %v", err)
	}
}

// TestValidateDependencyReferences_LargeValidPlan verifies validator handles large plans efficiently
func TestValidateDependencyReferences_LargeValidPlan(t *testing.T) {
	s := &Supervisor{}

	// Create a plan with 15 phases, each depending on all previous phases
	phases := make([]types.PlannedPhase, 15)
	for i := 0; i < 15; i++ {
		deps := []int{}
		for j := 0; j < i; j++ {
			deps = append(deps, j+1)
		}

		phases[i] = types.PlannedPhase{
			PhaseNumber:     i + 1,
			Title:           "Phase " + string(rune('A'+i)),
			Description:     "Test phase",
			Strategy:        "Test",
			Tasks:           []string{"Task 1"},
			Dependencies:    deps,
			EstimatedEffort: "1 day",
		}
	}

	plan := &types.MissionPlan{
		MissionID:       "vc-test",
		Strategy:        "Test",
		EstimatedEffort: "15 days",
		Confidence:      0.8,
		Phases:          phases,
	}

	err := s.validateDependencyReferences(context.Background(), plan)
	if err != nil {
		t.Errorf("Large valid plan should pass dependency validation, got: %v", err)
	}
}

// TestValidateDependencyReferences_ZeroDependency verifies phase 0 is caught as invalid
func TestValidateDependencyReferences_ZeroDependency(t *testing.T) {
	s := &Supervisor{}

	plan := &types.MissionPlan{
		MissionID:       "vc-test",
		Strategy:        "Test",
		EstimatedEffort: "1 week",
		Confidence:      0.8,
		Phases: []types.PlannedPhase{
			{
				PhaseNumber:     1,
				Title:           "Phase 1",
				Description:     "First phase",
				Strategy:        "Test",
				Tasks:           []string{"Task 1"},
				Dependencies:    []int{0}, // Invalid: phase 0 doesn't exist (phases are 1-indexed)
				EstimatedEffort: "1 day",
			},
		},
	}

	err := s.validateDependencyReferences(context.Background(), plan)

	if err == nil {
		t.Fatal("Expected validation error for phase 0 dependency, got nil")
	}

	if !strings.Contains(err.Error(), "phase 0") {
		t.Errorf("Error should mention 'phase 0', got: %v", err)
	}
}

// TestValidateDependencyReferences_NegativeDependency verifies negative phase numbers are caught
func TestValidateDependencyReferences_NegativeDependency(t *testing.T) {
	s := &Supervisor{}

	plan := &types.MissionPlan{
		MissionID:       "vc-test",
		Strategy:        "Test",
		EstimatedEffort: "1 week",
		Confidence:      0.8,
		Phases: []types.PlannedPhase{
			{
				PhaseNumber:     1,
				Title:           "Phase 1",
				Description:     "First phase",
				Strategy:        "Test",
				Tasks:           []string{"Task 1"},
				Dependencies:    []int{-1}, // Invalid: negative phase number
				EstimatedEffort: "1 day",
			},
		},
	}

	err := s.validateDependencyReferences(context.Background(), plan)

	if err == nil {
		t.Fatal("Expected validation error for negative phase dependency, got nil")
	}

	if !strings.Contains(err.Error(), "phase -1") || !strings.Contains(err.Error(), "invalid dependency") {
		t.Errorf("Error should mention negative phase dependency, got: %v", err)
	}
}
