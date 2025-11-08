package health

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/steveyegge/vc/internal/ai"
)

const (
	// maxCICDFilesForAI limits the number of CI/CD config files sent to AI evaluation
	maxCICDFilesForAI = 15
)

// CICDReviewer analyzes CI/CD pipeline configurations for quality,
// missing quality gates, and optimization opportunities.
//
// ZFC Compliance: Collects CI/CD configuration patterns and structures,
// then delegates judgment to AI supervisor for quality assessment.
type CICDReviewer struct {
	// RootPath is the codebase root directory
	RootPath string

	// CICDFilePatterns are patterns for CI/CD config files
	CICDFilePatterns map[string][]string // platform -> patterns

	// ExcludePatterns for files/directories to skip
	ExcludePatterns []string

	// AI supervisor for evaluating pipelines
	Supervisor AISupervisor
}

// NewCICDReviewer creates a CI/CD reviewer with sensible defaults.
func NewCICDReviewer(rootPath string, supervisor AISupervisor) (*CICDReviewer, error) {
	// Validate and clean the root path
	absPath, err := filepath.Abs(rootPath)
	if err != nil {
		return nil, fmt.Errorf("invalid root path %q: %w", rootPath, err)
	}

	return &CICDReviewer{
		RootPath: absPath,
		CICDFilePatterns: map[string][]string{
			"github": {
				".github/workflows/*.yml",
				".github/workflows/*.yaml",
			},
			"gitlab": {
				".gitlab-ci.yml",
				".gitlab-ci.yaml",
			},
			"circleci": {
				".circleci/config.yml",
				".circleci/config.yaml",
			},
			"travis": {
				".travis.yml",
			},
			"azure": {
				"azure-pipelines.yml",
				"azure-pipelines.yaml",
			},
			"jenkins": {
				"Jenkinsfile",
			},
		},
		ExcludePatterns: []string{
			"vendor/",
			".git/",
			"node_modules/",
			".beads/",
		},
		Supervisor: supervisor,
	}, nil
}

// Name implements HealthMonitor.
func (r *CICDReviewer) Name() string {
	return "cicd_reviewer"
}

// Philosophy implements HealthMonitor.
func (r *CICDReviewer) Philosophy() string {
	return "CI/CD pipelines should be fast, reliable, and enforce quality gates. " +
		"They should catch bugs early, run efficiently, and secure the deployment process."
}

// Schedule implements HealthMonitor.
func (r *CICDReviewer) Schedule() ScheduleConfig {
	return ScheduleConfig{
		Type:     ScheduleTimeBased,
		Interval: 14 * 24 * time.Hour, // Bi-weekly
	}
}

// Cost implements HealthMonitor.
func (r *CICDReviewer) Cost() CostEstimate {
	return CostEstimate{
		EstimatedDuration: 20 * time.Second,
		AICallsEstimated:  1, // One call to evaluate all CI/CD configs
		RequiresFullScan:  true,
		Category:          CostModerate,
	}
}

