package beads

import (
	"context"
	"fmt"
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
	defer func() { _ = store.Close() }()

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
	defer func() { _ = store.Close() }()

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
	defer func() { _ = store.Close() }()

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
	defer func() { _ = store.Close() }()

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
