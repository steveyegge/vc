package config

import (
	"fmt"
	"time"
)

// InstanceCleanupConfig holds configuration for executor instance cleanup (vc-33)
type InstanceCleanupConfig struct {
	// CleanupAgeHours is how old stopped instances must be before deletion (in hours)
	// Default: 24, Range: 0-720 (0-30 days)
	// 0 = disable cleanup
	CleanupAgeHours int

	// CleanupKeep is the minimum number of stopped instances to keep
	// Prevents deleting all historical data
	// Default: 10, Range: 0-1000
	// 0 = delete all old instances
	CleanupKeep int
}

// DefaultInstanceCleanupConfig returns the default instance cleanup configuration
//
// These defaults are chosen to:
// - Keep 24 hours of recent stopped instances for debugging
// - Preserve at least 10 stopped instances as historical record
// - Balance between disk space and debugging capability
func DefaultInstanceCleanupConfig() InstanceCleanupConfig {
	return InstanceCleanupConfig{
		CleanupAgeHours: 24,
		CleanupKeep:     10,
	}
}

// Validate checks if the configuration has valid values
func (c InstanceCleanupConfig) Validate() error {
	// Validate CleanupAgeHours
	if c.CleanupAgeHours < 0 || c.CleanupAgeHours > 720 {
		return fmt.Errorf("cleanup_age_hours must be between 0 and 720 (got %d)", c.CleanupAgeHours)
	}

	// Validate CleanupKeep
	if c.CleanupKeep < 0 || c.CleanupKeep > 1000 {
		return fmt.Errorf("cleanup_keep must be between 0 and 1000 (got %d)", c.CleanupKeep)
	}

	return nil
}

// String returns a human-readable representation of the config
func (c InstanceCleanupConfig) String() string {
	return fmt.Sprintf(
		"InstanceCleanupConfig{CleanupAgeHours: %d, CleanupKeep: %d}",
		c.CleanupAgeHours, c.CleanupKeep,
	)
}

// CleanupAge returns the age threshold as a time.Duration
func (c InstanceCleanupConfig) CleanupAge() time.Duration {
	return time.Duration(c.CleanupAgeHours) * time.Hour
}

// InstanceCleanupConfigFromEnv creates an InstanceCleanupConfig from environment variables,
// falling back to defaults (vc-33)
//
// Environment variables:
//   - VC_INSTANCE_CLEANUP_AGE_HOURS: How old stopped instances must be before deletion (default: 24)
//   - VC_INSTANCE_CLEANUP_KEEP: Minimum stopped instances to keep (default: 10)
//
// Returns an error if any environment variable has an invalid value.
func InstanceCleanupConfigFromEnv() (InstanceCleanupConfig, error) {
	cfg := DefaultInstanceCleanupConfig()

	// Parse environment variables with validation
	if err := parseEnvInt("VC_INSTANCE_CLEANUP_AGE_HOURS", &cfg.CleanupAgeHours); err != nil {
		return cfg, err
	}
	if err := parseEnvInt("VC_INSTANCE_CLEANUP_KEEP", &cfg.CleanupKeep); err != nil {
		return cfg, err
	}

	// Validate the final configuration
	if err := cfg.Validate(); err != nil {
		return cfg, fmt.Errorf("invalid instance cleanup configuration from environment: %w", err)
	}

	return cfg, nil
}
