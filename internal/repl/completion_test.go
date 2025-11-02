package repl

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/steveyegge/vc/internal/storage"
	"github.com/steveyegge/vc/internal/types"
)

func TestDynamicCompleter_Performance(t *testing.T) {
	ctx := context.Background()
	store, cleanup := setupTestStorage(t)
	defer cleanup()

	// Create some test issues
	createTestIssues(t, ctx, store, 10)

	completer := newDynamicCompleter(ctx, store, "")

	tests := []struct {
		name  string
		input string
	}{
		{"empty", ""},
		{"slash", "/"},
		{"prefix_w", "W"},
		{"prefix_what", "What"},
		{"prefix_show", "Show"},
		{"prefix_cont", "cont"},
		{"issue_prefix", "vc-"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			start := time.Now()
			completer.getCompletions(tt.input)
			duration := time.Since(start)

			// Performance requirement: < 100ms
			if duration > 100*time.Millisecond {
				t.Errorf("Completion too slow: %v (max 100ms)", duration)
			}
		})
	}
}

func TestDynamicCompleter_IssueIDCompletion(t *testing.T) {
	ctx := context.Background()
	store, cleanup := setupTestStorage(t)
	defer cleanup()

	// Create test issues with known IDs
	createTestIssues(t, ctx, store, 5)

	completer := newDynamicCompleter(ctx, store, "")
	completer.refreshReadyWork()

	completions := completer.getCompletions("vc-")
	
	// Should have issue IDs in completions
	hasIssueID := false
	for _, comp := range completions {
		if strings.HasPrefix(comp, "vc-") && len(comp) > 3 {
			hasIssueID = true
			break
		}
	}

	if !hasIssueID {
		t.Error("Expected issue IDs in completions for 'vc-' prefix")
	}
}

func TestDynamicCompleter_SlashCommands(t *testing.T) {
	ctx := context.Background()
	store, cleanup := setupTestStorage(t)
	defer cleanup()

	completer := newDynamicCompleter(ctx, store, "")

	completions := completer.getCompletions("/")
	
	expectedCommands := []string{"/quit", "/exit", "/help"}
	for _, cmd := range expectedCommands {
		found := false
		for _, comp := range completions {
			if comp == cmd {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Expected slash command %q in completions", cmd)
		}
	}
}

func TestDynamicCompleter_NaturalLanguage(t *testing.T) {
	ctx := context.Background()
	store, cleanup := setupTestStorage(t)
	defer cleanup()

	completer := newDynamicCompleter(ctx, store, "")

	completions := completer.getCompletions("What")
	
	// Should have natural language starters
	hasNatural := false
	for _, comp := range completions {
		if strings.HasPrefix(comp, "What's") {
			hasNatural = true
			break
		}
	}

	if !hasNatural {
		t.Error("Expected natural language completions for 'What' prefix")
	}
}

func TestDynamicCompleter_FuzzyMatching(t *testing.T) {
	ctx := context.Background()
	store, cleanup := setupTestStorage(t)
	defer cleanup()

	completer := newDynamicCompleter(ctx, store, "")

	tests := []struct {
		input    string
		expected string
	}{
		{"cont", "Continue"},
		{"bloc", "What's blocked?"},
		{"read", "What's ready to work on?"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			completions := completer.getCompletions(tt.input)
			
			found := false
			for _, comp := range completions {
				if strings.Contains(comp, tt.expected) || comp == tt.expected {
					found = true
					break
				}
			}

			if !found {
				t.Errorf("Expected fuzzy match %q for input %q, got: %v", 
					tt.expected, tt.input, completions)
			}
		})
	}
}

