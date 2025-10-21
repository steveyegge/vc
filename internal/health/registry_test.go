package health

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// mockMonitor implements HealthMonitor for testing
type mockMonitor struct {
	name       string
	philosophy string
	schedule   ScheduleConfig
	cost       CostEstimate
	checkFunc  func(ctx context.Context, codebase CodebaseContext) (*MonitorResult, error)
}

func (m *mockMonitor) Name() string {
	return m.name
}

func (m *mockMonitor) Philosophy() string {
	return m.philosophy
}

func (m *mockMonitor) Schedule() ScheduleConfig {
	return m.schedule
}

func (m *mockMonitor) Cost() CostEstimate {
	return m.cost
}

func (m *mockMonitor) Check(ctx context.Context, codebase CodebaseContext) (*MonitorResult, error) {
	if m.checkFunc != nil {
		return m.checkFunc(ctx, codebase)
	}
	return &MonitorResult{
		IssuesFound: []DiscoveredIssue{},
		CheckedAt:   time.Now(),
	}, nil
}

func TestNewMonitorRegistry(t *testing.T) {
	tmpDir := t.TempDir()
	statePath := filepath.Join(tmpDir, "health_state.json")

	registry, err := NewMonitorRegistry(statePath)
	if err != nil {
		t.Fatalf("Failed to create registry: %v", err)
	}

	if registry == nil {
		t.Fatal("Expected non-nil registry")
	}

	if registry.statePath != statePath {
		t.Errorf("Expected state path %s, got %s", statePath, registry.statePath)
	}

	// Verify state file directory was created
	if _, err := os.Stat(tmpDir); os.IsNotExist(err) {
		t.Error("Expected state directory to be created")
	}
}

func TestRegisterMonitor(t *testing.T) {
	tmpDir := t.TempDir()
	statePath := filepath.Join(tmpDir, "health_state.json")

	registry, err := NewMonitorRegistry(statePath)
	if err != nil {
		t.Fatalf("Failed to create registry: %v", err)
	}

	monitor := &mockMonitor{
		name:       "test_monitor",
		philosophy: "Test philosophy",
		schedule: ScheduleConfig{
			Type:     ScheduleTimeBased,
			Interval: 24 * time.Hour,
		},
	}

	// Register monitor
	if err := registry.Register(monitor); err != nil {
		t.Fatalf("Failed to register monitor: %v", err)
	}

	// Verify monitor was registered
	retrievedMonitor, exists := registry.GetMonitor("test_monitor")
	if !exists {
		t.Fatal("Expected monitor to exist")
	}

	if retrievedMonitor.Name() != "test_monitor" {
		t.Errorf("Expected monitor name %s, got %s", "test_monitor", retrievedMonitor.Name())
	}

	// Try to register duplicate - should fail
	if err := registry.Register(monitor); err == nil {
		t.Error("Expected error when registering duplicate monitor")
	}
}

func TestListMonitors(t *testing.T) {
	tmpDir := t.TempDir()
	statePath := filepath.Join(tmpDir, "health_state.json")

	registry, err := NewMonitorRegistry(statePath)
	if err != nil {
		t.Fatalf("Failed to create registry: %v", err)
	}

	// Register multiple monitors
	monitors := []*mockMonitor{
		{name: "monitor1"},
		{name: "monitor2"},
		{name: "monitor3"},
	}

	for _, m := range monitors {
		if err := registry.Register(m); err != nil {
			t.Fatalf("Failed to register monitor: %v", err)
		}
	}

	// List monitors
	names := registry.ListMonitors()
	if len(names) != 3 {
		t.Errorf("Expected 3 monitors, got %d", len(names))
	}

	// Verify all names are present
	nameSet := make(map[string]bool)
	for _, name := range names {
		nameSet[name] = true
	}

	for _, m := range monitors {
		if !nameSet[m.name] {
			t.Errorf("Expected monitor %s to be in list", m.name)
		}
	}
}

