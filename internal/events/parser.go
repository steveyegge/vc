package events

import (
	"fmt"
	"os"
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

	// Tool usage patterns (vc-129)
	readTool  *regexp.Regexp
	editTool  *regexp.Regexp
	writeTool *regexp.Regexp
	bashTool  *regexp.Regexp
	globTool  *regexp.Regexp
	grepTool  *regexp.Regexp
	taskTool  *regexp.Regexp
	toolUseGeneric *regexp.Regexp

	// Helper patterns for tool usage (vc-129)
	toolToPattern  *regexp.Regexp // Matches "tool to do something" pattern
	fileNamePattern *regexp.Regexp // Matches filename.ext patterns
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

		// Tool usage patterns (vc-129)
		// These match when Claude Code announces tool invocations
		// Note: File extraction is handled separately via extractFileName helper
		readTool:  regexp.MustCompile(`(?i)(?:use|using|invoke|invoking|call|calling|'ll use).*?\bRead\s+tool\b`),
		editTool:  regexp.MustCompile(`(?i)(?:use|using|invoke|invoking|call|calling|'ll use|'m going to use).*?\bEdit\s+tool\b`),
		writeTool: regexp.MustCompile(`(?i)(?:use|using|invoke|invoking|call|calling|'ll use).*?\bWrite\s+tool\b`),
		bashTool:  regexp.MustCompile(`(?i)(?:use|using|invoke|invoking|call|calling|run|running|execute|executing|'ll use).*?\bBash\s+tool\b`),
		globTool:  regexp.MustCompile(`(?i)(?:use|using|invoke|invoking|call|calling|'ll use).*?\bGlob\s+tool\b`),
		grepTool:  regexp.MustCompile(`(?i)(?:use|using|invoke|invoking|call|calling|'ll use).*?\bGrep\s+tool\b`),
		taskTool:  regexp.MustCompile(`(?i)(?:use|using|invoke|invoking|call|calling|launch|launching|spawn|spawning|'ll use).*?\bTask\s+tool\b`),
		// Generic tool usage pattern as fallback
		toolUseGeneric: regexp.MustCompile(`(?i)(?:use|using|invoke|invoking|call|calling).*?\b([A-Z][a-zA-Z]+)\s+tool\b`),

		// Helper patterns (compiled once for performance)
		toolToPattern:  regexp.MustCompile(`\s+tool.*?\s+to\s+(.+?)(?:\.|$)`),
		fileNamePattern: regexp.MustCompile(`\b([a-zA-Z0-9_\-./]+\.[a-z0-9]+)\b`),
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

	// 1. Tool usage (very specific - vc-129)
	if event := p.tryParseToolUse(line); event != nil {
		return []*AgentEvent{event}
	}

	// 2. File modifications (very specific patterns)
	if event := p.tryParseFileModification(line); event != nil {
		return []*AgentEvent{event}
	}

	// 3. Git operations (specific commands)
	if event := p.tryParseGitOperation(line); event != nil {
		return []*AgentEvent{event}
	}

	// 4. Test results (specific test output)
	if event := p.tryParseTestResult(line); event != nil {
		return []*AgentEvent{event}
	}

	// 5. Lint output (more specific than general build/error)
	if event := p.tryParseLintOutput(line); event != nil {
		return []*AgentEvent{event}
	}

	// 6. Build output (can overlap with errors)
	if event := p.tryParseBuildOutput(line); event != nil {
		return []*AgentEvent{event}
	}

	// 7. Progress indicators
	if event := p.tryParseProgress(line); event != nil {
		return []*AgentEvent{event}
	}

	// 8. Generic errors (last, as they're broad)
	if event := p.tryParseError(line); event != nil {
		return []*AgentEvent{event}
	}

	return events
}

// tryParseToolUse attempts to parse agent tool usage events (vc-129).
func (p *OutputParser) tryParseToolUse(line string) *AgentEvent {
	var toolName string
	var description string

	// Try specific tools first
	if p.patterns.readTool.MatchString(line) {
		toolName = "Read"
		description = extractToolDescription(line, "read")
	} else if p.patterns.editTool.MatchString(line) {
		toolName = "Edit"
		description = extractToolDescription(line, "edit")
	} else if p.patterns.writeTool.MatchString(line) {
		toolName = "Write"
		description = extractToolDescription(line, "write")
	} else if p.patterns.bashTool.MatchString(line) {
		toolName = "Bash"
		description = extractToolDescription(line, "bash")
	} else if p.patterns.globTool.MatchString(line) {
		toolName = "Glob"
		description = extractToolDescription(line, "glob")
	} else if p.patterns.grepTool.MatchString(line) {
		toolName = "Grep"
		description = extractToolDescription(line, "grep")
	} else if p.patterns.taskTool.MatchString(line) {
		toolName = "Task"
		description = extractToolDescription(line, "task")
	} else if matches := p.patterns.toolUseGeneric.FindStringSubmatch(line); len(matches) > 1 {
		// Generic tool pattern matched
		toolName = matches[1]
		description = extractToolDescription(line, strings.ToLower(toolName))
	} else {
		return nil
	}

	// Try to extract filename from the line
	targetFile := p.extractFileName(line)

	event := &AgentEvent{
		ID:         uuid.New().String(),
		Type:       EventTypeAgentToolUse,
		Timestamp:  time.Now(),
		IssueID:    p.IssueID,
		ExecutorID: p.ExecutorID,
		AgentID:    p.AgentID,
		Severity:   SeverityInfo,
		Message:    line,
		SourceLine: p.LineNumber,
	}

	// Set the tool use data (log error but don't fail parsing)
	if err := event.SetAgentToolUseData(AgentToolUseData{
		ToolName:        toolName,
		ToolDescription: description,
		TargetFile:      targetFile,
	}); err != nil {
		// Log warning but continue - we still want to emit the event
		fmt.Fprintf(os.Stderr, "warning: failed to set tool use data for %s: %v\n", toolName, err)
	}

	return event
}

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

// extractToolDescription attempts to extract a description from a tool usage line (vc-129).
// It looks for the common pattern "tool to do something" and extracts "do something".
// Note: Uses pre-compiled regex from parser.patterns for performance.
func extractToolDescription(line, toolKeyword string) string {
	// Try to extract the part after "to" which usually describes the purpose
	// Note: We still need to compile a keyword-specific pattern here because
	// the keyword varies per tool. This is acceptable since it's only called
	// once per matched line (not in a tight loop).
	toPattern := regexp.MustCompile(`(?i)` + toolKeyword + `\s+tool.*?\s+to\s+(.+?)(?:\.|$)`)
	if matches := toPattern.FindStringSubmatch(line); len(matches) > 1 {
		desc := strings.TrimSpace(matches[1])
		// Limit length to keep descriptions concise
		if len(desc) > 100 {
			desc = desc[:97] + "..."
		}
		return desc
	}
	// If no "to" pattern, just return a truncated version of the line
	desc := strings.TrimSpace(line)
	if len(desc) > 100 {
		desc = desc[:97] + "..."
	}
	return desc
}

// extractFileName attempts to extract a filename from a tool usage line (vc-129).
// It looks for common file patterns (e.g., "read main.go", "update parser.go").
// Uses pre-compiled regex from parser.patterns for performance.
func (p *OutputParser) extractFileName(line string) string {
	// Find all matches and return the last one (usually the target file)
	matches := p.patterns.fileNamePattern.FindAllStringSubmatch(line, -1)
	if len(matches) > 0 {
		// Return the last match (most likely the target file)
		return matches[len(matches)-1][1]
	}

	return ""
}
