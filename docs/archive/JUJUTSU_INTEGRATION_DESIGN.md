# Jujutsu Integration Design for VC

**Status**: Design Phase
**Created**: 2025-10-23
**Target**: Post-Bootstrap (after vc-9 complete)
**Estimated Timeline**: 8-10 weeks
**Priority**: P1 (High impact on scaling)

---

## Executive Summary

This design outlines the integration of Jujutsu VCS support into VC, enabling horizontal scaling of the executor colony model while maintaining full backward compatibility with git workflows.

### Why Jujutsu for VC?

**Current Limitations (Git-only):**
- Multiple executors create frequent JSONL merge conflicts
- Agent-discovered issues often conflict during sync
- Crash risk for long-running agents (data loss before export)
- Complex daemon coordination for git staging/commit cycle
- Worktree support broken (bd-73 equivalent issue)

**Jujutsu Solutions:**
- **Automatic commits** - Changes to issues.jsonl immediately safe
- **Deferred conflict resolution** - Executors don't block on conflicts
- **Smart JSONL merging** - Auto-resolve non-conflicting issue discoveries
- **Micro-checkpoints** - Long-running agents checkpoint every 2 minutes
- **Complete audit trail** - VCS operation log complements agent events
- **Horizontal scaling** - 10+ executors without coordination overhead

### Key Design Principles

1. **Backward Compatibility** - Git remains default, fully supported
2. **Zero User Impact** - Users keep using git, VC uses jj internally
3. **Git Backend Mode** - Jujutsu with `--git-backend` for coexistence
4. **Smart Conflict Resolution** - JSONL-aware merging for discovered issues
5. **Reuse Beads Work** - Leverage bd-74 VCS abstraction design
6. **Progressive Enhancement** - Core first, advanced features later

### Success Metrics

- ✅ 10+ executors run concurrently without conflicts
- ✅ 95%+ of discovered-issue conflicts auto-resolve
- ✅ Zero data loss from executor crashes
- ✅ Git users experience zero disruption
- ✅ Jujutsu users get 5-10× fewer blocking conflicts

---

## Epic Breakdown

### Epic 1: VCS Abstraction Layer (vc-200)
**Priority**: P0 (Foundation)
**Effort**: 2 weeks
**Dependencies**: None (post-bootstrap)

Create version control abstraction enabling both git and jujutsu backends.

**Child Issues**: 6 tasks
- Design VCS interface
- Implement Git backend
- Implement Jujutsu backend
- VCS auto-detection
- Configuration system
- Unit tests

---

### Epic 2: Executor VCS Integration (vc-201)
**Priority**: P0 (Core functionality)
**Effort**: 2 weeks
**Dependencies**: vc-200

Migrate executor to use VCS abstraction for all git operations.

**Child Issues**: 5 tasks
- Migrate sync operations
- Migrate export/commit cycle
- Migrate import/pull cycle
- Activity feed integration
- Integration tests

---

### Epic 3: Smart JSONL Conflict Resolution (vc-202)
**Priority**: P1 (High value)
**Effort**: 3 weeks
**Dependencies**: vc-200, vc-201

Intelligent conflict resolution for discovered issues and concurrent modifications.

**Child Issues**: 6 tasks
- JSONL conflict parser
- Semantic merge algorithm
- `vc resolve` command
- Executor auto-resolve integration
- Conflict detection and reporting
- Comprehensive testing

---

### Epic 4: Advanced Jujutsu Features (vc-203)
**Priority**: P2 (Enhancement)
**Effort**: 2 weeks
**Dependencies**: vc-201

Leverage jujutsu-specific capabilities for improved reliability and observability.

**Child Issues**: 5 tasks
- Micro-checkpoint system
- VCS operation audit trail
- Quality gate rollback
- Operation undo support
- Performance optimization

---

### Epic 5: Documentation and Migration (vc-204)
**Priority**: P1 (User enablement)
**Effort**: 1 week
**Dependencies**: vc-200, vc-201, vc-202

Comprehensive documentation and migration tooling.

**Child Issues**: 4 tasks
- User documentation
- Migration guide
- Configuration reference
- Tutorial and examples

---

## Detailed Issue Specifications

### Epic: vc-200 - VCS Abstraction Layer

#### vc-205: Design VCS Interface
**Type**: Task
**Priority**: 1
**Effort**: 3 days
**Parent**: vc-200

**Description**:
Design the VCS interface that abstracts version control operations needed by VC executor.

**Design**:
```go
// internal/vcs/vcs.go
package vcs

type VCS interface {
    // Detection
    Name() string
    IsRepo() (bool, error)
    HasUpstream() (bool, error)
    GetRepoRoot() (string, error)

    // State
    HasChanges(ctx context.Context, filePath string) (bool, error)
    HasMergeConflicts(ctx context.Context) (bool, error)

    // Operations
    Add(ctx context.Context, filePath string) error
    Commit(ctx context.Context, filePath string, message string) error
    Pull(ctx context.Context) error
    Push(ctx context.Context) error

    // History
    GetCurrentCommitHash(ctx context.Context) (string, error)
    GetFileFromHead(ctx context.Context, filePath string) ([]byte, error)

    // Config
    EnsureIgnoreFile(beadsDir string) error
}

type Config struct {
    Type       string // "git" or "jj"
    AutoDetect bool
}

func DetectVCS() (VCS, error)
func NewVCS(cfg Config) (VCS, error)
```

**Acceptance Criteria**:
- [ ] VCS interface defined with all necessary operations
- [ ] Config struct supports auto-detection and explicit selection
- [ ] DetectVCS() checks for jj first, then git
- [ ] NewVCS() creates appropriate backend from config
- [ ] Interface documented with godoc comments
- [ ] Design reviewed and approved

**Notes**:
Reuse design from Beads bd-74, adapt for VC's specific needs.

---

#### vc-206: Implement Git Backend
**Type**: Task
**Priority**: 1
**Effort**: 4 days
**Parent**: vc-200
**Depends On**: vc-205

**Description**:
Implement VCS interface for Git backend by refactoring existing git operations.

**Design**:
```go
// internal/vcs/git.go
type GitVCS struct{}

func NewGitVCS() *GitVCS
func (g *GitVCS) Name() string
func (g *GitVCS) IsRepo() (bool, error)
// ... implement all VCS interface methods
```

Migrate code from:
- Current git operations in executor
- Any git-specific code in storage layer

**Acceptance Criteria**:
- [ ] GitVCS implements all VCS interface methods
- [ ] All existing git functionality preserved
- [ ] Unit tests for each method
- [ ] Error handling matches current behavior
- [ ] No breaking changes to executor
- [ ] Worktree detection implemented (optional feature)

**Files**:
- Create: `internal/vcs/git.go`
- Create: `internal/vcs/git_test.go`

---

#### vc-207: Implement Jujutsu Backend
**Type**: Task
**Priority**: 1
**Effort**: 5 days
**Parent**: vc-200
**Depends On**: vc-205

**Description**:
Implement VCS interface for Jujutsu backend with auto-commit awareness.

**Design**:
```go
// internal/vcs/jujutsu.go
type JujutsuVCS struct{}

func NewJujutsuVCS() *JujutsuVCS
func (j *JujutsuVCS) Commit(ctx context.Context, filePath string, message string) error {
    // In jj, changes already auto-committed to working copy
    // We need to:
    // 1. Add description: jj describe -m "message"
    // 2. Start new commit: jj new
    cmd := exec.CommandContext(ctx, "jj", "describe", "-m", message)
    if err := cmd.Run(); err != nil {
        return err
    }

    cmd = exec.CommandContext(ctx, "jj", "new")
    return cmd.Run()
}
```

**Key Adaptations**:
- `Commit()` uses `jj describe + jj new` pattern
- `Pull()` uses `jj git fetch` (no pull in jj)
- `Push()` uses `jj git push --all`
- `HasChanges()` uses `jj diff --summary`
- `HasMergeConflicts()` uses `jj conflicts`

**Acceptance Criteria**:
- [ ] JujutsuVCS implements all VCS interface methods
- [ ] Auto-commit model properly handled
- [ ] Bookmark management working
- [ ] Conflict detection via `jj conflicts`
- [ ] Works with `--git-backend` mode
- [ ] Unit tests for each method
- [ ] Returns nil if jj not installed

**Files**:
- Create: `internal/vcs/jujutsu.go`
- Create: `internal/vcs/jujutsu_test.go`

---

#### vc-208: VCS Auto-Detection
**Type**: Task
**Priority**: 1
**Effort**: 2 days
**Parent**: vc-200
**Depends On**: vc-206, vc-207

**Description**:
Implement VCS auto-detection logic with proper fallback chain.

**Design**:
```go
func DetectVCS() (VCS, error) {
    // Check for jj first (if user has jj, they chose it)
    if jjVCS := NewJujutsuVCS(); jjVCS != nil {
        if isRepo, _ := jjVCS.IsRepo(); isRepo {
            log.Info("Detected jujutsu repository")
            return jjVCS, nil
        }
    }

    // Fall back to git (more common default)
    gitVCS := NewGitVCS()
    if isRepo, _ := gitVCS.IsRepo(); isRepo {
        log.Info("Detected git repository")
        return gitVCS, nil
    }

    return nil, fmt.Errorf("no supported VCS found (checked jj, git)")
}
```

