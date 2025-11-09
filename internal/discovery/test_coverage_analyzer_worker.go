package discovery

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

	"github.com/steveyegge/vc/internal/health"
)

// TestCoverageAnalyzerWorker analyzes test coverage gaps.
// Philosophy: 'Tests are executable documentation and safety nets. Critical code paths deserve test coverage'
//
// This worker performs static analysis to discover test coverage gaps:
// - Exported functions without corresponding tests
// - Critical error paths untested
// - Complex functions without test coverage
// - Packages with no test files
// - Test-to-code ratio analysis
//
// Note: This is static analysis of test file presence, not runtime coverage analysis.
// For runtime coverage, use `go test -cover`.
//
// ZFC Compliance: Identifies missing tests. AI determines which gaps are critical based on code importance.
type TestCoverageAnalyzerWorker struct {
	// No configuration - uses heuristics
}

// NewTestCoverageAnalyzerWorker creates a new test coverage analysis worker.
func NewTestCoverageAnalyzerWorker() *TestCoverageAnalyzerWorker {
	return &TestCoverageAnalyzerWorker{}
}

// Name implements DiscoveryWorker.
func (w *TestCoverageAnalyzerWorker) Name() string {
	return "test_coverage_analyzer"
}

// Philosophy implements DiscoveryWorker.
func (w *TestCoverageAnalyzerWorker) Philosophy() string {
	return "Tests are executable documentation and safety nets. Critical code paths deserve test coverage"
}

// Scope implements DiscoveryWorker.
func (w *TestCoverageAnalyzerWorker) Scope() string {
	return "Test file presence, exported function coverage, package test ratios, critical path coverage"
}

// Cost implements DiscoveryWorker.
func (w *TestCoverageAnalyzerWorker) Cost() health.CostEstimate {
	return health.CostEstimate{
		EstimatedDuration: 1 * time.Minute,
		AICallsEstimated:  8, // Assessment to prioritize coverage gaps
		RequiresFullScan:  true,
		Category:          health.CostModerate,
	}
}

// Dependencies implements DiscoveryWorker.
func (w *TestCoverageAnalyzerWorker) Dependencies() []string {
	return nil // Standalone analysis
}

