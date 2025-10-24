# Beads Extensibility: VC as an Extension (Not Intrusion)

**Created**: 2025-10-22
**Context**: Review of beads library integration revealed VC-specific schema needs
**Goal**: Design VC to extend Beads without polluting it

---

## The Problem

Initial review recommended adding VC-specific fields to Beads:

```sql
-- ❌ BAD: Pollutes Beads schema with VC-specific concepts
ALTER TABLE issues ADD COLUMN subtype TEXT;       -- VC missions
ALTER TABLE issues ADD COLUMN sandbox_path TEXT;  -- VC sandboxes
ALTER TABLE issues ADD COLUMN branch_name TEXT;   -- VC git branches

CREATE TABLE agent_events (...);  -- VC activity feed
```

**Why this is wrong:**
- Beads is a standalone utility with thousands of users
- VC-specific concepts don't belong in a general-purpose issue tracker
- Pollutes Beads with workflow engine details
- Violates separation of concerns

**The IntelliJ/Android Studio Model:**
- IntelliJ IDEA: General-purpose IDE platform
- Android Studio: Plugin (as large as IDEA itself)
- IDEA has NO Android-specific code
- IDEA evolved extensibility points to support AS
- AS uses extension APIs, doesn't modify core

**We should do the same**: Beads is the platform, VC is the extension.

---

## SQLite Extensibility: Can We Add Tables and Columns?

### Adding Tables to Existing Database ✅ YES

SQLite supports **extension tables** - additional tables in the same database file:

```sql
-- Beads owns these tables:
CREATE TABLE issues (...);
CREATE TABLE dependencies (...);
CREATE TABLE labels (...);

-- VC adds extension tables (Beads doesn't know about them):
CREATE TABLE IF NOT EXISTS vc_mission_state (...);
CREATE TABLE IF NOT EXISTS vc_agent_events (...);
CREATE TABLE IF NOT EXISTS vc_sandbox_metadata (...);
```

**Foreign keys work across extension tables:**

```sql
CREATE TABLE vc_mission_state (
    issue_id TEXT PRIMARY KEY,
    subtype TEXT,  -- 'mission', 'phase'
    sandbox_path TEXT,
    branch_name TEXT,
    FOREIGN KEY (issue_id) REFERENCES issues(id) ON DELETE CASCADE
);
```

**✅ This works perfectly!** Extension tables coexist with core tables.

### Adding Columns to Existing Tables ⚠️ POSSIBLE BUT RISKY

SQLite supports `ALTER TABLE ADD COLUMN`:

```sql
-- Beads schema (original)
CREATE TABLE issues (
    id TEXT PRIMARY KEY,
    title TEXT,
    ...
);

-- VC adds columns
ALTER TABLE issues ADD COLUMN vc_subtype TEXT;
ALTER TABLE issues ADD COLUMN vc_sandbox_path TEXT;
```

**Problems:**
1. **Beads export doesn't know about new columns** - JSONL export breaks
2. **Schema drift** - Beads schema and actual schema differ
3. **Migration conflicts** - Beads migrations could conflict with VC columns
4. **Breaking changes** - If Beads adds same column name, collision

**❌ Don't do this.** Use extension tables instead.

---

## Recommended Architecture: Extension Tables

### VC Extension Schema

**VC creates its own tables** in the same `.beads/vc.db` file:

