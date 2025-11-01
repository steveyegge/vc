package repl

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/steveyegge/vc/internal/events"
	"github.com/steveyegge/vc/internal/types"
)

// executeTool executes a tool and returns the result.
// This dispatcher routes tool calls from the AI to the appropriate handler function.
// Each tool has typed validation and error handling.
func (c *ConversationHandler) executeTool(ctx context.Context, name string, input interface{}) (string, error) {
	var inputMap map[string]interface{}

	// The Anthropic SDK may provide input as different types:
	// - map[string]interface{} (already decoded)
	// - []byte (raw JSON)
	// - json.RawMessage (JSON bytes)
	switch v := input.(type) {
	case map[string]interface{}:
		inputMap = v
	case []byte:
		if err := json.Unmarshal(v, &inputMap); err != nil {
			return "", fmt.Errorf("failed to unmarshal tool input from bytes: %w", err)
		}
	case json.RawMessage:
		if err := json.Unmarshal(v, &inputMap); err != nil {
			return "", fmt.Errorf("failed to unmarshal tool input from RawMessage: %w", err)
		}
	default:
		return "", fmt.Errorf("invalid tool input format: expected map[string]interface{}, []byte, or json.RawMessage, got %T", input)
	}

	switch name {
	case "create_issue":
		return c.toolCreateIssue(ctx, inputMap)
	case "create_epic":
		return c.toolCreateEpic(ctx, inputMap)
	case "add_child_to_epic":
		return c.toolAddChildToEpic(ctx, inputMap)
	case "get_ready_work":
		return c.toolGetReadyWork(ctx, inputMap)
	case "get_issue":
		return c.toolGetIssue(ctx, inputMap)
	case "get_status":
		return c.toolGetStatus(ctx, inputMap)
	case "get_blocked_issues":
		return c.toolGetBlockedIssues(ctx, inputMap)
	case "continue_execution":
		return c.toolContinueExecution(ctx, inputMap)
	case "get_recent_activity":
		return c.toolGetRecentActivity(ctx, inputMap)
	case "search_issues":
		return c.toolSearchIssues(ctx, inputMap)
	case "continue_until_blocked":
		return c.toolContinueUntilBlocked(ctx, inputMap)
	default:
		return "", fmt.Errorf("unknown tool: %s", name)
	}
}

// toolCreateIssue creates a new issue from natural language input.
// Supports all issue types (bug, feature, task, chore) with optional design and acceptance criteria.
// Input: title (required), description, type (default: task), priority (default: 2), design, acceptance
// Returns: Formatted success message with issue ID
func (c *ConversationHandler) toolCreateIssue(ctx context.Context, input map[string]interface{}) (string, error) {
	title, _ := input["title"].(string)
	if title == "" {
		return "", fmt.Errorf("title is required")
	}

	description, _ := input["description"].(string)
	design, _ := input["design"].(string)
	acceptance, _ := input["acceptance"].(string)

	issueType := "task"
	if t, ok := input["type"].(string); ok && t != "" {
		issueType = t
	}

	// Validate issue type
	validTypes := map[string]bool{
		"bug":     true,
		"feature": true,
		"task":    true,
		"chore":   true,
	}
	if !validTypes[issueType] {
		return "", fmt.Errorf("invalid issue type: %s (must be bug, feature, task, or chore)", issueType)
	}

	priority := 2
	if p, ok := input["priority"].(float64); ok {
		priority = int(p)
	}

	issue := &types.Issue{
		Title:              title,
		Description:        description,
		Design:             design,
		AcceptanceCriteria: acceptance,
		IssueType:          types.IssueType(issueType),
		Priority:           priority,
		Status:             types.StatusOpen,
		CreatedAt:          time.Now(),
		UpdatedAt:          time.Now(),
	}

	err := c.storage.CreateIssue(ctx, issue, AIActor)
	if err != nil {
		return "", fmt.Errorf("failed to create issue: %w", err)
	}

	return fmt.Sprintf("Created %s %s: %s", issueType, issue.ID, title), nil
}

// toolCreateEpic creates an epic (container for related work).
// Epics are automatically given priority 1 and can have children added via toolAddChildToEpic.
// Input: title (required), description, design, acceptance
// Returns: Formatted success message with epic ID
func (c *ConversationHandler) toolCreateEpic(ctx context.Context, input map[string]interface{}) (string, error) {
	title, _ := input["title"].(string)
	if title == "" {
		return "", fmt.Errorf("title is required")
	}

	description, _ := input["description"].(string)
	design, _ := input["design"].(string)
	acceptance, _ := input["acceptance"].(string)

	epic := &types.Issue{
		Title:              title,
		Description:        description,
		Design:             design,
		AcceptanceCriteria: acceptance,
		IssueType:          types.TypeEpic,
		Priority:           1,
		Status:             types.StatusOpen,
		CreatedAt:          time.Now(),
		UpdatedAt:          time.Now(),
	}

	err := c.storage.CreateIssue(ctx, epic, AIActor)
	if err != nil {
		return "", fmt.Errorf("failed to create epic: %w", err)
	}

	return fmt.Sprintf("Created epic %s: %s", epic.ID, title), nil
}

