package watchdog

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"sync"
	"time"
)

// DetectionState tracks consecutive detections of a specific anomaly type
// This enables accumulation-based intervention logic
type DetectionState struct {
	ConsecutiveCount int       // Number of consecutive detections
	FirstDetectedAt  time.Time // When this anomaly was first detected in the current sequence
	LastDetectedAt   time.Time // Most recent detection time
}

// WatchdogConfig holds the complete watchdog configuration
// This includes settings for monitoring, anomaly detection, and intervention policies
type WatchdogConfig struct {
	// Enabled controls whether the watchdog is active
	// Default: true
	Enabled bool `json:"enabled"`

	// CheckInterval is how often to run anomaly detection
	// Default: 30 seconds
	CheckInterval time.Duration `json:"check_interval"`

	// TelemetryWindowSize is the number of recent executions to keep for analysis
	// Default: 100
	TelemetryWindowSize int `json:"telemetry_window_size"`

	// AI sensitivity settings
	AIConfig AIConfig `json:"ai_config"`

	// Intervention policies
	InterventionConfig InterventionConfig `json:"intervention_config"`

	// MaxHistorySize is the maximum number of interventions to keep in memory
	// Default: 100
	MaxHistorySize int `json:"max_history_size"`

	// detectionStates tracks consecutive detections per anomaly type
	// This supports accumulation-based intervention logic (vc-227)
	detectionStates map[AnomalyType]*DetectionState

	mu sync.RWMutex // Protects runtime reconfiguration and detection state
}

// AIConfig holds AI-related watchdog settings
type AIConfig struct {
	// MinConfidenceThreshold is the minimum confidence level (0.0-1.0) required
	// for an anomaly detection to trigger an intervention
	// Default: 0.75 (conservative)
	MinConfidenceThreshold float64 `json:"min_confidence_threshold"`

	// MinSeverityLevel is the minimum severity level that triggers automatic intervention
	// Lower severity anomalies are logged but don't trigger interventions
	// Options: "low", "medium", "high", "critical"
	// Default: "high" (conservative - only intervene on high/critical)
	MinSeverityLevel AnomalySeverity `json:"min_severity_level"`

	// EnableAnomalyLogging controls whether all anomaly detections (even below threshold)
	// are logged for debugging and analysis
	// Default: true
	EnableAnomalyLogging bool `json:"enable_anomaly_logging"`
}

// InterventionConfig holds intervention policy settings
type InterventionConfig struct {
	// AutoKillEnabled controls whether the watchdog can automatically kill agents
	// If false, the watchdog will only create escalation issues without intervention
	// Default: true
	AutoKillEnabled bool `json:"auto_kill_enabled"`

	// MaxRetries is the number of times an issue can fail before being marked as blocked
	// 0 means no retry limit (not recommended)
	// Default: 3
	MaxRetries int `json:"max_retries"`

	// EscalateOnCritical controls whether critical anomalies create escalation issues
	// immediately even if intervention is disabled
	// Default: true
	EscalateOnCritical bool `json:"escalate_on_critical"`

	// EscalationPriority maps anomaly severity to escalation issue priority
	// Default: critical=P0, high=P1, medium=P2, low=P3
	EscalationPriority map[AnomalySeverity]int `json:"escalation_priority"`
}

// DefaultWatchdogConfig returns a watchdog configuration with safe, conservative defaults
// These defaults prioritize safety over aggressiveness:
// - High confidence threshold (0.75)
// - Only intervene on high/critical severity
// - Auto-kill enabled for dangerous situations
// - Limited retries (3) before escalation
func DefaultWatchdogConfig() *WatchdogConfig {
	return &WatchdogConfig{
		Enabled:             true,
		CheckInterval:       30 * time.Second,
		TelemetryWindowSize: 100,
		AIConfig: AIConfig{
			MinConfidenceThreshold: 0.75,
			MinSeverityLevel:       SeverityHigh,
			EnableAnomalyLogging:   true,
		},
		InterventionConfig: InterventionConfig{
			AutoKillEnabled:    true,
			MaxRetries:         3,
			EscalateOnCritical: true,
			EscalationPriority: map[AnomalySeverity]int{
				SeverityCritical: 0, // P0
				SeverityHigh:     1, // P1
				SeverityMedium:   2, // P2
				SeverityLow:      3, // P3
			},
		},
		MaxHistorySize:  100,
		detectionStates: make(map[AnomalyType]*DetectionState),
	}
}

