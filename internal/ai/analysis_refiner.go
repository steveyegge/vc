package ai

import (
	"context"
	"fmt"
	"strings"

	"github.com/steveyegge/vc/internal/iterative"
	"github.com/steveyegge/vc/internal/types"
)

// AnalysisRefiner implements iterative.Refiner for the analysis phase.
// It performs multiple refinement passes on execution analysis to catch
// more discovered issues, punted items, and quality problems.
//
// The refiner uses the AI supervisor to iteratively improve the analysis,
// incorporating feedback from previous iterations to find missed work.
type AnalysisRefiner struct {
	supervisor *Supervisor
	issue      *types.Issue
	agentOutput string
	success    bool

	// minConfidence is the minimum confidence for convergence (0.0-1.0)
	minConfidence float64
}

// NewAnalysisRefiner creates a refiner for the analysis phase
func NewAnalysisRefiner(supervisor *Supervisor, issue *types.Issue, agentOutput string, success bool) (*AnalysisRefiner, error) {
	if supervisor == nil {
		return nil, fmt.Errorf("supervisor cannot be nil")
	}
	if issue == nil {
		return nil, fmt.Errorf("issue cannot be nil")
	}

	return &AnalysisRefiner{
		supervisor:    supervisor,
		issue:         issue,
		agentOutput:   agentOutput,
		success:       success,
		minConfidence: 0.85, // High confidence threshold - analysis is critical
	}, nil
}

// Refine performs one refinement pass on the analysis artifact.
// It takes the current analysis and asks the AI to review it with fresh perspective,
// looking for missed discovered issues, punted items, and quality problems.
func (r *AnalysisRefiner) Refine(ctx context.Context, artifact *iterative.Artifact) (*iterative.Artifact, error) {
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

	// Parse the refined analysis
	parseResult := Parse[Analysis](responseText, ParseOptions{
		Context:   "analysis refinement",
		LogErrors: boolPtr(true),
	})
	if !parseResult.Success {
		return nil, fmt.Errorf("failed to parse refined analysis: %s", parseResult.Error)
	}

	refinedAnalysis := parseResult.Data

	// Convert back to artifact
	// We serialize the analysis struct as JSON-like text for diff comparison
	refinedContent := serializeAnalysis(&refinedAnalysis)

	// Build context for next iteration (include what we found this time)
	nextContext := r.buildIterationContext(artifact, &refinedAnalysis)

	return &iterative.Artifact{
		Type:    artifact.Type,
		Content: refinedContent,
		Context: nextContext,
	}, nil
}

