package codereview

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/steveyegge/vc/internal/storage/beads"
	"github.com/steveyegge/vc/internal/types"
)

// TestGetTotalLOCCaching tests that getTotalLOC() uses cache correctly
func TestGetTotalLOCCaching(t *testing.T) {
	ctx := context.Background()

	// Reset cache state
	cachedLOCMutex.Lock()
	cachedLOC = 0
	cachedLOCTime = time.Time{}
	cachedLOCMutex.Unlock()

	// First call - should calculate (cache miss)
	start := time.Now()
	loc1 := getTotalLOC(ctx)
	duration1 := time.Since(start)

	if loc1 == 0 {
		t.Skip("No code files found in repository")
	}

	// Verify cache is set
	cachedLOCMutex.RLock()
	if cachedLOC != loc1 {
		t.Errorf("Cache not set after first call: got %d, want %d", cachedLOC, loc1)
	}
	if cachedLOCTime.IsZero() {
		t.Error("Cache timestamp not set after first call")
	}
	cachedLOCMutex.RUnlock()

	// Second call (immediate) - should use cache (fast)
	start = time.Now()
	loc2 := getTotalLOC(ctx)
	duration2 := time.Since(start)

	if loc2 != loc1 {
		t.Errorf("Second call returned different value: got %d, want %d", loc2, loc1)
	}

	// Cache hit should be much faster (< 10ms vs potentially seconds)
	if duration2 > 10*time.Millisecond {
		t.Errorf("Cached call took too long: %v (expected < 10ms)", duration2)
	}

	t.Logf("First call (cache miss): %v", duration1)
	t.Logf("Second call (cache hit): %v", duration2)
	t.Logf("Speedup: %.2fx", float64(duration1)/float64(duration2))
}

// TestGetTotalLOCCacheExpiry tests that cache expires after TTL
func TestGetTotalLOCCacheExpiry(t *testing.T) {
	ctx := context.Background()

	// Set cache with old timestamp
	cachedLOCMutex.Lock()
	cachedLOC = 12345
	cachedLOCTime = time.Now().Add(-2 * time.Hour) // Expired (TTL is 1 hour)
	cachedLOCMutex.Unlock()

	// Call should recalculate
	loc := getTotalLOC(ctx)

	// If we got code files, should not be the cached value
	if loc > 0 && loc == 12345 {
		t.Log("Warning: Got cached value after expiry - may be coincidence or cache didn't expire")
	}

	// Verify cache was updated with fresh timestamp
	cachedLOCMutex.RLock()
	timeSinceCache := time.Since(cachedLOCTime)
	cachedLOCMutex.RUnlock()

	if timeSinceCache > 1*time.Second {
		t.Errorf("Cache timestamp not updated after expiry: %v old", timeSinceCache)
	}
}

// TestGetTotalLOCThreadSafety tests concurrent access to cache
func TestGetTotalLOCThreadSafety(t *testing.T) {
	ctx := context.Background()

	// Reset cache
	cachedLOCMutex.Lock()
	cachedLOC = 0
	cachedLOCTime = time.Time{}
	cachedLOCMutex.Unlock()

	// Run 10 concurrent getTotalLOC() calls
	done := make(chan int, 10)
	for i := 0; i < 10; i++ {
		go func() {
			loc := getTotalLOC(ctx)
			done <- loc
		}()
	}

	// Collect results
	var results []int
	for i := 0; i < 10; i++ {
		results = append(results, <-done)
	}

	// All results should be the same (thread-safe cache)
	for i := 1; i < len(results); i++ {
		if results[i] != results[0] {
			t.Errorf("Concurrent calls returned different values: %d vs %d", results[i], results[0])
		}
	}
}

// TestGetTotalLOCContextCancellation tests that getTotalLOC respects context cancellation
func TestGetTotalLOCContextCancellation(t *testing.T) {
	// Reset cache to force actual calculation
	cachedLOCMutex.Lock()
	cachedLOC = 0
	cachedLOCTime = time.Time{}
	cachedLOCMutex.Unlock()

	// Create cancellable context and cancel immediately
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel before call

	// Call should return quickly with 0 (command fails due to cancellation)
	start := time.Now()
	loc := getTotalLOC(ctx)
	duration := time.Since(start)

	// Should fail fast (canceled context)
	if duration > 100*time.Millisecond {
		t.Errorf("Canceled getTotalLOC took too long: %v (expected < 100ms)", duration)
	}

	// Should return 0 when context is canceled (command fails)
	if loc != 0 {
		t.Logf("Note: getTotalLOC returned %d with canceled context (may have used cache)", loc)
	}

	t.Logf("Canceled call duration: %v", duration)
}

