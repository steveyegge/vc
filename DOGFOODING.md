# VC Dogfooding Workflow

**Goal**: Systematically dogfood VC to make it fix itself, proving the architecture works and reaching the point where we prefer VC over manual/Claude Code for all future development.

## Overview

This document tracks VC's journey toward self-hosting through systematic dogfooding missions. VC autonomously works on its own codebase for hours-to-days with minimal human intervention.

## Current Status

**Phase**: Bootstrap - Building AI-supervised workflow in Go  
**Epic**: vc-5 (Beads Integration and Executor Tables)  
**Updated**: 2025-10-23

### Success Metrics

- **Total missions**: 19 (updated 2025-10-23)
- **Successful missions**: 11 (runs #17-19 fixed 4 critical bugs)
- **Quality gate pass rate**: 10/11 (90.9%)
- **Activity feed**: ✅ Working reliably
- **GitOps**: ❌ Intentionally disabled for safety
- **Auto-mission selection**: ❌ Human-guided for now
- **Human intervention rate**: ~35% (target: <10%)
- **Longest autonomous run**: ~3 hours (run #19)

### Milestones

✅ **Runs #1-16**: Foundation building, basic executor functionality  
✅ **Run #17**: Discovered 4 critical bugs in executor lifecycle  
✅ **Run #18**: Fixed vc-101 (P0 state transition errors)  
✅ **Run #19**: Fixed vc-102, vc-100, vc-103 - **EXECUTOR NOW RUNS CLEANLY!** 🎉

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

### Run #19 - 2025-10-23 (ALL P1 BUGS FIXED!)

**Target**: Fix remaining P1/P2 executor bugs from run #17  
**Duration**: ~1 hour  
**Issues fixed**: vc-102, vc-100, vc-103  
**Result**: ✅ **COMPLETE SUCCESS** - Executor runs cleanly with no errors!

**Bugs fixed**:
1. **vc-102** [P1]: Unique constraint on executor stop
   - Added `MarkInstanceStopped()` method (UPDATE instead of INSERT)
   - Updated storage interface + all implementations + mocks

2. **vc-100** [P1]: FK constraint on cleanup events
   - Convert empty issue_id to NULL for system events
   - FK constraints allow NULL (bypasses the check)

3. **vc-103** [P2]: Shutdown error logging
   - Auto-fixed by vc-101 context cancellation handling
   - Clear messages instead of confusing errors

**Before**: UNIQUE constraint errors, FK violations, confusing shutdown errors  
**After**: Clean startup, clean shutdown, NO ERRORS! 🎉

### Run #18 - 2025-10-22

**Target**: Fix vc-101 (P0 state transition errors)  
**Result**: ✅ SUCCESS - State machine now handles context cancellation properly

### Run #17 - 2025-10-21

**Target**: First full execution cycle test  
**Result**: ⚠️ DISCOVERED 4 CRITICAL BUGS - Filed vc-100, vc-101, vc-102, vc-103

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

- ✅ Workflow documented (this file exists)
- ✅ Process for mission selection defined
- ✅ Activity feed monitoring working reliably
- ✅ Process for issue triage defined
- ✅ Sandbox cleanup process defined
- ⏳ Success metrics tracked systematically (in progress)
- ⏳ 20+ successful missions with 90%+ quality gate pass rate (11/20, 90.9%)
- ⏳ Proven convergence (VC finishes work, doesn't spin)
- ⏳ GitOps enabled after stability proven
- ⏳ Human intervention < 10% of missions (currently ~35%)
- ⏳ VC autonomously runs for 24+ hours on complex epic

**Next milestone**: Run 9 more successful missions to reach 20+ threshold, then enable GitOps and test full autonomous mode.

## Next Steps

1. **Validate fixes**: Run executor with actual work (not just startup/shutdown)
2. **Test mission cycle**: Complete vc-44 (Beads migration dogfooding validation)
3. **Enable AI supervision**: Test assess → analyze → execute flow
4. **Reduce intervention**: Aim for <10% human intervention rate
5. **Longer runs**: Test 24+ hour autonomous operation on complex epic

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
