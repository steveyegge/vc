package executor

import (
	"testing"
	"time"

	"github.com/steveyegge/vc/internal/sandbox"
	"github.com/steveyegge/vc/internal/types"
)

// TestBuildClaudeCodeCommand_WithoutSandbox verifies autonomous operation without sandbox
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

	// Should have command name, --print flag, permission flag, and prompt (vc-q788: Claude Code requires --print)
	if len(cmd.Args) != 4 {
		t.Errorf("Expected 4 args [command, --print, permission-flag, prompt], got %d: %v", len(cmd.Args), cmd.Args)
	}

	// Check the prompt is the last argument
	if cmd.Args[len(cmd.Args)-1] != prompt {
		t.Errorf("Expected last arg to be prompt '%s', got '%s'", prompt, cmd.Args[len(cmd.Args)-1])
	}

	// Verify --print flag is present (vc-q788)
	hasPrintFlag := false
	for _, arg := range cmd.Args {
		if arg == "--print" {
			hasPrintFlag = true
			break
		}
	}
	if !hasPrintFlag {
		t.Error("Expected --print flag for non-interactive execution")
	}

	// Verify permission bypass flag is always present (vc-117)
	hasPermissionFlag := false
	for _, arg := range cmd.Args {
		if arg == "--dangerously-skip-permissions" {
			hasPermissionFlag = true
			break
		}
	}
	if !hasPermissionFlag {
		t.Error("Expected --dangerously-skip-permissions flag for autonomous operation")
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

	// Should have command name, --print flag, permission flag, and prompt (4 args total)
	if len(cmd.Args) != 4 {
		t.Errorf("Expected 4 args [command, --print, permission-flag, prompt], got %d: %v", len(cmd.Args), cmd.Args)
	}

	// Check the prompt is the last argument
	if cmd.Args[len(cmd.Args)-1] != prompt {
		t.Errorf("Expected last arg to be prompt '%s', got '%s'", prompt, cmd.Args[len(cmd.Args)-1])
	}

	// Verify --print flag is present (vc-q788)
	hasPrintFlag := false
	for _, arg := range cmd.Args {
		if arg == "--print" {
			hasPrintFlag = true
			break
		}
	}
	if !hasPrintFlag {
		t.Error("Expected --print flag for non-interactive execution")
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

// TestBuildClaudeCodeCommand_WithStreamJSON verifies streaming JSON output (vc-q788)
func TestBuildClaudeCodeCommand_WithStreamJSON(t *testing.T) {
	cfg := AgentConfig{
		Type:       AgentTypeClaudeCode,
		WorkingDir: "/tmp/test",
		Issue:      &types.Issue{ID: "vc-1", Title: "Test"},
		StreamJSON: true,
		Timeout:    5 * time.Minute,
	}
	prompt := "Fix the bug"

	cmd := buildClaudeCodeCommand(cfg, prompt)

	// Should have: [claude, --print, --dangerously-skip-permissions, --verbose, --output-format, stream-json, prompt]
	if len(cmd.Args) != 7 {
		t.Errorf("Expected 7 args with StreamJSON, got %d: %v", len(cmd.Args), cmd.Args)
	}

	// Verify --verbose flag is present (required for stream-json)
	hasVerbose := false
	for _, arg := range cmd.Args {
		if arg == "--verbose" {
			hasVerbose = true
			break
		}
	}
	if !hasVerbose {
		t.Error("Expected --verbose flag when StreamJSON is enabled")
	}

	// Verify --output-format stream-json
	hasOutputFormat := false
	for i, arg := range cmd.Args {
		if arg == "--output-format" && i+1 < len(cmd.Args) && cmd.Args[i+1] == "stream-json" {
			hasOutputFormat = true
			break
		}
	}
	if !hasOutputFormat {
		t.Error("Expected --output-format stream-json when StreamJSON is enabled")
	}
}

// TestBuildAmpCommand verifies amp command building with autonomous operation flags (vc-117)
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
	// Should have: [command_path, --dangerously-allow-all, --execute, prompt, --stream-json]
	if len(cmd.Args) != 5 {
		t.Errorf("Expected 5 args, got %d: %v", len(cmd.Args), cmd.Args)
	}

	// Verify --dangerously-allow-all flag (vc-117: always bypass for autonomous operation)
	if len(cmd.Args) < 2 || cmd.Args[1] != "--dangerously-allow-all" {
		t.Error("Expected --dangerously-allow-all flag as first flag")
	}

	// Verify --execute flag
	if len(cmd.Args) < 3 || cmd.Args[2] != "--execute" {
		t.Error("Expected --execute flag")
	}

	// Verify prompt
	if len(cmd.Args) < 4 || cmd.Args[3] != prompt {
		t.Errorf("Expected prompt '%s', got '%s'", prompt, cmd.Args[3])
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
