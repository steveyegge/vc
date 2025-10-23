#!/bin/bash
# File Jujutsu Integration Issues for VC
# Generated from: docs/JUJUTSU_INTEGRATION_DESIGN.md

set -e

echo "Filing Jujutsu Integration epics and tasks..."
echo ""

# =============================================================================
# EPICS (vc-200 through vc-204)
# =============================================================================

echo "Creating Epic: vc-200 - VCS Abstraction Layer"
bd create \
  "VCS Abstraction Layer" \
  --type epic \
  --priority 0 \
  --description "Create version control abstraction enabling both git and jujutsu backends. Foundation for all VCS work." \
  --design "Design VCS interface with methods: IsRepo, HasChanges, Commit, Pull, Push, etc. Implement GitVCS (refactor existing code) and JujutsuVCS (new backend). Auto-detection prefers jj over git. Config system allows explicit selection. See docs/JUJUTSU_INTEGRATION_DESIGN.md for details." \
  --acceptance "
- VCS interface defined with all necessary operations
- Git backend implements interface (backward compatible)
- Jujutsu backend implements interface (with auto-commit model)
- Auto-detection working (checks jj first, then git)
- Configuration system supports explicit VCS selection
- Unit tests >90% coverage
- No breaking changes to existing git users
"

echo "Creating Epic: vc-201 - Executor VCS Integration"
bd create \
  "Executor VCS Integration" \
  --type epic \
  --priority 0 \
  --description "Migrate executor to use VCS abstraction for all version control operations." \
  --design "Replace direct git commands with VCS interface calls. Inject VCS instance into executor. Update sync loop: export → commit → pull → auto-resolve → import → push. Integrate VCS events into activity feed. See docs/JUJUTSU_INTEGRATION_DESIGN.md for details." \
  --acceptance "
- All executor git operations use VCS abstraction
- Sync workflow works with both git and jujutsu
- Export/commit cycle adapted for auto-commit model
- Import/pull cycle handles conflicts gracefully
- Activity feed records VCS operations
- Integration tests pass for both backends
- No user-visible changes for git users
"

echo "Creating Epic: vc-202 - Smart JSONL Conflict Resolution"
bd create \
  "Smart JSONL Conflict Resolution" \
  --type epic \
  --priority 1 \
  --description "Intelligent conflict resolution for discovered issues and concurrent modifications using VC's domain knowledge." \
  --design "Parse conflicts from both git (markers) and jj (logical). Semantic merge algorithm: new issues = auto-merge both, dependencies/labels = union, same field changed = conflict. vc resolve command with --auto flag. Executor auto-resolve in sync loop. See docs/JUJUTSU_INTEGRATION_DESIGN.md for details." \
  --acceptance "
- JSONL conflicts parsed from git and jujutsu formats
- Semantic merge algorithm auto-resolves >95% of conflicts
- vc resolve command works (auto, interactive, dry-run modes)
- Executor auto-resolve integrated into sync loop
- Conflict detection and reporting comprehensive
- Tests cover 8+ real-world scenarios
- Documentation complete
"

echo "Creating Epic: vc-203 - Advanced Jujutsu Features"
bd create \
  "Advanced Jujutsu Features" \
  --type epic \
  --priority 2 \
  --description "Leverage jujutsu-specific capabilities: checkpointing, operation log, rollback, undo." \
  --design "Micro-checkpoints every 2 minutes (jj only). VCS operation audit trail from jj op log. Quality gate rollback with jj undo. vc undo command for operation rollback. Performance optimization to match git speed. See docs/JUJUTSU_INTEGRATION_DESIGN.md for details." \
  --acceptance "
- Micro-checkpointing works (2-minute interval, configurable)
- VCS operation log integrated into activity feed
- Quality gate rollback functional (jj only)
- vc undo command working
- Performance within 20% of git
- All features documented
- Tests comprehensive
"

