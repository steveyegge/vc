package ai

import (
	"context"
	"fmt"
	"strings"

	"github.com/steveyegge/vc/internal/iterative"
	"github.com/steveyegge/vc/internal/types"
)

// AssessmentRefiner implements iterative.Refiner for the assessment phase.
// It performs multiple refinement passes on task assessment to improve
// strategy, identify more risks, and develop better execution plans.
//
// The refiner uses the AI supervisor to iteratively improve the assessment,
// incorporating feedback from previous iterations to find better approaches.
type AssessmentRefiner struct {
	supervisor *Supervisor
	issue      *types.Issue

	// minConfidence is the minimum confidence for convergence (0.0-1.0)
	minConfidence float64
}

// NewAssessmentRefiner creates a refiner for the assessment phase
func NewAssessmentRefiner(supervisor *Supervisor, issue *types.Issue) (*AssessmentRefiner, error) {
	if supervisor == nil {
		return nil, fmt.Errorf("supervisor cannot be nil")
	}
	if issue == nil {
		return nil, fmt.Errorf("issue cannot be nil")
	}

	return &AssessmentRefiner{
		supervisor:    supervisor,
		issue:         issue,
		minConfidence: 0.80, // Slightly lower than analysis (0.85) - assessment is more exploratory
	}, nil
}

// Refine performs one refinement pass on the assessment artifact.
// It takes the current assessment and asks the AI to review it with fresh perspective,
// looking for better strategies, more risks, and improved execution plans.
func (r *AssessmentRefiner) Refine(ctx context.Context, artifact *iterative.Artifact) (*iterative.Artifact, error) {
	if artifact == nil {
		return nil, fmt.Errorf("artifact cannot be nil")
	}

	// Build refinement prompt
	prompt := r.buildRefinementPrompt(artifact)

	// Call AI API for refinement
	response, err := r.supervisor.CallAPI(ctx, prompt, ModelSonnet, 4096)
	if err != nil {
		return nil, fmt.Errorf("AI refinement call failed: %w", err)
	}

	// Extract response text
	var responseText string
	for _, block := range response.Content {
		if block.Type == "text" {
			responseText += block.Text
		}
	}

	// Parse the refined assessment
	parseResult := Parse[Assessment](responseText, ParseOptions{
		Context:   "assessment refinement",
		LogErrors: boolPtr(true),
	})
	if !parseResult.Success {
		return nil, fmt.Errorf("failed to parse refined assessment: %s", parseResult.Error)
	}

	refinedAssessment := parseResult.Data

	// Convert back to artifact
	refinedContent := serializeAssessment(&refinedAssessment)

	// Build context for next iteration
	nextContext := r.buildIterationContext(artifact, &refinedAssessment)

	return &iterative.Artifact{
		Type:    artifact.Type,
		Content: refinedContent,
		Context: nextContext,
	}, nil
}

// CheckConvergence uses AI to determine if the assessment has converged
func (r *AssessmentRefiner) CheckConvergence(ctx context.Context, current, previous *iterative.Artifact) (*iterative.ConvergenceDecision, error) {
	// Build convergence judgment prompt
	prompt := r.buildConvergencePrompt(current, previous)

	// Call AI API
	response, err := r.supervisor.CallAPI(ctx, prompt, ModelSonnet, 2048)
	if err != nil {
		return nil, fmt.Errorf("AI convergence check failed: %w", err)
	}

	// Extract response text
	var responseText string
	for _, block := range response.Content {
		if block.Type == "text" {
			responseText += block.Text
		}
	}

	// Parse convergence response
	type convergenceResponse struct {
		Converged  bool    `json:"converged"`
		Confidence float64 `json:"confidence"`
		Reasoning  string  `json:"reasoning"`
		DiffSize   string  `json:"diff_size,omitempty"`
		Marginal   string  `json:"marginal,omitempty"`
	}

	parseResult := Parse[convergenceResponse](responseText, ParseOptions{
		Context:   "assessment convergence check",
		LogErrors: boolPtr(true),
	})
	if !parseResult.Success {
		return nil, fmt.Errorf("failed to parse convergence response: %s", parseResult.Error)
	}

	aiResponse := parseResult.Data

	return &iterative.ConvergenceDecision{
		Converged:  aiResponse.Converged,
		Confidence: aiResponse.Confidence,
		Reasoning:  aiResponse.Reasoning,
		Strategy:   "AI",
	}, nil
}

