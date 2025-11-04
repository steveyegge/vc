package executor

import (
	"context"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/google/uuid"
	"github.com/steveyegge/vc/internal/ai"
	"github.com/steveyegge/vc/internal/config"
	"github.com/steveyegge/vc/internal/deduplication"
	"github.com/steveyegge/vc/internal/gates"
	"github.com/steveyegge/vc/internal/git"
	"github.com/steveyegge/vc/internal/health"
	"github.com/steveyegge/vc/internal/sandbox"
	"github.com/steveyegge/vc/internal/storage"
	"github.com/steveyegge/vc/internal/storage/beads"
	"github.com/steveyegge/vc/internal/types"
	"github.com/steveyegge/vc/internal/watchdog"
)

// Executor manages the issue processing event loop
type Executor struct {
	store           storage.Storage
	supervisor      *ai.Supervisor
	monitor         *watchdog.Monitor
	analyzer        *watchdog.Analyzer
	intervention    *watchdog.InterventionController
	watchdogConfig  *watchdog.WatchdogConfig
	sandboxMgr      sandbox.Manager
	healthRegistry  *health.MonitorRegistry
	preFlightChecker *PreFlightChecker              // Preflight quality gates checker (vc-196)
	deduplicator    deduplication.Deduplicator     // Shared deduplicator for sandbox manager and results processor (vc-137)
	gitOps          git.GitOperations              // Git operations for auto-commit (vc-136)
	messageGen      *git.MessageGenerator          // Commit message generator (vc-136)
	qaWorker        *QualityGateWorker             // QA worker for quality gate execution (vc-254)
	config          *Config
	instanceID      string
	hostname        string
	pid             int
	version         string

	// Control channels
	stopCh             chan struct{}
	doneCh             chan struct{}
	watchdogStopCh     chan struct{} // Separate channel for watchdog shutdown
	watchdogDoneCh     chan struct{} // Signals when watchdog goroutine finished
	cleanupStopCh      chan struct{} // Separate channel for cleanup goroutine shutdown
	cleanupDoneCh      chan struct{} // Signals when cleanup goroutine finished
	eventCleanupStopCh chan struct{} // Separate channel for event cleanup shutdown
	eventCleanupDoneCh chan struct{} // Signals when event cleanup goroutine finished

	// Configuration
	pollInterval            time.Duration
	cleanupInterval         time.Duration
	staleThreshold          time.Duration
	instanceCleanupAge      time.Duration
	instanceCleanupKeep     int
	enableAISupervision     bool
	enableQualityGates      bool
	enableSandboxes         bool
	enableHealthMonitoring  bool
	enableQualityGateWorker bool
	workingDir              string

	// State
	mu                   sync.RWMutex
	running              bool
	degradedMode         bool      // In self-healing mode, only claim baseline issues
	degradedModeMsgLast  time.Time // Last time we printed the self-healing mode message (for throttling)
	qaWorkersWg          sync.WaitGroup // Tracks active QA worker goroutines for graceful shutdown (vc-0d58)
}

// isDegraded returns whether the executor is in self-healing mode (thread-safe)
func (e *Executor) isDegraded() bool {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.degradedMode
}

// setDegraded sets the self-healing mode state (thread-safe)
func (e *Executor) setDegraded(v bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.degradedMode = v
}

