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

// SecurityScannerWorker analyzes code for security vulnerabilities.
// Philosophy: 'Security vulnerabilities create exploitable attack surface. Defense in depth requires multiple layers of validation'
//
// This worker performs static analysis to discover security issues:
// - SQL injection risks (string concatenation in queries)
// - Command injection risks (unsanitized input to exec)
// - Path traversal vulnerabilities (unchecked file paths)
// - Hardcoded secrets (API keys, passwords in code)
// - Insecure crypto usage (weak algorithms, hardcoded keys)
// - Missing input validation
//
// Note: This is heuristic-based static analysis, not comprehensive security audit.
// For production systems, use dedicated security scanners like gosec or semgrep.
//
// ZFC Compliance: Detects security patterns. AI determines severity based on context and attack surface.
type SecurityScannerWorker struct {
	// No configuration - uses pattern matching
}

// NewSecurityScannerWorker creates a new security scanning worker.
func NewSecurityScannerWorker() *SecurityScannerWorker {
	return &SecurityScannerWorker{}
}

// Name implements DiscoveryWorker.
func (w *SecurityScannerWorker) Name() string {
	return "security_scanner"
}

// Philosophy implements DiscoveryWorker.
func (w *SecurityScannerWorker) Philosophy() string {
	return "Security vulnerabilities create exploitable attack surface. Defense in depth requires multiple layers of validation"
}

// Scope implements DiscoveryWorker.
func (w *SecurityScannerWorker) Scope() string {
	return "SQL injection, command injection, path traversal, hardcoded secrets, insecure crypto, input validation"
}

// Cost implements DiscoveryWorker.
func (w *SecurityScannerWorker) Cost() health.CostEstimate {
	return health.CostEstimate{
		EstimatedDuration: 90 * time.Second,
		AICallsEstimated:  12, // Higher AI usage to filter false positives
		RequiresFullScan:  true,
		Category:          health.CostExpensive,
	}
}

// Dependencies implements DiscoveryWorker.
func (w *SecurityScannerWorker) Dependencies() []string {
	return nil // Standalone analysis
}

// Analyze implements DiscoveryWorker.
func (w *SecurityScannerWorker) Analyze(ctx context.Context, codebase health.CodebaseContext) (*WorkerResult, error) {
	startTime := time.Now()

	result := &WorkerResult{
		IssuesDiscovered: []DiscoveredIssue{},
		AnalyzedAt:       startTime,
		Stats: AnalysisStats{
			FilesAnalyzed: 0,
			IssuesFound:   0,
		},
	}

	// Analyze each Go file for security issues
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
			return nil
		}

		// Check for SQL injection risks
		sqlIssues := w.detectSQLInjection(node, fset, path)
		result.IssuesDiscovered = append(result.IssuesDiscovered, sqlIssues...)

		// Check for command injection risks
		cmdIssues := w.detectCommandInjection(node, fset, path)
		result.IssuesDiscovered = append(result.IssuesDiscovered, cmdIssues...)

		// Check for path traversal vulnerabilities
		pathIssues := w.detectPathTraversal(node, fset, path)
		result.IssuesDiscovered = append(result.IssuesDiscovered, pathIssues...)

		// Check for hardcoded secrets
		secretIssues := w.detectHardcodedSecrets(node, fset, path)
		result.IssuesDiscovered = append(result.IssuesDiscovered, secretIssues...)

		// Check for insecure crypto
		cryptoIssues := w.detectInsecureCrypto(node, fset, path)
		result.IssuesDiscovered = append(result.IssuesDiscovered, cryptoIssues...)

		return nil
	})

	if err != nil {
		return nil, err
	}

	// Set metadata on discovered issues
	for i := range result.IssuesDiscovered {
		if result.IssuesDiscovered[i].DiscoveredAt.IsZero() {
			result.IssuesDiscovered[i].DiscoveredAt = startTime
			result.IssuesDiscovered[i].DiscoveredBy = w.Name()
		}
	}

	// Build context and reasoning for AI
	result.Context = fmt.Sprintf(
		"Analyzed %d Go files for security vulnerabilities. Found %d potential security issues.",
		result.Stats.FilesAnalyzed,
		len(result.IssuesDiscovered),
	)

	result.Reasoning = fmt.Sprintf(
		"Based on philosophy: '%s'\n\nSecurity analysis found potential vulnerabilities:\n"+
			"- SQL injection risks from string concatenation\n"+
			"- Command injection from unsanitized input\n"+
			"- Path traversal from unchecked file paths\n"+
			"- Hardcoded secrets in source code\n"+
			"- Insecure cryptographic practices\n\n"+
			"AI should evaluate: Which issues represent real security risks vs. safe patterns in context?",
		w.Philosophy(),
	)

	result.Stats.IssuesFound = len(result.IssuesDiscovered)
	result.Stats.Duration = time.Since(startTime)
	result.Stats.PatternsFound = len(result.IssuesDiscovered)

	return result, nil
}

