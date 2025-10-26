package executor

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/steveyegge/vc/internal/config"
	"github.com/steveyegge/vc/internal/git"
)

// cleanupOrphanedBranches removes orphaned mission branches on startup (vc-135)
// This finds mission branches with no associated worktree and deletes them if older than 7 days.
// Returns error only on critical failures; logs warnings for individual branch deletion failures.
func (e *Executor) cleanupOrphanedBranches(ctx context.Context) error {
	// Initialize git operations
	gitOps, err := git.NewGit(ctx)
	if err != nil {
		return fmt.Errorf("failed to initialize git: %w", err)
	}

	// Get repository path from config
	repoPath := e.config.ParentRepo
	if repoPath == "" {
		repoPath = "."
	}

	// Default retention: 7 days for orphaned branches
	retentionDays := 7

	// Clean up orphaned branches
	deletedCount, err := gitOps.CleanupOrphanedBranches(ctx, repoPath, retentionDays, false)
	if err != nil {
		return fmt.Errorf("failed to cleanup orphaned branches: %w", err)
	}

	if deletedCount > 0 {
		fmt.Printf("Cleanup: Deleted %d orphaned mission branch(es) (older than %d days)\n",
			deletedCount, retentionDays)
	}

	return nil
}

// cleanupLoop runs periodic cleanup of stale executor instances in a background goroutine
// When instances are marked as stale, their claimed issues are automatically released
func (e *Executor) cleanupLoop(ctx context.Context) {
	defer close(e.cleanupDoneCh)

	ticker := time.NewTicker(e.cleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-e.cleanupStopCh:
			return
		case <-ticker.C:
			// Check if we should stop before running cleanup
			select {
			case <-e.cleanupStopCh:
				return
			default:
			}

			// Run cleanup with cancellation support
			// Use a channel to make cleanup interruptible
			done := make(chan error, 1)
			go func() {
				staleThresholdSecs := int(e.staleThreshold.Seconds())
				cleaned, err := e.store.CleanupStaleInstances(ctx, staleThresholdSecs)
				if err != nil {
					done <- err
					return
				}
				if cleaned > 0 {
					fmt.Printf("Cleanup: Marked %d stale instance(s) as stopped and released their claims\n", cleaned)
				}

				// Cleanup old failed sandboxes beyond retention policy (vc-134)
				if e.sandboxMgr != nil && e.config != nil && e.config.SandboxRetentionCount > 0 {
					if err := e.sandboxMgr.CleanupStaleFailedSandboxes(ctx, e.config.SandboxRetentionCount); err != nil {
						fmt.Fprintf(os.Stderr, "warning: failed to cleanup stale sandboxes: %v\n", err)
						// Don't fail the cleanup loop on sandbox cleanup errors
					}
				}

				// Cleanup old stopped executor instances (vc-244)
				// Prevents accumulation in long-running deployments
				olderThanSeconds := int(e.instanceCleanupAge.Seconds())
				deletedInstances, err := e.store.DeleteOldStoppedInstances(ctx, olderThanSeconds, e.instanceCleanupKeep)
				if err != nil {
					fmt.Fprintf(os.Stderr, "warning: failed to cleanup old executor instances: %v\n", err)
					// Don't fail the cleanup loop on cleanup errors
				} else if deletedInstances > 0 {
					fmt.Printf("Cleanup: Deleted %d old stopped executor instance(s) (older than %v, keeping %d most recent)\n",
						deletedInstances, e.instanceCleanupAge, e.instanceCleanupKeep)
				}

				done <- nil
			}()

			// Wait for either completion or stop signal
			select {
			case err := <-done:
				if err != nil {
					// Log error but continue monitoring
					fmt.Fprintf(os.Stderr, "cleanup: error cleaning up stale instances: %v\n", err)
				}
			case <-e.cleanupStopCh:
				// Stop signal received while cleaning - exit immediately
				// The goroutine will finish in the background
				return
			}
		}
	}
}

// eventCleanupLoop runs periodic cleanup of old events in a background goroutine
// This enforces event retention policies to prevent database bloat
func (e *Executor) eventCleanupLoop(ctx context.Context) {
	defer close(e.eventCleanupDoneCh)

	// Get event retention config (from executor config or defaults)
	retentionCfg := config.DefaultEventRetentionConfig()
	if e.config != nil && e.config.EventRetentionConfig != nil {
		retentionCfg = *e.config.EventRetentionConfig
	}

	// Validate configuration at startup to fail fast
	if err := retentionCfg.Validate(); err != nil {
		fmt.Fprintf(os.Stderr, "Event cleanup: Invalid configuration: %v (cleanup disabled)\n", err)
		return
	}

	// Skip cleanup if disabled
	if !retentionCfg.CleanupEnabled {
		fmt.Printf("Event cleanup: Disabled via configuration\n")
		return
	}

	// Create ticker with configured interval
	cleanupInterval := time.Duration(retentionCfg.CleanupIntervalHours) * time.Hour
	ticker := time.NewTicker(cleanupInterval)
	defer ticker.Stop()

	fmt.Printf("Event cleanup: Started (interval=%v, retention=%dd, per_issue_limit=%d, global_limit=%d)\n",
		cleanupInterval, retentionCfg.RetentionDays, retentionCfg.PerIssueLimitEvents, retentionCfg.GlobalLimitEvents)

	// Run cleanup immediately on startup (before first ticker)
	if err := e.runEventCleanup(ctx, retentionCfg); err != nil {
		fmt.Fprintf(os.Stderr, "event cleanup: initial cleanup failed: %v\n", err)
	}

	for {
		select {
		case <-ctx.Done():
			return
		case <-e.eventCleanupStopCh:
			return
		case <-ticker.C:
			// Check if we should stop before running cleanup
			select {
			case <-e.eventCleanupStopCh:
				return
			default:
			}

			// Run cleanup directly (blocking) - it's okay to block the loop
			// since cleanup should be relatively quick and we want clean shutdown
			if err := e.runEventCleanup(ctx, retentionCfg); err != nil {
				fmt.Fprintf(os.Stderr, "event cleanup: error during cleanup: %v\n", err)
			}
		}
	}
}

