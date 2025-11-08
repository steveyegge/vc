package health

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/steveyegge/vc/internal/ai"
)

const (
	// maxBuildFilesForAI limits the number of build files sent to AI evaluation
	maxBuildFilesForAI = 20
)

// BuildModernizer analyzes build files for quality, deprecated patterns,
// and missing optimizations.
//
// ZFC Compliance: Collects build configuration patterns and known deprecations,
// then delegates judgment to AI supervisor for impact assessment.
type BuildModernizer struct {
	// RootPath is the codebase root directory
	RootPath string

	// BuildFilePatterns are patterns for build-related files
	BuildFilePatterns []string

	// ExcludePatterns for files/directories to skip
	ExcludePatterns []string

	// AI supervisor for evaluating issues
	Supervisor AISupervisor
}

// NewBuildModernizer creates a build modernizer with sensible defaults.
func NewBuildModernizer(rootPath string, supervisor AISupervisor) (*BuildModernizer, error) {
	// Validate and clean the root path
	absPath, err := filepath.Abs(rootPath)
	if err != nil {
		return nil, fmt.Errorf("invalid root path %q: %w", rootPath, err)
	}

	return &BuildModernizer{
		RootPath: absPath,
		BuildFilePatterns: []string{
			"Makefile",
			"makefile",
			"GNUmakefile",
			"go.mod",
			"go.sum",
			"package.json",
			"package-lock.json",
			"yarn.lock",
			"pnpm-lock.yaml",
			"Cargo.toml",
			"Cargo.lock",
			"build.gradle",
			"build.gradle.kts",
			"pom.xml",
			"requirements.txt",
			"setup.py",
			"pyproject.toml",
			"Dockerfile",
			".tool-versions",
			".nvmrc",
			".ruby-version",
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
func (m *BuildModernizer) Name() string {
	return "build_modernizer"
}

// Philosophy implements HealthMonitor.
func (m *BuildModernizer) Philosophy() string {
	return "Build systems should be simple, fast, and follow current best practices. " +
		"Deprecated tools and missing optimizations slow development and create technical debt."
}

// Schedule implements HealthMonitor.
func (m *BuildModernizer) Schedule() ScheduleConfig {
	return ScheduleConfig{
		Type:     ScheduleTimeBased,
		Interval: 14 * 24 * time.Hour, // Bi-weekly
	}
}

// Cost implements HealthMonitor.
func (m *BuildModernizer) Cost() CostEstimate {
	return CostEstimate{
		EstimatedDuration: 10 * time.Second,
		AICallsEstimated:  1, // One call to evaluate all build files
		RequiresFullScan:  true,
		Category:          CostCheap,
	}
}

// Check implements HealthMonitor.
func (m *BuildModernizer) Check(ctx context.Context, codebase CodebaseContext) (*MonitorResult, error) {
	startTime := time.Now()

	// 1. Scan codebase for build files
	buildFiles, err := m.scanBuildFiles(ctx)
	if err != nil {
		return nil, fmt.Errorf("scanning build files: %w", err)
	}

	// 2. If no build files found, no action needed
	if len(buildFiles) == 0 {
		return &MonitorResult{
			IssuesFound: []DiscoveredIssue{},
			Context:     "No build files found in repository",
			CheckedAt:   startTime,
			Stats: CheckStats{
				FilesScanned: 0,
				Duration:     time.Since(startTime),
			},
		}, nil
	}

	// 3. Validate that AI supervisor is configured (only when we have files)
	if m.Supervisor == nil {
		return nil, fmt.Errorf("AI supervisor is required for build modernization")
	}

	// 4. Read build file contents
	buildFileContents, err := m.readBuildFiles(buildFiles)
	if err != nil {
		return nil, fmt.Errorf("reading build files: %w", err)
	}

	// 5. Ask AI to evaluate the build files
	evaluation, err := m.evaluateBuildFiles(ctx, buildFileContents)
	if err != nil {
		return nil, fmt.Errorf("evaluating build files: %w", err)
	}

	// 6. Build issues from AI evaluation
	issues := m.buildIssues(evaluation)

	return &MonitorResult{
		IssuesFound: issues,
		Context:     m.buildContext(buildFiles, evaluation),
		Reasoning:   evaluation.Reasoning,
		CheckedAt:   startTime,
		Stats: CheckStats{
			FilesScanned: len(buildFiles),
			IssuesFound:  len(issues),
			Duration:     time.Since(startTime),
			AICallsMade:  1,
		},
	}, nil
}

// buildFile represents a discovered build file.
type buildFile struct {
	Path     string
	Name     string
	FileType string // "makefile", "go.mod", "package.json", etc.
}

// buildFileContent represents a build file with its content.
type buildFileContent struct {
	Path     string
	Name     string
	FileType string
	Content  string
	Size     int64
}

// scanBuildFiles walks the directory tree and finds build-related files.
func (m *BuildModernizer) scanBuildFiles(ctx context.Context) ([]buildFile, error) {
	var files []buildFile

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

		// Get relative path for pattern matching
		relPath, err := filepath.Rel(m.RootPath, path)
		if err != nil {
			return nil
		}

		// Skip excluded patterns
		if ShouldExcludePath(relPath, info, m.ExcludePatterns) {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		// Only process files (not directories)
		if info.IsDir() {
			return nil
		}

		// Check if file matches any build file pattern
		fileName := filepath.Base(path)
		for _, pattern := range m.BuildFilePatterns {
			if fileName == pattern {
				fileType := m.detectFileType(fileName)
				files = append(files, buildFile{
					Path:     relPath,
					Name:     fileName,
					FileType: fileType,
				})
				break
			}
		}

		return nil
	})

	return files, err
}

// detectFileType determines the type of build file based on filename.
func (m *BuildModernizer) detectFileType(fileName string) string {
	switch fileName {
	case "Makefile", "makefile", "GNUmakefile":
		return "makefile"
	case "go.mod":
		return "go.mod"
	case "go.sum":
		return "go.sum"
	case "package.json":
		return "package.json"
	case "package-lock.json", "yarn.lock", "pnpm-lock.yaml":
		return "npm-lock"
	case "Cargo.toml":
		return "cargo.toml"
	case "Cargo.lock":
		return "cargo.lock"
	case "build.gradle", "build.gradle.kts":
		return "gradle"
	case "pom.xml":
		return "maven"
	case "requirements.txt", "setup.py", "pyproject.toml":
		return "python"
	case "Dockerfile":
		return "dockerfile"
	case ".tool-versions", ".nvmrc", ".ruby-version":
		return "version-file"
	default:
		return "unknown"
	}
}

// readBuildFiles reads the content of build files.
// Limits the number of files and content size to prevent token limits.
func (m *BuildModernizer) readBuildFiles(files []buildFile) ([]buildFileContent, error) {
	var contents []buildFileContent

	// Limit the number of files to prevent overwhelming the AI
	filesToRead := files
	if len(files) > maxBuildFilesForAI {
		filesToRead = files[:maxBuildFilesForAI]
	}

	for _, file := range filesToRead {
		fullPath := filepath.Join(m.RootPath, file.Path)

		// Get file size
		info, err := os.Stat(fullPath)
		if err != nil {
			// Skip files we can't stat
			continue
		}

		// Skip very large files (> 100KB)
		if info.Size() > 100*1024 {
			continue
		}

		// Read file content
		content, err := os.ReadFile(fullPath)
		if err != nil {
			// Skip files we can't read
			continue
		}

		contents = append(contents, buildFileContent{
			Path:     file.Path,
			Name:     file.Name,
			FileType: file.FileType,
			Content:  string(content),
			Size:     info.Size(),
		})
	}

	return contents, nil
}

// buildEvaluation is the AI's analysis of build files.
type buildEvaluation struct {
	DeprecatedPatterns []deprecatedPattern `json:"deprecated_patterns"`
	MissingOptimizations []missingOptimization `json:"missing_optimizations"`
	VersionIssues      []versionIssue      `json:"version_issues"`
	BestPractices      []bestPractice      `json:"best_practices"`
	Reasoning          string              `json:"reasoning"`
}

type deprecatedPattern struct {
	File        string `json:"file"`
	Pattern     string `json:"pattern"`
	Replacement string `json:"replacement"`
	Impact      string `json:"impact"` // "low", "medium", "high"
	Reason      string `json:"reason"`
}

type missingOptimization struct {
	File        string `json:"file"`
	Optimization string `json:"optimization"`
	Benefit      string `json:"benefit"`
	Effort       string `json:"effort"` // "low", "medium", "high"
}

type versionIssue struct {
	File           string `json:"file"`
	Tool           string `json:"tool"`
	CurrentVersion string `json:"current_version"`
	Issue          string `json:"issue"` // "eol", "outdated", "inconsistent"
	RecommendedVersion string `json:"recommended_version"`
}

type bestPractice struct {
	Practice    string `json:"practice"`
	Description string `json:"description"`
	Files       []string `json:"files"`
}

// evaluateBuildFiles uses AI to analyze build files.
func (m *BuildModernizer) evaluateBuildFiles(ctx context.Context, files []buildFileContent) (*buildEvaluation, error) {
	prompt := m.buildPrompt(files)

	// Call AI supervisor using Haiku for cost efficiency
	response, err := m.Supervisor.CallAI(ctx, prompt, "build_evaluation", "claude-3-5-haiku-20241022", 8192)
	if err != nil {
		return nil, fmt.Errorf("AI call failed: %w", err)
	}

	// Parse JSON response using resilient parser
	parseResult := ai.Parse[buildEvaluation](response, ai.ParseOptions{
		Context: "build_evaluation",
	})

	if !parseResult.Success {
		return nil, fmt.Errorf("parsing AI response: %s", parseResult.Error)
	}

	return &parseResult.Data, nil
}

// buildPrompt creates the AI evaluation prompt.
func (m *BuildModernizer) buildPrompt(files []buildFileContent) string {
	var sb strings.Builder

	sb.WriteString("# Build System Modernization Analysis\n\n")
	sb.WriteString("## Philosophy\n")
	sb.WriteString(m.Philosophy())
	sb.WriteString("\n\n")

	sb.WriteString("## Build Files Found\n")
	sb.WriteString(fmt.Sprintf("Analyzing %d build-related files:\n\n", len(files)))
	for _, f := range files {
		sb.WriteString(fmt.Sprintf("### %s (%s)\n", f.Path, f.FileType))
		sb.WriteString("```\n")
		// Truncate very long content
		content := f.Content
		if len(content) > 5000 {
			content = content[:5000] + "\n... (truncated)"
		}
		sb.WriteString(content)
		sb.WriteString("\n```\n\n")
	}

	year := time.Now().Year()
	sb.WriteString(fmt.Sprintf("## Analysis Guidelines (%d)\n\n", year))

	sb.WriteString("### 1. Deprecated Patterns\n")
	sb.WriteString("Look for:\n")
	sb.WriteString("- **Go**: `go get` (deprecated, use `go install`), old Go versions (< 1.21)\n")
	sb.WriteString("- **npm**: deprecated package.json scripts, missing `engines` field\n")
	sb.WriteString("- **Make**: deprecated flags, old patterns\n")
	sb.WriteString("- **Docker**: old base images, deprecated `MAINTAINER` directive\n\n")

	sb.WriteString("### 2. Missing Optimizations\n")
	sb.WriteString("Look for:\n")
	sb.WriteString("- **Build caching**: No build cache configuration\n")
	sb.WriteString("- **Parallelism**: Serial builds that could be parallel\n")
	sb.WriteString("- **Dependencies**: Missing lock files (go.sum, package-lock.json)\n")
	sb.WriteString("- **Incremental builds**: No incremental build support\n\n")

	sb.WriteString("### 3. Version Issues\n")
	sb.WriteString("Look for:\n")
	sb.WriteString("- **EOL versions**: Tools at end-of-life (Go < 1.21, Node < 18)\n")
	sb.WriteString("- **Inconsistent versions**: Different versions across files\n")
	sb.WriteString("- **Missing version pins**: No .tool-versions or similar\n\n")

	sb.WriteString("### 4. Best Practices\n")
	sb.WriteString("Look for:\n")
	sb.WriteString("- **Version managers**: asdf, nvm, etc.\n")
	sb.WriteString("- **Multi-stage Docker builds**\n")
	sb.WriteString("- **Proper dependency management**\n")
	sb.WriteString("- **Build reproducibility**\n\n")

	sb.WriteString("## Response Format\n")
	sb.WriteString("Return valid JSON:\n")
	sb.WriteString("```json\n")
	sb.WriteString("{\n")
	sb.WriteString("  \"deprecated_patterns\": [\n")
	sb.WriteString("    {\n")
	sb.WriteString("      \"file\": \"Makefile\",\n")
	sb.WriteString("      \"pattern\": \"go get -u ./...\",\n")
	sb.WriteString("      \"replacement\": \"go install\",\n")
	sb.WriteString("      \"impact\": \"medium\",\n")
	sb.WriteString("      \"reason\": \"go get is deprecated for installing binaries\"\n")
	sb.WriteString("    }\n")
	sb.WriteString("  ],\n")
	sb.WriteString("  \"missing_optimizations\": [\n")
	sb.WriteString("    {\n")
	sb.WriteString("      \"file\": \"Makefile\",\n")
	sb.WriteString("      \"optimization\": \"Add build caching\",\n")
	sb.WriteString("      \"benefit\": \"3x faster builds\",\n")
	sb.WriteString("      \"effort\": \"low\"\n")
	sb.WriteString("    }\n")
	sb.WriteString("  ],\n")
	sb.WriteString("  \"version_issues\": [\n")
	sb.WriteString("    {\n")
	sb.WriteString("      \"file\": \"go.mod\",\n")
	sb.WriteString("      \"tool\": \"Go\",\n")
	sb.WriteString("      \"current_version\": \"1.18\",\n")
	sb.WriteString("      \"issue\": \"eol\",\n")
	sb.WriteString("      \"recommended_version\": \"1.23\"\n")
	sb.WriteString("    }\n")
	sb.WriteString("  ],\n")
	sb.WriteString("  \"best_practices\": [\n")
	sb.WriteString("    {\n")
	sb.WriteString("      \"practice\": \"Using .tool-versions for version management\",\n")
	sb.WriteString("      \"description\": \"Ensures consistent tool versions across team\",\n")
	sb.WriteString("      \"files\": [\".tool-versions\"]\n")
	sb.WriteString("    }\n")
	sb.WriteString("  ],\n")
	sb.WriteString("  \"reasoning\": \"Overall assessment of build system quality\"\n")
	sb.WriteString("}\n")
	sb.WriteString("```\n\n")

	sb.WriteString("**Important**: Only report real issues. Empty arrays are fine if everything looks good.\n")

	return sb.String()
}

// buildIssues converts AI evaluation to DiscoveredIssues.
func (m *BuildModernizer) buildIssues(eval *buildEvaluation) []DiscoveredIssue {
	var issues []DiscoveredIssue

	// Group deprecated patterns into a single issue if found
	if len(eval.DeprecatedPatterns) > 0 {
		evidence := map[string]interface{}{
			"deprecated_patterns": eval.DeprecatedPatterns,
			"count":               len(eval.DeprecatedPatterns),
		}

		// Calculate severity based on impact
		severity := m.calculateDeprecationSeverity(eval.DeprecatedPatterns)

		issues = append(issues, DiscoveredIssue{
			Category:    "build",
			Severity:    severity,
			Description: fmt.Sprintf("Update %d deprecated build patterns", len(eval.DeprecatedPatterns)),
			Evidence:    evidence,
		})
	}

	// Group missing optimizations into a single issue if found
	if len(eval.MissingOptimizations) > 0 {
		evidence := map[string]interface{}{
			"missing_optimizations": eval.MissingOptimizations,
			"count":                 len(eval.MissingOptimizations),
		}

		issues = append(issues, DiscoveredIssue{
			Category:    "build",
			Severity:    "medium",
			Description: fmt.Sprintf("Add %d build optimizations", len(eval.MissingOptimizations)),
			Evidence:    evidence,
		})
	}

	// Group version issues into a single issue if found
	if len(eval.VersionIssues) > 0 {
		evidence := map[string]interface{}{
			"version_issues": eval.VersionIssues,
			"count":          len(eval.VersionIssues),
		}

		// Calculate severity based on issue type
		severity := m.calculateVersionSeverity(eval.VersionIssues)

		issues = append(issues, DiscoveredIssue{
			Category:    "build",
			Severity:    severity,
			Description: fmt.Sprintf("Fix %d tool version issues", len(eval.VersionIssues)),
			Evidence:    evidence,
		})
	}

	return issues
}

// calculateDeprecationSeverity determines severity based on deprecation impacts.
// Panics if called with an empty list (caller must check len > 0).
func (m *BuildModernizer) calculateDeprecationSeverity(patterns []deprecatedPattern) string {
	if len(patterns) == 0 {
		panic("calculateDeprecationSeverity called with empty patterns list")
	}

	highCount := 0
	for _, p := range patterns {
		if p.Impact == "high" {
			highCount++
		}
	}

	if highCount > 0 {
		return "high"
	}
	if len(patterns) >= 3 {
		return "medium"
	}
	return "low"
}

// calculateVersionSeverity determines severity based on version issue types.
// Panics if called with an empty list (caller must check len > 0).
func (m *BuildModernizer) calculateVersionSeverity(issues []versionIssue) string {
	if len(issues) == 0 {
		panic("calculateVersionSeverity called with empty issues list")
	}

	eolCount := 0
	for _, issue := range issues {
		if issue.Issue == "eol" {
			eolCount++
		}
	}

	if eolCount > 0 {
		return "high"
	}
	if len(issues) >= 2 {
		return "medium"
	}
	return "low"
}

// buildContext creates context string for MonitorResult.
func (m *BuildModernizer) buildContext(files []buildFile, eval *buildEvaluation) string {
	return fmt.Sprintf(
		"Analyzed %d build files. Found %d deprecated patterns, %d missing optimizations, "+
			"%d version issues. Identified %d best practices in use.",
		len(files),
		len(eval.DeprecatedPatterns),
		len(eval.MissingOptimizations),
		len(eval.VersionIssues),
		len(eval.BestPractices),
	)
}
