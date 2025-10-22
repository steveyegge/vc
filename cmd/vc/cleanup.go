package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/fatih/color"
	"github.com/spf13/cobra"
	"github.com/steveyegge/vc/internal/config"
	"github.com/steveyegge/vc/internal/git"
)

var cleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Cleanup and maintenance commands",
	Long:  `Commands for cleaning up old data and performing database maintenance.`,
}

var cleanupBranchesCmd = &cobra.Command{
	Use:   "branches",
	Short: "Clean up orphaned mission branches",
	Long: `Delete orphaned mission branches that have no associated worktree.

Mission branches are created for each sandbox but may not be cleaned up if the
executor crashes or is interrupted. This command finds branches matching the
pattern "mission/*" that have no corresponding worktree and deletes them.

By default, only branches older than 7 days are deleted to avoid removing
branches from active missions.

Examples:
  vc cleanup branches                    # Clean up branches older than 7 days
  vc cleanup branches --retention-days 14  # Clean up branches older than 14 days
  vc cleanup branches --dry-run          # Preview what would be deleted`,
	Run: func(cmd *cobra.Command, args []string) {
		dryRun, _ := cmd.Flags().GetBool("dry-run")
		retentionDays, _ := cmd.Flags().GetInt("retention-days")

		ctx := context.Background()

		// Get current working directory as the repository path
		repoPath, err := os.Getwd()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: failed to get current directory: %v\n", err)
			os.Exit(1)
		}

		// Initialize git operations
		gitOps, err := initGit(ctx)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: failed to initialize git: %v\n", err)
			os.Exit(1)
		}

		if dryRun {
			fmt.Printf("%s\n", color.YellowString("DRY RUN MODE - No branches will be deleted"))
		}
		fmt.Printf("Scanning for orphaned mission branches (retention: %d days)...\n\n", retentionDays)

		// Get summary of orphaned branches
		summary, err := gitOps.GetOrphanedBranchSummary(ctx, repoPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: failed to get orphaned branch summary: %v\n", err)
			os.Exit(1)
		}
		fmt.Print(summary)

		// Clean up orphaned branches
		deletedCount, err := gitOps.CleanupOrphanedBranches(ctx, repoPath, retentionDays, dryRun)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: branch cleanup failed: %v\n", err)
			os.Exit(1)
		}

		fmt.Println()
		if dryRun {
			fmt.Printf("Would delete %d orphaned branch(es)\n", deletedCount)
			fmt.Printf("Run without --dry-run to perform cleanup\n")
		} else {
			green := color.New(color.FgGreen).SprintFunc()
			fmt.Printf("%s Deleted %d orphaned branch(es)\n", green("✓"), deletedCount)
		}
	},
}

