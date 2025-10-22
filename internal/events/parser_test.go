package events

import (
	"testing"
)

func TestOutputParser_ParseFileModifications(t *testing.T) {
	parser := NewOutputParser("vc-123", "exec-1", "agent-1")

	tests := []struct {
		name          string
		line          string
		expectEvent   bool
		expectedOp    string
		expectedPath  string
		expectedType  EventType
	}{
		{
			name:         "Created file",
			line:         "Created: internal/parser/output.go",
			expectEvent:  true,
			expectedOp:   "created",
			expectedPath: "internal/parser/output.go",
			expectedType: EventTypeFileModified,
		},
		{
			name:         "Modified file",
			line:         "Modified: cmd/vc/main.go",
			expectEvent:  true,
			expectedOp:   "modified",
			expectedPath: "cmd/vc/main.go",
			expectedType: EventTypeFileModified,
		},
		{
			name:         "Deleted file",
			line:         "Deleted: old_file.txt",
			expectEvent:  true,
			expectedOp:   "deleted",
			expectedPath: "old_file.txt",
			expectedType: EventTypeFileModified,
		},
		{
			name:         "Create variation",
			line:         "Create: new_test.go",
			expectEvent:  true,
			expectedOp:   "created",
			expectedPath: "new_test.go",
			expectedType: EventTypeFileModified,
		},
		{
			name:         "Writing file",
			line:         "Writing: output.log",
			expectEvent:  true,
			expectedOp:   "created",
			expectedPath: "output.log",
			expectedType: EventTypeFileModified,
		},
		{
			name:        "Non-matching line",
			line:        "This is just regular output",
			expectEvent: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			events := parser.ParseLine(tt.line)

			if !tt.expectEvent {
				if len(events) > 0 {
					t.Errorf("expected no events but got %d", len(events))
				}
				return
			}

			if len(events) != 1 {
				t.Fatalf("expected 1 event but got %d", len(events))
			}

			event := events[0]
			if event.Type != tt.expectedType {
				t.Errorf("expected type %s but got %s", tt.expectedType, event.Type)
			}

			data, err := event.GetFileModifiedData()
			if err != nil {
				t.Fatalf("failed to get file modified data: %v", err)
			}

			if data.Operation != tt.expectedOp {
				t.Errorf("expected operation %s but got %s", tt.expectedOp, data.Operation)
			}

			if data.FilePath != tt.expectedPath {
				t.Errorf("expected path %s but got %s", tt.expectedPath, data.FilePath)
			}
		})
	}
}

func TestOutputParser_ParseTestResults(t *testing.T) {
	parser := NewOutputParser("vc-123", "exec-1", "agent-1")

	tests := []struct {
		name         string
		line         string
		expectEvent  bool
		expectedPass bool
		severity     EventSeverity
	}{
		{
			name:         "Test PASS",
			line:         "PASS: TestOutputParser",
			expectEvent:  true,
			expectedPass: true,
			severity:     SeverityInfo,
		},
		{
			name:         "Test FAIL",
			line:         "FAIL: TestParseError",
			expectEvent:  true,
			expectedPass: false,
			severity:     SeverityError,
		},
		{
			name:         "Tests passed message",
			line:         "3 tests passed successfully",
			expectEvent:  true,
			expectedPass: true,
			severity:     SeverityInfo,
		},
		{
			name:         "Test failed message",
			line:         "test suite failed with errors",
			expectEvent:  true,
			expectedPass: false,
			severity:     SeverityError,
		},
		{
			name:        "Non-test line",
			line:        "Compiling test files",
			expectEvent: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			events := parser.ParseLine(tt.line)

			if !tt.expectEvent {
				if len(events) > 0 {
					t.Errorf("expected no events but got %d", len(events))
				}
				return
			}

			if len(events) != 1 {
				t.Fatalf("expected 1 event but got %d", len(events))
			}

			event := events[0]
			if event.Type != EventTypeTestRun {
				t.Errorf("expected type %s but got %s", EventTypeTestRun, event.Type)
			}

			if event.Severity != tt.severity {
				t.Errorf("expected severity %s but got %s", tt.severity, event.Severity)
			}

			data, err := event.GetTestRunData()
			if err != nil {
				t.Fatalf("failed to get test run data: %v", err)
			}

			if data.Passed != tt.expectedPass {
				t.Errorf("expected passed=%v but got %v", tt.expectedPass, data.Passed)
			}
		})
	}
}

