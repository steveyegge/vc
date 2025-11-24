package planning

import (
	"context"
	"testing"
)

// TestValidationIntegration_GoodPlan tests all validators with a well-formed plan.
func TestValidationIntegration_GoodPlan(t *testing.T) {
	registry := NewValidatorRegistry()
	registry.Register(&CycleDetector{})
	registry.Register(&PhaseSizeValidator{})
	registry.Register(&AcceptanceCriteriaValidator{})
	registry.Register(&EstimateValidator{})
	registry.Register(&NFRCoverageValidator{})
	registry.Register(&DuplicateWorkDetector{})

	plan := &MissionPlan{
		MissionID:      "vc-test",
		MissionTitle:   "Test Mission",
		Goal:           "Validate the validation framework",
		Constraints:    []string{"Tests must pass in <5s", "No breaking changes"},
		EstimatedHours: 10.0,
		Phases: []Phase{
			{
				ID:             "phase-1",
				Title:          "Foundation",
				Description:    "Build the core types",
				Strategy:       "Bottom-up implementation",
				Dependencies:   []string{},
				EstimatedHours: 5.0,
				Priority:       1,
				Tasks: []Task{
					{
						ID:                 "task-1-1",
						Title:              "Define types",
						Description:        "Create Plan, Phase, Task types",
						AcceptanceCriteria: []string{"WHEN types defined THEN all fields present"},
						Dependencies:       []string{},
						EstimatedMinutes:   120,
						Priority:           1,
					},
					{
						ID:                 "task-1-2",
						Title:              "Add validation",
						Description:        "Create Validator interface",
						AcceptanceCriteria: []string{"WHEN validator runs THEN errors returned"},
						Dependencies:       []string{"task-1-1"},
						EstimatedMinutes:   120,
						Priority:           2,
					},
					{
						ID:                 "task-1-3",
						Title:              "Test types",
						Description:        "Add unit tests for all types",
						AcceptanceCriteria: []string{"WHEN tests run THEN coverage >80%"},
						Dependencies:       []string{"task-1-1"},
						EstimatedMinutes:   60,
						Priority:           3,
					},
				},
			},
			{
				ID:             "phase-2",
				Title:          "Validators",
				Description:    "Implement all validators",
				Strategy:       "Implement in priority order",
				Dependencies:   []string{"phase-1"},
				EstimatedHours: 5.0,
				Priority:       2,
				Tasks: []Task{
					{
						ID:                 "task-2-1",
						Title:              "Cycle detector",
						Description:        "Detect circular dependencies",
						AcceptanceCriteria: []string{"WHEN cycle present THEN error returned"},
						Dependencies:       []string{},
						EstimatedMinutes:   120,
						Priority:           1,
					},
					{
						ID:                 "task-2-2",
						Title:              "Phase size validator",
						Description:        "Check phase size bounds",
						AcceptanceCriteria: []string{"WHEN phase too large THEN warning returned"},
						Dependencies:       []string{},
						EstimatedMinutes:   60,
						Priority:           2,
					},
					{
						ID:                 "task-2-3",
						Title:              "Test validators",
						Description:        "Add tests that validate passing in <5s",
						AcceptanceCriteria: []string{"WHEN tests run THEN complete in <5s"},
						Dependencies:       []string{"task-2-1", "task-2-2"},
						EstimatedMinutes:   120,
						Priority:           3,
					},
				},
			},
		},
	}

	vctx := &ValidationContext{
		Constraints: plan.Constraints,
		Goals:       []string{plan.Goal},
	}

	result := registry.ValidateAll(context.Background(), plan, vctx)

	// Should have no errors
	if len(result.Errors) > 0 {
		t.Errorf("expected no errors for good plan, got %d", len(result.Errors))
		for _, err := range result.Errors {
			t.Logf("  %s: %s (location: %s)", err.Code, err.Message, err.Location)
		}
	}

	// May have some warnings (e.g., NFR coverage is heuristic-based)
	if len(result.Warnings) > 0 {
		t.Logf("Got %d warnings (this is OK for good plans):", len(result.Warnings))
		for _, w := range result.Warnings {
			t.Logf("  [%s] %s: %s", w.Severity, w.Code, w.Message)
		}
	}

	// Verify result helpers
	if !result.IsValid() {
		t.Error("expected IsValid() to be true when no errors")
	}
}

