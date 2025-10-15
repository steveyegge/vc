# AI Agent Instructions for VC

**See [CLAUDE.md](CLAUDE.md) for complete documentation.**

## Quick Start: "What's Next?"

VC uses **Beads for issue tracking**. When starting a session or asked "what's next":

```bash
bd ready              # Show ready work (no blockers)
bd show vc-X          # View issue details
bd update vc-X --status in_progress  # Claim the work
```

## Common Commands

```bash
# Finding work
bd ready                    # Ready issues
bd list --status open       # All open issues
bd show vc-X                # Issue details with dependencies

# Working on issues
bd update vc-X --status in_progress
bd update vc-X --notes "Progress update"
bd close vc-X --reason "Completed"

# Before committing
bd export -o .beads/issues.jsonl
git add .beads/issues.jsonl
```

## Key Facts

- **Issue tracker database**: `.beads/vc.db` (local cache)
- **Source of truth**: `.beads/issues.jsonl` (commit this to git)
- **Beads location**: `~/src/beads/bd` (local) or `/workspace/beads/bd` (GCE VM)
- **Current phase**: Bootstrap - building the AI-supervised workflow in Go
- **Start here**: Epic vc-5 (Beads Integration and Executor Tables)

For full details on architecture, workflow, principles, and troubleshooting, see [CLAUDE.md](CLAUDE.md).
