package mission

import (
	"context"
	"fmt"
	"time"

	"github.com/steveyegge/vc/internal/ai"
	"github.com/steveyegge/vc/internal/storage"
	"github.com/steveyegge/vc/internal/types"
)

// Orchestrator handles mission planning and phase creation
type Orchestrator struct {
	store      storage.Storage
	planner    types.MissionPlanner
	skipApproval bool // If true, auto-approve all plans
}

// Config holds orchestrator configuration
type Config struct {
	Store        storage.Storage
	Planner      types.MissionPlanner
	SkipApproval bool // Optional flag to bypass approval gate
}

// NewOrchestrator creates a new mission orchestrator
func NewOrchestrator(cfg *Config) (*Orchestrator, error) {
	if cfg.Store == nil {
		return nil, fmt.Errorf("storage is required")
	}
	if cfg.Planner == nil {
		return nil, fmt.Errorf("planner is required")
	}

	return &Orchestrator{
		store:        cfg.Store,
		planner:      cfg.Planner,
		skipApproval: cfg.SkipApproval,
	}, nil
}

// PlanResult represents the result of planning a mission
type PlanResult struct {
	Plan              *types.MissionPlan
	RequiresApproval  bool
	AutoApproved      bool
	PendingApproval   bool
}

// GenerateAndStorePlan generates a mission plan and handles approval workflow
func (o *Orchestrator) GenerateAndStorePlan(ctx context.Context, mission *types.Mission, planningCtx *types.PlanningContext) (*PlanResult, error) {
	// Generate the plan using AI
	plan, err := o.planner.GeneratePlan(ctx, planningCtx)
	if err != nil {
		return nil, fmt.Errorf("failed to generate plan: %w", err)
	}

	result := &PlanResult{
		Plan:             plan,
		RequiresApproval: mission.ApprovalRequired,
	}

	// Check if approval is required
	if mission.ApprovalRequired && !o.skipApproval {
		// Plan requires approval - store it for review
		result.PendingApproval = true
		return result, nil
	}

	// Auto-approve the plan
	result.AutoApproved = true
	return result, nil
}

// ApprovePlan marks a mission plan as approved
func (o *Orchestrator) ApprovePlan(ctx context.Context, missionID string, approvedBy string) error {
	// Verify mission exists
	_, err := o.store.GetIssue(ctx, missionID)
	if err != nil {
		return fmt.Errorf("failed to get mission: %w", err)
	}

	// Add approval comment
	now := time.Now()
	comment := fmt.Sprintf("Mission plan approved by %s at %s", approvedBy, now.Format(time.RFC3339))
	if err := o.store.AddComment(ctx, missionID, approvedBy, comment); err != nil {
		return fmt.Errorf("failed to add approval comment: %w", err)
	}

	return nil
}

// RejectPlan marks a mission plan as rejected
func (o *Orchestrator) RejectPlan(ctx context.Context, missionID string, rejectedBy string, reason string) error {
	// Add rejection comment
	now := time.Now()
	comment := fmt.Sprintf("Mission plan rejected by %s at %s\nReason: %s", rejectedBy, now.Format(time.RFC3339), reason)
	if err := o.store.AddComment(ctx, missionID, rejectedBy, comment); err != nil {
		return fmt.Errorf("failed to add rejection comment: %w", err)
	}

	return nil
}

