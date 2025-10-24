package health

import (
	"bufio"
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

	"github.com/steveyegge/vc/internal/ai"
)

const (
	// maxViolationsForAI limits the number of potential violations sent to AI evaluation
	// to prevent token limit errors and excessive API costs.
	// This is a technical constraint (API limits), not a semantic judgment.
	maxViolationsForAI = 30
)

// ZFCDetector identifies Zero Framework Cognition violations: places where
// hardcoded logic, thresholds, or heuristics are used instead of AI judgment.
//
// ZFC Compliance: This detector itself must be ZFC-compliant. It collects
// potential violations (facts), then delegates judgment to AI to distinguish
// true violations from legitimate code.
type ZFCDetector struct {
	// RootPath is the codebase root directory
	RootPath string

	// FileExtensions to scan (default: .go files)
	FileExtensions []string

	// ExcludePatterns for files/directories to skip
	ExcludePatterns []string

	// MinimumViolationThreshold - only file issue if this many violations found
	MinimumViolationThreshold int

	// AI supervisor for evaluating potential violations
	Supervisor AISupervisor
}

// NewZFCDetector creates a ZFC violation detector with sensible defaults.
func NewZFCDetector(rootPath string, supervisor AISupervisor) (*ZFCDetector, error) {
	// Validate and clean the root path
	absPath, err := filepath.Abs(rootPath)
	if err != nil {
		return nil, fmt.Errorf("invalid root path %q: %w", rootPath, err)
	}

	return &ZFCDetector{
		RootPath: absPath,
		FileExtensions: []string{
			".go", // Go source files
		},
		ExcludePatterns: []string{
			"vendor/",
			".git/",
			"testdata/",
			"node_modules/",
			".beads/",
			"_test.go",    // Test files often have legitimate thresholds
			"migrations/", // Migration files often have version numbers
		},
		MinimumViolationThreshold: 3, // Only file issue if ≥3 violations found
		Supervisor:                supervisor,
	}, nil
}

// Name implements HealthMonitor.
func (d *ZFCDetector) Name() string {
	return "zfc_detector"
}

// Philosophy implements HealthMonitor.
func (d *ZFCDetector) Philosophy() string {
	return "Zero Framework Cognition: All decisions should be delegated to AI judgment, " +
		"not encoded in hardcoded thresholds, regex patterns, heuristics, or brittle conditional logic. " +
		"Code should express timeless principles and let AI make context-aware decisions."
}

// Schedule implements HealthMonitor.
func (d *ZFCDetector) Schedule() ScheduleConfig {
	return ScheduleConfig{
		Type:     ScheduleTimeBased,
		Interval: 7 * 24 * time.Hour, // Weekly
	}
}

// Cost implements HealthMonitor.
func (d *ZFCDetector) Cost() CostEstimate {
	return CostEstimate{
		EstimatedDuration: 10 * time.Second,
		AICallsEstimated:  1, // One call to evaluate all violations
		RequiresFullScan:  true,
		Category:          CostModerate,
	}
}

// Check implements HealthMonitor.
func (d *ZFCDetector) Check(ctx context.Context, codebase CodebaseContext) (*MonitorResult, error) {
	startTime := time.Now()

	// 1. Scan codebase for potential ZFC violations
	violations, err := d.scanFiles(ctx)
	if err != nil {
		return nil, fmt.Errorf("scanning files: %w", err)
	}

	// 2. If below threshold, no action needed
	if len(violations) < d.MinimumViolationThreshold {
		return &MonitorResult{
			IssuesFound: []DiscoveredIssue{},
			Context:     fmt.Sprintf("Found %d potential ZFC violations (below threshold of %d)", len(violations), d.MinimumViolationThreshold),
			CheckedAt:   startTime,
			Stats: CheckStats{
				FilesScanned: len(violations),
				Duration:     time.Since(startTime),
			},
		}, nil
	}

	// 3. Validate that AI supervisor is configured (only needed if above threshold)
	if d.Supervisor == nil {
		return nil, fmt.Errorf("AI supervisor is required for ZFC detection")
	}

	// 4. Ask AI to evaluate the violations
	evaluation, err := d.evaluateViolations(ctx, violations)
	if err != nil {
		return nil, fmt.Errorf("evaluating violations: %w", err)
	}

	// 4. Build issues from AI evaluation
	issues := d.buildIssues(evaluation)

	return &MonitorResult{
		IssuesFound: issues,
		Context:     d.buildContext(violations, evaluation),
		Reasoning:   d.buildReasoning(evaluation),
		CheckedAt:   startTime,
		Stats: CheckStats{
			FilesScanned: d.countUniqueFiles(violations),
			IssuesFound:  len(issues),
			Duration:     time.Since(startTime),
			AICallsMade:  1,
		},
	}, nil
}