// Config holds executor configuration
type Config struct {
	Store                   storage.Storage
	Version                 string
	PollInterval            time.Duration
	HeartbeatPeriod         time.Duration
	CleanupInterval         time.Duration                // How often to check for stale instances (default: 5 minutes)
	StaleThreshold          time.Duration                // How long before an instance is considered stale (default: 5 minutes)
	EnableAISupervision     bool                         // Enable AI assessment and analysis (default: true)
	EnableQualityGates      bool                         // Enable quality gates enforcement (default: true)
	EnableAutoCommit        bool                         // Enable automatic git commits after successful execution (default: false, vc-142)
	EnableAutoPR            bool                         // Enable automatic PR creation after successful commit (default: false, requires EnableAutoCommit, vc-389e)
	EnableSandboxes         bool                         // Enable sandbox isolation (default: true, vc-144)
	KeepSandboxOnFailure    bool                         // Keep failed sandboxes for debugging (default: false)
	KeepBranches            bool                         // Keep mission branches after cleanup (default: false)
	SandboxRetentionCount   int                          // Number of failed sandboxes to keep (default: 3, 0 = keep all)
	EnableBlockerPriority   bool                         // Enable blocker-first prioritization (default: true, vc-161)
	EnableHealthMonitoring  bool                         // Enable health monitoring (default: false, opt-in)
	EnableQualityGateWorker bool                         // Enable QA worker for quality gate execution (default: true, vc-254)
	HealthConfigPath        string                       // Path to health_monitors.yaml (default: ".beads/health_monitors.yaml")
	HealthStatePath         string                       // Path to health_state.json (default: ".beads/health_state.json")
	WorkingDir              string                       // Working directory for quality gates (default: ".")
	SandboxRoot             string                       // Root directory for sandboxes (default: ".sandboxes")
	ParentRepo              string                       // Parent repository path (default: ".")
	DefaultBranch           string                       // Default git branch for sandboxes (default: "main")
	WatchdogConfig          *watchdog.WatchdogConfig     // Watchdog configuration (default: conservative defaults)
	DeduplicationConfig     *deduplication.Config        // Deduplication configuration (default: sensible defaults, nil = use defaults)
	EventRetentionConfig    *config.EventRetentionConfig // Event retention and cleanup configuration (default: sensible defaults, nil = use defaults)
	InstanceCleanupAge      time.Duration                // How old stopped instances must be before deletion (default: 24h)
	InstanceCleanupKeep     int                          // Minimum number of stopped instances to keep (default: 10, 0 = keep none)
}

// DefaultConfig returns default executor configuration
func DefaultConfig() *Config {
	return &Config{
		Version:                 "0.1.0",
		PollInterval:            5 * time.Second,
		HeartbeatPeriod:         30 * time.Second,
		CleanupInterval:         5 * time.Minute,
		StaleThreshold:          5 * time.Minute,
		InstanceCleanupAge:      24 * time.Hour,
		InstanceCleanupKeep:     10,
		EnableAISupervision:     true,
		EnableQualityGates:      true,
		EnableSandboxes:         true, // Changed to true for safety (vc-144)
		KeepSandboxOnFailure:    false,
		KeepBranches:            false,
		SandboxRetentionCount:   3,
		EnableBlockerPriority:   true,  // Enable blocker-first prioritization by default (vc-161)
		EnableHealthMonitoring:  false, // Opt-in for now
		EnableQualityGateWorker: true,  // Enable QA worker by default (vc-254)
		HealthConfigPath:        ".beads/health_monitors.yaml",
		HealthStatePath:         ".beads/health_state.json",
		WorkingDir:              ".",
		SandboxRoot:             ".sandboxes",
		ParentRepo:              ".",
		DefaultBranch:           "main",
	}
}

