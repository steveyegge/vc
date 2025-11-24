package repl

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/steveyegge/vc/internal/events"
	"github.com/steveyegge/vc/internal/types"
)

// mockStorage implements storage.Storage for testing
type mockStorage struct {
	statistics       *types.Statistics
	blockedIssues    []*types.BlockedIssue
	agentEvents      []*events.AgentEvent
	issues           map[string]*types.Issue
	dependencies     map[string][]*types.Issue
	searchResults    []*types.Issue
	statisticsError  error
	blockedError     error
	agentEventsError error
	issueError       error
	searchError      error
}

func (m *mockStorage) GetStatistics(ctx context.Context) (*types.Statistics, error) {
	if m.statisticsError != nil {
		return nil, m.statisticsError
	}
	return m.statistics, nil
}

func (m *mockStorage) GetBlockedIssues(ctx context.Context) ([]*types.BlockedIssue, error) {
	if m.blockedError != nil {
		return nil, m.blockedError
	}
	return m.blockedIssues, nil
}
func (m *mockStorage) GetReadyBlockers(ctx context.Context, limit int) ([]*types.Issue, error) {
	return nil, nil
}
func (m *mockStorage) GetReadyBaselineIssues(ctx context.Context, limit int) ([]*types.Issue, error) {
	return nil, nil
}
func (m *mockStorage) GetReadyDependentsOfBlockedBaselines(ctx context.Context, limit int) ([]*types.Issue, map[string]string, error) {
	return nil, nil, nil
}
func (m *mockStorage) IsEpicComplete(ctx context.Context, epicID string) (bool, error) {
	return false, nil
}
func (m *mockStorage) GetMissionForTask(ctx context.Context, taskID string) (*types.MissionContext, error) {
	return nil, fmt.Errorf("not implemented in mock")
}
func (m *mockStorage) GetMissionsNeedingGates(ctx context.Context) ([]*types.Issue, error) {
	return nil, nil
}

func (m *mockStorage) GetRecentAgentEvents(ctx context.Context, limit int) ([]*events.AgentEvent, error) {
	if m.agentEventsError != nil {
		return nil, m.agentEventsError
	}
	results := m.agentEvents
	if len(results) > limit {
		results = results[:limit]
	}
	return results, nil
}

func (m *mockStorage) GetAgentEventsByIssue(ctx context.Context, issueID string) ([]*events.AgentEvent, error) {
	if m.agentEventsError != nil {
		return nil, m.agentEventsError
	}
	var results []*events.AgentEvent
	for _, evt := range m.agentEvents {
		if evt.IssueID == issueID {
			results = append(results, evt)
		}
	}
	return results, nil
}

func (m *mockStorage) SearchIssues(ctx context.Context, query string, filter types.IssueFilter) ([]*types.Issue, error) {
	if m.searchError != nil {
		return nil, m.searchError
	}
	return m.searchResults, nil
}

func (m *mockStorage) GetIssue(ctx context.Context, id string) (*types.Issue, error) {
	if m.issueError != nil {
		return nil, m.issueError
	}
	issue, ok := m.issues[id]
	if !ok {
		return nil, context.DeadlineExceeded // Use a standard error
	}
	return issue, nil
}

func (m *mockStorage) GetIssues(ctx context.Context, ids []string) (map[string]*types.Issue, error) {
	result := make(map[string]*types.Issue)
	for _, id := range ids {
		if issue, exists := m.issues[id]; exists {
			result[id] = issue
		}
	}
	return result, nil
}

func (m *mockStorage) GetDependencies(ctx context.Context, issueID string) ([]*types.Issue, error) {
	return m.dependencies[issueID], nil
}

func (m *mockStorage) GetDependencyRecords(ctx context.Context, issueID string) ([]*types.Dependency, error) {
	return nil, nil
}

func (m *mockStorage) GetExecutionHistory(ctx context.Context, issueID string) ([]*types.ExecutionAttempt, error) {
	return nil, nil
}

func (m *mockStorage) GetExecutionHistoryPaginated(ctx context.Context, issueID string, limit, offset int) ([]*types.ExecutionAttempt, error) {
	return nil, nil
}

func (m *mockStorage) RecordExecutionAttempt(ctx context.Context, attempt *types.ExecutionAttempt) error {
	return nil
}

