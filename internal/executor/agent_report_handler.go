package executor

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/steveyegge/vc/internal/storage"
	"github.com/steveyegge/vc/internal/types"
)

// AgentReportHandler processes structured agent reports and updates the issue tracker
type AgentReportHandler struct {
	store storage.Storage
	actor string
}

// NewAgentReportHandler creates a new agent report handler
func NewAgentReportHandler(store storage.Storage, actor string) *AgentReportHandler {
	return &AgentReportHandler{
		store: store,
		actor: actor,
	}
}

// HandleReport processes an agent report and performs the appropriate actions
// Returns true if the issue should be considered complete, false otherwise
func (h *AgentReportHandler) HandleReport(ctx context.Context, issue *types.Issue, report *AgentReport) (completed bool, err error) {
	fmt.Printf("\n=== Processing Structured Agent Report ===\n")
	fmt.Printf("Status: %s\n", report.Status)
	fmt.Printf("Summary: %s\n", report.Summary)

	// Add the report as a comment for transparency
	h.addReportComment(ctx, issue.ID, report)

	switch report.Status {
	case AgentStatusCompleted:
		return h.handleCompleted(ctx, issue, report)

	case AgentStatusBlocked:
		return h.handleBlocked(ctx, issue, report)

	case AgentStatusPartial:
		return h.handlePartial(ctx, issue, report)

	case AgentStatusDecomposed:
		return h.handleDecomposed(ctx, issue, report)

	default:
		return false, fmt.Errorf("unknown agent status: %s", report.Status)
	}
}

// handleCompleted processes a completed status report
func (h *AgentReportHandler) handleCompleted(ctx context.Context, issue *types.Issue, report *AgentReport) (bool, error) {
	fmt.Printf("✓ Agent reports task as COMPLETED\n")

	// Add success comment
	comment := fmt.Sprintf("**Task Completed**\n\n%s", report.Summary)
	if report.TestsAdded {
		comment += "\n\n✓ Tests added"
	}
	if len(report.FilesModified) > 0 {
		comment += fmt.Sprintf("\n\nFiles modified (%d):\n", len(report.FilesModified))
		for _, file := range report.FilesModified {
			comment += fmt.Sprintf("- %s\n", file)
		}
	}

	if err := h.store.AddComment(ctx, issue.ID, h.actor, comment); err != nil {
		fmt.Fprintf(os.Stderr, "warning: failed to add completion comment: %v\n", err)
	}

	// Return true to indicate issue should be closed (subject to quality gates)
	return true, nil
}

// handleBlocked processes a blocked status report
func (h *AgentReportHandler) handleBlocked(ctx context.Context, issue *types.Issue, report *AgentReport) (bool, error) {
	fmt.Printf("✗ Agent reports task as BLOCKED\n")
	fmt.Printf("Blockers (%d):\n", len(report.Blockers))
	for i, blocker := range report.Blockers {
		fmt.Printf("  %d. %s\n", i+1, blocker)
	}

	// Create blocking issues for each blocker
	var createdIssues []string
	for i, blocker := range report.Blockers {
		blockerIssue := &types.Issue{
			Title:       fmt.Sprintf("Blocker: %s", truncateTitle(blocker)),
			Description: fmt.Sprintf("Blocker discovered while working on %s:\n\n%s\n\n_Automatically created from agent blocked status._", issue.ID, blocker),
			IssueType:   types.TypeTask,
			Status:      types.StatusOpen,
			Priority:    issue.Priority, // Inherit priority from parent
			Assignee:    "ai-supervisor",
		}

		if err := h.store.CreateIssue(ctx, blockerIssue, h.actor); err != nil {
			fmt.Fprintf(os.Stderr, "warning: failed to create blocker issue %d: %v\n", i+1, err)
			continue
		}

		createdIssues = append(createdIssues, blockerIssue.ID)

		// vc-d0r3: Add discovered:supervisor label to VC-filed blocker issues
		if err := h.store.AddLabel(ctx, blockerIssue.ID, types.LabelDiscoveredSupervisor, h.actor); err != nil {
			fmt.Fprintf(os.Stderr, "warning: failed to add %s label to %s: %v\n", types.LabelDiscoveredSupervisor, blockerIssue.ID, err)
		}

		// Add blocking dependency: parent is blocked by this blocker issue
		dep := &types.Dependency{
			IssueID:     issue.ID,
			DependsOnID: blockerIssue.ID,
			Type:        types.DepBlocks,
		}
		if err := h.store.AddDependency(ctx, dep, h.actor); err != nil {
			fmt.Fprintf(os.Stderr, "warning: failed to add blocking dependency: %v\n", err)
		}

		fmt.Printf("  ✓ Created blocker issue %s\n", blockerIssue.ID)
	}

	// Update original issue to blocked status
	updates := map[string]interface{}{
		"status": string(types.StatusBlocked),
	}

	// Log status change for audit trail (vc-n4lx)
	blockerList := strings.Join(createdIssues, ", ")
	h.store.LogStatusChangeFromUpdates(ctx, issue.ID, updates, h.actor,
		fmt.Sprintf("agent reported blockers: %s", blockerList))

	if err := h.store.UpdateIssue(ctx, issue.ID, updates, h.actor); err != nil {
		return false, fmt.Errorf("failed to update issue to blocked: %w", err)
	}

	// Add comment explaining the blockers
	blockersComment := fmt.Sprintf("**Task Blocked**\n\n%s\n\nBlockers created:\n%s",
		report.Summary, strings.Join(createdIssues, "\n"))
	if err := h.store.AddComment(ctx, issue.ID, h.actor, blockersComment); err != nil {
		fmt.Fprintf(os.Stderr, "warning: failed to add blockers comment: %v\n", err)
	}

	return false, nil
}

