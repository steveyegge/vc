package events_test

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/steveyegge/vc/internal/events"
)

// ExampleNewFileModifiedEvent demonstrates creating a file modification event with type-safe data.
func ExampleNewFileModifiedEvent() {
	fileData := events.FileModifiedData{
		FilePath:  "cmd/vc/tail.go",
		Operation: "created",
	}

	event, _ := events.NewFileModifiedEvent(
		"vc-145",
		"exec-1",
		"agent-1",
		events.SeverityInfo,
		"Created new tail command",
		fileData,
	)

	jsonBytes, _ := json.MarshalIndent(event, "", "  ")
	fmt.Println(string(jsonBytes))
	// Output will show snake_case JSON fields like "issue_id", "executor_id", etc.
}

// ExampleAgentEvent_SetFileModifiedData demonstrates type-safe data handling.
func ExampleAgentEvent_SetFileModifiedData() {
	event := events.NewSimpleEvent(
		events.EventTypeFileModified,
		"vc-145",
		"exec-1",
		"agent-1",
		events.SeverityInfo,
		"File modified",
	)

	// Set data in a type-safe way
	fileData := events.FileModifiedData{
		FilePath:  "main.go",
		Operation: "modified",
	}
	_ = event.SetFileModifiedData(fileData) // Intentionally ignore error in example

	// Retrieve data in a type-safe way
	retrieved, _ := event.GetFileModifiedData()
	fmt.Printf("File: %s, Operation: %s\n", retrieved.FilePath, retrieved.Operation)
	// Output: File: main.go, Operation: modified
}

// ExampleNewTestRunEvent demonstrates creating a test run event.
func ExampleNewTestRunEvent() {
	testData := events.TestRunData{
		TestName: "TestAgentEventJSON",
		Passed:   true,
		Duration: 2500 * time.Millisecond,
		Output:   "PASS",
	}

	event, _ := events.NewTestRunEvent(
		"vc-145",
		"exec-1",
		"agent-1",
		events.SeverityInfo,
		"All tests passed",
		testData,
	)

	retrieved, _ := event.GetTestRunData()
	fmt.Printf("Test: %s, Passed: %v, Duration: %v\n",
		retrieved.TestName, retrieved.Passed, retrieved.Duration)
	// Output: Test: TestAgentEventJSON, Passed: true, Duration: 2.5s
}
