package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/fatih/color"
	"github.com/spf13/cobra"
	"github.com/steveyegge/vc/internal/cost"
	"github.com/steveyegge/vc/internal/types"
)

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show executor status and budget information",
	Long:  `Display executor instance status, AI cost budget, and system health.`,
	Run: func(cmd *cobra.Command, args []string) {
		ctx := context.Background()

		// Print header
		cyan := color.New(color.FgCyan, color.Bold).SprintFunc()
		fmt.Printf("\n%s\n", cyan("=== VC Executor Status ==="))
		fmt.Println()

		// Get active executor instances
		instances, err := store.GetActiveInstances(ctx)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: failed to get executor instances: %v\n", err)
			os.Exit(1)
		}

		// Display executor instances
		yellow := color.New(color.FgYellow).SprintFunc()
		green := color.New(color.FgGreen).SprintFunc()
		red := color.New(color.FgRed).SprintFunc()
		gray := color.New(color.FgHiBlack).SprintFunc()

		fmt.Printf("%s\n", yellow("Executor Instances:"))

		if len(instances) == 0 {
			fmt.Printf("  %s\n", gray("No active executors"))
		} else {
			runningCount := 0
			stoppedCount := 0

			for _, inst := range instances {
				statusColor := gray
				statusIcon := "○"
				statusText := inst.Status

				if inst.Status == "running" {
					runningCount++
					statusColor = green
					statusIcon = "●"

					// Check if heartbeat is stale (more than 2 minutes old)
					timeSinceHeartbeat := time.Since(inst.LastHeartbeat)
					if timeSinceHeartbeat > 2*time.Minute {
						statusColor = color.New(color.FgYellow).SprintFunc()
						statusIcon = "⚠"
					}
				} else if inst.Status == "stopped" {
					stoppedCount++
					statusColor = gray
					statusIcon = "○"
				}

				// Format instance info
				fmt.Printf("  %s %s\n", statusColor(statusIcon), statusColor(statusText))
				fmt.Printf("    Instance: %s\n", inst.InstanceID)
				fmt.Printf("    Host:     %s (PID %d)\n", inst.Hostname, inst.PID)
				fmt.Printf("    Started:  %s\n", inst.StartedAt.Format("2006-01-02 15:04:05"))
				fmt.Printf("    Heartbeat: %s (%v ago)\n",
					inst.LastHeartbeat.Format("15:04:05"),
					time.Since(inst.LastHeartbeat).Round(time.Second))

				fmt.Println()
			}

			// Summary
			if runningCount > 0 {
				fmt.Printf("  Total: %s running", green(fmt.Sprintf("%d", runningCount)))
				if stoppedCount > 0 {
					fmt.Printf(", %s stopped", gray(fmt.Sprintf("%d", stoppedCount)))
				}
				fmt.Println()
			} else {
				fmt.Printf("  Total: %s stopped\n", gray(fmt.Sprintf("%d", stoppedCount)))
			}
		}

		fmt.Println()

		// Display AI cost budget status
		fmt.Printf("%s\n", yellow("AI Cost Budget:"))

		costConfig := cost.LoadFromEnv()
		if !costConfig.Enabled {
			fmt.Printf("  %s\n", gray("Budget tracking disabled"))
			fmt.Printf("  Set VC_COST_ENABLED=true to enable\n")
		} else {
			tracker, err := cost.NewTracker(costConfig, store)
			if err != nil {
				fmt.Printf("  %s Failed to initialize: %v\n", red("✗"), err)
			} else {
				stats := tracker.GetStats()

				// Status indicator
				statusColor := green
				statusIcon := "✓"
				if stats.Status == cost.BudgetWarning {
					statusColor = color.New(color.FgYellow).SprintFunc()
					statusIcon = "⚠️"
				} else if stats.Status == cost.BudgetExceeded {
					statusColor = red
					statusIcon = "🚨"
				}

				fmt.Printf("  %s Status: %s\n", statusIcon, statusColor(stats.Status.String()))

				// Budget details
				if costConfig.MaxCostPerHour > 0 {
					costPercent := stats.HourlyCostUsed / costConfig.MaxCostPerHour * 100
					fmt.Printf("  Cost:   $%.4f / $%.2f (%.0f%%)\n",
						stats.HourlyCostUsed, costConfig.MaxCostPerHour, costPercent)
				} else {
					fmt.Printf("  Cost:   $%.4f (unlimited)\n", stats.HourlyCostUsed)
				}

				if costConfig.MaxTokensPerHour > 0 {
					tokenPercent := float64(stats.HourlyTokensUsed) / float64(costConfig.MaxTokensPerHour) * 100
					fmt.Printf("  Tokens: %d / %d (%.0f%%)\n",
						stats.HourlyTokensUsed, costConfig.MaxTokensPerHour, tokenPercent)
				} else {
					fmt.Printf("  Tokens: %d (unlimited)\n", stats.HourlyTokensUsed)
				}

				// Reset time
				resetTime := stats.WindowStartTime.Add(costConfig.BudgetResetInterval)
				timeUntilReset := time.Until(resetTime)
				if timeUntilReset < 0 {
					fmt.Printf("  Resets: now (overdue)\n")
				} else {
					fmt.Printf("  Resets: in %v (%s)\n",
						timeUntilReset.Round(time.Minute),
						resetTime.Format("15:04 MST"))
				}

				// Warning if paused
				if stats.Status == cost.BudgetExceeded {
					fmt.Printf("\n")
					fmt.Printf("  %s Executor is PAUSED due to budget exceeded\n", red("⚠️"))
					fmt.Printf("  Run 'vc cost' for detailed budget information\n")
				}
			}
		}

		fmt.Println()

		// Display ready work count
		fmt.Printf("%s\n", yellow("Ready Work:"))
		readyIssues, err := store.GetReadyWork(ctx, types.WorkFilter{})
		if err != nil {
			fmt.Printf("  %s Failed to get ready work: %v\n", red("✗"), err)
		} else {
			fmt.Printf("  %d issues ready to work\n", len(readyIssues))
			fmt.Printf("  Run 'vc ready' to see details\n")
		}

		fmt.Println()
	},
}

func init() {
	rootCmd.AddCommand(statusCmd)
}
