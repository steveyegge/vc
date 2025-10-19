package deduplication

import (
	"os"
	"testing"
	"time"
)

func TestConfigFromEnv(t *testing.T) {
	tests := []struct {
		name    string
		envVars map[string]string
		wantErr bool
		check   func(t *testing.T, cfg Config)
	}{
		{
			name:    "no environment variables uses defaults",
			envVars: map[string]string{},
			wantErr: false,
			check: func(t *testing.T, cfg Config) {
				defaults := DefaultConfig()
				if cfg.ConfidenceThreshold != defaults.ConfidenceThreshold {
					t.Errorf("ConfidenceThreshold = %v, want %v", cfg.ConfidenceThreshold, defaults.ConfidenceThreshold)
				}
				if cfg.LookbackWindow != defaults.LookbackWindow {
					t.Errorf("LookbackWindow = %v, want %v", cfg.LookbackWindow, defaults.LookbackWindow)
				}
				if cfg.MaxCandidates != defaults.MaxCandidates {
					t.Errorf("MaxCandidates = %v, want %v", cfg.MaxCandidates, defaults.MaxCandidates)
				}
			},
		},
		{
			name: "valid custom configuration",
			envVars: map[string]string{
				"VC_DEDUP_CONFIDENCE_THRESHOLD": "0.90",
				"VC_DEDUP_LOOKBACK_DAYS":        "14",
				"VC_DEDUP_MAX_CANDIDATES":       "100",
				"VC_DEDUP_BATCH_SIZE":           "20",
				"VC_DEDUP_WITHIN_BATCH":         "false",
				"VC_DEDUP_FAIL_OPEN":            "false",
				"VC_DEDUP_INCLUDE_CLOSED":       "true",
				"VC_DEDUP_MIN_TITLE_LENGTH":     "15",
				"VC_DEDUP_MAX_RETRIES":          "3",
				"VC_DEDUP_TIMEOUT_SECS":         "60",
			},
			wantErr: false,
			check: func(t *testing.T, cfg Config) {
				if cfg.ConfidenceThreshold != 0.90 {
					t.Errorf("ConfidenceThreshold = %v, want 0.90", cfg.ConfidenceThreshold)
				}
				if cfg.LookbackWindow != 14*24*time.Hour {
					t.Errorf("LookbackWindow = %v, want %v", cfg.LookbackWindow, 14*24*time.Hour)
				}
				if cfg.MaxCandidates != 100 {
					t.Errorf("MaxCandidates = %v, want 100", cfg.MaxCandidates)
				}
				if cfg.BatchSize != 20 {
					t.Errorf("BatchSize = %v, want 20", cfg.BatchSize)
				}
				if cfg.EnableWithinBatchDedup != false {
					t.Errorf("EnableWithinBatchDedup = %v, want false", cfg.EnableWithinBatchDedup)
				}
				if cfg.FailOpen != false {
					t.Errorf("FailOpen = %v, want false", cfg.FailOpen)
				}
				if cfg.IncludeClosedIssues != true {
					t.Errorf("IncludeClosedIssues = %v, want true", cfg.IncludeClosedIssues)
				}
				if cfg.MinTitleLength != 15 {
					t.Errorf("MinTitleLength = %v, want 15", cfg.MinTitleLength)
				}
				if cfg.MaxRetries != 3 {
					t.Errorf("MaxRetries = %v, want 3", cfg.MaxRetries)
				}
				if cfg.RequestTimeout != 60*time.Second {
					t.Errorf("RequestTimeout = %v, want %v", cfg.RequestTimeout, 60*time.Second)
				}
			},
		},
		{
			name: "invalid float value",
			envVars: map[string]string{
				"VC_DEDUP_CONFIDENCE_THRESHOLD": "not-a-number",
			},
			wantErr: true,
		},
		{
			name: "invalid int value",
			envVars: map[string]string{
				"VC_DEDUP_MAX_CANDIDATES": "not-a-number",
			},
			wantErr: true,
		},
		{
			name: "invalid bool value",
			envVars: map[string]string{
				"VC_DEDUP_FAIL_OPEN": "maybe",
			},
			wantErr: true,
		},
		{
			name: "value out of range - confidence too high",
			envVars: map[string]string{
				"VC_DEDUP_CONFIDENCE_THRESHOLD": "1.5",
			},
			wantErr: true,
		},
		{
			name: "value out of range - lookback too large",
			envVars: map[string]string{
				"VC_DEDUP_LOOKBACK_DAYS": "365",
			},
			wantErr: true,
		},
		{
			name: "partial configuration",
			envVars: map[string]string{
				"VC_DEDUP_CONFIDENCE_THRESHOLD": "0.80",
				"VC_DEDUP_MAX_CANDIDATES":       "75",
			},
			wantErr: false,
			check: func(t *testing.T, cfg Config) {
				// Custom values
				if cfg.ConfidenceThreshold != 0.80 {
					t.Errorf("ConfidenceThreshold = %v, want 0.80", cfg.ConfidenceThreshold)
				}
				if cfg.MaxCandidates != 75 {
					t.Errorf("MaxCandidates = %v, want 75", cfg.MaxCandidates)
				}
				// Default values
				defaults := DefaultConfig()
				if cfg.LookbackWindow != defaults.LookbackWindow {
					t.Errorf("LookbackWindow = %v, want %v (default)", cfg.LookbackWindow, defaults.LookbackWindow)
				}
				if cfg.BatchSize != defaults.BatchSize {
					t.Errorf("BatchSize = %v, want %v (default)", cfg.BatchSize, defaults.BatchSize)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Clear environment variables
			clearEnv := []string{
				"VC_DEDUP_CONFIDENCE_THRESHOLD",
				"VC_DEDUP_LOOKBACK_DAYS",
				"VC_DEDUP_MAX_CANDIDATES",
				"VC_DEDUP_BATCH_SIZE",
				"VC_DEDUP_WITHIN_BATCH",
				"VC_DEDUP_FAIL_OPEN",
				"VC_DEDUP_INCLUDE_CLOSED",
				"VC_DEDUP_MIN_TITLE_LENGTH",
				"VC_DEDUP_MAX_RETRIES",
				"VC_DEDUP_TIMEOUT_SECS",
			}
			for _, key := range clearEnv {
				_ = os.Unsetenv(key) // Intentionally ignore error in test cleanup
			}

			// Set test environment variables
			for key, value := range tt.envVars {
				_ = os.Setenv(key, value) // Intentionally ignore error in test setup
			}

			// Cleanup after test
			defer func() {
				for _, key := range clearEnv {
					_ = os.Unsetenv(key) // Intentionally ignore error in test cleanup
				}
			}()

			// Run ConfigFromEnv
			cfg, err := ConfigFromEnv()

			// Check error expectation
			if (err != nil) != tt.wantErr {
				t.Errorf("ConfigFromEnv() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			// Run custom checks if provided and no error expected
			if !tt.wantErr && tt.check != nil {
				tt.check(t, cfg)
			}
		})
	}
}
