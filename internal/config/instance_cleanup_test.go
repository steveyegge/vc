package config

import (
	"os"
	"testing"
	"time"
)

func TestDefaultInstanceCleanupConfig(t *testing.T) {
	cfg := DefaultInstanceCleanupConfig()

	if cfg.CleanupAgeHours != 24 {
		t.Errorf("Expected CleanupAgeHours to be 24, got %d", cfg.CleanupAgeHours)
	}
	if cfg.CleanupKeep != 10 {
		t.Errorf("Expected CleanupKeep to be 10, got %d", cfg.CleanupKeep)
	}
}

func TestInstanceCleanupConfigValidate(t *testing.T) {
	tests := []struct {
		name    string
		cfg     InstanceCleanupConfig
		wantErr bool
	}{
		{
			name:    "default config is valid",
			cfg:     DefaultInstanceCleanupConfig(),
			wantErr: false,
		},
		{
			name: "valid config at minimum bounds",
			cfg: InstanceCleanupConfig{
				CleanupAgeHours: 0,
				CleanupKeep:     0,
			},
			wantErr: false,
		},
		{
			name: "valid config at maximum bounds",
			cfg: InstanceCleanupConfig{
				CleanupAgeHours: 720,
				CleanupKeep:     1000,
			},
			wantErr: false,
		},
		{
			name: "cleanup age too high",
			cfg: InstanceCleanupConfig{
				CleanupAgeHours: 721,
				CleanupKeep:     10,
			},
			wantErr: true,
		},
		{
			name: "cleanup age negative",
			cfg: InstanceCleanupConfig{
				CleanupAgeHours: -1,
				CleanupKeep:     10,
			},
			wantErr: true,
		},
		{
			name: "cleanup keep negative",
			cfg: InstanceCleanupConfig{
				CleanupAgeHours: 24,
				CleanupKeep:     -1,
			},
			wantErr: true,
		},
		{
			name: "cleanup keep too high",
			cfg: InstanceCleanupConfig{
				CleanupAgeHours: 24,
				CleanupKeep:     1001,
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cfg.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestInstanceCleanupConfigString(t *testing.T) {
	cfg := DefaultInstanceCleanupConfig()
	str := cfg.String()
	expected := "InstanceCleanupConfig{CleanupAgeHours: 24, CleanupKeep: 10}"
	if str != expected {
		t.Errorf("Expected String() to return %q, got %q", expected, str)
	}
}

func TestInstanceCleanupConfigCleanupAge(t *testing.T) {
	tests := []struct {
		name  string
		hours int
		want  time.Duration
	}{
		{
			name:  "24 hours",
			hours: 24,
			want:  24 * time.Hour,
		},
		{
			name:  "0 hours (disabled)",
			hours: 0,
			want:  0,
		},
		{
			name:  "720 hours (30 days)",
			hours: 720,
			want:  720 * time.Hour,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := InstanceCleanupConfig{CleanupAgeHours: tt.hours}
			got := cfg.CleanupAge()
			if got != tt.want {
				t.Errorf("CleanupAge() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestInstanceCleanupConfigFromEnv(t *testing.T) {
	// Save original env and restore after test
	origAge := os.Getenv("VC_INSTANCE_CLEANUP_AGE_HOURS")
	origKeep := os.Getenv("VC_INSTANCE_CLEANUP_KEEP")
	defer func() {
		os.Setenv("VC_INSTANCE_CLEANUP_AGE_HOURS", origAge)
		os.Setenv("VC_INSTANCE_CLEANUP_KEEP", origKeep)
	}()

	tests := []struct {
		name      string
		ageHours  string
		keep      string
		want      InstanceCleanupConfig
		wantErr   bool
		errString string
	}{
		{
			name:     "default config when no env vars",
			ageHours: "",
			keep:     "",
			want:     DefaultInstanceCleanupConfig(),
			wantErr:  false,
		},
		{
			name:     "custom valid config",
			ageHours: "48",
			keep:     "20",
			want: InstanceCleanupConfig{
				CleanupAgeHours: 48,
				CleanupKeep:     20,
			},
			wantErr: false,
		},
		{
			name:     "age hours disabled (0)",
			ageHours: "0",
			keep:     "5",
			want: InstanceCleanupConfig{
				CleanupAgeHours: 0,
				CleanupKeep:     5,
			},
			wantErr: false,
		},
		{
			name:      "invalid age hours (negative)",
			ageHours:  "-1",
			keep:      "10",
			wantErr:   true,
			errString: "cleanup_age_hours must be between 0 and 720",
		},
		{
			name:      "invalid age hours (too high)",
			ageHours:  "721",
			keep:      "10",
			wantErr:   true,
			errString: "cleanup_age_hours must be between 0 and 720",
		},
		{
			name:      "invalid keep (negative)",
			ageHours:  "24",
			keep:      "-1",
			wantErr:   true,
			errString: "cleanup_keep must be between 0 and 1000",
		},
		{
			name:      "invalid keep (too high)",
			ageHours:  "24",
			keep:      "1001",
			wantErr:   true,
			errString: "cleanup_keep must be between 0 and 1000",
		},
		{
			name:      "invalid age hours (not a number)",
			ageHours:  "foo",
			keep:      "10",
			wantErr:   true,
			errString: "invalid value for VC_INSTANCE_CLEANUP_AGE_HOURS",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Set env vars
			os.Setenv("VC_INSTANCE_CLEANUP_AGE_HOURS", tt.ageHours)
			os.Setenv("VC_INSTANCE_CLEANUP_KEEP", tt.keep)

			got, err := InstanceCleanupConfigFromEnv()
			if (err != nil) != tt.wantErr {
				t.Errorf("InstanceCleanupConfigFromEnv() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if tt.wantErr && err != nil {
				if tt.errString != "" && !contains(err.Error(), tt.errString) {
					t.Errorf("Expected error to contain %q, got %q", tt.errString, err.Error())
				}
				return
			}

			if got.CleanupAgeHours != tt.want.CleanupAgeHours {
				t.Errorf("CleanupAgeHours = %d, want %d", got.CleanupAgeHours, tt.want.CleanupAgeHours)
			}
			if got.CleanupKeep != tt.want.CleanupKeep {
				t.Errorf("CleanupKeep = %d, want %d", got.CleanupKeep, tt.want.CleanupKeep)
			}
		})
	}
}
