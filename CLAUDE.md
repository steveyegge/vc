# Instructions for AI Agents Working on VC

## 🎯 Starting a Session: "What's Next?"

**VC uses Beads for issue tracking.** All work is tracked in the `.beads/vc.db` SQLite database.

### Environment Setup

**Installing bd:**

The `bd` command should be installed via Homebrew for the best experience:

```bash
# One-time setup: tap the local beads repository
brew tap steveyegge/beads ~/src/homebrew-beads

# Install bd (installs to /opt/homebrew/bin/bd)
brew install bd

# Verify installation
bd version
```

This ensures you're always using the latest stable version from the homebrew tap.

**Alternative environments:**
- **GCE VM (claude-code-dev-vm)**: Beads is at `/workspace/beads/bd`
- **Development from source**: Build from `~/src/beads/` with `go build ./cmd/bd`

**For Claude Code sessions:** Simply use `bd` commands - the binary will be found automatically in `/opt/homebrew/bin/`.

**AI Supervision Requirements:**
- **`ANTHROPIC_API_KEY`**: Required for AI supervision (assessment and analysis)
- Export the environment variable: `export ANTHROPIC_API_KEY=your-key-here`
- Without this key, the executor will run without AI supervision (warnings will be logged)
- AI supervision can be explicitly disabled via config: `EnableAISupervision: false`

**Debug Environment Variables:**
- **`VC_DEBUG_PROMPTS`**: Log full prompts sent to agents (useful for debugging agent behavior)
- **`VC_DEBUG_EVENTS`**: Log JSON event parsing details (tool_use events from Amp --stream-json)
  ```bash
  export VC_DEBUG_EVENTS=1  # Enable debug logging for agent progress events
  ```

**Daemon Coexistence (vc-195):**
- **VC uses an exclusive lock protocol** to prevent bd daemon from interfering with execution
- When VC executor starts, it creates `.beads/.exclusive-lock`
- bd daemon (v0.17.3+) checks for this lock and skips locked databases
- **You can safely run bd daemon alongside VC** - they coexist peacefully
- VC manages its database exclusively, daemon manages other databases
- Lock is automatically removed on executor shutdown

When starting a new session:

```bash
# 1. Check for ready work (no blockers)
bd ready

# 2. View issue details
bd show vc-X

# 3. Start working on it (leave as 'open', add notes)
bd update vc-X --notes "Starting work in Claude Code session"
```

**Important**: Use the `bd` command from your beads installation - the VC binary doesn't exist yet (that's what we're building!).

---

## 📋 Current Focus

VC is in **bootstrap phase**. We're building the AI-supervised issue workflow from scratch in Go.

**Check ready work**:
```bash
bd ready --limit 5
```

**View dependency chain**:
```bash
bd list
bd dep tree vc-5
```

---

## 🏗️ Project Structure

```
vc/
├── .beads/
│   ├── vc.db           # Issue tracker database (derived from JSONL)
│   └── issues.jsonl    # Source of truth (commit this to git)
├── internal/
│   ├── types/          # Core data types
│   └── storage/        # Storage layer (from beads)
├── cmd/vc/             # VC CLI (to be built)
├── README.md           # Project overview
├── BOOTSTRAP.md        # Old roadmap (being replaced by beads)
└── CLAUDE.md           # This file
```

---

## 🔄 Workflow

### Finding Work

```bash
# Ready work (no blockers)
bd ready

# All open issues
bd list --status open

# Show specific issue with dependencies
bd show vc-X
```

### Claiming Work

**IMPORTANT**: The `in_progress` status is **ONLY** for active VC worker/agent execution. Claude Code sessions and humans should **NOT** use `in_progress`.

```bash
# For Claude Code / Human work: Leave as 'open' and update notes
bd update vc-X --notes "Working on this in Claude Code session"
bd update vc-X --notes "Progress: implemented X, testing Y"

# ONLY VC workers set in_progress (automatic when VC claims work)
# This makes orphan detection trivial: stale in_progress = orphaned worker
```

### Creating Issues

```bash
# Create child issue
bd create \
  "Issue title" \
  -t task \
  -p 2 \
  -d "Description" \
  --design "Design notes" \
  --acceptance "Success criteria"

# Add dependency (if needed)
bd dep add vc-NEW vc-PARENT --type blocks
```

