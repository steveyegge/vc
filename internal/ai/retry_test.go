package ai

import (
	"errors"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/stretchr/testify/assert"
)

// TestParseRetryAfterFromMessage tests parsing retry-after durations from error messages
func TestParseRetryAfterFromMessage(t *testing.T) {
	tests := []struct {
		name     string
		message  string
		expected time.Duration
	}{
		{
			name:     "try again in 12 minutes",
			message:  "rate limit exceeded, try again in 12 minutes",
			expected: 12 * time.Minute,
		},
		{
			name:     "try again in 720 seconds",
			message:  "quota exceeded, try again in 720 seconds",
			expected: 720 * time.Second,
		},
		{
			name:     "try again in 1 hour",
			message:  "rate limit hit, try again in 1 hour",
			expected: 1 * time.Hour,
		},
		{
			name:     "wait 5 minutes",
			message:  "please wait 5 minutes before retrying",
			expected: 5 * time.Minute,
		},
		{
			name:     "wait 30 seconds",
			message:  "wait 30 seconds",
			expected: 30 * time.Second,
		},
		{
			name:     "retry_after: 600",
			message:  `{"error": "rate_limit_error", "retry_after": 600}`,
			expected: 600 * time.Second,
		},
		{
			name:     "retry-after: 300",
			message:  "retry-after: 300 seconds recommended",
			expected: 300 * time.Second,
		},
		{
			name:     "case insensitive - Try Again In 10 Minutes",
			message:  "Try Again In 10 Minutes",
			expected: 10 * time.Minute,
		},
		{
			name:     "plural - try again in 2 hours",
			message:  "try again in 2 hours",
			expected: 2 * time.Hour,
		},
		{
			name:     "no match",
			message:  "unknown error format",
			expected: 0,
		},
		{
			name:     "empty message",
			message:  "",
			expected: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parseRetryAfterFromMessage(tt.message)
			assert.Equal(t, tt.expected, result, "Expected %v, got %v", tt.expected, result)
		})
	}
}

// TestClassifyError tests error type classification
func TestClassifyError(t *testing.T) {
	tests := []struct {
		name              string
		err               error
		expectedType      ErrorType
		expectWaitTime    bool
		minWait           time.Duration
		maxWait           time.Duration
	}{
		{
			name:         "nil error",
			err:          nil,
			expectedType: ErrorUnknown,
			expectWaitTime: false,
		},
		{
			name:         "generic error",
			err:          errors.New("something went wrong"),
			expectedType: ErrorUnknown,
			expectWaitTime: false,
		},
		{
			name:         "429 rate limit in message",
			err:          errors.New("HTTP 429: rate limit exceeded, try again in 12 minutes"),
			expectedType: ErrorQuota,
			expectWaitTime: true,
			minWait:      12 * time.Minute,
			maxWait:      12 * time.Minute,
		},
		{
			name:         "quota in message",
			err:          errors.New("quota exceeded, wait 720 seconds"),
			expectedType: ErrorQuota,
			expectWaitTime: true,
			minWait:      720 * time.Second,
			maxWait:      720 * time.Second,
		},
		{
			name:         "500 internal server error",
			err:          errors.New("HTTP 500: internal server error"),
			expectedType: ErrorTransient,
			expectWaitTime: false,
		},
		{
			name:         "502 bad gateway",
			err:          errors.New("502 bad gateway"),
			expectedType: ErrorTransient,
			expectWaitTime: false,
		},
		{
			name:         "503 service unavailable",
			err:          errors.New("service unavailable (503)"),
			expectedType: ErrorTransient,
			expectWaitTime: false,
		},
		{
			name:         "504 gateway timeout",
			err:          errors.New("504 gateway timeout"),
			expectedType: ErrorTransient,
			expectWaitTime: false,
		},
		{
			name:         "connection refused",
			err:          errors.New("connection refused"),
			expectedType: ErrorTransient,
			expectWaitTime: false,
		},
		{
			name:         "network timeout",
			err:          errors.New("network timeout"),
			expectedType: ErrorTransient,
			expectWaitTime: false,
		},
		{
			name:         "400 bad request",
			err:          errors.New("HTTP 400: bad request"),
			expectedType: ErrorInvalid,
			expectWaitTime: false,
		},
		{
			name:         "404 not found",
			err:          errors.New("404 not found"),
			expectedType: ErrorInvalid,
			expectWaitTime: false,
		},
		{
			name:         "401 unauthorized",
			err:          errors.New("401 unauthorized"),
			expectedType: ErrorAuth,
			expectWaitTime: false,
		},
		{
			name:         "403 forbidden",
			err:          errors.New("HTTP 403: forbidden"),
			expectedType: ErrorAuth,
			expectWaitTime: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			errorType, waitTime := classifyError(tt.err)
			assert.Equal(t, tt.expectedType, errorType, "Expected error type %s, got %s", tt.expectedType, errorType)

			if tt.expectWaitTime {
				assert.Greater(t, waitTime, time.Duration(0), "Expected non-zero wait time")
				if tt.minWait > 0 {
					assert.GreaterOrEqual(t, waitTime, tt.minWait, "Wait time should be >= %v", tt.minWait)
				}
				if tt.maxWait > 0 {
					assert.LessOrEqual(t, waitTime, tt.maxWait, "Wait time should be <= %v", tt.maxWait)
				}
			} else {
				assert.Equal(t, time.Duration(0), waitTime, "Expected zero wait time")
			}
		})
	}
}

