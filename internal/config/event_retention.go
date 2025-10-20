package config

import (
	"fmt"
	"os"
	"strconv"
)

// EventRetentionConfig holds configuration for event retention and cleanup
type EventRetentionConfig struct {
	// RetentionDays is the retention period for regular events (in days)
	// Events older than this are eligible for deletion
	// Default: 30, Range: 1-365
	RetentionDays int

	// RetentionCriticalDays is the retention period for critical/error events (in days)
	// Critical events are kept longer for error pattern analysis
	// Must be >= RetentionDays
	// Default: 90, Range: 1-730
	RetentionCriticalDays int

	// PerIssueLimitEvents is the maximum number of events to keep per issue
	// When this limit is reached, oldest non-critical events are deleted
	// Set to 0 for unlimited
	// Default: 1000, Range: 0 or 100-10000
	PerIssueLimitEvents int

	// GlobalLimitEvents is the maximum total number of events to keep
	// This is a safety limit to prevent database bloat
	// Aggressive cleanup triggered at 95% of this limit
	// Default: 100000, Range: 1000-1000000
	GlobalLimitEvents int

	// CleanupIntervalHours is how often to run cleanup (in hours)
	// Default: 24, Range: 1-168 (1 week)
	CleanupIntervalHours int

	// CleanupBatchSize is the number of events to delete per transaction
	// Larger batches = faster cleanup but longer locks
	// Default: 1000, Range: 100-10000
	CleanupBatchSize int

	// CleanupEnabled controls whether automatic cleanup is enabled
	// Default: true
	CleanupEnabled bool

	// CleanupStrategy determines which events to delete first
	// Options: "oldest_first" or "oldest_non_critical"
	// Default: "oldest_non_critical"
	CleanupStrategy string

	// CleanupVacuum controls whether to run VACUUM after cleanup
	// VACUUM reclaims disk space but can lock the database
	// Default: false
	CleanupVacuum bool
}

// DefaultEventRetentionConfig returns the default event retention configuration
//
// These defaults are chosen to:
// - Provide sufficient debugging history (30 days)
// - Extend critical event retention for error analysis (90 days)
// - Prevent runaway issues (1000 events per issue max)
// - Cap total database size (100k events = ~50 MB)
// - Run cleanup daily during off-hours
// - Use non-blocking cleanup (no VACUUM by default)
func DefaultEventRetentionConfig() EventRetentionConfig {
	return EventRetentionConfig{
		RetentionDays:         30,
		RetentionCriticalDays: 90,
		PerIssueLimitEvents:   1000,
		GlobalLimitEvents:     100000,
		CleanupIntervalHours:  24,
		CleanupBatchSize:      1000,
		CleanupEnabled:        true,
		CleanupStrategy:       "oldest_non_critical",
		CleanupVacuum:         false,
	}
}

// Validate checks if the configuration has valid values
func (c EventRetentionConfig) Validate() error {
	// Validate RetentionDays
	if c.RetentionDays < 1 || c.RetentionDays > 365 {
		return fmt.Errorf("retention_days must be between 1 and 365 (got %d)", c.RetentionDays)
	}

	// Validate RetentionCriticalDays
	if c.RetentionCriticalDays < 1 || c.RetentionCriticalDays > 730 {
		return fmt.Errorf("retention_critical_days must be between 1 and 730 (got %d)",
			c.RetentionCriticalDays)
	}
	if c.RetentionCriticalDays < c.RetentionDays {
		return fmt.Errorf("retention_critical_days (%d) must be >= retention_days (%d)",
			c.RetentionCriticalDays, c.RetentionDays)
	}

	// Validate PerIssueLimitEvents (0 = unlimited, or 100-10000)
	if c.PerIssueLimitEvents < 0 {
		return fmt.Errorf("per_issue_limit_events cannot be negative (got %d)",
			c.PerIssueLimitEvents)
	}
	if c.PerIssueLimitEvents > 0 && c.PerIssueLimitEvents < 100 {
		return fmt.Errorf("per_issue_limit_events must be 0 (unlimited) or >= 100 (got %d)",
			c.PerIssueLimitEvents)
	}
	if c.PerIssueLimitEvents > 10000 {
		return fmt.Errorf("per_issue_limit_events too large (got %d, max 10000)",
			c.PerIssueLimitEvents)
	}

	// Validate GlobalLimitEvents
	if c.GlobalLimitEvents < 1000 {
		return fmt.Errorf("global_limit_events must be at least 1000 (got %d)",
			c.GlobalLimitEvents)
	}
	if c.GlobalLimitEvents > 1000000 {
		return fmt.Errorf("global_limit_events too large (got %d, max 1000000)",
			c.GlobalLimitEvents)
	}

	// Validate CleanupIntervalHours
	if c.CleanupIntervalHours < 1 {
		return fmt.Errorf("cleanup_interval_hours must be at least 1 (got %d)",
			c.CleanupIntervalHours)
	}
	if c.CleanupIntervalHours > 168 {
		return fmt.Errorf("cleanup_interval_hours too large (got %d, max 168)",
			c.CleanupIntervalHours)
	}

	// Validate CleanupBatchSize
	if c.CleanupBatchSize < 100 {
		return fmt.Errorf("cleanup_batch_size must be at least 100 (got %d)",
			c.CleanupBatchSize)
	}
	if c.CleanupBatchSize > 10000 {
		return fmt.Errorf("cleanup_batch_size too large (got %d, max 10000)",
			c.CleanupBatchSize)
	}

	// Validate CleanupStrategy
	if c.CleanupStrategy != "oldest_first" && c.CleanupStrategy != "oldest_non_critical" {
		return fmt.Errorf("cleanup_strategy must be 'oldest_first' or 'oldest_non_critical' (got %q)",
			c.CleanupStrategy)
	}

	return nil
}