// Check implements HealthMonitor.
func (r *CICDReviewer) Check(ctx context.Context, codebase CodebaseContext) (*MonitorResult, error) {
	startTime := time.Now()

	// 1. Scan codebase for CI/CD config files
	cicdFiles, err := r.scanCICDFiles(ctx)
	if err != nil {
		return nil, fmt.Errorf("scanning CI/CD files: %w", err)
	}

	// 2. If no CI/CD files found, no action needed
	if len(cicdFiles) == 0 {
		return &MonitorResult{
			IssuesFound: []DiscoveredIssue{},
			Context:     "No CI/CD configuration files found in repository",
			CheckedAt:   startTime,
			Stats: CheckStats{
				FilesScanned: 0,
				Duration:     time.Since(startTime),
			},
		}, nil
	}

	// 3. Validate that AI supervisor is configured (only when we have files)
	if r.Supervisor == nil {
		return nil, fmt.Errorf("AI supervisor is required for CI/CD review")
	}

	// 4. Read CI/CD file contents
	cicdContents, errorsIgnored, err := r.readCICDFiles(cicdFiles)
	if err != nil {
		return nil, fmt.Errorf("reading CI/CD files: %w", err)
	}

	// 5. Ask AI to evaluate the CI/CD configs
	evaluation, err := r.evaluateCICD(ctx, cicdContents)
	if err != nil {
		return nil, fmt.Errorf("evaluating CI/CD configs: %w", err)
	}

	// 6. Build issues from AI evaluation
	issues := r.buildIssues(evaluation)

	return &MonitorResult{
		IssuesFound: issues,
		Context:     r.buildContext(cicdFiles, evaluation),
		Reasoning:   evaluation.Reasoning,
		CheckedAt:   startTime,
		Stats: CheckStats{
			FilesScanned:  len(cicdFiles),
			IssuesFound:   len(issues),
			Duration:      time.Since(startTime),
			AICallsMade:   1,
			ErrorsIgnored: errorsIgnored,
		},
	}, nil
}

// cicdFile represents a discovered CI/CD config file.
type cicdFile struct {
	Path     string
	Name     string
	Platform string // "github", "gitlab", "circleci", etc.
}

// cicdFileContent represents a CI/CD config file with its content.
type cicdFileContent struct {
	Path     string
	Name     string
	Platform string
	Content  string
	Size     int64
}

// scanCICDFiles walks the directory tree and finds CI/CD config files.
func (r *CICDReviewer) scanCICDFiles(ctx context.Context) ([]cicdFile, error) {
	var files []cicdFile

	// Check each platform's patterns
	for platform, patterns := range r.CICDFilePatterns {
		for _, pattern := range patterns {
			// Handle glob patterns in .github/workflows/*
			if strings.Contains(pattern, "*") {
				matches, err := r.findGlobMatches(ctx, pattern)
				if err != nil {
					continue // Skip patterns that error
				}
				for _, match := range matches {
					files = append(files, cicdFile{
						Path:     match,
						Name:     filepath.Base(match),
						Platform: platform,
					})
				}
			} else {
				// Direct file path
				fullPath := filepath.Join(r.RootPath, pattern)
				if _, err := os.Stat(fullPath); err == nil {
					files = append(files, cicdFile{
						Path:     pattern,
						Name:     filepath.Base(pattern),
						Platform: platform,
					})
				}
			}
		}
	}

	return files, nil
}

// findGlobMatches finds files matching a glob pattern.
func (r *CICDReviewer) findGlobMatches(ctx context.Context, pattern string) ([]string, error) {
	var matches []string

	// Extract directory and file pattern
	dir := filepath.Dir(pattern)
	filePattern := filepath.Base(pattern)

	fullDir := filepath.Join(r.RootPath, dir)

	// Check if directory exists
	if _, err := os.Stat(fullDir); os.IsNotExist(err) {
		return matches, nil // Directory doesn't exist, no matches
	}

	// Read directory
	entries, err := os.ReadDir(fullDir)
	if err != nil {
		return nil, err
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		matched, err := filepath.Match(filePattern, entry.Name())
		if err != nil {
			continue
		}

		if matched {
			relPath := filepath.Join(dir, entry.Name())
			matches = append(matches, relPath)
		}
	}

	return matches, nil
}

