package health

import (
	"bufio"
	"context"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/steveyegge/vc/internal/ai"
)

// ComplexityMonitor detects overly complex functions using AI-powered judgment
// rather than hardcoded thresholds.
//
// ZFC Compliance: Calculates cyclomatic complexity metrics, builds context,
// then delegates judgment to AI supervisor to distinguish inherent vs avoidable complexity.
type ComplexityMonitor struct {
	// RootPath is the codebase root directory
	RootPath string

	// ComplexityThreshold is the minimum complexity to consider for review
	// Default: 20 (complexity >20 are candidates for AI evaluation)
	ComplexityThreshold int

	// TopN limits analysis to the N most complex functions (0 = analyze all above threshold)
	// Default: 10 to avoid overwhelming AI with too many functions
	TopN int

	// MaxFunctionBodyLines limits the number of lines extracted per function
	// Default: 100 to keep prompts manageable
	MaxFunctionBodyLines int

	// FileExtensions to scan (default: [".go"])
	FileExtensions []string

	// ExcludePatterns for files/directories to skip
	ExcludePatterns []string

	// AI supervisor for evaluating complexity
	Supervisor AISupervisor
}

// FunctionComplexity represents a function's complexity metrics.
type FunctionComplexity struct {
	Complexity int
	Package    string
	Function   string
	FilePath   string
	Line       int
	Column     int
	Body       string // Function source code (extracted for AI analysis)
}

// NewComplexityMonitor creates a complexity monitor with sensible defaults.
func NewComplexityMonitor(rootPath string, supervisor AISupervisor) (*ComplexityMonitor, error) {
	absPath, err := filepath.Abs(rootPath)
	if err != nil {
		return nil, fmt.Errorf("invalid root path %q: %w", rootPath, err)
	}

	return &ComplexityMonitor{
		RootPath:             absPath,
		ComplexityThreshold:  20,
		TopN:                 10, // Limit to top 10 to keep prompts manageable
		MaxFunctionBodyLines: 100, // Limit function body size
		FileExtensions:       []string{".go"},
		ExcludePatterns: []string{
			"vendor/",
			".git/",
			".pb.go",  // Generated protobuf
			".gen.go", // Other generated code
			"testdata/",
		},
		Supervisor: supervisor,
	}, nil
}

// Name implements HealthMonitor.
func (m *ComplexityMonitor) Name() string {
	return "complexity"
}

// Philosophy implements HealthMonitor.
func (m *ComplexityMonitor) Philosophy() string {
	return "Complex functions are hard to understand, test, and maintain. " +
		"Complexity should be justified by inherent problem domain complexity " +
		"(e.g., parsers, state machines), not poor structure."
}

// Schedule implements HealthMonitor.
func (m *ComplexityMonitor) Schedule() ScheduleConfig {
	return ScheduleConfig{
		Type:         ScheduleEventBased,
		EventTrigger: "every_20_issues",
	}
}

// Cost implements HealthMonitor.
func (m *ComplexityMonitor) Cost() CostEstimate {
	return CostEstimate{
		EstimatedDuration: 30 * time.Second,
		AICallsEstimated:  1, // One call to evaluate all complex functions
		RequiresFullScan:  true,
		Category:          CostExpensive, // Heavy AI usage with function bodies
	}
}

