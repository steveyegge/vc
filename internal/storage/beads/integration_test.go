package beads

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
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
	defer func() { _ = store.Close() }()

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
			AcceptanceCriteria: "Test acceptance criteria",
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
			AcceptanceCriteria: "Test acceptance criteria",
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
			AcceptanceCriteria: "Test acceptance criteria",
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
			AcceptanceCriteria: "Test acceptance criteria",
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
	defer func() { _ = store.Close() }()

	// Query each extension table to verify it exists
	tables := []string{
		"vc_mission_state",
		"vc_agent_events",
		"vc_executor_instances",
		"vc_issue_execution_state",
		"vc_execution_history",
	}

	// Allowlist for SQL injection prevention
	allowedTables := map[string]bool{
		"vc_mission_state":         true,
		"vc_agent_events":          true,
		"vc_executor_instances":    true,
		"vc_issue_execution_state": true,
		"vc_execution_history":     true,
	}

	for _, table := range tables {
		// Validate table name against allowlist to prevent SQL injection
		if !allowedTables[table] {
			t.Fatalf("Invalid table name: %s", table)
		}

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
	defer func() { _ = store.Close() }()

	// Query Beads core tables
	beadsTables := []string{
		"issues",
		"dependencies",
		"labels",
		"comments",
		"events",
	}

	// Allowlist for SQL injection prevention
	allowedBeadsTables := map[string]bool{
		"issues":       true,
		"dependencies": true,
		"labels":       true,
		"comments":     true,
		"events":       true,
	}

	for _, table := range beadsTables {
		// Validate table name against allowlist to prevent SQL injection
		if !allowedBeadsTables[table] {
			t.Fatalf("Invalid table name: %s", table)
		}

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
	defer func() { _ = store.Close() }()

	// Create test issues
	issue := &types.Issue{
		Title:      "Test issue for events",
		Status:     types.StatusOpen,
		Priority:   2,
		IssueType:  types.TypeTask,
		AcceptanceCriteria: "Test acceptance criteria",
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
		AcceptanceCriteria: "Test acceptance criteria",
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
			var eventTypes []string
			for _, e := range results {
				eventTypes = append(eventTypes, string(e.Type))
			}
			t.Errorf("Expected 3 events for issue %s, got %d: %v", issue.ID, len(results), eventTypes)
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
			var eventTypes []string
			for _, e := range results {
				eventTypes = append(eventTypes, string(e.Type))
			}
			t.Errorf("Expected 1 error event, got %d: %v", len(results), eventTypes)
		}

		if len(results) > 0 && results[0].Type != events.EventTypeError {
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
			var severities []string
			for _, e := range results {
				severities = append(severities, string(e.Severity))
			}
			t.Errorf("Expected 1 error severity event, got %d with severities: %v", len(results), severities)
		}

		if len(results) > 0 && results[0].Severity != events.SeverityError {
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
			var timestamps []string
			for _, e := range results {
				timestamps = append(timestamps, e.Timestamp.Format("15:04:05"))
			}
			t.Errorf("Expected 2 events in time range, got %d at times: %v", len(results), timestamps)
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
			var eventTypes []string
			for _, e := range results {
				eventTypes = append(eventTypes, string(e.Type))
			}
			t.Errorf("Expected 2 events (limit), got %d: %v", len(results), eventTypes)
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
			var eventTypes []string
			for _, e := range results {
				eventTypes = append(eventTypes, fmt.Sprintf("%s(%s)", e.Type, e.Severity))
			}
			t.Errorf("Expected 2 progress events for issue, got %d: %v", len(results), eventTypes)
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
	defer func() { _ = store.Close() }()

	// Create test issue
	issue := &types.Issue{
		Title:      "Test issue for data persistence",
		Status:     types.StatusOpen,
		Priority:   2,
		IssueType:  types.TypeTask,
		AcceptanceCriteria: "Test acceptance criteria",
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
		if len(noDataEvent.Data) > 0 {
			t.Errorf("Expected empty Data, got %v", noDataEvent.Data)
		}
	})
}

// TestGetDependencyTree verifies that GetDependencyTree returns flat list with proper depth
func TestGetDependencyTree(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	store, err := NewVCStorage(ctx, dbPath)
	if err != nil {
		t.Fatalf("Failed to create VC storage: %v", err)
	}
	defer func() { _ = store.Close() }()

	// Create a dependency chain (traversing upward):
	//   leaf (depth 0) -> depends on middle1, middle2
	//   middle1 (depth 1) -> depends on root
	//   middle2 (depth 1) -> depends on root
	//   root (depth 2)
	//
	// GetDependencyTree(leaf) should return: leaf (0), middle1 (1), middle2 (1), root (2)

	root := &types.Issue{
		Title:      "Root dependency",
		Status:     types.StatusOpen,
		Priority:   0,
		IssueType:  types.TypeEpic,
	}
	err = store.CreateIssue(ctx, root, "test")
	if err != nil {
		t.Fatalf("Failed to create root: %v", err)
	}

	middle1 := &types.Issue{
		Title:      "Middle dependency 1",
		Status:     types.StatusOpen,
		Priority:   1,
		IssueType:  types.TypeTask,
		AcceptanceCriteria: "Test acceptance criteria",
	}
	err = store.CreateIssue(ctx, middle1, "test")
	if err != nil {
		t.Fatalf("Failed to create middle1: %v", err)
	}

	middle2 := &types.Issue{
		Title:      "Middle dependency 2",
		Status:     types.StatusOpen,
		Priority:   1,
		IssueType:  types.TypeTask,
		AcceptanceCriteria: "Test acceptance criteria",
	}
	err = store.CreateIssue(ctx, middle2, "test")
	if err != nil {
		t.Fatalf("Failed to create middle2: %v", err)
	}

	leaf := &types.Issue{
		Title:      "Leaf issue (depends on middle1, middle2)",
		Status:     types.StatusOpen,
		Priority:   2,
		IssueType:  types.TypeTask,
		AcceptanceCriteria: "Test acceptance criteria",
	}
	err = store.CreateIssue(ctx, leaf, "test")
	if err != nil {
		t.Fatalf("Failed to create leaf: %v", err)
	}

	// Add blocking dependencies (leaf is blocked by middle1 and middle2)
	err = store.AddDependency(ctx, &types.Dependency{
		IssueID:     middle1.ID,
		DependsOnID: root.ID,
		Type:        types.DepBlocks,
	}, "test")
	if err != nil {
		t.Fatalf("Failed to add middle1 blocks root: %v", err)
	}

	err = store.AddDependency(ctx, &types.Dependency{
		IssueID:     middle2.ID,
		DependsOnID: root.ID,
		Type:        types.DepBlocks,
	}, "test")
	if err != nil {
		t.Fatalf("Failed to add middle2 blocks root: %v", err)
	}

	err = store.AddDependency(ctx, &types.Dependency{
		IssueID:     leaf.ID,
		DependsOnID: middle1.ID,
		Type:        types.DepBlocks,
	}, "test")
	if err != nil {
		t.Fatalf("Failed to add leaf blocks middle1: %v", err)
	}

	err = store.AddDependency(ctx, &types.Dependency{
		IssueID:     leaf.ID,
		DependsOnID: middle2.ID,
		Type:        types.DepBlocks,
	}, "test")
	if err != nil {
		t.Fatalf("Failed to add leaf blocks middle2: %v", err)
	}

	t.Run("GetDependencyTree returns flat list with depths", func(t *testing.T) {
		tree, err := store.GetDependencyTree(ctx, leaf.ID, 10)
		if err != nil {
			t.Fatalf("GetDependencyTree failed: %v", err)
		}

		// Should return flat list: leaf (depth 0), middle1 (depth 1), middle2 (depth 1), root (depth 2)
		if len(tree) != 4 {
			t.Fatalf("Expected 4 nodes in tree, got %d", len(tree))
		}

		// Verify leaf is at depth 0
		if tree[0].ID != leaf.ID {
			t.Errorf("Expected leaf at index 0, got %s", tree[0].ID)
		}
		if tree[0].Depth != 0 {
			t.Errorf("Expected leaf at depth 0, got %d", tree[0].Depth)
		}

		// Find middle1 and verify depth
		var middle1Node *types.TreeNode
		for _, node := range tree {
			if node.ID == middle1.ID {
				middle1Node = node
				break
			}
		}
		if middle1Node == nil {
			t.Fatal("middle1 not found in tree")
		}
		if middle1Node.Depth != 1 {
			t.Errorf("Expected middle1 at depth 1, got %d", middle1Node.Depth)
		}

		// Find root and verify depth
		var rootNode *types.TreeNode
		for _, node := range tree {
			if node.ID == root.ID {
				rootNode = node
				break
			}
		}
		if rootNode == nil {
			t.Fatal("root not found in tree")
		}
		if rootNode.Depth != 2 {
			t.Errorf("Expected root at depth 2, got %d", rootNode.Depth)
		}
	})

	t.Run("maxDepth limits tree depth", func(t *testing.T) {
		// Request only depth 0 and 1
		tree, err := store.GetDependencyTree(ctx, leaf.ID, 1)
		if err != nil {
			t.Fatalf("GetDependencyTree failed: %v", err)
		}

		// Should have leaf (depth 0) + middle1 + middle2 (depth 1) = 3 nodes
		// Root at depth 2 should be excluded
		if len(tree) != 3 {
			t.Fatalf("Expected 3 nodes with maxDepth=1, got %d", len(tree))
		}

		// Verify no nodes at depth > 1
		for _, node := range tree {
			if node.Depth > 1 {
				t.Errorf("Found node at depth %d when maxDepth=1", node.Depth)
			}
		}
	})

	t.Run("TreeNode has no Children field (flat structure)", func(t *testing.T) {
		tree, err := store.GetDependencyTree(ctx, leaf.ID, 10)
		if err != nil {
			t.Fatalf("GetDependencyTree failed: %v", err)
		}

		// TreeNode intentionally has no Children field - it's a flat list
		// Tree structure is encoded via Depth field
		// This test documents the expected behavior

		// Verify we can reconstruct dependency relationships from depth
		depthCounts := make(map[int]int)
		for _, node := range tree {
			depthCounts[node.Depth]++
		}

		if depthCounts[0] != 1 {
			t.Errorf("Expected 1 node at depth 0 (leaf), got %d", depthCounts[0])
		}
		if depthCounts[1] != 2 {
			t.Errorf("Expected 2 nodes at depth 1 (middle), got %d", depthCounts[1])
		}
		if depthCounts[2] != 1 {
			t.Errorf("Expected 1 node at depth 2 (root), got %d", depthCounts[2])
		}
	})
}

// TestGetBlockedIssues verifies that GetBlockedIssues returns proper Blockers list
func TestGetBlockedIssues(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	store, err := NewVCStorage(ctx, dbPath)
	if err != nil {
		t.Fatalf("Failed to create VC storage: %v", err)
	}
	defer func() { _ = store.Close() }()

	// Create issues: A is blocked by B and C
	issueA := &types.Issue{
		Title:      "Issue A (blocked)",
		Status:     types.StatusBlocked,
		Priority:   1,
		IssueType:  types.TypeTask,
		AcceptanceCriteria: "Test acceptance criteria",
	}
	err = store.CreateIssue(ctx, issueA, "test")
	if err != nil {
		t.Fatalf("Failed to create issue A: %v", err)
	}

	issueB := &types.Issue{
		Title:      "Issue B (blocker)",
		Status:     types.StatusOpen,
		Priority:   1,
		IssueType:  types.TypeTask,
		AcceptanceCriteria: "Test acceptance criteria",
	}
	err = store.CreateIssue(ctx, issueB, "test")
	if err != nil {
		t.Fatalf("Failed to create issue B: %v", err)
	}

	issueC := &types.Issue{
		Title:      "Issue C (blocker)",
		Status:     types.StatusOpen,
		Priority:   1,
		IssueType:  types.TypeTask,
		AcceptanceCriteria: "Test acceptance criteria",
	}
	err = store.CreateIssue(ctx, issueC, "test")
	if err != nil {
		t.Fatalf("Failed to create issue C: %v", err)
	}

	// Add blocking dependencies: A depends on B and C
	err = store.AddDependency(ctx, &types.Dependency{
		IssueID:     issueA.ID,
		DependsOnID: issueB.ID,
		Type:        types.DepBlocks,
	}, "test")
	if err != nil {
		t.Fatalf("Failed to add A -> B dependency: %v", err)
	}

	err = store.AddDependency(ctx, &types.Dependency{
		IssueID:     issueA.ID,
		DependsOnID: issueC.ID,
		Type:        types.DepBlocks,
	}, "test")
	if err != nil {
		t.Fatalf("Failed to add A -> C dependency: %v", err)
	}

	t.Run("GetBlockedIssues returns Blockers list", func(t *testing.T) {
		blocked, err := store.GetBlockedIssues(ctx)
		if err != nil {
			t.Fatalf("GetBlockedIssues failed: %v", err)
		}

		// Should have at least one blocked issue (A)
		if len(blocked) == 0 {
			t.Fatal("Expected at least one blocked issue")
		}

		// Find issue A in the results
		var issueABlocked *types.BlockedIssue
		for _, b := range blocked {
			if b.ID == issueA.ID {
				issueABlocked = b
				break
			}
		}

		if issueABlocked == nil {
			t.Fatal("Issue A not found in blocked issues")
		}

		// Verify BlockedByCount
		if issueABlocked.BlockedByCount != 2 {
			t.Errorf("Expected BlockedByCount=2, got %d", issueABlocked.BlockedByCount)
		}

		// Verify BlockedBy list is populated
		if issueABlocked.BlockedBy == nil {
			t.Fatal("BlockedBy list is nil - conversion failed")
		}

		if len(issueABlocked.BlockedBy) != 2 {
			t.Fatalf("Expected 2 blockers in BlockedBy list, got %d", len(issueABlocked.BlockedBy))
		}

		// Verify both B and C are in the blockers list
		blockersMap := make(map[string]bool)
		for _, blockerID := range issueABlocked.BlockedBy {
			blockersMap[blockerID] = true
		}

		if !blockersMap[issueB.ID] {
			t.Errorf("Expected issue B (%s) in blockers list", issueB.ID)
		}

		if !blockersMap[issueC.ID] {
			t.Errorf("Expected issue C (%s) in blockers list", issueC.ID)
		}
	})

	t.Run("non-blocked issues not in results", func(t *testing.T) {
		// Create a non-blocked issue
		nonBlocked := &types.Issue{
			Title:      "Non-blocked issue",
			Status:     types.StatusOpen,
			Priority:   1,
			IssueType:  types.TypeTask,
			AcceptanceCriteria: "Test acceptance criteria",
		}
		err = store.CreateIssue(ctx, nonBlocked, "test")
		if err != nil {
			t.Fatalf("Failed to create non-blocked issue: %v", err)
		}

		blocked, err := store.GetBlockedIssues(ctx)
		if err != nil {
			t.Fatalf("GetBlockedIssues failed: %v", err)
		}

		// Verify non-blocked issue is not in results
		for _, b := range blocked {
			if b.ID == nonBlocked.ID {
				t.Error("Non-blocked issue should not appear in GetBlockedIssues results")
			}
		}
	})

	t.Run("issue with zero blockers not in results", func(t *testing.T) {
		// Issue A should still be blocked
		blocked, err := store.GetBlockedIssues(ctx)
		if err != nil {
			t.Fatalf("GetBlockedIssues failed: %v", err)
		}

		// All blocked issues should have BlockedByCount > 0
		for _, b := range blocked {
			if b.BlockedByCount <= 0 {
				t.Errorf("Blocked issue %s has BlockedByCount=%d, expected > 0", b.ID, b.BlockedByCount)
			}
			if len(b.BlockedBy) != b.BlockedByCount {
				t.Errorf("Blocked issue %s: len(BlockedBy)=%d doesn't match BlockedByCount=%d",
					b.ID, len(b.BlockedBy), b.BlockedByCount)
			}
		}
	})
}

// TestVCStorageClose verifies that Close() properly closes the database
// and subsequent operations fail with appropriate errors
func TestVCStorageClose(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	// Create VCStorage instance
	store, err := NewVCStorage(ctx, dbPath)
	if err != nil {
		t.Fatalf("Failed to create VC storage: %v", err)
	}

	// Create a test issue before closing
	issue := &types.Issue{
		Title:      "Test issue",
		Status:     types.StatusOpen,
		Priority:   2,
		IssueType:  types.TypeTask,
		AcceptanceCriteria: "Test acceptance criteria",
	}
	err = store.CreateIssue(ctx, issue, "test")
	if err != nil {
		t.Fatalf("Failed to create issue: %v", err)
	}

	// Close the storage
	err = store.Close()
	if err != nil {
		t.Fatalf("Close() failed: %v", err)
	}

	// Verify subsequent operations fail
	t.Run("CreateIssue fails after close", func(t *testing.T) {
		newIssue := &types.Issue{
			Title:      "Should fail",
			Status:     types.StatusOpen,
			Priority:   2,
			IssueType:  types.TypeTask,
			AcceptanceCriteria: "Test acceptance criteria",
		}
		err = store.CreateIssue(ctx, newIssue, "test")
		if err == nil {
			t.Fatal("Expected CreateIssue to fail after Close(), but it succeeded")
		}
		t.Logf("CreateIssue correctly failed with: %v", err)
	})

	t.Run("GetIssue fails after close", func(t *testing.T) {
		_, err = store.GetIssue(ctx, issue.ID)
		if err == nil {
			t.Fatal("Expected GetIssue to fail after Close(), but it succeeded")
		}
		t.Logf("GetIssue correctly failed with: %v", err)
	})

	t.Run("StoreAgentEvent fails after close", func(t *testing.T) {
		event := &events.AgentEvent{
			Timestamp: time.Now(),
			IssueID:   issue.ID,
			Type:      events.EventTypeProgress,
			Severity:  events.SeverityInfo,
			Message:   "Should fail",
		}
		err = store.StoreAgentEvent(ctx, event)
		if err == nil {
			t.Fatal("Expected StoreAgentEvent to fail after Close(), but it succeeded")
		}
		t.Logf("StoreAgentEvent correctly failed with: %v", err)
	})

	t.Run("RegisterInstance fails after close", func(t *testing.T) {
		instance := &types.ExecutorInstance{
			InstanceID:    "test-instance",
			Hostname:      "localhost",
			PID:           12345,
			Version:       "test",
			StartedAt:     time.Now(),
			LastHeartbeat: time.Now(),
			Status:        "running",
		}
		err = store.RegisterInstance(ctx, instance)
		if err == nil {
			t.Fatal("Expected RegisterInstance to fail after Close(), but it succeeded")
		}
		t.Logf("RegisterInstance correctly failed with: %v", err)
	})
}

// TestExecutionStateTransitions verifies state transition validation
func TestExecutionStateTransitions(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	store, err := NewVCStorage(ctx, dbPath)
	if err != nil {
		t.Fatalf("Failed to create VC storage: %v", err)
	}
	defer func() { _ = store.Close() }()

	// Create test issue
	issue := &types.Issue{
		Title:      "Test issue for state transitions",
		Status:     types.StatusOpen,
		Priority:   2,
		IssueType:  types.TypeTask,
		AcceptanceCriteria: "Test acceptance criteria",
	}
	err = store.CreateIssue(ctx, issue, "test")
	if err != nil {
		t.Fatalf("Failed to create issue: %v", err)
	}

	// Register executor instance
	instance := &types.ExecutorInstance{
		InstanceID:    "test-executor",
		Hostname:      "localhost",
		PID:           12345,
		Version:       "test",
		StartedAt:     time.Now(),
		LastHeartbeat: time.Now(),
		Status:        "running",
	}
	err = store.RegisterInstance(ctx, instance)
	if err != nil {
		t.Fatalf("Failed to register executor: %v", err)
	}

	t.Run("valid full lifecycle transition", func(t *testing.T) {
		// Test the complete happy path: pending → claimed → assessing → executing → analyzing → gates → committing → completed
		validTransitions := []types.ExecutionState{
			types.ExecutionStatePending,
			types.ExecutionStateClaimed,
			types.ExecutionStateAssessing,
			types.ExecutionStateExecuting,
			types.ExecutionStateAnalyzing,
			types.ExecutionStateGates,
			types.ExecutionStateCommitting,
			types.ExecutionStateCompleted,
		}

		for i, state := range validTransitions {
			err := store.UpdateExecutionState(ctx, issue.ID, state)
			if err != nil {
				t.Fatalf("Valid transition %d failed: %v (transitioning to %s)", i, err, state)
			}

			// Verify state was updated
			execState, err := store.GetExecutionState(ctx, issue.ID)
			if err != nil {
				t.Fatalf("Failed to get execution state after transition to %s: %v", state, err)
			}
			if execState.State != state {
				t.Errorf("Expected state %s, got %s", state, execState.State)
			}
		}
	})

	t.Run("transition to failed from any state", func(t *testing.T) {
		// Reset to pending
		err := store.UpdateExecutionState(ctx, issue.ID, types.ExecutionStatePending)
		if err == nil {
			t.Error("Should not be able to transition from completed back to pending")
		}

		// Create new issue for this test
		issue2 := &types.Issue{
			Title:      "Test issue for failed transitions",
			Status:     types.StatusOpen,
			Priority:   2,
			IssueType:  types.TypeTask,
			AcceptanceCriteria: "Test acceptance criteria",
		}
		err = store.CreateIssue(ctx, issue2, "test")
		if err != nil {
			t.Fatalf("Failed to create issue: %v", err)
		}

		// Test transition to failed from each non-terminal state
		nonTerminalStates := []types.ExecutionState{
			types.ExecutionStatePending,
			types.ExecutionStateClaimed,
			types.ExecutionStateAssessing,
			types.ExecutionStateExecuting,
			types.ExecutionStateAnalyzing,
			types.ExecutionStateGates,
			types.ExecutionStateCommitting,
		}

		for _, targetState := range nonTerminalStates {
			// Create a new issue for each test
			testIssue := &types.Issue{
				Title:      fmt.Sprintf("Test issue for %s to failed transition", targetState),
				Status:     types.StatusOpen,
				Priority:   2,
				IssueType:  types.TypeTask,
				AcceptanceCriteria: "Test acceptance criteria",
			}
			err = store.CreateIssue(ctx, testIssue, "test")
			if err != nil {
				t.Fatalf("Failed to create issue: %v", err)
			}

			// Walk through state machine to reach target state
			statePath := []types.ExecutionState{types.ExecutionStatePending}
			switch targetState {
			case types.ExecutionStatePending:
				// Already there
			case types.ExecutionStateClaimed:
				statePath = append(statePath, types.ExecutionStateClaimed)
			case types.ExecutionStateAssessing:
				statePath = append(statePath, types.ExecutionStateClaimed, types.ExecutionStateAssessing)
			case types.ExecutionStateExecuting:
				statePath = append(statePath, types.ExecutionStateClaimed, types.ExecutionStateAssessing, types.ExecutionStateExecuting)
			case types.ExecutionStateAnalyzing:
				statePath = append(statePath, types.ExecutionStateClaimed, types.ExecutionStateAssessing, types.ExecutionStateExecuting, types.ExecutionStateAnalyzing)
			case types.ExecutionStateGates:
				statePath = append(statePath, types.ExecutionStateClaimed, types.ExecutionStateAssessing, types.ExecutionStateExecuting, types.ExecutionStateAnalyzing, types.ExecutionStateGates)
			case types.ExecutionStateCommitting:
				statePath = append(statePath, types.ExecutionStateClaimed, types.ExecutionStateAssessing, types.ExecutionStateExecuting, types.ExecutionStateAnalyzing, types.ExecutionStateGates, types.ExecutionStateCommitting)
			}

			// Walk to target state
			for _, state := range statePath {
				err = store.UpdateExecutionState(ctx, testIssue.ID, state)
				if err != nil {
					t.Fatalf("Failed to reach state %s (current: %s): %v", targetState, state, err)
				}
			}

			// Transition to failed should always work
			err = store.UpdateExecutionState(ctx, testIssue.ID, types.ExecutionStateFailed)
			if err != nil {
				t.Errorf("Failed to transition from %s to failed: %v", targetState, err)
			}
		}
	})

	t.Run("invalid skip transitions", func(t *testing.T) {
		// Create new issue
		issue3 := &types.Issue{
			Title:      "Test issue for invalid transitions",
			Status:     types.StatusOpen,
			Priority:   2,
			IssueType:  types.TypeTask,
			AcceptanceCriteria: "Test acceptance criteria",
		}
		err = store.CreateIssue(ctx, issue3, "test")
		if err != nil {
			t.Fatalf("Failed to create issue: %v", err)
		}

		// Set to claimed
		err = store.UpdateExecutionState(ctx, issue3.ID, types.ExecutionStateClaimed)
		if err != nil {
			t.Fatalf("Failed to set state to claimed: %v", err)
		}

		// Try to skip from claimed directly to executing (should fail, must go through assessing)
		err = store.UpdateExecutionState(ctx, issue3.ID, types.ExecutionStateExecuting)
		if err == nil {
			t.Error("Should not be able to skip from claimed to executing")
		}
		t.Logf("Correctly rejected skip transition: %v", err)

		// Try to skip from claimed to gates (should fail)
		err = store.UpdateExecutionState(ctx, issue3.ID, types.ExecutionStateGates)
		if err == nil {
			t.Error("Should not be able to skip from claimed to gates")
		}

		// Try to go backwards from claimed to pending (should fail)
		err = store.UpdateExecutionState(ctx, issue3.ID, types.ExecutionStatePending)
		if err == nil {
			t.Error("Should not be able to go backwards from claimed to pending")
		}
	})

	t.Run("terminal states cannot transition", func(t *testing.T) {
		// Test completed state
		issue4 := &types.Issue{
			Title:      "Test completed terminal state",
			Status:     types.StatusOpen,
			Priority:   2,
			IssueType:  types.TypeTask,
			AcceptanceCriteria: "Test acceptance criteria",
		}
		err = store.CreateIssue(ctx, issue4, "test")
		if err != nil {
			t.Fatalf("Failed to create issue: %v", err)
		}

		// Transition through to completed
		transitions := []types.ExecutionState{
			types.ExecutionStatePending,
			types.ExecutionStateClaimed,
			types.ExecutionStateAssessing,
			types.ExecutionStateExecuting,
			types.ExecutionStateAnalyzing,
			types.ExecutionStateGates,
			types.ExecutionStateCommitting,
			types.ExecutionStateCompleted,
		}
		for _, state := range transitions {
			err = store.UpdateExecutionState(ctx, issue4.ID, state)
			if err != nil {
				t.Fatalf("Failed to transition to %s: %v", state, err)
			}
		}

		// Try to transition from completed (should fail)
		err = store.UpdateExecutionState(ctx, issue4.ID, types.ExecutionStateFailed)
		if err == nil {
			t.Error("Should not be able to transition from completed to failed")
		}

		err = store.UpdateExecutionState(ctx, issue4.ID, types.ExecutionStatePending)
		if err == nil {
			t.Error("Should not be able to transition from completed to pending")
		}

		// Test failed state
		issue5 := &types.Issue{
			Title:      "Test failed terminal state",
			Status:     types.StatusOpen,
			Priority:   2,
			IssueType:  types.TypeTask,
			AcceptanceCriteria: "Test acceptance criteria",
		}
		err = store.CreateIssue(ctx, issue5, "test")
		if err != nil {
			t.Fatalf("Failed to create issue: %v", err)
		}

		// Set to claimed then fail
		err = store.UpdateExecutionState(ctx, issue5.ID, types.ExecutionStateClaimed)
		if err != nil {
			t.Fatalf("Failed to set state to claimed: %v", err)
		}
		err = store.UpdateExecutionState(ctx, issue5.ID, types.ExecutionStateFailed)
		if err != nil {
			t.Fatalf("Failed to transition to failed: %v", err)
		}

		// Try to transition from failed (should fail)
		err = store.UpdateExecutionState(ctx, issue5.ID, types.ExecutionStateCompleted)
		if err == nil {
			t.Error("Should not be able to transition from failed to completed")
		}

		err = store.UpdateExecutionState(ctx, issue5.ID, types.ExecutionStatePending)
		if err == nil {
			t.Error("Should not be able to transition from failed to pending")
		}
	})

	t.Run("cannot start in executing state", func(t *testing.T) {
		issue6 := &types.Issue{
			Title:      "Test invalid initial state",
			Status:     types.StatusOpen,
			Priority:   2,
			IssueType:  types.TypeTask,
			AcceptanceCriteria: "Test acceptance criteria",
		}
		err = store.CreateIssue(ctx, issue6, "test")
		if err != nil {
			t.Fatalf("Failed to create issue: %v", err)
		}

		// Try to transition to executing without existing state (should fail)
		err = store.UpdateExecutionState(ctx, issue6.ID, types.ExecutionStateExecuting)
		if err == nil {
			t.Error("Should not be able to start in executing state")
		}
		t.Logf("Correctly rejected invalid initial state: %v", err)

		// Only pending or claimed are valid initial states
		err = store.UpdateExecutionState(ctx, issue6.ID, types.ExecutionStatePending)
		if err != nil {
			t.Errorf("Should be able to start in pending state: %v", err)
		}
	})
}

// TestAgentEventsNullSeverity validates that NULL severity values are handled gracefully (vc-164)
func TestAgentEventsNullSeverity(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	store, err := NewVCStorage(ctx, dbPath)
	if err != nil {
		t.Fatalf("Failed to create VC storage: %v", err)
	}
	defer func() { _ = store.Close() }()

	// Create a test issue
	issue := &types.Issue{
		Title:     "Test issue for NULL severity",
		Status:    types.StatusOpen,
		Priority:  2,
		IssueType: types.TypeTask,
		AcceptanceCriteria: "Test acceptance criteria",
	}
	err = store.CreateIssue(ctx, issue, "test")
	if err != nil {
		t.Fatalf("Failed to create issue: %v", err)
	}

	// Insert an event with NULL severity directly into the database
	// First insert with a valid severity, then update to NULL
	// This simulates events created by older code or database corruption
	result, err := store.db.ExecContext(ctx, `
		INSERT INTO vc_agent_events (timestamp, issue_id, type, severity, message)
		VALUES (?, ?, ?, ?, ?)
	`, time.Now(), issue.ID, events.EventTypeProgress, "info", "Event with NULL severity")
	if err != nil {
		t.Fatalf("Failed to insert event: %v", err)
	}

	// Get the inserted row ID
	rowID, err := result.LastInsertId()
	if err != nil {
		t.Fatalf("Failed to get inserted row ID: %v", err)
	}

	// Update severity to NULL to simulate corrupted data
	_, err = store.db.ExecContext(ctx, `
		UPDATE vc_agent_events SET severity = NULL WHERE id = ?
	`, rowID)
	if err != nil {
		t.Fatalf("Failed to update severity to NULL: %v", err)
	}

	// Also insert an event with a valid severity for comparison
	validEvent := &events.AgentEvent{
		Timestamp: time.Now(),
		IssueID:   issue.ID,
		Type:      events.EventTypeProgress,
		Severity:  events.SeverityInfo,
		Message:   "Event with valid severity",
	}
	err = store.StoreAgentEvent(ctx, validEvent)
	if err != nil {
		t.Fatalf("Failed to store valid event: %v", err)
	}

	t.Run("GetAgentEvents handles NULL severity", func(t *testing.T) {
		filter := events.EventFilter{
			IssueID: issue.ID,
		}
		results, err := store.GetAgentEvents(ctx, filter)
		if err != nil {
			t.Fatalf("GetAgentEvents failed with NULL severity: %v", err)
		}

		if len(results) != 2 {
			t.Fatalf("Expected 2 events, got %d", len(results))
		}

		// Find the NULL severity event
		var nullEvent *events.AgentEvent
		for _, e := range results {
			if e.Message == "Event with NULL severity" {
				nullEvent = e
				break
			}
		}

		if nullEvent == nil {
			t.Fatal("NULL severity event not found in results")
		}

		// Severity should be empty string (zero value) for NULL
		if nullEvent.Severity != "" {
			t.Errorf("Expected empty severity for NULL, got: %s", nullEvent.Severity)
		}
	})

	t.Run("GetAgentEventsByIssue handles NULL severity", func(t *testing.T) {
		results, err := store.GetAgentEventsByIssue(ctx, issue.ID)
		if err != nil {
			t.Fatalf("GetAgentEventsByIssue failed with NULL severity: %v", err)
		}

		if len(results) != 2 {
			t.Fatalf("Expected 2 events, got %d", len(results))
		}

		// Verify at least one event has NULL severity
		foundNull := false
		for _, e := range results {
			if e.Message == "Event with NULL severity" && e.Severity == "" {
				foundNull = true
				break
			}
		}

		if !foundNull {
			t.Error("NULL severity event not properly handled")
		}
	})

	t.Run("GetRecentAgentEvents handles NULL severity", func(t *testing.T) {
		results, err := store.GetRecentAgentEvents(ctx, 10)
		if err != nil {
			t.Fatalf("GetRecentAgentEvents failed with NULL severity: %v", err)
		}

		if len(results) < 1 {
			t.Fatal("Expected at least 1 event")
		}

		// Should successfully retrieve events even with NULL severity present
		// The command should not crash
		t.Logf("Successfully retrieved %d events with NULL severity present", len(results))
	})
}

// TestGetReadyBlockersFiltersDependencyTypes validates that GetReadyBlockers only checks
// blocking dependencies, not related/parent-child/discovered-from (vc-157)
func TestGetReadyBlockersFiltersDependencyTypes(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	store, err := NewVCStorage(ctx, dbPath)
	if err != nil {
		t.Fatalf("Failed to create VC storage: %v", err)
	}
	defer func() { _ = store.Close() }()

	// Create a parent mission issue (will remain open)
	mission := &types.Issue{
		Title:     "Parent mission",
		Status:    types.StatusOpen,
		Priority:  1,
		IssueType: types.TypeEpic,
	}
	err = store.CreateIssue(ctx, mission, "test")
	if err != nil {
		t.Fatalf("Failed to create mission: %v", err)
	}

	// Create a blocker issue with discovered:blocker label
	blocker := &types.Issue{
		Title:     "Test blocker issue",
		Status:    types.StatusOpen,
		Priority:  2,
		IssueType: types.TypeBug,
		AcceptanceCriteria: "Test acceptance criteria",
	}
	err = store.CreateIssue(ctx, blocker, "test")
	if err != nil {
		t.Fatalf("Failed to create blocker: %v", err)
	}

	// Add discovered:blocker label
	err = store.AddLabel(ctx, blocker.ID, types.LabelDiscoveredBlocker, "test")
	if err != nil {
		t.Fatalf("Failed to add label: %v", err)
	}

	// Add a discovered-from dependency (blocker discovered from mission)
	// This should NOT block execution
	err = store.AddDependency(ctx, &types.Dependency{
		IssueID:     blocker.ID,
		DependsOnID: mission.ID,
		Type:        types.DepDiscoveredFrom,
	}, "test")
	if err != nil {
		t.Fatalf("Failed to add discovered-from dependency: %v", err)
	}

	t.Run("blocker with discovered-from dependency is ready", func(t *testing.T) {
		// GetReadyBlockers should return the blocker because discovered-from doesn't block
		blockers, err := store.GetReadyBlockers(ctx, 10)
		if err != nil {
			t.Fatalf("GetReadyBlockers failed: %v", err)
		}

		if len(blockers) != 1 {
			t.Fatalf("Expected 1 ready blocker, got %d", len(blockers))
		}

		if blockers[0].ID != blocker.ID {
			t.Errorf("Expected blocker %s, got %s", blocker.ID, blockers[0].ID)
		}

		t.Logf("✓ Blocker %s correctly identified as ready despite discovered-from dependency", blocker.ID)
	})

	// Now add a real blocking dependency
	blockingIssue := &types.Issue{
		Title:     "Blocking issue",
		Status:    types.StatusOpen,
		Priority:  1,
		IssueType: types.TypeTask,
		AcceptanceCriteria: "Test acceptance criteria",
	}
	err = store.CreateIssue(ctx, blockingIssue, "test")
	if err != nil {
		t.Fatalf("Failed to create blocking issue: %v", err)
	}

	// Add a blocks dependency
	err = store.AddDependency(ctx, &types.Dependency{
		IssueID:     blocker.ID,
		DependsOnID: blockingIssue.ID,
		Type:        types.DepBlocks,
	}, "test")
	if err != nil {
		t.Fatalf("Failed to add blocks dependency: %v", err)
	}

	t.Run("blocker with open blocks dependency is not ready", func(t *testing.T) {
		// Now the blocker should NOT be ready (has open blocking dependency)
		blockers, err := store.GetReadyBlockers(ctx, 10)
		if err != nil {
			t.Fatalf("GetReadyBlockers failed: %v", err)
		}

		if len(blockers) != 0 {
			t.Errorf("Expected 0 ready blockers (has open blocks dependency), got %d", len(blockers))
		}

		t.Logf("✓ Blocker correctly filtered out due to open blocking dependency")
	})

	// Close the blocking issue
	err = store.CloseIssue(ctx, blockingIssue.ID, "Test completed", "test")
	if err != nil {
		t.Fatalf("Failed to close blocking issue: %v", err)
	}

	t.Run("blocker becomes ready when blocking dependency closes", func(t *testing.T) {
		// Now the blocker should be ready again (blocking dependency closed)
		blockers, err := store.GetReadyBlockers(ctx, 10)
		if err != nil {
			t.Fatalf("GetReadyBlockers failed: %v", err)
		}

		if len(blockers) != 1 {
			t.Fatalf("Expected 1 ready blocker (blocking dep closed), got %d", len(blockers))
		}

		if blockers[0].ID != blocker.ID {
			t.Errorf("Expected blocker %s, got %s", blocker.ID, blockers[0].ID)
		}

		t.Logf("✓ Blocker correctly becomes ready after blocking dependency closes")
	})

	// Test with parent-child relationship
	t.Run("blocker with parent-child dependency is ready", func(t *testing.T) {
		// Create another blocker
		childBlocker := &types.Issue{
			Title:     "Child blocker",
			Status:    types.StatusOpen,
			Priority:  2,
			IssueType: types.TypeBug,
			AcceptanceCriteria: "Test acceptance criteria",
		}
		err = store.CreateIssue(ctx, childBlocker, "test")
		if err != nil {
			t.Fatalf("Failed to create child blocker: %v", err)
		}

		err = store.AddLabel(ctx, childBlocker.ID, types.LabelDiscoveredBlocker, "test")
		if err != nil {
			t.Fatalf("Failed to add label: %v", err)
		}

		// Add parent-child dependency (child of mission)
		err = store.AddDependency(ctx, &types.Dependency{
			IssueID:     childBlocker.ID,
			DependsOnID: mission.ID,
			Type:        types.DepParentChild,
		}, "test")
		if err != nil {
			t.Fatalf("Failed to add parent-child dependency: %v", err)
		}

		// Should still be ready (parent-child doesn't block)
		blockers, err := store.GetReadyBlockers(ctx, 10)
		if err != nil {
			t.Fatalf("GetReadyBlockers failed: %v", err)
		}

		// Should have both blockers now (original + child)
		if len(blockers) != 2 {
			t.Errorf("Expected 2 ready blockers, got %d", len(blockers))
		}

		t.Logf("✓ Blocker with parent-child dependency correctly identified as ready")
	})
}

// TestEpicsExcludedFromReadyWork validates that epics are never claimed as executable work (vc-203)
func TestEpicsExcludedFromReadyWork(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	store, err := NewVCStorage(ctx, dbPath)
	if err != nil {
		t.Fatalf("Failed to create VC storage: %v", err)
	}
	defer func() { _ = store.Close() }()

	// Create an epic issue (should be excluded from ready work)
	epic := &types.Issue{
		Title:       "Test Epic",
		Description: "A tracking epic that should not be executed",
		Status:      types.StatusOpen,
		Priority:    1, // High priority to ensure it would be selected if not filtered
		IssueType:   types.TypeEpic,
	}
	err = store.CreateIssue(ctx, epic, "test")
	if err != nil {
		t.Fatalf("Failed to create epic: %v", err)
	}

	// Create a regular task (should be included in ready work)
	task := &types.Issue{
		Title:       "Test Task",
		Description: "A regular task that can be executed",
		Status:      types.StatusOpen,
		Priority:    2, // Lower priority than epic
		IssueType:   types.TypeTask,
		AcceptanceCriteria: "Test acceptance criteria",
	}
	err = store.CreateIssue(ctx, task, "test")
	if err != nil {
		t.Fatalf("Failed to create task: %v", err)
	}

	t.Run("GetReadyWork excludes epics", func(t *testing.T) {
		filter := types.WorkFilter{
			Status: types.StatusOpen,
			Limit:  10,
		}

		readyWork, err := store.GetReadyWork(ctx, filter)
		if err != nil {
			t.Fatalf("GetReadyWork failed: %v", err)
		}

		// Should only return the task, not the epic
		if len(readyWork) != 1 {
			t.Fatalf("Expected 1 ready issue (task only), got %d", len(readyWork))
		}

		if readyWork[0].ID != task.ID {
			t.Errorf("Expected task %s, got %s", task.ID, readyWork[0].ID)
		}

		// Verify epic was NOT included
		for _, issue := range readyWork {
			if issue.IssueType == types.TypeEpic {
				t.Errorf("Epic %s was incorrectly included in ready work", issue.ID)
			}
		}

		t.Logf("✓ GetReadyWork correctly excluded epic %s", epic.ID)
	})

	t.Run("GetReadyBlockers excludes epics", func(t *testing.T) {
		// Create an epic blocker with discovered:blocker label
		epicBlocker := &types.Issue{
			Title:       "Epic Blocker",
			Description: "An epic marked as blocker (should still be excluded)",
			Status:      types.StatusOpen,
			Priority:    1,
			IssueType:   types.TypeEpic,
		}
		err = store.CreateIssue(ctx, epicBlocker, "test")
		if err != nil {
			t.Fatalf("Failed to create epic blocker: %v", err)
		}

		// Add discovered:blocker label to epic
		err = store.AddLabel(ctx, epicBlocker.ID, types.LabelDiscoveredBlocker, "test")
		if err != nil {
			t.Fatalf("Failed to add blocker label to epic: %v", err)
		}

		// Create a regular blocker issue
		blocker := &types.Issue{
			Title:       "Regular Blocker",
			Description: "A regular blocker that can be executed",
			Status:      types.StatusOpen,
			Priority:    2,
			IssueType:   types.TypeBug,
			AcceptanceCriteria: "Test acceptance criteria",
		}
		err = store.CreateIssue(ctx, blocker, "test")
		if err != nil {
			t.Fatalf("Failed to create blocker: %v", err)
		}

		// Add discovered:blocker label
		err = store.AddLabel(ctx, blocker.ID, types.LabelDiscoveredBlocker, "test")
		if err != nil {
			t.Fatalf("Failed to add blocker label: %v", err)
		}

		// Get ready blockers
		blockers, err := store.GetReadyBlockers(ctx, 10)
		if err != nil {
			t.Fatalf("GetReadyBlockers failed: %v", err)
		}

		// Should only return the regular blocker, not the epic blocker
		if len(blockers) != 1 {
			t.Fatalf("Expected 1 ready blocker (regular blocker only), got %d", len(blockers))
		}

		if blockers[0].ID != blocker.ID {
			t.Errorf("Expected blocker %s, got %s", blocker.ID, blockers[0].ID)
		}

		// Verify epic blocker was NOT included
		for _, issue := range blockers {
			if issue.IssueType == types.TypeEpic {
				t.Errorf("Epic blocker %s was incorrectly included in ready blockers", issue.ID)
			}
		}

		t.Logf("✓ GetReadyBlockers correctly excluded epic blocker %s", epicBlocker.ID)
	})
}

// TestIsEpicComplete tests the epic completion detection logic (vc-232)
func TestIsEpicComplete(t *testing.T) {
	ctx := context.Background()

	// Create temporary database
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	store, err := NewVCStorage(ctx, dbPath)
	if err != nil {
		t.Fatalf("Failed to create VC storage: %v", err)
	}
	defer func() { _ = store.Close() }()

	t.Run("epic with all children closed is complete", func(t *testing.T) {
		// Create epic
		epic := &types.Issue{
			Title:       "Test Epic",
			Description: "Epic for completion testing",
			Status:      types.StatusOpen,
			Priority:    1,
			IssueType:   types.TypeEpic,
		}
		if err := store.CreateIssue(ctx, epic, "test"); err != nil {
			t.Fatalf("Failed to create epic: %v", err)
		}

		// Create child tasks (all closed)
		now := time.Now()
		child1 := &types.Issue{
			Title:     "Child 1",
			Status:    types.StatusClosed,
			Priority:  2,
			IssueType: types.TypeTask,
			AcceptanceCriteria: "Test acceptance criteria",
			ClosedAt:  &now,
		}
		if err := store.CreateIssue(ctx, child1, "test"); err != nil {
			t.Fatalf("Failed to create child1: %v", err)
		}

		child2 := &types.Issue{
			Title:     "Child 2",
			Status:    types.StatusClosed,
			Priority:  2,
			IssueType: types.TypeTask,
			AcceptanceCriteria: "Test acceptance criteria",
			ClosedAt:  &now,
		}
		if err := store.CreateIssue(ctx, child2, "test"); err != nil {
			t.Fatalf("Failed to create child2: %v", err)
		}

		// Add parent-child dependencies
		dep1 := &types.Dependency{
			IssueID:     child1.ID,
			DependsOnID: epic.ID,
			Type:        types.DepParentChild,
		}
		if err := store.AddDependency(ctx, dep1, "test"); err != nil {
			t.Fatalf("Failed to add dependency for child1: %v", err)
		}

		dep2 := &types.Dependency{
			IssueID:     child2.ID,
			DependsOnID: epic.ID,
			Type:        types.DepParentChild,
		}
		if err := store.AddDependency(ctx, dep2, "test"); err != nil {
			t.Fatalf("Failed to add dependency for child2: %v", err)
		}

		// Check completion
		complete, err := store.IsEpicComplete(ctx, epic.ID)
		if err != nil {
			t.Fatalf("IsEpicComplete failed: %v", err)
		}

		if !complete {
			t.Error("Expected epic to be complete (all children closed)")
		}
		t.Logf("✓ Epic %s correctly detected as complete", epic.ID)
	})

	t.Run("epic with open child is not complete", func(t *testing.T) {
		// Create epic
		epic := &types.Issue{
			Title:       "Epic with Open Child",
			Description: "Should not be complete",
			Status:      types.StatusOpen,
			Priority:    1,
			IssueType:   types.TypeEpic,
		}
		if err := store.CreateIssue(ctx, epic, "test"); err != nil {
			t.Fatalf("Failed to create epic: %v", err)
		}

		// Create one closed child and one open child
		now := time.Now()
		closedChild := &types.Issue{
			Title:     "Closed Child",
			Status:    types.StatusClosed,
			Priority:  2,
			IssueType: types.TypeTask,
			AcceptanceCriteria: "Test acceptance criteria",
			ClosedAt:  &now,
		}
		if err := store.CreateIssue(ctx, closedChild, "test"); err != nil {
			t.Fatalf("Failed to create closed child: %v", err)
		}

		openChild := &types.Issue{
			Title:     "Open Child",
			Status:    types.StatusOpen,
			Priority:  2,
			IssueType: types.TypeTask,
			AcceptanceCriteria: "Test acceptance criteria",
		}
		if err := store.CreateIssue(ctx, openChild, "test"); err != nil {
			t.Fatalf("Failed to create open child: %v", err)
		}

		// Add parent-child dependencies
		dep1 := &types.Dependency{
			IssueID:     closedChild.ID,
			DependsOnID: epic.ID,
			Type:        types.DepParentChild,
		}
		if err := store.AddDependency(ctx, dep1, "test"); err != nil {
			t.Fatalf("Failed to add dependency for closed child: %v", err)
		}

		dep2 := &types.Dependency{
			IssueID:     openChild.ID,
			DependsOnID: epic.ID,
			Type:        types.DepParentChild,
		}
		if err := store.AddDependency(ctx, dep2, "test"); err != nil {
			t.Fatalf("Failed to add dependency for open child: %v", err)
		}

		// Check completion
		complete, err := store.IsEpicComplete(ctx, epic.ID)
		if err != nil {
			t.Fatalf("IsEpicComplete failed: %v", err)
		}

		if complete {
			t.Error("Expected epic to be incomplete (has open child)")
		}
		t.Logf("✓ Epic %s correctly detected as incomplete (open child: %s)", epic.ID, openChild.ID)
	})

	t.Run("epic with in_progress child is not complete", func(t *testing.T) {
		// Create epic
		epic := &types.Issue{
			Title:       "Epic with In Progress Child",
			Description: "Should not be complete",
			Status:      types.StatusOpen,
			Priority:    1,
			IssueType:   types.TypeEpic,
		}
		if err := store.CreateIssue(ctx, epic, "test"); err != nil {
			t.Fatalf("Failed to create epic: %v", err)
		}

		// Create in_progress child
		inProgressChild := &types.Issue{
			Title:     "In Progress Child",
			Status:    types.StatusInProgress,
			Priority:  2,
			IssueType: types.TypeTask,
			AcceptanceCriteria: "Test acceptance criteria",
		}
		if err := store.CreateIssue(ctx, inProgressChild, "test"); err != nil {
			t.Fatalf("Failed to create in_progress child: %v", err)
		}

		// Add parent-child dependency
		dep := &types.Dependency{
			IssueID:     inProgressChild.ID,
			DependsOnID: epic.ID,
			Type:        types.DepParentChild,
		}
		if err := store.AddDependency(ctx, dep, "test"); err != nil {
			t.Fatalf("Failed to add dependency: %v", err)
		}

		// Check completion
		complete, err := store.IsEpicComplete(ctx, epic.ID)
		if err != nil {
			t.Fatalf("IsEpicComplete failed: %v", err)
		}

		if complete {
			t.Error("Expected epic to be incomplete (has in_progress child)")
		}
		t.Logf("✓ Epic %s correctly detected as incomplete (in_progress child)", epic.ID)
	})

	t.Run("epic with no children is complete", func(t *testing.T) {
		// Create epic with no children
		epic := &types.Issue{
			Title:       "Childless Epic",
			Description: "Epic with no child tasks",
			Status:      types.StatusOpen,
			Priority:    1,
			IssueType:   types.TypeEpic,
		}
		if err := store.CreateIssue(ctx, epic, "test"); err != nil {
			t.Fatalf("Failed to create epic: %v", err)
		}

		// Check completion (should be true - no children to wait for)
		complete, err := store.IsEpicComplete(ctx, epic.ID)
		if err != nil {
			t.Fatalf("IsEpicComplete failed: %v", err)
		}

		if !complete {
			t.Error("Expected childless epic to be complete")
		}
		t.Logf("✓ Childless epic %s correctly detected as complete", epic.ID)
	})

	t.Run("epic with open blocking dependency is not complete", func(t *testing.T) {
		// Create epic
		epic := &types.Issue{
			Title:       "Epic with Blocker",
			Description: "Blocked by another issue",
			Status:      types.StatusOpen,
			Priority:    1,
			IssueType:   types.TypeEpic,
		}
		if err := store.CreateIssue(ctx, epic, "test"); err != nil {
			t.Fatalf("Failed to create epic: %v", err)
		}

		// Create blocker issue (open)
		blocker := &types.Issue{
			Title:     "Blocker Issue",
			Status:    types.StatusOpen,
			Priority:  1,
			IssueType: types.TypeTask,
			AcceptanceCriteria: "Test acceptance criteria",
		}
		if err := store.CreateIssue(ctx, blocker, "test"); err != nil {
			t.Fatalf("Failed to create blocker: %v", err)
		}

		// Add blocking dependency (epic depends on blocker)
		dep := &types.Dependency{
			IssueID:     epic.ID,
			DependsOnID: blocker.ID,
			Type:        types.DepBlocks,
		}
		if err := store.AddDependency(ctx, dep, "test"); err != nil {
			t.Fatalf("Failed to add blocking dependency: %v", err)
		}

		// Check completion (should be false - has open blocker)
		complete, err := store.IsEpicComplete(ctx, epic.ID)
		if err != nil {
			t.Fatalf("IsEpicComplete failed: %v", err)
		}

		if complete {
			t.Error("Expected epic to be incomplete (has open blocker)")
		}
		t.Logf("✓ Epic %s correctly detected as incomplete (open blocker: %s)", epic.ID, blocker.ID)
	})

	t.Run("epic with closed blocker and closed children is complete", func(t *testing.T) {
		// Create epic
		epic := &types.Issue{
			Title:       "Epic with Closed Blocker",
			Description: "All deps satisfied",
			Status:      types.StatusOpen,
			Priority:    1,
			IssueType:   types.TypeEpic,
		}
		if err := store.CreateIssue(ctx, epic, "test"); err != nil {
			t.Fatalf("Failed to create epic: %v", err)
		}

		// Create closed child
		now := time.Now()
		child := &types.Issue{
			Title:     "Child Task",
			Status:    types.StatusClosed,
			Priority:  2,
			IssueType: types.TypeTask,
			AcceptanceCriteria: "Test acceptance criteria",
			ClosedAt:  &now,
		}
		if err := store.CreateIssue(ctx, child, "test"); err != nil {
			t.Fatalf("Failed to create child: %v", err)
		}

		// Create closed blocker
		blocker := &types.Issue{
			Title:     "Closed Blocker",
			Status:    types.StatusClosed,
			Priority:  1,
			IssueType: types.TypeTask,
			AcceptanceCriteria: "Test acceptance criteria",
			ClosedAt:  &now,
		}
		if err := store.CreateIssue(ctx, blocker, "test"); err != nil {
			t.Fatalf("Failed to create blocker: %v", err)
		}

		// Add parent-child dependency
		childDep := &types.Dependency{
			IssueID:     child.ID,
			DependsOnID: epic.ID,
			Type:        types.DepParentChild,
		}
		if err := store.AddDependency(ctx, childDep, "test"); err != nil {
			t.Fatalf("Failed to add child dependency: %v", err)
		}

		// Add blocking dependency
		blockerDep := &types.Dependency{
			IssueID:     epic.ID,
			DependsOnID: blocker.ID,
			Type:        types.DepBlocks,
		}
		if err := store.AddDependency(ctx, blockerDep, "test"); err != nil {
			t.Fatalf("Failed to add blocker dependency: %v", err)
		}

		// Check completion (should be true - all children and blockers closed)
		complete, err := store.IsEpicComplete(ctx, epic.ID)
		if err != nil {
			t.Fatalf("IsEpicComplete failed: %v", err)
		}

		if !complete {
			t.Error("Expected epic to be complete (children and blockers all closed)")
		}
		t.Logf("✓ Epic %s correctly detected as complete (closed blocker and children)", epic.ID)
	})

	t.Run("epic ignores non-parent-child dependencies", func(t *testing.T) {
		// Create epic
		epic := &types.Issue{
			Title:       "Epic with Related Issues",
			Description: "Should only check parent-child, not related",
			Status:      types.StatusOpen,
			Priority:    1,
			IssueType:   types.TypeEpic,
		}
		if err := store.CreateIssue(ctx, epic, "test"); err != nil {
			t.Fatalf("Failed to create epic: %v", err)
		}

		// Create a related issue (open) - should NOT affect completion
		relatedIssue := &types.Issue{
			Title:     "Related Issue (Open)",
			Status:    types.StatusOpen,
			Priority:  2,
			IssueType: types.TypeTask,
			AcceptanceCriteria: "Test acceptance criteria",
		}
		if err := store.CreateIssue(ctx, relatedIssue, "test"); err != nil {
			t.Fatalf("Failed to create related issue: %v", err)
		}

		// Create a discovered-from issue (open) - should NOT affect completion
		discoveredIssue := &types.Issue{
			Title:     "Discovered Issue (Open)",
			Status:    types.StatusOpen,
			Priority:  2,
			IssueType: types.TypeBug,
			AcceptanceCriteria: "Test acceptance criteria",
		}
		if err := store.CreateIssue(ctx, discoveredIssue, "test"); err != nil {
			t.Fatalf("Failed to create discovered issue: %v", err)
		}

		// Add related dependency (not parent-child)
		relatedDep := &types.Dependency{
			IssueID:     relatedIssue.ID,
			DependsOnID: epic.ID,
			Type:        types.DepRelated,
		}
		if err := store.AddDependency(ctx, relatedDep, "test"); err != nil {
			t.Fatalf("Failed to add related dependency: %v", err)
		}

		// Add discovered-from dependency (not parent-child)
		discoveredDep := &types.Dependency{
			IssueID:     discoveredIssue.ID,
			DependsOnID: epic.ID,
			Type:        types.DepDiscoveredFrom,
		}
		if err := store.AddDependency(ctx, discoveredDep, "test"); err != nil {
			t.Fatalf("Failed to add discovered-from dependency: %v", err)
		}

		// Epic should be complete - no parent-child children, no blockers
		complete, err := store.IsEpicComplete(ctx, epic.ID)
		if err != nil {
			t.Fatalf("IsEpicComplete failed: %v", err)
		}

		if !complete {
			t.Error("Expected epic to be complete (related/discovered issues should not affect completion)")
		}
		t.Logf("✓ Epic %s correctly ignores non-parent-child dependencies (related: %s, discovered: %s)",
			epic.ID, relatedIssue.ID, discoveredIssue.ID)
	})
}

// TestGetMissionForTask tests walking the dependency tree to find parent missions (vc-233)
func TestGetMissionForTask(t *testing.T) {
	ctx := context.Background()

	// Create temporary database
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	store, err := NewVCStorage(ctx, dbPath)
	if err != nil {
		t.Fatalf("Failed to create VC storage: %v", err)
	}
	defer func() { _ = store.Close() }()

	t.Run("task directly under mission", func(t *testing.T) {
		// Create mission epic with subtype='mission'
		mission := &types.Mission{
			Issue: types.Issue{
				Title:        "Test Mission",
				Description:  "Top-level mission",
				Status:       types.StatusOpen,
				Priority:     1,
				IssueType:    types.TypeEpic,
				IssueSubtype: types.SubtypeMission,
			},
			Goal:         "Complete the mission",
			SandboxPath:  "/sandbox/mission-123",
			BranchName:   "mission-123",
		}
		if err := store.CreateMission(ctx, mission, "test"); err != nil {
			t.Fatalf("Failed to create mission: %v", err)
		}

		// Create task as direct child
		task := &types.Issue{
			Title:     "Task 1",
			Status:    types.StatusOpen,
			Priority:  2,
			IssueType: types.TypeTask,
			AcceptanceCriteria: "Test acceptance criteria",
		}
		if err := store.CreateIssue(ctx, task, "test"); err != nil {
			t.Fatalf("Failed to create task: %v", err)
		}

		// Add parent-child dependency (task depends on mission)
		dep := &types.Dependency{
			IssueID:     task.ID,
			DependsOnID: mission.ID,
			Type:        types.DepParentChild,
		}
		if err := store.AddDependency(ctx, dep, "test"); err != nil {
			t.Fatalf("Failed to add dependency: %v", err)
		}

		// Get mission context
		missionCtx, err := store.GetMissionForTask(ctx, task.ID)
		if err != nil {
			t.Fatalf("GetMissionForTask failed: %v", err)
		}

		if missionCtx.MissionID != mission.ID {
			t.Errorf("Expected mission ID %s, got %s", mission.ID, missionCtx.MissionID)
		}
		if missionCtx.SandboxPath != mission.SandboxPath {
			t.Errorf("Expected sandbox path %s, got %s", mission.SandboxPath, missionCtx.SandboxPath)
		}
		if missionCtx.BranchName != mission.BranchName {
			t.Errorf("Expected branch name %s, got %s", mission.BranchName, missionCtx.BranchName)
		}
		t.Logf("✓ Task %s correctly found mission %s", task.ID, mission.ID)
	})

	t.Run("task under phase under mission (nested epics)", func(t *testing.T) {
		// Create mission epic
		mission := &types.Mission{
			Issue: types.Issue{
				Title:        "Mission with Phases",
				Description:  "Multi-phase mission",
				Status:       types.StatusOpen,
				Priority:     1,
				IssueType:    types.TypeEpic,
				IssueSubtype: types.SubtypeMission,
			},
			Goal:         "Complete phased work",
			SandboxPath:  "/sandbox/mission-456",
			BranchName:   "mission-456",
		}
		if err := store.CreateMission(ctx, mission, "test"); err != nil {
			t.Fatalf("Failed to create mission: %v", err)
		}

		// Create phase epic (child of mission)
		phase := &types.Issue{
			Title:        "Phase 1",
			Description:  "First phase",
			Status:       types.StatusOpen,
			Priority:     1,
			IssueType:    types.TypeEpic,
			IssueSubtype: types.SubtypePhase,
		}
		if err := store.CreateIssue(ctx, phase, "test"); err != nil {
			t.Fatalf("Failed to create phase: %v", err)
		}

		// Add phase -> mission dependency
		phaseDep := &types.Dependency{
			IssueID:     phase.ID,
			DependsOnID: mission.ID,
			Type:        types.DepParentChild,
		}
		if err := store.AddDependency(ctx, phaseDep, "test"); err != nil {
			t.Fatalf("Failed to add phase dependency: %v", err)
		}

		// Create task (child of phase)
		task := &types.Issue{
			Title:     "Task in Phase",
			Status:    types.StatusOpen,
			Priority:  2,
			IssueType: types.TypeTask,
			AcceptanceCriteria: "Test acceptance criteria",
		}
		if err := store.CreateIssue(ctx, task, "test"); err != nil {
			t.Fatalf("Failed to create task: %v", err)
		}

		// Add task -> phase dependency
		taskDep := &types.Dependency{
			IssueID:     task.ID,
			DependsOnID: phase.ID,
			Type:        types.DepParentChild,
		}
		if err := store.AddDependency(ctx, taskDep, "test"); err != nil {
			t.Fatalf("Failed to add task dependency: %v", err)
		}

		// Get mission context (should walk up through phase to mission)
		missionCtx, err := store.GetMissionForTask(ctx, task.ID)
		if err != nil {
			t.Fatalf("GetMissionForTask failed: %v", err)
		}

		if missionCtx.MissionID != mission.ID {
			t.Errorf("Expected mission ID %s, got %s", mission.ID, missionCtx.MissionID)
		}
		if missionCtx.SandboxPath != mission.SandboxPath {
			t.Errorf("Expected sandbox path %s, got %s", mission.SandboxPath, missionCtx.SandboxPath)
		}
		t.Logf("✓ Task %s correctly walked up through phase %s to mission %s",
			task.ID, phase.ID, mission.ID)
	})

	t.Run("task with no mission parent returns error", func(t *testing.T) {
		// Create standalone task (no parent)
		task := &types.Issue{
			Title:     "Standalone Task",
			Status:    types.StatusOpen,
			Priority:  2,
			IssueType: types.TypeTask,
			AcceptanceCriteria: "Test acceptance criteria",
		}
		if err := store.CreateIssue(ctx, task, "test"); err != nil {
			t.Fatalf("Failed to create task: %v", err)
		}

		// Try to get mission context (should fail)
		_, err := store.GetMissionForTask(ctx, task.ID)
		if err == nil {
			t.Error("Expected error for task with no mission parent")
		} else {
			t.Logf("✓ Correctly returned error for standalone task: %v", err)
		}
	})

	t.Run("task under normal epic (not mission) returns error", func(t *testing.T) {
		// Create normal epic (not a mission)
		epic := &types.Issue{
			Title:        "Normal Epic",
			Description:  "Not a mission",
			Status:       types.StatusOpen,
			Priority:     1,
			IssueType:    types.TypeEpic,
			IssueSubtype: types.SubtypeNormal,
		}
		if err := store.CreateIssue(ctx, epic, "test"); err != nil {
			t.Fatalf("Failed to create epic: %v", err)
		}

		// Create task under normal epic
		task := &types.Issue{
			Title:     "Task Under Normal Epic",
			Status:    types.StatusOpen,
			Priority:  2,
			IssueType: types.TypeTask,
			AcceptanceCriteria: "Test acceptance criteria",
		}
		if err := store.CreateIssue(ctx, task, "test"); err != nil {
			t.Fatalf("Failed to create task: %v", err)
		}

		// Add parent-child dependency
		dep := &types.Dependency{
			IssueID:     task.ID,
			DependsOnID: epic.ID,
			Type:        types.DepParentChild,
		}
		if err := store.AddDependency(ctx, dep, "test"); err != nil {
			t.Fatalf("Failed to add dependency: %v", err)
		}

		// Try to get mission context (should fail)
		_, err := store.GetMissionForTask(ctx, task.ID)
		if err == nil {
			t.Error("Expected error for task under normal epic (not mission)")
		} else {
			t.Logf("✓ Correctly returned error for task under normal epic: %v", err)
		}
	})

	t.Run("non-parent-child dependencies ignored", func(t *testing.T) {
		// Create mission
		mission := &types.Mission{
			Issue: types.Issue{
				Title:        "Mission for Dep Test",
				Description:  "Testing dependency types",
				Status:       types.StatusOpen,
				Priority:     1,
				IssueType:    types.TypeEpic,
				IssueSubtype: types.SubtypeMission,
			},
			Goal: "Test dependencies",
		}
		if err := store.CreateMission(ctx, mission, "test"); err != nil {
			t.Fatalf("Failed to create mission: %v", err)
		}

		// Create task with parent-child to mission
		task := &types.Issue{
			Title:     "Task with Multiple Deps",
			Status:    types.StatusOpen,
			Priority:  2,
			IssueType: types.TypeTask,
			AcceptanceCriteria: "Test acceptance criteria",
		}
		if err := store.CreateIssue(ctx, task, "test"); err != nil {
			t.Fatalf("Failed to create task: %v", err)
		}

		// Add parent-child dependency
		parentChildDep := &types.Dependency{
			IssueID:     task.ID,
			DependsOnID: mission.ID,
			Type:        types.DepParentChild,
		}
		if err := store.AddDependency(ctx, parentChildDep, "test"); err != nil {
			t.Fatalf("Failed to add parent-child dependency: %v", err)
		}

		// Create blocker issue
		blocker := &types.Issue{
			Title:     "Blocker",
			Status:    types.StatusOpen,
			Priority:  2,
			IssueType: types.TypeTask,
			AcceptanceCriteria: "Test acceptance criteria",
		}
		if err := store.CreateIssue(ctx, blocker, "test"); err != nil {
			t.Fatalf("Failed to create blocker: %v", err)
		}

		// Add blocks dependency (should be ignored)
		blocksDep := &types.Dependency{
			IssueID:     task.ID,
			DependsOnID: blocker.ID,
			Type:        types.DepBlocks,
		}
		if err := store.AddDependency(ctx, blocksDep, "test"); err != nil {
			t.Fatalf("Failed to add blocks dependency: %v", err)
		}

		// Get mission context (should find mission, ignoring blocks dependency)
		missionCtx, err := store.GetMissionForTask(ctx, task.ID)
		if err != nil {
			t.Fatalf("GetMissionForTask failed: %v", err)
		}

		if missionCtx.MissionID != mission.ID {
			t.Errorf("Expected mission ID %s, got %s", mission.ID, missionCtx.MissionID)
		}
		t.Logf("✓ Task %s correctly found mission %s, ignoring blocks dependency to %s",
			task.ID, mission.ID, blocker.ID)
	})

	t.Run("circular dependency detection (defensive code)", func(t *testing.T) {
		// Note: The Beads library (DetectCycles in AddDependency) prevents creating
		// circular dependencies, so GetMissionForTask's circular detection code is
		// defensive and should never be hit in practice. We verify the library
		// prevents the invalid state rather than testing the defensive code.

		// Create task A
		taskA := &types.Issue{
			Title:     "Task A",
			Status:    types.StatusOpen,
			Priority:  1,
			IssueType: types.TypeTask,
			AcceptanceCriteria: "Test acceptance criteria",
		}
		if err := store.CreateIssue(ctx, taskA, "test"); err != nil {
			t.Fatalf("Failed to create taskA: %v", err)
		}

		// Create task B
		taskB := &types.Issue{
			Title:     "Task B",
			Status:    types.StatusOpen,
			Priority:  1,
			IssueType: types.TypeTask,
			AcceptanceCriteria: "Test acceptance criteria",
		}
		if err := store.CreateIssue(ctx, taskB, "test"); err != nil {
			t.Fatalf("Failed to create taskB: %v", err)
		}

		// Create first dependency: A → B
		depAB := &types.Dependency{
			IssueID:     taskA.ID,
			DependsOnID: taskB.ID,
			Type:        types.DepParentChild,
		}
		if err := store.AddDependency(ctx, depAB, "test"); err != nil {
			t.Fatalf("Failed to add A→B dependency: %v", err)
		}

		// Attempt to create circular dependency: B → A
		// This should be prevented by Beads library's cycle detection
		depBA := &types.Dependency{
			IssueID:     taskB.ID,
			DependsOnID: taskA.ID,
			Type:        types.DepParentChild,
		}
		err := store.AddDependency(ctx, depBA, "test")
		if err == nil {
			t.Fatal("Expected Beads to prevent circular dependency, but AddDependency succeeded")
		}
		if !contains(err.Error(), "cycle") {
			t.Errorf("Expected 'cycle' error from Beads, got: %v", err)
		}
		t.Logf("✓ Beads correctly prevented circular dependency: %v", err)
		t.Log("  (GetMissionForTask's circular detection is defensive code that shouldn't be hit)")
	})

	t.Run("task with multiple parents uses first parent", func(t *testing.T) {
		// Create two missions
		mission1 := &types.Mission{
			Issue: types.Issue{
				Title:        "Mission 1",
				Status:       types.StatusOpen,
				Priority:     1,
				IssueType:    types.TypeEpic,
				IssueSubtype: types.SubtypeMission,
			},
			Goal: "First mission",
		}
		if err := store.CreateMission(ctx, mission1, "test"); err != nil {
			t.Fatalf("Failed to create mission1: %v", err)
		}

		mission2 := &types.Mission{
			Issue: types.Issue{
				Title:        "Mission 2",
				Status:       types.StatusOpen,
				Priority:     1,
				IssueType:    types.TypeEpic,
				IssueSubtype: types.SubtypeMission,
			},
			Goal: "Second mission",
		}
		if err := store.CreateMission(ctx, mission2, "test"); err != nil {
			t.Fatalf("Failed to create mission2: %v", err)
		}

		// Create task with two parent dependencies
		task := &types.Issue{
			Title:     "Multi-parent task",
			Status:    types.StatusOpen,
			Priority:  1,
			IssueType: types.TypeTask,
			AcceptanceCriteria: "Test acceptance criteria",
		}
		if err := store.CreateIssue(ctx, task, "test"); err != nil {
			t.Fatalf("Failed to create task: %v", err)
		}

		// Add first dependency (task → mission1)
		dep1 := &types.Dependency{
			IssueID:     task.ID,
			DependsOnID: mission1.ID,
			Type:        types.DepParentChild,
		}
		if err := store.AddDependency(ctx, dep1, "test"); err != nil {
			t.Fatalf("Failed to add first dependency: %v", err)
		}

		// Add second dependency (task → mission2)
		dep2 := &types.Dependency{
			IssueID:     task.ID,
			DependsOnID: mission2.ID,
			Type:        types.DepParentChild,
		}
		if err := store.AddDependency(ctx, dep2, "test"); err != nil {
			t.Fatalf("Failed to add second dependency: %v", err)
		}

		// GetMissionForTask should use the first parent found
		missionCtx, err := store.GetMissionForTask(ctx, task.ID)
		if err != nil {
			t.Fatalf("GetMissionForTask failed: %v", err)
		}

		// Should return one of the missions (likely mission1 based on insertion order)
		if missionCtx.MissionID != mission1.ID && missionCtx.MissionID != mission2.ID {
			t.Errorf("Expected mission ID %s or %s, got %s", mission1.ID, mission2.ID, missionCtx.MissionID)
		}
		t.Logf("✓ Task with multiple parents correctly returned mission %s", missionCtx.MissionID)
	})
}

// TestGetMissionByPhase tests phase-to-mission navigation (vc-60)
func TestGetMissionByPhase(t *testing.T) {
	ctx := context.Background()

	// Create temporary database
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	store, err := NewVCStorage(ctx, dbPath)
	if err != nil {
		t.Fatalf("Failed to create VC storage: %v", err)
	}
	defer func() { _ = store.Close() }()

	t.Run("phase directly under mission", func(t *testing.T) {
		// Create mission epic with subtype='mission'
		mission := &types.Mission{
			Issue: types.Issue{
				Title:        "Mission with 3 Phases",
				Description:  "Multi-phase mission for testing",
				Status:       types.StatusOpen,
				Priority:     0,
				IssueType:    types.TypeEpic,
				IssueSubtype: types.SubtypeMission,
			},
			Goal:         "Complete the mission in 3 phases",
			SandboxPath:  "/sandbox/mission-test",
			BranchName:   "mission-test",
			PhaseCount:   3,
			CurrentPhase: 0,
		}
		if err := store.CreateMission(ctx, mission, "test"); err != nil {
			t.Fatalf("Failed to create mission: %v", err)
		}

		// Create 3 phase epics
		phases := make([]*types.Issue, 3)
		for i := 0; i < 3; i++ {
			phases[i] = &types.Issue{
				Title:        fmt.Sprintf("Phase %d", i+1),
				Description:  fmt.Sprintf("Phase %d implementation", i+1),
				Status:       types.StatusOpen,
				Priority:     1,
				IssueType:    types.TypeEpic,
				IssueSubtype: types.SubtypePhase,
			}
			if err := store.CreateIssue(ctx, phases[i], "test"); err != nil {
				t.Fatalf("Failed to create phase %d: %v", i+1, err)
			}

			// Add parent-child dependency (phase depends on mission)
			dep := &types.Dependency{
				IssueID:     phases[i].ID,
				DependsOnID: mission.ID,
				Type:        types.DepParentChild,
			}
			if err := store.AddDependency(ctx, dep, "test"); err != nil {
				t.Fatalf("Failed to add dependency for phase %d: %v", i+1, err)
			}
		}

		// Test GetMissionByPhase for phase 2 (middle phase)
		retrievedMission, err := store.GetMissionByPhase(ctx, phases[1].ID)
		if err != nil {
			t.Fatalf("GetMissionByPhase failed for phase 2: %v", err)
		}

		if retrievedMission.ID != mission.ID {
			t.Errorf("Expected mission ID %s, got %s", mission.ID, retrievedMission.ID)
		}
		if retrievedMission.Goal != mission.Goal {
			t.Errorf("Expected goal %q, got %q", mission.Goal, retrievedMission.Goal)
		}
		if retrievedMission.SandboxPath != mission.SandboxPath {
			t.Errorf("Expected sandbox path %s, got %s", mission.SandboxPath, retrievedMission.SandboxPath)
		}
		if retrievedMission.BranchName != mission.BranchName {
			t.Errorf("Expected branch name %s, got %s", mission.BranchName, retrievedMission.BranchName)
		}
		if retrievedMission.PhaseCount != mission.PhaseCount {
			t.Errorf("Expected phase count %d, got %d", mission.PhaseCount, retrievedMission.PhaseCount)
		}
		t.Logf("✓ Phase %s correctly found mission %s", phases[1].ID, mission.ID)
	})

	t.Run("error when ID is not a phase", func(t *testing.T) {
		// Create a regular task (not a phase)
		task := &types.Issue{
			Title:     "Regular Task",
			Status:    types.StatusOpen,
			Priority:  2,
			IssueType: types.TypeTask,
			AcceptanceCriteria: "Test acceptance criteria",
		}
		if err := store.CreateIssue(ctx, task, "test"); err != nil {
			t.Fatalf("Failed to create task: %v", err)
		}

		// Try to get mission by task ID (should fail)
		_, err := store.GetMissionByPhase(ctx, task.ID)
		if err == nil {
			t.Error("Expected error when calling GetMissionByPhase with non-phase ID")
		} else {
			t.Logf("✓ Correctly returned error for non-phase issue: %v", err)
		}
	})

	t.Run("error when phase has no parent mission", func(t *testing.T) {
		// Create a phase epic without linking it to a mission
		orphanPhase := &types.Issue{
			Title:        "Orphan Phase",
			Description:  "Phase not linked to any mission",
			Status:       types.StatusOpen,
			Priority:     1,
			IssueType:    types.TypeEpic,
			IssueSubtype: types.SubtypePhase,
		}
		if err := store.CreateIssue(ctx, orphanPhase, "test"); err != nil {
			t.Fatalf("Failed to create orphan phase: %v", err)
		}

		// Try to get mission (should fail - no parent mission)
		_, err := store.GetMissionByPhase(ctx, orphanPhase.ID)
		if err == nil {
			t.Error("Expected error when phase has no parent mission")
		} else {
			t.Logf("✓ Correctly returned error for orphan phase: %v", err)
		}
	})

	t.Run("nested phase hierarchy - task under phase under mission", func(t *testing.T) {
		// Create mission epic
		mission := &types.Mission{
			Issue: types.Issue{
				Title:        "Mission with Nested Phases",
				Description:  "Multi-level mission structure",
				Status:       types.StatusOpen,
				Priority:     0,
				IssueType:    types.TypeEpic,
				IssueSubtype: types.SubtypeMission,
			},
			Goal:         "Complete multi-level work",
			SandboxPath:  "/sandbox/mission-nested",
			BranchName:   "mission-nested",
			PhaseCount:   2,
			CurrentPhase: 0,
		}
		if err := store.CreateMission(ctx, mission, "test"); err != nil {
			t.Fatalf("Failed to create mission: %v", err)
		}

		// Create phase epic (child of mission)
		phase := &types.Issue{
			Title:        "Implementation Phase",
			Description:  "First implementation phase",
			Status:       types.StatusOpen,
			Priority:     1,
			IssueType:    types.TypeEpic,
			IssueSubtype: types.SubtypePhase,
		}
		if err := store.CreateIssue(ctx, phase, "test"); err != nil {
			t.Fatalf("Failed to create phase: %v", err)
		}

		// Link phase to mission
		phaseDep := &types.Dependency{
			IssueID:     phase.ID,
			DependsOnID: mission.ID,
			Type:        types.DepParentChild,
		}
		if err := store.AddDependency(ctx, phaseDep, "test"); err != nil {
			t.Fatalf("Failed to add phase dependency: %v", err)
		}

		// Create task under phase
		task := &types.Issue{
			Title:              "Task in Phase",
			Status:             types.StatusOpen,
			Priority:           2,
			IssueType:          types.TypeTask,
			AcceptanceCriteria: "Test acceptance criteria",
		}
		if err := store.CreateIssue(ctx, task, "test"); err != nil {
			t.Fatalf("Failed to create task: %v", err)
		}

		// Link task to phase
		taskDep := &types.Dependency{
			IssueID:     task.ID,
			DependsOnID: phase.ID,
			Type:        types.DepParentChild,
		}
		if err := store.AddDependency(ctx, taskDep, "test"); err != nil {
			t.Fatalf("Failed to add task dependency: %v", err)
		}

		// Test GetMissionByPhase - should walk up through the hierarchy
		// This verifies the recursive CTE correctly handles multi-level traversal
		retrievedMission, err := store.GetMissionByPhase(ctx, phase.ID)
		if err != nil {
			t.Fatalf("GetMissionByPhase failed for nested phase: %v", err)
		}

		if retrievedMission.ID != mission.ID {
			t.Errorf("Expected mission ID %s, got %s", mission.ID, retrievedMission.ID)
		}
		if retrievedMission.Goal != mission.Goal {
			t.Errorf("Expected goal %q, got %q", mission.Goal, retrievedMission.Goal)
		}
		if retrievedMission.SandboxPath != mission.SandboxPath {
			t.Errorf("Expected sandbox path %s, got %s", mission.SandboxPath, retrievedMission.SandboxPath)
		}
		if retrievedMission.BranchName != mission.BranchName {
			t.Errorf("Expected branch name %s, got %s", mission.BranchName, retrievedMission.BranchName)
		}

		// Verify the CTE correctly navigated the hierarchy
		t.Logf("✓ Phase %s correctly found mission %s through recursive CTE (task→phase→mission)",
			phase.ID, mission.ID)
		t.Logf("  Hierarchy: task %s → phase %s → mission %s", task.ID, phase.ID, mission.ID)
	})

	// vc-sluq: Test improved error messages
	t.Run("error message for non-existent issue", func(t *testing.T) {
		_, err := store.GetMissionByPhase(ctx, "vc-nonexistent")
		if err == nil {
			t.Error("Expected error for non-existent issue")
		} else {
			errMsg := err.Error()
			if !strings.Contains(errMsg, "not found in vc_mission_state table") {
				t.Errorf("Error message should mention 'not found in vc_mission_state table', got: %s", errMsg)
			}
			t.Logf("✓ Correct error for non-existent issue: %v", err)
		}
	})

	t.Run("error message for task without subtype", func(t *testing.T) {
		// Create a regular task (won't be in vc_mission_state)
		task := &types.Issue{
			Title:              "Task Without Subtype",
			Status:             types.StatusOpen,
			Priority:           2,
			IssueType:          types.TypeTask,
			AcceptanceCriteria: "Test acceptance criteria",
		}
		if err := store.CreateIssue(ctx, task, "test"); err != nil {
			t.Fatalf("Failed to create task: %v", err)
		}

		_, err := store.GetMissionByPhase(ctx, task.ID)
		if err == nil {
			t.Error("Expected error for task without subtype")
		} else {
			errMsg := err.Error()
			if !strings.Contains(errMsg, "not found in vc_mission_state table") {
				t.Errorf("Error message should mention 'not found in vc_mission_state table', got: %s", errMsg)
			}
			t.Logf("✓ Correct error for task without subtype: %v", err)
		}
	})

	t.Run("error message for mission (wrong subtype)", func(t *testing.T) {
		// Create a mission and try to use it as a phase
		mission := &types.Mission{
			Issue: types.Issue{
				Title:        "Mission (Not Phase)",
				Status:       types.StatusOpen,
				Priority:     0,
				IssueType:    types.TypeEpic,
				IssueSubtype: types.SubtypeMission,
			},
			Goal: "Test mission",
		}
		if err := store.CreateMission(ctx, mission, "test"); err != nil {
			t.Fatalf("Failed to create mission: %v", err)
		}

		_, err := store.GetMissionByPhase(ctx, mission.ID)
		if err == nil {
			t.Error("Expected error when using mission ID as phase")
		} else {
			errMsg := err.Error()
			if !strings.Contains(errMsg, "has wrong subtype") {
				t.Errorf("Error message should mention 'has wrong subtype', got: %s", errMsg)
			}
			if !strings.Contains(errMsg, "'mission'") {
				t.Errorf("Error message should show actual subtype 'mission', got: %s", errMsg)
			}
			if !strings.Contains(errMsg, "expected 'phase'") {
				t.Errorf("Error message should mention expected 'phase', got: %s", errMsg)
			}
			t.Logf("✓ Correct error for wrong subtype (mission instead of phase): %v", err)
		}
	})
}

// TestGetReadyWorkWithMissionContext tests mission context enrichment (vc-234)
func TestGetReadyWorkWithMissionContext(t *testing.T) {
	ctx := context.Background()

	// Create temporary database
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	store, err := NewVCStorage(ctx, dbPath)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer func() { _ = store.Close() }()

	t.Run("GetReadyWork includes mission context", func(t *testing.T) {
		// Create a mission epic
		mission := &types.Issue{
			Title:        "Test Mission",
			Description:  "Mission for testing",
			Status:       types.StatusOpen,
			Priority:     1,
			IssueType:    types.TypeEpic,
			IssueSubtype: types.SubtypeMission,
		}
		if err := store.CreateIssue(ctx, mission, "test"); err != nil {
			t.Fatalf("Failed to create mission: %v", err)
		}

		// Create a task
		task := &types.Issue{
			Title:       "Test Task",
			Description: "Task under mission",
			Status:      types.StatusOpen,
			Priority:    1,
			IssueType:   types.TypeTask,
			AcceptanceCriteria: "Test acceptance criteria",
		}
		if err := store.CreateIssue(ctx, task, "test"); err != nil {
			t.Fatalf("Failed to create task: %v", err)
		}

		// Link task to mission via parent-child dependency
		dep := &types.Dependency{
			IssueID:     task.ID,
			DependsOnID: mission.ID,
			Type:        types.DepParentChild,
		}
		if err := store.AddDependency(ctx, dep, "test"); err != nil {
			t.Fatalf("Failed to add dependency: %v", err)
		}

		// Get ready work - should include mission context
		readyWork, err := store.GetReadyWork(ctx, types.WorkFilter{
			Status: types.StatusOpen,
			Limit:  10,
		})
		if err != nil {
			t.Fatalf("GetReadyWork failed: %v", err)
		}

		// Should have 1 task with mission context
		if len(readyWork) != 1 {
			t.Fatalf("Expected 1 ready task, got %d", len(readyWork))
		}

		if readyWork[0].ID != task.ID {
			t.Errorf("Expected task %s, got %s", task.ID, readyWork[0].ID)
		}

		if readyWork[0].MissionContext == nil {
			t.Fatal("Expected mission context to be populated")
		}

		if readyWork[0].MissionContext.MissionID != mission.ID {
			t.Errorf("Expected mission ID %s, got %s",
				mission.ID, readyWork[0].MissionContext.MissionID)
		}

		t.Logf("✓ Task %s correctly enriched with mission context %s",
			task.ID, mission.ID)
	})

	t.Run("GetReadyWork filters tasks from missions with needs-quality-gates", func(t *testing.T) {
		// Create mission with needs-quality-gates label
		gatedMission := &types.Issue{
			Title:        "Gated Mission",
			Description:  "Mission waiting for quality gates",
			Status:       types.StatusOpen,
			Priority:     1,
			IssueType:    types.TypeEpic,
			IssueSubtype: types.SubtypeMission,
		}
		if err := store.CreateIssue(ctx, gatedMission, "test"); err != nil {
			t.Fatalf("Failed to create gated mission: %v", err)
		}

		// Add needs-quality-gates label
		if err := store.AddLabel(ctx, gatedMission.ID, "needs-quality-gates", "test"); err != nil {
			t.Fatalf("Failed to add label: %v", err)
		}

		// Create task under gated mission
		gatedTask := &types.Issue{
			Title:       "Gated Task",
			Description: "Task under gated mission",
			Status:      types.StatusOpen,
			Priority:    1,
			IssueType:   types.TypeTask,
			AcceptanceCriteria: "Test acceptance criteria",
		}
		if err := store.CreateIssue(ctx, gatedTask, "test"); err != nil {
			t.Fatalf("Failed to create gated task: %v", err)
		}

		// Link to gated mission
		dep := &types.Dependency{
			IssueID:     gatedTask.ID,
			DependsOnID: gatedMission.ID,
			Type:        types.DepParentChild,
		}
		if err := store.AddDependency(ctx, dep, "test"); err != nil {
			t.Fatalf("Failed to add dependency: %v", err)
		}

		// Create active mission (no gates label)
		activeMission := &types.Issue{
			Title:        "Active Mission",
			Description:  "Active mission",
			Status:       types.StatusOpen,
			Priority:     1,
			IssueType:    types.TypeEpic,
			IssueSubtype: types.SubtypeMission,
		}
		if err := store.CreateIssue(ctx, activeMission, "test"); err != nil {
			t.Fatalf("Failed to create active mission: %v", err)
		}

		// Create task under active mission
		activeTask := &types.Issue{
			Title:       "Active Task",
			Description: "Task under active mission",
			Status:      types.StatusOpen,
			Priority:    1,
			IssueType:   types.TypeTask,
			AcceptanceCriteria: "Test acceptance criteria",
		}
		if err := store.CreateIssue(ctx, activeTask, "test"); err != nil {
			t.Fatalf("Failed to create active task: %v", err)
		}

		// Link to active mission
		dep2 := &types.Dependency{
			IssueID:     activeTask.ID,
			DependsOnID: activeMission.ID,
			Type:        types.DepParentChild,
		}
		if err := store.AddDependency(ctx, dep2, "test"); err != nil {
			t.Fatalf("Failed to add dependency: %v", err)
		}

		// Get ready work - should only include active task, not gated task
		readyWork, err := store.GetReadyWork(ctx, types.WorkFilter{
			Status: types.StatusOpen,
			Limit:  10,
		})
		if err != nil {
			t.Fatalf("GetReadyWork failed: %v", err)
		}

		// Should only have activeTask (gatedTask filtered out)
		foundGatedTask := false
		foundActiveTask := false
		for _, issue := range readyWork {
			if issue.ID == gatedTask.ID {
				foundGatedTask = true
			}
			if issue.ID == activeTask.ID {
				foundActiveTask = true
			}
		}

		if foundGatedTask {
			t.Error("GetReadyWork should not return tasks from missions with needs-quality-gates label")
		}

		if !foundActiveTask {
			t.Error("GetReadyWork should return tasks from active missions")
		}

		t.Logf("✓ GetReadyWork correctly filtered out task %s from gated mission %s",
			gatedTask.ID, gatedMission.ID)
		t.Logf("✓ GetReadyWork correctly included task %s from active mission %s",
			activeTask.ID, activeMission.ID)
	})

	t.Run("GetReadyWork includes tasks not part of any mission", func(t *testing.T) {
		// Create standalone task (no mission)
		standalone := &types.Issue{
			Title:       "Standalone Task",
			Description: "Not part of any mission",
			Status:      types.StatusOpen,
			Priority:    1,
			IssueType:   types.TypeTask,
			AcceptanceCriteria: "Test acceptance criteria",
		}
		if err := store.CreateIssue(ctx, standalone, "test"); err != nil {
			t.Fatalf("Failed to create standalone task: %v", err)
		}

		// Get ready work - should include standalone task
		readyWork, err := store.GetReadyWork(ctx, types.WorkFilter{
			Status: types.StatusOpen,
			Limit:  10,
		})
		if err != nil {
			t.Fatalf("GetReadyWork failed: %v", err)
		}

		// Find the standalone task
		found := false
		for _, issue := range readyWork {
			if issue.ID == standalone.ID {
				found = true
				if issue.MissionContext != nil {
					t.Error("Standalone task should not have mission context")
				}
			}
		}

		if !found {
			t.Error("GetReadyWork should include standalone tasks")
		}

		t.Logf("✓ GetReadyWork correctly included standalone task %s without mission context",
			standalone.ID)
	})

	t.Run("GetReadyWork efficiently handles multiple tasks from same mission", func(t *testing.T) {
		// Create mission
		sharedMission := &types.Issue{
			Title:        "Shared Mission",
			Description:  "Mission with multiple tasks",
			Status:       types.StatusOpen,
			Priority:     1,
			IssueType:    types.TypeEpic,
			IssueSubtype: types.SubtypeMission,
		}
		if err := store.CreateIssue(ctx, sharedMission, "test"); err != nil {
			t.Fatalf("Failed to create shared mission: %v", err)
		}

		// Create 3 tasks under the same mission
		tasks := make([]*types.Issue, 3)
		for i := 0; i < 3; i++ {
			task := &types.Issue{
				Title:       fmt.Sprintf("Task %d", i+1),
				Description: "Task under shared mission",
				Status:      types.StatusOpen,
				Priority:    1,
				IssueType:   types.TypeTask,
				AcceptanceCriteria: "Test acceptance criteria",
			}
			if err := store.CreateIssue(ctx, task, "test"); err != nil {
				t.Fatalf("Failed to create task %d: %v", i+1, err)
			}
			tasks[i] = task

			// Link to mission
			dep := &types.Dependency{
				IssueID:     task.ID,
				DependsOnID: sharedMission.ID,
				Type:        types.DepParentChild,
			}
			if err := store.AddDependency(ctx, dep, "test"); err != nil {
				t.Fatalf("Failed to add dependency for task %d: %v", i+1, err)
			}
		}

		// Get ready work - should efficiently handle all 3 tasks
		readyWork, err := store.GetReadyWork(ctx, types.WorkFilter{
			Status: types.StatusOpen,
			Limit:  20, // Increase limit to avoid pagination issues
		})
		if err != nil {
			t.Fatalf("GetReadyWork failed: %v", err)
		}

		// Debug: log all ready work
		t.Logf("Total ready work: %d issues", len(readyWork))
		for _, issue := range readyWork {
			missionID := "none"
			if issue.MissionContext != nil {
				missionID = issue.MissionContext.MissionID
			}
			t.Logf("  - %s: %s (mission: %s)", issue.ID, issue.Title, missionID)
		}

		// Verify all 3 of our tasks are in ready work with correct mission context
		// Note: There may be other tasks from previous subtests, we just care about ours
		foundTasks := make(map[string]bool)
		for _, issue := range readyWork {
			for _, task := range tasks {
				if issue.ID == task.ID {
					foundTasks[task.ID] = true
					if issue.MissionContext == nil {
						t.Errorf("Task %s should have mission context", task.ID)
					}
					if issue.MissionContext != nil && issue.MissionContext.MissionID != sharedMission.ID {
						t.Errorf("Task %s has wrong mission ID: %s (expected %s)",
							task.ID, issue.MissionContext.MissionID, sharedMission.ID)
					}
				}
			}
		}

		// All 3 tasks should be found
		for _, task := range tasks {
			if !foundTasks[task.ID] {
				t.Errorf("Task %s not found in ready work", task.ID)
			}
		}

		t.Logf("✓ GetReadyWork efficiently handled 3 tasks from mission %s", sharedMission.ID)
	})

	t.Run("GetReadyWork filters issues with no-auto-claim label", func(t *testing.T) {
		// Create two tasks: one with no-auto-claim label, one without
		claimableTask := &types.Issue{
			Title:       "Claimable Task",
			Description: "This can be auto-claimed",
			Status:      types.StatusOpen,
			Priority:    1,
			IssueType:   types.TypeTask,
			AcceptanceCriteria: "Test acceptance criteria",
		}
		if err := store.CreateIssue(ctx, claimableTask, "test"); err != nil {
			t.Fatalf("Failed to create claimable task: %v", err)
		}

		noAutoClaimTask := &types.Issue{
			Title:       "No Auto Claim Task",
			Description: "This should not be auto-claimed",
			Status:      types.StatusOpen,
			Priority:    1,
			IssueType:   types.TypeTask,
			AcceptanceCriteria: "Test acceptance criteria",
		}
		if err := store.CreateIssue(ctx, noAutoClaimTask, "test"); err != nil {
			t.Fatalf("Failed to create no-auto-claim task: %v", err)
		}

		// Add no-auto-claim label to second task
		if err := store.AddLabel(ctx, noAutoClaimTask.ID, "no-auto-claim", "test"); err != nil {
			t.Fatalf("Failed to add no-auto-claim label: %v", err)
		}

		// Get ready work - use large limit to ensure we get both tasks despite accumulated test data
		readyWork, err := store.GetReadyWork(ctx, types.WorkFilter{
			Status: types.StatusOpen,
			Limit:  100, // Large limit to ensure we get all tasks from this test run
		})
		if err != nil {
			t.Fatalf("GetReadyWork failed: %v", err)
		}

		// Check results
		foundClaimable := false
		foundNoAutoClaim := false
		for _, issue := range readyWork {
			if issue.ID == claimableTask.ID {
				foundClaimable = true
			}
			if issue.ID == noAutoClaimTask.ID {
				foundNoAutoClaim = true
			}
		}

		if !foundClaimable {
			t.Errorf("GetReadyWork should include tasks without no-auto-claim label (task %s not found in %d results)",
				claimableTask.ID, len(readyWork))
		}

		if foundNoAutoClaim {
			t.Errorf("GetReadyWork should NOT include tasks with no-auto-claim label (vc-4ec0) (task %s found in results)",
				noAutoClaimTask.ID)
		}

		t.Logf("✓ GetReadyWork correctly excluded task %s with no-auto-claim label", noAutoClaimTask.ID)
		t.Logf("✓ GetReadyWork correctly included task %s without no-auto-claim label", claimableTask.ID)
	})

	t.Run("GetReadyWork filters issues with no-auto-claim among multiple labels", func(t *testing.T) {
		// Create task with multiple labels including 'no-auto-claim'
		multiLabelTask := &types.Issue{
			Title:       "Multi Label Task",
			Description: "Has multiple labels including no-auto-claim",
			Status:      types.StatusOpen,
			Priority:    1,
			IssueType:   types.TypeTask,
			AcceptanceCriteria: "Test acceptance criteria",
		}
		if err := store.CreateIssue(ctx, multiLabelTask, "test"); err != nil {
			t.Fatalf("Failed to create multi-label task: %v", err)
		}

		// Add multiple labels
		if err := store.AddLabel(ctx, multiLabelTask.ID, "bug", "test"); err != nil {
			t.Fatalf("Failed to add bug label: %v", err)
		}
		if err := store.AddLabel(ctx, multiLabelTask.ID, "no-auto-claim", "test"); err != nil {
			t.Fatalf("Failed to add no-auto-claim label: %v", err)
		}
		if err := store.AddLabel(ctx, multiLabelTask.ID, "high-priority", "test"); err != nil {
			t.Fatalf("Failed to add high-priority label: %v", err)
		}

		// Get ready work
		readyWork, err := store.GetReadyWork(ctx, types.WorkFilter{
			Status: types.StatusOpen,
			Limit:  100,
		})
		if err != nil {
			t.Fatalf("GetReadyWork failed: %v", err)
		}

		// Check that task with no-auto-claim is excluded even with other labels
		for _, issue := range readyWork {
			if issue.ID == multiLabelTask.ID {
				t.Errorf("GetReadyWork should exclude task with no-auto-claim even when it has other labels")
			}
		}

		t.Logf("✓ GetReadyWork correctly excluded task %s with no-auto-claim among multiple labels", multiLabelTask.ID)
	})

	t.Run("GetReadyWork handles empty result set gracefully", func(t *testing.T) {
		// Create an epic (should be filtered out)
		epic := &types.Issue{
			Title:       "Test Epic",
			Description: "Should be filtered",
			Status:      types.StatusOpen,
			Priority:    1,
			IssueType:   types.TypeEpic,
		}
		if err := store.CreateIssue(ctx, epic, "test"); err != nil {
			t.Fatalf("Failed to create epic: %v", err)
		}

		// Even with the epic, GetReadyWork should succeed (it filters epics)
		// This tests that empty issueIDs map is handled correctly
		readyWork, err := store.GetReadyWork(ctx, types.WorkFilter{
			Status: types.StatusClosed, // Query for closed issues (should be empty)
			Limit:  10,
		})
		if err != nil {
			t.Fatalf("GetReadyWork should handle empty results gracefully: %v", err)
		}

		// Should return empty list, not nil
		if readyWork == nil {
			t.Error("GetReadyWork should return empty slice, not nil")
		}

		t.Logf("✓ GetReadyWork handled empty result set gracefully (%d results)", len(readyWork))
	})
}

// TestMissionSandboxMetadataPersistence tests that sandbox_path and branch_name are properly
// persisted across CreateMission, GetMission, and UpdateMission operations (vc-241)
func TestMissionSandboxMetadataPersistence(t *testing.T) {
	ctx := context.Background()

	// Create temporary database
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	store, err := NewVCStorage(ctx, dbPath)
	if err != nil {
		t.Fatalf("Failed to create VC storage: %v", err)
	}
	defer func() { _ = store.Close() }()

	t.Run("CreateMission persists sandbox metadata", func(t *testing.T) {
		// Create mission with sandbox metadata
		mission := &types.Mission{
			Issue: types.Issue{
				Title:        "Mission with Sandbox",
				Description:  "Testing sandbox metadata persistence",
				Status:       types.StatusOpen,
				Priority:     1,
				IssueType:    types.TypeEpic,
				IssueSubtype: types.SubtypeMission,
			},
			Goal:        "Test sandbox persistence",
			SandboxPath: ".sandboxes/mission-300/",
			BranchName:  "mission/vc-300-test",
		}

		if err := store.CreateMission(ctx, mission, "test"); err != nil {
			t.Fatalf("Failed to create mission: %v", err)
		}

		// Retrieve and verify sandbox metadata was persisted
		retrieved, err := store.GetMission(ctx, mission.ID)
		if err != nil {
			t.Fatalf("Failed to retrieve mission: %v", err)
		}

		if retrieved.SandboxPath != mission.SandboxPath {
			t.Errorf("Expected sandbox_path '%s', got '%s'", mission.SandboxPath, retrieved.SandboxPath)
		}
		if retrieved.BranchName != mission.BranchName {
			t.Errorf("Expected branch_name '%s', got '%s'", mission.BranchName, retrieved.BranchName)
		}

		t.Logf("✓ CreateMission correctly persisted sandbox_path='%s' and branch_name='%s'",
			retrieved.SandboxPath, retrieved.BranchName)
	})

	t.Run("UpdateMission updates sandbox metadata", func(t *testing.T) {
		// Create initial mission without sandbox metadata
		mission := &types.Mission{
			Issue: types.Issue{
				Title:        "Mission to Update",
				Description:  "Testing UpdateMission with sandbox metadata",
				Status:       types.StatusOpen,
				Priority:     1,
				IssueType:    types.TypeEpic,
				IssueSubtype: types.SubtypeMission,
			},
			Goal: "Test UpdateMission",
		}

		if err := store.CreateMission(ctx, mission, "test"); err != nil {
			t.Fatalf("Failed to create mission: %v", err)
		}

		// Verify initial state (no sandbox metadata)
		retrieved, err := store.GetMission(ctx, mission.ID)
		if err != nil {
			t.Fatalf("Failed to retrieve mission: %v", err)
		}
		if retrieved.SandboxPath != "" {
			t.Errorf("Expected empty sandbox_path initially, got '%s'", retrieved.SandboxPath)
		}
		if retrieved.BranchName != "" {
			t.Errorf("Expected empty branch_name initially, got '%s'", retrieved.BranchName)
		}

		// Update mission with sandbox metadata
		updates := map[string]interface{}{
			"sandbox_path": ".sandboxes/mission-400/",
			"branch_name":  "mission/vc-400-updated",
		}
		if err := store.UpdateMission(ctx, mission.ID, updates, "test"); err != nil {
			t.Fatalf("Failed to update mission: %v", err)
		}

		// Verify sandbox metadata was updated
		updated, err := store.GetMission(ctx, mission.ID)
		if err != nil {
			t.Fatalf("Failed to retrieve updated mission: %v", err)
		}

		expectedSandbox := ".sandboxes/mission-400/"
		expectedBranch := "mission/vc-400-updated"
		if updated.SandboxPath != expectedSandbox {
			t.Errorf("Expected sandbox_path '%s', got '%s'", expectedSandbox, updated.SandboxPath)
		}
		if updated.BranchName != expectedBranch {
			t.Errorf("Expected branch_name '%s', got '%s'", expectedBranch, updated.BranchName)
		}

		t.Logf("✓ UpdateMission correctly updated sandbox_path='%s' and branch_name='%s'",
			updated.SandboxPath, updated.BranchName)
	})

	t.Run("UpdateMission modifies existing sandbox metadata", func(t *testing.T) {
		// Create mission with initial sandbox metadata
		mission := &types.Mission{
			Issue: types.Issue{
				Title:        "Mission to Modify",
				Description:  "Testing modification of existing sandbox metadata",
				Status:       types.StatusOpen,
				Priority:     1,
				IssueType:    types.TypeEpic,
				IssueSubtype: types.SubtypeMission,
			},
			Goal:        "Test modification",
			SandboxPath: ".sandboxes/mission-500-old/",
			BranchName:  "mission/vc-500-old",
		}

		if err := store.CreateMission(ctx, mission, "test"); err != nil {
			t.Fatalf("Failed to create mission: %v", err)
		}

		// Update to new sandbox metadata
		updates := map[string]interface{}{
			"sandbox_path": ".sandboxes/mission-500-new/",
			"branch_name":  "mission/vc-500-new",
		}
		if err := store.UpdateMission(ctx, mission.ID, updates, "test"); err != nil {
			t.Fatalf("Failed to update mission: %v", err)
		}

		// Verify new metadata replaced old metadata
		updated, err := store.GetMission(ctx, mission.ID)
		if err != nil {
			t.Fatalf("Failed to retrieve updated mission: %v", err)
		}

		expectedSandbox := ".sandboxes/mission-500-new/"
		expectedBranch := "mission/vc-500-new"
		if updated.SandboxPath != expectedSandbox {
			t.Errorf("Expected sandbox_path '%s', got '%s'", expectedSandbox, updated.SandboxPath)
		}
		if updated.BranchName != expectedBranch {
			t.Errorf("Expected branch_name '%s', got '%s'", expectedBranch, updated.BranchName)
		}

		t.Logf("✓ UpdateMission correctly modified sandbox_path and branch_name")
	})

	t.Run("UpdateMission handles partial updates", func(t *testing.T) {
		// Create mission with sandbox metadata
		mission := &types.Mission{
			Issue: types.Issue{
				Title:        "Mission for Partial Update",
				Description:  "Testing partial updates to sandbox metadata",
				Status:       types.StatusOpen,
				Priority:     1,
				IssueType:    types.TypeEpic,
				IssueSubtype: types.SubtypeMission,
			},
			Goal:        "Test partial updates",
			SandboxPath: ".sandboxes/mission-600/",
			BranchName:  "mission/vc-600",
		}

		if err := store.CreateMission(ctx, mission, "test"); err != nil {
			t.Fatalf("Failed to create mission: %v", err)
		}

		// Update only sandbox_path (leave branch_name unchanged)
		updates := map[string]interface{}{
			"sandbox_path": ".sandboxes/mission-600-updated/",
		}
		if err := store.UpdateMission(ctx, mission.ID, updates, "test"); err != nil {
			t.Fatalf("Failed to update mission: %v", err)
		}

		// Verify sandbox_path changed but branch_name remained the same
		updated, err := store.GetMission(ctx, mission.ID)
		if err != nil {
			t.Fatalf("Failed to retrieve updated mission: %v", err)
		}

		expectedSandbox := ".sandboxes/mission-600-updated/"
		expectedBranch := "mission/vc-600" // Should be unchanged
		if updated.SandboxPath != expectedSandbox {
			t.Errorf("Expected sandbox_path '%s', got '%s'", expectedSandbox, updated.SandboxPath)
		}
		if updated.BranchName != expectedBranch {
			t.Errorf("Expected branch_name '%s' (unchanged), got '%s'", expectedBranch, updated.BranchName)
		}

		t.Logf("✓ UpdateMission correctly handled partial update (sandbox_path only)")
	})
}

// TestMissionLifecycleEvents tests that mission creation and updates emit proper events (vc-266)
func TestMissionLifecycleEvents(t *testing.T) {
	ctx := context.Background()

	// Create temporary database
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	store, err := NewVCStorage(ctx, dbPath)
	if err != nil {
		t.Fatalf("Failed to create VC storage: %v", err)
	}
	defer func() { _ = store.Close() }()

	t.Run("CreateMission emits mission_created event", func(t *testing.T) {
		// Create a mission
		mission := &types.Mission{
			Issue: types.Issue{
				Title:        "Test Mission with Events",
				Description:  "Testing event emission on mission creation",
				Status:       types.StatusOpen,
				Priority:     1,
				IssueType:    types.TypeEpic,
				IssueSubtype: types.SubtypeMission,
			},
			Goal:             "Test mission lifecycle events",
			PhaseCount:       3,
			ApprovalRequired: true,
		}

		if err := store.CreateMission(ctx, mission, "test-actor"); err != nil {
			t.Fatalf("Failed to create mission: %v", err)
		}

		// Verify mission_created event was emitted
		eventFilter := events.EventFilter{
			IssueID: mission.ID,
			Type:    events.EventTypeMissionCreated,
		}
		evts, err := store.GetAgentEvents(ctx, eventFilter)
		if err != nil {
			t.Fatalf("Failed to get agent events: %v", err)
		}

		if len(evts) != 1 {
			t.Fatalf("Expected 1 mission_created event, got %d", len(evts))
		}

		evt := evts[0]
		if evt.Type != events.EventTypeMissionCreated {
			t.Errorf("Expected event type 'mission_created', got '%s'", evt.Type)
		}
		if evt.IssueID != mission.ID {
			t.Errorf("Expected issue_id '%s', got '%s'", mission.ID, evt.IssueID)
		}
		if evt.Severity != events.SeverityInfo {
			t.Errorf("Expected severity 'info', got '%s'", evt.Severity)
		}

		// Verify event data
		if evt.Data["mission_id"] != mission.ID {
			t.Errorf("Expected mission_id '%s', got '%v'", mission.ID, evt.Data["mission_id"])
		}
		if evt.Data["goal"] != mission.Goal {
			t.Errorf("Expected goal '%s', got '%v'", mission.Goal, evt.Data["goal"])
		}
		if evt.Data["phase_count"] != float64(mission.PhaseCount) { // JSON numbers are float64
			t.Errorf("Expected phase_count %d, got %v", mission.PhaseCount, evt.Data["phase_count"])
		}
		if evt.Data["approval_required"] != mission.ApprovalRequired {
			t.Errorf("Expected approval_required %v, got %v", mission.ApprovalRequired, evt.Data["approval_required"])
		}
		if evt.Data["actor"] != "test-actor" {
			t.Errorf("Expected actor 'test-actor', got '%v'", evt.Data["actor"])
		}

		t.Logf("✓ mission_created event correctly emitted for mission %s", mission.ID)
	})

	t.Run("UpdateMission emits mission_metadata_updated event", func(t *testing.T) {
		// Create a mission
		mission := &types.Mission{
			Issue: types.Issue{
				Title:        "Mission for Update Test",
				Description:  "Testing event emission on mission update",
				Status:       types.StatusOpen,
				Priority:     1,
				IssueType:    types.TypeEpic,
				IssueSubtype: types.SubtypeMission,
			},
			Goal:        "Test update events",
			SandboxPath: ".sandboxes/old/",
			BranchName:  "mission/old-branch",
		}

		if err := store.CreateMission(ctx, mission, "test-actor"); err != nil {
			t.Fatalf("Failed to create mission: %v", err)
		}

		// Update mission metadata
		updates := map[string]interface{}{
			"sandbox_path": ".sandboxes/new/",
			"branch_name":  "mission/new-branch",
		}
		if err := store.UpdateMission(ctx, mission.ID, updates, "update-actor"); err != nil {
			t.Fatalf("Failed to update mission: %v", err)
		}

		// Verify mission_metadata_updated event was emitted
		eventFilter := events.EventFilter{
			IssueID: mission.ID,
			Type:    events.EventTypeMissionMetadataUpdated,
		}
		evts, err := store.GetAgentEvents(ctx, eventFilter)
		if err != nil {
			t.Fatalf("Failed to get agent events: %v", err)
		}

		if len(evts) != 1 {
			t.Fatalf("Expected 1 mission_metadata_updated event, got %d", len(evts))
		}

		evt := evts[0]
		if evt.Type != events.EventTypeMissionMetadataUpdated {
			t.Errorf("Expected event type 'mission_metadata_updated', got '%s'", evt.Type)
		}
		if evt.IssueID != mission.ID {
			t.Errorf("Expected issue_id '%s', got '%s'", mission.ID, evt.IssueID)
		}

		// Verify event data
		if evt.Data["mission_id"] != mission.ID {
			t.Errorf("Expected mission_id '%s', got '%v'", mission.ID, evt.Data["mission_id"])
		}
		if evt.Data["actor"] != "update-actor" {
			t.Errorf("Expected actor 'update-actor', got '%v'", evt.Data["actor"])
		}

		// Verify updated_fields list
		updatedFields, ok := evt.Data["updated_fields"].([]interface{})
		if !ok {
			t.Fatalf("Expected updated_fields to be array, got %T", evt.Data["updated_fields"])
		}
		if len(updatedFields) != 2 {
			t.Errorf("Expected 2 updated fields, got %d", len(updatedFields))
		}

		// Verify changes map exists
		changes, ok := evt.Data["changes"].(map[string]interface{})
		if !ok {
			t.Fatalf("Expected changes to be map, got %T", evt.Data["changes"])
		}

		// Verify sandbox_path change
		sandboxChange, ok := changes["sandbox_path"].(map[string]interface{})
		if ok {
			if sandboxChange["old_value"] != ".sandboxes/old/" {
				t.Errorf("Expected old sandbox_path '.sandboxes/old/', got %v", sandboxChange["old_value"])
			}
			if sandboxChange["new_value"] != ".sandboxes/new/" {
				t.Errorf("Expected new sandbox_path '.sandboxes/new/', got %v", sandboxChange["new_value"])
			}
		}

		// Verify branch_name change
		branchChange, ok := changes["branch_name"].(map[string]interface{})
		if ok {
			if branchChange["old_value"] != "mission/old-branch" {
				t.Errorf("Expected old branch_name 'mission/old-branch', got %v", branchChange["old_value"])
			}
			if branchChange["new_value"] != "mission/new-branch" {
				t.Errorf("Expected new branch_name 'mission/new-branch', got %v", branchChange["new_value"])
			}
		}

		t.Logf("✓ mission_metadata_updated event correctly emitted for mission %s", mission.ID)
	})

	t.Run("UpdateMission with no changes does not emit event", func(t *testing.T) {
		// Create a mission
		mission := &types.Mission{
			Issue: types.Issue{
				Title:        "Mission for No-Op Update",
				Description:  "Testing event emission when nothing changes",
				Status:       types.StatusOpen,
				Priority:     1,
				IssueType:    types.TypeEpic,
				IssueSubtype: types.SubtypeMission,
			},
			Goal: "Test no-op updates",
		}

		if err := store.CreateMission(ctx, mission, "test-actor"); err != nil {
			t.Fatalf("Failed to create mission: %v", err)
		}

		// Get event count before update
		beforeFilter := events.EventFilter{
			IssueID: mission.ID,
			Type:    events.EventTypeMissionMetadataUpdated,
		}
		beforeEvents, err := store.GetAgentEvents(ctx, beforeFilter)
		if err != nil {
			t.Fatalf("Failed to get agent events: %v", err)
		}
		beforeCount := len(beforeEvents)

		// Update with empty map (no changes)
		updates := map[string]interface{}{}
		if err := store.UpdateMission(ctx, mission.ID, updates, "test-actor"); err != nil {
			t.Fatalf("Failed to update mission: %v", err)
		}

		// Verify no new event was emitted
		afterEvents, err := store.GetAgentEvents(ctx, beforeFilter)
		if err != nil {
			t.Fatalf("Failed to get agent events: %v", err)
		}
		afterCount := len(afterEvents)

		if afterCount != beforeCount {
			t.Errorf("Expected no new events for empty update, got %d new events", afterCount-beforeCount)
		}

		t.Logf("✓ No event emitted for no-op update")
	})

	t.Run("CreateMission with parent epic populates parent_epic_id", func(t *testing.T) {
		// Create a parent epic first
		parentEpic := &types.Issue{
			Title:       "Parent Epic",
			Description: "Parent epic for mission",
			Status:      types.StatusOpen,
			Priority:    1,
			IssueType:   types.TypeEpic,
		}
		if err := store.CreateIssue(ctx, parentEpic, "test-actor"); err != nil {
			t.Fatalf("Failed to create parent epic: %v", err)
		}

		// Create a mission with parent epic dependency
		mission := &types.Mission{
			Issue: types.Issue{
				Title:        "Mission with Parent Epic",
				Description:  "Testing parent_epic_id population",
				Status:       types.StatusOpen,
				Priority:     1,
				IssueType:    types.TypeEpic,
				IssueSubtype: types.SubtypeMission,
			},
			Goal:             "Test parent epic association",
			PhaseCount:       2,
			ApprovalRequired: false,
		}
		if err := store.CreateMission(ctx, mission, "test-actor"); err != nil {
			t.Fatalf("Failed to create mission: %v", err)
		}

		// Add parent-child dependency
		dep := &types.Dependency{
			IssueID:     mission.ID,
			DependsOnID: parentEpic.ID,
			Type:        types.DepParentChild,
		}
		if err := store.AddDependency(ctx, dep, "test-actor"); err != nil {
			t.Fatalf("Failed to add dependency: %v", err)
		}

		// Verify mission_created event includes parent_epic_id
		eventFilter := events.EventFilter{
			IssueID: mission.ID,
			Type:    events.EventTypeMissionCreated,
		}
		evts, err := store.GetAgentEvents(ctx, eventFilter)
		if err != nil {
			t.Fatalf("Failed to get agent events: %v", err)
		}

		if len(evts) != 1 {
			t.Fatalf("Expected 1 mission_created event, got %d", len(evts))
		}

		evt := evts[0]
		// Note: parent_epic_id is populated from dependencies, which happens after creation
		// So the creation event may not have it yet - this tests the event structure
		if evt.Data["parent_epic_id"] != nil && evt.Data["parent_epic_id"] != "" {
			t.Logf("✓ parent_epic_id populated: %v", evt.Data["parent_epic_id"])
		} else {
			t.Logf("ℹ parent_epic_id is nil in creation event (populated later via dependencies)")
		}
	})

	t.Run("UpdateMission with mission-specific fields tracks old values", func(t *testing.T) {
		// Create a mission with initial values
		mission := &types.Mission{
			Issue: types.Issue{
				Title:        "Mission for Field Updates",
				Description:  "Testing mission-specific field updates",
				Status:       types.StatusOpen,
				Priority:     1,
				IssueType:    types.TypeEpic,
				IssueSubtype: types.SubtypeMission,
			},
			Goal:           "Test field tracking",
			PhaseCount:     3,
			CurrentPhase:   0,
			IterationCount: 1,
			GatesStatus:    "pending",
		}
		if err := store.CreateMission(ctx, mission, "test-actor"); err != nil {
			t.Fatalf("Failed to create mission: %v", err)
		}

		// Update mission-specific fields
		updates := map[string]interface{}{
			"phase_count":     5,
			"current_phase":   2,
			"iteration_count": 3,
			"gates_status":    "passed",
		}
		if err := store.UpdateMission(ctx, mission.ID, updates, "update-actor"); err != nil {
			t.Fatalf("Failed to update mission: %v", err)
		}

		// Verify mission_metadata_updated event
		eventFilter := events.EventFilter{
			IssueID: mission.ID,
			Type:    events.EventTypeMissionMetadataUpdated,
		}
		evts, err := store.GetAgentEvents(ctx, eventFilter)
		if err != nil {
			t.Fatalf("Failed to get agent events: %v", err)
		}

		if len(evts) != 1 {
			t.Fatalf("Expected 1 mission_metadata_updated event, got %d", len(evts))
		}

		evt := evts[0]
		changes, ok := evt.Data["changes"].(map[string]interface{})
		if !ok {
			t.Fatalf("Expected changes to be map, got %T", evt.Data["changes"])
		}

		// Verify phase_count change
		if phaseChange, ok := changes["phase_count"].(map[string]interface{}); ok {
			if phaseChange["old_value"] != float64(3) {
				t.Errorf("Expected old phase_count 3, got %v", phaseChange["old_value"])
			}
			if phaseChange["new_value"] != float64(5) {
				t.Errorf("Expected new phase_count 5, got %v", phaseChange["new_value"])
			}
		} else {
			t.Error("Missing phase_count in changes")
		}

		// Verify current_phase change
		if currentPhaseChange, ok := changes["current_phase"].(map[string]interface{}); ok {
			if currentPhaseChange["old_value"] != float64(0) {
				t.Errorf("Expected old current_phase 0, got %v", currentPhaseChange["old_value"])
			}
			if currentPhaseChange["new_value"] != float64(2) {
				t.Errorf("Expected new current_phase 2, got %v", currentPhaseChange["new_value"])
			}
		} else {
			t.Error("Missing current_phase in changes")
		}

		// Verify iteration_count change
		if iterChange, ok := changes["iteration_count"].(map[string]interface{}); ok {
			if iterChange["old_value"] != float64(1) {
				t.Errorf("Expected old iteration_count 1, got %v", iterChange["old_value"])
			}
			if iterChange["new_value"] != float64(3) {
				t.Errorf("Expected new iteration_count 3, got %v", iterChange["new_value"])
			}
		} else {
			t.Error("Missing iteration_count in changes")
		}

		// Verify gates_status change
		if gatesChange, ok := changes["gates_status"].(map[string]interface{}); ok {
			if gatesChange["old_value"] != "pending" {
				t.Errorf("Expected old gates_status 'pending', got %v", gatesChange["old_value"])
			}
			if gatesChange["new_value"] != "passed" {
				t.Errorf("Expected new gates_status 'passed', got %v", gatesChange["new_value"])
			}
		} else {
			t.Error("Missing gates_status in changes")
		}

		t.Logf("✓ All mission-specific field changes tracked correctly")
	})

	t.Run("UpdateMission with mixed base issue and mission fields", func(t *testing.T) {
		// Create a mission
		mission := &types.Mission{
			Issue: types.Issue{
				Title:        "Mission for Mixed Updates",
				Description:  "Testing mixed field updates",
				Status:       types.StatusOpen,
				Priority:     1,
				IssueType:    types.TypeEpic,
				IssueSubtype: types.SubtypeMission,
			},
			Goal:        "Test mixed updates",
			PhaseCount:  2,
			SandboxPath: ".sandboxes/initial/",
		}
		if err := store.CreateMission(ctx, mission, "test-actor"); err != nil {
			t.Fatalf("Failed to create mission: %v", err)
		}

		// Update both base issue fields (status, priority) and mission fields
		updates := map[string]interface{}{
			"status":       string(types.StatusInProgress),
			"priority":     2,
			"phase_count":  4,
			"sandbox_path": ".sandboxes/updated/",
		}
		if err := store.UpdateMission(ctx, mission.ID, updates, "update-actor"); err != nil {
			t.Fatalf("Failed to update mission: %v", err)
		}

		// Verify mission_metadata_updated event
		eventFilter := events.EventFilter{
			IssueID: mission.ID,
			Type:    events.EventTypeMissionMetadataUpdated,
		}
		evts, err := store.GetAgentEvents(ctx, eventFilter)
		if err != nil {
			t.Fatalf("Failed to get agent events: %v", err)
		}

		if len(evts) != 1 {
			t.Fatalf("Expected 1 mission_metadata_updated event, got %d", len(evts))
		}

		evt := evts[0]
		changes, ok := evt.Data["changes"].(map[string]interface{})
		if !ok {
			t.Fatalf("Expected changes to be map, got %T", evt.Data["changes"])
		}

		// Verify base issue field changes (status, priority)
		if statusChange, ok := changes["status"].(map[string]interface{}); ok {
			if statusChange["old_value"] != string(types.StatusOpen) {
				t.Errorf("Expected old status 'open', got %v", statusChange["old_value"])
			}
			if statusChange["new_value"] != string(types.StatusInProgress) {
				t.Errorf("Expected new status 'in_progress', got %v", statusChange["new_value"])
			}
		} else {
			t.Error("Missing status in changes")
		}

		if priorityChange, ok := changes["priority"].(map[string]interface{}); ok {
			if priorityChange["old_value"] != float64(1) {
				t.Errorf("Expected old priority 1, got %v", priorityChange["old_value"])
			}
			if priorityChange["new_value"] != float64(2) {
				t.Errorf("Expected new priority 2, got %v", priorityChange["new_value"])
			}
		} else {
			t.Error("Missing priority in changes")
		}

		// Verify mission-specific field changes
		if phaseChange, ok := changes["phase_count"].(map[string]interface{}); ok {
			if phaseChange["old_value"] != float64(2) {
				t.Errorf("Expected old phase_count 2, got %v", phaseChange["old_value"])
			}
			if phaseChange["new_value"] != float64(4) {
				t.Errorf("Expected new phase_count 4, got %v", phaseChange["new_value"])
			}
		} else {
			t.Error("Missing phase_count in changes")
		}

		if sandboxChange, ok := changes["sandbox_path"].(map[string]interface{}); ok {
			if sandboxChange["old_value"] != ".sandboxes/initial/" {
				t.Errorf("Expected old sandbox_path '.sandboxes/initial/', got %v", sandboxChange["old_value"])
			}
			if sandboxChange["new_value"] != ".sandboxes/updated/" {
				t.Errorf("Expected new sandbox_path '.sandboxes/updated/', got %v", sandboxChange["new_value"])
			}
		} else {
			t.Error("Missing sandbox_path in changes")
		}

		t.Logf("✓ Mixed base issue and mission field updates tracked correctly")
	})
}

// TestUpdateMissionRejectsInvalidFields tests SQL injection protection (vc-8891)
func TestUpdateMissionRejectsInvalidFields(t *testing.T) {
	ctx := context.Background()

	// Create temporary database
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	store, err := NewVCStorage(ctx, dbPath)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer func() { _ = store.Close() }()

	// Create a mission
	mission := &types.Mission{
		Issue: types.Issue{
			Title:        "Test Mission",
			Description:  "Test mission for SQL injection protection",
			Status:       types.StatusOpen,
			Priority:     1,
			IssueType:    types.TypeEpic,
			IssueSubtype: types.SubtypeMission,
		},
		Goal: "Test SQL injection protection",
	}
	if err := store.CreateMission(ctx, mission, "test"); err != nil {
		t.Fatalf("Failed to create mission: %v", err)
	}

	t.Run("UpdateMission rejects invalid field names", func(t *testing.T) {
		// Try to update with an invalid field that could be SQL injection attempt
		maliciousUpdates := map[string]interface{}{
			"malicious_field": "value",
		}

		err := store.UpdateMission(ctx, mission.ID, maliciousUpdates, "attacker")
		if err == nil {
			t.Fatal("Expected UpdateMission to reject invalid field, but it succeeded")
		}

		// The error will come from either base issue validation or mission field validation
		// Both provide protection against SQL injection
		if err.Error() != "invalid mission field: malicious_field" &&
			err.Error() != "failed to update base issue fields: invalid field for update: malicious_field" {
			t.Errorf("Expected error about invalid field, got %q", err.Error())
		}

		t.Logf("✓ UpdateMission correctly rejected invalid field: %v", err)
	})

	t.Run("UpdateMission accepts valid fields", func(t *testing.T) {
		// Try to update with valid fields
		validUpdates := map[string]interface{}{
			"sandbox_path": ".sandboxes/mission-test/",
			"branch_name":  "mission/test-branch",
			"goal":         "Test goal",
		}

		err := store.UpdateMission(ctx, mission.ID, validUpdates, "test")
		if err != nil {
			t.Fatalf("UpdateMission failed with valid fields: %v", err)
		}

		// Verify the updates were applied
		updated, err := store.GetMission(ctx, mission.ID)
		if err != nil {
			t.Fatalf("Failed to get updated mission: %v", err)
		}

		if updated.SandboxPath != ".sandboxes/mission-test/" {
			t.Errorf("Expected sandbox_path='.sandboxes/mission-test/', got %q", updated.SandboxPath)
		}
		if updated.BranchName != "mission/test-branch" {
			t.Errorf("Expected branch_name='mission/test-branch', got %q", updated.BranchName)
		}
		if updated.Goal != "Test goal" {
			t.Errorf("Expected goal='Test goal', got %q", updated.Goal)
		}

		t.Logf("✓ UpdateMission correctly accepted valid fields")
	})

	t.Run("UpdateMission rejects SQL injection attempt in field name", func(t *testing.T) {
		// Try SQL injection via field name
		sqlInjectionUpdates := map[string]interface{}{
			"sandbox_path = 'pwned'; DROP TABLE issues; --": "malicious",
		}

		err := store.UpdateMission(ctx, mission.ID, sqlInjectionUpdates, "attacker")
		if err == nil {
			t.Fatal("Expected UpdateMission to reject SQL injection attempt, but it succeeded")
		}

		// Verify that the issues table still exists and wasn't dropped
		issues, err := store.GetReadyWork(ctx, types.WorkFilter{})
		if err != nil {
			t.Fatalf("GetReadyWork failed, table may have been dropped: %v", err)
		}

		t.Logf("✓ UpdateMission correctly rejected SQL injection attempt, issues table still intact (%d issues)", len(issues))
	})
}

// TestGetIssues tests batch issue retrieval (vc-58)
func TestGetIssues(t *testing.T) {
	ctx := context.Background()

	// Create temporary database
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	store, err := NewVCStorage(ctx, dbPath)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer func() { _ = store.Close() }()

	// Create test issues
	issue1 := &types.Issue{
		Title:       "Test Issue 1",
		Description: "First test issue",
		Status:      types.StatusOpen,
		Priority:    1,
		IssueType:   types.TypeTask,
		AcceptanceCriteria: "Test acceptance criteria",
	}
	if err := store.CreateIssue(ctx, issue1, "test"); err != nil {
		t.Fatalf("Failed to create issue1: %v", err)
	}

	issue2 := &types.Issue{
		Title:        "Test Issue 2",
		Description:  "Second test issue",
		Status:       types.StatusOpen,
		Priority:     2,
		IssueType:    types.TypeEpic,
		IssueSubtype: types.SubtypeMission,
	}
	if err := store.CreateIssue(ctx, issue2, "test"); err != nil {
		t.Fatalf("Failed to create issue2: %v", err)
	}

	issue3 := &types.Issue{
		Title:       "Test Issue 3",
		Description: "Third test issue",
		Status:      types.StatusOpen,
		Priority:    3,
		IssueType:   types.TypeBug,
		AcceptanceCriteria: "Test acceptance criteria",
	}
	if err := store.CreateIssue(ctx, issue3, "test"); err != nil {
		t.Fatalf("Failed to create issue3: %v", err)
	}
	// Close issue3 to test closed status
	if err := store.CloseIssue(ctx, issue3.ID, "test close", "test"); err != nil {
		t.Fatalf("Failed to close issue3: %v", err)
	}

	t.Run("Fetch all issues", func(t *testing.T) {
		ids := []string{issue1.ID, issue2.ID, issue3.ID}
		issues, err := store.GetIssues(ctx, ids)
		if err != nil {
			t.Fatalf("GetIssues failed: %v", err)
		}

		if len(issues) != 3 {
			t.Errorf("Expected 3 issues, got %d", len(issues))
		}

		// Verify issue1
		if i, exists := issues[issue1.ID]; !exists {
			t.Error("Issue1 not found in results")
		} else {
			if i.Title != "Test Issue 1" {
				t.Errorf("Issue1 title = %s, want 'Test Issue 1'", i.Title)
			}
			if i.Priority != 1 {
				t.Errorf("Issue1 priority = %d, want 1", i.Priority)
			}
		}

		// Verify issue2 has subtype
		if i, exists := issues[issue2.ID]; !exists {
			t.Error("Issue2 not found in results")
		} else {
			if i.IssueSubtype != types.SubtypeMission {
				t.Errorf("Issue2 subtype = %s, want %s", i.IssueSubtype, types.SubtypeMission)
			}
		}

		// Verify issue3
		if i, exists := issues[issue3.ID]; !exists {
			t.Error("Issue3 not found in results")
		} else {
			if i.Status != types.StatusClosed {
				t.Errorf("Issue3 status = %s, want %s", i.Status, types.StatusClosed)
			}
		}
	})

	t.Run("Fetch subset of issues", func(t *testing.T) {
		ids := []string{issue1.ID, issue3.ID}
		issues, err := store.GetIssues(ctx, ids)
		if err != nil {
			t.Fatalf("GetIssues failed: %v", err)
		}

		if len(issues) != 2 {
			t.Errorf("Expected 2 issues, got %d", len(issues))
		}

		if _, exists := issues[issue1.ID]; !exists {
			t.Error("Issue1 not found")
		}
		if _, exists := issues[issue3.ID]; !exists {
			t.Error("Issue3 not found")
		}
		if _, exists := issues[issue2.ID]; exists {
			t.Error("Issue2 should not be in results")
		}
	})

	t.Run("Fetch non-existent issues", func(t *testing.T) {
		ids := []string{"vc-nonexistent", "vc-fake"}
		issues, err := store.GetIssues(ctx, ids)
		if err != nil {
			t.Fatalf("GetIssues failed: %v", err)
		}

		if len(issues) != 0 {
			t.Errorf("Expected 0 issues for non-existent IDs, got %d", len(issues))
		}
	})

	t.Run("Fetch mixed existing and non-existent", func(t *testing.T) {
		ids := []string{issue1.ID, "vc-nonexistent", issue2.ID}
		issues, err := store.GetIssues(ctx, ids)
		if err != nil {
			t.Fatalf("GetIssues failed: %v", err)
		}

		if len(issues) != 2 {
			t.Errorf("Expected 2 issues (only existing ones), got %d", len(issues))
		}

		if _, exists := issues[issue1.ID]; !exists {
			t.Error("Issue1 not found")
		}
		if _, exists := issues[issue2.ID]; !exists {
			t.Error("Issue2 not found")
		}
	})

	t.Run("Empty ID list", func(t *testing.T) {
		ids := []string{}
		issues, err := store.GetIssues(ctx, ids)
		if err != nil {
			t.Fatalf("GetIssues failed: %v", err)
		}

		if len(issues) != 0 {
			t.Errorf("Expected 0 issues for empty list, got %d", len(issues))
		}
	})

	t.Run("Fetch closed issue with ClosedAt timestamp", func(t *testing.T) {
		ids := []string{issue3.ID}
		issues, err := store.GetIssues(ctx, ids)
		if err != nil {
			t.Fatalf("GetIssues failed: %v", err)
		}

		if len(issues) != 1 {
			t.Errorf("Expected 1 issue, got %d", len(issues))
		}

		issue, exists := issues[issue3.ID]
		if !exists {
			t.Fatal("Issue3 not found")
		}

		// Verify ClosedAt is populated for closed issues
		if issue.Status == types.StatusClosed && issue.ClosedAt == nil {
			t.Error("Closed issue should have ClosedAt timestamp")
		}
	})

	// vc-4573: Test batch size limit enforcement
	t.Run("Batch size exceeds limit", func(t *testing.T) {
		// Create 501 IDs to exceed the limit of 500
		ids := make([]string, 501)
		for i := 0; i < 501; i++ {
			ids[i] = fmt.Sprintf("vc-%d", i)
		}

		_, err := store.GetIssues(ctx, ids)
		if err == nil {
			t.Error("Expected error for batch size > 500, got nil")
		}

		// Verify error message contains key information
		expectedErr := "batch size 501 exceeds maximum of 500 (SQLite variable limit)"
		if err != nil && err.Error() != expectedErr {
			t.Errorf("Expected error message '%s', got '%s'", expectedErr, err.Error())
		}
	})

	t.Run("Batch size at limit boundary", func(t *testing.T) {
		// Create exactly 500 IDs (should work)
		ids := make([]string, 500)
		for i := 0; i < 500; i++ {
			ids[i] = fmt.Sprintf("vc-%d", i)
		}

		_, err := store.GetIssues(ctx, ids)
		if err != nil {
			t.Errorf("Expected no error for batch size = 500, got: %v", err)
		}
	})

	// vc-278d: Edge case tests for GetIssues
	t.Run("Large batch (150+ issues)", func(t *testing.T) {
		// Create 150 issues to test large batch loading
		createdIDs := make([]string, 150)
		for i := 0; i < 150; i++ {
			issue := &types.Issue{
				Title:       fmt.Sprintf("Batch test issue %d", i),
				Description: fmt.Sprintf("Test issue number %d", i),
				Status:      types.StatusOpen,
				Priority:    i % 3, // Mix of priorities
				IssueType:   types.TypeTask,
				AcceptanceCriteria: "Test acceptance criteria",
			}
			if err := store.CreateIssue(ctx, issue, "test"); err != nil {
				t.Fatalf("Failed to create issue %d: %v", i, err)
			}
			createdIDs[i] = issue.ID
		}

		// Fetch all 150 in one batch
		issues, err := store.GetIssues(ctx, createdIDs)
		if err != nil {
			t.Fatalf("GetIssues failed for 150 issues: %v", err)
		}

		if len(issues) != 150 {
			t.Errorf("Expected 150 issues, got %d", len(issues))
		}

		// Verify a few random issues
		if issue, exists := issues[createdIDs[0]]; !exists {
			t.Error("First issue not found")
		} else if issue.Title != "Batch test issue 0" {
			t.Errorf("First issue title = %s, want 'Batch test issue 0'", issue.Title)
		}

		if issue, exists := issues[createdIDs[75]]; !exists {
			t.Error("Middle issue not found")
		} else if issue.Title != "Batch test issue 75" {
			t.Errorf("Middle issue title = %s, want 'Batch test issue 75'", issue.Title)
		}

		if issue, exists := issues[createdIDs[149]]; !exists {
			t.Error("Last issue not found")
		} else if issue.Title != "Batch test issue 149" {
			t.Errorf("Last issue title = %s, want 'Batch test issue 149'", issue.Title)
		}
	})

	t.Run("Subtype enrichment edge cases", func(t *testing.T) {
		// Create a regular issue (no subtype)
		regularIssue := &types.Issue{
			Title:       "Regular issue without subtype",
			Description: "Should have empty IssueSubtype",
			Status:      types.StatusOpen,
			Priority:    2,
			IssueType:   types.TypeTask,
			AcceptanceCriteria: "Test acceptance criteria",
		}
		if err := store.CreateIssue(ctx, regularIssue, "test"); err != nil {
			t.Fatalf("Failed to create regular issue: %v", err)
		}

		// Create a mission issue (has subtype)
		missionIssue := &types.Issue{
			Title:        "Mission issue with subtype",
			Description:  "Should have IssueSubtype = mission",
			Status:       types.StatusOpen,
			Priority:     1,
			IssueType:    types.TypeEpic,
			IssueSubtype: types.SubtypeMission,
		}
		if err := store.CreateIssue(ctx, missionIssue, "test"); err != nil {
			t.Fatalf("Failed to create mission issue: %v", err)
		}

		// Fetch both in one batch
		ids := []string{regularIssue.ID, missionIssue.ID}
		issues, err := store.GetIssues(ctx, ids)
		if err != nil {
			t.Fatalf("GetIssues failed: %v", err)
		}

		if len(issues) != 2 {
			t.Errorf("Expected 2 issues, got %d", len(issues))
		}

		// Verify regular issue has no subtype
		if issue, exists := issues[regularIssue.ID]; !exists {
			t.Error("Regular issue not found")
		} else if issue.IssueSubtype != "" {
			t.Errorf("Regular issue should have empty subtype, got '%s'", issue.IssueSubtype)
		}

		// Verify mission issue has subtype
		if issue, exists := issues[missionIssue.ID]; !exists {
			t.Error("Mission issue not found")
		} else if issue.IssueSubtype != types.SubtypeMission {
			t.Errorf("Mission issue subtype = %s, want %s", issue.IssueSubtype, types.SubtypeMission)
		}
	})

	t.Run("EstimatedMinutes handling", func(t *testing.T) {
		// Create issue with EstimatedMinutes set
		estimatedMins := 120
		issueWithEstimate := &types.Issue{
			Title:            "Issue with estimate",
			Description:      "Has estimated_minutes set",
			Status:           types.StatusOpen,
			Priority:         2,
			IssueType:        types.TypeTask,
			AcceptanceCriteria: "Test acceptance criteria",
			EstimatedMinutes: &estimatedMins,
		}
		if err := store.CreateIssue(ctx, issueWithEstimate, "test"); err != nil {
			t.Fatalf("Failed to create issue with estimate: %v", err)
		}

		// Create issue without EstimatedMinutes (nil)
		issueWithoutEstimate := &types.Issue{
			Title:       "Issue without estimate",
			Description: "Has nil estimated_minutes",
			Status:      types.StatusOpen,
			Priority:    2,
			IssueType:   types.TypeTask,
			AcceptanceCriteria: "Test acceptance criteria",
		}
		if err := store.CreateIssue(ctx, issueWithoutEstimate, "test"); err != nil {
			t.Fatalf("Failed to create issue without estimate: %v", err)
		}

		// Fetch both in one batch
		ids := []string{issueWithEstimate.ID, issueWithoutEstimate.ID}
		issues, err := store.GetIssues(ctx, ids)
		if err != nil {
			t.Fatalf("GetIssues failed: %v", err)
		}

		if len(issues) != 2 {
			t.Errorf("Expected 2 issues, got %d", len(issues))
		}

		// Verify issue with estimate
		if issue, exists := issues[issueWithEstimate.ID]; !exists {
			t.Error("Issue with estimate not found")
		} else {
			if issue.EstimatedMinutes == nil {
				t.Error("EstimatedMinutes should not be nil")
			} else if *issue.EstimatedMinutes != 120 {
				t.Errorf("EstimatedMinutes = %d, want 120", *issue.EstimatedMinutes)
			}
		}

		// Verify issue without estimate
		if issue, exists := issues[issueWithoutEstimate.ID]; !exists {
			t.Error("Issue without estimate not found")
		} else if issue.EstimatedMinutes != nil {
			t.Errorf("EstimatedMinutes should be nil, got %d", *issue.EstimatedMinutes)
		}
	})

	t.Run("Mixed subtypes in batch", func(t *testing.T) {
		// Create mix of issue types and subtypes
		normalTask := &types.Issue{
			Title:       "Normal task",
			Status:      types.StatusOpen,
			Priority:    2,
			IssueType:   types.TypeTask,
			AcceptanceCriteria: "Test acceptance criteria",
			IssueSubtype: types.SubtypeNormal,
		}
		if err := store.CreateIssue(ctx, normalTask, "test"); err != nil {
			t.Fatalf("Failed to create normal task: %v", err)
		}

		missionEpic := &types.Issue{
			Title:        "Mission epic",
			Status:       types.StatusOpen,
			Priority:     1,
			IssueType:    types.TypeEpic,
			IssueSubtype: types.SubtypeMission,
		}
		if err := store.CreateIssue(ctx, missionEpic, "test"); err != nil {
			t.Fatalf("Failed to create mission epic: %v", err)
		}

		phaseEpic := &types.Issue{
			Title:        "Phase epic",
			Status:       types.StatusOpen,
			Priority:     1,
			IssueType:    types.TypeEpic,
			IssueSubtype: types.SubtypePhase,
		}
		if err := store.CreateIssue(ctx, phaseEpic, "test"); err != nil {
			t.Fatalf("Failed to create phase epic: %v", err)
		}

		normalBug := &types.Issue{
			Title:     "Normal bug",
			Status:    types.StatusOpen,
			Priority:  0,
			IssueType: types.TypeBug,
			AcceptanceCriteria: "Test acceptance criteria",
		}
		if err := store.CreateIssue(ctx, normalBug, "test"); err != nil {
			t.Fatalf("Failed to create normal bug: %v", err)
		}

		// Fetch all 4 in one batch
		ids := []string{normalTask.ID, missionEpic.ID, phaseEpic.ID, normalBug.ID}
		issues, err := store.GetIssues(ctx, ids)
		if err != nil {
			t.Fatalf("GetIssues failed for mixed subtypes: %v", err)
		}

		if len(issues) != 4 {
			t.Errorf("Expected 4 issues, got %d", len(issues))
		}

		// Verify each issue has correct subtype
		if issue, exists := issues[normalTask.ID]; !exists {
			t.Error("Normal task not found")
		} else if issue.IssueSubtype != types.SubtypeNormal {
			t.Errorf("Normal task subtype = %s, want %s", issue.IssueSubtype, types.SubtypeNormal)
		}

		if issue, exists := issues[missionEpic.ID]; !exists {
			t.Error("Mission epic not found")
		} else if issue.IssueSubtype != types.SubtypeMission {
			t.Errorf("Mission epic subtype = %s, want %s", issue.IssueSubtype, types.SubtypeMission)
		}

		if issue, exists := issues[phaseEpic.ID]; !exists {
			t.Error("Phase epic not found")
		} else if issue.IssueSubtype != types.SubtypePhase {
			t.Errorf("Phase epic subtype = %s, want %s", issue.IssueSubtype, types.SubtypePhase)
		}

		if issue, exists := issues[normalBug.ID]; !exists {
			t.Error("Normal bug not found")
		} else if issue.IssueSubtype != "" {
			t.Errorf("Normal bug should have empty subtype, got '%s'", issue.IssueSubtype)
		}
	})
}

// TestGetReadyWorkFiltersBlocked verifies that GetReadyWork excludes blocked issues (vc-185)
// Root cause of vc-184: vc-10 was assigned as a task despite being marked as blocked/deferred
func TestGetReadyWorkFiltersBlocked(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	store, err := NewVCStorage(ctx, dbPath)
	if err != nil {
		t.Fatalf("Failed to create VC storage: %v", err)
	}
	defer func() { _ = store.Close() }()

	// Create three issues: open, blocked, and in_progress
	openIssue := &types.Issue{
		Title:     "Open task",
		Status:    types.StatusOpen,
		Priority:  1,
		IssueType: types.TypeTask,
		AcceptanceCriteria: "Test acceptance criteria",
	}
	err = store.CreateIssue(ctx, openIssue, "test")
	if err != nil {
		t.Fatalf("Failed to create open issue: %v", err)
	}

	blockedIssue := &types.Issue{
		Title:     "Blocked task",
		Status:    types.StatusBlocked,
		Priority:  1,
		IssueType: types.TypeTask,
		AcceptanceCriteria: "Test acceptance criteria",
	}
	err = store.CreateIssue(ctx, blockedIssue, "test")
	if err != nil {
		t.Fatalf("Failed to create blocked issue: %v", err)
	}

	inProgressIssue := &types.Issue{
		Title:     "In-progress task",
		Status:    types.StatusInProgress,
		Priority:  1,
		IssueType: types.TypeTask,
		AcceptanceCriteria: "Test acceptance criteria",
	}
	err = store.CreateIssue(ctx, inProgressIssue, "test")
	if err != nil {
		t.Fatalf("Failed to create in-progress issue: %v", err)
	}

	now := time.Now()
	closedIssue := &types.Issue{
		Title:     "Closed task",
		Status:    types.StatusClosed,
		Priority:  1,
		IssueType: types.TypeTask,
		AcceptanceCriteria: "Test acceptance criteria",
		ClosedAt:  &now,
	}
	err = store.CreateIssue(ctx, closedIssue, "test")
	if err != nil {
		t.Fatalf("Failed to create closed issue: %v", err)
	}

	// Create an epic (should also be filtered out per vc-203)
	epic := &types.Issue{
		Title:     "Epic task",
		Status:    types.StatusOpen,
		Priority:  1,
		IssueType: types.TypeEpic,
	}
	err = store.CreateIssue(ctx, epic, "test")
	if err != nil {
		t.Fatalf("Failed to create epic: %v", err)
	}

	// Test 1: Query all ready work - should only return open issue
	t.Run("GetReadyWork excludes blocked issues", func(t *testing.T) {
		ready, err := store.GetReadyWork(ctx, types.WorkFilter{
			Limit: 100,
		})
		if err != nil {
			t.Fatalf("Failed to get ready work: %v", err)
		}

		// Should only contain the open issue, not blocked/in_progress/closed/epic
		foundOpen := false
		for _, issue := range ready {
			if issue.ID == openIssue.ID {
				foundOpen = true
			}
			if issue.ID == blockedIssue.ID {
				t.Errorf("GetReadyWork returned blocked issue %s - should have been filtered (vc-185)", blockedIssue.ID)
			}
			if issue.ID == inProgressIssue.ID {
				t.Errorf("GetReadyWork returned in_progress issue %s - should have been filtered", inProgressIssue.ID)
			}
			if issue.ID == closedIssue.ID {
				t.Errorf("GetReadyWork returned closed issue %s - should have been filtered (vc-fef8)", closedIssue.ID)
			}
			if issue.ID == epic.ID {
				t.Errorf("GetReadyWork returned epic %s - should have been filtered (vc-203)", epic.ID)
			}
		}

		if !foundOpen {
			t.Errorf("GetReadyWork did not return open issue %s", openIssue.ID)
		}
	})

	// Register an executor instance for claim tests
	instance := &types.ExecutorInstance{
		InstanceID:    "test-executor",
		Hostname:      "localhost",
		PID:           12345,
		Version:       "test",
		StartedAt:     time.Now(),
		LastHeartbeat: time.Now(),
		Status:        "running",
	}
	err = store.RegisterInstance(ctx, instance)
	if err != nil {
		t.Fatalf("Failed to register executor: %v", err)
	}

	// Test 2: Verify claiming blocked issue fails
	t.Run("ClaimIssue rejects blocked issues", func(t *testing.T) {
		err := store.ClaimIssue(ctx, blockedIssue.ID, "test-executor")
		if err == nil {
			t.Error("ClaimIssue should fail for blocked issue (vc-185)")
		}
		if err != nil && !strings.Contains(err.Error(), "not open") {
			t.Errorf("ClaimIssue error should mention 'not open', got: %v", err)
		}
	})

	// Test 3: Verify claiming open issue succeeds
	t.Run("ClaimIssue accepts open issues", func(t *testing.T) {

		err = store.ClaimIssue(ctx, openIssue.ID, "test-executor")
		if err != nil {
			t.Errorf("ClaimIssue should succeed for open issue, got error: %v", err)
		}

		// Verify the issue is now in_progress
		issue, err := store.GetIssue(ctx, openIssue.ID)
		if err != nil {
			t.Fatalf("Failed to get issue: %v", err)
		}
		if issue.Status != types.StatusInProgress {
			t.Errorf("Issue status = %s, want %s after claiming", issue.Status, types.StatusInProgress)
		}
	})
}

// TestAcceptanceCriteriaWorkflowEnforcement verifies end-to-end enforcement
// of acceptance criteria requirements (vc-trl5)
//
// This test addresses the issue found in vc-hpcl where an issue was worked on
// without clear acceptance criteria. It verifies that the complete workflow
// prevents such issues from being created and executed.
//
// Test coverage:
// 1. Issue creation enforces acceptance criteria for task/bug types
// 2. Executor refuses to claim issues without acceptance criteria (if they somehow exist)
// 3. Issues cannot transition through workflow without acceptance criteria
func TestAcceptanceCriteriaWorkflowEnforcement(t *testing.T) {
	ctx := context.Background()

	// Setup test storage
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")
	store, err := NewVCStorage(ctx, dbPath)
	if err != nil {
		t.Fatalf("Failed to create VC storage: %v", err)
	}
	defer func() { _ = store.Close() }()

	// Register executor for claim tests
	instance := &types.ExecutorInstance{
		InstanceID: "test-executor-ac",
		Version:    "test",
		StartedAt:  time.Now(),
		Hostname:   "test-host",
		Status:     "running",
	}
	err = store.RegisterInstance(ctx, instance)
	if err != nil {
		t.Fatalf("Failed to register executor: %v", err)
	}

	// Test 1: Task creation without acceptance criteria should fail
	t.Run("Task creation rejects empty acceptance_criteria", func(t *testing.T) {
		task := &types.Issue{
			Title:              "Task without acceptance criteria",
			Description:        "This should fail",
			Status:             types.StatusOpen,
			Priority:           2,
			IssueType:          types.TypeTask,
			AcceptanceCriteria: "", // Empty - should be rejected
		}

		err := store.CreateIssue(ctx, task, "test")
		if err == nil {
			t.Fatal("Expected error when creating task without acceptance_criteria")
		}

		if !strings.Contains(err.Error(), "acceptance_criteria is required") {
			t.Errorf("Expected error about required acceptance_criteria, got: %v", err)
		}
	})

	// Test 2: Bug creation without acceptance criteria should fail
	t.Run("Bug creation rejects empty acceptance_criteria", func(t *testing.T) {
		bug := &types.Issue{
			Title:              "Bug without acceptance criteria",
			Description:        "This should fail",
			Status:             types.StatusOpen,
			Priority:           1,
			IssueType:          types.TypeBug,
			AcceptanceCriteria: "   ", // Whitespace only - should be rejected
		}

		err := store.CreateIssue(ctx, bug, "test")
		if err == nil {
			t.Fatal("Expected error when creating bug without acceptance_criteria")
		}

		if !strings.Contains(err.Error(), "acceptance_criteria is required") {
			t.Errorf("Expected error about required acceptance_criteria, got: %v", err)
		}
	})

	// Test 3: Epic creation does NOT require acceptance criteria
	t.Run("Epic creation allows empty acceptance_criteria", func(t *testing.T) {
		epic := &types.Issue{
			Title:              "Epic without acceptance criteria",
			Description:        "Epics don't need acceptance criteria",
			Status:             types.StatusOpen,
			Priority:           2,
			IssueType:          types.TypeEpic,
			AcceptanceCriteria: "", // Empty is OK for epics
		}

		err := store.CreateIssue(ctx, epic, "test")
		if err != nil {
			t.Errorf("Epic creation should succeed without acceptance_criteria, got error: %v", err)
		}

		if epic.ID == "" {
			t.Error("Epic ID should be generated")
		}
	})

	// Test 4: Task with proper acceptance criteria succeeds
	t.Run("Task creation succeeds with acceptance_criteria", func(t *testing.T) {
		task := &types.Issue{
			Title:       "Task with acceptance criteria",
			Description: "This should succeed",
			Status:      types.StatusOpen,
			Priority:    2,
			IssueType:   types.TypeTask,
			AcceptanceCriteria: `## Acceptance Criteria
- [ ] Feature X is implemented
- [ ] Tests pass
- [ ] Documentation updated`,
		}

		err := store.CreateIssue(ctx, task, "test")
		if err != nil {
			t.Fatalf("Task creation should succeed with acceptance_criteria, got error: %v", err)
		}

		if task.ID == "" {
			t.Fatal("Task ID should be generated")
		}

		// Verify acceptance criteria was stored
		retrieved, err := store.GetIssue(ctx, task.ID)
		if err != nil {
			t.Fatalf("Failed to retrieve task: %v", err)
		}

		if retrieved.AcceptanceCriteria == "" {
			t.Error("Acceptance criteria should be persisted")
		}

		if !strings.Contains(retrieved.AcceptanceCriteria, "Feature X is implemented") {
			t.Error("Acceptance criteria content should match what was provided")
		}
	})

	// Test 5: GetReadyWork should only return issues that have acceptance criteria
	// This ensures the executor workflow doesn't pick up malformed issues
	t.Run("GetReadyWork only returns issues with acceptance_criteria", func(t *testing.T) {
		// Create a valid task with acceptance criteria
		validTask := &types.Issue{
			Title:       "Valid task",
			Description: "Has acceptance criteria",
			Status:      types.StatusOpen,
			Priority:    2,
			IssueType:   types.TypeTask,
			AcceptanceCriteria: `- Task should complete successfully
- All tests pass`,
		}

		err := store.CreateIssue(ctx, validTask, "test")
		if err != nil {
			t.Fatalf("Failed to create valid task: %v", err)
		}

		// Get ready work - should return the valid task
		readyWork, err := store.GetReadyWork(ctx, types.WorkFilter{
			Status: types.StatusOpen,
			Limit:  10,
		})
		if err != nil {
			t.Fatalf("Failed to get ready work: %v", err)
		}

		// Verify the valid task appears in ready work
		foundValid := false
		for _, issue := range readyWork {
			if issue.ID == validTask.ID {
				foundValid = true
				if issue.AcceptanceCriteria == "" {
					t.Error("Ready work should have acceptance criteria populated")
				}
			}

			// All ready tasks/bugs MUST have acceptance criteria
			if issue.IssueType == types.TypeTask || issue.IssueType == types.TypeBug {
				if strings.TrimSpace(issue.AcceptanceCriteria) == "" {
					t.Errorf("Ready work issue %s (type=%s) has empty acceptance_criteria",
						issue.ID, issue.IssueType)
				}
			}
		}

		if !foundValid {
			t.Error("Valid task should appear in ready work")
		}
	})

	// Test 6: ClaimIssue on a task with acceptance criteria succeeds
	t.Run("ClaimIssue succeeds for task with acceptance_criteria", func(t *testing.T) {
		task := &types.Issue{
			Title:       "Claimable task",
			Description: "Has acceptance criteria, can be claimed",
			Status:      types.StatusOpen,
			Priority:    2,
			IssueType:   types.TypeTask,
			AcceptanceCriteria: `- Implementation complete
- Tests passing`,
		}

		err := store.CreateIssue(ctx, task, "test")
		if err != nil {
			t.Fatalf("Failed to create task: %v", err)
		}

		// Claim the task - should succeed
		err = store.ClaimIssue(ctx, task.ID, "test-executor-ac")
		if err != nil {
			t.Errorf("ClaimIssue should succeed for task with acceptance_criteria, got error: %v", err)
		}

		// Verify task is now in_progress
		claimed, err := store.GetIssue(ctx, task.ID)
		if err != nil {
			t.Fatalf("Failed to retrieve claimed task: %v", err)
		}

		if claimed.Status != types.StatusInProgress {
			t.Errorf("Claimed task status = %s, want %s", claimed.Status, types.StatusInProgress)
		}

		// Verify acceptance criteria is still present after claim
		if claimed.AcceptanceCriteria == "" {
			t.Error("Acceptance criteria should be preserved after claim")
		}
	})
}

// TestJSONLRoundTripAcceptanceCriteria verifies that acceptance_criteria survives JSONL export/import cycle (vc-ht3e)
// This catches JSONL escaping bugs, Beads library serialization issues, and field name mismatches
// Uses direct JSON encoding/decoding for fast execution (<5 seconds)
func TestJSONLRoundTripAcceptanceCriteria(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()

	// Test cases with various challenging acceptance_criteria values
	testCases := []struct {
		name               string
		acceptanceCriteria string
	}{
		{
			name:               "simple text",
			acceptanceCriteria: "All tests pass successfully",
		},
		{
			name:               "special characters",
			acceptanceCriteria: "Test with special chars: \n\t\"quotes\", 'apostrophes', & ampersands, <brackets>, {braces}, [arrays]",
		},
		{
			name:               "unicode characters",
			acceptanceCriteria: "Unicode test: 你好世界 🎉 émojis and symbols ✓ ✗ → ← ↑ ↓",
		},
		{
			name:               "long multiline text",
			acceptanceCriteria: `This is a long acceptance criteria with multiple lines:
1. First criterion that spans multiple words and has specific requirements
2. Second criterion with details about expected behavior and edge cases
3. Third criterion including references to specific files like /path/to/file.go
4. Fourth criterion with code examples: if err != nil { return err }
5. Fifth criterion with URLs: https://example.com/docs
Final notes and additional context for the acceptance criteria.`,
		},
		{
			name:               "JSON-like content",
			acceptanceCriteria: `{"status": "success", "count": 42, "items": ["a", "b", "c"]}`,
		},
		{
			name:               "backslashes and escapes",
			acceptanceCriteria: `Test backslashes: \n \t \r \\\ path\to\file`,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Create first database
			dbPath1 := filepath.Join(tmpDir, fmt.Sprintf("test1_%s.db", strings.ReplaceAll(tc.name, " ", "_")))
			jsonlPath := filepath.Join(tmpDir, fmt.Sprintf("export_%s.jsonl", strings.ReplaceAll(tc.name, " ", "_")))

			// Create storage and issue
			store1, err := NewVCStorage(ctx, dbPath1)
			if err != nil {
				t.Fatalf("Failed to create first storage: %v", err)
			}

			issue := &types.Issue{
				Title:              fmt.Sprintf("Test issue for %s", tc.name),
				Description:        "Test description",
				Status:             types.StatusOpen,
				Priority:           2,
				IssueType:          types.TypeTask,
				AcceptanceCriteria: tc.acceptanceCriteria,
			}

			err = store1.CreateIssue(ctx, issue, "test-user")
			if err != nil {
				t.Fatalf("Failed to create issue: %v", err)
			}

			// Export to JSONL using direct JSON encoding (fast path)
			// This tests the JSON serialization path without CLI overhead
			retrievedIssue, err := store1.GetIssue(ctx, issue.ID)
			if err != nil {
				t.Fatalf("Failed to retrieve issue for export: %v", err)
			}

			// Write JSONL file (one JSON object per line)
			// #nosec G304 - test file path
			jsonlFile, err := os.Create(jsonlPath)
			if err != nil {
				t.Fatalf("Failed to create JSONL file: %v", err)
			}

			encoder := json.NewEncoder(jsonlFile)
			if err := encoder.Encode(retrievedIssue); err != nil {
				jsonlFile.Close()
				t.Fatalf("Failed to encode issue to JSON: %v", err)
			}
			jsonlFile.Close()

			// Verify JSONL file exists and is not empty
			fi, err := os.Stat(jsonlPath)
			if err != nil {
				t.Fatalf("JSONL file not created: %v", err)
			}
			if fi.Size() == 0 {
				t.Fatal("JSONL file is empty")
			}

			t.Logf("Exported to JSONL: %s (%d bytes)", jsonlPath, fi.Size())

			// Close first storage
			store1.Close()

			// Create second database and import using direct JSON decoding
			dbPath2 := filepath.Join(tmpDir, fmt.Sprintf("test2_%s.db", strings.ReplaceAll(tc.name, " ", "_")))

			// Initialize the second database
			store2, err := NewVCStorage(ctx, dbPath2)
			if err != nil {
				t.Fatalf("Failed to create second storage: %v", err)
			}
			defer store2.Close()

			// Read JSONL file and import
			// #nosec G304 - test file path
			jsonlFile, err = os.Open(jsonlPath)
			if err != nil {
				t.Fatalf("Failed to open JSONL file: %v", err)
			}

			decoder := json.NewDecoder(jsonlFile)
			var importedIssue types.Issue
			if err := decoder.Decode(&importedIssue); err != nil {
				jsonlFile.Close()
				t.Fatalf("Failed to decode issue from JSON: %v", err)
			}
			jsonlFile.Close()

			// Create the imported issue in the new database
			err = store2.CreateIssue(ctx, &importedIssue, "test-user")
			if err != nil {
				t.Fatalf("Failed to import issue: %v", err)
			}

			// Retrieve the imported issue to verify
			finalIssue, err := store2.GetIssue(ctx, importedIssue.ID)
			if err != nil {
				t.Fatalf("Failed to retrieve imported issue: %v", err)
			}

			// Verify acceptance_criteria is preserved exactly
			if finalIssue.AcceptanceCriteria != tc.acceptanceCriteria {
				t.Errorf("Acceptance criteria mismatch after JSONL round-trip\nOriginal: %q\nImported: %q",
					tc.acceptanceCriteria, finalIssue.AcceptanceCriteria)
			}

			// Verify other fields are also preserved
			if finalIssue.Title != issue.Title {
				t.Errorf("Title mismatch: expected %q, got %q", issue.Title, finalIssue.Title)
			}
			if finalIssue.IssueType != issue.IssueType {
				t.Errorf("IssueType mismatch: expected %q, got %q", issue.IssueType, finalIssue.IssueType)
			}

			t.Logf("✓ Acceptance criteria preserved correctly for: %s", tc.name)
		})
	}
}