### Completing Work

```bash
# Before closing, ensure:
# - All acceptance criteria met
# - Tests passing (when test infrastructure exists)
# - Code documented

# Close issue
bd close vc-X --reason "Completed all acceptance criteria"
```

### Export to Git

**CRITICAL - Always export before committing**:
```bash
bd export -o .beads/issues.jsonl
git add .beads/issues.jsonl
```

**The `.beads/issues.jsonl` file is the source of truth**, not the database. The database is a local cache that gets rebuilt from the JSONL file.

---

## 🎯 Bootstrap Epics (Current Roadmap)

The 9 core epics in priority order:

1. **vc-5**: Beads Integration and Executor Tables ← **START HERE**
2. **vc-6**: Issue Processor Event Loop
3. **vc-7**: AI Supervision (Assess and Analyze)
4. **vc-8**: Quality Gates Enforcement
5. **vc-9**: REPL Shell and Natural Language Interface
6. **vc-1**: Activity Feed and Event Streaming
7. **vc-2**: Recursive Refinement and Follow-On Missions
8. **vc-3**: Watchdog and Convergence Detection
9. **vc-4**: Git Operations Integration

Each epic has:
- **Description**: Why this work matters
- **Design**: High-level approach
- **Acceptance Criteria**: Definition of done

---

## 🧩 Core Principles

### Zero Framework Cognition (ZFC)
All decisions delegated to AI. No heuristics, regex, or parsing in the orchestration layer.

### Issue-Oriented Orchestration
Work flows through the issue tracker. Dependencies are explicit. The executor claims ready work atomically.

### Nondeterministic Idempotence
Operations can crash and resume. AI figures out where we left off and continues.

### Tracer Bullet Development
Get end-to-end basics working before adding bells and whistles.

---

## 🔍 Understanding the Vision

**VC is building an AI-supervised coding agent colony.**

The workflow:
```
1. User: "Fix bug X"
2. AI translates to issue
3. Executor claims issue
4. AI assesses: strategy, steps, risks
5. Agent executes the work
6. AI analyzes: completion, punted items, discovered bugs
7. Auto-create follow-on issues
8. Quality gates enforce standards
9. Repeat until done
```

**Why this works**:
- Small, focused tasks (better agent performance)
- AI supervision (catches mistakes early)
- Automatic work discovery (nothing gets forgotten)
- Quality gates (prevent broken code)
- Issue tracker (handles complexity via dependencies)

---

## 📚 Key Files to Read

- **README.md** - Project vision and architecture
- **BOOTSTRAP.md** - Original roadmap (now in beads as vc-5 through vc-9)
- **Issue tracker** - Use `bd show vc-X` to read full issue details
- **TypeScript prototype** - The 350k LOC reference implementation at `../zoey/vc/`

---

## 🚧 What We're Building Toward

**End state**: User says "let's continue", VC:
1. Finds ready work in tracker
2. Claims issue atomically
3. AI assesses the task
4. Spawns coding agent (Amp/Claude Code)
5. AI analyzes the result
6. Creates follow-on issues for discovered work
7. Runs quality gates
8. Repeats until all work complete

**Then**: Code is ready for human review and merge.

---

## 💾 Database Bootstrap and Migrations

### Automatic Initialization

**The database schema is created automatically** when you first connect to a new database. You don't need to run initialization scripts manually - just start using the storage layer and it will set everything up.

The SQLite backend automatically:
1. Creates all tables (issues, dependencies, labels, events, executor_instances, issue_execution_state)
2. Creates indexes for performance
3. Creates views for ready work and blocked issues
4. Sets up foreign key constraints

### Manual Initialization (Optional)

If you want to pre-initialize a database, use the provided script:

```bash
# Initialize SQLite database (default: .beads/vc.db)
./scripts/init-db.sh

# Initialize SQLite at custom location
VC_DB_PATH=/path/to/db.sqlite ./scripts/init-db.sh
```

### Schema Migrations

**vc-37**: VC now uses Beads v0.12.0 as its storage library. Schema management works as follows:

