# Mission Convergence: Iterative Self-Healing

**Status**: MERGED INTO MISSIONS.md (2025-10-22)

**Addendum to MISSIONS.md**
**Created**: 2025-10-22 (addressing waterfall concerns and schema minimalism)

**Note**: The key concepts from this document have been merged into MISSIONS.md. This document is kept for reference showing the design evolution from waterfall to convergence loop and from custom tables to pure beads primitives.

---

## The Problem with the Waterfall Presentation

The original "What Actually Happens" section in MISSIONS.md presents a **linear flow**:

```
Code workers → Terminal state → Quality gates → Arbiter → Human → Merge ✅
```

But this is **too simplistic**. Real missions iterate:

```
Code workers → Terminal state → Quality gates
  ↓
  Gates FAIL (major issues detected)
  ↓
  AI Analyzer escalates: "This needs re-design"
  ↓
  Create re-design epic, re-plan mission
  ↓
  Code workers execute new plan
  ↓
  Terminal state → Quality gates
  ↓
  Gates FAIL (minor issues detected)
  ↓
  AI Analyzer: "File 8 bug fix tasks"
  ↓
  Code workers fix bugs
  ↓
  Terminal state → Quality gates
  ↓
  Gates PASS ✅
  ↓
  Arbiter → Human → Merge
```

**This is convergence, not waterfall.**

---

## The Convergence Loop

### State Diagram

```
┌─────────────────────────────────────────────────────────────────┐
│                      MISSION EPIC CREATED                       │
│                  labels: [mission, sandbox:XXX]                 │
└────────────────────────────┬────────────────────────────────────┘
                             ↓
                    ┌────────────────┐
                    │ Code Workers   │
                    │ Execute Tasks  │
                    └───────┬────────┘
                            ↓
                   ┌────────────────────┐
                   │ Terminal State?    │
                   │ (all tasks done)   │
                   └─────┬──────────────┘
                         │ YES
                         ↓
              ┌──────────────────────┐
              │ Quality Gate Worker  │
              │ Runs BUILD/TEST/LINT │
              └──────┬───────────────┘
                     │
        ┌────────────┼────────────┐
        │            │            │
     PASS         FAIL:        FAIL:
     ↓           MINOR        MAJOR
┌────────┐    ┌────────┐   ┌─────────┐
│ Arbiter│    │ File   │   │ Escalate│
│ Review │    │ Tasks  │   │ Re-plan │
└───┬────┘    └───┬────┘   └────┬────┘
    ↓             │              │
┌────────┐        │              │
│ Human  │        └──────┬───────┘
│Approval│               │
└───┬────┘               │
    ↓                    │
┌────────┐               │
│ Merge  │               │
│Cleanup │               │
└────────┘               │
                         ↓
                 ┌───────────────┐
                 │ Code Workers  │◄──── LOOP BACK
                 │ Fix/Implement │
                 └───────────────┘
```

### Convergence Criteria

**A mission converges when ONE of these is true:**

1. ✅ **Complete**: All tasks done, gates pass, arbiter approves, human approves
2. ⏸️ **Blocked on Human**: Issues need architectural decision, risk too high, unclear requirements
3. ❌ **Failed**: Too many iterations (>10 loops), no progress being made, fundamental blocker

**The system iterates until convergence is reached.**

---

## Escalation Paths After Quality Gate Failures

When quality gates fail, the **Quality Gate Analyzer** (AI supervisor) examines failures and decides escalation path:

### Decision Tree

```
Quality Gates Run
  ├─ BUILD fails
  │   └─ Analyze: syntax errors, import issues, type errors
  │       ├─ Simple fixes (< 5 files affected)
  │       │   → File bug tasks, assign to current mission
  │       └─ Complex (many files, architecture issues)
  │           → Create re-design epic, block mission
  │
  ├─ TEST fails
  │   └─ Analyze: which tests, why failing, coverage gaps
  │       ├─ Minor (< 10% tests failing, logic bugs)
  │       │   → File bug tasks + test tasks
  │       ├─ Major (> 10% failing, wrong assumptions)
  │       │   → Create re-design epic
  │       └─ Unclear (flaky tests, race conditions)
  │           → Create blocking issue for human investigation
  │
  └─ LINT fails
      └─ Analyze: style issues, complexity, duplication
          ├─ Auto-fixable
          │   → File cleanup tasks
          └─ Design issues (high complexity, poor structure)
              → Create re-design epic or defer (not critical)
```

