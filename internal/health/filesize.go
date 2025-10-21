package health

import (
	"bufio"
	"context"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/steveyegge/vc/internal/ai"
)

// FileSizeMonitor detects oversized files using statistical analysis
// and AI judgment rather than hardcoded thresholds.
//
// ZFC Compliance: Collects file size distribution, identifies outliers,
// then delegates judgment to AI supervisor.
type FileSizeMonitor struct {
	// RootPath is the codebase root directory
	RootPath string

	// OutlierThreshold is number of standard deviations for outlier detection
	// Default: 2.5 (files >2.5σ from mean are candidates for review)
	OutlierThreshold float64

	// FileExtensions to scan (default: [".go"])
	FileExtensions []string

	// ExcludePatterns for files/directories to skip
	ExcludePatterns []string

	// AI supervisor for evaluating outliers (interface for easier testing)
	Supervisor AISupervisor
}

// AISupervisor defines the interface for AI evaluation.
type AISupervisor interface {
	CallAI(ctx context.Context, prompt string, operation string, model string, maxTokens int) (string, error)
}

// NewFileSizeMonitor creates a file size monitor with sensible defaults.
// Returns an error if the rootPath is invalid or cannot be resolved to an absolute path.
func NewFileSizeMonitor(rootPath string, supervisor AISupervisor) (*FileSizeMonitor, error) {
	// Validate and clean the root path to prevent path traversal vulnerabilities
	absPath, err := filepath.Abs(rootPath)
	if err != nil {
		return nil, fmt.Errorf("invalid root path %q: %w", rootPath, err)
	}

	return &FileSizeMonitor{
		RootPath:         absPath,
		OutlierThreshold: 2.5,
		FileExtensions:   []string{".go"},
		ExcludePatterns: []string{
			"vendor/",
			".git/",
			"_test.go",
			".pb.go",  // Generated protobuf
			".gen.go", // Other generated code
			"testdata/",
		},
		Supervisor: supervisor,
	}, nil
}

// Name implements HealthMonitor.
func (m *FileSizeMonitor) Name() string {
	return "file_size_monitor"
}

// Philosophy implements HealthMonitor.
func (m *FileSizeMonitor) Philosophy() string {
	return "Files should be focused on a single responsibility. " +
		"Oversized files often indicate missing abstractions or unclear boundaries."
}

// Schedule implements HealthMonitor.
func (m *FileSizeMonitor) Schedule() ScheduleConfig {
	return ScheduleConfig{
		Type:     ScheduleTimeBased,
		Interval: 24 * time.Hour, // Daily
	}
}

// Cost implements HealthMonitor.
func (m *FileSizeMonitor) Cost() CostEstimate {
	return CostEstimate{
		EstimatedDuration: 5 * time.Second,
		AICallsEstimated:  1, // One call to evaluate all outliers
		RequiresFullScan:  true,
		Category:          CostModerate,
	}
}

