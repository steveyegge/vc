package executor

import (
	"context"
	"fmt"
	"time"

	"github.com/steveyegge/vc/internal/cost"
	"github.com/steveyegge/vc/internal/events"
)

// checkBudgetBeforeWork checks if we're within budget limits before processing work
// Returns true if we should proceed, false if we should pause
func (e *Executor) checkBudgetBeforeWork(ctx context.Context) bool {
	if e.costTracker == nil {
		return true // No budget tracking, proceed
	}

	status := e.costTracker.CheckBudget()

	// If budget exceeded, pause and wait
	if status == cost.BudgetExceeded {
		canProceed, reason := e.costTracker.CanProceed("")
		if !canProceed {
			// Log clear warning to console and activity feed
			stats := e.costTracker.GetStats()

			// Calculate time until reset
			resetTime := stats.WindowStartTime.Add(stats.Config.BudgetResetInterval)
			timeUntilReset := time.Until(resetTime)

			// Log to console (first time only, to avoid spam)
			fmt.Printf("\n")
			fmt.Printf("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n")
			fmt.Printf("🚨 AI BUDGET EXCEEDED - EXECUTOR PAUSED\n")
			fmt.Printf("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n")
			fmt.Printf("\n")
			fmt.Printf("Budget Limit:  $%.2f/hour\n", stats.Config.MaxCostPerHour)
			fmt.Printf("Current Usage: $%.2f (%.0f%%)\n", stats.HourlyCostUsed,
				(stats.HourlyCostUsed/stats.Config.MaxCostPerHour)*100)
			fmt.Printf("Tokens Used:   %d / %d\n", stats.HourlyTokensUsed, stats.Config.MaxTokensPerHour)
			fmt.Printf("\n")
			fmt.Printf("Reason: %s\n", reason)
			fmt.Printf("\n")
			fmt.Printf("⏰ Budget resets in: %v\n", timeUntilReset.Round(time.Second))
			fmt.Printf("   Reset time: %s\n", resetTime.Format("15:04:05 MST"))
			fmt.Printf("\n")
			fmt.Printf("The executor will resume automatically when the budget resets.\n")
			fmt.Printf("You can check budget status with: vc cost\n")
			fmt.Printf("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n")
			fmt.Printf("\n")

			// Log to activity feed (clear event for tail/activity commands)
			e.logEvent(ctx, events.EventTypeBudgetAlert, events.SeverityCritical, "SYSTEM",
				fmt.Sprintf("🚨 EXECUTOR PAUSED: AI budget exceeded - Budget: $%.2f/hour, Used: $%.2f (%.0f%%), Resets in: %v",
					stats.Config.MaxCostPerHour,
					stats.HourlyCostUsed,
					(stats.HourlyCostUsed/stats.Config.MaxCostPerHour)*100,
					timeUntilReset.Round(time.Minute)),
				map[string]interface{}{
					"paused":             true,
					"budget_status":      "EXCEEDED",
					"hourly_cost_limit":  stats.Config.MaxCostPerHour,
					"hourly_cost_used":   stats.HourlyCostUsed,
					"hourly_tokens_used": stats.HourlyTokensUsed,
					"max_tokens":         stats.Config.MaxTokensPerHour,
					"reset_time":         resetTime.Format(time.RFC3339),
					"time_until_reset":   timeUntilReset.String(),
					"reason":             reason,
				})

			return false
		}
	}

	// If approaching budget (warning), log but continue
	if status == cost.BudgetWarning {
		stats := e.costTracker.GetStats()
		tokenPercent := float64(stats.HourlyTokensUsed) / float64(stats.Config.MaxTokensPerHour) * 100
		costPercent := stats.HourlyCostUsed / stats.Config.MaxCostPerHour * 100

		// Log warning to activity feed (but don't pause)
		e.logEvent(ctx, events.EventTypeBudgetAlert, events.SeverityWarning, "SYSTEM",
			fmt.Sprintf("⚠️  Budget Warning: Approaching hourly limits (%.0f%% tokens, %.0f%% cost)",
				tokenPercent, costPercent),
			map[string]interface{}{
				"budget_status":      "WARNING",
				"hourly_cost_used":   stats.HourlyCostUsed,
				"hourly_cost_limit":  stats.Config.MaxCostPerHour,
				"cost_percent":       costPercent,
				"hourly_tokens_used": stats.HourlyTokensUsed,
				"max_tokens":         stats.Config.MaxTokensPerHour,
				"token_percent":      tokenPercent,
			})
	}

	return true // Proceed with work
}

// GetBudgetStatus returns the current budget status for status commands
func (e *Executor) GetBudgetStatus() (enabled bool, status string, stats cost.BudgetStats) {
	if e.costTracker == nil {
		return false, "DISABLED", cost.BudgetStats{}
	}

	stats = e.costTracker.GetStats()
	return true, stats.Status.String(), stats
}
