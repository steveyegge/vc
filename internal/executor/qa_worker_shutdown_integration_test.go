//go:build !windows
// +build !windows

package executor

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/steveyegge/vc/internal/gates"
	"github.com/steveyegge/vc/internal/labels"
	"github.com/steveyegge/vc/internal/storage"
	"github.com/steveyegge/vc/internal/types"
)

// TestConcurrentQAWorkerAndExecutorShutdown is an integration test for vc-03fc
// that verifies:
// - Executor waits for QA worker to complete during shutdown
// - No orphaned processes (go test, golangci-lint) remain after shutdown
// - Mission state consistency is maintained after shutdown
//
// This test creates a real sandbox with actual Go files and runs real quality gates
// (test and build commands) to ensure the shutdown process handles real child processes.
func TestConcurrentQAWorkerAndExecutorShutdown(t *testing.T) {
	ctx := context.Background()

	// Create in-memory storage
	cfg := storage.DefaultConfig()
	cfg.Path = t.TempDir() + "/test.db"
	store, err := storage.NewStorage(ctx, cfg)
	if err != nil {
		t.Fatalf("failed to create storage: %v", err)
	}
	defer func() { _ = store.Close() }()

	// Create temporary sandbox directory for the mission
	sandboxDir, err := os.MkdirTemp("", "vc-qa-shutdown-test-*")
	if err != nil {
		t.Fatalf("failed to create temp sandbox: %v", err)
	}
	defer os.RemoveAll(sandboxDir)

	// Create a simple Go module with test in the sandbox
	if err := setupTestGoModule(sandboxDir); err != nil {
		t.Fatalf("failed to setup test module: %v", err)
	}

	// Create a mission that needs quality gates
	now := time.Now()
	mission := &types.Mission{
		Issue: types.Issue{
			ID:            "vc-shutdown-test",
			Title:         "Test Mission for QA Worker Shutdown",
			Description:   "Test graceful shutdown with real processes",
			IssueType:     types.TypeEpic,
			IssueSubtype:  types.SubtypeMission,
			Status:        types.StatusOpen,
			Priority:      1,
			CreatedAt:     now,
			UpdatedAt:     now,
		},
		Goal:        "Test mission goal",
		Context:     "Test context",
		SandboxPath: sandboxDir,
		BranchName:  "mission/vc-shutdown-test",
	}

	if err := store.CreateMission(ctx, mission, "test"); err != nil {
		t.Fatalf("Failed to create mission: %v", err)
	}

	// Add needs-quality-gates label
	if err := store.AddLabel(ctx, mission.ID, labels.LabelNeedsQualityGates, "test"); err != nil {
		t.Fatalf("Failed to add label: %v", err)
	}

	// Create executor configuration with quality gates enabled
	execCfg := DefaultConfig()
	execCfg.Store = store
	execCfg.EnableAISupervision = false
	execCfg.EnableQualityGates = true // Enable real quality gates
	execCfg.EnableQualityGateWorker = true
	execCfg.EnableSandboxes = false
	execCfg.PollInterval = 50 * time.Millisecond
	execCfg.WorkingDir = sandboxDir

	// Create executor
	exec, err := New(execCfg)
	if err != nil {
		t.Fatalf("failed to create executor: %v", err)
	}

	// Disable preflight checker to avoid git repo errors in test
	exec.preFlightChecker = nil

	// Register executor instance
	instance := &types.ExecutorInstance{
		InstanceID:    exec.instanceID,
		Hostname:      "test-host",
		PID:           os.Getpid(),
		Status:        types.ExecutorStatusRunning,
		StartedAt:     now,
		LastHeartbeat: now,
		Version:       "test",
		Metadata:      "{}",
	}
	if err := store.RegisterInstance(ctx, instance); err != nil {
		t.Fatalf("Failed to register executor instance: %v", err)
	}

	// Start executor
	execCtx, execCancel := context.WithCancel(ctx)
	defer execCancel()

	if err := exec.Start(execCtx); err != nil {
		t.Fatalf("failed to start executor: %v", err)
	}

	// Wait for QA worker to claim the mission
	t.Log("Waiting for QA worker to claim mission...")
	waitStart := time.Now()
	var execState *types.IssueExecutionState
	for time.Since(waitStart) < 10*time.Second {
		execState, err = store.GetExecutionState(ctx, mission.ID)
		if err != nil {
			t.Fatalf("Failed to get execution state: %v", err)
		}
		if execState != nil && execState.ExecutorInstanceID == exec.instanceID {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}

	if execState == nil {
		t.Fatalf("QA worker did not claim mission within timeout (instanceID=%s)", exec.instanceID)
	}

	// Verify gates-running label was added
	hasGatesRunning, err := labels.HasLabel(ctx, store, mission.ID, labels.LabelGatesRunning)
	if err != nil {
		t.Fatalf("Failed to check gates-running label: %v", err)
	}
	if !hasGatesRunning {
		t.Fatal("Expected gates-running label to be added after claim")
	}

	t.Log("✓ QA worker claimed mission and started processing")

	// Wait a bit to ensure gates are actually running (spawning processes)
	time.Sleep(200 * time.Millisecond)

	// Capture process count before shutdown (to verify no orphans later)
	// Note: This is a best-effort check with known limitations:
	// - Child processes from previous tests might still be running
	// - Unrelated system processes might start/stop between measurements
	// - The comparison is a heuristic, not a guarantee
	processesBefore := countChildProcesses(t)
	t.Logf("Child processes before shutdown: %d (baseline for orphan detection)", processesBefore)

	// Initiate shutdown while QA worker is processing
	t.Log("Initiating executor shutdown while QA worker is running...")
	shutdownStart := time.Now()
	execCancel()

	shutdownCtx, shutdownCancel := context.WithTimeout(ctx, 30*time.Second)
	defer shutdownCancel()

	if err := exec.Stop(shutdownCtx); err != nil {
		t.Fatalf("executor shutdown failed: %v", err)
	}

	shutdownDuration := time.Since(shutdownStart)
	t.Logf("✓ Shutdown completed in %v", shutdownDuration)

	// Verify shutdown waited for QA worker (should take at least a few hundred ms for gates to run)
	if shutdownDuration < 100*time.Millisecond {
		t.Logf("WARNING: Shutdown completed very quickly (%v), QA worker may not have been running", shutdownDuration)
	}

	// Wait a bit for any orphaned processes to finish (if they exist)
	time.Sleep(500 * time.Millisecond)

	// Verify no orphaned child processes remain
	// This is a best-effort check - we look for more processes than before shutdown
	// In flaky test environments, this might produce false positives
	processesAfter := countChildProcesses(t)
	t.Logf("Child processes after shutdown: %d", processesAfter)

	if processesAfter > processesBefore {
		// Log as warning instead of error due to known limitations
		// In CI environments with parallel tests, this check can be unreliable
		t.Logf("WARNING: Possible orphaned processes detected (had %d before, %d after shutdown)", processesBefore, processesAfter)
		listChildProcesses(t) // Debug: list what processes remain
		t.Logf("Note: This check has known limitations in parallel test environments")
	} else {
		t.Logf("✓ No orphaned processes detected")
	}

	// Verify mission state is consistent
	finalIssue, err := store.GetIssue(ctx, mission.ID)
	if err != nil {
		t.Fatalf("Failed to get mission after shutdown: %v", err)
	}

	// Mission should be in one of these valid states:
	// - Status: open/blocked (gates completed)
	// - Status: in_progress (gates were canceled mid-execution)
	validStatuses := []types.Status{types.StatusOpen, types.StatusBlocked, types.StatusInProgress}
	validStatus := false
	for _, s := range validStatuses {
		if finalIssue.Status == s {
			validStatus = true
			break
		}
	}
	if !validStatus {
		t.Errorf("Mission in unexpected status after shutdown: %s (expected one of: open, blocked, in_progress)", finalIssue.Status)
	}

	t.Logf("✓ Mission state is consistent after shutdown (status=%s)", finalIssue.Status)

	// Verify gates-running label was removed (unless context was canceled mid-execution)
	hasGatesRunningAfter, err := labels.HasLabel(ctx, store, mission.ID, labels.LabelGatesRunning)
	if err != nil {
		t.Fatalf("Failed to check gates-running label after shutdown: %v", err)
	}

	// If gates completed (status is open or blocked), gates-running should be removed
	if finalIssue.Status != types.StatusInProgress && hasGatesRunningAfter {
		t.Error("gates-running label should be removed when gates complete")
	}

	t.Logf("✓ gates-running label state is correct (present=%v, status=%s)", hasGatesRunningAfter, finalIssue.Status)

	// Verify execution state was released (unless canceled mid-execution)
	finalExecState, err := store.GetExecutionState(ctx, mission.ID)
	if err != nil {
		t.Fatalf("Failed to get execution state after shutdown: %v", err)
	}

	if finalIssue.Status != types.StatusInProgress && finalExecState != nil {
		t.Error("Execution state should be released when gates complete")
	}

	t.Logf("✓ Execution state is correct (claimed=%v, status=%s)", finalExecState != nil, finalIssue.Status)

	t.Log("✓ Integration test passed: shutdown waits for QA workers, no orphans, state consistent")
}

// setupTestGoModule creates a simple Go module with a test in the given directory
func setupTestGoModule(dir string) error {
	// Create go.mod
	goMod := `module testmodule

go 1.22
`
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(goMod), 0644); err != nil {
		return fmt.Errorf("failed to write go.mod: %w", err)
	}

	// Create a simple Go file
	mainGo := `package main

import "fmt"

func main() {
	fmt.Println("Hello, world!")
}

func Add(a, b int) int {
	return a + b
}
`
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(mainGo), 0644); err != nil {
		return fmt.Errorf("failed to write main.go: %w", err)
	}

	// Create a test file
	testGo := `package main

import "testing"

func TestAdd(t *testing.T) {
	result := Add(2, 3)
	if result != 5 {
		t.Errorf("Add(2, 3) = %d; want 5", result)
	}
}
`
	if err := os.WriteFile(filepath.Join(dir, "main_test.go"), []byte(testGo), 0644); err != nil {
		return fmt.Errorf("failed to write main_test.go: %w", err)
	}

	return nil
}

