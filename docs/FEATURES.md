# Feature Deep Dives

This document contains detailed documentation for specific VC features.

---

## 🔧 Self-Healing Baseline Failures (vc-210)

When preflight detects baseline test failures, VC can automatically fix them.

### How It Works

1. **Preflight fails** → Creates `vc-baseline-test` issue (P1)
2. **Executor claims** baseline issue (vc-208 fix)
3. **Agent receives** specialized self-healing prompt
4. **AI diagnoses** failure type (flaky/real/environmental)
5. **Agent applies** minimal fix with verification
6. **Tests pass** → Baseline restored → Work resumes

### Failure Types

- **Flaky**: Race conditions, timing issues → Add sync, remove non-determinism
- **Real**: Actual bugs → Minimal fix to restore functionality
- **Environmental**: Missing deps → Mock externals, add setup

### Code References

- **Prompts**: `internal/executor/prompt.go:177-237` (self-healing baseline prompt)
- **AI Diagnosis**: `internal/ai/test_failure.go` (DiagnoseTestFailure function)
- **Events**: `internal/events/types.go` (baseline_test_fix_* event types)
- **Detection**: Preflight quality gate detects baseline failures

### Why This Matters

Before vc-210, baseline test failures blocked all work until a human intervened. With self-healing, VC can:
- Diagnose the root cause using AI
- Apply minimal, targeted fixes
- Verify the fix works
- Resume normal operation automatically

This dramatically reduces downtime and keeps work flowing.

### Monitoring

See [docs/QUERIES.md](./QUERIES.md) for self-healing metrics queries including:
- Self-healing success rate
- Recent self-healing attempts
- Diagnosis quality (confidence scores)
- Fix type distribution
- Failure type distribution

---

## 🛑 Executor Graceful Shutdown

The executor supports graceful shutdown via SIGTERM/SIGINT signals (Ctrl+C). When a shutdown signal is received, the executor:

1. **Stops claiming new work** - The event loop exits immediately after completing the current poll
2. **Allows current execution to finish** - If an issue is being processed, it continues (with 30-second grace period)
3. **Handles quality gates cancellation** - If gates are running when shutdown occurs:
   - Gates detect context cancellation and stop cleanly
   - Issue is NOT marked as blocked due to cancellation
   - Issue returns to 'open' status for retry
4. **Releases executor claims** - Instance is marked as stopped, claims are released by cleanup loop
5. **Closes connections** - Database and other resources are closed properly

### Graceful Shutdown Behavior

**During normal operation:**
```bash
$ ./vc execute
✓ Executor started (version 0.1.0)
  Polling for ready work every 5s
  Press Ctrl+C to stop

Executing issue vc-123: Fix authentication bug
Running quality gates (timeout: 5m)...
  test: PASS
^C

Shutting down executor...
Warning: quality gates cancelled due to executor shutdown
✓ Executor stopped
```

**Key Points:**
- **30-second grace period** - Executor has 30 seconds to complete current work before forced termination
- **Quality gates respect cancellation** - Gates check for context cancellation and exit cleanly
- **No false negatives** - Issues interrupted during execution are NOT marked as failed/blocked
- **Automatic cleanup** - Stale instance cleanup releases any orphaned claims

### For Developers

When implementing operations that may be interrupted:

```go
// Good: Check for cancellation before long operations
select {
case <-ctx.Done():
    return fmt.Errorf("operation cancelled: %w", ctx.Err())
default:
    // Continue with operation
}

// Good: Distinguish between timeout and cancellation
if ctx.Err() == context.DeadlineExceeded {
    // Operation timed out - this is a failure
} else if ctx.Err() == context.Canceled {
    // Executor is shutting down - not a failure, just cleanup
}
```

### Troubleshooting

**Issue stuck in 'in_progress' after executor kill:**
- Run the cleanup loop: The executor's cleanup goroutine runs every 5 minutes
- Issues claimed by stopped instances are automatically released
- Or manually release: `bd update vc-X --status open`

**Context canceled errors during shutdown:**
- Normal during graceful shutdown
- Quality gates and storage operations log warnings but don't fail
- Issues are properly released during cleanup phase

---

## 📡 Agent Progress Events (vc-129)

**Status:** Implemented. Agent tool usage is now captured and stored in real-time.

### Overview

When agents execute in background mode, their progress is captured as structured events in the activity feed. This provides visibility into what agents are doing and helps distinguish between actual hangs and normal operation.

### Event Types

Three new event types were added for agent progress tracking:

1. **`agent_tool_use`** - Captured when agent invokes a tool (Read, Edit, Write, Bash, Glob, Grep, Task)
2. **`agent_heartbeat`** - Periodic progress updates (future - not yet emitted)
3. **`agent_state_change`** - Agent state transitions like thinking→planning→executing (future - not yet emitted)

### Tool Usage Detection

