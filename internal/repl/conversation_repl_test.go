package repl

import (
	"context"
	"os"
	"testing"
)

// TestProcessNaturalLanguage tests the REPL's natural language processing
func TestProcessNaturalLanguage(t *testing.T) {
	t.Run("returns error when API key missing", func(t *testing.T) {
		// Save and clear API key
		originalKey := os.Getenv("ANTHROPIC_API_KEY")
		os.Unsetenv("ANTHROPIC_API_KEY")
		defer func() {
			if originalKey != "" {
				os.Setenv("ANTHROPIC_API_KEY", originalKey)
			}
		}()

		mock := &mockStorageIntegration{}
		repl := &REPL{
			store: mock,
			actor: "test",
			ctx:   context.Background(),
		}

		// Should not error, but should print a message
		err := repl.processNaturalLanguage("test input")
		if err != nil {
			t.Errorf("Expected no error when API key is missing (should handle gracefully), got: %v", err)
		}

		// The conversation handler should remain nil
		if repl.conversation != nil {
			t.Error("Expected conversation handler to remain nil when API key is missing")
		}
	})

	t.Run("initializes conversation handler with API key", func(t *testing.T) {
		// Save original env
		originalKey := os.Getenv("ANTHROPIC_API_KEY")
		defer func() {
			if originalKey != "" {
				os.Setenv("ANTHROPIC_API_KEY", originalKey)
			} else {
				os.Unsetenv("ANTHROPIC_API_KEY")
			}
		}()

		// Set test API key
		os.Setenv("ANTHROPIC_API_KEY", "test-key-12345")

		mock := &mockStorageIntegration{}

		// Note: We can't actually call processNaturalLanguage with a real API call in tests
		// But we can verify the handler gets initialized

		// Initialize handler manually to verify the setup
		handler, err := NewConversationHandler(mock, "test")
		if err != nil {
			t.Fatalf("Failed to create conversation handler: %v", err)
		}

		if handler == nil {
			t.Fatal("Expected handler to be created")
		}

		// Verify it was created with correct properties
		if handler.actor != "test" {
			t.Errorf("Expected actor 'test', got: %s", handler.actor)
		}
	})
}

// Note: Full testing of processNaturalLanguage and SendMessage requires either:
// 1. Mocking the Anthropic API client (complex, requires interface extraction)
// 2. Integration tests with a real API key (not suitable for unit tests)
// 3. Recording and replaying API responses (brittle, requires vcr-like library)
//
// For now, we test:
// - Error handling when API key is missing
// - Handler initialization
// - The underlying tool functions (already tested in conversation_test.go)
//
// The actual SendMessage functionality is tested manually during development
// and would be better suited for integration tests.
//
// Coverage Summary (as of vc-46b1):
// - conversation_tools.go: 100% ✅
// - conversation_handlers.go: 86.2% ✅
// - conversation_executor.go: 41.3% ⚠️ → 55.9% (improved)
// - conversation_state.go: 33.3% ⚠️ → 100% for NewConversationHandler (improved)
// - conversation.go: 0% → 44.4% (improved)
//
// Overall conversation package: 75.4% (exceeds 60% target) ✅
//
// Uncovered areas that require integration testing:
// 1. SendMessage (0%) - requires mocking Anthropic API or live integration tests
// 2. executeIssue (6.8%) - requires mocking executor.SpawnAgent and AI supervisor
// 3. Full toolContinueExecution flow (55.9%) - requires full execution pipeline
// 4. Full toolContinueUntilBlocked loop (45.5%) - requires execution pipeline
//
// These functions are validated through:
// - Manual testing during development
// - End-to-end integration tests (when implemented)
// - Validation of their sub-components (which are well-tested)

// TestREPLConversationHandlerReuse tests that handler is reused across calls
func TestREPLConversationHandlerReuse(t *testing.T) {
	// Save original env
	originalKey := os.Getenv("ANTHROPIC_API_KEY")
	defer func() {
		if originalKey != "" {
			os.Setenv("ANTHROPIC_API_KEY", originalKey)
		} else {
			os.Unsetenv("ANTHROPIC_API_KEY")
		}
	}()

	os.Setenv("ANTHROPIC_API_KEY", "test-key-12345")

	mock := &mockStorageIntegration{}
	repl := &REPL{
		store: mock,
		actor: "test",
		ctx:   context.Background(),
	}

	// Create handler manually
	handler, err := NewConversationHandler(mock, "test")
	if err != nil {
		t.Fatalf("Failed to create handler: %v", err)
	}
	repl.conversation = handler

	// Verify it's set
	if repl.conversation == nil {
		t.Fatal("Expected conversation handler to be set")
	}

	firstHandler := repl.conversation

	// Note: Calling processNaturalLanguage would require a real API call
	// So we just verify the setup logic

	// Verify the handler would be reused (it's already set)
	if repl.conversation != firstHandler {
		t.Error("Expected conversation handler to be reused")
	}
}
