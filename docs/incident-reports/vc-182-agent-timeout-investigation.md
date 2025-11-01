# Incident Report: Agent Timeout on vc-1 Initialization

**Issue**: vc-182  
**Date**: 2025-10-25  
**Severity**: P0 (Blocker)  
**Status**: Root Cause Identified

## Executive Summary

Agent session for vc-1 failed with "Network timeout" error after 207 seconds. The root cause was a **circuit breaker being in OPEN state** due to prior API failures, preventing the AI assessment phase from completing. The agent then encountered a network timeout when attempting to initialize.

## Timeline

| Time | Event | Details |
|------|-------|---------|
| 15:55:12 | Issue Claimed | Executor d5bc8390 claimed vc-1 |
| 15:55:12 | Assessment Started | AI assessment initiated |
| 15:55:12 | Assessment Failed | **Circuit breaker is open** - AI API call rejected |
| 15:55:13 | Agent Spawned | Amp agent started despite assessment failure |
| 15:58:40 | Network Timeout | Agent error: "Network timeout. Check your connection or proxy settings and retry." |
| 15:59:45 | Agent Completed | Agent exited with code 1 (failure) |
| 16:24:35 - 16:26:10 | Analysis Phase | AI analysis discovered 3 issues, including this vc-182 |

**Total Agent Runtime**: 272 seconds (4m 32s)  
**Time to First Error**: 207 seconds (3m 27s)

## Root Cause Analysis

### Primary Cause: Circuit Breaker in OPEN State

The AI Supervisor's circuit breaker was **already in OPEN state** when vc-1 execution began. This indicates that the supervisor had experienced ≥5 consecutive API failures in the previous 30 seconds.

**Evidence**:
```
assessment_completed|error|AI assessment failed: anthropic API call failed: assessment failed: circuit breaker is open
```

