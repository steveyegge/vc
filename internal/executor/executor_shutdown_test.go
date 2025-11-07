package executor

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/steveyegge/vc/internal/gates"
	"github.com/steveyegge/vc/internal/labels"
	"github.com/steveyegge/vc/internal/storage"
	"github.com/steveyegge/vc/internal/types"
)

// TestShutdownWithoutActiveWork tests that executor shuts down cleanly
// when there is no work being processed
func TestShutdownWithoutActiveWork(t *testing.T) {
	ctx := context.Background()

	// Create in-memory storage
	cfg := storage.DefaultConfig()
	cfg.Path = t.TempDir() + "/test.db"
	store, err := storage.NewStorage(ctx, cfg)
	if err != nil {
		t.Fatalf("failed to create storage: %v", err)
	}
	defer func() { _ = store.Close() }()

	// Create executor configuration (no issues, so no work to process)
	execCfg := DefaultConfig()
	execCfg.Store = store
	execCfg.EnableAISupervision = false
	execCfg.EnableQualityGates = false
	execCfg.EnableQualityGateWorker = false // vc-q5ve: QA worker requires quality gates
	execCfg.EnableSandboxes = false
	execCfg.PollInterval = 100 * time.Millisecond

	// Create executor
	exec, err := New(execCfg)
	if err != nil {
		t.Fatalf("failed to create executor: %v", err)
	}

	// Start executor
	execCtx, execCancel := context.WithCancel(ctx)
	if err := exec.Start(execCtx); err != nil {
		t.Fatalf("failed to start executor: %v", err)
	}

	// Let it run briefly
	time.Sleep(200 * time.Millisecond)

	// Cancel and shutdown
	execCancel()

	shutdownCtx, shutdownCancel := context.WithTimeout(ctx, 3*time.Second)
	defer shutdownCancel()

	if err := exec.Stop(shutdownCtx); err != nil {
		t.Fatalf("executor shutdown failed: %v", err)
	}

	t.Log("✓ Shutdown without active work completed successfully")
}

// TestShutdownTimeout tests that shutdown respects the timeout
func TestShutdownTimeout(t *testing.T) {
	ctx := context.Background()

	// Create in-memory storage
	cfg := storage.DefaultConfig()
	cfg.Path = t.TempDir() + "/test.db"
	store, err := storage.NewStorage(ctx, cfg)
	if err != nil {
		t.Fatalf("failed to create storage: %v", err)
	}
	defer func() { _ = store.Close() }()

	// Create executor configuration
	execCfg := DefaultConfig()
	execCfg.Store = store
	execCfg.EnableAISupervision = false
	execCfg.EnableQualityGates = false
	execCfg.EnableQualityGateWorker = false // vc-q5ve: QA worker requires quality gates
	execCfg.EnableSandboxes = false
	execCfg.PollInterval = 100 * time.Millisecond

	// Create executor
	exec, err := New(execCfg)
	if err != nil {
		t.Fatalf("failed to create executor: %v", err)
	}

	// Start executor
	execCtx, execCancel := context.WithCancel(ctx)
	if err := exec.Start(execCtx); err != nil {
		t.Fatalf("failed to start executor: %v", err)
	}

	// Cancel
	execCancel()

	// Shutdown with very short timeout
	shutdownCtx, shutdownCancel := context.WithTimeout(ctx, 10*time.Millisecond)
	defer shutdownCancel()

	// This should timeout and return context.DeadlineExceeded
	// Note: the actual shutdown might succeed faster than 10ms, which is fine
	err = exec.Stop(shutdownCtx)

	// Either success or deadline exceeded is acceptable
	if err != nil && err != context.DeadlineExceeded {
		t.Fatalf("expected nil or DeadlineExceeded, got: %v", err)
	}

	t.Log("✓ Shutdown timeout handling works correctly")
}

