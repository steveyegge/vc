package ai

import (
	"context"
	"testing"

	"github.com/steveyegge/vc/internal/types"
)

// mockStore implements IssueStore interface for testing
type mockStore struct {
	createdIssues    []*types.Issue
	dependencies     []*types.Dependency
	labels           map[string][]string // issueID -> labels
	updates          map[string]map[string]interface{} // issueID -> updates
	createError      error
	dependencyError  error
	labelError       error
	updateError      error
}

func newMockStore() *mockStore {
	return &mockStore{
		labels:  make(map[string][]string),
		updates: make(map[string]map[string]interface{}),
	}
}

func (m *mockStore) CreateIssue(ctx context.Context, issue *types.Issue, actor string) error {
	if m.createError != nil {
		return m.createError
	}
	// Simulate ID assignment
	if issue.ID == "" {
		issue.ID = "test-child-1"
	}
	m.createdIssues = append(m.createdIssues, issue)
	return nil
}

func (m *mockStore) AddDependency(ctx context.Context, dep *types.Dependency, actor string) error {
	if m.dependencyError != nil {
		return m.dependencyError
	}
	m.dependencies = append(m.dependencies, dep)
	return nil
}

func (m *mockStore) AddLabel(ctx context.Context, issueID, label, actor string) error {
	if m.labelError != nil {
		return m.labelError
	}
	m.labels[issueID] = append(m.labels[issueID], label)
	return nil
}

func (m *mockStore) UpdateIssue(ctx context.Context, issueID string, updates map[string]interface{}, actor string) error {
	if m.updateError != nil {
		return m.updateError
	}
	m.updates[issueID] = updates
	return nil
}

func TestDecomposeIssue(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name          string
		parentIssue   *types.Issue
		plan          *DecompositionPlan
		wantErr       bool
		wantChildCount int
	}{
		{
			name: "successful decomposition with 2 children",
			parentIssue: &types.Issue{
				ID:    "test-parent",
				Title: "Parent issue",
			},
			plan: &DecompositionPlan{
				Reasoning: "Multiple independent test failures",
				ChildIssues: []ChildIssue{
					{
						Title:              "Fix TestA",
						Description:        "Fix test A",
						AcceptanceCriteria: "Test passes",
						Priority:           2,
						EstimatedMinutes:   30,
					},
					{
						Title:              "Fix TestB",
						Description:        "Fix test B",
						AcceptanceCriteria: "Test passes",
						Priority:           2,
						EstimatedMinutes:   30,
					},
				},
			},
			wantErr:       false,
			wantChildCount: 2,
		},
		{
			name: "nil plan",
			parentIssue: &types.Issue{
				ID:    "test-parent",
				Title: "Parent issue",
			},
			plan:          nil,
			wantErr:       true,
			wantChildCount: 0,
		},
		{
			name: "empty plan",
			parentIssue: &types.Issue{
				ID:    "test-parent",
				Title: "Parent issue",
			},
			plan: &DecompositionPlan{
				Reasoning:   "Empty",
				ChildIssues: []ChildIssue{},
			},
			wantErr:       true,
			wantChildCount: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := newMockStore()
			supervisor := &Supervisor{}

			childIDs, err := supervisor.DecomposeIssue(ctx, store, tt.parentIssue, tt.plan)

			if (err != nil) != tt.wantErr {
				t.Errorf("DecomposeIssue() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if len(childIDs) != tt.wantChildCount {
				t.Errorf("DecomposeIssue() created %d children, want %d", len(childIDs), tt.wantChildCount)
			}

			if !tt.wantErr {
				// Check created issues
				if len(store.createdIssues) != tt.wantChildCount {
					t.Errorf("store has %d created issues, want %d", len(store.createdIssues), tt.wantChildCount)
				}

				// Check all children are tasks
				for _, child := range store.createdIssues {
					if child.IssueType != types.TypeTask {
						t.Errorf("child issue type = %v, want %v", child.IssueType, types.TypeTask)
					}
					if child.Status != types.StatusOpen {
						t.Errorf("child issue status = %v, want %v", child.Status, types.StatusOpen)
					}
				}

				// Check dependencies created
				if len(store.dependencies) != tt.wantChildCount {
					t.Errorf("store has %d dependencies, want %d", len(store.dependencies), tt.wantChildCount)
				}

				// Check labels applied
				parentLabels := store.labels[tt.parentIssue.ID]
				hasDecomposedLabel := false
				for _, label := range parentLabels {
					if label == types.LabelDecomposed {
						hasDecomposedLabel = true
						break
					}
				}
				if !hasDecomposedLabel {
					t.Errorf("parent issue missing %s label", types.LabelDecomposed)
				}

				// Check parent notes updated
				if _, ok := store.updates[tt.parentIssue.ID]; !ok {
					t.Errorf("parent issue notes not updated")
				}
			}
		})
	}
}

func TestDecomposeIssue_EstimatedMinutes(t *testing.T) {
	ctx := context.Background()
	store := newMockStore()
	supervisor := &Supervisor{}

	parentIssue := &types.Issue{
		ID:    "test-parent",
		Title: "Parent issue",
	}

	plan := &DecompositionPlan{
		Reasoning: "Test with estimated minutes",
		ChildIssues: []ChildIssue{
			{
				Title:              "Child with estimate",
				Description:        "Description",
				AcceptanceCriteria: "Done",
				Priority:           2,
				EstimatedMinutes:   45,
			},
			{
				Title:              "Child without estimate",
				Description:        "Description",
				AcceptanceCriteria: "Done",
				Priority:           2,
				EstimatedMinutes:   0, // Zero means no estimate
			},
		},
	}

	_, err := supervisor.DecomposeIssue(ctx, store, parentIssue, plan)
	if err != nil {
		t.Fatalf("DecomposeIssue() failed: %v", err)
	}

	// Check first child has estimate
	if store.createdIssues[0].EstimatedMinutes == nil {
		t.Errorf("first child should have estimated minutes")
	} else if *store.createdIssues[0].EstimatedMinutes != 45 {
		t.Errorf("first child estimated_minutes = %d, want 45", *store.createdIssues[0].EstimatedMinutes)
	}

	// Check second child has no estimate (nil)
	if store.createdIssues[1].EstimatedMinutes != nil {
		t.Errorf("second child should have no estimated minutes (nil), got %v", *store.createdIssues[1].EstimatedMinutes)
	}
}