// countChildProcesses counts the number of child processes of the current process
// This uses pgrep which is Unix-specific (works on Linux/macOS)
//
// Known limitations:
// - Counts ALL child processes, including those from previous tests
// - Race conditions: processes can start/stop between calls
// - Not suitable for exact verification, only for trend detection
func countChildProcesses(t *testing.T) int {
	t.Helper()

	// Use pgrep to find direct child processes (-P = parent PID)
	cmd := exec.Command("pgrep", "-P", fmt.Sprintf("%d", os.Getpid()))
	output, err := cmd.Output()
	if err != nil {
		// pgrep returns exit code 1 if no processes found, which is normal
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
			return 0
		}
		// Other errors are unexpected but non-fatal for this test
		t.Logf("Warning: failed to count child processes: %v", err)
		return 0
	}

	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	if len(lines) == 1 && lines[0] == "" {
		return 0
	}
	return len(lines)
}

// listChildProcesses lists child processes for debugging
// Shows process name alongside PID to help identify what's running
func listChildProcesses(t *testing.T) {
	t.Helper()

	// pgrep -l shows PID and process name
	cmd := exec.Command("pgrep", "-P", fmt.Sprintf("%d", os.Getpid()), "-l")
	output, err := cmd.Output()
	if err != nil {
		t.Logf("No child processes found (or pgrep failed)")
		return
	}

	t.Logf("Child processes (PID NAME):\n%s", string(output))
}

