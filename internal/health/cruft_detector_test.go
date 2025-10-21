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

func TestCruftDetector_Interface(t *testing.T) {
	detector, err := NewCruftDetector("/tmp", nil)
	require.NoError(t, err)

	assert.Equal(t, "cruft_detector", detector.Name())
	assert.Contains(t, detector.Philosophy(), "Development artifacts")
	assert.Contains(t, detector.Philosophy(), "source control")
	assert.Equal(t, ScheduleTimeBased, detector.Schedule().Type)
	assert.Equal(t, 7*24*time.Hour, detector.Schedule().Interval) // Weekly
	assert.Equal(t, CostCheap, detector.Cost().Category)
}

func TestNewCruftDetector_PathValidation(t *testing.T) {
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
			detector, err := NewCruftDetector(tt.rootPath, nil)
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

func TestCruftDetector_ScanFiles(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "cruft-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	// Create various test files
	testFiles := map[string]string{
		"main.go":           "package main\n",
		"main.go.bak":       "old backup\n",
		"config.tmp":        "temp config\n",
		"data.old":          "old version\n",
		"script_backup.sh":  "backup script\n",
		"notes_old.txt":     "old notes\n",
		".DS_Store":         "macos cruft\n",
		"Thumbs.db":         "windows cruft\n",
		"editor.swp":        "vim swap\n",
		"file~":             "editor backup\n",
		"legitimate.go":     "package legitimate\n",
	}

	for name, content := range testFiles {
		err := os.WriteFile(filepath.Join(tmpDir, name), []byte(content), 0644)
		require.NoError(t, err)
	}

	// Create testdata directory with cruft (should be excluded)
	testdataDir := filepath.Join(tmpDir, "testdata")
	err = os.MkdirAll(testdataDir, 0755)
	require.NoError(t, err)
	err = os.WriteFile(filepath.Join(testdataDir, "test.bak"), []byte("test fixture\n"), 0644)
	require.NoError(t, err)

	detector, err := NewCruftDetector(tmpDir, nil)
	require.NoError(t, err)
	ctx := context.Background()

	cruftFiles, err := detector.scanFiles(ctx)
	require.NoError(t, err)

	// Should find 9 cruft files (excluding testdata/test.bak and legitimate files)
	assert.GreaterOrEqual(t, len(cruftFiles), 9)

	// Verify cruft files are correctly identified
	foundFiles := make(map[string]bool)
	for _, cf := range cruftFiles {
		foundFiles[cf.Path] = true
	}

	// These should be found
	assert.True(t, foundFiles["main.go.bak"], "*.bak should be found")
	assert.True(t, foundFiles["config.tmp"], "*.tmp should be found")
	assert.True(t, foundFiles["data.old"], "*.old should be found")
	assert.True(t, foundFiles[".DS_Store"], ".DS_Store should be found")
	assert.True(t, foundFiles["Thumbs.db"], "Thumbs.db should be found")

	// These should not be found
	assert.False(t, foundFiles["main.go"], "normal .go files should not be found")
	assert.False(t, foundFiles["legitimate.go"], "normal .go files should not be found")
	assert.False(t, foundFiles[filepath.Join("testdata", "test.bak")], "testdata files should be excluded")
}

func TestCruftDetector_PatternMatching(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "cruft-pattern-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	// Test all cruft patterns
	cruftPatterns := map[string]string{
		"file.bak":        "*.bak",
		"temp.tmp":        "*.tmp",
		"data.temp":       "*.temp",
		"script.old":      "*.old",
		"config_backup.json": "*_backup.*",
		"data_old.csv":    "*_old.*",
		"editor.swp":      "*.swp",
		"buffer.swo":      "*.swo",
		"notes~":          "*~",
		".DS_Store":       ".DS_Store",
		"Thumbs.db":       "Thumbs.db",
	}

	for filename := range cruftPatterns {
		err := os.WriteFile(filepath.Join(tmpDir, filename), []byte("test\n"), 0644)
		require.NoError(t, err)
	}

	detector, err := NewCruftDetector(tmpDir, nil)
	require.NoError(t, err)
	ctx := context.Background()

	cruftFiles, err := detector.scanFiles(ctx)
	require.NoError(t, err)

	// Should find all cruft files
	assert.Equal(t, len(cruftPatterns), len(cruftFiles))

	// Verify each file is matched with correct pattern
	filePatterns := make(map[string]string)
	for _, cf := range cruftFiles {
		filePatterns[cf.Path] = cf.Pattern
	}

	for filename, expectedPattern := range cruftPatterns {
		pattern, found := filePatterns[filename]
		assert.True(t, found, "File %s should be found", filename)
		assert.Equal(t, expectedPattern, pattern, "File %s should match pattern %s", filename, expectedPattern)
	}
}

func TestCruftDetector_ExcludePatterns(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "cruft-exclude-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	// Create directory structure
	dirs := []string{
		"vendor",
		".git",
		"testdata",
		"node_modules",
		".beads",
		"src",
	}
	for _, dir := range dirs {
		err := os.MkdirAll(filepath.Join(tmpDir, dir), 0755)
		require.NoError(t, err)
	}

	// Create cruft files in various locations
	files := map[string]bool{
		"cruft.bak":                     true,  // Should be found
		"vendor/lib.bak":                false, // Should be excluded
		".git/config.bak":               false, // Should be excluded
		"testdata/fixture.bak":          false, // Should be excluded
		"node_modules/package.bak":      false, // Should be excluded
		".beads/issues.bak":             false, // Should be excluded
		"src/main.bak":                  true,  // Should be found
	}

	for path := range files {
		fullPath := filepath.Join(tmpDir, path)
		// Ensure parent directory exists
		dir := filepath.Dir(fullPath)
		if dir != tmpDir {
			err := os.MkdirAll(dir, 0755)
			require.NoError(t, err)
		}
		err := os.WriteFile(fullPath, []byte("test\n"), 0644)
		require.NoError(t, err)
	}

	detector, err := NewCruftDetector(tmpDir, nil)
	require.NoError(t, err)
	ctx := context.Background()

	cruftFiles, err := detector.scanFiles(ctx)
	require.NoError(t, err)

	foundFiles := make(map[string]bool)
	for _, cf := range cruftFiles {
		foundFiles[cf.Path] = true
	}

	for path, shouldFind := range files {
		if shouldFind {
			assert.True(t, foundFiles[path], "File %s should be found", path)
		} else {
			assert.False(t, foundFiles[path], "File %s should be excluded", path)
		}
	}
}

func TestCruftDetector_BuildPrompt(t *testing.T) {
	detector, err := NewCruftDetector("/tmp", nil)
	require.NoError(t, err)

	files := []cruftFile{
		{Path: "main.go.bak", Pattern: "*.bak"},
		{Path: "config.tmp", Pattern: "*.tmp"},
		{Path: ".DS_Store", Pattern: ".DS_Store"},
	}

	prompt := detector.buildPrompt(files)

	// Verify prompt contains key elements
	assert.Contains(t, prompt, detector.Philosophy())
	assert.Contains(t, prompt, "main.go.bak")
	assert.Contains(t, prompt, "config.tmp")
	assert.Contains(t, prompt, ".DS_Store")
	assert.Contains(t, prompt, "*.bak")
	assert.Contains(t, prompt, "*.tmp")

	// Verify dynamic year
	currentYear := fmt.Sprintf("%d", time.Now().Year())
	assert.Contains(t, prompt, currentYear)

	// Verify JSON structure
	assert.Contains(t, prompt, "cruft_to_delete")
	assert.Contains(t, prompt, "patterns_to_ignore")
	assert.Contains(t, prompt, "legitimate_files")
	assert.Contains(t, prompt, "reasoning")
}

func TestCruftDetector_BuildIssues(t *testing.T) {
	detector, err := NewCruftDetector("/tmp", nil)
	require.NoError(t, err)

	eval := &cruftEvaluation{
		CruftToDelete: []cruftFileAction{
			{
				File:    "main.go.bak",
				Reason:  "Editor backup",
				Pattern: "*.bak",
			},
			{
				File:    "config.tmp",
				Reason:  "Temporary file",
				Pattern: "*.tmp",
			},
		},
		PatternsToIgnore: []string{"*.bak", "*.tmp"},
		LegitimateFiles: []legitimateFile{
			{
				File:          "restore_backup.go",
				Justification: "Source code for backup functionality",
			},
		},
		Reasoning: "Found 2 true cruft files",
	}

	issues := detector.buildIssues(eval)

	require.Len(t, issues, 1)
	issue := issues[0]

	assert.Equal(t, "cruft", issue.Category)
	assert.Equal(t, "low", issue.Severity) // 2 files = low severity
	assert.Contains(t, issue.Description, "2 files to delete")
	assert.Contains(t, issue.Description, "2 patterns to add")

	// Verify evidence
	assert.Equal(t, 2, issue.Evidence["files_to_delete"])
	assert.Equal(t, 2, issue.Evidence["patterns_to_add"])
	assert.Equal(t, "Found 2 true cruft files", issue.Evidence["reasoning"])
}

func TestCruftDetector_CalculateSeverity(t *testing.T) {
	detector, err := NewCruftDetector("/tmp", nil)
	require.NoError(t, err)

	tests := []struct {
		cruftCount int
		expected   string
	}{
		{3, "low"},
		{9, "low"},
		{10, "medium"},
		{19, "medium"},
		{20, "high"},
		{50, "high"},
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("%d_files", tt.cruftCount), func(t *testing.T) {
			severity := detector.calculateSeverity(tt.cruftCount)
			assert.Equal(t, tt.expected, severity)
		})
	}
}