// TestClassifyErrorWithAnthropicSDKError tests classification with actual SDK error types
func TestClassifyErrorWithAnthropicSDKError(t *testing.T) {
	tests := []struct {
		name          string
		statusCode    int
		retryAfter    string
		expectedType  ErrorType
		expectWait    bool
	}{
		{
			name:         "429 with Retry-After header (seconds)",
			statusCode:   http.StatusTooManyRequests,
			retryAfter:   "720",
			expectedType: ErrorQuota,
			expectWait:   true,
		},
		{
			name:         "429 without Retry-After",
			statusCode:   http.StatusTooManyRequests,
			retryAfter:   "",
			expectedType: ErrorQuota,
			expectWait:   true, // Should default to 1 hour
		},
		{
			name:         "500 internal server error",
			statusCode:   http.StatusInternalServerError,
			expectedType: ErrorTransient,
			expectWait:   false,
		},
		{
			name:         "502 bad gateway",
			statusCode:   http.StatusBadGateway,
			expectedType: ErrorTransient,
			expectWait:   false,
		},
		{
			name:         "503 service unavailable",
			statusCode:   http.StatusServiceUnavailable,
			expectedType: ErrorTransient,
			expectWait:   false,
		},
		{
			name:         "400 bad request",
			statusCode:   http.StatusBadRequest,
			expectedType: ErrorInvalid,
			expectWait:   false,
		},
		{
			name:         "401 unauthorized",
			statusCode:   http.StatusUnauthorized,
			expectedType: ErrorAuth,
			expectWait:   false,
		},
		{
			name:         "403 forbidden",
			statusCode:   http.StatusForbidden,
			expectedType: ErrorAuth,
			expectWait:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create mock HTTP response
			resp := &http.Response{
				StatusCode: tt.statusCode,
				Header:     http.Header{},
			}
			if tt.retryAfter != "" {
				resp.Header.Set("Retry-After", tt.retryAfter)
			}

			// Create Anthropic SDK error
			apiErr := &anthropic.Error{
				StatusCode: tt.statusCode,
				Response:   resp,
			}

			errorType, waitTime := classifyError(apiErr)
			assert.Equal(t, tt.expectedType, errorType, "Expected error type %s, got %s", tt.expectedType, errorType)

			if tt.expectWait {
				assert.Greater(t, waitTime, time.Duration(0), "Expected non-zero wait time")
			} else {
				assert.Equal(t, time.Duration(0), waitTime, "Expected zero wait time")
			}
		})
	}
}