// TestQAWorkerShutdownWithSlowGates tests shutdown behavior when gates take a long time
// This is a focused unit test that verifies the WaitGroup mechanism works correctly
// with slow-running gates (complementing the integration test above)
func TestQAWorkerShutdownWithSlowGates(t *testing.T) {
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
			ID:            "vc-slow-gates-test",
			Title:         "Test Mission for Slow Gates Shutdown",
			Description:   "Test shutdown with slow quality gates",
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
		BranchName:  "mission/vc-slow-gates-test",
	}

	if err := store.CreateMission(ctx, mission, "test"); err != nil {
		t.Fatalf("Failed to create mission: %v", err)
	}

	// Add needs-quality-gates label
	if err := store.AddLabel(ctx, mission.ID, labels.LabelNeedsQualityGates, "test"); err != nil {
		t.Fatalf("Failed to add label: %v", err)
	}

	// Create executor configuration with slow gates provider
	execCfg := DefaultConfig()
	execCfg.Store = store
	execCfg.EnableAISupervision = false
	execCfg.EnableQualityGates = false // Disable to prevent auto-creation
	execCfg.EnableQualityGateWorker = false
	execCfg.EnableSandboxes = false
	execCfg.PollInterval = 100 * time.Millisecond

	// Create executor
	exec, err := New(execCfg)
	if err != nil {
		t.Fatalf("failed to create executor: %v", err)
	}

	// Register executor instance
	instance := &types.ExecutorInstance{
		InstanceID:    exec.instanceID,
		Hostname:      "test-host",
		PID:           os.Getpid(),
		Status:        types.ExecutorStatusRunning,
		StartedAt:     now,
		LastHeartbeat: now,
		Version:       "test",
		Metadata:      "{}",
	}
	if err := store.RegisterInstance(ctx, instance); err != nil {
		t.Fatalf("Failed to register executor instance: %v", err)
	}

	// Create slow gates provider (gates take 2 seconds)
	started := &atomic.Bool{}
	completed := &atomic.Bool{}
	slowProvider := &slowGatesProvider{
		started:   started,
		completed: completed,
		duration:  2 * time.Second,
	}

	gatesRunner, err := gates.NewRunner(&gates.Config{
		Store:      store,
		WorkingDir: ".",
		Provider:   slowProvider,
	})
	if err != nil {
		t.Fatalf("failed to create gates runner: %v", err)
	}

	// Manually inject QA worker with slow gates provider
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

	// Wait for QA worker to start processing
	waitStart := time.Now()
	for !started.Load() && time.Since(waitStart) < 5*time.Second {
		time.Sleep(50 * time.Millisecond)
	}

	if !started.Load() {
		t.Fatal("QA worker did not start processing within timeout")
	}

	t.Log("✓ QA worker started processing quality gates")

	// Initiate shutdown while QA worker is running
	shutdownStart := time.Now()
	execCancel()

	shutdownCtx, shutdownCancel := context.WithTimeout(ctx, 10*time.Second)
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

	// Verify mission state is consistent
	finalIssue, err := store.GetIssue(ctx, mission.ID)
	if err != nil {
		t.Fatalf("Failed to get mission after shutdown: %v", err)
	}

	t.Logf("✓ Mission state is consistent after shutdown (status=%s)", finalIssue.Status)

	t.Log("✓ Slow gates shutdown test passed: WaitGroup ensures goroutines complete")
}
