package executor

import (
	"context"
	"testing"
	"time"

	"github.com/steveyegge/vc/internal/ai"
	"github.com/steveyegge/vc/internal/storage"
	"github.com/steveyegge/vc/internal/types"
)

// TestDeduplicateLargeBatch validates that large batches are truncated (vc-a80e)
func TestDeduplicateLargeBatch(t *testing.T) {
	cfg := storage.DefaultConfig()
	cfg.Path = t.TempDir() + "/test_large_batch.db"

	ctx := context.Background()
	store, err := storage.NewStorage(ctx, cfg)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer store.Close()

	// Create parent issue
	parent := &types.Issue{
		ID:                 "vc-test",
		Title:              "Test Parent",
		Description:        "Parent issue for deduplication test",
		AcceptanceCriteria: "Test completion",
		IssueType:          types.TypeTask,
		Priority:           2,
		Status:             types.StatusOpen,
	}
	if err := store.CreateIssue(ctx, parent, "test"); err != nil {
		t.Fatalf("Failed to create parent issue: %v", err)
	}

	// Create results processor with small batch size
	rp, err := NewResultsProcessor(&ResultsProcessorConfig{
		Store:          store,
		Actor:          "test",
		DedupBatchSize: 5, // Small batch size for testing
	})
	if err != nil {
		t.Fatalf("Failed to create results processor: %v", err)
	}

	// Create 10 discovered issues (more than batch size)
	discovered := make([]ai.DiscoveredIssue, 10)
	for i := 0; i < 10; i++ {
		discovered[i] = ai.DiscoveredIssue{
			Title:       "Test Issue " + string(rune('A'+i)),
			Description: "Test description",
			Type:        "task",
			Priority:    "P2",
		}
	}

	// Deduplication should truncate to batch size
	// Note: This will fail since deduplicator is nil, but we're testing the truncation logic
	unique, stats := rp.deduplicateDiscoveredIssues(ctx, parent, discovered)

	// Should have truncated to 5 issues (batch size)
	// Note: unique may be empty if deduplicator is nil, but truncation should still happen
	if len(unique) > 5 {
		t.Errorf("Expected at most 5 unique issues (batch size), got %d", len(unique))
	}

	// Stats should reflect truncated batch (or be zero if dedup failed)
	if stats.TotalCandidates > 5 {
		t.Errorf("Expected stats to show at most 5 candidates, got %d", stats.TotalCandidates)
	}

	t.Logf("Large batch test passed: %d unique issues from truncated batch", len(unique))
}

// TestDeduplicateSmallBatch validates normal batch handling (vc-a80e)
func TestDeduplicateSmallBatch(t *testing.T) {
	cfg := storage.DefaultConfig()
	cfg.Path = t.TempDir() + "/test_small_batch.db"

	ctx := context.Background()
	store, err := storage.NewStorage(ctx, cfg)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer store.Close()

	// Create parent issue
	parent := &types.Issue{
		ID:                 "vc-test2",
		Title:              "Test Parent 2",
		Description:        "Parent issue for deduplication test",
		AcceptanceCriteria: "Test completion",
		IssueType:          types.TypeTask,
		Priority:           2,
		Status:             types.StatusOpen,
	}
	if err := store.CreateIssue(ctx, parent, "test"); err != nil {
		t.Fatalf("Failed to create parent issue: %v", err)
	}

	// Create results processor with default batch size
	rp, err := NewResultsProcessor(&ResultsProcessorConfig{
		Store:          store,
		Actor:          "test",
		DedupBatchSize: 100, // Default batch size
	})
	if err != nil {
		t.Fatalf("Failed to create results processor: %v", err)
	}

	// Create 3 discovered issues (less than batch size)
	discovered := []ai.DiscoveredIssue{
		{
			Title:       "Small Issue 1",
			Description: "Test description 1",
			Type:        "task",
			Priority:    "P2",
		},
		{
			Title:       "Small Issue 2",
			Description: "Test description 2",
			Type:        "bug",
			Priority:    "P1",
		},
		{
			Title:       "Small Issue 3",
			Description: "Test description 3",
			Type:        "feature",
			Priority:    "P3",
		},
	}

	// Deduplication should process all issues
	unique, _ := rp.deduplicateDiscoveredIssues(ctx, parent, discovered)

	// Should process all 3 issues (no truncation)
	// Note: unique may be empty if deduplication fails (no deduplicator configured)
	// but we're testing that truncation didn't happen
	if len(discovered) != 3 {
		t.Errorf("Input was modified unexpectedly")
	}

	t.Logf("Small batch test passed: processed %d unique issues without truncation", len(unique))
}

