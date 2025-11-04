# Analysis: Beads Issue #209 - Config.yaml Migration

**Date**: 2025-11-03
**Analyst**: Claude (Sonnet 4.5)
**Feature Branch**: `rrnewton/feature-config-enhancements`
**Status**: Ready for integration (with minor changes needed)

## Executive Summary

Beads issue #209 moves configuration (particularly `issue-prefix`) from the database to `config.yaml` as the single source of truth. This is a **backward-compatible change** that affects VC minimally. The Storage interface remains unchanged, but VC needs minor updates to align with best practices.

**Impact**: 🟢 Low - Minor code updates needed
**Breaking Changes**: None
**Action Required**: Update VC's initialization code and config.yaml

---

## What Changed in Beads

### 1. Configuration Source of Truth Migration

**Before** (current):
- `issue_prefix` stored in database `config` table
- Accessed via `store.GetConfig(ctx, "issue_prefix")`
- Set via `store.SetConfig(ctx, "issue_prefix", "vc")`

**After** (feature branch):
- `issue-prefix` (note hyphen) stored in `.beads/config.yaml`
- Accessed via `config.GetIssuePrefix()` (new Go package function)
- Set via `config.SetIssuePrefix(prefix)` (writes to config.yaml)
- Database still has `config` table but `issue_prefix` is no longer stored there
- Database methods `GetConfig/SetConfig` still exist (for other config like compaction settings)

### 2. New Configuration Package

The feature branch adds `internal/config` package to Beads with:

```go
// Initialize config system (must be called once at startup)
func Initialize() error

// Get/Set issue prefix (reads/writes config.yaml)
func GetIssuePrefix() string
func SetIssuePrefix(prefix string) error

// Generic config accessors (for other settings)
func GetString(key string) string
func GetBool(key string) bool
```

**Configuration precedence**:
1. Command-line flags (highest)
2. Environment variables (BD_* prefix)
3. `.beads/config.yaml` file
4. Built-in defaults (lowest)

### 3. Key Implementation Details

**Database behavior**:
- `config` table still exists (for compaction settings, etc.)
- `GetConfig/SetConfig` methods still work
- ID generation now reads prefix from `config.GetIssuePrefix()` instead of database query
- Migration path: `bd init` writes to config.yaml, old databases continue working

**Auto-creation**:
- If `.beads/` directory exists but no `config.yaml`, Beads auto-creates one
- Default template includes helpful comments and all settings
- `bd init` command now creates/updates `config.yaml`

### 4. Standardization Changes

- **Naming**: `issue_prefix` (underscore) → `issue-prefix` (hyphen) everywhere
- **Constants**: Magic strings replaced with typed constants (e.g., `sqlite.MetadataKeyBDVersion`)
- **Validation**: Config files are validated; warnings issued for unsupported keys

---

## Impact on VC

### Current VC Behavior

VC currently uses Beads as a library (not the `bd` CLI) and:

1. **Database**: Uses `.beads/vc.db` (custom name, not `beads.db`)
2. **Initialization**: Calls `beadsStore.SetConfig(ctx, "issue_prefix", "vc")` during startup
3. **Config.yaml**: Has `.beads/config.yaml` but `issue-prefix` is commented out (not set)
4. **Prefix source**: Currently stored in database only (`.beads/beads.db` has it set to "vc")

**File**: `internal/storage/beads/wrapper.go:42-49`
```go
// 1.5. Initialize issue_prefix config if not already set (required by Beads for ID generation)
if prefix, err := beadsStore.GetConfig(ctx, "issue_prefix"); err != nil || prefix == "" {
    // Set default prefix "vc" for VC project
    if err := beadsStore.SetConfig(ctx, "issue_prefix", "vc"); err != nil {
        beadsStore.Close()
        return nil, fmt.Errorf("failed to set issue_prefix config: %w", err)
    }
}
```

### Changes Needed in VC

#### 1. Update `.beads/config.yaml` ✅ **REQUIRED**

**File**: `.beads/config.yaml`

**Change**:
```yaml
# Before (commented out)
# issue-prefix: ""

# After (set explicitly)
issue-prefix: "vc"
```

**Why**: This makes config.yaml the source of truth. Without this, Beads will fall back to "issue" default or try to auto-detect from directory name.

#### 2. Update Initialization Code 🟡 **RECOMMENDED**

**File**: `internal/storage/beads/wrapper.go:35-49`

**Current code**:
```go
func NewVCStorage(ctx context.Context, dbPath string) (*VCStorage, error) {
    beadsStore, err := beadsLib.NewSQLiteStorage(dbPath)
    if err != nil {
        return nil, fmt.Errorf("failed to open Beads storage: %w", err)
    }

    // 1.5. Initialize issue_prefix config if not already set (required by Beads for ID generation)
    if prefix, err := beadsStore.GetConfig(ctx, "issue_prefix"); err != nil || prefix == "" {
        // Set default prefix "vc" for VC project
        if err := beadsStore.SetConfig(ctx, "issue_prefix", "vc"); err != nil {
            beadsStore.Close()
            return nil, fmt.Errorf("failed to set issue_prefix config: %w", err)
        }
    }
    // ...
}
```

