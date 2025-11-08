package discovery

import (
	"context"
	"time"

	"github.com/steveyegge/vc/internal/health"
)

// DiscoveryWorker defines the interface for codebase discovery workers.
// Each worker embodies a specific analysis philosophy and discovers actionable
// issues during initial codebase bootstrap.
//
// Discovery workers are similar to health monitors but run once during initialization
// rather than on a schedule. They share the CodebaseContext for efficiency.
//
// ZFC Compliance: Workers collect facts and patterns. AI makes judgments.
type DiscoveryWorker interface {
	// Name returns the unique identifier for this worker.
	// Example: "architecture", "bugs", "documentation"
	Name() string

	// Philosophy returns the guiding principle for this worker.
	// Example: "Well-designed systems have clear architectural boundaries"
	// NOT: "Classes should be under 500 lines"
	Philosophy() string

	// Scope returns what this worker analyzes.
	// Example: "File organization, package structure, dependency graph"
	Scope() string

	// Cost returns an estimate of resource usage.
	// Used for budget enforcement and preset configuration.
	Cost() health.CostEstimate

	// Dependencies returns other workers that should run first.
	// Example: "bugs" worker might depend on "architecture" worker
	// to understand the codebase structure before analyzing bugs.
	Dependencies() []string

	// Analyze examines the codebase and returns discovered issues.
	// The worker collects facts; AI supervision interprets them.
	Analyze(ctx context.Context, codebase health.CodebaseContext) (*WorkerResult, error)
}

// WorkerResult contains issues discovered by a worker,
// along with context and reasoning for AI supervision.
type WorkerResult struct {
	// Issues discovered during analysis
	IssuesDiscovered []DiscoveredIssue

	// Context provided to AI for assessment
	// (what was examined, what patterns were found)
	Context string

	// Reasoning for why these are potential problems
	// (citing the worker's philosophy)
	Reasoning string

	// Timestamp of the analysis
	AnalyzedAt time.Time

	// Statistics from the analysis
	Stats AnalysisStats
}

// DiscoveredIssue represents a potential issue found during codebase analysis.
// This is converted to a Beads issue after AI assessment and deduplication.
type DiscoveredIssue struct {
	// Issue classification
	Title       string
	Description string
	Category    string   // e.g., "architecture", "bugs", "documentation"
	Type        string   // "bug", "task", "epic"
	Priority    int      // 0-4 (P0-P4)
	Tags        []string // Additional metadata

	// Location in codebase (optional, for file-specific issues)
	FilePath  string
	LineStart int
	LineEnd   int

	// Supporting evidence
	// This is provided to AI for context during assessment
	Evidence map[string]interface{}

	// Worker metadata
	DiscoveredBy string    // Worker name
	DiscoveredAt time.Time // When it was found

	// Confidence score (0.0-1.0)
	// Higher confidence issues are more likely to be real problems
	Confidence float64
}

// AnalysisStats tracks statistics from a worker's analysis.
type AnalysisStats struct {
	FilesAnalyzed   int
	IssuesFound     int
	Duration        time.Duration
	AICallsMade     int
	TokensUsed      int
	EstimatedCost   float64 // In USD
	ErrorsIgnored   int
	PatternsFound   int
}

// Budget defines resource constraints for discovery.
// Discovery stops when any limit is reached.
type Budget struct {
	// Maximum total cost in USD
	MaxCost float64

	// Maximum total duration
	MaxDuration time.Duration

	// Maximum total AI API calls
	MaxAICalls int

	// Maximum total issues to file
	// (prevents overwhelming the issue tracker)
	MaxIssues int

	// Per-worker overrides
	WorkerBudgets map[string]WorkerBudget
}

// WorkerBudget defines resource constraints for a specific worker.
type WorkerBudget struct {
	MaxCost     float64
	MaxDuration time.Duration
	MaxAICalls  int
	MaxIssues   int
}

// Preset defines a predefined discovery configuration.
type Preset string

const (
	// PresetQuick runs fast, cheap workers only
	// Target: < $0.50, < 1 minute
	PresetQuick Preset = "quick"

	// PresetStandard runs core workers with moderate budgets
	// Target: < $2.00, < 5 minutes
	PresetStandard Preset = "standard"

	// PresetThorough runs all workers with generous budgets
	// Target: < $10.00, < 15 minutes
	PresetThorough Preset = "thorough"

	// PresetCustom uses user-provided configuration
	PresetCustom Preset = "custom"
)

// Config defines the full discovery configuration.
type Config struct {
	// Preset to use (quick/standard/thorough/custom)
	Preset Preset

	// Overall budget
	Budget Budget

	// Workers to run (if empty, uses preset defaults)
	Workers []string

	// Paths to include (glob patterns)
	// Default: all code files
	IncludePaths []string

	// Paths to exclude (glob patterns)
	// Default: vendor/, node_modules/, .git/
	ExcludePaths []string

	// Custom worker configurations
	WorkerConfigs map[string]interface{}

	// Deduplication settings
	DeduplicationEnabled bool
	DeduplicationWindow  time.Duration // How far back to check for duplicates

	// AI assessment settings
	AssessmentEnabled bool
	AssessmentModel   string // AI model for assessment

	// Issue filing settings
	AutoFileIssues  bool     // Automatically create Beads issues
	DefaultPriority int      // Default priority for discovered issues
	DefaultLabels   []string // Labels to add to all discovered issues
}

