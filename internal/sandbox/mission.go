package sandbox

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/steveyegge/vc/internal/events"
	"github.com/steveyegge/vc/internal/storage"
	"github.com/steveyegge/vc/internal/types"
)

// slugifyRegex is compiled once at package initialization for performance (vc-249)
var slugifyRegex = regexp.MustCompile(`[^a-z0-9]+`)

// logSandboxEvent creates and stores an event for sandbox lifecycle observability (vc-265)
func logSandboxEvent(ctx context.Context, store storage.Storage, eventType events.EventType, severity events.EventSeverity, missionID, message string, data map[string]interface{}) {
	// Skip logging if context is canceled (e.g., during shutdown)
	if ctx.Err() != nil {
		return
	}

	event := &events.AgentEvent{
		ID:         uuid.New().String(),
		Type:       eventType,
		Timestamp:  time.Now(),
		IssueID:    missionID,
		ExecutorID: "", // Sandbox operations are not tied to specific executor
		AgentID:    "", // Sandbox operations are not tied to specific agent
		Severity:   severity,
		Message:    message,
		Data:       data,
		SourceLine: 0, // Not applicable for sandbox lifecycle events
	}

	if err := store.StoreAgentEvent(ctx, event); err != nil {
		// Log error but don't fail sandbox operations
		fmt.Fprintf(os.Stderr, "warning: failed to store sandbox event: %v\n", err)
	}
}

// gitBranchExists checks if a git branch exists in the repository
func gitBranchExists(ctx context.Context, repoPath, branchName string) (bool, error) {
	cmd := exec.CommandContext(ctx, "git", "-C", repoPath, "show-ref", "--verify", "--quiet", fmt.Sprintf("refs/heads/%s", branchName))
	err := cmd.Run()
	if err == nil {
		return true, nil // Branch exists
	}
	if exitErr, ok := err.(*exec.ExitError); ok {
		if exitErr.ExitCode() == 1 {
			return false, nil // Branch doesn't exist (expected)
		}
	}
	return false, fmt.Errorf("failed to check if branch exists: %w", err)
}

// reconstructSandbox attempts to reconstruct a Sandbox object from stored metadata
// after executor restart. Returns nil, nil if the git branch no longer exists (stale metadata).
// This handles the vc-247 scenario where metadata exists but sandbox not in manager's active list.
func reconstructSandbox(ctx context.Context, m Manager, mission *types.Mission) (*Sandbox, error) {
	// Get manager as concrete type to access config
	mgr, ok := m.(*manager)
	if !ok {
		return nil, fmt.Errorf("manager is not a concrete *manager type")
	}

	// Verify git branch still exists
	exists, err := gitBranchExists(ctx, mgr.config.ParentRepo, mission.BranchName)
	if err != nil {
		return nil, fmt.Errorf("failed to check if branch exists: %w", err)
	}
	if !exists {
		// Branch doesn't exist - metadata is stale
		return nil, nil
	}

	// Reconstruct sandbox object from metadata
	sandboxID := fmt.Sprintf("mission-%s", mission.ID)
	beadsDBPath := fmt.Sprintf("%s/.beads/beads.db", mission.SandboxPath)

	sandbox := &Sandbox{
		ID:          sandboxID,
		MissionID:   mission.ID,
		Path:        mission.SandboxPath,
		GitBranch:   mission.BranchName,
		GitWorktree: mission.SandboxPath,
		BeadsDB:     beadsDBPath,
		ParentRepo:  mgr.config.ParentRepo,
		Created:     mission.CreatedAt,     // Use mission creation time as proxy
		LastUsed:    mission.UpdatedAt,     // Use mission update time as proxy
		Status:      SandboxStatusActive,
	}

	// Re-add to manager's active list
	mgr.mu.Lock()
	mgr.activeSandboxes[sandboxID] = sandbox
	mgr.mu.Unlock()

	return sandbox, nil
}

