package repl

import (
	"context"
	"strings"
	"testing"
	"time"

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

func (m *mockStorage) GetDependencies(ctx context.Context, issueID string) ([]*types.Issue, error) {
	return m.dependencies[issueID], nil
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
func (m *mockStorage) GetMission(ctx context.Context, id string) (*types.Mission, error) {
	return nil, nil
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
func (m *mockStorage) UpdateHeartbeat(ctx context.Context, instanceID string) error {
	return nil
}
func (m *mockStorage) GetActiveInstances(ctx context.Context) ([]*types.ExecutorInstance, error) {
	return nil, nil
}
func (m *mockStorage) CleanupStaleInstances(ctx context.Context, staleThreshold int) (int, error) {
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
func (m *mockStorage) Close() error {
	return nil
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
