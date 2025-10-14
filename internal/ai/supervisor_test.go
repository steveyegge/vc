package ai

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/steveyegge/vc/internal/types"
)

// mockStorage implements a minimal storage.Storage for testing
type mockStorage struct {
	issues       map[string]*types.Issue
	comments     []string
	dependencies []types.Dependency
	createError  error // Inject errors for testing
	depError     error
	createFunc   func(ctx context.Context, issue *types.Issue, actor string) error // Allow overriding
}

func newMockStorage() *mockStorage {
	return &mockStorage{
		issues:       make(map[string]*types.Issue),
		comments:     []string{},
		dependencies: []types.Dependency{},
	}
}

func (m *mockStorage) CreateIssue(ctx context.Context, issue *types.Issue, actor string) error {
	// Allow overriding for testing
	if m.createFunc != nil {
		return m.createFunc(ctx, issue, actor)
	}
	if m.createError != nil {
		return m.createError
	}
	// Generate a simple ID
	issue.ID = "test-" + issue.Title[:min(5, len(issue.Title))]
	m.issues[issue.ID] = issue
	return nil
}

func (m *mockStorage) AddComment(ctx context.Context, issueID, actor, comment string) error {
	m.comments = append(m.comments, comment)
	return nil
}

func (m *mockStorage) AddDependency(ctx context.Context, dep *types.Dependency, actor string) error {
	if m.depError != nil {
		return m.depError
	}
	m.dependencies = append(m.dependencies, *dep)
	return nil
}