// Stub implementations for other storage interface methods
func (m *mockStorage) StoreAgentEvent(ctx context.Context, event *events.AgentEvent) error {
	return nil
}
func (m *mockStorage) GetAgentEvents(ctx context.Context, filter events.EventFilter) ([]*events.AgentEvent, error) {
	return nil, nil
}
func (m *mockStorage) CreateIssue(ctx context.Context, issue *types.Issue, actor string) error {
	return nil
}
func (m *mockStorage) CreateMission(ctx context.Context, mission *types.Mission, actor string) error {
	return nil
}
func (m *mockStorage) GetMission(ctx context.Context, id string) (*types.Mission, error) {
	return nil, nil
}
func (m *mockStorage) UpdateMission(ctx context.Context, id string, updates map[string]interface{}, actor string) error {
	return nil
}
func (m *mockStorage) UpdateIssue(ctx context.Context, id string, updates map[string]interface{}, actor string) error {
	return nil
}
func (m *mockStorage) CloseIssue(ctx context.Context, id string, reason string, actor string) error {
	return nil
}
func (m *mockStorage) AddDependency(ctx context.Context, dep *types.Dependency, actor string) error {
	return nil
}
func (m *mockStorage) RemoveDependency(ctx context.Context, issueID, dependsOnID string, actor string) error {
	return nil
}
func (m *mockStorage) GetDependents(ctx context.Context, issueID string) ([]*types.Issue, error) {
	return nil, nil
}
func (m *mockStorage) GetDependencyTree(ctx context.Context, issueID string, maxDepth int) ([]*types.TreeNode, error) {
	return nil, nil
}
func (m *mockStorage) DetectCycles(ctx context.Context) ([][]*types.Issue, error) {
	return nil, nil
}
func (m *mockStorage) AddLabel(ctx context.Context, issueID, label, actor string) error {
	return nil
}
func (m *mockStorage) RemoveLabel(ctx context.Context, issueID, label, actor string) error {
	return nil
}
func (m *mockStorage) GetLabels(ctx context.Context, issueID string) ([]string, error) {
	return nil, nil
}
func (m *mockStorage) GetIssuesByLabel(ctx context.Context, label string) ([]*types.Issue, error) {
	return nil, nil
}
func (m *mockStorage) GetReadyWork(ctx context.Context, filter types.WorkFilter) ([]*types.Issue, error) {
	return nil, nil
}
func (m *mockStorage) AddComment(ctx context.Context, issueID, actor, comment string) error {
	return nil
}
func (m *mockStorage) GetEvents(ctx context.Context, issueID string, limit int) ([]*types.Event, error) {
	return nil, nil
}
func (m *mockStorage) RegisterInstance(ctx context.Context, instance *types.ExecutorInstance) error {
	return nil
}
func (m *mockStorage) MarkInstanceStopped(ctx context.Context, instanceID string) error {
	return nil
}
func (m *mockStorage) UpdateHeartbeat(ctx context.Context, instanceID string) error {
	return nil
}
func (m *mockStorage) UpdateSelfHealingMode(ctx context.Context, instanceID string, mode string) error {
	return nil
}
func (m *mockStorage) GetActiveInstances(ctx context.Context) ([]*types.ExecutorInstance, error) {
	return nil, nil
}
func (m *mockStorage) CleanupStaleInstances(ctx context.Context, staleThreshold int) (int, error) {
	return 0, nil
}
func (m *mockStorage) DeleteOldStoppedInstances(ctx context.Context, olderThanSeconds int, maxToKeep int) (int, error) {
	return 0, nil
}
func (m *mockStorage) ClaimIssue(ctx context.Context, issueID, executorInstanceID string) error {
	return nil
}
func (m *mockStorage) GetExecutionState(ctx context.Context, issueID string) (*types.IssueExecutionState, error) {
	return nil, nil
}
func (m *mockStorage) UpdateExecutionState(ctx context.Context, issueID string, state types.ExecutionState) error {
	return nil
}
func (m *mockStorage) SaveCheckpoint(ctx context.Context, issueID string, checkpointData interface{}) error {
	return nil
}
func (m *mockStorage) GetCheckpoint(ctx context.Context, issueID string) (string, error) {
	return "", nil
}
func (m *mockStorage) ReleaseIssue(ctx context.Context, issueID string) error {
	return nil
}
func (m *mockStorage) ReleaseIssueAndReopen(ctx context.Context, issueID, actor, errorComment string) error {
	return nil
}

// Status change logging (vc-n4lx)
func (m *mockStorage) LogStatusChange(ctx context.Context, issueID string, newStatus types.Status, actor, reason string) {
	// No-op for tests
}
func (m *mockStorage) LogStatusChangeFromUpdates(ctx context.Context, issueID string, updates map[string]interface{}, actor, reason string) {
	// No-op for tests
}

func (m *mockStorage) Close() error {
	return nil
}
func (m *mockStorage) GetConfig(ctx context.Context, key string) (string, error) {
	return "", nil
}
func (m *mockStorage) SetConfig(ctx context.Context, key, value string) error {
	return nil
}
func (m *mockStorage) GetIssuePrefix(ctx context.Context) (string, error) {
	return "vc", nil // vc-0bt1: Default to "vc" prefix for VC project tests
}
func (m *mockStorage) CleanupEventsByAge(ctx context.Context, retentionDays, criticalRetentionDays, batchSize int) (int, error) {
	return 0, nil
}
func (m *mockStorage) CleanupEventsByIssueLimit(ctx context.Context, perIssueLimit, batchSize int) (int, error) {
	return 0, nil
}
func (m *mockStorage) CleanupEventsByGlobalLimit(ctx context.Context, globalLimit, batchSize int) (int, error) {
	return 0, nil
}
func (m *mockStorage) GetEventCounts(ctx context.Context) (*types.EventCounts, error) {
	return &types.EventCounts{}, nil
}
func (m *mockStorage) VacuumDatabase(ctx context.Context) error {
	return nil
}
func (m *mockStorage) RecordWatchdogIntervention(ctx context.Context, issueID string) error {
	return nil
}

// Baseline Diagnostics methods (vc-9aa9)
func (m *mockStorage) StoreDiagnosis(ctx context.Context, issueID string, diagnosis *types.TestFailureDiagnosis) error {
	return nil
}
func (m *mockStorage) GetDiagnosis(ctx context.Context, issueID string) (*types.TestFailureDiagnosis, error) {
	return nil, nil
}

// Interrupt/resume methods (pause/resume workflow)
func (m *mockStorage) SaveInterruptMetadata(ctx context.Context, metadata *types.InterruptMetadata) error {
	return nil
}
func (m *mockStorage) GetInterruptMetadata(ctx context.Context, issueID string) (*types.InterruptMetadata, error) {
	return nil, nil
}
func (m *mockStorage) MarkInterruptResumed(ctx context.Context, issueID string) error {
	return nil
}
func (m *mockStorage) DeleteInterruptMetadata(ctx context.Context, issueID string) error {
	return nil
}
func (m *mockStorage) ListInterruptedIssues(ctx context.Context) ([]*types.InterruptMetadata, error) {
	return nil, nil
}

// Mission Plans (vc-un1o, vc-gxfn, vc-d295)
func (m *mockStorage) StorePlan(ctx context.Context, plan *types.MissionPlan, expectedIteration int) (int, error) {
	return 1, nil
}
func (m *mockStorage) GetPlan(ctx context.Context, missionID string) (*types.MissionPlan, int, error) {
	return nil, 0, nil
}
func (m *mockStorage) GetPlanHistory(ctx context.Context, missionID string) ([]*types.MissionPlan, error) {
	return nil, nil
}
func (m *mockStorage) DeletePlan(ctx context.Context, missionID string) error {
	return nil
}
func (m *mockStorage) ListDraftPlans(ctx context.Context) ([]*types.MissionPlan, error) {
	return nil, nil
}

