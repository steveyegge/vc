# Code Review: vc-129 Agent Tool Usage Event Parsing

**Reviewer:** Claude Code (self-review)
**Date:** 2025-10-21
**Commit:** 394f9f7

## Overview

Implementation adds real-time progress event capture for agent execution by detecting tool usage from agent output. The feature works but has **one critical bug** and several areas for improvement.

---

## 🔴 CRITICAL ISSUES

### 1. Database Schema Missing New Event Types

**Severity:** CRITICAL - Will cause runtime failures
**Location:** `internal/storage/sqlite/schema.go:157-170`

**Problem:**
The new event types are defined in `types.go` but **NOT included in the database CHECK constraint**. This means:
- Any attempt to store `agent_tool_use`, `agent_heartbeat`, or `agent_state_change` events will be **rejected by SQLite**
- The feature appears to work in tests (which don't use the real database) but will fail in production

**Evidence:**
```sql
-- schema.go line 157
CHECK(type IN (
    'file_modified', 'test_run', 'git_operation', ...,
    'deduplication_batch_started', 'deduplication_batch_completed', 'deduplication_decision',
    'event_cleanup_completed'
    -- Missing: 'agent_tool_use', 'agent_heartbeat', 'agent_state_change'
))
```

**Also affects:**
- `health_check_completed` (vc-205) - also missing from schema
- `health_check_failed` (vc-205) - also missing from schema

**Fix Required:**
Add missing event types to CHECK constraint in `schema.go`:
```sql
CHECK(type IN (
    ...,
    -- Agent progress events (vc-129)
    'agent_tool_use', 'agent_heartbeat', 'agent_state_change',
    -- Health monitoring events (vc-205)
    'health_check_completed', 'health_check_failed'
))
```

**Why Tests Passed:**
The `events` package tests don't use the actual storage layer - they just test parsing logic. Need integration tests that actually store events to catch this.

---

## 🟡 MAJOR ISSUES

### 2. Regex Compiled on Every Function Call

**Severity:** MAJOR - Performance issue
**Location:** `parser.go:605, 627`

**Problem:**
The helper functions `extractToolDescription()` and `extractFileName()` compile regex patterns **on every call**:

```go
func extractToolDescription(line, toolKeyword string) string {
    // COMPILED EVERY TIME THIS IS CALLED
    toPattern := regexp.MustCompile(`(?i)` + toolKeyword + `\s+tool.*?\s+to\s+(.+?)(?:\.|$)`)
    ...
}

func extractFileName(line string) string {
    // COMPILED EVERY TIME THIS IS CALLED
    filePattern := regexp.MustCompile(`\b([a-zA-Z0-9_\-./]+\.[a-z0-9]+)\b`)
    ...
}
```

**Impact:**
- Regex compilation is expensive (O(n) in pattern length)
- Called for every tool usage line parsed
- In high-volume scenarios (100+ events/second), this becomes a bottleneck

**Fix:**
Add these patterns to `eventPatterns` struct and compile once:
```go
type eventPatterns struct {
    ...
    // Helper patterns
    toolToPattern  *regexp.Regexp
    fileNamePattern *regexp.Regexp
}
```

---

### 3. Long If-Else Chain for Tool Detection

**Severity:** MAJOR - Code maintainability
**Location:** `parser.go:214-241`

**Problem:**
Tool detection uses a long if-else chain:
```go
if p.patterns.readTool.MatchString(line) {
    toolName = "Read"
    ...
} else if p.patterns.editTool.MatchString(line) {
    toolName = "Edit"
    ...
} else if p.patterns.writeTool.MatchString(line) {
    ...
```

**Issues:**
- Hard to maintain (8 tools currently, could grow)
- Each pattern is checked sequentially (not parallel)
- Adding new tools requires modifying this function

**Suggested Refactor:**
```go
type toolMatcher struct {
    pattern *regexp.Regexp
    name    string
}

var toolMatchers = []toolMatcher{
    {readTool, "Read"},
    {editTool, "Edit"},
    {writeTool, "Write"},
    ...
}

for _, matcher := range toolMatchers {
    if matcher.pattern.MatchString(line) {
        toolName = matcher.name
        break
    }
}
```

---

## 🟢 MINOR ISSUES

### 4. Silent Error Handling

**Severity:** MINOR - Observability
**Location:** `parser.go:258`

**Problem:**
Errors from `SetAgentToolUseData()` are silently ignored:
```go
_ = event.SetAgentToolUseData(AgentToolUseData{...})
```

**Why it matters:**
- If serialization fails, we lose data silently
- No logs, no metrics, no visibility

**Suggestion:**
Log errors (even if we can't fail the parse):
```go
if err := event.SetAgentToolUseData(data); err != nil {
    fmt.Fprintf(os.Stderr, "warning: failed to set tool use data: %v\n", err)
}
```

---

### 5. extractFileName() May Extract Wrong File

**Severity:** MINOR - Accuracy
**Location:** `parser.go:622-637`

**Problem:**
The function returns the **last** filename match, assuming it's the target:
```go
// Find all matches and return the last one (usually the target file)
matches := filePattern.FindAllStringSubmatch(line, -1)
if len(matches) > 0 {
    return matches[len(matches)-1][1]  // Last match
}
```

**Edge Case:**
```
"I'll use the Read tool to read config.yaml based on settings.json"
```
Returns `settings.json` (wrong) instead of `config.yaml` (correct).

**Suggestion:**
Return the **first** match after the tool name, not the last:
```go
// Find first filename after tool keyword in line
```

---

### 6. Inconsistent Tool Name in Patterns vs Data

**Severity:** MINOR - Code smell
**Location:** `parser.go:142-148`

**Problem:**
Pattern matching is case-insensitive (`(?i)`), but tool names are hardcoded as capitalized:
```go
readTool:  regexp.MustCompile(`(?i)...`),  // matches "read", "Read", "READ"
...
toolName = "Read"  // Always capitalized
```

**Question:**
What if the agent says "READ TOOL" or "read tool"? We'll still store as "Read", which is correct, but it's not explicit.

**Suggestion:**
Add comment explaining this is intentional:
```go
// Tool names are always capitalized for consistency, regardless of how
// they appear in agent output (case-insensitive matching)
toolName = "Read"
```

---

### 7. Missing Integration Test

**Severity:** MINOR - Testing gap
**Location:** `parser_test.go`

**Problem:**
Tests verify parsing logic but **don't test actual event storage**. This is why the schema bug went undetected.

**Gap:**
No test that:
1. Parses tool usage
2. Stores event to real database
3. Queries it back

**Suggestion:**
Add integration test in `executor` package:
```go
func TestAgentToolUseStorageIntegration(t *testing.T) {
    // Create real storage
    store := storage.NewStorage(...)

    // Parse and store tool use event
    parser := events.NewOutputParser(...)
    events := parser.ParseLine("Using Read tool to read config.yaml")

    for _, evt := range events {
        err := store.StoreAgentEvent(ctx, evt)
        require.NoError(t, err)  // Would fail with current schema!
    }

    // Query back
    retrieved := store.GetEvents(...)
    assert.Equal(t, "agent_tool_use", retrieved[0].Type)
}
```

---

## ✅ STRENGTHS

### What Was Done Well

1. **Comprehensive Test Coverage**
   - 15 test cases covering all tools
   - Edge cases tested (non-tool lines, false positives)
   - All tests passing (within scope)

2. **Clean Data Structures**
   - Type-safe helper methods (Set/Get)
   - Consistent with existing patterns (FileModifiedData, TestRunData, etc.)
   - Good field naming (tool_name, tool_description, target_file)

3. **Excellent Documentation**
   - 157 lines added to CLAUDE.md
   - SQL query examples
   - Clear explanation of what's implemented vs punted
   - Good "Why this matters" section

4. **Conservative Scope**
   - Correctly punted heartbeat/state change emission
   - Focused on core functionality first
   - Acknowledged future work

5. **Consistent Code Style**
   - Follows existing parser patterns
   - Good comments explaining regex patterns
   - Proper error handling in most places

---

## 🎯 RECOMMENDATIONS

### Immediate (Before Merge)

1. **FIX SCHEMA BUG** (Critical)
   - Add missing event types to CHECK constraint
   - Test with actual database insertion
   - Add integration test to prevent regression

2. **FIX REGEX PERFORMANCE** (Major)
   - Move helper regex patterns to eventPatterns struct
   - Compile once in compilePatterns()
   - Benchmark before/after

### Short Term (Follow-up Issue)

3. **Refactor Tool Detection**
   - Replace if-else chain with loop over matchers
   - Make it easier to add new tools

4. **Add Integration Tests**
   - Test actual event storage
   - Verify events can be queried back
   - Test with concurrent writes

5. **Improve File Extraction**
   - Return first match after tool name
   - Add tests for edge cases

### Long Term (Future)

6. **Structured Tool Output**
   - Instead of parsing natural language, have agents emit structured JSON
   - Example: `[TOOL:Read|file:config.yaml]`
   - More reliable than regex parsing

---

## 📊 RISK ASSESSMENT

**Current State:**
- ⚠️ **Cannot deploy** - Schema bug will cause runtime failures
- ⚠️ **Performance unknown** - Regex compilation on every call
- ✅ **Tests pass** - But only unit tests, not integration

**After Schema Fix:**
- ✅ **Can deploy** - Core functionality works
- ⚠️ **Performance acceptable** - May need optimization under load
- ✅ **Foundation solid** - Good basis for future work

---

## 🏁 VERDICT

**Overall Assessment:** Good implementation with one critical bug

**Code Quality:** B+ (would be A- after schema fix)

**Must Fix Before Merge:**
1. Add event types to database schema CHECK constraint
2. Add integration test that stores and retrieves events

**Should Fix Soon:**
3. Move regex compilation to eventPatterns struct
4. Add logging for SetAgentToolUseData errors

**Can Defer:**
5. Refactor tool detection if-else chain
6. Improve file extraction logic
7. Add structured tool output format

---

## 📝 ACTION ITEMS

- [ ] Fix schema.go CHECK constraint (CRITICAL)
- [ ] Add integration test for event storage (CRITICAL)
- [ ] Move regex patterns to eventPatterns (HIGH)
- [ ] Add error logging for SetAgentToolUseData (MEDIUM)
- [ ] File follow-up issue for refactoring (LOW)

**Estimated time to fix critical issues:** 30 minutes

---

**Signed:** Claude Code
**Review complete:** 2025-10-21 14:50 PST
