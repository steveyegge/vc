package executor

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/steveyegge/vc/internal/gates"
	"github.com/steveyegge/vc/internal/storage"
	"github.com/steveyegge/vc/internal/storage/beads"
	"github.com/steveyegge/vc/internal/types"
)

// TestPreFlightChecker tests basic preflight checker functionality
func TestPreFlightChecker(t *testing.T) {
	ctx := context.Background()

	// Create in-memory storage
	cfg := storage.DefaultConfig()
	cfg.Path = ":memory:"
	store, err := storage.NewStorage(ctx, cfg)
	if err != nil {
		t.Fatalf("failed to create storage: %v", err)
	}
	defer store.Close()

	// Get VCStorage for preflight checker
	vcStorage, ok := store.(*beads.VCStorage)
	if !ok {
		t.Fatal("storage is not VCStorage")
	}

	// Create mock gates runner
	mockRunner := &mockGateRunner{
		passAll: true,
	}

	// Create preflight config
	config := &PreFlightConfig{
		Enabled:      true,
		CacheTTL:     5 * time.Minute,
		FailureMode:  FailureModeBlock,
		WorkingDir:   ".",
		GatesTimeout: 30 * time.Second,
	}

	// Create preflight checker
	checker, err := NewPreFlightChecker(vcStorage, mockRunner, config)
	if err != nil {
		t.Fatalf("failed to create preflight checker: %v", err)
	}

	// Test 1: First check should run gates (cache miss)
	allPassed, commitHash, err := checker.CheckBaseline(ctx, "test-executor")
	if err != nil {
		t.Fatalf("CheckBaseline failed: %v", err)
	}
	if !allPassed {
		t.Error("Expected gates to pass")
	}
	if commitHash == "" {
		t.Error("Expected non-empty commit hash")
	}
	if !mockRunner.called {
		t.Error("Expected gates to be called on first check")
	}

	// Test 2: Second check should use cache (no gates run)
	mockRunner.called = false
	allPassed2, commitHash2, err := checker.CheckBaseline(ctx, "test-executor")
	if err != nil {
		t.Fatalf("CheckBaseline failed on second call: %v", err)
	}
	if !allPassed2 {
		t.Error("Expected gates to pass on second check")
	}
	if commitHash2 != commitHash {
		t.Error("Expected same commit hash on second check")
	}
	if mockRunner.called {
		t.Error("Expected gates NOT to be called on second check (should use cache)")
	}

	// Test 3: Failed gates
	mockRunner.passAll = false
	mockRunner.called = false

	// Invalidate cache to force re-run
	if err := vcStorage.InvalidateGateBaseline(ctx, commitHash); err != nil {
		t.Fatalf("failed to invalidate cache: %v", err)
	}
	checker.invalidateCachedBaseline(commitHash)

	allPassed3, _, err := checker.CheckBaseline(ctx, "test-executor")
	if err != nil {
		t.Fatalf("CheckBaseline failed: %v", err)
	}
	if allPassed3 {
		t.Error("Expected gates to fail")
	}
	if !mockRunner.called {
		t.Error("Expected gates to be called after cache invalidation")
	}
}

