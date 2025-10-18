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

// AgentMessage represents a JSON message from the agent
type AgentMessage struct {
	Type    string                 `json:"type"`
	Content string                 `json:"content,omitempty"`
	Data    map[string]interface{} `json:"data,omitempty"`
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
		// Timeout - kill the process
		a.Kill()
		return nil, fmt.Errorf("agent execution timed out after %v", a.config.Timeout)
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
func (a *Agent) parseAndStoreEvents(line string) {
	// Parse the line into events
	extractedEvents := a.parser.ParseLine(line)

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

// buildClaudeCodeCommand constructs the Claude Code CLI command
func buildClaudeCodeCommand(cfg AgentConfig, prompt string) *exec.Cmd {
	args := []string{}

	// When running in a sandbox, bypass permission checks for autonomous operation (vc-114)
	// This is safe because sandboxes are isolated environments
	if cfg.Sandbox != nil {
		args = append(args, "--dangerously-skip-permissions")
	}

	// Claude Code uses the message directly
	args = append(args, prompt)

	return exec.Command("claude", args...)
}

// buildAmpCommand constructs the Sourcegraph amp CLI command
func buildAmpCommand(cfg AgentConfig, prompt string) *exec.Cmd {
	args := []string{}

	// When running in a sandbox, bypass permission checks for autonomous operation (vc-117)
	// This is safe because sandboxes are isolated environments
	if cfg.Sandbox != nil {
		args = append(args, "--dangerously-allow-all")
	}

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