// CreateMissionSandbox creates a shared sandbox for a mission epic.
// This creates a stable, mission-level sandbox with predictable paths:
//   - Sandbox directory: .sandboxes/mission-{ID}/
//   - Branch name: mission/{ID}-{slug}
//
// The sandbox metadata (path, branch) is stored in vc_mission_state table
// so it can be retrieved and reused by workers on the same mission.
//
// This function is idempotent: calling it multiple times for the same mission
// returns the existing sandbox if one already exists.
func CreateMissionSandbox(ctx context.Context, manager Manager, store storage.Storage, missionID string) (*Sandbox, error) {
	startTime := time.Now()

	// 1. Get mission metadata to generate stable paths
	mission, err := store.GetMission(ctx, missionID)
	if err != nil {
		return nil, fmt.Errorf("failed to get mission %s: %w", missionID, err)
	}

	// Emit sandbox creation started event (vc-265)
	logSandboxEvent(ctx, store, events.EventTypeSandboxCreationStarted, events.SeverityInfo, missionID,
		fmt.Sprintf("Starting sandbox creation for mission %s", missionID),
		map[string]interface{}{
			"mission_id":  missionID,
			"base_branch": "main",
		})

	// 2. Check if sandbox already exists (idempotency)
	if mission.SandboxPath != "" && mission.BranchName != "" {
		// Mission already has a sandbox - try to retrieve it
		sandboxes, err := manager.List(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to list sandboxes: %w", err)
		}

		// Find existing sandbox by mission ID
		for _, sb := range sandboxes {
			if sb.MissionID == missionID {
				// Found it - return existing sandbox
				return sb, nil
			}
		}

		// Sandbox metadata exists but sandbox not in manager's active list
		// This can happen after executor restart (vc-247)
		// Try to reconstruct the sandbox if git branch still exists
		sandbox, err := reconstructSandbox(ctx, manager, mission)
		if err != nil {
			// Reconstruction failed - clear stale metadata and create fresh
			fmt.Printf("Warning: failed to reconstruct sandbox for %s, creating fresh: %v\n", missionID, err)
			updates := map[string]interface{}{
				"sandbox_path": nil,
				"branch_name":  nil,
			}
			_ = store.UpdateMission(ctx, missionID, updates, "system") // Best-effort cleanup
		} else if sandbox != nil {
			// Successfully reconstructed - return it
			return sandbox, nil
		}
		// If sandbox is nil but no error, metadata was stale - continue to create fresh
	}

	// 3. Create sandbox using Manager with stable paths
	titleSlug := slugify(mission.Title)

	cfg := SandboxConfig{
		MissionID:   missionID,
		// ParentRepo and SandboxRoot will be filled in by manager from its config
		StablePaths: true,      // Use stable, predictable paths for missions
		TitleSlug:   titleSlug, // For branch name generation
		BaseBranch:  "main",    // Default base branch
	}

	sandbox, err := manager.Create(ctx, cfg)
	if err != nil {
		// Emit creation failed event (vc-265)
		duration := time.Since(startTime)
		logSandboxEvent(ctx, store, events.EventTypeSandboxCreationCompleted, events.SeverityError, missionID,
			fmt.Sprintf("Sandbox creation failed for mission %s: %v", missionID, err),
			map[string]interface{}{
				"mission_id":  missionID,
				"success":     false,
				"error":       err.Error(),
				"duration_ms": duration.Milliseconds(),
			})
		return nil, fmt.Errorf("failed to create sandbox: %w", err)
	}

	// Emit worktree created event (vc-265)
	logSandboxEvent(ctx, store, events.EventTypeGitWorktreeCreated, events.SeverityInfo, missionID,
		fmt.Sprintf("Git worktree created at %s for mission %s", sandbox.Path, missionID),
		map[string]interface{}{
			"mission_id":    missionID,
			"worktree_path": sandbox.Path,
			"base_branch":   "main",
		})

	// Emit branch created event (vc-265)
	logSandboxEvent(ctx, store, events.EventTypeGitBranchCreated, events.SeverityInfo, missionID,
		fmt.Sprintf("Git branch %s created for mission %s", sandbox.GitBranch, missionID),
		map[string]interface{}{
			"mission_id":    missionID,
			"branch_name":   sandbox.GitBranch,
			"base_branch":   "main",
			"worktree_path": sandbox.Path,
		})

	// 4. Store metadata in vc_mission_state
	// sandbox.Path and sandbox.GitBranch are now set by manager with stable paths
	updates := map[string]interface{}{
		"sandbox_path": sandbox.Path,
		"branch_name":  sandbox.GitBranch,
	}

	if err := store.UpdateMission(ctx, missionID, updates, "system"); err != nil {
		// Failed to store metadata - clean up the sandbox
		_ = manager.Cleanup(ctx, sandbox) // Best-effort cleanup

		// Emit creation failed event (vc-265)
		duration := time.Since(startTime)
		logSandboxEvent(ctx, store, events.EventTypeSandboxCreationCompleted, events.SeverityError, missionID,
			fmt.Sprintf("Failed to store sandbox metadata for mission %s: %v", missionID, err),
			map[string]interface{}{
				"mission_id":   missionID,
				"sandbox_id":   sandbox.ID,
				"sandbox_path": sandbox.Path,
				"branch_name":  sandbox.GitBranch,
				"success":      false,
				"error":        err.Error(),
				"duration_ms":  duration.Milliseconds(),
			})
		return nil, fmt.Errorf("failed to store sandbox metadata: %w", err)
	}

	// Emit creation completed event (vc-265)
	duration := time.Since(startTime)
	logSandboxEvent(ctx, store, events.EventTypeSandboxCreationCompleted, events.SeverityInfo, missionID,
		fmt.Sprintf("Sandbox creation completed for mission %s in %dms", missionID, duration.Milliseconds()),
		map[string]interface{}{
			"mission_id":   missionID,
			"sandbox_id":   sandbox.ID,
			"sandbox_path": sandbox.Path,
			"branch_name":  sandbox.GitBranch,
			"success":      true,
			"duration_ms":  duration.Milliseconds(),
		})

	return sandbox, nil
}

