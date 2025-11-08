# Configuration Reference

This document contains all environment variable configuration options for VC.

---

## 🔍 Deduplication Configuration

VC uses AI-powered deduplication to prevent filing duplicate issues. This feature can be tuned via environment variables to balance between avoiding duplicates and avoiding false positives.

### Default Configuration (Performance Optimized)

The default settings are optimized for performance while maintaining accuracy:

- **Confidence threshold**: 0.85 (85%) - High confidence required to mark as duplicate
- **Lookback window**: 7 days - Only compare against issues from the past week
- **Max candidates**: 25 - Compare against up to 25 recent issues (reduced from 50 for speed)
- **Batch size**: 50 - Process 50 comparisons per AI call (increased from 10 for efficiency)
- **Within-batch dedup**: Enabled - Deduplicate within the same batch of discovered issues
- **Fail-open**: Enabled - File the issue if deduplication fails (prefer duplicates over lost work)
- **Include closed issues**: Disabled - Only compare against open issues
- **Min title length**: 10 characters - Skip dedup for very short titles
- **Max retries**: 2 - Retry AI calls twice on failure
- **Request timeout**: 30 seconds - Timeout for AI API calls

**Performance Impact** (vc-159):
With 3 discovered issues and default config:
- **Old** (BatchSize=10, MaxCandidates=50): ~15 AI calls, ~90 seconds
- **New** (BatchSize=50, MaxCandidates=25): ~3 AI calls, ~18 seconds
- **Result**: 80% reduction in API calls and deduplication time!

### Environment Variables

All deduplication settings can be customized via environment variables:

```bash
# Confidence threshold (0.0 to 1.0, default: 0.85)
# Higher = more conservative (fewer false positives, more false negatives)
# Lower = more aggressive (more false positives, fewer false negatives)
export VC_DEDUP_CONFIDENCE_THRESHOLD=0.85

# Lookback period in days (default: 7)
# How many days of recent issues to compare against
export VC_DEDUP_LOOKBACK_DAYS=7

# Maximum number of issues to compare against (default: 50)
# Limits AI API costs and processing time
export VC_DEDUP_MAX_CANDIDATES=50

# Batch size for AI calls (default: 10)
# Number of comparisons to send in a single AI API call
export VC_DEDUP_BATCH_SIZE=10

# Enable within-batch deduplication (default: true)
# If multiple discovered issues are duplicates of each other, only keep the first
export VC_DEDUP_WITHIN_BATCH=true

# Fail-open behavior (default: true)
# If true: file the issue anyway when deduplication fails
# If false: return error and block issue creation
export VC_DEDUP_FAIL_OPEN=true

# Include closed issues in comparison (default: false)
# Useful for preventing re-filing of recently closed issues
export VC_DEDUP_INCLUDE_CLOSED=false

# Minimum title length for deduplication (default: 10)
# Very short titles lack semantic meaning for comparison
export VC_DEDUP_MIN_TITLE_LENGTH=10

# Maximum retry attempts (default: 2)
# Number of times to retry AI API calls on failure
export VC_DEDUP_MAX_RETRIES=2

# Request timeout in seconds (default: 30)
# Timeout for individual AI API calls
export VC_DEDUP_TIMEOUT_SECS=30
```

### Tuning Guidelines

**To reduce false positives** (issues incorrectly marked as duplicates):
- Increase `VC_DEDUP_CONFIDENCE_THRESHOLD` to 0.90 or 0.95
- Decrease `VC_DEDUP_MAX_CANDIDATES` to compare against fewer issues
- Decrease `VC_DEDUP_LOOKBACK_DAYS` to only compare against very recent issues

**To reduce false negatives** (actual duplicates not caught):
- Decrease `VC_DEDUP_CONFIDENCE_THRESHOLD` to 0.75 or 0.80 (use with caution)
- Increase `VC_DEDUP_MAX_CANDIDATES` to compare against more issues
- Increase `VC_DEDUP_LOOKBACK_DAYS` to compare against older issues
- Enable `VC_DEDUP_INCLUDE_CLOSED=true` to catch recently closed duplicates

**To reduce costs**:
- Decrease `VC_DEDUP_MAX_CANDIDATES` to limit API calls
- Decrease `VC_DEDUP_LOOKBACK_DAYS` to narrow the search window
- Increase `VC_DEDUP_BATCH_SIZE` to make fewer API calls (up to 100)

**For debugging**:
- Set `VC_DEDUP_CONFIDENCE_THRESHOLD=1.0` to effectively disable deduplication
- Set `VC_DEDUP_MAX_CANDIDATES=0` to skip deduplication entirely
- Check logs for `[DEDUP]` messages showing comparison results