func TestTimeBasedScheduling(t *testing.T) {
	tmpDir := t.TempDir()
	statePath := filepath.Join(tmpDir, "health_state.json")

	registry, err := NewMonitorRegistry(statePath)
	if err != nil {
		t.Fatalf("Failed to create registry: %v", err)
	}

	monitor := &mockMonitor{
		name: "time_based_monitor",
		schedule: ScheduleConfig{
			Type:     ScheduleTimeBased,
			Interval: 1 * time.Hour,
		},
	}

	if err := registry.Register(monitor); err != nil {
		t.Fatalf("Failed to register monitor: %v", err)
	}

	// Should be scheduled immediately (never run before)
	now := time.Now()
	scheduled := registry.GetScheduledMonitors(now, 0, 0)
	if len(scheduled) != 1 {
		t.Errorf("Expected 1 scheduled monitor, got %d", len(scheduled))
	}

	// Record a run
	result := &MonitorResult{
		CheckedAt: now,
	}
	if err := registry.RecordRun("time_based_monitor", result, []string{}); err != nil {
		t.Fatalf("Failed to record run: %v", err)
	}

	// Should not be scheduled again immediately
	scheduled = registry.GetScheduledMonitors(now.Add(30*time.Minute), 0, 0)
	if len(scheduled) != 0 {
		t.Errorf("Expected 0 scheduled monitors, got %d", len(scheduled))
	}

	// Should be scheduled after interval
	scheduled = registry.GetScheduledMonitors(now.Add(2*time.Hour), 0, 0)
	if len(scheduled) != 1 {
		t.Errorf("Expected 1 scheduled monitor after interval, got %d", len(scheduled))
	}
}

func TestEventBasedScheduling(t *testing.T) {
	tmpDir := t.TempDir()
	statePath := filepath.Join(tmpDir, "health_state.json")

	registry, err := NewMonitorRegistry(statePath)
	if err != nil {
		t.Fatalf("Failed to create registry: %v", err)
	}

	monitor := &mockMonitor{
		name: "event_based_monitor",
		schedule: ScheduleConfig{
			Type:         ScheduleEventBased,
			EventTrigger: "every_10_issues",
		},
	}

	if err := registry.Register(monitor); err != nil {
		t.Fatalf("Failed to register monitor: %v", err)
	}

	now := time.Now()

	// Not scheduled yet (0 issues closed)
	scheduled := registry.GetScheduledMonitors(now, 0, 0)
	if len(scheduled) != 0 {
		t.Errorf("Expected 0 scheduled monitors, got %d", len(scheduled))
	}

	// Increment issues closed
	if err := registry.IncrementIssuesClosed(10); err != nil {
		t.Fatalf("Failed to increment issues closed: %v", err)
	}

	// Should be scheduled now
	scheduled = registry.GetScheduledMonitors(now, 10, 0)
	if len(scheduled) != 1 {
		t.Errorf("Expected 1 scheduled monitor, got %d", len(scheduled))
	}

	// Record the run (resets counter)
	result := &MonitorResult{
		CheckedAt: now,
	}
	if err := registry.RecordRun("event_based_monitor", result, []string{}); err != nil {
		t.Fatalf("Failed to record run: %v", err)
	}

	// Should not be scheduled (counter reset)
	scheduled = registry.GetScheduledMonitors(now, 10, 0)
	if len(scheduled) != 0 {
		t.Errorf("Expected 0 scheduled monitors after reset, got %d", len(scheduled))
	}
}

func TestHybridScheduling(t *testing.T) {
	tmpDir := t.TempDir()
	statePath := filepath.Join(tmpDir, "health_state.json")

	registry, err := NewMonitorRegistry(statePath)
	if err != nil {
		t.Fatalf("Failed to create registry: %v", err)
	}

	monitor := &mockMonitor{
		name: "hybrid_monitor",
		schedule: ScheduleConfig{
			Type:         ScheduleHybrid,
			MinInterval:  1 * time.Hour,
			MaxInterval:  24 * time.Hour,
			EventTrigger: "every_10_issues",
		},
	}

	if err := registry.Register(monitor); err != nil {
		t.Fatalf("Failed to register monitor: %v", err)
	}

	now := time.Now()

	// Record initial run
	result := &MonitorResult{
		CheckedAt: now,
	}
	if err := registry.RecordRun("hybrid_monitor", result, []string{}); err != nil {
		t.Fatalf("Failed to record run: %v", err)
	}

	// Not scheduled (min interval not met, even with event trigger)
	if err := registry.IncrementIssuesClosed(10); err != nil {
		t.Fatalf("Failed to increment issues closed: %v", err)
	}
	scheduled := registry.GetScheduledMonitors(now.Add(30*time.Minute), 10, 0)
	if len(scheduled) != 0 {
		t.Errorf("Expected 0 scheduled monitors (min interval not met), got %d", len(scheduled))
	}

	// Scheduled (min interval met + event trigger)
	scheduled = registry.GetScheduledMonitors(now.Add(2*time.Hour), 10, 0)
	if len(scheduled) != 1 {
		t.Errorf("Expected 1 scheduled monitor (min interval + event), got %d", len(scheduled))
	}

	// Record another run
	result = &MonitorResult{
		CheckedAt: now.Add(2 * time.Hour),
	}
	if err := registry.RecordRun("hybrid_monitor", result, []string{}); err != nil {
		t.Fatalf("Failed to record run: %v", err)
	}

	// Force run after max interval (even without event trigger)
	scheduled = registry.GetScheduledMonitors(now.Add(26*time.Hour), 0, 0)
	if len(scheduled) != 1 {
		t.Errorf("Expected 1 scheduled monitor (max interval exceeded), got %d", len(scheduled))
	}
}

