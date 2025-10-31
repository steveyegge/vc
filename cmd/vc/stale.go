package main

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"time"

	"github.com/fatih/color"
	"github.com/spf13/cobra"
)

var staleCmd = &cobra.Command{
	Use:   "stale",
	Short: "Show orphaned claims and dead executors",
	Long: `Show issues stuck in_progress with execution_state but executor is dead/stopped.

This command identifies:
1. Issues claimed by executors with status='stopped'
2. Issues claimed by executors with stale heartbeats (no heartbeat for threshold duration)

Examples:
  # Show all stale claims (default: 5 minute heartbeat threshold)
  vc stale

  # Show stale claims with custom heartbeat threshold
  vc stale --threshold 10m

  # Auto-release all stale claims
  vc stale --release

  # Auto-release with custom threshold
  vc stale --release --threshold 15m`,
	Run: func(cmd *cobra.Command, args []string) {
		thresholdStr, _ := cmd.Flags().GetString("threshold")
		release, _ := cmd.Flags().GetBool("release")

		// Parse threshold duration
		threshold, err := time.ParseDuration(thresholdStr)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: invalid threshold %q: %v\n", thresholdStr, err)
			os.Exit(1)
		}

		ctx := context.Background()

		// Query for orphaned claims
		staleIssues, err := getStaleIssues(ctx, threshold)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}

		if len(staleIssues) == 0 {
			green := color.New(color.FgGreen).SprintFunc()
			fmt.Printf("%s No stale claims found\n", green("✓"))
			return
		}

		// Display stale claims
		yellow := color.New(color.FgYellow).SprintFunc()
		cyan := color.New(color.FgCyan).SprintFunc()
		red := color.New(color.FgRed).SprintFunc()

		fmt.Printf("\n%s Found %d orphaned claim(s):\n\n", yellow("⚠"), len(staleIssues))

		for _, si := range staleIssues {
			fmt.Printf("%s: %s\n", cyan(si.IssueID), si.IssueTitle)
			fmt.Printf("  Status: %s\n", si.IssueStatus)
			fmt.Printf("  Executor: %s (PID: %d on %s)\n", si.ExecutorInstanceID, si.ExecutorPID, si.ExecutorHostname)

			if si.ExecutorStatus == "stopped" {
				fmt.Printf("  Reason: %s\n", red("Executor stopped"))
			} else {
				staleFor := time.Since(si.LastHeartbeat)
				fmt.Printf("  Reason: %s (no heartbeat for %s)\n",
					red("Stale heartbeat"),
					formatDuration(staleFor))
			}

			fmt.Printf("  Claimed: %s\n", si.ClaimedAt.Format("2006-01-02 15:04:05"))
			fmt.Printf("  Last heartbeat: %s\n", si.LastHeartbeat.Format("2006-01-02 15:04:05"))
			fmt.Printf("  Execution state: %s\n", si.ExecutionState)
			fmt.Println()
		}

		// Release if requested
		if release {
			green := color.New(color.FgGreen).SprintFunc()
			fmt.Printf("Releasing %d orphaned claim(s)...\n\n", len(staleIssues))

			releasedCount := 0
			for _, si := range staleIssues {
				if err := releaseStaleIssue(ctx, si); err != nil {
					fmt.Fprintf(os.Stderr, "%s Failed to release %s: %v\n", red("✗"), si.IssueID, err)
					continue
				}
				fmt.Printf("%s Released %s\n", green("✓"), si.IssueID)
				releasedCount++
			}

			fmt.Printf("\n%s Released %d of %d issue(s)\n", green("✓"), releasedCount, len(staleIssues))
		} else {
			fmt.Printf("Run 'vc stale --release' to automatically release these claims\n")
		}
	},
}

func init() {
	staleCmd.Flags().String("threshold", "5m", "Heartbeat staleness threshold (e.g., 5m, 10m, 1h)")
	staleCmd.Flags().Bool("release", false, "Auto-release all stale claims")
	rootCmd.AddCommand(staleCmd)
}

// StaleIssue represents an issue with a stale or dead executor claim
type StaleIssue struct {
	IssueID            string
	IssueTitle         string
	IssueStatus        string
	ExecutorInstanceID string
	ExecutorHostname   string
	ExecutorPID        int
	ExecutorStatus     string
	LastHeartbeat      time.Time
	ClaimedAt          time.Time
	ExecutionState     string
}

