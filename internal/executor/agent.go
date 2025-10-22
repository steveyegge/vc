package executor

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/steveyegge/vc/internal/events"
	"github.com/steveyegge/vc/internal/sandbox"
	"github.com/steveyegge/vc/internal/storage"
	"github.com/steveyegge/vc/internal/types"
)

// AgentType represents the type of coding agent to spawn
type AgentType string

const (
	AgentTypeAmp         AgentType = "amp"         // Sourcegraph Amp (agentic)
	AgentTypeClaudeCode  AgentType = "claude-code" // Anthropic Claude Code
)

// AgentConfig holds configuration for spawning an agent
type AgentConfig struct {
	Type        AgentType
	WorkingDir  string
	Issue       *types.Issue
	StreamJSON  bool
	Timeout     time.Duration
	// Event parsing and storage (optional - if nil, events won't be captured)
	Store      storage.Storage
	ExecutorID string
	AgentID    string
	// Sandbox context (optional - if nil, agent runs in main workspace)
	Sandbox    *sandbox.Sandbox
}

const (
	// maxOutputLines is the maximum number of output lines to capture
	// This prevents memory exhaustion from long-running agents
	maxOutputLines = 10000
)

// AgentResult contains the output and status from agent execution
type AgentResult struct {
	Success    bool
	Output     []string        // Captured stdout lines (capped at maxOutputLines)
	Errors     []string        // Captured stderr lines (capped at maxOutputLines)
	ExitCode   int
	Duration   time.Duration
	ParsedJSON []AgentMessage  // Parsed JSON messages if StreamJSON=true
}

// AgentMessage represents a JSON message from the agent.
// This matches Amp's --stream-json output format (vc-236).
//
// Event Types:
// - "tool_use": Agent is invoking a tool (Read, Edit, Write, Bash, Glob, Grep, Task)
// - "system": System messages (init, shutdown, etc.)
// - "result": Final result/completion message
//
// Tool-to-Field Mapping (for type="tool_use"):
// - Read/Edit/Write tools: use File field (target file path)
// - Bash tool: uses Command field (shell command to execute)
// - Glob/Grep tools: use Pattern field (search pattern)
// - Task tool: uses Data field (agent spawn parameters)
//
// Example JSON (tool_use):
//
//	{"type":"tool_use","tool":"read","file":"main.go"}
//	{"type":"tool_use","tool":"bash","command":"go test ./..."}
//	{"type":"tool_use","tool":"grep","pattern":"TODO"}
//
// Note: This struct is designed for Amp's JSON output. When StreamJSON=false,
// agent output is parsed via regex patterns in parser.go instead.
type AgentMessage struct {
	Type    string                 `json:"type"`    // Event type (e.g., "tool_use", "system", "result")
	Subtype string                 `json:"subtype,omitempty"` // Event subtype (e.g., "init", "error_during_execution")
	Content string                 `json:"content,omitempty"` // Human-readable message content
	Data    map[string]interface{} `json:"data,omitempty"`    // Additional event-specific data

	// Tool usage fields (vc-236: structured events instead of regex parsing)
	// Only populated when Type="tool_use"
	Tool       string `json:"tool,omitempty"`       // Tool name (e.g., "read", "edit", "bash")
	File       string `json:"file,omitempty"`       // Target file for Read/Edit/Write tools
	Command    string `json:"command,omitempty"`    // Shell command for Bash tool
	Pattern    string `json:"pattern,omitempty"`    // Search pattern for Glob/Grep tools

	// Session metadata
	SessionID string `json:"session_id,omitempty"` // Agent session identifier
}

// Agent represents a running coding agent process
type Agent struct {
	cmd       *exec.Cmd
	config    AgentConfig
	stdout    io.ReadCloser
	stderr    io.ReadCloser
	startTime time.Time
	ctx       context.Context // Context for storage operations

	mu     sync.Mutex
	result AgentResult
	parser *events.OutputParser // Parser for extracting events from output
}

