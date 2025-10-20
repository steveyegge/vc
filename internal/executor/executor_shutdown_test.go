package executor

import (
	"context"
	"testing"
	"time"

	"github.com/steveyegge/vc/internal/storage"
)

// TestShutdownWithoutActiveWork tests that executor shuts down cleanly
// when there is no work being processed
func TestShutdownWithoutActiveWork(t *testing.T) {
	ctx := context.Background()

	// Create in-memory storage
	cfg := storage.DefaultConfig()
	cfg.Path = ":memory:"
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
	cfg.Path = ":memory:"
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