// Check implements HealthMonitor.
func (m *ComplexityMonitor) Check(ctx context.Context, codebase CodebaseContext) (*MonitorResult, error) {
	if m.Supervisor == nil {
		return nil, fmt.Errorf("AI supervisor is required for complexity monitoring")
	}

	startTime := time.Now()

	// 1. Run gocyclo to get complexity metrics
	complexFunctions, err := m.analyzeComplexity(ctx)
	if err != nil {
		return nil, fmt.Errorf("analyzing complexity: %w", err)
	}

	if len(complexFunctions) == 0 {
		return &MonitorResult{
			IssuesFound: []DiscoveredIssue{},
			Context:     fmt.Sprintf("No functions found with complexity > %d", m.ComplexityThreshold),
			CheckedAt:   startTime,
			Stats: CheckStats{
				FilesScanned: 0,
				Duration:     time.Since(startTime),
			},
		}, nil
	}

	// 2. Extract function bodies for AI analysis
	if err := m.extractFunctionBodies(complexFunctions); err != nil {
		return nil, fmt.Errorf("extracting function bodies: %w", err)
	}

	// 3. Build AI prompt with context
	prompt := m.buildAIPrompt(complexFunctions, codebase)

	// 4. Call AI to evaluate complexity (vc-35: use environment-configurable model)
	response, err := m.Supervisor.CallAI(ctx, prompt, "complexity_evaluation", ai.GetDefaultModel(), 4096)
	if err != nil {
		return nil, fmt.Errorf("AI evaluation failed: %w", err)
	}

	// 5. Parse AI response
	issues, err := m.parseAIResponse(response)
	if err != nil {
		return nil, fmt.Errorf("parsing AI response: %w", err)
	}

	return &MonitorResult{
		IssuesFound: issues,
		Context: fmt.Sprintf("Analyzed %d functions with complexity > %d",
			len(complexFunctions), m.ComplexityThreshold),
		Reasoning: "AI evaluated each function to distinguish inherent problem domain complexity from poor structure",
		CheckedAt: startTime,
		Stats: CheckStats{
			FilesScanned: countUniqueFiles(complexFunctions),
			IssuesFound:  len(issues),
			Duration:     time.Since(startTime),
			AICallsMade:  1,
		},
	}, nil
}

// analyzeComplexity runs gocyclo to calculate complexity metrics.
func (m *ComplexityMonitor) analyzeComplexity(ctx context.Context) ([]*FunctionComplexity, error) {
	// Build gocyclo command
	args := []string{fmt.Sprintf("-over=%d", m.ComplexityThreshold-1)}

	// Add ignore patterns
	if len(m.ExcludePatterns) > 0 {
		// Combine patterns into regex: (pattern1|pattern2|pattern3)
		patterns := make([]string, len(m.ExcludePatterns))
		for i, p := range m.ExcludePatterns {
			// Escape special regex chars and convert glob-like patterns to regex
			p = strings.ReplaceAll(p, "/", "\\/")
			patterns[i] = p
		}
		ignoreRegex := strings.Join(patterns, "|")
		args = append(args, "-ignore", ignoreRegex)
	}

	args = append(args, m.RootPath)

	cmd := exec.CommandContext(ctx, "gocyclo", args...)
	output, err := cmd.Output()
	if err != nil {
		// Exit code 1 means functions found (expected), other errors are real failures
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() != 1 {
			return nil, fmt.Errorf("gocyclo failed: %w (stderr: %s)", err, exitErr.Stderr)
		}
		// Exit code 1 with output is success (functions found)
		if len(output) == 0 {
			return nil, fmt.Errorf("gocyclo failed: %w", err)
		}
	}

	// Parse gocyclo output
	// Format: <complexity> <package> <function> <file:line:column>
	functions, err := m.parseGocycloOutput(string(output))
	if err != nil {
		return nil, fmt.Errorf("parsing gocyclo output: %w", err)
	}

	// Apply TopN filter if set
	if m.TopN > 0 && len(functions) > m.TopN {
		// Sort by complexity descending
		sort.Slice(functions, func(i, j int) bool {
			return functions[i].Complexity > functions[j].Complexity
		})
		functions = functions[:m.TopN]
	}

	return functions, nil
}