// SpawnAgent starts a coding agent process with a pre-built prompt
func SpawnAgent(ctx context.Context, cfg AgentConfig, prompt string) (*Agent, error) {
	// Validate config
	if cfg.Issue == nil {
		return nil, fmt.Errorf("issue is required")
	}
	if cfg.WorkingDir == "" {
		cfg.WorkingDir = "."
	}
	if cfg.Timeout == 0 {
		cfg.Timeout = 30 * time.Minute // Default 30min timeout
	}
	if prompt == "" {
		return nil, fmt.Errorf("prompt is required")
	}

	// Build the command based on agent type
	var cmd *exec.Cmd
	switch cfg.Type {
	case AgentTypeAmp:
		cmd = buildAmpCommand(cfg, prompt)
	case AgentTypeClaudeCode:
		cmd = buildClaudeCodeCommand(cfg, prompt)
	default:
		return nil, fmt.Errorf("unsupported agent type: %s", cfg.Type)
	}

	// Set working directory
	cmd.Dir = cfg.WorkingDir

	// Create pipes for stdout/stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("failed to create stdout pipe: %w", err)
	}

	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, fmt.Errorf("failed to create stderr pipe: %w", err)
	}

	// Start the process
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("failed to start agent: %w", err)
	}

	agent := &Agent{
		cmd:       cmd,
		config:    cfg,
		stdout:    stdout,
		stderr:    stderr,
		startTime: time.Now(),
		ctx:       ctx,
		result: AgentResult{
			Output: []string{},
			Errors: []string{},
		},
	}

	// Initialize OutputParser if event storage is enabled
	if cfg.Store != nil && cfg.Issue != nil {
		agent.parser = events.NewOutputParser(cfg.Issue.ID, cfg.ExecutorID, cfg.AgentID)
	}

	// Start goroutines to capture output
	go agent.captureOutput()

	return agent, nil
}

// Wait waits for the agent to complete and returns the result
func (a *Agent) Wait(ctx context.Context) (*AgentResult, error) {
	// Check if parent context is already done
	select {
	case <-ctx.Done():
		return nil, fmt.Errorf("agent wait called with already-cancelled context: %w", ctx.Err())
	default:
	}

	// Create a context with timeout
	timeoutCtx, cancel := context.WithTimeout(ctx, a.config.Timeout)
	defer cancel()

	// Wait for process to complete or timeout
	errCh := make(chan error, 1)
	go func() {
		errCh <- a.cmd.Wait()
	}()

	select {
	case <-timeoutCtx.Done():
		// Check why timeout context was cancelled
		// context.DeadlineExceeded means actual timeout
		// context.Canceled means parent context was cancelled
		if timeoutCtx.Err() == context.DeadlineExceeded {
			// Actual timeout - kill the process
			if err := a.Kill(); err != nil {
				return nil, fmt.Errorf("agent execution timed out after %v (kill failed: %w)", a.config.Timeout, err)
			}
			return nil, fmt.Errorf("agent execution timed out after %v", a.config.Timeout)
		}
		// Parent context was cancelled (not a timeout)
		if err := a.Kill(); err != nil {
			return nil, fmt.Errorf("agent execution cancelled (parent context): %w (kill failed: %v)", timeoutCtx.Err(), err)
		}
		return nil, fmt.Errorf("agent execution cancelled (parent context): %w", timeoutCtx.Err())
	case err := <-errCh:
		// Process completed
		a.mu.Lock()
		defer a.mu.Unlock()

		a.result.Duration = time.Since(a.startTime)

		if err != nil {
			if exitErr, ok := err.(*exec.ExitError); ok {
				a.result.ExitCode = exitErr.ExitCode()
			}
			a.result.Success = false
			// Return the agent's result even if it failed - the error will be handled by caller
			// The actual execution completed, it just wasn't successful
		} else {
			a.result.ExitCode = 0
			a.result.Success = true
		}

		return &a.result, nil
	}
}

// Kill forcefully terminates the agent process
func (a *Agent) Kill() error {
	if a.cmd.Process != nil {
		return a.cmd.Process.Kill()
	}
	return nil
}

