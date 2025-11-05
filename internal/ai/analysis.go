package ai

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/steveyegge/vc/internal/types"
)

// Analysis represents an AI analysis of execution results
type Analysis struct {
	Completed        bool              `json:"completed"`         // Was the issue fully completed?
	PuntedItems      []string          `json:"punted_items"`      // Work that was deferred or skipped
	DiscoveredIssues []DiscoveredIssue `json:"discovered_issues"` // New issues found during execution
	QualityIssues    []string          `json:"quality_issues"`    // Quality problems detected
	Summary          string            `json:"summary"`           // Overall summary
	Confidence       float64           `json:"confidence"`        // Confidence in the analysis (0.0-1.0)

	// Enhanced validation fields (vc-179)
	ScopeValidation       *ScopeValidation            `json:"scope_validation,omitempty"`        // Did agent work on correct task?
	AcceptanceCriteriaMet map[string]*CriterionResult `json:"acceptance_criteria_met,omitempty"` // Per-criterion validation
}

// ScopeValidation tracks whether the agent worked on the correct task
type ScopeValidation struct {
	OnTask      bool   `json:"on_task"`     // Did the agent work on THIS issue's task?
	Explanation string `json:"explanation"` // What did the agent actually do?
}

// CriterionResult tracks whether a specific acceptance criterion was met
type CriterionResult struct {
	Met      bool   `json:"met"`                // Was this criterion met?
	Evidence string `json:"evidence,omitempty"` // Evidence from agent output (if met)
	Reason   string `json:"reason,omitempty"`   // Explanation (if not met)
}

// AnalyzeExecutionResult performs AI analysis after executing an issue
func (s *Supervisor) AnalyzeExecutionResult(ctx context.Context, issue *types.Issue, agentOutput string, success bool) (*Analysis, error) {
	startTime := time.Now()

	// Build the prompt for analysis
	prompt := s.buildAnalysisPrompt(issue, agentOutput, success)

	// Call Anthropic API with retry logic
	var response *anthropic.Message
	err := s.retryWithBackoff(ctx, "analysis", func(attemptCtx context.Context) error {
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
	// The parser automatically tries multiple strategies:
	// 1. Direct JSON parse
	// 2. Remove markdown code fences (```json, ```, etc.) and retry
	// 3. Fix common JSON issues (trailing commas, unquoted keys, comments) and retry
	// 4. Extract JSON from mixed content and retry
	parseResult := Parse[Analysis](responseText, ParseOptions{
		Context:   "analysis response",
		LogErrors: boolPtr(true),
	})
	if !parseResult.Success {
		// Show truncated response to avoid overwhelming logs
		// Note: Error message shows ORIGINAL text, but parser tried cleaning strategies above
		truncatedResponse := responseText
		if len(responseText) > 500 {
			truncatedResponse = responseText[:500] + "... (truncated)"
		}
		return nil, fmt.Errorf("%s\n\nNote: Parser tried all strategies (fence removal, JSON cleanup, extraction). Original response: %s", parseResult.Error, truncatedResponse)
	}
	analysis := parseResult.Data

	// Log the analysis
	duration := time.Since(startTime)
	fmt.Printf("AI Analysis for %s: completed=%v, discovered=%d issues, quality=%d issues, duration=%v\n",
		issue.ID, analysis.Completed, len(analysis.DiscoveredIssues), len(analysis.QualityIssues), duration)

	// Log AI usage to events
	if err := s.recordAIUsage(ctx, issue.ID, "analysis", response.Usage.InputTokens, response.Usage.OutputTokens, duration); err != nil {
		fmt.Fprintf(os.Stderr, "warning: failed to log AI usage: %v\n", err)
	}

	return &analysis, nil
}

// buildAnalysisPrompt builds the prompt for analyzing execution results
func (s *Supervisor) buildAnalysisPrompt(issue *types.Issue, agentOutput string, success bool) string {
	successStr := "succeeded"
	if !success {
		successStr = "failed"
	}

	return fmt.Sprintf(`You are an AI supervisor analyzing the results of a coding task. The agent has finished executing the following issue.

Issue ID: %s
Title: %s
Description: %s
Acceptance Criteria: %s

Agent Execution Status: %s

Agent Output (last 8000 chars):
%s

CRITICAL: Your primary job is to verify the agent did the RIGHT work, not just ANY work.

Please analyze the execution systematically:

1. SCOPE VALIDATION (Most Important!)
   - Did the agent work on THIS issue's task, or did it work on something else?
   - Compare what the agent did vs. what the Description and Acceptance Criteria asked for
   - If the agent did unrelated work, mark as NOT completed

2. ACCEPTANCE CRITERIA VALIDATION
   Quote each acceptance criterion and explicitly state whether it was met:
   %s

   For each criterion:
   - Was it addressed? (yes/no)
   - What evidence from the agent output shows it was met?
   - If not met, why not?

3. QUALITY ASSESSMENT
   - Are there any code quality issues? (lint errors, test failures, missing error handling, etc.)
   - Did the agent introduce new bugs or technical debt?
   - Are there missing tests or documentation?

4. WORK DISCOVERED
   - What follow-on work was mentioned but not completed?
   - Were any new bugs, tasks, or improvements discovered?

   For each discovered issue, classify its relationship to the parent mission:
   - "blocker": Blocks parent mission from completing (quality gate failures, missing dependencies, pre-existing bugs)
   - "related": Related to parent mission but not blocking (tech debt, improvements, follow-on enhancements)
   - "background": Opportunistic discoveries unrelated to mission (general refactoring, unrelated bugs)

Provide your analysis as a JSON object:
{
  "completed": true,
  "scope_validation": {
    "on_task": true,
    "explanation": "Agent worked on X which matches the issue requirements"
  },
  "acceptance_criteria_met": {
    "criterion_1": {"met": true, "evidence": "..."},
    "criterion_2": {"met": false, "reason": "..."}
  },
  "punted_items": ["Work that was deferred", ...],
  "discovered_issues": [
    {
      "title": "New issue title",
      "description": "Issue description",
      "type": "bug|task|enhancement",
      "priority": "P0|P1|P2|P3",
      "discovery_type": "blocker|related|background"
    }
  ],
  "quality_issues": ["Quality problem 1", ...],
  "summary": "Overall summary of what was accomplished",
  "confidence": 0.9
}

RULES:
1. Set "completed": false if the agent worked on the WRONG task (even if the work was good)
2. Set "completed": false if ANY acceptance criterion was not met
3. Be SPECIFIC in quality_issues - don't say "add tests", say "add unit tests for function X"
4. If agent output was truncated, note this in the summary

IMPORTANT: Respond with ONLY raw JSON. Do NOT wrap it in markdown code fences (`+"`"+`). Just the JSON object.`,
		issue.ID, issue.Title, issue.Description, issue.AcceptanceCriteria,
		successStr, truncateString(agentOutput, 8000), issue.AcceptanceCriteria)
}
