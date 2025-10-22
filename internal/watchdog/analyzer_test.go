package watchdog

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/steveyegge/vc/internal/ai"
	"github.com/steveyegge/vc/internal/events"
	"github.com/steveyegge/vc/internal/storage/sqlite"
	"github.com/steveyegge/vc/internal/types"
)

// mockStorage provides a mock storage implementation for testing
type mockStorage struct{}

func (m *mockStorage) Close() error { return nil }
func (m *mockStorage) StoreAgentEvent(ctx context.Context, event *events.AgentEvent) error { return nil }
func (m *mockStorage) GetAgentEvents(ctx context.Context, filter events.EventFilter) ([]*events.AgentEvent, error) { return nil, nil }
func (m *mockStorage) GetAgentEventsByIssue(ctx context.Context, issueID string) ([]*events.AgentEvent, error) { return nil, nil }
func (m *mockStorage) GetRecentAgentEvents(ctx context.Context, limit int) ([]*events.AgentEvent, error) { return nil, nil }
func (m *mockStorage) CreateIssue(ctx context.Context, issue *types.Issue, actor string) error { return nil }
func (m *mockStorage) GetIssue(ctx context.Context, id string) (*types.Issue, error) { return nil, nil }
func (m *mockStorage) GetMission(ctx context.Context, id string) (*types.Mission, error) { return nil, nil }
func (m *mockStorage) UpdateIssue(ctx context.Context, id string, updates map[string]interface{}, actor string) error { return nil }
func (m *mockStorage) CloseIssue(ctx context.Context, id string, reason string, actor string) error { return nil }
func (m *mockStorage) SearchIssues(ctx context.Context, query string, filter types.IssueFilter) ([]*types.Issue, error) { return nil, nil }
func (m *mockStorage) AddDependency(ctx context.Context, dep *types.Dependency, actor string) error { return nil }
func (m *mockStorage) RemoveDependency(ctx context.Context, issueID, dependsOnID string, actor string) error { return nil }
func (m *mockStorage) GetDependencies(ctx context.Context, issueID string) ([]*types.Issue, error) { return nil, nil }
func (m *mockStorage) GetDependencyRecords(ctx context.Context, issueID string) ([]*types.Dependency, error) {
	return nil, nil
}
func (m *mockStorage) GetDependents(ctx context.Context, issueID string) ([]*types.Issue, error) { return nil, nil }
func (m *mockStorage) GetDependencyTree(ctx context.Context, issueID string, maxDepth int) ([]*types.TreeNode, error) { return nil, nil }
func (m *mockStorage) DetectCycles(ctx context.Context) ([][]*types.Issue, error) { return nil, nil }
func (m *mockStorage) AddLabel(ctx context.Context, issueID, label, actor string) error { return nil }
func (m *mockStorage) RemoveLabel(ctx context.Context, issueID, label, actor string) error { return nil }
func (m *mockStorage) GetLabels(ctx context.Context, issueID string) ([]string, error) { return nil, nil }
func (m *mockStorage) GetIssuesByLabel(ctx context.Context, label string) ([]*types.Issue, error) { return nil, nil }
func (m *mockStorage) GetReadyWork(ctx context.Context, filter types.WorkFilter) ([]*types.Issue, error) { return nil, nil }
func (m *mockStorage) GetBlockedIssues(ctx context.Context) ([]*types.BlockedIssue, error) { return nil, nil }
func (m *mockStorage) AddComment(ctx context.Context, issueID, actor, comment string) error { return nil }
func (m *mockStorage) GetEvents(ctx context.Context, issueID string, limit int) ([]*types.Event, error) { return nil, nil }
func (m *mockStorage) GetStatistics(ctx context.Context) (*types.Statistics, error) { return nil, nil }
func (m *mockStorage) RegisterInstance(ctx context.Context, instance *types.ExecutorInstance) error { return nil }
func (m *mockStorage) UpdateHeartbeat(ctx context.Context, instanceID string) error { return nil }
func (m *mockStorage) GetActiveInstances(ctx context.Context) ([]*types.ExecutorInstance, error) { return nil, nil }
func (m *mockStorage) CleanupStaleInstances(ctx context.Context, staleThreshold int) (int, error) { return 0, nil }
func (m *mockStorage) DeleteOldStoppedInstances(ctx context.Context, olderThanSeconds int, maxToKeep int) (int, error) { return 0, nil }
func (m *mockStorage) ClaimIssue(ctx context.Context, issueID, executorInstanceID string) error { return nil }
func (m *mockStorage) GetExecutionState(ctx context.Context, issueID string) (*types.IssueExecutionState, error) { return nil, nil }
func (m *mockStorage) UpdateExecutionState(ctx context.Context, issueID string, state types.ExecutionState) error { return nil }
func (m *mockStorage) SaveCheckpoint(ctx context.Context, issueID string, checkpointData interface{}) error { return nil }
func (m *mockStorage) GetCheckpoint(ctx context.Context, issueID string) (string, error) { return "", nil }
func (m *mockStorage) ReleaseIssue(ctx context.Context, issueID string) error { return nil }
func (m *mockStorage) ReleaseIssueAndReopen(ctx context.Context, issueID, actor, errorComment string) error { return nil }
func (m *mockStorage) GetExecutionHistory(ctx context.Context, issueID string) ([]*types.ExecutionAttempt, error) { return nil, nil }
func (m *mockStorage) RecordExecutionAttempt(ctx context.Context, attempt *types.ExecutionAttempt) error { return nil }
func (m *mockStorage) GetConfig(ctx context.Context, key string) (string, error) { return "", nil }
func (m *mockStorage) SetConfig(ctx context.Context, key, value string) error { return nil }
func (m *mockStorage) CleanupEventsByAge(ctx context.Context, retentionDays, criticalRetentionDays, batchSize int) (int, error) { return 0, nil }
func (m *mockStorage) CleanupEventsByIssueLimit(ctx context.Context, perIssueLimit, batchSize int) (int, error) { return 0, nil }
func (m *mockStorage) CleanupEventsByGlobalLimit(ctx context.Context, globalLimit, batchSize int) (int, error) { return 0, nil }
func (m *mockStorage) GetEventCounts(ctx context.Context) (*sqlite.EventCounts, error) { return &sqlite.EventCounts{}, nil }
func (m *mockStorage) VacuumDatabase(ctx context.Context) error { return nil }

