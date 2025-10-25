# Interface Change Checklist

## Problem Statement

When updating interface definitions (like `storage.Storage`), it's easy to miss test files that contain mock implementations. During vc-225, the affected files list included:
- `internal/mission/orchestrator_rollback_test.go`
- `internal/mission/orchestrator_test.go`
- `internal/ai/supervisor_test.go`

But missed:
- `internal/repl/conversation_test.go`
- `internal/watchdog/analyzer_test.go`

This document provides a systematic approach to finding ALL affected files.

## Solution: Systematic Discovery

### Step 1: Find All Mock Implementations

Use the provided script to find all test files with mockStorage:

```bash
./scripts/find-storage-mocks.sh
```

This script searches for `type.*mockStorage.*struct` patterns in all `*_test.go` files.

### Step 2: Verify Compilation

Check if all mock implementations compile:

```bash
go test -c ./internal/ai/supervisor_test.go
go test -c ./internal/repl/conversation_test.go
go test -c ./internal/repl/conversation_integration_test.go
go test -c ./internal/watchdog/analyzer_test.go
```

Or use the automated check:

```bash
# Find all mocks and compile them
for file in $(find . -name "*_test.go" -exec grep -l "type.*mockStorage.*struct" {} \;); do
    echo "Compiling $file..."
    go test -c "$file" 2>&1 | grep -E "missing method|does not implement" || echo "  ✓ OK"
done
```

### Step 3: Update Each Mock

When adding a method to `storage.Storage`, add stub implementations to ALL mocks:

```go
func (m *mockStorage) NewMethod(ctx context.Context, param string) (result, error) {
    return defaultValue, nil
}
```

For methods that return pointers to structs, use empty struct initialization:

```go
func (m *mockStorage) GetEventCounts(ctx context.Context) (*sqlite.EventCounts, error) {
    return &sqlite.EventCounts{}, nil
}
```

## Best Practices

### 1. Add Interface Documentation

Update `internal/storage/storage.go` with a comment listing all known mock locations:

```go
// Storage defines the interface for issue storage backends
//
// IMPORTANT: When adding methods to this interface, you MUST update ALL mock implementations.
// Run ./scripts/find-storage-mocks.sh to find all files that need updates.
```

### 2. Use Embedded Mocks Where Possible

If you have multiple mocks in the same package, consider embedding:

```go
type mockStorageIntegration struct {
    mockStorage  // Embeds all methods from base mock
    // Additional fields/methods specific to integration tests
}
```

This reduces duplication - when you update `mockStorage`, the embedded versions get the methods automatically.

### 3. Run Full Test Suite

After updating all mocks, run the full test suite:

```bash
go test ./...
```

### 4. Document in Commit Message

When updating an interface, list ALL affected files in the commit message:

```
fix: Update storage.Storage interface with new methods

Added:
- CleanupEventsByAge
- GetEventCounts
- VacuumDatabase

Updated mock implementations in:
- internal/ai/supervisor_test.go
- internal/repl/conversation_test.go
- internal/repl/conversation_integration_test.go
- internal/watchdog/analyzer_test.go
```

## Prevention: Static Analysis

Consider adding a pre-commit hook or CI check:

```bash
#!/bin/bash
# .git/hooks/pre-commit

# Find all mockStorage implementations
mock_files=$(find . -name "*_test.go" -exec grep -l "type.*mockStorage.*struct" {} \;)

# Try to compile each one
failed=0
for file in $mock_files; do
    if ! go test -c "$file" 2>&1 | grep -q "^ok"; then
        echo "ERROR: $file has compilation errors"
        failed=1
    fi
done

exit $failed
```

## Related Issues

- vc-225: Original issue that required storage interface updates
- vc-227: Additional mock storage methods needed beyond original scope
- vc-228: Additional test files needed mock updates beyond affected files list (this issue)

## Future Work

Consider:
1. Generating mock implementations automatically (using tools like `mockgen`)
2. Creating a shared test package with common mocks
3. Using interface composition to reduce duplication
