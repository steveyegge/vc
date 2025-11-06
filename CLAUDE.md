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

See [docs/CONFIGURATION.md](docs/CONFIGURATION.md) for all configuration options.

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

## 🛬 Landing the Plane: Clean Session Ending

**When the user says "let's land the plane"**, follow this protocol to cleanly end your session:

### 1. File Remaining Work
Create beads issues for any follow-up work discovered during the session:

```bash
# File new issues for remaining tasks
bd create "Add integration tests for loop detector" -t task -p 2 \
  -d "Add tests that simulate loop conditions and verify halt behavior"

bd create "Document loop detector in FEATURES.md" -t task -p 3 \
  -d "Add detailed section explaining ZFC approach and configuration"
```

### 2. Run Quality Gates (if code changes were made)
Ensure all quality gates pass. **File P0 issues for any failures**:

```bash
# Run tests
go test ./...

# Run linters
golangci-lint run ./...

# If anything fails, file blocking issues:
bd create "Fix failing TestLoopDetector test" -t bug -p 0 \
  -d "Test fails with: [error details]" \
  --label "quality-gate-failure"
```

### 3. Update Beads Issues
Close completed work and update status:

```bash
# Close finished issues
bd close vc-0vfg --reason "Completed all acceptance criteria"

# Update in-progress issues
bd update vc-123 --notes "Completed initial implementation, needs testing"
```

### 4. Sync Issue Tracker (CRITICAL)
**Work methodically to ensure local and remote issues merge safely.** The `.beads/issues.jsonl` file is the source of truth - conflicts here must be resolved carefully.

```bash
# Check for remote changes
git fetch
git status

# Pull remote changes (may cause conflicts)
git pull --rebase

# If .beads/issues.jsonl has conflicts:
# Option A: Accept remote and re-apply your changes
git checkout --theirs .beads/issues.jsonl
bd import .beads/issues.jsonl
# Then re-close/update your issues
bd close vc-0vfg --reason "..."

# Option B: Manual merge
# Edit .beads/issues.jsonl to resolve conflicts
# Then import the merged result
bd import .beads/issues.jsonl

# Export your final state
bd export -o .beads/issues.jsonl

# Verify database consistency
bd list | head -20
bd show vc-0vfg  # Verify your changes survived

# Commit and push
git add .beads/issues.jsonl
git commit -m "Sync issue tracker after session"
git push

# If push fails (remote changed), repeat pull/merge/push cycle
```

**Goal:** Clean reconciliation where no issues are lost. Be patient and creative - sometimes multiple iterations are needed.

### 5. Verify Clean State
Ensure all changes are committed and no untracked files remain:

```bash
# Check git status
git status

# Verify no untracked files (except .beads/vc.db which is gitignored)
# Verify no uncommitted changes

# Verify pushed to remote
git log --oneline -5
git status  # Should show "Your branch is up to date with 'origin/main'"
```

### 6. Choose Follow-Up Issue
Provide the user with a clear prompt for the next session:

```bash
# Find next ready work
bd ready --limit 5

# Show details of recommended issue
bd show vc-X
```

**Then provide the user with:**
- **Summary of completed work** (what was accomplished this session)
- **Issues filed for follow-up** (with issue IDs)
- **Quality gate status** (all passing / issues filed for failures)
- **Recommended prompt for next session** in this format:

```
Continue work on vc-X: [issue title]

Context: [1-2 sentences about what's been done and what's next]
```

### Example Complete "Land the Plane" Session

```bash
# 1. File remaining work
bd create "Add loop detector metrics to activity feed" -t task -p 2

# 2. Run quality gates
go test ./...
golangci-lint run ./...
# All passed ✓

# 3. Close finished issues
bd close vc-0vfg --reason "Completed all acceptance criteria"

# 4. Sync carefully
git pull --rebase
# No conflicts
bd export -o .beads/issues.jsonl
git add .beads/issues.jsonl
git commit -m "Close vc-0vfg: loop detector implementation"
git push
# Success ✓

# 5. Verify clean state
git status
# On branch main, nothing to commit, working tree clean ✓

# 6. Choose next work
bd ready --limit 5
bd show vc-xyz
```

