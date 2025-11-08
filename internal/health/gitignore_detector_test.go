package health

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGitignoreDetector_Interface(t *testing.T) {
	detector, err := NewGitignoreDetector("/tmp", nil)
	require.NoError(t, err)

	assert.Equal(t, "gitignore_detector", detector.Name())
	assert.Contains(t, detector.Philosophy(), "Source control")
	assert.Contains(t, detector.Philosophy(), ".gitignore")
	assert.Equal(t, ScheduleHybrid, detector.Schedule().Type)
	assert.Equal(t, 12*time.Hour, detector.Schedule().MinInterval)
	assert.Equal(t, 24*time.Hour, detector.Schedule().MaxInterval)
	assert.Equal(t, "every_10_issues", detector.Schedule().EventTrigger)
	assert.Equal(t, CostModerate, detector.Cost().Category)
}

func TestNewGitignoreDetector_PathValidation(t *testing.T) {
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
			detector, err := NewGitignoreDetector(tt.rootPath, nil)
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

func TestGitignoreDetector_PatternCategories(t *testing.T) {
	detector, err := NewGitignoreDetector("/tmp", nil)
	require.NoError(t, err)

	// Verify all expected categories exist
	expectedCategories := []string{
		"secrets",
		"build_artifacts",
		"dependencies",
		"editor_files",
		"os_files",
	}

	for _, category := range expectedCategories {
		patterns, exists := detector.GitignorePatterns[category]
		assert.True(t, exists, "Expected category %s to exist", category)
		assert.NotEmpty(t, patterns, "Expected category %s to have patterns", category)
	}

	// Verify critical patterns are present
	secretsPatterns := detector.GitignorePatterns["secrets"]
	assert.Contains(t, secretsPatterns, ".env")
	assert.Contains(t, secretsPatterns, "*.pem")
	assert.Contains(t, secretsPatterns, "*.key")

	osPatterns := detector.GitignorePatterns["os_files"]
	assert.Contains(t, osPatterns, ".DS_Store")
	assert.Contains(t, osPatterns, "Thumbs.db")
}

func TestGitignoreDetector_MatchPattern(t *testing.T) {
	detector, err := NewGitignoreDetector("/tmp", nil)
	require.NoError(t, err)

	tests := []struct {
		name     string
		path     string
		pattern  string
		expected bool
	}{
		// Exact filename matches
		{
			name:     "exact match .DS_Store",
			path:     "foo/bar/.DS_Store",
			pattern:  ".DS_Store",
			expected: true,
		},
		{
			name:     "exact match .env",
			path:     ".env",
			pattern:  ".env",
			expected: true,
		},

		// Wildcard patterns
		{
			name:     "wildcard *.o matches object file",
			path:     "build/main.o",
			pattern:  "*.o",
			expected: true,
		},
		{
			name:     "wildcard *.pem matches certificate",
			path:     "certs/server.pem",
			pattern:  "*.pem",
			expected: true,
		},
		{
			name:     "wildcard .env.* matches .env.local",
			path:     ".env.local",
			pattern:  ".env.*",
			expected: true,
		},

		// Directory patterns
		{
			name:     "directory pattern matches file inside",
			path:     "node_modules/express/index.js",
			pattern:  "node_modules/",
			expected: true,
		},
		{
			name:     "directory pattern matches directory exactly",
			path:     "node_modules",
			pattern:  "node_modules/",
			expected: true,
		},
		{
			name:     "directory pattern does not match different directory",
			path:     "src/components/Button.js",
			pattern:  "node_modules/",
			expected: false,
		},

		// Non-matches
		{
			name:     "*.o does not match .go file",
			path:     "main.go",
			pattern:  "*.o",
			expected: false,
		},
		{
			name:     ".env does not match .env.example",
			path:     ".env.example",
			pattern:  ".env",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := detector.matchPattern(tt.path, tt.pattern)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestGitignoreDetector_FindViolations(t *testing.T) {
	detector, err := NewGitignoreDetector("/tmp", nil)
	require.NoError(t, err)

	trackedFiles := []string{
		"main.go",              // legitimate
		".env",                 // secrets violation (.env pattern)
		"config.pem",           // secrets violation (*.pem pattern)
		"build/main.o",         // build_artifacts violation (*.o pattern)
		"node_modules/pkg/index.js", // dependencies violation (node_modules/ pattern)
		".vscode/settings.json", // editor_files violation (.vscode/ pattern)
		".DS_Store",            // os_files violation (.DS_Store pattern)
		"README.md",            // legitimate
		"docs/example.md",      // legitimate
	}

	violations := detector.findViolations(trackedFiles)

	// Should find violations for: .env, config.pem, build/main.o, node_modules/..., .vscode/..., .DS_Store
	// Should NOT find violations for: main.go, README.md, docs/example.md
	assert.Len(t, violations, 6)

	// Verify categories
	categories := make(map[string]int)
	for _, v := range violations {
		categories[v.Category]++
	}

	assert.Equal(t, 2, categories["secrets"]) // .env, config.pem
	assert.Equal(t, 1, categories["build_artifacts"]) // main.o
	assert.Equal(t, 1, categories["dependencies"]) // node_modules
	assert.Equal(t, 1, categories["editor_files"]) // .vscode
	assert.Equal(t, 1, categories["os_files"]) // .DS_Store
}

func TestGitignoreDetector_GetTrackedFiles_NotGitRepo(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "gitignore-test-*")
	require.NoError(t, err)
	defer func() { _ = os.RemoveAll(tmpDir) }()

	detector, err := NewGitignoreDetector(tmpDir, nil)
	require.NoError(t, err)

	ctx := context.Background()
	files, err := detector.getTrackedFiles(ctx)

	assert.Error(t, err)
	assert.Nil(t, files)
	assert.Contains(t, err.Error(), "not a git repository")
}

func TestGitignoreDetector_GetTrackedFiles_GitRepo(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	tmpDir, err := os.MkdirTemp("", "gitignore-test-*")
	require.NoError(t, err)
	defer func() { _ = os.RemoveAll(tmpDir) }()

	// Initialize git repository
	cmd := exec.Command("git", "init")
	cmd.Dir = tmpDir
	err = cmd.Run()
	require.NoError(t, err)

	// Create and track some files
	testFiles := []string{
		"main.go",
		".env",
		"config.yaml",
	}

	for _, file := range testFiles {
		err = os.WriteFile(filepath.Join(tmpDir, file), []byte("content"), 0644)
		require.NoError(t, err)
	}

	// Add files to git
	cmd = exec.Command("git", "add", ".")
	cmd.Dir = tmpDir
	err = cmd.Run()
	require.NoError(t, err)

	// Now get tracked files
	detector, err := NewGitignoreDetector(tmpDir, nil)
	require.NoError(t, err)

	ctx := context.Background()
	files, err := detector.getTrackedFiles(ctx)

	require.NoError(t, err)
	assert.Len(t, files, 3)
	assert.Contains(t, files, "main.go")
	assert.Contains(t, files, ".env")
	assert.Contains(t, files, "config.yaml")
}

func TestGitignoreDetector_Check_NoSupervisor(t *testing.T) {
	detector, err := NewGitignoreDetector("/tmp", nil)
	require.NoError(t, err)

	ctx := context.Background()
	codebase := CodebaseContext{
		RootPath: "/tmp",
	}

	result, err := detector.Check(ctx, codebase)

	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "AI supervisor is required")
}

func TestGitignoreDetector_Check_BelowThreshold(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	tmpDir, err := os.MkdirTemp("", "gitignore-test-*")
	require.NoError(t, err)
	defer func() { _ = os.RemoveAll(tmpDir) }()

	// Initialize git repository with no violations
	cmd := exec.Command("git", "init")
	cmd.Dir = tmpDir
	err = cmd.Run()
	require.NoError(t, err)

	// Create only legitimate files
	err = os.WriteFile(filepath.Join(tmpDir, "main.go"), []byte("package main"), 0644)
	require.NoError(t, err)

	cmd = exec.Command("git", "add", ".")
	cmd.Dir = tmpDir
	err = cmd.Run()
	require.NoError(t, err)

	// Create detector with mock supervisor (won't be called)
	mockSupervisor := &mockAISupervisor{}
	detector, err := NewGitignoreDetector(tmpDir, mockSupervisor)
	require.NoError(t, err)

	ctx := context.Background()
	codebase := CodebaseContext{
		RootPath: tmpDir,
	}

	result, err := detector.Check(ctx, codebase)

	require.NoError(t, err)
	assert.NotNil(t, result)
	assert.Empty(t, result.IssuesFound)
	assert.Contains(t, result.Context, "no gitignore violations found")
	assert.Equal(t, 0, result.Stats.AICallsMade)
}

func TestGitignoreDetector_Check_WithViolations(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	tmpDir, err := os.MkdirTemp("", "gitignore-test-*")
	require.NoError(t, err)
	defer func() { _ = os.RemoveAll(tmpDir) }()

	// Initialize git repository
	cmd := exec.Command("git", "init")
	cmd.Dir = tmpDir
	err = cmd.Run()
	require.NoError(t, err)

	// Create files with violations
	testFiles := map[string]string{
		"main.go":   "package main",
		".env":      "SECRET=123",
		".DS_Store": "macos",
	}

	for name, content := range testFiles {
		err = os.WriteFile(filepath.Join(tmpDir, name), []byte(content), 0644)
		require.NoError(t, err)
	}

	cmd = exec.Command("git", "add", ".")
	cmd.Dir = tmpDir
	err = cmd.Run()
	require.NoError(t, err)

	// Create detector with mock supervisor
	mockSupervisor := &mockAISupervisor{
		response: `{
			"true_violations": [
				{
					"file": ".env",
					"category": "secrets",
					"reason": "Environment file with secrets",
					"action": "urgent",
					"pattern": ".env"
				},
				{
					"file": ".DS_Store",
					"category": "os_files",
					"reason": "macOS system file",
					"action": "stop_tracking",
					"pattern": ".DS_Store"
				}
			],
			"patterns_to_add": [".env", ".DS_Store"],
			"legitimate_files": [],
			"reasoning": "Found secret file and OS cruft that should not be tracked"
		}`,
	}

	detector, err := NewGitignoreDetector(tmpDir, mockSupervisor)
	require.NoError(t, err)

	ctx := context.Background()
	codebase := CodebaseContext{
		RootPath: tmpDir,
	}

	result, err := detector.Check(ctx, codebase)

	require.NoError(t, err)
	assert.NotNil(t, result)

	// Should have created issues
	assert.NotEmpty(t, result.IssuesFound)
	assert.Equal(t, 1, result.Stats.AICallsMade)

	// Verify issue categories
	categories := make(map[string]bool)
	for _, issue := range result.IssuesFound {
		categories[issue.Category] = true
	}

	// Should have high-severity secrets issue
	assert.True(t, categories["gitignore_secrets"] || categories["gitignore_violations"])
}

func TestGitignoreDetector_BuildIssues(t *testing.T) {
	detector, err := NewGitignoreDetector("/tmp", nil)
	require.NoError(t, err)

	tests := []struct {
		name     string
		eval     *gitignoreEvaluation
		expected int // expected number of issues
	}{
		{
			name: "urgent violations only",
			eval: &gitignoreEvaluation{
				TrueViolations: []violationAction{
					{
						File:     ".env",
						Category: "secrets",
						Action:   "urgent",
					},
				},
				PatternsToAdd: []string{".env"},
			},
			expected: 1, // one high-severity issue
		},
		{
			name: "regular violations only",
			eval: &gitignoreEvaluation{
				TrueViolations: []violationAction{
					{
						File:     ".DS_Store",
						Category: "os_files",
						Action:   "stop_tracking",
					},
				},
				PatternsToAdd: []string{".DS_Store"},
			},
			expected: 1, // one medium-severity issue
		},
		{
			name: "mixed violations",
			eval: &gitignoreEvaluation{
				TrueViolations: []violationAction{
					{
						File:     ".env",
						Category: "secrets",
						Action:   "urgent",
					},
					{
						File:     ".DS_Store",
						Category: "os_files",
						Action:   "stop_tracking",
					},
				},
				PatternsToAdd: []string{".env", ".DS_Store"},
			},
			expected: 2, // one high + one medium severity
		},
		{
			name: "no violations, only patterns",
			eval: &gitignoreEvaluation{
				TrueViolations: []violationAction{},
				PatternsToAdd:  []string{"*.log", "*.tmp"},
			},
			expected: 1, // one low-severity preventive issue
		},
		{
			name: "no issues",
			eval: &gitignoreEvaluation{
				TrueViolations: []violationAction{},
				PatternsToAdd:  []string{},
			},
			expected: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			issues := detector.buildIssues(tt.eval)
			assert.Len(t, issues, tt.expected)

			// Verify severity levels
			if tt.expected > 0 {
				for _, issue := range issues {
					assert.NotEmpty(t, issue.Category)
					assert.NotEmpty(t, issue.Severity)
					assert.NotEmpty(t, issue.Description)
				}
			}
		})
	}
}

func TestGitignoreDetector_ValidateEvaluation(t *testing.T) {
	detector, err := NewGitignoreDetector("/tmp", nil)
	require.NoError(t, err)

	violations := []gitignoreViolation{
		{Path: ".env", Category: "secrets"},
		{Path: ".DS_Store", Category: "os_files"},
	}

	tests := []struct {
		name      string
		eval      *gitignoreEvaluation
		shouldErr bool
		errMsg    string
	}{
		{
			name: "valid evaluation",
			eval: &gitignoreEvaluation{
				TrueViolations: []violationAction{
					{File: ".env"},
				},
				PatternsToAdd: []string{"*.log"},
				LegitimateFiles: []legitimateFile{
					{File: ".DS_Store"},
				},
			},
			shouldErr: false,
		},
		{
			name: "references unknown file in violations",
			eval: &gitignoreEvaluation{
				TrueViolations: []violationAction{
					{File: "unknown.txt"},
				},
			},
			shouldErr: true,
			errMsg:    "unknown file",
		},
		{
			name: "references unknown file in legitimate",
			eval: &gitignoreEvaluation{
				LegitimateFiles: []legitimateFile{
					{File: "unknown.txt"},
				},
			},
			shouldErr: true,
			errMsg:    "unknown file",
		},
		{
			name: "empty pattern",
			eval: &gitignoreEvaluation{
				PatternsToAdd: []string{""},
			},
			shouldErr: true,
			errMsg:    "empty glob pattern",
		},
		{
			name: "invalid glob pattern",
			eval: &gitignoreEvaluation{
				PatternsToAdd: []string{"[invalid"},
			},
			shouldErr: true,
			errMsg:    "invalid glob pattern",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := detector.validateEvaluation(tt.eval, violations)
			if tt.shouldErr {
				assert.Error(t, err)
				if tt.errMsg != "" {
					assert.Contains(t, err.Error(), tt.errMsg)
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestGitignoreDetector_BuildPrompt(t *testing.T) {
	detector, err := NewGitignoreDetector("/tmp", nil)
	require.NoError(t, err)

	violations := []gitignoreViolation{
		{Path: ".env", Category: "secrets", Pattern: ".env"},
		{Path: "build/main.o", Category: "build_artifacts", Pattern: "*.o"},
		{Path: ".DS_Store", Category: "os_files", Pattern: ".DS_Store"},
	}

	prompt := detector.buildPrompt(violations)

	// Verify prompt structure
	assert.Contains(t, prompt, "Gitignore Violation Detection Request")
	assert.Contains(t, prompt, detector.Philosophy())
	assert.Contains(t, prompt, "secrets")
	assert.Contains(t, prompt, "build_artifacts")
	assert.Contains(t, prompt, "os_files")

	// Verify all violations are mentioned
	assert.Contains(t, prompt, ".env")
	assert.Contains(t, prompt, "build/main.o")
	assert.Contains(t, prompt, ".DS_Store")

	// Verify JSON response format is described
	assert.Contains(t, prompt, "Response Format")
	assert.Contains(t, prompt, "true_violations")
	assert.Contains(t, prompt, "patterns_to_add")
	assert.Contains(t, prompt, "legitimate_files")
}
