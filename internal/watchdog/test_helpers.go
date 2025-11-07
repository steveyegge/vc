//go:build integration

package watchdog

import (
	"os"
	"testing"

	"github.com/steveyegge/vc/internal/ai"
)

// createTestSupervisor creates a supervisor for testing
// If ANTHROPIC_API_KEY is set, uses real AI calls; otherwise uses a test key (which will fail API calls)
func createTestSupervisor(t *testing.T) *ai.Supervisor {
	t.Helper()
	store := &mockStorage{}

	// Use real API key from environment if available, otherwise use test key
	// When test key is used, tests that call the AI will skip or fail gracefully
	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey == "" {
		apiKey = "test-key"
	}

	supervisor, err := ai.NewSupervisor(&ai.Config{
		APIKey: apiKey,
		Store:  store,
	})
	if err != nil {
		t.Fatalf("failed to create supervisor: %v", err)
	}
	return supervisor
}
