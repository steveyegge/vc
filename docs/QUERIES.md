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
