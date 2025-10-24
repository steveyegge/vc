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
// This matches Amp's --stream-json output format (vc-236, vc-107).
//
// Event Types:
// - "tool_use": Agent is invoking a tool (Read, Edit, Write, Bash, Glob, Grep, Task)
// - "system": System messages (init, shutdown, etc.)
// - "result": Final result/completion message
//
// Actual Amp JSON Format (from --stream-json):
//
//	{"type":"tool_use","id":"toolu_xxx","name":"Read","input":{"path":"/path/to/file"}}
//	{"type":"tool_use","id":"toolu_xxx","name":"edit_file","input":{"path":"/path/to/file","old_str":"...","new_str":"..."}}
//	{"type":"tool_use","id":"toolu_xxx","name":"Bash","input":{"cmd":"go test","cwd":"/path"}}
//	{"type":"tool_use","id":"toolu_xxx","name":"Grep","input":{"pattern":"TODO","path":"/path"}}
//
// Note: Tool names are capitalized (Read, Bash, Grep) or use underscores (edit_file, todo_write).
// The "input" field contains tool-specific parameters as a map.
//
// When StreamJSON=false, agent output is parsed via regex patterns in parser.go instead.
type AgentMessage struct {
	Type    string                 `json:"type"`              // Event type (e.g., "tool_use", "system", "result")
	Subtype string                 `json:"subtype,omitempty"` // Event subtype (e.g., "init", "error_during_execution")
	Content string                 `json:"content,omitempty"` // Human-readable message content
	Data    map[string]interface{} `json:"data,omitempty"`    // Additional event-specific data (legacy)

	// Tool usage fields (vc-107: actual Amp format)
	// Only populated when Type="tool_use"
	ID    string                 `json:"id,omitempty"`    // Tool invocation ID (e.g., "toolu_xxx")
	Name  string                 `json:"name,omitempty"`  // Tool name (e.g., "Read", "edit_file", "Bash")
	Input map[string]interface{} `json:"input,omitempty"` // Tool parameters (path, cmd, pattern, etc.)

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

	// Circuit breaker state for detecting infinite loops (vc-117)
	totalReadCount int            // Total number of Read tool invocations
	fileReadCounts map[string]int // Number of times each file has been read
	loopDetected   bool           // Whether an infinite loop was detected
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
		loopDetected:   false,
		loopReason:     "",
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

// convertJSONToEvent converts an Amp JSON message to an AgentEvent (vc-107)
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

	// Skip internal tools that aren't code operations (vc-107)
	// These are agent-internal tools that don't represent actual work
	toolName := normalizeToolName(msg.Name)
	if shouldSkipTool(toolName) {
		if os.Getenv("VC_DEBUG_EVENTS") != "" {
			fmt.Fprintf(os.Stderr, "[DEBUG] Skipping internal tool: name=%s\n", msg.Name)
		}
		return nil
	}

	// Circuit breaker: Track Read tool usage to detect infinite loops (vc-117)
	if toolName == "read" {
		// Extract file path from input
		var filePath string
		if msg.Input != nil {
			if pathVal, ok := msg.Input["path"].(string); ok {
				filePath = pathVal
			}
		}

		// Check for infinite loop condition
		if err := a.checkCircuitBreaker(filePath); err != nil {
			// Kill the agent immediately
			fmt.Fprintf(os.Stderr, "\n!!! CIRCUIT BREAKER TRIGGERED !!!\n%v\n", err)
			if killErr := a.Kill(); killErr != nil {
				fmt.Fprintf(os.Stderr, "warning: failed to kill agent after circuit breaker: %v\n", killErr)
			}
			// Don't return an event - the agent will be terminated
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
	if msg.Input != nil {
		// Path field (used by Read, edit_file, Grep, etc.)
		if pathVal, ok := msg.Input["path"].(string); ok {
			targetFile = pathVal
		}
		// Cmd field (used by Bash)
		if cmdVal, ok := msg.Input["cmd"].(string); ok {
			command = cmdVal
		}
		// Pattern field (used by Grep, Glob)
		if patternVal, ok := msg.Input["pattern"].(string); ok {
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
		return nil
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

	return event
}

// normalizeToolName converts Amp's tool names to canonical lowercase names
// Examples: "Read" -> "read", "edit_file" -> "edit", "Bash" -> "bash"
func normalizeToolName(ampToolName string) string {
	// Convert to lowercase
	name := strings.ToLower(ampToolName)

	// Map Amp-specific names to canonical names
	switch name {
	case "edit_file":
		return "edit"
	case "write_file":
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
// This must be called with the mutex held
func (a *Agent) checkCircuitBreaker(filePath string) error {
	// Initialize map if needed (for tests that create agents without using SpawnAgent)
	if a.fileReadCounts == nil {
		a.fileReadCounts = make(map[string]int)
	}

	// Increment total read count
	a.totalReadCount++

	// Track per-file read count
	if filePath != "" {
		a.fileReadCounts[filePath]++

		// Check if same file read too many times
		if a.fileReadCounts[filePath] > maxSameFileReads {
			a.loopDetected = true
			a.loopReason = fmt.Sprintf("Read file %s %d times (limit: %d)", filePath, a.fileReadCounts[filePath], maxSameFileReads)
			return fmt.Errorf("infinite loop detected: %s", a.loopReason)
		}
	}

	// Check if total reads exceeded
	if a.totalReadCount > maxFileReads {
		a.loopDetected = true
		a.loopReason = fmt.Sprintf("Total Read operations: %d (limit: %d)", a.totalReadCount, maxFileReads)
		return fmt.Errorf("infinite loop detected: %s", a.loopReason)
	}

	return nil
}
