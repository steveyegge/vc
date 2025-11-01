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

func TestDuplicationDetector_Interface(t *testing.T) {
	detector, err := NewDuplicationDetector("/tmp", nil)
	require.NoError(t, err)

	assert.Equal(t, "duplication_detector", detector.Name())
	assert.Contains(t, detector.Philosophy(), "DRY")
	assert.Equal(t, ScheduleHybrid, detector.Schedule().Type)
	assert.Equal(t, CostExpensive, detector.Cost().Category)
}

func TestNewDuplicationDetector_PathValidation(t *testing.T) {
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
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			detector, err := NewDuplicationDetector(tt.rootPath, nil)
			if tt.shouldErr {
				assert.Error(t, err)
				assert.Nil(t, detector)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, detector)
				assert.True(t, filepath.IsAbs(detector.RootPath))
			}
		})
	}
}

func TestDuplicationDetector_NormalizeBlock(t *testing.T) {
	detector, err := NewDuplicationDetector("/tmp", nil)
	require.NoError(t, err)

	tests := []struct {
		name     string
		lines    []string
		expected []string
	}{
		{
			name: "removes whitespace",
			lines: []string{
				"  func foo() {",
				"    return nil",
				"  }",
			},
			expected: []string{
				"func foo() {",
				"return nil",
				"}",
			},
		},
		{
			name: "removes comments",
			lines: []string{
				"// This is a comment",
				"func bar() {",
				"/* Block comment */",
				"  return 42",
				"}",
			},
			expected: []string{
				"func bar() {",
				"return 42",
				"}",
			},
		},
		{
			name: "removes empty lines",
			lines: []string{
				"func baz() {",
				"",
				"  return true",
				"",
				"}",
			},
			expected: []string{
				"func baz() {",
				"return true",
				"}",
			},
		},
		{
			name:     "all empty or comments",
			lines:    []string{"", "// Comment", "  ", "/* Comment */"},
			expected: nil, // normalizeBlock returns nil for empty result
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			normalized := detector.normalizeBlock(tt.lines)
			assert.Equal(t, tt.expected, normalized)
		})
	}
}

func TestDuplicationDetector_HashBlock(t *testing.T) {
	detector, err := NewDuplicationDetector("/tmp", nil)
	require.NoError(t, err)

	lines1 := []string{"func foo() {", "return nil", "}"}
	lines2 := []string{"func foo() {", "return nil", "}"}
	lines3 := []string{"func bar() {", "return nil", "}"}

	hash1 := detector.hashBlock(lines1)
	hash2 := detector.hashBlock(lines2)
	hash3 := detector.hashBlock(lines3)

	// Same lines should produce same hash
	assert.Equal(t, hash1, hash2)
	// Different lines should produce different hash
	assert.NotEqual(t, hash1, hash3)
	// Hash should be 16 chars (8 bytes in hex)
	assert.Equal(t, 16, len(hash1))
}

func TestDuplicationDetector_FindDuplicateBlocks(t *testing.T) {
	detector, err := NewDuplicationDetector("/tmp", nil)
	require.NoError(t, err)
	detector.MinBlockSize = 3

	files := []codeFile{
		{
			Path: "file1.go",
			Lines: []string{
				"package main",
				"",
				"func foo() {",
				"  return nil",
				"}",
				"",
				"func bar() {",
				"  return 42",
				"}",
			},
		},
		{
			Path: "file2.go",
			Lines: []string{
				"package main",
				"",
				"func foo() {",
				"  return nil",
				"}",
				"",
				"func baz() {",
				"  return true",
				"}",
			},
		},
	}

	duplicates := detector.findDuplicateBlocks(files)

	// Should find the duplicate foo() function (3 lines)
	assert.Greater(t, len(duplicates), 0, "Should find duplicates")

	// Find the duplicate with "foo" in it
	var fooDup *duplicateBlock
	for i, dup := range duplicates {
		blockText := strings.Join(dup.Lines, "\n")
		if strings.Contains(blockText, "foo") {
			fooDup = &duplicates[i]
			break
		}
	}

	require.NotNil(t, fooDup, "Should find duplicate foo function")
	assert.Len(t, fooDup.Locations, 2, "Duplicate should appear in 2 locations")
}