- **Beads core tables**: Managed by the Beads library (issues, dependencies, labels, etc.)
- **VC extension tables**: Created inline in `internal/storage/beads/wrapper.go`
- **Column migrations**: Handled by `migrateAgentEventsTable()` function

The old `internal/storage/migrations/` framework has been removed. VC follows the IntelliJ/Android Studio extension model:
- Beads provides the platform (general-purpose issue tracking)
- VC adds extension tables in the same database
- No modifications to Beads core schema
```

### Storage Configuration

VC uses SQLite for simple, lightweight operation:

```go
import "github.com/steveyegge/vc/internal/storage"

// Default configuration (.beads/vc.db)
cfg := storage.DefaultConfig()
store, err := storage.NewStorage(ctx, cfg)

// Custom path
cfg := storage.DefaultConfig()
cfg.Path = "/path/to/custom.db"
store, err := storage.NewStorage(ctx, cfg)

// In-memory database (useful for tests)
cfg := storage.DefaultConfig()
cfg.Path = ":memory:"
store, err := storage.NewStorage(ctx, cfg)
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

### Example: Conservative Configuration

For critical projects where missing work is worse than having duplicates:

```bash
export VC_DEDUP_CONFIDENCE_THRESHOLD=0.95  # Very high confidence required
export VC_DEDUP_FAIL_OPEN=true             # File on error
export VC_DEDUP_MAX_CANDIDATES=30          # Limited comparisons
```

### Example: Aggressive Configuration

For projects with lots of duplicate work being filed:

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

---

## 🛑 Executor Graceful Shutdown

The executor supports graceful shutdown via SIGTERM/SIGINT signals (Ctrl+C). When a shutdown signal is received, the executor:

1. **Stops claiming new work** - The event loop exits immediately after completing the current poll
2. **Allows current execution to finish** - If an issue is being processed, it continues (with 30-second grace period)
3. **Handles quality gates cancellation** - If gates are running when shutdown occurs:
   - Gates detect context cancellation and stop cleanly
   - Issue is NOT marked as blocked due to cancellation
   - Issue returns to 'open' status for retry
4. **Releases executor claims** - Instance is marked as stopped, claims are released by cleanup loop
5. **Closes connections** - Database and other resources are closed properly

### Graceful Shutdown Behavior

**During normal operation:**
```bash
$ ./vc execute
✓ Executor started (version 0.1.0)
  Polling for ready work every 5s
  Press Ctrl+C to stop

Executing issue vc-123: Fix authentication bug
Running quality gates (timeout: 5m)...
  test: PASS
^C

Shutting down executor...
Warning: quality gates cancelled due to executor shutdown
✓ Executor stopped
```

**Key Points:**
- **30-second grace period** - Executor has 30 seconds to complete current work before forced termination
- **Quality gates respect cancellation** - Gates check for context cancellation and exit cleanly
- **No false negatives** - Issues interrupted during execution are NOT marked as failed/blocked
- **Automatic cleanup** - Stale instance cleanup releases any orphaned claims

### For Developers

When implementing operations that may be interrupted:

```go
// Good: Check for cancellation before long operations
select {
case <-ctx.Done():
    return fmt.Errorf("operation cancelled: %w", ctx.Err())
default:
    // Continue with operation
}

// Good: Distinguish between timeout and cancellation
if ctx.Err() == context.DeadlineExceeded {
    // Operation timed out - this is a failure
} else if ctx.Err() == context.Canceled {
    // Executor is shutting down - not a failure, just cleanup
}
```

### Troubleshooting

**Issue stuck in 'in_progress' after executor kill:**
- Run the cleanup loop: The executor's cleanup goroutine runs every 5 minutes
- Issues claimed by stopped instances are automatically released
- Or manually release: `bd update vc-X --status open`

**Context canceled errors during shutdown:**
- Normal during graceful shutdown
- Quality gates and storage operations log warnings but don't fail
- Issues are properly released during cleanup phase

---

## 📐 Dependency Direction Convention

**CRITICAL**: Always use `(child, parent)` direction for parent-child dependencies.

```bash
bd dep add vc-10 vc-5 --type parent-child  # Child vc-10 depends ON parent vc-5
```

- `GetDependencies(child)` → returns parents
- `GetDependents(parent)` → returns children

