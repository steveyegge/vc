package executor

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/steveyegge/vc/internal/ai"
	"github.com/steveyegge/vc/internal/events"
	"github.com/steveyegge/vc/internal/storage"
	"github.com/steveyegge/vc/internal/types"
)

// LoopDetectorConfig holds configuration for the loop detector
type LoopDetectorConfig struct {
	// Enabled controls whether loop detection is active
	Enabled bool
	// CheckInterval is how often to sample and analyze activity feed (default: 30s)
	CheckInterval time.Duration
	// LookbackWindow is how far back to look in activity feed (default: 10m)
	LookbackWindow time.Duration
	// MinConfidenceThreshold is the minimum confidence required to halt (default: 0.8)
	MinConfidenceThreshold float64
}

// DefaultLoopDetectorConfig returns sensible defaults
func DefaultLoopDetectorConfig() *LoopDetectorConfig {
	return &LoopDetectorConfig{
		Enabled:                getEnvBool("VC_LOOP_DETECTOR_ENABLED", true),
		CheckInterval:          getEnvDuration("VC_LOOP_DETECTOR_CHECK_INTERVAL", 30*time.Second),
		LookbackWindow:         getEnvDuration("VC_LOOP_DETECTOR_LOOKBACK_WINDOW", 10*time.Minute),
		MinConfidenceThreshold: getEnvFloat("VC_LOOP_DETECTOR_MIN_CONFIDENCE", 0.8),
	}
}

// LoopDetector monitors the activity feed for unproductive loops
// and uses AI to decide when to halt the executor.
// This follows Zero Framework Cognition (ZFC) - no heuristics, AI makes all decisions.
type LoopDetector struct {
	config     *LoopDetectorConfig
	store      storage.Storage
	supervisor AISupervision
	instanceID string
	stopCh     chan struct{}
	doneCh     chan struct{}
}

// AISupervision defines the interface for AI-based loop detection analysis
// (subset of ai.Supervisor interface to avoid circular dependency)
type AISupervision interface {
	DetectLoop(ctx context.Context, recentEvents []*events.AgentEvent) (*ai.LoopDetectionResult, error)
}

// NewLoopDetector creates a new loop detector
func NewLoopDetector(config *LoopDetectorConfig, store storage.Storage, supervisor AISupervision, instanceID string) *LoopDetector {
	if config == nil {
		config = DefaultLoopDetectorConfig()
	}

	return &LoopDetector{
		config:     config,
		store:      store,
		supervisor: supervisor,
		instanceID: instanceID,
		stopCh:     make(chan struct{}),
		doneCh:     make(chan struct{}),
	}
}

// Start begins the loop detection background goroutine
func (ld *LoopDetector) Start(ctx context.Context) {
	if !ld.config.Enabled {
		close(ld.doneCh) // Signal immediate completion
		return
	}

	go ld.detectLoop(ctx)
}

// Stop gracefully stops the loop detector
func (ld *LoopDetector) Stop() {
	close(ld.stopCh)
	<-ld.doneCh // Wait for goroutine to finish
}

// detectLoop is the main loop that periodically samples activity and asks AI for analysis
func (ld *LoopDetector) detectLoop(ctx context.Context) {
	defer close(ld.doneCh)

	ticker := time.NewTicker(ld.config.CheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ld.stopCh:
			return
		case <-ticker.C:
			if err := ld.checkForLoop(ctx); err != nil {
				// Log error but continue monitoring
				fmt.Fprintf(os.Stderr, "Loop detector error: %v\n", err)
			}
		}
	}
}

// checkForLoop samples recent activity and asks AI if we're stuck in a loop
func (ld *LoopDetector) checkForLoop(ctx context.Context) error {
	// Fetch recent events from activity feed
	lookbackTime := time.Now().Add(-ld.config.LookbackWindow)
	recentEvents, err := ld.store.GetAgentEvents(ctx, events.EventFilter{
		AfterTime: lookbackTime,
		Limit:     1000, // Cap at 1000 events to avoid memory issues
	})
	if err != nil {
		return fmt.Errorf("failed to fetch recent events: %w", err)
	}

	// Skip analysis if we don't have enough data yet
	if len(recentEvents) < 5 {
		return nil
	}

	// Ask AI to analyze the activity pattern
	result, err := ld.supervisor.DetectLoop(ctx, recentEvents)
	if err != nil {
		return fmt.Errorf("AI loop detection failed: %w", err)
	}

	// Check if AI recommends halting
	if result.ShouldHalt && result.Confidence >= ld.config.MinConfidenceThreshold {
		fmt.Printf("\n🚨 Loop detected by AI (confidence: %.2f): %s\n", result.Confidence, result.Reasoning)
		fmt.Printf("Loop type: %s\n", result.LoopType)

		// Create diagnostic issue
		if err := ld.createDiagnosticIssue(ctx, result, recentEvents); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to create diagnostic issue: %v\n", err)
			// Continue with halt even if issue creation fails
		}

		// Trigger graceful shutdown with special exit code
		fmt.Printf("Initiating graceful halt (exit code 42)...\n")
		os.Exit(42) // Exit code 42 signals loop detection
	}

	return nil
}