**Acceptance Criteria**:
- [ ] Detects jj repos correctly (checks `.jj/` directory)
- [ ] Detects git repos correctly (checks `.git/` directory)
- [ ] Prefers jj over git if both present
- [ ] Returns clear error if neither present
- [ ] Logs which VCS was detected
- [ ] Handles edge cases (nested repos, worktrees, etc.)
- [ ] Integration tests with real repos

**Files**:
- Modify: `internal/vcs/vcs.go`
- Create: `internal/vcs/detection_test.go`

---

#### vc-209: VCS Configuration System
**Type**: Task
**Priority**: 2
**Effort**: 3 days
**Parent**: vc-200
**Depends On**: vc-208

**Description**:
Add configuration options for VCS selection and behavior.

**Design**:
```yaml
# .vc/config.yaml
vcs:
  type: auto          # auto, git, jj
  prefer_jujutsu: true # If both installed, prefer jj
  auto_commit: true    # Auto-commit on export
  auto_push: true      # Auto-push on sync
```

Environment variable overrides:
```bash
export VC_VCS=git      # Force git
export VC_VCS=jj       # Force jujutsu
export VC_VCS=auto     # Auto-detect (default)
```

**Acceptance Criteria**:
- [ ] Config file supports VCS settings
- [ ] Environment variables override config
- [ ] `VC_VCS` variable works correctly
- [ ] Config validation on startup
- [ ] `vc config show` displays VCS settings
- [ ] Migration from old config format (if needed)
- [ ] Documentation for all settings

**Files**:
- Modify: `internal/config/config.go`
- Modify: `internal/config/vcs.go` (new)
- Update: `docs/CONFIGURATION.md`

---

#### vc-210: VCS Unit Tests
**Type**: Task
**Priority**: 1
**Effort**: 3 days
**Parent**: vc-200
**Depends On**: vc-206, vc-207, vc-208

**Description**:
Comprehensive unit tests for VCS abstraction layer.

**Test Coverage**:
- ✅ GitVCS all methods (mocked git commands)
- ✅ JujutsuVCS all methods (mocked jj commands)
- ✅ VCS detection logic
- ✅ Config parsing and validation
- ✅ Error handling
- ✅ Edge cases (no VCS, both VCS, etc.)

**Acceptance Criteria**:
- [ ] >90% code coverage for vcs package
- [ ] All VCS methods tested
- [ ] Mock command execution for isolation
- [ ] Test with real repos in CI (integration tests)
- [ ] Error cases covered
- [ ] Documentation examples tested
- [ ] CI passes on all platforms

**Files**:
- `internal/vcs/git_test.go`
- `internal/vcs/jujutsu_test.go`
- `internal/vcs/vcs_test.go`
- `internal/vcs/detection_test.go`

---

### Epic: vc-201 - Executor VCS Integration

#### vc-211: Migrate Executor Sync Operations
**Type**: Task
**Priority**: 1
**Effort**: 3 days
**Parent**: vc-201
**Depends On**: vc-200 (all)

**Description**:
Refactor executor sync operations to use VCS abstraction instead of direct git commands.

**Current Code** (to be refactored):
```go
// internal/executor/sync.go (hypothetical current state)
func (e *Executor) syncIssues(ctx context.Context) error {
    // Direct git commands
    cmd := exec.Command("git", "add", ".beads/issues.jsonl")
    cmd.Run()

    cmd = exec.Command("git", "commit", "-m", "sync")
    cmd.Run()

    // ...
}
```

**New Code**:
```go
func (e *Executor) syncIssues(ctx context.Context) error {
    vcs := e.vcs // Injected VCS instance

    if err := vcs.Add(ctx, ".beads/issues.jsonl"); err != nil {
        return err
    }

    msg := fmt.Sprintf("vc auto-sync %s", time.Now().Format(time.RFC3339))
    if err := vcs.Commit(ctx, ".beads/issues.jsonl", msg); err != nil {
        return err
    }

    // ...
}
```

**Acceptance Criteria**:
- [ ] All git commands replaced with VCS calls
- [ ] Executor struct has `vcs VCS` field
- [ ] VCS injected via constructor
- [ ] Sync workflow unchanged for git users
- [ ] Works with both git and jj backends
- [ ] Error handling preserved
- [ ] Integration tests pass

**Files**:
- Modify: `internal/executor/executor.go`
- Modify: `internal/executor/sync.go` (if exists)

---

#### vc-212: Migrate Export/Commit Cycle
**Type**: Task
**Priority**: 1
**Effort**: 2 days
**Parent**: vc-201
**Depends On**: vc-211

**Description**:
Update the export → commit cycle to work with both git and jujutsu models.

**Key Difference**:
- **Git**: Export → stage (git add) → commit (git commit)
- **Jujutsu**: Export → describe (jj describe) → new (jj new)
  - Note: jj auto-commits export automatically!

**Design**:
```go
func (e *Executor) exportAndCommit(ctx context.Context) error {
    // 1. Export database to JSONL
    if err := e.storage.ExportJSONL(ctx, ".beads/issues.jsonl"); err != nil {
        return fmt.Errorf("export failed: %w", err)
    }

    // 2. Commit (VCS-agnostic call, but behavior differs)
    //    - Git: stages and commits
    //    - Jj: describes working copy commit and starts new one
    msg := fmt.Sprintf("vc executor: %s", e.instanceID)
    if err := e.vcs.Commit(ctx, ".beads/issues.jsonl", msg); err != nil {
        return fmt.Errorf("commit failed: %w", err)
    }

    return nil
}
```

**Acceptance Criteria**:
- [ ] Export writes to JSONL file
- [ ] VCS.Commit() called after export
- [ ] Works correctly with git backend
- [ ] Works correctly with jj backend
- [ ] Commit messages include executor instance ID
- [ ] Error handling for export and commit failures
- [ ] Activity feed events recorded

**Files**:
- Modify: `internal/executor/executor.go`
- Add: `internal/executor/export.go` (if extracted)

---

#### vc-213: Migrate Import/Pull Cycle
**Type**: Task
**Priority**: 1
**Effort**: 3 days
**Parent**: vc-201
**Depends On**: vc-212

**Description**:
Update the pull → import cycle with conflict awareness.

**Design**:
```go
func (e *Executor) pullAndImport(ctx context.Context) error {
    // 1. Pull changes from remote
    //    - Git: git pull
    //    - Jj: jj git fetch
    if err := e.vcs.Pull(ctx); err != nil {
        return fmt.Errorf("pull failed: %w", err)
    }

    // 2. Check for conflicts
    hasConflicts, err := e.vcs.HasMergeConflicts(ctx)
    if err != nil {
        return fmt.Errorf("conflict check failed: %w", err)
    }

    if hasConflicts {
        // Log warning, but continue (jj allows deferred resolution)
        e.logWarn("JSONL conflicts detected, attempting import anyway")

        // Try auto-resolve (vc-202)
        if err := e.autoResolveConflicts(ctx); err != nil {
            e.logError("Auto-resolve failed: %v", err)
            // With jj, we can continue working despite conflict
            if _, isJJ := e.vcs.(*vcs.JujutsuVCS); !isJJ {
                // Git requires resolution before continuing
                return fmt.Errorf("merge conflicts in issues.jsonl")
            }
        }
    }

    // 3. Import JSONL into database
    if err := e.storage.ImportJSONL(ctx, ".beads/issues.jsonl"); err != nil {
        return fmt.Errorf("import failed: %w", err)
    }

    return nil
}
```

**Acceptance Criteria**:
- [ ] Pull operation uses VCS abstraction
- [ ] Conflict detection works for both git and jj
- [ ] Import proceeds even with jj conflicts (deferred)
- [ ] Import blocks on git conflicts (current behavior)
- [ ] Activity feed records pull/import events
- [ ] Error handling for pull and import failures
- [ ] Integration tests with conflicts

**Files**:
- Modify: `internal/executor/executor.go`
- Add: `internal/executor/import.go` (if extracted)

---

#### vc-214: Activity Feed VCS Integration
**Type**: Task
**Priority**: 2
**Effort**: 3 days
**Parent**: vc-201
**Depends On**: vc-211, vc-212, vc-213

**Description**:
Integrate VCS operations into activity feed for observability.

**New Event Types**:
```go
// internal/activity/events.go

type EventType string

const (
    // Existing events
    EventAgentSpawned = "agent_spawned"
    EventFileModified = "file_modified"
    // ...

    // New VCS events
    EventVCSCommit    = "vcs_commit"
    EventVCSPull      = "vcs_pull"
    EventVCSPush      = "vcs_push"
    EventVCSConflict  = "vcs_conflict"
)

type VCSEventData struct {
    VCSType     string `json:"vcs_type"`     // "git" or "jujutsu"
    Operation   string `json:"operation"`    // "commit", "pull", "push"
    FilePath    string `json:"file_path"`    // .beads/issues.jsonl
    CommitHash  string `json:"commit_hash"`  // Result hash
    Message     string `json:"message"`      // Commit message
    Success     bool   `json:"success"`
    Error       string `json:"error,omitempty"`
}
```

