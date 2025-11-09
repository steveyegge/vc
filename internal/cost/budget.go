package cost

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/steveyegge/vc/internal/events"
	"github.com/steveyegge/vc/internal/storage"
	"github.com/steveyegge/vc/internal/types"
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

	// Quota monitoring (vc-7e21)
	lastSnapshotTime time.Time
	lastAlertLevel   AlertLevel
	lastAlertTime    time.Time
	snapshots        []QuotaSnapshot // Recent snapshots for burn rate calculation
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
	t.emitAlertsIfNeeded(status)

	// Capture snapshot if quota monitoring enabled (vc-7e21)
	if t.config.EnableQuotaMonitoring {
		t.captureSnapshotIfNeeded(ctx)
	}

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

// ======================================================================
// QUOTA MONITORING TYPES (vc-7e21)
// ======================================================================

// AlertLevel represents the urgency level of a quota alert
type AlertLevel int

const (
	// AlertGreen indicates healthy state - >30min until limit
	AlertGreen AlertLevel = iota
	// AlertYellow indicates warning - 15-30min until limit
	AlertYellow
	// AlertOrange indicates urgent - 5-15min until limit
	AlertOrange
	// AlertRed indicates critical - <5min until limit
	AlertRed
)

// String returns a human-readable string representation of the alert level
func (a AlertLevel) String() string {
	switch a {
	case AlertGreen:
		return "GREEN"
	case AlertYellow:
		return "YELLOW"
	case AlertOrange:
		return "ORANGE"
	case AlertRed:
		return "RED"
	default:
		return fmt.Sprintf("UNKNOWN(%d)", a)
	}
}

// BurnRate represents the rate of quota consumption and time-to-limit prediction
type BurnRate struct {
	TokensPerMinute      float64       `json:"tokens_per_minute"`
	CostPerMinute        float64       `json:"cost_per_minute"`
	EstimatedTimeToLimit time.Duration `json:"estimated_time_to_limit"`
	Confidence           float64       `json:"confidence"` // 0.0-1.0, based on sample size
	AlertLevel           AlertLevel    `json:"alert_level"`
}

// QuotaSnapshot represents a point-in-time snapshot of quota usage
type QuotaSnapshot struct {
	ID               string       `json:"id"`
	Timestamp        time.Time    `json:"timestamp"`
	WindowStart      time.Time    `json:"window_start"`
	HourlyTokensUsed int64        `json:"hourly_tokens_used"`
	HourlyCostUsed   float64      `json:"hourly_cost_used"`
	TotalTokensUsed  int64        `json:"total_tokens_used"`
	TotalCostUsed    float64      `json:"total_cost_used"`
	BudgetStatus     BudgetStatus `json:"budget_status"`
	IssuesWorked     int          `json:"issues_worked"` // Count of unique issues in this window
}

// QuotaOperation represents a single AI operation for cost attribution
type QuotaOperation struct {
	ID            string    `json:"id"`
	Timestamp     time.Time `json:"timestamp"`
	IssueID       string    `json:"issue_id"`        // May be empty for system operations
	OperationType string    `json:"operation_type"`  // "assessment", "analysis", "deduplication", etc.
	Model         string    `json:"model"`           // "sonnet", "haiku", "opus"
	InputTokens   int64     `json:"input_tokens"`
	OutputTokens  int64     `json:"output_tokens"`
	Cost          float64   `json:"cost"`
	DurationMs    int64     `json:"duration_ms"`     // How long the operation took
}

// QuotaAlert represents a predictive alert event
type QuotaAlert struct {
	Level              AlertLevel    `json:"level"`
	Message            string        `json:"message"`
	BurnRate           BurnRate      `json:"burn_rate"`
	CurrentUsage       BudgetStats   `json:"current_usage"`
	RecommendedAction  string        `json:"recommended_action"`
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
	// Intentionally ignoring error - cost tracking comments are informational only
	// and should not fail the execution if comment creation fails
	_ = t.store.AddComment(ctx, issueID, "cost-tracker", message)
}

