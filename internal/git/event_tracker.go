package git

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/steveyegge/vc/internal/events"
	"github.com/steveyegge/vc/internal/storage"
)

// EventTracker wraps GitOperations and emits events to the event store
type EventTracker struct {
	git        GitOperations
	store      storage.Storage
	issueID    string
	executorID string
	agentID    string
}

// EventTrackerConfig holds configuration for the event tracker
type EventTrackerConfig struct {
	Git        GitOperations
	Store      storage.Storage
	IssueID    string
	ExecutorID string
	AgentID    string
}

// NewEventTracker creates a new git event tracker
func NewEventTracker(cfg *EventTrackerConfig) (*EventTracker, error) {
	if cfg.Git == nil {
		return nil, fmt.Errorf("git operations required")
	}
	if cfg.Store == nil {
		return nil, fmt.Errorf("storage required")
	}
	if cfg.IssueID == "" {
		return nil, fmt.Errorf("issue ID required")
	}

	return &EventTracker{
		git:        cfg.Git,
		store:      cfg.Store,
		issueID:    cfg.IssueID,
		executorID: cfg.ExecutorID,
		agentID:    cfg.AgentID,
	}, nil
}

// HasUncommittedChanges checks if there are uncommitted changes and tracks the operation
func (et *EventTracker) HasUncommittedChanges(ctx context.Context, repoPath string) (bool, error) {
	hasChanges, err := et.git.HasUncommittedChanges(ctx, repoPath)

	// Track this as a status check operation
	_ = et.emitEvent(ctx, events.SeverityInfo, "Git status check", map[string]interface{}{
		"command":     "git",
		"args":        []string{"status", "--porcelain"},
		"success":     err == nil,
		"has_changes": hasChanges,
	})

	return hasChanges, err
}

// GetStatus returns the git status and tracks the operation
func (et *EventTracker) GetStatus(ctx context.Context, repoPath string) (*Status, error) {
	status, err := et.git.GetStatus(ctx, repoPath)

	// Track git status operation
	eventData := map[string]interface{}{
		"command": "git",
		"args":    []string{"status", "--porcelain"},
		"success": err == nil,
	}
	if status != nil {
		eventData["has_changes"] = status.HasChanges
		eventData["modified"] = len(status.Modified)
		eventData["added"] = len(status.Added)
		eventData["deleted"] = len(status.Deleted)
		eventData["untracked"] = len(status.Untracked)
	}

	_ = et.emitEvent(ctx, events.SeverityInfo, "Git status", eventData)

	return status, err
}

// CommitChanges creates a git commit and tracks the operation
func (et *EventTracker) CommitChanges(ctx context.Context, repoPath string, opts CommitOptions) (string, error) {
	commitHash, err := et.git.CommitChanges(ctx, repoPath, opts)

	// Track git commit operation
	severity := events.SeverityInfo
	message := "Git commit successful"
	if err != nil {
		severity = events.SeverityError
		message = fmt.Sprintf("Git commit failed: %v", err)
	} else {
		message = fmt.Sprintf("Git commit successful: %s", commitHash[:min(8, len(commitHash))])
	}

	eventData := map[string]interface{}{
		"command": "git",
		"args":    []string{"commit", "-m", opts.Message},
		"success": err == nil,
	}
	if commitHash != "" {
		eventData["commit_hash"] = commitHash
		eventData["commit_message"] = opts.Message
	}
	if opts.AddAll {
		eventData["add_all"] = true
	}
	if len(opts.CoAuthors) > 0 {
		eventData["co_authors"] = opts.CoAuthors
	}

	_ = et.emitEvent(ctx, severity, message, eventData)

	return commitHash, err
}

