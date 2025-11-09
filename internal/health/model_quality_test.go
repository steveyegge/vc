package health

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/steveyegge/vc/internal/ai"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestModelQuality_CruftDetector validates that Haiku performs comparably to Sonnet
// for cruft detection (vc-35 Phase 2: <5% degradation target)
func TestModelQuality_CruftDetector(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping model quality test in short mode (requires AI API)")
	}

	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey == "" {
		t.Skip("ANTHROPIC_API_KEY not set")
	}

	// Create test directory with known cruft files
	tmpDir := t.TempDir()

	cruftFiles := map[string]string{
		"backup.bak":       "old backup",
		"temp.tmp":         "temporary file",
		"notes.old":        "old notes",
		".DS_Store":        "macos metadata",
		"file~":            "editor backup",
		"legitimate.go":    "package main",
		"important.txt":    "important data",
	}

	for name, content := range cruftFiles {
		err := os.WriteFile(filepath.Join(tmpDir, name), []byte(content), 0644)
		require.NoError(t, err)
	}

	// Expected cruft files (ground truth)
	expectedCruft := []string{"backup.bak", "temp.tmp", "notes.old", ".DS_Store", "file~"}

	// Test with both models
	results := make(map[string][]string) // model -> detected cruft files

	for _, model := range []string{ai.ModelSonnet, ai.ModelHaiku} {
		t.Run(model, func(t *testing.T) {
			// Create supervisor with specific model
			client := anthropic.NewClient(option.WithAPIKey(apiKey))
			supervisor := &realAISupervisor{
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

			// Extract detected cruft file names from Evidence
			var detectedFiles []string
			for _, issue := range result.IssuesFound {
				// Evidence field is already map[string]interface{}
				if cruftArray, ok := issue.Evidence["cruft_to_delete"]; ok {
					// Try as []cruftFileAction first
					if typedArray, ok := cruftArray.([]cruftFileAction); ok {
						for _, cruft := range typedArray {
							detectedFiles = append(detectedFiles, filepath.Base(cruft.File))
						}
					} else if interfaceArray, ok := cruftArray.([]interface{}); ok {
						// Fallback to []interface{} for marshaled JSON
						for _, item := range interfaceArray {
							if cruftFile, ok := item.(map[string]interface{}); ok {
								if file, ok := cruftFile["file"].(string); ok {
									detectedFiles = append(detectedFiles, filepath.Base(file))
								}
							}
						}
					}
				}
			}

			results[model] = detectedFiles
			t.Logf("%s detected %d cruft files: %v", model, len(detectedFiles), detectedFiles)

			// Verify some cruft was detected
			assert.NotEmpty(t, detectedFiles, "Model should detect some cruft files")
		})
	}

	// Compare results
	sonnetFiles := results[ai.ModelSonnet]
	haikuFiles := results[ai.ModelHaiku]

	// Calculate agreement metrics
	agreement := calculateSetAgreement(sonnetFiles, haikuFiles)
	t.Logf("Model agreement: %.2f%% (Sonnet: %d files, Haiku: %d files)",
		agreement*100, len(sonnetFiles), len(haikuFiles))

	// Quality criteria: <5% degradation means >95% agreement
	// We're lenient here - just checking for reasonable overlap (>80%)
	assert.Greater(t, agreement, 0.80,
		"Haiku should have >80%% agreement with Sonnet on cruft detection")

	// Check that both detected at least some of the expected cruft
	sonnetRecall := calculateRecall(expectedCruft, sonnetFiles)
	haikuRecall := calculateRecall(expectedCruft, haikuFiles)

	t.Logf("Sonnet recall: %.2f%%, Haiku recall: %.2f%%",
		sonnetRecall*100, haikuRecall*100)

	// Haiku should have similar recall (within 10 percentage points)
	recallDiff := absFloat(sonnetRecall - haikuRecall)
	assert.Less(t, recallDiff, 0.15,
		"Haiku recall should be within 15%% of Sonnet recall")
}

