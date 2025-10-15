package ai

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/steveyegge/vc/internal/types"
)

func TestValidatePlan(t *testing.T) {
	tests := []struct {
		name    string
		plan    *types.MissionPlan
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid plan",
			plan: &types.MissionPlan{
				MissionID: "vc-1",
				Phases: []types.PlannedPhase{
					{
						PhaseNumber:     1,
						Title:           "Foundation",
						Description:     "Build foundation",
						Strategy:        "Start simple",
						Tasks:           []string{"Task 1", "Task 2"},
						EstimatedEffort: "1 week",
					},
					{
						PhaseNumber:     2,
						Title:           "Features",
						Description:     "Add features",
						Strategy:        "Iterate",
						Tasks:           []string{"Task 3"},
						Dependencies:    []int{1},
						EstimatedEffort: "2 weeks",
					},
				},
				Strategy:        "Phased approach",
				EstimatedEffort: "3 weeks",
				Confidence:      0.8,
			},
			wantErr: false,
		},
		{
			name: "plan with no phases",
			plan: &types.MissionPlan{
				MissionID:       "vc-1",
				Phases:          []types.PlannedPhase{},
				Strategy:        "Test",
				EstimatedEffort: "1 week",
				Confidence:      0.8,
			},
			wantErr: true,
			errMsg:  "at least one phase",
		},
		{
			name: "plan with too many phases",
			plan: &types.MissionPlan{
				MissionID:       "vc-1",
				Phases:          makePhases(20),
				Strategy:        "Test",
				EstimatedEffort: "1 year",
				Confidence:      0.5,
			},
			wantErr: true,
			errMsg:  "too many phases",
		},
		{
			name: "phase with no tasks",
			plan: &types.MissionPlan{
				MissionID: "vc-1",
				Phases: []types.PlannedPhase{
					{
						PhaseNumber:     1,
						Title:           "Empty",
						Description:     "Test",
						Strategy:        "Test",
						Tasks:           []string{},
						EstimatedEffort: "1 week",
					},
				},
				Strategy:        "Test",
				EstimatedEffort: "1 week",
				Confidence:      0.8,
			},
			wantErr: true,
			errMsg:  "at least one task",
		},
		{
			name: "phase with too many tasks",
			plan: &types.MissionPlan{
				MissionID: "vc-1",
				Phases: []types.PlannedPhase{
					{
						PhaseNumber:     1,
						Title:           "Too many",
						Description:     "Test",
						Strategy:        "Test",
						Tasks:           makeTasks(60),
						EstimatedEffort: "1 week",
					},
				},
				Strategy:        "Test",
				EstimatedEffort: "1 week",
				Confidence:      0.8,
			},
			wantErr: true,
			errMsg:  "too many tasks",
		},
		{
			name: "circular dependency (self)",
			plan: &types.MissionPlan{
				MissionID: "vc-1",
				Phases: []types.PlannedPhase{
					{
						PhaseNumber:     1,
						Title:           "Self-dependent",
						Description:     "Test",
						Strategy:        "Test",
						Tasks:           []string{"Task 1"},
						Dependencies:    []int{1},
						EstimatedEffort: "1 week",
					},
				},
				Strategy:        "Test",
				EstimatedEffort: "1 week",
				Confidence:      0.8,
			},
			wantErr: true,
			errMsg:  "cannot depend on",
		},
		{
			name: "circular dependency (cycle)",
			plan: &types.MissionPlan{
				MissionID: "vc-1",
				Phases: []types.PlannedPhase{
					{
						PhaseNumber:     1,
						Title:           "Phase 1",
						Description:     "Test",
						Strategy:        "Test",
						Tasks:           []string{"Task 1"},
						Dependencies:    []int{2},
						EstimatedEffort: "1 week",
					},
					{
						PhaseNumber:     2,
						Title:           "Phase 2",
						Description:     "Test",
						Strategy:        "Test",
						Tasks:           []string{"Task 2"},
						Dependencies:    []int{1},
						EstimatedEffort: "1 week",
					},
				},
				Strategy:        "Test",
				EstimatedEffort: "2 weeks",
				Confidence:      0.8,
			},
			wantErr: true,
			errMsg:  "earlier phases",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a mock supervisor (we only need ValidatePlan which doesn't use fields)
			s := &Supervisor{}
			err := s.ValidatePlan(nil, tt.plan)

			if (err != nil) != tt.wantErr {
				t.Errorf("ValidatePlan() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if tt.wantErr && tt.errMsg != "" {
				if err == nil || !strings.Contains(err.Error(), tt.errMsg) {
					t.Errorf("ValidatePlan() error = %v, want error containing %q", err, tt.errMsg)
				}
			}
		})
	}
}

