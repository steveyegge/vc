package discovery

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/steveyegge/vc/internal/health"
)

// ContextBuilder builds a CodebaseContext by analyzing the project structure.
// This is done once and shared by all discovery workers for efficiency.
type ContextBuilder struct {
	// Root directory to analyze
	RootDir string

	// Paths to exclude (glob patterns)
	ExcludePaths []string
}

// NewContextBuilder creates a new codebase context builder.
func NewContextBuilder(rootDir string, excludePaths []string) *ContextBuilder {
	if excludePaths == nil {
		excludePaths = []string{
			"vendor/",
			"node_modules/",
			".git/",
			"*.pb.go",
			"*_generated.go",
		}
	}

	return &ContextBuilder{
		RootDir:      rootDir,
		ExcludePaths: excludePaths,
	}
}

// Build constructs a CodebaseContext by scanning the project.
func (b *ContextBuilder) Build(ctx context.Context) (health.CodebaseContext, error) {
	// Collect file statistics
	var fileSizes []float64
	languageFiles := make(map[string]int)
	totalFiles := 0
	totalLines := 0

	err := filepath.WalkDir(b.RootDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		// Get relative path
		relPath, err := filepath.Rel(b.RootDir, path)
		if err != nil {
			return err
		}

		// Check for exclusions
		if b.shouldExclude(relPath, d) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		// Skip directories
		if d.IsDir() {
			return nil
		}

		// Get file info
		info, err := d.Info()
		if err != nil {
			return nil // Skip files we can't stat
		}

		// Record file size
		size := float64(info.Size())
		fileSizes = append(fileSizes, size)
		totalFiles++

		// Detect language
		lang := detectLanguage(path)
		if lang != "" {
			languageFiles[lang]++
		}

		// Count lines (for text files only)
		if isTextFile(path) {
			lines, err := countLines(path)
			if err == nil {
				totalLines += lines
			}
		}

		return nil
	})

	if err != nil {
		return health.CodebaseContext{}, fmt.Errorf("failed to walk directory: %w", err)
	}

	// Calculate file size distribution
	fileSizeDistribution := calculateDistribution(fileSizes)

	return health.CodebaseContext{
		FileSizeDistribution:  fileSizeDistribution,
		ComplexityDistribution: health.Distribution{}, // TODO: Calculate if needed
		DuplicationPercentage: 0.0,                    // TODO: Calculate if needed
		LanguageBreakdown:     languageFiles,
		TotalFiles:            totalFiles,
		TotalLines:            totalLines,
		NamingConventions:     []health.Pattern{},
		ArchitecturalPatterns: []health.Pattern{},
		GrowthRate:            0.0,
		RecentChanges:         []health.FileChange{},
	}, nil
}

// shouldExclude checks if a path should be excluded from analysis.
func (b *ContextBuilder) shouldExclude(relPath string, d fs.DirEntry) bool {
	// Always exclude hidden files and directories
	if strings.HasPrefix(filepath.Base(relPath), ".") && relPath != "." {
		return true
	}

	// Check against exclude patterns
	for _, pattern := range b.ExcludePaths {
		if matchesPattern(relPath, pattern, d.IsDir()) {
			return true
		}
	}

	return false
}

// matchesPattern checks if a path matches an exclude pattern.
func matchesPattern(path, pattern string, isDir bool) bool {
	// Directory patterns (e.g., "vendor/")
	if strings.HasSuffix(pattern, "/") {
		if isDir && (path == strings.TrimSuffix(pattern, "/") || strings.HasPrefix(path, pattern)) {
			return true
		}
		if strings.HasPrefix(path, pattern) || strings.Contains(path, "/"+pattern) {
			return true
		}
		return false
	}

	// Glob patterns (e.g., "*.pb.go")
	if strings.Contains(pattern, "*") {
		matched, _ := filepath.Match(pattern, filepath.Base(path))
		return matched
	}

	// Exact match
	return path == pattern || strings.HasPrefix(path, pattern+"/")
}

