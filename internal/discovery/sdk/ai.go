package sdk

import (
	"context"
	"fmt"
	"os"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
)

// AIRequest represents a request to the AI supervisor.
type AIRequest struct {
	// Prompt is the question or task for the AI
	Prompt string

	// Model specifies which AI model to use
	// Default: "claude-sonnet-4-5-20250929"
	Model string

	// MaxTokens limits the response length (default: 4096)
	MaxTokens int

	// Temperature controls randomness (0.0-1.0, default: 1.0)
	Temperature float64

	// SystemPrompt provides context and instructions
	SystemPrompt string
}

// AIResponse represents the AI's response.
type AIResponse struct {
	// Text is the AI's response text
	Text string

	// TokensUsed is the total tokens consumed (input + output)
	TokensUsed int

	// EstimatedCost is the approximate cost in USD
	EstimatedCost float64

	// Model is the model that was used
	Model string
}

// CallAI calls the AI supervisor with a prompt and returns the response.
//
// Example:
//
//	response, err := sdk.CallAI(ctx, sdk.AIRequest{
//		Prompt: "Is this code secure?\n" + code,
//		Model:  "claude-sonnet-4-5-20250929",
//	})
//
//	if err != nil {
//		return err
//	}
//
//	fmt.Println(response.Text)
func CallAI(ctx context.Context, req AIRequest) (*AIResponse, error) {
	// Check for API key
	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey == "" {
		return nil, fmt.Errorf("ANTHROPIC_API_KEY environment variable not set")
	}

	// Set defaults
	if req.Model == "" {
		req.Model = "claude-sonnet-4-5-20250929"
	}
	if req.MaxTokens == 0 {
		req.MaxTokens = 4096
	}
	if req.Temperature == 0 {
		req.Temperature = 1.0
	}
	if req.SystemPrompt == "" {
		req.SystemPrompt = "You are an AI code quality assistant helping analyze code for potential issues."
	}

	// Create client
	client := anthropic.NewClient(option.WithAPIKey(apiKey))

	// Build messages
	messages := []anthropic.MessageParam{
		anthropic.NewUserMessage(anthropic.NewTextBlock(req.Prompt)),
	}

	// Call API
	response, err := client.Messages.New(ctx, anthropic.MessageNewParams{
		Model:     anthropic.Model(req.Model),
		MaxTokens: int64(req.MaxTokens),
		Messages:  messages,
	})

	if err != nil {
		return nil, fmt.Errorf("AI API call failed: %w", err)
	}

	// Extract text from response
	var text string
	for _, block := range response.Content {
		if block.Type == "text" {
			text += block.Text
		}
	}

	// Calculate tokens and cost
	inputTokens := response.Usage.InputTokens
	outputTokens := response.Usage.OutputTokens
	totalTokens := inputTokens + outputTokens

	// Pricing for Claude Sonnet 4.5 (as of 2025)
	// Input: $3 per million tokens
	// Output: $15 per million tokens
	inputCost := float64(inputTokens) / 1_000_000.0 * 3.0
	outputCost := float64(outputTokens) / 1_000_000.0 * 15.0
	estimatedCost := inputCost + outputCost

	return &AIResponse{
		Text:          text,
		TokensUsed:    int(totalTokens),
		EstimatedCost: estimatedCost,
		Model:         req.Model,
	}, nil
}

// AssessCode asks the AI to assess code quality and return structured feedback.
//
// Example:
//
//	assessment, err := sdk.AssessCode(ctx, code, "security", sdk.AssessmentOptions{
//		Focus: "Look for SQL injection vulnerabilities",
//	})
func AssessCode(ctx context.Context, code string, category string, opts AssessmentOptions) (*CodeAssessment, error) {
	prompt := buildAssessmentPrompt(code, category, opts)

	response, err := CallAI(ctx, AIRequest{
		Prompt:      prompt,
		Model:       opts.Model,
		MaxTokens:   opts.MaxTokens,
		Temperature: opts.Temperature,
		SystemPrompt: "You are an expert code reviewer. Analyze the provided code and identify specific, actionable issues. Focus on real problems, not nitpicks.",
	})

	if err != nil {
		return nil, err
	}

	// Parse response into structured assessment
	// For now, we return a simple structure
	// A more sophisticated implementation would parse the AI's response
	return &CodeAssessment{
		Category:      category,
		Summary:       response.Text,
		Issues:        []string{}, // TODO: Parse issues from response
		Recommendations: []string{}, // TODO: Parse recommendations
		Confidence:    0.7, // Default confidence
		TokensUsed:    response.TokensUsed,
		EstimatedCost: response.EstimatedCost,
	}, nil
}

