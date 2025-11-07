package cost

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/steveyegge/vc/internal/storage"
)

// BudgetStatus represents the current budget state
type BudgetStatus int

const (
	// BudgetHealthy indicates normal operation - under budget limits
	BudgetHealthy BudgetStatus = iota
	// BudgetWarning indicates approaching budget limits (>80% by default)
	BudgetWarning
	// BudgetExceeded indicates budget limits have been exceeded
	BudgetExceeded
)

// String returns a human-readable string representation of the budget status
func (s BudgetStatus) String() string {
	switch s {
	case BudgetHealthy:
		return "HEALTHY"
	case BudgetWarning:
		return "WARNING"
	case BudgetExceeded:
		return "EXCEEDED"
	default:
		return fmt.Sprintf("UNKNOWN(%d)", s)
	}
}

// BudgetState represents the persisted budget tracking state
type BudgetState struct {
	// Hourly tracking
	HourlyTokensUsed int64     `json:"hourly_tokens_used"` // Total tokens used in current hour
	HourlyCostUsed   float64   `json:"hourly_cost_used"`   // Total cost in current hour
	WindowStartTime  time.Time `json:"window_start_time"`  // When current hour window started

	// Per-issue tracking (map of issue_id -> tokens used)
	IssueTokensUsed map[string]int64 `json:"issue_tokens_used"`

	// Historical data
	TotalTokensUsed int64   `json:"total_tokens_used"` // All-time total tokens
	TotalCostUsed   float64 `json:"total_cost_used"`   // All-time total cost

	// Last updated timestamp
	LastUpdated time.Time `json:"last_updated"`
}

// Tracker tracks AI cost budgets and enforces limits
type Tracker struct {
	config *Config
	state  *BudgetState
	store  storage.Storage // For logging events
	mu     sync.RWMutex    // Protects state

	// Alert tracking (to avoid spamming)
	lastWarningTime  time.Time
	lastExceededTime time.Time
	warningLogged    bool
}

// NewTracker creates a new cost budget tracker
func NewTracker(cfg *Config, store storage.Storage) (*Tracker, error) {
	if cfg == nil {
		return nil, fmt.Errorf("config is required")
	}

	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	t := &Tracker{
		config: cfg,
		store:  store,
		state: &BudgetState{
			WindowStartTime: time.Now(),
			IssueTokensUsed: make(map[string]int64),
			LastUpdated:     time.Now(),
		},
	}

	// Try to load existing state from disk (for restart recovery)
	if cfg.PersistStatePath != "" {
		if err := t.loadState(); err != nil {
			// Log warning but continue with fresh state
			fmt.Printf("Warning: failed to load cost state from %s: %v (starting fresh)\n", cfg.PersistStatePath, err)
		} else {
			fmt.Printf("✓ Loaded cost budget state from %s (total: $%.2f, hourly: %d tokens)\n",
				cfg.PersistStatePath, t.state.TotalCostUsed, t.state.HourlyTokensUsed)
		}
	}

	// Reset hourly budget if window has expired
	t.checkAndResetWindow()

	return t, nil
}

