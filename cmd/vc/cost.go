package main

import (
	"fmt"
	"os"

	"github.com/fatih/color"
	"github.com/spf13/cobra"
	"github.com/steveyegge/vc/internal/cost"
)

var costCmd = &cobra.Command{
	Use:   "cost",
	Short: "Show AI cost budget and usage statistics",
	Long:  `Display current AI cost budget status, usage statistics, and spending history.`,
	Run: func(cmd *cobra.Command, args []string) {
		// Load cost configuration
		cfg := cost.LoadFromEnv()

		if !cfg.Enabled {
			fmt.Println("Cost budgeting is disabled")
			fmt.Println("Set VC_COST_ENABLED=true to enable cost tracking")
			return
		}

		// Initialize cost tracker
		tracker, err := cost.NewTracker(cfg, store)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: failed to initialize cost tracker: %v\n", err)
			os.Exit(1)
		}

		// Get current stats
		stats := tracker.GetStats()

		// Display header
		cyan := color.New(color.FgCyan, color.Bold).SprintFunc()
		fmt.Printf("\n%s\n", cyan("=== AI Cost Budget Status ==="))
		fmt.Println()

		// Display budget status
		statusColor := color.New(color.FgGreen)
		statusIcon := "✓"
		if stats.Status == cost.BudgetWarning {
			statusColor = color.New(color.FgYellow)
			statusIcon = "⚠️"
		} else if stats.Status == cost.BudgetExceeded {
			statusColor = color.New(color.FgRed, color.Bold)
			statusIcon = "🚨"
		}

		fmt.Printf("%s Budget Status: %s\n", statusIcon, statusColor.Sprint(stats.Status.String()))
		fmt.Println()

		// Display hourly budget usage
		yellow := color.New(color.FgYellow).SprintFunc()
		fmt.Printf("%s\n", yellow("Hourly Budget:"))

		if cfg.MaxTokensPerHour > 0 {
			tokenPercent := float64(stats.HourlyTokensUsed) / float64(cfg.MaxTokensPerHour) * 100
			fmt.Printf("  Tokens:  %s / %d (%.1f%%)\n",
				formatTokens(stats.HourlyTokensUsed),
				cfg.MaxTokensPerHour,
				tokenPercent)

			// Show progress bar
			fmt.Printf("           %s\n", renderProgressBar(tokenPercent, 40))
		} else {
			fmt.Printf("  Tokens:  %s (unlimited)\n", formatTokens(stats.HourlyTokensUsed))
		}

		if cfg.MaxCostPerHour > 0 {
			costPercent := stats.HourlyCostUsed / cfg.MaxCostPerHour * 100
			fmt.Printf("  Cost:    $%.4f / $%.2f (%.1f%%)\n",
				stats.HourlyCostUsed,
				cfg.MaxCostPerHour,
				costPercent)

			// Show progress bar
			fmt.Printf("           %s\n", renderProgressBar(costPercent, 40))
		} else {
			fmt.Printf("  Cost:    $%.4f (unlimited)\n", stats.HourlyCostUsed)
		}

		fmt.Printf("  Window:  %s → %s\n",
			stats.WindowStartTime.Format("15:04:05"),
			stats.WindowStartTime.Add(cfg.BudgetResetInterval).Format("15:04:05"))
		fmt.Println()

		// Display all-time usage
		fmt.Printf("%s\n", yellow("All-Time Usage:"))
		fmt.Printf("  Tokens:  %s\n", formatTokens(stats.TotalTokensUsed))
		fmt.Printf("  Cost:    $%.2f\n", stats.TotalCostUsed)
		if stats.TotalTokensUsed > 0 {
			avgCostPerToken := stats.TotalCostUsed / float64(stats.TotalTokensUsed) * 1_000_000
			fmt.Printf("  Avg:     $%.2f per 1M tokens\n", avgCostPerToken)
		}
		fmt.Println()

		// Display configuration
		fmt.Printf("%s\n", yellow("Configuration:"))
		fmt.Printf("  Alert Threshold:    %.0f%%\n", cfg.AlertThreshold*100)
		fmt.Printf("  Budget Reset:       %v\n", cfg.BudgetResetInterval)
		fmt.Printf("  State Persistence:  %s\n", cfg.PersistStatePath)
		fmt.Println()

		// Display pricing
		fmt.Printf("%s\n", yellow("Pricing (per 1M tokens):"))
		fmt.Printf("  Input:   $%.2f\n", cfg.InputTokenCost)
		fmt.Printf("  Output:  $%.2f\n", cfg.OutputTokenCost)
		fmt.Println()
	},
}

func init() {
	rootCmd.AddCommand(costCmd)
}

// formatTokens formats a token count with commas for readability
func formatTokens(tokens int64) string {
	if tokens < 1000 {
		return fmt.Sprintf("%d", tokens)
	} else if tokens < 1_000_000 {
		return fmt.Sprintf("%.1fK", float64(tokens)/1000)
	} else {
		return fmt.Sprintf("%.2fM", float64(tokens)/1_000_000)
	}
}

// renderProgressBar renders a text-based progress bar
func renderProgressBar(percent float64, width int) string {
	if percent < 0 {
		percent = 0
	}
	if percent > 100 {
		percent = 100
	}

	filled := int(percent / 100.0 * float64(width))
	bar := ""

	// Choose color based on percentage
	var barColor *color.Color
	if percent >= 100 {
		barColor = color.New(color.FgRed, color.Bold)
	} else if percent >= 80 {
		barColor = color.New(color.FgYellow)
	} else {
		barColor = color.New(color.FgGreen)
	}

	for i := 0; i < width; i++ {
		if i < filled {
			bar += barColor.Sprint("█")
		} else {
			bar += color.New(color.FgHiBlack).Sprint("░")
		}
	}

	return fmt.Sprintf("[%s]", bar)
}
