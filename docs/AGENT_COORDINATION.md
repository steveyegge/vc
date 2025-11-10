# Agent Coordination Problem Space

**Date:** 2025-11-09 (updated 2025-11-10)
**Status:** Planning / Research
**Context:** Exploring coordination layer for multi-agent workflows

## The Problem Reframed

After initial analysis, we've clarified the core use case: **cross-project joint debugging** for agents working across related codebases.

### Two Customer Shapes

**1. Claude Code Users (Human + Agent Sessions)**
- Humans using agents (Claude Code, etc.) for coding
- Working in various projects (VC, Beads, Wyvern, random projects)
- **Primary need:** When agent in Project A discovers a bug in Project B (dependency), need to hand off context to agent in Project B
- Example: VC agent encounters Beads bug → needs to collaborate with Beads agent to debug

**2. VC as Orchestrator**
- VC uses Beads as a library (SQLite-backed issue storage)
- VC coordinates its own workers internally (supervised execution)
- When VC discovers a Beads bug, it's Case #1 (cross-project debugging)
- **Note:** Beads is NOT an orchestrator, just a storage layer

### The Core Use Case: Cross-Project Joint Debugging

**Concrete Scenario:**

```
Step 1: Discovery
- Human working in ~/src/vc with Claude Code agent
- VC agent encounters a Beads bug (parser crash, edge case)
- Agent has: repro case, stack trace, context, hypotheses

Step 2: Handoff
- VC agent needs to communicate with Beads project
- Must transfer: repro case, error details, context
- Beads project is in ~/src/beads (different workspace, same machine)

Step 3: Joint Investigation
- Human cd's to ~/src/beads, starts Beads agent
- Beads agent receives the context (async via inbox)
- Beads agent investigates, may reply with questions
- VC agent may still be online to answer (or async via inbox)

Step 4: Resolution
- Beads agent fixes the bug
- VC agent (or human) verifies fix works
- VC can continue original work
```

**Key Insight:** This is like GitHub issue workflow, but:
- Agents file issues for each other (with full context)
- Agents may be online to respond (live conversation)
- Or agents work async (check inbox, leave replies)
- Both sync and async must work seamlessly

### Why This Matters

**Without coordination:**
- Human manually copy/pastes error, context between sessions
- Context gets lost or incomplete
- Back-and-forth is slow and error-prone
- Agent restarts lose conversation history

**With coordination:**
- VC agent sends structured message to Beads project
- Beads agent gets full context immediately
- Conversation thread preserved across sessions
- Human can switch contexts without loss

## What We Know

### MCP Agent Mail Investigation

**Repository:** `~/src/mcp_agent_mail` (by Dicklesworthstone)
**Architecture:**
- Python-based coordination server
- SQLite storage (`storage.sqlite3`) for messages, agents, file reservations
- Git-based artifact archiving (human-auditable message history)
- MCP server integration (port 8765)
- Inbox/outbox metaphor for async messaging

**What It Provides:**
- ✅ Agent identity and registration
- ✅ Message passing (inbox/outbox, threading, search)
- ✅ File reservation "leases" (advisory locks on files/globs)
- ✅ Searchable message history
- ✅ MCP tool integration for coding agents
- ✅ Git-backed artifacts for human audit trail

**Integration Test Results:**
- Initial tests showed 98% reduction in git operations
- Promising for reducing coordination overhead

**The Blocker:**

MCP Agent Mail's project identity model doesn't fit our use case:

```python
# MCP Agent Mail (current):
Project Identity = Absolute Workspace Path

~/src/dave/vc  → project "users-stevey-src-dave-vc"
~/src/fred/vc  → project "users-stevey-src-fred-vc"
# These are DIFFERENT projects (agents can't coordinate)

# What VC Needs:
Project Identity = Git Remote URL

~/src/dave/vc  → git@github.com:steveyegge/vc.git → project "vc"
~/src/fred/vc  → git@github.com:steveyegge/vc.git → project "vc"
# These are the SAME project (agents can coordinate)
```