### Example Configurations

**Conservative Configuration** (for critical projects where missing work is worse than having duplicates):

```bash
export VC_DEDUP_CONFIDENCE_THRESHOLD=0.95  # Very high confidence required
export VC_DEDUP_FAIL_OPEN=true             # File on error
export VC_DEDUP_MAX_CANDIDATES=30          # Limited comparisons
```

**Aggressive Configuration** (for projects with lots of duplicate work being filed):

```bash
export VC_DEDUP_CONFIDENCE_THRESHOLD=0.75  # Lower threshold
export VC_DEDUP_LOOKBACK_DAYS=14           # Longer lookback
export VC_DEDUP_MAX_CANDIDATES=100         # More candidates
export VC_DEDUP_INCLUDE_CLOSED=true        # Include closed issues
```

### Configuration Validation

The executor validates all deduplication settings on startup. Invalid values (out of range, wrong type, etc.) will cause the executor to exit with a clear error message.

Validation checks:
- `VC_DEDUP_CONFIDENCE_THRESHOLD` must be between 0.0 and 1.0
- `VC_DEDUP_LOOKBACK_DAYS` must be between 1 and 90 days
- `VC_DEDUP_MAX_CANDIDATES` must be between 0 and 500
- `VC_DEDUP_BATCH_SIZE` must be between 1 and 100
- `VC_DEDUP_MIN_TITLE_LENGTH` must be between 0 and 500
- `VC_DEDUP_MAX_RETRIES` must be between 0 and 10
- `VC_DEDUP_TIMEOUT_SECS` must be between 1 and 300 seconds

See [docs/QUERIES.md](./QUERIES.md) for queries to monitor deduplication metrics.

---

## ⚙️ Quality Gates Configuration

VC runs quality gates (test/lint/build) after successful agent execution to ensure code quality. Gate execution has a configurable timeout to prevent indefinite hangs.

### Timeout Configuration

**VC_QUALITY_GATES_TIMEOUT** - Maximum time allowed for all quality gates to complete (default: 5 minutes)

```bash
# Default timeout (5 minutes)
export VC_QUALITY_GATES_TIMEOUT=5m

# Longer timeout for large codebases
export VC_QUALITY_GATES_TIMEOUT=10m

# Shorter timeout for fast feedback in tests
export VC_QUALITY_GATES_TIMEOUT=2m
```

**Format**: Duration string (e.g., "5m", "300s", "2m30s")

**Valid range**: 1 second to 60 minutes

**When to adjust**:
- **Increase** if gates are timing out on large codebases
- **Decrease** for faster feedback during development/testing
- **Default (5m)** is appropriate for most projects

**What happens on timeout**:
- Gates execution is canceled
- Issue is marked as blocked with timeout error
- Partial gate results are logged for debugging
- Agent work is not committed

### Tuning Guidelines

**For large codebases with slow tests**:
```bash
export VC_QUALITY_GATES_TIMEOUT=15m
```

**For fast iteration during development**:
```bash
export VC_QUALITY_GATES_TIMEOUT=3m
```

**For tests (faster feedback)**:
```bash
export VC_QUALITY_GATES_TIMEOUT=1m
```

---

## 🐛 Debug Environment Variables

**Debug Prompts:**
```bash
# Log full prompts sent to agents (useful for debugging agent behavior)
export VC_DEBUG_PROMPTS=1
```

**Debug Events:**
```bash
# Log JSON event parsing details (tool_use events from Amp --stream-json)
export VC_DEBUG_EVENTS=1
```

**Debug Status Changes (vc-n4lx):**
```bash
# Log all issue status changes with old/new status and actor
# Useful for debugging unexpected status changes (e.g., baseline issues becoming blocked)
export VC_DEBUG_STATUS=1
```

Example output:
```
[VC_DEBUG_STATUS] 2025-11-06T21:15:32Z: Status change for vc-baseline-test: open → blocked (actor: preflight)
[VC_DEBUG_STATUS] 2025-11-06T21:20:45Z: Status change for vc-baseline-test: blocked → open (actor: preflight-self-healing, reason: Self-healing reopened)
[VC_DEBUG_STATUS] 2025-11-06T21:25:10Z: Status change for vc-abc: in_progress → closed (actor: executor, reason: Completed: gates passed)
```

---

## 🔑 AI Supervision Configuration

