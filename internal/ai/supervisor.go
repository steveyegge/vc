package ai

import (
	"context"
	"fmt"
	"os"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/steveyegge/vc/internal/storage"
	"github.com/steveyegge/vc/internal/types"
	"golang.org/x/sync/semaphore"
)

// AI Model Constants (vc-35: Tiered AI Model Strategy)
//
// VC uses a tiered approach to AI model selection based on task complexity:
// - Sonnet 4.5: Complex tasks requiring deep reasoning (assessment, analysis, planning)
// - Haiku: Simple tasks like file size checks, commit messages, cruft detection
//
// Cost savings: Haiku is ~80% cheaper than Sonnet for simple operations.
//
// Environment variable overrides (vc-lf8j Phase 2):
// - VC_MODEL_DEFAULT: Override default model (default: Sonnet)
// - VC_MODEL_SIMPLE: Override model for simple tasks (default: Haiku)
const (
	// ModelSonnet is the high-end model for complex reasoning tasks
	ModelSonnet = "claude-sonnet-4-5-20250929"

	// ModelHaiku is the cost-efficient model for simple tasks
	ModelHaiku = "claude-3-5-haiku-20241022"
)

// GetDefaultModel returns the default model, checking VC_MODEL_DEFAULT env var first
func GetDefaultModel() string {
	if model := os.Getenv("VC_MODEL_DEFAULT"); model != "" {
		return model
	}
	return ModelSonnet
}

// GetSimpleTaskModel returns the model for simple tasks, checking VC_MODEL_SIMPLE env var first
func GetSimpleTaskModel() string {
	if model := os.Getenv("VC_MODEL_SIMPLE"); model != "" {
		return model
	}
	return ModelHaiku
}

// Supervisor handles AI-powered assessment and analysis of issues
// It also implements the MissionPlanner interface for mission orchestration
//
// The Supervisor's responsibilities are distributed across multiple files:
// - supervisor.go: Core struct and constructor (this file)
// - retry.go: Circuit breaker and retry logic
// - assessment.go: Pre-execution assessment and completion assessment
// - analysis.go: Post-execution analysis
// - recovery.go: Quality gate failure recovery strategies
// - code_review.go: Code quality and test coverage analysis
// - deduplication.go: Duplicate issue detection
// - translation.go: Discovered issue creation
// - planning.go: Mission planning and phase refinement
// - utils.go: Shared utilities (logging, summarization, truncation)
type Supervisor struct {
	client         *anthropic.Client
	store          storage.Storage
	model          string
	retry          RetryConfig
	circuitBreaker *CircuitBreaker
	concurrencySem *semaphore.Weighted // Limits concurrent AI API calls (vc-220)
	costTracker    CostTracker         // Tracks AI costs and enforces budgets (vc-e3s7)
}

// Compile-time check that Supervisor implements MissionPlanner
var _ types.MissionPlanner = (*Supervisor)(nil)

// CostTracker defines the interface for cost budgeting (vc-e3s7)
// This allows dependency injection and testing without circular imports
type CostTracker interface {
	// RecordUsage records token usage for an issue
	// Returns budget status (as interface{} to avoid circular dependencies) and error
	RecordUsage(ctx context.Context, issueID string, inputTokens, outputTokens int64) (interface{}, error)
	// CanProceed checks if we can make another AI call within budget
	CanProceed(issueID string) (bool, string)
}

// Config holds supervisor configuration
type Config struct {
	APIKey      string       // Anthropic API key (if empty, reads from ANTHROPIC_API_KEY env var)
	Model       string       // Model to use (default: claude-sonnet-4-5-20250929)
	Store       storage.Storage
	Retry       RetryConfig  // Retry configuration (uses defaults if not specified)
	CostTracker CostTracker  // Optional cost tracker for budget enforcement (vc-e3s7)
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
		model = GetDefaultModel()
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

	// Initialize concurrency limiter (vc-220)
	var concurrencySem *semaphore.Weighted
	if retry.MaxConcurrentCalls > 0 {
		concurrencySem = semaphore.NewWeighted(int64(retry.MaxConcurrentCalls))
		fmt.Printf("AI concurrency limiter initialized: max_concurrent=%d calls\n", retry.MaxConcurrentCalls)
	}

	return &Supervisor{
		client:         &client,
		store:          cfg.Store,
		model:          model,
		retry:          retry,
		circuitBreaker: circuitBreaker,
		concurrencySem: concurrencySem,
		costTracker:    cfg.CostTracker, // Optional cost tracker (vc-e3s7)
	}, nil
}

// HealthCheck performs a pre-flight check of the supervisor's health
// Returns an error if the circuit breaker is open or if there are API connectivity issues
func (s *Supervisor) HealthCheck(ctx context.Context) error {
	// Check circuit breaker state
	if s.circuitBreaker != nil {
		state, failures, _ := s.circuitBreaker.GetMetrics()
		switch state {
		case CircuitOpen:
			return fmt.Errorf("AI supervisor unavailable: %w (failures=%d, retry in %v)",
				ErrCircuitOpen, failures, s.retry.OpenTimeout)
		case CircuitHalfOpen:
			// Allow execution in half-open state (probing for recovery)
			fmt.Printf("AI supervisor in half-open state (probing for recovery)\n")
		case CircuitClosed:
			// Normal operation
		}
	}
	return nil
}
