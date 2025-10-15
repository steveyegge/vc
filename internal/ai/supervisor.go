package ai

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/steveyegge/vc/internal/storage"
	"github.com/steveyegge/vc/internal/types"
)

// Supervisor handles AI-powered assessment and analysis of issues
type Supervisor struct {
	client *anthropic.Client
	store  storage.Storage
	model  string
	retry  RetryConfig
}

// RetryConfig holds retry configuration for API calls
type RetryConfig struct {
	MaxRetries      int           // Maximum number of retries (default: 3)
	InitialBackoff  time.Duration // Initial backoff duration (default: 1s)
	MaxBackoff      time.Duration // Maximum backoff duration (default: 30s)
	BackoffMultiplier float64     // Backoff multiplier (default: 2.0)
	Timeout         time.Duration // Per-request timeout (default: 60s)
}

// DefaultRetryConfig returns the default retry configuration
func DefaultRetryConfig() RetryConfig {
	return RetryConfig{
		MaxRetries:        3,
		InitialBackoff:    1 * time.Second,
		MaxBackoff:        30 * time.Second,
		BackoffMultiplier: 2.0,
		Timeout:           60 * time.Second,
	}
}

// Config holds supervisor configuration
type Config struct {
	APIKey string // Anthropic API key (if empty, reads from ANTHROPIC_API_KEY env var)
	Model  string // Model to use (default: claude-sonnet-4-5-20250929)
	Store  storage.Storage
	Retry  RetryConfig // Retry configuration (uses defaults if not specified)
}

// NewSupervisor creates a new AI supervisor
func NewSupervisor(cfg *Config) (*Supervisor, error) {
	if cfg.Store == nil {
		return nil, fmt.Errorf("storage is required")
	}

	apiKey := cfg.APIKey
	if apiKey == "" {
		apiKey = os.Getenv("ANTHROPIC_API_KEY")
		if apiKey == "" {
			return nil, fmt.Errorf("ANTHROPIC_API_KEY not set")
		}
	}

	model := cfg.Model
	if model == "" {
		model = "claude-sonnet-4-5-20250929"
	}

	// Use default retry config if not specified
	retry := cfg.Retry
	if retry.MaxRetries == 0 {
		retry = DefaultRetryConfig()
	}

	client := anthropic.NewClient(option.WithAPIKey(apiKey))

	return &Supervisor{
		client: &client,
		store:  cfg.Store,
		model:  model,
		retry:  retry,
	}, nil
}

// retryWithBackoff executes an operation with exponential backoff retry logic
func (s *Supervisor) retryWithBackoff(ctx context.Context, operation string, fn func(context.Context) error) error {
	var lastErr error
	backoff := s.retry.InitialBackoff

	for attempt := 0; attempt <= s.retry.MaxRetries; attempt++ {
		// Create timeout context for this attempt
		attemptCtx, cancel := context.WithTimeout(ctx, s.retry.Timeout)

		// Execute the operation
		err := fn(attemptCtx)
		cancel()

		// Success!
		if err == nil {
			if attempt > 0 {
				fmt.Printf("AI API %s succeeded after %d retries\n", operation, attempt)
			}
			return nil
		}

		lastErr = err

		// Check if we should retry
		if !isRetriableError(err) {
			fmt.Fprintf(os.Stderr, "AI API %s failed with non-retriable error: %v\n", operation, err)
			return err
		}

		// Don't retry if we've exhausted attempts
		if attempt == s.retry.MaxRetries {
			break
		}

		// Check if context is already cancelled
		if ctx.Err() != nil {
			return fmt.Errorf("%s failed: context cancelled: %w", operation, ctx.Err())
		}

		// Log the retry
		fmt.Printf("AI API %s failed (attempt %d/%d), retrying in %v: %v\n",
			operation, attempt+1, s.retry.MaxRetries+1, backoff, err)

		// Sleep with exponential backoff
		select {
		case <-time.After(backoff):
			// Calculate next backoff with exponential growth
			backoff = time.Duration(float64(backoff) * s.retry.BackoffMultiplier)
			if backoff > s.retry.MaxBackoff {
				backoff = s.retry.MaxBackoff
			}
		case <-ctx.Done():
			return fmt.Errorf("%s failed: context cancelled during backoff: %w", operation, ctx.Err())
		}
	}

	return fmt.Errorf("%s failed after %d attempts: %w", operation, s.retry.MaxRetries+1, lastErr)
}

