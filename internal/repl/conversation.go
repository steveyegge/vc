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

You are having a conversation with a developer through the VC REPL. Your role is to be their intelligent assistant that understands their intent, manages work through an issue tracker, and orchestrates coding agents to execute tasks.

# SYSTEM ONTOLOGY

You have access to these conversational tools:

## Issue Management
- create_issue: Create a new issue (bug, feature, task, chore)
  • Use when: User describes new work to be done
  • Returns: Issue ID for the created issue

- create_epic: Create an epic (container for related work)
  • Use when: User describes a large feature or initiative
  • Returns: Epic ID for tracking sub-tasks

- add_child_to_epic: Add an issue as a child of an epic
  • Use when: Breaking down an epic into smaller tasks
  • Parameters: epic_id, child_issue_id, blocks (default: true)

- search_issues: Search issues by text query
  • Use when: User asks about specific work or topics
  • Parameters: query (required), status (optional), limit (default: 10)

## Work Execution
- continue_execution: Execute an issue by spawning a coding agent (THE VIBECODER PRIMITIVE)
  • Use when: User wants to start work on something
  • Parameters: issue_id (optional - picks next ready if not provided), async (default: false)
  • This is the core action - it spawns an agent, processes results, creates follow-on issues

- continue_until_blocked: Autonomously execute ready issues in a loop until blocked
  • Use when: User wants VC to work through multiple issues without intervention
  • Parameters: max_iterations (default: 10), timeout_minutes (default: 120), error_threshold (default: 3)
  • Executes issues sequentially, stops when no ready work or limits reached
  • Provides summary of completed/failed issues

- get_ready_work: Get issues ready to work on (no blockers)
  • Use when: User asks what's ready or what to do next
  • Parameters: limit (default: 5)

- get_blocked_issues: Get issues blocked by dependencies
  • Use when: User asks what's blocked or stuck
  • Parameters: limit (default: 10)

## Status & Monitoring
- get_status: Get overall project status and statistics
  • Use when: User asks about project health or progress
  • No parameters required

- get_recent_activity: View recent agent execution activity
  • Use when: User asks what's been happening
  • Parameters: limit (default: 20), issue_id (optional filter)

- get_issue: Get detailed information about a specific issue
  • Use when: User references a specific issue ID
  • Parameters: issue_id (required)

# CONVERSATIONAL INTENT PATTERNS

Map natural language to tool calls:

"let's continue" → continue_execution()
"keep going" → continue_execution()
"continue working" → continue_execution()
"work on vc-123" → continue_execution(issue_id: "vc-123")
"execute vc-456" → continue_execution(issue_id: "vc-456")

"keep working until blocked" → continue_until_blocked()
"work through everything" → continue_until_blocked()
"autonomous mode" → continue_until_blocked()
"execute all ready work" → continue_until_blocked()

"what's ready?" → get_ready_work(5)
"show ready work" → get_ready_work(5)
"what can I work on?" → get_ready_work(5)
"what's next?" → get_ready_work(5)

"what's blocked?" → get_blocked_issues(10)
"show blockers" → get_blocked_issues(10)
"what's stuck?" → get_blocked_issues(10)

"how's the project?" → get_status()
"show status" → get_status()
"project health" → get_status()

"what's happening?" → get_recent_activity(20)
"recent activity" → get_recent_activity(20)
"what have agents been doing?" → get_recent_activity(20)

"add authentication" → create_issue(type: feature)
"fix the login bug" → create_issue(type: bug)
"refactor database layer" → create_issue(type: chore)
"implement user management" → create_issue(type: task)

"build a payment system" → create_epic() → create multiple child issues
"add real-time notifications" → create_epic() → break down into tasks

# BEHAVIORAL GUIDELINES

1. BE PROACTIVE
   • When user describes work, create issues immediately without asking for permission
   • But DO ask before executing work (spawning agents) - it's a resource-intensive operation
   • Suggest next steps based on context
   • Example: "I created issue vc-123. Would you like me to start working on it?"

