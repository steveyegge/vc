package health

import (
	"os"
	"strings"
)

// pluralize returns the singular or plural form based on count (vc-4k1z).
func pluralize(count int, singular, plural string) string {
	if count == 1 {
		return singular
	}
	return plural
}

// ShouldExcludePath checks if a path matches any exclude patterns.
// Patterns can be:
//   - Directory prefixes: "vendor/" matches "vendor/foo.go"
//   - File suffixes: "_test.go" matches "foo_test.go"
//   - Anywhere in path: ".git/" matches "src/.git/config"
//
// This function is used by health monitors to filter out files/directories
// that should not be scanned (e.g., vendor, .git, test files).
func ShouldExcludePath(relPath string, info os.FileInfo, patterns []string) bool {
	for _, pattern := range patterns {
		matched := false

		// Match pattern at path component boundaries to avoid false matches
		// e.g., "vendor/" matches "vendor/foo" but not "vendorized/bar"
		if strings.HasPrefix(relPath, pattern) {
			// Pattern at start of path (e.g., "vendor/foo.go")
			matched = true
		} else if strings.Contains(relPath, "/"+pattern) {
			// Pattern after a path separator (e.g., "src/vendor/foo.go")
			matched = true
		} else if strings.HasSuffix(relPath, pattern) {
			// Suffix match for file patterns (e.g., "_test.go", ".pb.go")
			matched = true
		}

		if matched {
			return true
		}
	}

	return false
}
