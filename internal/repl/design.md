# REPL Design Document

## Overview
Interactive shell for directing VC through natural language. The "VibeCoder Primitive" - user says what they want, AI translates to issues, executor runs them.

## Architecture

```
┌─────────────┐
│  User Input │
└──────┬──────┘
       │
       v
┌─────────────────┐
│  REPL Loop      │  ← Read-Eval-Print Loop
│  - Readline     │
│  - History      │
│  - Completion   │
└──────┬──────────┘
       │
       v
┌─────────────────────┐
│  Command Router     │
└──────┬──────────────┘
       │
       ├─→ [Special Commands]
       │   - continue/let's continue
       │   - status
       │   - help
       │   - exit/quit
       │
       └─→ [Natural Language]
           │
           v
       ┌──────────────────┐
       │  AI Translator   │ ← Uses AI Supervisor
       │  - Parse intent  │
       │  - Create issues │
       │  - Set deps      │
       └──────┬───────────┘
              │
              v
       ┌──────────────┐
       │   Storage    │
       └──────────────┘
```

## Components

### 1. REPL Shell (`internal/repl/repl.go`)
- Input loop with readline support
- Command history (in-memory, could persist later)
- Colored output for better UX
- Context-aware tab completion

### 2. Command Router (`internal/repl/router.go`)
- Parse input to detect special commands
- Route to appropriate handler
- Fall through to AI translator for natural language

### 3. AI Translator (`internal/repl/translator.go`)
- Use Anthropic API (like AI Supervisor)
- Translate natural language to structured issue definitions
- Support patterns like:
  - "Add a login page" → feature issue
  - "Fix the bug in auth.go" → bug issue
  - "Build user auth system" → epic with child tasks
  - "Refactor the database layer" → epic with subtasks

### 4. Status Display (`internal/repl/status.go`)
- Show ready work
- Show in-progress issues
- Show blocked issues
- Show recent activity (from events)

### 5. Continue Handler (`internal/repl/continue.go`)
- Find ready work
- Show user what will be executed
- Optionally: start executor or spawn single agent
- Display activity feed while running

## Commands

### Special Commands
- `continue` or `let's continue` - Resume execution from tracker state
- `status` - Show current project state
- `ready` - Show ready work
- `blocked` - Show blocked issues
- `help` - Show available commands
- `exit` or `quit` - Exit REPL
- `history` - Show command history

### Natural Language Examples
```
> Add a login page with email and password
  → Creates feature issue "Add login page"
  → AI breaks into subtasks if needed

> Fix the bug where users can't logout
  → Creates bug issue "Fix logout bug"

> Build a complete authentication system
  → Creates epic "Authentication system"
  → AI breaks into: login, logout, registration, password reset, etc.

> let's continue
  → Finds ready work
  → Shows user what's available
  → Starts executor or spawns agent
```

## Implementation Plan

### Phase 1: Basic REPL (vc-9.1)
- [ ] REPL loop with readline
- [ ] Basic command parsing
- [ ] exit/quit commands
- [ ] help command
- [ ] Command history

### Phase 2: Status Display (vc-9.2)
- [ ] status command showing ready/blocked/in-progress
- [ ] ready command
- [ ] blocked command
- [ ] Integration with storage.GetReadyWork()

### Phase 3: AI Translator (vc-9.3)
- [ ] AI translation service
- [ ] Create issues from natural language
- [ ] Handle different issue types (bug/feature/epic/task)
- [ ] Auto-create epic subtasks for complex requests

### Phase 4: Continue Command (vc-9.4)
- [ ] continue command implementation
- [ ] Show ready work to user
- [ ] Options:
  - Option A: Start executor in background
  - Option B: Execute single issue interactively
  - Option C: Both (user choice)
- [ ] Activity feed display

### Phase 5: Polish (vc-9.5)
- [ ] Tab completion for commands
- [ ] Colored output
- [ ] Better error messages
- [ ] Persist history to ~/.vc/repl_history
- [ ] Ctrl+C handling (don't exit on single Ctrl+C)

## Dependencies

### Go Libraries
- `github.com/chzyer/readline` - Full-featured readline
- `github.com/fatih/color` - Already used in project
- `github.com/spf13/cobra` - Already used for CLI

### Internal
- `internal/storage` - Issue CRUD
- `internal/ai` - AI Supervisor for translation
- `internal/executor` - For continue command
- `internal/types` - Data structures

## Testing Strategy
- Unit tests for command parsing
- Integration tests for AI translation
- Mock AI responses for deterministic tests
- Manual testing for UX/interaction

## Success Metrics
- Can create issues via natural language ✓
- "let's continue" finds and shows ready work ✓
- Activity visible while agent runs ✓
- Good UX (colored output, history, help) ✓
- Integrates with existing executor ✓

## Notes
- Start simple: basic REPL first, then add AI
- Activity feed (vc-1) not yet implemented - stub for now
- Focus on core user journey: input → issues → continue → done
- Can expand with more commands later (show, list, etc.)
