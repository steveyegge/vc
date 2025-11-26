package storage

import (
	"context"
	"os"

	"github.com/steveyegge/vc/internal/events"
	"github.com/steveyegge/vc/internal/storage/beads"
	"github.com/steveyegge/vc/internal/types"
)

// ErrStaleIteration is re-exported from the beads package for convenience
// It's returned when attempting to update a plan with a stale iteration number
var ErrStaleIteration = beads.ErrStaleIteration

// VCTransaction provides atomic multi-operation support within a database transaction.
// Operations within a transaction either all succeed (commit) or all fail (rollback).
//
// VCTransaction wraps Beads transactions to accept VC types instead of Beads types,
// providing a type-safe interface for VC code to use transactions.
//
// Example:
//
//	err := store.RunInVCTransaction(ctx, func(tx *storage.VCTransaction) error {
//	    if err := tx.CreateIssue(ctx, issue, actor); err != nil {
//	        return err // Triggers rollback
//	    }
//	    if err := tx.AddDependency(ctx, dep, actor); err != nil {
//	        return err // Triggers rollback
//	    }
//	    return nil // Triggers commit
//	})
//
// vc-3hjg: Added for atomic plan approval workflow
type VCTransaction = beads.VCTransaction

// Storage defines the interface for issue storage backends
//
// IMPORTANT: When adding methods to this interface, you MUST update ALL mock implementations.
// Run ./scripts/find-storage-mocks.sh to find all files that need updates.
// The following test files contain mockStorage implementations:
//   - internal/ai/supervisor_test.go
//   - internal/repl/conversation_test.go
//   - internal/repl/conversation_integration_test.go
//   - internal/watchdog/analyzer_test.go
type Storage interface {
	// Agent Events - structured events extracted from agent output
	StoreAgentEvent(ctx context.Context, event *events.AgentEvent) error
	GetAgentEvents(ctx context.Context, filter events.EventFilter) ([]*events.AgentEvent, error)
	GetAgentEventsByIssue(ctx context.Context, issueID string) ([]*events.AgentEvent, error)
	GetRecentAgentEvents(ctx context.Context, limit int) ([]*events.AgentEvent, error)

	// Event Cleanup - retention policy enforcement (vc-194)
	CleanupEventsByAge(ctx context.Context, retentionDays, criticalRetentionDays, batchSize int) (int, error)
	CleanupEventsByIssueLimit(ctx context.Context, perIssueLimit, batchSize int) (int, error)
	CleanupEventsByGlobalLimit(ctx context.Context, globalLimit, batchSize int) (int, error)
	GetEventCounts(ctx context.Context) (*types.EventCounts, error)
	VacuumDatabase(ctx context.Context) error

	// Issues
	CreateIssue(ctx context.Context, issue *types.Issue, actor string) error
	CreateIssues(ctx context.Context, issues []*types.Issue, actor string) error // vc-3hjg: Bulk create for performance
	GetIssue(ctx context.Context, id string) (*types.Issue, error)
	GetIssues(ctx context.Context, ids []string) (map[string]*types.Issue, error) // vc-58: Batch fetch to avoid N+1 queries. vc-4573: Max batch size 500
	CreateMission(ctx context.Context, mission *types.Mission, actor string) error
	GetMission(ctx context.Context, id string) (*types.Mission, error)
	UpdateMission(ctx context.Context, id string, updates map[string]interface{}, actor string) error
	UpdateIssue(ctx context.Context, id string, updates map[string]interface{}, actor string) error
	CloseIssue(ctx context.Context, id string, reason string, actor string) error
	SearchIssues(ctx context.Context, query string, filter types.IssueFilter) ([]*types.Issue, error)

	// Dependencies
	AddDependency(ctx context.Context, dep *types.Dependency, actor string) error
	RemoveDependency(ctx context.Context, issueID, dependsOnID string, actor string) error
	GetDependencies(ctx context.Context, issueID string) ([]*types.Issue, error)
	GetDependents(ctx context.Context, issueID string) ([]*types.Issue, error)
	GetDependencyRecords(ctx context.Context, issueID string) ([]*types.Dependency, error)
	GetDependencyTree(ctx context.Context, issueID string, maxDepth int) ([]*types.TreeNode, error)
	DetectCycles(ctx context.Context) ([][]*types.Issue, error)

	// Labels
	AddLabel(ctx context.Context, issueID, label, actor string) error
	RemoveLabel(ctx context.Context, issueID, label, actor string) error
	GetLabels(ctx context.Context, issueID string) ([]string, error)
	GetIssuesByLabel(ctx context.Context, label string) ([]*types.Issue, error)

	// Ready Work & Blocking
	GetReadyWork(ctx context.Context, filter types.WorkFilter) ([]*types.Issue, error)
	GetBlockedIssues(ctx context.Context) ([]*types.BlockedIssue, error)
	GetReadyBlockers(ctx context.Context, limit int) ([]*types.Issue, error)
	GetReadyBaselineIssues(ctx context.Context, limit int) ([]*types.Issue, error)                               // vc-1nks: SQL-optimized baseline issue selection
	GetReadyDependentsOfBlockedBaselines(ctx context.Context, limit int) ([]*types.Issue, map[string]string, error) // vc-1nks: SQL-optimized dependent selection

	// Epic Completion (vc-232)
	IsEpicComplete(ctx context.Context, epicID string) (bool, error)

	// Mission Context (vc-233)
	GetMissionForTask(ctx context.Context, taskID string) (*types.MissionContext, error)

	// Quality Gate Workers (vc-252)
	GetMissionsNeedingGates(ctx context.Context) ([]*types.Issue, error)

	// Baseline Diagnostics (vc-9aa9)
	StoreDiagnosis(ctx context.Context, issueID string, diagnosis *types.TestFailureDiagnosis) error
	GetDiagnosis(ctx context.Context, issueID string) (*types.TestFailureDiagnosis, error)

	// Events
	AddComment(ctx context.Context, issueID, actor, comment string) error
	GetEvents(ctx context.Context, issueID string, limit int) ([]*types.Event, error)

	// Statistics
	GetStatistics(ctx context.Context) (*types.Statistics, error)

	// Executor Instances
	RegisterInstance(ctx context.Context, instance *types.ExecutorInstance) error
	MarkInstanceStopped(ctx context.Context, instanceID string) error
	UpdateHeartbeat(ctx context.Context, instanceID string) error
	UpdateSelfHealingMode(ctx context.Context, instanceID string, mode string) error // vc-556f: Persist self-healing mode
	GetActiveInstances(ctx context.Context) ([]*types.ExecutorInstance, error)
	CleanupStaleInstances(ctx context.Context, staleThreshold int) (int, error)
	DeleteOldStoppedInstances(ctx context.Context, olderThanSeconds int, maxToKeep int) (int, error)

	// Issue Execution State (Checkpoint/Resume)
	ClaimIssue(ctx context.Context, issueID, executorInstanceID string) error
	GetExecutionState(ctx context.Context, issueID string) (*types.IssueExecutionState, error)
	UpdateExecutionState(ctx context.Context, issueID string, state types.ExecutionState) error
	SaveCheckpoint(ctx context.Context, issueID string, checkpointData interface{}) error
	GetCheckpoint(ctx context.Context, issueID string) (string, error)

	// Status Change Logging (vc-n4lx) - audit trail for status changes
	LogStatusChange(ctx context.Context, issueID string, newStatus types.Status, actor, reason string)
	LogStatusChangeFromUpdates(ctx context.Context, issueID string, updates map[string]interface{}, actor, reason string)

	// Release functions - these handle execution state cleanup with different semantics:
	// - ReleaseIssue: Low-level primitive that ONLY deletes execution state, does not change issue status
	//   Use when you need fine-grained control or when status is managed separately (e.g., CloseIssue)
	// - ReleaseIssueAndReopen: Marks execution as failed, reopens issue (status -> open), adds error comment
	//   Use for failure/retry scenarios where work should be attempted again
	// - CloseIssue: Deletes execution state AND closes issue (status -> closed)
	//   Use for successful completion (defined in Issue Operations section)
	// - CleanupStaleInstances: Releases execution state AND resets status to open
	//   Use for crash recovery scenarios (defined in Executor Instance Management section)
	ReleaseIssue(ctx context.Context, issueID string) error
	ReleaseIssueAndReopen(ctx context.Context, issueID, actor, errorComment string) error
	RecordWatchdogIntervention(ctx context.Context, issueID string) error // vc-165b: Track intervention for backoff

	// Interrupt/Resume Context (vc-00cu, vc-sibm)
	SaveInterruptMetadata(ctx context.Context, metadata *types.InterruptMetadata) error
	GetInterruptMetadata(ctx context.Context, issueID string) (*types.InterruptMetadata, error)
	MarkInterruptResumed(ctx context.Context, issueID string) error
	DeleteInterruptMetadata(ctx context.Context, issueID string) error
	ListInterruptedIssues(ctx context.Context) ([]*types.InterruptMetadata, error)

	// Execution History
	// GetExecutionHistory retrieves all execution history for an issue (no pagination).
	// Deprecated: Use GetExecutionHistoryPaginated for issues with many attempts.
	GetExecutionHistory(ctx context.Context, issueID string) ([]*types.ExecutionAttempt, error)

	// GetExecutionHistoryPaginated retrieves execution history with pagination (vc-59).
	// limit: maximum number of results (required, must be > 0)
	// offset: number of results to skip (0 = start from beginning)
	// Returns attempts in chronological order (oldest first).
	GetExecutionHistoryPaginated(ctx context.Context, issueID string, limit, offset int) ([]*types.ExecutionAttempt, error)

	RecordExecutionAttempt(ctx context.Context, attempt *types.ExecutionAttempt) error

	// Config
	GetConfig(ctx context.Context, key string) (string, error)
	SetConfig(ctx context.Context, key, value string) error
	GetIssuePrefix(ctx context.Context) (string, error) // vc-0bt1: Get project issue prefix (e.g., "vc", "bd")

	// Mission Plans - ephemeral plan storage with concurrency control (vc-un1o, vc-gxfn, vc-d295)
	// StorePlan stores or updates a mission plan using optimistic locking to prevent concurrent modification races
	// If expectedIteration > 0, it will only update if the current iteration matches (returns ErrStaleIteration on mismatch)
	// If expectedIteration == 0, it will create a new plan or force-update regardless of iteration
	// Returns the new iteration number on success
	StorePlan(ctx context.Context, plan *types.MissionPlan, expectedIteration int) (int, error)

	// GetPlan retrieves the latest plan for a mission (returns nil if no plan exists)
	GetPlan(ctx context.Context, missionID string) (*types.MissionPlan, int, error) // plan, iteration, error

	// GetPlanHistory retrieves all historical iterations of a plan ordered by iteration DESC
	GetPlanHistory(ctx context.Context, missionID string) ([]*types.MissionPlan, error)

	// DeletePlan removes all plan data for a mission (all iterations)
	DeletePlan(ctx context.Context, missionID string) error

	// ListDraftPlans retrieves all plans with status not 'approved' (for cleanup/monitoring)
	ListDraftPlans(ctx context.Context) ([]*types.MissionPlan, error)

	// Transactions
	//
	// RunInVCTransaction executes a function within a database transaction using VC types.
	// All operations within the VCTransaction either succeed together (commit)
	// or fail together (rollback). This is essential for atomic workflows
	// like plan approval where multiple issues, dependencies, and labels
	// must be created as a single unit.
	//
	// VCTransaction wraps Beads transactions to accept VC types instead of Beads types,
	// providing a type-safe interface for VC code to use transactions.
	//
	// Transaction behavior:
	//   - If fn returns nil, the transaction is committed
	//   - If fn returns an error, the transaction is rolled back
	//   - If fn panics, the transaction is rolled back and the panic is re-raised
	//
	// vc-3hjg: Added for atomic plan approval workflow
	RunInVCTransaction(ctx context.Context, fn func(tx *VCTransaction) error) error

	// Lifecycle
	Close() error
}

