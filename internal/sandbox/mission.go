package sandbox

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/steveyegge/vc/internal/storage"
)

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
	// 1. Get mission metadata to generate stable paths
	mission, err := store.GetMission(ctx, missionID)
	if err != nil {
		return nil, fmt.Errorf("failed to get mission %s: %w", missionID, err)
	}

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
		// This can happen if executor restarted - treat as new sandbox creation
		// and update the metadata
	}

	// 3. Create sandbox using Manager with stable paths
	titleSlug := slugify(mission.Title)

	cfg := SandboxConfig{
		MissionID:   missionID,
		// ParentRepo and SandboxRoot will be filled in by manager from its config
		StablePaths: true,      // Use stable, predictable paths for missions
		TitleSlug:   titleSlug, // For branch name generation
	}

	sandbox, err := manager.Create(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create sandbox: %w", err)
	}

	// 4. Store metadata in vc_mission_state
	// sandbox.Path and sandbox.GitBranch are now set by manager with stable paths
	updates := map[string]interface{}{
		"sandbox_path": sandbox.Path,
		"branch_name":  sandbox.GitBranch,
	}

	if err := store.UpdateMission(ctx, missionID, updates, "system"); err != nil {
		// Failed to store metadata - clean up the sandbox
		_ = manager.Cleanup(ctx, sandbox) // Best-effort cleanup
		return nil, fmt.Errorf("failed to store sandbox metadata: %w", err)
	}

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

	// Replace non-alphanumeric characters with hyphens
	reg := regexp.MustCompile(`[^a-z0-9]+`)
	s = reg.ReplaceAllString(s, "-")

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
	// 1. Get mission metadata to find sandbox
	mission, err := store.GetMission(ctx, missionID)
	if err != nil {
		return fmt.Errorf("failed to get mission %s: %w", missionID, err)
	}

	// 2. If no sandbox metadata, nothing to clean up
	if mission.SandboxPath == "" && mission.BranchName == "" {
		return nil // No sandbox to clean up
	}

	// 3. Find sandbox in manager's active list
	sandboxes, err := manager.List(ctx)
	if err != nil {
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
			return fmt.Errorf("failed to cleanup sandbox: %w", err)
		}
	}

	// 5. Clear metadata from vc_mission_state (even if sandbox wasn't in manager)
	updates := map[string]interface{}{
		"sandbox_path": nil, // Set to NULL
		"branch_name":  nil, // Set to NULL
	}

	if err := store.UpdateMission(ctx, missionID, updates, "system"); err != nil {
		return fmt.Errorf("failed to clear sandbox metadata: %w", err)
	}

	return nil
}

// GetMissionSandbox retrieves the sandbox for a mission, if it exists.
// Returns nil if the mission has no sandbox.
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

	// Mission has metadata but sandbox not found in active list
	// This can happen after executor restart
	return nil, fmt.Errorf("mission %s has sandbox metadata but sandbox not found (may need to recreate)", missionID)
}
