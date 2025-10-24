package git

import (
	"context"
	"fmt"
	"strings"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/steveyegge/vc/internal/ai"
)

// MessageGenerator generates commit messages using AI.
type MessageGenerator struct {
	client        *anthropic.Client
	model         string
	retryAttempts int
}

// NewMessageGenerator creates a new MessageGenerator.
func NewMessageGenerator(client *anthropic.Client, model string) *MessageGenerator {
	return &MessageGenerator{
		client:        client,
		model:         model,
		retryAttempts: 3,
	}
}

// GenerateCommitMessage generates a commit message using AI.
func (m *MessageGenerator) GenerateCommitMessage(ctx context.Context, req CommitMessageRequest) (*CommitMessageResponse, error) {
	prompt := m.buildPrompt(req)

	var response *anthropic.Message
	err := m.retryWithBackoff(ctx, "commit-message", func(attemptCtx context.Context) error {
		resp, apiErr := m.client.Messages.New(attemptCtx, anthropic.MessageNewParams{
			Model:     anthropic.Model(m.model),
			MaxTokens: 2048,
			Messages: []anthropic.MessageParam{
				anthropic.NewUserMessage(anthropic.NewTextBlock(prompt)),
			},
		})
		if apiErr != nil {
			return apiErr
		}
		response = resp
		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("failed to generate commit message: %w", err)
	}

	// Extract text from response
	var responseText string
	for _, block := range response.Content {
		if block.Type == "text" {
			responseText += block.Text
		}
	}

	// Parse the JSON response
	parseResult := ai.Parse[CommitMessageResponse](responseText, ai.ParseOptions{
		Context:   "commit message response",
		LogErrors: ai.BoolPtr(true),
	})

	if !parseResult.Success {
		return nil, fmt.Errorf("failed to parse commit message response: %s (response: %s)", parseResult.Error, responseText)
	}

	return &parseResult.Data, nil
}

// buildPrompt constructs the prompt for commit message generation.
func (m *MessageGenerator) buildPrompt(req CommitMessageRequest) string {
	var prompt strings.Builder

	prompt.WriteString("You are a commit message generator for an AI-supervised coding agent.\n\n")
	prompt.WriteString("Generate a clear, concise commit message following conventional commits format.\n\n")

	prompt.WriteString("## Issue Context\n\n")
	prompt.WriteString(fmt.Sprintf("**Issue ID**: %s\n", req.IssueID))
	prompt.WriteString(fmt.Sprintf("**Title**: %s\n", req.IssueTitle))
	if req.IssueDescription != "" {
		prompt.WriteString(fmt.Sprintf("**Description**: %s\n", req.IssueDescription))
	}
	prompt.WriteString("\n")

	prompt.WriteString("## Changed Files\n\n")
	if len(req.ChangedFiles) > 0 {
		for _, file := range req.ChangedFiles {
			prompt.WriteString(fmt.Sprintf("- %s\n", file))
		}
	} else {
		prompt.WriteString("(no files listed)\n")
	}
	prompt.WriteString("\n")

	if req.Diff != "" {
		prompt.WriteString("## Diff\n\n")
		prompt.WriteString("```diff\n")
		// Truncate diff if too large (keep first 10000 chars)
		diff := req.Diff
		if len(diff) > 10000 {
			diff = diff[:10000] + "\n... (truncated)"
		}
		prompt.WriteString(diff)
		prompt.WriteString("\n```\n\n")
	}

	prompt.WriteString("## Instructions\n\n")
	prompt.WriteString("Generate a commit message with:\n")
	prompt.WriteString("1. **Subject**: One-line summary (50 chars max), format: `type(scope): description`\n")
	prompt.WriteString("   - Types: feat, fix, docs, refactor, test, chore\n")
	prompt.WriteString("   - Include issue ID in subject: e.g., `feat(git): implement auto-commit (vc-119)`\n")
	prompt.WriteString("2. **Body**: Detailed explanation of what changed and why (wrap at 72 chars)\n")
	prompt.WriteString("3. **Reasoning**: Brief explanation of your commit message choice\n\n")

	prompt.WriteString("Guidelines:\n")
	prompt.WriteString("- Focus on the 'why' not just the 'what'\n")
	prompt.WriteString("- Be specific about the functionality added/changed\n")
	prompt.WriteString("- Use imperative mood: 'add feature' not 'added feature'\n")
	prompt.WriteString("- Keep subject concise, put details in body\n\n")

	prompt.WriteString("Respond with JSON:\n")
	prompt.WriteString("```json\n")
	prompt.WriteString("{\n")
	prompt.WriteString("  \"subject\": \"feat(scope): concise description (vc-XXX)\",\n")
	prompt.WriteString("  \"body\": \"Detailed explanation of changes.\\n\\nWhy this change was needed.\",\n")
	prompt.WriteString("  \"reasoning\": \"Why I chose this message\"\n")
	prompt.WriteString("}\n")
	prompt.WriteString("```\n")

	return prompt.String()
}

// retryWithBackoff retries an operation with exponential backoff.
// This is borrowed from the supervisor pattern in internal/ai/supervisor.go
func (m *MessageGenerator) retryWithBackoff(ctx context.Context, operation string, fn func(context.Context) error) error {
	var lastErr error

	for attempt := 1; attempt <= m.retryAttempts; attempt++ {
		err := fn(ctx)
		if err == nil {
			return nil
		}

		lastErr = err

		// Check if context is canceled
		if ctx.Err() != nil {
			return fmt.Errorf("%s canceled: %w", operation, ctx.Err())
		}

		// Don't retry on last attempt
		if attempt == m.retryAttempts {
			break
		}

		// Simple backoff: just try again immediately for now
		// TODO: Add exponential backoff like supervisor.go if needed
	}

	return fmt.Errorf("%s failed after %d attempts: %w", operation, m.retryAttempts, lastErr)
}