**ANTHROPIC_API_KEY** (Required for AI supervision):
```bash
# Required for AI supervision (assessment and analysis)
export ANTHROPIC_API_KEY=your-key-here
```

Without this key, the executor will run without AI supervision (warnings will be logged).

AI supervision can be explicitly disabled via config: `EnableAISupervision: false`

---

## 🎯 Blocker Priority Configuration

**EnableBlockerPriority** (Default: true):

VC uses blocker-first prioritization to ensure missions run to completion. Discovered blockers are ALWAYS selected before regular ready work, regardless of priority numbers.

**Default behavior** (EnableBlockerPriority: true):
- Discovered blockers (label=discovered:blocker) have absolute priority
- A P3 blocker will be selected over a P0 regular task
- Regular work may wait indefinitely if blockers continuously appear
- This is intentional for mission convergence

**Disabling blocker priority** (EnableBlockerPriority: false):
- All work is prioritized by priority number only
- Blockers and regular work compete equally
- Use this if work starvation becomes a problem

**Configuration:**
```go
cfg := executor.DefaultConfig()
cfg.EnableBlockerPriority = false  // Disable blocker-first prioritization
```

**Monitoring:**
- Check blocker discovery rate: `bd list --status open | grep discovered:blocker`
- Monitor work starvation metrics (see vc-160)
- See CLAUDE.md Workflow section for full prioritization policy

**Related issues:**
- vc-161: Documentation for blocker prioritization policy
- vc-160: Monitoring work starvation

---

## 🔄 Self-Healing Configuration

VC uses a self-healing state machine to recover from baseline quality gate failures (test/lint/build). The self-healing system attempts to fix baseline issues automatically, escalating to humans when thresholds are exceeded.

### Environment Variables

```bash
# Maximum attempts before escalating baseline issues (default: 5)
# After this many failed attempts, the baseline issue is marked no-auto-claim
# and an escalation issue is created for human intervention
export VC_SELF_HEALING_MAX_ATTEMPTS=5

# Maximum duration in self-healing mode before escalating (default: 24h)
# If a baseline issue remains unresolved for this long, it gets escalated
# Format: duration string (e.g., "24h", "48h", "2h30m")
export VC_SELF_HEALING_MAX_DURATION=24h

# How often to recheck baseline in degraded mode (default: 5m)
# When in degraded mode (no baseline work found), this controls how often
# to recheck if the baseline has been fixed by other means
# Format: duration string (e.g., "5m", "10m", "1m")
export VC_SELF_HEALING_RECHECK_INTERVAL=5m

# Enable verbose logging for self-healing decisions (default: true)
# Logs every decision in the fallback chain for observability
# Useful for debugging self-healing behavior
export VC_SELF_HEALING_VERBOSE_LOGGING=true
```

### Self-Healing State Machine

The executor uses three states to manage baseline failures:

- **HEALTHY**: Normal operation, all quality gates passing
- **SELF_HEALING**: Baseline failed, actively trying to fix it with smart work selection
- **ESCALATED**: Thresholds exceeded, human intervention needed

### Smart Work Selection (SELF_HEALING Mode)

When in SELF_HEALING mode, the executor uses this fallback chain:

1. Find baseline-failure labeled issues (ready to execute)
2. Investigate blocked baseline → claim ready dependents
3. Find discovered:blocker issues (ready to execute)
4. Log diagnostics if no work found
5. Check escalation thresholds
6. Fall through to regular work

### Escalation Triggers

Escalation happens when EITHER threshold is exceeded:

- **Attempt threshold**: `VC_SELF_HEALING_MAX_ATTEMPTS` (default: 5)
- **Duration threshold**: `VC_SELF_HEALING_MAX_DURATION` (default: 24h)

When escalated:
- Baseline issue gets `no-auto-claim` label (executor stops working on it)
- Escalation issue created (P0, urgent, no-auto-claim)
- Executor transitions to ESCALATED mode
- Regular work continues normally

### Tuning Guidelines

**For aggressive self-healing** (attempt fixes more times before giving up):
```bash
export VC_SELF_HEALING_MAX_ATTEMPTS=10  # Try more times
export VC_SELF_HEALING_MAX_DURATION=48h # Allow more time
```

**For conservative self-healing** (escalate to humans sooner):
```bash
export VC_SELF_HEALING_MAX_ATTEMPTS=3   # Escalate sooner
export VC_SELF_HEALING_MAX_DURATION=12h # Shorter time window
```

**For debugging self-healing behavior:**
```bash
export VC_SELF_HEALING_VERBOSE_LOGGING=true  # Enable detailed logs
export VC_SELF_HEALING_RECHECK_INTERVAL=1m       # Check more frequently
```

