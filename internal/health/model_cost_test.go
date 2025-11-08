package health

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/steveyegge/vc/internal/ai"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TokenUsage tracks token counts and costs for a model run
type TokenUsage struct {
	InputTokens  int64
	OutputTokens int64
	TotalTokens  int64
	EstimatedCost float64
}

// Cost constants (as of late 2024/early 2025)
// See: https://www.anthropic.com/pricing
const (
	// Sonnet 4.5 pricing
	SonnetInputCostPerMToken  = 3.00  // $3 per million input tokens
	SonnetOutputCostPerMToken = 15.00 // $15 per million output tokens

	// Haiku pricing
	HaikuInputCostPerMToken  = 0.80  // $0.80 per million input tokens
	HaikuOutputCostPerMToken = 4.00  // $4 per million output tokens
)

// calculateCost calculates the cost in dollars for token usage
func calculateCost(inputTokens, outputTokens int64, inputCostPerMToken, outputCostPerMToken float64) float64 {
	inputCost := float64(inputTokens) * inputCostPerMToken / 1_000_000
	outputCost := float64(outputTokens) * outputCostPerMToken / 1_000_000
	return inputCost + outputCost
}

// TestModelCost_CruftDetector measures actual token usage and cost savings
// for cruft detection with Haiku vs Sonnet (vc-35 Phase 2)
func TestModelCost_CruftDetector(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping cost measurement test in short mode (requires AI API)")
	}

	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey == "" {
		t.Skip("ANTHROPIC_API_KEY not set")
	}

	// Create test directory with cruft files
	tmpDir := t.TempDir()

	cruftFiles := map[string]string{
		"backup.bak":    "old backup",
		"temp.tmp":      "temporary file",
		"notes.old":     "old notes",
		".DS_Store":     "macos metadata",
		"file~":         "editor backup",
		"legitimate.go": "package main",
		"important.txt": "important data",
	}

	for name, content := range cruftFiles {
		err := os.WriteFile(filepath.Join(tmpDir, name), []byte(content), 0644)
		require.NoError(t, err)
	}

	// Measure usage for both models
	results := make(map[string]*TokenUsage)

	for _, model := range []string{ai.ModelSonnet, ai.ModelHaiku} {
		t.Run(model, func(t *testing.T) {
			// Create cost-tracking supervisor
			client := anthropic.NewClient(option.WithAPIKey(apiKey))
			supervisor := &costTrackingSupervisor{
				client: &client,
				model:  model,
			}

			// Create detector
			detector, err := NewCruftDetector(tmpDir, supervisor)
			require.NoError(t, err)

			// Run check
			ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			defer cancel()

			result, err := detector.Check(ctx, CodebaseContext{})
			require.NoError(t, err)
			require.NotNil(t, result)

			// Get usage stats
			usage := supervisor.GetUsage()
			results[model] = usage

			t.Logf("%s usage: input=%d, output=%d, total=%d, cost=$%.4f",
				model, usage.InputTokens, usage.OutputTokens, usage.TotalTokens, usage.EstimatedCost)
		})
	}

	// Calculate and report savings
	sonnetUsage := results[ai.ModelSonnet]
	haikuUsage := results[ai.ModelHaiku]

	require.NotNil(t, sonnetUsage, "Sonnet usage should be recorded")
	require.NotNil(t, haikuUsage, "Haiku usage should be recorded")

	savingsDollars := sonnetUsage.EstimatedCost - haikuUsage.EstimatedCost
	savingsPercent := (savingsDollars / sonnetUsage.EstimatedCost) * 100

	t.Logf("\n=== Cost Comparison (Cruft Detector) ===")
	t.Logf("Sonnet: $%.4f", sonnetUsage.EstimatedCost)
	t.Logf("Haiku:  $%.4f", haikuUsage.EstimatedCost)
	t.Logf("Savings: $%.4f (%.1f%%)", savingsDollars, savingsPercent)

	// Haiku should be significantly cheaper (at least 50% savings expected)
	assert.Greater(t, savingsPercent, 50.0,
		"Haiku should save at least 50%% on cruft detection costs")
}

