package codereview

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/steveyegge/vc/internal/storage/beads"
	"github.com/steveyegge/vc/internal/types"
)

// Sweeper manages activity-based code review sweeps
type Sweeper struct {
	store *beads.VCStorage
}

// NewSweeper creates a new code review sweeper
func NewSweeper(store *beads.VCStorage) *Sweeper {
	return &Sweeper{
		store: store,
	}
}

// GetDiffMetrics gathers git diff statistics since the last review checkpoint
// Returns metrics for AI decision-making about whether to trigger a review
// Also returns the actual commit SHA used for calculations to prevent race conditions
func (s *Sweeper) GetDiffMetrics(ctx context.Context) (*types.ReviewMetricsResult, error) {
	// Get last checkpoint
	checkpoint, err := s.store.GetLastReviewCheckpoint(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get last review checkpoint: %w", err)
	}

	// If no checkpoint yet, use initial commit or default
	var lastCommitSHA string
	if checkpoint == nil {
		// Find first commit as baseline
		cmd := exec.Command("git", "rev-list", "--max-parents=0", "HEAD")
		output, err := cmd.Output()
		if err != nil {
			return nil, fmt.Errorf("failed to get initial commit: %w", err)
		}
		lastCommitSHA = strings.TrimSpace(string(output))
	} else {
		lastCommitSHA = checkpoint.CommitSHA
	}

	// Get current HEAD commit (ACTUAL SHA, not symbolic ref "HEAD")
	cmd := exec.Command("git", "rev-parse", "HEAD")
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to get current commit: %w", err)
	}
	currentCommitSHA := strings.TrimSpace(string(output))

	// If no changes since last checkpoint, return zero metrics with current SHA
	if lastCommitSHA == currentCommitSHA {
		return &types.ReviewMetricsResult{
			Metrics: &types.ReviewDecisionRequest{
				LinesAdded:      0,
				LinesDeleted:    0,
				FilesChanged:    0,
				HeavyChurnAreas: []string{},
				DaysSinceReview: 0,
				TotalLOC:        0,
			},
			CommitSHA: currentCommitSHA, // Return actual SHA
		}, nil
	}

	// Get diff stats using git diff --shortstat
	cmd = exec.Command("git", "diff", "--shortstat", lastCommitSHA+"..HEAD")
	output, err = cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to get diff stats: %w", err)
	}

	// Parse shortstat output: "N files changed, N insertions(+), N deletions(-)"
	statsLine := strings.TrimSpace(string(output))
	linesAdded, linesDeleted, filesChanged := parseDiffStats(statsLine)

	// Get heavy churn areas using git diff --stat
	cmd = exec.Command("git", "diff", "--stat", lastCommitSHA+"..HEAD")
	output, err = cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to get diff stat: %w", err)
	}

	heavyChurnAreas := identifyHeavyChurnAreas(string(output))

	// Calculate days since last review
	daysSinceReview := 0
	if checkpoint != nil {
		daysSinceReview = int(time.Since(checkpoint.Timestamp).Hours() / 24)
	}

	// Get total LOC (approximate using git ls-files and wc -l)
	totalLOC := getTotalLOC()

	// Get last review summary (if available)
	lastReviewSummary := ""
	// TODO: Retrieve summary from last review issue when implemented

	return &types.ReviewMetricsResult{
		Metrics: &types.ReviewDecisionRequest{
			LinesAdded:        linesAdded,
			LinesDeleted:      linesDeleted,
			FilesChanged:      filesChanged,
			HeavyChurnAreas:   heavyChurnAreas,
			DaysSinceReview:   daysSinceReview,
			TotalLOC:          totalLOC,
			LastReviewSummary: lastReviewSummary,
		},
		CommitSHA: currentCommitSHA, // Return actual SHA used for diff calculation
	}, nil
}

// parseDiffStats parses git diff --shortstat output
// Example: "15 files changed, 423 insertions(+), 187 deletions(-)"
func parseDiffStats(statsLine string) (linesAdded, linesDeleted, filesChanged int) {
	if statsLine == "" {
		return 0, 0, 0
	}

	parts := strings.Split(statsLine, ",")
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if strings.Contains(part, "file") {
			// Extract number of files changed
			fields := strings.Fields(part)
			if len(fields) > 0 {
				if n, err := strconv.Atoi(fields[0]); err == nil {
					filesChanged = n
				}
			}
		} else if strings.Contains(part, "insertion") {
			// Extract insertions
			fields := strings.Fields(part)
			if len(fields) > 0 {
				if n, err := strconv.Atoi(fields[0]); err == nil {
					linesAdded = n
				}
			}
		} else if strings.Contains(part, "deletion") {
			// Extract deletions
			fields := strings.Fields(part)
			if len(fields) > 0 {
				if n, err := strconv.Atoi(fields[0]); err == nil {
					linesDeleted = n
				}
			}
		}
	}

	return linesAdded, linesDeleted, filesChanged
}