// zfcViolation represents a potential ZFC violation.
type zfcViolation struct {
	FilePath      string
	LineNumber    int
	LineContent   string
	ViolationType string // "magic_number", "regex_parsing", "complex_conditional", "string_matching", "hardcoded_path"
	Context       string // Surrounding context for AI evaluation
}

// scanFiles walks the directory tree and finds potential ZFC violations.
func (d *ZFCDetector) scanFiles(ctx context.Context) ([]zfcViolation, error) {
	var violations []zfcViolation

	err := filepath.Walk(d.RootPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Check context cancellation
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		// Get relative path for pattern matching
		relPath, err := filepath.Rel(d.RootPath, path)
		if err != nil {
			return nil
		}

		// Skip excluded patterns
		if ShouldExcludePath(relPath, info, d.ExcludePatterns) {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		// Only process files (not directories)
		if info.IsDir() {
			return nil
		}

		// Check if file matches our extensions
		if !d.shouldScanFile(path) {
			return nil
		}

		// Scan file for violations
		fileViolations, err := d.scanFile(path, relPath)
		if err != nil {
			// Log error but continue scanning
			return nil
		}

		violations = append(violations, fileViolations...)

		return nil
	})

	return violations, err
}

// shouldScanFile checks if a file should be scanned based on its extension.
func (d *ZFCDetector) shouldScanFile(path string) bool {
	for _, ext := range d.FileExtensions {
		if strings.HasSuffix(path, ext) {
			return true
		}
	}
	return false
}

// scanFile scans a single file for ZFC violations.
func (d *ZFCDetector) scanFile(absPath, relPath string) ([]zfcViolation, error) {
	// Try AST-based analysis for Go files first
	if strings.HasSuffix(absPath, ".go") {
		violations, err := d.scanGoFile(absPath, relPath)
		if err == nil {
			return violations, nil
		}
		// Fall back to pattern-based analysis if AST parsing fails
	}

	// Pattern-based analysis for all files
	return d.scanFilePatterns(absPath, relPath)
}

// scanGoFile uses Go AST parsing to detect violations.
func (d *ZFCDetector) scanGoFile(absPath, relPath string) ([]zfcViolation, error) {
	fset := token.NewFileSet()
	node, err := parser.ParseFile(fset, absPath, nil, parser.ParseComments)
	if err != nil {
		return nil, err
	}

	var violations []zfcViolation

	// Walk the AST looking for violations
	ast.Inspect(node, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.BinaryExpr:
			// Check for magic number comparisons (e.g., if count > 10)
			if v := d.checkMagicNumberComparison(fset, x, relPath); v != nil {
				violations = append(violations, *v)
			}

		case *ast.CallExpr:
			// Check for regex compilation (potential semantic parsing)
			if v := d.checkRegexUsage(fset, x, relPath); v != nil {
				violations = append(violations, *v)
			}

			// Check for string matching (strings.Contains, strings.HasPrefix, etc.)
			if v := d.checkStringMatching(fset, x, relPath); v != nil {
				violations = append(violations, *v)
			}

		case *ast.IfStmt:
			// Check for complex conditionals
			if v := d.checkComplexConditional(fset, x, relPath); v != nil {
				violations = append(violations, *v)
			}
		}

		return true
	})

	return violations, nil
}