// readCICDFiles reads the content of CI/CD config files.
func (r *CICDReviewer) readCICDFiles(files []cicdFile) ([]cicdFileContent, int, error) {
	var contents []cicdFileContent
	var errorsIgnored int

	// Limit the number of files to prevent overwhelming the AI
	filesToRead := files
	if len(files) > maxCICDFilesForAI {
		filesToRead = files[:maxCICDFilesForAI]
	}

	for _, file := range filesToRead {
		fullPath := filepath.Join(r.RootPath, file.Path)

		// Get file size
		info, err := os.Stat(fullPath)
		if err != nil {
			// Log warning for file access errors (separate from intentional skips)
			log.Printf("CICDReviewer: failed to stat %s: %v", file.Path, err)
			errorsIgnored++
			continue
		}

		// Skip very large files (> 200KB) - this is intentional, not an error
		if info.Size() > 200*1024 {
			continue
		}

		// Read file content
		content, err := os.ReadFile(fullPath)
		if err != nil {
			// Log warning for file read errors
			log.Printf("CICDReviewer: failed to read %s: %v", file.Path, err)
			errorsIgnored++
			continue
		}

		contents = append(contents, cicdFileContent{
			Path:     file.Path,
			Name:     file.Name,
			Platform: file.Platform,
			Content:  string(content),
			Size:     info.Size(),
		})
	}

	return contents, errorsIgnored, nil
}

// cicdEvaluation is the AI's analysis of CI/CD configs.
type cicdEvaluation struct {
	MissingQualityGates []missingQualityGate `json:"missing_quality_gates"`
	SlowPipelines       []slowPipeline       `json:"slow_pipelines"`
	SecurityIssues      []securityIssue      `json:"security_issues"`
	DeprecatedActions   []deprecatedAction   `json:"deprecated_actions"`
	MissingCaching      []missingCache       `json:"missing_caching"`
	BestPractices       []cicdBestPractice   `json:"best_practices"`
	Reasoning           string               `json:"reasoning"`
}

type missingQualityGate struct {
	File        string `json:"file"`
	Gate        string `json:"gate"` // "tests", "lint", "security-scan", "build"
	Reason      string `json:"reason"`
	Severity    string `json:"severity"` // "low", "medium", "high"
}

type slowPipeline struct {
	File                 string   `json:"file"`
	Jobs                 []string `json:"jobs"`
	ParallelizationIdea  string   `json:"parallelization_idea"`
	EstimatedSpeedup     string   `json:"estimated_speedup"`
}

type securityIssue struct {
	File        string `json:"file"`
	Issue       string `json:"issue"`
	Location    string `json:"location"`
	Severity    string `json:"severity"`
	Remediation string `json:"remediation"`
}

type deprecatedAction struct {
	File          string `json:"file"`
	Action        string `json:"action"`
	CurrentVersion string `json:"current_version"`
	RecommendedVersion string `json:"recommended_version"`
	BreakingChanges bool `json:"breaking_changes"`
}

type missingCache struct {
	File        string `json:"file"`
	CacheType   string `json:"cache_type"` // "dependencies", "build", "test"
	Benefit     string `json:"benefit"`
}

type cicdBestPractice struct {
	Practice    string   `json:"practice"`
	Description string   `json:"description"`
	Files       []string `json:"files"`
}

// evaluateCICD uses AI to analyze CI/CD configs.
func (r *CICDReviewer) evaluateCICD(ctx context.Context, files []cicdFileContent) (*cicdEvaluation, error) {
	prompt := r.buildPrompt(files)

	// Call AI supervisor using Haiku for cost efficiency
	response, err := r.Supervisor.CallAI(ctx, prompt, "cicd_evaluation", "claude-3-5-haiku-20241022", 8192)
	if err != nil {
		return nil, fmt.Errorf("AI call failed: %w", err)
	}

	// Parse JSON response using resilient parser
	parseResult := ai.Parse[cicdEvaluation](response, ai.ParseOptions{
		Context: "cicd_evaluation",
	})

	if !parseResult.Success {
		return nil, fmt.Errorf("parsing AI response: %s", parseResult.Error)
	}

	return &parseResult.Data, nil
}

