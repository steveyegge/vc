package events

import (
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
)

// OutputParser parses agent output lines and extracts structured events.
type OutputParser struct {
	// IssueID is the current issue being worked on
	IssueID string
	// ExecutorID is the current executor instance
	ExecutorID string
	// AgentID is the current agent
	AgentID string
	// LineNumber tracks the current line number in the output
	LineNumber int
	// multiLineBuffer stores lines for multi-line event detection
	multiLineBuffer []string
	// patterns holds compiled regex patterns for event detection
	patterns *eventPatterns
}

// eventPatterns holds compiled regex patterns for different event types.
type eventPatterns struct {
	// File modification patterns
	fileCreated  *regexp.Regexp
	fileModified *regexp.Regexp
	fileDeleted  *regexp.Regexp

	// Test result patterns
	testPass     *regexp.Regexp
	testFail     *regexp.Regexp
	testPassed   *regexp.Regexp
	testFailed   *regexp.Regexp
	testSummary  *regexp.Regexp

	// Git operation patterns
	gitAdd       *regexp.Regexp
	gitCommit    *regexp.Regexp
	gitRebase    *regexp.Regexp
	gitPush      *regexp.Regexp
	gitPull      *regexp.Regexp
	gitCheckout  *regexp.Regexp
	gitMerge     *regexp.Regexp
	gitGeneric   *regexp.Regexp

	// Build output patterns
	buildError   *regexp.Regexp
	buildWarning *regexp.Regexp
	buildSuccess *regexp.Regexp
	compileError *regexp.Regexp

	// Progress patterns
	stepProgress     *regexp.Regexp
	percentProgress  *regexp.Regexp
	processingStatus *regexp.Regexp

	// Error patterns
	errorGeneric *regexp.Regexp
	fatalError   *regexp.Regexp
	panic        *regexp.Regexp

	// Lint output patterns
	lintWarning *regexp.Regexp
	lintError   *regexp.Regexp

	// vc-236: Removed regex-based tool usage patterns
	// Tool usage now comes from structured JSON events (Amp --stream-json)
	// See agent.go convertJSONToEvent() for structured event parsing
}

// NewOutputParser creates a new OutputParser for the given execution context.
func NewOutputParser(issueID, executorID, agentID string) *OutputParser {
	return &OutputParser{
		IssueID:         issueID,
		ExecutorID:      executorID,
		AgentID:         agentID,
		LineNumber:      0,
		multiLineBuffer: make([]string, 0),
		patterns:        compilePatterns(),
	}
}

// compilePatterns compiles all regex patterns used for event detection.
func compilePatterns() *eventPatterns {
	return &eventPatterns{
		// File operations - case insensitive
		fileCreated:  regexp.MustCompile(`(?i)^(?:Created|Create|New file|Writing):\s+(.+?)(?:\s|$)`),
		fileModified: regexp.MustCompile(`(?i)^(?:Modified|Modify|Updated|Editing):\s+(.+?)(?:\s|$)`),
		fileDeleted:  regexp.MustCompile(`(?i)^(?:Deleted|Delete|Removed|Removing):\s+(.+?)(?:\s|$)`),

		// Test results
		testPass:    regexp.MustCompile(`(?i)\bPASS\b`),
		testFail:    regexp.MustCompile(`(?i)\bFAIL\b`),
		testPassed:  regexp.MustCompile(`(?i)(?:\d+\s+)?test[s]?\s+.*?\s+passed|.*?\s+tests?\s+passed`),
		testFailed:  regexp.MustCompile(`(?i)(?:\d+\s+)?test[s]?\s+.*?\s+failed|.*?\s+tests?\s+failed`),
		testSummary: regexp.MustCompile(`(?i)(\d+)\s+passed.*?(\d+)\s+failed`),

		// Git operations
		gitAdd:      regexp.MustCompile(`(?i)git\s+add\s+(.+)`),
		gitCommit:   regexp.MustCompile(`(?i)git\s+commit(?:\s+.*)?`),
		gitRebase:   regexp.MustCompile(`(?i)git\s+rebase(?:\s+.*)?`),
		gitPush:     regexp.MustCompile(`(?i)git\s+push(?:\s+.*)?`),
		gitPull:     regexp.MustCompile(`(?i)git\s+pull(?:\s+.*)?`),
		gitCheckout: regexp.MustCompile(`(?i)git\s+checkout\s+(.+)`),
		gitMerge:    regexp.MustCompile(`(?i)git\s+merge(?:\s+.*)?`),
		gitGeneric:  regexp.MustCompile(`(?i)git\s+(\w+)(?:\s+.*)?`),

		// Build output
		buildError:   regexp.MustCompile(`(?i)\berror\b.*?:\s*(.*)`),
		buildWarning: regexp.MustCompile(`(?i)\bwarning\b.*?:\s*(.*)`),
		buildSuccess: regexp.MustCompile(`(?i)(?:build|compilation)\s+(?:succeeded|successful|complete)`),
		compileError: regexp.MustCompile(`(?i)compilation\s+(?:failed|error)`),

		// Progress indicators
		stepProgress:     regexp.MustCompile(`(?i)step\s+(\d+)\s+of\s+(\d+)`),
		percentProgress:  regexp.MustCompile(`\[(\d+)%\]`),
		processingStatus: regexp.MustCompile(`(?i)(?:processing|analyzing|executing):\s*(.+)`),

		// Errors
		errorGeneric: regexp.MustCompile(`(?i)^(?:error|exception|failure):\s*(.*)`),
		fatalError:   regexp.MustCompile(`(?i)^fatal(?:\s+error)?:\s*(.*)`),
		panic:        regexp.MustCompile(`(?i)^panic:\s*(.*)`),

		// Lint output
		lintWarning: regexp.MustCompile(`(?i)(?:lint|linter).*?warning:\s*(.*)`),
		lintError:   regexp.MustCompile(`(?i)(?:lint|linter).*?error:\s*(.*)`),

		// vc-236: Removed regex-based tool usage patterns (ZFC violation)
		// Tool usage now comes from structured JSON events (Amp --stream-json)
		// See agent.go convertJSONToEvent() for structured event parsing
	}
}