// TestParseRetryAfterWithHeaders tests parsing retry-after from HTTP headers
func TestParseRetryAfterWithHeaders(t *testing.T) {
	tests := []struct {
		name         string
		retryAfter   string
		rateLimitReset string
		expectedMin  time.Duration
		expectedMax  time.Duration
	}{
		{
			name:        "Retry-After in seconds",
			retryAfter:  "720",
			expectedMin: 720 * time.Second,
			expectedMax: 720 * time.Second,
		},
		{
			name:        "Retry-After 60 seconds",
			retryAfter:  "60",
			expectedMin: 60 * time.Second,
			expectedMax: 60 * time.Second,
		},
		{
			name:           "X-RateLimit-Reset (future timestamp)",
			rateLimitReset: fmt.Sprintf("%d", time.Now().Add(10*time.Minute).Unix()),
			expectedMin:    9 * time.Minute,  // Allow some slack for test execution time
			expectedMax:    11 * time.Minute, // Allow some slack
		},
		{
			name:        "No headers - default fallback",
			expectedMin: 1 * time.Hour,
			expectedMax: 1 * time.Hour,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create mock HTTP response
			resp := &http.Response{
				StatusCode: http.StatusTooManyRequests,
				Header:     http.Header{},
			}
			if tt.retryAfter != "" {
				resp.Header.Set("Retry-After", tt.retryAfter)
			}
			if tt.rateLimitReset != "" {
				resp.Header.Set("X-RateLimit-Reset", tt.rateLimitReset)
			}

			apiErr := &anthropic.Error{
				StatusCode: http.StatusTooManyRequests,
				Response:   resp,
			}

			waitTime := parseRetryAfter(apiErr)
			assert.GreaterOrEqual(t, waitTime, tt.expectedMin, "Wait time should be >= %v", tt.expectedMin)
			assert.LessOrEqual(t, waitTime, tt.expectedMax, "Wait time should be <= %v", tt.expectedMax)
		})
	}
}

// TestIsRetriableErrorBackwardsCompatibility tests that isRetriableError still works
func TestIsRetriableErrorBackwardsCompatibility(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		shouldRetry bool
	}{
		{
			name:       "nil error",
			err:        nil,
			shouldRetry: false,
		},
		{
			name:       "quota error - should retry",
			err:        errors.New("429 rate limit exceeded"),
			shouldRetry: true,
		},
		{
			name:       "transient error - should retry",
			err:        errors.New("500 internal server error"),
			shouldRetry: true,
		},
		{
			name:       "auth error - should NOT retry",
			err:        errors.New("401 unauthorized"),
			shouldRetry: false,
		},
		{
			name:       "invalid request - should NOT retry",
			err:        errors.New("400 bad request"),
			shouldRetry: false,
		},
		{
			name:       "unknown error - should retry (conservative)",
			err:        errors.New("mysterious error"),
			shouldRetry: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isRetriableError(tt.err)
			assert.Equal(t, tt.shouldRetry, result, "Expected shouldRetry=%v, got %v", tt.shouldRetry, result)
		})
	}
}

// TestCircuitBreakerQuotaWeighting tests that quota errors are weighted more heavily
func TestCircuitBreakerQuotaWeighting(t *testing.T) {
	cb := NewCircuitBreaker(5, 2, 30*time.Second)

	// Record 1 quota error (should count as 3)
	cb.recordFailureWithType(ErrorQuota)
	state, failures, _ := cb.GetMetrics()
	assert.Equal(t, CircuitClosed, state, "Circuit should still be closed")
	assert.Equal(t, 3, failures, "Quota error should count as 3 failures")

	// Record 1 more quota error (3 + 3 = 6, should trip circuit at threshold 5)
	cb.recordFailureWithType(ErrorQuota)
	state, failures, _ = cb.GetMetrics()
	assert.Equal(t, CircuitOpen, state, "Circuit should be open after 2 quota errors")
	assert.Equal(t, 6, failures, "Should have 6 total failures")
}

// TestCircuitBreakerTransientErrors tests that transient errors count as 1
func TestCircuitBreakerTransientErrors(t *testing.T) {
	cb := NewCircuitBreaker(5, 2, 30*time.Second)

	// Record 4 transient errors
	for i := 0; i < 4; i++ {
		cb.recordFailureWithType(ErrorTransient)
	}
	state, failures, _ := cb.GetMetrics()
	assert.Equal(t, CircuitClosed, state, "Circuit should still be closed")
	assert.Equal(t, 4, failures, "Should have 4 failures")

	// Record 1 more transient error (should trip at 5)
	cb.recordFailureWithType(ErrorTransient)
	state, failures, _ = cb.GetMetrics()
	assert.Equal(t, CircuitOpen, state, "Circuit should be open after 5 transient errors")
	assert.Equal(t, 5, failures, "Should have 5 total failures")
}

