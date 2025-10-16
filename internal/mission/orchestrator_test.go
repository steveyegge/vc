package mission

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/steveyegge/vc/internal/types"
)

var ErrNotFound = errors.New("not found")

// MockPlanner is a mock implementation of MissionPlanner for testing
type MockPlanner struct {
	plan *types.MissionPlan
	err  error
}

func (m *MockPlanner) GeneratePlan(ctx context.Context, planningCtx *types.PlanningContext) (*types.MissionPlan, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.plan, nil
}

func (m *MockPlanner) RefinePhase(ctx context.Context, phase *types.Phase, missionCtx *types.PlanningContext) ([]types.PlannedTask, error) {
	return nil, nil
}

func (m *MockPlanner) ValidatePlan(ctx context.Context, plan *types.MissionPlan) error {
	return nil
}

// MockStorage is a minimal mock for testing (only implements methods we need)
type MockStorage struct {
	issues           map[string]*types.Issue
	comments         []string
	dependencies     []*types.Dependency
	closedIssues     []string // Track closed issues for rollback testing
	nextID           int
	failOnIssueID    string // Simulate failure when creating this issue ID
	failOnDepCount   int    // Fail after N AddDependency calls (0 = never fail)
	depCallCount     int    // Track AddDependency call count
}

func NewMockStorage() *MockStorage {
	return &MockStorage{
		issues: make(map[string]*types.Issue),
		nextID: 1,
	}
}

func (m *MockStorage) GetIssue(ctx context.Context, id string) (*types.Issue, error) {
	if issue, ok := m.issues[id]; ok {
		return issue, nil
	}
	return nil, ErrNotFound
}

func (m *MockStorage) CreateIssue(ctx context.Context, issue *types.Issue, actor string) error {
	// Generate ID if not set
	if issue.ID == "" {
		issue.ID = "test-" + string(rune('0'+m.nextID))
		m.nextID++
	}
	// Simulate failure for specific issue ID
	if m.failOnIssueID != "" && issue.ID == m.failOnIssueID {
		return errors.New("simulated create issue failure")
	}
	m.issues[issue.ID] = issue
	return nil
}

func (m *MockStorage) AddComment(ctx context.Context, issueID, actor, comment string) error {
	m.comments = append(m.comments, comment)
	return nil
}

func (m *MockStorage) AddDependency(ctx context.Context, dep *types.Dependency, actor string) error {
	m.depCallCount++
	// Simulate failure after N calls
	if m.failOnDepCount > 0 && m.depCallCount >= m.failOnDepCount {
		return errors.New("simulated add dependency failure")
	}
	m.dependencies = append(m.dependencies, dep)
	return nil
}

