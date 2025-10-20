# Linting Exceptions

This document tracks known linting warnings that are acceptable and should not be "fixed".

## Status: 9 acceptable warnings (2025-10-19)

All remaining linting warnings are either intentional design choices or low-priority style suggestions that don't affect correctness.

## Unused Code (2 issues)

### 1. `internal/executor/results.go:1020` - `createCodeReviewIssue` function

**Warning**: `func (*ResultsProcessor).createCodeReviewIssue is unused`

**Status**: ACCEPTED - Intentional for future use

**Reason**: This function implements AI-driven code review issue creation. It's complete and tested, but not yet integrated into the main workflow. Will be activated when we enable GitOps and automated code review.

**Location**: `internal/executor/results.go:1020-1078`

---

### 2. `internal/repl/approval.go:13` - `displayMissionPlan` function

**Warning**: `func displayMissionPlan is unused`

**Status**: ACCEPTED - Intentional for future use

**Reason**: Per comment at line 11-12: "This is no longer accessible via slash commands, but kept for potential future use by the AI conversational interface." This function provides formatted mission plan display for manual approval workflows.

**Location**: `internal/repl/approval.go:13-99`

---

## StaticCheck Suggestions (7 issues)

These are style/optimization suggestions from staticcheck. They don't affect correctness and changing them would not meaningfully improve the code.

### 3. `internal/ai/json_parser.go:325` - Tagged switch suggestion

**Warning**: `QF1003: could use tagged switch on firstChar`

**Status**: ACCEPTED - Low priority style suggestion

**Reason**: Simple if/else is clear and readable for this case. Tagged switch would not improve readability.

---

### 4. `internal/ai/supervisor.go:1109` - Tagged switch suggestion

**Warning**: `QF1003: could use tagged switch on issue.IssueSubtype`

**Status**: ACCEPTED - Low priority style suggestion

**Reason**: Single condition check. Tagged switch would be overkill.

---

### 5. `internal/executor/results.go:1008` - De Morgan's law

**Warning**: `QF1001: could apply De Morgan's law`

**Status**: ACCEPTED - Current form is more readable

**Reason**: The current boolean expression is explicit and clear. Applying De Morgan's law would make it less obvious what the condition checks.

---

### 6. `internal/repl/conversation.go:1180` - Unnecessary fmt.Sprintf

**Warning**: `S1039: unnecessary use of fmt.Sprintf`

**Status**: ACCEPTED - Consistency with surrounding code

**Reason**: Part of string building pattern. Could be simplified but not worth the churn.

---

### 7. `internal/repl/conversation.go:1184` - Unnecessary fmt.Sprintf

**Warning**: `S1039: unnecessary use of fmt.Sprintf`

**Status**: ACCEPTED - Consistency with surrounding code

**Reason**: Part of string building pattern. Could be simplified but not worth the churn.

---

### 8. `internal/sandbox/manager.go:296` - Merge conditional assignment

**Warning**: `QF1007: could merge conditional assignment into variable declaration`

**Status**: ACCEPTED - Current form is clearer

**Reason**: Separating declaration from conditional logic improves readability.

---

### 9. `internal/watchdog/monitor_test.go:104` - time.Time.Equal usage

**Warning**: `QF1009: probably want to use time.Time.Equal instead`

**Status**: ACCEPTED - Zero value check is intentional

**Reason**: Checking if time is uninitialized (zero value), not comparing two times. The current approach is correct for this use case.

---

## Fixed Issues (2025-10-19)

The following issues were fixed:

1. ✅ **ineffassign** (2 issues) - `internal/git/event_tracker.go:101, 219` - Removed ineffectual assignments
2. ✅ **staticcheck SA1012** - `internal/ai/planner_test.go:173` - Changed nil Context to context.Background()
3. ✅ **staticcheck S1039** - `internal/executor/results.go:545, 875, 877` - Removed unnecessary fmt.Sprintf
4. ✅ **unused field** - `internal/executor/executor.go:47` - Removed unused `heartbeatTicker` field

## Summary

- **Total warnings**: 9
- **Acceptable**: 9 (100%)
- **Must fix**: 0

All remaining warnings are acceptable and documented. The codebase is in good shape for linting compliance.

## Updating This Document

When new linting warnings appear:

1. Evaluate if the warning indicates a real issue
2. If yes: Fix the issue
3. If no: Add to this document with justification
4. Update the summary counts
5. Commit changes with the code that introduces the warning

## Running the Linter

```bash
# Full lint check
golangci-lint run ./...

# Check specific file
golangci-lint run path/to/file.go

# Errcheck only
errcheck ./...
```
