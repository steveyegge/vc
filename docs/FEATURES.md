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

---

## 🏗️ Infrastructure Workers: Build & CI/CD (vc-c9an)

Infrastructure workers analyze build systems and CI/CD pipelines to identify modernization opportunities and quality improvements.

### BuildModernizer

**Philosophy:** "Build systems should be simple, fast, and follow current best practices."

Analyzes build configuration files for:
- **Deprecated patterns**: Old commands, removed flags, outdated syntax
- **Missing optimizations**: Build caching, parallelism, incremental builds
- **Version issues**: EOL tool versions, inconsistent versions across files
- **Best practices**: Version managers, dependency management, reproducibility

**Supported build systems:**
- **Go**: go.mod, Makefile
- **JavaScript**: package.json, package-lock.json, yarn.lock, pnpm-lock.yaml
- **Python**: requirements.txt, setup.py, pyproject.toml
- **Rust**: Cargo.toml, Cargo.lock
- **Java**: build.gradle, pom.xml
- **Docker**: Dockerfile
- **Version files**: .tool-versions, .nvmrc, .ruby-version

**Example issues discovered:**
- "Makefile uses deprecated `go get`, migrate to `go install`"
- "Go version in go.mod is EOL (1.18), upgrade to 1.23"
- "No build caching configured, add go build cache"
- "Missing .tool-versions file for consistent tooling"

**Cost:** Cheap (~10 seconds, 1 AI call)

### CICDReviewer

**Philosophy:** "CI/CD pipelines should be fast, reliable, and enforce quality gates."

Analyzes CI/CD configuration files for:
- **Missing quality gates**: No tests, linting, security scans in pipeline
- **Slow pipelines**: Serial jobs that could run in parallel
- **Security issues**: Hardcoded secrets, missing secret scanning, overly permissive permissions
- **Deprecated actions**: Old GitHub Actions versions, outdated Docker images
- **Missing caching**: Dependencies re-downloaded every run, no build artifact caching

