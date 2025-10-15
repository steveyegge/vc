package executor

import (
	"context"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/steveyegge/vc/internal/ai"
	"github.com/steveyegge/vc/internal/storage"
	"github.com/steveyegge/vc/internal/types"
)

// Executor manages the issue processing event loop
type Executor struct {
	store      storage.Storage
	supervisor *ai.Supervisor
	instanceID string
	hostname   string
	pid        int
	version    string

	// Control channels
	stopCh chan struct{}
	doneCh chan struct{}

	// Configuration
	pollInterval        time.Duration
	heartbeatTicker     *time.Ticker
	enableAISupervision bool
	enableQualityGates  bool
	workingDir          string

	// State
	mu      sync.RWMutex
	running bool
}

// Config holds executor configuration
type Config struct {
	Store               storage.Storage
	Version             string
	PollInterval        time.Duration
	HeartbeatPeriod     time.Duration
	EnableAISupervision bool // Enable AI assessment and analysis (default: true)
	EnableQualityGates  bool // Enable quality gates enforcement (default: true)
	WorkingDir          string // Working directory for quality gates (default: ".")
}

// DefaultConfig returns default executor configuration
func DefaultConfig() *Config {
	return &Config{
		Version:             "0.1.0",
		PollInterval:        5 * time.Second,
		HeartbeatPeriod:     30 * time.Second,
		EnableAISupervision: true,
		EnableQualityGates:  true,
		WorkingDir:          ".",
	}
}

// New creates a new executor instance
func New(cfg *Config) (*Executor, error) {
	if cfg.Store == nil {
		return nil, fmt.Errorf("storage is required")
	}

	hostname, err := os.Hostname()
	if err != nil {
		return nil, fmt.Errorf("failed to get hostname: %w", err)
	}

	// Set default working directory if not specified
	workingDir := cfg.WorkingDir
	if workingDir == "" {
		workingDir = "."
	}

	e := &Executor{
		store:               cfg.Store,
		instanceID:          uuid.New().String(),
		hostname:            hostname,
		pid:                 os.Getpid(),
		version:             cfg.Version,
		pollInterval:        cfg.PollInterval,
		enableAISupervision: cfg.EnableAISupervision,
		enableQualityGates:  cfg.EnableQualityGates,
		workingDir:          workingDir,
		stopCh:              make(chan struct{}),
		doneCh:              make(chan struct{}),
	}

	// Initialize AI supervisor if enabled
	if cfg.EnableAISupervision {
		supervisor, err := ai.NewSupervisor(&ai.Config{
			Store: cfg.Store,
		})
		if err != nil {
			// Don't fail - just disable AI supervision
			fmt.Fprintf(os.Stderr, "Warning: failed to initialize AI supervisor: %v (continuing without AI supervision)\n", err)
			e.enableAISupervision = false
		} else {
			e.supervisor = supervisor
		}
	}

	return e, nil
}

// Start begins the executor event loop
func (e *Executor) Start(ctx context.Context) error {
	e.mu.Lock()
	if e.running {
		e.mu.Unlock()
		return fmt.Errorf("executor is already running")
	}
	e.running = true
	e.mu.Unlock()

	// Register this executor instance
	instance := &types.ExecutorInstance{
		InstanceID:    e.instanceID,
		Hostname:      e.hostname,
		PID:           e.pid,
		Status:        types.ExecutorStatusRunning,
		StartedAt:     time.Now(),
		LastHeartbeat: time.Now(),
		Version:       e.version,
		Metadata:      "{}",
	}

	if err := e.store.RegisterInstance(ctx, instance); err != nil {
		e.mu.Lock()
		e.running = false
		e.mu.Unlock()
		return fmt.Errorf("failed to register executor instance: %w", err)
	}

	// Start the event loop
	go e.eventLoop(ctx)

	return nil
}

// Stop gracefully stops the executor
func (e *Executor) Stop(ctx context.Context) error {
	e.mu.Lock()
	if !e.running {
		e.mu.Unlock()
		return fmt.Errorf("executor is not running")
	}
	e.mu.Unlock()

	// Signal shutdown
	close(e.stopCh)

	// Wait for event loop to finish
	select {
	case <-e.doneCh:
		// Event loop finished
	case <-ctx.Done():
		return ctx.Err()
	}

	// Mark instance as stopped
	// Get current instance to preserve all fields
	instances, err := e.store.GetActiveInstances(ctx)
	if err != nil {
		return fmt.Errorf("failed to get active instances: %w", err)
	}

	var instance *types.ExecutorInstance
	for _, inst := range instances {
		if inst.InstanceID == e.instanceID {
			instance = inst
			break
		}
	}

	if instance != nil {
		instance.Status = types.ExecutorStatusStopped
		if err := e.store.RegisterInstance(ctx, instance); err != nil {
			return fmt.Errorf("failed to mark instance as stopped: %w", err)
		}
	}

	e.mu.Lock()
	e.running = false
	e.mu.Unlock()

	return nil
}

// IsRunning returns whether the executor is currently running
func (e *Executor) IsRunning() bool {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.running
}

// eventLoop is the main event loop that processes issues
func (e *Executor) eventLoop(ctx context.Context) {
	defer close(e.doneCh)

	ticker := time.NewTicker(e.pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-e.stopCh:
			return
		case <-ticker.C:
			// Update heartbeat
			if err := e.store.UpdateHeartbeat(ctx, e.instanceID); err != nil {
				fmt.Fprintf(os.Stderr, "failed to update heartbeat: %v\n", err)
			}

			// Process one issue
			if err := e.processNextIssue(ctx); err != nil {
				// Log error but continue
				fmt.Fprintf(os.Stderr, "error processing issue: %v\n", err)
			}
		}
	}
}

