package ai

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/steveyegge/vc/internal/types"
)

// TestFailureDiagnosis represents AI diagnosis of a test failure
// vc-210: Self-healing - AI agent can fix baseline test failures
type TestFailureDiagnosis struct {
	FailureType  FailureType  `json:"failure_type"`  // Type of failure: flaky, real, or environmental
	RootCause    string       `json:"root_cause"`    // Detailed explanation of why the test is failing
	ProposedFix  string       `json:"proposed_fix"`  // Proposed fix with rationale
	Confidence   float64      `json:"confidence"`    // Confidence in the diagnosis (0.0-1.0)
	TestNames    []string     `json:"test_names"`    // List of failing test names
	StackTraces  []string     `json:"stack_traces"`  // Relevant stack traces
	Verification []string     `json:"verification"`  // Steps to verify the fix works
}

// FailureType categorizes test failures
type FailureType string

const (
	FailureTypeFlaky         FailureType = "flaky"         // Intermittent failure (race condition, timing)
	FailureTypeReal          FailureType = "real"          // Actual bug in code
	FailureTypeEnvironmental FailureType = "environmental" // External dependency issue
	FailureTypeUnknown       FailureType = "unknown"       // Cannot determine
)

// DiagnoseTestFailure analyzes test failure output and provides a structured diagnosis
// This helps the agent understand what type of failure it is and how to fix it
func (s *Supervisor) DiagnoseTestFailure(ctx context.Context, issue *types.Issue, testOutput string) (*TestFailureDiagnosis, error) {
	// Input validation (vc-225)
	if issue == nil {
		return nil, fmt.Errorf("issue cannot be nil")
	}
	if testOutput == "" {
		return nil, fmt.Errorf("test output cannot be empty")
	}
	// Truncate very large outputs to avoid excessive AI API costs
	if len(testOutput) > 100000 {
		testOutput = testOutput[:100000] + "\n... (truncated)"
	}

	startTime := time.Now()

	// Build the diagnosis prompt
	prompt := s.buildTestFailureDiagnosisPrompt(issue, testOutput)

	// Call Anthropic API with retry logic
	var response *anthropic.Message
	err := s.retryWithBackoff(ctx, "test-failure-diagnosis", func(attemptCtx context.Context) error {
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
	parseResult := Parse[TestFailureDiagnosis](responseText, ParseOptions{
		Context:   "test failure diagnosis response",
		LogErrors: boolPtr(true),
	})
	if !parseResult.Success {
		// vc-227: Truncate AI response to prevent log spam
		return nil, fmt.Errorf("failed to parse test failure diagnosis: %s (response: %s)", parseResult.Error, truncateString(responseText, 200))
	}
	diagnosis := parseResult.Data

	// Log the diagnosis
	duration := time.Since(startTime)
	fmt.Printf("AI Test Failure Diagnosis for %s: type=%s, confidence=%.2f, duration=%v\n",
		issue.ID, diagnosis.FailureType, diagnosis.Confidence, duration)

	// Log AI usage to events
	if err := s.logAIUsage(ctx, issue.ID, "test-failure-diagnosis", response.Usage.InputTokens, response.Usage.OutputTokens, duration); err != nil {
		fmt.Fprintf(os.Stderr, "warning: failed to log AI usage: %v\n", err)
	}

	return &diagnosis, nil
}

// buildTestFailureDiagnosisPrompt constructs the prompt for diagnosing test failures
func (s *Supervisor) buildTestFailureDiagnosisPrompt(issue *types.Issue, testOutput string) string {
	return fmt.Sprintf(`You are an AI expert diagnosing test failures. Analyze the following baseline test failure and provide a structured diagnosis.

Issue: %s
Title: %s

Description:
%s

Test Output (last 8000 chars):
%s

DIAGNOSTIC FRAMEWORK:

Classify the failure into one of these types:

1. **FLAKY** - Test passes sometimes, fails sometimes:
   - Indicators: Race conditions, timing issues, goroutines, channels
   - Common causes: Non-deterministic behavior, shared mutable state, hardcoded timeouts
   - Look for: "fatal error: concurrent map writes", timing-dependent logic, randomness

2. **REAL** - Actual bug in the code being tested:
   - Indicators: Consistent failure, assertion errors, logic errors
   - Common causes: Code change broke functionality, missing null checks, wrong logic
   - Look for: Assertion failures, unexpected values, incorrect behavior

3. **ENVIRONMENTAL** - External dependency or setup issue:
   - Indicators: Missing files, network errors, dependency unavailable
   - Common causes: Missing test fixtures, external service down, environment variables
   - Look for: "file not found", "connection refused", "command not found"

Provide your diagnosis as a JSON object:
{
  "failure_type": "flaky|real|environmental|unknown",
  "root_cause": "Detailed explanation of why the test is failing",
  "proposed_fix": "Specific fix to apply with clear rationale",
  "confidence": 0.85,
  "test_names": ["TestFunctionName", ...],
  "stack_traces": ["Relevant stack trace excerpts", ...],
  "verification": [
    "Step 1: Run the specific test 10 times",
    "Step 2: Run full test suite",
    "Step 3: Check for regressions"
  ]
}

RULES:
1. Be SPECIFIC about the root cause - don't just describe symptoms
2. Proposed fix should be actionable - exact code changes or steps
3. For flaky tests, identify the source of non-determinism
4. For real failures, trace through the logic to find the bug
5. Include concrete verification steps

IMPORTANT: Respond with ONLY raw JSON. Do NOT wrap it in markdown code fences (` + "`" + `). Just the JSON object.`,
		issue.ID, issue.Title, issue.Description, truncateString(testOutput, 8000))
}
