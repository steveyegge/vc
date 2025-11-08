package health

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/steveyegge/vc/internal/ai"
)

// GitignoreDetector identifies files tracked in git that should be in .gitignore.
//
// ZFC Compliance: Collects files matching common gitignore patterns, then delegates
// judgment to AI supervisor to distinguish true violations from legitimate files.
type GitignoreDetector struct {
	// RootPath is the codebase root directory
	RootPath string

	// GitignorePatterns are patterns that commonly should be ignored
	GitignorePatterns map[string][]string // category -> patterns

	// MinimumViolationThreshold - only file issue if this many violations found
	MinimumViolationThreshold int

	// AI supervisor for evaluating files
	Supervisor AISupervisor
}

// NewGitignoreDetector creates a gitignore detector with sensible defaults.
func NewGitignoreDetector(rootPath string, supervisor AISupervisor) (*GitignoreDetector, error) {
	// Validate and clean the root path
	absPath, err := filepath.Abs(rootPath)
	if err != nil {
		return nil, fmt.Errorf("invalid root path %q: %w", rootPath, err)
	}

	return &GitignoreDetector{
		RootPath: absPath,
		GitignorePatterns: map[string][]string{
			"secrets": {
				".env",
				".env.*",
				"*.pem",
				"*.key",
				"*.p12",
				"*.pfx",
				"credentials.json",
				"secrets.yaml",
				"secrets.yml",
				"*_rsa",
				"*_dsa",
				"*_ecdsa",
				"*_ed25519",
				"id_rsa*",
				"id_dsa*",
				"*.cer",
				"*.der",
				"*.crt",
			},
			"build_artifacts": {
				"*.o",
				"*.so",
				"*.dylib",
				"*.dll",
				"*.exe",
				"*.out",
				"*.a",
				"*.lib",
				"dist/",
				"build/",
				"target/",
				"bin/",
				"obj/",
				"*.pyc",
				"*.pyo",
				"__pycache__/",
				"*.class",
				"*.jar",
				"*.war",
				"*.ear",
			},
			"dependencies": {
				"node_modules/",
				"vendor/",
				"bower_components/",
				".bundle/",
				"Pods/",
			},
			"editor_files": {
				".vscode/",
				".idea/",
				"*.swp",
				"*.swo",
				"*.swn",
				"*~",
				".project",
				".classpath",
				".settings/",
				"*.sublime-workspace",
				"*.sublime-project",
			},
			"os_files": {
				".DS_Store",
				".DS_Store?",
				"._*",
				".Spotlight-V100",
				".Trashes",
				"ehthumbs.db",
				"Thumbs.db",
				"Desktop.ini",
			},
		},
		MinimumViolationThreshold: 1, // File issue even for single violations (especially secrets)
		Supervisor:                supervisor,
	}, nil
}

// Name implements HealthMonitor.
func (d *GitignoreDetector) Name() string {
	return "gitignore_detector"
}

// Philosophy implements HealthMonitor.
func (d *GitignoreDetector) Philosophy() string {
	return "Source control should track source code and configuration, not " +
		"build artifacts, dependencies, secrets, or environment-specific files. " +
		"Git's .gitignore mechanism prevents committing these files."
}

// Schedule implements HealthMonitor.
func (d *GitignoreDetector) Schedule() ScheduleConfig {
	return ScheduleConfig{
		Type:        ScheduleHybrid,
		MinInterval: 12 * time.Hour, // At least twice daily
		MaxInterval: 24 * time.Hour, // At most daily
		EventTrigger: "every_10_issues", // Or every 10 issues
	}
}

// Cost implements HealthMonitor.
func (d *GitignoreDetector) Cost() CostEstimate {
	return CostEstimate{
		EstimatedDuration: 3 * time.Second,
		AICallsEstimated:  1, // One call to categorize all violations
		RequiresFullScan:  true,
		Category:          CostModerate,
	}
}

