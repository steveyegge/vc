package sqlite

import (
	"context"
	"testing"
	"time"

	"github.com/steveyegge/vc/internal/events"
	"github.com/steveyegge/vc/internal/types"
)

func TestAgentEventStorage(t *testing.T) {
	// Create in-memory database for testing
	store, err := New(":memory:")
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer func() { _ = store.Close() }()

	ctx := context.Background()

	// Create a test issue first (required for foreign key constraint)
	testIssue := &types.Issue{
		ID:          "vc-122",
		Title:       "Add git operation event tracking",
		Description: "Test issue",
		Status:      types.StatusOpen,
		IssueType:   types.TypeTask,
		Priority:    0, // P0
	}
	err = store.CreateIssue(ctx, testIssue, "test")
	if err != nil {
		t.Fatalf("Failed to create test issue: %v", err)
	}

	// Test 1: Store a git operation event
	t.Run("StoreGitOperationEvent", func(t *testing.T) {
		event := &events.AgentEvent{
			ID:         "evt-001",
			Type:       events.EventTypeGitOperation,
			Timestamp:  time.Now(),
			IssueID:    "vc-122",
			ExecutorID: "exec-1",
			AgentID:    "agent-1",
			Severity:   events.SeverityInfo,
			Message:    "Git commit successful",
			Data: map[string]interface{}{
				"command": "git",
				"args":    []interface{}{"commit", "-m", "test commit"},
				"success": true,
				"hash":    "abc123",
			},
			SourceLine: 42,
		}

		err := store.StoreAgentEvent(ctx, event)
		if err != nil {
			t.Fatalf("Failed to store event: %v", err)
		}
	})

	// Test 2: Retrieve events by issue
	t.Run("GetAgentEventsByIssue", func(t *testing.T) {
		events, err := store.GetAgentEventsByIssue(ctx, "vc-122")
		if err != nil {
			t.Fatalf("Failed to get events: %v", err)
		}

		if len(events) != 1 {
			t.Fatalf("Expected 1 event, got %d", len(events))
		}

		event := events[0]
		if event.ID != "evt-001" {
			t.Errorf("Expected ID 'evt-001', got '%s'", event.ID)
		}
		if event.Type != "git_operation" {
			t.Errorf("Expected type 'git_operation', got '%s'", event.Type)
		}
		if event.Message != "Git commit successful" {
			t.Errorf("Expected message 'Git commit successful', got '%s'", event.Message)
		}

		// Verify data field
		if event.Data["command"] != "git" {
			t.Errorf("Expected command 'git', got '%v'", event.Data["command"])
		}
		if event.Data["hash"] != "abc123" {
			t.Errorf("Expected hash 'abc123', got '%v'", event.Data["hash"])
		}
	})

	// Test 3: Store multiple events and filter by type
	t.Run("FilterByType", func(t *testing.T) {
		// Store a file modified event
		fileEvent := &events.AgentEvent{
			ID:         "evt-002",
			Type:       events.EventTypeFileModified,
			Timestamp:  time.Now(),
			IssueID:    "vc-122",
			ExecutorID: "exec-1",
			AgentID:    "agent-1",
			Severity:   events.SeverityInfo,
			Message:    "File modified",
			Data: map[string]interface{}{
				"file_path": "main.go",
				"operation": "modified",
			},
			SourceLine: 10,
		}
		err := store.StoreAgentEvent(ctx, fileEvent)
		if err != nil {
			t.Fatalf("Failed to store file event: %v", err)
		}

		// Filter by git_operation type
		filter := events.EventFilter{
			Type: events.EventTypeGitOperation,
		}
		gitEvents, err := store.GetAgentEvents(ctx, filter)
		if err != nil {
			t.Fatalf("Failed to get git events: %v", err)
		}

		if len(gitEvents) != 1 {
			t.Fatalf("Expected 1 git event, got %d", len(gitEvents))
		}
		if gitEvents[0].Type != events.EventTypeGitOperation {
			t.Errorf("Expected git_operation type, got %s", gitEvents[0].Type)
		}
	})

	// Test 4: Get recent events with limit
	t.Run("GetRecentEvents", func(t *testing.T) {
		recentEvents, err := store.GetRecentAgentEvents(ctx, 5)
		if err != nil {
			t.Fatalf("Failed to get recent events: %v", err)
		}

		if len(recentEvents) != 2 {
			t.Fatalf("Expected 2 events, got %d", len(recentEvents))
		}

		// Most recent should be first (DESC order)
		if recentEvents[0].ID != "evt-002" {
			t.Errorf("Expected most recent event 'evt-002', got '%s'", recentEvents[0].ID)
		}
	})

	// Test 5: Filter by severity
	t.Run("FilterBySeverity", func(t *testing.T) {
		// Store an error event
		errorEvent := &events.AgentEvent{
			ID:         "evt-003",
			Type:       events.EventTypeError,
			Timestamp:  time.Now(),
			IssueID:    "vc-122",
			ExecutorID: "exec-1",
			AgentID:    "agent-1",
			Severity:   events.SeverityError,
			Message:    "Build failed",
			Data: map[string]interface{}{
				"error": "compilation error",
			},
			SourceLine: 100,
		}
		err := store.StoreAgentEvent(ctx, errorEvent)
		if err != nil {
			t.Fatalf("Failed to store error event: %v", err)
		}

		// Filter by error severity
		filter := events.EventFilter{
			Severity: events.SeverityError,
		}
		errorEvents, err := store.GetAgentEvents(ctx, filter)
		if err != nil {
			t.Fatalf("Failed to get error events: %v", err)
		}

		if len(errorEvents) != 1 {
			t.Fatalf("Expected 1 error event, got %d", len(errorEvents))
		}
		if errorEvents[0].Severity != events.SeverityError {
			t.Errorf("Expected error severity, got %s", errorEvents[0].Severity)
		}
	})
}
