# Code Review: vc-210 Self-Healing for Baseline Test Failures

**Reviewer**: Claude Code
**Date**: 2025-10-28
**Files Changed**: 5 files (4 modified, 1 new)
**Lines Added**: ~350 lines

## Executive Summary

This is a significant feature addition that enables AI agents to automatically diagnose and fix baseline test failures. The implementation adds specialized prompts, AI diagnosis capabilities, and event tracking infrastructure.

**Overall Assessment**: ✅ APPROVE with minor recommendations

The code follows existing patterns well and provides solid foundation for self-healing. However, there are some edge cases and potential improvements to consider.

---

## File-by-File Review

### 1. `internal/executor/prompt.go` - Baseline Prompt Template

**Changes**: Added 60-line specialized prompt section for baseline test failures

#### Issues Found

🟡 **Minor**: Baseline detection is simplistic
```go
isBaselineIssue := len(ctx.Issue.ID) >= 12 && ctx.Issue.ID[:12] == "vc-baseline-"
```
- **Issue**: Hardcoded magic number (12) and no validation of gate type
- **Risk**: Could match unintended issue IDs like "vc-baseline-foobar-whatever"
- **Fix**: Consider using regex or explicit list of valid gates
```go
// Better approach:
isBaselineIssue := regexp.MustCompile(`^vc-baseline-(test|lint|build)$`).MatchString(ctx.Issue.ID)
```

🟡 **Minor**: Very long template
- **Issue**: The baseline directive adds 60 lines to already large template
- **Impact**: Token usage, harder to maintain
- **Recommendation**: Consider extracting to separate template file if more directives added

#### Strengths

✅ Clear categorization (flaky/real/environmental)
✅ Actionable guidance for each failure type
✅ Verification protocol prevents premature commits
✅ Commit message template ensures good documentation

---

### 2. `internal/ai/test_failure.go` - AI Diagnosis Function (NEW FILE)

**Changes**: New file with 152 lines implementing AI-powered test failure diagnosis

#### Issues Found

🟡 **Minor**: No input validation
```go
func (s *Supervisor) DiagnoseTestFailure(ctx context.Context, issue *types.Issue, testOutput string) (*TestFailureDiagnosis, error) {
    // No checks for nil issue or empty testOutput
```
- **Fix**: Add validation
```go
if issue == nil {
    return nil, fmt.Errorf("issue cannot be nil")
}
if testOutput == "" {
    return nil, fmt.Errorf("test output cannot be empty")
}
```

🟡 **Minor**: Error message could spam logs
```go
return nil, fmt.Errorf("failed to parse test failure diagnosis: %s (response: %s)", parseResult.Error, responseText)
```
- **Issue**: `responseText` could be 4096 tokens of AI output
- **Fix**: Truncate or remove from error
```go
return nil, fmt.Errorf("failed to parse test failure diagnosis: %s (response truncated)", parseResult.Error)
```

🟢 **Info**: Function is not yet integrated
- **Status**: This is expected - it's foundation work
- **TODO**: Track integration in follow-up issue

#### Strengths

✅ Good use of existing retry/circuit breaker patterns
✅ Proper AI usage event logging
✅ Comprehensive diagnosis prompt
✅ FailureType enum prevents typos
✅ Follows supervisor.go patterns consistently

---

### 3. `internal/events/types.go` - Event Types and Data Structures

**Changes**: Added 3 new event types + 3 data structures (50 lines)

#### Issues Found

🟡 **Minor**: FixType and FailureType are strings in events but enum in AI
```go
// In TestFailureDiagnosisData:
FailureType string `json:"failure_type"` // string

// But in test_failure.go:
type FailureType string
const (
    FailureTypeFlaky FailureType = "flaky"
    ...
)
```
- **Issue**: Inconsistency - events use string, AI uses typed enum
- **Risk**: Could lead to typos when emitting events
- **Fix**: Either use enum everywhere or add validation
```go
// Add validation function:
func IsValidFailureType(ft string) bool {
    switch ft {
    case "flaky", "real", "environmental", "unknown":
        return true
    }
    return false
}
```

#### Strengths

✅ Well-documented with clear comments
✅ Consistent with existing event patterns
✅ Proper JSON tags with omitempty
✅ Good field naming and structure

---

### 4. `internal/executor/prompt_test.go` - Test Coverage

**Changes**: Added 3 new test functions (130 lines)

#### Issues Found

