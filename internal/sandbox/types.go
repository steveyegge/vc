package sandbox

import "time"

// Sandbox represents an isolated development environment for working on a mission.
// Each sandbox has its own git worktree, branch, and beads database instance.
// This enables parallel work on multiple missions without interference.
type Sandbox struct {
	// ID is a unique identifier for this sandbox
	ID string

	// MissionID is the associated mission/epic ID this sandbox is for
	MissionID string

	// Path is the absolute path to the sandbox root directory
	Path string

	// GitBranch is the dedicated git branch for this sandbox
	GitBranch string

	// GitWorktree is the path to the git worktree
	GitWorktree string

	// BeadsDB is the path to the sandbox-local beads database
	BeadsDB string

	// ParentRepo is the original repository path
	ParentRepo string

	// Created is when this sandbox was created
	Created time.Time

	// LastUsed is when this sandbox was last accessed
	LastUsed time.Time

	// Status is the current status of this sandbox
	Status SandboxStatus

	// ApprovalStatus tracks whether the human has approved merging this sandbox (vc-145)
	// Values: "", "pending", "approved", "rejected"
	ApprovalStatus string
}

// SandboxStatus represents the lifecycle state of a sandbox
type SandboxStatus string

const (
	// SandboxStatusActive indicates the sandbox is currently in use
	SandboxStatusActive SandboxStatus = "active"

	// SandboxStatusCompleted indicates the mission completed successfully
	SandboxStatusCompleted SandboxStatus = "completed"

	// SandboxStatusFailed indicates the mission failed
	SandboxStatusFailed SandboxStatus = "failed"

	// SandboxStatusCleaned indicates the sandbox has been cleaned up
	SandboxStatusCleaned SandboxStatus = "cleaned"
)

// SandboxConfig holds configuration for creating a new sandbox
type SandboxConfig struct {
	// MissionID is the mission/epic this sandbox is for
	MissionID string

	// ParentRepo is the path to the parent repository
	ParentRepo string

	// BaseBranch is the branch to create the worktree from
	BaseBranch string

	// SandboxRoot is the directory where sandboxes are created
	SandboxRoot string

	// PreserveOnFailure determines if failed sandboxes should be kept for debugging
	PreserveOnFailure bool

	// StablePaths indicates whether to use stable, predictable paths for mission-level sandboxes
	// If true: sandbox-{missionID} and mission/{missionID}-{slug}
	// If false: sandbox-{missionID}-{timestamp} and mission/{missionID}/{timestamp}
	StablePaths bool

	// TitleSlug is used when StablePaths=true to generate branch names like mission/vc-123-user-auth
	TitleSlug string
}

// SandboxContext provides comprehensive context about a sandbox's current state.
// This is used to brief agents about where they're working and what's been done.
type SandboxContext struct {
	// Sandbox is the sandbox metadata
	Sandbox *Sandbox

	// GitStatus is the output of 'git status --porcelain'
	GitStatus string

	// ModifiedFiles is a list of files that have been modified
	ModifiedFiles []string

	// LastCommand is the last command executed in this sandbox (if tracked)
	LastCommand string

	// WorkState is arbitrary state data for the sandbox
	WorkState map[string]interface{}
}
