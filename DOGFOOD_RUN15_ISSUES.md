# Issues to File from Dogfooding Run #15

These issues were discovered during dogfooding run #15 (2025-10-20 evening) but lost due to database issues. They need to be filed to the vc tracker.

## 1. API Timeout Investigation (HIGH PRIORITY)

**Title**: Investigate API timeout issues during AI assessment and watchdog calls

**Type**: bug
**Priority**: P1

**Description**:
During dogfooding run #15, observed significant API timeouts:

- vc-233 assessment: 1m18s (normal ~15s) with 'context deadline exceeded' error, required retry
- Watchdog anomaly detection: 1m12s (normal ~10s) with same timeout/retry pattern

Both calls eventually succeeded after retry, but this represents 4-8x normal latency.

Possible causes:
1. Transient Anthropic API slowdown
2. Rate limiting kicking in
3. Increased prompt complexity causing longer processing
4. Network issues between executor and API

Impact: Slows down execution significantly, may indicate systemic issue if persistent.

**Design**:
1. Add metrics collection for AI API call durations
2. Log request/response sizes for correlation with timeouts
3. Monitor over multiple dogfooding runs to determine if transient or systemic
4. If systemic, consider: prompt optimization, request batching, timeout tuning, retry backoff strategy

**Acceptance Criteria**:
- Root cause identified (transient vs systemic)
- If systemic: mitigation strategy implemented
- Metrics dashboard showing API latency trends
- Timeout/retry configuration tuned appropriately

---

## 2. Quality Gates vs AI Recovery Strategy

**Title**: Review quality gate expectations vs AI recovery for discovered/retrospective issues

**Type**: task
**Priority**: P2

**Description**:
Run #15 showed pattern where quality gates fail (test FAIL, lint FAIL) but AI recovery compensates by filing fix issues. This pattern suggests either:
- Gates are too strict for certain issue types (discovered issues, retrospective notes)
- Recovery strategy is working as designed

Need to decide: should we tune gate expectations per issue type, or is current behavior correct?

Example: vc-228 was a retrospective issue documenting work already done. Gates failed because the "work" was already complete but gates ran anyway.

**Design**:
Options:
1. Skip gates for certain issue types (retrospective, documentation-only)
2. Add gate configuration per issue type
3. Tune AI recovery to better handle these cases
4. Accept current behavior as correct (gates catch problems, recovery files fixes)

**Acceptance Criteria**:
- Decision made on approach
- If tuning gates: configuration mechanism implemented
- Documented policy for when gates run vs skip
- AI recovery behavior well-understood and tested

---

## 3. Missing Acceptance Criteria on Discovered Issues

**Title**: AI analysis should add acceptance criteria when filing discovered issues

**Type**: enhancement
**Priority**: P2

**Description**:
AI correctly identified that vc-228 had NO acceptance criteria, making it impossible to verify completion. When AI files discovered issues during analysis phase, it should include clear acceptance criteria so future work can be properly validated.

Current: Discovered issues often have vague descriptions without clear done conditions.
Desired: Every discovered issue has specific, testable acceptance criteria.

**Design**:
Update AI analysis prompt to emphasize creating acceptance criteria for discovered issues. Provide examples of good ACs:
- "Tests pass without X error"
- "File Y contains implementation of Z"
- "Documentation updated with section on W"

**Acceptance Criteria**:
- AI analysis prompt updated to require acceptance criteria
- Test run shows discovered issues include ACs
- ACs are specific and testable (not vague)
- Documentation updated with examples

---

## 4. Lint Violations (vc-232 equivalent)

**Title**: Fix staticcheck lint violations in multiple files

**Type**: bug
**Priority**: P1

**Description**:
Quality gates found staticcheck warnings that should be fixed:

- internal/ai/json_parser.go:325 - Use tagged switch on firstChar
- internal/executor/result_git.go:68 - Apply De Morgan's law
- internal/repl/conversation.go:1180,1184 - Remove unnecessary fmt.Sprintf calls
- internal/sandbox/manager.go:296 - Merge conditional assignment
- internal/watchdog/monitor_test.go:104 - Use time.Time.Equal instead of ==
- internal/executor/result_issues.go:14 - Remove unused createCodeReviewIssue function

These are all straightforward code quality improvements.

**Design**:
Fix each violation as suggested by staticcheck. Most are mechanical changes.

**Acceptance Criteria**:
- All listed staticcheck warnings resolved
- `make lint` or equivalent passes
- No functionality changed (refactoring only)

---

## 5. Pre-commit Hooks for Storage Interface (vc-230 equivalent)

**Title**: Consider implementing pre-commit hooks for Storage interface changes

**Type**: feature
**Priority**: P2

**Description**:
The documentation (INTERFACE_CHANGES.md) suggests adding pre-commit hooks that automatically check for Storage interface changes and verify all mocks are updated. This would prevent issues where interface changes break mock implementations.

Discovered during execution of vc-228.

**Design**:
Implement a pre-commit hook that:
1. Detects changes to internal/storage/storage.go
2. Runs scripts/find-storage-mocks.sh to find all mock implementations
3. Attempts to compile all test files with mocks
4. Blocks commit if compilation fails

Tools: husky, pre-commit framework, or simple .git/hooks/pre-commit script

**Acceptance Criteria**:
- Pre-commit hook installed and documented
- Hook detects Storage interface changes
- Hook validates all mocks compile
- Hook can be bypassed with --no-verify if needed
- Documentation updated with installation instructions

---

## 6. Evaluate mockgen Migration (vc-231 equivalent)

**Title**: Evaluate migration to mockgen for Storage interface mocks

**Type**: task
**Priority**: P2

**Description**:
The agent suggested using mockgen to automatically generate mocks from the Storage interface, which would eliminate manual mock maintenance issues. This should be evaluated for feasibility.

Discovered during execution of vc-228.

**Design**:
Research and prototype:
1. Can mockgen handle the Storage interface? (test with one method)
2. How to integrate with test files? (generate vs commit)
3. What's the migration path for existing hand-written mocks?
4. Pros/cons vs manual mocks

Create spike/prototype to validate approach before full migration.

**Acceptance Criteria**:
- Feasibility assessment completed
- Prototype created showing mockgen integration
- Decision made: migrate, partial migration, or stay with manual mocks
- If migrating: migration plan documented
- If not: reasons documented

---

## Status

All issues above need to be filed to the vc tracker. The schema fix (FOREIGN KEY constraint removal) has been completed and committed (f7582d7).
