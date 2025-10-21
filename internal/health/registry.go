package health

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// MonitorRegistry manages the lifecycle of health monitors including
// registration, scheduling, and state persistence.
type MonitorRegistry struct {
	mu       sync.RWMutex
	monitors map[string]HealthMonitor
	state    *MonitorState
	statePath string // Path to state file (e.g., .beads/health_state.json)
}

// MonitorState tracks the execution history of health monitors.
// This is persisted to disk to survive restarts.
type MonitorState struct {
	Monitors map[string]*MonitorRunState `json:"monitors"`
}

// MonitorRunState tracks the execution history for a single monitor.
type MonitorRunState struct {
	LastRun          time.Time `json:"last_run"`
	LastIssuesFiled  []string  `json:"last_issues_filed"`
	LastIssueCount   int       `json:"last_issue_count"`
	RunsSinceEpoch   int       `json:"runs_since_epoch"`
	IssuesClosedSince int      `json:"issues_closed_since"` // For event-based scheduling
	CommitsSince     int       `json:"commits_since"`       // For event-based scheduling
}

// NewMonitorRegistry creates a new monitor registry.
// The statePath should point to the state file (e.g., .beads/health_state.json).
func NewMonitorRegistry(statePath string) (*MonitorRegistry, error) {
	// Ensure state directory exists
	stateDir := filepath.Dir(statePath)
	if err := os.MkdirAll(stateDir, 0755); err != nil {
		return nil, fmt.Errorf("creating state directory: %w", err)
	}

	registry := &MonitorRegistry{
		monitors:  make(map[string]HealthMonitor),
		state:     &MonitorState{Monitors: make(map[string]*MonitorRunState)},
		statePath: statePath,
	}

	// Load existing state if it exists
	if err := registry.loadState(); err != nil {
		// If state file doesn't exist, that's okay - we'll create it on first save
		if !os.IsNotExist(err) {
			return nil, fmt.Errorf("loading monitor state: %w", err)
		}
	}

	return registry, nil
}

// Register adds a health monitor to the registry.
func (r *MonitorRegistry) Register(monitor HealthMonitor) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	name := monitor.Name()
	if _, exists := r.monitors[name]; exists {
		return fmt.Errorf("monitor %q already registered", name)
	}

	r.monitors[name] = monitor

	// Initialize state for this monitor if it doesn't exist
	if _, exists := r.state.Monitors[name]; !exists {
		r.state.Monitors[name] = &MonitorRunState{
			LastIssuesFiled: []string{},
		}
	}

	return nil
}

// GetMonitor returns a registered monitor by name.
func (r *MonitorRegistry) GetMonitor(name string) (HealthMonitor, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	monitor, exists := r.monitors[name]
	return monitor, exists
}

// ListMonitors returns all registered monitor names.
func (r *MonitorRegistry) ListMonitors() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	names := make([]string, 0, len(r.monitors))
	for name := range r.monitors {
		names = append(names, name)
	}
	return names
}

// GetScheduledMonitors returns monitors that are due to run based on their schedule.
// This checks time-based schedules. Event-based schedules are checked separately.
func (r *MonitorRegistry) GetScheduledMonitors(now time.Time, issuesClosed int, commits int) []HealthMonitor {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var scheduled []HealthMonitor

	for name, monitor := range r.monitors {
		schedule := monitor.Schedule()
		runState := r.state.Monitors[name]

		if r.shouldRun(schedule, runState, now, issuesClosed, commits) {
			scheduled = append(scheduled, monitor)
		}
	}

	return scheduled
}

// shouldRun determines if a monitor should run based on its schedule and current state.
func (r *MonitorRegistry) shouldRun(schedule ScheduleConfig, state *MonitorRunState, now time.Time, issuesClosed int, commits int) bool {
	// Never run manual-only monitors automatically
	if schedule.Type == ScheduleManual {
		return false
	}

	// Handle time-based schedules
	if schedule.Type == ScheduleTimeBased {
		if state.LastRun.IsZero() {
			return true // Never run before
		}
		return now.Sub(state.LastRun) >= schedule.Interval
	}

	// Handle event-based schedules
	if schedule.Type == ScheduleEventBased {
		return r.checkEventTrigger(schedule.EventTrigger, state, issuesClosed, commits)
	}

	// Handle hybrid schedules
	if schedule.Type == ScheduleHybrid {
		timeSinceLastRun := now.Sub(state.LastRun)

		// Force run if max interval exceeded
		if !state.LastRun.IsZero() && timeSinceLastRun >= schedule.MaxInterval {
			return true
		}

		// Don't run if min interval not met
		if !state.LastRun.IsZero() && timeSinceLastRun < schedule.MinInterval {
			return false
		}

		// Check event trigger
		return r.checkEventTrigger(schedule.EventTrigger, state, issuesClosed, commits)
	}

	return false
}