### Escalation Levels

**Level 1: File Tasks (Minor Issues)**
```
Quality Gate Analyzer detects:
  - 3 build errors in oauth.go
  - 5 test failures in auth_test.go
  - 12 lint warnings

Decision: MINOR - file tasks

Creates:
  vc-350: [task] "Fix build errors in OAuth implementation"
  vc-351: [task] "Fix failing auth tests (5 failures)"
  vc-352: [task] "Address lint warnings in auth package"

Labels mission: [mission, sandbox:XXX, gates-failed, has-fix-tasks]

Code workers claim these tasks, fix issues, loop back to terminal state.
```

**Level 2: Re-Design Epic (Major Issues)**
```
Quality Gate Analyzer detects:
  - 40% of tests failing
  - Fundamental design flaw: auth tokens not thread-safe
  - Affects 15 files across 3 packages

Decision: MAJOR - needs re-design

Creates:
  vc-360: [epic] "Re-design auth token management for thread safety"
    parent: vc-300 (original mission)
    blocks: vc-300 (mission can't complete until this done)

  vc-361: [task] "Design thread-safe token storage"
  vc-362: [task] "Implement token manager with mutex"
  vc-363: [task] "Refactor auth handlers to use token manager"
  ... more tasks ...

Labels mission: [mission, sandbox:XXX, gates-failed, needs-redesign]

AI Planner breaks down re-design epic into tasks.
Code workers execute re-design.
After re-design complete, original mission continues.
```

**Level 3: Block on Human (Unclear/Risky)**
```
Quality Gate Analyzer detects:
  - Security concern: OAuth tokens stored in plaintext
  - Performance issue: auth checks blocking for 500ms
  - Design decision: use JWT vs opaque tokens?

Decision: BLOCKED - needs human input

Creates:
  vc-370: [blocker] "Security review: token storage and validation"
    description: "AI detected security concerns and performance issues.
                  Needs human architectural decision on token strategy."
    blocks: vc-300 (mission)

Labels mission: [mission, sandbox:XXX, blocked-on-human]

Mission waits for human decision.
Human reviews, makes decision, creates follow-up tasks.
Mission unblocked, continues.
```

---

## Convergence Monitoring

### Iteration Count

Each time quality gates fail and the loop repeats, increment iteration counter:

```
Mission vc-300: Add user authentication
  Iteration 1: Gates fail (build errors) → 3 tasks filed → fixed
  Iteration 2: Gates fail (test failures) → 5 tasks filed → fixed
  Iteration 3: Gates fail (major design issue) → re-design epic created
  Iteration 4: Re-design complete, gates pass ✅

Total iterations: 4
```

**Convergence thresholds:**
- < 5 iterations: Normal, healthy iteration
- 5-10 iterations: Concerning, investigate why so many loops
- > 10 iterations: Abort, escalate to human (something fundamentally wrong)

### Progress Detection

**The system is making progress if:**
- Each iteration reduces gate failures
- Different failures each iteration (not stuck in same loop)
- Discovered work is being completed

**The system is stuck if:**
- Same failures every iteration (infinite loop)
- No tasks being completed (all attempts fail)
- No new insights from AI analysis (stuck in local minimum)

**Watchdog detects stuck missions:**
```
SELECT m.id, m.title, COUNT(e.id) as gate_attempts
FROM issues m
JOIN agent_events e ON m.id = e.issue_id
WHERE m.type = 'epic'
  AND m.subtype = 'mission'
  AND e.type = 'quality_gates_failed'
  AND e.timestamp > datetime('now', '-1 hour')
GROUP BY m.id
HAVING gate_attempts > 3;
```

