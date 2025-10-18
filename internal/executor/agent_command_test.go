package executor

import (
	"testing"
	"time"

	"github.com/steveyegge/vc/internal/sandbox"
	"github.com/steveyegge/vc/internal/types"
)

// TestBuildClaudeCodeCommand_WithoutSandbox verifies normal operation without sandbox
func TestBuildClaudeCodeCommand_WithoutSandbox(t *testing.T) {
	cfg := AgentConfig{
		Type:       AgentTypeClaudeCode,
		WorkingDir: "/tmp/test",
		Issue:      &types.Issue{ID: "vc-1", Title: "Test"},
		Timeout:    5 * time.Minute,
		Sandbox:    nil, // No sandbox
	}
	prompt := "Fix the bug"

	cmd := buildClaudeCodeCommand(cfg, prompt)

	// Verify command name (Args[0] contains the command name)
	if len(cmd.Args) < 1 {
		t.Fatal("Expected at least one argument (command name)")
	}

	// Should have command name and prompt only (no permission flags)
	if len(cmd.Args) != 2 {
		t.Errorf("Expected 2 args [command, prompt], got %d: %v", len(cmd.Args), cmd.Args)
	}

	// Check the prompt is the last argument
	if cmd.Args[len(cmd.Args)-1] != prompt {
		t.Errorf("Expected last arg to be prompt '%s', got '%s'", prompt, cmd.Args[len(cmd.Args)-1])
	}

	// Verify no permission flags present
	for _, arg := range cmd.Args {
		if arg == "--dangerously-skip-permissions" {
			t.Error("Did not expect --dangerously-skip-permissions flag without sandbox")
		}
	}
}

// TestBuildClaudeCodeCommand_WithSandbox verifies permission bypass in sandbox (vc-114)
func TestBuildClaudeCodeCommand_WithSandbox(t *testing.T) {
	cfg := AgentConfig{
		Type:       AgentTypeClaudeCode,
		WorkingDir: "/tmp/test",
		Issue:      &types.Issue{ID: "vc-1", Title: "Test"},
		Timeout:    5 * time.Minute,
		Sandbox:    &sandbox.Sandbox{}, // Sandbox present
	}
	prompt := "Fix the bug"

	cmd := buildClaudeCodeCommand(cfg, prompt)

	// Verify command name (Args[0] contains the command name)
	if len(cmd.Args) < 1 {
		t.Fatal("Expected at least one argument (command name)")
	}

	// Should have command name, permission flag, and prompt (3 args total)
	if len(cmd.Args) != 3 {
		t.Errorf("Expected 3 args [command, flag, prompt], got %d: %v", len(cmd.Args), cmd.Args)
	}

	// Check the prompt is the last argument
	if cmd.Args[len(cmd.Args)-1] != prompt {
		t.Errorf("Expected last arg to be prompt '%s', got '%s'", prompt, cmd.Args[len(cmd.Args)-1])
	}

	// Verify the permission flag is present
	hasPermissionFlag := false
	for _, arg := range cmd.Args {
		if arg == "--dangerously-skip-permissions" {
			hasPermissionFlag = true
			break
		}
	}
	if !hasPermissionFlag {
		t.Error("Expected --dangerously-skip-permissions flag when sandbox is present")
	}
}

// TestBuildAmpCommand verifies amp command building (existing behavior)
func TestBuildAmpCommand(t *testing.T) {
	cfg := AgentConfig{
		Type:       AgentTypeAmp,
		WorkingDir: "/tmp/test",
		Issue:      &types.Issue{ID: "vc-1", Title: "Test"},
		StreamJSON: true,
		Timeout:    5 * time.Minute,
	}
	prompt := "Fix the bug"

	cmd := buildAmpCommand(cfg, prompt)

	// Verify command has correct args
	// Should have: [command_path, --execute, prompt, --stream-json]
	if len(cmd.Args) != 4 {
		t.Errorf("Expected 4 args, got %d: %v", len(cmd.Args), cmd.Args)
	}

	// Verify --execute flag
	if len(cmd.Args) < 2 || cmd.Args[1] != "--execute" {
		t.Error("Expected --execute flag as second argument")
	}

	// Verify prompt
	if len(cmd.Args) < 3 || cmd.Args[2] != prompt {
		t.Errorf("Expected prompt '%s' as third argument, got '%s'", prompt, cmd.Args[2])
	}

	// Verify --stream-json flag
	hasStreamJSON := false
	for _, arg := range cmd.Args {
		if arg == "--stream-json" {
			hasStreamJSON = true
			break
		}
	}
	if !hasStreamJSON {
		t.Error("Expected --stream-json flag when StreamJSON is true")
	}
}
