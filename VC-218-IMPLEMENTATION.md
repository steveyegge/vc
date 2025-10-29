# VC-218: Label-Driven State Machine - Implementation Summary

## What Was Implemented

This implements the foundation for a label-driven state machine workflow as specified in vc-218. The state machine allows missions to flow through different workflow states, claimed by different worker types.

## Components Delivered

### 1. State Machine Core (`internal/labels/state_machine.go`)

**State Labels:**
- `task-ready` - Tasks ready for code workers
- `needs-quality-gates` - Missions needing QA workers
- `needs-review` - Missions needing GitOps Arbiter
- `needs-human-approval` - Missions needing human approval
- `approved` - Missions approved for merging

**Trigger Types:**
- `task_completed` - Task was completed
- `epic_completed` - Epic was completed
- `gates_passed` - Quality gates passed
- `review_completed` - Arbiter review completed
- `human_approval` - Human approved the mission

**Functions:**
- `TransitionState()` - Transitions issue from one label state to another
  - Removes old label (if specified)
  - Adds new label
  - Logs transition event to agent_events table
- `HasLabel()` - Checks if issue has a specific label
- `GetStateLabel()` - Returns current state label (highest priority)

### 2. Event Infrastructure (`internal/events/types.go`)

**New Event Type:**
- `EventTypeLabelStateTransition` - Records state transitions

**New Data Structure:**
- `LabelStateTransitionData` - Contains:
  - `from_label` - Previous state
  - `to_label` - New state
  - `trigger` - What caused the transition
  - `actor` - Who/what initiated it
  - `mission_id` - Associated mission (optional)

### 3. Executor Integration

**Epic Completion (`internal/executor/epic.go`):**
- When an epic with subtype "mission" is completed (AI or fallback logic)
- Automatically transitions to `needs-quality-gates` state
- Logs: `✓ Mission vc-X transitioned to needs-quality-gates state`

**Quality Gates (`internal/executor/result_processor.go`):**
- When quality gates pass for a mission with `needs-quality-gates` label
- Automatically transitions to `needs-review` state
- Logs: `✓ Mission vc-X transitioned to needs-review state`

### 4. Comprehensive Tests (`internal/labels/state_machine_test.go`)

