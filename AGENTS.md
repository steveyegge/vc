# Parallel Agent Instructions

This file contains instructions for multiple agents working in parallel on the VC codebase.

**DO NOT work on issues assigned to other agents** - this will cause merge conflicts.

## Agent A: Validation Fixes

**Your Issues:**
- vc-30 (P1): Add limit validation to GetEvents in PostgreSQL backend
- vc-38 (P2): Add empty ID validation in AddDependency

**Files you will modify:**
- `internal/storage/postgres/postgres.go`

**Your tasks:**

### Task 1: vc-30 - Add limit validation to GetEvents

Location: `postgres.go` lines ~1014-1055 (GetEvents function)

**Problem:** GetEvents accepts a limit parameter but doesn't validate it. Negative or excessively large limits could cause DoS issues.

**Fix:**
1. Add validation at the start of GetEvents function
2. Reject negative limits with clear error
3. Cap maximum limit to reasonable value (e.g., 10000 events)
4. Document the limit behavior

**Acceptance:**
- Negative limits return error
- Limits > 10000 are capped or rejected
- Zero limit means "no limit" (all events)
- Error messages are clear
- Code compiles

### Task 2: vc-38 - Add empty ID validation in AddDependency

Location: `postgres.go` lines ~499-588 (AddDependency function)

**Problem:** AddDependency doesn't validate that dep.IssueID and dep.DependsOnID are non-empty before database operations.

**Fix:**
1. Add validation after self-dependency check (around line 501)
2. Check both IssueID and DependsOnID are non-empty
3. Return clear error if either is empty
4. Validation should occur before any database operations

**Acceptance:**
- Empty string validation for both IssueID and DependsOnID
- Clear error message if either is empty
- Validation before database operations
- Code compiles

**Workflow:**
1. Mark vc-30 as in_progress: `bd update vc-30 --status in_progress`
2. Complete vc-30 implementation
3. Build and verify: `go build ./...`
4. Close vc-30: `bd close vc-30 --reason "Your reason"`
5. Mark vc-38 as in_progress: `bd update vc-38 --status in_progress`
6. Complete vc-38 implementation
7. Build and verify: `go build ./...`
8. Close vc-38: `bd close vc-38 --reason "Your reason"`
9. Export: `bd export -o .beads/issues.jsonl`
10. Commit with message: "fix: add validation to GetEvents and AddDependency (vc-30, vc-38)"

---

## Agent B: Label/Event Operations and Error Handling

**Your Issues:**
- vc-27 (P1): Fix AddLabel/RemoveLabel to check RowsAffected before recording events
- vc-33 (P2): Add rows.Err() checks after iteration in PostgreSQL queries

**Files you will modify:**
- `internal/storage/postgres/postgres.go`

**Your tasks:**

### Task 1: vc-27 - Fix AddLabel/RemoveLabel RowsAffected checks

Locations:
- `postgres.go` lines ~884-909 (AddLabel function)
- `postgres.go` lines ~912-935 (RemoveLabel function)