2. BE CONTEXTUAL
   • Remember what was just discussed in this conversation
   • Use pronouns and references naturally ("that issue", "the feature we just talked about")
   • Connect related work automatically

3. BE ACTION-ORIENTED
   • Use tools to DO things, not just explain what tools exist
   • When user wants to work, call continue_execution - don't describe how it works
   • Show results, not instructions

4. BE CONVERSATIONAL
   • No command syntax in responses (never say "Run 'bd show vc-123'")
   • Natural language only
   • Friendly but professional tone

5. BE TRANSPARENT
   • Tell user what you're doing ("I'll create an issue for that...")
   • Explain tool results clearly
   • If something fails, explain why and suggest alternatives

# EXAMPLE CONVERSATIONS

Example 1 - Creating and Executing Work:
User: "We need to add Docker support to the project"
AI: "I'll create a feature issue for adding Docker support. Created vc-234: Add Docker support to the project. Would you like me to start working on it?"
User: "Yes, go ahead"
AI: "Starting work on vc-234... I've spawned an agent to implement Docker support."

Example 2 - Context Awareness:
User: "What's ready to work on?"
AI: "You have 3 issues ready: vc-201 (Add logging), vc-202 (Fix auth bug), vc-203 (Update docs)"
User: "Let's do the auth one"
AI: "Starting work on the auth bug fix..."

Example 3 - Breaking Down Work:
User: "I want to build a real-time notification system"
AI: "That sounds like a substantial feature. I'll create an epic for it and break it down into tasks. Created epic vc-240: Real-time notification system. I've added 4 child tasks: WebSocket server (vc-241), client library (vc-242), notification UI (vc-243), and database schema (vc-244). Would you like to see them or start working on the first one?"

Example 4 - Status Inquiry:
User: "How are things going?"
AI: "The project has 45 total issues: 12 open, 3 in progress, 2 blocked, 28 closed. You have 12 issues ready to work on. Average lead time is 2.3 hours."
User: "What's blocked?"
AI: "You have 2 blocked issues: vc-156 is blocked by vc-155, and vc-178 is blocked by vc-177 and vc-179"

# CRITICAL RULES

• ALWAYS use tools when appropriate - don't just describe them
• NEVER use CLI command syntax in responses (no "run bd show...")
• CREATE issues proactively when work is described
• REMEMBER context from earlier in the conversation
• BE CONCISE but friendly
• If a tool fails, explain clearly and suggest alternatives