// toolAddChildToEpic links an issue as a child of an epic.
// Creates parent-child dependency and optionally a blocks relationship.
// Input: epic_id (required), child_issue_id (required), blocks (default: true)
// Returns: Formatted success message with both IDs
func (c *ConversationHandler) toolAddChildToEpic(ctx context.Context, input map[string]interface{}) (string, error) {
	epicID, _ := input["epic_id"].(string)
	childID, _ := input["child_issue_id"].(string)
	if epicID == "" || childID == "" {
		return "", fmt.Errorf("epic_id and child_issue_id are required")
	}

	blocks := true
	if b, ok := input["blocks"].(bool); ok {
		blocks = b
	}

	// Always create parent-child relationship: child belongs to epic
	parentChildDep := &types.Dependency{
		IssueID:     childID, // child depends on parent
		DependsOnID: epicID,
		Type:        types.DepParentChild,
		CreatedAt:   time.Now(),
		CreatedBy:   AIActor,
	}

	err := c.storage.AddDependency(ctx, parentChildDep, AIActor)
	if err != nil {
		return "", fmt.Errorf("failed to add parent-child dependency: %w", err)
	}

	// If blocks=true, also create blocks relationship: epic blocked by child
	if blocks {
		blocksDep := &types.Dependency{
			IssueID:     epicID, // epic depends on child
			DependsOnID: childID,
			Type:        types.DepBlocks,
			CreatedAt:   time.Now(),
			CreatedBy:   AIActor,
		}

		err = c.storage.AddDependency(ctx, blocksDep, AIActor)
		if err != nil {
			return "", fmt.Errorf("failed to add blocks dependency: %w", err)
		}
	}

	return fmt.Sprintf("Added %s as child of epic %s (blocks=%v)", childID, epicID, blocks), nil
}

// toolGetReadyWork retrieves issues that are ready to execute (no blockers).
// Returns issues in priority order with type and priority information.
// Input: limit (default: 5)
// Returns: Formatted list of ready issues or "No ready work found"
func (c *ConversationHandler) toolGetReadyWork(ctx context.Context, input map[string]interface{}) (string, error) {
	limit := 5
	if l, ok := input["limit"].(float64); ok {
		limit = int(l)
	}

	filter := types.WorkFilter{
		Status: types.StatusOpen,
		Limit:  limit,
	}

	issues, err := c.storage.GetReadyWork(ctx, filter)
	if err != nil {
		return "", fmt.Errorf("failed to get ready work: %w", err)
	}

	if len(issues) == 0 {
		return "No ready work found", nil
	}

	result := fmt.Sprintf("Found %d ready issues:\n", len(issues))
	for _, issue := range issues {
		result += fmt.Sprintf("- %s [%s] %s (priority %d)\n", issue.ID, issue.IssueType, issue.Title, issue.Priority)
	}

	return result, nil
}

// toolGetIssue retrieves detailed information about a specific issue.
// Returns full issue data as JSON including dependencies and metadata.
// Input: issue_id (required)
// Returns: JSON representation of the issue
func (c *ConversationHandler) toolGetIssue(ctx context.Context, input map[string]interface{}) (string, error) {
	issueID, _ := input["issue_id"].(string)
	if issueID == "" {
		return "", fmt.Errorf("issue_id is required")
	}

	issue, err := c.storage.GetIssue(ctx, issueID)
	if err != nil {
		return "", fmt.Errorf("failed to get issue: %w", err)
	}

	data, err := json.MarshalIndent(issue, "", "  ")
	if err != nil {
		return "", fmt.Errorf("failed to marshal issue: %w", err)
	}

	return string(data), nil
}

// toolGetStatus retrieves overall project statistics.
// Shows issue counts by status, ready work, and average lead time.
// Input: none (validates that no parameters are passed)
// Returns: Formatted project status summary
func (c *ConversationHandler) toolGetStatus(ctx context.Context, input map[string]interface{}) (string, error) {
	// Validate no unexpected input parameters (tool takes no parameters)
	if len(input) > 0 {
		return "", fmt.Errorf("get_status takes no parameters, but received: %v", input)
	}

	stats, err := c.storage.GetStatistics(ctx)
	if err != nil {
		return "", fmt.Errorf("failed to get statistics: %w", err)
	}

	result := fmt.Sprintf(`Project Status:
- Total Issues: %d
- Open: %d
- In Progress: %d
- Blocked: %d
- Closed: %d
- Ready to Work: %d
- Average Lead Time: %.1f hours`,
		stats.TotalIssues,
		stats.OpenIssues,
		stats.InProgressIssues,
		stats.BlockedIssues,
		stats.ClosedIssues,
		stats.ReadyIssues,
		stats.AverageLeadTime,
	)

	return result, nil
}

