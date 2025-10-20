package health

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFileSizeMonitor_Interface(t *testing.T) {
	monitor := NewFileSizeMonitor("/tmp", nil)

	assert.Equal(t, "file_size_monitor", monitor.Name())
	assert.Contains(t, monitor.Philosophy(), "single responsibility")
	assert.Equal(t, ScheduleTimeBased, monitor.Schedule().Type)
	assert.Equal(t, CostModerate, monitor.Cost().Category)
}

func TestFileSizeMonitor_CalculateDistribution(t *testing.T) {
	monitor := NewFileSizeMonitor("/tmp", nil)

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
	monitor := NewFileSizeMonitor("/tmp", nil)

	dist := monitor.calculateDistribution([]fileSize{})

	assert.Equal(t, Distribution{}, dist)
}

func TestFileSizeMonitor_FindOutliers(t *testing.T) {
	monitor := NewFileSizeMonitor("/tmp", nil)
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
	monitor := NewFileSizeMonitor("/tmp", nil)
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
	monitor := NewFileSizeMonitor("/tmp", nil)

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

	monitor := NewFileSizeMonitor(tmpDir, nil)
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

	monitor := NewFileSizeMonitor(tmpDir, nil)
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

	monitor := NewFileSizeMonitor(tmpDir, nil)
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
	monitor := NewFileSizeMonitor("/tmp", nil)

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
	assert.Contains(t, prompt, "Late 2025")
	assert.Contains(t, prompt, "problematic_files")
	assert.Contains(t, prompt, "justified_files")
	assert.Contains(t, prompt, "JSON")
}

func TestFileSizeMonitor_BuildIssues(t *testing.T) {
	monitor := NewFileSizeMonitor("/tmp", nil)

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

	monitor := NewFileSizeMonitor(tmpDir, mockAI)
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
