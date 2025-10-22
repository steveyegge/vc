# Mission-Driven Architecture: VC as a Self-Healing AI Colony

**Status**: Design (In Review)
**Created**: 2025-10-22
**Author**: Claude (reconstructing lost session work)

---

## Executive Summary

VC is not a single AI agent doing work. It's a **colony of specialized AI workers** coordinating through an **epic-centric issue tracker**, sharing **sandboxed environments**, with work flowing through **label-driven state machines** until reaching a **human approval gate**.

**The Core Insight**: Each user request becomes a **Mission (epic)**. Workers execute tasks within that mission until the epic is complete. Then quality gates verify the work. Then a GitOps arbiter reviews for coherence. Finally, a human approves for merge.

**Terminal state is NOT "global queue empty"** - it's **"THIS epic is complete"**.

---

## The Vision: Self-Healing AI Colony

### What the User Sees

```
User: "Add user authentication with OAuth2"

[VC processes for 2-4 hours autonomously]

VC: "Mission complete. Review issue vc-567 created for approval."

User: [Reviews changes in sandbox branch]
User: "Looks good, approved"

[VC merges to main, cleans up sandbox]
```

### What Actually Happens (Iterative Convergence)

**NOTE**: This is NOT a waterfall. The system iterates until convergence.

1. **AI Planner** (1 worker): Translates request → Mission epic with 3 phase epics and 15 tasks
2. **Code Workers** (15 workers): Execute tasks sequentially in shared sandbox
3. **Analysis Workers** (15 workers): Verify each task, file 8 discovered issues
4. **Code Workers** (8 more workers): Fix discovered issues
5. **Terminal state reached** → Mission ready for quality gates
6. **Quality Gate Worker** (1 worker): Runs BUILD → TEST → LINT
   - **If PASS**: Continue to step 7
   - **If FAIL (minor)**: File 3-5 bug tasks → back to step 2 (iteration)
   - **If FAIL (major)**: Create re-design epic → back to step 1 (re-plan + iteration)
   - **If BLOCKED**: Escalate to human → wait for decision → continue
7. **GitOps Arbiter** (1 extended-thinking worker): Reviews entire mission for coherence
8. **Human** (you): Approves final review issue
9. **GitOps Merger** (1 worker): Merges branch, closes epic, cleans up sandbox

**Example with 2 iterations:**
- Iteration 1: Steps 1-6, gates fail (build errors) → 3 tasks filed
- Iteration 2: Steps 2-6, gates pass → continue to step 7
- Total: ~50 AI workers + 1 human decision = merged PR

---

## Core Principles

### 1. Mission = Epic (The Organizing Principle)

Every user request becomes a **Mission epic** with:
- Unique epic ID (e.g., `vc-300`)
- Phased breakdown (child epics for each phase)
- Task breakdown (child tasks for each phase)
- Dedicated sandbox (git worktree + branch)
- Label-driven state machine (tracks progress)

**Example Mission Structure:**
```
Mission: vc-300 "Add user authentication" [epic, subtype=mission]
  labels: [mission, sandbox:mission-300, status:in-progress]
  sandbox: .sandboxes/mission-300/
  branch: mission/vc-300-user-auth

├─ Phase 1: vc-301 "OAuth provider setup" [epic, subtype=phase]
│  ├─ Task: vc-302 "Configure OAuth credentials" [task]
│  ├─ Task: vc-303 "Implement OAuth flow" [task]
│  └─ Task: vc-304 "Add OAuth middleware" [task]
│
├─ Phase 2: vc-305 "User database integration" [epic, subtype=phase]
│  ├─ Task: vc-306 "Create user schema" [task]
│  ├─ Task: vc-307 "Implement user CRUD" [task]
│  └─ Task: vc-308 "Add session management" [task]
│
└─ Phase 3: vc-309 "Testing and validation" [epic, subtype=phase]
   ├─ Task: vc-310 "Write unit tests" [task]
   ├─ Task: vc-311 "Write integration tests" [task]
   └─ Task: vc-312 "Security audit" [task]
```

### 2. Workers Share Sandboxes (Sequential Execution Within Missions)

All workers on a mission share the **same sandbox directory and git branch**:

```bash
# Mission vc-300 starts
$ git worktree add .sandboxes/mission-300 -b mission/vc-300-user-auth

# Worker 1 claims vc-302, works in .sandboxes/mission-300
# Worker 2 claims vc-303, works in SAME .sandboxes/mission-300
# Worker 3 claims vc-304, works in SAME .sandboxes/mission-300
# ... all 15 workers share the sandbox ...

# Mission vc-300 completes, branch has 15 commits
# Quality gates run in .sandboxes/mission-300
# GitOps review analyzes .sandboxes/mission-300
# Human approves
# Merge mission/vc-300-user-auth → main
# Cleanup: git worktree remove .sandboxes/mission-300
```

**Why this works:**
- Context accumulates (later workers see earlier work)
- One coherent PR (not 15 separate PRs)
- Atomic quality gates (test the whole mission)
- Easy rollback (just discard the branch)

### 3. Terminal State Detection = Epic Completion

