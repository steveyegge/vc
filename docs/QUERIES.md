# SQL Query Reference

This document contains SQL queries for analyzing VC's activity feed and metrics.

All queries operate on the `.beads/vc.db` SQLite database, specifically the `agent_events` table.

**Quick access via sqlite3:**
```bash
sqlite3 .beads/vc.db
```

---

## 📊 Quality Gates Progress Queries (vc-267)

Quality gates emit progress events during execution, allowing you to monitor long-running test suites and distinguish between stuck gates and slow gates.

### Event Types

Three event types track quality gates execution:

1. **`quality_gates_started`** - When gates begin executing
2. **`quality_gates_progress`** - Progress updates during execution (every 30s + per-gate)
3. **`quality_gates_completed`** - When gates finish (success or failure)

### Progress Event Data

Progress events include:
- **current_gate**: Which gate is running (test, lint, build)
- **gates_completed**: Number of gates finished
- **total_gates**: Total gates to run (usually 3: build, test, lint)
- **elapsed_seconds**: Time since gates started
- **start_time**: Timestamp when gates started

### Queries

**View gate execution timeline for an issue:**
```sql
SELECT
  timestamp,
  type,
  message,
  json_extract(data, '$.current_gate') as gate,
  json_extract(data, '$.gates_completed') as completed,
  json_extract(data, '$.total_gates') as total,
  json_extract(data, '$.elapsed_seconds') as elapsed_sec
FROM agent_events
WHERE issue_id = 'vc-XXX'
  AND type IN ('quality_gates_started', 'quality_gates_progress', 'quality_gates_completed')
ORDER BY timestamp;
```

**Find slow gate executions (>3 minutes):**
```sql
SELECT
  issue_id,
  MIN(timestamp) as start_time,
  MAX(timestamp) as end_time,
  (julianday(MAX(timestamp)) - julianday(MIN(timestamp))) * 86400 as duration_sec
FROM agent_events
WHERE type IN ('quality_gates_started', 'quality_gates_completed')
GROUP BY issue_id
HAVING duration_sec > 180
ORDER BY duration_sec DESC;
```

**Gate progress during execution:**
```sql
SELECT
  timestamp,
  message,
  json_extract(data, '$.current_gate') as current_gate,
  json_extract(data, '$.gates_completed') as completed,
  json_extract(data, '$.elapsed_seconds') as elapsed_sec
FROM agent_events
WHERE issue_id = 'vc-XXX'
  AND type = 'quality_gates_progress'
ORDER BY timestamp;
```

**Average gate execution time by gate type:**
```sql
SELECT
  json_extract(data, '$.gate') as gate_type,
  COUNT(*) as executions,
  AVG(json_extract(data, '$.elapsed_seconds')) as avg_seconds
FROM agent_events
WHERE type = 'quality_gates_progress'
  AND json_extract(data, '$.gate') IS NOT NULL
  AND json_extract(data, '$.gate') != ''
GROUP BY gate_type
ORDER BY avg_seconds DESC;
```

**Detect stuck gates (no progress for >5 minutes):**
```sql
WITH latest_progress AS (
  SELECT
    issue_id,
    MAX(timestamp) as last_progress,
    (julianday('now') - julianday(MAX(timestamp))) * 86400 as seconds_since_progress
  FROM agent_events
  WHERE type IN ('quality_gates_started', 'quality_gates_progress')
  GROUP BY issue_id
)
SELECT
  issue_id,
  last_progress,
  ROUND(seconds_since_progress) as stuck_for_seconds
FROM latest_progress
WHERE seconds_since_progress > 300
  AND issue_id NOT IN (
    SELECT DISTINCT issue_id
    FROM agent_events
    WHERE type = 'quality_gates_completed'
  )
ORDER BY seconds_since_progress DESC;
```

---

## 📊 Deduplication Metrics Queries (vc-151)

VC tracks comprehensive deduplication metrics in the `agent_events` table. All deduplication operations emit structured events that can be queried for analysis.

