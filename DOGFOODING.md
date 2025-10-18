# Dogfooding Workflow: VC Self-Healing Missions

**Status**: Active dogfooding in progress
**Owner**: vc-106

## Overview

VibeCoder (VC) is designed to run **autonomously for hours to days** once given an initial task graph. During dogfooding, VC works on its own codebase to find and fix issues, with human oversight only when needed.

## The Vision: Autonomous Engineer-in-a-Box

VC implements a recursive, self-expanding workflow:

1. **VC claims ready work** from the beads queue atomically
2. **VC executes** the full engineer-in-a-box cycle:
   - Design → Code → Review → Test → Review → Quality Gates → Rebase → GitOps
3. **VC expands nontrivial work** into epics with child issues as needed
4. **VC files discovered issues** (bugs, missing features, improvements)
5. **VC continues** claiming next ready work until queue is empty or blocked

**Human intervention only needed when**:
- Setting up initial task graph
- Worker gets stuck (detected by watchdog)
- Key architectural decision required (flagged by supervisor)
- Monitoring discovers unexpected behavior

## Dogfooding Process

### Phase 1: Setup (Human)

```bash
# 1. Ensure VC is built and ready
cd ~/src/vc/vc
go build ./cmd/vc

# 2. Set API key for AI supervision
export ANTHROPIC_API_KEY=your-key-here

# 3. Choose starting work OR let VC choose
# Option A: Human picks
bd ready --limit 10
bd show vc-X  # Review details

# Option B: VC picks (coming soon - see Mission Selection below)
```

### Phase 2: Execution (VC Autonomous)

```bash
# Start VC - it will run autonomously
./vc

# In VC REPL:
You: Let's continue working

# VC will now:
# - Claim ready work atomically
# - Assess the task (AI supervision)
# - Execute the work (spawn coding agent)
# - Analyze results (AI supervision)
# - File discovered issues
# - Run quality gates
# - Repeat until no ready work or blocked
```

### Phase 3: Monitoring (Human + Claude Code)

**CRITICAL**: Monitor the activity feed to observe VC in action.

```bash
# In a separate terminal, tail the activity feed
./vc tail -f

# OR view recent activity without following:
./vc activity -n 20

# Filter by specific issue:
./vc tail -f --issue vc-123

# View only errors:
./vc activity --type error -n 50
```

**What to watch for**:
- Assessment quality (is AI supervision working?)
- Agent progress (is work being completed?)
- Discovered issues (is VC finding real problems?)
- Quality gate results (are standards being enforced?)
- Convergence (is VC making progress or spinning?)

**When to intervene**:
- Worker stuck for >30min with no progress
- Quality gates repeatedly failing
- VC filing nonsensical issues
- Execution diverging from goals

### Phase 4: Triage (Human + VC)

After VC completes a mission or gets blocked:

```bash
# Review what VC discovered
bd list --status open --created-after "2 hours ago"

# Categorize issues:
# - P0: Critical bugs blocking further dogfooding → Fix manually now
# - P1: Important features/bugs → Let VC tackle next
# - P2: Nice-to-haves → Leave in backlog

# Example: Promote critical issue
bd update vc-NEW --priority P0

# Example: Add to current epic
bd dep add vc-NEW vc-106 --type parent-child
```

### Phase 5: Reset and Iterate

**No GitOps yet is a FEATURE** - allows safe experimentation:

```bash
# Option A: Keep the changes if quality gates passed
git add -A
git commit -m "VC mission: completed vc-X"

# Option B: Discard changes if VC got confused
git reset --hard HEAD
git clean -fd

# Fix high-priority issues manually (or in Claude Code)
# Then run next mission with updated VC
```

## Mission Selection

### Current: Human-Guided Selection

Start simple and progressively increase complexity:

**Phase 1: Simple bugs** (single-file fixes)
- vc-31: Fix activity feed timestamp display
- vc-32: Handle missing issue fields gracefully

**Phase 2: Feature additions** (new functionality, tests required)
- Issues with clear requirements and acceptance criteria
- Likely to spawn child issues (good for testing recursive expansion)

**Phase 3: Complex refactoring** (multi-file, architectural changes)
- Only after VC proves stable on simpler tasks
- High supervision, frequent checkpoints

### Future: VC Self-Selection (Not Yet Implemented)

VC could autonomously choose next mission based on:
- Ready work ordered by priority
- Complexity estimation (simple first, hard later)
- Success rate history (avoid repeatedly failing tasks)
- Dependency chains (complete epics systematically)

**When to enable**: After 10+ successful human-guided missions prove stability.

## Activity Feed Monitoring

**STATUS: ✅ WORKING** - Commands available and functional

### Real-Time Monitoring

The activity feed streams real-time events showing executor actions:

```bash
# Follow live updates (recommended for monitoring)
./vc tail -f

# Example output:
ℹ️ [10:23:15] vc-31 issue_claimed: Issue vc-31 claimed by executor abc123
ℹ️ [10:23:18] vc-31 assessment_started: Starting AI assessment for issue vc-31
ℹ️ [10:23:45] vc-31 assessment_completed: AI assessment completed
    strategy: Fix timestamp format in display layer
    confidence: 0.9
ℹ️ [10:24:12] vc-31 analysis_completed: AI analysis completed
    issues_discovered: 0
⚠️ [10:24:15] vc-31 quality_gates_failed: Quality gates failed
```

### Filtering and Analysis