// Check implements HealthMonitor.
func (d *GitignoreDetector) Check(ctx context.Context, codebase CodebaseContext) (*MonitorResult, error) {
	// Validate that AI supervisor is configured
	if d.Supervisor == nil {
		return nil, fmt.Errorf("AI supervisor is required for gitignore detection")
	}

	startTime := time.Now()

	// 1. Get list of tracked files from git
	trackedFiles, err := d.getTrackedFiles(ctx)
	if err != nil {
		return nil, fmt.Errorf("getting tracked files: %w", err)
	}

	// 2. Find files that match gitignore patterns
	violations := d.findViolations(trackedFiles)

	// 3. If below threshold, no action needed
	if len(violations) < d.MinimumViolationThreshold {
		return &MonitorResult{
			IssuesFound: []DiscoveredIssue{},
			Context:     fmt.Sprintf("Scanned %d tracked files, no gitignore violations found", len(trackedFiles)),
			CheckedAt:   startTime,
			Stats: CheckStats{
				FilesScanned: len(trackedFiles),
				Duration:     time.Since(startTime),
			},
		}, nil
	}

	// 4. Ask AI to categorize the violations
	evaluation, err := d.evaluateViolations(ctx, violations)
	if err != nil {
		return nil, fmt.Errorf("evaluating violations: %w", err)
	}

	// 5. Build issues from AI evaluation
	issues := d.buildIssues(evaluation)

	return &MonitorResult{
		IssuesFound: issues,
		Context:     d.buildContext(trackedFiles, violations, evaluation),
		Reasoning:   d.buildReasoning(evaluation),
		CheckedAt:   startTime,
		Stats: CheckStats{
			FilesScanned: len(trackedFiles),
			IssuesFound:  len(issues),
			Duration:     time.Since(startTime),
			AICallsMade:  1,
		},
	}, nil
}

// gitignoreViolation represents a tracked file that matches gitignore patterns.
type gitignoreViolation struct {
	Path     string
	Category string // Which category pattern matched (secrets, build_artifacts, etc.)
	Pattern  string // Which specific pattern matched
}

// getTrackedFiles returns all files currently tracked by git.
func (d *GitignoreDetector) getTrackedFiles(ctx context.Context) ([]string, error) {
	cmd := exec.CommandContext(ctx, "git", "ls-files")
	cmd.Dir = d.RootPath

	output, err := cmd.Output()
	if err != nil {
		// Check if this is not a git repository
		if exitErr, ok := err.(*exec.ExitError); ok {
			stderr := string(exitErr.Stderr)
			if strings.Contains(stderr, "not a git repository") {
				return nil, fmt.Errorf("not a git repository: %s", d.RootPath)
			}
		}
		return nil, fmt.Errorf("git ls-files failed: %w", err)
	}

	// Parse output into file list
	files := []string{}
	for _, line := range strings.Split(string(output), "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			files = append(files, line)
		}
	}

	return files, nil
}

// findViolations identifies tracked files that match gitignore patterns.
func (d *GitignoreDetector) findViolations(trackedFiles []string) []gitignoreViolation {
	var violations []gitignoreViolation

	for _, file := range trackedFiles {
		// Check against each category of patterns
		for category, patterns := range d.GitignorePatterns {
			for _, pattern := range patterns {
				matched := d.matchPattern(file, pattern)
				if matched {
					violations = append(violations, gitignoreViolation{
						Path:     file,
						Category: category,
						Pattern:  pattern,
					})
					// Only record first match per file
					goto nextFile
				}
			}
		}
	nextFile:
	}

	return violations
}

// matchPattern checks if a file path matches a gitignore-style pattern.
func (d *GitignoreDetector) matchPattern(path, pattern string) bool {
	// Handle directory patterns (ending with /)
	if strings.HasSuffix(pattern, "/") {
		dirPattern := strings.TrimSuffix(pattern, "/")
		// Check if path starts with the directory
		return strings.HasPrefix(path, dirPattern+"/") || path == dirPattern
	}

	// Handle wildcard patterns
	if strings.Contains(pattern, "*") {
		// Simple glob matching
		matched, err := filepath.Match(pattern, filepath.Base(path))
		if err == nil && matched {
			return true
		}
		// Also check if the pattern matches the full path
		matched, err = filepath.Match(pattern, path)
		return err == nil && matched
	}

	// Exact match (filename or full path)
	return filepath.Base(path) == pattern || path == pattern
}