echo "Creating Epic: vc-204 - Documentation and Migration"
bd create \
  "Documentation and Migration" \
  --type epic \
  --priority 1 \
  --description "Comprehensive documentation and migration tooling for VCS features." \
  --design "User docs: VCS_SUPPORT.md, JUJUTSU_GUIDE.md, CONFLICT_RESOLUTION.md. Migration guide: git to jj conversion steps. Configuration reference: all VCS settings. Tutorial: 4 hands-on examples with scripts. See docs/JUJUTSU_INTEGRATION_DESIGN.md for details." \
  --acceptance "
- All documentation files created
- README updated with VCS features
- Migration guide tested end-to-end
- Configuration reference complete
- 4 tutorials with working examples
- Example scripts functional
- Reviewed for clarity and accuracy
"

echo ""
echo "Epics created. Now creating child tasks..."
echo ""

# =============================================================================
# Epic vc-200: VCS Abstraction Layer
# =============================================================================

echo "Creating tasks for Epic vc-200..."

bd create \
  "Design VCS Interface" \
  --type task \
  --priority 1 \
  --description "Design the VCS interface that abstracts version control operations needed by VC executor." \
  --design "
Define VCS interface in internal/vcs/vcs.go with methods:
- Detection: Name(), IsRepo(), HasUpstream(), GetRepoRoot()
- State: HasChanges(), HasMergeConflicts()
- Operations: Add(), Commit(), Pull(), Push()
- History: GetCurrentCommitHash(), GetFileFromHead()
- Config: EnsureIgnoreFile()

Config struct supports type (git/jj/auto) and auto_detect bool.
DetectVCS() checks jj first, then git.
NewVCS(cfg) creates appropriate backend.
" \
  --acceptance "
- VCS interface defined with all necessary operations
- Config struct supports auto-detection and explicit selection
- DetectVCS() checks for jj first, then git
- NewVCS() creates appropriate backend from config
- Interface documented with godoc comments
- Design reviewed and approved
"

bd create \
  "Implement Git Backend" \
  --type task \
  --priority 1 \
  --description "Implement VCS interface for Git backend by refactoring existing git operations." \
  --design "
Create internal/vcs/git.go with GitVCS struct.
Migrate existing git operations from executor:
- IsRepo() → git rev-parse --git-dir
- HasChanges() → git status --porcelain
- Commit() → git add + git commit
- Pull() → git pull
- Push() → git push
- GetCurrentCommitHash() → git rev-parse HEAD
- GetFileFromHead() → git show HEAD:path

All methods use os/exec.Command for git CLI.
" \
  --acceptance "
- GitVCS implements all VCS interface methods
- All existing git functionality preserved
- Unit tests for each method
- Error handling matches current behavior
- No breaking changes to executor
- Worktree detection implemented (optional feature)
"

bd create \
  "Implement Jujutsu Backend" \
  --type task \
  --priority 1 \
  --description "Implement VCS interface for Jujutsu backend with auto-commit awareness." \
  --design "
Create internal/vcs/jujutsu.go with JujutsuVCS struct.
Key adaptations for auto-commit model:
- Commit() → jj describe -m 'msg' && jj new
- Pull() → jj git fetch (no pull in jj)
- Push() → jj git push --all
- HasChanges() → jj diff --summary
- HasMergeConflicts() → jj conflicts

NewJujutsuVCS() returns nil if jj not installed.
Works with --git-backend mode.
" \
  --acceptance "
- JujutsuVCS implements all VCS interface methods
- Auto-commit model properly handled
- Bookmark management working
- Conflict detection via jj conflicts
- Works with --git-backend mode
- Unit tests for each method
- Returns nil if jj not installed
"

bd create \
  "VCS Auto-Detection" \
  --type task \
  --priority 1 \
  --description "Implement VCS auto-detection logic with proper fallback chain." \
  --design "
DetectVCS() function:
1. Check for jj (NewJujutsuVCS() non-nil and IsRepo() true)
2. Fall back to git (GitVCS.IsRepo() true)
3. Error if neither found

Prefer jj over git (if user installed jj, they chose it).
Log which VCS was detected.
Handle edge cases: nested repos, worktrees.
" \
  --acceptance "
