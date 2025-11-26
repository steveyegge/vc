#!/usr/bin/env python3
"""
Polecat Wrapper for Gastown Integration (vc-6gp7)

This script demonstrates how to invoke VC in polecat mode from within a
Gastown polecat. It handles:
- Receiving tasks via CLI or Gastown Mail
- Invoking vc exec --polecat-mode
- Parsing JSON results
- Creating discovered issues in beads
- Git operations (commit, merge)
- Replying via gm with status

Usage:
    # Direct invocation with task
    ./polecat_wrapper.py "Implement OAuth2 login"

    # With issue ID instead of task description
    ./polecat_wrapper.py --issue vc-123

    # From stdin (for longer task descriptions)
    cat task.txt | ./polecat_wrapper.py --stdin

    # With lite mode for simple tasks
    ./polecat_wrapper.py --lite "Fix typo in README"

See docs/design/GASTOWN_INTEGRATION.md Section 7 for full specification.
"""

import argparse
import json
import os
import subprocess
import sys
from dataclasses import dataclass
from typing import List, Optional, Dict, Any


@dataclass
class PolecatResult:
    """Parsed result from VC polecat mode execution."""
    status: str  # completed, partial, blocked, failed, decomposed
    success: bool
    iterations: int
    converged: bool
    duration_seconds: float
    files_modified: List[str]
    quality_gates: Dict[str, Any]
    discovered_issues: List[Dict[str, Any]]
    punted_items: List[str]
    summary: str
    error: Optional[str] = None
    message: Optional[str] = None
    decomposition: Optional[Dict[str, Any]] = None

    @classmethod
    def from_json(cls, data: Dict[str, Any]) -> 'PolecatResult':
        """Parse VC JSON output into PolecatResult."""
        return cls(
            status=data.get('status', 'unknown'),
            success=data.get('success', False),
            iterations=data.get('iterations', 0),
            converged=data.get('converged', False),
            duration_seconds=data.get('duration_seconds', 0.0),
            files_modified=data.get('files_modified', []),
            quality_gates=data.get('quality_gates', {}),
            discovered_issues=data.get('discovered_issues', []),
            punted_items=data.get('punted_items', []),
            summary=data.get('summary', ''),
            error=data.get('error'),
            message=data.get('message'),
            decomposition=data.get('decomposition'),
        )


def should_use_lite_mode(task: str) -> bool:
    """
    Heuristic to determine if a task should use lite mode.

    Lite mode skips:
    - Preflight checks (assume baseline is clean)
    - AI assessment (task is simple enough)
    - Multiple iterations (single pass is sufficient)

    Quality gates still run in lite mode.

    Args:
        task: The task description

    Returns:
        True if lite mode should be used
    """
    task_lower = task.lower()

    # Keywords that indicate simple tasks
    lite_keywords = [
        'typo',
        'fix comment',
        'update readme',
        'rename',
        'fix spelling',
        'add comment',
        'remove comment',
        'whitespace',
        'formatting',
        'capitalize',
        'punctuation',
    ]

    for keyword in lite_keywords:
        if keyword in task_lower:
            return True

    # Very short tasks are likely simple
    if len(task) < 50:
        # But not if they have complexity indicators
        complexity_indicators = [
            'implement',
            'refactor',
            'redesign',
            'integrate',
            'migrate',
            'add feature',
            'security',
            'authentication',
        ]
        for indicator in complexity_indicators:
            if indicator in task_lower:
                return False
        return True

    return False


def run_vc(
    task: str,
    issue_id: Optional[str] = None,
    from_stdin: bool = False,
    lite_mode: bool = False,
    force_lite: bool = False,
    verbose: bool = False,
) -> PolecatResult:
    """
    Execute VC in polecat mode.

    Args:
        task: The task description (ignored if issue_id is provided)
        issue_id: Optional beads issue ID to execute
        from_stdin: If True, read task from stdin
        lite_mode: If True, use lite mode
        force_lite: If True, always use lite mode (override heuristics)
        verbose: If True, print debug info

    Returns:
        PolecatResult with execution outcome

    Raises:
        RuntimeError: If VC execution fails
    """
    cmd = ['vc', 'exec', '--polecat-mode']

    # Determine if we should use lite mode
    use_lite = force_lite or lite_mode
    if not use_lite and task:
        use_lite = should_use_lite_mode(task)

    if use_lite:
        cmd.append('--lite')

    if issue_id:
        cmd.extend(['--issue', issue_id])
    elif from_stdin:
        cmd.append('--stdin')
    else:
        cmd.extend(['--task', task])

    if verbose:
        print(f"Running: {' '.join(cmd)}", file=sys.stderr)

    # Run VC
    try:
        if from_stdin:
            result = subprocess.run(
                cmd,
                input=task,
                capture_output=True,
                text=True,
            )
        else:
            result = subprocess.run(
                cmd,
                capture_output=True,
                text=True,
            )
    except FileNotFoundError:
        raise RuntimeError("vc command not found. Is VC installed and in PATH?")

    # Log stderr (progress info)
    if verbose and result.stderr:
        print(result.stderr, file=sys.stderr)

    # Check for execution failure (non-JSON output)
    if result.returncode != 0 and not result.stdout.strip().startswith('{'):
        raise RuntimeError(f"VC execution failed: {result.stderr}")

    # Parse JSON result from stdout
    try:
        vc_output = json.loads(result.stdout)
    except json.JSONDecodeError as e:
        raise RuntimeError(f"Failed to parse VC output as JSON: {e}\nOutput: {result.stdout[:500]}")

    return PolecatResult.from_json(vc_output)


