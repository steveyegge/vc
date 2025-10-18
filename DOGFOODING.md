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

**CRITICAL**: We need to monitor the activity feed to observe VC in action.

```bash
# In a separate terminal, tail the activity feed
# TODO: This is currently BROKEN - needs fixing!
# Expected command (once working):
./vc feed --follow

# OR use bd directly if feed endpoint exists:
bd activity --follow
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

**STATUS: BROKEN** - Needs investigation and fix

### Expected Behavior

The activity feed should stream real-time events:

```
[2025-10-18 10:23:15] EXECUTOR: Claimed vc-31
[2025-10-18 10:23:18] AI_ASSESS: Strategy: Fix timestamp format in display layer
[2025-10-18 10:23:45] AGENT: Modified internal/repl/feed.go
[2025-10-18 10:24:12] AI_ANALYZE: Complete - no issues discovered
[2025-10-18 10:24:15] QUALITY: All gates passed
```

### Current Issues

- `./vc feed --follow` may not exist yet
- Activity events may not be persisted correctly
- WebSocket/SSE streaming not implemented
- Alternative: polling `bd activity` in a loop (ugly but works)

### Workaround (Until Fixed)

```bash
# Poll activity in a loop
watch -n 2 'bd activity --limit 20'

# Or manual refresh
while true; do clear; bd activity --limit 20; sleep 5; done
```

### What Needs Fixing

- [ ] Implement `./vc feed --follow` command
- [ ] Verify activity events are written to storage
- [ ] Add streaming support (SSE or WebSocket)
- [ ] Add filtering (by issue, by event type, by severity)
- [ ] File issue for this if not already tracked

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
| Successful runs | 6+ | Per vc-106 notes |
| Activity feed | ❌ | Still broken after multiple attempts |
| GitOps enabled | ❌ | Intentionally disabled for safety |
| Auto-mission select | ❌ | Human-guided for now |

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

**Remember**: The goal is for VC to run **autonomously** while we **observe and learn**. Human intervention should be the exception, not the rule. The more VC runs unsupervised, the more we prove the architecture works.
