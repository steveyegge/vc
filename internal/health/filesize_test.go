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

func TestFileSizeMonitor_Interface(t *testing.T) {
	monitor, err := NewFileSizeMonitor("/tmp", nil)
	require.NoError(t, err)

	assert.Equal(t, "file_size_monitor", monitor.Name())
	assert.Contains(t, monitor.Philosophy(), "single responsibility")
	assert.Equal(t, ScheduleTimeBased, monitor.Schedule().Type)
	assert.Equal(t, CostModerate, monitor.Cost().Category)
}

func TestFileSizeMonitor_CalculateDistribution(t *testing.T) {
	monitor, err := NewFileSizeMonitor("/tmp", nil)
	require.NoError(t, err)

	sizes := []fileSize{
		{Path: "a.go", Lines: 100},
		{Path: "b.go", Lines: 200},
		{Path: "c.go", Lines: 300},
		{Path: "d.go", Lines: 400},
		{Path: "e.go", Lines: 500},
	}

	dist := monitor.calculateDistribution(sizes)

	assert.Equal(t, 300.0, dist.Mean)
	assert.Equal(t, 300.0, dist.Median)
	assert.Equal(t, 100.0, dist.Min)
	assert.Equal(t, 500.0, dist.Max)
	assert.Equal(t, 5, dist.Count)
	assert.Greater(t, dist.StdDev, 0.0)
}

func TestFileSizeMonitor_CalculateDistribution_Empty(t *testing.T) {
	monitor, err := NewFileSizeMonitor("/tmp", nil)
	require.NoError(t, err)

	dist := monitor.calculateDistribution([]fileSize{})

	assert.Equal(t, Distribution{}, dist)
}

func TestFileSizeMonitor_FindOutliers(t *testing.T) {
	monitor, err := NewFileSizeMonitor("/tmp", nil)
	require.NoError(t, err)
	monitor.OutlierThreshold = 1.5 // Use lower threshold for this test

	// Create distribution with a clear outlier
	// Small files around 10 lines, one huge file at 100 lines
	sizes := []fileSize{
		{Path: "small1.go", Lines: 10},
		{Path: "small2.go", Lines: 12},
		{Path: "small3.go", Lines: 11},
		{Path: "small4.go", Lines: 9},
		{Path: "outlier.go", Lines: 100}, // Way above the rest
	}

	dist := monitor.calculateDistribution(sizes)
	outliers := monitor.findOutliers(sizes, dist)

	// Debug: print actual values
	t.Logf("Mean: %.2f, StdDev: %.2f, Threshold: %.2f",
		dist.Mean, dist.StdDev, dist.Mean + (monitor.OutlierThreshold * dist.StdDev))

	require.Len(t, outliers, 1)
	assert.Equal(t, "outlier.go", outliers[0].Path)
	assert.Equal(t, 100, outliers[0].Lines)
}

func TestFileSizeMonitor_FindOutliers_NoOutliers(t *testing.T) {
	monitor, err := NewFileSizeMonitor("/tmp", nil)
	require.NoError(t, err)
	monitor.OutlierThreshold = 3.0

	// All files within 3σ of mean
	sizes := []fileSize{
		{Path: "a.go", Lines: 90},
		{Path: "b.go", Lines: 100},
		{Path: "c.go", Lines: 110},
	}

	dist := monitor.calculateDistribution(sizes)
	outliers := monitor.findOutliers(sizes, dist)

	assert.Len(t, outliers, 0)
}

func TestFileSizeMonitor_CalculateSeverity(t *testing.T) {
	monitor, err := NewFileSizeMonitor("/tmp", nil)
	require.NoError(t, err)

	dist := Distribution{
		Mean:   100.0,
		StdDev: 20.0,
	}

	tests := []struct {
		lines    int
		expected string
	}{
		{150, "low"},    // 2.5σ above
		{160, "low"},    // 3.0σ above
		{161, "medium"}, // >3.0σ above
		{180, "medium"}, // 4.0σ above
		{181, "high"},   // >4.0σ above
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			severity := monitor.calculateSeverity(tt.lines, dist)
			assert.Equal(t, tt.expected, severity)
		})
	}
}