// String returns a human-readable representation of the config
func (c EventRetentionConfig) String() string {
	return fmt.Sprintf(
		"EventRetentionConfig{RetentionDays: %d, RetentionCriticalDays: %d, "+
			"PerIssueLimit: %d, GlobalLimit: %d, CleanupInterval: %dh, "+
			"BatchSize: %d, Enabled: %t, Strategy: %s, Vacuum: %t}",
		c.RetentionDays, c.RetentionCriticalDays, c.PerIssueLimitEvents,
		c.GlobalLimitEvents, c.CleanupIntervalHours, c.CleanupBatchSize,
		c.CleanupEnabled, c.CleanupStrategy, c.CleanupVacuum,
	)
}

// EventRetentionConfigFromEnv creates an EventRetentionConfig from environment variables,
// falling back to defaults
//
// Environment variables:
//   - VC_EVENT_RETENTION_DAYS: Retention period for regular events in days (default: 30)
//   - VC_EVENT_RETENTION_CRITICAL_DAYS: Retention period for critical events in days (default: 90)
//   - VC_EVENT_PER_ISSUE_LIMIT: Maximum events per issue, 0 for unlimited (default: 1000)
//   - VC_EVENT_GLOBAL_LIMIT: Maximum total events (default: 100000)
//   - VC_EVENT_CLEANUP_INTERVAL_HOURS: How often to run cleanup in hours (default: 24)
//   - VC_EVENT_CLEANUP_BATCH_SIZE: Events to delete per transaction (default: 1000)
//   - VC_EVENT_CLEANUP_ENABLED: Enable automatic cleanup (default: true)
//   - VC_EVENT_CLEANUP_STRATEGY: Which events to delete first (default: oldest_non_critical)
//   - VC_EVENT_CLEANUP_VACUUM: Run VACUUM after cleanup (default: false)
//
// Returns an error if any environment variable has an invalid value.
func EventRetentionConfigFromEnv() (EventRetentionConfig, error) {
	cfg := DefaultEventRetentionConfig()

	// Parse environment variables with validation
	if err := parseEnvInt("VC_EVENT_RETENTION_DAYS", &cfg.RetentionDays); err != nil {
		return cfg, err
	}
	if err := parseEnvInt("VC_EVENT_RETENTION_CRITICAL_DAYS", &cfg.RetentionCriticalDays); err != nil {
		return cfg, err
	}
	if err := parseEnvInt("VC_EVENT_PER_ISSUE_LIMIT", &cfg.PerIssueLimitEvents); err != nil {
		return cfg, err
	}
	if err := parseEnvInt("VC_EVENT_GLOBAL_LIMIT", &cfg.GlobalLimitEvents); err != nil {
		return cfg, err
	}
	if err := parseEnvInt("VC_EVENT_CLEANUP_INTERVAL_HOURS", &cfg.CleanupIntervalHours); err != nil {
		return cfg, err
	}
	if err := parseEnvInt("VC_EVENT_CLEANUP_BATCH_SIZE", &cfg.CleanupBatchSize); err != nil {
		return cfg, err
	}
	if err := parseEnvBool("VC_EVENT_CLEANUP_ENABLED", &cfg.CleanupEnabled); err != nil {
		return cfg, err
	}
	if err := parseEnvString("VC_EVENT_CLEANUP_STRATEGY", &cfg.CleanupStrategy); err != nil {
		return cfg, err
	}
	if err := parseEnvBool("VC_EVENT_CLEANUP_VACUUM", &cfg.CleanupVacuum); err != nil {
		return cfg, err
	}

	// Validate the final configuration
	if err := cfg.Validate(); err != nil {
		return cfg, fmt.Errorf("invalid event retention configuration from environment: %w", err)
	}

	return cfg, nil
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

// parseEnvString parses a string from an environment variable
func parseEnvString(key string, dest *string) error {
	value := os.Getenv(key)
	if value == "" {
		return nil // Use default
	}
	*dest = value
	return nil
}
