package executor

import (
	"encoding/json"
	"fmt"
	"os"
	"time"
)

// PolecatEventType identifies the type of event emitted during polecat execution
type PolecatEventType string

const (
	// PolecatEventStart indicates execution has started
	PolecatEventStart PolecatEventType = "start"

	// PolecatEventPreflight indicates preflight check results
	PolecatEventPreflight PolecatEventType = "preflight"

	// PolecatEventAssessment indicates AI assessment results
	PolecatEventAssessment PolecatEventType = "assessment"

	// PolecatEventAgentStart indicates agent execution has started
	PolecatEventAgentStart PolecatEventType = "agent_start"

	// PolecatEventAgentComplete indicates agent execution has completed
	PolecatEventAgentComplete PolecatEventType = "agent_complete"

	// PolecatEventIteration indicates an iteration has completed
	PolecatEventIteration PolecatEventType = "iteration"

	// PolecatEventGate indicates a quality gate result
	PolecatEventGate PolecatEventType = "gate"

	// PolecatEventAnalysis indicates AI analysis results
	PolecatEventAnalysis PolecatEventType = "analysis"

	// PolecatEventDiscovered indicates a discovered issue
	PolecatEventDiscovered PolecatEventType = "discovered"

	// PolecatEventError indicates an error occurred
	PolecatEventError PolecatEventType = "error"

	// PolecatEventWarning indicates a warning
	PolecatEventWarning PolecatEventType = "warning"

	// PolecatEventComplete indicates execution has completed
	PolecatEventComplete PolecatEventType = "complete"
)

// PolecatEvent represents an activity event emitted during polecat execution.
// These events are written to stderr as JSON lines for Gastown monitoring.
type PolecatEvent struct {
	// Type identifies the event type
	Type PolecatEventType `json:"type"`

	// Timestamp is when the event occurred
	Timestamp time.Time `json:"timestamp"`

	// Message is a human-readable description
	Message string `json:"message"`

	// Data contains event-specific details (optional)
	Data map[string]interface{} `json:"data,omitempty"`
}

// PolecatEventEmitter writes activity events to stderr as JSON lines.
// This provides real-time progress monitoring for Gastown integration.
// No database writes are made - events are ephemeral.
type PolecatEventEmitter struct {
	enabled bool
}

// NewPolecatEventEmitter creates a new event emitter for polecat mode
func NewPolecatEventEmitter(enabled bool) *PolecatEventEmitter {
	return &PolecatEventEmitter{enabled: enabled}
}

// Emit writes an event to stderr as a JSON line
func (e *PolecatEventEmitter) Emit(eventType PolecatEventType, message string, data map[string]interface{}) {
	if !e.enabled {
		return
	}

	event := PolecatEvent{
		Type:      eventType,
		Timestamp: time.Now(),
		Message:   message,
		Data:      data,
	}

	jsonBytes, err := json.Marshal(event)
	if err != nil {
		// Fall back to simple log if JSON marshaling fails
		fmt.Fprintf(os.Stderr, "[%s] %s\n", eventType, message)
		return
	}

	// Write as JSON line to stderr (not stdout - that's for final result)
	fmt.Fprintln(os.Stderr, string(jsonBytes))
}

// EmitStart emits a start event
func (e *PolecatEventEmitter) EmitStart(taskDescription string, source string, liteMode bool) {
	data := map[string]interface{}{
		"source":    source,
		"lite_mode": liteMode,
	}
	e.Emit(PolecatEventStart, fmt.Sprintf("Starting task: %s", truncateForLog(taskDescription, 80)), data)
}

// EmitPreflight emits a preflight check result event
func (e *PolecatEventEmitter) EmitPreflight(passed bool, gates map[string]bool) {
	data := map[string]interface{}{
		"passed": passed,
		"gates":  gates,
	}
	msg := "Preflight passed"
	if !passed {
		msg = "Preflight failed"
	}
	e.Emit(PolecatEventPreflight, msg, data)
}

// EmitAssessment emits an AI assessment result event
func (e *PolecatEventEmitter) EmitAssessment(strategy string, confidence float64, shouldDecompose bool) {
	data := map[string]interface{}{
		"strategy":          strategy,
		"confidence":        confidence,
		"should_decompose":  shouldDecompose,
	}
	e.Emit(PolecatEventAssessment, fmt.Sprintf("Assessment: %s (%.0f%% confidence)", strategy, confidence*100), data)
}

// EmitAgentStart emits an agent start event
func (e *PolecatEventEmitter) EmitAgentStart(iteration int) {
	data := map[string]interface{}{
		"iteration": iteration,
	}
	e.Emit(PolecatEventAgentStart, fmt.Sprintf("Starting agent execution (iteration %d)", iteration), data)
}

// EmitAgentComplete emits an agent completion event
func (e *PolecatEventEmitter) EmitAgentComplete(iteration int, success bool, filesModified []string) {
	data := map[string]interface{}{
		"iteration":      iteration,
		"success":        success,
		"files_modified": filesModified,
	}
	msg := fmt.Sprintf("Agent execution complete (iteration %d)", iteration)
	if !success {
		msg = fmt.Sprintf("Agent execution failed (iteration %d)", iteration)
	}
	e.Emit(PolecatEventAgentComplete, msg, data)
}

// EmitGate emits a quality gate result event
func (e *PolecatEventEmitter) EmitGate(gateName string, passed bool, output string) {
	data := map[string]interface{}{
		"gate":   gateName,
		"passed": passed,
	}
	if output != "" && len(output) < 500 {
		data["output"] = output
	}
	msg := fmt.Sprintf("Gate %s: passed", gateName)
	if !passed {
		msg = fmt.Sprintf("Gate %s: failed", gateName)
	}
	e.Emit(PolecatEventGate, msg, data)
}

// EmitAnalysis emits an AI analysis result event
func (e *PolecatEventEmitter) EmitAnalysis(completed bool, discoveredCount int, puntedCount int) {
	data := map[string]interface{}{
		"completed":        completed,
		"discovered_count": discoveredCount,
		"punted_count":     puntedCount,
	}
	e.Emit(PolecatEventAnalysis, fmt.Sprintf("Analysis: completed=%v, discovered=%d, punted=%d", completed, discoveredCount, puntedCount), data)
}

// EmitDiscovered emits a discovered issue event
func (e *PolecatEventEmitter) EmitDiscovered(title string, issueType string, priority int) {
	data := map[string]interface{}{
		"title":    title,
		"type":     issueType,
		"priority": priority,
	}
	e.Emit(PolecatEventDiscovered, fmt.Sprintf("Discovered: %s (P%d %s)", title, priority, issueType), data)
}

// EmitError emits an error event
func (e *PolecatEventEmitter) EmitError(err string) {
	data := map[string]interface{}{
		"error": err,
	}
	e.Emit(PolecatEventError, err, data)
}

// EmitWarning emits a warning event
func (e *PolecatEventEmitter) EmitWarning(warning string) {
	data := map[string]interface{}{
		"warning": warning,
	}
	e.Emit(PolecatEventWarning, warning, data)
}

// EmitComplete emits a completion event with final status
func (e *PolecatEventEmitter) EmitComplete(status string, success bool, durationSeconds float64) {
	data := map[string]interface{}{
		"status":           status,
		"success":          success,
		"duration_seconds": durationSeconds,
	}
	e.Emit(PolecatEventComplete, fmt.Sprintf("Execution complete: %s (%.1fs)", status, durationSeconds), data)
}
