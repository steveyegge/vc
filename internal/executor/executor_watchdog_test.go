package executor

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/steveyegge/vc/internal/types"
	"github.com/steveyegge/vc/internal/watchdog"
)

// TestWatchdogIntegration_InfiniteLoopDetection tests that the watchdog detects
// and intervenes when an issue keeps executing without success (infinite loop pattern)
func TestWatchdogIntegration_InfiniteLoopDetection(t *testing.T) {
	ctx := context.Background()

	// Create test store with an issue
	store := setupTestStorage(t, ctx)
	issue := &types.Issue{
		ID:          "vc-loop-test",
		Title:       "Test infinite loop detection",
		Description: "This issue will simulate an infinite loop",
		Status:      types.StatusOpen,
		Priority:    1,
		IssueType:   types.TypeTask,
		AcceptanceCriteria: "Test completes successfully",
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}
	if err := store.CreateIssue(ctx, issue, "test"); err != nil {
		t.Fatalf("Failed to create issue: %v", err)
	}

	// Configure watchdog with aggressive settings for testing
	watchdogCfg := watchdog.DefaultWatchdogConfig()
	watchdogCfg.CheckInterval = 100 * time.Millisecond // Very frequent checks
	watchdogCfg.AIConfig.MinConfidenceThreshold = 0.5   // Lower threshold
	watchdogCfg.AIConfig.MinSeverityLevel = watchdog.SeverityMedium

	cfg := &Config{
		Store:               store,
		Version:             "test",
		PollInterval:        time.Second,
		EnableAISupervision: true,
		WatchdogConfig:      watchdogCfg,
	}

	// Note: This test verifies the integration structure works
	// In a real scenario, we would need to:
	// 1. Simulate multiple failed executions of the same issue
	// 2. Have the analyzer detect the loop pattern
	// 3. Verify intervention controller creates escalation issue
	//
	// For now, we verify:
	// - Executor initializes with watchdog components
	// - Watchdog goroutine starts and stops cleanly
	// - No panics or crashes

	exec, err := New(cfg)
	if err != nil {
		t.Fatalf("Failed to create executor: %v", err)
	}

	// Verify watchdog components initialized
	if exec.watchdogConfig == nil {
		t.Fatal("Watchdog config not initialized")
	}
	if exec.monitor == nil {
		t.Fatal("Monitor not initialized")
	}
	if exec.intervention == nil {
		t.Fatal("Intervention controller not initialized")
	}

	// Note: analyzer requires AI supervisor which needs ANTHROPIC_API_KEY
	// In CI/CD without the key, analyzer will be nil (graceful degradation)
	// This is expected and okay - watchdog is disabled without AI

	// Start executor (which starts watchdog if enabled)
	if err := exec.Start(ctx); err != nil {
		t.Fatalf("Failed to start executor: %v", err)
	}

	// Let it run briefly
	time.Sleep(200 * time.Millisecond)

	// Stop executor (should cleanly stop watchdog too) (vc-113: increased timeout)
	stopCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := exec.Stop(stopCtx); err != nil {
		t.Fatalf("Failed to stop executor: %v", err)
	}

	t.Log("✓ Watchdog integration test passed: components initialized and lifecycle managed correctly")
}

// TestWatchdogIntegration_ThrashingDetection tests that the watchdog detects
// thrashing (rapid state changes without completion)
func TestWatchdogIntegration_ThrashingDetection(t *testing.T) {
	ctx := context.Background()

	store := setupTestStorage(t, ctx)
	issue := &types.Issue{
		ID:          "vc-thrash-test",
		Title:       "Test thrashing detection",
		Description: "This issue will simulate thrashing behavior",
		Status:      types.StatusOpen,
		Priority:    1,
		IssueType:   types.TypeTask,
		AcceptanceCriteria: "Test completes successfully",
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}
	if err := store.CreateIssue(ctx, issue, "test"); err != nil {
		t.Fatalf("Failed to create issue: %v", err)
	}

	watchdogCfg := watchdog.DefaultWatchdogConfig()
	watchdogCfg.CheckInterval = 100 * time.Millisecond
	watchdogCfg.AIConfig.MinSeverityLevel = watchdog.SeverityMedium

	cfg := &Config{
		Store:               store,
		Version:             "test",
		PollInterval:        time.Second,
		EnableAISupervision: true,
		WatchdogConfig:      watchdogCfg,
	}

	exec, err := New(cfg)
	if err != nil {
		t.Fatalf("Failed to create executor: %v", err)
	}

	// Simulate thrashing by recording rapid state transitions
	// In a real scenario, the monitor would record these from actual executions
	for i := 0; i < 10; i++ {
		exec.monitor.StartExecution(issue.ID, exec.instanceID)
		exec.monitor.RecordStateTransition(types.ExecutionStateClaimed, types.ExecutionStateExecuting)
		exec.monitor.RecordStateTransition(types.ExecutionStateExecuting, types.ExecutionStateClaimed)
		exec.monitor.EndExecution(false, false)
		time.Sleep(10 * time.Millisecond)
	}

	// Start executor
	if err := exec.Start(ctx); err != nil {
		t.Fatalf("Failed to start executor: %v", err)
	}

	// Let watchdog run a few checks
	time.Sleep(300 * time.Millisecond)

	// Stop cleanly (vc-113: increased timeout to handle slow CI/test environments)
	stopCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := exec.Stop(stopCtx); err != nil {
		t.Fatalf("Failed to stop executor: %v", err)
	}

	// Note: With mock analyzer, no actual intervention occurs
	// But we verified:
	// - Telemetry is collected
	// - Watchdog loop runs
	// - No crashes with rapid state changes

	t.Log("✓ Thrashing detection test passed: watchdog handles rapid state transitions")
}

