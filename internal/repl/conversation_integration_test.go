package repl

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/steveyegge/vc/internal/events"
	"github.com/steveyegge/vc/internal/types"
)

// mockStorageIntegration is an enhanced mock for integration tests
type mockStorageIntegration struct {
	mockStorage
	createdIssues    []*types.Issue
	readyWork        []*types.Issue
	claimedIssues    map[string]string
	executionStates  map[string]types.ExecutionState
	addedDeps        []*types.Dependency
	comments         map[string][]string
}

func (m *mockStorageIntegration) CreateIssue(ctx context.Context, issue *types.Issue, actor string) error {
	// Auto-generate ID
	if issue.ID == "" {
		issue.ID = "vc-" + string(rune('1'+len(m.createdIssues)))
	}
	m.createdIssues = append(m.createdIssues, issue)
	if m.issues == nil {
		m.issues = make(map[string]*types.Issue)
	}
	m.issues[issue.ID] = issue
	return nil
}

func (m *mockStorageIntegration) CreateMission(ctx context.Context, mission *types.Mission, actor string) error {
	// Generate a simple ID for mission-based issues
	return m.CreateIssue(ctx, &mission.Issue, actor)
}

func (m *mockStorageIntegration) GetReadyWork(ctx context.Context, filter types.WorkFilter) ([]*types.Issue, error) {
	if m.readyWork != nil {
		result := m.readyWork
		if filter.Limit > 0 && len(result) > filter.Limit {
			result = result[:filter.Limit]
		}
		return result, nil
	}
	return nil, nil
}

func (m *mockStorageIntegration) IsEpicComplete(ctx context.Context, epicID string) (bool, error) {
	return false, nil
}

func (m *mockStorageIntegration) GetMissionForTask(ctx context.Context, taskID string) (*types.MissionContext, error) {
	return nil, fmt.Errorf("not implemented in mock")
}
func (m *mockStorageIntegration) GetMissionByPhase(ctx context.Context, phaseID string) (*types.Mission, error) {
	return nil, fmt.Errorf("not implemented in mock")
}

func (m *mockStorageIntegration) ClaimIssue(ctx context.Context, issueID, executorInstanceID string) error {
	if m.claimedIssues == nil {
		m.claimedIssues = make(map[string]string)
	}
	m.claimedIssues[issueID] = executorInstanceID
	return nil
}

func (m *mockStorageIntegration) UpdateExecutionState(ctx context.Context, issueID string, state types.ExecutionState) error {
	if m.executionStates == nil {
		m.executionStates = make(map[string]types.ExecutionState)
	}
	m.executionStates[issueID] = state
	return nil
}

func (m *mockStorageIntegration) ReleaseIssue(ctx context.Context, issueID string) error {
	if m.claimedIssues != nil {
		delete(m.claimedIssues, issueID)
	}
	return nil
}

func (m *mockStorageIntegration) ReleaseIssueAndReopen(ctx context.Context, issueID, actor, errorComment string) error {
	if m.claimedIssues != nil {
		delete(m.claimedIssues, issueID)
	}
	return nil
}

func (m *mockStorageIntegration) AddDependency(ctx context.Context, dep *types.Dependency, actor string) error {
	m.addedDeps = append(m.addedDeps, dep)
	return nil
}

func (m *mockStorageIntegration) AddComment(ctx context.Context, issueID, actor, comment string) error {
	if m.comments == nil {
		m.comments = make(map[string][]string)
	}
	m.comments[issueID] = append(m.comments[issueID], comment)
	return nil
}

func (m *mockStorageIntegration) GetDependencies(ctx context.Context, issueID string) ([]*types.Issue, error) {
	if m.dependencies != nil {
		return m.dependencies[issueID], nil
	}
	return nil, nil
}

// Interrupt/resume methods (pause/resume workflow)
func (m *mockStorageIntegration) SaveInterruptMetadata(ctx context.Context, metadata *types.InterruptMetadata) error {
	return nil
}
func (m *mockStorageIntegration) GetInterruptMetadata(ctx context.Context, issueID string) (*types.InterruptMetadata, error) {
	return nil, nil
}
func (m *mockStorageIntegration) MarkInterruptResumed(ctx context.Context, issueID string) error {
	return nil
}
func (m *mockStorageIntegration) DeleteInterruptMetadata(ctx context.Context, issueID string) error {
	return nil
}
func (m *mockStorageIntegration) ListInterruptedIssues(ctx context.Context) ([]*types.InterruptMetadata, error) {
	return nil, nil
}

