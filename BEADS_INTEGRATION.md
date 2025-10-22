# Beads Integration: Using Beads as a Library in VC

**Status**: Proposed
**Created**: 2025-10-22
**Author**: Claude (with Steve Yegge)

## Executive Summary

VC should use **beads as a Go library** rather than shelling out to the `bd` CLI or maintaining a separate issue tracking implementation. This document explains why this architectural decision is critical for VC's success, how it aligns with the overall vision, and what the integration looks like.

## Background

### What is Beads?

Beads is a lightweight, file-based issue tracker built in Go that stores issues in JSONL (JSON Lines) format. It was originally created to track VC's development, but has evolved into a general-purpose issue tracker with unique capabilities:

- **JSONL as source of truth**: `.beads/issues.jsonl` checked into git
- **SQLite as cache**: `.beads/vc.db` derived from JSONL, rebuilt on demand
- **Dependency tracking**: Explicit `blocks`, `parent-child`, `discovered-from` relationships
- **Labels**: Flexible categorization and claiming rules
- **Epic hierarchy**: Missions → Phases → Tasks
- **CLI tool**: `bd` command for human interaction
- **Go library**: `internal/storage` package for programmatic access

### Current State

VC currently uses beads in a hybrid way:

1. **Development tracking**: We file issues via `bd create`, track work via `bd show`
2. **JSONL in git**: `.beads/issues.jsonl` is our source of truth
3. **Shell-out pattern**: Some VC code calls `bd` via `exec.Command()`
4. **Duplication risk**: VC has its own `internal/storage` that could diverge from beads

This hybrid approach works but has limitations as VC scales.

## The Vision: VC as a Self-Improving AI Colony

### Core Workflow

```
Human: "Add user authentication"
   ↓
AI translates to Mission epic
   ↓
AI generates phased plan (Phases as child epics)
   ↓
AI breaks phases into Tasks
   ↓
Executor claims Tasks atomically
   ↓
Agents execute Tasks in sandboxed branches
   ↓
Quality gates run (BUILD → TEST → LINT)
   ↓
AI analyzes results, creates follow-on issues
   ↓
REPEAT until Mission complete
   ↓
Gitops worker creates review issue
   ↓
Human approves → merge to main
```

### Why This Requires Beads Integration

**Issue tracker is the BRAIN of the system.**

Every decision, every piece of work, every dependency flows through the issue tracker:

- **Mission planning**: AI creates epic hierarchy
- **Work claiming**: Executor queries ready work atomically
- **Dependency tracking**: Blocking relationships prevent premature work
- **Discovered work**: Agents file new issues during execution
- **Quality gates**: Create blocking issues on failures
- **Terminal state detection**: Query epic completion
- **Gitops review**: Create review issues for human approval
- **Activity feed**: All events tied to issues

**If VC can't reliably interact with the issue tracker, it can't function.**

## Why Use Beads as a Library?

### ❌ **Problem 1: Shell-Out Pattern is Fragile**

```go
// Current approach (problematic)
cmd := exec.Command("bd", "create", title, "-t", "task", "-p", "2", "-d", description)
output, err := cmd.CombinedOutput()
if err != nil {
    // What went wrong? Parse stderr? Check exit code?
    // Did the issue get created? What's its ID?
    return fmt.Errorf("bd command failed: %w", err)
}

// Now parse output to extract issue ID... fragile!
issueID := extractIDFromOutput(string(output))
```

**Problems:**
- **Error handling**: Exit codes don't tell you what failed
- **Atomicity**: Can't transact across multiple operations
- **Performance**: Process spawn overhead (100ms+ per call)
- **Parsing**: Brittle string parsing of CLI output
- **Testing**: Hard to mock subprocess calls
- **Debugging**: Can't step through bd code in VC debugger

### ✅ **Solution: Direct Library Access**