func TestFileSizeMonitor_ScanFiles(t *testing.T) {
	// Create temporary test directory
	tmpDir, err := os.MkdirTemp("", "filesize-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	// Create test files
	testFiles := map[string]string{
		"main.go":       "package main\n\nfunc main() {}\n",
		"types.go":      "package main\n\ntype Foo struct {}\n",
		"excluded.txt":  "should be ignored\n",
		"main_test.go":  "package main\n\nfunc TestMain(t *testing.T) {}\n", // Should be excluded
	}

	for name, content := range testFiles {
		err := os.WriteFile(filepath.Join(tmpDir, name), []byte(content), 0644)
		require.NoError(t, err)
	}

	// Create subdirectory with vendor (should be excluded)
	vendorDir := filepath.Join(tmpDir, "vendor")
	err = os.MkdirAll(vendorDir, 0755)
	require.NoError(t, err)
	err = os.WriteFile(filepath.Join(vendorDir, "vendor.go"), []byte("package vendor\n"), 0644)
	require.NoError(t, err)

	monitor, err := NewFileSizeMonitor(tmpDir, nil)
	require.NoError(t, err)
	ctx := context.Background()

	sizes, err := monitor.scanFiles(ctx)
	require.NoError(t, err)

	// Should find only main.go and types.go (excluding test files, vendor, and non-.go files)
	assert.Len(t, sizes, 2)

	// Verify file paths are relative
	for _, s := range sizes {
		assert.NotContains(t, s.Path, tmpDir)
		assert.True(t, s.Path == "main.go" || s.Path == "types.go")
		assert.Greater(t, s.Lines, 0)
	}
}

func TestFileSizeMonitor_Check_NoFiles(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "filesize-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	// Use mock supervisor (even though it won't be called)
	mockAI := &mockSupervisor{}
	monitor, err := NewFileSizeMonitor(tmpDir, mockAI)
	require.NoError(t, err)
	ctx := context.Background()

	result, err := monitor.Check(ctx, CodebaseContext{})
	require.NoError(t, err)

	assert.Len(t, result.IssuesFound, 0)
	assert.Contains(t, result.Context, "No files found")
	assert.Equal(t, 0, result.Stats.FilesScanned)
}

func TestFileSizeMonitor_Check_NoOutliers(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "filesize-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	// Create files with similar sizes (no outliers)
	for i := 1; i <= 5; i++ {
		content := "package main\n\nfunc foo() {}\n"
		err := os.WriteFile(filepath.Join(tmpDir, "file"+string(rune('0'+i))+".go"), []byte(content), 0644)
		require.NoError(t, err)
	}

	// Use mock supervisor (even though it won't be called for no outliers)
	mockAI := &mockSupervisor{}
	monitor, err := NewFileSizeMonitor(tmpDir, mockAI)
	require.NoError(t, err)
	monitor.OutlierThreshold = 2.5
	ctx := context.Background()

	result, err := monitor.Check(ctx, CodebaseContext{})
	require.NoError(t, err)

	assert.Len(t, result.IssuesFound, 0)
	assert.Contains(t, result.Context, "no outliers found")
	assert.Equal(t, 5, result.Stats.FilesScanned)
	assert.Equal(t, 0, result.Stats.AICallsMade)
}

func TestFileSizeMonitor_BuildPrompt(t *testing.T) {
	monitor, err := NewFileSizeMonitor("/tmp", nil)
	require.NoError(t, err)

	outliers := []fileSize{
		{Path: "large1.go", Lines: 1000},
		{Path: "large2.go", Lines: 800},
	}

	dist := Distribution{
		Mean:   200.0,
		Median: 180.0,
		StdDev: 50.0,
		P95:    350.0,
		P99:    450.0,
		Count:  100,
	}

	prompt := monitor.buildPrompt(outliers, dist)

	// Verify prompt contains key elements
	assert.Contains(t, prompt, monitor.Philosophy())
	assert.Contains(t, prompt, "200 lines") // Mean
	assert.Contains(t, prompt, "large1.go: 1000 lines")
	assert.Contains(t, prompt, "large2.go: 800 lines")
	// Verify dynamic year is included (not hardcoded "Late 2025")
	currentYear := fmt.Sprintf("%d", time.Now().Year())
	assert.Contains(t, prompt, currentYear)
	assert.Contains(t, prompt, "problematic_files")
	assert.Contains(t, prompt, "justified_files")
	assert.Contains(t, prompt, "JSON")
}

func TestFileSizeMonitor_BuildIssues(t *testing.T) {
	monitor, err := NewFileSizeMonitor("/tmp", nil)
	require.NoError(t, err)

	eval := &outlierEvaluation{
		ProblematicFiles: []problematicFile{
			{
				File:           "supervisor.go",
				Lines:          2844,
				Issue:          "Handles retry, assessment, analysis",
				SuggestedSplit: "retry.go, assessment.go, analysis.go",
			},
		},
		JustifiedFiles: []justifiedFile{
			{
				File:          "types.pb.go",
				Lines:         3500,
				Justification: "Generated protobuf code",
			},
		},
	}

	dist := Distribution{
		Mean:   200.0,
		StdDev: 100.0,
	}

	issues := monitor.buildIssues(eval, dist)

	require.Len(t, issues, 1)
	issue := issues[0]

	assert.Equal(t, "supervisor.go", issue.FilePath)
	assert.Equal(t, "file_size", issue.Category)
	assert.Equal(t, 2844, issue.Evidence["lines"])
	assert.Contains(t, issue.Description, "supervisor.go")
	assert.Contains(t, issue.Description, "2844 lines")
	assert.Contains(t, issue.Evidence, "suggested_split")
}

func TestCountLines(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "countlines-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	tests := []struct {
		name     string
		content  string
		expected int
	}{
		{
			name:     "empty file",
			content:  "",
			expected: 0,
		},
		{
			name:     "single line no newline",
			content:  "package main",
			expected: 1,
		},
		{
			name:     "single line with newline",
			content:  "package main\n",
			expected: 1,
		},
		{
			name:     "multiple lines",
			content:  "line1\nline2\nline3\n",
			expected: 3,
		},
		{
			name:     "multiple lines no trailing newline",
			content:  "line1\nline2\nline3",
			expected: 3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(tmpDir, tt.name+".txt")
			err := os.WriteFile(path, []byte(tt.content), 0644)
			require.NoError(t, err)

			count, err := countLines(path)
			require.NoError(t, err)
			assert.Equal(t, tt.expected, count)
		})
	}
}

