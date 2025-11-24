# VC Planning & Acceptance Criteria Guide

## Table of Contents
- [WHEN...THEN... Acceptance Criteria Format](#whenthen-acceptance-criteria-format)
- [Why Scenario-Based Criteria?](#why-scenario-based-criteria)
- [Writing Good Acceptance Criteria](#writing-good-acceptance-criteria)
- [Validation](#validation)
- [Examples](#examples)

---

## WHEN...THEN... Acceptance Criteria Format

VC uses **scenario-based acceptance criteria** with the WHEN...THEN... format for all issues, tasks, and phases. This format provides concrete, testable requirements that both AI agents and humans can verify.

### Format Structure

```
WHEN [triggering condition] THEN [observable outcome]
```

Each acceptance criterion should specify:
1. **WHEN**: A triggering condition, input, or scenario
2. **THEN**: An observable outcome, behavior, or result
3. **Specificity**: Concrete, measurable behavior (not vague goals)

---

## Why Scenario-Based Criteria?

Traditional acceptance criteria are often vague and hard to verify:
- ❌ "Test storage layer thoroughly"
- ❌ "Handle errors properly"
- ❌ "Make it robust"
- ❌ "Add good test coverage"

**Problems with vague criteria:**
- Agents don't know when they're done
- Humans can't verify completion objectively
- Edge cases are missed
- Tests are unclear

**Benefits of WHEN...THEN... format:**
- ✅ **Concrete**: Clear pass/fail conditions
- ✅ **Testable**: Can directly generate test cases
- ✅ **Complete**: Forces thinking about edge cases
- ✅ **Verifiable**: Both AI and humans can check
- ✅ **Actionable**: Obvious what behavior to implement

---

## Writing Good Acceptance Criteria

### DO: Be Specific and Measurable

✅ **Good Examples:**
```
WHEN creating an issue THEN it persists to SQLite database
WHEN reading non-existent issue THEN NotFoundError is returned
WHEN transaction fails THEN retry 3 times with exponential backoff
WHEN executor shuts down gracefully THEN all in-progress work is checkpointed
WHEN plan validation detects circular dependencies THEN it rejects the plan with clear error
WHEN test suite runs THEN all tests pass in under 5 seconds
WHEN function receives nil input THEN it returns ErrInvalidInput
WHEN convergence detected (diff < 5%) THEN CheckConvergence returns true
```

### DON'T: Be Vague or Ambiguous

❌ **Bad Examples:**
```
Test storage thoroughly
Handle errors properly
Make it robust
Add good test coverage
Improve performance
Make it production-ready
```

### Covering Edge Cases

Use multiple WHEN...THEN... criteria to cover different scenarios:

```
WHEN valid input provided THEN processing succeeds
WHEN empty input provided THEN ValidationError is returned
WHEN nil input provided THEN ErrInvalidInput is returned
WHEN timeout occurs THEN operation is cancelled gracefully
WHEN context cancelled THEN resources are cleaned up
```

### Performance and Quality Criteria

Include specific thresholds for non-functional requirements:

```
WHEN test suite runs THEN execution completes in under 5 seconds
WHEN processing 1000 issues THEN memory usage stays under 100MB
WHEN API rate limit hit THEN exponential backoff is applied
WHEN linter runs THEN no errors or warnings are reported
```

---

## Validation

VC automatically validates acceptance criteria format during plan validation.

### Validator Behavior

The `AcceptanceCriteriaValidator` checks all task acceptance criteria:

**Errors (blocking):**
- Missing acceptance criteria entirely
- Empty acceptance criteria array

**Warnings (non-blocking):**
- Criteria missing "WHEN" keyword
- Criteria missing "THEN" keyword
- Vague or ambiguous criteria

### Case-Insensitive Matching

The validator accepts any case variation:
```
WHEN ... THEN ...  ✓
when ... then ...  ✓
When ... Then ...  ✓
```

### Running Validation

Validation runs automatically during plan approval:
```bash
vc plan approve vc-123
```

To validate a plan without approving:
```bash
vc plan validate vc-123
```

---

## Examples

### Example 1: Storage Layer Task

**Vague (Bad):**
```
Title: Test storage layer thoroughly
AC: Make sure it works properly
```

**Specific (Good):**
```
Title: Add comprehensive storage layer tests
AC:
- WHEN creating an issue THEN it persists to SQLite database
- WHEN reading an existing issue THEN correct data is returned
- WHEN reading non-existent issue THEN NotFoundError is returned
- WHEN updating issue status THEN status_changed event is recorded
- WHEN deleting an issue THEN it's removed from database
- WHEN concurrent updates occur THEN transactions prevent race conditions
```

### Example 2: Error Handling Task

**Vague (Bad):**
```
Title: Add error handling
AC: Handle errors properly
```

**Specific (Good):**
```
Title: Implement robust error handling for API calls
AC:
- WHEN API returns 429 rate limit THEN exponential backoff is applied
- WHEN API returns 500 error THEN retry 3 times with backoff
- WHEN API returns 401 error THEN no retry (auth error)
- WHEN network timeout occurs THEN operation fails with TimeoutError
- WHEN context cancelled THEN operation stops immediately
```

### Example 3: Validation Task

**Vague (Bad):**
```
Title: Validate plan structure
AC: Make sure plans are valid
```

**Specific (Good):**
```
Title: Implement plan structure validation
AC:
- WHEN plan has circular dependencies THEN validation rejects with clear error
- WHEN phase references non-existent dependency THEN validation fails
- WHEN plan has no phases THEN validation fails
- WHEN plan has >20 phases THEN warning is issued
- WHEN all validations pass THEN plan status is set to 'validated'
```

### Example 4: Performance Task

**Vague (Bad):**
```
Title: Optimize query performance
AC: Make it faster
```

**Specific (Good):**
```
Title: Optimize GetReadyWork query performance
AC:
- WHEN querying 10,000 issues THEN response time is under 100ms
- WHEN query includes dependency check THEN uses efficient SQL join
- WHEN database has indexes THEN query plan uses index scan (not full table)
- WHEN benchmarked THEN performance is 10x faster than baseline
```

---

## Transformation Examples

Here are before/after examples showing how to transform vague criteria:

### Storage Layer
```
BEFORE: Test storage layer thoroughly

AFTER:
- WHEN creating an issue THEN it persists to SQLite database
- WHEN reading a non-existent issue THEN NotFoundError is returned
- WHEN updating issue status THEN status_changed event is recorded
```

### Error Handling
```
BEFORE: Handle errors properly

AFTER:
- WHEN API rate limit hit THEN exponential backoff is applied
- WHEN network error occurs THEN operation retries with timeout
- WHEN unrecoverable error occurs THEN user gets clear error message
```

### Test Coverage
```
BEFORE: Add good test coverage

AFTER:
- WHEN test suite runs THEN coverage is >80% on new code
- WHEN edge cases tested THEN nil, empty, and invalid inputs covered
- WHEN tests run THEN execution completes in under 5 seconds
```

### Robustness
```
BEFORE: Make it robust

AFTER:
- WHEN invalid input provided THEN validation error returned
- WHEN nil pointer encountered THEN panic is prevented
- WHEN concurrent access occurs THEN race conditions are prevented
- WHEN context cancelled THEN resources are cleaned up
```

---

## CLI Tools

### Enhancing Existing Issues (Future)

The `vc issue enhance-ac` command (when implemented) will use AI to transform vague acceptance criteria into WHEN...THEN... format:

```bash
# Enhance acceptance criteria for an issue
vc issue enhance-ac vc-123

# Preview changes without applying
vc issue enhance-ac vc-123 --dry-run
```

---

## Best Practices

1. **Start with WHEN...THEN...** when creating new issues
2. **Cover edge cases** with multiple criteria
3. **Be specific** about expected behavior
4. **Include thresholds** for performance/quality criteria
5. **Make it testable** - could you write a test directly from this?
6. **Think about failure modes** - what can go wrong?
7. **Avoid ambiguity** - no "properly", "thoroughly", "robust"

---

## See Also

- [FEATURES.md](FEATURES.md) - Deep dives on specific VC features
- [CONFIGURATION.md](CONFIGURATION.md) - Environment variables and tuning
- [QUERIES.md](QUERIES.md) - SQL queries for metrics and monitoring