// handlePartial processes a partial status report
func (h *AgentReportHandler) handlePartial(ctx context.Context, issue *types.Issue, report *AgentReport) (bool, error) {
	fmt.Printf("⚠ Agent reports task as PARTIAL\n")
	fmt.Printf("Completed (%d items), Remaining (%d items)\n", len(report.Completed), len(report.Remaining))

	// Create follow-on issues for remaining work
	var createdIssues []string
	for i, remainingItem := range report.Remaining {
		followOnIssue := &types.Issue{
			Title:       truncateTitle(remainingItem),
			Description: fmt.Sprintf("Follow-on work from %s:\n\n%s\n\n_Automatically created from partial completion._", issue.ID, remainingItem),
			IssueType:   issue.IssueType, // Inherit type from parent
			Status:      types.StatusOpen,
			Priority:    issue.Priority, // Inherit priority
			Assignee:    "ai-supervisor",
		}

		if err := h.store.CreateIssue(ctx, followOnIssue, h.actor); err != nil {
			fmt.Fprintf(os.Stderr, "warning: failed to create follow-on issue %d: %v\n", i+1, err)
			continue
		}

		createdIssues = append(createdIssues, followOnIssue.ID)

		// vc-d0r3: Add discovered:supervisor label to VC-filed follow-on issues
		if err := h.store.AddLabel(ctx, followOnIssue.ID, types.LabelDiscoveredSupervisor, h.actor); err != nil {
			fmt.Fprintf(os.Stderr, "warning: failed to add %s label to %s: %v\n", types.LabelDiscoveredSupervisor, followOnIssue.ID, err)
		}

		// Add discovered-from dependency
		dep := &types.Dependency{
			IssueID:     followOnIssue.ID,
			DependsOnID: issue.ID,
			Type:        types.DepDiscoveredFrom,
		}
		if err := h.store.AddDependency(ctx, dep, h.actor); err != nil {
			fmt.Fprintf(os.Stderr, "warning: failed to add dependency: %v\n", err)
		}

		fmt.Printf("  ✓ Created follow-on issue %s: %s\n", followOnIssue.ID, truncateTitle(remainingItem))
	}

	// Build detailed comment
	var comment strings.Builder
	comment.WriteString("**Partial Completion**\n\n")
	comment.WriteString(fmt.Sprintf("%s\n\n", report.Summary))

	if len(report.Completed) > 0 {
		comment.WriteString("**Completed:**\n")
		for _, item := range report.Completed {
			comment.WriteString(fmt.Sprintf("- ✓ %s\n", item))
		}
		comment.WriteString("\n")
	}

	if len(createdIssues) > 0 {
		comment.WriteString(fmt.Sprintf("**Remaining work** (created %d follow-on issues):\n", len(createdIssues)))
		for i, issueID := range createdIssues {
			comment.WriteString(fmt.Sprintf("- %s: %s\n", issueID, report.Remaining[i]))
		}
	}

	if err := h.store.AddComment(ctx, issue.ID, h.actor, comment.String()); err != nil {
		fmt.Fprintf(os.Stderr, "warning: failed to add partial completion comment: %v\n", err)
	}

	// Keep issue open (partial completion means not done)
	// But update notes to track progress
	notes := fmt.Sprintf("Partial completion: %d items done, %d follow-on issues created", len(report.Completed), len(createdIssues))
	updates := map[string]interface{}{
		"notes": notes,
	}
	if err := h.store.UpdateIssue(ctx, issue.ID, updates, h.actor); err != nil {
		fmt.Fprintf(os.Stderr, "warning: failed to update issue notes: %v\n", err)
	}

	return false, nil
}

