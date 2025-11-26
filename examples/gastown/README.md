# Gastown Integration Examples

This directory contains example scripts for integrating VC's polecat mode with Gastown.

## Overview

VC's polecat mode is designed to run inside Gastown polecats (isolated git clones). These wrappers handle:

1. **Invoking VC** - Running `vc exec --polecat-mode` with appropriate flags
2. **Parsing results** - Processing the JSON output from VC
3. **Creating issues** - Using `bd` to create discovered issues in beads
4. **Git operations** - Committing changes and merging to main
5. **GM replies** - Sending completion status via Gastown Mail (Python only)

## Scripts

### polecat_wrapper.py (Recommended)

Full-featured Python wrapper with:
- Automatic lite mode detection via heuristics
- Gastown Mail integration
- Robust error handling
- JSON output option

```bash
# Basic usage
./polecat_wrapper.py "Implement OAuth2 login"

# With lite mode for simple tasks
./polecat_wrapper.py --lite "Fix typo in README"

# Execute a beads issue
./polecat_wrapper.py --issue vc-abc

# Read task from stdin (for long descriptions)
cat task.txt | ./polecat_wrapper.py --stdin

# Dry run (don't commit/merge/reply)
./polecat_wrapper.py --dry-run "Test the wrapper"

# Verbose output
./polecat_wrapper.py -v "Debug this task"

# JSON output
./polecat_wrapper.py --json "Get structured result"
```

### polecat_wrapper.sh (Simpler alternative)

Bash script for environments without Python:
- Fewer features but no dependencies
- Color-coded output
- Basic jq-based JSON parsing

```bash
# Basic usage
./polecat_wrapper.sh "Implement OAuth2 login"

# With lite mode
./polecat_wrapper.sh --lite "Fix typo in README"

# Execute beads issue
./polecat_wrapper.sh --issue vc-abc

# Dry run
./polecat_wrapper.sh --dry-run "Test the wrapper"
```

## Lite Mode Heuristics

The Python wrapper automatically detects simple tasks that benefit from lite mode:

**Keywords triggering lite mode:**
- typo, fix comment, update readme, rename
- fix spelling, add comment, remove comment
- whitespace, formatting, capitalize, punctuation

**Short tasks (<50 chars) use lite mode unless they contain:**
- implement, refactor, redesign, integrate
- migrate, add feature, security, authentication

Override with `--lite` to force lite mode.

## Integration with Gastown

### Claude Code Hook

Add to your polecat's `.claude/hooks/pre-prompt.sh`:

```bash
#!/bin/bash
# Route complex tasks through VC

task="$1"

# Simple heuristic: multi-file or complex tasks use VC
if echo "$task" | grep -qEi "(implement|refactor|add feature|fix bug)"; then
    echo "Routing to VC for quality-assured execution..."
    /path/to/polecat_wrapper.py "$task"
    exit 0
fi

# Simple tasks go direct to Claude Code
exit 1
```

### GM Message Handler

In your polecat's message handler:

```python
#!/usr/bin/env python3
import subprocess
import sys

def handle_gm_message(message_id, sender, subject, body):
    """Handle incoming Gastown Mail message."""
    # Check if this is a work assignment
    if subject.startswith("Work:"):
        task = body
        subprocess.run([
            '/path/to/polecat_wrapper.py',
            '--message-id', message_id,
            task
        ])
```

## Output Format

VC outputs JSON to stdout. Example:

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
  "discovered_issues": [
    {
      "title": "Add rate limiting to OAuth endpoints",
      "description": "OAuth endpoints should have rate limiting",
      "type": "task",
      "priority": 2
    }
  ],
  "punted_items": ["Password reset flow - requires email service"],
  "summary": "Implemented OAuth2 login with Google and GitHub providers."
}
```

## Status Values

- **completed** - Task done, all quality gates passed
- **partial** - Some work done but incomplete
- **blocked** - Cannot proceed (baseline failures, unclear requirements)
- **failed** - Execution failed (gates failed, agent crashed)
- **decomposed** - Task was too large, subtasks created

## Requirements

### Python wrapper
- Python 3.7+
- vc command in PATH
- bd command in PATH (for issue creation)
- gm command in PATH (for GM replies, optional)

### Shell wrapper
- Bash 4.0+
- jq (for JSON parsing)
- vc command in PATH
- bd command in PATH (for issue creation)
- git (for commit/merge operations)

## See Also

- [GASTOWN_INTEGRATION.md](../../docs/design/GASTOWN_INTEGRATION.md) - Full design specification
- [FEATURES.md](../../docs/FEATURES.md) - VC feature documentation
