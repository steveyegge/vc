package watchdog

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/steveyegge/vc/internal/ai"
	"github.com/steveyegge/vc/internal/storage"
)

// AnomalyType categorizes the type of anomaly detected
type AnomalyType string

const (
	AnomalyInfiniteLoop      AnomalyType = "infinite_loop"      // Issue keeps re-executing without progress
	AnomalyThrashing         AnomalyType = "thrashing"          // Rapid state changes without completion
	AnomalyStuckState        AnomalyType = "stuck_state"        // Issue stuck in specific state for too long
	AnomalyRegression        AnomalyType = "regression"         // Pattern of failures after previous successes
	AnomalyResourceSpike     AnomalyType = "resource_spike"     // Unusual resource usage pattern
	AnomalyContextExhaustion AnomalyType = "context_exhaustion" // Context usage approaching limit
	AnomalyOther             AnomalyType = "other"              // Other anomalous behavior
)

// AnomalySeverity indicates how critical an anomaly is
type AnomalySeverity string

const (
	SeverityCritical AnomalySeverity = "critical" // Requires immediate intervention
	SeverityHigh     AnomalySeverity = "high"     // Should be addressed soon
	SeverityMedium   AnomalySeverity = "medium"   // Notable but not urgent
	SeverityLow      AnomalySeverity = "low"      // Informational
)

// RecommendedAction suggests what should be done about an anomaly
type RecommendedAction string

const (
	ActionStopExecution RecommendedAction = "stop_execution"  // Halt the problematic issue execution
	ActionRestartAgent  RecommendedAction = "restart_agent"   // Restart the agent
	ActionMarkAsBlocked RecommendedAction = "mark_as_blocked" // Mark issue as blocked for human review
	ActionInvestigate   RecommendedAction = "investigate"     // Flag for investigation
	ActionMonitor       RecommendedAction = "monitor"         // Continue monitoring but no action yet
	ActionNotifyHuman   RecommendedAction = "notify_human"    // Alert a human operator
	ActionCheckpoint    RecommendedAction = "checkpoint"      // Request checkpoint and graceful termination
)

// AnomalyReport represents the result of anomaly detection analysis
type AnomalyReport struct {
	// Detected indicates whether any anomaly was detected
	Detected bool `json:"detected"`

	// AnomalyType categorizes the anomaly (if detected)
	AnomalyType AnomalyType `json:"anomaly_type,omitempty"`

	// Severity indicates how critical the anomaly is
	Severity AnomalySeverity `json:"severity,omitempty"`

	// Description provides details about what was detected
	Description string `json:"description"`

	// RecommendedAction suggests what should be done
	RecommendedAction RecommendedAction `json:"recommended_action,omitempty"`

	// Reasoning explains why this anomaly was detected
	Reasoning string `json:"reasoning"`

	// Confidence indicates how confident the AI is in this detection (0.0-1.0)
	Confidence float64 `json:"confidence"`

	// AffectedIssues lists issue IDs involved in the anomaly
	AffectedIssues []string `json:"affected_issues,omitempty"`

	// Metrics contains relevant metrics that contributed to detection
	Metrics map[string]interface{} `json:"metrics,omitempty"`
}

// Analyzer performs AI-driven behavioral analysis on execution telemetry
// It detects anomalous patterns without using hardcoded heuristics (ZFC compliant)
type Analyzer struct {
	monitor    *Monitor
	supervisor *ai.Supervisor
	// TODO(vc-170): store will be used to query historical events for richer context
	// Currently unused but required for future event-based anomaly correlation
	store storage.Storage
}

// AnalyzerConfig holds configuration for the analyzer
type AnalyzerConfig struct {
	Monitor    *Monitor
	Supervisor *ai.Supervisor
	Store      storage.Storage
}

// NewAnalyzer creates a new behavioral analyzer
func NewAnalyzer(cfg *AnalyzerConfig) (*Analyzer, error) {
	if cfg.Monitor == nil {
		return nil, fmt.Errorf("monitor is required")
	}
	if cfg.Supervisor == nil {
		return nil, fmt.Errorf("supervisor is required")
	}
	if cfg.Store == nil {
		return nil, fmt.Errorf("store is required")
	}

	return &Analyzer{
		monitor:    cfg.Monitor,
		supervisor: cfg.Supervisor,
		store:      cfg.Store,
	}, nil
}

