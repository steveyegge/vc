package executor

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/google/uuid"
	"github.com/steveyegge/vc/internal/ai"
	"github.com/steveyegge/vc/internal/config"
	"github.com/steveyegge/vc/internal/cost"
	"github.com/steveyegge/vc/internal/deduplication"
	"github.com/steveyegge/vc/internal/events"
	"github.com/steveyegge/vc/internal/gates"
	"github.com/steveyegge/vc/internal/git"
	"github.com/steveyegge/vc/internal/health"
	"github.com/steveyegge/vc/internal/sandbox"
	"github.com/steveyegge/vc/internal/storage"
	"github.com/steveyegge/vc/internal/storage/beads"
	"github.com/steveyegge/vc/internal/types"
	"github.com/steveyegge/vc/internal/watchdog"
)

// SelfHealingMode represents the self-healing state machine state
type SelfHealingMode int

const (
	// ModeHealthy indicates normal operation - baseline quality gates passing
	ModeHealthy SelfHealingMode = iota
	// ModeSelfHealing indicates baseline failed and executor is actively trying to fix it
	ModeSelfHealing
	// ModeEscalated indicates baseline is broken and needs human intervention
	ModeEscalated
)

// String returns a human-readable string representation of the mode
func (m SelfHealingMode) String() string {
	switch m {
	case ModeHealthy:
		return "HEALTHY"
	case ModeSelfHealing:
		return "SELF_HEALING"
	case ModeEscalated:
		return "ESCALATED"
	default:
		return fmt.Sprintf("UNKNOWN(%d)", m)
	}
}

// Executor manages the issue processing event loop
type Executor struct {
	store            storage.Storage
	supervisor       *ai.Supervisor
	watchdog         *watchdog.Watchdog    // Unified watchdog instance (vc-mq3c)
	monitor          *watchdog.Monitor     // Standalone monitor when watchdog disabled (vc-mq3c)
	watchdogConfig   *watchdog.WatchdogConfig // Watchdog config (needed by result processor)
	sandboxMgr       sandbox.Manager
	healthRegistry   *health.MonitorRegistry
	preFlightChecker *PreFlightChecker          // Preflight quality gates checker (vc-196)
	deduplicator     deduplication.Deduplicator // Shared deduplicator for sandbox manager and results processor (vc-137)
	gitOps           git.GitOperations          // Git operations for auto-commit (vc-136)
	messageGen       *git.MessageGenerator      // Commit message generator (vc-136)
	qaWorker         *QualityGateWorker         // QA worker for quality gate execution (vc-254)
	costTracker      *cost.Tracker              // Cost budget tracker (vc-e3s7)
	loopDetector     *LoopDetector              // Loop detector for unproductive patterns (vc-0vfg)
	config           *Config
	instanceID       string
	hostname         string
	pid              int
	version          string

	// Control channels
	stopCh             chan struct{}
	doneCh             chan struct{}
	heartbeatStopCh    chan struct{} // Separate channel for heartbeat shutdown (vc-m4od)
	heartbeatDoneCh    chan struct{} // Signals when heartbeat goroutine finished (vc-m4od)
	cleanupStopCh      chan struct{} // Separate channel for cleanup goroutine shutdown
	cleanupDoneCh      chan struct{} // Signals when cleanup goroutine finished
	eventCleanupStopCh chan struct{} // Separate channel for event cleanup shutdown
	eventCleanupDoneCh chan struct{} // Signals when event cleanup goroutine finished

	// Configuration
	pollInterval            time.Duration
	heartbeatPeriod         time.Duration // vc-m4od: Period for heartbeat updates
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
	mu                 sync.RWMutex
	running            bool
	selfHealingMsgLast time.Time      // Last time we printed the self-healing mode message (for throttling)
	qaWorkersWg        sync.WaitGroup // Tracks active QA worker goroutines for graceful shutdown (vc-0d58)

	// Self-healing state machine (vc-23t0)
	selfHealingMode SelfHealingMode // Current state in the self-healing state machine
	modeMutex       sync.RWMutex    // Protects selfHealingMode and modeChangedAt
	modeChangedAt   time.Time       // When the mode last changed (for escalation thresholds)

	// Escalation tracking (vc-h8b8)
	escalationTrackers map[string]*escalationTracker // Maps baseline issue ID to escalation state
	escalationMutex    sync.RWMutex                  // Protects escalationTrackers map

	// Self-healing progress tracking (vc-ipoj)
	selfHealingLastProgress   time.Time // Last time we made progress (claimed or completed baseline work)
	selfHealingProgressMutex  sync.RWMutex
	selfHealingNoWorkCount    int       // Consecutive iterations with no work found
	selfHealingDeadlockIssue  string    // ID of escalation issue created for deadlock (empty if none)

	// Steady state polling (vc-onch)
	basePollInterval    time.Duration // Original poll interval (5s)
	currentPollInterval time.Duration // Dynamic poll interval (increases in steady state)
	steadyStateCount    int           // Consecutive polls in steady state
	lastGitCommit       string        // Last seen git commit hash
	steadyStateMutex    sync.RWMutex  // Protects steady state fields
}