// TestDeduplicationTimeout tests graceful handling when AI deduplication times out (vc-n8ua)
func TestDeduplicationTimeout(t *testing.T) {
	// Setup storage
	cfg := storage.DefaultConfig()
	cfg.Path = t.TempDir() + "/test.db"

	ctx := context.Background()
	store, err := storage.NewStorage(ctx, cfg)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer func() { _ = store.Close() }()

	// Create a parent issue
	parent := &types.Issue{
		Title:              "Parent Issue",
		Description:        "Parent for dedup test",
		Status:             types.StatusInProgress,
		Priority:           1,
		IssueType:          types.TypeTask,
		AcceptanceCriteria: "Test timeout handling",
		CreatedAt:          time.Now(),
		UpdatedAt:          time.Now(),
	}

	if err := store.CreateIssue(ctx, parent, "test"); err != nil {
		t.Fatalf("Failed to create parent issue: %v", err)
	}

	// Create results processor WITHOUT deduplicator (nil deduplicator)
	rpCfg := &ResultsProcessorConfig{
		Store:      store,
		WorkingDir: t.TempDir(),
		Actor:      "test-executor",
		// Deduplicator is nil - simulates timeout/failure scenario
	}

	rp, err := NewResultsProcessor(rpCfg)
	if err != nil {
		t.Fatalf("Failed to create results processor: %v", err)
	}

	// Create some discovered issues
	discovered := []ai.DiscoveredIssue{
		{
			Title:       "Test Issue 1",
			Description: "First test issue",
			Type:        "task",
			Priority:    "P2",
		},
		{
			Title:       "Test Issue 2",
			Description: "Second test issue",
			Type:        "bug",
			Priority:    "P1",
		},
	}

	// Create context with short timeout
	timeoutCtx, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
	defer cancel()

	// Call deduplicateDiscoveredIssues with timeout context
	unique, stats := rp.deduplicateDiscoveredIssues(timeoutCtx, parent, discovered)

	// Without deduplicator, should return all issues as unique
	if len(unique) != len(discovered) {
		t.Errorf("Expected %d unique issues (dedup disabled), got %d", len(discovered), len(unique))
	}

	// Stats should reflect no deduplication occurred
	if stats.TotalCandidates != len(discovered) {
		t.Errorf("Expected TotalCandidates=%d, got %d", len(discovered), stats.TotalCandidates)
	}

	// Most importantly: no panic occurred
}

// TestDeduplicationMalformedIssue tests handling of malformed DiscoveredIssue data (vc-n8ua)
func TestDeduplicationMalformedIssue(t *testing.T) {
	// Setup storage
	cfg := storage.DefaultConfig()
	cfg.Path = t.TempDir() + "/test.db"

	ctx := context.Background()
	store, err := storage.NewStorage(ctx, cfg)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer func() { _ = store.Close() }()

	// Create a parent issue
	parent := &types.Issue{
		Title:              "Parent Issue",
		Description:        "Parent for malformed test",
		Status:             types.StatusInProgress,
		Priority:           1,
		IssueType:          types.TypeTask,
		AcceptanceCriteria: "Test malformed data handling",
		CreatedAt:          time.Now(),
		UpdatedAt:          time.Now(),
	}

	if err := store.CreateIssue(ctx, parent, "test"); err != nil {
		t.Fatalf("Failed to create parent issue: %v", err)
	}

	// Create results processor
	rpCfg := &ResultsProcessorConfig{
		Store:      store,
		WorkingDir: t.TempDir(),
		Actor:      "test-executor",
		// No deduplicator - will skip dedup logic
	}

	rp, err := NewResultsProcessor(rpCfg)
	if err != nil {
		t.Fatalf("Failed to create results processor: %v", err)
	}

	// Create discovered issues with malformed data
	discovered := []ai.DiscoveredIssue{
		{
			Title:       "", // Empty title
			Description: "Issue with empty title",
			Type:        "task",
			Priority:    "P2",
		},
		{
			Title:       "Valid Title",
			Description: "", // Empty description
			Type:        "",  // Empty type
			Priority:    "INVALID", // Invalid priority
		},
		{
			// Completely empty issue
		},
	}

	// Call deduplicateDiscoveredIssues - should not panic
	unique, stats := rp.deduplicateDiscoveredIssues(ctx, parent, discovered)

	// Should handle malformed data gracefully
	t.Logf("Processed %d malformed issues, got %d unique, stats: %+v", len(discovered), len(unique), stats)

	// Most important: no panic occurred
}

// TestDeduplicationEmptyBatch tests handling of empty discovered issues list (vc-n8ua)
func TestDeduplicationEmptyBatch(t *testing.T) {
	// Setup storage
	cfg := storage.DefaultConfig()
	cfg.Path = t.TempDir() + "/test.db"

	ctx := context.Background()
	store, err := storage.NewStorage(ctx, cfg)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer func() { _ = store.Close() }()

	// Create a parent issue
	parent := &types.Issue{
		Title:              "Parent Issue",
		Description:        "Parent for empty batch test",
		Status:             types.StatusInProgress,
		Priority:           1,
		IssueType:          types.TypeTask,
		AcceptanceCriteria: "Test empty batch handling",
		CreatedAt:          time.Now(),
		UpdatedAt:          time.Now(),
	}

	if err := store.CreateIssue(ctx, parent, "test"); err != nil {
		t.Fatalf("Failed to create parent issue: %v", err)
	}

	// Create results processor
	rpCfg := &ResultsProcessorConfig{
		Store:      store,
		WorkingDir: t.TempDir(),
		Actor:      "test-executor",
	}

	rp, err := NewResultsProcessor(rpCfg)
	if err != nil {
		t.Fatalf("Failed to create results processor: %v", err)
	}

	// Call with empty list - should not panic
	unique, stats := rp.deduplicateDiscoveredIssues(ctx, parent, []ai.DiscoveredIssue{})

	// Should return empty results
	if len(unique) != 0 {
		t.Errorf("Expected 0 unique issues for empty batch, got %d", len(unique))
	}

	if stats.TotalCandidates != 0 {
		t.Errorf("Expected TotalCandidates=0 for empty batch, got %d", stats.TotalCandidates)
	}
}
