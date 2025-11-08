package health

import (
	"context"
	"time"
)

// HealthMonitor defines the interface for code health monitors.
// Each monitor embodies a timeless software engineering principle
// and detects issues by examining the codebase context.
//
// ZFC Compliance: Monitors collect facts and patterns. AI makes judgments.
type HealthMonitor interface {
	// Name returns the unique identifier for this monitor.
	Name() string

	// Philosophy returns a timeless principle (not rules or thresholds).
	// Example: "Code should be easy to understand and modify"
	// NOT: "Functions should be under 50 lines"
	Philosophy() string

	// Check examines the codebase and returns discovered issues.
	// The monitor collects facts; AI supervision interprets them.
	Check(ctx context.Context, codebase CodebaseContext) (*MonitorResult, error)

	// Schedule returns when/how often this monitor should run.
	Schedule() ScheduleConfig

	// Cost returns an estimate of resource usage (time, API calls, etc.)
	Cost() CostEstimate
}

// CodebaseContext provides statistical distributions and patterns,
// not absolute thresholds. This enables ZFC-compliant health monitoring.
type CodebaseContext struct {
	// Root directory of the codebase
	RootPath string

	// Statistical distributions for outlier detection
	FileSizeDistribution  Distribution
	ComplexityDistribution Distribution
	DuplicationPercentage float64

	// Discovered patterns and conventions
	NamingConventions     []Pattern
	ArchitecturalPatterns []Pattern

	// Growth trends
	GrowthRate    float64      // Files added per week
	RecentChanges []FileChange // Last N commits

	// Metadata
	LanguageBreakdown map[string]int // File count per language
	TotalFiles        int
	TotalLines        int
}

// MonitorResult contains issues discovered by a health monitor,
// along with context and reasoning for AI supervision.
type MonitorResult struct {
	// Issues discovered during this check
	IssuesFound []DiscoveredIssue

	// Context for AI prompt (what was examined, what patterns were found)
	Context string

	// Reasoning for why these are potential problems
	// (citing the monitor's philosophy)
	Reasoning string

	// Timestamp of the check
	CheckedAt time.Time

	// Statistics from the check
	Stats CheckStats
}

// DiscoveredIssue represents a potential code health problem.
type DiscoveredIssue struct {
	// Location in codebase
	FilePath string
	LineStart int
	LineEnd   int

	// Issue classification
	Category string // e.g., "cruft", "size", "complexity", "duplication"
	Severity string // "low", "medium", "high" (based on statistical outliers)

	// Description for AI context
	Description string

	// Supporting data (metrics, snippets, etc.)
	Evidence map[string]interface{}
}

// ScheduleConfig defines when a monitor should run.
type ScheduleConfig struct {
	Type ScheduleType

	// For time-based schedules
	Interval time.Duration

	// For event-based schedules
	EventTrigger string // e.g., "every_10_issues", "after_git_push"

	// For hybrid schedules
	MinInterval time.Duration // Minimum time between runs
	MaxInterval time.Duration // Maximum time between runs
}

// ScheduleType determines when a monitor runs.
type ScheduleType string

const (
	// ScheduleTimeBased runs at fixed intervals
	ScheduleTimeBased ScheduleType = "time_based"

	// ScheduleEventBased runs in response to events
	ScheduleEventBased ScheduleType = "event_based"

	// ScheduleHybrid combines time and event triggers
	ScheduleHybrid ScheduleType = "hybrid"

	// ScheduleManual only runs when explicitly requested
	ScheduleManual ScheduleType = "manual"
)

// CostEstimate predicts resource usage for a monitor check.
type CostEstimate struct {
	// Expected execution time
	EstimatedDuration time.Duration

	// AI API calls (for AI-powered monitors)
	AICallsEstimated int

	// Whether this requires full codebase scan
	RequiresFullScan bool

	// Relative cost category
	Category CostCategory
}

// CostCategory classifies the relative expense of a monitor.
type CostCategory string

const (
	CostCheap     CostCategory = "cheap"      // < 1 second, no AI
	CostModerate  CostCategory = "moderate"   // 1-10 seconds, minimal AI
	CostExpensive CostCategory = "expensive"  // > 10 seconds or heavy AI usage
)

// Distribution represents a statistical distribution of values.
// Used for outlier detection (N standard deviations from mean).
type Distribution struct {
	Mean   float64
	Median float64
	StdDev float64
	P95    float64 // 95th percentile
	P99    float64 // 99th percentile
	Min    float64
	Max    float64
	Count  int
}

// IsUpperOutlier returns true if the value is N standard deviations above the mean.
// This only detects upper tail outliers (values significantly larger than average).
// For lower tail detection, check if value < mean - (numStdDevs * stdDev).
func (d Distribution) IsUpperOutlier(value float64, numStdDevs float64) bool {
	if d.StdDev == 0 {
		return false
	}
	return value > d.Mean + (numStdDevs * d.StdDev)
}

// Pattern represents a discovered naming or architectural convention.
type Pattern struct {
	Name        string  // e.g., "snake_case", "MVC architecture"
	Confidence  float64 // 0.0 to 1.0
	Examples    []string
	Prevalence  float64 // Percentage of codebase following this pattern
}

// FileChange represents a recent modification to the codebase.
type FileChange struct {
	FilePath  string
	ChangeType string // "added", "modified", "deleted"
	LinesAdded int
	LinesRemoved int
	Timestamp time.Time
	CommitHash string
}

// CheckStats tracks statistics from a monitor check.
type CheckStats struct {
	FilesScanned   int
	IssuesFound    int
	Duration       time.Duration
	AICallsMade    int
	ErrorsIgnored  int
}