// gitignoreEvaluation is the AI's categorization of gitignore violations.
type gitignoreEvaluation struct {
	TrueViolations      []violationAction  `json:"true_violations"`
	PatternsToAdd       []string           `json:"patterns_to_add"`
	LegitimateFiles     []legitimateFile   `json:"legitimate_files"`
	Reasoning           string             `json:"reasoning"`
}

type violationAction struct {
	File        string `json:"file"`
	Category    string `json:"category"`
	Reason      string `json:"reason"`
	Action      string `json:"action"` // "remove" | "stop_tracking" | "urgent"
	Pattern     string `json:"pattern"`
}

// evaluateViolations uses AI to categorize gitignore violations.
func (d *GitignoreDetector) evaluateViolations(ctx context.Context, violations []gitignoreViolation) (*gitignoreEvaluation, error) {
	// Limit violations sent to AI to prevent token limit errors
	const maxGitignoreViolationsForAI = 100
	violationsToEvaluate := violations
	if len(violations) > maxGitignoreViolationsForAI {
		violationsToEvaluate = violations[:maxGitignoreViolationsForAI]
	}

	prompt := d.buildPrompt(violationsToEvaluate)

	// Call AI supervisor (vc-35: using Haiku for cost efficiency)
	response, err := d.Supervisor.CallAI(ctx, prompt, "gitignore_evaluation", ai.GetSimpleTaskModel(), 4096)
	if err != nil {
		return nil, fmt.Errorf("AI call failed: %w", err)
	}

	// Parse JSON response using resilient parser
	parseResult := ai.Parse[gitignoreEvaluation](response, ai.ParseOptions{
		Context: "gitignore_evaluation",
	})

	if !parseResult.Success {
		return nil, fmt.Errorf("parsing AI response: %s", parseResult.Error)
	}

	// Validate the AI response
	if err := d.validateEvaluation(&parseResult.Data, violationsToEvaluate); err != nil {
		return nil, fmt.Errorf("invalid AI response: %w", err)
	}

	return &parseResult.Data, nil
}

// validateEvaluation validates the AI's response for correctness.
func (d *GitignoreDetector) validateEvaluation(eval *gitignoreEvaluation, violations []gitignoreViolation) error {
	// Build set of valid file paths from input
	fileSet := make(map[string]bool)
	for _, v := range violations {
		fileSet[v.Path] = true
	}

	// Validate that true_violations references only files we sent
	for _, action := range eval.TrueViolations {
		if !fileSet[action.File] {
			return fmt.Errorf("AI referenced unknown file %q (not in input list)", action.File)
		}
	}

	// Validate that legitimate_files references only files we sent
	for _, legit := range eval.LegitimateFiles {
		if !fileSet[legit.File] {
			return fmt.Errorf("AI referenced unknown file %q (not in input list)", legit.File)
		}
	}

	// Validate glob patterns are valid
	for _, pattern := range eval.PatternsToAdd {
		if pattern == "" {
			return fmt.Errorf("empty glob pattern in patterns_to_add")
		}
		// Test pattern validity by matching against a test string
		if _, err := filepath.Match(pattern, "test"); err != nil {
			return fmt.Errorf("invalid glob pattern %q: %w", pattern, err)
		}
	}

	return nil
}