// Mission Plans (vc-un1o, vc-gxfn, vc-d295)
func (m *mockStorageIntegration) StorePlan(ctx context.Context, plan *types.MissionPlan, expectedIteration int) (int, error) {
	return 1, nil
}
func (m *mockStorageIntegration) GetPlan(ctx context.Context, missionID string) (*types.MissionPlan, int, error) {
	return nil, 0, nil
}
func (m *mockStorageIntegration) GetPlanHistory(ctx context.Context, missionID string) ([]*types.MissionPlan, error) {
	return nil, nil
}
func (m *mockStorageIntegration) DeletePlan(ctx context.Context, missionID string) error {
	return nil
}
func (m *mockStorageIntegration) ListDraftPlans(ctx context.Context) ([]*types.MissionPlan, error) {
	return nil, nil
}

// TestConversationalFlows tests end-to-end conversation scenarios
func TestConversationalFlows(t *testing.T) {
	// Note: These tests validate tool selection and parameter passing.
	// They cannot test actual AI conversation since that requires ANTHROPIC_API_KEY.
	// For full conversation testing, see manual test scenarios in docs.

	t.Run("create issue flow", func(t *testing.T) {
		mock := &mockStorageIntegration{}
		handler := &ConversationHandler{storage: mock}
		ctx := context.Background()

		// Simulate: User says "Add Docker support"
		// AI calls: create_issue(title="Add Docker support", type="feature")
		result, err := handler.toolCreateIssue(ctx, map[string]interface{}{
			"title": "Add Docker support",
			"type":  "feature",
		})

		if err != nil {
			t.Fatalf("Failed to create issue: %v", err)
		}

		if !strings.Contains(result, "Created feature") {
			t.Errorf("Expected feature creation, got: %s", result)
		}

		if len(mock.createdIssues) != 1 {
			t.Errorf("Expected 1 issue created, got %d", len(mock.createdIssues))
		}
	})

	t.Run("check ready work flow", func(t *testing.T) {
		mock := &mockStorageIntegration{
			readyWork: []*types.Issue{
				{
					ID:        "vc-1",
					Title:     "Fix login bug",
					IssueType: types.TypeBug,
					Priority:  0,
					Status:    types.StatusOpen,
				},
				{
					ID:        "vc-2",
					Title:     "Add logging",
					IssueType: types.TypeTask,
					Priority:  2,
					Status:    types.StatusOpen,
				},
			},
		}
		handler := &ConversationHandler{storage: mock}
		ctx := context.Background()

		// Simulate: User says "What's ready to work on?"
		// AI calls: get_ready_work()
		result, err := handler.toolGetReadyWork(ctx, map[string]interface{}{})

		if err != nil {
			t.Fatalf("Failed to get ready work: %v", err)
		}

		if !strings.Contains(result, "vc-1") || !strings.Contains(result, "vc-2") {
			t.Errorf("Expected both issues in result, got: %s", result)
		}
	})

	t.Run("check blocked issues flow", func(t *testing.T) {
		mock := &mockStorageIntegration{}
		mock.blockedIssues = []*types.BlockedIssue{
			{
				Issue: types.Issue{
					ID:        "vc-10",
					Title:     "Deploy to prod",
					IssueType: types.TypeTask,
					Priority:  1,
				},
				BlockedByCount: 2,
				BlockedBy:      []string{"vc-8", "vc-9"},
			},
		}
		handler := &ConversationHandler{storage: mock}
		ctx := context.Background()

		// Simulate: User says "What's blocked?"
		// AI calls: get_blocked_issues()
		result, err := handler.toolGetBlockedIssues(ctx, map[string]interface{}{})

		if err != nil {
			t.Fatalf("Failed to get blocked issues: %v", err)
		}

		if !strings.Contains(result, "vc-10") {
			t.Errorf("Expected blocked issue in result, got: %s", result)
		}
		if !strings.Contains(result, "vc-8") || !strings.Contains(result, "vc-9") {
			t.Errorf("Expected blockers in result, got: %s", result)
		}
	})

	t.Run("check project status flow", func(t *testing.T) {
		mock := &mockStorageIntegration{}
		mock.statistics = &types.Statistics{
			TotalIssues:      50,
			OpenIssues:       20,
			InProgressIssues: 5,
			BlockedIssues:    3,
			ClosedIssues:     22,
			ReadyIssues:      12,
			AverageLeadTime:  4.5,
		}
		handler := &ConversationHandler{storage: mock}
		ctx := context.Background()

		// Simulate: User says "How's the project doing?"
		// AI calls: get_status()
		result, err := handler.toolGetStatus(ctx, map[string]interface{}{})

		if err != nil {
			t.Fatalf("Failed to get status: %v", err)
		}

		if !strings.Contains(result, "Total Issues: 50") {
			t.Errorf("Expected total in result, got: %s", result)
		}
		if !strings.Contains(result, "Ready to Work: 12") {
			t.Errorf("Expected ready count in result, got: %s", result)
		}
	})

	t.Run("search issues flow", func(t *testing.T) {
		mock := &mockStorageIntegration{}
		mock.searchResults = []*types.Issue{
			{
				ID:          "vc-15",
				Title:       "Fix authentication bug",
				Description: "Users can't log in with OAuth",
				IssueType:   types.TypeBug,
				Priority:    0,
				Status:      types.StatusOpen,
			},
		}
		handler := &ConversationHandler{storage: mock}
		ctx := context.Background()

		// Simulate: User says "Show me issues about authentication"
		// AI calls: search_issues(query="authentication")
		result, err := handler.toolSearchIssues(ctx, map[string]interface{}{
			"query": "authentication",
		})

		if err != nil {
			t.Fatalf("Failed to search issues: %v", err)
		}

		if !strings.Contains(result, "vc-15") {
			t.Errorf("Expected issue in result, got: %s", result)
		}
		if !strings.Contains(result, "authentication") {
			t.Errorf("Expected search term in result, got: %s", result)
		}
	})

	t.Run("create epic with children flow", func(t *testing.T) {
		mock := &mockStorageIntegration{}
		handler := &ConversationHandler{storage: mock}
		ctx := context.Background()

		// Simulate: User says "Build a payment system"
		// AI calls: create_epic(title="Payment System")
		epicResult, err := handler.toolCreateEpic(ctx, map[string]interface{}{
			"title":       "Payment System",
			"description": "Complete payment infrastructure",
		})

		if err != nil {
			t.Fatalf("Failed to create epic: %v", err)
		}

		if !strings.Contains(epicResult, "Created epic") {
			t.Errorf("Expected epic creation, got: %s", epicResult)
		}

		// Verify epic was created
		if len(mock.createdIssues) != 1 {
			t.Fatalf("Expected 1 epic created, got %d", len(mock.createdIssues))
		}

		epicID := mock.createdIssues[0].ID

		// AI then creates child issues
		_, err = handler.toolCreateIssue(ctx, map[string]interface{}{
			"title": "Stripe integration",
			"type":  "task",
		})
		if err != nil {
			t.Fatalf("Failed to create child issue: %v", err)
		}

		childID := mock.createdIssues[1].ID

		// AI adds child to epic
		addResult, err := handler.toolAddChildToEpic(ctx, map[string]interface{}{
			"epic_id":        epicID,
			"child_issue_id": childID,
			"blocks":         true,
		})

		if err != nil {
			t.Fatalf("Failed to add child to epic: %v", err)
		}

		if !strings.Contains(addResult, epicID) {
			t.Errorf("Expected epic ID in result, got: %s", addResult)
		}

		// Verify dependency was created
		if len(mock.addedDeps) < 1 {
			t.Errorf("Expected at least 1 dependency added, got %d", len(mock.addedDeps))
		}
	})

	t.Run("recent activity flow", func(t *testing.T) {
		now := time.Now()
		mock := &mockStorageIntegration{}
		mock.agentEvents = []*events.AgentEvent{
			{
				ID:        "evt-1",
				Type:      events.EventTypeAgentSpawned,
				Timestamp: now,
				IssueID:   "vc-1",
				Severity:  events.SeverityInfo,
				Message:   "Started work on authentication",
			},
			{
				ID:        "evt-2",
				Type:      events.EventTypeAgentCompleted,
				Timestamp: now.Add(-5 * time.Minute),
				IssueID:   "vc-1",
				Severity:  events.SeverityInfo,
				Message:   "Completed authentication feature",
			},
		}
		handler := &ConversationHandler{storage: mock}
		ctx := context.Background()

		// Simulate: User says "What's been happening?"
		// AI calls: get_recent_activity()
		result, err := handler.toolGetRecentActivity(ctx, map[string]interface{}{})

		if err != nil {
			t.Fatalf("Failed to get recent activity: %v", err)
		}

		if !strings.Contains(result, "vc-1") {
			t.Errorf("Expected issue in result, got: %s", result)
		}
		if !strings.Contains(result, "authentication") {
			t.Errorf("Expected activity details in result, got: %s", result)
		}
	})

	t.Run("get issue details flow", func(t *testing.T) {
		mock := &mockStorageIntegration{}
		mock.issues = map[string]*types.Issue{
			"vc-42": {
				ID:                 "vc-42",
				Title:              "Implement caching layer",
				Description:        "Add Redis caching",
				Design:             "Use Redis with TTL",
				AcceptanceCriteria: "Response time < 100ms",
				IssueType:          types.TypeFeature,
				Priority:           1,
				Status:             types.StatusOpen,
			},
		}
		handler := &ConversationHandler{storage: mock}
		ctx := context.Background()

		// Simulate: User says "Tell me about vc-42"
		// AI calls: get_issue(issue_id="vc-42")
		result, err := handler.toolGetIssue(ctx, map[string]interface{}{
			"issue_id": "vc-42",
		})

		if err != nil {
			t.Fatalf("Failed to get issue: %v", err)
		}

		if !strings.Contains(result, "vc-42") {
			t.Errorf("Expected issue ID in result, got: %s", result)
		}
		if !strings.Contains(result, "caching") {
			t.Errorf("Expected issue details in result, got: %s", result)
		}
	})
}