// DetectAnomalies analyzes telemetry and event history to detect anomalous behavior
// This is pure ZFC - no hardcoded detection logic, all intelligence delegated to AI
func (a *Analyzer) DetectAnomalies(ctx context.Context) (*AnomalyReport, error) {
	startTime := time.Now()

	// Gather telemetry data
	telemetry := a.monitor.GetTelemetry()

	// Get current execution if any
	currentExecution := a.monitor.GetCurrentExecution()

	// If no data, nothing to analyze
	if len(telemetry) == 0 && currentExecution == nil {
		return &AnomalyReport{
			Detected:    false,
			Description: "No telemetry data available for analysis",
			Reasoning:   "Cannot detect anomalies without execution history",
			Confidence:  1.0,
		}, nil
	}

	// Build the analysis prompt with telemetry data
	prompt, err := a.buildAnomalyDetectionPrompt(telemetry, currentExecution)
	if err != nil {
		return nil, fmt.Errorf("failed to build analysis prompt: %w", err)
	}

	// Call AI supervisor for anomaly detection
	// We use the supervisor's internal retry logic for resilience
	report, err := a.callAISupervisor(ctx, prompt)
	if err != nil {
		return nil, fmt.Errorf("AI anomaly detection failed: %w", err)
	}

	duration := time.Since(startTime)

	// Log the detection result
	if report.Detected {
		fmt.Printf("Watchdog: Anomaly detected - type=%s, severity=%s, confidence=%.2f, duration=%v\n",
			report.AnomalyType, report.Severity, report.Confidence, duration)
	} else {
		fmt.Printf("Watchdog: No anomalies detected (analyzed %d executions, duration=%v)\n",
			len(telemetry), duration)
	}

	return report, nil
}

