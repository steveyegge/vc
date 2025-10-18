# Dogfooding Workflow: VC Self-Healing Missions

**Status**: Active dogfooding in progress
**Owner**: vc-106
**Detailed run logs**: See `docs/dogfooding-run-*.md`

## Quick Start

```bash
# 1. Build and set API key
go build ./cmd/vc
export ANTHROPIC_API_KEY=your-key-here

# 2. Start executor (autonomous mode)
./vc execute --enable-sandboxes

# 3. Monitor in separate terminal
./vc tail -f
# OR: ./vc activity -n 20
```

## The Vision

VC autonomously works on its own codebase:
1. Claims ready work atomically
2. Executes full cycle: Design → Code → Test → Quality Gates → GitOps
3. Files discovered issues
4. Continues until queue empty or blocked

**Human intervenes only when**: Stuck >30min, gates fail repeatedly, or architectural decisions needed.

---

## Current Status (Updated 2025-10-18)

| Metric | Value | Notes |
|--------|-------|-------|
| **Total runs** | 10 | See docs/dogfooding-run-*.md for details |
| **Successful runs** | 10 | All demonstrated architecture or discovered issues |
| **Quality gate pass** | 60% (6/10) | Run #10 correctly blocked incomplete work |
| **Issues discovered** | 10+ | vc-125 through vc-136 |
| **Issues fixed** | 4 | vc-125, vc-126, vc-127, vc-130 closed |
| **Activity feed** | ✅ Working | `vc tail -f` for live monitoring |
| **Auto-mission select** | ✅ Working | Executor picks highest priority |
| **AI supervision** | ✅ Excellent | Catches agent errors (validated run #10) |
| **GitOps** | ❌ Disabled | Intentional safety during bootstrap |
| **Human intervention** | ~30% | 3/10 runs needed manual cleanup |

**Key achievements**:
- Test gate timeout fixed (vc-130) - 1min vs 5min
- AI supervision catches agent blind spots (run #10)
- Executor auto-selects work by priority
- Quality gates enforce standards

**Current blockers**:
- vc-131 (P1): Agent event storage CHECK constraint
- vc-132 (P1): UNIQUE constraint on discovered issues

---

## Essential Commands

### Starting a Run
```bash
./vc execute --enable-sandboxes --poll-interval 2
```

### Monitoring
```bash
# Live feed (recommended)
./vc tail -f

# Recent activity
./vc activity -n 20

# Filter by issue
./vc activity --issue vc-123

# Filter by type/severity
./vc activity --type error
```

### Finding Work
```bash
bd ready              # Show ready issues
bd show vc-X          # Issue details
bd list --status open # All open issues
```

### Cleanup
```bash
# See docs/cleanup-artifacts.md for full script
git worktree prune
rm -rf .sandboxes/mission-*
bd export -o .beads/issues.jsonl
```

---

## Success Metrics

### Quantitative
- Missions completed: Total issues VC closed
- Quality gate pass rate: % passing all gates
- Issues discovered: Bugs/features VC found
- Human intervention rate: % of runs needing help

### Qualitative
- Assessment quality: Is AI supervision insightful?
- Issue quality: Are discovered issues actionable?
- Convergence: Does VC finish work or spin?

### Targets (for enabling GitOps)
- [ ] 20+ successful missions
- [ ] 90%+ quality gate pass rate
- [ ] <10% human intervention rate
- [ ] 24+ hour autonomous run on complex epic

---

## Recent Runs Summary

### Run #10 (2025-10-18 PM) - AI Supervision Validation ✅
- **Target**: vc-117 (P0, auto-selected)
- **Result**: 0 completions, gates correctly blocked
- **Duration**: 6m34s
- **Key finding**: AI analysis caught agent claiming completion when acceptance criteria not met
- **Validation**: Test gate timeout fixed, AI supervision working excellently
- **Details**: `docs/dogfooding-run-10.md`

### Run #9 (2025-10-18 AM) - Test Gate Timeout Discovery
- **Target**: vc-117, vc-128
- **Result**: 0 completions, blocked by test gate timeout
- **Issues filed**: vc-130 (test gate), vc-131, vc-132
- **Details**: `docs/dogfooding-run-9.md`

### Run #8 (2025-10-18) - Meta-Epic Work
- **Target**: vc-106 (documentation)
- **Result**: Documentation updated
- **Learning**: P0 epics shouldn't be auto-claimable

### Run #7 (2025-10-18) - Self-Demonstrating Bug
- **Target**: vc-122
- **Result**: Bug manifested, 2 new issues discovered
- **Issues**: vc-125, vc-126

### Runs #1-6
- 6 baseline runs establishing workflow
- Metrics tracked in aggregate

**Full history**: See Mission Log in `docs/dogfooding-archive.md`

---

## Key Learnings

1. **AI supervision > agent self-reporting** (Run #10)
   - Agent claimed "completed" but didn't meet acceptance criteria
   - AI analysis correctly identified work incomplete
   - Validates architecture's emphasis on supervision

2. **Quality gates prevent bad merges** (Run #10)
   - Blocked incomplete work correctly
   - Test/lint gates caught issues

3. **Watchdog detects anomalies** (Multiple runs)
   - Stuck state detection working
   - Threshold (0.75 confidence) may need tuning

4. **Executor is resilient** (Run #10)
   - Continues after blocking issues
   - Auto-selects by priority correctly

5. **Artifact cleanup needed** (After 10 runs)
   - Sandboxes, branches, instances accumulate
   - Automation filed: vc-133, vc-134, vc-135, vc-136

---

## Common Patterns & Workflows

### Issue Status Convention
- **`in_progress`**: ONLY for active VC executor work
- **`open`**: Everything else (Claude Code, human work, ready work)
- **Why**: Makes orphan detection trivial (stale in_progress = orphaned)

### When VC Discovers Issues
```bash
# VC files issues during analysis
# Human triages and prioritizes
bd update vc-NEW --priority 1  # Promote if critical
```

### When VC Gets Stuck
```bash
# Watchdog detects stuck state
# Manual intervention:
bd update vc-X --status blocked --notes "Reason"
# Executor continues with next ready work
```

### After Successful Run
```bash
# Review changes, merge if good
git log
git diff HEAD~1
git push
```

---

## Safety & Rollback

### Why No GitOps Yet
- VC may have bugs that break codebase
- Easy rollback via `git reset --hard`
- Human review all changes before merge
- **Will enable after**: 20+ missions, 90%+ gate pass, reliable watchdog

### Rollback Process
```bash
# If VC made bad changes:
git reset --hard HEAD
git clean -fd

# Fix issue manually, then continue
```

---

## Next Steps

### Immediate (before run #11)
1. Fix vc-131 (P1) - Quality gate event storage
2. Fix vc-132 (P1) - Discovered issue creation
3. Target vc-131 or vc-132 for run #11

### Short-term
1. Improve acceptance criteria enforcement
2. Lower watchdog threshold (0.75 → 0.70?)
3. Track completion accuracy metrics

### Long-term
1. Enable GitOps after stability proven
2. Run 24+ hour autonomous sessions
3. Self-hosting: VC handles all development

---

## Reference Documentation

- **Detailed run logs**: `docs/dogfooding-run-*.md`
- **Cleanup guide**: `docs/cleanup-artifacts.md`
- **Architecture**: `README.md`
- **Bootstrap roadmap**: `BOOTSTRAP.md` (now in beads)
- **AI agent guide**: `AGENTS.md`

---

**Remember**: The goal is for VC to run **autonomously** while we **observe and learn**. Human intervention should be the exception, not the rule.
