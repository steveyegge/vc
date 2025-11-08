package sdk_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/steveyegge/vc/internal/discovery/sdk"
	"github.com/steveyegge/vc/internal/discovery/sdk/examples"
	"github.com/steveyegge/vc/internal/health"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestWorkerResultBuilder tests the fluent builder API.
func TestWorkerResultBuilder(t *testing.T) {
	result := sdk.NewWorkerResultBuilder("test_worker").
		WithContext("Test context").
		WithReasoning("Test reasoning").
		AddIssue(sdk.NewIssue().
			WithTitle("Test issue").
			WithPriority(2).
			Build()).
		IncrementFilesAnalyzed().
		IncrementPatternsFound().
		Build()

	assert.Equal(t, "Test context", result.Context)
	assert.Equal(t, "Test reasoning", result.Reasoning)
	assert.Equal(t, 1, len(result.IssuesDiscovered))
	assert.Equal(t, "Test issue", result.IssuesDiscovered[0].Title)
	assert.Equal(t, 1, result.Stats.FilesAnalyzed)
	assert.Equal(t, 1, result.Stats.PatternsFound)
	assert.Equal(t, 1, result.Stats.IssuesFound)
}

// TestIssueBuilder tests the issue builder API.
func TestIssueBuilder(t *testing.T) {
	issue := sdk.NewIssue().
		WithTitle("Test issue").
		WithDescription("Test description").
		WithCategory("test").
		WithPriority(2).
		WithFile("/path/to/file.go", 42, 50).
		WithTag("tag1").
		WithTag("tag2").
		WithEvidence("key", "value").
		WithConfidence(0.8).
		Build()

	assert.Equal(t, "Test issue", issue.Title)
	assert.Equal(t, "Test description", issue.Description)
	assert.Equal(t, "test", issue.Category)
	assert.Equal(t, 2, issue.Priority)
	assert.Equal(t, "/path/to/file.go", issue.FilePath)
	assert.Equal(t, 42, issue.LineStart)
	assert.Equal(t, 50, issue.LineEnd)
	assert.Equal(t, []string{"tag1", "tag2"}, issue.Tags)
	assert.Equal(t, "value", issue.Evidence["key"])
	assert.Equal(t, 0.8, issue.Confidence)
}

// TestFindPattern tests pattern matching functionality.
func TestFindPattern(t *testing.T) {
	// Create a temporary test file
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.go")

	content := `package main

func main() {
	// TODO: implement this
	fmt.Println("hello")
	// FIXME: fix this bug
}
`

	err := os.WriteFile(testFile, []byte(content), 0644)
	require.NoError(t, err)

	// Find TODO comments
	matches, err := sdk.FindPattern(tmpDir, `TODO:.*`, sdk.PatternOptions{
		FilePattern: "*.go",
	})

	require.NoError(t, err)
	assert.Equal(t, 1, len(matches))
	assert.Contains(t, matches[0].Text, "TODO")
	assert.Equal(t, 4, matches[0].Line)
}

// TestFindMissingFiles tests missing file detection.
func TestFindMissingFiles(t *testing.T) {
	tmpDir := t.TempDir()

	// Create README.md but not LICENSE
	readmePath := filepath.Join(tmpDir, "README.md")
	err := os.WriteFile(readmePath, []byte("# Test"), 0644)
	require.NoError(t, err)

	missing := sdk.FindMissingFiles(tmpDir, []string{"README.md", "LICENSE", ".gitignore"})

	assert.Equal(t, 2, len(missing))
	assert.Contains(t, missing, "LICENSE")
	assert.Contains(t, missing, ".gitignore")
	assert.NotContains(t, missing, "README.md")
}

// TestWalkGoFiles tests the Go file walker.
func TestWalkGoFiles(t *testing.T) {
	tmpDir := t.TempDir()

	// Create test files
	testGo := filepath.Join(tmpDir, "test.go")
	testContent := `package main

func TestFunction() {
	// test
}

func AnotherFunction(x int) string {
	return "hello"
}
`
	err := os.WriteFile(testGo, []byte(testContent), 0644)
	require.NoError(t, err)

	// Walk files
	fileCount := 0
	funcCount := 0

	err = sdk.WalkGoFiles(tmpDir, func(file *sdk.GoFile) error {
		fileCount++
		funcCount += len(file.Functions())
		return nil
	})

	require.NoError(t, err)
	assert.Equal(t, 1, fileCount)
	assert.Equal(t, 2, funcCount)
}

