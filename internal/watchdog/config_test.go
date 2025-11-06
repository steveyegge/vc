package watchdog

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestDefaultWatchdogConfig(t *testing.T) {
	cfg := DefaultWatchdogConfig()

	// Check overall settings
	if !cfg.Enabled {
		t.Error("Expected watchdog to be enabled by default")
	}

	if cfg.CheckInterval != 30*time.Second {
		t.Errorf("Expected check interval 30s, got %v", cfg.CheckInterval)
	}

	if cfg.TelemetryWindowSize != 100 {
		t.Errorf("Expected telemetry window 100, got %d", cfg.TelemetryWindowSize)
	}

	// Check AI config defaults
	if cfg.AIConfig.MinConfidenceThreshold != 0.75 {
		t.Errorf("Expected min confidence 0.75, got %f", cfg.AIConfig.MinConfidenceThreshold)
	}

	if cfg.AIConfig.MinSeverityLevel != SeverityHigh {
		t.Errorf("Expected min severity high, got %s", cfg.AIConfig.MinSeverityLevel)
	}

	if !cfg.AIConfig.EnableAnomalyLogging {
		t.Error("Expected anomaly logging enabled by default")
	}

	// Check intervention config defaults
	if !cfg.InterventionConfig.AutoKillEnabled {
		t.Error("Expected auto-kill enabled by default")
	}

	if cfg.InterventionConfig.MaxRetries != 3 {
		t.Errorf("Expected max retries 3, got %d", cfg.InterventionConfig.MaxRetries)
	}

	if !cfg.InterventionConfig.EscalateOnCritical {
		t.Error("Expected escalate on critical enabled by default")
	}

	// Check escalation priorities
	expectedPriorities := map[AnomalySeverity]int{
		SeverityCritical: 0,
		SeverityHigh:     1,
		SeverityMedium:   2,
		SeverityLow:      3,
	}

	for sev, expectedPri := range expectedPriorities {
		if cfg.InterventionConfig.EscalationPriority[sev] != expectedPri {
			t.Errorf("Expected escalation priority %d for %s, got %d",
				expectedPri, sev, cfg.InterventionConfig.EscalationPriority[sev])
		}
	}
}

func TestLoadFromFile_ValidConfig(t *testing.T) {
	// Create temp config file
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "watchdog.json")

	// Write a valid config
	customConfig := &WatchdogConfig{
		Enabled:             false,
		CheckInterval:       1 * time.Minute,
		TelemetryWindowSize: 200,
		AIConfig: AIConfig{
			MinConfidenceThreshold: 0.85,
			MinSeverityLevel:       SeverityMedium,
			EnableAnomalyLogging:   false,
		},
		InterventionConfig: InterventionConfig{
			AutoKillEnabled:    false,
			MaxRetries:         5,
			EscalateOnCritical: false,
			EscalationPriority: map[AnomalySeverity]int{
				SeverityCritical: 0,
				SeverityHigh:     1,
				SeverityMedium:   2,
				SeverityLow:      3,
			},
		},
		MaxHistorySize: 200,
	}

	data, err := json.MarshalIndent(customConfig, "", "  ")
	if err != nil {
		t.Fatalf("Failed to marshal config: %v", err)
	}

	if err := os.WriteFile(configPath, data, 0644); err != nil {
		t.Fatalf("Failed to write config file: %v", err)
	}

	// Load the config
	loaded, err := LoadFromFile(configPath)
	if err != nil {
		t.Fatalf("Failed to load config: %v", err)
	}

	// Verify loaded values
	if loaded.Enabled {
		t.Error("Expected watchdog disabled")
	}

	if loaded.CheckInterval != 1*time.Minute {
		t.Errorf("Expected check interval 1m, got %v", loaded.CheckInterval)
	}

	if loaded.AIConfig.MinConfidenceThreshold != 0.85 {
		t.Errorf("Expected min confidence 0.85, got %f", loaded.AIConfig.MinConfidenceThreshold)
	}

	if loaded.InterventionConfig.MaxRetries != 5 {
		t.Errorf("Expected max retries 5, got %d", loaded.InterventionConfig.MaxRetries)
	}
}

