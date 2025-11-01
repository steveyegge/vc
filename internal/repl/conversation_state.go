package repl

import (
	"context"
	"fmt"
	"os"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/steveyegge/vc/internal/storage"
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
	actor   string // Actor name for this conversation handler
}

// NewConversationHandler creates a new conversation handler
func NewConversationHandler(store storage.Storage, actor string) (*ConversationHandler, error) {
	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey == "" {
		return nil, fmt.Errorf("ANTHROPIC_API_KEY not set")
	}

	if actor == "" {
		actor = "user"
	}

	client := anthropic.NewClient(option.WithAPIKey(apiKey))

	return &ConversationHandler{
		client:  &client,
		model:   "claude-sonnet-4-5-20250929",
		history: make([]anthropic.MessageParam, 0),
		storage: store,
		actor:   actor,
	}, nil
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
						// Log detailed error information for debugging
						fmt.Fprintf(os.Stderr, "REPL tool execution error: tool=%s input=%v error=%v\n", toolUse.Name, toolUse.Input, err)
						toolResults = append(toolResults, anthropic.NewToolResultBlock(toolUse.ID, fmt.Sprintf("Error: %v", err), true))
					} else {
						// Log successful tool execution for debugging
						fmt.Fprintf(os.Stderr, "REPL tool execution success: tool=%s\n", toolUse.Name)
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

// ClearHistory clears the conversation history
func (c *ConversationHandler) ClearHistory() {
	c.history = make([]anthropic.MessageParam, 0)
}
