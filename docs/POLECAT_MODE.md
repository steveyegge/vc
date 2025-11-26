# Polecat Mode User Guide

Polecat mode enables VC to run as a single-task executor inside Gastown polecats (isolated git clones). Instead of polling for work, it executes one task and outputs structured JSON.

## Overview

### What is Polecat Mode?

Polecat mode transforms VC from a continuous executor into a single-shot quality-assured coding agent. It's designed for Gastown integration where:

- **Gastown** handles coordination (clones, mail, work routing)
- **VC** handles quality (preflight, iteration, gates)

### Key Differences from Executor Mode

| Aspect | Executor Mode | Polecat Mode |
|--------|--------------|--------------|
| Work source | Polls beads for ready issues | Accepts task from args/stdin |
| Sandbox | Creates git worktree | Uses current directory |
| Concurrency | Tracks executor instances | Single instance, no tracking |
| Output | Updates beads, emits events | JSON result to stdout |
| Loop | Continuous polling | Single execution, exits |
| Database | Writes to beads | No database mutations |

## Quick Start

### Basic Usage

```bash
# Execute a task
vc exec --polecat-mode --task "Implement user authentication"

# Execute from a beads issue
vc exec --polecat-mode --issue vc-abc

# Use lite mode for simple tasks
vc exec --polecat-mode --lite --task "Fix typo in README"

# Read task from stdin (for longer descriptions)
cat task.txt | vc exec --polecat-mode --stdin
```

### Example Output

```json
{
  "status": "completed",
  "success": true,
  "iterations": 3,
  "converged": true,
  "duration_seconds": 245.5,
  "files_modified": [
    "internal/auth/oauth.go",
    "internal/auth/oauth_test.go"
  ],
  "quality_gates": {
    "test": {"passed": true, "output": "ok ./... 2.345s"},
    "lint": {"passed": true, "output": ""},
    "build": {"passed": true, "output": ""}
  },
  "discovered_issues": [...],
  "punted_items": [...],
  "summary": "Implemented OAuth2 login with Google and GitHub providers."
}
```

## CLI Reference

### Flags

| Flag | Description |
|------|-------------|
| `--polecat-mode` | **Required.** Enable polecat mode |
| `--task TEXT` | Task description (natural language) |
| `--issue ID` | Execute beads issue by ID |
| `--stdin` | Read task from stdin |
| `--lite` | Lite mode: skip preflight, assessment; single iteration |

### Input Methods

**Via `--task` flag (short tasks):**
```bash
vc exec --polecat-mode --task "Add rate limiting to API endpoints"
```

**Via `--stdin` (longer descriptions):**
```bash
cat <<EOF | vc exec --polecat-mode --stdin
Task: Implement OAuth2 login flow

Requirements:
- Support Google and GitHub providers
- Store tokens securely
- Handle refresh token rotation

Acceptance Criteria:
- Users can log in via OAuth2
- Tokens are encrypted at rest
- Refresh works transparently
EOF
```

**Via `--issue` (from beads):**
```bash
vc exec --polecat-mode --issue vc-abc
```

## JSON Output Specification

### Top-Level Fields

| Field | Type | Description |
|-------|------|-------------|
| `status` | string | Outcome: completed, partial, blocked, failed, decomposed |
| `success` | boolean | Whether task completed successfully |
| `iterations` | integer | Number of refinement iterations performed |
| `converged` | boolean | Whether AI determined work is complete |
| `duration_seconds` | float | Total execution time |
| `files_modified` | array | Paths of files changed |
| `quality_gates` | object | Results per gate (test, lint, build) |
| `discovered_issues` | array | Issues found during execution |
| `punted_items` | array | Work explicitly deferred |
| `summary` | string | Human-readable summary |
| `error` | string? | Error message (if failed/blocked) |
| `message` | string? | Status message |
| `decomposition` | object? | Decomposition details (if decomposed) |
| `preflight_result` | object? | Preflight results (if blocked) |
| `suggested_action` | string? | Recommended next step |

### Status Values

