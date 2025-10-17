package ai

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/steveyegge/vc/internal/storage"
	"github.com/steveyegge/vc/internal/types"
)

// Supervisor handles AI-powered assessment and analysis of issues
// It also implements the MissionPlanner interface for mission orchestration
type Supervisor struct {
	client         *anthropic.Client
	store          storage.Storage
	model          string
	retry          RetryConfig
	circuitBreaker *CircuitBreaker
}

// Compile-time check that Supervisor implements MissionPlanner
var _ types.MissionPlanner = (*Supervisor)(nil)

// RetryConfig holds retry configuration for API calls
type RetryConfig struct {
	MaxRetries        int           // Maximum number of retries (default: 3)
	InitialBackoff    time.Duration // Initial backoff duration (default: 1s)
	MaxBackoff        time.Duration // Maximum backoff duration (default: 30s)
	BackoffMultiplier float64       // Backoff multiplier (default: 2.0)
	Timeout           time.Duration // Per-request timeout (default: 60s)

	// Circuit breaker settings
	CircuitBreakerEnabled bool          // Enable circuit breaker (default: true)
	FailureThreshold      int           // Failures before opening circuit (default: 5)
	SuccessThreshold      int           // Successes in half-open before closing (default: 2)
	OpenTimeout           time.Duration // How long to keep circuit open (default: 30s)
}

// CircuitState represents the state of a circuit breaker
type CircuitState int

const (
	CircuitClosed CircuitState = iota // Normal operation, requests pass through
	CircuitOpen                        // Too many failures, block requests (fail fast)
	CircuitHalfOpen                    // Testing recovery, allow limited requests
)

func (s CircuitState) String() string {
	switch s {
	case CircuitClosed:
		return "CLOSED"
	case CircuitOpen:
		return "OPEN"
	case CircuitHalfOpen:
		return "HALF_OPEN"
	default:
		return "UNKNOWN"
	}
}

// CircuitBreaker implements the circuit breaker pattern to prevent cascading failures
type CircuitBreaker struct {
	mu sync.Mutex

	state            CircuitState
	failureCount     int
	successCount     int
	lastFailureTime  time.Time
	lastStateChange  time.Time
	failureThreshold int
	successThreshold int
	openTimeout      time.Duration
}

// ErrCircuitOpen is returned when the circuit breaker is open
var ErrCircuitOpen = errors.New("circuit breaker is open")

// DefaultRetryConfig returns the default retry configuration
func DefaultRetryConfig() RetryConfig {
	return RetryConfig{
		MaxRetries:            3,
		InitialBackoff:        1 * time.Second,
		MaxBackoff:            30 * time.Second,
		BackoffMultiplier:     2.0,
		Timeout:               60 * time.Second,
		CircuitBreakerEnabled: true,
		FailureThreshold:      5,
		SuccessThreshold:      2,
		OpenTimeout:           30 * time.Second,
	}
}

// NewCircuitBreaker creates a new circuit breaker with the given configuration
func NewCircuitBreaker(failureThreshold, successThreshold int, openTimeout time.Duration) *CircuitBreaker {
	return &CircuitBreaker{
		state:            CircuitClosed,
		failureThreshold: failureThreshold,
		successThreshold: successThreshold,
		openTimeout:      openTimeout,
		lastStateChange:  time.Now(),
	}
}

// Allow checks if a request should be allowed through the circuit breaker
// Returns an error if the circuit is open and hasn't timed out yet
func (cb *CircuitBreaker) Allow() error {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	switch cb.state {
	case CircuitClosed:
		// Normal operation, allow request
		return nil

	case CircuitOpen:
		// Check if we should transition to half-open
		if time.Since(cb.lastFailureTime) > cb.openTimeout {
			cb.transitionToHalfOpen()
			return nil
		}
		// Circuit is still open, fail fast
		return ErrCircuitOpen

	case CircuitHalfOpen:
		// In half-open state, allow one request through to probe
		return nil

	default:
		return ErrCircuitOpen
	}
}

// RecordSuccess records a successful request
func (cb *CircuitBreaker) RecordSuccess() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	switch cb.state {
	case CircuitClosed:
		// Reset failure count on success
		if cb.failureCount > 0 {
			cb.failureCount = 0
		}

	case CircuitHalfOpen:
		// Count successes in half-open state
		cb.successCount++
		if cb.successCount >= cb.successThreshold {
			cb.transitionToClosed()
		}
	}
}

// RecordFailure records a failed request
func (cb *CircuitBreaker) RecordFailure() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.lastFailureTime = time.Now()

	switch cb.state {
	case CircuitClosed:
		cb.failureCount++
		if cb.failureCount >= cb.failureThreshold {
			cb.transitionToOpen()
		}

	case CircuitHalfOpen:
		// Any failure in half-open immediately opens the circuit
		cb.transitionToOpen()
	}
}