**Recording Events**:
```go
func (e *Executor) exportAndCommit(ctx context.Context) error {
    // Export
    if err := e.storage.ExportJSONL(ctx, ".beads/issues.jsonl"); err != nil {
        return err
    }

    // Commit
    msg := fmt.Sprintf("vc executor: %s", e.instanceID)
    err := e.vcs.Commit(ctx, ".beads/issues.jsonl", msg)

    // Record VCS event
    e.activity.RecordEvent(ctx, Event{
        Type:    EventVCSCommit,
        IssueID: e.currentIssue,
        Data: VCSEventData{
            VCSType:   e.vcs.Name(),
            Operation: "commit",
            FilePath:  ".beads/issues.jsonl",
            Message:   msg,
            Success:   err == nil,
            Error:     errString(err),
        },
    })

    return err
}
```

**Acceptance Criteria**:
- [ ] VCS events defined in activity package
- [ ] Commit operations recorded
- [ ] Pull operations recorded
- [ ] Push operations recorded
- [ ] Conflict detections recorded
- [ ] Events include VCS type (git/jj)
- [ ] `vc tail --issue vc-X` shows VCS events
- [ ] Event schema documented

**Files**:
- Modify: `internal/activity/events.go`
- Modify: `internal/activity/feed.go`
- Modify: `internal/executor/executor.go`

---

#### vc-215: Executor Integration Tests
**Type**: Task
**Priority**: 1
**Effort**: 4 days
**Parent**: vc-201
**Depends On**: vc-211, vc-212, vc-213, vc-214

**Description**:
End-to-end integration tests for executor with both VCS backends.

**Test Scenarios**:

1. **Basic Sync (Git)**:
   - Initialize git repo
   - Run executor, complete issue
   - Verify JSONL committed to git
   - Verify git history correct

2. **Basic Sync (Jujutsu)**:
   - Initialize jj repo with git backend
   - Run executor, complete issue
   - Verify JSONL committed to jj
   - Verify jj history correct
   - Verify git compatibility

3. **Conflict Handling (Git)**:
   - Two executors, simultaneous changes
   - Verify second executor detects conflict
   - Verify executor blocks on conflict

4. **Conflict Handling (Jujutsu)**:
   - Two executors, simultaneous changes
   - Verify conflicts stored in jj
   - Verify executors don't block (deferred)

5. **Crash Recovery (Jujutsu)**:
   - Executor exports, jj auto-commits
   - Simulate crash before push
   - Restart executor
   - Verify no data loss

**Acceptance Criteria**:
- [ ] Integration tests for git backend pass
- [ ] Integration tests for jj backend pass
- [ ] Conflict scenarios tested for both
- [ ] Crash recovery tested (jj only)
- [ ] Multi-executor scenarios tested
- [ ] CI runs tests with both backends
- [ ] Tests documented with clear scenarios

**Files**:
- Create: `internal/executor/integration_git_test.go`
- Create: `internal/executor/integration_jj_test.go`
- Create: `internal/executor/testutil/vcs_helpers.go`

---

### Epic: vc-202 - Smart JSONL Conflict Resolution

#### vc-216: JSONL Conflict Parser
**Type**: Task
**Priority**: 1
**Effort**: 3 days
**Parent**: vc-202
**Depends On**: vc-200 (all)

**Description**:
Parse JSONL conflicts from both git and jujutsu conflict formats.

**Git Conflict Format**:
```jsonl
{"id":"vc-42","status":"open","priority":2}
<<<<<<< HEAD
{"id":"vc-43","status":"in_progress","priority":2}
=======
{"id":"vc-43","status":"closed","priority":2}
>>>>>>> remote
{"id":"vc-44","status":"open","priority":1}
```

**Jujutsu Conflict Format** (logical representation):
Jj stores conflicts as special markers, need to use `jj cat` to extract sides.

**Design**:
```go
// internal/vcs/conflict_parser.go

type ConflictSide struct {
    Base   []byte
    Ours   []byte
    Theirs []byte
}

type ConflictParser interface {
    ParseConflict(ctx context.Context, filePath string) (*ConflictSide, error)
}

type GitConflictParser struct{}
func (p *GitConflictParser) ParseConflict(ctx context.Context, filePath string) (*ConflictSide, error) {
    // Read file, extract <<<<<<< / ======= / >>>>>>> markers
    // Return base, ours, theirs sections
}

type JujutsuConflictParser struct {
    vcs *JujutsuVCS
}
func (p *JujutsuConflictParser) ParseConflict(ctx context.Context, filePath string) (*ConflictSide, error) {
    // Use `jj cat` to extract each side of conflict
    // jj cat -r base filePath
    // jj cat -r ours filePath
    // jj cat -r theirs filePath
}
```

**Acceptance Criteria**:
- [ ] GitConflictParser extracts all three sides
- [ ] JujutsuConflictParser uses jj commands
- [ ] Handles multiple conflicts in same file
- [ ] Handles malformed conflict markers
- [ ] Returns structured ConflictSide
- [ ] Unit tests with real conflict examples
- [ ] Error handling for corrupt conflicts

**Files**:
- Create: `internal/vcs/conflict_parser.go`
- Create: `internal/vcs/conflict_parser_test.go`

---

#### vc-217: Semantic JSONL Merge Algorithm
**Type**: Task
**Priority**: 1
**Effort**: 5 days
**Parent**: vc-202
**Depends On**: vc-216

**Description**:
Implement intelligent merging for JSONL issues using VC's domain knowledge.

**Algorithm**:
```go
// internal/vcs/jsonl_merger.go

type JSONLMerger struct {
    base   map[string]*types.Issue
    ours   map[string]*types.Issue
    theirs map[string]*types.Issue
}

type MergeResult struct {
    Merged      map[string]*types.Issue
    Conflicts   []Conflict
    AutoResolved int
}

type Conflict struct {
    IssueID string
    Field   string
    OurValue    interface{}
    TheirValue  interface{}
    Reasoning   string
}

func (m *JSONLMerger) AutoMerge() (*MergeResult, error) {
    result := &MergeResult{
        Merged: make(map[string]*types.Issue),
    }

    allIDs := m.getAllIssueIDs()

    for _, id := range allIDs {
        base := m.base[id]
        ours := m.ours[id]
        theirs := m.theirs[id]

        switch {
        case base == nil && ours != nil && theirs == nil:
            // New issue on our side only - keep it
            result.Merged[id] = ours
            result.AutoResolved++

        case base == nil && ours == nil && theirs != nil:
            // New issue on their side only - keep it
            result.Merged[id] = theirs
            result.AutoResolved++

        case base == nil && ours != nil && theirs != nil:
            // Both added same issue ID - this is a real conflict
            result.Conflicts = append(result.Conflicts, Conflict{
                IssueID: id,
                Field: "entire_issue",
                Reasoning: "Both sides created issue with same ID",
            })

        case base != nil && ours != nil && theirs != nil:
            // Both modified existing issue - semantic merge
            merged, conflicts := m.semanticMergeIssue(base, ours, theirs)
            if len(conflicts) > 0 {
                result.Conflicts = append(result.Conflicts, conflicts...)
            } else {
                result.Merged[id] = merged
                result.AutoResolved++
            }

        // ... other cases
        }
    }

    return result, nil
}

func (m *JSONLMerger) semanticMergeIssue(base, ours, theirs *types.Issue) (*types.Issue, []Conflict) {
    merged := &types.Issue{ID: base.ID}
    conflicts := []Conflict{}

    // Status: conflict if both changed differently
    if ours.Status != base.Status && theirs.Status != base.Status {
        if ours.Status != theirs.Status {
            conflicts = append(conflicts, Conflict{
                IssueID: base.ID,
                Field: "status",
                OurValue: ours.Status,
                TheirValue: theirs.Status,
                Reasoning: "Both sides changed status differently",
            })
        }
    }
    // Take non-base value
    if ours.Status != base.Status {
        merged.Status = ours.Status
    } else {
        merged.Status = theirs.Status
    }

    // Dependencies: union (additive)
    merged.Dependencies = m.unionDependencies(ours.Dependencies, theirs.Dependencies)

    // Labels: union (additive)
    merged.Labels = m.unionLabels(ours.Labels, theirs.Labels)

    // Notes: concatenate if both added (with separator)
    merged.Notes = m.mergeNotes(base.Notes, ours.Notes, theirs.Notes)

    // Priority: conflict if both changed differently
    if ours.Priority != base.Priority && theirs.Priority != base.Priority {
        if ours.Priority != theirs.Priority {
            conflicts = append(conflicts, Conflict{
                IssueID: base.ID,
                Field: "priority",
                OurValue: ours.Priority,
                TheirValue: theirs.Priority,
                Reasoning: "Both sides changed priority differently",
            })
        }
    }

    return merged, conflicts
}
```

**Merge Rules**:
1. **New issues**: Always include (no conflict)
2. **Status changes**: Conflict if both changed differently
3. **Dependencies**: Union of both sets (additive)
4. **Labels**: Union of both sets (additive)
5. **Notes**: Concatenate with separator
6. **Priority**: Conflict if both changed differently
7. **Deleted issues**: Conflict if one deleted, one modified