### Event Types

Three event types track deduplication activity:

1. **`deduplication_batch_started`** - When batch deduplication begins
2. **`deduplication_batch_completed`** - When batch deduplication completes (with stats)
3. **`deduplication_decision`** - Individual duplicate decisions (with confidence scores)

### Data Fields

**DeduplicationBatchCompletedData:**
- `total_candidates` - Number of issues checked
- `unique_count` - Number of unique issues
- `duplicate_count` - Duplicates against existing issues
- `within_batch_duplicate_count` - Duplicates within the batch
- `comparisons_made` - Total pairwise comparisons
- `ai_calls_made` - Number of AI API calls
- `processing_time_ms` - Time taken in milliseconds
- `success` - Whether deduplication succeeded
- `error` - Error message (if failed)

**DeduplicationDecisionData:**
- `candidate_title` - Title of the candidate issue
- `is_duplicate` - Whether marked as duplicate
- `duplicate_of` - ID of existing issue (if duplicate)
- `confidence` - AI confidence score (0.0 to 1.0)
- `reasoning` - AI explanation for the decision
- `within_batch_duplicate` - If this is a within-batch duplicate
- `within_batch_original` - Reference to original (if within-batch)

### Queries

**View recent deduplication batches:**
```sql
SELECT
  timestamp,
  issue_id,
  message,
  json_extract(data, '$.total_candidates') as candidates,
  json_extract(data, '$.unique_count') as unique,
  json_extract(data, '$.duplicate_count') as duplicates,
  json_extract(data, '$.ai_calls_made') as ai_calls,
  json_extract(data, '$.processing_time_ms') as time_ms
FROM agent_events
WHERE type = 'deduplication_batch_completed'
  AND json_extract(data, '$.success') = 1
ORDER BY timestamp DESC
LIMIT 10;
```

**Confidence score distribution:**
```sql
SELECT
  ROUND(json_extract(data, '$.confidence'), 1) as confidence_bucket,
  COUNT(*) as count,
  SUM(CASE WHEN json_extract(data, '$.is_duplicate') = 1 THEN 1 ELSE 0 END) as duplicates,
  SUM(CASE WHEN json_extract(data, '$.is_duplicate') = 0 THEN 1 ELSE 0 END) as unique
FROM agent_events
WHERE type = 'deduplication_decision'
GROUP BY confidence_bucket
ORDER BY confidence_bucket DESC;
```

**Deduplication efficiency over time:**
```sql
SELECT
  date(timestamp) as date,
  COUNT(*) as batches,
  SUM(json_extract(data, '$.total_candidates')) as total_candidates,
  SUM(json_extract(data, '$.duplicate_count')) as total_duplicates,
  ROUND(100.0 * SUM(json_extract(data, '$.duplicate_count')) /
        SUM(json_extract(data, '$.total_candidates')), 2) as duplicate_rate_pct,
  SUM(json_extract(data, '$.ai_calls_made')) as total_ai_calls,
  AVG(json_extract(data, '$.processing_time_ms')) as avg_time_ms
FROM agent_events
WHERE type = 'deduplication_batch_completed'
  AND json_extract(data, '$.success') = 1
GROUP BY date
ORDER BY date DESC
LIMIT 30;
```

**Failed deduplication operations:**
```sql
SELECT
  timestamp,
  issue_id,
  message,
  json_extract(data, '$.error') as error
FROM agent_events
WHERE type = 'deduplication_batch_completed'
  AND json_extract(data, '$.success') = 0
ORDER BY timestamp DESC;
```

**Individual duplicate decisions for an issue:**
```sql
SELECT
  json_extract(data, '$.candidate_title') as title,
  json_extract(data, '$.is_duplicate') as is_dup,
  json_extract(data, '$.duplicate_of') as dup_of,
  json_extract(data, '$.confidence') as confidence,
  json_extract(data, '$.reasoning') as reasoning
FROM agent_events
WHERE type = 'deduplication_decision'
  AND issue_id = 'vc-XXX'
ORDER BY timestamp;
```