// buildAnomalyDetectionPrompt constructs the prompt for AI anomaly detection
//
//nolint:unparam // error return reserved for future error conditions
func (a *Analyzer) buildAnomalyDetectionPrompt(telemetry []*ExecutionTelemetry, current *ExecutionTelemetry) (string, error) {
	var prompt strings.Builder

	prompt.WriteString(`You are analyzing executor telemetry to detect anomalous behavioral patterns.

Your task is to identify patterns that indicate problems like:
- Infinite loops (issue keeps re-executing without progress)
- Thrashing (rapid state changes without completion)
- Stuck states (issue stuck in a state for unusually long)
- Regression patterns (failures after previous successes)
- Resource spikes or unusual resource usage
- Any other concerning patterns

IMPORTANT: Base your analysis on the DATA provided, not on hardcoded thresholds.

`)

	// Add historical telemetry
	if len(telemetry) > 0 {
		prompt.WriteString(fmt.Sprintf("HISTORICAL EXECUTIONS (%d total):\n", len(telemetry)))

		// Format telemetry for AI consumption
		for i, t := range telemetry {
			duration := t.EndTime.Sub(t.StartTime)
			prompt.WriteString(fmt.Sprintf("\nExecution %d:\n", i+1))
			prompt.WriteString(fmt.Sprintf("  Issue: %s\n", t.IssueID))
			prompt.WriteString(fmt.Sprintf("  Executor: %s\n", t.ExecutorID))
			// Add temporal context (vc-78): absolute timestamps + duration
			prompt.WriteString(fmt.Sprintf("  Started: %s\n", t.StartTime.Format(time.RFC3339)))
			prompt.WriteString(fmt.Sprintf("  Ended: %s\n", t.EndTime.Format(time.RFC3339)))
			prompt.WriteString(fmt.Sprintf("  Duration: %v\n", duration))
			prompt.WriteString(fmt.Sprintf("  Success: %v\n", t.Success))
			prompt.WriteString(fmt.Sprintf("  Gates Passed: %v\n", t.GatesPassed))

			// Add state transitions
			if len(t.StateTransitions) > 0 {
				prompt.WriteString(fmt.Sprintf("  State Transitions (%d):\n", len(t.StateTransitions)))
				for _, trans := range t.StateTransitions {
					prompt.WriteString(fmt.Sprintf("    %s -> %s\n", trans.From, trans.To))
				}
			}

			// Add event counts
			if len(t.EventCounts) > 0 {
				prompt.WriteString("  Events:\n")
				for eventType, count := range t.EventCounts {
					prompt.WriteString(fmt.Sprintf("    %s: %d\n", eventType, count))
				}
			}
		}
	}

	// Add current execution if any
	if current != nil {
		now := time.Now()
		duration := now.Sub(current.StartTime)
		prompt.WriteString("\nCURRENT EXECUTION (in progress):\n")
		prompt.WriteString(fmt.Sprintf("  Issue: %s\n", current.IssueID))
		prompt.WriteString(fmt.Sprintf("  Executor: %s\n", current.ExecutorID))
		// Add temporal context (vc-78): start time, current time, and running duration
		prompt.WriteString(fmt.Sprintf("  Started: %s\n", current.StartTime.Format(time.RFC3339)))
		prompt.WriteString(fmt.Sprintf("  Current time: %s\n", now.Format(time.RFC3339)))
		prompt.WriteString(fmt.Sprintf("  Running for: %v\n", duration))

		if len(current.StateTransitions) > 0 {
			prompt.WriteString(fmt.Sprintf("  State Transitions (%d):\n", len(current.StateTransitions)))
			lastTrans := current.StateTransitions[len(current.StateTransitions)-1]
			prompt.WriteString(fmt.Sprintf("    Current state: %s (entered %v ago)\n",
				lastTrans.To, time.Since(lastTrans.Timestamp)))
		}

		if len(current.EventCounts) > 0 {
			prompt.WriteString("  Events so far:\n")
			for eventType, count := range current.EventCounts {
				prompt.WriteString(fmt.Sprintf("    %s: %d\n", eventType, count))
			}

			// Highlight agent progress indicators (vc-125)
			// Tool usage indicates the agent is actively working, not stuck
			toolUseCount := current.EventCounts["agent_tool_use"]
			if toolUseCount > 0 {
				prompt.WriteString(fmt.Sprintf("\n  IMPORTANT: Agent has used tools %d times, indicating active work in progress.\n", toolUseCount))
				prompt.WriteString("  Tool usage (Read, Edit, Write, Bash, etc.) means the agent is actively executing, NOT stuck.\n")
				prompt.WriteString("  Periods without tool usage may indicate AI thinking/planning, which is normal.\n")
			}
		}
	}

	prompt.WriteString(`

ANALYSIS TASK:
Analyze this telemetry data and determine if there are any anomalous patterns.

Consider:
1. Are there issues being executed repeatedly without success?
2. Are execution times unusually long or getting longer?
3. Are there patterns of state transitions that suggest problems?
4. Are there event patterns that seem abnormal?
5. Is there evidence of thrashing, looping, or stuckness?
6. TEMPORAL PATTERNS: Use the timestamps to detect time-based anomalies:
   - Time-of-day patterns (failures at specific times)
   - Rate-based anomalies (too many executions in short window)
   - Execution gaps (unusual delays between retries)
   - Trends over time (getting slower/faster)
   - Burst detection (sudden spike in activity)
7. AGENT PROGRESS INDICATORS (vc-125): Before flagging as "stuck", consider:
   - agent_tool_use events indicate active work (Read, Edit, Write, Bash, etc.)
   - Periods without events may be normal AI thinking/planning time
   - An agent making API calls may take 10-30 seconds between tool uses
   - Only flag as stuck if BOTH: (a) no tool usage AND (b) excessive time in same state
   - Short executions (<5 minutes) with tool activity are NOT stuck

Provide your analysis as a JSON object:
{
  "detected": true/false,
  "anomaly_type": "infinite_loop|thrashing|stuck_state|regression|resource_spike|other",
  "severity": "critical|high|medium|low",
  "description": "Brief description of the anomaly",
  "recommended_action": "stop_execution|restart_agent|mark_as_blocked|investigate|monitor|notify_human",
  "reasoning": "Detailed explanation of what patterns led to this detection",
  "confidence": 0.85,
  "affected_issues": ["vc-123", "vc-456"],
  "metrics": {
    "key_metric_1": value,
    "key_metric_2": value
  }
}

Fields:
- detected: true if anomaly found, false if everything looks normal
- anomaly_type: only if detected=true
- severity: only if detected=true (how urgent is this?)
- description: concise summary (1-2 sentences)
- recommended_action: only if detected=true (what should be done?)
- reasoning: detailed explanation of your analysis
- confidence: how confident are you (0.0-1.0)
- affected_issues: list of issue IDs involved
- metrics: relevant metrics that contributed to detection

Be conservative - only flag real anomalies, not minor variations.
Be specific - include actual data points in your reasoning.

IMPORTANT: Respond with ONLY raw JSON. Do NOT wrap it in markdown code fences. Just the JSON object.
`)

	return prompt.String(), nil
}

