# Circular Dependencies Analysis

## Executive Summary

The VC issue tracker system has **inconsistent handling of circular dependencies**. Cycle prevention only applies to "blocks" type dependencies, while cycle detection checks all types. This allows cross-type circular dependencies (like the vc-5↔vc-13 case) to exist in the system.

## Current Behavior

### Cycle Prevention (AddDependency)

**Location**: `internal/storage/postgres/postgres.go:559-599`

Cycle prevention is **type-specific**:
```go
if dep.Type == types.DepBlocks {
    // Check if adding this dependency creates a cycle
    // Only traverses "blocks" type dependencies
}
```

**What this means**:
- "blocks" type dependencies cannot form cycles within themselves
- "parent-child" and "discovered-from" dependencies are NOT checked for cycles
- **Cross-type cycles are not prevented** (e.g., A blocks B, B parent-child A)

### Cycle Detection (DetectCycles)

**Location**: `internal/storage/postgres/postgres.go:758-841`

Cycle detection is **type-agnostic**:
```sql
WITH RECURSIVE paths AS (
    SELECT issue_id, depends_on_id, issue_id as start_id, ...
    FROM dependencies  -- ALL types
    ...
)
```

**What this means**:
- `DetectCycles()` will find cycles across any combination of dependency types
- It's only used diagnostically (e.g., `bd dep cycles` command)
- It's not automatically run during dependency creation

## Operations Analysis

### ✅ SAFE Operations (Protected from infinite loops)

1. **GetDependencyTree** (`postgres.go:679-755`)
   - Uses recursive CTE with depth limit (default 50, max in query: 100)
   - `WHERE t.depth < $2` prevents infinite traversal
   - **Safe with circular dependencies**

2. **GetReadyWork** (`ready.go:14-75`)
   - Only checks direct blockers: `WHERE d.issue_id = i.id AND d.type = 'blocks'`
   - No transitive closure computation
   - **Safe with circular dependencies**

3. **GetDependencies/GetDependents** (`postgres.go:642-677`)
   - Only return direct dependencies (single JOIN, no recursion)
   - **Safe with circular dependencies**

4. **DetectCycles** (`postgres.go:758-841`)
   - Explicitly designed to handle cycles
   - Has depth limit: `WHERE p.depth < 100`
   - Has cycle detection: `WHERE depends_on_id = start_id`
   - **Safe by design**

### ⚠️ SEMANTIC Issues (Not technical failures)

1. **Conceptual Confusion**
   - `vc-13` (child task) depends on `vc-5` (parent epic) via "parent-child" type
   - This is semantically backwards: children shouldn't depend on their parents
   - The dependency direction should be: parent depends on children completing

2. **Dependency Visualization**
   - `bd dep tree` could show confusing cycles
   - Tools that render dependency graphs may struggle with circular references

3. **Future Operations Risk**
   - Any new code that recursively traverses ALL dependency types without depth limits could loop infinitely
   - Currently no such operations exist, but it's a maintenance hazard

## The vc-5 ↔ vc-13 Case

### What Happened

```
vc-13 (epic subtask)
  └─ depends on vc-5 (parent epic) via "parent-child" type

vc-5 (parent epic)
  └─ blocks vc-13 (epic subtask) via "blocks" type
```

### Why It Wasn't Prevented

1. When `vc-13 → vc-5` ("parent-child") was added:
   - No cycle check performed (only "blocks" type is checked)

2. When `vc-5 → vc-13` ("blocks") was added:
   - Cycle check ran, but only traversed "blocks" type dependencies
   - Didn't see the "parent-child" path back from vc-13 to vc-5
   - Therefore, didn't detect the cycle

### Why It Didn't Block Progress

- Both issues were closed based on their child tasks completing
- Ready work calculation only checks "blocks" type dependencies
- No operations performed transitive closure across all dependency types

## Recommendations

### Priority 1: Semantic Validation