**Top duplicate issues (what issues are most frequently found as duplicates):**
```sql
SELECT
  json_extract(data, '$.duplicate_of') as issue_id,
  COUNT(*) as times_found_as_duplicate,
  GROUP_CONCAT(DISTINCT json_extract(data, '$.candidate_title'), '; ') as duplicate_titles
FROM agent_events
WHERE type = 'deduplication_decision'
  AND json_extract(data, '$.is_duplicate') = 1
  AND json_extract(data, '$.duplicate_of') IS NOT NULL
GROUP BY json_extract(data, '$.duplicate_of')
ORDER BY times_found_as_duplicate DESC
LIMIT 20;
```

**Check for high false positive rate (low confidence duplicates being marked):**
```sql
SELECT COUNT(*) as low_confidence_duplicates
FROM agent_events
WHERE type = 'deduplication_decision'
  AND json_extract(data, '$.is_duplicate') = 1
  AND json_extract(data, '$.confidence') < 0.90;
```

**Check for deduplication performance issues:**
```sql
SELECT
  AVG(json_extract(data, '$.processing_time_ms')) as avg_ms,
  MAX(json_extract(data, '$.processing_time_ms')) as max_ms,
  AVG(json_extract(data, '$.ai_calls_made')) as avg_calls
FROM agent_events
WHERE type = 'deduplication_batch_completed'
  AND json_extract(data, '$.success') = 1
  AND timestamp > datetime('now', '-7 days');
```

---

## 📡 Agent Progress Queries (vc-129)

When agents execute in background mode, their progress is captured as structured events in the activity feed. This provides visibility into what agents are doing and helps distinguish between actual hangs and normal operation.

### Event Types

Three event types track agent progress:

1. **`agent_tool_use`** - Captured when agent invokes a tool (Read, Edit, Write, Bash, Glob, Grep, Task)
2. **`agent_heartbeat`** - Periodic progress updates (future - not yet emitted)
3. **`agent_state_change`** - Agent state transitions like thinking→planning→executing (future - not yet emitted)

### Data Fields

**AgentToolUseData:**
- `tool_name` - Name of the tool invoked
- `tool_description` - What the tool is doing
- `target_file` - File being operated on (if applicable)
- `command` - Command being executed (for Bash tool)

### Queries

**View tool usage for an issue:**
```sql
SELECT
  timestamp,
  message,
  json_extract(data, '$.tool_name') as tool,
  json_extract(data, '$.target_file') as file,
  json_extract(data, '$.tool_description') as description
FROM agent_events
WHERE type = 'agent_tool_use'
  AND issue_id = 'vc-XXX'
ORDER BY timestamp;
```

**Tool usage frequency:**
```sql
SELECT
  json_extract(data, '$.tool_name') as tool,
  COUNT(*) as usage_count
FROM agent_events
WHERE type = 'agent_tool_use'
  AND timestamp > datetime('now', '-7 days')
GROUP BY tool
ORDER BY usage_count DESC;
```

**Agent activity timeline:**
```sql
SELECT
  timestamp,
  type,
  message,
  CASE type
    WHEN 'agent_tool_use' THEN json_extract(data, '$.tool_name')
    WHEN 'file_modified' THEN json_extract(data, '$.file_path')
    WHEN 'git_operation' THEN json_extract(data, '$.command')
    ELSE ''
  END as detail
FROM agent_events
WHERE issue_id = 'vc-XXX'
  AND type IN ('agent_tool_use', 'file_modified', 'git_operation', 'progress')
ORDER BY timestamp;
```

---

## 🔧 Self-Healing Metrics Queries (vc-210, vc-230)

When preflight detects baseline test failures, VC can automatically fix them. These queries track self-healing success rates.

