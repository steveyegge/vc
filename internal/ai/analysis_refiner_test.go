package ai

import (
	"context"
	"testing"

	"github.com/steveyegge/vc/internal/iterative"
	"github.com/steveyegge/vc/internal/types"
)

// TestNewAnalysisRefiner tests refiner creation
func TestNewAnalysisRefiner(t *testing.T) {
	// Create test issue
	issue := &types.Issue{
		ID:    "test-1",
		Title: "Test Issue",
		Description: "Test description",
		AcceptanceCriteria: "1. Should work\n2. Should be tested",
	}

	tests := []struct {
		name        string
		supervisor  *Supervisor
		issue       *types.Issue
		agentOutput string
		success     bool
		wantErr     bool
	}{
		{
			name:        "valid refiner creation",
			supervisor:  &Supervisor{}, // Mock supervisor (won't actually call API in this test)
			issue:       issue,
			agentOutput: "test output",
			success:     true,
			wantErr:     false,
		},
		{
			name:        "nil supervisor",
			supervisor:  nil,
			issue:       issue,
			agentOutput: "test output",
			success:     true,
			wantErr:     true,
		},
		{
			name:        "nil issue",
			supervisor:  &Supervisor{},
			issue:       nil,
			agentOutput: "test output",
			success:     true,
			wantErr:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			refiner, err := NewAnalysisRefiner(tt.supervisor, tt.issue, tt.agentOutput, tt.success)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewAnalysisRefiner() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && refiner == nil {
				t.Error("NewAnalysisRefiner() returned nil refiner without error")
			}
			if !tt.wantErr {
				if refiner.supervisor != tt.supervisor {
					t.Error("Refiner supervisor not set correctly")
				}
				if refiner.issue != tt.issue {
					t.Error("Refiner issue not set correctly")
				}
				if refiner.minConfidence <= 0 {
					t.Error("Refiner minConfidence not set")
				}
			}
		})
	}
}

// TestSerializeAnalysis tests analysis serialization
func TestSerializeAnalysis(t *testing.T) {
	analysis := &Analysis{
		Completed:  true,
		Confidence: 0.95,
		Summary:    "Test summary",
		PuntedItems: []string{"Item 1", "Item 2"},
		DiscoveredIssues: []DiscoveredIssue{
			{
				Title:         "Bug found",
				Description:   "A bug was discovered",
				Type:          "bug",
				Priority:      "P1",
				DiscoveryType: "blocker",
			},
		},
		QualityIssues: []string{"Missing tests"},
		ScopeValidation: &ScopeValidation{
			OnTask:      true,
			Explanation: "Agent worked on correct task",
		},
	}

	serialized := serializeAnalysis(analysis)

	// Check that key elements are present in serialization
	if serialized == "" {
		t.Fatal("serializeAnalysis() returned empty string")
	}

	// Check for key markers
	expectedStrings := []string{
		"Completed: true",
		"Confidence: 0.95",
		"Test summary",
		"Punted Items (2)",
		"Item 1",
		"Discovered Issues (1)",
		"Bug found",
		"Quality Issues (1)",
		"Missing tests",
		"Scope Validation",
		"On Task: true",
	}

	for _, expected := range expectedStrings {
		if !contains(serialized, expected) {
			t.Errorf("serializeAnalysis() missing expected string: %q\nGot: %s", expected, serialized)
		}
	}
}

// TestAnalysisRefinerBuildRefinementPrompt tests prompt construction
func TestAnalysisRefinerBuildRefinementPrompt(t *testing.T) {
	issue := &types.Issue{
		ID:                 "test-1",
		Title:              "Test Issue",
		Description:        "Test description",
		AcceptanceCriteria: "1. Should work",
	}

	refiner := &AnalysisRefiner{
		supervisor:  &Supervisor{},
		issue:       issue,
		agentOutput: "Agent completed the work successfully",
		success:     true,
	}

	artifact := &iterative.Artifact{
		Type:    "analysis",
		Content: "Previous analysis content",
		Context: "Some context",
	}

	prompt := refiner.buildRefinementPrompt(artifact)

	// Check that prompt contains key elements
	expectedStrings := []string{
		"ITERATIVE REFINEMENT",
		"test-1",
		"Test Issue",
		"Test description",
		"1. Should work",
		"succeeded",
		"Agent completed the work successfully",
		"Previous analysis content",
		"Some context",
		"Discovered Issues",
		"Punted Items",
		"Quality Issues",
		"Scope Validation",
	}

	for _, expected := range expectedStrings {
		if !contains(prompt, expected) {
			t.Errorf("buildRefinementPrompt() missing expected string: %q", expected)
		}
	}
}

