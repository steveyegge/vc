# VC Architecture

**Last Updated**: 2025-10-25
**Status**: Production (Bootstrap Complete, Dogfooding Phase)

This document describes the architecture of VC, an AI-supervised autonomous coding system.

---

## Table of Contents

1. [High-Level Overview](#high-level-overview)
2. [Core Components](#core-components)
3. [Execution Flow](#execution-flow)
4. [Data Flow](#data-flow)
5. [Key Design Patterns](#key-design-patterns)

---

## High-Level Overview

VC is an **issue-oriented orchestration system** that uses AI supervision to execute coding tasks autonomously.

```
┌─────────────────────────────────────────────────────────────┐
│                         VC System                            │
│                                                              │
│  ┌────────────┐      ┌──────────────┐      ┌─────────────┐ │
│  │            │      │              │      │             │ │
│  │   REPL     │─────▶│   Executor   │─────▶│   Storage   │ │
│  │ (UI/CLI)   │      │ (Event Loop) │      │   (Beads)   │ │
│  │            │      │              │      │             │ │
│  └────────────┘      └───────┬──────┘      └─────────────┘ │
│                              │                              │
│                              ▼                              │
│              ┌───────────────────────────────┐              │
│              │    Coding Agent (Amp)         │              │
│              │  ┌─────────────────────────┐  │              │
│              │  │   AI Supervisor          │  │              │
│              │  │  ┌─────────┬─────────┐  │  │              │
│              │  │  │ Assess  │ Analyze │  │  │              │
│              │  │  │ (before)│ (after) │  │  │              │
│              │  │  └─────────┴─────────┘  │  │              │
│              │  └─────────────────────────┘  │              │
│              │                               │              │
│              │  ┌─────────────────────────┐  │              │
│              │  │   Quality Gates          │  │              │
│              │  │  ┌──────┬──────┬──────┐ │  │              │
│              │  │  │ Test │ Lint │Build │ │  │              │
│              │  │  └──────┴──────┴──────┘ │  │              │
│              │  └─────────────────────────┘  │              │
│              └───────────────────────────────┘              │
└─────────────────────────────────────────────────────────────┘
```

**Key Concepts:**

- **Issue Tracker as Orchestration**: Work is managed through Beads issue tracker
- **AI Supervision**: LLM assesses before and analyzes after agent execution
- **Sandboxed Execution**: Each issue runs in an isolated git worktree
- **Quality Gates**: Automated checks ensure code quality
- **Zero Framework Cognition**: All decisions delegated to AI, no heuristics

---

## Core Components

### 1. Executor (`internal/executor/`)

**Role**: Main orchestration event loop

**Responsibilities**:
- Poll for ready work from issue tracker
- Claim issues atomically
- Create sandboxes (git worktrees)
- Coordinate AI assessment → agent execution → AI analysis
- Run quality gates
- Commit changes if successful
- File discovered issues
- Handle failures and cleanup

**Entry Point**: `./vc execute`

**Key Files**:
- `executor.go` - Main event loop (`processNextIssue`)
- `blocker_priority.go` - Smart work selection (blockers first)
- `result_processor.go` - Handle agent results (punted work, discovered bugs)
- `result_dedup.go` - Deduplicate discovered issues

### 2. AI Supervisor (`internal/ai/supervisor.go`)

**Role**: AI-powered task analysis

**Responsibilities**:
- **Assessment** (before execution):
  - Analyze issue requirements
  - Estimate effort/complexity
  - Identify risks
  - Suggest approach
- **Analysis** (after execution):
  - Parse agent's structured report
  - Identify punted work items
  - Extract discovered bugs/issues
  - Determine success/failure

**AI Model**: Claude Sonnet 4.5

**Key Functions**:
- `AssessIssue()` - Pre-execution analysis
- `AnalyzeResult()` - Post-execution analysis

### 3. Coding Agent (Amp/Claude Code)

**Role**: Autonomous code modification

**Responsibilities**:
- Read code, understand requirements
- Make code changes (edit, create, delete files)
- Run tests, verify changes
- Report structured output (completed/blocked/partial/decomposed)

**Technology**: Amp CLI with Claude Sonnet 4.5

**Execution Mode**: Background process in sandbox

**Key Outputs**:
- Modified files in git worktree
- Structured status report (JSON)
- Tool usage events (Read, Edit, Write, Bash, etc.)

### 4. Storage Layer (`internal/storage/`)

**Role**: Persistence and state management

**Implementation**: Wraps Beads library (SQLite)

**Responsibilities**:
- Issue CRUD operations
- Dependency management
- Execution state tracking
- Executor instance coordination
- Agent event logging

**Key Tables**:
- `issues` - Core issue data (from Beads)
- `dependencies` - Issue relationships (from Beads)
- `vc_issue_execution_state` - Execution tracking (VC extension)
- `vc_executor_instances` - Executor coordination (VC extension)
- `agent_events` - Activity feed (VC extension)

### 5. Quality Gates (`internal/gates/`)

**Role**: Automated verification

**Responsibilities**:
- Run tests (`go test ./...`)
- Run linter (`golangci-lint`)
- Build project (`go build`)
- Timeout enforcement (5 minutes default)
- Parallel execution

**Configuration**: Per-gate enable/disable, timeout

**Behavior**:
- Gates run in parallel for speed
- Any failure = overall failure
- Cancellation-safe (executor shutdown)

### 6. Sandbox Manager (`internal/sandbox/`)

**Role**: Isolated execution environments

**Responsibilities**:
- Create git worktrees (`.sandboxes/mission-vc-X/`)
- Create branches (`mission/vc-X/timestamp`)
- Provide isolated workspace for agents
- Clean up on success/failure

**Benefits**:
- Agents can't corrupt main repo
- Multiple executors can run safely
- Failed work doesn't affect main branch

### 7. Watchdog (`internal/watchdog/`)

**Role**: Convergence detection and anomaly monitoring

**Responsibilities**:
- Monitor for stuck/looping agents
- Detect Read/Grep loops (circuit breaker)
- Assess agent progress vs. time
- Alert on anomalies (high confidence + high severity)

**AI-Powered**: Uses Claude to assess patterns

**Status**: Partially implemented

### 8. REPL (`internal/repl/`)

**Role**: Natural language interface

**Responsibilities**:
- Conversational UI for users
- Create issues from natural language
- Query project status
- Trigger execution
- Tool functions (create_issue, continue_execution, etc.)

**Entry Point**: `./vc` (interactive mode)

### 9. Deduplication (`internal/deduplication/`)

**Role**: Prevent filing duplicate issues

**Responsibilities**:
- Compare new issues against recent issues
- AI-powered semantic similarity
- Batch processing (50 comparisons/call)
- Within-batch dedup (discovered issues)

**Configuration**: Confidence threshold (0.85), lookback days (7)

**Performance**: 3 AI calls for 3 discovered issues (vs 15 with old config)

---

## Execution Flow

Here's what happens when you run `./vc execute`:

```
┌──────────────────────────────────────────────────────────────┐
│                    Executor Event Loop                        │
└──────────────────────────────────────────────────────────────┘

 1. Poll for ready work
    └─▶ storage.GetReadyWork(status='open', limit=1)
         │
         ├─ First try: GetReadyBlockers() - issues blocking other work
         └─ Fallback: GetReadyWork() - any ready issue

 2. Claim issue atomically
    └─▶ storage.ClaimIssue(issueID, instanceID)
         └─ UPDATE issues SET status='in_progress' WHERE status='open'
         └─ INSERT vc_issue_execution_state

 3. AI Assessment (optional, if enabled)
    └─▶ supervisor.AssessIssue(issue)
         └─ Returns: confidence, effort estimate, strategy

 4. Create sandbox
    └─▶ sandbox.Create(issueID)
         └─ git worktree add .sandboxes/mission-vc-X branch
         └─ Returns: sandbox path

 5. Spawn agent in sandbox
    └─▶ agent.Execute(issue, sandboxPath)
         └─ Runs: amp --background --cwd=sandbox
         └─ Agent has access to:
              - Read, Edit, Write, Bash tools
              - Issue description, acceptance criteria
              - CLAUDE.md project instructions
         └─ Agent outputs structured report:
              {
                "status": "completed|blocked|partial|decomposed",
                "summary": "...",
                ...
              }

 6. Parse agent result
    └─▶ Parse JSON from agent output
         └─ Extract: status, files_modified, blockers, punted work

 7. AI Analysis (optional, if enabled)
    └─▶ supervisor.AnalyzeResult(issue, agentOutput)
         └─ Returns: discovered issues, punted work items

 8. Deduplicate discovered issues
    └─▶ dedup.DeduplicateIssues(discoveredIssues)
         └─ Compare against recent issues
         └─ Filter out duplicates

 9. Run quality gates
    └─▶ gates.Run(sandboxPath)
         └─ Parallel: test, lint, build
         └─ Timeout: 5 minutes
         └─ Returns: pass/fail

10. Handle result
    ├─ SUCCESS:
    │   ├─ Commit changes in sandbox
    │   ├─ Close issue
    │   ├─ File discovered issues
    │   └─ Clean up sandbox
    │
    └─ FAILURE:
        ├─ Reopen issue (back to 'open')
        ├─ Add error comment
        ├─ Keep sandbox (if configured)
        └─ File blocker issues

11. Repeat (poll every 5 seconds)
```

---

## Data Flow

### Issue Lifecycle States

```
                     ┌──────────────┐
                     │     OPEN     │◀─── User creates issue
                     └──────┬───────┘     or AI discovers issue
                            │
              ClaimIssue()  │
                            ▼
                  ┌──────────────────┐
                  │   IN_PROGRESS    │◀─── Executor claims
                  └────┬────────┬────┘
                       │        │
        Success        │        │        Failure
                       │        │
                       ▼        ▼
              ┌────────────┐  ┌────────────┐
              │   CLOSED   │  │  BLOCKED   │
              └────────────┘  └────────────┘
                                    │
                        Manual fix  │
                                    ▼
                              ┌──────────┐
                              │   OPEN   │
                              └──────────┘
```

### Execution State Tracking

Each issue can have an `execution_state` record:

```sql
CREATE TABLE vc_issue_execution_state (
  issue_id TEXT PRIMARY KEY,
  executor_instance_id TEXT,
  claimed_at TIMESTAMP,
  state TEXT,  -- claimed|assessing|executing|analyzing|gates|committing
  checkpoint_data TEXT,
  error_message TEXT,
  updated_at TIMESTAMP
);
```

**States**:
- `claimed` - Executor has claimed the issue
- `assessing` - AI is assessing the issue
- `executing` - Agent is working on it
- `analyzing` - AI is analyzing agent output
- `gates` - Quality gates are running
- `committing` - Changes are being committed

### Event Stream

All activity is logged to `agent_events`:

```sql
CREATE TABLE agent_events (
  id INTEGER PRIMARY KEY,
  issue_id TEXT,
  type TEXT,  -- progress, file_modified, test_run, agent_tool_use, etc.
  message TEXT,
  severity TEXT,  -- info, warning, error
  data TEXT,  -- JSON payload
  timestamp TIMESTAMP
);
```

**Event Types**:
- `agent_spawned` - Agent started
- `agent_tool_use` - Agent used a tool (Read, Edit, Write, Bash)
- `file_modified` - File was changed
- `test_run` - Tests executed
- `quality_gate_*` - Gate results
- `agent_completed` - Agent finished
- `deduplication_*` - Dedup decisions

---

## Key Design Patterns

### 1. Issue-Oriented Orchestration

**Principle**: All work flows through the issue tracker. No ad-hoc task execution.

**Benefits**:
- Explicit dependencies
- Atomic work claiming
- Progress tracking
- Resumability after crashes

### 2. Zero Framework Cognition (ZFC)

**Principle**: Delegate all decisions to AI. No heuristics, regex, or parsing in orchestration layer.

**Examples**:
- AI decides if code is complete (not regex parsing test output)
- AI identifies punted work (not keyword matching)
- AI deduplicates issues (not string similarity)

**Benefits**:
- Adapts to new patterns
- Handles edge cases gracefully
- Self-improving as models improve

### 3. Nondeterministic Idempotence

**Principle**: Operations can crash and resume. AI figures out where we left off.

**Implementation**:
- Execution state tracking
- Checkpoint data
- Issue status transitions

**Benefits**:
- Resilient to crashes
- No complex rollback logic
- Natural recovery

### 4. Blocker Priority

**Principle**: Execute issues blocking other work first (vc-156, vc-157)

**Algorithm**:
```
1. Try GetReadyBlockers() - issues with 'discovered:blocker' label
2. If none, fallback to GetReadyWork() - regular ready issues
3. Within each category: priority order (P0 > P1 > P2 > P3)
```

**Benefits**:
- Unblocks parallelism faster
- Reduces work starvation
- Critical bugs fixed first

### 5. AI Supervision (Assess + Analyze)

**Principle**: AI bookends agent execution with analysis

**Assessment** (before):
- Is this task clear?
- What's the approach?
- What could go wrong?

**Analysis** (after):
- What did the agent accomplish?
- What work was punted?
- Were bugs discovered?

**Benefits**:
- Automatic work discovery
- Nothing gets forgotten
- Quality feedback loop

### 6. Sandbox Isolation

**Principle**: Each issue executes in its own git worktree

**Implementation**:
- Branch: `mission/vc-X/timestamp`
- Worktree: `.sandboxes/mission-vc-X/`

**Benefits**:
- Main repo stays clean
- Parallel executors don't conflict
- Easy rollback on failure

### 7. Quality Gates

**Principle**: Automated checks before merging

**Gates**:
- Test (go test)
- Lint (golangci-lint)
- Build (go build)

**Benefits**:
- Prevent broken code
- Enforce standards
- Fast feedback (parallel execution)

---

## Component Interactions

```
User runs: ./vc execute
     │
     ▼
┌────────────────────────────────────────────────────────────┐
│ Executor (internal/executor/executor.go)                   │
│                                                             │
│  processNextIssue() {                                      │
│    1. issue = storage.GetReadyBlockers() ||               │
│              storage.GetReadyWork()                        │
│       └─▶ Beads (SQLite) query                            │
│                                                            │
│    2. storage.ClaimIssue(issue)                           │
│       └─▶ UPDATE issues SET status='in_progress'          │
│       └─▶ INSERT vc_issue_execution_state                 │
│                                                            │
│    3. assessment = supervisor.AssessIssue(issue)          │
│       └─▶ AI Supervisor                                   │
│            └─▶ Claude API (Sonnet 4.5)                    │
│                 └─▶ Returns: {confidence, strategy, ...}  │
│                                                            │
│    4. sandbox = sandboxManager.Create(issue)              │
│       └─▶ git worktree add .sandboxes/mission-vc-X       │
│                                                            │
│    5. agentResult = agent.Execute(issue, sandbox)         │
│       └─▶ Spawn: amp --background --cwd=sandbox          │
│            └─▶ Agent runs in background                   │
│                 └─▶ Claude API (Sonnet 4.5)              │
│                      └─▶ Returns: {status, files, ...}    │
│                                                            │
│    6. analysis = supervisor.AnalyzeResult(agentResult)    │
│       └─▶ AI Supervisor                                   │
│            └─▶ Claude API                                 │
│                 └─▶ Returns: {discovered_issues, ...}     │
│                                                            │
│    7. uniqueIssues = dedup.Deduplicate(discovered)        │
│       └─▶ Deduplicator                                    │
│            └─▶ Claude API (batch comparison)             │
│                 └─▶ Returns: non-duplicate issues         │
│                                                            │
│    8. gateResults = gates.Run(sandbox)                    │
│       └─▶ Quality Gates (parallel)                       │
│            ├─▶ go test ./...                              │
│            ├─▶ golangci-lint run                          │
│            └─▶ go build ./...                             │
│                 └─▶ Returns: {pass: bool, ...}            │
│                                                            │
│    9. if success {                                        │
│         git.Commit(sandbox)                               │
│         storage.CloseIssue(issue)                         │
│         storage.CreateIssues(uniqueIssues)                │
│         sandbox.Cleanup()                                 │
│       } else {                                            │
│         storage.ReleaseIssueAndReopen(issue)              │
│         storage.CreateIssues(blockerIssues)               │
│       }                                                    │
│  }                                                         │
└────────────────────────────────────────────────────────────┘
```

---

## Configuration

Key environment variables:

```bash
# Required
export ANTHROPIC_API_KEY=sk-...

# Executor
export VC_POLL_INTERVAL=5  # seconds between polls

# AI Supervision
export VC_ENABLE_AI_SUPERVISION=true  # Enable assess+analyze
export VC_SKIP_ASSESSMENT=false       # Skip pre-execution assessment
export VC_SKIP_ANALYSIS=false         # Skip post-execution analysis

# Deduplication
export VC_DEDUP_CONFIDENCE_THRESHOLD=0.85  # Similarity threshold
export VC_DEDUP_LOOKBACK_DAYS=7            # Compare against recent issues
export VC_DEDUP_MAX_CANDIDATES=25          # Limit comparisons
export VC_DEDUP_BATCH_SIZE=50              # Comparisons per AI call

# Quality Gates
export VC_GATE_TIMEOUT=300  # seconds (5 minutes)

# Sandbox
export VC_KEEP_SANDBOX_ON_FAILURE=true  # Retain failed sandboxes for debugging
```

---

## Performance Characteristics

**Executor Event Loop**:
- Poll interval: 5 seconds
- Claim operation: <10ms (SQLite transaction)
- Typical execution: 2-5 minutes per issue

**AI Calls per Issue**:
- Assessment: 1 call (optional, ~5-10s)
- Agent execution: 10-50 calls (background, ~2-4 min)
- Analysis: 1 call (optional, ~5-10s)
- Deduplication: ~1 call per 50 discovered issues (~5s)

**Quality Gates** (parallel):
- Tests: 1-30 seconds (depends on test suite)
- Lint: 5-10 seconds
- Build: 3-5 seconds
- Total: ~30 seconds (max of parallel gates)

**Database**:
- SQLite, single file (`.beads/vc.db`)
- Typical size: <10MB
- Query performance: <1ms for ready work queries

---

## Production Status

**Current State** (as of October 2025):
- ✅ **Core workflow operational** - 24 dogfooding missions, 90.9% quality gate pass rate
- ✅ **254 issues closed** - System has successfully built itself through dogfooding
- ✅ **All bootstrap phases complete** - Executor, AI supervision, gates, REPL all working
- ✅ **930+ tests** - Comprehensive test coverage across 121 test files
- ✅ **Beads integration** - Using Beads v0.12.0 library (100x performance improvement)

**What's Working**:
- Issue-oriented orchestration with atomic claiming
- AI-supervised execution (assess before, analyze after)
- Quality gates (test, lint, build, human approval)
- Sandbox isolation via git worktrees
- Deduplication of discovered issues (AI-powered)
- Watchdog convergence detection
- Health monitoring (ZFC violations, cruft detection)
- Activity feed with 20+ event types
- Natural language REPL interface
- Auto-commit with AI-generated messages (configurable)

**Known Limitations**:
- GitOps auto-merge disabled for safety (design complete, awaiting more dogfooding)
- Human intervention rate ~35% (target: <10%)
- Mission convergence works for simple cases, complex scenarios in development
- Event retention not yet implemented (punted until DB >100MB per vc-195)

---

## Future Evolution

**In Progress**:
1. Recursive refinement (vc-2) - Agent iterations until quality gates pass
2. Multi-executor coordination (vc-203) - Multiple executors on same repo
3. Advanced mission convergence - Complex multi-issue workflows

**Planned**:
4. GitOps auto-merge (vc-4) - Automatic PR creation and merging (design done)
5. Advanced health monitoring - Additional pattern detectors

**Long-term Vision**:
- Self-healing system that maintains its own codebase
- Autonomous bug fixing from production logs
- Continuous improvement through dogfooding
- Zero-touch code maintenance

---

## See Also

### Core Documentation
- [README.md](README.md) - Project overview and vision
- [CLAUDE.md](CLAUDE.md) - Instructions for AI agents working on VC
- [DOGFOODING.md](DOGFOODING.md) - Dogfooding workflow and mission logs
- [.beads/issues.jsonl](.beads/issues.jsonl) - Issue tracker source of truth

### Implementation Details
- [docs/ARCHITECTURE_AUDIT.md](docs/ARCHITECTURE_AUDIT.md) - Comprehensive implementation review
- [docs/EXPLORATION_FINDINGS.md](docs/EXPLORATION_FINDINGS.md) - Current state analysis
- [docs/architecture/](docs/architecture/) - Detailed design documents

### Historical
- [docs/archive/BOOTSTRAP.md](docs/archive/BOOTSTRAP.md) - Original 2-week roadmap (completed)

---

**Questions?** File an issue or ask in the REPL (`./vc`)