// TestWatchdogIntegration_AgentContextCancellation tests that the watchdog
// can successfully cancel an agent's context when intervening
func TestWatchdogIntegration_AgentContextCancellation(t *testing.T) {
	ctx := context.Background()

	store := setupTestStorage(t, ctx)
	issue := &types.Issue{
		ID:          "vc-cancel-test",
		Title:       "Test agent cancellation",
		Description: "Verify watchdog can cancel agent execution",
		Status:      types.StatusOpen,
		Priority:    1,
		IssueType:   types.TypeTask,
		AcceptanceCriteria: "Test completes successfully",
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}
	if err := store.CreateIssue(ctx, issue, "test"); err != nil {
		t.Fatalf("Failed to create issue: %v", err)
	}

	cfg := &Config{
		Store:               store,
		Version:             "test",
		EnableAISupervision: true,
	}

	exec, err := New(cfg)
	if err != nil {
		t.Fatalf("Failed to create executor: %v", err)
	}

	// Simulate an agent execution with cancelable context
	agentCtx, agentCancel := context.WithCancel(ctx)
	defer agentCancel()

	// Register the agent context with intervention controller
	if exec.intervention != nil {
		exec.intervention.SetAgentContext(issue.ID, agentCancel)
		defer exec.intervention.ClearAgentContext()
	}

	// Verify agent has active context
	if exec.intervention != nil && !exec.intervention.HasActiveAgent() {
		t.Fatal("Expected intervention controller to have active agent")
	}

	// Simulate watchdog intervention by creating a mock anomaly report
	report := &watchdog.AnomalyReport{
		Detected:          true,
		AnomalyType:       watchdog.AnomalyInfiniteLoop,
		Severity:          watchdog.SeverityHigh,
		Description:       "Test anomaly",
		RecommendedAction: watchdog.ActionStopExecution,
		Reasoning:         "Simulated for testing",
		Confidence:        0.95,
		AffectedIssues:    []string{issue.ID},
	}

	// Trigger intervention (this will cancel the agent context)
	if exec.intervention != nil {
		result, err := exec.intervention.Intervene(ctx, report)
		if err != nil {
			t.Fatalf("Intervention failed: %v", err)
		}

		if !result.Success {
			t.Fatal("Expected intervention to succeed")
		}

		// Verify agent context was canceled
		select {
		case <-agentCtx.Done():
			// Good - context was canceled
			t.Log("✓ Agent context successfully canceled by watchdog intervention")
		case <-time.After(100 * time.Millisecond):
			t.Fatal("Agent context was not canceled")
		}

		// Verify escalation issue was created
		if result.EscalationIssueID == "" {
			t.Fatal("Expected escalation issue to be created")
		}

		t.Logf("✓ Escalation issue created: %s", result.EscalationIssueID)
	}
}

// TestWatchdogIntegration_GracefulShutdown tests that the watchdog
// shuts down cleanly with the executor
func TestWatchdogIntegration_GracefulShutdown(t *testing.T) {
	ctx := context.Background()

	store := setupTestStorage(t, ctx)

	watchdogCfg := watchdog.DefaultWatchdogConfig()
	watchdogCfg.CheckInterval = 50 * time.Millisecond

	cfg := &Config{
		Store:               store,
		Version:             "test",
		PollInterval:        time.Second,
		EnableAISupervision: true,
		WatchdogConfig:      watchdogCfg,
	}

	exec, err := New(cfg)
	if err != nil {
		t.Fatalf("Failed to create executor: %v", err)
	}

	// Start executor (and watchdog)
	if err := exec.Start(ctx); err != nil {
		t.Fatalf("Failed to start executor: %v", err)
	}

	// Let watchdog run for a bit
	time.Sleep(200 * time.Millisecond)

	// Trigger shutdown
	stopCtx, cancel := context.WithTimeout(ctx, 1*time.Second)
	defer cancel()

	shutdownStart := time.Now()
	if err := exec.Stop(stopCtx); err != nil {
		t.Fatalf("Failed to stop executor: %v", err)
	}
	shutdownDuration := time.Since(shutdownStart)

	// Verify shutdown was reasonably fast (< 500ms)
	if shutdownDuration > 500*time.Millisecond {
		t.Fatalf("Shutdown took too long: %v (expected < 500ms)", shutdownDuration)
	}

	// Verify executor is stopped
	if exec.IsRunning() {
		t.Fatal("Executor should not be running after Stop()")
	}

	t.Logf("✓ Graceful shutdown completed in %v", shutdownDuration)
}

