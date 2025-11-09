package executor

import (
	"context"
	"fmt"
	"strings"

	"github.com/steveyegge/vc/internal/cost"
	"github.com/steveyegge/vc/internal/events"
	"github.com/steveyegge/vc/internal/types"
)

// ShouldUseBootstrapMode determines if bootstrap mode should be activated for an issue
// Bootstrap mode activates when:
// 1. Bootstrap mode is enabled in config AND
// 2. Budget is exceeded AND
// 3. Issue has quota-crisis label OR issue title contains quota/budget/cost keywords
//
// vc-b027: Bootstrap mode for quota crisis scenarios
func (e *Executor) ShouldUseBootstrapMode(ctx context.Context, issue *types.Issue) (bool, string) {
	// Bootstrap mode must be explicitly enabled
	if !e.config.EnableBootstrapMode {
		return false, ""
	}

	// Check if budget is exceeded
	if e.costTracker == nil {
		return false, ""
	}

	budgetStatus := e.costTracker.CheckBudget()
	if budgetStatus != cost.BudgetExceeded {
		return false, ""
	}

	// Check if issue has quota-crisis label
	labels, err := e.store.GetLabels(ctx, issue.ID)
	if err == nil {
		for _, label := range labels {
			for _, bootstrapLabel := range e.config.BootstrapModeLabels {
				if label == bootstrapLabel {
					reason := fmt.Sprintf("budget_exceeded + label:%s", label)
					return true, reason
				}
			}
		}
	}

	// Check if issue title contains quota keywords
	lowerTitle := strings.ToLower(issue.Title)
	for _, keyword := range e.config.BootstrapModeTitleKeywords {
		if strings.Contains(lowerTitle, strings.ToLower(keyword)) {
			reason := fmt.Sprintf("budget_exceeded + title_keyword:%s", keyword)
			return true, reason
		}
	}

	return false, ""
}

// logBootstrapModeActivation logs that bootstrap mode was activated for an issue
func (e *Executor) logBootstrapModeActivation(ctx context.Context, issue *types.Issue, reason string) {
	// Get budget stats for context
	budgetStats := e.costTracker.GetStats()

	// Log prominent warning
	fmt.Printf("⚠️  BOOTSTRAP MODE ACTIVATED for %s (reason: %s)\n", issue.ID, reason)
	fmt.Printf("   Budget status: %s (hourly: %d/%d tokens, $%.2f/$%.2f)\n",
		budgetStats.Status, budgetStats.HourlyTokensUsed, budgetStats.Config.MaxTokensPerHour,
		budgetStats.HourlyCostUsed, budgetStats.Config.MaxCostPerHour)
	fmt.Printf("   ⚠️  LIMITED AI SUPERVISION: No assessment, no analysis, no discovered issues\n")

	// Emit activity feed event
	e.logEvent(ctx, events.EventTypeBootstrapModeActivated, events.SeverityWarning, issue.ID,
		fmt.Sprintf("Bootstrap mode activated for issue %s: %s", issue.ID, reason),
		map[string]interface{}{
			"reason":              reason,
			"budget_status":       budgetStats.Status.String(),
			"hourly_tokens_used":  budgetStats.HourlyTokensUsed,
			"hourly_tokens_limit": budgetStats.Config.MaxTokensPerHour,
			"hourly_cost_used":    budgetStats.HourlyCostUsed,
			"hourly_cost_limit":   budgetStats.Config.MaxCostPerHour,
		})

	// Add comment to issue noting bootstrap mode
	comment := fmt.Sprintf("⚠️ **BOOTSTRAP MODE ACTIVE**\n\n"+
		"This issue is being executed in bootstrap mode due to quota exhaustion.\n\n"+
		"**Limitations:**\n"+
		"- No AI assessment (pre-flight checks)\n"+
		"- No AI analysis (quality validation)\n"+
		"- No discovered issue creation (follow-on work)\n"+
		"- No deduplication (risk of duplicates)\n\n"+
		"**Quality gates still enforce:**\n"+
		"- Tests must pass\n"+
		"- Linting must pass\n"+
		"- Build must succeed\n\n"+
		"Reason: %s\n"+
		"Budget: %d/%d tokens used ($%.2f/$%.2f)",
		reason,
		budgetStats.HourlyTokensUsed, budgetStats.Config.MaxTokensPerHour,
		budgetStats.HourlyCostUsed, budgetStats.Config.MaxCostPerHour)

	if err := e.store.AddComment(ctx, issue.ID, "ai-supervisor", comment); err != nil {
		fmt.Printf("Warning: failed to add bootstrap mode comment: %v\n", err)
	}
}