// LoadFromFile loads watchdog configuration from a JSON file
// Returns default config if file doesn't exist
// Returns error if file exists but is invalid
func LoadFromFile(path string) (*WatchdogConfig, error) {
	// Check if file exists
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			// File doesn't exist, use defaults
			return DefaultWatchdogConfig(), nil
		}
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	// Parse JSON
	var cfg WatchdogConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config file: %w", err)
	}

	// Validate and apply defaults for missing fields
	if err := cfg.validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	return &cfg, nil
}

// LoadFromEnv loads watchdog configuration from environment variables
// Environment variables override default values
// Prefix: VC_WATCHDOG_
func LoadFromEnv() *WatchdogConfig {
	cfg := DefaultWatchdogConfig()

	// Overall watchdog settings
	if val := os.Getenv("VC_WATCHDOG_ENABLED"); val != "" {
		cfg.Enabled = parseBool(val, true)
	}

	if val := os.Getenv("VC_WATCHDOG_CHECK_INTERVAL"); val != "" {
		if duration, err := time.ParseDuration(val); err == nil {
			cfg.CheckInterval = duration
		}
	}

	if val := os.Getenv("VC_WATCHDOG_TELEMETRY_WINDOW"); val != "" {
		if size, err := strconv.Atoi(val); err == nil && size > 0 {
			cfg.TelemetryWindowSize = size
		}
	}

	if val := os.Getenv("VC_WATCHDOG_MAX_HISTORY"); val != "" {
		if size, err := strconv.Atoi(val); err == nil && size > 0 {
			cfg.MaxHistorySize = size
		}
	}

	// AI config
	if val := os.Getenv("VC_WATCHDOG_MIN_CONFIDENCE"); val != "" {
		if confidence, err := strconv.ParseFloat(val, 64); err == nil {
			cfg.AIConfig.MinConfidenceThreshold = confidence
		}
	}

	if val := os.Getenv("VC_WATCHDOG_MIN_SEVERITY"); val != "" {
		cfg.AIConfig.MinSeverityLevel = AnomalySeverity(val)
	}

	if val := os.Getenv("VC_WATCHDOG_LOG_ANOMALIES"); val != "" {
		cfg.AIConfig.EnableAnomalyLogging = parseBool(val, true)
	}

	// Intervention config
	if val := os.Getenv("VC_WATCHDOG_AUTO_KILL"); val != "" {
		cfg.InterventionConfig.AutoKillEnabled = parseBool(val, true)
	}

	if val := os.Getenv("VC_WATCHDOG_MAX_RETRIES"); val != "" {
		if retries, err := strconv.Atoi(val); err == nil && retries >= 0 {
			cfg.InterventionConfig.MaxRetries = retries
		}
	}

	if val := os.Getenv("VC_WATCHDOG_ESCALATE_CRITICAL"); val != "" {
		cfg.InterventionConfig.EscalateOnCritical = parseBool(val, true)
	}

	// Validate after loading from env
	if err := cfg.validate(); err != nil {
		fmt.Printf("Warning: invalid watchdog config from environment: %v\n", err)
		return DefaultWatchdogConfig()
	}

	return cfg
}

// parseBool parses a boolean string with a default value
func parseBool(val string, defaultVal bool) bool {
	switch val {
	case "true", "1", "yes", "on":
		return true
	case "false", "0", "no", "off":
		return false
	default:
		return defaultVal
	}
}

// Validate checks that the configuration has safe and reasonable values
// This prevents misconfigurations that could cause the watchdog to malfunction
func (c *WatchdogConfig) Validate() error {
	return c.validate()
}