- Detects jj repos correctly (checks .jj/ directory)
- Detects git repos correctly (checks .git/ directory)
- Prefers jj over git if both present
- Returns clear error if neither present
- Logs which VCS was detected
- Handles edge cases (nested repos, worktrees)
- Integration tests with real repos
"

bd create \
  "VCS Configuration System" \
  --type task \
  --priority 2 \
  --description "Add configuration options for VCS selection and behavior." \
  --design "
Config file (.vc/config.yaml):
  vcs:
    type: auto          # auto, git, jj
    prefer_jujutsu: true
    auto_commit: true
    auto_push: true

Environment variables:
  VC_VCS=git|jj|auto
  VC_AUTO_COMMIT=true|false
  VC_AUTO_PUSH=true|false

Environment overrides config file.
Config validation on startup.
" \
  --acceptance "
- Config file supports VCS settings
- Environment variables override config
- VC_VCS variable works correctly
- Config validation on startup
- vc config show displays VCS settings
- Migration from old config format (if needed)
- Documentation for all settings
"

bd create \
  "VCS Unit Tests" \
  --type task \
  --priority 1 \
  --description "Comprehensive unit tests for VCS abstraction layer." \
  --design "
Test coverage:
- GitVCS all methods (mocked git commands)
- JujutsuVCS all methods (mocked jj commands)
- VCS detection logic
- Config parsing and validation
- Error handling
- Edge cases (no VCS, both VCS, etc.)

Use gomock or testify for command mocking.
Integration tests with real repos in CI.
" \
  --acceptance "
- >90% code coverage for vcs package
- All VCS methods tested
- Mock command execution for isolation
- Test with real repos in CI (integration tests)
- Error cases covered
- Documentation examples tested
- CI passes on all platforms
"

# =============================================================================
# Epic vc-201: Executor VCS Integration
# =============================================================================

echo "Creating tasks for Epic vc-201..."

bd create \
  "Migrate Executor Sync Operations" \
  --type task \
  --priority 1 \
  --description "Refactor executor sync operations to use VCS abstraction instead of direct git commands." \
  --design "
Replace all git command execution with VCS interface calls:
- exec.Command('git', 'add') → vcs.Add()
- exec.Command('git', 'commit') → vcs.Commit()
- exec.Command('git', 'pull') → vcs.Pull()
- exec.Command('git', 'push') → vcs.Push()

Add vcs VCS field to Executor struct.
Inject via constructor/initializer.
Preserve error handling behavior.
" \
  --acceptance "
- All git commands replaced with VCS calls
- Executor struct has vcs VCS field
- VCS injected via constructor
- Sync workflow unchanged for git users
- Works with both git and jj backends
- Error handling preserved
- Integration tests pass
"

bd create \
  "Migrate Export/Commit Cycle" \
  --type task \
  --priority 1 \
  --description "Update the export → commit cycle to work with both git and jujutsu models." \
  --design "
Git: Export → stage (git add) → commit (git commit)
Jj: Export → describe (jj describe) → new (jj new)

VCS.Commit() abstracts the difference:
- Git: stages and commits
- Jj: describes working copy commit and starts new one

Export happens immediately before commit.
Commit messages include executor instance ID.
" \
  --acceptance "
- Export writes to JSONL file
- VCS.Commit() called after export
- Works correctly with git backend
- Works correctly with jj backend
- Commit messages include executor instance ID
- Error handling for export and commit failures
- Activity feed events recorded
"

bd create \
  "Migrate Import/Pull Cycle" \
  --type task \
  --priority 1 \
  --description "Update the pull → import cycle with conflict awareness." \
  --design "
Pull workflow:
1. VCS.Pull() - git pull OR jj git fetch
2. VCS.HasMergeConflicts() - check for conflicts
3. If conflicts:
   - Git: block and require resolution
   - Jj: log warning, attempt auto-resolve, continue
4. Import JSONL into database

Activity feed records pull/import events.
" \
  --acceptance "
