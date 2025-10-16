package events

import (
	"encoding/json"
	"testing"
	"time"
)

func TestJSONTagsSnakeCase(t *testing.T) {
	event := &AgentEvent{
		ID:         "test-event-123",
		Type:       EventTypeFileModified,
		Timestamp:  time.Date(2025, 10, 15, 12, 0, 0, 0, time.UTC),
		IssueID:    "vc-145",
		ExecutorID: "exec-1",
		AgentID:    "agent-1",
		Severity:   SeverityInfo,
		Message:    "File modified",
		Data: map[string]interface{}{
			"file_path": "test.go",
			"operation": "modified",
		},
		SourceLine: 42,
	}

	jsonBytes, err := json.Marshal(event)
	if err != nil {
		t.Fatalf("Failed to marshal AgentEvent: %v", err)
	}

	jsonStr := string(jsonBytes)

	// Verify snake_case field names
	expectedFields := []string{
		`"id"`,
		`"type"`,
		`"issue_id"`,
		`"executor_id"`,
		`"agent_id"`,
		`"severity"`,
		`"message"`,
		`"source_line"`,
	}

	for _, field := range expectedFields {
		if !contains(jsonStr, field) {
			t.Errorf("JSON missing expected field: %s\nGot: %s", field, jsonStr)
		}
	}
}

func TestFileModifiedDataHelpers(t *testing.T) {
	event := &AgentEvent{
		ID:         "test-1",
		Type:       EventTypeFileModified,
		Timestamp:  time.Now(),
		IssueID:    "vc-145",
		ExecutorID: "exec-1",
		AgentID:    "agent-1",
		Severity:   SeverityInfo,
		Message:    "File created",
	}

	fileData := FileModifiedData{
		FilePath:  "/path/to/file.go",
		Operation: "created",
	}

	if err := event.SetFileModifiedData(fileData); err != nil {
		t.Fatalf("SetFileModifiedData failed: %v", err)
	}

	if event.Data["file_path"] != "/path/to/file.go" {
		t.Errorf("Data map file_path incorrect: got %v", event.Data["file_path"])
	}

	retrieved, err := event.GetFileModifiedData()
	if err != nil {
		t.Fatalf("GetFileModifiedData failed: %v", err)
	}
	if retrieved.FilePath != fileData.FilePath {
		t.Errorf("FilePath mismatch: got %s, want %s", retrieved.FilePath, fileData.FilePath)
	}
}

func TestTestRunDataHelpers(t *testing.T) {
	event := &AgentEvent{
		ID:         "test-2",
		Type:       EventTypeTestRun,
		Timestamp:  time.Now(),
		IssueID:    "vc-145",
		ExecutorID: "exec-1",
		AgentID:    "agent-1",
		Severity:   SeverityInfo,
		Message:    "Test passed",
	}

	testData := TestRunData{
		TestName: "TestFoo",
		Passed:   true,
		Duration: 1500 * time.Millisecond,
		Output:   "ok",
	}

	if err := event.SetTestRunData(testData); err != nil {
		t.Fatalf("SetTestRunData failed: %v", err)
	}

	retrieved, err := event.GetTestRunData()
	if err != nil {
		t.Fatalf("GetTestRunData failed: %v", err)
	}
	if retrieved.TestName != testData.TestName {
		t.Errorf("TestName mismatch: got %s, want %s", retrieved.TestName, testData.TestName)
	}
	if retrieved.Passed != testData.Passed {
		t.Errorf("Passed mismatch: got %v, want %v", retrieved.Passed, testData.Passed)
	}
}

func TestGitOperationDataHelpers(t *testing.T) {
	event := &AgentEvent{
		ID:         "test-3",
		Type:       EventTypeGitOperation,
		Timestamp:  time.Now(),
		IssueID:    "vc-145",
		ExecutorID: "exec-1",
		AgentID:    "agent-1",
		Severity:   SeverityInfo,
		Message:    "Git commit",
	}

	gitData := GitOperationData{
		Command: "commit",
		Args:    []string{"-m", "test commit"},
		Success: true,
	}

	if err := event.SetGitOperationData(gitData); err != nil {
		t.Fatalf("SetGitOperationData failed: %v", err)
	}

	retrieved, err := event.GetGitOperationData()
	if err != nil {
		t.Fatalf("GetGitOperationData failed: %v", err)
	}
	if retrieved.Command != gitData.Command {
		t.Errorf("Command mismatch: got %s, want %s", retrieved.Command, gitData.Command)
	}
	if len(retrieved.Args) != len(gitData.Args) {
		t.Errorf("Args length mismatch: got %d, want %d", len(retrieved.Args), len(gitData.Args))
	}
}