// TestAnalysisRefinerBuildConvergencePrompt tests convergence prompt construction
func TestAnalysisRefinerBuildConvergencePrompt(t *testing.T) {
	refiner := &AnalysisRefiner{
		supervisor: &Supervisor{},
		issue:      &types.Issue{ID: "test-1"},
	}

	current := &iterative.Artifact{
		Type:    "analysis",
		Content: "Current analysis with 3 discovered issues",
		Context: "Found more issues in iteration 2",
	}

	previous := &iterative.Artifact{
		Type:    "analysis",
		Content: "Previous analysis with 1 discovered issue",
		Context: "Initial iteration",
	}

	prompt := refiner.buildConvergencePrompt(current, previous)

	// Check that prompt contains key elements
	expectedStrings := []string{
		"converged",
		"PREVIOUS VERSION",
		"Previous analysis with 1 discovered issue",
		"CURRENT VERSION",
		"Current analysis with 3 discovered issues",
		"CONTEXT",
		"Found more issues in iteration 2",
		"Diff size",
		"Completeness",
		"Gaps",
		"Marginal value",
		"confidence",
	}

	for _, expected := range expectedStrings {
		if !contains(prompt, expected) {
			t.Errorf("buildConvergencePrompt() missing expected string: %q", expected)
		}
	}
}

// TestAnalysisRefinerBuildIterationContext tests iteration context building
func TestAnalysisRefinerBuildIterationContext(t *testing.T) {
	refiner := &AnalysisRefiner{
		supervisor: &Supervisor{},
		issue:      &types.Issue{ID: "test-1"},
	}

	artifact := &iterative.Artifact{
		Type:    "analysis",
		Content: "previous content",
		Context: "initial context",
	}

	analysis := &Analysis{
		Completed:  true,
		Confidence: 0.90,
		DiscoveredIssues: []DiscoveredIssue{
			{Title: "Bug 1", Type: "bug", Priority: "P1", DiscoveryType: "blocker"},
			{Title: "Bug 2", Type: "bug", Priority: "P2", DiscoveryType: "related"},
		},
		PuntedItems:   []string{"Item 1", "Item 2"},
		QualityIssues: []string{"Issue 1"},
	}

	context := refiner.buildIterationContext(artifact, analysis)

	// Check that context includes previous context
	if !contains(context, "initial context") {
		t.Error("expected context to include previous context")
	}

	// Check for key statistics
	expectedStrings := []string{
		"Previous iteration found:",
		"Completed: true",
		"Discovered issues: 2",
		"Punted items: 2",
		"Quality issues: 1",
		"Discovered issues found:",
		"1. Bug 1 (type=bug, priority=P1, discovery=blocker)",
		"2. Bug 2 (type=bug, priority=P2, discovery=related)",
	}

	for _, expected := range expectedStrings {
		if !contains(context, expected) {
			t.Errorf("buildIterationContext() missing expected string: %q\nGot: %s", expected, context)
		}
	}
}

// TestAnalysisRefinerBuildIterationContextEmpty tests empty context
func TestAnalysisRefinerBuildIterationContextEmpty(t *testing.T) {
	refiner := &AnalysisRefiner{
		supervisor: &Supervisor{},
		issue:      &types.Issue{ID: "test-1"},
	}

	artifact := &iterative.Artifact{
		Type:    "analysis",
		Content: "content",
		Context: "", // Empty context
	}

	analysis := &Analysis{
		Completed:        false,
		DiscoveredIssues: []DiscoveredIssue{},
		PuntedItems:      []string{},
		QualityIssues:    []string{},
	}

	context := refiner.buildIterationContext(artifact, analysis)

	// Check that it still generates useful output
	expectedStrings := []string{
		"Previous iteration found:",
		"Completed: false",
		"Discovered issues: 0",
		"Punted items: 0",
		"Quality issues: 0",
	}

	for _, expected := range expectedStrings {
		if !contains(context, expected) {
			t.Errorf("buildIterationContext() missing expected string: %q", expected)
		}
	}

	// Should NOT contain "Discovered issues found:" section
	if contains(context, "Discovered issues found:") {
		t.Error("buildIterationContext() should not contain discovered issues section when empty")
	}
}

