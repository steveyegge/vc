# VC-Gastown Integration Design

**Status**: Draft
**Author**: Steve Yegge + Claude
**Date**: 2025-11-25

---

## Executive Summary

This document describes how VC (VibeCoder) integrates with Gastown to provide quality-assured coding agent execution. VC becomes the "execution engine" inside Gastown polecats, handling the iteration, verification, and decomposition that raw coding agents lack.

**Key insight**: Gastown handles coordination (clones, mail, Boss). VC handles quality (preflight, iteration, gates). They're complementary, not competing.

---

## 1. Problem Statement

### 1.1 The 85% Problem

Raw coding agents (Claude Code, Amp) have a structural limitation: they operate in a single context window with no iteration loop.

This manifests as:
- **No verification**: Agent says "done" but who checks if tests pass?
- **No iteration**: Agent gets to ~85% and stops. Edge cases missed.
- **No decomposition**: Large tasks degrade performance as context fills
- **No recovery**: If baseline is broken, agent starts from broken state
- **No persistence**: Agent forgets everything between sessions

These aren't going to be fixed by bigger context windows or smarter models. They're architectural gaps.

### 1.2 What Gastown Provides

Gastown solves the **coordination problem** for multi-agent development:
- **Isolated clones**: Each polecat gets full git clone with dedicated branch
- **Message-based coordination**: Agents communicate via `gm` (Gastown Mail)
- **Shared issue tracking**: Beads syncs via git
- **Boss orchestration**: Routes work to idle polecats

### 1.3 What Gastown Doesn't Provide

Gastown doesn't address **per-agent quality**:
- No preflight checks (is baseline healthy before starting?)
- No quality gates (did tests pass after execution?)
- No iterative refinement (is this really done?)
- No task decomposition (should we break this up?)

This is exactly what VC provides.

---

## 2. Integration Model

### 2.1 Architecture Overview

```
┌─────────────────────────────────────────────────────────────┐
│                         GASTOWN                              │
│  ┌─────────┐    ┌─────────┐    ┌─────────┐    ┌─────────┐   │
│  │  Boss   │    │ Polecat │    │ Polecat │    │ Polecat │   │
│  │         │    │GreenLake│    │ BlueDog │    │RedStone │   │
│  │ Routes  │    │         │    │         │    │         │   │
│  │  Work   │    │ ┌─────┐ │    │ ┌─────┐ │    │ ┌─────┐ │   │
│  │   via   │───▶│ │ VC  │ │    │ │ VC  │ │    │ │ VC  │ │   │
│  │   gm    │    │ │Exec │ │    │ │Exec │ │    │ │Exec │ │   │
│  │         │    │ └─────┘ │    │ └─────┘ │    │ └─────┘ │   │
│  └─────────┘    └─────────┘    └─────────┘    └─────────┘   │
│                      │              │              │         │
│                      ▼              ▼              ▼         │
│                 ┌─────────────────────────────────────┐     │
│                 │           Shared Main Branch         │     │
│                 │     (via git merge from polecats)    │     │
│                 └─────────────────────────────────────┘     │
└─────────────────────────────────────────────────────────────┘
```

### 2.2 Component Responsibilities

| Component | Responsibility |
|-----------|---------------|
| **Boss** | Routes work to idle polecats via gm messages |
| **Gastown Mail (gm)** | Inter-polecat communication, work delegation |
| **Polecat Clone** | Isolated git workspace with dedicated branch |
| **VC Executor** | Quality-assured execution within polecat |
| **Beads** | Issue tracking, synced via git |

### 2.3 Execution Flow

