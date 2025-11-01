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

	// Update approval metadata
	now := time.Now()
	updates := map[string]interface{}{
		"approved_at": now,
		"approved_by": approvedBy,
	}
	if err := o.store.UpdateIssue(ctx, missionID, updates, approvedBy); err != nil {
		return fmt.Errorf("failed to update approval metadata: %w", err)
	}

	// Add approval comment
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

	// Get mission to inherit priority
	mission, err := o.store.GetIssue(ctx, missionID)
	if err != nil {
		return nil, fmt.Errorf("failed to get mission: %w", err)
	}

	// Helper function to cleanup created phases on error
	cleanup := func() {
		for _, phaseID := range phaseIDs {
			// Best effort cleanup - log errors but don't fail
			if err := o.store.CloseIssue(ctx, phaseID, "Rollback: phase creation failed", "system"); err != nil {
				fmt.Printf("Warning: failed to cleanup phase %s during rollback: %v\n", phaseID, err)
			}
		}
	}

	// Validate phase structure using AI before creating issues
	if err := o.planner.ValidatePhaseStructure(ctx, plan.Phases); err != nil {
		return nil, fmt.Errorf("phase structure validation failed: %w", err)
	}

	// Create each phase as a child epic
	for _, plannedPhase := range plan.Phases {
		phase := &types.Issue{
			Title:              plannedPhase.Title,
			Description:        plannedPhase.Description,
			IssueType:          types.TypeEpic,
			IssueSubtype:       types.SubtypePhase, // Explicitly mark as phase
			Status:             types.StatusOpen,
			Priority:           mission.Priority, // Inherit from parent mission
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
			// Basic validation: phase numbers must be valid
			if depPhaseNum < 1 || depPhaseNum > len(plan.Phases) {
				cleanup()
				return nil, fmt.Errorf("invalid phase dependency: phase %d depends on non-existent phase %d", plannedPhase.PhaseNumber, depPhaseNum)
			}
			// Skip if referring to itself
			if depPhaseNum == plannedPhase.PhaseNumber {
				cleanup()
				return nil, fmt.Errorf("invalid phase dependency: phase %d cannot depend on itself", plannedPhase.PhaseNumber)
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

// HandlePhaseCompletion is called when a phase epic is closed
// It checks if the mission is complete and triggers any follow-up actions
func (o *Orchestrator) HandlePhaseCompletion(ctx context.Context, phaseID string, actor string) error {
	// Get the phase issue
	phase, err := o.store.GetIssue(ctx, phaseID)
	if err != nil {
		return fmt.Errorf("failed to get phase: %w", err)
	}

	// Verify it's an epic (phase)
	if phase.IssueType != types.TypeEpic {
		// Not a phase, nothing to do
		return nil
	}

	// Get parent mission from dependencies
	deps, err := o.store.GetDependencies(ctx, phaseID)
	if err != nil {
		return fmt.Errorf("failed to get dependencies: %w", err)
	}

	// Find parent mission (parent-child dependency)
	// Use explicit subtype instead of counting children (ZFC compliance)
	var missionID string
	for _, dep := range deps {
		if dep.IssueType == types.TypeEpic && dep.IssueSubtype == types.SubtypeMission {
			missionID = dep.ID
			break
		}
	}

	if missionID == "" {
		// No parent mission found, phase is standalone
		return nil
	}

	// Check if mission is complete
	return o.CheckMissionCompletion(ctx, missionID, actor)
}

// CheckMissionCompletion checks if all phases of a mission are complete
// Uses AI to assess completion based on objectives, not just counting closed phases
func (o *Orchestrator) CheckMissionCompletion(ctx context.Context, missionID string, actor string) error {
	// Get mission
	mission, err := o.store.GetIssue(ctx, missionID)
	if err != nil {
		return fmt.Errorf("failed to get mission: %w", err)
	}

	// Skip if already closed
	if mission.Status == types.StatusClosed {
		return nil
	}

	// Get all child phases
	children, err := o.store.GetDependents(ctx, missionID)
	if err != nil {
		return fmt.Errorf("failed to get mission phases: %w", err)
	}

	// If no phases found, nothing to do
	if len(children) == 0 {
		return nil
	}

	// Count phase progress for progress comment
	totalPhases := 0
	closedPhases := 0
	for _, child := range children {
		if child.IssueType == types.TypeEpic {
			totalPhases++
			if child.Status == types.StatusClosed {
				closedPhases++
			}
		}
	}

	// Add progress comment
	progressComment := fmt.Sprintf("Mission progress: %d/%d phases complete", closedPhases, totalPhases)
	if err := o.store.AddComment(ctx, missionID, "mission-orchestrator", progressComment); err != nil {
		// Non-fatal
		fmt.Printf("Warning: failed to add progress comment: %v\n", err)
	}

	// Use AI to assess completion if planner supports it
	// The planner is typically an AI supervisor that implements AssessCompletion
	if supervisor, ok := o.planner.(*ai.Supervisor); ok && supervisor != nil {
		assessment, err := supervisor.AssessCompletion(ctx, mission, children)
		if err != nil {
			// If AI assessment fails, log but don't fail the check
			// This maintains backward compatibility if AI is unavailable
			fmt.Printf("Warning: AI completion assessment failed for %s: %v (skipping auto-close)\n", missionID, err)
			return nil
		}

		// Log assessment reasoning
		reasoningComment := fmt.Sprintf("**AI Completion Assessment**\n\n"+
			"Should Close: %v\n"+
			"Confidence: %.2f\n\n"+
			"Reasoning: %s\n",
			assessment.ShouldClose, assessment.Confidence, assessment.Reasoning)

		if len(assessment.Caveats) > 0 {
			reasoningComment += "\nCaveats:\n"
			for _, caveat := range assessment.Caveats {
				reasoningComment += fmt.Sprintf("- %s\n", caveat)
			}
		}

		if err := o.store.AddComment(ctx, missionID, "ai-supervisor", reasoningComment); err != nil {
			fmt.Printf("Warning: failed to add AI assessment comment: %v\n", err)
		}

		// Close mission if AI recommends it
		if assessment.ShouldClose {
			fmt.Printf("AI recommends closing mission %s (confidence: %.2f)\n", missionID, assessment.Confidence)

			reason := fmt.Sprintf("AI assessment: objectives met (confidence: %.2f)", assessment.Confidence)
			if err := o.store.CloseIssue(ctx, missionID, reason, "ai-supervisor"); err != nil {
				return fmt.Errorf("failed to close mission: %w", err)
			}

			fmt.Printf("✓ Closed mission %s: %s\n", missionID, mission.Title)
		} else {
			fmt.Printf("AI recommends keeping mission %s open: %s\n", missionID, assessment.Reasoning)
		}

		return nil
	}

	// Fallback: No AI supervisor available, use simple heuristic
	// This is expected when AI supervision is disabled or API key is not configured
	// Silently use fallback logic (warning already logged during supervisor initialization if needed)

	// Check if all phase epics are closed
	allPhasesClosed := true
	for _, child := range children {
		if child.IssueType == types.TypeEpic {
			if child.Status != types.StatusClosed {
				allPhasesClosed = false
				break
			}
		}
	}

	// If all phases are closed, close the mission
	if allPhasesClosed {
		reason := fmt.Sprintf("All %d phases completed successfully (fallback logic)", totalPhases)
		if err := o.store.CloseIssue(ctx, missionID, reason, actor); err != nil {
			return fmt.Errorf("failed to close mission: %w", err)
		}
		fmt.Printf("✓ Closed mission %s: %s\n", missionID, mission.Title)
	}

	return nil
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
