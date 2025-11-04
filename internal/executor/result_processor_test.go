package executor

import (
	"context"
	"testing"
	"time"

	"github.com/steveyegge/vc/internal/events"
	"github.com/steveyegge/vc/internal/labels"
	"github.com/steveyegge/vc/internal/storage"
	"github.com/steveyegge/vc/internal/types"
)

// TestMissionSkipsInlineGates verifies that missions (epics with subtype=mission)
// skip inline quality gates and instead add the needs-quality-gates label (vc-251)
func TestMissionSkipsInlineGates(t *testing.T) {
	// Setup storage
	cfg := storage.DefaultConfig()
	cfg.Path = t.TempDir() + "/test.db"

	ctx := context.Background()
	store, err := storage.NewStorage(ctx, cfg)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer func() { _ = store.Close() }()

	// Create a mission issue (epic with subtype=mission)
	mission := &types.Issue{
		Title:              "Test Mission",
		Description:        "This is a test mission",
		IssueType:          types.TypeEpic,
		IssueSubtype:       types.SubtypeMission,
		Status:             types.StatusInProgress,
		Priority:           1,
		AcceptanceCriteria: "Mission should complete",
		CreatedAt:          time.Now(),
		UpdatedAt:          time.Now(),
	}

	if err := store.CreateIssue(ctx, mission, "test"); err != nil {
		t.Fatalf("Failed to create mission: %v", err)
	}

	// Create results processor with quality gates enabled
	rpCfg := &ResultsProcessorConfig{
		Store:              store,
		EnableQualityGates: true,
		WorkingDir:         "/tmp/test",
		Actor:              "test-executor",
	}

	rp, err := NewResultsProcessor(rpCfg)
	if err != nil {
		t.Fatalf("Failed to create results processor: %v", err)
	}

	// Create successful agent result
	agentResult := &AgentResult{
		Success:  true,
		ExitCode: 0,
		Duration: time.Second,
		Output:   []string{"Mission work complete"},
	}

	// Process the agent result
	result, err := rp.ProcessAgentResult(ctx, mission, agentResult)
	if err != nil {
		t.Fatalf("ProcessAgentResult failed: %v", err)
	}

	// Verify expectations for mission path (vc-251):
	// 1. Issue should NOT be completed (missions stay open until all tasks complete)
	if result.Completed {
		t.Error("Expected mission to NOT be completed, but it was marked as completed")
	}

	// 2. Gates should be marked as "passed" (not failed, just deferred)
	if !result.GatesPassed {
		t.Error("Expected GatesPassed=true for mission (deferred, not failed)")
	}

	// 3. needs-quality-gates label should be added
	issueLabels, err := store.GetLabels(ctx, mission.ID)
	if err != nil {
		t.Fatalf("Failed to get labels: %v", err)
	}

	hasLabel := false
	for _, label := range issueLabels {
		if label == labels.LabelNeedsQualityGates {
			hasLabel = true
			break
		}
	}
	if !hasLabel {
		t.Errorf("Expected needs-quality-gates label to be added, but it was not found. Labels: %v", issueLabels)
	}

	// 4. EventTypeQualityGatesDeferred should be emitted
	filter := events.EventFilter{
		IssueID: mission.ID,
		Type:    events.EventTypeQualityGatesDeferred,
	}
	deferredEvents, err := store.GetAgentEvents(ctx, filter)
	if err != nil {
		t.Fatalf("Failed to get agent events: %v", err)
	}

	if len(deferredEvents) == 0 {
		t.Error("Expected EventTypeQualityGatesDeferred to be emitted, but no events found")
	} else if len(deferredEvents) > 1 {
		t.Errorf("Expected exactly 1 deferred event, got %d", len(deferredEvents))
	} else {
		// Verify event data
		event := deferredEvents[0]
		if event.Type != events.EventTypeQualityGatesDeferred {
			t.Errorf("Expected event type %s, got %s", events.EventTypeQualityGatesDeferred, event.Type)
		}
		if event.Severity != events.SeverityInfo {
			t.Errorf("Expected severity %s, got %s", events.SeverityInfo, event.Severity)
		}

		// Check event data
		missionID, ok := event.Data["mission_id"].(string)
		if !ok || missionID != mission.ID {
			t.Errorf("Expected mission_id=%s in event data, got %v", mission.ID, event.Data["mission_id"])
		}

		reason, ok := event.Data["reason"].(string)
		if !ok || reason != "delegated-to-qa-worker" {
			t.Errorf("Expected reason='delegated-to-qa-worker' in event data, got %v", event.Data["reason"])
		}
	}

	// 5. No quality gates should have been run (no EventTypeQualityGatesStarted)
	gatesFilter := events.EventFilter{
		IssueID: mission.ID,
		Type:    events.EventTypeQualityGatesStarted,
	}
	gatesEvents, err := store.GetAgentEvents(ctx, gatesFilter)
	if err != nil {
		t.Fatalf("Failed to get gates agent events: %v", err)
	}

	if len(gatesEvents) > 0 {
		t.Errorf("Expected NO quality gates to run for mission, but found %d gate events", len(gatesEvents))
	}
}

