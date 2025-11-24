# Configuration Reference

This document contains all environment variable configuration options for VC.

---

## 🤖 AI Model Selection (vc-35, vc-lf8j)

VC uses a **tiered AI model strategy** to optimize cost and performance:
- **Sonnet 4.5** (`claude-sonnet-4-5-20250929`): Complex reasoning tasks
- **Haiku** (`claude-3-5-haiku-20241022`): Simple, deterministic tasks

### Cost Savings

Haiku is approximately **75% cheaper** than Sonnet:
- Sonnet: $3/$15 per million tokens (input/output)
- Haiku: $0.80/$4 per million tokens (input/output)

By using Haiku for simple operations (30-40% of total AI calls), VC achieves **25%+ overall cost savings** while maintaining quality.

### Model Assignment

**Operations using Haiku (simple tasks):**
- Cruft detection
- File size monitoring
- Gitignore pattern recommendations
- Commit message generation

**Operations using Sonnet (complex/medium tasks):**
- Pre-execution assessment
- Post-execution analysis
- Code review and test coverage
- **Deduplication detection** (medium complexity - requires semantic understanding)
- Discovered issue translation
- Complexity monitoring

**Why deduplication uses Sonnet:**
Deduplication requires nuanced semantic understanding and has a high cost of failure (false negatives create duplicate issues, false positives lose work). While it's not as complex as assessment/analysis, it's more complex than simple pattern matching. Additionally, vc-159 already optimized dedup cost by 80% through batching, making model switching less impactful.

### Environment Variables

Override model selection with environment variables:

```bash
# Override default model (used for complex tasks)
# Default: claude-sonnet-4-5-20250929
export VC_MODEL_DEFAULT="claude-sonnet-4-5-20250929"

# Override simple task model (used for cruft detection, file size, etc.)
# Default: claude-3-5-haiku-20241022
export VC_MODEL_SIMPLE="claude-3-5-haiku-20241022"
```

**Use cases for overrides:**

1. **Testing with cheaper models:**
   ```bash
   # Use Haiku for everything (save cost during development)
   export VC_MODEL_DEFAULT="claude-3-5-haiku-20241022"
   export VC_MODEL_SIMPLE="claude-3-5-haiku-20241022"
   ```

2. **Testing with premium models:**
   ```bash
   # Use Opus for everything (maximum quality)
   export VC_MODEL_DEFAULT="claude-opus-4-20250514"
   export VC_MODEL_SIMPLE="claude-opus-4-20250514"
   ```

3. **A/B testing quality:**
   ```bash
   # Compare Sonnet vs Haiku for simple tasks
   export VC_MODEL_SIMPLE="claude-sonnet-4-5-20250929"
   ```

### Quality Validation

Phase 2 validation tests (vc-lf8j) verify:
- **<5% quality degradation** when using Haiku vs Sonnet for simple tasks
- **>50% cost savings** for simple operations
- **Overall 25%+ cost reduction** across all AI calls

Run validation tests:
```bash
# Quality comparison tests
go test -v ./internal/health -run TestModelQuality

# Cost measurement tests
go test -v ./internal/health -run TestModelCost
```

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

## 🛡️ Validator Resilience Configuration (vc-e5qn)

VC's planning system uses multiple validators to check mission plans. Each validator runs with panic recovery and timeouts to prevent one bad validator from killing the entire validation pipeline.

### Validator Timeout

**Default:** 30 seconds per validator

```bash
# Override per-validator timeout
export VC_VALIDATOR_TIMEOUT=60s  # Allow 1 minute per validator
```

**Valid values:** Any Go duration string (e.g., `10s`, `1m`, `90s`)

### Validator Behavior

Each validator runs with the following protections:

1. **Panic Recovery:**
   - Panics are caught and logged
   - Validation continues with other validators
   - Panic is reported as a validation error

2. **Timeout Protection:**
   - Each validator gets its own timeout context
   - Prevents infinite loops or hangs
   - Timeout is reported as a validation error

3. **Fault Isolation:**
   - One validator failure doesn't block others
   - All validators run to completion
   - Combined error report shows all failures

### Validators

The following validators run on every mission plan:

- **phase_count:** Checks phase count is within acceptable range (1-15 phases)
- **plan_size:** Enforces plan size limits to prevent timeouts (configurable)
- **circular_dependencies:** Detects circular dependencies in phases
- **dependency_references:** Validates all dependency IDs reference existing phases
- **task_counts:** Validates each phase has reasonable task count (1-50 tasks)
- **phase_structure_ai:** AI-driven validation of phase dependencies and ordering (advisory only)