// runEventCleanup executes one cycle of event cleanup
func (e *Executor) runEventCleanup(ctx context.Context, cfg config.EventRetentionConfig) error {
	startTime := time.Now()

	// Track metrics for logging
	var timeBasedDeleted, perIssueDeleted, globalLimitDeleted int
	var vacuumRan bool
	var cleanupErr error

	// Step 1: Time-based cleanup (delete old events)
	deleted, err := e.store.CleanupEventsByAge(ctx, cfg.RetentionDays, cfg.RetentionCriticalDays, cfg.CleanupBatchSize)
	if err != nil {
		cleanupErr = fmt.Errorf("time-based cleanup failed: %w", err)
		// Log error event and return
		e.logCleanupEvent(ctx, 0, 0, 0, 0, time.Since(startTime).Milliseconds(), false, 0, false, cleanupErr.Error())
		return cleanupErr
	}
	timeBasedDeleted = deleted

	// Step 2: Per-issue limit cleanup (enforce per-issue event caps)
	deleted, err = e.store.CleanupEventsByIssueLimit(ctx, cfg.PerIssueLimitEvents, cfg.CleanupBatchSize)
	if err != nil {
		cleanupErr = fmt.Errorf("per-issue limit cleanup failed: %w", err)
		// Log error event with partial results
		e.logCleanupEvent(ctx, timeBasedDeleted, timeBasedDeleted, 0, 0, time.Since(startTime).Milliseconds(), false, 0, false, cleanupErr.Error())
		return cleanupErr
	}
	perIssueDeleted = deleted

	// Step 3: Global limit cleanup (enforce global safety limit)
	// Trigger aggressive cleanup at 95% of configured limit
	triggerThreshold := int(float64(cfg.GlobalLimitEvents) * 0.95)
	deleted, err = e.store.CleanupEventsByGlobalLimit(ctx, triggerThreshold, cfg.CleanupBatchSize)
	if err != nil {
		cleanupErr = fmt.Errorf("global limit cleanup failed: %w", err)
		// Log error event with partial results
		e.logCleanupEvent(ctx, timeBasedDeleted+perIssueDeleted, timeBasedDeleted, perIssueDeleted, 0, time.Since(startTime).Milliseconds(), false, 0, false, cleanupErr.Error())
		return cleanupErr
	}
	globalLimitDeleted = deleted

	totalDeleted := timeBasedDeleted + perIssueDeleted + globalLimitDeleted

	// Step 4: Optional VACUUM to reclaim disk space
	if cfg.CleanupVacuum && totalDeleted > 0 {
		if err := e.store.VacuumDatabase(ctx); err != nil {
			// Don't fail the whole cleanup if VACUUM fails
			fmt.Fprintf(os.Stderr, "event cleanup: warning: VACUUM failed: %v\n", err)
		} else {
			vacuumRan = true
		}
	}

	// Get remaining event count for metrics
	eventsRemaining := 0
	counts, err := e.store.GetEventCounts(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "event cleanup: warning: failed to get event counts: %v\n", err)
	} else if counts != nil {
		eventsRemaining = counts.TotalEvents
	}

	processingTimeMs := time.Since(startTime).Milliseconds()

	// Log cleanup metrics as structured agent event (vc-196)
	e.logCleanupEvent(ctx, totalDeleted, timeBasedDeleted, perIssueDeleted, globalLimitDeleted, processingTimeMs, vacuumRan, eventsRemaining, true, "")

	// Also log to stdout for visibility
	if totalDeleted > 0 || vacuumRan {
		fmt.Printf("Event cleanup: Deleted %d events (time_based=%d, per_issue=%d, global_limit=%d) in %dms",
			totalDeleted, timeBasedDeleted, perIssueDeleted, globalLimitDeleted, processingTimeMs)
		if vacuumRan {
			fmt.Printf(" [VACUUM ran]")
		}
		fmt.Printf(" (remaining=%d)\n", eventsRemaining)
	}

	return nil
}