// Analyze implements DiscoveryWorker.
func (w *TestCoverageAnalyzerWorker) Analyze(ctx context.Context, codebase health.CodebaseContext) (*WorkerResult, error) {
	startTime := time.Now()

	result := &WorkerResult{
		IssuesDiscovered: []DiscoveredIssue{},
		AnalyzedAt:       startTime,
		Stats: AnalysisStats{
			FilesAnalyzed: 0,
			IssuesFound:   0,
		},
	}

	// Build a map of packages and their test status
	pkgMap := make(map[string]*packageTestInfo)

	// First pass: collect all packages and their functions
	err := filepath.Walk(codebase.RootPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Skip vendor and hidden directories
		if info.IsDir() {
			if strings.Contains(path, "/vendor/") || strings.Contains(path, "/.") {
				return filepath.SkipDir
			}
			return nil
		}

		// Only process Go files
		if !strings.HasSuffix(path, ".go") {
			return nil
		}

		// Skip generated files
		if strings.Contains(path, "_generated.go") {
			return nil
		}

		result.Stats.FilesAnalyzed++

		// Determine package path
		pkgDir := filepath.Dir(path)
		isTestFile := strings.HasSuffix(path, "_test.go")

		// Get or create package info
		pkg, exists := pkgMap[pkgDir]
		if !exists {
			pkg = &packageTestInfo{
				path:        pkgDir,
				functions:   []functionInfo{},
				testFiles:   []string{},
				testedFuncs: make(map[string]bool),
			}
			pkgMap[pkgDir] = pkg
		}

		if isTestFile {
			pkg.testFiles = append(pkg.testFiles, path)
			// Parse test file to find what's being tested
			w.parseTestFile(path, pkg)
		} else {
			// Parse source file to find functions
			w.parseSourceFile(path, pkg)
		}

		return nil
	})

	if err != nil {
		return nil, err
	}

	// Second pass: analyze coverage gaps
	for pkgPath, pkg := range pkgMap {
		// Check if package has no tests at all
		if len(pkg.testFiles) == 0 && len(pkg.functions) > 0 {
			result.IssuesDiscovered = append(result.IssuesDiscovered, DiscoveredIssue{
				Title:       fmt.Sprintf("Package %s has no test files", filepath.Base(pkgPath)),
				Description: fmt.Sprintf("Package at %s contains %d functions but no test files.\n\nEvery package with logic should have corresponding tests to validate behavior and prevent regressions.", pkgPath, len(pkg.functions)),
				Category:    "testing",
				Type:        "task",
				Priority:    2, // P2 - testing is important
				Tags:        []string{"test-coverage", "missing-tests"},
				FilePath:    pkgPath,
				Evidence: map[string]interface{}{
					"package":        filepath.Base(pkgPath),
					"function_count": len(pkg.functions),
					"test_count":     0,
				},
				DiscoveredBy: w.Name(),
				DiscoveredAt: startTime,
				Confidence:   0.9, // High confidence
			})
			continue
		}

		// Check for untested exported functions
		for _, fn := range pkg.functions {
			if fn.isExported && !pkg.testedFuncs[fn.name] {
				result.IssuesDiscovered = append(result.IssuesDiscovered, DiscoveredIssue{
					Title:       fmt.Sprintf("Exported function %s has no tests", fn.name),
					Description: fmt.Sprintf("Function %s in %s:%d is exported but has no corresponding test.\n\nExported functions are part of the public API and should have tests to ensure correct behavior.", fn.name, filepath.Base(fn.filePath), fn.line),
					Category:    "testing",
					Type:        "task",
					Priority:    2, // P2 - exported API testing is important
					Tags:        []string{"test-coverage", "exported-api"},
					FilePath:    fn.filePath,
					LineStart:   fn.line,
					Evidence: map[string]interface{}{
						"function": fn.name,
						"file":     filepath.Base(fn.filePath),
						"line":     fn.line,
						"exported": true,
					},
					DiscoveredBy: w.Name(),
					DiscoveredAt: startTime,
					Confidence:   0.7, // Medium-high confidence (might have integration tests)
				})
			}
		}

		// Check for complex untested functions
		for _, fn := range pkg.functions {
			if fn.complexity > 10 && !pkg.testedFuncs[fn.name] {
				result.IssuesDiscovered = append(result.IssuesDiscovered, DiscoveredIssue{
					Title:       fmt.Sprintf("Complex function %s lacks tests (complexity: %d)", fn.name, fn.complexity),
					Description: fmt.Sprintf("Function %s in %s:%d has cyclomatic complexity of %d but no tests.\n\nComplex functions are more likely to have bugs and should be thoroughly tested.", fn.name, filepath.Base(fn.filePath), fn.line, fn.complexity),
					Category:    "testing",
					Type:        "task",
					Priority:    1, // P1 - complex code without tests is risky
					Tags:        []string{"test-coverage", "complexity"},
					FilePath:    fn.filePath,
					LineStart:   fn.line,
					Evidence: map[string]interface{}{
						"function":   fn.name,
						"complexity": fn.complexity,
						"file":       filepath.Base(fn.filePath),
						"line":       fn.line,
					},
					DiscoveredBy: w.Name(),
					DiscoveredAt: startTime,
					Confidence:   0.8, // High confidence for complex code
				})
			}
		}
	}

	// Build context and reasoning for AI
	totalPackages := len(pkgMap)
	packagesWithoutTests := 0
	for _, pkg := range pkgMap {
		if len(pkg.testFiles) == 0 && len(pkg.functions) > 0 {
			packagesWithoutTests++
		}
	}

	result.Context = fmt.Sprintf(
		"Analyzed %d packages with %d total files. Found %d packages without tests, %d coverage gaps.",
		totalPackages,
		result.Stats.FilesAnalyzed,
		packagesWithoutTests,
		len(result.IssuesDiscovered),
	)

	result.Reasoning = fmt.Sprintf(
		"Based on philosophy: '%s'\n\nTest coverage analysis found:\n"+
			"- Packages with no test files\n"+
			"- Exported functions without tests\n"+
			"- Complex functions lacking test coverage\n\n"+
			"AI should evaluate: Which coverage gaps represent real risk vs. acceptable for internal/simple code?",
		w.Philosophy(),
	)

	result.Stats.IssuesFound = len(result.IssuesDiscovered)
	result.Stats.Duration = time.Since(startTime)
	result.Stats.PatternsFound = len(result.IssuesDiscovered)

	return result, nil
}

