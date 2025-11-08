package examples

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/steveyegge/vc/internal/discovery"
	"github.com/steveyegge/vc/internal/discovery/sdk"
	"github.com/steveyegge/vc/internal/health"
)

// TODOTrackerWorker finds and tracks TODO/FIXME/HACK comments in code.
// This is an example custom worker showing simple pattern matching.
//
// Philosophy: "Technical debt should be visible and tracked, not hidden in comments"
//
// Example usage:
//
//	worker := examples.NewTODOTrackerWorker()
//	result, err := worker.Analyze(ctx, codebase)
type TODOTrackerWorker struct {
	// Patterns to search for (TODO, FIXME, HACK, XXX, etc.)
	patterns []string
}

// NewTODOTrackerWorker creates a TODO tracker worker.
func NewTODOTrackerWorker() *TODOTrackerWorker {
	return &TODOTrackerWorker{
		patterns: []string{"TODO", "FIXME", "HACK", "XXX", "BUG", "DEPRECATED"},
	}
}

// Name implements DiscoveryWorker.
func (w *TODOTrackerWorker) Name() string {
	return "todo_tracker"
}

// Philosophy implements DiscoveryWorker.
func (w *TODOTrackerWorker) Philosophy() string {
	return "Technical debt should be visible and tracked, not hidden in comments"
}

// Scope implements DiscoveryWorker.
func (w *TODOTrackerWorker) Scope() string {
	return "TODO, FIXME, HACK, XXX, and other technical debt markers in code comments"
}

// Cost implements DiscoveryWorker.
func (w *TODOTrackerWorker) Cost() health.CostEstimate {
	return health.CostEstimate{
		EstimatedDuration: 20 * time.Second,
		AICallsEstimated:  0, // Pure regex matching
		RequiresFullScan:  true,
		Category:          health.CostCheap,
	}
}

// Dependencies implements DiscoveryWorker.
func (w *TODOTrackerWorker) Dependencies() []string {
	return nil
}

// Analyze implements DiscoveryWorker.
func (w *TODOTrackerWorker) Analyze(ctx context.Context, codebase health.CodebaseContext) (*discovery.WorkerResult, error) {
	result := sdk.NewWorkerResultBuilder(w.Name()).
		WithContext(fmt.Sprintf("Searching for technical debt markers in %s", codebase.RootPath)).
		WithReasoning(fmt.Sprintf("Based on philosophy: '%s'\n\nTODO comments indicate planned work that hasn't been done. Converting them to tracked issues ensures they don't get forgotten.", w.Philosophy()))

	// Track TODOs by type
	todosByType := make(map[string]int)

	// Search for each pattern
	for _, pattern := range w.patterns {
		matches, err := sdk.FindPattern(codebase.RootPath,
			fmt.Sprintf(`//\s*%s:?\s*(.*)`, pattern),
			sdk.PatternOptions{
				FilePattern: "*.go",
				ExcludeDirs: []string{"vendor", ".git", "node_modules"},
			})

		if err != nil {
			continue // Skip patterns that fail
		}

		for _, match := range matches {
			// Extract TODO text
			todoText := w.extractTODOText(match.Context, pattern)

			// Determine priority based on pattern type
			priority := w.getPriorityForPattern(pattern)

			// Create issue
			result.AddIssue(sdk.NewIssue().
				WithTitle(fmt.Sprintf("%s: %s", pattern, w.summarizeTODO(todoText))).
				WithDescription(fmt.Sprintf("Technical debt marker found at %s:%d\n\n%s: %s\n\nConsider creating a proper issue to track this work.", match.File, match.Line, pattern, todoText)).
				WithCategory("technical-debt").
				WithFile(match.File, match.Line).
				WithPriority(priority).
				WithTag("todo").
				WithTag(strings.ToLower(pattern)).
				WithEvidence("pattern", pattern).
				WithEvidence("text", todoText).
				WithConfidence(1.0). // High confidence - exact match
				Build())

			todosByType[pattern]++
			result.IncrementPatternsFound()
		}
	}

	// Add summary statistics to context
	summaryParts := []string{}
	for pattern, count := range todosByType {
		summaryParts = append(summaryParts, fmt.Sprintf("%d %s", count, pattern))
	}

	if len(summaryParts) > 0 {
		result.WithContext(fmt.Sprintf("Found technical debt markers: %s", strings.Join(summaryParts, ", ")))
	}

	return result.Build(), nil
}

// extractTODOText extracts the actual TODO text from a comment line.
func (w *TODOTrackerWorker) extractTODOText(line string, pattern string) string {
	// Remove comment markers and pattern
	re := regexp.MustCompile(fmt.Sprintf(`//\s*%s:?\s*(.*)`, pattern))
	matches := re.FindStringSubmatch(line)

	if len(matches) > 1 {
		return strings.TrimSpace(matches[1])
	}

	return line
}

// summarizeTODO creates a short summary from TODO text.
func (w *TODOTrackerWorker) summarizeTODO(text string) string {
	// Limit to 50 characters
	if len(text) > 50 {
		return text[:47] + "..."
	}
	return text
}

// getPriorityForPattern determines issue priority based on pattern type.
func (w *TODOTrackerWorker) getPriorityForPattern(pattern string) int {
	switch pattern {
	case "BUG", "FIXME":
		return 1 // P1 - bugs should be fixed soon
	case "HACK", "XXX":
		return 2 // P2 - technical debt to address
	case "TODO":
		return 3 // P3 - planned work
	case "DEPRECATED":
		return 2 // P2 - should be removed
	default:
		return 3 // P3 default
	}
}