// isRetriableError determines if an error is retriable (transient)
func isRetriableError(err error) bool {
	if err == nil {
		return false
	}

	// Network errors and timeouts are retriable
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}

	// Check for HTTP status codes indicating transient errors
	// Anthropic SDK should wrap these, but we check the error string
	errStr := err.Error()

	// Rate limits (429) are retriable
	if strings.Contains(errStr, "429") || strings.Contains(errStr, "rate limit") {
		return true
	}

	// Server errors (5xx) are retriable
	if strings.Contains(errStr, "500") || strings.Contains(errStr, "502") ||
	   strings.Contains(errStr, "503") || strings.Contains(errStr, "504") ||
	   strings.Contains(errStr, "internal server error") ||
	   strings.Contains(errStr, "bad gateway") ||
	   strings.Contains(errStr, "service unavailable") ||
	   strings.Contains(errStr, "gateway timeout") {
		return true
	}

	// Network/connection errors are retriable
	if strings.Contains(errStr, "connection refused") ||
	   strings.Contains(errStr, "connection reset") ||
	   strings.Contains(errStr, "timeout") ||
	   strings.Contains(errStr, "temporary failure") ||
	   strings.Contains(errStr, "network") {
		return true
	}

	// 4xx client errors (except rate limits) are NOT retriable
	// These indicate bad requests that won't succeed on retry
	if strings.Contains(errStr, "400") || strings.Contains(errStr, "401") ||
	   strings.Contains(errStr, "403") || strings.Contains(errStr, "404") {
		return false
	}

	// Default to not retrying unknown errors
	return false
}

// Assessment represents an AI assessment of an issue before execution
type Assessment struct {
	Strategy   string   `json:"strategy"`    // High-level strategy for completing the issue
	Steps      []string `json:"steps"`       // Specific steps to take
	Risks      []string `json:"risks"`       // Potential risks or challenges
	Confidence float64  `json:"confidence"`  // Confidence score (0.0-1.0)
	Reasoning  string   `json:"reasoning"`   // Detailed reasoning
	EstimatedEffort string `json:"estimated_effort"` // e.g., "5 minutes", "1 hour", "4 hours"
}

// Analysis represents an AI analysis of execution results
type Analysis struct {
	Completed        bool     `json:"completed"`         // Was the issue fully completed?
	PuntedItems      []string `json:"punted_items"`      // Work that was deferred or skipped
	DiscoveredIssues []DiscoveredIssue `json:"discovered_issues"` // New issues found during execution
	QualityIssues    []string `json:"quality_issues"`    // Quality problems detected
	Summary          string   `json:"summary"`           // Overall summary
	Confidence       float64  `json:"confidence"`        // Confidence in the analysis (0.0-1.0)
}

// DiscoveredIssue represents a new issue discovered during execution
type DiscoveredIssue struct {
	Title       string `json:"title"`
	Description string `json:"description"`
	Type        string `json:"type"`     // bug, task, enhancement, etc.
	Priority    string `json:"priority"` // P0, P1, P2, P3
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
		LogErrors: true,
	})
	if !parseResult.Success {
		return nil, fmt.Errorf("failed to parse assessment response: %s (response: %s)", parseResult.Error, responseText)
	}
	assessment := parseResult.Data

	// Log the assessment
	duration := time.Since(startTime)
	fmt.Printf("AI Assessment for %s: confidence=%.2f, effort=%s, duration=%v\n",
		issue.ID, assessment.Confidence, assessment.EstimatedEffort, duration)

	// Log AI usage to events
	if err := s.logAIUsage(ctx, issue.ID, "assessment", response.Usage.InputTokens, response.Usage.OutputTokens, duration); err != nil {
		fmt.Fprintf(os.Stderr, "warning: failed to log AI usage: %v\n", err)
	}

	return &assessment, nil
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
	parseResult := Parse[Analysis](responseText, ParseOptions{
		Context:   "analysis response",
		LogErrors: true,
	})
	if !parseResult.Success {
		return nil, fmt.Errorf("failed to parse analysis response: %s (response: %s)", parseResult.Error, responseText)
	}
	analysis := parseResult.Data

	// Log the analysis
	duration := time.Since(startTime)
	fmt.Printf("AI Analysis for %s: completed=%v, discovered=%d issues, quality=%d issues, duration=%v\n",
		issue.ID, analysis.Completed, len(analysis.DiscoveredIssues), len(analysis.QualityIssues), duration)

	// Log AI usage to events
	if err := s.logAIUsage(ctx, issue.ID, "analysis", response.Usage.InputTokens, response.Usage.OutputTokens, duration); err != nil {
		fmt.Fprintf(os.Stderr, "warning: failed to log AI usage: %v\n", err)
	}

	return &analysis, nil
}

