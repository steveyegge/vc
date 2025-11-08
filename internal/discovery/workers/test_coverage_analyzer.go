package workers

import (
	"context"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/steveyegge/vc/internal/discovery"
	"github.com/steveyegge/vc/internal/health"
)

// TestCoverageAnalyzer is a discovery worker that analyzes test coverage.
// Philosophy: 'Tests should cover critical paths, edge cases, and error conditions'
//
// Analyzes:
// - Untested packages/files (no *_test.go)
// - Missing edge case tests (nil inputs, boundary conditions)
// - Missing error path tests (error returns not exercised)
// - Integration test gaps (components not tested together)
// - Test quality (weak assertions, no mocking, brittle tests)
type TestCoverageAnalyzer struct{}

// NewTestCoverageAnalyzer creates a new test coverage analyzer worker.
func NewTestCoverageAnalyzer() discovery.DiscoveryWorker {
	return &TestCoverageAnalyzer{}
}

// Name implements DiscoveryWorker.
func (t *TestCoverageAnalyzer) Name() string {
	return "test_coverage_analyzer"
}

// Philosophy implements DiscoveryWorker.
func (t *TestCoverageAnalyzer) Philosophy() string {
	return "Tests should cover critical paths, edge cases, and error conditions"
}

// Scope implements DiscoveryWorker.
func (t *TestCoverageAnalyzer) Scope() string {
	return "Test file existence, test coverage, edge case coverage, error path testing"
}

// Cost implements DiscoveryWorker.
func (t *TestCoverageAnalyzer) Cost() health.CostEstimate {
	return health.CostEstimate{
		EstimatedDuration: 3 * time.Minute,
		AICallsEstimated:  10, // Expensive - needs AI to evaluate test quality
		RequiresFullScan:  true,
		Category:          health.CostExpensive,
	}
}

// Dependencies implements DiscoveryWorker.
func (t *TestCoverageAnalyzer) Dependencies() []string {
	// Would benefit from architecture scanner to identify critical paths
	// but can run standalone
	return nil
}

// Analyze implements DiscoveryWorker.
func (t *TestCoverageAnalyzer) Analyze(ctx context.Context, codebase health.CodebaseContext) (*discovery.WorkerResult, error) {
	startTime := time.Now()
	issues := []discovery.DiscoveredIssue{}
	filesAnalyzed := 0

	// Find project root
	rootDir, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("failed to get working directory: %w", err)
	}

	// Build map of source files to test files
	sourceFiles := make(map[string]string) // source -> test file
	testFiles := make(map[string]bool)
	packageTests := make(map[string]int) // package path -> test count

	// First pass: collect all files
	err = filepath.Walk(rootDir, func(path string, info os.FileInfo, err error) error {
		// Check for context cancellation every file
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		if err != nil {
			return nil
		}

		if !strings.HasSuffix(path, ".go") {
			return nil
		}

		// Skip vendor, hidden directories (vc-dkho fix: check IsDir first)
		if info.IsDir() {
			if strings.Contains(path, "/vendor/") ||
				strings.Contains(path, "/.") ||
				strings.Contains(path, "/node_modules/") {
				return filepath.SkipDir
			}
		}

		relPath, _ := filepath.Rel(rootDir, path)
		filesAnalyzed++

		if strings.HasSuffix(path, "_test.go") {
			testFiles[path] = true
			// Track package test count
			pkg := filepath.Dir(relPath)
			packageTests[pkg]++
		} else {
			// Map source file to expected test file
			testPath := strings.TrimSuffix(path, ".go") + "_test.go"
			sourceFiles[path] = testPath
		}

		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("failed to walk directory: %w", err)
	}

	// Second pass: identify untested files
	for sourcePath, testPath := range sourceFiles {
		relPath, _ := filepath.Rel(rootDir, sourcePath)

		// Skip main packages and generated files
		if strings.HasSuffix(sourcePath, "_generated.go") ||
			strings.HasSuffix(sourcePath, ".pb.go") ||
			strings.Contains(relPath, "/cmd/") {
			continue
		}

		// Check if test file exists
		if !testFiles[testPath] {
			// Check if package has any tests
			pkg := filepath.Dir(relPath)
			testCount := packageTests[pkg]

			priority := 2 // P2 default
			confidence := 0.7

			// Higher priority if package has no tests at all
			if testCount == 0 {
				priority = 1 // P1
				confidence = 0.9
			}

			issues = append(issues, discovery.DiscoveredIssue{
				Title:       fmt.Sprintf("No tests for %s", relPath),
				Description: fmt.Sprintf("File %s has no corresponding test file. Consider adding tests for critical functions.", relPath),
				Category:    "testing",
				Type:        "task",
				Priority:    priority,
				Tags:        []string{"test-coverage", "untested"},
				FilePath:    sourcePath,
				Evidence: map[string]interface{}{
					"expected_test_file": testPath,
					"package_test_count": testCount,
				},
				DiscoveredBy: "test_coverage_analyzer",
				DiscoveredAt: time.Now(),
				Confidence:   confidence,
			})
		}
	}

	// Third pass: analyze existing test quality
	for testPath := range testFiles {
		testIssues := t.analyzeTestFile(testPath, rootDir)
		issues = append(issues, testIssues...)
	}

	// Check for integration tests
	hasIntegrationTests := false
	for path := range testFiles {
		if strings.Contains(path, "integration") || strings.Contains(path, "e2e") {
			hasIntegrationTests = true
			break
		}
	}

	if !hasIntegrationTests && len(sourceFiles) > 10 {
		// Only suggest integration tests for non-trivial projects
		issues = append(issues, discovery.DiscoveredIssue{
			Title:       "Missing integration tests",
			Description: "Project has no integration tests (no files matching *integration* or *e2e*). Consider adding integration tests to verify components work together.",
			Category:    "testing",
			Type:        "task",
			Priority:    2, // P2
			Tags:        []string{"integration-tests", "test-strategy"},
			FilePath:    rootDir,
			Evidence: map[string]interface{}{
				"has_integration_tests": hasIntegrationTests,
				"source_file_count":     len(sourceFiles),
			},
			DiscoveredBy: "test_coverage_analyzer",
			DiscoveredAt: time.Now(),
			Confidence:   0.6,
		})
	}

	return &discovery.WorkerResult{
		IssuesDiscovered: issues,
		Context: fmt.Sprintf("Analyzed %d files (%d tests, %d source). "+
			"Found %d test coverage issues.", filesAnalyzed, len(testFiles), len(sourceFiles), len(issues)),
		Reasoning: "Test coverage ensures code quality and catches regressions. " +
			"Untested code is more likely to contain bugs and break during refactoring. " +
			"This analysis identifies critical gaps in test coverage.",
		AnalyzedAt: time.Now(),
		Stats: discovery.AnalysisStats{
			FilesAnalyzed: filesAnalyzed,
			IssuesFound:   len(issues),
			Duration:      time.Since(startTime),
			AICallsMade:   0, // AI assessment happens later
			PatternsFound: len(issues),
		},
	}, nil
}

