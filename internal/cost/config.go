package cost

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

// Config holds cost budgeting configuration
type Config struct {
	// MaxTokensPerHour is the maximum number of tokens (input + output) allowed per hour
	// 0 = unlimited
	// Default: 100000 (conservative limit to prevent runaway costs)
	MaxTokensPerHour int64 `json:"max_tokens_per_hour"`

	// MaxTokensPerIssue is the maximum number of tokens allowed per issue
	// 0 = unlimited
	// Default: 50000 (enough for most issues, prevents single issue burning budget)
	MaxTokensPerIssue int64 `json:"max_tokens_per_issue"`

	// MaxCostPerHour is the maximum cost in USD allowed per hour
	// 0.0 = unlimited (use token limits instead)
	// Default: 1.50 (conservative for Claude Sonnet 4.5)
	MaxCostPerHour float64 `json:"max_cost_per_hour"`

	// AlertThreshold is the percentage of budget usage that triggers alerts
	// Default: 0.80 (80%)
	AlertThreshold float64 `json:"alert_threshold"`

	// BudgetResetInterval is how often the hourly budget resets
	// Default: 1 hour
	BudgetResetInterval time.Duration `json:"budget_reset_interval"`

	// PersistStatePath is where budget state is persisted (for restart recovery)
	// Default: .beads/cost_state.json
	PersistStatePath string `json:"persist_state_path"`

	// Enabled controls whether cost budgeting is active
	// Default: true
	Enabled bool `json:"enabled"`

	// InputTokenCost is the cost per 1M input tokens (in USD)
	// Default: $3.00 for Claude Sonnet 4.5
	InputTokenCost float64 `json:"input_token_cost"`

	// OutputTokenCost is the cost per 1M output tokens (in USD)
	// Default: $15.00 for Claude Sonnet 4.5
	OutputTokenCost float64 `json:"output_token_cost"`

	// === Quota Monitoring (vc-7e21) ===

	// EnableQuotaMonitoring enables quota tracking and predictive alerting
	// Default: true
	EnableQuotaMonitoring bool `json:"enable_quota_monitoring"`

	// QuotaSnapshotInterval controls how often to capture usage snapshots
	// Default: 5 minutes
	QuotaSnapshotInterval time.Duration `json:"quota_snapshot_interval"`

	// QuotaAlertYellowThreshold is time-to-limit that triggers YELLOW alert (warning)
	// Default: 30 minutes
	QuotaAlertYellowThreshold time.Duration `json:"quota_alert_yellow_threshold"`

	// QuotaAlertOrangeThreshold is time-to-limit that triggers ORANGE alert (urgent)
	// Default: 15 minutes
	QuotaAlertOrangeThreshold time.Duration `json:"quota_alert_orange_threshold"`

	// QuotaAlertRedThreshold is time-to-limit that triggers RED alert (critical)
	// Default: 5 minutes
	QuotaAlertRedThreshold time.Duration `json:"quota_alert_red_threshold"`

	// QuotaRetentionDays controls how long to keep historical snapshot data
	// Default: 30 days
	QuotaRetentionDays int `json:"quota_retention_days"`

	// EnableQuotaCrisisAutoIssue auto-creates P0 issues on RED alerts
	// Default: true
	EnableQuotaCrisisAutoIssue bool `json:"enable_quota_crisis_auto_issue"`
}

// DefaultConfig returns default cost budgeting configuration
func DefaultConfig() *Config {
	return &Config{
		Enabled:              true,
		MaxTokensPerHour:     100000, // Conservative default
		MaxTokensPerIssue:    50000,  // Half of hourly budget
		MaxCostPerHour:       5.00,   // $5/hour = ~$120/day max
		AlertThreshold:       0.80,   // Alert at 80% of budget
		BudgetResetInterval:  time.Hour,
		PersistStatePath:     ".beads/cost_state.json",
		InputTokenCost:       3.00,  // $3 per 1M input tokens (Claude Sonnet 4.5)
		OutputTokenCost:      15.00, // $15 per 1M output tokens (Claude Sonnet 4.5)
		// Quota monitoring (vc-7e21)
		EnableQuotaMonitoring:       true,
		QuotaSnapshotInterval:       5 * time.Minute,
		QuotaAlertYellowThreshold:   30 * time.Minute,
		QuotaAlertOrangeThreshold:   15 * time.Minute,
		QuotaAlertRedThreshold:      5 * time.Minute,
		QuotaRetentionDays:          30,
		EnableQuotaCrisisAutoIssue:  true,
	}
}

