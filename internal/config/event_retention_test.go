package config

import (
	"os"
	"testing"
)

func TestEventRetentionConfigFromEnv(t *testing.T) {
	tests := []struct {
		name    string
		envVars map[string]string
		wantErr bool
		check   func(t *testing.T, cfg EventRetentionConfig)
	}{
		{
			name:    "no environment variables uses defaults",
			envVars: map[string]string{},
			wantErr: false,
			check: func(t *testing.T, cfg EventRetentionConfig) {
				defaults := DefaultEventRetentionConfig()
				if cfg.RetentionDays != defaults.RetentionDays {
					t.Errorf("RetentionDays = %v, want %v", cfg.RetentionDays, defaults.RetentionDays)
				}
				if cfg.RetentionCriticalDays != defaults.RetentionCriticalDays {
					t.Errorf("RetentionCriticalDays = %v, want %v", cfg.RetentionCriticalDays, defaults.RetentionCriticalDays)
				}
				if cfg.PerIssueLimitEvents != defaults.PerIssueLimitEvents {
					t.Errorf("PerIssueLimitEvents = %v, want %v", cfg.PerIssueLimitEvents, defaults.PerIssueLimitEvents)
				}
				if cfg.GlobalLimitEvents != defaults.GlobalLimitEvents {
					t.Errorf("GlobalLimitEvents = %v, want %v", cfg.GlobalLimitEvents, defaults.GlobalLimitEvents)
				}
				if cfg.CleanupIntervalHours != defaults.CleanupIntervalHours {
					t.Errorf("CleanupIntervalHours = %v, want %v", cfg.CleanupIntervalHours, defaults.CleanupIntervalHours)
				}
				if cfg.CleanupBatchSize != defaults.CleanupBatchSize {
					t.Errorf("CleanupBatchSize = %v, want %v", cfg.CleanupBatchSize, defaults.CleanupBatchSize)
				}
				if cfg.CleanupEnabled != defaults.CleanupEnabled {
					t.Errorf("CleanupEnabled = %v, want %v", cfg.CleanupEnabled, defaults.CleanupEnabled)
				}
				if cfg.CleanupStrategy != defaults.CleanupStrategy {
					t.Errorf("CleanupStrategy = %v, want %v", cfg.CleanupStrategy, defaults.CleanupStrategy)
				}
				if cfg.CleanupVacuum != defaults.CleanupVacuum {
					t.Errorf("CleanupVacuum = %v, want %v", cfg.CleanupVacuum, defaults.CleanupVacuum)
				}
			},
		},
		{
			name: "valid custom configuration",
			envVars: map[string]string{
				"VC_EVENT_RETENTION_DAYS":          "60",
				"VC_EVENT_RETENTION_CRITICAL_DAYS": "180",
				"VC_EVENT_PER_ISSUE_LIMIT":         "2000",
				"VC_EVENT_GLOBAL_LIMIT":            "200000",
				"VC_EVENT_CLEANUP_INTERVAL_HOURS":  "12",
				"VC_EVENT_CLEANUP_BATCH_SIZE":      "500",
				"VC_EVENT_CLEANUP_ENABLED":         "false",
				"VC_EVENT_CLEANUP_STRATEGY":        "oldest_first",
				"VC_EVENT_CLEANUP_VACUUM":          "true",
			},
			wantErr: false,
			check: func(t *testing.T, cfg EventRetentionConfig) {
				if cfg.RetentionDays != 60 {
					t.Errorf("RetentionDays = %v, want 60", cfg.RetentionDays)
				}
				if cfg.RetentionCriticalDays != 180 {
					t.Errorf("RetentionCriticalDays = %v, want 180", cfg.RetentionCriticalDays)
				}
				if cfg.PerIssueLimitEvents != 2000 {
					t.Errorf("PerIssueLimitEvents = %v, want 2000", cfg.PerIssueLimitEvents)
				}
				if cfg.GlobalLimitEvents != 200000 {
					t.Errorf("GlobalLimitEvents = %v, want 200000", cfg.GlobalLimitEvents)
				}
				if cfg.CleanupIntervalHours != 12 {
					t.Errorf("CleanupIntervalHours = %v, want 12", cfg.CleanupIntervalHours)
				}
				if cfg.CleanupBatchSize != 500 {
					t.Errorf("CleanupBatchSize = %v, want 500", cfg.CleanupBatchSize)
				}
				if cfg.CleanupEnabled != false {
					t.Errorf("CleanupEnabled = %v, want false", cfg.CleanupEnabled)
				}
				if cfg.CleanupStrategy != "oldest_first" {
					t.Errorf("CleanupStrategy = %v, want oldest_first", cfg.CleanupStrategy)
				}
				if cfg.CleanupVacuum != true {
					t.Errorf("CleanupVacuum = %v, want true", cfg.CleanupVacuum)
				}
			},
		},
		{
			name: "unlimited per-issue events (zero value)",
			envVars: map[string]string{
				"VC_EVENT_PER_ISSUE_LIMIT": "0",
			},
			wantErr: false,
			check: func(t *testing.T, cfg EventRetentionConfig) {
				if cfg.PerIssueLimitEvents != 0 {
					t.Errorf("PerIssueLimitEvents = %v, want 0 (unlimited)", cfg.PerIssueLimitEvents)
				}
			},
		},
		{
			name: "invalid int value",
			envVars: map[string]string{
				"VC_EVENT_RETENTION_DAYS": "not-a-number",
			},
			wantErr: true,
		},
		{
			name: "invalid bool value",
			envVars: map[string]string{
				"VC_EVENT_CLEANUP_ENABLED": "maybe",
			},
			wantErr: true,
		},
		{
			name: "retention days out of range - too low",
			envVars: map[string]string{
				"VC_EVENT_RETENTION_DAYS": "0",
			},
			wantErr: true,
		},
		{
			name: "retention days out of range - too high",
			envVars: map[string]string{
				"VC_EVENT_RETENTION_DAYS": "400",
			},
			wantErr: true,
		},
		{
			name: "critical retention days out of range - too high",
			envVars: map[string]string{
				"VC_EVENT_RETENTION_CRITICAL_DAYS": "800",
			},
			wantErr: true,
		},
		{
			name: "critical retention less than regular retention",
			envVars: map[string]string{
				"VC_EVENT_RETENTION_DAYS":          "60",
				"VC_EVENT_RETENTION_CRITICAL_DAYS": "30",
			},
			wantErr: true,
		},
		{
			name: "per-issue limit too low (not zero)",
			envVars: map[string]string{
				"VC_EVENT_PER_ISSUE_LIMIT": "50",
			},
			wantErr: true,
		},
		{
			name: "per-issue limit too high",
			envVars: map[string]string{
				"VC_EVENT_PER_ISSUE_LIMIT": "20000",
			},
			wantErr: true,
		},
		{
			name: "global limit too low",
			envVars: map[string]string{
				"VC_EVENT_GLOBAL_LIMIT": "500",
			},
			wantErr: true,
		},
		{
			name: "global limit too high",
			envVars: map[string]string{
				"VC_EVENT_GLOBAL_LIMIT": "2000000",
			},
			wantErr: true,
		},
		{
			name: "cleanup interval too low",
			envVars: map[string]string{
				"VC_EVENT_CLEANUP_INTERVAL_HOURS": "0",
			},
			wantErr: true,
		},
		{
			name: "cleanup interval too high",
			envVars: map[string]string{
				"VC_EVENT_CLEANUP_INTERVAL_HOURS": "200",
			},
			wantErr: true,
		},
		{
			name: "batch size too low",
			envVars: map[string]string{
				"VC_EVENT_CLEANUP_BATCH_SIZE": "50",
			},
			wantErr: true,
		},
		{
			name: "batch size too high",
			envVars: map[string]string{
				"VC_EVENT_CLEANUP_BATCH_SIZE": "20000",
			},
			wantErr: true,
		},
		{
			name: "invalid cleanup strategy",
			envVars: map[string]string{
				"VC_EVENT_CLEANUP_STRATEGY": "newest_first",
			},
			wantErr: true,
		},
		{
			name: "partial configuration",
			envVars: map[string]string{
				"VC_EVENT_RETENTION_DAYS": "45",
				"VC_EVENT_GLOBAL_LIMIT":   "150000",
			},
			wantErr: false,
			check: func(t *testing.T, cfg EventRetentionConfig) {
				// Custom values
				if cfg.RetentionDays != 45 {
					t.Errorf("RetentionDays = %v, want 45", cfg.RetentionDays)
				}
				if cfg.GlobalLimitEvents != 150000 {
					t.Errorf("GlobalLimitEvents = %v, want 150000", cfg.GlobalLimitEvents)
				}
				// Default values
				defaults := DefaultEventRetentionConfig()
				if cfg.RetentionCriticalDays != defaults.RetentionCriticalDays {
					t.Errorf("RetentionCriticalDays = %v, want %v (default)", cfg.RetentionCriticalDays, defaults.RetentionCriticalDays)
				}
				if cfg.PerIssueLimitEvents != defaults.PerIssueLimitEvents {
					t.Errorf("PerIssueLimitEvents = %v, want %v (default)", cfg.PerIssueLimitEvents, defaults.PerIssueLimitEvents)
				}
				if cfg.CleanupStrategy != defaults.CleanupStrategy {
					t.Errorf("CleanupStrategy = %v, want %v (default)", cfg.CleanupStrategy, defaults.CleanupStrategy)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Clear environment variables
			clearEnv := []string{
				"VC_EVENT_RETENTION_DAYS",
				"VC_EVENT_RETENTION_CRITICAL_DAYS",
				"VC_EVENT_PER_ISSUE_LIMIT",
				"VC_EVENT_GLOBAL_LIMIT",
				"VC_EVENT_CLEANUP_INTERVAL_HOURS",
				"VC_EVENT_CLEANUP_BATCH_SIZE",
				"VC_EVENT_CLEANUP_ENABLED",
				"VC_EVENT_CLEANUP_STRATEGY",
				"VC_EVENT_CLEANUP_VACUUM",
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

			// Run EventRetentionConfigFromEnv
			cfg, err := EventRetentionConfigFromEnv()

			// Check error expectation
			if (err != nil) != tt.wantErr {
				t.Errorf("EventRetentionConfigFromEnv() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			// Run custom checks if provided and no error expected
			if !tt.wantErr && tt.check != nil {
				tt.check(t, cfg)
			}
		})
	}
}

func TestEventRetentionConfigValidate(t *testing.T) {
	tests := []struct {
		name    string
		config  EventRetentionConfig
		wantErr bool
		errMsg  string
	}{
		{
			name:    "default config is valid",
			config:  DefaultEventRetentionConfig(),
			wantErr: false,
		},
		{
			name: "valid config at minimum bounds",
			config: EventRetentionConfig{
				RetentionDays:         1,
				RetentionCriticalDays: 1,
				PerIssueLimitEvents:   100,
				GlobalLimitEvents:     1000,
				CleanupIntervalHours:  1,
				CleanupBatchSize:      100,
				CleanupEnabled:        true,
				CleanupStrategy:       "oldest_first",
				CleanupVacuum:         false,
			},
			wantErr: false,
		},
		{
			name: "valid config at maximum bounds",
			config: EventRetentionConfig{
				RetentionDays:         365,
				RetentionCriticalDays: 730,
				PerIssueLimitEvents:   10000,
				GlobalLimitEvents:     1000000,
				CleanupIntervalHours:  168,
				CleanupBatchSize:      10000,
				CleanupEnabled:        false,
				CleanupStrategy:       "oldest_non_critical",
				CleanupVacuum:         true,
			},
			wantErr: false,
		},
		{
			name: "retention days too low",
			config: EventRetentionConfig{
				RetentionDays:         0,
				RetentionCriticalDays: 90,
				PerIssueLimitEvents:   1000,
				GlobalLimitEvents:     100000,
				CleanupIntervalHours:  24,
				CleanupBatchSize:      1000,
				CleanupEnabled:        true,
				CleanupStrategy:       "oldest_non_critical",
				CleanupVacuum:         false,
			},
			wantErr: true,
			errMsg:  "retention_days must be between 1 and 365",
		},
		{
			name: "retention days too high",
			config: EventRetentionConfig{
				RetentionDays:         400,
				RetentionCriticalDays: 400,
				PerIssueLimitEvents:   1000,
				GlobalLimitEvents:     100000,
				CleanupIntervalHours:  24,
				CleanupBatchSize:      1000,
				CleanupEnabled:        true,
				CleanupStrategy:       "oldest_non_critical",
				CleanupVacuum:         false,
			},
			wantErr: true,
			errMsg:  "retention_days must be between 1 and 365",
		},
		{
			name: "critical retention less than regular retention",
			config: EventRetentionConfig{
				RetentionDays:         60,
				RetentionCriticalDays: 30,
				PerIssueLimitEvents:   1000,
				GlobalLimitEvents:     100000,
				CleanupIntervalHours:  24,
				CleanupBatchSize:      1000,
				CleanupEnabled:        true,
				CleanupStrategy:       "oldest_non_critical",
				CleanupVacuum:         false,
			},
			wantErr: true,
			errMsg:  "retention_critical_days (30) must be >= retention_days (60)",
		},
		{
			name: "per-issue limit negative",
			config: EventRetentionConfig{
				RetentionDays:         30,
				RetentionCriticalDays: 90,
				PerIssueLimitEvents:   -1,
				GlobalLimitEvents:     100000,
				CleanupIntervalHours:  24,
				CleanupBatchSize:      1000,
				CleanupEnabled:        true,
				CleanupStrategy:       "oldest_non_critical",
				CleanupVacuum:         false,
			},
			wantErr: true,
			errMsg:  "per_issue_limit_events cannot be negative",
		},
		{
			name: "per-issue limit too low (but not zero)",
			config: EventRetentionConfig{
				RetentionDays:         30,
				RetentionCriticalDays: 90,
				PerIssueLimitEvents:   50,
				GlobalLimitEvents:     100000,
				CleanupIntervalHours:  24,
				CleanupBatchSize:      1000,
				CleanupEnabled:        true,
				CleanupStrategy:       "oldest_non_critical",
				CleanupVacuum:         false,
			},
			wantErr: true,
			errMsg:  "per_issue_limit_events must be 0 (unlimited) or >= 100",
		},
		{
			name: "per-issue limit zero is valid (unlimited)",
			config: EventRetentionConfig{
				RetentionDays:         30,
				RetentionCriticalDays: 90,
				PerIssueLimitEvents:   0,
				GlobalLimitEvents:     100000,
				CleanupIntervalHours:  24,
				CleanupBatchSize:      1000,
				CleanupEnabled:        true,
				CleanupStrategy:       "oldest_non_critical",
				CleanupVacuum:         false,
			},
			wantErr: false,
		},
		{
			name: "invalid cleanup strategy",
			config: EventRetentionConfig{
				RetentionDays:         30,
				RetentionCriticalDays: 90,
				PerIssueLimitEvents:   1000,
				GlobalLimitEvents:     100000,
				CleanupIntervalHours:  24,
				CleanupBatchSize:      1000,
				CleanupEnabled:        true,
				CleanupStrategy:       "random_order",
				CleanupVacuum:         false,
			},
			wantErr: true,
			errMsg:  "cleanup_strategy must be 'oldest_first' or 'oldest_non_critical'",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr && tt.errMsg != "" && err != nil {
				if err.Error() != tt.errMsg && !contains(err.Error(), tt.errMsg) {
					t.Errorf("Validate() error = %q, want error containing %q", err.Error(), tt.errMsg)
				}
			}
		})
	}
}

func TestEventRetentionConfigString(t *testing.T) {
	cfg := DefaultEventRetentionConfig()
	str := cfg.String()

	// Check that the string contains key information
	expected := []string{
		"EventRetentionConfig",
		"RetentionDays: 30",
		"RetentionCriticalDays: 90",
		"PerIssueLimit: 1000",
		"GlobalLimit: 100000",
		"CleanupInterval: 24h",
		"BatchSize: 1000",
		"Enabled: true",
		"Strategy: oldest_non_critical",
		"Vacuum: false",
	}

	for _, exp := range expected {
		if !contains(str, exp) {
			t.Errorf("String() = %q, want to contain %q", str, exp)
		}
	}
}

// contains checks if a string contains a substring
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > len(substr) && containsAt(s, substr))
}

func containsAt(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