// TestModelCost_FileSizeMonitor measures cost savings for file size evaluation
func TestModelCost_FileSizeMonitor(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping cost measurement test in short mode (requires AI API)")
	}

	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey == "" {
		t.Skip("ANTHROPIC_API_KEY not set")
	}

	// Create test directory
	tmpDir := t.TempDir()

	// Create a few large files to trigger evaluation
	largeContent := make([]byte, 1024*1024) // 1MB
	for i := range largeContent {
		largeContent[i] = 'A'
	}
	err := os.WriteFile(filepath.Join(tmpDir, "large1.txt"), largeContent, 0644)
	require.NoError(t, err)

	// Measure usage for both models
	results := make(map[string]*TokenUsage)

	for _, model := range []string{ai.ModelSonnet, ai.ModelHaiku} {
		t.Run(model, func(t *testing.T) {
			client := anthropic.NewClient(option.WithAPIKey(apiKey))
			supervisor := &costTrackingSupervisor{
				client: &client,
				model:  model,
			}

			monitor, err := NewFileSizeMonitor(tmpDir, supervisor)
			require.NoError(t, err)

			ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			defer cancel()

			result, err := monitor.Check(ctx, CodebaseContext{})
			require.NoError(t, err)
			require.NotNil(t, result)

			usage := supervisor.GetUsage()
			results[model] = usage

			t.Logf("%s usage: input=%d, output=%d, total=%d, cost=$%.4f",
				model, usage.InputTokens, usage.OutputTokens, usage.TotalTokens, usage.EstimatedCost)
		})
	}

	// Calculate savings
	sonnetUsage := results[ai.ModelSonnet]
	haikuUsage := results[ai.ModelHaiku]

	require.NotNil(t, sonnetUsage)
	require.NotNil(t, haikuUsage)

	savingsDollars := sonnetUsage.EstimatedCost - haikuUsage.EstimatedCost
	savingsPercent := (savingsDollars / sonnetUsage.EstimatedCost) * 100

	t.Logf("\n=== Cost Comparison (File Size Monitor) ===")
	t.Logf("Sonnet: $%.4f", sonnetUsage.EstimatedCost)
	t.Logf("Haiku:  $%.4f", haikuUsage.EstimatedCost)
	t.Logf("Savings: $%.4f (%.1f%%)", savingsDollars, savingsPercent)

	assert.Greater(t, savingsPercent, 50.0,
		"Haiku should save at least 50%% on file size monitoring costs")
}

// TestModelCost_GitignoreDetector measures cost savings for gitignore recommendations
func TestModelCost_GitignoreDetector(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping cost measurement test in short mode (requires AI API)")
	}

	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey == "" {
		t.Skip("ANTHROPIC_API_KEY not set")
	}

	// Create test directory
	tmpDir := t.TempDir()

	gitignoreContent := `# Minimal gitignore
*.log
`
	err := os.WriteFile(filepath.Join(tmpDir, ".gitignore"), []byte(gitignoreContent), 0644)
	require.NoError(t, err)

	testFiles := map[string]string{
		"secrets.env":      "API_KEY=secret123",
		"config.local.yml": "database: localhost",
		"debug.tmp":        "debug output",
	}

	for path, content := range testFiles {
		err := os.WriteFile(filepath.Join(tmpDir, path), []byte(content), 0644)
		require.NoError(t, err)
	}

	// Measure usage for both models
	results := make(map[string]*TokenUsage)

	for _, model := range []string{ai.ModelSonnet, ai.ModelHaiku} {
		t.Run(model, func(t *testing.T) {
			client := anthropic.NewClient(option.WithAPIKey(apiKey))
			supervisor := &costTrackingSupervisor{
				client: &client,
				model:  model,
			}

			detector, err := NewGitignoreDetector(tmpDir, supervisor)
			require.NoError(t, err)

			ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			defer cancel()

			result, err := detector.Check(ctx, CodebaseContext{})
			require.NoError(t, err)
			require.NotNil(t, result)

			usage := supervisor.GetUsage()
			results[model] = usage

			t.Logf("%s usage: input=%d, output=%d, total=%d, cost=$%.4f",
				model, usage.InputTokens, usage.OutputTokens, usage.TotalTokens, usage.EstimatedCost)
		})
	}

	// Calculate savings
	sonnetUsage := results[ai.ModelSonnet]
	haikuUsage := results[ai.ModelHaiku]

	require.NotNil(t, sonnetUsage)
	require.NotNil(t, haikuUsage)

	savingsDollars := sonnetUsage.EstimatedCost - haikuUsage.EstimatedCost
	savingsPercent := (savingsDollars / sonnetUsage.EstimatedCost) * 100

	t.Logf("\n=== Cost Comparison (Gitignore Detector) ===")
	t.Logf("Sonnet: $%.4f", sonnetUsage.EstimatedCost)
	t.Logf("Haiku:  $%.4f", haikuUsage.EstimatedCost)
	t.Logf("Savings: $%.4f (%.1f%%)", savingsDollars, savingsPercent)

	assert.Greater(t, savingsPercent, 50.0,
		"Haiku should save at least 50%% on gitignore detection costs")
}

