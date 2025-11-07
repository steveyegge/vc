package main

import (
	"context"
	"os"
	"os/exec"
	"syscall"
	"testing"
	"time"

	"github.com/steveyegge/vc/internal/storage/beads"
	"github.com/steveyegge/vc/internal/types"
)

func TestStopCommand_NoRunningExecutor(t *testing.T) {
	// Create in-memory database for testing
	ctx := context.Background()
	testStore, err := beads.NewVCStorage(ctx, ":memory:")
	if err != nil {
		t.Fatalf("Failed to create test storage: %v", err)
	}
	defer testStore.Close()

	// Override the global store for the test
	originalStore := store
	store = testStore
	defer func() { store = originalStore }()

	// Test stopping when no executor is running
	err = stopExecutor(30*time.Second, false)
	if err != nil {
		t.Errorf("stopExecutor should succeed when no executor is running, got error: %v", err)
	}
}

func TestStopCommand_StaleEntry(t *testing.T) {
	// Create in-memory database for testing
	ctx := context.Background()
	testStore, err := beads.NewVCStorage(ctx, ":memory:")
	if err != nil {
		t.Fatalf("Failed to create test storage: %v", err)
	}
	defer testStore.Close()

	// Override the global store for the test
	originalStore := store
	store = testStore
	defer func() { store = originalStore }()

	// Register an executor with a PID that doesn't exist
	executorInstance := &types.ExecutorInstance{
		InstanceID:    "test-executor-stale",
		Hostname:      "test-host",
		PID:           999999, // Unlikely to exist
		Version:       "test-v1",
		Status:        types.ExecutorStatusRunning,
		StartedAt:     time.Now().Add(-1 * time.Hour),
		LastHeartbeat: time.Now(),
	}

	err = testStore.RegisterInstance(ctx, executorInstance)
	if err != nil {
		t.Fatalf("Failed to register executor instance: %v", err)
	}

	// Stop should succeed and clean up the stale entry
	err = stopExecutor(30*time.Second, false)
	if err != nil {
		t.Errorf("stopExecutor should succeed with stale entry, got error: %v", err)
	}

	// Verify the instance was marked as stopped
	instances, err := testStore.GetActiveInstances(ctx)
	if err != nil {
		t.Fatalf("Failed to get active instances: %v", err)
	}

	if len(instances) != 0 {
		t.Errorf("Expected 0 active instances after cleanup, got %d", len(instances))
	}
}

func TestStopCommand_RealProcess(t *testing.T) {
	// Create in-memory database for testing
	ctx := context.Background()
	testStore, err := beads.NewVCStorage(ctx, ":memory:")
	if err != nil {
		t.Fatalf("Failed to create test storage: %v", err)
	}
	defer testStore.Close()

	// Override the global store for the test
	originalStore := store
	store = testStore
	defer func() { store = originalStore }()

	// Start a long-running sleep process that we can stop
	cmd := exec.Command("sleep", "300")
	err = cmd.Start()
	if err != nil {
		t.Fatalf("Failed to start test process: %v", err)
	}
	testPID := cmd.Process.Pid

	// Launch Wait() in goroutine so it can reap the process when killed
	waitDone := make(chan error, 1)
	go func() {
		waitDone <- cmd.Wait()
	}()
	defer func() {
		// Ensure cleanup even if test fails
		_ = syscall.Kill(testPID, syscall.SIGKILL)
		// Try to drain waitDone if not already drained, with timeout
		select {
		case <-waitDone:
		case <-time.After(1 * time.Second):
		}
	}()

	// Register the executor with the test process PID
	executorInstance := &types.ExecutorInstance{
		InstanceID:    "test-executor-real",
		Hostname:      "test-host",
		PID:           testPID,
		Version:       "test-v1",
		Status:        types.ExecutorStatusRunning,
		StartedAt:     time.Now(),
		LastHeartbeat: time.Now(),
	}

	err = testStore.RegisterInstance(ctx, executorInstance)
	if err != nil {
		t.Fatalf("Failed to register executor instance: %v", err)
	}

	// Stop the executor (should send SIGINT/SIGKILL to our sleep process)
	// Use a shorter timeout since sleep doesn't handle SIGINT gracefully
	err = stopExecutor(1*time.Second, false)
	if err != nil {
		t.Errorf("stopExecutor failed: %v", err)
	}

	// Wait for the process to be reaped
	select {
	case <-waitDone:
		// Process was reaped
	case <-time.After(2 * time.Second):
		t.Fatal("Process was not reaped within timeout")
	}

	// Small delay to ensure process state is updated
	time.Sleep(100 * time.Millisecond)

	// Verify the process no longer exists
	if processExists(testPID) {
		t.Errorf("Process %d should have been killed", testPID)
	}

	// Verify the instance was marked as stopped
	instances, err := testStore.GetActiveInstances(ctx)
	if err != nil {
		t.Fatalf("Failed to get active instances: %v", err)
	}

	if len(instances) != 0 {
		t.Errorf("Expected 0 active instances after stop, got %d", len(instances))
	}
}

