package sdk

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/steveyegge/vc/internal/discovery"
	"github.com/steveyegge/vc/internal/health"
	"gopkg.in/yaml.v3"
)

// YAMLWorker represents a worker defined in YAML format.
// This allows creating simple discovery workers without writing Go code.
//
// Example YAML file (naming-conventions.yaml):
//
//	name: naming_conventions
//	philosophy: "Consistent naming improves readability"
//	scope: "Function and variable names"
//	cost:
//	  duration: 30s
//	  ai_calls: 0
//	  category: cheap
//
//	patterns:
//	  - name: "snake_case_functions"
//	    regex: "func [a-z_]+[a-z0-9_]*\\("
//	    file_pattern: "*.go"
//	    title: "Function uses snake_case (should be camelCase)"
//	    description: "Go convention is camelCase for function names"
//	    priority: 3
//	    category: "naming"
//
//	missing_files:
//	  - path: "README.md"
//	    title: "Missing README.md"
//	    description: "Project should have a README"
//	    priority: 2
//
//	ai_checks:
//	  - name: "api_security"
//	    pattern: "func.*Handler.*ResponseWriter"
//	    prompt: "Check this HTTP handler for security issues"
//	    priority: 1
//	    category: "security"
type YAMLWorker struct {
	config YAMLWorkerConfig
}

// YAMLWorkerConfig defines the YAML worker configuration structure.
type YAMLWorkerConfig struct {
	// Worker metadata
	Name       string `yaml:"name"`
	Philosophy string `yaml:"philosophy"`
	Scope      string `yaml:"scope"`

	// Cost configuration
	Cost struct {
		Duration string `yaml:"duration"` // e.g., "30s", "2m"
		AICalls  int    `yaml:"ai_calls"`
		Category string `yaml:"category"` // cheap, moderate, expensive
	} `yaml:"cost"`

	// Dependencies on other workers
	Dependencies []string `yaml:"dependencies,omitempty"`

	// Pattern-based checks
	Patterns []PatternCheck `yaml:"patterns,omitempty"`

	// Missing file checks
	MissingFiles []MissingFileCheck `yaml:"missing_files,omitempty"`

	// AI-powered checks
	AIChecks []AICheck `yaml:"ai_checks,omitempty"`
}

// PatternCheck defines a regex-based code pattern to find.
type PatternCheck struct {
	Name         string   `yaml:"name"`
	Regex        string   `yaml:"regex"`
	FilePattern  string   `yaml:"file_pattern"`  // e.g., "*.go", "**/*.ts"
	ExcludeDirs  []string `yaml:"exclude_dirs,omitempty"`
	Title        string   `yaml:"title"`
	Description  string   `yaml:"description"`
	Priority     int      `yaml:"priority"`
	Category     string   `yaml:"category"`
	Tags         []string `yaml:"tags,omitempty"`
	Confidence   float64  `yaml:"confidence,omitempty"`
}

// MissingFileCheck defines an expected file that should exist.
type MissingFileCheck struct {
	Path        string `yaml:"path"`        // Relative path from project root
	Title       string `yaml:"title"`
	Description string `yaml:"description"`
	Priority    int    `yaml:"priority"`
	Category    string `yaml:"category,omitempty"`
	Tags        []string `yaml:"tags,omitempty"`
}

// AICheck defines an AI-powered analysis check.
type AICheck struct {
	Name        string `yaml:"name"`
	Pattern     string `yaml:"pattern"`      // Regex to find code to analyze
	FilePattern string `yaml:"file_pattern"` // File pattern to search
	Prompt      string `yaml:"prompt"`       // Prompt for AI
	Priority    int    `yaml:"priority"`
	Category    string `yaml:"category"`
	Tags        []string `yaml:"tags,omitempty"`
	MaxMatches  int    `yaml:"max_matches,omitempty"` // Limit AI calls for cost control
}