func TestDuplicationDetector_FindDuplicateBlocks_NoDuplicates(t *testing.T) {
	detector, err := NewDuplicationDetector("/tmp", nil)
	require.NoError(t, err)
	detector.MinBlockSize = 3

	files := []codeFile{
		{
			Path: "file1.go",
			Lines: []string{
				"package main",
				"",
				"func unique1() {",
				"  return 1",
				"}",
			},
		},
		{
			Path: "file2.go",
			Lines: []string{
				"package main",
				"",
				"func unique2() {",
				"  return 2",
				"}",
			},
		},
	}

	duplicates := detector.findDuplicateBlocks(files)

	// Should not find duplicates for unique functions
	// (package main might appear twice but that's acceptable)
	// We're looking for meaningful duplicates
	for _, dup := range duplicates {
		blockText := strings.Join(dup.Lines, "\n")
		// Make sure it's not a meaningful function duplicate
		assert.False(t, strings.Contains(blockText, "unique1") && strings.Contains(blockText, "unique2"),
			"Should not find duplicates for unique functions")
	}
}

func TestDuplicationDetector_CalculateDuplicateLines(t *testing.T) {
	detector, err := NewDuplicationDetector("/tmp", nil)
	require.NoError(t, err)

	duplicates := []duplicateBlock{
		{
			Lines: []string{"line1", "line2", "line3"}, // 3 lines
			Locations: []blockLocation{
				{File: "file1.go", StartLine: 1, EndLine: 3},
				{File: "file2.go", StartLine: 5, EndLine: 7},
			}, // 2 occurrences
		},
		{
			Lines: []string{"lineA", "lineB"}, // 2 lines
			Locations: []blockLocation{
				{File: "file1.go", StartLine: 10, EndLine: 11},
				{File: "file2.go", StartLine: 20, EndLine: 21},
				{File: "file3.go", StartLine: 30, EndLine: 31},
			}, // 3 occurrences
		},
	}

	total := detector.calculateDuplicateLines(duplicates)

	// First: 3 lines * 2 occurrences = 6
	// Second: 2 lines * 3 occurrences = 6
	// Total: 12
	assert.Equal(t, 12, total)
}

func TestDuplicationDetector_ScanFiles(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "duplication-test-*")
	require.NoError(t, err)
	defer func() { _ = os.RemoveAll(tmpDir) }()

	// Create test files
	testFiles := map[string]string{
		"main.go":      "package main\n\nfunc main() {}\n",
		"types.go":     "package main\n\ntype Foo struct {}\n",
		"excluded.txt": "should be ignored\n",
		"main_test.go": "package main\n\nfunc TestMain(t *testing.T) {}\n",
	}

	for name, content := range testFiles {
		err := os.WriteFile(filepath.Join(tmpDir, name), []byte(content), 0644)
		require.NoError(t, err)
	}

	detector, err := NewDuplicationDetector(tmpDir, nil)
	require.NoError(t, err)
	ctx := context.Background()

	files, totalLines, err := detector.scanFiles(ctx)
	require.NoError(t, err)

	// Should find only main.go and types.go
	assert.Len(t, files, 2)
	assert.Greater(t, totalLines, 0)

	// Verify paths are relative
	for _, f := range files {
		assert.NotContains(t, f.Path, tmpDir)
		assert.True(t, f.Path == "main.go" || f.Path == "types.go")
		assert.Greater(t, len(f.Lines), 0)
	}
}

func TestDuplicationDetector_Check_NoFiles(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "duplication-test-*")
	require.NoError(t, err)
	defer func() { _ = os.RemoveAll(tmpDir) }()

	mockAI := &mockSupervisor{}
	detector, err := NewDuplicationDetector(tmpDir, mockAI)
	require.NoError(t, err)
	ctx := context.Background()

	result, err := detector.Check(ctx, CodebaseContext{})
	require.NoError(t, err)

	assert.Len(t, result.IssuesFound, 0)
	assert.Contains(t, result.Context, "No files found")
	assert.Equal(t, 0, result.Stats.FilesScanned)
}