You are the conversational interface to a powerful AI-orchestrated development system. Make it feel natural and effortless.`
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
		{
			Name:        "continue_until_blocked",
			Description: anthropic.String("Autonomously execute ready issues in a loop until no more work is available. This enables supervised autonomous operation where VC works through multiple issues without intervention."),
			InputSchema: anthropic.ToolInputSchemaParam{
				Properties: map[string]interface{}{
					"max_iterations":  map[string]interface{}{"type": "integer", "minimum": 1, "maximum": 100, "description": "Maximum number of issues to execute (default: 10)"},
					"timeout_minutes": map[string]interface{}{"type": "integer", "minimum": 1, "maximum": 480, "description": "Maximum time to run in minutes (default: 120)"},
					"error_threshold": map[string]interface{}{"type": "integer", "minimum": 1, "maximum": 10, "description": "Stop after this many consecutive errors (default: 3)"},
				},
			},
		},
	}

	tools := make([]anthropic.ToolUnionParam, len(toolParams))
	for i := range toolParams {
		// Create a copy to avoid capturing loop variable address
		tool := toolParams[i]
		tools[i] = anthropic.ToolUnionParam{OfTool: &tool}
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

// executeTool executes a tool and returns the result.
// This dispatcher routes tool calls from the AI to the appropriate handler function.
// Each tool has typed validation and error handling.
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

// toolContinueExecution executes work on an issue (THE VIBECODER PRIMITIVE).
// Spawns a coding agent, processes results, runs quality gates, and creates follow-on issues.
// This is the core operation of VibeCoder - AI-supervised execution of coding work.
// Input: issue_id (optional - picks next ready if not provided), async (not yet implemented)
// Returns: Execution status, completion details, and any discovered follow-on work
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

		// Validate issue can be executed
		if errMsg, err := c.validateIssueForExecution(ctx, issue); err != nil {
			return "", err
		} else if errMsg != "" {
			return errMsg, nil
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

	// Execute the issue using shared execution logic
	result, err := c.executeIssue(ctx, issue)
	if err != nil {
		return "", err
	}

	// Build response (format differs from batch execution in continue_until_blocked)
	var response string
	if result.Completed {
		response = fmt.Sprintf("✓ Issue %s completed successfully!\n", issue.ID)
	} else if !result.GatesPassed {
		response = fmt.Sprintf("✗ Issue %s blocked by quality gates\n", issue.ID)
	} else {
		response = fmt.Sprintf("⚡ Issue %s partially complete (left open)\n", issue.ID)
	}

	if len(result.DiscoveredIssues) > 0 {
		response += fmt.Sprintf("\nCreated %d follow-on issues: %v\n", len(result.DiscoveredIssues), result.DiscoveredIssues)
	}

	return response, nil
}

// toolContinueUntilBlocked autonomously executes ready issues in a loop until blocked.
// This is the autonomous execution mode that enables VC to work through multiple issues
// without manual intervention. It includes watchdog monitoring, error tracking, and
// graceful shutdown capabilities.
// Input: max_iterations (default: 10), timeout_minutes (default: 120), error_threshold (default: 3)
// Returns: Summary of execution including issues completed, errors, and stop reason
func (c *ConversationHandler) toolContinueUntilBlocked(ctx context.Context, input map[string]interface{}) (string, error) {
	// Parse parameters with defaults
	maxIterations := 10
	if mi, ok := input["max_iterations"].(float64); ok {
		maxIterations = int(mi)
	}

	timeoutMinutes := 120
	if tm, ok := input["timeout_minutes"].(float64); ok {
		timeoutMinutes = int(tm)
	}

	errorThreshold := 3
	if et, ok := input["error_threshold"].(float64); ok {
		errorThreshold = int(et)
	}

	// Create timeout context
	timeoutDuration := time.Duration(timeoutMinutes) * time.Minute
	ctx, cancel := context.WithTimeout(ctx, timeoutDuration)
	defer cancel()

	// Track execution state with three categories
	var (
		completedIssues   []string
		partialIssues     []string
		failedIssues      []string
		consecutiveErrors int
		iteration         int
		startTime         = time.Now()
	)

	// Execution loop
	for iteration = 0; iteration < maxIterations; iteration++ {
		// Check for context cancellation (timeout or Ctrl+C)
		select {
		case <-ctx.Done():
			elapsed := time.Since(startTime)
			return c.formatContinueLoopResult(completedIssues, partialIssues, failedIssues, iteration, "timeout or interruption", elapsed), nil
		default:
		}

		// Check for ready work
		readyIssues, err := c.storage.GetReadyWork(ctx, types.WorkFilter{
			Status: types.StatusOpen,
			Limit:  1,
		})
		if err != nil {
			return "", fmt.Errorf("failed to check for ready work: %w", err)
		}

		if len(readyIssues) == 0 {
			// No more work - successful completion
			elapsed := time.Since(startTime)
			return c.formatContinueLoopResult(completedIssues, partialIssues, failedIssues, iteration, "no more ready work", elapsed), nil
		}

		issue := readyIssues[0]

		// Execute the issue using the same logic as continue_execution
		executionResult, execErr := c.executeIssue(ctx, issue)

		if execErr != nil {
			// Execution error (spawn failed, agent crashed, etc.)
			failedIssues = append(failedIssues, issue.ID)
			consecutiveErrors++

			// Check error threshold
			if consecutiveErrors >= errorThreshold {
				elapsed := time.Since(startTime)
				return c.formatContinueLoopResult(completedIssues, partialIssues, failedIssues, iteration+1, fmt.Sprintf("error threshold exceeded (%d consecutive errors)", consecutiveErrors), elapsed), nil
			}
		} else if !executionResult.GatesPassed {
			// Quality gates failed - actual failure
			failedIssues = append(failedIssues, issue.ID)
			consecutiveErrors++

			// Check error threshold
			if consecutiveErrors >= errorThreshold {
				elapsed := time.Since(startTime)
				return c.formatContinueLoopResult(completedIssues, partialIssues, failedIssues, iteration+1, fmt.Sprintf("error threshold exceeded (%d consecutive errors)", consecutiveErrors), elapsed), nil
			}
		} else if executionResult.Completed {
			// Issue closed successfully
			completedIssues = append(completedIssues, issue.ID)
			consecutiveErrors = 0 // Reset error counter on success
		} else {
			// Issue left open but work was done (partial completion)
			partialIssues = append(partialIssues, issue.ID)
			consecutiveErrors = 0 // Reset error counter on partial success
		}

		// Progress update (will be visible in the conversation)
		// The AI will see this in the tool result
	}

	// Reached max iterations
	elapsed := time.Since(startTime)
	return c.formatContinueLoopResult(completedIssues, partialIssues, failedIssues, iteration, "max iterations reached", elapsed), nil
}

// validateIssueForExecution validates that an issue can be executed.
// Returns a user-facing error message if the issue cannot be executed, or empty string if valid.
// Returns a system error if validation itself fails (e.g., database error).
func (c *ConversationHandler) validateIssueForExecution(ctx context.Context, issue *types.Issue) (string, error) {
	switch issue.Status {
	case types.StatusClosed:
		return fmt.Sprintf("Cannot execute issue %s: already closed", issue.ID), nil
	case types.StatusInProgress:
		return fmt.Sprintf("Cannot execute issue %s: already in progress (may be claimed by another executor)", issue.ID), nil
	case types.StatusBlocked:
		// Get dependencies to show what's blocking it
		deps, err := c.storage.GetDependencies(ctx, issue.ID)
		if err != nil {
			return "", fmt.Errorf("failed to get dependencies for issue %s: %w", issue.ID, err)
		}

		if len(deps) > 0 {
			var blockingIDs []string
			for _, dep := range deps {
				if dep.Status != types.StatusClosed {
					blockingIDs = append(blockingIDs, dep.ID)
				}
			}
			if len(blockingIDs) > 0 {
				return fmt.Sprintf("Cannot execute issue %s: blocked by %v", issue.ID, blockingIDs), nil
			}
		}
		return fmt.Sprintf("Cannot execute issue %s: currently blocked", issue.ID), nil
	}

	return "", nil
}

// executeIssue executes a single issue and returns the result.
// This is extracted from toolContinueExecution to enable reuse in the autonomous loop.
type issueExecutionResult struct {
	Completed        bool
	GatesPassed      bool
	DiscoveredIssues []string
}

func (c *ConversationHandler) executeIssue(ctx context.Context, issue *types.Issue) (*issueExecutionResult, error) {
	// Validate issue can be executed (prevent race conditions)
	if errMsg, err := c.validateIssueForExecution(ctx, issue); err != nil {
		return nil, err
	} else if errMsg != "" {
		return nil, fmt.Errorf("%s", errMsg)
	}

	// Claim the issue
	instanceID := fmt.Sprintf("conversation-%s", AIActor)
	if err := c.storage.ClaimIssue(ctx, issue.ID, instanceID); err != nil {
		return nil, fmt.Errorf("failed to claim issue %s: %w", issue.ID, err)
	}

	// Update execution state to executing
	if err := c.storage.UpdateExecutionState(ctx, issue.ID, types.ExecutionStateExecuting); err != nil {
		// Log warning but continue
		fmt.Fprintf(os.Stderr, "warning: failed to update execution state: %v\n", err)
	}

	// Gather context for comprehensive prompt
	gatherer := executor.NewContextGatherer(c.storage)
	promptCtx, err := gatherer.GatherContext(ctx, issue, nil)
	if err != nil {
		c.releaseIssueWithError(ctx, issue.ID, instanceID, fmt.Sprintf("Failed to gather context: %v", err))
		return nil, fmt.Errorf("failed to gather context: %w", err)
	}

	// Build comprehensive prompt using PromptBuilder
	builder, err := executor.NewPromptBuilder()
	if err != nil {
		c.releaseIssueWithError(ctx, issue.ID, instanceID, fmt.Sprintf("Failed to create prompt builder: %v", err))
		return nil, fmt.Errorf("failed to create prompt builder: %w", err)
	}

	prompt, err := builder.BuildPrompt(promptCtx)
	if err != nil {
		c.releaseIssueWithError(ctx, issue.ID, instanceID, fmt.Sprintf("Failed to build prompt: %v", err))
		return nil, fmt.Errorf("failed to build prompt: %w", err)
	}

	// Log prompt for debugging if VC_DEBUG_PROMPTS is set
	if os.Getenv("VC_DEBUG_PROMPTS") != "" {
		fmt.Fprintf(os.Stderr, "\n=== AGENT PROMPT ===\n%s\n=== END PROMPT ===\n\n", prompt)
	}

	// Spawn agent
	agentCfg := executor.AgentConfig{
		Type:       executor.AgentTypeClaudeCode,
		WorkingDir: ".",
		Issue:      issue,
		StreamJSON: false,
		Timeout:    30 * time.Minute,
	}

	agent, err := executor.SpawnAgent(ctx, agentCfg, prompt)
	if err != nil {
		c.releaseIssueWithError(ctx, issue.ID, instanceID, fmt.Sprintf("Failed to spawn agent: %v", err))
		return nil, fmt.Errorf("failed to spawn agent: %w", err)
	}

	// Wait for completion
	result, err := agent.Wait(ctx)
	if err != nil {
		c.releaseIssueWithError(ctx, issue.ID, instanceID, fmt.Sprintf("Agent execution failed: %v", err))
		return nil, fmt.Errorf("agent execution failed: %w", err)
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
		return nil, fmt.Errorf("failed to create results processor: %w", err)
	}

	procResult, err := processor.ProcessAgentResult(ctx, issue, result)
	if err != nil {
		c.releaseIssueWithError(ctx, issue.ID, instanceID, fmt.Sprintf("Failed to process results: %v", err))
		return nil, fmt.Errorf("failed to process results: %w", err)
	}

	return &issueExecutionResult{
		Completed:        procResult.Completed,
		GatesPassed:      procResult.GatesPassed,
		DiscoveredIssues: procResult.DiscoveredIssues,
	}, nil
}

// formatContinueLoopResult formats the result of a continue_until_blocked execution.
// Displays three categories: completed (closed), partial (work done, left open), and failed (errors).
func (c *ConversationHandler) formatContinueLoopResult(completed, partial, failed []string, iterations int, stopReason string, elapsed time.Duration) string {
	var result string

	result += fmt.Sprintf("⚡ Autonomous Execution Complete\n\n")
	result += fmt.Sprintf("Stop Reason: %s\n", stopReason)
	result += fmt.Sprintf("Iterations: %d\n", iterations)
	result += fmt.Sprintf("Elapsed Time: %s\n", elapsed.Round(time.Second))
	result += fmt.Sprintf("\n")

	// Completed issues (fully closed)
	result += fmt.Sprintf("Completed: %d issues\n", len(completed))
	if len(completed) > 0 {
		result += fmt.Sprintf("  %v\n", completed)
	}

	// Partial completion (work done, left open for more)
	result += fmt.Sprintf("Partial: %d issues (work done, left open)\n", len(partial))
	if len(partial) > 0 {
		result += fmt.Sprintf("  %v\n", partial)
	}

	// Failed issues (execution errors or quality gates failed)
	result += fmt.Sprintf("Failed: %d issues\n", len(failed))
	if len(failed) > 0 {
		result += fmt.Sprintf("  %v\n", failed)
	}

	return result
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