// Mock AI supervisor for integration testing
type mockSupervisor struct {
	response string
	err      error
}

func (m *mockSupervisor) CallAI(ctx context.Context, prompt string, operation string, model string, maxTokens int) (string, error) {
	if m.err != nil {
		return "", m.err
	}
	return m.response, nil
}

func TestFileSizeMonitor_Check_WithAI(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "filesize-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	// Create several small files to establish a baseline
	for i := 1; i <= 5; i++ {
		small := "package main\n\nfunc main() {}\n"
		filename := filepath.Join(tmpDir, "small"+string(rune('0'+i))+".go")
		err = os.WriteFile(filename, []byte(small), 0644)
		require.NoError(t, err)
	}

	// Create a large file (clear outlier)
	large := "package main\n\n"
	for i := 0; i < 500; i++ {
		large += "// Comment line to make file large\n"
	}
	err = os.WriteFile(filepath.Join(tmpDir, "large.go"), []byte(large), 0644)
	require.NoError(t, err)

	// Mock AI response
	mockAI := &mockSupervisor{
		response: `{
			"problematic_files": [
				{
					"file": "large.go",
					"lines": 502,
					"issue": "File contains 500 comment lines with no real code",
					"suggested_split": "Remove unnecessary comments"
				}
			],
			"justified_files": []
		}`,
	}

	monitor, err := NewFileSizeMonitor(tmpDir, mockAI)
	require.NoError(t, err)
	monitor.OutlierThreshold = 2.0

	ctx := context.Background()
	result, err := monitor.Check(ctx, CodebaseContext{})
	require.NoError(t, err)

	// Should find the large outlier and mark it as problematic
	require.Len(t, result.IssuesFound, 1, "Expected 1 issue, stats: %+v", result.Stats)
	assert.Equal(t, "large.go", result.IssuesFound[0].FilePath)
	assert.Equal(t, "file_size", result.IssuesFound[0].Category)
	assert.Equal(t, 1, result.Stats.AICallsMade)
}