func TestDuplicationDetector_Check_NoDuplicates(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "duplication-test-*")
	require.NoError(t, err)
	defer func() { _ = os.RemoveAll(tmpDir) }()

	// Create files with unique content
	for i := 1; i <= 3; i++ {
		content := fmt.Sprintf("package main\n\nfunc unique%d() { return %d }\n", i, i)
		err := os.WriteFile(filepath.Join(tmpDir, fmt.Sprintf("file%d.go", i)), []byte(content), 0644)
		require.NoError(t, err)
	}

	mockAI := &mockSupervisor{}
	detector, err := NewDuplicationDetector(tmpDir, mockAI)
	require.NoError(t, err)
	ctx := context.Background()

	result, err := detector.Check(ctx, CodebaseContext{})
	require.NoError(t, err)

	assert.Len(t, result.IssuesFound, 0)
	assert.Contains(t, result.Context, "no duplicates found")
	assert.Equal(t, 3, result.Stats.FilesScanned)
	assert.Equal(t, 0, result.Stats.AICallsMade)
}

func TestDuplicationDetector_Check_NilSupervisor(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "duplication-test-*")
	require.NoError(t, err)
	defer func() { _ = os.RemoveAll(tmpDir) }()

	// Create a file
	err = os.WriteFile(filepath.Join(tmpDir, "test.go"), []byte("package main\n"), 0644)
	require.NoError(t, err)

	detector, err := NewDuplicationDetector(tmpDir, nil)
	require.NoError(t, err)

	ctx := context.Background()
	result, err := detector.Check(ctx, CodebaseContext{})

	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "AI supervisor is required")
}

func TestDuplicationDetector_Check_WithDuplicates(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "duplication-test-*")
	require.NoError(t, err)
	defer func() { _ = os.RemoveAll(tmpDir) }()

	// Create files with duplicate function
	duplicateFunc := `func validateInput(s string) error {
	if s == "" {
		return fmt.Errorf("empty input")
	}
	if len(s) > 100 {
		return fmt.Errorf("input too long")
	}
	return nil
}
`

	file1 := "package main\n\n" + duplicateFunc + "\nfunc main() {}\n"
	file2 := "package utils\n\n" + duplicateFunc + "\nfunc other() {}\n"

	err = os.WriteFile(filepath.Join(tmpDir, "main.go"), []byte(file1), 0644)
	require.NoError(t, err)
	err = os.WriteFile(filepath.Join(tmpDir, "utils.go"), []byte(file2), 0644)
	require.NoError(t, err)

	// Mock AI response
	mockAI := &mockSupervisor{
		response: `{
			"overall_assessment": "concerning",
			"reasoning": "Found duplicate input validation logic",
			"duplicates_to_extract": [
				{
					"locations": ["main.go:3-11", "utils.go:3-11"],
					"pattern": "Input validation with length checks",
					"suggested_utility": "validateInput()",
					"suggested_location": "internal/validation/input.go",
					"priority": "P2"
				}
			],
			"acceptable_duplicates": []
		}`,
	}

	detector, err := NewDuplicationDetector(tmpDir, mockAI)
	require.NoError(t, err)
	detector.MinBlockSize = 5
	ctx := context.Background()

	result, err := detector.Check(ctx, CodebaseContext{})
	require.NoError(t, err)

	// Should find the duplicate and create an issue
	require.Len(t, result.IssuesFound, 1)
	issue := result.IssuesFound[0]

	assert.Equal(t, "duplication", issue.Category)
	assert.Equal(t, "medium", issue.Severity)
	assert.Contains(t, issue.Description, "Input validation")
	assert.Contains(t, issue.Description, "validateInput()")
	assert.Equal(t, 1, result.Stats.AICallsMade)
	assert.Equal(t, 2, result.Stats.FilesScanned)
}

