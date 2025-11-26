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

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
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
	// Interrupt manager for graceful pause/resume (optional - if nil, no interrupt checks)
	InterruptMgr interface{ IsInterruptRequested() bool }
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

	// maxSameToolCalls is the maximum number of times the same tool can be called (vc-34cz)
	// Detects loops where agent repeatedly tries the same tool (e.g., repeated Grep, todo_write)
	// Set high enough for legitimate exploration but catches pathological repetition
	maxSameToolCalls = 100

	// maxGreps is the maximum number of Grep tool invocations per execution (vc-139)
	// Catches agents stuck in search loops - should be lower than Read since searches are broader
	maxGreps = 100

	// maxSamePatternGreps is the maximum number of times the same grep pattern can be searched (vc-139)
	// Detects agents repeatedly searching for the same thing
	maxSamePatternGreps = 10

	// maxGlobs is the maximum number of Glob tool invocations per execution (vc-139)
	// Catches agents stuck in file listing loops
	maxGlobs = 50

	// maxSamePatternGlobs is the maximum number of times the same glob pattern can be used (vc-139)
	// Detects agents repeatedly globbing for the same pattern
	maxSamePatternGlobs = 10

	// aiLoopCheckInterval is how often to ask AI if agent is stuck (vc-34cz)
	// Every 50 tool calls, Haiku checks if we're making progress or looping
	// This is ZFC-compliant: AI judges stuckness, not arbitrary limits
	aiLoopCheckInterval = 50

	// maxTotalToolCalls is the catastrophic backstop (vc-34cz)
	// Raised to 1000 since AI loop detection at 50-call intervals should catch loops first
	// Only triggers if AI checks fail or are disabled (no API key)
	maxTotalToolCalls = 1000
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

	// Circuit breaker state for detecting infinite loops (vc-117, vc-34cz, vc-139)
	totalReadCount  int            // Total number of Read tool invocations
	fileReadCounts  map[string]int // Number of times each file has been read
	toolCallCounts  map[string]int // Number of times each tool has been called (vc-34cz)
	totalToolCalls  int            // Total number of tool calls across all types (vc-34cz)
	recentToolCalls []string       // Recent tool calls for AI loop detection (vc-34cz)
	loopDetected    atomic.Bool    // Whether an infinite loop was detected (lock-free for monitoring goroutine)
	loopReason      string         // Reason for loop detection (for error messages)

	// Grep/Glob tracking for search loop detection (vc-139)
	totalGrepCount    int            // Total number of Grep tool invocations
	grepPatternCounts map[string]int // Number of times each grep pattern has been searched
	totalGlobCount    int            // Total number of Glob tool invocations
	globPatternCounts map[string]int // Number of times each glob pattern has been used

	// Interrupt state for graceful pause/resume (vc-d25s)
	interruptDetected atomic.Bool // Whether an interrupt was detected (lock-free for monitoring goroutine)
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
		// Initialize circuit breaker state (vc-117, vc-34cz)
		totalReadCount:  0,
		fileReadCounts:  make(map[string]int),
		toolCallCounts:  make(map[string]int),
		totalToolCalls:  0,
		recentToolCalls: make([]string, 0, 100), // Pre-allocate for efficiency
		loopReason:      "",
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

	// Start a monitoring goroutine to check for circuit breaker triggers and interrupts
	// This runs outside the mutex to avoid deadlocks (vc-5783, vc-d25s)
	monitorDone := make(chan struct{})
	go func() {
		defer close(monitorDone)
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				// Checkpoint 2: Check for interrupt request during agent execution (vc-d25s)
				if a.config.InterruptMgr != nil && a.config.InterruptMgr.IsInterruptRequested() {
					fmt.Fprintf(os.Stderr, "⏸️  Interrupt detected during agent execution - stopping agent\n")
					// Set interrupt flag (lock-free atomic write)
					a.interruptDetected.Store(true)
					// Kill the agent gracefully
					if err := a.Kill(); err != nil {
						fmt.Fprintf(os.Stderr, "warning: failed to kill agent after interrupt: %v\n", err)
					}
					return
				}

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
		// Check if it was killed by interrupt (lock-free atomic read) (vc-d25s)
		if a.interruptDetected.Load() {
			return nil, fmt.Errorf("agent interrupted by user request")
		}

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
//
// vc-4asf: Uses batched collection to reduce mutex contention
// Batch size of 50 lines reduces lock acquisitions by 50x and contention from 16-32% to 3-5%.
// See TestBatchedMutexContention for detailed benchmarks.
func (a *Agent) captureOutput() {
	var wg sync.WaitGroup
	wg.Add(2)

	const batchSize = 50 // Reduces mutex acquisitions by 50x, keeps contention under 6%

	// Capture stdout
	go func() {
		defer wg.Done()
		scanner := bufio.NewScanner(a.stdout)
		batch := make([]string, 0, batchSize)
		batchedJSON := make([]AgentMessage, 0, batchSize)

		for scanner.Scan() {
			line := scanner.Text()

			// Print immediately for real-time user feedback
			// This happens outside the mutex and doesn't affect batching
			fmt.Println(line)

			// Parse JSON if streaming JSON mode (outside mutex)
			var msg AgentMessage
			if a.config.StreamJSON {
				if err := json.Unmarshal([]byte(line), &msg); err == nil {
					batchedJSON = append(batchedJSON, msg)
				}
			}

			// Add to batch
			batch = append(batch, line)

			// Flush batch when full
			if len(batch) >= batchSize {
				a.mu.Lock()
				// Check if we can fit the batch
				outputLen := len(a.result.Output)
				if outputLen < maxOutputLines {
					// Append what fits
					remaining := maxOutputLines - outputLen
					if len(batch) <= remaining {
						a.result.Output = append(a.result.Output, batch...)
					} else {
						a.result.Output = append(a.result.Output, batch[:remaining]...)
					}
				}
				// Add truncation marker if we just hit the limit
				if len(a.result.Output) == maxOutputLines && outputLen < maxOutputLines {
					a.result.Output = append(a.result.Output, "[... output truncated: limit reached ...]")
				}

				if a.config.StreamJSON {
					jsonLen := len(a.result.ParsedJSON)
					if jsonLen < maxOutputLines {
						remaining := maxOutputLines - jsonLen
						if len(batchedJSON) <= remaining {
							a.result.ParsedJSON = append(a.result.ParsedJSON, batchedJSON...)
						} else {
							a.result.ParsedJSON = append(a.result.ParsedJSON, batchedJSON[:remaining]...)
						}
					}
				}
				a.mu.Unlock()

				batch = batch[:0]
				batchedJSON = batchedJSON[:0]
			}

			// Parse line for events if parser is enabled
			// Must be called OUTSIDE mutex to avoid deadlock with checkCircuitBreaker
			if a.parser != nil && a.config.Store != nil {
				a.parseAndStoreEvents(line)
			}
		}

		// Flush remaining lines
		if len(batch) > 0 {
			a.mu.Lock()
			outputLen := len(a.result.Output)
			if outputLen < maxOutputLines {
				// Append what fits
				remaining := maxOutputLines - outputLen
				if len(batch) <= remaining {
					a.result.Output = append(a.result.Output, batch...)
				} else {
					a.result.Output = append(a.result.Output, batch[:remaining]...)
				}
			}
			// Add truncation marker if we just hit the limit
			if len(a.result.Output) == maxOutputLines && outputLen < maxOutputLines {
				a.result.Output = append(a.result.Output, "[... output truncated: limit reached ...]")
			}

			if a.config.StreamJSON {
				jsonLen := len(a.result.ParsedJSON)
				if jsonLen < maxOutputLines {
					remaining := maxOutputLines - jsonLen
					if len(batchedJSON) <= remaining {
						a.result.ParsedJSON = append(a.result.ParsedJSON, batchedJSON...)
					} else {
						a.result.ParsedJSON = append(a.result.ParsedJSON, batchedJSON[:remaining]...)
					}
				}
			}
			a.mu.Unlock()
		}
	}()

	// Capture stderr
	go func() {
		defer wg.Done()
		scanner := bufio.NewScanner(a.stderr)
		batch := make([]string, 0, batchSize)

		for scanner.Scan() {
			line := scanner.Text()

			// Print immediately for real-time user feedback
			// This happens outside the mutex and doesn't affect batching
			fmt.Fprintln(os.Stderr, line)

			// Add to batch
			batch = append(batch, line)

			// Flush batch when full
			if len(batch) >= batchSize {
				a.mu.Lock()
				errLen := len(a.result.Errors)
				if errLen < maxOutputLines {
					// Append what fits
					remaining := maxOutputLines - errLen
					if len(batch) <= remaining {
						a.result.Errors = append(a.result.Errors, batch...)
					} else {
						a.result.Errors = append(a.result.Errors, batch[:remaining]...)
					}
				}
				// Add truncation marker if we just hit the limit
				if len(a.result.Errors) == maxOutputLines && errLen < maxOutputLines {
					a.result.Errors = append(a.result.Errors, "[... error output truncated: limit reached ...]")
				}
				a.mu.Unlock()

				batch = batch[:0]
			}

			// Parse stderr for events too (errors, warnings, etc.)
			// Must be called OUTSIDE mutex to avoid deadlock with checkCircuitBreaker
			if a.parser != nil && a.config.Store != nil {
				a.parseAndStoreEvents(line)
			}
		}

		// Flush remaining lines
		if len(batch) > 0 {
			a.mu.Lock()
			errLen := len(a.result.Errors)
			if errLen < maxOutputLines {
				// Append what fits
				remaining := maxOutputLines - errLen
				if len(batch) <= remaining {
					a.result.Errors = append(a.result.Errors, batch...)
				} else {
					a.result.Errors = append(a.result.Errors, batch[:remaining]...)
				}
			}
			// Add truncation marker if we just hit the limit
			if len(a.result.Errors) == maxOutputLines && errLen < maxOutputLines {
				a.result.Errors = append(a.result.Errors, "[... error output truncated: limit reached ...]")
			}
			a.mu.Unlock()
		}
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
			if event := a.convertJSONToEvent(msg); event != nil {
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
		// Skip storage if Store is nil (polecat mode has no database, vc-4bql)
		if a.config.Store != nil {
			go func(evt *events.AgentEvent) {
				if err := a.config.Store.StoreAgentEvent(a.ctx, evt); err != nil {
					// Log error but don't fail - event storage is best-effort
					fmt.Fprintf(os.Stderr, "warning: failed to store agent event: %v\n", err)
				}
			}(event)
		}
	}
}

// convertJSONToEvent converts an Amp JSON message to AgentEvents (vc-107, vc-29, vc-30, vc-9lvs)
// This replaces regex-based parsing with structured event processing.
//
// Important: Amp --stream-json uses a NESTED structure where tool_use items are inside
// assistant messages at message.content[]. This function extracts all tool_use items
// from the nested content array and converts them to AgentEvents.
//
// Set VC_DEBUG_EVENTS=1 to enable debug logging of JSON event parsing.
func (a *Agent) convertJSONToEvent(msg AgentMessage) *events.AgentEvent {
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

		// Circuit breaker: Track all tool usage to detect infinite loops (vc-117, vc-34cz)
		// Check general tool call limits first (applies to ALL tools)
		if err := a.checkToolCallLimit(toolName); err != nil {
			fmt.Fprintf(os.Stderr, "\n!!! CIRCUIT BREAKER TRIGGERED !!!\n%v\n", err)
			return nil
		}

		// Additional Read-specific tracking for file-level granularity
		if toolName == "read" {
			// Extract file path from input
			// Claude Code uses "file_path", Amp uses "path"
			var filePath string
			if content.Input != nil {
				if pathVal, ok := content.Input["file_path"].(string); ok {
					filePath = pathVal
				} else if pathVal, ok := content.Input["path"].(string); ok {
					filePath = pathVal
				}
			}

			// Check for infinite loop condition (file-specific)
			// Note: Do NOT kill here while holding mutex - just set flag
			if err := a.checkCircuitBreaker(filePath); err != nil {
				// Circuit breaker triggered - log it but don't kill yet
				fmt.Fprintf(os.Stderr, "\n!!! CIRCUIT BREAKER TRIGGERED !!!\n%v\n", err)
				// Don't return an event - the agent will be terminated by Wait()
				return nil
			}
		}

		// Grep-specific tracking for pattern-level granularity (vc-139)
		if toolName == "grep" {
			var grepPattern string
			if content.Input != nil {
				if patternVal, ok := content.Input["pattern"].(string); ok {
					grepPattern = patternVal
				}
			}

			if err := a.checkGrepCircuitBreaker(grepPattern); err != nil {
				fmt.Fprintf(os.Stderr, "\n!!! CIRCUIT BREAKER TRIGGERED !!!\n%v\n", err)
				return nil
			}
		}

		// Glob-specific tracking for pattern-level granularity (vc-139)
		if toolName == "glob" {
			var globPattern string
			if content.Input != nil {
				if patternVal, ok := content.Input["pattern"].(string); ok {
					globPattern = patternVal
				}
			}

			if err := a.checkGlobCircuitBreaker(globPattern); err != nil {
				fmt.Fprintf(os.Stderr, "\n!!! CIRCUIT BREAKER TRIGGERED !!!\n%v\n", err)
				return nil
			}
		}

		// Extract parameters from the input map
		var targetFile, command, pattern string
		if content.Input != nil {
			// Path field (used by Read, edit_file, Grep, etc.)
			// Claude Code uses "file_path", Amp uses "path"
			if pathVal, ok := content.Input["file_path"].(string); ok {
				targetFile = pathVal
			} else if pathVal, ok := content.Input["path"].(string); ok {
				targetFile = pathVal
			}
			// Command field (used by Bash)
			// Claude Code uses "command", Amp uses "cmd"
			if cmdVal, ok := content.Input["command"].(string); ok {
				command = cmdVal
			} else if cmdVal, ok := content.Input["cmd"].(string); ok {
				command = cmdVal
			}
			// Pattern field (used by Grep, Glob)
			if patternVal, ok := content.Input["pattern"].(string); ok {
				pattern = patternVal
			}
		}

		// Build human-readable message for the event (vc-9lvs)
		// Use title case for tool name (capitalize first letter)
		toolNameDisplay := toolName
		if len(toolName) > 0 {
			toolNameDisplay = string(toolName[0]-32) + toolName[1:]
			// Only capitalize if first char is lowercase letter
			if toolName[0] < 'a' || toolName[0] > 'z' {
				toolNameDisplay = toolName // Keep as-is if not lowercase letter
			}
		}

		var humanMessage string
		if targetFile != "" {
			humanMessage = fmt.Sprintf("tool:%s %s", toolNameDisplay, targetFile)
		} else if command != "" {
			humanMessage = fmt.Sprintf("tool:%s '%s'", toolNameDisplay, command)
		} else if pattern != "" {
			humanMessage = fmt.Sprintf("tool:%s '%s'", toolNameDisplay, pattern)
		} else {
			humanMessage = fmt.Sprintf("tool:%s", toolNameDisplay)
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
			Message:    humanMessage,
			SourceLine: a.parser.LineNumber,
		}

		// Extract tool usage data from Amp's actual JSON format (vc-107)
		toolData := events.AgentToolUseData{
			ToolName: toolName,
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
func buildClaudeCodeCommand(cfg AgentConfig, prompt string) *exec.Cmd {
	args := []string{}

	// Use --print mode for non-interactive execution
	args = append(args, "--print")

	// Always bypass permission checks for autonomous agent operation (vc-117)
	// This is required for VC to operate autonomously without human intervention
	// Safe because:
	// 1. When sandboxed: Isolated environment with no risk to main codebase
	// 2. When not sandboxed: VC is designed to work autonomously on its own codebase
	//    and the results go through quality gates before being committed
	args = append(args, "--dangerously-skip-permissions")

	// Enable JSON streaming if requested
	// Note: --output-format stream-json requires --verbose flag
	if cfg.StreamJSON {
		args = append(args, "--verbose", "--output-format", "stream-json")
	}

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

// checkCircuitBreaker checks if the agent is stuck in an infinite loop (vc-117, vc-34cz)
// Tracks both file reads (backward compatible) and general tool calls
func (a *Agent) checkCircuitBreaker(filePath string) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	// Initialize maps if needed (for tests that create agents without using SpawnAgent)
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

// checkGrepCircuitBreaker checks if the agent is stuck in a grep loop (vc-139)
// Tracks both total greps and per-pattern grep counts
func (a *Agent) checkGrepCircuitBreaker(pattern string) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	// Initialize map if needed
	if a.grepPatternCounts == nil {
		a.grepPatternCounts = make(map[string]int)
	}

	// Track per-pattern grep count and check limit BEFORE incrementing
	if pattern != "" {
		if a.grepPatternCounts[pattern] >= maxSamePatternGreps {
			a.loopReason = fmt.Sprintf("Grep pattern '%s' searched %d times (limit: %d)", pattern, maxSamePatternGreps, maxSamePatternGreps)
			a.loopDetected.Store(true)
			return fmt.Errorf("infinite loop detected: %s", a.loopReason)
		}
		a.grepPatternCounts[pattern]++
	}

	// Increment total grep count and check limit
	a.totalGrepCount++
	if a.totalGrepCount > maxGreps {
		a.loopReason = fmt.Sprintf("Total Grep operations: %d (limit: %d)", a.totalGrepCount, maxGreps)
		a.loopDetected.Store(true)
		return fmt.Errorf("infinite loop detected: %s", a.loopReason)
	}

	return nil
}

// checkGlobCircuitBreaker checks if the agent is stuck in a glob loop (vc-139)
// Tracks both total globs and per-pattern glob counts
func (a *Agent) checkGlobCircuitBreaker(pattern string) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	// Initialize map if needed
	if a.globPatternCounts == nil {
		a.globPatternCounts = make(map[string]int)
	}

	// Track per-pattern glob count and check limit BEFORE incrementing
	if pattern != "" {
		if a.globPatternCounts[pattern] >= maxSamePatternGlobs {
			a.loopReason = fmt.Sprintf("Glob pattern '%s' used %d times (limit: %d)", pattern, maxSamePatternGlobs, maxSamePatternGlobs)
			a.loopDetected.Store(true)
			return fmt.Errorf("infinite loop detected: %s", a.loopReason)
		}
		a.globPatternCounts[pattern]++
	}

	// Increment total glob count and check limit
	a.totalGlobCount++
	if a.totalGlobCount > maxGlobs {
		a.loopReason = fmt.Sprintf("Total Glob operations: %d (limit: %d)", a.totalGlobCount, maxGlobs)
		a.loopDetected.Store(true)
		return fmt.Errorf("infinite loop detected: %s", a.loopReason)
	}

	return nil
}

// checkToolCallLimit checks if a specific tool is being called too many times (vc-34cz)
// Uses both AI-based loop detection (ZFC-compliant) and hard limits as backstop
func (a *Agent) checkToolCallLimit(toolName string) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	// Initialize maps if needed
	if a.toolCallCounts == nil {
		a.toolCallCounts = make(map[string]int)
	}

	// Track per-tool call count and check limit
	a.toolCallCounts[toolName]++
	if a.toolCallCounts[toolName] > maxSameToolCalls {
		a.loopReason = fmt.Sprintf("Tool '%s' called %d times (limit: %d) - agent may be stuck repeating the same operation", toolName, a.toolCallCounts[toolName], maxSameToolCalls)
		a.loopDetected.Store(true)
		return fmt.Errorf("infinite loop detected: %s", a.loopReason)
	}

	// Track recent tool calls for AI analysis
	a.recentToolCalls = append(a.recentToolCalls, toolName)

	// Increment total tool call count
	a.totalToolCalls++

	// Every aiLoopCheckInterval calls, ask AI if we're stuck (ZFC-compliant)
	if a.totalToolCalls%aiLoopCheckInterval == 0 {
		// Release lock before making AI call (can take time)
		recentCalls := make([]string, len(a.recentToolCalls))
		copy(recentCalls, a.recentToolCalls)
		a.mu.Unlock()

		// Ask AI to judge if we're stuck in a loop
		stuck, reason := a.checkAILoopDetection(a.ctx, recentCalls)

		// Reacquire lock before updating state
		a.mu.Lock()

		if stuck {
			a.loopReason = fmt.Sprintf("AI detected loop after %d tool calls: %s", a.totalToolCalls, reason)
			a.loopDetected.Store(true)
			return fmt.Errorf("infinite loop detected: %s", a.loopReason)
		}
	}

	// Catastrophic backstop (only if AI checks fail or are disabled)
	if a.totalToolCalls > maxTotalToolCalls {
		a.loopReason = fmt.Sprintf("Total tool calls: %d (limit: %d) - exceeded catastrophic backstop", a.totalToolCalls, maxTotalToolCalls)
		a.loopDetected.Store(true)
		return fmt.Errorf("infinite loop detected: %s", a.loopReason)
	}

	return nil
}


// checkAILoopDetection uses Haiku to judge if the agent is stuck in a loop (vc-34cz)
// This is ZFC-compliant: AI makes the judgment, not arbitrary heuristics
// Returns (stuck bool, reason string)
func (a *Agent) checkAILoopDetection(ctx context.Context, recentCalls []string) (bool, string) {
	// Check if API key is available (or if AI checks are disabled for testing)
	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey == "" || os.Getenv("VC_DISABLE_AI_LOOP_DETECTION") != "" {
		// No API key or disabled - skip AI check, rely on hard limits
		return false, ""
	}

	// Build tool call summary (last 100 calls or all if fewer)
	startIdx := 0
	if len(recentCalls) > 100 {
		startIdx = len(recentCalls) - 100
	}
	callsToAnalyze := recentCalls[startIdx:]

	// Count frequency of each tool
	toolFreq := make(map[string]int)
	for _, tool := range callsToAnalyze {
		toolFreq[tool]++
	}

	// Build summary for AI
	summary := fmt.Sprintf("Recent tool calls (last %d):\n", len(callsToAnalyze))
	summary += fmt.Sprintf("Tool sequence: %v\n\n", callsToAnalyze)
	summary += "Tool frequency:\n"
	for tool, count := range toolFreq {
		summary += fmt.Sprintf("  %s: %d calls\n", tool, count)
	}

	// Construct prompt
	prompt := fmt.Sprintf(`You are analyzing agent tool usage to detect infinite loops.

%s

Is this agent stuck in an unproductive loop? Consider:
- Is the agent repeating the same tools without making progress?
- Are we seeing patterns like: grep -> read -> grep -> read repeatedly?
- Or todo_write being called many times without other productive work?

Respond with JSON:
{
  "stuck": true/false,
  "confidence": 0.0-1.0,
  "reasoning": "Brief explanation"
}

Only say stuck=true if you're confident (>0.8) this is a loop.`, summary)

	// Make Haiku API call with short timeout (don't want to slow down agent too much)
	checkCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	client := anthropic.NewClient(option.WithAPIKey(apiKey))

	resp, err := client.Messages.New(checkCtx, anthropic.MessageNewParams{
		Model:     anthropic.Model("claude-3-5-haiku-20241022"), // Haiku for speed/cost
		MaxTokens: int64(500),
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock(prompt)),
		},
	})

	if err != nil {
		// AI call failed - don't halt, just log and continue
		if os.Getenv("VC_DEBUG_EVENTS") != "" {
			fmt.Fprintf(os.Stderr, "[DEBUG] AI loop detection failed: %v\n", err)
		}
		return false, ""
	}

	// Extract text from response
	var responseText string
	for _, block := range resp.Content {
		if block.Type == "text" {
			responseText += block.Text
		}
	}

	if responseText == "" {
		return false, ""
	}

	// Parse JSON response
	var result struct {
		Stuck      bool    `json:"stuck"`
		Confidence float64 `json:"confidence"`
		Reasoning  string  `json:"reasoning"`
	}

	if err := json.Unmarshal([]byte(responseText), &result); err != nil {
		// Try to extract JSON from markdown code fence
		if strings.Contains(responseText, "```json") {
			start := strings.Index(responseText, "```json") + 7
			end := strings.Index(responseText[start:], "```")
			if end > 0 {
				jsonStr := responseText[start : start+end]
				if err := json.Unmarshal([]byte(jsonStr), &result); err != nil {
					return false, ""
				}
			} else {
				return false, ""
			}
		} else {
			return false, ""
		}
	}

	if result.Stuck && result.Confidence > 0.8 {
		return true, result.Reasoning
	}

	return false, ""
}