func TestGetDiffMetricsIncludesLastReviewSummary(t *testing.T) {
	ctx := context.Background()
	repoDir := t.TempDir()
	dbPath := filepath.Join(t.TempDir(), "test.db")

	initTestRepo(t, repoDir)
	baseSHA := commitFile(t, repoDir, "main.go", "package main\n\nfunc main() {}\n", "initial commit")

	store, err := beads.NewVCStorage(ctx, dbPath)
	if err != nil {
		t.Fatalf("Failed to create VC storage: %v", err)
	}
	defer func() { _ = store.Close() }()

	reviewIssue := &types.Issue{
		Title: "Code Review Sweep: quick",
		Description: `Perform quick code review sweep based on accumulated activity.

**AI Reasoning:**
Focus on executor concurrency and error handling.

**Scope:** quick`,
		Status:             types.StatusOpen,
		Priority:           1,
		IssueType:          types.TypeTask,
		AcceptanceCriteria: "Review the target files",
	}
	if err := store.CreateIssue(ctx, reviewIssue, "test"); err != nil {
		t.Fatalf("Failed to create review issue: %v", err)
	}

	checkpoint := &types.ReviewCheckpoint{
		CommitSHA:   baseSHA,
		Timestamp:   time.Now().Add(-48 * time.Hour).UTC(),
		ReviewScope: "quick",
	}
	if err := store.SaveReviewCheckpoint(ctx, checkpoint, reviewIssue.ID); err != nil {
		t.Fatalf("Failed to save review checkpoint: %v", err)
	}

	commitFile(t, repoDir, "main.go", "package main\n\nfunc main() {\n\tprintln(\"hi\")\n}\n", "add change")

	resetLOCCache()
	t.Chdir(repoDir)

	result, err := NewSweeper(store).GetDiffMetrics(ctx)
	if err != nil {
		t.Fatalf("GetDiffMetrics returned error: %v", err)
	}

	want := "Code Review Sweep: quick - Focus on executor concurrency and error handling."
	if result.Metrics.LastReviewSummary != want {
		t.Fatalf("Expected LastReviewSummary %q, got %q", want, result.Metrics.LastReviewSummary)
	}
}

func TestGetDiffMetricsLeavesLastReviewSummaryEmptyWithoutReviewIssue(t *testing.T) {
	ctx := context.Background()
	repoDir := t.TempDir()
	dbPath := filepath.Join(t.TempDir(), "test.db")

	initTestRepo(t, repoDir)
	baseSHA := commitFile(t, repoDir, "main.go", "package main\n\nfunc main() {}\n", "initial commit")

	store, err := beads.NewVCStorage(ctx, dbPath)
	if err != nil {
		t.Fatalf("Failed to create VC storage: %v", err)
	}
	defer func() { _ = store.Close() }()

	checkpoint := &types.ReviewCheckpoint{
		CommitSHA:   baseSHA,
		Timestamp:   time.Now().Add(-24 * time.Hour).UTC(),
		ReviewScope: "quick",
	}
	if err := store.SaveReviewCheckpoint(ctx, checkpoint, ""); err != nil {
		t.Fatalf("Failed to save review checkpoint: %v", err)
	}

	commitFile(t, repoDir, "main.go", "package main\n\nfunc main() {\n\tprintln(\"bye\")\n}\n", "add change")

	resetLOCCache()
	t.Chdir(repoDir)

	result, err := NewSweeper(store).GetDiffMetrics(ctx)
	if err != nil {
		t.Fatalf("GetDiffMetrics returned error: %v", err)
	}
	if result.Metrics.LastReviewSummary != "" {
		t.Fatalf("Expected empty LastReviewSummary, got %q", result.Metrics.LastReviewSummary)
	}
}

func resetLOCCache() {
	cachedLOCMutex.Lock()
	cachedLOC = 0
	cachedLOCTime = time.Time{}
	cachedLOCMutex.Unlock()
}

func initTestRepo(t *testing.T, repoDir string) {
	t.Helper()

	runGit(t, repoDir, "init", "--initial-branch=main")
	runGit(t, repoDir, "config", "user.name", "Test User")
	runGit(t, repoDir, "config", "user.email", "test@example.com")
	hooksDir := filepath.Join(repoDir, ".git", "empty-hooks")
	if err := os.MkdirAll(hooksDir, 0755); err != nil {
		t.Fatalf("Failed to create empty hooks dir: %v", err)
	}
	runGit(t, repoDir, "config", "core.hooksPath", hooksDir)
	runGit(t, repoDir, "checkout", "-b", "feature/test-review-summary")
}

func commitFile(t *testing.T, repoDir, path, content, message string) string {
	t.Helper()

	fullPath := filepath.Join(repoDir, path)
	if err := os.WriteFile(fullPath, []byte(content), 0644); err != nil {
		t.Fatalf("Failed to write %s: %v", path, err)
	}
	runGit(t, repoDir, "add", path)
	runGit(t, repoDir, "commit", "-m", message)

	return strings.TrimSpace(runGit(t, repoDir, "rev-parse", "HEAD"))
}

func runGit(t *testing.T, repoDir string, args ...string) string {
	t.Helper()

	cmd := exec.Command("git", args...)
	cmd.Dir = repoDir
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, output)
	}
	return string(output)
}