// getSelfHealingMode returns the current self-healing mode state (thread-safe)
func (e *Executor) getSelfHealingMode() SelfHealingMode {
	e.modeMutex.RLock()
	defer e.modeMutex.RUnlock()
	return e.selfHealingMode
}

// getModeChangedAt returns when the mode last changed (thread-safe)
func (e *Executor) getModeChangedAt() time.Time {
	e.modeMutex.RLock()
	defer e.modeMutex.RUnlock()
	return e.modeChangedAt
}

// transitionToHealthy transitions to HEALTHY state (baseline passing)
func (e *Executor) transitionToHealthy(ctx context.Context) {
	e.modeMutex.Lock()
	oldMode := e.selfHealingMode
	if oldMode == ModeHealthy {
		e.modeMutex.Unlock()
		return // Already healthy, no transition needed
	}
	e.selfHealingMode = ModeHealthy
	e.modeChangedAt = time.Now()
	e.modeMutex.Unlock()

	// Clear all escalation trackers since baseline is now healthy (vc-h8b8)
	e.clearAllTrackers()

	// Log transition
	fmt.Printf("✓ State transition: %s → HEALTHY (baseline quality gates passing)\n", oldMode)

	// Emit activity feed event
	e.logEvent(ctx, events.EventTypeExecutorSelfHealingMode, events.SeverityInfo, "SYSTEM",
		fmt.Sprintf("Executor transitioned from %s to HEALTHY", oldMode),
		map[string]interface{}{
			"from_mode": oldMode.String(),
			"to_mode":   "HEALTHY",
			"timestamp": time.Now().Format(time.RFC3339),
		})
}

// transitionToSelfHealing transitions to SELF_HEALING state (baseline failed)
func (e *Executor) transitionToSelfHealing(ctx context.Context) {
	e.modeMutex.Lock()
	oldMode := e.selfHealingMode
	if oldMode == ModeSelfHealing {
		e.modeMutex.Unlock()
		return // Already in self-healing, no transition needed
	}
	e.selfHealingMode = ModeSelfHealing
	e.modeChangedAt = time.Now()
	e.modeMutex.Unlock()

	// Initialize self-healing progress tracking (vc-ipoj)
	e.selfHealingProgressMutex.Lock()
	e.selfHealingLastProgress = time.Now()
	e.selfHealingNoWorkCount = 0
	e.selfHealingDeadlockIssue = ""
	e.selfHealingProgressMutex.Unlock()

	// Log transition
	fmt.Printf("⚠️  State transition: %s → SELF_HEALING (baseline failed, attempting fix)\n", oldMode)

	// Emit activity feed event
	e.logEvent(ctx, events.EventTypeExecutorSelfHealingMode, events.SeverityWarning, "SYSTEM",
		fmt.Sprintf("Executor transitioned from %s to SELF_HEALING", oldMode),
		map[string]interface{}{
			"from_mode": oldMode.String(),
			"to_mode":   "SELF_HEALING",
			"timestamp": time.Now().Format(time.RFC3339),
		})
}

