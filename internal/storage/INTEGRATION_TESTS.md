# Integration Test Coverage

This document describes the integration tests for executor functionality in `internal/storage/integration_test.go`.

## Overview

The integration tests validate the full executor table functionality, including multi-executor scenarios, claim/checkpoint/resume flows, and both SQLite and PostgreSQL database backends.

## Test Scenarios

### 1. TestMultiExecutorClaiming

**Purpose**: Verify that multiple executors can claim different issues concurrently without conflicts.

**What it tests**:
- 3 executors attempting to claim 10 issues concurrently
- No double-claiming occurs (each issue claimed by at most one executor)
- Claim counts match actual execution state
- Atomic claiming prevents race conditions

**Key assertions**:
- Total claimed count matches actual database state
- Each executor's reported claims match reality
- No issue has multiple execution states
- All claims are in the correct `claimed` state

### 2. TestRaceConditionPrevention

**Purpose**: Verify that the database prevents double-claiming when two executors attempt to claim the same issue simultaneously.

**What it tests**:
- 2 executors racing to claim a single issue
- Exactly one succeeds, one fails
- Database maintains consistency

**Key assertions**:
- Exactly 1 successful claim
- Exactly 1 failed claim
- Winner has valid execution state
- Atomic transaction prevents split-brain

### 3. TestCheckpointSaveAndRestore

**Purpose**: Validate checkpoint save and restore functionality for resumable execution.

**What it tests**:
- Saving complex checkpoint data (nested JSON)
- Retrieving checkpoint data
- Data integrity (all fields preserved correctly)

**Key assertions**:
- Checkpoint data round-trips correctly
- Nested structures (arrays, maps) preserved
- Metadata fields intact
- Step numbers and task lists accurate

### 4. TestStaleInstanceCleanup

**Purpose**: Test cleanup of stale executor instances based on heartbeat timeout.

**What it tests**:
- Creating instances with different heartbeat ages
- Cleanup threshold detection (5 minutes)
- Stale instances marked as stopped
- Fresh instances remain active

**Key assertions**:
- Correct number of instances cleaned up
- Only stale instances (>5 min old) are stopped
- Fresh instances remain running
- Active instance count accurate after cleanup

### 5. TestResumeAfterInterruption

**Purpose**: Validate that work can be resumed after executor crash/restart (end-to-end scenario).

**What it tests**:
- Executor claims issue and starts work
- Progress through multiple execution states
- Checkpoint saved mid-execution
- Executor becomes stale (simulated crash)
- Stale instance cleanup
- Checkpoint retrieval before release
- New executor claims released issue
- Work resumes from checkpoint
- Issue completed successfully

**Key assertions**:
- Checkpoint data survives executor transition
- Stale instance cleanup works correctly
- Released issue appears in ready work
- New executor can claim and complete work
- Previous progress (completed tasks) preserved
- State transitions work correctly for resumed work

### 6. TestCompleteExecutorWorkflow

**Purpose**: Test the complete executor workflow from claim to completion.

**What it tests**:
- Full state machine traversal
- All execution states in sequence:
  - claimed → assessing → executing → analyzing → gates → completed
- Checkpoint save at each state
- Execution state release
- State cleanup after completion

**Key assertions**:
- All state transitions valid
- States progress in correct order
- Checkpoints save successfully at each step
- Execution state properly cleared after release
- No orphaned execution state

## Backend Coverage

All tests run against both database backends:
- **SQLite**: Uses temporary file-based database for isolation
- **PostgreSQL**: Uses test database (skipped if not available)

PostgreSQL tests are automatically skipped if:
- Environment variables not set (`VC_PG_HOST`, `VC_PG_DATABASE`)
- Connection fails (2-second timeout)

## Test Helpers

### setupStorage(t, backend)
Creates isolated test storage for each backend.
- SQLite: Creates temporary file, auto-cleanup
- PostgreSQL: Connects to test database

### createExecutors(t, ctx, store, count)
Creates and registers N executor instances.
- Generates unique instance IDs
- Registers with running status
- Returns executor instances for test use

### createTestIssues(t, ctx, store, count)
Creates N test issues in open status.
- Generates unique titles and IDs
- Sets up for ready work claiming
- Returns issue objects for test use

### isPostgresAvailable()
Checks if PostgreSQL is available for testing.
- Tries to connect with defaults
- Returns false on timeout/error
- Enables conditional test execution

## Running Tests

```bash
# Run all integration tests
go test -v ./internal/storage/ -run 'Test(Multi|Race|Checkpoint|Stale|Resume|Complete)'

# Run with PostgreSQL (requires setup)
export VC_PG_HOST=localhost
export VC_PG_DATABASE=vc_test
export VC_PG_USER=vc
export VC_PG_PASSWORD=secret
go test -v ./internal/storage/ -run 'Test.*'

# Run specific test
go test -v ./internal/storage/ -run TestResumeAfterInterruption
```

## Coverage Summary

✅ Multi-executor claim scenarios (no conflicts)
✅ Race condition handling (no double-claiming)
✅ Checkpoint save and restore
✅ Stale instance cleanup
✅ Resume after interruption
✅ Complete workflow (claim → complete)
✅ Both SQLite and PostgreSQL backends
✅ State machine validation
✅ Atomic operations
✅ Transaction safety

## Notes

- **Concurrency**: Tests use goroutines to simulate concurrent executors
- **Isolation**: Each test creates fresh database state
- **Cleanup**: Temporary files and connections automatically cleaned up
- **Real-world scenarios**: Tests simulate actual executor crash/restart patterns
- **Checkpoint persistence**: Tests validate watchdog recovery pattern (save before release)
