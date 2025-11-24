package types

import (
	"testing"
	"time"
)

func TestMission_Validate(t *testing.T) {
	now := time.Now()

	tests := []struct {
		name    string
		mission *Mission
		wantErr bool
	}{
		{
			name: "valid mission",
			mission: &Mission{
				Issue: Issue{
					ID:          "vc-1",
					Title:       "Implement feature X",
					Description: "Test description",
					IssueType:   TypeEpic,
					Status:      StatusOpen,
					Priority:    0,
					CreatedAt:   now,
					UpdatedAt:   now,
				},
				Goal:       "Build feature X end-to-end",
				Context:    "Additional context",
			},
			wantErr: false,
		},
		{
			name: "mission without goal",
			mission: &Mission{
				Issue: Issue{
					ID:          "vc-1",
					Title:       "Test",
					Description: "Test",
					IssueType:   TypeEpic,
					Status:      StatusOpen,
					Priority:    0,
					CreatedAt:   now,
					UpdatedAt:   now,
				},
				Goal:       "",
			},
			wantErr: true,
		},
		{
			name: "mission with wrong issue type",
			mission: &Mission{
				Issue: Issue{
					ID:          "vc-1",
					Title:       "Test",
					Description: "Test",
					IssueType:   TypeTask,
					Status:      StatusOpen,
					Priority:    0,
					CreatedAt:   now,
					UpdatedAt:   now,
				},
				Goal:       "Build feature",
			},
			wantErr: true,
		},
		{
			name: "approved without approver",
			mission: &Mission{
				Issue: Issue{
					ID:          "vc-1",
					Title:       "Test",
					Description: "Test",
					IssueType:   TypeEpic,
					Status:      StatusOpen,
					Priority:    0,
					CreatedAt:   now,
					UpdatedAt:   now,
				},
				Goal:             "Build feature",
				ApprovalRequired: true,
				ApprovedAt:       &now,
				ApprovedBy:       "",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.mission.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Mission.Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestMission_IsApproved(t *testing.T) {
	now := time.Now()

	tests := []struct {
		name    string
		mission *Mission
		want    bool
	}{
		{
			name: "no approval required",
			mission: &Mission{
				ApprovalRequired: false,
			},
			want: true,
		},
		{
			name: "approval required and approved",
			mission: &Mission{
				ApprovalRequired: true,
				ApprovedAt:       &now,
				ApprovedBy:       "user@example.com",
			},
			want: true,
		},
		{
			name: "approval required but not approved",
			mission: &Mission{
				ApprovalRequired: true,
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.mission.IsApproved(); got != tt.want {
				t.Errorf("Mission.IsApproved() = %v, want %v", got, tt.want)
			}
		})
	}
}


func TestPlannedPhase_Validate(t *testing.T) {
	tests := []struct {
		name    string
		phase   *PlannedPhase
		wantErr bool
	}{
		{
			name: "valid planned phase",
			phase: &PlannedPhase{
				PhaseNumber:     1,
				Title:           "Foundation",
				Description:     "Build core infrastructure",
				Strategy:        "Start with data models",
				Tasks:           []string{"Create types", "Add storage"},
				EstimatedEffort: "4 hours",
			},
			wantErr: false,
		},
		{
			name: "phase with dependencies",
			phase: &PlannedPhase{
				PhaseNumber:     3,
				Title:           "Integration",
				Description:     "Integrate components",
				Strategy:        "Connect the pieces",
				Tasks:           []string{"Wire up APIs"},
				Dependencies:    []int{1, 2},
				EstimatedEffort: "2 hours",
			},
			wantErr: false,
		},
		{
			name: "invalid phase number",
			phase: &PlannedPhase{
				PhaseNumber:     0,
				Title:           "Test",
				Description:     "Test",
				Strategy:        "Test",
				Tasks:           []string{"task"},
				EstimatedEffort: "1 hour",
			},
			wantErr: true,
		},
		{
			name: "missing title",
			phase: &PlannedPhase{
				PhaseNumber:     1,
				Title:           "",
				Description:     "Test",
				Strategy:        "Test",
				Tasks:           []string{"task"},
				EstimatedEffort: "1 hour",
			},
			wantErr: true,
		},
		{
			name: "no tasks",
			phase: &PlannedPhase{
				PhaseNumber:     1,
				Title:           "Test",
				Description:     "Test",
				Strategy:        "Test",
				Tasks:           []string{},
				EstimatedEffort: "1 hour",
			},
			wantErr: true,
		},
		{
			name: "dependency on later phase",
			phase: &PlannedPhase{
				PhaseNumber:     2,
				Title:           "Test",
				Description:     "Test",
				Strategy:        "Test",
				Tasks:           []string{"task"},
				Dependencies:    []int{3},
				EstimatedEffort: "1 hour",
			},
			wantErr: true,
		},
		{
			name: "dependency on self",
			phase: &PlannedPhase{
				PhaseNumber:     2,
				Title:           "Test",
				Description:     "Test",
				Strategy:        "Test",
				Tasks:           []string{"task"},
				Dependencies:    []int{2},
				EstimatedEffort: "1 hour",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.phase.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("PlannedPhase.Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestMissionPlan_Validate(t *testing.T) {
	now := time.Now()

	tests := []struct {
		name    string
		plan    *MissionPlan
		wantErr bool
	}{
		{
			name: "valid plan",
			plan: &MissionPlan{
				MissionID: "vc-1",
				Phases: []PlannedPhase{
					{
						PhaseNumber:     1,
						Title:           "Foundation",
						Description:     "Build foundation",
						Strategy:        "Start simple",
						Tasks:           []string{"Task 1"},
						EstimatedEffort: "2 hours",
					},
					{
						PhaseNumber:     2,
						Title:           "Features",
						Description:     "Add features",
						Strategy:        "Iterate",
						Tasks:           []string{"Task 2"},
						Dependencies:    []int{1},
						EstimatedEffort: "3 hours",
					},
				},
				Strategy:        "Phased approach",
				Risks:           []string{"Risk 1"},
				EstimatedEffort: "5 hours",
				Confidence:      0.8,
				GeneratedAt:     now,
				GeneratedBy:     "ai-planner",
			},
			wantErr: false,
		},
		{
			name: "plan without mission ID",
			plan: &MissionPlan{
				MissionID: "",
				Phases: []PlannedPhase{
					{
						PhaseNumber:     1,
						Title:           "Test",
						Description:     "Test",
						Strategy:        "Test",
						Tasks:           []string{"Task"},
						EstimatedEffort: "1 hour",
					},
				},
				Strategy:        "Test",
				EstimatedEffort: "1 hour",
				Confidence:      0.8,
			},
			wantErr: true,
		},
		{
			name: "plan with no phases",
			plan: &MissionPlan{
				MissionID:       "vc-1",
				Phases:          []PlannedPhase{},
				Strategy:        "Test",
				EstimatedEffort: "1 hour",
				Confidence:      0.8,
			},
			wantErr: true,
		},
		{
			name: "plan with invalid confidence",
			plan: &MissionPlan{
				MissionID: "vc-1",
				Phases: []PlannedPhase{
					{
						PhaseNumber:     1,
						Title:           "Test",
						Description:     "Test",
						Strategy:        "Test",
						Tasks:           []string{"Task"},
						EstimatedEffort: "1 hour",
					},
				},
				Strategy:        "Test",
				EstimatedEffort: "1 hour",
				Confidence:      1.5,
			},
			wantErr: true,
		},
		{
			name: "plan with non-sequential phase numbers",
			plan: &MissionPlan{
				MissionID: "vc-1",
				Phases: []PlannedPhase{
					{
						PhaseNumber:     1,
						Title:           "Test",
						Description:     "Test",
						Strategy:        "Test",
						Tasks:           []string{"Task"},
						EstimatedEffort: "1 hour",
					},
					{
						PhaseNumber:     3,
						Title:           "Test 2",
						Description:     "Test",
						Strategy:        "Test",
						Tasks:           []string{"Task"},
						EstimatedEffort: "1 hour",
					},
				},
				Strategy:        "Test",
				EstimatedEffort: "2 hours",
				Confidence:      0.8,
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.plan.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("MissionPlan.Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestPlanningContext_Validate(t *testing.T) {
	now := time.Now()

	validMission := &Mission{
		Issue: Issue{
			ID:          "vc-1",
			Title:       "Test Mission",
			Description: "Test",
			IssueType:   TypeEpic,
			Status:      StatusOpen,
			Priority:    0,
			CreatedAt:   now,
			UpdatedAt:   now,
		},
		Goal:       "Test goal",
	}

	tests := []struct {
		name    string
		ctx     *PlanningContext
		wantErr bool
	}{
		{
			name: "valid context",
			ctx: &PlanningContext{
				Mission:        validMission,
				CodebaseInfo:   "Go project",
				RecentIssues:   []*Issue{},
				FailedAttempts: 0,
			},
			wantErr: false,
		},
		{
			name: "context without mission",
			ctx: &PlanningContext{
				Mission:      nil,
				CodebaseInfo: "Go project",
			},
			wantErr: true,
		},
		{
			name: "negative failed attempts",
			ctx: &PlanningContext{
				Mission:        validMission,
				FailedAttempts: -1,
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.ctx.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("PlanningContext.Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestPlannedTask_Validate(t *testing.T) {
	tests := []struct {
		name    string
		task    *PlannedTask
		wantErr bool
	}{
		{
			name: "valid task",
			task: &PlannedTask{
				Title:              "Implement feature",
				Description:        "Add new functionality",
				AcceptanceCriteria: "Tests pass",
				EstimatedMinutes:   30,
				Priority:           1,
				Type:               "task",
			},
			wantErr: false,
		},
		{
			name: "task without title",
			task: &PlannedTask{
				Title:            "",
				Description:      "Test",
				EstimatedMinutes: 30,
				Priority:         1,
				Type:             "task",
			},
			wantErr: true,
		},
		{
			name: "task with negative minutes",
			task: &PlannedTask{
				Title:            "Test",
				Description:      "Test",
				EstimatedMinutes: -10,
				Priority:         1,
				Type:             "task",
			},
			wantErr: true,
		},
		{
			name: "task with invalid priority",
			task: &PlannedTask{
				Title:            "Test",
				Description:      "Test",
				EstimatedMinutes: 30,
				Priority:         5,
				Type:             "task",
			},
			wantErr: true,
		},
		{
			name: "task with invalid type",
			task: &PlannedTask{
				Title:            "Test",
				Description:      "Test",
				EstimatedMinutes: 30,
				Priority:         1,
				Type:             "invalid-type",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.task.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("PlannedTask.Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