If mission attempts gates 3+ times in 1 hour with no progress → escalate to human.

---

## Who Makes Escalation Decisions?

### Quality Gate Analyzer (AI Supervisor)

**After gates run, Quality Gate Analyzer:**

1. **Examines failures** (build errors, test failures, lint issues)
2. **Assesses impact** (how many files, which modules, severity)
3. **Estimates complexity** (simple fixes vs redesign needed)
4. **Decides escalation path:**
   - Minor → file tasks
   - Major → create re-design epic
   - Unclear → block on human

**Prompt template:**
```
You are the Quality Gate Analyzer for mission vc-300: "Add user authentication".

Quality gates have failed with the following results:

BUILD: FAIL
  - Error: undefined: oauth.TokenManager
  - Error: cannot use token (type string) as TokenInfo
  ... 15 more errors across 8 files

TEST: FAIL (40% failure rate)
  - TestOAuthFlow: FAIL (token validation fails)
  - TestAuthMiddleware: FAIL (panic: nil pointer)
  ... 12 more failures

LINT: PASS

Analyze these failures and decide escalation path:
1. MINOR (file tasks): Simple fixes, < 5 files, < 2 hours work
2. MAJOR (re-design epic): Fundamental issues, needs architectural changes
3. BLOCKED (human input): Unclear how to proceed, or risky changes

Provide:
- Assessment: What's wrong and why?
- Complexity: Simple, moderate, or complex?
- Recommended path: MINOR / MAJOR / BLOCKED
- If MINOR: List 3-5 tasks to file
- If MAJOR: Describe re-design epic needed
- If BLOCKED: What decision/input is needed from human?
```

**Example response:**
```json
{
  "assessment": "OAuth implementation is missing the TokenManager abstraction that was referenced but never created. This is a design gap - the auth middleware expects TokenManager but it doesn't exist.",

  "complexity": "moderate",

  "recommended_path": "MAJOR",

  "reasoning": "While the fixes themselves are straightforward (create TokenManager, update callers), this reveals a design oversight. We should re-design the token management layer properly rather than patching individual call sites.",

  "redesign_epic": {
    "title": "Implement TokenManager abstraction layer",
    "description": "Create proper TokenManager interface and implementation to centralize token validation, storage, and lifecycle management.",
    "estimated_tasks": 6,
    "estimated_duration": "3-4 hours"
  }
}
```

---

## Pure Beads Primitives: Eliminating Custom Tables

### The Question

In BEADS_INTEGRATION.md, I proposed adding executor-specific tables:
```sql
CREATE TABLE executor_instances (...);
CREATE TABLE issue_execution_state (...);
```

**But do we actually need these?**

### What State Do We Actually Track?

**For mission-centric workflow, we need:**

1. **Mission → sandbox mapping**: Where do workers work?
2. **Mission state**: What phase is the mission in?
3. **Task claiming**: Prevent duplicate work
4. **Execution history**: What happened?

**Can we do this with pure beads primitives (issues, dependencies, labels)?**

---

## Redesign: Pure Beads Primitives

### Use Cases and Solutions

#### 1. Mission → Sandbox Mapping

**Option A: Labels**
```
Mission epic vc-300 has labels:
  - mission
  - sandbox:mission-300
  - branch:mission/vc-300-user-auth
```

**Option B: Issue columns**
```sql
ALTER TABLE issues ADD COLUMN sandbox_path TEXT;
ALTER TABLE issues ADD COLUMN branch_name TEXT;

Mission epic vc-300:
  sandbox_path = ".sandboxes/mission-300"
  branch_name = "mission/vc-300-user-auth"
```

**Verdict**: **Option B** (columns) is cleaner for static data. Labels are better for dynamic state.

#### 2. Mission State (Lifecycle)

