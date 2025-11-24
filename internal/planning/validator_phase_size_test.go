package planning

import (
	"context"
	"testing"
)

func TestPhaseSizeValidator_NormalSize(t *testing.T) {
	validator := &PhaseSizeValidator{}

	plan := &MissionPlan{
		Phases: []Phase{
			{
				ID:    "phase-1",
				Title: "Normal Phase",
				Tasks: []Task{
					{ID: "task-1-1"},
					{ID: "task-1-2"},
					{ID: "task-1-3"},
					{ID: "task-1-4"},
					{ID: "task-1-5"},
				},
			},
		},
	}

	result := validator.Validate(context.Background(), plan, &ValidationContext{})

	if len(result.Errors) > 0 {
		t.Errorf("expected no errors for normal phase size, got %d", len(result.Errors))
	}
	if len(result.Warnings) > 0 {
		t.Errorf("expected no warnings for normal phase size, got %d", len(result.Warnings))
	}
}

func TestPhaseSizeValidator_TooSmall(t *testing.T) {
	validator := &PhaseSizeValidator{}

	plan := &MissionPlan{
		Phases: []Phase{
			{
				ID:    "phase-1",
				Title: "Tiny Phase",
				Tasks: []Task{
					{ID: "task-1-1"},
					{ID: "task-1-2"},
				},
			},
		},
	}

	result := validator.Validate(context.Background(), plan, &ValidationContext{})

	if len(result.Errors) > 0 {
		t.Errorf("expected no errors for small phase (warnings only), got %d", len(result.Errors))
	}
	if len(result.Warnings) != 1 {
		t.Fatalf("expected 1 warning for small phase, got %d", len(result.Warnings))
	}

	warning := result.Warnings[0]
	if warning.Code != "PHASE_TOO_SMALL" {
		t.Errorf("expected warning code PHASE_TOO_SMALL, got %s", warning.Code)
	}
	if warning.Severity != WarningSeverityMedium {
		t.Errorf("expected medium severity, got %s", warning.Severity)
	}
}

func TestPhaseSizeValidator_TooLarge(t *testing.T) {
	validator := &PhaseSizeValidator{}

	// Create a phase with 20 tasks (over the limit of 15)
	tasks := make([]Task, 20)
	for i := range tasks {
		tasks[i] = Task{ID: "task-1-" + string(rune('a'+i))}
	}

	plan := &MissionPlan{
		Phases: []Phase{
			{
				ID:    "phase-1",
				Title: "Huge Phase",
				Tasks: tasks,
			},
		},
	}

	result := validator.Validate(context.Background(), plan, &ValidationContext{})

	if len(result.Errors) > 0 {
		t.Errorf("expected no errors for large phase (warnings only), got %d", len(result.Errors))
	}
	if len(result.Warnings) != 1 {
		t.Fatalf("expected 1 warning for large phase, got %d", len(result.Warnings))
	}

	warning := result.Warnings[0]
	if warning.Code != "PHASE_TOO_LARGE" {
		t.Errorf("expected warning code PHASE_TOO_LARGE, got %s", warning.Code)
	}
	if warning.Severity != WarningSeverityHigh {
		t.Errorf("expected high severity, got %s", warning.Severity)
	}
}

func TestPhaseSizeValidator_MultiplePhases(t *testing.T) {
	validator := &PhaseSizeValidator{}

	// Create tasks for different phases
	smallTasks := []Task{{ID: "task-1-1"}}
	normalTasks := []Task{
		{ID: "task-2-1"},
		{ID: "task-2-2"},
		{ID: "task-2-3"},
		{ID: "task-2-4"},
		{ID: "task-2-5"},
	}
	largeTasks := make([]Task, 20)
	for i := range largeTasks {
		largeTasks[i] = Task{ID: "task-3-" + string(rune('a'+i))}
	}

	plan := &MissionPlan{
		Phases: []Phase{
			{ID: "phase-1", Title: "Small", Tasks: smallTasks},
			{ID: "phase-2", Title: "Normal", Tasks: normalTasks},
			{ID: "phase-3", Title: "Large", Tasks: largeTasks},
		},
	}

	result := validator.Validate(context.Background(), plan, &ValidationContext{})

	// Should have warnings for phase-1 (too small) and phase-3 (too large)
	if len(result.Warnings) != 2 {
		t.Fatalf("expected 2 warnings, got %d", len(result.Warnings))
	}

	// Check that we got one PHASE_TOO_SMALL and one PHASE_TOO_LARGE
	codes := make(map[string]bool)
	for _, w := range result.Warnings {
		codes[w.Code] = true
	}
	if !codes["PHASE_TOO_SMALL"] {
		t.Error("expected PHASE_TOO_SMALL warning")
	}
	if !codes["PHASE_TOO_LARGE"] {
		t.Error("expected PHASE_TOO_LARGE warning")
	}
}

func TestPhaseSizeValidator_Priority(t *testing.T) {
	validator := &PhaseSizeValidator{}
	if validator.Priority() != 10 {
		t.Errorf("expected priority 10, got %d", validator.Priority())
	}
}

func TestPhaseSizeValidator_Name(t *testing.T) {
	validator := &PhaseSizeValidator{}
	if validator.Name() != "phase_size" {
		t.Errorf("expected name 'phase_size', got '%s'", validator.Name())
	}
}
