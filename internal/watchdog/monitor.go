package watchdog

import (
	"sync"
	"time"

	"github.com/steveyegge/vc/internal/types"
)

// ExecutionTelemetry represents telemetry data for a single issue execution
type ExecutionTelemetry struct {
	// IssueID is the issue being executed
	IssueID string
	// StartTime is when execution started
	StartTime time.Time
	// EndTime is when execution completed (zero if still running)
	EndTime time.Time
	// StateTransitions tracks state changes during execution
	StateTransitions []StateTransition
	// EventCounts tracks event counts by type
	EventCounts map[string]int
	// Success indicates whether the execution succeeded
	Success bool
	// GatesPassed indicates whether quality gates passed
	GatesPassed bool
	// ExecutorID is the executor instance that processed this issue
	ExecutorID string
}

// StateTransition represents a state change during issue execution
type StateTransition struct {
	// From is the previous execution state
	From types.ExecutionState
	// To is the new execution state
	To types.ExecutionState
	// Timestamp is when the transition occurred
	Timestamp time.Time
}

// Monitor collects telemetry data from executor executions for analysis
// It maintains a sliding window of recent execution history
type Monitor struct {
	mu sync.RWMutex

	// telemetry stores recent execution telemetry (bounded by windowSize)
	telemetry []*ExecutionTelemetry
	// windowSize is the maximum number of executions to keep
	windowSize int

	// currentExecution tracks the currently executing issue (if any)
	currentExecution *ExecutionTelemetry
}

// Config holds monitor configuration
type Config struct {
	// WindowSize is the number of recent executions to keep in memory
	// Default: 100
	WindowSize int
}

// DefaultConfig returns default monitor configuration
func DefaultConfig() *Config {
	return &Config{
		WindowSize: 100,
	}
}

// NewMonitor creates a new telemetry monitor
func NewMonitor(cfg *Config) *Monitor {
	if cfg == nil {
		cfg = DefaultConfig()
	}
	if cfg.WindowSize <= 0 {
		cfg.WindowSize = 100
	}

	return &Monitor{
		telemetry:  make([]*ExecutionTelemetry, 0, cfg.WindowSize),
		windowSize: cfg.WindowSize,
	}
}

// StartExecution begins tracking a new issue execution
// This should be called when the executor claims an issue
func (m *Monitor) StartExecution(issueID, executorID string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.currentExecution = &ExecutionTelemetry{
		IssueID:          issueID,
		ExecutorID:       executorID,
		StartTime:        time.Now(),
		StateTransitions: []StateTransition{},
		EventCounts:      make(map[string]int),
	}
}

// RecordStateTransition records a state change during execution
func (m *Monitor) RecordStateTransition(from, to types.ExecutionState) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.currentExecution == nil {
		return
	}

	transition := StateTransition{
		From:      from,
		To:        to,
		Timestamp: time.Now(),
	}
	m.currentExecution.StateTransitions = append(m.currentExecution.StateTransitions, transition)
}

// RecordEvent increments the count for a specific event type
func (m *Monitor) RecordEvent(eventType string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.currentExecution == nil {
		return
	}

	m.currentExecution.EventCounts[eventType]++
}

// EndExecution completes tracking for the current execution
// This should be called when the executor finishes processing an issue
func (m *Monitor) EndExecution(success, gatesPassed bool) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.currentExecution == nil {
		return
	}

	m.currentExecution.EndTime = time.Now()
	m.currentExecution.Success = success
	m.currentExecution.GatesPassed = gatesPassed

	// Add to telemetry history
	m.telemetry = append(m.telemetry, m.currentExecution)

	// Enforce sliding window
	if len(m.telemetry) > m.windowSize {
		// Remove oldest entries
		copy(m.telemetry, m.telemetry[len(m.telemetry)-m.windowSize:])
		m.telemetry = m.telemetry[:m.windowSize]
	}

	// Clear current execution
	m.currentExecution = nil
}

// GetTelemetry returns a deep copy of the telemetry history
// This is safe for concurrent access
func (m *Monitor) GetTelemetry() []*ExecutionTelemetry {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// Return a deep copy to prevent external modification
	result := make([]*ExecutionTelemetry, len(m.telemetry))
	for i, t := range m.telemetry {
		// Deep copy the telemetry entry
		entry := *t
		entry.StateTransitions = append([]StateTransition{}, t.StateTransitions...)
		entry.EventCounts = make(map[string]int)
		for k, v := range t.EventCounts {
			entry.EventCounts[k] = v
		}
		result[i] = &entry
	}
	return result
}

// GetCurrentExecution returns the currently executing issue (if any)
func (m *Monitor) GetCurrentExecution() *ExecutionTelemetry {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.currentExecution == nil {
		return nil
	}

	// Return a copy to prevent external modification
	current := *m.currentExecution
	current.StateTransitions = append([]StateTransition{}, m.currentExecution.StateTransitions...)
	current.EventCounts = make(map[string]int)
	for k, v := range m.currentExecution.EventCounts {
		current.EventCounts[k] = v
	}
	return &current
}

// GetRecentExecutions returns the last N executions
func (m *Monitor) GetRecentExecutions(n int) []*ExecutionTelemetry {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if n <= 0 || len(m.telemetry) == 0 {
		return nil
	}

	start := len(m.telemetry) - n
	if start < 0 {
		start = 0
	}

	result := make([]*ExecutionTelemetry, len(m.telemetry)-start)
	copy(result, m.telemetry[start:])
	return result
}

// GetExecutionsByIssue returns all telemetry for a specific issue ID
func (m *Monitor) GetExecutionsByIssue(issueID string) []*ExecutionTelemetry {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var result []*ExecutionTelemetry
	for _, t := range m.telemetry {
		if t.IssueID == issueID {
			// Copy the telemetry entry
			entry := *t
			entry.StateTransitions = append([]StateTransition{}, t.StateTransitions...)
			entry.EventCounts = make(map[string]int)
			for k, v := range t.EventCounts {
				entry.EventCounts[k] = v
			}
			result = append(result, &entry)
		}
	}
	return result
}

// Clear resets the monitor state (useful for testing)
func (m *Monitor) Clear() {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.telemetry = make([]*ExecutionTelemetry, 0, m.windowSize)
	m.currentExecution = nil
}