// CheckConvergence uses AI to determine if the analysis has converged
func (r *AnalysisRefiner) CheckConvergence(ctx context.Context, current, previous *iterative.Artifact) (*iterative.ConvergenceDecision, error) {
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
		Context:   "analysis convergence check",
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
func (r *AnalysisRefiner) buildConvergencePrompt(current, previous *iterative.Artifact) string {
	return fmt.Sprintf(`You are judging whether an analysis artifact has converged through iterative refinement.

ARTIFACT TYPE: %s

PREVIOUS VERSION:
%s

CURRENT VERSION:
%s

CONTEXT:
%s

YOUR TASK:
Determine if this analysis has converged (reached stability) or if another refinement iteration would find more discovered issues, punted items, or quality problems.

Consider:
1. **Diff size**: Are changes minimal, or substantive new issues found?
2. **Completeness**: Have we thoroughly analyzed the agent output?
3. **Gaps**: Are there obvious things we're missing?
4. **Marginal value**: Would another iteration find meaningful new issues?

GUIDELINES:
- Minimal diff + no new issues found = likely converged
- New discovered issues added = NOT converged (we're still finding work)
- Same issues rewording = likely converged
- If we found 5+ new issues this iteration = definitely NOT converged

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

// Helper functions
func truncateForPrompt(text string, maxChars int) string {
	if len(text) <= maxChars {
		return text
	}
	return text[:maxChars] + "... (truncated)"
}

// buildRefinementPrompt constructs the prompt for a refinement iteration
func (r *AnalysisRefiner) buildRefinementPrompt(artifact *iterative.Artifact) string {
	successStr := "succeeded"
	if !r.success {
		successStr = "failed"
	}

	return fmt.Sprintf(`You are performing ITERATIVE REFINEMENT on an AI analysis of execution results.

TASK: Review the current analysis with FRESH PERSPECTIVE and find what was MISSED.

Issue ID: %s
Title: %s
Description: %s
Acceptance Criteria: %s

Agent Execution Status: %s

Agent Output (smart truncation to 8000 chars):
%s

CURRENT ANALYSIS (from previous iteration):
%s

ITERATION CONTEXT:
%s

YOUR MISSION: Find what the previous iteration MISSED.

Focus areas:
1. **Discovered Issues** - Were there bugs, tasks, or improvements mentioned in the output that weren't captured?
   - Look for "TODO", "FIXME", "NOTE", error messages, warnings, deferred work
   - Check if follow-on work was implied but not explicitly stated
   - For each issue, classify as "blocker" (blocks parent mission), "related" (mission-related but not blocking), or "background" (opportunistic)

2. **Punted Items** - Was work deferred, skipped, or left incomplete?
   - Look for "skipping", "deferring", "will do later", incomplete implementation
   - Check if acceptance criteria mention work that wasn't done

3. **Quality Issues** - Were there problems the previous analysis overlooked?
   - Test failures, lint errors, missing error handling
   - Missing tests, missing documentation
   - Technical debt, code smells

4. **Scope Validation** - Did the agent actually work on THIS issue's task?
   - Compare what was done vs. what the Description and Acceptance Criteria asked for
   - If the agent did unrelated work, ensure "completed" is false

5. **Acceptance Criteria** - Were ALL criteria truly met?
   - Quote each criterion and verify it was addressed
   - Provide evidence from agent output if met, or reason if not met

CRITICAL: For "meta-issues" (issues about needing something for another issue):
- Add "meta-issue" to labels array
- MUST provide acceptance_criteria (specific, measurable criteria)
- Example: Issue "Add acceptance criteria to vc-xyz" should have:
  labels: ["meta-issue"]
  acceptance_criteria: "1. Add specific acceptance criteria to vc-xyz\n2. Ensure criteria are measurable\n3. Verify criteria match issue description"

Provide your REFINED analysis as JSON:
{
  "completed": true,
  "scope_validation": {
    "on_task": true,
    "explanation": "..."
  },
  "acceptance_criteria_met": {
    "criterion_1": {"met": true, "evidence": "..."},
    "criterion_2": {"met": false, "reason": "..."}
  },
  "punted_items": ["..."],
  "discovered_issues": [
    {
      "title": "...",
      "description": "...",
      "type": "bug|task|enhancement",
      "priority": "P0|P1|P2|P3",
      "discovery_type": "blocker|related|background",
      "acceptance_criteria": "Required for meta-issues",
      "labels": ["meta-issue"] // Optional
    }
  ],
  "quality_issues": ["..."],
  "summary": "...",
  "confidence": 0.9
}

IMPORTANT: Respond with ONLY raw JSON. Do NOT wrap in markdown code fences.

Remember: Your job is to find what was MISSED. Be thorough and critical.`,
		r.issue.ID, r.issue.Title, r.issue.Description, r.issue.AcceptanceCriteria,
		successStr,
		truncateString(r.agentOutput, 8000),
		artifact.Content,
		artifact.Context,
	)
}

// buildIterationContext builds context for the next iteration
func (r *AnalysisRefiner) buildIterationContext(artifact *iterative.Artifact, refinedAnalysis *Analysis) string {
	var context strings.Builder

	// Add previous context
	if artifact.Context != "" {
		context.WriteString(artifact.Context)
		context.WriteString("\n\n")
	}

	// Add summary of what this iteration found
	context.WriteString("Previous iteration found:\n")
	context.WriteString(fmt.Sprintf("- Completed: %v\n", refinedAnalysis.Completed))
	context.WriteString(fmt.Sprintf("- Discovered issues: %d\n", len(refinedAnalysis.DiscoveredIssues)))
	context.WriteString(fmt.Sprintf("- Punted items: %d\n", len(refinedAnalysis.PuntedItems)))
	context.WriteString(fmt.Sprintf("- Quality issues: %d\n", len(refinedAnalysis.QualityIssues)))

	if len(refinedAnalysis.DiscoveredIssues) > 0 {
		context.WriteString("\nDiscovered issues found:\n")
		for i, issue := range refinedAnalysis.DiscoveredIssues {
			context.WriteString(fmt.Sprintf("%d. %s (type=%s, priority=%s, discovery=%s)\n",
				i+1, issue.Title, issue.Type, issue.Priority, issue.DiscoveryType))
		}
	}

	return context.String()
}

// serializeAnalysis converts an Analysis struct to a readable text format for diffing
func serializeAnalysis(analysis *Analysis) string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("Completed: %v\n", analysis.Completed))
	sb.WriteString(fmt.Sprintf("Confidence: %.2f\n\n", analysis.Confidence))

	if analysis.ScopeValidation != nil {
		sb.WriteString("Scope Validation:\n")
		sb.WriteString(fmt.Sprintf("  On Task: %v\n", analysis.ScopeValidation.OnTask))
		sb.WriteString(fmt.Sprintf("  Explanation: %s\n\n", analysis.ScopeValidation.Explanation))
	}

	if len(analysis.AcceptanceCriteriaMet) > 0 {
		sb.WriteString("Acceptance Criteria:\n")
		for criterion, result := range analysis.AcceptanceCriteriaMet {
			sb.WriteString(fmt.Sprintf("  %s: met=%v", criterion, result.Met))
			if result.Evidence != "" {
				sb.WriteString(fmt.Sprintf(" (evidence: %s)", truncateString(result.Evidence, 100)))
			}
			if result.Reason != "" {
				sb.WriteString(fmt.Sprintf(" (reason: %s)", truncateString(result.Reason, 100)))
			}
			sb.WriteString("\n")
		}
		sb.WriteString("\n")
	}

	if len(analysis.PuntedItems) > 0 {
		sb.WriteString(fmt.Sprintf("Punted Items (%d):\n", len(analysis.PuntedItems)))
		for i, item := range analysis.PuntedItems {
			sb.WriteString(fmt.Sprintf("  %d. %s\n", i+1, item))
		}
		sb.WriteString("\n")
	}

	if len(analysis.DiscoveredIssues) > 0 {
		sb.WriteString(fmt.Sprintf("Discovered Issues (%d):\n", len(analysis.DiscoveredIssues)))
		for i, issue := range analysis.DiscoveredIssues {
			sb.WriteString(fmt.Sprintf("  %d. %s\n", i+1, issue.Title))
			sb.WriteString(fmt.Sprintf("     Type: %s, Priority: %s, Discovery: %s\n",
				issue.Type, issue.Priority, issue.DiscoveryType))
			sb.WriteString(fmt.Sprintf("     Description: %s\n", truncateString(issue.Description, 200)))
			if issue.AcceptanceCriteria != "" {
				sb.WriteString(fmt.Sprintf("     Acceptance Criteria: %s\n", truncateString(issue.AcceptanceCriteria, 150)))
			}
			if len(issue.Labels) > 0 {
				sb.WriteString(fmt.Sprintf("     Labels: %v\n", issue.Labels))
			}
		}
		sb.WriteString("\n")
	}

	if len(analysis.QualityIssues) > 0 {
		sb.WriteString(fmt.Sprintf("Quality Issues (%d):\n", len(analysis.QualityIssues)))
		for i, issue := range analysis.QualityIssues {
			sb.WriteString(fmt.Sprintf("  %d. %s\n", i+1, issue))
		}
		sb.WriteString("\n")
	}

	sb.WriteString(fmt.Sprintf("Summary: %s\n", analysis.Summary))

	return sb.String()
}

// deserializeAnalysis extracts an Analysis struct from the artifact content.
// This parses the text format created by serializeAnalysis() back into a structured Analysis.
//
// NOTE: This is NOT currently used in the production code path. The AnalyzeExecutionResultWithRefinement
// function uses a different approach: it re-calls the AI with the final artifact to get a fresh
// structured JSON response. This function exists for completeness and testing, but the AI re-parse
// approach is actually preferred because:
// 1. It ensures we get a properly structured Analysis (not lossy text parsing)
// 2. It's more robust against serialization format changes
// 3. The AI can correct any issues in the serialized form
//
// If you're considering using this function, think carefully about whether re-parsing via AI
// would be more appropriate for your use case.
//
//nolint:unparam // Function intentionally always returns nil to document unsupported operation
func deserializeAnalysis(artifact *iterative.Artifact) (*Analysis, error) {
	if artifact == nil {
		return nil, fmt.Errorf("artifact cannot be nil")
	}
	if artifact.Type != "analysis" {
		return nil, fmt.Errorf("expected artifact type 'analysis', got '%s'", artifact.Type)
	}

	// The serialized format is a lossy text representation optimized for human readability
	// and diff comparison. We cannot reliably reconstruct the full Analysis struct from it
	// without AI assistance.
	//
	// The serialized format contains:
	// - Truncated strings (Evidence, Reason, Description truncated to 100-200 chars)
	// - Lost map keys in AcceptanceCriteriaMet (we can't extract criterion names reliably)
	// - No way to distinguish between missing fields and empty fields
	//
	// For these reasons, this function intentionally returns an error directing users
	// to use the AI-based re-parsing approach instead.
	return nil, fmt.Errorf("deserialization from text format is not supported - use AI re-parsing via AnalyzeExecutionResultWithRefinement instead (see function comments for details)")
}
