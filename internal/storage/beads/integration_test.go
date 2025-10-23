package beads

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/steveyegge/vc/internal/events"
	"github.com/steveyegge/vc/internal/types"
)

// TestBeadsIntegration validates that VC storage wraps Beads correctly
func TestBeadsIntegration(t *testing.T) {
	ctx := context.Background()

	// Create temporary database
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	// Create VC storage (wraps Beads)
	store, err := NewVCStorage(ctx, dbPath)
	if err != nil {
		t.Fatalf("Failed to create VC storage: %v", err)
	}
	defer store.Close()

	// Verify database file was created
	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		t.Fatalf("Database file was not created: %s", dbPath)
	}

	t.Run("create regular issue", func(t *testing.T) {
		issue := &types.Issue{
			Title:       "Test issue",
			Description: "Test description",
			Status:      types.StatusOpen,
			Priority:    2,
			IssueType:   types.TypeTask,
		}

		err := store.CreateIssue(ctx, issue, "test")
		if err != nil {
			t.Fatalf("Failed to create issue: %v", err)
		}

		if issue.ID == "" {
			t.Fatal("Issue ID was not generated")
		}

		t.Logf("Created issue: %s", issue.ID)
	})

	t.Run("create mission issue", func(t *testing.T) {
		mission := &types.Issue{
			Title:        "Test mission",
			Description:  "Mission description",
			Status:       types.StatusOpen,
			Priority:     0,
			IssueType:    types.TypeEpic,
			IssueSubtype: types.SubtypeMission,
		}

		err := store.CreateIssue(ctx, mission, "test")
		if err != nil {
			t.Fatalf("Failed to create mission: %v", err)
		}

		if mission.ID == "" {
			t.Fatal("Mission ID was not generated")
		}

		// Verify mission was created in extension table
		retrieved, err := store.GetIssue(ctx, mission.ID)
		if err != nil {
			t.Fatalf("Failed to retrieve mission: %v", err)
		}

		if retrieved.IssueSubtype != types.SubtypeMission {
			t.Errorf("Expected subtype 'mission', got '%s'", retrieved.IssueSubtype)
		}

		t.Logf("Created mission: %s with subtype %s", mission.ID, retrieved.IssueSubtype)
	})

	t.Run("add and retrieve labels", func(t *testing.T) {
		issue := &types.Issue{
			Title:      "Labeled issue",
			Status:     types.StatusOpen,
			Priority:   2,
			IssueType:  types.TypeTask,
		}

		err := store.CreateIssue(ctx, issue, "test")
		if err != nil {
			t.Fatalf("Failed to create issue: %v", err)
		}

		// Add labels (via Beads)
		err = store.AddLabel(ctx, issue.ID, "mission", "test")
		if err != nil {
			t.Fatalf("Failed to add label: %v", err)
		}

		err = store.AddLabel(ctx, issue.ID, "sandbox:mission-100", "test")
		if err != nil {
			t.Fatalf("Failed to add sandbox label: %v", err)
		}

		// Retrieve labels
		labels, err := store.GetLabels(ctx, issue.ID)
		if err != nil {
			t.Fatalf("Failed to get labels: %v", err)
		}

		if len(labels) != 2 {
			t.Errorf("Expected 2 labels, got %d", len(labels))
		}

		t.Logf("Issue %s has labels: %v", issue.ID, labels)
	})

	t.Run("get ready work", func(t *testing.T) {
		// Create a task
		task := &types.Issue{
			Title:      "Ready task",
			Status:     types.StatusOpen,
			Priority:   1,
			IssueType:  types.TypeTask,
		}

		err := store.CreateIssue(ctx, task, "test")
		if err != nil {
			t.Fatalf("Failed to create task: %v", err)
		}

		// Query ready work
		ready, err := store.GetReadyWork(ctx, types.WorkFilter{
			Status: types.StatusOpen,
			Limit:  10,
		})
		if err != nil {
			t.Fatalf("Failed to get ready work: %v", err)
		}

		if len(ready) == 0 {
			t.Error("Expected at least one ready issue")
		}

		t.Logf("Found %d ready issues", len(ready))
	})

	t.Run("executor instance registration", func(t *testing.T) {
		instance := &types.ExecutorInstance{
			InstanceID:    "test-executor-1",
			Hostname:      "localhost",
			PID:           12345,
			Version:       "0.1.0",
			StartedAt:     time.Now(),
			LastHeartbeat: time.Now(),
			Status:        "running",
		}

		err := store.RegisterInstance(ctx, instance)
		if err != nil {
			t.Fatalf("Failed to register instance: %v", err)
		}

		// Retrieve active instances
		instances, err := store.GetActiveInstances(ctx)
		if err != nil {
			t.Fatalf("Failed to get active instances: %v", err)
		}

		if len(instances) != 1 {
			t.Errorf("Expected 1 active instance, got %d", len(instances))
		}

		if instances[0].InstanceID != "test-executor-1" {
			t.Errorf("Expected instance ID 'test-executor-1', got '%s'", instances[0].InstanceID)
		}

		t.Logf("Registered executor instance: %s", instance.InstanceID)
	})

	t.Run("claim and release issue", func(t *testing.T) {
		// Create issue
		issue := &types.Issue{
			Title:      "Claimable task",
			Status:     types.StatusOpen,
			Priority:   2,
			IssueType:  types.TypeTask,
		}

		err := store.CreateIssue(ctx, issue, "test")
		if err != nil {
			t.Fatalf("Failed to create issue: %v", err)
		}

		// Claim issue
		err = store.ClaimIssue(ctx, issue.ID, "test-executor-1")
		if err != nil {
			t.Fatalf("Failed to claim issue: %v", err)
		}

		// Verify execution state
		state, err := store.GetExecutionState(ctx, issue.ID)
		if err != nil {
			t.Fatalf("Failed to get execution state: %v", err)
		}

		if state.State != "claimed" {
			t.Errorf("Expected state 'claimed', got '%s'", state.State)
		}

		// Release issue
		err = store.ReleaseIssue(ctx, issue.ID)
		if err != nil {
			t.Fatalf("Failed to release issue: %v", err)
		}

		t.Logf("Successfully claimed and released issue: %s", issue.ID)
	})
}