**WRONG:** "Workers continue until global ready queue is empty"
- Multiple missions running in parallel
- Workers on mission A don't care about mission B
- Global queue never empties (always more work)

**RIGHT:** "Workers continue until THIS epic is complete"

**Epic is complete when:**
```sql
-- All children are in terminal states
SELECT COUNT(*) FROM dependencies d
  JOIN issues child ON d.issue_id = child.id
  WHERE d.depends_on_id = 'vc-300'  -- the mission epic
    AND d.type = 'parent-child'
    AND child.status NOT IN ('closed', 'blocked', 'deferred');
-- Returns 0 → epic complete!
```

**Then what?**
1. Executor detects epic vc-300 complete
2. Adds label `needs-quality-gates` to vc-300
3. Quality gate worker claims it (next section)

### 4. Label-Driven State Machine (Work Flows Through Labels)

Workers claim work based on **labels**, not just status. This allows different **worker types** to claim different **kinds** of work.

**State transitions:**
```
[User Request]
    ↓
[AI Planner creates mission epic]
    ↓
mission: {labels: [mission, sandbox:mission-300]}
    ↓
[Code workers claim tasks, work in sandbox]
    ↓
mission: {labels: [mission, sandbox:mission-300, needs-quality-gates]}
    ↓
[Quality gate worker claims mission]
    ↓
mission: {labels: [mission, sandbox:mission-300, gates-passed, needs-review]}
    ↓
[GitOps arbiter claims mission]
    ↓
mission: {labels: [mission, sandbox:mission-300, review-complete, needs-human-approval]}
    ↓
[Human approves review issue]
    ↓
mission: {labels: [mission, sandbox:mission-300, approved]}
    ↓
[GitOps merger merges branch]
    ↓
mission: {status: closed, labels: [mission, merged]}
```

### 5. Worker Types (Specialized AI Colony)

Different workers claim different types of work using **label-based claiming rules**.

#### Worker Type 1: Code Workers (Task Execution)

**Claiming rule:**
```sql
SELECT id FROM issues
WHERE status = 'open'
  AND type = 'task'
  AND id IN (
    -- Only tasks belonging to missions with active sandboxes
    SELECT d.issue_id FROM dependencies d
    JOIN issues mission ON d.depends_on_id = mission.id
    JOIN labels l ON mission.id = l.issue_id
    WHERE d.type = 'parent-child'
      AND mission.type = 'epic'
      AND l.label LIKE 'sandbox:%'
      AND l.label NOT LIKE '%needs-quality-gates%'
  )
  AND NOT EXISTS (
    -- No blockers
    SELECT 1 FROM dependencies d2
    JOIN issues blocker ON d2.depends_on_id = blocker.id
    WHERE d2.issue_id = issues.id
      AND d2.type = 'blocks'
      AND blocker.status NOT IN ('closed')
  )
ORDER BY priority, created_at
LIMIT 1;
```

**What they do:**
1. Claim a task within a mission
2. Work in the mission's sandbox (`.sandboxes/mission-XXX/`)
3. Execute the task (write code, fix bugs, etc.)
4. Commit changes to mission branch
5. Mark task as complete
6. AI analysis: check completion, file discovered issues
7. Discovered issues become new tasks in the same mission/phase

#### Worker Type 2: Quality Gate Workers (Mission Verification)

**Claiming rule:**
```sql
SELECT id FROM issues
WHERE type = 'epic'
  AND subtype = 'mission'
  AND EXISTS (
    SELECT 1 FROM labels
    WHERE issue_id = issues.id
      AND label = 'needs-quality-gates'
  )
ORDER BY priority, updated_at
LIMIT 1;
```

**What they do:**
1. Claim a completed mission epic
2. Change to mission sandbox (`.sandboxes/mission-XXX/`)
3. Run quality gates in order:
   - **BUILD**: `go build ./...` (must pass)
   - **TEST**: `go test ./...` (must pass)
   - **LINT**: `golangci-lint run` (must pass)
4. **Quality Gate Analyzer (AI)** examines failures and decides escalation:
   - **MINOR** (< 5 files, simple fixes):
     - File 3-5 bug tasks as children of mission
     - Add label `gates-failed`, `has-fix-tasks`
     - Code workers claim and fix → loop back to terminal state
   - **MAJOR** (fundamental design issues):
     - Create re-design epic as child of mission
     - Add label `gates-failed`, `needs-redesign`
     - AI planner breaks down re-design → code workers execute → loop back
   - **BLOCKED** (unclear, risky, needs human):
     - Create blocking issue requiring human decision
     - Add label `blocked-on-human`
     - Mission waits until human resolves blocker
5. If all pass:
   - Remove label `needs-quality-gates`
   - Add label `gates-passed`
   - Add label `needs-review`

**Key insights:**
- Quality gates are **workers**, not passive checks!
- **AI decides escalation path** based on failure severity
- System **iterates until convergence** (not one-shot)

#### Worker Type 3: GitOps Arbiter (Coherence Review)

**Claiming rule:**
```sql
SELECT id FROM issues
WHERE type = 'epic'
  AND subtype = 'mission'
  AND EXISTS (
    SELECT 1 FROM labels
    WHERE issue_id = issues.id
      AND label = 'needs-review'
  )
ORDER BY priority, updated_at
LIMIT 1;
```