func TestOutputParser_ParseGitOperations(t *testing.T) {
	parser := NewOutputParser("vc-123", "exec-1", "agent-1")

	tests := []struct {
		name            string
		line            string
		expectEvent     bool
		expectedCommand string
		severity        EventSeverity
	}{
		{
			name:            "Git add",
			line:            "git add internal/events/parser.go",
			expectEvent:     true,
			expectedCommand: "add",
			severity:        SeverityInfo,
		},
		{
			name:            "Git commit",
			line:            "git commit -m 'Add parser'",
			expectEvent:     true,
			expectedCommand: "commit",
			severity:        SeverityInfo,
		},
		{
			name:            "Git push",
			line:            "git push origin main",
			expectEvent:     true,
			expectedCommand: "push",
			severity:        SeverityWarning,
		},
		{
			name:            "Git rebase",
			line:            "git rebase main",
			expectEvent:     true,
			expectedCommand: "rebase",
			severity:        SeverityWarning,
		},
		{
			name:            "Git checkout",
			line:            "git checkout feature-branch",
			expectEvent:     true,
			expectedCommand: "checkout",
			severity:        SeverityInfo,
		},
		{
			name:            "Git merge",
			line:            "git merge feature-branch",
			expectEvent:     true,
			expectedCommand: "merge",
			severity:        SeverityWarning,
		},
		{
			name:        "Non-git line",
			line:        "Building the project",
			expectEvent: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			events := parser.ParseLine(tt.line)

			if !tt.expectEvent {
				if len(events) > 0 {
					t.Errorf("expected no events but got %d", len(events))
				}
				return
			}

			if len(events) != 1 {
				t.Fatalf("expected 1 event but got %d", len(events))
			}

			event := events[0]
			if event.Type != EventTypeGitOperation {
				t.Errorf("expected type %s but got %s", EventTypeGitOperation, event.Type)
			}

			if event.Severity != tt.severity {
				t.Errorf("expected severity %s but got %s", tt.severity, event.Severity)
			}

			data, err := event.GetGitOperationData()
			if err != nil {
				t.Fatalf("failed to get git operation data: %v", err)
			}

			if data.Command != tt.expectedCommand {
				t.Errorf("expected command %s but got %s", tt.expectedCommand, data.Command)
			}
		})
	}
}

func TestOutputParser_ParseBuildOutput(t *testing.T) {
	parser := NewOutputParser("vc-123", "exec-1", "agent-1")

	tests := []struct {
		name     string
		line     string
		expect   bool
		severity EventSeverity
	}{
		{
			name:     "Build error",
			line:     "error: undefined reference to 'foo'",
			expect:   true,
			severity: SeverityError,
		},
		{
			name:     "Build warning",
			line:     "warning: unused variable 'x'",
			expect:   true,
			severity: SeverityWarning,
		},
		{
			name:     "Build success",
			line:     "Build succeeded",
			expect:   true,
			severity: SeverityInfo,
		},
		{
			name:     "Compilation successful",
			line:     "Compilation complete",
			expect:   true,
			severity: SeverityInfo,
		},
		{
			name:     "Compilation failed",
			line:     "Compilation failed",
			expect:   true,
			severity: SeverityError,
		},
		{
			name:   "Non-build line",
			line:   "Starting build process",
			expect: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			events := parser.ParseLine(tt.line)

			if !tt.expect {
				if len(events) > 0 {
					t.Errorf("expected no events but got %d", len(events))
				}
				return
			}

			if len(events) != 1 {
				t.Fatalf("expected 1 event but got %d", len(events))
			}

			event := events[0]
			if event.Type != EventTypeBuildOutput {
				t.Errorf("expected type %s but got %s", EventTypeBuildOutput, event.Type)
			}

			if event.Severity != tt.severity {
				t.Errorf("expected severity %s but got %s", tt.severity, event.Severity)
			}
		})
	}
}

func TestOutputParser_ParseProgress(t *testing.T) {
	parser := NewOutputParser("vc-123", "exec-1", "agent-1")

	tests := []struct {
		name   string
		line   string
		expect bool
	}{
		{
			name:   "Step progress",
			line:   "Step 3 of 10",
			expect: true,
		},
		{
			name:   "Percent progress",
			line:   "Processing... [75%]",
			expect: true,
		},
		{
			name:   "Processing status",
			line:   "Processing: data.json",
			expect: true,
		},
		{
			name:   "Analyzing status",
			line:   "Analyzing: source code",
			expect: true,
		},
		{
			name:   "Non-progress line",
			line:   "This is regular output",
			expect: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			events := parser.ParseLine(tt.line)

			if !tt.expect {
				if len(events) > 0 {
					t.Errorf("expected no events but got %d", len(events))
				}
				return
			}

			if len(events) != 1 {
				t.Fatalf("expected 1 event but got %d", len(events))
			}

			event := events[0]
			if event.Type != EventTypeProgress {
				t.Errorf("expected type %s but got %s", EventTypeProgress, event.Type)
			}

			if event.Severity != SeverityInfo {
				t.Errorf("expected severity %s but got %s", SeverityInfo, event.Severity)
			}

			if event.Data == nil {
				t.Error("expected progress data but got nil")
			}
		})
	}
}