func TestCruftDetector_BuildIssues_WeightedSeverity(t *testing.T) {
	detector, err := NewCruftDetector("/tmp", nil)
	require.NoError(t, err)

	tests := []struct {
		name             string
		deletions        int
		patterns         int
		expectedSeverity string
	}{
		{
			name:             "patterns_only_medium",
			deletions:        0,
			patterns:         20,
			expectedSeverity: "medium", // weighted: 0 + 10 = 10
		},
		{
			name:             "patterns_only_high",
			deletions:        0,
			patterns:         40,
			expectedSeverity: "high", // weighted: 0 + 20 = 20
		},
		{
			name:             "mixed_medium",
			deletions:        5,
			patterns:         10,
			expectedSeverity: "medium", // weighted: 5 + 5 = 10
		},
		{
			name:             "mixed_low",
			deletions:        3,
			patterns:         6,
			expectedSeverity: "low", // weighted: 3 + 3 = 6
		},
		{
			name:             "deletions_only_high",
			deletions:        25,
			patterns:         0,
			expectedSeverity: "high", // weighted: 25 + 0 = 25
		},
		{
			name:             "both_high",
			deletions:        15,
			patterns:         20,
			expectedSeverity: "high", // weighted: 15 + 10 = 25
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Build evaluation with specified counts
			eval := &cruftEvaluation{
				CruftToDelete:    make([]cruftFileAction, tt.deletions),
				PatternsToIgnore: make([]string, tt.patterns),
				Reasoning:        "Test reasoning",
			}

			// Populate with dummy data
			for i := 0; i < tt.deletions; i++ {
				eval.CruftToDelete[i] = cruftFileAction{
					File:    fmt.Sprintf("file%d.bak", i),
					Reason:  "Test file",
					Pattern: "*.bak",
				}
			}
			for i := 0; i < tt.patterns; i++ {
				eval.PatternsToIgnore[i] = fmt.Sprintf("*.pattern%d", i)
			}

			issues := detector.buildIssues(eval)

			require.Len(t, issues, 1, "Should create one issue")
			issue := issues[0]
			assert.Equal(t, tt.expectedSeverity, issue.Severity,
				"Expected severity %s for %d deletions + %d patterns (weighted: %d)",
				tt.expectedSeverity, tt.deletions, tt.patterns,
				tt.deletions+(tt.patterns/2))
		})
	}
}