**What they do:**
1. Claim a gates-passed mission
2. **Extended thinking session** (3-5 minutes, deep analysis):
   - Read all files changed in mission branch
   - Review all commits in mission
   - Check coherence: Does this accomplish the mission goal?
   - Check completeness: Are there obvious gaps?
   - Check safety: Any security issues, breaking changes?
   - Check quality: Is code maintainable, well-tested?
3. Generate detailed review report:
   - Summary of changes
   - Assessment of completeness
   - Risks identified
   - Recommendation: APPROVE / REQUEST_CHANGES / REJECT
4. Create **review issue** (new beads issue):
   - Type: `review`
   - Title: `Review: Mission vc-XXX - [Title]`
   - Description: Full review report
   - Labels: `needs-human-approval`, `mission:vc-XXX`
   - Blocks: mission epic (until human approves)
5. Update mission:
   - Remove label `needs-review`
   - Add label `review-complete`
   - Add dependency: mission blocked by review issue

**Key insight:** GitOps arbiter is an **extended-thinking AI agent**, not a simple check!

#### Worker Type 4: Human Approvers (Final Gate)

**Claiming rule:** Manual (humans review review issues)

**What they do:**
1. See review issue: `vc-XXX: Review: Mission vc-300 - Add user authentication`
2. Read GitOps arbiter's review report
3. Check out mission branch: `git worktree add /tmp/review-300 mission/vc-300-user-auth`
4. Review changes: `git log`, `git diff main`, test locally
5. Decision:
   - **APPROVE**: Close review issue with approval
   - **REQUEST_CHANGES**: Add comment, create blocking tasks
   - **REJECT**: Close mission as `rejected`

#### Worker Type 5: GitOps Merger (Automated Merge)

**Claiming rule:**
```sql
SELECT id FROM issues
WHERE type = 'epic'
  AND subtype = 'mission'
  AND EXISTS (
    SELECT 1 FROM labels
    WHERE issue_id = issues.id
      AND label = 'approved'
  )
ORDER BY priority, updated_at
LIMIT 1;
```

**What they do:**
1. Claim approved mission
2. Merge mission branch to main:
   ```bash
   git checkout main
   git merge --no-ff mission/vc-300-user-auth -m "Mission vc-300: Add user authentication"
   git push origin main
   ```
3. Close mission epic with reason `merged`
4. Cleanup sandbox:
   ```bash
   git worktree remove .sandboxes/mission-300
   git branch -d mission/vc-300-user-auth
   ```
5. Log event: `mission_merged`

---

## Safety: Human Gate Instead of GitHub PRs

### Why NOT GitHub PRs Yet

From dogfooding run #15, we discovered a bug that caused VC to file **100k bogus issues** in the beads tracker. If we were auto-creating GitHub PRs, this would have:
- Spammed GitHub API (rate limited, possibly banned)
- Created 100k PRs (impossible to clean up)
- Destroyed credibility of the repo

**Instead:** Use beads review issues with human approval gate.

### The Review Issue Pattern

```
Issue: vc-567
Type: review
Title: "Review: Mission vc-300 - Add user authentication"
Status: open
Priority: P1
Labels: [needs-human-approval, mission:vc-300]

Description:
# GitOps Arbiter Review Report

## Mission Summary
- Epic: vc-300 "Add user authentication"
- Duration: 3h 45m
- Tasks completed: 15
- Discovered issues fixed: 8
- Quality gates: ✅ BUILD ✅ TEST ✅ LINT

## Changes Overview
- Files changed: 23
- Insertions: +1,247
- Deletions: -89
- Commits: 23

## Coherence Assessment (Confidence: 0.91)
This mission successfully implements OAuth2 authentication with Google as the
provider. All acceptance criteria met:
- ✅ OAuth2 configuration
- ✅ User session management
- ✅ Middleware integration
- ✅ Comprehensive tests

## Safety Review
- ✅ No credentials in code
- ✅ Environment variables for secrets
- ✅ HTTPS-only in production
- ⚠️ Rate limiting not implemented (nice-to-have)

## Recommendation: **APPROVE**

Branch: `mission/vc-300-user-auth`
Sandbox: `.sandboxes/mission-300/`

---

**Human Action Required:**
1. Review changes in sandbox
2. Test locally if needed
3. Approve or request changes
```

**Human approves** → Mission gets label `approved` → GitOps merger runs → Merged to main!

---

## Parallel Missions (Multiple Sandboxes)

VC can work on **multiple missions simultaneously** using separate sandboxes:

```
Mission vc-300 "Add user authentication"
  Sandbox: .sandboxes/mission-300/
  Branch: mission/vc-300-user-auth
  Workers: 3 code workers active
  Status: Phase 2 (6/15 tasks complete)

Mission vc-350 "Fix caching bug"
  Sandbox: .sandboxes/mission-350/
  Branch: mission/vc-350-caching-fix
  Workers: 1 code worker active
  Status: Phase 1 (2/4 tasks complete)

Mission vc-375 "Refactor parser"
  Sandbox: .sandboxes/mission-375/
  Branch: mission/vc-375-parser-refactor
  Workers: 0 (waiting on quality gates)
  Status: All tasks complete, gates running
```