func TestStopCommand_ForceKill(t *testing.T) {
	// Create in-memory database for testing
	ctx := context.Background()
	testStore, err := beads.NewVCStorage(ctx, ":memory:")
	if err != nil {
		t.Fatalf("Failed to create test storage: %v", err)
	}
	defer testStore.Close()

	// Override the global store for the test
	originalStore := store
	store = testStore
	defer func() { store = originalStore }()

	// Start a process that ignores SIGINT but will die on SIGKILL
	cmd := exec.Command("sleep", "300")
	err = cmd.Start()
	if err != nil {
		t.Fatalf("Failed to start test process: %v", err)
	}
	testPID := cmd.Process.Pid

	// Launch Wait() in goroutine so it can reap the process when killed
	waitDone := make(chan error, 1)
	go func() {
		waitDone <- cmd.Wait()
	}()
	defer func() {
		// Ensure cleanup even if test fails
		_ = syscall.Kill(testPID, syscall.SIGKILL)
		// Try to drain waitDone if not already drained, with timeout
		select {
		case <-waitDone:
		case <-time.After(1 * time.Second):
		}
	}()

	// Register the executor
	executorInstance := &types.ExecutorInstance{
		InstanceID:    "test-executor-force",
		Hostname:      "test-host",
		PID:           testPID,
		Version:       "test-v1",
		Status:        types.ExecutorStatusRunning,
		StartedAt:     time.Now(),
		LastHeartbeat: time.Now(),
	}

	err = testStore.RegisterInstance(ctx, executorInstance)
	if err != nil {
		t.Fatalf("Failed to register executor instance: %v", err)
	}

	// Stop with force flag (should send SIGKILL immediately)
	err = stopExecutor(5*time.Second, true)
	if err != nil {
		t.Errorf("stopExecutor with force failed: %v", err)
	}

	// Wait for the process to be reaped
	select {
	case <-waitDone:
		// Process was reaped
	case <-time.After(2 * time.Second):
		t.Fatal("Process was not reaped within timeout")
	}

	// Small delay to ensure process state is updated
	time.Sleep(100 * time.Millisecond)

	// Verify the process was killed
	if processExists(testPID) {
		t.Errorf("Process %d should have been killed", testPID)
	}
}

func TestProcessExists(t *testing.T) {
	// Test with current process (should exist)
	currentPID := os.Getpid()
	if !processExists(currentPID) {
		t.Errorf("Current process PID %d should exist", currentPID)
	}

	// Test with non-existent PID
	if processExists(999999) {
		t.Errorf("PID 999999 should not exist")
	}
}

