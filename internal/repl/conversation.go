package repl

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/fatih/color"
	"github.com/steveyegge/vc/internal/ai"
	"github.com/steveyegge/vc/internal/events"
	"github.com/steveyegge/vc/internal/executor"
	"github.com/steveyegge/vc/internal/storage"
	"github.com/steveyegge/vc/internal/types"
)

const (
	// AIActor is the actor name used when AI creates issues or dependencies
	AIActor = "ai"
	// MaxConversationIterations prevents infinite loops in tool-use conversations
	MaxConversationIterations = 10
)

// ConversationHandler handles AI conversations
type ConversationHandler struct {
	client  *anthropic.Client
	model   string
	history []anthropic.MessageParam
	storage storage.Storage
}

// NewConversationHandler creates a new conversation handler
func NewConversationHandler(store storage.Storage) (*ConversationHandler, error) {
	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey == "" {
		return nil, fmt.Errorf("ANTHROPIC_API_KEY not set")
	}

	client := anthropic.NewClient(option.WithAPIKey(apiKey))

	return &ConversationHandler{
		client:  &client,
		model:   "claude-sonnet-4-5-20250929",
		history: make([]anthropic.MessageParam, 0),
		storage: store,
	}, nil
}

// systemPrompt returns the system prompt for VC conversations
func (c *ConversationHandler) systemPrompt() string {
	return `You are VC (VibeCoder), an AI-orchestrated coding agent colony system.

You are having a conversation with a developer through the VC REPL. You can:
1. Answer questions about their code and project
2. Help plan and break down work
3. Create issues, epics, and manage dependencies using function calling
4. Query the issue tracker state
5. Execute work by spawning coding agents

You have access to these tools:

Issue Creation:
- create_issue: Create a new issue (bug, feature, task, chore)
- create_epic: Create an epic (container for related issues)
- add_child_to_epic: Add an issue as a child of an epic, optionally marking it as blocking

Querying:
- get_ready_work: Query issues ready to work on (no blockers)
- get_issue: Get detailed information about an issue
- get_status: Get overall project status and statistics
- get_blocked_issues: List issues blocked by dependencies
- get_recent_activity: View recent agent execution activity
- search_issues: Search issues by text query

Execution:
- continue_execution: Execute the next ready issue or a specific issue (the VibeCoder Primitive)

When users describe work, proactively create appropriate issues. Examples:
- "Add Docker support" → Create feature issue
- "Fix the login bug" → Create bug issue
- "Build auth system" → Create epic with child tasks
- "Refactor the database layer" → Create chore issue

When users say things like "let's continue", "keep going", or "work on that", use continue_execution to spawn an agent.

Be helpful, concise, and action-oriented. Use tools immediately when appropriate.`
}