func TestManualScheduling(t *testing.T) {
	tmpDir := t.TempDir()
	statePath := filepath.Join(tmpDir, "health_state.json")

	registry, err := NewMonitorRegistry(statePath)
	if err != nil {
		t.Fatalf("Failed to create registry: %v", err)
	}

	monitor := &mockMonitor{
		name: "manual_monitor",
		schedule: ScheduleConfig{
			Type: ScheduleManual,
		},
	}

	if err := registry.Register(monitor); err != nil {
		t.Fatalf("Failed to register monitor: %v", err)
	}

	// Should never be scheduled automatically
	now := time.Now()
	scheduled := registry.GetScheduledMonitors(now, 100, 100)
	if len(scheduled) != 0 {
		t.Errorf("Expected 0 scheduled monitors (manual only), got %d", len(scheduled))
	}
}

func TestStatePersistence(t *testing.T) {
	tmpDir := t.TempDir()
	statePath := filepath.Join(tmpDir, "health_state.json")

	// Create registry and register monitor
	registry1, err := NewMonitorRegistry(statePath)
	if err != nil {
		t.Fatalf("Failed to create registry: %v", err)
	}

	monitor := &mockMonitor{
		name: "test_monitor",
		schedule: ScheduleConfig{
			Type:     ScheduleTimeBased,
			Interval: 1 * time.Hour,
		},
	}

	if err := registry1.Register(monitor); err != nil {
		t.Fatalf("Failed to register monitor: %v", err)
	}

	// Record a run
	now := time.Now()
	result := &MonitorResult{
		CheckedAt: now,
	}
	if err := registry1.RecordRun("test_monitor", result, []string{"vc-1", "vc-2"}); err != nil {
		t.Fatalf("Failed to record run: %v", err)
	}

	// Create new registry (should load persisted state)
	registry2, err := NewMonitorRegistry(statePath)
	if err != nil {
		t.Fatalf("Failed to create second registry: %v", err)
	}

	// Verify state was loaded
	state, exists := registry2.GetMonitorState("test_monitor")
	if !exists {
		t.Fatal("Expected monitor state to exist")
	}

	if state.RunsSinceEpoch != 1 {
		t.Errorf("Expected 1 run, got %d", state.RunsSinceEpoch)
	}

	if len(state.LastIssuesFiled) != 2 {
		t.Errorf("Expected 2 issues filed, got %d", len(state.LastIssuesFiled))
	}

	if !state.LastRun.Equal(now) {
		t.Errorf("Expected last run time %v, got %v", now, state.LastRun)
	}
}

func TestRunMonitor(t *testing.T) {
	tmpDir := t.TempDir()
	statePath := filepath.Join(tmpDir, "health_state.json")

	registry, err := NewMonitorRegistry(statePath)
	if err != nil {
		t.Fatalf("Failed to create registry: %v", err)
	}

	// Create monitor with custom check function
	checkCalled := false
	monitor := &mockMonitor{
		name: "test_monitor",
		checkFunc: func(ctx context.Context, codebase CodebaseContext) (*MonitorResult, error) {
			checkCalled = true
			return &MonitorResult{
				IssuesFound: []DiscoveredIssue{
					{
						FilePath:    "test.go",
						Category:    "test",
						Severity:    "low",
						Description: "Test issue",
					},
				},
				CheckedAt: time.Now(),
			}, nil
		},
	}

	if err := registry.Register(monitor); err != nil {
		t.Fatalf("Failed to register monitor: %v", err)
	}

	// Run the monitor
	ctx := context.Background()
	result, err := registry.RunMonitor(ctx, "test_monitor", CodebaseContext{})
	if err != nil {
		t.Fatalf("Failed to run monitor: %v", err)
	}

	if !checkCalled {
		t.Error("Expected check function to be called")
	}

	if len(result.IssuesFound) != 1 {
		t.Errorf("Expected 1 issue found, got %d", len(result.IssuesFound))
	}

	// Try to run non-existent monitor
	_, err = registry.RunMonitor(ctx, "nonexistent", CodebaseContext{})
	if err == nil {
		t.Error("Expected error when running non-existent monitor")
	}
}