**How workers choose which mission:**
1. Priority first (P0 before P1 before P2)
2. Within same priority: oldest first
3. Workers claim ANY ready task across ALL missions
4. Sandbox is determined by the mission the task belongs to

**Concurrency limits:**
- Max missions active: 5 (configurable)
- Max workers per mission: 3 (configurable)
- Max total workers: 10 (configurable)

---

## Self-Healing: Discovered Issues and Convergence

VC is **self-healing** through two mechanisms:

### 1. Discovered Issues During Task Execution

When AI analysis detects problems during task execution, it files **discovered issues**:

**Example: Worker Discovers Missing Test**
```
Worker executes: vc-303 "Implement OAuth flow"
Files changed: internal/auth/oauth.go

AI Analysis detects:
- ✅ OAuth flow implemented
- ✅ Error handling present
- ❌ No unit tests for OAuth flow
- ❌ No integration test for full auth cycle

AI files discovered issues:
  vc-320: "Write unit tests for OAuth flow" [task, P1]
    parent: vc-309 (Testing phase)
    discovered-from: vc-303

  vc-321: "Write integration test for auth cycle" [task, P1]
    parent: vc-309 (Testing phase)
    discovered-from: vc-303
```

Issues vc-320 and vc-321 become ready work → Workers fix before mission completes.

### 2. Quality Gate Failures Trigger Iterations

When quality gates fail, **Quality Gate Analyzer (AI)** decides escalation path:

**Example: Gates Detect Design Flaw**
```
Quality gates run on mission vc-300:
  BUILD: FAIL (40 errors across 8 files)
  TEST: FAIL (40% of tests failing)

Quality Gate Analyzer examines failures:
  "OAuth implementation missing TokenManager abstraction.
   Auth middleware expects TokenManager but it doesn't exist.
   This is a design gap requiring architectural fix."

Decision: MAJOR - needs re-design

Creates:
  vc-360: [epic] "Implement TokenManager abstraction layer"
    parent: vc-300 (mission)
    blocks: vc-300 (mission can't complete until this done)

AI Planner breaks down vc-360:
  vc-361: [task] "Design TokenManager interface"
  vc-362: [task] "Implement token validation logic"
  vc-363: [task] "Refactor auth handlers to use TokenManager"
  ... 3 more tasks

Code workers execute re-design → terminal state → gates again
```

**Convergence happens when:**
- ✅ **Complete**: All tasks done, gates pass, arbiter approves, human approves
- ⏸️ **Blocked**: Waiting on human decision
- ❌ **Failed**: Too many iterations (>10), no progress

**Mission completion requires:**
- All planned tasks: done
- All discovered tasks: done
- All blocking issues: resolved
- Quality gates: pass
- Then and only then: ready for arbiter review

---

## Terminal State Detection (The Critical Query)

### Epic Completion Check

**SQL query to check if mission epic is complete:**

```sql
-- Mission vc-300 is complete when ALL of these are true:

-- 1. All child tasks/phases are in terminal states
SELECT COUNT(*) = 0 AS all_children_done
FROM dependencies d
JOIN issues child ON d.issue_id = child.id
WHERE d.depends_on_id = 'vc-300'
  AND d.type = 'parent-child'
  AND child.status NOT IN ('closed', 'deferred');

-- 2. No blocking issues
SELECT COUNT(*) = 0 AS no_blockers
FROM dependencies d
JOIN issues blocker ON d.depends_on_id = blocker.id
WHERE d.issue_id = 'vc-300'
  AND d.type = 'blocks'
  AND blocker.status NOT IN ('closed');

-- 3. Mission itself is still open
SELECT status = 'open' AS mission_open
FROM issues
WHERE id = 'vc-300';
```

**Executor runs this check:**
- After every task completion
- Every poll interval (5 seconds)
- If all checks pass → add label `needs-quality-gates`

### Ready Work Query (For Code Workers)

**SQL query to find next ready task:**

```sql
SELECT
  i.id,
  i.title,
  i.priority,
  m.id AS mission_id,
  l.label AS sandbox_label
FROM issues i
-- Join to find parent mission
JOIN dependencies d ON i.id = d.issue_id
  AND d.type = 'parent-child'
JOIN issues m ON d.depends_on_id = m.id
  AND m.type = 'epic'
  AND m.subtype = 'mission'
-- Join to get sandbox label
JOIN labels l ON m.id = l.issue_id
  AND l.label LIKE 'sandbox:%'
WHERE
  -- Task is ready
  i.status = 'open'
  AND i.type = 'task'
  -- Mission is active (not waiting for gates/review)
  AND NOT EXISTS (
    SELECT 1 FROM labels ml
    WHERE ml.issue_id = m.id
      AND ml.label IN ('needs-quality-gates', 'needs-review', 'gates-failed')
  )
  -- No blocking dependencies
  AND NOT EXISTS (
    SELECT 1 FROM dependencies d2
    JOIN issues blocker ON d2.depends_on_id = blocker.id
    WHERE d2.issue_id = i.id
      AND d2.type = 'blocks'
      AND blocker.status NOT IN ('closed')
  )
ORDER BY i.priority, i.created_at
LIMIT 1;
```