// TestCircuitBreakerNonRetriableErrors tests that auth/invalid errors don't affect circuit
func TestCircuitBreakerNonRetriableErrors(t *testing.T) {
	cb := NewCircuitBreaker(5, 2, 30*time.Second)

	// These errors should not be recorded in circuit breaker
	// (tested via retryWithBackoff which skips recording for these types)

	// For this test, we verify the public RecordFailure method still works
	cb.RecordFailure() // Should count as unknown (1 failure)
	state, failures, _ := cb.GetMetrics()
	assert.Equal(t, CircuitClosed, state)
	assert.Equal(t, 1, failures)
}

// TestErrorTypeStringer tests ErrorType.String() method
func TestErrorTypeStringer(t *testing.T) {
	tests := []struct {
		errorType ErrorType
		expected  string
	}{
		{ErrorTransient, "TRANSIENT"},
		{ErrorQuota, "QUOTA"},
		{ErrorInvalid, "INVALID"},
		{ErrorAuth, "AUTH"},
		{ErrorUnknown, "UNKNOWN"},
		{ErrorType(99), "UNKNOWN"}, // Invalid value
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			result := tt.errorType.String()
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestDefaultRetryConfigMaxQuotaWait tests that MaxQuotaWait is set correctly
func TestDefaultRetryConfigMaxQuotaWait(t *testing.T) {
	// Test default value (no env var)
	t.Setenv("VC_MAX_QUOTA_WAIT", "")
	cfg := DefaultRetryConfig()
	assert.Equal(t, 15*time.Minute, cfg.MaxQuotaWait, "Default MaxQuotaWait should be 15 minutes")

	// Test custom value via env var
	t.Setenv("VC_MAX_QUOTA_WAIT", "30m")
	cfg = DefaultRetryConfig()
	assert.Equal(t, 30*time.Minute, cfg.MaxQuotaWait, "Should read MaxQuotaWait from env var")

	// Test invalid env var (should use default)
	t.Setenv("VC_MAX_QUOTA_WAIT", "invalid")
	cfg = DefaultRetryConfig()
	assert.Equal(t, 15*time.Minute, cfg.MaxQuotaWait, "Should use default for invalid env var")

	// Test negative value (should use default)
	t.Setenv("VC_MAX_QUOTA_WAIT", "-5m")
	cfg = DefaultRetryConfig()
	assert.Equal(t, 15*time.Minute, cfg.MaxQuotaWait, "Should use default for negative value")

	// Test excessive value (should cap at 24h)
	t.Setenv("VC_MAX_QUOTA_WAIT", "48h")
	cfg = DefaultRetryConfig()
	assert.Equal(t, 24*time.Hour, cfg.MaxQuotaWait, "Should cap at 24h for excessive value")

	// Test boundary: exactly 24h (should be allowed)
	t.Setenv("VC_MAX_QUOTA_WAIT", "24h")
	cfg = DefaultRetryConfig()
	assert.Equal(t, 24*time.Hour, cfg.MaxQuotaWait, "Should allow exactly 24h")

	// Test boundary: 0 (should use default)
	t.Setenv("VC_MAX_QUOTA_WAIT", "0")
	cfg = DefaultRetryConfig()
	assert.Equal(t, 15*time.Minute, cfg.MaxQuotaWait, "Zero is not positive, should use default")
}

// BenchmarkClassifyError benchmarks error classification performance
func BenchmarkClassifyError(b *testing.B) {
	err := errors.New("HTTP 429: rate limit exceeded, try again in 12 minutes")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		classifyError(err)
	}
}

// BenchmarkParseRetryAfterFromMessage benchmarks message parsing
func BenchmarkParseRetryAfterFromMessage(b *testing.B) {
	msg := "rate limit exceeded, try again in 12 minutes"
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		parseRetryAfterFromMessage(msg)
	}
}