**Self-healing success rate:**
```sql
SELECT
  COUNT(*) as total_attempts,
  SUM(CASE WHEN json_extract(data, '$.success') = 1 THEN 1 ELSE 0 END) as successful,
  ROUND(100.0 * SUM(CASE WHEN json_extract(data, '$.success') = 1 THEN 1 ELSE 0 END) / COUNT(*), 2) as success_rate_pct
FROM agent_events
WHERE type = 'baseline_test_fix_completed';
```

**Recent self-healing attempts:**
```sql
SELECT
  timestamp,
  issue_id,
  message,
  json_extract(data, '$.success') as success,
  json_extract(data, '$.fix_type') as fix_type,
  json_extract(data, '$.duration_sec') as duration_sec
FROM agent_events
WHERE type = 'baseline_test_fix_completed'
ORDER BY timestamp DESC
LIMIT 10;
```

**Diagnosis quality (confidence scores):**
```sql
SELECT
  issue_id,
  json_extract(data, '$.failure_type') as failure_type,
  json_extract(data, '$.confidence') as confidence,
  json_extract(data, '$.test_names') as test_names
FROM agent_events
WHERE type = 'baseline_test_fix_started'
ORDER BY timestamp DESC
LIMIT 10;
```

**Fix type distribution:**
```sql
SELECT
  json_extract(data, '$.fix_type') as fix_type,
  COUNT(*) as count,
  SUM(CASE WHEN json_extract(data, '$.success') = 1 THEN 1 ELSE 0 END) as successful
FROM agent_events
WHERE type = 'baseline_test_fix_completed'
GROUP BY fix_type
ORDER BY count DESC;
```

**Failure type distribution:**
```sql
SELECT
  json_extract(data, '$.failure_type') as failure_type,
  COUNT(*) as count,
  SUM(CASE WHEN json_extract(data, '$.success') = 1 THEN 1 ELSE 0 END) as fixed
FROM agent_events
WHERE type = 'baseline_test_fix_started'
GROUP BY failure_type
ORDER BY count DESC;
```

---

## 📈 Executor Metrics Queries (vc-b5db)

The executor captures comprehensive metrics about issue completion including phase durations, discovered issues, and quality gate results. These are tracked in the monitoring system and displayed in execution summaries.

### Success Rate by Issue Type

**Overall success rate:**
```sql
SELECT
  COUNT(*) as total_completions,
  SUM(CASE WHEN json_extract(data, '$.success') = 1 THEN 1 ELSE 0 END) as successful,
  ROUND(100.0 * SUM(CASE WHEN json_extract(data, '$.success') = 1 THEN 1 ELSE 0 END) / COUNT(*), 2) as success_rate_pct
FROM agent_events
WHERE type = 'issue_completed';
```

**Success rate by issue type (bug vs feature vs task):**
```sql
SELECT
  i.type as issue_type,
  COUNT(*) as total,
  SUM(CASE WHEN json_extract(ae.data, '$.success') = 1 THEN 1 ELSE 0 END) as successful,
  ROUND(100.0 * SUM(CASE WHEN json_extract(ae.data, '$.success') = 1 THEN 1 ELSE 0 END) / COUNT(*), 2) as success_rate_pct
FROM agent_events ae
JOIN issues i ON ae.issue_id = i.id
WHERE ae.type = 'issue_completed'
GROUP BY i.type
ORDER BY success_rate_pct DESC;
```

**Success rate by priority:**
```sql
SELECT
  i.priority as priority,
  COUNT(*) as total,
  SUM(CASE WHEN json_extract(ae.data, '$.success') = 1 THEN 1 ELSE 0 END) as successful,
  ROUND(100.0 * SUM(CASE WHEN json_extract(ae.data, '$.success') = 1 THEN 1 ELSE 0 END) / COUNT(*), 2) as success_rate_pct
FROM agent_events ae
JOIN issues i ON ae.issue_id = i.id
WHERE ae.type = 'issue_completed'
GROUP BY i.priority
ORDER BY i.priority;
```