// ========== Tests for vc-211 Fixes ==========

// Test Fix #1: Nil supervisor check
func TestFileSizeMonitor_Check_NilSupervisor(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "filesize-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	// Create a file
	err = os.WriteFile(filepath.Join(tmpDir, "test.go"), []byte("package main\n"), 0644)
	require.NoError(t, err)

	monitor, err := NewFileSizeMonitor(tmpDir, nil)
	require.NoError(t, err)

	ctx := context.Background()
	result, err := monitor.Check(ctx, CodebaseContext{})

	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "AI supervisor is required")
}

// Test Fix #2: Stream-based line counting (verify it works with various file sizes)
func TestCountLines_StreamBased(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "countlines-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	tests := []struct {
		name        string
		lines       int
		withNewline bool
	}{
		{"small file", 10, true},
		{"medium file", 1000, true},
		{"large file", 10000, true},
		{"no trailing newline", 100, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(tmpDir, tt.name+".txt")
			f, err := os.Create(path)
			require.NoError(t, err)

			// Write lines
			for i := 0; i < tt.lines; i++ {
				if i < tt.lines-1 || tt.withNewline {
					_, err = f.WriteString(fmt.Sprintf("line %d\n", i))
				} else {
					_, err = f.WriteString(fmt.Sprintf("line %d", i))
				}
				require.NoError(t, err)
			}
			f.Close()

			count, err := countLines(path)
			require.NoError(t, err)
			assert.Equal(t, tt.lines, count)
		})
	}
}

// Test Fix #3: Path validation in constructor
func TestNewFileSizeMonitor_PathValidation(t *testing.T) {
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
			monitor, err := NewFileSizeMonitor(tt.rootPath, nil)
			if tt.shouldErr {
				assert.Error(t, err)
				assert.Nil(t, monitor)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, monitor)
				// Verify path is absolute
				assert.True(t, filepath.IsAbs(monitor.RootPath))
			}
		})
	}
}