// buildPrompt creates the AI evaluation prompt.
func (d *GitignoreDetector) buildPrompt(violations []gitignoreViolation) string {
	var sb strings.Builder

	sb.WriteString("# Gitignore Violation Detection Request\n\n")
	sb.WriteString("## Philosophy\n")
	sb.WriteString(d.Philosophy())
	sb.WriteString("\n\n")

	sb.WriteString("## Files Tracked in Git Matching Gitignore Patterns\n")
	sb.WriteString(fmt.Sprintf("Found %d tracked files matching common gitignore patterns:\n\n", len(violations)))

	// Group violations by category for clearer presentation
	byCategory := make(map[string][]gitignoreViolation)
	for _, v := range violations {
		byCategory[v.Category] = append(byCategory[v.Category], v)
	}

	for category, items := range byCategory {
		sb.WriteString(fmt.Sprintf("### %s (%d files)\n", category, len(items)))
		for _, v := range items {
			sb.WriteString(fmt.Sprintf("- %s (pattern: %s)\n", v.Path, v.Pattern))
		}
		sb.WriteString("\n")
	}

	// Use dynamic year to prevent prompt from becoming stale
	year := time.Now().Year()
	sb.WriteString(fmt.Sprintf("## Guidance (%d)\n", year))
	sb.WriteString("Common gitignore violations:\n")
	sb.WriteString("- **Secrets** (CRITICAL): .env files, API keys, certificates, credentials\n")
	sb.WriteString("- **Build artifacts**: Compiled binaries, .o files, dist/ directories\n")
	sb.WriteString("- **Dependencies**: node_modules/, vendor/, downloaded packages\n")
	sb.WriteString("- **Editor files**: .vscode/, .idea/, swap files\n")
	sb.WriteString("- **OS files**: .DS_Store, Thumbs.db, desktop.ini\n\n")

	sb.WriteString("However, some files that match these patterns may be legitimate:\n")
	sb.WriteString("- Example/template files (e.g., '.env.example', 'config.template')\n")
	sb.WriteString("- Documentation about tools (e.g., '.vscode/recommended-extensions.md')\n")
	sb.WriteString("- Deliberately committed test fixtures\n\n")

	sb.WriteString("## Your Task\n")
	sb.WriteString("Categorize each file into one of three categories:\n\n")

	sb.WriteString("1. **True violations**: Files that should not be tracked\n")
	sb.WriteString("   - Secrets (URGENT - security risk)\n")
	sb.WriteString("   - Build artifacts (bloat, merge conflicts)\n")
	sb.WriteString("   - Dependencies (huge, regenerable)\n")
	sb.WriteString("   - Editor/OS files (environment-specific)\n")
	sb.WriteString("   - Action: 'urgent' for secrets, 'stop_tracking' for others\n\n")

	sb.WriteString("2. **Patterns to add to .gitignore**: Prevent future occurrences\n")
	sb.WriteString("   - Generic patterns like *.o, .DS_Store, node_modules/\n")
	sb.WriteString("   - Only suggest if not already in common .gitignore templates\n\n")

	sb.WriteString("3. **Legitimate files**: Not actually violations\n")
	sb.WriteString("   - Example/template files (.env.example)\n")
	sb.WriteString("   - Documented tool configurations\n")
	sb.WriteString("   - Intentional test fixtures\n\n")

	sb.WriteString("## Response Format\n")
	sb.WriteString("Return valid JSON:\n")
	sb.WriteString("```json\n")
	sb.WriteString("{\n")
	sb.WriteString("  \"true_violations\": [\n")
	sb.WriteString("    {\n")
	sb.WriteString("      \"file\": \"path/to/file.env\",\n")
	sb.WriteString("      \"category\": \"secrets\",\n")
	sb.WriteString("      \"reason\": \"Environment file with credentials\",\n")
	sb.WriteString("      \"action\": \"urgent\",\n")
	sb.WriteString("      \"pattern\": \".env\"\n")
	sb.WriteString("    }\n")
	sb.WriteString("  ],\n")
	sb.WriteString("  \"patterns_to_add\": [\n")
	sb.WriteString("    \".env\",\n")
	sb.WriteString("    \".DS_Store\"\n")
	sb.WriteString("  ],\n")
	sb.WriteString("  \"legitimate_files\": [\n")
	sb.WriteString("    {\n")
	sb.WriteString("      \"file\": \"config/.env.example\",\n")
	sb.WriteString("      \"justification\": \"Example template file for configuration, contains no secrets\"\n")
	sb.WriteString("    }\n")
	sb.WriteString("  ],\n")
	sb.WriteString("  \"reasoning\": \"Brief explanation of overall categorization\"\n")
	sb.WriteString("}\n")
	sb.WriteString("```\n")

	return sb.String()
}