// packageTestInfo tracks test coverage for a package.
type packageTestInfo struct {
	path        string
	functions   []functionInfo
	testFiles   []string
	testedFuncs map[string]bool // Functions that have tests
}

// functionInfo contains metadata about a function.
type functionInfo struct {
	name       string
	filePath   string
	line       int
	isExported bool
	complexity int
}

// parseSourceFile extracts functions from a source file.
func (w *TestCoverageAnalyzerWorker) parseSourceFile(path string, pkg *packageTestInfo) {
	fset := token.NewFileSet()
	node, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		return // Skip files that fail to parse
	}

	ast.Inspect(node, func(n ast.Node) bool {
		if funcDecl, ok := n.(*ast.FuncDecl); ok {
			// Skip methods on test types (like Test*, Benchmark*)
			if funcDecl.Recv != nil {
				return true
			}

			pos := fset.Position(funcDecl.Pos())
			complexity := w.calculateComplexity(funcDecl)

			pkg.functions = append(pkg.functions, functionInfo{
				name:       funcDecl.Name.Name,
				filePath:   path,
				line:       pos.Line,
				isExported: funcDecl.Name.IsExported(),
				complexity: complexity,
			})
		}
		return true
	})
}

// parseTestFile identifies which functions are tested.
func (w *TestCoverageAnalyzerWorker) parseTestFile(path string, pkg *packageTestInfo) {
	fset := token.NewFileSet()
	node, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		return
	}

	ast.Inspect(node, func(n ast.Node) bool {
		// Look for Test* and Benchmark* functions
		if funcDecl, ok := n.(*ast.FuncDecl); ok {
			funcName := funcDecl.Name.Name

			// Extract tested function name from test name
			// TestFoo -> Foo
			// TestFoo_EdgeCase -> Foo
			// BenchmarkFoo -> Foo
			if strings.HasPrefix(funcName, "Test") {
				testedFunc := strings.TrimPrefix(funcName, "Test")
				// Remove test case suffix (everything after first underscore)
				if idx := strings.Index(testedFunc, "_"); idx != -1 {
					testedFunc = testedFunc[:idx]
				}
				pkg.testedFuncs[testedFunc] = true
			} else if strings.HasPrefix(funcName, "Benchmark") {
				testedFunc := strings.TrimPrefix(funcName, "Benchmark")
				if idx := strings.Index(testedFunc, "_"); idx != -1 {
					testedFunc = testedFunc[:idx]
				}
				pkg.testedFuncs[testedFunc] = true
			}
		}
		return true
	})
}

// calculateComplexity computes cyclomatic complexity.
func (w *TestCoverageAnalyzerWorker) calculateComplexity(funcDecl *ast.FuncDecl) int {
	if funcDecl.Body == nil {
		return 0
	}

	complexity := 1

	ast.Inspect(funcDecl.Body, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.IfStmt:
			complexity++
		case *ast.ForStmt:
			complexity++
		case *ast.RangeStmt:
			complexity++
		case *ast.SwitchStmt:
			complexity++
		case *ast.CaseClause:
			complexity++
		case *ast.BinaryExpr:
			if n.Op == token.LAND || n.Op == token.LOR {
				complexity++
			}
		}
		return true
	})

	return complexity
}
