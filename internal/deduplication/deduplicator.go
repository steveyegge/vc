package deduplication

import (
	"context"
	"fmt"

	"github.com/steveyegge/vc/internal/types"
)

// Deduplicator defines the interface for detecting duplicate issues
// using AI-powered semantic similarity analysis.
//
// The deduplicator is used to prevent multiple workers from filing
// duplicate issues when they encounter the same problems during execution.
//
// Example usage:
//
//	dedup := NewAIDeduplicator(supervisor, store, DefaultConfig())
//
//	// Check single issue
//	decision, err := dedup.CheckDuplicate(ctx, candidateIssue)
//	if err != nil {
//	    log.Printf("Dedup check failed: %v", err)
//	}
//	if decision.IsDuplicate {
//	    log.Printf("Issue is duplicate of %s (confidence: %.2f)",
//	        decision.DuplicateOf, decision.Confidence)
//	}
//
//	// Batch deduplication
//	result, err := dedup.DeduplicateBatch(ctx, discoveredIssues)
//	if err != nil {
//	    log.Printf("Batch dedup failed: %v", err)
//	}
//	log.Printf("Unique: %d, Duplicates: %d",
//	    len(result.UniqueIssues), len(result.DuplicatePairs))
type Deduplicator interface {
	// CheckDuplicate checks if a candidate issue is a duplicate of any recent open issues.
	// It returns a DuplicateDecision with the duplicate determination and confidence score.
	//
	// The method compares against recent open issues (configurable window, default 7 days)
	// using semantic similarity analysis. Considers:
	// - Title similarity
	// - File/line references in description
	// - Parent issue context (if any)
	// - Issue type and priority
	//
	// Returns:
	// - DuplicateDecision with IsDuplicate=true if high confidence match found (>= threshold)
	// - DuplicateDecision with IsDuplicate=false if no high confidence match
	// - Error if the deduplication process fails (caller should fail-safe and file the issue)
	CheckDuplicate(ctx context.Context, candidate *types.Issue) (*DuplicateDecision, error)

	// DeduplicateBatch processes multiple issues at once for efficiency.
	// This is more efficient than calling CheckDuplicate repeatedly because:
	// - Fetches comparison candidates once
	// - Can batch AI API calls
	// - Deduplicates within the batch itself (detects duplicates among candidates)
	//
	// Returns:
	// - DeduplicationResult with unique issues, duplicate pairs, and statistics
	// - Error if the batch processing fails
	DeduplicateBatch(ctx context.Context, candidates []*types.Issue) (*DeduplicationResult, error)
}

// DuplicateDecision represents the result of checking a single issue for duplicates
type DuplicateDecision struct {
	// IsDuplicate is true if the candidate is a duplicate with high confidence
	IsDuplicate bool `json:"is_duplicate"`

	// DuplicateOf is the ID of the existing issue that this is a duplicate of
	// Only set when IsDuplicate is true
	DuplicateOf string `json:"duplicate_of,omitempty"`

	// Confidence is the AI's confidence score (0.0 to 1.0)
	// Only mark as duplicate if confidence >= threshold (default 0.85)
	Confidence float64 `json:"confidence"`

	// Reasoning explains why the AI made this determination
	// Useful for debugging and transparency
	Reasoning string `json:"reasoning,omitempty"`

	// ComparedCount is the number of existing issues compared against
	// Useful for metrics and understanding search scope
	ComparedCount int `json:"compared_count"`
}

// Validate checks if the duplicate decision has valid values
func (d *DuplicateDecision) Validate() error {
	if d.Confidence < 0.0 || d.Confidence > 1.0 {
		return fmt.Errorf("confidence must be between 0.0 and 1.0 (got %.2f)", d.Confidence)
	}
	if d.IsDuplicate && d.DuplicateOf == "" {
		return fmt.Errorf("duplicate_of must be set when is_duplicate is true")
	}
	if !d.IsDuplicate && d.DuplicateOf != "" {
		return fmt.Errorf("duplicate_of should not be set when is_duplicate is false")
	}
	if d.ComparedCount < 0 {
		return fmt.Errorf("compared_count cannot be negative (got %d)", d.ComparedCount)
	}
	return nil
}

// DeduplicationResult represents the result of batch deduplication
type DeduplicationResult struct {
	// UniqueIssues are the issues that are not duplicates
	// These should be filed to the tracker
	UniqueIssues []*types.Issue `json:"unique_issues"`

	// DuplicatePairs maps duplicate issue indices to the existing issue ID
	// Key: index in the original candidates slice
	// Value: ID of the existing issue it's a duplicate of
	DuplicatePairs map[int]string `json:"duplicate_pairs"`

	// WithinBatchDuplicates maps duplicate issue indices to the first occurrence index
	// Key: index in the original candidates slice (duplicate)
	// Value: index in the original candidates slice (first occurrence)
	// This handles duplicates within the batch itself
	WithinBatchDuplicates map[int]int `json:"within_batch_duplicates,omitempty"`

	// Statistics about the deduplication process
	Stats DeduplicationStats `json:"stats"`
}

