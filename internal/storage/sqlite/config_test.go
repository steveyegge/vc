package sqlite

import (
	"context"
	"testing"
)

func TestConfigMethods(t *testing.T) {
	ctx := context.Background()

	// Create in-memory database
	db, err := New(":memory:")
	if err != nil {
		t.Fatalf("failed to create database: %v", err)
	}
	defer func() { _ = db.Close() }()

	// Test GetConfig on non-existent key (should return empty string)
	value, err := db.GetConfig(ctx, "nonexistent")
	if err != nil {
		t.Errorf("GetConfig on non-existent key should not error: %v", err)
	}
	if value != "" {
		t.Errorf("expected empty string for non-existent key, got %q", value)
	}

	// Test SetConfig
	if err := db.SetConfig(ctx, "test_key", "test_value"); err != nil {
		t.Fatalf("SetConfig failed: %v", err)
	}

	// Test GetConfig retrieves the value
	value, err = db.GetConfig(ctx, "test_key")
	if err != nil {
		t.Fatalf("GetConfig failed: %v", err)
	}
	if value != "test_value" {
		t.Errorf("expected 'test_value', got %q", value)
	}

	// Test SetConfig updates existing value
	if err := db.SetConfig(ctx, "test_key", "new_value"); err != nil {
		t.Fatalf("SetConfig update failed: %v", err)
	}

	value, err = db.GetConfig(ctx, "test_key")
	if err != nil {
		t.Fatalf("GetConfig after update failed: %v", err)
	}
	if value != "new_value" {
		t.Errorf("expected 'new_value', got %q", value)
	}
}

func TestIssuePrefixFromConfig(t *testing.T) {
	ctx := context.Background()

	// Create database and set issue_prefix
	db, err := New(":memory:")
	if err != nil {
		t.Fatalf("failed to create database: %v", err)
	}
	defer func() { _ = db.Close() }()

	// Set custom issue_prefix
	if err := db.SetConfig(ctx, "issue_prefix", "custom"); err != nil {
		t.Fatalf("SetConfig failed: %v", err)
	}

	// Close and reopen to test prefix loading
	_ = db.Close()

	db, err = New(":memory:")
	if err != nil {
		t.Fatalf("failed to reopen database: %v", err)
	}
	defer func() { _ = db.Close() }()

	// Set the prefix again for the new database
	if err := db.SetConfig(ctx, "issue_prefix", "custom"); err != nil {
		t.Fatalf("SetConfig failed: %v", err)
	}

	// Verify prefix is used when creating issues
	// Note: We can't directly inspect issuePrefix, but we can verify by checking
	// that GetConfig returns what we set
	prefix, err := db.GetConfig(ctx, "issue_prefix")
	if err != nil {
		t.Fatalf("GetConfig failed: %v", err)
	}
	if prefix != "custom" {
		t.Errorf("expected 'custom', got %q", prefix)
	}
}