// TestToolGetStatus tests the get_status tool
func TestToolGetStatus(t *testing.T) {
	t.Run("successful status retrieval", func(t *testing.T) {
		mock := &mockStorage{
			statistics: &types.Statistics{
				TotalIssues:      100,
				OpenIssues:       40,
				InProgressIssues: 10,
				BlockedIssues:    5,
				ClosedIssues:     45,
				ReadyIssues:      25,
				AverageLeadTime:  12.5,
			},
		}
		handler := &ConversationHandler{storage: mock}
		ctx := context.Background()

		result, err := handler.toolGetStatus(ctx, map[string]interface{}{})
		if err != nil {
			t.Fatalf("toolGetStatus failed: %v", err)
		}

		// Verify output contains expected values
		if !strings.Contains(result, "Total Issues: 100") {
			t.Errorf("Expected total issues in output, got: %s", result)
		}
		if !strings.Contains(result, "Open: 40") {
			t.Errorf("Expected open issues in output, got: %s", result)
		}
		if !strings.Contains(result, "Ready to Work: 25") {
			t.Errorf("Expected ready issues in output, got: %s", result)
		}
	})

	t.Run("rejects unexpected parameters", func(t *testing.T) {
		mock := &mockStorage{}
		handler := &ConversationHandler{storage: mock}
		ctx := context.Background()

		_, err := handler.toolGetStatus(ctx, map[string]interface{}{"foo": "bar"})
		if err == nil {
			t.Error("Expected error for unexpected parameters, got nil")
		}
		if !strings.Contains(err.Error(), "takes no parameters") {
			t.Errorf("Expected 'takes no parameters' error, got: %v", err)
		}
	})
}

// TestToolGetBlockedIssues tests the get_blocked_issues tool
func TestToolGetBlockedIssues(t *testing.T) {
	t.Run("successful blocked issues retrieval", func(t *testing.T) {
		mock := &mockStorage{
			blockedIssues: []*types.BlockedIssue{
				{
					Issue: types.Issue{
						ID:        "vc-1",
						Title:     "Blocked Issue 1",
						IssueType: types.TypeTask,
						Priority:  1,
					},
					BlockedByCount: 2,
					BlockedBy:      []string{"vc-2", "vc-3"},
				},
				{
					Issue: types.Issue{
						ID:        "vc-4",
						Title:     "Blocked Issue 2",
						IssueType: types.TypeFeature,
						Priority:  0,
					},
					BlockedByCount: 1,
					BlockedBy:      []string{"vc-5"},
				},
			},
		}
		handler := &ConversationHandler{storage: mock}
		ctx := context.Background()

		result, err := handler.toolGetBlockedIssues(ctx, map[string]interface{}{})
		if err != nil {
			t.Fatalf("toolGetBlockedIssues failed: %v", err)
		}

		if !strings.Contains(result, "vc-1") || !strings.Contains(result, "vc-4") {
			t.Errorf("Expected both blocked issues in output, got: %s", result)
		}
		if !strings.Contains(result, "Blocked by 2 issues") {
			t.Errorf("Expected blocker count in output, got: %s", result)
		}
	})

	t.Run("applies limit correctly", func(t *testing.T) {
		blockedIssues := make([]*types.BlockedIssue, 20)
		for i := 0; i < 20; i++ {
			blockedIssues[i] = &types.BlockedIssue{
				Issue: types.Issue{
					ID:        "vc-" + string(rune('1'+i)),
					Title:     "Issue",
					IssueType: types.TypeTask,
					Priority:  1,
				},
				BlockedByCount: 1,
				BlockedBy:      []string{"dep"},
			}
		}
		mock := &mockStorage{blockedIssues: blockedIssues}
		handler := &ConversationHandler{storage: mock}
		ctx := context.Background()

		result, err := handler.toolGetBlockedIssues(ctx, map[string]interface{}{"limit": float64(5)})
		if err != nil {
			t.Fatalf("toolGetBlockedIssues failed: %v", err)
		}

		// Count occurrences - should be limited to 5
		if strings.Count(result, "vc-") != 5 {
			t.Errorf("Expected 5 issues with limit, got different count in: %s", result)
		}
	})

	t.Run("handles no blocked issues", func(t *testing.T) {
		mock := &mockStorage{blockedIssues: []*types.BlockedIssue{}}
		handler := &ConversationHandler{storage: mock}
		ctx := context.Background()

		result, err := handler.toolGetBlockedIssues(ctx, map[string]interface{}{})
		if err != nil {
			t.Fatalf("toolGetBlockedIssues failed: %v", err)
		}

		if result != "No blocked issues found" {
			t.Errorf("Expected 'No blocked issues found', got: %s", result)
		}
	})
}