// analyzeTestFile checks a single test file for quality issues.
func (t *TestCoverageAnalyzer) analyzeTestFile(path string, rootDir string) []discovery.DiscoveredIssue {
	issues := []discovery.DiscoveredIssue{}

	fset := token.NewFileSet()
	node, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
	if err != nil {
		return issues
	}

	relPath, _ := filepath.Rel(rootDir, path)
	testCount := 0
	tableTestCount := 0
	var currentTestFunc *ast.FuncDecl

	// Analyze test functions in a single pass
	ast.Inspect(node, func(n ast.Node) bool {
		switch node := n.(type) {
		case *ast.FuncDecl:
			// Track if we're in a test function
			if strings.HasPrefix(node.Name.Name, "Test") {
				testCount++
				currentTestFunc = node
			} else {
				currentTestFunc = nil
			}

		case *ast.RangeStmt:
			// Check for table-driven tests only within test functions
			if currentTestFunc != nil {
				// Look for "tests" or "cases" variable
				if ident, ok := node.X.(*ast.Ident); ok {
					if strings.Contains(strings.ToLower(ident.Name), "test") ||
						strings.Contains(strings.ToLower(ident.Name), "case") {
						tableTestCount++
					}
				}
			}
		}

		return true
	})

	// Weak test detection: very few tests
	if testCount > 0 && testCount < 3 {
		issues = append(issues, discovery.DiscoveredIssue{
			Title:       fmt.Sprintf("Test file %s has only %d test(s)", relPath, testCount),
			Description: fmt.Sprintf("Test file %s has only %d test function(s). Consider adding more tests to cover edge cases and error paths.", relPath, testCount),
			Category:    "testing",
			Type:        "task",
			Priority:    3, // P3 - nice to have
			Tags:        []string{"test-quality", "weak-tests"},
			FilePath:    path,
			Evidence: map[string]interface{}{
				"test_count": testCount,
			},
			DiscoveredBy: "test_coverage_analyzer",
			DiscoveredAt: time.Now(),
			Confidence:   0.6,
		})
	}

	// No table-driven tests (for files with multiple tests)
	if testCount >= 3 && tableTestCount == 0 {
		issues = append(issues, discovery.DiscoveredIssue{
			Title:       fmt.Sprintf("Consider table-driven tests in %s", relPath),
			Description: fmt.Sprintf("Test file %s has %d tests but no table-driven tests. Table-driven tests make it easier to add test cases and improve coverage.", relPath, testCount),
			Category:    "testing",
			Type:        "task",
			Priority:    3, // P3 - suggestion
			Tags:        []string{"test-quality", "table-driven"},
			FilePath:    path,
			Evidence: map[string]interface{}{
				"test_count":        testCount,
				"table_test_count":  tableTestCount,
			},
			DiscoveredBy: "test_coverage_analyzer",
			DiscoveredAt: time.Now(),
			Confidence:   0.5, // Lower confidence - this is a style suggestion
		})
	}

	return issues
}