// TestValidationIntegration_BadPlan tests all validators with a problematic plan.
func TestValidationIntegration_BadPlan(t *testing.T) {
	registry := NewValidatorRegistry()
	registry.Register(&CycleDetector{})
	registry.Register(&PhaseSizeValidator{})
	registry.Register(&AcceptanceCriteriaValidator{})
	registry.Register(&EstimateValidator{})

	plan := &MissionPlan{
		MissionID:      "vc-bad",
		MissionTitle:   "Bad Plan",
		Goal:           "Test error detection",
		Constraints:    []string{},
		EstimatedHours: 2.0,
		Phases: []Phase{
			{
				ID:             "phase-1",
				Title:          "Broken Phase",
				Description:    "This phase has problems",
				Strategy:       "Try to break things",
				Dependencies:   []string{"phase-2"}, // Creates cycle with phase-2
				EstimatedHours: 1.0,
				Tasks: []Task{
					{
						ID:                 "task-1-1",
						Title:              "Bad task",
						Description:        "Missing acceptance criteria",
						AcceptanceCriteria: nil, // ERROR: Missing AC
						EstimatedMinutes:   300, // WARNING: Too long (5 hours)
					},
				},
			},
			{
				ID:             "phase-2",
				Title:          "Another Broken Phase",
				Description:    "This one has dependency cycle",
				Strategy:       "Make it worse",
				Dependencies:   []string{"phase-1"}, // Creates cycle with phase-1
				EstimatedHours: 1.0,
				Tasks: []Task{
					{
						ID:                 "task-2-1",
						Title:              "Task with vague AC",
						Description:        "This has vague criteria",
						AcceptanceCriteria: []string{"Make it work"}, // WARNING: Vague
						EstimatedMinutes:   60,
					},
				},
			},
		},
	}

	vctx := &ValidationContext{
		Constraints: plan.Constraints,
		Goals:       []string{plan.Goal},
	}

	result := registry.ValidateAll(context.Background(), plan, vctx)

	// Should have multiple errors
	if len(result.Errors) == 0 {
		t.Fatal("expected errors for bad plan, got none")
	}

	// Verify we got the expected error types
	errorCodes := make(map[string]bool)
	for _, err := range result.Errors {
		errorCodes[err.Code] = true
		t.Logf("Error: %s - %s", err.Code, err.Message)
	}

	// Must have cycle error
	if !errorCodes["PHASE_CYCLE_DETECTED"] {
		t.Error("expected PHASE_CYCLE_DETECTED error")
	}

	// Must have missing AC error
	if !errorCodes["MISSING_ACCEPTANCE_CRITERIA"] {
		t.Error("expected MISSING_ACCEPTANCE_CRITERIA error")
	}

	// Should have warnings
	if len(result.Warnings) == 0 {
		t.Error("expected warnings for bad plan")
	}

	warningCodes := make(map[string]bool)
	for _, w := range result.Warnings {
		warningCodes[w.Code] = true
		t.Logf("Warning: %s - %s", w.Code, w.Message)
	}

	// Should warn about vague AC
	if !warningCodes["VAGUE_ACCEPTANCE_CRITERIA"] {
		t.Error("expected VAGUE_ACCEPTANCE_CRITERIA warning")
	}

	// Verify result helpers
	if result.IsValid() {
		t.Error("expected IsValid() to be false when errors present")
	}
	if !result.HasErrors() {
		t.Error("expected HasErrors() to be true")
	}
	if !result.HasWarnings() {
		t.Error("expected HasWarnings() to be true")
	}
}

// TestValidationIntegration_DuplicateTasks tests duplicate detection across phases.
func TestValidationIntegration_DuplicateTasks(t *testing.T) {
	registry := NewValidatorRegistry()
	registry.Register(&DuplicateWorkDetector{})

	plan := &MissionPlan{
		Phases: []Phase{
			{
				ID:    "phase-1",
				Title: "First Phase",
				Tasks: []Task{
					{
						ID:                 "task-1-1",
						Title:              "Implement user authentication system",
						Description:        "Build login and signup functionality with JWT tokens",
						AcceptanceCriteria: []string{"WHEN user logs in THEN JWT token returned"},
					},
				},
			},
			{
				ID:    "phase-2",
				Title: "Second Phase",
				Tasks: []Task{
					{
						ID:                 "task-2-1",
						Title:              "Implement user authentication system",
						Description:        "Build login and signup with JWT authentication",
						AcceptanceCriteria: []string{"WHEN user signs up THEN account created"},
					},
				},
			},
		},
	}

	result := registry.ValidateAll(context.Background(), plan, &ValidationContext{})

	// Should detect the duplicate
	if len(result.Warnings) == 0 {
		t.Fatal("expected duplicate warning, got none")
	}

	found := false
	for _, w := range result.Warnings {
		if w.Code == "POTENTIAL_DUPLICATE" {
			found = true
			t.Logf("Duplicate detected: %s", w.Message)
		}
	}

	if !found {
		t.Error("expected POTENTIAL_DUPLICATE warning")
	}
}

// TestValidationIntegration_ExecutionOrder tests that validators run in priority order.
func TestValidationIntegration_ExecutionOrder(t *testing.T) {
	registry := NewValidatorRegistry()

	// Register in random order
	registry.Register(&PhaseSizeValidator{})      // Priority: 10
	registry.Register(&CycleDetector{})           // Priority: 1
	registry.Register(&EstimateValidator{})       // Priority: 10
	registry.Register(&DuplicateWorkDetector{})   // Priority: 10

	// Verify validators are sorted by priority
	if len(registry.validators) != 4 {
		t.Fatalf("expected 4 validators, got %d", len(registry.validators))
	}

	// First should be CycleDetector (priority 1)
	if registry.validators[0].Priority() != 1 {
		t.Errorf("expected first validator to have priority 1, got %d", registry.validators[0].Priority())
	}

	// All others should have priority >= 10
	for i := 1; i < len(registry.validators); i++ {
		if registry.validators[i].Priority() < 10 {
			t.Errorf("expected validator %d to have priority >= 10, got %d", i, registry.validators[i].Priority())
		}
	}
}