// Stub methods to satisfy interface
func (m *MockStorage) Close() error { return nil }
func (m *MockStorage) UpdateIssue(ctx context.Context, id string, updates map[string]interface{}, actor string) error {
	return nil
}
func (m *MockStorage) CloseIssue(ctx context.Context, id string, reason string, actor string) error {
	m.closedIssues = append(m.closedIssues, id)
	// Remove from open issues
	delete(m.issues, id)
	return nil
}
func (m *MockStorage) SearchIssues(ctx context.Context, query string, filter types.IssueFilter) ([]*types.Issue, error) {
	return nil, nil
}
func (m *MockStorage) GetDependencies(ctx context.Context, issueID string) ([]*types.Issue, error) {
	return nil, nil
}
func (m *MockStorage) GetDependents(ctx context.Context, issueID string) ([]*types.Issue, error) {
	return nil, nil
}
func (m *MockStorage) GetDependencyTree(ctx context.Context, issueID string, maxDepth int) ([]*types.TreeNode, error) {
	return nil, nil
}
func (m *MockStorage) DetectCycles(ctx context.Context) ([][]*types.Issue, error) { return nil, nil }
func (m *MockStorage) RemoveDependency(ctx context.Context, issueID, dependsOnID string, actor string) error {
	return nil
}
func (m *MockStorage) AddLabel(ctx context.Context, issueID, label, actor string) error { return nil }
func (m *MockStorage) RemoveLabel(ctx context.Context, issueID, label, actor string) error {
	return nil
}
func (m *MockStorage) GetLabels(ctx context.Context, issueID string) ([]string, error) {
	return nil, nil
}
func (m *MockStorage) GetIssuesByLabel(ctx context.Context, label string) ([]*types.Issue, error) {
	return nil, nil
}
func (m *MockStorage) GetEvents(ctx context.Context, issueID string, limit int) ([]*types.Event, error) {
	return nil, nil
}
func (m *MockStorage) GetStatistics(ctx context.Context) (*types.Statistics, error) { return nil, nil }
func (m *MockStorage) RegisterInstance(ctx context.Context, instance *types.ExecutorInstance) error {
	return nil
}
func (m *MockStorage) UpdateHeartbeat(ctx context.Context, instanceID string) error { return nil }
func (m *MockStorage) GetActiveInstances(ctx context.Context) ([]*types.ExecutorInstance, error) {
	return nil, nil
}
func (m *MockStorage) CleanupStaleInstances(ctx context.Context, staleThreshold int) (int, error) {
	return 0, nil
}
func (m *MockStorage) GetReadyWork(ctx context.Context, filter types.WorkFilter) ([]*types.Issue, error) {
	return nil, nil
}
func (m *MockStorage) GetBlockedIssues(ctx context.Context) ([]*types.BlockedIssue, error) {
	return nil, nil
}
func (m *MockStorage) ClaimIssue(ctx context.Context, issueID, instanceID string) error { return nil }
func (m *MockStorage) ReleaseIssue(ctx context.Context, issueID string) error           { return nil }
func (m *MockStorage) UpdateExecutionState(ctx context.Context, issueID string, state types.ExecutionState) error {
	return nil
}
func (m *MockStorage) GetExecutionState(ctx context.Context, issueID string) (*types.IssueExecutionState, error) {
	return nil, nil
}
func (m *MockStorage) SaveCheckpoint(ctx context.Context, issueID string, checkpointData interface{}) error {
	return nil
}
func (m *MockStorage) GetCheckpoint(ctx context.Context, issueID string) (string, error) {
	return "", nil
}

func TestGenerateAndStorePlan_RequiresApproval(t *testing.T) {
	ctx := context.Background()

	// Create mock dependencies
	store := NewMockStorage()
	mockPlan := &types.MissionPlan{
		MissionID: "test-mission",
		Phases: []types.PlannedPhase{
			{
				PhaseNumber:     1,
				Title:           "Phase 1",
				Description:     "Test phase",
				Strategy:        "Test",
				Tasks:           []string{"Task 1"},
				EstimatedEffort: "1 week",
			},
		},
		Strategy:        "Test strategy",
		EstimatedEffort: "1 week",
		Confidence:      0.8,
	}
	planner := &MockPlanner{plan: mockPlan}

	orchestrator, err := NewOrchestrator(&Config{
		Store:        store,
		Planner:      planner,
		SkipApproval: false,
	})
	if err != nil {
		t.Fatalf("Failed to create orchestrator: %v", err)
	}

	// Create a mission that requires approval
	mission := &types.Mission{
		Issue: types.Issue{
			ID:        "test-mission",
			Title:     "Test Mission",
			IssueType: types.TypeEpic,
			Status:    types.StatusOpen,
		},
		ApprovalRequired: true,
	}

	planningCtx := &types.PlanningContext{
		Mission: mission,
	}

	// Generate plan
	result, err := orchestrator.GenerateAndStorePlan(ctx, mission, planningCtx)
	if err != nil {
		t.Fatalf("Failed to generate plan: %v", err)
	}

	// Verify plan requires approval
	if !result.RequiresApproval {
		t.Errorf("Expected RequiresApproval=true, got false")
	}
	if !result.PendingApproval {
		t.Errorf("Expected PendingApproval=true, got false")
	}
	if result.AutoApproved {
		t.Errorf("Expected AutoApproved=false, got true")
	}
	if result.Plan == nil {
		t.Errorf("Expected plan to be set")
	}
}