// TestMultiTurnContextPreservation tests context across multiple tool calls
func TestMultiTurnContextPreservation(t *testing.T) {
	// These tests verify that multiple tool calls can work together
	// Real conversation context is handled by the Anthropic API

	t.Run("create then view issue", func(t *testing.T) {
		mock := &mockStorageIntegration{}
		handler := &ConversationHandler{storage: mock}
		ctx := context.Background()

		// Turn 1: Create issue
		createResult, err := handler.toolCreateIssue(ctx, map[string]interface{}{
			"title":       "Add rate limiting",
			"type":        "feature",
			"description": "Prevent API abuse",
		})
		if err != nil {
			t.Fatalf("Failed to create issue: %v", err)
		}

		if len(mock.createdIssues) != 1 {
			t.Fatalf("Expected 1 issue created, got %d", len(mock.createdIssues))
		}
		issueID := mock.createdIssues[0].ID

		// Turn 2: View the created issue
		viewResult, err := handler.toolGetIssue(ctx, map[string]interface{}{
			"issue_id": issueID,
		})
		if err != nil {
			t.Fatalf("Failed to get issue: %v", err)
		}

		if !strings.Contains(viewResult, "rate limiting") {
			t.Errorf("Expected title in view result, got: %s", viewResult)
		}

		// Verify both operations succeeded
		if !strings.Contains(createResult, "Created") {
			t.Errorf("Create result should show success: %s", createResult)
		}
	})

	t.Run("search then get specific issue", func(t *testing.T) {
		mock := &mockStorageIntegration{}
		mock.searchResults = []*types.Issue{
			{
				ID:          "vc-20",
				Title:       "Database migration",
				Description: "Migrate to PostgreSQL",
				IssueType:   types.TypeTask,
				Priority:    1,
				Status:      types.StatusOpen,
			},
		}
		mock.issues = map[string]*types.Issue{
			"vc-20": mock.searchResults[0],
		}
		handler := &ConversationHandler{storage: mock}
		ctx := context.Background()

		// Turn 1: Search for database issues
		searchResult, err := handler.toolSearchIssues(ctx, map[string]interface{}{
			"query": "database",
		})
		if err != nil {
			t.Fatalf("Failed to search: %v", err)
		}

		if !strings.Contains(searchResult, "vc-20") {
			t.Fatalf("Expected vc-20 in search results, got: %s", searchResult)
		}

		// Turn 2: Get details for the found issue
		detailsResult, err := handler.toolGetIssue(ctx, map[string]interface{}{
			"issue_id": "vc-20",
		})
		if err != nil {
			t.Fatalf("Failed to get issue: %v", err)
		}

		if !strings.Contains(detailsResult, "PostgreSQL") {
			t.Errorf("Expected full details in result, got: %s", detailsResult)
		}
	})

	t.Run("create epic then add multiple children", func(t *testing.T) {
		mock := &mockStorageIntegration{}
		handler := &ConversationHandler{storage: mock}
		ctx := context.Background()

		// Turn 1: Create epic
		_, err := handler.toolCreateEpic(ctx, map[string]interface{}{
			"title": "User Management System",
		})
		if err != nil {
			t.Fatalf("Failed to create epic: %v", err)
		}
		epicID := mock.createdIssues[0].ID

		// Turn 2: Create first child
		_, err = handler.toolCreateIssue(ctx, map[string]interface{}{
			"title": "User registration",
			"type":  "task",
		})
		if err != nil {
			t.Fatalf("Failed to create child 1: %v", err)
		}
		child1ID := mock.createdIssues[1].ID

		// Turn 3: Create second child
		_, err = handler.toolCreateIssue(ctx, map[string]interface{}{
			"title": "User login",
			"type":  "task",
		})
		if err != nil {
			t.Fatalf("Failed to create child 2: %v", err)
		}
		child2ID := mock.createdIssues[2].ID

		// Turn 4: Add first child to epic
		_, err = handler.toolAddChildToEpic(ctx, map[string]interface{}{
			"epic_id":        epicID,
			"child_issue_id": child1ID,
		})
		if err != nil {
			t.Fatalf("Failed to add child 1 to epic: %v", err)
		}

		// Turn 5: Add second child to epic
		_, err = handler.toolAddChildToEpic(ctx, map[string]interface{}{
			"epic_id":        epicID,
			"child_issue_id": child2ID,
		})
		if err != nil {
			t.Fatalf("Failed to add child 2 to epic: %v", err)
		}

		// Verify structure
		if len(mock.createdIssues) != 3 {
			t.Errorf("Expected 3 issues (1 epic, 2 children), got %d", len(mock.createdIssues))
		}

		if len(mock.addedDeps) < 2 {
			t.Errorf("Expected at least 2 dependencies added, got %d", len(mock.addedDeps))
		}
	})
}

