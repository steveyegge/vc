package executor

import (
	"context"
	"fmt"
	"os"

	"github.com/steveyegge/vc/internal/ai"
	"github.com/steveyegge/vc/internal/deduplication"
	"github.com/steveyegge/vc/internal/events"
	"github.com/steveyegge/vc/internal/types"
)

// deduplicateDiscoveredIssues uses the deduplicator to filter out duplicate discovered issues
// Returns the unique issues to create and deduplication statistics
func (rp *ResultsProcessor) deduplicateDiscoveredIssues(ctx context.Context, parentIssue *types.Issue, discovered []ai.DiscoveredIssue) ([]ai.DiscoveredIssue, deduplication.DeduplicationStats) {
	// vc-a80e: Validate batch size to prevent performance issues and API quota exhaustion
	originalCount := len(discovered)
	if originalCount > rp.dedupBatchSize {
		fmt.Fprintf(os.Stderr, 
			"⚠ Warning: discovered issues batch too large (%d > %d), processing first %d\n",
			originalCount, rp.dedupBatchSize, rp.dedupBatchSize)
		
		// Truncate to max batch size
		discovered = discovered[:rp.dedupBatchSize]
		
		// Log event for observability
		rp.logEvent(ctx, events.EventTypeDeduplicationBatchStarted, events.SeverityWarning, 
			parentIssue.ID,
			fmt.Sprintf("Deduplication batch truncated to %d issues", rp.dedupBatchSize),
			map[string]interface{}{
				"original_count": originalCount,
				"truncated_count": rp.dedupBatchSize,
			})
	}

	// Convert discovered issues to types.Issue for deduplication
	candidates := make([]*types.Issue, len(discovered))
	for i, disc := range discovered {
		// Map priority
		priority := 2
		switch disc.Priority {
		case "P0":
			priority = 0
		case "P1":
			priority = 1
		case "P2":
			priority = 2
		case "P3":
			priority = 3
		}

		// Map type
		issueType := types.TypeTask
		switch disc.Type {
		case "bug":
			issueType = types.TypeBug
		case "task":
			issueType = types.TypeTask
		case "feature", "enhancement":
			issueType = types.TypeFeature
		case "epic":
			issueType = types.TypeEpic
		case "chore":
			issueType = types.TypeChore
		}

		candidates[i] = &types.Issue{
			Title:       disc.Title,
			Description: disc.Description,
			IssueType:   issueType,
			Priority:    priority,
			Status:      types.StatusOpen,
		}
	}

	// vc-151: Log deduplication batch started event
	rp.logDeduplicationBatchStarted(ctx, parentIssue.ID, len(candidates), parentIssue.ID)

	// Deduplicate (return early if deduplicator not configured)
	if rp.deduplicator == nil {
		fmt.Fprintf(os.Stderr, "⚠ Deduplication disabled - creating all %d discovered issues\n", len(discovered))
		return discovered, deduplication.DeduplicationStats{
			TotalCandidates: len(discovered),
			UniqueCount:     len(discovered),
		}
	}

	result, err := rp.deduplicator.DeduplicateBatch(ctx, candidates)
	if err != nil {
		// vc-5sxl: Deduplication failed - return empty list to prevent creating invalid issues
		// The error message clearly states that NO issues will be created
		fmt.Fprintf(os.Stderr, "✗ Deduplication failed - NO discovered issues will be created: %v\n", err)
		// vc-151: Log failure
		rp.logDeduplicationBatchCompleted(ctx, parentIssue.ID, nil, err)
		// Return empty list to prevent downstream creation attempts
		return []ai.DiscoveredIssue{}, deduplication.DeduplicationStats{}
	}

	// vc-151: Log deduplication batch completed event with stats and individual decisions
	rp.logDeduplicationBatchCompleted(ctx, parentIssue.ID, result, nil)

	// Build list of unique discovered issues to create
	// We need to map back from unique issues to original DiscoveredIssue objects
	uniqueDiscovered := []ai.DiscoveredIssue{}
	createdSet := make(map[int]bool)

	// Mark which indices were created (unique issues)
	for _, uniqueIssue := range result.UniqueIssues {
		// Find the original index by matching title
		for i, candidate := range candidates {
			if candidate.Title == uniqueIssue.Title && !createdSet[i] {
				uniqueDiscovered = append(uniqueDiscovered, discovered[i])
				createdSet[i] = true
				break
			}
		}
	}

	return uniqueDiscovered, result.Stats
}

// logDeduplicationBatchStarted logs a deduplication batch start event (vc-151)
func (rp *ResultsProcessor) logDeduplicationBatchStarted(ctx context.Context, issueID string, candidateCount int, parentIssueID string) {
	// Skip logging if context is canceled
	if ctx.Err() != nil {
		return
	}

	event, err := events.NewDeduplicationBatchStartedEvent(
		issueID,
		rp.actor,
		"deduplication",
		events.SeverityInfo,
		fmt.Sprintf("Starting deduplication of %d discovered issues for %s", candidateCount, parentIssueID),
		events.DeduplicationBatchStartedData{
			CandidateCount: candidateCount,
			ParentIssueID:  parentIssueID,
		},
	)
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: failed to create deduplication batch started event: %v\n", err)
		return
	}

	if err := rp.store.StoreAgentEvent(ctx, event); err != nil {
		fmt.Fprintf(os.Stderr, "warning: failed to store deduplication batch started event: %v\n", err)
	}
}

