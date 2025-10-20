package ai

import (
	"fmt"
	"os"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/steveyegge/vc/internal/storage"
	"github.com/steveyegge/vc/internal/types"
	"golang.org/x/sync/semaphore"
)

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
}

// Compile-time check that Supervisor implements MissionPlanner
var _ types.MissionPlanner = (*Supervisor)(nil)

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
	}, nil
}