```
1. Boss sends work via gm
   └─▶ gm send GreenLake -s "bd-123: Add auth" -m "Implement OAuth2 login"

2. Polecat receives message
   └─▶ gm inbox (or hook injects into Claude Code)

3. Polecat invokes VC executor
   └─▶ vc exec --polecat-mode --task "Implement OAuth2 login"

4. VC executes with quality loop:
   ┌─▶ Preflight: Is baseline healthy?
   │   └─▶ If not, self-heal or escalate
   │
   ├─▶ Assessment: Should we decompose?
   │   └─▶ If yes, create subtasks, execute sequentially
   │
   ├─▶ Execute: Spawn Claude Code/Amp
   │   └─▶ Agent does the actual coding work
   │
   ├─▶ Iterate: Is this really done?
   │   └─▶ AI convergence check (3-7 passes)
   │   └─▶ If not converged, refine and retry
   │
   └─▶ Quality Gates: Tests pass? Lint pass? Build works?
       └─▶ If not, retry or escalate

5. VC returns structured result
   └─▶ JSON to stdout with status, files, discovered issues

6. Polecat handles result:
   └─▶ Creates discovered issues in beads
   └─▶ Merges to main if gates passed
   └─▶ Replies via gm: "Done, merged to main"

7. Boss receives completion
   └─▶ Routes next work or marks mission complete
```

---

## 3. VC Polecat Mode

### 3.1 Mode Definition

VC gains a new execution mode optimized for running inside Gastown polecats:

```go
type ExecutionMode string

const (
    ModeExecutor = "executor"   // Full polling loop (current behavior)
    ModePolecat  = "polecat"    // Single-task execution (new)
)
```

### 3.2 Polecat Mode Characteristics

| Aspect | Executor Mode (Current) | Polecat Mode (New) |
|--------|------------------------|-------------------|
| **Work source** | Polls beads for ready issues | Accepts task from args/stdin |
| **Sandbox** | Creates git worktree | Uses polecat's clone/branch |
| **Concurrency** | Tracks executor instances | Single instance, no tracking |
| **Output** | Updates beads, emits events | JSON result to stdout |
| **Loop** | Continuous polling | Single execution, exits |

### 3.3 CLI Interface

```bash
# Execute a task (natural language)
vc exec --polecat-mode --task "Implement user authentication with OAuth2"

# Execute from a beads issue
vc exec --polecat-mode --issue bd-123

# Execute with lite mode (skip assessment/decomposition)
vc exec --polecat-mode --lite --task "Fix typo in README"

# Execute with specific config
vc exec --polecat-mode --config /path/to/vc.yaml --task "..."
```

### 3.4 Input Specification

**Via --task flag:**
```bash
vc exec --polecat-mode --task "Implement OAuth2 login flow"
```

**Via stdin (for longer task descriptions):**
```bash
cat <<EOF | vc exec --polecat-mode --stdin
Task: Implement OAuth2 login flow

Requirements:
- Support Google and GitHub providers
- Store tokens securely
- Handle refresh token rotation

Acceptance Criteria:
- Users can log in via OAuth2
- Tokens are encrypted at rest
- Refresh works transparently
EOF
```

**Via beads issue:**
```bash
vc exec --polecat-mode --issue bd-123
```

### 3.5 Output Specification

VC outputs structured JSON to stdout:

```json
{
  "status": "completed",
  "success": true,
  "iterations": 3,
  "converged": true,
  "duration_seconds": 245,

  "files_modified": [
    "internal/auth/oauth.go",
    "internal/auth/oauth_test.go",
    "internal/config/providers.go"
  ],

  "quality_gates": {
    "test": {"passed": true, "output": "ok  ./... 2.345s"},
    "lint": {"passed": true, "output": ""},
    "build": {"passed": true, "output": ""}
  },

  "discovered_issues": [
    {
      "title": "Add rate limiting to OAuth endpoints",
      "description": "OAuth endpoints should have rate limiting to prevent abuse",
      "type": "task",
      "priority": 2
    },
    {
      "title": "Token encryption uses deprecated algorithm",
      "description": "Found AES-128-CBC usage, should upgrade to AES-256-GCM",
      "type": "bug",
      "priority": 1
    }
  ],

  "punted_items": [
    "Password reset flow - requires email service integration",
    "Multi-factor authentication - out of scope for this task"
  ],

  "summary": "Implemented OAuth2 login with Google and GitHub providers. Tokens stored with encryption. All tests passing.",

  "error": null
}
```