// checkMagicNumberComparison detects comparisons with hardcoded numbers.
func (d *ZFCDetector) checkMagicNumberComparison(fset *token.FileSet, expr *ast.BinaryExpr, relPath string) *zfcViolation {
	// Look for comparison operators: <, >, <=, >=, ==, !=
	switch expr.Op {
	case token.LSS, token.GTR, token.LEQ, token.GEQ, token.EQL, token.NEQ:
		// Check if one side is a basic literal (number)
		var numLit *ast.BasicLit
		if lit, ok := expr.Y.(*ast.BasicLit); ok && (lit.Kind == token.INT || lit.Kind == token.FLOAT) {
			numLit = lit
		} else if lit, ok := expr.X.(*ast.BasicLit); ok && (lit.Kind == token.INT || lit.Kind == token.FLOAT) {
			numLit = lit
		}

		if numLit != nil {
			// ZFC Compliance: Send ALL numeric comparisons to AI for evaluation.
			// Don't assume 0, 1, -1, or 100 are always legitimate - context matters!
			// Example: "if retryCount > 100" could be a violation (arbitrary limit)
			//          "if percentage == 100" is probably legitimate (semantic meaning)
			// Let AI decide based on variable names and context.

			pos := fset.Position(expr.Pos())
			return &zfcViolation{
				FilePath:      relPath,
				LineNumber:    pos.Line,
				LineContent:   d.getLineContent(fset.File(expr.Pos()).Name(), pos.Line),
				ViolationType: "magic_number",
				Context:       fmt.Sprintf("Comparison with hardcoded threshold: %s", numLit.Value),
			}
		}
	}

	return nil
}

// checkRegexUsage detects regex compilation that might be used for semantic parsing.
func (d *ZFCDetector) checkRegexUsage(fset *token.FileSet, call *ast.CallExpr, relPath string) *zfcViolation {
	// Check for regexp.MustCompile or regexp.Compile
	if sel, ok := call.Fun.(*ast.SelectorExpr); ok {
		if ident, ok := sel.X.(*ast.Ident); ok && ident.Name == "regexp" {
			if sel.Sel.Name == "MustCompile" || sel.Sel.Name == "Compile" {
				pos := fset.Position(call.Pos())
				return &zfcViolation{
					FilePath:      relPath,
					LineNumber:    pos.Line,
					LineContent:   d.getLineContent(fset.File(call.Pos()).Name(), pos.Line),
					ViolationType: "regex_parsing",
					Context:       "Regex pattern used (may encode semantic parsing logic)",
				}
			}
		}
	}

	return nil
}

// checkStringMatching detects string matching functions that might encode classification logic.
func (d *ZFCDetector) checkStringMatching(fset *token.FileSet, call *ast.CallExpr, relPath string) *zfcViolation {
	// Check for strings.Contains, strings.HasPrefix, strings.HasSuffix
	if sel, ok := call.Fun.(*ast.SelectorExpr); ok {
		if ident, ok := sel.X.(*ast.Ident); ok && ident.Name == "strings" {
			switch sel.Sel.Name {
			case "Contains", "HasPrefix", "HasSuffix", "EqualFold":
				// Check if this is in a conditional or switch statement (likely classification logic)
				pos := fset.Position(call.Pos())
				return &zfcViolation{
					FilePath:      relPath,
					LineNumber:    pos.Line,
					LineContent:   d.getLineContent(fset.File(call.Pos()).Name(), pos.Line),
					ViolationType: "string_matching",
					Context:       fmt.Sprintf("String matching using strings.%s (may encode classification)", sel.Sel.Name),
				}
			}
		}
	}

	return nil
}

// checkComplexConditional detects complex if statements with multiple conditions.
func (d *ZFCDetector) checkComplexConditional(fset *token.FileSet, ifStmt *ast.IfStmt, relPath string) *zfcViolation {
	// Count the number of && and || operators in the condition
	complexity := d.countConditionalComplexity(ifStmt.Cond)

	// ZFC Compliance: Send ALL conditionals with multiple conditions to AI for evaluation.
	// Don't hardcode "3 is too complex" - let AI decide based on context.
	// A 2-condition check might encode business rules, or might be simple safety.
	// Examples:
	//   if amount > 1000 && user != "admin" { } // Business rule - likely violation
	//   if ptr != nil && ptr.IsValid() { }       // Safety check - legitimate
	if complexity >= 2 {
		pos := fset.Position(ifStmt.Pos())
		return &zfcViolation{
			FilePath:      relPath,
			LineNumber:    pos.Line,
			LineContent:   d.getLineContent(fset.File(ifStmt.Pos()).Name(), pos.Line),
			ViolationType: "complex_conditional",
			Context:       fmt.Sprintf("Complex conditional with %d conditions (may encode business rules)", complexity),
		}
	}

	return nil
}

// countConditionalComplexity counts the number of && and || operators in an expression.
func (d *ZFCDetector) countConditionalComplexity(expr ast.Expr) int {
	count := 0
	ast.Inspect(expr, func(n ast.Node) bool {
		if binExpr, ok := n.(*ast.BinaryExpr); ok {
			if binExpr.Op == token.LAND || binExpr.Op == token.LOR {
				count++
			}
		}
		return true
	})
	return count
}

