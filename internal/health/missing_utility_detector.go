package health

import (
	"context"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/steveyegge/vc/internal/ai"
)

// MissingUtilityDetector identifies repeated code patterns that should be extracted
// into reusable utility functions.
//
// ZFC Compliance: Collects code samples and existing utilities, then delegates
// judgment about which patterns warrant extraction to AI supervisor.
type MissingUtilityDetector struct {
	// RootPath is the codebase root directory
	RootPath string

	// FileExtensions to scan (default: [".go"])
	FileExtensions []string

	// ExcludePatterns for files/directories to skip
	ExcludePatterns []string

	// AI supervisor for identifying missing utilities
	Supervisor AISupervisor

	// MaxSamples is the maximum number of file snippets to send to AI (default: 15)
	MaxSamples int

	// SnippetLines is the number of lines per snippet (default: 50)
	SnippetLines int
}

// NewMissingUtilityDetector creates a missing utility detector with sensible defaults.
func NewMissingUtilityDetector(rootPath string, supervisor AISupervisor) (*MissingUtilityDetector, error) {
	// Validate and clean the root path
	absPath, err := filepath.Abs(rootPath)
	if err != nil {
		return nil, fmt.Errorf("invalid root path %q: %w", rootPath, err)
	}

	return &MissingUtilityDetector{
		RootPath:     absPath,
		FileExtensions: []string{".go"},
		ExcludePatterns: []string{
			"vendor/",
			".git/",
			"_test.go", // Skip tests (test utilities are separate concern)
			".pb.go",   // Generated protobuf
			".gen.go",  // Other generated code
			"testdata/",
		},
		Supervisor:   supervisor,
		MaxSamples:   15,
		SnippetLines: 50,
	}, nil
}

// Name implements HealthMonitor.
func (m *MissingUtilityDetector) Name() string {
	return "missing_utility_detector"
}

// Philosophy implements HealthMonitor.
func (m *MissingUtilityDetector) Philosophy() string {
	return "When the same pattern appears multiple times, it indicates a missing abstraction. " +
		"Extract utilities to reduce duplication and centralize behavior. " +
		"However, not all repetition is bad - simple operations and context-specific logic " +
		"can be repeated without harm."
}

// Schedule implements HealthMonitor.
func (m *MissingUtilityDetector) Schedule() ScheduleConfig {
	return ScheduleConfig{
		Type:         ScheduleEventBased,
		EventTrigger: "every_50_issues", // Run every 50 issues as per design
	}
}

// Cost implements HealthMonitor.
func (m *MissingUtilityDetector) Cost() CostEstimate {
	return CostEstimate{
		EstimatedDuration: 30 * time.Second, // Large context, AI analysis
		AICallsEstimated:  1,
		RequiresFullScan:  false, // Uses sampling
		Category:          CostExpensive,
	}
}

// Check implements HealthMonitor.
func (m *MissingUtilityDetector) Check(ctx context.Context, codebase CodebaseContext) (*MonitorResult, error) {
	// Validate that AI supervisor is configured
	if m.Supervisor == nil {
		return nil, fmt.Errorf("AI supervisor is required for missing utility detection")
	}

	startTime := time.Now()

	// 1. Find existing utility files to learn what's already available
	existingUtils, err := m.scanExistingUtilities(ctx)
	if err != nil {
		return nil, fmt.Errorf("scanning existing utilities: %w", err)
	}

	// 2. Sample random code files for analysis
	samples, err := m.sampleCodeFiles(ctx)
	if err != nil {
		return nil, fmt.Errorf("sampling code files: %w", err)
	}

	if len(samples) == 0 {
		return &MonitorResult{
			IssuesFound: []DiscoveredIssue{},
			Context:     "No code files found for sampling",
			CheckedAt:   startTime,
			Stats: CheckStats{
				FilesScanned: 0,
				Duration:     time.Since(startTime),
			},
		}, nil
	}

	// 3. Build AI prompt with existing utilities and code samples
	prompt := m.buildPrompt(existingUtils, samples)

	// 4. Call AI to identify missing utilities
	response, err := m.Supervisor.CallAI(ctx, prompt, "missing_utility_analysis", "", 8192)
	if err != nil {
		return nil, fmt.Errorf("AI call failed: %w", err)
	}

	// 5. Parse AI response
	parseResult := ai.Parse[missingUtilityAnalysis](response, ai.ParseOptions{
		Context: "missing_utility_analysis",
	})

	if !parseResult.Success {
		return nil, fmt.Errorf("parsing AI response: %s", parseResult.Error)
	}

	analysis := &parseResult.Data

	// 6. Convert to DiscoveredIssues
	issues := m.buildIssues(analysis)

	return &MonitorResult{
		IssuesFound: issues,
		Context:     m.buildContext(len(samples), len(existingUtils), len(analysis.MissingUtilities)),
		Reasoning:   analysis.OverallAssessment,
		CheckedAt:   startTime,
		Stats: CheckStats{
			FilesScanned: len(samples),
			IssuesFound:  len(issues),
			Duration:     time.Since(startTime),
			AICallsMade:  1,
		},
	}, nil
}

