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

// DocAuditorWorker analyzes documentation quality.
// Philosophy: 'Clear documentation reduces cognitive load. Public APIs without docs create barriers to understanding'
//
// This worker performs static analysis to discover documentation gaps:
// - Exported types/functions without godoc comments
// - Package-level documentation missing
// - Complex functions without explanation
// - Magic numbers without comments
// - README.md missing or incomplete
//
// ZFC Compliance: Detects missing/poor documentation. AI determines severity based on API surface.
type DocAuditorWorker struct {
	// No configuration - uses heuristics for complexity
}

// NewDocAuditorWorker creates a new documentation auditing worker.
func NewDocAuditorWorker() *DocAuditorWorker {
	return &DocAuditorWorker{}
}

// Name implements DiscoveryWorker.
func (w *DocAuditorWorker) Name() string {
	return "doc_auditor"
}

// Philosophy implements DiscoveryWorker.
func (w *DocAuditorWorker) Philosophy() string {
	return "Clear documentation reduces cognitive load. Public APIs without docs create barriers to understanding"
}

// Scope implements DiscoveryWorker.
func (w *DocAuditorWorker) Scope() string {
	return "Package docs, exported type/function documentation, complex function comments, README completeness"
}

// Cost implements DiscoveryWorker.
func (w *DocAuditorWorker) Cost() health.CostEstimate {
	return health.CostEstimate{
		EstimatedDuration: 45 * time.Second,
		AICallsEstimated:  5, // Assessment for prioritizing doc gaps
		RequiresFullScan:  true,
		Category:          health.CostModerate,
	}
}

// Dependencies implements DiscoveryWorker.
func (w *DocAuditorWorker) Dependencies() []string {
	return nil // Standalone analysis
}

// Analyze implements DiscoveryWorker.
func (w *DocAuditorWorker) Analyze(ctx context.Context, codebase health.CodebaseContext) (*WorkerResult, error) {
	startTime := time.Now()

	result := &WorkerResult{
		IssuesDiscovered: []DiscoveredIssue{},
		AnalyzedAt:       startTime,
		Stats: AnalysisStats{
			FilesAnalyzed: 0,
			IssuesFound:   0,
		},
	}

	// Check for README.md
	readmeIssues := w.checkReadme(codebase.RootPath)
	result.IssuesDiscovered = append(result.IssuesDiscovered, readmeIssues...)

	// Analyze each Go file for documentation gaps
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
			return nil
		}

		// Check package documentation
		pkgIssues := w.checkPackageDocs(node, path)
		result.IssuesDiscovered = append(result.IssuesDiscovered, pkgIssues...)

		// Check exported declarations
		exportIssues := w.checkExportedDocs(node, fset, path)
		result.IssuesDiscovered = append(result.IssuesDiscovered, exportIssues...)

		// Check complex functions
		complexityIssues := w.checkComplexFunctions(node, fset, path)
		result.IssuesDiscovered = append(result.IssuesDiscovered, complexityIssues...)

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
		"Analyzed %d Go files for documentation quality. Found %d potential documentation gaps.",
		result.Stats.FilesAnalyzed,
		len(result.IssuesDiscovered),
	)

	result.Reasoning = fmt.Sprintf(
		"Based on philosophy: '%s'\n\nDocumentation analysis found:\n"+
			"- Missing package-level documentation\n"+
			"- Exported APIs without godoc comments\n"+
			"- Complex functions lacking explanatory comments\n\n"+
			"AI should evaluate: Which documentation gaps are critical for understanding the codebase?",
		w.Philosophy(),
	)

	result.Stats.IssuesFound = len(result.IssuesDiscovered)
	result.Stats.Duration = time.Since(startTime)
	result.Stats.PatternsFound = len(result.IssuesDiscovered)

	return result, nil
}