**Note:** The AI validator is advisory only and will log warnings but not block validation on failure (e.g., network issues, API errors).

### Plan Size Limits (vc-r3an)

To prevent timeouts during refinement, validation, and approval, VC enforces configurable limits on plan size:

**Default Limits:**
- Max phases per plan: **20**
- Max tasks per phase: **30**
- Max total tasks: **600** (computed as max_phases × max_tasks_per_phase)
- Max dependency depth: **10** levels

**Environment Variables:**

```bash
# Maximum number of phases in a mission plan
# Default: 20
export VC_MAX_PLAN_PHASES=20

# Maximum number of tasks per phase
# Default: 30
export VC_MAX_PHASE_TASKS=30

# Maximum dependency depth (longest dependency chain)
# Default: 10
export VC_MAX_DEPENDENCY_DEPTH=10
```

**Why These Limits?**

1. **Refinement Timeout Risk:** Plans with >30 tasks per phase may exceed the 5-minute refinement timeout
2. **Validation Hang Risk:** Plans with >50 phases may cause cycle detector to hang
3. **Approval Timeout Risk:** Plans with >600 total tasks may exceed database transaction timeout
4. **Pathological Graphs:** Dependency depth >10 suggests overly complex dependency chains

**Validation Errors:**

```bash
# Too many phases
Error: validation failed: plan_size: plan has too many phases (25 > 20 limit); risk of timeout during validation

# Phase with too many tasks
Error: validation failed: plan_size: phase 1 (Setup) has too many tasks (35 > 30 limit); risk of timeout during refinement

# Excessive dependency depth
Error: validation failed: plan_size: plan has excessive dependency depth (12 > 10 limit); risk of pathological dependency graph
```

**Custom Limits:**

For larger missions, you can increase limits:

```bash
# Allow larger plans
export VC_MAX_PLAN_PHASES=30
export VC_MAX_PHASE_TASKS=50
export VC_MAX_DEPENDENCY_DEPTH=15
```

For stricter validation during testing:

```bash
# Enforce smaller plans
export VC_MAX_PLAN_PHASES=10
export VC_MAX_PHASE_TASKS=15
export VC_MAX_DEPENDENCY_DEPTH=5
```

**Dependency Depth Calculation:**

Dependency depth is the longest path from a phase with no dependencies to any phase. For example:

- Linear chain (1 → 2 → 3): depth = 3
- Diamond (1 → 2,3 → 4): depth = 3
- Complex graph (1 → 2 → 3 → 4 → 5): depth = 5

This prevents pathological dependency graphs that could cause performance issues in cycle detection and topological sorting.

### Example Error Output

```bash
# Multiple validator failures
Error: validation failed: phase_count: plan has too many phases (20); consider breaking into multiple missions; task_counts: phase 1 (Setup) has too many tasks (60); break it down further

# Validator timeout
Error: validation failed: circular_dependencies: validator timeout after 30s

# Validator panic
Error: validation failed: phase_structure_ai: validator panic: runtime error: invalid memory address or nil pointer dereference
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

## 🔄 Quota Retry Configuration (vc-5b22)

VC intelligently handles Anthropic API quota/rate limit errors (429 responses) by respecting the `retry-after` duration instead of immediately retrying with exponential backoff.

### The Problem

When the Anthropic API quota is exceeded, the response includes:
- **HTTP 429** status code
- **Retry-After header** or error message like "try again in 12 minutes"
- Without intelligent handling, repeated retries waste attempts and burn through retries

### Intelligent Quota Waiting

VC classifies errors into types and handles each appropriately:

**Error Types:**
- **QUOTA** (429): Wait for `retry-after` duration, then retry
- **TRANSIENT** (5xx): Use exponential backoff (immediate retry with delays)
- **AUTH** (401/403): Don't retry (auth failures won't succeed)
- **INVALID** (400/404): Don't retry (malformed requests won't succeed)
- **UNKNOWN**: Use exponential backoff (conservative approach)

### Environment Variables

```bash
# Maximum time to wait for quota reset (default: 15 minutes)
# If retry-after exceeds this, fail fast instead of waiting indefinitely
export VC_MAX_QUOTA_WAIT=15m

