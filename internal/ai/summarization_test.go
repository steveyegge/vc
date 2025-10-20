package ai

import (
	"context"
	"strings"
	"testing"

	"github.com/steveyegge/vc/internal/storage/sqlite"
	"github.com/steveyegge/vc/internal/types"
)

func TestSummarizeAgentOutput(t *testing.T) {
	// Skip if ANTHROPIC_API_KEY not set
	supervisor, err := createTestSupervisor(t)
	if err != nil {
		t.Skipf("Skipping test: %v", err)
	}

	issue := &types.Issue{
		ID:          "vc-test-summarize",
		Title:       "Implement feature X",
		Description: "Add new feature to handle user authentication",
	}

	tests := []struct {
		name           string
		output         string
		maxLength      int
		expectContains []string
	}{
		{
			name:      "empty output",
			output:    "",
			maxLength: 500,
			expectContains: []string{
				"no output",
			},
		},
		{
			name: "short output (no summarization needed)",
			output: "File modified: auth.go\n" +
				"Tests passed: TestAuth\n" +
				"Build successful",
			maxLength: 500,
			expectContains: []string{
				"auth.go",
				"TestAuth",
			},
		},
		{
			name: "long output with test results",
			output: strings.Repeat("Running tests...\n", 10) +
				"=== RUN   TestAuth\n" +
				"=== RUN   TestAuth/valid_credentials\n" +
				"--- PASS: TestAuth (0.01s)\n" +
				"    --- PASS: TestAuth/valid_credentials (0.00s)\n" +
				"=== RUN   TestAuth/invalid_credentials\n" +
				"--- PASS: TestAuth (0.01s)\n" +
				"    --- PASS: TestAuth/invalid_credentials (0.00s)\n" +
				"PASS\n" +
				"ok  	github.com/test/auth	0.123s\n" +
				strings.Repeat("Build output...\n", 20),
			maxLength: 500,
			expectContains: []string{
				"test", // Should mention tests
			},
		},
		{
			name: "build failure output",
			output: "go build ./...\n" +
				"# github.com/test/auth\n" +
				"./auth.go:42:15: undefined: validatePassword\n" +
				"./auth.go:45:20: cannot use credentials (variable of type Credentials) as type string in argument to hash.Sum\n" +
				"FAIL	github.com/test/auth [build failed]\n",
			maxLength: 500,
			expectContains: []string{
				"error", "fail", // Should mention the failure
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()

			summary, err := supervisor.SummarizeAgentOutput(ctx, issue, tt.output, tt.maxLength)
			if err != nil {
				t.Fatalf("SummarizeAgentOutput failed: %v", err)
			}

			// Check length constraint
			if len(summary) > tt.maxLength*2 { // Allow some slack (AI might exceed slightly)
				t.Errorf("Summary too long: got %d chars, expected ~%d", len(summary), tt.maxLength)
			}

			// Check for expected content (case-insensitive)
			summaryLower := strings.ToLower(summary)
			for _, expected := range tt.expectContains {
				if !strings.Contains(summaryLower, strings.ToLower(expected)) {
					t.Errorf("Summary missing expected content '%s'\nGot: %s", expected, summary)
				}
			}

			t.Logf("Summary (%d chars): %s", len(summary), summary)
		})
	}
}

func TestSummarizeAgentOutput_LargeOutput(t *testing.T) {
	// Skip if ANTHROPIC_API_KEY not set
	supervisor, err := createTestSupervisor(t)
	if err != nil {
		t.Skipf("Skipping test: %v", err)
	}

	issue := &types.Issue{
		ID:          "vc-test-large",
		Title:       "Large output test",
		Description: "Testing with very large output",
	}

	// Create a very large output (100k chars)
	var largeOutput strings.Builder
	largeOutput.WriteString("=== Beginning of execution ===\n")
	largeOutput.WriteString("Important context at the start\n\n")

	// Fill middle with noise
	for i := 0; i < 1000; i++ {
		largeOutput.WriteString(strings.Repeat("Some build output line...\n", 10))
	}

	// Add important stuff at the end
	largeOutput.WriteString("\n=== Tests ===\n")
	largeOutput.WriteString("All 47 tests passed\n")
	largeOutput.WriteString("=== Build successful ===\n")

	ctx := context.Background()
	summary, err := supervisor.SummarizeAgentOutput(ctx, issue, largeOutput.String(), 1000)
	if err != nil {
		t.Fatalf("SummarizeAgentOutput failed: %v", err)
	}

	// Should be much shorter than input
	if len(summary) > 5000 {
		t.Errorf("Summary not concise enough: got %d chars from %d char input",
			len(summary), largeOutput.Len())
	}

	// Should capture key info despite large input
	summaryLower := strings.ToLower(summary)
	if !strings.Contains(summaryLower, "test") {
		t.Error("Summary should mention tests from large output")
	}

	t.Logf("Large input: %d chars -> Summary: %d chars", largeOutput.Len(), len(summary))
	t.Logf("Summary: %s", summary)
}

// TestSummarizeAgentOutput_ErrorHandling tests that errors are properly returned
// without falling back to heuristics (ZFC compliance)
func TestSummarizeAgentOutput_ErrorHandling(t *testing.T) {
	// Create supervisor with invalid API key to force errors
	tmpDB := t.TempDir() + "/test.db"
	store, err := sqlite.New(tmpDB)
	if err != nil {
		t.Fatalf("Failed to create test store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	cfg := &Config{
		Store:  store,
		APIKey: "invalid-key-should-fail",
		Retry:  DefaultRetryConfig(),
	}

	supervisor, err := NewSupervisor(cfg)
	if err != nil {
		t.Fatalf("Failed to create supervisor: %v", err)
	}

	issue := &types.Issue{
		ID:          "vc-test-error",
		Title:       "Error handling test",
		Description: "Testing error handling",
	}

	ctx := context.Background()
	_, err = supervisor.SummarizeAgentOutput(ctx, issue, "test output", 1000)

	// Should return an error, not fall back to heuristics
	if err == nil {
		t.Error("Expected error with invalid API key, got nil")
	}

	// Error should mention retry attempts (ZFC compliance)
	if !strings.Contains(err.Error(), "retry") {
		t.Errorf("Error should mention retry attempts, got: %v", err)
	}
}

// Helper to create test supervisor
func createTestSupervisor(t *testing.T) (*Supervisor, error) {
	t.Helper()

	// Use temp SQLite database for testing
	tmpDB := t.TempDir() + "/test.db"
	store, err := sqlite.New(tmpDB)
	if err != nil {
		return nil, err
	}
	t.Cleanup(func() { _ = store.Close() })

	cfg := &Config{
		Store: store,
		// API key from environment
		Retry: DefaultRetryConfig(),
	}

	return NewSupervisor(cfg)
}
