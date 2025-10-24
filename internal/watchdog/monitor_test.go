package watchdog

import (
	"testing"
	"time"

	"github.com/steveyegge/vc/internal/types"
)

func TestNewMonitor(t *testing.T) {
	tests := []struct {
		name       string
		cfg        *Config
		wantWindow int
	}{
		{
			name:       "default config",
			cfg:        nil,
			wantWindow: 100,
		},
		{
			name:       "custom window size",
			cfg:        &Config{WindowSize: 50},
			wantWindow: 50,
		},
		{
			name:       "zero window size uses default",
			cfg:        &Config{WindowSize: 0},
			wantWindow: 100,
		},
		{
			name:       "negative window size uses default",
			cfg:        &Config{WindowSize: -10},
			wantWindow: 100,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := NewMonitor(tt.cfg)
			if m == nil {
				t.Fatal("NewMonitor returned nil")
			}
			if m.windowSize != tt.wantWindow {
				t.Errorf("window size = %d, want %d", m.windowSize, tt.wantWindow)
			}
			if len(m.telemetry) != 0 {
				t.Errorf("initial telemetry length = %d, want 0", len(m.telemetry))
			}
		})
	}
}

func TestMonitor_StartEndExecution(t *testing.T) {
	m := NewMonitor(nil)

	// Initially no current execution
	if curr := m.GetCurrentExecution(); curr != nil {
		t.Errorf("expected no current execution, got %+v", curr)
	}

	// Start execution
	issueID := "vc-168"
	executorID := "test-executor"
	m.StartExecution(issueID, executorID)

	// Should have current execution
	curr := m.GetCurrentExecution()
	if curr == nil {
		t.Fatal("expected current execution, got nil")
	}
	if curr.IssueID != issueID {
		t.Errorf("issue ID = %s, want %s", curr.IssueID, issueID)
	}
	if curr.ExecutorID != executorID {
		t.Errorf("executor ID = %s, want %s", curr.ExecutorID, executorID)
	}
	if curr.EndTime != (time.Time{}) {
		t.Errorf("expected zero end time, got %v", curr.EndTime)
	}

	// End execution
	m.EndExecution(true, true)

	// Should no longer have current execution
	if curr := m.GetCurrentExecution(); curr != nil {
		t.Errorf("expected no current execution after end, got %+v", curr)
	}

	// Should have one telemetry entry
	telemetry := m.GetTelemetry()
	if len(telemetry) != 1 {
		t.Fatalf("telemetry length = %d, want 1", len(telemetry))
	}
	if telemetry[0].IssueID != issueID {
		t.Errorf("telemetry issue ID = %s, want %s", telemetry[0].IssueID, issueID)
	}
	if !telemetry[0].Success {
		t.Error("expected success = true")
	}
	if !telemetry[0].GatesPassed {
		t.Error("expected gates passed = true")
	}
	if telemetry[0].EndTime.Equal(time.Time{}) {
		t.Error("expected non-zero end time")
	}
}

func TestMonitor_RecordStateTransition(t *testing.T) {
	m := NewMonitor(nil)

	// Start execution
	m.StartExecution("vc-168", "test-executor")

	// Record some state transitions
	m.RecordStateTransition(types.ExecutionStateClaimed, types.ExecutionStateAssessing)
	m.RecordStateTransition(types.ExecutionStateAssessing, types.ExecutionStateExecuting)
	m.RecordStateTransition(types.ExecutionStateExecuting, types.ExecutionStateCompleted)

	// Check transitions recorded
	curr := m.GetCurrentExecution()
	if curr == nil {
		t.Fatal("expected current execution, got nil")
	}
	if len(curr.StateTransitions) != 3 {
		t.Fatalf("state transitions count = %d, want 3", len(curr.StateTransitions))
	}

	// Verify transition details
	trans := curr.StateTransitions[0]
	if trans.From != types.ExecutionStateClaimed {
		t.Errorf("first transition from = %v, want %v", trans.From, types.ExecutionStateClaimed)
	}
	if trans.To != types.ExecutionStateAssessing {
		t.Errorf("first transition to = %v, want %v", trans.To, types.ExecutionStateAssessing)
	}
	if trans.Timestamp.IsZero() {
		t.Error("expected non-zero timestamp")
	}
}

