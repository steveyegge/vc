# VC Issue Tracker Pollution Analysis

**Date:** 2025-11-23
**Total Issues:** 438 (245 open, 173 closed, 19 blocked, 1 in_progress)

## Executive Summary

The issue tracker has **~350+ issues of pollution** across several categories:
- **109 test-related noise** (94 open + estimated 15 closed spurious)
- **16 duplicate code review sweeps** (15 open + 1 closed)
- **~115 supervisor over-discovery spike** (Nov 2, 2025)
- **Estimated 150-200 legitimate issues**

## Pollution Breakdown

### 1. Test-Related Issues (130 total)

**Open:** 94 issues (38% of all open issues)
**Closed:** 36 issues

**Pattern:** AI supervisor filing issues for every missing test case:
- "Add unit tests for..." (50+ issues)
- "Add integration test for..." (30+ issues)
- "Add test coverage for..." (10+ issues)
- "Add race detector test for..." (5+ issues)

**Supervisor-discovered test issues (open):** 64 out of 86 supervisor-discovered open issues (74%)

**Example spam:**
- vc-536c: Add unit tests for circuit breaker monitoring goroutine lifecycle
- vc-b2cd: Add test for QA worker WaitGroup under error conditions
- vc-7161: Add integration test for circuit breaker during concurrent agent operations
- vc-aaa5: Re-enable or remove testMissionSandboxComprehensiveLifecycle test

**Duplicates found:**
- vc-xxx (3×): "Add test for issue status transition from blocked to open"

### 2. Code Review Sweep Duplication (16 total)

**Open:** 15 issues
**Closed:** 1 issue

**Pattern:** Multiple code review sweep issues created in rapid succession:
- 7× "Code Review Sweep: thorough"
- 7× "Code Review Sweep: quick"
- 2× "Code Review Sweep: targeted"

**Timeline:** 14 of 16 created on **Nov 2, 2025** between 12:56 PM - 10:12 PM (9 hour window)

**Issue IDs:**
- Thorough: vc-cc56, vc-77b3, vc-a409, vc-5e29, vc-ebcb, vc-ab59, vc-i9hf
- Quick: vc-cea7, vc-0261, vc-ac21, vc-f81a, vc-aedf, vc-dfc2, vc-aj8o
- Targeted: vc-20b8, vc-dccc

### 3. Discovered Issue Labels

**discovered:supervisor:** 115 total (86 open, 21 closed)
- 64 are test-related (74%)
- Created mostly on Nov 2, 2025 (115 issues that day!)

**Other discovered labels (open):**
- discovered:related: 14
- discovered:background: 7
- discovered:code-review: 5
- discovered:blocker: 2

### 4. Nov 2, 2025 Explosion

**115 issues created in a single day** - clear supervisor over-firing event.

**Timeline:**
- Nov 2: 115 issues
- Nov 5: 46 issues
- Nov 8: 61 issues
- Nov 7: 27 issues

## Legitimate Work Estimate

**Category breakdown of 245 open issues:**
- Test-related: 94 (likely spurious)
- Code review sweeps: 15 (duplicates)
- Other: 136

**Estimated legitimate open issues:** ~120-150

**High-value work visible in non-test, non-supervisor issues:**
- vc-4778: Define no-auto-claim policy
- vc-mwgv, vc-ob73, vc-3e0o: P0 bugs blocking executor
- vc-75-88: VCS abstraction epic tasks
- vc-185, vc-4c0d, vc-f18b: Real features

## Issue Type Distribution

- task: 276 (63%)
- bug: 102 (23%)
- feature: 32 (7%)
- epic: 24 (5%)
- chore: 4 (1%)

## Priority Distribution

- P0: 49
- P1: 145
- P2: 174
- P3: 64
- P4: 6

## Root Causes

### 1. Supervisor Over-Discovery
The AI supervisor is filing issues for **every possible improvement** rather than **blocking issues**:
- Test coverage gaps are filed as P1 tasks
- Code quality nitpicks become standalone issues
- Multiple overlapping review sweeps

### 2. No Deduplication
The supervisor lacks awareness of:
- Existing similar issues
- Recent similar creations
- Semantic duplicates (same work, different phrasing)

### 3. Test Coverage Obsession
The supervisor appears to have a rule like "file an issue for every function without a test" rather than "file an issue if critical code lacks tests"

### 4. Code Review Sweep Runaway
Something caused repeated code review sweep creation on Nov 2:
- Same sweep types created multiple times
- No consolidation or batching
- Likely a loop or retry bug

## Recommendations

### Immediate Actions (P0)

1. **Close all Code Review Sweep duplicates** - Keep 1 of each type (thorough/quick/targeted), close the rest
   - Close 13 duplicates immediately
   - Consolidate requirements into the kept issues

2. **Audit and close spurious test issues** - Review all 94 open test-related issues:
   - Keep only tests for critical/complex code
   - Close "add unit test for X" where X is trivial
   - Batch similar test issues into single tracking issues

3. **Review Nov 2 supervisor spike** - Manually review the 115 issues from Nov 2:
   - Many are likely low-value noise
   - Close or consolidate aggressively

### Medium-Term Fixes (P1)

4. **Add supervisor deduplication** - Before filing an issue, check for:
   - Exact title matches
   - Semantic similarity (embeddings or keywords)
   - Recent similar issues (24-48 hour window)

5. **Tune supervisor discovery thresholds**:
   - Only file test issues for critical paths or complex logic
   - Batch test issues by area ("Add tests for executor/storage/ai modules")
   - Require minimum severity for discovered issues

6. **Add supervisor rate limiting**:
   - Max N issues per run/hour/day
   - Prevent runaway discovery loops

7. **Add issue consolidation workflow**:
   - Periodic sweep to find and merge duplicates
   - Human-in-loop approval for bulk closures

### Long-Term Strategy (P2)

8. **Supervisor quality metrics**:
   - Track % of discovered issues that get completed
   - Track % that get closed as duplicates/spurious
   - Use metrics to tune discovery strategy

9. **Issue lifecycle analysis**:
   - Which types of issues age out without completion?
   - Which priorities are ignored?
   - Adjust supervisor priorities based on actual work patterns

10. **Mission-scoped discovery**:
    - Only file issues within current mission scope
    - Background/related work goes to a separate "ideas" tracker
    - Prevents mission creep and endless expansion

## Cleanup Commands

```bash
# Find all code review sweep issues
bd list | grep "Code Review Sweep"

# Sample test issues to review
sqlite3 .beads/beads.db "SELECT id, title FROM issues WHERE status='open' AND (title LIKE '%test%' OR title LIKE '%Test%') LIMIT 50"

# Find all supervisor-discovered issues
sqlite3 .beads/beads.db "SELECT i.id, i.title FROM issues i JOIN labels l ON i.id = l.issue_id WHERE l.label = 'discovered:supervisor' AND i.status = 'open' ORDER BY i.created_at"

# Close duplicates (example)
bd close vc-cea7 --reason "Duplicate code review sweep, consolidated into vc-cc56"
```

## Expected Impact

**After cleanup:**
- ~16 code review sweep duplicates closed → 229 open
- ~60-70 spurious test issues closed → 159-169 open
- ~20-30 other noise closed → 129-149 open

**Final healthy state:** 130-150 open issues (down from 245)

**Percentage reduction:** 38-47% fewer open issues

This would represent **actual legitimate work** rather than supervisor-generated noise.
