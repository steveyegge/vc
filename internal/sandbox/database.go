package sandbox

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"github.com/steveyegge/vc/internal/deduplication"
	"github.com/steveyegge/vc/internal/events"
	"github.com/steveyegge/vc/internal/storage"
	"github.com/steveyegge/vc/internal/types"
)

// SandboxMetadata tracks the provenance and relationship to the main database
type SandboxMetadata struct {
	SandboxID    string    `json:"sandbox_id"`
	ParentDBPath string    `json:"parent_db_path"`
	MissionID    string    `json:"mission_id"`
	CreatedAt    time.Time `json:"created_at"`
}

// initSandboxDB creates and initializes a beads database for the sandbox.
// It creates a .beads/mission.db file in the sandbox path and initializes
// it with the proper schema and metadata.
//
// Returns the absolute path to the created database file.
func initSandboxDB(ctx context.Context, sandboxPath, missionID, parentDBPath string) (string, error) {
	// Validate inputs
	if sandboxPath == "" {
		return "", fmt.Errorf("sandboxPath cannot be empty")
	}
	if missionID == "" {
		return "", fmt.Errorf("missionID cannot be empty")
	}

	// Create .beads directory in sandbox
	beadsDir := filepath.Join(sandboxPath, ".beads")
	if err := os.MkdirAll(beadsDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create .beads directory: %w", err)
	}

	// Database path
	dbPath := filepath.Join(beadsDir, "mission.db")

	// Create storage config
	cfg := &storage.Config{
		Path: dbPath,
	}

	// Create and initialize the database (schema is created automatically)
	store, err := storage.NewStorage(ctx, cfg)
	if err != nil {
		return "", fmt.Errorf("failed to create sandbox database: %w", err)
	}

	// Set issue_prefix to 'vc' to match the main database
	// This prevents sandbox-created issues from using the wrong prefix
	if err := store.SetConfig(ctx, "issue_prefix", "vc"); err != nil {
		if closeErr := store.Close(); closeErr != nil {
			log.Printf("warning: failed to close store after config error: %v", closeErr)
		}
		return "", fmt.Errorf("failed to set issue_prefix config: %w", err)
	}

	// Close the storage connection before opening a raw connection for metadata
	if err := store.Close(); err != nil {
		log.Printf("warning: failed to close store: %v", err)
	}

	// Store metadata in a custom table
	if err := storeSandboxMetadata(ctx, dbPath, missionID, parentDBPath); err != nil {
		return "", fmt.Errorf("failed to store sandbox metadata: %w", err)
	}

	return dbPath, nil
}