// Check implements HealthMonitor.
// Note: The codebase parameter is currently unused. This monitor performs its own
// file scanning and statistical analysis independent of pre-computed codebase context.
// Future optimization: Consider using codebase.FileSizeDistribution if available
// to avoid redundant scanning.
func (m *FileSizeMonitor) Check(ctx context.Context, codebase CodebaseContext) (*MonitorResult, error) {
	// Validate that AI supervisor is configured
	if m.Supervisor == nil {
		return nil, fmt.Errorf("AI supervisor is required for file size monitoring")
	}

	startTime := time.Now()

	// 1. Scan codebase and gather file size statistics
	fileSizes, err := m.scanFiles(ctx)
	if err != nil {
		return nil, fmt.Errorf("scanning files: %w", err)
	}

	if len(fileSizes) == 0 {
		return &MonitorResult{
			IssuesFound: []DiscoveredIssue{},
			Context:     "No files found matching criteria",
			CheckedAt:   startTime,
			Stats: CheckStats{
				FilesScanned: 0,
				Duration:     time.Since(startTime),
			},
		}, nil
	}

	// 2. Calculate statistical distribution
	dist := m.calculateDistribution(fileSizes)

	// 3. Identify outliers
	outliers := m.findOutliers(fileSizes, dist)

	if len(outliers) == 0 {
		return &MonitorResult{
			IssuesFound: []DiscoveredIssue{},
			Context:     fmt.Sprintf("Scanned %d files, no outliers found", len(fileSizes)),
			CheckedAt:   startTime,
			Stats: CheckStats{
				FilesScanned: len(fileSizes),
				Duration:     time.Since(startTime),
			},
		}, nil
	}

	// Limit outliers sent to AI to prevent token limit issues and timeouts
	const maxOutliersForAI = 50
	outliersForAI := outliers
	if len(outliers) > maxOutliersForAI {
		outliersForAI = outliers[:maxOutliersForAI]
		// Outliers are already sorted by size descending, so we get the largest ones
	}

	// 4. Build AI prompt and get evaluation
	evaluation, err := m.evaluateOutliers(ctx, outliersForAI, dist)
	if err != nil {
		return nil, fmt.Errorf("evaluating outliers: %w", err)
	}

	// 5. Convert AI evaluation to DiscoveredIssues
	issues := m.buildIssues(evaluation, dist)

	return &MonitorResult{
		IssuesFound: issues,
		Context:     m.buildContext(fileSizes, dist, outliers),
		Reasoning:   m.buildReasoning(evaluation),
		CheckedAt:   startTime,
		Stats: CheckStats{
			FilesScanned: len(fileSizes),
			IssuesFound:  len(issues),
			Duration:     time.Since(startTime),
			AICallsMade:  1,
		},
	}, nil
}

// fileSize represents a file and its line count.
type fileSize struct {
	Path  string
	Lines int
}

// scanFiles walks the directory tree and counts lines in matching files.
func (m *FileSizeMonitor) scanFiles(ctx context.Context) ([]fileSize, error) {
	var sizes []fileSize

	err := filepath.Walk(m.RootPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Check context cancellation
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		// Skip excluded patterns
		relPath, err := filepath.Rel(m.RootPath, path)
		if err != nil {
			// Skip files where relative path cannot be determined
			// This should be rare but prevents silent failures
			return nil
		}

		if ShouldExcludePath(relPath, info, m.ExcludePatterns) {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		// Only process files with matching extensions
		if info.IsDir() {
			return nil
		}

		hasExt := false
		for _, ext := range m.FileExtensions {
			if strings.HasSuffix(path, ext) {
				hasExt = true
				break
			}
		}
		if !hasExt {
			return nil
		}

		// Count lines
		lines, err := countLines(path)
		if err != nil {
			// Log but don't fail on individual file errors
			return nil
		}

		sizes = append(sizes, fileSize{
			Path:  relPath,
			Lines: lines,
		})

		return nil
	})

	return sizes, err
}

// countLines counts lines in a file using streaming to avoid memory exhaustion.
func countLines(path string) (int, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	count := 0
	for scanner.Scan() {
		count++
	}

	if err := scanner.Err(); err != nil {
		return 0, err
	}

	return count, nil
}

// calculateDistribution computes statistical distribution of file sizes.
func (m *FileSizeMonitor) calculateDistribution(sizes []fileSize) Distribution {
	if len(sizes) == 0 {
		return Distribution{}
	}

	// Extract line counts
	lines := make([]int, len(sizes))
	for i, s := range sizes {
		lines[i] = s.Lines
	}

	// Sort for percentiles and median
	sorted := make([]int, len(lines))
	copy(sorted, lines)
	sort.Ints(sorted)

	// Calculate mean
	sum := 0
	for _, l := range lines {
		sum += l
	}
	mean := float64(sum) / float64(len(lines))

	// Calculate standard deviation
	variance := 0.0
	for _, l := range lines {
		diff := float64(l) - mean
		variance += diff * diff
	}
	stdDev := math.Sqrt(variance / float64(len(lines)))

	// Percentiles with bounds checking for small datasets
	p95Idx := int(float64(len(sorted)) * 0.95)
	if p95Idx >= len(sorted) {
		p95Idx = len(sorted) - 1
	}
	p99Idx := int(float64(len(sorted)) * 0.99)
	if p99Idx >= len(sorted) {
		p99Idx = len(sorted) - 1
	}

	return Distribution{
		Mean:   mean,
		Median: float64(sorted[len(sorted)/2]),
		StdDev: stdDev,
		P95:    float64(sorted[p95Idx]),
		P99:    float64(sorted[p99Idx]),
		Min:    float64(sorted[0]),
		Max:    float64(sorted[len(sorted)-1]),
		Count:  len(sorted),
	}
}

