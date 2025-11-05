package ai

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/steveyegge/vc/internal/types"
)

// DuplicateCheckResponse represents the AI's analysis of whether a candidate issue
// is a duplicate of an existing issue
type DuplicateCheckResponse struct {
	IsDuplicate bool    `json:"is_duplicate"` // Is the candidate a duplicate?
	Confidence  float64 `json:"confidence"`   // Confidence score (0.0-1.0)
	Reasoning   string  `json:"reasoning"`    // Explanation of the determination
}

// BatchDuplicateCheckResult represents a single comparison result in a batch check
type BatchDuplicateCheckResult struct {
	ExistingIssueID string  `json:"existing_issue_id"` // ID of the existing issue compared against
	IsDuplicate     bool    `json:"is_duplicate"`      // Is the candidate a duplicate of this issue?
	Confidence      float64 `json:"confidence"`        // Confidence score (0.0-1.0)
	Reasoning       string  `json:"reasoning"`         // Explanation of the determination
}

// BatchDuplicateCheckResponse represents the AI's analysis when comparing one candidate
// against multiple existing issues in a single API call
type BatchDuplicateCheckResponse struct {
	Results []BatchDuplicateCheckResult `json:"results"` // One result per existing issue
}

// CheckIssueDuplicate uses AI to determine if a candidate issue is a duplicate
// of an existing issue. This is used by the deduplication system to prevent
// filing duplicate issues.
//
// Parameters:
//   - ctx: Context for the API call
//   - candidate: The issue to check for duplicates
//   - existing: The existing issue to compare against
//
// Returns:
//   - DuplicateCheckResponse with AI's determination and reasoning
//   - Error if the API call fails
//
// The AI compares semantic similarity, considering:
// - Title similarity (semantic, not just string matching)
// - File/line references in descriptions
// - Parent issue context (if any)
// - Issue type and priority
func (s *Supervisor) CheckIssueDuplicate(ctx context.Context, candidate, existing *types.Issue) (*DuplicateCheckResponse, error) {
	startTime := time.Now()

	// Build prompt
	prompt := s.buildDuplicateCheckPrompt(candidate, existing)

	// Call AI with retry
	var responseText string
	var err error

	err = s.retryWithBackoff(ctx, "duplicate_check", func(ctx context.Context) error {
		responseText, err = s.CallAI(ctx, prompt, "duplicate_check", s.model, 1000)
		return err
	})

	if err != nil {
		return nil, fmt.Errorf("AI duplicate check failed: %w", err)
	}

	// Parse response using resilient parser
	parseResult := Parse[DuplicateCheckResponse](responseText, ParseOptions{
		Context:   "duplicate check response",
		LogErrors: boolPtr(true),
	})
	if !parseResult.Success {
		// vc-227: Truncate AI response to prevent log spam
		return nil, fmt.Errorf("failed to parse duplicate check response: %s (response: %s)", parseResult.Error, truncateString(responseText, 200))
	}
	response := parseResult.Data

	// Validate response
	if response.Confidence < 0.0 || response.Confidence > 1.0 {
		return nil, fmt.Errorf("invalid confidence score: %.2f (must be 0.0-1.0)", response.Confidence)
	}

	// Log AI usage (don't fail on logging errors)
	duration := time.Since(startTime)
	_ = s.recordAIUsage(ctx, candidate.ID, fmt.Sprintf("duplicate_check vs %s", existing.ID), 0, 0, duration)

	return &response, nil
}