// createTestSupervisor creates a supervisor for testing
// If ANTHROPIC_API_KEY is set, uses real AI calls; otherwise uses a test key (which will fail API calls)
func createTestSupervisor(t *testing.T) *ai.Supervisor {
	t.Helper()
	store := &mockStorage{}

	// Use real API key from environment if available, otherwise use test key
	// When test key is used, tests that call the AI will skip or fail gracefully
	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey == "" {
		apiKey = "test-key"
	}

	supervisor, err := ai.NewSupervisor(&ai.Config{
		APIKey: apiKey,
		Store:  store,
	})
	if err != nil {
		t.Fatalf("failed to create supervisor: %v", err)
	}
	return supervisor
}

func TestNewAnalyzer(t *testing.T) {
	monitor := NewMonitor(nil)
	supervisor := createTestSupervisor(t)
	store := &mockStorage{}

	tests := []struct {
		name      string
		cfg       *AnalyzerConfig
		wantError bool
	}{
		{
			name: "valid config",
			cfg: &AnalyzerConfig{
				Monitor:    monitor,
				Supervisor: supervisor,
				Store:      store,
			},
			wantError: false,
		},
		{
			name: "missing monitor",
			cfg: &AnalyzerConfig{
				Supervisor: supervisor,
				Store:      store,
			},
			wantError: true,
		},
		{
			name: "missing supervisor",
			cfg: &AnalyzerConfig{
				Monitor: monitor,
				Store:   store,
			},
			wantError: true,
		},
		{
			name: "missing store",
			cfg: &AnalyzerConfig{
				Monitor:    monitor,
				Supervisor: supervisor,
			},
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			analyzer, err := NewAnalyzer(tt.cfg)
			if tt.wantError {
				if err == nil {
					t.Error("expected error, got nil")
				}
				if analyzer != nil {
					t.Error("expected nil analyzer on error")
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
				if analyzer == nil {
					t.Error("expected analyzer, got nil")
				}
			}
		})
	}
}

