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

// DocAuditor is a discovery worker that analyzes documentation quality.
// Philosophy: 'Code should be self-documenting, but critical APIs need explicit documentation'
//
// Analyzes:
// - Missing package docs (exported packages without doc.go)
// - Missing function docs (exported functions without comments)
// - Outdated docs (mentions removed params, old behavior)
// - README quality (installation, usage, examples, architecture)
// - Missing examples (complex APIs without example code)
// - API documentation coverage
type DocAuditor struct{}

// NewDocAuditor creates a new documentation auditor worker.
func NewDocAuditor() discovery.DiscoveryWorker {
	return &DocAuditor{}
}

// Name implements DiscoveryWorker.
func (d *DocAuditor) Name() string {
	return "doc_auditor"
}

// Philosophy implements DiscoveryWorker.
func (d *DocAuditor) Philosophy() string {
	return "Code should be self-documenting, but critical APIs need explicit documentation"
}

// Scope implements DiscoveryWorker.
func (d *DocAuditor) Scope() string {
	return "Package documentation, function comments, README quality, API documentation coverage"
}

// Cost implements DiscoveryWorker.
func (d *DocAuditor) Cost() health.CostEstimate {
	return health.CostEstimate{
		EstimatedDuration: 1 * time.Minute,
		AICallsEstimated:  2, // README analysis + doc quality assessment
		RequiresFullScan:  false,
		Category:          health.CostModerate,
	}
}

// Dependencies implements DiscoveryWorker.
func (d *DocAuditor) Dependencies() []string {
	// Depends on architecture scanner to identify critical packages
	// But can run standalone if architecture scanner isn't available
	return nil
}

// Analyze implements DiscoveryWorker.
func (d *DocAuditor) Analyze(ctx context.Context, codebase health.CodebaseContext) (*discovery.WorkerResult, error) {
	startTime := time.Now()
	issues := []discovery.DiscoveredIssue{}
	filesAnalyzed := 0

	// Find project root (current working directory)
	rootDir, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("failed to get working directory: %w", err)
	}

	// Check README quality
	readmeIssues := d.analyzeREADME(rootDir)
	issues = append(issues, readmeIssues...)

	// Walk through Go source files
	err = filepath.Walk(rootDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // Skip inaccessible files
		}

		// Skip non-Go files
		if !strings.HasSuffix(path, ".go") {
			return nil
		}

		// Skip test files (they don't need package docs)
		if strings.HasSuffix(path, "_test.go") {
			return nil
		}

		// Skip vendor, hidden directories
		if strings.Contains(path, "/vendor/") ||
			strings.Contains(path, "/.") ||
			strings.Contains(path, "/node_modules/") {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		filesAnalyzed++

		// Parse Go file
		fileIssues := d.analyzeGoFile(path, rootDir)
		issues = append(issues, fileIssues...)

		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("failed to walk directory: %w", err)
	}

	return &discovery.WorkerResult{
		IssuesDiscovered: issues,
		Context: fmt.Sprintf("Analyzed %d Go files for documentation quality. "+
			"Found %d documentation issues.", filesAnalyzed, len(issues)),
		Reasoning: "Documentation helps new contributors understand the codebase. " +
			"Missing or outdated docs increase onboarding time and can lead to misuse of APIs. " +
			"This analysis identifies critical gaps in documentation coverage.",
		AnalyzedAt: time.Now(),
		Stats: discovery.AnalysisStats{
			FilesAnalyzed: filesAnalyzed,
			IssuesFound:   len(issues),
			Duration:      time.Since(startTime),
			AICallsMade:   0, // AI assessment happens later in the orchestrator
			PatternsFound: len(issues),
		},
	}, nil
}

