package codereview

import (
	"testing"
)

func TestShouldExcludeFile(t *testing.T) {
	tests := []struct {
		name        string
		filePath    string
		maxLines    int
		wantExclude bool
		wantReason  string
	}{
		{
			name:        "generated protobuf file",
			filePath:    "internal/api/service.pb.go",
			maxLines:    0,
			wantExclude: true,
			wantReason:  "generated protobuf file",
		},
		{
			name:        "generated go file",
			filePath:    "internal/gen/types.gen.go",
			maxLines:    0,
			wantExclude: true,
			wantReason:  "generated code",
		},
		{
			name:        "vendor code",
			filePath:    "vendor/github.com/some/package/file.go",
			maxLines:    0,
			wantExclude: true,
			wantReason:  "vendor code",
		},
		{
			name:        "third party code",
			filePath:    "third_party/lib/code.go",
			maxLines:    0,
			wantExclude: true,
			wantReason:  "third-party code",
		},
		{
			name:        "binary file",
			filePath:    "assets/image.png",
			maxLines:    0,
			wantExclude: true,
			wantReason:  "binary/media file",
		},
		{
			name:        "normal go file",
			filePath:    "internal/executor/executor.go",
			maxLines:    0,
			wantExclude: false,
			wantReason:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotExclude, gotReason := ShouldExcludeFile(tt.filePath, tt.maxLines)
			if gotExclude != tt.wantExclude {
				t.Errorf("ShouldExcludeFile() exclude = %v, want %v", gotExclude, tt.wantExclude)
			}
			if tt.wantExclude && gotReason != tt.wantReason {
				t.Errorf("ShouldExcludeFile() reason = %v, want %v", gotReason, tt.wantReason)
			}
		})
	}
}

func TestIsCodeFile(t *testing.T) {
	tests := []struct {
		name     string
		filePath string
		want     bool
	}{
		{"go file", "main.go", true},
		{"typescript file", "app.ts", true},
		{"javascript file", "script.js", true},
		{"python file", "main.py", true},
		{"text file", "README.txt", false},
		{"markdown file", "README.md", false},
		{"json file", "package.json", false},
		{"yaml file", "config.yaml", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isCodeFile(tt.filePath)
			if got != tt.want {
				t.Errorf("isCodeFile(%q) = %v, want %v", tt.filePath, got, tt.want)
			}
		})
	}
}

func TestRandomSample(t *testing.T) {
	items := []string{"a", "b", "c", "d", "e", "f", "g", "h", "i", "j"}

	// Test sampling fewer items than available
	result := randomSample(items, 5)
	if len(result) != 5 {
		t.Errorf("randomSample() returned %d items, want 5", len(result))
	}

	// Test sampling all items
	result = randomSample(items, 10)
	if len(result) != 10 {
		t.Errorf("randomSample() returned %d items, want 10", len(result))
	}

	// Test sampling more items than available
	result = randomSample(items, 20)
	if len(result) != 10 {
		t.Errorf("randomSample() returned %d items, want 10", len(result))
	}

	// Verify all items are from the original list
	itemMap := make(map[string]bool)
	for _, item := range items {
		itemMap[item] = true
	}
	for _, item := range result {
		if !itemMap[item] {
			t.Errorf("randomSample() returned unexpected item: %s", item)
		}
	}
}