// buildPrompt creates the AI evaluation prompt.
func (r *CICDReviewer) buildPrompt(files []cicdFileContent) string {
	var sb strings.Builder

	sb.WriteString("# CI/CD Pipeline Review\n\n")
	sb.WriteString("## Philosophy\n")
	sb.WriteString(r.Philosophy())
	sb.WriteString("\n\n")

	sb.WriteString("## CI/CD Configurations Found\n")
	sb.WriteString(fmt.Sprintf("Analyzing %d CI/CD configuration files:\n\n", len(files)))
	for _, f := range files {
		sb.WriteString(fmt.Sprintf("### %s (%s)\n", f.Path, f.Platform))
		sb.WriteString("```yaml\n")
		// Truncate very long content
		content := f.Content
		if len(content) > 8000 {
			content = content[:8000] + "\n... (truncated)"
		}
		sb.WriteString(content)
		sb.WriteString("\n```\n\n")
	}

	year := time.Now().Year()
	sb.WriteString(fmt.Sprintf("## Analysis Guidelines (%d)\n\n", year))

	sb.WriteString("### 1. Missing Quality Gates\n")
	sb.WriteString("Check if pipeline includes:\n")
	sb.WriteString("- **Tests**: Unit tests, integration tests\n")
	sb.WriteString("- **Linting**: Code quality checks (golangci-lint, eslint, etc.)\n")
	sb.WriteString("- **Security scanning**: Vulnerability checks (govulncheck, npm audit, Snyk)\n")
	sb.WriteString("- **Build verification**: Ensures code compiles\n")
	sb.WriteString("- **Code coverage**: Coverage reporting\n\n")

	sb.WriteString("### 2. Slow Pipelines\n")
	sb.WriteString("Look for:\n")
	sb.WriteString("- **Serial jobs** that could run in parallel\n")
	sb.WriteString("- **Long-running steps** without parallelization\n")
	sb.WriteString("- **Redundant operations** (building same thing twice)\n")
	sb.WriteString("- **Matrix builds** that could be optimized\n\n")

	sb.WriteString("### 3. Security Issues\n")
	sb.WriteString("Look for:\n")
	sb.WriteString("- **Hardcoded secrets** or credentials in configs\n")
	sb.WriteString("- **Missing secret scanning** in pipeline\n")
	sb.WriteString("- **Overly permissive permissions** (e.g., GitHub Actions permissions)\n")
	sb.WriteString("- **Unsafe script execution** (eval, curl | bash)\n")
	sb.WriteString("- **Third-party actions without version pins**\n\n")

	sb.WriteString("### 4. Deprecated Actions/Images\n")
	sb.WriteString("Look for:\n")
	sb.WriteString("- **GitHub Actions**: Old versions (actions/checkout@v2, use v4)\n")
	sb.WriteString("- **Docker images**: Old base images, deprecated versions\n")
	sb.WriteString("- **Node versions**: EOL Node versions in CI\n")
	sb.WriteString("- **Deprecated CI features**: Old syntax, removed options\n\n")

	sb.WriteString("### 5. Missing Caching\n")
	sb.WriteString("Look for:\n")
	sb.WriteString("- **Dependencies** re-downloaded every run (npm, go mod, pip)\n")
	sb.WriteString("- **Build artifacts** not cached between runs\n")
	sb.WriteString("- **Test results** not cached for incremental testing\n\n")

	sb.WriteString("### 6. Best Practices\n")
	sb.WriteString("Identify good patterns:\n")
	sb.WriteString("- **Matrix builds** for multi-platform testing\n")
	sb.WriteString("- **Proper secret management**\n")
	sb.WriteString("- **Deployment automation**\n")
	sb.WriteString("- **Fast feedback loops** (fail fast)\n\n")

	sb.WriteString("## Response Format\n")
	sb.WriteString("Return valid JSON:\n")
	sb.WriteString("```json\n")
	sb.WriteString("{\n")
	sb.WriteString("  \"missing_quality_gates\": [\n")
	sb.WriteString("    {\n")
	sb.WriteString("      \"file\": \".github/workflows/ci.yml\",\n")
	sb.WriteString("      \"gate\": \"security-scan\",\n")
	sb.WriteString("      \"reason\": \"No govulncheck or similar security scanning\",\n")
	sb.WriteString("      \"severity\": \"high\"\n")
	sb.WriteString("    }\n")
	sb.WriteString("  ],\n")
	sb.WriteString("  \"slow_pipelines\": [\n")
	sb.WriteString("    {\n")
	sb.WriteString("      \"file\": \".github/workflows/ci.yml\",\n")
	sb.WriteString("      \"jobs\": [\"test\", \"lint\", \"build\"],\n")
	sb.WriteString("      \"parallelization_idea\": \"Run test, lint, and build in parallel instead of sequentially\",\n")
	sb.WriteString("      \"estimated_speedup\": \"3x faster\"\n")
	sb.WriteString("    }\n")
	sb.WriteString("  ],\n")
	sb.WriteString("  \"security_issues\": [\n")
	sb.WriteString("    {\n")
	sb.WriteString("      \"file\": \".github/workflows/deploy.yml\",\n")
	sb.WriteString("      \"issue\": \"Hardcoded AWS credentials\",\n")
	sb.WriteString("      \"location\": \"Line 25\",\n")
	sb.WriteString("      \"severity\": \"high\",\n")
	sb.WriteString("      \"remediation\": \"Use GitHub Actions secrets\"\n")
	sb.WriteString("    }\n")
	sb.WriteString("  ],\n")
	sb.WriteString("  \"deprecated_actions\": [\n")
	sb.WriteString("    {\n")
	sb.WriteString("      \"file\": \".github/workflows/ci.yml\",\n")
	sb.WriteString("      \"action\": \"actions/checkout\",\n")
	sb.WriteString("      \"current_version\": \"v2\",\n")
	sb.WriteString("      \"recommended_version\": \"v4\",\n")
	sb.WriteString("      \"breaking_changes\": false\n")
	sb.WriteString("    }\n")
	sb.WriteString("  ],\n")
	sb.WriteString("  \"missing_caching\": [\n")
	sb.WriteString("    {\n")
	sb.WriteString("      \"file\": \".github/workflows/ci.yml\",\n")
	sb.WriteString("      \"cache_type\": \"dependencies\",\n")
	sb.WriteString("      \"benefit\": \"Reduce npm install time from 2min to 10sec\"\n")
	sb.WriteString("    }\n")
	sb.WriteString("  ],\n")
	sb.WriteString("  \"best_practices\": [\n")
	sb.WriteString("    {\n")
	sb.WriteString("      \"practice\": \"Matrix builds for multiple Go versions\",\n")
	sb.WriteString("      \"description\": \"Tests against Go 1.21, 1.22, 1.23\",\n")
	sb.WriteString("      \"files\": [\".github/workflows/ci.yml\"]\n")
	sb.WriteString("    }\n")
	sb.WriteString("  ],\n")
	sb.WriteString("  \"reasoning\": \"Overall assessment of CI/CD pipeline quality\"\n")
	sb.WriteString("}\n")
	sb.WriteString("```\n\n")

	sb.WriteString("**Important**: Only report real issues. Empty arrays are fine if everything looks good.\n")

	return sb.String()
}

