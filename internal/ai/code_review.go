package ai

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/steveyegge/vc/internal/types"
)

// CodeReviewDecision represents AI decision about whether code review is needed
type CodeReviewDecision struct {
	NeedsReview bool    `json:"needs_review"` // Should this code be reviewed?
	Reasoning   string  `json:"reasoning"`    // Detailed reasoning for the decision
	Confidence  float64 `json:"confidence"`   // Confidence in the assessment (0.0-1.0)
}

// CodeQualityAnalysis represents automated code quality review findings (vc-79)
// This replaces manual code review with AI-driven analysis that automatically files fix issues
type CodeQualityAnalysis struct {
	Issues     []DiscoveredIssue `json:"issues"`     // Specific fix issues to create
	Summary    string            `json:"summary"`    // Overall code quality assessment
	Confidence float64           `json:"confidence"` // Confidence in the analysis (0.0-1.0)
}

// TestSufficiencyAnalysis represents test coverage analysis (vc-79)
// AI analyzes code changes and existing tests to identify gaps
type TestSufficiencyAnalysis struct {
	SufficientCoverage bool              `json:"sufficient_coverage"` // Is test coverage adequate?
	UncoveredAreas     []string          `json:"uncovered_areas"`     // Specific areas lacking tests
	TestIssues         []DiscoveredIssue `json:"test_issues"`         // Test improvement issues to create
	Summary            string            `json:"summary"`             // Overall test coverage assessment
	Confidence         float64           `json:"confidence"`          // Confidence in the analysis (0.0-1.0)
}

