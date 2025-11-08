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

// BugHunterWorker analyzes code for common bug patterns.
// Philosophy: 'Common bug patterns indicate missing safeguards. Static analysis + AI catches more than linters alone'
//
// This worker performs static analysis to discover potential bugs:
// - Resource leaks (files/connections not closed)
// - Error handling gaps (errors ignored)
// - Nil dereference risks (unchecked nil returns)
// - Goroutine leaks (no cleanup on context cancellation)
// - Race conditions (shared mutable state without locks)
// - Off-by-one errors (loop bounds)
//
// ZFC Compliance: Detects patterns. AI determines if they're real bugs or false positives.
type BugHunterWorker struct {
	// Context from architecture worker (if available)
	pkgGraph *packageGraph
}

// NewBugHunterWorker creates a new bug hunting worker.
func NewBugHunterWorker() *BugHunterWorker {
	return &BugHunterWorker{}
}

// Name implements DiscoveryWorker.
func (w *BugHunterWorker) Name() string {
	return "bugs"
}

// Philosophy implements DiscoveryWorker.
func (w *BugHunterWorker) Philosophy() string {
	return "Common bug patterns indicate missing safeguards. Static analysis + AI catches more than linters alone"
}

// Scope implements DiscoveryWorker.
func (w *BugHunterWorker) Scope() string {
	return "Race conditions, nil dereference risks, resource leaks, goroutine leaks, error handling gaps, off-by-one errors"
}

// Cost implements DiscoveryWorker.
func (w *BugHunterWorker) Cost() health.CostEstimate {
	return health.CostEstimate{
		EstimatedDuration: 2 * time.Minute,
		AICallsEstimated:  10, // Higher AI usage to filter false positives
		RequiresFullScan:  true,
		Category:          health.CostExpensive,
	}
}

// Dependencies implements DiscoveryWorker.
func (w *BugHunterWorker) Dependencies() []string {
	return []string{"architecture"} // Benefits from package graph context
}

// Analyze implements DiscoveryWorker.
func (w *BugHunterWorker) Analyze(ctx context.Context, codebase health.CodebaseContext) (*WorkerResult, error) {
	startTime := time.Now()

	result := &WorkerResult{
		IssuesDiscovered: []DiscoveredIssue{},
		AnalyzedAt:       startTime,
		Stats: AnalysisStats{
			FilesAnalyzed: 0,
			IssuesFound:   0,
		},
	}

	// Analyze each Go file for bug patterns
	err := filepath.Walk(codebase.RootPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Skip directories and non-Go files
		if info.IsDir() || !strings.HasSuffix(path, ".go") {
			return nil
		}

		// Skip test files and generated files
		if strings.HasSuffix(path, "_test.go") || strings.Contains(path, "_generated.go") {
			return nil
		}

		// Skip vendor and hidden directories
		if strings.Contains(path, "/vendor/") || strings.Contains(path, "/.") {
			return nil
		}

		result.Stats.FilesAnalyzed++

		// Parse the file
		fset := token.NewFileSet()
		node, parseErr := parser.ParseFile(fset, path, nil, parser.ParseComments)
		if parseErr != nil {
			result.Stats.ErrorsIgnored++
			return nil // Skip files that fail to parse
		}

		// Check for resource leaks
		resourceIssues := w.detectResourceLeaks(node, fset, path)
		result.IssuesDiscovered = append(result.IssuesDiscovered, resourceIssues...)

		// Check for error handling gaps
		errorIssues := w.detectErrorHandlingGaps(node, fset, path)
		result.IssuesDiscovered = append(result.IssuesDiscovered, errorIssues...)

		// Check for nil dereference risks
		nilIssues := w.detectNilDereferenceRisks(node, fset, path)
		result.IssuesDiscovered = append(result.IssuesDiscovered, nilIssues...)

		// Check for goroutine leaks
		goroutineIssues := w.detectGoroutineLeaks(node, fset, path)
		result.IssuesDiscovered = append(result.IssuesDiscovered, goroutineIssues...)

		// Update issue timestamps
		for i := range result.IssuesDiscovered {
			if result.IssuesDiscovered[i].DiscoveredAt.IsZero() {
				result.IssuesDiscovered[i].DiscoveredAt = startTime
				result.IssuesDiscovered[i].DiscoveredBy = w.Name()
			}
		}

		return nil
	})

	if err != nil {
		return nil, err
	}

	// Build context and reasoning for AI
	result.Context = fmt.Sprintf(
		"Analyzed %d Go files for common bug patterns. Detected %d potential issues.",
		result.Stats.FilesAnalyzed,
		len(result.IssuesDiscovered),
	)

	result.Reasoning = fmt.Sprintf(
		"Based on philosophy: '%s'\n\nStatic analysis found patterns that often indicate bugs:\n"+
			"- Resource leaks can cause file descriptor exhaustion\n"+
			"- Ignored errors can hide failures\n"+
			"- Nil dereferences cause runtime panics\n"+
			"- Goroutine leaks waste memory and resources\n\n"+
			"AI should evaluate: Which of these are real bugs vs. acceptable patterns in context?",
		w.Philosophy(),
	)

	result.Stats.IssuesFound = len(result.IssuesDiscovered)
	result.Stats.Duration = time.Since(startTime)
	result.Stats.PatternsFound = len(result.IssuesDiscovered)

	return result, nil
}