// scanFilePatterns uses regex-based pattern matching to detect violations.
func (d *ZFCDetector) scanFilePatterns(absPath, relPath string) ([]zfcViolation, error) {
	file, err := os.Open(absPath)
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()

	var violations []zfcViolation
	scanner := bufio.NewScanner(file)
	lineNumber := 0

	// ZFC Meta-Note: Yes, this detector uses regexp.MustCompile!
	// This is the "bootstrap problem": we need SOME pattern matching to identify
	// candidates before sending them to AI for judgment. These regex patterns
	// don't make semantic decisions - they just collect potential violations.
	// The AI makes the final judgment on whether each match is a true violation.
	//
	// Think of it like a metal detector: it beeps on all metal objects (candidates),
	// then a human decides which are treasure vs trash (AI evaluation).
	magicNumberPattern := regexp.MustCompile(`if\s+\w+\s*[<>!=]+\s*\d{2,}`)
	regexPattern := regexp.MustCompile(`regexp\.(MustCompile|Compile)`)
	stringMatchPattern := regexp.MustCompile(`strings\.(Contains|HasPrefix|HasSuffix|EqualFold)`)
	hardcodedPathPattern := regexp.MustCompile(`"[/\\][a-zA-Z0-9_/\\.-]+"`)

	for scanner.Scan() {
		lineNumber++
		line := scanner.Text()

		// Skip comments only (not blank lines - they won't match patterns anyway)
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "//") || strings.HasPrefix(trimmed, "/*") || strings.HasPrefix(trimmed, "*") {
			continue
		}

		// Check for magic number thresholds
		if magicNumberPattern.MatchString(line) {
			violations = append(violations, zfcViolation{
				FilePath:      relPath,
				LineNumber:    lineNumber,
				LineContent:   line,
				ViolationType: "magic_number",
				Context:       "Comparison with hardcoded threshold",
			})
		}

		// Check for regex usage
		if regexPattern.MatchString(line) {
			violations = append(violations, zfcViolation{
				FilePath:      relPath,
				LineNumber:    lineNumber,
				LineContent:   line,
				ViolationType: "regex_parsing",
				Context:       "Regex pattern compilation",
			})
		}

		// Check for string matching
		if stringMatchPattern.MatchString(line) {
			violations = append(violations, zfcViolation{
				FilePath:      relPath,
				LineNumber:    lineNumber,
				LineContent:   line,
				ViolationType: "string_matching",
				Context:       "String matching function",
			})
		}

		// Check for hardcoded paths
		if hardcodedPathPattern.MatchString(line) {
			violations = append(violations, zfcViolation{
				FilePath:      relPath,
				LineNumber:    lineNumber,
				LineContent:   line,
				ViolationType: "hardcoded_path",
				Context:       "Hardcoded file path",
			})
		}
	}

	return violations, scanner.Err()
}

// getLineContent reads a specific line from a file.
func (d *ZFCDetector) getLineContent(filePath string, lineNumber int) string {
	file, err := os.Open(filePath)
	if err != nil {
		return ""
	}
	defer func() { _ = file.Close() }()

	scanner := bufio.NewScanner(file)
	currentLine := 0

	for scanner.Scan() {
		currentLine++
		if currentLine == lineNumber {
			return scanner.Text()
		}
	}

	return ""
}

// zfcEvaluation is the AI's assessment of potential ZFC violations.
type zfcEvaluation struct {
	TrueViolations      []zfcViolationAssessment `json:"true_violations"`
	LegitimateCode      []legitimateCodePattern  `json:"legitimate_code"`
	RefactoringGuidance string                   `json:"refactoring_guidance"`
	Reasoning           string                   `json:"reasoning"`
}

type zfcViolationAssessment struct {
	FilePath              string `json:"file_path"`
	LineNumber            int    `json:"line_number"`
	ViolationType         string `json:"violation_type"`
	Impact                string `json:"impact"` // "low", "medium", "high"
	WhyViolation          string `json:"why_violation"`
	RefactoringSuggestion string `json:"refactoring_suggestion"`
}

type legitimateCodePattern struct {
	FilePath      string `json:"file_path"`
	LineNumber    int    `json:"line_number"`
	Justification string `json:"justification"`
}

