package main

import (
	"context"
	"fmt"
	"os"

	"github.com/fatih/color"
	"github.com/spf13/cobra"
)

var planCmd = &cobra.Command{
	Use:   "plan",
	Short: "Mission planning commands",
	Long:  `Interactive mission planning with AI-guided refinement and validation.`,
	Run: func(cmd *cobra.Command, args []string) {
		// Show help if no subcommand provided
		cmd.Help()
	},
}

var planGenerateCmd = &cobra.Command{
	Use:   "generate <issue-id>",
	Short: "Generate a plan from an existing Beads issue",
	Long: `Generate an initial mission plan from an existing issue in the Beads tracker.
This reads the issue's description, design, and acceptance criteria to create a draft plan.`,
	Args: cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		issueID := args[0]
		ctx := context.Background()

		// Get the issue from storage
		issue, err := store.GetIssue(ctx, issueID)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: failed to get issue: %v\n", err)
			os.Exit(1)
		}
		if issue == nil {
			fmt.Fprintf(os.Stderr, "Error: issue %s not found\n", issueID)
			os.Exit(1)
		}

		// TODO: Generate plan using AI planner (Epic 2: vc-3yi1)
		// For now, just confirm the issue exists
		green := color.New(color.FgGreen).SprintFunc()
		yellow := color.New(color.FgYellow).SprintFunc()
		fmt.Printf("\n%s Found issue: %s - %s\n", green("✓"), issue.ID, issue.Title)
		fmt.Printf("%s Plan generation not yet implemented (see vc-3yi1)\n\n", yellow("⚠"))
	},
}

var planNewCmd = &cobra.Command{
	Use:   "new <description>",
	Short: "Create a new mission plan from a freeform description",
	Long: `Create a new mission plan from a natural language description.
This creates both the plan and the mission issue in the tracker.`,
	Args: cobra.MinimumNArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		description := args[0]

		// TODO: Implement plan creation from description (vc-26hh)
		// This should:
		// 1. Use AI to parse the description into a mission
		// 2. Generate a draft plan
		// 3. Store both in the database

		yellow := color.New(color.FgYellow).SprintFunc()
		fmt.Printf("\n%s Creating plan from: %s\n", yellow("⚠"), description)
		fmt.Printf("%s 'vc plan new' not yet implemented (see vc-26hh)\n\n", yellow("⚠"))
	},
}

var planShowCmd = &cobra.Command{
	Use:   "show <mission-id>",
	Short: "Display a plan's structure and details",
	Long: `Display a mission plan as a tree structure showing phases and tasks.
This includes estimates, dependencies, and validation status.`,
	Args: cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		missionID := args[0]
		ctx := context.Background()

		// Get the plan from storage
		plan, iteration, err := store.GetPlan(ctx, missionID)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: failed to get plan: %v\n", err)
			os.Exit(1)
		}
		if plan == nil {
			fmt.Fprintf(os.Stderr, "Error: no plan found for mission %s\n", missionID)
			os.Exit(1)
		}

		// Display plan summary
		cyan := color.New(color.FgCyan, color.Bold).SprintFunc()
		gray := color.New(color.FgHiBlack).SprintFunc()

		fmt.Printf("\n%s\n", cyan("=== Mission Plan ==="))
		fmt.Printf("Mission: %s\n", plan.MissionID)
		fmt.Printf("Status: %s\n", plan.Status)
		fmt.Printf("Iteration: %d\n", iteration)
		fmt.Printf("Confidence: %.0f%%\n", plan.Confidence*100)
		fmt.Printf("Estimated Effort: %s\n", plan.EstimatedEffort)
		fmt.Println()

		// Display phases
		fmt.Printf("%s (%d phases):\n", cyan("Phases"), len(plan.Phases))
		for _, phase := range plan.Phases {
			fmt.Printf("\n  Phase %d: %s\n", phase.PhaseNumber, phase.Title)
			fmt.Printf("  %s %s\n", gray("├─"), phase.Description)
			fmt.Printf("  %s Strategy: %s\n", gray("├─"), phase.Strategy)
			fmt.Printf("  %s Effort: %s\n", gray("├─"), phase.EstimatedEffort)
			fmt.Printf("  %s Tasks: %d\n", gray("└─"), len(phase.Tasks))
			for i, task := range phase.Tasks {
				prefix := "    "
				if i == len(phase.Tasks)-1 {
					fmt.Printf("%s%s %s\n", prefix, gray("└─"), task)
				} else {
					fmt.Printf("%s%s %s\n", prefix, gray("├─"), task)
				}
			}
		}
		fmt.Println()
	},
}

var planListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all draft plans",
	Long:  `List all mission plans that have not been approved yet.`,
	Run: func(cmd *cobra.Command, args []string) {
		ctx := context.Background()

		// Get all draft plans
		plans, err := store.ListDraftPlans(ctx)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: failed to list plans: %v\n", err)
			os.Exit(1)
		}

		if len(plans) == 0 {
			yellow := color.New(color.FgYellow).SprintFunc()
			fmt.Printf("\n%s No draft plans found\n\n", yellow("✨"))
			return
		}

		cyan := color.New(color.FgCyan).SprintFunc()
		gray := color.New(color.FgHiBlack).SprintFunc()

		fmt.Printf("\n%s Draft plans (%d):\n\n", cyan("📋"), len(plans))

		for i, plan := range plans {
			statusColor := getStatusColor(plan.Status)
			fmt.Printf("%d. %s - %s\n", i+1, plan.MissionID, statusColor(plan.Status))
			fmt.Printf("   %s Phases: %d, Confidence: %.0f%%, Effort: %s\n",
				gray("├─"), len(plan.Phases), plan.Confidence*100, plan.EstimatedEffort)
		}
		fmt.Println()
	},
}

var planRefineCmd = &cobra.Command{
	Use:   "refine <mission-id>",
	Short: "Refine a plan with AI feedback",
	Long: `Iteratively refine a mission plan using AI-guided convergence.
This runs multiple refinement iterations until the plan stabilizes.`,
	Args: cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		missionID := args[0]

		// TODO: Implement plan refinement (Epic 2: vc-3yi1)
		yellow := color.New(color.FgYellow).SprintFunc()
		fmt.Printf("\n%s 'vc plan refine' not yet implemented (see vc-3yi1)\n", yellow("⚠"))
		fmt.Printf("Mission: %s\n\n", missionID)
	},
}

var planValidateCmd = &cobra.Command{
	Use:   "validate <mission-id>",
	Short: "Validate a plan against quality checks",
	Long: `Run validation checks on a mission plan to ensure quality.
This includes dependency checks, effort estimates, and completeness validation.`,
	Args: cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		missionID := args[0]

		// TODO: Implement plan validation (Epic 3: vc-pob3)
		yellow := color.New(color.FgYellow).SprintFunc()
		fmt.Printf("\n%s 'vc plan validate' not yet implemented (see vc-pob3)\n", yellow("⚠"))
		fmt.Printf("Mission: %s\n\n", missionID)
	},
}

var planApproveCmd = &cobra.Command{
	Use:   "approve <mission-id>",
	Short: "Approve a plan and create Beads issues",
	Long: `Approve a validated plan and atomically create all phase and task issues in Beads.
This marks the plan as approved and makes it immutable.`,
	Args: cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		missionID := args[0]

		// TODO: Implement plan approval (Epic 5: vc-apx8)
		yellow := color.New(color.FgYellow).SprintFunc()
		fmt.Printf("\n%s 'vc plan approve' not yet implemented (see vc-apx8)\n", yellow("⚠"))
		fmt.Printf("Mission: %s\n\n", missionID)
	},
}

// getStatusColor returns a color function for a plan status
func getStatusColor(status string) func(...interface{}) string {
	switch status {
	case "draft":
		return color.New(color.FgYellow).SprintFunc()
	case "refining":
		return color.New(color.FgCyan).SprintFunc()
	case "validated":
		return color.New(color.FgGreen).SprintFunc()
	case "approved":
		return color.New(color.FgBlue).SprintFunc()
	default:
		return color.New(color.FgWhite).SprintFunc()
	}
}

func init() {
	// Add subcommands to plan command
	planCmd.AddCommand(planGenerateCmd)
	planCmd.AddCommand(planNewCmd)
	planCmd.AddCommand(planShowCmd)
	planCmd.AddCommand(planListCmd)
	planCmd.AddCommand(planRefineCmd)
	planCmd.AddCommand(planValidateCmd)
	planCmd.AddCommand(planApproveCmd)

	// Register plan command with root
	rootCmd.AddCommand(planCmd)
}