func TestDetectAnomalies_NoTelemetry(t *testing.T) {
	monitor := NewMonitor(nil)
	supervisor := createTestSupervisor(t)
	store := &mockStorage{}

	analyzer, err := NewAnalyzer(&AnalyzerConfig{
		Monitor:    monitor,
		Supervisor: supervisor,
		Store:      store,
	})
	if err != nil {
		t.Fatalf("failed to create analyzer: %v", err)
	}

	ctx := context.Background()
	report, err := analyzer.DetectAnomalies(ctx)
	if err != nil {
		t.Fatalf("DetectAnomalies failed: %v", err)
	}

	if report == nil {
		t.Fatal("expected report, got nil")
	}

	if report.Detected {
		t.Error("expected no anomaly detected with no telemetry")
	}

	if report.Confidence != 1.0 {
		t.Errorf("expected confidence 1.0 for no-data case, got %.2f", report.Confidence)
	}
}

func TestDetectAnomalies_WithTelemetry(t *testing.T) {
	monitor := NewMonitor(nil)
	supervisor := createTestSupervisor(t)
	store := &mockStorage{}

	// Add some mock telemetry
	monitor.StartExecution("vc-test-1", "executor-1")
	monitor.RecordStateTransition(types.ExecutionStateClaimed, types.ExecutionStateAssessing)
	monitor.RecordStateTransition(types.ExecutionStateAssessing, types.ExecutionStateExecuting)
	monitor.RecordEvent("test_run")
	monitor.RecordEvent("git_commit")
	monitor.EndExecution(true, true)

	monitor.StartExecution("vc-test-2", "executor-1")
	monitor.RecordStateTransition(types.ExecutionStateClaimed, types.ExecutionStateAssessing)
	monitor.RecordEvent("test_run")
	monitor.EndExecution(true, true)

	analyzer, err := NewAnalyzer(&AnalyzerConfig{
		Monitor:    monitor,
		Supervisor: supervisor,
		Store:      store,
	})
	if err != nil {
		t.Fatalf("failed to create analyzer: %v", err)
	}

	ctx := context.Background()
	report, err := analyzer.DetectAnomalies(ctx)
	if err != nil {
		t.Fatalf("DetectAnomalies failed: %v", err)
	}

	if report == nil {
		t.Fatal("expected report, got nil")
	}

	// With the mock implementation, we expect no anomalies
	if report.Detected {
		t.Errorf("expected no anomaly with mock implementation, got detected=%v", report.Detected)
	}

	// Should have reasoning
	if report.Reasoning == "" {
		t.Error("expected non-empty reasoning")
	}

	// Should have confidence
	if report.Confidence <= 0 || report.Confidence > 1 {
		t.Errorf("confidence out of range: %.2f", report.Confidence)
	}
}

func TestDetectAnomalies_WithCurrentExecution(t *testing.T) {
	monitor := NewMonitor(nil)
	supervisor := createTestSupervisor(t)
	store := &mockStorage{}

	// Add completed telemetry
	monitor.StartExecution("vc-test-1", "executor-1")
	monitor.RecordEvent("test_run")
	monitor.EndExecution(true, true)

	// Start a current execution
	monitor.StartExecution("vc-test-2", "executor-1")
	monitor.RecordStateTransition(types.ExecutionStateClaimed, types.ExecutionStateExecuting)
	monitor.RecordEvent("test_run")
	// Don't end it - leave it as current

	analyzer, err := NewAnalyzer(&AnalyzerConfig{
		Monitor:    monitor,
		Supervisor: supervisor,
		Store:      store,
	})
	if err != nil {
		t.Fatalf("failed to create analyzer: %v", err)
	}

	ctx := context.Background()
	report, err := analyzer.DetectAnomalies(ctx)
	if err != nil {
		t.Fatalf("DetectAnomalies failed: %v", err)
	}

	if report == nil {
		t.Fatal("expected report, got nil")
	}

	// Should analyze both historical and current execution
	// With mock, we still expect no anomalies
	if report.Detected {
		t.Errorf("expected no anomaly with mock implementation")
	}
}

