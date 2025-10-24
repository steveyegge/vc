package sandbox

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/steveyegge/vc/internal/deduplication"
	"github.com/steveyegge/vc/internal/storage"
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

	// CleanupStaleFailedSandboxes removes old failed sandboxes from disk,
	// keeping only the most recent N as specified by retentionCount.
	// If retentionCount is 0, all failed sandboxes are kept.
	CleanupStaleFailedSandboxes(ctx context.Context, retentionCount int) error
}

// Config holds configuration for the sandbox manager
type Config struct {
	// SandboxRoot is the directory where sandboxes are created
	SandboxRoot string

	// ParentRepo is the path to the parent git repository
	ParentRepo string

	// MainDB is the main beads database storage instance
	MainDB storage.Storage

	// Deduplicator is used to prevent filing duplicate issues when merging sandbox results
	// Optional: if nil, all discovered issues will be filed without deduplication
	Deduplicator deduplication.Deduplicator

	// DeduplicationConfig is the configuration for deduplication behavior
	// Optional: if nil, defaults will be used
	DeduplicationConfig *deduplication.Config

	// PreserveOnFailure determines if failed sandboxes should be kept for debugging
	PreserveOnFailure bool

	// KeepBranches determines if mission branches should be kept after cleanup
	// If false, branches are deleted when sandbox is cleaned up (default: false)
	KeepBranches bool

	// MaxAge is the maximum age for sandboxes before they're considered stale
	MaxAge time.Duration
}

// manager is the concrete implementation of Manager
type manager struct {
	config          Config
	activeSandboxes map[string]*Sandbox
	mu              sync.RWMutex
}

// NewManager creates a new sandbox manager with the provided configuration
func NewManager(cfg Config) (Manager, error) {
	// Validate configuration
	if cfg.SandboxRoot == "" {
		return nil, fmt.Errorf("SandboxRoot cannot be empty")
	}
	if cfg.ParentRepo == "" {
		return nil, fmt.Errorf("ParentRepo cannot be empty")
	}
	if cfg.MainDB == nil {
		return nil, fmt.Errorf("MainDB cannot be nil")
	}

	// Validate parent repo is a git repository
	if err := validateGitRepo(cfg.ParentRepo); err != nil {
		return nil, fmt.Errorf("invalid parent repo: %w", err)
	}

	// Create sandbox root directory if it doesn't exist
	if err := os.MkdirAll(cfg.SandboxRoot, 0755); err != nil {
		return nil, fmt.Errorf("failed to create sandbox root: %w", err)
	}

	// Set default MaxAge if not specified
	if cfg.MaxAge == 0 {
		cfg.MaxAge = 24 * time.Hour // Default to 24 hours
	}

	m := &manager{
		config:          cfg,
		activeSandboxes: make(map[string]*Sandbox),
	}

	return m, nil
}

// Create creates a new sandbox for the specified mission
func (m *manager) Create(ctx context.Context, cfg SandboxConfig) (*Sandbox, error) {
	// Validate config
	if cfg.MissionID == "" {
		return nil, fmt.Errorf("MissionID cannot be empty")
	}
	if cfg.SandboxRoot == "" {
		cfg.SandboxRoot = m.config.SandboxRoot
	}
	if cfg.ParentRepo == "" {
		cfg.ParentRepo = m.config.ParentRepo
	}
	if cfg.BaseBranch == "" {
		cfg.BaseBranch = "main" // Default to main branch
	}

	// Generate unique sandbox ID
	sandboxID := fmt.Sprintf("sandbox-%s-%d", cfg.MissionID, time.Now().Unix())
	branchName := fmt.Sprintf("mission/%s/%d", cfg.MissionID, time.Now().Unix())

	// Create git worktree
	worktreePath, err := createWorktree(ctx, cfg, branchName)
	if err != nil {
		return nil, fmt.Errorf("failed to create worktree: %w", err)
	}

	// Create branch in worktree
	if err := createBranch(ctx, worktreePath, branchName, cfg.BaseBranch); err != nil {
		// Clean up worktree on failure
		_ = removeWorktree(ctx, cfg.ParentRepo, worktreePath) // Best-effort cleanup
		return nil, fmt.Errorf("failed to create branch: %w", err)
	}

	// Get main DB path for metadata
	mainDBPath := ""
	if m.config.MainDB != nil {
		// Try to get the path from the storage config
		// This is a bit of a hack, but we need it for metadata tracking
		mainDBPath = filepath.Join(cfg.ParentRepo, ".beads", "vc.db")
	}

	// Initialize sandbox database
	beadsDBPath, err := initSandboxDB(ctx, worktreePath, cfg.MissionID, mainDBPath)
	if err != nil {
		// Clean up on failure
		_ = removeWorktree(ctx, cfg.ParentRepo, worktreePath) // Best-effort cleanup
		return nil, fmt.Errorf("failed to initialize sandbox database: %w", err)
	}

	// Open sandbox database storage for copying issues
	sandboxDBCfg := &storage.Config{
		Path: beadsDBPath,
	}
	sandboxDB, err := storage.NewStorage(ctx, sandboxDBCfg)
	if err != nil {
		// Clean up on failure
		_ = removeWorktree(ctx, cfg.ParentRepo, worktreePath) // Best-effort cleanup
		return nil, fmt.Errorf("failed to open sandbox database: %w", err)
	}
	defer func() { _ = sandboxDB.Close() }()

	// Copy mission and dependencies to sandbox database
	if err := copyCoreIssues(ctx, m.config.MainDB, sandboxDB, cfg.MissionID); err != nil {
		// Clean up on failure
		_ = removeWorktree(ctx, cfg.ParentRepo, worktreePath) // Best-effort cleanup
		return nil, fmt.Errorf("failed to copy core issues: %w", err)
	}

	// Create sandbox metadata
	now := time.Now()
	sandbox := &Sandbox{
		ID:          sandboxID,
		MissionID:   cfg.MissionID,
		Path:        worktreePath,
		GitBranch:   branchName,
		GitWorktree: worktreePath,
		BeadsDB:     beadsDBPath,
		ParentRepo:  cfg.ParentRepo,
		Created:     now,
		LastUsed:    now,
		Status:      SandboxStatusActive,
	}

	// Register sandbox in tracking map
	m.mu.Lock()
	m.activeSandboxes[sandboxID] = sandbox
	m.mu.Unlock()

	return sandbox, nil
}