// transitionToEscalated transitions to ESCALATED state (needs human intervention)
func (e *Executor) transitionToEscalated(ctx context.Context, reason string) {
	e.modeMutex.Lock()
	oldMode := e.selfHealingMode
	if oldMode == ModeEscalated {
		e.modeMutex.Unlock()
		return // Already escalated, no transition needed
	}
	e.selfHealingMode = ModeEscalated
	e.modeChangedAt = time.Now()
	e.modeMutex.Unlock()

	// Log transition with reason
	fmt.Printf("🚨 State transition: %s → ESCALATED (reason: %s)\n", oldMode, reason)

	// Emit activity feed event
	e.logEvent(ctx, events.EventTypeExecutorSelfHealingMode, events.SeverityCritical, "SYSTEM",
		fmt.Sprintf("Executor transitioned from %s to ESCALATED: %s", oldMode, reason),
		map[string]interface{}{
			"from_mode": oldMode.String(),
			"to_mode":   "ESCALATED",
			"reason":    reason,
			"timestamp": time.Now().Format(time.RFC3339),
		})
}

// Config holds executor configuration
//
// Supported Degraded Modes (vc-q5ve):
// The executor supports several degraded operating modes when optional components fail to initialize:
//
// 1. No AI Supervision (EnableAISupervision=false or init failure):
//    - Issues are claimed and executed without assessment/analysis
//    - No loop detection
//    - No health monitoring
//    - No auto-commit message generation
//    - No deduplication
//
// 2. No Quality Gates (EnableQualityGates=false):
//    - No preflight checks
//    - No quality gate enforcement
//    - No QA worker
//
// 3. No Sandboxes (EnableSandboxes=false or init failure):
//    - Work executes directly in parent repo (less isolation)
//    - No sandbox cleanup
//    - Higher risk of repo contamination
//
// 4. No Git Operations (git init failure):
//    - No auto-commit
//    - No auto-PR
//    - Test coverage analysis disabled
//    - Code quality analysis disabled
//
// 5. No Cost Tracking (cost config disabled or init failure):
//    - Budget enforcement disabled
//    - AI calls proceed without cost checks
//
// Minimum Viable Configuration:
// - Store must be non-nil
// - All timing values (PollInterval, HeartbeatPeriod, etc.) must be non-negative
// - Dependent features must have their requirements enabled (see Validate())
//
// The executor will log warnings when optional components fail but will continue
// with reduced functionality. Use Validate() to check for configuration errors
// before calling New().
type Config struct {
	Store                   storage.Storage
	Version                 string
	PollInterval            time.Duration
	HeartbeatPeriod         time.Duration
	CleanupInterval         time.Duration                // How often to check for stale instances (default: 5 minutes)
	StaleThreshold          time.Duration                // How long before an instance is considered stale (default: 5 minutes)
	EnableAISupervision     bool                         // Enable AI assessment and analysis (default: true)
	EnableQualityGates      bool                         // Enable quality gates enforcement (default: true)
	GatesTimeout            time.Duration                // Quality gates timeout (default: 5 minutes, env: VC_QUALITY_GATES_TIMEOUT, vc-xcfw)
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
	MaxEscalationAttempts   int                          // Maximum attempts before escalating baseline issues (default: 5, vc-h8b8)
	MaxEscalationDuration   time.Duration                // Maximum duration in self-healing mode before escalating (default: 24h, vc-h8b8)
	MaxIncompleteRetries    int                          // Maximum retries for incomplete work before escalation (default: 1, vc-hsfz)

	// Self-healing configuration (vc-tn9c)
	SelfHealingMaxAttempts     int           // Maximum attempts before escalating (same as MaxEscalationAttempts, default: 5)
	SelfHealingMaxDuration     time.Duration // Maximum duration before escalating (same as MaxEscalationDuration, default: 24h)
	SelfHealingRecheckInterval time.Duration // How often to recheck in self-healing mode (default: 5m)
	SelfHealingVerboseLogging  bool          // Enable verbose logging for self-healing decisions (default: true)
	SelfHealingDeadlockTimeout time.Duration // Timeout for detecting deadlocked baselines (default: 30m, vc-ipoj)

	// Loop detector configuration (vc-0vfg)
	LoopDetectorConfig *LoopDetectorConfig // Loop detector configuration (default: sensible defaults, nil = use defaults)

	// Bootstrap mode configuration (vc-b027)
	EnableBootstrapMode     bool     // Enable bootstrap mode during quota crisis (default: false, opt-in)
	BootstrapModeLabels     []string // Labels that trigger bootstrap mode (default: ["quota-crisis"])
	BootstrapModeTitleKeywords []string // Title keywords that trigger bootstrap mode (default: ["quota", "budget", "cost", "API limit"])
}