// New creates a new executor instance
func New(cfg *Config) (*Executor, error) {
	if cfg.Store == nil {
		return nil, fmt.Errorf("storage is required")
	}

	hostname, err := os.Hostname()
	if err != nil {
		return nil, fmt.Errorf("failed to get hostname: %w", err)
	}

	// Set default working directory if not specified
	workingDir := cfg.WorkingDir
	if workingDir == "" {
		workingDir = "."
	}

	// Set default sandbox root if not specified
	sandboxRoot := cfg.SandboxRoot
	if sandboxRoot == "" {
		sandboxRoot = ".sandboxes"
	}

	// Set default parent repo if not specified
	parentRepo := cfg.ParentRepo
	if parentRepo == "" {
		parentRepo = "."
	}

	// Set default cleanup interval if not specified
	cleanupInterval := cfg.CleanupInterval
	if cleanupInterval == 0 {
		cleanupInterval = 5 * time.Minute
	}

	// Set default stale threshold if not specified
	staleThreshold := cfg.StaleThreshold
	if staleThreshold == 0 {
		staleThreshold = 5 * time.Minute
	}

	// Set default instance cleanup age if not specified
	instanceCleanupAge := cfg.InstanceCleanupAge
	if instanceCleanupAge == 0 {
		instanceCleanupAge = 24 * time.Hour
	}

	// Set default instance cleanup keep count if not specified
	instanceCleanupKeep := cfg.InstanceCleanupKeep
	if instanceCleanupKeep == 0 {
		instanceCleanupKeep = 10
	}

	e := &Executor{
		store:                   cfg.Store,
		config:                  cfg,
		instanceID:              uuid.New().String(),
		hostname:                hostname,
		pid:                     os.Getpid(),
		version:                 cfg.Version,
		pollInterval:            cfg.PollInterval,
		cleanupInterval:         cleanupInterval,
		staleThreshold:          staleThreshold,
		instanceCleanupAge:      instanceCleanupAge,
		instanceCleanupKeep:     instanceCleanupKeep,
		enableAISupervision:     cfg.EnableAISupervision,
		enableQualityGates:      cfg.EnableQualityGates,
		enableSandboxes:         cfg.EnableSandboxes,
		enableQualityGateWorker: cfg.EnableQualityGateWorker,
		workingDir:              workingDir,
		stopCh:                  make(chan struct{}),
		doneCh:                  make(chan struct{}),
		cleanupStopCh:           make(chan struct{}),
		cleanupDoneCh:           make(chan struct{}),
		eventCleanupStopCh:      make(chan struct{}),
		eventCleanupDoneCh:      make(chan struct{}),
	}

	// Initialize AI supervisor if enabled (do this before sandbox manager to provide deduplicator)
	if cfg.EnableAISupervision {
		supervisor, err := ai.NewSupervisor(&ai.Config{
			Store: cfg.Store,
		})
		if err != nil {
			// Don't fail - just disable AI supervision
			fmt.Fprintf(os.Stderr, "Warning: failed to initialize AI supervisor: %v (continuing without AI supervision)\n", err)
			e.enableAISupervision = false
		} else {
			e.supervisor = supervisor
		}
	}

	// Initialize git operations for auto-commit (vc-136)
	// This is required for auto-commit, test coverage analysis, and code quality analysis
	gitOps, err := git.NewGit(context.Background())
	if err != nil {
		// Don't fail - just log warning and continue without git operations
		fmt.Fprintf(os.Stderr, "Warning: failed to initialize git operations: %v (auto-commit disabled)\n", err)
	} else {
		e.gitOps = gitOps
	}

	// Initialize message generator for auto-commit (vc-136)
	// Only if we have AI supervisor (need API client)
	if e.supervisor != nil {
		// Get Anthropic API key
		apiKey := os.Getenv("ANTHROPIC_API_KEY")
		if apiKey != "" {
			// Create Anthropic client for message generation
			client := anthropic.NewClient(option.WithAPIKey(apiKey))
			e.messageGen = git.NewMessageGenerator(&client, "claude-sonnet-4-5-20250929")
		} else {
			fmt.Fprintf(os.Stderr, "Warning: ANTHROPIC_API_KEY not set (auto-commit message generation disabled)\n")
		}
	}

	// Create deduplicator if we have a supervisor (vc-137, vc-148)
	// Shared by both sandbox manager and results processor
	if e.supervisor != nil {
		// Get deduplication config from executor config or use defaults
		dedupConfig := deduplication.DefaultConfig()
		if cfg.DeduplicationConfig != nil {
			dedupConfig = *cfg.DeduplicationConfig
		}

		var err error
		e.deduplicator, err = deduplication.NewAIDeduplicator(e.supervisor, cfg.Store, dedupConfig)
		if err != nil {
			// Don't fail - just continue without deduplication
			fmt.Fprintf(os.Stderr, "Warning: failed to create deduplicator: %v (continuing without deduplication)\n", err)
			e.deduplicator = nil
		}
	}

	// Initialize sandbox manager if enabled
	if cfg.EnableSandboxes {
		sandboxMgr, err := sandbox.NewManager(sandbox.Config{
			SandboxRoot:         sandboxRoot,
			ParentRepo:          parentRepo,
			MainDB:              cfg.Store,
			Deduplicator:        e.deduplicator, // Use shared deduplicator (vc-137)
			DeduplicationConfig: cfg.DeduplicationConfig,
			PreserveOnFailure:   cfg.KeepSandboxOnFailure, // Preserve failed sandboxes for debugging (vc-134)
			KeepBranches:        cfg.KeepBranches,         // Keep mission branches after cleanup (vc-134)
		})
		if err != nil {
			// Don't fail - just disable sandboxes
			fmt.Fprintf(os.Stderr, "Warning: failed to initialize sandbox manager: %v (continuing without sandboxes)\n", err)
			e.enableSandboxes = false
		} else {
			e.sandboxMgr = sandboxMgr

			// Prune orphaned worktrees on startup (vc-194)
			// This cleans up worktrees left behind by previous crashes
			ctx := context.Background()
			if err := sandbox.PruneWorktrees(ctx, parentRepo); err != nil {
				// Log warning but don't fail - prune is best-effort
				fmt.Fprintf(os.Stderr, "Warning: failed to prune worktrees on startup: %v\n", err)
			}
		}
	}

	// Initialize watchdog system
	e.monitor = watchdog.NewMonitor(watchdog.DefaultConfig())

	// Use provided watchdog config or default
	if cfg.WatchdogConfig == nil {
		e.watchdogConfig = watchdog.DefaultWatchdogConfig()
	} else {
		e.watchdogConfig = cfg.WatchdogConfig
	}

	// Initialize watchdog channels
	e.watchdogStopCh = make(chan struct{})
	e.watchdogDoneCh = make(chan struct{})

	// Initialize analyzer if AI supervision is enabled
	if e.enableAISupervision && e.supervisor != nil {
		analyzer, err := watchdog.NewAnalyzer(&watchdog.AnalyzerConfig{
			Monitor:    e.monitor,
			Supervisor: e.supervisor,
			Store:      cfg.Store,
		})
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to initialize watchdog analyzer: %v (watchdog disabled)\n", err)
		} else {
			e.analyzer = analyzer
		}
	}

	// Initialize intervention controller
	intervention, err := watchdog.NewInterventionController(&watchdog.InterventionControllerConfig{
		Store:              cfg.Store,
		ExecutorInstanceID: e.instanceID,
		MaxHistorySize:     e.watchdogConfig.MaxHistorySize,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to initialize intervention controller: %v (watchdog disabled)\n", err)
	} else {
		e.intervention = intervention
	}

	// Initialize health monitoring if enabled
	if cfg.EnableHealthMonitoring {
		// Set default paths if not specified
		healthStatePath := cfg.HealthStatePath
		if healthStatePath == "" {
			healthStatePath = ".beads/health_state.json"
		}

		// Create health registry
		registry, err := health.NewMonitorRegistry(healthStatePath)
		if err != nil {
			// Don't fail - just disable health monitoring
			fmt.Fprintf(os.Stderr, "Warning: failed to initialize health registry: %v (health monitoring disabled)\n", err)
			e.enableHealthMonitoring = false
		} else {
			e.healthRegistry = registry

			// Register monitors (requires supervisor for AI calls)
			if e.supervisor != nil {
				// Get project root
				projectRoot, err := getProjectRootFromStore(cfg.Store)
				if err != nil {
					fmt.Fprintf(os.Stderr, "Warning: failed to get project root: %v (health monitoring disabled)\n", err)
					e.enableHealthMonitoring = false
				} else {
					// Register file size monitor
					fileSizeMonitor, err := health.NewFileSizeMonitor(projectRoot, e.supervisor)
					if err != nil {
						fmt.Fprintf(os.Stderr, "Warning: failed to create file size monitor: %v\n", err)
					} else {
						if err := registry.Register(fileSizeMonitor); err != nil {
							fmt.Fprintf(os.Stderr, "Warning: failed to register file size monitor: %v\n", err)
						}
					}

					// Register cruft detector
					cruftDetector, err := health.NewCruftDetector(projectRoot, e.supervisor)
					if err != nil {
						fmt.Fprintf(os.Stderr, "Warning: failed to create cruft detector: %v\n", err)
					} else {
						if err := registry.Register(cruftDetector); err != nil {
							fmt.Fprintf(os.Stderr, "Warning: failed to register cruft detector: %v\n", err)
						}
					}
				}
			} else {
				fmt.Fprintf(os.Stderr, "Warning: health monitoring requires AI supervision (health monitoring disabled)\n")
				e.enableHealthMonitoring = false
			}
		}
	}

	// Initialize preflight quality gates checker (vc-196)
	if cfg.EnableQualityGates {
		// Load preflight configuration from environment
		preFlightConfig, err := PreFlightConfigFromEnv()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: invalid preflight configuration: %v (using defaults)\n", err)
			preFlightConfig = DefaultPreFlightConfig()
		}
		preFlightConfig.WorkingDir = workingDir

		// Get VCStorage from storage interface
		vcStorage, ok := cfg.Store.(*beads.VCStorage)
		if !ok {
			fmt.Fprintf(os.Stderr, "Warning: storage is not VCStorage (preflight disabled)\n")
		} else {
			// Create gates runner for preflight checker
			gatesRunner, err := gates.NewRunner(&gates.Config{
				Store:      cfg.Store,
				Supervisor: e.supervisor, // Optional: for AI-driven recovery
				WorkingDir: workingDir,
			})
			if err != nil {
				fmt.Fprintf(os.Stderr, "Warning: failed to create gates runner: %v (preflight disabled)\n", err)
			} else {
				// Create preflight checker
				preFlightChecker, err := NewPreFlightChecker(vcStorage, gatesRunner, preFlightConfig)
				if err != nil {
					fmt.Fprintf(os.Stderr, "Warning: failed to create preflight checker: %v (preflight disabled)\n", err)
				} else {
					e.preFlightChecker = preFlightChecker
					if preFlightConfig.Enabled {
						fmt.Printf("✓ Preflight quality gates enabled (TTL: %v, mode: %s)\n",
							preFlightConfig.CacheTTL, preFlightConfig.FailureMode)
					}
				}
			}
		}
	}

	// Initialize QA worker if enabled (vc-254)
	if cfg.EnableQualityGateWorker && cfg.EnableQualityGates {
		// Create gates runner for QA worker (separate from preflight runner)
		gatesRunner, err := gates.NewRunner(&gates.Config{
			Store:      cfg.Store,
			Supervisor: e.supervisor, // Optional: for AI-driven recovery
			WorkingDir: workingDir,   // Default working dir (will be overridden per-mission)
		})
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to create gates runner for QA worker: %v (QA worker disabled)\n", err)
			e.enableQualityGateWorker = false
		} else {
			qaWorker, err := NewQualityGateWorker(&QualityGateWorkerConfig{
				Store:       cfg.Store,
				Supervisor:  e.supervisor,
				WorkingDir:  workingDir,
				InstanceID:  e.instanceID,
				GatesRunner: gatesRunner,
			})
			if err != nil {
				fmt.Fprintf(os.Stderr, "Warning: failed to create QA worker: %v (QA worker disabled)\n", err)
				e.enableQualityGateWorker = false
			} else {
				e.qaWorker = qaWorker
				fmt.Printf("✓ Quality gate worker enabled (parallel execution)\n")
			}
		}
	}

	return e, nil
}