// detectLanguage returns the programming language based on file extension.
func detectLanguage(path string) string {
	ext := strings.ToLower(filepath.Ext(path))

	languageMap := map[string]string{
		".go":    "Go",
		".py":    "Python",
		".js":    "JavaScript",
		".ts":    "TypeScript",
		".java":  "Java",
		".c":     "C",
		".cpp":   "C++",
		".cc":    "C++",
		".h":     "C/C++ Header",
		".hpp":   "C++ Header",
		".rs":    "Rust",
		".rb":    "Ruby",
		".php":   "PHP",
		".swift": "Swift",
		".kt":    "Kotlin",
		".scala": "Scala",
		".sh":    "Shell",
		".bash":  "Shell",
		".zsh":   "Shell",
		".sql":   "SQL",
		".r":     "R",
		".m":     "Objective-C",
		".cs":    "C#",
		".fs":    "F#",
		".hs":    "Haskell",
		".elm":   "Elm",
		".erl":   "Erlang",
		".ex":    "Elixir",
		".clj":   "Clojure",
		".lua":   "Lua",
		".vim":   "VimScript",
		".pl":    "Perl",
	}

	if lang, ok := languageMap[ext]; ok {
		return lang
	}

	return ""
}

// isTextFile checks if a file is likely a text file.
func isTextFile(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))

	textExtensions := []string{
		".go", ".py", ".js", ".ts", ".java", ".c", ".cpp", ".cc", ".h", ".hpp",
		".rs", ".rb", ".php", ".swift", ".kt", ".scala", ".sh", ".bash", ".zsh",
		".sql", ".r", ".m", ".cs", ".fs", ".hs", ".elm", ".erl", ".ex", ".clj",
		".lua", ".vim", ".pl", ".txt", ".md", ".json", ".yaml", ".yml", ".toml",
		".xml", ".html", ".css", ".scss", ".sass", ".less", ".proto", ".thrift",
	}

	for _, textExt := range textExtensions {
		if ext == textExt {
			return true
		}
	}

	return false
}

// countLines counts the number of lines in a text file.
func countLines(path string) (int, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}

	// Count newlines
	lines := 0
	for _, b := range data {
		if b == '\n' {
			lines++
		}
	}

	// If file doesn't end with newline, add 1
	if len(data) > 0 && data[len(data)-1] != '\n' {
		lines++
	}

	return lines, nil
}

// calculateDistribution computes statistical distribution from a set of values.
func calculateDistribution(values []float64) health.Distribution {
	if len(values) == 0 {
		return health.Distribution{}
	}

	// Sort values
	sorted := make([]float64, len(values))
	copy(sorted, values)
	sort.Float64s(sorted)

	// Calculate basic statistics
	sum := 0.0
	min := sorted[0]
	max := sorted[len(sorted)-1]

	for _, v := range sorted {
		sum += v
	}
	mean := sum / float64(len(sorted))

	// Calculate median
	median := 0.0
	if len(sorted)%2 == 0 {
		median = (sorted[len(sorted)/2-1] + sorted[len(sorted)/2]) / 2
	} else {
		median = sorted[len(sorted)/2]
	}

	// Calculate standard deviation
	sumSquaredDiff := 0.0
	for _, v := range sorted {
		diff := v - mean
		sumSquaredDiff += diff * diff
	}
	stdDev := 0.0
	if len(sorted) > 1 {
		variance := sumSquaredDiff / float64(len(sorted)-1)
		stdDev = variance
		if stdDev > 0 {
			// Simple square root approximation
			// For better accuracy, use math.Sqrt
			x := stdDev
			for i := 0; i < 10; i++ {
				x = (x + stdDev/x) / 2
			}
			stdDev = x
		}
	}

	// Calculate percentiles
	p95Index := int(float64(len(sorted)) * 0.95)
	if p95Index >= len(sorted) {
		p95Index = len(sorted) - 1
	}
	p95 := sorted[p95Index]

	p99Index := int(float64(len(sorted)) * 0.99)
	if p99Index >= len(sorted) {
		p99Index = len(sorted) - 1
	}
	p99 := sorted[p99Index]

	return health.Distribution{
		Mean:   mean,
		Median: median,
		StdDev: stdDev,
		P95:    p95,
		P99:    p99,
		Min:    min,
		Max:    max,
		Count:  len(sorted),
	}
}
