package ai

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/steveyegge/vc/internal/iterative"
	"github.com/steveyegge/vc/internal/types"
)

// PlanRefiner implements the iterative.Refiner interface for mission plans.
// It bridges the planning system with the iterative convergence framework,
// allowing plans to be refined through multiple AI iterations until they
// converge to a stable, high-quality state.
type PlanRefiner struct {
	supervisor    *Supervisor
	planningCtx   *types.PlanningContext
	currentIter   int
}

// NewPlanRefiner creates a new plan refiner.
func NewPlanRefiner(supervisor *Supervisor, planningCtx *types.PlanningContext) *PlanRefiner {
	return &PlanRefiner{
		supervisor:  supervisor,
		planningCtx: planningCtx,
		currentIter: 0,
	}
}

// Refine performs one refinement pass on a mission plan.
// It takes the current plan, asks the AI to review and improve it,
// and returns the refined version.
func (r *PlanRefiner) Refine(ctx context.Context, artifact *iterative.Artifact) (*iterative.Artifact, error) {
	r.currentIter++

	// Deserialize the current plan from the artifact
	var currentPlan types.MissionPlan
	if err := json.Unmarshal([]byte(artifact.Content), &currentPlan); err != nil {
		return nil, fmt.Errorf("failed to deserialize plan: %w", err)
	}

	// Build refinement prompt
	prompt := r.buildRefinementPrompt(&currentPlan, artifact.Context)

	// Call AI with retry logic
	var response *anthropic.Message
	err := r.supervisor.retryWithBackoff(ctx, "plan-refinement", func(attemptCtx context.Context) error {
		resp, apiErr := r.supervisor.client.Messages.New(attemptCtx, anthropic.MessageNewParams{
			Model:     anthropic.Model(r.supervisor.model),
			MaxTokens: 8192,
			Messages: []anthropic.MessageParam{
				anthropic.NewUserMessage(anthropic.NewTextBlock(prompt)),
			},
		})
		if apiErr != nil {
			return apiErr
		}
		response = resp
		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("anthropic API call failed: %w", err)
	}

	// Extract text from response
	var responseText string
	for _, block := range response.Content {
		if block.Type == "text" {
			responseText += block.Text
		}
	}

	// Parse the refined plan
	parseResult := Parse[types.MissionPlan](responseText, ParseOptions{
		Context:   "plan refinement response",
		LogErrors: boolPtr(true),
	})

	if !parseResult.Success {
		return nil, fmt.Errorf("failed to parse refined plan: %s (response: %s)",
			parseResult.Error, truncateString(responseText, 200))
	}

	refinedPlan := parseResult.Data

	// Validate the refined plan
	if err := r.supervisor.ValidatePlan(ctx, &refinedPlan); err != nil {
		return nil, fmt.Errorf("refined plan failed validation: %w", err)
	}

	// Serialize the refined plan back to artifact
	refinedJSON, err := json.Marshal(refinedPlan)
	if err != nil {
		return nil, fmt.Errorf("failed to serialize refined plan: %w", err)
	}

	// Return refined artifact
	return &iterative.Artifact{
		Type:    artifact.Type,
		Content: string(refinedJSON),
		Context: artifact.Context,
	}, nil
}

// CheckConvergence determines if the plan has converged to a stable state.
// It uses AI to analyze the diff between current and previous versions.
func (r *PlanRefiner) CheckConvergence(ctx context.Context, current, previous *iterative.Artifact) (*iterative.ConvergenceDecision, error) {
	// Deserialize both plans
	var currentPlan, previousPlan types.MissionPlan
	if err := json.Unmarshal([]byte(current.Content), &currentPlan); err != nil {
		return nil, fmt.Errorf("failed to deserialize current plan: %w", err)
	}
	if err := json.Unmarshal([]byte(previous.Content), &previousPlan); err != nil {
		return nil, fmt.Errorf("failed to deserialize previous plan: %w", err)
	}

	// Build convergence prompt
	prompt := r.supervisor.buildConvergencePrompt(&previousPlan, &currentPlan)

	// Call AI with retry logic
	var response *anthropic.Message
	err := r.supervisor.retryWithBackoff(ctx, "convergence-check", func(attemptCtx context.Context) error {
		resp, apiErr := r.supervisor.client.Messages.New(attemptCtx, anthropic.MessageNewParams{
			Model:     anthropic.Model(r.supervisor.model),
			MaxTokens: 2048,
			Messages: []anthropic.MessageParam{
				anthropic.NewUserMessage(anthropic.NewTextBlock(prompt)),
			},
		})
		if apiErr != nil {
			return apiErr
		}
		response = resp
		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("anthropic API call failed: %w", err)
	}

	// Extract text from response
	var responseText string
	for _, block := range response.Content {
		if block.Type == "text" {
			responseText += block.Text
		}
	}

	// Parse the convergence decision
	type convergenceResponse struct {
		Converged     bool     `json:"converged"`
		Confidence    float64  `json:"confidence"`
		Reasoning     string   `json:"reasoning"`
		DiffPercent   float64  `json:"diff_percentage"`
		MajorChanges  []string `json:"major_changes"`
	}

	parseResult := Parse[convergenceResponse](responseText, ParseOptions{
		Context:   "convergence check response",
		LogErrors: boolPtr(true),
	})

	if !parseResult.Success {
		return nil, fmt.Errorf("failed to parse convergence response: %s (response: %s)",
			parseResult.Error, truncateString(responseText, 200))
	}

	result := parseResult.Data

	// Return convergence decision
	return &iterative.ConvergenceDecision{
		Converged:  result.Converged,
		Confidence: result.Confidence,
		Reasoning:  result.Reasoning,
		Strategy:   "ai-analysis", // Using AI to analyze diff
	}, nil
}

// buildRefinementPrompt builds the prompt for refining a plan
func (r *PlanRefiner) buildRefinementPrompt(currentPlan *types.MissionPlan, context string) string {
	// Serialize current plan for the prompt
	planJSON, _ := json.MarshalIndent(currentPlan, "", "  ")

	feedbackSection := ""
	if context != "" {
		feedbackSection = fmt.Sprintf("\n\nFEEDBACK FROM PREVIOUS ITERATION:\n%s\n", context)
	}

	return fmt.Sprintf(`You are refining a mission plan to improve its quality and completeness.

CURRENT PLAN (iteration %d):
%s%s

YOUR TASK:
Review the current plan and refine it to address any gaps or weaknesses. Consider:

1. **Completeness**: Are all necessary phases included? Any missing edge cases?
2. **Balance**: Are phases roughly equal in size/complexity?
3. **Dependencies**: Are phase dependencies correct and complete?
4. **Clarity**: Are titles and descriptions clear and specific?
5. **Estimates**: Are effort estimates realistic?
6. **Acceptance Criteria**: Do tasks have clear, testable WHEN...THEN... criteria?

REFINEMENT GOALS:
- Fill gaps in coverage (missing phases, edge cases)
- Balance phase sizes (split large phases, merge tiny ones)
- Clarify vague descriptions
- Add missing dependencies
- Improve estimates based on complexity
- Ensure all acceptance criteria use WHEN...THEN... format

Return the refined plan as a JSON object matching the same structure:
{
  "mission_id": "...",
  "phases": [...],
  "strategy": "...",
  "risks": [...],
  "estimated_effort": "...",
  "confidence": 0.0-1.0,
  "generated_at": "...",
  "generated_by": "ai-planner",
  "status": "refining"
}

IMPORTANT GUIDELINES:
- Preserve good parts of the current plan (don't change things arbitrarily)
- Make substantive improvements, not just cosmetic changes
- Ensure all acceptance criteria use WHEN...THEN... format
- Confidence should reflect uncertainty (0.0-1.0)
- Status should be "refining" during iteration

IMPORTANT: Respond with ONLY raw JSON. Do NOT wrap it in markdown code fences. Just the JSON object.`,
		r.currentIter, string(planJSON), feedbackSection)
}
