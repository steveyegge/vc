package main

import (
	"testing"
)

func TestIsWellFormed(t *testing.T) {
	tests := []struct {
		name     string
		ac       string
		expected bool
	}{
		{
			name:     "well-formed single scenario",
			ac:       "WHEN creating an issue THEN it persists to SQLite database",
			expected: true,
		},
		{
			name: "well-formed multiple scenarios",
			ac: `- WHEN creating an issue THEN it persists to SQLite database
- WHEN reading non-existent issue THEN NotFoundError is returned
- WHEN transaction fails THEN retry 3 times with exponential backoff`,
			expected: true,
		},
		{
			name:     "case insensitive",
			ac:       "when creating an issue then it persists to database",
			expected: true,
		},
		{
			name:     "mixed case",
			ac:       "When creating an issue Then it persists to database",
			expected: true,
		},
		{
			name:     "missing WHEN",
			ac:       "Creating an issue THEN it persists to database",
			expected: false,
		},
		{
			name:     "missing THEN",
			ac:       "WHEN creating an issue it persists to database",
			expected: false,
		},
		{
			name:     "vague criteria - no WHEN/THEN",
			ac:       "Test storage thoroughly",
			expected: false,
		},
		{
			name:     "vague criteria - just description",
			ac:       "Handle errors properly and make it robust",
			expected: false,
		},
		{
			name:     "empty string",
			ac:       "",
			expected: false,
		},
		{
			name: "partial scenario - only one has WHEN...THEN",
			ac: `- WHEN creating an issue THEN it persists to database
- Test edge cases thoroughly`,
			expected: true, // This returns true because at least one scenario is well-formed
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isWellFormed(tt.ac)
			if result != tt.expected {
				t.Errorf("isWellFormed(%q) = %v, want %v", tt.ac, result, tt.expected)
			}
		})
	}
}