// validate performs the actual validation (lowercase for internal use)
func (c *WatchdogConfig) validate() error {
	// Check interval must be positive
	if c.CheckInterval <= 0 {
		return fmt.Errorf("check_interval must be positive, got %v", c.CheckInterval)
	}

	// Check interval should be reasonable (not too fast, not too slow)
	if c.CheckInterval < 5*time.Second {
		return fmt.Errorf("check_interval too fast (minimum 5s), got %v", c.CheckInterval)
	}
	if c.CheckInterval > 5*time.Minute {
		return fmt.Errorf("check_interval too slow (maximum 5m), got %v", c.CheckInterval)
	}

	// Telemetry window must be positive and reasonable
	if c.TelemetryWindowSize <= 0 {
		return fmt.Errorf("telemetry_window_size must be positive, got %d", c.TelemetryWindowSize)
	}
	if c.TelemetryWindowSize > 10000 {
		return fmt.Errorf("telemetry_window_size too large (maximum 10000), got %d", c.TelemetryWindowSize)
	}

	// AI config validation
	if c.AIConfig.MinConfidenceThreshold < 0.0 || c.AIConfig.MinConfidenceThreshold > 1.0 {
		return fmt.Errorf("min_confidence_threshold must be between 0.0 and 1.0, got %f", c.AIConfig.MinConfidenceThreshold)
	}

	// Validate severity level
	validSeverities := map[AnomalySeverity]bool{
		SeverityCritical: true,
		SeverityHigh:     true,
		SeverityMedium:   true,
		SeverityLow:      true,
	}
	if !validSeverities[c.AIConfig.MinSeverityLevel] {
		return fmt.Errorf("invalid min_severity_level: %s (must be low, medium, high, or critical)", c.AIConfig.MinSeverityLevel)
	}

	// Intervention config validation
	if c.InterventionConfig.MaxRetries < 0 {
		return fmt.Errorf("max_retries must be non-negative, got %d", c.InterventionConfig.MaxRetries)
	}
	if c.InterventionConfig.MaxRetries > 100 {
		return fmt.Errorf("max_retries too large (maximum 100), got %d", c.InterventionConfig.MaxRetries)
	}

	// Validate escalation priorities
	if c.InterventionConfig.EscalationPriority == nil {
		c.InterventionConfig.EscalationPriority = DefaultWatchdogConfig().InterventionConfig.EscalationPriority
	}

	// History size validation
	if c.MaxHistorySize <= 0 {
		return fmt.Errorf("max_history_size must be positive, got %d", c.MaxHistorySize)
	}
	if c.MaxHistorySize > 10000 {
		return fmt.Errorf("max_history_size too large (maximum 10000), got %d", c.MaxHistorySize)
	}

	return nil
}

// Clone creates a deep copy of the configuration (for runtime reconfiguration)
func (c *WatchdogConfig) Clone() *WatchdogConfig {
	c.mu.RLock()
	defer c.mu.RUnlock()

	// Copy escalation priority map
	escPriority := make(map[AnomalySeverity]int)
	for k, v := range c.InterventionConfig.EscalationPriority {
		escPriority[k] = v
	}

	// Copy detection states map
	detectionStates := make(map[AnomalyType]*DetectionState)
	for k, v := range c.detectionStates {
		detectionStates[k] = &DetectionState{
			ConsecutiveCount: v.ConsecutiveCount,
			FirstDetectedAt:  v.FirstDetectedAt,
			LastDetectedAt:   v.LastDetectedAt,
		}
	}

	return &WatchdogConfig{
		Enabled:             c.Enabled,
		CheckInterval:       c.CheckInterval,
		TelemetryWindowSize: c.TelemetryWindowSize,
		AIConfig: AIConfig{
			MinConfidenceThreshold: c.AIConfig.MinConfidenceThreshold,
			MinSeverityLevel:       c.AIConfig.MinSeverityLevel,
			EnableAnomalyLogging:   c.AIConfig.EnableAnomalyLogging,
		},
		InterventionConfig: InterventionConfig{
			AutoKillEnabled:    c.InterventionConfig.AutoKillEnabled,
			MaxRetries:         c.InterventionConfig.MaxRetries,
			EscalateOnCritical: c.InterventionConfig.EscalateOnCritical,
			EscalationPriority: escPriority,
		},
		MaxHistorySize:  c.MaxHistorySize,
		detectionStates: detectionStates,
	}
}