// callAISupervisor sends the prompt to the AI supervisor and parses the response
func (a *Analyzer) callAISupervisor(ctx context.Context, prompt string) (*AnomalyReport, error) {
	responseText, err := a.callAIWithRetry(ctx, prompt)
	if err != nil {
		return nil, err
	}

	// Strip markdown code fences if present (Claude sometimes adds them despite instructions)
	responseText = strings.TrimSpace(responseText)
	if strings.HasPrefix(responseText, "```json") {
		responseText = strings.TrimPrefix(responseText, "```json")
		responseText = strings.TrimPrefix(responseText, "```")
		responseText = strings.TrimSuffix(responseText, "```")
		responseText = strings.TrimSpace(responseText)
	} else if strings.HasPrefix(responseText, "```") {
		responseText = strings.TrimPrefix(responseText, "```")
		responseText = strings.TrimSuffix(responseText, "```")
		responseText = strings.TrimSpace(responseText)
	}

	// Parse the response using AI's resilient JSON parser
	parseResult := ai.Parse[AnomalyReport](responseText, ai.ParseOptions{
		Context:   "anomaly detection response",
		LogErrors: ai.BoolPtr(true),
	})
	if !parseResult.Success {
		return nil, fmt.Errorf("failed to parse anomaly detection response: %s (response: %s)",
			parseResult.Error, responseText)
	}

	return &parseResult.Data, nil
}

// callAIWithRetry calls the AI API with the prompt using the supervisor's generic CallAI method
// This leverages the supervisor's retry logic and circuit breaker without code duplication
func (a *Analyzer) callAIWithRetry(ctx context.Context, prompt string) (string, error) {
	// Use supervisor's generic CallAI method
	// This provides retry logic, circuit breaker, and proper error handling
	responseText, err := a.supervisor.CallAI(ctx, prompt, "anomaly-detection", "claude-sonnet-4-5-20250929", 4096)
	if err != nil {
		return "", fmt.Errorf("AI anomaly detection API call failed: %w", err)
	}

	return responseText, nil
}

// GetTelemetrySummary returns a summary of recent telemetry for external consumers
func (a *Analyzer) GetTelemetrySummary() TelemetrySummary {
	telemetry := a.monitor.GetTelemetry()
	current := a.monitor.GetCurrentExecution()

	var totalExecutions, successCount, failureCount int
	var totalDuration time.Duration
	issueMap := make(map[string]int)

	for _, t := range telemetry {
		totalExecutions++
		if t.Success {
			successCount++
		} else {
			failureCount++
		}
		totalDuration += t.EndTime.Sub(t.StartTime)
		issueMap[t.IssueID]++
	}

	uniqueIssues := len(issueMap)
	var avgDuration time.Duration
	if totalExecutions > 0 {
		avgDuration = totalDuration / time.Duration(totalExecutions)
	}

	return TelemetrySummary{
		TotalExecutions: totalExecutions,
		SuccessCount:    successCount,
		FailureCount:    failureCount,
		UniqueIssues:    uniqueIssues,
		AverageDuration: avgDuration,
		IsExecuting:     current != nil,
	}
}

// TelemetrySummary provides high-level metrics about execution history
type TelemetrySummary struct {
	TotalExecutions int
	SuccessCount    int
	FailureCount    int
	UniqueIssues    int
	AverageDuration time.Duration
	IsExecuting     bool
}
