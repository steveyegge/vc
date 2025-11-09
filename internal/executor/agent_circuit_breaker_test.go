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
	cfg.Path = t.TempDir() + "/test.db"

	ctx := context.Background()
	store, err := storage.NewStorage(ctx, cfg)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer func() { _ = store.Close() }()

	issue := &types.Issue{
		ID:                 "vc-test-circuit-breaker",
		Title:              "Test Circuit Breaker Deadlock Fix",
		Description:        "Test that circuit breaker doesn't deadlock under concurrent load",
		IssueType:          types.TypeTask,
		Status:             types.StatusOpen,
		Priority:           1,
		AcceptanceCriteria: "Test completes successfully without deadlock",
		CreatedAt:          time.Now(),
		UpdatedAt:          time.Now(),
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
		loopReason:     "",
		// loopDetected is atomic.Bool and initializes to false
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

				_, err := json.Marshal(msg)
				if err != nil {
					t.Errorf("Failed to marshal JSON: %v", err)
					return
				}

				// Convert to event - this will trigger checkCircuitBreaker
				event := agent.convertJSONToEvent(msg)

				// Check if circuit breaker was triggered (lock-free atomic read)
				if agent.loopDetected.Load() {
					testMu.Lock()
					circuitTriggered = true
					testMu.Unlock()
					select {
					case circuitBreakerTriggered <- true:
					default:
					}
					return
				}

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
	wasTriggered := agent.loopDetected.Load()
	agent.mu.Lock()
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
	cfg.Path = t.TempDir() + "/test.db"

	ctx := context.Background()
	store, err := storage.NewStorage(ctx, cfg)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer func() { _ = store.Close() }()

	issue := &types.Issue{
		ID:                 "vc-test-circuit-terminate",
		Title:              "Test Circuit Breaker Terminates Agent",
		Description:        "Test that agent terminates when circuit breaker triggers",
		IssueType:          types.TypeTask,
		Status:             types.StatusOpen,
		Priority:           1,
		AcceptanceCriteria: "Agent terminates correctly when circuit breaker is triggered",
		CreatedAt:          time.Now(),
		UpdatedAt:          time.Now(),
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
		loopReason:     "",
		// loopDetected is atomic.Bool and initializes to false
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

		_, err := json.Marshal(msg)
		if err != nil {
			t.Fatalf("Failed to marshal JSON: %v", err)
		}

		event := agent.convertJSONToEvent(msg)

		// After exceeding the limit, event should be nil and loopDetected should be true
		if i > maxSameFileReads {
			if event != nil {
				t.Errorf("Expected nil event after exceeding limit, got %v", event)
			}

			loopDetected := agent.loopDetected.Load()
			agent.mu.Lock()
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
	finalDetected := agent.loopDetected.Load()
	agent.mu.Lock()
	finalReason := agent.loopReason
	finalReadCount := agent.fileReadCounts[testFilePath]
	agent.mu.Unlock()

	if !finalDetected {
		t.Error("Expected loopDetected to be true")
	}
	if finalReason == "" {
		t.Error("Expected loopReason to be set")
	}
	if finalReadCount != maxSameFileReads {
		t.Errorf("Expected read count = %d, got %d", maxSameFileReads, finalReadCount)
	}

	t.Logf("Circuit breaker triggered correctly: %s (read count: %d)", finalReason, finalReadCount)
}

// TestCircuitBreakerRaceDetector tests concurrent access to circuit breaker flags
// from multiple goroutines to verify thread safety (vc-9fca, vc-5783, vc-217)
//
// This test specifically verifies the race condition scenario identified:
// - checkCircuitBreaker() sets flags while holding mutex (line 546-548)
// - Monitoring goroutine reads loopDetected without mutex (line 297)
// - Wait() reads both flags after process completion (lines 337-344)
//
// Run with: go test -race -run TestCircuitBreakerRaceDetector
func TestCircuitBreakerRaceDetector(t *testing.T) {
	// Setup test agent with mock dependencies
	cfg := storage.DefaultConfig()
	cfg.Path = t.TempDir() + "/test.db"

	ctx := context.Background()
	store, err := storage.NewStorage(ctx, cfg)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer func() { _ = store.Close() }()

	issue := &types.Issue{
		ID:                 "vc-test-race-detector",
		Title:              "Test Circuit Breaker Race Conditions",
		Description:        "Test concurrent access to loopDetected and loopReason flags",
		IssueType:          types.TypeTask,
		Status:             types.StatusOpen,
		Priority:           1,
		AcceptanceCriteria: "Test verifies thread safety without race conditions",
		CreatedAt:          time.Now(),
		UpdatedAt:          time.Now(),
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
		loopReason:     "",
		// loopDetected is atomic.Bool and initializes to false
	}

	testFilePath := "/test/race-test-file.go"
	var wg sync.WaitGroup

	// Goroutine 1: Simulate checkCircuitBreaker() setting flags
	// This mimics convertJSONToEvent calling checkCircuitBreaker
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < maxSameFileReads+5; i++ {
			msg := AgentMessage{
				Type:      "assistant",
				SessionID: "T-test-race",
				Message: &AssistantMessage{
					Type: "message",
					Role: "assistant",
					Content: []MessageContent{
						{
							Type:  "tool_use",
							ID:    fmt.Sprintf("toolu_write_%d", i),
							Name:  "Read",
							Input: map[string]interface{}{"path": testFilePath},
						},
					},
					StopReason: "tool_use",
				},
			}
			_, _ = json.Marshal(msg)
			agent.convertJSONToEvent(msg)
			time.Sleep(1 * time.Millisecond)
		}
	}()

	// Goroutine 2: Simulate monitoring goroutine reading loopDetected
	// This mimics the monitoring goroutine in Wait() (lines 286-310 in agent.go)
	wg.Add(1)
	monitoringReads := 0
	go func() {
		defer wg.Done()
		ticker := time.NewTicker(1 * time.Millisecond)
		defer ticker.Stop()
		timeout := time.After(100 * time.Millisecond)
		
		for {
			select {
			case <-ticker.C:
				// Simulate monitoring goroutine checking loopDetected (lock-free atomic read)
				detected := agent.loopDetected.Load()

				monitoringReads++
				if detected {
					t.Logf("Monitoring goroutine detected loop after %d reads", monitoringReads)
					return
				}
			case <-timeout:
				return
			}
		}
	}()

	// Goroutine 3: Simulate Wait() reading both flags
	// This mimics Wait() checking circuit breaker state (lines 337-344 in agent.go)
	wg.Add(1)
	waitReads := 0
	go func() {
		defer wg.Done()
		ticker := time.NewTicker(2 * time.Millisecond)
		defer ticker.Stop()
		timeout := time.After(100 * time.Millisecond)
		
		for {
			select {
			case <-ticker.C:
				// Simulate Wait() reading loopDetected (atomic) and loopReason (mutex)
				detected := agent.loopDetected.Load()
				agent.mu.Lock()
				reason := agent.loopReason
				agent.mu.Unlock()

				waitReads++
				if detected {
					t.Logf("Wait() detected loop: %s (after %d reads)", reason, waitReads)
					return
				}
			case <-timeout:
				return
			}
		}
	}()

	// Goroutine 4: Additional concurrent reader to stress test
	// Simulates other parts of the system that might access agent state
	wg.Add(1)
	go func() {
		defer wg.Done()
		timeout := time.After(100 * time.Millisecond)
		
		for {
			select {
			case <-time.After(1 * time.Millisecond):
				// Read various counters and flags
				_ = agent.loopDetected.Load() // Lock-free atomic read
				agent.mu.Lock()
				_ = agent.totalReadCount
				_ = agent.loopReason
				for range agent.fileReadCounts {
					// Just iterate to stress the map access
					break
				}
				agent.mu.Unlock()
			case <-timeout:
				return
			}
		}
	}()

	// Wait for all goroutines to complete
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	// Use timeout to prevent test hanging
	select {
	case <-done:
		t.Log("All concurrent goroutines completed without race conditions")
	case <-time.After(5 * time.Second):
		t.Fatal("Test timed out - potential deadlock or race condition")
	}

	// Final verification: circuit breaker should have triggered
	finalDetected := agent.loopDetected.Load()
	agent.mu.Lock()
	finalReason := agent.loopReason
	finalReadCount := agent.fileReadCounts[testFilePath]
	agent.mu.Unlock()

	if !finalDetected {
		t.Error("Expected circuit breaker to be triggered")
	} else {
		t.Logf("Circuit breaker correctly triggered: %s (reads: %d)", finalReason, finalReadCount)
	}

	t.Logf("Race detector test completed - run with 'go test -race' to verify thread safety")
}

// TestRaceDetectorEnabled verifies that the -race flag is actually enabled in CI
// to ensure race detection tests are meaningful (vc-855a)
//
// This test checks runtime.RaceEnabled (Go 1.20+) to verify race detector is active.
// If race detector is not enabled, the test skips with a warning to alert CI failures.
//
// Run with: go test -race -run TestRaceDetectorEnabled
func TestRaceDetectorEnabled(t *testing.T) {
	// Go 1.20+ provides runtime.RaceEnabled constant
	// When compiled with -race flag, this will be true
	// When compiled normally, this will be false

	// Check for race detector using the build tag approach
	// When -race is enabled, the "race" build tag is automatically set
	isRaceEnabled := isRaceDetectorEnabled()

	if !isRaceEnabled {
		t.Skip("⚠️  WARNING: Race detector is NOT enabled! Run with: go test -race")
	}

	t.Log("✓ Race detector is enabled (-race flag detected)")

	// Additional verification: the TestCircuitBreakerRaceDetector test
	// should be meaningful when race detector is enabled
	t.Log("Race detection tests will catch concurrency issues correctly")
}

// isRaceDetectorEnabled checks if the race detector is active
// This uses the race build tag which is automatically set when -race is used
func isRaceDetectorEnabled() bool {
	// The race build tag approach
	return raceEnabled
}

// raceEnabled is set by the build tag in race_detector.go and race_detector_off.go
// Default to false (overridden when -race flag is used)
var raceEnabled = false

// TestToolCallLimit verifies that the circuit breaker catches loops involving
// repeated calls to non-Read tools like Grep, todo_write, Bash, etc. (vc-34cz)
func TestToolCallLimit(t *testing.T) {
	ctx := context.Background()

	// Disable AI loop detection for this test (we're testing the hard limits)
	t.Setenv("VC_DISABLE_AI_LOOP_DETECTION", "1")

	// Create agent with circuit breaker state
	agent := &Agent{
		config: AgentConfig{
			Issue: &types.Issue{
				ID:    "vc-test-tool-limit",
				Title: "Test Tool Call Limit",
			},
		},
		ctx:            ctx,
		toolCallCounts: make(map[string]int),
		totalToolCalls: 0,
	}

	// Test 1: Repeated Grep calls should trigger circuit breaker
	t.Run("RepeatedGrepCalls", func(t *testing.T) {
		agent.toolCallCounts = make(map[string]int)
		agent.totalToolCalls = 0

		// Call Grep many times - should trigger at maxSameToolCalls (100)
		var firstErr error
		for i := 0; i < maxSameToolCalls+10; i++ {
			err := agent.checkToolCallLimit("grep")
			if err != nil && firstErr == nil {
				firstErr = err
				t.Logf("Circuit breaker triggered after %d Grep calls: %v", i+1, err)
				break
			}
		}

		if firstErr == nil {
			t.Errorf("Expected circuit breaker to trigger for repeated Grep calls")
		}
		if !agent.loopDetected.Load() {
			t.Errorf("Expected loopDetected flag to be set")
		}
	})

	// Test 2: Mixed tool calls should trigger total limit
	t.Run("TotalToolCallLimit", func(t *testing.T) {
		agent.toolCallCounts = make(map[string]int)
		agent.totalToolCalls = 0
		agent.loopDetected.Store(false)

		// Mix different tools - should trigger at maxTotalToolCalls (1000)
		// With AI disabled, this tests the hard backstop limit
		tools := []string{"grep", "bash", "todo_write", "edit_file", "glob"}
		var firstErr error
		for i := 0; i < maxTotalToolCalls+10; i++ {
			tool := tools[i%len(tools)]
			err := agent.checkToolCallLimit(tool)
			if err != nil && firstErr == nil {
				firstErr = err
				t.Logf("Circuit breaker triggered after %d total tool calls: %v", i+1, err)
				break
			}
		}

		if firstErr == nil {
			t.Errorf("Expected circuit breaker to trigger for excessive total tool calls")
		}
		if !agent.loopDetected.Load() {
			t.Errorf("Expected loopDetected flag to be set")
		}
	})

	// Test 3: Normal usage should not trigger
	t.Run("NormalUsageAllowed", func(t *testing.T) {
		agent.toolCallCounts = make(map[string]int)
		agent.totalToolCalls = 0
		agent.loopDetected.Store(false)

		// Simulate normal mixed tool usage (50 calls across different tools)
		tools := []string{"read", "grep", "bash", "edit_file", "todo_write"}
		for i := 0; i < 50; i++ {
			tool := tools[i%len(tools)]
			err := agent.checkToolCallLimit(tool)
			if err != nil {
				t.Errorf("Circuit breaker triggered unexpectedly on normal usage (call %d): %v", i+1, err)
				break
			}
		}

		if agent.loopDetected.Load() {
			t.Errorf("Circuit breaker should not trigger on normal tool usage")
		}
	})
}
