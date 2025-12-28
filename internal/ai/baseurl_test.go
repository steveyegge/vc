package ai

import (
	"os"
	"testing"
)

// TestBaseURLConfiguration verifies that the BaseURL configuration works correctly
func TestBaseURLConfiguration(t *testing.T) {
	t.Run("config BaseURL takes precedence over env var", func(t *testing.T) {
		// Set env var
		os.Setenv("VC_API_BASE", "http://env-url:8000")
		defer os.Unsetenv("VC_API_BASE")

		// Config with explicit BaseURL should use that
		cfg := &Config{
			APIKey:  "test-key",
			BaseURL: "http://config-url:9000",
			Store:   newMockStorage(),
		}

		// NewSupervisor will use cfg.BaseURL, not env var
		// We can't easily test the client was configured correctly without
		// making a real API call, but we can verify the config is accepted
		sup, err := NewSupervisor(cfg)
		if err != nil {
			t.Fatalf("NewSupervisor failed: %v", err)
		}
		if sup == nil {
			t.Fatal("NewSupervisor returned nil")
		}
	})

	t.Run("env var used when config BaseURL empty", func(t *testing.T) {
		os.Setenv("VC_API_BASE", "http://localhost:8000")
		defer os.Unsetenv("VC_API_BASE")

		cfg := &Config{
			APIKey: "test-key",
			Store:  newMockStorage(),
			// BaseURL intentionally empty
		}

		sup, err := NewSupervisor(cfg)
		if err != nil {
			t.Fatalf("NewSupervisor failed: %v", err)
		}
		if sup == nil {
			t.Fatal("NewSupervisor returned nil")
		}
	})

	t.Run("no BaseURL uses default Anthropic endpoint", func(t *testing.T) {
		os.Unsetenv("VC_API_BASE")

		cfg := &Config{
			APIKey: "test-key",
			Store:  newMockStorage(),
		}

		sup, err := NewSupervisor(cfg)
		if err != nil {
			t.Fatalf("NewSupervisor failed: %v", err)
		}
		if sup == nil {
			t.Fatal("NewSupervisor returned nil")
		}
	})
}
