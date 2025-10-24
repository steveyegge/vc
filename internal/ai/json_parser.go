// Package ai provides AI supervision functionality for the VC executor.
package ai

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
)

// Pre-compiled regular expressions for performance.
// Compiling regexes on every parse is ~15x slower than using pre-compiled patterns.
var (
	// Code fence patterns
	// Made newlines optional to handle edge cases where AI doesn't include newlines
	// Matches: ```json\n{...}\n```, ```{...}```, ``` json{...}```, etc.
	codeFenceStartRegex = regexp.MustCompile(`(?s)^` + "`" + `{3}(?:json|javascript|js)?\s*\n?([\s\S]*?)\n?` + "`" + `{3}\s*$`)
	codeFenceAnyRegex   = regexp.MustCompile(`(?s)` + "`" + `{3}(?:json|javascript|js)?\s*\n?([\s\S]*?)\n?` + "`" + `{3}`)

	// JSON cleanup patterns
	trailingCommaRegex     = regexp.MustCompile(`,(\s*[}\]])`)
	unquotedKeyRegex       = regexp.MustCompile(`([{,]\s*)([a-zA-Z_$][a-zA-Z0-9_$]*)\s*:`)
	singleLineCommentRegex = regexp.MustCompile(`(?m)//.*$`)
	multiLineCommentRegex  = regexp.MustCompile(`(?s)/\*.*?\*/`)

	// JSON extraction patterns (greedy to capture nested structures)
	// The first-character check in extractJSON prevents over-matching in most cases
	objectRegex = regexp.MustCompile(`(?s)\{[\s\S]*\}`)
	arrayRegex  = regexp.MustCompile(`(?s)\[[\s\S]*\]`)
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
//
// Optional fields use pointers to distinguish between "not set" (nil) and
// "explicitly set to false" (*false). This allows proper defaults without heuristics.
type ParseOptions struct {
	Context       string // Context for error messages
	EnableCleanup *bool  // Enable AI response cleanup strategies (default: true, nil = use default)
	LogErrors     *bool  // Log parsing errors (default: true, nil = use default)
	MaxInputSize  *int   // Maximum input size in bytes (nil = use default of 10MB)
}

// Default values for optional fields
var (
	defaultEnableCleanup = true
	defaultLogErrors     = true
	defaultMaxInputSize  = 10 * 1024 * 1024 // 10MB
)

// Helper functions for creating ParseOptions with common configurations
func BoolPtr(b bool) *bool { return &b }
func boolPtr(b bool) *bool { return BoolPtr(b) } // Keep for backward compat
func IntPtr(i int) *int    { return &i }
func intPtr(i int) *int    { return IntPtr(i) } // Keep for backward compat

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
	// Resolve options with proper defaults (vc-248)
	var options ParseOptions
	if len(opts) > 0 {
		options = opts[0]
	}

	// Set Context (no default)
	context := options.Context

	// Resolve EnableCleanup: use provided value or default
	enableCleanup := defaultEnableCleanup
	if options.EnableCleanup != nil {
		enableCleanup = *options.EnableCleanup
	}

	// Resolve LogErrors: use provided value or default
	logErrors := defaultLogErrors
	if options.LogErrors != nil {
		logErrors = *options.LogErrors
	}

	// Resolve MaxInputSize: use provided value or default
	maxInputSize := defaultMaxInputSize
	if options.MaxInputSize != nil {
		maxInputSize = *options.MaxInputSize
	}

	// Check size limit to prevent memory issues
	if maxInputSize > 0 && len(text) > maxInputSize {
		preview := text
		if len(text) > 1000 {
			preview = text[:1000] + "..."
		}
		return createError[T](
			fmt.Sprintf("input exceeds size limit (%d > %d bytes)", len(text), maxInputSize),
			preview,
			context,
		)
	}

	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return createError[T]("empty input", text, context)
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

	if !enableCleanup {
		return createError[T](err.Error(), text, context)
	}

	if logErrors {
		slog.Debug("Direct JSON parse failed, trying cleanup strategies",
			"error", err.Error(),
			"textPreview", truncate(text, 100),
			"context", context)
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
	// Extract from cleaned version, not original trimmed (which may still have fences)
	extracted := extractJSON(cleaned)
	if extracted != "" {
		if result, err := tryDirectParse[T](extracted); err == nil {
			return ParseResult[T]{
				Success:      true,
				Data:         result,
				OriginalText: text,
			}
		}
	}

	return createError[T]("all JSON parsing strategies failed", text, context)
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

	// Resolve options for error logging (vc-248)
	var options ParseOptions
	if len(opts) > 0 {
		options = opts[0]
	}

	context := options.Context
	logErrors := defaultLogErrors
	if options.LogErrors != nil {
		logErrors = *options.LogErrors
	}

	if logErrors {
		slog.Warn("JSON validation failed",
			"data", parseResult.Data,
			"context", context)
	}

	return createError[T]("type validation failed", text, context)
}

// ParseOrDefault parses JSON and returns a fallback value on error.
func ParseOrDefault[T any](text string, fallback T, opts ...ParseOptions) T {
	result := Parse[T](text, opts...)
	if result.Success {
		return result.Data
	}

	// Resolve options for error logging (vc-248)
	var options ParseOptions
	if len(opts) > 0 {
		options = opts[0]
	}

	context := options.Context
	logErrors := defaultLogErrors
	if options.LogErrors != nil {
		logErrors = *options.LogErrors
	}

	if logErrors {
		slog.Debug("JSON parse failed, using fallback",
			"error", result.Error,
			"textPreview", truncate(text, 100),
			"context", context)
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
	cleaned := codeFenceStartRegex.ReplaceAllString(text, "$1")

	// If that didn't match, try finding fences anywhere in the text
	if cleaned == text {
		// Look for ```lang\n...\n``` pattern anywhere
		cleaned = codeFenceAnyRegex.ReplaceAllString(text, "$1")
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
// - Fixes unquoted object keys (basic cases, JavaScript identifiers only)
// - Removes // and /* */ comments
//
// Note: Does NOT convert single quotes to double quotes, as this would break
// valid JSON containing apostrophes (e.g., {"message": "I'm valid"}).
// Claude/AI models consistently use double quotes in JSON output.
func cleanupJSON(text string) string {
	cleaned := strings.TrimSpace(text)

	// Remove trailing commas before closing braces/brackets
	cleaned = trailingCommaRegex.ReplaceAllString(cleaned, "$1")

	// Fix unquoted object keys (basic cases)
	// Match: { or , followed by whitespace, then JavaScript identifier, then :
	// Limitation: Only handles [a-zA-Z_$][a-zA-Z0-9_$]* pattern
	cleaned = unquotedKeyRegex.ReplaceAllString(cleaned, `$1"$2":`)

	// Remove single-line comments (multiline mode: $ matches end of line)
	cleaned = singleLineCommentRegex.ReplaceAllString(cleaned, "")

	// Remove multi-line comments
	cleaned = multiLineCommentRegex.ReplaceAllString(cleaned, "")

	return strings.TrimSpace(cleaned)
}

// extractJSON tries to extract JSON objects or arrays from mixed content.
// Returns empty string if no JSON-like content is found.
//
// Strategy: Check the first JSON-like character to determine type, preventing
// incorrect matches like extracting {"id": 1} from [{"id": 1}, {"id": 2}].
func extractJSON(text string) string {
	trimmed := strings.TrimSpace(text)

	// Quick check: if text starts with { or [, we know the type
	if len(trimmed) > 0 {
		switch trimmed[0] {
		case '[':
			// It's an array - extract the full array
			if match := arrayRegex.FindString(text); match != "" {
				return match
			}
		case '{':
			// It's an object - extract the object
			if match := objectRegex.FindString(text); match != "" {
				return match
			}
		}
	}

	// Fallback: search for JSON anywhere in mixed content
	// Try objects first (more common in AI responses)
	if match := objectRegex.FindString(text); match != "" {
		return match
	}

	// Try arrays
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