// ParseLine parses a single line of output and returns any events detected.
// This supports real-time parsing as output arrives line-by-line.
// Pattern matching is exclusive - each line produces at most one event.
// Patterns are tried in priority order.
func (p *OutputParser) ParseLine(line string) []*AgentEvent {
	p.LineNumber++
	events := make([]*AgentEvent, 0)

	// Try to match against patterns in priority order
	// Most specific patterns first to avoid false positives

	// vc-236: Removed tryParseToolUse - tool usage comes from structured JSON events
	// Tool events are now parsed in agent.go convertJSONToEvent() from Amp's --stream-json

	// 1. File modifications (very specific patterns)
	if event := p.tryParseFileModification(line); event != nil {
		return []*AgentEvent{event}
	}

	// 2. Git operations (specific commands)
	if event := p.tryParseGitOperation(line); event != nil {
		return []*AgentEvent{event}
	}

	// 3. Test results (specific test output)
	if event := p.tryParseTestResult(line); event != nil {
		return []*AgentEvent{event}
	}

	// 4. Lint output (more specific than general build/error)
	if event := p.tryParseLintOutput(line); event != nil {
		return []*AgentEvent{event}
	}

	// 5. Build output (can overlap with errors)
	if event := p.tryParseBuildOutput(line); event != nil {
		return []*AgentEvent{event}
	}

	// 6. Progress indicators
	if event := p.tryParseProgress(line); event != nil {
		return []*AgentEvent{event}
	}

	// 7. Generic errors (last, as they're broad)
	if event := p.tryParseError(line); event != nil {
		return []*AgentEvent{event}
	}

	return events
}

// vc-236: tryParseToolUse removed - ZFC violation
// Tool usage events now come from structured JSON (Amp --stream-json)
// See agent.go convertJSONToEvent() for the replacement

// tryParseFileModification attempts to parse file modification events.
func (p *OutputParser) tryParseFileModification(line string) *AgentEvent {
	var operation string
	var filePath string

	if matches := p.patterns.fileCreated.FindStringSubmatch(line); len(matches) > 1 {
		operation = "created"
		filePath = strings.TrimSpace(matches[1])
	} else if matches := p.patterns.fileModified.FindStringSubmatch(line); len(matches) > 1 {
		operation = "modified"
		filePath = strings.TrimSpace(matches[1])
	} else if matches := p.patterns.fileDeleted.FindStringSubmatch(line); len(matches) > 1 {
		operation = "deleted"
		filePath = strings.TrimSpace(matches[1])
	} else {
		return nil
	}

	event := &AgentEvent{
		ID:         uuid.New().String(),
		Type:       EventTypeFileModified,
		Timestamp:  time.Now(),
		IssueID:    p.IssueID,
		ExecutorID: p.ExecutorID,
		AgentID:    p.AgentID,
		Severity:   SeverityInfo,
		Message:    line,
		SourceLine: p.LineNumber,
	}

	_ = event.SetFileModifiedData(FileModifiedData{
		FilePath:  filePath,
		Operation: operation,
	})

	return event
}