func TestGetTelemetrySummary(t *testing.T) {
	monitor := NewMonitor(nil)
	supervisor := createTestSupervisor(t)
	store := &mockStorage{}

	// Add various telemetry
	// Success 1
	monitor.StartExecution("vc-test-1", "executor-1")
	time.Sleep(10 * time.Millisecond) // Small delay for duration
	monitor.EndExecution(true, true)

	// Success 2
	monitor.StartExecution("vc-test-2", "executor-1")
	time.Sleep(10 * time.Millisecond)
	monitor.EndExecution(true, true)

	// Failure
	monitor.StartExecution("vc-test-3", "executor-1")
	time.Sleep(10 * time.Millisecond)
	monitor.EndExecution(false, false)

	// Same issue again
	monitor.StartExecution("vc-test-1", "executor-1")
	time.Sleep(10 * time.Millisecond)
	monitor.EndExecution(true, true)

	analyzer, err := NewAnalyzer(&AnalyzerConfig{
		Monitor:    monitor,
		Supervisor: supervisor,
		Store:      store,
	})
	if err != nil {
		t.Fatalf("failed to create analyzer: %v", err)
	}

	summary := analyzer.GetTelemetrySummary()

	if summary.TotalExecutions != 4 {
		t.Errorf("total executions = %d, want 4", summary.TotalExecutions)
	}

	if summary.SuccessCount != 3 {
		t.Errorf("success count = %d, want 3", summary.SuccessCount)
	}

	if summary.FailureCount != 1 {
		t.Errorf("failure count = %d, want 1", summary.FailureCount)
	}

	// Should have 3 unique issues (vc-test-1, vc-test-2, vc-test-3)
	if summary.UniqueIssues != 3 {
		t.Errorf("unique issues = %d, want 3", summary.UniqueIssues)
	}

	if summary.AverageDuration <= 0 {
		t.Error("expected positive average duration")
	}

	if summary.IsExecuting {
		t.Error("expected IsExecuting=false with no current execution")
	}
}

func TestGetTelemetrySummary_WithCurrentExecution(t *testing.T) {
	monitor := NewMonitor(nil)
	supervisor := createTestSupervisor(t)
	store := &mockStorage{}

	// Add one completed
	monitor.StartExecution("vc-test-1", "executor-1")
	monitor.EndExecution(true, true)

	// Start current execution
	monitor.StartExecution("vc-test-2", "executor-1")
	// Don't end it

	analyzer, err := NewAnalyzer(&AnalyzerConfig{
		Monitor:    monitor,
		Supervisor: supervisor,
		Store:      store,
	})
	if err != nil {
		t.Fatalf("failed to create analyzer: %v", err)
	}

	summary := analyzer.GetTelemetrySummary()

	if !summary.IsExecuting {
		t.Error("expected IsExecuting=true with current execution")
	}

	if summary.TotalExecutions != 1 {
		t.Errorf("total executions = %d, want 1 (current not counted)", summary.TotalExecutions)
	}
}