# Examples:
export VC_MAX_QUOTA_WAIT=30m   # Wait up to 30 minutes
export VC_MAX_QUOTA_WAIT=5m    # Wait maximum 5 minutes
export VC_MAX_QUOTA_WAIT=1h    # Wait up to 1 hour
```

### How It Works

**When quota is exceeded (429 error):**

1. **Parse retry-after duration** from:
   - `Retry-After` HTTP header (seconds or HTTP-date)
   - `X-RateLimit-Reset` header (Unix timestamp)
   - Error message patterns: "try again in 12 minutes"

2. **Check against MaxQuotaWait**:
   - If `retry-after <= MaxQuotaWait`: Wait intelligently, then retry
   - If `retry-after > MaxQuotaWait`: Fail fast with clear error message

3. **During wait**:
   - Log clear message showing wait time and reset time
   - Respect context cancellation (allow graceful shutdown)
   - Don't burn through retry attempts

4. **After wait completes**:
   - Retry immediately (quota should be reset)
   - If still failing, use normal retry logic

**Example output:**
```
⚠️  Quota exceeded: API rate limit hit
    Retry after: 12m0s (at 14:30:00 UTC)
    Attempt: 1/3
    Waiting for quota reset...
Quota wait completed, retrying assessment
```

### Circuit Breaker Integration

Quota errors are **weighted more heavily** in the circuit breaker:
- **Regular errors**: Count as 1 failure
- **Quota errors**: Count as 3 failures (trip circuit faster)

This prevents repeatedly hitting rate limits and gives the system time to recover.

### Retry-After Parsing

VC handles multiple retry-after formats:

**HTTP Headers:**
```
Retry-After: 720                    # 720 seconds
X-RateLimit-Reset: 1736348400       # Unix timestamp
```

**Error Messages:**
```
"rate limit exceeded, try again in 12 minutes"
"quota exceeded, wait 720 seconds"
"retry_after": 600
```

**Default Fallback:**
If no retry-after information is found, VC conservatively waits **1 hour** (the typical quota reset window).

### Tuning Guidelines

**For overnight/unattended execution:**
```bash
# Allow longer waits for quota resets
export VC_MAX_QUOTA_WAIT=1h
```

**For interactive development:**
```bash
# Fail fast on quota errors, don't wait
export VC_MAX_QUOTA_WAIT=1m
```

**For production (recommended):**
```bash
# Default 15 minutes balances patience with responsiveness
export VC_MAX_QUOTA_WAIT=15m
```

### Integration with Cost Budgeting

Quota retry works alongside cost budgeting (vc-e3s7):
- **Cost budgeting**: Proactive limit before hitting API quota
- **Quota retry**: Reactive handling when quota is actually exceeded
- **Together**: Complete quota management solution

When both features are enabled:
1. Cost budgeting prevents most quota errors (stay under limit)
2. Quota retry handles edge cases (concurrent executions, budget estimation errors)
3. System stays operational even under quota pressure

### Related Features

- **Cost Budgeting** (vc-e3s7): Proactive quota management via token limits
- **Bootstrap Mode** (vc-b027): Fallback mode for quota crisis issues
- **Quota Monitoring** (vc-7e21): Real-time burn rate tracking

### Testing

**Unit tests:**
```bash
go test -v ./internal/ai -run "TestClassifyError|TestParseRetryAfter"
```

**Manual testing:**
```bash
# Simulate quota error by reducing MaxQuotaWait to 1 second
export VC_MAX_QUOTA_WAIT=1s
# Run executor until quota is hit
# Should fail fast with clear error message
```

**Verification:**
- Quota errors show clear wait time and reset time
- Wait duration respects `VC_MAX_QUOTA_WAIT`
- Circuit breaker trips faster on quota errors
- No wasted retry attempts during quota wait

---

## 💰 Quota Monitoring and Pre-emptive Alerting (vc-7e21)

VC tracks quota usage in real-time and predicts quota exhaustion **before** it happens, allowing preventive action instead of reactive crisis management.

### The Problem

Without monitoring, quota exhaustion happens unexpectedly:
- No visibility into burn rate trends
- Can't predict when limits will be hit
- Emergency response when quota is already exhausted
- Lost productivity during quota outages

### Proactive Monitoring Solution

VC captures usage snapshots every 5 minutes and uses them to:
1. **Calculate burn rate** (tokens/min, cost/min)
2. **Predict time-to-limit** with confidence scoring
3. **Emit pre-emptive alerts** at escalating levels (YELLOW → ORANGE → RED)
4. **Auto-create crisis issues** when exhaustion is imminent

### Environment Variables

```bash
# Enable/disable quota monitoring (default: true)
export VC_ENABLE_QUOTA_MONITORING=true