The parser automatically detects tool usage from agent output patterns like:
- "Let me use the Read tool to read the file"
- "I'll use the Edit tool to modify parser.go"
- "Using the Bash tool to run tests"
- "Spawning the Task tool to launch an agent"

**Supported tools:**
- Read (file reads)
- Edit (file modifications)
- Write (file creation)
- Bash (command execution)
- Glob (file search by pattern)
- Grep (content search)
- Task (agent spawning)
- Generic fallback for any "XYZ tool" pattern

### Future Work

**Not yet implemented** (punted for now):

1. **Heartbeat emission** - Agent doesn't emit periodic heartbeat events yet
   - Would require goroutine in agent.go to emit events every 30-60s
   - Would track "current action" based on recent tool usage

2. **State change detection** - No explicit state tracking yet
   - Would require analyzing output patterns for thinking/planning/executing
   - Or structured state markers in agent output

3. **Watchdog integration** (vc-234) - Watchdog doesn't consume progress events yet
   - Would check time since last progress event
   - Would distinguish stuck (no events >5min) vs thinking (recent events)

4. **CLI visualization** - No `vc tail -f --issue vc-X` command yet
   - Would stream progress events in real-time
   - Would show colorized tool usage, file changes, etc.

### Why This Helps

**Before vc-129:**
- Agent spawned, no output for 5+ minutes
- Activity feed showed "agent_spawned" then silence
- Appeared stuck, but was actually working
- Watchdog saw "0 executions" (no progress events)

**After vc-129:**
- Tool usage captured in real-time (Read, Edit, Write, Bash, etc.)
- Activity feed shows what agent is doing
- Clear distinction between working vs stuck
- Foundation for watchdog convergence detection

**Example event stream:**
```
10:06:20 agent_spawned: Claude Code started on vc-123
10:06:25 agent_tool_use: Read tool - parser.go
10:06:30 agent_tool_use: Glob tool - find test files
10:07:15 file_modified: Created parser_test.go
10:07:45 agent_tool_use: Bash tool - run tests
10:08:10 test_run: PASS (all tests passed)
10:08:20 git_operation: git add parser_test.go
10:08:25 agent_completed: Success
```

### Monitoring

See [docs/QUERIES.md](./QUERIES.md) for agent progress queries including:
- View tool usage for an issue
- Tool usage frequency
- Agent activity timeline

---

## 🔒 Daemon Coexistence (vc-195)

**VC uses an exclusive lock protocol** to prevent bd daemon from interfering with execution.

### How It Works

- When VC executor starts, it creates `.beads/.exclusive-lock`
- bd daemon (v0.17.3+) checks for this lock and skips locked databases
- **You can safely run bd daemon alongside VC** - they coexist peacefully
- VC manages its database exclusively, daemon manages other databases
- Lock is automatically removed on executor shutdown

### Requirements

- Beads v0.17.3 or later (for daemon lock awareness)
- VC executor running (creates the lock)

---

## 📐 Dependency Direction Convention

**CRITICAL**: Always use `(child, parent)` direction for parent-child dependencies.

```bash
bd dep add vc-10 vc-5 --type parent-child  # Child vc-10 depends ON parent vc-5
```

- `GetDependencies(child)` → returns parents
- `GetDependents(parent)` → returns children

Early issues (vc-5 through vc-9) had inverted dependencies, fixed in vc-90. All new code must use standard direction.

---

## 💬 Using the VC Conversational REPL (For End Users)

**Note**: This section describes the VC REPL for end users. As an AI agent working on VC's codebase, you'll use `bd` commands. But users of VC will interact via the conversational interface.

### Pure Conversational Interface

VC provides a natural language interface - no slash commands to memorize (except `/quit`).

**Starting VC:**
```bash
export ANTHROPIC_API_KEY=your-key-here
./vc
```

**Example conversations:**

Finding and starting work:
```
You: What's ready to work on?
AI: [Shows ready issues]
You: Let's continue working
AI: [Executes next ready issue]
```

Creating issues:
```
You: We need Docker support
AI: [Creates feature issue]
You: Add tests for authentication
AI: [Creates task]
```

Monitoring:
```
You: How's the project doing?
AI: [Shows project statistics]
You: What's blocked?
AI: [Lists blocked issues with details]
```

Multi-turn context:
```
You: Create an epic for user management
AI: [Creates epic vc-200]
You: Add login, registration, and password reset as children
AI: [Creates 3 tasks and links to epic]
```

### Available Conversational Tools

The AI has access to these tools (you don't call them directly):
- **create_issue**: Creates issues from natural language
- **create_epic**: Creates epic (container) issues
- **add_child_to_epic**: Links issues to epics
- **get_ready_work**: Shows issues ready to execute
- **get_issue**: Retrieves issue details
- **get_status**: Shows project statistics
- **get_blocked_issues**: Lists blocked issues
- **continue_execution**: Executes work (the VibeCoder Primitive)
- **get_recent_activity**: Shows agent execution history
- **search_issues**: Searches issues by text

The AI understands your intent and uses these tools automatically.
