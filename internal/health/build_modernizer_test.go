package health

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildModernizer_Interface(t *testing.T) {
	modernizer, err := NewBuildModernizer("/tmp", nil)
	require.NoError(t, err)

	assert.Equal(t, "build_modernizer", modernizer.Name())
	assert.Contains(t, modernizer.Philosophy(), "Build systems should be simple")
	assert.Contains(t, modernizer.Philosophy(), "best practices")
	assert.Equal(t, ScheduleTimeBased, modernizer.Schedule().Type)
	assert.Equal(t, 14*24*time.Hour, modernizer.Schedule().Interval) // Bi-weekly
	assert.Equal(t, CostCheap, modernizer.Cost().Category)
}

func TestNewBuildModernizer_PathValidation(t *testing.T) {
	tests := []struct {
		name      string
		rootPath  string
	}{
		{
			name:     "valid absolute path",
			rootPath: "/tmp",
		},
		{
			name:     "valid relative path",
			rootPath: ".",
		},
		{
			name:     "valid relative path with ..",
			rootPath: "../",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			modernizer, err := NewBuildModernizer(tt.rootPath, nil)
			assert.NoError(t, err)
			assert.NotNil(t, modernizer)
			// Verify path is absolute
			assert.True(t, filepath.IsAbs(modernizer.RootPath))
		})
	}
}

func TestBuildModernizer_ScanBuildFiles(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "build-test-*")
	require.NoError(t, err)
	defer func() { _ = os.RemoveAll(tmpDir) }()

	// Create various build files
	buildFiles := map[string]string{
		"Makefile":            "all:\n\tgo build\n",
		"go.mod":              "module example.com/test\n\ngo 1.21\n",
		"package.json":        `{"name": "test", "version": "1.0.0"}`,
		"Dockerfile":          "FROM golang:1.21\n",
		".tool-versions":      "golang 1.21.0\n",
		"main.go":             "package main\n", // Not a build file
	}

	for name, content := range buildFiles {
		err := os.WriteFile(filepath.Join(tmpDir, name), []byte(content), 0644)
		require.NoError(t, err)
	}

	// Create vendor directory with build files (should be excluded)
	vendorDir := filepath.Join(tmpDir, "vendor")
	err = os.MkdirAll(vendorDir, 0755)
	require.NoError(t, err)
	err = os.WriteFile(filepath.Join(vendorDir, "Makefile"), []byte("vendor makefile\n"), 0644)
	require.NoError(t, err)

	modernizer, err := NewBuildModernizer(tmpDir, nil)
	require.NoError(t, err)

	files, err := modernizer.scanBuildFiles(context.Background())
	require.NoError(t, err)

	// Verify we found the expected build files (not main.go, not vendor/Makefile)
	expectedFiles := []string{"Makefile", "go.mod", "package.json", "Dockerfile", ".tool-versions"}
	assert.Len(t, files, len(expectedFiles))

	foundNames := make(map[string]bool)
	for _, f := range files {
		foundNames[f.Name] = true
	}

	for _, expected := range expectedFiles {
		assert.True(t, foundNames[expected], "Expected to find %s", expected)
	}
}

func TestBuildModernizer_DetectFileType(t *testing.T) {
	modernizer, err := NewBuildModernizer("/tmp", nil)
	require.NoError(t, err)

	tests := []struct {
		fileName     string
		expectedType string
	}{
		{"Makefile", "makefile"},
		{"makefile", "makefile"},
		{"GNUmakefile", "makefile"},
		{"go.mod", "go.mod"},
		{"go.sum", "go.sum"},
		{"package.json", "package.json"},
		{"package-lock.json", "npm-lock"},
		{"yarn.lock", "npm-lock"},
		{"pnpm-lock.yaml", "npm-lock"},
		{"Cargo.toml", "cargo.toml"},
		{"Cargo.lock", "cargo.lock"},
		{"build.gradle", "gradle"},
		{"build.gradle.kts", "gradle"},
		{"pom.xml", "maven"},
		{"requirements.txt", "python"},
		{"setup.py", "python"},
		{"pyproject.toml", "python"},
		{"Dockerfile", "dockerfile"},
		{".tool-versions", "version-file"},
		{".nvmrc", "version-file"},
		{".ruby-version", "version-file"},
	}

	for _, tt := range tests {
		t.Run(tt.fileName, func(t *testing.T) {
			fileType := modernizer.detectFileType(tt.fileName)
			assert.Equal(t, tt.expectedType, fileType)
		})
	}
}

func TestBuildModernizer_ReadBuildFiles(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "build-read-test-*")
	require.NoError(t, err)
	defer func() { _ = os.RemoveAll(tmpDir) }()

	// Create test build files
	testFiles := map[string]string{
		"Makefile": "all:\n\tgo build\n",
		"go.mod":   "module example.com/test\n\ngo 1.21\n",
	}

	for name, content := range testFiles {
		err := os.WriteFile(filepath.Join(tmpDir, name), []byte(content), 0644)
		require.NoError(t, err)
	}

	// Create a very large file that should be skipped
	largeContent := make([]byte, 150*1024) // 150KB
	err = os.WriteFile(filepath.Join(tmpDir, "large.gradle"), largeContent, 0644)
	require.NoError(t, err)

	modernizer, err := NewBuildModernizer(tmpDir, nil)
	require.NoError(t, err)

	buildFiles := []buildFile{
		{Path: "Makefile", Name: "Makefile", FileType: "makefile"},
		{Path: "go.mod", Name: "go.mod", FileType: "go.mod"},
		{Path: "large.gradle", Name: "large.gradle", FileType: "gradle"},
	}

	contents, err := modernizer.readBuildFiles(buildFiles)
	require.NoError(t, err)

	// Should have skipped the large file
	assert.Len(t, contents, 2)

	// Verify content was read correctly
	foundMakefile := false
	foundGoMod := false
	for _, content := range contents {
		if content.Name == "Makefile" {
			foundMakefile = true
			assert.Contains(t, content.Content, "go build")
		}
		if content.Name == "go.mod" {
			foundGoMod = true
			assert.Contains(t, content.Content, "module example.com/test")
		}
	}
	assert.True(t, foundMakefile)
	assert.True(t, foundGoMod)
}

