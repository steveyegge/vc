package watchdog

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/steveyegge/vc/internal/ai"
	"github.com/steveyegge/vc/internal/storage"
)

// Watchdog orchestrates monitoring, anomaly detection, and intervention
// It combines telemetry monitoring, context usage tracking, and AI-driven analysis
type Watchdog struct {
	mu sync.RWMutex

	// Core components
	monitor                *Monitor
	analyzer               *Analyzer
	contextDetector        *ContextDetector
	interventionController *InterventionController

	// Configuration
	config *WatchdogConfig

	// Control
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	// State
	running bool
}

// WatchdogDeps holds dependencies for creating a Watchdog
type WatchdogDeps struct {
	Store              storage.Storage
	Supervisor         *ai.Supervisor
	ExecutorInstanceID string
	Config             *WatchdogConfig
}

// NewWatchdog creates a new watchdog instance
func NewWatchdog(deps *WatchdogDeps) (*Watchdog, error) {
	if deps.Store == nil {
		return nil, fmt.Errorf("store is required")
	}
	if deps.Supervisor == nil {
		return nil, fmt.Errorf("supervisor is required")
	}
	if deps.ExecutorInstanceID == "" {
		return nil, fmt.Errorf("executor_instance_id is required")
	}

	// Use default config if not provided
	config := deps.Config
	if config == nil {
		config = DefaultWatchdogConfig()
	}

	// Create telemetry monitor
	monitor := NewMonitor(&Config{
		WindowSize: config.TelemetryWindowSize,
	})

	// Create analyzer
	analyzer, err := NewAnalyzer(&AnalyzerConfig{
		Monitor:    monitor,
		Supervisor: deps.Supervisor,
		Store:      deps.Store,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create analyzer: %w", err)
	}

	// Create context detector
	contextDetector := NewContextDetector(deps.Store)

	// Create intervention controller
	interventionController, err := NewInterventionController(&InterventionControllerConfig{
		Store:              deps.Store,
		ExecutorInstanceID: deps.ExecutorInstanceID,
		MaxHistorySize:     config.MaxHistorySize,
		Config:             config, // Pass config for backoff tracking (vc-21pw)
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create intervention controller: %w", err)
	}

	// Wire up analyzer with intervention controller and config for backoff analysis (vc-ysqs)
	analyzer.SetInterventionController(interventionController)
	analyzer.SetConfig(config)

	return &Watchdog{
		monitor:                monitor,
		analyzer:               analyzer,
		contextDetector:        contextDetector,
		interventionController: interventionController,
		config:                 config,
	}, nil
}

// Start begins the watchdog monitoring loop
func (w *Watchdog) Start(ctx context.Context) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.running {
		return fmt.Errorf("watchdog already running")
	}

	if !w.config.IsEnabled() {
		fmt.Println("Watchdog: disabled by configuration, not starting")
		return nil
	}

	// Create context for watchdog lifecycle
	w.ctx, w.cancel = context.WithCancel(ctx)
	w.running = true

	// Start monitoring loop
	w.wg.Add(1)
	go w.monitoringLoop()

	fmt.Printf("Watchdog: started (check_interval=%v)\n", w.config.GetCheckInterval())
	return nil
}

// Stop gracefully stops the watchdog
func (w *Watchdog) Stop() {
	w.mu.Lock()
	defer w.mu.Unlock()

	if !w.running {
		return
	}

	fmt.Println("Watchdog: stopping...")
	w.cancel()
	w.running = false
	w.wg.Wait()
	fmt.Println("Watchdog: stopped")
}

// monitoringLoop is the main watchdog loop
// It periodically checks for anomalies and intervenes if needed
// Uses dynamic interval based on backoff state (vc-21pw)
func (w *Watchdog) monitoringLoop() {
	defer w.wg.Done()

	// Use a timer instead of ticker so we can reset the interval dynamically
	timer := time.NewTimer(w.config.GetCurrentCheckInterval())
	defer timer.Stop()

	for {
		select {
		case <-w.ctx.Done():
			return

		case <-timer.C:
			tickCtx, tickCancel := context.WithTimeout(w.ctx, 30*time.Second)
			
			// Check for context exhaustion first (highest priority)
			if err := w.checkContextExhaustion(tickCtx); err != nil {
				fmt.Printf("Watchdog: context exhaustion check failed: %v\n", err)
			}

			// Run general anomaly detection
			if err := w.checkAnomalies(tickCtx); err != nil {
				fmt.Printf("Watchdog: anomaly detection failed: %v\n", err)
			}

			tickCancel()

			// Reset timer with current interval (may have changed due to backoff)
			currentInterval := w.config.GetCurrentCheckInterval()
			timer.Reset(currentInterval)
		}
	}
}

// checkContextExhaustion checks if context usage is approaching exhaustion
// This is the highest priority check since it's time-sensitive
func (w *Watchdog) checkContextExhaustion(ctx context.Context) error {
	metrics := w.contextDetector.GetMetrics()

	// Check if context is exhausting (80%+ usage)
	if !metrics.IsExhausting {
		return nil
	}

	// Create an anomaly report for context exhaustion
	report := &AnomalyReport{
		Detected:          true,
		AnomalyType:       AnomalyContextExhaustion,
		Severity:          SeverityHigh,
		Description:       fmt.Sprintf("Context usage at %.1f%%, approaching exhaustion limit", metrics.CurrentUsagePercent),
		RecommendedAction: ActionCheckpoint,
		Reasoning: fmt.Sprintf("Context usage has reached %.1f%% (threshold: 80%%). "+
			"Burn rate: %.2f%%/min. "+
			"Estimated exhaustion: %v. "+
			"Requesting checkpoint to prevent context overflow.",
			metrics.CurrentUsagePercent,
			metrics.BurnRate,
			metrics.EstimatedExhaustion.Format("15:04:05")),
		Confidence:     metrics.CurrentUsagePercent / 100.0, // Use usage % as confidence
		AffectedIssues: []string{w.interventionController.GetCurrentIssueID()},
		Metrics: map[string]interface{}{
			"usage_percent":        metrics.CurrentUsagePercent,
			"burn_rate":            metrics.BurnRate,
			"estimated_exhaustion": metrics.EstimatedExhaustion,
			"measurement_count":    metrics.MeasurementCount,
		},
	}

	// Check if we should intervene based on config
	if !w.config.ShouldIntervene(report) {
		// Log but don't intervene
		if w.config.AIConfig.EnableAnomalyLogging {
			fmt.Printf("Watchdog: Context exhaustion detected but below intervention threshold\n")
		}
		return nil
	}

	// Intervene
	result, err := w.interventionController.Intervene(ctx, report)
	if err != nil {
		return fmt.Errorf("intervention failed: %w", err)
	}

	fmt.Printf("Watchdog: Context exhaustion intervention completed: %s\n", result.Message)
	return nil
}

// checkAnomalies runs general anomaly detection on telemetry
func (w *Watchdog) checkAnomalies(ctx context.Context) error {
	// Run AI-driven anomaly detection
	report, err := w.analyzer.DetectAnomalies(ctx)
	if err != nil {
		return fmt.Errorf("anomaly detection failed: %w", err)
	}

	// If no anomaly detected, nothing to do
	if !report.Detected {
		return nil
	}

	// Check if we should intervene based on config
	if !w.config.ShouldIntervene(report) {
		// Log but don't intervene
		if w.config.AIConfig.EnableAnomalyLogging {
			fmt.Printf("Watchdog: Anomaly detected (%s) but below intervention threshold\n", report.AnomalyType)
		}
		return nil
	}

	// Intervene
	result, err := w.interventionController.Intervene(ctx, report)
	if err != nil {
		return fmt.Errorf("intervention failed: %w", err)
	}

	fmt.Printf("Watchdog: Intervention completed: %s\n", result.Message)
	return nil
}

// ParseAgentOutput parses agent output for events (context usage, errors, etc.)
// This should be called periodically as the agent produces output
func (w *Watchdog) ParseAgentOutput(ctx context.Context, output string, issueID, executorID, agentID string) error {
	// Parse for context usage
	detected, err := w.contextDetector.ParseAgentOutput(ctx, output, issueID, executorID, agentID)
	if err != nil {
		return fmt.Errorf("failed to parse agent output: %w", err)
	}

	if detected {
		// Context usage was detected and recorded
		metrics := w.contextDetector.GetMetrics()
		if metrics.IsExhausting {
			fmt.Printf("Watchdog: Context usage at %.1f%% (WARNING: approaching exhaustion)\n", metrics.CurrentUsagePercent)
		}
	}

	return nil
}

// SetAgentContext registers the cancel function for the current agent
// This should be called when an agent starts executing
func (w *Watchdog) SetAgentContext(issueID string, cancelFunc context.CancelFunc) {
	w.interventionController.SetAgentContext(issueID, cancelFunc)
}

// ClearAgentContext clears the current agent context
// This should be called when an agent completes
func (w *Watchdog) ClearAgentContext() {
	w.interventionController.ClearAgentContext()
}

// GetMonitor returns the telemetry monitor
func (w *Watchdog) GetMonitor() *Monitor {
	return w.monitor
}

// GetContextMetrics returns current context usage metrics
func (w *Watchdog) GetContextMetrics() ContextMetricsSnapshot {
	return w.contextDetector.GetMetrics()
}

// GetInterventionHistory returns recent intervention results
func (w *Watchdog) GetInterventionHistory() []InterventionResult {
	return w.interventionController.GetInterventionHistory()
}