// GetState returns the current state (for testing/monitoring)
func (cb *CircuitBreaker) GetState() CircuitState {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	return cb.state
}

// GetMetrics returns current metrics (for monitoring/logging)
func (cb *CircuitBreaker) GetMetrics() (state CircuitState, failures, successes int) {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	return cb.state, cb.failureCount, cb.successCount
}

// transitionToClosed moves the circuit to closed state (must be called with lock held)
func (cb *CircuitBreaker) transitionToClosed() {
	oldState := cb.state
	cb.state = CircuitClosed
	cb.failureCount = 0
	cb.successCount = 0
	cb.lastStateChange = time.Now()
	fmt.Printf("Circuit breaker state transition: %s → %s (failures reset)\n", oldState, cb.state)
}

// transitionToOpen moves the circuit to open state (must be called with lock held)
func (cb *CircuitBreaker) transitionToOpen() {
	oldState := cb.state
	cb.state = CircuitOpen
	cb.successCount = 0
	cb.lastStateChange = time.Now()
	fmt.Printf("Circuit breaker state transition: %s → %s (failures=%d, will reopen in %v)\n",
		oldState, cb.state, cb.failureCount, cb.openTimeout)
}

// transitionToHalfOpen moves the circuit to half-open state (must be called with lock held)
func (cb *CircuitBreaker) transitionToHalfOpen() {
	oldState := cb.state
	cb.state = CircuitHalfOpen
	cb.successCount = 0
	cb.lastStateChange = time.Now()
	fmt.Printf("Circuit breaker state transition: %s → %s (probing for recovery)\n", oldState, cb.state)
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

	// Initialize circuit breaker if enabled
	var circuitBreaker *CircuitBreaker
	if retry.CircuitBreakerEnabled {
		circuitBreaker = NewCircuitBreaker(
			retry.FailureThreshold,
			retry.SuccessThreshold,
			retry.OpenTimeout,
		)
		fmt.Printf("Circuit breaker initialized: threshold=%d failures, recovery=%d successes, timeout=%v\n",
			retry.FailureThreshold, retry.SuccessThreshold, retry.OpenTimeout)
	}

	return &Supervisor{
		client:         &client,
		store:          cfg.Store,
		model:          model,
		retry:          retry,
		circuitBreaker: circuitBreaker,
	}, nil
}

// retryWithBackoff executes an operation with exponential backoff retry logic
// and circuit breaker protection
func (s *Supervisor) retryWithBackoff(ctx context.Context, operation string, fn func(context.Context) error) error {
	var lastErr error
	backoff := s.retry.InitialBackoff

	for attempt := 0; attempt <= s.retry.MaxRetries; attempt++ {
		// Check circuit breaker before attempting request
		if s.circuitBreaker != nil {
			if err := s.circuitBreaker.Allow(); err != nil {
				// Circuit is open, fail fast without retrying
				state, failures, _ := s.circuitBreaker.GetMetrics()
				fmt.Fprintf(os.Stderr, "AI API %s blocked by circuit breaker (state=%s, failures=%d)\n",
					operation, state, failures)
				return fmt.Errorf("%s failed: %w", operation, err)
			}
		}

		// Create timeout context for this attempt
		attemptCtx, cancel := context.WithTimeout(ctx, s.retry.Timeout)

		// Execute the operation
		err := fn(attemptCtx)
		cancel()

		// Success!
		if err == nil {
			// Record success with circuit breaker
			if s.circuitBreaker != nil {
				s.circuitBreaker.RecordSuccess()
			}

			if attempt > 0 {
				fmt.Printf("AI API %s succeeded after %d retries\n", operation, attempt)
			}
			return nil
		}

		lastErr = err

		// Record failure with circuit breaker if it's a retriable error
		// Non-retriable errors (like auth failures) shouldn't count against circuit breaker
		if s.circuitBreaker != nil && isRetriableError(err) {
			s.circuitBreaker.RecordFailure()
		}

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

// CompletionAssessment represents AI assessment of whether an epic/mission is complete
type CompletionAssessment struct {
	ShouldClose bool     `json:"should_close"` // Should this epic/mission be closed?
	Reasoning   string   `json:"reasoning"`    // Detailed reasoning for the decision
	Confidence  float64  `json:"confidence"`   // Confidence in the assessment (0.0-1.0)
	Caveats     []string `json:"caveats"`      // Any caveats or concerns
}

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

// CodeReviewDecision represents AI decision about whether code review is needed
type CodeReviewDecision struct {
	NeedsReview bool    `json:"needs_review"` // Should this code be reviewed?
	Reasoning   string  `json:"reasoning"`    // Detailed reasoning for the decision
	Confidence  float64 `json:"confidence"`   // Confidence in the assessment (0.0-1.0)
}

// CodeQualityAnalysis represents automated code quality review findings (vc-79)
// This replaces manual code review with AI-driven analysis that automatically files fix issues
type CodeQualityAnalysis struct {
	Issues     []DiscoveredIssue `json:"issues"`      // Specific fix issues to create
	Summary    string            `json:"summary"`     // Overall code quality assessment
	Confidence float64           `json:"confidence"`  // Confidence in the analysis (0.0-1.0)
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

// AssessCompletion uses AI to determine if an epic or mission is truly complete.
// This replaces the hardcoded "all children closed = complete" heuristic with AI decision-making.
//
// The AI considers:
// - Whether the epic/mission objectives are met
// - Child issue statuses (but doesn't just count them)
// - Any blocked or punted work
// - Overall goal achievement vs. completeness
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
		LogErrors: true,
	})
	if !parseResult.Success {
		return nil, fmt.Errorf("failed to parse completion assessment response: %s (response: %s)", parseResult.Error, responseText)
	}
	assessment := parseResult.Data

	// Log the assessment
	duration := time.Since(startTime)
	fmt.Printf("AI Completion Assessment for %s: should_close=%v, confidence=%.2f, duration=%v\n",
		issue.ID, assessment.ShouldClose, assessment.Confidence, duration)

	// Log AI usage to events
	if err := s.logAIUsage(ctx, issue.ID, "completion-assessment", response.Usage.InputTokens, response.Usage.OutputTokens, duration); err != nil {
		fmt.Fprintf(os.Stderr, "warning: failed to log AI usage: %v\n", err)
	}

	return &assessment, nil
}