// captureOutput reads stdout/stderr and stores in result
// If event parsing is enabled, it also parses lines into structured events and stores them
func (a *Agent) captureOutput() {
	var wg sync.WaitGroup
	wg.Add(2)

	// Capture stdout
	go func() {
		defer wg.Done()
		scanner := bufio.NewScanner(a.stdout)
		for scanner.Scan() {
			line := scanner.Text()
			a.mu.Lock()

			// Only append if we haven't reached the limit
			if len(a.result.Output) < maxOutputLines {
				a.result.Output = append(a.result.Output, line)
				// Print inside mutex to ensure ordering matches captured output
				fmt.Println(line)
			} else if len(a.result.Output) == maxOutputLines {
				// Add truncation marker once
				a.result.Output = append(a.result.Output, "[... output truncated: limit reached ...]")
			}

			// Parse JSON if streaming JSON mode
			if a.config.StreamJSON && len(a.result.ParsedJSON) < maxOutputLines {
				var msg AgentMessage
				if err := json.Unmarshal([]byte(line), &msg); err == nil {
					a.result.ParsedJSON = append(a.result.ParsedJSON, msg)
				}
			}

			// Parse line for events if parser is enabled
			if a.parser != nil && a.config.Store != nil {
				a.parseAndStoreEvents(line)
			}

			a.mu.Unlock()
		}
	}()

	// Capture stderr
	go func() {
		defer wg.Done()
		scanner := bufio.NewScanner(a.stderr)
		for scanner.Scan() {
			line := scanner.Text()
			a.mu.Lock()

			// Only append if we haven't reached the limit
			if len(a.result.Errors) < maxOutputLines {
				a.result.Errors = append(a.result.Errors, line)
				// Print inside mutex to ensure ordering matches captured output
				fmt.Fprintln(os.Stderr, line)
			} else if len(a.result.Errors) == maxOutputLines {
				// Add truncation marker once
				a.result.Errors = append(a.result.Errors, "[... error output truncated: limit reached ...]")
			}

			// Parse stderr for events too (errors, warnings, etc.)
			if a.parser != nil && a.config.Store != nil {
				a.parseAndStoreEvents(line)
			}

			a.mu.Unlock()
		}
	}()

	wg.Wait()
}

// parseAndStoreEvents parses a line for events and stores them immediately
// This method should be called with the mutex held
// vc-236: First tries to parse as JSON (structured events from Amp), then falls back to regex patterns
func (a *Agent) parseAndStoreEvents(line string) {
	var extractedEvents []*events.AgentEvent

	// If StreamJSON mode, try to parse as JSON first (vc-236)
	if a.config.StreamJSON {
		var msg AgentMessage
		if err := json.Unmarshal([]byte(line), &msg); err == nil {
			// Successfully parsed JSON - convert to agent event
			if event := a.convertJSONToEvent(msg, line); event != nil {
				extractedEvents = append(extractedEvents, event)
			}
			// Don't fall through to regex parsing - we have structured data
		} else {
			// Not valid JSON, fall back to regex parsing (for mixed output)
			extractedEvents = a.parser.ParseLine(line)
		}
	} else {
		// No StreamJSON - use traditional regex parsing
		extractedEvents = a.parser.ParseLine(line)
	}

	// Store each event immediately
	for _, event := range extractedEvents {
		// Store event asynchronously to avoid blocking output capture
		go func(evt *events.AgentEvent) {
			if err := a.config.Store.StoreAgentEvent(a.ctx, evt); err != nil {
				// Log error but don't fail - event storage is best-effort
				fmt.Fprintf(os.Stderr, "warning: failed to store agent event: %v\n", err)
			}
		}(event)
	}
}