func TestDynamicCompleter_HistoryBased(t *testing.T) {
	ctx := context.Background()
	store, cleanup := setupTestStorage(t)
	defer cleanup()

	// Create a temporary history file
	tmpDir := t.TempDir()
	historyPath := filepath.Join(tmpDir, "test_history")
	
	// Write test history with repeated commands
	history := `What's ready to work on?
What's ready to work on?
What's ready to work on?
Let's continue
Let's continue
Show me what's blocked
/quit
/help
`
	if err := os.WriteFile(historyPath, []byte(history), 0644); err != nil {
		t.Fatalf("Failed to write test history: %v", err)
	}

	completer := newDynamicCompleter(ctx, store, historyPath)
	historyCompletions := completer.getHistoryBasedCompletions("")

	// Should include frequently used commands
	expectedCommands := []string{"What's ready to work on?", "Let's continue"}
	for _, expected := range expectedCommands {
		found := false
		for _, comp := range historyCompletions {
			if comp == expected {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Expected history-based completion %q", expected)
		}
	}
}

func TestDynamicCompleter_Sorting(t *testing.T) {
	completions := []string{
		"What's ready?",
		"vc-123",
		"/quit",
		"Continue",
		"vc-456",
		"/help",
	}

	sortCompletions(completions)

	// Check order: slash commands, then issue IDs, then natural language
	
	// First two should be slash commands
	if !strings.HasPrefix(completions[0], "/") {
		t.Errorf("Expected first completion to be slash command, got: %s", completions[0])
	}
	if !strings.HasPrefix(completions[1], "/") {
		t.Errorf("Expected second completion to be slash command, got: %s", completions[1])
	}

	// Next should be issue IDs
	foundIssueIdx := -1
	for i := 2; i < len(completions); i++ {
		if strings.HasPrefix(completions[i], "vc-") {
			foundIssueIdx = i
			break
		}
	}
	if foundIssueIdx == -1 {
		t.Error("Expected issue IDs after slash commands")
	}
}

func TestDynamicCompleter_DoInterface(t *testing.T) {
	ctx := context.Background()
	store, cleanup := setupTestStorage(t)
	defer cleanup()

	createTestIssues(t, ctx, store, 3)

	completer := newDynamicCompleter(ctx, store, "")
	completer.refreshReadyWork()

	tests := []struct {
		name      string
		line      string
		expectLen int // expect at least this many matches
	}{
		{"slash_commands", "/", 3},
		{"natural_what", "What", 1},
		{"issue_prefix", "vc-", 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			line := []rune(tt.line)
			matches, length := completer.Do(line, len(line))

			if len(matches) < tt.expectLen {
				t.Errorf("Expected at least %d matches for %q, got %d", 
					tt.expectLen, tt.line, len(matches))
			}

			if length != len(line) {
				t.Errorf("Expected length %d, got %d", len(line), length)
			}
		})
	}
}

func TestDynamicCompleter_CachingPerformance(t *testing.T) {
	ctx := context.Background()
	store, cleanup := setupTestStorage(t)
	defer cleanup()

	createTestIssues(t, ctx, store, 20)

	completer := newDynamicCompleter(ctx, store, "")
	completer.cacheDuration = 1 * time.Second

	// First call - should populate cache
	start := time.Now()
	completer.refreshReadyWork()
	firstDuration := time.Since(start)

	// Second call immediately - should use cache (no DB query)
	start = time.Now()
	completions := completer.getCompletions("vc-")
	secondDuration := time.Since(start)

	// Second call should be faster (using cache)
	// This is a soft check - just ensure it didn't take unreasonably long
	if secondDuration > 50*time.Millisecond {
		t.Errorf("Cached completion took too long: %v", secondDuration)
	}

	// Should have completions
	if len(completions) == 0 {
		t.Error("Expected completions from cache")
	}

	t.Logf("First refresh: %v, Cached call: %v", firstDuration, secondDuration)
}

// Helper functions

func setupTestStorage(t *testing.T) (storage.Storage, func()) {
	t.Helper()
	
	ctx := context.Background()
	cfg := &storage.Config{Path: ":memory:"}
	store, err := storage.NewStorage(ctx, cfg)
	if err != nil {
		t.Fatalf("Failed to create test storage: %v", err)
	}

	cleanup := func() {
		if err := store.Close(); err != nil {
			t.Errorf("Failed to close storage: %v", err)
		}
	}

	return store, cleanup
}

func createTestIssues(t *testing.T, ctx context.Context, store storage.Storage, count int) {
	t.Helper()
	
	for i := 0; i < count; i++ {
		issue := &types.Issue{
			IssueType:   types.TypeTask,
			Title:       "Test issue",
			Description: "Test description",
			Status:      types.StatusOpen,
			Priority:    2,
		}
		if err := store.CreateIssue(ctx, issue, "test"); err != nil {
			t.Fatalf("Failed to create test issue: %v", err)
		}
	}
}