// toolGetBlockedIssues retrieves issues blocked by dependencies.
// Returns issues with details about what's blocking them.
// Input: limit (default: 10)
// Returns: Formatted list of blocked issues with blocker IDs or "No blocked issues found"
func (c *ConversationHandler) toolGetBlockedIssues(ctx context.Context, input map[string]interface{}) (string, error) {
	limit := 10
	if l, ok := input["limit"].(float64); ok {
		limit = int(l)
	}

	// Note: GetBlockedIssues() fetches all blocked issues from the database.
	// We apply the limit in-memory afterward because the storage interface
	// doesn't support limit parameters. This is inefficient for large datasets
	// but acceptable given typical blocked issue counts.
	blockedIssues, err := c.storage.GetBlockedIssues(ctx)
	if err != nil {
		return "", fmt.Errorf("failed to get blocked issues: %w", err)
	}

	if len(blockedIssues) == 0 {
		return "No blocked issues found", nil
	}

	// Apply limit in-memory
	if limit < len(blockedIssues) {
		blockedIssues = blockedIssues[:limit]
	}

	result := fmt.Sprintf("Found %d blocked issues:\n\n", len(blockedIssues))
	for _, bi := range blockedIssues {
		result += fmt.Sprintf("- %s [%s] %s (P%d)\n", bi.ID, bi.IssueType, bi.Title, bi.Priority)
		result += fmt.Sprintf("  Blocked by %d issues: %v\n", bi.BlockedByCount, bi.BlockedBy)
	}

	return result, nil
}

// toolGetRecentActivity retrieves recent agent execution events.
// Shows what agents have been doing across all issues or for a specific issue.
// Input: limit (default: 20), issue_id (optional filter)
// Returns: Formatted list of agent events with timestamps and severity or "No recent agent activity found"
func (c *ConversationHandler) toolGetRecentActivity(ctx context.Context, input map[string]interface{}) (string, error) {
	limit := 20
	if l, ok := input["limit"].(float64); ok {
		limit = int(l)
	}

	issueID, _ := input["issue_id"].(string)

	var agentEvents []*events.AgentEvent
	var err error

	if issueID != "" {
		// Get agent events for specific issue
		agentEvents, err = c.storage.GetAgentEventsByIssue(ctx, issueID)
		// Apply limit
		if err == nil && len(agentEvents) > limit {
			agentEvents = agentEvents[:limit]
		}
	} else {
		// Get recent agent events across all issues
		agentEvents, err = c.storage.GetRecentAgentEvents(ctx, limit)
	}

	if err != nil {
		return "", fmt.Errorf("failed to get recent activity: %w", err)
	}

	if len(agentEvents) == 0 {
		return "No recent agent activity found", nil
	}

	result := fmt.Sprintf("Recent Agent Activity (%d events):\n\n", len(agentEvents))
	for _, event := range agentEvents {
		timestamp := event.Timestamp.Format("2006-01-02 15:04:05")
		result += fmt.Sprintf("[%s] %s - %s", timestamp, event.IssueID, event.Type)
		if event.Severity != events.SeverityInfo {
			result += fmt.Sprintf(" [%s]", event.Severity)
		}
		if event.Message != "" {
			result += fmt.Sprintf(": %s", event.Message)
		}
		result += "\n"
	}

	return result, nil
}

// toolSearchIssues performs full-text search across issues.
// Searches titles, descriptions, and other text fields with optional status filter.
// Input: query (required), status (optional), limit (default: 10)
// Returns: Formatted list of matching issues with truncated descriptions or "No issues found"
func (c *ConversationHandler) toolSearchIssues(ctx context.Context, input map[string]interface{}) (string, error) {
	query, _ := input["query"].(string)
	if query == "" {
		return "", fmt.Errorf("query is required")
	}

	limit := 10
	if l, ok := input["limit"].(float64); ok {
		limit = int(l)
	}

	// Build filter
	filter := types.IssueFilter{
		Limit: limit,
	}

	if statusStr, ok := input["status"].(string); ok && statusStr != "" {
		status := types.Status(statusStr)
		filter.Status = &status
	}

	issues, err := c.storage.SearchIssues(ctx, query, filter)
	if err != nil {
		return "", fmt.Errorf("failed to search issues: %w", err)
	}

	if len(issues) == 0 {
		return fmt.Sprintf("No issues found matching query: %s", query), nil
	}

	result := fmt.Sprintf("Found %d issues matching '%s':\n\n", len(issues), query)
	for _, issue := range issues {
		result += fmt.Sprintf("- %s [%s] %s (P%d, %s)\n", issue.ID, issue.IssueType, issue.Title, issue.Priority, issue.Status)
		if issue.Description != "" && len(issue.Description) > 100 {
			result += fmt.Sprintf("  %s...\n", issue.Description[:100])
		} else if issue.Description != "" {
			result += fmt.Sprintf("  %s\n", issue.Description)
		}
	}

	return result, nil
}
