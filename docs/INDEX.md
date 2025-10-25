# VC Documentation Index

This directory contains documentation for the VC (VibeCoder) project.

## Quick Start

**New to VC?** Start here:

1. **[../README.md](../README.md)** - Project overview and quick start
2. **[architecture/README.md](architecture/README.md)** - Architecture documentation reading guide
3. **[architecture/MISSIONS.md](architecture/MISSIONS.md)** - Core architecture (the blueprint)
4. **[EXPLORATION_FINDINGS.md](EXPLORATION_FINDINGS.md)** - What's actually implemented

## Architecture Documentation

**For understanding the system design:**

- **[architecture/README.md](architecture/README.md)** - Reading guide and quick reference (start here!)
- **[architecture/MISSIONS.md](architecture/MISSIONS.md)** - Core mission-driven architecture (1263 lines, comprehensive)
- **[architecture/MISSIONS_CONVERGENCE.md](architecture/MISSIONS_CONVERGENCE.md)** - Iterative convergence design
- **[architecture/BEADS_INTEGRATION.md](architecture/BEADS_INTEGRATION.md)** - Storage layer integration
- **[architecture/BEADS_EXTENSIBILITY.md](architecture/BEADS_EXTENSIBILITY.md)** - Extension model (platform + extensions)
- **[architecture/BEADS_LIBRARY_REVIEW.md](architecture/BEADS_LIBRARY_REVIEW.md)** - Library integration review

## Implementation Documentation

**For understanding what's actually built:**

- **[ARCHITECTURE_AUDIT.md](ARCHITECTURE_AUDIT.md)** - Comprehensive implementation review (30KB)
  - What's implemented vs what's designed
  - 12 core components detailed
  - Testing infrastructure
  - Quality assessment
  - Beads integration analysis

- **[EXPLORATION_FINDINGS.md](EXPLORATION_FINDINGS.md)** - Summary of exploration (this doc)
  - Quick status table
  - Architecture alignment
  - Code quality assessment
  - Documentation gaps
  - Recommendations

## Project Documentation

**For general information:**

- **[../CLAUDE.md](../CLAUDE.md)** - AI agent instructions and guides (34KB, comprehensive)
- **[../ARCHITECTURE.md](../ARCHITECTURE.md)** - System architecture overview (22KB, partially dated)
- **[../BOOTSTRAP.md](../BOOTSTRAP.md)** - Bootstrap roadmap (superseded by beads issues)
- **[../DOGFOODING.md](../DOGFOODING.md)** - Dogfooding workflow and status
- **[../LINTING.md](../LINTING.md)** - Code quality and linting strategy

## Topic-Specific Documentation

**For specific features:**

- **[dogfooding-mission-log.md](dogfooding-mission-log.md)** - Historical dogfooding runs
- **[DOGFOOD_RUN25.md](DOGFOOD_RUN25.md)** - Latest dogfooding session notes
- **[BEADS_MIGRATION_STATUS.md](BEADS_MIGRATION_STATUS.md)** - Beads migration history
- **[INTERFACE_CHANGES.md](INTERFACE_CHANGES.md)** - Interface evolution
- **[DATABASE_DISCOVERY.md](DATABASE_DISCOVERY.md)** - Database discovery mechanism
- **[cleanup-artifacts.md](cleanup-artifacts.md)** - Cleanup strategies

## Package-Specific Documentation

**For understanding individual packages:**

- **[../internal/repl/design.md](../internal/repl/design.md)** - REPL design
- **[../internal/repl/TESTING.md](../internal/repl/TESTING.md)** - REPL testing
- **[../internal/watchdog/WATCHDOG_CONFIG.md](../internal/watchdog/WATCHDOG_CONFIG.md)** - Watchdog configuration
- **[../internal/storage/INTEGRATION_TESTS.md](../internal/storage/INTEGRATION_TESTS.md)** - Storage integration tests

---

## Implementation Status Overview

### What's Fully Implemented (Production-Ready)

✅ **Core Components:**
- Executor (event loop, atomic claiming, sandboxes, AI supervision)
- AI Supervisor (assessment, analysis, code review)
- Storage (Beads + extensions)
- Quality Gates (test, lint, build, approval)
- Sandboxes (git worktree isolation)
- REPL (interactive shell)
- Deduplication (AI-powered)
- Watchdog (convergence detection)
- Health Monitoring (ZFC, cruft, filesize)
- Git Operations (auto-commit, branches)
- Event System (activity feed)
- Testing (930+ tests)

