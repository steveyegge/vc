# VC - VibeCoder v2

AI-orchestrated coding agent colony. Built on lessons learned from 350k LOC TypeScript prototype.

## Vision

> Build a colony of coding agents, not the world's largest ant.

VC orchestrates multiple coding agents (Cody, Claude Code, etc.) to work on small, well-defined tasks, guided by AI supervision. This keeps agents focused, improves quality, and minimizes context window costs.

## Core Principles

**Zero Framework Cognition**: All decisions delegated to AI. No heuristics, regex, or parsing.

**Issue-Oriented Orchestration**: Work tracked in SQLite/PostgreSQL issue tracker with dependency awareness.

**Nondeterministic Idempotence**: Workflows can be interrupted and resumed - AI figures out where it left off.

**Tracer Bullet Development**: Get end-to-end basics working before adding bells and whistles.

## Architecture

```
VC Shell (REPL)
    ↓
AI Supervisor (Sonnet 4.5)
    ↓
Issue Workflow Executor (event loop)
    ↓
Worker Agents (Cody, Claude Code)
    ↓
Code Changes
```

## The AI Supervised Issue Workflow

```
Loop {
  1. Claim ready issue (atomic SQL)
  2. AI Assessment: strategy, steps, risks
  3. Execute via agent
  4. AI Analysis: extract punted work, bugs
  5. Auto-create discovered issues
  6. Quality gates (test, lint, build)
  7. AI decides: close, partial, or blocked
}
```

## Status

**Phase**: Early bootstrap (porting from TypeScript vibecoder)

**Tracker**: Beads (SQLite) - see `.beads/vc.db`

**Next**: Check ready work with `/workspace/beads/bd ready`

## Quick Start

```bash
# Build
go build -o vc ./cmd/vc

# View work
./vc ready

# Start executor (when ready)
./vc execute --epic vc-1
```

## Documentation

- `BOOTSTRAP.md` - Bootstrap roadmap and phase tracking
- `DESIGN.md` - Architecture and key decisions (TODO)
- `~/src/vc/zoey/vc/` - TypeScript prototype (reference)

## Lessons from V1

1. ✅ AI Supervised Issue Workflow worked brilliantly
2. ✅ PostgreSQL issue tracker was simple and scalable
3. ✅ Issue-oriented orchestration enabled self-hosting
4. ❌ Temporal was too heavyweight for individual dev tool
5. ❌ Built auxiliary systems before core workflow proved out
6. ❌ TypeScript ecosystem and AI code quality issues

## License

MIT
