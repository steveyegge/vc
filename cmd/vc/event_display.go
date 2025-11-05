package main

import (
	"fmt"

	"github.com/fatih/color"
	"github.com/steveyegge/vc/internal/events"
)

// displayActivityEvent formats and prints a single event with consistent two-line format
func displayActivityEvent(event *events.AgentEvent) {
	// Filter out noisy system events that aren't interesting for monitoring
	if shouldSkipEvent(event) {
		return
	}

	// Get emoji and colors based on event type and severity
	emoji := getEventEmoji(event)
	severityColor := getSeverityColor(event.Severity)

	// Format timestamp
	timestamp := event.Timestamp.Format("15:04:05")

	// Color the issue ID
	issueColor := color.New(color.FgGreen)
	issueID := issueColor.Sprint(event.IssueID)

	// Color the event type
	typeColor := color.New(color.FgMagenta)
	eventType := typeColor.Sprint(event.Type)

	// Line 1: emoji + [timestamp] + issueID + event_type: message
	// Truncate message to fit mobile width (~80 chars total)
	maxMessageLen := 60 - len(event.IssueID) - len(string(event.Type))
	message := truncateString(event.Message, maxMessageLen)

	fmt.Printf("%s [%s] %s %s: %s\n",
		emoji,
		timestamp,
		issueID,
		eventType,
		severityColor.Sprint(message),
	)

	// Line 2: metadata fields (3-5 key fields, pipe-separated)
	metadata := extractEventMetadata(event)
	if len(metadata) > 0 {
		gray := color.New(color.FgHiBlack)
		fmt.Printf("  %s\n", gray.Sprint(metadata))
	} else {
		// Empty line to maintain two-line format
		fmt.Println()
	}
}

// getEventEmoji returns the appropriate emoji for each event type
func getEventEmoji(event *events.AgentEvent) string {
	// Event type-specific emojis (override severity-based icons)
	switch event.Type {
	case events.EventTypeAgentToolUse:
		return "🔧"
	case events.EventTypeIssueClaimed:
		return "📌"
	case events.EventTypeAssessmentStarted, events.EventTypeAssessmentCompleted:
		return "🔍"
	case events.EventTypeAgentSpawned:
		return "🚀"
	case events.EventTypeAgentCompleted:
		return "✅"
	case events.EventTypeAnalysisStarted, events.EventTypeAnalysisCompleted:
		return "🧠"
	case events.EventTypeQualityGatesStarted, events.EventTypeQualityGatesCompleted:
		return "🛡️"
	case events.EventTypeQualityGateFail:
		return "🚫"
	case events.EventTypeQualityGatePass:
		return "✨"
	case events.EventTypeTestRun:
		return "🧪"
	case events.EventTypeGitOperation:
		return "🌿"
	case events.EventTypeBuildOutput:
		return "🔨"
	case events.EventTypeLintOutput:
		return "🔎"
	case events.EventTypeFileModified:
		return "📝"
	case events.EventTypeDeduplicationBatchStarted, events.EventTypeDeduplicationBatchCompleted:
		return "🔀"
	case events.EventTypeDeduplicationDecision:
		return "🎯"
	case events.EventTypeBaselineTestFixStarted, events.EventTypeBaselineTestFixCompleted:
		return "🩹"
	case events.EventTypeTestFailureDiagnosis:
		return "🔬"
	case events.EventTypeExecutorSelfHealingMode:
		return "🏥"
	case events.EventTypeSandboxCreationStarted, events.EventTypeSandboxCreationCompleted:
		return "📦"
	case events.EventTypeSandboxCleanupStarted, events.EventTypeSandboxCleanupCompleted:
		return "🧹"
	case events.EventTypeMissionCreated:
		return "🎯"
	case events.EventTypeEpicCompleted:
		return "🏆"
	}

	// Fallback to severity-based icons
	switch event.Severity {
	case events.SeverityInfo:
		return "ℹ️"
	case events.SeverityWarning:
		return "⚠️"
	case events.SeverityError:
		return "❌"
	case events.SeverityCritical:
		return "🔥"
	default:
		return "•"
	}
}