// parseGocycloOutput parses gocyclo output into FunctionComplexity structs.
func (m *ComplexityMonitor) parseGocycloOutput(output string) ([]*FunctionComplexity, error) {
	var functions []*FunctionComplexity

	scanner := bufio.NewScanner(strings.NewReader(output))
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}

		// Parse format: <complexity> <package> <function> <file:line:column>
		parts := strings.Fields(line)
		if len(parts) < 4 {
			continue // Skip malformed lines
		}

		complexity, err := strconv.Atoi(parts[0])
		if err != nil {
			continue // Skip if complexity not a number
		}

		pkg := parts[1]
		function := parts[2]
		location := parts[3]

		// Parse location: file:line:column
		locationParts := strings.Split(location, ":")
		if len(locationParts) < 3 {
			continue // Skip if location malformed
		}

		filePath := locationParts[0]
		lineNum, err := strconv.Atoi(locationParts[1])
		if err != nil {
			continue
		}
		colNum, err := strconv.Atoi(locationParts[2])
		if err != nil {
			continue
		}

		functions = append(functions, &FunctionComplexity{
			Complexity: complexity,
			Package:    pkg,
			Function:   function,
			FilePath:   filePath,
			Line:       lineNum,
			Column:     colNum,
		})
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("reading gocyclo output: %w", err)
	}

	return functions, nil
}

// extractFunctionBodies reads function source code for AI analysis.
func (m *ComplexityMonitor) extractFunctionBodies(functions []*FunctionComplexity) error {
	// Group functions by file to minimize file reads
	fileMap := make(map[string][]*FunctionComplexity)
	for _, fn := range functions {
		fileMap[fn.FilePath] = append(fileMap[fn.FilePath], fn)
	}

	// Extract bodies file by file
	for filePath, fileFunctions := range fileMap {
		content, err := os.ReadFile(filePath)
		if err != nil {
			return fmt.Errorf("reading %s: %w", filePath, err)
		}

		lines := strings.Split(string(content), "\n")

		for _, fn := range fileFunctions {
			body, err := m.extractFunctionBody(lines, fn.Line-1) // Line is 1-indexed
			if err != nil {
				// Log warning but continue with other functions
				fn.Body = fmt.Sprintf("// Error extracting body: %v", err)
				continue
			}
			fn.Body = body
		}
	}

	return nil
}

// extractFunctionBody extracts function source from line index to closing brace.
func (m *ComplexityMonitor) extractFunctionBody(lines []string, startLine int) (string, error) {
	if startLine < 0 || startLine >= len(lines) {
		return "", fmt.Errorf("invalid start line %d", startLine)
	}

	// Find function definition and body
	var bodyLines []string
	braceDepth := 0
	foundStart := false

	maxLines := m.MaxFunctionBodyLines
	if maxLines == 0 {
		maxLines = 100 // Default limit
	}

	for i := startLine; i < len(lines) && i < startLine+maxLines; i++ {
		line := lines[i]
		bodyLines = append(bodyLines, line)

		// Count braces to find function end
		for _, ch := range line {
			switch ch {
			case '{':
				braceDepth++
				foundStart = true
			case '}':
				braceDepth--
				if foundStart && braceDepth == 0 {
					return strings.Join(bodyLines, "\n"), nil
				}
			}
		}
	}

	// If we hit the line limit, add a note
	if len(bodyLines) >= maxLines {
		bodyLines = append(bodyLines, fmt.Sprintf("// ... (truncated at %d lines)", maxLines))
	}

	// If we didn't find the end, return what we have
	return strings.Join(bodyLines, "\n"), nil
}