# How often to capture usage snapshots (default: 5 minutes)
export VC_QUOTA_SNAPSHOT_INTERVAL=5m

# Alert thresholds (time-to-limit that triggers alerts)
export VC_QUOTA_ALERT_YELLOW=30m    # Warning: 15-30min to limit
export VC_QUOTA_ALERT_ORANGE=15m    # Urgent: 5-15min to limit
export VC_QUOTA_ALERT_RED=5m        # Critical: <5min to limit

# Historical data retention (default: 30 days)
export VC_QUOTA_RETENTION_DAYS=30

# Auto-create P0 quota-crisis issues on RED alerts (default: true)
export VC_QUOTA_AUTO_CREATE_CRISIS_ISSUE=true
```

### Alert Levels

**GREEN** (Healthy):
- >30 minutes until limit at current burn rate
- No alerts emitted (normal operation)

**YELLOW** (Warning):
- 15-30 minutes until limit
- Alert: "Monitor usage, consider reducing AI operations"
- Console warning logged
- Event logged to activity feed

**ORANGE** (Urgent):
- 5-15 minutes until limit
- Alert: "Urgent - reduce AI operations or risk hitting limit"
- Escalated console warning
- Event logged with URGENT severity

**RED** (Critical):
- <5 minutes until limit
- Alert: "CRITICAL - quota exhaustion imminent"
- P0 `quota-crisis` issue auto-created (if enabled)
- Enables Bootstrap Mode for minimal-AI fixes (vc-b027)

### Burn Rate Calculation

VC uses **linear regression over last 15 minutes** of snapshots:

**Algorithm:**
1. Collect snapshots from last 15 minutes (3 snapshots at 5-min intervals)
2. Calculate `tokens_per_minute` and `cost_per_minute` from oldest to newest
3. Project when each limit (tokens, cost) will be reached
4. Report whichever limit will be hit first
5. Include confidence score (based on sample size, 0.0-1.0)

**Confidence scoring:**
- 3 snapshots = 0.6 confidence
- 5+ snapshots = 1.0 confidence
- Only alert if confidence >0.5

### Cost Attribution

Every AI operation is logged with full attribution:
- **Operation type**: assessment, analysis, deduplication, code_review, discovery
- **Model used**: sonnet, haiku, opus
- **Tokens consumed**: input + output
- **Cost**: calculated from token counts
- **Duration**: milliseconds taken
- **Issue**: which issue the operation was for

This enables queries like:
- "Which operation types cost the most?"
- "Which issues burn through quota fastest?"
- "Is sonnet or haiku more cost-effective for assessments?"

See [docs/QUERIES.md](./QUERIES.md) for cost attribution queries.

### Integration with Other Features

**Quota Retry (vc-5b22):**
- Monitoring = proactive (prevent hitting limits)
- Retry = reactive (handle limits gracefully when hit)
- Together = comprehensive quota management

**Bootstrap Mode (vc-b027):**
- RED alert auto-creates `quota-crisis` issue
- Bootstrap mode activates (minimal AI usage)
- Crisis can be fixed without exhausting remaining quota

**Cost Budgeting (vc-e3s7):**
- Budgeting = hard limits (stop at threshold)
- Monitoring = predictive alerts (warn before threshold)
- Together = stay informed while staying under budget

### Example Alert Flow

**Normal operation:**
```
✓ Quota healthy (45min to limit, 85% confidence)
```

**Approaching limit:**
```
⚠️  Quota approaching limit: ~25 minutes remaining at current burn rate
   Burn rate: 3,200 tokens/min ($0.12/min)
   Current usage: 75,000/100,000 tokens ($3.75/$5.00)
   Recommended: Monitor usage. Consider reducing AI operations or increasing quota limits.
```

**Imminent exhaustion:**
```
🚨 CRITICAL: Quota exhaustion in ~4 minutes at current burn rate
   Burn rate: 5,000 tokens/min ($0.18/min)
   Current usage: 95,000/100,000 tokens ($4.80/$5.00)
   Recommended: IMMEDIATE ACTION REQUIRED: Stop non-essential AI operations. Quota crisis issue will be auto-created.