// TestBeadsExtensionTablesCreated verifies extension tables are created
func TestBeadsExtensionTablesCreated(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	store, err := NewVCStorage(ctx, dbPath)
	if err != nil {
		t.Fatalf("Failed to create VC storage: %v", err)
	}
	defer store.Close()

	// Query each extension table to verify it exists
	tables := []string{
		"vc_mission_state",
		"vc_agent_events",
		"vc_executor_instances",
		"vc_issue_execution_state",
		"vc_execution_history",
	}

	for _, table := range tables {
		var count int
		err := store.db.QueryRowContext(ctx,
			"SELECT COUNT(*) FROM "+table,
		).Scan(&count)

		if err != nil {
			t.Errorf("Extension table '%s' does not exist or is not accessible: %v", table, err)
		} else {
			t.Logf("✓ Extension table '%s' exists (count=%d)", table, count)
		}
	}
}

// TestBeadsCoreTables verifies that Beads core tables exist
func TestBeadsCoreTables(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	store, err := NewVCStorage(ctx, dbPath)
	if err != nil {
		t.Fatalf("Failed to create VC storage: %v", err)
	}
	defer store.Close()

	// Query Beads core tables
	beadsTables := []string{
		"issues",
		"dependencies",
		"labels",
		"comments",
		"events",
	}

	for _, table := range beadsTables {
		var count int
		err := store.db.QueryRowContext(ctx,
			"SELECT COUNT(*) FROM "+table,
		).Scan(&count)

		if err != nil {
			t.Errorf("Beads core table '%s' does not exist: %v", table, err)
		} else {
			t.Logf("✓ Beads table '%s' exists (count=%d)", table, count)
		}
	}
}