// emitAlertsIfNeeded emits alerts if budget thresholds are crossed
func (t *Tracker) emitAlertsIfNeeded(status BudgetStatus) {
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

// ======================================================================
// QUOTA MONITORING METHODS (vc-7e21)
// ======================================================================

// captureSnapshotIfNeeded captures a quota usage snapshot if enough time has elapsed
// Must be called with mu lock held
func (t *Tracker) captureSnapshotIfNeeded(ctx context.Context) {
	now := time.Now()

	// Check if it's time for a snapshot
	if now.Sub(t.lastSnapshotTime) < t.config.QuotaSnapshotInterval {
		return // Not time yet
	}

	// Count unique issues worked in this window
	issuesWorked := len(t.state.IssueTokensUsed)

	// Create snapshot
	snapshot := QuotaSnapshot{
		ID:               uuid.New().String(),
		Timestamp:        now,
		WindowStart:      t.state.WindowStartTime,
		HourlyTokensUsed: t.state.HourlyTokensUsed,
		HourlyCostUsed:   t.state.HourlyCostUsed,
		TotalTokensUsed:  t.state.TotalTokensUsed,
		TotalCostUsed:    t.state.TotalCostUsed,
		BudgetStatus:     t.getBudgetStatusLocked(),
		IssuesWorked:     issuesWorked,
	}

	// Add to recent snapshots (keep last 20 for burn rate calculation)
	t.snapshots = append(t.snapshots, snapshot)
	if len(t.snapshots) > 20 {
		t.snapshots = t.snapshots[len(t.snapshots)-20:]
	}

	t.lastSnapshotTime = now

	// Store snapshot in database (best-effort, don't fail on error)
	if t.store != nil {
		_ = t.storeSnapshot(ctx, snapshot)
	}

	// Calculate burn rate and check for alerts
	burnRate := t.calculateBurnRate()
	if burnRate.Confidence > 0.5 { // Only alert if we have enough confidence
		t.checkAndEmitQuotaAlert(ctx, burnRate)
	}
}

// calculateBurnRate calculates the current burn rate and predicts time to limit
// Must be called with mu lock held
func (t *Tracker) calculateBurnRate() BurnRate {
	// Need at least 3 snapshots for meaningful calculation
	if len(t.snapshots) < 3 {
		return BurnRate{
			Confidence: 0.0,
			AlertLevel: AlertGreen,
		}
	}

	// Use last 15 minutes of snapshots (3 snapshots at 5-minute intervals)
	sampleWindow := 15 * time.Minute
	cutoffTime := time.Now().Add(-sampleWindow)

	var recentSnapshots []QuotaSnapshot
	for _, s := range t.snapshots {
		if s.Timestamp.After(cutoffTime) {
			recentSnapshots = append(recentSnapshots, s)
		}
	}

	if len(recentSnapshots) < 2 {
		return BurnRate{
			Confidence: 0.0,
			AlertLevel: AlertGreen,
		}
	}

	// Calculate token and cost burn rates using linear regression
	oldest := recentSnapshots[0]
	newest := recentSnapshots[len(recentSnapshots)-1]

	timeDelta := newest.Timestamp.Sub(oldest.Timestamp).Minutes()
	if timeDelta <= 0 {
		return BurnRate{
			Confidence: 0.0,
			AlertLevel: AlertGreen,
		}
	}

	tokenDelta := newest.HourlyTokensUsed - oldest.HourlyTokensUsed
	costDelta := newest.HourlyCostUsed - oldest.HourlyCostUsed

	tokensPerMinute := float64(tokenDelta) / timeDelta
	costPerMinute := costDelta / timeDelta

	// Calculate time to each limit
	var timeToLimit time.Duration

	if t.config.MaxTokensPerHour > 0 && tokensPerMinute > 0 {
		tokensRemaining := t.config.MaxTokensPerHour - t.state.HourlyTokensUsed
		tokenTimeToLimit := time.Duration(float64(tokensRemaining)/tokensPerMinute) * time.Minute

		if timeToLimit == 0 || tokenTimeToLimit < timeToLimit {
			timeToLimit = tokenTimeToLimit
		}
	}

	if t.config.MaxCostPerHour > 0 && costPerMinute > 0 {
		costRemaining := t.config.MaxCostPerHour - t.state.HourlyCostUsed
		costTimeToLimit := time.Duration(costRemaining/costPerMinute) * time.Minute

		if timeToLimit == 0 || costTimeToLimit < timeToLimit {
			timeToLimit = costTimeToLimit
		}
	}

	// If no burn, time to limit is infinite (return max duration)
	if timeToLimit == 0 {
		timeToLimit = 24 * time.Hour // Arbitrarily large
	}

	// Calculate confidence based on sample size and consistency
	// More samples = higher confidence
	// Confidence ranges from 0.0 to 1.0
	baseConfidence := float64(len(recentSnapshots)) / 5.0 // Max at 5 samples
	if baseConfidence > 1.0 {
		baseConfidence = 1.0
	}

	// Determine alert level based on time to limit
	var alertLevel AlertLevel
	if timeToLimit < t.config.QuotaAlertRedThreshold {
		alertLevel = AlertRed
	} else if timeToLimit < t.config.QuotaAlertOrangeThreshold {
		alertLevel = AlertOrange
	} else if timeToLimit < t.config.QuotaAlertYellowThreshold {
		alertLevel = AlertYellow
	} else {
		alertLevel = AlertGreen
	}

	return BurnRate{
		TokensPerMinute:      tokensPerMinute,
		CostPerMinute:        costPerMinute,
		EstimatedTimeToLimit: timeToLimit,
		Confidence:           baseConfidence,
		AlertLevel:           alertLevel,
	}
}

// checkAndEmitQuotaAlert checks if we should emit a quota alert and emits it if needed
// Must be called with mu lock held
func (t *Tracker) checkAndEmitQuotaAlert(ctx context.Context, burnRate BurnRate) {
	now := time.Now()

	// Only escalate alerts, never de-escalate (avoid alert spam)
	if burnRate.AlertLevel <= t.lastAlertLevel && now.Sub(t.lastAlertTime) < 5*time.Minute {
		return // Same or lower level within throttle window
	}

	// Skip GREEN alerts (normal operation)
	if burnRate.AlertLevel == AlertGreen {
		return
	}

	// Create alert message
	var message string
	var recommendedAction string

	switch burnRate.AlertLevel {
	case AlertYellow:
		message = fmt.Sprintf("⚠️  Quota approaching limit: ~%.0f minutes remaining at current burn rate", burnRate.EstimatedTimeToLimit.Minutes())
		recommendedAction = "Monitor usage. Consider reducing AI operations or increasing quota limits."
	case AlertOrange:
		message = fmt.Sprintf("🔶 Quota limit imminent: ~%.0f minutes remaining at current burn rate", burnRate.EstimatedTimeToLimit.Minutes())
		recommendedAction = "Urgent: Reduce AI operations or risk hitting quota limit soon."
	case AlertRed:
		message = fmt.Sprintf("🚨 CRITICAL: Quota exhaustion in ~%.0f minutes at current burn rate", burnRate.EstimatedTimeToLimit.Minutes())
		recommendedAction = "IMMEDIATE ACTION REQUIRED: Stop non-essential AI operations. Quota crisis issue will be auto-created."
	}

	// Log to console
	fmt.Printf("\n%s\n", message)
	fmt.Printf("   Burn rate: %.0f tokens/min ($%.4f/min)\n", burnRate.TokensPerMinute, burnRate.CostPerMinute)
	fmt.Printf("   Current usage: %d/%d tokens ($%.2f/$%.2f)\n",
		t.state.HourlyTokensUsed, t.config.MaxTokensPerHour,
		t.state.HourlyCostUsed, t.config.MaxCostPerHour)
	fmt.Printf("   Recommended: %s\n\n", recommendedAction)

	// Create alert event
	alert := QuotaAlert{
		Level:             burnRate.AlertLevel,
		Message:           message,
		BurnRate:          burnRate,
		CurrentUsage:      t.GetStats(),
		RecommendedAction: recommendedAction,
	}

	// Log alert event to activity feed (best-effort)
	if t.store != nil {
		_ = t.logQuotaAlert(ctx, alert)
	}

	// Auto-create P0 issue on RED alert (if enabled)
	if burnRate.AlertLevel == AlertRed && t.config.EnableQuotaCrisisAutoIssue {
		_ = t.createQuotaCrisisIssue(ctx, alert)
	}

	t.lastAlertLevel = burnRate.AlertLevel
	t.lastAlertTime = now
}

// storeSnapshot stores a quota snapshot in the database
func (t *Tracker) storeSnapshot(ctx context.Context, snapshot QuotaSnapshot) error {
	if t.store == nil {
		return nil // No storage configured
	}

	// Convert to storage layer type
	storageSnapshot := &struct {
		ID               string
		Timestamp        time.Time
		WindowStart      time.Time
		HourlyTokensUsed int64
		HourlyCostUsed   float64
		TotalTokensUsed  int64
		TotalCostUsed    float64
		BudgetStatus     string
		IssuesWorked     int
	}{
		ID:               snapshot.ID,
		Timestamp:        snapshot.Timestamp,
		WindowStart:      snapshot.WindowStart,
		HourlyTokensUsed: snapshot.HourlyTokensUsed,
		HourlyCostUsed:   snapshot.HourlyCostUsed,
		TotalTokensUsed:  snapshot.TotalTokensUsed,
		TotalCostUsed:    snapshot.TotalCostUsed,
		BudgetStatus:     snapshot.BudgetStatus.String(),
		IssuesWorked:     snapshot.IssuesWorked,
	}

	// Use reflection to call StoreQuotaSnapshot on the storage interface
	// This is needed because the storage interface doesn't know about quota types
	type quotaStore interface {
		StoreQuotaSnapshot(ctx context.Context, snapshot interface{}) error
	}

	if qs, ok := t.store.(quotaStore); ok {
		return qs.StoreQuotaSnapshot(ctx, storageSnapshot)
	}

	return nil // Storage doesn't support quota monitoring yet
}

// logQuotaAlert logs a quota alert event to the activity feed
func (t *Tracker) logQuotaAlert(ctx context.Context, alert QuotaAlert) error {
	if t.store == nil {
		return nil // No storage configured
	}

	// Determine event severity based on alert level
	var severity events.EventSeverity
	switch alert.Level {
	case AlertYellow:
		severity = events.SeverityWarning
	case AlertOrange:
		severity = events.SeverityError
	case AlertRed:
		severity = events.SeverityCritical
	default:
		severity = events.SeverityInfo
	}

	// Create structured data for the event
	data := map[string]interface{}{
		"alert_level":                alert.Level.String(),
		"tokens_per_minute":          alert.BurnRate.TokensPerMinute,
		"cost_per_minute":            alert.BurnRate.CostPerMinute,
		"estimated_minutes_to_limit": alert.BurnRate.EstimatedTimeToLimit.Minutes(),
		"confidence":                 alert.BurnRate.Confidence,
		"current_tokens_used":        alert.CurrentUsage.HourlyTokensUsed,
		"token_limit":                alert.CurrentUsage.Config.MaxTokensPerHour,
		"current_cost_used":          alert.CurrentUsage.HourlyCostUsed,
		"cost_limit":                 alert.CurrentUsage.Config.MaxCostPerHour,
		"recommended_action":         alert.RecommendedAction,
	}

	// Create the event
	event := &events.AgentEvent{
		ID:         uuid.New().String(),
		Type:       events.EventTypeQuotaAlert,
		Timestamp:  time.Now(),
		IssueID:    "SYSTEM", // System-level event (not tied to a specific issue)
		ExecutorID: "",       // No executor ID (cost tracker is global)
		AgentID:    "",       // Not produced by a coding agent
		Severity:   severity,
		Message:    alert.Message,
		Data:       data,
		SourceLine: 0, // Not applicable for cost tracker events
	}

	// Store the event
	return t.store.StoreAgentEvent(ctx, event)
}

// createQuotaCrisisIssue auto-creates a P0 quota crisis issue
func (t *Tracker) createQuotaCrisisIssue(ctx context.Context, alert QuotaAlert) error {
	if t.store == nil {
		return nil // No storage configured
	}

	// Check if a quota crisis issue already exists (avoid duplicates)
	existingIssues, err := t.store.GetIssuesByLabel(ctx, "quota-crisis")
	if err != nil {
		return fmt.Errorf("failed to check for existing quota crisis issues: %w", err)
	}

	// Count open quota crisis issues
	openCrisisCount := 0
	for _, issue := range existingIssues {
		if issue.Status == types.StatusOpen || issue.Status == types.StatusInProgress {
			openCrisisCount++
		}
	}

	// If there are already open quota crisis issues, don't create another
	if openCrisisCount > 0 {
		fmt.Printf("Quota crisis issue already exists (found %d open issues), skipping auto-creation\n", openCrisisCount)
		return nil
	}

	// Create detailed description with actionable information
	description := fmt.Sprintf(`CRITICAL: Quota exhaustion predicted in ~%.0f minutes at current burn rate.

**Current Usage:**
- Tokens: %d / %d (%.1f%%)
- Cost: $%.2f / $%.2f (%.1f%%)

**Burn Rate:**
- Tokens: %.0f tokens/minute
- Cost: $%.4f/minute
- Confidence: %.0f%%

**Immediate Actions Required:**
1. Review current executor activity and pause non-critical missions
2. Check for any runaway loops or inefficient operations
3. Consider increasing quota limits if justified
4. Investigate root cause of high burn rate

**Recommended Action:**
%s

This issue was auto-created by the quota monitoring system (vc-7e21).
`,
		alert.BurnRate.EstimatedTimeToLimit.Minutes(),
		alert.CurrentUsage.HourlyTokensUsed, alert.CurrentUsage.Config.MaxTokensPerHour,
		float64(alert.CurrentUsage.HourlyTokensUsed)/float64(alert.CurrentUsage.Config.MaxTokensPerHour)*100,
		alert.CurrentUsage.HourlyCostUsed, alert.CurrentUsage.Config.MaxCostPerHour,
		alert.CurrentUsage.HourlyCostUsed/alert.CurrentUsage.Config.MaxCostPerHour*100,
		alert.BurnRate.TokensPerMinute,
		alert.BurnRate.CostPerMinute,
		alert.BurnRate.Confidence*100,
		alert.RecommendedAction,
	)

	// Create acceptance criteria
	acceptanceCriteria := `- [ ] Burn rate reduced to sustainable level (<80% of quota)
- [ ] Root cause identified and addressed
- [ ] Quota limits reviewed and adjusted if needed
- [ ] Monitoring shows stable usage for 1+ hour`

	// Create the issue
	issue := &types.Issue{
		Title:              fmt.Sprintf("QUOTA CRISIS: Exhaustion predicted in %.0fm", alert.BurnRate.EstimatedTimeToLimit.Minutes()),
		IssueType:          types.TypeBug, // Crisis is a type of urgent bug
		Priority:           0,              // P0 - highest priority
		Status:             types.StatusOpen,
		Description:        description,
		AcceptanceCriteria: acceptanceCriteria,
	}

	// Create the issue
	if err := t.store.CreateIssue(ctx, issue, "quota-monitor"); err != nil {
		return fmt.Errorf("failed to create quota crisis issue: %w", err)
	}

	// Add label to mark as quota crisis
	if err := t.store.AddLabel(ctx, issue.ID, "quota-crisis", "quota-monitor"); err != nil {
		// Log warning but don't fail (issue was created successfully)
		fmt.Printf("Warning: failed to add quota-crisis label to %s: %v\n", issue.ID, err)
	}

	// Add label to prevent auto-claiming (requires human intervention)
	if err := t.store.AddLabel(ctx, issue.ID, "no-auto-claim", "quota-monitor"); err != nil {
		// Log warning but don't fail
		fmt.Printf("Warning: failed to add no-auto-claim label to %s: %v\n", issue.ID, err)
	}

	fmt.Printf("🚨 Created quota crisis issue %s (P0)\n", issue.ID)

	return nil
}

// RecordOperation records a quota operation for cost attribution (vc-7e21)
// Accepts either QuotaOperation or map[string]interface{} (for avoiding circular dependencies)
func (t *Tracker) RecordOperation(ctx context.Context, opInterface interface{}) error {
	if !t.config.EnableQuotaMonitoring {
		return nil // Monitoring disabled
	}

	// Convert interface to QuotaOperation
	var op QuotaOperation

	switch v := opInterface.(type) {
	case QuotaOperation:
		op = v
	case map[string]interface{}:
		// Extract fields from map (sent by AI supervisor to avoid circular dependency)
		if issueID, ok := v["issue_id"].(string); ok {
			op.IssueID = issueID
		}
		if opType, ok := v["operation_type"].(string); ok {
			op.OperationType = opType
		}
		if model, ok := v["model"].(string); ok {
			op.Model = model
		}
		if inputTokens, ok := v["input_tokens"].(int64); ok {
			op.InputTokens = inputTokens
		}
		if outputTokens, ok := v["output_tokens"].(int64); ok {
			op.OutputTokens = outputTokens
		}
		if durationMs, ok := v["duration_ms"].(int64); ok {
			op.DurationMs = durationMs
		}
	default:
		return fmt.Errorf("unsupported operation type: %T", opInterface)
	}

	// Calculate cost if not provided
	if op.Cost == 0 {
		op.Cost = t.calculateCost(op.InputTokens, op.OutputTokens)
	}

	// Set timestamp if not provided
	if op.Timestamp.IsZero() {
		op.Timestamp = time.Now()
	}

	// Generate ID if not provided
	if op.ID == "" {
		op.ID = uuid.New().String()
	}

	// Store operation in database (best-effort)
	if t.store != nil {
		_ = t.storeOperation(ctx, op)
	}

	return nil
}

// storeOperation stores a quota operation in the database
func (t *Tracker) storeOperation(ctx context.Context, op QuotaOperation) error {
	if t.store == nil {
		return nil // No storage configured
	}

	// Convert to storage layer type
	storageOp := &struct {
		ID            string
		Timestamp     time.Time
		IssueID       string
		OperationType string
		Model         string
		InputTokens   int64
		OutputTokens  int64
		Cost          float64
		DurationMs    int64
	}{
		ID:            op.ID,
		Timestamp:     op.Timestamp,
		IssueID:       op.IssueID,
		OperationType: op.OperationType,
		Model:         op.Model,
		InputTokens:   op.InputTokens,
		OutputTokens:  op.OutputTokens,
		Cost:          op.Cost,
		DurationMs:    op.DurationMs,
	}

	// Use type assertion to call StoreQuotaOperation on the storage interface
	type quotaStore interface {
		StoreQuotaOperation(ctx context.Context, op interface{}) error
	}

	if qs, ok := t.store.(quotaStore); ok {
		return qs.StoreQuotaOperation(ctx, storageOp)
	}

	return nil // Storage doesn't support quota monitoring yet
}