// buildConvergencePrompt constructs the prompt for convergence judgment
func (r *AssessmentRefiner) buildConvergencePrompt(current, previous *iterative.Artifact) string {
	return fmt.Sprintf(`You are judging whether an assessment artifact has converged through iterative refinement.

ARTIFACT TYPE: %s

PREVIOUS VERSION:
%s

CURRENT VERSION:
%s

CONTEXT:
%s

YOUR TASK:
Determine if this assessment has converged (reached stability) or if another refinement iteration would find better strategies, more risks, or improved execution plans.

Consider:
1. **Diff size**: Are changes minimal, or substantive improvements found?
2. **Completeness**: Have we thoroughly assessed the task?
3. **Gaps**: Are there obvious risks or edge cases we're missing?
4. **Marginal value**: Would another iteration find meaningful improvements?

GUIDELINES:
- Minimal diff + no new risks/steps = likely converged
- New risks or significantly better strategy = NOT converged (still improving)
- Same content rewording = likely converged
- If we found 3+ new risks or major strategy change = definitely NOT converged

Respond with JSON:
{
  "converged": true/false,
  "confidence": 0.0-1.0,
  "reasoning": "Brief explanation",
  "diff_size": "minimal|small|moderate|large",
  "marginal": "none|low|medium|high"
}

IMPORTANT: Respond with ONLY raw JSON. Do NOT wrap in markdown code fences.`,
		current.Type,
		truncateForPrompt(previous.Content, 3000),
		truncateForPrompt(current.Content, 3000),
		truncateForPrompt(current.Context, 1000),
	)
}

// buildRefinementPrompt constructs the prompt for a refinement iteration
func (r *AssessmentRefiner) buildRefinementPrompt(artifact *iterative.Artifact) string {
	return fmt.Sprintf(`You are performing ITERATIVE REFINEMENT on an AI assessment of a task.

TASK: Review the current assessment with FRESH PERSPECTIVE and find what can be IMPROVED.

Issue ID: %s
Title: %s
Description: %s
Acceptance Criteria: %s
Priority: P%d
Type: %s

CURRENT ASSESSMENT (from previous iteration):
%s

ITERATION CONTEXT:
%s

YOUR MISSION: Improve the assessment by finding better strategies, more risks, and clearer steps.

Focus areas:
1. **Strategy** - Is there a better approach?
   - Look for simpler solutions, fewer moving parts
   - Consider alternative approaches the previous iteration missed
   - Check if the strategy addresses all acceptance criteria
   - For complex issues, consider decomposition

2. **Steps** - Are the execution steps clear and complete?
   - Look for missing steps, unclear instructions
   - Check if steps are in the right order
   - Verify steps map to acceptance criteria
   - Add concrete file paths, function names where helpful

3. **Risks** - What could go wrong?
   - Look for edge cases, race conditions, error paths
   - Consider integration points, dependencies
   - Check for performance issues, scalability concerns
   - Identify areas needing extra testing or validation
   - For critical/complex issues: be extra thorough on risks

4. **Confidence** - How confident are you?
   - Lower if many unknowns, high complexity, novel area
   - Higher if clear precedent, simple change, well-understood

5. **Decomposition** - Should this be split up?
   - Large issues (>8 steps) might benefit from decomposition
   - Complex issues with multiple independent parts
   - Issues blocking many others might need faster iteration

CRITICAL: For complex/high-risk issues (P0, >5 dependencies, critical path):
- Be EXTRA thorough in identifying risks
- Consider edge cases, concurrency issues, error handling
- Think about testing strategy and validation
- Add specific validation steps to the plan

Provide your REFINED assessment as JSON:
{
  "strategy": "High-level approach (improved or confirmed)",
  "steps": ["Step 1...", "Step 2...", ...],
  "risks": ["Risk 1...", "Risk 2...", ...],
  "confidence": 0.0-1.0,
  "reasoning": "Why this strategy/these steps/these risks",
  "should_decompose": true/false,
  "decomposition_plan": {
    "reasoning": "Why decompose",
    "child_issues": [
      {
        "title": "...",
        "description": "...",
        "acceptance_criteria": "...",
        "priority": 0-3,
        "estimated_minutes": 30
      }
    ]
  }
}

IMPORTANT: Respond with ONLY raw JSON. Do NOT wrap in markdown code fences.

Remember: Your job is to IMPROVE the assessment. Be critical and find better approaches.`,
		r.issue.ID, r.issue.Title, r.issue.Description, r.issue.AcceptanceCriteria,
		r.issue.Priority, r.issue.IssueType,
		artifact.Content,
		artifact.Context,
	)
}