// Rebase performs a git rebase and tracks the operation
func (et *EventTracker) Rebase(ctx context.Context, repoPath string, opts RebaseOptions) (*RebaseResult, error) {
	result, err := et.git.Rebase(ctx, repoPath, opts)

	// Track git rebase operation
	severity := events.SeverityInfo
	message := "Git rebase"
	if result != nil {
		if result.Success {
			message = "Git rebase successful"
		} else if result.HasConflicts {
			message = fmt.Sprintf("Git rebase has conflicts (%d files)", len(result.ConflictedFiles))
			severity = events.SeverityWarning
		} else if err != nil {
			message = fmt.Sprintf("Git rebase failed: %v", err)
			severity = events.SeverityError
		}
	}

	eventData := map[string]interface{}{
		"command": "git",
		"args":    []string{"rebase"},
		"success": err == nil && result != nil && result.Success,
	}
	if opts.BaseBranch != "" {
		eventData["base_branch"] = opts.BaseBranch
		eventData["args"] = []string{"rebase", opts.BaseBranch}
	} else if opts.Continue {
		eventData["args"] = []string{"rebase", "--continue"}
	} else if opts.Abort {
		eventData["args"] = []string{"rebase", "--abort"}
	}

	if result != nil {
		eventData["has_conflicts"] = result.HasConflicts
		if result.HasConflicts {
			eventData["conflicted_files"] = result.ConflictedFiles
		}
		if result.CurrentBranch != "" {
			eventData["current_branch"] = result.CurrentBranch
		}
	}

	_ = et.emitEvent(ctx, severity, message, eventData)

	return result, err
}

// GetConflictDetails parses merge conflicts and tracks the operation
func (et *EventTracker) GetConflictDetails(ctx context.Context, req ConflictResolutionRequest) (*ConflictResolutionResult, error) {
	result, err := et.git.GetConflictDetails(ctx, req)

	// Track conflict details operation
	severity := events.SeverityInfo
	message := "Retrieved conflict details"
	if err != nil {
		severity = events.SeverityError
		message = fmt.Sprintf("Failed to get conflict details: %v", err)
	} else if result != nil {
		message = fmt.Sprintf("Found %d conflicts in %d files", result.TotalConflicts, len(result.FileConflicts))
	}

	eventData := map[string]interface{}{
		"command":          "conflict_details",
		"success":          err == nil,
		"conflicted_files": req.ConflictedFiles,
	}
	if result != nil {
		eventData["total_conflicts"] = result.TotalConflicts
		eventData["file_count"] = len(result.FileConflicts)
	}

	_ = et.emitEvent(ctx, severity, message, eventData)

	return result, err
}

// ValidateConflictResolution checks if conflicts are resolved and tracks the operation
func (et *EventTracker) ValidateConflictResolution(ctx context.Context, repoPath string, files []string) (bool, error) {
	resolved, err := et.git.ValidateConflictResolution(ctx, repoPath, files)

	// Track validation operation
	severity := events.SeverityInfo
	message := "Validated conflict resolution"
	if err != nil {
		severity = events.SeverityError
		message = fmt.Sprintf("Failed to validate: %v", err)
	} else if resolved {
		message = "All conflicts resolved"
	} else {
		message = "Conflicts still present"
		severity = events.SeverityWarning
	}

	eventData := map[string]interface{}{
		"command":  "validate_resolution",
		"success":  err == nil,
		"resolved": resolved,
		"files":    files,
	}

	_ = et.emitEvent(ctx, severity, message, eventData)

	return resolved, err
}

// emitEvent creates and stores a git operation event
func (et *EventTracker) emitEvent(ctx context.Context, severity events.EventSeverity, message string, data map[string]interface{}) error {
	event := &events.AgentEvent{
		ID:         uuid.New().String(),
		Type:       events.EventTypeGitOperation,
		Timestamp:  time.Now(),
		IssueID:    et.issueID,
		ExecutorID: et.executorID,
		AgentID:    et.agentID,
		Severity:   severity,
		Message:    message,
		Data:       data,
		SourceLine: 0, // Not applicable for git operations
	}

	return et.store.StoreAgentEvent(ctx, event)
}

// min returns the minimum of two integers
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