// tryParseTestResult attempts to parse test execution events.
func (p *OutputParser) tryParseTestResult(line string) *AgentEvent {
	var passed bool
	var detected bool

	if p.patterns.testPass.MatchString(line) || p.patterns.testPassed.MatchString(line) {
		passed = true
		detected = true
	} else if p.patterns.testFail.MatchString(line) || p.patterns.testFailed.MatchString(line) {
		passed = false
		detected = true
	}

	if !detected {
		return nil
	}

	severity := SeverityInfo
	if !passed {
		severity = SeverityError
	}

	event := &AgentEvent{
		ID:         uuid.New().String(),
		Type:       EventTypeTestRun,
		Timestamp:  time.Now(),
		IssueID:    p.IssueID,
		ExecutorID: p.ExecutorID,
		AgentID:    p.AgentID,
		Severity:   severity,
		Message:    line,
		SourceLine: p.LineNumber,
	}

	_ = event.SetTestRunData(TestRunData{
		TestName: extractTestName(line),
		Passed:   passed,
		Duration: 0, // Duration would need more context to extract
		Output:   line,
	})

	return event
}

// tryParseGitOperation attempts to parse git command events.
func (p *OutputParser) tryParseGitOperation(line string) *AgentEvent {
	var command string
	var args []string

	// Try specific git commands first
	if matches := p.patterns.gitAdd.FindStringSubmatch(line); len(matches) > 1 {
		command = "add"
		args = strings.Fields(matches[1])
	} else if p.patterns.gitCommit.MatchString(line) {
		command = "commit"
		args = extractGitArgs(line, "commit")
	} else if p.patterns.gitRebase.MatchString(line) {
		command = "rebase"
		args = extractGitArgs(line, "rebase")
	} else if p.patterns.gitPush.MatchString(line) {
		command = "push"
		args = extractGitArgs(line, "push")
	} else if p.patterns.gitPull.MatchString(line) {
		command = "pull"
		args = extractGitArgs(line, "pull")
	} else if matches := p.patterns.gitCheckout.FindStringSubmatch(line); len(matches) > 1 {
		command = "checkout"
		args = strings.Fields(matches[1])
	} else if p.patterns.gitMerge.MatchString(line) {
		command = "merge"
		args = extractGitArgs(line, "merge")
	} else if matches := p.patterns.gitGeneric.FindStringSubmatch(line); len(matches) > 1 {
		command = matches[1]
		args = extractGitArgs(line, command)
	} else {
		return nil
	}

	// Determine severity based on command
	severity := SeverityInfo
	if command == "push" || command == "rebase" || command == "merge" {
		severity = SeverityWarning // Higher risk operations
	}

	event := &AgentEvent{
		ID:         uuid.New().String(),
		Type:       EventTypeGitOperation,
		Timestamp:  time.Now(),
		IssueID:    p.IssueID,
		ExecutorID: p.ExecutorID,
		AgentID:    p.AgentID,
		Severity:   severity,
		Message:    line,
		SourceLine: p.LineNumber,
	}

	_ = event.SetGitOperationData(GitOperationData{
		Command: command,
		Args:    args,
		Success: true, // Assume success unless we detect failure in subsequent lines
	})

	return event
}

// tryParseBuildOutput attempts to parse build/compilation events.
func (p *OutputParser) tryParseBuildOutput(line string) *AgentEvent {
	var eventType EventType
	var severity EventSeverity
	var message string

	if matches := p.patterns.buildError.FindStringSubmatch(line); len(matches) > 1 {
		eventType = EventTypeBuildOutput
		severity = SeverityError
		message = matches[1]
	} else if matches := p.patterns.buildWarning.FindStringSubmatch(line); len(matches) > 1 {
		eventType = EventTypeBuildOutput
		severity = SeverityWarning
		message = matches[1]
	} else if p.patterns.buildSuccess.MatchString(line) {
		eventType = EventTypeBuildOutput
		severity = SeverityInfo
		message = "Build succeeded"
	} else if p.patterns.compileError.MatchString(line) {
		eventType = EventTypeBuildOutput
		severity = SeverityError
		message = "Compilation failed"
	} else {
		return nil
	}

	return &AgentEvent{
		ID:         uuid.New().String(),
		Type:       eventType,
		Timestamp:  time.Now(),
		IssueID:    p.IssueID,
		ExecutorID: p.ExecutorID,
		AgentID:    p.AgentID,
		Severity:   severity,
		Message:    message,
		Data:       map[string]interface{}{"raw_line": line},
		SourceLine: p.LineNumber,
	}
}

