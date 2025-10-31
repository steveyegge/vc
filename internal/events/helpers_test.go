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

// vc-275: Tests for epic lifecycle event typed constructors

func TestEpicCompletedDataHelpers(t *testing.T) {
	event := &AgentEvent{
		ID:         "test-epic-1",
		Type:       EventTypeEpicCompleted,
		Timestamp:  time.Now(),
		IssueID:    "vc-5",
		ExecutorID: "exec-1",
		AgentID:    "",
		Severity:   SeverityInfo,
		Message:    "Epic completed",
	}

	epicData := EpicCompletedData{
		EpicID:            "vc-5",
		EpicTitle:         "Beads Integration",
		ChildrenCompleted: 10,
		CompletionMethod:  "ai_assessment",
		Confidence:        0.95,
		IsMission:         true,
		Actor:             "ai-supervisor",
	}

	if err := event.SetEpicCompletedData(epicData); err != nil {
		t.Fatalf("SetEpicCompletedData failed: %v", err)
	}

	if event.Data["epic_id"] != "vc-5" {
		t.Errorf("Data map epic_id incorrect: got %v", event.Data["epic_id"])
	}

	retrieved, err := event.GetEpicCompletedData()
	if err != nil {
		t.Fatalf("GetEpicCompletedData failed: %v", err)
	}
	if retrieved.EpicID != epicData.EpicID {
		t.Errorf("EpicID mismatch: got %s, want %s", retrieved.EpicID, epicData.EpicID)
	}
	if retrieved.ChildrenCompleted != epicData.ChildrenCompleted {
		t.Errorf("ChildrenCompleted mismatch: got %d, want %d", retrieved.ChildrenCompleted, epicData.ChildrenCompleted)
	}
	if retrieved.Confidence != epicData.Confidence {
		t.Errorf("Confidence mismatch: got %f, want %f", retrieved.Confidence, epicData.Confidence)
	}
}

func TestEpicCleanupStartedDataHelpers(t *testing.T) {
	event := &AgentEvent{
		ID:         "test-epic-2",
		Type:       EventTypeEpicCleanupStarted,
		Timestamp:  time.Now(),
		IssueID:    "vc-5",
		ExecutorID: "exec-1",
		AgentID:    "",
		Severity:   SeverityInfo,
		Message:    "Starting epic cleanup",
	}

	cleanupData := EpicCleanupStartedData{
		EpicID:      "vc-5",
		IsMission:   true,
		SandboxPath: "/tmp/vc-mission-5",
	}

	if err := event.SetEpicCleanupStartedData(cleanupData); err != nil {
		t.Fatalf("SetEpicCleanupStartedData failed: %v", err)
	}

	retrieved, err := event.GetEpicCleanupStartedData()
	if err != nil {
		t.Fatalf("GetEpicCleanupStartedData failed: %v", err)
	}
	if retrieved.EpicID != cleanupData.EpicID {
		t.Errorf("EpicID mismatch: got %s, want %s", retrieved.EpicID, cleanupData.EpicID)
	}
	if retrieved.SandboxPath != cleanupData.SandboxPath {
		t.Errorf("SandboxPath mismatch: got %s, want %s", retrieved.SandboxPath, cleanupData.SandboxPath)
	}
}

func TestEpicCleanupCompletedDataHelpers(t *testing.T) {
	event := &AgentEvent{
		ID:         "test-epic-3",
		Type:       EventTypeEpicCleanupCompleted,
		Timestamp:  time.Now(),
		IssueID:    "vc-5",
		ExecutorID: "exec-1",
		AgentID:    "",
		Severity:   SeverityInfo,
		Message:    "Epic cleanup completed",
	}

	completeData := EpicCleanupCompletedData{
		EpicID:      "vc-5",
		IsMission:   true,
		SandboxPath: "/tmp/vc-mission-5",
		Success:     true,
		DurationMs:  1500,
	}

	if err := event.SetEpicCleanupCompletedData(completeData); err != nil {
		t.Fatalf("SetEpicCleanupCompletedData failed: %v", err)
	}

	retrieved, err := event.GetEpicCleanupCompletedData()
	if err != nil {
		t.Fatalf("GetEpicCleanupCompletedData failed: %v", err)
	}
	if retrieved.Success != completeData.Success {
		t.Errorf("Success mismatch: got %v, want %v", retrieved.Success, completeData.Success)
	}
	if retrieved.DurationMs != completeData.DurationMs {
		t.Errorf("DurationMs mismatch: got %d, want %d", retrieved.DurationMs, completeData.DurationMs)
	}
}