// handleDecomposed processes a decomposed status report
// This converts the original issue to an epic and creates child issues
func (h *AgentReportHandler) handleDecomposed(ctx context.Context, issue *types.Issue, report *AgentReport) (bool, error) {
	fmt.Printf("🔄 Agent reports task as DECOMPOSED\n")
	fmt.Printf("Reasoning: %s\n", report.Reasoning)
	fmt.Printf("Creating epic with %d children...\n", len(report.Children))

	// Step 1: Convert original issue to epic
	updates := map[string]interface{}{
		"issue_type":  types.TypeEpic,
		"title":       report.Epic.Title,
		"description": report.Epic.Description,
		"notes":       fmt.Sprintf("Autonomously decomposed by agent: %s", report.Reasoning),
	}
	if err := h.store.UpdateIssue(ctx, issue.ID, updates, h.actor); err != nil {
		return false, fmt.Errorf("failed to convert issue to epic: %w", err)
	}

	fmt.Printf("✓ Converted %s to epic: %s\n", issue.ID, report.Epic.Title)

	// Step 2: Create child issues
	var createdChildren []string
	for i, child := range report.Children {
		// Map priority string to int
		priority := 2 // default P2
		switch child.Priority {
		case "P0":
			priority = 0
		case "P1":
			priority = 1
		case "P2":
			priority = 2
		case "P3":
			priority = 3
		}

		// Map type string to IssueType
		issueType := types.TypeTask // default
		switch child.Type {
		case "bug":
			issueType = types.TypeBug
		case "task":
			issueType = types.TypeTask
		case "feature":
			issueType = types.TypeFeature
		case "chore":
			issueType = types.TypeChore
		}

		childIssue := &types.Issue{
			Title:       child.Title,
			Description: child.Description + fmt.Sprintf("\n\n_Child of epic %s (autonomously decomposed)_", issue.ID),
			IssueType:   issueType,
			Status:      types.StatusOpen,
			Priority:    priority,
			Assignee:    "ai-supervisor",
		}

		if err := h.store.CreateIssue(ctx, childIssue, h.actor); err != nil {
			fmt.Fprintf(os.Stderr, "warning: failed to create child issue %d: %v\n", i+1, err)
			continue
		}

		createdChildren = append(createdChildren, childIssue.ID)

		// vc-d0r3: Add discovered:supervisor label to VC-filed decomposed child issues
		if err := h.store.AddLabel(ctx, childIssue.ID, types.LabelDiscoveredSupervisor, h.actor); err != nil {
			fmt.Fprintf(os.Stderr, "warning: failed to add %s label to %s: %v\n", types.LabelDiscoveredSupervisor, childIssue.ID, err)
		}

		// Add parent-child dependency
		dep := &types.Dependency{
			IssueID:     childIssue.ID,
			DependsOnID: issue.ID,
			Type:        types.DepParentChild,
		}
		if err := h.store.AddDependency(ctx, dep, h.actor); err != nil {
			fmt.Fprintf(os.Stderr, "warning: failed to add parent-child dependency: %v\n", err)
		}

		fmt.Printf("  ✓ Created child %s (%s, P%d): %s\n", childIssue.ID, issueType, priority, child.Title)
	}

	// Step 3: Add comment explaining the decomposition
	comment := fmt.Sprintf("**Task Autonomously Decomposed**\n\n"+
		"**Reasoning:** %s\n\n"+
		"**Summary:** %s\n\n"+
		"Converted this issue to epic and created %d child issues:\n%s\n\n"+
		"The executor will pick up ready children on next iteration.",
		report.Reasoning,
		report.Summary,
		len(createdChildren),
		strings.Join(createdChildren, "\n"))

	if err := h.store.AddComment(ctx, issue.ID, h.actor, comment); err != nil {
		fmt.Fprintf(os.Stderr, "warning: failed to add decomposition comment: %v\n", err)
	}

	// Issue is now an epic - leave it open, executor will work on children
	return false, nil
}

// addReportComment adds the full agent report as a comment for transparency
func (h *AgentReportHandler) addReportComment(ctx context.Context, issueID string, report *AgentReport) {
	comment := fmt.Sprintf("**Structured Agent Report**\n\nStatus: `%s`\n\nSummary: %s", report.Status, report.Summary)

	// Add status-specific details
	switch report.Status {
	case AgentStatusBlocked:
		if len(report.Blockers) > 0 {
			comment += "\n\nBlockers:\n"
			for _, blocker := range report.Blockers {
				comment += fmt.Sprintf("- %s\n", blocker)
			}
		}

	case AgentStatusPartial:
		if len(report.Completed) > 0 {
			comment += "\n\nCompleted:\n"
			for _, item := range report.Completed {
				comment += fmt.Sprintf("- ✓ %s\n", item)
			}
		}
		if len(report.Remaining) > 0 {
			comment += "\n\nRemaining:\n"
			for _, item := range report.Remaining {
				comment += fmt.Sprintf("- ⏳ %s\n", item)
			}
		}

	case AgentStatusDecomposed:
		comment += fmt.Sprintf("\n\nReasoning: %s", report.Reasoning)
		comment += fmt.Sprintf("\n\nEpic: %s", report.Epic.Title)
		comment += fmt.Sprintf("\n\nChildren (%d):\n", len(report.Children))
		for i, child := range report.Children {
			comment += fmt.Sprintf("%d. %s (%s, %s)\n", i+1, child.Title, child.Type, child.Priority)
		}
	}

	if err := h.store.AddComment(ctx, issueID, "agent-protocol", comment); err != nil {
		fmt.Fprintf(os.Stderr, "warning: failed to add report comment: %v\n", err)
	}
}

// truncateTitle ensures a title doesn't exceed 100 characters
func truncateTitle(title string) string {
	const maxLen = 100
	if len(title) <= maxLen {
		return title
	}
	return title[:maxLen-3] + "..."
}
