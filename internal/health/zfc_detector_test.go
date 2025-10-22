package health

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestZFCDetector_Interface(t *testing.T) {
	detector, err := NewZFCDetector("/tmp", nil)
	require.NoError(t, err)

	assert.Equal(t, "zfc_detector", detector.Name())
	assert.Contains(t, detector.Philosophy(), "Zero Framework Cognition")
	assert.Contains(t, detector.Philosophy(), "AI judgment")
	assert.Equal(t, ScheduleTimeBased, detector.Schedule().Type)
	assert.Equal(t, 7*24*time.Hour, detector.Schedule().Interval) // Weekly
	assert.Equal(t, CostModerate, detector.Cost().Category)
}

func TestNewZFCDetector_PathValidation(t *testing.T) {
	tests := []struct {
		name      string
		rootPath  string
		shouldErr bool
	}{
		{
			name:      "valid absolute path",
			rootPath:  "/tmp",
			shouldErr: false,
		},
		{
			name:      "valid relative path",
			rootPath:  ".",
			shouldErr: false,
		},
		{
			name:      "valid relative path with ..",
			rootPath:  "../",
			shouldErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			detector, err := NewZFCDetector(tt.rootPath, nil)
			if tt.shouldErr {
				assert.Error(t, err)
				assert.Nil(t, detector)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, detector)
				// Verify path is absolute
				assert.True(t, filepath.IsAbs(detector.RootPath))
			}
		})
	}
}

func TestZFCDetector_ScanFiles_MagicNumbers(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "zfc-test-*")
	require.NoError(t, err)
	defer func() { _ = os.RemoveAll(tmpDir) }()

	// Create test files with magic numbers
	testFiles := map[string]string{
		"magic.go": `package main

func processItems(count int) bool {
	// This should be flagged: magic number threshold
	if count > 50 {
		return true
	}

	// This should NOT be flagged: legitimate boundary check
	if count < 0 {
		return false
	}

	// This should NOT be flagged: common values
	if count == 0 || count == 1 {
		return false
	}

	return false
}
`,
		"legitimate.go": `package main

const MaxRetries = 3 // Named constant, not magic number

func retry(attempts int) bool {
	return attempts < MaxRetries
}
`,
	}

	for name, content := range testFiles {
		err := os.WriteFile(filepath.Join(tmpDir, name), []byte(content), 0644)
		require.NoError(t, err)
	}

	detector, err := NewZFCDetector(tmpDir, nil)
	require.NoError(t, err)

	ctx := context.Background()
	violations, err := detector.scanFiles(ctx)
	require.NoError(t, err)

	// Should find the magic number violation
	magicNumberViolations := filterByType(violations, "magic_number")
	assert.GreaterOrEqual(t, len(magicNumberViolations), 1, "Should detect magic number threshold")

	// Verify it found the right line
	found := false
	for _, v := range magicNumberViolations {
		if strings.Contains(v.LineContent, "count > 50") {
			found = true
			assert.Equal(t, "magic.go", v.FilePath)
		}
	}
	assert.True(t, found, "Should find 'count > 50' violation")
}

func TestZFCDetector_ScanFiles_RegexPatterns(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "zfc-test-*")
	require.NoError(t, err)
	defer func() { _ = os.RemoveAll(tmpDir) }()

	testFiles := map[string]string{
		"parser.go": `package main

import "regexp"

func parseIntent(text string) string {
	// This should be flagged: regex for semantic parsing
	pattern := regexp.MustCompile("(create|add|new)\\s+(\\w+)")
	matches := pattern.FindStringSubmatch(text)
	if len(matches) > 0 {
		return matches[1]
	}
	return ""
}

func validateEmail(email string) bool {
	// This might be flagged but is actually legitimate (format validation)
	emailPattern := regexp.Compile("^[a-z]+@[a-z]+\\.[a-z]+$")
	return emailPattern.MatchString(email)
}
`,
	}

	for name, content := range testFiles {
		err := os.WriteFile(filepath.Join(tmpDir, name), []byte(content), 0644)
		require.NoError(t, err)
	}

	detector, err := NewZFCDetector(tmpDir, nil)
	require.NoError(t, err)

	ctx := context.Background()
	violations, err := detector.scanFiles(ctx)
	require.NoError(t, err)

	// Should find regex violations
	regexViolations := filterByType(violations, "regex_parsing")
	assert.GreaterOrEqual(t, len(regexViolations), 1, "Should detect regex usage")
}