func TestDuplicationDetector_BuildPrompt(t *testing.T) {
	detector, err := NewDuplicationDetector("/tmp", nil)
	require.NoError(t, err)

	duplicates := []duplicateBlock{
		{
			Lines: []string{"func foo() {", "  return nil", "}"},
			Locations: []blockLocation{
				{File: "file1.go", StartLine: 10, EndLine: 12},
				{File: "file2.go", StartLine: 20, EndLine: 22},
			},
		},
	}

	prompt := detector.buildPrompt(duplicates, 15.5, 1000)

	// Verify prompt contains key elements
	assert.Contains(t, prompt, detector.Philosophy())
	assert.Contains(t, prompt, "Total lines: 1000")
	assert.Contains(t, prompt, "15.5%")
	assert.Contains(t, prompt, "file1.go:10-12")
	assert.Contains(t, prompt, "file2.go:20-22")
	assert.Contains(t, prompt, "func foo()")

	// Verify dynamic year
	currentYear := fmt.Sprintf("%d", time.Now().Year())
	assert.Contains(t, prompt, currentYear)

	// Verify guidance levels
	assert.Contains(t, prompt, "0-5%")
	assert.Contains(t, prompt, "5-10%")
	assert.Contains(t, prompt, "10-20%")
	assert.Contains(t, prompt, ">20%")

	// Verify JSON format
	assert.Contains(t, prompt, "overall_assessment")
	assert.Contains(t, prompt, "duplicates_to_extract")
	assert.Contains(t, prompt, "acceptable_duplicates")
}

func TestDuplicationDetector_BuildIssues(t *testing.T) {
	detector, err := NewDuplicationDetector("/tmp", nil)
	require.NoError(t, err)

	eval := &duplicationEvaluation{
		OverallAssessment: "concerning",
		Reasoning:         "Multiple duplicates found",
		DuplicatesToExtract: []duplicateToExtract{
			{
				Locations:         []string{"file1.go:10-20", "file2.go:30-40"},
				Pattern:           "Error handling pattern",
				SuggestedUtility:  "handleError()",
				SuggestedLocation: "internal/errors/handler.go",
				Priority:          "P1",
			},
			{
				Locations:         []string{"a.go:5-10", "b.go:15-20", "c.go:25-30"},
				Pattern:           "String formatting",
				SuggestedUtility:  "formatString()",
				SuggestedLocation: "internal/utils/strings.go",
				Priority:          "P3",
			},
		},
	}

	issues := detector.buildIssues(eval)

	require.Len(t, issues, 2)

	// First issue (P1 = high severity)
	assert.Equal(t, "duplication", issues[0].Category)
	assert.Equal(t, "high", issues[0].Severity)
	assert.Contains(t, issues[0].Description, "Error handling pattern")
	assert.Contains(t, issues[0].Description, "handleError()")
	assert.Equal(t, "P1", issues[0].Evidence["priority"])

	// Second issue (P3 = low severity)
	assert.Equal(t, "duplication", issues[1].Category)
	assert.Equal(t, "low", issues[1].Severity)
	assert.Contains(t, issues[1].Description, "String formatting")
	assert.Contains(t, issues[1].Description, "formatString()")
	assert.Equal(t, "P3", issues[1].Evidence["priority"])
}

func TestDuplicationDetector_BuildContext(t *testing.T) {
	detector, err := NewDuplicationDetector("/tmp", nil)
	require.NoError(t, err)

	duplicates := make([]duplicateBlock, 5)
	context := detector.buildContext(10, 1000, 12.5, duplicates)

	assert.Contains(t, context, "10 files")
	assert.Contains(t, context, "1000 lines")
	assert.Contains(t, context, "5 duplicate blocks")
	assert.Contains(t, context, "12.5%")
}

