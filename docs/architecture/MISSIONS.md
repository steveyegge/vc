# Mission-Driven Architecture: VC as a Self-Healing AI Colony

**Status**: Tracked in Beads (see below)
**Parent Epic**: [vc-215](https://github.com/steveyegge/vc) - MISSIONS.md is documentation debt - convert to tracked epics

---

## Vision

VC is not a single AI agent doing work. It's a **colony of specialized AI workers** coordinating through an **epic-centric issue tracker**, sharing **sandboxed environments**, with work flowing through **label-driven state machines** until reaching a **human approval gate**.

**The Core Insight**: Each user request becomes a **Mission (epic)**. Workers execute tasks within that mission until the epic is complete. Then quality gates verify the work. Then a GitOps arbiter reviews for coherence. Finally, a human approves for merge.

**Terminal state is NOT "global queue empty"** - it's **"THIS epic is complete"**.

### User Experience

```
User: "Add user authentication with OAuth2"

[VC processes for 2-4 hours autonomously]

VC: "Mission complete. Review issue vc-567 created for approval."

User: [Reviews changes in sandbox branch]
User: "Looks good, approved"

[VC merges to main, cleans up sandbox]
```

### How It Works

1. **AI Planner**: Translates request → Mission epic with phases and tasks
2. **Code Workers**: Execute tasks in shared sandbox (iterative)
3. **Quality Gates**: Verify BUILD, TEST, LINT pass
4. **GitOps Arbiter**: Extended-thinking review (coherence, safety)
5. **Human Approval**: Reviews arbiter report, approves/rejects
6. **GitOps Merger**: Auto-merges approved missions to main

The system iterates until convergence (all tasks done, gates pass, review approved).

---

## Implementation Status

**All implementation tracked in Beads.** Do NOT update this file with design details.

### View Roadmap

```bash
# Parent epic with all implementation work
bd show vc-215

# Full dependency tree (8 child epics)
bd dep tree vc-215

# See what's ready to build
bd ready

# Filter to mission architecture work
bd list --label mission-architecture
```

### Epics (Priority Order)

**P1 - Critical Infrastructure:**
- **vc-216**: Epic-Centric Infrastructure (terminal state detection, mission context)
- **vc-217**: Sandbox Lifecycle Management (git worktrees, auto-cleanup)

**P2 - Core Features:**
- **vc-218**: Label-Driven State Machine (needs-quality-gates → needs-review → approved)
- **vc-219**: Quality Gate Workers (gates as separate workers, not inline)
- **vc-220**: GitOps Arbiter (extended-thinking review, human approval gate)
- **vc-223**: Mission Planning (AI Planner translates NL requests → missions)

**P3 - Advanced:**
- **vc-221**: GitOps Merger (auto-merge on approval, conflict handling)
- **vc-222**: Parallel Missions (5 concurrent missions, worker distribution)

**Documentation:**
- **vc-224**: Reduce MISSIONS.md to <100 lines (this task)

### Design Details

**DO NOT add design details to this file.** All design, acceptance criteria, and implementation status live in Beads:

```bash
# Example: View Epic-Centric Infrastructure design
bd show vc-216

# Example: View Sandbox Lifecycle design
bd show vc-217
```

Each epic issue contains:
- Description (what and why)
- Design (how to implement)
- Acceptance Criteria (definition of done)
- Dependencies (what blocks what)

---

## Why This Approach?

**Problem**: This file was 37K of unimplemented design that went stale.

**Solution**: Track all implementation as issues in Beads:
- ✅ Design in issue descriptions (stays current)
- ✅ Status in issue tracker (open/in_progress/closed)
- ✅ Dependencies explicit (bd dep tree shows roadmap)
- ✅ Can query: "what's ready to build?" (bd ready)
- ✅ Beads is source of truth (not markdown files)

**This file**: High-level vision only. Everything else → Beads.

---

For historical context, see: `docs/archive/MISSIONS_FULL.md` (original 37K design doc).