```sql
-- Mission state (maps issue_id → mission metadata)
CREATE TABLE IF NOT EXISTS vc_mission_state (
    issue_id TEXT PRIMARY KEY,
    subtype TEXT NOT NULL,      -- 'mission', 'phase', 'review'
    sandbox_path TEXT,           -- '.sandboxes/mission-300/'
    branch_name TEXT,            -- 'mission/vc-300-user-auth'
    iteration_count INTEGER DEFAULT 0,
    last_gates_run DATETIME,
    gates_status TEXT,           -- 'pending', 'running', 'passed', 'failed'
    FOREIGN KEY (issue_id) REFERENCES issues(id) ON DELETE CASCADE
);

CREATE INDEX idx_vc_mission_subtype ON vc_mission_state(subtype);
CREATE INDEX idx_vc_mission_gates ON vc_mission_state(gates_status);

-- Agent events (activity feed)
CREATE TABLE IF NOT EXISTS vc_agent_events (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    timestamp DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    issue_id TEXT,
    type TEXT NOT NULL,
    severity TEXT,
    message TEXT NOT NULL,
    data TEXT,  -- JSON blob
    FOREIGN KEY (issue_id) REFERENCES issues(id) ON DELETE CASCADE
);

CREATE INDEX idx_vc_agent_events_issue ON vc_agent_events(issue_id);
CREATE INDEX idx_vc_agent_events_timestamp ON vc_agent_events(timestamp);
CREATE INDEX idx_vc_agent_events_type ON vc_agent_events(type);

-- Executor instances (optional - can use watchdog instead)
-- CREATE TABLE IF NOT EXISTS vc_executor_instances (...);
```

### How VC Queries Work

**Get mission with metadata:**

```go
// VC code - JOIN extension table
func (s *VCStorage) GetMission(ctx context.Context, issueID string) (*Mission, error) {
    query := `
        SELECT
            i.id, i.title, i.status, i.priority,
            m.subtype, m.sandbox_path, m.branch_name,
            m.iteration_count, m.gates_status
        FROM issues i
        LEFT JOIN vc_mission_state m ON i.id = m.issue_id
        WHERE i.id = ?
    `

    var mission Mission
    err := s.db.QueryRowContext(ctx, query, issueID).Scan(
        &mission.ID, &mission.Title, &mission.Status, &mission.Priority,
        &mission.Subtype, &mission.SandboxPath, &mission.BranchName,
        &mission.IterationCount, &mission.GatesStatus,
    )
    return &mission, err
}
```

**Find ready missions:**

```go
// VC code - JOIN to filter by mission-specific criteria
func (s *VCStorage) GetReadyMissions(ctx context.Context) ([]*Mission, error) {
    query := `
        SELECT i.id, i.title, m.sandbox_path
        FROM issues i
        JOIN labels l ON i.id = l.issue_id
        JOIN vc_mission_state m ON i.id = m.issue_id
        WHERE i.type = 'epic'
          AND i.status = 'open'
          AND l.label = 'needs-quality-gates'
          AND m.subtype = 'mission'
        ORDER BY i.priority, i.created_at
    `
    // ... execute query
}
```

**Log agent event:**

```go
// VC code - insert into extension table
func (s *VCStorage) LogAgentEvent(ctx context.Context, event *AgentEvent) error {
    _, err := s.db.ExecContext(ctx, `
        INSERT INTO vc_agent_events (issue_id, type, severity, message, data)
        VALUES (?, ?, ?, ?, ?)
    `, event.IssueID, event.Type, event.Severity, event.Message, event.Data)
    return err
}
```

### Benefits of Extension Tables

1. **Beads stays clean** ✅
   - No VC-specific columns or tables
   - Beads export works unchanged
   - Beads migrations never conflict

2. **VC has full control** ✅
   - Can add any tables/columns needed
   - Can evolve schema independently
   - Migrations managed by VC

3. **Foreign keys work** ✅
   - `FOREIGN KEY (issue_id) REFERENCES issues(id)`
   - Cascade deletes work
   - Referential integrity enforced

4. **Shared database** ✅
   - Both use `.beads/vc.db`
   - No synchronization needed
   - Single source of truth

5. **Labels still work** ✅
   - Beads `labels` table is core
   - VC adds labels for state machine
   - No pollution of Beads schema

---

## What Beads Needs to Support This

### Already Supported ✅

1. **Shared database access**
   - ✅ Beads exports `NewSQLiteStorage(dbPath)`
   - ✅ VC can open same database
   - ✅ No conflicts (both use transactions)

2. **Foreign keys**
   - ✅ Beads enables foreign keys
   - ✅ Extension tables can reference core tables

