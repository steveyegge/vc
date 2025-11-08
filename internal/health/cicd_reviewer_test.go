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

func TestCICDReviewer_Interface(t *testing.T) {
	mockAI := &mockSupervisor{}
	reviewer, err := NewCICDReviewer("/tmp", mockAI)
	require.NoError(t, err)

	assert.Equal(t, "cicd_reviewer", reviewer.Name())
	assert.Contains(t, reviewer.Philosophy(), "CI/CD pipelines should be fast")
	assert.Contains(t, reviewer.Philosophy(), "quality gates")
	assert.Equal(t, ScheduleTimeBased, reviewer.Schedule().Type)
	assert.Equal(t, 14*24*time.Hour, reviewer.Schedule().Interval) // Bi-weekly
	assert.Equal(t, CostModerate, reviewer.Cost().Category)
}

func TestNewCICDReviewer_PathValidation(t *testing.T) {
	tests := []struct {
		name     string
		rootPath string
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
			mockAI := &mockSupervisor{}
			reviewer, err := NewCICDReviewer(tt.rootPath, mockAI)
			assert.NoError(t, err)
			assert.NotNil(t, reviewer)
			// Verify path is absolute
			assert.True(t, filepath.IsAbs(reviewer.RootPath))
		})
	}
}

func TestCICDReviewer_ScanCICDFiles(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "cicd-test-*")
	require.NoError(t, err)
	defer func() { _ = os.RemoveAll(tmpDir) }()

	// Create GitHub Actions workflow directory
	workflowDir := filepath.Join(tmpDir, ".github", "workflows")
	err = os.MkdirAll(workflowDir, 0755)
	require.NoError(t, err)

	// Create various CI/CD config files
	cicdFiles := map[string]string{
		".github/workflows/ci.yml":     "name: CI\non: [push]\njobs:\n  test:\n    runs-on: ubuntu-latest\n",
		".github/workflows/release.yml": "name: Release\non: [push]\njobs:\n  release:\n    runs-on: ubuntu-latest\n",
		".gitlab-ci.yml":                "test:\n  script:\n    - go test ./...\n",
		".circleci/config.yml":          "version: 2.1\njobs:\n  test:\n    docker:\n      - image: golang:1.21\n",
		"Jenkinsfile":                   "pipeline {\n  agent any\n  stages {\n    stage('Test') {\n      steps {\n        sh 'go test ./...'\n      }\n    }\n  }\n}\n",
	}

	for path, content := range cicdFiles {
		fullPath := filepath.Join(tmpDir, path)
		dir := filepath.Dir(fullPath)
		err = os.MkdirAll(dir, 0755)
		require.NoError(t, err)
		err = os.WriteFile(fullPath, []byte(content), 0644)
		require.NoError(t, err)
	}

	// Create a non-CI file
	err = os.WriteFile(filepath.Join(tmpDir, "README.md"), []byte("# Test\n"), 0644)
	require.NoError(t, err)

	mockAI := &mockSupervisor{}
	reviewer, err := NewCICDReviewer(tmpDir, mockAI)
	require.NoError(t, err)

	files, err := reviewer.scanCICDFiles(context.Background())
	require.NoError(t, err)

	// Verify we found all CI/CD files
	assert.Len(t, files, 5) // 2 GitHub Actions + 1 GitLab + 1 CircleCI + 1 Jenkins

	platforms := make(map[string]int)
	for _, f := range files {
		platforms[f.Platform]++
	}

	assert.Equal(t, 2, platforms["github"])
	assert.Equal(t, 1, platforms["gitlab"])
	assert.Equal(t, 1, platforms["circleci"])
	assert.Equal(t, 1, platforms["jenkins"])
}

func TestCICDReviewer_ScanCICDFiles_NoWorkflowsDir(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "cicd-noworkflows-test-*")
	require.NoError(t, err)
	defer func() { _ = os.RemoveAll(tmpDir) }()

	// Create .github directory but no workflows subdirectory
	githubDir := filepath.Join(tmpDir, ".github")
	err = os.MkdirAll(githubDir, 0755)
	require.NoError(t, err)

	mockAI := &mockSupervisor{}
	reviewer, err := NewCICDReviewer(tmpDir, mockAI)
	require.NoError(t, err)

	files, err := reviewer.scanCICDFiles(context.Background())
	require.NoError(t, err)

	// Should find no files
	assert.Len(t, files, 0)
}

