package watchdog

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/steveyegge/vc/internal/events"
)

// mockEventStore implements a minimal EventStore for testing
type mockEventStore struct {
	events []*events.AgentEvent
}

func (m *mockEventStore) StoreAgentEvent(ctx context.Context, event *events.AgentEvent) error {
	m.events = append(m.events, event)
	return nil
}

func (m *mockEventStore) GetAgentEvents(ctx context.Context, filter events.EventFilter) ([]*events.AgentEvent, error) {
	return m.events, nil
}

func (m *mockEventStore) GetAgentEventsByIssue(ctx context.Context, issueID string) ([]*events.AgentEvent, error) {
	var result []*events.AgentEvent
	for _, e := range m.events {
		if e.IssueID == issueID {
			result = append(result, e)
		}
	}
	return result, nil
}

func (m *mockEventStore) GetRecentAgentEvents(ctx context.Context, limit int) ([]*events.AgentEvent, error) {
	if limit > len(m.events) {
		limit = len(m.events)
	}
	return m.events[len(m.events)-limit:], nil
}

// Implement other storage.Storage methods as no-ops for testing
func (m *mockEventStore) CreateIssue(ctx context.Context, issue interface{}, actor string) error {
	return nil
}

func (m *mockEventStore) GetIssue(ctx context.Context, id string) (interface{}, error) {
	return nil, nil
}

func (m *mockEventStore) UpdateIssue(ctx context.Context, id string, updates map[string]interface{}, actor string) error {
	return nil
}

func (m *mockEventStore) SearchIssues(ctx context.Context, query string, filter interface{}) ([]interface{}, error) {
	return nil, nil
}

func (m *mockEventStore) AddLabel(ctx context.Context, issueID, label, actor string) error {
	return nil
}

func (m *mockEventStore) AddComment(ctx context.Context, issueID, actor, comment string) error {
	return nil
}

func (m *mockEventStore) Close() error {
	return nil
}

func TestContextDetectorParseAmpFormat(t *testing.T) {
	ctx := context.Background()
	store := &mockEventStore{}
	detector := NewContextDetector(store)

	// Test amp format: "Context: 45000/200000 (22.5%)"
	output := `Some agent output
Context: 45000/200000 (22.5%)
More output`

	detected, err := detector.ParseAgentOutput(ctx, output, "vc-123", "exec-1", "agent-1")
	if err != nil {
		t.Fatalf("ParseAgentOutput failed: %v", err)
	}

	if !detected {
		t.Fatal("Expected context usage to be detected")
	}

	// Check metrics
	metrics := detector.GetMetrics()
	if metrics.CurrentUsagePercent != 22.5 {
		t.Errorf("Expected usage 22.5%%, got %.1f%%", metrics.CurrentUsagePercent)
	}

	if metrics.LatestMeasurement == nil {
		t.Fatal("Expected latest measurement to be set")
	}

	if metrics.LatestMeasurement.UsedTokens != 45000 {
		t.Errorf("Expected used tokens 45000, got %d", metrics.LatestMeasurement.UsedTokens)
	}

	if metrics.LatestMeasurement.TotalTokens != 200000 {
		t.Errorf("Expected total tokens 200000, got %d", metrics.LatestMeasurement.TotalTokens)
	}

	if metrics.LatestMeasurement.AgentType != "amp" {
		t.Errorf("Expected agent type 'amp', got '%s'", metrics.LatestMeasurement.AgentType)
	}

	// Check that event was stored
	if len(store.events) != 1 {
		t.Fatalf("Expected 1 event to be stored, got %d", len(store.events))
	}

	event := store.events[0]
	if event.Type != events.EventTypeContextUsage {
		t.Errorf("Expected event type context_usage, got %s", event.Type)
	}

	if event.Severity != events.SeverityInfo {
		t.Errorf("Expected severity info for 22.5%%, got %s", event.Severity)
	}
}

