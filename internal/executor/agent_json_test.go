package executor

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/steveyegge/vc/internal/events"
	"github.com/steveyegge/vc/internal/storage"
	"github.com/steveyegge/vc/internal/types"
)

// TestConvertJSONToEventActualAmpFormat tests parsing of the ACTUAL Amp --stream-json format (vc-29, vc-30)
// This test was added after verifying the real Amp JSON output structure.
// Amp uses a NESTED format: type="assistant" contains message.content[] with tool_use items.
func TestConvertJSONToEventActualAmpFormat(t *testing.T) {
	// Setup test agent with mock dependencies
	cfg := storage.DefaultConfig()
	cfg.Path = t.TempDir() + "/test.db"

	ctx := context.Background()
	store, err := storage.NewStorage(ctx, cfg)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer func() { _ = store.Close() }()

	issue := &types.Issue{
		ID:                 "vc-test-amp-format",
		Title:              "Test Real Amp JSON Format",
		Description:        "Test convertJSONToEvent with actual Amp structure",
		IssueType:          types.TypeTask,
		Status:             types.StatusOpen,
		Priority:           1,
		AcceptanceCriteria: "Test correctly parses Amp JSON format",
		CreatedAt:          time.Now(),
		UpdatedAt:          time.Now(),
	}
	if err := store.CreateIssue(ctx, issue, "test"); err != nil {
		t.Fatalf("Failed to create issue: %v", err)
	}

	executorID := "test-executor"
	agentID := "test-agent"

	agent := &Agent{
		config: AgentConfig{
			Issue:      issue,
			Store:      store,
			ExecutorID: executorID,
			AgentID:    agentID,
			StreamJSON: true,
		},
		parser: events.NewOutputParser(issue.ID, executorID, agentID),
		ctx:    ctx,
	}

	t.Run("Read tool - actual Amp nested format", func(t *testing.T) {
		// This is what Amp ACTUALLY outputs (verified with real Amp 0.0.1761854483-g125cd7)
		msg := AgentMessage{
			Type:      "assistant",
			SessionID: "T-test-123",
			Message: &AssistantMessage{
				Type: "message",
				Role: "assistant",
				Content: []MessageContent{
					{Type: "text", Text: "I'll read the file for you."},
					{Type: "tool_use", ID: "toolu_123", Name: "Read", Input: map[string]interface{}{"path": "/private/tmp/amp-test/main.go"}},
				},
				StopReason: "tool_use",
			},
		}

		rawJSON, err := json.Marshal(msg)
		if err != nil {
			t.Fatalf("Failed to marshal JSON: %v", err)
		}
		rawLine := string(rawJSON)

		// Convert to event
		event := agent.convertJSONToEvent(msg, rawLine)

		// Verify event was created
		if event == nil {
			t.Fatal("Expected event to be created from nested Amp format, got nil")
		}

		// Verify event structure
		if event.Type != events.EventTypeAgentToolUse {
			t.Errorf("Expected type %s, got %s", events.EventTypeAgentToolUse, event.Type)
		}

		// Verify tool use data
		toolData, err := event.GetAgentToolUseData()
		if err != nil {
			t.Fatalf("Failed to get tool use data: %v", err)
		}

		if toolData.ToolName != "read" {
			t.Errorf("Expected ToolName 'read', got %s", toolData.ToolName)
		}
		if toolData.TargetFile != "/private/tmp/amp-test/main.go" {
			t.Errorf("Expected TargetFile '/private/tmp/amp-test/main.go', got %s", toolData.TargetFile)
		}
	})

	t.Run("edit_file tool - actual Amp nested format", func(t *testing.T) {
		msg := AgentMessage{
			Type:      "assistant",
			SessionID: "T-test-456",
			Message: &AssistantMessage{
				Type: "message",
				Role: "assistant",
				Content: []MessageContent{
					{Type: "tool_use", ID: "toolu_456", Name: "edit_file", Input: map[string]interface{}{
						"path":    "/private/tmp/amp-test/main.go",
						"old_str": "Hello world",
						"new_str": "Hello Amp",
					}},
				},
				StopReason: "tool_use",
			},
		}

		rawJSON, _ := json.Marshal(msg)
		event := agent.convertJSONToEvent(msg, string(rawJSON))

		if event == nil {
			t.Fatal("Expected event for edit_file")
		}

		toolData, err := event.GetAgentToolUseData()
		if err != nil {
			t.Fatalf("Failed to get tool data: %v", err)
		}

		if toolData.ToolName != "edit" { // Should be normalized
			t.Errorf("Expected normalized ToolName 'edit', got %s", toolData.ToolName)
		}
		if toolData.TargetFile != "/private/tmp/amp-test/main.go" {
			t.Errorf("Expected TargetFile path, got %s", toolData.TargetFile)
		}
	})

	t.Run("Bash tool - actual Amp nested format", func(t *testing.T) {
		msg := AgentMessage{
			Type:      "assistant",
			SessionID: "T-test-789",
			Message: &AssistantMessage{
				Type: "message",
				Role: "assistant",
				Content: []MessageContent{
					{Type: "text", Text: "I'll run the command."},
					{Type: "tool_use", ID: "toolu_789", Name: "Bash", Input: map[string]interface{}{"cmd": "ls -la"}},
				},
				StopReason: "tool_use",
			},
		}

		rawJSON, _ := json.Marshal(msg)
		event := agent.convertJSONToEvent(msg, string(rawJSON))

		if event == nil {
			t.Fatal("Expected event for Bash tool")
		}

		toolData, err := event.GetAgentToolUseData()
		if err != nil {
			t.Fatalf("Failed to get tool data: %v", err)
		}

		if toolData.ToolName != "bash" {
			t.Errorf("Expected ToolName 'bash', got %s", toolData.ToolName)
		}
		if toolData.Command != "ls -la" {
			t.Errorf("Expected Command 'ls -la', got %s", toolData.Command)
		}
	})

	t.Run("System event - should be skipped", func(t *testing.T) {
		msg := AgentMessage{
			Type:      "system",
			Subtype:   "init",
			SessionID: "T-test-system",
			Cwd:       "/private/tmp/amp-test",
			Tools:     []string{"Read", "Bash", "edit_file"},
		}

		rawJSON, _ := json.Marshal(msg)
		event := agent.convertJSONToEvent(msg, string(rawJSON))

		// System events should NOT create agent_tool_use events
		if event != nil {
			t.Error("Expected nil for system event type")
		}
	})

	t.Run("Result event - should be skipped", func(t *testing.T) {
		msg := AgentMessage{
			Type:       "result",
			Subtype:    "success",
			SessionID:  "T-test-result",
			DurationMs: 5000,
			IsError:    false,
			Result:     "Task completed successfully",
		}

		rawJSON, _ := json.Marshal(msg)
		event := agent.convertJSONToEvent(msg, string(rawJSON))

		// Result events should NOT create agent_tool_use events
		if event != nil {
			t.Error("Expected nil for result event type")
		}
	})

	t.Run("Assistant message with only text - no tool use", func(t *testing.T) {
		msg := AgentMessage{
			Type:      "assistant",
			SessionID: "T-test-textonly",
			Message: &AssistantMessage{
				Type: "message",
				Role: "assistant",
				Content: []MessageContent{
					{Type: "text", Text: "I've completed the task."},
				},
				StopReason: "end_turn",
			},
		}

		rawJSON, _ := json.Marshal(msg)
		event := agent.convertJSONToEvent(msg, string(rawJSON))

		// No tool_use in content array - should return nil
		if event != nil {
			t.Error("Expected nil for assistant message with no tool_use")
		}
	})

	t.Run("create_file tool - actual Amp nested format", func(t *testing.T) {
		msg := AgentMessage{
			Type:      "assistant",
			SessionID: "T-test-create",
			Message: &AssistantMessage{
				Type: "message",
				Role: "assistant",
				Content: []MessageContent{
					{Type: "tool_use", ID: "toolu_create", Name: "create_file", Input: map[string]interface{}{
						"path":    "/tmp/newfile.go",
						"content": "package main",
					}},
				},
				StopReason: "tool_use",
			},
		}

		rawJSON, _ := json.Marshal(msg)
		event := agent.convertJSONToEvent(msg, string(rawJSON))

		if event == nil {
			t.Fatal("Expected event for create_file")
		}

		toolData, err := event.GetAgentToolUseData()
		if err != nil {
			t.Fatalf("Failed to get tool data: %v", err)
		}

		// create_file should be normalized to "write"
		if toolData.ToolName != "write" {
			t.Errorf("Expected normalized ToolName 'write', got %s", toolData.ToolName)
		}
	})
}