Early issues (vc-5 through vc-9) had inverted dependencies, fixed in vc-90. All new code must use standard direction.

---

## ⚠️ Important Notes

- **JSONL is source of truth** - `.beads/issues.jsonl` in git, NOT the database
- **Import after pull/rebase** - Run `bd import .beads/issues.jsonl` to sync database
- **Don't use markdown TODOs** - Everything goes in beads
- **Don't create one-off scripts** - Use `bd` commands
- **Always export before committing** - Keep JSONL in sync with database changes
- **bd daemon can coexist with VC** - VC uses exclusive lock protocol (vc-195, requires Beads v0.17.3+)
- **Beads path is `.beads/vc.db`** - Not `.vc/vc.db` (README is outdated)
- **Bootstrap first** - Don't jump ahead to advanced features
- **Set `ANTHROPIC_API_KEY`** - Required for AI supervision features (assessment, analysis, discovered issues)
- **Use standard dependency direction** - Always `(child, parent)`, never `(parent, child)`

---

## 🆘 Common Commands Reference

```bash
# === Finding work ===
bd ready                    # Show ready work
bd list --status open       # All open issues
bd show vc-X                # Issue details

# === Managing issues ===
bd update vc-X --status in_progress
bd update vc-X --notes "Progress update"
bd close vc-X --reason "Done"

# === Dependencies ===
bd dep tree vc-X            # Show dependency tree
bd dep add vc-A vc-B        # A depends on B

# === Sync with git ===
bd import .beads/issues.jsonl   # Import JSONL to database (after pull)
bd export -o .beads/issues.jsonl # Export database to JSONL (before commit)
```

---

## 🎓 First Session Checklist

1. Read this file (CLAUDE.md)
2. Read README.md for vision
3. Run `bd ready` to see what's ready
4. Run `bd show vc-5` to see first epic
5. Start working on vc-5 or break it down into child issues
6. Export to JSONL before committing

---

## 🔄 Git Workflow & Source of Truth

**CRITICAL**: The `.beads/issues.jsonl` file checked into git is the **source of truth**. The `.beads/vc.db` SQLite database is a **local cache** derived from the JSONL file.

### After pulling/rebasing:

```bash
# The JSONL file from git is canonical
# DO NOT re-export from your local database - it may be stale
# If you made local changes, merge them carefully or discard them

# To sync your database from git's JSONL:
bd import .beads/issues.jsonl
```

### When merging conflicts:

If `.beads/issues.jsonl` has merge conflicts, **never resolve by re-exporting from your local database**. Instead:
1. Resolve conflicts in the JSONL file manually or take one side
2. Import the resolved JSONL into your database: `bd import .beads/issues.jsonl`

### Before committing your work:

```bash
# Export your changes to JSONL
bd export -o .beads/issues.jsonl

# Commit both code and issue changes together
git add .beads/issues.jsonl
git commit -m "your message"
```

---

**Remember**: When in doubt, check `bd ready` to see what needs doing!

---

## 💬 Using the VC Conversational REPL (For End Users)

**Note**: This section describes the VC REPL for end users. As an AI agent working on VC's codebase, you'll use `bd` commands. But users of VC will interact via the conversational interface.

### Pure Conversational Interface

VC provides a natural language interface - no slash commands to memorize (except `/quit`).

**Starting VC:**
```bash
export ANTHROPIC_API_KEY=your-key-here
./vc
```

**Example conversations:**

Finding and starting work:
```
You: What's ready to work on?
AI: [Shows ready issues]
You: Let's continue working
AI: [Executes next ready issue]
```

Creating issues:
```
You: We need Docker support
AI: [Creates feature issue]
You: Add tests for authentication
AI: [Creates task]
```

Monitoring:
```
You: How's the project doing?
AI: [Shows project statistics]
You: What's blocked?
AI: [Lists blocked issues with details]
```

Multi-turn context:
```
You: Create an epic for user management
AI: [Creates epic vc-200]
You: Add login, registration, and password reset as children
AI: [Creates 3 tasks and links to epic]
```

### Available Conversational Tools