**Circuit Breaker Configuration** ([internal/ai/retry.go](file:///Users/stevey/src/vc/internal/ai/retry.go)):
- Failure Threshold: 5 consecutive failures
- Open Timeout: 30 seconds
- Success Threshold: 2 successes to close in HALF_OPEN state

### Secondary Cause: Network Timeout During Agent Initialization

After the assessment was skipped, the Amp agent was spawned but encountered a network timeout 207 seconds later.

**Evidence**:
```
agent_id: 58209e93-74c2-4ba4-9cf5-2f341d85a1d7
build_output|error|Network timeout. Check your connection or proxy settings and retry.
```

This suggests the agent was attempting to:
1. Initialize its connection to the Anthropic API, OR
2. Download required resources/tools, OR
3. Perform first API call to Claude

### Incorrect Timeout Description

The issue description states "timed out after 57 seconds" but the actual agent runtime was **272 seconds** before completion. The 57-second figure may refer to a different metric or be based on incomplete data.

## Contributing Factors

### 1. Graceful Degradation Not Fully Implemented

The executor **continued spawning the agent** despite the assessment failure. The code includes a fallback:

```go
// internal/executor/executor_execution.go:81-88
fmt.Fprintf(os.Stderr, "Warning: AI assessment failed: %v (continuing without assessment)\n", err)
```

However, if the circuit breaker is open due to **API authentication issues** or **network connectivity problems**, spawning an agent that requires the same API will also fail.

### 2. No Pre-Flight Connectivity Check

Before spawning expensive agent processes, VC doesn't verify:
- API key validity
- Network connectivity to api.anthropic.com
- Circuit breaker state (OPEN/HALF_OPEN should trigger warnings)

### 3. Insufficient Circuit Breaker Visibility

The circuit breaker state transitions are not logged to events or exposed to operators. The executor has no visibility into:
- Current circuit state
- Failure count
- Time until HALF_OPEN transition

## Impact

**Execution Failure**: vc-1 failed to execute, blocking dependent work  
**Resource Waste**: 272 seconds of agent runtime with no useful output  
**Discovered Issues**: Despite failure, analysis discovered 3 issues including:
- vc-182 (this issue)
- vc-183 (cost approval needed)
- [duplicate of vc-15]

## Recommended Fixes

### Fix 1: Pre-Flight Health Check (High Priority)

Before spawning agent, check circuit breaker state and API connectivity:

```go
// Pseudo-code
func (e *Executor) executeIssue(ctx context.Context, issue *types.Issue) error {
    // Check supervisor health before proceeding
    if e.supervisor != nil {
        if err := e.supervisor.HealthCheck(ctx); err != nil {
            if errors.Is(err, ai.ErrCircuitOpen) {
                return fmt.Errorf("cannot execute: AI supervisor circuit breaker is open (will retry in %v)", waitTime)
            }
            return fmt.Errorf("AI supervisor health check failed: %w", err)
        }
    }
    
    // Continue with execution...
}
```

**Benefits**:
- Fail fast before spawning expensive agent
- Clear error messages about circuit breaker state
- Automatic retry when circuit recovers

### Fix 2: Circuit Breaker Observability (Medium Priority)

Emit events for circuit breaker state transitions:

```go
func (cb *CircuitBreaker) transitionToOpen() {
    // Log state transition
    cb.logStateTransition(CircuitClosed, CircuitOpen)
    // Emit event for monitoring
}
```

Log these events to `vc_agent_events` table with type `circuit_breaker_state_change`.

**Benefits**:
- Operators can see API health degradation
- Watchdog can detect patterns
- Post-mortem analysis has full timeline

### Fix 3: Smarter Assessment Fallback (Low Priority)

Distinguish between **temporary failures** (rate limit, network) and **permanent failures** (auth, invalid key):

```go
if err := e.supervisor.AssessIssueState(ctx, issue); err != nil {
    if isPermanentFailure(err) {
        return fmt.Errorf("AI supervision unavailable: %w", err)
    }
    // Continue without assessment for temporary issues
    fmt.Fprintf(os.Stderr, "Warning: skipping assessment due to temporary issue: %v\n", err)
}
```

**Benefits**:
- Don't spawn agents when API is fundamentally broken
- Continue gracefully for transient issues

### Fix 4: Agent Initialization Timeout Handling (Medium Priority)

The "Network timeout" from Amp suggests the agent itself couldn't connect to the API. Consider:
1. Amp should inherit the same circuit breaker logic as the supervisor
2. Agent initialization should have a shorter timeout (30s) before full execution timeout (30m)
3. Agent startup failures should return clear error codes

**This would require changes to Amp itself** - file issue with Sourcegraph team.

## Open Questions

1. **Why was the circuit breaker open?** What were the 5+ failures that triggered it?
   - Need to check logs before 15:55:12 to see the failure pattern
   - Possibly rate limiting, network issues, or API key problems

2. **Why did Amp timeout?** The specific "Network timeout" error from Amp needs investigation:
   - Is this a DNS resolution failure?
   - TCP connection timeout to api.anthropic.com?
   - TLS handshake failure?
   - HTTP request timeout waiting for first response?

3. **Should assessment be mandatory?** Current code continues without assessment, but this leads to wasted agent executions. Consider making assessment mandatory with fast-fail if unavailable.

## Resolution

**Immediate**:
- ✅ Root cause identified and documented
- ✅ **Fix 1 Implemented**: Added pre-flight health check in [internal/executor/executor_execution.go](file:///Users/stevey/src/vc/internal/executor/executor_execution.go)
  - Added `HealthCheck()` method to AI Supervisor
  - Executor now checks circuit breaker state before spawning agents
  - Fails fast with clear error message when circuit breaker is OPEN
  - Allows execution in HALF_OPEN state (probing for recovery)

**Short-term**:
- ✅ **Fix 2 Implemented**: Added observability event type
  - Added `EventTypeCircuitBreakerStateChange` to [internal/events/types.go](file:///Users/stevey/src/vc/internal/events/types.go)
  - Circuit breaker already logs state transitions to stdout
  - TODO: Wire up event emission from circuit breaker to storage
- Investigate prior circuit breaker failures (historical analysis needed)

**Long-term**:
- Implement Fix 3 (smarter fallback) - distinguish permanent vs temporary failures
- File issue with Sourcegraph for Fix 4 (Amp initialization timeout handling)

## Testing

**Added Tests**:
- [internal/ai/health_check_test.go](file:///Users/stevey/src/vc/internal/ai/health_check_test.go): Comprehensive tests for HealthCheck() method
  - Circuit breaker CLOSED state (passes)
  - Circuit breaker HALF_OPEN state (passes, allows probing)
  - Circuit breaker OPEN state (fails with ErrCircuitOpen)
  - Circuit breaker disabled (passes)

**Verification**:
```bash
$ go test ./internal/ai -v -run TestSupervisorHealthCheck
PASS
$ go test ./internal/... -short
PASS (all tests passing)
```

## Related Issues

- vc-1: Original task that triggered this failure
- vc-28: Watchdog ineffective due to lack of progress events (related monitoring issue)
- vc-220: Concurrency limit for AI calls (circuit breaker config)
- vc-117: Agent infinite loop detection (different circuit breaker)

---

**Investigator**: AI Supervisor  
**Date**: 2025-10-31  
**Review Status**: Pending human review
