package repl

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/fatih/color"
	"github.com/steveyegge/vc/internal/types"
)

// cmdPlan displays a mission plan for review
// Usage: /plan <mission-id>
func (r *REPL) cmdPlan(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: /plan <mission-id>")
	}

	missionID := args[0]
	ctx := r.ctx

	// Get the mission from storage
	issue, err := r.store.GetIssue(ctx, missionID)
	if err != nil {
		return fmt.Errorf("failed to get mission %s: %w", missionID, err)
	}

	// Check if it's a mission (epic type)
	if issue.IssueType != types.TypeEpic {
		return fmt.Errorf("%s is not a mission (type: %s, expected: epic)", missionID, issue.IssueType)
	}

	// Convert to Mission type (we need the mission-specific fields)
	// For now, we'll check if there's a pending plan
	r.plansMu.RLock()
	plan, hasPlan := r.pendingPlans[missionID]
	r.plansMu.RUnlock()

	if !hasPlan {
		return fmt.Errorf("no pending plan found for mission %s\nGenerate a plan first or check if it was already approved", missionID)
	}

	// Display the plan
	displayMissionPlan(issue, plan)

	return nil
}

// cmdApprove approves a mission plan
// Usage: /approve <mission-id>
func (r *REPL) cmdApprove(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: /approve <mission-id>")
	}

	missionID := args[0]
	ctx := r.ctx

	// Check if there's a pending plan
	r.plansMu.RLock()
	plan, hasPlan := r.pendingPlans[missionID]
	r.plansMu.RUnlock()

	if !hasPlan {
		return fmt.Errorf("no pending plan found for mission %s\nUse /plan to view the plan first", missionID)
	}

	// Verify the mission exists
	_, err := r.store.GetIssue(ctx, missionID)
	if err != nil {
		return fmt.Errorf("failed to get mission %s: %w", missionID, err)
	}

	green := color.New(color.FgGreen).SprintFunc()
	cyan := color.New(color.FgCyan, color.Bold).SprintFunc()

	fmt.Printf("\n%s\n", cyan("Approving Mission Plan"))
	fmt.Println()

	// Mark as approved by adding a comment with approval metadata
	now := time.Now()
	approvalComment := fmt.Sprintf("Mission plan approved by %s at %s\n\nPlan Summary:\n- %d phases\n- Estimated effort: %s\n- Confidence: %.2f\n- Strategy: %s",
		r.actor, now.Format(time.RFC3339),
		len(plan.Phases), plan.EstimatedEffort, plan.Confidence, plan.Strategy)

	if err := r.store.AddComment(ctx, missionID, r.actor, approvalComment); err != nil {
		return fmt.Errorf("failed to add approval comment: %w", err)
	}

	// Store the full plan as a comment for reference
	planJSON, err := json.MarshalIndent(plan, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to serialize plan: %w", err)
	}
	planComment := fmt.Sprintf("Approved Plan (JSON):\n```json\n%s\n```", string(planJSON))
	if err := r.store.AddComment(ctx, missionID, "ai-planner", planComment); err != nil {
		return fmt.Errorf("failed to store plan: %w", err)
	}

	fmt.Printf("%s Plan approved for mission %s\n", green("✓"), missionID)
	fmt.Printf("  Approved by: %s\n", r.actor)
	fmt.Printf("  Approved at: %s\n", now.Format("2006-01-02 15:04:05"))
	fmt.Println()

	// Remove from pending plans
	r.plansMu.Lock()
	delete(r.pendingPlans, missionID)
	r.plansMu.Unlock()

	fmt.Println("Next steps:")
	fmt.Println("  The mission orchestrator will now create phase epics and begin execution")
	fmt.Println()

	return nil
}

// cmdReject rejects a mission plan
// Usage: /reject <mission-id> [reason]
func (r *REPL) cmdReject(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: /reject <mission-id> [reason]")
	}

	missionID := args[0]
	reason := "No reason provided"
	if len(args) > 1 {
		reason = strings.Join(args[1:], " ")
	}

	ctx := r.ctx

	// Check if there's a pending plan
	r.plansMu.RLock()
	_, hasPlan := r.pendingPlans[missionID]
	r.plansMu.RUnlock()

	if !hasPlan {
		return fmt.Errorf("no pending plan found for mission %s", missionID)
	}

	red := color.New(color.FgRed).SprintFunc()
	cyan := color.New(color.FgCyan, color.Bold).SprintFunc()

	fmt.Printf("\n%s\n", cyan("Rejecting Mission Plan"))
	fmt.Println()

	// Add rejection comment
	now := time.Now()
	rejectionComment := fmt.Sprintf("Mission plan rejected by %s at %s\n\nReason: %s",
		r.actor, now.Format(time.RFC3339), reason)

	if err := r.store.AddComment(ctx, missionID, r.actor, rejectionComment); err != nil {
		return fmt.Errorf("failed to add rejection comment: %w", err)
	}

	fmt.Printf("%s Plan rejected for mission %s\n", red("✗"), missionID)
	fmt.Printf("  Rejected by: %s\n", r.actor)
	fmt.Printf("  Reason: %s\n", reason)
	fmt.Println()

	// Remove from pending plans
	r.plansMu.Lock()
	delete(r.pendingPlans, missionID)
	r.plansMu.Unlock()

	fmt.Println("Next steps:")
	fmt.Println("  Request a new plan or modify the mission description")
	fmt.Println()

	return nil
}

