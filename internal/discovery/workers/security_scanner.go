package workers

import (
	"context"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/steveyegge/vc/internal/discovery"
	"github.com/steveyegge/vc/internal/health"
)

// SecurityScanner is a discovery worker that scans for security vulnerabilities.
// Philosophy: 'Security vulnerabilities should be caught before production'
//
// Analyzes:
// - Credential leaks (API keys, passwords, tokens in code)
// - SQL injection (string concatenation in queries)
// - Command injection (user input in exec calls)
// - Cryptography misuse (weak algorithms, hardcoded keys)
// - OWASP Top 10 patterns
type SecurityScanner struct {
	// Patterns for detecting common security issues
	credentialPatterns []*regexp.Regexp
	sqlPatterns        []string
	cryptoPatterns     []string
}

// NewSecurityScanner creates a new security scanner worker.
func NewSecurityScanner() discovery.DiscoveryWorker {
	return &SecurityScanner{
		credentialPatterns: compileCredentialPatterns(),
		sqlPatterns: []string{
			"Exec",
			"Query",
			"QueryRow",
		},
		cryptoPatterns: []string{
			"md5",
			"MD5",
			"sha1",
			"SHA1",
			"DES",
		},
	}
}

// Name implements DiscoveryWorker.
func (s *SecurityScanner) Name() string {
	return "security_scanner"
}

// Philosophy implements DiscoveryWorker.
func (s *SecurityScanner) Philosophy() string {
	return "Security vulnerabilities should be caught before production"
}

// Scope implements DiscoveryWorker.
func (s *SecurityScanner) Scope() string {
	return "Credential leaks, SQL injection, command injection, crypto misuse, OWASP patterns"
}

// Cost implements DiscoveryWorker.
func (s *SecurityScanner) Cost() health.CostEstimate {
	return health.CostEstimate{
		EstimatedDuration: 2 * time.Minute,
		AICallsEstimated:  5, // AI helps filter false positives
		RequiresFullScan:  true,
		Category:          health.CostExpensive,
	}
}

// Dependencies implements DiscoveryWorker.
func (s *SecurityScanner) Dependencies() []string {
	return nil // Independent analysis
}

