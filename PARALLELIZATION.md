# VC Dogfooding Parallelization Guide

This document identifies independent work streams that can be executed in parallel by different agents or developers.

## 🚀 Executive Summary

**6 major work streams are ready for parallel execution RIGHT NOW!**

Each stream is self-contained and can be worked on independently until the final integration phase. All design tasks (first step in each stream) are unblocked and ready.

## 📊 Work Streams Overview

```
Stream A: Sandbox Management      (vc-91) → 5 tasks
Stream B: Enhanced Context        (vc-97) → 5 tasks
Stream C: Structured Output       (vc-103) → 4 tasks
Stream D: Code Review Workflow    (vc-108) → 4 tasks
Stream E: Mission Orchestration   (vc-113) → 4 tasks
Stream F: Git Operations          (vc-118) → 4 tasks

Total: 26 parallelizable tasks
```

## 🔗 Dependency Analysis

### Zero Inter-Stream Dependencies (Pure Parallel)
- Stream A (Sandbox) - Standalone
- Stream B (Context) - Standalone
- Stream D (Code Review) - Standalone
- Stream E (Mission Orchestration) - Standalone

### Minimal Inter-Stream Dependencies
- **Stream C (Structured Output)** - Standalone until integration
- **Stream F (Git Operations)** - Has 1 dependency on Stream C (vc-122 needs vc-104)

### Cross-Stream Dependency
```
vc-122 (Git event tracking) → vc-104 (AgentEvent types)
```

**Impact**: Stream F can proceed with tasks vc-119, vc-120, vc-121 independently. Only vc-122 needs to wait for vc-104 from Stream C.

## 📋 Work Stream Details

### Stream A: Sandbox Management (vc-91)

**Goal**: Isolated execution environments with git worktrees

**Critical Path**:
```
vc-92 (types) → vc-93 (git worktree) → vc-95 (manager) → vc-96 (integration)
                vc-94 (database)     ↗
```

**Ready Now**: vc-92
**Blocks**: Nothing in other streams
**Blocked By**: Nothing

**Key Deliverables**:
- Sandbox isolation with git worktrees
- Per-sandbox beads database
- Lifecycle management

---

### Stream B: Enhanced Context Management (vc-97)

**Goal**: Comprehensive agent prompting with history and resume capability

**Critical Path**:
```
vc-98 (types) → vc-99 (history) → vc-100 (gatherer) → vc-101 (builder) → vc-102 (integration)
```

**Ready Now**: vc-98
**Blocks**: Nothing in other streams
**Blocked By**: Nothing

**Key Deliverables**:
- Prompt templates with all context
- Execution history tracking
- Nondeterministic idempotence support

---

### Stream C: Structured Output Parsing (vc-103)

**Goal**: Extract structured events from agent output for activity feed and watchdog

**Critical Path**:
```
vc-104 (types) → vc-105 (parser) → vc-107 (integration)
                vc-106 (storage) ↗
```

**Ready Now**: vc-104
**Blocks**: Stream F (vc-122 needs vc-104)
**Blocked By**: Nothing

**Key Deliverables**:
- Event extraction from agent output
- Activity feed infrastructure
- Watchdog data source

---

### Stream D: Code Review Workflow (vc-108)

**Goal**: Automated code review with issue filing

**Critical Path**:
```
vc-109 (review-only mode) → vc-110 (auto-create) → vc-111 (findings) → vc-112 (gate)
```

**Ready Now**: vc-109
**Blocks**: Nothing in other streams
**Blocked By**: Nothing

**Key Deliverables**:
- Review-only agent mode
- Automatic review issue creation
- Review findings → blocking issues

---

### Stream E: Mission Orchestration (vc-113)

**Goal**: Outer/middle loop with AI-driven phase planning

**Critical Path**:
```
vc-114 (types) → vc-115 (AI planning) → vc-116 (approval gate)
                                      → vc-117 (epic completion)
```

**Ready Now**: vc-114
**Blocks**: Nothing in other streams (vc-126 is optional REPL enhancement)
**Blocked By**: Nothing

**Key Deliverables**:
- Mission → Phase → Task hierarchy
- AI-driven work breakdown
- Human approval gates

---

### Stream F: Git Operations Integration (vc-118)

**Goal**: Automated git workflow with conflict handling

**Critical Path**:
```
vc-119 (auto-commit) → vc-120 (rebase) → vc-121 (conflicts)
                                        ↘
vc-104 (from Stream C) → vc-122 (git event tracking)
```

**Ready Now**: vc-119
**Blocks**: Nothing in other streams
**Blocked By**: vc-122 depends on vc-104 from Stream C

**Key Deliverables**:
- Auto-commit with AI messages
- Rebase and conflict handling
- Git event tracking

---

## 🎯 Parallelization Strategy

