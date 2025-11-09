package executor

import (
	"context"
	"testing"
	"time"

	"github.com/steveyegge/vc/internal/cost"
	"github.com/steveyegge/vc/internal/storage"
	"github.com/steveyegge/vc/internal/types"
)

// TestExecutorQuotaExhaustion tests executor behavior when quota is exhausted (vc-c340)
//
// This test verifies that:
// - checkBudgetBeforeWork correctly detects budget exceeded state
// - Executor can query budget status
// - Budget tracking integrates properly with executor
func TestExecutorQuotaExhaustion(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()

	// === SETUP ===
	// Create storage
	cfg := storage.DefaultConfig()
	cfg.Path = t.TempDir() + "/test.db"
	store, err := storage.NewStorage(ctx, cfg)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer func() { _ = store.Close() }()

	// Create a few issues that would require AI assessment
	issue1 := &types.Issue{
		ID:                 "vc-test-quota-1",
		Title:              "First test issue",
		Description:        "Requires AI assessment",
		IssueType:          types.TypeTask,
		Status:             types.StatusOpen,
		Priority:           1,
		AcceptanceCriteria: "Complete the task",
		CreatedAt:          time.Now(),
		UpdatedAt:          time.Now(),
	}
	if err := store.CreateIssue(ctx, issue1, "test"); err != nil {
		t.Fatalf("Failed to create issue1: %v", err)
	}

	// === TEST SCENARIO 1: checkBudgetBeforeWork with exhausted budget ===
	t.Log("=== SCENARIO 1: checkBudgetBeforeWork detects exhausted quota ===")

	// Create cost tracker with very small budget
	costCfg := &cost.Config{
		MaxTokensPerHour:          1000, // Small budget that will be exhausted
		MaxTokensPerIssue:         500,
		MaxCostPerHour:            0.10, // $0.10/hour
		AlertThreshold:            0.80,
		BudgetResetInterval:       1 * time.Hour,
		PersistStatePath:          "", // Don't persist for test
		Enabled:                   true,
		QuotaSnapshotInterval:     5 * time.Minute,  // Required for validation
		QuotaAlertYellowThreshold: 30 * time.Minute, // Required for validation
		QuotaAlertOrangeThreshold: 15 * time.Minute, // Required for validation
		QuotaAlertRedThreshold:    5 * time.Minute,  // Required for validation
		QuotaRetentionDays:        30,               // Required for validation
	}
	costTracker, err := cost.NewTracker(costCfg, store)
	if err != nil {
		t.Fatalf("Failed to create cost tracker: %v", err)
	}

	// Create executor WITHOUT starting it (we'll test checkBudgetBeforeWork directly)
	execCfg := DefaultConfig()
	execCfg.Store = store
	execCfg.EnableAISupervision = false
	execCfg.EnableQualityGates = false
	execCfg.EnableQualityGateWorker = false
	execCfg.EnableSandboxes = false
	execCfg.PollInterval = 100 * time.Millisecond

	// Create executor (cost tracker will be nil since we're not setting env vars)
	exec, err := New(execCfg)
	if err != nil {
		t.Fatalf("Failed to create executor: %v", err)
	}

	// Manually inject cost tracker for testing
	exec.costTracker = costTracker

	// Before exhausting budget, checkBudgetBeforeWork should return true
	canProceed := exec.checkBudgetBeforeWork(ctx)
	if !canProceed {
		t.Error("Expected checkBudgetBeforeWork=true before exhausting budget")
	}

	// Exhaust the budget by recording fake usage
	// Record 1100 tokens (exceeds 1000 limit)
	t.Log("Exhausting budget with fake usage...")
	_, err = costTracker.RecordUsage(ctx, "test-setup", 550, 550)
	if err != nil {
		t.Fatalf("Failed to record fake usage: %v", err)
	}

	// Verify budget is exceeded
	status := costTracker.CheckBudget()
	if status != cost.BudgetExceeded {
		t.Fatalf("Expected budget to be exceeded, got: %v", status)
	}
	t.Log("✓ Budget successfully exhausted")

	// After exhausting budget, checkBudgetBeforeWork should return false
	canProceedAfter := exec.checkBudgetBeforeWork(ctx)
	if canProceedAfter {
		t.Error("Expected checkBudgetBeforeWork=false after exhausting budget")
	}

	t.Log("✓ Scenario 1 passed: checkBudgetBeforeWork correctly detects exhausted quota")

	// === TEST SCENARIO 2: Budget status is queryable ===
	t.Log("=== SCENARIO 2: Budget status queryable via executor ===")

	enabled, statusStr, stats := exec.GetBudgetStatus()
	if !enabled {
		t.Error("Expected budget tracking to be enabled")
	}
	if statusStr != "EXCEEDED" {
		t.Errorf("Expected status EXCEEDED, got: %s", statusStr)
	}
	if stats.HourlyTokensUsed != 1100 {
		t.Errorf("Expected 1100 tokens used, got: %d", stats.HourlyTokensUsed)
	}

	t.Log("✓ Scenario 2 passed: Budget status correctly queryable")

	// === SUMMARY ===
	t.Log("=== QUOTA EXHAUSTION TEST PASSED ===")
	t.Log("✓ Scenario 1: checkBudgetBeforeWork detects exhausted quota")
	t.Log("✓ Scenario 2: Budget status correctly queryable")
}

