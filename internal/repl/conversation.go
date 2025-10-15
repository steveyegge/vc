package repl

import (
	"context"
	"fmt"
	"os"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/fatih/color"
)

// ConversationHandler handles AI conversations
type ConversationHandler struct {
	client  *anthropic.Client
	model   string
	history []anthropic.MessageParam
}

// NewConversationHandler creates a new conversation handler
func NewConversationHandler() (*ConversationHandler, error) {
	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey == "" {
		return nil, fmt.Errorf("ANTHROPIC_API_KEY not set")
	}

	client := anthropic.NewClient(option.WithAPIKey(apiKey))

	return &ConversationHandler{
		client:  &client,
		model:   "claude-sonnet-4-5-20250929",
		history: make([]anthropic.MessageParam, 0),
	}, nil
}

// systemPrompt returns the system prompt for VC conversations
func (c *ConversationHandler) systemPrompt() string {
	return `You are VC (VibeCoder), an AI-orchestrated coding agent colony system.

You are having a conversation with a developer through the VC REPL. You can:
1. Answer questions about their code and project
2. Help plan and break down work
3. Eventually: create issues and spawn worker agents to execute tasks

Current capabilities:
- You can discuss code, architecture, and planning
- You have visibility into the VC issue tracker
- Slash commands are available: /status, /ready, /blocked, /help, /exit

Not yet implemented (coming soon):
- Creating issues directly from conversation
- Spawning worker agents to execute tasks
- Real-time visibility into agent work

Be helpful, concise, and action-oriented. When a user asks to do work, acknowledge it
and explain that the agent spawning feature is coming soon. For now, guide them to use
the existing commands to see project state.`
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

	// Call Claude API
	response, err := c.client.Messages.New(ctx, anthropic.MessageNewParams{
		Model:     anthropic.Model(c.model),
		MaxTokens: 4096,
		Messages:  c.history,
	})

	if err != nil {
		return "", fmt.Errorf("API call failed: %w", err)
	}

	// Extract response text
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

// ClearHistory clears the conversation history
func (c *ConversationHandler) ClearHistory() {
	c.history = make([]anthropic.MessageParam, 0)
}

// processNaturalLanguage processes natural language input from the REPL
func (r *REPL) processNaturalLanguage(input string) error {
	// Initialize conversation handler if needed
	if r.conversation == nil {
		handler, err := NewConversationHandler()
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