### Average Duration by Phase

**Note:** Phase duration metrics are captured by the monitoring system but not currently stored as separate events. This is tracked via in-memory telemetry and displayed in execution summaries. Future enhancement could emit phase completion events for historical analysis.

**Quality gates duration (from progress events):**
```sql
SELECT
  AVG(
    (julianday(MAX(CASE WHEN type = 'quality_gates_completed' THEN timestamp END)) -
     julianday(MIN(CASE WHEN type = 'quality_gates_started' THEN timestamp END))) * 86400
  ) as avg_gates_duration_sec
FROM agent_events
WHERE issue_id IN (
  SELECT DISTINCT issue_id FROM agent_events
  WHERE type = 'quality_gates_started'
)
GROUP BY issue_id;
```

### Discovered Issues Per Completion

**Average discovered issues:**
```sql
SELECT
  AVG(json_extract(data, '$.discovered_count')) as avg_discovered,
  MAX(json_extract(data, '$.discovered_count')) as max_discovered,
  MIN(json_extract(data, '$.discovered_count')) as min_discovered
FROM agent_events
WHERE type = 'analysis_completed'
  AND json_extract(data, '$.discovered_count') IS NOT NULL;
```

**Issues that spawned most follow-on work:**
```sql
SELECT
  issue_id,
  json_extract(data, '$.discovered_count') as discovered,
  message
FROM agent_events
WHERE type = 'analysis_completed'
  AND json_extract(data, '$.discovered_count') > 3
ORDER BY json_extract(data, '$.discovered_count') DESC
LIMIT 10;
```

**Discovered issues breakdown by type:**
```sql
SELECT
  json_extract(data, '$.issue_type') as discovered_type,
  COUNT(*) as count
FROM agent_events
WHERE type = 'issue_created'
  AND json_extract(data, '$.discovered_by') IS NOT NULL
GROUP BY discovered_type
ORDER BY count DESC;
```

### Quality Gate Pass Rate

**Overall quality gate pass rate:**
```sql
SELECT
  COUNT(*) as total_gate_runs,
  SUM(CASE WHEN json_extract(data, '$.all_passed') = 1 THEN 1 ELSE 0 END) as passed,
  ROUND(100.0 * SUM(CASE WHEN json_extract(data, '$.all_passed') = 1 THEN 1 ELSE 0 END) / COUNT(*), 2) as pass_rate_pct
FROM agent_events
WHERE type = 'quality_gates_completed';
```

**Pass rate by gate type:**
```sql
SELECT
  json_extract(data, '$.gate_name') as gate,
  COUNT(*) as executions,
  SUM(CASE WHEN json_extract(data, '$.passed') = 1 THEN 1 ELSE 0 END) as passed,
  ROUND(100.0 * SUM(CASE WHEN json_extract(data, '$.passed') = 1 THEN 1 ELSE 0 END) / COUNT(*), 2) as pass_rate_pct
FROM agent_events
WHERE type = 'quality_gate_result'
GROUP BY gate
ORDER BY pass_rate_pct ASC;
```

**Recent quality gate failures:**
```sql
SELECT
  timestamp,
  issue_id,
  json_extract(data, '$.gate_name') as gate,
  json_extract(data, '$.message') as error
FROM agent_events
WHERE type = 'quality_gate_result'
  AND json_extract(data, '$.passed') = 0
ORDER BY timestamp DESC
LIMIT 20;
```

### Velocity and Throughput

**Issues completed per day (last 30 days):**
```sql
SELECT
  date(timestamp) as date,
  COUNT(*) as completions,
  SUM(CASE WHEN json_extract(data, '$.success') = 1 THEN 1 ELSE 0 END) as successful
FROM agent_events
WHERE type = 'issue_completed'
  AND timestamp > datetime('now', '-30 days')
GROUP BY date
ORDER BY date DESC;
```

