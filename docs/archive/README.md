# Archived Documentation

This directory contains historical documentation that is no longer current but preserved for reference.

## Contents

### Bootstrap Phase Documentation
- **BOOTSTRAP.md** - Original 2-week roadmap for initial development
  - Status: All phases complete as of Oct 2025
  - Superseded by: Beads issue tracker (`.beads/vc.db`)

### Migration Documentation
- **BEADS_MIGRATION_STATUS.md** - Beads v0.12.0 integration status
  - Status: Migration complete (vc-37 closed)
  - Current: Beads is production storage backend

- **DATABASE_DISCOVERY.md** - Database discovery mechanism design
  - Status: Implemented in `internal/storage/discovery.go`

- **INTERFACE_CHANGES.md** - Historical interface evolution
  - Status: Interfaces stabilized

### Dogfooding History
- **dogfooding-mission-log.md** - Early mission logs (Runs 1-20)
  - Superseded by: `DOGFOOD_RUN25.md` and beads tracker

### Cleanup and Maintenance
- **cleanup-artifacts.md** - Historical cleanup notes
  - Status: Cleanup processes now automated in executor

### Future Design (Not Yet Implemented)
- **JUJUTSU_INTEGRATION_DESIGN.md** - Jujutsu VCS integration design
- **JUJUTSU_INTEGRATION_SUMMARY.md** - Jujutsu integration summary
  - Status: Future work, not current focus
  - Context: Alternative to git worktrees

## Why Archived?

These documents served their purpose during development but are no longer actively maintained. They're preserved for:
1. Historical context
2. Understanding design evolution
3. Reference for similar future work

## Current Documentation

For current documentation, see:
- `README.md` - Project overview
- `ARCHITECTURE.md` - System architecture
- `CLAUDE.md` - AI agent instructions
- `DOGFOODING.md` - Current dogfooding workflow
- `docs/ARCHITECTURE_AUDIT.md` - Comprehensive implementation review
- `docs/EXPLORATION_FINDINGS.md` - Current state analysis
- `.beads/issues.jsonl` - Source of truth for work tracking