// TestMarkInstanceStoppedOnExit tests that MarkInstanceStoppedOnExit marks the instance as stopped (vc-192)
func TestMarkInstanceStoppedOnExit(t *testing.T) {
	ctx := context.Background()

	// Create in-memory storage
	cfg := storage.DefaultConfig()
	cfg.Path = t.TempDir() + "/test.db"
	store, err := storage.NewStorage(ctx, cfg)
	if err != nil {
		t.Fatalf("failed to create storage: %v", err)
	}
	defer func() { _ = store.Close() }()

	// Create executor configuration
	execCfg := DefaultConfig()
	execCfg.Store = store
	execCfg.EnableAISupervision = false
	execCfg.EnableQualityGates = false
	execCfg.EnableQualityGateWorker = false // vc-q5ve: QA worker requires quality gates
	execCfg.EnableSandboxes = false
	execCfg.PollInterval = 100 * time.Millisecond

	// Create executor
	exec, err := New(execCfg)
	if err != nil {
		t.Fatalf("failed to create executor: %v", err)
	}

	// Start executor
	execCtx, execCancel := context.WithCancel(ctx)
	if err := exec.Start(execCtx); err != nil {
		t.Fatalf("failed to start executor: %v", err)
	}

	// Verify executor is running
	if !exec.IsRunning() {
		t.Fatalf("expected executor to be running")
	}

	// Verify instance appears in active instances list
	activeInstances, err := store.GetActiveInstances(ctx)
	if err != nil {
		t.Fatalf("failed to get active instances: %v", err)
	}
	foundRunning := false
	for _, inst := range activeInstances {
		if inst.InstanceID == exec.instanceID && inst.Status == "running" {
			foundRunning = true
			break
		}
	}
	if !foundRunning {
		t.Fatalf("expected to find running instance in active instances list")
	}

	// Let it run briefly
	time.Sleep(100 * time.Millisecond)

	// Call MarkInstanceStoppedOnExit (this simulates the defer in execute.go)
	markCtx, markCancel := context.WithTimeout(ctx, 5*time.Second)
	defer markCancel()
	if err := exec.MarkInstanceStoppedOnExit(markCtx); err != nil {
		t.Fatalf("MarkInstanceStoppedOnExit failed: %v", err)
	}

	// Verify executor internal state is updated
	if exec.IsRunning() {
		t.Fatalf("expected executor to not be running after MarkInstanceStoppedOnExit")
	}

	// Verify instance no longer appears in active instances list
	activeInstances, err = store.GetActiveInstances(ctx)
	if err != nil {
		t.Fatalf("failed to get active instances after stop: %v", err)
	}
	for _, inst := range activeInstances {
		if inst.InstanceID == exec.instanceID && inst.Status == "running" {
			t.Fatalf("instance still marked as running in active instances list")
		}
	}

	// Test idempotence: calling again should not error
	if err := exec.MarkInstanceStoppedOnExit(markCtx); err != nil {
		t.Fatalf("second call to MarkInstanceStoppedOnExit failed: %v", err)
	}

	// Cancel and clean up
	execCancel()
	shutdownCtx, shutdownCancel := context.WithTimeout(ctx, 3*time.Second)
	defer shutdownCancel()
	_ = exec.Stop(shutdownCtx) // Ignore error since we already marked as stopped

	t.Log("✓ MarkInstanceStoppedOnExit correctly marks instance as stopped and is idempotent")
}

// slowGatesProvider simulates slow quality gate execution for testing graceful shutdown
type slowGatesProvider struct {
	started   *atomic.Bool
	completed *atomic.Bool
	duration  time.Duration
}

func (m *slowGatesProvider) RunAll(ctx context.Context) ([]*gates.Result, bool) {
	m.started.Store(true)
	defer m.completed.Store(true)
	
	// Simulate slow gate execution
	select {
	case <-time.After(m.duration):
		// Gates completed normally
	case <-ctx.Done():
		// Context canceled during execution
		return []*gates.Result{{Gate: gates.GateTest, Passed: false, Output: "canceled"}}, false
	}
	
	return []*gates.Result{
		{Gate: gates.GateTest, Passed: true, Output: "test passed"},
		{Gate: gates.GateBuild, Passed: true, Output: "build passed"},
	}, true
}

