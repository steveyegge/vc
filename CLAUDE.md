# Instructions for AI Agents Working on VC

## 🎯 Starting a Session: "What's Next?"

**VC uses Beads for issue tracking.** All work is tracked in the `.beads/vc.db` SQLite database.

### Environment Setup

- **GCE VM (claude-code-dev-vm)**: Beads is at `/workspace/beads/bd`
- **Local development**: Beads is typically at `~/src/beads/bd`

Ensure `bd` is in your PATH or use the full path to your beads installation.

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

# 3. Start working on it
bd update vc-X --status in_progress
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
│   ├── vc.db           # Issue tracker database (source of truth)
│   └── issues.jsonl    # JSONL export (commit this to git)
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

```bash
# Mark as in progress
bd update vc-X --status in_progress --actor "your-name"

# Add notes as you work
bd update vc-X --notes "Working on executor loop..."
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

**Always export before committing**:
```bash
bd export -o .beads/issues.jsonl
git add .beads/issues.jsonl
```

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
4. Spawns coding agent (Cody/Claude Code)
5. AI analyzes the result
6. Creates follow-on issues for discovered work
7. Runs quality gates
8. Repeats until all work complete

**Then**: Code is ready for human review and merge.

---

## 💾 Database Bootstrap and Migrations

### Automatic Initialization

**The database schema is created automatically** when you first connect to a new database. You don't need to run initialization scripts manually - just start using the storage layer and it will set everything up.

Both SQLite and PostgreSQL backends automatically:
1. Create all tables (issues, dependencies, labels, events, executor_instances, issue_execution_state)
2. Create indexes for performance
3. Create views for ready work and blocked issues
4. Set up foreign key constraints

### Manual Initialization (Optional)

If you want to pre-initialize a database, use the provided script:

```bash
# Initialize SQLite database (default: .beads/vc.db)
./scripts/init-db.sh sqlite

# Initialize SQLite at custom location
VC_DB_PATH=/path/to/db.sqlite ./scripts/init-db.sh sqlite

# Initialize PostgreSQL database
VC_PG_HOST=localhost VC_PG_DATABASE=vc ./scripts/init-db.sh postgres
```

See `./scripts/init-db.sh --help` for all options.

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
// or
err := manager.ApplyPostgreSQL(ctx, pool)
```

### Backend Configuration

Choose between SQLite and PostgreSQL via the storage configuration:

```go
import "github.com/steveyegge/vc/internal/storage"

// SQLite (default)
cfg := storage.DefaultConfig()
cfg.Backend = "sqlite"
cfg.Path = ".beads/vc.db"
store, err := storage.NewStorage(ctx, cfg)

// PostgreSQL
cfg := storage.DefaultConfig()
cfg.Backend = "postgres"
cfg.Host = "localhost"
cfg.Port = 5432
cfg.Database = "vc"
cfg.User = "vc"
cfg.Password = "secret"
store, err := storage.NewStorage(ctx, cfg)
```

---

## ⚠️ Important Notes

- **Don't use markdown TODOs** - Everything goes in beads
- **Don't create one-off scripts** - Use `bd` commands
- **Always export before committing** - Keep JSONL in sync
- **Beads path is `.beads/vc.db`** - Not `.vc/vc.db` (README is outdated)
- **Bootstrap first** - Don't jump ahead to advanced features
- **Set `ANTHROPIC_API_KEY`** - Required for AI supervision features (assessment, analysis, discovered issues)

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

# === Export ===
bd export -o .beads/issues.jsonl
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

**Remember**: The issue tracker is the source of truth. When in doubt, check `bd ready` to see what needs doing!
