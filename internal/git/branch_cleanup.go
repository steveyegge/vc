package git

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// OrphanedBranch represents a mission branch with no associated worktree
type OrphanedBranch struct {
	Name      string
	Timestamp time.Time
	Age       time.Duration
}

// FindOrphanedMissionBranches finds mission branches that have no associated worktree.
// These are likely leftover from crashed executors or interrupted sandboxes.
// Only considers branches matching "mission/*" pattern.
// SECURITY: repoPath must be a validated, trusted path.
func (g *Git) FindOrphanedMissionBranches(ctx context.Context, repoPath string) ([]OrphanedBranch, error) {
	// Get all mission branches
	branches, err := g.ListBranches(ctx, repoPath, "mission/*")
	if err != nil {
		return nil, fmt.Errorf("failed to list mission branches: %w", err)
	}

	// Get all worktrees and their branches
	worktrees, err := g.ListWorktrees(ctx, repoPath)
	if err != nil {
		return nil, fmt.Errorf("failed to list worktrees: %w", err)
	}

	// Build a set of branches that have worktrees
	activeBranches := make(map[string]bool)
	for _, branch := range worktrees {
		activeBranches[branch] = true
	}

	// Find orphaned branches
	var orphaned []OrphanedBranch
	now := time.Now()

	for _, branch := range branches {
		if !activeBranches[branch] {
			// This branch has no worktree - it's orphaned
			timestamp, err := g.GetBranchTimestamp(ctx, repoPath, branch)
			if err != nil {
				// Skip branches we can't get timestamps for
				continue
			}

			orphaned = append(orphaned, OrphanedBranch{
				Name:      branch,
				Timestamp: timestamp,
				Age:       now.Sub(timestamp),
			})
		}
	}

	return orphaned, nil
}

// CleanupOrphanedBranches deletes orphaned mission branches older than the specified retention period.
// Returns the number of branches deleted and any error encountered.
// If dryRun is true, branches are identified but not deleted.
// SECURITY: repoPath must be a validated, trusted path.
func (g *Git) CleanupOrphanedBranches(ctx context.Context, repoPath string, retentionDays int, dryRun bool) (int, error) {
	orphaned, err := g.FindOrphanedMissionBranches(ctx, repoPath)
	if err != nil {
		return 0, fmt.Errorf("failed to find orphaned branches: %w", err)
	}

	if len(orphaned) == 0 {
		return 0, nil
	}

	retentionPeriod := time.Duration(retentionDays) * 24 * time.Hour
	deletedCount := 0

	for _, branch := range orphaned {
		if branch.Age < retentionPeriod {
			// Branch is too recent to delete
			continue
		}

		if dryRun {
			fmt.Printf("[DRY RUN] Would delete: %s (age: %.1f days)\n",
				branch.Name, branch.Age.Hours()/24)
			deletedCount++
			continue
		}

		// Delete the branch
		if err := g.DeleteBranch(ctx, repoPath, branch.Name); err != nil {
			// Log error but continue with other branches
			fmt.Printf("Warning: failed to delete branch %s: %v\n", branch.Name, err)
			continue
		}

		fmt.Printf("Deleted orphaned branch: %s (age: %.1f days)\n",
			branch.Name, branch.Age.Hours()/24)
		deletedCount++
	}

	return deletedCount, nil
}

// GetOrphanedBranchSummary returns a summary of orphaned branches for display.
// Groups branches by age category for better visibility.
func (g *Git) GetOrphanedBranchSummary(ctx context.Context, repoPath string) (string, error) {
	orphaned, err := g.FindOrphanedMissionBranches(ctx, repoPath)
	if err != nil {
		return "", fmt.Errorf("failed to find orphaned branches: %w", err)
	}

	if len(orphaned) == 0 {
		return "No orphaned mission branches found.", nil
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Found %d orphaned mission branch(es):\n\n", len(orphaned)))

	// Group by age
	var recent, old, veryOld []OrphanedBranch
	for _, branch := range orphaned {
		days := branch.Age.Hours() / 24
		if days < 7 {
			recent = append(recent, branch)
		} else if days < 30 {
			old = append(old, branch)
		} else {
			veryOld = append(veryOld, branch)
		}
	}

	if len(recent) > 0 {
		sb.WriteString("Recent (< 7 days):\n")
		for _, b := range recent {
			sb.WriteString(fmt.Sprintf("  - %s (%.1f days old)\n", b.Name, b.Age.Hours()/24))
		}
		sb.WriteString("\n")
	}

	if len(old) > 0 {
		sb.WriteString("Old (7-30 days):\n")
		for _, b := range old {
			sb.WriteString(fmt.Sprintf("  - %s (%.1f days old)\n", b.Name, b.Age.Hours()/24))
		}
		sb.WriteString("\n")
	}

	if len(veryOld) > 0 {
		sb.WriteString("Very Old (> 30 days):\n")
		for _, b := range veryOld {
			sb.WriteString(fmt.Sprintf("  - %s (%.1f days old)\n", b.Name, b.Age.Hours()/24))
		}
		sb.WriteString("\n")
	}

	return sb.String(), nil
}
