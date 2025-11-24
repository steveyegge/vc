package planning

import (
	"context"
	"strings"
	"testing"
)

func TestCycleDetector_NoCycles(t *testing.T) {
	detector := &CycleDetector{}

	plan := &MissionPlan{
		Phases: []Phase{
			{
				ID:           "phase-1",
				Dependencies: []string{}, // No dependencies
				Tasks: []Task{
					{ID: "task-1-1", Dependencies: []string{}},
					{ID: "task-1-2", Dependencies: []string{"task-1-1"}}, // Linear dependency
				},
			},
			{
				ID:           "phase-2",
				Dependencies: []string{"phase-1"}, // Depends on phase-1
				Tasks: []Task{
					{ID: "task-2-1", Dependencies: []string{}},
				},
			},
		},
	}

	result := detector.Validate(context.Background(), plan, &ValidationContext{})

	if len(result.Errors) > 0 {
		t.Errorf("expected no errors for valid DAG, got %d errors", len(result.Errors))
		for _, err := range result.Errors {
			t.Logf("  %s: %s", err.Code, err.Message)
		}
	}
}

func TestCycleDetector_PhaseCycle(t *testing.T) {
	detector := &CycleDetector{}

	plan := &MissionPlan{
		Phases: []Phase{
			{
				ID:           "phase-1",
				Dependencies: []string{"phase-3"}, // Creates cycle
			},
			{
				ID:           "phase-2",
				Dependencies: []string{"phase-1"},
			},
			{
				ID:           "phase-3",
				Dependencies: []string{"phase-2"},
			},
		},
	}

	result := detector.Validate(context.Background(), plan, &ValidationContext{})

	if len(result.Errors) != 1 {
		t.Fatalf("expected 1 error for phase cycle, got %d", len(result.Errors))
	}

	err := result.Errors[0]
	if err.Code != "PHASE_CYCLE_DETECTED" {
		t.Errorf("expected error code PHASE_CYCLE_DETECTED, got %s", err.Code)
	}

	// Verify cycle is reported in the message
	if !strings.Contains(err.Message, "→") {
		t.Errorf("expected cycle path in message, got: %s", err.Message)
	}
}

func TestCycleDetector_TaskCycle(t *testing.T) {
	detector := &CycleDetector{}

	plan := &MissionPlan{
		Phases: []Phase{
			{
				ID: "phase-1",
				Tasks: []Task{
					{ID: "task-1-1", Dependencies: []string{"task-1-3"}}, // Creates cycle
					{ID: "task-1-2", Dependencies: []string{"task-1-1"}},
					{ID: "task-1-3", Dependencies: []string{"task-1-2"}},
				},
			},
		},
	}

	result := detector.Validate(context.Background(), plan, &ValidationContext{})

	if len(result.Errors) != 1 {
		t.Fatalf("expected 1 error for task cycle, got %d", len(result.Errors))
	}

	err := result.Errors[0]
	if err.Code != "TASK_CYCLE_DETECTED" {
		t.Errorf("expected error code TASK_CYCLE_DETECTED, got %s", err.Code)
	}

	if err.Location != "phase-1" {
		t.Errorf("expected location 'phase-1', got %s", err.Location)
	}

	// Verify cycle is reported in the message
	if !strings.Contains(err.Message, "→") {
		t.Errorf("expected cycle path in message, got: %s", err.Message)
	}
}

func TestCycleDetector_SelfCycle(t *testing.T) {
	detector := &CycleDetector{}

	plan := &MissionPlan{
		Phases: []Phase{
			{
				ID:           "phase-1",
				Dependencies: []string{"phase-1"}, // Self-dependency
			},
		},
	}

	result := detector.Validate(context.Background(), plan, &ValidationContext{})

	if len(result.Errors) != 1 {
		t.Fatalf("expected 1 error for self-cycle, got %d", len(result.Errors))
	}

	err := result.Errors[0]
	if err.Code != "PHASE_CYCLE_DETECTED" {
		t.Errorf("expected error code PHASE_CYCLE_DETECTED, got %s", err.Code)
	}
}

func TestCycleDetector_MultipleCycles(t *testing.T) {
	detector := &CycleDetector{}

	plan := &MissionPlan{
		Phases: []Phase{
			{
				ID:           "phase-1",
				Dependencies: []string{"phase-2"},
			},
			{
				ID:           "phase-2",
				Dependencies: []string{"phase-1"}, // First cycle
				Tasks: []Task{
					{ID: "task-2-1", Dependencies: []string{"task-2-2"}},
					{ID: "task-2-2", Dependencies: []string{"task-2-1"}}, // Second cycle
				},
			},
		},
	}

	result := detector.Validate(context.Background(), plan, &ValidationContext{})

	// Should detect both the phase cycle and the task cycle
	if len(result.Errors) != 2 {
		t.Errorf("expected 2 errors (phase cycle + task cycle), got %d", len(result.Errors))
		for _, err := range result.Errors {
			t.Logf("  %s: %s", err.Code, err.Message)
		}
	}
}

func TestCycleDetector_ComplexDAG(t *testing.T) {
	detector := &CycleDetector{}

	// Diamond dependency pattern (valid DAG)
	plan := &MissionPlan{
		Phases: []Phase{
			{
				ID:           "phase-1",
				Dependencies: []string{},
			},
			{
				ID:           "phase-2",
				Dependencies: []string{"phase-1"},
			},
			{
				ID:           "phase-3",
				Dependencies: []string{"phase-1"},
			},
			{
				ID:           "phase-4",
				Dependencies: []string{"phase-2", "phase-3"}, // Converge
			},
		},
	}

	result := detector.Validate(context.Background(), plan, &ValidationContext{})

	if len(result.Errors) > 0 {
		t.Errorf("expected no errors for valid diamond DAG, got %d errors", len(result.Errors))
		for _, err := range result.Errors {
			t.Logf("  %s: %s", err.Code, err.Message)
		}
	}
}

func TestCycleDetector_Priority(t *testing.T) {
	detector := &CycleDetector{}

	if detector.Priority() != 1 {
		t.Errorf("expected priority 1, got %d", detector.Priority())
	}
}

func TestCycleDetector_Name(t *testing.T) {
	detector := &CycleDetector{}

	if detector.Name() != "cycle_detector" {
		t.Errorf("expected name 'cycle_detector', got '%s'", detector.Name())
	}
}