// getTools returns the tool definitions for function calling
func (c *ConversationHandler) getTools() []anthropic.ToolUnionParam {
	toolParams := []anthropic.ToolParam{
		{
			Name:        "create_issue",
			Description: anthropic.String("Create a new issue (bug, feature, task, or chore). Returns the created issue ID."),
			InputSchema: anthropic.ToolInputSchemaParam{
				Properties: map[string]interface{}{
					"title":       map[string]interface{}{"type": "string", "description": "Issue title (required)"},
					"description": map[string]interface{}{"type": "string", "description": "Detailed description"},
					"type":        map[string]interface{}{"type": "string", "enum": []string{"bug", "feature", "task", "chore"}, "description": "Issue type (default: task)"},
					"priority":    map[string]interface{}{"type": "integer", "minimum": 0, "maximum": 4, "description": "Priority 0-4 (0=highest, default: 2)"},
					"design":      map[string]interface{}{"type": "string", "description": "Design notes"},
					"acceptance":  map[string]interface{}{"type": "string", "description": "Acceptance criteria"},
				},
				Required: []string{"title"},
			},
		},
		{
			Name:        "create_epic",
			Description: anthropic.String("Create an epic (container for related work). Returns the created epic ID."),
			InputSchema: anthropic.ToolInputSchemaParam{
				Properties: map[string]interface{}{
					"title":       map[string]interface{}{"type": "string", "description": "Epic title (required)"},
					"description": map[string]interface{}{"type": "string", "description": "Epic description"},
					"design":      map[string]interface{}{"type": "string", "description": "Overall design approach"},
					"acceptance":  map[string]interface{}{"type": "string", "description": "Acceptance criteria for completion"},
				},
				Required: []string{"title"},
			},
		},
		{
			Name:        "add_child_to_epic",
			Description: anthropic.String("Add an issue as a child of an epic with parent-child dependency. Optionally mark it as blocking."),
			InputSchema: anthropic.ToolInputSchemaParam{
				Properties: map[string]interface{}{
					"epic_id":        map[string]interface{}{"type": "string", "description": "Epic ID (required)"},
					"child_issue_id": map[string]interface{}{"type": "string", "description": "Child issue ID (required)"},
					"blocks":         map[string]interface{}{"type": "boolean", "description": "Whether this child blocks the epic (default: true)"},
				},
				Required: []string{"epic_id", "child_issue_id"},
			},
		},
		{
			Name:        "get_ready_work",
			Description: anthropic.String("Get issues that are ready to work on (no blockers). Returns list of issues."),
			InputSchema: anthropic.ToolInputSchemaParam{
				Properties: map[string]interface{}{
					"limit": map[string]interface{}{"type": "integer", "minimum": 1, "maximum": 50, "description": "Max results (default: 5)"},
				},
			},
		},
		{
			Name:        "get_issue",
			Description: anthropic.String("Get detailed information about a specific issue including dependencies and dependents."),
			InputSchema: anthropic.ToolInputSchemaParam{
				Properties: map[string]interface{}{
					"issue_id": map[string]interface{}{"type": "string", "description": "Issue ID (required)"},
				},
				Required: []string{"issue_id"},
			},
		},
		{
			Name:        "get_status",
			Description: anthropic.String("Get overall project status including open/in-progress/blocked counts and statistics."),
			InputSchema: anthropic.ToolInputSchemaParam{
				Properties: map[string]interface{}{},
			},
		},
		{
			Name:        "get_blocked_issues",
			Description: anthropic.String("Get list of issues blocked by dependencies. Returns issues that cannot be worked on because they depend on other incomplete work."),
			InputSchema: anthropic.ToolInputSchemaParam{
				Properties: map[string]interface{}{
					"limit": map[string]interface{}{"type": "integer", "minimum": 1, "maximum": 50, "description": "Max results (default: 10)"},
				},
			},
		},
		{
			Name:        "continue_execution",
			Description: anthropic.String("Execute the next ready issue or a specific issue. This is the VibeCoder Primitive - it spawns an agent to work on the issue and processes results."),
			InputSchema: anthropic.ToolInputSchemaParam{
				Properties: map[string]interface{}{
					"issue_id": map[string]interface{}{"type": "string", "description": "Specific issue ID to execute (optional - if not provided, picks next ready issue)"},
					"async":    map[string]interface{}{"type": "boolean", "description": "Run execution asynchronously in background (default: false)"},
				},
			},
		},
		{
			Name:        "get_recent_activity",
			Description: anthropic.String("Get recent agent execution activity and events. Shows what agents have been doing."),
			InputSchema: anthropic.ToolInputSchemaParam{
				Properties: map[string]interface{}{
					"limit":    map[string]interface{}{"type": "integer", "minimum": 1, "maximum": 100, "description": "Max results (default: 20)"},
					"issue_id": map[string]interface{}{"type": "string", "description": "Filter by specific issue ID (optional)"},
				},
			},
		},
		{
			Name:        "search_issues",
			Description: anthropic.String("Search issues by text query. Searches titles, descriptions, and other text fields."),
			InputSchema: anthropic.ToolInputSchemaParam{
				Properties: map[string]interface{}{
					"query":  map[string]interface{}{"type": "string", "description": "Search query (required)"},
					"status": map[string]interface{}{"type": "string", "enum": []string{"open", "in_progress", "blocked", "closed"}, "description": "Filter by status (optional)"},
					"limit":  map[string]interface{}{"type": "integer", "minimum": 1, "maximum": 50, "description": "Max results (default: 10)"},
				},
				Required: []string{"query"},
			},
		},
	}

	tools := make([]anthropic.ToolUnionParam, len(toolParams))
	for i, toolParam := range toolParams {
		tools[i] = anthropic.ToolUnionParam{OfTool: &toolParam}
	}
	return tools
}