// TestGetAgentEvents validates GetAgentEvents filtering functionality
func TestGetAgentEvents(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	store, err := NewVCStorage(ctx, dbPath)
	if err != nil {
		t.Fatalf("Failed to create VC storage: %v", err)
	}
	defer store.Close()

	// Create test issues
	issue := &types.Issue{
		Title:      "Test issue for events",
		Status:     types.StatusOpen,
		Priority:   2,
		IssueType:  types.TypeTask,
	}
	err = store.CreateIssue(ctx, issue, "test")
	if err != nil {
		t.Fatalf("Failed to create issue: %v", err)
	}

	otherIssue := &types.Issue{
		Title:      "Other test issue",
		Status:     types.StatusOpen,
		Priority:   2,
		IssueType:  types.TypeTask,
	}
	err = store.CreateIssue(ctx, otherIssue, "test")
	if err != nil {
		t.Fatalf("Failed to create other issue: %v", err)
	}

	// Create test events with different attributes
	now := time.Now()
	testEvents := []events.AgentEvent{
		{
			Timestamp: now.Add(-3 * time.Hour),
			IssueID:   issue.ID,
			Type:      events.EventTypeProgress,
			Severity:  events.SeverityInfo,
			Message:   "Progress event 1",
		},
		{
			Timestamp: now.Add(-2 * time.Hour),
			IssueID:   issue.ID,
			Type:      events.EventTypeError,
			Severity:  events.SeverityError,
			Message:   "Error event",
		},
		{
			Timestamp: now.Add(-1 * time.Hour),
			IssueID:   issue.ID,
			Type:      events.EventTypeProgress,
			Severity:  events.SeverityInfo,
			Message:   "Progress event 2",
		},
		{
			Timestamp: now,
			IssueID:   otherIssue.ID,
			Type:      events.EventTypeProgress,
			Severity:  events.SeverityInfo,
			Message:   "Other issue event",
		},
	}

	// Store test events
	for _, event := range testEvents {
		err := store.StoreAgentEvent(ctx, &event)
		if err != nil {
			t.Fatalf("Failed to store event: %v", err)
		}
	}

	t.Run("filter by issue ID", func(t *testing.T) {
		filter := events.EventFilter{
			IssueID: issue.ID,
		}
		results, err := store.GetAgentEvents(ctx, filter)
		if err != nil {
			t.Fatalf("GetAgentEvents failed: %v", err)
		}

		if len(results) != 3 {
			t.Errorf("Expected 3 events for issue %s, got %d", issue.ID, len(results))
		}

		for _, e := range results {
			if e.IssueID != issue.ID {
				t.Errorf("Expected issue ID %s, got %s", issue.ID, e.IssueID)
			}
		}
	})

	t.Run("filter by type", func(t *testing.T) {
		filter := events.EventFilter{
			Type: events.EventTypeError,
		}
		results, err := store.GetAgentEvents(ctx, filter)
		if err != nil {
			t.Fatalf("GetAgentEvents failed: %v", err)
		}

		if len(results) != 1 {
			t.Errorf("Expected 1 error event, got %d", len(results))
		}

		if results[0].Type != events.EventTypeError {
			t.Errorf("Expected type %s, got %s", events.EventTypeError, results[0].Type)
		}
	})

	t.Run("filter by severity", func(t *testing.T) {
		filter := events.EventFilter{
			Severity: events.SeverityError,
		}
		results, err := store.GetAgentEvents(ctx, filter)
		if err != nil {
			t.Fatalf("GetAgentEvents failed: %v", err)
		}

		if len(results) != 1 {
			t.Errorf("Expected 1 error severity event, got %d", len(results))
		}

		if results[0].Severity != events.SeverityError {
			t.Errorf("Expected severity %s, got %s", events.SeverityError, results[0].Severity)
		}
	})

	t.Run("filter by time range", func(t *testing.T) {
		filter := events.EventFilter{
			AfterTime:  now.Add(-2*time.Hour - 30*time.Minute),
			BeforeTime: now.Add(-30 * time.Minute),
		}
		results, err := store.GetAgentEvents(ctx, filter)
		if err != nil {
			t.Fatalf("GetAgentEvents failed: %v", err)
		}

		if len(results) != 2 {
			t.Errorf("Expected 2 events in time range, got %d", len(results))
		}
	})

	t.Run("filter with limit", func(t *testing.T) {
		filter := events.EventFilter{
			Limit: 2,
		}
		results, err := store.GetAgentEvents(ctx, filter)
		if err != nil {
			t.Fatalf("GetAgentEvents failed: %v", err)
		}

		if len(results) != 2 {
			t.Errorf("Expected 2 events (limit), got %d", len(results))
		}

		// Verify ordering (DESC by timestamp - newest first)
		if !results[0].Timestamp.After(results[1].Timestamp) {
			t.Error("Expected results ordered by timestamp DESC")
		}
	})

	t.Run("combined filters", func(t *testing.T) {
		filter := events.EventFilter{
			IssueID:  issue.ID,
			Type:     events.EventTypeProgress,
			Severity: events.SeverityInfo,
		}
		results, err := store.GetAgentEvents(ctx, filter)
		if err != nil {
			t.Fatalf("GetAgentEvents failed: %v", err)
		}

		if len(results) != 2 {
			t.Errorf("Expected 2 progress events for issue, got %d", len(results))
		}

		for _, e := range results {
			if e.IssueID != issue.ID {
				t.Errorf("Expected issue ID %s, got %s", issue.ID, e.IssueID)
			}
			if e.Type != events.EventTypeProgress {
				t.Errorf("Expected type %s, got %s", events.EventTypeProgress, e.Type)
			}
			if e.Severity != events.SeverityInfo {
				t.Errorf("Expected severity %s, got %s", events.SeverityInfo, e.Severity)
			}
		}
	})

	t.Run("no filters returns all events", func(t *testing.T) {
		filter := events.EventFilter{}
		results, err := store.GetAgentEvents(ctx, filter)
		if err != nil {
			t.Fatalf("GetAgentEvents failed: %v", err)
		}

		if len(results) != 4 {
			t.Errorf("Expected 4 total events, got %d", len(results))
		}
	})
}