func TestMonitor_RecordEvent(t *testing.T) {
	m := NewMonitor(nil)

	// Start execution
	m.StartExecution("vc-168", "test-executor")

	// Record various events
	m.RecordEvent("test_run")
	m.RecordEvent("test_run")
	m.RecordEvent("test_run")
	m.RecordEvent("file_modified")
	m.RecordEvent("git_operation")
	m.RecordEvent("git_operation")

	// Check event counts
	curr := m.GetCurrentExecution()
	if curr == nil {
		t.Fatal("expected current execution, got nil")
	}

	want := map[string]int{
		"test_run":      3,
		"file_modified": 1,
		"git_operation": 2,
	}

	for eventType, wantCount := range want {
		gotCount, ok := curr.EventCounts[eventType]
		if !ok {
			t.Errorf("event type %s not found in counts", eventType)
			continue
		}
		if gotCount != wantCount {
			t.Errorf("event count for %s = %d, want %d", eventType, gotCount, wantCount)
		}
	}
}

func TestMonitor_SlidingWindow(t *testing.T) {
	// Create monitor with small window
	m := NewMonitor(&Config{WindowSize: 3})

	// Add 5 executions
	for i := 0; i < 5; i++ {
		issueID := "vc-" + string(rune('0'+i))
		m.StartExecution(issueID, "test-executor")
		m.RecordEvent("test_event")
		m.EndExecution(true, true)
	}

	// Should only have last 3 executions
	telemetry := m.GetTelemetry()
	if len(telemetry) != 3 {
		t.Fatalf("telemetry length = %d, want 3 (sliding window)", len(telemetry))
	}

	// Should have the last 3 issues (vc-2, vc-3, vc-4)
	// Note: '0' + 2 = '2'
	expectedIssues := []string{"vc-2", "vc-3", "vc-4"}
	for i, tel := range telemetry {
		if tel.IssueID != expectedIssues[i] {
			t.Errorf("telemetry[%d] issue ID = %s, want %s", i, tel.IssueID, expectedIssues[i])
		}
	}
}

func TestMonitor_GetRecentExecutions(t *testing.T) {
	m := NewMonitor(nil)

	// Add some executions
	for i := 0; i < 10; i++ {
		m.StartExecution("vc-"+string(rune('0'+i)), "test-executor")
		m.EndExecution(true, true)
	}

	tests := []struct {
		name      string
		n         int
		wantCount int
	}{
		{"get last 5", 5, 5},
		{"get last 3", 3, 3},
		{"get more than available", 20, 10},
		{"get zero", 0, 0},
		{"get negative", -1, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			recent := m.GetRecentExecutions(tt.n)
			if len(recent) != tt.wantCount {
				t.Errorf("recent executions count = %d, want %d", len(recent), tt.wantCount)
			}
		})
	}
}

func TestMonitor_GetExecutionsByIssue(t *testing.T) {
	m := NewMonitor(nil)

	// Add executions for multiple issues
	targetIssue := "vc-168"
	for i := 0; i < 3; i++ {
		m.StartExecution(targetIssue, "test-executor")
		m.EndExecution(i%2 == 0, true) // Alternate success
	}
	for i := 0; i < 2; i++ {
		m.StartExecution("vc-169", "test-executor")
		m.EndExecution(true, true)
	}

	// Get executions for target issue
	executions := m.GetExecutionsByIssue(targetIssue)
	if len(executions) != 3 {
		t.Fatalf("executions count for %s = %d, want 3", targetIssue, len(executions))
	}

	// Verify all are for the target issue
	for i, exec := range executions {
		if exec.IssueID != targetIssue {
			t.Errorf("execution[%d] issue ID = %s, want %s", i, exec.IssueID, targetIssue)
		}
	}

	// Verify success pattern (true, false, true)
	expectedSuccess := []bool{true, false, true}
	for i, exec := range executions {
		if exec.Success != expectedSuccess[i] {
			t.Errorf("execution[%d] success = %v, want %v", i, exec.Success, expectedSuccess[i])
		}
	}
}

func TestMonitor_Clear(t *testing.T) {
	m := NewMonitor(nil)

	// Add some executions
	for i := 0; i < 5; i++ {
		m.StartExecution("vc-test", "test-executor")
		m.RecordEvent("test_event")
		m.EndExecution(true, true)
	}

	// Start a current execution
	m.StartExecution("vc-current", "test-executor")

	// Verify state before clear
	if len(m.GetTelemetry()) != 5 {
		t.Fatalf("pre-clear telemetry count = %d, want 5", len(m.GetTelemetry()))
	}
	if m.GetCurrentExecution() == nil {
		t.Fatal("expected current execution before clear")
	}

	// Clear
	m.Clear()

	// Verify state after clear
	if len(m.GetTelemetry()) != 0 {
		t.Errorf("post-clear telemetry count = %d, want 0", len(m.GetTelemetry()))
	}
	if m.GetCurrentExecution() != nil {
		t.Error("expected no current execution after clear")
	}
}