// tryParseProgress attempts to parse progress indicator events.
func (p *OutputParser) tryParseProgress(line string) *AgentEvent {
	var progressData map[string]interface{}

	if matches := p.patterns.stepProgress.FindStringSubmatch(line); len(matches) > 2 {
		progressData = map[string]interface{}{
			"current_step": matches[1],
			"total_steps":  matches[2],
			"type":         "step",
		}
	} else if matches := p.patterns.percentProgress.FindStringSubmatch(line); len(matches) > 1 {
		progressData = map[string]interface{}{
			"percent": matches[1],
			"type":    "percentage",
		}
	} else if matches := p.patterns.processingStatus.FindStringSubmatch(line); len(matches) > 1 {
		progressData = map[string]interface{}{
			"status": matches[1],
			"type":   "status",
		}
	} else {
		return nil
	}

	return &AgentEvent{
		ID:         uuid.New().String(),
		Type:       EventTypeProgress,
		Timestamp:  time.Now(),
		IssueID:    p.IssueID,
		ExecutorID: p.ExecutorID,
		AgentID:    p.AgentID,
		Severity:   SeverityInfo,
		Message:    line,
		Data:       progressData,
		SourceLine: p.LineNumber,
	}
}

// tryParseError attempts to parse error events.
func (p *OutputParser) tryParseError(line string) *AgentEvent {
	var severity EventSeverity
	var message string
	var detected bool

	if matches := p.patterns.fatalError.FindStringSubmatch(line); len(matches) > 1 {
		severity = SeverityCritical
		message = matches[1]
		detected = true
	} else if matches := p.patterns.panic.FindStringSubmatch(line); len(matches) > 1 {
		severity = SeverityCritical
		message = matches[1]
		detected = true
	} else if matches := p.patterns.errorGeneric.FindStringSubmatch(line); len(matches) > 1 {
		severity = SeverityError
		message = matches[1]
		detected = true
	}

	if !detected {
		return nil
	}

	return &AgentEvent{
		ID:         uuid.New().String(),
		Type:       EventTypeError,
		Timestamp:  time.Now(),
		IssueID:    p.IssueID,
		ExecutorID: p.ExecutorID,
		AgentID:    p.AgentID,
		Severity:   severity,
		Message:    message,
		Data:       map[string]interface{}{"raw_line": line},
		SourceLine: p.LineNumber,
	}
}

// tryParseLintOutput attempts to parse linter output events.
func (p *OutputParser) tryParseLintOutput(line string) *AgentEvent {
	var severity EventSeverity
	var message string
	var detected bool

	if matches := p.patterns.lintError.FindStringSubmatch(line); len(matches) > 1 {
		severity = SeverityError
		message = matches[1]
		detected = true
	} else if matches := p.patterns.lintWarning.FindStringSubmatch(line); len(matches) > 1 {
		severity = SeverityWarning
		message = matches[1]
		detected = true
	}

	if !detected {
		return nil
	}

	return &AgentEvent{
		ID:         uuid.New().String(),
		Type:       EventTypeLintOutput,
		Timestamp:  time.Now(),
		IssueID:    p.IssueID,
		ExecutorID: p.ExecutorID,
		AgentID:    p.AgentID,
		Severity:   severity,
		Message:    message,
		Data:       map[string]interface{}{"raw_line": line},
		SourceLine: p.LineNumber,
	}
}

// Helper functions

// extractTestName attempts to extract a test name from the line.
func extractTestName(line string) string {
	// Simple extraction - look for common test patterns
	// This is a basic implementation and could be enhanced
	testNamePattern := regexp.MustCompile(`(?i)test[:\s]+([^\s]+)`)
	if matches := testNamePattern.FindStringSubmatch(line); len(matches) > 1 {
		return matches[1]
	}
	return "unknown"
}

// extractGitArgs extracts arguments from a git command line.
func extractGitArgs(line, command string) []string {
	// Find the command and extract everything after it
	cmdPattern := regexp.MustCompile(`(?i)git\s+` + command + `\s+(.*)`)
	if matches := cmdPattern.FindStringSubmatch(line); len(matches) > 1 {
		return strings.Fields(matches[1])
	}
	return []string{}
}

// ParseLines parses multiple lines at once, useful for batch processing.
func (p *OutputParser) ParseLines(lines []string) []*AgentEvent {
	events := make([]*AgentEvent, 0)
	for _, line := range lines {
		events = append(events, p.ParseLine(line)...)
	}
	return events
}

// Reset resets the parser state for a new execution context.
func (p *OutputParser) Reset(issueID, executorID, agentID string) {
	p.IssueID = issueID
	p.ExecutorID = executorID
	p.AgentID = agentID
	p.LineNumber = 0
	p.multiLineBuffer = make([]string, 0)
}

// vc-236: extractToolDescription and extractFileName removed
// These were part of regex-based tool usage parsing (ZFC violation)
// Tool usage data now comes directly from structured JSON events