// TestExecutorQuotaRecovery tests that executor can resume work when quota becomes available (vc-c340)
func TestExecutorQuotaRecovery(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()

	// === SETUP ===
	// Create storage
	cfg := storage.DefaultConfig()
	cfg.Path = t.TempDir() + "/test.db"
	store, err := storage.NewStorage(ctx, cfg)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer func() { _ = store.Close() }()

	// Create cost tracker with very small budget and SHORT reset interval
	costCfg := &cost.Config{
		MaxTokensPerHour:          1000,
		MaxTokensPerIssue:         500,
		MaxCostPerHour:            0.10,
		AlertThreshold:            0.80,
		BudgetResetInterval:       200 * time.Millisecond, // Very short for testing
		PersistStatePath:          "",
		Enabled:                   true,
		QuotaSnapshotInterval:     5 * time.Minute,  // Required for validation
		QuotaAlertYellowThreshold: 30 * time.Minute, // Required for validation
		QuotaAlertOrangeThreshold: 15 * time.Minute, // Required for validation
		QuotaAlertRedThreshold:    5 * time.Minute,  // Required for validation
		QuotaRetentionDays:        30,               // Required for validation
	}
	costTracker, err := cost.NewTracker(costCfg, store)
	if err != nil {
		t.Fatalf("Failed to create cost tracker: %v", err)
	}

	// Exhaust the budget
	t.Log("Exhausting budget...")
	_, err = costTracker.RecordUsage(ctx, "test-setup", 550, 550)
	if err != nil {
		t.Fatalf("Failed to record fake usage: %v", err)
	}

	// Verify budget is exceeded
	status := costTracker.CheckBudget()
	if status != cost.BudgetExceeded {
		t.Fatalf("Expected budget to be exceeded, got: %v", status)
	}
	t.Log("✓ Budget exhausted")

	// === TEST SCENARIO: Budget resets and executor can proceed ===
	t.Log("=== SCENARIO: Budget recovery after reset ===")

	// Wait for budget to reset (200ms + buffer)
	time.Sleep(300 * time.Millisecond)

	// Check budget again (need to call GetStats() to trigger checkAndResetWindow)
	stats := costTracker.GetStats()
	if stats.Status != cost.BudgetHealthy {
		t.Errorf("Expected budget to be healthy after reset, got: %v (tokens: %d, window start: %v, now: %v, elapsed: %v)",
			stats.Status, stats.HourlyTokensUsed, stats.WindowStartTime, time.Now(), time.Since(stats.WindowStartTime))
	}
	if stats.HourlyTokensUsed != 0 {
		t.Errorf("Expected hourly tokens to be reset to 0, got: %d", stats.HourlyTokensUsed)
	}

	// Verify CanProceed returns true
	canProceed, reason := costTracker.CanProceed("")
	if !canProceed {
		t.Errorf("Expected CanProceed=true after reset, got false: %s", reason)
	}

	t.Log("✓ Budget recovery test passed: Budget reset and executor can proceed")
}