// CheckIssueDuplicateBatch uses AI to check if a candidate issue is a duplicate
// of any issue in a batch of existing issues. This is much more efficient than
// calling CheckIssueDuplicate multiple times, as it makes a single AI API call
// to compare against multiple existing issues.
//
// Parameters:
//   - ctx: Context for the API call
//   - candidate: The issue to check for duplicates
//   - existingIssues: The batch of existing issues to compare against
//
// Returns:
//   - A slice of results (one per existing issue) with duplicate determination
//   - An error if the AI call fails or response cannot be parsed
//
// The AI compares semantic similarity considering title, description, file references,
// and context. This is significantly faster than individual comparisons when checking
// against many existing issues.
//
// Example:
//
//	existingIssues := []*types.Issue{issue1, issue2, issue3}
//	results, err := supervisor.CheckIssueDuplicateBatch(ctx, candidate, existingIssues)
//	for _, result := range results.Results {
//	    if result.IsDuplicate && result.Confidence >= 0.85 {
//	        log.Printf("Duplicate of %s (confidence: %.2f)", result.ExistingIssueID, result.Confidence)
//	    }
//	}
func (s *Supervisor) CheckIssueDuplicateBatch(ctx context.Context, candidate *types.Issue, existingIssues []*types.Issue) (*BatchDuplicateCheckResponse, error) {
	startTime := time.Now()

	// Validate inputs
	if candidate == nil {
		return nil, fmt.Errorf("candidate issue cannot be nil")
	}
	if len(existingIssues) == 0 {
		return &BatchDuplicateCheckResponse{Results: []BatchDuplicateCheckResult{}}, nil
	}

	// Build prompt
	prompt := s.buildBatchDuplicateCheckPrompt(candidate, existingIssues)

	// Each result needs ~100 tokens, plus overhead
	maxTokens := len(existingIssues)*150 + 200
	if maxTokens < 1000 {
		maxTokens = 1000
	}
	if maxTokens > 4000 {
		maxTokens = 4000
	}

	// Call AI with retry
	var responseText string
	var err error

	err = s.retryWithBackoff(ctx, "batch_duplicate_check", func(ctx context.Context) error {
		responseText, err = s.CallAI(ctx, prompt, "batch_duplicate_check", s.model, maxTokens)
		return err
	})

	if err != nil {
		return nil, fmt.Errorf("AI batch duplicate check failed: %w", err)
	}

	// Parse response using resilient parser
	parseResult := Parse[BatchDuplicateCheckResponse](responseText, ParseOptions{
		Context:   "batch duplicate check response",
		LogErrors: boolPtr(true),
	})
	if !parseResult.Success {
		// vc-227: Truncate AI response to prevent log spam
		return nil, fmt.Errorf("failed to parse batch duplicate check response: %s (response: %s)", parseResult.Error, truncateString(responseText, 200))
	}
	response := parseResult.Data

	// Validate response: should have one result per existing issue
	if len(response.Results) != len(existingIssues) {
		log.Printf("[WARN] Batch duplicate check returned %d results, expected %d", len(response.Results), len(existingIssues))

		// If we got less than half the expected results, fail rather than give false confidence
		// This prevents scenarios where we think we compared against 50 issues but only got 10 results
		if len(response.Results) < len(existingIssues)/2 {
			return nil, fmt.Errorf("insufficient results from AI: got %d, expected %d (less than 50%% coverage)",
				len(response.Results), len(existingIssues))
		}
		// If we got more than 50% coverage, continue with partial results (fail-safe)
		// The deduplicator's fail-safe behavior will handle this gracefully
	}

	// Validate each result
	for i, result := range response.Results {
		if result.Confidence < 0.0 || result.Confidence > 1.0 {
			return nil, fmt.Errorf("invalid confidence score in result %d: %.2f (must be 0.0-1.0)", i, result.Confidence)
		}
		// Verify the existing_issue_id matches one of the input issues
		found := false
		for _, existing := range existingIssues {
			if result.ExistingIssueID == existing.ID {
				found = true
				break
			}
		}
		if !found {
			log.Printf("[WARN] Result %d references unknown issue ID: %s", i, result.ExistingIssueID)
		}
	}

	// Log AI usage (don't fail on logging errors)
	duration := time.Since(startTime)
	issueIDs := make([]string, len(existingIssues))
	for i, issue := range existingIssues {
		issueIDs[i] = issue.ID
	}
	_ = s.recordAIUsage(ctx, candidate.ID, fmt.Sprintf("batch_duplicate_check vs [%s]", join(issueIDs, ",")), 0, 0, duration)

	return &response, nil
}

