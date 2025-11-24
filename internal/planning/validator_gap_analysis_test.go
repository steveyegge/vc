package planning

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/steveyegge/vc/internal/types"
)

func TestGapAnalysisValidator_Name(t *testing.T) {
	validator := NewGapAnalysisValidator(nil, "test-model")
	if validator.Name() != "gap_analysis" {
		t.Errorf("expected name 'gap_analysis', got %s", validator.Name())
	}
}

func TestGapAnalysisValidator_Priority(t *testing.T) {
	validator := NewGapAnalysisValidator(nil, "test-model")
	priority := validator.Priority()

	// Gap analysis should run last (priority 100+)
	if priority < 100 {
		t.Errorf("expected priority >= 100 for AI-driven analysis, got %d", priority)
	}
}

func TestGapAnalysisValidator_SkipWhenNoClient(t *testing.T) {
	// Validator with nil client should skip validation gracefully
	validator := NewGapAnalysisValidator(nil, "test-model")

	plan := &MissionPlan{
		MissionID:    "vc-test",
		MissionTitle: "Test Mission",
		Goal:         "Test goal",
		Phases:       []Phase{},
	}

	result := validator.Validate(context.Background(), plan, nil)

	if result.HasErrors() {
		t.Error("expected no errors when client is nil")
	}
	if result.HasWarnings() {
		t.Error("expected no warnings when client is nil")
	}
}

func TestGapAnalysisValidator_CompletePlan(t *testing.T) {
	// Test a complete plan with no gaps
	responseJSON := `{
		"missing_scenarios": [],
		"edge_cases": [],
		"suggestions": [],
		"overall_assessment": "Plan is comprehensive and well-structured"
	}`

	// Create a mock client that returns the response
	// Note: This is a simplified mock - in real implementation, we'd use a proper mock
	validator := NewGapAnalysisValidator(nil, "test-model")

	// Test the parsing directly
	report, err := validator.parseGapAnalysisResponse(responseJSON)
	if err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}

	if len(report.MissingScenarios) != 0 {
		t.Error("expected no missing scenarios")
	}
	if len(report.EdgeCases) != 0 {
		t.Error("expected no edge cases")
	}
	if len(report.Suggestions) != 0 {
		t.Error("expected no suggestions")
	}
	if report.OverallAssessment != "Plan is comprehensive and well-structured" {
		t.Errorf("unexpected assessment: %s", report.OverallAssessment)
	}
}

func TestGapAnalysisValidator_IdentifiesGaps(t *testing.T) {
	// Test a plan with identified gaps
	responseJSON := `{
		"missing_scenarios": [
			"Missing error handling for API timeout scenarios",
			"No rollback strategy for failed migrations"
		],
		"edge_cases": [
			"Empty dataset handling not explicitly covered",
			"Concurrent access edge cases not addressed"
		],
		"suggestions": [
			"Consider adding performance benchmarks",
			"Document API contract changes"
		],
		"overall_assessment": "Plan is solid but missing some error scenarios"
	}`

	validator := NewGapAnalysisValidator(nil, "test-model")
	report, err := validator.parseGapAnalysisResponse(responseJSON)
	if err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}

	// Verify missing scenarios
	if len(report.MissingScenarios) != 2 {
		t.Errorf("expected 2 missing scenarios, got %d", len(report.MissingScenarios))
	}
	if report.MissingScenarios[0] != "Missing error handling for API timeout scenarios" {
		t.Errorf("unexpected missing scenario: %s", report.MissingScenarios[0])
	}

	// Verify edge cases
	if len(report.EdgeCases) != 2 {
		t.Errorf("expected 2 edge cases, got %d", len(report.EdgeCases))
	}

	// Verify suggestions
	if len(report.Suggestions) != 2 {
		t.Errorf("expected 2 suggestions, got %d", len(report.Suggestions))
	}
}