func TestCruftDetector_Check_NilSupervisor(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "cruft-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	// Create cruft files
	err = os.WriteFile(filepath.Join(tmpDir, "test.bak"), []byte("test\n"), 0644)
	require.NoError(t, err)

	detector, err := NewCruftDetector(tmpDir, nil)
	require.NoError(t, err)

	ctx := context.Background()
	result, err := detector.Check(ctx, CodebaseContext{})

	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "AI supervisor is required")
}

func TestCruftDetector_Check_BelowThreshold(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "cruft-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	// Create only 2 cruft files (below threshold of 3)
	err = os.WriteFile(filepath.Join(tmpDir, "file1.bak"), []byte("test\n"), 0644)
	require.NoError(t, err)
	err = os.WriteFile(filepath.Join(tmpDir, "file2.bak"), []byte("test\n"), 0644)
	require.NoError(t, err)

	mockAI := &mockSupervisor{}
	detector, err := NewCruftDetector(tmpDir, mockAI)
	require.NoError(t, err)

	ctx := context.Background()
	result, err := detector.Check(ctx, CodebaseContext{})
	require.NoError(t, err)

	assert.Len(t, result.IssuesFound, 0)
	assert.Contains(t, result.Context, "below threshold")
	assert.Equal(t, 2, result.Stats.FilesScanned)
	assert.Equal(t, 0, result.Stats.AICallsMade)
}