// convertJSONToEvent converts an Amp JSON message to an AgentEvent (vc-236)
// This replaces regex-based parsing with structured event processing.
//
// Set VC_DEBUG_EVENTS=1 to enable debug logging of JSON event parsing.
func (a *Agent) convertJSONToEvent(msg AgentMessage, rawLine string) *events.AgentEvent {
	// Only process tool_use events for now
	// Other event types (system, result, etc.) are informational but not progress events
	if msg.Type != "tool_use" {
		// Debug log non-tool_use events if debugging is enabled
		if os.Getenv("VC_DEBUG_EVENTS") != "" {
			fmt.Fprintf(os.Stderr, "[DEBUG] Skipping non-tool_use event: type=%s subtype=%s\n", msg.Type, msg.Subtype)
		}
		return nil
	}

	// Create agent_tool_use event from structured JSON
	event := &events.AgentEvent{
		ID:         uuid.New().String(),
		Type:       events.EventTypeAgentToolUse,
		Timestamp:  time.Now(),
		IssueID:    a.config.Issue.ID,
		ExecutorID: a.config.ExecutorID,
		AgentID:    a.config.AgentID,
		Severity:   events.SeverityInfo,
		Message:    rawLine,
		SourceLine: a.parser.LineNumber,
	}

	// Extract tool usage data from structured JSON (no regex needed!)
	toolData := events.AgentToolUseData{
		ToolName:   msg.Tool,
		TargetFile: msg.File,
		Command:    msg.Command,
	}

	// Build a human-readable description from the structured data
	if msg.File != "" {
		toolData.ToolDescription = fmt.Sprintf("%s %s", msg.Tool, msg.File)
	} else if msg.Command != "" {
		toolData.ToolDescription = fmt.Sprintf("run: %s", msg.Command)
	} else if msg.Pattern != "" {
		toolData.ToolDescription = fmt.Sprintf("search: %s", msg.Pattern)
	} else {
		toolData.ToolDescription = msg.Tool
	}

	// Set the data
	if err := event.SetAgentToolUseData(toolData); err != nil {
		fmt.Fprintf(os.Stderr, "warning: failed to set tool use data: %v\n", err)
		return nil
	}

	// Debug log successful event conversion
	if os.Getenv("VC_DEBUG_EVENTS") != "" {
		fmt.Fprintf(os.Stderr, "[DEBUG] Parsed tool_use event: tool=%s file=%s command=%s pattern=%s\n",
			msg.Tool, msg.File, msg.Command, msg.Pattern)
	}

	return event
}

// buildClaudeCodeCommand constructs the Claude Code CLI command
func buildClaudeCodeCommand(cfg AgentConfig, prompt string) *exec.Cmd {
	args := []string{}

	// Always bypass permission checks for autonomous agent operation (vc-117)
	// This is required for VC to operate autonomously without human intervention
	// Safe because:
	// 1. When sandboxed: Isolated environment with no risk to main codebase
	// 2. When not sandboxed: VC is designed to work autonomously on its own codebase
	//    and the results go through quality gates before being committed
	args = append(args, "--dangerously-skip-permissions")

	// Claude Code uses the message directly
	args = append(args, prompt)

	return exec.Command("claude", args...)
}

// buildAmpCommand constructs the Sourcegraph amp CLI command
func buildAmpCommand(cfg AgentConfig, prompt string) *exec.Cmd {
	args := []string{}

	// Always bypass permission checks for autonomous agent operation (vc-117)
	// This is required for VC to operate autonomously without human intervention
	// Safe because:
	// 1. When sandboxed: Isolated environment with no risk to main codebase
	// 2. When not sandboxed: VC is designed to work autonomously on its own codebase
	//    and the results go through quality gates before being committed
	args = append(args, "--dangerously-allow-all")

	// amp requires --execute for single-shot execution mode
	args = append(args, "--execute", prompt)
	if cfg.StreamJSON {
		args = append(args, "--stream-json")
	}

	return exec.Command("amp", args...)
}

// GetOutput returns a copy of the current output
func (a *Agent) GetOutput() []string {
	a.mu.Lock()
	defer a.mu.Unlock()

	output := make([]string, len(a.result.Output))
	copy(output, a.result.Output)
	return output
}

// GetErrors returns a copy of the current errors
func (a *Agent) GetErrors() []string {
	a.mu.Lock()
	defer a.mu.Unlock()

	errors := make([]string, len(a.result.Errors))
	copy(errors, a.result.Errors)
	return errors
}
