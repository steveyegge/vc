package watchdog

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/steveyegge/vc/internal/events"
)

// EventStorer is the minimal interface needed for storing context events
type EventStorer interface {
	StoreAgentEvent(ctx context.Context, event *events.AgentEvent) error
}

// ContextUsage represents a single context usage measurement
type ContextUsage struct {
	Timestamp      time.Time
	UsagePercent   float64 // 0-100
	TotalTokens    int     // Total context window size
	UsedTokens     int     // Tokens currently used
	RawMessage     string  // Original message from agent
	AgentType      string  // "amp" or "claude-code"
	IssueID        string
	ExecutorID     string
	AgentID        string
}

// ContextMetrics tracks context usage over time and calculates burn rate
type ContextMetrics struct {
	mu sync.RWMutex

	// History of context measurements (bounded window)
	history []ContextUsage
	maxHistory int // Maximum measurements to keep (default: 100)

	// Calculated metrics
	currentUsagePercent float64
	burnRate            float64 // Percent per minute
	estimatedExhaustion time.Time // When context will hit 100%
	isExhausting        bool      // True if approaching exhaustion threshold

	// Configuration
	exhaustionThreshold float64 // Usage % that triggers exhaustion (default: 80.0)
}

// ContextDetector parses agent output for context usage signals
// Supports both amp and claude-code output formats
type ContextDetector struct {
	store   EventStorer
	metrics *ContextMetrics

	// Configuration
	defaultContextWindow int // Default context window size when not detectable (default: 200000)

	// Regex patterns for parsing agent output
	ampPattern         *regexp.Regexp // amp shows: "Context: 45000/200000 (22.5%)"
	claudeCodePattern  *regexp.Regexp // claude-code shows: "approaching auto-compaction limit"
	claudeCodePercent  *regexp.Regexp // claude-code shows: "Token usage: 150000/200000"
}

// ContextDetectorConfig holds configuration for ContextDetector
type ContextDetectorConfig struct {
	ExhaustionThreshold  float64 // Usage % that triggers exhaustion (default: 80.0)
	DefaultContextWindow int     // Default context window when not detectable (default: 200000)
}

// NewContextDetector creates a new context usage detector
func NewContextDetector(store EventStorer) *ContextDetector {
	return NewContextDetectorWithConfig(store, nil)
}

// NewContextDetectorWithConfig creates a new context usage detector with custom config
func NewContextDetectorWithConfig(store EventStorer, config *ContextDetectorConfig) *ContextDetector {
	// Apply defaults
	exhaustionThreshold := 80.0
	defaultContextWindow := 200000

	if config != nil {
		if config.ExhaustionThreshold > 0 {
			exhaustionThreshold = config.ExhaustionThreshold
		}
		if config.DefaultContextWindow > 0 {
			defaultContextWindow = config.DefaultContextWindow
		}
	}

	return &ContextDetector{
		store: store,
		metrics: &ContextMetrics{
			history:             make([]ContextUsage, 0, 100),
			maxHistory:          100,
			exhaustionThreshold: exhaustionThreshold,
		},
		defaultContextWindow: defaultContextWindow,
		// amp format: "Context: 45000/200000 (22.5%)"
		ampPattern: regexp.MustCompile(`Context:\s*(\d+)/(\d+)\s*\((\d+(?:\.\d+)?)%\)`),
		// claude-code format: "Token usage: 150000/200000"
		claudeCodePercent: regexp.MustCompile(`Token usage:\s*(\d+)/(\d+)`),
		// claude-code warning: "approaching auto-compaction limit"
		claudeCodePattern: regexp.MustCompile(`approaching auto-compaction limit`),
	}
}