// Test Fix #5: Pattern matching edge cases
func TestFileSizeMonitor_PatternMatching(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "filesize-pattern-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	// Create directory structure
	dirs := []string{
		"vendor",
		"vendorized",
		"src/vendor",
		".git",
	}
	for _, dir := range dirs {
		err := os.MkdirAll(filepath.Join(tmpDir, dir), 0755)
		require.NoError(t, err)
	}

	// Create test files
	files := map[string]string{
		"vendor/dep.go":       "package vendor\n",
		"vendorized/util.go":  "package vendorized\n",
		"src/vendor/lib.go":   "package lib\n",
		".git/config":         "config\n",
		"main.go":             "package main\n",
		"main_test.go":        "package main\n",
		"types.pb.go":         "package types\n",
		"handler.gen.go":      "package handler\n",
		"testdata/helper.go":  "package testdata\n",
	}

	for path, content := range files {
		fullPath := filepath.Join(tmpDir, path)
		// Create parent directory if it doesn't exist
		dir := filepath.Dir(fullPath)
		if dir != tmpDir {
			err := os.MkdirAll(dir, 0755)
			require.NoError(t, err)
		}
		err := os.WriteFile(fullPath, []byte(content), 0644)
		require.NoError(t, err)
	}

	monitor, err := NewFileSizeMonitor(tmpDir, nil)
	require.NoError(t, err)

	ctx := context.Background()
	sizes, err := monitor.scanFiles(ctx)
	require.NoError(t, err)

	// Should only find main.go and vendorized/util.go
	// (vendor/, .git/, _test.go, .pb.go, .gen.go, testdata/ are excluded)
	foundFiles := make(map[string]bool)
	for _, s := range sizes {
		foundFiles[s.Path] = true
	}

	// Should be included
	assert.True(t, foundFiles["main.go"], "main.go should be included")
	assert.True(t, foundFiles[filepath.Join("vendorized", "util.go")], "vendorized/util.go should be included")

	// Should be excluded
	assert.False(t, foundFiles[filepath.Join("vendor", "dep.go")], "vendor/dep.go should be excluded")
	assert.False(t, foundFiles[filepath.Join("src", "vendor", "lib.go")], "src/vendor/lib.go should be excluded")
	assert.False(t, foundFiles["main_test.go"], "main_test.go should be excluded")
	assert.False(t, foundFiles["types.pb.go"], "types.pb.go should be excluded")
	assert.False(t, foundFiles["handler.gen.go"], "handler.gen.go should be excluded")
	assert.False(t, foundFiles[filepath.Join("testdata", "helper.go")], "testdata/helper.go should be excluded")
}

// ========== Tests for vc-212 Fixes ==========

// capturePromptSupervisor is a mock that captures the prompt sent to AI
type capturePromptSupervisor struct {
	capturedPrompt string
	response       string
}

func (m *capturePromptSupervisor) CallAI(ctx context.Context, prompt string, operation string, model string, maxTokens int) (string, error) {
	m.capturedPrompt = prompt
	return m.response, nil
}

// Test Fix #8: Outlier limit for AI (max 50)
func TestFileSizeMonitor_OutlierLimit(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "filesize-limit-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	// Create many small files to establish baseline
	for i := 0; i < 100; i++ {
		content := "package main\n\nfunc main() {}\n"
		filename := filepath.Join(tmpDir, fmt.Sprintf("small%d.go", i))
		err = os.WriteFile(filename, []byte(content), 0644)
		require.NoError(t, err)
	}

	// Create 60 large outlier files
	for i := 0; i < 60; i++ {
		large := "package main\n\n"
		for j := 0; j < 500; j++ {
			large += "// Comment line\n"
		}
		filename := filepath.Join(tmpDir, fmt.Sprintf("large%d.go", i))
		err = os.WriteFile(filename, []byte(large), 0644)
		require.NoError(t, err)
	}

	// Use custom mock that captures prompt
	mockAI := &capturePromptSupervisor{
		response: `{
			"problematic_files": [],
			"justified_files": []
		}`,
	}

	monitor, err := NewFileSizeMonitor(tmpDir, mockAI)
	require.NoError(t, err)
	monitor.OutlierThreshold = 2.0 // Ensure we get many outliers

	ctx := context.Background()
	result, err := monitor.Check(ctx, CodebaseContext{})
	require.NoError(t, err)
	assert.NotNil(t, result)

	// Verify that prompt contains at most 50 outliers, not all 60
	// Count occurrences of "large" in the outlier list section
	outlierCount := 0
	for i := 0; i < 60; i++ {
		contains := fmt.Sprintf("large%d.go", i)
		if strings.Contains(mockAI.capturedPrompt, contains) {
			outlierCount++
		}
	}
	assert.LessOrEqual(t, outlierCount, 50, "Should limit outliers sent to AI to 50")
}