// RecordUsage records token usage for an issue
// Returns the new budget status after recording (as interface{} for interface compatibility)
func (t *Tracker) RecordUsage(ctx context.Context, issueID string, inputTokens, outputTokens int64) (interface{}, error) {
	if !t.config.Enabled {
		return BudgetHealthy, nil
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	// Calculate cost
	totalTokens := inputTokens + outputTokens
	cost := t.calculateCost(inputTokens, outputTokens)

	// Check and reset window if needed
	t.checkAndResetWindow()

	// Update state
	t.state.HourlyTokensUsed += totalTokens
	t.state.HourlyCostUsed += cost
	t.state.TotalTokensUsed += totalTokens
	t.state.TotalCostUsed += cost
	t.state.LastUpdated = time.Now()

	// Update per-issue tracking
	if issueID != "" {
		t.state.IssueTokensUsed[issueID] += totalTokens
	}

	// Persist state to disk
	if err := t.persistState(); err != nil {
		// Log warning but don't fail
		fmt.Printf("Warning: failed to persist cost state: %v\n", err)
	}

	// Determine budget status
	status := t.getBudgetStatusLocked()

	// Log event if status changed or significant usage
	t.logUsageEvent(ctx, issueID, inputTokens, outputTokens, cost, status)

	// Emit alerts if needed
	t.emitAlertsIfNeeded(ctx, status)

	return status, nil
}

// CheckBudget checks if we're within budget limits
// Returns the current budget status without recording usage
func (t *Tracker) CheckBudget() BudgetStatus {
	if !t.config.Enabled {
		return BudgetHealthy
	}

	t.mu.RLock()
	defer t.mu.RUnlock()

	return t.getBudgetStatusLocked()
}

// CanProceed returns true if we can make another AI call without exceeding budget
func (t *Tracker) CanProceed(issueID string) (bool, string) {
	status := t.CheckBudget()

	if status == BudgetExceeded {
		t.mu.RLock()
		defer t.mu.RUnlock()

		// Check which limit was exceeded
		if t.isHourlyTokenLimitExceeded() {
			return false, fmt.Sprintf("hourly token budget exceeded (%d/%d tokens used)",
				t.state.HourlyTokensUsed, t.config.MaxTokensPerHour)
		}

		if t.isHourlyCostLimitExceeded() {
			return false, fmt.Sprintf("hourly cost budget exceeded ($%.2f/$%.2f used)",
				t.state.HourlyCostUsed, t.config.MaxCostPerHour)
		}

		if issueID != "" && t.isIssueLimitExceeded(issueID) {
			issueTokens := t.state.IssueTokensUsed[issueID]
			return false, fmt.Sprintf("per-issue token budget exceeded for %s (%d/%d tokens used)",
				issueID, issueTokens, t.config.MaxTokensPerIssue)
		}
	}

	return true, ""
}

// GetStats returns current budget statistics
func (t *Tracker) GetStats() BudgetStats {
	t.mu.RLock()
	defer t.mu.RUnlock()

	t.checkAndResetWindow()

	return BudgetStats{
		Status:           t.getBudgetStatusLocked(),
		HourlyTokensUsed: t.state.HourlyTokensUsed,
		HourlyCostUsed:   t.state.HourlyCostUsed,
		TotalTokensUsed:  t.state.TotalTokensUsed,
		TotalCostUsed:    t.state.TotalCostUsed,
		WindowStartTime:  t.state.WindowStartTime,
		LastUpdated:      t.state.LastUpdated,
		Config:           *t.config,
	}
}

// BudgetStats contains budget statistics
type BudgetStats struct {
	Status           BudgetStatus  `json:"status"`
	HourlyTokensUsed int64         `json:"hourly_tokens_used"`
	HourlyCostUsed   float64       `json:"hourly_cost_used"`
	TotalTokensUsed  int64         `json:"total_tokens_used"`
	TotalCostUsed    float64       `json:"total_cost_used"`
	WindowStartTime  time.Time     `json:"window_start_time"`
	LastUpdated      time.Time     `json:"last_updated"`
	Config           Config        `json:"config"`
}

// Internal helper methods

// getBudgetStatusLocked returns the current budget status (must be called with lock held)
func (t *Tracker) getBudgetStatusLocked() BudgetStatus {
	// Check if any limit is exceeded
	if t.isHourlyTokenLimitExceeded() || t.isHourlyCostLimitExceeded() {
		return BudgetExceeded
	}

	// Check if approaching limits (warning threshold)
	tokenUsagePercent := float64(t.state.HourlyTokensUsed) / float64(t.config.MaxTokensPerHour)
	costUsagePercent := t.state.HourlyCostUsed / t.config.MaxCostPerHour

	if (t.config.MaxTokensPerHour > 0 && tokenUsagePercent >= t.config.AlertThreshold) ||
		(t.config.MaxCostPerHour > 0 && costUsagePercent >= t.config.AlertThreshold) {
		return BudgetWarning
	}

	return BudgetHealthy
}

// isHourlyTokenLimitExceeded checks if hourly token limit is exceeded
func (t *Tracker) isHourlyTokenLimitExceeded() bool {
	return t.config.MaxTokensPerHour > 0 && t.state.HourlyTokensUsed >= t.config.MaxTokensPerHour
}

// isHourlyCostLimitExceeded checks if hourly cost limit is exceeded
func (t *Tracker) isHourlyCostLimitExceeded() bool {
	return t.config.MaxCostPerHour > 0 && t.state.HourlyCostUsed >= t.config.MaxCostPerHour
}

// isIssueLimitExceeded checks if per-issue token limit is exceeded
func (t *Tracker) isIssueLimitExceeded(issueID string) bool {
	if t.config.MaxTokensPerIssue <= 0 {
		return false
	}
	issueTokens := t.state.IssueTokensUsed[issueID]
	return issueTokens >= t.config.MaxTokensPerIssue
}

// calculateCost calculates the cost in USD for given token usage
func (t *Tracker) calculateCost(inputTokens, outputTokens int64) float64 {
	inputCost := float64(inputTokens) * t.config.InputTokenCost / 1_000_000
	outputCost := float64(outputTokens) * t.config.OutputTokenCost / 1_000_000
	return inputCost + outputCost
}

// checkAndResetWindow checks if the budget window has expired and resets if needed
// MUST be called with mu lock held
func (t *Tracker) checkAndResetWindow() {
	now := time.Now()
	elapsed := now.Sub(t.state.WindowStartTime)

	if elapsed >= t.config.BudgetResetInterval {
		// Reset hourly counters
		t.state.HourlyTokensUsed = 0
		t.state.HourlyCostUsed = 0
		t.state.WindowStartTime = now
		t.warningLogged = false // Reset warning flag on new window
	}
}

// persistState saves the budget state to disk
func (t *Tracker) persistState() error {
	if t.config.PersistStatePath == "" {
		return nil // Persistence disabled
	}

	data, err := json.MarshalIndent(t.state, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal state: %w", err)
	}

	if err := os.WriteFile(t.config.PersistStatePath, data, 0644); err != nil {
		return fmt.Errorf("failed to write state file: %w", err)
	}

	return nil
}

// loadState loads the budget state from disk
func (t *Tracker) loadState() error {
	if t.config.PersistStatePath == "" {
		return nil // Persistence disabled
	}

	data, err := os.ReadFile(t.config.PersistStatePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // No state file yet, start fresh
		}
		return fmt.Errorf("failed to read state file: %w", err)
	}

	var state BudgetState
	if err := json.Unmarshal(data, &state); err != nil {
		return fmt.Errorf("failed to unmarshal state: %w", err)
	}

	// Ensure map is initialized
	if state.IssueTokensUsed == nil {
		state.IssueTokensUsed = make(map[string]int64)
	}

	t.state = &state
	return nil
}

