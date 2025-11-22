package ai

import (
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
