# Instructions for AI Agents Working on VC

## 🎯 Starting a Session: "What's Next?"

**VC uses Beads for issue tracking.** All work is tracked in the `.beads/vc.db` SQLite database.

When starting a new session:

```bash
# 1. Check for ready work (no blockers)
/workspace/beads/bd ready

# 2. View issue details
/workspace/beads/bd show vc-X

# 3. Start working on it
/workspace/beads/bd update vc-X --status in_progress
```

**Important**: Use the `bd` command from `/workspace/beads/bd` - the VC binary doesn't exist yet (that's what we're building!).

---

## 📋 Current Focus

VC is in **bootstrap phase**. We're building the AI-supervised issue workflow from scratch in Go.

**Check ready work**:
```bash
/workspace/beads/bd ready --limit 5
```

**View dependency chain**:
```bash
/workspace/beads/bd list
/workspace/beads/bd dep tree vc-5
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
/workspace/beads/bd ready

# All open issues
/workspace/beads/bd list --status open

# Show specific issue with dependencies
/workspace/beads/bd show vc-X
```

### Claiming Work

```bash
# Mark as in progress
/workspace/beads/bd update vc-X --status in_progress --actor "your-name"

# Add notes as you work
/workspace/beads/bd update vc-X --notes "Working on executor loop..."
```

### Creating Issues

```bash
# Create child issue
/workspace/beads/bd create \
  "Issue title" \
  -t task \
  -p 2 \
  -d "Description" \
  --design "Design notes" \
  --acceptance "Success criteria"

# Add dependency (if needed)
/workspace/beads/bd dep add vc-NEW vc-PARENT --type blocks
```

### Completing Work

```bash
# Before closing, ensure:
# - All acceptance criteria met
# - Tests passing (when test infrastructure exists)
# - Code documented

# Close issue
/workspace/beads/bd close vc-X --reason "Completed all acceptance criteria"
```

### Export to Git

**Always export before committing**:
```bash
/workspace/beads/bd export -o .beads/issues.jsonl
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
- **/workspace/vibecoder/** - The 350k LOC TypeScript prototype (reference)

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

## ⚠️ Important Notes

- **Don't use markdown TODOs** - Everything goes in beads
- **Don't create one-off scripts** - Use `bd` commands
- **Always export before committing** - Keep JSONL in sync
- **Beads path is `.beads/vc.db`** - Not `.vc/vc.db` (README is outdated)
- **Bootstrap first** - Don't jump ahead to advanced features

---

## 🆘 Common Commands Reference

```bash
# === Finding work ===
/workspace/beads/bd ready                    # Show ready work
/workspace/beads/bd list --status open       # All open issues
/workspace/beads/bd show vc-X                # Issue details

# === Managing issues ===
/workspace/beads/bd update vc-X --status in_progress
/workspace/beads/bd update vc-X --notes "Progress update"
/workspace/beads/bd close vc-X --reason "Done"

# === Dependencies ===
/workspace/beads/bd dep tree vc-X            # Show dependency tree
/workspace/beads/bd dep add vc-A vc-B        # A depends on B

# === Export ===
/workspace/beads/bd export -o .beads/issues.jsonl
```

---

## 🎓 First Session Checklist

1. Read this file (CLAUDE.md)
2. Read README.md for vision
3. Run `/workspace/beads/bd ready` to see what's ready
4. Run `/workspace/beads/bd show vc-5` to see first epic
5. Start working on vc-5 or break it down into child issues
6. Export to JSONL before committing

---

**Remember**: The issue tracker is the source of truth. When in doubt, check `bd ready` to see what needs doing!