**Status values:**
- `completed` - Task done, gates passed
- `partial` - Some work done, but incomplete
- `blocked` - Cannot proceed (dependency, unclear requirements)
- `failed` - Execution failed (gates failed, agent crashed)
- `decomposed` - Task was too large, subtasks created

---

## 4. Components to Keep

### 4.1 Preflight Checker

**Purpose**: Verify baseline is healthy before starting work.

**Current implementation**: `internal/executor/preflight.go`

**Behavior in polecat mode**:
- Runs test/lint/build on polecat's current branch
- If baseline fails:
  - **Option A**: Self-heal (create fix, verify, continue)
  - **Option B**: Return `status: "blocked"` with details
- Caches baseline results by git commit hash (5-minute TTL)

**Why keep it**: Prevents cascading failures. Agent shouldn't start work on broken code.

### 4.2 Quality Gates

**Purpose**: Verify work is correct after execution.

**Current implementation**: `internal/gates/`

**Behavior in polecat mode**:
- Runs test/lint/build after agent execution
- Parallel execution with 5-minute timeout
- Returns detailed results per gate

**Why keep it**: Raw agents don't verify their work. This catches bugs before merge.

### 4.3 AI Supervisor

**Purpose**: Assessment, analysis, and convergence detection.

**Current implementation**: `internal/ai/supervisor.go`, `internal/ai/assessment.go`, `internal/ai/analysis.go`

**Behavior in polecat mode**:
- **Assessment** (optional): Evaluate task, determine if decomposition needed
- **Analysis**: Parse agent output, extract discovered issues
- **Convergence**: Determine if refinement is complete

**Why keep it**: This is the brain that catches the 15% agents miss.

### 4.4 Iterative Refinement

**Purpose**: Keep refining until AI says "done."

**Current implementation**: `internal/iterative/converge.go`

**Behavior in polecat mode**:
- Min 3, max 7 iterations
- AI checks convergence after each pass
- Stops when confident work is complete

**Why keep it**: This is the core fix for the 85% problem.

### 4.5 Task Decomposition

**Purpose**: Break large tasks into focused subtasks.

**Current implementation**: `internal/ai/decomposition.go`

**Behavior in polecat mode**:
- Assessment phase detects "too large" tasks
- AI creates decomposition plan with subtasks
- Execute subtasks sequentially within same VC invocation
- Return aggregate result

**Why keep it**: Large tasks degrade agent performance. Decomposition improves focus and enables cost optimization.

---

## 5. Components to Remove or Simplify

### 5.1 Sandbox Manager (Remove)

**Current**: Creates git worktrees for isolated execution.

**Why remove**: Gastown clones already provide isolation. Each polecat IS a sandbox.

**Migration**:
- VC works directly in polecat's clone
- Commits to polecat's local branch
- Gastown handles merge to main

### 5.2 Issue Claiming/Polling (Remove)

**Current**: Executor polls beads for ready issues, claims atomically.

**Why remove**: In polecat mode, work arrives explicitly via gm or CLI args.

**Migration**:
- No polling loop in polecat mode
- Task provided via `--task` or `--issue` flag
- Single execution, then exit

### 5.3 Executor Instance Tracking (Remove)

**Current**: Tracks multiple executor instances, detects orphans.

**Why remove**: One VC per polecat, no concurrency concerns.

**Migration**:
- No instance registration
- No orphan detection needed
- Simpler state model

### 5.4 Activity Feed Storage (Simplify)

**Current**: Stores events in SQLite `agent_events` table.

**Why simplify**: Just log to stdout/stderr. Gastown can capture if needed.

**Migration**:
- Events logged as structured JSON to stderr
- No persistent storage in polecat mode
- External monitoring can capture stderr

### 5.5 Result Processor (Simplify)

**Current**: 1700+ lines handling many code paths.