func TestLoadFromFile_NonExistent(t *testing.T) {
	// Loading a non-existent file should return default config
	cfg, err := LoadFromFile("/non/existent/path.json")
	if err != nil {
		t.Fatalf("Expected no error for non-existent file, got %v", err)
	}

	// Should be default config
	defaultCfg := DefaultWatchdogConfig()
	if cfg.CheckInterval != defaultCfg.CheckInterval {
		t.Error("Expected default config when file doesn't exist")
	}
}

func TestLoadFromFile_InvalidJSON(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "invalid.json")

	// Write invalid JSON
	if err := os.WriteFile(configPath, []byte("not valid json"), 0644); err != nil {
		t.Fatalf("Failed to write invalid config: %v", err)
	}

	// Should return error
	_, err := LoadFromFile(configPath)
	if err == nil {
		t.Error("Expected error for invalid JSON")
	}
}

func TestLoadFromEnv(t *testing.T) {
	// Save original env vars to restore later
	originalEnv := make(map[string]string)
	envVars := []string{
		"VC_WATCHDOG_ENABLED",
		"VC_WATCHDOG_CHECK_INTERVAL",
		"VC_WATCHDOG_TELEMETRY_WINDOW",
		"VC_WATCHDOG_MIN_CONFIDENCE",
		"VC_WATCHDOG_MIN_SEVERITY",
		"VC_WATCHDOG_AUTO_KILL",
		"VC_WATCHDOG_MAX_RETRIES",
	}

	for _, key := range envVars {
		originalEnv[key] = os.Getenv(key)
	}

	// Clean up after test
	defer func() {
		for key, val := range originalEnv {
			if val == "" {
				_ = os.Unsetenv(key)
			} else {
				_ = os.Setenv(key, val)
			}
		}
	}()

	// Set test env vars
	_ = os.Setenv("VC_WATCHDOG_ENABLED", "false")
	_ = os.Setenv("VC_WATCHDOG_CHECK_INTERVAL", "45s")
	_ = os.Setenv("VC_WATCHDOG_TELEMETRY_WINDOW", "150")
	_ = os.Setenv("VC_WATCHDOG_MIN_CONFIDENCE", "0.80")
	_ = os.Setenv("VC_WATCHDOG_MIN_SEVERITY", "medium")
	_ = os.Setenv("VC_WATCHDOG_AUTO_KILL", "no")
	_ = os.Setenv("VC_WATCHDOG_MAX_RETRIES", "7")

	// Load from env
	cfg := LoadFromEnv()

	// Verify loaded values
	if cfg.Enabled {
		t.Error("Expected watchdog disabled from env")
	}

	if cfg.CheckInterval != 45*time.Second {
		t.Errorf("Expected check interval 45s, got %v", cfg.CheckInterval)
	}

	if cfg.TelemetryWindowSize != 150 {
		t.Errorf("Expected telemetry window 150, got %d", cfg.TelemetryWindowSize)
	}

	if cfg.AIConfig.MinConfidenceThreshold != 0.80 {
		t.Errorf("Expected min confidence 0.80, got %f", cfg.AIConfig.MinConfidenceThreshold)
	}

	if cfg.AIConfig.MinSeverityLevel != SeverityMedium {
		t.Errorf("Expected min severity medium, got %s", cfg.AIConfig.MinSeverityLevel)
	}

	if cfg.InterventionConfig.AutoKillEnabled {
		t.Error("Expected auto-kill disabled from env")
	}

	if cfg.InterventionConfig.MaxRetries != 7 {
		t.Errorf("Expected max retries 7, got %d", cfg.InterventionConfig.MaxRetries)
	}
}

