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
//   - supervisor: The AI supervisor for making duplicate detection API calls (must be non-nil)
//   - store: The storage layer for querying existing issues (must be non-nil)
//   - config: Configuration for deduplication behavior (must be valid)
//
// Returns an error if any dependencies are nil or if config validation fails.
//
// Example:
//
//	supervisor := ai.NewSupervisor(client, store, "claude-3-5-sonnet-20241022", ai.DefaultRetryConfig())
//	dedup, err := NewAIDeduplicator(supervisor, store, DefaultConfig())
//	if err != nil {
//	    return fmt.Errorf("failed to create deduplicator: %w", err)
//	}
func NewAIDeduplicator(supervisor *ai.Supervisor, store storage.Storage, config Config) (*AIDeduplicator, error) {
	// Validate dependencies
	if supervisor == nil {
		return nil, fmt.Errorf("supervisor cannot be nil")
	}
	if store == nil {
		return nil, fmt.Errorf("store cannot be nil")
	}

	// Validate configuration
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	return &AIDeduplicator{
		supervisor: supervisor,
		store:      store,
		config:     config,
	}, nil
}

// CheckDuplicate checks if a candidate issue is a duplicate of any recent open issues
func (d *AIDeduplicator) CheckDuplicate(ctx context.Context, candidate *types.Issue) (*DuplicateDecision, error) {
	// Validate candidate issue (config is already validated in constructor)
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

	// Query recent open issues
	// Note: GetReadyWork returns open issues with no blockers
	// For deduplication, we actually want ALL open issues, but this is a reasonable approximation
	filter := types.WorkFilter{
		Status: types.StatusOpen,
		Limit:  d.config.MaxCandidates,
	}

	existingIssues, err := d.store.GetReadyWork(ctx, filter)
	if err != nil {
		// Fail-safe: if we can't query existing issues, assume not duplicate
		log.Printf("[DEDUP] Failed to query existing issues: %v (assuming not duplicate)", err)
		return &DuplicateDecision{
			IsDuplicate:   false,
			Confidence:    0.0,
			Reasoning:     fmt.Sprintf("Failed to query existing issues: %v", err),
			ComparedCount: 0,
		}, nil
	}

	// Filter out self-comparison
	filteredIssues := []*types.Issue{}
	for _, existing := range existingIssues {
		if existing.ID != candidate.ID {
			filteredIssues = append(filteredIssues, existing)
		}
	}

	if len(filteredIssues) == 0 {
		return &DuplicateDecision{
			IsDuplicate:   false,
			Confidence:    0.0,
			Reasoning:     "No existing issues to compare against",
			ComparedCount: 0,
		}, nil
	}

	// Process in batches for efficiency
	var bestMatch *DuplicateDecision
	totalCompared := 0

	for i := 0; i < len(filteredIssues); i += d.config.BatchSize {
		// Get batch slice
		end := i + d.config.BatchSize
		if end > len(filteredIssues) {
			end = len(filteredIssues)
		}
		batch := filteredIssues[i:end]

		// Use batch AI check (single API call for entire batch)
		batchResp, err := d.supervisor.CheckIssueDuplicateBatch(ctx, candidate, batch)
		if err != nil {
			// Log error but continue checking other batches (fail-safe)
			log.Printf("[DEDUP] Batch AI check failed for %s vs batch: %v", candidate.ID, err)
			continue
		}

		totalCompared += len(batch)

		// Process results from this batch
		for _, result := range batchResp.Results {
			// Track best match across all batches
			// Design decision: We honor BOTH the AI's semantic IsDuplicate judgment
			// AND validate it meets our confidence threshold. This means:
			// - If AI says IsDuplicate=false with confidence=0.95, we respect that
			//   semantic judgment (e.g., similar but distinct issues)
			// - If AI says IsDuplicate=true but confidence=0.70, we reject it (below threshold)
			// This dual-check prevents false positives while trusting AI's semantic analysis.
			if bestMatch == nil || result.Confidence > bestMatch.Confidence {
				bestMatch = &DuplicateDecision{
					IsDuplicate:   result.IsDuplicate && result.Confidence >= d.config.ConfidenceThreshold,
					DuplicateOf:   result.ExistingIssueID,
					Confidence:    result.Confidence,
					Reasoning:     result.Reasoning,
					ComparedCount: totalCompared,
				}
			}

			// If we found a high-confidence duplicate, we can stop early
			if result.IsDuplicate && result.Confidence >= d.config.ConfidenceThreshold {
				log.Printf("[DEDUP] High-confidence duplicate found: %s is duplicate of %s (%.2f)",
					candidate.ID, result.ExistingIssueID, result.Confidence)
				bestMatch.ComparedCount = totalCompared
				return bestMatch, nil
			}
		}
	}

	// Return best match or non-duplicate if no matches found
	if bestMatch != nil {
		return bestMatch, nil
	}

	return &DuplicateDecision{
		IsDuplicate:   false,
		Confidence:    0.0,
		Reasoning:     "No similar issues found",
		ComparedCount: totalCompared,
	}, nil
}