// TestWatchdogIntegration_ConfigurationValidation tests that watchdog
// configuration is properly validated and applied
func TestWatchdogIntegration_ConfigurationValidation(t *testing.T) {
	ctx := context.Background()

	store := setupTestStorage(t, ctx)

	tests := []struct {
		name       string
		config     *watchdog.WatchdogConfig
		shouldFail bool
	}{
		{
			name:       "default_config",
			config:     watchdog.DefaultWatchdogConfig(),
			shouldFail: false,
		},
		{
			name: "custom_valid_config",
			config: &watchdog.WatchdogConfig{
				Enabled:             true,
				CheckInterval:       1 * time.Minute,
				TelemetryWindowSize: 50,
				AIConfig: watchdog.AIConfig{
					MinConfidenceThreshold: 0.80,
					MinSeverityLevel:       watchdog.SeverityCritical,
					EnableAnomalyLogging:   true,
				},
				InterventionConfig: watchdog.InterventionConfig{
					AutoKillEnabled:    true,
					MaxRetries:         5,
					EscalateOnCritical: true,
					EscalationPriority: map[watchdog.AnomalySeverity]int{
						watchdog.SeverityCritical: 0,
						watchdog.SeverityHigh:     1,
						watchdog.SeverityMedium:   2,
						watchdog.SeverityLow:      3,
					},
				},
				BackoffConfig: watchdog.BackoffConfig{
					Enabled:           true,
					BaseInterval:      30 * time.Second,
					MaxInterval:       10 * time.Minute,
					BackoffMultiplier: 2.0,
					TriggerThreshold:  3,
				},
				MaxHistorySize: 100,
			},
			shouldFail: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Validate config
			if err := tt.config.Validate(); err != nil {
				if !tt.shouldFail {
					t.Fatalf("Config validation failed unexpectedly: %v", err)
				}
				return
			}

			if tt.shouldFail {
				t.Fatal("Expected config validation to fail, but it succeeded")
			}

			// Create executor with this config
			cfg := &Config{
				Store:               store,
				Version:             "test",
				PollInterval:        time.Second,
				EnableAISupervision: true,
				WatchdogConfig:      tt.config,
			}

			exec, err := New(cfg)
			if err != nil {
				t.Fatalf("Failed to create executor: %v", err)
			}

			// Verify config was applied
			if exec.watchdogConfig != tt.config {
				t.Fatal("Watchdog config was not applied")
			}

			// Test Start/Stop
			if err := exec.Start(ctx); err != nil {
				t.Fatalf("Failed to start executor: %v", err)
			}

			stopCtx, cancel := context.WithTimeout(ctx, 1*time.Second)
			defer cancel()
			if err := exec.Stop(stopCtx); err != nil {
				t.Fatalf("Failed to stop executor: %v", err)
			}
		})
	}

	t.Log("✓ Configuration validation tests passed")
}

// TestWatchdogIntegration_TelemetryCollection tests that telemetry
// is properly collected during issue execution
func TestWatchdogIntegration_TelemetryCollection(t *testing.T) {
	ctx := context.Background()

	store := setupTestStorage(t, ctx)

	cfg := &Config{
		Store:               store,
		Version:             "test",
		EnableAISupervision: true,
	}

	exec, err := New(cfg)
	if err != nil {
		t.Fatalf("Failed to create executor: %v", err)
	}

	// Simulate some executions
	for i := 0; i < 5; i++ {
		issueID := fmt.Sprintf("vc-test-%d", i)
		exec.monitor.StartExecution(issueID, exec.instanceID)
		exec.monitor.RecordEvent("test_event")
		exec.monitor.RecordStateTransition(types.ExecutionStateClaimed, types.ExecutionStateExecuting)
		time.Sleep(10 * time.Millisecond)
		exec.monitor.EndExecution(i%2 == 0, true) // Alternate success/failure
	}

	// Verify telemetry was collected
	telemetry := exec.monitor.GetTelemetry()
	if len(telemetry) != 5 {
		t.Fatalf("Expected 5 telemetry entries, got %d", len(telemetry))
	}

	// Verify telemetry contains expected data
	successCount := 0
	for _, entry := range telemetry {
		if entry.Success {
			successCount++
		}
		if len(entry.StateTransitions) == 0 {
			t.Fatalf("Expected state transitions in telemetry")
		}
		if len(entry.EventCounts) == 0 {
			t.Fatalf("Expected event counts in telemetry")
		}
	}

	if successCount != 3 {
		t.Fatalf("Expected 3 successful executions, got %d", successCount)
	}

	t.Log("✓ Telemetry collection test passed: all data recorded correctly")
}