// TestToolGetRecentActivity tests the get_recent_activity tool
func TestToolGetRecentActivity(t *testing.T) {
	now := time.Now()

	t.Run("gets all recent activity", func(t *testing.T) {
		mock := &mockStorage{
			agentEvents: []*events.AgentEvent{
				{
					ID:        "evt-1",
					Type:      events.EventTypeAgentSpawned,
					Timestamp: now,
					IssueID:   "vc-1",
					Severity:  events.SeverityInfo,
					Message:   "Agent spawned",
				},
				{
					ID:        "evt-2",
					Type:      events.EventTypeError,
					Timestamp: now.Add(-1 * time.Minute),
					IssueID:   "vc-2",
					Severity:  events.SeverityError,
					Message:   "Build failed",
				},
			},
		}
		handler := &ConversationHandler{storage: mock}
		ctx := context.Background()

		result, err := handler.toolGetRecentActivity(ctx, map[string]interface{}{})
		if err != nil {
			t.Fatalf("toolGetRecentActivity failed: %v", err)
		}

		if !strings.Contains(result, "vc-1") || !strings.Contains(result, "vc-2") {
			t.Errorf("Expected both events in output, got: %s", result)
		}
		if !strings.Contains(result, "Agent spawned") {
			t.Errorf("Expected event message in output, got: %s", result)
		}
		if !strings.Contains(result, "[error]") {
			t.Errorf("Expected severity in output, got: %s", result)
		}
	})

	t.Run("filters by issue_id", func(t *testing.T) {
		mock := &mockStorage{
			agentEvents: []*events.AgentEvent{
				{
					ID:        "evt-1",
					Type:      events.EventTypeProgress,
					Timestamp: now,
					IssueID:   "vc-1",
					Severity:  events.SeverityInfo,
					Message:   "Working on vc-1",
				},
				{
					ID:        "evt-2",
					Type:      events.EventTypeProgress,
					Timestamp: now,
					IssueID:   "vc-2",
					Severity:  events.SeverityInfo,
					Message:   "Working on vc-2",
				},
			},
		}
		handler := &ConversationHandler{storage: mock}
		ctx := context.Background()

		result, err := handler.toolGetRecentActivity(ctx, map[string]interface{}{"issue_id": "vc-1"})
		if err != nil {
			t.Fatalf("toolGetRecentActivity failed: %v", err)
		}

		if !strings.Contains(result, "vc-1") {
			t.Errorf("Expected vc-1 in output, got: %s", result)
		}
		if strings.Contains(result, "vc-2") {
			t.Errorf("Expected vc-2 to be filtered out, got: %s", result)
		}
	})

	t.Run("respects limit parameter", func(t *testing.T) {
		manyEvents := make([]*events.AgentEvent, 50)
		for i := 0; i < 50; i++ {
			manyEvents[i] = &events.AgentEvent{
				ID:        "evt-" + string(rune('1'+i)),
				Type:      events.EventTypeProgress,
				Timestamp: now,
				IssueID:   "vc-1",
				Severity:  events.SeverityInfo,
				Message:   "Event",
			}
		}
		mock := &mockStorage{agentEvents: manyEvents}
		handler := &ConversationHandler{storage: mock}
		ctx := context.Background()

		result, err := handler.toolGetRecentActivity(ctx, map[string]interface{}{"limit": float64(10)})
		if err != nil {
			t.Fatalf("toolGetRecentActivity failed: %v", err)
		}

		// Should show "10 events" in header
		if !strings.Contains(result, "10 events") {
			t.Errorf("Expected '10 events' in output, got: %s", result)
		}
	})

	t.Run("handles no activity", func(t *testing.T) {
		mock := &mockStorage{agentEvents: []*events.AgentEvent{}}
		handler := &ConversationHandler{storage: mock}
		ctx := context.Background()

		result, err := handler.toolGetRecentActivity(ctx, map[string]interface{}{})
		if err != nil {
			t.Fatalf("toolGetRecentActivity failed: %v", err)
		}

		if result != "No recent agent activity found" {
			t.Errorf("Expected 'No recent agent activity found', got: %s", result)
		}
	})
}

// TestToolSearchIssues tests the search_issues tool
func TestToolSearchIssues(t *testing.T) {
	t.Run("successful search", func(t *testing.T) {
		mock := &mockStorage{
			searchResults: []*types.Issue{
				{
					ID:          "vc-1",
					Title:       "Add authentication",
					Description: "Implement user authentication",
					IssueType:   types.TypeFeature,
					Priority:    1,
					Status:      types.StatusOpen,
				},
				{
					ID:          "vc-2",
					Title:       "Fix auth bug",
					Description: "Users can't log in",
					IssueType:   types.TypeBug,
					Priority:    0,
					Status:      types.StatusInProgress,
				},
			},
		}
		handler := &ConversationHandler{storage: mock}
		ctx := context.Background()

		result, err := handler.toolSearchIssues(ctx, map[string]interface{}{"query": "auth"})
		if err != nil {
			t.Fatalf("toolSearchIssues failed: %v", err)
		}

		if !strings.Contains(result, "vc-1") || !strings.Contains(result, "vc-2") {
			t.Errorf("Expected both results in output, got: %s", result)
		}
		if !strings.Contains(result, "Add authentication") {
			t.Errorf("Expected issue title in output, got: %s", result)
		}
	})

	t.Run("requires query parameter", func(t *testing.T) {
		mock := &mockStorage{}
		handler := &ConversationHandler{storage: mock}
		ctx := context.Background()

		_, err := handler.toolSearchIssues(ctx, map[string]interface{}{})
		if err == nil {
			t.Error("Expected error for missing query, got nil")
		}
		if !strings.Contains(err.Error(), "query is required") {
			t.Errorf("Expected 'query is required' error, got: %v", err)
		}
	})

	t.Run("handles no results", func(t *testing.T) {
		mock := &mockStorage{searchResults: []*types.Issue{}}
		handler := &ConversationHandler{storage: mock}
		ctx := context.Background()

		result, err := handler.toolSearchIssues(ctx, map[string]interface{}{"query": "nonexistent"})
		if err != nil {
			t.Fatalf("toolSearchIssues failed: %v", err)
		}

		if !strings.Contains(result, "No issues found") {
			t.Errorf("Expected 'No issues found', got: %s", result)
		}
	})

	t.Run("truncates long descriptions", func(t *testing.T) {
		longDesc := strings.Repeat("a", 150)
		mock := &mockStorage{
			searchResults: []*types.Issue{
				{
					ID:          "vc-1",
					Title:       "Test",
					Description: longDesc,
					IssueType:   types.TypeTask,
					Priority:    1,
					Status:      types.StatusOpen,
				},
			},
		}
		handler := &ConversationHandler{storage: mock}
		ctx := context.Background()

		result, err := handler.toolSearchIssues(ctx, map[string]interface{}{"query": "test"})
		if err != nil {
			t.Fatalf("toolSearchIssues failed: %v", err)
		}

		// Should truncate to 100 chars and add "..."
		if !strings.Contains(result, "...") {
			t.Errorf("Expected truncation marker '...' in output, got: %s", result)
		}
		if strings.Count(result, "a") > 110 {
			t.Errorf("Description should be truncated, got: %s", result)
		}
	})
}

