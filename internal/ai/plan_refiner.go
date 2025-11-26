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

// PlanningCostMetrics tracks cost and performance metrics for a planning cycle
type PlanningCostMetrics struct {
	Iterations      int
	TotalTokens     int
	InputTokens     int
	OutputTokens    int
	EstimatedCostUSD float64
	TotalDuration   int64 // milliseconds
}

// NewPlanRefiner creates a new plan refiner.
func NewPlanRefiner(supervisor *Supervisor, planningCtx *types.PlanningContext) *PlanRefiner {
	return &PlanRefiner{
		supervisor:  supervisor,
		planningCtx: planningCtx,
		currentIter: 0,
	}
}

// ComputePlanningCost calculates the cost metrics from an iterative convergence result.
// This extracts token counts and duration from the metrics collector and returns
// human-readable cost information.
//
// Note: Token tracking happens through the Supervisor's recordAIUsage() method,
// which logs all AI calls to the agent_events table. The MetricsCollector in the
// iterative framework currently doesn't capture token counts (the Refiner interface
// doesn't provide a way to pass them back). For detailed token metrics, query the
// agent_events table filtered by issue_id and event_type='ai_usage'.
func ComputePlanningCost(result *iterative.ConvergenceResult, metrics *iterative.ArtifactMetrics) *PlanningCostMetrics {
	if metrics == nil {
		return &PlanningCostMetrics{
			Iterations:    result.Iterations,
			TotalDuration: result.ElapsedTime.Milliseconds(),
		}
	}

	// If metrics are available and populated with token counts, calculate cost
	// Otherwise, callers should query agent_events for token metrics
	var estimatedCost float64
	if metrics.TotalInputTokens > 0 || metrics.TotalOutputTokens > 0 {
		// Claude Sonnet 4.5 pricing (as of Jan 2025): $3/MTok input, $15/MTok output
		inputCostPerMToken := 3.0
		outputCostPerMToken := 15.0
		estimatedCost = (float64(metrics.TotalInputTokens)/1_000_000)*inputCostPerMToken +
			(float64(metrics.TotalOutputTokens)/1_000_000)*outputCostPerMToken
	}

	return &PlanningCostMetrics{
		Iterations:       result.Iterations,
		TotalTokens:      metrics.TotalInputTokens + metrics.TotalOutputTokens,
		InputTokens:      metrics.TotalInputTokens,
		OutputTokens:     metrics.TotalOutputTokens,
		EstimatedCostUSD: estimatedCost,
		TotalDuration:    result.ElapsedTime.Milliseconds(),
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
			Model:       anthropic.Model(r.supervisor.model),
			MaxTokens:   4096,
			Temperature: anthropic.Float(0), // Deterministic output
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
			Model:       anthropic.Model(r.supervisor.model),
			MaxTokens:   1024,
			Temperature: anthropic.Float(0), // Deterministic output
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

// IncorporateFeedback takes human feedback and uses AI to update the plan accordingly.
// This allows humans to guide the refinement process with specific concerns or requests.
func (r *PlanRefiner) IncorporateFeedback(ctx context.Context, currentPlan *types.MissionPlan, feedback string) (*types.MissionPlan, error) {
	// Build feedback incorporation prompt
	prompt := r.buildFeedbackPrompt(currentPlan, feedback)

	// Call AI with retry logic
	var response *anthropic.Message
	err := r.supervisor.retryWithBackoff(ctx, "feedback-incorporation", func(attemptCtx context.Context) error {
		resp, apiErr := r.supervisor.client.Messages.New(attemptCtx, anthropic.MessageNewParams{
			Model:       anthropic.Model(r.supervisor.model),
			MaxTokens:   4096,
			Temperature: anthropic.Float(0), // Deterministic output
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

	// Parse the updated plan
	parseResult := Parse[types.MissionPlan](responseText, ParseOptions{
		Context:   "feedback incorporation response",
		LogErrors: boolPtr(true),
	})

	if !parseResult.Success {
		return nil, fmt.Errorf("failed to parse updated plan: %s (response: %s)",
			parseResult.Error, truncateString(responseText, 200))
	}

	updatedPlan := parseResult.Data

	// Validate the updated plan
	if err := r.supervisor.ValidatePlan(ctx, &updatedPlan); err != nil {
		return nil, fmt.Errorf("updated plan failed validation: %w", err)
	}

	return &updatedPlan, nil
}

// buildFeedbackPrompt builds the prompt for incorporating human feedback
func (r *PlanRefiner) buildFeedbackPrompt(currentPlan *types.MissionPlan, feedback string) string {
	// Serialize current plan compactly
	planJSON, _ := json.Marshal(currentPlan)

	return fmt.Sprintf(`Update this plan based on feedback. Be concise.

PLAN: %s

FEEDBACK: %s

Return ONLY a compact JSON object (no markdown, no explanation):
{"mission_id":"...","phases":[{"phase_number":1,"title":"...","description":"...","strategy":"...","tasks":["..."],"estimated_effort":"..."}],"strategy":"...","risks":["..."],"estimated_effort":"...","confidence":0.8,"generated_at":"2025-01-01T00:00:00Z","generated_by":"ai-planner","status":"refining"}`,
		string(planJSON), feedback)
}

// buildRefinementPrompt builds the prompt for refining a plan
func (r *PlanRefiner) buildRefinementPrompt(currentPlan *types.MissionPlan, context string) string {
	// Serialize current plan compactly
	planJSON, _ := json.Marshal(currentPlan)

	feedbackSection := ""
	if context != "" {
		feedbackSection = fmt.Sprintf(" FEEDBACK: %s", context)
	}

	return fmt.Sprintf(`Refine this plan (iteration %d). Fix gaps, improve clarity, balance phases. Be concise.

PLAN: %s%s

Return ONLY a compact JSON object (no markdown, no explanation):
{"mission_id":"...","phases":[{"phase_number":1,"title":"...","description":"...","strategy":"...","tasks":["..."],"estimated_effort":"..."}],"strategy":"...","risks":["..."],"estimated_effort":"...","confidence":0.8,"generated_at":"2025-01-01T00:00:00Z","generated_by":"ai-planner","status":"refining"}`,
		r.currentIter, string(planJSON), feedbackSection)
}