// buildAIPrompt creates the prompt for AI complexity evaluation.
func (m *ComplexityMonitor) buildAIPrompt(functions []*FunctionComplexity, codebase CodebaseContext) string {
	var sb strings.Builder

	sb.WriteString("# Complexity Analysis Request\n\n")

	sb.WriteString("## Philosophy\n\n")
	sb.WriteString(m.Philosophy())
	sb.WriteString("\n\n")

	sb.WriteString("## Guidance (late-2025)\n\n")
	sb.WriteString("Complexity 1-10: Simple\n")
	sb.WriteString("Complexity 11-20: Moderate, often acceptable\n")
	sb.WriteString("Complexity 21-50: High, review if simplifiable\n")
	sb.WriteString("Complexity 50+: Very high, usually warrants refactoring\n\n")

	// Add codebase context if available
	if codebase.ComplexityDistribution.Count > 0 {
		dist := codebase.ComplexityDistribution
		sb.WriteString("## Codebase Complexity Distribution\n\n")
		sb.WriteString(fmt.Sprintf("- Mean: %.1f\n", dist.Mean))
		sb.WriteString(fmt.Sprintf("- Median: %.1f\n", dist.Median))
		sb.WriteString(fmt.Sprintf("- Std Dev: %.1f\n", dist.StdDev))
		sb.WriteString(fmt.Sprintf("- P95: %.1f\n", dist.P95))
		sb.WriteString(fmt.Sprintf("- P99: %.1f\n", dist.P99))
		sb.WriteString(fmt.Sprintf("- Range: %.1f - %.1f\n\n", dist.Min, dist.Max))
	}

	sb.WriteString("## Functions to Review\n\n")
	sb.WriteString(fmt.Sprintf("Identified %d functions with high complexity:\n\n", len(functions)))

	for i, fn := range functions {
		sb.WriteString(fmt.Sprintf("### %d. %s.%s (Complexity: %d)\n\n",
			i+1, fn.Package, fn.Function, fn.Complexity))
		sb.WriteString(fmt.Sprintf("**Location:** %s:%d\n\n", fn.FilePath, fn.Line))
		sb.WriteString("**Code:**\n```go\n")
		sb.WriteString(fn.Body)
		sb.WriteString("\n```\n\n")
	}

	sb.WriteString("## Task\n\n")
	sb.WriteString("For each function above, evaluate:\n\n")
	sb.WriteString("1. **Is complexity inherent to the problem domain?**\n")
	sb.WriteString("   - Examples of justified complexity: parsers, state machines, protocol implementations\n")
	sb.WriteString("   - Look for: irreducible branching logic, necessary edge cases\n\n")
	sb.WriteString("2. **Can it be simplified via extraction?**\n")
	sb.WriteString("   - Extract helper functions\n")
	sb.WriteString("   - Table-driven approaches for branching\n")
	sb.WriteString("   - Strategy/polymorphism for complex conditionals\n\n")
	sb.WriteString("3. **Is it well-tested and documented?**\n")
	sb.WriteString("   - High complexity is more acceptable if thoroughly tested\n")
	sb.WriteString("   - Good documentation can mitigate complexity\n\n")

	sb.WriteString("## Response Format\n\n")
	sb.WriteString("Respond with JSON:\n")
	sb.WriteString("```json\n")
	sb.WriteString("{\n")
	sb.WriteString("  \"functions_to_refactor\": [\n")
	sb.WriteString("    {\n")
	sb.WriteString("      \"function\": \"Package.FunctionName\",\n")
	sb.WriteString("      \"file\": \"path/to/file.go\",\n")
	sb.WriteString("      \"line\": 123,\n")
	sb.WriteString("      \"complexity\": 45,\n")
	sb.WriteString("      \"reason\": \"Why refactoring is needed\",\n")
	sb.WriteString("      \"suggested_approach\": \"How to simplify it\"\n")
	sb.WriteString("    }\n")
	sb.WriteString("  ],\n")
	sb.WriteString("  \"acceptable_complexity\": [\n")
	sb.WriteString("    {\n")
	sb.WriteString("      \"function\": \"Package.FunctionName\",\n")
	sb.WriteString("      \"file\": \"path/to/file.go\",\n")
	sb.WriteString("      \"line\": 456,\n")
	sb.WriteString("      \"complexity\": 30,\n")
	sb.WriteString("      \"justification\": \"Why complexity is acceptable\",\n")
	sb.WriteString("      \"recommendation\": \"Optional suggestions (testing, docs, etc.)\"\n")
	sb.WriteString("    }\n")
	sb.WriteString("  ]\n")
	sb.WriteString("}\n")
	sb.WriteString("```\n")

	return sb.String()
}