// detectSQLInjection finds potential SQL injection vulnerabilities.
func (w *SecurityScannerWorker) detectSQLInjection(node *ast.File, fset *token.FileSet, filePath string) []DiscoveredIssue {
	var issues []DiscoveredIssue

	// Look for SQL query construction using string concatenation or fmt.Sprintf
	ast.Inspect(node, func(n ast.Node) bool {
		// Check for binary expressions with SQL keywords
		if binExpr, ok := n.(*ast.BinaryExpr); ok {
			if binExpr.Op == token.ADD {
				// Check if either side contains SQL keywords
				leftStr := w.exprToString(binExpr.X)
				rightStr := w.exprToString(binExpr.Y)

				if w.containsSQLKeywords(leftStr) || w.containsSQLKeywords(rightStr) {
					pos := fset.Position(binExpr.Pos())
					issues = append(issues, DiscoveredIssue{
						Title:       "Potential SQL injection risk: query built with string concatenation",
						Description: fmt.Sprintf("At %s:%d, SQL query appears to be constructed using string concatenation.\n\nString concatenation for SQL queries can lead to SQL injection. Use parameterized queries instead (e.g., db.Query with placeholders).", filepath.Base(filePath), pos.Line),
						Category:    "security",
						Type:        "bug",
						Priority:    0, // P0 - SQL injection is critical
						Tags:        []string{"sql-injection", "security"},
						FilePath:    filePath,
						LineStart:   pos.Line,
						Evidence: map[string]interface{}{
							"line":     pos.Line,
							"file":     filepath.Base(filePath),
							"pattern":  "string concatenation",
						},
						Confidence: 0.6, // Medium confidence - might be safe constants
					})
				}
			}
		}

		// Check for fmt.Sprintf with SQL queries
		if callExpr, ok := n.(*ast.CallExpr); ok {
			if sel, ok := callExpr.Fun.(*ast.SelectorExpr); ok {
				if ident, ok := sel.X.(*ast.Ident); ok && ident.Name == "fmt" && sel.Sel.Name == "Sprintf" {
					// Check if the format string contains SQL keywords
					if len(callExpr.Args) > 0 {
						if lit, ok := callExpr.Args[0].(*ast.BasicLit); ok {
							if lit.Kind == token.STRING && w.containsSQLKeywords(lit.Value) {
								pos := fset.Position(callExpr.Pos())
								issues = append(issues, DiscoveredIssue{
									Title:       "Potential SQL injection risk: query built with fmt.Sprintf",
									Description: fmt.Sprintf("At %s:%d, SQL query appears to be constructed using fmt.Sprintf.\n\nBuilding SQL queries with fmt.Sprintf can lead to SQL injection. Use parameterized queries with placeholders instead.", filepath.Base(filePath), pos.Line),
									Category:    "security",
									Type:        "bug",
									Priority:    0, // P0 - SQL injection is critical
									Tags:        []string{"sql-injection", "security"},
									FilePath:    filePath,
									LineStart:   pos.Line,
									Evidence: map[string]interface{}{
										"line":    pos.Line,
										"file":    filepath.Base(filePath),
										"pattern": "fmt.Sprintf",
									},
									Confidence: 0.7, // Higher confidence for fmt.Sprintf
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

// detectCommandInjection finds potential command injection vulnerabilities.
func (w *SecurityScannerWorker) detectCommandInjection(node *ast.File, fset *token.FileSet, filePath string) []DiscoveredIssue {
	var issues []DiscoveredIssue

	// Look for exec.Command or similar with potentially unsafe input
	ast.Inspect(node, func(n ast.Node) bool {
		if callExpr, ok := n.(*ast.CallExpr); ok {
			if sel, ok := callExpr.Fun.(*ast.SelectorExpr); ok {
				// Check for exec.Command, os.StartProcess, etc.
				if ident, ok := sel.X.(*ast.Ident); ok {
					isCommandExec := (ident.Name == "exec" && sel.Sel.Name == "Command") ||
						(ident.Name == "os" && (sel.Sel.Name == "StartProcess" || sel.Sel.Name == "Exec"))

					if isCommandExec {
						// Check if arguments come from variables (potential user input)
						hasVariableArgs := false
						for _, arg := range callExpr.Args {
							if _, isLiteral := arg.(*ast.BasicLit); !isLiteral {
								hasVariableArgs = true
								break
							}
						}

						if hasVariableArgs {
							pos := fset.Position(callExpr.Pos())
							issues = append(issues, DiscoveredIssue{
								Title:       "Potential command injection risk: exec with variable arguments",
								Description: fmt.Sprintf("At %s:%d, command execution uses variable arguments that may contain unsanitized input.\n\nCommand injection can occur if user input is passed to exec without validation. Sanitize inputs and avoid shell interpretation.", filepath.Base(filePath), pos.Line),
								Category:    "security",
								Type:        "bug",
								Priority:    0, // P0 - command injection is critical
								Tags:        []string{"command-injection", "security"},
								FilePath:    filePath,
								LineStart:   pos.Line,
								Evidence: map[string]interface{}{
									"line":    pos.Line,
									"file":    filepath.Base(filePath),
									"pattern": "variable args",
								},
								Confidence: 0.5, // Medium confidence - might be safe
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

// detectPathTraversal finds potential path traversal vulnerabilities.
func (w *SecurityScannerWorker) detectPathTraversal(node *ast.File, fset *token.FileSet, filePath string) []DiscoveredIssue {
	var issues []DiscoveredIssue

	// Look for file operations with potentially unsafe paths
	ast.Inspect(node, func(n ast.Node) bool {
		if callExpr, ok := n.(*ast.CallExpr); ok {
			if sel, ok := callExpr.Fun.(*ast.SelectorExpr); ok {
				// Check for os.Open, ioutil.ReadFile, etc.
				if ident, ok := sel.X.(*ast.Ident); ok {
					isFileOp := (ident.Name == "os" && (sel.Sel.Name == "Open" || sel.Sel.Name == "OpenFile" || sel.Sel.Name == "Create")) ||
						(ident.Name == "ioutil" && sel.Sel.Name == "ReadFile") ||
						(ident.Name == "filepath" && sel.Sel.Name == "Join")

					if isFileOp && len(callExpr.Args) > 0 {
						// Check if path comes from variable (potential user input)
						if _, isLiteral := callExpr.Args[0].(*ast.BasicLit); !isLiteral {
							// Check if filepath.Clean is used
							hasClean := w.hasFilepathClean(callExpr.Args[0])

							if !hasClean {
								pos := fset.Position(callExpr.Pos())
								issues = append(issues, DiscoveredIssue{
									Title:       "Potential path traversal risk: file operation with unchecked path",
									Description: fmt.Sprintf("At %s:%d, file operation uses a variable path without filepath.Clean.\n\nPath traversal vulnerabilities allow attackers to access files outside intended directories using '../' sequences. Always use filepath.Clean and validate paths.", filepath.Base(filePath), pos.Line),
									Category:    "security",
									Type:        "bug",
									Priority:    1, // P1 - path traversal is serious
									Tags:        []string{"path-traversal", "security"},
									FilePath:    filePath,
									LineStart:   pos.Line,
									Evidence: map[string]interface{}{
										"line":      pos.Line,
										"file":      filepath.Base(filePath),
										"operation": sel.Sel.Name,
									},
									Confidence: 0.5, // Medium confidence - might be internal paths
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

// detectHardcodedSecrets finds potential hardcoded secrets in the code.
func (w *SecurityScannerWorker) detectHardcodedSecrets(node *ast.File, fset *token.FileSet, filePath string) []DiscoveredIssue {
	var issues []DiscoveredIssue

	// Look for string literals that might be secrets
	// We use variable name heuristics rather than regex patterns for simplicity
	ast.Inspect(node, func(n ast.Node) bool {
		// Check variable assignments
		if assign, ok := n.(*ast.AssignStmt); ok {
			for i, lhs := range assign.Lhs {
				if ident, ok := lhs.(*ast.Ident); ok {
					varName := strings.ToLower(ident.Name)

					// Check if variable name suggests a secret
					if strings.Contains(varName, "password") ||
						strings.Contains(varName, "secret") ||
						strings.Contains(varName, "apikey") ||
						strings.Contains(varName, "token") {

						// Check if assigned a non-empty string literal
						if i < len(assign.Rhs) {
							if lit, ok := assign.Rhs[i].(*ast.BasicLit); ok {
								if lit.Kind == token.STRING && len(lit.Value) > 10 {
									pos := fset.Position(assign.Pos())
									issues = append(issues, DiscoveredIssue{
										Title:       fmt.Sprintf("Potential hardcoded secret: %s assigned string literal", ident.Name),
										Description: fmt.Sprintf("At %s:%d, variable %s that appears to be a secret is assigned a string literal.\n\nHardcoded secrets in source code are a security risk. Use environment variables or secret management systems instead.", filepath.Base(filePath), pos.Line, ident.Name),
										Category:    "security",
										Type:        "bug",
										Priority:    0, // P0 - hardcoded secrets are critical
										Tags:        []string{"hardcoded-secret", "security"},
										FilePath:    filePath,
										LineStart:   pos.Line,
										Evidence: map[string]interface{}{
											"variable": ident.Name,
											"line":     pos.Line,
											"file":     filepath.Base(filePath),
										},
										Confidence: 0.6, // Medium confidence - might be test data
									})
								}
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

// detectInsecureCrypto finds insecure cryptographic practices.
func (w *SecurityScannerWorker) detectInsecureCrypto(node *ast.File, fset *token.FileSet, filePath string) []DiscoveredIssue {
	var issues []DiscoveredIssue

	// Look for usage of weak crypto algorithms
	ast.Inspect(node, func(n ast.Node) bool {
		if callExpr, ok := n.(*ast.CallExpr); ok {
			if sel, ok := callExpr.Fun.(*ast.SelectorExpr); ok {
				// Check for weak hash algorithms (MD5, SHA1)
				if ident, ok := sel.X.(*ast.Ident); ok {
					if (ident.Name == "md5" && sel.Sel.Name == "New") ||
						(ident.Name == "sha1" && sel.Sel.Name == "New") {
						pos := fset.Position(callExpr.Pos())
						issues = append(issues, DiscoveredIssue{
							Title:       fmt.Sprintf("Insecure cryptographic algorithm: %s", ident.Name),
							Description: fmt.Sprintf("At %s:%d, weak hash algorithm %s is used.\n\nMD5 and SHA1 are cryptographically broken. Use SHA256 or SHA3 for secure hashing.", filepath.Base(filePath), pos.Line, strings.ToUpper(ident.Name)),
							Category:    "security",
							Type:        "bug",
							Priority:    1, // P1 - weak crypto is serious
							Tags:        []string{"weak-crypto", "security"},
							FilePath:    filePath,
							LineStart:   pos.Line,
							Evidence: map[string]interface{}{
								"algorithm": ident.Name,
								"line":      pos.Line,
								"file":      filepath.Base(filePath),
							},
							Confidence: 0.8, // High confidence
						})
					}
				}
			}
		}
		return true
	})

	return issues
}

// containsSQLKeywords checks if a string contains SQL keywords.
func (w *SecurityScannerWorker) containsSQLKeywords(s string) bool {
	upper := strings.ToUpper(s)
	keywords := []string{"SELECT", "INSERT", "UPDATE", "DELETE", "FROM", "WHERE", "JOIN"}

	for _, kw := range keywords {
		if strings.Contains(upper, kw) {
			return true
		}
	}
	return false
}

// exprToString attempts to convert an expression to a string representation.
func (w *SecurityScannerWorker) exprToString(expr ast.Expr) string {
	if lit, ok := expr.(*ast.BasicLit); ok {
		return lit.Value
	}
	return ""
}

// hasFilepathClean checks if an expression uses filepath.Clean.
func (w *SecurityScannerWorker) hasFilepathClean(expr ast.Expr) bool {
	// Check if expression is filepath.Clean(...)
	if callExpr, ok := expr.(*ast.CallExpr); ok {
		if sel, ok := callExpr.Fun.(*ast.SelectorExpr); ok {
			if ident, ok := sel.X.(*ast.Ident); ok {
				if ident.Name == "filepath" && sel.Sel.Name == "Clean" {
					return true
				}
			}
		}
	}
	return false
}