// TestDeserializeAnalysis tests the not-implemented path
func TestDeserializeAnalysis(t *testing.T) {
	artifact := &iterative.Artifact{
		Type:    "analysis",
		Content: "some content",
	}

	analysis, err := deserializeAnalysis(artifact)
	if err == nil {
		t.Error("expected error from deserializeAnalysis (not implemented)")
	}
	if analysis != nil {
		t.Error("expected nil analysis from deserializeAnalysis")
	}
}

// TestSerializeAnalysisEdgeCases tests serialization edge cases
func TestSerializeAnalysisEdgeCases(t *testing.T) {
	tests := []struct {
		name     string
		analysis *Analysis
		expected []string
		notExpected []string
	}{
		{
			name: "empty analysis",
			analysis: &Analysis{
				Completed:  false,
				Confidence: 0.0,
			},
			expected: []string{
				"Completed: false",
				"Confidence: 0.00",
			},
			notExpected: []string{
				"Scope Validation",
				"Punted Items",
				"Discovered Issues",
				"Quality Issues",
			},
		},
		{
			name: "with acceptance criteria",
			analysis: &Analysis{
				Completed:  true,
				Confidence: 1.0,
				AcceptanceCriteriaMet: map[string]*CriterionResult{
					"criterion1": {Met: true, Evidence: "evidence", Reason: "reason"},
					"criterion2": {Met: false, Evidence: "", Reason: "not met"},
				},
			},
			expected: []string{
				"Acceptance Criteria:",
				"criterion1: met=true",
				"criterion2: met=false",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			serialized := serializeAnalysis(tt.analysis)

			for _, expected := range tt.expected {
				if !contains(serialized, expected) {
					t.Errorf("expected serialization to contain %q\nGot: %s", expected, serialized)
				}
			}

			for _, notExpected := range tt.notExpected {
				if contains(serialized, notExpected) {
					t.Errorf("expected serialization NOT to contain %q", notExpected)
				}
			}
		})
	}
}

// TestAnalysisRefinerRefineNilArtifact tests error handling for nil artifact
func TestAnalysisRefinerRefineNilArtifact(t *testing.T) {
	refiner := &AnalysisRefiner{
		supervisor: &Supervisor{},
		issue:      &types.Issue{ID: "test-1"},
	}

	_, err := refiner.Refine(context.Background(), nil)
	if err == nil {
		t.Error("expected error for nil artifact")
	}
	if !contains(err.Error(), "artifact cannot be nil") {
		t.Errorf("unexpected error message: %v", err)
	}
}

// TestCheckConvergencePromptContent tests convergence prompt structure
func TestCheckConvergencePromptContent(t *testing.T) {
	refiner := &AnalysisRefiner{
		supervisor: &Supervisor{},
		issue:      &types.Issue{ID: "test-1"},
	}

	current := &iterative.Artifact{
		Type:    "analysis",
		Content: "Current version with many details that should be visible in the prompt",
		Context: "Iteration 3 context information",
	}

	previous := &iterative.Artifact{
		Type:    "analysis",
		Content: "Previous version with fewer details",
		Context: "Iteration 2 context",
	}

	prompt := refiner.buildConvergencePrompt(current, previous)

	// Verify all key sections are present
	requiredSections := []string{
		"converged",
		"PREVIOUS VERSION",
		"CURRENT VERSION",
		"CONTEXT",
		"Diff size",
		"Completeness",
		"Gaps",
		"Marginal value",
		"confidence",
		"Previous version with fewer details",
		"Current version with many details",
		"Iteration 3 context information",
	}

	for _, section := range requiredSections {
		if !contains(prompt, section) {
			t.Errorf("convergence prompt missing required section: %q", section)
		}
	}
}

// TestTruncateForPrompt tests prompt truncation
func TestTruncateForPrompt(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		maxChars int
		wantLen  int
	}{
		{
			name:     "short text unchanged",
			input:    "short",
			maxChars: 100,
			wantLen:  5,
		},
		{
			name:     "long text truncated",
			input:    string(make([]byte, 5000)),
			maxChars: 1000,
			wantLen:  1015, // includes "...[truncated]" suffix (actual measured length)
		},
		{
			name:     "empty text",
			input:    "",
			maxChars: 100,
			wantLen:  0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := truncateForPrompt(tt.input, tt.maxChars)
			if len(result) != tt.wantLen {
				t.Errorf("truncateForPrompt() length = %d, want %d", len(result), tt.wantLen)
			}
			if tt.wantLen > tt.maxChars && !contains(result, "truncated") {
				t.Error("expected truncated text to contain truncation marker")
			}
		})
	}
}

// Helper function to check if string contains substring
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > len(substr) && containsHelper(s, substr))
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