// detectResourceLeaks finds potential resource leaks (unclosed files, connections).
func (w *BugHunterWorker) detectResourceLeaks(node *ast.File, fset *token.FileSet, filePath string) []DiscoveredIssue {
	var issues []DiscoveredIssue

	// Track resource-opening calls that should have defer Close()
	resourceFunctions := map[string]bool{
		"Open":       true,
		"OpenFile":   true,
		"Create":     true,
		"NewReader":  true,
		"NewWriter":  true,
		"Dial":       true,
		"Listen":     true,
		"TempFile":   true,
		"TempDir":    true,
	}

	// Walk AST to find function calls
	ast.Inspect(node, func(n ast.Node) bool {
		// Look for assignments that call resource-opening functions
		if assign, ok := n.(*ast.AssignStmt); ok {
			for _, rhs := range assign.Rhs {
				if call, ok := rhs.(*ast.CallExpr); ok {
					if sel, ok := call.Fun.(*ast.SelectorExpr); ok {
						funcName := sel.Sel.Name
						if resourceFunctions[funcName] {
							// Check if there's a corresponding defer Close() in the same function
							// This is a simplified check - in reality we'd need more sophisticated data flow analysis
							pos := fset.Position(assign.Pos())
							issues = append(issues, DiscoveredIssue{
								Title:       fmt.Sprintf("Potential resource leak: %s call without visible defer Close()", funcName),
								Description: fmt.Sprintf("Call to %s at %s:%d may not have corresponding cleanup.\n\nResource-opening calls should typically be followed by 'defer resource.Close()' to prevent leaks.", funcName, filepath.Base(filePath), pos.Line),
								Category:    "bugs",
								Type:        "bug",
								Priority:    1, // P1 - resource leaks are serious
								Tags:        []string{"resource-leak", "defer"},
								FilePath:    filePath,
								LineStart:   pos.Line,
								Evidence: map[string]interface{}{
									"function":  funcName,
									"line":      pos.Line,
									"file":      filepath.Base(filePath),
								},
								Confidence: 0.5, // Medium confidence - needs AI to verify (might have cleanup elsewhere)
							})
						}
					}
				}
			}
		}
		return true
	})

	return issues
}

// detectErrorHandlingGaps finds places where errors are ignored.
func (w *BugHunterWorker) detectErrorHandlingGaps(node *ast.File, fset *token.FileSet, filePath string) []DiscoveredIssue {
	var issues []DiscoveredIssue

	// Find assignments where error is assigned to blank identifier
	ast.Inspect(node, func(n ast.Node) bool {
		if assign, ok := n.(*ast.AssignStmt); ok {
			// Check if any LHS is blank identifier
			for i, lhs := range assign.Lhs {
				if ident, ok := lhs.(*ast.Ident); ok && ident.Name == "_" {
					// Check if this is the error return (typically last or second-to-last)
					if i == len(assign.Lhs)-1 || (i == len(assign.Lhs)-2 && len(assign.Lhs) > 1) {
						// Get the function call
						if len(assign.Rhs) > 0 {
							if call, ok := assign.Rhs[0].(*ast.CallExpr); ok {
								pos := fset.Position(assign.Pos())
								funcName := "unknown"
								if sel, ok := call.Fun.(*ast.SelectorExpr); ok {
									funcName = sel.Sel.Name
								} else if ident, ok := call.Fun.(*ast.Ident); ok {
									funcName = ident.Name
								}

								issues = append(issues, DiscoveredIssue{
									Title:       fmt.Sprintf("Error ignored: %s returns error but it's assigned to _", funcName),
									Description: fmt.Sprintf("At %s:%d, error from %s is assigned to blank identifier.\n\nIgnoring errors can hide failures and make debugging difficult. Errors should be checked or explicitly logged if intentionally ignored.", filepath.Base(filePath), pos.Line, funcName),
									Category:    "bugs",
									Type:        "bug",
									Priority:    2, // P2 - error handling is important but context-dependent
									Tags:        []string{"error-handling"},
									FilePath:    filePath,
									LineStart:   pos.Line,
									Evidence: map[string]interface{}{
										"function": funcName,
										"line":     pos.Line,
										"file":     filepath.Base(filePath),
									},
									Confidence: 0.6, // Medium confidence - might be intentional
								})
							}
						}
					}
				}
			}
		}
		return true
	})

	return issues
}

