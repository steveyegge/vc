package events

import (
	"testing"
)

// TestQualityGatesProgressConstructor tests the typed constructor for quality gates progress events (vc-273)
func TestQualityGatesProgressConstructor(t *testing.T) {
	data := QualityGatesProgressData{
		CurrentGate:    "test",
		GatesCompleted: 1,
		TotalGates:     3,
		ElapsedSeconds: 10,
		Message:        "Running test gate",
	}

	event, err := NewQualityGatesProgressEvent(
		"vc-123",
		"executor-1",
		"",
		SeverityInfo,
		"Progress update",
		data,
	)

	if err != nil {
		t.Fatalf("Failed to create event: %v", err)
	}

	if event.Type != EventTypeQualityGatesProgress {
		t.Errorf("Expected event type %s, got %s", EventTypeQualityGatesProgress, event.Type)
	}

	if event.IssueID != "vc-123" {
		t.Errorf("Expected issue ID vc-123, got %s", event.IssueID)
	}

	if event.ExecutorID != "executor-1" {
		t.Errorf("Expected executor ID executor-1, got %s", event.ExecutorID)
	}

	if event.Severity != SeverityInfo {
		t.Errorf("Expected severity Info, got %s", event.Severity)
	}

	// Test getting the data back
	retrieved, err := event.GetQualityGatesProgressData()
	if err != nil {
		t.Fatalf("Failed to retrieve data: %v", err)
	}

	if retrieved.CurrentGate != "test" {
		t.Errorf("Expected current gate 'test', got '%s'", retrieved.CurrentGate)
	}

	if retrieved.GatesCompleted != 1 {
		t.Errorf("Expected gates completed 1, got %d", retrieved.GatesCompleted)
	}

	if retrieved.TotalGates != 3 {
		t.Errorf("Expected total gates 3, got %d", retrieved.TotalGates)
	}

	if retrieved.ElapsedSeconds != 10 {
		t.Errorf("Expected elapsed seconds 10, got %d", retrieved.ElapsedSeconds)
	}

	if retrieved.Message != "Running test gate" {
		t.Errorf("Expected message 'Running test gate', got '%s'", retrieved.Message)
	}
}

// TestQualityGatesProgressSetterGetter tests the setter and getter methods (vc-273)
func TestQualityGatesProgressSetterGetter(t *testing.T) {
	event := &AgentEvent{}

	data := QualityGatesProgressData{
		CurrentGate:    "build",
		GatesCompleted: 2,
		TotalGates:     3,
		ElapsedSeconds: 45,
		Message:        "Building project",
	}

	// Test setter
	err := event.SetQualityGatesProgressData(data)
	if err != nil {
		t.Fatalf("Failed to set data: %v", err)
	}

	// Test getter
	retrieved, err := event.GetQualityGatesProgressData()
	if err != nil {
		t.Fatalf("Failed to get data: %v", err)
	}

	if retrieved.CurrentGate != data.CurrentGate {
		t.Errorf("Expected current gate '%s', got '%s'", data.CurrentGate, retrieved.CurrentGate)
	}

	if retrieved.GatesCompleted != data.GatesCompleted {
		t.Errorf("Expected gates completed %d, got %d", data.GatesCompleted, retrieved.GatesCompleted)
	}

	if retrieved.TotalGates != data.TotalGates {
		t.Errorf("Expected total gates %d, got %d", data.TotalGates, retrieved.TotalGates)
	}

	if retrieved.ElapsedSeconds != data.ElapsedSeconds {
		t.Errorf("Expected elapsed seconds %d, got %d", data.ElapsedSeconds, retrieved.ElapsedSeconds)
	}

	if retrieved.Message != data.Message {
		t.Errorf("Expected message '%s', got '%s'", data.Message, retrieved.Message)
	}
}
