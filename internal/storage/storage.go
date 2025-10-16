package storage

import (
	"context"
	"fmt"
	"time"

	"github.com/steveyegge/vc/internal/events"
	"github.com/steveyegge/vc/internal/storage/postgres"
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

	// Lifecycle
	Close() error
}

// Config holds database configuration
type Config struct {
	Backend string // "sqlite" or "postgres"

	// SQLite config
	Path string // database file path

	// PostgreSQL config
	Host            string
	Port            int
	Database        string
	User            string
	Password        string
	SSLMode         string
	MaxConns        int32
	MinConns        int32
	MaxConnLifetime time.Duration
	MaxConnIdleTime time.Duration
	HealthCheck     time.Duration
}

// DefaultConfig returns a config with sensible defaults
func DefaultConfig() *Config {
	return &Config{
		Backend:         "sqlite",
		Path:            ".beads/vc.db",
		Host:            "localhost",
		Port:            5432,
		Database:        "vc",
		User:            "vc",
		SSLMode:         "prefer",
		MaxConns:        25,
		MinConns:        5,
		MaxConnLifetime: 1 * time.Hour,
		MaxConnIdleTime: 30 * time.Minute,
		HealthCheck:     1 * time.Minute,
	}
}

// NewStorage creates a new storage backend based on configuration
//
// Note: There is a known API inconsistency between the backend constructors:
// - sqlite.New(path string) does not accept a context parameter
// - postgres.New(ctx context.Context, cfg *Config) accepts a context parameter
// This means SQLite initialization cannot respect context cancellation, while PostgreSQL can.
// The ctx parameter is only used for PostgreSQL initialization.
func NewStorage(ctx context.Context, cfg *Config) (Storage, error) {
	if cfg == nil {
		cfg = DefaultConfig()
	}

	// Default to sqlite if backend not specified
	if cfg.Backend == "" {
		cfg.Backend = "sqlite"
	}

	// Validate backend type
	switch cfg.Backend {
	case "sqlite":
		// Validate SQLite config
		if cfg.Path == "" {
			return nil, fmt.Errorf("sqlite backend requires Path to be set")
		}
		return sqlite.New(cfg.Path)

	case "postgres":
		// Validate PostgreSQL config
		if cfg.Host == "" {
			return nil, fmt.Errorf("postgres backend requires Host to be set")
		}
		if cfg.Port == 0 {
			return nil, fmt.Errorf("postgres backend requires Port to be set")
		}
		if cfg.Database == "" {
			return nil, fmt.Errorf("postgres backend requires Database to be set")
		}
		if cfg.User == "" {
			return nil, fmt.Errorf("postgres backend requires User to be set")
		}

		// Start with postgres defaults and override with user-provided values
		pgCfg := postgres.DefaultConfig()
		pgCfg.Host = cfg.Host
		pgCfg.Port = cfg.Port
		pgCfg.Database = cfg.Database
		pgCfg.User = cfg.User
		pgCfg.Password = cfg.Password

		// Override pool settings only if explicitly set (non-zero)
		if cfg.SSLMode != "" {
			pgCfg.SSLMode = cfg.SSLMode
		}
		if cfg.MaxConns != 0 {
			pgCfg.MaxConns = cfg.MaxConns
		}
		if cfg.MinConns != 0 {
			pgCfg.MinConns = cfg.MinConns
		}
		if cfg.MaxConnLifetime != 0 {
			pgCfg.MaxConnLifetime = cfg.MaxConnLifetime
		}
		if cfg.MaxConnIdleTime != 0 {
			pgCfg.MaxConnIdleTime = cfg.MaxConnIdleTime
		}
		if cfg.HealthCheck != 0 {
			pgCfg.HealthCheck = cfg.HealthCheck
		}

		return postgres.New(ctx, pgCfg)

	default:
		return nil, fmt.Errorf("unsupported backend: %s (must be 'sqlite' or 'postgres')", cfg.Backend)
	}
}