func TestCICDReviewer_ReadCICDFiles(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "cicd-read-test-*")
	require.NoError(t, err)
	defer func() { _ = os.RemoveAll(tmpDir) }()

	// Create test CI/CD files
	testFiles := map[string]string{
		".gitlab-ci.yml": "test:\n  script:\n    - go test ./...\n",
		"Jenkinsfile":    "pipeline {\n  agent any\n}\n",
	}

	for name, content := range testFiles {
		err := os.WriteFile(filepath.Join(tmpDir, name), []byte(content), 0644)
		require.NoError(t, err)
	}

	// Create a very large file that should be skipped
	largeContent := make([]byte, 250*1024) // 250KB
	err = os.WriteFile(filepath.Join(tmpDir, "large-ci.yml"), largeContent, 0644)
	require.NoError(t, err)

	mockAI := &mockSupervisor{}
	reviewer, err := NewCICDReviewer(tmpDir, mockAI)
	require.NoError(t, err)

	cicdFiles := []cicdFile{
		{Path: ".gitlab-ci.yml", Name: ".gitlab-ci.yml", Platform: "gitlab"},
		{Path: "Jenkinsfile", Name: "Jenkinsfile", Platform: "jenkins"},
		{Path: "large-ci.yml", Name: "large-ci.yml", Platform: "gitlab"},
	}

	contents, errorsIgnored, err := reviewer.readCICDFiles(cicdFiles)
	require.NoError(t, err)

	// Should have skipped the large file
	assert.Len(t, contents, 2)
	// No errors expected (large file is intentionally skipped, not an error)
	assert.Equal(t, 0, errorsIgnored)

	// Verify content was read correctly
	foundGitLab := false
	foundJenkins := false
	for _, content := range contents {
		if content.Name == ".gitlab-ci.yml" {
			foundGitLab = true
			assert.Contains(t, content.Content, "go test")
		}
		if content.Name == "Jenkinsfile" {
			foundJenkins = true
			assert.Contains(t, content.Content, "pipeline")
		}
	}
	assert.True(t, foundGitLab)
	assert.True(t, foundJenkins)
}

func TestCICDReviewer_CalculateQualityGateSeverity(t *testing.T) {
	mockAI := &mockSupervisor{}
	reviewer, err := NewCICDReviewer("/tmp", mockAI)
	require.NoError(t, err)

	tests := []struct {
		name     string
		gates    []missingQualityGate
		expected string
	}{
		{
			name: "one low severity",
			gates: []missingQualityGate{
				{Severity: "low"},
			},
			expected: "low",
		},
		{
			name: "multiple low severity",
			gates: []missingQualityGate{
				{Severity: "low"},
				{Severity: "low"},
				{Severity: "low"},
			},
			expected: "medium",
		},
		{
			name: "one high severity",
			gates: []missingQualityGate{
				{Severity: "high"},
			},
			expected: "high",
		},
		{
			name: "mixed severity with high",
			gates: []missingQualityGate{
				{Severity: "low"},
				{Severity: "medium"},
				{Severity: "high"},
			},
			expected: "high",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			severity := reviewer.calculateQualityGateSeverity(tt.gates)
			assert.Equal(t, tt.expected, severity)
		})
	}
}

func TestCICDReviewer_CalculateSecuritySeverity(t *testing.T) {
	mockAI := &mockSupervisor{}
	reviewer, err := NewCICDReviewer("/tmp", mockAI)
	require.NoError(t, err)

	tests := []struct {
		name     string
		issues   []securityIssue
		expected string
	}{
		{
			name: "one low severity",
			issues: []securityIssue{
				{Severity: "low"},
			},
			expected: "medium", // Security issues are at least medium
		},
		{
			name: "one high severity",
			issues: []securityIssue{
				{Severity: "high"},
			},
			expected: "high",
		},
		{
			name: "mixed severity",
			issues: []securityIssue{
				{Severity: "low"},
				{Severity: "high"},
			},
			expected: "high",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			severity := reviewer.calculateSecuritySeverity(tt.issues)
			assert.Equal(t, tt.expected, severity)
		})
	}
}

func TestCICDReviewer_BuildContext(t *testing.T) {
	mockAI := &mockSupervisor{}
	reviewer, err := NewCICDReviewer("/tmp", mockAI)
	require.NoError(t, err)

	files := []cicdFile{
		{Path: ".github/workflows/ci.yml", Name: "ci.yml", Platform: "github"},
		{Path: ".gitlab-ci.yml", Name: ".gitlab-ci.yml", Platform: "gitlab"},
	}

	eval := &cicdEvaluation{
		MissingQualityGates: []missingQualityGate{
			{Gate: "security-scan"},
		},
		SlowPipelines: []slowPipeline{
			{File: "ci.yml"},
		},
		SecurityIssues: []securityIssue{
			{Issue: "hardcoded secret"},
		},
		DeprecatedActions: []deprecatedAction{
			{Action: "actions/checkout@v2"},
		},
		MissingCaching: []missingCache{
			{CacheType: "dependencies"},
		},
		BestPractices: []cicdBestPractice{
			{Practice: "matrix builds"},
		},
	}

	context := reviewer.buildContext(files, eval)

	assert.Contains(t, context, "2 CI/CD config files")
	assert.Contains(t, context, "1 missing quality gates")
	assert.Contains(t, context, "1 slow pipelines")
	assert.Contains(t, context, "1 security issues")
	assert.Contains(t, context, "1 deprecated actions")
	assert.Contains(t, context, "1 missing caching")
	assert.Contains(t, context, "1 best practices")
}