// TestRegularTaskRunsInlineGates verifies that regular tasks (non-missions)
// still run quality gates inline, maintaining backward compatibility (vc-251)
func TestRegularTaskRunsInlineGates(t *testing.T) {
	// Setup storage
	cfg := storage.DefaultConfig()
	cfg.Path = t.TempDir() + "/test.db"

	ctx := context.Background()
	store, err := storage.NewStorage(ctx, cfg)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer func() { _ = store.Close() }()

	// Create a regular task issue (not a mission)
	task := &types.Issue{
		Title:              "Test Task",
		Description:        "This is a regular test task",
		IssueType:          types.TypeTask,
		IssueSubtype:       types.SubtypeNormal,
		Status:             types.StatusInProgress,
		Priority:           1,
		AcceptanceCriteria: "Task should complete",
		CreatedAt:          time.Now(),
		UpdatedAt:          time.Now(),
	}

	if err := store.CreateIssue(ctx, task, "test"); err != nil {
		t.Fatalf("Failed to create task: %v", err)
	}

	// Create results processor with quality gates enabled
	// Note: We're using a non-VC working directory so gates will be skipped
	// (to avoid running actual gates in tests), but we can verify the skipped event
	rpCfg := &ResultsProcessorConfig{
		Store:              store,
		EnableQualityGates: true,
		WorkingDir:         "/tmp/test", // Non-VC repo - gates will be skipped
		Actor:              "test-executor",
	}

	rp, err := NewResultsProcessor(rpCfg)
	if err != nil {
		t.Fatalf("Failed to create results processor: %v", err)
	}

	// Create successful agent result
	agentResult := &AgentResult{
		Success:  true,
		ExitCode: 0,
		Duration: time.Second,
		Output:   []string{"Task work complete"},
	}

	// Process the agent result
	result, err := rp.ProcessAgentResult(ctx, task, agentResult)
	if err != nil {
		t.Fatalf("ProcessAgentResult failed: %v", err)
	}

	// Verify expectations for regular task path (backward compatibility):
	// 1. needs-quality-gates label should NOT be added (this is mission-only)
	issueLabels, err := store.GetLabels(ctx, task.ID)
	if err != nil {
		t.Fatalf("Failed to get labels: %v", err)
	}

	hasLabel := false
	for _, label := range issueLabels {
		if label == labels.LabelNeedsQualityGates {
			hasLabel = true
			break
		}
	}
	if hasLabel {
		t.Error("Expected needs-quality-gates label NOT to be added for regular task, but it was found")
	}

	// 2. EventTypeQualityGatesDeferred should NOT be emitted (mission-only)
	deferredFilter := events.EventFilter{
		IssueID: task.ID,
		Type:    events.EventTypeQualityGatesDeferred,
	}
	deferredEvents, err := store.GetAgentEvents(ctx, deferredFilter)
	if err != nil {
		t.Fatalf("Failed to get deferred agent events: %v", err)
	}

	if len(deferredEvents) > 0 {
		t.Errorf("Expected NO deferred events for regular task, but found %d", len(deferredEvents))
	}

	// 3. Quality gates should be attempted (EventTypeQualityGatesSkipped in this case
	//    because we're not in VC repo, but the important thing is we didn't defer)
	skippedFilter := events.EventFilter{
		IssueID: task.ID,
		Type:    events.EventTypeQualityGatesSkipped,
	}
	skippedEvents, err := store.GetAgentEvents(ctx, skippedFilter)
	if err != nil {
		t.Fatalf("Failed to get skipped agent events: %v", err)
	}

	// We expect exactly one skipped event (because we're not in VC repo)
	// This proves we went through the normal gates path, not the mission deferral path
	if len(skippedEvents) != 1 {
		t.Errorf("Expected exactly 1 quality gates skipped event for regular task, got %d", len(skippedEvents))
	}

	// 4. Task should be completed (unlike missions which stay open)
	if !result.Completed {
		t.Error("Expected regular task to be completed, but it was not")
	}
}