// buildDuplicateCheckPrompt builds the AI prompt for duplicate detection
func (s *Supervisor) buildDuplicateCheckPrompt(candidate, existing *types.Issue) string {
	return fmt.Sprintf(`You are analyzing whether two issues are duplicates.

CANDIDATE ISSUE:
ID: %s
Title: %s
Type: %s
Priority: P%d
Description: %s

EXISTING ISSUE:
ID: %s
Title: %s
Type: %s
Priority: P%d
Description: %s

TASK:
Determine if the CANDIDATE issue is a semantic duplicate of the EXISTING issue.

IMPORTANT GUIDELINES:
1. Consider SEMANTIC SIMILARITY, not just exact string matching
2. Two issues are duplicates if they describe the SAME underlying problem/task
3. Different wording is OK if they address the same issue
4. Same file/function/line reference strongly suggests duplicate
5. Different priority or type does NOT automatically mean non-duplicate
6. If the EXISTING issue is an epic/container, check if CANDIDATE is a subset of its goals
7. Small differences in scope may still be duplicates (e.g., "fix null check" vs "add validation")

EXAMPLES OF DUPLICATES:
- "Fix null pointer in parseConfig line 45" vs "Add null check to parseConfig:45"
- "Improve error handling in auth" vs "Better error messages in authentication module"
- "Update docs for API" vs "Document REST API endpoints"

EXAMPLES OF NON-DUPLICATES:
- "Fix login bug" vs "Add registration feature"
- "Optimize database queries" vs "Fix database connection leak"
- "Add tests for parser" vs "Fix parser crash on empty input"

OUTPUT FORMAT (JSON only, no markdown):
{
  "is_duplicate": boolean,
  "confidence": float (0.0-1.0),
  "reasoning": "Brief explanation of why this is/isn't a duplicate"
}

CONFIDENCE SCORING:
- 0.95-1.0: Exact same issue, possibly same file/line
- 0.85-0.95: Very similar issue, same root cause
- 0.70-0.85: Related issues, but different aspects
- 0.50-0.70: Somewhat similar, different problems
- 0.0-0.50: Different issues

IMPORTANT: Respond with ONLY raw JSON. Do NOT wrap it in markdown code fences.`,
		candidate.ID, candidate.Title, candidate.IssueType, candidate.Priority, candidate.Description,
		existing.ID, existing.Title, existing.IssueType, existing.Priority, existing.Description)
}

// buildBatchDuplicateCheckPrompt creates a prompt to check if a candidate issue
// is a duplicate of any in a batch of existing issues
func (s *Supervisor) buildBatchDuplicateCheckPrompt(candidate *types.Issue, existingIssues []*types.Issue) string {
	// Build the candidate section
	prompt := fmt.Sprintf(`You are analyzing whether a candidate issue is a duplicate of any existing issues.

CANDIDATE ISSUE:
ID: %s
Title: %s
Type: %s
Priority: P%d
Description: %s

EXISTING ISSUES TO COMPARE AGAINST:
`,
		candidate.ID, candidate.Title, candidate.IssueType, candidate.Priority, candidate.Description)

	// Add each existing issue
	for i, existing := range existingIssues {
		prompt += fmt.Sprintf(`
[%d] ID: %s
    Title: %s
    Type: %s
    Priority: P%d
    Description: %s
`,
			i+1, existing.ID, existing.Title, existing.IssueType, existing.Priority, existing.Description)
	}

	// Add task instructions
	prompt += `
TASK:
For EACH existing issue listed above, determine if the CANDIDATE issue is a semantic duplicate.

IMPORTANT GUIDELINES:
1. Consider SEMANTIC SIMILARITY, not just exact string matching
2. Two issues are duplicates if they describe the SAME underlying problem/task
3. Different wording is OK if they address the same issue
4. Same file/function/line reference strongly suggests duplicate
5. Different priority or type does NOT automatically mean non-duplicate
6. If the EXISTING issue is an epic/container, check if CANDIDATE is a subset of its goals
7. Small differences in scope may still be duplicates (e.g., "fix null check" vs "add validation")

EXAMPLES OF DUPLICATES:
- "Fix null pointer in parseConfig line 45" vs "Add null check to parseConfig:45"
- "Improve error handling in auth" vs "Better error messages in authentication module"
- "Update docs for API" vs "Document REST API endpoints"

EXAMPLES OF NON-DUPLICATES:
- "Fix login bug" vs "Add registration feature"
- "Optimize database queries" vs "Fix database connection leak"
- "Add tests for parser" vs "Fix parser crash on empty input"

OUTPUT FORMAT (JSON only, no markdown):
{
  "results": [
    {
      "existing_issue_id": "issue_id_1",
      "is_duplicate": boolean,
      "confidence": float (0.0-1.0),
      "reasoning": "Brief explanation"
    },
    {
      "existing_issue_id": "issue_id_2",
      "is_duplicate": boolean,
      "confidence": float (0.0-1.0),
      "reasoning": "Brief explanation"
    }
    // ... one entry for each existing issue
  ]
}

CONFIDENCE SCORING:
- 0.95-1.0: Exact same issue, possibly same file/line
- 0.85-0.95: Very similar issue, same root cause
- 0.70-0.85: Related issues, but different aspects
- 0.50-0.70: Somewhat similar, different problems
- 0.0-0.50: Different issues

IMPORTANT:
1. Respond with ONLY raw JSON. Do NOT wrap it in markdown code fences.
2. Include exactly one result per existing issue in the same order they were listed.
3. The "existing_issue_id" field must match the ID of each existing issue.
`

	return prompt
}