func TestZFCDetector_ScanFiles_StringMatching(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "zfc-test-*")
	require.NoError(t, err)
	defer func() { _ = os.RemoveAll(tmpDir) }()

	testFiles := map[string]string{
		"classifier.go": `package main

import "strings"

func classifyIssue(title string) string {
	// These should be flagged: classification by keyword matching
	if strings.Contains(title, "bug") || strings.Contains(title, "error") {
		return "bug"
	}
	if strings.HasPrefix(title, "feat") || strings.HasPrefix(title, "feature") {
		return "feature"
	}
	return "unknown"
}
`,
	}

	for name, content := range testFiles {
		err := os.WriteFile(filepath.Join(tmpDir, name), []byte(content), 0644)
		require.NoError(t, err)
	}

	detector, err := NewZFCDetector(tmpDir, nil)
	require.NoError(t, err)

	ctx := context.Background()
	violations, err := detector.scanFiles(ctx)
	require.NoError(t, err)

	// Should find string matching violations
	stringViolations := filterByType(violations, "string_matching")
	assert.GreaterOrEqual(t, len(stringViolations), 1, "Should detect string matching")
}

func TestZFCDetector_ScanFiles_ComplexConditionals(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "zfc-test-*")
	require.NoError(t, err)
	defer func() { _ = os.RemoveAll(tmpDir) }()

	testFiles := map[string]string{
		"business_logic.go": `package main

func shouldApprove(amount int, user string, hasAuth bool, isWeekend bool) bool {
	// This should be flagged: complex business rule
	if amount < 1000 && user != "admin" && hasAuth && !isWeekend {
		return true
	}

	// Simple conditional - should NOT be flagged
	if amount < 0 {
		return false
	}

	return false
}
`,
	}

	for name, content := range testFiles {
		err := os.WriteFile(filepath.Join(tmpDir, name), []byte(content), 0644)
		require.NoError(t, err)
	}

	detector, err := NewZFCDetector(tmpDir, nil)
	require.NoError(t, err)

	ctx := context.Background()
	violations, err := detector.scanFiles(ctx)
	require.NoError(t, err)

	// Should find complex conditional
	complexViolations := filterByType(violations, "complex_conditional")
	assert.GreaterOrEqual(t, len(complexViolations), 1, "Should detect complex conditional")
}

func TestZFCDetector_ScanFiles_ExcludePatterns(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "zfc-test-*")
	require.NoError(t, err)
	defer func() { _ = os.RemoveAll(tmpDir) }()

	// Create files that should be excluded
	testFiles := map[string]string{
		"main.go": `package main
func main() {
	if count > 50 { } // Should be found
}`,
		"main_test.go": `package main
func TestMain(t *testing.T) {
	if count > 50 { } // Should be EXCLUDED (_test.go)
}`,
	}

	// Create vendor directory (should be excluded)
	vendorDir := filepath.Join(tmpDir, "vendor", "foo")
	err = os.MkdirAll(vendorDir, 0755)
	require.NoError(t, err)
	err = os.WriteFile(filepath.Join(vendorDir, "vendor.go"), []byte("package foo\nif count > 50 {}"), 0644)
	require.NoError(t, err)

	for name, content := range testFiles {
		err := os.WriteFile(filepath.Join(tmpDir, name), []byte(content), 0644)
		require.NoError(t, err)
	}

	detector, err := NewZFCDetector(tmpDir, nil)
	require.NoError(t, err)

	ctx := context.Background()
	violations, err := detector.scanFiles(ctx)
	require.NoError(t, err)

	// Should only find violations in main.go, not in _test.go or vendor/
	for _, v := range violations {
		assert.NotContains(t, v.FilePath, "_test.go", "Should exclude test files")
		assert.NotContains(t, v.FilePath, "vendor/", "Should exclude vendor directory")
	}

	// Should find at least one violation in main.go
	foundInMain := false
	for _, v := range violations {
		if strings.Contains(v.FilePath, "main.go") && !strings.Contains(v.FilePath, "_test") {
			foundInMain = true
			break
		}
	}
	assert.True(t, foundInMain, "Should find violation in main.go")
}

func TestZFCDetector_Check_BelowThreshold(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "zfc-test-*")
	require.NoError(t, err)
	defer func() { _ = os.RemoveAll(tmpDir) }()

	// Create file with only 1 violation (below threshold of 3)
	testFile := `package main
func main() {
	if count > 50 {
		println("test")
	}
}
`
	err = os.WriteFile(filepath.Join(tmpDir, "main.go"), []byte(testFile), 0644)
	require.NoError(t, err)

	detector, err := NewZFCDetector(tmpDir, nil)
	require.NoError(t, err)

	ctx := context.Background()
	result, err := detector.Check(ctx, CodebaseContext{})
	require.NoError(t, err)

	// Should return no issues because below threshold
	assert.Empty(t, result.IssuesFound)
	assert.Contains(t, result.Context, "below threshold")
}

func TestZFCDetector_Check_RequiresSupervisor(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "zfc-test-*")
	require.NoError(t, err)
	defer func() { _ = os.RemoveAll(tmpDir) }()

	detector, err := NewZFCDetector(tmpDir, nil) // No supervisor
	require.NoError(t, err)

	ctx := context.Background()
	_, err = detector.Check(ctx, CodebaseContext{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "AI supervisor is required")
}