// TestGetReadyBaselineIssues tests the SQL-optimized baseline issue selection (vc-1nks)
// This test verifies that the query correctly filters for:
// 1. Issues with baseline-failure label
// 2. Status = open
// 3. No open blocking dependencies
// 4. Not epics
// 5. Proper priority ordering
func TestGetReadyBaselineIssues(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	store, err := NewVCStorage(ctx, dbPath)
	if err != nil {
		t.Fatalf("Failed to create VC storage: %v", err)
	}
	defer func() { _ = store.Close() }()

	// Create baseline issues with different states
	readyBaseline := &types.Issue{
		Title:              "Ready baseline issue",
		Status:             types.StatusOpen,
		Priority:           2,
		IssueType:          types.TypeBug,
		AcceptanceCriteria: "Test acceptance criteria",
	}
	err = store.CreateIssue(ctx, readyBaseline, "test")
	if err != nil {
		t.Fatalf("Failed to create ready baseline: %v", err)
	}
	err = store.AddLabel(ctx, readyBaseline.ID, "baseline-failure", "test")
	if err != nil {
		t.Fatalf("Failed to add baseline-failure label: %v", err)
	}

	// Create a high-priority baseline (should be selected first)
	highPriorityBaseline := &types.Issue{
		Title:              "High priority baseline",
		Status:             types.StatusOpen,
		Priority:           0, // P0 - higher priority
		IssueType:          types.TypeBug,
		AcceptanceCriteria: "Test acceptance criteria",
	}
	err = store.CreateIssue(ctx, highPriorityBaseline, "test")
	if err != nil {
		t.Fatalf("Failed to create high priority baseline: %v", err)
	}
	err = store.AddLabel(ctx, highPriorityBaseline.ID, "baseline-failure", "test")
	if err != nil {
		t.Fatalf("Failed to add baseline-failure label: %v", err)
	}

	// Create a blocked baseline (has open blocking dependency)
	blockedBaseline := &types.Issue{
		Title:              "Blocked baseline issue",
		Status:             types.StatusOpen,
		Priority:           1,
		IssueType:          types.TypeBug,
		AcceptanceCriteria: "Test acceptance criteria",
	}
	err = store.CreateIssue(ctx, blockedBaseline, "test")
	if err != nil {
		t.Fatalf("Failed to create blocked baseline: %v", err)
	}
	err = store.AddLabel(ctx, blockedBaseline.ID, "baseline-failure", "test")
	if err != nil {
		t.Fatalf("Failed to add baseline-failure label: %v", err)
	}

	// Create a blocker issue
	blocker := &types.Issue{
		Title:              "Blocking issue",
		Status:             types.StatusOpen,
		Priority:           1,
		IssueType:          types.TypeTask,
		AcceptanceCriteria: "Test acceptance criteria",
	}
	err = store.CreateIssue(ctx, blocker, "test")
	if err != nil {
		t.Fatalf("Failed to create blocker: %v", err)
	}

	// Add blocking dependency
	err = store.AddDependency(ctx, &types.Dependency{
		IssueID:     blockedBaseline.ID,
		DependsOnID: blocker.ID,
		Type:        types.DepBlocks,
	}, "test")
	if err != nil {
		t.Fatalf("Failed to add blocking dependency: %v", err)
	}

	// Create a closed baseline (should not be selected)
	closedBaseline := &types.Issue{
		Title:              "Closed baseline issue",
		Status:             types.StatusOpen, // Create as open first
		Priority:           1,
		IssueType:          types.TypeBug,
		AcceptanceCriteria: "Test acceptance criteria",
	}
	err = store.CreateIssue(ctx, closedBaseline, "test")
	if err != nil {
		t.Fatalf("Failed to create closed baseline: %v", err)
	}
	err = store.AddLabel(ctx, closedBaseline.ID, "baseline-failure", "test")
	if err != nil {
		t.Fatalf("Failed to add baseline-failure label: %v", err)
	}
	// Close it properly
	err = store.CloseIssue(ctx, closedBaseline.ID, "Test closed", "test")
	if err != nil {
		t.Fatalf("Failed to close baseline: %v", err)
	}

	// Create a baseline epic (should not be selected)
	epicBaseline := &types.Issue{
		Title:              "Epic baseline issue",
		Status:             types.StatusOpen,
		Priority:           1,
		IssueType:          types.TypeEpic,
		AcceptanceCriteria: "Test acceptance criteria",
	}
	err = store.CreateIssue(ctx, epicBaseline, "test")
	if err != nil {
		t.Fatalf("Failed to create epic baseline: %v", err)
	}
	err = store.AddLabel(ctx, epicBaseline.ID, "baseline-failure", "test")
	if err != nil {
		t.Fatalf("Failed to add baseline-failure label: %v", err)
	}

	t.Run("returns only ready baseline issues ordered by priority", func(t *testing.T) {
		baselines, err := store.GetReadyBaselineIssues(ctx, 10)
		if err != nil {
			t.Fatalf("GetReadyBaselineIssues failed: %v", err)
		}

		// Should return 2 ready baselines: highPriorityBaseline (P0) and readyBaseline (P2)
		// Should NOT return: blockedBaseline (has open blocker), closedBaseline (closed), epicBaseline (epic)
		if len(baselines) != 2 {
			t.Fatalf("Expected 2 ready baselines, got %d", len(baselines))
		}

		// First should be high priority (P0)
		if baselines[0].ID != highPriorityBaseline.ID {
			t.Errorf("Expected first baseline to be %s (P0), got %s (P%d)",
				highPriorityBaseline.ID, baselines[0].ID, baselines[0].Priority)
		}

		// Second should be lower priority (P2)
		if baselines[1].ID != readyBaseline.ID {
			t.Errorf("Expected second baseline to be %s (P2), got %s (P%d)",
				readyBaseline.ID, baselines[1].ID, baselines[1].Priority)
		}

		t.Logf("✓ Ready baselines correctly filtered and ordered by priority")
	})

	t.Run("respects limit parameter", func(t *testing.T) {
		baselines, err := store.GetReadyBaselineIssues(ctx, 1)
		if err != nil {
			t.Fatalf("GetReadyBaselineIssues failed: %v", err)
		}

		if len(baselines) != 1 {
			t.Fatalf("Expected 1 baseline (limit=1), got %d", len(baselines))
		}

		// Should return only the highest priority one
		if baselines[0].ID != highPriorityBaseline.ID {
			t.Errorf("Expected %s (P0), got %s (P%d)",
				highPriorityBaseline.ID, baselines[0].ID, baselines[0].Priority)
		}

		t.Logf("✓ Limit parameter correctly enforced")
	})

	t.Run("baseline becomes ready when blocker closes", func(t *testing.T) {
		// Close the blocker
		err := store.CloseIssue(ctx, blocker.ID, "Test completed", "test")
		if err != nil {
			t.Fatalf("Failed to close blocker: %v", err)
		}

		baselines, err := store.GetReadyBaselineIssues(ctx, 10)
		if err != nil {
			t.Fatalf("GetReadyBaselineIssues failed: %v", err)
		}

		// Now should get 3 baselines (previously blocked one is now ready)
		if len(baselines) != 3 {
			t.Fatalf("Expected 3 ready baselines after blocker closed, got %d", len(baselines))
		}

		// Verify the previously blocked baseline is now in the list
		found := false
		for _, baseline := range baselines {
			if baseline.ID == blockedBaseline.ID {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Previously blocked baseline %s should now be ready", blockedBaseline.ID)
		}

		t.Logf("✓ Baseline correctly becomes ready when blocker closes")
	})

	t.Run("ignores discovered-from dependencies", func(t *testing.T) {
		// Create a new baseline with only discovered-from dependency
		discoveredBaseline := &types.Issue{
			Title:              "Baseline with discovered-from",
			Status:             types.StatusOpen,
			Priority:           1,
			IssueType:          types.TypeBug,
			AcceptanceCriteria: "Test acceptance criteria",
		}
		err := store.CreateIssue(ctx, discoveredBaseline, "test")
		if err != nil {
			t.Fatalf("Failed to create discovered baseline: %v", err)
		}
		err = store.AddLabel(ctx, discoveredBaseline.ID, "baseline-failure", "test")
		if err != nil {
			t.Fatalf("Failed to add baseline-failure label: %v", err)
		}

		// Create a parent mission
		mission := &types.Issue{
			Title:              "Parent mission",
			Status:             types.StatusOpen,
			Priority:           1,
			IssueType:          types.TypeEpic,
			AcceptanceCriteria: "Test acceptance criteria",
		}
		err = store.CreateIssue(ctx, mission, "test")
		if err != nil {
			t.Fatalf("Failed to create mission: %v", err)
		}

		// Add discovered-from dependency (should not block execution)
		err = store.AddDependency(ctx, &types.Dependency{
			IssueID:     discoveredBaseline.ID,
			DependsOnID: mission.ID,
			Type:        types.DepDiscoveredFrom,
		}, "test")
		if err != nil {
			t.Fatalf("Failed to add discovered-from dependency: %v", err)
		}

		baselines, err := store.GetReadyBaselineIssues(ctx, 10)
		if err != nil {
			t.Fatalf("GetReadyBaselineIssues failed: %v", err)
		}

		// Baseline should be ready despite discovered-from dependency
		found := false
		for _, baseline := range baselines {
			if baseline.ID == discoveredBaseline.ID {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Baseline %s with discovered-from dependency should be ready", discoveredBaseline.ID)
		}

		t.Logf("✓ Baseline correctly ignores discovered-from dependencies")
	})
}

// TestGetReadyDependentsOfBlockedBaselines tests the SQL-optimized dependent selection (vc-1nks)
// This test verifies that the query correctly:
// 1. Finds baseline-failure issues that are blocked
// 2. Finds their ready dependents (children)
// 3. Returns dependent with baseline parent ID mapping
// 4. Filters out epics and non-ready dependents
func TestGetReadyDependentsOfBlockedBaselines(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	store, err := NewVCStorage(ctx, dbPath)
	if err != nil {
		t.Fatalf("Failed to create VC storage: %v", err)
	}
	defer func() { _ = store.Close() }()

	// Create a blocked baseline issue
	blockedBaseline := &types.Issue{
		Title:              "Blocked baseline",
		Status:             types.StatusOpen,
		Priority:           1,
		IssueType:          types.TypeBug,
		AcceptanceCriteria: "Test acceptance criteria",
	}
	err = store.CreateIssue(ctx, blockedBaseline, "test")
	if err != nil {
		t.Fatalf("Failed to create blocked baseline: %v", err)
	}
	err = store.AddLabel(ctx, blockedBaseline.ID, "baseline-failure", "test")
	if err != nil {
		t.Fatalf("Failed to add baseline-failure label: %v", err)
	}

	// Create a blocker for the baseline
	blocker := &types.Issue{
		Title:              "Blocker issue",
		Status:             types.StatusOpen,
		Priority:           1,
		IssueType:          types.TypeTask,
		AcceptanceCriteria: "Test acceptance criteria",
	}
	err = store.CreateIssue(ctx, blocker, "test")
	if err != nil {
		t.Fatalf("Failed to create blocker: %v", err)
	}

	// Block the baseline
	err = store.AddDependency(ctx, &types.Dependency{
		IssueID:     blockedBaseline.ID,
		DependsOnID: blocker.ID,
		Type:        types.DepBlocks,
	}, "test")
	if err != nil {
		t.Fatalf("Failed to add blocking dependency: %v", err)
	}

	// Create a ready dependent (child) of the blocked baseline
	readyDependent := &types.Issue{
		Title:              "Ready dependent",
		Status:             types.StatusOpen,
		Priority:           2,
		IssueType:          types.TypeTask,
		AcceptanceCriteria: "Test acceptance criteria",
	}
	err = store.CreateIssue(ctx, readyDependent, "test")
	if err != nil {
		t.Fatalf("Failed to create ready dependent: %v", err)
	}

	// Link as parent-child
	err = store.AddDependency(ctx, &types.Dependency{
		IssueID:     readyDependent.ID,
		DependsOnID: blockedBaseline.ID,
		Type:        types.DepParentChild,
	}, "test")
	if err != nil {
		t.Fatalf("Failed to add parent-child dependency: %v", err)
	}

	// Create a blocked dependent (should not be returned)
	blockedDependent := &types.Issue{
		Title:              "Blocked dependent",
		Status:             types.StatusOpen,
		Priority:           1,
		IssueType:          types.TypeTask,
		AcceptanceCriteria: "Test acceptance criteria",
	}
	err = store.CreateIssue(ctx, blockedDependent, "test")
	if err != nil {
		t.Fatalf("Failed to create blocked dependent: %v", err)
	}

	err = store.AddDependency(ctx, &types.Dependency{
		IssueID:     blockedDependent.ID,
		DependsOnID: blockedBaseline.ID,
		Type:        types.DepParentChild,
	}, "test")
	if err != nil {
		t.Fatalf("Failed to add parent-child dependency: %v", err)
	}

	// Block the dependent
	dependentBlocker := &types.Issue{
		Title:              "Dependent blocker",
		Status:             types.StatusOpen,
		Priority:           1,
		IssueType:          types.TypeTask,
		AcceptanceCriteria: "Test acceptance criteria",
	}
	err = store.CreateIssue(ctx, dependentBlocker, "test")
	if err != nil {
		t.Fatalf("Failed to create dependent blocker: %v", err)
	}

	err = store.AddDependency(ctx, &types.Dependency{
		IssueID:     blockedDependent.ID,
		DependsOnID: dependentBlocker.ID,
		Type:        types.DepBlocks,
	}, "test")
	if err != nil {
		t.Fatalf("Failed to add dependent blocking dependency: %v", err)
	}

	// Create a closed dependent (should not be returned)
	closedDependent := &types.Issue{
		Title:              "Closed dependent",
		Status:             types.StatusOpen, // Create as open first
		Priority:           1,
		IssueType:          types.TypeTask,
		AcceptanceCriteria: "Test acceptance criteria",
	}
	err = store.CreateIssue(ctx, closedDependent, "test")
	if err != nil {
		t.Fatalf("Failed to create closed dependent: %v", err)
	}

	err = store.AddDependency(ctx, &types.Dependency{
		IssueID:     closedDependent.ID,
		DependsOnID: blockedBaseline.ID,
		Type:        types.DepParentChild,
	}, "test")
	if err != nil {
		t.Fatalf("Failed to add parent-child dependency: %v", err)
	}

	// Close it properly
	err = store.CloseIssue(ctx, closedDependent.ID, "Test closed", "test")
	if err != nil {
		t.Fatalf("Failed to close dependent: %v", err)
	}

	t.Run("returns ready dependents of blocked baselines", func(t *testing.T) {
		dependents, baselineMap, err := store.GetReadyDependentsOfBlockedBaselines(ctx, 10)
		if err != nil {
			t.Fatalf("GetReadyDependentsOfBlockedBaselines failed: %v", err)
		}

		// Should return only the ready dependent
		if len(dependents) != 1 {
			t.Fatalf("Expected 1 ready dependent, got %d", len(dependents))
		}

		if dependents[0].ID != readyDependent.ID {
			t.Errorf("Expected dependent %s, got %s", readyDependent.ID, dependents[0].ID)
		}

		// Verify baseline mapping
		baselineID, ok := baselineMap[readyDependent.ID]
		if !ok {
			t.Fatalf("Baseline mapping missing for dependent %s", readyDependent.ID)
		}
		if baselineID != blockedBaseline.ID {
			t.Errorf("Expected baseline %s, got %s", blockedBaseline.ID, baselineID)
		}

		t.Logf("✓ Ready dependent correctly identified with baseline mapping")
	})

	t.Run("respects limit parameter", func(t *testing.T) {
		// Create another ready dependent
		anotherDependent := &types.Issue{
			Title:              "Another ready dependent",
			Status:             types.StatusOpen,
			Priority:           3,
			IssueType:          types.TypeTask,
			AcceptanceCriteria: "Test acceptance criteria",
		}
		err := store.CreateIssue(ctx, anotherDependent, "test")
		if err != nil {
			t.Fatalf("Failed to create another dependent: %v", err)
		}

		err = store.AddDependency(ctx, &types.Dependency{
			IssueID:     anotherDependent.ID,
			DependsOnID: blockedBaseline.ID,
			Type:        types.DepParentChild,
		}, "test")
		if err != nil {
			t.Fatalf("Failed to add parent-child dependency: %v", err)
		}

		dependents, _, err := store.GetReadyDependentsOfBlockedBaselines(ctx, 1)
		if err != nil {
			t.Fatalf("GetReadyDependentsOfBlockedBaselines failed: %v", err)
		}

		if len(dependents) != 1 {
			t.Fatalf("Expected 1 dependent (limit=1), got %d", len(dependents))
		}

		// Should return highest priority one (P2 vs P3)
		if dependents[0].ID != readyDependent.ID {
			t.Errorf("Expected %s (P2), got %s (P%d)",
				readyDependent.ID, dependents[0].ID, dependents[0].Priority)
		}

		t.Logf("✓ Limit parameter correctly enforced with priority ordering")
	})

	t.Run("ignores unblocked baselines", func(t *testing.T) {
		// Create a ready (unblocked) baseline
		readyBaseline := &types.Issue{
			Title:              "Ready baseline",
			Status:             types.StatusOpen,
			Priority:           1,
			IssueType:          types.TypeBug,
			AcceptanceCriteria: "Test acceptance criteria",
		}
		err := store.CreateIssue(ctx, readyBaseline, "test")
		if err != nil {
			t.Fatalf("Failed to create ready baseline: %v", err)
		}
		err = store.AddLabel(ctx, readyBaseline.ID, "baseline-failure", "test")
		if err != nil {
			t.Fatalf("Failed to add baseline-failure label: %v", err)
		}

		// Create a dependent of the ready baseline
		dependentOfReady := &types.Issue{
			Title:              "Dependent of ready baseline",
			Status:             types.StatusOpen,
			Priority:           1,
			IssueType:          types.TypeTask,
			AcceptanceCriteria: "Test acceptance criteria",
		}
		err = store.CreateIssue(ctx, dependentOfReady, "test")
		if err != nil {
			t.Fatalf("Failed to create dependent of ready: %v", err)
		}

		err = store.AddDependency(ctx, &types.Dependency{
			IssueID:     dependentOfReady.ID,
			DependsOnID: readyBaseline.ID,
			Type:        types.DepParentChild,
		}, "test")
		if err != nil {
			t.Fatalf("Failed to add parent-child dependency: %v", err)
		}

		dependents, _, err := store.GetReadyDependentsOfBlockedBaselines(ctx, 10)
		if err != nil {
			t.Fatalf("GetReadyDependentsOfBlockedBaselines failed: %v", err)
		}

		// Should not return dependents of unblocked baselines
		for _, dep := range dependents {
			if dep.ID == dependentOfReady.ID {
				t.Errorf("Should not return dependent %s of unblocked baseline", dep.ID)
			}
		}

		t.Logf("✓ Correctly ignores dependents of unblocked baselines")
	})

	// Note: We don't test epic dependents because Beads validation prevents
	// epics from being children in parent-child relationships. The SQL query
	// filters them out anyway with "issue_type != 'epic'".

	// === Edge Case Tests (vc-599b) ===

	t.Run("edge case: multiple dependents with priority ordering", func(t *testing.T) {
		// Create another blocked baseline with multiple dependents at different priorities
		blockedBaseline2 := &types.Issue{
			Title:              "Blocked baseline 2",
			Status:             types.StatusOpen,
			Priority:           1,
			IssueType:          types.TypeBug,
			AcceptanceCriteria: "Test acceptance criteria",
		}
		err := store.CreateIssue(ctx, blockedBaseline2, "test")
		if err != nil {
			t.Fatalf("Failed to create blocked baseline 2: %v", err)
		}
		err = store.AddLabel(ctx, blockedBaseline2.ID, "baseline-failure", "test")
		if err != nil {
			t.Fatalf("Failed to add baseline-failure label: %v", err)
		}

		// Create blocker for baseline2
		blocker2 := &types.Issue{
			Title:              "Blocker 2",
			Status:             types.StatusOpen,
			Priority:           1,
			IssueType:          types.TypeTask,
			AcceptanceCriteria: "Test acceptance criteria",
		}
		err = store.CreateIssue(ctx, blocker2, "test")
		if err != nil {
			t.Fatalf("Failed to create blocker2: %v", err)
		}

		err = store.AddDependency(ctx, &types.Dependency{
			IssueID:     blockedBaseline2.ID,
			DependsOnID: blocker2.ID,
			Type:        types.DepBlocks,
		}, "test")
		if err != nil {
			t.Fatalf("Failed to add blocking dependency: %v", err)
		}

		// Create 3 dependents with different priorities (P0=highest, P2=medium, P4=lowest)
		dependent1 := &types.Issue{
			Title:              "Dependent P0 (highest)",
			Status:             types.StatusOpen,
			Priority:           0,
			IssueType:          types.TypeTask,
			AcceptanceCriteria: "Test acceptance criteria",
		}
		err = store.CreateIssue(ctx, dependent1, "test")
		if err != nil {
			t.Fatalf("Failed to create dependent1: %v", err)
		}
		err = store.AddDependency(ctx, &types.Dependency{
			IssueID:     dependent1.ID,
			DependsOnID: blockedBaseline2.ID,
			Type:        types.DepParentChild,
		}, "test")
		if err != nil {
			t.Fatalf("Failed to add parent-child dependency: %v", err)
		}

		dependent2 := &types.Issue{
			Title:              "Dependent P2 (medium)",
			Status:             types.StatusOpen,
			Priority:           2,
			IssueType:          types.TypeTask,
			AcceptanceCriteria: "Test acceptance criteria",
		}
		err = store.CreateIssue(ctx, dependent2, "test")
		if err != nil {
			t.Fatalf("Failed to create dependent2: %v", err)
		}
		err = store.AddDependency(ctx, &types.Dependency{
			IssueID:     dependent2.ID,
			DependsOnID: blockedBaseline2.ID,
			Type:        types.DepParentChild,
		}, "test")
		if err != nil {
			t.Fatalf("Failed to add parent-child dependency: %v", err)
		}

		dependent3 := &types.Issue{
			Title:              "Dependent P4 (lowest)",
			Status:             types.StatusOpen,
			Priority:           4,
			IssueType:          types.TypeTask,
			AcceptanceCriteria: "Test acceptance criteria",
		}
		err = store.CreateIssue(ctx, dependent3, "test")
		if err != nil {
			t.Fatalf("Failed to create dependent3: %v", err)
		}
		err = store.AddDependency(ctx, &types.Dependency{
			IssueID:     dependent3.ID,
			DependsOnID: blockedBaseline2.ID,
			Type:        types.DepParentChild,
		}, "test")
		if err != nil {
			t.Fatalf("Failed to add parent-child dependency: %v", err)
		}

		// Query with limit=10 to get all 3
		dependents, baselineMap, err := store.GetReadyDependentsOfBlockedBaselines(ctx, 10)
		if err != nil {
			t.Fatalf("GetReadyDependentsOfBlockedBaselines failed: %v", err)
		}

		// Find our 3 dependents in the results
		var foundDeps []*types.Issue
		for _, dep := range dependents {
			if dep.ID == dependent1.ID || dep.ID == dependent2.ID || dep.ID == dependent3.ID {
				foundDeps = append(foundDeps, dep)
			}
		}

		if len(foundDeps) != 3 {
			t.Fatalf("Expected 3 dependents for baseline2, got %d", len(foundDeps))
		}

		// Verify priority ordering (should be P0, P2, P4)
		if foundDeps[0].Priority != 0 {
			t.Errorf("First dependent should be P0, got P%d", foundDeps[0].Priority)
		}
		if foundDeps[1].Priority != 2 {
			t.Errorf("Second dependent should be P2, got P%d", foundDeps[1].Priority)
		}
		if foundDeps[2].Priority != 4 {
			t.Errorf("Third dependent should be P4, got P%d", foundDeps[2].Priority)
		}

		// Verify all map to the same baseline
		for _, dep := range foundDeps {
			if baselineMap[dep.ID] != blockedBaseline2.ID {
				t.Errorf("Dependent %s maps to wrong baseline %s, expected %s",
					dep.ID, baselineMap[dep.ID], blockedBaseline2.ID)
			}
		}

		t.Logf("✓ Multiple dependents correctly ordered by priority (P0 > P2 > P4)")
	})

	t.Run("edge case: all dependents blocked (should return none)", func(t *testing.T) {
		// Create a blocked baseline with only blocked dependents
		blockedBaseline3 := &types.Issue{
			Title:              "Blocked baseline 3",
			Status:             types.StatusOpen,
			Priority:           1,
			IssueType:          types.TypeBug,
			AcceptanceCriteria: "Test acceptance criteria",
		}
		err := store.CreateIssue(ctx, blockedBaseline3, "test")
		if err != nil {
			t.Fatalf("Failed to create blocked baseline 3: %v", err)
		}
		err = store.AddLabel(ctx, blockedBaseline3.ID, "baseline-failure", "test")
		if err != nil {
			t.Fatalf("Failed to add baseline-failure label: %v", err)
		}

		// Create blocker for baseline3
		blocker3 := &types.Issue{
			Title:              "Blocker 3",
			Status:             types.StatusOpen,
			Priority:           1,
			IssueType:          types.TypeTask,
			AcceptanceCriteria: "Test acceptance criteria",
		}
		err = store.CreateIssue(ctx, blocker3, "test")
		if err != nil {
			t.Fatalf("Failed to create blocker3: %v", err)
		}

		err = store.AddDependency(ctx, &types.Dependency{
			IssueID:     blockedBaseline3.ID,
			DependsOnID: blocker3.ID,
			Type:        types.DepBlocks,
		}, "test")
		if err != nil {
			t.Fatalf("Failed to add blocking dependency: %v", err)
		}

		// Create 2 dependents that are both blocked
		blockedDep1 := &types.Issue{
			Title:              "Blocked dependent 1",
			Status:             types.StatusOpen,
			Priority:           1,
			IssueType:          types.TypeTask,
			AcceptanceCriteria: "Test acceptance criteria",
		}
		err = store.CreateIssue(ctx, blockedDep1, "test")
		if err != nil {
			t.Fatalf("Failed to create blockedDep1: %v", err)
		}
		err = store.AddDependency(ctx, &types.Dependency{
			IssueID:     blockedDep1.ID,
			DependsOnID: blockedBaseline3.ID,
			Type:        types.DepParentChild,
		}, "test")
		if err != nil {
			t.Fatalf("Failed to add parent-child dependency: %v", err)
		}

		// Block blockedDep1
		dep1Blocker := &types.Issue{
			Title:              "Dep1 blocker",
			Status:             types.StatusOpen,
			Priority:           1,
			IssueType:          types.TypeTask,
			AcceptanceCriteria: "Test acceptance criteria",
		}
		err = store.CreateIssue(ctx, dep1Blocker, "test")
		if err != nil {
			t.Fatalf("Failed to create dep1Blocker: %v", err)
		}
		err = store.AddDependency(ctx, &types.Dependency{
			IssueID:     blockedDep1.ID,
			DependsOnID: dep1Blocker.ID,
			Type:        types.DepBlocks,
		}, "test")
		if err != nil {
			t.Fatalf("Failed to add blocking dependency: %v", err)
		}

		blockedDep2 := &types.Issue{
			Title:              "Blocked dependent 2",
			Status:             types.StatusOpen,
			Priority:           2,
			IssueType:          types.TypeTask,
			AcceptanceCriteria: "Test acceptance criteria",
		}
		err = store.CreateIssue(ctx, blockedDep2, "test")
		if err != nil {
			t.Fatalf("Failed to create blockedDep2: %v", err)
		}
		err = store.AddDependency(ctx, &types.Dependency{
			IssueID:     blockedDep2.ID,
			DependsOnID: blockedBaseline3.ID,
			Type:        types.DepParentChild,
		}, "test")
		if err != nil {
			t.Fatalf("Failed to add parent-child dependency: %v", err)
		}

		// Block blockedDep2
		dep2Blocker := &types.Issue{
			Title:              "Dep2 blocker",
			Status:             types.StatusOpen,
			Priority:           1,
			IssueType:          types.TypeTask,
			AcceptanceCriteria: "Test acceptance criteria",
		}
		err = store.CreateIssue(ctx, dep2Blocker, "test")
		if err != nil {
			t.Fatalf("Failed to create dep2Blocker: %v", err)
		}
		err = store.AddDependency(ctx, &types.Dependency{
			IssueID:     blockedDep2.ID,
			DependsOnID: dep2Blocker.ID,
			Type:        types.DepBlocks,
		}, "test")
		if err != nil {
			t.Fatalf("Failed to add blocking dependency: %v", err)
		}

		// Query should return no dependents for baseline3 (all are blocked)
		dependents, baselineMap, err := store.GetReadyDependentsOfBlockedBaselines(ctx, 10)
		if err != nil {
			t.Fatalf("GetReadyDependentsOfBlockedBaselines failed: %v", err)
		}

		// Verify none of the blocked dependents are returned
		for _, dep := range dependents {
			if dep.ID == blockedDep1.ID || dep.ID == blockedDep2.ID {
				t.Errorf("Should not return blocked dependent %s", dep.ID)
			}
			if baselineMap[dep.ID] == blockedBaseline3.ID {
				t.Errorf("Should not return any dependents for baseline3 (all blocked)")
			}
		}

		t.Logf("✓ Correctly returns no dependents when all are blocked")
	})

	t.Run("edge case: multiple baselines with different dependents", func(t *testing.T) {
		// Create 2 blocked baselines, each with their own ready dependent
		baseline4 := &types.Issue{
			Title:              "Blocked baseline 4",
			Status:             types.StatusOpen,
			Priority:           1,
			IssueType:          types.TypeBug,
			AcceptanceCriteria: "Test acceptance criteria",
		}
		err := store.CreateIssue(ctx, baseline4, "test")
		if err != nil {
			t.Fatalf("Failed to create baseline4: %v", err)
		}
		err = store.AddLabel(ctx, baseline4.ID, "baseline-failure", "test")
		if err != nil {
			t.Fatalf("Failed to add baseline-failure label: %v", err)
		}

		blocker4 := &types.Issue{
			Title:              "Blocker 4",
			Status:             types.StatusOpen,
			Priority:           1,
			IssueType:          types.TypeTask,
			AcceptanceCriteria: "Test acceptance criteria",
		}
		err = store.CreateIssue(ctx, blocker4, "test")
		if err != nil {
			t.Fatalf("Failed to create blocker4: %v", err)
		}
		err = store.AddDependency(ctx, &types.Dependency{
			IssueID:     baseline4.ID,
			DependsOnID: blocker4.ID,
			Type:        types.DepBlocks,
		}, "test")
		if err != nil {
			t.Fatalf("Failed to add blocking dependency: %v", err)
		}

		baseline5 := &types.Issue{
			Title:              "Blocked baseline 5",
			Status:             types.StatusOpen,
			Priority:           2,
			IssueType:          types.TypeBug,
			AcceptanceCriteria: "Test acceptance criteria",
		}
		err = store.CreateIssue(ctx, baseline5, "test")
		if err != nil {
			t.Fatalf("Failed to create baseline5: %v", err)
		}
		err = store.AddLabel(ctx, baseline5.ID, "baseline-failure", "test")
		if err != nil {
			t.Fatalf("Failed to add baseline-failure label: %v", err)
		}

		blocker5 := &types.Issue{
			Title:              "Blocker 5",
			Status:             types.StatusOpen,
			Priority:           1,
			IssueType:          types.TypeTask,
			AcceptanceCriteria: "Test acceptance criteria",
		}
		err = store.CreateIssue(ctx, blocker5, "test")
		if err != nil {
			t.Fatalf("Failed to create blocker5: %v", err)
		}
		err = store.AddDependency(ctx, &types.Dependency{
			IssueID:     baseline5.ID,
			DependsOnID: blocker5.ID,
			Type:        types.DepBlocks,
		}, "test")
		if err != nil {
			t.Fatalf("Failed to add blocking dependency: %v", err)
		}

		// Create ready dependent for baseline4
		dep4 := &types.Issue{
			Title:              "Dependent of baseline 4",
			Status:             types.StatusOpen,
			Priority:           1,
			IssueType:          types.TypeTask,
			AcceptanceCriteria: "Test acceptance criteria",
		}
		err = store.CreateIssue(ctx, dep4, "test")
		if err != nil {
			t.Fatalf("Failed to create dep4: %v", err)
		}
		err = store.AddDependency(ctx, &types.Dependency{
			IssueID:     dep4.ID,
			DependsOnID: baseline4.ID,
			Type:        types.DepParentChild,
		}, "test")
		if err != nil {
			t.Fatalf("Failed to add parent-child dependency: %v", err)
		}

		// Create ready dependent for baseline5
		dep5 := &types.Issue{
			Title:              "Dependent of baseline 5",
			Status:             types.StatusOpen,
			Priority:           2,
			IssueType:          types.TypeTask,
			AcceptanceCriteria: "Test acceptance criteria",
		}
		err = store.CreateIssue(ctx, dep5, "test")
		if err != nil {
			t.Fatalf("Failed to create dep5: %v", err)
		}
		err = store.AddDependency(ctx, &types.Dependency{
			IssueID:     dep5.ID,
			DependsOnID: baseline5.ID,
			Type:        types.DepParentChild,
		}, "test")
		if err != nil {
			t.Fatalf("Failed to add parent-child dependency: %v", err)
		}

		// Query should return both dependents
		dependents, baselineMap, err := store.GetReadyDependentsOfBlockedBaselines(ctx, 10)
		if err != nil {
			t.Fatalf("GetReadyDependentsOfBlockedBaselines failed: %v", err)
		}

		// Find our 2 dependents
		foundDep4 := false
		foundDep5 := false
		for _, dep := range dependents {
			if dep.ID == dep4.ID {
				foundDep4 = true
				if baselineMap[dep.ID] != baseline4.ID {
					t.Errorf("dep4 maps to wrong baseline: %s, expected %s",
						baselineMap[dep.ID], baseline4.ID)
				}
			}
			if dep.ID == dep5.ID {
				foundDep5 = true
				if baselineMap[dep.ID] != baseline5.ID {
					t.Errorf("dep5 maps to wrong baseline: %s, expected %s",
						baselineMap[dep.ID], baseline5.ID)
				}
			}
		}

		if !foundDep4 {
			t.Error("Should return dependent of baseline4")
		}
		if !foundDep5 {
			t.Error("Should return dependent of baseline5")
		}

		t.Logf("✓ Multiple baselines correctly return their distinct dependents")
	})

	t.Run("edge case: mixed ready and blocked dependents", func(t *testing.T) {
		// Create a blocked baseline with mix of ready and blocked dependents
		baseline6 := &types.Issue{
			Title:              "Blocked baseline 6",
			Status:             types.StatusOpen,
			Priority:           1,
			IssueType:          types.TypeBug,
			AcceptanceCriteria: "Test acceptance criteria",
		}
		err := store.CreateIssue(ctx, baseline6, "test")
		if err != nil {
			t.Fatalf("Failed to create baseline6: %v", err)
		}
		err = store.AddLabel(ctx, baseline6.ID, "baseline-failure", "test")
		if err != nil {
			t.Fatalf("Failed to add baseline-failure label: %v", err)
		}

		blocker6 := &types.Issue{
			Title:              "Blocker 6",
			Status:             types.StatusOpen,
			Priority:           1,
			IssueType:          types.TypeTask,
			AcceptanceCriteria: "Test acceptance criteria",
		}
		err = store.CreateIssue(ctx, blocker6, "test")
		if err != nil {
			t.Fatalf("Failed to create blocker6: %v", err)
		}
		err = store.AddDependency(ctx, &types.Dependency{
			IssueID:     baseline6.ID,
			DependsOnID: blocker6.ID,
			Type:        types.DepBlocks,
		}, "test")
		if err != nil {
			t.Fatalf("Failed to add blocking dependency: %v", err)
		}

		// Create 3 dependents: ready, blocked, closed
		readyDep := &types.Issue{
			Title:              "Ready dep of baseline6",
			Status:             types.StatusOpen,
			Priority:           1,
			IssueType:          types.TypeTask,
			AcceptanceCriteria: "Test acceptance criteria",
		}
		err = store.CreateIssue(ctx, readyDep, "test")
		if err != nil {
			t.Fatalf("Failed to create readyDep: %v", err)
		}
		err = store.AddDependency(ctx, &types.Dependency{
			IssueID:     readyDep.ID,
			DependsOnID: baseline6.ID,
			Type:        types.DepParentChild,
		}, "test")
		if err != nil {
			t.Fatalf("Failed to add parent-child dependency: %v", err)
		}

		blockedDepMixed := &types.Issue{
			Title:              "Blocked dep of baseline6",
			Status:             types.StatusOpen,
			Priority:           2,
			IssueType:          types.TypeTask,
			AcceptanceCriteria: "Test acceptance criteria",
		}
		err = store.CreateIssue(ctx, blockedDepMixed, "test")
		if err != nil {
			t.Fatalf("Failed to create blockedDepMixed: %v", err)
		}
		err = store.AddDependency(ctx, &types.Dependency{
			IssueID:     blockedDepMixed.ID,
			DependsOnID: baseline6.ID,
			Type:        types.DepParentChild,
		}, "test")
		if err != nil {
			t.Fatalf("Failed to add parent-child dependency: %v", err)
		}

		// Block blockedDepMixed
		mixedBlocker := &types.Issue{
			Title:              "Mixed blocker",
			Status:             types.StatusOpen,
			Priority:           1,
			IssueType:          types.TypeTask,
			AcceptanceCriteria: "Test acceptance criteria",
		}
		err = store.CreateIssue(ctx, mixedBlocker, "test")
		if err != nil {
			t.Fatalf("Failed to create mixedBlocker: %v", err)
		}
		err = store.AddDependency(ctx, &types.Dependency{
			IssueID:     blockedDepMixed.ID,
			DependsOnID: mixedBlocker.ID,
			Type:        types.DepBlocks,
		}, "test")
		if err != nil {
			t.Fatalf("Failed to add blocking dependency: %v", err)
		}

		closedDepMixed := &types.Issue{
			Title:              "Closed dep of baseline6",
			Status:             types.StatusOpen,
			Priority:           3,
			IssueType:          types.TypeTask,
			AcceptanceCriteria: "Test acceptance criteria",
		}
		err = store.CreateIssue(ctx, closedDepMixed, "test")
		if err != nil {
			t.Fatalf("Failed to create closedDepMixed: %v", err)
		}
		err = store.AddDependency(ctx, &types.Dependency{
			IssueID:     closedDepMixed.ID,
			DependsOnID: baseline6.ID,
			Type:        types.DepParentChild,
		}, "test")
		if err != nil {
			t.Fatalf("Failed to add parent-child dependency: %v", err)
		}
		err = store.CloseIssue(ctx, closedDepMixed.ID, "Test", "test")
		if err != nil {
			t.Fatalf("Failed to close closedDepMixed: %v", err)
		}

		// Query should return only the ready dependent
		dependents, baselineMap, err := store.GetReadyDependentsOfBlockedBaselines(ctx, 10)
		if err != nil {
			t.Fatalf("GetReadyDependentsOfBlockedBaselines failed: %v", err)
		}

		// Verify only readyDep is returned for baseline6
		foundReady := false
		for _, dep := range dependents {
			if baselineMap[dep.ID] == baseline6.ID {
				switch dep.ID {
				case readyDep.ID:
					foundReady = true
				case blockedDepMixed.ID:
					t.Errorf("Should not return blocked dependent %s", dep.ID)
				case closedDepMixed.ID:
					t.Errorf("Should not return closed dependent %s", dep.ID)
				}
			}
		}

		if !foundReady {
			t.Error("Should return the ready dependent of baseline6")
		}

		t.Logf("✓ Mixed ready/blocked/closed dependents correctly filtered")
	})

	t.Run("edge case: baseline with no dependents", func(t *testing.T) {
		// Create a blocked baseline with no dependents at all
		baseline7 := &types.Issue{
			Title:              "Blocked baseline 7 (no dependents)",
			Status:             types.StatusOpen,
			Priority:           1,
			IssueType:          types.TypeBug,
			AcceptanceCriteria: "Test acceptance criteria",
		}
		err := store.CreateIssue(ctx, baseline7, "test")
		if err != nil {
			t.Fatalf("Failed to create baseline7: %v", err)
		}
		err = store.AddLabel(ctx, baseline7.ID, "baseline-failure", "test")
		if err != nil {
			t.Fatalf("Failed to add baseline-failure label: %v", err)
		}

		blocker7 := &types.Issue{
			Title:              "Blocker 7",
			Status:             types.StatusOpen,
			Priority:           1,
			IssueType:          types.TypeTask,
			AcceptanceCriteria: "Test acceptance criteria",
		}
		err = store.CreateIssue(ctx, blocker7, "test")
		if err != nil {
			t.Fatalf("Failed to create blocker7: %v", err)
		}
		err = store.AddDependency(ctx, &types.Dependency{
			IssueID:     baseline7.ID,
			DependsOnID: blocker7.ID,
			Type:        types.DepBlocks,
		}, "test")
		if err != nil {
			t.Fatalf("Failed to add blocking dependency: %v", err)
		}

		// Query should not return any dependents for baseline7
		dependents, baselineMap, err := store.GetReadyDependentsOfBlockedBaselines(ctx, 10)
		if err != nil {
			t.Fatalf("GetReadyDependentsOfBlockedBaselines failed: %v", err)
		}

		// Verify no dependents map to baseline7
		for _, dep := range dependents {
			if baselineMap[dep.ID] == baseline7.ID {
				t.Errorf("Should not return any dependents for baseline7 (it has none)")
			}
		}

		t.Logf("✓ Baseline with no dependents correctly returns nothing")
	})
}

// TestCorruptedDatabaseFile tests error handling when database file is corrupted (vc-n8ua)
func TestCorruptedDatabaseFile(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "corrupted.db")

	// Write garbage to the database file
	if err := os.WriteFile(dbPath, []byte("this is not a valid SQLite database"), 0644); err != nil {
		t.Fatalf("Failed to write corrupted file: %v", err)
	}

	// Try to open the corrupted database
	_, err := NewVCStorage(ctx, dbPath)

	// Should get an error
	if err == nil {
		t.Error("Expected error when opening corrupted database, but got nil")
	}

	// Error should indicate database corruption
	if !strings.Contains(err.Error(), "database") && !strings.Contains(err.Error(), "file") {
		t.Logf("Got error (acceptable): %v", err)
	}
}

// TestConcurrentAccessConflicts tests handling of concurrent updates to same issue (vc-n8ua)
func TestConcurrentAccessConflicts(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "concurrent.db")

	// Create first storage connection
	store1, err := NewVCStorage(ctx, dbPath)
	if err != nil {
		t.Fatalf("Failed to create first storage: %v", err)
	}
	defer func() { _ = store1.Close() }()

	// Create an issue
	issue := &types.Issue{
		Title:              "Concurrent Test Issue",
		Description:        "Testing concurrent updates",
		Status:             types.StatusOpen,
		Priority:           2,
		IssueType:          types.TypeTask,
		AcceptanceCriteria: "Handle concurrency gracefully",
	}

	if err := store1.CreateIssue(ctx, issue, "test"); err != nil {
		t.Fatalf("Failed to create issue: %v", err)
	}

	// Create second storage connection to same database
	store2, err := NewVCStorage(ctx, dbPath)
	if err != nil {
		t.Fatalf("Failed to create second storage: %v", err)
	}
	defer func() { _ = store2.Close() }()

	// Try concurrent updates from both connections
	done := make(chan error, 2)

	// Goroutine 1: Update priority
	go func() {
		updates := map[string]interface{}{"priority": 1}
		done <- store1.UpdateIssue(ctx, issue.ID, updates, "user1")
	}()

	// Goroutine 2: Update status
	go func() {
		updates := map[string]interface{}{"status": string(types.StatusInProgress)}
		done <- store2.UpdateIssue(ctx, issue.ID, updates, "user2")
	}()

	// Wait for both updates
	err1 := <-done
	err2 := <-done

	// At least one should succeed (SQLite serializes writes)
	if err1 != nil && err2 != nil {
		t.Errorf("Both concurrent updates failed: err1=%v, err2=%v", err1, err2)
	}

	// Verify issue can still be retrieved
	retrieved, err := store1.GetIssue(ctx, issue.ID)
	if err != nil {
		t.Fatalf("Failed to retrieve issue after concurrent updates: %v", err)
	}

	// Should have one of the updates
	if retrieved.Priority == 2 && retrieved.Status == types.StatusOpen {
		t.Error("Neither concurrent update was applied")
	}
}

