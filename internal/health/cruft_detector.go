package health

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	// maxFilesForAI limits the number of cruft files sent to AI evaluation
	// to prevent token limit errors and excessive API costs.
	// This matches FileSizeMonitor's limit for consistency.
	maxFilesForAI = 50
)

// CruftDetector identifies backup files, temp files, and other development
// artifacts that shouldn't be in source control.
//
// ZFC Compliance: Collects files matching cruft patterns, then delegates
// judgment to AI supervisor to distinguish true cruft from legitimate files.
type CruftDetector struct {
	// RootPath is the codebase root directory
	RootPath string

	// CruftPatterns are file patterns that may indicate cruft
	CruftPatterns []string

	// ExcludePatterns for files/directories to skip
	ExcludePatterns []string

	// MinimumCruftThreshold - only file issue if this many cruft files found
	MinimumCruftThreshold int

	// AI supervisor for evaluating files
	Supervisor AISupervisor
}

// NewCruftDetector creates a cruft detector with sensible defaults.
func NewCruftDetector(rootPath string, supervisor AISupervisor) (*CruftDetector, error) {
	// Validate and clean the root path
	absPath, err := filepath.Abs(rootPath)
	if err != nil {
		return nil, fmt.Errorf("invalid root path %q: %w", rootPath, err)
	}

	return &CruftDetector{
		RootPath: absPath,
		CruftPatterns: []string{
			"*.bak",      // Backup files
			"*.tmp",      // Temporary files
			"*.temp",     // Temporary files
			"*.old",      // Old versions
			"*_backup.*", // Naming pattern: foo_backup.go
			"*_old.*",    // Naming pattern: foo_old.go
			"*.swp",      // Vim swap files
			"*.swo",      // Vim swap files
			"*~",         // Editor backup files
			".DS_Store",  // macOS cruft
			"Thumbs.db",  // Windows cruft
		},
		ExcludePatterns: []string{
			"vendor/",
			".git/",
			"testdata/",    // Test fixtures are legitimate
			"node_modules/",
			".beads/",      // Issue tracker database
		},
		MinimumCruftThreshold: 3, // Only file issue if ≥3 files found
		Supervisor:            supervisor,
	}, nil
}

// Name implements HealthMonitor.
func (d *CruftDetector) Name() string {
	return "cruft_detector"
}

// Philosophy implements HealthMonitor.
func (d *CruftDetector) Philosophy() string {
	return "Development artifacts (backups, temp files, archives) should not be " +
		"committed to source control. Git provides versioning and backup."
}

// Schedule implements HealthMonitor.
func (d *CruftDetector) Schedule() ScheduleConfig {
	return ScheduleConfig{
		Type:     ScheduleTimeBased,
		Interval: 7 * 24 * time.Hour, // Weekly
	}
}

// Cost implements HealthMonitor.
func (d *CruftDetector) Cost() CostEstimate {
	return CostEstimate{
		EstimatedDuration: 2 * time.Second,
		AICallsEstimated:  1, // One call to categorize all files
		RequiresFullScan:  true,
		Category:          CostCheap,
	}
}

// Check implements HealthMonitor.
func (d *CruftDetector) Check(ctx context.Context, codebase CodebaseContext) (*MonitorResult, error) {
	// Validate that AI supervisor is configured
	if d.Supervisor == nil {
		return nil, fmt.Errorf("AI supervisor is required for cruft detection")
	}

	startTime := time.Now()

	// 1. Scan codebase for files matching cruft patterns
	cruftFiles, err := d.scanFiles(ctx)
	if err != nil {
		return nil, fmt.Errorf("scanning files: %w", err)
	}

	// 2. If below threshold, no action needed
	if len(cruftFiles) < d.MinimumCruftThreshold {
		return &MonitorResult{
			IssuesFound: []DiscoveredIssue{},
			Context:     fmt.Sprintf("Found %d potential cruft files (below threshold of %d)", len(cruftFiles), d.MinimumCruftThreshold),
			CheckedAt:   time.Now(),
			Stats: CheckStats{
				FilesScanned: len(cruftFiles),
				Duration:     time.Since(startTime),
			},
		}, nil
	}

	// 3. Ask AI to categorize the files
	evaluation, err := d.evaluateCruft(ctx, cruftFiles)
	if err != nil {
		return nil, fmt.Errorf("evaluating cruft: %w", err)
	}

	// 4. Build issues from AI evaluation
	issues := d.buildIssues(evaluation)

	return &MonitorResult{
		IssuesFound: issues,
		Context:     d.buildContext(cruftFiles, evaluation),
		Reasoning:   d.buildReasoning(evaluation),
		CheckedAt:   time.Now(),
		Stats: CheckStats{
			FilesScanned: len(cruftFiles),
			IssuesFound:  len(issues),
			Duration:     time.Since(startTime),
			AICallsMade:  1,
		},
	}, nil
}

