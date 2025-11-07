package watchdog

import (
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/steveyegge/vc/internal/types"
)

// ExecutionTelemetry represents telemetry data for a single issue execution
// vc-b5db: Enhanced with comprehensive metrics capture
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

	// vc-b5db: Phase duration tracking
	// PhaseDurations tracks time spent in each phase (assess, execute, analyze, gates)
	PhaseDurations map[string]time.Duration
	// DiscoveredIssuesCount tracks how many issues were discovered during analysis
	DiscoveredIssuesCount int
	// GateResults tracks detailed quality gate results
	GateResults map[string]GateResult
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

// GateResult represents the result of a quality gate check
// vc-b5db: Added for detailed gate result tracking
type GateResult struct {
	// Name is the gate name (e.g., "build", "test", "lint")
	Name string
	// Passed indicates whether the gate passed
	Passed bool
	// Duration is how long the gate took to run
	Duration time.Duration
	// Message provides additional context (e.g., error message if failed)
	Message string
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
// vc-b5db: Enhanced to initialize phase duration and gate result tracking
func (m *Monitor) StartExecution(issueID, executorID string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.currentExecution = &ExecutionTelemetry{
		IssueID:               issueID,
		ExecutorID:            executorID,
		StartTime:             time.Now(),
		StateTransitions:      []StateTransition{},
		EventCounts:           make(map[string]int),
		PhaseDurations:        make(map[string]time.Duration),
		DiscoveredIssuesCount: 0,
		GateResults:           make(map[string]GateResult),
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
// vc-b5db: Updated to include new metrics fields
//
// Performance (vc-gtr5): Benchmarked at realistic scale:
//   - 100 entries, 50 events: ~288µs, ~547KB allocation
//   - 100 entries, 100 events: ~318µs, ~611KB allocation
//   - 50 entries, 25 events: ~125µs, ~245KB allocation
// Safe for periodic use but avoid tight loops
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
		// vc-b5db: Copy new metrics fields
		entry.PhaseDurations = make(map[string]time.Duration)
		for k, v := range t.PhaseDurations {
			entry.PhaseDurations[k] = v
		}
		entry.GateResults = make(map[string]GateResult)
		for k, v := range t.GateResults {
			entry.GateResults[k] = v
		}
		result[i] = &entry
	}
	return result
}

// GetCurrentExecution returns the currently executing issue (if any)
// vc-b5db: Updated to include new metrics fields
//
// Performance (vc-gtr5): Benchmarked at ~2.4µs with ~5.5KB allocation per call
// Lightweight enough for frequent polling
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
	// vc-b5db: Copy new metrics fields
	current.PhaseDurations = make(map[string]time.Duration)
	for k, v := range m.currentExecution.PhaseDurations {
		current.PhaseDurations[k] = v
	}
	current.GateResults = make(map[string]GateResult)
	for k, v := range m.currentExecution.GateResults {
		current.GateResults[k] = v
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
// vc-b5db: Updated to include new metrics fields
//
// Performance (vc-gtr5): Benchmarked filtering 100 entries (25% match) at ~35µs, ~62KB allocation
// Efficient for occasional queries
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
			// vc-b5db: Copy new metrics fields
			entry.PhaseDurations = make(map[string]time.Duration)
			for k, v := range t.PhaseDurations {
				entry.PhaseDurations[k] = v
			}
			entry.GateResults = make(map[string]GateResult)
			for k, v := range t.GateResults {
				entry.GateResults[k] = v
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

// vc-b5db: Comprehensive metrics capture methods

// RecordPhaseDuration records the duration of a specific execution phase
// phase should be one of: "assess", "execute", "analyze", "gates", "commit"
func (m *Monitor) RecordPhaseDuration(phase string, duration time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.currentExecution == nil {
		return
	}

	m.currentExecution.PhaseDurations[phase] = duration
}

// RecordDiscoveredIssues records the count of issues discovered during analysis
func (m *Monitor) RecordDiscoveredIssues(count int) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.currentExecution == nil {
		return
	}

	m.currentExecution.DiscoveredIssuesCount = count
}

// RecordGateResult records the result of a quality gate check
func (m *Monitor) RecordGateResult(name string, passed bool, duration time.Duration, message string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.currentExecution == nil {
		return
	}

	m.currentExecution.GateResults[name] = GateResult{
		Name:     name,
		Passed:   passed,
		Duration: duration,
		Message:  message,
	}
}

// GetTotalDuration returns the total execution duration
func (t *ExecutionTelemetry) GetTotalDuration() time.Duration {
	if t.EndTime.IsZero() {
		// Still running - calculate from start to now
		return time.Since(t.StartTime)
	}
	return t.EndTime.Sub(t.StartTime)
}

// GetPhaseDuration returns the duration of a specific phase
func (t *ExecutionTelemetry) GetPhaseDuration(phase string) time.Duration {
	if t.PhaseDurations == nil {
		return 0
	}
	return t.PhaseDurations[phase]
}

// vc-b5db: JSON Export functionality

// ExportToJSON exports current telemetry to a JSON file
// If appendMode is true, adds to existing file; otherwise overwrites
func (m *Monitor) ExportToJSON(filepath string, appendMode bool) error {
	m.mu.RLock()
	telemetry := m.telemetry
	m.mu.RUnlock()

	var existingData []*ExecutionTelemetry
	if appendMode {
		// Read existing data if file exists
		if data, err := os.ReadFile(filepath); err == nil {
			if err := json.Unmarshal(data, &existingData); err != nil {
				return fmt.Errorf("failed to parse existing JSON: %w", err)
			}
		}
	}

	// Append new telemetry
	allData := append(existingData, telemetry...)

	// Marshal to JSON with indentation for readability
	jsonData, err := json.MarshalIndent(allData, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal telemetry: %w", err)
	}

	// Write to file
	if err := os.WriteFile(filepath, jsonData, 0644); err != nil {
		return fmt.Errorf("failed to write JSON file: %w", err)
	}

	return nil
}

// ExportCurrentExecutionToJSON exports the currently executing issue to JSON
// Useful for real-time monitoring
func (m *Monitor) ExportCurrentExecutionToJSON(filepath string) error {
	current := m.GetCurrentExecution()
	if current == nil {
		return fmt.Errorf("no execution currently running")
	}

	jsonData, err := json.MarshalIndent(current, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal current execution: %w", err)
	}

	if err := os.WriteFile(filepath, jsonData, 0644); err != nil {
		return fmt.Errorf("failed to write JSON file: %w", err)
	}

	return nil
}

// GetMetricsSummary returns a summary of metrics across all telemetry
// vc-b5db: Provides aggregate statistics for reporting
func (m *Monitor) GetMetricsSummary() MetricsSummary {
	m.mu.RLock()
	defer m.mu.RUnlock()

	summary := MetricsSummary{
		TotalExecutions:   len(m.telemetry),
		SuccessfulCount:   0,
		FailedCount:       0,
		GatesPassedCount:  0,
		GatesFailedCount:  0,
		TotalDiscovered:   0,
		AverageDuration:   0,
		AveragePhaseTimes: make(map[string]time.Duration),
	}

	if len(m.telemetry) == 0 {
		return summary
	}

	var totalDuration time.Duration
	phaseCounts := make(map[string]int)
	phaseTotals := make(map[string]time.Duration)

	for _, t := range m.telemetry {
		// Count successes/failures
		if t.Success {
			summary.SuccessfulCount++
		} else {
			summary.FailedCount++
		}

		// Count gate results
		if t.GatesPassed {
			summary.GatesPassedCount++
		} else {
			summary.GatesFailedCount++
		}

		// Sum discovered issues
		summary.TotalDiscovered += t.DiscoveredIssuesCount

		// Sum durations
		duration := t.GetTotalDuration()
		totalDuration += duration

		// Sum phase durations
		for phase, dur := range t.PhaseDurations {
			phaseTotals[phase] += dur
			phaseCounts[phase]++
		}
	}

	// Calculate averages
	summary.AverageDuration = totalDuration / time.Duration(len(m.telemetry))
	for phase, total := range phaseTotals {
		summary.AveragePhaseTimes[phase] = total / time.Duration(phaseCounts[phase])
	}

	return summary
}

// MetricsSummary provides aggregate metrics across all executions
type MetricsSummary struct {
	TotalExecutions   int
	SuccessfulCount   int
	FailedCount       int
	GatesPassedCount  int
	GatesFailedCount  int
	TotalDiscovered   int
	AverageDuration   time.Duration
	AveragePhaseTimes map[string]time.Duration
}