// buildIssues converts AI evaluation to DiscoveredIssues.
func (d *GitignoreDetector) buildIssues(eval *gitignoreEvaluation) []DiscoveredIssue {
	var issues []DiscoveredIssue

	// Group violations by action urgency
	urgentViolations := []violationAction{}
	regularViolations := []violationAction{}

	for _, violation := range eval.TrueViolations {
		if violation.Action == "urgent" || violation.Category == "secrets" {
			urgentViolations = append(urgentViolations, violation)
		} else {
			regularViolations = append(regularViolations, violation)
		}
	}

	// Create high-severity issue for urgent violations (secrets)
	if len(urgentViolations) > 0 {
		description := fmt.Sprintf("URGENT: Remove %d secret/credential file(s) from git history", len(urgentViolations))

		evidence := map[string]interface{}{
			"violations":       urgentViolations,
			"patterns_to_add":  eval.PatternsToAdd,
			"reasoning":        eval.Reasoning,
			"violation_count":  len(urgentViolations),
		}

		issues = append(issues, DiscoveredIssue{
			Category:    "gitignore_secrets",
			Severity:    "high",
			Description: description,
			Evidence:    evidence,
		})
	}

	// Create medium-severity issue for regular violations
	if len(regularViolations) > 0 {
		description := fmt.Sprintf("Clean up %d tracked file(s) that should be gitignored", len(regularViolations))

		evidence := map[string]interface{}{
			"violations":       regularViolations,
			"patterns_to_add":  eval.PatternsToAdd,
			"reasoning":        eval.Reasoning,
			"violation_count":  len(regularViolations),
		}

		issues = append(issues, DiscoveredIssue{
			Category:    "gitignore_violations",
			Severity:    "medium",
			Description: description,
			Evidence:    evidence,
		})
	}

	// Create low-severity issue if only patterns need to be added (preventive)
	if len(eval.TrueViolations) == 0 && len(eval.PatternsToAdd) > 0 {
		description := fmt.Sprintf("Add %d pattern(s) to .gitignore (preventive)", len(eval.PatternsToAdd))

		evidence := map[string]interface{}{
			"patterns_to_add": eval.PatternsToAdd,
			"reasoning":       eval.Reasoning,
		}

		issues = append(issues, DiscoveredIssue{
			Category:    "gitignore_patterns",
			Severity:    "low",
			Description: description,
			Evidence:    evidence,
		})
	}

	return issues
}

// buildContext creates context string for MonitorResult.
func (d *GitignoreDetector) buildContext(trackedFiles []string, violations []gitignoreViolation, eval *gitignoreEvaluation) string {
	return fmt.Sprintf(
		"Scanned %d tracked files and found %d matching gitignore patterns. "+
			"AI categorized %d as true violations, %d as legitimate files. "+
			"Recommends adding %d patterns to .gitignore.",
		len(trackedFiles), len(violations), len(eval.TrueViolations), len(eval.LegitimateFiles), len(eval.PatternsToAdd),
	)
}

// buildReasoning creates reasoning string from AI evaluation.
func (d *GitignoreDetector) buildReasoning(eval *gitignoreEvaluation) string {
	if eval.Reasoning != "" {
		return eval.Reasoning
	}
	return fmt.Sprintf(
		"Identified %d gitignore violations to fix and %d .gitignore patterns to add.",
		len(eval.TrueViolations), len(eval.PatternsToAdd),
	)
}