// Stub implementations for other required methods
func (m *mockStorage) GetIssue(ctx context.Context, id string) (*types.Issue, error) {
	return m.issues[id], nil
}
func (m *mockStorage) UpdateIssue(ctx context.Context, id string, updates map[string]interface{}, actor string) error {
	return nil
}
func (m *mockStorage) CloseIssue(ctx context.Context, id string, reason string, actor string) error {
	return nil
}
func (m *mockStorage) SearchIssues(ctx context.Context, query string, filter types.IssueFilter) ([]*types.Issue, error) {
	return nil, nil
}
func (m *mockStorage) RemoveDependency(ctx context.Context, issueID, dependsOnID string, actor string) error {
	return nil
}
func (m *mockStorage) GetDependencies(ctx context.Context, issueID string) ([]*types.Issue, error) {
	return nil, nil
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
func (m *mockStorage) GetBlockedIssues(ctx context.Context) ([]*types.BlockedIssue, error) {
	return nil, nil
}
func (m *mockStorage) GetEvents(ctx context.Context, issueID string, limit int) ([]*types.Event, error) {
	return nil, nil
}
func (m *mockStorage) GetStatistics(ctx context.Context) (*types.Statistics, error) {
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

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// TestBuildAssessmentPrompt tests prompt construction
func TestBuildAssessmentPrompt(t *testing.T) {
	store := newMockStorage()
	supervisor := &Supervisor{
		store: store,
		model: "test-model",
	}

	issue := &types.Issue{
		ID:                 "test-1",
		Title:              "Add feature X",
		Description:        "Implement feature X with Y",
		Design:             "Use pattern Z",
		AcceptanceCriteria: "Feature works",
		IssueType:          types.TypeTask,
		Priority:           1,
	}

	prompt := supervisor.buildAssessmentPrompt(issue)

	// Verify prompt contains key elements
	if !strings.Contains(prompt, "test-1") {
		t.Error("Prompt should contain issue ID")
	}
	if !strings.Contains(prompt, "Add feature X") {
		t.Error("Prompt should contain title")
	}
	if !strings.Contains(prompt, "Implement feature X with Y") {
		t.Error("Prompt should contain description")
	}
	if !strings.Contains(prompt, "Use pattern Z") {
		t.Error("Prompt should contain design")
	}
	if !strings.Contains(prompt, "Feature works") {
		t.Error("Prompt should contain acceptance criteria")
	}
	if !strings.Contains(prompt, "strategy") {
		t.Error("Prompt should request strategy")
	}
	if !strings.Contains(prompt, "confidence") {
		t.Error("Prompt should request confidence")
	}
}

// TestBuildAnalysisPrompt tests analysis prompt construction
func TestBuildAnalysisPrompt(t *testing.T) {
	store := newMockStorage()
	supervisor := &Supervisor{
		store: store,
		model: "test-model",
	}

	issue := &types.Issue{
		ID:                 "test-2",
		Title:              "Fix bug Y",
		Description:        "Bug in module Y",
		AcceptanceCriteria: "Bug is fixed",
	}

	agentOutput := "Fixed the bug by changing line 42"

	tests := []struct {
		name    string
		success bool
		want    string
	}{
		{"successful execution", true, "succeeded"},
		{"failed execution", false, "failed"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			prompt := supervisor.buildAnalysisPrompt(issue, agentOutput, tt.success)

			if !strings.Contains(prompt, tt.want) {
				t.Errorf("Prompt should contain status '%s'", tt.want)
			}
			if !strings.Contains(prompt, "test-2") {
				t.Error("Prompt should contain issue ID")
			}
			if !strings.Contains(prompt, "Fixed the bug") {
				t.Error("Prompt should contain agent output")
			}
			if !strings.Contains(prompt, "completed") {
				t.Error("Prompt should ask about completion status")
			}
			if !strings.Contains(prompt, "discovered_issues") {
				t.Error("Prompt should ask about discovered issues")
			}
		})
	}
}

// TestCreateDiscoveredIssues tests issue creation from AI analysis
func TestCreateDiscoveredIssues(t *testing.T) {
	store := newMockStorage()
	supervisor := &Supervisor{
		store: store,
		model: "test-model",
	}

	parentIssue := &types.Issue{
		ID:    "parent-1",
		Title: "Parent task",
	}

	tests := []struct {
		name          string
		discovered    []DiscoveredIssue
		wantCount     int
		wantTypes     []types.IssueType
		wantPriorities []int
	}{
		{
			name: "single bug",
			discovered: []DiscoveredIssue{
				{
					Title:       "Found a bug",
					Description: "Bug description",
					Type:        "bug",
					Priority:    "P0",
				},
			},
			wantCount:      1,
			wantTypes:      []types.IssueType{types.TypeBug},
			wantPriorities: []int{0},
		},
		{
			name: "multiple issues with different types",
			discovered: []DiscoveredIssue{
				{
					Title:       "Add test",
					Description: "Missing test",
					Type:        "task",
					Priority:    "P1",
				},
				{
					Title:       "Refactor code",
					Description: "Code needs cleanup",
					Type:        "enhancement",
					Priority:    "P2",
				},
				{
					Title:       "Fix typo",
					Description: "Documentation typo",
					Type:        "chore",
					Priority:    "P3",
				},
			},
			wantCount:      3,
			wantTypes:      []types.IssueType{types.TypeTask, types.TypeFeature, types.TypeChore},
			wantPriorities: []int{1, 2, 3},
		},
		{
			name: "default values when type/priority unknown",
			discovered: []DiscoveredIssue{
				{
					Title:       "Unknown thing",
					Description: "Something",
					Type:        "unknown-type",
					Priority:    "P999",
				},
			},
			wantCount:      1,
			wantTypes:      []types.IssueType{types.TypeTask}, // default
			wantPriorities: []int{2},                          // default P2
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Reset store
			store.issues = make(map[string]*types.Issue)
			store.dependencies = []types.Dependency{}

			ctx := context.Background()
			createdIDs, err := supervisor.CreateDiscoveredIssues(ctx, parentIssue, tt.discovered)

			if err != nil {
				t.Fatalf("CreateDiscoveredIssues failed: %v", err)
			}

			if len(createdIDs) != tt.wantCount {
				t.Errorf("Created %d issues, want %d", len(createdIDs), tt.wantCount)
			}

			// Verify created issues have correct types and priorities
			for i, id := range createdIDs {
				issue := store.issues[id]
				if issue == nil {
					t.Fatalf("Issue %s not found in store", id)
				}

				if issue.IssueType != tt.wantTypes[i] {
					t.Errorf("Issue %d: got type %s, want %s", i, issue.IssueType, tt.wantTypes[i])
				}

				if issue.Priority != tt.wantPriorities[i] {
					t.Errorf("Issue %d: got priority %d, want %d", i, issue.Priority, tt.wantPriorities[i])
				}

				if !strings.Contains(issue.Description, "parent-1") {
					t.Errorf("Issue %d: description should mention parent issue", i)
				}

				if issue.Assignee != "ai-supervisor" {
					t.Errorf("Issue %d: got assignee %s, want ai-supervisor", i, issue.Assignee)
				}
			}

			// Verify dependencies were created
			if len(store.dependencies) != tt.wantCount {
				t.Errorf("Created %d dependencies, want %d", len(store.dependencies), tt.wantCount)
			}

			for _, dep := range store.dependencies {
				if dep.DependsOnID != parentIssue.ID {
					t.Errorf("Dependency should reference parent issue %s, got %s", parentIssue.ID, dep.DependsOnID)
				}
				if dep.Type != types.DepDiscoveredFrom {
					t.Errorf("Dependency type should be %s, got %s", types.DepDiscoveredFrom, dep.Type)
				}
			}
		})
	}
}

