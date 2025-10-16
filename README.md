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