// TestModelCost_OverallSavings provides a summary of all cost savings
func TestModelCost_OverallSavings(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping cost summary test in short mode")
	}

	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey == "" {
		t.Skip("ANTHROPIC_API_KEY not set")
	}

	// This test documents the overall cost savings achieved by using Haiku
	// for simple operations (vc-35 Phase 2 acceptance criteria: 25%+ savings)

	t.Logf("\n=== VC Model Strategy Cost Analysis ===")
	t.Logf("\nOperations using Haiku (vc-35 Phase 1):")
	t.Logf("  1. Cruft detection")
	t.Logf("  2. File size monitoring")
	t.Logf("  3. Gitignore detection")
	t.Logf("  4. Commit message generation")
	t.Logf("\nOperations using Sonnet:")
	t.Logf("  1. Assessment (pre-execution)")
	t.Logf("  2. Analysis (post-execution)")
	t.Logf("  3. Code review")
	t.Logf("  4. Deduplication detection")
	t.Logf("  5. Discovered issue translation")
	t.Logf("  6. Complexity monitoring")
	t.Logf("\nExpected cost reduction:")
	t.Logf("  - Haiku is ~75%% cheaper than Sonnet")
	t.Logf("  - Simple operations represent ~30-40%% of total AI calls")
	t.Logf("  - Target: 25%%+ overall cost savings")
	t.Logf("\nPricing reference (late 2024/early 2025):")
	t.Logf("  Sonnet: $%.2f/$%.2f per MTok (input/output)", SonnetInputCostPerMToken, SonnetOutputCostPerMToken)
	t.Logf("  Haiku:  $%.2f/$%.2f per MTok (input/output)", HaikuInputCostPerMToken, HaikuOutputCostPerMToken)
	t.Logf("\nRun individual TestModelCost_* tests for detailed measurements.")
}

// Helper: costTrackingSupervisor implements AISupervisor and tracks token usage
type costTrackingSupervisor struct {
	client *anthropic.Client
	model  string

	totalInputTokens  int64
	totalOutputTokens int64
}

func (s *costTrackingSupervisor) CallAI(ctx context.Context, prompt string, operation string, model string, maxTokens int) (string, error) {
	// Use the supervisor's model (override passed-in model for testing)
	actualModel := s.model

	resp, err := s.client.Messages.New(ctx, anthropic.MessageNewParams{
		Model:     anthropic.Model(actualModel),
		MaxTokens: int64(maxTokens),
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock(prompt)),
		},
	})
	if err != nil {
		return "", fmt.Errorf("AI API call failed: %w", err)
	}

	// Track token usage
	s.totalInputTokens += resp.Usage.InputTokens
	s.totalOutputTokens += resp.Usage.OutputTokens

	var result string
	for _, block := range resp.Content {
		if block.Type == "text" {
			result += block.Text
		}
	}

	return result, nil
}

func (s *costTrackingSupervisor) GetUsage() *TokenUsage {
	var inputCost, outputCost float64

	if s.model == ai.ModelSonnet {
		inputCost = SonnetInputCostPerMToken
		outputCost = SonnetOutputCostPerMToken
	} else if s.model == ai.ModelHaiku {
		inputCost = HaikuInputCostPerMToken
		outputCost = HaikuOutputCostPerMToken
	}

	totalCost := calculateCost(s.totalInputTokens, s.totalOutputTokens, inputCost, outputCost)

	return &TokenUsage{
		InputTokens:   s.totalInputTokens,
		OutputTokens:  s.totalOutputTokens,
		TotalTokens:   s.totalInputTokens + s.totalOutputTokens,
		EstimatedCost: totalCost,
	}
}