// TestToolContinueExecution tests the continue_execution tool validation
func TestToolContinueExecution(t *testing.T) {
	t.Run("rejects closed issues", func(t *testing.T) {
		mock := &mockStorage{
			issues: map[string]*types.Issue{
				"vc-1": {
					ID:     "vc-1",
					Title:  "Closed issue",
					Status: types.StatusClosed,
				},
			},
		}
		handler := &ConversationHandler{storage: mock}
		ctx := context.Background()

		result, err := handler.toolContinueExecution(ctx, map[string]interface{}{"issue_id": "vc-1"})
		if err != nil {
			t.Fatalf("Expected no error, got: %v", err)
		}

		if !strings.Contains(result, "already closed") {
			t.Errorf("Expected 'already closed' message, got: %s", result)
		}
	})

	t.Run("rejects in-progress issues", func(t *testing.T) {
		mock := &mockStorage{
			issues: map[string]*types.Issue{
				"vc-1": {
					ID:     "vc-1",
					Title:  "In progress issue",
					Status: types.StatusInProgress,
				},
			},
		}
		handler := &ConversationHandler{storage: mock}
		ctx := context.Background()

		result, err := handler.toolContinueExecution(ctx, map[string]interface{}{"issue_id": "vc-1"})
		if err != nil {
			t.Fatalf("Expected no error, got: %v", err)
		}

		if !strings.Contains(result, "already in progress") {
			t.Errorf("Expected 'already in progress' message, got: %s", result)
		}
	})

	t.Run("rejects blocked issues with blocker details", func(t *testing.T) {
		mock := &mockStorage{
			issues: map[string]*types.Issue{
				"vc-1": {
					ID:     "vc-1",
					Title:  "Blocked issue",
					Status: types.StatusBlocked,
				},
			},
			dependencies: map[string][]*types.Issue{
				"vc-1": {
					{
						ID:     "vc-2",
						Title:  "Blocker",
						Status: types.StatusOpen,
					},
				},
			},
		}
		handler := &ConversationHandler{storage: mock}
		ctx := context.Background()

		result, err := handler.toolContinueExecution(ctx, map[string]interface{}{"issue_id": "vc-1"})
		if err != nil {
			t.Fatalf("Expected no error, got: %v", err)
		}

		if !strings.Contains(result, "blocked by") {
			t.Errorf("Expected 'blocked by' message, got: %s", result)
		}
		if !strings.Contains(result, "vc-2") {
			t.Errorf("Expected blocker ID in message, got: %s", result)
		}
	})

	t.Run("rejects async mode", func(t *testing.T) {
		mock := &mockStorage{}
		handler := &ConversationHandler{storage: mock}
		ctx := context.Background()

		_, err := handler.toolContinueExecution(ctx, map[string]interface{}{"async": true})
		if err == nil {
			t.Error("Expected error for async mode, got nil")
		}
		if !strings.Contains(err.Error(), "async execution not yet implemented") {
			t.Errorf("Expected async error, got: %v", err)
		}
	})
}

// TestToolCreateIssue tests the create_issue tool
func TestToolCreateIssue(t *testing.T) {
	t.Run("creates issue with required fields", func(t *testing.T) {
		mock := &mockStorage{}
		handler := &ConversationHandler{storage: mock}
		ctx := context.Background()

		result, err := handler.toolCreateIssue(ctx, map[string]interface{}{
			"title": "Add authentication",
		})
		if err != nil {
			t.Fatalf("toolCreateIssue failed: %v", err)
		}

		if !strings.Contains(result, "Created") {
			t.Errorf("Expected success message, got: %s", result)
		}
		if !strings.Contains(result, "Add authentication") {
			t.Errorf("Expected title in result, got: %s", result)
		}
	})

	t.Run("creates issue with all fields", func(t *testing.T) {
		mock := &mockStorage{}
		handler := &ConversationHandler{storage: mock}
		ctx := context.Background()

		result, err := handler.toolCreateIssue(ctx, map[string]interface{}{
			"title":       "Add authentication",
			"description": "Implement OAuth2",
			"type":        "feature",
			"priority":    float64(1),
			"design":      "Use OAuth2 flow",
			"acceptance":  "Users can log in",
		})
		if err != nil {
			t.Fatalf("toolCreateIssue failed: %v", err)
		}

		if !strings.Contains(result, "Created feature") {
			t.Errorf("Expected 'Created feature', got: %s", result)
		}
	})

	t.Run("requires title", func(t *testing.T) {
		mock := &mockStorage{}
		handler := &ConversationHandler{storage: mock}
		ctx := context.Background()

		_, err := handler.toolCreateIssue(ctx, map[string]interface{}{})
		if err == nil {
			t.Error("Expected error for missing title, got nil")
		}
		if !strings.Contains(err.Error(), "title is required") {
			t.Errorf("Expected 'title is required' error, got: %v", err)
		}
	})

	t.Run("validates issue type", func(t *testing.T) {
		mock := &mockStorage{}
		handler := &ConversationHandler{storage: mock}
		ctx := context.Background()

		_, err := handler.toolCreateIssue(ctx, map[string]interface{}{
			"title": "Test",
			"type":  "invalid",
		})
		if err == nil {
			t.Error("Expected error for invalid type, got nil")
		}
		if !strings.Contains(err.Error(), "invalid issue type") {
			t.Errorf("Expected 'invalid issue type' error, got: %v", err)
		}
	})

	t.Run("defaults to task type", func(t *testing.T) {
		mock := &mockStorage{}
		handler := &ConversationHandler{storage: mock}
		ctx := context.Background()

		result, err := handler.toolCreateIssue(ctx, map[string]interface{}{
			"title": "Test",
		})
		if err != nil {
			t.Fatalf("toolCreateIssue failed: %v", err)
		}

		if !strings.Contains(result, "Created task") {
			t.Errorf("Expected 'Created task', got: %s", result)
		}
	})

	t.Run("defaults to priority 2", func(t *testing.T) {
		mock := &mockStorage{}
		handler := &ConversationHandler{storage: mock}
		ctx := context.Background()

		// We can't directly test priority in the result string,
		// but we can verify no error occurs
		_, err := handler.toolCreateIssue(ctx, map[string]interface{}{
			"title": "Test",
		})
		if err != nil {
			t.Fatalf("toolCreateIssue failed: %v", err)
		}
	})
}

// TestToolCreateEpic tests the create_epic tool
func TestToolCreateEpic(t *testing.T) {
	t.Run("creates epic with required fields", func(t *testing.T) {
		mock := &mockStorage{}
		handler := &ConversationHandler{storage: mock}
		ctx := context.Background()

		result, err := handler.toolCreateEpic(ctx, map[string]interface{}{
			"title": "Payment System",
		})
		if err != nil {
			t.Fatalf("toolCreateEpic failed: %v", err)
		}

		if !strings.Contains(result, "Created epic") {
			t.Errorf("Expected 'Created epic', got: %s", result)
		}
		if !strings.Contains(result, "Payment System") {
			t.Errorf("Expected title in result, got: %s", result)
		}
	})

	t.Run("creates epic with all fields", func(t *testing.T) {
		mock := &mockStorage{}
		handler := &ConversationHandler{storage: mock}
		ctx := context.Background()

		result, err := handler.toolCreateEpic(ctx, map[string]interface{}{
			"title":       "Payment System",
			"description": "Build payment infrastructure",
			"design":      "Use Stripe API",
			"acceptance":  "Users can pay with credit cards",
		})
		if err != nil {
			t.Fatalf("toolCreateEpic failed: %v", err)
		}

		if !strings.Contains(result, "Created epic") {
			t.Errorf("Expected success message, got: %s", result)
		}
	})

	t.Run("requires title", func(t *testing.T) {
		mock := &mockStorage{}
		handler := &ConversationHandler{storage: mock}
		ctx := context.Background()

		_, err := handler.toolCreateEpic(ctx, map[string]interface{}{})
		if err == nil {
			t.Error("Expected error for missing title, got nil")
		}
		if !strings.Contains(err.Error(), "title is required") {
			t.Errorf("Expected 'title is required' error, got: %v", err)
		}
	})
}