func TestCruftDetector_Check_WithAI(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "cruft-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	// Create 5 cruft files (above threshold)
	for i := 1; i <= 5; i++ {
		filename := filepath.Join(tmpDir, fmt.Sprintf("file%d.bak", i))
		err = os.WriteFile(filename, []byte("old backup\n"), 0644)
		require.NoError(t, err)
	}

	// Mock AI response
	mockAI := &mockSupervisor{
		response: `{
			"cruft_to_delete": [
				{
					"file": "file1.bak",
					"reason": "Editor backup",
					"pattern": "*.bak"
				},
				{
					"file": "file2.bak",
					"reason": "Editor backup",
					"pattern": "*.bak"
				}
			],
			"patterns_to_ignore": ["*.bak"],
			"legitimate_files": [],
			"reasoning": "All .bak files appear to be editor backups"
		}`,
	}

	detector, err := NewCruftDetector(tmpDir, mockAI)
	require.NoError(t, err)

	ctx := context.Background()
	result, err := detector.Check(ctx, CodebaseContext{})
	require.NoError(t, err)

	// Should create one grouped issue
	require.Len(t, result.IssuesFound, 1)
	issue := result.IssuesFound[0]

	assert.Equal(t, "cruft", issue.Category)
	assert.Contains(t, issue.Description, "2 files to delete")
	assert.Contains(t, issue.Description, "1 patterns to add")
	assert.Equal(t, 1, result.Stats.AICallsMade)
}

func TestCruftDetector_Check_ContextCancellation(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "cruft-cancel-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	// Create many cruft files
	for i := 0; i < 100; i++ {
		filename := filepath.Join(tmpDir, fmt.Sprintf("file%d.bak", i))
		err = os.WriteFile(filename, []byte("test\n"), 0644)
		require.NoError(t, err)
	}

	mockAI := &mockSupervisor{
		response: `{"cruft_to_delete": [], "patterns_to_ignore": [], "legitimate_files": [], "reasoning": "test"}`,
	}

	detector, err := NewCruftDetector(tmpDir, mockAI)
	require.NoError(t, err)

	// Create context that we'll cancel
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	// Should get context canceled error
	result, err := detector.Check(ctx, CodebaseContext{})
	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "context")
}

func TestCruftDetector_Check_InvalidJSON(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "cruft-json-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	// Create cruft files above threshold
	for i := 0; i < 5; i++ {
		filename := filepath.Join(tmpDir, fmt.Sprintf("file%d.bak", i))
		err = os.WriteFile(filename, []byte("test\n"), 0644)
		require.NoError(t, err)
	}

	// Mock AI that returns invalid JSON
	mockAI := &mockSupervisor{
		response: `{invalid json: not valid at all}`,
	}

	detector, err := NewCruftDetector(tmpDir, mockAI)
	require.NoError(t, err)

	ctx := context.Background()
	result, err := detector.Check(ctx, CodebaseContext{})

	// Should get error due to invalid JSON parsing
	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "parsing")
}