🟡 **Minor**: Tests only check string containment
```go
if !strings.Contains(prompt, "# BASELINE TEST FAILURE SELF-HEALING DIRECTIVE") {
    t.Error("Prompt missing 'BASELINE TEST FAILURE SELF-HEALING DIRECTIVE' section")
}
```
- **Issue**: Doesn't verify content quality or correctness
- **Recommendation**: Add tests that verify the actual guidance is useful

🟡 **Minor**: Missing edge case tests
- No test for `"vc-baseline"` (exactly 11 chars - would fail length check)
- No test for invalid gate types like `"vc-baseline-invalid"`
- No test for empty test output

#### Strengths

✅ Good coverage of main use cases (test, lint)
✅ Verifies regular issues don't get baseline sections
✅ Follows existing test patterns
✅ Tests are clear and well-named

---

### 5. `.beads/issues.jsonl` - Issue Tracking

**Changes**: Updated with vc-210 completion

✅ No issues - standard issue tracking update

---

## Cross-Cutting Concerns

### Integration Points

🟢 **Foundation Complete**
- Prompt template detects baseline issues ✓
- AI supervisor can diagnose failures ✓
- Events can track metrics ✓

🟡 **Not Yet Integrated**
- `DiagnoseTestFailure()` is not called anywhere
- Events are defined but not emitted
- No end-to-end test of the flow

**Recommendation**: Create follow-up issue for integration

### Performance

✅ No performance concerns identified
- Baseline detection is O(1) string check
- AI diagnosis only called for baseline issues (rare)
- Event structures are lightweight

### Security

✅ No security issues identified
- No user input validation needed (internal only)
- No sensitive data in events
- AI prompts don't expose secrets

### Documentation

✅ Good inline documentation
- All types have clear comments
- Event data structures documented
- Prompt guidance is self-documenting

🟡 **Minor**: No README update
- New AI diagnosis capability not mentioned in docs
- Recommendation: Add section to CLAUDE.md about self-healing

---

## Test Results

```
=== RUN   TestBuildPrompt_BaselineTestIssue
--- PASS: TestBuildPrompt_BaselineTestIssue (0.00s)
=== RUN   TestBuildPrompt_BaselineLintIssue
--- PASS: TestBuildPrompt_BaselineLintIssue (0.00s)
=== RUN   TestBuildPrompt_RegularIssue_NoBaseline
--- PASS: TestBuildPrompt_RegularIssue_NoBaseline (0.00s)
PASS
ok      github.com/steveyegge/vc/internal/executor     0.293s
```

✅ All tests passing

---

## Recommendations

### Must Fix (None)
No blocking issues found.

### Should Fix

1. **Add input validation to `DiagnoseTestFailure()`**
   ```go
   if issue == nil || testOutput == "" {
       return nil, fmt.Errorf("invalid input")
   }
   ```

2. **Improve baseline issue detection**
   ```go
   var validBaselineIssues = map[string]bool{
       "vc-baseline-test": true,
       "vc-baseline-lint": true,
       "vc-baseline-build": true,
   }
   isBaselineIssue := validBaselineIssues[ctx.Issue.ID]
   ```

3. **Truncate error messages to avoid log spam**
   ```go
   // Don't include full AI response in error
   return nil, fmt.Errorf("failed to parse: %s", parseResult.Error)
   ```

### Nice to Have

4. Add validation helper for FailureType strings
5. Add edge case tests (empty ID, invalid gates, etc.)
6. Create follow-up issue for integration + end-to-end test
7. Update CLAUDE.md with self-healing documentation

---

## Decision

✅ **APPROVE** - Code is ready to commit

This is solid foundation work that follows existing patterns and provides clear value. The issues identified are minor and can be addressed in follow-up work. The self-healing capability is well-designed and will work correctly once integrated.

**Suggested commit message:**
```
Add self-healing foundation for baseline test failures (vc-210)

Implements AI-powered test failure diagnosis and specialized prompts
to enable automatic fixing of baseline test failures.

Changes:
- Enhanced prompt template with baseline test failure directive
- Added TestFailureDiagnosis with AI-powered root cause analysis
- Added event types for tracking self-healing metrics
- Added tests verifying baseline prompt behavior

The foundation is complete but not yet integrated into executor flow.
Follow-up work needed to wire up DiagnoseTestFailure() and emit events.

🤖 Generated with Claude Code
Co-Authored-By: Claude <noreply@anthropic.com>
```