**Acceptance Criteria**:
- [ ] Parses JSONL from all three sides
- [ ] Auto-resolves new issue additions (both sides)
- [ ] Detects semantic conflicts (same field, different values)
- [ ] Merges dependencies as union
- [ ] Merges labels as union
- [ ] Handles deleted issues correctly
- [ ] Returns list of remaining conflicts
- [ ] Unit tests with comprehensive scenarios
- [ ] >95% auto-resolve rate in simulations

**Files**:
- Create: `internal/vcs/jsonl_merger.go`
- Create: `internal/vcs/jsonl_merger_test.go`

---

#### vc-218: `vc resolve` Command
**Type**: Task
**Priority**: 1
**Effort**: 4 days
**Parent**: vc-202
**Depends On**: vc-217

**Description**:
CLI command for resolving JSONL conflicts interactively and automatically.

**Usage**:
```bash
# Auto-resolve non-conflicting changes
vc resolve --auto

# Dry-run (preview what would be resolved)
vc resolve --auto --dry-run

# Interactive resolution for remaining conflicts
vc resolve --interactive

# Force resolution (take ours/theirs)
vc resolve --take-ours
vc resolve --take-theirs
```

**Implementation**:
```go
// cmd/vc/resolve.go

func resolveCommand() *cobra.Command {
    cmd := &cobra.Command{
        Use:   "resolve",
        Short: "Resolve JSONL merge conflicts",
        Long: `Intelligently resolve conflicts in .beads/issues.jsonl.

Uses VC's domain knowledge to auto-resolve non-conflicting changes:
  - New issues added by both sides: merge both
  - Dependencies/labels: union of both sets
  - Same field changed differently: manual resolution required

Examples:
  vc resolve --auto              # Auto-resolve, prompt for conflicts
  vc resolve --auto --dry-run    # Preview without changing files
  vc resolve --take-ours         # Resolve all conflicts with our version
        `,
        RunE: runResolve,
    }

    cmd.Flags().Bool("auto", false, "Auto-resolve non-conflicting changes")
    cmd.Flags().Bool("dry-run", false, "Preview resolution without applying")
    cmd.Flags().Bool("interactive", true, "Interactively resolve conflicts")
    cmd.Flags().Bool("take-ours", false, "Resolve all conflicts with our version")
    cmd.Flags().Bool("take-theirs", false, "Resolve all conflicts with their version")

    return cmd
}

func runResolve(cmd *cobra.Command, args []string) error {
    ctx := cmd.Context()

    // Detect VCS
    vcs, err := vcs.DetectVCS()
    if err != nil {
        return fmt.Errorf("no VCS found: %w", err)
    }

    // Check for conflicts
    hasConflicts, err := vcs.HasMergeConflicts(ctx)
    if err != nil {
        return err
    }
    if !hasConflicts {
        fmt.Println("✓ No conflicts to resolve")
        return nil
    }

    // Parse conflict
    parser := getConflictParser(vcs)
    sides, err := parser.ParseConflict(ctx, ".beads/issues.jsonl")
    if err != nil {
        return fmt.Errorf("failed to parse conflict: %w", err)
    }

    // Attempt auto-merge
    merger := vcs.NewJSONLMerger(sides)
    result, err := merger.AutoMerge()
    if err != nil {
        return fmt.Errorf("auto-merge failed: %w", err)
    }

    // Display results
    fmt.Printf("Auto-resolved: %d issues\n", result.AutoResolved)
    fmt.Printf("Conflicts remaining: %d\n", len(result.Conflicts))

    if cmd.Flags().GetBool("dry-run") {
        printMergePreview(result)
        return nil
    }

    // Handle remaining conflicts
    if len(result.Conflicts) > 0 {
        if cmd.Flags().GetBool("take-ours") {
            resolveConflictsOurs(result)
        } else if cmd.Flags().GetBool("take-theirs") {
            resolveConflictsTheirs(result)
        } else if cmd.Flags().GetBool("interactive") {
            if err := resolveConflictsInteractive(result); err != nil {
                return err
            }
        } else {
            return fmt.Errorf("conflicts remain, use --interactive or --take-ours/--take-theirs")
        }
    }

    // Write resolved JSONL
    if err := writeResolvedJSONL(result.Merged); err != nil {
        return fmt.Errorf("failed to write resolved JSONL: %w", err)
    }

    // Mark conflict as resolved in VCS
    if err := markConflictResolved(ctx, vcs); err != nil {
        return fmt.Errorf("failed to mark resolved: %w", err)
    }

    fmt.Println("✓ Conflicts resolved successfully")
    return nil
}
```

**Interactive Mode**:
```
Conflict in issue vc-42 on field 'status':
  Our version:    in_progress
  Their version:  closed

[O] Take ours (in_progress)
[T] Take theirs (closed)
[E] Edit manually
[S] Skip (resolve later)

Choice:
```

**Acceptance Criteria**:
- [ ] `vc resolve --auto` works for simple conflicts
- [ ] `--dry-run` shows preview without changes
- [ ] `--interactive` prompts for each conflict
- [ ] `--take-ours` and `--take-theirs` work
- [ ] Writes resolved JSONL file
- [ ] Marks conflict as resolved in VCS
- [ ] Works with both git and jj
- [ ] Clear error messages
- [ ] Help text comprehensive
- [ ] Integration tests

**Files**:
- Create: `cmd/vc/resolve.go`
- Create: `cmd/vc/resolve_interactive.go`
- Create: `cmd/vc/resolve_test.go`

---

#### vc-219: Executor Auto-Resolve Integration
**Type**: Task
**Priority**: 1
**Effort**: 3 days
**Parent**: vc-202
**Depends On**: vc-218

**Description**:
Integrate auto-resolve into executor sync loop to handle conflicts automatically.

**Design**:
```go
// internal/executor/auto_resolve.go

func (e *Executor) autoResolveConflicts(ctx context.Context) error {
    // Check if conflicts exist
    hasConflicts, err := e.vcs.HasMergeConflicts(ctx)
    if err != nil || !hasConflicts {
        return err
    }

    e.logInfo("Detected JSONL conflict, attempting auto-resolve")

    // Parse conflict
    parser := e.getConflictParser()
    sides, err := parser.ParseConflict(ctx, ".beads/issues.jsonl")
    if err != nil {
        return fmt.Errorf("parse conflict failed: %w", err)
    }

    // Auto-merge
    merger := vcs.NewJSONLMerger(sides)
    result, err := merger.AutoMerge()
    if err != nil {
        return fmt.Errorf("auto-merge failed: %w", err)
    }

    // Check if fully resolved
    if len(result.Conflicts) > 0 {
        e.logWarn("Auto-resolve incomplete: %d conflicts remain", len(result.Conflicts))

        // Record activity event
        e.activity.RecordEvent(ctx, Event{
            Type: EventVCSConflict,
            Data: VCSConflictData{
                AutoResolved: result.AutoResolved,
                Remaining:    len(result.Conflicts),
                RequiresManual: true,
            },
        })

        // Jujutsu: can continue with conflict
        // Git: must stop
        if _, isJJ := e.vcs.(*vcs.JujutsuVCS); !isJJ {
            return fmt.Errorf("conflicts require manual resolution (run: vc resolve)")
        }

        e.logInfo("Deferring remaining conflicts (jujutsu allows continuation)")
        return nil
    }

    // Fully auto-resolved!
    e.logInfo("Auto-resolved all conflicts (%d issues)", result.AutoResolved)

    // Write resolved JSONL
    if err := e.writeResolvedJSONL(result.Merged); err != nil {
        return fmt.Errorf("write resolved JSONL failed: %w", err)
    }

    // Mark resolved
    if err := e.markConflictResolved(ctx); err != nil {
        return fmt.Errorf("mark resolved failed: %w", err)
    }

    // Record success
    e.activity.RecordEvent(ctx, Event{
        Type: EventVCSConflict,
        Data: VCSConflictData{
            AutoResolved: result.AutoResolved,
            Remaining:    0,
            RequiresManual: false,
        },
    })

    return nil
}
```

**Integration into Sync Loop**:
```go
func (e *Executor) syncIssues(ctx context.Context) error {
    // 1. Export
    if err := e.exportJSONL(ctx); err != nil {
        return err
    }

    // 2. Commit
    if err := e.commitJSONL(ctx); err != nil {
        return err
    }

    // 3. Pull
    if err := e.vcs.Pull(ctx); err != nil {
        return err
    }

    // 4. Auto-resolve conflicts (NEW)
    if err := e.autoResolveConflicts(ctx); err != nil {
        e.logError("Auto-resolve failed: %v", err)
        // Continue anyway if jj (deferred resolution)
        if _, isJJ := e.vcs.(*vcs.JujutsuVCS); !isJJ {
            return err
        }
    }

    // 5. Import
    if err := e.importJSONL(ctx); err != nil {
        return err
    }

    // 6. Push
    return e.vcs.Push(ctx)
}
```

**Acceptance Criteria**:
- [ ] Auto-resolve integrated into sync loop
- [ ] Conflicts attempted on every pull
- [ ] Git executors stop on unresolved conflicts
- [ ] Jujutsu executors continue despite conflicts
- [ ] Activity feed records auto-resolve attempts
- [ ] Logs show auto-resolve progress
- [ ] Metrics track auto-resolve success rate
- [ ] Integration tests verify behavior

