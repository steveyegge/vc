package executor

import (
	"context"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/steveyegge/vc/internal/ai"
	"github.com/steveyegge/vc/internal/config"
	"github.com/steveyegge/vc/internal/deduplication"
	"github.com/steveyegge/vc/internal/events"
	"github.com/steveyegge/vc/internal/sandbox"
	"github.com/steveyegge/vc/internal/storage"
	"github.com/steveyegge/vc/internal/types"
	"github.com/steveyegge/vc/internal/watchdog"
)

// Executor manages the issue processing event loop
type Executor struct {
	store      storage.Storage
	supervisor *ai.Supervisor
	monitor    *watchdog.Monitor
	analyzer   *watchdog.Analyzer
	intervention *watchdog.InterventionController
	watchdogConfig *watchdog.WatchdogConfig
	sandboxMgr sandbox.Manager
	config     *Config
	instanceID string
	hostname   string
	pid        int
	version    string

	// Control channels
	stopCh chan struct{}
	doneCh chan struct{}
	watchdogStopCh      chan struct{} // Separate channel for watchdog shutdown
	watchdogDoneCh      chan struct{} // Signals when watchdog goroutine finished
	cleanupStopCh       chan struct{} // Separate channel for cleanup goroutine shutdown
	cleanupDoneCh       chan struct{} // Signals when cleanup goroutine finished
	eventCleanupStopCh  chan struct{} // Separate channel for event cleanup shutdown
	eventCleanupDoneCh  chan struct{} // Signals when event cleanup goroutine finished

	// Configuration
	pollInterval        time.Duration
	cleanupInterval     time.Duration
	staleThreshold      time.Duration
	enableAISupervision bool
	enableQualityGates  bool
	enableSandboxes     bool
	workingDir          string

	// State
	mu      sync.RWMutex
	running bool
}

// Config holds executor configuration
type Config struct {
	Store                storage.Storage
	Version              string
	PollInterval         time.Duration
	HeartbeatPeriod      time.Duration
	CleanupInterval      time.Duration // How often to check for stale instances (default: 5 minutes)
	StaleThreshold       time.Duration // How long before an instance is considered stale (default: 5 minutes)
	EnableAISupervision  bool          // Enable AI assessment and analysis (default: true)
	EnableQualityGates   bool          // Enable quality gates enforcement (default: true)
	EnableSandboxes      bool          // Enable sandbox isolation (default: false)
	WorkingDir           string        // Working directory for quality gates (default: ".")
	SandboxRoot          string        // Root directory for sandboxes (default: ".sandboxes")
	ParentRepo           string        // Parent repository path (default: ".")
	DefaultBranch        string        // Default git branch for sandboxes (default: "main")
	WatchdogConfig       *watchdog.WatchdogConfig   // Watchdog configuration (default: conservative defaults)
	DeduplicationConfig  *deduplication.Config      // Deduplication configuration (default: sensible defaults, nil = use defaults)
	EventRetentionConfig *config.EventRetentionConfig // Event retention and cleanup configuration (default: sensible defaults, nil = use defaults)
}