// buildIssues converts AI evaluation to DiscoveredIssues.
func (r *CICDReviewer) buildIssues(eval *cicdEvaluation) []DiscoveredIssue {
	var issues []DiscoveredIssue

	// Group missing quality gates into a single issue if found
	if len(eval.MissingQualityGates) > 0 {
		evidence := map[string]interface{}{
			"missing_gates": eval.MissingQualityGates,
			"count":         len(eval.MissingQualityGates),
		}

		severity := r.calculateQualityGateSeverity(eval.MissingQualityGates)

		issues = append(issues, DiscoveredIssue{
			Category:    "cicd",
			Severity:    severity,
			Description: fmt.Sprintf("Add %d missing quality gates to CI/CD pipeline", len(eval.MissingQualityGates)),
			Evidence:    evidence,
		})
	}

	// Group slow pipelines into a single issue if found
	if len(eval.SlowPipelines) > 0 {
		evidence := map[string]interface{}{
			"slow_pipelines": eval.SlowPipelines,
			"count":          len(eval.SlowPipelines),
		}

		issues = append(issues, DiscoveredIssue{
			Category:    "cicd",
			Severity:    "medium",
			Description: fmt.Sprintf("Optimize %d slow CI/CD pipelines", len(eval.SlowPipelines)),
			Evidence:    evidence,
		})
	}

	// Group security issues into a single issue if found
	if len(eval.SecurityIssues) > 0 {
		evidence := map[string]interface{}{
			"security_issues": eval.SecurityIssues,
			"count":           len(eval.SecurityIssues),
		}

		severity := r.calculateSecuritySeverity(eval.SecurityIssues)

		issues = append(issues, DiscoveredIssue{
			Category:    "cicd",
			Severity:    severity,
			Description: fmt.Sprintf("Fix %d security issues in CI/CD configs", len(eval.SecurityIssues)),
			Evidence:    evidence,
		})
	}

	// Group deprecated actions into a single issue if found
	if len(eval.DeprecatedActions) > 0 {
		evidence := map[string]interface{}{
			"deprecated_actions": eval.DeprecatedActions,
			"count":              len(eval.DeprecatedActions),
		}

		issues = append(issues, DiscoveredIssue{
			Category:    "cicd",
			Severity:    "low",
			Description: fmt.Sprintf("Update %d deprecated CI/CD actions", len(eval.DeprecatedActions)),
			Evidence:    evidence,
		})
	}

	// Group missing caching into a single issue if found
	if len(eval.MissingCaching) > 0 {
		evidence := map[string]interface{}{
			"missing_caching": eval.MissingCaching,
			"count":           len(eval.MissingCaching),
		}

		issues = append(issues, DiscoveredIssue{
			Category:    "cicd",
			Severity:    "medium",
			Description: fmt.Sprintf("Add caching to %d CI/CD steps", len(eval.MissingCaching)),
			Evidence:    evidence,
		})
	}

	return issues
}