func TestOutputParser_ParseErrors(t *testing.T) {
	parser := NewOutputParser("vc-123", "exec-1", "agent-1")

	tests := []struct {
		name          string
		line          string
		expect        bool
		expectedType  EventType
		severity      EventSeverity
	}{
		{
			name:         "Generic error - caught as build error",
			line:         "Error: file not found",
			expect:       true,
			expectedType: EventTypeBuildOutput, // Build errors come before generic errors in priority
			severity:     SeverityError,
		},
		{
			name:         "Fatal error at line start",
			line:         "fatal: repository not initialized",
			expect:       true,
			expectedType: EventTypeError,
			severity:     SeverityCritical,
		},
		{
			name:         "Fatal error with capital - caught as build error",
			line:         "Fatal Error: system crash",
			expect:       true,
			expectedType: EventTypeBuildOutput, // Contains "error" keyword so matches build pattern
			severity:     SeverityError,
		},
		{
			name:         "Panic",
			line:         "panic: runtime error",
			expect:       true,
			expectedType: EventTypeError,
			severity:     SeverityCritical,
		},
		{
			name:   "Non-error line",
			line:   "Everything is fine",
			expect: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			events := parser.ParseLine(tt.line)

			if !tt.expect {
				if len(events) > 0 {
					t.Errorf("expected no events but got %d", len(events))
				}
				return
			}

			if len(events) != 1 {
				t.Fatalf("expected 1 event but got %d", len(events))
			}

			event := events[0]
			if event.Type != tt.expectedType {
				t.Errorf("expected type %s but got %s", tt.expectedType, event.Type)
			}

			if event.Severity != tt.severity {
				t.Errorf("expected severity %s but got %s", tt.severity, event.Severity)
			}
		})
	}
}

func TestOutputParser_ParseLintOutput(t *testing.T) {
	parser := NewOutputParser("vc-123", "exec-1", "agent-1")

	tests := []struct {
		name     string
		line     string
		expect   bool
		severity EventSeverity
	}{
		{
			name:     "Lint warning",
			line:     "linter warning: variable 'x' is unused",
			expect:   true,
			severity: SeverityWarning,
		},
		{
			name:     "Lint error",
			line:     "linter error: syntax error on line 42",
			expect:   true,
			severity: SeverityError,
		},
		{
			name:     "Lint output warning",
			line:     "lint: warning: missing documentation",
			expect:   true,
			severity: SeverityWarning,
		},
		{
			name:   "Non-lint line",
			line:   "Running linter...",
			expect: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			events := parser.ParseLine(tt.line)

			if !tt.expect {
				if len(events) > 0 {
					t.Errorf("expected no events but got %d", len(events))
				}
				return
			}

			if len(events) != 1 {
				t.Fatalf("expected 1 event but got %d", len(events))
			}

			event := events[0]
			if event.Type != EventTypeLintOutput {
				t.Errorf("expected type %s but got %s", EventTypeLintOutput, event.Type)
			}

			if event.Severity != tt.severity {
				t.Errorf("expected severity %s but got %s", tt.severity, event.Severity)
			}
		})
	}
}

func TestOutputParser_ParseLines(t *testing.T) {
	parser := NewOutputParser("vc-123", "exec-1", "agent-1")

	lines := []string{
		"Starting build...",
		"Created: parser.go",
		"Modified: main.go",
		"git add .",
		"Build succeeded",
		"PASS: TestParser",
		"error: test failed",
		"Step 5 of 10",
	}

	events := parser.ParseLines(lines)

	// We expect: 1 file created, 1 file modified, 1 git op, 1 build success,
	// 1 test pass, 1 build error, 1 progress = 7 events
	expectedCount := 7
	if len(events) != expectedCount {
		t.Errorf("expected %d events but got %d", expectedCount, len(events))
	}

	// Verify event types
	eventTypes := make(map[EventType]int)
	for _, event := range events {
		eventTypes[event.Type]++
	}

	// With exclusive matching:
	// - "Created: parser.go" -> FileModified
	// - "Modified: main.go" -> FileModified
	// - "git add ." -> GitOperation
	// - "Build succeeded" -> BuildOutput
	// - "PASS: TestParser" -> TestRun
	// - "error: test failed" -> TestRun (contains "test failed" pattern)
	// - "Step 5 of 10" -> Progress
	expected := map[EventType]int{
		EventTypeFileModified:  2,
		EventTypeGitOperation:  1,
		EventTypeBuildOutput:   1, // build success only
		EventTypeTestRun:       2, // PASS + "test failed"
		EventTypeProgress:      1,
	}

	for eventType, count := range expected {
		if eventTypes[eventType] != count {
			t.Errorf("expected %d events of type %s but got %d", count, eventType, eventTypes[eventType])
		}
	}
}