func TestCruftDetector_Check_ErrorTruncation(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "cruft-truncate-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	// Create cruft files above threshold
	for i := 0; i < 5; i++ {
		filename := filepath.Join(tmpDir, fmt.Sprintf("file%d.bak", i))
		err = os.WriteFile(filename, []byte("test\n"), 0644)
		require.NoError(t, err)
	}

	// Mock AI that returns huge invalid JSON (>500 chars)
	hugeResponse := "INVALID JSON: " + strings.Repeat("X", 1000)
	mockAI := &mockSupervisor{
		response: hugeResponse,
	}

	detector, err := NewCruftDetector(tmpDir, mockAI)
	require.NoError(t, err)

	ctx := context.Background()
	result, err := detector.Check(ctx, CodebaseContext{})

	// Should get error due to invalid JSON
	assert.Error(t, err)
	assert.Nil(t, result)

	// Error message should be truncated to ~500 chars, not the full 1000+
	errMsg := err.Error()
	assert.LessOrEqual(t, len(errMsg), 700, "Error message should be truncated")
	assert.Contains(t, errMsg, "truncated", "Error should indicate truncation")
}

func TestCruftDetector_Check_OnlyDeleteNoPatterns(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "cruft-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	// Create cruft files
	for i := 0; i < 5; i++ {
		filename := filepath.Join(tmpDir, fmt.Sprintf("file%d.bak", i))
		err = os.WriteFile(filename, []byte("test\n"), 0644)
		require.NoError(t, err)
	}

	// AI says delete files but don't add patterns (already in .gitignore)
	mockAI := &mockSupervisor{
		response: `{
			"cruft_to_delete": [
				{"file": "file1.bak", "reason": "Backup", "pattern": "*.bak"}
			],
			"patterns_to_ignore": [],
			"legitimate_files": [],
			"reasoning": "Patterns already in .gitignore"
		}`,
	}

	detector, err := NewCruftDetector(tmpDir, mockAI)
	require.NoError(t, err)

	ctx := context.Background()
	result, err := detector.Check(ctx, CodebaseContext{})
	require.NoError(t, err)

	require.Len(t, result.IssuesFound, 1)
	issue := result.IssuesFound[0]
	assert.Contains(t, issue.Description, "1 files to delete")
	assert.NotContains(t, issue.Description, "patterns to add")
}

func TestCruftDetector_Check_OnlyPatternsNoDelete(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "cruft-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	// Create cruft files
	for i := 0; i < 5; i++ {
		filename := filepath.Join(tmpDir, fmt.Sprintf("file%d.bak", i))
		err = os.WriteFile(filename, []byte("test\n"), 0644)
		require.NoError(t, err)
	}

	// AI says all files are legitimate but add patterns for prevention
	mockAI := &mockSupervisor{
		response: `{
			"cruft_to_delete": [],
			"patterns_to_ignore": ["*.bak"],
			"legitimate_files": [
				{"file": "file1.bak", "justification": "Test fixture"}
			],
			"reasoning": "Files are test fixtures but add pattern to prevent real cruft"
		}`,
	}

	detector, err := NewCruftDetector(tmpDir, mockAI)
	require.NoError(t, err)

	ctx := context.Background()
	result, err := detector.Check(ctx, CodebaseContext{})
	require.NoError(t, err)

	require.Len(t, result.IssuesFound, 1)
	issue := result.IssuesFound[0]
	assert.Contains(t, issue.Description, "1 patterns to add")
	assert.NotContains(t, issue.Description, "files to delete")
}