// Validate checks the configuration for invalid combinations (vc-q5ve)
// Returns an error if the configuration is invalid or unsupported
func (c *Config) Validate() error {
	// Minimum required configuration: Store must be present
	if c.Store == nil {
		return fmt.Errorf("storage is required")
	}

	// Auto-PR requires auto-commit (vc-389e)
	if c.EnableAutoPR && !c.EnableAutoCommit {
		return fmt.Errorf("EnableAutoPR requires EnableAutoCommit to be enabled")
	}

	// Quality gate worker requires quality gates
	if c.EnableQualityGateWorker && !c.EnableQualityGates {
		return fmt.Errorf("EnableQualityGateWorker requires EnableQualityGates to be enabled")
	}

	// Health monitoring requires AI supervision (monitors use AI for analysis)
	if c.EnableHealthMonitoring && !c.EnableAISupervision {
		return fmt.Errorf("EnableHealthMonitoring requires EnableAISupervision to be enabled")
	}

	// Auto-commit requires git operations (implicit, will fail during init, but we can validate)
	// This is a soft requirement - we'll just log a warning during initialization

	// Validate timing configurations
	if c.PollInterval < 0 {
		return fmt.Errorf("PollInterval must be non-negative, got %v", c.PollInterval)
	}
	if c.HeartbeatPeriod < 0 {
		return fmt.Errorf("HeartbeatPeriod must be non-negative, got %v", c.HeartbeatPeriod)
	}
	if c.CleanupInterval < 0 {
		return fmt.Errorf("CleanupInterval must be non-negative, got %v", c.CleanupInterval)
	}
	if c.StaleThreshold < 0 {
		return fmt.Errorf("StaleThreshold must be non-negative, got %v", c.StaleThreshold)
	}

	// Validate self-healing configuration
	if c.SelfHealingMaxAttempts < 0 {
		return fmt.Errorf("SelfHealingMaxAttempts must be non-negative, got %d", c.SelfHealingMaxAttempts)
	}
	if c.SelfHealingMaxDuration < 0 {
		return fmt.Errorf("SelfHealingMaxDuration must be non-negative, got %v", c.SelfHealingMaxDuration)
	}
	if c.SelfHealingRecheckInterval < 0 {
		return fmt.Errorf("SelfHealingRecheckInterval must be non-negative, got %v", c.SelfHealingRecheckInterval)
	}

	// Sandbox retention count must be non-negative
	if c.SandboxRetentionCount < 0 {
		return fmt.Errorf("SandboxRetentionCount must be non-negative, got %d", c.SandboxRetentionCount)
	}

	return nil
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
		GatesTimeout:            getEnvDuration("VC_QUALITY_GATES_TIMEOUT", 5*time.Minute), // Configurable timeout (vc-xcfw)
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
		// Self-healing / Escalation configuration (vc-h8b8, vc-tn9c)
		// MaxEscalation* fields are legacy, use SelfHealing* fields for consistency
		MaxEscalationAttempts:      getEnvInt("VC_SELF_HEALING_MAX_ATTEMPTS", 5),
		MaxEscalationDuration:      getEnvDuration("VC_SELF_HEALING_MAX_DURATION", 24*time.Hour),
		MaxIncompleteRetries:       getEnvInt("VC_MAX_INCOMPLETE_RETRIES", 1), // vc-hsfz
		SelfHealingMaxAttempts:     getEnvInt("VC_SELF_HEALING_MAX_ATTEMPTS", 5),
		SelfHealingMaxDuration:     getEnvDuration("VC_SELF_HEALING_MAX_DURATION", 24*time.Hour),
		SelfHealingRecheckInterval: getEnvDuration("VC_SELF_HEALING_RECHECK_INTERVAL", 5*time.Minute),
		SelfHealingVerboseLogging:  getEnvBool("VC_SELF_HEALING_VERBOSE_LOGGING", true),
		SelfHealingDeadlockTimeout: getEnvDuration("VC_SELF_HEALING_DEADLOCK_TIMEOUT", 30*time.Minute),
		// Bootstrap mode (vc-b027) - disabled by default (opt-in)
		EnableBootstrapMode:        getEnvBool("VC_ENABLE_BOOTSTRAP_MODE", false),
		BootstrapModeLabels:        getEnvStringSlice("VC_BOOTSTRAP_MODE_LABELS", []string{"quota-crisis"}),
		BootstrapModeTitleKeywords: getEnvStringSlice("VC_BOOTSTRAP_MODE_TITLE_KEYWORDS", []string{"quota", "budget", "cost", "API limit"}),
	}
}