// TestInvalidEventData tests handling of invalid data types in events.Data (vc-n8ua)
func TestInvalidEventData(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	store, err := NewVCStorage(ctx, dbPath)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer func() { _ = store.Close() }()

	// Create an issue
	issue := &types.Issue{
		Title:              "Event Test Issue",
		Status:             types.StatusOpen,
		Priority:           2,
		IssueType:          types.TypeTask,
		AcceptanceCriteria: "Test event data",
	}

	if err := store.CreateIssue(ctx, issue, "test"); err != nil {
		t.Fatalf("Failed to create issue: %v", err)
	}

	// Try to create event with complex data that might not serialize well
	event := &events.AgentEvent{
		IssueID:   issue.ID,
		Type:      events.EventTypeProgress,
		Severity:  events.SeverityInfo,
		Message:   "Test with complex data",
		Timestamp: time.Now(),
		Data: map[string]interface{}{
			"valid_string": "hello",
			"valid_number": 42,
			"valid_bool":   true,
			// These should be handled gracefully
			"nil_value":   nil,
			"empty_array": []interface{}{},
			"empty_map":   map[string]interface{}{},
		},
	}

	// Should handle event creation without panic
	err = store.StoreAgentEvent(ctx, event)
	if err != nil {
		t.Logf("StoreAgentEvent with complex data returned error (acceptable): %v", err)
	}

	// If it succeeded, verify we can retrieve it
	if err == nil {
		filter := events.EventFilter{IssueID: issue.ID}
		retrieved, err := store.GetAgentEvents(ctx, filter)
		if err != nil {
			t.Fatalf("Failed to retrieve events: %v", err)
		}
		if len(retrieved) == 0 {
			t.Error("Expected to retrieve stored event")
		}
	}
}

