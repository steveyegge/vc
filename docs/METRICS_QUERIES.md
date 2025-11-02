# VC Executor Metrics Queries

This document provides SQL queries for analyzing executor performance metrics captured during dogfooding experiments and production runs.

## Overview

The VC executor tracks comprehensive metrics via the `ExecutionTelemetry` system (vc-b5db):
- **Phase durations**: Time spent in assess, execute, analyze, gates phases
- **Discovered issues**: Count of issues found during analysis
- **Quality gate results**: Detailed pass/fail status for each gate
- **Success metrics**: Overall execution success rates

Metrics are exported to JSON files via `Monitor.ExportToJSON()` and can be queried using SQLite or jq.

## Metrics Export

```bash
# Export current telemetry to JSON
./vc-executor --export-metrics metrics.json

# Append to existing metrics file
./vc-executor --export-metrics metrics.json --append

# Export during executor run (programmatically)
monitor.ExportToJSON("metrics-run-20250102.json", false)
```

## JSON Structure

```json
[
  {
    "IssueID": "vc-879d",
    "StartTime": "2025-11-02T14:37:40Z",
    "EndTime": "2025-11-02T14:42:15Z",
    "PhaseDurations": {
      "assess": "25s",
      "execute": "245s",
      "analyze": "15s",
      "gates": "30s"
    },
    "DiscoveredIssuesCount": 9,
    "Success": true,
    "GatesPassed": true,
    "GateResults": {
      "build": {"Passed": true, "Duration": "15s"},
      "test": {"Passed": true, "Duration": "12s"},
      "lint": {"Passed": true, "Duration": "3s"}
    }
  }
]
```

## SQL Queries (via sqlite3)

### Convert JSON to SQLite

```bash
# Create temporary SQLite database from JSON
cat metrics.json | jq -r '.[] | [.IssueID, .Success, .GatesPassed, .DiscoveredIssuesCount] | @csv' > metrics.csv
sqlite3 metrics.db << EOF
CREATE TABLE executions (
  issue_id TEXT PRIMARY KEY,
  success BOOLEAN,
  gates_passed BOOLEAN,
  discovered_count INTEGER,
  total_duration_ms INTEGER,
  assess_duration_ms INTEGER,
  execute_duration_ms INTEGER,
  analyze_duration_ms INTEGER,
  gates_duration_ms INTEGER
);
.mode csv
.import metrics.csv executions