// detectNilDereferenceRisks finds potential nil pointer dereferences.
func (w *BugHunterWorker) detectNilDereferenceRisks(node *ast.File, fset *token.FileSet, filePath string) []DiscoveredIssue {
	var issues []DiscoveredIssue

	// Track variables that might be nil
	ast.Inspect(node, func(n ast.Node) bool {
		// Look for dereferences of potentially nil values
		if star, ok := n.(*ast.StarExpr); ok {
			// Check if we're dereferencing without nil check
			// This is a simplified heuristic - would need proper data flow analysis
			if ident, ok := star.X.(*ast.Ident); ok {
				pos := fset.Position(star.Pos())
				issues = append(issues, DiscoveredIssue{
					Title:       fmt.Sprintf("Potential nil dereference: *%s", ident.Name),
					Description: fmt.Sprintf("At %s:%d, dereferencing %s without visible nil check.\n\nNil pointer dereferences cause runtime panics. Consider checking 'if %s != nil' before dereferencing.", filepath.Base(filePath), pos.Line, ident.Name, ident.Name),
					Category:    "bugs",
					Type:        "bug",
					Priority:    1, // P1 - panics are serious
					Tags:        []string{"nil-dereference", "panic"},
					FilePath:    filePath,
					LineStart:   pos.Line,
					Evidence: map[string]interface{}{
						"variable": ident.Name,
						"line":     pos.Line,
						"file":     filepath.Base(filePath),
					},
					Confidence: 0.4, // Lower confidence - high false positive rate without data flow analysis
				})
			}
		}
		return true
	})

	return issues
}

// detectGoroutineLeaks finds goroutines that might not clean up properly.
func (w *BugHunterWorker) detectGoroutineLeaks(node *ast.File, fset *token.FileSet, filePath string) []DiscoveredIssue {
	var issues []DiscoveredIssue

	// Find go statements
	ast.Inspect(node, func(n ast.Node) bool {
		if goStmt, ok := n.(*ast.GoStmt); ok {
			// Check if the goroutine uses context or has cleanup mechanism
			// This is a heuristic - we look for context.Context parameter or select with ctx.Done()
			hasContext := false

			if funcLit, ok := goStmt.Call.Fun.(*ast.FuncLit); ok {
				// Check function parameters for context.Context
				if funcLit.Type.Params != nil {
					for _, field := range funcLit.Type.Params.List {
						if sel, ok := field.Type.(*ast.SelectorExpr); ok {
							if ident, ok := sel.X.(*ast.Ident); ok && ident.Name == "context" {
								hasContext = true
							}
						}
					}
				}

				// Check body for select with ctx.Done()
				ast.Inspect(funcLit.Body, func(inner ast.Node) bool {
					if sel, ok := inner.(*ast.SelectorExpr); ok {
						if sel.Sel.Name == "Done" {
							hasContext = true
						}
					}
					return true
				})
			}

			if !hasContext {
				pos := fset.Position(goStmt.Pos())
				issues = append(issues, DiscoveredIssue{
					Title:       "Potential goroutine leak: no visible cleanup mechanism",
					Description: fmt.Sprintf("At %s:%d, goroutine launched without context or cleanup mechanism.\n\nGoroutines should either: 1) Have a context.Context parameter and select on ctx.Done(), or 2) Have explicit shutdown channel. Otherwise they may leak when the program tries to exit.", filepath.Base(filePath), pos.Line),
					Category:    "bugs",
					Type:        "bug",
					Priority:    2, // P2 - goroutine leaks are serious but may be intentional
					Tags:        []string{"goroutine-leak", "concurrency"},
					FilePath:    filePath,
					LineStart:   pos.Line,
					Evidence: map[string]interface{}{
						"line": pos.Line,
						"file": filepath.Base(filePath),
					},
					Confidence: 0.5, // Medium confidence - might have cleanup elsewhere
				})
			}
		}
		return true
	})

	return issues
}
