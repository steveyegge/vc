#!/bin/bash
# VC Issue Tracker Pollution Cleanup Script
# Generated: 2025-11-23
#
# This script identifies and closes spurious/duplicate issues from supervisor over-discovery.
# Review each section before running - some judgement calls required.

set -e

echo "=== VC Issue Tracker Cleanup ==="
echo "Total issues before cleanup:"
bd stats

# Phase 1: Close Code Review Sweep Duplicates
# Keep: vc-cc56 (thorough), vc-cea7 (quick), vc-20b8 (targeted)
# Close: all other duplicates

echo ""
echo "Phase 1: Closing Code Review Sweep duplicates..."
echo "Keeping: vc-cc56 (thorough), vc-cea7 (quick), vc-20b8 (targeted)"

# Thorough duplicates (keep vc-cc56)
bd close vc-77b3 --reason "Duplicate code review sweep, consolidated into vc-cc56"
bd close vc-a409 --reason "Duplicate code review sweep, consolidated into vc-cc56"
bd close vc-5e29 --reason "Duplicate code review sweep, consolidated into vc-cc56"
bd close vc-ebcb --reason "Duplicate code review sweep, consolidated into vc-cc56"
bd close vc-ab59 --reason "Duplicate code review sweep, consolidated into vc-cc56"
bd close vc-i9hf --reason "Duplicate code review sweep, consolidated into vc-cc56"

# Quick duplicates (keep vc-cea7)
bd close vc-0261 --reason "Duplicate code review sweep, consolidated into vc-cea7"
bd close vc-ac21 --reason "Duplicate code review sweep, consolidated into vc-cea7"
bd close vc-f81a --reason "Duplicate code review sweep, consolidated into vc-cea7"
bd close vc-aedf --reason "Duplicate code review sweep, consolidated into vc-cea7"
bd close vc-dfc2 --reason "Duplicate code review sweep, consolidated into vc-cea7"
bd close vc-aj8o --reason "Duplicate code review sweep, consolidated into vc-cea7"

# Targeted duplicates (keep vc-20b8)
bd close vc-dccc --reason "Duplicate code review sweep, consolidated into vc-20b8"

echo "✓ Closed 13 code review sweep duplicates"

# Phase 2: Close explicit duplicate test issues
echo ""
echo "Phase 2: Closing explicit duplicate test issues..."

# "Add test for issue status transition from blocked to open" - 3 copies
# Keep one, close the other 2 (need to identify the IDs)
sqlite3 .beads/beads.db "SELECT id FROM issues WHERE title = 'Add test for issue status transition from blocked to open' ORDER BY created_at" | while read id; do
    if [ -z "$first_id" ]; then
        first_id="$id"
        echo "Keeping $first_id as canonical"
    else
        echo "Closing duplicate $id"
        bd close "$id" --reason "Duplicate test issue, consolidated into $first_id"
    fi
done

echo "✓ Closed duplicate test issues"

# Phase 3: Interactive review of supervisor-discovered test issues
echo ""
echo "Phase 3: Review supervisor-discovered test issues (64 total)"
echo "This requires manual review. Dumping list to review_test_issues.txt"

sqlite3 .beads/beads.db "SELECT i.id || ' | ' || i.title || ' | P' || i.priority FROM issues i JOIN labels l ON i.id = l.issue_id WHERE l.label = 'discovered:supervisor' AND i.status = 'open' AND (i.title LIKE '%test%' OR i.title LIKE '%Test%') ORDER BY i.priority, i.created_at" > review_test_issues.txt

echo "Review review_test_issues.txt and close low-value test issues"
echo "Example: bd close vc-XXXX --reason 'Low-value test issue, not critical path'"

# Phase 4: Close trivial/obvious noise test issues (examples)
echo ""
echo "Phase 4: Closing obvious noise test issues..."

# These are examples - adjust based on your assessment
# Close tests for trivial getters/setters
# Close tests for code that's already well-tested indirectly
# Close tests for deprecated/to-be-removed code

# Example batch closure (customize this list):
# bd close vc-536c --reason "Circuit breaker has sufficient coverage via integration tests"
# bd close vc-b2cd --reason "WaitGroup edge case not critical path"
# ... add more as you identify them

echo "⚠️  Phase 4 requires manual identification of trivial test issues"
echo "Review the codebase and close test issues for:"
echo "  - Trivial functions (getters, setters, simple utils)"
echo "  - Code with sufficient indirect test coverage"
echo "  - Non-critical edge cases"

# Phase 5: Summary
echo ""
echo "=== Cleanup Summary ==="
bd stats

echo ""
echo "Next steps:"
echo "1. Review review_test_issues.txt and close low-value issues"
echo "2. Export to git: bd export -o .beads/issues.jsonl"
echo "3. Commit changes: git add .beads/issues.jsonl && git commit -m 'Clean up issue tracker pollution'"
echo "4. File follow-up issues for supervisor tuning (see ISSUE_POLLUTION_ANALYSIS.md)"
