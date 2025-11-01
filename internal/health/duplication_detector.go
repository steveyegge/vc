package health

import (
	"bufio"
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/steveyegge/vc/internal/ai"
)

// DuplicationDetector identifies duplicate code blocks using simple token-based analysis
// and AI evaluation for extracting duplicates.
//
// ZFC Compliance: Collects duplicate blocks and overall percentage, then delegates
// judgment about which duplicates warrant extraction to AI supervisor.
type DuplicationDetector struct {
	// RootPath is the codebase root directory
	RootPath string

	// MinBlockSize is minimum lines to consider for duplication (default: 10)
	MinBlockSize int

	// FileExtensions to scan (default: [".go"])
	FileExtensions []string

	// ExcludePatterns for files/directories to skip
	ExcludePatterns []string

	// AI supervisor for evaluating duplicates
	Supervisor AISupervisor
}

// NewDuplicationDetector creates a duplication detector with sensible defaults.
func NewDuplicationDetector(rootPath string, supervisor AISupervisor) (*DuplicationDetector, error) {
	// Validate and clean the root path
	absPath, err := filepath.Abs(rootPath)
	if err != nil {
		return nil, fmt.Errorf("invalid root path %q: %w", rootPath, err)
	}

	return &DuplicationDetector{
		RootPath:     absPath,
		MinBlockSize: 10,
		FileExtensions: []string{".go"},
		ExcludePatterns: []string{
			"vendor/",
			".git/",
			"_test.go", // Skip tests for now (test duplication is often acceptable)
			".pb.go",   // Generated protobuf
			".gen.go",  // Other generated code
			"testdata/",
		},
		Supervisor: supervisor,
	}, nil
}

// Name implements HealthMonitor.
func (d *DuplicationDetector) Name() string {
	return "duplication_detector"
}

// Philosophy implements HealthMonitor.
func (d *DuplicationDetector) Philosophy() string {
	return "DRY (Don't Repeat Yourself) reduces maintenance burden. " +
		"However, some duplication is acceptable for clarity (test setup, simple logic, different contexts)."
}

// Schedule implements HealthMonitor.
func (d *DuplicationDetector) Schedule() ScheduleConfig {
	return ScheduleConfig{
		Type:        ScheduleHybrid,
		MinInterval: 24 * time.Hour,  // At least daily
		EventTrigger: "every_20_issues", // Or every 20 issues
	}
}

// Cost implements HealthMonitor.
func (d *DuplicationDetector) Cost() CostEstimate {
	return CostEstimate{
		EstimatedDuration: 10 * time.Second,
		AICallsEstimated:  1, // One call to evaluate duplicates
		RequiresFullScan:  true,
		Category:          CostExpensive, // Large context for AI
	}
}

// Check implements HealthMonitor.
func (d *DuplicationDetector) Check(ctx context.Context, codebase CodebaseContext) (*MonitorResult, error) {
	// Validate that AI supervisor is configured
	if d.Supervisor == nil {
		return nil, fmt.Errorf("AI supervisor is required for duplication detection")
	}

	startTime := time.Now()

	// 1. Scan codebase and find duplicate blocks
	files, totalLines, err := d.scanFiles(ctx)
	if err != nil {
		return nil, fmt.Errorf("scanning files: %w", err)
	}

	if len(files) == 0 {
		return &MonitorResult{
			IssuesFound: []DiscoveredIssue{},
			Context:     "No files found matching criteria",
			CheckedAt:   startTime,
			Stats: CheckStats{
				FilesScanned: 0,
				Duration:     time.Since(startTime),
			},
		}, nil
	}

	// 2. Find duplicate blocks
	duplicates := d.findDuplicateBlocks(files)

	// 3. Calculate duplication percentage
	duplicateLines := d.calculateDuplicateLines(duplicates)
	dupPercentage := 0.0
	if totalLines > 0 {
		dupPercentage = (float64(duplicateLines) / float64(totalLines)) * 100
	}

	if len(duplicates) == 0 {
		return &MonitorResult{
			IssuesFound: []DiscoveredIssue{},
			Context:     fmt.Sprintf("Scanned %d files (%d lines), no duplicates found", len(files), totalLines),
			CheckedAt:   startTime,
			Stats: CheckStats{
				FilesScanned: len(files),
				Duration:     time.Since(startTime),
			},
		}, nil
	}

	// 4. Build AI prompt and get evaluation
	evaluation, err := d.evaluateDuplicates(ctx, duplicates, dupPercentage, totalLines)
	if err != nil {
		return nil, fmt.Errorf("evaluating duplicates: %w", err)
	}

	// 5. Convert AI evaluation to DiscoveredIssues
	issues := d.buildIssues(evaluation)

	return &MonitorResult{
		IssuesFound: issues,
		Context:     d.buildContext(len(files), totalLines, dupPercentage, duplicates),
		Reasoning:   d.buildReasoning(evaluation, dupPercentage),
		CheckedAt:   startTime,
		Stats: CheckStats{
			FilesScanned: len(files),
			IssuesFound:  len(issues),
			Duration:     time.Since(startTime),
			AICallsMade:  1,
		},
	}, nil
}

