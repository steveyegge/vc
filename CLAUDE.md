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

The migration framework is in `internal/storage/migrations/`. It provides:

- **Version tracking**: `schema_version` table tracks applied migrations
- **Up/down migrations**: Forward and rollback support
- **Ordered execution**: Migrations applied in version order
- **Transaction safety**: Each migration runs in a transaction

Example migration:

```go
import "github.com/steveyegge/vc/internal/storage/migrations"

manager := migrations.NewManager()
manager.Register(migrations.Migration{
    Version:     1,
    Description: "Add new feature table",
    Up:          "CREATE TABLE ...",
    Down:        "DROP TABLE ...",
})

// Apply migrations
err := manager.ApplySQLite(db)
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

### Default Configuration

The default settings are conservative and work well for most cases:

- **Confidence threshold**: 0.85 (85%) - High confidence required to mark as duplicate
- **Lookback window**: 7 days - Only compare against issues from the past week
- **Max candidates**: 50 - Compare against up to 50 recent issues
- **Batch size**: 10 - Process 10 comparisons per AI call
- **Within-batch dedup**: Enabled - Deduplicate within the same batch of discovered issues
- **Fail-open**: Enabled - File the issue if deduplication fails (prefer duplicates over lost work)
- **Include closed issues**: Disabled - Only compare against open issues
- **Min title length**: 10 characters - Skip dedup for very short titles
- **Max retries**: 2 - Retry AI calls twice on failure
- **Request timeout**: 30 seconds - Timeout for AI API calls

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