// TestCreateDiscoveredIssues_PartialFailure tests behavior when issue creation fails mid-way
func TestCreateDiscoveredIssues_PartialFailure(t *testing.T) {
	store := newMockStorage()
	supervisor := &Supervisor{
		store: store,
		model: "test-model",
	}

	parentIssue := &types.Issue{
		ID:    "parent-1",
		Title: "Parent task",
	}

	discovered := []DiscoveredIssue{
		{Title: "Issue 1", Description: "First", Type: "task", Priority: "P1"},
		{Title: "Issue 2", Description: "Second", Type: "bug", Priority: "P0"},
		{Title: "Issue 3", Description: "Third", Type: "task", Priority: "P2"},
	}

	// First two succeed, third fails
	callCount := 0
	store.createFunc = func(ctx context.Context, issue *types.Issue, actor string) error {
		callCount++
		if callCount > 2 {
			return fmt.Errorf("simulated creation failure")
		}
		// Default creation logic with unique ID
		issue.ID = fmt.Sprintf("test-%d", callCount)
		store.issues[issue.ID] = issue
		return nil
	}

	ctx := context.Background()
	createdIDs, err := supervisor.CreateDiscoveredIssues(ctx, parentIssue, discovered)

	// Should return error but include IDs of successfully created issues
	if err == nil {
		t.Error("Expected error when issue creation fails")
	}

	if len(createdIDs) != 2 {
		t.Errorf("Should have created 2 issues before failing, got %d", len(createdIDs))
	}

	// Verify the two successful issues exist
	if len(store.issues) != 2 {
		t.Errorf("Store should have 2 issues, got %d", len(store.issues))
	}
}

// TestTruncateString tests the truncation utility
func TestTruncateString(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		maxLen int
		want   string
	}{
		{
			name:   "string shorter than max",
			input:  "short",
			maxLen: 10,
			want:   "short",
		},
		{
			name:   "string exactly max length",
			input:  "exact",
			maxLen: 5,
			want:   "exact",
		},
		{
			name:   "string longer than max - takes last N chars",
			input:  "This is a long string",
			maxLen: 10,
			want:   "ong string",
		},
		{
			name:   "empty string",
			input:  "",
			maxLen: 10,
			want:   "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := truncateString(tt.input, tt.maxLen)
			if got != tt.want {
				t.Errorf("truncateString() = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestPriorityMapping tests all priority mappings
func TestPriorityMapping(t *testing.T) {
	tests := []struct {
		input string
		want  int
	}{
		{"P0", 0},
		{"P1", 1},
		{"P2", 2},
		{"P3", 3},
		{"P4", 2},       // unknown, defaults to P2
		{"invalid", 2},  // unknown, defaults to P2
		{"", 2},         // empty, defaults to P2
	}

	store := newMockStorage()
	supervisor := &Supervisor{
		store: store,
		model: "test-model",
	}

	parentIssue := &types.Issue{ID: "parent", Title: "Parent"}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			store.issues = make(map[string]*types.Issue)

			discovered := []DiscoveredIssue{
				{
					Title:       "Test",
					Description: "Test",
					Type:        "task",
					Priority:    tt.input,
				},
			}

			ctx := context.Background()
			createdIDs, err := supervisor.CreateDiscoveredIssues(ctx, parentIssue, discovered)

			if err != nil {
				t.Fatalf("CreateDiscoveredIssues failed: %v", err)
			}

			if len(createdIDs) != 1 {
				t.Fatal("Should create 1 issue")
			}

			issue := store.issues[createdIDs[0]]
			if issue.Priority != tt.want {
				t.Errorf("Priority %s mapped to %d, want %d", tt.input, issue.Priority, tt.want)
			}
		})
	}
}

// TestTypeMapping tests all type mappings
func TestTypeMapping(t *testing.T) {
	tests := []struct {
		input string
		want  types.IssueType
	}{
		{"bug", types.TypeBug},
		{"task", types.TypeTask},
		{"feature", types.TypeFeature},
		{"enhancement", types.TypeFeature}, // maps to feature
		{"epic", types.TypeEpic},
		{"chore", types.TypeChore},
		{"unknown", types.TypeTask},  // unknown, defaults to task
		{"", types.TypeTask},         // empty, defaults to task
	}

	store := newMockStorage()
	supervisor := &Supervisor{
		store: store,
		model: "test-model",
	}

	parentIssue := &types.Issue{ID: "parent", Title: "Parent"}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			store.issues = make(map[string]*types.Issue)

			discovered := []DiscoveredIssue{
				{
					Title:       "Test",
					Description: "Test",
					Type:        tt.input,
					Priority:    "P1",
				},
			}

			ctx := context.Background()
			createdIDs, err := supervisor.CreateDiscoveredIssues(ctx, parentIssue, discovered)

			if err != nil {
				t.Fatalf("CreateDiscoveredIssues failed: %v", err)
			}

			if len(createdIDs) != 1 {
				t.Fatal("Should create 1 issue")
			}

			issue := store.issues[createdIDs[0]]
			if issue.IssueType != tt.want {
				t.Errorf("Type %s mapped to %s, want %s", tt.input, issue.IssueType, tt.want)
			}
		})
	}
}
