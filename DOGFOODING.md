# VC Dogfooding Workflow

**Goal**: Systematically dogfood VC to make it fix itself, proving the architecture works and reaching the point where we prefer VC over manual/Claude Code for all future development.

## Current Status

**Phase**: Bootstrap - Building AI-supervised workflow in Go
**Updated**: 2025-10-24

### Success Metrics

- **Total missions**: 24
- **Successful missions**: 13
- **Recent bugs found**: vc-148 (CLI help text), vc-109 (executor startup cleanup)
- **Quality gate pass rate**: 90.9% (10/11)
- **Activity feed**: ✅ Working reliably
- **Executor**: ✅ FIXED - Startup cleanup working
- **CLI Infrastructure**: ✅ VALIDATED - All commands working
- **GitOps**: ❌ Intentionally disabled for safety
- **Human intervention rate**: ~35% (target: <10%)
- **Longest autonomous run**: ~3 hours

### Recent Highlights

✅ **Run #24** (2025-10-24): CLI testing phase - validated infrastructure, found vc-148
✅ **Run #23** (2025-10-23): Found and fixed vc-109 - executor startup cleanup bug
✅ **Run #22** (2025-10-23): Investigation practice (false alarm, but valuable learning)
✅ **Run #19** (2025-10-23): Fixed vc-102, vc-100, vc-103 - executor runs cleanly!
✅ **Run #18**: Fixed vc-101 (P0 state transition errors)
✅ **Run #17**: Discovered 4 critical bugs in executor lifecycle

## The Dogfooding Process

### 1. Mission Selection

```bash
# Find ready work
bd ready

# Review issue details
bd show vc-X

# Claim the work
bd update vc-X --status in_progress
```

**Selection criteria** (priority order):
1. P0 bugs blocking execution
2. P1 bugs affecting reliability
3. Core infrastructure features
4. Nice-to-have features and polish

### 2. Execution Modes

#### Manual Mode (Current)
Human fixes issues discovered by VC, validates fixes, commits changes.

**When to use**:
- P0/P1 bugs that block VC from running
- Architectural decisions
- Complex refactoring requiring deep system knowledge

#### Semi-Autonomous Mode (Next Phase)
VC attempts fix with human oversight. Human reviews, approves/rejects.

**When to use**:
- P2/P3 bugs
- Well-defined feature additions
- Testing VC's capabilities

#### Autonomous Mode (Goal)
VC handles everything: claim work → analyze → implement → test → commit → repeat.

**Requirements before enabling**:
- 20+ successful missions
- 90%+ quality gate pass rate
- Proven convergence (no infinite loops)
- Reliable rollback mechanisms

### 3. Activity Feed Monitoring

```bash
# Real-time monitoring
vc tail -f

# Review recent activity
vc activity

# Check specific instance
vc activity --instance <id>
```

**What to watch for**:
- State transitions (claimed → assessing → analyzing → executing → cleanup)
- Error messages and stack traces
- Resource usage (CPU, memory, sandbox count)
- Quality gate results
- Time spent in each phase

### 4. Issue Triage

When VC discovers issues or execution reveals bugs:

1. **File immediately**: `bd create --title "..." --type bug --priority PX`
2. **Add context**: Link to activity feed, logs, error messages
3. **Triage priority**:
   - **P0**: Blocks execution completely
   - **P1**: Causes errors but execution continues
   - **P2**: Affects UX/performance but not correctness
   - **P3**: Nice-to-have improvements

4. **Track dependencies**: `bd dep <from-id> <to-id> --type blocks`

#### Investigation Best Practices

**Before filing a bug**:
- Check git history for recent related fixes (`git log --grep="keyword"`)
- Verify the issue is current, not residual data from old bugs
- Test assumptions manually (e.g., `sqlite3` INSERT/UPDATE for schema issues)
- Search for similar/duplicate issues (`bd list --status open | grep keyword`)

**False alarms are okay**! Better to investigate thoroughly than ignore potential issues. Close as invalid with detailed explanation of what you learned.

**Lessons from Run #22** (vc-108 false alarm):
- Stale data from before a fix can look like a new bug
- Always check git history when investigating schema/infrastructure issues
- Manual testing (sqlite3, curl, etc.) can quickly validate assumptions
- Document false alarms - they're valuable learning for future investigations

### 5. Sandbox Cleanup

After each mission (success or failure):

```bash
# Automatic cleanup (when executor stops gracefully)
# Manual cleanup (if executor crashes)
vc cleanup --force

# Verify cleanup
vc instances  # Should show no active instances
```

**Cleanup checklist**:
- [ ] Sandbox directories removed
- [ ] Database entries marked stopped
- [ ] No orphaned processes
- [ ] Claimed issues released

### 6. Quality Gates