// codeFile represents a scanned file with its lines
type codeFile struct {
	Path  string
	Lines []string
}

// duplicateBlock represents a duplicate code block
type duplicateBlock struct {
	Hash      string
	Lines     []string
	Locations []blockLocation
}

// blockLocation represents where a duplicate block appears
type blockLocation struct {
	File      string
	StartLine int
	EndLine   int
}

// scanFiles reads all matching files
func (d *DuplicationDetector) scanFiles(ctx context.Context) ([]codeFile, int, error) {
	var files []codeFile
	totalLines := 0

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

		// Skip excluded patterns
		relPath, err := filepath.Rel(d.RootPath, path)
		if err != nil {
			return nil
		}

		if ShouldExcludePath(relPath, info, d.ExcludePatterns) {
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
		for _, ext := range d.FileExtensions {
			if strings.HasSuffix(path, ext) {
				hasExt = true
				break
			}
		}
		if !hasExt {
			return nil
		}

		// Read file lines
		lines, err := d.readFileLines(path)
		if err != nil {
			// Log but don't fail on individual file errors
			return nil
		}

		files = append(files, codeFile{
			Path:  relPath,
			Lines: lines,
		})
		totalLines += len(lines)

		return nil
	})

	return files, totalLines, err
}

// readFileLines reads all lines from a file
func (d *DuplicationDetector) readFileLines(path string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()

	var lines []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	return lines, nil
}

// findDuplicateBlocks finds duplicate blocks using simple hashing
func (d *DuplicationDetector) findDuplicateBlocks(files []codeFile) []duplicateBlock {
	// Map of hash -> block with locations
	blockMap := make(map[string]*duplicateBlock)

	// Scan each file for blocks
	for _, file := range files {
		for i := 0; i+d.MinBlockSize <= len(file.Lines); i++ {
			// Extract block
			block := file.Lines[i : i+d.MinBlockSize]

			// Normalize and hash the block
			normalized := d.normalizeBlock(block)
			if len(normalized) == 0 {
				continue // Skip empty blocks
			}

			hash := d.hashBlock(normalized)

			// Track this occurrence
			if existing, ok := blockMap[hash]; ok {
				existing.Locations = append(existing.Locations, blockLocation{
					File:      file.Path,
					StartLine: i + 1, // 1-indexed
					EndLine:   i + d.MinBlockSize,
				})
			} else {
				blockMap[hash] = &duplicateBlock{
					Hash:  hash,
					Lines: block,
					Locations: []blockLocation{
						{
							File:      file.Path,
							StartLine: i + 1,
							EndLine:   i + d.MinBlockSize,
						},
					},
				}
			}
		}
	}

	// Filter to only duplicates (appears in 2+ locations)
	var duplicates []duplicateBlock
	for _, block := range blockMap {
		if len(block.Locations) >= 2 {
			duplicates = append(duplicates, *block)
		}
	}

	// Sort by number of occurrences (most duplicated first)
	sort.Slice(duplicates, func(i, j int) bool {
		return len(duplicates[i].Locations) > len(duplicates[j].Locations)
	})

	return duplicates
}

// normalizeBlock removes whitespace and comments for comparison
func (d *DuplicationDetector) normalizeBlock(lines []string) []string {
	var normalized []string
	for _, line := range lines {
		// Trim whitespace
		trimmed := strings.TrimSpace(line)

		// Skip empty lines
		if trimmed == "" {
			continue
		}

		// Skip comments
		if strings.HasPrefix(trimmed, "//") || strings.HasPrefix(trimmed, "/*") || strings.HasPrefix(trimmed, "*") {
			continue
		}

		normalized = append(normalized, trimmed)
	}
	return normalized
}

// hashBlock creates a hash of normalized lines
func (d *DuplicationDetector) hashBlock(lines []string) string {
	joined := strings.Join(lines, "\n")
	hash := sha256.Sum256([]byte(joined))
	return fmt.Sprintf("%x", hash[:8]) // Use first 8 bytes for shorter hash
}