func TestValidate_ValidConfig(t *testing.T) {
	cfg := DefaultWatchdogConfig()

	if err := cfg.Validate(); err != nil {
		t.Errorf("Default config should be valid: %v", err)
	}
}

func TestValidate_InvalidCheckInterval(t *testing.T) {
	tests := []struct {
		name     string
		interval time.Duration
	}{
		{"zero", 0},
		{"negative", -1 * time.Second},
		{"too fast", 1 * time.Second},
		{"too slow", 10 * time.Minute},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := DefaultWatchdogConfig()
			cfg.CheckInterval = tt.interval

			if err := cfg.Validate(); err == nil {
				t.Errorf("Expected validation error for check interval %v", tt.interval)
			}
		})
	}
}

func TestValidate_InvalidTelemetryWindow(t *testing.T) {
	tests := []struct {
		name string
		size int
	}{
		{"zero", 0},
		{"negative", -1},
		{"too large", 20000},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := DefaultWatchdogConfig()
			cfg.TelemetryWindowSize = tt.size

			if err := cfg.Validate(); err == nil {
				t.Errorf("Expected validation error for telemetry window %d", tt.size)
			}
		})
	}
}

func TestValidate_InvalidConfidence(t *testing.T) {
	tests := []struct {
		name       string
		confidence float64
	}{
		{"negative", -0.1},
		{"too high", 1.5},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := DefaultWatchdogConfig()
			cfg.AIConfig.MinConfidenceThreshold = tt.confidence

			if err := cfg.Validate(); err == nil {
				t.Errorf("Expected validation error for confidence %f", tt.confidence)
			}
		})
	}
}

func TestValidate_InvalidSeverity(t *testing.T) {
	cfg := DefaultWatchdogConfig()
	cfg.AIConfig.MinSeverityLevel = AnomalySeverity("invalid")

	if err := cfg.Validate(); err == nil {
		t.Error("Expected validation error for invalid severity level")
	}
}

func TestValidate_InvalidMaxRetries(t *testing.T) {
	tests := []struct {
		name    string
		retries int
	}{
		{"negative", -1},
		{"too large", 500},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := DefaultWatchdogConfig()
			cfg.InterventionConfig.MaxRetries = tt.retries

			if err := cfg.Validate(); err == nil {
				t.Errorf("Expected validation error for max retries %d", tt.retries)
			}
		})
	}
}

func TestClone(t *testing.T) {
	original := DefaultWatchdogConfig()
	original.Enabled = false
	original.CheckInterval = 1 * time.Minute
	original.AIConfig.MinConfidenceThreshold = 0.90

	// Clone the config
	cloned := original.Clone()

	// Verify values match
	if cloned.Enabled != original.Enabled {
		t.Error("Clone enabled doesn't match")
	}

	if cloned.CheckInterval != original.CheckInterval {
		t.Error("Clone check interval doesn't match")
	}

	if cloned.AIConfig.MinConfidenceThreshold != original.AIConfig.MinConfidenceThreshold {
		t.Error("Clone min confidence doesn't match")
	}

	// Modify clone and verify original unchanged
	cloned.Enabled = true
	cloned.AIConfig.MinConfidenceThreshold = 0.50

	if original.Enabled {
		t.Error("Modifying clone affected original")
	}

	if original.AIConfig.MinConfidenceThreshold == 0.50 {
		t.Error("Modifying clone's AI config affected original")
	}
}

func TestUpdateAIConfig(t *testing.T) {
	cfg := DefaultWatchdogConfig()

	newAIConfig := AIConfig{
		MinConfidenceThreshold: 0.90,
		MinSeverityLevel:       SeverityCritical,
		EnableAnomalyLogging:   false,
	}

	if err := cfg.UpdateAIConfig(newAIConfig); err != nil {
		t.Fatalf("Failed to update AI config: %v", err)
	}

	if cfg.AIConfig.MinConfidenceThreshold != 0.90 {
		t.Error("AI config not updated")
	}

	if cfg.AIConfig.MinSeverityLevel != SeverityCritical {
		t.Error("Min severity not updated")
	}
}

