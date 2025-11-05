package main

import (
	"context"
	"fmt"
	"os"

	"github.com/fatih/color"
	"github.com/spf13/cobra"
	"github.com/steveyegge/vc/internal/events"
)

// Note: displayActivityEvent and related helper functions are in event_display.go

var activityCmd = &cobra.Command{
	Use:   "activity",
	Short: "Show recent agent execution events",
	Long: `Display recent activity from the VC executor.

Shows events from the agent_events table including:
- Issue claims and completions
- AI assessments and analysis
- Agent spawns and executions
- File modifications and git operations
- Test runs and build output
- Context usage monitoring
- Errors and warnings
- Watchdog alerts and interventions

Use filters to narrow down events by issue, type, or severity.

Examples:
  vc activity                              # Show last 20 events
  vc activity -n 50                        # Show last 50 events
  vc activity --issue vc-123               # Show events for specific issue
  vc activity --type error                 # Show only error events
  vc activity --type context_usage         # Show context usage events
  vc activity --severity warning           # Show warnings and above
  vc activity --type git_operation -n 10   # Show last 10 git operations`,
	Run: func(cmd *cobra.Command, args []string) {
		limit, _ := cmd.Flags().GetInt("limit")
		issueID, _ := cmd.Flags().GetString("issue")
		eventType, _ := cmd.Flags().GetString("type")
		severity, _ := cmd.Flags().GetString("severity")

		ctx := context.Background()

		// Build filter
		filter := events.EventFilter{
			Limit: limit,
		}
		if issueID != "" {
			filter.IssueID = issueID
		}
		if eventType != "" {
			filter.Type = events.EventType(eventType)
		}
		if severity != "" {
			filter.Severity = events.EventSeverity(severity)
		}

		// Fetch events
		var eventList []*events.AgentEvent
		var err error

		// Use optimized queries when possible
		if issueID != "" && eventType == "" && severity == "" {
			eventList, err = store.GetAgentEventsByIssue(ctx, issueID)
		} else if issueID == "" && eventType == "" && severity == "" {
			eventList, err = store.GetRecentAgentEvents(ctx, limit)
		} else {
			eventList, err = store.GetAgentEvents(ctx, filter)
		}

		if err != nil {
			fmt.Fprintf(os.Stderr, "Error fetching events: %v\n", err)
			os.Exit(1)
		}

		// Display results
		if len(eventList) == 0 {
			yellow := color.New(color.FgYellow).SprintFunc()
			fmt.Printf("\n%s No events found matching the criteria\n\n", yellow("✨"))
			return
		}

		cyan := color.New(color.FgCyan).SprintFunc()
		fmt.Printf("\n%s Recent Activity (%d events):\n\n", cyan("📋"), len(eventList))

		// Display events in reverse chronological order (newest last, so we read top to bottom)
		for i := len(eventList) - 1; i >= 0; i-- {
			displayActivityEvent(eventList[i])
		}

		fmt.Println()
	},
}

func init() {
	activityCmd.Flags().IntP("limit", "n", 20, "Number of recent events to show")
	activityCmd.Flags().StringP("issue", "i", "", "Filter events by issue ID")
	activityCmd.Flags().StringP("type", "t", "", "Filter by event type (e.g., error, git_operation, test_run)")
	activityCmd.Flags().StringP("severity", "s", "", "Filter by severity (info, warning, error, critical)")
	rootCmd.AddCommand(activityCmd)
}