func TestContextDetectorParseClaudeCodeFormat(t *testing.T) {
	ctx := context.Background()
	store := &mockEventStore{}
	detector := NewContextDetector(store)

	// Test claude-code format: "Token usage: 150000/200000"
	output := `Agent is working...
Token usage: 150000/200000
More progress...`

	detected, err := detector.ParseAgentOutput(ctx, output, "vc-456", "exec-1", "agent-2")
	if err != nil {
		t.Fatalf("ParseAgentOutput failed: %v", err)
	}

	if !detected {
		t.Fatal("Expected context usage to be detected")
	}

	// Check metrics
	metrics := detector.GetMetrics()
	expectedPercent := 75.0
	if metrics.CurrentUsagePercent != expectedPercent {
		t.Errorf("Expected usage %.1f%%, got %.1f%%", expectedPercent, metrics.CurrentUsagePercent)
	}

	if metrics.LatestMeasurement.AgentType != "claude-code" {
		t.Errorf("Expected agent type 'claude-code', got '%s'", metrics.LatestMeasurement.AgentType)
	}
}

func TestContextDetectorParseClaudeCodeWarning(t *testing.T) {
	ctx := context.Background()
	store := &mockEventStore{}
	detector := NewContextDetector(store)

	// Test claude-code warning format
	output := `Warning: approaching auto-compaction limit`

	detected, err := detector.ParseAgentOutput(ctx, output, "vc-789", "exec-1", "agent-3")
	if err != nil {
		t.Fatalf("ParseAgentOutput failed: %v", err)
	}

	if !detected {
		t.Fatal("Expected context usage to be detected")
	}

	// Check metrics - should estimate 85% when warning appears
	metrics := detector.GetMetrics()
	if metrics.CurrentUsagePercent != 85.0 {
		t.Errorf("Expected usage 85.0%% (estimated), got %.1f%%", metrics.CurrentUsagePercent)
	}
}

func TestContextDetectorBurnRateCalculation(t *testing.T) {
	ctx := context.Background()
	store := &mockEventStore{}
	detector := NewContextDetector(store)

	// First measurement
	detected, err := detector.ParseAgentOutput(ctx, "Context: 10000/200000 (5.0%)", "vc-123", "exec-1", "agent-1")
	if err != nil {
		t.Fatalf("First measurement failed: %v", err)
	}
	if !detected {
		t.Fatal("Expected first measurement to be detected")
	}

	metrics := detector.GetMetrics()
	if metrics.BurnRate != 0.0 {
		t.Errorf("Expected burn rate 0.0 for single measurement, got %.2f", metrics.BurnRate)
	}

	// Second measurement after sufficient time (need > 0.001 minutes = 60ms)
	time.Sleep(100 * time.Millisecond)

	detected, err = detector.ParseAgentOutput(ctx, "Context: 20000/200000 (10.0%)", "vc-123", "exec-1", "agent-1")
	if err != nil {
		t.Fatalf("Second measurement failed: %v", err)
	}
	if !detected {
		t.Fatal("Expected second measurement to be detected")
	}

	metrics = detector.GetMetrics()

	// With two measurements, burn rate should be calculated
	// We increased from 5% to 10% in ~100ms, so burn rate should be positive
	if metrics.BurnRate <= 0 {
		t.Errorf("Expected positive burn rate after two measurements with increased usage, got %.2f", metrics.BurnRate)
	}

	// Third measurement after more time
	time.Sleep(100 * time.Millisecond)

	detected, err = detector.ParseAgentOutput(ctx, "Context: 35000/200000 (17.5%)", "vc-123", "exec-1", "agent-1")
	if err != nil {
		t.Fatalf("Third measurement failed: %v", err)
	}
	if !detected {
		t.Fatal("Expected third measurement to be detected")
	}

	metrics = detector.GetMetrics()

	// Burn rate should still be positive (5% -> 17.5% over ~200ms)
	if metrics.BurnRate <= 0 {
		t.Errorf("Expected positive burn rate after third measurement, got %.2f", metrics.BurnRate)
	}

	t.Logf("Final burn rate: %.2f%%/min", metrics.BurnRate)
}