**Why simplify**: Polecat mode has simpler flow - no status updates, no human approval gate.

**Migration**:
- Linear flow: assess → execute → iterate → gates → output
- Return JSON result
- Let polecat wrapper handle beads updates

---

## 6. Lite Mode

### 6.1 Purpose

For trivial tasks, full VC pipeline is overkill. Lite mode provides fast path:

```bash
vc exec --polecat-mode --lite --task "Fix typo in README"
```

### 6.2 What Lite Mode Skips

| Component | Full Mode | Lite Mode |
|-----------|-----------|-----------|
| Preflight | ✅ Run | ⚡ Skip (assume clean) |
| Assessment | ✅ Run | ⚡ Skip |
| Decomposition | ✅ If needed | ⚡ Skip |
| Execution | ✅ Run | ✅ Run |
| Iteration | ✅ 3-7 passes | ⚡ 1 pass |
| Quality Gates | ✅ Run | ✅ Run (always verify) |

### 6.3 When to Use Lite Mode

- Typo fixes
- Simple documentation updates
- Single-file changes with obvious scope
- Tasks under ~30 lines of code

### 6.4 Polecat Wrapper Decision

The polecat wrapper (or Claude Code hook) can decide:

```python
def should_use_lite_mode(task: str) -> bool:
    """Heuristic for lite mode selection."""
    lite_keywords = ["typo", "fix comment", "update readme", "rename"]
    task_lower = task.lower()

    for keyword in lite_keywords:
        if keyword in task_lower:
            return True

    # Could also check task length, complexity markers, etc.
    return False
```

---

## 7. Polecat Wrapper Integration

### 7.1 Wrapper Responsibilities

The polecat wrapper (Python/shell script in Gastown) handles:

1. **Receive work** via gm or Claude Code
2. **Invoke VC** with appropriate mode
3. **Parse result** JSON from VC
4. **Create discovered issues** in beads
5. **Merge to main** if gates passed
6. **Reply via gm** with completion status

### 7.2 Example Wrapper (Pseudocode)

```python
#!/usr/bin/env python3
"""Polecat wrapper that invokes VC for quality-assured execution."""

import json
import subprocess
from gastown import gm, beads

def handle_task(task: str, message_id: str = None):
    """Execute task via VC and handle result."""

    # Decide mode
    lite = should_use_lite_mode(task)

    # Build command
    cmd = ["vc", "exec", "--polecat-mode"]
    if lite:
        cmd.append("--lite")
    cmd.extend(["--task", task])

    # Execute VC
    result = subprocess.run(cmd, capture_output=True, text=True)

    if result.returncode != 0:
        # VC execution failed
        if message_id:
            gm.reply(message_id, f"Execution failed: {result.stderr}")
        return

    # Parse result
    vc_result = json.loads(result.stdout)

    # Create discovered issues
    for issue in vc_result.get("discovered_issues", []):
        beads.create_issue(
            title=issue["title"],
            description=issue["description"],
            type=issue["type"],
            priority=issue["priority"],
            labels=["discovered:related"]
        )

    # Merge if successful
    if vc_result["status"] == "completed" and vc_result["success"]:
        # Commit to local branch
        subprocess.run(["git", "add", "-A"])
        subprocess.run(["git", "commit", "-m", f"VC: {task[:50]}"])

        # Merge to main
        subprocess.run(["git", "checkout", "main"])
        subprocess.run(["git", "merge", "-"])
        subprocess.run(["git", "push", "origin", "main"])

        # Return to polecat branch
        subprocess.run(["git", "checkout", "-"])

    # Reply via gm
    if message_id:
        status = "completed" if vc_result["success"] else "failed"
        gm.reply(message_id, f"Task {status}: {vc_result['summary']}")

if __name__ == "__main__":
    # Could be invoked from gm hook or directly
    import sys
    task = sys.argv[1] if len(sys.argv) > 1 else None
    if task:
        handle_task(task)
```

