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
				} else if refiner.minConfidence != 0.80 {
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

	shouldIterate, triggers, skipReason := supervisor.shouldIterateAssessment(context.Background(), issue)

	if !shouldIterate {
		t.Error("expected P0 issue to trigger iteration")
	}
	if len(triggers) == 0 || triggers[0] != "P0 priority" {
		t.Errorf("unexpected triggers: %v", triggers)
	}
	if skipReason != "" {
		t.Errorf("expected empty skipReason, got: %s", skipReason)
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

	shouldIterate, triggers, skipReason := supervisor.shouldIterateAssessment(context.Background(), issue)

	if !shouldIterate {
		t.Error("expected mission issue to trigger iteration")
	}
	if len(triggers) == 0 || !contains(triggers[0], "complex structural issue") {
		t.Errorf("unexpected triggers: %v", triggers)
	}
	if skipReason != "" {
		t.Errorf("expected empty skipReason, got: %s", skipReason)
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

	shouldIterate, triggers, skipReason := supervisor.shouldIterateAssessment(context.Background(), issue)

	if !shouldIterate {
		t.Error("expected phase issue to trigger iteration")
	}
	if len(triggers) == 0 || !contains(triggers[0], "complex structural issue") {
		t.Errorf("unexpected triggers: %v", triggers)
	}
	if skipReason != "" {
		t.Errorf("expected empty skipReason, got: %s", skipReason)
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

	shouldIterate, triggers, skipReason := supervisor.shouldIterateAssessment(context.Background(), issue)

	if shouldIterate {
		t.Errorf("expected simple issue to skip iteration, but got triggers: %v", triggers)
	}
	if len(triggers) != 0 {
		t.Errorf("expected no triggers, got: %v", triggers)
	}
	if !contains(skipReason, "simple issue") {
		t.Errorf("unexpected skipReason: %s", skipReason)
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

// TestBuildIterationContextEmptyRisks tests context building with no risks
func TestBuildIterationContextEmptyRisks(t *testing.T) {
	supervisor := &Supervisor{}
	issue := &types.Issue{ID: "vc-test"}
	refiner, _ := NewAssessmentRefiner(supervisor, issue)

	artifact := &iterative.Artifact{
		Type:    "assessment",
		Content: "content",
		Context: "",
	}

	assessment := &Assessment{
		Strategy:   "Simple strategy",
		Steps:      []string{"Step 1"},
		Risks:      []string{},
		Confidence: 0.90,
		ShouldDecompose: false,
	}

	context := refiner.buildIterationContext(artifact, assessment)

	// Should have basic stats
	expectedStrings := []string{
		"Previous iteration:",
		"Strategy: Simple strategy",
		"Steps: 1",
		"Risks: 0",
	}

	for _, expected := range expectedStrings {
		if !contains(context, expected) {
			t.Errorf("expected context to contain %q", expected)
		}
	}

	// Should NOT have risks section
	if contains(context, "Risks identified:") {
		t.Error("expected no risks section when empty")
	}
}

// TestAssessmentRefinerBuildRefinementPrompt tests prompt building for refinement
func TestAssessmentRefinerBuildRefinementPrompt(t *testing.T) {
	supervisor := &Supervisor{}
	issue := &types.Issue{
		ID:                 "vc-123",
		Title:              "Test Issue",
		Description:        "Test description",
		AcceptanceCriteria: "1. Must work\n2. Must be tested",
		Priority:           1,
		IssueType:          types.TypeTask,
	}
	refiner, _ := NewAssessmentRefiner(supervisor, issue)

	artifact := &iterative.Artifact{
		Type:    "assessment",
		Content: "Previous assessment content",
		Context: "Previous context",
	}

	prompt := refiner.buildRefinementPrompt(artifact)

	// Check for key sections
	expectedStrings := []string{
		"ITERATIVE REFINEMENT",
		"vc-123",
		"Test Issue",
		"Test description",
		"1. Must work",
		"P1",
		"task",
		"Previous assessment content",
		"Previous context",
		"Strategy",
		"Steps",
		"Risks",
		"Confidence",
		"should_decompose",
	}

	for _, expected := range expectedStrings {
		if !contains(prompt, expected) {
			t.Errorf("refinement prompt missing expected string: %q", expected)
		}
	}
}

// TestAssessmentRefinerBuildConvergencePrompt tests prompt building for convergence check
func TestAssessmentRefinerBuildConvergencePrompt(t *testing.T) {
	supervisor := &Supervisor{}
	issue := &types.Issue{ID: "vc-test"}
	refiner, _ := NewAssessmentRefiner(supervisor, issue)

	current := &iterative.Artifact{
		Type:    "assessment",
		Content: "Current assessment with 5 risks identified",
		Context: "Iteration 2 context",
	}

	previous := &iterative.Artifact{
		Type:    "assessment",
		Content: "Previous assessment with 3 risks",
		Context: "Iteration 1 context",
	}

	prompt := refiner.buildConvergencePrompt(current, previous)

	// Check for key sections
	expectedStrings := []string{
		"converged",
		"ARTIFACT TYPE",
		"PREVIOUS VERSION",
		"Previous assessment with 3 risks",
		"CURRENT VERSION",
		"Current assessment with 5 risks",
		"CONTEXT",
		"Iteration 2 context",
		"Diff size",
		"Completeness",
		"Gaps",
		"Marginal value",
		"confidence",
		"reasoning",
	}

	for _, expected := range expectedStrings {
		if !contains(prompt, expected) {
			t.Errorf("convergence prompt missing expected string: %q", expected)
		}
	}
}

// TestAssessmentRefinerRefineNilArtifact tests error handling
func TestAssessmentRefinerRefineNilArtifact(t *testing.T) {
	supervisor := &Supervisor{}
	issue := &types.Issue{ID: "vc-test"}
	refiner, _ := NewAssessmentRefiner(supervisor, issue)

	_, err := refiner.Refine(context.Background(), nil)
	if err == nil {
		t.Error("expected error for nil artifact")
	}
	if !contains(err.Error(), "artifact cannot be nil") {
		t.Errorf("unexpected error message: %v", err)
	}
}

// TestSerializeAssessmentMinimal tests minimal assessment serialization
func TestSerializeAssessmentMinimal(t *testing.T) {
	assessment := &Assessment{
		Strategy:   "Simple strategy",
		Steps:      []string{},
		Risks:      []string{},
		Confidence: 0.5,
		Reasoning:  "Basic reasoning",
		ShouldDecompose: false,
	}

	serialized := serializeAssessment(assessment)

	// Check basic fields
	expectedStrings := []string{
		"Strategy: Simple strategy",
		"Confidence: 0.50",
		"Reasoning: Basic reasoning",
		"Should Decompose: false",
	}

	for _, expected := range expectedStrings {
		if !contains(serialized, expected) {
			t.Errorf("expected serialization to contain %q", expected)
		}
	}

	// Should NOT contain steps/risks sections when empty
	if contains(serialized, "Steps (") {
		t.Error("expected no steps section when empty")
	}
	if contains(serialized, "Risks (") {
		t.Error("expected no risks section when empty")
	}
}

// TestSelectivityMetrics tests that selectivity metrics are correctly recorded (vc-642z)
func TestSelectivityMetrics(t *testing.T) {
	t.Run("skipped artifact records skip reason", func(t *testing.T) {
		collector := iterative.NewInMemoryMetricsCollector()

		// Record a skipped artifact
		metrics := &iterative.ArtifactMetrics{
			ArtifactType:     "assessment",
			Priority:         "P2",
			TotalIterations:  0,
			Converged:        true,
			ConvergenceReason: "selectivity skip",
			IterationSkipped: true,
			SkipReason:       "simple issue (no complexity triggers)",
		}

		collector.RecordArtifactComplete(&iterative.ConvergenceResult{
			Iterations:  0,
			Converged:   true,
		}, metrics)

		agg := collector.GetAggregateMetrics()

		// Verify skip metrics
		if agg.SkippedArtifacts != 1 {
			t.Errorf("expected 1 skipped artifact, got %d", agg.SkippedArtifacts)
		}
		if agg.IteratedArtifacts != 0 {
			t.Errorf("expected 0 iterated artifacts, got %d", agg.IteratedArtifacts)
		}

		// Verify skip reason tracking
		skipCount, ok := agg.BySelectivityReason["simple issue (no complexity triggers)"]
		if !ok || skipCount != 1 {
			t.Errorf("expected skip reason count of 1, got %d", skipCount)
		}
	})

	t.Run("iterated artifact records triggers", func(t *testing.T) {
		collector := iterative.NewInMemoryMetricsCollector()

		// Record an iterated artifact with triggers
		metrics := &iterative.ArtifactMetrics{
			ArtifactType:        "assessment",
			Priority:            "P0",
			TotalIterations:     4,
			Converged:           true,
			ConvergenceReason:   "AI convergence",
			IterationSkipped:    false,
			SelectivityTriggers: []string{"P0 priority", "mission (complex structural issue)"},
		}

		collector.RecordArtifactComplete(&iterative.ConvergenceResult{
			Iterations:  4,
			Converged:   true,
		}, metrics)

		agg := collector.GetAggregateMetrics()

		// Verify iteration metrics
		if agg.SkippedArtifacts != 0 {
			t.Errorf("expected 0 skipped artifacts, got %d", agg.SkippedArtifacts)
		}
		if agg.IteratedArtifacts != 1 {
			t.Errorf("expected 1 iterated artifact, got %d", agg.IteratedArtifacts)
		}

		// Verify trigger tracking (each trigger counted independently)
		p0Count, ok := agg.BySelectivityTrigger["P0 priority"]
		if !ok || p0Count != 1 {
			t.Errorf("expected P0 priority trigger count of 1, got %d", p0Count)
		}

		missionCount, ok := agg.BySelectivityTrigger["mission (complex structural issue)"]
		if !ok || missionCount != 1 {
			t.Errorf("expected mission trigger count of 1, got %d", missionCount)
		}
	})

	t.Run("mixed artifacts track both skip and iterate", func(t *testing.T) {
		collector := iterative.NewInMemoryMetricsCollector()

		// Record 2 skipped and 1 iterated
		collector.RecordArtifactComplete(&iterative.ConvergenceResult{}, &iterative.ArtifactMetrics{
			ArtifactType:     "assessment",
			IterationSkipped: true,
			SkipReason:       "simple issue (no complexity triggers)",
		})

		collector.RecordArtifactComplete(&iterative.ConvergenceResult{}, &iterative.ArtifactMetrics{
			ArtifactType:     "assessment",
			IterationSkipped: true,
			SkipReason:       "simple issue (no complexity triggers)",
		})

		collector.RecordArtifactComplete(&iterative.ConvergenceResult{}, &iterative.ArtifactMetrics{
			ArtifactType:        "assessment",
			IterationSkipped:    false,
			SelectivityTriggers: []string{"P0 priority"},
		})

		agg := collector.GetAggregateMetrics()

		if agg.SkippedArtifacts != 2 {
			t.Errorf("expected 2 skipped artifacts, got %d", agg.SkippedArtifacts)
		}
		if agg.IteratedArtifacts != 1 {
			t.Errorf("expected 1 iterated artifact, got %d", agg.IteratedArtifacts)
		}
		if agg.TotalArtifacts != 3 {
			t.Errorf("expected 3 total artifacts, got %d", agg.TotalArtifacts)
		}
	})
}
