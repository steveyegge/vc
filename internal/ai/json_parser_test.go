package ai

import (
	"encoding/json"
	"testing"
)

// Test types for validation
type TestResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
}

type TestAssessment struct {
	Phase      string   `json:"phase"`
	Confidence float64  `json:"confidence"`
	Blockers   []string `json:"blockers"`
	NextSteps  []string `json:"nextSteps"`
}

func TestParse_DirectJSON(t *testing.T) {
	input := `{"success": true, "message": "hello"}`

	result := Parse[TestResponse](input)

	if !result.Success {
		t.Fatalf("Expected successful parse, got error: %s", result.Error)
	}

	if !result.Data.Success {
		t.Error("Expected success=true")
	}

	if result.Data.Message != "hello" {
		t.Errorf("Expected message='hello', got '%s'", result.Data.Message)
	}
}

func TestParse_EmptyInput(t *testing.T) {
	result := Parse[TestResponse]("")

	if result.Success {
		t.Error("Expected parse to fail on empty input")
	}

	if result.Error != "empty input" {
		t.Errorf("Expected 'empty input' error, got: %s", result.Error)
	}
}

func TestParse_WithCodeFences(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{
			name: "json fence",
			input: "```json\n" +
				`{"success": true, "message": "fenced"}` + "\n" +
				"```",
		},
		{
			name: "generic fence",
			input: "```\n" +
				`{"success": true, "message": "generic"}` + "\n" +
				"```",
		},
		{
			name: "with preamble",
			input: "Here's the result:\n" +
				"```json\n" +
				`{"success": true, "message": "with preamble"}` + "\n" +
				"```\n" +
				"That's it!",
		},
		{
			name: "javascript fence",
			input: "```javascript\n" +
				`{"success": true, "message": "js fence"}` + "\n" +
				"```",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := Parse[TestResponse](tt.input)

			if !result.Success {
				t.Fatalf("Expected successful parse, got error: %s", result.Error)
			}

			if !result.Data.Success {
				t.Error("Expected success=true")
			}
		})
	}
}

func TestParse_TrailingCommas(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{
			name:  "trailing comma in object",
			input: `{"field1": "value1", "field2": "value2",}`,
		},
		{
			name:  "trailing comma in array",
			input: `{"items": [1, 2, 3,]}`,
		},
		{
			name: "multiple trailing commas",
			input: `{
				"field1": "value1",
				"field2": "value2",
				"nested": {
					"a": 1,
					"b": 2,
				},
			}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := Parse[map[string]any](tt.input)

			if !result.Success {
				t.Fatalf("Expected successful parse after cleanup, got error: %s", result.Error)
			}
		})
	}
}

func TestParse_Comments(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{
			name: "single-line comment",
			input: `{
				"field": "value", // This is a comment
				"other": "data"
			}`,
		},
		{
			name: "multi-line comment",
			input: `{
				"field": "value",
				/* This is a
				   multi-line comment */
				"other": "data"
			}`,
		},
		{
			name: "mixed comments",
			input: `{
				// Start comment
				"field": "value", // inline comment
				/* block */ "other": "data"
			}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := Parse[map[string]any](tt.input)

			if !result.Success {
				t.Fatalf("Expected successful parse after cleanup, got error: %s", result.Error)
			}
		})
	}
}

func TestParse_UnquotedKeys(t *testing.T) {
	input := `{field1: "value1", field2: "value2"}`

	result := Parse[map[string]any](input)

	if !result.Success {
		t.Fatalf("Expected successful parse after cleanup, got error: %s", result.Error)
	}

	if result.Data["field1"] != "value1" {
		t.Error("Expected field1='value1'")
	}
}

func TestParse_MixedContent(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{
			name: "JSON in text",
			input: `Here's the analysis:

				{"success": true, "result": "found it"}

				End of response.`,
		},
		{
			name: "markdown with JSON",
			input: `## Analysis

				The following is the result:

				{"phase": "implementation", "confidence": 0.95}

				### Summary
				Looking good!`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := Parse[map[string]any](tt.input)

			if !result.Success {
				t.Fatalf("Expected successful parse from mixed content, got error: %s", result.Error)
			}
		})
	}
}

