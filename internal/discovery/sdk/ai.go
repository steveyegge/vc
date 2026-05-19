package sdk

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	vcai "github.com/steveyegge/vc/internal/ai"
)

var callAI = CallAI

const assessmentSystemPrompt = "You are an expert code reviewer. Analyze the provided code and identify specific, actionable issues. Focus on real problems, not nitpicks."

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

	response, err := callAI(ctx, AIRequest{
		Prompt:       prompt,
		Model:        opts.Model,
		MaxTokens:    opts.MaxTokens,
		Temperature:  opts.Temperature,
		SystemPrompt: assessmentSystemPrompt,
	})

	if err != nil {
		return nil, err
	}

	parsed, err := parseAssessmentResponse(response.Text)
	if err != nil {
		return nil, err
	}

	return &CodeAssessment{
		Category:        category,
		Summary:         parsed.Summary,
		Issues:          normalizeStrings(parsed.Issues),
		Recommendations: normalizeStrings(parsed.Recommendations),
		Confidence:      parsed.Confidence,
		TokensUsed:      response.TokensUsed,
		EstimatedCost:   response.EstimatedCost,
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

type codeAssessmentPayload struct {
	Summary         string   `json:"summary"`
	Issues          []string `json:"issues"`
	Recommendations []string `json:"recommendations"`
	Confidence      float64  `json:"confidence"`
}

type batchAssessmentPayload struct {
	Assessments []identifiedAssessmentPayload `json:"assessments"`
}

type identifiedAssessmentPayload struct {
	ID string `json:"id"`
	codeAssessmentPayload
}

// buildAssessmentPrompt constructs a prompt for code assessment.
func buildAssessmentPrompt(code string, category string, opts AssessmentOptions) string {
	var prompt strings.Builder
	prompt.WriteString(fmt.Sprintf("Analyze the following code for %s issues.\n\n", category))

	if opts.Focus != "" {
		prompt.WriteString(fmt.Sprintf("Focus: %s\n\n", opts.Focus))
	}

	if opts.Context != "" {
		prompt.WriteString(fmt.Sprintf("Context: %s\n\n", opts.Context))
	}

	prompt.WriteString("Respond with ONLY raw JSON in this shape:\n")
	prompt.WriteString("{\n")
	prompt.WriteString(`  "summary": "short overall assessment",` + "\n")
	prompt.WriteString(`  "issues": ["specific issue 1"],` + "\n")
	prompt.WriteString(`  "recommendations": ["specific recommendation 1"],` + "\n")
	prompt.WriteString(`  "confidence": 0.85` + "\n")
	prompt.WriteString("}\n\n")
	prompt.WriteString("Rules:\n")
	prompt.WriteString("- Use [] for `issues` or `recommendations` when there are none.\n")
	prompt.WriteString("- Keep `confidence` between 0.0 and 1.0.\n")
	prompt.WriteString("- Do not include markdown fences or extra prose.\n\n")
	prompt.WriteString("Code:\n```\n")
	prompt.WriteString(code)
	prompt.WriteString("\n```\n")

	return prompt.String()
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
	if len(snippets) == 0 {
		return map[string]*CodeAssessment{}, nil
	}

	prompt := buildBatchAssessmentPrompt(snippets, category, opts)

	response, err := callAI(ctx, AIRequest{
		Prompt:       prompt,
		Model:        opts.Model,
		MaxTokens:    opts.MaxTokens,
		Temperature:  opts.Temperature,
		SystemPrompt: assessmentSystemPrompt,
	})

	if err != nil {
		return nil, err
	}

	parsedAssessments, err := parseBatchAssessmentResponse(response.Text, snippets)
	if err != nil {
		return nil, err
	}

	perSnippetTokens := response.TokensUsed / len(snippets)
	perSnippetCost := response.EstimatedCost / float64(len(snippets))
	assessments := make(map[string]*CodeAssessment, len(snippets))
	for _, parsed := range parsedAssessments {
		assessments[parsed.ID] = &CodeAssessment{
			Category:        category,
			Summary:         parsed.Summary,
			Issues:          normalizeStrings(parsed.Issues),
			Recommendations: normalizeStrings(parsed.Recommendations),
			Confidence:      parsed.Confidence,
			TokensUsed:      perSnippetTokens,
			EstimatedCost:   perSnippetCost,
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

func buildBatchAssessmentPrompt(snippets []CodeSnippet, category string, opts AssessmentOptions) string {
	var prompt strings.Builder
	prompt.WriteString(fmt.Sprintf("Analyze the following code snippets for %s issues.\n\n", category))

	if opts.Focus != "" {
		prompt.WriteString(fmt.Sprintf("Focus: %s\n\n", opts.Focus))
	}

	if opts.Context != "" {
		prompt.WriteString(fmt.Sprintf("Context: %s\n\n", opts.Context))
	}

	prompt.WriteString("Respond with ONLY raw JSON in this shape:\n")
	prompt.WriteString("{\n")
	prompt.WriteString(`  "assessments": [` + "\n")
	prompt.WriteString("    {\n")
	prompt.WriteString(`      "id": "snippet-id",` + "\n")
	prompt.WriteString(`      "summary": "short overall assessment",` + "\n")
	prompt.WriteString(`      "issues": ["specific issue 1"],` + "\n")
	prompt.WriteString(`      "recommendations": ["specific recommendation 1"],` + "\n")
	prompt.WriteString(`      "confidence": 0.85` + "\n")
	prompt.WriteString("    }\n")
	prompt.WriteString("  ]\n")
	prompt.WriteString("}\n\n")
	prompt.WriteString("Rules:\n")
	prompt.WriteString("- Include exactly one assessment per snippet.\n")
	prompt.WriteString("- Copy each snippet `id` exactly.\n")
	prompt.WriteString("- Use [] for `issues` or `recommendations` when there are none.\n")
	prompt.WriteString("- Keep `confidence` between 0.0 and 1.0.\n")
	prompt.WriteString("- Do not include markdown fences or extra prose.\n\n")

	for i, snippet := range snippets {
		prompt.WriteString(fmt.Sprintf("## Snippet %d\n", i+1))
		prompt.WriteString(fmt.Sprintf("id: %s\n", snippet.ID))
		if snippet.Context != "" {
			prompt.WriteString(fmt.Sprintf("context: %s\n", snippet.Context))
		}
		prompt.WriteString("code:\n```\n")
		prompt.WriteString(snippet.Code)
		prompt.WriteString("\n```\n\n")
	}

	return prompt.String()
}

func parseAssessmentResponse(text string) (*codeAssessmentPayload, error) {
	parseResult := vcai.Parse[codeAssessmentPayload](text, vcai.ParseOptions{
		Context:   "sdk code assessment response",
		LogErrors: vcai.BoolPtr(false),
	})
	if !parseResult.Success {
		return nil, fmt.Errorf("failed to parse structured code assessment: %s", parseResult.Error)
	}

	parsed := parseResult.Data
	parsed.Issues = normalizeStrings(parsed.Issues)
	parsed.Recommendations = normalizeStrings(parsed.Recommendations)
	return &parsed, nil
}

func parseBatchAssessmentResponse(text string, snippets []CodeSnippet) ([]identifiedAssessmentPayload, error) {
	parseResult := vcai.Parse[batchAssessmentPayload](text, vcai.ParseOptions{
		Context:   "sdk batch code assessment response",
		LogErrors: vcai.BoolPtr(false),
	})
	if !parseResult.Success {
		return nil, fmt.Errorf("failed to parse structured batch code assessment: %s", parseResult.Error)
	}

	expected := make(map[string]struct{}, len(snippets))
	for _, snippet := range snippets {
		expected[snippet.ID] = struct{}{}
	}

	seen := make(map[string]struct{}, len(snippets))
	for i := range parseResult.Data.Assessments {
		assessment := &parseResult.Data.Assessments[i]
		if _, ok := expected[assessment.ID]; !ok {
			return nil, fmt.Errorf("AI returned assessment for unknown snippet %q", assessment.ID)
		}
		if _, duplicate := seen[assessment.ID]; duplicate {
			return nil, fmt.Errorf("AI returned duplicate assessment for snippet %q", assessment.ID)
		}

		assessment.Issues = normalizeStrings(assessment.Issues)
		assessment.Recommendations = normalizeStrings(assessment.Recommendations)
		seen[assessment.ID] = struct{}{}
	}

	for _, snippet := range snippets {
		if _, ok := seen[snippet.ID]; !ok {
			return nil, fmt.Errorf("AI response missing assessment for snippet %q", snippet.ID)
		}
	}

	return parseResult.Data.Assessments, nil
}

func normalizeStrings(values []string) []string {
	if values == nil {
		return []string{}
	}
	return values
}