**Quote from author:** "The author assumed all the agents would be working in the same repo clone, which is an unusual workflow that won't work for most people, since the agents need exclusive access to their worktree for running e.g. builds."

**Cross-Project Support:**
- MCP Agent Mail has "cross-project coordination" features (planned/partial)
- But "two workers on the same GitHub project in different repos are considered different projects"
- This is by design for their use case, but wrong for ours

### Architectural Observations

**Storage:**
- Both MCP Agent Mail and VC use SQLite for state
- Git is used for artifacts/audit trail, not primary coordination
- Database-backed coordination is dramatically faster than git-based

**Identity Schemes:**

Path-based agent naming (from workspace path) is elegant:
- `dave/vc` immediately tells humans "dave's workspace, vc project"
- No manual registration ceremony needed
- Natural mapping from filesystem layout

But needs project identity decoupled from workspace path:
- Project = git remote URL (logical project)
- Agent = workspace path (physical workspace)
- Multiple agents can work on same project from different workspaces

**Key Primitives Needed (Revised):**

After clarifying the use cases, we need:

1. **Cross-Project Messaging** (primary use case)
   - Agent in Project A sends message to Project B
   - Persistent inbox (survives agent restarts)
   - Async by default (agent may be offline)
   - Optional sync (if receiving agent is online)
   - Message threading (conversation about a bug)
   - Attachments (repro cases, logs, diffs)

2. **Project Identity & Discovery**
   - Projects identified by git remote URL (not workspace path)
   - Multiple workspaces of same git remote = same project
   - Agent discovery: how does VC workspace find Beads endpoint?
   - Agent naming: `dave/vc` format (workspace/project)

3. **Issue Integration** (critical)
   - Messages should create/attach to issues
   - Issue tracker is source of truth
   - Messages are like GitHub issue comments
   - Work lifecycle tied to issues, not just messages

4. **Agent Lifecycle** (online/offline)
   - Agents register when starting (workspace + project)
   - Agents can be online (respond immediately)
   - Or offline (check inbox on next start)
   - Other agents can query: "Is beads agent online?"