```go
// Library approach (robust)
issue := &types.Issue{
    Title:       title,
    Description: description,
    Status:      types.StatusOpen,
    Priority:    types.PriorityP2,
    IssueType:   types.TypeTask,
}

// Direct function call, proper error handling
if err := beadsStore.CreateIssue(ctx, issue, "executor"); err != nil {
    return fmt.Errorf("failed to create issue: %w", err)
}

// issueID is immediately available
fmt.Printf("Created issue: %s\n", issue.ID)
```

**Benefits:**
- **Type safety**: Go structs, compile-time checks
- **Rich errors**: Structured error types with context
- **Atomicity**: Transactions across multiple operations
- **Performance**: No process spawning (microseconds vs milliseconds)
- **Testability**: Easy to mock storage interface
- **Debugging**: Step through storage code directly

### ❌ **Problem 2: Duplicate Storage Layers**

Currently we have TWO storage implementations:

1. **Beads storage** (`~/src/beads/internal/storage`)
   - SQLite backend
   - JSONL export/import
   - Used by `bd` CLI
   - Schema: issues, dependencies, labels, events

2. **VC storage** (`~/src/vc/internal/storage`)
   - SQLite backend
   - JSONL export/import
   - Used by VC executor
   - Schema: issues, dependencies, labels, events, **executor_instances, issue_execution_state**

**Problems:**
- **Divergence risk**: Features added to one not added to other
- **Maintenance burden**: Bug fixes duplicated
- **Schema drift**: Tables/columns become incompatible
- **Testing overhead**: Same functionality tested twice
- **Migration pain**: Schema changes require coordination

### ✅ **Solution: Single Storage Layer**

**Beads storage becomes the single source of truth:**

```
beads/internal/storage/
├── storage.go           # Interface (used by both)
├── sqlite/
│   ├── sqlite.go        # Core implementation
│   ├── issues.go        # Issue CRUD
│   ├── dependencies.go  # Dependency graph
│   ├── labels.go        # Labels
│   ├── events.go        # Activity feed
│   └── execution.go     # VC-specific: executor state
└── jsonl/
    ├── export.go        # JSONL export
    └── import.go        # JSONL import
```

**VC imports beads storage:**
```go
import "github.com/steveyegge/beads/internal/storage"

store, err := storage.NewStorage(ctx, storage.Config{
    Type: storage.TypeSQLite,
    Path: ".beads/vc.db",
})
```

**Benefits:**
- **Single implementation**: One codebase to maintain
- **Schema sync**: Impossible to drift
- **Feature sharing**: Beads improvements benefit VC automatically
- **Testing**: Test once, works everywhere
- **Migration**: Schema changes happen atomically

### ❌ **Problem 3: Atomic Operations Are Hard**

When creating a mission with phases:

```go
// Shell-out approach (NOT ATOMIC)
mission := exec.Command("bd", "create", title, "-t", "epic")
missionOutput, _ := mission.CombinedOutput()
missionID := parseID(missionOutput)

phase1 := exec.Command("bd", "create", phase1Title, "-t", "epic")
phase1Output, _ := phase1.CombinedOutput()
phase1ID := parseID(phase1Output)

// What if this fails? mission and phase1 are already created!
dep := exec.Command("bd", "dep", "add", phase1ID, missionID, "--type", "parent-child")
if err := dep.Run(); err != nil {
    // Orphaned issues! No way to rollback!
    return err
}
```

### ✅ **Solution: Transactions**

```go
// Library approach with transaction
tx, err := beadsStore.Begin(ctx)
if err != nil {
    return err
}
defer tx.Rollback()

mission := &types.Issue{...}
if err := tx.CreateIssue(ctx, mission, "ai-planner"); err != nil {
    return err
}

phase1 := &types.Issue{...}
if err := tx.CreateIssue(ctx, phase1, "ai-planner"); err != nil {
    return err
}

dep := &types.Dependency{
    IssueID:     phase1.ID,
    DependsOnID: mission.ID,
    Type:        types.DepParentChild,
}
if err := tx.AddDependency(ctx, dep, "ai-planner"); err != nil {
    return err
}

// Atomic commit - all or nothing
return tx.Commit()
```

