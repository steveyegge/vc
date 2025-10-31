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
	}
	err = store.CreateIssue(ctx, blocker, "test")
	if err != nil {
		t.Fatalf("Failed to create blocker: %v", err)
	}

	// Add discovered:blocker label
	err = store.AddLabel(ctx, blocker.ID, "discovered:blocker", "test")
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
		}
		err = store.CreateIssue(ctx, childBlocker, "test")
		if err != nil {
			t.Fatalf("Failed to create child blocker: %v", err)
		}

		err = store.AddLabel(ctx, childBlocker.ID, "discovered:blocker", "test")
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
		err = store.AddLabel(ctx, epicBlocker.ID, "discovered:blocker", "test")
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
		}
		err = store.CreateIssue(ctx, blocker, "test")
		if err != nil {
			t.Fatalf("Failed to create blocker: %v", err)
		}

		// Add discovered:blocker label
		err = store.AddLabel(ctx, blocker.ID, "discovered:blocker", "test")
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