### Phase 1: Parallel Design (Week 1)
**All agents can start immediately:**
- Agent 1: vc-92 (Sandbox types)
- Agent 2: vc-98 (Context types)
- Agent 3: vc-104 (Event types)
- Agent 4: vc-109 (Review-only mode)
- Agent 5: vc-114 (Mission types)
- Agent 6: vc-119 (Auto-commit)

**Duration**: ~1-2 days per task
**Parallelism**: 6x speedup

### Phase 2: Parallel Implementation (Week 2-3)
**Each stream proceeds independently:**
- Stream A: vc-93, vc-94 (can parallelize these 2)
- Stream B: vc-99, vc-100
- Stream C: vc-105, vc-106 (can parallelize these 2)
- Stream D: vc-110, vc-111
- Stream E: vc-115, vc-116, vc-117
- Stream F: vc-120, vc-121

**Duration**: ~3-5 days per task
**Parallelism**: 6x speedup continues

### Phase 3: Integration (Week 4)
**Some coordination required:**
- Stream A: vc-96 (integrates sandbox into executor)
- Stream B: vc-102 (integrates prompting into agent spawn)
- Stream C: vc-107 (integrates output parsing)
- Stream D: vc-112 (integrates review gate)
- Stream F: vc-122 (can now proceed, depends on Stream C completion)

**Note**: These can still overlap significantly since they integrate into different parts of the system.

---

## 🔢 Capacity Planning

### Optimal Team Size
**6 agents/developers** - one per stream

### Minimum Team Size
**3 agents** - can still achieve 3x parallelism:
- Agent 1: Streams A + B (sandbox + context)
- Agent 2: Streams C + F (output + git)
- Agent 3: Streams D + E (review + orchestration)

### Single Agent
Even with one agent, the clear dependency chains make it easy to context-switch between streams when blocked.

---

## 📌 Labels for Organization

Add these labels to issues for easy filtering:

- `stream:sandbox` - vc-91 through vc-96
- `stream:context` - vc-97 through vc-102
- `stream:output` - vc-103 through vc-107
- `stream:review` - vc-108 through vc-112
- `stream:orchestration` - vc-113 through vc-117
- `stream:git` - vc-118 through vc-122

---

## 🚦 Ready Work Query

To see all ready work across streams:
```bash
bd ready --limit 50
```

To filter by stream (once labels are added):
```bash
bd list --status open --labels stream:sandbox
```

---

## ⚠️ Integration Risks

### Risk 1: Interface Mismatches
**Mitigation**: Design tasks (vc-92, vc-98, vc-104, etc.) define clear interfaces. Review these early for consistency.

### Risk 2: Executor Refactoring Conflicts
**Mitigation**: Integration tasks (vc-96, vc-102, vc-107) may touch same code. Do these sequentially or coordinate carefully.

### Risk 3: Testing Dependencies
**Mitigation**: vc-128 (Comprehensive Integration Tests) can't complete until all streams integrate. This is expected.

---

## 📈 Estimated Timeline

### With 6 Parallel Agents
- **Week 1**: All design tasks complete
- **Week 2-3**: All implementation tasks complete
- **Week 4**: Integration complete
- **Total**: 4 weeks

### With 1 Agent (Sequential)
- **Week 1-6**: Design tasks (6 days)
- **Week 2-12**: Implementation tasks (20 days)
- **Week 13-14**: Integration tasks (6 days)
- **Total**: ~14 weeks

**Parallelization Gain**: 3.5x speedup with 6 agents

---

## ✅ Verification

All streams are correctly showing as ready:
```bash
$ bd ready --limit 15
1. [P0] vc-128: Comprehensive Integration Tests
2. [P0] vc-119: Implement auto-commit with AI-generated messages      ← Stream F
3. [P0] vc-118: Git Operations Integration                            ← Stream F Epic
4. [P0] vc-114: Design mission planning types and interfaces          ← Stream E
5. [P0] vc-113: Mission Orchestration and Middle Loop                 ← Stream E Epic
6. [P0] vc-109: Implement review-only agent mode                      ← Stream D
7. [P0] vc-108: Code Review Workflow                                  ← Stream D Epic
8. [P0] vc-104: Design AgentEvent types and EventStore interface      ← Stream C
9. [P0] vc-103: Structured Output Parsing and Event Extraction        ← Stream C Epic
10. [P0] vc-98: Design PromptContext types and ContextGatherer        ← Stream B
11. [P0] vc-97: Enhanced Context Management and Prompting             ← Stream B Epic
12. [P0] vc-92: Design sandbox package types and interfaces           ← Stream A
13. [P0] vc-91: Sandbox Management System                             ← Stream A Epic
```

**✓ All 6 streams have ready work!**
**✓ Zero cross-stream blocking at this stage!**
**✓ Can parallelize 6x immediately!**