func TestParse_ArrayInMixedContent(t *testing.T) {
	// Test extracting an array from text with no surrounding curlies
	// Note: If there are curlies in array elements, extractJSON may match those first
	input := `Results: [1, 2, 3, 4, 5]
		Done.`

	// Parse as generic any type since it's an array
	result := Parse[any](input)

	if !result.Success {
		t.Fatalf("Expected successful parse from mixed content, got error: %s", result.Error)
	}

	// Verify it's an array
	arr, ok := result.Data.([]any)
	if !ok {
		t.Fatalf("Expected array, got %T", result.Data)
	}

	if len(arr) != 5 {
		t.Errorf("Expected 5 items, got %d", len(arr))
	}
}

func TestParse_RealAIResponses(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{
			name: "typical AI response with preamble",
			input: `I'll analyze this for you. Here's the result:

` + "```json" + `
{
  "phase": "implementation",
  "confidence": 0.95,
  "blockers": [],
  "nextSteps": ["Write tests", "Deploy"]
}
` + "```" + `

This analysis shows the system is ready.`,
		},
		{
			name: "AI response with markdown",
			input: `## Analysis Results

The following JSON contains the complete analysis:

` + "```json" + `
{
  "phase": "testing",
  "confidence": 0.85,
  "blockers": ["Missing tests"],
  "nextSteps": ["Add unit tests"]
}
` + "```" + `

### Next Steps
- Review changes
- Deploy`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := Parse[TestAssessment](tt.input)

			if !result.Success {
				t.Fatalf("Expected successful parse, got error: %s", result.Error)
			}

			if result.Data.Phase == "" {
				t.Error("Expected non-empty phase")
			}

			if result.Data.Confidence <= 0 {
				t.Error("Expected positive confidence")
			}

			if result.Data.Blockers == nil {
				t.Error("Expected blockers array (even if empty)")
			}

			if result.Data.NextSteps == nil {
				t.Error("Expected nextSteps array")
			}
		})
	}
}

func TestParseWithValidation_Success(t *testing.T) {
	input := `{"success": true, "message": "valid"}`

	validator := func(data any) bool {
		m, ok := data.(map[string]any)
		if !ok {
			return false
		}
		success, ok := m["success"].(bool)
		return ok && success
	}

	result := ParseWithValidation[map[string]any](input, validator)

	if !result.Success {
		t.Fatalf("Expected successful validation, got error: %s", result.Error)
	}
}

func TestParseWithValidation_FailedValidation(t *testing.T) {
	input := `{"success": false, "message": "invalid"}`

	validator := func(data any) bool {
		m, ok := data.(map[string]any)
		if !ok {
			return false
		}
		success, ok := m["success"].(bool)
		return ok && success // Requires success=true
	}

	result := ParseWithValidation[map[string]any](input, validator)

	if result.Success {
		t.Error("Expected validation to fail")
	}

	if result.Error != "type validation failed" {
		t.Errorf("Expected 'type validation failed' error, got: %s", result.Error)
	}
}

func TestParseOrDefault_Success(t *testing.T) {
	input := `{"success": true, "message": "good"}`
	fallback := TestResponse{Success: false, Message: "fallback"}

	result := ParseOrDefault[TestResponse](input, fallback)

	if !result.Success {
		t.Error("Expected success=true")
	}

	if result.Message != "good" {
		t.Errorf("Expected message='good', got '%s'", result.Message)
	}
}

func TestParseOrDefault_UseFallback(t *testing.T) {
	input := `invalid json{{{`
	fallback := TestResponse{Success: false, Message: "fallback"}

	result := ParseOrDefault[TestResponse](input, fallback)

	if result.Success {
		t.Error("Expected fallback with success=false")
	}

	if result.Message != "fallback" {
		t.Errorf("Expected message='fallback', got '%s'", result.Message)
	}
}

