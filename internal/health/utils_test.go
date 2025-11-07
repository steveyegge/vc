package health

import (
	"os"
	"testing"
	"time"
)

// mockFileInfo implements os.FileInfo for testing
type mockFileInfo struct {
	name  string
	isDir bool
}

func (m mockFileInfo) Name() string       { return m.name }
func (m mockFileInfo) Size() int64        { return 0 }
func (m mockFileInfo) Mode() os.FileMode  { return 0 }
func (m mockFileInfo) ModTime() time.Time { return time.Time{} }
func (m mockFileInfo) IsDir() bool        { return m.isDir }
func (m mockFileInfo) Sys() interface{}   { return nil }

func TestShouldExcludePath(t *testing.T) {
	tests := []struct {
		name     string
		relPath  string
		isDir    bool
		patterns []string
		want     bool
	}{
		// Prefix matches
		{
			name:     "prefix match - vendor directory",
			relPath:  "vendor/foo/bar.go",
			patterns: []string{"vendor/"},
			want:     true,
		},
		{
			name:     "prefix match - .git directory",
			relPath:  ".git/config",
			patterns: []string{".git/"},
			want:     true,
		},
		{
			name:     "prefix no match - vendorized not vendor",
			relPath:  "vendorized/foo.go",
			patterns: []string{"vendor/"},
			want:     false,
		},

		// Contains matches (pattern after path separator)
		{
			name:     "contains match - nested vendor",
			relPath:  "src/vendor/foo.go",
			patterns: []string{"vendor/"},
			want:     true,
		},
		{
			name:     "contains match - nested .git",
			relPath:  "project/.git/config",
			patterns: []string{".git/"},
			want:     true,
		},
		{
			name:     "contains no match - without separator",
			relPath:  "myvendor/foo.go",
			patterns: []string{"vendor/"},
			want:     false,
		},

		// Suffix matches
		{
			name:     "suffix match - test file",
			relPath:  "foo_test.go",
			patterns: []string{"_test.go"},
			want:     true,
		},
		{
			name:     "suffix match - protobuf",
			relPath:  "api/proto.pb.go",
			patterns: []string{".pb.go"},
			want:     true,
		},
		{
			name:     "suffix match - generated",
			relPath:  "models/types.gen.go",
			patterns: []string{".gen.go"},
			want:     true,
		},
		{
			name:     "suffix no match - testing.go not _test.go",
			relPath:  "testing.go",
			patterns: []string{"_test.go"},
			want:     false,
		},

		// Multiple patterns
		{
			name:     "multiple patterns - matches first",
			relPath:  "vendor/foo.go",
			patterns: []string{"vendor/", ".git/", "_test.go"},
			want:     true,
		},
		{
			name:     "multiple patterns - matches second",
			relPath:  ".git/config",
			patterns: []string{"vendor/", ".git/", "_test.go"},
			want:     true,
		},
		{
			name:     "multiple patterns - matches last",
			relPath:  "foo_test.go",
			patterns: []string{"vendor/", ".git/", "_test.go"},
			want:     true,
		},
		{
			name:     "multiple patterns - no match",
			relPath:  "src/main.go",
			patterns: []string{"vendor/", ".git/", "_test.go"},
			want:     false,
		},

		// Edge cases
		{
			name:     "empty patterns",
			relPath:  "foo.go",
			patterns: []string{},
			want:     false,
		},
		{
			name:     "nil patterns",
			relPath:  "foo.go",
			patterns: nil,
			want:     false,
		},
		{
			name:     "empty path",
			relPath:  "",
			patterns: []string{"vendor/"},
			want:     false,
		},
		{
			name:     "testdata directory",
			relPath:  "testdata/fixtures.json",
			patterns: []string{"testdata/"},
			want:     true,
		},
		{
			name:     "node_modules",
			relPath:  "node_modules/pkg/index.js",
			patterns: []string{"node_modules/"},
			want:     true,
		},

		// Real-world examples from CruftDetector
		{
			name:     ".beads directory",
			relPath:  ".beads/beads.db",
			patterns: []string{".beads/"},
			want:     true,
		},
		{
			name:     "nested testdata",
			relPath:  "internal/testdata/fixtures.json",
			patterns: []string{"testdata/"},
			want:     true,
		},

		// Directory vs file behavior
		{
			name:     "directory - vendor",
			relPath:  "vendor",
			isDir:    true,
			patterns: []string{"vendor/"},
			want:     false, // "vendor" doesn't match "vendor/" without trailing slash
		},
		{
			name:     "directory - vendor with slash",
			relPath:  "vendor/",
			isDir:    true,
			patterns: []string{"vendor/"},
			want:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			info := mockFileInfo{
				name:  tt.relPath,
				isDir: tt.isDir,
			}

			got := ShouldExcludePath(tt.relPath, info, tt.patterns)
			if got != tt.want {
				t.Errorf("ShouldExcludePath(%q, %v) = %v, want %v",
					tt.relPath, tt.patterns, got, tt.want)
			}
		})
	}
}

// TestShouldExcludePath_Patterns tests specific pattern matching behavior
func TestShouldExcludePath_Patterns(t *testing.T) {
	patterns := []string{
		"vendor/",
		".git/",
		"testdata/",
		"node_modules/",
		".beads/",
		"_test.go",
		".pb.go",
		".gen.go",
	}

	shouldExclude := []string{
		"vendor/foo.go",
		".git/config",
		"testdata/fixtures.json",
		"node_modules/package.json",
		".beads/beads.db",
		"foo_test.go",
		"api.pb.go",
		"types.gen.go",
		"src/vendor/bar.go",
		"project/.git/HEAD",
		"internal/testdata/data.json",
	}

	shouldInclude := []string{
		"src/main.go",
		"internal/storage/db.go",
		"cmd/vc/main.go",
		"testing.go",          // Not _test.go
		"vendorized/foo.go",   // Not vendor/
		"gitignore.go",        // Not .git/
	}

	info := mockFileInfo{name: "test", isDir: false}

	for _, path := range shouldExclude {
		if !ShouldExcludePath(path, info, patterns) {
			t.Errorf("Expected %q to be excluded", path)
		}
	}

	for _, path := range shouldInclude {
		if ShouldExcludePath(path, info, patterns) {
			t.Errorf("Expected %q to be included (not excluded)", path)
		}
	}
}