// logUsageEvent logs a cost usage event to issue comments (if issue ID provided)
func (t *Tracker) logUsageEvent(ctx context.Context, issueID string, inputTokens, outputTokens int64, cost float64, status BudgetStatus) {
	if t.store == nil || issueID == "" {
		return // No store or no issue to attach comment to
	}

	totalTokens := inputTokens + outputTokens
	message := fmt.Sprintf("AI cost: %d tokens ($%.4f), hourly: %d tokens ($%.2f), status: %s",
		totalTokens, cost, t.state.HourlyTokensUsed, t.state.HourlyCostUsed, status.String())

	// Log as comment to the issue (best-effort)
	// Ignore errors - this is best-effort logging, not critical to execution
	_ = t.store.AddComment(ctx, issueID, "cost-tracker", message)
}

// emitAlertsIfNeeded emits alerts if budget thresholds are crossed
func (t *Tracker) emitAlertsIfNeeded(ctx context.Context, status BudgetStatus) {
	now := time.Now()

	switch status {
	case BudgetWarning:
		// Only log warning once per window and throttle to once per 5 minutes
		if !t.warningLogged && now.Sub(t.lastWarningTime) > 5*time.Minute {
			tokenPercent := float64(t.state.HourlyTokensUsed) / float64(t.config.MaxTokensPerHour) * 100
			costPercent := t.state.HourlyCostUsed / t.config.MaxCostPerHour * 100

			fmt.Printf("⚠️  Cost budget warning: %.0f%% tokens used (%.0f%% cost)\n",
				tokenPercent, costPercent)

			t.lastWarningTime = now
			t.warningLogged = true

			// Note: Budget alerts are logged to stdout only
			// Event-level logging would require passing an event logger interface
		}

	case BudgetExceeded:
		// Throttle exceeded alerts to once per 5 minutes
		if now.Sub(t.lastExceededTime) > 5*time.Minute {
			fmt.Printf("🚨 Cost budget EXCEEDED: pausing new AI calls until budget resets\n")
			fmt.Printf("   Hourly usage: %d/%d tokens ($%.2f/$%.2f)\n",
				t.state.HourlyTokensUsed, t.config.MaxTokensPerHour,
				t.state.HourlyCostUsed, t.config.MaxCostPerHour)

			resetTime := t.state.WindowStartTime.Add(t.config.BudgetResetInterval)
			timeUntilReset := time.Until(resetTime)
			fmt.Printf("   Budget resets in: %v\n", timeUntilReset.Round(time.Minute))

			t.lastExceededTime = now

			// Note: Budget alerts are logged to stdout only
			// Event-level logging would require passing an event logger interface
		}
	}
}