// existingUtility represents a utility function that already exists
type existingUtility struct {
	Name     string
	Location string
	Purpose  string // Inferred from function name/comments
}

// codeSample represents a snippet of code for AI analysis
type codeSample struct {
	FilePath string
	StartLine int
	Lines    []string
}

// missingUtilityAnalysis is the AI's assessment of missing utilities
type missingUtilityAnalysis struct {
	OverallAssessment   string             `json:"overall_assessment"`
	MissingUtilities    []missingUtility   `json:"missing_utilities"`
	AcceptableRepetition []acceptablePattern `json:"acceptable_repetition"`
}

type missingUtility struct {
	Pattern            string   `json:"pattern"`
	Occurrences        []string `json:"occurrences"`
	SuggestedName      string   `json:"suggested_name"`
	SuggestedLocation  string   `json:"suggested_location"`
	Justification      string   `json:"justification"`
	Priority           string   `json:"priority"` // "P0" | "P1" | "P2" | "P3"
}

type acceptablePattern struct {
	Pattern string `json:"pattern"`
	Reason  string `json:"reason"`
}

// scanExistingUtilities finds utility functions that already exist
func (m *MissingUtilityDetector) scanExistingUtilities(ctx context.Context) ([]existingUtility, error) {
	var utils []existingUtility

	// Look for common utility locations
	utilDirs := []string{
		"internal/utils",
		"internal/util",
		"pkg/utils",
		"pkg/util",
		"internal/health/utils.go", // Health package utilities
	}

	for _, relPath := range utilDirs {
		fullPath := filepath.Join(m.RootPath, relPath)

		// Check if path exists
		info, err := os.Stat(fullPath)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			continue // Skip on error
		}

		// Check context cancellation
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		if info.IsDir() {
			// Scan directory for .go files
			dirUtils, err := m.scanUtilityDir(ctx, fullPath)
			if err != nil {
				continue // Skip on error
			}
			utils = append(utils, dirUtils...)
		} else if strings.HasSuffix(fullPath, ".go") {
			// Single file
			fileUtils, err := m.scanUtilityFile(fullPath, relPath)
			if err != nil {
				continue // Skip on error
			}
			utils = append(utils, fileUtils...)
		}
	}

	return utils, nil
}

// scanUtilityDir scans a directory for utility functions
func (m *MissingUtilityDetector) scanUtilityDir(ctx context.Context, dirPath string) ([]existingUtility, error) {
	var utils []existingUtility

	err := filepath.Walk(dirPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // Skip on error
		}

		// Check context cancellation
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		if info.IsDir() || !strings.HasSuffix(path, ".go") {
			return nil
		}

		fileRelPath, err := filepath.Rel(m.RootPath, path)
		if err != nil {
			fileRelPath = path
		}

		fileUtils, err := m.scanUtilityFile(path, fileRelPath)
		if err != nil {
			return nil // Skip on error
		}

		utils = append(utils, fileUtils...)
		return nil
	})

	return utils, err
}

// scanUtilityFile extracts utility function names from a file
func (m *MissingUtilityDetector) scanUtilityFile(filePath, relPath string) ([]existingUtility, error) {
	content, err := os.ReadFile(filePath)
	if err != nil {
		return nil, err
	}

	var utils []existingUtility
	lines := strings.Split(string(content), "\n")

	// Simple heuristic: look for exported functions (starts with "func ")
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "func ") {
			// Extract function name
			funcDecl := strings.TrimPrefix(trimmed, "func ")

			// Handle methods: func (r *Receiver) Name(...) -> skip
			if strings.HasPrefix(funcDecl, "(") {
				continue
			}

			// Extract name before '('
			nameEnd := strings.Index(funcDecl, "(")
			if nameEnd > 0 {
				funcName := funcDecl[:nameEnd]
				utils = append(utils, existingUtility{
					Name:     funcName,
					Location: relPath,
					Purpose:  "", // Could extract from comments if needed
				})
			}
		}
	}

	return utils, nil
}