func TestContextDetectorExhaustionDetection(t *testing.T) {
	ctx := context.Background()
	store := &mockEventStore{}
	detector := NewContextDetector(store)

	tests := []struct {
		output       string
		shouldExhaust bool
	}{
		{"Context: 50000/200000 (25.0%)", false}, // Below threshold
		{"Context: 150000/200000 (75.0%)", false}, // Just below threshold
		{"Context: 160000/200000 (80.0%)", true},  // At threshold
		{"Context: 180000/200000 (90.0%)", true},  // Above threshold
		{"Context: 195000/200000 (97.5%)", true},  // Near exhaustion
	}

	for _, tt := range tests {
		detector.Clear() // Reset for each test

		detected, err := detector.ParseAgentOutput(ctx, tt.output, "vc-123", "exec-1", "agent-1")
		if err != nil {
			t.Fatalf("ParseAgentOutput failed for '%s': %v", tt.output, err)
		}

		if !detected {
			t.Fatalf("Expected detection for '%s'", tt.output)
		}

		metrics := detector.GetMetrics()
		if metrics.IsExhausting != tt.shouldExhaust {
			t.Errorf("For '%s': expected IsExhausting=%v, got %v",
				tt.output, tt.shouldExhaust, metrics.IsExhausting)
		}
	}
}

func TestContextDetectorEventSeverity(t *testing.T) {
	ctx := context.Background()
	store := &mockEventStore{}
	detector := NewContextDetector(store)

	tests := []struct {
		usage            float64
		expectedSeverity events.EventSeverity
	}{
		{25.0, events.SeverityInfo},
		{59.0, events.SeverityInfo},
		{60.0, events.SeverityWarning},
		{79.0, events.SeverityWarning},
		{80.0, events.SeverityError},
		{89.0, events.SeverityError},
		{90.0, events.SeverityCritical},
		{95.0, events.SeverityCritical},
	}

	for _, tt := range tests {
		store.events = nil // Clear events
		detector.Clear()

		// Construct output with target usage
		usedTokens := int(tt.usage * 2000) // 200000 total
		output := fmt.Sprintf("Context: %d/200000 (%.1f%%)", usedTokens, tt.usage)

		detected, err := detector.ParseAgentOutput(ctx, output, "vc-123", "exec-1", "agent-1")
		if err != nil {
			t.Fatalf("ParseAgentOutput failed for %.1f%%: %v", tt.usage, err)
		}

		if !detected {
			t.Fatalf("Expected detection for %.1f%%", tt.usage)
		}

		if len(store.events) != 1 {
			t.Fatalf("Expected 1 event for %.1f%%, got %d", tt.usage, len(store.events))
		}

		event := store.events[0]
		if event.Severity != tt.expectedSeverity {
			t.Errorf("For %.1f%% usage: expected severity %s, got %s",
				tt.usage, tt.expectedSeverity, event.Severity)
		}
	}
}

func TestContextDetectorNoMatch(t *testing.T) {
	ctx := context.Background()
	store := &mockEventStore{}
	detector := NewContextDetector(store)

	// Test output with no context usage information
	output := `Just some random agent output
No context information here
Still working on the task...`

	detected, err := detector.ParseAgentOutput(ctx, output, "vc-123", "exec-1", "agent-1")
	if err != nil {
		t.Fatalf("ParseAgentOutput failed: %v", err)
	}

	if detected {
		t.Fatal("Did not expect context usage to be detected")
	}

	// Should have no events
	if len(store.events) != 0 {
		t.Errorf("Expected 0 events, got %d", len(store.events))
	}

	// Metrics should be empty
	metrics := detector.GetMetrics()
	if metrics.CurrentUsagePercent != 0.0 {
		t.Errorf("Expected 0%% usage, got %.1f%%", metrics.CurrentUsagePercent)
	}
}
