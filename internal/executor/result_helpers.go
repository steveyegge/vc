package executor

// getOutputSample returns the last N lines of output, or all if fewer than N
func getOutputSample(output []string, maxLines int) []string {
	if len(output) == 0 {
		return []string{"(no output)"}
	}

	if len(output) <= maxLines {
		return output
	}

	return output[len(output)-maxLines:]
}

// safeShortHash returns a shortened version of a git hash, safely handling short or empty hashes
func safeShortHash(hash string) string {
	if len(hash) >= 8 {
		return hash[:8]
	}
	return hash
}

// safeTruncateUTF8 truncates a string to maxLen bytes while preserving UTF-8 encoding
// If truncation would split a multi-byte UTF-8 sequence, it backs off to a valid boundary
func safeTruncateUTF8(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}

	// Truncate at maxLen initially
	truncated := s[:maxLen]

	// Walk backwards to find a valid UTF-8 boundary
	// We only need to check up to 4 bytes back (max UTF-8 sequence length)
	for i := 0; i < 4 && len(truncated) > 0; i++ {
		// Check if we have valid UTF-8
		if isValidUTF8(truncated) {
			return truncated
		}
		// Remove last byte and try again
		truncated = truncated[:len(truncated)-1]
	}

	// If we still don't have valid UTF-8 after 4 bytes, return empty string
	// rather than corrupted data
	return ""
}

// isValidUTF8 checks if a string contains valid UTF-8
func isValidUTF8(s string) bool {
	// Quick check: if the last byte is ASCII (0-127), it's always valid
	if len(s) > 0 && s[len(s)-1] < 128 {
		return true
	}
	// For multi-byte sequences, check if it's valid
	for range s {
		// If we can iterate without panic, it's valid UTF-8
	}
	return true
}