def create_discovered_issues(result: PolecatResult, verbose: bool = False) -> List[str]:
    """
    Create beads issues for discovered work.

    Args:
        result: The PolecatResult containing discovered issues
        verbose: If True, print debug info

    Returns:
        List of created issue IDs
    """
    created_ids = []

    for issue in result.discovered_issues:
        title = issue.get('title', 'Discovered issue')
        description = issue.get('description', '')
        issue_type = issue.get('type', 'task')
        priority = issue.get('priority', 2)

        cmd = [
            'bd', 'create',
            '--title', title,
            '--type', issue_type,
            '--priority', str(priority),
            '--label', 'discovered:related',
        ]

        if description:
            cmd.extend(['--description', description])

        if verbose:
            print(f"Creating issue: {title}", file=sys.stderr)

        try:
            result = subprocess.run(
                cmd,
                capture_output=True,
                text=True,
            )

            # Extract issue ID from output (bd create outputs "Created issue: vc-xyz")
            if result.returncode == 0:
                output = result.stdout.strip()
                if 'Created' in output or 'vc-' in output:
                    # Try to extract ID
                    import re
                    match = re.search(r'(vc-[a-z0-9]+)', output)
                    if match:
                        created_ids.append(match.group(1))
        except FileNotFoundError:
            print("Warning: bd command not found, skipping issue creation", file=sys.stderr)

    return created_ids


def commit_and_merge(task: str, result: PolecatResult, verbose: bool = False) -> bool:
    """
    Commit changes to local branch and merge to main.

    This is a simplified example. In production, you'd want more
    robust error handling and potentially PR-based workflows.

    Args:
        task: The task description (for commit message)
        result: The PolecatResult (for files to commit)
        verbose: If True, print debug info

    Returns:
        True if merge succeeded
    """
    if not result.files_modified:
        if verbose:
            print("No files modified, skipping commit", file=sys.stderr)
        return True

    # Stage all changes
    subprocess.run(['git', 'add', '-A'], check=True)

    # Create commit message
    commit_msg = f"VC: {task[:50]}"
    if len(task) > 50:
        commit_msg += "..."

    # Commit
    try:
        subprocess.run(
            ['git', 'commit', '-m', commit_msg],
            check=True,
            capture_output=True,
        )
    except subprocess.CalledProcessError as e:
        if b'nothing to commit' in e.stderr:
            return True
        raise

    # Get current branch
    current = subprocess.run(
        ['git', 'rev-parse', '--abbrev-ref', 'HEAD'],
        capture_output=True,
        text=True,
    ).stdout.strip()

    # Switch to main
    subprocess.run(['git', 'checkout', 'main'], check=True, capture_output=True)

    # Merge
    try:
        subprocess.run(
            ['git', 'merge', current],
            check=True,
            capture_output=True,
        )
    except subprocess.CalledProcessError:
        # Merge conflict - stay on main but report failure
        print("Warning: Merge conflict, manual resolution needed", file=sys.stderr)
        subprocess.run(['git', 'checkout', current])
        return False

    # Push to origin
    try:
        subprocess.run(
            ['git', 'push', 'origin', 'main'],
            check=True,
            capture_output=True,
        )
    except subprocess.CalledProcessError as e:
        print(f"Warning: Push failed: {e}", file=sys.stderr)

    # Return to polecat branch
    subprocess.run(['git', 'checkout', current], capture_output=True)

    return True


def send_gm_reply(message_id: Optional[str], status: str, summary: str):
    """
    Send reply via Gastown Mail.

    Args:
        message_id: The message ID to reply to (if any)
        status: The completion status
        summary: A brief summary of what was done
    """
    if not message_id:
        return

    try:
        subprocess.run(
            ['gm', 'reply', message_id, f"Task {status}: {summary}"],
            capture_output=True,
        )
    except FileNotFoundError:
        # gm not installed, skip
        pass