// findOutliers identifies files that are statistical outliers.
func (m *FileSizeMonitor) findOutliers(sizes []fileSize, dist Distribution) []fileSize {
	var outliers []fileSize

	threshold := dist.Mean + (m.OutlierThreshold * dist.StdDev)

	for _, s := range sizes {
		if float64(s.Lines) > threshold {
			outliers = append(outliers, s)
		}
	}

	// Sort by size descending
	sort.Slice(outliers, func(i, j int) bool {
		return outliers[i].Lines > outliers[j].Lines
	})

	return outliers
}

// outlierEvaluation is the AI's assessment of outlier files.
type outlierEvaluation struct {
	ProblematicFiles []problematicFile `json:"problematic_files"`
	JustifiedFiles   []justifiedFile   `json:"justified_files"`
}

type problematicFile struct {
	File           string `json:"file"`
	Lines          int    `json:"lines"`
	Issue          string `json:"issue"`
	SuggestedSplit string `json:"suggested_split"`
}

type justifiedFile struct {
	File          string `json:"file"`
	Lines         int    `json:"lines"`
	Justification string `json:"justification"`
}

// evaluateOutliers uses AI to judge whether outliers are problematic.
func (m *FileSizeMonitor) evaluateOutliers(ctx context.Context, outliers []fileSize, dist Distribution) (*outlierEvaluation, error) {
	prompt := m.buildPrompt(outliers, dist)

	// Call AI supervisor
	// Note: Using model="" and maxTokens=0 means use supervisor's defaults
	response, err := m.Supervisor.CallAI(ctx, prompt, "file_size_evaluation", "", 4096)
	if err != nil {
		return nil, fmt.Errorf("AI call failed: %w", err)
	}

	// Parse JSON response using resilient parser
	parseResult := ai.Parse[outlierEvaluation](response, ai.ParseOptions{
		Context: "file_size_evaluation",
	})

	if !parseResult.Success {
		return nil, fmt.Errorf("parsing AI response: %s", parseResult.Error)
	}

	return &parseResult.Data, nil
}