**Test Coverage:**
- State transitions (6 scenarios including initial state)
- Label preservation (state labels don't remove other labels)
- HasLabel functionality
- GetStateLabel with priority handling
- Event logging verification

All tests passing ✓

## State Flow (As Implemented)

```
1. Task completed → Epic complete check
2. Epic (mission) completed → Add "needs-quality-gates" label ✓
3. QA worker claims mission with "needs-quality-gates" → Runs gates
4. Gates pass → Remove "needs-quality-gates", Add "needs-review" ✓
5. GitOps Arbiter claims "needs-review" → Performs review (FUTURE)
6. Review complete → Add "needs-human-approval" (FUTURE)
7. Human approves → Add "approved" (FUTURE)
8. GitOps Merger claims "approved" → Merges (FUTURE)
```

**Implemented:** Steps 2 and 4 (automatic state transitions)
**Future Work:** Steps 5-8 (separate worker types)

## What's NOT Yet Implemented

### Worker-Type Specific Claiming (vc-219, vc-220, vc-221)

The current executor claims any ready work. Future workers would claim based on labels:

```go
// FUTURE: Different worker types
// Code Workers: claim open tasks without state labels
// QA Workers: claim missions with 'needs-quality-gates'
// Arbiter: claim missions with 'needs-review'
// Merger: claim missions with 'approved'
```

This requires:
- Separate executor types or claiming filters
- QA workers that ONLY run quality gates (not inline)
- GitOps Arbiter with extended-thinking review
- GitOps Merger with automated merge

### Integration with GetReadyWork

The storage layer's `GetReadyWork()` query doesn't yet filter by state labels. Future enhancement:

```go
// FUTURE: Filter ready work by worker type
filter := types.WorkFilter{
    WorkerType: types.WorkerTypeQA,  // Only return missions with needs-quality-gates
    IncludeLabels: []string{"needs-quality-gates"},
}
```

## Querying State Transitions

State transitions are logged to the `vc_agent_events` table and can be queried:

```sql
-- View state transitions for a mission
SELECT
  timestamp,
  message,
  json_extract(data, '$.from_label') as from_state,
  json_extract(data, '$.to_label') as to_state,
  json_extract(data, '$.trigger') as trigger,
  json_extract(data, '$.actor') as actor
FROM vc_agent_events
WHERE type = 'label_state_transition'
  AND issue_id = 'vc-XXX'
ORDER BY timestamp;

-- State transition statistics
SELECT
  json_extract(data, '$.to_label') as to_state,
  COUNT(*) as count
FROM vc_agent_events
WHERE type = 'label_state_transition'
GROUP BY to_state
ORDER BY count DESC;

-- Recent state transitions
SELECT
  issue_id,
  message,
  timestamp
FROM vc_agent_events
WHERE type = 'label_state_transition'
ORDER BY timestamp DESC
LIMIT 20;
```

## Files Changed

1. `internal/labels/state_machine.go` - NEW (state machine core)
2. `internal/labels/state_machine_test.go` - NEW (comprehensive tests)
3. `internal/events/types.go` - Added event type and data structure
4. `internal/executor/epic.go` - Added state transition on epic completion
5. `internal/executor/result_processor.go` - Added state transition on gates pass

## Testing

```bash
# Run label state machine tests
go test ./internal/labels/... -v

# Run epic completion tests (includes state transitions)
go test ./internal/executor/... -run TestCheckEpicCompletion -v

# All tests pass ✓
```

## Acceptance Criteria Status

From vc-218:

- ✅ Label helpers implemented (Add/Remove/Has via Beads, TransitionState added)
- ✅ State transitions automatic after task completion
- ✅ Epic completion triggers 'needs-quality-gates'
- ⏳ Each state has worker type that claims it (FUTURE - requires vc-219, vc-220, vc-221)
- ⏳ Labels block/unblock work appropriately (FUTURE - requires GetReadyWork filtering)
- ✅ Tests: task complete → epic complete → needs-quality-gates
- ⏳ Tests: verify claiming rules filter by labels (FUTURE)
- ✅ Tests: state machine doesn't skip states

**Overall:** Core infrastructure complete. Worker types deferred to follow-on issues.

## Next Steps (Follow-On Issues)

1. **vc-219: Quality Gate Workers** - Separate workers that claim needs-quality-gates
2. **vc-220: GitOps Arbiter** - Extended-thinking review, claims needs-review
3. **vc-221: GitOps Merger** - Automated merge, claims approved
4. **GetReadyWork Filtering** - Update query to filter by state labels

## Usage Example

```go
// Example: Transitioning a mission through states

// After epic completion
labels.TransitionState(ctx, store, "vc-300", "",
    labels.LabelNeedsQualityGates, labels.TriggerEpicCompleted, "executor")

// After gates pass
labels.TransitionState(ctx, store, "vc-300",
    labels.LabelNeedsQualityGates, labels.LabelNeedsReview,
    labels.TriggerGatesPassed, "qa-worker")

// After arbiter review
labels.TransitionState(ctx, store, "vc-300",
    labels.LabelNeedsReview, labels.LabelNeedsHumanApproval,
    labels.TriggerReviewCompleted, "arbiter")

// After human approval
labels.TransitionState(ctx, store, "vc-300",
    labels.LabelNeedsHumanApproval, labels.LabelApproved,
    labels.TriggerHumanApproval, "user@example.com")
```

## Architecture Notes

- **ZFC Compliance:** State transitions are events, not hardcoded logic
- **Extensible:** New states can be added without changing executor
- **Observable:** All transitions logged to agent_events for debugging
- **Idempotent:** TransitionState can be called multiple times safely
- **Best-Effort:** State transitions log warnings but don't fail operations