// cruftFile represents a file that matches cruft patterns.
type cruftFile struct {
	Path    string
	Pattern string // Which pattern matched
}

// scanFiles walks the directory tree and finds files matching cruft patterns.
func (d *CruftDetector) scanFiles(ctx context.Context) ([]cruftFile, error) {
	var files []cruftFile

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
		for _, pattern := range d.ExcludePatterns {
			matched := false
			if strings.HasPrefix(relPath, pattern) {
				matched = true
			} else if strings.Contains(relPath, "/"+pattern) {
				matched = true
			} else if strings.HasSuffix(relPath, pattern) {
				matched = true
			}

			if matched {
				if info.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
		}

		// Only process files (not directories)
		if info.IsDir() {
			return nil
		}

		// Check if file matches any cruft pattern
		fileName := filepath.Base(path)
		for _, pattern := range d.CruftPatterns {
			matched, err := filepath.Match(pattern, fileName)
			if err != nil {
				// Invalid pattern, skip it
				continue
			}
			if matched {
				files = append(files, cruftFile{
					Path:    relPath,
					Pattern: pattern,
				})
				break // Only record once per file
			}
		}

		return nil
	})

	return files, err
}

// cruftEvaluation is the AI's categorization of cruft files.
type cruftEvaluation struct {
	CruftToDelete     []cruftFileAction `json:"cruft_to_delete"`
	PatternsToIgnore  []string          `json:"patterns_to_ignore"`
	LegitimateFiles   []legitimateFile  `json:"legitimate_files"`
	Reasoning         string            `json:"reasoning"`
}

type cruftFileAction struct {
	File        string `json:"file"`
	Reason      string `json:"reason"`
	Pattern     string `json:"pattern"`
}

type legitimateFile struct {
	File          string `json:"file"`
	Justification string `json:"justification"`
}

// evaluateCruft uses AI to categorize cruft files.
func (d *CruftDetector) evaluateCruft(ctx context.Context, files []cruftFile) (*cruftEvaluation, error) {
	// Limit files sent to AI to prevent token limit errors
	filesToEvaluate := files
	if len(files) > maxFilesForAI {
		filesToEvaluate = files[:maxFilesForAI]
	}

	prompt := d.buildPrompt(filesToEvaluate)

	// Call AI supervisor
	response, err := d.Supervisor.CallAI(ctx, prompt, "cruft_evaluation", "", 4096)
	if err != nil {
		return nil, fmt.Errorf("AI call failed: %w", err)
	}

	// Parse JSON response
	var eval cruftEvaluation
	if err := json.Unmarshal([]byte(response), &eval); err != nil {
		// Truncate response in error message to avoid huge logs
		truncated := response
		if len(response) > 500 {
			truncated = response[:500] + "... (truncated)"
		}
		return nil, fmt.Errorf("parsing AI response: %w (response: %s)", err, truncated)
	}

	return &eval, nil
}

// buildPrompt creates the AI evaluation prompt.
func (d *CruftDetector) buildPrompt(files []cruftFile) string {
	var sb strings.Builder

	sb.WriteString("# Cruft Detection Request\n\n")
	sb.WriteString("## Philosophy\n")
	sb.WriteString(d.Philosophy())
	sb.WriteString("\n\n")

	sb.WriteString("## Files Matching Cruft Patterns\n")
	sb.WriteString(fmt.Sprintf("Found %d files matching cruft patterns:\n\n", len(files)))
	for _, f := range files {
		sb.WriteString(fmt.Sprintf("- %s (pattern: %s)\n", f.Path, f.Pattern))
	}
	sb.WriteString("\n")

	// Use dynamic year to prevent prompt from becoming stale
	year := time.Now().Year()
	sb.WriteString(fmt.Sprintf("## Guidance (%d)\n", year))
	sb.WriteString("Common cruft patterns:\n")
	sb.WriteString("- Editor backups: *.bak, *~, *.swp\n")
	sb.WriteString("- Temporary files: *.tmp, *.temp\n")
	sb.WriteString("- Old versions: *.old, *_backup.*, *_old.*\n")
	sb.WriteString("- OS artifacts: .DS_Store, Thumbs.db\n\n")

	sb.WriteString("However, some files that match these patterns may be legitimate:\n")
	sb.WriteString("- Test fixtures in testdata/ (already excluded)\n")
	sb.WriteString("- Files with semantic meaning (e.g., 'restore_backup.go' is code, not cruft)\n")
	sb.WriteString("- Configuration examples (e.g., 'config.bak.example')\n\n")

	sb.WriteString("## Your Task\n")
	sb.WriteString("Categorize each file into one of three categories:\n\n")

	sb.WriteString("1. **Cruft to delete**: True development artifacts with no value\n")
	sb.WriteString("   - Editor backups of source files\n")
	sb.WriteString("   - Temporary files from builds or tests\n")
	sb.WriteString("   - Accidental OS files (.DS_Store, etc.)\n\n")

	sb.WriteString("2. **Patterns to add to .gitignore**: Prevent future occurrences\n")
	sb.WriteString("   - Generic patterns like *.bak, *.tmp\n")
	sb.WriteString("   - Only suggest if not already in common .gitignore templates\n\n")

	sb.WriteString("3. **Legitimate files**: Not actually cruft\n")
	sb.WriteString("   - Code that happens to match pattern (restore_backup.go)\n")
	sb.WriteString("   - Documentation or examples\n")
	sb.WriteString("   - Test fixtures (though these should be in testdata/)\n\n")

	sb.WriteString("## Response Format\n")
	sb.WriteString("Return valid JSON:\n")
	sb.WriteString("```json\n")
	sb.WriteString("{\n")
	sb.WriteString("  \"cruft_to_delete\": [\n")
	sb.WriteString("    {\n")
	sb.WriteString("      \"file\": \"path/to/file.bak\",\n")
	sb.WriteString("      \"reason\": \"Editor backup of source file\",\n")
	sb.WriteString("      \"pattern\": \"*.bak\"\n")
	sb.WriteString("    }\n")
	sb.WriteString("  ],\n")
	sb.WriteString("  \"patterns_to_ignore\": [\n")
	sb.WriteString("    \"*.bak\",\n")
	sb.WriteString("    \"*.swp\"\n")
	sb.WriteString("  ],\n")
	sb.WriteString("  \"legitimate_files\": [\n")
	sb.WriteString("    {\n")
	sb.WriteString("      \"file\": \"internal/backup/restore.go\",\n")
	sb.WriteString("      \"justification\": \"Source code for backup functionality, not a backup file\"\n")
	sb.WriteString("    }\n")
	sb.WriteString("  ],\n")
	sb.WriteString("  \"reasoning\": \"Brief explanation of overall categorization\"\n")
	sb.WriteString("}\n")
	sb.WriteString("```\n")

	return sb.String()
}

