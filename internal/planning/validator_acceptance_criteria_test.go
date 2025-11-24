package planning

import (
	"context"
	"testing"
)

func TestAcceptanceCriteriaValidator_WellFormed(t *testing.T) {
	validator := &AcceptanceCriteriaValidator{}

	plan := &MissionPlan{
		Phases: []Phase{
			{
				ID: "phase-1",
				Tasks: []Task{
					{
						ID:    "task-1-1",
						Title: "Good Task",
						AcceptanceCriteria: []string{
							"WHEN validator runs THEN it detects valid criteria",
							"WHEN criteria properly formatted THEN no warnings",
						},
					},
				},
			},
		},
	}

	result := validator.Validate(context.Background(), plan, &ValidationContext{})

	if len(result.Errors) > 0 {
		t.Errorf("expected no errors for well-formed criteria, got %d", len(result.Errors))
	}
	if len(result.Warnings) > 0 {
		t.Errorf("expected no warnings for well-formed criteria, got %d", len(result.Warnings))
	}
}

func TestAcceptanceCriteriaValidator_Missing(t *testing.T) {
	validator := &AcceptanceCriteriaValidator{}

	plan := &MissionPlan{
		Phases: []Phase{
			{
				ID: "phase-1",
				Tasks: []Task{
					{
						ID:                 "task-1-1",
						Title:              "Task Without AC",
						AcceptanceCriteria: nil, // Missing
					},
				},
			},
		},
	}

	result := validator.Validate(context.Background(), plan, &ValidationContext{})

	if len(result.Errors) != 1 {
		t.Fatalf("expected 1 error for missing criteria, got %d", len(result.Errors))
	}

	err := result.Errors[0]
	if err.Code != "MISSING_ACCEPTANCE_CRITERIA" {
		t.Errorf("expected error code MISSING_ACCEPTANCE_CRITERIA, got %s", err.Code)
	}
	if err.Location != "task-1-1" {
		t.Errorf("expected location 'task-1-1', got %s", err.Location)
	}
}

func TestAcceptanceCriteriaValidator_Vague(t *testing.T) {
	validator := &AcceptanceCriteriaValidator{}

	plan := &MissionPlan{
		Phases: []Phase{
			{
				ID: "phase-1",
				Tasks: []Task{
					{
						ID:    "task-1-1",
						Title: "Task With Vague AC",
						AcceptanceCriteria: []string{
							"Test the feature",           // Vague
							"Make sure it works properly", // Vague
						},
					},
				},
			},
		},
	}

	result := validator.Validate(context.Background(), plan, &ValidationContext{})

	// Should have warnings, not errors
	if len(result.Errors) > 0 {
		t.Errorf("expected no errors for vague criteria (warnings only), got %d", len(result.Errors))
	}
	if len(result.Warnings) != 2 {
		t.Fatalf("expected 2 warnings for vague criteria, got %d", len(result.Warnings))
	}

	for _, warning := range result.Warnings {
		if warning.Code != "VAGUE_ACCEPTANCE_CRITERIA" {
			t.Errorf("expected warning code VAGUE_ACCEPTANCE_CRITERIA, got %s", warning.Code)
		}
		if warning.Severity != WarningSeverityMedium {
			t.Errorf("expected medium severity, got %s", warning.Severity)
		}
	}
}

func TestAcceptanceCriteriaValidator_Mixed(t *testing.T) {
	validator := &AcceptanceCriteriaValidator{}

	plan := &MissionPlan{
		Phases: []Phase{
			{
				ID: "phase-1",
				Tasks: []Task{
					{
						ID:    "task-1-1",
						Title: "Task With Mixed AC",
						AcceptanceCriteria: []string{
							"WHEN validator runs THEN it detects valid criteria", // Good
							"Make sure it works",                                  // Vague
							"WHEN input invalid THEN error returned",              // Good
						},
					},
				},
			},
		},
	}

	result := validator.Validate(context.Background(), plan, &ValidationContext{})

	// Should have 1 warning for the vague criterion
	if len(result.Errors) > 0 {
		t.Errorf("expected no errors, got %d", len(result.Errors))
	}
	if len(result.Warnings) != 1 {
		t.Fatalf("expected 1 warning for vague criterion, got %d", len(result.Warnings))
	}
}

func TestAcceptanceCriteriaValidator_CaseInsensitive(t *testing.T) {
	validator := &AcceptanceCriteriaValidator{}

	plan := &MissionPlan{
		Phases: []Phase{
			{
				ID: "phase-1",
				Tasks: []Task{
					{
						ID:    "task-1-1",
						Title: "Task With Lowercase",
						AcceptanceCriteria: []string{
							"when validator runs then it detects valid criteria", // Lowercase
						},
					},
					{
						ID:    "task-1-2",
						Title: "Task With Mixed Case",
						AcceptanceCriteria: []string{
							"When Validator Runs Then It Detects Valid Criteria", // Mixed case
						},
					},
				},
			},
		},
	}

	result := validator.Validate(context.Background(), plan, &ValidationContext{})

	// Should accept lowercase/mixed case WHEN...THEN...
	if len(result.Errors) > 0 {
		t.Errorf("expected no errors for case variations, got %d", len(result.Errors))
	}
	if len(result.Warnings) > 0 {
		t.Errorf("expected no warnings for case variations, got %d", len(result.Warnings))
	}
}

func TestAcceptanceCriteriaValidator_Priority(t *testing.T) {
	validator := &AcceptanceCriteriaValidator{}
	if validator.Priority() != 10 {
		t.Errorf("expected priority 10, got %d", validator.Priority())
	}
}

func TestAcceptanceCriteriaValidator_Name(t *testing.T) {
	validator := &AcceptanceCriteriaValidator{}
	if validator.Name() != "acceptance_criteria" {
		t.Errorf("expected name 'acceptance_criteria', got '%s'", validator.Name())
	}
}
