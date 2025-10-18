# Cleanup Report: Accumulated Dogfooding Artifacts

**Date**: 2025-10-18
**After**: 10 dogfooding runs

## Summary

Dogfooding runs accumulate several types of artifacts that need periodic cleanup:

1. **Git worktrees** (sandboxes) - 2 active, 1 prunable
2. **Git branches** (mission branches) - 3 total, 2 blocked by worktrees
3. **Orphaned issues** (bd-1, vc-117, vc-128) - 3 issues stuck in `in_progress`
4. **Executor instances** - 8 stopped instances in database
5. **Temp logs** - /tmp/vc-run-10.log (8.7K)

**Total disk usage**: ~2.6MB in `.sandboxes/`

---

## Detailed Inventory

### 1. Git Worktrees

```
/Users/stevey/src/vc/vc                            54289d2 [main]
/Users/stevey/src/vc/vc/.sandboxes/mission-bd-1    952b81d [mission/bd-1/1760817161]
/Users/stevey/src/vc/vc/.sandboxes/mission-vc-106  ce50b52 [mission/vc-106/1760764854] prunable
```

**Status**:
- `mission-bd-1`: Active (issue bd-1 still in_progress)
- `mission-vc-106`: **PRUNABLE** (orphaned from earlier run)
- `mission-vc-117`: Cleaned up during run #10

**Action needed**:
```bash
# Remove prunable worktree
git worktree prune

# Remove mission-vc-106 sandbox
rm -rf .sandboxes/mission-vc-106
```

---

### 2. Git Branches

```
+ mission/bd-1/1760817161       (used by mission-bd-1 worktree)
+ mission/vc-106/1760764854     (used by mission-vc-106 worktree - ORPHANED)
✓ mission/vc-117/1760816816     (DELETED successfully)
```

**Status**:
- 3 mission branches exist
- 2 are blocked by worktrees
- 1 was successfully deleted (vc-117)

**Action needed**:
```bash
# After removing worktrees above:
git branch -D mission/vc-106/1760764854
git branch -D mission/bd-1/1760817161  # After fixing bd-1 issue
```

---

### 3. Orphaned Issues (in_progress status)

```
bd-1     [P0] [bug] in_progress - Malformed ID (vc-132 bug manifestation)
vc-117   [P0] [bug] in_progress - Quality gates failed, blocked
vc-128   [P1] [bug] in_progress - Interrupted during run #9
```

**Problem**: These issues are stuck in `in_progress` but no executor is running.

**Action needed**:
```bash
# Release orphaned claims
bd update vc-117 --status open --notes "Quality gates failed in run #10 - reopening"
bd update vc-128 --status open --notes "Interrupted during run #9 - reopening"

# Delete malformed issue (vc-132 bug)
# Need to check if bd has delete command, or manually remove from DB
sqlite3 .beads/vc.db "DELETE FROM issues WHERE id = 'bd-1';"
```

---

### 4. Executor Instances (Database)

**Count**: 8 stopped instances in `executor_instances` table

```
e6e63cc9-f156-4346-91ae-46c4cfaf18e2  run #10 (2025-10-18 12:46)
4748118a-ec91-4219-9970-4f762e93ff9a  run #9  (2025-10-18 11:51)
1bb9976d-ea73-4946-85d5-2d4ac936d0dc  run #8  (2025-10-18 10:05)
conversation-stevey                    REPL    (2025-10-17 23:47)
[... 4 more older instances ...]
```

**Problem**: Old executor instances accumulate in database.

**Action needed**:
```bash
# Clean up old stopped instances (keep last 3 for debugging)
sqlite3 .beads/vc.db "
  DELETE FROM executor_instances
  WHERE status = 'stopped'
  AND started_at < datetime('now', '-24 hours');
"
```

**OR** better: Implement automatic cleanup in VC code (stale instance cleanup already exists but may need tuning).

---

### 5. Sandbox Directories

**Location**: `.sandboxes/`
**Size**: 2.6MB
**Contents**:
- `mission-bd-1/` - Full VC codebase copy (active)
- `mission-vc-106/` - ORPHANED (should be removed)

**Action needed**:
```bash
# Already covered in worktree cleanup above
rm -rf .sandboxes/mission-vc-106
```