// DeduplicateBatch processes multiple issues at once for efficiency
func (d *AIDeduplicator) DeduplicateBatch(ctx context.Context, candidates []*types.Issue) (*DeduplicationResult, error) {
	startTime := time.Now()

	// Validate candidates (config is already validated in constructor)
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

	// Track results
	uniqueIssues := []*types.Issue{}
	duplicatePairs := make(map[int]string)
	withinBatchDuplicates := make(map[int]int)
	comparisons := 0
	aiCalls := 0

	// Process each candidate
	for i, candidate := range candidates {
		// First check if it's a duplicate within the batch (if enabled)
		isWithinBatchDup := false
		if d.config.EnableWithinBatchDedup {
			for j := 0; j < i; j++ {
				// Skip if j is already marked as duplicate
				if _, isDup := duplicatePairs[j]; isDup {
					continue
				}
				if _, isDup := withinBatchDuplicates[j]; isDup {
					continue
				}

				// Compare against earlier candidate
				resp, err := d.supervisor.CheckIssueDuplicate(ctx, candidate, candidates[j])
				aiCalls++
				comparisons++

				if err != nil {
					log.Printf("[DEDUP] Within-batch check failed for %d vs %d: %v", i, j, err)
					continue
				}

				if resp.IsDuplicate && resp.Confidence >= d.config.ConfidenceThreshold {
					withinBatchDuplicates[i] = j
					isWithinBatchDup = true
					log.Printf("[DEDUP] Within-batch duplicate: %s is duplicate of %s (confidence: %.2f)",
						candidate.ID, candidates[j].ID, resp.Confidence)
					break
				}
			}
		}

		// If it's a within-batch duplicate, skip further checks
		if isWithinBatchDup {
			continue
		}

		// Check against existing issues in storage
		decision, err := d.CheckDuplicate(ctx, candidate)
		if err != nil {
			// Fail-safe: treat as unique on error
			log.Printf("[DEDUP] CheckDuplicate failed for %s: %v (treating as unique)", candidate.ID, err)
			uniqueIssues = append(uniqueIssues, candidate)
			continue
		}

		comparisons += decision.ComparedCount
		// Calculate actual AI calls made (CheckDuplicate uses batching)
		// Number of batches = ceil(ComparedCount / BatchSize)
		batchCount := (decision.ComparedCount + d.config.BatchSize - 1) / d.config.BatchSize
		aiCalls += batchCount

		if decision.IsDuplicate {
			duplicatePairs[i] = decision.DuplicateOf
			log.Printf("[DEDUP] Duplicate found: %s is duplicate of %s (confidence: %.2f)",
				candidate.ID, decision.DuplicateOf, decision.Confidence)
		} else {
			uniqueIssues = append(uniqueIssues, candidate)
		}
	}

	result := &DeduplicationResult{
		UniqueIssues:          uniqueIssues,
		DuplicatePairs:        duplicatePairs,
		WithinBatchDuplicates: withinBatchDuplicates,
		Stats: DeduplicationStats{
			TotalCandidates:           len(candidates),
			UniqueCount:               len(uniqueIssues),
			DuplicateCount:            len(duplicatePairs),
			WithinBatchDuplicateCount: len(withinBatchDuplicates),
			ComparisonsMade:           comparisons,
			AICallsMade:               aiCalls,
			ProcessingTimeMs:          time.Since(startTime).Milliseconds(),
		},
	}

	return result, nil
}