// TestToolAddChildToEpic tests the add_child_to_epic tool
func TestToolAddChildToEpic(t *testing.T) {
	t.Run("adds child with default blocks=true", func(t *testing.T) {
		mock := &mockStorage{}
		handler := &ConversationHandler{storage: mock}
		ctx := context.Background()

		result, err := handler.toolAddChildToEpic(ctx, map[string]interface{}{
			"epic_id":        "vc-1",
			"child_issue_id": "vc-2",
		})
		if err != nil {
			t.Fatalf("toolAddChildToEpic failed: %v", err)
		}

		if !strings.Contains(result, "Added vc-2 as child of epic vc-1") {
			t.Errorf("Expected success message, got: %s", result)
		}
		if !strings.Contains(result, "blocks=true") {
			t.Errorf("Expected blocks=true in result, got: %s", result)
		}
	})

	t.Run("adds child with blocks=false", func(t *testing.T) {
		mock := &mockStorage{}
		handler := &ConversationHandler{storage: mock}
		ctx := context.Background()

		result, err := handler.toolAddChildToEpic(ctx, map[string]interface{}{
			"epic_id":        "vc-1",
			"child_issue_id": "vc-2",
			"blocks":         false,
		})
		if err != nil {
			t.Fatalf("toolAddChildToEpic failed: %v", err)
		}

		if !strings.Contains(result, "blocks=false") {
			t.Errorf("Expected blocks=false in result, got: %s", result)
		}
	})

	t.Run("requires epic_id", func(t *testing.T) {
		mock := &mockStorage{}
		handler := &ConversationHandler{storage: mock}
		ctx := context.Background()

		_, err := handler.toolAddChildToEpic(ctx, map[string]interface{}{
			"child_issue_id": "vc-2",
		})
		if err == nil {
			t.Error("Expected error for missing epic_id, got nil")
		}
		if !strings.Contains(err.Error(), "required") {
			t.Errorf("Expected 'required' error, got: %v", err)
		}
	})

	t.Run("requires child_issue_id", func(t *testing.T) {
		mock := &mockStorage{}
		handler := &ConversationHandler{storage: mock}
		ctx := context.Background()

		_, err := handler.toolAddChildToEpic(ctx, map[string]interface{}{
			"epic_id": "vc-1",
		})
		if err == nil {
			t.Error("Expected error for missing child_issue_id, got nil")
		}
		if !strings.Contains(err.Error(), "required") {
			t.Errorf("Expected 'required' error, got: %v", err)
		}
	})
}

// TestToolGetReadyWork tests the get_ready_work tool
func TestToolGetReadyWork(t *testing.T) {
	t.Run("gets ready work with default limit", func(t *testing.T) {
		mock := &mockStorage{
			// GetReadyWork is called by the tool, needs to be implemented in mock
		}
		handler := &ConversationHandler{storage: mock}
		ctx := context.Background()

		// Mock will return empty list, which is fine for this test
		result, err := handler.toolGetReadyWork(ctx, map[string]interface{}{})
		if err != nil {
			t.Fatalf("toolGetReadyWork failed: %v", err)
		}

		if result != "No ready work found" {
			t.Errorf("Expected 'No ready work found', got: %s", result)
		}
	})

	t.Run("applies limit parameter", func(t *testing.T) {
		mock := &mockStorage{}
		handler := &ConversationHandler{storage: mock}
		ctx := context.Background()

		// Just verify it doesn't error with limit param
		_, err := handler.toolGetReadyWork(ctx, map[string]interface{}{
			"limit": float64(10),
		})
		if err != nil {
			t.Fatalf("toolGetReadyWork failed: %v", err)
		}
	})
}

// TestToolGetIssue tests the get_issue tool
func TestToolGetIssue(t *testing.T) {
	t.Run("gets issue successfully", func(t *testing.T) {
		mock := &mockStorage{
			issues: map[string]*types.Issue{
				"vc-1": {
					ID:          "vc-1",
					Title:       "Test Issue",
					Description: "Test description",
					IssueType:   types.TypeTask,
					Priority:    1,
					Status:      types.StatusOpen,
				},
			},
		}
		handler := &ConversationHandler{storage: mock}
		ctx := context.Background()

		result, err := handler.toolGetIssue(ctx, map[string]interface{}{
			"issue_id": "vc-1",
		})
		if err != nil {
			t.Fatalf("toolGetIssue failed: %v", err)
		}

		if !strings.Contains(result, "vc-1") {
			t.Errorf("Expected issue ID in result, got: %s", result)
		}
		if !strings.Contains(result, "Test Issue") {
			t.Errorf("Expected title in result, got: %s", result)
		}
	})

	t.Run("requires issue_id", func(t *testing.T) {
		mock := &mockStorage{}
		handler := &ConversationHandler{storage: mock}
		ctx := context.Background()

		_, err := handler.toolGetIssue(ctx, map[string]interface{}{})
		if err == nil {
			t.Error("Expected error for missing issue_id, got nil")
		}
		if !strings.Contains(err.Error(), "issue_id is required") {
			t.Errorf("Expected 'issue_id is required' error, got: %v", err)
		}
	})

	t.Run("handles issue not found", func(t *testing.T) {
		mock := &mockStorage{
			issues: map[string]*types.Issue{},
		}
		handler := &ConversationHandler{storage: mock}
		ctx := context.Background()

		_, err := handler.toolGetIssue(ctx, map[string]interface{}{
			"issue_id": "vc-999",
		})
		if err == nil {
			t.Error("Expected error for non-existent issue, got nil")
		}
	})
}