**Use labels:**
```
Mission progression through labels:
  [mission, sandbox:XXX]                    # Active, code workers claim tasks
  [mission, sandbox:XXX, needs-quality-gates]  # Terminal state reached
  [mission, sandbox:XXX, gates-running]     # Quality gate worker claimed
  [mission, sandbox:XXX, gates-failed, has-fix-tasks]  # Minor issues
  [mission, sandbox:XXX, gates-failed, needs-redesign] # Major issues
  [mission, sandbox:XXX, blocked-on-human]  # Escalated
  [mission, sandbox:XXX, gates-passed, needs-review]  # Ready for arbiter
  [mission, sandbox:XXX, review-complete, needs-human-approval]
  [mission, sandbox:XXX, approved]          # Ready for merge
  [mission, sandbox:XXX, merged]            # Complete
```

**Verdict**: Labels are perfect for state machine transitions.

#### 3. Task Claiming (Prevent Duplicate Work)

**Current approach (with executor tables):**
```sql
-- Claim issue
UPDATE issue_execution_state
SET executor_instance_id = 'exec-abc123', state = 'executing'
WHERE issue_id = 'vc-303';
```

**Pure beads approach:**
```sql
-- Claim issue atomically
UPDATE issues
SET status = 'in_progress', updated_at = CURRENT_TIMESTAMP
WHERE id = 'vc-303' AND status = 'open';

-- Add label for tracking (optional)
INSERT INTO labels (issue_id, label, added_by)
VALUES ('vc-303', 'claimed-at:2025-10-22T10:30:00Z', 'executor');
```

**Verdict**: Issue status is sufficient. Labels can add metadata if needed.

#### 4. Execution History (What Happened?)

**Use agent_events table (already exists):**
```sql
CREATE TABLE agent_events (
  id INTEGER PRIMARY KEY,
  timestamp TIMESTAMP,
  issue_id TEXT,
  type TEXT,  -- 'agent_spawned', 'agent_completed', 'quality_gates_failed', etc.
  severity TEXT,
  message TEXT,
  data TEXT,  -- JSON blob with details
  FOREIGN KEY (issue_id) REFERENCES issues(id)
);
```

**Verdict**: Activity feed already captures execution history. No need for separate execution_state table.

#### 5. Executor Instance Tracking (Orphan Detection)

**Problem**: If executor crashes, issues stay in 'in_progress' forever.

**Option A: Executor instances as issues**
```
Create issue:
  vc-exec-abc123: [type: executor-instance]
    status: running
    updated_at: 2025-10-22T10:35:00Z (heartbeat)

Orphan detection:
  SELECT i.id FROM issues i
  LEFT JOIN labels l ON i.id = l.issue_id AND l.label LIKE 'claimed-by:%'
  LEFT JOIN issues e ON l.label = 'claimed-by:' || e.id
  WHERE i.status = 'in_progress'
    AND (e.status != 'running' OR e.updated_at < datetime('now', '-5 minutes'));
```

**Option B: Heartbeat file**
```bash
# Executor writes heartbeat
echo "$(date +%s)" > .vc/executor-heartbeat

# On startup, check staleness
if [ $(( $(date +%s) - $(cat .vc/executor-heartbeat) )) -gt 300 ]; then
  # Stale (>5min), reset orphaned issues
  sqlite3 .beads/vc.db "UPDATE issues SET status = 'open' WHERE status = 'in_progress';"
fi
```

**Option C: Watchdog resets stale claims**
```
Watchdog runs every 5 minutes:
  SELECT id FROM issues
  WHERE status = 'in_progress'
    AND updated_at < datetime('now', '-30 minutes');

  For each stale issue:
    Log warning: "Issue vc-X stuck in_progress for 30+ min, resetting"
    UPDATE issues SET status = 'open' WHERE id = 'vc-X';
```

**Verdict**: **Option C** (watchdog) is simplest. No executor tracking needed.

---

## Minimal Schema: What We Actually Need

### Tables (All from Beads)

