# No-Auto-Claim Audit - November 4, 2025

## Summary

Audited all open issues with `no-auto-claim` label against the new narrow policy criteria:
1. External coordination
2. Human creativity
3. Business judgment
4. Pure research

**Found**: 4 issues with the label
**Recommendation**: Remove from 2, keep on 2

---

## Issue-by-Issue Assessment

### ✅ KEEP: vc-4778 - Define no-auto-claim policy and push toward self-hosting
**Status**: P0 task, currently being worked on
**Current justification**: Meta/strategic planning
**New policy assessment**:
- **Pure research** (initially) - Policy definition required exploration
- **Business judgment** - Strategic decisions about VC's capabilities and risk tolerance
- However, research phase is complete; remaining work is execution (audits, experiments)

**Recommendation**: **KEEP for now** (actively being worked on), but could remove once current work completes since execution tasks don't require the label

**Rationale**: This is the policy-defining issue itself. While the remaining acceptance criteria are execution tasks, keeping the label signals that this requires strategic oversight.

---

### ⚠️ BORDERLINE: vc-a447 - Test single-repo mode compatibility when beads ships multi-repo
**Status**: P3 task, blocked
**Current justification**: "Requires human oversight, external coordination with beads team"
**New policy assessment**:
- **External coordination** - Potentially, if requires communication with beads team
- However, description suggests this is just "wait for release, then test" not active coordination
- Testing scope is well-defined: performance, locking, config, JSONL workflow

**Recommendation**: **REASSESS when unblocked**
- If testing can be done autonomously (no beads team questions), remove label
- If requires active coordination (asking questions, clarifying behavior), keep label
- Currently blocked anyway, so not urgent

**Rationale**: The distinction between "blocked by external dependency" (remove label) vs. "requires coordination with external team" (keep label) matters. This looks more like the former.

---

### ❌ REMOVE: vc-222 - Parallel Missions (Multi-Tenancy)
**Status**: P3 epic, depends on vc-217
**Current justification**: Not stated in issue
**New policy assessment**:
- ❌ Not external coordination (internal development)
- ❌ Not human creativity (technical implementation)
- ❌ Not business judgment (engineering work)
- ❌ Not pure research (has clear design, acceptance criteria)

**Recommendation**: **REMOVE label**

**Rationale**: This is complex, well-defined engineering work. It has:
- Detailed design (worker scheduling, resource management, conflict handling)
- Clear acceptance criteria (5 missions concurrent, priority distribution, tests)
- Technical specifications (config params, claiming logic)

This is exactly the type of complex work VC should be handling. The safety nets (tests, quality gates, sandbox isolation) can catch issues. Complexity alone doesn't justify no-auto-claim under the new policy.

**Action**:
```bash
bd label remove vc-222 no-auto-claim
```

---

### ❌ REMOVE: vc-221 - GitOps Merger (Automated Merge)
**Status**: P3 epic, depends on vc-220
**Current justification**: Not stated in issue
**New policy assessment**:
- ❌ Not external coordination (internal automation)
- ❌ Not human creativity (technical implementation)
- ❌ Not business judgment (implementation of defined requirements)
- ❌ Not pure research (has clear design, acceptance criteria)

**Recommendation**: **REMOVE label**

**Rationale**: This is complex, safety-critical engineering work, but it has:
- Detailed merge process design
- Conflict handling (escalate to human)
- Safety mechanisms (preconditions, rollback)
- Clear acceptance criteria with tests

The fact that it's "risky" (automated merging) doesn't justify no-auto-claim. The design includes safety mechanisms (gates, approvals, conflict detection). This is exactly the kind of work VC should learn to handle.

**Action**:
```bash
bd label remove vc-221 no-auto-claim
```

---

## Summary Actions

### Immediate (remove labels):
```bash
bd label remove vc-222 no-auto-claim  # Parallel Missions
bd label remove vc-221 no-auto-claim  # GitOps Merger
```

### Monitor/Reassess:
- **vc-a447**: Reassess when unblocked (after beads ships multi-repo)
- **vc-4778**: Keep for now (active work), remove when issue closes

---

## Impact

**Before audit**: 4 issues with no-auto-claim
**After audit**: 2 issues with no-auto-claim (50% reduction)

**Issues newly available for VC executor**:
- vc-222 (when dependencies resolve)
- vc-221 (when dependencies resolve)

Both are P3 and blocked by dependencies, so won't be claimed immediately. This gives us time to ensure VC is ready for L2 "Feature Builder" level work (these are features, not bugs).

---

## Lessons Learned

1. **Complexity ≠ no-auto-claim**: Well-designed complex work with tests and acceptance criteria is fair game for VC

2. **"Safety-critical" needs analysis**: Don't reflexively add the label. Ask: "Which safety net would catch a mistake?"
   - vc-221 has: conflict detection, escalation, rollback, gates
   - These safety nets justify letting VC try

3. **Blocked issues can wait**: No urgency to remove labels from blocked P3 issues, but we should remove them on principle (sends signal about what VC can handle)

4. **Research vs. execution**: vc-4778 was research initially, but is now execution. Labels should evolve as work progresses.

---

## Next Steps (from vc-4778)

1. ✅ **Policy defined**: docs/NO_AUTO_CLAIM_POLICY.md complete
2. ✅ **CLAUDE.md updated**: References policy doc
3. ✅ **Audit complete**: This document
4. ⏭️ **Remove inappropriate labels**: vc-222, vc-221
5. ⏭️ **Design Phase 1 experiment**: Select 5 code review bugs (next acceptance criterion)

---

**Audit conducted**: November 4, 2025
**Auditor**: Claude Code (assisting user stevey)
**Context**: vc-4778 Phase 1 acceptance criteria