| Status | Description |
|--------|-------------|
| `completed` | Task done, all quality gates passed |
| `partial` | Some work done but incomplete |
| `blocked` | Cannot proceed (preflight failure, unclear requirements) |
| `failed` | Execution failed (gates failed, agent crashed) |
| `decomposed` | Task was too large, subtasks created |

### Quality Gate Structure

```json
{
  "quality_gates": {
    "test": {
      "passed": true,
      "output": "ok ./... 2.345s",
      "error": ""
    },
    "lint": {
      "passed": true,
      "output": "",
      "error": ""
    },
    "build": {
      "passed": true,
      "output": "",
      "error": ""
    }
  }
}
```

### Discovered Issue Structure

```json
{
  "discovered_issues": [
    {
      "title": "Add rate limiting to OAuth endpoints",
      "description": "OAuth endpoints should have rate limiting to prevent abuse",
      "type": "task",
      "priority": 2
    }
  ]
}
```

### Decomposition Structure

```json
{
  "decomposition": {
    "reasoning": "Task estimated at 120 minutes, breaking into subtasks",
    "subtasks": [
      {"title": "Implement OAuth2 provider interface", "priority": 0},
      {"title": "Add Google OAuth provider", "priority": 1},
      {"title": "Add GitHub OAuth provider", "priority": 1},
      {"title": "Implement token storage", "priority": 2}
    ]
  }
}
```

## Lite Mode

Lite mode is optimized for trivial tasks where full VC overhead is unnecessary.

### What Lite Mode Skips

| Component | Full Mode | Lite Mode |
|-----------|-----------|-----------|
| Preflight | Run | Skip |
| Assessment | Run | Skip |
| Decomposition | If needed | Skip |
| Execution | Run | Run |
| Iteration | 3-7 passes | 1 pass |
| Quality Gates | Run | **Run** (always verify) |

### When to Use Lite Mode

Use lite mode for:
- Typo fixes
- Simple documentation updates
- Single-file changes with obvious scope
- Tasks under ~30 lines of code

Don't use lite mode for:
- Multi-file changes
- Architectural decisions
- Complex bug fixes
- Any task with unclear scope

### Heuristic Detection

The polecat wrapper scripts in `examples/gastown/` include heuristics for automatic lite mode detection. See their documentation for details.

## Activity Feed Events

Polecat mode emits structured JSON events to stderr for monitoring:

```json
{"type":"polecat_start","task":"...","source":"cli","lite":false,"timestamp":"..."}
{"type":"polecat_preflight","passed":true,"gates":{"build":true,"lint":true},"timestamp":"..."}
{"type":"polecat_agent_start","iteration":1,"timestamp":"..."}
{"type":"polecat_agent_complete","iteration":1,"success":true,"files":["..."],"timestamp":"..."}
{"type":"polecat_gate","name":"test","passed":true,"output":"...","timestamp":"..."}
{"type":"polecat_complete","status":"completed","success":true,"duration":245.5,"timestamp":"..."}
```

Events are enabled by default. Disable with `EnableEvents: false` in config or by redirecting stderr.

## Configuration

### YAML Configuration File

Place at `.vc/config.yaml` or specify with `--config`:

```yaml
mode: polecat

preflight:
  enabled: true
  failure_mode: block  # block | warn | ignore
  cache_ttl: 5m

assessment:
  enabled: true
  decomposition_threshold: 30  # minutes

iteration:
  min_iterations: 1
  max_iterations: 7
  convergence_confidence: 0.85

gates:
  test:
    enabled: true
    command: "go test ./..."
    timeout: 5m
  lint:
    enabled: true
    command: "golangci-lint run ./..."
    timeout: 2m
  build:
    enabled: true
    command: "go build ./..."
    timeout: 3m

lite_mode:
  skip_preflight: true
  skip_assessment: true
  max_iterations: 1
```

### Environment Variables

```bash
# AI configuration (required for assessment/analysis)
export ANTHROPIC_API_KEY=sk-...

# Polecat mode defaults
export VC_POLECAT_MODE=true
export VC_LITE_MODE=false
export VC_SKIP_PREFLIGHT=false

# Gate configuration
export VC_GATES_TIMEOUT=5m
export VC_TEST_COMMAND="go test ./..."
export VC_LINT_COMMAND="golangci-lint run ./..."
```