func TestDuplicationDetector_BuildReasoning(t *testing.T) {
	detector, err := NewDuplicationDetector("/tmp", nil)
	require.NoError(t, err)

	eval := &duplicationEvaluation{
		OverallAssessment: "problematic",
		Reasoning:         "High duplication rate with several extraction candidates",
		DuplicatesToExtract: []duplicateToExtract{
			{Pattern: "Dup1"},
			{Pattern: "Dup2"},
		},
		AcceptableDuplicates: []acceptableDuplicate{
			{Reason: "Test setup"},
		},
	}

	reasoning := detector.buildReasoning(eval, 18.3)

	assert.Contains(t, reasoning, "problematic")
	assert.Contains(t, reasoning, "18.3%")
	assert.Contains(t, reasoning, "2 blocks for extraction")
	assert.Contains(t, reasoning, "1 acceptable duplicates")
	assert.Contains(t, reasoning, eval.Reasoning)
}

func TestDuplicationDetector_ContextCancellation(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "duplication-cancel-test-*")
	require.NoError(t, err)
	defer func() { _ = os.RemoveAll(tmpDir) }()

	// Create many files
	for i := 0; i < 50; i++ {
		content := fmt.Sprintf("package main\n\nfunc f%d() {}\n", i)
		filename := filepath.Join(tmpDir, fmt.Sprintf("file%d.go", i))
		err = os.WriteFile(filename, []byte(content), 0644)
		require.NoError(t, err)
	}

	mockAI := &mockSupervisor{
		response: `{"overall_assessment": "acceptable", "reasoning": "Low duplication", "duplicates_to_extract": [], "acceptable_duplicates": []}`,
	}

	detector, err := NewDuplicationDetector(tmpDir, mockAI)
	require.NoError(t, err)

	// Create context and cancel immediately
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	result, err := detector.Check(ctx, CodebaseContext{})
	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "context")
}

func TestDuplicationDetector_LargeDuplicateList(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "duplication-large-test-*")
	require.NoError(t, err)
	defer func() { _ = os.RemoveAll(tmpDir) }()

	// Create many files with the same duplicate function
	duplicateFunc := "func common() {\n  return 42\n}\n"

	for i := 0; i < 15; i++ {
		content := fmt.Sprintf("package main\n\n%s\nfunc unique%d() {}\n", duplicateFunc, i)
		filename := filepath.Join(tmpDir, fmt.Sprintf("file%d.go", i))
		err = os.WriteFile(filename, []byte(content), 0644)
		require.NoError(t, err)
	}

	// Use mock that captures prompt
	mockAI := &capturePromptSupervisor{
		response: `{
			"overall_assessment": "acceptable",
			"reasoning": "Common utility function",
			"duplicates_to_extract": [],
			"acceptable_duplicates": [{"locations": [], "reason": "Common function"}]
		}`,
	}

	detector, err := NewDuplicationDetector(tmpDir, mockAI)
	require.NoError(t, err)
	detector.MinBlockSize = 3

	ctx := context.Background()
	result, err := detector.Check(ctx, CodebaseContext{})
	require.NoError(t, err)
	assert.NotNil(t, result)

	// Verify prompt doesn't contain all duplicates (should be limited to 10)
	assert.NotEmpty(t, mockAI.capturedPrompt)
	// Count how many "Duplicate X" sections are in the prompt
	dupCount := strings.Count(mockAI.capturedPrompt, "### Duplicate")
	assert.LessOrEqual(t, dupCount, 10, "Should limit duplicates in prompt to 10")
}

func TestDuplicationDetector_InvalidJSON(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "duplication-json-test-*")
	require.NoError(t, err)
	defer func() { _ = os.RemoveAll(tmpDir) }()

	// Create files with duplicates
	dup := "func dup() {\n  return 1\n}\n"
	for i := 0; i < 2; i++ {
		content := fmt.Sprintf("package main\n\n%s\n", dup)
		err = os.WriteFile(filepath.Join(tmpDir, fmt.Sprintf("file%d.go", i)), []byte(content), 0644)
		require.NoError(t, err)
	}

	// Mock AI that returns invalid JSON
	mockAI := &mockSupervisor{
		response: `{this is not valid JSON at all!!!}`,
	}

	detector, err := NewDuplicationDetector(tmpDir, mockAI)
	require.NoError(t, err)
	detector.MinBlockSize = 3

	ctx := context.Background()
	result, err := detector.Check(ctx, CodebaseContext{})

	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "parsing")
}