5. **Within-Project Coordination** (VC-specific, lower priority)
   - Atomic issue claiming (for VC's multiple workers)
   - File reservations (advisory locks)
   - Worker heartbeat (orphan detection)
   - Event broadcasting (activity feed)

## Metaphor Analysis: Is "Mail" Right?

**Revised after clarifying use cases:**

For **cross-project joint debugging**, the mail metaphor is actually quite good:
- ✅ Async, persistent messages (agent may be offline)
- ✅ Inbox/outbox (check messages when starting session)
- ✅ Threading (conversation about a bug)
- ✅ Attachments (repro cases, logs, diffs)
- ✅ Like GitHub issues/comments workflow

For **within-project VC coordination** (multiple VC workers), mail is less appropriate:
- ❌ Not really messaging, more like atomic state transitions
- ❌ Workers don't "send messages", they claim issues from queue
- ❌ More like distributed task queue than email

**Conclusion:**
- **Standalone coordination service** for cross-project messaging (mail model fits)
- **VC-specific coordination** (within project) can be simpler, may not need full messaging

## The VC Context: Orchestrator Design

**VC is fundamentally different from typical multi-agent systems:**

### Current Architecture: Supervised Serial Execution

VC sidesteps traditional swarming problems by:
- Serializing work into small, focused issues
- AI supervision at key points (assess → execute → analyze)
- Explicit dependency chains (no race conditions)
- Quality gates before work is "done"
- Automatic work discovery (agent reports back what's needed)

**The VC Workflow:**
```
1. User: "Fix bug X"
2. AI assesses: breaks down into issues, creates dependencies
3. Executor claims ready work (no blockers)
4. AI assesses specific issue: strategy, steps, risks
5. Agent executes in isolated workspace
6. AI analyzes result: completion, punted items, discovered bugs
7. Auto-create follow-on issues with correct dependencies
8. Quality gates enforce standards (test/lint/build)
9. Repeat until mission complete
```

**Key Insight: Coordination is mostly hierarchical, not peer-to-peer**
- Executor (supervisor) assigns work
- Workers execute in isolation
- Workers report back to supervisor
- Supervisor discovers new work, creates issues
- Issue tracker (Beads) provides dependency ordering

### When Does VC Need Multi-Worker?

**Scenarios where parallelism matters:**

1. **Independent work streams** (no file conflicts)
   - Frontend + backend work
   - Different microservices
   - Documentation + code
   - Tests + implementation (if careful)

2. **Speculative parallel attempts** (AI experiment)
   - Try 3 different approaches to hard problem
   - First one to pass quality gates wins
   - Others cancelled automatically

3. **Pipeline parallelism** (future optimization)
   - Worker A: executes current task
   - Worker B: AI assessment for next task (pre-warming)
   - Worker C: runs quality gates on completed work

4. **Scale-out for large missions** (far future)
   - 10+ issues ready to work
   - Spin up worker pool
   - Distribute work across machines

### Coordination Pattern: Supervised vs Peer-to-Peer

**Current: 100% Supervised (Serial)**
```
Executor (Supervisor)
   ↓ assigns work
Worker executes
   ↓ reports completion
Executor analyzes
   ↓ discovers new issues
Beads Issue Tracker
```

**Near-term: Supervised Parallel**
```
Executor (Supervisor)
   ↓ assigns work to multiple workers
Worker A     Worker B     Worker C
   ↓            ↓            ↓
All report completion to Executor
   ↓
Executor coordinates, analyzes, discovers
```

**Far-term: Hybrid (Supervised + Peer Coordination)**
```
Executor (Supervisor)
   ↓ assigns work
Worker A ←→ Worker B  (peer coordination for conflicts)
   ↓            ↓
Both report to Executor
```

**Key Question: How much peer-to-peer do we actually need?**

If workers are assigned non-conflicting work (via file reservations or dependency analysis), they might not need to talk to each other at all. The supervisor handles coordination.

## Critical Design Questions

These questions need answers before we can commit to an architecture. One wrong choice could be costly.

### 1. Message Addressing & Routing

**Question:** When VC agent sends message "to Beads", who receives it?

**Option A: Project-Level Inbox**
```
VC agent → sends to project "beads"
→ Message lands in beads project inbox
→ Any beads agent (main/beads, dave/beads) can read it
→ First to respond claims the conversation?
```

**Option B: Specific Agent Addressing**
```
VC agent → sends to specific agent "dave/beads"
→ Only dave/beads workspace receives it
→ Requires knowing which agent to contact
```

**Option C: Issue-Attached Messages (GitHub-style)**
```
VC agent → creates Beads issue with context
→ Message is first comment on issue
→ Any Beads agent working on that issue sees context
→ Replies are issue comments
→ Issue lifecycle = conversation lifecycle
```

**Leaning toward:** Option C (issue-attached). Messages without issues might get lost or ignored.

### 2. Project Discovery Mechanism

**Question:** How does VC agent discover how to contact Beads project?

**Option A: Central Registry Service**
```bash
# All projects register with central service
coordination-service register ~/src/vc git@github.com:steveyegge/vc.git
coordination-service register ~/src/beads git@github.com:steveyegge/beads.git

# Agents query registry to find projects
coordination-service send --from vc --to beads "message..."
```

**Option B: Local Configuration File**
```yaml
# ~/src/vc/.beads/coordination.yaml
external_projects:
  beads:
    git_remote: git@github.com:steveyegge/beads.git
    local_path: ~/src/beads  # optional
    coordination_url: http://localhost:8765  # or auto-discover
```

**Option C: Convention-Based Discovery**
```bash
# Each project runs coordination service on well-known port
# ~/src/beads → http://localhost:8765 (derived from project hash?)
# ~/src/vc → http://localhost:8766
# Service discovery via mDNS or process registry
```

**Leaning toward:** Option B (local config) with optional convention fallback. Explicit dependencies, no global state.

### 3. Message-Issue Relationship

**Question:** How do messages relate to issues?

**Model A: Messages Create Issues**
```
VC agent → sends message to Beads via coordination service
Coordination service → automatically creates Beads issue
Beads agent → works on issue (standard workflow)
VC agent → polls/watches issue status for resolution
```

**Model B: Messages Are Issue Comments**
```
VC agent → creates Beads issue via Beads API
VC agent → attaches message as first comment
Beads agent → adds replies as comments
Conversation = issue comment thread
Issue tracker is source of truth
```

**Model C: Separate but Linked**
```
VC agent → sends message via coordination service
Message references future Beads issue
Beads agent → creates issue from message
Message thread and issue are separate, but linked
```

**Leaning toward:** Model B (messages as issue comments). Issue tracker already has everything we need.

### 4. Agent Identity Model

**Question:** How do we identify message senders?

**Option A: Agent Only**
```yaml
from: dave/vc  # agent in workspace
to: beads      # project
```

**Option B: Human + Agent**
```yaml
from:
  human: steve
  agent: dave/vc
  project: vc
to:
  project: beads
```

**Option C: Just Project**
```yaml
from: vc
to: beads
# Workspace details are metadata
```

**Leaning toward:** Option B. For debugging, knowing "Steve via dave/vc found this" is valuable context.

### 5. Agent Lifecycle & Online Presence

**Question:** How do agents signal they're online and ready to respond?

**Option A: Registration on Start**
```bash
# Agent registers when starting
cd ~/src/beads
claude-code  # or vc worker
→ registers "dave/beads" as online
→ heartbeat every 30s
→ deregistered on exit (or timeout)
```

**Option B: Polling for Inbox**
```bash
# No explicit registration
# Agents poll inbox when they check for work
# Online = "recently polled inbox"
→ last_seen timestamp
```

**Option C: WebSocket Connection**
```bash
# Agents connect via WebSocket when online
# Coordination service knows active connections
# Can push messages immediately if agent online
# Falls back to inbox if offline
```

**Leaning toward:** Option A (registration + heartbeat) with Option C (WebSocket) as future optimization.

### 6. Integration with Existing Tools

**Question:** Where does coordination service fit with Beads and VC?

**Architecture A: Coordination Service + Beads API**
```
┌──────────────────────┐
│ Coordination Service │  (messaging, discovery, agent registry)
│ (Standalone)         │
└──────────────────────┘
         ↕ (HTTP/MCP)
┌──────────────────────┐  ┌──────────────────────┐
│ Beads Project (VC)   │  │ Beads Project (Beads)│
│ - Issue tracker      │  │ - Issue tracker      │
│ - No coordination    │  │ - No coordination    │
└──────────────────────┘  └──────────────────────┘
         ↕                         ↕
   VC Agents               Beads Agents
```

**Architecture B: Beads with Coordination**
```
┌──────────────────────┐  ┌──────────────────────┐
│ Beads Server (VC)    │  │ Beads Server (Beads) │
│ - Issue tracker      │  │ - Issue tracker      │
│ - Coordination       │  │ - Coordination       │
│ - Messaging          │  │ - Messaging          │
└──────────────────────┘  └──────────────────────┘
         ↕                         ↕
   VC Agents               Beads Agents
```

**Architecture C: Hybrid**
```
┌──────────────────────┐
│ Coordination Service │  (cross-project only)
└──────────────────────┘
         ↕
┌──────────────────────┐  ┌──────────────────────┐
│ Beads (VC)           │  │ Beads (Beads)        │
│ - Issue tracker      │  │ - Issue tracker      │
│ - Within-project     │  │ - Within-project     │
│   coordination       │  │   coordination       │
└──────────────────────┘  └──────────────────────┘
```

**Leaning toward:** Architecture A (standalone coordination service). Keeps Beads focused, allows cross-project messaging without coupling.

### 7. Persistence & Durability

**Question:** What happens when services/agents crash?

**Requirements:**
- Messages must not be lost (durable storage)
- In-flight conversations must survive agent restarts
- Coordination service crash must not lose messages
- Messages should be archived (human-auditable)

**Implementation:**
- SQLite for coordination state (messages, agents, inbox)
- Git-backed archive for human audit (like Agent Mail)
- Write-ahead logging for durability
- Agents can reconstruct state from durable storage

### 8. Access Control & Security

**Question:** Should projects require approval before messaging each other?

**Scenario A: Same maintainer projects**
```
steve owns: vc, beads, wyvern
→ All can message freely (no approval needed)
```

**Scenario B: External projects**
```
steve's vc → wants to message facebook/react
→ Should require approval (or not supported in MVP)
```

**Recommendation for MVP:**
- No access control (trust all projects on same machine)
- Projects identified by git remote
- Only support same-maintainer cross-project messaging
- Add access control later if needed for external collaboration

### 9. Protocol & API Design

**Question:** What protocol should coordination service use?

**Option A: REST API**
```http
POST /projects/beads/messages
{
  "from": {"project": "vc", "agent": "dave/vc"},
  "subject": "Parser crash on empty files",
  "body": "...",
  "attachments": [...]
}

GET /projects/vc/inbox
→ List of messages
```

**Option B: gRPC**
```protobuf
service Coordination {
  rpc SendMessage(MessageRequest) returns (MessageResponse);
  rpc GetInbox(InboxRequest) returns (stream Message);
}
```

**Option C: MCP (Model Context Protocol)**
```json
{
  "method": "tools/call",
  "params": {
    "name": "send_message",
    "arguments": {
      "to_project": "beads",
      "subject": "...",
      "body": "..."
    }
  }
}
```

**Leaning toward:** Option C (MCP). Agent Mail already uses it, Claude Code integrates with it, most agent-friendly.

### 10. Integration with VC Executor

**Question:** Does VC executor use coordination service, or just VC workers?

**Option A: Workers Only**
```
VC Executor (orchestrates VC work)
  ↓ spawns
VC Worker (Claude Code) → discovers Beads bug
  ↓ directly uses coordination service
Coordination Service → creates Beads issue
```

**Option B: Executor + Workers**
```
VC Executor → discovers Beads bug during analysis
  ↓ uses coordination service
Coordination Service → creates Beads issue
```

**Option C: Human-in-the-Loop**
```
VC Worker → reports "found external issue"
  ↓ human approves
Human uses coordination service to file
```

**Leaning toward:** Option A (workers only). Worker agents (Claude Code sessions) are the ones encountering bugs, they should handle the handoff.

## Preliminary Recommendations

Based on analysis so far, leaning toward:

### Architecture: Standalone Coordination Service

**Why standalone:**
- Cross-project messaging is the primary use case
- Keeps Beads focused on issue tracking (no scope creep)
- Can be reused by any project (not VC-specific)
- Clean separation of concerns

**Key Design Choices:**
1. **Messages as Issue Comments** - Issues are source of truth, messages attach to issues
2. **Git-remote-based Project Identity** - Multiple workspaces = same project
3. **MCP Protocol** - Agent-friendly, already used by Agent Mail
4. **Local Config for Discovery** - Explicit dependencies, no global registry
5. **Async-first with Optional Sync** - Inbox model, but can notify online agents
6. **No Access Control in MVP** - Trust all projects on same machine

### What This Means for Implementation

**Option 1: Fork/Fix Agent Mail**
- Modify project identity model (git remote instead of path)
- Simplify/remove features we don't need (contact approval, some mailbox ops)
- Keep what works (messaging, file reservations, git archive, MCP)
- **Pros:** Working implementation, proven design
- **Cons:** Python (not Go), maintenance burden of fork, learning codebase

**Option 2: Build Fresh in Go**
- Clean implementation focused on our use case
- Integrated with Beads ecosystem (Go)
- Can reuse Beads patterns (SQLite, JSONL, git archive)
- **Pros:** Full control, Go native, exact fit for needs
- **Cons:** More work upfront, need to prove the design

**Option 3: Minimal Prototype First**
- Quick prototype (Go or Python) to validate design choices
- Test cross-project messaging with real agents
- Prove issue-attachment model works
- Then decide: fork Agent Mail vs build from scratch
- **Pros:** De-risks the decision, validates assumptions
- **Cons:** Extra work before real implementation

**Leaning toward:** Option 3 (prototype) → then probably Option 2 (build fresh in Go)

### What to Prototype

Minimal coordination service with:
1. **Project registration** (git remote → project ID)
2. **Agent registration** (workspace → agent ID)
3. **Send message** (create issue in target project with context)
4. **Check inbox** (list issues with external messages)
5. **Reply** (add comment to issue)

Test with:
- VC workspace discovers Beads bug
- Creates Beads issue with repro
- Beads workspace checks inbox, sees issue
- Beads agent investigates, adds comments
- VC polls for resolution

If this works smoothly, build full implementation.

## Next Steps

### Immediate: Answer Critical Questions

Before prototyping, need to finalize:

1. **Message-Issue relationship** - Confirm issue-attachment model
2. **Discovery mechanism** - Local config? Convention? Registry?
3. **Agent identity** - Include human in message metadata?
4. **Online/offline model** - Registration + heartbeat? Polling?

### Short-term: Prototype (1-2 days)

Build minimal prototype to validate:
- Cross-project messaging flow
- Issue-attachment model
- Agent lifecycle (online/offline)
- Discovery mechanism

### Medium-term: Production Implementation

After prototype proves design:
1. Decide: fork Agent Mail or build fresh
2. Design complete schema and API
3. Implement coordination service
4. MCP integration for Claude Code
5. Integration with Beads (issue creation API)
6. Documentation and examples

### Long-term: Advanced Features

After basic coordination works:
- File reservations (from Agent Mail)
- Within-project coordination (VC multi-worker)
- WebSocket for push notifications
- Git-backed message archive
- Cross-machine coordination (network)
- Access control for external projects

## References

- **MCP Agent Mail**: `~/src/mcp_agent_mail`
  - `README.md` - Overview and quickstart
  - `CROSS_PROJECT_COORDINATION.md` - Project isolation explanation
  - `project_idea_and_guide.md` - Original design rationale
  - `src/mcp_agent_mail/app.py` - Core implementation

- **VC Documentation**: `~/src/vc/docs/`
  - `README.md` - VC architecture and vision
  - `CLAUDE.md` - Agent instructions and workflow
  - `FEATURES.md` - Executor design, self-healing, etc.

- **Beads**: Issue tracker foundation
  - SQLite-backed issue storage
  - `.beads/beads.db` - Local database
  - `.beads/issues.jsonl` - Git-tracked source of truth

## Open Questions

1. Is "messaging" even the right abstraction, or is it really just "distributed state machine"?
2. Can VC's supervised architecture avoid most peer coordination needs?
3. Should agent naming be `dave/vc` or `vc/dave`? (human ergonomics vs technical consistency)
4. Does coordination belong in Beads, VC, or separate service?
5. How much do we care about reusability by other tools vs optimizing for VC's specific needs?
6. What's the migration path? (Can we start simple and evolve?)

---

**To continue this discussion:** Re-read this document, then analyze VC's actual coordination needs in context of its orchestrator architecture. Focus on: what's the simplest thing that could work for Phase 1 multi-worker support?