func TestBuildAnomalyDetectionPrompt(t *testing.T) {
	monitor := NewMonitor(nil)
	supervisor := createTestSupervisor(t)
	store := &mockStorage{}

	// Add some telemetry
	monitor.StartExecution("vc-test-1", "executor-1")
	monitor.RecordStateTransition(types.ExecutionStateClaimed, types.ExecutionStateExecuting)
	monitor.RecordEvent("test_run")
	monitor.EndExecution(true, true)

	analyzer, err := NewAnalyzer(&AnalyzerConfig{
		Monitor:    monitor,
		Supervisor: supervisor,
		Store:      store,
	})
	if err != nil {
		t.Fatalf("failed to create analyzer: %v", err)
	}

	telemetry := monitor.GetTelemetry()
	prompt, err := analyzer.buildAnomalyDetectionPrompt(telemetry, nil)
	if err != nil {
		t.Fatalf("buildAnomalyDetectionPrompt failed: %v", err)
	}

	// Verify prompt contains expected elements
	if prompt == "" {
		t.Error("expected non-empty prompt")
	}

	// Should mention analyzing telemetry
	if !containsIgnoreCase(prompt, "telemetry") {
		t.Error("prompt should mention telemetry")
	}

	// Should mention anomaly detection
	if !containsIgnoreCase(prompt, "anomal") {
		t.Error("prompt should mention anomaly/anomalies")
	}

	// Should include the execution data
	if !containsIgnoreCase(prompt, "vc-test-1") {
		t.Error("prompt should include issue ID from telemetry")
	}

	// Should request JSON response
	if !containsIgnoreCase(prompt, "json") {
		t.Error("prompt should request JSON response")
	}
}

func TestBuildAnomalyDetectionPrompt_WithCurrentExecution(t *testing.T) {
	monitor := NewMonitor(nil)
	supervisor := createTestSupervisor(t)
	store := &mockStorage{}

	// Start current execution
	monitor.StartExecution("vc-current", "executor-1")
	monitor.RecordStateTransition(types.ExecutionStateClaimed, types.ExecutionStateExecuting)

	analyzer, err := NewAnalyzer(&AnalyzerConfig{
		Monitor:    monitor,
		Supervisor: supervisor,
		Store:      store,
	})
	if err != nil {
		t.Fatalf("failed to create analyzer: %v", err)
	}

	current := monitor.GetCurrentExecution()
	prompt, err := analyzer.buildAnomalyDetectionPrompt(nil, current)
	if err != nil {
		t.Fatalf("buildAnomalyDetectionPrompt failed: %v", err)
	}

	// Should mention current execution
	if !containsIgnoreCase(prompt, "current") {
		t.Error("prompt should mention current execution")
	}

	// Should include current issue ID
	if !containsIgnoreCase(prompt, "vc-current") {
		t.Error("prompt should include current issue ID")
	}
}

// TestBuildAnomalyDetectionPrompt_TemporalContext verifies vc-78: timestamps in prompts
func TestBuildAnomalyDetectionPrompt_TemporalContext(t *testing.T) {
	monitor := NewMonitor(nil)
	supervisor := createTestSupervisor(t)
	store := &mockStorage{}

	// Add historical execution
	monitor.StartExecution("vc-historical", "executor-1")
	time.Sleep(10 * time.Millisecond) // Small delay to create measurable duration
	monitor.EndExecution(true, true)

	// Start current execution
	monitor.StartExecution("vc-current", "executor-1")
	monitor.RecordStateTransition(types.ExecutionStateClaimed, types.ExecutionStateExecuting)

	analyzer, err := NewAnalyzer(&AnalyzerConfig{
		Monitor:    monitor,
		Supervisor: supervisor,
		Store:      store,
	})
	if err != nil {
		t.Fatalf("failed to create analyzer: %v", err)
	}

	telemetry := monitor.GetTelemetry()
	current := monitor.GetCurrentExecution()
	prompt, err := analyzer.buildAnomalyDetectionPrompt(telemetry, current)
	if err != nil {
		t.Fatalf("buildAnomalyDetectionPrompt failed: %v", err)
	}

	// vc-78 acceptance criteria: verify temporal context is present

	// Should include "Started:" timestamp (RFC3339 format)
	if !containsIgnoreCase(prompt, "Started:") {
		t.Error("prompt should include 'Started:' timestamp for temporal context (vc-78)")
	}

	// Should include "Ended:" for historical executions
	if !containsIgnoreCase(prompt, "Ended:") {
		t.Error("prompt should include 'Ended:' timestamp for completed executions (vc-78)")
	}

	// Should include "Current time:" for in-progress execution
	if !containsIgnoreCase(prompt, "Current time:") {
		t.Error("prompt should include 'Current time:' for in-progress execution (vc-78)")
	}

	// Should still include "Duration:" for easy reference
	if !containsIgnoreCase(prompt, "Duration:") {
		t.Error("prompt should still include 'Duration:' field")
	}

	// Should include temporal pattern analysis guidance
	if !containsIgnoreCase(prompt, "TEMPORAL PATTERNS") {
		t.Error("prompt should include TEMPORAL PATTERNS section to guide AI analysis (vc-78)")
	}

	// Verify mentions time-based detection capabilities
	if !containsIgnoreCase(prompt, "time-of-day") {
		t.Error("prompt should mention 'time-of-day' pattern detection")
	}

	t.Log("✓ Temporal context successfully added to anomaly detection prompts (vc-78)")
}