**Benefits:**
- **Atomicity**: All operations succeed or none do
- **Consistency**: No orphaned issues or broken dependencies
- **Rollback**: Easy error recovery
- **Performance**: Bulk operations batched

### ❌ **Problem 4: Mission-Scoped Queries Are Slow**

```bash
# How do we find all issues for mission vc-26?
# Shell approach: parse output of multiple bd commands

# 1. Get mission
bd show vc-26

# 2. Get all children (recursive!)
bd dep tree vc-26 | grep -E "vc-[0-9]+" | ...

# 3. For each child, check status
for issue in $issues; do
    bd show $issue | grep "Status:" | ...
done

# 4. Aggregate results
# This takes SECONDS for a large mission
```

### ✅ **Solution: Efficient SQL Queries**

```go
// Library approach: single SQL query
incomplete, err := beadsStore.CountDependents(ctx, "vc-26",
    storage.StatusFilter{Open, InProgress, Blocked})

// Returns immediately (milliseconds)
if incomplete == 0 {
    // Mission complete!
}
```

**Enables:**
- **Fast terminal state detection**: Check all missions in < 10ms
- **Efficient ready work queries**: `SELECT ... WHERE NOT EXISTS (SELECT ...)`
- **Complex filters**: Labels + dependencies + status in one query
- **Batch operations**: Update hundreds of issues in transaction

### ❌ **Problem 5: Testing is Brittle**

```go
// Testing shell-out code
func TestCreateIssue(t *testing.T) {
    // Mock exec.Command? Fragile!
    // Run actual bd command? Slow + requires bd in PATH
    // Parse actual output? Breaks when output format changes
}
```

### ✅ **Solution: Interface-Based Mocking**

```go
// Storage interface
type Storage interface {
    CreateIssue(ctx context.Context, issue *Issue, actor string) error
    GetIssue(ctx context.Context, id string) (*Issue, error)
    // ... all operations
}

// Real implementation
type sqliteStorage struct { ... }

// Mock for testing
type mockStorage struct {
    issues map[string]*Issue
}

func TestCreateIssue(t *testing.T) {
    store := &mockStorage{issues: make(map[string]*Issue)}

    issue := &types.Issue{Title: "Test"}
    err := store.CreateIssue(ctx, issue, "test")

    assert.NoError(t, err)
    assert.Equal(t, "vc-1", issue.ID)
}
```

**Benefits:**
- **Fast tests**: No subprocess spawning, no disk I/O
- **Predictable**: Mock storage returns exactly what you configure
- **Coverage**: Test error paths easily (mock returns errors)
- **Isolated**: Tests don't interfere with each other

## Architecture: How the Integration Works

### Repository Structure

```
~/src/beads/          # Beads repository (library + CLI)
├── cmd/bd/           # CLI tool (humans use this)
├── internal/
│   ├── storage/      # ⭐ Storage interface (VC imports this)
│   ├── types/        # ⭐ Issue, Dependency, Label types
│   └── ...
└── go.mod            # module: github.com/steveyegge/beads

~/src/vc/             # VC repository (imports beads)
├── cmd/vc/           # VC CLI
├── internal/
│   ├── executor/     # Uses beads.Storage
│   ├── ai/           # Creates issues via beads.Storage
│   ├── gates/        # Creates blocking issues via beads.Storage
│   ├── repl/         # Queries issues via beads.Storage
│   └── ...
├── go.mod            # requires: github.com/steveyegge/beads v0.1.0
└── .beads/
    ├── issues.jsonl  # Source of truth (shared with beads)
    └── vc.db         # SQLite cache (beads storage reads/writes this)
```

### Import Pattern

```go
// VC code
import (
    beads "github.com/steveyegge/beads/internal/storage"
    "github.com/steveyegge/beads/internal/types"
)

// Create storage instance
store, err := beads.NewStorage(ctx, beads.Config{
    Type: beads.TypeSQLite,
    Path: ".beads/vc.db",
})

// Use storage
issue := &types.Issue{...}
if err := store.CreateIssue(ctx, issue, "executor"); err != nil {
    return err
}
```

### Shared Database

**Both `bd` and `vc` use the SAME database file:**