func TestNewEpicCompletedEvent(t *testing.T) {
	data := EpicCompletedData{
		EpicID:            "vc-5",
		EpicTitle:         "Test Epic",
		ChildrenCompleted: 5,
		CompletionMethod:  "ai_assessment",
		Confidence:        0.9,
		IsMission:         false,
		Actor:             "ai-supervisor",
	}

	event, err := NewEpicCompletedEvent("vc-5", "exec-1", "", SeverityInfo, "Epic completed", data)
	if err != nil {
		t.Fatalf("NewEpicCompletedEvent failed: %v", err)
	}

	if event.Type != EventTypeEpicCompleted {
		t.Errorf("Wrong event type: got %s, want %s", event.Type, EventTypeEpicCompleted)
	}

	retrieved, err := event.GetEpicCompletedData()
	if err != nil {
		t.Fatalf("GetEpicCompletedData failed: %v", err)
	}
	if retrieved.EpicID != data.EpicID {
		t.Errorf("EpicID mismatch: got %s, want %s", retrieved.EpicID, data.EpicID)
	}
	if retrieved.CompletionMethod != data.CompletionMethod {
		t.Errorf("CompletionMethod mismatch: got %s, want %s", retrieved.CompletionMethod, data.CompletionMethod)
	}
}

func TestNewEpicCleanupStartedEvent(t *testing.T) {
	data := EpicCleanupStartedData{
		EpicID:      "vc-5",
		IsMission:   true,
		SandboxPath: "/tmp/sandbox",
	}

	event, err := NewEpicCleanupStartedEvent("vc-5", "exec-1", "", SeverityInfo, "Starting cleanup", data)
	if err != nil {
		t.Fatalf("NewEpicCleanupStartedEvent failed: %v", err)
	}

	if event.Type != EventTypeEpicCleanupStarted {
		t.Errorf("Wrong event type: got %s, want %s", event.Type, EventTypeEpicCleanupStarted)
	}

	retrieved, err := event.GetEpicCleanupStartedData()
	if err != nil {
		t.Fatalf("GetEpicCleanupStartedData failed: %v", err)
	}
	if retrieved.SandboxPath != data.SandboxPath {
		t.Errorf("SandboxPath mismatch: got %s, want %s", retrieved.SandboxPath, data.SandboxPath)
	}
}

func TestNewEpicCleanupCompletedEvent(t *testing.T) {
	data := EpicCleanupCompletedData{
		EpicID:      "vc-5",
		IsMission:   true,
		SandboxPath: "/tmp/sandbox",
		Success:     true,
		DurationMs:  2000,
	}

	event, err := NewEpicCleanupCompletedEvent("vc-5", "exec-1", "", SeverityInfo, "Cleanup completed", data)
	if err != nil {
		t.Fatalf("NewEpicCleanupCompletedEvent failed: %v", err)
	}

	if event.Type != EventTypeEpicCleanupCompleted {
		t.Errorf("Wrong event type: got %s, want %s", event.Type, EventTypeEpicCleanupCompleted)
	}

	retrieved, err := event.GetEpicCleanupCompletedData()
	if err != nil {
		t.Fatalf("GetEpicCleanupCompletedData failed: %v", err)
	}
	if retrieved.Success != data.Success {
		t.Errorf("Success mismatch: got %v, want %v", retrieved.Success, data.Success)
	}
}

func TestEpicCleanupCompletedEventWithError(t *testing.T) {
	data := EpicCleanupCompletedData{
		EpicID:      "vc-5",
		IsMission:   true,
		SandboxPath: "/tmp/sandbox",
		Success:     false,
		Error:       "failed to remove worktree",
		DurationMs:  500,
	}

	event, err := NewEpicCleanupCompletedEvent("vc-5", "exec-1", "", SeverityWarning, "Cleanup failed", data)
	if err != nil {
		t.Fatalf("NewEpicCleanupCompletedEvent failed: %v", err)
	}

	retrieved, err := event.GetEpicCleanupCompletedData()
	if err != nil {
		t.Fatalf("GetEpicCleanupCompletedData failed: %v", err)
	}
	if retrieved.Error != data.Error {
		t.Errorf("Error mismatch: got %s, want %s", retrieved.Error, data.Error)
	}
	if retrieved.Success {
		t.Errorf("Success should be false for failed cleanup")
	}
}