// Test Fix #9: Dynamic year in prompt
func TestFileSizeMonitor_DynamicYear(t *testing.T) {
	monitor, err := NewFileSizeMonitor("/tmp", nil)
	require.NoError(t, err)

	outliers := []fileSize{
		{Path: "large.go", Lines: 1000},
	}

	dist := Distribution{
		Mean:   200.0,
		Median: 180.0,
		StdDev: 50.0,
		P95:    350.0,
		P99:    450.0,
		Count:  100,
	}

	prompt := monitor.buildPrompt(outliers, dist)

	// Should NOT contain hardcoded "Late 2025"
	assert.NotContains(t, prompt, "Late 2025")

	// Should contain current year
	currentYear := time.Now().Year()
	yearStr := fmt.Sprintf("%d", currentYear)
	assert.Contains(t, prompt, yearStr)
}

// Test Fix #10: Error message truncation
func TestFileSizeMonitor_ErrorTruncation(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "filesize-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	// Create small files to establish baseline
	for i := 0; i < 10; i++ {
		content := "package main\n\nfunc main() {}\n"
		filename := filepath.Join(tmpDir, fmt.Sprintf("small%d.go", i))
		err = os.WriteFile(filename, []byte(content), 0644)
		require.NoError(t, err)
	}

	// Create large outlier
	large := "package main\n\n"
	for i := 0; i < 500; i++ {
		large += "// Comment\n"
	}
	err = os.WriteFile(filepath.Join(tmpDir, "large.go"), []byte(large), 0644)
	require.NoError(t, err)

	// Mock AI that returns huge invalid JSON (>500 chars)
	hugeResponse := "INVALID JSON: " + strings.Repeat("X", 1000)
	mockAI := &mockSupervisor{
		response: hugeResponse,
	}

	monitor, err := NewFileSizeMonitor(tmpDir, mockAI)
	require.NoError(t, err)
	monitor.OutlierThreshold = 2.0

	ctx := context.Background()
	result, err := monitor.Check(ctx, CodebaseContext{})

	// Should get error due to invalid JSON
	assert.Error(t, err)
	assert.Nil(t, result)

	// Error message should be truncated to ~500 chars, not the full 1000+
	errMsg := err.Error()
	assert.LessOrEqual(t, len(errMsg), 700, "Error message should be truncated")
	assert.Contains(t, errMsg, "truncated", "Error should indicate truncation")
}

// Test Fix #6: Percentile calculation with small datasets
func TestFileSizeMonitor_Percentiles_SmallDatasets(t *testing.T) {
	monitor, err := NewFileSizeMonitor("/tmp", nil)
	require.NoError(t, err)

	tests := []struct {
		name  string
		sizes []fileSize
	}{
		{
			name: "single file",
			sizes: []fileSize{
				{Path: "a.go", Lines: 100},
			},
		},
		{
			name: "two files",
			sizes: []fileSize{
				{Path: "a.go", Lines: 100},
				{Path: "b.go", Lines: 200},
			},
		},
		{
			name: "five files",
			sizes: []fileSize{
				{Path: "a.go", Lines: 10},
				{Path: "b.go", Lines: 20},
				{Path: "c.go", Lines: 30},
				{Path: "d.go", Lines: 40},
				{Path: "e.go", Lines: 50},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Should not panic
			dist := monitor.calculateDistribution(tt.sizes)

			// Verify all fields are set reasonably
			assert.Equal(t, len(tt.sizes), dist.Count)
			assert.GreaterOrEqual(t, dist.P95, dist.Median)
			assert.GreaterOrEqual(t, dist.P99, dist.P95)
			assert.GreaterOrEqual(t, dist.Max, dist.P99)
			assert.LessOrEqual(t, dist.Min, dist.Median)
		})
	}
}