// calculateDuplicateLines counts total duplicate lines
func (d *DuplicationDetector) calculateDuplicateLines(duplicates []duplicateBlock) int {
	total := 0
	for _, dup := range duplicates {
		// Count lines for each occurrence
		total += len(dup.Lines) * len(dup.Locations)
	}
	return total
}

// duplicationEvaluation is the AI's assessment of duplicates
type duplicationEvaluation struct {
	OverallAssessment  string                   `json:"overall_assessment"` // "acceptable" | "concerning" | "problematic"
	Reasoning          string                   `json:"reasoning"`
	DuplicatesToExtract []duplicateToExtract     `json:"duplicates_to_extract"`
	AcceptableDuplicates []acceptableDuplicate   `json:"acceptable_duplicates"`
}

type duplicateToExtract struct {
	Locations         []string `json:"locations"`
	Pattern           string   `json:"pattern"`
	SuggestedUtility  string   `json:"suggested_utility"`
	SuggestedLocation string   `json:"suggested_location"`
	Priority          string   `json:"priority"` // "P0" | "P1" | "P2" | "P3"
}

type acceptableDuplicate struct {
	Locations []string `json:"locations"`
	Reason    string   `json:"reason"`
}

// evaluateDuplicates uses AI to judge which duplicates should be extracted
func (d *DuplicationDetector) evaluateDuplicates(ctx context.Context, duplicates []duplicateBlock, dupPercentage float64, totalLines int) (*duplicationEvaluation, error) {
	// Limit duplicates sent to AI to prevent token limit issues
	const maxDuplicatesForAI = 10
	topDuplicates := duplicates
	if len(duplicates) > maxDuplicatesForAI {
		topDuplicates = duplicates[:maxDuplicatesForAI]
	}

	prompt := d.buildPrompt(topDuplicates, dupPercentage, totalLines)

	// Call AI supervisor
	response, err := d.Supervisor.CallAI(ctx, prompt, "duplication_evaluation", "", 4096)
	if err != nil {
		return nil, fmt.Errorf("AI call failed: %w", err)
	}

	// Parse JSON response using resilient parser
	parseResult := ai.Parse[duplicationEvaluation](response, ai.ParseOptions{
		Context: "duplication_evaluation",
	})

	if !parseResult.Success {
		return nil, fmt.Errorf("parsing AI response: %s", parseResult.Error)
	}

	return &parseResult.Data, nil
}