// slugify converts a string to a URL-friendly slug
// Examples:
//   - "User Authentication" -> "user-authentication"
//   - "Fix bug #123" -> "fix-bug-123"
//   - "Add support for OAuth2.0" -> "add-support-for-oauth2-0"
func slugify(s string) string {
	// Convert to lowercase
	s = strings.ToLower(s)

	// Replace non-alphanumeric characters with hyphens (using pre-compiled regex)
	s = slugifyRegex.ReplaceAllString(s, "-")

	// Remove leading/trailing hyphens
	s = strings.Trim(s, "-")

	// Limit length to 50 characters for reasonable branch names
	if len(s) > 50 {
		s = s[:50]
		// Remove trailing hyphen if we cut in the middle of a word
		s = strings.TrimRight(s, "-")
	}

	return s
}

// CleanupMissionSandbox removes a mission sandbox and clears metadata.
// This is called when a mission is closed or abandoned.
func CleanupMissionSandbox(ctx context.Context, manager Manager, store storage.Storage, missionID string) error {
	startTime := time.Now()

	// 1. Get mission metadata to find sandbox
	mission, err := store.GetMission(ctx, missionID)
	if err != nil {
		return fmt.Errorf("failed to get mission %s: %w", missionID, err)
	}

	// 2. If no sandbox metadata, nothing to clean up
	if mission.SandboxPath == "" && mission.BranchName == "" {
		return nil // No sandbox to clean up
	}

	// Emit cleanup started event (vc-265)
	sandboxID := fmt.Sprintf("mission-%s", missionID)
	logSandboxEvent(ctx, store, events.EventTypeSandboxCleanupStarted, events.SeverityInfo, missionID,
		fmt.Sprintf("Starting sandbox cleanup for mission %s", missionID),
		map[string]interface{}{
			"mission_id":   missionID,
			"sandbox_id":   sandboxID,
			"sandbox_path": mission.SandboxPath,
			"branch_name":  mission.BranchName,
		})

	// 3. Find sandbox in manager's active list
	sandboxes, err := manager.List(ctx)
	if err != nil {
		// Emit cleanup failed event (vc-265)
		duration := time.Since(startTime)
		logSandboxEvent(ctx, store, events.EventTypeSandboxCleanupCompleted, events.SeverityError, missionID,
			fmt.Sprintf("Sandbox cleanup failed for mission %s: %v", missionID, err),
			map[string]interface{}{
				"mission_id":   missionID,
				"sandbox_id":   sandboxID,
				"sandbox_path": mission.SandboxPath,
				"branch_name":  mission.BranchName,
				"success":      false,
				"error":        err.Error(),
				"duration_ms":  duration.Milliseconds(),
			})
		return fmt.Errorf("failed to list sandboxes: %w", err)
	}

	var sandbox *Sandbox
	for _, sb := range sandboxes {
		if sb.MissionID == missionID {
			sandbox = sb
			break
		}
	}

	// 4. Clean up sandbox if found
	if sandbox != nil {
		if err := manager.Cleanup(ctx, sandbox); err != nil {
			// Emit cleanup failed event (vc-265)
			duration := time.Since(startTime)
			logSandboxEvent(ctx, store, events.EventTypeSandboxCleanupCompleted, events.SeverityError, missionID,
				fmt.Sprintf("Sandbox cleanup failed for mission %s: %v", missionID, err),
				map[string]interface{}{
					"mission_id":   missionID,
					"sandbox_id":   sandbox.ID,
					"sandbox_path": sandbox.Path,
					"branch_name":  sandbox.GitBranch,
					"success":      false,
					"error":        err.Error(),
					"duration_ms":  duration.Milliseconds(),
				})
			return fmt.Errorf("failed to cleanup sandbox: %w", err)
		}

		// Emit worktree removed event (vc-265)
		logSandboxEvent(ctx, store, events.EventTypeGitWorktreeRemoved, events.SeverityInfo, missionID,
			fmt.Sprintf("Git worktree removed at %s for mission %s", sandbox.Path, missionID),
			map[string]interface{}{
				"mission_id":    missionID,
				"worktree_path": sandbox.Path,
				"success":       true,
			})

		// Emit branch deleted event (vc-265)
		logSandboxEvent(ctx, store, events.EventTypeGitBranchDeleted, events.SeverityInfo, missionID,
			fmt.Sprintf("Git branch %s deleted for mission %s", sandbox.GitBranch, missionID),
			map[string]interface{}{
				"mission_id":  missionID,
				"branch_name": sandbox.GitBranch,
				"success":     true,
			})
	}

	// 5. Clear metadata from vc_mission_state (even if sandbox wasn't in manager)
	updates := map[string]interface{}{
		"sandbox_path": nil, // Set to NULL
		"branch_name":  nil, // Set to NULL
	}

	if err := store.UpdateMission(ctx, missionID, updates, "system"); err != nil {
		// Emit cleanup failed event (vc-265)
		duration := time.Since(startTime)
		logSandboxEvent(ctx, store, events.EventTypeSandboxCleanupCompleted, events.SeverityError, missionID,
			fmt.Sprintf("Failed to clear sandbox metadata for mission %s: %v", missionID, err),
			map[string]interface{}{
				"mission_id":   missionID,
				"sandbox_id":   sandboxID,
				"sandbox_path": mission.SandboxPath,
				"branch_name":  mission.BranchName,
				"success":      false,
				"error":        err.Error(),
				"duration_ms":  duration.Milliseconds(),
			})
		return fmt.Errorf("failed to clear sandbox metadata: %w", err)
	}

	// Emit cleanup completed event (vc-265)
	duration := time.Since(startTime)
	logSandboxEvent(ctx, store, events.EventTypeSandboxCleanupCompleted, events.SeverityInfo, missionID,
		fmt.Sprintf("Sandbox cleanup completed for mission %s in %dms", missionID, duration.Milliseconds()),
		map[string]interface{}{
			"mission_id":   missionID,
			"sandbox_id":   sandboxID,
			"sandbox_path": mission.SandboxPath,
			"branch_name":  mission.BranchName,
			"success":      true,
			"duration_ms":  duration.Milliseconds(),
		})

	return nil
}