### 7.3 Claude Code Hook Integration

Polecat's Claude Code can have a hook that:

1. Receives user prompt
2. Determines if task should go through VC
3. Invokes VC in polecat mode
4. Presents result to user

```bash
# .claude/hooks/pre-prompt.sh
#!/bin/bash
# Check if task should use VC

task="$1"

# Simple heuristic: multi-file or complex tasks use VC
if echo "$task" | grep -qE "(implement|refactor|add feature|fix bug)"; then
    echo "Routing to VC for quality-assured execution..."
    vc exec --polecat-mode --task "$task"
    exit 0
fi

# Simple tasks go direct to Claude Code
exit 1  # Continue with normal Claude Code execution
```

---

## 8. Configuration

### 8.1 Polecat Mode Config File

VC can be configured via YAML in polecat mode:

```yaml
# ~/ai/myproject/GreenLake/.vc/config.yaml

mode: polecat

preflight:
  enabled: true
  failure_mode: block  # block | warn | ignore
  cache_ttl: 5m

assessment:
  enabled: true
  decomposition_threshold: 30  # minutes

iteration:
  min_iterations: 3
  max_iterations: 7
  convergence_confidence: 0.85

gates:
  test:
    enabled: true
    command: "go test ./..."
    timeout: 5m
  lint:
    enabled: true
    command: "golangci-lint run ./..."
    timeout: 2m
  build:
    enabled: true
    command: "go build ./..."
    timeout: 3m

lite_mode:
  skip_preflight: true
  skip_assessment: true
  max_iterations: 1
```

### 8.2 Environment Variables

```bash
# AI configuration
export ANTHROPIC_API_KEY=sk-...

# VC polecat mode defaults
export VC_POLECAT_MODE=true
export VC_LITE_MODE=false
export VC_SKIP_PREFLIGHT=false

# Gate configuration
export VC_GATES_TIMEOUT=5m
export VC_TEST_COMMAND="go test ./..."
export VC_LINT_COMMAND="golangci-lint run ./..."
```

---

## 9. Error Handling

### 9.1 Preflight Failures

When preflight detects broken baseline:

```json
{
  "status": "blocked",
  "success": false,
  "error": "preflight_failed",
  "preflight_result": {
    "test": {"passed": false, "output": "TestAuth failed: ..."},
    "lint": {"passed": true},
    "build": {"passed": true}
  },
  "message": "Baseline tests failing. Fix required before work can proceed.",
  "suggested_action": "Run 'vc heal' to attempt self-healing"
}
```

### 9.2 Gate Failures

When quality gates fail after execution:

```json
{
  "status": "failed",
  "success": false,
  "error": "gates_failed",
  "quality_gates": {
    "test": {"passed": false, "output": "TestNewFeature failed: ..."},
    "lint": {"passed": true},
    "build": {"passed": true}
  },
  "iterations": 3,
  "message": "Tests failing after 3 iterations. Manual intervention needed."
}
```

### 9.3 Decomposition Required

When task is too large:

```json
{
  "status": "decomposed",
  "success": true,
  "decomposition": {
    "reasoning": "Task estimated at 120 minutes, breaking into subtasks",
    "subtasks": [
      {"title": "Implement OAuth2 provider interface", "priority": 0},
      {"title": "Add Google OAuth provider", "priority": 1},
      {"title": "Add GitHub OAuth provider", "priority": 1},
      {"title": "Implement token storage", "priority": 2}
    ]
  },
  "message": "Task decomposed into 4 subtasks. Executing sequentially..."
}
```

---

## 10. Migration Path

### 10.1 Phase 1: Add Polecat Mode (Non-Breaking)

- Add `--polecat-mode` flag to `vc exec`
- Implement single-task execution flow
- Output JSON to stdout
- Keep all existing executor mode behavior

**Deliverable**: VC can run in polecat mode alongside existing executor mode.

### 10.2 Phase 2: Simplify Internals