func TestBuildModernizer_CalculateDeprecationSeverity(t *testing.T) {
	modernizer, err := NewBuildModernizer("/tmp", nil)
	require.NoError(t, err)

	tests := []struct {
		name     string
		patterns []deprecatedPattern
		expected string
	}{
		{
			name:     "no patterns",
			patterns: []deprecatedPattern{},
			expected: "low",
		},
		{
			name: "one low impact",
			patterns: []deprecatedPattern{
				{Impact: "low"},
			},
			expected: "low",
		},
		{
			name: "multiple low impact",
			patterns: []deprecatedPattern{
				{Impact: "low"},
				{Impact: "low"},
				{Impact: "low"},
			},
			expected: "medium",
		},
		{
			name: "one high impact",
			patterns: []deprecatedPattern{
				{Impact: "high"},
			},
			expected: "high",
		},
		{
			name: "mixed impacts with high",
			patterns: []deprecatedPattern{
				{Impact: "low"},
				{Impact: "medium"},
				{Impact: "high"},
			},
			expected: "high",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			severity := modernizer.calculateDeprecationSeverity(tt.patterns)
			assert.Equal(t, tt.expected, severity)
		})
	}
}

func TestBuildModernizer_CalculateVersionSeverity(t *testing.T) {
	modernizer, err := NewBuildModernizer("/tmp", nil)
	require.NoError(t, err)

	tests := []struct {
		name     string
		issues   []versionIssue
		expected string
	}{
		{
			name:     "no issues",
			issues:   []versionIssue{},
			expected: "low",
		},
		{
			name: "one outdated",
			issues: []versionIssue{
				{Issue: "outdated"},
			},
			expected: "low",
		},
		{
			name: "multiple outdated",
			issues: []versionIssue{
				{Issue: "outdated"},
				{Issue: "outdated"},
			},
			expected: "medium",
		},
		{
			name: "one eol",
			issues: []versionIssue{
				{Issue: "eol"},
			},
			expected: "high",
		},
		{
			name: "mixed with eol",
			issues: []versionIssue{
				{Issue: "outdated"},
				{Issue: "eol"},
			},
			expected: "high",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			severity := modernizer.calculateVersionSeverity(tt.issues)
			assert.Equal(t, tt.expected, severity)
		})
	}
}

func TestBuildModernizer_BuildContext(t *testing.T) {
	modernizer, err := NewBuildModernizer("/tmp", nil)
	require.NoError(t, err)

	files := []buildFile{
		{Path: "Makefile", Name: "Makefile"},
		{Path: "go.mod", Name: "go.mod"},
	}

	eval := &buildEvaluation{
		DeprecatedPatterns: []deprecatedPattern{
			{File: "Makefile", Pattern: "go get"},
		},
		MissingOptimizations: []missingOptimization{
			{File: "Makefile", Optimization: "caching"},
		},
		VersionIssues: []versionIssue{
			{File: "go.mod", Issue: "eol"},
		},
		BestPractices: []bestPractice{
			{Practice: "using .tool-versions"},
		},
	}

	context := modernizer.buildContext(files, eval)

	assert.Contains(t, context, "2 build files")
	assert.Contains(t, context, "1 deprecated patterns")
	assert.Contains(t, context, "1 missing optimizations")
	assert.Contains(t, context, "1 version issues")
	assert.Contains(t, context, "1 best practices")
}

func TestBuildModernizer_Check_NoBuildFiles(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "build-empty-test-*")
	require.NoError(t, err)
	defer func() { _ = os.RemoveAll(tmpDir) }()

	// Create only source files, no build files
	err = os.WriteFile(filepath.Join(tmpDir, "main.go"), []byte("package main\n"), 0644)
	require.NoError(t, err)

	modernizer, err := NewBuildModernizer(tmpDir, nil)
	require.NoError(t, err)

	result, err := modernizer.Check(context.Background(), CodebaseContext{})
	require.NoError(t, err)

	assert.Len(t, result.IssuesFound, 0)
	assert.Contains(t, result.Context, "No build files found")
}

func TestBuildModernizer_Check_NoSupervisor(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "build-nosup-test-*")
	require.NoError(t, err)
	defer func() { _ = os.RemoveAll(tmpDir) }()

	// Create a build file
	err = os.WriteFile(filepath.Join(tmpDir, "Makefile"), []byte("all:\n\tgo build\n"), 0644)
	require.NoError(t, err)

	modernizer, err := NewBuildModernizer(tmpDir, nil)
	require.NoError(t, err)

	_, err = modernizer.Check(context.Background(), CodebaseContext{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "AI supervisor is required")
}