- Pull operation uses VCS abstraction
- Conflict detection works for both git and jj
- Import proceeds even with jj conflicts (deferred)
- Import blocks on git conflicts (current behavior)
- Activity feed records pull/import events
- Error handling for pull and import failures
- Integration tests with conflicts
"

bd create \
  "Activity Feed VCS Integration" \
  --type task \
  --priority 2 \
  --description "Integrate VCS operations into activity feed for observability." \
  --design "
New event types:
- EventVCSCommit
- EventVCSPull
- EventVCSPush
- EventVCSConflict

VCSEventData struct:
- VCSType (git/jujutsu)
- Operation (commit/pull/push)
- FilePath
- CommitHash
- Message
- Success
- Error

Record events in executor sync operations.
" \
  --acceptance "
- VCS events defined in activity package
- Commit operations recorded
- Pull operations recorded
- Push operations recorded
- Conflict detections recorded
- Events include VCS type (git/jj)
- vc tail --issue vc-X shows VCS events
- Event schema documented
"

bd create \
  "Executor Integration Tests" \
  --type task \
  --priority 1 \
  --description "End-to-end integration tests for executor with both VCS backends." \
  --design "
Test scenarios:
1. Basic sync (git)
2. Basic sync (jujutsu)
3. Conflict handling (git) - blocks
4. Conflict handling (jujutsu) - defers
5. Crash recovery (jujutsu) - no data loss
6. Multi-executor scenarios

Each test uses real repos (temp directories).
CI runs tests for both backends.
" \
  --acceptance "
- Integration tests for git backend pass
- Integration tests for jj backend pass
- Conflict scenarios tested for both
- Crash recovery tested (jj only)
- Multi-executor scenarios tested
- CI runs tests with both backends
- Tests documented with clear scenarios
"

# =============================================================================
# Epic vc-202: Smart JSONL Conflict Resolution
# =============================================================================

echo "Creating tasks for Epic vc-202..."

bd create \
  "JSONL Conflict Parser" \
  --type task \
  --priority 1 \
  --description "Parse JSONL conflicts from both git and jujutsu conflict formats." \
  --design "
ConflictParser interface:
- ParseConflict(filePath) → (base, ours, theirs)

GitConflictParser:
- Read file, extract <<<<<<< / ======= / >>>>>>> markers
- Parse JSONL sections

JujutsuConflictParser:
- Use 'jj cat -r base/ours/theirs filePath'
- Extract each side from jj

Return ConflictSide struct with base/ours/theirs []byte.
" \
  --acceptance "
- GitConflictParser extracts all three sides
- JujutsuConflictParser uses jj commands
- Handles multiple conflicts in same file
- Handles malformed conflict markers
- Returns structured ConflictSide
- Unit tests with real conflict examples
- Error handling for corrupt conflicts
"

bd create \
  "Semantic JSONL Merge Algorithm" \
  --type task \
  --priority 1 \
  --description "Implement intelligent merging for JSONL issues using VC's domain knowledge." \
  --design "
JSONLMerger algorithm:
1. Parse base/ours/theirs into Issue maps
2. For each issue ID:
   - New issue (one side only) → auto-merge
   - Both added same ID → conflict
   - Both modified → semantic merge by field:
     * Status: conflict if both changed differently
     * Dependencies: union (additive)
     * Labels: union (additive)
     * Notes: concatenate with separator
     * Priority: conflict if both changed differently

Return MergeResult with merged issues and conflicts.
Target >95% auto-resolve rate.
" \
  --acceptance "
- Parses JSONL from all three sides
- Auto-resolves new issue additions (both sides)
- Detects semantic conflicts (same field, different values)
- Merges dependencies as union
- Merges labels as union
- Handles deleted issues correctly
- Returns list of remaining conflicts
- Unit tests with comprehensive scenarios
- >95% auto-resolve rate in simulations
"

bd create \
  "vc resolve Command" \
  --type task \
  --priority 1 \
  --description "CLI command for resolving JSONL conflicts interactively and automatically." \
  --design "
