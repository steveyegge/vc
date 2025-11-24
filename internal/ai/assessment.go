package ai

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/steveyegge/vc/internal/iterative"
	"github.com/steveyegge/vc/internal/types"
)

// Assessment represents an AI assessment of an issue before execution
type Assessment struct {
	Strategy         string            `json:"strategy"`           // High-level strategy for completing the issue
	Steps            []string          `json:"steps"`              // Specific steps to take
	Risks            []string          `json:"risks"`              // Potential risks or challenges
	Confidence       float64           `json:"confidence"`         // Confidence score (0.0-1.0)
	Reasoning        string            `json:"reasoning"`          // Detailed reasoning
	ShouldDecompose  bool              `json:"should_decompose"`   // Whether this issue should be split into child issues (vc-rzqe)
	DecompositionPlan *DecompositionPlan `json:"decomposition_plan,omitempty"` // Plan for decomposing into child issues (vc-rzqe)
}

// DecompositionPlan describes how to break an issue into child issues (vc-rzqe)
type DecompositionPlan struct {
	Reasoning  string      `json:"reasoning"`   // Why decomposition is recommended
	ChildIssues []ChildIssue `json:"child_issues"` // Proposed child issues
}

// ChildIssue represents a proposed child issue in a decomposition plan (vc-rzqe)
type ChildIssue struct {
	Title              string `json:"title"`               // Title for the child issue
	Description        string `json:"description"`         // Description for the child issue
	AcceptanceCriteria string `json:"acceptance_criteria"` // Acceptance criteria for the child issue
	Priority           int    `json:"priority"`            // Priority (0-4)
	EstimatedMinutes   int    `json:"estimated_minutes"`   // Time estimate
}

// CompletionAssessment represents AI assessment of whether an epic/mission is complete
type CompletionAssessment struct {
	ShouldClose bool     `json:"should_close"` // Should this epic/mission be closed?
	Reasoning   string   `json:"reasoning"`    // Detailed reasoning for the decision
	Confidence  float64  `json:"confidence"`   // Confidence in the assessment (0.0-1.0)
	Caveats     []string `json:"caveats"`      // Any caveats or concerns
}