// Start begins the executor event loop
func (e *Executor) Start(ctx context.Context) error {
	e.mu.Lock()
	if e.running {
		e.mu.Unlock()
		return fmt.Errorf("executor is already running")
	}
	e.running = true
	e.mu.Unlock()

	// Register this executor instance
	instance := &types.ExecutorInstance{
		InstanceID:    e.instanceID,
		Hostname:      e.hostname,
		PID:           e.pid,
		Status:        types.ExecutorStatusRunning,
		StartedAt:     time.Now(),
		LastHeartbeat: time.Now(),
		Version:       e.version,
		Metadata:      "{}",
	}

	if err := e.store.RegisterInstance(ctx, instance); err != nil {
		e.mu.Lock()
		e.running = false
		e.mu.Unlock()
		return fmt.Errorf("failed to register executor instance: %w", err)
	}

	// Clean up orphaned claims and stale instances on startup (vc-109)
	// This runs synchronously before event loop starts to prevent claiming already-claimed issues
	staleThresholdSecs := int(e.staleThreshold.Seconds())
	cleaned, err := e.store.CleanupStaleInstances(ctx, staleThresholdSecs)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to cleanup stale instances on startup: %v\n", err)
		// Don't fail startup - log warning and continue
	} else if cleaned > 0 {
		fmt.Printf("Cleanup: Cleaned up %d stale/orphaned instance(s) on startup\n", cleaned)
	}

	// Clean up orphaned mission branches on startup (vc-135)
	// This runs synchronously to ensure branches are cleaned before claiming work
	if e.enableSandboxes && !e.config.KeepBranches {
		if err := e.cleanupOrphanedBranches(ctx); err != nil {
			// Log warning but don't fail startup
			fmt.Fprintf(os.Stderr, "Warning: failed to cleanup orphaned branches: %v\n", err)
		}
	}

	// Start the event loop
	go e.eventLoop(ctx)

	// Start the watchdog loop if enabled and components are initialized
	if e.watchdogConfig.IsEnabled() && e.analyzer != nil && e.intervention != nil {
		go e.watchdogLoop(ctx)
		fmt.Printf("Watchdog: Started monitoring (check_interval=%v, min_confidence=%.2f, min_severity=%s)\n",
			e.watchdogConfig.GetCheckInterval(),
			e.watchdogConfig.AIConfig.MinConfidenceThreshold,
			e.watchdogConfig.AIConfig.MinSeverityLevel)
	}

	// Start the cleanup loop
	go e.cleanupLoop(ctx)
	fmt.Printf("Cleanup: Started stale instance cleanup (check_interval=%v, stale_threshold=%v)\n",
		e.cleanupInterval, e.staleThreshold)

	// Start the event cleanup loop
	go e.eventCleanupLoop(ctx)

	return nil
}