func TestCheckCircularDependencies(t *testing.T) {
	tests := []struct {
		name    string
		phases  []types.PlannedPhase
		wantErr bool
	}{
		{
			name: "no dependencies",
			phases: []types.PlannedPhase{
				{PhaseNumber: 1, Title: "P1", Description: "Test", Strategy: "Test", Tasks: []string{"T1"}, EstimatedEffort: "1w"},
				{PhaseNumber: 2, Title: "P2", Description: "Test", Strategy: "Test", Tasks: []string{"T2"}, EstimatedEffort: "1w"},
			},
			wantErr: false,
		},
		{
			name: "valid linear dependencies",
			phases: []types.PlannedPhase{
				{PhaseNumber: 1, Title: "P1", Description: "Test", Strategy: "Test", Tasks: []string{"T1"}, EstimatedEffort: "1w"},
				{PhaseNumber: 2, Title: "P2", Description: "Test", Strategy: "Test", Tasks: []string{"T2"}, Dependencies: []int{1}, EstimatedEffort: "1w"},
				{PhaseNumber: 3, Title: "P3", Description: "Test", Strategy: "Test", Tasks: []string{"T3"}, Dependencies: []int{2}, EstimatedEffort: "1w"},
			},
			wantErr: false,
		},
		{
			name: "valid diamond dependencies",
			phases: []types.PlannedPhase{
				{PhaseNumber: 1, Title: "P1", Description: "Test", Strategy: "Test", Tasks: []string{"T1"}, EstimatedEffort: "1w"},
				{PhaseNumber: 2, Title: "P2", Description: "Test", Strategy: "Test", Tasks: []string{"T2"}, Dependencies: []int{1}, EstimatedEffort: "1w"},
				{PhaseNumber: 3, Title: "P3", Description: "Test", Strategy: "Test", Tasks: []string{"T3"}, Dependencies: []int{1}, EstimatedEffort: "1w"},
				{PhaseNumber: 4, Title: "P4", Description: "Test", Strategy: "Test", Tasks: []string{"T4"}, Dependencies: []int{2, 3}, EstimatedEffort: "1w"},
			},
			wantErr: false,
		},
		{
			name: "self-dependency cycle",
			phases: []types.PlannedPhase{
				{PhaseNumber: 1, Title: "P1", Description: "Test", Strategy: "Test", Tasks: []string{"T1"}, Dependencies: []int{1}, EstimatedEffort: "1w"},
			},
			wantErr: true,
		},
		{
			name: "two-phase cycle",
			phases: []types.PlannedPhase{
				{PhaseNumber: 1, Title: "P1", Description: "Test", Strategy: "Test", Tasks: []string{"T1"}, Dependencies: []int{2}, EstimatedEffort: "1w"},
				{PhaseNumber: 2, Title: "P2", Description: "Test", Strategy: "Test", Tasks: []string{"T2"}, Dependencies: []int{1}, EstimatedEffort: "1w"},
			},
			wantErr: true,
		},
		{
			name: "three-phase cycle",
			phases: []types.PlannedPhase{
				{PhaseNumber: 1, Title: "P1", Description: "Test", Strategy: "Test", Tasks: []string{"T1"}, Dependencies: []int{3}, EstimatedEffort: "1w"},
				{PhaseNumber: 2, Title: "P2", Description: "Test", Strategy: "Test", Tasks: []string{"T2"}, Dependencies: []int{1}, EstimatedEffort: "1w"},
				{PhaseNumber: 3, Title: "P3", Description: "Test", Strategy: "Test", Tasks: []string{"T3"}, Dependencies: []int{2}, EstimatedEffort: "1w"},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := checkCircularDependencies(tt.phases)
			if (err != nil) != tt.wantErr {
				t.Errorf("checkCircularDependencies() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestBuildPlanningPrompt(t *testing.T) {
	now := time.Now()
	mission := &types.Mission{
		Issue: types.Issue{
			ID:          "vc-100",
			Title:       "Implement feature X",
			Description: "Build feature X from scratch",
			IssueType:   types.TypeEpic,
			Status:      types.StatusOpen,
			Priority:    0,
			CreatedAt:   now,
			UpdatedAt:   now,
		},
		Goal:       "Build a working feature X",
		Context:    "This is a critical feature",
		PhaseCount: 0,
	}

	tests := []struct {
		name           string
		ctx            *types.PlanningContext
		wantInPrompt   []string
		wantNotInPrompt []string
	}{
		{
			name: "basic mission",
			ctx: &types.PlanningContext{
				Mission: mission,
			},
			wantInPrompt: []string{
				"vc-100",
				"Implement feature X",
				"Build a working feature X",
				"Build feature X from scratch",
				"THREE-TIER WORKFLOW",
				"GENERATE A JSON PLAN",
			},
		},
		{
			name: "mission with codebase info",
			ctx: &types.PlanningContext{
				Mission:      mission,
				CodebaseInfo: "Go project with microservices",
			},
			wantInPrompt: []string{
				"Codebase Context",
				"Go project with microservices",
			},
		},
		{
			name: "mission with constraints",
			ctx: &types.PlanningContext{
				Mission:     mission,
				Constraints: []string{"No breaking changes", "Must be backward compatible"},
			},
			wantInPrompt: []string{
				"Constraints:",
				"No breaking changes",
				"Must be backward compatible",
			},
		},
		{
			name: "mission with failed attempts",
			ctx: &types.PlanningContext{
				Mission:        mission,
				FailedAttempts: 2,
			},
			wantInPrompt: []string{
				"attempt 3",
				"Previous plans had issues",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &Supervisor{}
			prompt := s.buildPlanningPrompt(tt.ctx)

			for _, want := range tt.wantInPrompt {
				if !strings.Contains(prompt, want) {
					t.Errorf("buildPlanningPrompt() missing expected string: %q", want)
				}
			}

			for _, notWant := range tt.wantNotInPrompt {
				if strings.Contains(prompt, notWant) {
					t.Errorf("buildPlanningPrompt() contains unexpected string: %q", notWant)
				}
			}
		})
	}
}

func TestBuildRefinementPrompt(t *testing.T) {
	now := time.Now()
	phase := &types.Phase{
		Issue: types.Issue{
			ID:          "vc-101",
			Title:       "Phase 1: Foundation",
			Description: "Build the foundational components",
			IssueType:   types.TypeEpic,
			Status:      types.StatusOpen,
			Priority:    0,
			CreatedAt:   now,
			UpdatedAt:   now,
		},
		MissionID:   "vc-100",
		PhaseNumber: 1,
		Strategy:    "Start with core data structures",
	}

	tests := []struct {
		name         string
		phase        *types.Phase
		missionCtx   *types.PlanningContext
		wantInPrompt []string
	}{
		{
			name:  "basic phase",
			phase: phase,
			wantInPrompt: []string{
				"Phase 1: Foundation",
				"Start with core data structures",
				"Build the foundational components",
				"5-20 granular tasks",
				"30 minutes to 2 hours",
			},
		},
		{
			name:  "phase with mission context",
			phase: phase,
			missionCtx: &types.PlanningContext{
				Mission: &types.Mission{
					Issue: types.Issue{
						ID:        "vc-100",
						Title:     "Big Mission",
						CreatedAt: now,
						UpdatedAt: now,
						IssueType: types.TypeEpic,
						Status:    types.StatusOpen,
					},
					Goal: "Accomplish something big",
				},
			},
			wantInPrompt: []string{
				"MISSION CONTEXT",
				"Big Mission",
				"Accomplish something big",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &Supervisor{}
			prompt := s.buildRefinementPrompt(tt.phase, tt.missionCtx)

			for _, want := range tt.wantInPrompt {
				if !strings.Contains(prompt, want) {
					t.Errorf("buildRefinementPrompt() missing expected string: %q", want)
				}
			}
		})
	}
}

// Helper functions

func makePhases(count int) []types.PlannedPhase {
	phases := make([]types.PlannedPhase, count)
	for i := 0; i < count; i++ {
		phases[i] = types.PlannedPhase{
			PhaseNumber:     i + 1,
			Title:           fmt.Sprintf("Phase %d", i+1),
			Description:     "Test phase",
			Strategy:        "Test strategy",
			Tasks:           []string{"Task 1"},
			EstimatedEffort: "1 week",
		}
	}
	return phases
}

func makeTasks(count int) []string {
	tasks := make([]string, count)
	for i := 0; i < count; i++ {
		tasks[i] = "Task " + string(rune('1'+i))
	}
	return tasks
}