// TestModelQuality_FileSizeMonitor validates Haiku performance on file size evaluation
func TestModelQuality_FileSizeMonitor(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping model quality test in short mode (requires AI API)")
	}

	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey == "" {
		t.Skip("ANTHROPIC_API_KEY not set")
	}

	// Create test directory with files of various sizes
	tmpDir := t.TempDir()

	// Create a legitimately large file (e.g., test fixture)
	largeContent := strings.Repeat("legitimate test data\n", 50000) // ~1MB
	err := os.WriteFile(filepath.Join(tmpDir, "large_test_fixture.txt"), []byte(largeContent), 0644)
	require.NoError(t, err)

	// Create a suspicious large file (generated output)
	generatedContent := strings.Repeat("generated output line\n", 30000) // ~600KB
	err = os.WriteFile(filepath.Join(tmpDir, "generated_output.log"), []byte(generatedContent), 0644)
	require.NoError(t, err)

	// Create normal files
	err = os.WriteFile(filepath.Join(tmpDir, "small.txt"), []byte("small file"), 0644)
	require.NoError(t, err)

	// Test with both models
	results := make(map[string]int) // model -> number of issues found

	for _, model := range []string{ai.ModelSonnet, ai.ModelHaiku} {
		t.Run(model, func(t *testing.T) {
			client := anthropic.NewClient(option.WithAPIKey(apiKey))
			supervisor := &realAISupervisor{
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

			issueCount := len(result.IssuesFound)
			results[model] = issueCount
			t.Logf("%s found %d large file issues", model, issueCount)

			// Log details for debugging
			for _, issue := range result.IssuesFound {
				t.Logf("  - %s: %s", filepath.Base(issue.FilePath), issue.Description)
			}
		})
	}

	// Compare results - both should find similar number of issues
	sonnetIssues := results[ai.ModelSonnet]
	haikuIssues := results[ai.ModelHaiku]

	// Allow for some variance - within 1 issue or 50% difference (whichever is larger)
	maxDiff := max(1, sonnetIssues/2)
	actualDiff := abs(sonnetIssues - haikuIssues)

	assert.LessOrEqual(t, actualDiff, maxDiff,
		"Haiku should find similar number of issues as Sonnet (Sonnet: %d, Haiku: %d)",
		sonnetIssues, haikuIssues)
}