// buildPrompt creates the AI evaluation prompt.
func (m *FileSizeMonitor) buildPrompt(outliers []fileSize, dist Distribution) string {
	var sb strings.Builder

	sb.WriteString("# File Size Analysis Request\n\n")
	sb.WriteString("## Philosophy\n")
	sb.WriteString(m.Philosophy())
	sb.WriteString("\n\n")

	sb.WriteString("## Codebase Statistics\n")
	sb.WriteString(fmt.Sprintf("- Total files analyzed: %d\n", dist.Count))
	sb.WriteString(fmt.Sprintf("- Mean file size: %.0f lines\n", dist.Mean))
	sb.WriteString(fmt.Sprintf("- Median file size: %.0f lines\n", dist.Median))
	sb.WriteString(fmt.Sprintf("- Std deviation: %.0f lines\n", dist.StdDev))
	sb.WriteString(fmt.Sprintf("- 95th percentile: %.0f lines\n", dist.P95))
	sb.WriteString(fmt.Sprintf("- 99th percentile: %.0f lines\n", dist.P99))
	sb.WriteString("\n")

	sb.WriteString("## Statistical Outliers (>2.5σ from mean)\n")
	for _, o := range outliers {
		sb.WriteString(fmt.Sprintf("- %s: %d lines\n", o.Path, o.Lines))
	}
	sb.WriteString("\n")

	// Use dynamic year to prevent prompt from becoming stale
	year := time.Now().Year()
	sb.WriteString(fmt.Sprintf("## Guidance (%d)\n", year))
	sb.WriteString("Most Go projects: 100-500 lines typical, 1000+ warrants review.\n")
	sb.WriteString("However, judgment should adapt to THIS codebase's patterns.\n\n")

	sb.WriteString("## Your Task\n")
	sb.WriteString("For each outlier file, determine if it's problematic or justified.\n\n")
	sb.WriteString("Problematic if:\n")
	sb.WriteString("- Handles multiple unrelated responsibilities\n")
	sb.WriteString("- Could be split into focused modules\n")
	sb.WriteString("- Violates single responsibility principle\n\n")

	sb.WriteString("Justified if:\n")
	sb.WriteString("- Generated code (protobuf, etc.)\n")
	sb.WriteString("- Legitimate single responsibility (large state machine, etc.)\n")
	sb.WriteString("- Splitting would reduce clarity\n\n")

	sb.WriteString("## Response Format\n")
	sb.WriteString("Return valid JSON:\n")
	sb.WriteString("```json\n")
	sb.WriteString("{\n")
	sb.WriteString("  \"problematic_files\": [\n")
	sb.WriteString("    {\n")
	sb.WriteString("      \"file\": \"path/to/file.go\",\n")
	sb.WriteString("      \"lines\": 1234,\n")
	sb.WriteString("      \"issue\": \"Handles X, Y, and Z responsibilities\",\n")
	sb.WriteString("      \"suggested_split\": \"Split into x.go, y.go, z.go\"\n")
	sb.WriteString("    }\n")
	sb.WriteString("  ],\n")
	sb.WriteString("  \"justified_files\": [\n")
	sb.WriteString("    {\n")
	sb.WriteString("      \"file\": \"path/to/file.go\",\n")
	sb.WriteString("      \"lines\": 1234,\n")
	sb.WriteString("      \"justification\": \"Generated protobuf code\"\n")
	sb.WriteString("    }\n")
	sb.WriteString("  ]\n")
	sb.WriteString("}\n")
	sb.WriteString("```\n")

	return sb.String()
}

// buildIssues converts AI evaluation to DiscoveredIssues.
func (m *FileSizeMonitor) buildIssues(eval *outlierEvaluation, dist Distribution) []DiscoveredIssue {
	var issues []DiscoveredIssue

	for _, pf := range eval.ProblematicFiles {
		issues = append(issues, DiscoveredIssue{
			FilePath:    pf.File,
			Category:    "file_size",
			Severity:    m.calculateSeverity(pf.Lines, dist),
			Description: fmt.Sprintf("%s (%d lines): %s", pf.File, pf.Lines, pf.Issue),
			Evidence: map[string]interface{}{
				"lines":           pf.Lines,
				"mean":            dist.Mean,
				"std_devs_above":  (float64(pf.Lines) - dist.Mean) / dist.StdDev,
				"issue":           pf.Issue,
				"suggested_split": pf.SuggestedSplit,
			},
		})
	}

	return issues
}

// calculateSeverity determines issue severity based on how extreme the outlier is.
func (m *FileSizeMonitor) calculateSeverity(lines int, dist Distribution) string {
	if dist.StdDev == 0 {
		return "medium"
	}

	stdDevsAbove := (float64(lines) - dist.Mean) / dist.StdDev

	if stdDevsAbove > 4.0 {
		return "high"
	} else if stdDevsAbove > 3.0 {
		return "medium"
	}
	return "low"
}

// buildContext creates context string for MonitorResult.
func (m *FileSizeMonitor) buildContext(sizes []fileSize, dist Distribution, outliers []fileSize) string {
	return fmt.Sprintf(
		"Scanned %d files. Found %d statistical outliers (>%.1fσ from mean of %.0f lines). "+
			"Range: %.0f-%.0f lines. P95: %.0f, P99: %.0f.",
		len(sizes), len(outliers), m.OutlierThreshold, dist.Mean,
		dist.Min, dist.Max, dist.P95, dist.P99,
	)
}

// buildReasoning creates reasoning string from AI evaluation.
func (m *FileSizeMonitor) buildReasoning(eval *outlierEvaluation) string {
	return fmt.Sprintf(
		"AI evaluation identified %d problematic files that violate single responsibility principle "+
			"and %d justified outliers (generated code, legitimate size).",
		len(eval.ProblematicFiles), len(eval.JustifiedFiles),
	)
}
