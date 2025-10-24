package repl

import (
	"fmt"

	"github.com/fatih/color"
	"github.com/steveyegge/vc/internal/types"
)

// displayMissionPlan formats and displays a mission plan
// Note: This is no longer accessible via slash commands, but kept for potential
// future use by the AI conversational interface
//
//nolint:unused // Reserved for future conversational interface feature
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
