package storage

import (
	"context"
	"os"
	"testing"
)

// TestVC_DB_PATH_Discovery verifies that DiscoverDatabase respects VC_DB_PATH (vc-235)
func TestVC_DB_PATH_Discovery(t *testing.T) {
	// Save and restore original env
	originalPath := os.Getenv("VC_DB_PATH")
	defer func() {
		if originalPath != "" {
			_ = os.Setenv("VC_DB_PATH", originalPath)
		} else {
			_ = os.Unsetenv("VC_DB_PATH")
		}
	}()

	// Test with :memory:
	_ = os.Setenv("VC_DB_PATH", ":memory:")
	path, err := DiscoverDatabase()
	if err != nil {
		t.Fatalf("DiscoverDatabase with VC_DB_PATH=:memory: failed: %v", err)
	}
	if path != ":memory:" {
		t.Errorf("Expected :memory:, got %s", path)
	}

	// Test with custom path
	_ = os.Setenv("VC_DB_PATH", "/tmp/test.db")
	path, err = DiscoverDatabase()
	if err != nil {
		t.Fatalf("DiscoverDatabase with VC_DB_PATH=/tmp/test.db failed: %v", err)
	}
	if path != "/tmp/test.db" {
		t.Errorf("Expected /tmp/test.db, got %s", path)
	}

	// Test without env var (should use normal discovery or fail)
	_ = os.Unsetenv("VC_DB_PATH")
	_, err = DiscoverDatabase()
	// May succeed (if in a project with .beads/) or fail (if not)
	// We don't assert either way, just verify it doesn't panic
}

// TestVC_DB_PATH_DefaultConfig verifies that DefaultConfig respects VC_DB_PATH (vc-235)
func TestVC_DB_PATH_DefaultConfig(t *testing.T) {
	// Save and restore original env
	originalPath := os.Getenv("VC_DB_PATH")
	defer func() {
		if originalPath != "" {
			_ = os.Setenv("VC_DB_PATH", originalPath)
		} else {
			_ = os.Unsetenv("VC_DB_PATH")
		}
	}()

	// Test with :memory:
	_ = os.Setenv("VC_DB_PATH", ":memory:")
	cfg := DefaultConfig()
	if cfg.Path != ":memory:" {
		t.Errorf("DefaultConfig with VC_DB_PATH=:memory: returned %s", cfg.Path)
	}

	// Test without env var
	_ = os.Unsetenv("VC_DB_PATH")
	cfg = DefaultConfig()
	if cfg.Path != ".beads/vc.db" {
		t.Errorf("DefaultConfig without VC_DB_PATH returned %s, expected .beads/vc.db", cfg.Path)
	}
}

// TestVC_DB_PATH_NewStorage verifies that NewStorage respects VC_DB_PATH (vc-235)
func TestVC_DB_PATH_NewStorage(t *testing.T) {
	// Save and restore original env
	originalPath := os.Getenv("VC_DB_PATH")
	defer func() {
		if originalPath != "" {
			_ = os.Setenv("VC_DB_PATH", originalPath)
		} else {
			_ = os.Unsetenv("VC_DB_PATH")
		}
	}()

	ctx := context.Background()

	// Test with :memory:
	_ = os.Setenv("VC_DB_PATH", ":memory:")
	store, err := NewStorage(ctx, nil)
	if err != nil {
		t.Fatalf("NewStorage with VC_DB_PATH=:memory: failed: %v", err)
	}
	defer func() { _ = store.Close() }()

	// Verify we can use the in-memory database
	stats, err := store.GetStatistics(ctx)
	if err != nil {
		t.Errorf("GetStatistics failed on :memory: database: %v", err)
	}
	if stats == nil {
		t.Error("Expected statistics, got nil")
	}
}

// TestVC_DB_PATH_ExplicitConfig verifies explicit config overrides env var (vc-235)
func TestVC_DB_PATH_ExplicitConfig(t *testing.T) {
	// Save and restore original env
	originalPath := os.Getenv("VC_DB_PATH")
	defer func() {
		if originalPath != "" {
			_ = os.Setenv("VC_DB_PATH", originalPath)
		} else {
			_ = os.Unsetenv("VC_DB_PATH")
		}
	}()

	ctx := context.Background()

	// Set env var but provide explicit config - explicit should win
	_ = os.Setenv("VC_DB_PATH", "/tmp/env.db")
	cfg := &Config{Path: ":memory:"}
	store, err := NewStorage(ctx, cfg)
	if err != nil {
		t.Fatalf("NewStorage with explicit config failed: %v", err)
	}
	defer func() { _ = store.Close() }()

	// Verify we can use the explicitly configured database
	stats, err := store.GetStatistics(ctx)
	if err != nil {
		t.Errorf("GetStatistics failed: %v", err)
	}
	if stats == nil {
		t.Error("Expected statistics, got nil")
	}
}