func TestRemoveCodeFences(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "json fence",
			input:    "```json\n{\"test\": true}\n```",
			expected: `{"test": true}`,
		},
		{
			name:     "generic fence",
			input:    "```\n{\"test\": true}\n```",
			expected: `{"test": true}`,
		},
		{
			name:     "javascript fence",
			input:    "```javascript\n{\"test\": true}\n```",
			expected: `{"test": true}`,
		},
		{
			name:     "single backticks",
			input:    "`{\"test\": true}`",
			expected: `{"test": true}`,
		},
		{
			name:     "no fences",
			input:    `{"test": true}`,
			expected: `{"test": true}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := removeCodeFences(tt.input)
			if result != tt.expected {
				t.Errorf("Expected '%s', got '%s'", tt.expected, result)
			}
		})
	}
}

func TestCleanupJSON(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		shouldParse bool
	}{
		{
			name:        "trailing comma in object",
			input:       `{"a": 1, "b": 2,}`,
			shouldParse: true,
		},
		{
			name:        "trailing comma in array",
			input:       `[1, 2, 3,]`,
			shouldParse: true,
		},
		{
			name:        "unquoted keys",
			input:       `{field: "value"}`,
			shouldParse: true,
		},
		{
			name: "single-line comment",
			input: `{
				"field": "value" // comment
			}`,
			shouldParse: true,
		},
		{
			name:        "multi-line comment",
			input:       `{"field": /* comment */ "value"}`,
			shouldParse: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cleaned := cleanupJSON(tt.input)

			var result any
			err := json.Unmarshal([]byte(cleaned), &result)

			if tt.shouldParse && err != nil {
				t.Errorf("Expected cleaned JSON to parse, got error: %v\nCleaned: %s", err, cleaned)
			}
		})
	}
}

func TestExtractJSON(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		hasMatch bool
	}{
		{
			name:     "JSON object in text",
			input:    `Some text {"key": "value"} more text`,
			hasMatch: true,
		},
		{
			name:     "JSON array in text",
			input:    `Results: [1, 2, 3] end`,
			hasMatch: true,
		},
		{
			name:     "no JSON",
			input:    `Just plain text`,
			hasMatch: false,
		},
		{
			name:     "nested JSON",
			input:    `Text {"outer": {"inner": "value"}} end`,
			hasMatch: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractJSON(tt.input)

			if tt.hasMatch && result == "" {
				t.Error("Expected to extract JSON, got empty string")
			}

			if !tt.hasMatch && result != "" {
				t.Errorf("Expected empty string, got: %s", result)
			}

			if result != "" {
				// Verify it's valid JSON
				var parsed any
				err := json.Unmarshal([]byte(result), &parsed)
				if err != nil {
					t.Errorf("Extracted content is not valid JSON: %v\nExtracted: %s", err, result)
				}
			}
		})
	}
}

func TestParse_DisableCleanup(t *testing.T) {
	input := "```json\n{\"test\": true}\n```"

	// With cleanup disabled, code fences should cause parse to fail
	result := Parse[map[string]any](input, ParseOptions{
		EnableCleanup: false,
	})

	if result.Success {
		t.Error("Expected parse to fail with cleanup disabled")
	}
}

func TestParse_WithContext(t *testing.T) {
	input := `invalid json`

	result := Parse[map[string]any](input, ParseOptions{
		Context:   "test operation",
		LogErrors: false, // Disable logs for cleaner test output
	})

	if result.Success {
		t.Error("Expected parse to fail")
	}

	if !containsString(result.Error, "test operation") {
		t.Errorf("Expected error to include context, got: %s", result.Error)
	}
}

func TestTruncate(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		maxLen   int
		expected string
	}{
		{
			name:     "shorter than max",
			input:    "hello",
			maxLen:   10,
			expected: "hello",
		},
		{
			name:     "exactly max",
			input:    "hello",
			maxLen:   5,
			expected: "hello",
		},
		{
			name:     "longer than max",
			input:    "hello world",
			maxLen:   5,
			expected: "hello...",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := truncate(tt.input, tt.maxLen)
			if result != tt.expected {
				t.Errorf("Expected '%s', got '%s'", tt.expected, result)
			}
		})
	}
}

// Helper function
func containsString(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 ||
		(len(s) > 0 && len(substr) > 0 && findSubstring(s, substr)))
}

func findSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
