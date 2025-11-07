// Package vcs provides an abstraction layer over version control systems.
// It supports both git and jujutsu (jj) backends with automatic detection.
package vcs

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

var (
	// ErrNotImplemented is returned when a VCS backend doesn't support an operation.
	ErrNotImplemented = errors.New("operation not implemented")
	
	// ErrNotARepository is returned when the directory is not a VCS repository.
	ErrNotARepository = errors.New("not a repository")
	
	// ErrNoVCSFound is returned when no supported VCS is detected.
	ErrNoVCSFound = errors.New("no supported VCS found")
)

// VCSType represents the type of version control system.
type VCSType string

const (
	// VCSTypeGit represents git version control.
	VCSTypeGit VCSType = "git"
	
	// VCSTypeJJ represents jujutsu version control.
	VCSTypeJJ VCSType = "jj"
	
	// VCSTypeAuto enables automatic detection of the VCS type.
	VCSTypeAuto VCSType = "auto"
)

// VCS defines the interface for version control system operations.
// All implementations must support these core operations needed by the VC executor.
type VCS interface {
	// Detection methods
	
	// Name returns the name of the VCS (e.g., "git", "jj").
	Name() string
	
	// IsRepo checks if the current directory is within a VCS repository.
	IsRepo(ctx context.Context) (bool, error)
	
	// HasUpstream checks if the repository has a configured upstream/remote.
	HasUpstream(ctx context.Context) (bool, error)
	
	// GetRepoRoot returns the absolute path to the repository root directory.
	GetRepoRoot(ctx context.Context) (string, error)
	
	// State methods
	
	// HasChanges checks if there are uncommitted changes in the working directory.
	HasChanges(ctx context.Context) (bool, error)
	
	// HasMergeConflicts checks if there are unresolved merge conflicts.
	HasMergeConflicts(ctx context.Context) (bool, error)
	
	// Operations
	
	// Add stages files for the next commit.
	// For git: git add <paths>
	// For jj: files are automatically tracked
	Add(ctx context.Context, paths []string) error
	
	// Commit creates a new commit with the given message.
	Commit(ctx context.Context, message string) error
	
	// Pull fetches and integrates changes from the upstream repository.
	Pull(ctx context.Context) error
	
	// Push uploads local commits to the upstream repository.
	Push(ctx context.Context) error
	
	// History methods
	
	// GetCurrentCommitHash returns the hash/ID of the current commit.
	GetCurrentCommitHash(ctx context.Context) (string, error)
	
	// GetFileFromHead retrieves the contents of a file from the HEAD commit.
	// Returns the file contents or an error if the file doesn't exist at HEAD.
	GetFileFromHead(ctx context.Context, path string) ([]byte, error)
	
	// Config methods
	
	// EnsureIgnoreFile ensures the VCS ignore file exists and contains the given patterns.
	// For git: .gitignore
	// For jj: .gitignore (jj respects gitignore)
	EnsureIgnoreFile(ctx context.Context, patterns []string) error
}

// Config holds configuration for VCS initialization.
type Config struct {
	// Type specifies the VCS type: "git", "jj", or "auto".
	// When set to "auto", the system will auto-detect the VCS type.
	Type VCSType
	
	// AutoDetect enables automatic VCS detection.
	// When true, DetectVCS will be called to determine the VCS type.
	// This overrides the Type field.
	AutoDetect bool
	
	// WorkingDir is the directory to use for VCS operations.
	// If empty, the current working directory is used.
	WorkingDir string
}

// DetectVCS attempts to detect the VCS type in the given directory.
// It checks for jujutsu first, then git, as per the design requirements.
// Returns the detected VCS type or an error if no VCS is found.
func DetectVCS(ctx context.Context, dir string) (VCSType, error) {
	if dir == "" {
		var err error
		dir, err = os.Getwd()
		if err != nil {
			return "", fmt.Errorf("failed to get working directory: %w", err)
		}
	}
	
	// Check for jujutsu first
	if isJJRepo(ctx, dir) {
		return VCSTypeJJ, nil
	}
	
	// Then check for git
	if isGitRepo(ctx, dir) {
		return VCSTypeGit, nil
	}
	
	return "", ErrNoVCSFound
}

// isJJRepo checks if the directory is a jujutsu repository.
func isJJRepo(ctx context.Context, dir string) bool {
	// Check if .jj directory exists
	jjDir := filepath.Join(dir, ".jj")
	if info, err := os.Stat(jjDir); err == nil && info.IsDir() {
		return true
	}
	
	// Check if we're inside a jj repo by running jj root
	cmd := exec.CommandContext(ctx, "jj", "root")
	cmd.Dir = dir
	if err := cmd.Run(); err == nil {
		return true
	}
	
	return false
}

// isGitRepo checks if the directory is a git repository.
func isGitRepo(ctx context.Context, dir string) bool {
	// Check if .git directory exists
	gitDir := filepath.Join(dir, ".git")
	if info, err := os.Stat(gitDir); err == nil && (info.IsDir() || info.Mode().IsRegular()) {
		return true
	}
	
	// Check if we're inside a git repo by running git rev-parse
	cmd := exec.CommandContext(ctx, "git", "rev-parse", "--git-dir")
	cmd.Dir = dir
	if err := cmd.Run(); err == nil {
		return true
	}
	
	return false
}

// NewVCS creates a new VCS instance based on the provided configuration.
// If config.AutoDetect is true, it will automatically detect the VCS type.
// Otherwise, it uses config.Type to determine which backend to create.
func NewVCS(ctx context.Context, cfg Config) (VCS, error) {
	workingDir := cfg.WorkingDir
	if workingDir == "" {
		var err error
		workingDir, err = os.Getwd()
		if err != nil {
			return nil, fmt.Errorf("failed to get working directory: %w", err)
		}
	}
	
	vcsType := cfg.Type
	
	// Auto-detect if requested
	if cfg.AutoDetect || cfg.Type == VCSTypeAuto {
		detected, err := DetectVCS(ctx, workingDir)
		if err != nil {
			return nil, fmt.Errorf("failed to detect VCS: %w", err)
		}
		vcsType = detected
	}
	
	// Create the appropriate backend
	switch vcsType {
	case VCSTypeGit:
		return newGitBackend(workingDir)
	case VCSTypeJJ:
		return newJJBackend(workingDir)
	default:
		return nil, fmt.Errorf("unsupported VCS type: %s", vcsType)
	}
}

// newGitBackend creates a git VCS backend.
// This is a placeholder that will be implemented in vc-75.
func newGitBackend(workingDir string) (VCS, error) {
	return nil, fmt.Errorf("git backend not yet implemented (see vc-75)")
}

// newJJBackend creates a jujutsu VCS backend.
// This is a placeholder that will be implemented in vc-76.
func newJJBackend(workingDir string) (VCS, error) {
	return nil, fmt.Errorf("jj backend not yet implemented (see vc-76)")
}