**Recommended new code** (if adopting feature branch):
```go
func NewVCStorage(ctx context.Context, dbPath string) (*VCStorage, error) {
    // 1. Initialize Beads config system (reads .beads/config.yaml)
    if err := beadsLib.InitializeConfig(); err != nil {
        return nil, fmt.Errorf("failed to initialize config: %w", err)
    }

    // 1.5. Ensure issue-prefix is set in config.yaml
    // This is now the source of truth (not the database)
    if beadsLib.GetIssuePrefix() == "" {
        // Fallback: set to "vc" if not configured
        if err := beadsLib.SetIssuePrefix("vc"); err != nil {
            return nil, fmt.Errorf("failed to set issue-prefix: %w", err)
        }
    }

    beadsStore, err := beadsLib.NewSQLiteStorage(dbPath)
    if err != nil {
        return nil, fmt.Errorf("failed to open Beads storage: %w", err)
    }
    // ...
}
```

**Why**: This aligns with the new pattern where config.yaml is read before opening storage.

**Note**: The old code will continue to work because:
- `GetConfig/SetConfig` methods still exist
- Database can still store `issue_prefix` (just won't be used for ID generation)
- Beads falls back gracefully if config.yaml isn't initialized

#### 3. Update Sandbox Initialization 🟡 **RECOMMENDED**

**File**: `internal/sandbox/database.go:73-80`

**Current code**:
```go
// Set issue_prefix to 'vc' to match the main database
if err := store.SetConfig(ctx, "issue_prefix", "vc"); err != nil {
    if closeErr := store.Close(); closeErr != nil {
        log.Printf("warning: failed to close store after config error: %v", closeErr)
    }
    return "", fmt.Errorf("failed to set issue_prefix config: %w", err)
}
```

**Recommended new code**:
```go
// Create .beads/config.yaml in sandbox with issue-prefix set to 'vc'
// This matches the main database and ensures ID generation works correctly
sandboxConfigPath := filepath.Join(beadsDir, "config.yaml")
configContent := "issue-prefix: vc\n"
if err := os.WriteFile(sandboxConfigPath, []byte(configContent), 0644); err != nil {
    if closeErr := store.Close(); closeErr != nil {
        log.Printf("warning: failed to close store after config error: %v", closeErr)
    }
    return "", fmt.Errorf("failed to create sandbox config.yaml: %w", err)
}

// Reinitialize config to pick up the sandbox's config.yaml
// (Important because Beads searches upward from CWD for .beads/config.yaml)
if err := beadsLib.InitializeConfig(); err != nil {
    return "", fmt.Errorf("failed to reinitialize config for sandbox: %w", err)
}
```

**Why**: Sandboxes are isolated git worktrees with their own `.beads/` directory. They need their own `config.yaml` to specify the issue prefix.

**Alternative**: Keep using `SetConfig` for sandboxes since they're temporary. The database approach works fine for ephemeral sandboxes.

#### 4. No Changes Needed in Tests ✅

**Impact**: None - tests create in-memory databases and the old `SetConfig` approach works fine for test isolation.

---

## Migration Strategy

### Option A: Conservative (Recommended for VC)

**Keep current code working** while gradually adopting new patterns:

1. ✅ **Now**: Set `issue-prefix: "vc"` in `.beads/config.yaml`
2. 🔄 **Later**: When Beads #209 merges, update initialization to use `config.GetIssuePrefix()`
3. 🔄 **Eventually**: Clean up old database-based config code

**Pros**:
- No rush to change VC code
- Backward compatible
- Works with both current and future Beads versions

**Cons**:
- Slight technical debt (using old pattern temporarily)

### Option B: Proactive

**Adopt new patterns immediately** (requires feature branch):

1. Update VC's `go.mod` to point at feature branch: `github.com/steveyegge/beads @ feature-config-enhancements`
2. Update all code as described above
3. Thoroughly test before Beads #209 merges

**Pros**:
- Clean migration
- Early testing helps Beads find bugs

**Cons**:
- Depends on unmerged branch
- Requires thorough testing

### Recommended: **Option A**

Wait for Beads #209 to be reviewed and merged, then update VC in a single clean commit. In the meantime, just set `issue-prefix` in config.yaml to prepare.

---

## Risks and Mitigations

### Risk 1: Config.yaml vs Database Conflicts
**Scenario**: Config.yaml says "vc", database has "issue" stored in old config table
**Impact**: Beads will use config.yaml value (by design)
**Mitigation**: Set config.yaml correctly, database value becomes irrelevant

### Risk 2: Sandbox Config Isolation
**Scenario**: Sandboxes inherit parent's config.yaml instead of having their own
**Impact**: ID generation works but sandbox loses isolation
**Mitigation**: Ensure sandboxes create their own `.beads/config.yaml` (as recommended above)

### Risk 3: Missing Config During Tests
**Scenario**: Tests run in temp directories without config.yaml
**Impact**: Beads defaults to "issue" prefix or directory name
**Mitigation**: Tests can still use `SetConfig` for database-based config (backward compatible)

### Risk 4: CI/CD Environment Issues
**Scenario**: VC executor runs in environment without writable `.beads/` directory
**Impact**: Can't create config.yaml, fails to initialize
**Mitigation**: Ensure `.beads/config.yaml` is checked into git (not gitignored)

---

## Testing Plan

When adopting Beads #209:

### Unit Tests
- [ ] Test `NewVCStorage` with missing config.yaml (should auto-create)
- [ ] Test `NewVCStorage` with existing config.yaml (should read prefix)
- [ ] Test sandbox initialization creates isolated config.yaml
- [ ] Test ID generation uses config.yaml prefix, not database

### Integration Tests
- [ ] Run VC executor end-to-end with new config system
- [ ] Create issues and verify IDs use "vc" prefix
- [ ] Test sandbox workflow (mission claims issue, creates sandbox DB)
- [ ] Verify `.beads/config.yaml` is source of truth for prefix

### Regression Tests
- [ ] Old databases (with issue_prefix in config table) still work
- [ ] Mixed state (config.yaml + database both have prefix) works correctly
- [ ] Beads library can be used without calling `config.Initialize()` (falls back gracefully)

---

## API Compatibility Matrix

| Feature | Current Beads | After #209 | Breaking? | Notes |
|---------|---------------|------------|-----------|-------|
| `GetConfig(ctx, "issue_prefix")` | ✅ Works | ✅ Works | No | Still reads database config table |
| `SetConfig(ctx, "issue_prefix", "vc")` | ✅ Works | ✅ Works | No | Writes to database but not used for IDs |
| `config.GetIssuePrefix()` | ❌ N/A | ✅ New | No | New function, reads config.yaml |
| `config.SetIssuePrefix("vc")` | ❌ N/A | ✅ New | No | New function, writes config.yaml |
| ID generation uses database prefix | ✅ Yes | ❌ No | No* | Now uses config.yaml, but backward compatible |

**Note**: ID generation change is **not breaking** because:
- Config.yaml takes precedence (documented behavior)
- Database fallback removed but config.yaml required anyway
- `bd init` ensures config.yaml exists

---

## Recommendations for VC

### Immediate Actions (Before Beads #209 Merges)

1. ✅ **Set `issue-prefix: "vc"` in `.beads/config.yaml`**
   - Uncommit the line
   - Set explicit value
   - Commit to git

2. ✅ **Document this in `CLAUDE.md`**
   - Note that config.yaml is source of truth for issue prefix
   - Explain relationship to database config table

3. 🟡 **Optional**: Test with feature branch
   - Create test branch in VC
   - Point `go.mod` at `rrnewton/feature-config-enhancements`
   - Run full test suite
   - Report findings to Beads team

### After Beads #209 Merges

1. ✅ **Update VC initialization code** (see "Changes Needed" section)
2. ✅ **Update sandbox code** to create config.yaml
3. ✅ **Add tests** for config.yaml behavior
4. 🟡 **Optional cleanup**: Remove old database config comments/code

### Long-term Considerations

- **Config validation**: Consider adding VC-specific config validation
- **Config documentation**: Document all VC config options in a central location
- **Config migration**: If VC adds new config options, follow Beads pattern (config.yaml > env vars > flags)

---

## Related Beads Issues

- **#209**: Config.yaml enhancements (this analysis)
- Potentially related: Any issues about configuration management or database schema

---

## Questions for Beads Team

1. **Migration path**: Is there a recommended migration for existing databases with `issue_prefix` in config table?
2. **Sandbox isolation**: What's the recommended pattern for isolated sandboxes with their own config?
3. **Testing**: Should library users call `config.Initialize()` in tests or rely on fallback?
4. **API exports**: Will `config.Initialize/GetIssuePrefix/SetIssuePrefix` be exported in `beads.go` public API?

---

## Conclusion

**Verdict**: ✅ Safe to adopt, minor changes needed

Beads #209 is a **well-designed, backward-compatible change** that improves configuration management. VC's current code will continue to work, but should be updated to align with best practices.

**Recommended timeline**:
- **Now**: Set `issue-prefix` in config.yaml
- **After #209 merges**: Update initialization code in a single PR
- **Future**: Clean up technical debt as needed

**Risk level**: 🟢 Low
**Confidence**: High (based on code review and understanding of VC's usage patterns)