func TestWaitForProcessExit(t *testing.T) {
	// Start a short-lived process
	cmd := exec.Command("sh", "-c", "sleep 0.1")
	err := cmd.Start()
	if err != nil {
		t.Fatalf("Failed to start test process: %v", err)
	}
	testPID := cmd.Process.Pid

	// Launch Wait() in goroutine to reap the process
	go func() {
		_ = cmd.Wait()
	}()

	// Wait for it to exit (should succeed quickly)
	err = waitForProcessExit(testPID, 5*time.Second)
	if err != nil {
		t.Errorf("waitForProcessExit should succeed, got error: %v", err)
	}

	// Verify it's gone
	if processExists(testPID) {
		t.Errorf("Process %d should have exited", testPID)
	}
}

func TestWaitForProcessExit_Timeout(t *testing.T) {
	// Start a long-lived process
	cmd := exec.Command("sleep", "300")
	err := cmd.Start()
	if err != nil {
		t.Fatalf("Failed to start test process: %v", err)
	}
	testPID := cmd.Process.Pid
	defer func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	}()

	// Wait with short timeout (should timeout)
	err = waitForProcessExit(testPID, 100*time.Millisecond)
	if err == nil {
		t.Errorf("waitForProcessExit should timeout, but succeeded")
	}

	// Process should still exist
	if !processExists(testPID) {
		t.Errorf("Process %d should still exist after timeout", testPID)
	}
}

func TestStopInstance_SignalHandling(t *testing.T) {
	// This test verifies that stopInstance correctly sends signals
	// Create in-memory database
	ctx := context.Background()
	testStore, err := beads.NewVCStorage(ctx, ":memory:")
	if err != nil {
		t.Fatalf("Failed to create test storage: %v", err)
	}
	defer testStore.Close()

	// Override the global store
	originalStore := store
	store = testStore
	defer func() { store = originalStore }()

	// Create a process that can handle signals gracefully
	// We'll use a shell script that traps SIGINT
	cmd := exec.Command("sh", "-c", `
		trap 'exit 0' INT TERM
		sleep 300
	`)
	err = cmd.Start()
	if err != nil {
		t.Fatalf("Failed to start test process: %v", err)
	}
	testPID := cmd.Process.Pid

	// Launch Wait() in goroutine so it can reap the process when killed
	waitDone := make(chan error, 1)
	go func() {
		waitDone <- cmd.Wait()
	}()
	defer func() {
		// Ensure cleanup even if test fails
		_ = syscall.Kill(testPID, syscall.SIGKILL)
		// Try to drain waitDone if not already drained, with timeout
		select {
		case <-waitDone:
		case <-time.After(1 * time.Second):
		}
	}()

	// Register the executor
	executorInstance := &types.ExecutorInstance{
		InstanceID:    "test-signal-handling",
		Hostname:      "test-host",
		PID:           testPID,
		Version:       "test-v1",
		Status:        types.ExecutorStatusRunning,
		StartedAt:     time.Now(),
		LastHeartbeat: time.Now(),
	}

	err = testStore.RegisterInstance(ctx, executorInstance)
	if err != nil {
		t.Fatalf("Failed to register executor instance: %v", err)
	}

	// Stop the instance (gracefully)
	err = stopInstance(ctx, executorInstance, 5*time.Second, false)
	if err != nil {
		t.Errorf("stopInstance failed: %v", err)
	}

	// Wait for the process to be reaped
	select {
	case <-waitDone:
		// Process was reaped
	case <-time.After(2 * time.Second):
		t.Fatal("Process was not reaped within timeout")
	}

	// Small delay to ensure process state is updated
	time.Sleep(100 * time.Millisecond)

	// Verify process is gone
	if processExists(testPID) {
		t.Errorf("Process should have exited after SIGINT")
	}

	// Verify database was updated
	instances, err := testStore.GetActiveInstances(ctx)
	if err != nil {
		t.Fatalf("Failed to get active instances: %v", err)
	}
	if len(instances) != 0 {
		t.Errorf("Expected 0 active instances, got %d", len(instances))
	}
}