// buildPrompt creates the AI evaluation prompt
func (d *DuplicationDetector) buildPrompt(duplicates []duplicateBlock, dupPercentage float64, totalLines int) string {
	var sb strings.Builder

	sb.WriteString("# Code Duplication Analysis Request\n\n")
	sb.WriteString("## Philosophy\n")
	sb.WriteString(d.Philosophy())
	sb.WriteString("\n\n")

	sb.WriteString("## Codebase Statistics\n")
	sb.WriteString(fmt.Sprintf("- Total lines: %d\n", totalLines))
	sb.WriteString(fmt.Sprintf("- Duplication percentage: %.1f%%\n", dupPercentage))
	sb.WriteString(fmt.Sprintf("- Top duplicate blocks: %d\n", len(duplicates)))
	sb.WriteString("\n")

	// Use dynamic year to prevent prompt from becoming stale
	year := time.Now().Year()
	sb.WriteString(fmt.Sprintf("## Guidance (%d)\n", year))
	sb.WriteString("- 0-5% duplication: Excellent\n")
	sb.WriteString("- 5-10%: Good, monitor trends\n")
	sb.WriteString("- 10-20%: Review largest blocks\n")
	sb.WriteString("- >20%: Likely systematic issues\n\n")

	sb.WriteString("## Top Duplicate Blocks\n")
	for i, dup := range duplicates {
		sb.WriteString(fmt.Sprintf("\n### Duplicate %d (%d occurrences)\n", i+1, len(dup.Locations)))
		sb.WriteString("**Locations:**\n")
		for _, loc := range dup.Locations {
			sb.WriteString(fmt.Sprintf("- %s:%d-%d\n", loc.File, loc.StartLine, loc.EndLine))
		}
		sb.WriteString("\n**Code:**\n```go\n")
		// Show first few lines of the duplicate
		maxLinesToShow := 15
		linesToShow := dup.Lines
		if len(linesToShow) > maxLinesToShow {
			linesToShow = linesToShow[:maxLinesToShow]
			sb.WriteString(strings.Join(linesToShow, "\n"))
			sb.WriteString(fmt.Sprintf("\n... (%d more lines)", len(dup.Lines)-maxLinesToShow))
		} else {
			sb.WriteString(strings.Join(linesToShow, "\n"))
		}
		sb.WriteString("\n```\n")
	}

	sb.WriteString("\n## Your Task\n")
	sb.WriteString("For each duplicate block, determine if extraction is warranted or if duplication is acceptable.\n\n")

	sb.WriteString("Extract if:\n")
	sb.WriteString("- The pattern appears 3+ times\n")
	sb.WriteString("- It represents reusable logic\n")
	sb.WriteString("- Extraction improves maintainability\n\n")

	sb.WriteString("Accept if:\n")
	sb.WriteString("- Context-specific (test setup, different use cases)\n")
	sb.WriteString("- Simple/trivial code (error checks, basic validation)\n")
	sb.WriteString("- Extraction would reduce clarity\n\n")

	sb.WriteString("## Response Format\n")
	sb.WriteString("Return valid JSON:\n")
	sb.WriteString("```json\n")
	sb.WriteString("{\n")
	sb.WriteString("  \"overall_assessment\": \"acceptable\" | \"concerning\" | \"problematic\",\n")
	sb.WriteString("  \"reasoning\": \"Brief explanation of overall duplication level\",\n")
	sb.WriteString("  \"duplicates_to_extract\": [\n")
	sb.WriteString("    {\n")
	sb.WriteString("      \"locations\": [\"file1.go:45-67\", \"file2.go:123-145\"],\n")
	sb.WriteString("      \"pattern\": \"String truncation with UTF-8 safety\",\n")
	sb.WriteString("      \"suggested_utility\": \"safeTruncateString()\",\n")
	sb.WriteString("      \"suggested_location\": \"internal/utils/strings.go\",\n")
	sb.WriteString("      \"priority\": \"P2\"\n")
	sb.WriteString("    }\n")
	sb.WriteString("  ],\n")
	sb.WriteString("  \"acceptable_duplicates\": [\n")
	sb.WriteString("    {\n")
	sb.WriteString("      \"locations\": [\"test1_test.go:10-15\", \"test2_test.go:12-17\"],\n")
	sb.WriteString("      \"reason\": \"Test setup boilerplate, context-specific\"\n")
	sb.WriteString("    }\n")
	sb.WriteString("  ]\n")
	sb.WriteString("}\n")
	sb.WriteString("```\n")

	return sb.String()
}

// buildIssues converts AI evaluation to DiscoveredIssues
func (d *DuplicationDetector) buildIssues(eval *duplicationEvaluation) []DiscoveredIssue {
	var issues []DiscoveredIssue

	for _, extract := range eval.DuplicatesToExtract {
		// Determine severity based on priority
		severity := "medium"
		if extract.Priority == "P0" || extract.Priority == "P1" {
			severity = "high"
		} else if extract.Priority == "P3" {
			severity = "low"
		}

		// Build description
		description := fmt.Sprintf("Extract duplicated %s into utility function %s",
			extract.Pattern, extract.SuggestedUtility)

		issues = append(issues, DiscoveredIssue{
			Category:    "duplication",
			Severity:    severity,
			Description: description,
			Evidence: map[string]interface{}{
				"pattern":            extract.Pattern,
				"locations":          extract.Locations,
				"suggested_utility":  extract.SuggestedUtility,
				"suggested_location": extract.SuggestedLocation,
				"priority":           extract.Priority,
			},
		})
	}

	return issues
}

// buildContext creates context string for MonitorResult
func (d *DuplicationDetector) buildContext(numFiles int, totalLines int, dupPercentage float64, duplicates []duplicateBlock) string {
	return fmt.Sprintf(
		"Scanned %d files (%d lines). Found %d duplicate blocks. "+
			"Overall duplication: %.1f%%.",
		numFiles, totalLines, len(duplicates), dupPercentage,
	)
}

// buildReasoning creates reasoning string from AI evaluation
func (d *DuplicationDetector) buildReasoning(eval *duplicationEvaluation, dupPercentage float64) string {
	return fmt.Sprintf(
		"AI evaluation: %s (%.1f%% duplication). Identified %d blocks for extraction and %d acceptable duplicates. %s",
		eval.OverallAssessment, dupPercentage, len(eval.DuplicatesToExtract),
		len(eval.AcceptableDuplicates), eval.Reasoning,
	)
}
