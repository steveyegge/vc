# Linting in VC

## Overview

VC uses `golangci-lint` to maintain code quality. This document explains our linting strategy and how to work with lint errors.

## Current Status

As of 2025-10-24, we have **36 lint issues** to address:
- **20** unparam (unused parameters)
- **12** staticcheck (code quality improvements)
- **3** misspell (cancelled → canceled)
- **1** ineffassign (ineffectual assignment)

## Strategy

We're taking an **incremental approach** similar to Beads:

1. **Conservative initial configuration** - Most linters disabled to avoid overwhelming noise
2. **Fix as we go** - Address lint issues opportunistically when working on code
3. **Enable linters gradually** - Turn on more linters as the codebase improves

## Running the Linter

```bash
# Run all linters
golangci-lint run

# Run specific linters
golangci-lint run --disable-all -E misspell

# Auto-fix some issues
golangci-lint run --fix
```

## Enabled Linters

Currently enabled (in `.golangci.yml`):
- **misspell** - Catch spelling mistakes (e.g., "cancelled" → "canceled")
- **unconvert** - Remove unnecessary type conversions
- **unparam** - Detect unused function parameters

## Disabled Linters

Temporarily disabled (will enable gradually):
- **dupl** - Duplicate code detection (threshold: 100)
- **errcheck** - Unchecked errors (too noisy initially)
- **goconst** - Repeated strings that could be constants
- **gosec** - Security issues (many false positives)
- **revive** - General code style (too opinionated initially)
- **gocyclo** - Cyclomatic complexity (acceptable during bootstrap)

## Common Issues

### Misspelling: cancelled vs canceled

**Issue:** US English uses "canceled" (one 'l'), but British English uses "cancelled"

**Fix:** Use "canceled" consistently
```go
// Before
// context is being cancelled
fmt.Errorf("context cancelled")

// After
// context is being canceled
fmt.Errorf("context canceled")
```

### Unparam: Unused Parameters

**Issue:** Function parameters that are never used

**Options:**
1. Remove if truly unused
2. Use underscore prefix if required by interface: `_paramName`
3. Add a comment explaining why it's there for future use

```go
// Before
func process(ctx context.Context, id string, unused string) error {
    return doWork(ctx, id)
}

// Option 1: Remove
func process(ctx context.Context, id string) error {
    return doWork(ctx, id)
}

// Option 2: Interface requirement
func process(ctx context.Context, id string, _reserved string) error {
    return doWork(ctx, id)
}
```

### Staticcheck Issues

Various code quality improvements:
- Use tagged switch instead of if-else chains
- Simplify boolean logic (De Morgan's law)
- Remove unnecessary `fmt.Sprintf()` calls
- Remove unnecessary nil checks before `len()`
- Use `time.Time.Equal()` for time comparisons

## Philosophy

Our linting philosophy mirrors Beads:

1. **Code quality > perfection** - We prefer working code to perfectly linted code
2. **Incremental improvement** - Fix issues when touching code, don't stop everything for linting
3. **Pragmatic exclusions** - Some lint rules are too strict; we exclude patterns that make sense
4. **Developer experience** - Linting should help, not hinder development

## Adding Exclusions

If a lint rule produces false positives or is too strict, add an exclusion to `.golangci.yml`:

```yaml
issues:
  exclude-rules:
    - path: _test\.go
      linters:
        - gosec  # Security checks too strict for tests
    - text: "G201: SQL string formatting"  # We use prepared statements
```

## Future Work

As we clean up existing issues, we'll gradually enable more linters:
1. Enable `errcheck` after adding proper error handling
2. Enable `goconst` after identifying repeated strings
3. Enable `gocyclo` after refactoring complex functions
4. Consider enabling `revive` with custom rules

## References

- [golangci-lint documentation](https://golangci-lint.run/)
- [Effective Go](https://golang.org/doc/effective_go.html)
- Beads LINTING.md for similar approach
