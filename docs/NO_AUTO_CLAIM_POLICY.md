# No-Auto-Claim Policy

**Goal**: Enable VC to self-host by tackling hard production bugs while reserving human judgment for truly human-only tasks.

## Policy Summary

The `no-auto-claim` label prevents the VC executor from automatically claiming an issue. Use it **ONLY** for these 4 narrow criteria:

1. **External coordination** - Requires interaction with other teams, approval workflows, or external dependencies
2. **Human creativity** - Product design, UX decisions, branding, marketing
3. **Business judgment** - Pricing, legal, compliance, contracts
4. **Pure research** - Exploring unknowns with no deliverable

**Everything else is fair game for VC.** This includes concurrency bugs, race conditions, shutdown logic, schema changes, performance issues, and critical infrastructure work.

## Rationale: Trust the Safety Nets

VC has robust safety mechanisms that catch issues before they cause damage:

### Quality Gates
- **Tests** must pass before merge
- **Linting** enforces code standards
- **Build** validates compilation and dependencies
- Gates run in sandbox (git worktree) - cannot contaminate main branch

### AI Supervision
- **Assessment phase** reviews task, identifies risks, plans approach
- **Analysis phase** verifies completion, catches mistakes, discovers follow-on work
- Both phases use Claude to catch issues humans might miss

### Sandbox Isolation
- Each issue executes in isolated git worktree
- Failed work never pollutes main branch
- Easy rollback if something goes wrong

### Self-Healing (vc-210)
- Detects broken baselines (test/lint/build failures in main branch)
- Automatically creates fix issues with full diagnostic context
- Ensures main branch stays healthy even if human commits break things

### Human Oversight
- Activity feed shows everything VC is doing in real-time
- Humans can intervene at any point via CLI
- Failed quality gates stop progress until fixed
- Issues can be manually claimed/reassigned anytime

**Bottom line**: The old conservative approach (marking complex issues `no-auto-claim`) slowed VC's path to self-hosting. The safety nets are strong enough to handle hard problems.

## Decision Tree

```
┌─ Does this issue require interaction with external teams or systems?
│  (Examples: API approval from backend team, coordination with DevOps,
│   waiting for vendor response)
│
├─ YES → Use no-auto-claim
│
└─ NO ↓

┌─ Does this issue require human creativity or subjective taste?
│  (Examples: Choose color scheme, design marketing page, write blog post,
│   create brand identity)
│
├─ YES → Use no-auto-claim
│
└─ NO ↓

┌─ Does this issue require business/legal judgment?
│  (Examples: Set pricing, review ToS changes, approve compliance policy,
│   negotiate contract)
│
├─ YES → Use no-auto-claim
│
└─ NO ↓

┌─ Is this pure research with no clear deliverable?
│  (Examples: "Explore feasibility of quantum computing for our use case",
│   "Research whether we should migrate to Rust")
│
├─ YES → Use no-auto-claim
│
└─ NO → DO NOT use no-auto-claim (let VC handle it)
```

## Examples

### ✅ Should NOT have no-auto-claim (VC can handle)

| Issue | Why VC Can Handle It |
|-------|---------------------|
| Fix race condition in executor shutdown | Safety nets: tests catch regressions, sandbox isolates changes, self-healing fixes breaks |
| Optimize database queries causing 3s page load | AI can profile, identify bottlenecks, test improvements. Quality gates verify no regressions |
| Refactor authentication layer to support OAuth2 | Clear technical requirements. AI supervision guides approach. Tests validate behavior |
| Fix memory leak in worker pool | Diagnostic with profiling tools, fix implementation, verify with load tests |
| Migrate from SQLite to Postgres schema | Well-defined technical task. Migrations testable. Rollback possible |
| Fix deadlock in dependency graph traversal | Concurrency bugs have clear failure modes. Tests can reproduce. AI can reason about locks |
| Add rate limiting to API endpoints | Clear requirements (e.g., 1000 req/min). Implementation patterns well-known. Tests validate |
| Debug why CI randomly fails on macOS | Systematic debugging: gather logs, reproduce locally, fix root cause. Safety nets catch mistakes |

### ❌ Should have no-auto-claim (requires human judgment)

| Issue | Why Human Is Needed | Which Criterion |
|-------|-------------------|-----------------|
| Get approval from backend team for new API endpoint | Need to coordinate with another team, wait for their review | External coordination |
| Design the landing page for new product launch | Requires aesthetic judgment, brand alignment, UX intuition | Human creativity |
| Decide whether to offer free tier or 14-day trial | Business strategy decision with market implications | Business judgment |
| Research whether to rebuild in Rust or stay with Go | Open-ended exploration, no clear action without human decision | Pure research |
| Update Terms of Service for GDPR compliance | Legal review required, liability implications | Business judgment |
| Choose color scheme for new dashboard UI | Subjective design decision, brand consistency | Human creativity |
| Schedule infrastructure migration with SRE team | Requires coordination, approval, scheduling with external team | External coordination |

### 🔬 Borderline Cases (How to Decide)