func TestGenerateAndStorePlan_AutoApprove(t *testing.T) {
	ctx := context.Background()

	// Create mock dependencies
	store := NewMockStorage()
	mockPlan := &types.MissionPlan{
		MissionID: "test-mission",
		Phases: []types.PlannedPhase{
			{
				PhaseNumber:     1,
				Title:           "Phase 1",
				Description:     "Test phase",
				Strategy:        "Test",
				Tasks:           []string{"Task 1"},
				EstimatedEffort: "1 week",
			},
		},
		Strategy:        "Test strategy",
		EstimatedEffort: "1 week",
		Confidence:      0.8,
	}
	planner := &MockPlanner{plan: mockPlan}

	orchestrator, err := NewOrchestrator(&Config{
		Store:        store,
		Planner:      planner,
		SkipApproval: false,
	})
	if err != nil {
		t.Fatalf("Failed to create orchestrator: %v", err)
	}

	// Create a mission that doesn't require approval
	mission := &types.Mission{
		Issue: types.Issue{
			ID:        "test-mission",
			Title:     "Test Mission",
			IssueType: types.TypeEpic,
			Status:    types.StatusOpen,
		},
		ApprovalRequired: false,
	}

	planningCtx := &types.PlanningContext{
		Mission: mission,
	}

	// Generate plan
	result, err := orchestrator.GenerateAndStorePlan(ctx, mission, planningCtx)
	if err != nil {
		t.Fatalf("Failed to generate plan: %v", err)
	}

	// Verify plan is auto-approved
	if result.RequiresApproval {
		t.Errorf("Expected RequiresApproval=false, got true")
	}
	if result.PendingApproval {
		t.Errorf("Expected PendingApproval=false, got true")
	}
	if !result.AutoApproved {
		t.Errorf("Expected AutoApproved=true, got false")
	}
}

func TestGenerateAndStorePlan_SkipApproval(t *testing.T) {
	ctx := context.Background()

	// Create mock dependencies
	store := NewMockStorage()
	mockPlan := &types.MissionPlan{
		MissionID: "test-mission",
		Phases: []types.PlannedPhase{
			{
				PhaseNumber:     1,
				Title:           "Phase 1",
				Description:     "Test phase",
				Strategy:        "Test",
				Tasks:           []string{"Task 1"},
				EstimatedEffort: "1 week",
			},
		},
		Strategy:        "Test strategy",
		EstimatedEffort: "1 week",
		Confidence:      0.8,
	}
	planner := &MockPlanner{plan: mockPlan}

	// Create orchestrator with SkipApproval=true
	orchestrator, err := NewOrchestrator(&Config{
		Store:        store,
		Planner:      planner,
		SkipApproval: true,
	})
	if err != nil {
		t.Fatalf("Failed to create orchestrator: %v", err)
	}

	// Create a mission that requires approval
	mission := &types.Mission{
		Issue: types.Issue{
			ID:        "test-mission",
			Title:     "Test Mission",
			IssueType: types.TypeEpic,
			Status:    types.StatusOpen,
		},
		ApprovalRequired: true,
	}

	planningCtx := &types.PlanningContext{
		Mission: mission,
	}

	// Generate plan
	result, err := orchestrator.GenerateAndStorePlan(ctx, mission, planningCtx)
	if err != nil {
		t.Fatalf("Failed to generate plan: %v", err)
	}

	// Verify plan is auto-approved despite ApprovalRequired=true
	if !result.AutoApproved {
		t.Errorf("Expected AutoApproved=true when SkipApproval=true, got false")
	}
	if result.PendingApproval {
		t.Errorf("Expected PendingApproval=false when SkipApproval=true, got true")
	}
}

func TestApprovePlan(t *testing.T) {
	ctx := context.Background()

	// Create mock dependencies
	store := NewMockStorage()
	mission := &types.Issue{
		ID:        "test-mission",
		Title:     "Test Mission",
		IssueType: types.TypeEpic,
		Status:    types.StatusOpen,
	}
	store.issues["test-mission"] = mission

	planner := &MockPlanner{}
	orchestrator, err := NewOrchestrator(&Config{
		Store:   store,
		Planner: planner,
	})
	if err != nil {
		t.Fatalf("Failed to create orchestrator: %v", err)
	}

	// Approve the plan
	err = orchestrator.ApprovePlan(ctx, "test-mission", "test-user")
	if err != nil {
		t.Fatalf("Failed to approve plan: %v", err)
	}

	// Verify comment was added
	if len(store.comments) != 1 {
		t.Errorf("Expected 1 comment, got %d", len(store.comments))
	}
}