// TestErrorHandlingAndRecovery tests error scenarios
func TestErrorHandlingAndRecovery(t *testing.T) {
	t.Run("missing required parameters", func(t *testing.T) {
		mock := &mockStorageIntegration{}
		handler := &ConversationHandler{storage: mock}
		ctx := context.Background()

		// Missing title
		_, err := handler.toolCreateIssue(ctx, map[string]interface{}{})
		if err == nil {
			t.Error("Expected error for missing title")
		}

		// Missing issue_id
		_, err = handler.toolGetIssue(ctx, map[string]interface{}{})
		if err == nil {
			t.Error("Expected error for missing issue_id")
		}

		// Missing query
		_, err = handler.toolSearchIssues(ctx, map[string]interface{}{})
		if err == nil {
			t.Error("Expected error for missing query")
		}
	})

	t.Run("invalid parameter values", func(t *testing.T) {
		mock := &mockStorageIntegration{}
		handler := &ConversationHandler{storage: mock}
		ctx := context.Background()

		// Invalid issue type
		_, err := handler.toolCreateIssue(ctx, map[string]interface{}{
			"title": "Test",
			"type":  "invalid_type",
		})
		if err == nil {
			t.Error("Expected error for invalid type")
		}

		// Unexpected parameters for get_status
		_, err = handler.toolGetStatus(ctx, map[string]interface{}{
			"invalid_param": "value",
		})
		if err == nil {
			t.Error("Expected error for unexpected parameters")
		}
	})

	t.Run("issue not found", func(t *testing.T) {
		mock := &mockStorageIntegration{}
		mock.issues = map[string]*types.Issue{}
		handler := &ConversationHandler{storage: mock}
		ctx := context.Background()

		_, err := handler.toolGetIssue(ctx, map[string]interface{}{
			"issue_id": "vc-9999",
		})
		if err == nil {
			t.Error("Expected error for non-existent issue")
		}
	})

	t.Run("empty results scenarios", func(t *testing.T) {
		mock := &mockStorageIntegration{}
		handler := &ConversationHandler{storage: mock}
		ctx := context.Background()

		// No ready work
		result, err := handler.toolGetReadyWork(ctx, map[string]interface{}{})
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
		if result != "No ready work found" {
			t.Errorf("Expected 'No ready work found', got: %s", result)
		}

		// No blocked issues
		result, err = handler.toolGetBlockedIssues(ctx, map[string]interface{}{})
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
		if result != "No blocked issues found" {
			t.Errorf("Expected 'No blocked issues found', got: %s", result)
		}

		// No recent activity
		result, err = handler.toolGetRecentActivity(ctx, map[string]interface{}{})
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
		if result != "No recent agent activity found" {
			t.Errorf("Expected 'No recent agent activity found', got: %s", result)
		}
	})

	t.Run("continue_execution validation errors", func(t *testing.T) {
		mock := &mockStorageIntegration{}
		handler := &ConversationHandler{storage: mock}
		ctx := context.Background()

		// Async not supported
		_, err := handler.toolContinueExecution(ctx, map[string]interface{}{
			"async": true,
		})
		if err == nil {
			t.Error("Expected error for async execution")
		}

		// Closed issue
		mock.issues = map[string]*types.Issue{
			"vc-1": {
				ID:     "vc-1",
				Status: types.StatusClosed,
			},
		}
		result, err := handler.toolContinueExecution(ctx, map[string]interface{}{
			"issue_id": "vc-1",
		})
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
		if !strings.Contains(result, "already closed") {
			t.Errorf("Expected closed error, got: %s", result)
		}
	})
}
