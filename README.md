# VC - VibeCoder v2

AI-orchestrated coding agent colony. Built on lessons learned from 350k LOC TypeScript prototype.

## Vision

> Build a colony of coding agents, not the world's largest ant.

VC orchestrates multiple coding agents (Amp, Claude Code, etc.) to work on small, well-defined tasks, guided by AI supervision. This keeps agents focused, improves quality, and minimizes context window costs.

## Core Principles

**Zero Framework Cognition**: All decisions delegated to AI. No heuristics, regex, or parsing.

**Issue-Oriented Orchestration**: Work tracked in SQLite issue tracker with dependency awareness.

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
Worker Agents (Amp, Claude Code)
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

**Phase**: Production (Dogfooding)

**Tracker**: Beads v0.12.0 (SQLite) - see `.beads/vc.db`

**Progress**:
- ✅ Bootstrap complete - All 5 phases done
- ✅ 254 issues closed through dogfooding
- ✅ 24 successful missions with 90.9% quality gate pass rate
- ✅ Core workflow operational and self-improving

**Next**: Check ready work with `bd ready` (see CLAUDE.md for details)

## Quick Start

```bash
# Set up environment
export ANTHROPIC_API_KEY=your-key-here

# Build and run
go build -o vc ./cmd/vc
./vc

# Talk to VC naturally:
vc> What's ready to work on?
vc> Let's continue working
vc> Add a feature for CSV export
vc> Show me what's blocked
vc> How's the project doing?
```

The REPL provides a pure conversational interface - no commands to memorize. The AI understands your intent and uses the appropriate tools to help you manage work.

### Example Conversations

**Starting work:**
```
You: What's ready to work on?
AI: [Shows 3 ready issues with priorities]
You: Let's work on the first one
AI: [Starts execution on vc-123]
```

**Creating issues:**
```
You: We need Docker support
AI: [Creates feature issue vc-145]
You: Make that priority 0
AI: [Updates priority]
You: Now work on it
AI: [Starts execution]
```

**Monitoring progress:**
```
You: How's the project doing?
AI: [Shows 50 total, 12 ready, 3 blocked, 22 closed]
You: What's blocking us?
AI: [Lists blocked issues with blocker details]
```

**Context-aware:**
```
You: Add user authentication
AI: [Creates epic vc-200]
You: Break that into login, registration, and password reset
AI: [Creates 3 child tasks]
You: Link them to the epic
AI: [Adds dependencies]
```

## Documentation

### Core Docs
- `ARCHITECTURE.md` - System architecture and implementation details
- `CLAUDE.md` - Instructions for AI agents working on VC (comprehensive guide)
- `DOGFOODING.md` - Dogfooding workflow and mission logs

### Implementation Details
- `docs/ARCHITECTURE_AUDIT.md` - Comprehensive implementation review
- `docs/EXPLORATION_FINDINGS.md` - Current state analysis
- `docs/architecture/` - Detailed design documents (MISSIONS, BEADS, etc.)

### Historical
- `docs/archive/BOOTSTRAP.md` - Original 2-week roadmap (completed)

## Key Achievements

1. ✅ **AI Supervised Issue Workflow** - Proven through 24+ dogfooding missions
2. ✅ **Beads Integration** - 100x performance improvement over shell-based CLI
3. ✅ **Self-Hosting** - System successfully builds and improves itself
4. ✅ **Quality Gates** - 90.9% pass rate prevents broken code
5. ✅ **Zero Framework Cognition** - AI makes all decisions, no heuristics
6. ✅ **Sandbox Isolation** - Git worktrees enable safe concurrent execution

## Lessons from V1 (TypeScript Prototype)

1. ✅ AI Supervised Issue Workflow worked brilliantly → **Reimplemented in Go**
2. ✅ SQLite issue tracker is simple and lightweight → **Now using Beads library**
3. ✅ Issue-oriented orchestration enabled self-hosting → **Core principle validated**
4. ❌ Temporal was too heavyweight → **Removed, using simpler event loop**
5. ❌ Built auxiliary systems too early → **Tracer bullet approach this time**
6. ❌ TypeScript ecosystem challenges → **Go provides better AI code quality**

## License

MIT