// LoadFromEnv loads cost configuration from environment variables
// Environment variables override default values
// Prefix: VC_COST_
func LoadFromEnv() *Config {
	cfg := DefaultConfig()

	if val := os.Getenv("VC_COST_ENABLED"); val != "" {
		cfg.Enabled = parseBool(val)
	}

	if val := os.Getenv("VC_COST_MAX_TOKENS_PER_HOUR"); val != "" {
		if tokens, err := strconv.ParseInt(val, 10, 64); err == nil && tokens >= 0 {
			cfg.MaxTokensPerHour = tokens
		}
	}

	if val := os.Getenv("VC_COST_MAX_TOKENS_PER_ISSUE"); val != "" {
		if tokens, err := strconv.ParseInt(val, 10, 64); err == nil && tokens >= 0 {
			cfg.MaxTokensPerIssue = tokens
		}
	}

	if val := os.Getenv("VC_COST_MAX_COST_PER_HOUR"); val != "" {
		if cost, err := strconv.ParseFloat(val, 64); err == nil && cost >= 0 {
			cfg.MaxCostPerHour = cost
		}
	}

	if val := os.Getenv("VC_COST_ALERT_THRESHOLD"); val != "" {
		if threshold, err := strconv.ParseFloat(val, 64); err == nil && threshold > 0 && threshold <= 1.0 {
			cfg.AlertThreshold = threshold
		}
	}

	if val := os.Getenv("VC_COST_BUDGET_RESET_INTERVAL"); val != "" {
		if duration, err := time.ParseDuration(val); err == nil && duration > 0 {
			cfg.BudgetResetInterval = duration
		}
	}

	if val := os.Getenv("VC_COST_PERSIST_STATE_PATH"); val != "" {
		cfg.PersistStatePath = val
	}

	if val := os.Getenv("VC_COST_INPUT_TOKEN_COST"); val != "" {
		if cost, err := strconv.ParseFloat(val, 64); err == nil && cost >= 0 {
			cfg.InputTokenCost = cost
		}
	}

	if val := os.Getenv("VC_COST_OUTPUT_TOKEN_COST"); val != "" {
		if cost, err := strconv.ParseFloat(val, 64); err == nil && cost >= 0 {
			cfg.OutputTokenCost = cost
		}
	}

	// Quota monitoring config (vc-7e21)
	if val := os.Getenv("VC_ENABLE_QUOTA_MONITORING"); val != "" {
		cfg.EnableQuotaMonitoring = parseBool(val)
	}

	if val := os.Getenv("VC_QUOTA_SNAPSHOT_INTERVAL"); val != "" {
		if duration, err := time.ParseDuration(val); err == nil && duration > 0 {
			cfg.QuotaSnapshotInterval = duration
		}
	}

	if val := os.Getenv("VC_QUOTA_ALERT_YELLOW"); val != "" {
		if duration, err := time.ParseDuration(val); err == nil && duration > 0 {
			cfg.QuotaAlertYellowThreshold = duration
		}
	}

	if val := os.Getenv("VC_QUOTA_ALERT_ORANGE"); val != "" {
		if duration, err := time.ParseDuration(val); err == nil && duration > 0 {
			cfg.QuotaAlertOrangeThreshold = duration
		}
	}

	if val := os.Getenv("VC_QUOTA_ALERT_RED"); val != "" {
		if duration, err := time.ParseDuration(val); err == nil && duration > 0 {
			cfg.QuotaAlertRedThreshold = duration
		}
	}

	if val := os.Getenv("VC_QUOTA_RETENTION_DAYS"); val != "" {
		if days, err := strconv.Atoi(val); err == nil && days > 0 {
			cfg.QuotaRetentionDays = days
		}
	}

	if val := os.Getenv("VC_QUOTA_AUTO_CREATE_CRISIS_ISSUE"); val != "" {
		cfg.EnableQuotaCrisisAutoIssue = parseBool(val)
	}

	// Validate configuration
	if err := cfg.Validate(); err != nil {
		fmt.Printf("Warning: invalid cost config from environment: %v (using defaults)\n", err)
		return DefaultConfig()
	}

	return cfg
}