// LoadYAMLWorker loads a worker definition from a YAML file.
//
// Example:
//
//	worker, err := sdk.LoadYAMLWorker("naming-conventions.yaml")
//	if err != nil {
//		return err
//	}
//
//	result, err := worker.Analyze(ctx, codebase)
func LoadYAMLWorker(path string) (*YAMLWorker, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading YAML file: %w", err)
	}

	var config YAMLWorkerConfig
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("parsing YAML: %w", err)
	}

	// Validate config
	if err := validateYAMLConfig(&config); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	return &YAMLWorker{config: config}, nil
}

// validateYAMLConfig validates a YAML worker configuration.
func validateYAMLConfig(config *YAMLWorkerConfig) error {
	if config.Name == "" {
		return fmt.Errorf("worker name is required")
	}
	if config.Philosophy == "" {
		return fmt.Errorf("worker philosophy is required")
	}
	if config.Scope == "" {
		return fmt.Errorf("worker scope is required")
	}

	// Validate patterns
	for i, pattern := range config.Patterns {
		if pattern.Regex == "" {
			return fmt.Errorf("pattern %d: regex is required", i)
		}
		if pattern.Title == "" {
			return fmt.Errorf("pattern %d: title is required", i)
		}
	}

	// Validate missing file checks
	for i, check := range config.MissingFiles {
		if check.Path == "" {
			return fmt.Errorf("missing_file %d: path is required", i)
		}
		if check.Title == "" {
			return fmt.Errorf("missing_file %d: title is required", i)
		}
	}

	// Validate AI checks
	for i, check := range config.AIChecks {
		if check.Pattern == "" {
			return fmt.Errorf("ai_check %d: pattern is required", i)
		}
		if check.Prompt == "" {
			return fmt.Errorf("ai_check %d: prompt is required", i)
		}
	}

	return nil
}

// Name implements DiscoveryWorker.
func (w *YAMLWorker) Name() string {
	return w.config.Name
}

// Philosophy implements DiscoveryWorker.
func (w *YAMLWorker) Philosophy() string {
	return w.config.Philosophy
}

// Scope implements DiscoveryWorker.
func (w *YAMLWorker) Scope() string {
	return w.config.Scope
}

// Cost implements DiscoveryWorker.
func (w *YAMLWorker) Cost() health.CostEstimate {
	duration, _ := time.ParseDuration(w.config.Cost.Duration)
	if duration == 0 {
		duration = 30 * time.Second // Default
	}

	category := health.CostCheap
	switch w.config.Cost.Category {
	case "moderate":
		category = health.CostModerate
	case "expensive":
		category = health.CostExpensive
	}

	return health.CostEstimate{
		EstimatedDuration: duration,
		AICallsEstimated:  w.config.Cost.AICalls,
		RequiresFullScan:  true,
		Category:          category,
	}
}

// Dependencies implements DiscoveryWorker.
func (w *YAMLWorker) Dependencies() []string {
	return w.config.Dependencies
}

// Analyze implements DiscoveryWorker.
func (w *YAMLWorker) Analyze(ctx context.Context, codebase health.CodebaseContext) (*discovery.WorkerResult, error) {
	result := NewWorkerResultBuilder(w.Name()).
		WithContext(fmt.Sprintf("Running YAML-defined checks in %s", codebase.RootPath)).
		WithReasoning(fmt.Sprintf("Based on philosophy: '%s'\n\n%s", w.Philosophy(), w.Scope()))

	// Run pattern checks
	for _, pattern := range w.config.Patterns {
		if err := w.runPatternCheck(codebase, pattern, result); err != nil {
			// Log error but continue with other checks
			result.IncrementPatternsFound()
		}
	}

	// Run missing file checks
	for _, check := range w.config.MissingFiles {
		w.runMissingFileCheck(codebase, check, result)
	}

	// Run AI checks
	for _, check := range w.config.AIChecks {
		if err := w.runAICheck(ctx, codebase, check, result); err != nil {
			// Log error but continue
			result.IncrementPatternsFound()
		}
	}

	return result.Build(), nil
}