// TestGoFileHelpers tests the GoFile helper methods.
func TestGoFileHelpers(t *testing.T) {
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.go")

	content := `package main

import "fmt"

type MyStruct struct {
	Field1 string
	Field2 int
}

func MyFunction(a int, b string) (string, error) {
	return "", nil
}
`

	err := os.WriteFile(testFile, []byte(content), 0644)
	require.NoError(t, err)

	// Parse file
	file, err := sdk.ParseGoFile(testFile, false)
	require.NoError(t, err)

	// Test package
	assert.Equal(t, "main", file.Package)

	// Test imports
	imports := file.Imports()
	assert.Equal(t, 1, len(imports))
	assert.Equal(t, "fmt", imports[0])

	// Test types
	types := file.Types()
	assert.Equal(t, 1, len(types))
	assert.Equal(t, "MyStruct", types[0].Name())
	assert.True(t, types[0].IsStruct())

	fields := types[0].Fields()
	assert.Equal(t, 2, len(fields))
	assert.Equal(t, "Field1", fields[0].Name)
	assert.Equal(t, "string", fields[0].Type)

	// Test functions
	functions := file.Functions()
	assert.Equal(t, 1, len(functions))
	assert.Equal(t, "MyFunction", functions[0].Name())

	params := functions[0].Parameters()
	assert.Equal(t, 2, len(params))
	assert.Equal(t, "a", params[0].Name)
	assert.Equal(t, "int", params[0].Type)

	returns := functions[0].Returns()
	assert.Equal(t, 2, len(returns))
	assert.Equal(t, "string", returns[0])
	assert.Equal(t, "error", returns[1])
}

// TestTODOTrackerWorker tests the TODO tracker example worker.
func TestTODOTrackerWorker(t *testing.T) {
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.go")

	content := `package main

func main() {
	// TODO: implement this feature
	// FIXME: fix this bug
	// HACK: temporary workaround
}
`

	err := os.WriteFile(testFile, []byte(content), 0644)
	require.NoError(t, err)

	worker := examples.NewTODOTrackerWorker()
	codebase := health.CodebaseContext{
		RootPath: tmpDir,
	}

	result, err := worker.Analyze(context.Background(), codebase)
	require.NoError(t, err)

	// Should find TODO, FIXME, and HACK
	assert.GreaterOrEqual(t, len(result.IssuesDiscovered), 3)

	// Check issue properties
	for _, issue := range result.IssuesDiscovered {
		assert.NotEmpty(t, issue.Title)
		assert.NotEmpty(t, issue.Description)
		assert.Equal(t, "technical-debt", issue.Category)
		assert.Equal(t, "todo_tracker", issue.DiscoveredBy)
		assert.Contains(t, issue.Tags, "todo")
	}
}

// TestYAMLWorkerLoading tests YAML worker loading.
func TestYAMLWorkerLoading(t *testing.T) {
	tmpDir := t.TempDir()
	yamlFile := filepath.Join(tmpDir, "test-worker.yaml")

	yamlContent := `name: test_worker
philosophy: "Test philosophy"
scope: "Test scope"

cost:
  duration: "30s"
  ai_calls: 0
  category: cheap

patterns:
  - name: "test_pattern"
    regex: 'TODO:.*'
    file_pattern: "*.go"
    title: "TODO found"
    description: "Test description"
    priority: 3
    category: "test"
    confidence: 0.9

missing_files:
  - path: "README.md"
    title: "Missing README"
    description: "Need README"
    priority: 2
`

	err := os.WriteFile(yamlFile, []byte(yamlContent), 0644)
	require.NoError(t, err)

	// Load worker
	worker, err := sdk.LoadYAMLWorker(yamlFile)
	require.NoError(t, err)

	assert.Equal(t, "test_worker", worker.Name())
	assert.Equal(t, "Test philosophy", worker.Philosophy())
	assert.Equal(t, "Test scope", worker.Scope())
}

// TestYAMLWorkerExecution tests YAML worker execution.
func TestYAMLWorkerExecution(t *testing.T) {
	tmpDir := t.TempDir()

	// Create YAML worker
	yamlFile := filepath.Join(tmpDir, "worker.yaml")
	yamlContent := `name: test_worker
philosophy: "Test"
scope: "Test"

cost:
  duration: "10s"
  ai_calls: 0
  category: cheap

patterns:
  - name: "todo_pattern"
    regex: 'TODO:.*'
    file_pattern: "*.go"
    title: "TODO found"
    description: "Found TODO comment"
    priority: 3
    category: "test"

missing_files:
  - path: "LICENSE"
    title: "Missing LICENSE"
    description: "Need LICENSE file"
    priority: 2
    category: "legal"
`

	err := os.WriteFile(yamlFile, []byte(yamlContent), 0644)
	require.NoError(t, err)

	// Create test code with TODO
	codeFile := filepath.Join(tmpDir, "code.go")
	codeContent := `package main

func main() {
	// TODO: implement this
}
`

	err = os.WriteFile(codeFile, []byte(codeContent), 0644)
	require.NoError(t, err)

	// Load and run worker
	worker, err := sdk.LoadYAMLWorker(yamlFile)
	require.NoError(t, err)

	codebase := health.CodebaseContext{
		RootPath: tmpDir,
	}

	result, err := worker.Analyze(context.Background(), codebase)
	require.NoError(t, err)

	// Should find TODO pattern and missing LICENSE
	assert.GreaterOrEqual(t, len(result.IssuesDiscovered), 2)

	// Check for TODO issue
	foundTODO := false
	foundLicense := false

	for _, issue := range result.IssuesDiscovered {
		if issue.Title == "TODO found" {
			foundTODO = true
			assert.Equal(t, "test", issue.Category)
		}
		if issue.Title == "Missing LICENSE" {
			foundLicense = true
			assert.Equal(t, "legal", issue.Category)
		}
	}

	assert.True(t, foundTODO, "Should find TODO pattern")
	assert.True(t, foundLicense, "Should find missing LICENSE")
}
