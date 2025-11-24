package planning

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
)

// GapAnalysisValidator uses AI to detect missing edge cases or coverage gaps in mission plans.
// This is a meta-validator that reviews the overall plan for completeness and quality.
type GapAnalysisValidator struct {
	client *anthropic.Client
	model  string
}

// NewGapAnalysisValidator creates a new AI-driven gap analysis validator.
func NewGapAnalysisValidator(client *anthropic.Client, model string) *GapAnalysisValidator {
	return &GapAnalysisValidator{
		client: client,
		model:  model,
	}
}

// Name returns the validator identifier.
func (v *GapAnalysisValidator) Name() string {
	return "gap_analysis"
}

// Priority returns 100 (runs after all structural and content checks).
// AI-driven analysis should run last to avoid wasting tokens on invalid plans.
func (v *GapAnalysisValidator) Priority() int {
	return 100
}

// Validate uses AI to identify gaps in the mission plan.
func (v *GapAnalysisValidator) Validate(ctx context.Context, plan *MissionPlan, vctx *ValidationContext) ValidationResult {
	result := ValidationResult{
		Errors:   make([]ValidationError, 0),
		Warnings: make([]ValidationWarning, 0),
	}

	// Skip if client not configured (e.g., tests without API key)
	if v.client == nil {
		return result
	}

	// Build the gap analysis prompt
	prompt := v.buildGapAnalysisPrompt(plan, vctx)

	// Add timeout for API call
	apiCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	// Call Anthropic API
	response, err := v.client.Messages.New(apiCtx, anthropic.MessageNewParams{
		Model:     anthropic.Model(v.model),
		MaxTokens: 4096, // Larger limit for detailed gap analysis
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock(prompt)),
		},
	})

	if err != nil {
		// AI failure shouldn't block validation - log warning and continue
		result.Warnings = append(result.Warnings, ValidationWarning{
			Code:     "GAP_ANALYSIS_FAILED",
			Message:  fmt.Sprintf("AI gap analysis failed: %v", err),
			Location: "plan",
			Severity: WarningSeverityLow,
		})
		return result
	}

	// Extract response text
	var responseText string
	for _, block := range response.Content {
		if block.Type == "text" {
			responseText += block.Text
		}
	}

	// Parse the AI response
	gapReport, parseErr := v.parseGapAnalysisResponse(responseText)
	if parseErr != nil {
		result.Warnings = append(result.Warnings, ValidationWarning{
			Code:     "GAP_ANALYSIS_PARSE_ERROR",
			Message:  fmt.Sprintf("Failed to parse gap analysis: %v", parseErr),
			Location: "plan",
			Severity: WarningSeverityLow,
		})
		return result
	}

	// Convert AI findings to validation warnings
	for _, gap := range gapReport.MissingScenarios {
		result.Warnings = append(result.Warnings, ValidationWarning{
			Code:     "MISSING_SCENARIO",
			Message:  gap,
			Location: "plan",
			Severity: WarningSeverityHigh,
		})
	}

	for _, gap := range gapReport.EdgeCases {
		result.Warnings = append(result.Warnings, ValidationWarning{
			Code:     "MISSING_EDGE_CASE",
			Message:  gap,
			Location: "plan",
			Severity: WarningSeverityMedium,
		})
	}

	for _, suggestion := range gapReport.Suggestions {
		result.Warnings = append(result.Warnings, ValidationWarning{
			Code:     "IMPROVEMENT_SUGGESTION",
			Message:  suggestion,
			Location: "plan",
			Severity: WarningSeverityLow,
		})
	}

	return result
}

// GapAnalysisReport represents the AI's gap analysis findings.
type GapAnalysisReport struct {
	// MissingScenarios are critical scenarios that should be covered but aren't.
	MissingScenarios []string `json:"missing_scenarios"`

	// EdgeCases are edge cases that should be considered but aren't explicitly addressed.
	EdgeCases []string `json:"edge_cases"`

	// Suggestions are recommendations for improving the plan.
	Suggestions []string `json:"suggestions"`

	// OverallAssessment is a brief summary of the plan's completeness.
	OverallAssessment string `json:"overall_assessment"`
}