[Auto-created vc-abc: "Quota crisis imminent: <5min until exhaustion"]
```

### Database Schema

**vc_quota_snapshots** - Point-in-time usage (every 5 minutes):
- Hourly tokens/cost used
- Total tokens/cost (all-time)
- Budget status (HEALTHY/WARNING/EXCEEDED)
- Issues worked in this window

**vc_quota_operations** - Individual AI calls:
- Operation type, model, tokens, cost
- Issue attribution
- Duration (for performance analysis)

### Tuning Guidelines

**For high-volume production:**
```bash
# More frequent snapshots for better predictions
export VC_QUOTA_SNAPSHOT_INTERVAL=2m

# Earlier warnings to allow more reaction time
export VC_QUOTA_ALERT_YELLOW=45m
export VC_QUOTA_ALERT_ORANGE=20m
export VC_QUOTA_ALERT_RED=10m
```

**For development/testing:**
```bash
# Less frequent snapshots (reduce noise)
export VC_QUOTA_SNAPSHOT_INTERVAL=10m

# Shorter retention (save disk space)
export VC_QUOTA_RETENTION_DAYS=7

# Disable auto-issue creation (manual review)
export VC_QUOTA_AUTO_CREATE_CRISIS_ISSUE=false
```

**For cost-sensitive environments:**
```bash
# Aggressive early warnings
export VC_QUOTA_ALERT_YELLOW=50m
export VC_QUOTA_ALERT_ORANGE=30m
export VC_QUOTA_ALERT_RED=15m

# Auto-create crisis issues earlier
export VC_QUOTA_AUTO_CREATE_CRISIS_ISSUE=true
```

### Monitoring Queries

See [docs/QUERIES.md](./QUERIES.md) for comprehensive queries including:
- Current burn rate calculation
- Time-to-limit prediction
- Top quota consumers by operation/issue/model
- Budget window analysis
- Cost efficiency metrics

### Performance Impact

Minimal overhead:
- Snapshot collection: <1ms every 5 minutes
- Burn rate calculation: <5ms (only on snapshots)
- Database writes: Batched, non-blocking
- No impact on AI operations

### Cleanup and Maintenance

Old data is automatically cleaned up:
```bash
# Default retention: 30 days
# Runs daily as background goroutine
# Cleanup is transactional and batched
```

Manual cleanup (future):
```bash
vc cleanup quotas --dry-run    # Preview
vc cleanup quotas               # Execute
```

### Related Features

- **vc-5b22**: Intelligent quota retry (reactive handling)
- **vc-b027**: Bootstrap mode (minimal AI for crisis fixes)
- **vc-e3s7**: Cost budgeting (proactive limits)

---

## 🆘 Bootstrap Mode (vc-b027)

**Bootstrap mode** enables VC to fix quota-related issues even when AI budget is exhausted, breaking the circular dependency where quota issues need AI supervision but no quota is available.

### The Problem

Without bootstrap mode, quota exhaustion creates a deadlock:
- Quota issues need to be fixed to restore AI budget
- Fixing issues requires AI supervision (assessment, analysis)
- But AI supervision requires available quota
- **Result:** VC is stuck and cannot self-heal

### Bootstrap Mode Solution

Bootstrap mode is a **degraded execution mode** that activates automatically when:
1. AI budget is exceeded (cost tracker status = `BudgetExceeded`) AND
2. Issue has `quota-crisis` label OR title contains quota-related keywords

When active, bootstrap mode:
- ✅ **Still runs**: Agent execution, quality gates (test/lint/build)
- ❌ **Skips**: AI assessment, AI analysis, discovered issue creation, deduplication

This allows VC to work on quota fixes with minimal AI usage.

### Environment Variables

```bash
# Enable bootstrap mode (default: false, opt-in for safety)
# IMPORTANT: Only enable if you trust VC to work without AI supervision
export VC_ENABLE_BOOTSTRAP_MODE=true

# Labels that trigger bootstrap mode (default: quota-crisis)
# Comma-separated list of labels
export VC_BOOTSTRAP_MODE_LABELS="quota-crisis,budget-fix"

