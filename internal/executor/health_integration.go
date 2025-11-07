package executor

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/steveyegge/vc/internal/events"
	"github.com/steveyegge/vc/internal/health"
	"github.com/steveyegge/vc/internal/types"
)

// checkHealthMonitors runs health monitors that are due and files discovered issues.
// This is called after successfully completing an issue.
func (e *Executor) checkHealthMonitors(ctx context.Context) error {
	// Skip if health monitoring is not enabled
	if e.healthRegistry == nil {
		return nil
	}

	// Determine the number of issues closed (for event-based scheduling)
	// For MVP, we'll just increment by 1 when this is called
	now := time.Now()

	// Get monitors that are due to run
	monitors := e.healthRegistry.GetScheduledMonitors(now, 1, 0)
	if len(monitors) == 0 {
		return nil
	}

	// Log that we're running health checks
	fmt.Printf("Health: Running %d scheduled monitor(s)\n", len(monitors))

	// Get project root from database path
	projectRoot, err := getProjectRootFromStore(e.store)
	if err != nil {
		return fmt.Errorf("getting project root: %w", err)
	}

	// Run each monitor
	for _, monitor := range monitors {
		if err := e.runHealthMonitor(ctx, monitor, projectRoot); err != nil {
			// Log error but continue with other monitors
			fmt.Fprintf(os.Stderr, "Health: Error running monitor %s: %v\n", monitor.Name(), err)
			continue
		}
	}

	return nil
}

// runHealthMonitor executes a single health monitor and files any discovered issues.
func (e *Executor) runHealthMonitor(ctx context.Context, monitor health.HealthMonitor, _ string) error {
	monitorName := monitor.Name()
	fmt.Printf("Health: Running %s\n", monitorName)

	// Build codebase context (currently unused by monitors, but part of interface)
	codebaseCtx := health.CodebaseContext{}

	// Run the monitor
	result, err := monitor.Check(ctx, codebaseCtx)
	if err != nil {
		e.logEvent(ctx, events.EventTypeHealthCheckFailed, events.SeverityError, "",
			fmt.Sprintf("Health monitor %s failed: %v", monitorName, err),
			map[string]interface{}{
				"monitor": monitorName,
				"error":   err.Error(),
			})
		return fmt.Errorf("monitor check failed: %w", err)
	}

	// File discovered issues
	var issuesFiled []string
	for _, discovered := range result.IssuesFound {
		issueID, err := e.fileHealthIssue(ctx, monitor, discovered)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Health: Failed to file issue: %v\n", err)
			continue
		}
		issuesFiled = append(issuesFiled, issueID)
	}

	// Record the run in the registry
	if err := e.healthRegistry.RecordRun(monitorName, result, issuesFiled); err != nil {
		return fmt.Errorf("recording monitor run: %w", err)
	}

	// Log results
	if len(issuesFiled) > 0 {
		fmt.Printf("Health: %s found %d issue(s)\n", monitorName, len(issuesFiled))
		e.logEvent(ctx, events.EventTypeHealthCheckCompleted, events.SeverityInfo, "",
			fmt.Sprintf("Health monitor %s filed %d issue(s)", monitorName, len(issuesFiled)),
			map[string]interface{}{
				"monitor":      monitorName,
				"issues_filed": issuesFiled,
			})
	} else {
		fmt.Printf("Health: %s found no issues\n", monitorName)
		e.logEvent(ctx, events.EventTypeHealthCheckCompleted, events.SeverityInfo, "",
			fmt.Sprintf("Health monitor %s found no issues", monitorName),
			map[string]interface{}{
				"monitor": monitorName,
			})
	}

	return nil
}

