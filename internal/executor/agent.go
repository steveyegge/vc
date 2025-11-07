package executor

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
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
	// Watchdog monitoring (optional - if nil, events won't be reported to watchdog)
	Monitor    interface{ RecordEvent(eventType string) }
	// Sandbox context (optional - if nil, agent runs in main workspace)
	Sandbox    *sandbox.Sandbox
}

const (
	// maxOutputLines is the maximum number of output lines to capture
	// This prevents memory exhaustion from long-running agents
	maxOutputLines = 10000

	// maxFileReads is the maximum number of Read tool invocations per execution (vc-117)
	// This is a catastrophic failure backstop - watchdog should detect stuck agents before this
	// Set high enough to allow normal exploration but catch truly pathological loops
	// Pragmatic hybrid: ZFC violation for safety, but high enough to rarely trigger
	maxFileReads = 500

	// maxSameFileReads is the maximum number of times the same file can be read (vc-117)
	// Backstop for pathological same-file loops - watchdog should catch normal stuckness
	// Even complex refactoring shouldn't read the same file 20+ times
	maxSameFileReads = 20
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
// This matches Amp's --stream-json output format (vc-236, vc-107, vc-29, vc-30).
//
// Amp Version: Verified with Amp 0.0.1761854483-g125cd7 (2025-10-30)
// The --stream-json flag was introduced to provide structured event streaming for tools like VC.
//
// Top-Level Event Types:
// - "system": System events (init, shutdown, etc.) - includes "subtype" field
// - "user": User messages being sent to the agent
// - "assistant": Agent responses - contains "message" with nested "content" array
// - "result": Final execution result (success/failure)
//
// Actual Amp JSON Format Examples:
//
// 1. System Init Event:
//
//	{"type":"system","subtype":"init","cwd":"/path","session_id":"T-xxx","tools":["Read","Bash",...]}
//
// 2. Assistant Message with Tool Use (NESTED structure):
//
//	{
//	  "type": "assistant",
//	  "message": {
//	    "content": [
//	      {"type": "text", "text": "I'll read the file."},
//	      {"type": "tool_use", "id": "toolu_xxx", "name": "Read", "input": {"path": "/path/to/file"}}
//	    ],
//	    "stop_reason": "tool_use"
//	  },
//	  "session_id": "T-xxx"
//	}
//
// 3. Tool Names and Input Fields:
//   - Read: {"name": "Read", "input": {"path": "/path/to/file"}}
//   - edit_file: {"name": "edit_file", "input": {"path": "/path", "old_str": "...", "new_str": "..."}}
//   - create_file: {"name": "create_file", "input": {"path": "/path", "content": "..."}}
//   - Bash: {"name": "Bash", "input": {"cmd": "go test", "cwd": "/path"}}
//   - Grep: {"name": "Grep", "input": {"pattern": "TODO", "path": "/path"}}
//   - glob: {"name": "glob", "input": {"pattern": "*.go", "path": "/path"}}
//   - Task: {"name": "Task", "input": {"description": "...", "prompt": "..."}}
//
// Tool Name Conventions:
// - Capitalized: Read, Bash, Grep, Task
// - Lowercase with underscores: edit_file, create_file, todo_write
// - MCP tools: mcp__beads__show, mcp__beads__list, mcp__playwright__browser_click, etc.
//
// Input Field Mappings (tool-specific parameters):
// - Read/edit_file/create_file: Use "path" field for file path
// - Bash: Uses "cmd" field for command, optional "cwd" for working directory
// - Grep/glob: Use "pattern" field for search pattern, "path" for search location
// - Task: Uses "description" and "prompt" fields
//
// Important: Tool use is NESTED inside assistant messages, not at the top level.
// The AgentMessage struct represents the outer envelope. To extract tool usage,
// you must parse the nested "message.content" array and look for items with type="tool_use".
//
// When StreamJSON=false, agent output is parsed via regex patterns in parser.go instead.
type AgentMessage struct {
	// Top-level fields (all event types)
	Type      string                 `json:"type"`              // Event type: "system", "user", "assistant", "result"
	Subtype   string                 `json:"subtype,omitempty"` // Event subtype (e.g., "init" for system events)
	SessionID string                 `json:"session_id,omitempty"` // Agent session identifier

	// System event fields
	Cwd    string   `json:"cwd,omitempty"`   // Current working directory (system init events)
	Tools  []string `json:"tools,omitempty"` // Available tools (system init events)

	// Result event fields
	DurationMs int    `json:"duration_ms,omitempty"` // Execution duration (result events)
	IsError    bool   `json:"is_error,omitempty"`    // Whether execution failed (result events)
	Result     string `json:"result,omitempty"`      // Final result message (result events)
	NumTurns   int    `json:"num_turns,omitempty"`   // Number of conversation turns (result events)

	// Assistant message wrapper (contains nested tool use)
	Message *AssistantMessage `json:"message,omitempty"` // Nested message structure (assistant events)

	// Legacy fields (no longer used by Amp --stream-json, kept for backward compatibility)
	Content string                 `json:"content,omitempty"` // Human-readable message content (user events)
	Data    map[string]interface{} `json:"data,omitempty"`    // Additional event-specific data
}

// AssistantMessage represents the nested message structure in assistant events.
// This contains the actual content array with text and tool use.
type AssistantMessage struct {
	Type       string                   `json:"type"`                  // Always "message"
	Role       string                   `json:"role"`                  // Always "assistant"
	Content    []MessageContent         `json:"content"`               // Array of text and tool_use items
	StopReason string                   `json:"stop_reason,omitempty"` // Why the agent stopped: "tool_use", "end_turn", etc.
	Usage      map[string]interface{}   `json:"usage,omitempty"`       // Token usage statistics
}

// MessageContent represents an item in the assistant message content array.
// Can be either text or tool use.
type MessageContent struct {
	Type  string                 `json:"type"`             // "text" or "tool_use"
	Text  string                 `json:"text,omitempty"`   // Text content (when type="text")
	ID    string                 `json:"id,omitempty"`     // Tool invocation ID (when type="tool_use")
	Name  string                 `json:"name,omitempty"`   // Tool name (when type="tool_use")
	Input map[string]interface{} `json:"input,omitempty"`  // Tool parameters (when type="tool_use")
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

	// Circuit breaker state for detecting infinite loops (vc-117)
	totalReadCount int            // Total number of Read tool invocations
	fileReadCounts map[string]int // Number of times each file has been read
	loopDetected   atomic.Bool    // Whether an infinite loop was detected (lock-free for monitoring goroutine)
	loopReason     string         // Reason for loop detection (for error messages)
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
		// Initialize circuit breaker state (vc-117)
		totalReadCount: 0,
		fileReadCounts: make(map[string]int),
		loopReason:     "",
		// loopDetected is atomic.Bool and initializes to false automatically
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
		return nil, fmt.Errorf("agent wait called with already-canceled context: %w", ctx.Err())
	default:
	}

	// Create a context with timeout
	timeoutCtx, cancel := context.WithTimeout(ctx, a.config.Timeout)
	defer cancel()

	// Start a monitoring goroutine to check for circuit breaker triggers
	// This runs outside the mutex to avoid deadlocks (vc-5783)
	monitorDone := make(chan struct{})
	go func() {
		defer close(monitorDone)
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()
		
		for {
			select {
			case <-ticker.C:
				// Check if circuit breaker was triggered (lock-free read via atomic)
				if a.loopDetected.Load() {
					// Kill the agent
					if err := a.Kill(); err != nil {
						fmt.Fprintf(os.Stderr, "warning: failed to kill agent after circuit breaker: %v\n", err)
					}
					return
				}
			case <-monitorDone:
				return
			}
		}
	}()

	// Wait for process to complete or timeout
	errCh := make(chan error, 1)
	go func() {
		errCh <- a.cmd.Wait()
	}()

	select {
	case <-timeoutCtx.Done():
		// Check why timeout context was canceled
		// context.DeadlineExceeded means actual timeout
		// context.Canceled means parent context was canceled
		if timeoutCtx.Err() == context.DeadlineExceeded {
			// Actual timeout - kill the process
			if err := a.Kill(); err != nil {
				return nil, fmt.Errorf("agent execution timed out after %v (kill failed: %w)", a.config.Timeout, err)
			}
			return nil, fmt.Errorf("agent execution timed out after %v", a.config.Timeout)
		}
		// Parent context was canceled (not a timeout)
		if err := a.Kill(); err != nil {
			return nil, fmt.Errorf("agent execution canceled (parent context): %w (kill failed: %v)", timeoutCtx.Err(), err)
		}
		return nil, fmt.Errorf("agent execution canceled (parent context): %w", timeoutCtx.Err())
	case err := <-errCh:
		// Process completed
		// Check if it was killed by circuit breaker (lock-free atomic read)
		if a.loopDetected.Load() {
			// Read loopReason with mutex protection (it's a string)
			a.mu.Lock()
			loopReason := a.loopReason
			a.mu.Unlock()
			return nil, fmt.Errorf("agent killed by circuit breaker: %s", loopReason)
		}

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
	if a.cmd != nil && a.cmd.Process != nil {
		return a.cmd.Process.Kill()
	}
	return nil
}

// captureOutput reads stdout/stderr and stores in result
// If event parsing is enabled, it also parses lines into structured events and stores them
// vc-4asf: Uses batched output collection to reduce mutex contention
func (a *Agent) captureOutput() {
	var wg sync.WaitGroup
	wg.Add(2)

	// Capture stdout with batching
	go func() {
		defer wg.Done()
		scanner := bufio.NewScanner(a.stdout)
		const batchSize = 10 // Batch up to 10 lines before acquiring mutex

		batch := make([]string, 0, batchSize)

		flushBatch := func() {
			if len(batch) == 0 {
				return
			}

			a.mu.Lock()
			for _, line := range batch {
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
			}
			a.mu.Unlock()

			// Parse lines for events if parser is enabled (outside mutex)
			if a.parser != nil && a.config.Store != nil {
				for _, line := range batch {
					a.parseAndStoreEvents(line)
				}
			}

			batch = batch[:0] // Clear batch
		}

		for scanner.Scan() {
			line := scanner.Text()
			batch = append(batch, line)

			// Flush when batch is full
			if len(batch) >= batchSize {
				flushBatch()
			}
		}

		// Flush remaining lines
		flushBatch()
	}()

	// Capture stderr with batching
	go func() {
		defer wg.Done()
		scanner := bufio.NewScanner(a.stderr)
		const batchSize = 10 // Batch up to 10 lines before acquiring mutex

		batch := make([]string, 0, batchSize)

		flushBatch := func() {
			if len(batch) == 0 {
				return
			}

			a.mu.Lock()
			for _, line := range batch {
				// Only append if we haven't reached the limit
				if len(a.result.Errors) < maxOutputLines {
					a.result.Errors = append(a.result.Errors, line)
					// Print inside mutex to ensure ordering matches captured output
					fmt.Fprintln(os.Stderr, line)
				} else if len(a.result.Errors) == maxOutputLines {
					// Add truncation marker once
					a.result.Errors = append(a.result.Errors, "[... error output truncated: limit reached ...]")
				}
			}
			a.mu.Unlock()

			// Parse stderr for events too (errors, warnings, etc.) (outside mutex)
			if a.parser != nil && a.config.Store != nil {
				for _, line := range batch {
					a.parseAndStoreEvents(line)
				}
			}

			batch = batch[:0] // Clear batch
		}

		for scanner.Scan() {
			line := scanner.Text()
			batch = append(batch, line)

			// Flush when batch is full
			if len(batch) >= batchSize {
				flushBatch()
			}
		}

		// Flush remaining lines
		flushBatch()
	}()

	wg.Wait()
}

// parseAndStoreEvents parses a line for events and stores them immediately
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
		// Record event with watchdog monitor for anomaly detection (vc-118)
		// Do this synchronously before async storage to ensure monitor sees events in order
		if a.config.Monitor != nil {
			a.config.Monitor.RecordEvent(string(event.Type))
		}

		// Store event asynchronously to avoid blocking output capture
		go func(evt *events.AgentEvent) {
			if err := a.config.Store.StoreAgentEvent(a.ctx, evt); err != nil {
				// Log error but don't fail - event storage is best-effort
				fmt.Fprintf(os.Stderr, "warning: failed to store agent event: %v\n", err)
			}
		}(event)
	}
}

