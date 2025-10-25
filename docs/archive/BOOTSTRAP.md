# VC Bootstrap Roadmap

**Status**: ✅ COMPLETE (as of October 2025)
**Archived**: This document represents the original 2-week roadmap. All phases are now complete.
**Current Tracking**: Work is tracked in `.beads/issues.jsonl` and the Beads database.

---

Tracer bullet approach: Get end-to-end workflow working before expanding.

**Historical Context**: This was the original bootstrap plan. The actual implementation exceeded this plan with additional features like deduplication, watchdog, health monitoring, and comprehensive testing infrastructure.

## Phase 1: Beads Integration (1-2 days)

**Goal**: Extend Beads with VC-specific fields and executor tables

- [ ] Add `discovered-from` dependency type
- [ ] Add `design`, `acceptance_criteria`, `notes` fields to issues
- [ ] Add `executor_instances` table for orchestration
- [ ] Add `issue_execution_state` table for checkpointing
- [ ] Ensure PostgreSQL backend works (keep SQLite default)

**Deliverable**: Enhanced Beads that supports full VC workflow

## Phase 2: Issue Processor (No AI) (2 days)

**Goal**: Event loop that claims and executes issues via agents

- [ ] Port IssueWorkflowExecutor (atomic claiming with FOR UPDATE SKIP LOCKED)
- [ ] Spawn coding agent (Amp/Claude Code) with `--stream-json` for execution
- [ ] Parse agent output and update issue status
- [ ] Handle epic completion detection
- [ ] Support pause/resume/abort

**Deliverable**: `vc execute` runs event loop and completes issues (no AI supervision yet)

## Phase 3: AI Supervision (2-3 days)

**Goal**: Add AI assessment and analysis loops

- [ ] Integrate Anthropic Go SDK (Sonnet 4.5)
- [ ] Implement `assessIssueState` (before execution)
- [ ] Implement `analyzeExecutionResult` (after execution)
- [ ] Auto-create discovered issues from AI analysis
- [ ] Log AI confidence and reasoning

**Deliverable**: AI reviews every task, extracts hidden work

## Phase 4: Quality Gates (1 day)

**Goal**: Enforce quality standards before closing issues

- [ ] Add `go test` gate
- [ ] Add `golangci-lint` gate
- [ ] Add `go build` gate
- [ ] Create blocking issues on gate failures

**Deliverable**: Quality gates prevent broken code from being marked complete

## Phase 5: REPL Shell (1-2 days)

**Goal**: Interactive shell for directing VC

- [ ] Simple `vc` command with chat interface
- [ ] Translate user requests into issues (via AI)
- [ ] Show activity feed of agent work
- [ ] "Let's continue" resumes from tracker state

**Deliverable**: User can have natural conversation to drive development

## Total Timeline

**~2 weeks to working prototype**

## Success Criteria

- [ ] User creates epic via REPL
- [ ] AI breaks epic into child issues
- [ ] Executor claims and processes issues
- [ ] AI supervises each execution
- [ ] Quality gates enforce standards
- [ ] User sees progress in activity feed
- [ ] System recovers from crashes (nondeterministic idempotence)

## Post-Bootstrap

After basics work:

- Swarm support (parallel workers)
- Web portal (rich visualizations)
- Event streaming (detailed activity feed)
- Watchdog monitoring (convergence detection)
- Rich metrics and dashboards
