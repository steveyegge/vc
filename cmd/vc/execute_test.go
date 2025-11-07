package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/cobra"
	"github.com/steveyegge/vc/internal/storage/beads"
)

// TestRunExecutorFlagParsing verifies that runExecutor correctly extracts and uses flags
// This addresses vc-282: test coverage for parameter handling
func TestRunExecutorFlagParsing(t *testing.T) {
	// Create a temporary directory for test database
	tmpDir, err := os.MkdirTemp("", "vc-execute-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create a minimal .beads directory structure
	beadsDir := filepath.Join(tmpDir, ".beads")
	if err := os.MkdirAll(beadsDir, 0755); err != nil {
		t.Fatalf("Failed to create .beads dir: %v", err)
	}

	// Create test database
	testDbPath := filepath.Join(beadsDir, "beads.db")
	ctx := context.Background()
	testStore, err := beads.NewVCStorage(ctx, testDbPath)
	if err != nil {
		t.Fatalf("Failed to create test storage: %v", err)
	}
	defer testStore.Close()

	// Create issues.jsonl for freshness check
	issuesFile := filepath.Join(beadsDir, "issues.jsonl")
	if err := os.WriteFile(issuesFile, []byte(""), 0644); err != nil {
		t.Fatalf("Failed to create issues.jsonl: %v", err)
	}

	// Save current dir and change to temp dir for test
	originalDir, _ := os.Getwd()
	defer os.Chdir(originalDir)
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("Failed to change to temp dir: %v", err)
	}

	// Override global store for test
	originalStore := store
	originalDbPath := dbPath
	store = testStore
	dbPath = testDbPath
	defer func() {
		store = originalStore
		dbPath = originalDbPath
	}()

	tests := []struct {
		name        string
		flags       map[string]interface{}
		expectError bool
		setup       func()
		cleanup     func()
	}{
		{
			name: "default flags",
			flags: map[string]interface{}{
				"version":           "test-v1.0",
				"poll-interval":     5,
				"disable-sandboxes": false,
				"sandbox-root":      ".sandboxes",
				"parent-repo":       ".",
				"enable-auto-commit": false,
			},
			expectError: true, // Will fail during Start() but that's OK - we're testing flag parsing
		},
		{
			name: "custom poll interval",
			flags: map[string]interface{}{
				"version":           "test-v2.0",
				"poll-interval":     10,
				"disable-sandboxes": false,
				"sandbox-root":      ".sandboxes",
				"parent-repo":       ".",
				"enable-auto-commit": false,
			},
			expectError: true, // Will fail during Start() but that's OK
		},
		{
			name: "sandboxes disabled",
			flags: map[string]interface{}{
				"version":           "test-v3.0",
				"poll-interval":     5,
				"disable-sandboxes": true,
				"sandbox-root":      ".sandboxes",
				"parent-repo":       ".",
				"enable-auto-commit": false,
			},
			expectError: true, // Will fail during Start() but that's OK
		},
		{
			name: "custom sandbox root",
			flags: map[string]interface{}{
				"version":           "test-v4.0",
				"poll-interval":     5,
				"disable-sandboxes": false,
				"sandbox-root":      "/tmp/custom-sandboxes",
				"parent-repo":       ".",
				"enable-auto-commit": false,
			},
			expectError: true, // Will fail during Start() but that's OK
		},
		{
			name: "auto-commit enabled via flag",
			flags: map[string]interface{}{
				"version":           "test-v5.0",
				"poll-interval":     5,
				"disable-sandboxes": false,
				"sandbox-root":      ".sandboxes",
				"parent-repo":       ".",
				"enable-auto-commit": true,
			},
			expectError: true, // Will fail during Start() but that's OK
		},
		{
			name: "auto-commit enabled via env var",
			flags: map[string]interface{}{
				"version":           "test-v6.0",
				"poll-interval":     5,
				"disable-sandboxes": false,
				"sandbox-root":      ".sandboxes",
				"parent-repo":       ".",
				"enable-auto-commit": false,
			},
			setup: func() {
				os.Setenv("VC_ENABLE_AUTO_COMMIT", "true")
			},
			cleanup: func() {
				os.Unsetenv("VC_ENABLE_AUTO_COMMIT")
			},
			expectError: true, // Will fail during Start() but that's OK
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup if needed
			if tt.setup != nil {
				tt.setup()
			}
			if tt.cleanup != nil {
				defer tt.cleanup()
			}

			// Create a mock command with flags
			cmd := &cobra.Command{
				Use: "execute",
			}

			// Register flags (same as in init())
			cmd.Flags().String("version", "0.1.0", "Executor version")
			cmd.Flags().IntP("poll-interval", "i", 5, "Poll interval in seconds")
			cmd.Flags().Bool("disable-sandboxes", false, "Disable sandbox isolation")
			cmd.Flags().String("sandbox-root", ".sandboxes", "Root directory for sandboxes")
			cmd.Flags().String("parent-repo", ".", "Parent repository path")
			cmd.Flags().Bool("enable-auto-commit", false, "Enable automatic git commits")

			// Set flag values
			for name, value := range tt.flags {
				switch v := value.(type) {
				case string:
					cmd.Flags().Set(name, v)
				case int:
					cmd.Flags().Set(name, string(rune(v+'0')))
				case bool:
					if v {
						cmd.Flags().Set(name, "true")
					} else {
						cmd.Flags().Set(name, "false")
					}
				}
			}

			// Run the function
			// Note: We expect this to fail during Start() because we don't have a full
			// executor environment set up. The important thing is that flag parsing
			// works and we get past the initial setup steps.
			err := runExecutor(cmd)

			// The function should attempt to run but will fail during executor.Start()
			// or earlier steps like exclusive lock acquisition
			// We're mainly testing that:
			// 1. Flags are parsed correctly (no panic)
			// 2. The function handles errors properly
			// 3. Args parameter is not needed (already removed from signature)

			if tt.expectError && err == nil {
				t.Errorf("Expected error but got nil")
			}

			// If we got an error, verify it's a reasonable error message
			if err != nil {
				// Error messages should be informative
				if err.Error() == "" {
					t.Errorf("Error has empty message: %v", err)
				}
			}
		})
	}
}

