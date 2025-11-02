package executor

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/steveyegge/vc/internal/codereview"
	"github.com/steveyegge/vc/internal/events"
	"github.com/steveyegge/vc/internal/storage/beads"
	"github.com/steveyegge/vc/internal/types"
)

// checkCodeReviewSweep checks if a code review sweep is needed after completing an issue
// This is the integration of vc-1 into the executor lifecycle
//
// Flow:
// 1. Get git diff metrics since last review checkpoint
// 2. Pass metrics to AI for decision
// 3. If AI decides review is needed, create review issue
// 4. Save checkpoint
//
//nolint:unparam // error return reserved for future use
func (e *Executor) checkCodeReviewSweep(ctx context.Context) error {
	// Only run if AI supervision is enabled (code review requires AI)
	if !e.enableAISupervision || e.supervisor == nil {
		return nil
	}

	// Get VCStorage from storage interface
	vcStorage, ok := e.store.(*beads.VCStorage)
	if !ok {
		// Log warning and skip
		fmt.Fprintf(os.Stderr, "warning: storage is not VCStorage, skipping code review check\n")
		return nil
	}

	// Create sweeper
	sweeper := codereview.NewSweeper(vcStorage)

	// Step 1: Get diff metrics since last checkpoint (includes commit SHA)
	result, err := sweeper.GetDiffMetrics(ctx)
	if err != nil {
		// Log warning but don't fail - review checks are non-critical
		fmt.Fprintf(os.Stderr, "warning: failed to get diff metrics for code review: %v\n", err)
		return nil
	}

	// If no changes, skip
	if result.Metrics.FilesChanged == 0 {
		return nil
	}

	// Step 2: Ask AI if review is needed
	decision, err := e.supervisor.DecideCodeReviewSweep(ctx, result.Metrics)
	if err != nil {
		// Log warning but don't fail
		fmt.Fprintf(os.Stderr, "warning: failed to get AI decision for code review: %v\n", err)
		return nil
	}

	// Log the decision
	e.logEvent(ctx, events.EventTypeCodeReviewDecision, events.SeverityInfo, "",
		fmt.Sprintf("Code review sweep decision: should_review=%v, scope=%s", decision.ShouldReview, decision.Scope),
		map[string]interface{}{
			"should_review":   decision.ShouldReview,
			"reasoning":       decision.Reasoning,
			"scope":           decision.Scope,
			"target_areas":    decision.TargetAreas,
			"estimated_files": decision.EstimatedFiles,
			"estimated_cost":  decision.EstimatedCost,
		})

	// Step 3: If AI says yes, create review issue
	if decision.ShouldReview {
		reviewIssueID, err := sweeper.CreateReviewIssue(ctx, decision)
		if err != nil {
			// Log error but don't fail the main workflow
			fmt.Fprintf(os.Stderr, "warning: failed to create code review issue: %v\n", err)
			return nil
		}

		fmt.Printf("✓ Created code review sweep issue: %s (scope=%s, estimated_files=%d)\n",
			reviewIssueID, decision.Scope, decision.EstimatedFiles)

		// Step 4: Save checkpoint with the commit SHA that was used for metrics
		// Use result.CommitSHA to prevent race conditions (vc-8093)
		checkpoint := &types.ReviewCheckpoint{
			CommitSHA:   result.CommitSHA, // Use same SHA from metrics calculation
			Timestamp:   time.Now(),
			ReviewScope: decision.Scope,
		}

		if err := vcStorage.SaveReviewCheckpoint(ctx, checkpoint, reviewIssueID); err != nil {
			// Log warning but don't fail
			fmt.Fprintf(os.Stderr, "warning: failed to save review checkpoint: %v\n", err)
		}

		e.logEvent(ctx, events.EventTypeCodeReviewCreated, events.SeverityInfo, reviewIssueID,
			fmt.Sprintf("Created code review sweep issue %s", reviewIssueID),
			map[string]interface{}{
				"review_issue_id": reviewIssueID,
				"scope":           decision.Scope,
				"estimated_files": decision.EstimatedFiles,
			})
	} else {
		fmt.Printf("✓ Code review sweep: AI decided review not needed yet (reasoning: %s)\n", decision.Reasoning)
	}

	return nil
}

// getCurrentCommitSHA has been removed (vc-8093)
// Now using result.CommitSHA from GetDiffMetrics() to prevent race conditions
// and ensure we save the actual commit SHA (not "HEAD") that was used for metrics