// processNextIssue claims and processes the next ready issue
func (e *Executor) processNextIssue(ctx context.Context) error {
	// Get ready work (limit 1)
	filter := types.WorkFilter{
		Status: types.StatusOpen,
		Limit:  1,
	}

	issues, err := e.store.GetReadyWork(ctx, filter)
	if err != nil {
		return fmt.Errorf("failed to get ready work: %w", err)
	}

	if len(issues) == 0 {
		// No work available
		return nil
	}

	issue := issues[0]

	// Attempt to claim the issue
	if err := e.store.ClaimIssue(ctx, issue.ID, e.instanceID); err != nil {
		// Issue may have been claimed by another executor
		// This is expected in multi-executor scenarios
		return nil
	}

	// Successfully claimed - now execute it
	return e.executeIssue(ctx, issue)
}

// executeIssue executes a single issue by spawning a coding agent
func (e *Executor) executeIssue(ctx context.Context, issue *types.Issue) error {
	fmt.Printf("Executing issue %s: %s\n", issue.ID, issue.Title)

	// Phase 1: AI Assessment (if enabled)
	var assessment *ai.Assessment
	if e.enableAISupervision && e.supervisor != nil {
		// Update execution state to assessing
		if err := e.store.UpdateExecutionState(ctx, issue.ID, types.ExecutionStateAssessing); err != nil {
			fmt.Fprintf(os.Stderr, "warning: failed to update execution state: %v\n", err)
		}


		var err error
		assessment, err = e.supervisor.AssessIssueState(ctx, issue)
		if err != nil {
			// Don't fail execution - just log and continue without assessment
			fmt.Fprintf(os.Stderr, "Warning: AI assessment failed: %v (continuing without assessment)\n", err)
		} else {
			// Log the assessment as a comment
			assessmentComment := fmt.Sprintf("**AI Assessment**\n\nStrategy: %s\n\nConfidence: %.0f%%\n\nEstimated Effort: %s\n\nSteps:\n",
				assessment.Strategy, assessment.Confidence*100, assessment.EstimatedEffort)
			for i, step := range assessment.Steps {
				assessmentComment += fmt.Sprintf("%d. %s\n", i+1, step)
			}
			if len(assessment.Risks) > 0 {
				assessmentComment += "\nRisks:\n"
				for _, risk := range assessment.Risks {
					assessmentComment += fmt.Sprintf("- %s\n", risk)
				}
			}
			if err := e.store.AddComment(ctx, issue.ID, "ai-supervisor", assessmentComment); err != nil {
				fmt.Fprintf(os.Stderr, "warning: failed to add assessment comment: %v\n", err)
			}
		}
	}

	// Phase 2: Spawn the coding agent
	// Update execution state to executing
	if err := e.store.UpdateExecutionState(ctx, issue.ID, types.ExecutionStateExecuting); err != nil {
		fmt.Fprintf(os.Stderr, "warning: failed to update execution state: %v\n", err)
	}

	agentCfg := AgentConfig{
		Type:       AgentTypeClaudeCode, // Default to Claude Code for now
		WorkingDir: ".",
		Issue:      issue,
		StreamJSON: false,
		Timeout:    30 * time.Minute,
	}

	agent, err := SpawnAgent(ctx, agentCfg)
	if err != nil {
		e.releaseIssueWithError(ctx, issue.ID, fmt.Sprintf("Failed to spawn agent: %v", err))
		return fmt.Errorf("failed to spawn agent: %w", err)
	}

	// Wait for agent to complete
	result, err := agent.Wait(ctx)
	if err != nil {
		e.releaseIssueWithError(ctx, issue.ID, fmt.Sprintf("Agent execution failed: %v", err))
		return fmt.Errorf("agent execution failed: %w", err)
	}

	// Phase 3: Process results using ResultsProcessor
	// This handles AI analysis, quality gates, discovered issues, and tracker updates
	processor, err := NewResultsProcessor(&ResultsProcessorConfig{
		Store:              e.store,
		Supervisor:         e.supervisor,
		EnableQualityGates: e.enableQualityGates,
		WorkingDir:         e.workingDir,
		Actor:              e.instanceID,
	})
	if err != nil {
		e.releaseIssueWithError(ctx, issue.ID, fmt.Sprintf("Failed to create results processor: %v", err))
		return fmt.Errorf("failed to create results processor: %w", err)
	}

	procResult, err := processor.ProcessAgentResult(ctx, issue, result)
	if err != nil {
		e.releaseIssueWithError(ctx, issue.ID, fmt.Sprintf("Failed to process results: %v", err))
		return fmt.Errorf("failed to process agent result: %w", err)
	}

	// Print summary
	fmt.Println(procResult.Summary)

	return nil
}

// releaseIssueWithError releases an issue and adds an error comment
func (e *Executor) releaseIssueWithError(ctx context.Context, issueID, errMsg string) {
	// Add error comment
	if err := e.store.AddComment(ctx, issueID, e.instanceID, errMsg); err != nil {
		fmt.Fprintf(os.Stderr, "warning: failed to add error comment: %v\n", err)
	}

	// Release the execution state
	if err := e.store.ReleaseIssue(ctx, issueID); err != nil {
		fmt.Fprintf(os.Stderr, "warning: failed to release issue: %v\n", err)
	}
}