```
.beads/
├── issues.jsonl      # Source of truth (git committed)
└── vc.db             # SQLite cache (git ignored)
```

**Operations:**

```bash
# Human uses bd CLI
$ bd create "Fix authentication bug"
# Writes to .beads/vc.db, exports to issues.jsonl

# VC executor uses library
$ ./vc execute
# Reads from .beads/vc.db, writes to .beads/vc.db

# Both see same data!
$ bd show vc-123  # Shows issue created by VC
$ vc activity     # Shows issues created by bd
```

**Synchronization:**

```bash
# After git pull (JSONL changed)
$ bd import .beads/issues.jsonl
# Rebuilds .beads/vc.db from JSONL

# Before git commit (DB changed)
$ bd export -o .beads/issues.jsonl
# Updates JSONL from .beads/vc.db

# Git only tracks JSONL
$ git add .beads/issues.jsonl
$ git commit -m "Update issues"
```

## VC-Specific Extensions: Pure Beads Approach

**UPDATE 2025-10-22**: After design review, we've decided to use **pure beads primitives** instead of custom executor tables.

### Design Philosophy

**We use ONLY beads primitives** - no custom executor tables like `executor_instances` or `issue_execution_state`.

**Why?**
- ✅ Simpler (fewer tables, fewer JOINs)
- ✅ Everything exports to JSONL (state in git)
- ✅ No schema coordination between beads and VC
- ✅ Easier testing (mock one store, not four tables)

### What VC Adds to Beads

**1. New columns in issues table:**
```sql
ALTER TABLE issues ADD COLUMN subtype TEXT;       -- 'mission', 'phase', 'review'
ALTER TABLE issues ADD COLUMN sandbox_path TEXT;  -- '.sandboxes/mission-300/'
ALTER TABLE issues ADD COLUMN branch_name TEXT;   -- 'mission/vc-300-user-auth'
```

**2. Labels table** (if not already in beads):
```sql
CREATE TABLE IF NOT EXISTS labels (
  id INTEGER PRIMARY KEY,
  issue_id TEXT NOT NULL,
  label TEXT NOT NULL,
  added_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
  added_by TEXT,
  FOREIGN KEY (issue_id) REFERENCES issues(id),
  UNIQUE(issue_id, label)
);
```

**3. Activity feed table** (VC-specific events):
```sql
CREATE TABLE agent_events (
  id INTEGER PRIMARY KEY,
  timestamp TIMESTAMP,
  issue_id TEXT,
  type TEXT,  -- 'agent_spawned', 'agent_tool_use', 'quality_gates', etc.
  severity TEXT,
  message TEXT,
  data TEXT,  -- JSON blob
  FOREIGN KEY (issue_id) REFERENCES issues(id)
);
```

**That's it!** No executor_instances, no issue_execution_state.

### State Storage Using Pure Beads

| State | Storage | Example |
|-------|---------|---------|
| Mission sandbox | issue.sandbox_path | `.sandboxes/mission-300` |
| Mission lifecycle | labels | `needs-quality-gates`, `approved` |
| Task claiming | issue.status | `open` → `in_progress` → `closed` |
| Execution history | agent_events | `agent_spawned`, `gates_failed` |
| Orphan detection | issue.updated_at | Watchdog resets stale `in_progress` |

### Orphan Detection Without Executor Tracking

**Watchdog runs every 5 minutes:**
```go
// Find stale in_progress issues
stale, err := store.ListIssues(ctx, &storage.ListOptions{
  Status: storage.StatusInProgress,
  UpdatedBefore: time.Now().Add(-30 * time.Minute),
})

for _, issue := range stale {
  // Reset to open
  store.UpdateIssue(ctx, issue.ID, map[string]interface{}{
    "status": "open",
  }, "watchdog")
}
```

**No executor tracking needed!**

### Benefits

**Activity feed queries:**
- VC activity feed queries are just SQL queries
- `bd` can potentially show VC events (future: `bd activity`)
- Schema migrations handled by beads
- Testing uses same mock storage