**Average time to completion:**
```sql
SELECT
  AVG(
    (julianday(completed_at) - julianday(created_at)) * 86400
  ) as avg_completion_time_sec
FROM issues
WHERE status = 'closed'
  AND completed_at IS NOT NULL
  AND created_at IS NOT NULL;
```

**Executor efficiency (issues per hour when running):**
```sql
WITH executor_runtime AS (
  SELECT
    SUM(
      (julianday(MAX(timestamp)) - julianday(MIN(timestamp))) * 24
    ) as total_hours
  FROM agent_events
  GROUP BY issue_id
),
total_completions AS (
  SELECT COUNT(*) as count FROM issues WHERE status = 'closed'
)
SELECT
  tc.count as total_completed,
  er.total_hours as total_runtime_hours,
  ROUND(tc.count * 1.0 / er.total_hours, 2) as issues_per_hour
FROM executor_runtime er, total_completions tc;
```

### Failure Mode Analysis

**Failure reasons distribution:**
```sql
SELECT
  json_extract(data, '$.failure_reason') as reason,
  COUNT(*) as count
FROM agent_events
WHERE type = 'issue_failed'
  OR (type = 'issue_completed' AND json_extract(data, '$.success') = 0)
GROUP BY reason
ORDER BY count DESC;
```

**Blocked issues by blocker type:**
```sql
SELECT
  json_extract(data, '$.blocker_type') as blocker,
  COUNT(*) as count
FROM agent_events
WHERE type = 'issue_blocked'
GROUP BY blocker
ORDER BY count DESC;
```

### Resource Usage Trends

**Note:** Agent message/token counts are tracked in agent telemetry but not currently emitted as events. This data is available in real-time summaries but not stored for historical analysis. Consider adding `agent_stats` events in the future.

**Estimated API usage (from agent activity):**
```sql
SELECT
  date(timestamp) as date,
  COUNT(DISTINCT issue_id) as issues_worked,
  COUNT(*) as total_tool_uses,
  SUM(CASE WHEN json_extract(data, '$.tool_name') = 'Task' THEN 1 ELSE 0 END) as subagent_spawns
FROM agent_events
WHERE type = 'agent_tool_use'
  AND timestamp > datetime('now', '-30 days')
GROUP BY date
ORDER BY date DESC;
```

---

## 🗄️ Event Retention Queries (Future)

**Status:** Not yet implemented. See docs/CONFIGURATION.md for planned event retention features.

When event retention is implemented, these queries will be useful:

**Check event table size:**
```sql
SELECT COUNT(*) as total_events,
       COUNT(DISTINCT issue_id) as issues_with_events,
       MIN(timestamp) as oldest_event,
       MAX(timestamp) as newest_event
FROM agent_events;
```

**Events per issue distribution:**
```sql
SELECT issue_id,
       COUNT(*) as event_count,
       MIN(timestamp) as first_event,
       MAX(timestamp) as last_event
FROM agent_events
GROUP BY issue_id
ORDER BY event_count DESC
LIMIT 20;
```

**Event types by age:**
```sql
SELECT type,
       COUNT(*) as count,
       AVG(julianday('now') - julianday(timestamp)) as avg_age_days
FROM agent_events
GROUP BY type
ORDER BY avg_age_days DESC;
```

---

## 💰 Quota Monitoring Queries (vc-7e21)

VC tracks quota usage at two levels:
1. **Snapshots** - Point-in-time usage captured every 5 minutes
2. **Operations** - Individual AI calls with full attribution

These queries help analyze burn rates, predict quota exhaustion, and identify high-cost operations.

### Quota Usage Overview

**Current quota snapshot (most recent):**
```sql
SELECT *
FROM vc_quota_snapshots
ORDER BY timestamp DESC
LIMIT 1;
```

**Quota usage trend (last hour):**
```sql
SELECT
  timestamp,
  hourly_tokens_used,
  hourly_cost_used,
  budget_status,
  issues_worked
FROM vc_quota_snapshots
WHERE timestamp > datetime('now', '-1 hour')
ORDER BY timestamp;
```

