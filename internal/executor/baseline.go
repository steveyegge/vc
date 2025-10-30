package executor

import "strings"

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
