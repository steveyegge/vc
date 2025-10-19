package deduplication

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

// Config holds configuration for the deduplication engine
type Config struct {
	// ConfidenceThreshold is the minimum confidence score (0.0-1.0) to mark as duplicate
	// Higher values = more conservative (fewer false positives, more false negatives)
	// Lower values = more aggressive (more false positives, fewer false negatives)
	// Default: 0.85 (high confidence required to skip filing an issue)
	ConfidenceThreshold float64

	// LookbackWindow is how far back to search for potential duplicates
	// Default: 7 days (recent open issues only)
	// Too large = slow comparisons, too many false positives
	// Too small = miss legitimate duplicates that were filed earlier
	LookbackWindow time.Duration

	// MaxCandidates is the maximum number of existing issues to compare against
	// This limits the AI API costs and processing time
	// Default: 50 (compare against up to 50 recent open issues)
	MaxCandidates int

	// BatchSize is the number of comparisons to send in a single AI API call
	// Larger batches = fewer API calls, but longer individual requests
	// Default: 10 (balance between efficiency and responsiveness)
	BatchSize int

	// EnableWithinBatchDedup enables deduplication within the candidate batch itself
	// When true, if multiple candidates are duplicates of each other, only the first is kept
	// Default: true (prevent filing multiple identical issues in the same batch)
	EnableWithinBatchDedup bool

	// FailOpen determines behavior when deduplication fails
	// If true: file the issue anyway (fail-safe, prefer duplicates over lost work)
	// If false: return error and block issue creation
	// Default: true (better to have a duplicate than lose work)
	FailOpen bool

	// IncludeClosedIssues includes recently closed issues in duplicate checks
	// Useful for preventing re-filing of issues that were just closed
	// Default: false (only check against open issues)
	IncludeClosedIssues bool

	// MinTitleLength is the minimum title length to perform deduplication
	// Very short titles often lack semantic meaning for comparison
	// Default: 10 characters
	MinTitleLength int

	// MaxRetries is the number of times to retry AI API calls on failure
	// Default: 2 (total 3 attempts including initial call)
	MaxRetries int

	// RequestTimeout is the timeout for individual AI API calls
	// Default: 30 seconds
	RequestTimeout time.Duration
}

// DefaultConfig returns the default deduplication configuration
//
// These defaults are chosen to:
// - Prevent false positives (high confidence threshold)
// - Keep costs reasonable (limited candidates and batch size)
// - Fail safely (file duplicates rather than lose work)
// - Focus on recent issues (7 day window)
func DefaultConfig() Config {
	return Config{
		ConfidenceThreshold:    0.85,              // High confidence required
		LookbackWindow:         7 * 24 * time.Hour, // 7 days
		MaxCandidates:          50,                // Up to 50 recent issues
		BatchSize:              10,                // 10 comparisons per AI call
		EnableWithinBatchDedup: true,              // Dedup within batch
		FailOpen:               true,              // File on error
		IncludeClosedIssues:    false,             // Open issues only
		MinTitleLength:         10,                // Minimum title length
		MaxRetries:             2,                 // Retry twice on failure
		RequestTimeout:         30 * time.Second,  // 30 second timeout
	}
}

// Validate checks if the configuration has valid values
func (c Config) Validate() error {
	if c.ConfidenceThreshold < 0.0 || c.ConfidenceThreshold > 1.0 {
		return fmt.Errorf("confidence_threshold must be between 0.0 and 1.0 (got %.2f)",
			c.ConfidenceThreshold)
	}
	if c.LookbackWindow <= 0 {
		return fmt.Errorf("lookback_window must be positive (got %v)", c.LookbackWindow)
	}
	if c.LookbackWindow > 90*24*time.Hour {
		return fmt.Errorf("lookback_window too large (got %v, max 90 days)", c.LookbackWindow)
	}
	if c.MaxCandidates <= 0 {
		return fmt.Errorf("max_candidates must be positive (got %d)", c.MaxCandidates)
	}
	if c.MaxCandidates > 500 {
		return fmt.Errorf("max_candidates too large (got %d, max 500)", c.MaxCandidates)
	}
	if c.BatchSize <= 0 {
		return fmt.Errorf("batch_size must be positive (got %d)", c.BatchSize)
	}
	if c.BatchSize > 100 {
		return fmt.Errorf("batch_size too large (got %d, max 100)", c.BatchSize)
	}
	if c.MinTitleLength < 0 {
		return fmt.Errorf("min_title_length cannot be negative (got %d)", c.MinTitleLength)
	}
	if c.MinTitleLength > 500 {
		return fmt.Errorf("min_title_length too large (got %d, max 500)", c.MinTitleLength)
	}
	if c.MaxRetries < 0 {
		return fmt.Errorf("max_retries cannot be negative (got %d)", c.MaxRetries)
	}
	if c.MaxRetries > 10 {
		return fmt.Errorf("max_retries too large (got %d, max 10)", c.MaxRetries)
	}
	if c.RequestTimeout <= 0 {
		return fmt.Errorf("request_timeout must be positive (got %v)", c.RequestTimeout)
	}
	if c.RequestTimeout > 5*time.Minute {
		return fmt.Errorf("request_timeout too large (got %v, max 5 minutes)", c.RequestTimeout)
	}
	return nil
}