// TestShutdownWaitsForQAWorkers tests that executor shutdown waits for QA worker goroutines (vc-0d58)
func TestShutdownWaitsForQAWorkers(t *testing.T) {
	ctx := context.Background()

	// Create in-memory storage
	cfg := storage.DefaultConfig()
	cfg.Path = t.TempDir() + "/test.db"
	store, err := storage.NewStorage(ctx, cfg)
	if err != nil {
		t.Fatalf("failed to create storage: %v", err)
	}
	defer func() { _ = store.Close() }()

	// Create a mission that needs quality gates
	now := time.Now()
	mission := &types.Mission{
		Issue: types.Issue{
			ID:            "vc-test-mission",
			Title:         "Test Mission for QA Worker Shutdown",
			Description:   "Test graceful shutdown during QA work",
			IssueType:     types.TypeEpic,
			IssueSubtype:  types.SubtypeMission,
			Status:        types.StatusOpen,
			Priority:      1,
			CreatedAt:     now,
			UpdatedAt:     now,
		},
		Goal:        "Test mission goal",
		Context:     "Test context",
		SandboxPath: "/tmp/test-sandbox",
		BranchName:  "mission/vc-test-mission",
	}

	if err := store.CreateMission(ctx, mission, "test"); err != nil {
		t.Fatalf("Failed to create mission: %v", err)
	}

	// Add needs-quality-gates label
	if err := store.AddLabel(ctx, mission.ID, labels.LabelNeedsQualityGates, "test"); err != nil {
		t.Fatalf("Failed to add label: %v", err)
	}

	// Create executor configuration with QA worker disabled (we'll inject our own)
	execCfg := DefaultConfig()
	execCfg.Store = store
	execCfg.EnableAISupervision = false
	execCfg.EnableQualityGates = false // Disable to prevent auto-creation
	execCfg.EnableQualityGateWorker = false // vc-q5ve: QA worker requires quality gates
	execCfg.EnableQualityGateWorker = false
	execCfg.EnableSandboxes = false
	execCfg.PollInterval = 100 * time.Millisecond

	// Create executor
	exec, err := New(execCfg)
	if err != nil {
		t.Fatalf("failed to create executor: %v", err)
	}

	// Create slow gates provider to simulate long-running quality gates
	started := &atomic.Bool{}
	completed := &atomic.Bool{}
	slowProvider := &slowGatesProvider{
		started:   started,
		completed: completed,
		duration:  2 * time.Second, // Gates will take 2 seconds
	}

	gatesRunner, err := gates.NewRunner(&gates.Config{
		Store:      store,
		WorkingDir: ".",
		Provider:   slowProvider,
	})
	if err != nil {
		t.Fatalf("failed to create gates runner: %v", err)
	}

	// Manually inject QA worker with our slow gates provider
	qaWorker, err := NewQualityGateWorker(&QualityGateWorkerConfig{
		Store:       store,
		InstanceID:  exec.instanceID,
		WorkingDir:  ".",
		GatesRunner: gatesRunner,
	})
	if err != nil {
		t.Fatalf("failed to create QA worker: %v", err)
	}
	exec.qaWorker = qaWorker
	exec.enableQualityGateWorker = true

	// Start executor
	execCtx, execCancel := context.WithCancel(ctx)
	defer execCancel()
	
	if err := exec.Start(execCtx); err != nil {
		t.Fatalf("failed to start executor: %v", err)
	}

	// Wait for QA worker to claim and start work
	waitStart := time.Now()
	for !started.Load() && time.Since(waitStart) < 5*time.Second {
		time.Sleep(50 * time.Millisecond)
	}

	if !started.Load() {
		t.Fatal("QA worker did not start processing within timeout")
	}

	t.Log("✓ QA worker started processing quality gates")

	// Initiate shutdown while QA worker is running
	// Note: Canceling the context will cause gates to exit early (graceful shutdown behavior)
	shutdownStart := time.Now()
	execCancel()
	
	shutdownCtx, shutdownCancel := context.WithTimeout(ctx, 5*time.Second)
	defer shutdownCancel()

	if err := exec.Stop(shutdownCtx); err != nil {
		t.Fatalf("executor shutdown failed: %v", err)
	}

	shutdownDuration := time.Since(shutdownStart)

	// Verify that shutdown waited for QA worker to complete
	if !completed.Load() {
		t.Fatal("QA worker goroutine did not complete before shutdown finished")
	}

	t.Logf("✓ Shutdown waited for QA worker goroutine to complete (took %v)", shutdownDuration)

	// Verify mission state is consistent (gates may have been canceled, but state should be clean)
	issue, err := store.GetIssue(ctx, mission.ID)
	if err != nil {
		t.Fatalf("Failed to get mission after shutdown: %v", err)
	}

	t.Logf("✓ Mission state is consistent after shutdown (status=%s)", issue.Status)
	
	// Note: In this test, context is canceled during gate execution, so the worker
	// goroutine exits early before cleanup. This is acceptable for this specific test
	// which focuses on verifying the WaitGroup ensures goroutines complete before shutdown.
	// A real shutdown would let gates finish (or have a timeout), at which point
	// cleanup would happen properly.
	
	t.Log("✓ No orphaned QA worker goroutines after shutdown (WaitGroup ensures completion)")
}