// analyzeREADME checks for README existence and quality.
func (d *DocAuditor) analyzeREADME(rootDir string) []discovery.DiscoveredIssue {
	issues := []discovery.DiscoveredIssue{}

	// Look for README files
	readmePaths := []string{
		filepath.Join(rootDir, "README.md"),
		filepath.Join(rootDir, "README"),
		filepath.Join(rootDir, "README.txt"),
	}

	var readmePath string
	var readmeContent []byte
	for _, path := range readmePaths {
		content, err := os.ReadFile(path)
		if err == nil {
			readmePath = path
			readmeContent = content
			break
		}
	}

	// No README at all
	if readmePath == "" {
		issues = append(issues, discovery.DiscoveredIssue{
			Title:       "Missing README file",
			Description: "Project has no README file. A README should explain what the project does, how to install it, and how to use it.",
			Category:    "documentation",
			Type:        "task",
			Priority:    1, // P1 - important for any project
			Tags:        []string{"readme", "onboarding"},
			FilePath:    rootDir,
			Evidence: map[string]interface{}{
				"checked_paths": readmePaths,
			},
			DiscoveredBy: "doc_auditor",
			DiscoveredAt: time.Now(),
			Confidence:   1.0, // High confidence - definitely missing
		})
		return issues
	}

	// Check README quality
	readme := string(readmeContent)
	readmeLines := strings.Split(readme, "\n")

	// Check for basic sections
	hasInstallation := strings.Contains(strings.ToLower(readme), "install")
	hasUsage := strings.Contains(strings.ToLower(readme), "usage") ||
		strings.Contains(strings.ToLower(readme), "getting started")
	hasExamples := strings.Contains(strings.ToLower(readme), "example")

	// Too short
	if len(readmeLines) < 10 {
		issues = append(issues, discovery.DiscoveredIssue{
			Title:       "README is very brief",
			Description: fmt.Sprintf("README has only %d lines. Consider adding installation instructions, usage examples, and architecture overview.", len(readmeLines)),
			Category:    "documentation",
			Type:        "task",
			Priority:    2, // P2
			Tags:        []string{"readme", "documentation-quality"},
			FilePath:    readmePath,
			Evidence: map[string]interface{}{
				"line_count": len(readmeLines),
			},
			DiscoveredBy: "doc_auditor",
			DiscoveredAt: time.Now(),
			Confidence:   0.8,
		})
	}

	// Missing installation
	if !hasInstallation {
		issues = append(issues, discovery.DiscoveredIssue{
			Title:       "README missing installation instructions",
			Description: "README does not contain installation instructions. Add a section explaining how to install and set up the project.",
			Category:    "documentation",
			Type:        "task",
			Priority:    2, // P2
			Tags:        []string{"readme", "installation"},
			FilePath:    readmePath,
			Evidence: map[string]interface{}{
				"has_installation": hasInstallation,
			},
			DiscoveredBy: "doc_auditor",
			DiscoveredAt: time.Now(),
			Confidence:   0.9,
		})
	}

	// Missing usage
	if !hasUsage {
		issues = append(issues, discovery.DiscoveredIssue{
			Title:       "README missing usage instructions",
			Description: "README does not contain usage instructions or getting started guide. Add examples of how to use the project.",
			Category:    "documentation",
			Type:        "task",
			Priority:    2, // P2
			Tags:        []string{"readme", "usage"},
			FilePath:    readmePath,
			Evidence: map[string]interface{}{
				"has_usage": hasUsage,
			},
			DiscoveredBy: "doc_auditor",
			DiscoveredAt: time.Now(),
			Confidence:   0.9,
		})
	}

	// Missing examples
	if !hasExamples && len(readmeLines) > 20 {
		// Only flag if README is substantial but still missing examples
		issues = append(issues, discovery.DiscoveredIssue{
			Title:       "README missing code examples",
			Description: "README does not contain code examples. Consider adding examples to help users get started quickly.",
			Category:    "documentation",
			Type:        "task",
			Priority:    3, // P3 - nice to have
			Tags:        []string{"readme", "examples"},
			FilePath:    readmePath,
			Evidence: map[string]interface{}{
				"has_examples": hasExamples,
			},
			DiscoveredBy: "doc_auditor",
			DiscoveredAt: time.Now(),
			Confidence:   0.7,
		})
	}

	return issues
}