// TestPreFlightConfig tests configuration loading
func TestPreFlightConfig(t *testing.T) {
	tests := []struct {
		name        string
		env         map[string]string
		wantEnabled bool
		wantTTL     time.Duration
		wantMode    FailureMode
		wantErr     bool
	}{
		{
			name:        "defaults",
			env:         map[string]string{},
			wantEnabled: true,
			wantTTL:     5 * time.Minute,
			wantMode:    FailureModeBlock,
			wantErr:     false,
		},
		{
			name: "custom values",
			env: map[string]string{
				"VC_PREFLIGHT_ENABLED":      "false",
				"VC_PREFLIGHT_CACHE_TTL":    "10m",
				"VC_PREFLIGHT_FAILURE_MODE": "warn",
			},
			wantEnabled: false,
			wantTTL:     10 * time.Minute,
			wantMode:    FailureModeWarn,
			wantErr:     false,
		},
		{
			name: "invalid TTL",
			env: map[string]string{
				"VC_PREFLIGHT_CACHE_TTL": "invalid",
			},
			wantErr: true,
		},
		{
			name: "invalid mode",
			env: map[string]string{
				"VC_PREFLIGHT_FAILURE_MODE": "invalid",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Set environment variables
			for k, v := range tt.env {
				t.Setenv(k, v)
			}

			cfg, err := PreFlightConfigFromEnv()
			if (err != nil) != tt.wantErr {
				t.Errorf("PreFlightConfigFromEnv() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr {
				if cfg.Enabled != tt.wantEnabled {
					t.Errorf("Enabled = %v, want %v", cfg.Enabled, tt.wantEnabled)
				}
				if cfg.CacheTTL != tt.wantTTL {
					t.Errorf("CacheTTL = %v, want %v", cfg.CacheTTL, tt.wantTTL)
				}
				if cfg.FailureMode != tt.wantMode {
					t.Errorf("FailureMode = %v, want %v", cfg.FailureMode, tt.wantMode)
				}
			}
		})
	}
}

// TestBaselineCache tests baseline caching behavior
func TestBaselineCache(t *testing.T) {
	ctx := context.Background()

	// Create in-memory storage
	cfg := storage.DefaultConfig()
	cfg.Path = ":memory:"
	store, err := storage.NewStorage(ctx, cfg)
	if err != nil {
		t.Fatalf("failed to create storage: %v", err)
	}
	defer store.Close()

	vcStorage, ok := store.(*beads.VCStorage)
	if !ok {
		t.Fatal("storage is not VCStorage")
	}

	// Create test baseline
	baseline := &beads.GateBaseline{
		CommitHash: "abc123",
		BranchName: "main",
		Timestamp:  time.Now().Format(time.RFC3339),
		AllPassed:  true,
		Results: map[string]*types.GateResult{
			"test": {
				Gate:   "test",
				Passed: true,
				Output: "All tests passed",
			},
		},
	}

	// Test SetGateBaseline
	if err := vcStorage.SetGateBaseline(ctx, baseline); err != nil {
		t.Fatalf("SetGateBaseline failed: %v", err)
	}

	// Test GetGateBaseline
	retrieved, err := vcStorage.GetGateBaseline(ctx, "abc123")
	if err != nil {
		t.Fatalf("GetGateBaseline failed: %v", err)
	}
	if retrieved == nil {
		t.Fatal("Expected baseline to be retrieved")
	}
	if retrieved.CommitHash != baseline.CommitHash {
		t.Errorf("CommitHash = %v, want %v", retrieved.CommitHash, baseline.CommitHash)
	}
	if !retrieved.AllPassed {
		t.Error("Expected AllPassed to be true")
	}

	// Test InvalidateGateBaseline
	if err := vcStorage.InvalidateGateBaseline(ctx, "abc123"); err != nil {
		t.Fatalf("InvalidateGateBaseline failed: %v", err)
	}

	// Verify baseline is gone
	retrieved, err = vcStorage.GetGateBaseline(ctx, "abc123")
	if err != nil {
		t.Fatalf("GetGateBaseline failed after invalidation: %v", err)
	}
	if retrieved != nil {
		t.Error("Expected baseline to be invalidated")
	}
}

// mockGateRunner is a mock implementation of GateProvider for testing
type mockGateRunner struct {
	passAll bool
	called  bool
}

func (m *mockGateRunner) RunAll(ctx context.Context) ([]*gates.Result, bool) {
	m.called = true
	results := []*gates.Result{
		{
			Gate:   gates.GateTest,
			Passed: m.passAll,
			Output: "test output",
		},
		{
			Gate:   gates.GateLint,
			Passed: m.passAll,
			Output: "lint output",
		},
		{
			Gate:   gates.GateBuild,
			Passed: m.passAll,
			Output: "build output",
		},
	}
	return results, m.passAll
}

// TestHandleBaselineFailure tests that baseline issues are created when gates fail
func TestHandleBaselineFailure(t *testing.T) {
	ctx := context.Background()

	// Create in-memory storage
	cfg := storage.DefaultConfig()
	cfg.Path = ":memory:"
	store, err := storage.NewStorage(ctx, cfg)
	if err != nil {
		t.Fatalf("failed to create storage: %v", err)
	}
	defer store.Close()

	vcStorage, ok := store.(*beads.VCStorage)
	if !ok {
		t.Fatal("storage is not VCStorage")
	}

	// Create mock gates runner with failing gates
	mockRunner := &mockGateRunner{
		passAll: false, // Gates will fail
	}

	// Create preflight checker
	config := &PreFlightConfig{
		Enabled:      true,
		CacheTTL:     5 * time.Minute,
		FailureMode:  FailureModeBlock,
		WorkingDir:   ".",
		GatesTimeout: 30 * time.Second,
	}

	checker, err := NewPreFlightChecker(vcStorage, mockRunner, config)
	if err != nil {
		t.Fatalf("failed to create preflight checker: %v", err)
	}

	// Get gate results (all failing)
	results, allPassed := mockRunner.RunAll(ctx)
	if allPassed {
		t.Fatal("Expected gates to fail for this test")
	}

	// Call HandleBaselineFailure
	err = checker.HandleBaselineFailure(ctx, "test-executor", "abc123", results)
	if err != nil {
		t.Fatalf("HandleBaselineFailure failed: %v", err)
	}

	// Verify baseline issues were created
	expectedIssues := []string{
		"vc-baseline-test",
		"vc-baseline-lint",
		"vc-baseline-build",
	}

	for _, issueID := range expectedIssues {
		issue, err := vcStorage.GetIssue(ctx, issueID)
		if err != nil {
			t.Fatalf("failed to get issue %s: %v", issueID, err)
		}
		if issue == nil {
			t.Errorf("Expected issue %s to be created", issueID)
			continue
		}

		// Verify issue properties
		if issue.Status != types.StatusOpen {
			t.Errorf("Issue %s status = %v, want %v", issueID, issue.Status, types.StatusOpen)
		}
		if issue.Priority != 1 {
			t.Errorf("Issue %s priority = %d, want 1", issueID, issue.Priority)
		}
		if issue.IssueType != types.TypeBug {
			t.Errorf("Issue %s type = %v, want %v", issueID, issue.IssueType, types.TypeBug)
		}

		// Verify labels
		labels, err := vcStorage.GetLabels(ctx, issueID)
		if err != nil {
			t.Fatalf("failed to get labels for %s: %v", issueID, err)
		}

		hasBaselineLabel := false
		hasSystemLabel := false
		for _, label := range labels {
			if label == "baseline-failure" {
				hasBaselineLabel = true
			}
			if label == "system" {
				hasSystemLabel = true
			}
		}

		if !hasBaselineLabel {
			t.Errorf("Issue %s missing baseline-failure label", issueID)
		}
		if !hasSystemLabel {
			t.Errorf("Issue %s missing system label", issueID)
		}
	}

	// Verify that calling HandleBaselineFailure again doesn't create duplicates
	err = checker.HandleBaselineFailure(ctx, "test-executor", "abc123", results)
	if err != nil {
		t.Fatalf("HandleBaselineFailure failed on second call: %v", err)
	}

	// Issues should still exist (no duplicates)
	for _, issueID := range expectedIssues {
		issue, err := vcStorage.GetIssue(ctx, issueID)
		if err != nil {
			t.Fatalf("failed to get issue %s: %v", issueID, err)
		}
		if issue == nil {
			t.Errorf("Expected issue %s to still exist after second call", issueID)
		}
	}
}

// TestBaselineIssueReopening tests that closed baseline issues are reopened when gates fail again
func TestBaselineIssueReopening(t *testing.T) {
	ctx := context.Background()

	// Create in-memory storage
	cfg := storage.DefaultConfig()
	cfg.Path = ":memory:"
	store, err := storage.NewStorage(ctx, cfg)
	if err != nil {
		t.Fatalf("failed to create storage: %v", err)
	}
	defer store.Close()

	vcStorage, ok := store.(*beads.VCStorage)
	if !ok {
		t.Fatal("storage is not VCStorage")
	}

	// Create mock gates runner with failing gates
	mockRunner := &mockGateRunner{
		passAll: false, // Gates will fail
	}

	// Create preflight checker
	config := &PreFlightConfig{
		Enabled:      true,
		CacheTTL:     5 * time.Minute,
		FailureMode:  FailureModeBlock,
		WorkingDir:   ".",
		GatesTimeout: 30 * time.Second,
	}

	checker, err := NewPreFlightChecker(vcStorage, mockRunner, config)
	if err != nil {
		t.Fatalf("failed to create preflight checker: %v", err)
	}

	// Get gate results (all failing)
	results, allPassed := mockRunner.RunAll(ctx)
	if allPassed {
		t.Fatal("Expected gates to fail for this test")
	}

	// Step 1: Create initial baseline issues
	err = checker.HandleBaselineFailure(ctx, "test-executor", "abc123", results)
	if err != nil {
		t.Fatalf("HandleBaselineFailure failed: %v", err)
	}

	// Verify baseline issues were created
	expectedIssues := []string{
		"vc-baseline-test",
		"vc-baseline-lint",
		"vc-baseline-build",
	}

	for _, issueID := range expectedIssues {
		issue, err := vcStorage.GetIssue(ctx, issueID)
		if err != nil {
			t.Fatalf("failed to get issue %s: %v", issueID, err)
		}
		if issue == nil {
			t.Fatalf("Expected issue %s to be created", issueID)
		}
		if issue.Status != types.StatusOpen {
			t.Errorf("Issue %s status = %v, want %v", issueID, issue.Status, types.StatusOpen)
		}
	}

	// Step 2: Close the baseline issues (simulating that they were fixed)
	for _, issueID := range expectedIssues {
		err := vcStorage.UpdateIssue(ctx, issueID, map[string]interface{}{
			"status": string(types.StatusClosed),
			"notes":  "Fixed the gate failure",
		}, "test-actor")
		if err != nil {
			t.Fatalf("failed to close issue %s: %v", issueID, err)
		}
	}

	// Verify issues are closed
	for _, issueID := range expectedIssues {
		issue, err := vcStorage.GetIssue(ctx, issueID)
		if err != nil {
			t.Fatalf("failed to get issue %s: %v", issueID, err)
		}
		if issue.Status != types.StatusClosed {
			t.Errorf("Issue %s status = %v, want %v", issueID, issue.Status, types.StatusClosed)
		}
	}

	// Step 3: Call HandleBaselineFailure again (gates failed again)
	err = checker.HandleBaselineFailure(ctx, "test-executor", "def456", results)
	if err != nil {
		t.Fatalf("HandleBaselineFailure failed on reopening: %v", err)
	}

	// Step 4: Verify issues were reopened (status changed from closed to open)
	for _, issueID := range expectedIssues {
		issue, err := vcStorage.GetIssue(ctx, issueID)
		if err != nil {
			t.Fatalf("failed to get issue %s after reopening: %v", issueID, err)
		}
		if issue == nil {
			t.Fatalf("Expected issue %s to exist after reopening", issueID)
		}

		// Verify status changed to open
		if issue.Status != types.StatusOpen {
			t.Errorf("Issue %s status = %v, want %v (should be reopened)", issueID, issue.Status, types.StatusOpen)
		}

		// Verify notes were updated with new failure info
		if issue.Notes == "" {
			t.Errorf("Issue %s notes are empty (expected updated failure info)", issueID)
		}
		if issue.Notes == "Fixed the gate failure" {
			t.Errorf("Issue %s notes not updated (still has old notes)", issueID)
		}
		// Notes should contain "Gate failed again"
		if len(issue.Notes) > 0 && !strings.Contains(issue.Notes, "Gate failed again") {
			t.Errorf("Issue %s notes don't mention gate failing again: %s", issueID, issue.Notes)
		}
	}
}
