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

	"github.com/steveyegge/vc/internal/types"
)

// AgentType represents the type of coding agent to spawn
type AgentType string

const (
	AgentTypeCody        AgentType = "cody"
	AgentTypeClaudeCode  AgentType = "claude-code"
)

// AgentConfig holds configuration for spawning an agent
type AgentConfig struct {
	Type        AgentType
	WorkingDir  string
	Issue       *types.Issue
	StreamJSON  bool
	Timeout     time.Duration
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

	mu     sync.Mutex
	result AgentResult
}

// SpawnAgent starts a coding agent process
func SpawnAgent(ctx context.Context, cfg AgentConfig) (*Agent, error) {
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

	// Build the command based on agent type
	var cmd *exec.Cmd
	switch cfg.Type {
	case AgentTypeCody:
		cmd = buildCodyCommand(cfg)
	case AgentTypeClaudeCode:
		cmd = buildClaudeCodeCommand(cfg)
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
		result: AgentResult{
			Output: []string{},
			Errors: []string{},
		},
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
			a.mu.Unlock()

			// Also print to console for visibility
			fmt.Println(line)
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
			} else if len(a.result.Errors) == maxOutputLines {
				// Add truncation marker once
				a.result.Errors = append(a.result.Errors, "[... error output truncated: limit reached ...]")
			}
			a.mu.Unlock()

			// Print errors to stderr
			fmt.Fprintln(os.Stderr, line)
		}
	}()

	wg.Wait()
}

// buildCodyCommand constructs the Cody CLI command
func buildCodyCommand(cfg AgentConfig) *exec.Cmd {
	prompt := buildPrompt(cfg.Issue)

	args := []string{"chat", "--message", prompt}
	if cfg.StreamJSON {
		args = append(args, "--stream-json")
	}

	return exec.Command("cody", args...)
}

// buildClaudeCodeCommand constructs the Claude Code CLI command
func buildClaudeCodeCommand(cfg AgentConfig) *exec.Cmd {
	prompt := buildPrompt(cfg.Issue)

	// Claude Code uses the message directly
	args := []string{prompt}

	return exec.Command("claude", args...)
}

// buildPrompt creates the prompt to send to the coding agent
func buildPrompt(issue *types.Issue) string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("# Task: %s\n\n", issue.Title))

	if issue.Description != "" {
		sb.WriteString("## Description\n")
		sb.WriteString(issue.Description)
		sb.WriteString("\n\n")
	}

	if issue.Design != "" {
		sb.WriteString("## Design\n")
		sb.WriteString(issue.Design)
		sb.WriteString("\n\n")
	}

	if issue.AcceptanceCriteria != "" {
		sb.WriteString("## Acceptance Criteria\n")
		sb.WriteString(issue.AcceptanceCriteria)
		sb.WriteString("\n\n")
	}

	if issue.Notes != "" {
		sb.WriteString("## Notes\n")
		sb.WriteString(issue.Notes)
		sb.WriteString("\n\n")
	}

	sb.WriteString("Please implement this task following the acceptance criteria.\n")
	sb.WriteString("When complete, respond with a summary of what was done.\n")

	return sb.String()
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
