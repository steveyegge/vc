package main

import (
	"context"
	"fmt"
	"os"

	"github.com/fatih/color"
	"github.com/spf13/cobra"
	"github.com/steveyegge/vc/internal/types"
)

var resumeCmd = &cobra.Command{
	Use:   "resume <issue-id>",
	Short: "Resume a paused task",
	Long: `Resume a previously paused task from its saved state.

The executor will restart the agent with the saved context (todos, notes,
progress) from when the task was interrupted. The agent receives a brief
explaining that it was interrupted and where it left off.

Note: This command requires the executor to be running. If the executor
was stopped, use 'vc execute <issue-id>' instead - the executor will
automatically detect and load the interrupt metadata.`,
	Args: cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		issueID := args[0]

		ctx := context.Background()

		// Check if issue exists and has interrupt metadata
		issue, err := store.GetIssue(ctx, issueID)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		if issue == nil {
			fmt.Fprintf(os.Stderr, "Error: issue %s not found\n", issueID)
			os.Exit(1)
		}

		// Check if issue has interrupt metadata
		labels, _ := store.GetLabels(ctx, issueID)
		hasInterrupted := false
		for _, label := range labels {
			if label == "interrupted" {
				hasInterrupted = true
				break
			}
		}

		if !hasInterrupted {
			yellow := color.New(color.FgYellow).SprintFunc()
			fmt.Printf("%s Warning: Issue %s does not appear to be interrupted\n", yellow("!"), issueID)
			fmt.Printf("  Status: %s\n", issue.Status)
			fmt.Printf("\n")
		}

		// Try to find running executor first (hot resume via RPC)
		socketPath, socketErr := findExecutorSocket()
		if socketErr == nil {
			// Executor is running - use RPC to trigger resume
			fmt.Printf("Found running executor, sending resume command...\n")
			// Note: Resume via RPC is not strictly necessary since executor
			// will pick up the issue from ready work queue anyway.
			// We just validate that the issue is ready and remove interrupted label.

			// For now, just print a message - the actual resume logic will be
			// in the executor when it claims the issue
			yellow := color.New(color.FgYellow).SprintFunc()
			fmt.Printf("%s Resume via RPC not yet implemented\n", yellow("⚠"))
			fmt.Printf("  The issue will be picked up automatically by the executor.\n")
			fmt.Printf("  Removing 'interrupted' label to make it ready for claiming...\n\n")
		}

		// Remove 'interrupted' label to make issue ready for claiming
		if hasInterrupted {
			if err := store.RemoveLabel(ctx, issueID, "interrupted", actor); err != nil {
				fmt.Fprintf(os.Stderr, "Error: failed to remove interrupted label: %v\n", err)
				os.Exit(1)
			}
		}

		// Ensure issue is in 'open' status
		if issue.Status != types.StatusOpen {
			updates := map[string]interface{}{
				"status": types.StatusOpen,
			}
			if err := store.UpdateIssue(ctx, issueID, updates, actor); err != nil {
				fmt.Fprintf(os.Stderr, "Error: failed to update issue status: %v\n", err)
				os.Exit(1)
			}
		}

		green := color.New(color.FgGreen).SprintFunc()
		fmt.Printf("%s Issue %s ready for resume\n", green("✓"), issueID)
		fmt.Printf("  Status: %s\n", types.StatusOpen)

		if socketErr == nil {
			fmt.Printf("  The running executor will pick this up automatically.\n")
		} else {
			fmt.Printf("\n  No executor running. Start one with:\n")
			fmt.Printf("    vc execute --continuous\n")
			fmt.Printf("  or:\n")
			fmt.Printf("    vc execute %s\n", issueID)
		}
	},
}

func init() {
	rootCmd.AddCommand(resumeCmd)
}