**Storage interface stays simple:**
```go
// In beads/internal/storage/storage.go
type Storage interface {
    // Core operations (used by both bd and VC)
    CreateIssue(ctx context.Context, issue *Issue, actor string) error
    GetIssue(ctx context.Context, id string) (*Issue, error)
    UpdateIssue(ctx context.Context, id string, updates map[string]interface{}, actor string) error

    // Label operations (used by VC mission workflow)
    AddLabel(ctx context.Context, issueID string, label string, actor string) error
    RemoveLabel(ctx context.Context, issueID string, label string, actor string) error
    GetLabels(ctx context.Context, issueID string) ([]string, error)

    // Activity feed (VC-specific)
    LogEvent(ctx context.Context, event *AgentEvent) error
    GetEvents(ctx context.Context, opts *EventQueryOptions) ([]*AgentEvent, error)
}
```

No VC-specific executor methods needed!

## Migration Path

### Phase 1: Dual Storage (Current State)

**Status**: ✅ Done

- VC has its own `internal/storage`
- Some code shells out to `bd`
- Works but has limitations

### Phase 2: Import Beads Types

**Status**: 🔄 In Progress

```bash
# In vc/go.mod
go get github.com/steveyegge/beads@latest
```

```go
// Replace vc/internal/types with beads types
import "github.com/steveyegge/beads/internal/types"

// Keep using vc/internal/storage for now
```

**Benefits:**
- Type compatibility with beads
- Can pass Issue structs between systems
- Reduces divergence risk

### Phase 3: Import Beads Storage

**Timeline**: 1-2 weeks

```go
// Replace vc/internal/storage with beads storage
import beads "github.com/steveyegge/beads/internal/storage"

// Update all code using storage.Storage interface
store, err := beads.NewStorage(ctx, beads.Config{...})
```

**Migration steps:**
1. Update all VC code to use beads.Storage interface
2. Run migration to add VC-specific tables to beads schema
3. Test with dual imports (both storages) to verify compatibility
4. Remove vc/internal/storage entirely
5. Update tests to use beads mocks

**Risks:**
- Breaking changes if beads storage API changes
- Need to coordinate schema migrations
- Testing overhead during transition

**Mitigation:**
- Pin beads version in go.mod until migration complete
- Run integration tests continuously
- Keep vc/internal/storage as fallback initially

### Phase 4: Remove Shell-Outs

**Timeline**: 1 week (after Phase 3)

Find all `exec.Command("bd", ...)` calls and replace:

```go
// Before
cmd := exec.Command("bd", "show", issueID)
output, _ := cmd.CombinedOutput()
issue := parseOutput(output)  // Fragile!

// After
issue, err := store.GetIssue(ctx, issueID)  // Direct!
```

**Benefits:**
- Faster (no process spawning)
- More reliable (no parsing)
- Better error messages
- Easier testing

### Phase 5: Advanced Features

**Timeline**: Ongoing

Once using beads as library:

- **Atomic mission creation**: Create mission + phases + dependencies in one transaction
- **Efficient terminal state detection**: SQL query for mission completion
- **Fast ready work queries**: Complex filters in single query
- **Batch operations**: Create 100 discovered issues in one transaction
- **Activity feed integration**: Query events alongside issues
- **Real-time updates**: Watch for changes via database triggers

## Interface Design

### Core Storage Interface