// GetMissionSandbox retrieves the sandbox for a mission, if it exists.
// Returns (nil, nil) if the mission has no sandbox or if metadata exists but git branch doesn't.
// Returns error only for actual failures (database error, git error, etc.).
//
// This function handles the executor restart scenario (vc-247, vc-250):
// If metadata exists but sandbox not in manager's active list, it attempts to
// reconstruct the sandbox from metadata + git state.
func GetMissionSandbox(ctx context.Context, manager Manager, store storage.Storage, missionID string) (*Sandbox, error) {
	// 1. Get mission metadata
	mission, err := store.GetMission(ctx, missionID)
	if err != nil {
		return nil, fmt.Errorf("failed to get mission %s: %w", missionID, err)
	}

	// 2. Check if mission has sandbox metadata
	if mission.SandboxPath == "" && mission.BranchName == "" {
		return nil, nil // No sandbox
	}

	// 3. Find sandbox in manager's active list
	sandboxes, err := manager.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list sandboxes: %w", err)
	}

	for _, sb := range sandboxes {
		if sb.MissionID == missionID {
			return sb, nil
		}
	}

	// 4. Sandbox metadata exists but not in manager's active list
	// This can happen after executor restart - try to reconstruct
	sandbox, err := reconstructSandbox(ctx, manager, mission)
	if err != nil {
		// Failed to reconstruct - return error
		return nil, fmt.Errorf("failed to reconstruct sandbox for %s: %w", missionID, err)
	}
	if sandbox == nil {
		// Git branch doesn't exist - metadata is stale
		// Return (nil, nil) to indicate "no sandbox" rather than error
		return nil, nil
	}

	return sandbox, nil
}