**Hourly budget consumption over time:**
```sql
SELECT
  strftime('%H:%M', timestamp) as time,
  hourly_tokens_used,
  hourly_cost_used,
  budget_status
FROM vc_quota_snapshots
WHERE window_start > datetime('now', '-6 hours')
ORDER BY timestamp;
```

### Burn Rate Analysis

**Calculate current burn rate (tokens/min):**
```sql
WITH recent_snapshots AS (
  SELECT *
  FROM vc_quota_snapshots
  WHERE timestamp > datetime('now', '-15 minutes')
  ORDER BY timestamp
),
first_snap AS (
  SELECT * FROM recent_snapshots ORDER BY timestamp ASC LIMIT 1
),
last_snap AS (
  SELECT * FROM recent_snapshots ORDER BY timestamp DESC LIMIT 1
)
SELECT
  (last_snap.hourly_tokens_used - first_snap.hourly_tokens_used) /
    ((julianday(last_snap.timestamp) - julianday(first_snap.timestamp)) * 1440) as tokens_per_minute,
  (last_snap.hourly_cost_used - first_snap.hourly_cost_used) /
    ((julianday(last_snap.timestamp) - julianday(first_snap.timestamp)) * 1440) as cost_per_minute
FROM first_snap, last_snap;
```

**Time to quota limit prediction:**
```sql
-- Assumes max tokens = 100,000 and max cost = $5.00
WITH recent_snapshots AS (
  SELECT *
  FROM vc_quota_snapshots
  WHERE timestamp > datetime('now', '-15 minutes')
  ORDER BY timestamp
),
first_snap AS (
  SELECT * FROM recent_snapshots ORDER BY timestamp ASC LIMIT 1
),
last_snap AS (
  SELECT * FROM recent_snapshots ORDER BY timestamp DESC LIMIT 1
),
burn_rate AS (
  SELECT
    (last_snap.hourly_tokens_used - first_snap.hourly_tokens_used) /
      ((julianday(last_snap.timestamp) - julianday(first_snap.timestamp)) * 1440) as tokens_per_min,
    (last_snap.hourly_cost_used - first_snap.hourly_cost_used) /
      ((julianday(last_snap.timestamp) - julianday(first_snap.timestamp)) * 1440) as cost_per_min,
    last_snap.hourly_tokens_used as current_tokens,
    last_snap.hourly_cost_used as current_cost
  FROM first_snap, last_snap
)
SELECT
  current_tokens,
  tokens_per_min,
  (100000 - current_tokens) / tokens_per_min as minutes_until_token_limit,
  current_cost,
  cost_per_min,
  (5.00 - current_cost) / cost_per_min as minutes_until_cost_limit
FROM burn_rate
WHERE tokens_per_min > 0 OR cost_per_min > 0;
```

### Cost Attribution Queries

**Top quota consumers by operation type (last hour):**
```sql
SELECT
  operation_type,
  COUNT(*) as calls,
  SUM(input_tokens + output_tokens) as total_tokens,
  SUM(cost) as total_cost,
  AVG(cost) as avg_cost_per_call,
  AVG(duration_ms) as avg_duration_ms
FROM vc_quota_operations
WHERE timestamp > datetime('now', '-1 hour')
GROUP BY operation_type
ORDER BY total_cost DESC;
```

**Top quota consumers by issue (last hour):**
```sql
SELECT
  issue_id,
  COUNT(*) as ai_calls,
  SUM(input_tokens + output_tokens) as tokens,
  SUM(cost) as cost,
  AVG(duration_ms) as avg_duration_ms
FROM vc_quota_operations
WHERE timestamp > datetime('now', '-1 hour')
  AND issue_id IS NOT NULL
GROUP BY issue_id
ORDER BY cost DESC
LIMIT 20;
```