## Error Handling

### Preflight Failures

When baseline is broken before starting:

```json
{
  "status": "blocked",
  "success": false,
  "error": "preflight_failed",
  "preflight_result": {
    "test": {"passed": false, "output": "TestAuth failed: ..."},
    "lint": {"passed": true},
    "build": {"passed": true}
  },
  "message": "Baseline tests failing. Fix required before work can proceed.",
  "suggested_action": "Fix baseline failures before running VC"
}
```

### Quality Gate Failures

When gates fail after execution:

```json
{
  "status": "failed",
  "success": false,
  "error": "gates_failed",
  "quality_gates": {
    "test": {"passed": false, "output": "TestNewFeature failed: ..."},
    "lint": {"passed": true},
    "build": {"passed": true}
  },
  "iterations": 3,
  "message": "Tests failing after 3 iterations. Manual intervention needed."
}
```

### Agent Failures

When the coding agent crashes or times out:

```json
{
  "status": "failed",
  "success": false,
  "error": "agent execution failed: timeout after 30m",
  "iterations": 1,
  "message": "Agent timed out during execution"
}
```

## Gastown Integration

### Architecture

```
┌─────────────────────────────────────────────────────────────┐
│                         GASTOWN                              │
│  ┌─────────┐    ┌─────────┐    ┌─────────┐    ┌─────────┐   │
│  │  Boss   │    │ Polecat │    │ Polecat │    │ Polecat │   │
│  │         │───▶│GreenLake│    │ BlueDog │    │RedStone │   │
│  │ Routes  │    │ ┌─────┐ │    │ ┌─────┐ │    │ ┌─────┐ │   │
│  │  Work   │    │ │ VC  │ │    │ │ VC  │ │    │ │ VC  │ │   │
│  └─────────┘    │ │Exec │ │    │ │Exec │ │    │ │Exec │ │   │
│                 │ └─────┘ │    │ └─────┘ │    │ └─────┘ │   │
│                 └─────────┘    └─────────┘    └─────────┘   │
└─────────────────────────────────────────────────────────────┘
```

### Workflow

1. **Boss sends work** via Gastown Mail
2. **Polecat receives message** and invokes VC
3. **VC executes** with quality loop (preflight → assess → execute → iterate → gates)
4. **VC returns JSON** to stdout
5. **Polecat wrapper**:
   - Creates discovered issues in beads
   - Commits/merges if successful
   - Replies via GM with status
6. **Boss routes next work** or marks mission complete

### Example Wrapper Scripts

See `examples/gastown/` for ready-to-use wrapper scripts:

- `polecat_wrapper.py` - Full-featured Python wrapper
- `polecat_wrapper.sh` - Simpler shell alternative

## Troubleshooting

### VC command not found

Ensure `vc` is in your PATH:
```bash
which vc
# Should output path to vc binary

# If not found, add to PATH:
export PATH=$PATH:/path/to/vc
```

### No JSON output

Check if VC failed before producing output:
```bash
vc exec --polecat-mode --task "..." 2>&1 | head -20
```

Progress info goes to stderr; JSON to stdout.

### Preflight always fails

Check if your baseline is actually broken:
```bash
go test ./...
golangci-lint run ./...
go build ./...
```

Use `--lite` to skip preflight for testing.

### Agent timeout

Increase timeout or use lite mode:
```bash
# In config.yaml
agent_timeout: 60m

# Or use lite mode for simpler tasks
vc exec --polecat-mode --lite --task "..."
```

### No AI assessment/analysis

Ensure `ANTHROPIC_API_KEY` is set:
```bash
export ANTHROPIC_API_KEY=sk-...
```

Without the key, VC runs without AI supervision (warnings logged to stderr).

## See Also

- [GASTOWN_INTEGRATION.md](design/GASTOWN_INTEGRATION.md) - Full design specification
- [FEATURES.md](FEATURES.md) - VC feature documentation
- [CONFIGURATION.md](CONFIGURATION.md) - All configuration options
- [examples/gastown/](../examples/gastown/) - Wrapper script examples
