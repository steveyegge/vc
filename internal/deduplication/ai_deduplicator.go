package deduplication

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/steveyegge/vc/internal/ai"
	"github.com/steveyegge/vc/internal/storage"
	"github.com/steveyegge/vc/internal/types"
)

// AIDeduplicator implements the Deduplicator interface using AI-powered semantic analysis
type AIDeduplicator struct {
	supervisor *ai.Supervisor
	store      storage.Storage
	config     Config
}

// Compile-time check that AIDeduplicator implements Deduplicator
var _ Deduplicator = (*AIDeduplicator)(nil)

// NewAIDeduplicator creates a new AI-powered deduplicator
//
// Parameters:
//   - supervisor: The AI supervisor for making duplicate detection API calls
//   - store: The storage layer for querying existing issues
//   - config: Configuration for deduplication behavior
//
// Example:
//
//	supervisor := ai.NewSupervisor(client, store, "claude-3-5-sonnet-20241022", ai.DefaultRetryConfig())
//	dedup := NewAIDeduplicator(supervisor, store, DefaultConfig())
func NewAIDeduplicator(supervisor *ai.Supervisor, store storage.Storage, config Config) *AIDeduplicator {
	return &AIDeduplicator{
		supervisor: supervisor,
		store:      store,
		config:     config,
	}
}

// CheckDuplicate checks if a candidate issue is a duplicate of any recent open issues
//
// This is a stub implementation that will be completed in the next issue.
// For now, it returns a non-duplicate decision to maintain fail-safe behavior.
func (d *AIDeduplicator) CheckDuplicate(ctx context.Context, candidate *types.Issue) (*DuplicateDecision, error) {
	// Validate configuration
	if err := d.config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	// Validate candidate issue
	if candidate == nil {
		return nil, fmt.Errorf("candidate issue cannot be nil")
	}
	if err := candidate.Validate(); err != nil {
		return nil, fmt.Errorf("invalid candidate issue: %w", err)
	}

	// Skip very short titles (not enough semantic content)
	if len(candidate.Title) < d.config.MinTitleLength {
		log.Printf("[DEDUP] Skipping dedup for short title (len=%d, min=%d): %s",
			len(candidate.Title), d.config.MinTitleLength, candidate.Title)
		return &DuplicateDecision{
			IsDuplicate:   false,
			Confidence:    0.0,
			Reasoning:     fmt.Sprintf("Title too short for semantic comparison (len=%d)", len(candidate.Title)),
			ComparedCount: 0,
		}, nil
	}

	// TODO: Implement actual AI-powered duplicate detection
	// This will be completed in the next issue (after vc-146)
	//
	// Steps:
	// 1. Query recent open issues from storage (within lookback window)
	// 2. Filter to MaxCandidates most relevant issues
	// 3. Use AI supervisor to compare candidate against each existing issue
	// 4. Return decision with highest confidence match
	//
	// For now, return non-duplicate decision (fail-safe behavior)
	log.Printf("[DEDUP] CheckDuplicate stub: would check '%s' against recent issues", candidate.Title)

	return &DuplicateDecision{
		IsDuplicate:   false,
		Confidence:    0.0,
		Reasoning:     "Stub implementation - actual AI deduplication not yet implemented",
		ComparedCount: 0,
	}, nil
}

// DeduplicateBatch processes multiple issues at once for efficiency
//
// This is a stub implementation that will be completed in the next issue.
// For now, it returns all candidates as unique to maintain fail-safe behavior.
func (d *AIDeduplicator) DeduplicateBatch(ctx context.Context, candidates []*types.Issue) (*DeduplicationResult, error) {
	startTime := time.Now()

	// Validate configuration
	if err := d.config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	// Validate candidates
	if len(candidates) == 0 {
		return &DeduplicationResult{
			UniqueIssues:          []*types.Issue{},
			DuplicatePairs:        make(map[int]string),
			WithinBatchDuplicates: make(map[int]int),
			Stats: DeduplicationStats{
				TotalCandidates:           0,
				UniqueCount:               0,
				DuplicateCount:            0,
				WithinBatchDuplicateCount: 0,
				ComparisonsMade:           0,
				AICallsMade:               0,
				ProcessingTimeMs:          time.Since(startTime).Milliseconds(),
			},
		}, nil
	}

	for i, candidate := range candidates {
		if candidate == nil {
			return nil, fmt.Errorf("candidate at index %d is nil", i)
		}
		if err := candidate.Validate(); err != nil {
			return nil, fmt.Errorf("invalid candidate at index %d: %w", i, err)
		}
	}

	// TODO: Implement actual batch deduplication
	// This will be completed in the next issue (after vc-146)
	//
	// Steps:
	// 1. Query recent open issues from storage (within lookback window)
	// 2. If EnableWithinBatchDedup, also compare candidates against each other
	// 3. Batch AI API calls for efficiency
	// 4. Build result with unique issues and duplicate mappings
	//
	// For now, return all as unique (fail-safe behavior)
	log.Printf("[DEDUP] DeduplicateBatch stub: would process %d candidates", len(candidates))

	result := &DeduplicationResult{
		UniqueIssues:          candidates,
		DuplicatePairs:        make(map[int]string),
		WithinBatchDuplicates: make(map[int]int),
		Stats: DeduplicationStats{
			TotalCandidates:           len(candidates),
			UniqueCount:               len(candidates),
			DuplicateCount:            0,
			WithinBatchDuplicateCount: 0,
			ComparisonsMade:           0,
			AICallsMade:               0,
			ProcessingTimeMs:          time.Since(startTime).Milliseconds(),
		},
	}

	return result, nil
}
