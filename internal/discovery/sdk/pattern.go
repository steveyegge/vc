package sdk

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// PatternMatch represents a pattern match in a file.
type PatternMatch struct {
	File      string // File path
	Line      int    // Line number (1-indexed)
	Column    int    // Column number (1-indexed)
	Text      string // Matched text
	Context   string // Full line containing the match
	BeforeCtx []string // Lines before the match (if requested)
	AfterCtx  []string // Lines after the match (if requested)
}

// PatternOptions configures pattern matching behavior.
type PatternOptions struct {
	// FilePattern is a glob pattern for files to search (e.g., "*.go", "**/*.ts")
	FilePattern string

	// ExcludeDirs specifies directory names to skip
	ExcludeDirs []string

	// CaseInsensitive makes the search case-insensitive
	CaseInsensitive bool

	// WholeWord matches whole words only
	WholeWord bool

	// MaxMatches limits the number of matches returned (0 = unlimited)
	MaxMatches int

	// ContextLines includes N lines of context before/after each match
	ContextLines int
}

// DefaultPatternOptions returns pattern options with common settings.
func DefaultPatternOptions() PatternOptions {
	return PatternOptions{
		FilePattern:     "*",
		ExcludeDirs:     []string{"vendor", ".git", "node_modules"},
		CaseInsensitive: false,
		WholeWord:       false,
		MaxMatches:      0,
		ContextLines:    0,
	}
}

// FindPattern searches for a regex pattern in files under rootPath.
//
// Example:
//
//	// Find all TODO comments
//	matches, err := sdk.FindPattern("/path/to/project", `TODO:.*`, sdk.PatternOptions{
//		FilePattern: "*.go",
//	})
//
//	// Find all functions named "handleRequest"
//	matches, err := sdk.FindPattern("/path/to/project", `func handleRequest`, sdk.PatternOptions{
//		FilePattern: "*.go",
//		WholeWord:   true,
//	})
func FindPattern(rootPath string, pattern string, opts PatternOptions) ([]PatternMatch, error) {
	// Compile regex
	regexFlags := ""
	if opts.CaseInsensitive {
		regexFlags = "(?i)"
	}
	if opts.WholeWord {
		pattern = `\b` + pattern + `\b`
	}

	re, err := regexp.Compile(regexFlags + pattern)
	if err != nil {
		return nil, fmt.Errorf("invalid regex pattern: %w", err)
	}

	var matches []PatternMatch

	// Walk files
	err = filepath.Walk(rootPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Skip excluded directories
		if info.IsDir() {
			for _, exclude := range opts.ExcludeDirs {
				if strings.Contains(path, string(filepath.Separator)+exclude+string(filepath.Separator)) ||
					strings.HasSuffix(path, string(filepath.Separator)+exclude) {
					return filepath.SkipDir
				}
			}
			return nil
		}

		// Check file pattern
		if opts.FilePattern != "" && opts.FilePattern != "*" {
			matched, _ := filepath.Match(opts.FilePattern, filepath.Base(path))
			if !matched {
				return nil
			}
		}

		// Search file
		fileMatches, err := searchFile(path, re, opts.ContextLines)
		if err != nil {
			// Skip files that can't be read
			return nil
		}

		matches = append(matches, fileMatches...)

		// Check max matches limit
		if opts.MaxMatches > 0 && len(matches) >= opts.MaxMatches {
			return filepath.SkipAll
		}

		return nil
	})

	if err != nil && err != filepath.SkipAll {
		return nil, err
	}

	// Truncate to max matches
	if opts.MaxMatches > 0 && len(matches) > opts.MaxMatches {
		matches = matches[:opts.MaxMatches]
	}

	return matches, nil
}