3. **Labels API**
   - ✅ `AddLabel`, `RemoveLabel`, `GetLabels`
   - ✅ VC uses labels for state machine

4. **Storage interface**
   - ✅ All CRUD operations
   - ✅ Dependency operations
   - ✅ Ready work queries

### Nice to Have (Future Extensibility)

1. **Schema migration hooks** (Optional)
   ```go
   // Hypothetical: Beads calls extension hooks after migration
   type Extension interface {
       Name() string
       Migrate(db *sql.DB) error
   }

   beads.RegisterExtension(&VCExtension{})
   ```

2. **Extension table registration** (Optional)
   ```go
   // Hypothetical: Beads knows about extension tables for export
   beads.RegisterExtensionTables("vc_mission_state", "vc_agent_events")
   // Then JSONL export includes them
   ```

3. **Generic metadata columns** (If useful for multiple extensions)
   ```sql
   -- Beads could add generic extension point
   ALTER TABLE issues ADD COLUMN metadata TEXT;  -- JSON blob

   -- Extensions use it:
   UPDATE issues SET metadata = '{"vc_subtype": "mission"}' WHERE id = 'vc-300';
   ```

   **But**: Extension tables are cleaner than JSON blobs.

**For now**: VC doesn't need any of these. Extension tables are sufficient.

---

## VC Storage Layer Design

### Architecture

```
VC
├── Uses Beads storage for core operations
│   import "github.com/steveyegge/beads"
│   store := beads.NewSQLiteStorage(".beads/vc.db")
│
└── Adds extension layer for VC-specific operations
    internal/storage/
    ├── vc_storage.go       # VC extension wrapper
    ├── mission_state.go    # Mission metadata operations
    ├── agent_events.go     # Activity feed operations
    └── schema.go           # VC extension schema (DDL)
```

### Implementation

**`internal/storage/vc_storage.go`:**

```go
package storage

import (
    "context"
    "database/sql"
    "github.com/steveyegge/beads"
)

// VCStorage wraps beads storage and adds VC extensions
type VCStorage struct {
    beads.Storage  // Embedded - all beads operations available
    db *sql.DB     // Direct DB access for extension tables
}

// NewVCStorage creates VC storage with extension tables
func NewVCStorage(ctx context.Context, dbPath string) (*VCStorage, error) {
    // 1. Open beads storage (creates core tables)
    beadsStore, err := beads.NewSQLiteStorage(dbPath)
    if err != nil {
        return nil, err
    }

    // 2. Get underlying DB connection
    db := getUnderlyingDB(beadsStore) // Need to expose this in beads

    // 3. Create VC extension tables
    if err := createVCExtensionTables(ctx, db); err != nil {
        return nil, err
    }

    return &VCStorage{
        Storage: beadsStore,
        db:      db,
    }, nil
}

// VC-specific methods
func (s *VCStorage) CreateMission(ctx context.Context, mission *Mission) error {
    // 1. Create issue in Beads
    issue := &beads.Issue{
        Title:      mission.Title,
        Status:     beads.StatusOpen,
        Priority:   mission.Priority,
        IssueType:  beads.TypeEpic,
        // ... other fields
    }

    if err := s.Storage.CreateIssue(ctx, issue, mission.Actor); err != nil {
        return err
    }

    // 2. Add mission metadata in extension table
    _, err := s.db.ExecContext(ctx, `
        INSERT INTO vc_mission_state (issue_id, subtype, sandbox_path, branch_name)
        VALUES (?, ?, ?, ?)
    `, issue.ID, mission.Subtype, mission.SandboxPath, mission.BranchName)

    return err
}

func (s *VCStorage) GetMission(ctx context.Context, issueID string) (*Mission, error) {
    // JOIN core issue + extension metadata
    query := `
        SELECT i.*, m.subtype, m.sandbox_path, m.branch_name
        FROM issues i
        LEFT JOIN vc_mission_state m ON i.id = m.issue_id
        WHERE i.id = ?
    `
    // ... execute and return
}
```

**`internal/storage/schema.go`:**