// DefaultConfig returns default executor configuration
func DefaultConfig() *Config {
	return &Config{
		Version:             "0.1.0",
		PollInterval:        5 * time.Second,
		HeartbeatPeriod:     30 * time.Second,
		CleanupInterval:     5 * time.Minute,
		StaleThreshold:      5 * time.Minute,
		EnableAISupervision: true,
		EnableQualityGates:  true,
		EnableSandboxes:     false,
		WorkingDir:          ".",
		SandboxRoot:         ".sandboxes",
		ParentRepo:          ".",
		DefaultBranch:       "main",
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

	// Set default sandbox root if not specified
	sandboxRoot := cfg.SandboxRoot
	if sandboxRoot == "" {
		sandboxRoot = ".sandboxes"
	}

	// Set default parent repo if not specified
	parentRepo := cfg.ParentRepo
	if parentRepo == "" {
		parentRepo = "."
	}

	// Set default cleanup interval if not specified
	cleanupInterval := cfg.CleanupInterval
	if cleanupInterval == 0 {
		cleanupInterval = 5 * time.Minute
	}

	// Set default stale threshold if not specified
	staleThreshold := cfg.StaleThreshold
	if staleThreshold == 0 {
		staleThreshold = 5 * time.Minute
	}

	e := &Executor{
		store:               cfg.Store,
		config:              cfg,
		instanceID:          uuid.New().String(),
		hostname:            hostname,
		pid:                 os.Getpid(),
		version:             cfg.Version,
		pollInterval:        cfg.PollInterval,
		cleanupInterval:     cleanupInterval,
		staleThreshold:      staleThreshold,
		enableAISupervision: cfg.EnableAISupervision,
		enableQualityGates:  cfg.EnableQualityGates,
		enableSandboxes:     cfg.EnableSandboxes,
		workingDir:          workingDir,
		stopCh:              make(chan struct{}),
		doneCh:              make(chan struct{}),
		cleanupStopCh:       make(chan struct{}),
		cleanupDoneCh:       make(chan struct{}),
		eventCleanupStopCh:  make(chan struct{}),
		eventCleanupDoneCh:  make(chan struct{}),
	}

	// Initialize AI supervisor if enabled (do this before sandbox manager to provide deduplicator)
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

	// Initialize sandbox manager if enabled
	if cfg.EnableSandboxes {
		// Create deduplicator if we have a supervisor (vc-148)
		var dedup deduplication.Deduplicator
		if e.supervisor != nil {
			// Get deduplication config from executor config or use defaults
			dedupConfig := deduplication.DefaultConfig()
			if cfg.DeduplicationConfig != nil {
				dedupConfig = *cfg.DeduplicationConfig
			}

			var err error
			dedup, err = deduplication.NewAIDeduplicator(e.supervisor, cfg.Store, dedupConfig)
			if err != nil {
				// Don't fail - just continue without deduplication
				fmt.Fprintf(os.Stderr, "Warning: failed to create deduplicator: %v (continuing without deduplication)\n", err)
				dedup = nil
			}
		}

		sandboxMgr, err := sandbox.NewManager(sandbox.Config{
			SandboxRoot:         sandboxRoot,
			ParentRepo:          parentRepo,
			MainDB:              cfg.Store,
			Deduplicator:        dedup, // Pass deduplicator for sandbox merge deduplication
			DeduplicationConfig: cfg.DeduplicationConfig,
		})
		if err != nil {
			// Don't fail - just disable sandboxes
			fmt.Fprintf(os.Stderr, "Warning: failed to initialize sandbox manager: %v (continuing without sandboxes)\n", err)
			e.enableSandboxes = false
		} else {
			e.sandboxMgr = sandboxMgr
		}
	}

	// Initialize watchdog system
	e.monitor = watchdog.NewMonitor(watchdog.DefaultConfig())

	// Use provided watchdog config or default
	if cfg.WatchdogConfig == nil {
		e.watchdogConfig = watchdog.DefaultWatchdogConfig()
	} else {
		e.watchdogConfig = cfg.WatchdogConfig
	}

	// Initialize watchdog channels
	e.watchdogStopCh = make(chan struct{})
	e.watchdogDoneCh = make(chan struct{})

	// Initialize analyzer if AI supervision is enabled
	if e.enableAISupervision && e.supervisor != nil {
		analyzer, err := watchdog.NewAnalyzer(&watchdog.AnalyzerConfig{
			Monitor:    e.monitor,
			Supervisor: e.supervisor,
			Store:      cfg.Store,
		})
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to initialize watchdog analyzer: %v (watchdog disabled)\n", err)
		} else {
			e.analyzer = analyzer
		}
	}

	// Initialize intervention controller
	intervention, err := watchdog.NewInterventionController(&watchdog.InterventionControllerConfig{
		Store:              cfg.Store,
		ExecutorInstanceID: e.instanceID,
		MaxHistorySize:     e.watchdogConfig.MaxHistorySize,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to initialize intervention controller: %v (watchdog disabled)\n", err)
	} else {
		e.intervention = intervention
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

	// Start the watchdog loop if enabled and components are initialized
	if e.watchdogConfig.IsEnabled() && e.analyzer != nil && e.intervention != nil {
		go e.watchdogLoop(ctx)
		fmt.Printf("Watchdog: Started monitoring (check_interval=%v, min_confidence=%.2f, min_severity=%s)\n",
			e.watchdogConfig.GetCheckInterval(),
			e.watchdogConfig.AIConfig.MinConfidenceThreshold,
			e.watchdogConfig.AIConfig.MinSeverityLevel)
	}

	// Start the cleanup loop
	go e.cleanupLoop(ctx)
	fmt.Printf("Cleanup: Started stale instance cleanup (check_interval=%v, stale_threshold=%v)\n",
		e.cleanupInterval, e.staleThreshold)

	// Start the event cleanup loop
	go e.eventCleanupLoop(ctx)

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

	// Stop watchdog if it's running
	if e.watchdogConfig.IsEnabled() && e.analyzer != nil {
		close(e.watchdogStopCh)
	}

	// Stop cleanup goroutine
	close(e.cleanupStopCh)

	// Stop event cleanup goroutine
	close(e.eventCleanupStopCh)

	// Wait for event loop, watchdog, cleanup, and event cleanup to finish concurrently (vc-113, vc-122, vc-195)
	// This prevents sequential timeouts if one takes longer than expected
	eventDone := false
	watchdogDone := !e.watchdogConfig.IsEnabled() || e.analyzer == nil // Skip if not enabled
	cleanupDone := false
	eventCleanupDone := false

	for !eventDone || !watchdogDone || !cleanupDone || !eventCleanupDone {
		select {
		case <-e.doneCh:
			eventDone = true
		case <-e.watchdogDoneCh:
			watchdogDone = true
		case <-e.cleanupDoneCh:
			cleanupDone = true
		case <-e.eventCleanupDoneCh:
			eventCleanupDone = true
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	// Mark instance as stopped (vc-113: Don't fail shutdown if this fails)
	// Get current instance to preserve all fields
	instances, err := e.store.GetActiveInstances(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: failed to get active instances during shutdown: %v\n", err)
	} else {
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
				fmt.Fprintf(os.Stderr, "warning: failed to mark instance as stopped: %v\n", err)
			}
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

	// Start telemetry collection for this execution
	e.monitor.StartExecution(issue.ID, e.instanceID)

	// Log issue claimed event
	e.logEvent(ctx, events.EventTypeIssueClaimed, events.SeverityInfo, issue.ID,
		fmt.Sprintf("Issue %s claimed by executor %s", issue.ID, e.instanceID),
		map[string]interface{}{
			"issue_title": issue.Title,
		})
	e.monitor.RecordEvent(string(events.EventTypeIssueClaimed))

	// Phase 1: AI Assessment (if enabled)
	var assessment *ai.Assessment
	var assessmentRan bool
	if e.enableAISupervision && e.supervisor != nil {
		assessmentRan = true
		// Update execution state to assessing
		if err := e.store.UpdateExecutionState(ctx, issue.ID, types.ExecutionStateAssessing); err != nil {
			fmt.Fprintf(os.Stderr, "warning: failed to update execution state: %v\n", err)
		}
		e.monitor.RecordStateTransition(types.ExecutionStateClaimed, types.ExecutionStateAssessing)

		// Log assessment started
		e.logEvent(ctx, events.EventTypeAssessmentStarted, events.SeverityInfo, issue.ID,
			fmt.Sprintf("Starting AI assessment for issue %s", issue.ID),
			map[string]interface{}{})

		var err error
		assessment, err = e.supervisor.AssessIssueState(ctx, issue)
		if err != nil {
			// Don't fail execution - just log and continue without assessment
			fmt.Fprintf(os.Stderr, "Warning: AI assessment failed: %v (continuing without assessment)\n", err)
			// Log assessment failure
			e.logEvent(ctx, events.EventTypeAssessmentCompleted, events.SeverityError, issue.ID,
				fmt.Sprintf("AI assessment failed: %v", err),
				map[string]interface{}{
					"success": false,
					"error":   err.Error(),
				})
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

			// Log assessment success
			e.logEvent(ctx, events.EventTypeAssessmentCompleted, events.SeverityInfo, issue.ID,
				fmt.Sprintf("AI assessment completed for issue %s", issue.ID),
				map[string]interface{}{
					"success":          true,
					"strategy":         assessment.Strategy,
					"confidence":       assessment.Confidence,
					"estimated_effort": assessment.EstimatedEffort,
					"steps_count":      len(assessment.Steps),
					"risks_count":      len(assessment.Risks),
				})
		}
	}

	// Phase 2: Create sandbox if enabled
	var sb *sandbox.Sandbox
	workingDir := e.workingDir
	if e.enableSandboxes && e.sandboxMgr != nil {
		fmt.Printf("Creating sandbox for issue %s...\n", issue.ID)

		// Get parent repo from config (will be set by manager if not specified)
		parentRepo := "."
		if e.config != nil && e.config.ParentRepo != "" {
			parentRepo = e.config.ParentRepo
		}

		// Get base branch from config
		baseBranch := "main"
		if e.config != nil && e.config.DefaultBranch != "" {
			baseBranch = e.config.DefaultBranch
		}

		sandboxCfg := sandbox.SandboxConfig{
			MissionID:  issue.ID,
			ParentRepo: parentRepo,
			BaseBranch: baseBranch,
		}

		var err error
		sb, err = e.sandboxMgr.Create(ctx, sandboxCfg)
		if err != nil {
			// Don't fail execution - just log and continue without sandbox
			fmt.Fprintf(os.Stderr, "Warning: failed to create sandbox: %v (continuing in main workspace)\n", err)
		} else {
			// Set working directory to sandbox path
			workingDir = sb.Path
			fmt.Printf("Sandbox created: %s (branch: %s)\n", sb.Path, sb.GitBranch)

			// Ensure cleanup happens
			defer func() {
				if sb != nil {
					fmt.Printf("Cleaning up sandbox %s...\n", sb.ID)
					if err := e.sandboxMgr.Cleanup(ctx, sb); err != nil {
						fmt.Fprintf(os.Stderr, "warning: failed to cleanup sandbox: %v\n", err)
					}
				}
			}()
		}
	}

	// Phase 3: Spawn the coding agent
	// Update execution state to executing
	if err := e.store.UpdateExecutionState(ctx, issue.ID, types.ExecutionStateExecuting); err != nil {
		fmt.Fprintf(os.Stderr, "warning: failed to update execution state: %v\n", err)
	}
	// Record state transition based on whether assessment actually ran
	if assessmentRan {
		e.monitor.RecordStateTransition(types.ExecutionStateAssessing, types.ExecutionStateExecuting)
	} else {
		e.monitor.RecordStateTransition(types.ExecutionStateClaimed, types.ExecutionStateExecuting)
	}

	// Create a cancelable context for the agent so watchdog can intervene
	agentCtx, agentCancel := context.WithCancel(ctx)
	defer func() {
		fmt.Printf("[DEBUG vc-177] agentCancel called for issue %s\n", issue.ID)
		agentCancel() // Always cancel when we're done
	}()

	// Register agent context with intervention controller for watchdog
	if e.intervention != nil {
		e.intervention.SetAgentContext(issue.ID, agentCancel)
		defer e.intervention.ClearAgentContext()
	}

	// Gather context for comprehensive prompt
	gatherer := NewContextGatherer(e.store)
	promptCtx, err := gatherer.GatherContext(ctx, issue, nil)
	if err != nil {
		e.logEvent(ctx, events.EventTypeAgentSpawned, events.SeverityError, issue.ID,
			fmt.Sprintf("Failed to gather context: %v", err),
			map[string]interface{}{
				"success": false,
				"error":   err.Error(),
			})
		e.releaseIssueWithError(ctx, issue.ID, fmt.Sprintf("Failed to gather context: %v", err))
		e.monitor.EndExecution(false, false)
		return fmt.Errorf("failed to gather context: %w", err)
	}

	// Build comprehensive prompt using PromptBuilder
	builder, err := NewPromptBuilder()
	if err != nil {
		e.logEvent(ctx, events.EventTypeAgentSpawned, events.SeverityError, issue.ID,
			fmt.Sprintf("Failed to create prompt builder: %v", err),
			map[string]interface{}{
				"success": false,
				"error":   err.Error(),
			})
		e.releaseIssueWithError(ctx, issue.ID, fmt.Sprintf("Failed to create prompt builder: %v", err))
		e.monitor.EndExecution(false, false)
		return fmt.Errorf("failed to create prompt builder: %w", err)
	}

	prompt, err := builder.BuildPrompt(promptCtx)
	if err != nil {
		e.logEvent(ctx, events.EventTypeAgentSpawned, events.SeverityError, issue.ID,
			fmt.Sprintf("Failed to build prompt: %v", err),
			map[string]interface{}{
				"success": false,
				"error":   err.Error(),
			})
		e.releaseIssueWithError(ctx, issue.ID, fmt.Sprintf("Failed to build prompt: %v", err))
		e.monitor.EndExecution(false, false)
		return fmt.Errorf("failed to build prompt: %w", err)
	}

	// Log prompt for debugging if VC_DEBUG_PROMPTS is set
	if os.Getenv("VC_DEBUG_PROMPTS") != "" {
		fmt.Fprintf(os.Stderr, "\n=== AGENT PROMPT ===\n%s\n=== END PROMPT ===\n\n", prompt)
	}

	// Generate a unique agent ID for this execution
	agentID := uuid.New().String()

	agentCfg := AgentConfig{
		Type:       AgentTypeClaudeCode, // Default to Claude Code for now
		WorkingDir: workingDir,
		Issue:      issue,
		StreamJSON: false,
		Timeout:    30 * time.Minute,
		// Enable event parsing and storage
		Store:      e.store,
		ExecutorID: e.instanceID,
		AgentID:    agentID,
		Sandbox:    sb,
	}

	agent, err := SpawnAgent(agentCtx, agentCfg, prompt)
	if err != nil {
		// Log agent spawn failure BEFORE releasing issue
		e.logEvent(ctx, events.EventTypeAgentSpawned, events.SeverityError, issue.ID,
			fmt.Sprintf("Failed to spawn agent: %v", err),
			map[string]interface{}{
				"success":    false,
				"agent_type": agentCfg.Type,
				"error":      err.Error(),
			})
		e.releaseIssueWithError(ctx, issue.ID, fmt.Sprintf("Failed to spawn agent: %v", err))
		// End telemetry collection on failure
		e.monitor.EndExecution(false, false)
		return fmt.Errorf("failed to spawn agent: %w", err)
	}

	// Log agent spawned successfully
	e.logEvent(ctx, events.EventTypeAgentSpawned, events.SeverityInfo, issue.ID,
		fmt.Sprintf("Agent spawned for issue %s", issue.ID),
		map[string]interface{}{
			"success":    true,
			"agent_type": agentCfg.Type,
		})

	// Wait for agent to complete
	result, err := agent.Wait(agentCtx)
	if err != nil {
		// Log agent execution failure BEFORE releasing issue
		e.logEvent(ctx, events.EventTypeAgentCompleted, events.SeverityError, issue.ID,
			fmt.Sprintf("Agent execution failed: %v", err),
			map[string]interface{}{
				"success": false,
				"error":   err.Error(),
			})
		e.releaseIssueWithError(ctx, issue.ID, fmt.Sprintf("Agent execution failed: %v", err))
		// End telemetry collection on failure
		e.monitor.EndExecution(false, false)
		return fmt.Errorf("agent execution failed: %w", err)
	}

	// Log agent execution success
	e.logEvent(ctx, events.EventTypeAgentCompleted, events.SeverityInfo, issue.ID,
		fmt.Sprintf("Agent completed execution for issue %s", issue.ID),
		map[string]interface{}{
			"success":      true,
			"exit_code":    result.ExitCode,
			"duration_ms":  result.Duration.Milliseconds(),
			"output_lines": len(result.Output),
		})

	// Phase 3: Process results using ResultsProcessor
	// This handles AI analysis, quality gates, discovered issues, and tracker updates

	// Log results processing started
	e.logEvent(ctx, events.EventTypeResultsProcessingStarted, events.SeverityInfo, issue.ID,
		fmt.Sprintf("Starting results processing for issue %s", issue.ID),
		map[string]interface{}{})

	// Create deduplicator if AI supervision is enabled (vc-145)
	var dedup deduplication.Deduplicator
	if e.supervisor != nil {
		// Get deduplication config from executor config or use defaults
		dedupConfig := deduplication.DefaultConfig()
		if e.config != nil && e.config.DeduplicationConfig != nil {
			dedupConfig = *e.config.DeduplicationConfig
		}

		var err error
		dedup, err = deduplication.NewAIDeduplicator(e.supervisor, e.store, dedupConfig)
		if err != nil {
			// Log warning but continue without deduplication (fail-safe behavior)
			e.logEvent(ctx, events.EventTypeError, events.SeverityWarning, issue.ID,
				fmt.Sprintf("Failed to create deduplicator: %v (continuing without deduplication)", err),
				map[string]interface{}{"error": err.Error()})
			dedup = nil
		}
	}

	processor, err := NewResultsProcessor(&ResultsProcessorConfig{
		Store:              e.store,
		Supervisor:         e.supervisor,
		Deduplicator:       dedup,
		EnableQualityGates: e.enableQualityGates,
		WorkingDir:         workingDir, // Use sandbox path if sandboxing is enabled (vc-117)
		Actor:              e.instanceID,
	})
	if err != nil {
		// Log results processing failure BEFORE releasing issue
		e.logEvent(ctx, events.EventTypeResultsProcessingCompleted, events.SeverityError, issue.ID,
			fmt.Sprintf("Results processor creation failed: %v", err),
			map[string]interface{}{
				"success": false,
				"error":   err.Error(),
			})
		e.releaseIssueWithError(ctx, issue.ID, fmt.Sprintf("Failed to create results processor: %v", err))
		// End telemetry collection on failure
		e.monitor.EndExecution(false, false)
		return fmt.Errorf("failed to create results processor: %w", err)
	}

	procResult, err := processor.ProcessAgentResult(ctx, issue, result)
	if err != nil {
		// Log results processing failure BEFORE releasing issue
		e.logEvent(ctx, events.EventTypeResultsProcessingCompleted, events.SeverityError, issue.ID,
			fmt.Sprintf("Results processing failed: %v", err),
			map[string]interface{}{
				"success": false,
				"error":   err.Error(),
			})
		e.releaseIssueWithError(ctx, issue.ID, fmt.Sprintf("Failed to process results: %v", err))
		// End telemetry collection on failure
		e.monitor.EndExecution(false, false)
		return fmt.Errorf("failed to process agent result: %w", err)
	}

	// Log results processing success
	e.logEvent(ctx, events.EventTypeResultsProcessingCompleted, events.SeverityInfo, issue.ID,
		fmt.Sprintf("Results processing completed for issue %s", issue.ID),
		map[string]interface{}{
			"success":           true,
			"completed":         procResult.Completed,
			"gates_passed":      procResult.GatesPassed,
			"discovered_issues": len(procResult.DiscoveredIssues),
			"commit_hash":       procResult.CommitHash,
		})

	// Print summary
	fmt.Println(procResult.Summary)

	// End telemetry collection
	e.monitor.EndExecution(procResult.Completed && result.Success, procResult.GatesPassed)

	return nil
}

// logEvent creates and stores an agent event for observability
func (e *Executor) logEvent(ctx context.Context, eventType events.EventType, severity events.EventSeverity, issueID, message string, data map[string]interface{}) {
	// Skip logging if context is cancelled (e.g., during shutdown)
	if ctx.Err() != nil {
		return
	}

	event := &events.AgentEvent{
		ID:         uuid.New().String(),
		Type:       eventType,
		Timestamp:  time.Now(),
		IssueID:    issueID,
		ExecutorID: e.instanceID,
		AgentID:    "", // Empty for executor-level events (not produced by coding agents)
		Severity:   severity,
		Message:    message,
		Data:       data,
		SourceLine: 0, // Not applicable for executor-level events
	}

	if err := e.store.StoreAgentEvent(ctx, event); err != nil {
		// Log error but don't fail execution
		fmt.Fprintf(os.Stderr, "warning: failed to store agent event: %v\n", err)
	}
}

// releaseIssueWithError releases an issue and adds an error comment
// If there are too many consecutive failures, the issue is marked as blocked instead of reopened
func (e *Executor) releaseIssueWithError(ctx context.Context, issueID, errMsg string) {
	const maxConsecutiveFailures = 3 // Block after 3 consecutive failures

	// Get execution history to check for consecutive failures
	history, err := e.store.GetExecutionHistory(ctx, issueID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: failed to get execution history for %s: %v\n", issueID, err)
		// Fall through to reopen - safer to retry than block on error
	}

	// Count recent consecutive failures
	consecutiveFailures := 0
	for i := len(history) - 1; i >= 0; i-- {
		attempt := history[i]
		// Only count completed attempts
		if attempt.Success == nil {
			continue // Skip incomplete attempts
		}
		if !*attempt.Success {
			consecutiveFailures++
		} else {
			break // Stop counting at first success
		}
	}

	// Check if we should block due to too many failures
	if consecutiveFailures >= maxConsecutiveFailures {
		fmt.Fprintf(os.Stderr, "Issue %s has %d consecutive failures, marking as blocked\n",
			issueID, consecutiveFailures)

		// Mark as blocked instead of reopening
		blockReason := fmt.Sprintf("Blocked after %d consecutive execution failures. Last error: %s",
			consecutiveFailures, errMsg)

		// Release execution state and mark as blocked
		if err := e.store.ReleaseIssue(ctx, issueID); err != nil {
			fmt.Fprintf(os.Stderr, "warning: failed to release issue %s: %v\n", issueID, err)
		}

		if err := e.store.UpdateIssue(ctx, issueID, map[string]interface{}{
			"status": types.StatusBlocked,
		}, "executor"); err != nil {
			fmt.Fprintf(os.Stderr, "warning: failed to mark issue %s as blocked: %v\n", issueID, err)
		}

		if err := e.store.AddComment(ctx, issueID, "executor", blockReason); err != nil {
			fmt.Fprintf(os.Stderr, "warning: failed to add comment to %s: %v\n", issueID, err)
		}
		return
	}

	// Not enough failures yet, reopen for retry
	if consecutiveFailures > 0 {
		fmt.Fprintf(os.Stderr, "Issue %s has %d consecutive failures, reopening for retry\n",
			issueID, consecutiveFailures)
	}

	// Use atomic ReleaseIssueAndReopen to ensure issue returns to 'open' status
	// This allows the issue to be retried instead of getting stuck in 'in_progress'
	if err := e.store.ReleaseIssueAndReopen(ctx, issueID, e.instanceID, errMsg); err != nil {
		fmt.Fprintf(os.Stderr, "warning: failed to release and reopen issue: %v\n", err)
	}
}

// watchdogLoop runs the watchdog monitoring in a background goroutine
// It periodically checks for anomalies and intervenes when necessary
func (e *Executor) watchdogLoop(ctx context.Context) {
	defer close(e.watchdogDoneCh)

	ticker := time.NewTicker(e.watchdogConfig.GetCheckInterval())
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-e.watchdogStopCh:
			return
		case <-ticker.C:
			// Check if we should stop before running potentially slow anomaly check (vc-113)
			select {
			case <-e.watchdogStopCh:
				return
			default:
			}

			// Run anomaly detection with cancellation support (vc-113)
			// Use a channel to make check interruptible
			done := make(chan error, 1)
			go func() {
				done <- e.checkForAnomalies(ctx)
			}()

			// Wait for either completion or stop signal
			select {
			case err := <-done:
				if err != nil {
					// Log error but continue monitoring
					fmt.Fprintf(os.Stderr, "watchdog: error checking for anomalies: %v\n", err)
				}
			case <-e.watchdogStopCh:
				// Stop signal received while checking - exit immediately
				// The goroutine will finish in the background
				return
			}
		}
	}
}

// checkForAnomalies performs one cycle of anomaly detection and intervention
func (e *Executor) checkForAnomalies(ctx context.Context) error {
	// Skip if no analyzer (watchdog disabled)
	if e.analyzer == nil {
		return nil
	}

	// Detect anomalies using AI analysis of telemetry
	report, err := e.analyzer.DetectAnomalies(ctx)
	if err != nil {
		return fmt.Errorf("anomaly detection failed: %w", err)
	}

	// If no anomaly detected, nothing to do
	if !report.Detected {
		return nil
	}

	// Check if this anomaly meets the threshold for intervention
	if !e.watchdogConfig.ShouldIntervene(report) {
		// Anomaly detected but below threshold - just log it
		if e.watchdogConfig.AIConfig.EnableAnomalyLogging {
			fmt.Printf("Watchdog: Anomaly detected but below threshold - type=%s, severity=%s, confidence=%.2f (threshold: confidence=%.2f, severity=%s)\n",
				report.AnomalyType, report.Severity, report.Confidence,
				e.watchdogConfig.AIConfig.MinConfidenceThreshold,
				e.watchdogConfig.AIConfig.MinSeverityLevel)
		}
		return nil
	}

	// Anomaly meets threshold - intervene
	fmt.Printf("Watchdog: Intervening - type=%s, severity=%s, confidence=%.2f, recommended_action=%s\n",
		report.AnomalyType, report.Severity, report.Confidence, report.RecommendedAction)

	// Use intervention controller to decide and execute intervention
	result, err := e.intervention.Intervene(ctx, report)
	if err != nil {
		return fmt.Errorf("intervention failed: %w", err)
	}

	fmt.Printf("Watchdog: Intervention completed - %s (escalation issue: %s)\n",
		result.Message, result.EscalationIssueID)

	return nil
}

// cleanupLoop runs periodic cleanup of stale executor instances in a background goroutine
// When instances are marked as stale, their claimed issues are automatically released
func (e *Executor) cleanupLoop(ctx context.Context) {
	defer close(e.cleanupDoneCh)

	ticker := time.NewTicker(e.cleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-e.cleanupStopCh:
			return
		case <-ticker.C:
			// Check if we should stop before running cleanup
			select {
			case <-e.cleanupStopCh:
				return
			default:
			}

			// Run cleanup with cancellation support
			// Use a channel to make cleanup interruptible
			done := make(chan error, 1)
			go func() {
				staleThresholdSecs := int(e.staleThreshold.Seconds())
				cleaned, err := e.store.CleanupStaleInstances(ctx, staleThresholdSecs)
				if err != nil {
					done <- err
					return
				}
				if cleaned > 0 {
					fmt.Printf("Cleanup: Marked %d stale instance(s) as stopped and released their claims\n", cleaned)
				}
				done <- nil
			}()

			// Wait for either completion or stop signal
			select {
			case err := <-done:
				if err != nil {
					// Log error but continue monitoring
					fmt.Fprintf(os.Stderr, "cleanup: error cleaning up stale instances: %v\n", err)
				}
			case <-e.cleanupStopCh:
				// Stop signal received while cleaning - exit immediately
				// The goroutine will finish in the background
				return
			}
		}
	}
}

// eventCleanupLoop runs periodic cleanup of old events in a background goroutine
// This enforces event retention policies to prevent database bloat
func (e *Executor) eventCleanupLoop(ctx context.Context) {
	defer close(e.eventCleanupDoneCh)

	// Get event retention config (from executor config or defaults)
	retentionCfg := config.DefaultEventRetentionConfig()
	if e.config != nil && e.config.EventRetentionConfig != nil {
		retentionCfg = *e.config.EventRetentionConfig
	}

	// Skip cleanup if disabled
	if !retentionCfg.CleanupEnabled {
		fmt.Printf("Event cleanup: Disabled via configuration\n")
		return
	}

	// Create ticker with configured interval
	cleanupInterval := time.Duration(retentionCfg.CleanupIntervalHours) * time.Hour
	ticker := time.NewTicker(cleanupInterval)
	defer ticker.Stop()

	fmt.Printf("Event cleanup: Started (interval=%v, retention=%dd, per_issue_limit=%d, global_limit=%d)\n",
		cleanupInterval, retentionCfg.RetentionDays, retentionCfg.PerIssueLimitEvents, retentionCfg.GlobalLimitEvents)

	for {
		select {
		case <-ctx.Done():
			return
		case <-e.eventCleanupStopCh:
			return
		case <-ticker.C:
			// Check if we should stop before running cleanup
			select {
			case <-e.eventCleanupStopCh:
				return
			default:
			}

			// Run cleanup with cancellation support
			done := make(chan error, 1)
			go func() {
				done <- e.runEventCleanup(ctx, retentionCfg)
			}()

			// Wait for either completion or stop signal
			select {
			case err := <-done:
				if err != nil {
					// Log error but continue monitoring
					fmt.Fprintf(os.Stderr, "event cleanup: error during cleanup: %v\n", err)
				}
			case <-e.eventCleanupStopCh:
				// Stop signal received while cleaning - exit immediately
				// The goroutine will finish in the background
				return
			}
		}
	}
}

// runEventCleanup executes one cycle of event cleanup
func (e *Executor) runEventCleanup(ctx context.Context, cfg config.EventRetentionConfig) error {
	startTime := time.Now()

	// Track metrics for logging
	var timeBasedDeleted, perIssueDeleted, globalLimitDeleted int
	var vacuumRan bool

	// Step 1: Time-based cleanup (delete old events)
	deleted, err := e.store.CleanupEventsByAge(ctx, cfg.RetentionDays, cfg.RetentionCriticalDays, cfg.CleanupBatchSize)
	if err != nil {
		return fmt.Errorf("time-based cleanup failed: %w", err)
	}
	timeBasedDeleted = deleted

	// Step 2: Per-issue limit cleanup (enforce per-issue event caps)
	deleted, err = e.store.CleanupEventsByIssueLimit(ctx, cfg.PerIssueLimitEvents, cfg.CleanupBatchSize)
	if err != nil {
		return fmt.Errorf("per-issue limit cleanup failed: %w", err)
	}
	perIssueDeleted = deleted

	// Step 3: Global limit cleanup (enforce global safety limit)
	// Trigger aggressive cleanup at 95% of configured limit
	triggerThreshold := int(float64(cfg.GlobalLimitEvents) * 0.95)
	deleted, err = e.store.CleanupEventsByGlobalLimit(ctx, triggerThreshold, cfg.CleanupBatchSize)
	if err != nil {
		return fmt.Errorf("global limit cleanup failed: %w", err)
	}
	globalLimitDeleted = deleted

	totalDeleted := timeBasedDeleted + perIssueDeleted + globalLimitDeleted

	// Step 4: Optional VACUUM to reclaim disk space
	if cfg.CleanupVacuum && totalDeleted > 0 {
		if err := e.store.VacuumDatabase(ctx); err != nil {
			// Don't fail the whole cleanup if VACUUM fails
			fmt.Fprintf(os.Stderr, "event cleanup: warning: VACUUM failed: %v\n", err)
		} else {
			vacuumRan = true
		}
	}

	// Get remaining event count for metrics
	counts, err := e.store.GetEventCounts(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "event cleanup: warning: failed to get event counts: %v\n", err)
	}

	processingTimeMs := time.Since(startTime).Milliseconds()

	// Log cleanup metrics by storing structured cleanup event
	// Note: We can't use logEvent() since it requires a non-empty issue_id
	// Instead, we just log to stdout for observability
	// In the future, we could create a separate cleanup_events table if needed

	if totalDeleted > 0 || vacuumRan {
		fmt.Printf("Event cleanup: Deleted %d events (time_based=%d, per_issue=%d, global_limit=%d) in %dms",
			totalDeleted, timeBasedDeleted, perIssueDeleted, globalLimitDeleted, processingTimeMs)
		if vacuumRan {
			fmt.Printf(" [VACUUM ran]")
		}
		if counts != nil {
			fmt.Printf(" (remaining=%d)", counts.TotalEvents)
		}
		fmt.Println()
	}

	return nil
}
