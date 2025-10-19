package ai

import (
	"context"
	"strings"
	"testing"

	"github.com/steveyegge/vc/internal/storage/sqlite"
	"github.com/steveyegge/vc/internal/types"
)

func TestAssessCompletion(t *testing.T) {
	// Skip if ANTHROPIC_API_KEY not set
	supervisor, err := createTestSupervisor(t)
	if err != nil {
		t.Skipf("Skipping test: %v", err)
	}

	tests := []struct {
		name              string
		issue             *types.Issue
		children          []*types.Issue
		expectShouldClose bool
	}{
		{
			name: "epic with all children closed",
			issue: &types.Issue{
				ID:                 "vc-test-epic-1",
				Title:              "Implement authentication",
				Description:        "Add user authentication to the system",
				IssueType:          types.TypeEpic,
				Status:             types.StatusOpen,
				AcceptanceCriteria: "Users can log in and log out securely",
			},
			children: []*types.Issue{
				{
					ID:          "vc-test-1",
					Title:       "Add login endpoint",
					Description: "Create /login endpoint",
					Status:      types.StatusClosed,
					IssueType:   types.TypeTask,
				},
				{
					ID:          "vc-test-2",
					Title:       "Add logout endpoint",
					Description: "Create /logout endpoint",
					Status:      types.StatusClosed,
					IssueType:   types.TypeTask,
				},
			},
			expectShouldClose: true,
		},
		{
			name: "epic with some children open",
			issue: &types.Issue{
				ID:                 "vc-test-epic-2",
				Title:              "Build dashboard",
				Description:        "Create user dashboard with analytics",
				IssueType:          types.TypeEpic,
				Status:             types.StatusOpen,
				AcceptanceCriteria: "Dashboard shows user activity metrics",
			},
			children: []*types.Issue{
				{
					ID:          "vc-test-3",
					Title:       "Create dashboard page",
					Description: "Build React dashboard component",
					Status:      types.StatusClosed,
					IssueType:   types.TypeTask,
				},
				{
					ID:          "vc-test-4",
					Title:       "Add analytics API",
					Description: "Endpoint for analytics data",
					Status:      types.StatusOpen,
					IssueType:   types.TypeTask,
				},
			},
			expectShouldClose: false,
		},
		{
			name: "mission with all phases complete",
			issue: &types.Issue{
				ID:                 "vc-test-mission-1",
				Title:              "Launch new feature",
				Description:        "Complete rollout of new authentication system",
				IssueType:          types.TypeEpic,
				IssueSubtype:       types.SubtypeMission,
				Status:             types.StatusOpen,
				AcceptanceCriteria: "Feature is live and tested in production",
			},
			children: []*types.Issue{
				{
					ID:           "vc-test-phase-1",
					Title:        "Phase 1: Development",
					Description:  "Build core features",
					Status:       types.StatusClosed,
					IssueType:    types.TypeEpic,
					IssueSubtype: types.SubtypePhase,
				},
				{
					ID:           "vc-test-phase-2",
					Title:        "Phase 2: Testing",
					Description:  "QA and bug fixes",
					Status:       types.StatusClosed,
					IssueType:    types.TypeEpic,
					IssueSubtype: types.SubtypePhase,
				},
				{
					ID:           "vc-test-phase-3",
					Title:        "Phase 3: Deployment",
					Description:  "Deploy to production",
					Status:       types.StatusClosed,
					IssueType:    types.TypeEpic,
					IssueSubtype: types.SubtypePhase,
				},
			},
			expectShouldClose: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()

			assessment, err := supervisor.AssessCompletion(ctx, tt.issue, tt.children)
			if err != nil {
				t.Fatalf("AssessCompletion failed: %v", err)
			}

			// Verify should_close matches expectation
			if assessment.ShouldClose != tt.expectShouldClose {
				t.Errorf("Expected should_close=%v, got %v\nReasoning: %s",
					tt.expectShouldClose, assessment.ShouldClose, assessment.Reasoning)
			}

			// Verify confidence is in valid range
			if assessment.Confidence < 0 || assessment.Confidence > 1 {
				t.Errorf("Invalid confidence: %f (must be 0-1)", assessment.Confidence)
			}

			// Verify reasoning is present
			if assessment.Reasoning == "" {
				t.Error("Assessment must include reasoning")
			}

			t.Logf("Assessment for %s:", tt.issue.ID)
			t.Logf("  Should Close: %v", assessment.ShouldClose)
			t.Logf("  Confidence: %.2f", assessment.Confidence)
			t.Logf("  Reasoning: %s", assessment.Reasoning)
			if len(assessment.Caveats) > 0 {
				t.Logf("  Caveats: %v", assessment.Caveats)
			}
		})
	}
}

