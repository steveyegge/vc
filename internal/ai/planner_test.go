package ai

import (
	"context"
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
			err := s.ValidatePlan(context.Background(), tt.plan)

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

func TestValidatePlanSize(t *testing.T) {
	tests := []struct {
		name    string
		plan    *types.MissionPlan
		envVars map[string]string
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid plan within limits",
			plan: &types.MissionPlan{
				MissionID: "vc-1",
				Phases: []types.PlannedPhase{
					{
						PhaseNumber:     1,
						Title:           "Phase 1",
						Description:     "Test",
						Strategy:        "Test",
						Tasks:           makeTasks(10),
						EstimatedEffort: "1w",
					},
					{
						PhaseNumber:     2,
						Title:           "Phase 2",
						Description:     "Test",
						Strategy:        "Test",
						Tasks:           makeTasks(5),
						Dependencies:    []int{1},
						EstimatedEffort: "1w",
					},
				},
				Strategy:        "Test",
				EstimatedEffort: "2w",
				Confidence:      0.8,
			},
			wantErr: false,
		},
		{
			name: "too many phases (exceeds default limit of 20)",
			plan: &types.MissionPlan{
				MissionID:       "vc-1",
				Phases:          makePhases(25),
				Strategy:        "Test",
				EstimatedEffort: "25w",
				Confidence:      0.5,
			},
			wantErr: true,
			errMsg:  "too many phases",
		},
		{
			name: "phase with too many tasks (exceeds default limit of 30)",
			plan: &types.MissionPlan{
				MissionID: "vc-1",
				Phases: []types.PlannedPhase{
					{
						PhaseNumber:     1,
						Title:           "Huge Phase",
						Description:     "Test",
						Strategy:        "Test",
						Tasks:           makeTasks(35),
						EstimatedEffort: "1w",
					},
				},
				Strategy:        "Test",
				EstimatedEffort: "1w",
				Confidence:      0.8,
			},
			wantErr: true,
			errMsg:  "too many tasks",
		},
		{
			name: "too many total tasks (20 phases * 30 tasks = 600 > default limit)",
			plan: &types.MissionPlan{
				MissionID: "vc-1",
				Phases: func() []types.PlannedPhase {
					phases := make([]types.PlannedPhase, 20)
					for i := 0; i < 20; i++ {
						phases[i] = types.PlannedPhase{
							PhaseNumber:     i + 1,
							Title:           fmt.Sprintf("Phase %d", i+1),
							Description:     "Test",
							Strategy:        "Test",
							Tasks:           makeTasks(30), // 20 * 30 = 600 tasks
							EstimatedEffort: "1w",
						}
					}
					return phases
				}(),
				Strategy:        "Test",
				EstimatedEffort: "20w",
				Confidence:      0.5,
			},
			wantErr: false, // 600 = 20*30 is exactly at limit
		},
		{
			name: "excessive dependency depth (> 10 levels)",
			plan: &types.MissionPlan{
				MissionID: "vc-1",
				Phases: func() []types.PlannedPhase {
					// Create 12 phases in a chain: 1 -> 2 -> 3 -> ... -> 12
					phases := make([]types.PlannedPhase, 12)
					for i := 0; i < 12; i++ {
						phase := types.PlannedPhase{
							PhaseNumber:     i + 1,
							Title:           fmt.Sprintf("Phase %d", i+1),
							Description:     "Test",
							Strategy:        "Test",
							Tasks:           []string{"Task 1"},
							EstimatedEffort: "1w",
						}
						if i > 0 {
							phase.Dependencies = []int{i} // Depends on previous phase
						}
						phases[i] = phase
					}
					return phases
				}(),
				Strategy:        "Test",
				EstimatedEffort: "12w",
				Confidence:      0.5,
			},
			wantErr: true,
			errMsg:  "excessive dependency depth",
		},
		{
			name: "custom limits via environment (smaller limits)",
			plan: &types.MissionPlan{
				MissionID: "vc-1",
				Phases: []types.PlannedPhase{
					{
						PhaseNumber:     1,
						Title:           "Phase 1",
						Description:     "Test",
						Strategy:        "Test",
						Tasks:           makeTasks(8), // Would be OK with default (30), but exceeds custom limit (5)
						EstimatedEffort: "1w",
					},
				},
				Strategy:        "Test",
				EstimatedEffort: "1w",
				Confidence:      0.8,
			},
			envVars: map[string]string{
				"VC_MAX_PLAN_PHASES":      "10",
				"VC_MAX_PHASE_TASKS":      "5",
				"VC_MAX_DEPENDENCY_DEPTH": "3",
			},
			wantErr: true,
			errMsg:  "too many tasks",
		},
		{
			name: "valid plan with custom larger limits",
			plan: &types.MissionPlan{
				MissionID: "vc-1",
				Phases: []types.PlannedPhase{
					{
						PhaseNumber:     1,
						Title:           "Large Phase",
						Description:     "Test",
						Strategy:        "Test",
						Tasks:           makeTasks(45), // Exceeds default (30), OK with custom (50)
						EstimatedEffort: "1w",
					},
				},
				Strategy:        "Test",
				EstimatedEffort: "1w",
				Confidence:      0.8,
			},
			envVars: map[string]string{
				"VC_MAX_PHASE_TASKS": "50",
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Set environment variables for this test
			for k, v := range tt.envVars {
				t.Setenv(k, v)
			}

			s := &Supervisor{}
			err := s.validatePlanSize(context.Background(), tt.plan)

			if (err != nil) != tt.wantErr {
				t.Errorf("validatePlanSize() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if tt.wantErr && tt.errMsg != "" {
				if err == nil || !strings.Contains(err.Error(), tt.errMsg) {
					t.Errorf("validatePlanSize() error = %v, want error containing %q", err, tt.errMsg)
				}
			}
		})
	}
}

func TestCalculateDependencyDepth(t *testing.T) {
	tests := []struct {
		name      string
		phases    []types.PlannedPhase
		wantDepth int
	}{
		{
			name: "no dependencies",
			phases: []types.PlannedPhase{
				{PhaseNumber: 1, Title: "P1", Description: "Test", Strategy: "Test", Tasks: []string{"T1"}, EstimatedEffort: "1w"},
				{PhaseNumber: 2, Title: "P2", Description: "Test", Strategy: "Test", Tasks: []string{"T2"}, EstimatedEffort: "1w"},
			},
			wantDepth: 1, // Each phase has depth 1 (no deps)
		},
		{
			name: "linear chain (1 -> 2 -> 3)",
			phases: []types.PlannedPhase{
				{PhaseNumber: 1, Title: "P1", Description: "Test", Strategy: "Test", Tasks: []string{"T1"}, EstimatedEffort: "1w"},
				{PhaseNumber: 2, Title: "P2", Description: "Test", Strategy: "Test", Tasks: []string{"T2"}, Dependencies: []int{1}, EstimatedEffort: "1w"},
				{PhaseNumber: 3, Title: "P3", Description: "Test", Strategy: "Test", Tasks: []string{"T3"}, Dependencies: []int{2}, EstimatedEffort: "1w"},
			},
			wantDepth: 3, // P1=1, P2=2, P3=3
		},
		{
			name: "diamond (1 -> 2,3 -> 4)",
			phases: []types.PlannedPhase{
				{PhaseNumber: 1, Title: "P1", Description: "Test", Strategy: "Test", Tasks: []string{"T1"}, EstimatedEffort: "1w"},
				{PhaseNumber: 2, Title: "P2", Description: "Test", Strategy: "Test", Tasks: []string{"T2"}, Dependencies: []int{1}, EstimatedEffort: "1w"},
				{PhaseNumber: 3, Title: "P3", Description: "Test", Strategy: "Test", Tasks: []string{"T3"}, Dependencies: []int{1}, EstimatedEffort: "1w"},
				{PhaseNumber: 4, Title: "P4", Description: "Test", Strategy: "Test", Tasks: []string{"T4"}, Dependencies: []int{2, 3}, EstimatedEffort: "1w"},
			},
			wantDepth: 3, // P1=1, P2=P3=2, P4=3
		},
		{
			name: "complex graph with depth 5",
			phases: []types.PlannedPhase{
				{PhaseNumber: 1, Title: "P1", Description: "Test", Strategy: "Test", Tasks: []string{"T1"}, EstimatedEffort: "1w"},
				{PhaseNumber: 2, Title: "P2", Description: "Test", Strategy: "Test", Tasks: []string{"T2"}, Dependencies: []int{1}, EstimatedEffort: "1w"},
				{PhaseNumber: 3, Title: "P3", Description: "Test", Strategy: "Test", Tasks: []string{"T3"}, Dependencies: []int{1}, EstimatedEffort: "1w"},
				{PhaseNumber: 4, Title: "P4", Description: "Test", Strategy: "Test", Tasks: []string{"T4"}, Dependencies: []int{2, 3}, EstimatedEffort: "1w"},
				{PhaseNumber: 5, Title: "P5", Description: "Test", Strategy: "Test", Tasks: []string{"T5"}, Dependencies: []int{4}, EstimatedEffort: "1w"},
			},
			wantDepth: 4, // P1=1, P2=P3=2, P4=3, P5=4
		},
		{
			name: "long chain (depth 10)",
			phases: func() []types.PlannedPhase {
				phases := make([]types.PlannedPhase, 10)
				for i := 0; i < 10; i++ {
					phase := types.PlannedPhase{
						PhaseNumber:     i + 1,
						Title:           fmt.Sprintf("P%d", i+1),
						Description:     "Test",
						Strategy:        "Test",
						Tasks:           []string{"T1"},
						EstimatedEffort: "1w",
					}
					if i > 0 {
						phase.Dependencies = []int{i}
					}
					phases[i] = phase
				}
				return phases
			}(),
			wantDepth: 10,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			depth := calculateDependencyDepth(tt.phases)
			if depth != tt.wantDepth {
				t.Errorf("calculateDependencyDepth() = %d, want %d", depth, tt.wantDepth)
			}
		})
	}
}

func TestGetEnvInt(t *testing.T) {
	tests := []struct {
		name         string
		key          string
		envValue     string
		defaultValue int
		want         int
	}{
		{
			name:         "environment variable not set",
			key:          "TEST_VAR_NOTSET",
			defaultValue: 42,
			want:         42,
		},
		{
			name:         "environment variable set to valid int",
			key:          "TEST_VAR_VALID",
			envValue:     "123",
			defaultValue: 42,
			want:         123,
		},
		{
			name:         "environment variable set to invalid value",
			key:          "TEST_VAR_INVALID",
			envValue:     "not-a-number",
			defaultValue: 42,
			want:         42, // Should fall back to default
		},
		{
			name:         "environment variable set to empty string",
			key:          "TEST_VAR_EMPTY",
			envValue:     "",
			defaultValue: 42,
			want:         42, // Should fall back to default
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.envValue != "" {
				t.Setenv(tt.key, tt.envValue)
			}

			got := getEnvInt(tt.key, tt.defaultValue)
			if got != tt.want {
				t.Errorf("getEnvInt(%q, %d) = %d, want %d", tt.key, tt.defaultValue, got, tt.want)
			}
		})
	}
}