func TestUpdateAIConfig_Invalid(t *testing.T) {
	cfg := DefaultWatchdogConfig()

	invalidConfig := AIConfig{
		MinConfidenceThreshold: 2.0, // Invalid: > 1.0
		MinSeverityLevel:       SeverityHigh,
	}

	if err := cfg.UpdateAIConfig(invalidConfig); err == nil {
		t.Error("Expected error for invalid AI config")
	}

	// Original config should be unchanged
	if cfg.AIConfig.MinConfidenceThreshold == 2.0 {
		t.Error("Invalid config was applied")
	}
}

func TestUpdateInterventionConfig(t *testing.T) {
	cfg := DefaultWatchdogConfig()

	newInterventionConfig := InterventionConfig{
		AutoKillEnabled:    false,
		MaxRetries:         10,
		EscalateOnCritical: false,
		EscalationPriority: map[AnomalySeverity]int{
			SeverityCritical: 0,
			SeverityHigh:     1,
			SeverityMedium:   2,
			SeverityLow:      3,
		},
	}

	if err := cfg.UpdateInterventionConfig(newInterventionConfig); err != nil {
		t.Fatalf("Failed to update intervention config: %v", err)
	}

	if cfg.InterventionConfig.AutoKillEnabled {
		t.Error("Auto-kill not disabled")
	}

	if cfg.InterventionConfig.MaxRetries != 10 {
		t.Error("Max retries not updated")
	}
}

func TestSetEnabled(t *testing.T) {
	cfg := DefaultWatchdogConfig()

	cfg.SetEnabled(false)
	if cfg.IsEnabled() {
		t.Error("Expected watchdog disabled")
	}

	cfg.SetEnabled(true)
	if !cfg.IsEnabled() {
		t.Error("Expected watchdog enabled")
	}
}

func TestSetCheckInterval(t *testing.T) {
	cfg := DefaultWatchdogConfig()

	// Valid interval
	if err := cfg.SetCheckInterval(45 * time.Second); err != nil {
		t.Errorf("Failed to set valid check interval: %v", err)
	}

	if cfg.GetCheckInterval() != 45*time.Second {
		t.Error("Check interval not updated")
	}

	// Invalid intervals
	invalidIntervals := []time.Duration{
		1 * time.Second,  // Too fast
		10 * time.Minute, // Too slow
	}

	for _, interval := range invalidIntervals {
		if err := cfg.SetCheckInterval(interval); err == nil {
			t.Errorf("Expected error for invalid interval %v", interval)
		}
	}
}

func TestShouldIntervene(t *testing.T) {
	cfg := DefaultWatchdogConfig()

	tests := []struct {
		name     string
		report   *AnomalyReport
		expected bool
	}{
		{
			name: "high severity, high confidence",
			report: &AnomalyReport{
				Detected:   true,
				Severity:   SeverityHigh,
				Confidence: 0.90,
			},
			expected: true,
		},
		{
			name: "critical severity, high confidence",
			report: &AnomalyReport{
				Detected:   true,
				Severity:   SeverityCritical,
				Confidence: 0.95,
			},
			expected: true,
		},
		{
			name: "medium severity (below threshold)",
			report: &AnomalyReport{
				Detected:   true,
				Severity:   SeverityMedium,
				Confidence: 0.90,
			},
			expected: false, // Default min severity is high
		},
		{
			name: "low confidence (below threshold)",
			report: &AnomalyReport{
				Detected:   true,
				Severity:   SeverityHigh,
				Confidence: 0.60, // Below default 0.75
			},
			expected: false,
		},
		{
			name: "not detected",
			report: &AnomalyReport{
				Detected:   false,
				Severity:   SeverityHigh,
				Confidence: 0.90,
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := cfg.ShouldIntervene(tt.report)
			if result != tt.expected {
				t.Errorf("Expected %v, got %v for %s", tt.expected, result, tt.name)
			}
		})
	}

	// Test with watchdog disabled
	cfg.SetEnabled(false)
	report := &AnomalyReport{
		Detected:   true,
		Severity:   SeverityCritical,
		Confidence: 0.95,
	}

	if cfg.ShouldIntervene(report) {
		t.Error("Should not intervene when watchdog is disabled")
	}
}