// convertJSONToEvent converts an Amp JSON message to AgentEvents (vc-107, vc-29, vc-30)
// This replaces regex-based parsing with structured event processing.
//
// Important: Amp --stream-json uses a NESTED structure where tool_use items are inside
// assistant messages at message.content[]. This function extracts all tool_use items
// from the nested content array and converts them to AgentEvents.
//
// Set VC_DEBUG_EVENTS=1 to enable debug logging of JSON event parsing.
func (a *Agent) convertJSONToEvent(msg AgentMessage, rawLine string) *events.AgentEvent {
	// Only process "assistant" messages - these contain tool use in nested content array
	if msg.Type != "assistant" {
		// Debug log non-assistant events if debugging is enabled
		if os.Getenv("VC_DEBUG_EVENTS") != "" {
			fmt.Fprintf(os.Stderr, "[DEBUG] Skipping non-assistant event: type=%s subtype=%s\n", msg.Type, msg.Subtype)
		}
		return nil
	}

	// Check if message wrapper exists
	if msg.Message == nil {
		if os.Getenv("VC_DEBUG_EVENTS") != "" {
			fmt.Fprintf(os.Stderr, "[DEBUG] Assistant message has no nested message field\n")
		}
		return nil
	}

	// Extract tool use items from the content array
	// Note: We only process the FIRST tool_use in the array for now, to maintain
	// single-event-per-call semantics. In practice, agents typically emit one tool at a time.
	for _, content := range msg.Message.Content {
		if content.Type != "tool_use" {
			continue // Skip text content
		}

		// Found a tool_use - extract it
		toolName := normalizeToolName(content.Name)

		// Skip internal tools that aren't code operations (vc-107)
		if shouldSkipTool(toolName) {
			if os.Getenv("VC_DEBUG_EVENTS") != "" {
				fmt.Fprintf(os.Stderr, "[DEBUG] Skipping internal tool: name=%s\n", content.Name)
			}
			continue
		}

		// Circuit breaker: Track Read tool usage to detect infinite loops (vc-117)
		if toolName == "read" {
			// Extract file path from input
			var filePath string
			if content.Input != nil {
				if pathVal, ok := content.Input["path"].(string); ok {
					filePath = pathVal
				}
			}

			// Check for infinite loop condition
			// Note: Do NOT kill here while holding mutex - just set flag
			if err := a.checkCircuitBreaker(filePath); err != nil {
				// Circuit breaker triggered - log it but don't kill yet
				fmt.Fprintf(os.Stderr, "\n!!! CIRCUIT BREAKER TRIGGERED !!!\n%v\n", err)
				// Don't return an event - the agent will be terminated by Wait()
				return nil
			}
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

		// Extract tool usage data from Amp's actual JSON format (vc-107)
		toolData := events.AgentToolUseData{
			ToolName: toolName,
		}

		// Extract parameters from the input map
		var targetFile, command, pattern string
		if content.Input != nil {
			// Path field (used by Read, edit_file, Grep, etc.)
			if pathVal, ok := content.Input["path"].(string); ok {
				targetFile = pathVal
			}
			// Cmd field (used by Bash)
			if cmdVal, ok := content.Input["cmd"].(string); ok {
				command = cmdVal
			}
			// Pattern field (used by Grep, Glob)
			if patternVal, ok := content.Input["pattern"].(string); ok {
				pattern = patternVal
			}
		}

		toolData.TargetFile = targetFile
		toolData.Command = command

		// Build a human-readable description from the structured data
		if targetFile != "" {
			toolData.ToolDescription = fmt.Sprintf("%s %s", toolName, targetFile)
		} else if command != "" {
			toolData.ToolDescription = fmt.Sprintf("run: %s", command)
		} else if pattern != "" {
			toolData.ToolDescription = fmt.Sprintf("search: %s", pattern)
		} else {
			toolData.ToolDescription = toolName
		}

		// Set the data
		if err := event.SetAgentToolUseData(toolData); err != nil {
			fmt.Fprintf(os.Stderr, "warning: failed to set tool use data: %v\n", err)
			continue
		}

		// Debug log successful event conversion
		if os.Getenv("VC_DEBUG_EVENTS") != "" {
			fmt.Fprintf(os.Stderr, "[DEBUG] Parsed tool_use event: tool=%s file=%s command=%s pattern=%s\n",
				toolName, targetFile, command, pattern)
		}

		// Record event with watchdog monitor for anomaly detection (vc-118)
		if a.config.Monitor != nil {
			a.config.Monitor.RecordEvent(string(events.EventTypeAgentToolUse))
		}

		// Return the first tool_use event found
		return event
	}

	// No tool_use found in content array
	if os.Getenv("VC_DEBUG_EVENTS") != "" {
		fmt.Fprintf(os.Stderr, "[DEBUG] Assistant message has no tool_use in content array\n")
	}
	return nil
}

// normalizeToolName converts Amp's tool names to canonical lowercase names
// Examples: "Read" -> "read", "edit_file" -> "edit", "Bash" -> "bash", "create_file" -> "write"
func normalizeToolName(ampToolName string) string {
	// Convert to lowercase
	name := strings.ToLower(ampToolName)

	// Map Amp-specific names to canonical names
	switch name {
	case "edit_file":
		return "edit"
	case "create_file", "write_file":
		return "write"
	default:
		return name
	}
}

// shouldSkipTool returns true if this tool is internal and shouldn't generate events
func shouldSkipTool(toolName string) bool {
	// Skip internal agent tools that don't represent actual code work
	internalTools := map[string]bool{
		"todo_write":          true, // Claude Code's internal TODO tracking
		"mcp__beads__show":    true, // MCP beads integration
		"mcp__beads__list":    true,
		"mcp__beads__create":  true,
		"mcp__beads__update":  true,
		"mcp__beads__close":   true,
	}

	return internalTools[toolName]
}

// buildClaudeCodeCommand constructs the Claude Code CLI command
func buildClaudeCodeCommand(_ AgentConfig, prompt string) *exec.Cmd {
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

// checkCircuitBreaker checks if the agent is stuck in an infinite loop (vc-117)
func (a *Agent) checkCircuitBreaker(filePath string) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	// Initialize map if needed (for tests that create agents without using SpawnAgent)
	if a.fileReadCounts == nil {
		a.fileReadCounts = make(map[string]int)
	}

	// Track per-file read count and check limit BEFORE incrementing
	// This ensures we stop at exactly the limit, not limit+1
	if filePath != "" {
		if a.fileReadCounts[filePath] >= maxSameFileReads {
			a.loopReason = fmt.Sprintf("Read file %s %d times (limit: %d)", filePath, maxSameFileReads, maxSameFileReads)
			a.loopDetected.Store(true) // Atomic write for lock-free monitoring
			return fmt.Errorf("infinite loop detected: %s", a.loopReason)
		}
		a.fileReadCounts[filePath]++
	}

	// Increment total read count and check limit
	a.totalReadCount++
	if a.totalReadCount > maxFileReads {
		a.loopReason = fmt.Sprintf("Total Read operations: %d (limit: %d)", a.totalReadCount, maxFileReads)
		a.loopDetected.Store(true) // Atomic write for lock-free monitoring
		return fmt.Errorf("infinite loop detected: %s", a.loopReason)
	}

	return nil
}