// evaluateViolations uses AI to assess whether potential violations are true ZFC violations.
func (d *ZFCDetector) evaluateViolations(ctx context.Context, violations []zfcViolation) (*zfcEvaluation, error) {
	// Limit violations sent to AI to prevent token limit errors
	violationsToEvaluate := violations
	if len(violations) > maxViolationsForAI {
		violationsToEvaluate = violations[:maxViolationsForAI]
	}

	prompt := d.buildPrompt(violationsToEvaluate)

	// Call AI supervisor
	response, err := d.Supervisor.CallAI(ctx, prompt, "zfc_evaluation", "", 8192)
	if err != nil {
		return nil, fmt.Errorf("AI call failed: %w", err)
	}

	// Parse JSON response using resilient parser
	parseResult := ai.Parse[zfcEvaluation](response, ai.ParseOptions{
		Context: "zfc_evaluation",
	})

	if !parseResult.Success {
		return nil, fmt.Errorf("parsing AI response: %s", parseResult.Error)
	}

	return &parseResult.Data, nil
}

// buildPrompt creates the AI evaluation prompt.
func (d *ZFCDetector) buildPrompt(violations []zfcViolation) string {
	var sb strings.Builder

	sb.WriteString("# ZFC Violation Detection Request\n\n")
	sb.WriteString("## Philosophy: Zero Framework Cognition\n")
	sb.WriteString(d.Philosophy())
	sb.WriteString("\n\n")

	sb.WriteString("## Potential Violations Found\n")
	sb.WriteString(fmt.Sprintf("Found %d potential ZFC violations to evaluate:\n\n", len(violations)))

	// Group by violation type for clarity
	byType := make(map[string][]zfcViolation)
	for _, v := range violations {
		byType[v.ViolationType] = append(byType[v.ViolationType], v)
	}

	for vType, vList := range byType {
		sb.WriteString(fmt.Sprintf("### %s (%d instances)\n\n", strings.ReplaceAll(vType, "_", " "), len(vList)))
		for i, v := range vList {
			if i >= 5 {
				sb.WriteString(fmt.Sprintf("... and %d more\n", len(vList)-5))
				break
			}
			sb.WriteString(fmt.Sprintf("- `%s:%d`\n", v.FilePath, v.LineNumber))
			sb.WriteString(fmt.Sprintf("  ```\n  %s\n  ```\n", strings.TrimSpace(v.LineContent)))
		}
		sb.WriteString("\n")
	}

	// Use dynamic year to prevent prompt from becoming stale
	year := time.Now().Year()
	sb.WriteString(fmt.Sprintf("## Guidance (%d)\n\n", year))

	sb.WriteString("### True ZFC Violations\n")
	sb.WriteString("Code that encodes decisions that should be AI-driven:\n")
	sb.WriteString("- **Magic number thresholds**: `if count > 50` where 50 is a judgment call\n")
	sb.WriteString("- **Regex for semantic parsing**: Pattern matching to extract intent or meaning\n")
	sb.WriteString("- **Business rule conditionals**: Complex if statements encoding policy decisions\n")
	sb.WriteString("- **Classification by string matching**: Keyword detection for categorization\n")
	sb.WriteString("- **Hardcoded assumptions**: File paths, naming patterns that will become stale\n\n")

	sb.WriteString("### Legitimate Code (NOT violations)\n")
	sb.WriteString("- **Constants with semantic meaning**: `const MaxRetries = 3` (algorithm parameter, not judgment)\n")
	sb.WriteString("- **Protocol/format parsing**: Regex for well-defined formats (URLs, emails, JSON)\n")
	sb.WriteString("- **Boundary checks**: `if index < 0` (safety, not decision-making)\n")
	sb.WriteString("- **Configuration values**: Loaded from env/config (externalized, not hardcoded)\n")
	sb.WriteString("- **Standard library usage**: Normal string operations not encoding classification\n\n")

	sb.WriteString("### Key Distinction\n")
	sb.WriteString("Ask: \"Could this decision change with context, expertise, or new information?\"\n")
	sb.WriteString("- **YES** → ZFC violation (should ask AI)\n")
	sb.WriteString("- **NO** → Legitimate code (algorithmic or safety constraint)\n\n")

	sb.WriteString("## Your Task\n")
	sb.WriteString("For each potential violation, determine:\n")
	sb.WriteString("1. **Is this a true ZFC violation?** Does it encode a judgment/decision?\n")
	sb.WriteString("2. **Impact level**: low/medium/high (based on how brittle the assumption is)\n")
	sb.WriteString("3. **Refactoring approach**: How should this be delegated to AI?\n\n")

	sb.WriteString("## Response Format\n")
	sb.WriteString("Return valid JSON:\n")
	sb.WriteString("```json\n")
	sb.WriteString("{\n")
	sb.WriteString("  \"true_violations\": [\n")
	sb.WriteString("    {\n")
	sb.WriteString("      \"file_path\": \"internal/foo/bar.go\",\n")
	sb.WriteString("      \"line_number\": 42,\n")
	sb.WriteString("      \"violation_type\": \"magic_number\",\n")
	sb.WriteString("      \"impact\": \"high\",\n")
	sb.WriteString("      \"why_violation\": \"Threshold will become stale as data volume grows\",\n")
	sb.WriteString("      \"refactoring_suggestion\": \"Replace with AI judgment: 'Is this file size concerning for this project?'\"\n")
	sb.WriteString("    }\n")
	sb.WriteString("  ],\n")
	sb.WriteString("  \"legitimate_code\": [\n")
	sb.WriteString("    {\n")
	sb.WriteString("      \"file_path\": \"internal/foo/bar.go\",\n")
	sb.WriteString("      \"line_number\": 100,\n")
	sb.WriteString("      \"justification\": \"Boundary check for array access, not a judgment call\"\n")
	sb.WriteString("    }\n")
	sb.WriteString("  ],\n")
	sb.WriteString("  \"refactoring_guidance\": \"Overall strategy for eliminating these violations\",\n")
	sb.WriteString("  \"reasoning\": \"Brief explanation of evaluation approach\"\n")
	sb.WriteString("}\n")
	sb.WriteString("```\n")

	return sb.String()
}