// getSeverityColor returns the appropriate color for a severity level
func getSeverityColor(severity events.EventSeverity) *color.Color {
	switch severity {
	case events.SeverityInfo:
		return color.New(color.FgCyan)
	case events.SeverityWarning:
		return color.New(color.FgYellow)
	case events.SeverityError:
		return color.New(color.FgRed)
	case events.SeverityCritical:
		return color.New(color.FgRed, color.Bold)
	default:
		return color.New(color.FgWhite)
	}
}

// extractEventMetadata extracts 3-5 key metadata fields for each event type
// Returns a pipe-separated string of metadata, truncated to fit mobile width (~80 chars)
func extractEventMetadata(event *events.AgentEvent) string {
	var fields []string

	switch event.Type {
	case events.EventTypeAgentToolUse:
		// tool_use: tool_name | args | status
		toolName := getStringField(event.Data, "tool_name", "unknown")
		args := ""
		switch toolName {
		case "Read", "read":
			args = truncateString(getStringField(event.Data, "path", ""), 30)
		case "edit_file", "Edit":
			args = truncateString(getStringField(event.Data, "path", ""), 30)
		case "Bash", "bash":
			args = truncateString(getStringField(event.Data, "command", ""), 30)
		case "Grep", "grep":
			args = truncateString(getStringField(event.Data, "pattern", ""), 30)
		default:
			// Generic: try to find any useful arg
			if target, ok := event.Data["target_file"].(string); ok {
				args = truncateString(target, 30)
			} else if cmd, ok := event.Data["command"].(string); ok {
				args = truncateString(cmd, 30)
			}
		}
		fields = []string{toolName, args}

	case events.EventTypeAssessmentCompleted:
		// assessment: confidence | steps | risks
		confidence := fmt.Sprintf("%.0f%%", getFloatField(event.Data, "confidence", 0)*100)
		steps := fmt.Sprintf("%d steps", getIntField(event.Data, "step_count", 0))
		risks := fmt.Sprintf("%d risks", getIntField(event.Data, "risk_count", 0))
		fields = []string{confidence, steps, risks}

	case events.EventTypeQualityGatesCompleted, events.EventTypeQualityGateFail, events.EventTypeQualityGatePass:
		// quality_gates: result | failing_gate | duration
		result := getStringField(event.Data, "result", "unknown")
		failingGate := getStringField(event.Data, "failing_gate", "none")
		duration := formatDurationMs(getIntField(event.Data, "duration_ms", 0))
		fields = []string{result, failingGate, duration}

	case events.EventTypeIssueClaimed:
		// issue_claimed: priority | type | status
		priority := getStringField(event.Data, "priority", "unknown")
		issueType := getStringField(event.Data, "type", "unknown")
		status := getStringField(event.Data, "status", "claimed")
		fields = []string{priority, issueType, status}

	case events.EventTypeAgentCompleted:
		// agent_completed: duration | tools_used | files_modified
		duration := formatDurationMs(getIntField(event.Data, "duration_ms", 0))
		toolsUsed := fmt.Sprintf("%d tools", getIntField(event.Data, "tools_used", 0))
		filesModified := fmt.Sprintf("%d files", getIntField(event.Data, "files_modified", 0))
		fields = []string{duration, toolsUsed, filesModified}

	case events.EventTypeAnalysisCompleted:
		// analysis: issues_discovered | confidence | duration
		issuesDiscovered := fmt.Sprintf("%d issues", getIntField(event.Data, "issues_discovered", 0))
		confidence := fmt.Sprintf("%.0f%%", getFloatField(event.Data, "confidence", 0)*100)
		duration := formatDurationMs(getIntField(event.Data, "duration_ms", 0))
		fields = []string{issuesDiscovered, confidence, duration}

	case events.EventTypeTestRun:
		// test_run: passed | duration | test_name
		passed := "✓ passed"
		if !getBoolField(event.Data, "passed", false) {
			passed = "✗ failed"
		}
		duration := formatDurationMs(getIntField(event.Data, "duration_ms", 0))
		testName := truncateString(getStringField(event.Data, "test_name", ""), 25)
		fields = []string{passed, duration, testName}

	case events.EventTypeGitOperation:
		// git_operation: command | success | args
		command := getStringField(event.Data, "command", "git")
		success := "✓"
		if !getBoolField(event.Data, "success", true) {
			success = "✗"
		}
		args := truncateString(getStringField(event.Data, "args", ""), 30)
		fields = []string{command, success, args}

	case events.EventTypeDeduplicationBatchCompleted:
		// deduplication: unique | duplicates | comparisons | duration
		unique := fmt.Sprintf("%d unique", getIntField(event.Data, "unique_count", 0))
		duplicates := fmt.Sprintf("%d dupes", getIntField(event.Data, "duplicate_count", 0))
		comparisons := fmt.Sprintf("%d comps", getIntField(event.Data, "comparisons_made", 0))
		duration := formatDurationMs(getIntField(event.Data, "processing_time_ms", 0))
		fields = []string{unique, duplicates, comparisons, duration}

	case events.EventTypeDeduplicationDecision:
		// deduplication_decision: is_duplicate | confidence | duplicate_of
		isDupe := "unique"
		if getBoolField(event.Data, "is_duplicate", false) {
			isDupe = "duplicate"
		}
		confidence := fmt.Sprintf("%.0f%%", getFloatField(event.Data, "confidence", 0)*100)
		dupeOf := truncateString(getStringField(event.Data, "duplicate_of", "n/a"), 15)
		fields = []string{isDupe, confidence, dupeOf}

	case events.EventTypeBaselineTestFixCompleted:
		// baseline_fix: fix_type | success | tests_fixed | duration
		fixType := getStringField(event.Data, "fix_type", "unknown")
		success := "✓"
		if !getBoolField(event.Data, "success", false) {
			success = "✗"
		}
		testsFixed := fmt.Sprintf("%d tests", getIntField(event.Data, "tests_fixed", 0))
		duration := formatDurationMs(getIntField(event.Data, "processing_time_ms", 0))
		fields = []string{fixType, success, testsFixed, duration}

	case events.EventTypeTestFailureDiagnosis:
		// test_failure_diagnosis: failure_type | confidence | root_cause
		failureType := getStringField(event.Data, "failure_type", "unknown")
		confidence := fmt.Sprintf("%.0f%%", getFloatField(event.Data, "confidence", 0)*100)
		rootCause := truncateString(getStringField(event.Data, "root_cause", ""), 30)
		fields = []string{failureType, confidence, rootCause}

	case events.EventTypeError:
		// error: error_type | context
		errorType := getStringField(event.Data, "error_type", "unknown")
		context := truncateString(getStringField(event.Data, "context", ""), 40)
		fields = []string{errorType, context}

	case events.EventTypeSandboxCreationCompleted, events.EventTypeSandboxCleanupCompleted:
		// sandbox: branch_name | duration | success
		branchName := truncateString(getStringField(event.Data, "branch_name", ""), 25)
		duration := formatDurationMs(getIntField(event.Data, "duration_ms", 0))
		success := "✓"
		if !getBoolField(event.Data, "success", true) {
			success = "✗"
		}
		fields = []string{branchName, duration, success}

	case events.EventTypeMissionCreated:
		// mission_created: phase_count | approval_required | actor
		phaseCount := fmt.Sprintf("%d phases", getIntField(event.Data, "phase_count", 0))
		approvalReq := "no approval"
		if getBoolField(event.Data, "approval_required", false) {
			approvalReq = "approval needed"
		}
		actor := truncateString(getStringField(event.Data, "actor", ""), 20)
		fields = []string{phaseCount, approvalReq, actor}

	case events.EventTypeEpicCompleted:
		// epic_completed: children_completed | completion_method | confidence
		childrenDone := fmt.Sprintf("%d children", getIntField(event.Data, "children_completed", 0))
		method := getStringField(event.Data, "completion_method", "unknown")
		confidence := fmt.Sprintf("%.0f%%", getFloatField(event.Data, "confidence", 0)*100)
		fields = []string{childrenDone, method, confidence}

	default:
		// Generic fallback: try to extract some useful fields
		if err, ok := event.Data["error"].(string); ok {
			fields = append(fields, truncateString(err, 50))
		}
		if duration := getIntField(event.Data, "duration_ms", 0); duration > 0 {
			fields = append(fields, formatDurationMs(duration))
		}
		if confidence := getFloatField(event.Data, "confidence", -1); confidence >= 0 {
			fields = append(fields, fmt.Sprintf("%.0f%%", confidence*100))
		}
	}

	// Join fields with " | " separator
	if len(fields) == 0 {
		return ""
	}

	// Truncate total metadata to fit mobile width (~70 chars for metadata line)
	metadata := truncateString(joinFields(fields), 70)
	return metadata
}

