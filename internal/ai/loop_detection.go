package ai

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/steveyegge/vc/internal/events"
)

// LoopDetectionResult represents the AI's analysis of executor activity
type LoopDetectionResult struct {
	// ShouldHalt indicates whether the executor should stop
	ShouldHalt bool `json:"should_halt"`
	// Confidence is the AI's confidence in this decision (0.0 to 1.0)
	Confidence float64 `json:"confidence"`
	// Reasoning explains why the AI made this decision
	Reasoning string `json:"reasoning"`
	// LoopType categorizes the loop (e.g., "baseline_stagnation", "preflight_thrashing", "no_progress")
	LoopType string `json:"loop_type"`
	// DiagnosticSummary is a brief summary for the diagnostic issue
	DiagnosticSummary string `json:"diagnostic_summary"`
}

// DetectLoop analyzes recent executor activity to determine if it's stuck in an unproductive loop.
// This follows Zero Framework Cognition (ZFC) - the AI makes all decisions about what constitutes a loop.
//
// Examples of loops the AI might detect:
// - "No baseline issues ready" repeating indefinitely
// - Preflight checks cycling without claiming work
// - Watchdog anomalies repeating without resolution
// - No progress events (agent_completed, issue_claimed) for extended period
//
// vc-0vfg: Activity feed loop detector with AI-driven analysis
func (s *Supervisor) DetectLoop(ctx context.Context, recentEvents []*events.AgentEvent) (*LoopDetectionResult, error) {
	// Build event summary for AI analysis
	eventSummary := buildEventSummary(recentEvents)

	// Construct prompt for AI
	systemPrompt := `You are an expert at analyzing executor behavior patterns to detect unproductive loops.

Your task: Analyze recent activity feed events and determine if the executor is stuck in an unproductive loop that requires intervention.

**What is an unproductive loop?**
- Executor repeatedly performs the same actions without making progress toward completing work
- System enters a state where it cannot escape without external intervention
- Resources are being consumed with no forward movement on issues

**Common loop patterns:**
1. **Baseline stagnation**: "No baseline issues ready" repeating endlessly (baseline failed, no fix issues being claimed)
2. **Preflight thrashing**: Preflight checks running constantly but no work being claimed
3. **No progress**: No agent completions or issue claims for extended period despite activity
4. **Watchdog anomalies**: Watchdog alerts repeating without resolution
5. **Self-healing failure**: Stuck in self-healing mode without fixing baseline

**Not a loop (false positives to avoid):**
- Executor is idle because there genuinely is no ready work (normal operation)
- Executor is actively working on a long-running issue (progress events visible)
- System is recovering from a transient error (pattern will break soon)
- Low activity during off-hours or after work completion

**Response format:**
{
  "should_halt": true/false,
  "confidence": 0.0-1.0,
  "reasoning": "Clear explanation of why this is/isn't a loop",
  "loop_type": "baseline_stagnation|preflight_thrashing|no_progress|watchdog_loop|self_healing_failure|none",
  "diagnostic_summary": "Brief 1-2 sentence summary for diagnostic issue"
}

**Decision criteria:**
- Only recommend halt if you're highly confident (>0.8) it's a genuine loop
- Look for repetitive patterns with NO interleaved progress events
- Consider time duration - legitimate long-running work vs. stuck state
- Err on the side of caution - false positives are costly (unnecessary executor restarts)`

	userPrompt := fmt.Sprintf(`Analyze these recent executor events and determine if the executor is stuck in an unproductive loop.

**Time range:** Last %d minutes
**Total events:** %d

%s

Is the executor stuck in an unproductive loop that requires halting?`,
		int(time.Since(recentEvents[0].Timestamp).Minutes()),
		len(recentEvents),
		eventSummary)

	// Full prompt combining system and user
	fullPrompt := fmt.Sprintf("%s\n\n---\n\n%s", systemPrompt, userPrompt)

	// Make API call with retry logic
	var response *anthropic.Message
	err := s.retryWithBackoff(ctx, "loop-detection", func(attemptCtx context.Context) error {
		resp, apiErr := s.client.Messages.New(attemptCtx, anthropic.MessageNewParams{
			Model:     anthropic.Model(s.model),
			MaxTokens: 2000,
			Messages: []anthropic.MessageParam{
				anthropic.NewUserMessage(anthropic.NewTextBlock(fullPrompt)),
			},
		})
		if apiErr != nil {
			return apiErr
		}
		response = resp
		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("anthropic API call failed: %w", err)
	}

	// Extract text from response
	var responseText string
	for _, block := range response.Content {
		if block.Type == "text" {
			responseText += block.Text
		}
	}

	// Parse JSON response using resilient parser
	parseResult := Parse[LoopDetectionResult](responseText, ParseOptions{
		Context:   "loop detection response",
		LogErrors: boolPtr(true),
	})
	if !parseResult.Success {
		return nil, fmt.Errorf("failed to parse loop detection response: %s (response: %s)", parseResult.Error, safeTruncateString(responseText, 200))
	}

	loopResult := parseResult.Data

	// Log AI usage to events
	if err := s.recordAIUsage(ctx, "SYSTEM", "loop-detection", response.Usage.InputTokens, response.Usage.OutputTokens, time.Since(time.Now())); err != nil {
		fmt.Fprintf(os.Stderr, "warning: failed to log AI usage: %v\n", err)
	}

	return &loopResult, nil
}

// buildEventSummary creates a concise summary of events for AI analysis
func buildEventSummary(recentEvents []*events.AgentEvent) string {
	if len(recentEvents) == 0 {
		return "No events in time window."
	}

	// Build event frequency histogram
	eventCounts := make(map[events.EventType]int)
	severityCounts := make(map[events.EventSeverity]int)
	for _, event := range recentEvents {
		eventCounts[event.Type]++
		severityCounts[event.Severity]++
	}

	summary := "## Event Summary\n\n"
	summary += fmt.Sprintf("**Time range:** %s to %s\n",
		recentEvents[0].Timestamp.Format("15:04:05"),
		recentEvents[len(recentEvents)-1].Timestamp.Format("15:04:05"))
	summary += fmt.Sprintf("**Total events:** %d\n\n", len(recentEvents))

	// Event type frequencies
	summary += "**Event types:**\n"
	for eventType, count := range eventCounts {
		summary += fmt.Sprintf("- %s: %d\n", eventType, count)
	}

	// Severity distribution
	summary += "\n**Severity distribution:**\n"
	for severity, count := range severityCounts {
		summary += fmt.Sprintf("- %s: %d\n", severity, count)
	}

	// Sample of recent events (last 30)
	sampleCount := 30
	if len(recentEvents) < sampleCount {
		sampleCount = len(recentEvents)
	}
	sample := recentEvents[len(recentEvents)-sampleCount:]

	summary += fmt.Sprintf("\n**Recent events (last %d):**\n", sampleCount)
	for i, event := range sample {
		summary += fmt.Sprintf("%d. [%s] %s (%s): %s\n",
			i+1,
			event.Timestamp.Format("15:04:05"),
			event.Type,
			event.Severity,
			truncateString(event.Message, 100))
	}

	return summary
}