// sampleCodeFiles randomly samples code files for AI analysis
func (m *MissingUtilityDetector) sampleCodeFiles(ctx context.Context) ([]codeSample, error) {
	// 1. Find all matching files
	var allFiles []string

	err := filepath.Walk(m.RootPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Check context cancellation
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		// Skip excluded patterns
		relPath, err := filepath.Rel(m.RootPath, path)
		if err != nil {
			return nil
		}

		if ShouldExcludePath(relPath, info, m.ExcludePatterns) {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		// Only process files with matching extensions
		if info.IsDir() {
			return nil
		}

		hasExt := false
		for _, ext := range m.FileExtensions {
			if strings.HasSuffix(path, ext) {
				hasExt = true
				break
			}
		}
		if !hasExt {
			return nil
		}

		allFiles = append(allFiles, path)
		return nil
	})

	if err != nil {
		return nil, err
	}

	if len(allFiles) == 0 {
		return nil, nil
	}

	// 2. Randomly sample files
	numSamples := m.MaxSamples
	if len(allFiles) < numSamples {
		numSamples = len(allFiles)
	}

	// Shuffle and take first N
	rand.Shuffle(len(allFiles), func(i, j int) {
		allFiles[i], allFiles[j] = allFiles[j], allFiles[i]
	})

	sampledFiles := allFiles[:numSamples]

	// 3. Extract snippets from each file
	var samples []codeSample
	for _, filePath := range sampledFiles {
		relPath, err := filepath.Rel(m.RootPath, filePath)
		if err != nil {
			relPath = filePath
		}

		snippet, err := m.extractSnippet(filePath, relPath)
		if err != nil {
			continue // Skip on error
		}

		if snippet != nil {
			samples = append(samples, *snippet)
		}
	}

	return samples, nil
}

// extractSnippet reads a file and extracts a representative snippet
func (m *MissingUtilityDetector) extractSnippet(filePath, relPath string) (*codeSample, error) {
	content, err := os.ReadFile(filePath)
	if err != nil {
		return nil, err
	}

	lines := strings.Split(string(content), "\n")

	// If file is small, take the whole thing
	if len(lines) <= m.SnippetLines {
		return &codeSample{
			FilePath:  relPath,
			StartLine: 1,
			Lines:     lines,
		}, nil
	}

	// Take a random snippet from the middle of the file
	// (Skip package declaration and imports at top)
	minStart := 20 // Skip first 20 lines (package, imports)
	if len(lines)-m.SnippetLines < minStart {
		minStart = 0
	}

	maxStart := len(lines) - m.SnippetLines
	if maxStart < minStart {
		maxStart = minStart
	}

	startLine := minStart
	if maxStart > minStart {
		startLine = rand.Intn(maxStart-minStart) + minStart
	}

	snippet := lines[startLine : startLine+m.SnippetLines]

	return &codeSample{
		FilePath:  relPath,
		StartLine: startLine + 1, // 1-indexed
		Lines:     snippet,
	}, nil
}

