# VC Architecture Documentation

This directory contains the core architecture and design documents for VC (VibeCoder).

---

## 📖 Reading Order (Start Here)

If you're new to VC, read these documents in this order:

1. **[MISSIONS.md](MISSIONS.md)** - The core architecture
   - Mission-centric workflow (epic-based self-healing AI colony)
   - Worker types and claiming rules
   - Terminal state detection
   - Label-driven state machine
   - Self-healing through discovered issues and convergence
   - **Start here!** This is the blueprint.

2. **[MISSIONS_CONVERGENCE.md](MISSIONS_CONVERGENCE.md)** - Iteration design
   - Why convergence, not waterfall
   - Quality gate escalation paths (minor/major/blocked)
   - Pure beads primitives approach
   - Design evolution details

3. **[BEADS_INTEGRATION.md](BEADS_INTEGRATION.md)** - Using Beads as a library
   - Why Beads integration is critical
   - Benefits of library vs CLI shell-out
   - Performance comparison
   - Migration path

4. **[BEADS_EXTENSIBILITY.md](BEADS_EXTENSIBILITY.md)** - VC as a Beads extension
   - IntelliJ/Android Studio model
   - Extension tables (not column pollution)
   - Keeping Beads general-purpose
   - How VC extends without intruding

5. **[BEADS_LIBRARY_REVIEW.md](BEADS_LIBRARY_REVIEW.md)** - Implementation review
   - Review of beads library integration
   - What's present, what's needed
   - API quality assessment
   - Security and performance analysis

---

## 🎯 Quick Reference

### Core Concepts

| Concept | Document | Section |
|---------|----------|---------|
| **Missions = Epics** | MISSIONS.md | Core Principles |
| **Workers share sandboxes** | MISSIONS.md | Core Principles |
| **Terminal state detection** | MISSIONS.md | Terminal State Detection |
| **Label-driven state machine** | MISSIONS.md | Worker Types |
| **Convergence loop** | MISSIONS_CONVERGENCE.md | Convergence Loop |
| **Quality gate escalation** | MISSIONS_CONVERGENCE.md | Escalation Paths |
| **Extension tables** | BEADS_EXTENSIBILITY.md | Extension Schema |
| **Beads as platform** | BEADS_EXTENSIBILITY.md | The Extension Model |

### Architecture Decisions

| Decision | Rationale | Document |
|----------|-----------|----------|
| **Use Beads as library** | 100x faster than shell-out, type-safe, atomic operations | BEADS_INTEGRATION.md |
| **Pure beads primitives** | No custom executor tables, simpler, everything in JSONL | MISSIONS_CONVERGENCE.md |
| **Extension tables** | Keep Beads clean, VC-specific schema in own tables | BEADS_EXTENSIBILITY.md |
| **Shared database** | Single source of truth, foreign keys work, no sync needed | BEADS_EXTENSIBILITY.md |
| **Iterative convergence** | Not waterfall, system iterates until done/blocked | MISSIONS_CONVERGENCE.md |
| **Labels for state** | Drive mission lifecycle, better than custom columns | MISSIONS.md |

---

## 📐 Implementation Roadmap

From MISSIONS.md, the 8 epics to build the mission workflow:

1. **Epic-Centric Infrastructure** (P0) - Terminal state detection, label queries
2. **Sandbox Lifecycle** (P0) - Auto-create/cleanup sandboxes
3. **Label-Driven State Machine** (P1) - State transitions via labels
4. **Quality Gate Workers** (P1) - Gates as workers, not inline checks
5. **GitOps Arbiter** (P1) - Extended-thinking review before merge
6. **GitOps Merger** (P2) - Automated merge on human approval
7. **Parallel Missions** (P2) - 5 concurrent missions support
8. **AI Planner** (P1) - User request → mission structure

---

## 🔍 Key Design Patterns

### IntelliJ/Android Studio Model

From BEADS_EXTENSIBILITY.md:

```
Beads (Platform)          VC (Extension)
├─ General-purpose        ├─ Workflow engine
├─ Thousands of users     ├─ Extends Beads
├─ No VC code             ├─ Uses Beads API
└─ Extensibility points   └─ Extension tables
```

### Worker Hierarchy

From MISSIONS.md:

```
1. Code Workers       → Execute tasks in sandboxes
2. Quality Gates      → Run BUILD/TEST/LINT, escalate failures
3. GitOps Arbiter     → Extended-thinking review (3-5min)
4. Human Approvers    → Final safety gate
5. GitOps Merger      → Automated merge + cleanup
```

### Convergence Loop

From MISSIONS_CONVERGENCE.md:

```
Code → Terminal → Gates
  ↓ FAIL (minor)   → File tasks → Loop
  ↓ FAIL (major)   → Re-design epic → Loop
  ↓ FAIL (blocked) → Human decision → Continue
  ↓ PASS → Arbiter → Human → Merge ✅
```

---

## 📊 Schema Architecture

### Core Beads Tables (Platform)

From BEADS_EXTENSIBILITY.md:

```sql
-- Beads owns these:
issues           -- Core issue data
dependencies     -- Relationships (blocks, parent-child, etc.)
labels           -- Issue labels (state machine!)
comments         -- Issue comments
events           -- Audit trail
```

### VC Extension Tables

From BEADS_EXTENSIBILITY.md:

```sql
-- VC adds these:
vc_mission_state    -- Mission metadata (subtype, sandbox_path, branch_name)
vc_agent_events     -- Activity feed (agent_spawned, tool_use, etc.)
```

**Key**: Extension tables use foreign keys to Beads tables, enabling clean separation while maintaining referential integrity.

---

## 🚀 Getting Started

### For Implementers

1. Read **MISSIONS.md** to understand the vision
2. Read **BEADS_EXTENSIBILITY.md** to understand the extension model
3. Check the implementation roadmap (8 epics)
4. Start with Epic 1: Epic-Centric Infrastructure

### For Contributors

1. Read **MISSIONS.md** (core architecture)
2. Read **MISSIONS_CONVERGENCE.md** (why iterative, not waterfall)
3. Understand the label-driven state machine
4. Understand extension tables (don't pollute Beads!)

### For Reviewers

1. Read **BEADS_LIBRARY_REVIEW.md** for quality assessment
2. Check that VC doesn't add columns to Beads tables
3. Check that extension tables use foreign keys correctly
4. Verify convergence loop handles all failure modes

---

## 📝 Document Status

| Document | Status | Last Updated |
|----------|--------|--------------|
| MISSIONS.md | Approved | 2025-10-22 |
| MISSIONS_CONVERGENCE.md | Merged into MISSIONS.md | 2025-10-22 |
| BEADS_INTEGRATION.md | Updated (extension model) | 2025-10-22 |
| BEADS_EXTENSIBILITY.md | Current design | 2025-10-22 |
| BEADS_LIBRARY_REVIEW.md | Review complete | 2025-10-22 |

---

## 🤝 Related Documentation

- **[../../README.md](../../README.md)** - Project overview and vision
- **[../../CLAUDE.md](../../CLAUDE.md)** - AI agent instructions
- **[../dogfooding-mission-log.md](../dogfooding-mission-log.md)** - Dogfooding runs history
- **[../../BOOTSTRAP.md](../../BOOTSTRAP.md)** - Original roadmap (now in beads)

---

**Questions?** File an issue in beads or add to vc-26 (Dogfooding epic).