**Returns:**
- Task ID to claim (e.g., `vc-303`)
- Mission ID it belongs to (e.g., `vc-300`)
- Sandbox to use (e.g., `.sandboxes/mission-300/`)

---

## Implementation Roadmap

### What Exists Today (Dogfooding Runs 1-16)

✅ **Working:**
- Executor claims ready work atomically
- AI supervision (assess + analyze)
- Agent execution (Claude Code in background)
- Quality gates (BUILD, TEST, LINT)
- Activity feed monitoring (`vc tail -f`)
- Graceful shutdown
- Watchdog convergence detection
- Issue discovery and filing

❌ **Missing:**
- Epic-centric workflow (workers don't scope to epics)
- Label-based claiming (workers claim by status only)
- Terminal state detection for epics
- Quality gate workers (gates run inline, not as workers)
- GitOps arbiter (no extended-thinking review)
- Review issues (no human approval gate)
- GitOps merger (no automated merge)
- Sandbox lifecycle (sandboxes created manually)
- Parallel missions (only one mission at a time)

### What We Need to Build

The missing pieces, broken into epics and tasks:

#### Epic 1: Epic-Centric Infrastructure (P0)

**vc-TBD: Epic-centric infrastructure**
- vc-TBD: Add mission/phase subtypes to epic type
- vc-TBD: Add `labels` table to storage layer (may exist)
- vc-TBD: Implement label-based claiming queries
- vc-TBD: Add terminal state detection query
- vc-TBD: Add `get_mission_for_task()` helper
- vc-TBD: Update executor to scope work to epics

**Success criteria:**
- Executor can query "is this epic complete?"
- Executor can find "next ready task in any active mission"
- Workers can look up "which mission am I working on?"

#### Epic 2: Sandbox Lifecycle (P0)

**vc-TBD: Sandbox lifecycle management**
- vc-TBD: Add `sandbox_path` and `branch_name` to issues table
- vc-TBD: Implement `create_sandbox(mission_id)` function
- vc-TBD: Implement `cleanup_sandbox(mission_id)` function
- vc-TBD: Add sandbox label when mission created
- vc-TBD: Update agent executor to use mission sandbox
- vc-TBD: Add sandbox cleanup on mission close

**Success criteria:**
- Mission creation auto-creates sandbox + branch
- All workers on a mission share same sandbox
- Mission closure auto-cleans up sandbox + branch

#### Epic 3: Label-Driven State Machine (P1)

**vc-TBD: Label-driven work claiming**
- vc-TBD: Add label helpers (add_label, remove_label, has_label)
- vc-TBD: Implement state transition: task complete → check epic complete → add `needs-quality-gates`
- vc-TBD: Implement quality gate worker (claims `needs-quality-gates`)
- vc-TBD: Implement state transition: gates pass → add `needs-review`
- vc-TBD: Implement GitOps arbiter worker (claims `needs-review`)
- vc-TBD: Implement state transition: review complete → add `needs-human-approval`

**Success criteria:**
- Mission flows through states automatically
- Each state has a worker type that claims it
- Labels drive which worker claims what

#### Epic 4: Quality Gate Workers (P1)

**vc-TBD: Quality gates as workers**
- vc-TBD: Extract quality gate logic to standalone worker
- vc-TBD: Implement quality gate claiming rule
- vc-TBD: Update gates to run in mission sandbox
- vc-TBD: Add gate failure → create blocking issue
- vc-TBD: Add gate success → transition to next state

**Success criteria:**
- Quality gates are workers, not inline checks
- Gates can run in parallel with code workers (on different missions)
- Gate failures create actionable blocking issues

#### Epic 5: GitOps Arbiter (P1)

**vc-TBD: GitOps arbiter (extended-thinking review)**
- vc-TBD: Design arbiter assessment prompt
- vc-TBD: Implement arbiter worker (claims `needs-review`)
- vc-TBD: Implement extended-thinking analysis (3-5min)
- vc-TBD: Implement review report generation
- vc-TBD: Implement review issue creation
- vc-TBD: Add mission blocking on review issue

**Success criteria:**
- Arbiter produces insightful review reports
- Review issues contain actionable information
- Human can approve/reject based on review

#### Epic 6: GitOps Merger (P2)

**vc-TBD: Automated merge on human approval**
- vc-TBD: Implement merger worker (claims `approved`)
- vc-TBD: Implement safe merge logic (--no-ff)
- vc-TBD: Implement post-merge cleanup
- vc-TBD: Add merge conflict detection + human escalation
- vc-TBD: Add rollback mechanism

**Success criteria:**
- Approved missions auto-merge to main
- Merge conflicts escalate to human
- Cleanup happens automatically

#### Epic 7: Parallel Missions (P2)

**vc-TBD: Support multiple concurrent missions**
- vc-TBD: Add concurrency limits (max missions, max workers)
- vc-TBD: Implement mission priority scheduling
- vc-TBD: Handle resource contention (disk, memory)
- vc-TBD: Add monitoring for parallel missions

**Success criteria:**
- VC can work on 5 missions simultaneously
- Workers distributed across missions by priority
- No resource exhaustion

#### Epic 8: Mission Planning (AI Planner) (P1)

**vc-TBD: AI planner translates requests to missions**
- vc-TBD: Design planner prompt (request → mission structure)
- vc-TBD: Implement planner worker (claims `needs-planning`)
- vc-TBD: Implement phased breakdown (mission → phases)
- vc-TBD: Implement task breakdown (phases → tasks)
- vc-TBD: Implement dependency creation (parent-child links)
- vc-TBD: Add acceptance criteria generation

**Success criteria:**
- User submits natural language request
- Planner creates mission epic with phases and tasks
- Tasks have clear acceptance criteria
- Dependencies correctly modeled

---

## SQL Schema: Pure Beads Primitives

### Design Philosophy

**We use ONLY beads primitives** - no custom executor tables.

**Why?**
- ✅ Simpler (fewer tables, fewer JOINs)
- ✅ Everything exports to JSONL (state in git)
- ✅ No schema coordination between beads and VC
- ✅ Easier testing (mock one store, not four tables)

### Required Tables (All from Beads)

```sql
-- Core beads tables (already exist)
CREATE TABLE issues (...);
CREATE TABLE dependencies (...);
CREATE TABLE comments (...);

-- Labels table (check if exists in beads, add if needed)
CREATE TABLE IF NOT EXISTS labels (
  id INTEGER PRIMARY KEY,
  issue_id TEXT NOT NULL,
  label TEXT NOT NULL,
  added_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
  added_by TEXT,
  FOREIGN KEY (issue_id) REFERENCES issues(id),
  UNIQUE(issue_id, label)
);

CREATE INDEX idx_labels_issue_id ON labels(issue_id);
CREATE INDEX idx_labels_label ON labels(label);

-- Activity feed (VC-specific, already exists)
CREATE TABLE agent_events (
  id INTEGER PRIMARY KEY,
  timestamp TIMESTAMP,
  issue_id TEXT,
  type TEXT,
  severity TEXT,
  message TEXT,
  data TEXT,  -- JSON blob
  FOREIGN KEY (issue_id) REFERENCES issues(id)
);
```

### New Columns for issues Table

**Only 3 columns added:**

```sql
ALTER TABLE issues ADD COLUMN subtype TEXT;       -- 'mission', 'phase', 'review'
ALTER TABLE issues ADD COLUMN sandbox_path TEXT;  -- '.sandboxes/mission-300/'
ALTER TABLE issues ADD COLUMN branch_name TEXT;   -- 'mission/vc-300-user-auth'
```

**That's it!** No executor_instances, no issue_execution_state.

### What We Eliminated

❌ **executor_instances table** - Not needed (watchdog handles orphans)
❌ **issue_execution_state table** - Not needed (status + labels + events)

✅ **Pure beads primitives** - issues, dependencies, labels, events

### State Storage Mapping

| State | Storage | Example |
|-------|---------|---------|
| Mission sandbox | issue.sandbox_path | `.sandboxes/mission-300` |
| Mission branch | issue.branch_name | `mission/vc-300-user-auth` |
| Mission lifecycle | labels | `needs-quality-gates`, `approved` |
| Task claiming | issue.status | `open` → `in_progress` → `closed` |
| Execution history | agent_events | `agent_spawned`, `quality_gates_failed` |
| Iteration count | labels | `iterations:4` |

### Label Conventions

**Mission lifecycle labels:**
- `mission` - This is a mission epic
- `sandbox:mission-XXX` - Points to sandbox directory
- `needs-quality-gates` - Ready for quality gate worker
- `gates-passed` - Quality gates passed
- `gates-failed` - Quality gates failed
- `has-fix-tasks` - Minor issues, fix tasks filed
- `needs-redesign` - Major issues, re-design epic created
- `blocked-on-human` - Escalated, needs human decision
- `needs-review` - Ready for GitOps arbiter
- `review-complete` - Arbiter completed review
- `needs-human-approval` - Review issue created, awaiting human
- `approved` - Human approved, ready for merge
- `merged` - Successfully merged to main

**Worker claiming labels:**
- Workers filter by presence/absence of labels
- Labels drive state transitions
- Labels enable parallel worker types

### Orphan Detection (No Executor Tracking)

**Watchdog runs every 5 minutes:**

```go
// Find stale in_progress issues
stale, err := store.ListIssues(ctx, &storage.ListOptions{
  Status: storage.StatusInProgress,
  UpdatedBefore: time.Now().Add(-30 * time.Minute),
})

for _, issue := range stale {
  log.Warnf("Issue %s stuck in_progress for 30+ min, resetting", issue.ID)

  // Reset to open
  store.UpdateIssue(ctx, issue.ID, map[string]interface{}{
    "status": "open",
  }, "watchdog")

  // Log event
  store.LogEvent(ctx, &AgentEvent{
    IssueID: issue.ID,
    Type: "orphan_detected",
    Severity: "warning",
    Message: "Issue was stuck in_progress, reset to open",
  })
}
```

**No executor_instances table needed!**

---

## Example Mission Lifecycle (End-to-End)

### T+0: User Request

```bash
$ ./vc
> "Add rate limiting to API endpoints"
```

### T+30s: AI Planner Creates Mission

```
Created:
  vc-400: [epic/mission] "Add rate limiting to API endpoints"
    labels: [mission, needs-planning]
    sandbox: .sandboxes/mission-400/
    branch: mission/vc-400-rate-limiting

Planner creates phases:
  vc-401: [epic/phase] "Phase 1: Rate limiter infrastructure"
  vc-402: [epic/phase] "Phase 2: API integration"
  vc-403: [epic/phase] "Phase 3: Testing and monitoring"

Planner creates tasks:
  vc-404: [task] "Implement token bucket algorithm"
  vc-405: [task] "Add Redis backend for distributed limiting"
  vc-406: [task] "Create rate limiter middleware"
  ... 12 more tasks ...

Labels updated:
  vc-400: [mission, sandbox:mission-400] (planning complete)
```

### T+1m: Code Worker 1 Claims First Task

```
Claimed: vc-404 "Implement token bucket algorithm"
Mission: vc-400
Sandbox: .sandboxes/mission-400/
Branch: mission/vc-400-rate-limiting

Agent spawned, working...
```

### T+8m: Task Complete, Analysis Runs

```
vc-404 completed
Analysis detects:
  ✅ Token bucket implemented
  ❌ No unit tests

Discovered issue filed:
  vc-420: [task] "Write unit tests for token bucket"
    parent: vc-403 (Testing phase)
    discovered-from: vc-404
```

### T+10m - T+2h: Workers Execute Tasks

```
vc-405 claimed → completed (8m)
vc-406 claimed → completed (12m)
vc-407 claimed → completed (6m)
... 9 more tasks ...
vc-420 claimed → completed (5m, discovered work)
... 3 more discovered tasks ...

Total: 15 planned + 4 discovered = 19 tasks completed
```

### T+2h: Terminal State Reached

```
Executor detects:
  - All children of vc-400: closed
  - No blockers on vc-400
  - vc-400 status: open

Terminal state reached!

Labels updated:
  vc-400: [mission, sandbox:mission-400, needs-quality-gates]
```

### T+2h 5m: Quality Gate Worker Claims Mission

```
Quality Gate Worker claimed: vc-400
Running gates in .sandboxes/mission-400/...

BUILD: ✅ PASS (20s)
TEST:  ✅ PASS (45s)
LINT:  ✅ PASS (8s)

All gates passed!

Labels updated:
  vc-400: [mission, sandbox:mission-400, gates-passed, needs-review]
```

### T+2h 10m: GitOps Arbiter Claims Mission

```
GitOps Arbiter claimed: vc-400
Extended thinking analysis (3-5 minutes)...

Reading files changed...
Reviewing commits...
Assessing coherence...
Checking safety...

Review report generated (confidence: 0.89)
Recommendation: APPROVE

Created review issue:
  vc-421: [review] "Review: Mission vc-400 - Add rate limiting"
    labels: [needs-human-approval, mission:vc-400]
    blocks: vc-400

Labels updated:
  vc-400: [mission, sandbox:mission-400, review-complete]
```

### T+2h 15m: Human Reviews

```
$ bd show vc-421

Issue: vc-421
Type: review
Title: "Review: Mission vc-400 - Add rate limiting"

[Full review report from arbiter]

Files changed: 18
Tests added: 23
Coverage: 94%
Recommendation: APPROVE

$ git worktree add /tmp/review-400 mission/vc-400-rate-limiting
$ cd /tmp/review-400
$ go test ./...
[all tests pass]

$ bd close vc-421 --reason "LGTM, approved"
```

### T+2h 20m: GitOps Merger Merges

```
GitOps Merger detected: vc-400 approved

Merging mission/vc-400-rate-limiting → main...
Merge commit: abc123f
Pushed to origin/main

Cleaning up:
  git worktree remove .sandboxes/mission-400
  git branch -d mission/vc-400-rate-limiting

Labels updated:
  vc-400: [mission, merged]

Status updated:
  vc-400: closed (reason: merged)

Mission complete! 🎉
```

---

## Success Metrics

### For Enabling Autonomous Missions

Before we trust VC to run unsupervised, we need:

| Metric | Target | Current |
|--------|--------|---------|
| Mission completion rate | >80% | N/A (not implemented) |
| Quality gate pass rate | >90% | ~85% (from dogfooding) |
| GitOps arbiter accuracy | >95% | N/A (not implemented) |
| Human approval rate | >90% | N/A (not implemented) |
| False positive reviews | <5% | N/A (not implemented) |
| Sandbox cleanup success | 100% | N/A (not implemented) |
| Parallel mission stability | 5 concurrent | 1 (dogfooding)

---

## Next Steps

### Immediate Actions (This Session)

1. **Review this design document** - Ensure vision is aligned
2. **Create implementation epics** - File the 8 epics outlined above in beads
3. **Break down Epic 1** - Create child tasks for epic-centric infrastructure
4. **Start building** - Begin with terminal state detection queries

### Phase 1: Foundation (Weeks 1-2)

**Build:**
- Epic-centric infrastructure (Epic 1)
- Sandbox lifecycle (Epic 2)
- Terminal state detection

**Validate:**
- Create a test mission manually
- Run workers on tasks within mission
- Verify terminal state detection works

### Phase 2: State Machine (Weeks 3-4)

**Build:**
- Label-driven state machine (Epic 3)
- Quality gate workers (Epic 4)

**Validate:**
- Mission flows from open → gates → blocked/pass
- Gate workers can run in parallel with code workers

### Phase 3: GitOps (Weeks 5-6)

**Build:**
- GitOps arbiter (Epic 5)
- GitOps merger (Epic 6)
- Review issue workflow

**Validate:**
- Arbiter produces quality reviews
- Human approval gates work
- Merge and cleanup automated

### Phase 4: Scale (Weeks 7-8)

**Build:**
- Parallel missions (Epic 7)
- AI planner (Epic 8)

**Validate:**
- Multiple missions run concurrently
- Planner creates well-structured missions
- System handles 5+ concurrent missions

### Phase 5: Production (Week 9+)

**Dogfood missions end-to-end:**
- User request → AI planner → workers → gates → review → merge
- Track metrics, tune thresholds
- Achieve >90% quality gate pass rate
- Enable autonomous mission execution

---

## Frequently Asked Questions

### Q: Why epics instead of simple tasks?

**A:** Epics provide the **scoping** and **terminal state** that VC needs:
- Workers know when they're "done" (epic complete, not global queue empty)
- Multiple missions can run in parallel (each epic is isolated)
- Quality gates run on coherent chunks (entire mission, not individual tasks)
- GitOps review analyzes meaningful PRs (one mission = one PR)

### Q: Why not just use GitHub PRs directly?

**A:** Safety. During dogfooding run #15, a bug caused 100k bogus issues to be filed in beads. If we were creating GitHub PRs automatically, we'd have:
- Rate limit violations
- API bans
- Irreversible spam

Review issues in beads give us the same workflow with a human safety gate, until we prove reliability.

### Q: What if a mission gets stuck forever?

**A:** Multiple safety mechanisms:
- Watchdog detects stuck states (no progress >30min)
- Humans can intervene and block missions
- Missions have timeout limits (configurable)
- Failed missions can be marked `deferred` or `rejected`

### Q: How do workers know which sandbox to use?

**A:** Every task is linked to a mission epic (via parent-child dependency). The mission epic has a label like `sandbox:mission-300` that encodes the sandbox path. Workers look up their task's parent mission and use that sandbox.

### Q: Can I run VC without missions (legacy mode)?

**A:** Yes! The current dogfooding workflow (task-by-task execution) will continue to work. Missions are opt-in. You can:
- Use VC for simple tasks (current mode)
- Use VC for missions (new mode, once implemented)
- Mix both (some work is missions, some is standalone tasks)

### Q: What about dependencies between missions?

**A:** Missions can block each other using `blocks` dependencies:
```
Mission vc-400: "Add rate limiting"
  blocks: Mission vc-450 "Add API v2"
  (v2 API needs rate limiting first)
```

Workers won't claim tasks from vc-450 until vc-400 is closed.

### Q: How does this relate to the REPL?

**A:** The REPL becomes the **user interface for missions**:
```
$ ./vc
> "Add user authentication"
[AI planner creates mission, phases, tasks]
VC: Created mission vc-500 with 3 phases and 18 tasks. Starting execution...
[Workers begin executing]

> status vc-500
Mission vc-500: Add user authentication
  Status: Phase 2 (12/18 tasks complete)
  Workers: 2 active
  ETA: ~45 minutes

> watch vc-500
[Live feed of mission progress]
```

The conversational interface hides the complexity, but missions are the underlying mechanism.

---

## Conclusion

**VC as a self-healing AI colony is achievable** with epic-centric architecture.

**Key insights:**
1. **Missions = Epics** - Each user request becomes a scoped epic
2. **Terminal state = Epic complete** - Not global queue empty
3. **Workers share sandboxes** - Sequential execution, one coherent PR
4. **Labels drive state** - Different worker types claim different work
5. **Quality gates are workers** - Not passive checks
6. **GitOps arbiter reviews** - Extended thinking before merge
7. **Human approval gate** - Safety until we prove reliability

**This document is the blueprint.** Next step: Create the epics and start building.

---

**Status**: Approved (2025-10-22)
**Design Evolution**: See MISSIONS_CONVERGENCE.md for the evolution from waterfall to convergence loop

**Key Design Decisions:**
1. ✅ **Convergence loop** - System iterates until convergence, not waterfall
2. ✅ **Pure beads primitives** - No custom executor tables, just issues + labels + events
3. ✅ **AI-driven escalation** - Quality Gate Analyzer decides minor/major/blocked paths
4. ✅ **Watchdog for orphans** - No executor tracking, just reset stale in_progress

**Next Steps:**
1. Create implementation epics in beads (8 epics outlined above)
2. Start with Epic 1: Epic-centric infrastructure
3. Build terminal state detection and label-based claiming

**Questions?** Comment on vc-26 or create a review issue.