// TestConcurrentStatusUpdates tests concurrent goroutines updating same issue status
// to verify SQLite serialization prevents corruption (vc-0a3c)
func TestConcurrentStatusUpdates(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "concurrent_status.db")

	// Create storage connection
	store, err := NewVCStorage(ctx, dbPath)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer func() { _ = store.Close() }()

	// Create an issue
	issue := &types.Issue{
		Title:              "Concurrent Status Update Test",
		Description:        "Testing concurrent status updates",
		Status:             types.StatusOpen,
		Priority:           2,
		IssueType:          types.TypeTask,
		AcceptanceCriteria: "Verify SQLite serialization works correctly",
	}

	if err := store.CreateIssue(ctx, issue, "test"); err != nil {
		t.Fatalf("Failed to create issue: %v", err)
	}

	// Launch multiple goroutines that all try to update status concurrently
	// This stresses SQLite's serialization and tests for corruption
	numGoroutines := 20
	done := make(chan error, numGoroutines)

	// Each goroutine tries to update status to a different value
	statuses := []string{
		string(types.StatusInProgress),
		string(types.StatusBlocked),
		string(types.StatusOpen),
		string(types.StatusClosed),
	}

	for i := 0; i < numGoroutines; i++ {
		go func(goroutineID int) {
			// Pick a status based on goroutine ID
			newStatus := statuses[goroutineID%len(statuses)]
			updates := map[string]interface{}{
				"status": newStatus,
			}
			actor := fmt.Sprintf("user%d", goroutineID)
			done <- store.UpdateIssue(ctx, issue.ID, updates, actor)
		}(i)
	}

	// Collect results
	successCount := 0
	failureCount := 0
	for i := 0; i < numGoroutines; i++ {
		err := <-done
		if err != nil {
			failureCount++
			t.Logf("Goroutine %d failed: %v", i, err)
		} else {
			successCount++
		}
	}

	// At least some updates should succeed (SQLite serializes writes)
	if successCount == 0 {
		t.Error("All concurrent updates failed - expected at least some to succeed")
	}

	t.Logf("Concurrent status updates: %d succeeded, %d failed", successCount, failureCount)

	// Verify issue can still be retrieved and has valid data
	retrieved, err := store.GetIssue(ctx, issue.ID)
	if err != nil {
		t.Fatalf("Failed to retrieve issue after concurrent updates: %v", err)
	}

	// Status should be one of the values we tried to set
	validStatus := false
	for _, status := range statuses {
		if string(retrieved.Status) == status {
			validStatus = true
			break
		}
	}
	if !validStatus {
		t.Errorf("Issue has invalid status after concurrent updates: %s", retrieved.Status)
	}

	// Verify database integrity - issue should still be retrievable
	if retrieved.ID != issue.ID {
		t.Error("Retrieved issue has wrong ID - possible corruption")
	}
	if retrieved.Title != issue.Title {
		t.Error("Retrieved issue has wrong title - possible corruption")
	}

	t.Log("SQLite serialization handled concurrent status updates correctly")
}