### Related Issues

- vc-210: Self-Healing Baseline Failures (original implementation)
- vc-wlk2: Robust Self-Healing: Graceful Degradation and Smart Fallback (epic)
- vc-23t0: Implement SelfHealingMode state machine
- vc-h8b8: Implement escalation mechanism with thresholds
- vc-tn9c: Add configuration for self-healing thresholds

---

## 🔁 Incomplete Work Retry Configuration

VC detects when an agent succeeds technically (exit code 0, quality gates pass) but fails to fully complete the work according to acceptance criteria. This can happen when the agent reads files but doesn't make required edits, or only partially completes the task.

### Environment Variables

```bash
# Maximum retries for incomplete work before escalation (default: 1)
# After this many incomplete attempts, the issue is marked needs-human-review
# and blocked to prevent infinite retry loops
export VC_MAX_INCOMPLETE_RETRIES=1
```

### How It Works

When AI analysis reports `completed: false` but the agent succeeded:

1. **First attempt**: Issue gets a retry comment and stays open for another attempt
2. **Second attempt** (default threshold): Issue is escalated with `needs-human-review` label and marked as blocked

The retry logic counts "Incomplete Work Detected" comments in the event history to track attempts across executions.

### Tuning Guidelines

**For more aggressive retries** (give the agent more chances):
```bash
export VC_MAX_INCOMPLETE_RETRIES=2  # Allow 2 retries before escalation
```

**For conservative approach** (escalate immediately):
```bash
export VC_MAX_INCOMPLETE_RETRIES=0  # Escalate on first incomplete attempt
```

**Default recommendation**: Keep at 1 retry. Most incomplete work issues are due to fundamental misunderstanding of requirements rather than transient issues, so additional retries rarely help.

### Related Issues

- vc-1ows: Handle incomplete work with retry mechanism
- vc-hsfz: Make maxIncompleteRetries configurable

---

## 🗄️ Event Retention Configuration (Future Work)

**Status:** Not yet implemented. Punted until database size becomes a real issue (vc-184, vc-198).

### Why Punted?

Following the lesson learned from deduplication metrics (vc-151), we're deferring event retention infrastructure until we have real production data showing it's needed. This avoids building observability for theoretical future problems.

### When to Implement

Implement event retention when:
- `.beads/vc.db` exceeds 100MB
- Query performance degrades noticeably
- Developers complain about database size
- Event table has >100k rows

Until then: **YAGNI** (You Aren't Gonna Need It).

### Proposed Configuration

When we do implement this, here's the plan:

**Retention Policy Tiers:**
- **Regular events** (progress, file_modified, etc.): 30 days
- **Critical events** (error, watchdog_alert): 180 days
- **Per-issue limit**: 1000 events max per issue
- **Global limit**: Configurable, default 50k events

**Proposed Environment Variables:**
```bash
# Event retention in days (default: 30)
export VC_EVENT_RETENTION_DAYS=30

# Critical event retention in days (default: 180)
export VC_EVENT_CRITICAL_RETENTION_DAYS=180

# Per-issue event limit (default: 1000, 0 = unlimited)
export VC_EVENT_PER_ISSUE_LIMIT=1000

# Global event limit (default: 50000, 0 = unlimited)
export VC_EVENT_GLOBAL_LIMIT=50000

# Cleanup frequency in hours (default: 24)
export VC_EVENT_CLEANUP_INTERVAL_HOURS=24

# Batch size for cleanup (default: 1000)
export VC_EVENT_CLEANUP_BATCH_SIZE=1000
```

**Cleanup Strategy:**
- Run as background goroutine in executor
- Execute every 24 hours (configurable)
- Transaction-based deletion in batches of 1000
- Log cleanup metrics (events deleted, time taken)

**CLI Command (Not Yet Implemented):**
```bash
# Manual cleanup trigger
vc cleanup events --dry-run  # Preview what would be deleted
vc cleanup events             # Execute cleanup
vc cleanup events --force     # Bypass safety checks
```

### Related Issues

- vc-183: Agent Events Retention and Cleanup [OPEN - Low Priority]
- vc-184: Design event retention policy [CLOSED - Design complete]
- vc-193 through vc-197: Implementation tasks [OPEN - Punted]
- vc-199: Tests for event retention [OPEN - Punted]

**Remember:** Build this when you need it, not before. Let real usage drive the requirements.

See [docs/QUERIES.md](./QUERIES.md) for event retention monitoring queries (for future use).