// buildIssues converts AI evaluation to DiscoveredIssues.
func (d *ZFCDetector) buildIssues(eval *zfcEvaluation) []DiscoveredIssue {
	var issues []DiscoveredIssue

	// Group violations by impact level
	byImpact := make(map[string][]zfcViolationAssessment)
	for _, v := range eval.TrueViolations {
		byImpact[v.Impact] = append(byImpact[v.Impact], v)
	}

	// Create one issue per impact level
	for impact, violations := range byImpact {
		if len(violations) == 0 {
			continue
		}

		description := d.buildIssueDescription(impact, violations, eval.RefactoringGuidance)

		evidence := map[string]interface{}{
			"violations":           violations,
			"refactoring_guidance": eval.RefactoringGuidance,
			"reasoning":            eval.Reasoning,
			"violation_count":      len(violations),
		}

		issues = append(issues, DiscoveredIssue{
			Category:    "zfc_violation",
			Severity:    impact,
			Description: description,
			Evidence:    evidence,
		})
	}

	return issues
}

// buildIssueDescription creates a description for a ZFC violation issue.
func (d *ZFCDetector) buildIssueDescription(impact string, violations []zfcViolationAssessment, guidance string) string {
	// Count by type
	byType := make(map[string]int)
	for _, v := range violations {
		byType[v.ViolationType]++
	}

	var parts []string
	for vType, count := range byType {
		typeName := strings.ReplaceAll(vType, "_", " ")
		if count == 1 {
			parts = append(parts, fmt.Sprintf("1 %s", typeName))
		} else {
			parts = append(parts, fmt.Sprintf("%d %s instances", count, typeName))
		}
	}

	return fmt.Sprintf("ZFC violations (%s impact): %s", impact, strings.Join(parts, ", "))
}

// buildContext creates context string for MonitorResult.
func (d *ZFCDetector) buildContext(violations []zfcViolation, eval *zfcEvaluation) string {
	return fmt.Sprintf(
		"Scanned codebase and found %d potential ZFC violations. "+
			"AI categorized %d as true violations, %d as legitimate code patterns.",
		len(violations), len(eval.TrueViolations), len(eval.LegitimateCode),
	)
}

// buildReasoning creates reasoning string from AI evaluation.
func (d *ZFCDetector) buildReasoning(eval *zfcEvaluation) string {
	if eval.Reasoning != "" {
		return eval.Reasoning
	}
	return fmt.Sprintf(
		"Identified %d ZFC violations requiring refactoring to delegate decisions to AI.",
		len(eval.TrueViolations),
	)
}

// countUniqueFiles counts the number of unique files in the violations list.
func (d *ZFCDetector) countUniqueFiles(violations []zfcViolation) int {
	files := make(map[string]bool)
	for _, v := range violations {
		files[v.FilePath] = true
	}
	return len(files)
}