```sql
-- Core beads tables (already exist)
CREATE TABLE issues (...);           -- From beads
CREATE TABLE dependencies (...);     -- From beads
CREATE TABLE comments (...);         -- From beads

-- Labels table (may need to add to beads)
CREATE TABLE labels (
  id INTEGER PRIMARY KEY,
  issue_id TEXT NOT NULL,
  label TEXT NOT NULL,
  added_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
  added_by TEXT,
  FOREIGN KEY (issue_id) REFERENCES issues(id),
  UNIQUE(issue_id, label)
);

-- Activity feed (VC-specific, already exists)
CREATE TABLE agent_events (
  id INTEGER PRIMARY KEY,
  timestamp TIMESTAMP,
  issue_id TEXT,
  type TEXT,
  severity TEXT,
  message TEXT,
  data TEXT,  -- JSON
  FOREIGN KEY (issue_id) REFERENCES issues(id)
);
```

### Columns to Add to issues Table

```sql
ALTER TABLE issues ADD COLUMN subtype TEXT;       -- 'mission', 'phase', 'review', 'executor-instance'
ALTER TABLE issues ADD COLUMN sandbox_path TEXT;  -- '.sandboxes/mission-300/'
ALTER TABLE issues ADD COLUMN branch_name TEXT;   -- 'mission/vc-300-user-auth'
```

**That's it!** No executor_instances, no issue_execution_state.

### What We Eliminated

❌ **executor_instances table** - Not needed (watchdog handles orphans)
❌ **issue_execution_state table** - Not needed (status + labels + events)

✅ **Pure beads primitives** - issues, dependencies, labels, events

---

## Benefits of Pure Beads Approach

### 1. Simplicity

**Before** (custom tables):
```go
// Claim issue - requires 3 table updates
tx.Begin()
tx.Exec("UPDATE issues SET status = 'in_progress' WHERE id = ?", issueID)
tx.Exec("INSERT INTO issue_execution_state VALUES (?, ?, ?)", issueID, execID, "executing")
tx.Exec("UPDATE executor_instances SET last_heartbeat = ? WHERE id = ?", time.Now(), execID)
tx.Commit()
```

**After** (pure beads):
```go
// Claim issue - single update
store.UpdateIssue(ctx, issueID, map[string]interface{}{
  "status": "in_progress",
}, "executor")
```

### 2. JSONL Export Includes Everything

**Before**: executor_instances and issue_execution_state in SQLite only, not in JSONL
**After**: All state in issues + labels → exports to JSONL → in git ✅

### 3. No Schema Coordination

**Before**: Beads repo and VC repo both have to coordinate schema changes
**After**: VC just uses beads tables, adds a few columns

### 4. Easier Testing

**Before**: Mock executor_instances, issue_execution_state, issues, dependencies...
**After**: Mock issues store, done

---

## Revised Architecture

### State Storage

| State | Storage | Example |
|-------|---------|---------|
| Mission sandbox | issue.sandbox_path | `.sandboxes/mission-300` |
| Mission branch | issue.branch_name | `mission/vc-300-user-auth` |
| Mission lifecycle | labels | `needs-quality-gates`, `approved` |
| Task claiming | issue.status | `open` → `in_progress` → `closed` |
| Execution history | agent_events | `agent_spawned`, `quality_gates_failed` |
| Iteration count | labels | `iterations:4` |
| Claimed timestamp | labels | `claimed-at:2025-10-22T10:30:00Z` |

### Orphan Detection

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

No executor tracking needed!

---

## Summary

### Key Changes from Original Design

1. **Convergence loop** - not waterfall, system iterates until convergence
2. **Escalation paths** - AI decides: minor tasks, major redesign, or block on human
3. **Pure beads primitives** - eliminate executor_instances and issue_execution_state tables
4. **Labels for state** - mission lifecycle driven by labels
5. **Watchdog for orphans** - no need to track executor instances

### What We Need

**Tables** (all from beads):
- issues (with subtype, sandbox_path, branch_name columns)
- dependencies
- labels
- agent_events

**That's it!** Clean, simple, pure beads.

---

**Next steps:**
1. Review this convergence design
2. Update MISSIONS.md with convergence loop
3. Confirm pure beads approach
4. Create implementation epics