Usage:
  vc resolve --auto           # Auto-resolve, prompt for conflicts
  vc resolve --auto --dry-run # Preview
  vc resolve --interactive    # Prompt for each conflict
  vc resolve --take-ours      # Resolve with our version
  vc resolve --take-theirs    # Resolve with their version

Flow:
1. Detect VCS
2. Check for conflicts
3. Parse conflict (use appropriate parser)
4. Auto-merge with JSONLMerger
5. Display results (auto-resolved count, conflicts)
6. Handle remaining conflicts (interactive/ours/theirs)
7. Write resolved JSONL
8. Mark conflict as resolved in VCS
" \
  --acceptance "
- vc resolve --auto works for simple conflicts
- --dry-run shows preview without changes
- --interactive prompts for each conflict
- --take-ours and --take-theirs work
- Writes resolved JSONL file
- Marks conflict as resolved in VCS
- Works with both git and jj
- Clear error messages
- Help text comprehensive
- Integration tests
"

bd create \
  "Executor Auto-Resolve Integration" \
  --type task \
  --priority 1 \
  --description "Integrate auto-resolve into executor sync loop to handle conflicts automatically." \
  --design "
autoResolveConflicts() function:
1. Check if conflicts exist
2. Parse conflict with appropriate parser
3. Auto-merge with JSONLMerger
4. If fully resolved:
   - Write resolved JSONL
   - Mark resolved
   - Record success event
5. If partially resolved:
   - Git: return error (block)
   - Jj: log warning, continue (defer)

Integrate into sync loop after pull.
" \
  --acceptance "
- Auto-resolve integrated into sync loop
- Conflicts attempted on every pull
- Git executors stop on unresolved conflicts
- Jujutsu executors continue despite conflicts
- Activity feed records auto-resolve attempts
- Logs show auto-resolve progress
- Metrics track auto-resolve success rate
- Integration tests verify behavior
"

bd create \
  "Conflict Detection and Reporting" \
  --type task \
  --priority 2 \
  --description "Enhanced conflict detection, reporting, and monitoring." \
  --design "
Features:
1. detectConflicts() hook after every pull
2. vc status --conflicts command
3. ConflictMetrics collection
4. Activity feed conflict events
5. Prometheus metrics (if enabled)
6. Alert if auto-resolve rate <80%

ConflictReport struct:
- TotalIssues
- AutoResolvable
- Conflicts
- Details (list of conflict fields)
" \
  --acceptance "
- Conflict detection runs after every pull
- vc status --conflicts shows conflict summary
- Metrics track auto-resolve rate
- Activity feed shows conflict events
- Prometheus metrics exported (if enabled)
- Documentation for conflict workflow
- Alert if auto-resolve rate drops below 80%
"

bd create \
  "Conflict Resolution Testing" \
  --type task \
  --priority 1 \
  --description "Comprehensive testing for conflict resolution with real-world scenarios." \
  --design "
8 test scenarios:
1. Simple addition conflicts (both sides add different issues)
2. Same issue modified (conflicting status changes)
3. Dependency additions (union merge)
4. Label additions (union merge)
5. Priority conflicts
6. Delete vs. modify
7. Cascading discovered issues (many issues both sides)
8. Mixed scenario (some auto-resolve, some conflict)

Performance tests: 1000+ issues, <1 second auto-resolve.
Fuzzing tests for parser robustness.
" \
  --acceptance "
- All 8 scenarios tested with unit tests
- Integration tests with real repos (git and jj)
- Performance benchmarks pass
- Edge cases covered (malformed JSONL, etc.)
- Fuzzing tests for parser robustness
- Documentation of test scenarios
- CI runs full conflict test suite
"

# =============================================================================
# Epic vc-203: Advanced Jujutsu Features
# =============================================================================

echo "Creating tasks for Epic vc-203..."

bd create \
  "Micro-Checkpoint System" \
  --type task \
  --priority 2 \
  --description "Implement periodic checkpointing for long-running agent executions (jujutsu only)." \
  --design "