// calculateQualityGateSeverity determines severity based on missing gates.
// Panics if called with an empty list (caller must check len > 0).
func (r *CICDReviewer) calculateQualityGateSeverity(gates []missingQualityGate) string {
	if len(gates) == 0 {
		panic("calculateQualityGateSeverity called with empty gates list")
	}

	highCount := 0
	for _, gate := range gates {
		if gate.Severity == "high" {
			highCount++
		}
	}

	if highCount > 0 {
		return "high"
	}
	if len(gates) >= 3 {
		return "medium"
	}
	return "low"
}

// calculateSecuritySeverity determines severity based on security issues.
// Panics if called with an empty list (caller must check len > 0).
// Security issues are at least medium severity when present.
func (r *CICDReviewer) calculateSecuritySeverity(issues []securityIssue) string {
	if len(issues) == 0 {
		panic("calculateSecuritySeverity called with empty issues list")
	}

	highCount := 0
	for _, issue := range issues {
		if issue.Severity == "high" {
			highCount++
		}
	}

	if highCount > 0 {
		return "high"
	}
	return "medium" // Security issues are at least medium severity
}

// buildContext creates context string for MonitorResult.
func (r *CICDReviewer) buildContext(files []cicdFile, eval *cicdEvaluation) string {
	return fmt.Sprintf(
		"Analyzed %d CI/CD config files. Found %d missing quality gates, %d slow pipelines, "+
			"%d security issues, %d deprecated actions, %d missing caching opportunities. "+
			"Identified %d best practices in use.",
		len(files),
		len(eval.MissingQualityGates),
		len(eval.SlowPipelines),
		len(eval.SecurityIssues),
		len(eval.DeprecatedActions),
		len(eval.MissingCaching),
		len(eval.BestPractices),
	)
}
