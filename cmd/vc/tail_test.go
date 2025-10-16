package main

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/steveyegge/vc/internal/events"
	"github.com/steveyegge/vc/internal/storage/sqlite"
	"github.com/steveyegge/vc/internal/types"
)

func TestTailCommand(t *testing.T) {
	// Create temporary database
	tmpDB := t.TempDir() + "/test.db"
	testStore, err := sqlite.New(tmpDB)
	if err != nil {
		t.Fatalf("Failed to create test database: %v", err)
	}
	defer testStore.Close()

	ctx := context.Background()

	// Create a test issue
	issue := &types.Issue{
		Title:       "Test issue for tail",
		Description: "Testing tail command",
		Status:      types.StatusOpen,
		Priority:    1,
		IssueType:   types.TypeTask,
	}
	err = testStore.CreateIssue(ctx, issue, "test")
	if err != nil {
		t.Fatalf("Failed to create issue: %v", err)
	}

	// Create some test events
	testEvents := []*events.AgentEvent{
		{
			ID:         uuid.New().String(),
			Type:       events.EventTypeIssueClaimed,
			Timestamp:  time.Now().Add(-5 * time.Minute),
			IssueID:    issue.ID,
			ExecutorID: "test-executor-1",
			AgentID:    "",
			Severity:   events.SeverityInfo,
			Message:    "Issue claimed for processing",
			Data:       map[string]interface{}{},
		},
		{
			ID:         uuid.New().String(),
			Type:       events.EventTypeAssessmentStarted,
			Timestamp:  time.Now().Add(-4 * time.Minute),
			IssueID:    issue.ID,
			ExecutorID: "test-executor-1",
			AgentID:    "",
			Severity:   events.SeverityInfo,
			Message:    "AI assessment phase started",
			Data:       map[string]interface{}{},
		},
		{
			ID:         uuid.New().String(),
			Type:       events.EventTypeAgentSpawned,
			Timestamp:  time.Now().Add(-3 * time.Minute),
			IssueID:    issue.ID,
			ExecutorID: "test-executor-1",
			AgentID:    "agent-123",
			Severity:   events.SeverityInfo,
			Message:    "Coding agent spawned",
			Data: map[string]interface{}{
				"agent_type": "claude-code",
			},
		},
		{
			ID:         uuid.New().String(),
			Type:       events.EventTypeError,
			Timestamp:  time.Now().Add(-2 * time.Minute),
			IssueID:    issue.ID,
			ExecutorID: "test-executor-1",
			AgentID:    "agent-123",
			Severity:   events.SeverityError,
			Message:    "Test failed: syntax error in foo.go",
			Data: map[string]interface{}{
				"file": "foo.go",
				"line": 42,
			},
		},
		{
			ID:         uuid.New().String(),
			Type:       events.EventTypeAgentCompleted,
			Timestamp:  time.Now().Add(-1 * time.Minute),
			IssueID:    issue.ID,
			ExecutorID: "test-executor-1",
			AgentID:    "agent-123",
			Severity:   events.SeverityInfo,
			Message:    "Coding agent completed execution",
			Data:       map[string]interface{}{},
		},
	}

	for _, event := range testEvents {
		err := testStore.StoreAgentEvent(ctx, event)
		if err != nil {
			t.Fatalf("Failed to store event: %v", err)
		}
	}

	// Test GetRecentAgentEvents
	t.Run("GetRecentEvents", func(t *testing.T) {
		events, err := testStore.GetRecentAgentEvents(ctx, 10)
		if err != nil {
			t.Fatalf("Failed to get recent events: %v", err)
		}
		if len(events) != 5 {
			t.Errorf("Expected 5 events, got %d", len(events))
		}
	})

	// Test GetAgentEventsByIssue
	t.Run("GetEventsByIssue", func(t *testing.T) {
		events, err := testStore.GetAgentEventsByIssue(ctx, issue.ID)
		if err != nil {
			t.Fatalf("Failed to get events by issue: %v", err)
		}
		if len(events) != 5 {
			t.Errorf("Expected 5 events for issue, got %d", len(events))
		}
	})

	// Test GetAgentEvents with filter
	t.Run("GetEventsWithFilter", func(t *testing.T) {
		filter := events.EventFilter{
			IssueID:  issue.ID,
			Severity: events.SeverityError,
		}
		filteredEvents, err := testStore.GetAgentEvents(ctx, filter)
		if err != nil {
			t.Fatalf("Failed to get filtered events: %v", err)
		}
		if len(filteredEvents) != 1 {
			t.Errorf("Expected 1 error event, got %d", len(filteredEvents))
		}
		if filteredEvents[0].Type != events.EventTypeError {
			t.Errorf("Expected error event, got %s", filteredEvents[0].Type)
		}
	})

	// Test AfterTime filter
	t.Run("GetEventsAfterTime", func(t *testing.T) {
		afterTime := time.Now().Add(-2*time.Minute - 30*time.Second)
		filter := events.EventFilter{
			AfterTime: afterTime,
		}
		recentEvents, err := testStore.GetAgentEvents(ctx, filter)
		if err != nil {
			t.Fatalf("Failed to get events after time: %v", err)
		}
		// Should get the last 2 events (error and completed)
		if len(recentEvents) < 2 {
			t.Errorf("Expected at least 2 recent events, got %d", len(recentEvents))
		}
	})
}