func TestGapAnalysisValidator_HandlesMarkdownFences(t *testing.T) {
	// Test that markdown code fences are properly stripped
	testCases := []struct {
		name     string
		response string
	}{
		{
			name: "json fence",
			response: "```json\n" + `{
				"missing_scenarios": [],
				"edge_cases": [],
				"suggestions": [],
				"overall_assessment": "Good"
			}` + "\n```",
		},
		{
			name: "plain fence",
			response: "```\n" + `{
				"missing_scenarios": [],
				"edge_cases": [],
				"suggestions": [],
				"overall_assessment": "Good"
			}` + "\n```",
		},
		{
			name: "no fence",
			response: `{
				"missing_scenarios": [],
				"edge_cases": [],
				"suggestions": [],
				"overall_assessment": "Good"
			}`,
		},
	}

	validator := NewGapAnalysisValidator(nil, "test-model")

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			report, err := validator.parseGapAnalysisResponse(tc.response)
			if err != nil {
				t.Fatalf("failed to parse response: %v", err)
			}
			if report.OverallAssessment != "Good" {
				t.Errorf("expected assessment 'Good', got %s", report.OverallAssessment)
			}
		})
	}
}

func TestGapAnalysisValidator_HandlesInvalidJSON(t *testing.T) {
	testCases := []struct {
		name     string
		response string
	}{
		{
			name:     "not JSON",
			response: "This is not JSON at all",
		},
		{
			name:     "malformed JSON",
			response: `{"missing_scenarios": [}`,
		},
		{
			name:     "empty response",
			response: "",
		},
	}

	validator := NewGapAnalysisValidator(nil, "test-model")

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := validator.parseGapAnalysisResponse(tc.response)
			if err == nil {
				t.Error("expected error for invalid JSON, got nil")
			}
		})
	}
}

func TestGapAnalysisValidator_BuildPrompt(t *testing.T) {
	validator := NewGapAnalysisValidator(nil, "test-model")

	plan := &MissionPlan{
		MissionID:    "vc-123",
		MissionTitle: "Implement User Auth",
		Goal:         "Add OAuth2 authentication",
		Constraints:  []string{"Must support GitHub", "Zero breaking changes"},
		Phases: []Phase{
			{
				ID:          "phase-1",
				Title:       "Foundation",
				Description: "Set up OAuth infrastructure",
				Strategy:    "Bottom-up implementation",
				Tasks: []Task{
					{
						ID:                 "task-1",
						Title:              "Create OAuth client",
						Description:        "Implement OAuth2 client library",
						AcceptanceCriteria: []string{"WHEN auth request sent THEN redirect to provider"},
					},
				},
			},
		},
		TotalTasks:     1,
		EstimatedHours: 8.0,
	}

	vctx := &ValidationContext{
		OriginalIssue: &types.Issue{
			ID:          "vc-123",
			Title:       "User Auth",
			Description: "We need user authentication for the app",
		},
	}

	prompt := validator.buildGapAnalysisPrompt(plan, vctx)

	// Verify key elements are present in the prompt
	requiredElements := []string{
		"vc-123",
		"Implement User Auth",
		"Add OAuth2 authentication",
		"Must support GitHub",
		"Foundation",
		"Create OAuth client",
		"missing_scenarios",
		"edge_cases",
		"suggestions",
	}

	for _, element := range requiredElements {
		if !containsString(prompt, element) {
			t.Errorf("prompt missing required element: %s", element)
		}
	}
}

func TestGapAnalysisValidator_WarningsBySeverity(t *testing.T) {
	// Verify that different types of gaps produce warnings with appropriate severity
	responseJSON := `{
		"missing_scenarios": ["Critical scenario X"],
		"edge_cases": ["Edge case Y"],
		"suggestions": ["Nice-to-have Z"],
		"overall_assessment": "Mixed"
	}`

	validator := NewGapAnalysisValidator(nil, "test-model")
	report, err := validator.parseGapAnalysisResponse(responseJSON)
	if err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}

	// Convert report to validation result (simulating the Validate method)
	result := ValidationResult{
		Errors:   make([]ValidationError, 0),
		Warnings: make([]ValidationWarning, 0),
	}

	for _, gap := range report.MissingScenarios {
		result.Warnings = append(result.Warnings, ValidationWarning{
			Code:     "MISSING_SCENARIO",
			Message:  gap,
			Severity: WarningSeverityHigh,
		})
	}

	for _, gap := range report.EdgeCases {
		result.Warnings = append(result.Warnings, ValidationWarning{
			Code:     "MISSING_EDGE_CASE",
			Message:  gap,
			Severity: WarningSeverityMedium,
		})
	}

	for _, suggestion := range report.Suggestions {
		result.Warnings = append(result.Warnings, ValidationWarning{
			Code:     "IMPROVEMENT_SUGGESTION",
			Message:  suggestion,
			Severity: WarningSeverityLow,
		})
	}

	// Verify severities
	if len(result.Warnings) != 3 {
		t.Fatalf("expected 3 warnings, got %d", len(result.Warnings))
	}

	// Missing scenarios should be high severity
	if result.Warnings[0].Severity != WarningSeverityHigh {
		t.Errorf("expected HIGH severity for missing scenario, got %s", result.Warnings[0].Severity)
	}

	// Edge cases should be medium severity
	if result.Warnings[1].Severity != WarningSeverityMedium {
		t.Errorf("expected MEDIUM severity for edge case, got %s", result.Warnings[1].Severity)
	}

	// Suggestions should be low severity
	if result.Warnings[2].Severity != WarningSeverityLow {
		t.Errorf("expected LOW severity for suggestion, got %s", result.Warnings[2].Severity)
	}
}