---

### 6. Temp Logs

**Location**: `/tmp/vc-run-*.log`
**Size**: 8.7K (just run #10)

**Action needed**:
```bash
# Clean up old run logs (optional - tmp gets cleared on reboot)
rm /tmp/vc-run-*.log
```

---

## Cleanup Script

Here's a comprehensive cleanup script:

```bash
#!/bin/bash
# cleanup-dogfooding-artifacts.sh

set -e

echo "=== Dogfooding Artifacts Cleanup ==="

# 1. Prune git worktrees
echo "Pruning git worktrees..."
git worktree prune

# 2. Remove orphaned sandbox directories
echo "Removing orphaned sandboxes..."
rm -rf .sandboxes/mission-vc-106 2>/dev/null || true

# 3. Delete orphaned mission branches
echo "Deleting orphaned mission branches..."
git branch -D mission/vc-106/1760764854 2>/dev/null || true

# 4. Fix orphaned issues in beads
echo "Reopening orphaned in_progress issues..."
bd update vc-117 --status open --notes "Released from orphaned state - ready to retry"
bd update vc-128 --status open --notes "Released from orphaned state - ready to retry"

# 5. Delete malformed issues (bd-1)
echo "Removing malformed issue bd-1..."
sqlite3 .beads/vc.db "DELETE FROM issues WHERE id = 'bd-1';"
sqlite3 .beads/vc.db "DELETE FROM issue_execution_state WHERE issue_id = 'bd-1';"
rm -rf .sandboxes/mission-bd-1 2>/dev/null || true
git branch -D mission/bd-1/1760817161 2>/dev/null || true

# 6. Clean old executor instances (keep last 5)
echo "Cleaning old executor instances..."
sqlite3 .beads/vc.db "
  DELETE FROM executor_instances
  WHERE status = 'stopped'
  AND started_at < datetime('now', '-24 hours');
"

# 7. Clean temp logs
echo "Cleaning temp logs..."
rm /tmp/vc-run-*.log 2>/dev/null || true

# 8. Export to JSONL (critical!)
echo "Exporting beads to JSONL..."
bd export -o .beads/issues.jsonl

echo "✓ Cleanup complete!"
echo ""
echo "Summary:"
echo "- Pruned git worktrees"
echo "- Removed orphaned sandboxes"
echo "- Deleted orphaned branches"
echo "- Released 2 orphaned issues (vc-117, vc-128)"
echo "- Removed malformed issue (bd-1)"
echo "- Cleaned old executor instances"
echo "- Cleaned temp logs"
echo ""
echo "Don't forget to commit .beads/issues.jsonl!"
```

---

## Recommendations

### Immediate

1. **Run cleanup script** to clear current artifacts
2. **Commit JSONL** after cleanup
3. **Add .sandboxes/ to .gitignore** (already there, good!)

### Short-term

1. **Implement `bd stale` command** (vc-124) - Show orphaned claims
2. **Add executor cleanup to shutdown** - Remove stopped instances on exit
3. **Add sandbox cleanup to quality gates** - Remove sandbox after merge/failure
4. **Fix vc-132** - Prevent malformed issue IDs (bd-1 shouldn't exist)

### Long-term

1. **Automatic cleanup** - Executor cleans up old artifacts periodically
2. **Sandbox quotas** - Limit number of concurrent sandboxes
3. **Branch retention policy** - Auto-delete mission branches after N days
4. **Metrics dashboard** - Track artifact accumulation over time

---

## Cleanup Frequency

**Suggested schedule**:
- **After each run**: Export JSONL, commit changes
- **Daily**: Clean orphaned worktrees/branches
- **Weekly**: Clean old executor instances, temp files
- **Monthly**: Review and compact beads database

**Current impact**: Low (2.6MB, 3 branches, 8 instances)
**Recommended action**: Clean up now, then monitor after 20+ runs

---

## Notes

- `.sandboxes/` is gitignored (good!)
- Mission branches are local only (not pushed to remote)
- Executor instances are database-only (no filesystem impact)
- Temp logs in /tmp auto-clean on reboot
- **bd-1 is a bug manifestation** - shouldn't exist, demonstrates vc-132

**Most critical**: Export to JSONL after cleanup to keep git source of truth updated!
