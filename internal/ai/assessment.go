package ai

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/steveyegge/vc/internal/types"
)

// Assessment represents an AI assessment of an issue before execution
type Assessment struct {
	Strategy   string   `json:"strategy"`   // High-level strategy for completing the issue
	Steps      []string `json:"steps"`      // Specific steps to take
	Risks      []string `json:"risks"`      // Potential risks or challenges
	Confidence float64  `json:"confidence"` // Confidence score (0.0-1.0)
	Reasoning  string   `json:"reasoning"`  // Detailed reasoning
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

Please provide your assessment as a JSON object with the following structure:
{
  "strategy": "High-level strategy for completing this issue",
  "steps": ["Step 1", "Step 2", ...],
  "risks": ["Risk 1", "Risk 2", ...],
  "confidence": 0.85,
  "reasoning": "Detailed reasoning about the approach"
}

Focus on:
1. What's the best approach to tackle this issue?
2. What are the key steps in order?
3. What could go wrong or needs special attention?
4. How confident are you this can be completed successfully?

IMPORTANT: Respond with ONLY raw JSON. Do NOT wrap it in markdown code fences (`+"`"+`). Just the JSON object.`,
		issue.ID, issue.Title, issue.IssueType, issue.Priority,
		issue.Description, issue.Design, issue.AcceptanceCriteria)
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
