package executor

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/steveyegge/vc/internal/events"
	"github.com/steveyegge/vc/internal/storage"
	"github.com/steveyegge/vc/internal/types"
)

// TestCircuitBreakerNoDeadlock verifies that the circuit breaker can trigger
// without deadlocking when called under concurrent load (vc-5783)
func TestCircuitBreakerNoDeadlock(t *testing.T) {
	// Setup test agent with mock dependencies
	cfg := storage.DefaultConfig()
	cfg.Path = ":memory:"

	ctx := context.Background()
	store, err := storage.NewStorage(ctx, cfg)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer func() { _ = store.Close() }()

	issue := &types.Issue{
		ID:          "vc-test-circuit-breaker",
		Title:       "Test Circuit Breaker Deadlock Fix",
		Description: "Test that circuit breaker doesn't deadlock under concurrent load",
		IssueType:   types.TypeTask,
		Status:      types.StatusOpen,
		Priority:    1,
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}
	if err := store.CreateIssue(ctx, issue, "test"); err != nil {
		t.Fatalf("Failed to create issue: %v", err)
	}

	executorID := "test-executor"
	agentID := "test-agent"

	agent := &Agent{
		config: AgentConfig{
			Issue:      issue,
			Store:      store,
			ExecutorID: executorID,
			AgentID:    agentID,
			StreamJSON: true,
		},
		parser:         events.NewOutputParser(issue.ID, executorID, agentID),
		ctx:            ctx,
		totalReadCount: 0,
		fileReadCounts: make(map[string]int),
		loopDetected:   false,
		loopReason:     "",
	}

	// Create a test file path that we'll "read" multiple times
	testFilePath := "/test/file.go"

	// Simulate concurrent reads that will trigger the circuit breaker
	// This tests the scenario where:
	// 1. convertJSONToEvent is parsing events and calling checkCircuitBreaker
	// 2. Other goroutines might be trying to acquire the mutex
	var wg sync.WaitGroup
	numGoroutines := 10
	readsPerGoroutine := 3 // This will exceed maxSameFileReads (20) quickly

	// Channel to signal when circuit breaker triggered
	circuitBreakerTriggered := make(chan bool, 1)

	// Mutex to protect shared state during concurrent testing
	var testMu sync.Mutex
	circuitTriggered := false

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(goroutineID int) {
			defer wg.Done()

			for j := 0; j < readsPerGoroutine; j++ {
				// Check if circuit breaker already triggered
				testMu.Lock()
				if circuitTriggered {
					testMu.Unlock()
					return
				}
				testMu.Unlock()

				// Create a Read tool event (Amp's actual format)
				msg := AgentMessage{
					Type:      "assistant",
					SessionID: "T-test-circuit",
					Message: &AssistantMessage{
						Type: "message",
						Role: "assistant",
						Content: []MessageContent{
							{Type: "text", Text: "Reading file"},
							{
								Type:  "tool_use",
								ID:    fmt.Sprintf("toolu_%d_%d", goroutineID, j),
								Name:  "Read",
								Input: map[string]interface{}{"path": testFilePath},
							},
						},
						StopReason: "tool_use",
					},
				}

				rawJSON, err := json.Marshal(msg)
				if err != nil {
					t.Errorf("Failed to marshal JSON: %v", err)
					return
				}
				rawLine := string(rawJSON)

				// Convert to event - this will trigger checkCircuitBreaker
				event := agent.convertJSONToEvent(msg, rawLine)

				// Check if circuit breaker was triggered
				agent.mu.Lock()
				if agent.loopDetected {
					agent.mu.Unlock()
					testMu.Lock()
					circuitTriggered = true
					testMu.Unlock()
					select {
					case circuitBreakerTriggered <- true:
					default:
					}
					return
				}
				agent.mu.Unlock()

				if event == nil {
					// Circuit breaker may have triggered
					testMu.Lock()
					circuitTriggered = true
					testMu.Unlock()
					select {
					case circuitBreakerTriggered <- true:
					default:
					}
					return
				}

				// Small delay to increase chance of concurrent access
				time.Sleep(1 * time.Millisecond)
			}
		}(i)
	}

	// Wait for all goroutines to complete with a timeout
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	// Use a timeout to detect potential deadlocks
	timeout := time.After(5 * time.Second)
	select {
	case <-done:
		// Success - all goroutines completed without deadlock
		t.Log("All goroutines completed without deadlock")
	case <-timeout:
		t.Fatal("Test timed out - potential deadlock detected")
	}

	// Verify that circuit breaker was actually triggered
	agent.mu.Lock()
	wasTriggered := agent.loopDetected
	reason := agent.loopReason
	agent.mu.Unlock()

	if !wasTriggered {
		t.Error("Expected circuit breaker to be triggered, but it wasn't")
	} else {
		t.Logf("Circuit breaker correctly triggered: %s", reason)
	}
}