func TestOutputParser_LineNumberTracking(t *testing.T) {
	parser := NewOutputParser("vc-123", "exec-1", "agent-1")

	lines := []string{
		"Line 1: regular output",
		"Created: file.go",
		"Line 3: more output",
		"Modified: main.go",
	}

	parser.ParseLines(lines)

	if parser.LineNumber != 4 {
		t.Errorf("expected line number 4 but got %d", parser.LineNumber)
	}
}

func TestOutputParser_Reset(t *testing.T) {
	parser := NewOutputParser("vc-123", "exec-1", "agent-1")

	// Parse some lines to change state
	parser.ParseLine("Created: file.go")
	parser.ParseLine("Modified: main.go")

	if parser.LineNumber != 2 {
		t.Errorf("expected line number 2 before reset but got %d", parser.LineNumber)
	}

	// Reset with new context
	parser.Reset("vc-456", "exec-2", "agent-2")

	if parser.IssueID != "vc-456" {
		t.Errorf("expected issue ID vc-456 but got %s", parser.IssueID)
	}

	if parser.LineNumber != 0 {
		t.Errorf("expected line number 0 after reset but got %d", parser.LineNumber)
	}
}

// vc-236: TestOutputParser_ParseToolUse removed
// This test was for regex-based tool usage parsing (ZFC violation)
// Tool usage events now come from structured JSON (Amp --stream-json)
// See agent_test.go for tests of JSON event parsing

func TestOutputParser_RealWorldSample(t *testing.T) {
	parser := NewOutputParser("vc-105", "exec-1", "claude-code")

	// Simulate real agent output
	sample := []string{
		"Starting work on vc-105: Implement OutputParser",
		"Step 1 of 5: Creating parser.go",
		"Created: internal/events/parser.go",
		"Writing unit tests...",
		"Created: internal/events/parser_test.go",
		"Running tests...",
		"Processing: parser_test.go [25%]",
		"PASS: TestOutputParser_ParseFileModifications",
		"PASS: TestOutputParser_ParseTestResults",
		"PASS: TestOutputParser_ParseGitOperations",
		"Step 2 of 5: Running go test",
		"Build succeeded",
		"All tests passed",
		"Step 3 of 5: Committing changes",
		"git add internal/events/parser.go internal/events/parser_test.go",
		"git commit -m 'Implement OutputParser'",
		"Step 4 of 5: Exporting to JSONL",
		"Modified: .beads/issues.jsonl",
		"Step 5 of 5: Complete",
	}

	events := parser.ParseLines(sample)

	// We should get multiple events
	if len(events) == 0 {
		t.Fatal("expected events from real-world sample but got none")
	}

	// Count event types
	eventCounts := make(map[EventType]int)
	for _, event := range events {
		eventCounts[event.Type]++

		// Verify all events have required fields
		if event.ID == "" {
			t.Error("event has empty ID")
		}
		if event.IssueID != "vc-105" {
			t.Errorf("expected issue ID vc-105 but got %s", event.IssueID)
		}
		if event.Timestamp.IsZero() {
			t.Error("event has zero timestamp")
		}
	}

	// Verify we extracted the main event types
	if eventCounts[EventTypeFileModified] == 0 {
		t.Error("expected file modification events")
	}
	if eventCounts[EventTypeTestRun] == 0 {
		t.Error("expected test run events")
	}
	if eventCounts[EventTypeGitOperation] == 0 {
		t.Error("expected git operation events")
	}
	if eventCounts[EventTypeProgress] == 0 {
		t.Error("expected progress events")
	}
	if eventCounts[EventTypeBuildOutput] == 0 {
		t.Error("expected build output events")
	}

	t.Logf("Extracted %d events from %d lines", len(events), len(sample))
	t.Logf("Event type breakdown: %+v", eventCounts)
}
