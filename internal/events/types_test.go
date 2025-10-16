package events

import (
	"encoding/json"
	"testing"
	"time"
)

// TestAgentEventJSONSerialization verifies that AgentEvent can be serialized to JSON.
func TestAgentEventJSONSerialization(t *testing.T) {
	now := time.Now()

	tests := []struct {
		name  string
		event *AgentEvent
	}{
		{
			name: "file modified event",
			event: &AgentEvent{
				ID:         "evt-001",
				Type:       EventTypeFileModified,
				Timestamp:  now,
				IssueID:    "vc-100",
				ExecutorID: "exec-1",
				AgentID:    "agent-1",
				Severity:   SeverityInfo,
				Message:    "File modified",
				Data: map[string]interface{}{
					"file_path": "main.go",
					"operation": "modified",
				},
				SourceLine: 42,
			},
		},
		{
			name: "test run event",
			event: &AgentEvent{
				ID:         "evt-002",
				Type:       EventTypeTestRun,
				Timestamp:  now,
				IssueID:    "vc-101",
				ExecutorID: "exec-1",
				AgentID:    "agent-1",
				Severity:   SeverityInfo,
				Message:    "Test passed",
				Data: map[string]interface{}{
					"test_name": "TestFoo",
					"passed":    true,
					"duration":  1500000000, // 1.5s in nanoseconds
					"output":    "ok",
				},
				SourceLine: 100,
			},
		},
		{
			name: "git operation event",
			event: &AgentEvent{
				ID:         "evt-003",
				Type:       EventTypeGitOperation,
				Timestamp:  now,
				IssueID:    "vc-102",
				ExecutorID: "exec-1",
				AgentID:    "agent-1",
				Severity:   SeverityInfo,
				Message:    "Git commit successful",
				Data: map[string]interface{}{
					"command": "git",
					"args":    []interface{}{"commit", "-m", "test"},
					"success": true,
				},
				SourceLine: 200,
			},
		},
		{
			name: "error event",
			event: &AgentEvent{
				ID:         "evt-004",
				Type:       EventTypeError,
				Timestamp:  now,
				IssueID:    "vc-103",
				ExecutorID: "exec-1",
				AgentID:    "agent-1",
				Severity:   SeverityCritical,
				Message:    "Build failed",
				Data: map[string]interface{}{
					"error": "compilation error",
				},
				SourceLine: 300,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Marshal to JSON
			data, err := json.Marshal(tt.event)
			if err != nil {
				t.Fatalf("Failed to marshal event: %v", err)
			}

			// Verify we got valid JSON
			if len(data) == 0 {
				t.Fatal("Marshaled JSON is empty")
			}

			// Unmarshal back to verify round-trip
			var unmarshaled AgentEvent
			if err := json.Unmarshal(data, &unmarshaled); err != nil {
				t.Fatalf("Failed to unmarshal event: %v", err)
			}

			// Verify key fields
			if unmarshaled.ID != tt.event.ID {
				t.Errorf("ID mismatch: got %s, want %s", unmarshaled.ID, tt.event.ID)
			}
			if unmarshaled.Type != tt.event.Type {
				t.Errorf("Type mismatch: got %s, want %s", unmarshaled.Type, tt.event.Type)
			}
			if unmarshaled.Severity != tt.event.Severity {
				t.Errorf("Severity mismatch: got %s, want %s", unmarshaled.Severity, tt.event.Severity)
			}
			if unmarshaled.Message != tt.event.Message {
				t.Errorf("Message mismatch: got %s, want %s", unmarshaled.Message, tt.event.Message)
			}
		})
	}
}

// TestEventTypeConstants verifies all event type constants are defined.
func TestEventTypeConstants(t *testing.T) {
	types := []EventType{
		EventTypeFileModified,
		EventTypeTestRun,
		EventTypeGitOperation,
		EventTypeBuildOutput,
		EventTypeLintOutput,
		EventTypeProgress,
		EventTypeError,
		EventTypeWatchdog,
	}

	for _, et := range types {
		if et == "" {
			t.Error("Event type constant is empty")
		}
	}
}

// TestEventSeverityConstants verifies all severity constants are defined.
func TestEventSeverityConstants(t *testing.T) {
	severities := []EventSeverity{
		SeverityInfo,
		SeverityWarning,
		SeverityError,
		SeverityCritical,
	}

	for _, s := range severities {
		if s == "" {
			t.Error("Severity constant is empty")
		}
	}
}