// DeduplicationStats provides metrics about the deduplication process
type DeduplicationStats struct {
	// TotalCandidates is the number of issues checked
	TotalCandidates int `json:"total_candidates"`

	// UniqueCount is the number of unique issues
	UniqueCount int `json:"unique_count"`

	// DuplicateCount is the number of duplicates found (against existing issues)
	DuplicateCount int `json:"duplicate_count"`

	// WithinBatchDuplicateCount is the number of duplicates within the batch
	WithinBatchDuplicateCount int `json:"within_batch_duplicate_count"`

	// ComparisonsMade is the total number of pairwise comparisons
	ComparisonsMade int `json:"comparisons_made"`

	// AICallsMade is the number of AI API calls made
	AICallsMade int `json:"ai_calls_made"`

	// ProcessingTimeMs is the time taken for deduplication in milliseconds
	ProcessingTimeMs int64 `json:"processing_time_ms"`
}

// Validate checks if the deduplication result has valid values
func (r *DeduplicationResult) Validate() error {
	uniqueCount := len(r.UniqueIssues)
	duplicateCount := len(r.DuplicatePairs)
	withinBatchCount := len(r.WithinBatchDuplicates)

	// Validate stats match actual data
	if r.Stats.UniqueCount != uniqueCount {
		return fmt.Errorf("stats.unique_count (%d) does not match unique_issues length (%d)",
			r.Stats.UniqueCount, uniqueCount)
	}
	if r.Stats.DuplicateCount != duplicateCount {
		return fmt.Errorf("stats.duplicate_count (%d) does not match duplicate_pairs length (%d)",
			r.Stats.DuplicateCount, duplicateCount)
	}
	if r.Stats.WithinBatchDuplicateCount != withinBatchCount {
		return fmt.Errorf("stats.within_batch_duplicate_count (%d) does not match within_batch_duplicates length (%d)",
			r.Stats.WithinBatchDuplicateCount, withinBatchCount)
	}

	// Total should add up
	total := uniqueCount + duplicateCount + withinBatchCount
	if r.Stats.TotalCandidates != total {
		return fmt.Errorf("stats.total_candidates (%d) does not match sum of unique + duplicates + within_batch (%d)",
			r.Stats.TotalCandidates, total)
	}

	// Validate duplicate pairs reference valid indices
	for idx := range r.DuplicatePairs {
		if idx < 0 || idx >= r.Stats.TotalCandidates {
			return fmt.Errorf("duplicate_pairs contains invalid index %d (total: %d)",
				idx, r.Stats.TotalCandidates)
		}
	}

	// Validate within-batch duplicates reference valid indices
	for dupIdx, origIdx := range r.WithinBatchDuplicates {
		if dupIdx < 0 || dupIdx >= r.Stats.TotalCandidates {
			return fmt.Errorf("within_batch_duplicates contains invalid duplicate index %d (total: %d)",
				dupIdx, r.Stats.TotalCandidates)
		}
		if origIdx < 0 || origIdx >= r.Stats.TotalCandidates {
			return fmt.Errorf("within_batch_duplicates contains invalid original index %d (total: %d)",
				origIdx, r.Stats.TotalCandidates)
		}
		if dupIdx <= origIdx {
			return fmt.Errorf("within_batch_duplicates: duplicate index %d must be > original index %d",
				dupIdx, origIdx)
		}
	}

	// Check for overlapping indices between DuplicatePairs and WithinBatchDuplicates
	// An index cannot be both a duplicate of an existing issue AND a within-batch duplicate
	for idx := range r.DuplicatePairs {
		if _, exists := r.WithinBatchDuplicates[idx]; exists {
			return fmt.Errorf("index %d appears in both duplicate_pairs and within_batch_duplicates", idx)
		}
	}

	// Check that within-batch duplicate targets don't appear as duplicates themselves
	// If issue B is a duplicate of issue A (within batch), then A cannot itself be marked as duplicate
	for dupIdx, origIdx := range r.WithinBatchDuplicates {
		// The original cannot be in DuplicatePairs (would mean it's duplicate of existing issue)
		if _, exists := r.DuplicatePairs[origIdx]; exists {
			return fmt.Errorf("within_batch_duplicates references index %d as original, but it appears in duplicate_pairs", origIdx)
		}
		// The original cannot itself be a duplicate in WithinBatchDuplicates
		if _, exists := r.WithinBatchDuplicates[origIdx]; exists {
			return fmt.Errorf("within_batch_duplicates references index %d as original, but it is also a duplicate", origIdx)
		}
		// The duplicate itself cannot also be in DuplicatePairs (checked above, but explicit)
		if _, exists := r.DuplicatePairs[dupIdx]; exists {
			return fmt.Errorf("index %d appears in both duplicate_pairs and within_batch_duplicates", dupIdx)
		}
	}

	return nil
}
