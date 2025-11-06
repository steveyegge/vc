//go:build integration
// +build integration

package watchdog

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/steveyegge/vc/internal/types"
)

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

// TestBuildAnomalyDetectionPrompt_AgentProgressIndicators verifies vc-125: agent tool usage guidance
func TestBuildAnomalyDetectionPrompt_AgentProgressIndicators(t *testing.T) {
	monitor := NewMonitor(nil)
	supervisor := createTestSupervisor(t)
	store := &mockStorage{}

	// Start current execution with agent tool usage
	monitor.StartExecution("vc-active", "executor-1")
	monitor.RecordStateTransition(types.ExecutionStateClaimed, types.ExecutionStateExecuting)

	// Simulate agent tool usage (which indicates active work)
	monitor.RecordEvent("agent_tool_use")
	monitor.RecordEvent("agent_tool_use")
	monitor.RecordEvent("agent_tool_use")

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

	// vc-125 acceptance criteria: verify agent progress indicators in prompt

	// Should highlight tool usage count
	if !containsIgnoreCase(prompt, "Agent has used tools") {
		t.Error("prompt should highlight agent tool usage count (vc-125)")
	}

	// Should explain that tool usage means active work
	if !containsIgnoreCase(prompt, "actively executing") {
		t.Error("prompt should explain that tool usage indicates active work (vc-125)")
	}

	// Should mention that periods without tool usage may be normal
	if !containsIgnoreCase(prompt, "AI thinking/planning") {
		t.Error("prompt should explain that gaps may be normal AI thinking time (vc-125)")
	}

	// Should include AGENT PROGRESS INDICATORS section in analysis guidance
	if !containsIgnoreCase(prompt, "AGENT PROGRESS INDICATORS") {
		t.Error("prompt should include AGENT PROGRESS INDICATORS section in guidance (vc-125)")
	}

	// Should warn against flagging short executions as stuck
	if !containsIgnoreCase(prompt, "Short executions") {
		t.Error("prompt should warn against flagging short executions with tool activity as stuck (vc-125)")
	}

	// Should mention checking for both no tool usage AND excessive time
	if !containsIgnoreCase(prompt, "no tool usage AND") {
		t.Error("prompt should require BOTH no tool usage AND excessive time to flag as stuck (vc-125)")
	}

	t.Log("✓ Agent progress indicators successfully added to anomaly detection prompts (vc-125)")
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