func TestZFCDetector_Check_WithMockSupervisor(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "zfc-test-*")
	require.NoError(t, err)
	defer func() { _ = os.RemoveAll(tmpDir) }()

	// Create multiple files with violations (above threshold)
	testFiles := map[string]string{
		"file1.go": `package main
func foo() {
	if count > 50 { }
	if size > 100 { }
}`,
		"file2.go": `package main
import "regexp"
func bar() {
	pattern := regexp.MustCompile("test")
	if count > 75 { }
}`,
		"file3.go": `package main
import "strings"
func baz() {
	if strings.Contains(text, "keyword") { }
}`,
	}

	for name, content := range testFiles {
		err := os.WriteFile(filepath.Join(tmpDir, name), []byte(content), 0644)
		require.NoError(t, err)
	}

	// Mock supervisor that returns a valid evaluation
	mockSupervisor := &mockAISupervisor{
		response: `{
			"true_violations": [
				{
					"file_path": "file1.go",
					"line_number": 3,
					"violation_type": "magic_number",
					"impact": "high",
					"why_violation": "Hardcoded threshold will become stale",
					"refactoring_suggestion": "Ask AI to evaluate if count is concerning"
				},
				{
					"file_path": "file2.go",
					"line_number": 4,
					"violation_type": "regex_parsing",
					"impact": "medium",
					"why_violation": "Regex encodes semantic meaning",
					"refactoring_suggestion": "Use AI for text classification"
				}
			],
			"legitimate_code": [
				{
					"file_path": "file3.go",
					"line_number": 3,
					"justification": "Simple string operation, not classification"
				}
			],
			"refactoring_guidance": "Replace hardcoded thresholds with AI judgment calls",
			"reasoning": "Found 2 true violations and 1 legitimate pattern"
		}`,
	}

	detector, err := NewZFCDetector(tmpDir, mockSupervisor)
	require.NoError(t, err)

	ctx := context.Background()
	result, err := detector.Check(ctx, CodebaseContext{})
	require.NoError(t, err)

	// Should find issues based on AI evaluation
	assert.NotEmpty(t, result.IssuesFound)
	assert.Contains(t, result.Context, "2 as true violations")
	assert.Contains(t, result.Context, "1 as legitimate")
	assert.Equal(t, 1, result.Stats.AICallsMade)
}

func TestZFCDetector_BuildPrompt(t *testing.T) {
	detector, err := NewZFCDetector("/tmp", nil)
	require.NoError(t, err)

	violations := []zfcViolation{
		{
			FilePath:      "test.go",
			LineNumber:    10,
			LineContent:   "if count > 50 {",
			ViolationType: "magic_number",
			Context:       "Hardcoded threshold",
		},
		{
			FilePath:      "parser.go",
			LineNumber:    20,
			LineContent:   `pattern := regexp.MustCompile("test")`,
			ViolationType: "regex_parsing",
			Context:       "Regex pattern",
		},
	}

	prompt := detector.buildPrompt(violations)

	// Verify prompt contains key elements
	assert.Contains(t, prompt, "Zero Framework Cognition")
	assert.Contains(t, prompt, detector.Philosophy())
	assert.Contains(t, prompt, "2 potential ZFC violations")
	assert.Contains(t, prompt, "test.go:10")
	assert.Contains(t, prompt, "parser.go:20")
	assert.Contains(t, prompt, "magic_number")
	assert.Contains(t, prompt, "regex_parsing")
	assert.Contains(t, prompt, "true_violations")
	assert.Contains(t, prompt, "legitimate_code")
	assert.Contains(t, prompt, fmt.Sprintf("%d", time.Now().Year()))
}

func TestZFCDetector_CountUniqueFiles(t *testing.T) {
	detector, err := NewZFCDetector("/tmp", nil)
	require.NoError(t, err)

	violations := []zfcViolation{
		{FilePath: "file1.go", LineNumber: 1},
		{FilePath: "file1.go", LineNumber: 2},
		{FilePath: "file2.go", LineNumber: 1},
		{FilePath: "file1.go", LineNumber: 3},
	}

	count := detector.countUniqueFiles(violations)
	assert.Equal(t, 2, count, "Should count 2 unique files")
}

// Helper functions

func filterByType(violations []zfcViolation, violationType string) []zfcViolation {
	var filtered []zfcViolation
	for _, v := range violations {
		if v.ViolationType == violationType {
			filtered = append(filtered, v)
		}
	}
	return filtered
}

// mockAISupervisor implements AISupervisor for testing
type mockAISupervisor struct {
	response string
	err      error
}

func (m *mockAISupervisor) CallAI(ctx context.Context, prompt string, operation string, model string, maxTokens int) (string, error) {
	if m.err != nil {
		return "", m.err
	}
	return m.response, nil
}