// SendMessage sends a user message and gets AI response
func (c *ConversationHandler) SendMessage(ctx context.Context, userMessage string) (string, error) {
	// If this is the first message, prepend system context
	var fullMessage string
	if len(c.history) == 0 {
		fullMessage = c.systemPrompt() + "\n\n---\n\nUser: " + userMessage
	} else {
		fullMessage = userMessage
	}

	// Add user message to history
	c.history = append(c.history, anthropic.NewUserMessage(
		anthropic.NewTextBlock(fullMessage),
	))

	// Conversation loop to handle tool use
	for iteration := 0; iteration < MaxConversationIterations; iteration++ {
		// Call Claude API with tools
		response, err := c.client.Messages.New(ctx, anthropic.MessageNewParams{
			Model:     anthropic.Model(c.model),
			MaxTokens: 4096,
			Messages:  c.history,
			Tools:     c.getTools(),
		})

		if err != nil {
			return "", fmt.Errorf("API call failed: %w", err)
		}

		// Check stop reason
		if response.StopReason == "end_turn" {
			// Normal text response - extract and return
			var responseText string
			for _, block := range response.Content {
				if block.Type == "text" {
					responseText += block.Text
				}
			}

			// Add assistant response to history
			c.history = append(c.history, anthropic.NewAssistantMessage(
				anthropic.NewTextBlock(responseText),
			))

			return responseText, nil
		}

		if response.StopReason == "tool_use" {
			// Add assistant's response to history (includes tool use blocks)
			c.history = append(c.history, response.ToParam())

			// Process tool calls and collect results
			var toolResults []anthropic.ContentBlockParamUnion

			for _, block := range response.Content {
				variant := block.AsAny()
				if toolUse, ok := variant.(anthropic.ToolUseBlock); ok {
					// Execute the tool
					result, err := c.executeTool(ctx, toolUse.Name, toolUse.Input)
					if err != nil {
						toolResults = append(toolResults, anthropic.NewToolResultBlock(toolUse.ID, fmt.Sprintf("Error: %v", err), true))
					} else {
						toolResults = append(toolResults, anthropic.NewToolResultBlock(toolUse.ID, result, false))
					}
				}
			}

			// Add tool results as a user message
			c.history = append(c.history, anthropic.NewUserMessage(toolResults...))

			// Continue loop to get final response
		} else {
			return "", fmt.Errorf("unexpected stop reason: %s", response.StopReason)
		}
	}

	return "", fmt.Errorf("conversation exceeded maximum iterations (%d)", MaxConversationIterations)
}

// executeTool executes a tool and returns the result
func (c *ConversationHandler) executeTool(ctx context.Context, name string, input interface{}) (string, error) {
	inputMap, ok := input.(map[string]interface{})
	if !ok {
		return "", fmt.Errorf("invalid tool input format")
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
	default:
		return "", fmt.Errorf("unknown tool: %s", name)
	}
}

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