// AssessIssueState performs AI assessment before executing an issue
func (s *Supervisor) AssessIssueState(ctx context.Context, issue *types.Issue) (*Assessment, error) {
	startTime := time.Now()

	// Build the prompt for assessment
	prompt := s.buildAssessmentPrompt(issue)

	// Call Anthropic API with retry logic
	var response *anthropic.Message
	err := s.retryWithBackoff(ctx, "assessment", func(attemptCtx context.Context) error {
		resp, apiErr := s.client.Messages.New(attemptCtx, anthropic.MessageNewParams{
			Model:     anthropic.Model(s.model),
			MaxTokens: 4096,
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

	// Extract the text content from the response
	var responseText string
	for _, block := range response.Content {
		if block.Type == "text" {
			responseText += block.Text
		}
	}

	// Parse the response as JSON using resilient parser
	parseResult := Parse[Assessment](responseText, ParseOptions{
		Context:   "assessment response",
		LogErrors: boolPtr(true),
	})
	if !parseResult.Success {
		// vc-227: Truncate AI response to prevent log spam
		return nil, fmt.Errorf("failed to parse assessment response: %s (response: %s)", parseResult.Error, truncateString(responseText, 200))
	}
	assessment := parseResult.Data

	// Log the assessment
	duration := time.Since(startTime)
	fmt.Printf("AI Assessment for %s: confidence=%.2f, duration=%v\n",
		issue.ID, assessment.Confidence, duration)

	// Log AI usage to events
	if err := s.recordAIUsage(ctx, issue.ID, "assessment", response.Usage.InputTokens, response.Usage.OutputTokens, duration); err != nil {
		fmt.Fprintf(os.Stderr, "warning: failed to log AI usage: %v\n", err)
	}

	return &assessment, nil
}

// AssessIssueStateWithRefinement performs AI assessment with iterative refinement
// for complex/high-risk issues. Simple issues skip iteration to avoid unnecessary cost.
//
// Heuristic for when to iterate (vc-43kd):
// - P0 issues (critical)
// - Critical path issues (many dependents)
// - Novel areas (no similar closed issues)
// - High dependency count (>5 dependencies)
//
// Configuration:
// - MinIterations: 3 (ensures thorough risk identification)
// - MaxIterations: 6 (prevents runaway iteration)
// - AI-driven convergence: stops when AI determines assessment has stabilized
//
// Returns the refined assessment and metrics about the refinement process.
func (s *Supervisor) AssessIssueStateWithRefinement(ctx context.Context, issue *types.Issue, collector iterative.MetricsCollector) (*Assessment, *iterative.ConvergenceResult, error) {
	startTime := time.Now()

	// Check if this issue should use iterative refinement (vc-43kd)
	shouldIterate, triggers, skipReason := s.shouldIterateAssessment(ctx, issue)
	if !shouldIterate {
		fmt.Printf("Skipping assessment iteration for %s: %s\n", issue.ID, skipReason)
		// Fall back to single-pass assessment
		assessment, err := s.AssessIssueState(ctx, issue)
		if err != nil {
			return nil, nil, err
		}

		// Record metrics for skipped iteration (vc-642z)
		if collector != nil {
			metrics := &iterative.ArtifactMetrics{
				ArtifactType:     "assessment",
				Priority:         fmt.Sprintf("P%d", issue.Priority),
				TotalIterations:  0,
				Converged:        true,
				ConvergenceReason: "selectivity skip",
				TotalDuration:    time.Since(startTime),
				IterationSkipped: true,
				SkipReason:       skipReason,
			}
			collector.RecordArtifactComplete(&iterative.ConvergenceResult{
				Iterations:  0,
				Converged:   true,
				ElapsedTime: time.Since(startTime),
			}, metrics)
		}

		// Return mock convergence result for consistency
		return assessment, &iterative.ConvergenceResult{
			FinalArtifact: &iterative.Artifact{
				Type:    "assessment",
				Content: serializeAssessment(assessment),
				Context: fmt.Sprintf("Single-pass assessment for issue %s: %s", issue.ID, issue.Title),
			},
			Iterations:  0,
			Converged:   true,
			ElapsedTime: time.Since(startTime),
		}, nil
	}

	fmt.Printf("Using iterative refinement for assessment of %s: %v\n", issue.ID, triggers)

	// Perform initial assessment (iteration 0)
	initialAssessment, err := s.AssessIssueState(ctx, issue)
	if err != nil {
		return nil, nil, fmt.Errorf("initial assessment failed: %w", err)
	}

	// Create refiner
	refiner, err := NewAssessmentRefiner(s, issue)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create refiner: %w", err)
	}

	// Create initial artifact from the assessment
	initialArtifact := &iterative.Artifact{
		Type:    "assessment",
		Content: serializeAssessment(initialAssessment),
		Context: fmt.Sprintf("Initial assessment for issue %s: %s", issue.ID, issue.Title),
	}

	// Configure refinement
	config := iterative.RefinementConfig{
		MinIterations: 3, // Ensure thorough risk identification
		MaxIterations: 6, // Prevent runaway iteration (shorter than analysis phase)
		SkipSimple:    false,
		Timeout:       0, // No timeout - rely on MaxIterations
	}

	// Run iterative refinement
	result, err := iterative.Converge(ctx, initialArtifact, refiner, config, collector)
	if err != nil {
		return nil, nil, fmt.Errorf("iterative refinement failed: %w", err)
	}

	// Update metrics with selectivity triggers (vc-642z)
	if collector != nil {
		// The collector has already recorded artifact metrics via Converge
		// We need to update the most recent artifact with selectivity triggers
		if memCollector, ok := collector.(*iterative.InMemoryMetricsCollector); ok {
			artifacts := memCollector.GetArtifacts()
			if len(artifacts) > 0 {
				lastMetrics := artifacts[len(artifacts)-1]
				// Only update if this is the assessment artifact we just processed
				if lastMetrics.ArtifactType == "assessment" {
					lastMetrics.SelectivityTriggers = triggers
					lastMetrics.Priority = fmt.Sprintf("P%d", issue.Priority)
				}
			}
		}
	}

	// Parse the final refined assessment from the artifact content
	// We need to re-run the AI to get the structured Assessment object
	// since serializeAssessment creates a text representation
	finalPrompt := fmt.Sprintf(`Convert this assessment text back to structured JSON:

%s

Respond with a JSON object matching this structure:
{
  "strategy": "...",
  "steps": ["...", "..."],
  "risks": ["...", "..."],
  "confidence": 0.0-1.0,
  "reasoning": "...",
  "should_decompose": true/false,
  "decomposition_plan": null or {...}
}

IMPORTANT: Respond with ONLY raw JSON. Do NOT wrap in markdown code fences.`,
		result.FinalArtifact.Content)

	var response *anthropic.Message
	err = s.retryWithBackoff(ctx, "assessment-final-parse", func(attemptCtx context.Context) error {
		resp, apiErr := s.client.Messages.New(attemptCtx, anthropic.MessageNewParams{
			Model:     anthropic.Model(s.model),
			MaxTokens: 4096,
			Messages: []anthropic.MessageParam{
				anthropic.NewUserMessage(anthropic.NewTextBlock(finalPrompt)),
			},
		})
		if apiErr != nil {
			return apiErr
		}
		response = resp
		return nil
	})

	if err != nil {
		return nil, nil, fmt.Errorf("failed to parse final assessment: %w", err)
	}

	// Extract response text
	var responseText string
	for _, block := range response.Content {
		if block.Type == "text" {
			responseText += block.Text
		}
	}

	// Parse as Assessment
	parseResult := Parse[Assessment](responseText, ParseOptions{
		Context:   "final assessment parse",
		LogErrors: boolPtr(true),
	})
	if !parseResult.Success {
		return nil, nil, fmt.Errorf("failed to parse final assessment: %s", parseResult.Error)
	}

	finalAssessment := parseResult.Data

	// Log refinement summary
	fmt.Printf("Assessment refinement complete for %s: iterations=%d, converged=%v, duration=%v\n",
		issue.ID, result.Iterations, result.Converged, result.ElapsedTime)

	return &finalAssessment, result, nil
}

// shouldIterateAssessment determines if an issue warrants iterative assessment refinement.
// Returns (shouldIterate, triggers, skipReason).
//
// If shouldIterate is true:
//   - triggers contains the list of heuristics that matched
//   - skipReason is empty
//
// If shouldIterate is false:
//   - triggers is empty
//   - skipReason explains why iteration was skipped
//
// Heuristic for when to iterate (vc-43kd):
// - P0 issues (critical)
// - Complex issue types (epic, mission, phase)
// - Novel areas (no similar closed issues)
//
// Simple issues skip iteration to avoid unnecessary cost.
func (s *Supervisor) shouldIterateAssessment(ctx context.Context, issue *types.Issue) (bool, []string, string) {
	var triggers []string

	// P0 issues are critical - always iterate for thorough risk identification
	if issue.Priority == 0 {
		triggers = append(triggers, "P0 priority")
	}

	// Complex structural issues (epics, missions, phases) need careful planning
	if issue.IssueSubtype == types.SubtypeMission || issue.IssueSubtype == types.SubtypePhase {
		triggers = append(triggers, fmt.Sprintf("%s (complex structural issue)", issue.IssueSubtype))
	}

	// Check if this is a novel area (no similar closed issues)
	// We need storage access for this check
	if s.store != nil {
		isNovel, err := s.isNovelArea(ctx, issue)
		if err != nil {
			// Log error but don't fail - just skip this heuristic
			fmt.Fprintf(os.Stderr, "warning: failed to check novelty for %s: %v\n", issue.ID, err)
		} else if isNovel {
			triggers = append(triggers, "novel area (no similar closed issues)")
		}
	}

	// If we found any triggers, iterate
	if len(triggers) > 0 {
		return true, triggers, ""
	}

	// Otherwise skip iteration for simple issues
	return false, nil, "simple issue (no complexity triggers)"
}

// isNovelArea checks if this issue is in a novel area with no precedent.
// Returns true if we can't find similar closed issues, suggesting this is new territory.
//
//nolint:unparam // ctx will be used in full implementation for storage queries
func (s *Supervisor) isNovelArea(_ context.Context, issue *types.Issue) (bool, error) {
	// For now, use a simple heuristic: check if the title contains uncommon technical terms
	// A full implementation would search closed issues, but that requires storage interface extensions

	// Extract key terms from the title (ignore common words)
	titleWords := strings.Fields(strings.ToLower(issue.Title))
	var keywords []string
	stopWords := map[string]bool{
		"add": true, "fix": true, "update": true, "implement": true,
		"the": true, "a": true, "an": true, "and": true, "or": true,
		"for": true, "to": true, "in": true, "of": true, "with": true,
	}
	for _, word := range titleWords {
		if len(word) > 3 && !stopWords[word] {
			keywords = append(keywords, word)
			if len(keywords) >= 3 { // Limit to first 3 meaningful keywords
				break
			}
		}
	}

	if len(keywords) == 0 {
		// Can't determine novelty without keywords - assume not novel
		return false, nil
	}

	// For now, assume not novel (conservative approach)
	// In the future, this would query storage for similar closed issues
	// The heuristic is currently P0 + structural issues, which is sufficient
	return false, nil
}

// AssessCompletion uses AI to determine if an epic or mission is truly complete.
// This replaces the hardcoded "all children closed = complete" heuristic with AI decision-making.
//
// The AI considers:
// - Are the acceptance criteria met?
// - Is open/blocked work critical or just nice-to-have?
// - Does "complete enough" apply here?
//
// Returns a completion assessment with reasoning.
func (s *Supervisor) AssessCompletion(ctx context.Context, issue *types.Issue, children []*types.Issue) (*CompletionAssessment, error) {
	startTime := time.Now()

	// Build the prompt for completion assessment
	prompt := s.buildCompletionPrompt(issue, children)

	// Call Anthropic API with retry logic
	var response *anthropic.Message
	err := s.retryWithBackoff(ctx, "completion-assessment", func(attemptCtx context.Context) error {
		resp, apiErr := s.client.Messages.New(attemptCtx, anthropic.MessageNewParams{
			Model:     anthropic.Model(s.model),
			MaxTokens: 2048, // Shorter responses for completion decisions
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

	// Extract the text content from the response
	var responseText string
	for _, block := range response.Content {
		if block.Type == "text" {
			responseText += block.Text
		}
	}

	// Parse the response as JSON using resilient parser
	parseResult := Parse[CompletionAssessment](responseText, ParseOptions{
		Context:   "completion assessment response",
		LogErrors: boolPtr(true),
	})
	if !parseResult.Success {
		// vc-227: Truncate AI response to prevent log spam
		return nil, fmt.Errorf("failed to parse completion assessment response: %s (response: %s)", parseResult.Error, truncateString(responseText, 200))
	}
	assessment := parseResult.Data

	// Log the assessment
	duration := time.Since(startTime)
	fmt.Printf("AI Completion Assessment for %s: should_close=%v, confidence=%.2f, duration=%v\n",
		issue.ID, assessment.ShouldClose, assessment.Confidence, duration)

	// Log AI usage to events
	if err := s.recordAIUsage(ctx, issue.ID, "completion-assessment", response.Usage.InputTokens, response.Usage.OutputTokens, duration); err != nil {
		fmt.Fprintf(os.Stderr, "warning: failed to log AI usage for issue %s: %v\n", issue.ID, err)
	}

	return &assessment, nil
}

// buildAssessmentPrompt builds the prompt for assessing an issue before execution
func (s *Supervisor) buildAssessmentPrompt(issue *types.Issue) string {
	// Check if this is a baseline issue - baseline test issues are prime candidates for decomposition (vc-rzqe)
	isBaseline := strings.Contains(issue.ID, "-baseline-")

	decompositionGuidance := ""
	if isBaseline {
		decompositionGuidance = `

**TASK DECOMPOSITION ANALYSIS (vc-rzqe)**:
This is a baseline issue that may involve fixing multiple test failures. Consider whether this should be decomposed:

WHEN TO DECOMPOSE (set should_decompose: true):
- Multiple independent test failures in different files
- Estimated work >60 minutes or >3 files to fix
- Low confidence (<0.7) or high complexity
- Multiple distinct acceptance criteria that could be separate tasks

WHEN NOT TO DECOMPOSE (set should_decompose: false):
- Single test failure or closely related failures
- Simple fix affecting 1-2 files
- High confidence (>0.8) and straightforward approach
- Estimated work <60 minutes

If should_decompose is true, provide a decomposition_plan with:
{
  "reasoning": "Why decomposition is recommended (e.g., '5 independent test failures across different packages')",
  "child_issues": [
    {
      "title": "Fix TestXxx in package Y",
      "description": "Detailed description of what needs to be fixed",
      "acceptance_criteria": "Test passes, related tests still pass",
      "priority": 2,
      "estimated_minutes": 30
    }
  ]
}

Each child issue should be independently completable with bounded context (<50K tokens expected).`
	}

	return fmt.Sprintf(`You are an AI supervisor assessing a coding task before execution. Analyze the following issue and provide a structured assessment.

Issue ID: %s
Title: %s
Type: %s
Priority: %d

Description:
%s

Design:
%s

Acceptance Criteria:
%s
%s

Please provide your assessment as a JSON object with the following structure:
{
  "strategy": "High-level strategy for completing this issue",
  "steps": ["Step 1", "Step 2", ...],
  "risks": ["Risk 1", "Risk 2", ...],
  "confidence": 0.85,
  "reasoning": "Detailed reasoning about the approach",
  "should_decompose": false,
  "decomposition_plan": null
}

Focus on:
1. What's the best approach to tackle this issue?
2. What are the key steps in order?
3. What could go wrong or needs special attention?
4. How confident are you this can be completed successfully?
5. Should this be decomposed into smaller child issues?

IMPORTANT: Respond with ONLY raw JSON. Do NOT wrap it in markdown code fences (`+"`"+`). Just the JSON object.`,
		issue.ID, issue.Title, issue.IssueType, issue.Priority,
		issue.Description, issue.Design, issue.AcceptanceCriteria,
		decompositionGuidance)
}

// buildCompletionPrompt builds the prompt for assessing epic/mission completion
func (s *Supervisor) buildCompletionPrompt(issue *types.Issue, children []*types.Issue) string {
	// Build child summary
	var childSummary strings.Builder
	closedCount := 0
	openCount := 0
	blockedCount := 0

	for _, child := range children {
		statusSymbol := "○"
		switch child.Status {
		case types.StatusClosed:
			statusSymbol = "✓"
			closedCount++
		case types.StatusBlocked:
			statusSymbol = "✗"
			blockedCount++
		default:
			openCount++
		}

		childSummary.WriteString(fmt.Sprintf("%s %s (%s) - %s\n", statusSymbol, child.ID, child.Status, child.Title))
	}

	// Use explicit subtype instead of heuristics (ZFC compliance)
	var issueTypeStr string
	switch issue.IssueSubtype {
	case types.SubtypeMission:
		issueTypeStr = "mission"
	case types.SubtypePhase:
		issueTypeStr = "phase"
	default:
		issueTypeStr = "epic"
	}

	// Add guidance for all structural containers (epics, missions, phases)
	// When all children are closed, the burden of proof shifts to finding concrete gaps
	structuralGuidance := `
IMPORTANT PRINCIPLE:
Epics, missions, and phases are structural containers that organize work into logical groupings.
When ALL children are closed, this strongly indicates the parent's objectives are met,
UNLESS there is clear evidence that the acceptance criteria were not satisfied.

The burden of proof is: if all children are complete, assume the parent is complete unless you can identify
a specific, concrete gap between what was delivered and what was required.

Do not invent hypothetical missing work. If all tracked child issues are closed, trust that the work
breakdown was reasonable and the objectives have been met. Only vote to keep open if there's a clear,
demonstrable gap in what was delivered vs. what the acceptance criteria explicitly require.
`

	return fmt.Sprintf(`You are assessing whether an %s is truly complete and should be closed.

IMPORTANT: Don't just count closed children. Consider whether the OBJECTIVES are met.
%s
%s DETAILS:
ID: %s
Title: %s
Description: %s

Acceptance Criteria:
%s

CHILD ISSUES (%d total: %d closed, %d open, %d blocked):
%s

ASSESSMENT TASK:
Determine if this %s should be closed. Consider:

1. Are the core objectives met? (not just "are children closed")
2. Is blocked or open work critical to the goal?
3. Could this be "complete enough" despite open items?
4. Would closing now vs. later make sense?

Examples of when to close despite open children:
- Core functionality works, open items are polish/enhancements
- Blocked items are non-critical improvements
- Goal achieved even if some "nice-to-haves" remain

Examples of when NOT to close despite all children closed:
- Core acceptance criteria not actually met
- Critical functionality missing even though tasks closed
- Goal not achieved despite busy work completed

Provide your assessment as a JSON object:
{
  "should_close": true/false,
  "reasoning": "Detailed explanation of why this should/shouldn't close",
  "confidence": 0.85,
  "caveats": ["Any concerns or caveats", "..."]
}

Be honest and objective. It's okay to say "not complete" even if most children are closed.
It's also okay to say "complete enough" even if some children are open.

IMPORTANT: Respond with ONLY raw JSON. Do NOT wrap it in markdown code fences (`+"`"+`). Just the JSON object.`,
		issueTypeStr,
		structuralGuidance,
		strings.ToUpper(issueTypeStr),
		issue.ID, issue.Title, issue.Description,
		issue.AcceptanceCriteria,
		len(children), closedCount, openCount, blockedCount,
		childSummary.String(),
		issueTypeStr)
}