func TestMonitor_NoCurrentExecution(t *testing.T) {
	m := NewMonitor(nil)

	// Recording events/transitions without starting execution should not crash
	m.RecordEvent("test_event")
	m.RecordStateTransition(types.ExecutionStateClaimed, types.ExecutionStateExecuting)

	// Should have no telemetry
	if len(m.GetTelemetry()) != 0 {
		t.Errorf("telemetry count = %d, want 0", len(m.GetTelemetry()))
	}

	// Ending without starting should not crash
	m.EndExecution(true, true)
	if len(m.GetTelemetry()) != 0 {
		t.Errorf("telemetry count after end = %d, want 0", len(m.GetTelemetry()))
	}
}

func TestMonitor_ConcurrentAccess(t *testing.T) {
	m := NewMonitor(nil)

	// Start execution
	m.StartExecution("vc-concurrent", "test-executor")

	// Concurrent reads and writes
	done := make(chan bool, 3)

	// Writer 1: record events
	go func() {
		for i := 0; i < 100; i++ {
			m.RecordEvent("event_type_1")
		}
		done <- true
	}()

	// Writer 2: record state transitions
	go func() {
		for i := 0; i < 100; i++ {
			m.RecordStateTransition(types.ExecutionStateClaimed, types.ExecutionStateExecuting)
		}
		done <- true
	}()

	// Reader: get current execution
	go func() {
		for i := 0; i < 100; i++ {
			_ = m.GetCurrentExecution()
			_ = m.GetTelemetry()
		}
		done <- true
	}()

	// Wait for all goroutines
	for i := 0; i < 3; i++ {
		<-done
	}

	// End execution
	m.EndExecution(true, true)

	// Verify data consistency
	telemetry := m.GetTelemetry()
	if len(telemetry) != 1 {
		t.Errorf("telemetry count = %d, want 1", len(telemetry))
	}
	if telemetry[0].EventCounts["event_type_1"] != 100 {
		t.Errorf("event count = %d, want 100", telemetry[0].EventCounts["event_type_1"])
	}
	if len(telemetry[0].StateTransitions) != 100 {
		t.Errorf("state transitions count = %d, want 100", len(telemetry[0].StateTransitions))
	}
}

func TestMonitor_GetTelemetryDeepCopy(t *testing.T) {
	m := NewMonitor(nil)

	// Add execution with events and transitions
	m.StartExecution("vc-test", "test-executor")
	m.RecordEvent("test_event")
	m.RecordStateTransition(types.ExecutionStateClaimed, types.ExecutionStateExecuting)
	m.EndExecution(true, true)

	// Get telemetry
	telemetry := m.GetTelemetry()
	if len(telemetry) != 1 {
		t.Fatalf("expected 1 telemetry entry, got %d", len(telemetry))
	}

	// Verify original values
	if telemetry[0].EventCounts["test_event"] != 1 {
		t.Fatalf("expected event count = 1, got %d", telemetry[0].EventCounts["test_event"])
	}
	if len(telemetry[0].StateTransitions) != 1 {
		t.Fatalf("expected 1 state transition, got %d", len(telemetry[0].StateTransitions))
	}

	// Attempt to mutate the returned data
	telemetry[0].EventCounts["test_event"] = 999
	telemetry[0].StateTransitions = append(telemetry[0].StateTransitions, StateTransition{
		From:      types.ExecutionStateExecuting,
		To:        types.ExecutionStateCompleted,
		Timestamp: time.Now(),
	})

	// Get telemetry again and verify it wasn't mutated
	telemetry2 := m.GetTelemetry()
	if len(telemetry2) != 1 {
		t.Fatalf("expected 1 telemetry entry, got %d", len(telemetry2))
	}
	if telemetry2[0].EventCounts["test_event"] != 1 {
		t.Errorf("external mutation affected internal state: event count = %d, want 1",
			telemetry2[0].EventCounts["test_event"])
	}
	if len(telemetry2[0].StateTransitions) != 1 {
		t.Errorf("external mutation affected internal state: transitions count = %d, want 1",
			len(telemetry2[0].StateTransitions))
	}
}