// TestAgentEventDataPersistence verifies that Data field is properly stored and retrieved
func TestAgentEventDataPersistence(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	store, err := NewVCStorage(ctx, dbPath)
	if err != nil {
		t.Fatalf("Failed to create VC storage: %v", err)
	}
	defer store.Close()

	// Create test issue
	issue := &types.Issue{
		Title:      "Test issue for data persistence",
		Status:     types.StatusOpen,
		Priority:   2,
		IssueType:  types.TypeTask,
	}
	err = store.CreateIssue(ctx, issue, "test")
	if err != nil {
		t.Fatalf("Failed to create issue: %v", err)
	}

	// Create event with complex Data structure
	now := time.Now()
	originalData := map[string]interface{}{
		"tool_name":         "Read",
		"file_path":         "/tmp/test.go",
		"lines_read":        float64(100),
		"success":           true,
		"nested_object": map[string]interface{}{
			"key1": "value1",
			"key2": float64(42),
		},
		"array_field": []interface{}{"item1", "item2", "item3"},
	}

	event := &events.AgentEvent{
		Timestamp: now,
		IssueID:   issue.ID,
		Type:      events.EventTypeAgentToolUse,
		Severity:  events.SeverityInfo,
		Message:   "Test event with data",
		Data:      originalData,
	}

	err = store.StoreAgentEvent(ctx, event)
	if err != nil {
		t.Fatalf("Failed to store event: %v", err)
	}

	t.Run("GetAgentEventsByIssue preserves Data", func(t *testing.T) {
		results, err := store.GetAgentEventsByIssue(ctx, issue.ID)
		if err != nil {
			t.Fatalf("GetAgentEventsByIssue failed: %v", err)
		}

		if len(results) != 1 {
			t.Fatalf("Expected 1 event, got %d", len(results))
		}

		retrieved := results[0]
		if retrieved.Data == nil {
			t.Fatal("Data field is nil - unmarshaling failed")
		}

		// Verify all fields
		if retrieved.Data["tool_name"] != "Read" {
			t.Errorf("Expected tool_name='Read', got %v", retrieved.Data["tool_name"])
		}
		if retrieved.Data["file_path"] != "/tmp/test.go" {
			t.Errorf("Expected file_path='/tmp/test.go', got %v", retrieved.Data["file_path"])
		}
		if retrieved.Data["lines_read"] != float64(100) {
			t.Errorf("Expected lines_read=100, got %v", retrieved.Data["lines_read"])
		}
		if retrieved.Data["success"] != true {
			t.Errorf("Expected success=true, got %v", retrieved.Data["success"])
		}

		// Verify nested object
		nested, ok := retrieved.Data["nested_object"].(map[string]interface{})
		if !ok {
			t.Fatal("nested_object is not a map")
		}
		if nested["key1"] != "value1" {
			t.Errorf("Expected nested key1='value1', got %v", nested["key1"])
		}
		if nested["key2"] != float64(42) {
			t.Errorf("Expected nested key2=42, got %v", nested["key2"])
		}

		// Verify array
		arr, ok := retrieved.Data["array_field"].([]interface{})
		if !ok {
			t.Fatal("array_field is not an array")
		}
		if len(arr) != 3 {
			t.Errorf("Expected array length 3, got %d", len(arr))
		}
		if arr[0] != "item1" {
			t.Errorf("Expected arr[0]='item1', got %v", arr[0])
		}
	})

	t.Run("GetRecentAgentEvents preserves Data", func(t *testing.T) {
		results, err := store.GetRecentAgentEvents(ctx, 10)
		if err != nil {
			t.Fatalf("GetRecentAgentEvents failed: %v", err)
		}

		if len(results) == 0 {
			t.Fatal("Expected at least 1 event")
		}

		// Find our test event
		var retrieved *events.AgentEvent
		for _, e := range results {
			if e.IssueID == issue.ID {
				retrieved = e
				break
			}
		}

		if retrieved == nil {
			t.Fatal("Test event not found in recent events")
		}

		if retrieved.Data == nil {
			t.Fatal("Data field is nil - unmarshaling failed")
		}

		if retrieved.Data["tool_name"] != "Read" {
			t.Errorf("Expected tool_name='Read', got %v", retrieved.Data["tool_name"])
		}
	})

	t.Run("GetAgentEvents preserves Data", func(t *testing.T) {
		filter := events.EventFilter{
			IssueID: issue.ID,
		}
		results, err := store.GetAgentEvents(ctx, filter)
		if err != nil {
			t.Fatalf("GetAgentEvents failed: %v", err)
		}

		if len(results) != 1 {
			t.Fatalf("Expected 1 event, got %d", len(results))
		}

		retrieved := results[0]
		if retrieved.Data == nil {
			t.Fatal("Data field is nil - unmarshaling failed")
		}

		if retrieved.Data["tool_name"] != "Read" {
			t.Errorf("Expected tool_name='Read', got %v", retrieved.Data["tool_name"])
		}
	})

	t.Run("empty Data field handled correctly", func(t *testing.T) {
		// Create event with nil Data
		eventNoData := &events.AgentEvent{
			Timestamp: now.Add(1 * time.Minute),
			IssueID:   issue.ID,
			Type:      events.EventTypeProgress,
			Severity:  events.SeverityInfo,
			Message:   "Event without data",
			Data:      nil,
		}

		err := store.StoreAgentEvent(ctx, eventNoData)
		if err != nil {
			t.Fatalf("Failed to store event without data: %v", err)
		}

		results, err := store.GetAgentEventsByIssue(ctx, issue.ID)
		if err != nil {
			t.Fatalf("GetAgentEventsByIssue failed: %v", err)
		}

		// Should have 2 events now
		if len(results) != 2 {
			t.Errorf("Expected 2 events, got %d", len(results))
		}

		// Find the event without data
		var noDataEvent *events.AgentEvent
		for _, e := range results {
			if e.Type == events.EventTypeProgress {
				noDataEvent = e
				break
			}
		}

		if noDataEvent == nil {
			t.Fatal("Event without data not found")
		}

		// Data should be nil or empty
		if noDataEvent.Data != nil && len(noDataEvent.Data) > 0 {
			t.Errorf("Expected empty Data, got %v", noDataEvent.Data)
		}
	})
}
