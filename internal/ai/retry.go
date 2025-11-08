package ai

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
)

// Pre-compiled regex patterns for parseRetryAfterFromMessage (vc-5b22)
// Compiled at init time for better performance
var (
	retryAfterTryAgainRegex = regexp.MustCompile(`(?i)try again in (\d+)\s*(second|minute|hour)s?`)
	retryAfterWaitRegex     = regexp.MustCompile(`(?i)wait (\d+)\s*(second|minute|hour)s?`)
	retryAfterColonRegex    = regexp.MustCompile(`(?i)retry[_-]?after["']?\s*:\s*(\d+)`)
)

// ErrorType classifies errors for intelligent retry handling (vc-5b22)
type ErrorType int

const (
	ErrorTransient ErrorType = iota // Network hiccup, server error (5xx) - retry with backoff
	ErrorQuota                       // 429 quota/rate limit exceeded - wait for reset
	ErrorInvalid                     // 400 bad request - don't retry
	ErrorAuth                        // 401/403 auth error - don't retry
	ErrorUnknown                     // Catch-all - retry with backoff
)

func (e ErrorType) String() string {
	switch e {
	case ErrorTransient:
		return "TRANSIENT"
	case ErrorQuota:
		return "QUOTA"
	case ErrorInvalid:
		return "INVALID"
	case ErrorAuth:
		return "AUTH"
	default:
		return "UNKNOWN"
	}
}

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

	// Quota retry settings (vc-5b22)
	MaxQuotaWait time.Duration // Maximum time to wait for quota reset (default: 15 minutes)
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
	// Read MaxQuotaWait from environment (vc-5b22)
	maxQuotaWait := 15 * time.Minute
	if env := os.Getenv("VC_MAX_QUOTA_WAIT"); env != "" {
		if d, err := time.ParseDuration(env); err == nil {
			// Validate bounds
			if d <= 0 {
				fmt.Fprintf(os.Stderr, "Warning: VC_MAX_QUOTA_WAIT must be positive (%v), using default 15m\n", d)
				maxQuotaWait = 15 * time.Minute
			} else if d > 24*time.Hour {
				fmt.Fprintf(os.Stderr, "Warning: VC_MAX_QUOTA_WAIT exceeds 24h (%v), capping at 24h\n", d)
				maxQuotaWait = 24 * time.Hour
			} else {
				maxQuotaWait = d
			}
		} else {
			fmt.Fprintf(os.Stderr, "Warning: Invalid VC_MAX_QUOTA_WAIT format (%q), using default 15m\n", env)
		}
	}

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
		MaxQuotaWait:          maxQuotaWait, // Maximum wait for quota reset (vc-5b22)
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
	cb.recordFailureWithType(ErrorUnknown)
}