func TestConcurrentAccess(t *testing.T) {
	cfg := DefaultWatchdogConfig()

	// Test concurrent reads and writes
	var wg sync.WaitGroup
	iterations := 100

	// Concurrent readers
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				_ = cfg.IsEnabled()
				_ = cfg.GetCheckInterval()
			}
		}()
	}

	// Concurrent writers
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				cfg.SetEnabled(id%2 == 0)
			}
		}(i)
	}

	wg.Wait()
	// If we get here without deadlock or race, test passed
}

func TestSaveToFile(t *testing.T) {
	cfg := DefaultWatchdogConfig()
	cfg.Enabled = false
	cfg.CheckInterval = 1 * time.Minute

	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "saved.json")

	if err := cfg.SaveToFile(configPath); err != nil {
		t.Fatalf("Failed to save config: %v", err)
	}

	// Load it back
	loaded, err := LoadFromFile(configPath)
	if err != nil {
		t.Fatalf("Failed to load saved config: %v", err)
	}

	if loaded.Enabled {
		t.Error("Saved config not loaded correctly")
	}

	if loaded.CheckInterval != 1*time.Minute {
		t.Error("Check interval not saved correctly")
	}
}

// TestBackoffMechanism tests the exponential backoff functionality (vc-21pw)
func TestBackoffMechanism(t *testing.T) {
	cfg := DefaultWatchdogConfig()

	// Verify initial state
	if cfg.IsInBackoff() {
		t.Error("Should not be in backoff initially")
	}

	baseInterval := cfg.BackoffConfig.BaseInterval
	if cfg.GetCurrentCheckInterval() != baseInterval {
		t.Errorf("Initial interval should be base interval %v, got %v", baseInterval, cfg.GetCurrentCheckInterval())
	}

	// Record consecutive interventions
	for i := 0; i < cfg.BackoffConfig.TriggerThreshold-1; i++ {
		cfg.RecordIntervention()
		if cfg.IsInBackoff() {
			t.Errorf("Should not enter backoff until threshold reached (iteration %d)", i)
		}
	}

	// One more intervention should trigger backoff
	cfg.RecordIntervention()
	if !cfg.IsInBackoff() {
		t.Error("Should be in backoff mode after reaching threshold")
	}

	// Check that interval has increased
	currentInterval := cfg.GetCurrentCheckInterval()
	expectedInterval := time.Duration(float64(baseInterval) * cfg.BackoffConfig.BackoffMultiplier)
	if currentInterval != expectedInterval {
		t.Errorf("Expected interval %v after first backoff, got %v", expectedInterval, currentInterval)
	}

	// Record more interventions to test exponential increase
	prevInterval := currentInterval
	cfg.RecordIntervention()
	currentInterval = cfg.GetCurrentCheckInterval()
	expectedInterval = time.Duration(float64(prevInterval) * cfg.BackoffConfig.BackoffMultiplier)
	if currentInterval != expectedInterval {
		t.Errorf("Expected exponential backoff to %v, got %v", expectedInterval, currentInterval)
	}

	// Test max interval cap
	for i := 0; i < 20; i++ {
		cfg.RecordIntervention()
	}
	currentInterval = cfg.GetCurrentCheckInterval()
	if currentInterval > cfg.BackoffConfig.MaxInterval {
		t.Errorf("Interval exceeded max: %v > %v", currentInterval, cfg.BackoffConfig.MaxInterval)
	}

	// Test reset on progress
	cfg.RecordProgress()
	if cfg.IsInBackoff() {
		t.Error("Should exit backoff mode on progress")
	}
	if cfg.GetCurrentCheckInterval() != baseInterval {
		t.Errorf("Interval should reset to base %v on progress, got %v", baseInterval, cfg.GetCurrentCheckInterval())
	}

	state := cfg.GetBackoffState()
	if state.ConsecutiveInterventions != 0 {
		t.Errorf("Consecutive interventions should reset to 0, got %d", state.ConsecutiveInterventions)
	}
}