```go
// In beads/internal/storage/storage.go
package storage

type Storage interface {
    // Issue operations
    CreateIssue(ctx context.Context, issue *Issue, actor string) error
    GetIssue(ctx context.Context, id string) (*Issue, error)
    UpdateIssue(ctx context.Context, id string, updates map[string]interface{}, actor string) error
    CloseIssue(ctx context.Context, id string, reason string, actor string) error
    ListIssues(ctx context.Context, opts *ListOptions) ([]*Issue, error)

    // Dependency operations
    AddDependency(ctx context.Context, dep *Dependency, actor string) error
    GetDependencies(ctx context.Context, issueID string) ([]*Dependency, error)
    GetDependents(ctx context.Context, issueID string) ([]*Issue, error)
    CountDependents(ctx context.Context, issueID string, filter StatusFilter) (int, error)

    // Label operations
    AddLabel(ctx context.Context, issueID string, label string, actor string) error
    RemoveLabel(ctx context.Context, issueID string, label string, actor string) error
    GetLabels(ctx context.Context, issueID string) ([]string, error)

    // Comment operations
    AddComment(ctx context.Context, issueID string, actor string, comment string) error
    GetComments(ctx context.Context, issueID string) ([]*Comment, error)

    // Transaction support
    Begin(ctx context.Context) (Transaction, error)

    // JSONL export/import
    Export(ctx context.Context, path string) error
    Import(ctx context.Context, path string) error

    // Executor operations (VC-specific)
    CreateExecutorInstance(ctx context.Context, instance *ExecutorInstance) error
    UpdateExecutorHeartbeat(ctx context.Context, instanceID string) error
    GetStaleExecutors(ctx context.Context, timeout time.Duration) ([]*ExecutorInstance, error)
    UpdateExecutionState(ctx context.Context, issueID string, state ExecutionState) error
    GetExecutionState(ctx context.Context, issueID string) (*ExecutionState, error)

    // Activity feed (VC-specific)
    LogEvent(ctx context.Context, event *AgentEvent) error
    GetEvents(ctx context.Context, opts *EventQueryOptions) ([]*AgentEvent, error)
}

type Transaction interface {
    Storage  // Inherits all Storage methods
    Commit() error
    Rollback() error
}
```

### Configuration

```go
type Config struct {
    Type    StorageType  // "sqlite", "memory"
    Path    string       // Database path
    Options map[string]interface{}  // Backend-specific options
}

type StorageType string
const (
    TypeSQLite StorageType = "sqlite"
    TypeMemory StorageType = "memory"  // For testing
)
```

### Example Usage

```go
// Create storage
store, err := beads.NewStorage(ctx, beads.Config{
    Type: beads.TypeSQLite,
    Path: ".beads/vc.db",
})
if err != nil {
    return err
}
defer store.Close()

// Create mission with phases atomically
tx, err := store.Begin(ctx)
if err != nil {
    return err
}
defer tx.Rollback()

mission := &types.Issue{
    Title:       "Add user authentication",
    Description: "Implement OAuth2 authentication",
    Status:      types.StatusOpen,
    Priority:    types.PriorityP1,
    IssueType:   types.TypeEpic,
    IssueSubtype: types.SubtypeMission,
}
if err := tx.CreateIssue(ctx, mission, "ai-planner"); err != nil {
    return err
}

phase1 := &types.Issue{
    Title:       "Phase 1: OAuth provider setup",
    IssueType:   types.TypeEpic,
    IssueSubtype: types.SubtypePhase,
    // ...
}
if err := tx.CreateIssue(ctx, phase1, "ai-planner"); err != nil {
    return err
}

dep := &types.Dependency{
    IssueID:     phase1.ID,
    DependsOnID: mission.ID,
    Type:        types.DepParentChild,
}
if err := tx.AddDependency(ctx, dep, "ai-planner"); err != nil {
    return err
}

// Atomic commit
if err := tx.Commit(); err != nil {
    return err
}

fmt.Printf("Created mission %s with phase %s\n", mission.ID, phase1.ID)
```

## Benefits Summary

### Performance

| Operation | Shell-out (current) | Library (proposed) |
|-----------|---------------------|--------------------|
| Create issue | ~100ms (process spawn) | ~1ms (function call) |
| Query ready work | ~500ms (multiple processes) | ~5ms (single SQL query) |
| Create mission + 5 phases | ~600ms (6 processes) | ~10ms (one transaction) |
| Check mission complete | ~2000ms (recursive tree walk) | ~2ms (SQL COUNT query) |

**Result:** ~100x faster for critical operations

### Reliability