**Files**:
- Create: `internal/executor/auto_resolve.go`
- Modify: `internal/executor/sync.go`
- Create: `internal/executor/auto_resolve_test.go`

---

#### vc-220: Conflict Detection and Reporting
**Type**: Task
**Priority**: 2
**Effort**: 3 days
**Parent**: vc-202
**Depends On**: vc-219

**Description**:
Enhanced conflict detection, reporting, and monitoring.

**Features**:

1. **Conflict Detection Hook**:
```go
// Run after every pull, log conflicts
func (e *Executor) detectConflicts(ctx context.Context) *ConflictReport {
    hasConflicts, _ := e.vcs.HasMergeConflicts(ctx)
    if !hasConflicts {
        return nil
    }

    // Parse and analyze
    parser := e.getConflictParser()
    sides, _ := parser.ParseConflict(ctx, ".beads/issues.jsonl")
    merger := vcs.NewJSONLMerger(sides)
    result, _ := merger.AutoMerge()

    return &ConflictReport{
        TotalIssues: len(merger.AllIssueIDs()),
        AutoResolvable: result.AutoResolved,
        Conflicts: len(result.Conflicts),
        Details: result.Conflicts,
    }
}
```

2. **CLI Conflict Status**:
```bash
vc status --conflicts

Output:
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
JSONL Conflict Status
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
Auto-resolvable: 8 issues
Conflicts:       2 issues

Conflicting Issues:
  vc-42: status (ours: in_progress, theirs: closed)
  vc-43: priority (ours: 1, theirs: 2)

Run: vc resolve --auto
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
```

3. **Metrics Collection**:
```go
// Track conflict resolution metrics
type ConflictMetrics struct {
    TotalConflicts     int
    AutoResolved       int
    ManualResolved     int
    AutoResolveRate    float64
    AverageResolveTime time.Duration
}
```

**Acceptance Criteria**:
- [ ] Conflict detection runs after every pull
- [ ] `vc status --conflicts` shows conflict summary
- [ ] Metrics track auto-resolve rate
- [ ] Activity feed shows conflict events
- [ ] Prometheus metrics exported (if enabled)
- [ ] Documentation for conflict workflow
- [ ] Alert if auto-resolve rate drops below 80%

**Files**:
- Create: `internal/executor/conflict_detection.go`
- Modify: `cmd/vc/status.go`
- Create: `internal/metrics/conflict_metrics.go`

---

#### vc-221: Conflict Resolution Testing
**Type**: Task
**Priority**: 1
**Effort**: 4 days
**Parent**: vc-202
**Depends On**: vc-216, vc-217, vc-218, vc-219, vc-220

**Description**:
Comprehensive testing for conflict resolution with real-world scenarios.

**Test Scenarios**:

1. **Simple Addition Conflicts**:
   - Executor A adds vc-100, vc-101
   - Executor B adds vc-102, vc-103
   - Expected: All 4 issues merged (100% auto-resolve)

2. **Same Issue Modified**:
   - Both change vc-42 status differently
   - Expected: Conflict detected, manual resolution required

3. **Dependency Additions**:
   - Executor A adds dependency: vc-42 → vc-43
   - Executor B adds dependency: vc-42 → vc-44
   - Expected: Union of dependencies (auto-resolve)

4. **Label Additions**:
   - Executor A adds label "bug" to vc-42
   - Executor B adds label "urgent" to vc-42
   - Expected: Both labels merged (auto-resolve)

5. **Priority Conflicts**:
   - Executor A changes vc-42 priority to 1
   - Executor B changes vc-42 priority to 3
   - Expected: Conflict detected

6. **Delete vs. Modify**:
   - Executor A deletes vc-42
   - Executor B modifies vc-42 status
   - Expected: Conflict detected

7. **Cascading Discovered Issues**:
   - Executor A discovers 10 issues
   - Executor B discovers 15 issues
   - Expected: All 25 issues merged (100% auto-resolve)

8. **Mixed Scenario**:
   - 5 new issues (both sides)
   - 2 status changes (conflicting)
   - 3 dependency additions
   - Expected: 8 auto-resolved, 2 conflicts

**Performance Tests**:
- Large JSONL (1000+ issues) with conflicts
- Auto-resolve time < 1 second
- Memory usage reasonable

**Acceptance Criteria**:
- [ ] All 8 scenarios tested with unit tests
- [ ] Integration tests with real repos (git and jj)
- [ ] Performance benchmarks pass
- [ ] Edge cases covered (malformed JSONL, etc.)
- [ ] Fuzzing tests for parser robustness
- [ ] Documentation of test scenarios
- [ ] CI runs full conflict test suite

**Files**:
- Create: `internal/vcs/conflict_scenarios_test.go`
- Create: `internal/vcs/conflict_performance_test.go`
- Create: `cmd/vc/resolve_integration_test.go`

---

### Epic: vc-203 - Advanced Jujutsu Features

#### vc-222: Micro-Checkpoint System
**Type**: Task
**Priority**: 2
**Effort**: 4 days
**Parent**: vc-203
**Depends On**: vc-201 (all)

**Description**:
Implement periodic checkpointing for long-running agent executions (jujutsu only).

**Design**:
```go
// internal/executor/checkpoint.go

type CheckpointConfig struct {
    Enabled  bool
    Interval time.Duration // Default: 2 minutes
}

type Checkpointer struct {
    vcs      vcs.VCS
    storage  storage.Storage
    interval time.Duration
    logger   *log.Logger
}

func (e *Executor) executeWithCheckpoints(ctx context.Context, issueID string) error {
    // Only enable for jujutsu (git checkpoints are expensive)
    if _, isJJ := e.vcs.(*vcs.JujutsuVCS); !isJJ {
        return e.executeNormally(ctx, issueID)
    }

    // Start checkpoint goroutine
    checkpointCtx, cancel := context.WithCancel(ctx)
    defer cancel()

    checkpointer := &Checkpointer{
        vcs:      e.vcs,
        storage:  e.storage,
        interval: 2 * time.Minute,
        logger:   e.logger,
    }

    go checkpointer.periodicCheckpoint(checkpointCtx, issueID)

    // Execute agent (long-running)
    return e.executeNormally(ctx, issueID)
}

func (c *Checkpointer) periodicCheckpoint(ctx context.Context, issueID string) {
    ticker := time.NewTicker(c.interval)
    defer ticker.Stop()

    checkpointNum := 0

    for {
        select {
        case <-ticker.C:
            checkpointNum++
            if err := c.checkpoint(ctx, issueID, checkpointNum); err != nil {
                c.logger.Error("Checkpoint %d failed: %v", checkpointNum, err)
            } else {
                c.logger.Info("Checkpoint %d saved", checkpointNum)
            }

        case <-ctx.Done():
            c.logger.Info("Checkpointing stopped")
            return
        }
    }
}

func (c *Checkpointer) checkpoint(ctx context.Context, issueID string, num int) error {
    // Export current database state
    if err := c.storage.ExportJSONL(ctx, ".beads/issues.jsonl"); err != nil {
        return fmt.Errorf("export failed: %w", err)
    }

    // Commit with jj (cheap operation)
    msg := fmt.Sprintf("checkpoint %d for %s (%s)", num, issueID, time.Now().Format("15:04:05"))
    if err := c.vcs.Commit(ctx, ".beads/issues.jsonl", msg); err != nil {
        return fmt.Errorf("commit failed: %w", err)
    }

    return nil
}
```

**Recovery After Crash**:
```go
func (e *Executor) recoverFromCrash(ctx context.Context) error {
    // Check for incomplete execution (in_progress issues with no recent activity)
    incomplete, err := e.storage.GetInProgressIssues(ctx)
    if err != nil {
        return err
    }

    for _, issue := range incomplete {
        e.logger.Warn("Found incomplete issue: %s (last checkpoint: %v)",
            issue.ID, issue.LastModified)

        // Import latest checkpoint from VCS
        if err := e.importFromCheckpoint(ctx); err != nil {
            return err
        }

        // Release the claim (allow retry)
        if err := e.storage.UpdateIssue(ctx, issue.ID, storage.UpdateOptions{
            Status: types.StatusOpen,
            Notes:  "Recovered from crashed execution",
        }); err != nil {
            return err
        }
    }

    return nil
}
```

**Acceptance Criteria**:
- [ ] Checkpointing enabled only for jujutsu
- [ ] Checkpoints every 2 minutes (configurable)
- [ ] Checkpoint commits are cheap (<100ms)
- [ ] Recovery on restart detects incomplete executions
- [ ] Lost work limited to checkpoint interval
- [ ] No history pollution (can squash checkpoints)
- [ ] Configuration via environment variable
- [ ] Integration tests with simulated crashes
- [ ] Documentation of recovery procedure

**Files**:
- Create: `internal/executor/checkpoint.go`
- Create: `internal/executor/recovery.go`
- Modify: `internal/executor/executor.go`
- Create: `internal/executor/checkpoint_test.go`

---

#### vc-223: VCS Operation Audit Trail
**Type**: Task
**Priority**: 2
**Effort**: 3 days
**Parent**: vc-203
**Depends On**: vc-214

**Description**:
Integrate jujutsu's operation log into VC's activity feed for complete audit trail.

