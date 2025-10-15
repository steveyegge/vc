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

You have access to these tools:
- create_issue: Create a new issue (bug, feature, task, chore)
- create_epic: Create an epic (container for related issues)
- add_child_to_epic: Add an issue as a child of an epic, optionally marking it as blocking
- get_ready_work: Query issues ready to work on (no blockers)
- get_issue: Get detailed information about an issue

When users describe work, proactively create appropriate issues. Examples:
- "Add Docker support" → Create feature issue
- "Fix the login bug" → Create bug issue
- "Build auth system" → Create epic with child tasks
- "Refactor the database layer" → Create chore issue

Be helpful, concise, and action-oriented. Use tools to create issues immediately when the user describes work.`
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