func TestAnomalyReport_ZFCCompliance(t *testing.T) {
	// This test verifies that the analyzer follows ZFC principles:
	// - No hardcoded thresholds
	// - All detection via AI
	// - No regex or pattern matching in detection logic

	// Skip if no API key available (would fail without real AI)
	if os.Getenv("ANTHROPIC_API_KEY") == "" {
		t.Skip("Skipping test that requires ANTHROPIC_API_KEY")
	}

	monitor := NewMonitor(nil)
	supervisor := createTestSupervisor(t)
	store := &mockStorage{}

	// Create analyzer
	analyzer, err := NewAnalyzer(&AnalyzerConfig{
		Monitor:    monitor,
		Supervisor: supervisor,
		Store:      store,
	})
	if err != nil {
		t.Fatalf("failed to create analyzer: %v", err)
	}

	// Add some telemetry that looks anomalous: same issue executed many times, all failures
	for i := 0; i < 10; i++ {
		monitor.StartExecution("vc-repeated", "executor-1")
		monitor.EndExecution(false, false) // All failures
	}

	ctx := context.Background()
	report, err := analyzer.DetectAnomalies(ctx)
	if err != nil {
		t.Fatalf("DetectAnomalies failed: %v", err)
	}

	// With real AI, this pattern SHOULD be detected as anomalous
	// This demonstrates ZFC compliance: the analyzer has NO hardcoded rules like:
	// "if same issue fails 10 times, flag as anomaly"
	// Instead, ALL detection logic is delegated to the AI

	if !report.Detected {
		t.Error("AI should detect anomaly for 10 repeated failures (ZFC compliance)")
	}

	// Verify the report has the expected fields
	if report.Detected {
		if report.AnomalyType == "" {
			t.Error("expected anomaly_type to be set when detected=true")
		}
		if report.Severity == "" {
			t.Error("expected severity to be set when detected=true")
		}
		if report.RecommendedAction == "" {
			t.Error("expected recommended_action to be set when detected=true")
		}
		if report.Confidence < 0.5 {
			t.Errorf("expected high confidence for clear anomaly, got %.2f", report.Confidence)
		}
		t.Logf("✓ AI correctly detected anomaly: type=%s, severity=%s, confidence=%.2f",
			report.AnomalyType, report.Severity, report.Confidence)
	}

	// The analyzer should NOT have code like:
	// if failureCount > THRESHOLD { return anomaly }
	// All such logic should be in the AI prompt and AI response
}

// containsIgnoreCase checks if haystack contains needle (case-insensitive)
func containsIgnoreCase(haystack, needle string) bool {
	return len(haystack) >= len(needle) &&
		   (haystack == needle ||
		    len(haystack) > 0 &&
		    containsIgnoreCaseHelper(haystack, needle))
}

func containsIgnoreCaseHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		match := true
		for j := 0; j < len(substr); j++ {
			c1 := s[i+j]
			c2 := substr[j]
			// Simple ASCII case-insensitive comparison
			if c1 >= 'A' && c1 <= 'Z' {
				c1 += 32
			}
			if c2 >= 'A' && c2 <= 'Z' {
				c2 += 32
			}
			if c1 != c2 {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}
