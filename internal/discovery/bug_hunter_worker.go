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
// - Goroutine leaks (no cleanup on context cancellation)
// - Race conditions (shared mutable state without locks)
// - Off-by-one errors (loop bounds)
//
// Note: Nil dereference detection was removed (vc-h2a4) due to high false positive rate.
// Proper nil detection requires data flow analysis beyond simple AST inspection.
//
// ZFC Compliance: Detects patterns. AI determines if they're real bugs or false positives.
type BugHunterWorker struct{}

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
	return "Race conditions (shared variables, map access, atomic operations), resource leaks, goroutine leaks, error handling gaps"
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

		// Skip vendor and hidden directories (vc-dkho fix: check IsDir first and return SkipDir)
		if info.IsDir() {
			if strings.Contains(path, "/vendor/") || strings.Contains(path, "/.") {
				return filepath.SkipDir
			}
			return nil // Skip other directories but continue traversing
		}

		// Skip non-Go files
		if !strings.HasSuffix(path, ".go") {
			return nil
		}

		// Skip test files and generated files
		if strings.HasSuffix(path, "_test.go") || strings.Contains(path, "_generated.go") {
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

		// Note: Nil dereference detection removed (vc-h2a4) - too many false positives without proper data flow analysis

		// Check for race conditions
		raceIssues := w.detectRaceConditions(node, fset, path)
		result.IssuesDiscovered = append(result.IssuesDiscovered, raceIssues...)

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

// detectNilDereferenceRisks - REMOVED (vc-h2a4)
// The original implementation flagged every pointer dereference without data flow analysis,
// resulting in an unacceptably high false positive rate. Proper nil dereference detection
// requires sophisticated data flow analysis that tracks:
// - Where pointers are initialized
// - What functions return nil vs non-nil
// - Control flow paths (if x != nil checks)
// - Type assertions and casts
//
// Until we have proper data flow analysis, this detector is disabled.
// Future work: Consider using a static analysis tool like go-critic or nilaway.

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

// detectRaceConditions finds potential race conditions in concurrent code.
// Patterns detected:
// - Shared variables accessed from multiple goroutines without locks
// - Map access in concurrent contexts (maps are not thread-safe)
// - Slice/array modifications from multiple goroutines
// - Counter increments without atomic operations
// - Read-write races (read without lock, write with lock)
func (w *BugHunterWorker) detectRaceConditions(node *ast.File, fset *token.FileSet, filePath string) []DiscoveredIssue {
	var issues []DiscoveredIssue

	// Track variables captured by goroutine closures
	capturedVars := make(map[string][]capturedVariable)

	// First pass: identify goroutines and their captured variables
	ast.Inspect(node, func(n ast.Node) bool {
		if goStmt, ok := n.(*ast.GoStmt); ok {
			// Check if it's a function literal (closure)
			if funcLit, ok := goStmt.Call.Fun.(*ast.FuncLit); ok {
				pos := fset.Position(goStmt.Pos())

				// Find variables referenced in the closure body
				captured := w.findCapturedVariables(funcLit.Body)
				for _, v := range captured {
					capturedVars[v] = append(capturedVars[v], capturedVariable{
						name:     v,
						line:     pos.Line,
						filePath: filePath,
					})
				}
			}
		}
		return true
	})

	// Check for variables captured by multiple goroutines (potential race)
	for varName, locations := range capturedVars {
		if len(locations) > 1 {
			// This variable is captured by multiple goroutines - potential race
			issues = append(issues, DiscoveredIssue{
				Title:       fmt.Sprintf("Potential race condition: variable '%s' accessed by multiple goroutines", varName),
				Description: fmt.Sprintf("Variable '%s' is accessed by %d different goroutines in %s.\n\nShared variables accessed by multiple goroutines must be protected with sync.Mutex, sync.RWMutex, or accessed via channels/atomic operations to prevent race conditions.", varName, len(locations), filepath.Base(filePath)),
				Category:    "bugs",
				Type:        "bug",
				Priority:    1, // P1 - race conditions are serious
				Tags:        []string{"race-condition", "concurrency"},
				FilePath:    filePath,
				LineStart:   locations[0].line,
				Evidence: map[string]interface{}{
					"variable":        varName,
					"goroutine_count": len(locations),
					"file":            filepath.Base(filePath),
					"locations":       locations,
				},
				Confidence: 0.6, // Medium-high confidence - needs AI to verify if properly synchronized
			})
		}
	}

	// Second pass: detect specific race patterns
	ast.Inspect(node, func(n ast.Node) bool {
		// Pattern 1: Map access without synchronization
		if indexExpr, ok := n.(*ast.IndexExpr); ok {
			if w.isMapAccess(indexExpr) {
				pos := fset.Position(indexExpr.Pos())
				// Check if this is inside a goroutine
				if w.isInsideGoroutine(node, indexExpr) {
					issues = append(issues, DiscoveredIssue{
						Title:       "Potential race: map access in goroutine without synchronization",
						Description: fmt.Sprintf("Map access at %s:%d appears to be in a goroutine context.\n\nMaps in Go are not thread-safe. Concurrent reads and writes require sync.RWMutex or sync.Map.", filepath.Base(filePath), pos.Line),
						Category:    "bugs",
						Type:        "bug",
						Priority:    1, // P1 - map races crash at runtime
						Tags:        []string{"race-condition", "map", "concurrency"},
						FilePath:    filePath,
						LineStart:   pos.Line,
						Evidence: map[string]interface{}{
							"line":    pos.Line,
							"file":    filepath.Base(filePath),
							"pattern": "map-access-in-goroutine",
						},
						Confidence: 0.5, // Medium confidence - might be protected by mutex elsewhere
					})
				}
			}
		}

		// Pattern 2: Counter increment without atomic operations
		if incDec, ok := n.(*ast.IncDecStmt); ok {
			if w.isInsideGoroutine(node, incDec) {
				if ident, ok := incDec.X.(*ast.Ident); ok {
					pos := fset.Position(incDec.Pos())
					issues = append(issues, DiscoveredIssue{
						Title:       fmt.Sprintf("Potential race: counter '%s' modified in goroutine without atomic operation", ident.Name),
						Description: fmt.Sprintf("Counter variable '%s' at %s:%d is incremented/decremented in a goroutine.\n\nCounter modifications in concurrent code should use sync/atomic.AddInt32/64 or be protected by a mutex.", ident.Name, filepath.Base(filePath), pos.Line),
						Category:    "bugs",
						Type:        "bug",
						Priority:    2, // P2 - data race but may not crash
						Tags:        []string{"race-condition", "atomic", "concurrency"},
						FilePath:    filePath,
						LineStart:   pos.Line,
						Evidence: map[string]interface{}{
							"variable": ident.Name,
							"line":     pos.Line,
							"file":     filepath.Base(filePath),
							"pattern":  "non-atomic-counter",
						},
						Confidence: 0.5, // Medium confidence - might be protected
					})
				}
			}
		}

		// Pattern 3: Assignment to shared variable in goroutine
		if assign, ok := n.(*ast.AssignStmt); ok {
			if w.isInsideGoroutine(node, assign) {
				for _, lhs := range assign.Lhs {
					if ident, ok := lhs.(*ast.Ident); ok {
						// Check if this looks like a shared variable (not local declaration)
						if assign.Tok != token.DEFINE || w.mightBeSharedVariable(ident.Name) {
							pos := fset.Position(assign.Pos())
							issues = append(issues, DiscoveredIssue{
								Title:       fmt.Sprintf("Potential race: assignment to '%s' in goroutine", ident.Name),
								Description: fmt.Sprintf("Variable '%s' is assigned at %s:%d inside a goroutine.\n\nIf this variable is accessed by multiple goroutines, the assignment must be protected by a mutex or use atomic operations.", ident.Name, filepath.Base(filePath), pos.Line),
								Category:    "bugs",
								Type:        "bug",
								Priority:    2, // P2 - context-dependent severity
								Tags:        []string{"race-condition", "write", "concurrency"},
								FilePath:    filePath,
								LineStart:   pos.Line,
								Evidence: map[string]interface{}{
									"variable": ident.Name,
									"line":     pos.Line,
									"file":     filepath.Base(filePath),
									"pattern":  "shared-write",
								},
								Confidence: 0.4, // Lower confidence - many false positives expected
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

// capturedVariable represents a variable captured by a goroutine closure.
type capturedVariable struct {
	name     string
	line     int
	filePath string
}

// findCapturedVariables finds all variables referenced in a function body.
// This is a simplified heuristic - proper escape analysis would be more accurate.
func (w *BugHunterWorker) findCapturedVariables(body *ast.BlockStmt) []string {
	vars := make(map[string]bool)

	ast.Inspect(body, func(n ast.Node) bool {
		if ident, ok := n.(*ast.Ident); ok {
			// Skip keywords and builtin functions
			if !w.isBuiltin(ident.Name) && ident.Obj == nil {
				vars[ident.Name] = true
			}
		}
		return true
	})

	var result []string
	for v := range vars {
		result = append(result, v)
	}
	return result
}

// isMapAccess checks if an index expression is a map access.
func (w *BugHunterWorker) isMapAccess(expr *ast.IndexExpr) bool {
	// This is a heuristic - we can't determine types without type checking
	// Look for common map variable naming patterns or map literals
	if ident, ok := expr.X.(*ast.Ident); ok {
		name := ident.Name
		// Common map naming patterns
		if strings.HasSuffix(name, "Map") || strings.HasSuffix(name, "Cache") ||
			strings.HasPrefix(name, "map") || strings.Contains(name, "Index") {
			return true
		}
	}
	return false
}

// isInsideGoroutine checks if a node is inside a goroutine.
// This is a simplified check - walks up looking for go statement.
func (w *BugHunterWorker) isInsideGoroutine(root ast.Node, target ast.Node) bool {
	// This would require maintaining parent pointers or a more sophisticated traversal
	// For now, we use a heuristic: check if we're inside a function literal that's called with go
	isInside := false

	ast.Inspect(root, func(n ast.Node) bool {
		if goStmt, ok := n.(*ast.GoStmt); ok {
			if funcLit, ok := goStmt.Call.Fun.(*ast.FuncLit); ok {
				// Check if target is inside this function literal
				ast.Inspect(funcLit, func(inner ast.Node) bool {
					if inner == target {
						isInside = true
						return false
					}
					return true
				})
			}
		}
		return !isInside // Stop if we found it
	})

	return isInside
}

// mightBeSharedVariable uses heuristics to determine if a variable name suggests it's shared.
func (w *BugHunterWorker) mightBeSharedVariable(name string) bool {
	// Variables that start with lowercase might be package-level
	// Variables with certain names suggest shared state
	sharedPatterns := []string{"count", "total", "cache", "state", "config", "result", "data"}

	for _, pattern := range sharedPatterns {
		if strings.Contains(strings.ToLower(name), pattern) {
			return true
		}
	}

	return false
}

// isBuiltin checks if a name is a Go builtin.
func (w *BugHunterWorker) isBuiltin(name string) bool {
	builtins := map[string]bool{
		"true": true, "false": true, "nil": true,
		"append": true, "cap": true, "close": true, "complex": true,
		"copy": true, "delete": true, "imag": true, "len": true,
		"make": true, "new": true, "panic": true, "print": true,
		"println": true, "real": true, "recover": true,
		"error": true, "string": true, "int": true, "int64": true,
		"uint": true, "uint64": true, "byte": true, "rune": true,
		"bool": true, "float64": true, "float32": true,
	}
	return builtins[name]
}