# Title keywords that trigger bootstrap mode (default: quota,budget,cost,API limit)
# Comma-separated list (case-insensitive)
export VC_BOOTSTRAP_MODE_TITLE_KEYWORDS="quota,budget,cost,API limit"
```

### Activation Logic

Bootstrap mode activates when **ALL** conditions are met:
1. `VC_ENABLE_BOOTSTRAP_MODE=true`
2. Cost tracker reports `BudgetExceeded`
3. Either:
   - Issue has a label matching `VC_BOOTSTRAP_MODE_LABELS` OR
   - Issue title contains any keyword from `VC_BOOTSTRAP_MODE_TITLE_KEYWORDS`

**Example scenarios:**

✅ **Activates:**
- Issue: "Fix quota exhaustion in cost tracker" + budget exceeded
- Issue labeled `quota-crisis` + budget exceeded

❌ **Doesn't activate:**
- Issue: "Fix authentication bug" (not quota-related)
- Issue: "Fix quota exhaustion" but budget not exceeded (not a crisis yet)
- Budget exceeded but bootstrap mode disabled in config

### What Changes in Bootstrap Mode

**Assessment Phase (Skipped):**
- No AI assessment call
- No risk analysis
- No pre-flight checks
- Logs: "Skipping AI assessment (bootstrap mode active)"

**Analysis Phase (Skipped):**
- No AI analysis call
- No quality validation
- No discovered issue creation
- Logs: "Skipping AI analysis (bootstrap mode active)"

**Deduplication (Skipped):**
- No AI deduplication calls
- All discovered issues treated as unique
- **Risk:** May create duplicate issues
- Logs: "Bootstrap mode active - skipping deduplication (risk of duplicates)"

**Quality Gates (Still Run):**
- Tests must still pass
- Linting must still pass
- Build must still succeed
- No degradation in code quality enforcement

**Agent Execution (Still Runs):**
- Coding agent executes normally
- Uses separate API key via Amp CLI
- Not affected by VC's AI budget

### Visibility and Logging

When bootstrap mode activates, VC emits:

**Console Warning:**
```
⚠️  BOOTSTRAP MODE ACTIVATED for vc-123 (reason: budget_exceeded + label:quota-crisis)
   Budget status: EXCEEDED (hourly: 105000/100000 tokens, $5.25/$5.00)
   ⚠️  LIMITED AI SUPERVISION: No assessment, no analysis, no discovered issues
```

**Activity Feed Event:**
```json
{
  "type": "bootstrap_mode_activated",
  "severity": "WARNING",
  "issue_id": "vc-123",
  "reason": "budget_exceeded + label:quota-crisis",
  "budget_status": "EXCEEDED",
  "hourly_tokens_used": 105000,
  "hourly_tokens_limit": 100000
}
```

**Issue Comment:**
```markdown
⚠️ **BOOTSTRAP MODE ACTIVE**

This issue is being executed in bootstrap mode due to quota exhaustion.

**Limitations:**
- No AI assessment (pre-flight checks)
- No AI analysis (quality validation)
- No discovered issue creation (follow-on work)
- No deduplication (risk of duplicates)

**Quality gates still enforce:**
- Tests must pass
- Linting must pass
- Build must succeed