func TestRejectPlan(t *testing.T) {
	ctx := context.Background()

	// Create mock dependencies
	store := NewMockStorage()
	mission := &types.Issue{
		ID:        "test-mission",
		Title:     "Test Mission",
		IssueType: types.TypeEpic,
		Status:    types.StatusOpen,
	}
	store.issues["test-mission"] = mission

	planner := &MockPlanner{}
	orchestrator, err := NewOrchestrator(&Config{
		Store:   store,
		Planner: planner,
	})
	if err != nil {
		t.Fatalf("Failed to create orchestrator: %v", err)
	}

	// Reject the plan
	err = orchestrator.RejectPlan(ctx, "test-mission", "test-user", "Not good enough")
	if err != nil {
		t.Fatalf("Failed to reject plan: %v", err)
	}

	// Verify comment was added
	if len(store.comments) != 1 {
		t.Errorf("Expected 1 comment, got %d", len(store.comments))
	}
}

func TestCreatePhasesFromPlan(t *testing.T) {
	ctx := context.Background()

	// Create mock dependencies
	store := NewMockStorage()
	planner := &MockPlanner{}
	orchestrator, err := NewOrchestrator(&Config{
		Store:   store,
		Planner: planner,
	})
	if err != nil {
		t.Fatalf("Failed to create orchestrator: %v", err)
	}

	// Create a plan with 2 phases
	now := time.Now()
	plan := &types.MissionPlan{
		MissionID: "mission-1",
		Phases: []types.PlannedPhase{
			{
				PhaseNumber:     1,
				Title:           "Phase 1: Foundation",
				Description:     "Build foundation",
				Strategy:        "Start simple",
				Tasks:           []string{"Task 1", "Task 2"},
				EstimatedEffort: "1 week",
			},
			{
				PhaseNumber:     2,
				Title:           "Phase 2: Features",
				Description:     "Add features",
				Strategy:        "Iterate",
				Tasks:           []string{"Task 3"},
				Dependencies:    []int{1}, // Depends on phase 1
				EstimatedEffort: "2 weeks",
			},
		},
		Strategy:        "Phased approach",
		EstimatedEffort: "3 weeks",
		Confidence:      0.8,
		GeneratedAt:     now,
	}

	// Create phases
	phaseIDs, err := orchestrator.CreatePhasesFromPlan(ctx, "mission-1", plan, "test-user")
	if err != nil {
		t.Fatalf("Failed to create phases: %v", err)
	}

	// Verify 2 phases were created
	if len(phaseIDs) != 2 {
		t.Errorf("Expected 2 phases, got %d", len(phaseIDs))
	}

	// Verify phases were stored
	if len(store.issues) != 2 {
		t.Errorf("Expected 2 issues in store, got %d", len(store.issues))
	}

	// Verify dependencies were created
	// Each phase should have:
	// - 1 parent-child dependency to mission
	// - Phase 2 should have 1 blocks dependency to Phase 1
	// Total: 2 parent-child + 1 blocks = 3 dependencies
	if len(store.dependencies) != 3 {
		t.Errorf("Expected 3 dependencies, got %d", len(store.dependencies))
	}
}

func TestMissionIsApproved(t *testing.T) {
	tests := []struct {
		name     string
		mission  *types.Mission
		expected bool
	}{
		{
			name: "approval not required",
			mission: &types.Mission{
				ApprovalRequired: false,
			},
			expected: true,
		},
		{
			name: "approval required and approved",
			mission: &types.Mission{
				ApprovalRequired: true,
				ApprovedAt:       &time.Time{},
			},
			expected: true,
		},
		{
			name: "approval required but not approved",
			mission: &types.Mission{
				ApprovalRequired: true,
				ApprovedAt:       nil,
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.mission.IsApproved()
			if result != tt.expected {
				t.Errorf("IsApproved() = %v, want %v", result, tt.expected)
			}
		})
	}
}