The AI has access to these tools (you don't call them directly):
- **create_issue**: Creates issues from natural language
- **create_epic**: Creates epic (container) issues
- **add_child_to_epic**: Links issues to epics
- **get_ready_work**: Shows issues ready to execute
- **get_issue**: Retrieves issue details
- **get_status**: Shows project statistics
- **get_blocked_issues**: Lists blocked issues
- **continue_execution**: Executes work (the VibeCoder Primitive)
- **get_recent_activity**: Shows agent execution history
- **search_issues**: Searches issues by text

The AI understands your intent and uses these tools automatically.

---

## 📊 Querying Deduplication Metrics (vc-151)

VC tracks comprehensive deduplication metrics in the `agent_events` table. All deduplication operations emit structured events that can be queried for analysis.

### Event Types

Three event types track deduplication activity:

1. **`deduplication_batch_started`** - When batch deduplication begins
2. **`deduplication_batch_completed`** - When batch deduplication completes (with stats)
3. **`deduplication_decision`** - Individual duplicate decisions (with confidence scores)

### Quick Queries

**View recent deduplication batches:**
```sql
SELECT
  timestamp,
  issue_id,
  message,
  json_extract(data, '$.total_candidates') as candidates,
  json_extract(data, '$.unique_count') as unique,
  json_extract(data, '$.duplicate_count') as duplicates,
  json_extract(data, '$.ai_calls_made') as ai_calls,
  json_extract(data, '$.processing_time_ms') as time_ms
FROM agent_events
WHERE type = 'deduplication_batch_completed'
  AND json_extract(data, '$.success') = 1
ORDER BY timestamp DESC
LIMIT 10;
```

**Confidence score distribution:**
```sql
SELECT
  ROUND(json_extract(data, '$.confidence'), 1) as confidence_bucket,
  COUNT(*) as count,
  SUM(CASE WHEN json_extract(data, '$.is_duplicate') = 1 THEN 1 ELSE 0 END) as duplicates,
  SUM(CASE WHEN json_extract(data, '$.is_duplicate') = 0 THEN 1 ELSE 0 END) as unique
FROM agent_events
WHERE type = 'deduplication_decision'
GROUP BY confidence_bucket
ORDER BY confidence_bucket DESC;
```

**Deduplication efficiency over time:**
```sql
SELECT
  date(timestamp) as date,
  COUNT(*) as batches,
  SUM(json_extract(data, '$.total_candidates')) as total_candidates,
  SUM(json_extract(data, '$.duplicate_count')) as total_duplicates,
  ROUND(100.0 * SUM(json_extract(data, '$.duplicate_count')) /
        SUM(json_extract(data, '$.total_candidates')), 2) as duplicate_rate_pct,
  SUM(json_extract(data, '$.ai_calls_made')) as total_ai_calls,
  AVG(json_extract(data, '$.processing_time_ms')) as avg_time_ms
FROM agent_events
WHERE type = 'deduplication_batch_completed'
  AND json_extract(data, '$.success') = 1
GROUP BY date
ORDER BY date DESC
LIMIT 30;
```

**Failed deduplication operations:**
```sql
SELECT
  timestamp,
  issue_id,
  message,
  json_extract(data, '$.error') as error
FROM agent_events
WHERE type = 'deduplication_batch_completed'
  AND json_extract(data, '$.success') = 0
ORDER BY timestamp DESC;
```

**Individual duplicate decisions for an issue:**
```sql
SELECT
  json_extract(data, '$.candidate_title') as title,
  json_extract(data, '$.is_duplicate') as is_dup,
  json_extract(data, '$.duplicate_of') as dup_of,
  json_extract(data, '$.confidence') as confidence,
  json_extract(data, '$.reasoning') as reasoning
FROM agent_events
WHERE type = 'deduplication_decision'
  AND issue_id = 'vc-XXX'
ORDER BY timestamp;
```

**Top duplicate issues (what issues are most frequently found as duplicates):**
```sql
SELECT
  json_extract(data, '$.duplicate_of') as issue_id,
  COUNT(*) as times_found_as_duplicate,
  GROUP_CONCAT(DISTINCT json_extract(data, '$.candidate_title'), '; ') as duplicate_titles
FROM agent_events
WHERE type = 'deduplication_decision'
  AND json_extract(data, '$.is_duplicate') = 1
  AND json_extract(data, '$.duplicate_of') IS NOT NULL
GROUP BY json_extract(data, '$.duplicate_of')
ORDER BY times_found_as_duplicate DESC
LIMIT 20;
```

### Data Fields

**DeduplicationBatchCompletedData:**
- `total_candidates` - Number of issues checked
- `unique_count` - Number of unique issues
- `duplicate_count` - Duplicates against existing issues
- `within_batch_duplicate_count` - Duplicates within the batch
- `comparisons_made` - Total pairwise comparisons
- `ai_calls_made` - Number of AI API calls
- `processing_time_ms` - Time taken in milliseconds
- `success` - Whether deduplication succeeded
- `error` - Error message (if failed)

**DeduplicationDecisionData:**
- `candidate_title` - Title of the candidate issue
- `is_duplicate` - Whether marked as duplicate
- `duplicate_of` - ID of existing issue (if duplicate)
- `confidence` - AI confidence score (0.0 to 1.0)
- `reasoning` - AI explanation for the decision
- `within_batch_duplicate` - If this is a within-batch duplicate
- `within_batch_original` - Reference to original (if within-batch)

### Monitoring Deduplication Health

**Check for high false positive rate (low confidence duplicates being marked):**
```sql
SELECT COUNT(*) as low_confidence_duplicates
FROM agent_events
WHERE type = 'deduplication_decision'
  AND json_extract(data, '$.is_duplicate') = 1
  AND json_extract(data, '$.confidence') < 0.90;
```

**Check for deduplication performance issues:**
```sql
SELECT
  AVG(json_extract(data, '$.processing_time_ms')) as avg_ms,
  MAX(json_extract(data, '$.processing_time_ms')) as max_ms,
  AVG(json_extract(data, '$.ai_calls_made')) as avg_calls
FROM agent_events
WHERE type = 'deduplication_batch_completed'
  AND json_extract(data, '$.success') = 1
  AND timestamp > datetime('now', '-7 days');
```

---

## 📡 Agent Progress Events (vc-129)

**Status:** Implemented. Agent tool usage is now captured and stored in real-time.

### Overview

When agents execute in background mode, their progress is captured as structured events in the activity feed. This provides visibility into what agents are doing and helps distinguish between actual hangs and normal operation.

### Event Types

Three new event types were added for agent progress tracking:

1. **`agent_tool_use`** - Captured when agent invokes a tool (Read, Edit, Write, Bash, Glob, Grep, Task)
2. **`agent_heartbeat`** - Periodic progress updates (future - not yet emitted)
3. **`agent_state_change`** - Agent state transitions like thinking→planning→executing (future - not yet emitted)

### Tool Usage Detection

The parser automatically detects tool usage from agent output patterns like:
- "Let me use the Read tool to read the file"
- "I'll use the Edit tool to modify parser.go"
- "Using the Bash tool to run tests"
- "Spawning the Task tool to launch an agent"

**Supported tools:**
- Read (file reads)
- Edit (file modifications)
- Write (file creation)
- Bash (command execution)
- Glob (file search by pattern)
- Grep (content search)
- Task (agent spawning)
- Generic fallback for any "XYZ tool" pattern

### Event Data Structure

**AgentToolUseData:**
```go
{
  "tool_name": "Read",              // Name of the tool invoked
  "tool_description": "read the configuration file",  // What the tool is doing
  "target_file": "config.yaml",     // File being operated on (if applicable)
  "command": ""                     // Command being executed (for Bash tool)
}
```

**AgentHeartbeatData (future):**
```go
{
  "current_action": "Running tests",  // What agent is currently doing
  "elapsed_seconds": 120              // Time since agent started
}
```

**AgentStateChangeData (future):**
```go
{
  "from_state": "thinking",           // Previous state
  "to_state": "executing",            // New state
  "description": "Starting implementation"  // Context
}
```

### Querying Progress Events

**View tool usage for an issue:**
```sql
SELECT
  timestamp,
  message,
  json_extract(data, '$.tool_name') as tool,
  json_extract(data, '$.target_file') as file,
  json_extract(data, '$.tool_description') as description
FROM agent_events
WHERE type = 'agent_tool_use'
  AND issue_id = 'vc-XXX'
ORDER BY timestamp;
```

**Tool usage frequency:**
```sql
SELECT
  json_extract(data, '$.tool_name') as tool,
  COUNT(*) as usage_count
FROM agent_events
WHERE type = 'agent_tool_use'
  AND timestamp > datetime('now', '-7 days')
GROUP BY tool
ORDER BY usage_count DESC;
```

**Agent activity timeline:**
```sql
SELECT
  timestamp,
  type,
  message,
  CASE type
    WHEN 'agent_tool_use' THEN json_extract(data, '$.tool_name')
    WHEN 'file_modified' THEN json_extract(data, '$.file_path')
    WHEN 'git_operation' THEN json_extract(data, '$.command')
    ELSE ''
  END as detail
FROM agent_events
WHERE issue_id = 'vc-XXX'
  AND type IN ('agent_tool_use', 'file_modified', 'git_operation', 'progress')
ORDER BY timestamp;
```

### Future Work

**Not yet implemented** (punted for now):

1. **Heartbeat emission** - Agent doesn't emit periodic heartbeat events yet
   - Would require goroutine in agent.go to emit events every 30-60s
   - Would track "current action" based on recent tool usage

2. **State change detection** - No explicit state tracking yet
   - Would require analyzing output patterns for thinking/planning/executing
   - Or structured state markers in agent output

3. **Watchdog integration** (vc-234) - Watchdog doesn't consume progress events yet
   - Would check time since last progress event
   - Would distinguish stuck (no events >5min) vs thinking (recent events)

4. **CLI visualization** - No `vc tail -f --issue vc-X` command yet
   - Would stream progress events in real-time
   - Would show colorized tool usage, file changes, etc.

### Why This Helps

**Before vc-129:**
- Agent spawned, no output for 5+ minutes
- Activity feed showed "agent_spawned" then silence
- Appeared stuck, but was actually working
- Watchdog saw "0 executions" (no progress events)

**After vc-129:**
- Tool usage captured in real-time (Read, Edit, Write, Bash, etc.)
- Activity feed shows what agent is doing
- Clear distinction between working vs stuck
- Foundation for watchdog convergence detection

**Example event stream:**
```
10:06:20 agent_spawned: Claude Code started on vc-123
10:06:25 agent_tool_use: Read tool - parser.go
10:06:30 agent_tool_use: Glob tool - find test files
10:07:15 file_modified: Created parser_test.go
10:07:45 agent_tool_use: Bash tool - run tests
10:08:10 test_run: PASS (all tests passed)
10:08:20 git_operation: git add parser_test.go
10:08:25 agent_completed: Success
```

---

## 🗄️ Event Retention and Cleanup (Future Work)

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

### Design (From vc-184)

When we do implement this, here's the plan:

**Retention Policy Tiers:**
- **Regular events** (progress, file_modified, etc.): 30 days
- **Critical events** (error, watchdog_alert): 180 days
- **Per-issue limit**: 1000 events max per issue
- **Global limit**: Configurable, default 50k events

**Configuration (Proposed Environment Variables):**
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

**Monitoring Queries (For Future Use):**

Check event table size:
```sql
SELECT COUNT(*) as total_events,
       COUNT(DISTINCT issue_id) as issues_with_events,
       MIN(timestamp) as oldest_event,
       MAX(timestamp) as newest_event
FROM agent_events;
```

Events per issue distribution:
```sql
SELECT issue_id,
       COUNT(*) as event_count,
       MIN(timestamp) as first_event,
       MAX(timestamp) as last_event
FROM agent_events
GROUP BY issue_id
ORDER BY event_count DESC
LIMIT 20;
```

Event types by age:
```sql
SELECT type,
       COUNT(*) as count,
       AVG(julianday('now') - julianday(timestamp)) as avg_age_days
FROM agent_events
GROUP BY type
ORDER BY avg_age_days DESC;
```

### Related Issues

- vc-183: Agent Events Retention and Cleanup [OPEN - Low Priority]
- vc-184: Design event retention policy [CLOSED - Design complete]
- vc-193 through vc-197: Implementation tasks [OPEN - Punted]
- vc-199: Tests for event retention [OPEN - Punted]

**Remember:** Build this when you need it, not before. Let real usage drive the requirements.

---