### What's Partially Done

⚠️ **Incomplete Features:**
- Event Retention Cleanup (vc-195): Design done, YAGNI until DB >100MB
- Agent Heartbeats (vc-129): Tool use working, periodic heartbeat future
- Mission Convergence (vc-160): Simple cases working, complex scenarios in progress

### What's Intentionally Deferred

📋 **Deferred:**
- GitOps Merger (vc-4): Design done, disabled for safety
- Advanced Health Monitors: Basic working, complex ones future

---

## Documentation Quality Summary

| Area | Status | Notes |
|------|--------|-------|
| Architecture Design | ✅ EXCELLENT | 4.6KB across 5 files, perfectly aligned |
| Implementation | ✅ PROD | 291 Go files, 930+ tests, fully working |
| User Guide | ❌ MISSING | REPL examples, workflows needed |
| API Reference | ❌ MISSING | No generated docs |
| Implementation Guide | ❌ MISSING | Package-by-package walkthrough needed |
| Code Quality Docs | ✅ GOOD | LINTING.md, conventions clear |
| Configuration Docs | ✅ GOOD | CLAUDE.md has env vars, needs consolidation |

---

## Recommended Reading Order

### For New Developers

1. **[../README.md](../README.md)** - High-level overview
2. **[architecture/MISSIONS.md](architecture/MISSIONS.md)** - Core architecture
3. **[EXPLORATION_FINDINGS.md](EXPLORATION_FINDINGS.md)** - What's actually built
4. **[ARCHITECTURE_AUDIT.md](ARCHITECTURE_AUDIT.md)** - Deep dive into components

### For Architecture Review

1. **[architecture/README.md](architecture/README.md)** - Reading guide
2. **[architecture/MISSIONS.md](architecture/MISSIONS.md)** - Vision
3. **[architecture/BEADS_EXTENSIBILITY.md](architecture/BEADS_EXTENSIBILITY.md)** - Design pattern
4. **[ARCHITECTURE_AUDIT.md](ARCHITECTURE_AUDIT.md)** - Implementation assessment

### For Contributing Code

1. **[EXPLORATION_FINDINGS.md](EXPLORATION_FINDINGS.md)** - What's implemented
2. **[ARCHITECTURE_AUDIT.md](ARCHITECTURE_AUDIT.md)** - Component details
3. **[../LINTING.md](../LINTING.md)** - Code standards
4. Package-specific docs (e.g., `internal/executor/`, `internal/storage/`)

### For Using VC

1. **[../README.md](../README.md)** - Quick start
2. **[../DOGFOODING.md](../DOGFOODING.md)** - Workflow examples
3. **[../CLAUDE.md](../CLAUDE.md)** - Detailed commands and features
4. `./vc --help` and `./vc repl` for interactive help

---

## Key Statistics

**Codebase:**
- 291 Go source files
- 121 test files (42% of all files)
- 930+ test functions
- 36 packages organized by concern
- 40 TODO/FIXME comments (minimal tech debt)

**Documentation:**
- 4.6KB+ architecture docs (5 files)
- 30KB+ implementation audit
- 34KB comprehensive CLAUDE.md
- Multiple package-specific docs

**Quality:**
- 36 lint warnings (mostly low-impact)
- Excellent test coverage
- Well-organized code
- Comprehensive configuration

---

## Quick Links

**Code Repository**: `/Users/stevey/src/vc/`

**Core Packages**:
- `internal/executor/` - Event loop
- `internal/ai/` - AI supervision
- `internal/storage/` - Storage layer
- `internal/gates/` - Quality gates
- `internal/sandbox/` - Sandboxes
- `internal/repl/` - Interactive shell
- `internal/watchdog/` - Convergence detection

**CLI**:
- `cmd/vc/` - Command-line interface

**Tests**:
- 121 test files across all packages
- Run with `go test ./...`

---

## Last Updated

- **Documentation**: 2025-10-25
- **Last Implementation Review**: 2025-10-25
- **Git Status**: Main branch (clean)

---

## Questions?

1. Check the relevant documentation section above
2. Search the architecture docs
3. Look at ARCHITECTURE_AUDIT.md for detailed component info
4. Review EXPLORATION_FINDINGS.md for quick reference
5. File an issue in the beads tracker