// identifyHeavyChurnAreas identifies directories with most changes
// Parses git diff --stat output to find high-churn areas
func identifyHeavyChurnAreas(statOutput string) []string {
	// Parse --stat output and identify directories with most changes
	// Example line: " internal/executor/executor.go | 45 ++++++++++++++++++++++"

	dirChanges := make(map[string]int)
	lines := strings.Split(statOutput, "\n")

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || !strings.Contains(line, "|") {
			continue
		}

		parts := strings.Split(line, "|")
		if len(parts) < 2 {
			continue
		}

		filePath := strings.TrimSpace(parts[0])
		changeLine := strings.TrimSpace(parts[1])

		// Extract directory from file path
		lastSlash := strings.LastIndex(filePath, "/")
		var dir string
		if lastSlash == -1 {
			dir = "." // Root directory
		} else {
			dir = filePath[:lastSlash]
		}

		// Count changes (approximate based on +/- symbols)
		changeCount := strings.Count(changeLine, "+") + strings.Count(changeLine, "-")
		dirChanges[dir] += changeCount
	}

	// Find directories with most changes (top 5)
	type dirChange struct {
		dir   string
		count int
	}

	var sorted []dirChange
	for dir, count := range dirChanges {
		sorted = append(sorted, dirChange{dir, count})
	}

	// Simple bubble sort (good enough for small lists)
	for i := 0; i < len(sorted); i++ {
		for j := i + 1; j < len(sorted); j++ {
			if sorted[j].count > sorted[i].count {
				sorted[i], sorted[j] = sorted[j], sorted[i]
			}
		}
	}

	// Return top 5 directories
	var result []string
	limit := 5
	if len(sorted) < limit {
		limit = len(sorted)
	}

	for i := 0; i < limit; i++ {
		result = append(result, sorted[i].dir)
	}

	return result
}

// getTotalLOC gets approximate total lines of code in the repository
func getTotalLOC() int {
	// Use git ls-files to get all tracked files, then count lines
	// Exclude common non-code files
	cmd := exec.Command("sh", "-c",
		`git ls-files | grep -E '\.(go|js|ts|py|java|c|cpp|h|hpp)$' | xargs wc -l 2>/dev/null | tail -1 | awk '{print $1}'`)
	output, err := cmd.Output()
	if err != nil {
		// If command fails, return 0 (not critical)
		return 0
	}

	totalStr := strings.TrimSpace(string(output))
	total, err := strconv.Atoi(totalStr)
	if err != nil {
		return 0
	}

	return total
}

// CreateReviewIssue creates a code review sweep issue based on AI decision
// This issue will be claimed and executed by the executor like any other task
func (s *Sweeper) CreateReviewIssue(ctx context.Context, decision *types.ReviewDecision) (string, error) {
	// Build issue title and description
	title := fmt.Sprintf("Code Review Sweep: %s", decision.Scope)

	description := fmt.Sprintf(`Perform %s code review sweep based on accumulated activity.

**AI Reasoning:**
%s

**Scope:** %s
**Target Areas:** %s
**Estimated Files:** %d
**Estimated Cost:** %s

**Task:**
Review files for non-obvious issues that agents miss during focused work:
- Inefficiencies (algorithmic, resource usage)
- Subtle bugs (race conditions, off-by-one, copy-paste)
- Poor patterns (coupling, complexity, duplication)
- Missing best practices (error handling, docs, tests)
- Unnamed anti-patterns

File discovered issues with detailed reasoning and suggestions.`,
		decision.Scope,
		decision.Reasoning,
		decision.Scope,
		formatTargetAreas(decision.TargetAreas),
		decision.EstimatedFiles,
		decision.EstimatedCost,
	)

	// Determine priority based on scope
	priority := 2 // Default P2
	if decision.Scope == "thorough" {
		priority = 1 // P1 for thorough reviews
	}

	// Create the issue using Beads
	issue := &types.Issue{
		Title:       title,
		Description: description,
		IssueType:   types.TypeTask,
		Priority:    priority,
		Status:      types.StatusOpen,
	}

	// Create the issue
	actor := "code-review-sweeper"
	if err := s.store.CreateIssue(ctx, issue, actor); err != nil {
		return "", fmt.Errorf("failed to create review issue: %w", err)
	}

	// Add 'code-review-sweep' label
	if err := s.store.AddLabel(ctx, issue.ID, "code-review-sweep", actor); err != nil {
		return "", fmt.Errorf("failed to add code-review-sweep label: %w", err)
	}

	// Add target area labels if specified
	if decision.TargetAreas != nil && len(decision.TargetAreas) > 0 {
		for _, area := range decision.TargetAreas {
			label := fmt.Sprintf("review-area:%s", area)
			if err := s.store.AddLabel(ctx, issue.ID, label, actor); err != nil {
				// Log warning but continue
				fmt.Fprintf(os.Stderr, "warning: failed to add review-area label: %v\n", err)
			}
		}
	}

	return issue.ID, nil
}

// formatTargetAreas formats target areas for display
func formatTargetAreas(areas []string) string {
	if areas == nil || len(areas) == 0 {
		return "Broad sampling across entire codebase"
	}
	return strings.Join(areas, ", ")
}