// buildIterationContext builds context for the next iteration
func (r *AssessmentRefiner) buildIterationContext(artifact *iterative.Artifact, refinedAssessment *Assessment) string {
	var context strings.Builder

	// Add previous context
	if artifact.Context != "" {
		context.WriteString(artifact.Context)
		context.WriteString("\n\n")
	}

	// Add summary of what this iteration found
	context.WriteString("Previous iteration:\n")
	context.WriteString(fmt.Sprintf("- Strategy: %s\n", truncateString(refinedAssessment.Strategy, 100)))
	context.WriteString(fmt.Sprintf("- Steps: %d\n", len(refinedAssessment.Steps)))
	context.WriteString(fmt.Sprintf("- Risks: %d\n", len(refinedAssessment.Risks)))
	context.WriteString(fmt.Sprintf("- Confidence: %.2f\n", refinedAssessment.Confidence))
	context.WriteString(fmt.Sprintf("- Should decompose: %v\n", refinedAssessment.ShouldDecompose))

	if len(refinedAssessment.Risks) > 0 {
		context.WriteString("\nRisks identified:\n")
		for i, risk := range refinedAssessment.Risks {
			context.WriteString(fmt.Sprintf("%d. %s\n", i+1, truncateString(risk, 80)))
		}
	}

	return context.String()
}

// serializeAssessment converts an Assessment struct to a readable text format for diffing
func serializeAssessment(assessment *Assessment) string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("Strategy: %s\n\n", assessment.Strategy))
	sb.WriteString(fmt.Sprintf("Confidence: %.2f\n\n", assessment.Confidence))

	if len(assessment.Steps) > 0 {
		sb.WriteString(fmt.Sprintf("Steps (%d):\n", len(assessment.Steps)))
		for i, step := range assessment.Steps {
			sb.WriteString(fmt.Sprintf("  %d. %s\n", i+1, step))
		}
		sb.WriteString("\n")
	}

	if len(assessment.Risks) > 0 {
		sb.WriteString(fmt.Sprintf("Risks (%d):\n", len(assessment.Risks)))
		for i, risk := range assessment.Risks {
			sb.WriteString(fmt.Sprintf("  %d. %s\n", i+1, risk))
		}
		sb.WriteString("\n")
	}

	sb.WriteString(fmt.Sprintf("Reasoning: %s\n\n", assessment.Reasoning))

	sb.WriteString(fmt.Sprintf("Should Decompose: %v\n", assessment.ShouldDecompose))
	if assessment.ShouldDecompose && assessment.DecompositionPlan != nil {
		sb.WriteString(fmt.Sprintf("Decomposition Reasoning: %s\n", assessment.DecompositionPlan.Reasoning))
		sb.WriteString(fmt.Sprintf("Child Issues: %d\n", len(assessment.DecompositionPlan.ChildIssues)))
		for i, child := range assessment.DecompositionPlan.ChildIssues {
			sb.WriteString(fmt.Sprintf("  %d. %s (P%d, %dm)\n", i+1, child.Title, child.Priority, child.EstimatedMinutes))
		}
	}

	return sb.String()
}