// runPatternCheck executes a pattern-based check.
func (w *YAMLWorker) runPatternCheck(codebase health.CodebaseContext, check PatternCheck, result *WorkerResultBuilder) error {
	opts := PatternOptions{
		FilePattern: check.FilePattern,
		ExcludeDirs: check.ExcludeDirs,
	}

	matches, err := FindPattern(codebase.RootPath, check.Regex, opts)
	if err != nil {
		return err
	}

	confidence := check.Confidence
	if confidence == 0 {
		confidence = 0.8 // Default
	}

	for _, match := range matches {
		result.AddIssue(NewIssue().
			WithTitle(check.Title).
			WithDescription(fmt.Sprintf("%s\n\nFound at %s:%d:\n%s", check.Description, match.File, match.Line, match.Context)).
			WithCategory(check.Category).
			WithFile(match.File, match.Line).
			WithPriority(check.Priority).
			WithTags(check.Tags...).
			WithConfidence(confidence).
			WithEvidence("pattern", check.Name).
			WithEvidence("regex", check.Regex).
			WithEvidence("matched_text", match.Text).
			Build())

		result.IncrementPatternsFound()
	}

	return nil
}

// runMissingFileCheck executes a missing file check.
func (w *YAMLWorker) runMissingFileCheck(codebase health.CodebaseContext, check MissingFileCheck, result *WorkerResultBuilder) {
	fullPath := filepath.Join(codebase.RootPath, check.Path)

	if _, err := os.Stat(fullPath); os.IsNotExist(err) {
		category := check.Category
		if category == "" {
			category = "documentation"
		}

		result.AddIssue(NewIssue().
			WithTitle(check.Title).
			WithDescription(check.Description).
			WithCategory(category).
			WithPriority(check.Priority).
			WithTags(check.Tags...).
			WithEvidence("expected_path", check.Path).
			WithConfidence(1.0). // High confidence - file definitely doesn't exist
			Build())

		result.IncrementPatternsFound()
	}
}

// runAICheck executes an AI-powered check.
func (w *YAMLWorker) runAICheck(ctx context.Context, codebase health.CodebaseContext, check AICheck, result *WorkerResultBuilder) error {
	// Find code matching the pattern
	opts := PatternOptions{
		FilePattern: check.FilePattern,
		MaxMatches:  check.MaxMatches,
	}

	matches, err := FindPattern(codebase.RootPath, check.Pattern, opts)
	if err != nil {
		return err
	}

	// Batch matches for AI analysis
	for _, match := range matches {
		// Call AI for each match (in production, you'd batch these)
		response, err := CallAI(ctx, AIRequest{
			Prompt: fmt.Sprintf("%s\n\nCode at %s:%d:\n```\n%s\n```", check.Prompt, match.File, match.Line, match.Context),
		})

		if err != nil {
			continue // Skip failures
		}

		result.IncrementAICalls()
		result.AddTokensUsed(response.TokensUsed)
		result.AddCost(response.EstimatedCost)

		// Create issue if AI found problems
		result.AddIssue(NewIssue().
			WithTitle(fmt.Sprintf("%s: %s", check.Name, match.File)).
			WithDescription(fmt.Sprintf("AI analysis:\n\n%s", response.Text)).
			WithCategory(check.Category).
			WithFile(match.File, match.Line).
			WithPriority(check.Priority).
			WithTags(check.Tags...).
			WithTag("ai-detected").
			WithConfidence(0.6). // Medium confidence for AI
			WithEvidence("ai_prompt", check.Prompt).
			WithEvidence("ai_response", response.Text).
			Build())

		result.IncrementPatternsFound()
	}

	return nil
}

// LoadYAMLWorkersFromDir loads all YAML workers from a directory.
//
// Example:
//
//	workers, err := sdk.LoadYAMLWorkersFromDir(".vc/workers/")
func LoadYAMLWorkersFromDir(dir string) ([]*YAMLWorker, error) {
	var workers []*YAMLWorker

	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return workers, nil // Directory doesn't exist, return empty list
		}
		return nil, err
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		// Only load .yaml and .yml files
		name := entry.Name()
		if filepath.Ext(name) != ".yaml" && filepath.Ext(name) != ".yml" {
			continue
		}

		path := filepath.Join(dir, name)
		worker, err := LoadYAMLWorker(path)
		if err != nil {
			// Log error but continue loading other workers
			continue
		}

		workers = append(workers, worker)
	}

	return workers, nil
}