// TestCircuitBreakerTerminatesAgent verifies that the agent terminates cleanly
// when the circuit breaker is triggered (vc-5783)
func TestCircuitBreakerTerminatesAgent(t *testing.T) {
	// Setup test agent with mock dependencies
	cfg := storage.DefaultConfig()
	cfg.Path = ":memory:"

	ctx := context.Background()
	store, err := storage.NewStorage(ctx, cfg)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer func() { _ = store.Close() }()

	issue := &types.Issue{
		ID:          "vc-test-circuit-terminate",
		Title:       "Test Circuit Breaker Terminates Agent",
		Description: "Test that agent terminates when circuit breaker triggers",
		IssueType:   types.TypeTask,
		Status:      types.StatusOpen,
		Priority:    1,
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}
	if err := store.CreateIssue(ctx, issue, "test"); err != nil {
		t.Fatalf("Failed to create issue: %v", err)
	}

	executorID := "test-executor"
	agentID := "test-agent"

	agent := &Agent{
		config: AgentConfig{
			Issue:      issue,
			Store:      store,
			ExecutorID: executorID,
			AgentID:    agentID,
			StreamJSON: true,
		},
		parser:         events.NewOutputParser(issue.ID, executorID, agentID),
		ctx:            ctx,
		totalReadCount: 0,
		fileReadCounts: make(map[string]int),
		loopDetected:   false,
		loopReason:     "",
	}

	// Trigger the circuit breaker by reading the same file too many times
	testFilePath := "/test/same-file.go"

	// Read the same file maxSameFileReads + 1 times to trigger the circuit breaker
	for i := 0; i <= maxSameFileReads+1; i++ {
		msg := AgentMessage{
			Type:      "assistant",
			SessionID: "T-test-circuit",
			Message: &AssistantMessage{
				Type: "message",
				Role: "assistant",
				Content: []MessageContent{
					{Type: "text", Text: "Reading file"},
					{
						Type:  "tool_use",
						ID:    fmt.Sprintf("toolu_%d", i),
						Name:  "Read",
						Input: map[string]interface{}{"path": testFilePath},
					},
				},
				StopReason: "tool_use",
			},
		}

		rawJSON, err := json.Marshal(msg)
		if err != nil {
			t.Fatalf("Failed to marshal JSON: %v", err)
		}
		rawLine := string(rawJSON)

		event := agent.convertJSONToEvent(msg, rawLine)

		// After exceeding the limit, event should be nil and loopDetected should be true
		if i > maxSameFileReads {
			if event != nil {
				t.Errorf("Expected nil event after exceeding limit, got %v", event)
			}

			agent.mu.Lock()
			loopDetected := agent.loopDetected
			loopReason := agent.loopReason
			agent.mu.Unlock()

			if !loopDetected {
				t.Error("Expected loopDetected to be true after exceeding limit")
			}
			if loopReason == "" {
				t.Error("Expected loopReason to be set after exceeding limit")
			}
			break
		}
	}

	// Verify final state
	agent.mu.Lock()
	finalDetected := agent.loopDetected
	finalReason := agent.loopReason
	finalReadCount := agent.fileReadCounts[testFilePath]
	agent.mu.Unlock()

	if !finalDetected {
		t.Error("Expected loopDetected to be true")
	}
	if finalReason == "" {
		t.Error("Expected loopReason to be set")
	}
	if finalReadCount <= maxSameFileReads {
		t.Errorf("Expected read count > %d, got %d", maxSameFileReads, finalReadCount)
	}

	t.Logf("Circuit breaker triggered correctly: %s (read count: %d)", finalReason, finalReadCount)
}