Checkpointer goroutine:
- Runs every 2 minutes (configurable)
- Export database to JSONL
- VCS.Commit() with checkpoint message
- Jj makes this very cheap (<100ms)

Recovery on restart:
- Detect incomplete executions (in_progress issues)
- Import from last checkpoint
- Release claim (allow retry)

Only enabled for jujutsu (git checkpoints too expensive).
" \
  --acceptance "
- Checkpointing enabled only for jujutsu
- Checkpoints every 2 minutes (configurable)
- Checkpoint commits are cheap (<100ms)
- Recovery on restart detects incomplete executions
- Lost work limited to checkpoint interval
- No history pollution (can squash checkpoints)
- Configuration via environment variable
- Integration tests with simulated crashes
- Documentation of recovery procedure
"

bd create \
  "VCS Operation Audit Trail" \
  --type task \
  --priority 2 \
  --description "Integrate jujutsu's operation log into VC's activity feed for complete audit trail." \
  --design "
JujutsuVCS.GetOperationLog():
- Run 'jj op log --limit N --no-graph'
- Parse output into JujutsuOperation structs
- Return list of operations

Activity feed integration:
- Sync VCS operations periodically
- Record as EventVCSOperation
- vc audit --vcs-log shows combined view

Only for jujutsu (git has limited reflog).
" \
  --acceptance "
- Jujutsu operation log parsed correctly
- VCS operations recorded in activity feed
- vc audit --vcs-log shows combined view
- Timestamps synchronized
- Can filter by issue ID
- Can export audit trail (JSON, CSV)
- Documentation of audit capabilities
- Only enabled for jujutsu (graceful for git)
"

bd create \
  "Quality Gate Rollback" \
  --type task \
  --priority 2 \
  --description "Implement automatic rollback on quality gate failure (jujutsu only)." \
  --design "
runQualityGatesWithRollback():
1. Checkpoint before gates
2. Run quality gates
3. If failure and config.rollback_on_failure:
   - VCS.Undo() (jj undo)
   - Rollback includes discovered issues
   - Log rollback event

JujutsuVCS.Undo():
- Run 'jj undo' (undo last operation)
- UndoToOperation(id) for specific operation

Config: rollback_on_failure (default: false)
" \
  --acceptance "
- Checkpoint created before quality gates
- Rollback on quality gate failure (if configured)
- Rollback includes discovered issues
- Works only with jujutsu backend
- Configuration option for rollback behavior
- Activity feed records rollback events
- Tests verify rollback correctness
- Documentation of rollback behavior
"

bd create \
  "Operation Undo Support" \
  --type task \
  --priority 3 \
  --description "CLI command for undoing operations using jujutsu's undo capability." \
  --design "
Commands:
  vc undo                    # Undo last operation
  vc undo --operation abc123 # Undo specific operation
  vc log --operations        # Show operation log

Implementation:
- Check VCS is jujutsu (error otherwise)
- Call JujutsuVCS.Undo() or UndoToOperation()
- Re-import JSONL after undo
- Log undo event

Jujutsu-only feature.
" \
  --acceptance "
- vc undo undoes last operation
- vc undo --operation ID undoes specific operation
- Re-imports JSONL after undo
- Error if not using jujutsu
- Integration tests
- Documentation with examples
"

bd create \
  "Jujutsu Performance Optimization" \
  --type task \
  --priority 3 \
  --description "Optimize jujutsu operations for performance, ensure competitive with git." \
  --design "
Optimizations:
1. Batch operations (combine commit + fetch)
2. Lazy conflict detection (only parse when needed)
3. Command pooling (reuse jj process)
4. Parallel operations (fetch while importing)

Benchmarks:
- BenchmarkGitSync vs BenchmarkJujutsuSync
- Target: Jj within 20% of git performance

Profile and identify hotspots.
" \
  --acceptance "
- Benchmarks show jj competitive with git (<20% slower)
- Batch operations implemented where possible
- Lazy conflict detection reduces overhead
- No unnecessary command invocations
- Profiling identifies no hotspots
- Documentation of performance characteristics
- CI tracks performance regressions
"