**Session Summary to User:**
```
✅ Completed this session:
- Implemented activity feed loop detector (vc-0vfg)
- All quality gates passing
- All changes committed and pushed

📋 Issues filed for follow-up:
- vc-abc: Add loop detector metrics to activity feed

🎯 Recommended next session:
"Continue work on vc-xyz: Add health monitoring for executor state

Context: The loop detector is now complete. Next up is implementing
health monitoring to track executor state transitions and detect
anomalies proactively."
```

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
├── docs/               # Detailed documentation
│   ├── CONFIGURATION.md  # Environment variables and tuning
│   ├── FEATURES.md       # Feature deep dives
│   └── QUERIES.md        # SQL query reference
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

### Blocker-First Prioritization

**VC uses blocker-first prioritization to ensure missions run to completion.**

Discovered blockers are ALWAYS selected before regular ready work, regardless of priority numbers. This prevents missions from abandoning discovered work and ensures quality gates pass before moving forward.

**What this means:**
- A P3 blocker will be selected over a P0 regular task
- If missions continuously spawn blockers, regular work waits indefinitely
- This is intentional behavior for mission convergence

**Work starvation is acceptable** for ensuring mission completion. If regular work appears stuck:
- Check if blocker issues are continuously being discovered: `bd list --status open | grep discovered:blocker`
- Monitor work starvation metrics (see vc-160 for monitoring tools)
- Blockers will eventually be exhausted as the mission converges

**Priority order:**
1. Baseline-failure issues (in self-healing mode)
2. Discovered blockers (`discovered:blocker` label)
3. Regular ready work (sorted by priority)
4. Discovered related work (`discovered:related` label)

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

# Prevent executor from auto-claiming (vc-4ec0)
# Use for design tasks, research, or issues requiring human oversight
bd label add vc-X no-auto-claim
```

**Executor Exclusion Label** - Narrow Policy (vc-c913):

**ONLY use `no-auto-claim` for these 4 criteria:**
1. **External coordination** - Requires talking to other teams, approval workflows, or external dependencies
2. **Human creativity** - Product design decisions, UX choices, branding, marketing content
3. **Business judgment** - Pricing decisions, legal review, compliance, contracts
4. **Pure research** - Exploring unknowns with no clear deliverable or action plan

**Everything else is FAIR GAME for VC**, including:
- Concurrency bugs, race conditions, deadlocks
- Shutdown logic, lifecycle issues, cleanup
- Schema changes, migrations, data integrity
- Performance issues, optimization
- Critical code paths, core infrastructure
- Architectural changes, refactoring
- Complex debugging, root cause analysis

**Why this narrow policy?**
VC has robust safety nets that catch issues before they cause damage:
- **Quality gates** (test/lint/build) validate changes before merge
- **AI supervision** (assessment + analysis) guides approach and catches mistakes
- **Sandbox isolation** (git worktrees) prevents contamination of main branch
- **Self-healing** (vc-210) fixes broken baselines automatically
- **Activity feed** provides full visibility into what's happening
- **Human intervention** possible at any time via CLI

The old conservative approach slowed the path to self-hosting. Trust the safety nets and let VC tackle hard problems.

See [docs/NO_AUTO_CLAIM_POLICY.md](docs/NO_AUTO_CLAIM_POLICY.md) for detailed guidance and examples.

**Technical notes:**
- The executor's `GetReadyWork()` query automatically filters out `no-auto-claim` issues
- Humans and Claude Code sessions can still work on these issues normally
- When in doubt, err on the side of letting VC try (safety nets will catch real issues)

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
- **[docs/CONFIGURATION.md](docs/CONFIGURATION.md)** - Environment variables and configuration
- **[docs/FEATURES.md](docs/FEATURES.md)** - Deep dives on specific features
- **[docs/QUERIES.md](docs/QUERIES.md)** - SQL queries for metrics and monitoring

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
- **Use standard dependency direction** - Always `(child, parent)`, never `(parent, child)` (see [docs/FEATURES.md](docs/FEATURES.md))

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

## 📖 Additional Documentation

- **[docs/CONFIGURATION.md](docs/CONFIGURATION.md)** - All environment variables, tuning guidelines, and configuration validation
- **[docs/FEATURES.md](docs/FEATURES.md)** - Deep dives on:
  - Self-Healing Baseline Failures (vc-210)
  - Executor Graceful Shutdown
  - Agent Progress Events (vc-129)
  - Daemon Coexistence (vc-195)
  - Dependency Direction Convention
  - Conversational REPL Interface
- **[docs/QUERIES.md](docs/QUERIES.md)** - SQL queries for:
  - Quality gates progress monitoring
  - Deduplication metrics analysis
  - Agent progress tracking
  - Self-healing metrics
  - Event retention queries (future)

---