Before merging changes from any mission:

- [ ] **Tests pass**: All existing tests green
- [ ] **Linting clean**: `golangci-lint run` passes
- [ ] **Type checking**: `go build ./...` succeeds
- [ ] **No regressions**: Manual smoke test of core flows
- [ ] **Metrics updated**: Document mission results

## Recent Missions

Detailed run logs archived in `docs/` and `docs/archive/`. Summary of key runs:

**Run #24** (2025-10-24): CLI Testing Phase - Systematic testing of all VC CLI commands (stats, activity, tail, cleanup, repl, health). Validated database integration (Beads library usage, VC extension tables). Found vc-148 (misleading --db help text). All tested infrastructure working correctly. Full report: `docs/DOGFOOD_RUN24.md`.

**Run #23** (2025-10-23): Found and fixed vc-109 - executor startup cleanup bug. Silent failure where executor polled but never claimed work due to orphaned claims from previous sessions.

**Run #22** (2025-10-23): False alarm investigation - vc-108 closed as invalid. Valuable lesson: check git history before filing schema bugs, stale data can look like new bugs.

**Run #19** (2025-10-23): Fixed vc-102, vc-100, vc-103 - executor now runs cleanly with no errors!

**Runs #17-18**: Discovered and fixed 4 critical executor lifecycle bugs.

**Runs #9-10** (archived): Test gate timeout fixed (vc-130), AI supervision validated (catches agent blind spots).

## Safety & Rollback

### GitOps (Currently Disabled)

**Why disabled**: Need to prove stability before automated commits.

**Requirements to enable**:
- 20+ successful missions
- 90%+ quality gate pass rate
- No infinite loops observed
- Reliable convergence

**How it will work**:
1. VC commits changes to feature branch
2. CI runs tests/linting
3. If green, PR created automatically
4. Human reviews and merges

### Rollback Process

```bash
# Immediate rollback (nuclear option)
git reset --hard HEAD

# Selective rollback (preferred)
git diff HEAD  # Review changes
git restore <files>  # Restore specific files
git restore .  # Restore all working tree changes

# Clean up database state
bd update vc-X --status open  # Release claimed work
```

## Troubleshooting

### VC Won't Start

```bash
# Check database
sqlite3 .beads/vc.db "SELECT * FROM vc_executor_instances ORDER BY started_at DESC LIMIT 5;"

# Force cleanup
vc cleanup --force

# Check for orphaned processes
ps aux | grep vc
```

### VC Stuck in Loop

1. **Observe**: Use `vc tail -f` to watch activity
2. **Timeout**: If >30min in same phase, intervene
3. **Stop**: `kill <pid>` (graceful) or `kill -9 <pid>` (force)
4. **Cleanup**: `vc cleanup --force`
5. **File bug**: Document observed behavior

### Quality Gates Failing

1. **Don't panic**: This is expected during dogfooding
2. **Review changes**: `git diff`
3. **Run tests locally**: `go test ./...`
4. **Fix or rollback**: Fix if obvious, rollback if complex
5. **File bug**: Document gate failures for future improvement

## Success Criteria (from vc-26)

- ✅ Workflow documented
- ✅ Process for mission selection defined
- ✅ Activity feed monitoring working
- ✅ Issue triage process defined
- ✅ Sandbox cleanup process defined
- ⏳ Success metrics tracked (in progress)
- ⏳ 20+ successful missions with 90%+ pass rate (12/23, goal: 20+)
- ⏳ Proven convergence (VC finishes work, doesn't spin)
- ⏳ GitOps enabled after stability proven
- ⏳ Human intervention < 10% (currently ~35%)
- ⏳ VC autonomously runs 24+ hours on complex epic

## Next Steps

1. Continue dogfooding runs to reach 20+ successful missions
2. Reduce human intervention rate from 35% to <10%
3. Test longer autonomous runs (24+ hours)
4. Enable GitOps after stability proven

## Resources

- **Issue tracker**: `.beads/vc.db` (local cache)
- **Source of truth**: `.beads/issues.jsonl` (commit this!)
- **Beads CLI**: `~/src/beads/bd`
- **Activity feed**: `vc tail -f` or `vc activity`
- **Executor logs**: Stdout/stderr from VC process

## Philosophy

> "The best way to prove an AI-supervised development system works is to make it build itself."

We dogfood aggressively because:
1. **Validates architecture**: If VC can't fix itself, it can't fix anything
2. **Discovers bugs early**: Using VC reveals issues faster than manual testing
3. **Builds confidence**: Success on our own codebase proves real-world viability
4. **Closes feedback loop**: Users (us) directly improve the product

The goal is **self-hosting**: VC autonomously handles all future development with minimal human intervention, just like a senior engineer who occasionally needs direction but otherwise works independently.