// New creates a new executor instance
func New(cfg *Config) (*Executor, error) {
	// Validate configuration before initialization (vc-q5ve)
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid configuration: %w", err)
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

	// Set default heartbeat period if not specified (vc-m4od)
	heartbeatPeriod := cfg.HeartbeatPeriod
	if heartbeatPeriod == 0 {
		heartbeatPeriod = 30 * time.Second
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
		heartbeatPeriod:         heartbeatPeriod,
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
		heartbeatStopCh:         make(chan struct{}),
		heartbeatDoneCh:         make(chan struct{}),
		cleanupStopCh:           make(chan struct{}),
		cleanupDoneCh:           make(chan struct{}),
		eventCleanupStopCh:      make(chan struct{}),
		eventCleanupDoneCh:      make(chan struct{}),
		// Initialize self-healing state machine (vc-23t0)
		selfHealingMode: ModeHealthy,
		modeChangedAt:   time.Now(),
		// Initialize escalation tracking (vc-h8b8)
		escalationTrackers: make(map[string]*escalationTracker),
		// Initialize steady state polling (vc-onch)
		basePollInterval:    cfg.PollInterval,
		currentPollInterval: cfg.PollInterval,
		steadyStateCount:    0,
	}

	// Initialize cost tracker first (vc-e3s7)
	// This is initialized even if AI supervision is disabled, for budget monitoring
	var costTracker *cost.Tracker
	costConfig := cost.LoadFromEnv()
	if costConfig.Enabled {
		tracker, err := cost.NewTracker(costConfig, cfg.Store)
		if err != nil {
			// Log warning but continue without cost tracking
			fmt.Fprintf(os.Stderr, "Warning: failed to initialize cost tracker: %v (continuing without cost budgeting)\n", err)
		} else {
			costTracker = tracker
			fmt.Printf("✓ Cost budget tracking enabled (limit: %d tokens/hour, $%.2f/hour)\n",
				costConfig.MaxTokensPerHour, costConfig.MaxCostPerHour)
		}
	}
	e.costTracker = costTracker

	// Initialize AI supervisor if enabled (do this after cost tracker)
	if cfg.EnableAISupervision {
		supervisor, err := ai.NewSupervisor(&ai.Config{
			Store:       cfg.Store,
			CostTracker: costTracker, // Pass cost tracker to supervisor (vc-e3s7)
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
			// Create Anthropic client for message generation (vc-35: using Haiku for cost efficiency)
			client := anthropic.NewClient(option.WithAPIKey(apiKey))
			e.messageGen = git.NewMessageGenerator(&client, ai.GetSimpleTaskModel())
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

	// Initialize unified watchdog system (vc-mq3c)
	// Set up watchdog config first
	e.watchdogConfig = cfg.WatchdogConfig
	if e.watchdogConfig == nil {
		e.watchdogConfig = watchdog.DefaultWatchdogConfig()
	}

	// Only create full watchdog if AI supervision is enabled (since it requires supervisor)
	if e.enableAISupervision && e.supervisor != nil {
		wd, err := watchdog.NewWatchdog(&watchdog.WatchdogDeps{
			Store:              cfg.Store,
			Supervisor:         e.supervisor,
			ExecutorInstanceID: e.instanceID,
			Config:             e.watchdogConfig,
		})
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to initialize watchdog: %v (watchdog disabled)\n", err)
			// Fall back to standalone monitor
			e.monitor = watchdog.NewMonitor(watchdog.DefaultConfig())
		} else {
			e.watchdog = wd
		}
	} else {
		// AI supervision disabled - create standalone monitor for telemetry
		e.monitor = watchdog.NewMonitor(watchdog.DefaultConfig())
	}

	// Initialize health monitoring if enabled
	if cfg.EnableHealthMonitoring {
		// Set default paths if not specified
		healthStatePath := cfg.HealthStatePath
		if healthStatePath == "" {
			healthStatePath = ".beads/health_state.json"
		}
		// Resolve relative to workingDir
		if !filepath.IsAbs(healthStatePath) {
			healthStatePath = filepath.Join(workingDir, healthStatePath)
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

					// Register build modernizer
					buildModernizer, err := health.NewBuildModernizer(projectRoot, e.supervisor)
					if err != nil {
						fmt.Fprintf(os.Stderr, "Warning: failed to create build modernizer: %v\n", err)
					} else {
						if err := registry.Register(buildModernizer); err != nil {
							fmt.Fprintf(os.Stderr, "Warning: failed to register build modernizer: %v\n", err)
						}
					}

					// Register CI/CD reviewer
					cicdReviewer, err := health.NewCICDReviewer(projectRoot, e.supervisor)
					if err != nil {
						fmt.Fprintf(os.Stderr, "Warning: failed to create CI/CD reviewer: %v\n", err)
					} else {
						if err := registry.Register(cicdReviewer); err != nil {
							fmt.Fprintf(os.Stderr, "Warning: failed to register CI/CD reviewer: %v\n", err)
						}
					}

					// Register dependency auditor
					dependencyAuditor, err := health.NewDependencyAuditor(projectRoot, e.supervisor)
					if err != nil {
						fmt.Fprintf(os.Stderr, "Warning: failed to create dependency auditor: %v\n", err)
					} else {
						if err := registry.Register(dependencyAuditor); err != nil {
							fmt.Fprintf(os.Stderr, "Warning: failed to register dependency auditor: %v\n", err)
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

	// Initialize loop detector if AI supervision is enabled (vc-0vfg)
	// Loop detector requires AI supervisor to analyze activity patterns
	if e.enableAISupervision && e.supervisor != nil {
		loopDetectorConfig := cfg.LoopDetectorConfig
		if loopDetectorConfig == nil {
			loopDetectorConfig = DefaultLoopDetectorConfig()
		}

		loopDetector := NewLoopDetector(loopDetectorConfig, cfg.Store, e.supervisor, e.instanceID)
		e.loopDetector = loopDetector

		if loopDetectorConfig.Enabled {
			fmt.Printf("✓ Loop detector enabled (check_interval=%v, lookback=%v, min_confidence=%.2f)\n",
				loopDetectorConfig.CheckInterval, loopDetectorConfig.LookbackWindow, loopDetectorConfig.MinConfidenceThreshold)
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

	// Start the heartbeat loop (vc-m4od)
	go e.heartbeatLoop(ctx)

	// Start the unified watchdog if initialized (vc-mq3c)
	if e.watchdog != nil {
		if err := e.watchdog.Start(ctx); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to start watchdog: %v\n", err)
		}
	}

	// Start the cleanup loop
	go e.cleanupLoop(ctx)
	fmt.Printf("Cleanup: Started stale instance cleanup (check_interval=%v, stale_threshold=%v)\n",
		e.cleanupInterval, e.staleThreshold)

	// Start the event cleanup loop
	go e.eventCleanupLoop(ctx)

	// Start the loop detector if enabled (vc-0vfg)
	if e.loopDetector != nil {
		e.loopDetector.Start(ctx)
	}

	return nil
}

// getMonitor returns the telemetry monitor (vc-mq3c)
// If watchdog is enabled, returns its monitor; otherwise returns standalone monitor
func (e *Executor) getMonitor() *watchdog.Monitor {
	if e.watchdog != nil {
		return e.watchdog.GetMonitor()
	}
	return e.monitor
}

// heartbeatLoop sends periodic heartbeats independently of issue execution (vc-m4od)
// This runs in a separate goroutine to ensure heartbeats continue even when
// processNextIssue() blocks for extended periods during agent execution.
func (e *Executor) heartbeatLoop(ctx context.Context) {
	defer close(e.heartbeatDoneCh)

	ticker := time.NewTicker(e.heartbeatPeriod)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-e.stopCh:
			return
		case <-e.heartbeatStopCh:
			return
		case <-ticker.C:
			if err := e.store.UpdateHeartbeat(ctx, e.instanceID); err != nil {
				fmt.Fprintf(os.Stderr, "heartbeat update failed: %v\n", err)
			}
		}
	}
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

	// Stop heartbeat goroutine (vc-m4od)
	close(e.heartbeatStopCh)

	// Stop unified watchdog if it's running (vc-mq3c)
	if e.watchdog != nil {
		e.watchdog.Stop()
	}

	// Stop cleanup goroutine
	close(e.cleanupStopCh)

	// Stop event cleanup goroutine
	close(e.eventCleanupStopCh)

	// Stop loop detector if it's running (vc-0vfg)
	if e.loopDetector != nil {
		e.loopDetector.Stop()
	}

	// Wait for event loop, heartbeat, cleanup, and event cleanup to finish concurrently (vc-m4od, vc-113, vc-122, vc-195, vc-mq3c)
	// This prevents sequential timeouts if one takes longer than expected
	// Note: watchdog manages its own lifecycle and doesn't need to be waited on here
	eventDone := false
	heartbeatDone := false
	cleanupDone := false
	eventCleanupDone := false

	for !eventDone || !heartbeatDone || !cleanupDone || !eventCleanupDone {
		select {
		case <-e.doneCh:
			eventDone = true
		case <-e.heartbeatDoneCh:
			heartbeatDone = true
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

// checkSteadyState detects if the executor is in a steady state (no work, baseline failing).
// Returns true if we should increase poll interval.
// vc-onch: Reduce preflight thrashing when no work available
func (e *Executor) checkSteadyState(_ context.Context, foundWork bool) (bool, error) {
	e.steadyStateMutex.Lock()
	defer e.steadyStateMutex.Unlock()

	// Get current git commit using same approach as preflight checker
	cmd := exec.Command("git", "rev-parse", "HEAD")
	cmd.Dir = e.workingDir
	output, err := cmd.Output()
	if err != nil {
		// If we can't get git commit, reset to avoid false steady state
		e.resetSteadyStateUnlocked()
		return false, nil // Don't fail the loop, just skip steady state detection
	}
	currentCommit := strings.TrimSpace(string(output))

	// Check for state changes that invalidate steady state
	stateChanged := false
	if e.lastGitCommit != "" && e.lastGitCommit != currentCommit {
		fmt.Printf("State change detected: git commit changed (%s → %s), resetting poll interval\n",
			safePrefix(e.lastGitCommit, 7), safePrefix(currentCommit, 7))
		stateChanged = true

		// vc-onch: Invalidate preflight cache on git commit change
		// This allows executor to detect when baseline issues are fixed
		if e.preFlightChecker != nil {
			e.preFlightChecker.InvalidateAllCache()
		}
	}

	// Update tracked state
	e.lastGitCommit = currentCommit

	// If state changed or work was found, reset steady state
	if stateChanged || foundWork {
		e.resetSteadyStateUnlocked()
		return false, nil
	}

	// Check if we're in a state that qualifies for steady state:
	// 1. In self-healing mode (baseline failed)
	// 2. No work found
	// 3. Preflight checker has cached results (baseline checked and cached)
	inSelfHealing := e.getSelfHealingMode() != ModeHealthy
	hasCachedBaseline := e.preFlightChecker != nil && e.preFlightChecker.HasAnyCachedResults()

	if inSelfHealing && !foundWork && hasCachedBaseline {
		e.steadyStateCount++

		// Consider it steady state after 10 consecutive identical polls
		if e.steadyStateCount >= 10 {
			return true, nil
		}
	} else {
		// Not in qualifying state, reset counter
		e.steadyStateCount = 0
	}

	return false, nil
}

// resetSteadyStateUnlocked resets steady state (assumes lock is held)
func (e *Executor) resetSteadyStateUnlocked() {
	if e.currentPollInterval != e.basePollInterval {
		fmt.Printf("Resetting poll interval: %v → %v\n", e.currentPollInterval, e.basePollInterval)
	}
	e.currentPollInterval = e.basePollInterval
	e.steadyStateCount = 0
}

// increasePollInterval increases the poll interval using exponential backoff
// vc-onch: 5s → 10s → 30s → 60s → 300s (max)
func (e *Executor) increasePollInterval() {
	e.steadyStateMutex.Lock()
	defer e.steadyStateMutex.Unlock()

	oldInterval := e.currentPollInterval

	// Exponential backoff schedule
	switch {
	case e.currentPollInterval < 10*time.Second:
		e.currentPollInterval = 10 * time.Second
	case e.currentPollInterval < 30*time.Second:
		e.currentPollInterval = 30 * time.Second
	case e.currentPollInterval < 60*time.Second:
		e.currentPollInterval = 60 * time.Second
	case e.currentPollInterval < 300*time.Second:
		e.currentPollInterval = 300 * time.Second
	default:
		// Already at max, no change
		return
	}

	fmt.Printf("Entering steady state: increasing poll interval %v → %v\n", oldInterval, e.currentPollInterval)
}

// getCurrentPollInterval returns the current dynamic poll interval (thread-safe)
func (e *Executor) getCurrentPollInterval() time.Duration {
	e.steadyStateMutex.RLock()
	defer e.steadyStateMutex.RUnlock()
	return e.currentPollInterval
}

// safePrefix returns the first n characters of s, or the entire string if shorter than n.
// Used for safely slicing git commit hashes that might be shorter than expected.
func safePrefix(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

// getEnvInt retrieves an integer from an environment variable, or returns the default value
func getEnvInt(key string, defaultValue int) int {
	if value := os.Getenv(key); value != "" {
		if intValue, err := strconv.Atoi(value); err == nil {
			return intValue
		}
	}
	return defaultValue
}

// getEnvDuration retrieves a duration from an environment variable, or returns the default value
func getEnvDuration(key string, defaultValue time.Duration) time.Duration {
	if value := os.Getenv(key); value != "" {
		if duration, err := time.ParseDuration(value); err == nil {
			return duration
		}
	}
	return defaultValue
}

// getEnvBool retrieves a boolean from an environment variable, or returns the default value
func getEnvBool(key string, defaultValue bool) bool {
	if value := os.Getenv(key); value != "" {
		if boolValue, err := strconv.ParseBool(value); err == nil {
			return boolValue
		}
	}
	return defaultValue
}

// getEnvStringSlice retrieves a comma-separated string slice from an environment variable, or returns the default value
func getEnvStringSlice(key string, defaultValue []string) []string {
	if value := os.Getenv(key); value != "" {
		return strings.Split(value, ",")
	}
	return defaultValue
}