```go
package storage

const vcExtensionSchema = `
-- VC Extension Tables
-- These tables extend Beads issues with mission workflow metadata

CREATE TABLE IF NOT EXISTS vc_mission_state (
    issue_id TEXT PRIMARY KEY,
    subtype TEXT NOT NULL CHECK(subtype IN ('mission', 'phase', 'review')),
    sandbox_path TEXT,
    branch_name TEXT,
    iteration_count INTEGER DEFAULT 0,
    last_gates_run DATETIME,
    gates_status TEXT CHECK(gates_status IN ('pending', 'running', 'passed', 'failed')),
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (issue_id) REFERENCES issues(id) ON DELETE CASCADE
);

CREATE INDEX idx_vc_mission_subtype ON vc_mission_state(subtype);
CREATE INDEX idx_vc_mission_gates ON vc_mission_state(gates_status);

CREATE TABLE IF NOT EXISTS vc_agent_events (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    timestamp DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    issue_id TEXT,
    type TEXT NOT NULL,
    severity TEXT CHECK(severity IN ('info', 'warning', 'error')),
    message TEXT NOT NULL,
    data TEXT,
    FOREIGN KEY (issue_id) REFERENCES issues(id) ON DELETE CASCADE
);

CREATE INDEX idx_vc_agent_events_issue ON vc_agent_events(issue_id);
CREATE INDEX idx_vc_agent_events_timestamp ON vc_agent_events(timestamp);
CREATE INDEX idx_vc_agent_events_type ON vc_agent_events(type);
`

func createVCExtensionTables(ctx context.Context, db *sql.DB) error {
    _, err := db.ExecContext(ctx, vcExtensionSchema)
    return err
}
```

---

## Beads API Requirements

For this to work, Beads needs to expose:

### 1. Underlying Database Connection

**Current**: `Storage` interface doesn't expose `*sql.DB`

**Needed**:

```go
// Option A: Add to Storage interface
type Storage interface {
    // ... existing methods ...

    // UnderlyingDB returns the database connection for advanced operations
    // Extensions can use this to create their own tables
    UnderlyingDB() *sql.DB
}

// Option B: Type assertion (less clean)
type SQLiteStorage interface {
    Storage
    DB() *sql.DB
}

store := beads.NewSQLiteStorage(dbPath)
sqliteStore := store.(SQLiteStorage)
db := sqliteStore.DB()
```

**Recommendation**: Option A (add to interface). This is a common extensibility pattern.

### 2. Transaction Support (Already Exists?)

**Needed for atomic operations**:

```go
// Does beads have this?
type Storage interface {
    Begin(ctx context.Context) (Transaction, error)
}

type Transaction interface {
    Storage  // All storage methods work in transaction
    Commit() error
    Rollback() error
}
```

If not, VC needs this for atomic mission creation (issue + extension metadata).

---

## Export Strategy: JSONL

### Problem

Beads exports `.beads/issues.jsonl` with core data:

```json
{"id":"vc-300","title":"Add user auth","type":"epic",...}
```

But VC extension tables (`vc_mission_state`, `vc_agent_events`) are **not** exported.

### Solutions

#### Option 1: VC-Specific JSONL Files (Simplest)

```
.beads/
├── issues.jsonl        # Beads export (core issues)
├── vc-missions.jsonl   # VC export (mission metadata)
└── vc-events.jsonl     # VC export (agent events)
```

**Pros:**
- ✅ Clean separation
- ✅ Beads doesn't know about VC
- ✅ VC controls export format