// displayMissionPlan formats and displays a mission plan
func displayMissionPlan(mission *types.Issue, plan *types.MissionPlan) {
	cyan := color.New(color.FgCyan, color.Bold).SprintFunc()
	green := color.New(color.FgGreen).SprintFunc()
	yellow := color.New(color.FgYellow).SprintFunc()
	gray := color.New(color.FgHiBlack).SprintFunc()
	bold := color.New(color.Bold).SprintFunc()

	fmt.Printf("\n%s\n", cyan("═══════════════════════════════════════════════════════════════"))
	fmt.Printf("%s\n", cyan("Mission Plan"))
	fmt.Printf("%s\n", cyan("═══════════════════════════════════════════════════════════════"))
	fmt.Println()

	// Mission header
	fmt.Printf("%s %s\n", bold("Mission:"), green(mission.ID))
	fmt.Printf("%s %s\n", bold("Title:"), mission.Title)
	fmt.Println()

	// Plan metadata
	fmt.Printf("%s %s\n", bold("Strategy:"), plan.Strategy)
	fmt.Printf("%s %s\n", bold("Estimated Effort:"), plan.EstimatedEffort)
	fmt.Printf("%s %.0f%%\n", bold("Confidence:"), plan.Confidence*100)
	fmt.Printf("%s %d\n", bold("Total Phases:"), len(plan.Phases))
	fmt.Println()

	// Risks
	if len(plan.Risks) > 0 {
		fmt.Printf("%s\n", yellow("⚠  Risks & Challenges:"))
		for _, risk := range plan.Risks {
			fmt.Printf("   • %s\n", risk)
		}
		fmt.Println()
	}

	// Phases
	fmt.Printf("%s\n", cyan("───────────────────────────────────────────────────────────────"))
	fmt.Printf("%s\n", bold("Phases:"))
	fmt.Printf("%s\n", cyan("───────────────────────────────────────────────────────────────"))
	fmt.Println()

	for i, phase := range plan.Phases {
		phaseNum := color.New(color.FgCyan, color.Bold).SprintFunc()
		fmt.Printf("%s. %s\n", phaseNum(fmt.Sprintf("Phase %d", phase.PhaseNumber)), bold(phase.Title))
		fmt.Printf("   %s %s\n", gray("Effort:"), phase.EstimatedEffort)

		if len(phase.Dependencies) > 0 {
			fmt.Printf("   %s Phase", gray("Depends on:"))
			for j, dep := range phase.Dependencies {
				if j > 0 {
					fmt.Print(",")
				}
				fmt.Printf(" %d", dep)
			}
			fmt.Println()
		}

		fmt.Println()
		fmt.Printf("   %s\n", phase.Description)
		fmt.Println()

		fmt.Printf("   %s %s\n", gray("Strategy:"), phase.Strategy)
		fmt.Println()

		if len(phase.Tasks) > 0 {
			fmt.Printf("   %s\n", gray("High-level tasks:"))
			for _, task := range phase.Tasks {
				fmt.Printf("     • %s\n", task)
			}
		}

		if i < len(plan.Phases)-1 {
			fmt.Println()
			fmt.Println(gray("   ─────────────────────────────────────────────────"))
			fmt.Println()
		}
	}

	fmt.Println()
	fmt.Printf("%s\n", cyan("═══════════════════════════════════════════════════════════════"))
	fmt.Println()

	// Instructions
	fmt.Printf("%s\n", bold("Review this plan carefully."))
	fmt.Println()
	fmt.Printf("To approve: %s\n", green(fmt.Sprintf("/approve %s", mission.ID)))
	fmt.Printf("To reject:  %s\n", yellow(fmt.Sprintf("/reject %s [reason]", mission.ID)))
	fmt.Println()
}

// StorePendingPlan stores a generated plan for approval
// This is called by the mission orchestrator after generating a plan
func (r *REPL) StorePendingPlan(missionID string, plan *types.MissionPlan) {
	r.plansMu.Lock()
	defer r.plansMu.Unlock()
	r.pendingPlans[missionID] = plan
}

// GetPendingPlan retrieves a pending plan if one exists
func (r *REPL) GetPendingPlan(missionID string) (*types.MissionPlan, bool) {
	r.plansMu.RLock()
	defer r.plansMu.RUnlock()
	plan, exists := r.pendingPlans[missionID]
	return plan, exists
}

// ClearPendingPlan removes a pending plan
func (r *REPL) ClearPendingPlan(missionID string) {
	r.plansMu.Lock()
	defer r.plansMu.Unlock()
	delete(r.pendingPlans, missionID)
}