- Remove sandbox manager (behind feature flag)
- Simplify result processor for polecat mode
- Remove instance tracking in polecat mode

**Deliverable**: Cleaner codebase, reduced complexity.

### 10.3 Phase 3: Gastown Integration

- Create polecat wrapper in Gastown
- Integrate with gm hooks
- Test end-to-end workflow

**Deliverable**: Polecats use VC for quality-assured execution.

### 10.4 Phase 4: Lite Mode

- Implement lite mode shortcuts
- Add heuristics for mode selection
- Optimize for trivial tasks

**Deliverable**: Fast path for simple work.

---

## 11. Success Metrics

### 11.1 Quality Metrics

| Metric | Target | Measurement |
|--------|--------|-------------|
| Gate pass rate | >95% | Successful executions / total |
| Iteration convergence | 80% converge by iteration 5 | Average iterations to convergence |
| Discovered issues captured | >90% | Manual audit of agent output |

### 11.2 Performance Metrics

| Metric | Target | Measurement |
|--------|--------|-------------|
| Full mode overhead | <60s | Time added vs. raw agent |
| Lite mode overhead | <10s | Time added vs. raw agent |
| Preflight cache hit rate | >80% | Cache hits / total preflight checks |

### 11.3 Integration Metrics

| Metric | Target | Measurement |
|--------|--------|-------------|
| Polecat task success rate | >90% | Successful completions / total tasks |
| Merge success rate | >95% | Clean merges / total merges |
| Message response time | <5min | Time from gm receive to reply |

---

## 12. Open Questions

### 12.1 Decomposition Execution Model

**Question**: When VC decomposes a task, should it:
- A) Execute subtasks sequentially within same invocation
- B) Return subtask list and let polecat orchestrate
- C) Create beads issues and exit (Boss routes subtasks)

**Recommendation**: Option A for initial implementation. Keeps execution atomic. Can evolve to B/C if needed for parallelism.

### 12.2 Cross-Polecat Work Discovery

**Question**: When VC discovers issues during execution, should it:
- A) Create beads issues in local clone (current polecat handles)
- B) Send gm messages for cross-polecat delegation
- C) Return in JSON and let wrapper decide

**Recommendation**: Option C. Keep VC focused on execution. Let wrapper/Boss decide routing.

### 12.3 Preflight Self-Healing Scope

**Question**: How aggressive should preflight self-healing be?
- A) Never self-heal, just report
- B) Self-heal obvious failures (test flakes, missing deps)
- C) Full self-healing with AI guidance

**Recommendation**: Option B for polecat mode. Full self-healing is complex and may modify code unexpectedly. Let polecat/Boss handle complex baseline fixes.

### 12.4 Beads Database Location

**Question**: Should VC in polecat mode:
- A) Use polecat's `.beads/beads.db`
- B) Use `:memory:` database (stateless)
- C) Not use beads at all (output-only)

**Recommendation**: Option C for simplest integration. VC outputs JSON, polecat wrapper handles beads. Avoids database conflicts and simplifies VC's role.

---

## 13. Appendix

### 13.1 Related Documents

- [Gastown README](https://github.com/steveyegge/gastown)
- [VC Architecture](../ARCHITECTURE_AUDIT.md)
- [Iterative Refinement](../ITERATIVE_REFINEMENT.md)
- [Quality Gates](../FEATURES.md#quality-gates)

### 13.2 Glossary

| Term | Definition |
|------|------------|
| **Polecat** | Long-lived agent worker in Gastown with dedicated git clone |
| **Boss** | Meta-agent that routes work to polecats |
| **gm** | Gastown Mail - messaging system for polecat coordination |
| **Preflight** | Quality check before starting work |
| **Quality Gates** | test/lint/build verification after execution |
| **Convergence** | AI determination that work is complete |
| **Decomposition** | Breaking large tasks into focused subtasks |

### 13.3 Revision History

| Date | Author | Changes |
|------|--------|---------|
| 2025-11-25 | Steve + Claude | Initial draft |