// CreatePhasesFromPlan creates phase epics from an approved plan
// This is called after a plan has been approved (or auto-approved)
// If any phase creation fails, all previously created phases are cleaned up (rollback)
func (o *Orchestrator) CreatePhasesFromPlan(ctx context.Context, missionID string, plan *types.MissionPlan, actor string) ([]string, error) {
	var phaseIDs []string

	// Helper function to cleanup created phases on error
	cleanup := func() {
		for _, phaseID := range phaseIDs {
			// Best effort cleanup - log errors but don't fail
			if err := o.store.CloseIssue(ctx, phaseID, "Rollback: phase creation failed", "system"); err != nil {
				fmt.Printf("Warning: failed to cleanup phase %s during rollback: %v\n", phaseID, err)
			}
		}
	}

	// Create each phase as a child epic
	for _, plannedPhase := range plan.Phases {
		phase := &types.Issue{
			Title:              plannedPhase.Title,
			Description:        plannedPhase.Description,
			IssueType:          types.TypeEpic,
			Status:             types.StatusOpen,
			Priority:           0, // Phases inherit mission priority
			Design:             fmt.Sprintf("Strategy: %s\n\nTasks:\n%s", plannedPhase.Strategy, joinTasks(plannedPhase.Tasks)),
			AcceptanceCriteria: fmt.Sprintf("Complete all tasks for this phase:\n%s", joinTasks(plannedPhase.Tasks)),
		}

		// Create the phase issue
		if err := o.store.CreateIssue(ctx, phase, actor); err != nil {
			cleanup()
			return nil, fmt.Errorf("failed to create phase %d: %w", plannedPhase.PhaseNumber, err)
		}

		phaseID := phase.ID
		phaseIDs = append(phaseIDs, phaseID)

		// Add parent-child dependency: phase depends on mission
		dep := &types.Dependency{
			IssueID:     phaseID,
			DependsOnID: missionID,
			Type:        types.DepParentChild,
		}
		if err := o.store.AddDependency(ctx, dep, actor); err != nil {
			cleanup()
			return nil, fmt.Errorf("failed to add parent-child dependency for phase %s: %w", phaseID, err)
		}

		// Add blocks dependencies to other phases
		// plannedPhase.Dependencies contains phase numbers (1-indexed)
		// We need to convert to phase IDs
		for _, depPhaseNum := range plannedPhase.Dependencies {
			// depPhaseNum is 1-indexed, array is 0-indexed
			if depPhaseNum < 1 || depPhaseNum > len(phaseIDs) {
				cleanup()
				return nil, fmt.Errorf("invalid phase dependency: phase %d depends on phase %d", plannedPhase.PhaseNumber, depPhaseNum)
			}
			depPhaseID := phaseIDs[depPhaseNum-1]

			// Create blocks dependency: current phase blocks on dependency phase
			blocksDep := &types.Dependency{
				IssueID:     phaseID,
				DependsOnID: depPhaseID,
				Type:        types.DepBlocks,
			}
			if err := o.store.AddDependency(ctx, blocksDep, actor); err != nil {
				cleanup()
				return nil, fmt.Errorf("failed to add blocks dependency for phase %s: %w", phaseID, err)
			}
		}
	}

	return phaseIDs, nil
}

// joinTasks formats a task list as a numbered list
func joinTasks(tasks []string) string {
	var result string
	for i, task := range tasks {
		result += fmt.Sprintf("%d. %s\n", i+1, task)
	}
	return result
}

// ProcessMission is the main entry point for processing a mission
// It generates a plan, handles approval, and creates phases
func (o *Orchestrator) ProcessMission(ctx context.Context, mission *types.Mission, planningCtx *types.PlanningContext, actor string) (*PlanResult, error) {
	// Generate the plan
	result, err := o.GenerateAndStorePlan(ctx, mission, planningCtx)
	if err != nil {
		return nil, fmt.Errorf("failed to generate plan: %w", err)
	}

	// If pending approval, return and wait for user action
	if result.PendingApproval {
		return result, nil
	}

	// Plan is approved (or auto-approved), create phases
	phaseIDs, err := o.CreatePhasesFromPlan(ctx, mission.ID, result.Plan, actor)
	if err != nil {
		return result, fmt.Errorf("failed to create phases: %w", err)
	}

	// Add comment documenting phase creation
	comment := fmt.Sprintf("Created %d phases from approved plan: %v", len(phaseIDs), phaseIDs)
	if err := o.store.AddComment(ctx, mission.ID, actor, comment); err != nil {
		// Non-fatal, just log
		fmt.Printf("Warning: failed to add phase creation comment: %v\n", err)
	}

	return result, nil
}

// DefaultOrchestrator creates an orchestrator with default configuration
func DefaultOrchestrator(store storage.Storage) (*Orchestrator, error) {
	// Create AI supervisor as planner
	supervisor, err := ai.NewSupervisor(&ai.Config{
		Store: store,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create AI supervisor: %w", err)
	}

	return NewOrchestrator(&Config{
		Store:        store,
		Planner:      supervisor,
		SkipApproval: false,
	})
}
