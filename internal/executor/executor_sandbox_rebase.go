package executor

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/steveyegge/vc/internal/events"
	"github.com/steveyegge/vc/internal/types"
)

// RebaseResult represents the outcome of a sandbox rebase operation
type RebaseResult struct {
	SandboxPath string
	BranchName  string
	Success     bool
	HasConflict bool
	Output      string
	Error       error
}

// rebaseAllSandboxes rebases all existing mission sandboxes against the base branch
// This is called during executor startup (Phase 1 of vc-sd8r)
// Returns (anySucceeded bool, error) where anySucceeded indicates if any rebases completed successfully
func (e *Executor) rebaseAllSandboxes(ctx context.Context) (bool, error) {
	// Query all missions with sandbox metadata
	missions, err := e.listMissionsWithSandboxes(ctx)
	if err != nil {
		return false, fmt.Errorf("failed to list missions with sandboxes: %w", err)
	}

	if len(missions) == 0 {
		fmt.Printf("Sandbox rebasing: No active mission sandboxes found\n")
		return false, nil
	}

	fmt.Printf("Sandbox rebasing: Found %d mission sandbox(es) to rebase\n", len(missions))

	// Get base branch from config
	baseBranch := e.config.DefaultBranch
	if baseBranch == "" {
		baseBranch = "main"
	}

	// Rebase each sandbox
	var successCount, conflictCount, errorCount int
	for _, mission := range missions {
		if mission.SandboxPath == "" || mission.BranchName == "" {
			// Skip missions without sandbox metadata
			continue
		}

		// Check if sandbox path exists
		if _, err := os.Stat(mission.SandboxPath); os.IsNotExist(err) {
			fmt.Printf("⚠️  Sandbox path does not exist: %s (mission: %s), skipping\n",
				mission.SandboxPath, mission.ID)
			continue
		}

		result := e.rebaseSandbox(ctx, mission.SandboxPath, mission.BranchName, baseBranch)

		if result.Success {
			successCount++
			fmt.Printf("✓ Rebased sandbox %s (branch: %s)\n", result.SandboxPath, result.BranchName)

			// Log success event to activity feed
			e.logEvent(ctx, events.EventTypeSandboxRebase, events.SeverityInfo, e.instanceID,
				fmt.Sprintf("Rebased mission sandbox on startup: %s", mission.ID),
				map[string]interface{}{
					"mission_id":    mission.ID,
					"sandbox_path":  result.SandboxPath,
					"branch_name":   result.BranchName,
					"base_branch":   baseBranch,
					"success":       true,
					"has_conflicts": false,
					"timestamp":     time.Now().Format(time.RFC3339),
				})
		} else if result.HasConflict {
			conflictCount++
			fmt.Printf("⚠️  Rebase conflicts in sandbox %s (branch: %s)\n",
				result.SandboxPath, result.BranchName)

			// Handle rebase conflicts (create resolution task)
			if err := e.handleRebaseConflicts(ctx, mission, result); err != nil {
				fmt.Fprintf(os.Stderr, "Warning: failed to handle rebase conflicts: %v\n", err)
			}

			// Log conflict event
			e.logEvent(ctx, events.EventTypeSandboxRebase, events.SeverityWarning, e.instanceID,
				fmt.Sprintf("Rebase conflicts in mission sandbox: %s", mission.ID),
				map[string]interface{}{
					"mission_id":    mission.ID,
					"sandbox_path":  result.SandboxPath,
					"branch_name":   result.BranchName,
					"base_branch":   baseBranch,
					"success":       false,
					"has_conflicts": true,
					"output":        result.Output,
					"timestamp":     time.Now().Format(time.RFC3339),
				})
		} else {
			errorCount++
			fmt.Fprintf(os.Stderr, "✗ Failed to rebase sandbox %s: %v\n",
				result.SandboxPath, result.Error)

			// Log error event
			e.logEvent(ctx, events.EventTypeSandboxRebase, events.SeverityError, e.instanceID,
				fmt.Sprintf("Rebase failed for mission sandbox: %s", mission.ID),
				map[string]interface{}{
					"mission_id":    mission.ID,
					"sandbox_path":  result.SandboxPath,
					"branch_name":   result.BranchName,
					"base_branch":   baseBranch,
					"success":       false,
					"has_conflicts": false,
					"error":         result.Error.Error(),
					"output":        result.Output,
					"timestamp":     time.Now().Format(time.RFC3339),
				})
		}
	}

	// Summary
	fmt.Printf("Sandbox rebasing: %d succeeded, %d conflicts, %d errors\n",
		successCount, conflictCount, errorCount)

	// Return whether any rebases succeeded
	// Caller will run preflight check if true to catch rebase-induced breakage
	return successCount > 0, nil
}