// logDeduplicationBatchCompleted logs a deduplication batch completion event with stats and decisions (vc-151)
func (rp *ResultsProcessor) logDeduplicationBatchCompleted(ctx context.Context, issueID string, result *deduplication.DeduplicationResult, err error) {
	// Skip logging if context is canceled
	if ctx.Err() != nil {
		return
	}

	var batchEvent *events.AgentEvent
	var eventErr error

	if err != nil {
		// Deduplication failed
		batchEvent, eventErr = events.NewDeduplicationBatchCompletedEvent(
			issueID,
			rp.actor,
			"deduplication",
			events.SeverityError,
			fmt.Sprintf("Deduplication failed for %s: %v", issueID, err),
			events.DeduplicationBatchCompletedData{
				Success: false,
				Error:   err.Error(),
			},
		)
	} else {
		// Deduplication succeeded
		severity := events.SeverityInfo
		if result.Stats.DuplicateCount > 0 || result.Stats.WithinBatchDuplicateCount > 0 {
			severity = events.SeverityWarning // Duplicates found - worth highlighting
		}

		batchEvent, eventErr = events.NewDeduplicationBatchCompletedEvent(
			issueID,
			rp.actor,
			"deduplication",
			severity,
			fmt.Sprintf("Deduplication completed: %d unique, %d duplicates, %d within-batch duplicates",
				result.Stats.UniqueCount, result.Stats.DuplicateCount, result.Stats.WithinBatchDuplicateCount),
			events.DeduplicationBatchCompletedData{
				TotalCandidates:           result.Stats.TotalCandidates,
				UniqueCount:               result.Stats.UniqueCount,
				DuplicateCount:            result.Stats.DuplicateCount,
				WithinBatchDuplicateCount: result.Stats.WithinBatchDuplicateCount,
				ComparisonsMade:           result.Stats.ComparisonsMade,
				AICallsMade:               result.Stats.AICallsMade,
				ProcessingTimeMs:          result.Stats.ProcessingTimeMs,
				Success:                   true,
			},
		)
	}

	if eventErr != nil {
		fmt.Fprintf(os.Stderr, "warning: failed to create deduplication batch completed event: %v\n", eventErr)
		return
	}

	if err := rp.store.StoreAgentEvent(ctx, batchEvent); err != nil {
		fmt.Fprintf(os.Stderr, "warning: failed to store deduplication batch completed event: %v\n", err)
		return
	}

	// Log individual decision events (for confidence score distribution analysis)
	if result != nil && len(result.Decisions) > 0 {
		for _, decision := range result.Decisions {
			rp.logDeduplicationDecision(ctx, issueID, decision)
		}
	}
}

// logDeduplicationDecision logs an individual deduplication decision (vc-151)
func (rp *ResultsProcessor) logDeduplicationDecision(ctx context.Context, issueID string, decision deduplication.DecisionDetail) {
	// Skip logging if context is canceled
	if ctx.Err() != nil {
		return
	}

	severity := events.SeverityInfo
	var message string

	if decision.IsDuplicate {
		severity = events.SeverityWarning
		if decision.WithinBatchOriginalIndex >= 0 {
			message = fmt.Sprintf("Within-batch duplicate: %s (confidence: %.2f)", decision.CandidateTitle, decision.Confidence)
		} else {
			message = fmt.Sprintf("Duplicate of %s: %s (confidence: %.2f)", decision.DuplicateOf, decision.CandidateTitle, decision.Confidence)
		}
	} else {
		message = fmt.Sprintf("Unique issue: %s (confidence: %.2f)", decision.CandidateTitle, decision.Confidence)
	}

	var withinBatchOriginal string
	if decision.WithinBatchOriginalIndex >= 0 {
		withinBatchOriginal = fmt.Sprintf("candidate_%d", decision.WithinBatchOriginalIndex)
	}

	event, err := events.NewDeduplicationDecisionEvent(
		issueID,
		rp.actor,
		"deduplication",
		severity,
		message,
		events.DeduplicationDecisionData{
			CandidateTitle:       decision.CandidateTitle,
			IsDuplicate:          decision.IsDuplicate,
			DuplicateOf:          decision.DuplicateOf,
			Confidence:           decision.Confidence,
			Reasoning:            decision.Reasoning,
			WithinBatchDuplicate: decision.WithinBatchOriginalIndex >= 0,
			WithinBatchOriginal:  withinBatchOriginal,
		},
	)
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: failed to create deduplication decision event: %v\n", err)
		return
	}

	if err := rp.store.StoreAgentEvent(ctx, event); err != nil {
		fmt.Fprintf(os.Stderr, "warning: failed to store deduplication decision event: %v\n", err)
	}
}
