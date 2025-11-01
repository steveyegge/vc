package codereview

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/steveyegge/vc/internal/storage/beads"
	"github.com/steveyegge/vc/internal/types"
)

// commitSHAPattern matches valid git commit SHAs (7-40 hexadecimal characters)
var commitSHAPattern = regexp.MustCompile(`^[0-9a-f]{7,40}$`)

// Cache for getTotalLOC() to avoid expensive pipeline on every call (vc-ab9d)
var (
	cachedLOC      int
	cachedLOCTime  time.Time
	cachedLOCMutex sync.RWMutex
	locCacheTTL    = 1 * time.Hour
)

// isValidCommitSHA validates that a string is a valid git commit SHA
// Valid SHAs are 7-40 hexadecimal characters (lowercase)
// This prevents command injection when using SHAs in git commands
func isValidCommitSHA(sha string) bool {
	return commitSHAPattern.MatchString(sha)
}

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
		// Find first commit as baseline (vc-ecc6: use CommandContext)
		cmd := exec.CommandContext(ctx, "git", "rev-list", "--max-parents=0", "HEAD")
		output, err := cmd.Output()
		if err != nil {
			return nil, fmt.Errorf("failed to get initial commit: %w", err)
		}
		lastCommitSHA = strings.TrimSpace(string(output))

		// Validate initial commit SHA (vc-cfb3)
		if !isValidCommitSHA(lastCommitSHA) {
			return nil, fmt.Errorf("invalid initial commit SHA: %s", lastCommitSHA)
		}
	} else {
		lastCommitSHA = checkpoint.CommitSHA

		// Validate checkpoint commit SHA (vc-cfb3: defense-in-depth)
		if !isValidCommitSHA(lastCommitSHA) {
			return nil, fmt.Errorf("invalid checkpoint commit SHA: %s", lastCommitSHA)
		}
	}

	// Get current HEAD commit (ACTUAL SHA, not symbolic ref "HEAD")
	// vc-ecc6: use CommandContext for cancellation support
	cmd := exec.CommandContext(ctx, "git", "rev-parse", "HEAD")
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to get current commit: %w", err)
	}
	currentCommitSHA := strings.TrimSpace(string(output))

	// Validate current commit SHA (vc-cfb3)
	if !isValidCommitSHA(currentCommitSHA) {
		return nil, fmt.Errorf("invalid current commit SHA: %s", currentCommitSHA)
	}

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

	// Get diff stats using git diff --shortstat (vc-ecc6: use CommandContext)
	cmd = exec.CommandContext(ctx, "git", "diff", "--shortstat", lastCommitSHA+"..HEAD")
	output, err = cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to get diff stats: %w", err)
	}

	// Parse shortstat output: "N files changed, N insertions(+), N deletions(-)"
	statsLine := strings.TrimSpace(string(output))
	linesAdded, linesDeleted, filesChanged := parseDiffStats(statsLine)

	// Get heavy churn areas using git diff --stat (vc-ecc6: use CommandContext)
	cmd = exec.CommandContext(ctx, "git", "diff", "--stat", lastCommitSHA+"..HEAD")
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
	totalLOC := getTotalLOC(ctx)

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
// Uses 1-hour cache to avoid expensive pipeline on every call (vc-ab9d)
// Accepts context for cancellation support (vc-ecc6)
func getTotalLOC(ctx context.Context) int {
	// Check cache first (read lock)
	cachedLOCMutex.RLock()
	if time.Since(cachedLOCTime) < locCacheTTL && cachedLOC > 0 {
		loc := cachedLOC
		cachedLOCMutex.RUnlock()
		return loc
	}
	cachedLOCMutex.RUnlock()

	// Cache miss or expired - calculate (expensive operation)
	// Use git ls-files to get all tracked files, then count lines
	// Exclude common non-code files
	// vc-ecc6: use CommandContext for cancellation support
	cmd := exec.CommandContext(ctx, "sh", "-c",
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

	// Update cache (write lock)
	cachedLOCMutex.Lock()
	cachedLOC = total
	cachedLOCTime = time.Now()
	cachedLOCMutex.Unlock()

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
	if len(decision.TargetAreas) > 0 {
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
	if len(areas) == 0 {
		return "Broad sampling across entire codebase"
	}
	return strings.Join(areas, ", ")
}

// ShouldExcludeFile determines if a file should be excluded from code review
// AC 10: Excludes generated files, vendor code, very large files, and binary/non-code files
func ShouldExcludeFile(filePath string, maxLines int) (bool, string) {
	// Generated files
	if strings.HasSuffix(filePath, ".pb.go") {
		return true, "generated protobuf file"
	}
	if strings.HasSuffix(filePath, ".gen.go") {
		return true, "generated code"
	}
	if strings.Contains(filePath, "_generated.") {
		return true, "generated file"
	}

	// Vendor code
	if strings.Contains(filePath, "/vendor/") || strings.HasPrefix(filePath, "vendor/") {
		return true, "vendor code"
	}
	if strings.Contains(filePath, "/third_party/") || strings.HasPrefix(filePath, "third_party/") {
		return true, "third-party code"
	}

	// Binary and non-code files
	binaryExts := []string{".bin", ".exe", ".dll", ".so", ".dylib", ".a", ".o", ".obj",
		".png", ".jpg", ".jpeg", ".gif", ".bmp", ".ico", ".svg",
		".pdf", ".zip", ".tar", ".gz", ".bz2", ".xz",
		".mp3", ".mp4", ".avi", ".mov", ".wav",
		".ttf", ".woff", ".woff2", ".eot"}
	for _, ext := range binaryExts {
		if strings.HasSuffix(filePath, ext) {
			return true, "binary/media file"
		}
	}

	// Very large files (requires reading file to count lines)
	if maxLines > 0 {
		content, err := os.ReadFile(filePath)
		if err != nil {
			return true, fmt.Sprintf("cannot read file: %v", err)
		}
		lineCount := strings.Count(string(content), "\n") + 1
		if lineCount > maxLines {
			return true, fmt.Sprintf("too large (%d lines)", lineCount)
		}
	}

	return false, ""
}

// SelectFilesForReview selects files for review based on decision scope and target areas
// AC 10: Applies file exclusions
func (s *Sweeper) SelectFilesForReview(ctx context.Context, decision *types.ReviewDecision) ([]string, error) {
	var files []string

	// Build git ls-files command based on target areas
	var cmd *exec.Cmd
	if len(decision.TargetAreas) > 0 {
		// Targeted review: specific directories
		args := []string{"ls-files"}
		args = append(args, decision.TargetAreas...)
		cmd = exec.CommandContext(ctx, "git", args...)
	} else {
		// Broad review: all tracked files
		cmd = exec.CommandContext(ctx, "git", "ls-files")
	}

	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to list files: %w", err)
	}

	allFiles := strings.Split(strings.TrimSpace(string(output)), "\n")

	// Filter files based on exclusion rules (AC 10)
	const maxFileLines = 1000 // Exclude files larger than 1000 lines
	for _, file := range allFiles {
		if file == "" {
			continue
		}

		// Apply exclusion rules
		excluded, _ := ShouldExcludeFile(file, maxFileLines)
		if excluded {
			continue
		}

		// Only include code files
		if isCodeFile(file) {
			files = append(files, file)
		}
	}

	// Randomly sample based on estimated file count
	if len(files) > decision.EstimatedFiles {
		files = randomSample(files, decision.EstimatedFiles)
	}

	return files, nil
}

// isCodeFile checks if a file is a code file worth reviewing
func isCodeFile(filePath string) bool {
	codeExts := []string{".go", ".js", ".ts", ".tsx", ".jsx", ".py", ".java", ".c", ".cpp", ".h", ".hpp", ".rs", ".rb", ".php", ".sh"}
	for _, ext := range codeExts {
		if strings.HasSuffix(filePath, ext) {
			return true
		}
	}
	return false
}

// randomSample returns a random sample of n items from the input slice
func randomSample(items []string, n int) []string {
	if n >= len(items) {
		return items
	}

	// Simple random sampling using current time as seed
	result := make([]string, n)
	indices := make(map[int]bool)

	for len(indices) < n {
		// Simple pseudo-random index using time
		idx := (int(time.Now().UnixNano()) + len(indices)) % len(items)
		if !indices[idx] {
			indices[idx] = true
		}
	}

	i := 0
	for idx := range indices {
		result[i] = items[idx]
		i++
	}

	return result
}

// FileDiscoveredIssue creates an issue from a file review finding
// AC 7: Files discovered issues with AI reasoning and suggestions
func (s *Sweeper) FileDiscoveredIssue(ctx context.Context, reviewIssueID string, fileIssue *types.FileReviewIssue, filePath string) (string, error) {
	// Parse priority (e.g., "P1" -> 1)
	priority := 2 // Default P2
	if len(fileIssue.Priority) >= 2 && fileIssue.Priority[0] == 'P' {
		if p, err := strconv.Atoi(fileIssue.Priority[1:]); err == nil {
			priority = p
		}
	}

	// Determine issue type based on file issue type
	issueType := types.TypeTask
	if fileIssue.Type == "bug" {
		issueType = types.TypeBug
	}

	// Build description with AI reasoning and suggestion
	description := fmt.Sprintf(`**File:** %s
**Location:** %s
**Severity:** %s

**Issue:**
%s

**Suggested Fix:**
%s

---
*Discovered by AI code review sweep*
*Type: %s*`, filePath, fileIssue.Location, fileIssue.Severity, fileIssue.Description, fileIssue.Suggestion, fileIssue.Type)

	// Create the issue
	issue := &types.Issue{
		Title:       fileIssue.Title,
		Description: description,
		IssueType:   issueType,
		Priority:    priority,
		Status:      types.StatusOpen,
	}

	actor := "code-review-sweeper"
	if err := s.store.CreateIssue(ctx, issue, actor); err != nil {
		return "", fmt.Errorf("failed to create discovered issue: %w", err)
	}

	// Add 'code-review-sweep' label
	if err := s.store.AddLabel(ctx, issue.ID, "code-review-sweep", actor); err != nil {
		// Log warning but continue
		fmt.Fprintf(os.Stderr, "warning: failed to add code-review-sweep label: %v\n", err)
	}

	// Add discovered-from dependency to link back to review issue
	if reviewIssueID != "" {
		dep := &types.Dependency{
			IssueID:     issue.ID,
			DependsOnID: reviewIssueID,
			Type:        types.DepDiscoveredFrom,
		}
		if err := s.store.AddDependency(ctx, dep, actor); err != nil {
			// Log warning but continue
			fmt.Fprintf(os.Stderr, "warning: failed to add discovered-from dependency: %v\n", err)
		}
	}

	return issue.ID, nil
}
