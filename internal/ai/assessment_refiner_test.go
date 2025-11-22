package ai

import (
	"context"
	"testing"

	"github.com/steveyegge/vc/internal/iterative"
	"github.com/steveyegge/vc/internal/types"
)

func TestNewAssessmentRefiner(t *testing.T) {
	tests := []struct {
		name        string
		supervisor  *Supervisor
		issue       *types.Issue
		expectError bool
	}{
		{
			name:        "nil supervisor",
			supervisor:  nil,
			issue:       &types.Issue{ID: "vc-test"},
			expectError: true,
		},
		{
			name:        "nil issue",
			supervisor:  &Supervisor{},
			issue:       nil,
			expectError: true,
		},
		{
			name: "valid inputs",
			supervisor: &Supervisor{
				model: ModelSonnet,
			},
			issue: &types.Issue{
				ID:          "vc-test",
				Title:       "Test issue",
				Description: "Test description",
			},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			refiner, err := NewAssessmentRefiner(tt.supervisor, tt.issue)
			if tt.expectError {
				if err == nil {
					t.Errorf("expected error but got nil")
				}
				if refiner != nil {
					t.Errorf("expected nil refiner but got %v", refiner)
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
				if refiner == nil {
					t.Errorf("expected refiner but got nil")
				}
				if refiner.minConfidence != 0.80 {
					t.Errorf("expected minConfidence 0.80, got %.2f", refiner.minConfidence)
				}
			}
		})
	}
}

func TestSerializeAssessment(t *testing.T) {
	assessment := &Assessment{
		Strategy:   "Implement feature X",
		Steps:      []string{"Step 1", "Step 2", "Step 3"},
		Risks:      []string{"Risk A", "Risk B"},
		Confidence: 0.85,
		Reasoning:  "This is the best approach",
		ShouldDecompose: false,
	}

	serialized := serializeAssessment(assessment)

	// Check that key fields are present
	if serialized == "" {
		t.Error("expected non-empty serialization")
	}

	// Check for key content
	expectedStrings := []string{
		"Strategy: Implement feature X",
		"Confidence: 0.85",
		"Steps (3):",
		"1. Step 1",
		"2. Step 2",
		"3. Step 3",
		"Risks (2):",
		"1. Risk A",
		"2. Risk B",
		"Reasoning: This is the best approach",
		"Should Decompose: false",
	}

	for _, expected := range expectedStrings {
		if !contains(serialized, expected) {
			t.Errorf("expected serialization to contain %q", expected)
		}
	}
}

func TestSerializeAssessmentWithDecomposition(t *testing.T) {
	assessment := &Assessment{
		Strategy:   "Decompose into smaller tasks",
		Steps:      []string{"Analyze", "Break down"},
		Risks:      []string{"Complexity"},
		Confidence: 0.70,
		Reasoning:  "Too large for one task",
		ShouldDecompose: true,
		DecompositionPlan: &DecompositionPlan{
			Reasoning: "Multiple independent components",
			ChildIssues: []ChildIssue{
				{
					Title:              "Implement component A",
					Description:        "Build the A component",
					AcceptanceCriteria: "A works",
					Priority:           1,
					EstimatedMinutes:   30,
				},
				{
					Title:              "Implement component B",
					Description:        "Build the B component",
					AcceptanceCriteria: "B works",
					Priority:           2,
					EstimatedMinutes:   45,
				},
			},
		},
	}

	serialized := serializeAssessment(assessment)

	// Check decomposition is included
	expectedStrings := []string{
		"Should Decompose: true",
		"Decomposition Reasoning: Multiple independent components",
		"Child Issues: 2",
		"1. Implement component A (P1, 30m)",
		"2. Implement component B (P2, 45m)",
	}

	for _, expected := range expectedStrings {
		if !contains(serialized, expected) {
			t.Errorf("expected serialization to contain %q", expected)
		}
	}
}

func TestShouldIterateAssessment_P0Issue(t *testing.T) {
	supervisor := &Supervisor{}
	issue := &types.Issue{
		ID:       "vc-test",
		Priority: 0, // P0
		Title:    "Critical bug",
	}

	shouldIterate, reason := supervisor.shouldIterateAssessment(context.Background(), issue)

	if !shouldIterate {
		t.Error("expected P0 issue to trigger iteration")
	}
	if reason != "P0 issue (critical priority)" {
		t.Errorf("unexpected reason: %s", reason)
	}
}

func TestShouldIterateAssessment_Mission(t *testing.T) {
	supervisor := &Supervisor{}
	issue := &types.Issue{
		ID:           "vc-test",
		Priority:     2,
		Title:        "Core mission",
		IssueSubtype: types.SubtypeMission,
	}

	shouldIterate, reason := supervisor.shouldIterateAssessment(context.Background(), issue)

	if !shouldIterate {
		t.Error("expected mission issue to trigger iteration")
	}
	if !contains(reason, "complex structural issue") {
		t.Errorf("unexpected reason: %s", reason)
	}
}

func TestShouldIterateAssessment_Phase(t *testing.T) {
	supervisor := &Supervisor{}
	issue := &types.Issue{
		ID:           "vc-test",
		Priority:     2,
		Title:        "Implementation phase",
		IssueSubtype: types.SubtypePhase,
	}

	shouldIterate, reason := supervisor.shouldIterateAssessment(context.Background(), issue)

	if !shouldIterate {
		t.Error("expected phase issue to trigger iteration")
	}
	if !contains(reason, "complex structural issue") {
		t.Errorf("unexpected reason: %s", reason)
	}
}

func TestShouldIterateAssessment_SimpleIssue(t *testing.T) {
	supervisor := &Supervisor{}
	issue := &types.Issue{
		ID:       "vc-test",
		Priority: 2,
		Title:    "Simple fix",
		IssueType: types.TypeTask,
	}

	shouldIterate, reason := supervisor.shouldIterateAssessment(context.Background(), issue)

	if shouldIterate {
		t.Errorf("expected simple issue to skip iteration, but got: %s", reason)
	}
	if !contains(reason, "simple issue") {
		t.Errorf("unexpected reason: %s", reason)
	}
}

func TestBuildIterationContext(t *testing.T) {
	supervisor := &Supervisor{}
	issue := &types.Issue{ID: "vc-test"}
	refiner, _ := NewAssessmentRefiner(supervisor, issue)

	artifact := &iterative.Artifact{
		Type:    "assessment",
		Content: "initial content",
		Context: "initial context",
	}

	assessment := &Assessment{
		Strategy:   "New strategy",
		Steps:      []string{"Step 1", "Step 2", "Step 3"},
		Risks:      []string{"Risk A", "Risk B", "Risk C"},
		Confidence: 0.85,
		ShouldDecompose: false,
	}

	context := refiner.buildIterationContext(artifact, assessment)

	// Check that context includes previous context
	if !contains(context, "initial context") {
		t.Error("expected context to include previous context")
	}

	// Check that context includes summary of iteration
	expectedStrings := []string{
		"Previous iteration:",
		"Strategy: New strategy",
		"Steps: 3",
		"Risks: 3",
		"Confidence: 0.85",
		"Should decompose: false",
		"Risks identified:",
		"1. Risk A",
		"2. Risk B",
		"3. Risk C",
	}

	for _, expected := range expectedStrings {
		if !contains(context, expected) {
			t.Errorf("expected context to contain %q", expected)
		}
	}
}