// ParseAgentOutput scans agent output for context usage signals
// Returns true if context usage was detected and recorded
func (cd *ContextDetector) ParseAgentOutput(ctx context.Context, output string, issueID, executorID, agentID string) (bool, error) {
	// Try amp format first
	if matches := cd.ampPattern.FindStringSubmatch(output); len(matches) == 4 {
		usedTokens, _ := strconv.Atoi(matches[1])
		totalTokens, _ := strconv.Atoi(matches[2])
		usagePercent, _ := strconv.ParseFloat(matches[3], 64)

		usage := ContextUsage{
			Timestamp:    time.Now(),
			UsagePercent: usagePercent,
			TotalTokens:  totalTokens,
			UsedTokens:   usedTokens,
			RawMessage:   matches[0],
			AgentType:    "amp",
			IssueID:      issueID,
			ExecutorID:   executorID,
			AgentID:      agentID,
		}

		// Record the measurement
		cd.recordUsage(usage)

		// Emit context_usage event (best effort - don't fail if event storage fails)
		if err := cd.emitContextEvent(ctx, usage); err != nil {
			fmt.Printf("Warning: failed to emit context event: %v\n", err)
		}

		return true, nil
	}

	// Try claude-code token usage format
	if matches := cd.claudeCodePercent.FindStringSubmatch(output); len(matches) == 3 {
		usedTokens, _ := strconv.Atoi(matches[1])
		totalTokens, _ := strconv.Atoi(matches[2])
		usagePercent := (float64(usedTokens) / float64(totalTokens)) * 100.0

		usage := ContextUsage{
			Timestamp:    time.Now(),
			UsagePercent: usagePercent,
			TotalTokens:  totalTokens,
			UsedTokens:   usedTokens,
			RawMessage:   matches[0],
			AgentType:    "claude-code",
			IssueID:      issueID,
			ExecutorID:   executorID,
			AgentID:      agentID,
		}

		cd.recordUsage(usage)

		// Emit context_usage event (best effort - don't fail if event storage fails)
		if err := cd.emitContextEvent(ctx, usage); err != nil {
			fmt.Printf("Warning: failed to emit context event: %v\n", err)
		}

		return true, nil
	}

	// Check for claude-code warning (estimate 85% when warning appears)
	if cd.claudeCodePattern.MatchString(output) {
		// Extract any token usage from surrounding context
		lines := strings.Split(output, "\n")
		for _, line := range lines {
			if strings.Contains(line, "approaching auto-compaction") {
				// Estimate 85% usage when this warning appears
				usage := ContextUsage{
					Timestamp:    time.Now(),
					UsagePercent: 85.0,
					TotalTokens:  cd.defaultContextWindow,
					UsedTokens:   int(0.85 * float64(cd.defaultContextWindow)),
					RawMessage:   line,
					AgentType:    "claude-code",
					IssueID:      issueID,
					ExecutorID:   executorID,
					AgentID:      agentID,
				}

				cd.recordUsage(usage)

				// Emit context_usage event (best effort - don't fail if event storage fails)
				if err := cd.emitContextEvent(ctx, usage); err != nil {
					fmt.Printf("Warning: failed to emit context event: %v\n", err)
				}

				return true, nil
			}
		}
	}

	return false, nil
}

// recordUsage adds a usage measurement to history and updates metrics
func (cd *ContextDetector) recordUsage(usage ContextUsage) {
	cd.metrics.mu.Lock()
	defer cd.metrics.mu.Unlock()

	// Add to history
	cd.metrics.history = append(cd.metrics.history, usage)

	// Enforce max history size (sliding window)
	if len(cd.metrics.history) > cd.metrics.maxHistory {
		cd.metrics.history = cd.metrics.history[len(cd.metrics.history)-cd.metrics.maxHistory:]
	}

	// Update current usage
	cd.metrics.currentUsagePercent = usage.UsagePercent

	// Calculate burn rate if we have enough history
	cd.calculateBurnRateLocked()

	// Check if approaching exhaustion
	cd.checkExhaustionLocked()
}

// calculateBurnRateLocked calculates context burn rate (% per minute)
// MUST be called with cd.metrics.mu held (write lock)
func (cd *ContextDetector) calculateBurnRateLocked() {
	if len(cd.metrics.history) < 2 {
		cd.metrics.burnRate = 0.0
		return
	}

	// Use first and last measurements for burn rate calculation
	first := cd.metrics.history[0]
	last := cd.metrics.history[len(cd.metrics.history)-1]

	// Calculate time elapsed in minutes
	elapsedMinutes := last.Timestamp.Sub(first.Timestamp).Minutes()
	// Use epsilon comparison to avoid division by zero or near-zero values
	// Less than 0.001 minutes (0.06 seconds) is too short for meaningful burn rate
	if elapsedMinutes < 0.001 {
		cd.metrics.burnRate = 0.0
		return
	}

	// Calculate percent change per minute
	percentChange := last.UsagePercent - first.UsagePercent

	// Handle negative burn rate (context compaction)
	// When context is compacted, usage decreases, giving negative burn rate
	// We clamp to 0 since negative burn rate doesn't help predict exhaustion
	if percentChange < 0 {
		cd.metrics.burnRate = 0.0
		return
	}

	cd.metrics.burnRate = percentChange / elapsedMinutes

	// Calculate estimated exhaustion time
	if cd.metrics.burnRate > 0 {
		percentRemaining := 100.0 - last.UsagePercent
		minutesToExhaustion := percentRemaining / cd.metrics.burnRate
		cd.metrics.estimatedExhaustion = last.Timestamp.Add(time.Duration(minutesToExhaustion) * time.Minute)
	}
}