| Issue | Initial Reaction | Correct Answer | Why |
|-------|-----------------|----------------|-----|
| Implement dark mode feature | "Complex UI work, needs design" | **No label** | Requirements likely defined already. Implementation is mechanical. Tests verify behavior |
| Fix intermittent test failure | "Flaky tests are hard" | **No label** | Systematic debugging. Add logging, reproduce, fix. Safety nets catch mistakes |
| Add telemetry to track user behavior | "Privacy implications?" | **No label** (unless legal review needed) | Technical implementation. If legal review needed, that's separate issue with `no-auto-claim` |
| Upgrade Go from 1.20 to 1.21 | "Critical infrastructure" | **No label** | Clear upgrade path. Tests validate compatibility. Rollback easy |
| Investigate why production pod crashed | "Production systems scary" | **No label** | Diagnostic work: logs, metrics, root cause. If needs SRE help, separate coordination issue |
| Implement new compression algorithm | "Novel algorithm?" | **No label** if requirements clear, **label** if pure research | If "implement zstd compression" = no label. If "research compression options" = label |

**Rule of thumb**: If you're saying "This is too hard/risky for VC", ask yourself: "Which safety net would catch a mistake?" If there's an answer (tests, gates, sandbox, self-healing), **don't use the label**.

## Migration Strategy

During the L1 "Bug Crusher" phase, we're migrating from the old conservative policy to this narrow policy:

### Phase 1: Controlled Experiment (vc-8d71)
- Remove `no-auto-claim` from 5 carefully chosen bugs
- Monitor success rate, intervention rate, failure modes
- **Target**: 60%+ success rate, <20% intervention, zero catastrophic failures

### Phase 2: Expanded Experiment (vc-9d78)
- Remove from 10 more issues based on Phase 1 learnings
- Include more complex issues (concurrency, shutdown, schema)
- **Target**: 70%+ success rate, validate safety nets at scale

### Phase 3: Policy Enforcement (vc-0a42)
- Audit all remaining `no-auto-claim` issues
- Remove label from issues that don't meet 4 narrow criteria
- Document any new edge cases discovered

### L1 Graduation Criteria
- **Volume**: 50+ bugs completed (including formerly `no-auto-claim` issues)
- **Success rate**: 85%+
- **Intervention rate**: <15%
- **Catastrophic failures**: 0 (where safety nets failed to catch issues)
- **Timeline**: 2-3 weeks

## Technical Implementation

The `no-auto-claim` label is enforced in `GetReadyWork()` query:

```go
// internal/storage/beads/wrapper.go
// Excludes issues with no-auto-claim label
WHERE NOT EXISTS (
    SELECT 1 FROM labels
    WHERE issue_id = issues.id
    AND name = 'no-auto-claim'
)
```

This means:
- VC executor never sees `no-auto-claim` issues in ready work queue
- Humans can still claim these issues via `bd update vc-X --notes "working on this"`
- Claude Code sessions can work on these issues normally
- Label acts as gate only for autonomous executor

## When Humans Should Override VC

Even without `no-auto-claim`, humans can manually claim issues:

```bash
# Claim issue before VC gets to it
bd update vc-X --notes "I'll handle this one personally"
```

**When to do this:**
- You have specific context/expertise that would be hard to communicate in issue description
- Issue involves sensitive customer data or security incident response
- You want to pair program with VC (claim issue, let VC assist via REPL)
- Issue is extremely time-sensitive and you can do it faster

**Note**: This is about execution preference, not capability. If VC *could* do it with proper guidance, don't use `no-auto-claim` label - just manually claim if you want to handle it yourself.

## FAQ

**Q: What if an issue spans multiple categories (e.g., implement feature that requires legal review)?**

A: Break it into separate issues:
- `vc-123`: Design and implement feature (no label - VC handles)
- `vc-124`: Legal review of feature behavior (label: business judgment)
- Make 123 block on 124 if needed via dependency

**Q: What if I'm genuinely unsure if VC can handle something?**

A: **Don't use `no-auto-claim`**. Let VC try. Worst case:
- Quality gates fail → VC doesn't merge broken code
- AI supervision catches issues → Creates follow-on issue for human review
- Sandbox isolation → Failed attempt doesn't pollute main branch
- You can manually intervene anytime

**Q: What about issues affecting production/customer-facing systems?**

A: VC can handle these **if the work is testable and has clear acceptance criteria**. The sandbox isolation and quality gates protect production. If you need coordination with SRE/oncall, that's a separate issue with `no-auto-claim` for external coordination.

**Q: What if safety nets fail and VC merges bad code?**

A: This is what the L1 experiments will measure. If safety nets prove insufficient:
1. Self-healing (vc-210) auto-creates fix issue
2. Rollback is trivial (revert commit)
3. We learn what additional gates/checks are needed
4. We improve safety nets, not add more `no-auto-claim` labels

The goal is to make VC **safe** to run unsupervised, not to restrict what it can work on.

**Q: Can I still review VC's PRs before merge?**

A: Absolutely! VC creates PRs for human review (unless configured otherwise). The `no-auto-claim` policy is about **who claims the issue**, not whether code gets reviewed before merge.

## Summary

**Old policy**: Mark complex/risky issues `no-auto-claim` to be safe

**New policy**: ONLY use `no-auto-claim` for 4 narrow human-only criteria

**Why**: Trust the safety nets. They're strong enough to handle hard problems. The old conservative approach was slowing VC's path to self-hosting.

**Next**: Audit existing `no-auto-claim` issues (vc-2d0c), run controlled experiments (vc-8d71, vc-9d78), graduate to L1 Bug Crusher capability.

---

**See also:**
- [CLAUDE.md](../CLAUDE.md) - Quick reference for policy
- [FEATURES.md](FEATURES.md) - Deep dives on safety nets (quality gates, self-healing, etc.)
- [vc-4778](../.beads/vc.db) - L1 Bug Crusher roadmap (view with `bd show vc-4778`)