// Helper functions to safely extract typed fields from event data
func getStringField(data map[string]interface{}, key, defaultValue string) string {
	if val, ok := data[key].(string); ok {
		return val
	}
	return defaultValue
}

func getIntField(data map[string]interface{}, key string, defaultValue int) int {
	if val, ok := data[key].(int); ok {
		return val
	}
	if val, ok := data[key].(float64); ok {
		return int(val)
	}
	return defaultValue
}

func getFloatField(data map[string]interface{}, key string, defaultValue float64) float64 {
	if val, ok := data[key].(float64); ok {
		return val
	}
	if val, ok := data[key].(int); ok {
		return float64(val)
	}
	return defaultValue
}

func getBoolField(data map[string]interface{}, key string, defaultValue bool) bool {
	if val, ok := data[key].(bool); ok {
		return val
	}
	return defaultValue
}

// formatDurationMs formats milliseconds into a human-readable duration
func formatDurationMs(ms int) string {
	if ms < 1000 {
		return fmt.Sprintf("%dms", ms)
	}
	if ms < 60000 {
		return fmt.Sprintf("%.1fs", float64(ms)/1000)
	}
	return fmt.Sprintf("%.1fm", float64(ms)/60000)
}

// joinFields joins metadata fields with " | " separator
func joinFields(fields []string) string {
	// Filter out empty fields
	nonEmpty := make([]string, 0, len(fields))
	for _, f := range fields {
		if f != "" {
			nonEmpty = append(nonEmpty, f)
		}
	}
	if len(nonEmpty) == 0 {
		return ""
	}
	result := ""
	for i, f := range nonEmpty {
		if i > 0 {
			result += " | "
		}
		result += f
	}
	return result
}

// shouldSkipEvent returns true for noisy system events that clutter the feed
func shouldSkipEvent(event *events.AgentEvent) bool {
	// Skip routine system maintenance events
	noisyEvents := []string{
		"instance_cleanup_started",
		"instance_cleanup_completed",
		"watchdog_check",
		"health_check",
		"event_cleanup_started",
		"event_cleanup_completed",
		"pre_flight_check_started",
		"pre_flight_check_completed", // Too frequent (every 5s polling)
		"baseline_cache_miss",
		"baseline_cache_hit",
		"quality_gates_progress", // Keep started/completed, skip progress updates
		"results_processing_completed", // Redundant with issue_completed
	}

	eventTypeStr := string(event.Type)
	for _, noisy := range noisyEvents {
		if eventTypeStr == noisy {
			return true
		}
	}

	return false
}

// truncateString truncates a string to maxLen, adding "..." if needed
func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	if maxLen <= 3 {
		return "..."
	}
	return s[:maxLen-3] + "..."
}