// parseAIResponse extracts refactoring tasks from AI response.
func (m *ComplexityMonitor) parseAIResponse(response string) ([]DiscoveredIssue, error) {
	// Define response structure
	type ComplexityEvaluation struct {
		FunctionsToRefactor []struct {
			Function          string `json:"function"`
			File              string `json:"file"`
			Line              int    `json:"line"`
			Complexity        int    `json:"complexity"`
			Reason            string `json:"reason"`
			SuggestedApproach string `json:"suggested_approach"`
		} `json:"functions_to_refactor"`
		AcceptableComplexity []struct {
			Function       string `json:"function"`
			File           string `json:"file"`
			Line           int    `json:"line"`
			Complexity     int    `json:"complexity"`
			Justification  string `json:"justification"`
			Recommendation string `json:"recommendation"`
		} `json:"acceptable_complexity"`
	}

	// Use AI package's JSON parser with all cleanup strategies
	parseResult := ai.Parse[ComplexityEvaluation](response, ai.ParseOptions{
		Context: "complexity_evaluation",
	})

	if !parseResult.Success {
		return nil, fmt.Errorf("parsing AI response: %s", parseResult.Error)
	}

	result := parseResult.Data

	var issues []DiscoveredIssue

	// Create issues for functions that need refactoring
	for _, fn := range result.FunctionsToRefactor {
		issues = append(issues, DiscoveredIssue{
			FilePath:    fn.File,
			LineStart:   fn.Line,
			Category:    "complexity",
			Severity:    determineSeverity(fn.Complexity),
			Description: fmt.Sprintf("Refactor %s (complexity %d): %s", fn.Function, fn.Complexity, fn.Reason),
			Evidence: map[string]interface{}{
				"complexity":          fn.Complexity,
				"function":            fn.Function,
				"suggested_approach":  fn.SuggestedApproach,
			},
		})
	}

	// Log acceptable complexity (not creating issues, just noting in evidence)
	// This could be used for documentation or monitoring trends

	return issues, nil
}

// determineSeverity maps complexity to severity level.
func determineSeverity(complexity int) string {
	if complexity >= 50 {
		return "high"
	} else if complexity >= 30 {
		return "medium"
	}
	return "low"
}

// countUniqueFiles counts unique file paths in functions.
func countUniqueFiles(functions []*FunctionComplexity) int {
	fileSet := make(map[string]struct{})
	for _, fn := range functions {
		fileSet[fn.FilePath] = struct{}{}
	}
	return len(fileSet)
}

// calculateComplexityDistribution computes statistics for complexity values.
func calculateComplexityDistribution(functions []*FunctionComplexity) Distribution {
	if len(functions) == 0 {
		return Distribution{}
	}

	complexities := make([]float64, len(functions))
	for i, fn := range functions {
		complexities[i] = float64(fn.Complexity)
	}

	sort.Float64s(complexities)

	// Calculate statistics
	var sum float64
	for _, c := range complexities {
		sum += c
	}
	mean := sum / float64(len(complexities))

	// Standard deviation
	var variance float64
	for _, c := range complexities {
		diff := c - mean
		variance += diff * diff
	}
	stdDev := math.Sqrt(variance / float64(len(complexities)))

	// Median
	median := complexities[len(complexities)/2]

	// Percentiles
	p95Index := int(float64(len(complexities)) * 0.95)
	p99Index := int(float64(len(complexities)) * 0.99)
	if p95Index >= len(complexities) {
		p95Index = len(complexities) - 1
	}
	if p99Index >= len(complexities) {
		p99Index = len(complexities) - 1
	}

	return Distribution{
		Mean:   mean,
		Median: median,
		StdDev: stdDev,
		P95:    complexities[p95Index],
		P99:    complexities[p99Index],
		Min:    complexities[0],
		Max:    complexities[len(complexities)-1],
		Count:  len(complexities),
	}
}