// TestToolContinueUntilBlocked tests the continue_until_blocked tool
func TestToolContinueUntilBlocked(t *testing.T) {
	t.Run("stops when no ready work", func(t *testing.T) {
		mock := &mockStorage{}
		handler := &ConversationHandler{storage: mock}
		ctx := context.Background()

		result, err := handler.toolContinueUntilBlocked(ctx, map[string]interface{}{})
		if err != nil {
			t.Fatalf("toolContinueUntilBlocked failed: %v", err)
		}

		if !strings.Contains(result, "no more ready work") {
			t.Errorf("Expected 'no more ready work' stop reason, got: %s", result)
		}
		if !strings.Contains(result, "Completed: 0") {
			t.Errorf("Expected 0 completed issues, got: %s", result)
		}
	})

	t.Run("accepts max_iterations parameter", func(t *testing.T) {
		mock := &mockStorage{}
		handler := &ConversationHandler{storage: mock}
		ctx := context.Background()

		// Should not error with max_iterations
		_, err := handler.toolContinueUntilBlocked(ctx, map[string]interface{}{
			"max_iterations": float64(5),
		})
		if err != nil {
			t.Fatalf("toolContinueUntilBlocked failed: %v", err)
		}
	})

	t.Run("accepts timeout_minutes parameter", func(t *testing.T) {
		mock := &mockStorage{}
		handler := &ConversationHandler{storage: mock}
		ctx := context.Background()

		// Should not error with timeout_minutes
		_, err := handler.toolContinueUntilBlocked(ctx, map[string]interface{}{
			"timeout_minutes": float64(60),
		})
		if err != nil {
			t.Fatalf("toolContinueUntilBlocked failed: %v", err)
		}
	})

	t.Run("accepts error_threshold parameter", func(t *testing.T) {
		mock := &mockStorage{}
		handler := &ConversationHandler{storage: mock}
		ctx := context.Background()

		// Should not error with error_threshold
		_, err := handler.toolContinueUntilBlocked(ctx, map[string]interface{}{
			"error_threshold": float64(5),
		})
		if err != nil {
			t.Fatalf("toolContinueUntilBlocked failed: %v", err)
		}
	})

	t.Run("uses default parameters", func(t *testing.T) {
		mock := &mockStorage{}
		handler := &ConversationHandler{storage: mock}
		ctx := context.Background()

		// Should work with no parameters (all defaults)
		result, err := handler.toolContinueUntilBlocked(ctx, map[string]interface{}{})
		if err != nil {
			t.Fatalf("toolContinueUntilBlocked failed: %v", err)
		}

		if !strings.Contains(result, "Autonomous Execution Complete") {
			t.Errorf("Expected success message, got: %s", result)
		}
	})

	t.Run("formats result correctly", func(t *testing.T) {
		mock := &mockStorage{}
		handler := &ConversationHandler{storage: mock}
		ctx := context.Background()

		result, err := handler.toolContinueUntilBlocked(ctx, map[string]interface{}{})
		if err != nil {
			t.Fatalf("toolContinueUntilBlocked failed: %v", err)
		}

		// Verify all expected sections are present
		expectedSections := []string{
			"Autonomous Execution Complete",
			"Stop Reason:",
			"Iterations:",
			"Elapsed Time:",
			"Completed:",
			"Failed:",
		}

		for _, section := range expectedSections {
			if !strings.Contains(result, section) {
				t.Errorf("Expected section '%s' in result, got: %s", section, result)
			}
		}
	})
}

// TestExecuteTool tests the tool dispatcher
func TestExecuteTool(t *testing.T) {
	t.Run("dispatches to correct tool", func(t *testing.T) {
		mock := &mockStorage{
			statistics: &types.Statistics{
				TotalIssues: 10,
			},
		}
		handler := &ConversationHandler{storage: mock}
		ctx := context.Background()

		// Test dispatching to get_status
		result, err := handler.executeTool(ctx, "get_status", map[string]interface{}{})
		if err != nil {
			t.Fatalf("executeTool failed: %v", err)
		}
		if !strings.Contains(result, "Project Status") {
			t.Errorf("Expected status output, got: %s", result)
		}

		// Test dispatching to create_issue
		result, err = handler.executeTool(ctx, "create_issue", map[string]interface{}{
			"title": "Test",
		})
		if err != nil {
			t.Fatalf("executeTool failed: %v", err)
		}
		if !strings.Contains(result, "Created") {
			t.Errorf("Expected creation message, got: %s", result)
		}
	})

	t.Run("returns error for unknown tool", func(t *testing.T) {
		mock := &mockStorage{}
		handler := &ConversationHandler{storage: mock}
		ctx := context.Background()

		_, err := handler.executeTool(ctx, "unknown_tool", map[string]interface{}{})
		if err == nil {
			t.Error("Expected error for unknown tool, got nil")
		}
		if !strings.Contains(err.Error(), "unknown tool") {
			t.Errorf("Expected 'unknown tool' error, got: %v", err)
		}
	})

	t.Run("returns error for invalid input format", func(t *testing.T) {
		mock := &mockStorage{}
		handler := &ConversationHandler{storage: mock}
		ctx := context.Background()

		_, err := handler.executeTool(ctx, "get_status", "invalid")
		if err == nil {
			t.Error("Expected error for invalid input, got nil")
		}
		if !strings.Contains(err.Error(), "invalid tool input") {
			t.Errorf("Expected 'invalid tool input' error, got: %v", err)
		}
	})

	t.Run("handles JSON byte input from Anthropic SDK", func(t *testing.T) {
		mock := &mockStorage{}
		handler := &ConversationHandler{storage: mock}
		ctx := context.Background()

		// Simulate what Anthropic SDK sends: raw JSON bytes
		jsonBytes := []byte(`{"limit":5}`)

		// This should work - the SDK sends []byte, not map[string]interface{}
		_, err := handler.executeTool(ctx, "get_ready_work", jsonBytes)
		if err != nil {
			t.Fatalf("executeTool should handle []byte input, got error: %v", err)
		}
	})

	t.Run("handles json.RawMessage input", func(t *testing.T) {
		mock := &mockStorage{}
		handler := &ConversationHandler{storage: mock}
		ctx := context.Background()

		// Simulate json.RawMessage input
		jsonRaw := json.RawMessage(`{"title":"Test Issue"}`)

		_, err := handler.executeTool(ctx, "create_issue", jsonRaw)
		if err != nil {
			t.Fatalf("executeTool should handle json.RawMessage input, got error: %v", err)
		}
	})

	t.Run("handles empty JSON object as bytes", func(t *testing.T) {
		mock := &mockStorage{
			statistics: &types.Statistics{
				TotalIssues: 5,
			},
		}
		handler := &ConversationHandler{storage: mock}
		ctx := context.Background()

		// Empty JSON object as bytes (common for tools with no parameters)
		jsonBytes := []byte(`{}`)

		result, err := handler.executeTool(ctx, "get_status", jsonBytes)
		if err != nil {
			t.Fatalf("executeTool should handle empty JSON object, got error: %v", err)
		}
		if !strings.Contains(result, "Project Status") {
			t.Errorf("Expected status output, got: %s", result)
		}
	})
}