```bash
# Show last N events
./vc activity -n 50

# Filter by issue
./vc activity --issue vc-123 -n 20
./vc tail -f --issue vc-123

# Filter by type
./vc activity --type error
./vc activity --type git_operation -n 10

# Filter by severity
./vc activity --severity warning
```

### What's Logged

Events include:
- Issue claims and completions
- AI assessments and analysis
- Agent spawns and executions
- File modifications and git operations
- Test runs and build output
- Context usage monitoring
- Errors and warnings
- Watchdog alerts and interventions

## Success Metrics

Track these to measure dogfooding progress:

### Quantitative
- **Missions completed**: Total issues VC closed
- **Issues filed**: Bugs/features VC discovered
- **Quality gate pass rate**: % of missions passing gates
- **Time to completion**: Average mission duration
- **Intervention rate**: How often human had to intervene

### Qualitative
- **Assessment quality**: Is AI supervision insightful?
- **Issue quality**: Are discovered issues real and actionable?
- **Code quality**: Does VC's code meet standards?
- **Convergence**: Does VC finish work or spin endlessly?

### Current Progress

| Metric | Value | Notes |
|--------|-------|-------|
| Total runs | 7 | Run #7 completed 2025-10-18 |
| Successful runs | 7 | All runs discovered issues or demonstrated bugs |
| Quality gate pass | 6/7 | Run #7 REPL hung, but discovered 2 bugs |
| Issues discovered | 2+ | vc-125 (REPL hang), vc-126 (heartbeat) |
| Issues fixed | 2 | Both vc-125 and vc-126 closed |
| Activity feed | ✅ | Working! Use `vc tail -f` for live monitoring |
| GitOps enabled | ❌ | Intentionally disabled for safety |
| Auto-mission select | ❌ | Human-guided for now |
| Human intervention | ~40% | 3/7 runs needed manual cleanup |

## Safety and Rollback

### Why No GitOps Yet

**Intentional safety measure** during bootstrap:
- VC may have bugs that break the codebase
- Easy rollback via `git reset --hard`
- No risk of auto-committing broken code
- Humans review all changes before merge

### When to Enable GitOps

Only after:
1. 20+ successful missions with 90%+ quality gate pass rate
2. Activity feed monitoring proven reliable
3. Watchdog detecting convergence issues
4. Quality gates enforcing all standards
5. Recursive expansion working correctly

## Common Patterns

### Issue Status Convention: `in_progress` is ONLY for VC Workers

**CRITICAL RULE**: The `in_progress` status is **exclusively** for active VC worker/agent execution. Claude Code sessions and humans should **NEVER** use `in_progress`.

**Why this matters**:
- Makes orphan detection trivial: any `in_progress` with stale heartbeat = orphaned worker
- Clear separation: `in_progress` = automated VC work, `open` = everything else
- Prevents accidental orphaning when Claude Code sessions end

**For human/Claude Code work**:
```bash
# Leave as 'open' and update notes to track progress
bd update vc-X --notes "Working on this in Claude Code"
bd update vc-X --notes "Progress: fixed bug, testing now"
```

**For VC autonomous work**:
```bash
# VC automatically sets in_progress when claiming work
# open → in_progress (VC claims) → closed (VC completes)
# or back to open if orphaned/failed
```

### VC Discovers Missing Feature

```
You: Let's continue working
VC: [Claims vc-31]
VC: [During execution] Filed vc-107: Need better error messages in executor
You: [Later] Good catch - let's make that P1
```

### VC Gets Stuck

```
[Activity feed shows no progress for 30min]
You: [In VC REPL] What are you working on?
VC: I'm stuck on vc-45 - missing test fixtures
You: File an issue for the missing fixtures and move on
VC: [Files vc-108, continues with next ready work]
```

### VC Completes Epic

```
VC: Completed vc-50 and all 5 child issues
VC: Quality gates: PASS
VC: Ready to merge
You: [Reviews changes] Looks good!
You: git add -A && git commit -m "VC epic: user authentication"
```

## Next Steps

1. **Fix activity feed monitoring** (critical for observability)
2. **Document current quality gates** (what's actually enforced?)
3. **Track first 10 missions** in a log or separate epic
4. **Measure success metrics** systematically
5. **Consider VC self-selection** once stable
6. **Enable GitOps** when safety proven

## Questions and Refinements

- Should VC prefer breadth-first (many issues) or depth-first (finish epics)?
- What timeout triggers human intervention? (30min? 1hr?)
- Should VC auto-prioritize discovered issues?
- How to handle quality gate failures? (retry? file issue? skip?)

---

## Mission Log

Detailed record of all dogfooding runs:

### Run #7 - 2025-10-18
**Target**: vc-122 (CleanupStaleInstances bug)
**Result**: Partial success - bug demonstrated, 2 new bugs discovered, REPL hung
**Issues discovered**: vc-125 (REPL hang), vc-126 (heartbeat NULL)
**Issues fixed**: vc-125, vc-126, vc-122 (all closed)
**Human intervention**: Yes (manual cleanup of stale claim, killed hung REPL)
**Key learning**: The bug being tested (vc-122) manifested perfectly - self-demonstrating issue! State transition validation needed fixing, heartbeat mechanism not working properly.

### Run #1-6
**Status**: Completed prior to detailed logging
**Result**: 6 successful runs establishing baseline workflow
**Note**: Metrics tracked in aggregate, detailed logs not captured

---

**Remember**: The goal is for VC to run **autonomously** while we **observe and learn**. Human intervention should be the exception, not the rule. The more VC runs unsupervised, the more we prove the architecture works.