// createDiagnosticIssue creates an escalation issue with diagnostic information
func (ld *LoopDetector) createDiagnosticIssue(ctx context.Context, result *ai.LoopDetectionResult, recentEvents []*events.AgentEvent) error {
	timestamp := time.Now().Format("2006-01-02-15-04-05")
	issueID := fmt.Sprintf("vc-loop-%s", timestamp)

	// Build diagnostic report
	diagnostic := ld.generateDiagnostic(result, recentEvents)

	issue := &types.Issue{
		ID:          issueID,
		Title:       fmt.Sprintf("Executor loop detected: %s", result.LoopType),
		Description: result.DiagnosticSummary,
		Design:      diagnostic,
		IssueType:   types.TypeBug,
		Status:      types.StatusOpen,
		Priority:    0, // P0 - critical
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}

	if err := ld.store.CreateIssue(ctx, issue, "loop-detector"); err != nil {
		return fmt.Errorf("failed to create diagnostic issue: %w", err)
	}

	// Add escalation label
	if err := ld.store.AddLabel(ctx, issueID, "escalation", "loop-detector"); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to add escalation label: %v\n", err)
	}

	fmt.Printf("✓ Created diagnostic issue: %s\n", issueID)
	return nil
}

// generateDiagnostic creates a detailed diagnostic report
func (ld *LoopDetector) generateDiagnostic(result *ai.LoopDetectionResult, recentEvents []*events.AgentEvent) string {
	// Event frequency histogram
	eventCounts := make(map[events.EventType]int)
	for _, event := range recentEvents {
		eventCounts[event.Type]++
	}

	// Serialize event frequency
	histogramJSON, _ := json.MarshalIndent(eventCounts, "", "  ")

	// Get last N events for context
	lastEventsCount := 50
	if len(recentEvents) < lastEventsCount {
		lastEventsCount = len(recentEvents)
	}
	lastEvents := recentEvents[len(recentEvents)-lastEventsCount:]

	// Build diagnostic report
	diagnostic := fmt.Sprintf(`## Loop Detection Diagnostic Report

**Detected At:** %s
**Executor Instance:** %s
**Loop Type:** %s
**AI Confidence:** %.2f

### AI Analysis

%s

### Event Frequency Histogram (last %v)

`+"```json"+`
%s
`+"```"+`

### Recent Events (last %d)

`, time.Now().Format(time.RFC3339), ld.instanceID, result.LoopType, result.Confidence,
		result.Reasoning, ld.config.LookbackWindow, string(histogramJSON), lastEventsCount)

	// Append last events
	for i, event := range lastEvents {
		diagnostic += fmt.Sprintf("%d. [%s] %s - %s: %s\n",
			i+1,
			event.Timestamp.Format("15:04:05"),
			event.Type,
			event.Severity,
			event.Message,
		)
	}

	diagnostic += fmt.Sprintf(`
### Recommendations

1. Review the event histogram above to identify repetitive patterns
2. Check if executor is stuck in self-healing mode without progress
3. Investigate baseline quality gates if preflight checks are cycling
4. Look for watchdog anomalies indicating deeper issues
5. Consider adjusting loop detector thresholds if this is a false positive

### Configuration

- Check Interval: %v
- Lookback Window: %v
- Min Confidence Threshold: %.2f
`, ld.config.CheckInterval, ld.config.LookbackWindow, ld.config.MinConfidenceThreshold)

	return diagnostic
}

// getEnvFloat retrieves a float from an environment variable, or returns the default value
func getEnvFloat(key string, defaultValue float64) float64 {
	if value := os.Getenv(key); value != "" {
		var floatValue float64
		if _, err := fmt.Sscanf(value, "%f", &floatValue); err == nil {
			return floatValue
		}
	}
	return defaultValue
}