**Problem:** AddLabel and RemoveLabel always record events, even when the operation is a no-op (label already exists, or label doesn't exist to remove). The INSERT has ON CONFLICT DO NOTHING, and DELETE might affect 0 rows.

**Fix:**
1. Check RowsAffected() after INSERT/DELETE operations
2. Only record event if rows were actually affected
3. Still commit transaction (not an error), just skip event recording
4. Functions return success even if no-op (idempotent behavior)

**Acceptance:**
- Check RowsAffected() after label INSERT/DELETE
- Only record event if RowsAffected() > 0
- Transaction still commits successfully
- Idempotent behavior maintained
- Code compiles

### Task 2: vc-33 - Add rows.Err() checks after iteration

**Problem:** Multiple functions iterate over pgx.Rows but don't check rows.Err() after the loop. This could hide errors that occur during iteration.

**Locations to fix:**
- GetDependencies (~lines 618-634)
- GetDependents (~lines 637-653)
- GetDependencyTree (~lines 656-725)
- DetectCycles (~lines 728-881)
- GetLabels (~lines 938-957)
- GetIssuesByLabel (~lines 960-976)
- GetEvents (~lines 1014-1055)
- scanIssues helper (~lines 1102-1130)

**Fix pattern:**
```go
for rows.Next() {
    // ... scan logic
}

// Add this after the loop:
if err := rows.Err(); err != nil {
    return nil, fmt.Errorf("error iterating rows: %w", err)
}

return results, nil
```

**Acceptance:**
- All functions that iterate over rows check rows.Err()
- Error message includes context about which operation failed
- Code compiles

**Workflow:**
1. Mark vc-27 as in_progress: `bd update vc-27 --status in_progress`
2. Complete vc-27 implementation
3. Build and verify: `go build ./...`
4. Close vc-27: `bd close vc-27 --reason "Your reason"`
5. Mark vc-33 as in_progress: `bd update vc-33 --status in_progress`
6. Complete vc-33 implementation (this will touch many functions)
7. Build and verify: `go build ./...`
8. Close vc-33: `bd close vc-33 --reason "Your reason"`
9. Export: `bd export -o .beads/issues.jsonl`
10. Commit with message: "fix: improve error handling in label operations and row iteration (vc-27, vc-33)"

---

## Agent C: DetectCycles Improvements

**Your Issues:**
- vc-39 (P2): Add parameter limit protection in DetectCycles bulk query
- vc-41 (P3): Use deterministic ordering in DetectCycles for consistent results
- vc-40 (P3): Handle missing issues gracefully in DetectCycles

**Files you will modify:**
- `internal/storage/postgres/postgres.go`

**Your tasks:**

### Task 1: vc-39 - Add parameter limit protection in DetectCycles

Location: `postgres.go` lines ~728-881 (DetectCycles function), specifically the bulk query at lines ~810-837

**Problem:** The bulk WHERE IN query could exceed PostgreSQL's 65535 parameter limit in pathological cases with extremely large cycles.

**Fix:**
1. Define batch size constant (e.g., 1000 issues per query)
2. If issueIDList length <= batch size, use current single-query approach
3. If issueIDList length > batch size, split into batches:
   - Process batches of up to 1000 IDs each
   - Execute query for each batch
   - Merge results into issueMap
4. Continue with existing cycle assembly logic

**Acceptance:**
- Batch size constant added (1000 issues)
- Batching logic when issue count exceeds limit
- Single query optimization still used for small result sets
- All issues fetched and merged correctly
- Code compiles

### Task 2: vc-41 - Use deterministic ordering in DetectCycles

Location: `postgres.go` lines ~812-817 (building issueIDList from map)

**Problem:** Building issueIDList from map using range iteration has non-deterministic order in Go. Makes debugging harder and test results non-deterministic.

**Fix:**
1. Import "sort" package if not already imported
2. After building issueIDList from map, sort it: `sort.Strings(issueIDList)`
3. This ensures consistent parameter order in SQL queries

**Acceptance:**
- Import sort package
- Sort issueIDList after building from map
- SQL queries use consistent parameter order
- Tests produce deterministic results
- Code compiles

### Task 3: vc-40 - Handle missing issues gracefully in DetectCycles

Location: `postgres.go` lines ~866-879 (cycle assembly loop)

**Problem:** Currently silently skips issues that are in cycle path but can't be fetched from database. This could hide data integrity issues.

**Fix:**
1. When issue is not found in issueMap (line ~871), log a warning
2. You'll need to add logging infrastructure or use fmt.Fprintf to stderr
3. Include issue ID and cycle path in the warning
4. Continue processing (don't break existing functionality)

**Acceptance:**
- Add logging/warning for missing issues in cycle paths
- Log includes issue ID and full cycle path
- Function continues processing other cycles
- Does not break existing functionality
- Code compiles

**Workflow:**
1. Mark vc-39 as in_progress: `bd update vc-39 --status in_progress`
2. Complete vc-39 implementation
3. Build and verify: `go build ./...`
4. Close vc-39: `bd close vc-39 --reason "Your reason"`
5. Mark vc-41 as in_progress: `bd update vc-41 --status in_progress`
6. Complete vc-41 implementation
7. Build and verify: `go build ./...`
8. Close vc-41: `bd close vc-41 --reason "Your reason"`
9. Mark vc-40 as in_progress: `bd update vc-40 --status in_progress`
10. Complete vc-40 implementation
11. Build and verify: `go build ./...`
12. Close vc-40: `bd close vc-40 --reason "Your reason"`
13. Export: `bd export -o .beads/issues.jsonl`
14. Commit with message: "feat: improve DetectCycles robustness and determinism (vc-39, vc-41, vc-40)"

---

## Important Notes for All Agents

### Beads Command Location
- **Local Mac**: `~/src/beads/bd`
- **GCE VM**: `/workspace/beads/bd`
- If `bd` is not in your PATH, use the full path

### Build and Test
Always build after each change:
```bash
go build ./...
```

### Git Workflow
**DO NOT push until instructed by the user.** The user will coordinate merging all agent work.

Just commit locally:
```bash
git add -A
git commit -m "your message"
```

### File Conflicts
All agents are working on `internal/storage/postgres/postgres.go`. The user will handle merge conflicts. Focus on your assigned functions only.

### Issue Status
- Start: `bd update <issue-id> --status in_progress`
- Finish: `bd close <issue-id> --reason "description of what you did"`
- Export: `bd export -o .beads/issues.jsonl`

### Questions
If you're blocked or have questions about your tasks, stop and ask the user. Don't make assumptions about requirements.

---

## Summary

**Agent A:** Validation (GetEvents limit, AddDependency empty IDs)
**Agent B:** Error handling (AddLabel/RemoveLabel RowsAffected, rows.Err() checks)
**Agent C:** DetectCycles improvements (batching, determinism, logging)

Each agent works on different functions in postgres.go to minimize conflicts. Good luck!
