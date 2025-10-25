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

// RecoveryStrategy represents AI-generated strategy for recovering from quality gate failures
type RecoveryStrategy struct {
	Action           string            `json:"action"`             // "fix_in_place", "acceptable_failure", "split_work", "escalate", "retry"
	Reasoning        string            `json:"reasoning"`          // Detailed reasoning for the recommended action
	Confidence       float64           `json:"confidence"`         // Confidence in the recommendation (0.0-1.0)
	CreateIssues     []DiscoveredIssue `json:"create_issues"`      // Issues to create for fixes
	MarkAsBlocked    bool              `json:"mark_as_blocked"`    // Whether to mark original issue as blocked
	CloseOriginal    bool              `json:"close_original"`     // Whether to close the original issue (acceptable failure)
	AddComment       string            `json:"add_comment"`        // Comment to add to original issue
	RequiresApproval bool              `json:"requires_approval"`  // Whether human approval is needed
}

// GateFailure represents a failed quality gate with details
type GateFailure struct {
	Gate   string // Gate type: "test", "lint", "build"
	Output string // Truncated output from the gate
	Error  string // Error message
}

// GenerateRecoveryStrategy uses AI to determine how to recover from quality gate failures.
// This replaces hardcoded recovery logic with AI decision-making (ZFC compliance).
//
// The AI analyzes:
// - Which gates failed and why
// - Issue context and priority
// - Severity of failures
// - Available recovery options
//
// Returns a recovery strategy with specific actions to take.
func (s *Supervisor) GenerateRecoveryStrategy(ctx context.Context, issue *types.Issue, gateResults []GateFailure) (*RecoveryStrategy, error) {
	startTime := time.Now()

	// Build the prompt for recovery strategy
	prompt := s.buildRecoveryPrompt(issue, gateResults)

	// Call Anthropic API with retry logic
	var response *anthropic.Message
	err := s.retryWithBackoff(ctx, "recovery-strategy", func(attemptCtx context.Context) error {
		resp, apiErr := s.client.Messages.New(attemptCtx, anthropic.MessageNewParams{
			Model:     anthropic.Model(s.model),
			MaxTokens: 3072, // Medium-length responses for strategy
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
	parseResult := Parse[RecoveryStrategy](responseText, ParseOptions{
		Context:   "recovery strategy response",
		LogErrors: boolPtr(true),
	})
	if !parseResult.Success {
		return nil, fmt.Errorf("failed to parse recovery strategy response: %s (response: %s)", parseResult.Error, responseText)
	}
	strategy := parseResult.Data

	// Log the strategy
	duration := time.Since(startTime)
	fmt.Printf("AI Recovery Strategy for %s: action=%s, confidence=%.2f, duration=%v\n",
		issue.ID, strategy.Action, strategy.Confidence, duration)

	// Log AI usage to events
	if err := s.logAIUsage(ctx, issue.ID, "recovery-strategy", response.Usage.InputTokens, response.Usage.OutputTokens, duration); err != nil {
		fmt.Fprintf(os.Stderr, "warning: failed to log AI usage: %v\n", err)
	}

	return &strategy, nil
}

// buildRecoveryPrompt builds the prompt for generating a recovery strategy
func (s *Supervisor) buildRecoveryPrompt(issue *types.Issue, gateResults []GateFailure) string {
	// Build failure summary
	var failureSummary strings.Builder
	for i, result := range gateResults {
		failureSummary.WriteString(fmt.Sprintf("\n%d. %s GATE FAILED:\n", i+1, strings.ToUpper(result.Gate)))
		failureSummary.WriteString(fmt.Sprintf("   Error: %s\n", result.Error))
		if result.Output != "" {
			failureSummary.WriteString(fmt.Sprintf("   Output:\n```\n%s\n```\n", result.Output))
		}
	}

	return fmt.Sprintf(`You are determining how to recover from quality gate failures.

IMPORTANT: Don't just create blocking issues. Consider the CONTEXT and SEVERITY.

ISSUE DETAILS:
ID: %s
Title: %s
Type: %s
Priority: P%d
Description: %s

FAILED GATES (%d total):
%s

AVAILABLE RECOVERY ACTIONS:
1. "fix_in_place" - Mark as blocked, create focused fix issues
2. "acceptable_failure" - Close anyway if failures are non-critical or pre-existing
3. "split_work" - Create separate issues for fixes, close original
4. "escalate" - Flag for human review and decision
5. "retry" - Suggest retry (for flaky tests/transient failures)

DECISION CRITERIA:
- Issue priority and type
- Severity of failures
- Whether failures are in the core work or incidental
- Whether failures are pre-existing (not caused by current work)
- Cost/benefit of fixing vs accepting

Examples:
- Flaky test failures → retry or acceptable_failure
- Critical bug in P0 issue → fix_in_place
- Lint warnings in chore task → acceptable_failure (with blocker issue for pre-existing lint errors)
- Build failures → fix_in_place
- Test failures for new features → fix_in_place
- Pre-existing test failures unrelated to current work → acceptable_failure (with blocker issue to fix them)

IMPORTANT for "acceptable_failure":
When failures are PRE-EXISTING (not caused by the current work), you should:
1. Set action to "acceptable_failure"
2. Include the pre-existing issues in "create_issues" array
3. Set discovery_type to "blocker" for these issues
4. These blocker issues will be created to fix the pre-existing problems

Provide your strategy as a JSON object:
{
  "action": "fix_in_place|acceptable_failure|split_work|escalate|retry",
  "reasoning": "Detailed explanation of why this action is recommended",
  "confidence": 0.85,
  "create_issues": [
    {
      "title": "Fix pre-existing lint errors",
      "description": "Details of what needs to be fixed",
      "type": "bug|task",
      "priority": "P0|P1|P2|P3",
      "discovery_type": "blocker|related|background"
    }
  ],
  "mark_as_blocked": true/false,
  "close_original": true/false,
  "add_comment": "Comment to add to original issue explaining the decision",
  "requires_approval": true/false
}

GUIDELINES:
- create_issues: array of issues to create (empty if none needed)
- mark_as_blocked: true if original should be blocked
- close_original: true if original should be closed (acceptable failure)
- add_comment: always provide a comment explaining the decision
- requires_approval: true if human review is needed for this action

Be pragmatic. Not all gate failures require fixes. Consider the bigger picture.

IMPORTANT: Respond with ONLY raw JSON. Do NOT wrap it in markdown code fences (`+"```"+`). Just the JSON object.`,
		issue.ID, issue.Title, issue.IssueType, issue.Priority,
		issue.Description,
		len(gateResults),
		failureSummary.String())
}