func TestNewFileModifiedEvent(t *testing.T) {
	data := FileModifiedData{
		FilePath:  "/test/file.go",
		Operation: "modified",
	}

	event, err := NewFileModifiedEvent("vc-145", "exec-1", "agent-1", SeverityInfo, "File modified", data)
	if err != nil {
		t.Fatalf("NewFileModifiedEvent failed: %v", err)
	}

	if event.Type != EventTypeFileModified {
		t.Errorf("Wrong event type: got %s, want %s", event.Type, EventTypeFileModified)
	}

	retrieved, err := event.GetFileModifiedData()
	if err != nil {
		t.Fatalf("GetFileModifiedData failed: %v", err)
	}
	if retrieved.FilePath != data.FilePath {
		t.Errorf("FilePath mismatch: got %s, want %s", retrieved.FilePath, data.FilePath)
	}
}

func TestNewTestRunEvent(t *testing.T) {
	data := TestRunData{
		TestName: "TestExample",
		Passed:   true,
		Duration: 2 * time.Second,
		Output:   "PASS",
	}

	event, err := NewTestRunEvent("vc-145", "exec-1", "agent-1", SeverityInfo, "Test passed", data)
	if err != nil {
		t.Fatalf("NewTestRunEvent failed: %v", err)
	}

	if event.Type != EventTypeTestRun {
		t.Errorf("Wrong event type: got %s, want %s", event.Type, EventTypeTestRun)
	}

	retrieved, err := event.GetTestRunData()
	if err != nil {
		t.Fatalf("GetTestRunData failed: %v", err)
	}
	if retrieved.TestName != data.TestName {
		t.Errorf("TestName mismatch: got %s, want %s", retrieved.TestName, data.TestName)
	}
}

func TestNewGitOperationEvent(t *testing.T) {
	data := GitOperationData{
		Command: "push",
		Args:    []string{"origin", "main"},
		Success: true,
	}

	event, err := NewGitOperationEvent("vc-145", "exec-1", "agent-1", SeverityInfo, "Pushed to origin", data)
	if err != nil {
		t.Fatalf("NewGitOperationEvent failed: %v", err)
	}

	if event.Type != EventTypeGitOperation {
		t.Errorf("Wrong event type: got %s, want %s", event.Type, EventTypeGitOperation)
	}
}

func TestNewSimpleEvent(t *testing.T) {
	event := NewSimpleEvent(EventTypeProgress, "vc-145", "exec-1", "agent-1", SeverityInfo, "Working on task")

	if event.Type != EventTypeProgress {
		t.Errorf("Wrong event type: got %s, want %s", event.Type, EventTypeProgress)
	}
	if len(event.Data) != 0 {
		t.Errorf("Data should be empty, got %d items", len(event.Data))
	}
}

func TestNewExecutorEvent(t *testing.T) {
	customData := map[string]interface{}{
		"phase": "assessment",
		"step":  1,
	}

	event := NewExecutorEvent(EventTypeAssessmentStarted, "vc-145", "exec-1", "", SeverityInfo, "Assessment started", customData)

	if event.Type != EventTypeAssessmentStarted {
		t.Errorf("Wrong event type: got %s, want %s", event.Type, EventTypeAssessmentStarted)
	}
	if event.Data["phase"] != "assessment" {
		t.Errorf("Wrong data phase: got %v", event.Data["phase"])
	}
}

func TestHumanReadableJSON(t *testing.T) {
	event := NewSimpleEvent(EventTypeProgress, "vc-145", "exec-1", "agent-1", SeverityInfo, "Test message")

	jsonBytes, err := json.MarshalIndent(event, "", "  ")
	if err != nil {
		t.Fatalf("Failed to marshal: %v", err)
	}

	jsonStr := string(jsonBytes)

	// Verify snake_case fields
	if !contains(jsonStr, `"issue_id"`) {
		t.Error("JSON should contain 'issue_id' field with snake_case")
	}
	if !contains(jsonStr, `"executor_id"`) {
		t.Error("JSON should contain 'executor_id' field with snake_case")
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && stringContains(s, substr)
}

func stringContains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