// TestBackoffDisabled tests that backoff doesn't activate when disabled (vc-21pw)
func TestBackoffDisabled(t *testing.T) {
	cfg := DefaultWatchdogConfig()
	cfg.BackoffConfig.Enabled = false

	baseInterval := cfg.BackoffConfig.BaseInterval

	// Record many interventions
	for i := 0; i < 10; i++ {
		cfg.RecordIntervention()
	}

	// Should still be at base interval
	if cfg.IsInBackoff() {
		t.Error("Should not enter backoff when disabled")
	}

	if cfg.GetCurrentCheckInterval() != baseInterval {
		t.Errorf("Interval should remain at base %v when backoff disabled, got %v", baseInterval, cfg.GetCurrentCheckInterval())
	}
}

// TestBackoffEnvVars tests loading backoff config from environment variables (vc-21pw)
func TestBackoffEnvVars(t *testing.T) {
	// Save and restore original env vars
	originalEnv := make(map[string]string)
	envVars := []string{
		"VC_WATCHDOG_BACKOFF_ENABLED",
		"VC_WATCHDOG_BACKOFF_BASE_INTERVAL",
		"VC_WATCHDOG_BACKOFF_MAX_INTERVAL",
		"VC_WATCHDOG_BACKOFF_MULTIPLIER",
		"VC_WATCHDOG_BACKOFF_THRESHOLD",
	}

	for _, key := range envVars {
		originalEnv[key] = os.Getenv(key)
	}

	defer func() {
		for key, val := range originalEnv {
			if val == "" {
				_ = os.Unsetenv(key)
			} else {
				_ = os.Setenv(key, val)
			}
		}
	}()

	// Set test env vars
	_ = os.Setenv("VC_WATCHDOG_BACKOFF_ENABLED", "true")
	_ = os.Setenv("VC_WATCHDOG_BACKOFF_BASE_INTERVAL", "1m")
	_ = os.Setenv("VC_WATCHDOG_BACKOFF_MAX_INTERVAL", "15m")
	_ = os.Setenv("VC_WATCHDOG_BACKOFF_MULTIPLIER", "3.0")
	_ = os.Setenv("VC_WATCHDOG_BACKOFF_THRESHOLD", "5")

	cfg := LoadFromEnv()

	if !cfg.BackoffConfig.Enabled {
		t.Error("Expected backoff enabled from env")
	}

	if cfg.BackoffConfig.BaseInterval != 1*time.Minute {
		t.Errorf("Expected base interval 1m, got %v", cfg.BackoffConfig.BaseInterval)
	}

	if cfg.BackoffConfig.MaxInterval != 15*time.Minute {
		t.Errorf("Expected max interval 15m, got %v", cfg.BackoffConfig.MaxInterval)
	}

	if cfg.BackoffConfig.BackoffMultiplier != 3.0 {
		t.Errorf("Expected multiplier 3.0, got %f", cfg.BackoffConfig.BackoffMultiplier)
	}

	if cfg.BackoffConfig.TriggerThreshold != 5 {
		t.Errorf("Expected threshold 5, got %d", cfg.BackoffConfig.TriggerThreshold)
	}
}
