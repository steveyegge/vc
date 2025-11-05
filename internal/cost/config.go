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
}

// DefaultConfig returns default cost budgeting configuration
func DefaultConfig() *Config {
	return &Config{
		Enabled:              true,
		MaxTokensPerHour:     100000, // Conservative default
		MaxTokensPerIssue:    50000,  // Half of hourly budget
		MaxCostPerHour:       1.50,   // ~$1.50/hour = ~$36/day max
		AlertThreshold:       0.80,   // Alert at 80% of budget
		BudgetResetInterval:  time.Hour,
		PersistStatePath:     ".beads/cost_state.json",
		InputTokenCost:       3.00,  // $3 per 1M input tokens (Claude Sonnet 4.5)
		OutputTokenCost:      15.00, // $15 per 1M output tokens (Claude Sonnet 4.5)
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