func TestCICDReviewer_BuildIssues(t *testing.T) {
	mockAI := &mockSupervisor{}
	reviewer, err := NewCICDReviewer("/tmp", mockAI)
	require.NoError(t, err)

	eval := &cicdEvaluation{
		MissingQualityGates: []missingQualityGate{
			{Gate: "tests", Severity: "high"},
			{Gate: "lint", Severity: "medium"},
		},
		SlowPipelines: []slowPipeline{
			{File: "ci.yml", EstimatedSpeedup: "3x"},
		},
		SecurityIssues: []securityIssue{
			{Issue: "hardcoded secret", Severity: "high"},
		},
		DeprecatedActions: []deprecatedAction{
			{Action: "actions/checkout@v2"},
		},
		MissingCaching: []missingCache{
			{CacheType: "dependencies"},
		},
	}

	issues := reviewer.buildIssues(eval)

	// Should have 5 issues (one per category)
	assert.Len(t, issues, 5)

	// Verify issue categories
	categories := make(map[string]int)
	for _, issue := range issues {
		categories[issue.Category]++
	}
	assert.Equal(t, 5, categories["cicd"])

	// Verify descriptions contain counts
	descriptions := make(map[string]bool)
	for _, issue := range issues {
		descriptions[issue.Description] = true
	}

	assert.True(t, descriptions["Add 2 missing quality gates to CI/CD pipeline"])
	assert.True(t, descriptions["Optimize 1 slow CI/CD pipelines"])
	assert.True(t, descriptions["Fix 1 security issues in CI/CD configs"])
	assert.True(t, descriptions["Update 1 deprecated CI/CD actions"])
	assert.True(t, descriptions["Add caching to 1 CI/CD steps"])
}

func TestCICDReviewer_Check_NoCICDFiles(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "cicd-empty-test-*")
	require.NoError(t, err)
	defer func() { _ = os.RemoveAll(tmpDir) }()

	// Create only source files, no CI/CD files
	err = os.WriteFile(filepath.Join(tmpDir, "main.go"), []byte("package main\n"), 0644)
	require.NoError(t, err)

	mockAI := &mockSupervisor{}
	reviewer, err := NewCICDReviewer(tmpDir, mockAI)
	require.NoError(t, err)

	result, err := reviewer.Check(context.Background(), CodebaseContext{})
	require.NoError(t, err)

	assert.Len(t, result.IssuesFound, 0)
	assert.Contains(t, result.Context, "No CI/CD configuration files found")
}

func TestCICDReviewer_Check_NoSupervisor(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "cicd-nosup-test-*")
	require.NoError(t, err)
	defer func() { _ = os.RemoveAll(tmpDir) }()

	// Create a CI/CD file
	err = os.WriteFile(filepath.Join(tmpDir, ".gitlab-ci.yml"), []byte("test:\n  script:\n    - go test\n"), 0644)
	require.NoError(t, err)

	// Constructor now fails fast with nil supervisor (vc-i8vz)
	_, err = NewCICDReviewer(tmpDir, nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "AI supervisor is required")
}

func TestCICDReviewer_FindGlobMatches(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "cicd-glob-test-*")
	require.NoError(t, err)
	defer func() { _ = os.RemoveAll(tmpDir) }()

	// Create workflow directory with multiple files
	workflowDir := filepath.Join(tmpDir, ".github", "workflows")
	err = os.MkdirAll(workflowDir, 0755)
	require.NoError(t, err)

	// Create workflow files
	workflows := []string{"ci.yml", "release.yml", "deploy.yaml", "test.txt"}
	for _, name := range workflows {
		err := os.WriteFile(filepath.Join(workflowDir, name), []byte("test\n"), 0644)
		require.NoError(t, err)
	}

	mockAI := &mockSupervisor{}
	reviewer, err := NewCICDReviewer(tmpDir, mockAI)
	require.NoError(t, err)

	// Test glob pattern for .yml files
	matches, err := reviewer.findGlobMatches(context.Background(), ".github/workflows/*.yml")
	require.NoError(t, err)

	assert.Len(t, matches, 2) // ci.yml and release.yml
	matchNames := make(map[string]bool)
	for _, match := range matches {
		matchNames[filepath.Base(match)] = true
	}
	assert.True(t, matchNames["ci.yml"])
	assert.True(t, matchNames["release.yml"])
	assert.False(t, matchNames["deploy.yaml"]) // .yaml not matched
	assert.False(t, matchNames["test.txt"])    // .txt not matched
}