// AssessmentOptions configures code assessment.
type AssessmentOptions struct {
	// Focus provides specific guidance on what to look for
	Focus string

	// Model to use (default: claude-sonnet-4-5-20250929)
	Model string

	// MaxTokens for the response (default: 4096)
	MaxTokens int

	// Temperature (default: 1.0)
	Temperature float64

	// Context provides additional background information
	Context string
}

// CodeAssessment represents structured code assessment results.
type CodeAssessment struct {
	Category        string
	Summary         string
	Issues          []string
	Recommendations []string
	Confidence      float64
	TokensUsed      int
	EstimatedCost   float64
}

// buildAssessmentPrompt constructs a prompt for code assessment.
func buildAssessmentPrompt(code string, category string, opts AssessmentOptions) string {
	prompt := fmt.Sprintf("Analyze the following code for %s issues:\n\n", category)

	if opts.Focus != "" {
		prompt += fmt.Sprintf("Focus: %s\n\n", opts.Focus)
	}

	if opts.Context != "" {
		prompt += fmt.Sprintf("Context: %s\n\n", opts.Context)
	}

	prompt += "```\n" + code + "\n```\n\n"
	prompt += "Provide a summary of issues found, if any. Be specific and actionable."

	return prompt
}

// BatchAssessCode assesses multiple code snippets in a single AI call.
// This is more efficient than calling AssessCode multiple times.
//
// Example:
//
//	snippets := []sdk.CodeSnippet{
//		{ID: "func1", Code: code1, Context: "API handler"},
//		{ID: "func2", Code: code2, Context: "Database query"},
//	}
//
//	assessments, err := sdk.BatchAssessCode(ctx, snippets, "security", sdk.AssessmentOptions{})
func BatchAssessCode(ctx context.Context, snippets []CodeSnippet, category string, opts AssessmentOptions) (map[string]*CodeAssessment, error) {
	// Build batch prompt
	prompt := fmt.Sprintf("Analyze the following code snippets for %s issues:\n\n", category)

	if opts.Focus != "" {
		prompt += fmt.Sprintf("Focus: %s\n\n", opts.Focus)
	}

	for i, snippet := range snippets {
		prompt += fmt.Sprintf("## Snippet %d: %s\n", i+1, snippet.ID)
		if snippet.Context != "" {
			prompt += fmt.Sprintf("Context: %s\n", snippet.Context)
		}
		prompt += "```\n" + snippet.Code + "\n```\n\n"
	}

	prompt += "For each snippet, provide:\n"
	prompt += "1. A summary of issues found (or 'No issues' if clean)\n"
	prompt += "2. Specific recommendations if needed\n\n"
	prompt += "Format your response clearly for each snippet."

	response, err := CallAI(ctx, AIRequest{
		Prompt:      prompt,
		Model:       opts.Model,
		MaxTokens:   opts.MaxTokens,
		Temperature: opts.Temperature,
	})

	if err != nil {
		return nil, err
	}

	// Parse response
	// This is a simplified implementation - a real parser would extract structured data
	assessments := make(map[string]*CodeAssessment)
	for _, snippet := range snippets {
		assessments[snippet.ID] = &CodeAssessment{
			Category:      category,
			Summary:       response.Text,
			TokensUsed:    response.TokensUsed / len(snippets), // Approximate
			EstimatedCost: response.EstimatedCost / float64(len(snippets)),
		}
	}

	return assessments, nil
}

// CodeSnippet represents a code snippet for batch assessment.
type CodeSnippet struct {
	ID      string // Unique identifier
	Code    string // Code to assess
	Context string // Optional context
}
