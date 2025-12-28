package ai

import (
	"context"
	"os"
	"testing"
	"time"
)

// TestBaseURLIntegration tests actual API connectivity with custom base URL
// Run with: VC_API_BASE=http://localhost:8000 go test ./internal/ai/... -run TestBaseURLIntegration -v
func TestBaseURLIntegration(t *testing.T) {
	baseURL := os.Getenv("VC_API_BASE")
	if baseURL == "" {
		t.Skip("Skipping integration test: VC_API_BASE not set")
	}

	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey == "" {
		// For wrapper, we may not need a real key - use a placeholder
		apiKey = "test-wrapper-key"
	}

	cfg := &Config{
		APIKey:  apiKey,
		BaseURL: baseURL,
		Store:   newMockStorage(),
	}

	sup, err := NewSupervisor(cfg)
	if err != nil {
		t.Fatalf("NewSupervisor failed: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Make a simple API call
	resp, err := sup.CallAPI(ctx, "Say 'hello' and nothing else.", "", 50)
	if err != nil {
		t.Fatalf("CallAPI failed: %v", err)
	}

	if resp == nil {
		t.Fatal("CallAPI returned nil response")
	}

	t.Logf("API response received successfully via custom endpoint %s", baseURL)
	if len(resp.Content) > 0 {
		t.Logf("Response content: %+v", resp.Content[0])
	}
}