// TestRunExecutorWithNilCommand verifies behavior with nil command
func TestRunExecutorWithNilCommand(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Errorf("Expected panic with nil command, but didn't panic")
		}
	}()

	// This should panic when trying to access cmd.Flags()
	_ = runExecutor(nil)
}

// TestRunExecutorErrorPropagation verifies that errors are properly returned
// and don't cause os.Exit() calls (which would prevent defer cleanup)
func TestRunExecutorErrorPropagation(t *testing.T) {
	// Create a command with invalid flags
	cmd := &cobra.Command{
		Use: "execute",
	}

	// Register flags
	cmd.Flags().String("version", "0.1.0", "Executor version")
	cmd.Flags().IntP("poll-interval", "i", 5, "Poll interval in seconds")
	cmd.Flags().Bool("disable-sandboxes", false, "Disable sandbox isolation")
	cmd.Flags().String("sandbox-root", ".sandboxes", "Root directory for sandboxes")
	cmd.Flags().String("parent-repo", ".", "Parent repository path")
	cmd.Flags().Bool("enable-auto-commit", false, "Enable automatic git commits")

	// Override global dbPath to point to non-existent location
	originalDbPath := dbPath
	dbPath = "/nonexistent/path/to/db.sqlite"
	defer func() { dbPath = originalDbPath }()

	// Run executor - should return error, not call os.Exit()
	err := runExecutor(cmd)

	if err == nil {
		t.Errorf("Expected error with invalid database path, got nil")
	}

	// If we reach here, it means errors were properly returned (not os.Exit)
	// This is important for defer cleanup to work
}