**Problem**: Child tasks shouldn't depend on parent epics.

**Solution**: Add validation in `AddDependency`:
```go
// For "parent-child" type, validate direction
if dep.Type == types.DepParentChild {
    // Check if DependsOnID is actually the parent of IssueID
    // Prevent child→parent dependencies (backwards)
}
```

### Priority 2: Cross-Type Cycle Prevention

**Problem**: Cycles can form across different dependency types.

**Solution**: Expand cycle check to cover all types:
```go
// Option A: Check for cycles across ALL dependency types
var cycleExists bool
err = tx.QueryRow(ctx, `
    WITH RECURSIVE paths AS (
        SELECT issue_id, depends_on_id, 1 as depth
        FROM dependencies
        WHERE issue_id = $1
        -- NO type filter here

        UNION ALL

        SELECT d.issue_id, d.depends_on_id, p.depth + 1
        FROM dependencies d
        JOIN paths p ON d.issue_id = p.depends_on_id
        -- NO type filter here
        WHERE p.depth < 100
    )
    SELECT EXISTS(SELECT 1 FROM paths WHERE depends_on_id = $2)
`, dep.DependsOnID, dep.IssueID).Scan(&cycleExists)

if cycleExists {
    return fmt.Errorf("cannot add dependency: would create a cycle (type: %s)", dep.Type)
}
```

### Priority 3: Diagnostic Warnings

**Problem**: Users aren't notified about cycles across different types.

**Solution**: Make `bd dep add` run `DetectCycles()` after adding and warn if cycles exist:
```go
// After adding dependency, check for cycles
cycles, err := store.DetectCycles(ctx)
if err != nil {
    return fmt.Errorf("failed to check for cycles: %w", err)
}

if len(cycles) > 0 {
    fmt.Fprintf(os.Stderr, "WARNING: Circular dependency detected:\n")
    for _, cycle := range cycles {
        fmt.Fprintf(os.Stderr, "  %s\n", formatCycle(cycle))
    }
    fmt.Fprintf(os.Stderr, "This may cause confusion in dependency visualization.\n")
}
```

### Priority 4: Documentation

**Problem**: The current behavior isn't documented.

**Solution**: Add comments to `AddDependency` explaining:
- Which dependency types are checked for cycles
- Why cross-type cycles are possible
- What operations are safe vs risky

## Decision Matrix

| Approach | Pros | Cons | Effort |
|----------|------|------|--------|
| **Status Quo** | No code changes needed | Semantic confusion continues | None |
| **Semantic Validation Only** | Prevents obvious mistakes (child→parent) | Doesn't address cross-type cycles | Low |
| **Full Cycle Prevention** | Mathematically correct, prevents all cycles | May break legitimate use cases? | Medium |
| **Detection + Warning** | Alerts users without blocking | Doesn't prevent the issue | Low |
| **All Three** | Comprehensive solution | Most code changes | Medium |

## Recommended Path Forward

1. **Immediate**: Add semantic validation for "parent-child" dependencies (prevent child→parent)
2. **Short-term**: Add warning in `bd dep add` when cycles are detected
3. **Medium-term**: Evaluate whether full cross-type cycle prevention is needed based on usage patterns
4. **Long-term**: Add depth-limit protection to any future recursive dependency traversal

## Open Questions

1. **Are cross-type cycles ever legitimate?**
   - Example: A blocks B, B discovered-from A (A spawned B, but B blocks A)
   - This could be valid in some workflows

2. **Should cycle prevention be configurable?**
   - Add a `--allow-cycle` flag to `bd dep add`?
   - Store a flag in the dependency to mark intentional cycles?

3. **What about cycles involving more than 2 issues?**
   - A blocks B, B parent-child C, C discovered-from A
   - Current system would allow this

4. **Performance impact of full cycle checking?**
   - Recursive CTEs are efficient, but checking all types adds overhead
   - Benchmark needed for large dependency graphs (1000+ issues)