**Top quota consumers by model (last hour):**
```sql
SELECT
  model,
  COUNT(*) as calls,
  SUM(input_tokens + output_tokens) as total_tokens,
  SUM(cost) as total_cost,
  ROUND(AVG(cost), 4) as avg_cost_per_call
FROM vc_quota_operations
WHERE timestamp > datetime('now', '-1 hour')
GROUP BY model
ORDER BY total_cost DESC;
```

**Most expensive individual operations:**
```sql
SELECT
  timestamp,
  issue_id,
  operation_type,
  model,
  input_tokens,
  output_tokens,
  cost,
  duration_ms
FROM vc_quota_operations
ORDER BY cost DESC
LIMIT 20;
```

### Budget Window Analysis

**Usage by budget window (hourly buckets):**
```sql
SELECT
  window_start,
  MAX(hourly_tokens_used) as peak_tokens,
  MAX(hourly_cost_used) as peak_cost,
  MAX(issues_worked) as issues_worked,
  MAX(CASE WHEN budget_status = 'EXCEEDED' THEN 1 ELSE 0 END) as was_exceeded
FROM vc_quota_snapshots
GROUP BY window_start
ORDER BY window_start DESC
LIMIT 24;
```

**Budget exhaustion incidents:**
```sql
SELECT
  window_start,
  timestamp,
  hourly_tokens_used,
  hourly_cost_used,
  budget_status
FROM vc_quota_snapshots
WHERE budget_status = 'EXCEEDED'
ORDER BY timestamp DESC;
```

**Warning-to-exceeded transition time:**
```sql
WITH status_changes AS (
  SELECT
    window_start,
    timestamp,
    budget_status,
    LAG(budget_status) OVER (PARTITION BY window_start ORDER BY timestamp) as prev_status
  FROM vc_quota_snapshots
)
SELECT
  window_start,
  timestamp as exceeded_at,
  (SELECT timestamp FROM vc_quota_snapshots s2
   WHERE s2.window_start = status_changes.window_start
     AND s2.budget_status = 'WARNING'
     AND s2.timestamp < status_changes.timestamp
   ORDER BY s2.timestamp DESC
   LIMIT 1) as warning_at
FROM status_changes
WHERE prev_status = 'WARNING'
  AND budget_status = 'EXCEEDED';
```

### Operation Performance Analysis

**Slowest operations by type:**
```sql
SELECT
  operation_type,
  model,
  AVG(duration_ms) as avg_duration_ms,
  MAX(duration_ms) as max_duration_ms,
  MIN(duration_ms) as min_duration_ms,
  COUNT(*) as sample_size
FROM vc_quota_operations
WHERE duration_ms IS NOT NULL
GROUP BY operation_type, model
ORDER BY avg_duration_ms DESC;
```

**Operation cost efficiency (tokens per dollar):**
```sql
SELECT
  operation_type,
  model,
  SUM(input_tokens + output_tokens) as total_tokens,
  SUM(cost) as total_cost,
  ROUND(SUM(input_tokens + output_tokens) / SUM(cost), 0) as tokens_per_dollar
FROM vc_quota_operations
WHERE cost > 0
GROUP BY operation_type, model
ORDER BY tokens_per_dollar DESC;
```

### Cleanup and Maintenance

**Snapshot retention check:**
```sql
SELECT
  COUNT(*) as total_snapshots,
  MIN(timestamp) as oldest,
  MAX(timestamp) as newest,
  julianday('now') - julianday(MIN(timestamp)) as age_days
FROM vc_quota_snapshots;
```

**Old snapshots eligible for cleanup (>30 days):**
```sql
SELECT COUNT(*) as snapshots_to_delete
FROM vc_quota_snapshots
WHERE timestamp < datetime('now', '-30 days');
```

**Old operations eligible for cleanup (>30 days):**
```sql
SELECT COUNT(*) as operations_to_delete
FROM vc_quota_operations
WHERE timestamp < datetime('now', '-30 days');
```

---
