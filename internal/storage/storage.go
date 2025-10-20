package storage

import (
	"context"

	"github.com/steveyegge/vc/internal/events"
	"github.com/steveyegge/vc/internal/storage/sqlite"
	"github.com/steveyegge/vc/internal/types"
)

// Storage defines the interface for issue storage backends
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
	GetEventCounts(ctx context.Context) (*sqlite.EventCounts, error)
	VacuumDatabase(ctx context.Context) error

	// Issues
	CreateIssue(ctx context.Context, issue *types.Issue, actor string) error
	GetIssue(ctx context.Context, id string) (*types.Issue, error)
	GetMission(ctx context.Context, id string) (*types.Mission, error)
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

	// Events
	AddComment(ctx context.Context, issueID, actor, comment string) error
	GetEvents(ctx context.Context, issueID string, limit int) ([]*types.Event, error)

	// Statistics
	GetStatistics(ctx context.Context) (*types.Statistics, error)

	// Executor Instances
	RegisterInstance(ctx context.Context, instance *types.ExecutorInstance) error
	UpdateHeartbeat(ctx context.Context, instanceID string) error
	GetActiveInstances(ctx context.Context) ([]*types.ExecutorInstance, error)
	CleanupStaleInstances(ctx context.Context, staleThreshold int) (int, error)

	// Issue Execution State (Checkpoint/Resume)
	ClaimIssue(ctx context.Context, issueID, executorInstanceID string) error
	GetExecutionState(ctx context.Context, issueID string) (*types.IssueExecutionState, error)
	UpdateExecutionState(ctx context.Context, issueID string, state types.ExecutionState) error
	SaveCheckpoint(ctx context.Context, issueID string, checkpointData interface{}) error
	GetCheckpoint(ctx context.Context, issueID string) (string, error)
	ReleaseIssue(ctx context.Context, issueID string) error
	ReleaseIssueAndReopen(ctx context.Context, issueID, actor, errorComment string) error

	// Execution History
	GetExecutionHistory(ctx context.Context, issueID string) ([]*types.ExecutionAttempt, error)
	RecordExecutionAttempt(ctx context.Context, attempt *types.ExecutionAttempt) error

	// Config
	GetConfig(ctx context.Context, key string) (string, error)
	SetConfig(ctx context.Context, key, value string) error

	// Lifecycle
	Close() error
}

// Config holds database configuration
type Config struct {
	// Path is the SQLite database file path
	// Default: ".beads/vc.db"
	// Special value ":memory:" creates an in-memory database (useful for tests)
	Path string
}

// DefaultConfig returns a config with sensible defaults
func DefaultConfig() *Config {
	return &Config{
		Path: ".beads/vc.db",
	}
}

// NewStorage creates a new SQLite storage backend
// The ctx parameter is currently unused but kept for API consistency
// and future extension possibilities
func NewStorage(ctx context.Context, cfg *Config) (Storage, error) {
	if cfg == nil {
		cfg = DefaultConfig()
	}

	// Default to standard path if not specified
	if cfg.Path == "" {
		cfg.Path = ".beads/vc.db"
	}

	return sqlite.New(cfg.Path)
}
