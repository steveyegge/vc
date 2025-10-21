package health

import (
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

// HealthConfig represents the health monitoring configuration loaded from YAML.
type HealthConfig struct {
	// Enabled controls whether health monitoring runs automatically
	Enabled bool `yaml:"enabled"`

	// Monitors maps monitor names to their configurations
	Monitors map[string]MonitorConfig `yaml:"monitors"`
}

// MonitorConfig configures a specific health monitor's schedule.
type MonitorConfig struct {
	// Enabled controls whether this specific monitor runs
	Enabled bool `yaml:"enabled"`

	// Schedule configuration
	Schedule ScheduleYAMLConfig `yaml:"schedule"`
}

// ScheduleYAMLConfig represents a schedule in the YAML config file.
// This is converted to ScheduleConfig for internal use.
type ScheduleYAMLConfig struct {
	// Type: "time_based", "event_based", "hybrid", or "manual"
	Type string `yaml:"type"`

	// For time-based schedules
	Interval string `yaml:"interval,omitempty"` // e.g., "24h", "7d"

	// For event-based schedules
	EveryNIssues int `yaml:"every_n_issues,omitempty"`
	EveryNCommits int `yaml:"every_n_commits,omitempty"`

	// For hybrid schedules
	MinInterval string `yaml:"min_interval,omitempty"` // e.g., "1h"
	MaxInterval string `yaml:"max_interval,omitempty"` // e.g., "168h"
}

// LoadConfig loads health monitoring configuration from a YAML file.
func LoadConfig(path string) (*HealthConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config file: %w", err)
	}

	var config HealthConfig
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("parsing YAML: %w", err)
	}

	return &config, nil
}

// ToScheduleConfig converts a YAML schedule config to the internal ScheduleConfig type.
func (c *ScheduleYAMLConfig) ToScheduleConfig() (ScheduleConfig, error) {
	config := ScheduleConfig{}

	// Parse schedule type
	switch c.Type {
	case "time_based":
		config.Type = ScheduleTimeBased
	case "event_based":
		config.Type = ScheduleEventBased
	case "hybrid":
		config.Type = ScheduleHybrid
	case "manual":
		config.Type = ScheduleManual
	default:
		return config, fmt.Errorf("unknown schedule type: %q", c.Type)
	}

	// Parse time-based fields
	if c.Interval != "" {
		interval, err := parseDuration(c.Interval)
		if err != nil {
			return config, fmt.Errorf("invalid interval %q: %w", c.Interval, err)
		}
		config.Interval = interval
	}

	if c.MinInterval != "" {
		minInterval, err := parseDuration(c.MinInterval)
		if err != nil {
			return config, fmt.Errorf("invalid min_interval %q: %w", c.MinInterval, err)
		}
		config.MinInterval = minInterval
	}

	if c.MaxInterval != "" {
		maxInterval, err := parseDuration(c.MaxInterval)
		if err != nil {
			return config, fmt.Errorf("invalid max_interval %q: %w", c.MaxInterval, err)
		}
		config.MaxInterval = maxInterval
	}

	// Parse event-based fields
	if c.EveryNIssues > 0 {
		config.EventTrigger = fmt.Sprintf("every_%d_issues", c.EveryNIssues)
	} else if c.EveryNCommits > 0 {
		config.EventTrigger = fmt.Sprintf("every_%d_commits", c.EveryNCommits)
	}

	return config, nil
}

// parseDuration extends time.ParseDuration to support days and weeks.
func parseDuration(s string) (time.Duration, error) {
	// Handle days (e.g., "7d")
	var days int
	if _, err := fmt.Sscanf(s, "%dd", &days); err == nil {
		return time.Duration(days) * 24 * time.Hour, nil
	}

	// Handle weeks (e.g., "2w")
	var weeks int
	if _, err := fmt.Sscanf(s, "%dw", &weeks); err == nil {
		return time.Duration(weeks) * 7 * 24 * time.Hour, nil
	}

	// Fall back to standard time.ParseDuration (handles h, m, s, ms, etc.)
	return time.ParseDuration(s)
}

// DefaultConfig returns a sensible default health configuration.
func DefaultConfig() *HealthConfig {
	return &HealthConfig{
		Enabled: false, // Opt-in for now
		Monitors: map[string]MonitorConfig{
			"file_size_monitor": {
				Enabled: true,
				Schedule: ScheduleYAMLConfig{
					Type:     "time_based",
					Interval: "24h",
				},
			},
			"cruft_detector": {
				Enabled: true,
				Schedule: ScheduleYAMLConfig{
					Type:     "time_based",
					Interval: "24h",
				},
			},
		},
	}
}

// SaveDefaultConfig writes the default configuration to a file.
func SaveDefaultConfig(path string) error {
	config := DefaultConfig()

	data, err := yaml.Marshal(config)
	if err != nil {
		return fmt.Errorf("marshaling config: %w", err)
	}

	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("writing config file: %w", err)
	}

	return nil
}