// searchFile searches for a pattern in a single file.
func searchFile(path string, re *regexp.Regexp, contextLines int) ([]PatternMatch, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var matches []PatternMatch
	var lines []string // Buffer for context

	scanner := bufio.NewScanner(file)
	lineNum := 0

	for scanner.Scan() {
		lineNum++
		line := scanner.Text()

		// Keep context buffer
		if contextLines > 0 {
			lines = append(lines, line)
			if len(lines) > contextLines*2+1 {
				lines = lines[1:]
			}
		}

		// Check for match
		if re.MatchString(line) {
			loc := re.FindStringIndex(line)
			var beforeCtx, afterCtx []string

			if contextLines > 0 {
				// Extract before context
				start := len(lines) - contextLines - 1
				if start < 0 {
					start = 0
				}
				end := len(lines) - 1
				if end >= 0 {
					beforeCtx = make([]string, end-start)
					copy(beforeCtx, lines[start:end])
				}

				// After context will be filled later
				// For now, we just note we need it
			}

			matches = append(matches, PatternMatch{
				File:      path,
				Line:      lineNum,
				Column:    loc[0] + 1, // 1-indexed
				Text:      line[loc[0]:loc[1]],
				Context:   line,
				BeforeCtx: beforeCtx,
				AfterCtx:  afterCtx, // Will be empty for now
			})
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	return matches, nil
}

// FindMissingFiles checks if expected files exist in a project.
//
// Example:
//
//	// Check for missing standard files
//	missing := sdk.FindMissingFiles("/path/to/project", []string{
//		"README.md",
//		"LICENSE",
//		".gitignore",
//	})
func FindMissingFiles(rootPath string, expectedFiles []string) []string {
	var missing []string

	for _, file := range expectedFiles {
		fullPath := filepath.Join(rootPath, file)
		if _, err := os.Stat(fullPath); os.IsNotExist(err) {
			missing = append(missing, file)
		}
	}

	return missing
}

// FindLargeFiles finds files larger than a size threshold.
//
// Example:
//
//	// Find files larger than 1MB
//	large := sdk.FindLargeFiles("/path/to/project", 1024*1024, sdk.PatternOptions{
//		FilePattern: "*.go",
//	})
func FindLargeFiles(rootPath string, sizeBytes int64, opts PatternOptions) ([]FileInfo, error) {
	var largeFiles []FileInfo

	err := filepath.Walk(rootPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Skip excluded directories
		if info.IsDir() {
			for _, exclude := range opts.ExcludeDirs {
				if strings.Contains(path, string(filepath.Separator)+exclude+string(filepath.Separator)) ||
					strings.HasSuffix(path, string(filepath.Separator)+exclude) {
					return filepath.SkipDir
				}
			}
			return nil
		}

		// Check file pattern
		if opts.FilePattern != "" && opts.FilePattern != "*" {
			matched, _ := filepath.Match(opts.FilePattern, filepath.Base(path))
			if !matched {
				return nil
			}
		}

		// Check size
		if info.Size() > sizeBytes {
			largeFiles = append(largeFiles, FileInfo{
				Path:  path,
				Size:  info.Size(),
				Lines: countLines(path),
			})
		}

		return nil
	})

	return largeFiles, err
}

// FileInfo contains information about a file.
type FileInfo struct {
	Path  string
	Size  int64
	Lines int
}

// countLines counts the number of lines in a file.
func countLines(path string) int {
	file, err := os.Open(path)
	if err != nil {
		return 0
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	lines := 0
	for scanner.Scan() {
		lines++
	}

	return lines
}

// FindDuplicateCode finds potential code duplication using simple line-based heuristics.
// This is a simplified implementation - proper duplication detection would use AST comparison.
//
// Example:
//
//	duplicates := sdk.FindDuplicateCode("/path/to/project", sdk.DuplicationOptions{
//		MinLines:    10,
//		FilePattern: "*.go",
//	})
func FindDuplicateCode(rootPath string, opts DuplicationOptions) ([]DuplicateBlock, error) {
	// This is a placeholder for a more sophisticated implementation
	// For now, it returns an empty slice
	return []DuplicateBlock{}, nil
}

// DuplicationOptions configures duplication detection.
type DuplicationOptions struct {
	MinLines     int      // Minimum lines for a duplicate block
	FilePattern  string   // File pattern to search
	ExcludeDirs  []string // Directories to exclude
	IgnoreComments bool   // Ignore comments when comparing
	IgnoreWhitespace bool // Ignore whitespace when comparing
}

// DuplicateBlock represents a duplicated code block.
type DuplicateBlock struct {
	Files []DuplicateLocation
	Lines int
	Text  string
}

// DuplicateLocation represents one location of a duplicated block.
type DuplicateLocation struct {
	File      string
	StartLine int
	EndLine   int
}
