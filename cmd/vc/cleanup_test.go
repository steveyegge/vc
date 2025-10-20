package main

import (
	"testing"
)

func TestFormatNumber(t *testing.T) {
	tests := []struct {
		input    int
		expected string
	}{
		// Small numbers
		{0, "0"},
		{1, "1"},
		{42, "42"},
		{999, "999"},

		// Thousands
		{1000, "1,000"},
		{1001, "1,001"},
		{9999, "9,999"},
		{10000, "10,000"},
		{99999, "99,999"},
		{999999, "999,999"},

		// Millions
		{1000000, "1,000,000"},
		{1234567, "1,234,567"},
		{9999999, "9,999,999"},
		{99999999, "99,999,999"},
		{999999999, "999,999,999"},

		// Billions
		{1000000000, "1,000,000,000"},
		{1234567890, "1,234,567,890"},
		{2147483647, "2,147,483,647"}, // Max int32

		// Negative numbers
		{-1, "-1"},
		{-1000, "-1,000"},
		{-1234567, "-1,234,567"},
	}

	for _, tt := range tests {
		result := formatNumber(tt.input)
		if result != tt.expected {
			t.Errorf("formatNumber(%d) = %s; want %s", tt.input, result, tt.expected)
		}
	}
}
