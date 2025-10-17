package sandbox

import (
	"context"
	"time"
)

// Manager handles creation, management, and cleanup of sandboxed development environments.
// Each sandbox provides an isolated workspace for mission execution with its own
// git worktree, branch, and beads database instance.
type Manager interface {
	// Create creates a new sandbox for the specified mission.
	// Returns the created sandbox or an error if creation fails.
	Create(ctx context.Context, cfg SandboxConfig) (*Sandbox, error)

	// Get retrieves a sandbox by its ID.
	// Returns nil if the sandbox doesn't exist.
	Get(ctx context.Context, id string) (*Sandbox, error)

	// List retrieves all sandboxes.
	// Returns an empty slice if no sandboxes exist.
	List(ctx context.Context) ([]*Sandbox, error)

	// InspectState examines a sandbox and returns its current state.
	// This includes git status, modified files, and other context needed
	// for briefing agents about the sandbox environment.
	InspectState(ctx context.Context, sandbox *Sandbox) (*SandboxContext, error)

	// Cleanup removes a sandbox and its associated resources.
	// This includes the worktree, branch, and database.
	Cleanup(ctx context.Context, sandbox *Sandbox) error

	// CleanupAll removes all sandboxes older than the specified duration.
	// This is useful for periodic cleanup of stale sandboxes.
	CleanupAll(ctx context.Context, olderThan time.Duration) error
}