// Get retrieves a sandbox by its ID
func (m *manager) Get(ctx context.Context, id string) (*Sandbox, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	sandbox, exists := m.activeSandboxes[id]
	if !exists {
		return nil, nil
	}

	return sandbox, nil
}

// List retrieves all sandboxes
func (m *manager) List(ctx context.Context) ([]*Sandbox, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	sandboxes := make([]*Sandbox, 0, len(m.activeSandboxes))
	for _, sandbox := range m.activeSandboxes {
		sandboxes = append(sandboxes, sandbox)
	}

	return sandboxes, nil
}

// InspectState examines a sandbox and returns its current state
func (m *manager) InspectState(ctx context.Context, sandbox *Sandbox) (*SandboxContext, error) {
	if sandbox == nil {
		return nil, fmt.Errorf("sandbox cannot be nil")
	}

	// Get git status
	gitStatus, err := getGitStatus(ctx, sandbox.GitWorktree)
	if err != nil {
		return nil, fmt.Errorf("failed to get git status: %w", err)
	}

	// Get modified files
	modifiedFiles, err := getModifiedFiles(ctx, sandbox.GitWorktree)
	if err != nil {
		return nil, fmt.Errorf("failed to get modified files: %w", err)
	}

	// Update last used time
	m.mu.Lock()
	if sb, exists := m.activeSandboxes[sandbox.ID]; exists {
		sb.LastUsed = time.Now()
	}
	m.mu.Unlock()

	// Create and return context
	context := &SandboxContext{
		Sandbox:       sandbox,
		GitStatus:     gitStatus,
		ModifiedFiles: modifiedFiles,
		WorkState:     make(map[string]interface{}),
	}

	return context, nil
}

// Cleanup removes a sandbox and its associated resources
func (m *manager) Cleanup(ctx context.Context, sandbox *Sandbox) error {
	if sandbox == nil {
		return fmt.Errorf("sandbox cannot be nil")
	}

	// Open sandbox database for merging results
	sandboxDBCfg := &storage.Config{
		Path: sandbox.BeadsDB,
	}
	sandboxDB, err := storage.NewStorage(ctx, sandboxDBCfg)
	if err != nil {
		// If we can't open the database, we might still want to clean up the files
		// depending on the error type
		if !os.IsNotExist(err) {
			return fmt.Errorf("failed to open sandbox database for cleanup: %w", err)
		}
	} else {
		// Merge results to main database if sandbox completed successfully
		if sandbox.Status == SandboxStatusCompleted || sandbox.Status == SandboxStatusActive {
			if err := mergeResults(ctx, sandboxDB, m.config.MainDB, sandbox.MissionID, m.config.Deduplicator); err != nil {
				_ = sandboxDB.Close() // Best-effort cleanup
				return fmt.Errorf("failed to merge results: %w", err)
			}
		}
		_ = sandboxDB.Close() // Best-effort cleanup
	}

	// Determine if we should remove the sandbox directory
	//nolint:staticcheck // Explicit form is more readable than De Morgan simplification
	shouldRemove := true
	if sandbox.Status == SandboxStatusFailed && m.config.PreserveOnFailure {
		shouldRemove = false
	}

	if shouldRemove {
		// Remove git worktree
		if err := removeWorktree(ctx, sandbox.ParentRepo, sandbox.GitWorktree); err != nil {
			return fmt.Errorf("failed to remove worktree: %w", err)
		}

		// Delete mission branch unless KeepBranches is set (vc-134)
		if !m.config.KeepBranches {
			if err := deleteBranch(ctx, sandbox.ParentRepo, sandbox.GitBranch); err != nil {
				// Log warning but don't fail - branch deletion is not critical
				fmt.Fprintf(os.Stderr, "warning: failed to delete branch %s: %v\n", sandbox.GitBranch, err)
			}
		}

		// Remove sandbox directory (if different from worktree)
		if sandbox.Path != sandbox.GitWorktree {
			if err := os.RemoveAll(sandbox.Path); err != nil {
				return fmt.Errorf("failed to remove sandbox directory: %w", err)
			}
		}
	}

	// Update sandbox status
	m.mu.Lock()
	if sb, exists := m.activeSandboxes[sandbox.ID]; exists {
		sb.Status = SandboxStatusCleaned
	}
	// Remove from active map
	delete(m.activeSandboxes, sandbox.ID)
	m.mu.Unlock()

	return nil
}