// getStaleIssues queries for all issues with stale or stopped executor claims
func getStaleIssues(ctx context.Context, threshold time.Duration) ([]StaleIssue, error) {
	// Get VCStorage to access database directly
	vcStore, ok := store.(interface{ GetDB() interface{} })
	if !ok {
		return nil, fmt.Errorf("store does not support direct database access")
	}

	db, ok := vcStore.GetDB().(*sql.DB)
	if !ok {
		return nil, fmt.Errorf("database is not *sql.DB")
	}

	staleTime := time.Now().Add(-threshold)

	query := `
		SELECT
			i.id,
			i.title,
			i.status,
			ies.executor_instance_id,
			ei.hostname,
			ei.pid,
			ei.status,
			ei.last_heartbeat,
			ies.claimed_at,
			ies.state
		FROM issues i
		JOIN vc_issue_execution_state ies ON i.id = ies.issue_id
		JOIN vc_executor_instances ei ON ies.executor_instance_id = ei.id
		WHERE
			ies.executor_instance_id IS NOT NULL
			AND (
				ei.status = 'stopped'
				OR ei.status = 'crashed'
				OR (ei.status = 'running' AND ei.last_heartbeat < ?)
			)
		ORDER BY ies.claimed_at ASC
	`

	rows, err := db.QueryContext(ctx, query, staleTime)
	if err != nil {
		return nil, fmt.Errorf("failed to query stale issues: %w", err)
	}
	defer rows.Close()

	var staleIssues []StaleIssue
	for rows.Next() {
		var si StaleIssue
		err := rows.Scan(
			&si.IssueID,
			&si.IssueTitle,
			&si.IssueStatus,
			&si.ExecutorInstanceID,
			&si.ExecutorHostname,
			&si.ExecutorPID,
			&si.ExecutorStatus,
			&si.LastHeartbeat,
			&si.ClaimedAt,
			&si.ExecutionState,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan stale issue: %w", err)
		}
		staleIssues = append(staleIssues, si)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating stale issues: %w", err)
	}

	return staleIssues, nil
}

// releaseStaleIssue releases a single stale issue claim
func releaseStaleIssue(ctx context.Context, si StaleIssue) error {
	// Get VCStorage to access database directly
	vcStore, ok := store.(interface{ GetDB() interface{} })
	if !ok {
		return fmt.Errorf("store does not support direct database access")
	}

	db, ok := vcStore.GetDB().(*sql.DB)
	if !ok {
		return fmt.Errorf("database is not *sql.DB")
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	// Clear the executor claim but preserve checkpoint data
	_, err = tx.ExecContext(ctx, `
		UPDATE vc_issue_execution_state
		SET executor_instance_id = NULL,
		    state = 'pending',
		    updated_at = ?
		WHERE issue_id = ?
	`, time.Now(), si.IssueID)
	if err != nil {
		return fmt.Errorf("failed to update execution state: %w", err)
	}

	// Reset issue status to 'open'
	_, err = tx.ExecContext(ctx, `
		UPDATE issues
		SET status = 'open', updated_at = ?, closed_at = NULL
		WHERE id = ?
	`, time.Now(), si.IssueID)
	if err != nil {
		return fmt.Errorf("failed to reset issue status: %w", err)
	}

	// Commit the transaction first
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	// Add comment explaining the release (after commit to avoid nested transactions)
	var reason string
	if si.ExecutorStatus == "stopped" || si.ExecutorStatus == "crashed" {
		reason = fmt.Sprintf("Issue automatically released - executor instance %s is %s", si.ExecutorInstanceID, si.ExecutorStatus)
	} else {
		staleFor := time.Since(si.LastHeartbeat)
		reason = fmt.Sprintf("Issue automatically released - executor instance %s has stale heartbeat (no heartbeat for %s)", si.ExecutorInstanceID, formatDuration(staleFor))
	}

	err = store.AddComment(ctx, si.IssueID, "vc-stale-cleaner", reason)
	if err != nil {
		// Don't fail release if comment fails
		fmt.Fprintf(os.Stderr, "Warning: failed to add comment to %s: %v\n", si.IssueID, err)
	}

	return nil
}

// formatDuration formats a duration in a human-readable way
func formatDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
	if d < 24*time.Hour {
		return fmt.Sprintf("%.1fh", d.Hours())
	}
	return fmt.Sprintf("%.1fd", d.Hours()/24)
}
