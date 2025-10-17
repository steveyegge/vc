package sandbox

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "github.com/mattn/go-sqlite3"
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
	// Close the storage connection before opening a raw connection for metadata
	store.Close()

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
	defer db.Close()

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
	for _, issue := range issuesToCopy {
		if err := sandboxDB.CreateIssue(ctx, issue, "sandbox-init"); err != nil {
			return fmt.Errorf("failed to copy issue %s: %w", issue.ID, err)
		}

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
// - New issues that were discovered during execution
// - Comments and events from the sandbox
// - Execution history
//
// Note: This does NOT merge code changes - those are handled by git operations.
func mergeResults(ctx context.Context, sandboxDB, mainDB storage.Storage, missionID string) error {
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
		updates := map[string]interface{}{
			"status": sandboxMission.Status,
		}
		if err := mainDB.UpdateIssue(ctx, missionID, updates, "sandbox-merge"); err != nil {
			return fmt.Errorf("failed to update mission status: %w", err)
		}
	}

	// Merge any new issues created in the sandbox (discovered issues, follow-up tasks, etc.)
	// These would be issues that exist in sandbox but not in main DB
	sandboxIssues, err := sandboxDB.SearchIssues(ctx, "", types.IssueFilter{})
	if err != nil {
		return fmt.Errorf("failed to search sandbox issues: %w", err)
	}

	// Track discovered issues to add dependencies in second pass
	discoveredIssues := make(map[string]*types.Issue)

	// First pass: Create all discovered issues and their labels, update existing issues
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

		// If issue doesn't exist in main DB, it's a discovered issue - create it
		if mainIssue == nil {
			if err := mainDB.CreateIssue(ctx, sandboxIssue, "sandbox-discovered"); err != nil {
				return fmt.Errorf("failed to create discovered issue %s: %w", sandboxIssue.ID, err)
			}

			// Track this as a discovered issue
			discoveredIssues[sandboxIssue.ID] = sandboxIssue

			// Copy labels for the new issue
			labels, err := sandboxDB.GetLabels(ctx, sandboxIssue.ID)
			if err != nil {
				return fmt.Errorf("failed to get labels for %s: %w", sandboxIssue.ID, err)
			}
			for _, label := range labels {
				if err := mainDB.AddLabel(ctx, sandboxIssue.ID, label, "sandbox-discovered"); err != nil {
					return fmt.Errorf("failed to add label %s to %s: %w", label, sandboxIssue.ID, err)
				}
			}
		} else if mainIssue.Status != sandboxIssue.Status {
			// Issue exists but status changed - update it
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