// TestClearHistory tests conversation history clearing
func TestClearHistory(t *testing.T) {
	mock := &mockStorage{}
	handler := &ConversationHandler{storage: mock}

	// Add some history (using the proper type)
	handler.history = append(handler.history,
		anthropic.NewUserMessage(anthropic.NewTextBlock("test message")))

	if len(handler.history) == 0 {
		t.Error("Setup failed - history should not be empty before clearing")
	}

	handler.ClearHistory()

	if len(handler.history) != 0 {
		t.Errorf("Expected empty history, got %d items", len(handler.history))
	}
}

// TestGetTools tests tool definition generation
func TestGetTools(t *testing.T) {
	mock := &mockStorage{}
	handler := &ConversationHandler{storage: mock}

	tools := handler.getTools()

	// Should have 11 tools
	expectedTools := 11
	if len(tools) != expectedTools {
		t.Errorf("Expected %d tools, got %d", expectedTools, len(tools))
	}

	// Verify all expected tools are present
	expectedToolNames := []string{
		"create_issue",
		"create_epic",
		"add_child_to_epic",
		"get_ready_work",
		"get_issue",
		"get_status",
		"get_blocked_issues",
		"continue_execution",
		"continue_until_blocked",
		"get_recent_activity",
		"search_issues",
	}

	toolNames := make(map[string]bool)
	for _, tool := range tools {
		if tool.OfTool != nil {
			toolNames[tool.OfTool.Name] = true
		}
	}

	for _, expectedName := range expectedToolNames {
		if !toolNames[expectedName] {
			t.Errorf("Missing expected tool: %s", expectedName)
		}
	}
}

// TestSystemPrompt tests that system prompt is generated
func TestSystemPrompt(t *testing.T) {
	mock := &mockStorage{}
	handler := &ConversationHandler{storage: mock}

	prompt := handler.systemPrompt()

	// Verify key sections are present
	expectedSections := []string{
		"VibeCoder",
		"conversational tools",
		"create_issue",
		"continue_execution",
		"get_ready_work",
		"CONVERSATIONAL INTENT PATTERNS",
		"BEHAVIORAL GUIDELINES",
	}

	for _, section := range expectedSections {
		if !strings.Contains(prompt, section) {
			t.Errorf("System prompt missing expected section: %s", section)
		}
	}

	// Verify it's not empty
	if len(prompt) < 1000 {
		t.Errorf("System prompt seems too short: %d characters", len(prompt))
	}
}

// TestNewConversationHandler tests the constructor
func TestNewConversationHandler(t *testing.T) {
	t.Run("succeeds with API key", func(t *testing.T) {
		// Save original env
		originalKey := os.Getenv("ANTHROPIC_API_KEY")
		defer func() {
			if originalKey != "" {
				os.Setenv("ANTHROPIC_API_KEY", originalKey)
			} else {
				os.Unsetenv("ANTHROPIC_API_KEY")
			}
		}()

		// Set test API key
		os.Setenv("ANTHROPIC_API_KEY", "test-key-12345")

		mock := &mockStorage{}
		handler, err := NewConversationHandler(mock, "test-actor")

		if err != nil {
			t.Fatalf("Expected no error, got: %v", err)
		}

		if handler == nil {
			t.Fatal("Expected handler to be created")
		}

		if handler.storage != mock {
			t.Error("Expected storage to be set")
		}

		if handler.actor != "test-actor" {
			t.Errorf("Expected actor 'test-actor', got: %s", handler.actor)
		}

		if handler.model != "claude-sonnet-4-5-20250929" {
			t.Errorf("Expected correct model, got: %s", handler.model)
		}

		if handler.history == nil {
			t.Error("Expected history to be initialized")
		}

		if len(handler.history) != 0 {
			t.Errorf("Expected empty history, got %d items", len(handler.history))
		}
	})

	t.Run("defaults actor to 'user'", func(t *testing.T) {
		originalKey := os.Getenv("ANTHROPIC_API_KEY")
		defer func() {
			if originalKey != "" {
				os.Setenv("ANTHROPIC_API_KEY", originalKey)
			} else {
				os.Unsetenv("ANTHROPIC_API_KEY")
			}
		}()

		os.Setenv("ANTHROPIC_API_KEY", "test-key-12345")

		mock := &mockStorage{}
		handler, err := NewConversationHandler(mock, "")

		if err != nil {
			t.Fatalf("Expected no error, got: %v", err)
		}

		if handler.actor != "user" {
			t.Errorf("Expected default actor 'user', got: %s", handler.actor)
		}
	})

	t.Run("fails without API key", func(t *testing.T) {
		// Save and clear API key
		originalKey := os.Getenv("ANTHROPIC_API_KEY")
		os.Unsetenv("ANTHROPIC_API_KEY")
		defer func() {
			if originalKey != "" {
				os.Setenv("ANTHROPIC_API_KEY", originalKey)
			}
		}()

		mock := &mockStorage{}
		handler, err := NewConversationHandler(mock, "test")

		if err == nil {
			t.Error("Expected error for missing API key, got nil")
		}

		if handler != nil {
			t.Error("Expected nil handler when API key is missing")
		}

		if !strings.Contains(err.Error(), "ANTHROPIC_API_KEY") {
			t.Errorf("Expected error message to mention ANTHROPIC_API_KEY, got: %v", err)
		}
	})
}