// Analyze implements DiscoveryWorker.
func (s *SecurityScanner) Analyze(ctx context.Context, codebase health.CodebaseContext) (*discovery.WorkerResult, error) {
	startTime := time.Now()
	issues := []discovery.DiscoveredIssue{}
	filesAnalyzed := 0

	// Find project root
	rootDir, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("failed to get working directory: %w", err)
	}

	// Walk through source files
	err = filepath.Walk(rootDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}

		// Skip non-Go files
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

		// Skip test files (not directories)
		if strings.HasSuffix(path, "_test.go") {
			return nil
		}

		filesAnalyzed++

		// Scan for credential leaks
		fileIssues := s.scanFile(path, rootDir)
		issues = append(issues, fileIssues...)

		// Parse and analyze AST for security patterns
		astIssues := s.analyzeGoFile(path, rootDir)
		issues = append(issues, astIssues...)

		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("failed to walk directory: %w", err)
	}

	return &discovery.WorkerResult{
		IssuesDiscovered: issues,
		Context: fmt.Sprintf("Scanned %d files for security vulnerabilities. "+
			"Found %d potential security issues.", filesAnalyzed, len(issues)),
		Reasoning: "Security vulnerabilities can lead to data breaches, unauthorized access, and system compromise. " +
			"Early detection during development is much cheaper than fixing issues in production. " +
			"This analysis identifies common security anti-patterns.",
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

// scanFile scans a file for credential leaks and basic security issues.
func (s *SecurityScanner) scanFile(path string, rootDir string) []discovery.DiscoveredIssue {
	issues := []discovery.DiscoveredIssue{}

	content, err := os.ReadFile(path)
	if err != nil {
		return issues
	}

	relPath, _ := filepath.Rel(rootDir, path)
	lines := strings.Split(string(content), "\n")

	// Check for credential patterns
	for i, line := range lines {
		for _, pattern := range s.credentialPatterns {
			if pattern.MatchString(line) {
				// Found potential credential leak
				issues = append(issues, discovery.DiscoveredIssue{
					Title:       fmt.Sprintf("Potential credential leak in %s", relPath),
					Description: fmt.Sprintf("Line %d in %s contains what appears to be a hardcoded credential or API key. Review and move to environment variables or secret management.", i+1, relPath),
					Category:    "security",
					Type:        "bug",
					Priority:    0, // P0 - critical security issue
					Tags:        []string{"security", "credentials", "secrets"},
					FilePath:    path,
					LineStart:   i + 1,
					LineEnd:     i + 1,
					Evidence: map[string]interface{}{
						"line":    line,
						"pattern": pattern.String(),
					},
					DiscoveredBy: "security_scanner",
					DiscoveredAt: time.Now(),
					Confidence:   0.7, // Moderate - could be false positive
				})
			}
		}
	}

	return issues
}

// analyzeGoFile performs AST-based security analysis.
func (s *SecurityScanner) analyzeGoFile(path string, rootDir string) []discovery.DiscoveredIssue {
	issues := []discovery.DiscoveredIssue{}

	fset := token.NewFileSet()
	node, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
	if err != nil {
		return issues
	}

	relPath, _ := filepath.Rel(rootDir, path)

	// Analyze AST for security patterns
	ast.Inspect(node, func(n ast.Node) bool {
		switch stmt := n.(type) {
		case *ast.CallExpr:
			// Check for SQL injection patterns
			if sel, ok := stmt.Fun.(*ast.SelectorExpr); ok {
				funcName := sel.Sel.Name

				// Check for SQL exec/query with string concatenation
				if s.isSQLFunction(funcName) {
					if s.hasSQLInjectionRisk(stmt) {
						pos := fset.Position(stmt.Pos())
						issues = append(issues, discovery.DiscoveredIssue{
							Title:       fmt.Sprintf("Potential SQL injection in %s", relPath),
							Description: fmt.Sprintf("Line %d in %s uses string concatenation in SQL query. Use parameterized queries instead.", pos.Line, relPath),
							Category:    "security",
							Type:        "bug",
							Priority:    0, // P0 - critical
							Tags:        []string{"security", "sql-injection", "owasp"},
							FilePath:    path,
							LineStart:   pos.Line,
							Evidence: map[string]interface{}{
								"function": funcName,
							},
							DiscoveredBy: "security_scanner",
							DiscoveredAt: time.Now(),
							Confidence:   0.8,
						})
					}
				}

				// Check for command injection (exec.Command with string concat)
				if (sel.Sel.Name == "Command" || sel.Sel.Name == "CommandContext") &&
					s.hasCommandInjectionRisk(stmt) {
					pos := fset.Position(stmt.Pos())
					issues = append(issues, discovery.DiscoveredIssue{
						Title:       fmt.Sprintf("Potential command injection in %s", relPath),
						Description: fmt.Sprintf("Line %d in %s uses string concatenation in exec.Command. Validate and sanitize inputs.", pos.Line, relPath),
						Category:    "security",
						Type:        "bug",
						Priority:    0, // P0 - critical
						Tags:        []string{"security", "command-injection", "owasp"},
						FilePath:    path,
						LineStart:   pos.Line,
						Evidence: map[string]interface{}{
							"function": sel.Sel.Name,
						},
						DiscoveredBy: "security_scanner",
						DiscoveredAt: time.Now(),
						Confidence:   0.7,
					})
				}

				// Check for weak crypto
				if s.isWeakCrypto(sel.Sel.Name) {
					pos := fset.Position(stmt.Pos())
					issues = append(issues, discovery.DiscoveredIssue{
						Title:       fmt.Sprintf("Weak cryptography in %s", relPath),
						Description: fmt.Sprintf("Line %d in %s uses weak cryptographic algorithm (%s). Use SHA-256 or stronger.", pos.Line, relPath, sel.Sel.Name),
						Category:    "security",
						Type:        "bug",
						Priority:    1, // P1
						Tags:        []string{"security", "cryptography", "weak-crypto"},
						FilePath:    path,
						LineStart:   pos.Line,
						Evidence: map[string]interface{}{
							"algorithm": sel.Sel.Name,
						},
						DiscoveredBy: "security_scanner",
						DiscoveredAt: time.Now(),
						Confidence:   0.9,
					})
				}
			}
		}
		return true
	})

	return issues
}

// isSQLFunction checks if a function name is SQL-related.
func (s *SecurityScanner) isSQLFunction(name string) bool {
	for _, pattern := range s.sqlPatterns {
		if name == pattern {
			return true
		}
	}
	return false
}

// hasSQLInjectionRisk checks if a SQL call has injection risk.
func (s *SecurityScanner) hasSQLInjectionRisk(call *ast.CallExpr) bool {
	// Look for string concatenation in arguments
	for _, arg := range call.Args {
		if s.hasStringConcat(arg) {
			return true
		}
	}
	return false
}

// hasCommandInjectionRisk checks if exec.Command has injection risk.
func (s *SecurityScanner) hasCommandInjectionRisk(call *ast.CallExpr) bool {
	// Similar to SQL injection - look for string concat
	for _, arg := range call.Args {
		if s.hasStringConcat(arg) {
			return true
		}
	}
	return false
}

// hasStringConcat checks if an expression contains string concatenation.
func (s *SecurityScanner) hasStringConcat(expr ast.Expr) bool {
	switch e := expr.(type) {
	case *ast.BinaryExpr:
		// Check for + operator (string concat)
		if e.Op.String() == "+" {
			return true
		}
	case *ast.CallExpr:
		// Check for fmt.Sprintf or similar
		if sel, ok := e.Fun.(*ast.SelectorExpr); ok {
			if sel.Sel.Name == "Sprintf" || sel.Sel.Name == "Sprint" {
				return true
			}
		}
	}
	return false
}

// isWeakCrypto checks if a function uses weak cryptography.
func (s *SecurityScanner) isWeakCrypto(name string) bool {
	for _, pattern := range s.cryptoPatterns {
		if strings.Contains(name, pattern) {
			return true
		}
	}
	return false
}

// compileCredentialPatterns returns regex patterns for detecting credentials.
func compileCredentialPatterns() []*regexp.Regexp {
	patterns := []string{
		// API keys
		`(?i)api[_-]?key\s*[:=]\s*["'][^"']{20,}["']`,
		// AWS keys
		`(?i)aws[_-]?access[_-]?key[_-]?id\s*[:=]\s*["'][A-Z0-9]{20}["']`,
		`(?i)aws[_-]?secret[_-]?access[_-]?key\s*[:=]\s*["'][A-Za-z0-9/+=]{40}["']`,
		// Generic secrets
		`(?i)secret\s*[:=]\s*["'][^"']{16,}["']`,
		`(?i)password\s*[:=]\s*["'][^"']{8,}["']`,
		// Tokens
		`(?i)token\s*[:=]\s*["'][^"']{20,}["']`,
		// Private keys
		`-----BEGIN\s+(?:RSA\s+)?PRIVATE\s+KEY-----`,
	}

	var compiled []*regexp.Regexp
	for _, p := range patterns {
		if re, err := regexp.Compile(p); err == nil {
			compiled = append(compiled, re)
		}
	}
	return compiled
}