// GenerateRecoveryStrategy uses AI to determine how to recover from quality gate failures.
// This replaces the hardcoded "mark as blocked" logic with AI decision-making.
//
// The AI considers:
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
		LogErrors: true,
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

// AnalyzeCodeReviewNeed uses Haiku to decide if code review is warranted.
// This replaces arbitrary heuristics (line counts, file counts) with AI decision-making (ZFC compliance).
//
// The AI analyzes the git diff considering:
// - Complexity and risk of changes
// - Critical paths touched (auth, security, data integrity)
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
			MaxTokens: 1024, // Short decision
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
		LogErrors: true,
	})
	if !parseResult.Success {
		return nil, fmt.Errorf("failed to parse code review decision response: %s (response: %s)", parseResult.Error, responseText)
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

// GateFailure represents a failed quality gate with details
type GateFailure struct {
	Gate   string // Gate type: "test", "lint", "build"
	Output string // Truncated output from the gate
	Error  string // Error message
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

IMPORTANT: Respond with ONLY raw JSON. Do NOT wrap it in markdown code fences (` + "```" + `). Just the JSON object.`,
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

IMPORTANT: Respond with ONLY raw JSON. Do NOT wrap it in markdown code fences (` + "```" + `). Just the JSON object.`,
		issue.ID, issue.Title, issue.Description, issue.AcceptanceCriteria,
		successStr, truncateString(agentOutput, 2000))
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
	issueTypeStr := "epic"
	if issue.IssueSubtype == types.SubtypeMission {
		issueTypeStr = "mission"
	} else if issue.IssueSubtype == types.SubtypePhase {
		issueTypeStr = "phase"
	}

	return fmt.Sprintf(`You are assessing whether an %s is truly complete and should be closed.

IMPORTANT: Don't just count closed children. Consider whether the OBJECTIVES are met.

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

IMPORTANT: Respond with ONLY raw JSON. Do NOT wrap it in markdown code fences (` + "```" + `). Just the JSON object.`,
		issueTypeStr,
		strings.ToUpper(issueTypeStr),
		issue.ID, issue.Title, issue.Description,
		issue.AcceptanceCriteria,
		len(children), closedCount, openCount, blockedCount,
		childSummary.String(),
		issueTypeStr)
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
2. "acceptable_failure" - Close anyway if failures are non-critical (requires approval)
3. "split_work" - Create separate issues for fixes, close original
4. "escalate" - Flag for human review and decision
5. "retry" - Suggest retry (for flaky tests/transient failures)

DECISION CRITERIA:
- Issue priority and type
- Severity of failures
- Whether failures are in the core work or incidental
- Cost/benefit of fixing vs accepting

Examples:
- Flaky test failures → retry or acceptable_failure
- Critical bug in P0 issue → fix_in_place
- Lint warnings in chore task → acceptable_failure
- Build failures → fix_in_place
- Test failures for new features → fix_in_place

Provide your strategy as a JSON object:
{
  "action": "fix_in_place|acceptable_failure|split_work|escalate|retry",
  "reasoning": "Detailed explanation of why this action is recommended",
  "confidence": 0.85,
  "create_issues": [
    {
      "title": "Fix test X",
      "description": "Details of what needs to be fixed",
      "type": "bug|task",
      "priority": "P0|P1|P2|P3"
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

IMPORTANT: Respond with ONLY raw JSON. Do NOT wrap it in markdown code fences (` + "```" + `). Just the JSON object.`,
		issue.ID, issue.Title, issue.IssueType, issue.Priority,
		issue.Description,
		len(gateResults),
		failureSummary.String())
}

// buildCodeReviewPrompt builds the prompt for deciding if code review is needed
func (s *Supervisor) buildCodeReviewPrompt(issue *types.Issue, gitDiff string) string {
	// Truncate diff if it's too large (keep it under 30k chars for fast processing)
	diffToAnalyze := gitDiff
	wasTruncated := false
	const maxDiffSize = 30000
	if len(gitDiff) > maxDiffSize {
		diffToAnalyze = gitDiff[:maxDiffSize] + "\n\n... [diff truncated - remaining content omitted for brevity] ..."
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

IMPORTANT: Respond with ONLY raw JSON. Do NOT wrap it in markdown code fences (` + "```" + `). Just the JSON object.`,
		issue.ID, issue.Title, issue.IssueType, issue.Priority,
		issue.Description,
		diffToAnalyze,
		truncationNote)
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

// CallAI makes a generic AI API call with the given prompt
// This provides a generic interface for other components (like watchdog) to use AI
// without duplicating retry logic and circuit breaker code
func (s *Supervisor) CallAI(ctx context.Context, prompt string, operation string, model string, maxTokens int) (string, error) {
	startTime := time.Now()
	var responseText string

	// Use default model if not specified
	if model == "" {
		model = s.model
	}

	// Use default maxTokens if not specified
	if maxTokens == 0 {
		maxTokens = 4096
	}

	// Call Anthropic API with retry logic
	var response *anthropic.Message
	err := s.retryWithBackoff(ctx, operation, func(attemptCtx context.Context) error {
		resp, apiErr := s.client.Messages.New(attemptCtx, anthropic.MessageNewParams{
			Model:     anthropic.Model(model),
			MaxTokens: int64(maxTokens),
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
		return "", fmt.Errorf("anthropic API call failed: %w", err)
	}

	// Extract the text content from the response
	for _, block := range response.Content {
		if block.Type == "text" {
			responseText += block.Text
		}
	}

	// Log the call
	duration := time.Since(startTime)
	fmt.Printf("AI %s call: input=%d tokens, output=%d tokens, duration=%v\n",
		operation, response.Usage.InputTokens, response.Usage.OutputTokens, duration)

	return responseText, nil
}

// SummarizeAgentOutput uses AI to create an intelligent summary of agent output
// instead of using a simple "last N lines" heuristic.
//
// This method:
// - Sends the full output to AI with context about the issue
// - AI extracts: what was done, key decisions, important warnings
// - Returns a concise summary suitable for comments/notifications
// - Handles various output formats (test results, build logs, etc.)
func (s *Supervisor) SummarizeAgentOutput(ctx context.Context, issue *types.Issue, fullOutput string, maxLength int) (string, error) {
	startTime := time.Now()

	// Handle empty output
	if len(fullOutput) == 0 {
		return "Agent completed with no output", nil
	}

	// For very short output, just return it directly
	if len(fullOutput) <= maxLength {
		return fullOutput, nil
	}

	// Build the summarization prompt
	prompt := s.buildSummarizationPrompt(issue, fullOutput, maxLength)

	// Call Anthropic API with retry logic
	var response *anthropic.Message
	err := s.retryWithBackoff(ctx, "summarization", func(attemptCtx context.Context) error {
		resp, apiErr := s.client.Messages.New(attemptCtx, anthropic.MessageNewParams{
			Model:     anthropic.Model(s.model),
			MaxTokens: 2048, // Summaries should be concise
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
		// Don't fall back to heuristics - return the error (ZFC compliance)
		return "", fmt.Errorf("AI summarization failed after %d retry attempts: %w", s.retry.MaxRetries+1, err)
	}

	// Extract the text content from the response
	var summary strings.Builder
	for _, block := range response.Content {
		if block.Type == "text" {
			summary.WriteString(block.Text)
		}
	}

	summaryText := summary.String()

	// Log the summarization
	duration := time.Since(startTime)
	fmt.Printf("AI Summarization: input=%d chars, output=%d chars, duration=%v\n",
		len(fullOutput), len(summaryText), duration)

	// Log AI usage to events
	if err := s.logAIUsage(ctx, issue.ID, "summarization", response.Usage.InputTokens, response.Usage.OutputTokens, duration); err != nil {
		fmt.Fprintf(os.Stderr, "warning: failed to log AI usage: %v\n", err)
	}

	return summaryText, nil
}

// buildSummarizationPrompt builds the prompt for summarizing agent output
func (s *Supervisor) buildSummarizationPrompt(issue *types.Issue, fullOutput string, maxLength int) string {
	// Intelligently sample the output if it's very large
	// Send beginning + end for context, with indication of truncation
	outputToAnalyze := fullOutput
	wasTruncated := false

	// If output is enormous (>50k chars), sample it intelligently
	const maxPromptOutput = 50000
	if len(fullOutput) > maxPromptOutput {
		// Take first 20k and last 30k for context
		outputToAnalyze = fullOutput[:20000] + "\n\n... [truncated middle section] ...\n\n" + fullOutput[len(fullOutput)-30000:]
		wasTruncated = true
	}

	truncationNote := ""
	if wasTruncated {
		truncationNote = "\n\nNote: The full output was very large and has been sampled. Focus on extracting the most important information from what's provided."
	}

	return fmt.Sprintf(`You are summarizing the output from a coding agent that just worked on an issue. Extract the key information into a concise summary.

Issue Context:
Issue ID: %s
Title: %s
Description: %s

Agent Output (may be truncated):
%s%s

Please provide a concise summary (max %d characters) that captures:
1. What was actually done/accomplished
2. Key decisions or changes made
3. Important warnings, errors, or issues encountered
4. Test results (if any)
5. Next steps mentioned (if any)

Format the summary as plain text, suitable for adding as a comment. Be specific about concrete actions taken, not just "the agent worked on X". Include actual file names, test names, command outputs, etc.

Focus on information that would be useful to someone reviewing this work later. Skip boilerplate or irrelevant output.`,
		issue.ID, issue.Title, issue.Description,
		outputToAnalyze,
		truncationNote,
		maxLength)
}


// GeneratePlan generates a phased implementation plan for a mission
// This is the core of the middle loop: breaking high-level missions into executable phases
func (s *Supervisor) GeneratePlan(ctx context.Context, planningCtx *types.PlanningContext) (*types.MissionPlan, error) {
	// Add overall timeout to prevent indefinite retries
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()

	startTime := time.Now()

	// Validate input
	if err := planningCtx.Validate(); err != nil {
		return nil, fmt.Errorf("invalid planning context: %w", err)
	}

	// Build the planning prompt
	prompt := s.buildPlanningPrompt(planningCtx)

	// Call Anthropic API with retry logic
	var response *anthropic.Message
	err := s.retryWithBackoff(ctx, "planning", func(attemptCtx context.Context) error {
		resp, apiErr := s.client.Messages.New(attemptCtx, anthropic.MessageNewParams{
			Model:     anthropic.Model(s.model),
			MaxTokens: 8192, // Larger token limit for complex plans
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
	parseResult := Parse[types.MissionPlan](responseText, ParseOptions{
		Context:   "mission plan response",
		LogErrors: true,
	})
	if !parseResult.Success {
		// Log full response for debugging, but truncate in error message
		fmt.Fprintf(os.Stderr, "Full AI planning response: %s\n", responseText)
		return nil, fmt.Errorf("failed to parse mission plan response: %s (response: %s)", parseResult.Error, truncateString(responseText, 500))
	}
	plan := parseResult.Data

	// Set metadata
	plan.MissionID = planningCtx.Mission.ID
	plan.GeneratedAt = time.Now()
	plan.GeneratedBy = "ai-planner"

	// Validate the generated plan
	if err := s.ValidatePlan(ctx, &plan); err != nil {
		return nil, fmt.Errorf("generated plan failed validation: %w", err)
	}

	// Log the plan generation
	duration := time.Since(startTime)
	fmt.Printf("AI Planning for %s: phases=%d, confidence=%.2f, effort=%s, duration=%v\n",
		planningCtx.Mission.ID, len(plan.Phases), plan.Confidence, plan.EstimatedEffort, duration)

	// Log AI usage to events
	if err := s.logAIUsage(ctx, planningCtx.Mission.ID, "planning", response.Usage.InputTokens, response.Usage.OutputTokens, duration); err != nil {
		fmt.Fprintf(os.Stderr, "warning: failed to log AI usage: %v\n", err)
	}

	return &plan, nil
}

// RefinePhase breaks a phase down into granular tasks
// This is called when a phase is ready to execute
func (s *Supervisor) RefinePhase(ctx context.Context, phase *types.Phase, missionCtx *types.PlanningContext) ([]types.PlannedTask, error) {
	// Add overall timeout to prevent indefinite retries
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()

	startTime := time.Now()

	// Validate inputs
	if phase == nil {
		return nil, fmt.Errorf("phase is required")
	}
	if err := phase.Validate(); err != nil {
		return nil, fmt.Errorf("invalid phase: %w", err)
	}
	if missionCtx != nil {
		if err := missionCtx.Validate(); err != nil {
			return nil, fmt.Errorf("invalid mission context: %w", err)
		}
	}

	// Build the refinement prompt
	prompt := s.buildRefinementPrompt(phase, missionCtx)

	// Call Anthropic API with retry logic
	var response *anthropic.Message
	err := s.retryWithBackoff(ctx, "refinement", func(attemptCtx context.Context) error {
		resp, apiErr := s.client.Messages.New(attemptCtx, anthropic.MessageNewParams{
			Model:     anthropic.Model(s.model),
			MaxTokens: 8192,
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

	// Parse the response - expecting {"tasks": [...]}
	type refinementResponse struct {
		Tasks []types.PlannedTask `json:"tasks"`
	}
	parseResult := Parse[refinementResponse](responseText, ParseOptions{
		Context:   "phase refinement response",
		LogErrors: true,
	})
	if !parseResult.Success {
		// Log full response for debugging, but truncate in error message
		fmt.Fprintf(os.Stderr, "Full AI refinement response: %s\n", responseText)
		return nil, fmt.Errorf("failed to parse refinement response: %s (response: %s)", parseResult.Error, truncateString(responseText, 500))
	}
	tasks := parseResult.Data.Tasks

	// Validate tasks
	if len(tasks) == 0 {
		return nil, fmt.Errorf("refinement produced no tasks")
	}
	for i, task := range tasks {
		if err := task.Validate(); err != nil {
			return nil, fmt.Errorf("task %d invalid: %w", i+1, err)
		}
	}

	// Log the refinement
	duration := time.Since(startTime)
	fmt.Printf("AI Refinement for phase %s: tasks=%d, duration=%v\n",
		phase.ID, len(tasks), duration)

	// Log AI usage
	if err := s.logAIUsage(ctx, phase.ID, "refinement", response.Usage.InputTokens, response.Usage.OutputTokens, duration); err != nil {
		fmt.Fprintf(os.Stderr, "warning: failed to log AI usage: %v\n", err)
	}

	return tasks, nil
}

// ValidatePlan checks if a generated plan is valid and executable
func (s *Supervisor) ValidatePlan(ctx context.Context, plan *types.MissionPlan) error {
	// Basic validation already done by types.MissionPlan.Validate()
	if err := plan.Validate(); err != nil {
		return err
	}

	// Additional validation rules
	phaseCount := len(plan.Phases)
	if phaseCount < 1 {
		return fmt.Errorf("plan must have at least 1 phase (got %d)", phaseCount)
	}
	if phaseCount > 15 {
		return fmt.Errorf("plan has too many phases (%d); consider breaking into multiple missions", phaseCount)
	}

	// Check for circular dependencies
	if err := checkCircularDependencies(plan.Phases); err != nil {
		return fmt.Errorf("circular dependencies detected: %w", err)
	}

	// Validate each phase has reasonable task count
	for i, phase := range plan.Phases {
		taskCount := len(phase.Tasks)
		if taskCount == 0 {
			return fmt.Errorf("phase %d (%s) has no tasks", i+1, phase.Title)
		}
		if taskCount > 50 {
			return fmt.Errorf("phase %d (%s) has too many tasks (%d); break it down further", i+1, phase.Title, taskCount)
		}
	}

	return nil
}

// ValidatePhaseStructure validates phase dependencies and ordering using AI
// This replaces hardcoded validation rules (like "phases can only depend on earlier phases")
// with AI-driven validation that can be more flexible and context-aware
func (s *Supervisor) ValidatePhaseStructure(ctx context.Context, phases []types.PlannedPhase) error {
	startTime := time.Now()

	// For very simple cases (single phase, no dependencies), skip AI validation
	if len(phases) == 1 {
		return nil
	}

	// Build validation prompt
	prompt := s.buildPhaseValidationPrompt(phases)

	// Call Anthropic API with retry logic
	var response *anthropic.Message
	err := s.retryWithBackoff(ctx, "phase-validation", func(attemptCtx context.Context) error {
		resp, apiErr := s.client.Messages.New(attemptCtx, anthropic.MessageNewParams{
			Model:     anthropic.Model(s.model),
			MaxTokens: 2048,
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
		return fmt.Errorf("anthropic API call failed: %w", err)
	}

	// Extract the text content from the response
	var responseText string
	for _, block := range response.Content {
		if block.Type == "text" {
			responseText += block.Text
		}
	}

	// Parse the response
	type validationResult struct {
		Valid     bool     `json:"valid"`
		Errors    []string `json:"errors"`
		Warnings  []string `json:"warnings"`
		Reasoning string   `json:"reasoning"`
	}

	parseResult := Parse[validationResult](responseText, ParseOptions{
		Context:   "phase validation response",
		LogErrors: true,
	})
	if !parseResult.Success {
		return fmt.Errorf("failed to parse phase validation response: %s (response: %s)", parseResult.Error, responseText)
	}
	result := parseResult.Data

	// Log the validation
	duration := time.Since(startTime)
	fmt.Printf("AI Phase Validation: valid=%v, errors=%d, warnings=%d, duration=%v\n",
		result.Valid, len(result.Errors), len(result.Warnings), duration)

	// Log AI usage (use a dummy issue ID for now since we don't have one in this context)
	if err := s.logAIUsage(ctx, "phase-validation", "phase-validation", response.Usage.InputTokens, response.Usage.OutputTokens, duration); err != nil {
		fmt.Fprintf(os.Stderr, "warning: failed to log AI usage: %v\n", err)
	}

	// If invalid, return the errors
	if !result.Valid {
		return fmt.Errorf("phase structure validation failed: %s (errors: %v)", result.Reasoning, result.Errors)
	}

	// Log warnings if any
	for _, warning := range result.Warnings {
		fmt.Printf("Phase validation warning: %s\n", warning)
	}

	return nil
}

// buildPhaseValidationPrompt builds the prompt for validating phase structure
func (s *Supervisor) buildPhaseValidationPrompt(phases []types.PlannedPhase) string {
	// Build phase summary
	var phaseSummary strings.Builder
	for _, phase := range phases {
		phaseSummary.WriteString(fmt.Sprintf("\nPhase %d: %s\n", phase.PhaseNumber, phase.Title))
		phaseSummary.WriteString(fmt.Sprintf("  Description: %s\n", phase.Description))
		phaseSummary.WriteString(fmt.Sprintf("  Dependencies: %v\n", phase.Dependencies))
		phaseSummary.WriteString(fmt.Sprintf("  Estimated Effort: %s\n", phase.EstimatedEffort))
	}

	return fmt.Sprintf(`You are validating the structure and dependencies of a multi-phase implementation plan.

PHASES TO VALIDATE:
%s

VALIDATION TASK:
Check if this phase structure makes logical sense. Consider:

1. **Dependency Validity**: Are dependencies sensible?
   - Typically phases depend on earlier phases, but forward dependencies MAY be valid in special cases
   - Example: Phase 3 depending on Phase 5 might be valid if Phase 5 is foundational infrastructure

2. **Circular Dependencies**: Are there any circular dependency chains?
   - Phase A → Phase B → Phase A is always invalid

3. **Missing Dependencies**: Are there obvious missing dependencies?
   - If Phase 3 builds on Phase 2's work, it should depend on Phase 2

4. **Logical Ordering**: Does the phase sequence make sense?
   - Foundation before features
   - Core before polish
   - Setup before execution

IMPORTANT: Be flexible. Not all plans follow strict "earlier phases only" rules. Consider the context.

Provide your validation as a JSON object:
{
  "valid": true/false,
  "errors": ["Critical error 1", "Critical error 2"],
  "warnings": ["Concern 1", "Concern 2"],
  "reasoning": "Detailed explanation of the assessment"
}

Guidelines:
- errors: Critical issues that MUST be fixed (invalid structure, circular deps)
- warnings: Concerns that should be reviewed but might be intentional
- reasoning: Explain your assessment clearly
- Be pragmatic: unusual structures might be valid if there's good reason

IMPORTANT: Respond with ONLY raw JSON. Do NOT wrap it in markdown code fences (` + "```" + `). Just the JSON object.`,
		phaseSummary.String())
}

// buildPlanningPrompt builds the prompt for generating a mission plan
func (s *Supervisor) buildPlanningPrompt(ctx *types.PlanningContext) string {
	mission := ctx.Mission

	// Build constraints section
	constraintsSection := ""
	if len(ctx.Constraints) > 0 {
		constraintsSection = "\n\nConstraints:\n"
		for _, constraint := range ctx.Constraints {
			constraintsSection += fmt.Sprintf("- %s\n", constraint)
		}
	}

	// Build context section
	contextSection := ""
	if ctx.CodebaseInfo != "" {
		contextSection = fmt.Sprintf("\n\nCodebase Context:\n%s", ctx.CodebaseInfo)
	}

	// Build failed attempts section
	failedAttemptsSection := ""
	if ctx.FailedAttempts > 0 {
		failedAttemptsSection = fmt.Sprintf("\n\nNote: This is attempt %d at planning. Previous plans had issues. Please try a different approach.", ctx.FailedAttempts+1)
	}

	return fmt.Sprintf(`You are an AI mission planner helping break down a large software development mission into executable phases.

MISSION OVERVIEW:
Mission ID: %s
Title: %s
Goal: %s

Description:
%s

Context:
%s%s%s%s

THREE-TIER WORKFLOW:
This system uses a three-tier workflow:
1. OUTER LOOP (Mission): High-level goal (what you're planning now)
2. MIDDLE LOOP (Phases): Implementation stages (what you'll generate)
3. INNER LOOP (Tasks): Granular work items (generated later when each phase executes)

YOUR TASK:
Generate a phased implementation plan. Each phase should be:
- A major milestone that takes 1-2 weeks to complete
- Focused on a specific aspect or stage of the work
- Independently valuable (produces working functionality)
- Ordered logically with clear dependencies

GENERATE A JSON PLAN WITH THIS STRUCTURE:
{
  "phases": [
    {
      "phase_number": 1,
      "title": "Phase 1: Foundation",
      "description": "Detailed description of what this phase accomplishes",
      "strategy": "High-level approach for this phase",
      "tasks": [
        "High-level task 1 (will be refined later into granular tasks)",
        "High-level task 2",
        "High-level task 3"
      ],
      "dependencies": [],
      "estimated_effort": "1 week"
    },
    {
      "phase_number": 2,
      "title": "Phase 2: Core Features",
      "description": "...",
      "strategy": "...",
      "tasks": ["..."],
      "dependencies": [1],
      "estimated_effort": "2 weeks"
    }
  ],
  "strategy": "Overall implementation strategy across all phases",
  "risks": [
    "Potential risk or challenge 1",
    "Potential risk or challenge 2"
  ],
  "estimated_effort": "6 weeks",
  "confidence": 0.85
}

IMPORTANT GUIDELINES:
- Generate 2-10 phases (prefer fewer, larger phases over many tiny ones)
- Phase numbers start at 1 and must be sequential
- Dependencies array contains phase numbers (must be earlier phases only)
- Each phase should have 3-8 high-level tasks
- Tasks are high-level descriptions, NOT granular implementation steps
- Estimated effort should be realistic: "3 days", "1 week", "2 weeks"
- Confidence should reflect uncertainty (0.0-1.0)
- Consider technical dependencies, logical ordering, and risk

IMPORTANT: Respond with ONLY raw JSON. Do NOT wrap it in markdown code fences (` + "```" + `). Just the JSON object.`,
		mission.ID, mission.Title, mission.Goal,
		mission.Description,
		mission.Context,
		contextSection,
		constraintsSection,
		failedAttemptsSection)
}

// buildRefinementPrompt builds the prompt for refining a phase into tasks
func (s *Supervisor) buildRefinementPrompt(phase *types.Phase, missionCtx *types.PlanningContext) string {
	// Build mission context if available
	missionSection := ""
	if missionCtx != nil && missionCtx.Mission != nil {
		missionSection = fmt.Sprintf(`
MISSION CONTEXT:
Mission: %s
Goal: %s
`, missionCtx.Mission.Title, missionCtx.Mission.Goal)
	}

	return fmt.Sprintf(`You are refining a phase of a software development mission into granular, executable tasks.

%s
PHASE TO REFINE:
Phase: %s
Strategy: %s

Description:
%s

YOUR TASK:
Break this phase down into 5-20 granular tasks. Each task should be:
- Small enough to complete in 30 minutes to 2 hours
- Concrete and actionable (not vague)
- Testable with clear acceptance criteria
- Ordered logically

GENERATE A JSON RESPONSE WITH THIS STRUCTURE:
{
  "tasks": [
    {
      "title": "Implement X data structure",
      "description": "Detailed description of what needs to be done",
      "acceptance_criteria": "Specific criteria for completion",
      "dependencies": [],
      "estimated_minutes": 60,
      "priority": 0,
      "type": "task"
    },
    {
      "title": "Add unit tests for X",
      "description": "...",
      "acceptance_criteria": "All tests pass, coverage > 80%%",
      "dependencies": ["Implement X data structure"],
      "estimated_minutes": 45,
      "priority": 1,
      "type": "task"
    }
  ]
}

GUIDELINES:
- Dependencies array contains task TITLES (not IDs) of tasks in this same list
- Priority: 0=P0 (critical), 1=P1 (high), 2=P2 (medium), 3=P3 (low)
- Type: "task", "bug", "feature", "chore"
- Estimated minutes should be realistic (15-120 minutes typical)
- Acceptance criteria should be specific and measurable
- Include tests as separate tasks
- Order tasks logically (dependencies should reference earlier tasks)

IMPORTANT: Respond with ONLY raw JSON. Do NOT wrap it in markdown code fences (` + "```" + `). Just the JSON object.`,
		missionSection,
		phase.Title,
		phase.Strategy,
		phase.Description)
}

// checkCircularDependencies detects circular dependencies in phases
func checkCircularDependencies(phases []types.PlannedPhase) error {
	// Build adjacency list
	graph := make(map[int][]int)
	for _, phase := range phases {
		graph[phase.PhaseNumber] = phase.Dependencies
	}

	// Check each phase for circular dependencies using DFS
	for _, phase := range phases {
		visited := make(map[int]bool)
		if hasCycle(graph, phase.PhaseNumber, visited, make(map[int]bool)) {
			return fmt.Errorf("phase %d (%s) has circular dependencies", phase.PhaseNumber, phase.Title)
		}
	}

	return nil
}

// hasCycle performs DFS to detect cycles
func hasCycle(graph map[int][]int, node int, visited, recStack map[int]bool) bool {
	visited[node] = true
	recStack[node] = true

	for _, neighbor := range graph[node] {
		if !visited[neighbor] {
			if hasCycle(graph, neighbor, visited, recStack) {
				return true
			}
		} else if recStack[neighbor] {
			return true
		}
	}

	recStack[node] = false
	return false
}