// AnalyzeCodeReviewNeed uses AI to decide if code review is warranted based on semantic analysis.
// This is the implementation of vc-214.
//
// The AI analyzes the git diff using Haiku (fast and cheap) to determine:
// - Code complexity and risk level
// - Critical paths (auth, security, data integrity)
// - Semantic significance vs. boilerplate
// - API changes and public interfaces
//
// Returns a decision with reasoning.
func (s *Supervisor) AnalyzeCodeReviewNeed(ctx context.Context, issue *types.Issue, gitDiff string) (*CodeReviewDecision, error) {
	startTime := time.Now()

	// Build the prompt for code review decision
	prompt := s.buildCodeReviewPrompt(issue, gitDiff)

	// Call Anthropic API with retry logic using Haiku (fast and cheap)
	var response *anthropic.Message
	err := s.retryWithBackoff(ctx, "code-review-decision", func(attemptCtx context.Context) error {
		resp, apiErr := s.client.Messages.New(attemptCtx, anthropic.MessageNewParams{
			Model:     anthropic.Model("claude-3-5-haiku-20241022"), // Use Haiku for cost efficiency
			MaxTokens: 1024,                                         // Short decision
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
	parseResult := Parse[CodeReviewDecision](responseText, ParseOptions{
		Context:   "code review decision response",
		LogErrors: boolPtr(true),
	})
	if !parseResult.Success {
		// vc-227: Truncate AI response to prevent log spam
		return nil, fmt.Errorf("failed to parse code review decision response: %s (response: %s)", parseResult.Error, truncateString(responseText, 200))
	}
	decision := parseResult.Data

	// Log the decision
	duration := time.Since(startTime)
	fmt.Printf("AI Code Review Decision for %s: needs_review=%v, confidence=%.2f, duration=%v\n",
		issue.ID, decision.NeedsReview, decision.Confidence, duration)

	// Log AI usage to events
	if err := s.logAIUsage(ctx, issue.ID, "code-review-decision", response.Usage.InputTokens, response.Usage.OutputTokens, duration); err != nil {
		fmt.Fprintf(os.Stderr, "warning: failed to log AI usage: %v\n", err)
	}

	return &decision, nil
}

// AnalyzeTestCoverage analyzes test coverage for code changes and identifies specific test gaps.
// This is the implementation of vc-217.
//
// The AI analyzes the git diff using Sonnet to:
// - Identify what code/logic was added or modified
// - Review existing tests to understand patterns
// - Determine specific test coverage gaps
// - File granular test improvement issues (not generic "add more tests")
//
// Returns test sufficiency analysis with specific test issues to file.
func (s *Supervisor) AnalyzeTestCoverage(ctx context.Context, issue *types.Issue, gitDiff string, existingTests string) (*TestSufficiencyAnalysis, error) {
	startTime := time.Now()

	// Build the prompt for test coverage analysis
	prompt := s.buildTestCoveragePrompt(issue, gitDiff, existingTests)

	// Call Anthropic API with retry logic using Sonnet (thorough analysis)
	var response *anthropic.Message
	err := s.retryWithBackoff(ctx, "test-coverage-analysis", func(attemptCtx context.Context) error {
		resp, apiErr := s.client.Messages.New(attemptCtx, anthropic.MessageNewParams{
			Model:     anthropic.Model(s.model), // Use Sonnet for thorough analysis
			MaxTokens: 4096,                     // Longer responses for detailed analysis
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
	parseResult := Parse[TestSufficiencyAnalysis](responseText, ParseOptions{
		Context:   "test coverage analysis response",
		LogErrors: boolPtr(true),
	})
	if !parseResult.Success {
		// vc-227: Truncate AI response to prevent log spam
		return nil, fmt.Errorf("failed to parse test coverage analysis response: %s (response: %s)", parseResult.Error, truncateString(responseText, 200))
	}
	analysis := parseResult.Data

	// Log the analysis
	duration := time.Since(startTime)
	fmt.Printf("AI Test Coverage Analysis for %s: sufficient=%v, test_issues=%d, confidence=%.2f, duration=%v\n",
		issue.ID, analysis.SufficientCoverage, len(analysis.TestIssues), analysis.Confidence, duration)

	// Log AI usage to events
	if err := s.logAIUsage(ctx, issue.ID, "test-coverage-analysis", response.Usage.InputTokens, response.Usage.OutputTokens, duration); err != nil {
		fmt.Fprintf(os.Stderr, "warning: failed to log AI usage: %v\n", err)
	}

	return &analysis, nil
}

// AnalyzeCodeQuality performs detailed code quality analysis and identifies specific issues.
// This replaces manual code review with AI-driven analysis (vc-216).
//
// The AI analyzes the git diff using Sonnet (not Haiku) for thorough analysis:
// - Code correctness and bugs
// - Security vulnerabilities
// - Performance issues
// - Code smells and maintainability
// - Best practices violations
//
// Returns a list of specific issues to file (each becomes a separate blocking issue).
func (s *Supervisor) AnalyzeCodeQuality(ctx context.Context, issue *types.Issue, gitDiff string) (*CodeQualityAnalysis, error) {
	startTime := time.Now()

	// Build the prompt for code quality analysis
	prompt := s.buildCodeQualityPrompt(issue, gitDiff)

	// Call Anthropic API with retry logic using Sonnet (thorough analysis)
	var response *anthropic.Message
	err := s.retryWithBackoff(ctx, "code-quality-analysis", func(attemptCtx context.Context) error {
		resp, apiErr := s.client.Messages.New(attemptCtx, anthropic.MessageNewParams{
			Model:     anthropic.Model(s.model), // Use Sonnet for thorough analysis
			MaxTokens: 4096,                     // Longer responses for detailed analysis
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
	parseResult := Parse[CodeQualityAnalysis](responseText, ParseOptions{
		Context:   "code quality analysis response",
		LogErrors: boolPtr(true),
	})
	if !parseResult.Success {
		// vc-227: Truncate AI response to prevent log spam
		return nil, fmt.Errorf("failed to parse code quality analysis response: %s (response: %s)", parseResult.Error, truncateString(responseText, 200))
	}
	analysis := parseResult.Data

	// Log the analysis
	duration := time.Since(startTime)
	fmt.Printf("AI Code Quality Analysis for %s: issues=%d, confidence=%.2f, duration=%v\n",
		issue.ID, len(analysis.Issues), analysis.Confidence, duration)

	// Log AI usage to events
	if err := s.logAIUsage(ctx, issue.ID, "code-quality-analysis", response.Usage.InputTokens, response.Usage.OutputTokens, duration); err != nil {
		fmt.Fprintf(os.Stderr, "warning: failed to log AI usage: %v\n", err)
	}

	return &analysis, nil
}

// buildCodeReviewPrompt builds the prompt for deciding if code review is needed
func (s *Supervisor) buildCodeReviewPrompt(issue *types.Issue, gitDiff string) string {
	// Truncate diff if it's too large (keep it under 30k chars for fast processing)
	diffToAnalyze := gitDiff
	wasTruncated := false
	const maxDiffSize = 30000
	if len(gitDiff) > maxDiffSize {
		diffToAnalyze = safeTruncateString(gitDiff, maxDiffSize) + "\n\n... [diff truncated - remaining content omitted for brevity] ..."
		wasTruncated = true
	}

	truncationNote := ""
	if wasTruncated {
		truncationNote = "\n\nNote: The diff was truncated. Base your decision on what's shown."
	}

	return fmt.Sprintf(`You are deciding whether code review is warranted for this change.

IMPORTANT: Use SEMANTIC ANALYSIS, not heuristics like line counts or file counts.

ISSUE CONTEXT:
Issue ID: %s
Title: %s
Type: %s
Priority: P%d
Description: %s

GIT DIFF:
%s%s

DECISION TASK:
Analyze the diff and determine if code review is needed. Consider:

1. **Complexity and Risk**: Are the changes complex or risky?
2. **Critical Paths**: Does this touch auth, security, data integrity, or core business logic?
3. **Semantic Significance**: Is this meaningful code or generated boilerplate?
4. **API Changes**: Are public interfaces or contracts being modified?
5. **Refactoring vs New Features**: Refactoring often needs review, boilerplate doesn't

EXAMPLES WHERE REVIEW **IS** NEEDED:
- 10 lines changing authentication logic → REVIEW
- 30 lines refactoring critical payment code → REVIEW
- API contract changes (even small) → REVIEW
- Security-sensitive changes → REVIEW
- Complex algorithm changes → REVIEW

EXAMPLES WHERE REVIEW **IS NOT** NEEDED:
- 200 lines of generated test fixtures → SKIP
- 100 lines updating dependency versions → SKIP
- Trivial formatting changes → SKIP
- Simple config updates → SKIP
- Documentation-only changes → SKIP

Provide your decision as a JSON object:
{
  "needs_review": true/false,
  "reasoning": "Detailed explanation of why review is/isn't needed",
  "confidence": 0.9
}

Be objective. It's better to request review when unsure than to miss a critical issue.

IMPORTANT: Respond with ONLY raw JSON. Do NOT wrap it in markdown code fences (`+"```"+`). Just the JSON object.`,
		issue.ID, issue.Title, issue.IssueType, issue.Priority,
		issue.Description,
		diffToAnalyze,
		truncationNote)
}

// buildTestCoveragePrompt builds the prompt for test coverage analysis
func (s *Supervisor) buildTestCoveragePrompt(issue *types.Issue, gitDiff string, existingTests string) string {
	// Truncate diff if it's too large
	diffToAnalyze := gitDiff
	diffTruncated := false
	const maxDiffSize = 30000
	if len(gitDiff) > maxDiffSize {
		diffToAnalyze = safeTruncateString(gitDiff, maxDiffSize) + "\n\n... [diff truncated] ..."
		diffTruncated = true
	}

	// Truncate existing tests if too large
	testsToAnalyze := existingTests
	testsTruncated := false
	const maxTestsSize = 20000
	if len(existingTests) > maxTestsSize {
		testsToAnalyze = safeTruncateString(existingTests, maxTestsSize) + "\n\n... [tests truncated] ..."
		testsTruncated = true
	}

	truncationNote := ""
	if diffTruncated || testsTruncated {
		truncationNote = "\n\nNote: Content was truncated. Base your analysis on what's shown."
	}

	return fmt.Sprintf(`You are analyzing test coverage for code changes to identify specific test gaps.

IMPORTANT: Your job is to find SPECIFIC, ACTIONABLE test gaps. Each gap you identify will become a separate test improvement issue.

ISSUE CONTEXT:
Issue ID: %s
Title: %s
Type: %s
Priority: P%d
Description: %s

GIT DIFF (what changed):
%s

EXISTING TESTS (for reference):
%s%s

ANALYSIS TASK:
Analyze the changes and existing tests to identify specific test coverage gaps. Consider:

1. **New Functionality Without Tests**
   - New functions/methods that lack test coverage
   - New API endpoints without integration tests
   - New business logic without unit tests
   - New edge cases introduced

2. **Edge Cases and Error Handling**
   - Error conditions not tested
   - Boundary conditions not covered
   - Null/empty input handling
   - Concurrent access scenarios
   - Resource exhaustion scenarios

3. **Integration and System Tests**
   - Component interactions not tested
   - Database operations without integration tests
   - External service interactions not mocked/tested
   - End-to-end workflows not covered

4. **Security and Data Validation**
   - Input validation not tested
   - Authentication/authorization logic
   - Data sanitization and escaping
   - Permission checks

5. **Regression Prevention**
   - Bug fixes without regression tests
   - Critical paths without coverage
   - Previously broken functionality

GUIDELINES FOR TEST ISSUE CREATION:
- Be SPECIFIC: "Add unit tests for UserAuth.validateToken edge cases (nil token, expired token, invalid signature)" not "Add more tests"
- Be ACTIONABLE: Each issue should clearly describe what tests to add
- Be SELECTIVE: Only file issues for meaningful gaps, not trivial cases
- Include CONTEXT: Explain why the test is needed and what it should cover
- Reference CODE LOCATIONS: Include file names and line numbers/function names
- Assign PRIORITY appropriately:
  - P0: Critical security or data integrity paths with no tests
  - P1: Core functionality, bug fixes, or security-sensitive code without tests
  - P2: Important features or error handling without coverage
  - P3: Nice-to-have coverage for edge cases
- Assign TYPE appropriately:
  - task: Adding new tests (most common)
  - bug: When lack of tests indicates a likely bug
  - chore: Minor test improvements or cleanup

WHEN TO SKIP FILING ISSUES:
- Trivial getters/setters that are tested implicitly
- Code that's already well-tested
- Generated code or boilerplate
- Debug/logging code
- Changes that only refactor without adding logic

EXISTING TEST PATTERNS TO FOLLOW:
Look at the existing tests provided to understand:
- Testing framework being used (e.g., Go testing, Jest, pytest)
- File naming conventions (e.g., _test.go, .test.js, test_*.py)
- Test organization and structure
- Mocking/stubbing patterns
- Assertion style

Provide your analysis as a JSON object:
{
  "sufficient_coverage": false,
  "uncovered_areas": [
    "UserAuth.validateToken edge cases (nil token, expired token, invalid signature)",
    "Payment flow error handling when payment gateway is down",
    "Concurrent access to user session cache"
  ],
  "test_issues": [
    {
      "title": "Add unit tests for UserAuth.validateToken edge cases",
      "description": "The validateToken method in internal/auth/user_auth.go (lines 45-78) handles token validation but lacks tests for edge cases.\n\nAdd tests for:\n- Nil token input (should return error)\n- Expired token (should return specific error)\n- Invalid signature (should return error)\n- Malformed token format (should handle gracefully)\n- Token with missing claims\n\nThese are security-critical paths that must be tested.",
      "type": "task",
      "priority": "P1"
    },
    {
      "title": "Add integration test for payment flow with gateway failures",
      "description": "The payment processing logic in internal/payments/processor.go (lines 120-145) was modified to handle timeouts, but there's no test coverage for failure scenarios.\n\nAdd integration test covering:\n- Gateway connection timeout\n- Gateway returning error response\n- Partial payment failure and rollback\n- Retry logic validation\n\nThis is critical for ensuring payment reliability.",
      "type": "task",
      "priority": "P1"
    }
  ],
  "summary": "Found 2 significant test gaps: UserAuth edge cases and payment error handling. Both are high-priority areas.",
  "confidence": 0.85
}

IMPORTANT NOTES:
- Empty test_issues array is valid if test coverage looks good
- sufficient_coverage should be true only if no meaningful gaps exist
- Don't be overly pedantic - focus on real testing gaps
- Consider the risk and importance of the changed code
- Be constructive and specific in descriptions

IMPORTANT: Respond with ONLY raw JSON. Do NOT wrap it in markdown code fences (`+"```"+`). Just the JSON object.`,
		issue.ID, issue.Title, issue.IssueType, issue.Priority,
		issue.Description,
		diffToAnalyze,
		testsToAnalyze,
		truncationNote)
}

// buildCodeQualityPrompt builds the prompt for automated code quality analysis
func (s *Supervisor) buildCodeQualityPrompt(issue *types.Issue, gitDiff string) string {
	// Truncate diff if it's too large (keep it under 50k chars for thorough analysis)
	diffToAnalyze := gitDiff
	wasTruncated := false
	const maxDiffSize = 50000
	if len(gitDiff) > maxDiffSize {
		diffToAnalyze = safeTruncateString(gitDiff, maxDiffSize) + "\n\n... [diff truncated - remaining content omitted for brevity] ..."
		wasTruncated = true
	}

	truncationNote := ""
	if wasTruncated {
		truncationNote = "\n\nNote: The diff was truncated. Base your analysis on what's shown."
	}

	return fmt.Sprintf(`You are performing automated code quality analysis to identify specific issues that need fixes.

IMPORTANT: Your job is to find SPECIFIC, ACTIONABLE problems. Each issue you identify will become a separate blocking issue.

ISSUE CONTEXT:
Issue ID: %s
Title: %s
Type: %s
Priority: P%d
Description: %s

GIT DIFF:
%s%s

ANALYSIS TASK:
Analyze the diff and identify specific quality issues. Consider:

1. **Code Correctness and Bugs**
   - Logic errors or incorrect implementations
   - Edge cases not handled
   - Potential null pointer dereferences
   - Off-by-one errors
   - Race conditions or concurrency issues

2. **Security Vulnerabilities**
   - SQL injection risks
   - XSS vulnerabilities
   - Authentication/authorization issues
   - Insecure data handling
   - Exposed secrets or credentials

3. **Performance Issues**
   - Inefficient algorithms (O(n²) where O(n) possible)
   - Memory leaks
   - Unnecessary database queries
   - Missing indexes or caching

4. **Code Smells and Maintainability**
   - Code duplication
   - Overly complex functions
   - Poor naming or unclear code
   - Missing error handling
   - Inconsistent patterns

5. **Best Practices Violations**
   - Missing tests for new functionality
   - Inadequate error messages
   - Poor API design
   - Lack of documentation for complex logic

GUIDELINES FOR ISSUE CREATION:
- Be SPECIFIC: "Fix null check in parseConfig line 45" not "Improve error handling"
- Be ACTIONABLE: Each issue should have a clear fix
- Be SELECTIVE: Only file issues for real problems, not nitpicks
- Include CONTEXT: Explain why it's a problem and how to fix it
- Assign PRIORITY appropriately:
  - P0: Critical bugs, security vulnerabilities
  - P1: Important bugs, significant code quality issues
  - P2: Moderate issues, code smells
  - P3: Minor improvements, nitpicks
- Assign TYPE appropriately:
  - bug: Correctness issues, security vulnerabilities
  - task: Refactoring, adding tests, documentation
  - chore: Style fixes, minor cleanup

WHEN TO SKIP FILING ISSUES:
- Trivial style differences (unless project has strict style guide)
- Personal preference disputes
- Changes that are intentional trade-offs
- Generated code that follows patterns

Provide your analysis as a JSON object:
{
  "issues": [
    {
      "title": "Fix null pointer dereference in parseConfig",
      "description": "The parseConfig function at line 45 doesn't check if cfg is nil before accessing cfg.Name. This will panic if parseConfig receives a nil pointer.\n\nFix: Add nil check at the start of the function:\nif cfg == nil { return nil, fmt.Errorf(\"config cannot be nil\") }",
      "type": "bug",
      "priority": "P1"
    },
    {
      "title": "Add unit tests for new authentication logic",
      "description": "The new authentication logic in handleLogin (lines 120-145) has no test coverage. This is security-sensitive code that should be thoroughly tested.\n\nCreate tests for:\n- Valid credentials\n- Invalid credentials\n- Missing credentials\n- Expired tokens",
      "type": "task",
      "priority": "P1"
    }
  ],
  "summary": "Found 2 issues: 1 null pointer bug and missing tests for authentication",
  "confidence": 0.9
}

IMPORTANT NOTES:
- Empty issues array is valid if code looks good
- Don't be overly pedantic - focus on real problems
- Consider the context and intent of the change
- Be constructive and specific in descriptions

IMPORTANT: Respond with ONLY raw JSON. Do NOT wrap it in markdown code fences (`+"```"+`). Just the JSON object.`,
		issue.ID, issue.Title, issue.IssueType, issue.Priority,
		issue.Description,
		diffToAnalyze,
		truncationNote)
}