// buildAssessmentPrompt builds the prompt for assessing an issue
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
  "reasoning": "Detailed reasoning about the approach",
  "estimated_effort": "30 minutes"
}

Focus on:
1. What's the best approach to tackle this issue?
2. What are the key steps in order?
3. What could go wrong or needs special attention?
4. How confident are you this can be completed successfully?
5. How long will this likely take?

Respond with ONLY the JSON object, no additional text.`,
		issue.ID, issue.Title, issue.IssueType, issue.Priority,
		issue.Description, issue.Design, issue.AcceptanceCriteria)
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

Agent Output (last 2000 chars):
%s

Please analyze the execution and provide a structured response as a JSON object:
{
  "completed": true,
  "punted_items": ["Work that was deferred", ...],
  "discovered_issues": [
    {
      "title": "New issue title",
      "description": "Issue description",
      "type": "bug|task|enhancement",
      "priority": "P0|P1|P2|P3"
    }
  ],
  "quality_issues": ["Quality problem 1", ...],
  "summary": "Overall summary of what was accomplished",
  "confidence": 0.9
}

Focus on:
1. Was the issue fully completed according to acceptance criteria?
2. What work was mentioned but not completed?
3. Were any new bugs, tasks, or improvements discovered?
4. Are there any quality issues (missing tests, poor code structure, etc.)?
5. What was actually accomplished?

Be thorough in identifying discovered work - this is how we prevent things from falling through the cracks.

Respond with ONLY the JSON object, no additional text.`,
		issue.ID, issue.Title, issue.Description, issue.AcceptanceCriteria,
		successStr, truncateString(agentOutput, 2000))
}

// logAIUsage logs AI API usage via comments
func (s *Supervisor) logAIUsage(ctx context.Context, issueID, activity string, inputTokens, outputTokens int64, duration time.Duration) error {
	comment := fmt.Sprintf("AI Usage (%s): input=%d tokens, output=%d tokens, duration=%v, model=%s",
		activity, inputTokens, outputTokens, duration, s.model)
	return s.store.AddComment(ctx, issueID, "ai-supervisor", comment)
}

// CreateDiscoveredIssues creates issues from the AI analysis
func (s *Supervisor) CreateDiscoveredIssues(ctx context.Context, parentIssue *types.Issue, discovered []DiscoveredIssue) ([]string, error) {
	var createdIDs []string

	for _, disc := range discovered {
		// Map string priority to int (0-3)
		priority := 2 // default P2
		switch disc.Priority {
		case "P0":
			priority = 0
		case "P1":
			priority = 1
		case "P2":
			priority = 2
		case "P3":
			priority = 3
		}

		// Map string type to types.IssueType
		issueType := types.TypeTask // default
		switch disc.Type {
		case "bug":
			issueType = types.TypeBug
		case "task":
			issueType = types.TypeTask
		case "feature", "enhancement":
			issueType = types.TypeFeature
		case "epic":
			issueType = types.TypeEpic
		case "chore":
			issueType = types.TypeChore
		}

		// Create the issue
		newIssue := &types.Issue{
			Title:       disc.Title,
			Description: disc.Description + fmt.Sprintf("\n\n_Discovered during execution of %s_", parentIssue.ID),
			IssueType:   issueType,
			Status:      types.StatusOpen,
			Priority:    priority,
			Assignee:    "ai-supervisor",
		}

		err := s.store.CreateIssue(ctx, newIssue, "ai-supervisor")
		if err != nil {
			return createdIDs, fmt.Errorf("failed to create discovered issue: %w", err)
		}

		// The ID is set on the issue by CreateIssue
		id := newIssue.ID

		createdIDs = append(createdIDs, id)
		fmt.Printf("Created discovered issue %s: %s\n", id, disc.Title)

		// Add a dependency: new issue was discovered from parent
		// This ensures discovered work doesn't get lost and is tracked properly
		dep := &types.Dependency{
			IssueID:     id,
			DependsOnID: parentIssue.ID,
			Type:        types.DepDiscoveredFrom,
		}
		if err := s.store.AddDependency(ctx, dep, "ai-supervisor"); err != nil {
			fmt.Fprintf(os.Stderr, "warning: failed to add dependency %s -> %s: %v\n", id, parentIssue.ID, err)
		}
	}

	return createdIDs, nil
}

// truncateString truncates a string to maxLen characters
func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[len(s)-maxLen:]
}