// rebaseSandbox rebases a single sandbox branch against the base branch
func (e *Executor) rebaseSandbox(ctx context.Context, sandboxPath, branchName, baseBranch string) RebaseResult {
	result := RebaseResult{
		SandboxPath: sandboxPath,
		BranchName:  branchName,
	}

	// Fetch latest changes from origin
	fetchCmd := exec.CommandContext(ctx, "git", "fetch", "origin", baseBranch)
	fetchCmd.Dir = sandboxPath
	if output, err := fetchCmd.CombinedOutput(); err != nil {
		result.Error = fmt.Errorf("git fetch failed: %w (output: %s)", err, string(output))
		result.Output = string(output)
		return result
	}

	// Attempt rebase
	rebaseCmd := exec.CommandContext(ctx, "git", "rebase", fmt.Sprintf("origin/%s", baseBranch))
	rebaseCmd.Dir = sandboxPath
	output, err := rebaseCmd.CombinedOutput()
	result.Output = string(output)

	if err != nil {
		// Check if it's a conflict or other error
		if strings.Contains(string(output), "CONFLICT") ||
			strings.Contains(string(output), "could not apply") {
			result.HasConflict = true

			// Abort the rebase to leave sandbox in clean state
			abortCmd := exec.CommandContext(ctx, "git", "rebase", "--abort")
			abortCmd.Dir = sandboxPath
			_ = abortCmd.Run() // Best-effort abort

			return result
		}

		result.Error = fmt.Errorf("git rebase failed: %w", err)
		return result
	}

	result.Success = true
	return result
}

// handleRebaseConflicts creates a conflict resolution task or blocks the mission
func (e *Executor) handleRebaseConflicts(ctx context.Context, mission *types.Mission, result RebaseResult) error {
	// Create a task to resolve rebase conflicts
	// This task should be added as a blocker to the mission

	conflictTask := &types.Issue{
		Title:              fmt.Sprintf("Resolve rebase conflicts for mission %s", mission.ID),
		Description:        fmt.Sprintf("The mission sandbox branch '%s' has conflicts when rebasing against main.\n\nConflicting files need to be resolved before the mission can continue.\n\nSandbox path: %s\n\nRebase output:\n```\n%s\n```", result.BranchName, result.SandboxPath, result.Output),
		IssueType:          types.TypeTask,
		Status:             types.StatusOpen,
		Priority:           0, // P0 - blocking
		AcceptanceCriteria: "Rebase conflicts are resolved and branch is successfully rebased onto main.",
	}

	// Create the conflict resolution task
	if err := e.store.CreateIssue(ctx, conflictTask, "vc-executor"); err != nil {
		return fmt.Errorf("failed to create conflict resolution task: %w", err)
	}

	// Add labels (rebase-conflict, no-auto-claim)
	if err := e.store.AddLabel(ctx, conflictTask.ID, "rebase-conflict", "vc-executor"); err != nil {
		return fmt.Errorf("failed to add rebase-conflict label: %w", err)
	}
	if err := e.store.AddLabel(ctx, conflictTask.ID, "no-auto-claim", "vc-executor"); err != nil {
		return fmt.Errorf("failed to add no-auto-claim label: %w", err)
	}

	// Add dependency: mission blocks on conflict task (child depends on parent)
	dep := &types.Dependency{
		IssueID:     mission.ID,
		DependsOnID: conflictTask.ID,
		Type:        types.DepBlocks,
	}
	if err := e.store.AddDependency(ctx, dep, "vc-executor"); err != nil {
		return fmt.Errorf("failed to add dependency: %w", err)
	}

	fmt.Printf("Created conflict resolution task: %s (blocks mission %s)\n",
		conflictTask.ID, mission.ID)

	return nil
}

// listMissionsWithSandboxes queries all missions that have sandbox metadata
func (e *Executor) listMissionsWithSandboxes(ctx context.Context) ([]*types.Mission, error) {
	// Query for all missions with sandbox_path not empty
	// We filter for epic-type issues with mission subtype
	status := types.StatusOpen
	epicType := types.TypeEpic
	filter := types.IssueFilter{
		Type:   &epicType,
		Status: &status,
	}

	issues, err := e.store.SearchIssues(ctx, "", filter)
	if err != nil {
		return nil, fmt.Errorf("failed to search for mission issues: %w", err)
	}

	var missions []*types.Mission
	for _, issue := range issues {
		// Get full mission details to check sandbox metadata
		mission, err := e.store.GetMission(ctx, issue.ID)
		if err != nil {
			// Not a mission epic, skip
			continue
		}

		// Only include missions with sandbox metadata
		if mission.SandboxPath != "" && mission.BranchName != "" {
			missions = append(missions, mission)
		}
	}

	return missions, nil
}