// checkExhaustionLocked checks if context is approaching exhaustion threshold
// MUST be called with cd.metrics.mu held (write lock)
func (cd *ContextDetector) checkExhaustionLocked() {
	// Use configured threshold (default: 80% usage)
	cd.metrics.isExhausting = cd.metrics.currentUsagePercent >= cd.metrics.exhaustionThreshold
}

// GetMetrics returns current context metrics (thread-safe)
func (cd *ContextDetector) GetMetrics() ContextMetricsSnapshot {
	cd.metrics.mu.RLock()
	defer cd.metrics.mu.RUnlock()

	// Return snapshot
	return ContextMetricsSnapshot{
		CurrentUsagePercent: cd.metrics.currentUsagePercent,
		BurnRate:            cd.metrics.burnRate,
		EstimatedExhaustion: cd.metrics.estimatedExhaustion,
		IsExhausting:        cd.metrics.isExhausting,
		MeasurementCount:    len(cd.metrics.history),
		LatestMeasurement:   cd.getLatestMeasurementLocked(),
	}
}

// ContextMetricsSnapshot is a thread-safe snapshot of context metrics
type ContextMetricsSnapshot struct {
	CurrentUsagePercent float64
	BurnRate            float64 // Percent per minute
	EstimatedExhaustion time.Time
	IsExhausting        bool
	MeasurementCount    int
	LatestMeasurement   *ContextUsage
}

// getLatestMeasurementLocked returns the most recent context measurement
// MUST be called with cd.metrics.mu held (read or write lock)
func (cd *ContextDetector) getLatestMeasurementLocked() *ContextUsage {
	if len(cd.metrics.history) == 0 {
		return nil
	}
	latest := cd.metrics.history[len(cd.metrics.history)-1]
	return &latest
}

// emitContextEvent creates a context_usage event in the agent_events table
func (cd *ContextDetector) emitContextEvent(ctx context.Context, usage ContextUsage) error {
	// Determine severity based on usage percent
	severity := events.SeverityInfo
	if usage.UsagePercent >= 90 {
		severity = events.SeverityCritical
	} else if usage.UsagePercent >= 80 {
		severity = events.SeverityError
	} else if usage.UsagePercent >= 60 {
		severity = events.SeverityWarning
	}

	event := &events.AgentEvent{
		ID:         fmt.Sprintf("%s-context-%d", usage.AgentID, time.Now().UnixNano()),
		Type:       events.EventTypeContextUsage,
		Timestamp:  usage.Timestamp,
		IssueID:    usage.IssueID,
		ExecutorID: usage.ExecutorID,
		AgentID:    usage.AgentID,
		Severity:   severity,
		Message:    fmt.Sprintf("Context usage: %.1f%% (%d/%d tokens)", usage.UsagePercent, usage.UsedTokens, usage.TotalTokens),
		Data: map[string]interface{}{
			"usage_percent": usage.UsagePercent,
			"used_tokens":   usage.UsedTokens,
			"total_tokens":  usage.TotalTokens,
			"agent_type":    usage.AgentType,
			"raw_message":   usage.RawMessage,
		},
	}

	// Create the event
	if err := cd.store.StoreAgentEvent(ctx, event); err != nil {
		return fmt.Errorf("failed to create context_usage event: %w", err)
	}

	return nil
}

// Clear resets the detector state (useful for testing)
func (cd *ContextDetector) Clear() {
	cd.metrics.mu.Lock()
	defer cd.metrics.mu.Unlock()

	cd.metrics.history = make([]ContextUsage, 0, cd.metrics.maxHistory)
	cd.metrics.currentUsagePercent = 0.0
	cd.metrics.burnRate = 0.0
	cd.metrics.estimatedExhaustion = time.Time{}
	cd.metrics.isExhausting = false
}
