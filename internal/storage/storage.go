package storage

import (
	"context"

	"github.com/steveyegge/vc/internal/types"
)

// Storage defines the interface for issue storage backends
type Storage interface {
	// Issues
	CreateIssue(ctx context.Context, issue *types.Issue, actor string) error
	GetIssue(ctx context.Context, id string) (*types.Issue, error)
	UpdateIssue(ctx context.Context, id string, updates map[string]interface{}, actor string) error
	CloseIssue(ctx context.Context, id string, reason string, actor string) error
	SearchIssues(ctx context.Context, query string, filter types.IssueFilter) ([]*types.Issue, error)

	// Dependencies
	AddDependency(ctx context.Context, dep *types.Dependency, actor string) error
	RemoveDependency(ctx context.Context, issueID, dependsOnID string, actor string) error
	GetDependencies(ctx context.Context, issueID string) ([]*types.Issue, error)
	GetDependents(ctx context.Context, issueID string) ([]*types.Issue, error)
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

	// Lifecycle
	Close() error
}

// Config holds database configuration
type Config struct {
	Backend string // "sqlite" or "postgres"

	// SQLite config
	Path string // database file path

	// PostgreSQL config
	Host     string
	Port     int
	Database string
	User     string
	Password string
	SSLMode  string
}