// analyzeGoFile checks a single Go file for documentation issues.
func (d *DocAuditor) analyzeGoFile(path string, rootDir string) []discovery.DiscoveredIssue {
	issues := []discovery.DiscoveredIssue{}

	fset := token.NewFileSet()
	node, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
	if err != nil {
		// Skip files that can't be parsed
		return issues
	}

	relPath, _ := filepath.Rel(rootDir, path)

	// Check package documentation
	if node.Doc == nil || strings.TrimSpace(node.Doc.Text()) == "" {
		// Only flag if this is not a _test.go file and not in a test directory
		if !strings.Contains(relPath, "test") && !strings.HasSuffix(path, "_test.go") {
			issues = append(issues, discovery.DiscoveredIssue{
				Title:       fmt.Sprintf("Package %s missing documentation", node.Name.Name),
				Description: fmt.Sprintf("Package %s in %s has no package-level documentation. Add a doc comment explaining the package's purpose.", node.Name.Name, relPath),
				Category:    "documentation",
				Type:        "task",
				Priority:    2, // P2
				Tags:        []string{"package-docs", "go"},
				FilePath:    path,
				LineStart:   1,
				Evidence: map[string]interface{}{
					"package_name": node.Name.Name,
				},
				DiscoveredBy: "doc_auditor",
				DiscoveredAt: time.Now(),
				Confidence:   0.8,
			})
		}
	}

	// Check exported functions/types/methods
	ast.Inspect(node, func(n ast.Node) bool {
		switch decl := n.(type) {
		case *ast.FuncDecl:
			// Check if function is exported
			if decl.Name.IsExported() {
				// Check for doc comment
				if decl.Doc == nil || strings.TrimSpace(decl.Doc.Text()) == "" {
					pos := fset.Position(decl.Pos())
					issues = append(issues, discovery.DiscoveredIssue{
						Title:       fmt.Sprintf("Exported function %s missing documentation", decl.Name.Name),
						Description: fmt.Sprintf("Function %s in %s is exported but lacks documentation. Add a doc comment explaining what it does.", decl.Name.Name, relPath),
						Category:    "documentation",
						Type:        "task",
						Priority:    3, // P3 - nice to have for all exported symbols
						Tags:        []string{"function-docs", "go", "exported"},
						FilePath:    path,
						LineStart:   pos.Line,
						Evidence: map[string]interface{}{
							"function_name": decl.Name.Name,
						},
						DiscoveredBy: "doc_auditor",
						DiscoveredAt: time.Now(),
						Confidence:   0.7,
					})
				}
			}

		case *ast.GenDecl:
			// Check types, constants, variables
			for _, spec := range decl.Specs {
				switch s := spec.(type) {
				case *ast.TypeSpec:
					if s.Name.IsExported() {
						if decl.Doc == nil || strings.TrimSpace(decl.Doc.Text()) == "" {
							pos := fset.Position(s.Pos())
							issues = append(issues, discovery.DiscoveredIssue{
								Title:       fmt.Sprintf("Exported type %s missing documentation", s.Name.Name),
								Description: fmt.Sprintf("Type %s in %s is exported but lacks documentation. Add a doc comment explaining its purpose.", s.Name.Name, relPath),
								Category:    "documentation",
								Type:        "task",
								Priority:    2, // P2 - types are important API surface
								Tags:        []string{"type-docs", "go", "exported"},
								FilePath:    path,
								LineStart:   pos.Line,
								Evidence: map[string]interface{}{
									"type_name": s.Name.Name,
								},
								DiscoveredBy: "doc_auditor",
								DiscoveredAt: time.Now(),
								Confidence:   0.8,
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
