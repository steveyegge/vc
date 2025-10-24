# Dogfooding Workflow: VC Self-Healing Missions

**Status**: Active dogfooding in progress
**Owner**: vc-106
**Detailed run logs**: See `docs/dogfooding-run-*.md`

## Quick Start

```bash
# 1. Build and set API key
go build ./cmd/vc
export ANTHROPIC_API_KEY=your-key-here

# 2. Start executor (autonomous mode - sandboxes enabled by default)
./vc execute

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

## Current Status (Updated 2025-10-21)

| Metric | Value | Notes |
|--------|-------|-------|
| **Total runs** | 14 | Runs #1-14 complete |
| **Agent completion** | ~95% | Agents successfully execute work |
| **Quality gate pass** | ~40% | Gates often too strict, but AI recovery working |
| **Issues discovered** | 15+ | Run #14: 3 new issues filed |
| **Activity feed** | ✅ Working | `vc tail -f` for live monitoring |
| **AI supervision** | ✅ Excellent | Assessment/analysis confidence 0.88-0.92 |
| **GitOps** | ❌ Disabled | Intentional safety during bootstrap |
| **Human intervention** | ~40% | Still need to reduce to <10% |

**Key achievements**:
- Agent execution highly reliable (run #14: 100% completion)
- AI supervision working well (assess + analyze)
- Graceful shutdown implemented and working
- Watchdog running continuously, detecting anomalies

**Known issues**:
- Quality gates too strict (blocking good work)
- Event type CHECK constraints missing (deduplication/cleanup)
- Execution state race condition with quality gates

---

## Essential Commands

### Starting a Run
```bash
# Sandboxes enabled by default (vc-144)
./vc execute --poll-interval 2

# To disable sandboxes (DANGEROUS - development only):
./vc execute --disable-sandboxes
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

### Run #14 (2025-10-20) - Infrastructure Validation ✅
- **Target**: Ready work queue (vc-72, vc-225, vc-229, vc-228)
- **Result**: 4/4 agents completed, 1 issue closed (acceptable_failure strategy)
- **Duration**: ~20 minutes
- **Key findings**: Agent execution perfect, quality gates too strict, AI recovery working
- **Issues filed**: vc-226 (P2), vc-227 (P1), vc-228 (P1, completed)
- **Bugs exposed**: Event schema CHECK constraints, state race condition

### Runs #10-13
- Focused on AI supervision validation and infrastructure hardening
- Quality gates, watchdog, graceful shutdown all validated
- Multiple bugs discovered and fixed

### Runs #1-9
- Bootstrap phase: establishing workflow, tooling, and patterns
- Activity feed, executor, basic infrastructure

**Full history**: See vc-106 notes for detailed run logs

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

### Run #15 (Planned)
**Goal**: Observe pipeline, identify bugs, file issues. Actual fix is secondary.
**Target**: Pick a P2 issue (vc-129, vc-133, vc-134, vc-200, or vc-206)
**Focus**: Pipeline behavior, bug discovery, not completion

### Short-term
1. Fix event schema CHECK constraints (from run #14)
2. Fix execution state race condition (from run #14)
3. Tune quality gate expectations (too strict currently)

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
