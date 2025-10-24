package ai

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"
)

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

	// Concurrency limit (vc-220)
	MaxConcurrentCalls int // Maximum concurrent AI API calls (default: 3, 0 = unlimited)
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
		MaxConcurrentCalls:    3, // Limit concurrent AI calls to prevent rate limiting (vc-220)
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

// retryWithBackoff executes an operation with retry and exponential backoff
func (s *Supervisor) retryWithBackoff(ctx context.Context, operation string, fn func(context.Context) error) error {
	// Acquire concurrency slot if limiter is enabled (vc-220)
	if s.concurrencySem != nil {
		if err := s.concurrencySem.Acquire(ctx, 1); err != nil {
			return fmt.Errorf("failed to acquire concurrency slot for %s: %w", operation, err)
		}
		defer s.concurrencySem.Release(1)
	}

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

		// Check if context is already canceled
		if ctx.Err() != nil {
			return fmt.Errorf("%s failed: context canceled: %w", operation, ctx.Err())
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
			return fmt.Errorf("%s failed: context canceled during backoff: %w", operation, ctx.Err())
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