func TestAssessCompletion_EmptyChildren(t *testing.T) {
	// Skip if ANTHROPIC_API_KEY not set
	supervisor, err := createTestSupervisor(t)
	if err != nil {
		t.Skipf("Skipping test: %v", err)
	}

	epic := &types.Issue{
		ID:                 "vc-test-empty",
		Title:              "Epic with no children",
		Description:        "This epic has no child issues",
		IssueType:          types.TypeEpic,
		Status:             types.StatusOpen,
		AcceptanceCriteria: "Work is complete",
	}

	ctx := context.Background()
	assessment, err := supervisor.AssessCompletion(ctx, epic, []*types.Issue{})
	if err != nil {
		t.Fatalf("AssessCompletion failed: %v", err)
	}

	// Empty children should probably not close the epic
	// (AI will decide, but let's verify it doesn't crash)
	t.Logf("Assessment for epic with no children:")
	t.Logf("  Should Close: %v", assessment.ShouldClose)
	t.Logf("  Reasoning: %s", assessment.Reasoning)
}

func TestAssessCompletion_ErrorHandling(t *testing.T) {
	// Create supervisor with invalid API key to force errors
	tmpDB := t.TempDir() + "/test.db"
	store, err := sqlite.New(tmpDB)
	if err != nil {
		t.Fatalf("Failed to create test store: %v", err)
	}
	t.Cleanup(func() { store.Close() })

	cfg := &Config{
		Store:  store,
		APIKey: "invalid-key-should-fail",
		Retry:  DefaultRetryConfig(),
	}

	supervisor, err := NewSupervisor(cfg)
	if err != nil {
		t.Fatalf("Failed to create supervisor: %v", err)
	}

	epic := &types.Issue{
		ID:                 "vc-test-error",
		Title:              "Error handling test",
		Description:        "Testing error handling",
		IssueType:          types.TypeEpic,
		Status:             types.StatusOpen,
		AcceptanceCriteria: "Test complete",
	}

	children := []*types.Issue{
		{
			ID:        "vc-test-child",
			Title:     "Child task",
			Status:    types.StatusClosed,
			IssueType: types.TypeTask,
		},
	}

	ctx := context.Background()
	_, err = supervisor.AssessCompletion(ctx, epic, children)

	// Should return an error with invalid API key
	if err == nil {
		t.Error("Expected error with invalid API key, got nil")
	}

	// Error should mention authentication or API key
	if !strings.Contains(err.Error(), "authentication_error") &&
		!strings.Contains(err.Error(), "invalid") {
		t.Errorf("Error should mention authentication issue, got: %v", err)
	}
}

func TestAssessCompletion_ObjectivesFocus(t *testing.T) {
	// Skip if ANTHROPIC_API_KEY not set
	supervisor, err := createTestSupervisor(t)
	if err != nil {
		t.Skipf("Skipping test: %v", err)
	}

	// Test case where all children are closed, but objectives may not be met
	epic := &types.Issue{
		ID:          "vc-test-objectives",
		Title:       "Improve performance",
		Description: "Optimize database queries to reduce latency",
		IssueType:   types.TypeEpic,
		Status:      types.StatusOpen,
		AcceptanceCriteria: "Query latency reduced by 50% and handles 10k req/s\n" +
			"All database indexes are optimized\n" +
			"No degradation in accuracy",
	}

	children := []*types.Issue{
		{
			ID:          "vc-test-child-1",
			Title:       "Add database index",
			Description: "Add index to users table",
			Status:      types.StatusClosed,
			IssueType:   types.TypeTask,
		},
		{
			ID:          "vc-test-child-2",
			Title:       "Profile slow queries",
			Description: "Use profiler to identify slow queries",
			Status:      types.StatusClosed,
			IssueType:   types.TypeTask,
		},
	}

	ctx := context.Background()
	assessment, err := supervisor.AssessCompletion(ctx, epic, children)
	if err != nil {
		t.Fatalf("AssessCompletion failed: %v", err)
	}

	// The AI should recognize that while tasks are closed,
	// the acceptance criteria (50% latency reduction, 10k req/s)
	// may not be met without verification
	t.Logf("Assessment for objectives-focused epic:")
	t.Logf("  Should Close: %v", assessment.ShouldClose)
	t.Logf("  Confidence: %.2f", assessment.Confidence)
	t.Logf("  Reasoning: %s", assessment.Reasoning)

	// If AI decides to close, confidence should be lower due to lack of verification
	if assessment.ShouldClose && assessment.Confidence > 0.8 {
		t.Logf("Warning: High confidence despite unverified acceptance criteria")
	}

	// Caveats should mention verification if present
	if len(assessment.Caveats) > 0 {
		t.Logf("  Caveats: %v", assessment.Caveats)
	}
}
