# Next Session Prompt: Fix Phase 2 Blockers

**Goal**: Fix 4 critical bugs blocking Phase 2 expansion experiment, then retry Phase 2 with confidence.

---

## Context (Read First)

Phase 2 experiment (vc-92da) **failed catastrophically** - only 1/9 issues completed (11.1% vs Phase 1's 100%). The failure revealed critical bugs in the executor infrastructure that cause infinite retry loops, stale caching, and orphaned state.

**Phase 2 is the gateway to L1 "Bug Crusher" capability** - without it, VC remains stuck at L0 "Supervised Small Tasks" and can't progress toward self-hosting.

Full analysis in: `PHASE_2_ASSESSMENT.md`

---

## Session Objectives

Fix these 4 bugs in priority order, then prepare to retry Phase 2:

### 🔴 P0 Bugs (Must Fix)

1. **vc-39e8**: Watchdog infinite loop detection causes 24-hour retry storms
   - Exponential backoff (vc-165b) exists but not working
   - Executor immediately re-claims issues after watchdog kills them
   - Results in 180+ interventions with zero progress

2. **vc-47e0**: Executor baseline health cache not invalidated after fixes
   - Stays in degraded mode even after baseline is healthy
   - Only workaround: restart executor
   - Blocks regular work unnecessarily

3. **vc-db5d**: GIT_EDITOR=':' may fail on Windows systems
   - Unix-specific no-op editor
   - Will break git operations on Windows

### 🟡 P1 Bug (Should Fix)

4. **vc-4820**: 'bd close' doesn't clear execution state, leaving orphaned claims
   - 8 issues from Phase 2 currently orphaned
   - Causes query confusion (closed but also in_progress)

---

## Suggested Approach

### Phase 1: Fix vc-39e8 (Watchdog Infinite Loop) - HIGHEST PRIORITY

**Time Estimate**: 30-60 minutes

**Diagnosis Plan**:
1. Review vc-165b implementation (exponential backoff)
   - Find RecordWatchdogIntervention() function
   - Check if it's being called from watchdog events
   - Verify it's updating intervention_count and last_intervention_time

2. Review GetReadyWork() implementation
   - Find where it filters issues
   - Check if it's calling CalculateInterventionBackoff()
   - Verify backoff logic is correct

3. Add debug logging if needed
   - Log when RecordWatchdogIntervention() is called
   - Log when GetReadyWork() skips an issue due to backoff
   - Set VC_DEBUG_EVENTS=1 if needed

**Fix Plan**:
- If RecordWatchdogIntervention() not called: wire it up to watchdog events
- If GetReadyWork() not checking backoff: add filter logic
- If backoff calculation wrong: fix the math
- Add test if time permits: simulate watchdog kill → verify issue not re-claimed

**Success Criteria**:
- After watchdog kills agent, issue not re-claimed for backoff period
- Backoff schedule working: 5min, 10min, 20min, 40min, 1h20, 2h40, 4h
- Log evidence or test proves it works

---

### Phase 2: Fix vc-47e0 (Baseline Cache Invalidation)

**Time Estimate**: 20-40 minutes

**Options**:
A. Re-check baseline every N poll cycles (simplest)
B. Watch for issue status changes and invalidate cache
C. Add 'vc refresh-baseline' CLI command
D. Periodic background baseline health check

**Recommended**: Option A (periodic re-check)

**Fix Plan**:
1. Find where executor checks baseline health
2. Add counter: re-run baseline quality gates every 5 poll cycles
3. If baseline now healthy, clear degraded mode flag
4. Log when exiting degraded mode

**Success Criteria**:
- After fixing baseline issues, executor exits degraded mode within 5 poll cycles
- No executor restart required
- Log evidence shows cache invalidation happening

---

### Phase 3: Fix vc-db5d (Windows GIT_EDITOR)

**Time Estimate**: 15-30 minutes

**Fix Plan**:
1. Find git wrapper code (internal/git/ or similar)
2. Add OS detection: `runtime.GOOS == "windows"`
3. Use platform-specific no-op editor:
   - Unix/Linux/macOS: `:`
   - Windows: `cmd /c exit` or `true.exe` (if Git Bash) or document requirement
4. Test if possible, otherwise document

**Success Criteria**:
- GIT_EDITOR set appropriately per platform
- Git operations work without hanging for editor
- Documented if Windows testing not available

---

### Phase 4: Fix vc-4820 (Orphaned Execution State)

**Time Estimate**: 15-30 minutes

**Fix Plan**:
1. Clean up current orphans:
   ```sql
   DELETE FROM vc_issue_execution_state
   WHERE issue_id IN (
     SELECT id FROM issues WHERE status='closed'
   );
   ```

2. Modify `bd close` command:
   - Find close logic in beads storage layer
   - Add: DELETE FROM vc_issue_execution_state WHERE issue_id = ?
   - Make it atomic with issue status update

3. Verify fix:
   - Run `bd close test-issue`
   - Query: should have no execution state

**Success Criteria**:
- Current 8 orphaned issues cleaned up
- `bd close vc-X` atomically clears execution state
- `bd list --status in_progress` doesn't show closed issues

---

## After All Fixes: Prepare Phase 2 Retry

### Cleanup and Documentation

1. **Export to JSONL**: `bd export -o .beads/issues.jsonl`

2. **Close vc-92da** (the failed Phase 2 attempt):
   ```bash
   bd close vc-92da --reason "Phase 2 experiment failed (1/9 = 11.1%). Discovered critical bugs: vc-39e8 (watchdog loop), vc-47e0 (baseline cache), vc-4820 (orphaned state), vc-db5d (Windows). All fixed. Ready to retry via vc-3121."
   ```

3. **Verify vc-3121 ready**: `bd show vc-3121`
   - Should be ready to claim
   - 10 diverse bugs selected
   - Success target: 75%+ combined Phase 1+2

4. **Commit all fixes**:
   ```bash
   git add -A
   git commit -m "fix: Resolve 4 critical bugs blocking Phase 2 expansion experiment

   - vc-39e8: Fix watchdog infinite loop (exponential backoff now working)
   - vc-47e0: Add baseline cache invalidation (periodic re-check)
   - vc-db5d: Fix Windows GIT_EDITOR compatibility
   - vc-4820: Clear execution state on bd close (cleanup orphans)

   These bugs caused Phase 2 to fail (1/9 = 11.1%). Now ready to retry.

   🤖 Generated with [Claude Code](https://claude.com/claude-code)

   Co-Authored-By: Claude <noreply@anthropic.com>"
   ```

5. **Push**: `git push`

---

## Next Steps After This Session

Once all 4 bugs are fixed:

**Option A: Immediately retry Phase 2**
- Start executor
- Monitor vc-3121 execution
- Track success rate in real-time
- Target: 75%+ success (11+/15 total)

**Option B: Wait for human decision**
- Document fixes completed
- Get confirmation before retrying Phase 2
- Allows time to review changes

**Recommend Option B** - give human chance to review fixes before running expensive experiment.

---

## Testing Strategy

### Minimal Testing (Fast Path)
- Add log statements proving fixes work
- Manual verification: close issue, check it's not re-claimed immediately
- Document expected behavior

### Full Testing (Thorough Path)
- Write unit tests for each fix
- Add integration test for watchdog + executor interaction
- Verify baseline cache invalidation with test
- Test Windows compatibility if possible

**Recommend Minimal Testing** - fixes are straightforward, Phase 2 retry is the real test.

---

## Expected Outcomes

### After this session:
- ✅ All 4 P0/P1 bugs fixed and tested
- ✅ Executor infrastructure solid for scale
- ✅ vc-92da closed with lessons learned
- ✅ Ready to retry Phase 2 via vc-3121
- ✅ Commits pushed, JSONL exported

### After Phase 2 retry (future session):
- 🎯 75%+ success rate (11+/15 issues)
- 🎯 <20% human intervention
- 🎯 Watchdog interventions ≤5 per issue
- 🎯 Clear path to Phase 3 and L1 "Bug Crusher"

---

## Ready Issues for Reference

Current ready work (`bd ready --limit 10`):

1. [P0] vc-4778: Define no-auto-claim policy (meta/strategy - skip for now)
2. [P0] vc-db5d: GIT_EDITOR Windows fix 🎯 **TARGET**
3. [P0] vc-47e0: Baseline cache fix 🎯 **TARGET**
4. [P0] vc-39e8: Watchdog loop fix 🎯 **TARGET**
5. [P1] vc-4820: Orphaned execution state 🎯 **TARGET**
6-10. [P1] Various test tasks (defer until after Phase 2)

---

## Quick Start Command

To start this session effectively:

```bash
# Set up environment
export ANTHROPIC_API_KEY=your-key-here
export VC_DEBUG_EVENTS=1  # Optional: debug logging

# Check ready work
bd ready --limit 10

# Start with highest priority
bd show vc-39e8

# Read the assessment
cat PHASE_2_ASSESSMENT.md
```

---

## Success Metrics for This Session

**Must achieve before session ends**:

- [ ] vc-39e8 fixed and verified (watchdog backoff working)
- [ ] vc-47e0 fixed and verified (baseline cache invalidates)
- [ ] vc-db5d fixed (Windows GIT_EDITOR compatibility)
- [ ] vc-4820 fixed (orphaned execution state cleaned up)
- [ ] vc-92da closed with failure summary
- [ ] All changes committed and pushed
- [ ] JSONL exported to git
- [ ] Ready to retry Phase 2 with confidence

**Time Budget**: 2-3 hours for all 4 fixes

---

## Important Notes

- **Don't skip vc-39e8** - it's the highest priority and most impactful
- **Test minimally but effectively** - Phase 2 retry is the real test
- **Document Windows testing limitations** if you can't test on Windows
- **Export JSONL before committing** - always keep git in sync
- **Don't start Phase 2 retry in this session** - wait for human confirmation

---

## Key Files to Reference

- `PHASE_2_ASSESSMENT.md` - Full analysis (read this!)
- `CLAUDE.md` - Project instructions and workflow
- `docs/NO_AUTO_CLAIM_POLICY.md` - Policy context
- `.beads/issues.jsonl` - Source of truth for issues

---

**Good luck! Phase 2 success unlocks the path to self-hosting. 🚀**