// buildPrompt creates the AI analysis prompt
func (m *MissingUtilityDetector) buildPrompt(existingUtils []existingUtility, samples []codeSample) string {
	var sb strings.Builder

	sb.WriteString("# Missing Utility Detection\n\n")

	sb.WriteString("## Philosophy\n")
	sb.WriteString(m.Philosophy())
	sb.WriteString("\n\n")

	// Use dynamic year to prevent prompt from becoming stale
	year := time.Now().Year()
	sb.WriteString(fmt.Sprintf("## Guidance (%d)\n", year))
	sb.WriteString("**Extract when:**\n")
	sb.WriteString("- Pattern appears 3+ times\n")
	sb.WriteString("- Logic is non-trivial (>5 lines)\n")
	sb.WriteString("- Behavior should be consistent across uses\n")
	sb.WriteString("- Pattern has clear semantic meaning\n\n")

	sb.WriteString("**Don't extract:**\n")
	sb.WriteString("- Simple operations (x == nil checks)\n")
	sb.WriteString("- Context-dependent logic\n")
	sb.WriteString("- Test boilerplate\n\n")

	sb.WriteString("## Existing Utilities\n")
	if len(existingUtils) == 0 {
		sb.WriteString("(No utility functions found - this may be a new codebase)\n\n")
	} else {
		sb.WriteString("The following utility functions already exist. Do NOT suggest duplicates:\n\n")
		for _, util := range existingUtils {
			sb.WriteString(fmt.Sprintf("- `%s` in %s\n", util.Name, util.Location))
		}
		sb.WriteString("\n")
	}

	sb.WriteString("## Code Samples\n")
	sb.WriteString(fmt.Sprintf("Analyzing %d random code samples for repeated patterns:\n\n", len(samples)))

	for i, sample := range samples {
		sb.WriteString(fmt.Sprintf("### Sample %d: %s (starting at line %d)\n", i+1, sample.FilePath, sample.StartLine))
		sb.WriteString("```go\n")

		// Limit lines to prevent token overflow
		maxLines := 40
		linesToShow := sample.Lines
		if len(linesToShow) > maxLines {
			linesToShow = linesToShow[:maxLines]
			sb.WriteString(strings.Join(linesToShow, "\n"))
			sb.WriteString(fmt.Sprintf("\n... (%d more lines)", len(sample.Lines)-maxLines))
		} else {
			sb.WriteString(strings.Join(linesToShow, "\n"))
		}

		sb.WriteString("\n```\n\n")
	}

	sb.WriteString("## Your Task\n")
	sb.WriteString("Analyze these code samples to identify:\n")
	sb.WriteString("1. Repeated patterns that appear across multiple files\n")
	sb.WriteString("2. Operations that should be extracted into utilities\n")
	sb.WriteString("3. Common sequences that lack abstraction\n\n")

	sb.WriteString("For each missing utility, suggest:\n")
	sb.WriteString("- A descriptive function name\n")
	sb.WriteString("- Where it should live (e.g., internal/utils/strings.go)\n")
	sb.WriteString("- Why extraction is warranted\n")
	sb.WriteString("- Priority (P0-P3)\n\n")

	sb.WriteString("## Response Format\n")
	sb.WriteString("Return valid JSON:\n")
	sb.WriteString("```json\n")
	sb.WriteString("{\n")
	sb.WriteString("  \"overall_assessment\": \"Brief summary of code quality and duplication level\",\n")
	sb.WriteString("  \"missing_utilities\": [\n")
	sb.WriteString("    {\n")
	sb.WriteString("      \"pattern\": \"String truncation with UTF-8 safety\",\n")
	sb.WriteString("      \"occurrences\": [\n")
	sb.WriteString("        \"internal/ai/utils.go:222-247\",\n")
	sb.WriteString("        \"internal/health/duplication_detector.go:357-382\"\n")
	sb.WriteString("      ],\n")
	sb.WriteString("      \"suggested_name\": \"SafeTruncateString\",\n")
	sb.WriteString("      \"suggested_location\": \"internal/utils/strings.go\",\n")
	sb.WriteString("      \"justification\": \"Repeated 3 times, non-trivial UTF-8 handling logic\",\n")
	sb.WriteString("      \"priority\": \"P2\"\n")
	sb.WriteString("    }\n")
	sb.WriteString("  ],\n")
	sb.WriteString("  \"acceptable_repetition\": [\n")
	sb.WriteString("    {\n")
	sb.WriteString("      \"pattern\": \"nil check and early return\",\n")
	sb.WriteString("      \"reason\": \"Too simple to warrant utility function\"\n")
	sb.WriteString("    }\n")
	sb.WriteString("  ]\n")
	sb.WriteString("}\n")
	sb.WriteString("```\n")

	return sb.String()
}

// buildIssues converts AI analysis to DiscoveredIssues
func (m *MissingUtilityDetector) buildIssues(analysis *missingUtilityAnalysis) []DiscoveredIssue {
	var issues []DiscoveredIssue

	for _, util := range analysis.MissingUtilities {
		// Determine severity based on priority
		var severity string
		switch util.Priority {
		case "P0", "P1":
			severity = "high"
		case "P3":
			severity = "low"
		default:
			severity = "medium"
		}

		// Build description
		description := fmt.Sprintf("Extract utility for %s: %s",
			util.Pattern, util.SuggestedName)

		issues = append(issues, DiscoveredIssue{
			Category:    "missing_utility",
			Severity:    severity,
			Description: description,
			Evidence: map[string]interface{}{
				"pattern":            util.Pattern,
				"occurrences":        util.Occurrences,
				"suggested_name":     util.SuggestedName,
				"suggested_location": util.SuggestedLocation,
				"justification":      util.Justification,
				"priority":           util.Priority,
			},
		})
	}

	return issues
}

// buildContext creates context string for MonitorResult
func (m *MissingUtilityDetector) buildContext(numSamples, numExistingUtils, numMissing int) string {
	return fmt.Sprintf(
		"Analyzed %d code samples. Found %d existing utilities. Identified %d missing abstractions.",
		numSamples, numExistingUtils, numMissing,
	)
}