**Design**:
```go
// internal/vcs/jujutsu.go

type JujutsuOperation struct {
    ID          string
    Timestamp   time.Time
    Command     string
    Description string
    User        string
}

func (j *JujutsuVCS) GetOperationLog(ctx context.Context, limit int) ([]JujutsuOperation, error) {
    cmd := exec.CommandContext(ctx, "jj", "op", "log", "--limit", fmt.Sprintf("%d", limit), "--no-graph")
    output, err := cmd.Output()
    if err != nil {
        return nil, fmt.Errorf("jj op log failed: %w", err)
    }

    // Parse jj op log output
    return parseOperationLog(string(output))
}
```

**Activity Feed Integration**:
```go
// internal/activity/vcs_operations.go

func (a *ActivityFeed) syncVCSOperations(ctx context.Context) error {
    jjVCS, ok := a.vcs.(*vcs.JujutsuVCS)
    if !ok {
        // Only jujutsu has operation log
        return nil
    }

    // Get recent operations
    ops, err := jjVCS.GetOperationLog(ctx, 100)
    if err != nil {
        return err
    }

    // Record as events
    for _, op := range ops {
        a.RecordEvent(ctx, Event{
            Type:      EventVCSOperation,
            Timestamp: op.Timestamp,
            Data: VCSOperationData{
                OperationID: op.ID,
                Command:     op.Command,
                Description: op.Description,
                User:        op.User,
            },
        })
    }

    return nil
}
```

**CLI Command**:
```bash
vc audit --vcs-log --limit 50

Output:
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
Combined Audit Trail (VC + Jujutsu)
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

14:30:00 | executor_claimed        | vc-42
14:30:05 | agent_spawned           | Claude Code
14:30:10 | agent_tool_use          | Read: auth.go
14:30:15 | file_modified           | auth.go
14:30:20 | VCS: snapshot           | working copy
14:30:21 | VCS: describe           | "Fix auth token validation"
14:30:22 | VCS: new commit         | Created commit abc123
14:30:30 | issue_discovered        | vc-100
14:30:35 | VCS: snapshot           | working copy
14:30:36 | VCS: describe           | "Discovered issues"
14:30:40 | quality_gates_started   |
14:31:00 | test_run                | PASS
14:31:20 | VCS: push               | Pushed to origin
14:31:25 | executor_completed      | vc-42

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
```

**Acceptance Criteria**:
- [ ] Jujutsu operation log parsed correctly
- [ ] VCS operations recorded in activity feed
- [ ] `vc audit --vcs-log` shows combined view
- [ ] Timestamps synchronized
- [ ] Can filter by issue ID
- [ ] Can export audit trail (JSON, CSV)
- [ ] Documentation of audit capabilities
- [ ] Only enabled for jujutsu (graceful for git)

**Files**:
- Modify: `internal/vcs/jujutsu.go`
- Create: `internal/activity/vcs_operations.go`
- Modify: `cmd/vc/audit.go`

---

#### vc-224: Quality Gate Rollback
**Type**: Task
**Priority**: 2
**Effort**: 3 days
**Parent**: vc-203
**Depends On**: vc-222

**Description**:
Implement automatic rollback on quality gate failure (jujutsu only).

**Use Case**:
```
Agent completes work on vc-42
Discovers 3 new issues (vc-100, vc-101, vc-102)
Exports, commits
Quality gates run: tests FAIL
Decision: Rollback everything (including discovered issues)
```

**Design**:
```go
// internal/executor/quality_gates.go

type QualityGateConfig struct {
    RollbackOnFailure bool // Default: false (keep changes for analysis)
}

func (e *Executor) runQualityGatesWithRollback(ctx context.Context, result *ExecutionResult) error {
    // Checkpoint before quality gates
    if err := e.checkpoint(ctx, "before-quality-gates"); err != nil {
        return err
    }

    // Run quality gates
    gates := e.createQualityGates()
    gateResult, err := gates.Run(ctx, result)

    if err != nil || !gateResult.Passed {
        e.logger.Error("Quality gates failed: %v", err)

        // Rollback if configured (jujutsu only)
        if e.config.QualityGates.RollbackOnFailure {
            if err := e.rollbackToCheckpoint(ctx, "before-quality-gates"); err != nil {
                return fmt.Errorf("rollback failed: %w", err)
            }
            e.logger.Info("Rolled back changes due to quality gate failure")
        }

        return fmt.Errorf("quality gates failed")
    }

    return nil
}

func (e *Executor) rollbackToCheckpoint(ctx context.Context, checkpointName string) error {
    jjVCS, ok := e.vcs.(*vcs.JujutsuVCS)
    if !ok {
        return fmt.Errorf("rollback only supported with jujutsu")
    }

    // Use jj undo to rollback
    return jjVCS.Undo(ctx)
}
```

**Jujutsu VCS Extension**:
```go
// internal/vcs/jujutsu.go

func (j *JujutsuVCS) Undo(ctx context.Context) error {
    cmd := exec.CommandContext(ctx, "jj", "undo")
    if err := cmd.Run(); err != nil {
        return fmt.Errorf("jj undo failed: %w", err)
    }
    return nil
}

func (j *JujutsuVCS) UndoToOperation(ctx context.Context, operationID string) error {
    cmd := exec.CommandContext(ctx, "jj", "op", "undo", operationID)
    if err := cmd.Run(); err != nil {
        return fmt.Errorf("jj op undo failed: %w", err)
    }
    return nil
}
```

**Acceptance Criteria**:
- [ ] Checkpoint created before quality gates
- [ ] Rollback on quality gate failure (if configured)
- [ ] Rollback includes discovered issues
- [ ] Works only with jujutsu backend
- [ ] Configuration option for rollback behavior
- [ ] Activity feed records rollback events
- [ ] Tests verify rollback correctness
- [ ] Documentation of rollback behavior

**Files**:
- Modify: `internal/executor/quality_gates.go`
- Modify: `internal/vcs/jujutsu.go`
- Create: `internal/executor/rollback_test.go`

---

#### vc-225: Operation Undo Support
**Type**: Task
**Priority**: 3
**Effort**: 2 days
**Parent**: vc-203
**Depends On**: vc-224

**Description**:
CLI command for undoing operations using jujutsu's undo capability.

**Usage**:
```bash
# Undo last operation
vc undo

# Undo specific operation by ID
vc undo --operation abc123

# Show operation log
vc log --operations
```

**Implementation**:
```go
// cmd/vc/undo.go

func undoCommand() *cobra.Command {
    cmd := &cobra.Command{
        Use:   "undo",
        Short: "Undo VCS operations (jujutsu only)",
        Long: `Undo version control operations using jujutsu's operation log.

This command only works with jujutsu backend. It allows you to undo:
  - Commits
  - Pushes
  - Merges
  - Any other VCS operation

Examples:
  vc undo                    # Undo last operation
  vc undo --operation abc123 # Undo specific operation
  vc log --operations        # Show operation log
        `,
        RunE: runUndo,
    }

    cmd.Flags().String("operation", "", "Specific operation ID to undo")

    return cmd
}

func runUndo(cmd *cobra.Command, args []string) error {
    ctx := cmd.Context()

    vcs, err := vcs.DetectVCS()
    if err != nil {
        return err
    }

    jjVCS, ok := vcs.(*vcs.JujutsuVCS)
    if !ok {
        return fmt.Errorf("vc undo only works with jujutsu backend")
    }

    operationID := cmd.Flags().GetString("operation")

    if operationID != "" {
        // Undo specific operation
        if err := jjVCS.UndoToOperation(ctx, operationID); err != nil {
            return err
        }
        fmt.Printf("✓ Undid operation %s\n", operationID)
    } else {
        // Undo last operation
        if err := jjVCS.Undo(ctx); err != nil {
            return err
        }
        fmt.Println("✓ Undid last operation")
    }

    // Re-import JSONL after undo
    // (database may be out of sync with rolled-back JSONL)
    storage := getStorage()
    if err := storage.ImportJSONL(ctx, ".beads/issues.jsonl"); err != nil {
        fmt.Fprintf(os.Stderr, "Warning: import after undo failed: %v\n", err)
    }

    return nil
}
```

**Acceptance Criteria**:
- [ ] `vc undo` undoes last operation
- [ ] `vc undo --operation ID` undoes specific operation
- [ ] Re-imports JSONL after undo
- [ ] Error if not using jujutsu
- [ ] Integration tests
- [ ] Documentation with examples

**Files**:
- Create: `cmd/vc/undo.go`
- Modify: `cmd/vc/log.go` (add --operations flag)

---

#### vc-226: Jujutsu Performance Optimization
**Type**: Task
**Priority**: 3
**Effort**: 3 days
**Parent**: vc-203
**Depends On**: vc-201 (all)

**Description**:
Optimize jujutsu operations for performance, ensure competitive with git.

**Optimization Areas**:

1. **Batch Operations**:
```go
// Instead of: commit, pull, push separately
// Combine: commit + fetch + push in single flow
func (e *Executor) optimizedSync(ctx context.Context) error {
    // Export + describe + new (minimal overhead)
    e.exportJSONL(ctx)
    e.vcs.Commit(ctx, ".beads/issues.jsonl", "sync")

    // Fetch in background while importing
    fetchDone := make(chan error, 1)
    go func() {
        fetchDone <- e.vcs.Pull(ctx)
    }()

    // Import local changes
    e.importJSONL(ctx)

    // Wait for fetch
    if err := <-fetchDone; err != nil {
        return err
    }

    // Push
    return e.vcs.Push(ctx)
}
```