// buildGapAnalysisPrompt builds the AI prompt for gap analysis.
func (v *GapAnalysisValidator) buildGapAnalysisPrompt(plan *MissionPlan, vctx *ValidationContext) string {
	var sb strings.Builder

	sb.WriteString("You are reviewing a mission plan for completeness and quality.\n\n")

	// Mission context
	sb.WriteString(fmt.Sprintf("MISSION ID: %s\n", plan.MissionID))
	sb.WriteString(fmt.Sprintf("MISSION: %s\n", plan.MissionTitle))
	sb.WriteString(fmt.Sprintf("GOAL: %s\n\n", plan.Goal))

	// Constraints
	if len(plan.Constraints) > 0 {
		sb.WriteString("CONSTRAINTS:\n")
		for _, c := range plan.Constraints {
			sb.WriteString(fmt.Sprintf("- %s\n", c))
		}
		sb.WriteString("\n")
	}

	// Original issue context (if available)
	if vctx != nil && vctx.OriginalIssue != nil {
		if vctx.OriginalIssue.Description != "" {
			sb.WriteString(fmt.Sprintf("ORIGINAL DESCRIPTION:\n%s\n\n", vctx.OriginalIssue.Description))
		}
	}

	// Plan summary
	sb.WriteString("PLAN OVERVIEW:\n")
	sb.WriteString(fmt.Sprintf("- Total Phases: %d\n", len(plan.Phases)))
	sb.WriteString(fmt.Sprintf("- Total Tasks: %d\n", plan.TotalTasks))
	sb.WriteString(fmt.Sprintf("- Estimated Effort: %.1f hours\n\n", plan.EstimatedHours))

	// Phases detail
	sb.WriteString("PHASES:\n")
	for i, phase := range plan.Phases {
		sb.WriteString(fmt.Sprintf("\nPhase %d: %s\n", i+1, phase.Title))
		sb.WriteString(fmt.Sprintf("Description: %s\n", phase.Description))
		sb.WriteString(fmt.Sprintf("Strategy: %s\n", phase.Strategy))
		sb.WriteString(fmt.Sprintf("Tasks (%d):\n", len(phase.Tasks)))
		for j, task := range phase.Tasks {
			sb.WriteString(fmt.Sprintf("  %d. %s\n", j+1, task.Title))
			if len(task.AcceptanceCriteria) > 0 {
				sb.WriteString(fmt.Sprintf("     AC: %s\n", strings.Join(task.AcceptanceCriteria, "; ")))
			}
		}
	}

	sb.WriteString("\n")

	// Analysis instructions
	sb.WriteString(`YOUR TASK:
Identify gaps in this mission plan. Consider:

1. **Missing Scenarios**: Are there critical user flows or use cases not covered?
   - Error handling paths (what if API fails? what if data is invalid?)
   - Edge cases (empty inputs, large datasets, concurrent access)
   - Recovery scenarios (rollback, retry, fallback)

2. **Coverage Gaps**: Are there areas of the codebase that should be touched but aren't?
   - Database migrations (if schema changes)
   - API contract changes (if interfaces change)
   - Documentation updates (if behavior changes)
   - Integration tests (if components interact)

3. **Risk Areas**: Are there high-risk areas that need extra attention?
   - Security implications (authentication, authorization, input validation)
   - Performance concerns (n+1 queries, memory leaks, scaling)
   - Backward compatibility (breaking changes, deprecation)

4. **Missing Dependencies**: Are there obvious dependencies or prerequisites not mentioned?
   - External services or libraries
   - Database setup or seed data
   - Configuration or environment variables

Provide your analysis as a JSON object:
{
  "missing_scenarios": [
    "Specific missing scenario 1",
    "Specific missing scenario 2"
  ],
  "edge_cases": [
    "Edge case 1 not explicitly handled",
    "Edge case 2 not explicitly handled"
  ],
  "suggestions": [
    "Suggestion for improvement 1",
    "Suggestion for improvement 2"
  ],
  "overall_assessment": "Brief summary of plan completeness (1-2 sentences)"
}

Guidelines:
- Be specific: "Missing error handling for API timeout" not "Needs better error handling"
- Focus on gaps, not nitpicks: only flag things that could cause real problems
- If the plan is comprehensive, return empty arrays (don't invent problems)
- Prioritize: missing_scenarios > edge_cases > suggestions

IMPORTANT: Respond with ONLY raw JSON. Do NOT wrap in markdown code fences. Just the JSON object.
`)

	return sb.String()
}

// parseGapAnalysisResponse parses the AI's JSON response into a GapAnalysisReport.
func (v *GapAnalysisValidator) parseGapAnalysisResponse(responseText string) (*GapAnalysisReport, error) {
	// Note: We can't directly import the ai package here to avoid circular dependencies.
	// The planning package is used by the ai package, so we use encoding/json directly.
	// This is acceptable since the AI is prompted to return clean JSON.

	var report GapAnalysisReport

	// Strip markdown fences if present
	cleaned := strings.TrimSpace(responseText)
	cleaned = strings.TrimPrefix(cleaned, "```json")
	cleaned = strings.TrimPrefix(cleaned, "```")
	cleaned = strings.TrimSuffix(cleaned, "```")
	cleaned = strings.TrimSpace(cleaned)

	// Simple validation: check if it looks like JSON
	if !strings.HasPrefix(cleaned, "{") {
		return nil, fmt.Errorf("response doesn't appear to be JSON: %s", cleaned[:min(50, len(cleaned))])
	}

	// Parse JSON
	if err := json.Unmarshal([]byte(cleaned), &report); err != nil {
		return nil, fmt.Errorf("failed to parse gap analysis JSON: %w", err)
	}

	return &report, nil
}

// min returns the minimum of two integers.
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