Reason: budget_exceeded + label:quota-crisis
Budget: 105000/100000 tokens used ($5.25/$5.00)
```

### Safety Mechanisms

**Opt-in Required:**
- Bootstrap mode disabled by default
- Requires explicit `VC_ENABLE_BOOTSTRAP_MODE=true`
- Must be consciously enabled to use

**Limited Scope:**
- Only affects issues with specific labels/keywords
- Normal issues wait for budget reset
- No system-wide AI supervision bypass

**Quality Gates Still Apply:**
- Tests must pass
- Linting must pass
- Build must succeed
- Code quality is not degraded

**Clear Visibility:**
- Prominent warnings when activated
- Activity feed event logged
- Issue comment added for audit trail

### Limitations and Risks

**What You Lose:**

1. **No AI Assessment:**
   - No risk analysis
   - No pre-flight validation
   - No strategic planning
   - May miss complex issues

2. **No AI Analysis:**
   - No completion validation
   - No quality issue detection
   - No punted items tracking
   - May mark incomplete work as done

3. **No Discovered Issues:**
   - Follow-on work not automatically filed
   - Must manually track remaining tasks
   - May lose context for future work

4. **No Deduplication:**
   - May create duplicate issues
   - Increases tracker noise
   - Requires manual cleanup later

**When NOT to Use Bootstrap Mode:**

- ❌ Complex architectural changes (need assessment)
- ❌ Production incidents (need full analysis)
- ❌ Issues that typically spawn many discovered issues
- ❌ Any work where AI supervision is critical

**Mitigation Strategies:**

1. **Manual review:** Review bootstrap mode executions more carefully
2. **Post-budget sweep:** Run deduplication sweep after budget resets
3. **Follow-up issues:** Manually file discovered work after execution
4. **Label tracking:** Add `bootstrap-mode-used` label for audit

### Integration with Other Features

**Quota Monitoring (vc-7e21):**
- Monitoring predicts quota exhaustion
- AUTO-creates `quota-crisis` issue on RED alert
- Bootstrap mode activates for auto-created issue
- Crisis can be fixed before complete exhaustion

**Cost Budgeting (vc-e3s7):**
- Budgeting blocks AI calls when budget exceeded
- Bootstrap mode works around this for quota issues
- Non-quota issues still respect budget limits

**Quota Retry (vc-5b22):**
- Retry handles temporary quota errors
- Bootstrap mode handles exhausted budgets
- Together: comprehensive quota crisis handling

### Example Workflow

**Normal operation:**
```
1. Quota monitoring detects high burn rate
2. RED alert issued (<5min to limit)
3. Auto-creates vc-abc: "Quota crisis: reduce burn rate"
4. Label: quota-crisis
5. Executor claims vc-abc
6. Budget now exceeded (from continued use)
7. Bootstrap mode activates (quota-crisis label + budget exceeded)
8. Agent runs with minimal AI supervision
9. Quality gates enforce correctness
10. Issue closed, burn rate reduced
11. Budget resets in new hour
12. Normal operation resumes
```

**Manual quota fix:**
```bash
# Create quota crisis issue
bd create "Fix quota exhaustion in deduplication" \
  -t bug \
  -p 0 \
  --label quota-crisis

# Enable bootstrap mode
export VC_ENABLE_BOOTSTRAP_MODE=true

# Start executor - will use bootstrap mode for this issue
vc run
```

### Testing Bootstrap Mode

**Unit tests:**
```bash
# Test bootstrap mode detection
go test -v ./internal/executor -run TestBootstrapMode

# Test AI skipping in bootstrap mode
go test -v ./internal/executor -run TestBootstrapModeSkipsAI
```

**Manual testing:**
```bash
# 1. Exhaust AI budget
export VC_COST_MAX_TOKENS_PER_HOUR=100
export VC_COST_MAX_COST_PER_HOUR=0.01

# 2. Enable bootstrap mode
export VC_ENABLE_BOOTSTRAP_MODE=true

# 3. Create quota issue
bd create "Test bootstrap mode" --label quota-crisis -p 0

# 4. Run executor
vc run

# 5. Verify in logs:
# - "BOOTSTRAP MODE ACTIVATED" warning
# - "Skipping AI assessment (bootstrap mode active)"
# - "Skipping AI analysis (bootstrap mode active)"
# - Quality gates still run
```

**Verification checklist:**
- ✅ Bootstrap mode only activates when budget exceeded + label/keyword match
- ✅ Assessment phase skipped
- ✅ Analysis phase skipped
- ✅ Deduplication skipped
- ✅ Quality gates still run
- ✅ Clear warnings logged
- ✅ Activity feed event emitted
- ✅ Issue comment added

### Tuning Guidelines

**Conservative (recommended):**
```bash
# Only enable for emergencies
export VC_ENABLE_BOOTSTRAP_MODE=false  # Disabled by default

# Only manual quota issues
export VC_BOOTSTRAP_MODE_LABELS="quota-crisis"

# Strict keyword matching
export VC_BOOTSTRAP_MODE_TITLE_KEYWORDS="quota,budget"
```

**Aggressive (for self-hosting):**
```bash
# Always enabled
export VC_ENABLE_BOOTSTRAP_MODE=true

# Broader label matching
export VC_BOOTSTRAP_MODE_LABELS="quota-crisis,budget-fix,cost-emergency"

# More lenient keyword matching
export VC_BOOTSTRAP_MODE_TITLE_KEYWORDS="quota,budget,cost,API,rate limit,exhaustion"
```

### Related Features

- **vc-7e21**: Quota monitoring (creates quota-crisis issues)
- **vc-e3s7**: Cost budgeting (enforces limits that trigger bootstrap)
- **vc-5b22**: Quota retry (handles transient quota errors)

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