**Supported CI/CD platforms:**
- **GitHub Actions**: .github/workflows/*.yml
- **GitLab CI**: .gitlab-ci.yml
- **CircleCI**: .circleci/config.yml
- **Travis CI**: .travis.yml
- **Azure Pipelines**: azure-pipelines.yml
- **Jenkins**: Jenkinsfile

**Example issues discovered:**
- "CI runs tests serially, parallelize for 3x speedup"
- "No security scanning in CI pipeline, add govulncheck"
- "Using deprecated actions/checkout@v2, upgrade to v4"
- "Deploy job has hardcoded credentials, use secrets"
- "No dependency caching, reduce npm install time from 2min to 10sec"

**Cost:** Moderate (~20 seconds, 1-3 AI calls)

### Usage

Infrastructure workers run automatically during discovery:

```bash
# Standard preset includes both workers
vc discover

# Run specific infrastructure workers
vc discover --workers=build_modernizer,cicd_reviewer

# List all available workers
vc discover --list
```

### Code References

- **BuildModernizer**: `internal/health/build_modernizer.go`
- **BuildModernizer Tests**: `internal/health/build_modernizer_test.go`
- **CICDReviewer**: `internal/health/cicd_reviewer.go`
- **CICDReviewer Tests**: `internal/health/cicd_reviewer_test.go`
- **Registration**: `cmd/vc/discover.go:156-166` (discovery), `internal/executor/executor.go:700-718` (executor)

### Why This Matters

Infrastructure issues often get overlooked because they're not blocking immediate feature work. But they create technical debt:
- Slow builds waste developer time daily
- Missing CI quality gates let bugs slip through
- EOL tool versions create security risks
- Deprecated commands break on tooling upgrades

Infrastructure workers automatically find these issues during codebase discovery, ensuring they get prioritized and fixed before they cause problems.

---

## 🏷️ Discovered Label Taxonomy (vc-d0r3)

VC uses a consistent `discovered:*` label namespace to identify all issues filed by the AI supervisor. This taxonomy makes it easy to distinguish VC-filed issues from human-filed issues and to query/filter by discovery source.

### Label Categories

**1. discovered:blocker**
- Issues that block mission progress
- Selected BEFORE regular ready work (absolute priority)
- Example: "Missing API credentials needed for integration test"
- Added by: AI analysis during execution
- Code: `internal/ai/translation.go`

**2. discovered:related**
- Issues related to the mission but not blocking
- Selected AFTER regular ready work
- Example: "Add logging to error handling path"
- Added by: AI analysis during execution
- Code: `internal/ai/translation.go`

**3. discovered:background**
- Issues unrelated to the current mission
- Lower priority than discovered:related
- Example: "Update outdated documentation in README"
- Added by: AI analysis during execution
- Code: `internal/ai/translation.go`

**4. discovered:supervisor**
- Applied to ALL issues filed by VC's AI supervisor
- Distinguishes VC-filed issues from human-filed issues
- Used for filtering and analysis
- Added by: All AI supervisor issue creation paths
- Code: `internal/ai/translation.go`, `internal/executor/agent_report_handler.go`, `internal/executor/result_issues.go`

**5. discovered:code-review**
- Issues found during automated code review
- Used by code review sweep functionality
- Example: "Function exceeds cyclomatic complexity threshold"
- Added by: Code review worker

**6. discovered:self-healing**
- Issues created during self-healing mode
- Example: "Fix failing baseline test"
- Added by: Self-healing recovery logic

### Usage Patterns

**Query all VC-filed issues:**
```bash
# In JSONL file
grep 'discovered:' .beads/issues.jsonl

# In database
bd list | grep discovered:

# Count by discovery type
sqlite3 .beads/beads.db "SELECT label, COUNT(*) FROM labels WHERE label LIKE 'discovered:%' GROUP BY label"
```

**Filter by discovery source:**
```bash
# Only blockers (highest priority)
bd list | grep discovered:blocker

# All supervisor-filed issues (may have multiple discovered:* labels)
bd list | grep discovered:supervisor

# Self-healing issues only
bd list | grep discovered:self-healing
```

**Priority Order in Executor:**
1. Baseline-failure issues (in self-healing mode)
2. discovered:blocker (absolute priority)
3. Regular ready work (sorted by P0, P1, P2, P3)
4. discovered:related
5. discovered:background

### Why Multiple Labels?

VC-filed issues often have BOTH `discovered:supervisor` AND a type-specific label:
- `discovered:supervisor` → identifies WHO filed it (VC, not human)
- `discovered:blocker` → identifies WHAT it is (blocker, related, background)

Example issue labels:
```
["discovered:supervisor", "discovered:blocker", "infrastructure"]
```

This allows filtering by either:
- All VC-filed issues: `discovered:supervisor`
- Only blocking issues: `discovered:blocker`
- Both: issues that are VC-filed blockers

### Code References

**Label Constants**: `internal/types/labels.go:1-21`

**Issue Creation (adds discovered:supervisor):**
- AI Analysis: `internal/ai/translation.go:415-420`
- Agent Blockers: `internal/executor/agent_report_handler.go:106-109`
- Partial Completion: `internal/executor/agent_report_handler.go:172-175`
- Decomposed Children: `internal/executor/agent_report_handler.go:292-295`
- Code Review: `internal/executor/result_issues.go:65-68`
- Quality Issues: `internal/executor/result_issues.go:171-174`
- Test Issues: `internal/executor/result_issues.go:285-288`

**Work Selection (uses discovered:blocker/related/background):**
- Priority Logic: `internal/executor/work.go`
- Blocker-First: `docs/CONFIGURATION.md:238-267`

### Retroactive Labeling

All existing ai-supervisor issues were retroactively labeled with `discovered:supervisor` when this taxonomy was introduced (vc-d0r3). The labeling was done via SQL migration to ensure consistency.

### Why This Matters

The discovered:* taxonomy enables:
1. **Filtering**: Separate VC-filed issues from human-filed issues for analysis
2. **Metrics**: Track false-positive rates and quality of AI-discovered work
3. **Debugging**: Understand what the AI supervisor is creating and why
4. **Prioritization**: Apply blocker-first logic only to AI-discovered blockers
5. **Trust**: Users can see what VC filed vs what humans filed

---

## ⏸️ Task Pause/Resume (vc-d25s, vc-sibm, vc-ub00)

VC supports graceful task interruption with full context preservation, allowing you to pause long-running agents and resume them later without losing progress.

### When to Use Pause/Resume

**Common scenarios:**
- **Budget limits approaching** - Pause before exceeding cost thresholds
- **Urgent work arrived** - Interrupt current task to prioritize critical issue
- **Need to board a plane** - Save progress before losing connectivity
- **Debug agent state** - Pause to inspect what the agent is doing
- **Executor shutdown** - Gracefully stop work before restarting

### How It Works

**Architecture:**
1. **RPC Control Server** - Unix socket at `.vc/executor.sock` accepts commands
2. **Interrupt Checkpoints** - Agent execution checks interrupt flag at safe points
3. **Context Persistence** - Interrupt metadata saved to database with agent state
4. **Resume Context Injection** - Saved context briefed to agent on resume

**Flow:**
```
User: vc pause vc-123 --reason "need to board plane"
  ↓
Control CLI → RPC to executor → Set interrupt flag
  ↓
Agent: Checks interrupt at checkpoint → Detects flag
  ↓
Executor: Saves agent context + marks issue as interrupted
  ↓
Issue: Released back to 'open' status with 'interrupted' label
```

**Resume flow:**
```
User: vc resume vc-123
  ↓
Control CLI: Removes 'interrupted' label
  ↓
Executor: Claims issue from ready work
  ↓
CheckAndLoadInterruptContext: Loads saved metadata
  ↓
Agent: Receives resume brief with previous context
  ↓
Agent: Continues from where it left off
```

### Usage

**Pause a running task:**
```bash
# Pause with reason
vc pause vc-123 --reason "budget approaching limit"

# Pause without reason
vc pause vc-123

# Output:
# ⏸️  Pause requested for vc-123 (reason: budget approaching limit)
#    Waiting for agent to reach safe checkpoint...
# ✓ Saved interrupt context for vc-123
```

**Resume a paused task:**
```bash
vc resume vc-123

# Output:
# ✓ Issue vc-123 ready for resume
#   Status: open
#   The running executor will pick this up automatically.
```

**Check status:**
```bash
vc status

# Shows interrupted issues in separate section:
# Interrupted Issues (1):
#   vc-123: Fix authentication bug (interrupted 2h ago)
#     Reason: budget approaching limit
#     Resume count: 0
```

**List all interrupted issues:**
```bash
bd list | grep interrupted
```

### Interrupt Checkpoints

The executor checks for interrupts at safe points during execution:

**Checkpoint locations:**
- After each agent tool use (Read, Edit, Write, Bash, etc.)
- Between execution phases (assess → execute → analyze)
- Before quality gate runs
- During agent streaming output

**NOT checked during:**
- Active AI API calls (would waste tokens)
- File I/O operations (would corrupt state)
- Git operations (would leave repo in inconsistent state)

This ensures interrupts happen at clean boundaries without data loss.

### Interrupt Metadata Schema

**Saved to `issue_interrupt_metadata` table:**
```json
{
  "issue_id": "vc-123",
  "interrupted_at": "2025-11-23T10:30:00Z",
  "interrupted_by": "control-cli",
  "reason": "budget approaching limit",
  "executor_instance_id": "uuid-1234",
  "execution_state": "executing",
  "context_snapshot": "{\"working_notes\":\"...\",\"todos\":[...]}",
  "resume_count": 0,
  "resumed_at": null
}
```

**Context snapshot structure:**
```go
type AgentContext struct {
    InterruptedAt   time.Time
    WorkingNotes    string   // Agent's current thinking
    ProgressSummary string   // What's been done so far
    CurrentPhase    string   // "executing", "testing", etc.
    LastTool        string   // Last tool used
    LastToolResult  string   // Result of last tool
    Todos           []string // Remaining work
    CompletedTodos  []string // Finished tasks
    Observations    []string // Key insights
    SessionDuration time.Duration
}
```

### Resume Context Brief

When resuming, the agent receives a brief explaining the interruption:

**Example brief:**
```markdown
**Task Resumed from Interrupt**

This task was interrupted at 2025-11-23 10:30:00.

**Reason**: budget approaching limit
**Interrupted by**: control-cli
**Execution phase**: executing

**Your working notes**:
Working on authentication fix in auth.go. Need to validate token expiry logic.

**Progress so far**: Fixed token parsing, updated tests, ready to test integration.

**Please continue from where you left off.**
```

### Configuration

**Socket path (executor config):**
```go
cfg := executor.DefaultConfig()
cfg.ControlSocketPath = ".vc/executor.sock"  // Default
cfg.EnableControlServer = true               // Default: enabled
```

**Environment variables:**
```bash
# Override socket path
export VC_CONTROL_SOCKET_PATH="/tmp/vc-executor.sock"
```

### Troubleshooting

**Socket not found:**
```
Error: no running executor found (no control socket)
Hint: Is the executor running? Try 'vc status' to check.
```

**Solution:** Start the executor: `vc execute --continuous`

**Permission denied:**
```
Error: failed to connect to control socket: permission denied
```

**Solution:** Check socket file permissions: `ls -l .vc/executor.sock`

**Wrong issue:**
```
Error: issue vc-456 is not currently executing (current: vc-123)
```

**Solution:** Check which issue is running: `vc status`

**No task executing:**
```
Error: no task currently executing
```

**Solution:** The executor is idle. Check ready work: `bd ready`

### Multiple Resume Cycles

Issues can be paused and resumed multiple times. The `resume_count` field tracks this:

```bash
# First pause
vc pause vc-123 --reason "lunch break"
# resume_count: 0

# Resume
vc resume vc-123
# resume_count: 1

# Second pause
vc pause vc-123 --reason "meeting"
# resume_count: 1 (preserved)

# Second resume
vc resume vc-123
# resume_count: 2
```

Each resume increments the count and updates `resumed_at` timestamp.

### Code References

**Core Implementation:**
- Interrupt Manager: `internal/executor/executor_interrupt.go:1-270`
- Control Server: `internal/control/server.go:1-255`
- CLI Commands: `cmd/vc/pause.go`, `cmd/vc/resume.go`
- Integration Tests: `internal/executor/pause_resume_integration_test.go`

**Database Schema:**
- Table: `issue_interrupt_metadata`
- Functions: `SaveInterruptMetadata`, `GetInterruptMetadata`, `MarkInterruptResumed`
- Query: `ListInterruptedIssues`

**Executor Integration:**
- Checkpoint detection: `internal/executor/executor_execution.go`
- Resume context injection: `CheckAndLoadInterruptContext` called before `executeIssue`
- Context brief generation: `buildAgentResumeContext`

### Limitations

**Current limitations:**
- **Agent context extraction** - Context snapshot is basic (no real-time todo list extraction from agent)
- **Resume via RPC** - `vc resume` doesn't send RPC command, just removes label (executor claims from queue)
- **Budget integration** - Budget monitor doesn't auto-pause yet (manual pause only)
- **Multi-executor** - Interrupt metadata doesn't prevent other executors from claiming

**Future enhancements:**
- Extract full agent state (todos, file diffs, partial edits)
- Auto-pause when budget thresholds exceeded
- Distributed locking for multi-executor environments
- Resume-specific priority boost (interrupted issues first)

### Why This Matters

**Before pause/resume:**
- Long-running tasks couldn't be stopped gracefully
- Killing executor lost all agent progress
- Budget overruns required manual intervention
- Urgent work required waiting for current task to finish

**After pause/resume:**
- Graceful interruption at safe checkpoints
- Full context preservation across restarts
- Budget-triggered auto-pause (future)
- Instant priority switching without progress loss

This enables **iterative workflow** where humans can course-correct without losing expensive AI work.

---

## 🏥 Code Health Monitors

VC includes a suite of AI-powered code health monitors that proactively detect quality issues and technical debt. All monitors follow **Zero Framework Cognition (ZFC)** principles: they collect facts and patterns, then delegate judgment to AI.

### Available Monitors

#### 1. Gitignore Detector (vc-pf2o)

**Purpose**: Identifies files tracked in git that should be in `.gitignore`.

**Detection Categories**:
- **Secrets** (HIGH severity): `.env` files, API keys, certificates, credentials
- **Build Artifacts** (MEDIUM): Compiled binaries, `.o` files, `dist/` directories
- **Dependencies** (MEDIUM): `node_modules/`, `vendor/`, downloaded packages  
- **Editor Files** (MEDIUM): `.vscode/`, `.idea/`, swap files
- **OS Files** (MEDIUM): `.DS_Store`, `Thumbs.db`, `desktop.ini`

**How It Works**:
1. Runs `git ls-files` to get all tracked files
2. Matches files against common gitignore patterns
3. AI categorizes violations vs legitimate files (e.g., `.env.example` is OK)
4. Creates issues with remediation steps (`git rm --cached <file>`)
5. Suggests `.gitignore` patterns to prevent recurrence

**Philosophy**: "Source control should track source code and configuration, not build artifacts, dependencies, secrets, or environment-specific files."

**Schedule**: Hybrid (12-24 hours, or every 10 issues)

**Cost**: Moderate (3s, 1 AI call, full scan)

**Code References**:
- Implementation: `internal/health/gitignore_detector.go`
- Tests: `internal/health/gitignore_detector_test.go`
- Registration: `cmd/vc/health.go:205-207`

**Usage**:
```bash
# Run gitignore detector only
vc health check --monitor gitignore

# Dry run (show issues without filing)
vc health check --monitor gitignore --dry-run

# Verbose output with reasoning
vc health check --monitor gitignore --verbose
```

**Example Issues Created**:
- **High Severity**: "URGENT: Remove 2 secret/credential file(s) from git history"
  - Evidence: `.env`, `credentials.json`
  - Action: Remove from git, add to `.gitignore`, rotate secrets
  
- **Medium Severity**: "Clean up 5 tracked file(s) that should be gitignored"
  - Evidence: `.DS_Store`, `build/main.o`, `node_modules/...`
  - Action: `git rm --cached`, add patterns to `.gitignore`

#### 2. Other Health Monitors

See other monitor implementations for similar ZFC-compliant health checks:
- **File Size Monitor**: Detects oversized files that should be split
- **Cruft Detector**: Finds backup files, temp files, old versions
- **ZFC Detector**: Identifies hardcoded thresholds, regex-based parsing
- **Duplication Detector**: Finds duplicate code blocks worth extracting
- **Complexity Monitor**: Identifies high-complexity functions/files

### Running Health Checks

```bash
# Run all health monitors
vc health check

# Run specific monitor
vc health check --monitor <name>

# Dry run (preview without filing issues)
vc health check --dry-run

# Verbose output (show AI reasoning)
vc health check --verbose
```

### Integration with VC Workflow

Health monitors integrate seamlessly with the VC workflow:

1. **Scheduled Execution**: Monitors run based on their schedule (time-based, event-based, or hybrid)
2. **AI Evaluation**: Each monitor uses AI to distinguish real problems from false positives
3. **Issue Creation**: Discovered problems become tracked issues in `.beads/issues.jsonl`
4. **Priority Assignment**: Severity maps to priority (high→P1, medium→P2, low→P3)
5. **Label Tagging**: Issues tagged with `health`, category, and severity labels
6. **Executor Claims**: Issues can be claimed by VC executor for automated fixing

### Why ZFC Compliance Matters

Traditional code quality tools use hardcoded rules and thresholds:
- "Files over 500 lines are too big" ← Brittle
- "Complexity > 10 is bad" ← Context-blind
- "All duplicates must be extracted" ← Cargo-cult

ZFC-compliant monitors:
- **Collect facts**: "This file has 1200 lines and handles 5 different concerns"
- **Provide context**: "Most files in this codebase are 100-300 lines"
- **Defer judgment**: AI decides if splitting makes sense given the specific case
- **Explain reasoning**: "This violates single responsibility and hinders testing"

This produces far fewer false positives and more actionable issues.

### Monitoring the Monitors

Track health monitor effectiveness:
```sql
-- Recent health issues filed
SELECT id, title, priority, created_at 
FROM issues 
WHERE actor = 'vc-health-monitor'
ORDER BY created_at DESC 
LIMIT 20;

-- Health issues by category
SELECT 
    json_extract(labels.value, '$') as label,
    COUNT(*) as count
FROM issues, json_each(labels) as labels
WHERE actor = 'vc-health-monitor'
    AND json_extract(labels.value, '$') NOT IN ('health', 'severity:high', 'severity:medium', 'severity:low')
GROUP BY label
ORDER BY count DESC;
```

### Future Enhancements

Planned health monitor improvements:
- **Test Coverage Monitor**: Detect untested code paths
- **Dependency Audit Monitor**: Check for outdated/vulnerable dependencies  
- **Dead Code Monitor**: Find unused functions, imports, variables
- **Documentation Monitor**: Detect missing/stale docs
- **Performance Monitor**: Identify performance regressions

---