// storeSandboxMetadata creates a metadata table and stores sandbox provenance information
func storeSandboxMetadata(ctx context.Context, dbPath, missionID, parentDBPath string) error {
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}
	defer func() {
		if err := db.Close(); err != nil {
			log.Printf("warning: failed to close database: %v", err)
		}
	}()

	// Create metadata table
	_, err = db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS sandbox_metadata (
			key TEXT PRIMARY KEY,
			value TEXT NOT NULL,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		)
	`)
	if err != nil {
		return fmt.Errorf("failed to create metadata table: %w", err)
	}

	// Store metadata
	now := time.Now()
	metadata := SandboxMetadata{
		SandboxID:    fmt.Sprintf("sandbox-%s-%d", missionID, now.Unix()),
		ParentDBPath: parentDBPath,
		MissionID:    missionID,
		CreatedAt:    now,
	}

	metadataJSON, err := json.Marshal(metadata)
	if err != nil {
		return fmt.Errorf("failed to marshal metadata: %w", err)
	}

	_, err = db.ExecContext(ctx, `
		INSERT OR REPLACE INTO sandbox_metadata (key, value)
		VALUES (?, ?)
	`, "sandbox_info", string(metadataJSON))
	if err != nil {
		return fmt.Errorf("failed to insert metadata: %w", err)
	}

	return nil
}

// copyCoreIssues copies the mission issue and all its blocking dependencies
// from the main database to the sandbox database. This includes:
// - The mission issue itself
// - All issues that block the mission (recursively)
// - All child issues of the mission
// - All dependencies and labels associated with these issues
func copyCoreIssues(ctx context.Context, mainDB, sandboxDB storage.Storage, missionID string) error {
	// Validate inputs
	if missionID == "" {
		return fmt.Errorf("missionID cannot be empty")
	}
	if mainDB == nil || sandboxDB == nil {
		return fmt.Errorf("storage instances cannot be nil")
	}

	// Get the mission issue
	mission, err := mainDB.GetIssue(ctx, missionID)
	if err != nil {
		return fmt.Errorf("failed to get mission %s: %w", missionID, err)
	}
	if mission == nil {
		return fmt.Errorf("mission %s not found", missionID)
	}

	// Track visited issues to avoid duplicates
	visited := make(map[string]bool)
	issuesToCopy := []*types.Issue{}

	// Recursively collect all dependencies (with depth limit to prevent pathological cases)
	if err := collectDependenciesRecursive(ctx, mainDB, missionID, visited, &issuesToCopy, 0); err != nil {
		return fmt.Errorf("failed to collect dependencies: %w", err)
	}

	// Collect all child issues
	children, err := mainDB.GetDependents(ctx, missionID)
	if err != nil {
		return fmt.Errorf("failed to get child issues: %w", err)
	}
	for _, child := range children {
		if !visited[child.ID] {
			issuesToCopy = append(issuesToCopy, child)
			visited[child.ID] = true
		}
	}

	// Copy all collected issues to sandbox DB
	copiedIDs := make(map[string]bool)
	for _, issue := range issuesToCopy {
		// Skip if already copied (defensive check for duplicates in issuesToCopy)
		if copiedIDs[issue.ID] {
			continue
		}

		if err := sandboxDB.CreateIssue(ctx, issue, "sandbox-init"); err != nil {
			return fmt.Errorf("failed to copy issue %s: %w", issue.ID, err)
		}
		copiedIDs[issue.ID] = true

		// Copy labels
		labels, err := mainDB.GetLabels(ctx, issue.ID)
		if err != nil {
			return fmt.Errorf("failed to get labels for %s: %w", issue.ID, err)
		}
		for _, label := range labels {
			if err := sandboxDB.AddLabel(ctx, issue.ID, label, "sandbox-init"); err != nil {
				return fmt.Errorf("failed to copy label %s for %s: %w", label, issue.ID, err)
			}
		}
	}

	// Copy dependency records
	for issueID := range visited {
		deps, err := mainDB.GetDependencyRecords(ctx, issueID)
		if err != nil {
			return fmt.Errorf("failed to get dependencies for %s: %w", issueID, err)
		}
		for _, dep := range deps {
			// Only copy if both ends are in the sandbox
			if visited[dep.IssueID] && visited[dep.DependsOnID] {
				if err := sandboxDB.AddDependency(ctx, dep, "sandbox-init"); err != nil {
					return fmt.Errorf("failed to copy dependency %s -> %s: %w",
						dep.IssueID, dep.DependsOnID, err)
				}
			}
		}
	}

	return nil
}

// Maximum depth for dependency traversal to prevent pathological cases
const maxDependencyDepth = 50

// collectDependenciesRecursive recursively collects all issues that the given issue depends on
func collectDependenciesRecursive(ctx context.Context, db storage.Storage, issueID string,
	visited map[string]bool, result *[]*types.Issue, depth int) error {

	// Prevent excessive recursion depth
	if depth > maxDependencyDepth {
		return fmt.Errorf("maximum dependency depth (%d) exceeded for issue %s (possible cycle or pathological chain)",
			maxDependencyDepth, issueID)
	}

	// Skip if already visited
	if visited[issueID] {
		return nil
	}
	visited[issueID] = true

	// Get the issue
	issue, err := db.GetIssue(ctx, issueID)
	if err != nil {
		return fmt.Errorf("failed to get issue %s: %w", issueID, err)
	}
	if issue == nil {
		return fmt.Errorf("issue %s not found", issueID)
	}

	// Add to result
	*result = append(*result, issue)

	// Get dependencies (issues this one depends on)
	deps, err := db.GetDependencies(ctx, issueID)
	if err != nil {
		return fmt.Errorf("failed to get dependencies for %s: %w", issueID, err)
	}

	// Recursively collect dependencies
	for _, dep := range deps {
		if err := collectDependenciesRecursive(ctx, db, dep.ID, visited, result, depth+1); err != nil {
			return err
		}
	}

	return nil
}

// Maximum number of events to merge from sandbox execution
const maxEventsToMerge = 100

// mergeResults merges completed work from the sandbox database back to the main database.
// This includes:
// - Status updates for issues that were worked on
// - New issues that were discovered during execution (after deduplication)
// - Comments and events from the sandbox
// - Execution history
//
// The deduplicator parameter is optional. If nil, all discovered issues will be filed without
// deduplication (fail-safe behavior). If provided, discovered issues will be deduplicated
// against recent open issues in the main database before filing.
//
// Note: This does NOT merge code changes - those are handled by git operations.
func mergeResults(ctx context.Context, sandboxDB, mainDB storage.Storage, missionID string, deduplicator deduplication.Deduplicator) error {
	// Get the mission from sandbox to see its final state
	sandboxMission, err := sandboxDB.GetIssue(ctx, missionID)
	if err != nil {
		return fmt.Errorf("failed to get sandbox mission: %w", err)
	}
	if sandboxMission == nil {
		return fmt.Errorf("mission %s not found in sandbox", missionID)
	}

	// Update mission status in main DB if it changed
	mainMission, err := mainDB.GetIssue(ctx, missionID)
	if err != nil {
		return fmt.Errorf("failed to get main mission: %w", err)
	}
	if mainMission == nil {
		return fmt.Errorf("mission %s not found in main database", missionID)
	}

	// Update status if it changed
	if sandboxMission.Status != mainMission.Status {
		// Use CloseIssue if the sandbox mission was closed
		if sandboxMission.Status == types.StatusClosed {
			closeReason := "Completed in sandbox execution"
			if err := mainDB.CloseIssue(ctx, missionID, closeReason, "sandbox-merge"); err != nil {
				return fmt.Errorf("failed to close mission: %w", err)
			}
		} else {
			// For other status changes, use UpdateIssue
			updates := map[string]interface{}{
				"status": sandboxMission.Status,
			}
			if err := mainDB.UpdateIssue(ctx, missionID, updates, "sandbox-merge"); err != nil {
				return fmt.Errorf("failed to update mission status: %w", err)
			}
		}
	}

	// Merge any new issues created in the sandbox (discovered issues, follow-up tasks, etc.)
	// These would be issues that exist in sandbox but not in main DB
	sandboxIssues, err := sandboxDB.SearchIssues(ctx, "", types.IssueFilter{})
	if err != nil {
		return fmt.Errorf("failed to search sandbox issues: %w", err)
	}

	// Collect all discovered issues (issues in sandbox that don't exist in main DB)
	var candidateDiscoveredIssues []*types.Issue
	for _, sandboxIssue := range sandboxIssues {
		// Skip the original mission
		if sandboxIssue.ID == missionID {
			continue
		}

		// Check if issue exists in main DB
		mainIssue, err := mainDB.GetIssue(ctx, sandboxIssue.ID)
		if err != nil {
			return fmt.Errorf("failed to check issue %s in main DB: %w", sandboxIssue.ID, err)
		}

		// If issue doesn't exist in main DB, it's a discovered issue
		if mainIssue == nil {
			candidateDiscoveredIssues = append(candidateDiscoveredIssues, sandboxIssue)
		}
	}

	// Run deduplication on discovered issues if deduplicator is provided
	var issuesToFile []*types.Issue
	var dedupStats *deduplication.DeduplicationStats
	if deduplicator != nil && len(candidateDiscoveredIssues) > 0 {
		log.Printf("[SANDBOX] Running deduplication on %d discovered issues", len(candidateDiscoveredIssues))

		// vc-151: Log deduplication batch started event
		logSandboxDeduplicationBatchStarted(ctx, mainDB, missionID, len(candidateDiscoveredIssues))

		result, err := deduplicator.DeduplicateBatch(ctx, candidateDiscoveredIssues)
		if err != nil {
			// Fail-safe: if deduplication fails, file all issues with warning
			log.Printf("[SANDBOX] WARNING: Deduplication failed (%v), filing all issues", err)
			// vc-151: Log deduplication failure
			logSandboxDeduplicationBatchCompleted(ctx, mainDB, missionID, nil, err)
			issuesToFile = candidateDiscoveredIssues
		} else {
			issuesToFile = result.UniqueIssues
			dedupStats = &result.Stats

			// Log deduplication results
			log.Printf("[SANDBOX] Deduplication complete: %d unique, %d duplicates, %d within-batch duplicates",
				result.Stats.UniqueCount, result.Stats.DuplicateCount, result.Stats.WithinBatchDuplicateCount)

			// vc-151: Log deduplication success with stats and decisions
			logSandboxDeduplicationBatchCompleted(ctx, mainDB, missionID, result, nil)

			// Add cross-reference comments for duplicates
			for idx, existingID := range result.DuplicatePairs {
				candidate := candidateDiscoveredIssues[idx]
				comment := fmt.Sprintf("Skipped filing duplicate issue during sandbox merge: '%s' (duplicate of %s)",
					candidate.Title, existingID)
				if err := mainDB.AddComment(ctx, existingID, "sandbox-dedup", comment); err != nil {
					log.Printf("[SANDBOX] WARNING: Failed to add cross-reference comment to %s: %v", existingID, err)
				}
			}

			// Add comments for within-batch duplicates
			for dupIdx, origIdx := range result.WithinBatchDuplicates {
				duplicate := candidateDiscoveredIssues[dupIdx]
				original := candidateDiscoveredIssues[origIdx]
				log.Printf("[SANDBOX] Within-batch duplicate: '%s' is duplicate of '%s'",
					duplicate.Title, original.Title)
			}
		}
	} else if deduplicator == nil {
		log.Printf("[SANDBOX] No deduplicator provided, filing all %d discovered issues", len(candidateDiscoveredIssues))
		issuesToFile = candidateDiscoveredIssues
	} else {
		// No discovered issues to deduplicate
		issuesToFile = candidateDiscoveredIssues
	}

	// Log final statistics
	if dedupStats != nil {
		log.Printf("[SANDBOX] Dedup stats: %d candidates -> %d filed, %d duplicates skipped (processing time: %dms)",
			dedupStats.TotalCandidates, dedupStats.UniqueCount,
			dedupStats.DuplicateCount+dedupStats.WithinBatchDuplicateCount,
			dedupStats.ProcessingTimeMs)
	}

	// Track discovered issues to add dependencies in second pass
	discoveredIssues := make(map[string]*types.Issue)

	// First pass: Create deduplicated discovered issues and their labels
	for _, sandboxIssue := range issuesToFile {
		// Save the old sandbox ID before clearing it
		oldSandboxID := sandboxIssue.ID

		// Clear the ID to force fresh ID generation in main DB
		// This prevents collisions between sandbox-generated IDs and main DB IDs
		sandboxIssue.ID = ""

		if err := mainDB.CreateIssue(ctx, sandboxIssue, "sandbox-discovered"); err != nil {
			return fmt.Errorf("failed to create discovered issue (was %s): %w", oldSandboxID, err)
		}

		// Track this as a discovered issue with its new ID
		// sandboxIssue.ID is now the newly generated ID from main DB
		discoveredIssues[sandboxIssue.ID] = sandboxIssue

		// Copy labels from the sandbox issue (using old ID) to the new issue (using new ID)
		labels, err := sandboxDB.GetLabels(ctx, oldSandboxID)
		if err != nil {
			return fmt.Errorf("failed to get labels for %s: %w", oldSandboxID, err)
		}
		for _, label := range labels {
			if err := mainDB.AddLabel(ctx, sandboxIssue.ID, label, "sandbox-discovered"); err != nil {
				return fmt.Errorf("failed to add label %s to %s: %w", label, sandboxIssue.ID, err)
			}
		}
	}

	// Update status for existing issues that were worked on in the sandbox
	for _, sandboxIssue := range sandboxIssues {
		// Skip the mission (already handled above)
		if sandboxIssue.ID == missionID {
			continue
		}

		// Check if this is an existing issue (not a discovered one)
		mainIssue, err := mainDB.GetIssue(ctx, sandboxIssue.ID)
		if err != nil {
			return fmt.Errorf("failed to check issue %s in main DB: %w", sandboxIssue.ID, err)
		}

		// If issue exists and status changed, update it
		if mainIssue != nil && mainIssue.Status != sandboxIssue.Status {
			updates := map[string]interface{}{
				"status": sandboxIssue.Status,
			}
			if err := mainDB.UpdateIssue(ctx, sandboxIssue.ID, updates, "sandbox-merge"); err != nil {
				return fmt.Errorf("failed to update issue %s status: %w", sandboxIssue.ID, err)
			}
		}
	}

	// Second pass: Add dependencies for discovered issues
	// This ensures all issues exist before we try to create dependencies between them
	for issueID := range discoveredIssues {
		deps, err := sandboxDB.GetDependencyRecords(ctx, issueID)
		if err != nil {
			return fmt.Errorf("failed to get dependencies for %s: %w", issueID, err)
		}

		for _, dep := range deps {
			// Check if both ends of the dependency exist in main DB
			issueExists, err := mainDB.GetIssue(ctx, dep.IssueID)
			if err != nil {
				return fmt.Errorf("failed to check issue %s exists: %w", dep.IssueID, err)
			}

			targetExists, err := mainDB.GetIssue(ctx, dep.DependsOnID)
			if err != nil {
				return fmt.Errorf("failed to check dependency target %s exists: %w", dep.DependsOnID, err)
			}

			// Only create dependency if both ends exist
			if issueExists != nil && targetExists != nil {
				if err := mainDB.AddDependency(ctx, dep, "sandbox-discovered"); err != nil {
					// Ignore if dependency already exists (might happen if issue was pre-existing)
					// TODO: Consider checking for specific "already exists" error
					continue
				}
			}
		}
	}

	// Copy comments/events from sandbox that reference the mission
	sandboxEvents, err := sandboxDB.GetEvents(ctx, missionID, maxEventsToMerge)
	if err != nil {
		return fmt.Errorf("failed to get sandbox events: %w", err)
	}

	for _, event := range sandboxEvents {
		// Skip creation events and events from sandbox-init
		if event.EventType == types.EventCreated || event.Actor == "sandbox-init" {
			continue
		}

		// Add comment to main DB summarizing what happened in sandbox
		if event.Comment != nil {
			comment := fmt.Sprintf("[Sandbox execution] %s", *event.Comment)
			if err := mainDB.AddComment(ctx, missionID, "sandbox-merge", comment); err != nil {
				return fmt.Errorf("failed to add sandbox comment: %w", err)
			}
		}
	}

	return nil
}

// logSandboxDeduplicationBatchStarted logs a deduplication batch start event from sandbox merge (vc-151)
func logSandboxDeduplicationBatchStarted(ctx context.Context, store storage.Storage, issueID string, candidateCount int) {
	// Skip logging if context is canceled
	if ctx.Err() != nil {
		return
	}

	event, err := events.NewDeduplicationBatchStartedEvent(
		issueID,
		"sandbox-dedup",
		"deduplication",
		events.SeverityInfo,
		fmt.Sprintf("[Sandbox merge] Starting deduplication of %d discovered issues", candidateCount),
		events.DeduplicationBatchStartedData{
			CandidateCount: candidateCount,
			ParentIssueID:  issueID,
		},
	)
	if err != nil {
		log.Printf("[SANDBOX] warning: failed to create deduplication batch started event: %v", err)
		return
	}

	if err := store.StoreAgentEvent(ctx, event); err != nil {
		log.Printf("[SANDBOX] warning: failed to store deduplication batch started event: %v", err)
	}
}

// logSandboxDeduplicationBatchCompleted logs a deduplication batch completion event from sandbox merge (vc-151)
func logSandboxDeduplicationBatchCompleted(ctx context.Context, store storage.Storage, issueID string, result *deduplication.DeduplicationResult, dedupErr error) {
	// Skip logging if context is canceled
	if ctx.Err() != nil {
		return
	}

	var batchEvent *events.AgentEvent
	var eventErr error

	if dedupErr != nil {
		// Deduplication failed
		batchEvent, eventErr = events.NewDeduplicationBatchCompletedEvent(
			issueID,
			"sandbox-dedup",
			"deduplication",
			events.SeverityError,
			fmt.Sprintf("[Sandbox merge] Deduplication failed: %v", dedupErr),
			events.DeduplicationBatchCompletedData{
				Success: false,
				Error:   dedupErr.Error(),
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
			"sandbox-dedup",
			"deduplication",
			severity,
			fmt.Sprintf("[Sandbox merge] Deduplication completed: %d unique, %d duplicates, %d within-batch duplicates",
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
		log.Printf("[SANDBOX] warning: failed to create deduplication batch completed event: %v", eventErr)
		return
	}

	if err := store.StoreAgentEvent(ctx, batchEvent); err != nil {
		log.Printf("[SANDBOX] warning: failed to store deduplication batch completed event: %v", err)
		return
	}

	// Log individual decision events (for confidence score distribution analysis)
	if result != nil && len(result.Decisions) > 0 {
		for _, decision := range result.Decisions {
			logSandboxDeduplicationDecision(ctx, store, issueID, decision)
		}
	}
}

// logSandboxDeduplicationDecision logs an individual deduplication decision from sandbox merge (vc-151)
func logSandboxDeduplicationDecision(ctx context.Context, store storage.Storage, issueID string, decision deduplication.DecisionDetail) {
	// Skip logging if context is canceled
	if ctx.Err() != nil {
		return
	}

	severity := events.SeverityInfo
	var message string

	if decision.IsDuplicate {
		severity = events.SeverityWarning
		if decision.WithinBatchOriginalIndex >= 0 {
			message = fmt.Sprintf("[Sandbox merge] Within-batch duplicate: %s (confidence: %.2f)", decision.CandidateTitle, decision.Confidence)
		} else {
			message = fmt.Sprintf("[Sandbox merge] Duplicate of %s: %s (confidence: %.2f)", decision.DuplicateOf, decision.CandidateTitle, decision.Confidence)
		}
	} else {
		message = fmt.Sprintf("[Sandbox merge] Unique issue: %s (confidence: %.2f)", decision.CandidateTitle, decision.Confidence)
	}

	var withinBatchOriginal string
	if decision.WithinBatchOriginalIndex >= 0 {
		withinBatchOriginal = fmt.Sprintf("candidate_%d", decision.WithinBatchOriginalIndex)
	}

	event, err := events.NewDeduplicationDecisionEvent(
		issueID,
		"sandbox-dedup",
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
		log.Printf("[SANDBOX] warning: failed to create deduplication decision event: %v", err)
		return
	}

	if err := store.StoreAgentEvent(ctx, event); err != nil {
		log.Printf("[SANDBOX] warning: failed to store deduplication decision event: %v", err)
	}
}