// Config holds database configuration
type Config struct {
	// Path is the SQLite database file path
	// Default: ".beads/beads.db"
	// Special value ":memory:" creates an in-memory database (useful for tests)
	Path string
}

// DefaultConfig returns a config with sensible defaults
// vc-235: Check VC_DB_PATH environment variable first for test isolation
// vc-rdmh: Changed from vc.db to canonical beads.db name
func DefaultConfig() *Config {
	// vc-235: Allow environment variable override for test isolation
	path := os.Getenv("VC_DB_PATH")
	if path == "" {
		path = ".beads/beads.db"
	}
	return &Config{
		Path: path,
	}
}

// NewStorage creates a new Beads storage backend with VC extensions
// vc-37: Migrated from internal SQLite to Beads library v0.12.0
// vc-235: Respects VC_DB_PATH environment variable for test isolation
// vc-rdmh: Uses canonical beads.db database name
func NewStorage(ctx context.Context, cfg *Config) (Storage, error) {
	if cfg == nil {
		cfg = DefaultConfig()
	}

	// Default to standard path if not specified
	if cfg.Path == "" {
		// vc-235: Check environment variable before falling back to default
		cfg.Path = os.Getenv("VC_DB_PATH")
		if cfg.Path == "" {
			cfg.Path = ".beads/beads.db"
		}
	}

	return beads.NewVCStorage(ctx, cfg.Path)
}