**Cons:**
- ⚠️ Must sync 3 files (but that's okay)

#### Option 2: Extend Beads Export (Future)

```go
// Hypothetical: Beads allows extensions to register export handlers
type Exporter interface {
    ExportExtensionData(w io.Writer) error
}

beads.RegisterExporter("vc", &VCExporter{})

// Then bd export includes extension data
```

**For now**: Option 1 is fine. VC manages its own JSONL exports.

---

## Migration Strategy

### VC First Run

```go
func InitializeVC(ctx context.Context, dbPath string) error {
    // 1. Beads creates core tables
    beadsStore, err := beads.NewSQLiteStorage(dbPath)
    if err != nil {
        return err
    }

    // 2. VC creates extension tables
    vcStore, err := NewVCStorage(ctx, dbPath)
    if err != nil {
        return err
    }

    // Done! Both schemas exist in same DB
    return nil
}
```

### VC Schema Migrations

**vc-37**: VC manages schema migrations inline in code:

- **Extension table creation**: `vcExtensionSchema` constant in `internal/storage/beads/wrapper.go`
- **Column migrations**: `migrateAgentEventsTable()` function checks for missing columns and adds them
- **No separate migration files**: Schema is part of the code, applied at startup

This approach is simpler and more maintainable for the extension model. The old `internal/storage/migrations/` directory has been removed.
```

Or VC creates its own tracking table:

```sql
CREATE TABLE vc_schema_version (
    version INTEGER PRIMARY KEY,
    applied_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
```

---

## Summary: The Extension Model

### What Beads Provides (Platform)

- ✅ Core schema: issues, dependencies, labels, comments, events
- ✅ Storage interface: CRUD, dependencies, labels, ready work
- ✅ Database access: SQLite with foreign keys
- ✅ Export: JSONL for core data

### What VC Adds (Extension)

- ✅ Extension tables: `vc_mission_state`, `vc_agent_events`
- ✅ Extension operations: CreateMission, LogAgentEvent, GetMissionStatus
- ✅ Extension schema: Migrations managed by VC
- ✅ Extension export: VC-specific JSONL files

### How They Interact

```
┌─────────────────────────────────────┐
│  VC (Extension)                     │
│  ┌───────────────────────────────┐  │
│  │ VCStorage (wrapper)           │  │
│  │ - CreateMission()             │  │
│  │ - GetMissionStatus()          │  │
│  │ - LogAgentEvent()             │  │
│  └───────────────────────────────┘  │
│           │                          │
│           │ Embeds                   │
│           ↓                          │
│  ┌───────────────────────────────┐  │
│  │ beads.Storage (platform)      │  │
│  │ - CreateIssue()               │  │
│  │ - AddLabel()                  │  │
│  │ - GetReadyWork()              │  │
│  └───────────────────────────────┘  │
│           │                          │
│           │ + Direct DB access       │
│           ↓                          │
│  ┌───────────────────────────────┐  │
│  │ .beads/vc.db (shared)         │  │
│  │ - issues (Beads)              │  │
│  │ - labels (Beads)              │  │
│  │ - vc_mission_state (VC)       │  │
│  │ - vc_agent_events (VC)        │  │
│  └───────────────────────────────┘  │
└─────────────────────────────────────┘
```

### Clean Separation

- **Beads knows nothing about VC**
- **VC knows about Beads** (imports as library)
- **Both use same database file**
- **Foreign keys connect them**
- **Labels bridge the gap** (Beads feature, VC uses for state machine)

---

## Beads Changes Needed (Minimal)

1. **Expose underlying DB** (one method):
   ```go
   type Storage interface {
       UnderlyingDB() *sql.DB  // For extensions
   }
   ```

2. **That's it!** Everything else already works.

---

## Advantages of This Approach

1. **Beads stays general-purpose** ✅
   - No VC-specific code
   - Thousands of users unaffected
   - Clean, focused scope

2. **VC has full control** ✅
   - Extension tables for anything needed
   - Independent schema evolution
   - Own export strategy

3. **IntelliJ/AS pattern** ✅
   - Platform provides extensibility
   - Extension uses platform APIs
   - No pollution of core

4. **Shared database benefits** ✅
   - Foreign keys work
   - No synchronization
   - Single source of truth

5. **Future-proof** ✅
   - Other workflow engines can extend Beads
   - Each extension has its own tables
   - No conflicts

---

**Recommendation**: Use extension tables, not column additions. Keep Beads clean!

**Next Step**: Get `UnderlyingDB()` method added to Beads storage interface.