// CleanupAll removes all sandboxes older than the specified duration
func (m *manager) CleanupAll(ctx context.Context, olderThan time.Duration) error {
	m.mu.RLock()
	// Collect sandboxes to clean up
	toCleanup := []*Sandbox{}
	cutoff := time.Now().Add(-olderThan)

	for _, sandbox := range m.activeSandboxes {
		if sandbox.LastUsed.Before(cutoff) {
			toCleanup = append(toCleanup, sandbox)
		}
	}
	m.mu.RUnlock()

	// Clean up sandboxes (don't hold the lock during cleanup)
	var lastErr error
	for _, sandbox := range toCleanup {
		if err := m.Cleanup(ctx, sandbox); err != nil {
			// Continue cleaning up other sandboxes even if one fails
			lastErr = fmt.Errorf("failed to cleanup sandbox %s: %w", sandbox.ID, err)
		}
	}

	return lastErr
}

// CleanupStaleFailedSandboxes removes old failed sandboxes from disk,
// keeping only the most recent N as specified by retentionCount.
// This implements the retention policy from vc-134.
// If retentionCount is 0, all failed sandboxes are kept.
//
// IMPORTANT: This only removes sandboxes that are NOT in the activeSandboxes map,
// preventing deletion of currently running or recently created sandboxes (vc-249).
func (m *manager) CleanupStaleFailedSandboxes(ctx context.Context, retentionCount int) error {
	if retentionCount == 0 {
		// Keep all failed sandboxes
		return nil
	}

	// Scan sandbox root directory for sandbox directories
	entries, err := os.ReadDir(m.config.SandboxRoot)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // No sandbox directory yet
		}
		return fmt.Errorf("failed to read sandbox root: %w", err)
	}

	// Get set of active sandbox paths to skip
	m.mu.RLock()
	activePaths := make(map[string]bool)
	for _, sb := range m.activeSandboxes {
		activePaths[sb.Path] = true
	}
	m.mu.RUnlock()

	// Collect sandbox directories with their modification times
	// ONLY include directories that are NOT in activeSandboxes (vc-249)
	type sandboxInfo struct {
		path    string
		modTime time.Time
	}
	var sandboxes []sandboxInfo

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		sandboxPath := filepath.Join(m.config.SandboxRoot, entry.Name())

		// Skip active sandboxes (vc-249: prevent deleting work in progress)
		if activePaths[sandboxPath] {
			continue
		}

		info, err := entry.Info()
		if err != nil {
			fmt.Fprintf(os.Stderr, "warning: failed to get info for %s: %v\n", sandboxPath, err)
			continue
		}

		sandboxes = append(sandboxes, sandboxInfo{
			path:    sandboxPath,
			modTime: info.ModTime(),
		})
	}

	// If we have fewer sandboxes than the retention count, keep all
	if len(sandboxes) <= retentionCount {
		return nil
	}

	// Sort by modification time (newest first)
	sort.Slice(sandboxes, func(i, j int) bool {
		return sandboxes[i].modTime.After(sandboxes[j].modTime)
	})

	// Remove sandboxes beyond the retention count
	var lastErr error
	for i := retentionCount; i < len(sandboxes); i++ {
		sandboxPath := sandboxes[i].path
		fmt.Printf("Removing stale failed sandbox: %s (modified: %s)\n",
			filepath.Base(sandboxPath), sandboxes[i].modTime.Format(time.RFC3339))

		if err := os.RemoveAll(sandboxPath); err != nil {
			lastErr = fmt.Errorf("failed to remove sandbox %s: %w", sandboxPath, err)
			fmt.Fprintf(os.Stderr, "warning: %v\n", lastErr)
			continue
		}
	}

	return lastErr
}