// UpdateAIConfig updates AI-related settings at runtime
// This allows tuning sensitivity without restarting
func (c *WatchdogConfig) UpdateAIConfig(newConfig AIConfig) error {
	// Validate the new AI config
	tempCfg := c.Clone()
	tempCfg.AIConfig = newConfig
	if err := tempCfg.validate(); err != nil {
		return fmt.Errorf("invalid AI config: %w", err)
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	c.AIConfig = newConfig
	return nil
}

// UpdateInterventionConfig updates intervention policy settings at runtime
func (c *WatchdogConfig) UpdateInterventionConfig(newConfig InterventionConfig) error {
	// Validate the new intervention config
	tempCfg := c.Clone()
	tempCfg.InterventionConfig = newConfig
	if err := tempCfg.validate(); err != nil {
		return fmt.Errorf("invalid intervention config: %w", err)
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	c.InterventionConfig = newConfig
	return nil
}

// SetEnabled enables or disables the watchdog at runtime
func (c *WatchdogConfig) SetEnabled(enabled bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.Enabled = enabled
}

// IsEnabled returns whether the watchdog is currently enabled
func (c *WatchdogConfig) IsEnabled() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.Enabled
}

// GetCheckInterval returns the current check interval (thread-safe)
func (c *WatchdogConfig) GetCheckInterval() time.Duration {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.CheckInterval
}

// SetCheckInterval updates the check interval at runtime
func (c *WatchdogConfig) SetCheckInterval(interval time.Duration) error {
	// Validate the new interval
	if interval < 5*time.Second || interval > 5*time.Minute {
		return fmt.Errorf("invalid check interval: %v (must be between 5s and 5m)", interval)
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	c.CheckInterval = interval
	return nil
}

// ShouldIntervene determines if an anomaly report should trigger an intervention
// based on the current configuration thresholds and accumulation model (vc-227)
//
// Accumulation model for stuck_state:
//   - Intervene after 10 consecutive detections (regardless of confidence/severity)
//   - OR intervene after 3 minutes stuck (regardless of confidence/severity)
//
// Other anomaly types use standard thresholds (confidence + severity)
func (c *WatchdogConfig) ShouldIntervene(report *AnomalyReport) bool {
	c.mu.Lock() // Need write lock to update detection states
	defer c.mu.Unlock()

	if !c.Enabled {
		return false
	}

	if !report.Detected {
		// No anomaly - clear all detection states
		c.detectionStates = make(map[AnomalyType]*DetectionState)
		return false
	}

	// Update detection state for this anomaly type
	state, exists := c.detectionStates[report.AnomalyType]
	now := time.Now()

	if !exists {
		// First detection of this anomaly type
		state = &DetectionState{
			ConsecutiveCount: 1,
			FirstDetectedAt:  now,
			LastDetectedAt:   now,
		}
		c.detectionStates[report.AnomalyType] = state
	} else {
		// Consecutive detection
		state.ConsecutiveCount++
		state.LastDetectedAt = now
	}

	// Special accumulation logic for stuck_state (vc-227)
	if report.AnomalyType == AnomalyStuckState {
		// Condition 1: 10 consecutive detections
		if state.ConsecutiveCount >= 10 {
			return true
		}

		// Condition 2: Stuck for 3+ minutes
		stuckDuration := now.Sub(state.FirstDetectedAt)
		if stuckDuration >= 3*time.Minute {
			return true
		}

		// Neither condition met - don't intervene yet
		return false
	}

	// For other anomaly types, use standard threshold logic
	// Check confidence threshold
	if report.Confidence < c.AIConfig.MinConfidenceThreshold {
		return false
	}

	// Check severity threshold
	return c.meetsMinSeverity(report.Severity)
}

// meetsMinSeverity checks if a severity level meets the minimum threshold
// MUST be called with c.mu held (read or write lock)
func (c *WatchdogConfig) meetsMinSeverity(severity AnomalySeverity) bool {
	minSev := c.AIConfig.MinSeverityLevel

	// Severity ordering: low < medium < high < critical
	severityOrder := map[AnomalySeverity]int{
		SeverityLow:      1,
		SeverityMedium:   2,
		SeverityHigh:     3,
		SeverityCritical: 4,
	}

	return severityOrder[severity] >= severityOrder[minSev]
}

// SaveToFile saves the current configuration to a JSON file
func (c *WatchdogConfig) SaveToFile(path string) error {
	c.mu.RLock()
	defer c.mu.RUnlock()

	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}

	return nil
}