// String returns a human-readable representation of the config
func (c Config) String() string {
	return fmt.Sprintf(
		"Config{Threshold: %.2f, Lookback: %v, MaxCandidates: %d, BatchSize: %d, "+
			"WithinBatch: %t, FailOpen: %t, IncludeClosed: %t, MinTitleLen: %d, "+
			"MaxRetries: %d, Timeout: %v}",
		c.ConfidenceThreshold, c.LookbackWindow, c.MaxCandidates, c.BatchSize,
		c.EnableWithinBatchDedup, c.FailOpen, c.IncludeClosedIssues, c.MinTitleLength,
		c.MaxRetries, c.RequestTimeout,
	)
}

// ConfigFromEnv creates a Config from environment variables, falling back to defaults
//
// Environment variables:
//   - VC_DEDUP_CONFIDENCE_THRESHOLD: Minimum confidence (0.0-1.0) to mark as duplicate (default: 0.85)
//   - VC_DEDUP_LOOKBACK_DAYS: How many days to look back for duplicates (default: 7)
//   - VC_DEDUP_MAX_CANDIDATES: Maximum number of issues to compare against (default: 50)
//   - VC_DEDUP_BATCH_SIZE: Number of comparisons per AI call (default: 10)
//   - VC_DEDUP_WITHIN_BATCH: Enable within-batch deduplication (default: true)
//   - VC_DEDUP_FAIL_OPEN: File issue on dedup failure (default: true)
//   - VC_DEDUP_INCLUDE_CLOSED: Include closed issues in comparison (default: false)
//   - VC_DEDUP_MIN_TITLE_LENGTH: Minimum title length for dedup (default: 10)
//   - VC_DEDUP_MAX_RETRIES: Maximum retry attempts (default: 2)
//   - VC_DEDUP_TIMEOUT_SECS: Request timeout in seconds (default: 30)
//
// Returns an error if any environment variable has an invalid value.
func ConfigFromEnv() (Config, error) {
	cfg := DefaultConfig()

	// Parse environment variables with validation
	if err := parseEnvFloat("VC_DEDUP_CONFIDENCE_THRESHOLD", &cfg.ConfidenceThreshold); err != nil {
		return cfg, err
	}
	if err := parseEnvDuration("VC_DEDUP_LOOKBACK_DAYS", &cfg.LookbackWindow, 24*time.Hour); err != nil {
		return cfg, err
	}
	if err := parseEnvInt("VC_DEDUP_MAX_CANDIDATES", &cfg.MaxCandidates); err != nil {
		return cfg, err
	}
	if err := parseEnvInt("VC_DEDUP_BATCH_SIZE", &cfg.BatchSize); err != nil {
		return cfg, err
	}
	if err := parseEnvBool("VC_DEDUP_WITHIN_BATCH", &cfg.EnableWithinBatchDedup); err != nil {
		return cfg, err
	}
	if err := parseEnvBool("VC_DEDUP_FAIL_OPEN", &cfg.FailOpen); err != nil {
		return cfg, err
	}
	if err := parseEnvBool("VC_DEDUP_INCLUDE_CLOSED", &cfg.IncludeClosedIssues); err != nil {
		return cfg, err
	}
	if err := parseEnvInt("VC_DEDUP_MIN_TITLE_LENGTH", &cfg.MinTitleLength); err != nil {
		return cfg, err
	}
	if err := parseEnvInt("VC_DEDUP_MAX_RETRIES", &cfg.MaxRetries); err != nil {
		return cfg, err
	}
	if err := parseEnvDuration("VC_DEDUP_TIMEOUT_SECS", &cfg.RequestTimeout, time.Second); err != nil {
		return cfg, err
	}

	// Validate the final configuration
	if err := cfg.Validate(); err != nil {
		return cfg, fmt.Errorf("invalid configuration from environment: %w", err)
	}

	return cfg, nil
}

// parseEnvFloat parses a float64 from an environment variable
func parseEnvFloat(key string, dest *float64) error {
	value := os.Getenv(key)
	if value == "" {
		return nil // Use default
	}
	parsed, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return fmt.Errorf("invalid value for %s: %w", key, err)
	}
	*dest = parsed
	return nil
}

// parseEnvInt parses an int from an environment variable
func parseEnvInt(key string, dest *int) error {
	value := os.Getenv(key)
	if value == "" {
		return nil // Use default
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return fmt.Errorf("invalid value for %s: %w", key, err)
	}
	*dest = parsed
	return nil
}

// parseEnvBool parses a bool from an environment variable
func parseEnvBool(key string, dest *bool) error {
	value := os.Getenv(key)
	if value == "" {
		return nil // Use default
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return fmt.Errorf("invalid value for %s: %w", key, err)
	}
	*dest = parsed
	return nil
}

// parseEnvDuration parses a duration from an environment variable
// The multiplier is used to convert the numeric value to a duration
// (e.g., for days: multiplier = 24*time.Hour)
func parseEnvDuration(key string, dest *time.Duration, multiplier time.Duration) error {
	value := os.Getenv(key)
	if value == "" {
		return nil // Use default
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return fmt.Errorf("invalid value for %s: %w", key, err)
	}
	*dest = time.Duration(parsed) * multiplier
	return nil
}