var cleanupEventsCmd = &cobra.Command{
	Use:   "events",
	Short: "Clean up old agent events",
	Long: `Delete old agent events according to retention policy.

Executes three cleanup strategies in sequence:
  1. Time-based: Delete events older than retention period
  2. Per-issue: Limit events per issue to configured maximum
  3. Global: Enforce global event count limit

Configuration is read from environment variables (see CLAUDE.md for details).
Default retention: 30 days (regular), 90 days (critical), 1000 events/issue, 100k global.

Examples:
  vc cleanup events                # Run cleanup with defaults
  vc cleanup events --vacuum       # Run cleanup and reclaim disk space
  vc cleanup events --dry-run      # Preview what would be deleted`,
	Run: func(cmd *cobra.Command, args []string) {
		dryRun, _ := cmd.Flags().GetBool("dry-run")
		vacuum, _ := cmd.Flags().GetBool("vacuum")

		// Create context with timeout for long-running operations
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
		defer cancel()

		// Load retention configuration from environment
		retentionCfg, err := config.EventRetentionConfigFromEnv()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: failed to load retention configuration: %v\n", err)
			fmt.Fprintf(os.Stderr, "Check environment variables (VC_EVENT_RETENTION_* - see CLAUDE.md)\n")
			os.Exit(1)
		}

		// Show configuration
		fmt.Printf("Event Retention Configuration:\n")
		fmt.Printf("  Regular events: %d days\n", retentionCfg.RetentionDays)
		fmt.Printf("  Critical events: %d days\n", retentionCfg.RetentionCriticalDays)
		fmt.Printf("  Per-issue limit: %d events\n", retentionCfg.PerIssueLimitEvents)
		fmt.Printf("  Global limit: %d events\n", retentionCfg.GlobalLimitEvents)
		fmt.Printf("  Batch size: %d events/txn\n", retentionCfg.CleanupBatchSize)
		if dryRun {
			fmt.Printf("\n%s\n", color.YellowString("DRY RUN MODE - No events will be deleted"))
		}
		fmt.Println()

		// Get event counts before cleanup
		beforeCounts, err := store.GetEventCounts(ctx)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: failed to get event counts: %v\n", err)
			os.Exit(1)
		}

		fmt.Printf("Current state:\n")
		fmt.Printf("  Total events: %s\n", formatNumber(beforeCounts.TotalEvents))
		fmt.Printf("  Issues with events: %s\n", formatNumber(len(beforeCounts.EventsByIssue)))
		fmt.Println()

		// In dry-run mode, we still run cleanup but note that it's safe
		// because the storage layer's cleanup methods are read-only in terms
		// of what they return (they count what would be deleted)
		// However, the current implementation actually deletes, so we need to warn
		if dryRun {
			fmt.Printf("%s\n", color.YellowString("Note: Full dry-run preview requires storage layer support (vc-199)"))
			fmt.Printf("Current implementation shows before/after state without deletion.\n")
			fmt.Println("Dry run complete. Use without --dry-run to perform cleanup.")
			return
		}

		// Execute cleanup sequence
		startTime := time.Now()
		totalDeleted := 0

		// 1. Time-based cleanup
		fmt.Printf("Running time-based cleanup (>%d days, critical >%d days)...\n",
			retentionCfg.RetentionDays, retentionCfg.RetentionCriticalDays)
		ageDeleted, err := store.CleanupEventsByAge(ctx,
			retentionCfg.RetentionDays,
			retentionCfg.RetentionCriticalDays,
			retentionCfg.CleanupBatchSize)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: time-based cleanup failed: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("  Deleted %s events\n", formatNumber(ageDeleted))
		totalDeleted += ageDeleted

		// 2. Per-issue limit cleanup
		if retentionCfg.PerIssueLimitEvents > 0 {
			fmt.Printf("\nRunning per-issue cleanup (limit: %d events/issue)...\n",
				retentionCfg.PerIssueLimitEvents)
			issueDeleted, err := store.CleanupEventsByIssueLimit(ctx,
				retentionCfg.PerIssueLimitEvents,
				retentionCfg.CleanupBatchSize)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error: per-issue cleanup failed: %v\n", err)
				os.Exit(1)
			}
			fmt.Printf("  Deleted %s events\n", formatNumber(issueDeleted))
			totalDeleted += issueDeleted
		} else {
			fmt.Printf("\nSkipping per-issue cleanup (unlimited)\n")
		}

		// 3. Global limit cleanup
		fmt.Printf("\nRunning global limit cleanup (limit: %d events)...\n",
			retentionCfg.GlobalLimitEvents)
		globalDeleted, err := store.CleanupEventsByGlobalLimit(ctx,
			retentionCfg.GlobalLimitEvents,
			retentionCfg.CleanupBatchSize)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: global limit cleanup failed: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("  Deleted %s events\n", formatNumber(globalDeleted))
		totalDeleted += globalDeleted

		// Get event counts after cleanup
		afterCounts, err := store.GetEventCounts(ctx)

		// Show summary
		elapsed := time.Since(startTime)
		green := color.New(color.FgGreen).SprintFunc()
		fmt.Printf("\n%s Cleanup complete\n", green("✓"))
		fmt.Printf("  Events deleted: %s\n", formatNumber(totalDeleted))

		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to get final event counts: %v\n", err)
			// Calculate estimated remaining count
			estimatedRemaining := beforeCounts.TotalEvents - totalDeleted
			if estimatedRemaining < 0 {
				estimatedRemaining = 0
			}
			fmt.Printf("  Events remaining: ~%s (estimated)\n", formatNumber(estimatedRemaining))
		} else {
			fmt.Printf("  Events remaining: %s\n", formatNumber(afterCounts.TotalEvents))
		}

		fmt.Printf("  Time taken: %s\n", elapsed.Round(time.Millisecond))

		// Run VACUUM if requested
		if vacuum {
			fmt.Printf("\nRunning VACUUM to reclaim disk space...\n")
			if err := store.VacuumDatabase(ctx); err != nil {
				fmt.Fprintf(os.Stderr, "Error: VACUUM failed: %v\n", err)
				os.Exit(1)
			}
			fmt.Printf("%s VACUUM complete\n", green("✓"))
		} else {
			fmt.Printf("\nNote: Use --vacuum to reclaim disk space\n")
		}
	},
}

func init() {
	// Branch cleanup flags
	cleanupBranchesCmd.Flags().Bool("dry-run", false, "Preview deletions without committing")
	cleanupBranchesCmd.Flags().Int("retention-days", 7, "Delete branches older than N days")

	// Event cleanup flags
	cleanupEventsCmd.Flags().Bool("dry-run", false, "Preview deletions without committing")
	cleanupEventsCmd.Flags().Bool("vacuum", false, "Run VACUUM after cleanup to reclaim disk space")

	cleanupCmd.AddCommand(cleanupBranchesCmd)
	cleanupCmd.AddCommand(cleanupEventsCmd)
	rootCmd.AddCommand(cleanupCmd)
}

// initGit initializes git operations for branch cleanup
func initGit(ctx context.Context) (*git.Git, error) {
	gitOps, err := git.NewGit(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize git: %w", err)
	}
	return gitOps, nil
}

// formatNumber formats a number with thousand separators
// Handles numbers from 0 to billions with proper formatting
func formatNumber(n int) string {
	if n < 0 {
		return fmt.Sprintf("-%s", formatNumber(-n))
	}
	if n < 1000 {
		return fmt.Sprintf("%d", n)
	}
	if n < 1000000 {
		return fmt.Sprintf("%d,%03d", n/1000, n%1000)
	}
	if n < 1000000000 {
		return fmt.Sprintf("%d,%03d,%03d", n/1000000, (n/1000)%1000, n%1000)
	}
	// Billions
	return fmt.Sprintf("%d,%03d,%03d,%03d", n/1000000000, (n/1000000)%1000, (n/1000)%1000, n%1000)
}