2. **Lazy Conflict Detection**:
```go
// Only parse conflicts when necessary
type LazyConflictDetector struct {
    vcs vcs.VCS
    cached *ConflictReport
}

func (d *LazyConflictDetector) HasConflicts(ctx context.Context) bool {
    // Quick check without parsing
    return d.vcs.HasMergeConflicts(ctx)
}

func (d *LazyConflictDetector) GetConflicts(ctx context.Context) (*ConflictReport, error) {
    if d.cached != nil {
        return d.cached, nil
    }

    // Parse on demand
    parser := getConflictParser(d.vcs)
    sides, err := parser.ParseConflict(ctx, ".beads/issues.jsonl")
    if err != nil {
        return nil, err
    }

    merger := vcs.NewJSONLMerger(sides)
    result, err := merger.AutoMerge()
    if err != nil {
        return nil, err
    }

    d.cached = &ConflictReport{
        AutoResolvable: result.AutoResolved,
        Conflicts:      len(result.Conflicts),
        Details:        result.Conflicts,
    }

    return d.cached, nil
}
```

3. **Command Pooling**:
```go
// Reuse jj process for multiple commands
type CommandPool struct {
    // Connection to long-running jj daemon
}
```

**Benchmarks**:
```go
// Benchmark sync operations
func BenchmarkGitSync(b *testing.B) {
    // Measure git: export + add + commit + pull + push
}

func BenchmarkJujutsuSync(b *testing.B) {
    // Measure jj: export + describe + new + fetch + push
}

// Target: Jujutsu within 20% of git performance
```

**Acceptance Criteria**:
- [ ] Benchmarks show jj competitive with git (<20% slower)
- [ ] Batch operations implemented where possible
- [ ] Lazy conflict detection reduces overhead
- [ ] No unnecessary command invocations
- [ ] Profiling identifies no hotspots
- [ ] Documentation of performance characteristics
- [ ] CI tracks performance regressions

**Files**:
- Create: `internal/executor/optimized_sync.go`
- Create: `internal/vcs/performance_test.go`
- Create: `internal/vcs/benchmark_test.go`

---

### Epic: vc-204 - Documentation and Migration

#### vc-227: User Documentation
**Type**: Task
**Priority**: 1
**Effort**: 3 days
**Parent**: vc-204
**Depends On**: vc-200 (all), vc-201 (all)

**Description**:
Comprehensive user-facing documentation for VCS features.

**Documentation Structure**:

1. **docs/VCS_SUPPORT.md** - Overview
   - Supported VCS systems (git, jujutsu)
   - Why jujutsu is better for multi-executor setups
   - Architecture overview
   - When to use which backend

2. **docs/JUJUTSU_GUIDE.md** - Jujutsu-specific guide
   - Installing jujutsu
   - Initializing with git backend
   - VC-specific jujutsu workflows
   - Conflict resolution guide
   - Operation undo examples
   - Troubleshooting

3. **docs/CONFLICT_RESOLUTION.md** - Conflict handling
   - How conflicts occur
   - Auto-resolve capabilities
   - Using `vc resolve` command
   - Manual resolution guide
   - Best practices

4. **README.md** - Update main README
   - Add jujutsu to features
   - Link to VCS docs
   - Quick start for both git and jj

**Acceptance Criteria**:
- [ ] All documentation files created
- [ ] README updated with VCS features
- [ ] Code examples tested and working
- [ ] Screenshots/diagrams where helpful
- [ ] Links between docs work
- [ ] Reviewed for clarity and accuracy
- [ ] Spell-checked and formatted

**Files**:
- Create: `docs/VCS_SUPPORT.md`
- Create: `docs/JUJUTSU_GUIDE.md`
- Create: `docs/CONFLICT_RESOLUTION.md`
- Modify: `README.md`

---

#### vc-228: Migration Guide
**Type**: Task
**Priority**: 1
**Effort**: 2 days
**Parent**: vc-204
**Depends On**: vc-227

**Description**:
Step-by-step migration guides for adopting jujutsu.

**docs/MIGRATION_GUIDE.md**:

**Content**:
```markdown
# VC VCS Migration Guide

## Git to Jujutsu (Recommended)

### Prerequisites
- VC already working with git
- Jujutsu installed (`brew install jj`)
- Backup of .beads/issues.jsonl

### Migration Steps

1. **Initialize Jujutsu with Git Backend**
   ```bash
   cd your-project
   jj git init --git-backend
   ```

   This creates `.jj/` directory alongside `.git/`.
   Both VCS coexist peacefully.

2. **Verify VC Detects Jujutsu**
   ```bash
   vc status
   # Should show: "Using jujutsu for version control"
   ```

3. **Test Sync**
   ```bash
   vc execute --limit 1
   # Monitor for VCS operations in logs
   ```

4. **Verify Git Compatibility**
   ```bash
   git log
   # Should show commits from both git and jj
   ```

5. **Done!**
   VC now uses jujutsu internally.
   You can still use git for your code.

## Rollback to Git Only

If you want to remove jujutsu:

```bash
rm -rf .jj/
vc status
# Should show: "Using git for version control"
```

## Pure Jujutsu (Advanced)

For users who want pure jj without git backend:

1. Export issues: `vc export -o backup.jsonl`
2. Remove git: `rm -rf .git .jj`
3. Initialize jj: `jj init`
4. Initialize VC: `vc init`
5. Import issues: `vc import -i backup.jsonl`

## Troubleshooting

### VC still uses git after installing jj
- Run: `jj git init --git-backend` in project root
- Verify: `ls -la | grep .jj`

### Conflicts after migration
- Run: `vc resolve --auto`
- See: docs/CONFLICT_RESOLUTION.md

### Performance issues
- Check: `jj --version` (ensure latest)
- See: docs/JUJUTSU_GUIDE.md#performance
```

**Acceptance Criteria**:
- [ ] Migration guide complete
- [ ] Step-by-step instructions tested
- [ ] Rollback procedure documented
- [ ] Troubleshooting section comprehensive
- [ ] Screenshots for key steps
- [ ] Reviewed by early testers

**Files**:
- Create: `docs/MIGRATION_GUIDE.md`

---

#### vc-229: Configuration Reference
**Type**: Task
**Priority**: 2
**Effort**: 2 days
**Parent**: vc-204
**Depends On**: vc-209

**Description**:
Complete reference for VCS configuration options.

**docs/CONFIGURATION.md** (update existing or create):

**Content**:
```markdown
# VC Configuration Reference

## VCS Configuration

### Config File (.vc/config.yaml)

```yaml
vcs:
  # VCS backend type
  # Options: auto, git, jj
  # Default: auto
  type: auto

  # Prefer jujutsu if both installed
  # Only used when type: auto
  # Default: true
  prefer_jujutsu: true

  # Auto-commit on export
  # Default: true
  auto_commit: true

  # Auto-push on sync
  # Default: false (requires manual vc sync --push)
  auto_push: false

  # Checkpoint interval (jujutsu only)
  # Format: duration string (e.g., "2m", "5m")
  # Default: "2m"
  checkpoint_interval: "2m"

  # Rollback on quality gate failure (jujutsu only)
  # Default: false (keep changes for debugging)
  rollback_on_failure: false
```

### Environment Variables

All config file settings can be overridden via environment:

```bash
# Force specific VCS backend
export VC_VCS=git      # Use git
export VC_VCS=jj       # Use jujutsu
export VC_VCS=auto     # Auto-detect (default)

# Auto-commit behavior
export VC_AUTO_COMMIT=true
export VC_AUTO_PUSH=false

# Jujutsu-specific
export VC_CHECKPOINT_INTERVAL=2m
export VC_ROLLBACK_ON_FAILURE=false
```

### VCS Detection Order

When `type: auto`:

1. Check for `.jj/` directory → Use jujutsu
2. Check for `.git/` directory → Use git
3. Error if neither found

### Command-Line Overrides

```bash
# Force VCS for single command
vc execute --vcs git
vc sync --vcs jj
```

## Conflict Resolution Config

```yaml
conflict_resolution:
  # Auto-resolve on sync
  # Default: true
  auto_resolve: true

  # Fail execution if conflicts remain
  # Default: false (jujutsu allows deferred)
  fail_on_conflict: false

  # Log auto-resolve decisions
  # Default: true
  log_decisions: true
```

## Examples

### Git-Only Setup
```yaml
vcs:
  type: git
  auto_commit: true
  auto_push: false
```

### Jujutsu with Aggressive Checkpointing
```yaml
vcs:
  type: jj
  checkpoint_interval: "1m"
  rollback_on_failure: true
```

### Production Multi-Executor
```yaml
vcs:
  type: auto
  prefer_jujutsu: true
  auto_commit: true
  auto_push: true

conflict_resolution:
  auto_resolve: true
  fail_on_conflict: false
```
```

**Acceptance Criteria**:
- [ ] All config options documented
- [ ] Examples for common scenarios
- [ ] Environment variables listed
- [ ] Detection order explained
- [ ] Default values specified
- [ ] Examples tested

**Files**:
- Modify or create: `docs/CONFIGURATION.md`

