package ai

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/steveyegge/vc/internal/storage/beads"
)

func TestParseDescription(t *testing.T) {
	// Skip if no API key (CI environments without key)
	if os.Getenv("ANTHROPIC_API_KEY") == "" {
		t.Skip("ANTHROPIC_API_KEY not set, skipping integration test")
	}

	// Create in-memory storage for testing
	ctx := context.Background()
	store, err := beads.NewVCStorage(ctx, ":memory:")
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer store.Close()

	// Create supervisor
	supervisor, err := NewSupervisor(&Config{
		Model: "claude-sonnet-4-5-20250929",
		Store: store,
	})
	if err != nil {
		t.Fatalf("Failed to create supervisor: %v", err)
	}

	tests := []struct {
		name                string
		description         string
		expectGoal          bool
		expectConstraints   bool
		minConstraints      int
		maxConstraints      int
		shouldContain       []string // Keywords that should appear in goal or constraints
	}{
		{
			name:              "simple goal without constraints",
			description:       "Refactor the database layer to use connection pooling for better performance and resource management",
			expectGoal:        true,
			expectConstraints: false,
			shouldContain:     []string{"database", "connection pooling"},
		},
		{
			name:              "goal with explicit constraints",
			description:       "Improve test coverage from 46% to 80%. Must not slow down test suite beyond 5s baseline.",
			expectGoal:        true,
			expectConstraints: true,
			minConstraints:    1,
			maxConstraints:    2,
			shouldContain:     []string{"test coverage", "80", "5"}, // Just "5" to allow "5s" or "5 seconds"
		},
		{
			name:              "goal with multiple constraints",
			description:       "Add user authentication with OAuth2. Need to support GitHub and Google providers. Must maintain backward compatibility with existing session system.",
			expectGoal:        true,
			expectConstraints: true,
			minConstraints:    2,
			maxConstraints:    3,
			shouldContain:     []string{"OAuth2", "GitHub", "Google", "backward compatibility"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			goal, constraints, err := supervisor.ParseDescription(ctx, tt.description)

			if err != nil {
				t.Fatalf("ParseDescription failed: %v", err)
			}

			// Validate goal exists
			if tt.expectGoal && goal == "" {
				t.Error("Expected non-empty goal, got empty string")
			}

			// Validate constraints
			if tt.expectConstraints {
				if len(constraints) < tt.minConstraints {
					t.Errorf("Expected at least %d constraints, got %d", tt.minConstraints, len(constraints))
				}
				if tt.maxConstraints > 0 && len(constraints) > tt.maxConstraints {
					t.Errorf("Expected at most %d constraints, got %d", tt.maxConstraints, len(constraints))
				}
			} else {
				if len(constraints) > 0 {
					t.Logf("Note: Got %d constraints even though not expected (may be inferred): %v", len(constraints), constraints)
				}
			}

			// Check for expected keywords
			combinedText := strings.ToLower(goal)
			for _, c := range constraints {
				combinedText += " " + strings.ToLower(c)
			}

			for _, keyword := range tt.shouldContain {
				if !strings.Contains(combinedText, strings.ToLower(keyword)) {
					t.Errorf("Expected keyword '%s' not found in goal or constraints", keyword)
					t.Logf("Goal: %s", goal)
					t.Logf("Constraints: %v", constraints)
				}
			}

			// Log results for debugging
			t.Logf("Parsed goal: %s", goal)
			if len(constraints) > 0 {
				t.Logf("Parsed constraints:")
				for i, c := range constraints {
					t.Logf("  %d. %s", i+1, c)
				}
			} else {
				t.Logf("No constraints extracted")
			}
		})
	}
}

func TestParseDescription_EmptyInput(t *testing.T) {
	// Skip if no API key (CI environments without key)
	if os.Getenv("ANTHROPIC_API_KEY") == "" {
		t.Skip("ANTHROPIC_API_KEY not set, skipping integration test")
	}

	// Create in-memory storage for testing
	ctx := context.Background()
	store, err := beads.NewVCStorage(ctx, ":memory:")
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer store.Close()

	// Create supervisor
	supervisor, err := NewSupervisor(&Config{
		Model: "claude-sonnet-4-5-20250929",
		Store: store,
	})
	if err != nil {
		t.Fatalf("Failed to create supervisor: %v", err)
	}

	// Test empty description
	_, _, err = supervisor.ParseDescription(ctx, "")
	if err == nil {
		t.Error("Expected error for empty description, got nil")
	}
	if !strings.Contains(err.Error(), "cannot be empty") {
		t.Errorf("Expected 'cannot be empty' error, got: %v", err)
	}

	// Test whitespace-only description
	_, _, err = supervisor.ParseDescription(ctx, "   \t\n  ")
	if err == nil {
		t.Error("Expected error for whitespace description, got nil")
	}
	if !strings.Contains(err.Error(), "cannot be empty") {
		t.Errorf("Expected 'cannot be empty' error, got: %v", err)
	}
}
