package executor

import (
	"context"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/steveyegge/vc/internal/ai"
	"github.com/steveyegge/vc/internal/events"
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
	instanceID string
	hostname   string
	pid        int
	version    string

	// Control channels
	stopCh chan struct{}
	doneCh chan struct{}
	watchdogStopCh chan struct{} // Separate channel for watchdog shutdown
	watchdogDoneCh chan struct{} // Signals when watchdog goroutine finished

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
	WatchdogConfig      *watchdog.WatchdogConfig // Watchdog configuration (default: conservative defaults)
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

	// Wait for event loop to finish
	select {
	case <-e.doneCh:
		// Event loop finished
	case <-ctx.Done():
		return ctx.Err()
	}

	// Wait for watchdog to finish
	if e.watchdogConfig.IsEnabled() && e.analyzer != nil {
		select {
		case <-e.watchdogDoneCh:
			// Watchdog finished
		case <-ctx.Done():
			return ctx.Err()
		}
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

	// Phase 2: Spawn the coding agent
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
	defer agentCancel() // Always cancel when we're done

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
		WorkingDir: ".",
		Issue:      issue,
		StreamJSON: false,
		Timeout:    30 * time.Minute,
		// Enable event parsing and storage
		Store:      e.store,
		ExecutorID: e.instanceID,
		AgentID:    agentID,
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

	processor, err := NewResultsProcessor(&ResultsProcessorConfig{
		Store:              e.store,
		Supervisor:         e.supervisor,
		EnableQualityGates: e.enableQualityGates,
		WorkingDir:         e.workingDir,
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
			// Run anomaly detection
			if err := e.checkForAnomalies(ctx); err != nil {
				// Log error but continue monitoring
				fmt.Fprintf(os.Stderr, "watchdog: error checking for anomalies: %v\n", err)
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
