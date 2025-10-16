package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/fatih/color"
	"github.com/spf13/cobra"
	"github.com/steveyegge/vc/internal/events"
)

var tailCmd = &cobra.Command{
	Use:   "tail",
	Short: "Watch VC execution in real-time",
	Long: `Display recent activity from the VC executor and follow live updates.

Shows events from the agent_events table including:
- Issue claims
- AI assessments
- Agent executions
- Completions
- Errors and warnings

Also shows comments from the events table for additional context.`,
	Run: func(cmd *cobra.Command, args []string) {
		follow, _ := cmd.Flags().GetBool("follow")
		issueID, _ := cmd.Flags().GetString("issue")
		limit, _ := cmd.Flags().GetInt("limit")

		ctx := context.Background()

		if follow {
			runTailFollow(ctx, issueID, limit)
		} else {
			runTailOnce(ctx, issueID, limit)
		}
	},
}

func init() {
	tailCmd.Flags().BoolP("follow", "f", false, "Follow mode - watch for live updates (Ctrl+C to stop)")
	tailCmd.Flags().StringP("issue", "i", "", "Filter events by issue ID")
	tailCmd.Flags().IntP("limit", "n", 20, "Number of recent events to show initially")
	rootCmd.AddCommand(tailCmd)
}

// runTailOnce shows recent events and exits
func runTailOnce(ctx context.Context, issueID string, limit int) {
	events, err := fetchEvents(ctx, issueID, limit)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error fetching events: %v\n", err)
		os.Exit(1)
	}

	if len(events) == 0 {
		yellow := color.New(color.FgYellow).SprintFunc()
		if issueID != "" {
			fmt.Printf("\n%s No events found for issue %s\n\n", yellow("✨"), issueID)
		} else {
			fmt.Printf("\n%s No events found\n\n", yellow("✨"))
		}
		return
	}

	// Display events in reverse chronological order (newest last)
	for i := len(events) - 1; i >= 0; i-- {
		displayEvent(events[i])
	}
}

// runTailFollow shows recent events and continues polling for new ones
func runTailFollow(ctx context.Context, issueID string, initialLimit int) {
	// Set up signal handling for graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	cyan := color.New(color.FgCyan).SprintFunc()
	fmt.Printf("\n%s Following live updates (Ctrl+C to stop)...\n\n", cyan("👁️"))

	// Show initial events
	events, err := fetchEvents(ctx, issueID, initialLimit)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error fetching events: %v\n", err)
		os.Exit(1)
	}

	// Display initial events in reverse chronological order
	for i := len(events) - 1; i >= 0; i-- {
		displayEvent(events[i])
	}

	// Track the most recent event timestamp
	var lastTimestamp time.Time
	if len(events) > 0 {
		lastTimestamp = events[0].Timestamp
	}

	// Poll for new events
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-sigChan:
			fmt.Println("\n\nStopped following")
			return
		case <-ticker.C:
			// Fetch events newer than the last one we saw
			newEvents, err := fetchEventsAfter(ctx, issueID, lastTimestamp)
			if err != nil {
				fmt.Fprintf(os.Stderr, "\nError fetching new events: %v\n", err)
				continue
			}

			// Display new events in chronological order
			for i := len(newEvents) - 1; i >= 0; i-- {
				displayEvent(newEvents[i])
				if newEvents[i].Timestamp.After(lastTimestamp) {
					lastTimestamp = newEvents[i].Timestamp
				}
			}
		}
	}
}

// fetchEvents retrieves events based on the given filters
func fetchEvents(ctx context.Context, issueID string, limit int) ([]*events.AgentEvent, error) {
	if issueID != "" {
		return store.GetAgentEventsByIssue(ctx, issueID)
	}
	return store.GetRecentAgentEvents(ctx, limit)
}

// fetchEventsAfter retrieves events that occurred after the given timestamp
func fetchEventsAfter(ctx context.Context, issueID string, afterTime time.Time) ([]*events.AgentEvent, error) {
	filter := events.EventFilter{
		AfterTime: afterTime,
		Limit:     100, // Fetch up to 100 new events at a time
	}
	if issueID != "" {
		filter.IssueID = issueID
	}
	return store.GetAgentEvents(ctx, filter)
}

// displayEvent formats and prints a single event with color
func displayEvent(event *events.AgentEvent) {
	// Color coding by severity
	var severityColor *color.Color
	var severityIcon string

	switch event.Severity {
	case events.SeverityInfo:
		severityColor = color.New(color.FgCyan)
		severityIcon = "ℹ️"
	case events.SeverityWarning:
		severityColor = color.New(color.FgYellow)
		severityIcon = "⚠️"
	case events.SeverityError:
		severityColor = color.New(color.FgRed)
		severityIcon = "❌"
	case events.SeverityCritical:
		severityColor = color.New(color.FgRed, color.Bold)
		severityIcon = "🔥"
	default:
		severityColor = color.New(color.FgWhite)
		severityIcon = "•"
	}

	// Format timestamp
	timestamp := event.Timestamp.Format("15:04:05")

	// Color the event type
	typeColor := color.New(color.FgMagenta)
	eventType := typeColor.Sprint(event.Type)

	// Color the issue ID
	issueColor := color.New(color.FgGreen)
	issueID := issueColor.Sprint(event.IssueID)

	// Print the event
	fmt.Printf("%s [%s] %s %s: %s\n",
		severityIcon,
		timestamp,
		issueID,
		eventType,
		severityColor.Sprint(event.Message),
	)

	// If there's additional structured data, show it indented
	if len(event.Data) > 0 {
		gray := color.New(color.FgHiBlack)
		for key, value := range event.Data {
			fmt.Printf("    %s: %v\n", gray.Sprint(key), value)
		}
	}
}