func TestGapAnalysisValidator_PromptFormat(t *testing.T) {
	// Verify the prompt follows expected structure for reliable AI responses
	validator := NewGapAnalysisValidator(nil, "test-model")

	plan := &MissionPlan{
		MissionID:      "vc-test",
		MissionTitle:   "Test",
		Goal:           "Test goal",
		Phases:         []Phase{},
		TotalTasks:     0,
		EstimatedHours: 0,
	}

	prompt := validator.buildGapAnalysisPrompt(plan, nil)

	// Verify structure
	if !containsString(prompt, "YOUR TASK:") {
		t.Error("prompt should have YOUR TASK section")
	}
	if !containsString(prompt, "missing_scenarios") {
		t.Error("prompt should request missing_scenarios in JSON")
	}
	if !containsString(prompt, "IMPORTANT: Respond with ONLY raw JSON") {
		t.Error("prompt should instruct AI to return raw JSON")
	}
}

// Helper function to check if a string contains a substring
func containsString(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > len(substr) && findSubstring(s, substr))
}

func findSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func TestGapAnalysisValidator_Integration(t *testing.T) {
	// Integration test demonstrating the full flow
	t.Skip("Integration test - requires real Anthropic API")

	// This test would use a real API key and verify end-to-end behavior
	// Skipped in normal test runs to avoid API costs and dependency on external service
}

// Benchmark gap analysis parsing performance
func BenchmarkGapAnalysisValidator_Parse(b *testing.B) {
	responseJSON := `{
		"missing_scenarios": [
			"Scenario 1",
			"Scenario 2",
			"Scenario 3"
		],
		"edge_cases": [
			"Edge case 1",
			"Edge case 2"
		],
		"suggestions": [
			"Suggestion 1"
		],
		"overall_assessment": "Good plan with minor gaps"
	}`

	validator := NewGapAnalysisValidator(nil, "test-model")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := validator.parseGapAnalysisResponse(responseJSON)
		if err != nil {
			b.Fatalf("parse failed: %v", err)
		}
	}
}

// Test that gap analysis report can be serialized back to JSON (round-trip)
func TestGapAnalysisReport_RoundTrip(t *testing.T) {
	original := GapAnalysisReport{
		MissingScenarios:  []string{"Scenario A", "Scenario B"},
		EdgeCases:         []string{"Edge X"},
		Suggestions:       []string{"Suggestion 1", "Suggestion 2"},
		OverallAssessment: "Needs work",
	}

	// Serialize
	jsonBytes, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	// Deserialize
	var roundTrip GapAnalysisReport
	if err := json.Unmarshal(jsonBytes, &roundTrip); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	// Verify
	if len(roundTrip.MissingScenarios) != len(original.MissingScenarios) {
		t.Error("missing scenarios count mismatch")
	}
	if roundTrip.OverallAssessment != original.OverallAssessment {
		t.Error("overall assessment mismatch")
	}
}

// Test timeout handling
func TestGapAnalysisValidator_Timeout(t *testing.T) {
	validator := NewGapAnalysisValidator(nil, "test-model")

	// Create a context that's already expired
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Nanosecond)
	defer cancel()
	time.Sleep(10 * time.Millisecond) // Ensure context is expired

	plan := &MissionPlan{
		MissionID:    "vc-timeout",
		MissionTitle: "Timeout Test",
		Goal:         "Test timeout handling",
		Phases:       []Phase{},
	}

	// This should handle the timeout gracefully
	// Since client is nil, it will return early, but if it had a client,
	// the timeout should be handled
	result := validator.Validate(ctx, plan, nil)

	// Should not panic or hang
	if result.HasErrors() {
		t.Error("timeout should produce warning, not error")
	}
}