func (c *ConversationHandler) toolContinueExecution(ctx context.Context, input map[string]interface{}) (string, error) {
	issueID, _ := input["issue_id"].(string)
	async := false
	if a, ok := input["async"].(bool); ok {
		async = a
	}

	// Note: async execution is not yet implemented
	if async {
		return "", fmt.Errorf("async execution not yet implemented")
	}

	var issue *types.Issue
	var err error

	// Get issue to execute
	if issueID != "" {
		// Execute specific issue
		issue, err = c.storage.GetIssue(ctx, issueID)
		if err != nil {
			return "", fmt.Errorf("failed to get issue %s: %w", issueID, err)
		}

		// Validate issue status
		switch issue.Status {
		case types.StatusClosed:
			return fmt.Sprintf("Cannot execute issue %s: already closed", issueID), nil
		case types.StatusInProgress:
			return fmt.Sprintf("Cannot execute issue %s: already in progress (may be claimed by another executor)", issueID), nil
		case types.StatusBlocked:
			// Get dependencies to show what's blocking it
			deps, depErr := c.storage.GetDependencies(ctx, issueID)
			if depErr == nil && len(deps) > 0 {
				var blockingIDs []string
				for _, dep := range deps {
					if dep.Status != types.StatusClosed {
						blockingIDs = append(blockingIDs, dep.ID)
					}
				}
				if len(blockingIDs) > 0 {
					return fmt.Sprintf("Cannot execute issue %s: blocked by %v", issueID, blockingIDs), nil
				}
			}
			return fmt.Sprintf("Cannot execute issue %s: currently blocked", issueID), nil
		}
	} else {
		// Get next ready work
		issues, err := c.storage.GetReadyWork(ctx, types.WorkFilter{
			Status: types.StatusOpen,
			Limit:  1,
		})
		if err != nil {
			return "", fmt.Errorf("failed to get ready work: %w", err)
		}

		if len(issues) == 0 {
			return "No ready work found. All issues are either completed or blocked.", nil
		}

		issue = issues[0]
	}

	// Claim the issue
	instanceID := fmt.Sprintf("conversation-%s", AIActor)
	if err := c.storage.ClaimIssue(ctx, issue.ID, instanceID); err != nil {
		return "", fmt.Errorf("failed to claim issue %s: %w", issue.ID, err)
	}

	// Update execution state to executing
	if err := c.storage.UpdateExecutionState(ctx, issue.ID, types.ExecutionStateExecuting); err != nil {
		// Log warning but continue
		fmt.Fprintf(os.Stderr, "warning: failed to update execution state: %v\n", err)
	}

	// Spawn agent
	agentCfg := executor.AgentConfig{
		Type:       executor.AgentTypeClaudeCode,
		WorkingDir: ".",
		Issue:      issue,
		StreamJSON: false,
		Timeout:    30 * time.Minute,
	}

	agent, err := executor.SpawnAgent(ctx, agentCfg)
	if err != nil {
		c.releaseIssueWithError(ctx, issue.ID, instanceID, fmt.Sprintf("Failed to spawn agent: %v", err))
		return "", fmt.Errorf("failed to spawn agent: %w", err)
	}

	// Wait for completion
	result, err := agent.Wait(ctx)
	if err != nil {
		c.releaseIssueWithError(ctx, issue.ID, instanceID, fmt.Sprintf("Agent execution failed: %v", err))
		return "", fmt.Errorf("agent execution failed: %w", err)
	}

	// Process results using ResultsProcessor
	supervisor, err := ai.NewSupervisor(&ai.Config{
		Store: c.storage,
	})
	if err != nil {
		// Continue without AI supervision
		fmt.Fprintf(os.Stderr, "Warning: AI supervisor not available: %v (continuing without AI analysis)\n", err)
		supervisor = nil
	}

	processor, err := executor.NewResultsProcessor(&executor.ResultsProcessorConfig{
		Store:              c.storage,
		Supervisor:         supervisor,
		EnableQualityGates: true,
		WorkingDir:         ".",
		Actor:              instanceID,
	})
	if err != nil {
		c.releaseIssueWithError(ctx, issue.ID, instanceID, fmt.Sprintf("Failed to create results processor: %v", err))
		return "", fmt.Errorf("failed to create results processor: %w", err)
	}

	procResult, err := processor.ProcessAgentResult(ctx, issue, result)
	if err != nil {
		c.releaseIssueWithError(ctx, issue.ID, instanceID, fmt.Sprintf("Failed to process results: %v", err))
		return "", fmt.Errorf("failed to process results: %w", err)
	}

	// Build response
	var response string
	if procResult.Completed {
		response = fmt.Sprintf("✓ Issue %s completed successfully!\n", issue.ID)
	} else if !procResult.GatesPassed {
		response = fmt.Sprintf("✗ Issue %s blocked by quality gates\n", issue.ID)
	} else if !result.Success {
		response = fmt.Sprintf("✗ Worker failed for issue %s\n", issue.ID)
	} else {
		response = fmt.Sprintf("⚡ Issue %s partially complete (left open)\n", issue.ID)
	}

	if len(procResult.DiscoveredIssues) > 0 {
		response += fmt.Sprintf("\nCreated %d follow-on issues: %v\n", len(procResult.DiscoveredIssues), procResult.DiscoveredIssues)
	}

	return response, nil
}

// releaseIssueWithError releases an issue and adds an error comment
func (c *ConversationHandler) releaseIssueWithError(ctx context.Context, issueID, actor, errMsg string) {
	// Add error comment
	if err := c.storage.AddComment(ctx, issueID, actor, errMsg); err != nil {
		fmt.Fprintf(os.Stderr, "warning: failed to add error comment: %v\n", err)
	}

	// Release the execution state
	if err := c.storage.ReleaseIssue(ctx, issueID); err != nil {
		fmt.Fprintf(os.Stderr, "warning: failed to release issue: %v\n", err)
	}
}

// ClearHistory clears the conversation history
func (c *ConversationHandler) ClearHistory() {
	c.history = make([]anthropic.MessageParam, 0)
}

// processNaturalLanguage processes natural language input from the REPL
func (r *REPL) processNaturalLanguage(input string) error {
	// Initialize conversation handler if needed
	if r.conversation == nil {
		handler, err := NewConversationHandler(r.store)
		if err != nil {
			yellow := color.New(color.FgYellow).SprintFunc()
			fmt.Printf("\n%s AI conversation requires ANTHROPIC_API_KEY environment variable.\n", yellow("Note:"))
			fmt.Println("Set your API key and restart the REPL to enable AI features.")
			fmt.Println()
			return nil
		}
		r.conversation = handler
	}

	// Show thinking indicator
	gray := color.New(color.FgHiBlack).SprintFunc()
	fmt.Printf("%s\n", gray("Thinking..."))

	// Send message to AI
	response, err := r.conversation.SendMessage(r.ctx, input)
	if err != nil {
		return fmt.Errorf("AI conversation failed: %w", err)
	}

	// Display response
	fmt.Println()
	fmt.Println(response)
	fmt.Println()

	return nil
}