// RecordFailureWithType records a failed request with error type classification (vc-5b22)
// Quota errors are weighted more heavily to trip the circuit faster
func (cb *CircuitBreaker) recordFailureWithType(errorType ErrorType) {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.lastFailureTime = time.Now()

	// Weight quota errors more heavily (vc-5b22)
	// Quota errors count as 3 failures to trip circuit faster and prevent
	// repeatedly hitting rate limits
	failureIncrement := 1
	if errorType == ErrorQuota {
		failureIncrement = 3
	}

	switch cb.state {
	case CircuitClosed:
		cb.failureCount += failureIncrement
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

// classifyError determines the error type for intelligent retry handling (vc-5b22)
// Returns the error type and suggested wait duration for quota errors
func classifyError(err error) (ErrorType, time.Duration) {
	if err == nil {
		return ErrorUnknown, 0
	}

	// Try to unwrap as Anthropic SDK error
	var apiErr *anthropic.Error
	if errors.As(err, &apiErr) {
		statusCode := apiErr.StatusCode

		// 429 Rate Limit / Quota Exceeded
		if statusCode == http.StatusTooManyRequests {
			waitTime := parseRetryAfter(apiErr)
			return ErrorQuota, waitTime
		}

		// 5xx Server Errors (transient)
		if statusCode >= 500 && statusCode < 600 {
			return ErrorTransient, 0
		}

		// 401/403 Auth Errors (non-retriable)
		if statusCode == http.StatusUnauthorized || statusCode == http.StatusForbidden {
			return ErrorAuth, 0
		}

		// 400 Bad Request (non-retriable)
		if statusCode == http.StatusBadRequest {
			return ErrorInvalid, 0
		}

		// Other 4xx errors (non-retriable)
		if statusCode >= 400 && statusCode < 500 {
			return ErrorInvalid, 0
		}
	}

	// Fallback: check error string for patterns
	errStr := err.Error()

	// Rate limit patterns
	if strings.Contains(errStr, "429") || strings.Contains(errStr, "rate limit") ||
		strings.Contains(errStr, "quota") {
		waitTime := parseRetryAfterFromMessage(errStr)
		return ErrorQuota, waitTime
	}

	// Server error patterns (5xx)
	if strings.Contains(errStr, "500") || strings.Contains(errStr, "502") ||
		strings.Contains(errStr, "503") || strings.Contains(errStr, "504") ||
		strings.Contains(errStr, "internal server error") ||
		strings.Contains(errStr, "bad gateway") ||
		strings.Contains(errStr, "service unavailable") ||
		strings.Contains(errStr, "gateway timeout") {
		return ErrorTransient, 0
	}

	// Network/connection errors (transient)
	if strings.Contains(errStr, "connection refused") ||
		strings.Contains(errStr, "connection reset") ||
		strings.Contains(errStr, "timeout") ||
		strings.Contains(errStr, "temporary failure") ||
		strings.Contains(errStr, "network") ||
		errors.Is(err, context.DeadlineExceeded) {
		return ErrorTransient, 0
	}

	// Auth error patterns
	if strings.Contains(errStr, "401") || strings.Contains(errStr, "403") ||
		strings.Contains(errStr, "unauthorized") || strings.Contains(errStr, "forbidden") {
		return ErrorAuth, 0
	}

	// Client error patterns (4xx)
	if strings.Contains(errStr, "400") || strings.Contains(errStr, "404") ||
		strings.Contains(errStr, "bad request") {
		return ErrorInvalid, 0
	}

	// Default to unknown (will retry with backoff)
	return ErrorUnknown, 0
}

// parseRetryAfter extracts the wait duration from a rate limit error (vc-5b22)
// Checks both HTTP Retry-After header and error message
func parseRetryAfter(apiErr *anthropic.Error) time.Duration {
	// Option 1: Check Retry-After header
	if apiErr.Response != nil {
		if retryAfter := apiErr.Response.Header.Get("Retry-After"); retryAfter != "" {
			// Retry-After can be either seconds (integer) or HTTP-date
			if seconds, err := strconv.Atoi(retryAfter); err == nil {
				return time.Duration(seconds) * time.Second
			}
			// TODO: Parse HTTP-date format if needed
		}

		// Option 2: Check X-RateLimit-Reset header (Unix timestamp)
		if resetTime := apiErr.Response.Header.Get("X-RateLimit-Reset"); resetTime != "" {
			if timestamp, err := strconv.ParseInt(resetTime, 10, 64); err == nil {
				resetAt := time.Unix(timestamp, 0)
				waitTime := time.Until(resetAt)
				if waitTime > 0 {
					return waitTime
				} else if waitTime < 0 {
					// Clock skew or stale header - log for debugging
					fmt.Fprintf(os.Stderr, "Warning: X-RateLimit-Reset is in the past (skew: %v)\n", -waitTime)
				}
			}
		}
	}

	// Option 3: Parse error message from raw JSON
	if rawJSON := apiErr.RawJSON(); rawJSON != "" {
		if wait := parseRetryAfterFromMessage(rawJSON); wait > 0 {
			return wait
		}
	}

	// Fallback: check error string (only if Request is not nil to avoid panic)
	if apiErr.Request != nil {
		if wait := parseRetryAfterFromMessage(apiErr.Error()); wait > 0 {
			return wait
		}
	}

	// Default: conservative wait (1 hour for quota errors)
	// This is safe but may be longer than necessary
	return 1 * time.Hour
}

// parseRetryAfterFromMessage extracts wait duration from error message text (vc-5b22)
// Handles patterns like "try again in 12 minutes" or "wait 720 seconds"
// Uses pre-compiled regex patterns for better performance
func parseRetryAfterFromMessage(msg string) time.Duration {
	// Pattern: "try again in N (second|minute|hour)s?"
	if matches := retryAfterTryAgainRegex.FindStringSubmatch(msg); len(matches) == 3 {
		value, _ := strconv.Atoi(matches[1])
		unit := strings.ToLower(matches[2])
		switch unit {
		case "second":
			return time.Duration(value) * time.Second
		case "minute":
			return time.Duration(value) * time.Minute
		case "hour":
			return time.Duration(value) * time.Hour
		}
	}

	// Pattern: "wait N (second|minute|hour)s?"
	if matches := retryAfterWaitRegex.FindStringSubmatch(msg); len(matches) == 3 {
		value, _ := strconv.Atoi(matches[1])
		unit := strings.ToLower(matches[2])
		switch unit {
		case "second":
			return time.Duration(value) * time.Second
		case "minute":
			return time.Duration(value) * time.Minute
		case "hour":
			return time.Duration(value) * time.Hour
		}
	}

	// Pattern: "retry_after": N or "retry-after: N" (seconds)
	if matches := retryAfterColonRegex.FindStringSubmatch(msg); len(matches) == 2 {
		if seconds, err := strconv.Atoi(matches[1]); err == nil {
			return time.Duration(seconds) * time.Second
		}
	}

	return 0
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
		// Check cost budget before attempting request (vc-e3s7)
		// Note: We check budget per-attempt to handle budget resets during retries
		if err := s.checkBudget(""); err != nil {
			// Budget exceeded, fail fast without retrying
			fmt.Fprintf(os.Stderr, "AI API %s blocked by cost budget: %v\n", operation, err)
			return fmt.Errorf("%s failed: %w", operation, err)
		}

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

		// Classify error for intelligent retry handling (vc-5b22)
		errorType, quotaWait := classifyError(err)

		// Record failure with circuit breaker, weighting quota errors more heavily
		if s.circuitBreaker != nil {
			if errorType == ErrorAuth || errorType == ErrorInvalid {
				// Non-retriable errors don't count against circuit breaker
			} else {
				s.circuitBreaker.recordFailureWithType(errorType)
			}
		}

		// Check if we should retry based on error type
		switch errorType {
		case ErrorAuth, ErrorInvalid:
			// Non-retriable errors - fail immediately
			fmt.Fprintf(os.Stderr, "AI API %s failed with non-retriable error (%s): %v\n",
				operation, errorType, err)
			return err

		case ErrorQuota:
			// Quota exceeded - intelligent wait based on retry-after (vc-5b22)
			if quotaWait > s.retry.MaxQuotaWait {
				fmt.Fprintf(os.Stderr, "⚠️  Quota exceeded: retry-after (%v) exceeds max wait (%v)\n",
					quotaWait, s.retry.MaxQuotaWait)
				fmt.Fprintf(os.Stderr, "    Current attempt: %d/%d\n", attempt+1, s.retry.MaxRetries+1)
				fmt.Fprintf(os.Stderr, "    Consider adjusting VC_MAX_QUOTA_WAIT or waiting manually\n")
				return fmt.Errorf("%s failed: %w (quota wait %v exceeds max %v)",
					operation, err, quotaWait, s.retry.MaxQuotaWait)
			}

			// Don't retry if we've exhausted attempts
			if attempt == s.retry.MaxRetries {
				break
			}

			// Check if context is already canceled
			if ctx.Err() != nil {
				return fmt.Errorf("%s failed: context canceled: %w", operation, ctx.Err())
			}

			// Wait for quota reset
			resetAt := time.Now().Add(quotaWait)
			fmt.Printf("⚠️  Quota exceeded: API rate limit hit\n")
			fmt.Printf("    Retry after: %v (at %s)\n", quotaWait, resetAt.Format("15:04:05 MST"))
			fmt.Printf("    Attempt: %d/%d\n", attempt+1, s.retry.MaxRetries+1)
			fmt.Printf("    Waiting for quota reset...\n")

			select {
			case <-time.After(quotaWait):
				fmt.Printf("Quota wait completed, retrying %s\n", operation)
				continue // Retry immediately after wait
			case <-ctx.Done():
				return fmt.Errorf("%s failed: context canceled during quota wait: %w", operation, ctx.Err())
			}

		case ErrorTransient, ErrorUnknown:
			// Transient errors - use exponential backoff
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
	}

	return fmt.Errorf("%s failed after %d attempts: %w", operation, s.retry.MaxRetries+1, lastErr)
}

// isRetriableError determines if an error is retriable (transient)
// Deprecated: Use classifyError instead for intelligent retry handling (vc-5b22)
// This function is kept for backwards compatibility but now uses classifyError internally
func isRetriableError(err error) bool {
	if err == nil {
		return false
	}

	errorType, _ := classifyError(err)
	return errorType != ErrorAuth && errorType != ErrorInvalid
}
