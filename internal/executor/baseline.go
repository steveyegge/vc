package executor

import (
	"crypto/sha256"
	"fmt"
	"regexp"
	"strings"
)

// Baseline issue IDs for quality gate failures (vc-210, vc-261)
const (
	BaselineTestIssueID  = "vc-baseline-test"
	BaselineLintIssueID  = "vc-baseline-lint"
	BaselineBuildIssueID = "vc-baseline-build"
)

// IsBaselineIssue returns true if the given issue ID is a baseline issue (vc-261).
// Baseline issues are special issues created by preflight quality gates when
// they detect failures in the baseline (main branch).
func IsBaselineIssue(issueID string) bool {
	return issueID == BaselineTestIssueID ||
		issueID == BaselineLintIssueID ||
		issueID == BaselineBuildIssueID
}

// GetGateType extracts the gate type from a baseline issue ID (vc-261).
// For example, "vc-baseline-test" returns "test".
// Returns empty string if the issue ID is not a baseline issue.
func GetGateType(issueID string) string {
	if !IsBaselineIssue(issueID) {
		return ""
	}
	return strings.TrimPrefix(issueID, "vc-baseline-")
}

// TestFailure represents a parsed individual test failure (vc-ebd9)
type TestFailure struct {
	Package string // Package path (e.g., "github.com/steveyegge/vc/internal/executor")
	Test    string // Test name (e.g., "TestExecutorRun")
	Error   string // Error message/output
}

// ParseTestFailures extracts individual test failures from Go test output (vc-ebd9)
// This parses standard Go test output format:
//   --- FAIL: TestName (0.00s)
//       file.go:123: error message
//
// Returns a slice of TestFailure structs, one for each failing test
func ParseTestFailures(output string) []TestFailure {
	var failures []TestFailure

	// Regex pattern to match Go test failures
	// Matches: --- FAIL: TestName (0.00s)
	failPattern := regexp.MustCompile(`(?m)^--- FAIL: (\S+) \([\d.]+s\)`)

	// Find all test failure markers
	matches := failPattern.FindAllStringSubmatchIndex(output, -1)
	if len(matches) == 0 {
		return failures
	}

	// Extract package path from output (appears at the top)
	// Format: "FAIL	github.com/steveyegge/vc/internal/executor	0.123s"
	packagePattern := regexp.MustCompile(`(?m)^FAIL\s+(\S+)\s+`)
	packageMatches := packagePattern.FindStringSubmatch(output)
	packagePath := ""
	if len(packageMatches) > 1 {
		packagePath = packageMatches[1]
	}

	// For each test failure, extract the test name and error output
	for i, match := range matches {
		testName := output[match[2]:match[3]]

		// Find the start and end of this test's output
		startIdx := match[1] // End of "--- FAIL: TestName (0.00s)" line
		endIdx := len(output)

		// If there's another test failure after this, end at that point
		if i+1 < len(matches) {
			endIdx = matches[i+1][0]
		}

		// Extract error output for this test
		errorOutput := strings.TrimSpace(output[startIdx:endIdx])

		failures = append(failures, TestFailure{
			Package: packagePath,
			Test:    testName,
			Error:   errorOutput,
		})
	}

	return failures
}

// ComputeFailureSignature computes a stable signature for a test failure (vc-ebd9)
// The signature is used for deduplication - same test failure = same signature
//
// Signature components:
// - Package path (stable)
// - Test name (stable)
// - Normalized error pattern (strips line numbers, timestamps, temp paths)
func ComputeFailureSignature(failure TestFailure) string {
	normalizedError := normalizeError(failure.Error)

	// Combine components and hash
	input := fmt.Sprintf("%s|%s|%s", failure.Package, failure.Test, normalizedError)
	hash := sha256.Sum256([]byte(input))

	return fmt.Sprintf("%x", hash[:16]) // Use first 16 bytes (32 hex chars)
}

// normalizeError removes unstable elements from error messages (vc-ebd9)
// This ensures that the same logical error produces the same signature
// even if line numbers, timestamps, or temp paths differ
func normalizeError(errMsg string) string {
	normalized := errMsg

	// Remove line numbers (e.g., "file.go:123" -> "file.go:XXX")
	lineNumPattern := regexp.MustCompile(`(\w+\.go):\d+`)
	normalized = lineNumPattern.ReplaceAllString(normalized, "${1}:XXX")

	// Remove timestamps (e.g., "2024-11-04 12:34:56" -> "TIMESTAMP")
	timestampPattern := regexp.MustCompile(`\d{4}-\d{2}-\d{2}[T\s]\d{2}:\d{2}:\d{2}`)
	normalized = timestampPattern.ReplaceAllString(normalized, "TIMESTAMP")

	// Remove temp paths (e.g., "/tmp/go-build123" -> "/tmp/go-buildXXX")
	tempPathPattern := regexp.MustCompile(`/tmp/[\w-]+\d+`)
	normalized = tempPathPattern.ReplaceAllString(normalized, "/tmp/XXXXX")

	// Remove hex addresses (e.g., "0x1a2b3c4d" -> "0xXXXXXXXX")
	hexAddrPattern := regexp.MustCompile(`0x[0-9a-fA-F]+`)
	normalized = hexAddrPattern.ReplaceAllString(normalized, "0xXXXXXXXX")

	// Remove goroutine IDs (e.g., "goroutine 123" -> "goroutine XXX")
	goroutinePattern := regexp.MustCompile(`goroutine \d+`)
	normalized = goroutinePattern.ReplaceAllString(normalized, "goroutine XXX")

	// Remove durations (e.g., "(took 1.234s)" -> "(took X.XXXs)")
	durationPattern := regexp.MustCompile(`\d+\.?\d*[mµn]?s`)
	normalized = durationPattern.ReplaceAllString(normalized, "X.XXXs")

	// Normalize whitespace
	normalized = strings.TrimSpace(normalized)
	normalized = regexp.MustCompile(`\s+`).ReplaceAllString(normalized, " ")

	return normalized
}
