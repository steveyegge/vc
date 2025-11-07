package watchdog

import (
	"testing"
	"time"

	"github.com/steveyegge/vc/internal/types"
)

// BenchmarkGetTelemetry measures the performance of deep copying telemetry data
func BenchmarkGetTelemetry(b *testing.B) {
	benchmarks := []struct {
		name           string
		windowSize     int
		eventCounts    int
		transitionCount int
	}{
		{"small_10entries_10events", 10, 10, 5},
		{"medium_50entries_25events", 50, 25, 10},
		{"large_100entries_50events", 100, 50, 20},
		{"xlarge_100entries_100events", 100, 100, 30},
	}

	for _, bm := range benchmarks {
		b.Run(bm.name, func(b *testing.B) {
			m := NewMonitor(&Config{WindowSize: bm.windowSize})

			// Populate monitor with realistic telemetry
			for i := 0; i < bm.windowSize; i++ {
				tel := &ExecutionTelemetry{
					IssueID:   "test-issue",
					StartTime: time.Now(),
					EndTime:   time.Now().Add(time.Minute),
					Success:    true,
					GatesPassed: true,
					EventCounts: make(map[string]int),
					StateTransitions: make([]StateTransition, bm.transitionCount),
					PhaseDurations: make(map[string]time.Duration),
					GateResults: make(map[string]GateResult),
				}

				// Add realistic event counts
				for j := 0; j < bm.eventCounts; j++ {
					tel.EventCounts["event_"+string(rune('A'+j%26))] = j
				}

				// Add phase durations
				for j := 0; j < 10; j++ {
					tel.PhaseDurations["phase_"+string(rune('A'+j%26))] = time.Duration(j) * time.Second
				}

				// Add gate results
				for j := 0; j < 10; j++ {
					tel.GateResults["gate_"+string(rune('A'+j%26))] = GateResult{
						Passed:   j%2 == 0,
						Duration: time.Duration(j) * time.Second,
						Message:  "test message",
					}
				}

				// Add state transitions
				for j := 0; j < bm.transitionCount; j++ {
					tel.StateTransitions[j] = StateTransition{
						From:      types.ExecutionStateAssessing,
						To:        types.ExecutionStateExecuting,
						Timestamp: time.Now(),
					}
				}

				m.telemetry = append(m.telemetry, tel)
			}

			// Measure allocation stats
			b.ReportAllocs()
			
			// Reset timer before benchmark loop
			b.ResetTimer()

			// Benchmark GetTelemetry
			for i := 0; i < b.N; i++ {
				result := m.GetTelemetry()
				if len(result) != bm.windowSize {
					b.Fatalf("got %d entries, want %d", len(result), bm.windowSize)
				}
			}
		})
	}
}

// BenchmarkGetCurrentExecution measures the performance of copying current execution
func BenchmarkGetCurrentExecution(b *testing.B) {
	m := NewMonitor(nil)
	
	// Set up current execution with realistic data
	m.currentExecution = &ExecutionTelemetry{
		IssueID:   "test-issue",
		StartTime: time.Now(),
		EventCounts: make(map[string]int),
		StateTransitions: make([]StateTransition, 20),
		PhaseDurations: make(map[string]time.Duration),
		GateResults: make(map[string]GateResult),
	}

	// Add 50 event counts
	for i := 0; i < 50; i++ {
		m.currentExecution.EventCounts["event_"+string(rune('A'+i%26))] = i
	}

	// Add phase durations
	for i := 0; i < 10; i++ {
		m.currentExecution.PhaseDurations["phase_"+string(rune('A'+i%26))] = time.Duration(i) * time.Second
	}

	// Add gate results
	for i := 0; i < 10; i++ {
		m.currentExecution.GateResults["gate_"+string(rune('A'+i%26))] = GateResult{
			Passed:   i%2 == 0,
			Duration: time.Duration(i) * time.Second,
			Message:  "test message",
		}
	}

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		result := m.GetCurrentExecution()
		if result == nil {
			b.Fatal("got nil result")
		}
	}
}

// BenchmarkGetExecutionsByIssue measures the performance of filtering and copying by issue ID
func BenchmarkGetExecutionsByIssue(b *testing.B) {
	m := NewMonitor(&Config{WindowSize: 100})

	// Populate with 100 entries, 25% for our target issue
	for i := 0; i < 100; i++ {
		issueID := "other-issue"
		if i%4 == 0 {
			issueID = "target-issue"
		}

		tel := &ExecutionTelemetry{
			IssueID:   issueID,
			StartTime: time.Now(),
			EndTime:   time.Now().Add(time.Minute),
			Success:    true,
			GatesPassed: true,
			EventCounts: make(map[string]int),
			StateTransitions: make([]StateTransition, 10),
			PhaseDurations: make(map[string]time.Duration),
			GateResults: make(map[string]GateResult),
		}

		// Add some data to each entry
		for j := 0; j < 25; j++ {
			tel.EventCounts["event_"+string(rune('A'+j%26))] = j
		}

		m.telemetry = append(m.telemetry, tel)
	}

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		result := m.GetExecutionsByIssue("target-issue")
		if len(result) != 25 {
			b.Fatalf("got %d entries, want 25", len(result))
		}
	}
}