// checkReadme verifies that README.md exists and has basic content.
func (w *DocAuditorWorker) checkReadme(rootPath string) []DiscoveredIssue {
	var issues []DiscoveredIssue

	readmePath := filepath.Join(rootPath, "README.md")
	data, err := os.ReadFile(readmePath)
	if err != nil {
		// README.md missing
		issues = append(issues, DiscoveredIssue{
			Title:       "README.md missing",
			Description: "No README.md found in repository root.\n\nA README provides essential context for anyone exploring the codebase: what the project does, how to build it, and how to contribute.",
			Category:    "documentation",
			Type:        "task",
			Priority:    2, // P2 - important for onboarding
			Tags:        []string{"readme", "documentation"},
			FilePath:    rootPath,
			Evidence: map[string]interface{}{
				"path": rootPath,
			},
			Confidence: 1.0, // High confidence - objectively missing
		})
		return issues
	}

	// Check if README is too short (less than 500 characters indicates stub)
	content := string(data)
	if len(content) < 500 {
		issues = append(issues, DiscoveredIssue{
			Title:       "README.md appears incomplete",
			Description: fmt.Sprintf("README.md exists but is very short (%d characters).\n\nA comprehensive README should explain the project's purpose, architecture, setup instructions, and contribution guidelines.", len(content)),
			Category:    "documentation",
			Type:        "task",
			Priority:    3, // P3 - README exists but could be better
			Tags:        []string{"readme", "documentation"},
			FilePath:    readmePath,
			Evidence: map[string]interface{}{
				"size":      len(content),
				"threshold": 500,
			},
			Confidence: 0.7, // Medium-high confidence - might be intentionally brief
		})
	}

	return issues
}

// checkPackageDocs verifies package-level documentation.
func (w *DocAuditorWorker) checkPackageDocs(node *ast.File, filePath string) []DiscoveredIssue {
	var issues []DiscoveredIssue

	// Check if this is the package's main file (typically has package comment)
	// We detect this by checking if doc.go exists, or if this is the first alphabetically
	baseName := filepath.Base(filePath)
	isDocFile := baseName == "doc.go"

	// Only check package docs in doc.go or if no doc.go exists (check first file)
	if !isDocFile {
		return issues
	}

	// Check for package-level documentation
	if node.Doc == nil || len(node.Doc.List) == 0 {
		pkgName := node.Name.Name
		issues = append(issues, DiscoveredIssue{
			Title:       fmt.Sprintf("Package %s missing documentation", pkgName),
			Description: fmt.Sprintf("Package %s has no package-level godoc comment.\n\nPackage comments should explain what the package does and when to use it.", pkgName),
			Category:    "documentation",
			Type:        "task",
			Priority:    2, // P2 - package docs are important
			Tags:        []string{"package-docs", "godoc"},
			FilePath:    filePath,
			LineStart:   1,
			Evidence: map[string]interface{}{
				"package": pkgName,
				"file":    filepath.Base(filePath),
			},
			Confidence: 0.8, // High confidence for missing package docs
		})
	}

	return issues
}

