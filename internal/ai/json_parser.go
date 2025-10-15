// Package ai provides AI supervision functionality for the VC executor.
package ai

import (
	"encoding/json"
	"log/slog"
	"regexp"
	"strings"
)

// ParseResult represents the result of a JSON parse operation.
// It uses a result-style pattern to avoid panics and provide detailed error info.
type ParseResult[T any] struct {
	Success      bool
	Data         T
	Error        string
	OriginalText string
}

// ParseOptions configures JSON parsing behavior.
type ParseOptions struct {
	Context      string // Context for error messages
	EnableCleanup bool   // Enable AI response cleanup strategies (default: true)
	LogErrors    bool   // Log parsing errors (default: true)
}

var defaultOptions = ParseOptions{
	EnableCleanup: true,
	LogErrors:     true,
}

// Parse attempts to parse JSON with multiple fallback strategies.
// It handles common AI response formatting issues like code fences,
// trailing commas, and other quirks in LLM JSON output.
//
// Strategy sequence:
//  1. Direct JSON parse
//  2. Remove code fences and retry
//  3. Fix common JSON issues and retry
//  4. Extract JSON from mixed content and retry
func Parse[T any](text string, opts ...ParseOptions) ParseResult[T] {
	options := defaultOptions
	if len(opts) > 0 {
		options = opts[0]
	}

	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return createError[T]("empty input", text, options.Context)
	}

	// Strategy 1: Direct JSON parse
	result, err := tryDirectParse[T](trimmed)
	if err == nil {
		return ParseResult[T]{
			Success:      true,
			Data:         result,
			OriginalText: text,
		}
	}

	if !options.EnableCleanup {
		return createError[T](err.Error(), text, options.Context)
	}

	if options.LogErrors {
		slog.Debug("Direct JSON parse failed, trying cleanup strategies",
			"error", err.Error(),
			"textPreview", truncate(text, 100),
			"context", options.Context)
	}

	// Strategy 2: Remove code fences and try again
	withoutFences := removeCodeFences(trimmed)
	if withoutFences != trimmed {
		if result, err := tryDirectParse[T](withoutFences); err == nil {
			return ParseResult[T]{
				Success:      true,
				Data:         result,
				OriginalText: text,
			}
		}
	}

	// Strategy 3: Fix common JSON issues
	cleaned := cleanupJSON(withoutFences)
	if result, err := tryDirectParse[T](cleaned); err == nil {
		return ParseResult[T]{
			Success:      true,
			Data:         result,
			OriginalText: text,
		}
	}

	// Strategy 4: Extract JSON from mixed content
	extracted := extractJSON(trimmed)
	if extracted != "" {
		if result, err := tryDirectParse[T](extracted); err == nil {
			return ParseResult[T]{
				Success:      true,
				Data:         result,
				OriginalText: text,
			}
		}
	}

	return createError[T]("all JSON parsing strategies failed", text, options.Context)
}

// ParseWithValidation parses JSON and validates the result with a type guard.
func ParseWithValidation[T any](text string, validator func(any) bool, opts ...ParseOptions) ParseResult[T] {
	parseResult := Parse[any](text, opts...)

	if !parseResult.Success {
		return ParseResult[T]{
			Success:      false,
			Error:        parseResult.Error,
			OriginalText: parseResult.OriginalText,
		}
	}

	if validator(parseResult.Data) {
		// Type assertion should be safe here since validator returned true
		data, ok := parseResult.Data.(T)
		if !ok {
			// This shouldn't happen if the validator is correct, but handle it gracefully
			return createError[T]("type assertion failed after validation", text, "")
		}
		return ParseResult[T]{
			Success:      true,
			Data:         data,
			OriginalText: parseResult.OriginalText,
		}
	}

	options := defaultOptions
	if len(opts) > 0 {
		options = opts[0]
	}

	if options.LogErrors {
		slog.Warn("JSON validation failed",
			"data", parseResult.Data,
			"context", options.Context)
	}

	return createError[T]("type validation failed", text, options.Context)
}

// ParseOrDefault parses JSON and returns a fallback value on error.
func ParseOrDefault[T any](text string, fallback T, opts ...ParseOptions) T {
	result := Parse[T](text, opts...)
	if result.Success {
		return result.Data
	}

	options := defaultOptions
	if len(opts) > 0 {
		options = opts[0]
	}

	if options.LogErrors {
		slog.Debug("JSON parse failed, using fallback",
			"error", result.Error,
			"textPreview", truncate(text, 100),
			"context", options.Context)
	}

	return fallback
}

