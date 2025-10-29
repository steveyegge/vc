package ai

import (
	"context"
	"strings"
	"testing"

	"github.com/steveyegge/vc/internal/types"
)

// TestDiagnoseTestFailure_InputValidation tests input validation (vc-225)
func TestDiagnoseTestFailure_InputValidation(t *testing.T) {
	// Create a minimal supervisor for testing
	supervisor := &Supervisor{
		model: "claude-sonnet-4-5-20250929",
	}

	ctx := context.Background()

	t.Run("nil issue returns error", func(t *testing.T) {
		_, err := supervisor.DiagnoseTestFailure(ctx, nil, "test output")
		if err == nil {
			t.Fatal("expected error for nil issue, got nil")
		}
		if !strings.Contains(err.Error(), "issue cannot be nil") {
			t.Errorf("expected 'issue cannot be nil' error, got: %v", err)
		}
	})

	t.Run("empty test output returns error", func(t *testing.T) {
		issue := &types.Issue{
			ID:    "vc-baseline-test",
			Title: "Fix baseline test failures",
		}
		_, err := supervisor.DiagnoseTestFailure(ctx, issue, "")
		if err == nil {
			t.Fatal("expected error for empty test output, got nil")
		}
		if !strings.Contains(err.Error(), "test output cannot be empty") {
			t.Errorf("expected 'test output cannot be empty' error, got: %v", err)
		}
	})

	// Note: We don't test large output truncation here because it would require
	// a full supervisor setup with API client. The truncation logic is simple
	// and verified by code review. If we need to test it, we should add a
	// separate unit test that calls buildTestFailureDiagnosisPrompt directly.
}

// TestBuildTestFailureDiagnosisPrompt tests the prompt builder
func TestBuildTestFailureDiagnosisPrompt(t *testing.T) {
	supervisor := &Supervisor{
		model: "claude-sonnet-4-5-20250929",
	}

	issue := &types.Issue{
		ID:          "vc-baseline-test",
		Title:       "Fix baseline test failures",
		Description: "Tests are failing in CI",
	}

	testOutput := "--- FAIL: TestExample (0.00s)\n    example_test.go:10: expected 42, got 43"

	prompt := supervisor.buildTestFailureDiagnosisPrompt(issue, testOutput)

	// Verify prompt contains key elements
	if !strings.Contains(prompt, issue.ID) {
		t.Error("prompt should contain issue ID")
	}
	if !strings.Contains(prompt, issue.Title) {
		t.Error("prompt should contain issue title")
	}
	if !strings.Contains(prompt, issue.Description) {
		t.Error("prompt should contain issue description")
	}
	if !strings.Contains(prompt, testOutput) {
		t.Error("prompt should contain test output")
	}
	if !strings.Contains(prompt, "failure_type") {
		t.Error("prompt should describe the JSON schema")
	}
	if !strings.Contains(prompt, "flaky|real|environmental") {
		t.Error("prompt should specify failure types")
	}
}

// TestFailureType_Constants verifies the FailureType enum values
func TestFailureType_Constants(t *testing.T) {
	tests := []struct {
		name     string
		value    FailureType
		expected string
	}{
		{"flaky", FailureTypeFlaky, "flaky"},
		{"real", FailureTypeReal, "real"},
		{"environmental", FailureTypeEnvironmental, "environmental"},
		{"unknown", FailureTypeUnknown, "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if string(tt.value) != tt.expected {
				t.Errorf("expected %s, got %s", tt.expected, string(tt.value))
			}
		})
	}
}
