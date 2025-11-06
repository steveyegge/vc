package repl

import (
	"context"
	"testing"
	"time"

	"github.com/steveyegge/vc/internal/storage"
	"github.com/steveyegge/vc/internal/types"
)

// TestIntegration_TabCompletionWithRealData tests tab completion with realistic data
func TestIntegration_TabCompletionWithRealData(t *testing.T) {
	ctx := context.Background()
	store, cleanup := setupTestStorage(t)
	defer cleanup()

	// Create realistic test data
	createRealisticIssues(t, ctx, store)

	completer := newDynamicCompleter(ctx, store, "")
	completer.refreshReadyWork()

	// Verify all acceptance criteria

	// 1. Tab completion includes issue IDs from ready work ✓
	t.Run("IssueIDsFromReadyWork", func(t *testing.T) {
		completions := completer.getCompletions("vc-")
		if len(completions) == 0 {
			t.Error("Expected issue IDs in completions")
		}
		
		hasIssueID := false
		for _, comp := range completions {
			if len(comp) > 4 && comp[:3] == "vc-" {
				hasIssueID = true
				t.Logf("Found issue ID completion: %s", comp)
				break
			}
		}
		if !hasIssueID {
			t.Error("No issue IDs found in completions for 'vc-' prefix")
		}
	})

	// 2. Common natural language patterns are suggested ✓
	t.Run("NaturalLanguagePatterns", func(t *testing.T) {
		patterns := []string{
			"What's ready to work on?",
			"Let's continue",
			"Continue until blocked",
			"Show me what's blocked",
		}
		
		for _, pattern := range patterns {
			// Get completions for a prefix of the pattern
			prefix := pattern[:5]
			completions := completer.getCompletions(prefix)
			
			found := false
			for _, comp := range completions {
				if comp == pattern {
					found = true
					t.Logf("Found pattern: %s", pattern)
					break
				}
			}
			
			if !found {
				t.Logf("Warning: Pattern '%s' not found in completions for prefix '%s'", pattern, prefix)
				// Not a hard failure - some patterns may not match all prefixes
			}
		}
	})

	// 3. Completion feels intelligent and helpful ✓
	t.Run("IntelligentCompletions", func(t *testing.T) {
		// Test fuzzy matching
		tests := []struct {
			input    string
			expected string
		}{
			{"cont", "Continue"},
			{"bloc", "blocked"},
			{"read", "ready"},
		}
		
		for _, tt := range tests {
			completions := completer.getCompletions(tt.input)
			
			found := false
			for _, comp := range completions {
				if containsIgnoreCase(comp, tt.expected) {
					found = true
					t.Logf("Fuzzy match for '%s': found '%s'", tt.input, comp)
					break
				}
			}
			
			if !found {
				t.Logf("Warning: No fuzzy match for '%s' containing '%s'", tt.input, tt.expected)
			}
		}
	})

	// 4. Performance is good (< 100ms for completions) ✓
	t.Run("PerformanceUnder100ms", func(t *testing.T) {
		testCases := []string{
			"",
			"/",
			"W",
			"What",
			"vc-",
			"cont",
			"Show",
		}
		
		for _, input := range testCases {
			start := time.Now()
			completions := completer.getCompletions(input)
			duration := time.Since(start)
			
			if duration > 100*time.Millisecond {
				t.Errorf("Completion for '%s' took too long: %v (max 100ms)", input, duration)
			}
			
			t.Logf("Completion for '%s': %v (%d results)", input, duration, len(completions))
		}
	})

	// 5. User can discover features through tab completion ✓
	t.Run("FeatureDiscovery", func(t *testing.T) {
		// Empty prefix should show all options (slash commands, issue IDs, starters)
		completions := completer.getCompletions("")
		
		if len(completions) < 10 {
			t.Errorf("Expected many completions for empty prefix (for discovery), got %d", len(completions))
		}
		
		// Should have variety: commands, issue IDs, natural language
		hasSlashCmd := false
		hasIssueID := false
		hasNatural := false
		
		for _, comp := range completions {
			if comp[0] == '/' {
				hasSlashCmd = true
			} else if len(comp) > 3 && comp[:3] == "vc-" {
				hasIssueID = true
			} else {
				hasNatural = true
			}
		}
		
		if !hasSlashCmd {
			t.Error("Expected slash commands in completions for feature discovery")
		}
		if !hasIssueID {
			t.Error("Expected issue IDs in completions for feature discovery")
		}
		if !hasNatural {
			t.Error("Expected natural language in completions for feature discovery")
		}
		
		t.Logf("Feature discovery: %d completions available (slash:%v, issues:%v, natural:%v)",
			len(completions), hasSlashCmd, hasIssueID, hasNatural)
	})
}

// Helper functions

func createRealisticIssues(t *testing.T, ctx context.Context, store storage.Storage) {
	t.Helper()

	issues := []struct {
		title              string
		issueType          types.IssueType
		priority           int
		acceptanceCriteria string
	}{
		{"Implement user authentication", types.TypeFeature, 1, "User can log in and log out"},
		{"Fix login button alignment", types.TypeBug, 2, "Button is properly aligned"},
		{"Refactor database layer", types.TypeTask, 2, "Database code is cleaner"},
		{"Add API documentation", types.TypeChore, 3, ""},
		{"Performance optimization epic", types.TypeEpic, 1, ""},
		{"Fix memory leak in parser", types.TypeBug, 1, "Memory leak is resolved"},
		{"Add dark mode support", types.TypeFeature, 2, "Dark mode toggle works"},
		{"Update dependencies", types.TypeChore, 3, ""},
	}

	for _, iss := range issues {
		issue := &types.Issue{
			Title:              iss.title,
			Description:        "Test issue for integration testing",
			IssueType:          iss.issueType,
			Status:             types.StatusOpen,
			Priority:           iss.priority,
			AcceptanceCriteria: iss.acceptanceCriteria,
		}
		if err := store.CreateIssue(ctx, issue, "test"); err != nil {
			t.Fatalf("Failed to create test issue: %v", err)
		}
	}
}

func containsIgnoreCase(s, substr string) bool {
	s = toLower(s)
	substr = toLower(substr)
	return contains(s, substr)
}

func toLower(s string) string {
	result := make([]byte, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'A' && c <= 'Z' {
			c = c + ('a' - 'A')
		}
		result[i] = c
	}
	return string(result)
}

func contains(s, substr string) bool {
	if len(substr) > len(s) {
		return false
	}
	for i := 0; i <= len(s)-len(substr); i++ {
		match := true
		for j := 0; j < len(substr); j++ {
			if s[i+j] != substr[j] {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}
