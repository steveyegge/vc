# Preflight Quality Gates

**CRITICAL PRINCIPLE**: **ALL baseline failures block work. No "pre-existing failure" excuses allowed.**

## Table of Contents

- [Overview](#overview)
- [The "No Pre-Existing Excuse" Philosophy](#the-no-pre-existing-excuse-philosophy)
- [How It Works](#how-it-works)
- [Commit-Hash Caching Strategy](#commit-hash-caching-strategy)
- [Degraded Mode](#degraded-mode)
- [Configuration](#configuration)
- [How to Fix Baseline Failures](#how-to-fix-baseline-failures)
- [Performance](#performance)
- [Future Phases](#future-phases)

## Overview

Preflight quality gates run **before** the executor claims work to verify that the baseline (main branch) is clean. This prevents agents from working on a broken codebase and disclaiming responsibility for pre-existing failures.

**Key Innovation**: Baseline cache keyed by git commit hash enables near-instant preflight checks for unchanged code.

## The "No Pre-Existing Excuse" Philosophy

### Why ALL Failures Block

The preflight system enforces a simple, unambiguous rule:

> **If the baseline is broken, fix it before claiming new work.**

No exceptions. No "pre-existing failure" loopholes. No insurance-adjuster disclaimers.

### The Problem with "Pre-Existing" Excuses

Without preflight gates, agents could:
1. Claim work on a broken baseline
2. Make changes that interact with existing failures
3. Declare "those failures were pre-existing, not my fault"
4. Leave the codebase in an unclear state

This creates ambiguity:
- Which failures are new vs. pre-existing?
- Did the agent's changes make existing failures worse?
- Who is responsible for fixing each failure?

### The Preflight Solution

Preflight gates make responsibility **crystal clear**:

```
✓ Baseline passes → Agent claims work → Baseline still passes = SUCCESS
✓ Baseline passes → Agent claims work → Baseline fails = AGENT'S RESPONSIBILITY

❌ Baseline fails → Agent CANNOT claim work (degraded mode)
```

The agent can never disclaim responsibility because work is **only claimed when the baseline is clean**.

### Real-World Example

**Without preflight:**
```
Agent: "I completed the feature, but 3 tests are failing."
Human: "Did your changes break those tests?"
Agent: "They might have been failing before I started. I'm not sure."
Human: "Can you check?"
Agent: "I don't have access to the state before my changes."
Result: Ambiguity, wasted time, unclear responsibility
```

**With preflight:**
```
Executor checks: Baseline has 3 failing tests
Executor: "⚠️ DEGRADED MODE: Baseline quality gates failed"
Executor: "Not claiming work until baseline is fixed"
Executor: "Created blocking issues: vc-baseline-test"
Human fixes the 3 tests
Executor detects: Baseline now passes
Executor resumes: Claims and executes work
Result: Clear, no ambiguity, no excuses
```

## How It Works

### Workflow

```
┌─────────────────────────────────────────────────────────────┐
│ 1. Executor polls for ready work every 5 seconds           │
└─────────────────────────────────────────────────────────────┘
                          ↓
┌─────────────────────────────────────────────────────────────┐
│ 2. PREFLIGHT CHECK: Run quality gates on baseline          │
│    - Get current commit hash (git rev-parse HEAD)          │
│    - Check cache (memory → database)                       │
│    - If cache miss: run gates (build, test, lint)         │
│    - Cache result by commit hash                           │
└─────────────────────────────────────────────────────────────┘
                          ↓
                    ┌─────────┐
                    │ Passed? │
                    └─────────┘
                     ↙       ↘
              YES ↙           ↘ NO
                 ↓               ↓
    ┌──────────────────┐  ┌──────────────────────┐
    │ 3a. CLAIM WORK   │  │ 3b. DEGRADED MODE    │
    │ Continue normal  │  │ Don't claim work     │
    │ execution        │  │ Create blocking      │
    │                  │  │ issues for failures  │
    │                  │  │ Keep polling         │
    └──────────────────┘  └──────────────────────┘
                                    ↓
                          ┌─────────────────────┐
                          │ Human fixes baseline│
                          │ Commits to main     │
                          └─────────────────────┘
                                    ↓
                          ┌─────────────────────┐
                          │ Executor detects:   │
                          │ New commit hash     │
                          │ Runs gates (cache   │
                          │ miss)               │
                          │ Baseline now passes │
                          └─────────────────────┘
                                    ↓
                          ┌─────────────────────┐
                          │ RESUME: Claim work  │
                          └─────────────────────┘
```

### Code Path

1. **Executor event loop** (`internal/executor/executor_event_loop.go:125-159`)
   - Calls `CheckBaseline()` before claiming work
   - Enters degraded mode if baseline fails

2. **PreFlightChecker** (`internal/executor/preflight.go`)
   - `CheckBaseline()`: Main entry point
   - `getCachedBaseline()`: Check memory cache
   - `GetGateBaseline()`: Check database cache
   - `runGatesAndCache()`: Run gates and cache result

3. **Database cache** (`internal/storage/beads/wrapper.go:275-282`)
   - Table: `vc_gate_baselines`
   - Primary key: `commit_hash`
   - Stores: results JSON, timestamp, pass/fail

## Commit-Hash Caching Strategy

### Why Cache by Commit Hash?

**Problem**: Running quality gates on every poll is expensive (30-60 seconds).

**Solution**: Cache results by git commit hash. If the code hasn't changed, the gates results are still valid.

### Two-Tier Cache

1. **Memory cache** (process-local, fastest)
   - Stores baseline results in-memory
   - Checked first (nanosecond lookup)
   - TTL: 5 minutes (configurable)

2. **Database cache** (persistent, shared across executors)
   - Table: `vc_gate_baselines`
   - Checked on memory cache miss (millisecond lookup)
   - TTL: 5 minutes (configurable)
   - Shared across executor restarts

### Cache Workflow

```
CheckBaseline(commit_hash)
  ↓
Check memory cache
  ↓ cache miss
Check database cache
  ↓ cache miss
Run gates (30-60s)
  ↓
Store in database
  ↓
Store in memory
  ↓
Return result
```

### Cache Invalidation

Caches are invalidated when:
1. **TTL expires** (5 minutes default)
   - Ensures fresh results even on same commit
   - Catches flaky test failures
2. **Commit changes** (new commit hash)
   - Automatic invalidation
   - No explicit cleanup needed

### Cache Hit Rate

Expected cache hit rate: **>90%** during normal development

- Cache miss on first poll after commit
- Cache hits on subsequent polls (every 5 seconds)
- Cache hits for ~60 polls (5min TTL / 5sec interval)

Example:
```
Commit pushed → 1 cache miss (30s gates run)
Next 60 polls → 60 cache hits (instant, <1ms)
Total time saved: 60 * 30s = 30 minutes
```

## Degraded Mode

### What is Degraded Mode?

When preflight gates fail, the executor enters **degraded mode**:
- ❌ Does NOT claim work
- ✅ Continues polling every 5 seconds
- ✅ Creates blocking issues for each failure
- ✅ Auto-recovers when baseline is fixed

### Blocking Issues Created

For each failing gate, a system-level blocking issue is created:

- `vc-baseline-test` - Test gate failures
- `vc-baseline-lint` - Lint gate failures
- `vc-baseline-build` - Build gate failures

**Issue details**:
- **Priority**: P1 (critical)
- **Type**: Bug
- **Labels**: `system`, `baseline-failure`, `gate:test`
- **Description**: Full error output and instructions
- **Acceptance Criteria**: Gate passes, executor resumes

### Executor Output in Degraded Mode

```
⚠️  DEGRADED MODE: Baseline quality gates failed
   Commit: 8e6cefe1234567890abcdef
   Failing gates: [test, lint]
   Executor will not claim work until baseline is fixed

   Created baseline blocking issue: vc-baseline-test
   Created baseline blocking issue: vc-baseline-lint

   Fix the failing gates and they will be auto-detected
```

### Auto-Recovery

The executor automatically recovers when:
1. Human fixes baseline failures
2. Commits and pushes to main
3. Executor's next poll detects new commit hash
4. Preflight runs gates on new commit (cache miss)
5. Gates pass → executor resumes claiming work

No manual intervention required beyond fixing the failures.

## Configuration

### Environment Variables

```bash
# Enable/disable preflight checks (default: true)
export VC_PREFLIGHT_ENABLED=true

# Cache TTL duration (default: 5m)
export VC_PREFLIGHT_CACHE_TTL=5m

# Failure mode: block, warn, or ignore (default: block)
export VC_PREFLIGHT_FAILURE_MODE=block

# Timeout for gate execution (default: 5m)
export VC_PREFLIGHT_GATES_TIMEOUT=5m
```

### Failure Modes

#### 1. Block Mode (DEFAULT) ⭐

```bash
export VC_PREFLIGHT_FAILURE_MODE=block
```

**Behavior**: Executor enters degraded mode, does NOT claim work

**Use when**:
- Production/critical projects
- You want guaranteed baseline quality
- You want clear responsibility attribution

**Output**:
```
⚠️  Baseline failed on commit abc123 - entering degraded mode
   Not claiming work until baseline is fixed
```

#### 2. Warn Mode

```bash
export VC_PREFLIGHT_FAILURE_MODE=warn
```

**Behavior**: Executor warns but continues claiming work

**Use when**:
- Development/experimental projects
- Baseline failures are expected/tolerable
- You want to work around temporary failures

**Output**:
```
⚠️  WARNING: Baseline failed on commit abc123 but continuing anyway (warn mode)
```

⚠️ **WARNING**: This defeats the "no pre-existing excuse" principle. Use sparingly.

#### 3. Ignore Mode

```bash
export VC_PREFLIGHT_FAILURE_MODE=ignore
```

**Behavior**: Executor silently ignores failures

**Use when**:
- Testing/debugging preflight system itself
- You want to disable preflight without setting `ENABLED=false`

⚠️ **WARNING**: This completely bypasses preflight. Not recommended for normal use.

### Disabling Preflight

```bash
export VC_PREFLIGHT_ENABLED=false
```

Preflight checks are skipped entirely. Executor always claims work.

## How to Fix Baseline Failures

### Step 1: Identify the Failure

Check the blocking issue created by the executor:

```bash
bd show vc-baseline-test
```

**Issue contains**:
- Full error output
- Stack traces (if applicable)
- Instructions for fixing

### Step 2: Fix Locally

```bash
# Run the failing gate locally
go test ./...        # For test failures
golangci-lint run    # For lint failures
go build ./...       # For build failures

# Fix the failures
# ...edit code...

# Verify fix
go test ./...
```

### Step 3: Commit and Push

```bash
git add .
git commit -m "Fix baseline: resolve test failures"
git push origin main
```

### Step 4: Executor Auto-Recovers

The executor will:
1. Detect new commit hash on next poll (~5 seconds)
2. Run preflight gates (cache miss)
3. Gates pass → resume claiming work

**Output**:
```
✓ Preflight check passed on commit def456
✓ Resuming normal operation
✓ Claimed issue vc-123
```

### Step 5: Close Blocking Issue

Once the executor resumes, manually close the blocking issue:

```bash
bd close vc-baseline-test --reason "Fixed: all tests passing"
```

## Performance

### Typical Timings

| Scenario | Time | Notes |
|----------|------|-------|
| Cache hit (memory) | <1ms | 99% of polls |
| Cache hit (database) | ~5ms | After executor restart |
| Cache miss (run gates) | 30-60s | On commit change |
| Cache miss amortized | ~0.5s | 1 miss / 60 hits |

### Expected Cache Hit Rate

**Goal**: >90% cache hit rate

**Actual** (typical development):
- Commits: ~1 per 5 minutes
- Polls: 60 per 5 minutes (at 5s interval)
- Cache hits: 59/60 = **98.3%**

### Cost Analysis

**Without preflight caching**:
- 60 polls/5min × 30s/poll = **30 minutes of gates per 5 minutes** (impossible)

**With preflight caching**:
- 1 cache miss × 30s = 30s of gates per 5 minutes
- 59 cache hits × 1ms = ~0.06s
- **Total**: ~30 seconds per 5 minutes
- **Overhead**: 10% of polling time

### Tuning Cache TTL

```bash
# Longer TTL = fewer cache misses, staler results
export VC_PREFLIGHT_CACHE_TTL=10m

# Shorter TTL = more cache misses, fresher results
export VC_PREFLIGHT_CACHE_TTL=2m
```

**Recommendation**: Default 5m is optimal for most projects

- Fresh enough to catch flaky tests
- Long enough for good cache hit rate
- Balanced overhead (~10%)

## Future Phases

### Phase 1 (COMPLETE) ✅

**Commit-based caching with degraded mode**

- [x] Cache keyed by git commit hash
- [x] Two-tier cache (memory + database)
- [x] TTL-based invalidation
- [x] Degraded mode on baseline failure
- [x] Blocking issues for gate failures
- [x] Configuration via environment variables

### Phase 2 (FUTURE)

**Baseline comparison - only NEW failures block**

Currently: ALL failures block work (no "pre-existing" excuse)

Future: Track which failures are "pre-existing" vs "new":

```
Baseline A:   test1 ✅  test2 ❌  test3 ✅
Commit change → Baseline B
Baseline B:   test1 ✅  test2 ❌  test3 ❌

Result: test3 is NEW failure → BLOCK work
        test2 is PRE-EXISTING → Don't block (grandfathered)
```

**Rationale**: Prevent agents from breaking new things, but allow work on codebases with known pre-existing issues.

**Trade-off**: Relaxes "no pre-existing excuse" principle. May lead to gradual quality degradation.

### Phase 3 (FUTURE)

**Sandbox reuse for unchanged baselines**

Currently: New sandbox created for each execution

Future: Reuse sandbox when baseline hasn't changed:

```
Baseline A (commit abc123) → Sandbox /tmp/vc-sandbox-abc123
Agent executes work in sandbox
Commit pushed → Baseline B (commit def456)
New sandbox → /tmp/vc-sandbox-def456

Next agent execution on same commit def456
Reuse sandbox → /tmp/vc-sandbox-def456 (faster startup)
```

**Benefits**:
- Faster execution startup (no git clone)
- Faster gate runs (dependencies already installed)
- Reduced disk I/O

**Implementation**: `sandbox_path` column in `vc_gate_baselines` table (already exists)

---

## FAQ

### Why not allow "pre-existing" failures?

**Answer**: Clear responsibility attribution. If we allow work on a broken baseline, agents can disclaim responsibility for failures. This creates ambiguity and wasted time debugging "who broke what".

The preflight system makes it **impossible** for agents to disclaim responsibility because they only work on clean baselines.

### What if the baseline is always broken?

**Answer**: Fix the baseline before running the executor. If your baseline is chronically broken, you have a bigger problem than the executor.

The executor is designed to work on **quality codebases**, not to be a workaround for broken tests.

### Can I disable the "no pre-existing excuse" rule?

**Answer**: Yes, but you probably shouldn't.

```bash
# Warn mode: executor continues despite failures
export VC_PREFLIGHT_FAILURE_MODE=warn

# Ignore mode: executor silently ignores failures
export VC_PREFLIGHT_FAILURE_MODE=ignore
```

⚠️ **WARNING**: This defeats the core principle of preflight gates. Use only for experimental/development projects.

### How does this affect development velocity?

**Answer**: Slightly slower initially (enforces baseline quality), **much faster** long-term (no ambiguous failures).

**Without preflight**:
- Agent works on broken baseline
- Creates unclear failures
- Human debugs "was this pre-existing?"
- Wastes hours disambiguating
- Quality degrades over time

**With preflight**:
- Executor blocks if baseline broken
- Human fixes baseline first (~10 minutes)
- Executor works on clean baseline
- All failures are clearly agent's responsibility
- Quality maintained over time

**Net effect**: Trade 10 minutes upfront for hours saved in debugging ambiguity.

### What if gates are flaky?

**Answer**: Fix the flaky tests. Flaky tests are a codebase quality problem, not an executor problem.

The preflight system **exposes** flakiness by running gates frequently. This is a feature, not a bug.

**Workarounds**:
1. Fix the flaky tests (best solution)
2. Increase cache TTL to reduce gate runs
3. Disable flaky tests temporarily
4. Use warn mode (last resort)

### How do I monitor preflight performance?

**Answer**: Check the activity feed for preflight events:

```bash
# View preflight events
sqlite3 .beads/vc.db "
SELECT timestamp, type, message, data
FROM vc_agent_events
WHERE type LIKE 'pre_flight%' OR type LIKE 'baseline%'
ORDER BY timestamp DESC
LIMIT 20
"
```

**Event types**:
- `pre_flight_check_started` - Preflight check initiated
- `baseline_cache_hit` - Cache hit (memory or database)
- `baseline_cache_miss` - Cache miss, running gates
- `pre_flight_check_completed` - Preflight check result
- `executor_degraded_mode` - Executor entered degraded mode

---

**Remember**: The preflight system exists to maintain baseline quality and clear responsibility attribution. ALL baseline failures block work. No exceptions.

If you find yourself fighting the preflight system, ask: "Should I fix my baseline instead?"