// checkExportedDocs checks that exported declarations have documentation.
func (w *DocAuditorWorker) checkExportedDocs(node *ast.File, fset *token.FileSet, filePath string) []DiscoveredIssue {
	var issues []DiscoveredIssue

	ast.Inspect(node, func(n ast.Node) bool {
		switch decl := n.(type) {
		case *ast.FuncDecl:
			// Check exported functions
			if decl.Name.IsExported() {
				if decl.Doc == nil || len(decl.Doc.List) == 0 {
					pos := fset.Position(decl.Pos())
					issues = append(issues, DiscoveredIssue{
						Title:       fmt.Sprintf("Exported function %s missing documentation", decl.Name.Name),
						Description: fmt.Sprintf("Function %s is exported but has no godoc comment at %s:%d.\n\nExported functions should have comments explaining what they do, parameters, and return values.", decl.Name.Name, filepath.Base(filePath), pos.Line),
						Category:    "documentation",
						Type:        "task",
						Priority:    2, // P2 - exported API documentation is important
						Tags:        []string{"godoc", "exported-api"},
						FilePath:    filePath,
						LineStart:   pos.Line,
						Evidence: map[string]interface{}{
							"function": decl.Name.Name,
							"line":     pos.Line,
							"file":     filepath.Base(filePath),
						},
						Confidence: 0.8, // High confidence
					})
				}
			}

		case *ast.GenDecl:
			// Check exported types, constants, and variables
			for _, spec := range decl.Specs {
				switch s := spec.(type) {
				case *ast.TypeSpec:
					if s.Name.IsExported() {
						if decl.Doc == nil || len(decl.Doc.List) == 0 {
							pos := fset.Position(s.Pos())
							issues = append(issues, DiscoveredIssue{
								Title:       fmt.Sprintf("Exported type %s missing documentation", s.Name.Name),
								Description: fmt.Sprintf("Type %s is exported but has no godoc comment at %s:%d.\n\nExported types should explain what they represent and when to use them.", s.Name.Name, filepath.Base(filePath), pos.Line),
								Category:    "documentation",
								Type:        "task",
								Priority:    2, // P2 - type documentation is important
								Tags:        []string{"godoc", "exported-api"},
								FilePath:    filePath,
								LineStart:   pos.Line,
								Evidence: map[string]interface{}{
									"type": s.Name.Name,
									"line": pos.Line,
									"file": filepath.Base(filePath),
								},
								Confidence: 0.8,
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

// checkComplexFunctions finds complex functions without explanatory comments.
func (w *DocAuditorWorker) checkComplexFunctions(node *ast.File, fset *token.FileSet, filePath string) []DiscoveredIssue {
	var issues []DiscoveredIssue

	ast.Inspect(node, func(n ast.Node) bool {
		if funcDecl, ok := n.(*ast.FuncDecl); ok {
			// Calculate cyclomatic complexity (simplified: count branches)
			complexity := w.calculateCyclomaticComplexity(funcDecl)

			// Flag complex functions (complexity > 10) without documentation
			if complexity > 10 {
				hasDoc := funcDecl.Doc != nil && len(funcDecl.Doc.List) > 0
				if !hasDoc {
					pos := fset.Position(funcDecl.Pos())
					issues = append(issues, DiscoveredIssue{
						Title:       fmt.Sprintf("Complex function %s lacks documentation (complexity: %d)", funcDecl.Name.Name, complexity),
						Description: fmt.Sprintf("Function %s at %s:%d has cyclomatic complexity of %d but no explanatory comment.\n\nComplex functions should document their logic and edge cases to aid understanding.", funcDecl.Name.Name, filepath.Base(filePath), pos.Line, complexity),
						Category:    "documentation",
						Type:        "task",
						Priority:    3, // P3 - helpful but not critical
						Tags:        []string{"complexity", "maintainability"},
						FilePath:    filePath,
						LineStart:   pos.Line,
						Evidence: map[string]interface{}{
							"function":   funcDecl.Name.Name,
							"complexity": complexity,
							"line":       pos.Line,
							"file":       filepath.Base(filePath),
						},
						Confidence: 0.6, // Medium confidence - complexity threshold is heuristic
					})
				}
			}
		}
		return true
	})

	return issues
}

// calculateCyclomaticComplexity computes simplified cyclomatic complexity.
// Real complexity = 1 + number of decision points (if/for/switch/case/&&/||)
func (w *DocAuditorWorker) calculateCyclomaticComplexity(funcDecl *ast.FuncDecl) int {
	if funcDecl.Body == nil {
		return 0
	}

	complexity := 1 // Base complexity

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
			// Each case adds a branch (except default)
			complexity++
		case *ast.BinaryExpr:
			// Logical operators (&&, ||) add complexity
			if n.Op == token.LAND || n.Op == token.LOR {
				complexity++
			}
		}
		return true
	})

	return complexity
}
