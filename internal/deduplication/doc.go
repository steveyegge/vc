// Package deduplication provides AI-powered duplicate detection for issues.
//
// # Overview
//
// The deduplication package prevents multiple workers from filing duplicate issues
// when they encounter the same problems during execution. It uses AI-powered semantic
// similarity analysis to detect duplicates with high confidence.
//
// # Architecture
//
// The deduplication engine works in two modes:
//
//  1. Single issue check (CheckDuplicate): Compare one candidate against recent issues
//  2. Batch processing (DeduplicateBatch): Compare multiple candidates efficiently
//
// Both modes use the Supervisor AI to perform semantic similarity analysis, considering:
//   - Title similarity
//   - File/line references in descriptions
//   - Parent issue context
//   - Issue type and priority
//
// # Integration Points
//
// Sandbox Mode (current implementation):
//   - Discovered issues filed to sandbox DB (.beads/mission.db)
//   - Deduplication runs AFTER quality gates, BEFORE mergeResults()
//   - Prevents duplicates when merging sandbox issues to main DB
//
// Non-Sandbox Mode (future enhancement):
//   - Discovered issues collected in memory
//   - Deduplication runs AFTER AI analysis, BEFORE CreateDiscoveredIssues()
//   - Prevents duplicates when filing directly to main DB
//
// # Configuration
//
// The default configuration is conservative to prevent false positives:
//   - ConfidenceThreshold: 0.85 (high confidence required to mark as duplicate)
//   - LookbackWindow: 7 days (check recent open issues only)
//   - MaxCandidates: 50 (limit comparisons for performance)
//   - FailOpen: true (file the issue if deduplication fails)
//
// See DefaultConfig() for full default values.
//
// # Usage Examples
//
// Basic single issue check:
//
//	import (
//	    "github.com/steveyegge/vc/internal/deduplication"
//	    "github.com/steveyegge/vc/internal/ai"
//	)
//
//	// Create deduplicator
//	supervisor := ai.NewSupervisor(client, store, model, retryConfig)
//	dedup := deduplication.NewAIDeduplicator(supervisor, store, deduplication.DefaultConfig())
//
//	// Check if issue is duplicate
//	decision, err := dedup.CheckDuplicate(ctx, candidateIssue)
//	if err != nil {
//	    if config.FailOpen {
//	        log.Printf("Dedup failed, filing issue anyway: %v", err)
//	        // File the issue
//	    } else {
//	        return fmt.Errorf("dedup check failed: %w", err)
//	    }
//	}
//
//	if decision.IsDuplicate {
//	    log.Printf("Skipping duplicate issue (matches %s, confidence %.2f)",
//	        decision.DuplicateOf, decision.Confidence)
//	    // Add cross-reference comment instead of filing
//	    store.CreateEvent(ctx, &types.Event{
//	        IssueID: decision.DuplicateOf,
//	        EventType: types.EventCommented,
//	        Comment: strPtr(fmt.Sprintf("Duplicate detected: %s", candidateIssue.Title)),
//	    })
//	} else {
//	    // File the issue
//	    store.CreateIssue(ctx, candidateIssue)
//	}
//
// Batch deduplication:
//
//	// Deduplicate multiple issues efficiently
//	result, err := dedup.DeduplicateBatch(ctx, discoveredIssues)
//	if err != nil {
//	    if config.FailOpen {
//	        log.Printf("Batch dedup failed, filing all issues: %v", err)
//	        result = &DeduplicationResult{UniqueIssues: discoveredIssues}
//	    } else {
//	        return fmt.Errorf("batch dedup failed: %w", err)
//	    }
//	}
//
//	// File unique issues
//	for _, issue := range result.UniqueIssues {
//	    store.CreateIssue(ctx, issue)
//	}
//
//	// Add cross-references for duplicates
//	for _, existingID := range result.DuplicatePairs {
//	    store.CreateEvent(ctx, &types.Event{
//	        IssueID: existingID,
//	        EventType: types.EventCommented,
//	        Comment: strPtr("Duplicate issue detected and merged"),
//	    })
//	}
//
//	// Log statistics
//	log.Printf("Dedup stats: %d candidates, %d unique, %d duplicates, %d within-batch",
//	    result.Stats.TotalCandidates, result.Stats.UniqueCount,
//	    result.Stats.DuplicateCount, result.Stats.WithinBatchDuplicateCount)
//
// Custom configuration:
//
//	config := deduplication.DefaultConfig()
//	config.ConfidenceThreshold = 0.90  // More conservative
//	config.LookbackWindow = 14 * 24 * time.Hour  // 14 days
//	config.FailOpen = false  // Block on errors
//	dedup := deduplication.NewAIDeduplicator(supervisor, store, config)
//
// # Design Principles
//
// Zero Framework Cognition (ZFC):
//   - No heuristics or regex-based duplicate detection
//   - AI makes all similarity judgments
//   - Framework only handles data flow and error cases
//
// Fail-Safe by Default:
//   - If deduplication fails, file the issue anyway (FailOpen: true)
//   - Better to have a duplicate than lose discovered work
//   - All failures are logged for investigation
//
// Transparent Decisions:
//   - All duplicate decisions include reasoning from AI
//   - Cross-reference comments explain why issues were merged
//   - Statistics track deduplication effectiveness
//
// Efficient Processing:
//   - Batch API calls to minimize costs
//   - Within-batch deduplication reduces redundant comparisons
//   - Configurable limits prevent unbounded processing
//
// # Performance Considerations
//
// The deduplication engine makes AI API calls, which have cost and latency implications:
//
//   - Single check: 1 API call per existing issue (up to MaxCandidates)
//   - Batch check: Optimized to batch comparisons (BatchSize per API call)
//   - Typical batch: 5-10 candidates × 20-30 existing issues = 100-300 comparisons
//   - With BatchSize=10: 10-30 API calls for full batch
//
// For cost control:
//   - Reduce MaxCandidates (fewer comparisons per candidate)
//   - Reduce LookbackWindow (fewer existing issues to compare)
//   - Increase BatchSize (fewer API calls, but longer per call)
//
// For latency control:
//   - Reduce BatchSize (faster individual calls, more parallel)
//   - Set shorter RequestTimeout
//   - Enable circuit breaker in Supervisor config
//
// # Error Handling
//
// The deduplicator distinguishes between recoverable and fatal errors:
//
// Recoverable (with FailOpen=true):
//   - AI API timeouts
//   - Temporary storage failures
//   - Invalid confidence scores from AI
//
// Fatal (always error):
//   - Invalid configuration
//   - Nil candidate issues
//   - Invalid candidate issue data
//
// When FailOpen=true and a recoverable error occurs:
//   - Error is logged with context
//   - Empty DuplicateDecision returned (IsDuplicate=false)
//   - Caller should file the issue to avoid losing work
//
// # Testing
//
// The package includes comprehensive tests:
//   - Unit tests for data structure validation
//   - Integration tests with mock AI responses
//   - Fail-safe behavior tests
//   - Configuration validation tests
//
// See deduplicator_test.go for examples.
package deduplication