func TestCruftDetector_Check_AllLegitimate(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "cruft-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	// Create files that match patterns but are legitimate
	// Use .bak files which clearly match
	for i := 0; i < 5; i++ {
		filename := filepath.Join(tmpDir, fmt.Sprintf("schema_v%d.bak", i))
		err = os.WriteFile(filename, []byte("schema backup\n"), 0644)
		require.NoError(t, err)
	}

	// AI says all files are legitimate (they're versioned schema backups)
	mockAI := &mockSupervisor{
		response: `{
			"cruft_to_delete": [],
			"patterns_to_ignore": [],
			"legitimate_files": [
				{"file": "schema_v0.bak", "justification": "Versioned schema backups used in tests"},
				{"file": "schema_v1.bak", "justification": "Versioned schema backups used in tests"}
			],
			"reasoning": "All .bak files are versioned schema backups used by test suite, not cruft"
		}`,
	}

	detector, err := NewCruftDetector(tmpDir, mockAI)
	require.NoError(t, err)

	ctx := context.Background()
	result, err := detector.Check(ctx, CodebaseContext{})
	require.NoError(t, err)

	// Should not create any issues (all legitimate)
	assert.Len(t, result.IssuesFound, 0)
	assert.Contains(t, result.Context, "5 files matching cruft patterns")
	assert.Contains(t, result.Context, "0 as true cruft")
}

func TestCruftDetector_BuildReasoning(t *testing.T) {
	detector, err := NewCruftDetector("/tmp", nil)
	require.NoError(t, err)

	tests := []struct {
		name     string
		eval     *cruftEvaluation
		expected string
	}{
		{
			name: "with AI reasoning",
			eval: &cruftEvaluation{
				CruftToDelete:    []cruftFileAction{{File: "test.bak"}},
				PatternsToIgnore: []string{"*.bak"},
				Reasoning:        "Custom AI reasoning here",
			},
			expected: "Custom AI reasoning here",
		},
		{
			name: "without AI reasoning",
			eval: &cruftEvaluation{
				CruftToDelete:    []cruftFileAction{{File: "test.bak"}, {File: "other.tmp"}},
				PatternsToIgnore: []string{"*.bak"},
				Reasoning:        "",
			},
			expected: "2 development artifacts to remove and 1 .gitignore patterns to add",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reasoning := detector.buildReasoning(tt.eval)
			assert.Contains(t, reasoning, tt.expected)
		})
	}
}

// capturingMockSupervisor captures the prompt sent to AI for verification
type capturingMockSupervisor struct {
	response       string
	capturedPrompt string
}

func (m *capturingMockSupervisor) CallAI(ctx context.Context, prompt string, operation string, model string, maxTokens int) (string, error) {
	m.capturedPrompt = prompt
	return m.response, nil
}

func TestCruftDetector_Check_LargeNumberOfFiles(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "cruft-large-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	// Create 100 cruft files (well above the 50 file limit)
	for i := 0; i < 100; i++ {
		filename := filepath.Join(tmpDir, fmt.Sprintf("file%d.bak", i))
		err = os.WriteFile(filename, []byte("backup content\n"), 0644)
		require.NoError(t, err)
	}

	// Use capturing mock to verify what was sent to AI
	mockAI := &capturingMockSupervisor{
		response: `{
			"cruft_to_delete": [
				{
					"file": "file0.bak",
					"reason": "Editor backup",
					"pattern": "*.bak"
				}
			],
			"patterns_to_ignore": ["*.bak"],
			"legitimate_files": [],
			"reasoning": "All .bak files are editor backups"
		}`,
	}

	detector, err := NewCruftDetector(tmpDir, mockAI)
	require.NoError(t, err)

	ctx := context.Background()
	result, err := detector.Check(ctx, CodebaseContext{})
	require.NoError(t, err)

	// Verify the check succeeded
	require.NotNil(t, result)
	assert.Equal(t, 1, result.Stats.AICallsMade)

	// Verify that only 50 files were sent to AI (not all 100)
	// Count file lines in the prompt (format: "- fileN.bak (pattern: *.bak)")
	prompt := mockAI.capturedPrompt
	fileLineCount := 0
	for _, line := range strings.Split(prompt, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "- file") && strings.Contains(line, "(pattern:") {
			fileLineCount++
		}
	}
	assert.Equal(t, 50, fileLineCount, "Should send exactly 50 files to AI, not all 100")

	// Verify the prompt mentions "Found 50 files" (not 100)
	assert.Contains(t, prompt, "Found 50 files matching cruft patterns")
}