def handle_task(
    task: str,
    issue_id: Optional[str] = None,
    from_stdin: bool = False,
    lite_mode: bool = False,
    message_id: Optional[str] = None,
    dry_run: bool = False,
    verbose: bool = False,
) -> PolecatResult:
    """
    Execute a task via VC and handle the results.

    This is the main orchestration function that:
    1. Runs VC in polecat mode
    2. Creates discovered issues in beads
    3. Commits and merges if successful
    4. Replies via gm with status

    Args:
        task: The task description
        issue_id: Optional beads issue ID
        from_stdin: If True, task came from stdin
        lite_mode: If True, use lite mode
        message_id: Optional gm message ID to reply to
        dry_run: If True, don't commit/merge/reply
        verbose: If True, print debug info

    Returns:
        The PolecatResult from VC execution
    """
    # Step 1: Run VC
    result = run_vc(
        task=task,
        issue_id=issue_id,
        from_stdin=from_stdin,
        lite_mode=lite_mode,
        verbose=verbose,
    )

    if verbose:
        print(f"\nVC Result: status={result.status}, success={result.success}", file=sys.stderr)
        print(f"Files modified: {result.files_modified}", file=sys.stderr)
        print(f"Discovered issues: {len(result.discovered_issues)}", file=sys.stderr)

    # Step 2: Create discovered issues
    if result.discovered_issues and not dry_run:
        created_ids = create_discovered_issues(result, verbose=verbose)
        if verbose and created_ids:
            print(f"Created issues: {', '.join(created_ids)}", file=sys.stderr)

    # Step 3: Commit and merge if successful
    merged = False
    if result.success and result.status == 'completed' and not dry_run:
        merged = commit_and_merge(task, result, verbose=verbose)
        if verbose:
            print(f"Merge {'succeeded' if merged else 'failed'}", file=sys.stderr)

    # Step 4: Reply via gm
    if message_id and not dry_run:
        status = 'completed' if result.success else 'failed'
        send_gm_reply(message_id, status, result.summary or 'No summary available')

    return result


def main():
    """Main entry point."""
    parser = argparse.ArgumentParser(
        description='Polecat wrapper for VC integration with Gastown',
        epilog='See docs/design/GASTOWN_INTEGRATION.md for full specification.',
    )

    parser.add_argument(
        'task',
        nargs='?',
        help='Task description (use --stdin to read from stdin instead)',
    )
    parser.add_argument(
        '--issue', '-i',
        metavar='ID',
        help='Execute a beads issue by ID instead of task description',
    )
    parser.add_argument(
        '--stdin',
        action='store_true',
        help='Read task from stdin (for longer descriptions)',
    )
    parser.add_argument(
        '--lite', '-l',
        action='store_true',
        help='Use lite mode (skip preflight and assessment)',
    )
    parser.add_argument(
        '--message-id', '-m',
        metavar='ID',
        help='Gastown Mail message ID to reply to',
    )
    parser.add_argument(
        '--dry-run', '-n',
        action='store_true',
        help="Don't commit, merge, or send gm replies",
    )
    parser.add_argument(
        '--verbose', '-v',
        action='store_true',
        help='Print verbose debug output',
    )
    parser.add_argument(
        '--json',
        action='store_true',
        help='Output result as JSON',
    )

    args = parser.parse_args()

    # Validate arguments
    if args.stdin:
        task = sys.stdin.read()
    elif args.issue:
        task = None
    elif args.task:
        task = args.task
    else:
        parser.error('Either task, --issue, or --stdin is required')
        return 1

    try:
        result = handle_task(
            task=task or '',
            issue_id=args.issue,
            from_stdin=args.stdin,
            lite_mode=args.lite,
            message_id=args.message_id,
            dry_run=args.dry_run,
            verbose=args.verbose,
        )

        # Output
        if args.json:
            print(json.dumps({
                'status': result.status,
                'success': result.success,
                'summary': result.summary,
                'files_modified': result.files_modified,
                'discovered_issues': len(result.discovered_issues),
            }, indent=2))
        else:
            print(f"\nStatus: {result.status}")
            print(f"Success: {result.success}")
            print(f"Duration: {result.duration_seconds:.1f}s")
            print(f"Iterations: {result.iterations}")
            if result.summary:
                print(f"Summary: {result.summary}")
            if result.files_modified:
                print(f"Files modified: {len(result.files_modified)}")
            if result.discovered_issues:
                print(f"Discovered issues: {len(result.discovered_issues)}")
            if result.error:
                print(f"Error: {result.error}")

        return 0 if result.success else 1

    except RuntimeError as e:
        print(f"Error: {e}", file=sys.stderr)
        return 1
    except KeyboardInterrupt:
        print("\nInterrupted", file=sys.stderr)
        return 130


if __name__ == '__main__':
    sys.exit(main())