---

#### vc-230: Tutorial and Examples
**Type**: Task
**Priority**: 2
**Effort**: 3 days
**Parent**: vc-204
**Depends On**: vc-227, vc-228

**Description**:
Hands-on tutorials with working examples.

**docs/tutorials/JUJUTSU_TUTORIAL.md**:

**Content**:

1. **Tutorial 1: Basic Setup**
   - Install jujutsu
   - Initialize in existing git repo
   - Run first sync
   - Verify operation log

2. **Tutorial 2: Conflict Resolution**
   - Simulate two executors
   - Create conflict
   - Use `vc resolve --auto`
   - Verify auto-resolve

3. **Tutorial 3: Crash Recovery**
   - Enable checkpointing
   - Start long-running agent
   - Kill process mid-execution
   - Restart and verify recovery

4. **Tutorial 4: Multi-Executor Setup**
   - Configure 3 executors
   - Different machines/workspaces
   - Monitor conflict rate
   - Verify smooth collaboration

**Example Scripts**:
```bash
# examples/jujutsu-demo/setup.sh
#!/bin/bash
set -e

echo "Setting up Jujutsu demo..."

# Create demo repo
mkdir vc-jj-demo
cd vc-jj-demo

# Initialize git
git init
git remote add origin https://github.com/user/vc-demo

# Initialize VC
vc init

# Initialize jujutsu
jj git init --git-backend

# Create sample issues
vc create "Example task 1" -p 1
vc create "Example task 2" -p 2

# Export and sync
vc sync

echo "✓ Demo setup complete!"
echo "Run: vc execute"
```

**Acceptance Criteria**:
- [ ] 4 tutorials created
- [ ] Each tutorial tested end-to-end
- [ ] Example scripts work
- [ ] Screen recordings/GIFs for key steps
- [ ] Troubleshooting tips included
- [ ] Feedback from beta testers

**Files**:
- Create: `docs/tutorials/JUJUTSU_TUTORIAL.md`
- Create: `examples/jujutsu-demo/setup.sh`
- Create: `examples/jujutsu-demo/simulate-conflict.sh`
- Create: `examples/jujutsu-demo/README.md`

---

## Dependencies and Timeline

### Dependency Graph

```
vc-200 (VCS Abstraction)
  ├─ vc-205 (Design Interface)
  ├─ vc-206 (Git Backend) ← depends on vc-205
  ├─ vc-207 (Jujutsu Backend) ← depends on vc-205
  ├─ vc-208 (Auto-Detection) ← depends on vc-206, vc-207
  ├─ vc-209 (Configuration) ← depends on vc-208
  └─ vc-210 (Unit Tests) ← depends on vc-206, vc-207, vc-208

vc-201 (Executor Integration)
  ├─ vc-211 (Migrate Sync) ← depends on vc-200 (all)
  ├─ vc-212 (Export/Commit) ← depends on vc-211
  ├─ vc-213 (Import/Pull) ← depends on vc-212
  ├─ vc-214 (Activity Feed) ← depends on vc-211, vc-212, vc-213
  └─ vc-215 (Integration Tests) ← depends on vc-211-214

vc-202 (Smart Conflict Resolution)
  ├─ vc-216 (Conflict Parser) ← depends on vc-200
  ├─ vc-217 (Merge Algorithm) ← depends on vc-216
  ├─ vc-218 (vc resolve command) ← depends on vc-217
  ├─ vc-219 (Executor Integration) ← depends on vc-218
  ├─ vc-220 (Detection/Reporting) ← depends on vc-219
  └─ vc-221 (Testing) ← depends on vc-216-220

vc-203 (Advanced Features)
  ├─ vc-222 (Checkpointing) ← depends on vc-201 (all)
  ├─ vc-223 (Audit Trail) ← depends on vc-214
  ├─ vc-224 (Rollback) ← depends on vc-222
  ├─ vc-225 (Undo Command) ← depends on vc-224
  └─ vc-226 (Performance) ← depends on vc-201 (all)

vc-204 (Documentation)
  ├─ vc-227 (User Docs) ← depends on vc-200, vc-201
  ├─ vc-228 (Migration Guide) ← depends on vc-227
  ├─ vc-229 (Config Reference) ← depends on vc-209
  └─ vc-230 (Tutorial) ← depends on vc-227, vc-228
```

### Timeline (Estimated)

**Week 1-2: VCS Abstraction (vc-200)**
- Design interface (3 days)
- Implement Git backend (4 days)
- Implement Jujutsu backend (5 days)
- Auto-detection + config (3 days)
- Testing (2 days)

**Week 3-4: Executor Integration (vc-201)**
- Migrate sync operations (3 days)
- Export/commit cycle (2 days)
- Import/pull cycle (3 days)
- Activity feed integration (3 days)
- Integration tests (4 days)

**Week 5-7: Smart Conflict Resolution (vc-202)**
- Conflict parser (3 days)
- Merge algorithm (5 days)
- vc resolve command (4 days)
- Executor integration (3 days)
- Detection/reporting (3 days)
- Comprehensive testing (4 days)

**Week 8-9: Advanced Features (vc-203)**
- Checkpointing (4 days)
- Audit trail (3 days)
- Rollback (3 days)
- Undo command (2 days)
- Performance optimization (3 days)

**Week 10: Documentation (vc-204)**
- User documentation (3 days)
- Migration guide (2 days)
- Config reference (2 days)
- Tutorial (3 days)

**Total: 10 weeks**

### Parallel Work Opportunities

- vc-206 and vc-207 can be done in parallel (Git and Jujutsu backends)
- vc-222 through vc-226 can be parallelized (Advanced features)
- vc-227 through vc-230 can be parallelized (Documentation)

With 2 developers: **~7 weeks**
With 3 developers: **~5 weeks**

---

## Risk Assessment

### Technical Risks

| Risk | Probability | Impact | Mitigation |
|------|-------------|--------|------------|
| Jujutsu not widely adopted | Medium | Low | Git remains default, jj optional |
| Breaking changes in jj | Low | Medium | Pin jj version, test upgrades |
| Performance issues | Low | Medium | Benchmark early, optimize aggressively |
| Complex conflict scenarios | Medium | High | Extensive testing, fallback to manual |
| Git compatibility issues | Low | High | Use --git-backend, test interop |

### Project Risks

| Risk | Probability | Impact | Mitigation |
|------|-------------|--------|------------|
| Timeline slip | Medium | Medium | Prioritize P0/P1, defer P2/P3 |
| Resource constraints | Medium | Medium | Clear dependencies, parallelizable work |
| User confusion | Low | Medium | Excellent docs, gradual rollout |
| Regression in git support | Low | High | Comprehensive tests, CI coverage |

---

## Success Criteria

### Must-Have (MVP)

- [ ] VCS abstraction layer complete
- [ ] Git backend fully functional (backward compatible)
- [ ] Jujutsu backend working with --git-backend
- [ ] Auto-detection working
- [ ] Basic conflict resolution (manual)
- [ ] Integration tests passing
- [ ] Documentation complete

### Should-Have (V1)

- [ ] Smart JSONL conflict resolution
- [ ] `vc resolve --auto` command
- [ ] Executor auto-resolve integration
- [ ] Activity feed VCS events
- [ ] Migration guide
- [ ] >90% auto-resolve rate

### Nice-to-Have (V2)

- [ ] Micro-checkpointing
- [ ] VCS operation audit trail
- [ ] Quality gate rollback
- [ ] `vc undo` command
- [ ] Performance competitive with git
- [ ] Comprehensive tutorials

---

## Metrics and Monitoring

### Development Metrics

- Code coverage: >85% for vcs package
- Integration test coverage: Git and Jujutsu
- Performance benchmarks: <20% slower than git
- Documentation coverage: All features documented

### Production Metrics

- Auto-resolve success rate: >90%
- Conflict frequency: <10% of syncs
- Manual resolution time: <5 minutes average
- Crash recovery success: 100%
- User adoption: Track git vs. jj usage

---

## Appendix: Issue Templates

### Epic Template
```
Title: [Epic Name]
Type: Epic
Priority: [0-3]
Description: [1-2 paragraphs]
Design: [High-level approach]
Acceptance Criteria: [5-10 bullets]
Child Issues: [List of IDs]
Estimated Effort: [weeks]
```

### Task Template
```
Title: [Task Name]
Type: Task
Priority: [1-3]
Parent: [Epic ID]
Depends On: [Comma-separated IDs]
Estimated Effort: [days]
Description: [What needs to be done]
Design: [Implementation approach, code examples]
Acceptance Criteria: [Checklist of requirements]
Files: [List of files to create/modify]
```

---

## Next Steps

1. **Review this design document**
   - Technical review
   - Timeline review
   - Resource allocation

2. **File epics and tasks**
   - Create vc-200 through vc-204 (epics)
   - Create vc-205 through vc-230 (tasks)
   - Set dependencies in beads

3. **Prioritize for post-bootstrap**
   - After vc-9 complete
   - P0: vc-200, vc-201
   - P1: vc-202, vc-204
   - P2: vc-203

4. **Begin implementation**
   - Start with vc-205 (VCS interface design)
   - Parallel work on vc-206 and vc-207
   - Iterative development with testing

---

**End of Design Document**