// buildIssues converts AI evaluation to DiscoveredIssues.
func (d *CruftDetector) buildIssues(eval *cruftEvaluation) []DiscoveredIssue {
	var issues []DiscoveredIssue

	// If we have cruft to delete or patterns to add to .gitignore, file a single grouped issue
	if len(eval.CruftToDelete) > 0 || len(eval.PatternsToIgnore) > 0 {
		description := "Clean up development artifacts and prevent future cruft"

		evidence := map[string]interface{}{
			"cruft_to_delete":    eval.CruftToDelete,
			"patterns_to_ignore": eval.PatternsToIgnore,
			"reasoning":          eval.Reasoning,
			"files_to_delete":    len(eval.CruftToDelete),
			"patterns_to_add":    len(eval.PatternsToIgnore),
		}

		// Build a detailed description
		var detailParts []string
		if len(eval.CruftToDelete) > 0 {
			detailParts = append(detailParts, fmt.Sprintf("%d files to delete", len(eval.CruftToDelete)))
		}
		if len(eval.PatternsToIgnore) > 0 {
			detailParts = append(detailParts, fmt.Sprintf("%d patterns to add to .gitignore", len(eval.PatternsToIgnore)))
		}

		if len(detailParts) > 0 {
			description = fmt.Sprintf("Clean up cruft: %s", strings.Join(detailParts, ", "))
		}

		// Calculate severity based on total work needed
		// Weight deletions higher than patterns (deletions are more urgent)
		weightedWork := len(eval.CruftToDelete) + (len(eval.PatternsToIgnore) / 2)

		issues = append(issues, DiscoveredIssue{
			Category:    "cruft",
			Severity:    d.calculateSeverity(weightedWork),
			Description: description,
			Evidence:    evidence,
		})
	}

	return issues
}

// calculateSeverity determines issue severity based on weighted work count.
// The work count includes both files to delete and patterns to add (weighted).
func (d *CruftDetector) calculateSeverity(workCount int) string {
	if workCount >= 20 {
		return "high"
	} else if workCount >= 10 {
		return "medium"
	}
	return "low"
}

// buildContext creates context string for MonitorResult.
func (d *CruftDetector) buildContext(files []cruftFile, eval *cruftEvaluation) string {
	return fmt.Sprintf(
		"Scanned codebase and found %d files matching cruft patterns. "+
			"AI categorized %d as true cruft, %d as legitimate files. "+
			"Recommends adding %d patterns to .gitignore.",
		len(files), len(eval.CruftToDelete), len(eval.LegitimateFiles), len(eval.PatternsToIgnore),
	)
}

// buildReasoning creates reasoning string from AI evaluation.
func (d *CruftDetector) buildReasoning(eval *cruftEvaluation) string {
	if eval.Reasoning != "" {
		return eval.Reasoning
	}
	return fmt.Sprintf(
		"Identified %d development artifacts to remove and %d .gitignore patterns to add.",
		len(eval.CruftToDelete), len(eval.PatternsToIgnore),
	)
}