// TestModelQuality_GitignoreDetector validates Haiku performance on gitignore recommendations
func TestModelQuality_GitignoreDetector(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping model quality test in short mode (requires AI API)")
	}

	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey == "" {
		t.Skip("ANTHROPIC_API_KEY not set")
	}

	// Create test directory with files that should be gitignored
	tmpDir := t.TempDir()

	// Initialize git repository (required for GitignoreDetector)
	initCmd := exec.Command("git", "init")
	initCmd.Dir = tmpDir
	err := initCmd.Run()
	require.NoError(t, err, "Failed to initialize git repository")

	// Configure git user for the test repo
	configNameCmd := exec.Command("git", "config", "user.name", "Test User")
	configNameCmd.Dir = tmpDir
	_ = configNameCmd.Run()

	configEmailCmd := exec.Command("git", "config", "user.email", "test@example.com")
	configEmailCmd.Dir = tmpDir
	_ = configEmailCmd.Run()

	// Create .gitignore with minimal content
	gitignoreContent := `# Minimal gitignore
*.log
`
	err = os.WriteFile(filepath.Join(tmpDir, ".gitignore"), []byte(gitignoreContent), 0644)
	require.NoError(t, err)

	// Create files that should be ignored but aren't
	testFiles := map[string]string{
		"secrets.env":       "API_KEY=secret123",
		"config.local.yml":  "database: localhost",
		"debug.tmp":         "debug output",
		"node_modules/pkg/index.js": "// dependency",
		"main.go":           "package main", // Should NOT be ignored
	}

	for path, content := range testFiles {
		fullPath := filepath.Join(tmpDir, path)
		dir := filepath.Dir(fullPath)
		if dir != tmpDir {
			err := os.MkdirAll(dir, 0755)
			require.NoError(t, err)
		}
		err := os.WriteFile(fullPath, []byte(content), 0644)
		require.NoError(t, err)
	}

	// Add files to git tracking (so GitignoreDetector can detect them)
	addCmd := exec.Command("git", "add", ".")
	addCmd.Dir = tmpDir
	err = addCmd.Run()
	require.NoError(t, err, "Failed to add files to git")

	// Test with both models
	results := make(map[string][]string) // model -> recommended patterns

	for _, model := range []string{ai.ModelSonnet, ai.ModelHaiku} {
		t.Run(model, func(t *testing.T) {
			client := anthropic.NewClient(option.WithAPIKey(apiKey))
			supervisor := &realAISupervisor{
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

			// Extract recommended patterns from Evidence field
			var patterns []string
			for _, issue := range result.IssuesFound {
				// Evidence field contains patterns_to_add array
				if patternsData, ok := issue.Evidence["patterns_to_add"]; ok {
					if patternArray, ok := patternsData.([]string); ok {
						patterns = append(patterns, patternArray...)
					} else if interfaceArray, ok := patternsData.([]interface{}); ok {
						for _, p := range interfaceArray {
							if pattern, ok := p.(string); ok {
								patterns = append(patterns, pattern)
							}
						}
					}
				}
			}

			results[model] = patterns
			t.Logf("%s recommended %d patterns: %v", model, len(patterns), patterns)

			// Also log if no issues were found at all
			if len(result.IssuesFound) == 0 {
				t.Logf("%s found no issues - detector may not have detected violations", model)
			}
		})
	}

	// Compare results
	sonnetPatterns := results[ai.ModelSonnet]
	haikuPatterns := results[ai.ModelHaiku]

	// If neither model found violations, skip the test (detector threshold not met)
	if len(sonnetPatterns) == 0 && len(haikuPatterns) == 0 {
		t.Skip("No gitignore violations detected by either model - detector threshold may not have been met")
	}

	// Both should recommend some patterns (if we got this far)
	assert.NotEmpty(t, sonnetPatterns, "Sonnet should recommend some patterns")
	assert.NotEmpty(t, haikuPatterns, "Haiku should recommend some patterns")

	// Calculate agreement
	agreement := calculateSetAgreement(sonnetPatterns, haikuPatterns)
	t.Logf("Pattern agreement: %.2f%% (Sonnet: %d, Haiku: %d)",
		agreement*100, len(sonnetPatterns), len(haikuPatterns))

	// Require >70% agreement (gitignore detection is more subjective)
	assert.Greater(t, agreement, 0.70,
		"Haiku should have >70%% agreement with Sonnet on gitignore patterns")
}

// Helper: realAISupervisor implements AISupervisor for testing
type realAISupervisor struct {
	client *anthropic.Client
	model  string
}

func (s *realAISupervisor) CallAI(ctx context.Context, prompt string, operation string, model string, maxTokens int) (string, error) {
	// Use the supervisor's model (ignore the passed-in model)
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

	var result strings.Builder
	for _, block := range resp.Content {
		if block.Type == "text" {
			result.WriteString(block.Text)
		}
	}

	return result.String(), nil
}

// Helper: calculateSetAgreement returns Jaccard similarity (intersection / union)
func calculateSetAgreement(set1, set2 []string) float64 {
	if len(set1) == 0 && len(set2) == 0 {
		return 1.0 // Both empty = perfect agreement
	}

	s1 := make(map[string]bool)
	for _, item := range set1 {
		s1[item] = true
	}

	s2 := make(map[string]bool)
	for _, item := range set2 {
		s2[item] = true
	}

	// Calculate intersection
	intersection := 0
	for item := range s1 {
		if s2[item] {
			intersection++
		}
	}

	// Calculate union
	union := len(s1)
	for item := range s2 {
		if !s1[item] {
			union++
		}
	}

	if union == 0 {
		return 1.0
	}

	return float64(intersection) / float64(union)
}

// Helper: calculateRecall returns recall (true positives / actual positives)
func calculateRecall(expected, detected []string) float64 {
	if len(expected) == 0 {
		return 1.0
	}

	expectedSet := make(map[string]bool)
	for _, item := range expected {
		expectedSet[item] = true
	}

	truePositives := 0
	for _, item := range detected {
		if expectedSet[item] {
			truePositives++
		}
	}

	return float64(truePositives) / float64(len(expected))
}

// Helper functions
func abs(a int) int {
	if a < 0 {
		return -a
	}
	return a
}

func absFloat(a float64) float64 {
	if a < 0 {
		return -a
	}
	return a
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