| Scenario | Shell-out | Library |
|----------|-----------|---------|
| Partial failure (mission created, phase fails) | ⚠️ Orphaned mission | ✅ Rollback transaction |
| Parse error in CLI output | ❌ Silent failure | ✅ Type-safe structs |
| Schema change in beads | ❌ CLI breaks VC | ✅ Compile error (caught early) |
| bd not in PATH | ❌ Runtime error | ✅ Import error (caught at build) |

### Maintainability

| Aspect | Dual Storage | Beads Library |
|--------|--------------|---------------|
| Lines of code | ~5000 (duplicated) | ~2500 (shared) |
| Schema migrations | Coordinate 2 repos | Single source of truth |
| Bug fixes | Apply to both | Fix once |
| Testing | Mock both | Mock once |
| Feature additions | Implement twice | Implement once |

## Risks and Mitigations

### Risk: Beads API instability

**Risk:** Beads storage API changes break VC

**Mitigation:**
- Pin beads version in go.mod
- Semantic versioning (breaking changes = major version bump)
- Deprecation warnings before API changes
- Compatibility shims for gradual migration

### Risk: Tight coupling

**Risk:** VC becomes too dependent on beads internals

**Mitigation:**
- VC only imports public storage interface
- Beads storage interface is stable and well-documented
- VC can implement its own Storage backend if needed
- Interface allows swapping implementations (SQLite → Postgres later)

### Risk: Schema conflicts

**Risk:** VC needs tables that conflict with beads

**Mitigation:**
- Prefix VC-specific tables with `vc_` or `executor_`
- Beads schema reserves namespace for extensions
- Schema migrations coordinated between repos
- Separate migration folders: `beads/migrations/` and `vc/migrations/`

### Risk: Performance regression

**Risk:** Shared database causes lock contention

**Mitigation:**
- SQLite WAL mode for concurrent reads
- VC uses transactions for bulk operations
- Monitoring for slow queries
- Can partition into separate DBs if needed (main vs executor)

## Alternatives Considered

### Alternative 1: Keep Dual Storage

**Pros:**
- No migration work
- Complete independence

**Cons:**
- Maintenance burden
- Divergence risk
- Duplicate testing
- Slower performance (shell-outs)

**Verdict:** ❌ Not sustainable long-term

### Alternative 2: VC Implements Its Own Tracker

**Pros:**
- Full control
- Optimized for VC

**Cons:**
- Reinventing the wheel
- Beads lessons lost
- No human-readable issue tracker (no `bd show`)
- Can't use JSONL → git workflow

**Verdict:** ❌ Worse than using beads

### Alternative 3: REST API Between Beads and VC

**Pros:**
- Loose coupling
- Language-agnostic

**Cons:**
- Network overhead
- Serialization cost
- Authentication complexity
- Can't use transactions
- Requires running server

**Verdict:** ❌ Overkill for same-machine usage

### Alternative 4: Shared JSONL Files Only

**Pros:**
- Simple
- Git-friendly

**Cons:**
- No efficient queries (must parse entire file)
- No atomic operations
- No concurrent access
- Linear scan for every query
- Can't index by labels, status, etc.

**Verdict:** ❌ Too slow for production

## Conclusion

**Using beads as a Go library is the right architectural decision for VC.**

It provides:
- ✅ **Performance**: 100x faster than shell-outs
- ✅ **Reliability**: Atomic operations, transactions
- ✅ **Maintainability**: Single source of truth
- ✅ **Correctness**: Type safety, compile-time checks
- ✅ **Testability**: Easy mocking, fast tests
- ✅ **Scalability**: Efficient SQL queries
- ✅ **Integration**: Seamless with beads CLI

The migration path is clear, risks are manageable, and benefits are substantial.

**Next Steps:**

1. Finalize beads storage interface (make it stable API)
2. Export beads storage as public package
3. Update VC to import beads types (Phase 2)
4. Migrate VC to use beads storage (Phase 3)
5. Remove VC storage implementation
6. Remove all shell-outs to `bd`
7. Add VC-specific tables to beads schema
8. Profit! 🚀

---

**Questions? Concerns? Feedback?**

Discuss in issue tracking: `bd create "Discussion: Beads library integration" -t task`