# =============================================================================
# Epic vc-204: Documentation and Migration
# =============================================================================

echo "Creating tasks for Epic vc-204..."

bd create \
  "User Documentation" \
  --type task \
  --priority 1 \
  --description "Comprehensive user-facing documentation for VCS features." \
  --design "
Documentation files:
1. docs/VCS_SUPPORT.md - Overview, architecture, when to use which
2. docs/JUJUTSU_GUIDE.md - Installing, workflows, troubleshooting
3. docs/CONFLICT_RESOLUTION.md - How conflicts occur, auto-resolve, manual
4. README.md - Update with VCS features

All include code examples, diagrams, troubleshooting.
" \
  --acceptance "
- All documentation files created
- README updated with VCS features
- Code examples tested and working
- Screenshots/diagrams where helpful
- Links between docs work
- Reviewed for clarity and accuracy
- Spell-checked and formatted
"

bd create \
  "Migration Guide" \
  --type task \
  --priority 1 \
  --description "Step-by-step migration guides for adopting jujutsu." \
  --design "
docs/MIGRATION_GUIDE.md:
1. Git to Jujutsu (jj git init --git-backend)
2. Rollback to Git (rm -rf .jj/)
3. Pure Jujutsu (export, reinit, import)
4. Troubleshooting

Each section:
- Prerequisites
- Step-by-step instructions
- Verification steps
- Rollback procedure
" \
  --acceptance "
- Migration guide complete
- Step-by-step instructions tested
- Rollback procedure documented
- Troubleshooting section comprehensive
- Screenshots for key steps
- Reviewed by early testers
"

bd create \
  "Configuration Reference" \
  --type task \
  --priority 2 \
  --description "Complete reference for VCS configuration options." \
  --design "
Update docs/CONFIGURATION.md:
- VCS config section (type, prefer_jujutsu, auto_commit, auto_push)
- Environment variables (VC_VCS, etc.)
- VCS detection order
- Command-line overrides
- Examples for common scenarios
- Default values

All options documented with examples.
" \
  --acceptance "
- All config options documented
- Examples for common scenarios
- Environment variables listed
- Detection order explained
- Default values specified
- Examples tested
"

bd create \
  "Tutorial and Examples" \
  --type task \
  --priority 2 \
  --description "Hands-on tutorials with working examples." \
  --design "
docs/tutorials/JUJUTSU_TUTORIAL.md:
1. Tutorial 1: Basic Setup
2. Tutorial 2: Conflict Resolution
3. Tutorial 3: Crash Recovery
4. Tutorial 4: Multi-Executor Setup

examples/jujutsu-demo/:
- setup.sh
- simulate-conflict.sh
- README.md

Each tutorial tested end-to-end.
Screen recordings/GIFs for key steps.
" \
  --acceptance "
- 4 tutorials created
- Each tutorial tested end-to-end
- Example scripts work
- Screen recordings/GIFs for key steps
- Troubleshooting tips included
- Feedback from beta testers
"

echo ""
echo "All issues filed!"
echo ""
echo "Next steps:"
echo "1. Add dependencies between issues:"
echo "   - vc-206, vc-207 depend on vc-205"
echo "   - vc-208 depends on vc-206, vc-207"
echo "   - vc-201 tasks depend on vc-200 (all)"
echo "   - etc. (see JUJUTSU_INTEGRATION_DESIGN.md dependency graph)"
echo ""
echo "2. Link child issues to epics:"
echo "   - vc-205 through vc-210 → vc-200"
echo "   - vc-211 through vc-215 → vc-201"
echo "   - vc-216 through vc-221 → vc-202"
echo "   - vc-222 through vc-226 → vc-203"
echo "   - vc-227 through vc-230 → vc-204"
echo ""
echo "3. Export to JSONL:"
echo "   bd export -o .beads/issues.jsonl"
echo ""
echo "4. Commit to git:"
echo "   git add .beads/issues.jsonl"
echo "   git commit -m 'Add Jujutsu integration epics and tasks'"
echo ""