// tryDirectParse attempts a direct JSON parse without any cleanup.
func tryDirectParse[T any](text string) (T, error) {
	var result T
	err := json.Unmarshal([]byte(text), &result)
	return result, err
}

// removeCodeFences strips markdown code fences from text.
// Handles both ```json and ``` formats, as well as single backticks.
func removeCodeFences(text string) string {
	// Remove ```json ... ``` or ``` ... ``` fences (may appear anywhere in text)
	// First try: fences at start and end of string
	codeFenceRegex := regexp.MustCompile("(?s)^```(?:json|javascript|js)?\\s*\\n?([\\s\\S]*?)\\n?```\\s*$")
	cleaned := codeFenceRegex.ReplaceAllString(text, "$1")

	// If that didn't match, try finding fences anywhere in the text
	if cleaned == text {
		// Look for ```lang\n...\n``` pattern anywhere
		anyFenceRegex := regexp.MustCompile("(?s)```(?:json|javascript|js)?\\s*\\n([\\s\\S]*?)\\n```")
		cleaned = anyFenceRegex.ReplaceAllString(text, "$1")
	}

	// Remove single backticks if they wrap the entire content
	if strings.HasPrefix(cleaned, "`") && strings.HasSuffix(cleaned, "`") {
		cleaned = strings.TrimPrefix(cleaned, "`")
		cleaned = strings.TrimSuffix(cleaned, "`")
	}

	return strings.TrimSpace(cleaned)
}

// cleanupJSON fixes common JSON formatting issues.
// - Removes trailing commas before closing braces/brackets
// - Fixes unquoted object keys (basic cases)
// - Converts single quotes to double quotes
// - Removes // and /* */ comments
func cleanupJSON(text string) string {
	cleaned := strings.TrimSpace(text)

	// Remove trailing commas before closing braces/brackets
	trailingCommaRegex := regexp.MustCompile(",(\\s*[}\\]])")
	cleaned = trailingCommaRegex.ReplaceAllString(cleaned, "$1")

	// Fix unquoted object keys (basic cases)
	// Match: { or , followed by whitespace, then identifier, then :
	unquotedKeyRegex := regexp.MustCompile("([{,]\\s*)([a-zA-Z_$][a-zA-Z0-9_$]*)\\s*:")
	cleaned = unquotedKeyRegex.ReplaceAllString(cleaned, `$1"$2":`)

	// Convert single quotes to double quotes
	// Note: This is a simple replacement and may not handle all edge cases
	cleaned = strings.ReplaceAll(cleaned, "'", "\"")

	// Remove single-line comments (multiline mode: $ matches end of line)
	singleLineCommentRegex := regexp.MustCompile("(?m)//.*$")
	cleaned = singleLineCommentRegex.ReplaceAllString(cleaned, "")

	// Remove multi-line comments
	multiLineCommentRegex := regexp.MustCompile("(?s)/\\*.*?\\*/")
	cleaned = multiLineCommentRegex.ReplaceAllString(cleaned, "")

	return strings.TrimSpace(cleaned)
}

// extractJSON tries to extract JSON objects or arrays from mixed content.
// Returns empty string if no JSON-like content is found.
// Note: Checks for objects first since they're more common in AI responses.
func extractJSON(text string) string {
	// Look for JSON objects first (outermost braces)
	// Most AI responses return objects, not arrays
	objectRegex := regexp.MustCompile("(?s)\\{[\\s\\S]*\\}")
	if match := objectRegex.FindString(text); match != "" {
		return match
	}

	// Look for JSON arrays (outermost brackets)
	arrayRegex := regexp.MustCompile("(?s)\\[[\\s\\S]*\\]")
	if match := arrayRegex.FindString(text); match != "" {
		return match
	}

	return ""
}

// createError creates a failed ParseResult with error details.
func createError[T any](message, text, context string) ParseResult[T] {
	var zero T
	errorMsg := message
	if context != "" {
		errorMsg = context + ": " + message
	}

	return ParseResult[T]{
		Success:      false,
		Data:         zero,
		Error:        errorMsg,
		OriginalText: text,
	}
}

// truncate truncates a string to maxLen characters.
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