// Stop gracefully stops the executor
func (e *Executor) Stop(ctx context.Context) error {
	e.mu.Lock()
	if !e.running {
		e.mu.Unlock()
		return fmt.Errorf("executor is not running")
	}
	e.mu.Unlock()

	// Signal shutdown
	close(e.stopCh)

	// Stop watchdog if it's running
	if e.watchdogConfig.IsEnabled() && e.analyzer != nil {
		close(e.watchdogStopCh)
	}

	// Stop cleanup goroutine
	close(e.cleanupStopCh)

	// Stop event cleanup goroutine
	close(e.eventCleanupStopCh)

	// Wait for event loop, watchdog, cleanup, and event cleanup to finish concurrently (vc-113, vc-122, vc-195)
	// This prevents sequential timeouts if one takes longer than expected
	eventDone := false
	watchdogDone := !e.watchdogConfig.IsEnabled() || e.analyzer == nil // Skip if not enabled
	cleanupDone := false
	eventCleanupDone := false

	for !eventDone || !watchdogDone || !cleanupDone || !eventCleanupDone {
		select {
		case <-e.doneCh:
			eventDone = true
		case <-e.watchdogDoneCh:
			watchdogDone = true
		case <-e.cleanupDoneCh:
			cleanupDone = true
		case <-e.eventCleanupDoneCh:
			eventCleanupDone = true
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	// Wait for all QA worker goroutines to complete (vc-0d58)
	// This ensures quality gates finish and release database claims before shutdown
	e.qaWorkersWg.Wait()

	// Update internal state first (vc-192: set running=false before DB update)
	e.mu.Lock()
	e.running = false
	e.mu.Unlock()

	// Prune worktrees on shutdown (vc-194)
	// This is best-effort cleanup - don't fail shutdown if it doesn't work
	if e.enableSandboxes && e.config.ParentRepo != "" {
		if err := sandbox.PruneWorktrees(ctx, e.config.ParentRepo); err != nil {
			fmt.Fprintf(os.Stderr, "warning: failed to prune worktrees on shutdown: %v\n", err)
		}
	}

	// Mark instance as stopped (vc-102: Use UPDATE instead of INSERT)
	if err := e.store.MarkInstanceStopped(ctx, e.instanceID); err != nil {
		fmt.Fprintf(os.Stderr, "warning: failed to mark instance as stopped: %v\n", err)
	}

	// Clean up old stopped instances (vc-133, vc-32)
	// This prevents accumulation of historical instances that are no longer needed
	startTime := time.Now()
	olderThanSeconds := int(e.instanceCleanupAge.Seconds())
	deleted, err := e.store.DeleteOldStoppedInstances(ctx, olderThanSeconds, e.instanceCleanupKeep)
	processingTimeMs := time.Since(startTime).Milliseconds()

	if err != nil {
		// Don't fail shutdown if cleanup fails, just log warning
		fmt.Fprintf(os.Stderr, "warning: failed to cleanup old executor instances: %v\n", err)
		// Log failure event (vc-32)
		e.logInstanceCleanupEvent(ctx, 0, 0, processingTimeMs, olderThanSeconds, e.instanceCleanupKeep, false, err.Error())
	} else {
		if deleted > 0 {
			fmt.Printf("Cleanup: Deleted %d old stopped executor instance(s)\n", deleted)
		}
		// Get count of remaining stopped instances for metrics (vc-32)
		// Note: This is a best-effort query - if it fails, we still log the event with 0 remaining
		instances, err := e.store.GetActiveInstances(ctx)
		stoppedRemaining := 0
		if err == nil {
			// Count instances that are stopped
			for _, inst := range instances {
				if inst.Status == "stopped" {
					stoppedRemaining++
				}
			}
		}
		// Log success event (vc-32)
		e.logInstanceCleanupEvent(ctx, deleted, stoppedRemaining, processingTimeMs, olderThanSeconds, e.instanceCleanupKeep, true, "")
	}

	return nil
}

// IsRunning returns whether the executor is currently running
func (e *Executor) IsRunning() bool {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.running
}

// MarkInstanceStoppedOnExit marks this executor instance as stopped.
// This is called via defer to ensure instance is marked stopped even on abnormal exit.
// It's idempotent - safe to call multiple times.
func (e *Executor) MarkInstanceStoppedOnExit(ctx context.Context) error {
	// Update internal state first (under lock)
	e.mu.Lock()
	wasRunning := e.running
	e.running = false
	e.mu.Unlock()

	// Only call MarkInstanceStopped if we were running
	// This avoids redundant DB calls when Stop() already marked it stopped
	if !wasRunning {
		return nil
	}

	// Mark as stopped in database
	if err := e.store.MarkInstanceStopped(ctx, e.instanceID); err != nil {
		return fmt.Errorf("failed to mark instance as stopped: %w", err)
	}

	return nil
}