// checkEventTrigger evaluates event-based triggers.
// Event trigger format: "every_N_issues" or "every_N_commits"
func (r *MonitorRegistry) checkEventTrigger(trigger string, state *MonitorRunState, issuesClosed int, commits int) bool {
	if trigger == "" {
		return false
	}

	// Parse trigger format (e.g., "every_10_issues")
	var count int
	var eventType string

	// Simple parsing - in production, consider more robust parsing
	if _, err := fmt.Sscanf(trigger, "every_%d_issues", &count); err == nil {
		eventType = "issues"
	} else if _, err := fmt.Sscanf(trigger, "every_%d_commits", &count); err == nil {
		eventType = "commits"
	} else {
		return false // Unknown trigger format
	}

	switch eventType {
	case "issues":
		return state.IssuesClosedSince >= count
	case "commits":
		return state.CommitsSince >= count
	default:
		return false
	}
}

// RecordRun updates the state after a monitor completes execution.
func (r *MonitorRegistry) RecordRun(monitorName string, result *MonitorResult, issuesFiled []string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	state, exists := r.state.Monitors[monitorName]
	if !exists {
		state = &MonitorRunState{
			LastIssuesFiled: []string{},
		}
		r.state.Monitors[monitorName] = state
	}

	// Update state
	state.LastRun = result.CheckedAt
	state.LastIssuesFiled = issuesFiled
	state.LastIssueCount = len(issuesFiled)
	state.RunsSinceEpoch++

	// Reset event counters
	state.IssuesClosedSince = 0
	state.CommitsSince = 0

	// Persist to disk
	return r.saveState()
}

// IncrementIssuesClosed increments the issues closed counter for event-based scheduling.
func (r *MonitorRegistry) IncrementIssuesClosed(count int) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	for _, state := range r.state.Monitors {
		state.IssuesClosedSince += count
	}

	return r.saveState()
}

// IncrementCommits increments the commits counter for event-based scheduling.
func (r *MonitorRegistry) IncrementCommits(count int) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	for _, state := range r.state.Monitors {
		state.CommitsSince += count
	}

	return r.saveState()
}

// GetMonitorState returns the current state for a monitor.
func (r *MonitorRegistry) GetMonitorState(monitorName string) (*MonitorRunState, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	state, exists := r.state.Monitors[monitorName]
	return state, exists
}

// loadState loads monitor state from disk.
func (r *MonitorRegistry) loadState() error {
	data, err := os.ReadFile(r.statePath)
	if err != nil {
		return err
	}

	var state MonitorState
	if err := json.Unmarshal(data, &state); err != nil {
		return fmt.Errorf("parsing state file: %w", err)
	}

	r.state = &state
	if r.state.Monitors == nil {
		r.state.Monitors = make(map[string]*MonitorRunState)
	}

	return nil
}

// saveState persists monitor state to disk.
func (r *MonitorRegistry) saveState() error {
	data, err := json.MarshalIndent(r.state, "", "  ")
	if err != nil {
		return fmt.Errorf("serializing state: %w", err)
	}

	// Write atomically using temp file + rename
	tmpPath := r.statePath + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0644); err != nil {
		return fmt.Errorf("writing state file: %w", err)
	}

	if err := os.Rename(tmpPath, r.statePath); err != nil {
		_ = os.Remove(tmpPath) // Clean up on error (best effort)
		return fmt.Errorf("committing state file: %w", err)
	}

	return nil
}

// RunMonitor executes a specific monitor and returns the result.
// This is a convenience method that doesn't update state - call RecordRun separately.
func (r *MonitorRegistry) RunMonitor(ctx context.Context, monitorName string, codebaseCtx CodebaseContext) (*MonitorResult, error) {
	monitor, exists := r.GetMonitor(monitorName)
	if !exists {
		return nil, fmt.Errorf("monitor %q not registered", monitorName)
	}

	return monitor.Check(ctx, codebaseCtx)
}