// fileHealthIssue creates an issue in the tracker for a discovered health problem.
func (e *Executor) fileHealthIssue(ctx context.Context, monitor health.HealthMonitor, discovered health.DiscoveredIssue) (string, error) {
	// Build issue title and description
	title := buildHealthIssueTitle(monitor, discovered)
	description := buildHealthIssueDescription(monitor, discovered)

	// Determine priority based on severity
	priority := 3 // Default: P3 (low)
	switch discovered.Severity {
	case "high":
		priority = 1 // P1
	case "medium":
		priority = 2 // P2
	}

	// Create the issue
	issue := &types.Issue{
		Title:       title,
		Description: description,
		Status:      types.StatusOpen,
		Priority:    priority,
		IssueType:   types.TypeTask,
	}

	// Add health monitor label
	err := e.store.CreateIssue(ctx, issue, "vc-health-monitor")
	if err != nil {
		return "", fmt.Errorf("creating issue: %w", err)
	}

	// Add labels
	labels := []string{
		"health",
		discovered.Category,
		fmt.Sprintf("severity:%s", discovered.Severity),
	}

	for _, label := range labels {
		if err := e.store.AddLabel(ctx, issue.ID, label, "vc-health-monitor"); err != nil {
			// Log but don't fail on label errors
			fmt.Fprintf(os.Stderr, "Warning: failed to add label %q to %s: %v\n", label, issue.ID, err)
		}
	}

	return issue.ID, nil
}

// buildHealthIssueTitle creates a concise title for the health issue.
func buildHealthIssueTitle(_ health.HealthMonitor, discovered health.DiscoveredIssue) string {
	// Use the first sentence or first 80 chars of description
	desc := discovered.Description
	for idx := 0; idx < len(desc); idx++ {
		if desc[idx] == '.' && idx < 80 {
			return desc[:idx]
		}
	}
	if len(desc) > 80 {
		return desc[:77] + "..."
	}
	return desc
}

// buildHealthIssueDescription creates a detailed description for the health issue.
func buildHealthIssueDescription(monitor health.HealthMonitor, discovered health.DiscoveredIssue) string {
	desc := "## Health Monitor Finding\n\n"
	desc += fmt.Sprintf("**Monitor:** %s\n", monitor.Name())
	desc += fmt.Sprintf("**Category:** %s\n", discovered.Category)
	desc += fmt.Sprintf("**Severity:** %s\n\n", discovered.Severity)

	desc += "## Issue\n\n"
	desc += discovered.Description + "\n\n"

	if discovered.FilePath != "" {
		desc += "## Location\n\n"
		desc += fmt.Sprintf("File: `%s`\n", discovered.FilePath)
		if discovered.LineStart > 0 {
			if discovered.LineEnd > 0 && discovered.LineEnd != discovered.LineStart {
				desc += fmt.Sprintf("Lines: %d-%d\n", discovered.LineStart, discovered.LineEnd)
			} else {
				desc += fmt.Sprintf("Line: %d\n", discovered.LineStart)
			}
		}
		desc += "\n"
	}

	// Add evidence if available
	if len(discovered.Evidence) > 0 {
		desc += "## Evidence\n\n"
		for key, value := range discovered.Evidence {
			desc += fmt.Sprintf("- %s: %v\n", key, value)
		}
		desc += "\n"
	}

	desc += "## Philosophy\n\n"
	desc += monitor.Philosophy() + "\n"

	return desc
}

// getProjectRootFromStore determines the project root from the storage configuration.
func getProjectRootFromStore(_ interface{}) (string, error) {
	// For SQLite storage, the database path should be .beads/beads.db
	// The project root is the parent of .beads/
	// This is a simplified implementation - in production, you'd want to
	// query the storage for its path

	// For now, return current working directory
	cwd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("getting current directory: %w", err)
	}

	// Check if .beads directory exists
	beadsDir := filepath.Join(cwd, ".beads")
	if info, err := os.Stat(beadsDir); err == nil && info.IsDir() {
		return cwd, nil
	}

	// Try parent directories
	parent := filepath.Dir(cwd)
	for parent != cwd {
		beadsDir := filepath.Join(parent, ".beads")
		if info, err := os.Stat(beadsDir); err == nil && info.IsDir() {
			return parent, nil
		}
		cwd = parent
		parent = filepath.Dir(cwd)
	}

	return "", fmt.Errorf("could not find project root (no .beads directory)")
}
