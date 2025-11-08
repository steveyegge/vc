package executor

import (
	"time"

	"github.com/steveyegge/vc/internal/ai"
	"github.com/steveyegge/vc/internal/deduplication"
	"github.com/steveyegge/vc/internal/git"
	"github.com/steveyegge/vc/internal/sandbox"
	"github.com/steveyegge/vc/internal/storage"
	"github.com/steveyegge/vc/internal/watchdog"
)

// Code review decision thresholds
const (
	// minCodeReviewConfidence is the minimum confidence threshold for skipping code review.
	// If AI confidence is below this threshold, we request review as a safety measure.
	minCodeReviewConfidence = 0.70
)

// ResultsProcessor handles post-execution results collection and tracker updates
type ResultsProcessor struct {
	store              storage.Storage
	supervisor         *ai.Supervisor
	deduplicator       deduplication.Deduplicator // Can be nil to disable deduplication
	gitOps             git.GitOperations
	messageGen         *git.MessageGenerator
	enableQualityGates bool
	enableAutoCommit   bool
	enableAutoPR       bool
	workingDir         string
	actor              string             // The actor performing the update (e.g., "repl", "executor-instance-id")
	sandbox            *sandbox.Sandbox   // The sandbox being used (can be nil if sandboxing is disabled)
	sandboxManager     sandbox.Manager    // Sandbox manager for cleanup operations (can be nil if sandboxing is disabled)
	executor           *Executor          // Reference to parent executor for code review checks (can be nil for REPL)
	watchdogConfig     *watchdog.WatchdogConfig // Watchdog config for backoff reset (vc-an5o, can be nil)
	gatesTimeout       time.Duration      // Quality gates timeout (default: 5 minutes)
}

// ResultsProcessorConfig holds configuration for the results processor
type ResultsProcessorConfig struct {
	Store              storage.Storage
	Supervisor         *ai.Supervisor             // Can be nil to disable AI analysis
	Deduplicator       deduplication.Deduplicator // Can be nil to disable deduplication
	GitOps             git.GitOperations          // Can be nil to disable auto-commit
	MessageGen         *git.MessageGenerator      // Can be nil to disable auto-commit
	EnableQualityGates bool
	EnableAutoCommit   bool
	EnableAutoPR       bool
	WorkingDir         string
	Actor              string           // Actor ID for tracking who made the changes
	Sandbox            *sandbox.Sandbox // The sandbox being used (can be nil if sandboxing is disabled)
	SandboxManager     sandbox.Manager  // Sandbox manager for cleanup operations (can be nil if sandboxing is disabled)
	Executor           *Executor        // Reference to parent executor for code review checks (can be nil for REPL)
	WatchdogConfig     *watchdog.WatchdogConfig // Watchdog config for backoff reset (vc-an5o, can be nil)
	GatesTimeout       time.Duration    // Quality gates timeout (default: 5 minutes if zero)
}

// ProcessingResult contains the outcome of processing agent results
type ProcessingResult struct {
	Completed        bool     // Was the issue marked as completed?
	DiscoveredIssues []string // IDs of discovered issues created
	GatesPassed      bool     // Did quality gates pass?
	CommitHash       string   // Git commit hash (if auto-commit succeeded)
	Summary          string   // Human-readable summary
	AIAnalysis       *ai.Analysis // The AI analysis result (if available)
}
