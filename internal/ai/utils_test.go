package ai

import (
	"strings"
	"testing"
)

func TestTruncateString_ShortString(t *testing.T) {
	// Test with string shorter than maxLen - should return as-is
	s := "short string"
	maxLen := 100
	result := truncateString(s, maxLen)
	if result != s {
		t.Errorf("Expected no truncation for short string, got: %v", result)
	}
}

func TestTruncateString_EdgeCase(t *testing.T) {
	// Test with string only slightly longer than maxLen
	// This triggered the original panic: slice bounds out of range
	s := strings.Repeat("x", 150)
	maxLen := 135

	// Should not panic
	result := truncateString(s, maxLen)

	// Should contain truncation markers
	if !strings.Contains(result, "[... truncated") {
		t.Errorf("Expected truncation markers in result")
	}
}

func TestTruncateString_VeryShortMaxLen(t *testing.T) {
	// Test with very small maxLen that could cause negative chunk sizes
	s := strings.Repeat("x", 200)
	maxLen := 50

	// Should not panic
	result := truncateString(s, maxLen)

	if result == "" {
		t.Errorf("Expected non-empty result")
	}
}

func TestTruncateString_LargeString(t *testing.T) {
	// Test with normal large string
	s := strings.Repeat("abcdefghij", 1000) // 10,000 chars
	maxLen := 1000

	result := truncateString(s, maxLen)

	// Should contain all three sections
	if !strings.Contains(result, "[... truncated") {
		t.Errorf("Expected truncation markers in result")
	}
}