// DefaultConfig returns the default discovery configuration.
func DefaultConfig() *Config {
	return &Config{
		Preset: PresetStandard,
		Budget: Budget{
			MaxCost:     2.00,
			MaxDuration: 5 * time.Minute,
			MaxAICalls:  100,
			MaxIssues:   50,
		},
		ExcludePaths: []string{
			"vendor/",
			"node_modules/",
			".git/",
			"*.pb.go",        // Generated protobuf code
			"*_generated.go", // Generated code
		},
		DeduplicationEnabled: true,
		DeduplicationWindow:  7 * 24 * time.Hour, // 7 days
		AssessmentEnabled:    true,
		AssessmentModel:      "claude-sonnet-4-5-20250929",
		AutoFileIssues:       true,
		DefaultPriority:      2,
		DefaultLabels:        []string{"discovered:discovery"},
	}
}

// PresetConfig returns the configuration for a given preset.
func PresetConfig(preset Preset) *Config {
	cfg := DefaultConfig()
	cfg.Preset = preset

	switch preset {
	case PresetQuick:
		cfg.Budget = Budget{
			MaxCost:     0.50,
			MaxDuration: 1 * time.Minute,
			MaxAICalls:  20,
			MaxIssues:   20,
		}
		cfg.Workers = []string{
			"file_size_monitor",    // Quick scan for oversized files
			"cruft_detector",       // Quick scan for obvious cruft
			"dependency_auditor",   // Check dependencies (cheap - uses APIs)
		}

	case PresetStandard:
		cfg.Budget = Budget{
			MaxCost:     2.00,
			MaxDuration: 5 * time.Minute,
			MaxAICalls:  100,
			MaxIssues:   50,
		}
		cfg.Workers = []string{
			"file_size_monitor",    // Oversized files
			"cruft_detector",       // Cruft detection
			"duplication_detector", // Code duplication
			"architecture",         // Package structure analysis
			"doc_auditor",          // Documentation quality
			"dependency_auditor",   // Dependency analysis
		}

	case PresetThorough:
		cfg.Budget = Budget{
			MaxCost:     10.00,
			MaxDuration: 15 * time.Minute,
			MaxAICalls:  500,
			MaxIssues:   100,
		}
		cfg.Workers = []string{
			"file_size_monitor",       // Oversized files
			"cruft_detector",          // Cruft detection
			"duplication_detector",    // Code duplication
			"zfc_detector",            // ZFC violations
			"architecture",            // Package structure analysis
			"bugs",                    // Bug pattern detection
			"doc_auditor",             // Documentation quality
			"test_coverage_analyzer",  // Test coverage analysis
			"dependency_auditor",      // Dependency analysis
			"security_scanner",        // Security vulnerabilities
		}
	}

	return cfg
}

// WorkerAdapter adapts a HealthMonitor to a DiscoveryWorker.
// This allows reusing existing health monitors during discovery.
type WorkerAdapter struct {
	monitor health.HealthMonitor
}

// NewWorkerAdapter creates a discovery worker from a health monitor.
func NewWorkerAdapter(monitor health.HealthMonitor) DiscoveryWorker {
	return &WorkerAdapter{monitor: monitor}
}

// Name implements DiscoveryWorker.
func (w *WorkerAdapter) Name() string {
	return w.monitor.Name()
}

// Philosophy implements DiscoveryWorker.
func (w *WorkerAdapter) Philosophy() string {
	return w.monitor.Philosophy()
}

// Scope implements DiscoveryWorker.
func (w *WorkerAdapter) Scope() string {
	return "Adapted from health monitor: " + w.monitor.Name()
}

// Cost implements DiscoveryWorker.
func (w *WorkerAdapter) Cost() health.CostEstimate {
	return w.monitor.Cost()
}

// Dependencies implements DiscoveryWorker.
func (w *WorkerAdapter) Dependencies() []string {
	// Health monitors don't have dependencies
	return nil
}

// Analyze implements DiscoveryWorker.
func (w *WorkerAdapter) Analyze(ctx context.Context, codebase health.CodebaseContext) (*WorkerResult, error) {
	result, err := w.monitor.Check(ctx, codebase)
	if err != nil {
		return nil, err
	}

	// Convert MonitorResult to WorkerResult
	workerResult := &WorkerResult{
		IssuesDiscovered: make([]DiscoveredIssue, len(result.IssuesFound)),
		Context:          result.Context,
		Reasoning:        result.Reasoning,
		AnalyzedAt:       result.CheckedAt,
		Stats: AnalysisStats{
			FilesAnalyzed: result.Stats.FilesScanned,
			IssuesFound:   result.Stats.IssuesFound,
			Duration:      result.Stats.Duration,
			AICallsMade:   result.Stats.AICallsMade,
			ErrorsIgnored: result.Stats.ErrorsIgnored,
		},
	}

	// Convert DiscoveredIssues
	for i, issue := range result.IssuesFound {
		workerResult.IssuesDiscovered[i] = DiscoveredIssue{
			Title:        issue.Description,
			Description:  issue.Description,
			Category:     issue.Category,
			Type:         "task", // Default to task
			Priority:     severityToPriority(issue.Severity),
			FilePath:     issue.FilePath,
			LineStart:    issue.LineStart,
			LineEnd:      issue.LineEnd,
			Evidence:     issue.Evidence,
			DiscoveredBy: w.monitor.Name(),
			DiscoveredAt: result.CheckedAt,
			Confidence:   0.7, // Default confidence for health monitor issues
		}
	}

	return workerResult, nil
}

// severityToPriority converts a severity string to a priority number.
func severityToPriority(severity string) int {
	switch severity {
	case "high":
		return 1 // P1
	case "medium":
		return 2 // P2
	case "low":
		return 3 // P3
	default:
		return 2 // P2 default
	}
}