// Validate checks that the configuration has safe and reasonable values
func (c *Config) Validate() error {
	if c.MaxTokensPerHour < 0 {
		return fmt.Errorf("max_tokens_per_hour must be non-negative, got %d", c.MaxTokensPerHour)
	}

	if c.MaxTokensPerIssue < 0 {
		return fmt.Errorf("max_tokens_per_issue must be non-negative, got %d", c.MaxTokensPerIssue)
	}

	if c.MaxCostPerHour < 0 {
		return fmt.Errorf("max_cost_per_hour must be non-negative, got %.2f", c.MaxCostPerHour)
	}

	if c.AlertThreshold <= 0 || c.AlertThreshold > 1.0 {
		return fmt.Errorf("alert_threshold must be between 0 and 1, got %.2f", c.AlertThreshold)
	}

	if c.BudgetResetInterval <= 0 {
		return fmt.Errorf("budget_reset_interval must be positive, got %v", c.BudgetResetInterval)
	}

	if c.InputTokenCost < 0 {
		return fmt.Errorf("input_token_cost must be non-negative, got %.2f", c.InputTokenCost)
	}

	if c.OutputTokenCost < 0 {
		return fmt.Errorf("output_token_cost must be non-negative, got %.2f", c.OutputTokenCost)
	}

	// Validate quota monitoring config (vc-7e21)
	if c.QuotaSnapshotInterval <= 0 {
		return fmt.Errorf("quota_snapshot_interval must be positive, got %v", c.QuotaSnapshotInterval)
	}

	if c.QuotaAlertYellowThreshold <= 0 {
		return fmt.Errorf("quota_alert_yellow_threshold must be positive, got %v", c.QuotaAlertYellowThreshold)
	}

	if c.QuotaAlertOrangeThreshold <= 0 {
		return fmt.Errorf("quota_alert_orange_threshold must be positive, got %v", c.QuotaAlertOrangeThreshold)
	}

	if c.QuotaAlertRedThreshold <= 0 {
		return fmt.Errorf("quota_alert_red_threshold must be positive, got %v", c.QuotaAlertRedThreshold)
	}

	// Validate alert threshold ordering (YELLOW > ORANGE > RED)
	if c.QuotaAlertOrangeThreshold >= c.QuotaAlertYellowThreshold {
		return fmt.Errorf("quota_alert_orange_threshold (%v) must be < yellow (%v)", c.QuotaAlertOrangeThreshold, c.QuotaAlertYellowThreshold)
	}

	if c.QuotaAlertRedThreshold >= c.QuotaAlertOrangeThreshold {
		return fmt.Errorf("quota_alert_red_threshold (%v) must be < orange (%v)", c.QuotaAlertRedThreshold, c.QuotaAlertOrangeThreshold)
	}

	if c.QuotaRetentionDays <= 0 {
		return fmt.Errorf("quota_retention_days must be positive, got %d", c.QuotaRetentionDays)
	}

	return nil
}

// parseBool parses a boolean string
func parseBool(val string) bool {
	switch val {
	case "true", "1", "yes", "on":
		return true
	case "false", "0", "no", "off":
		return false
	default:
		return true
	}
}
