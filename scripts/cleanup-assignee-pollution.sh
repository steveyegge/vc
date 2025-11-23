#!/bin/bash
# cleanup-assignee-pollution.sh - Bulk cleanup of assignee field pollution (vc-3e0o)
#
# This script clears the assignee field from all issues where it was incorrectly
# set at creation time instead of being used for ephemeral claim state.
#
# Usage: ./scripts/cleanup-assignee-pollution.sh [--dry-run]

set -e

DB_PATH="${VC_DB_PATH:-.beads/beads.db}"
DRY_RUN=false

if [[ "$1" == "--dry-run" ]]; then
    DRY_RUN=true
    echo "🔍 DRY RUN MODE - No changes will be made"
    echo
fi

# Check if database exists
if [[ ! -f "$DB_PATH" ]]; then
    echo "❌ Error: Database not found at $DB_PATH"
    echo "   Set VC_DB_PATH environment variable or run from project root"
    exit 1
fi

echo "Assignee Field Pollution Cleanup (vc-3e0o)"
echo "==========================================="
echo "Database: $DB_PATH"
echo

# Get current pollution metrics
echo "📊 Current Pollution Metrics:"
echo

sqlite3 "$DB_PATH" <<SQL
.mode column
.headers on
SELECT
    status,
    COUNT(*) as total_issues,
    COUNT(assignee) as with_assignee,
    PRINTF('%.1f%%', 100.0 * COUNT(assignee) / COUNT(*)) as pollution_pct
FROM issues
GROUP BY status
ORDER BY status;
SQL

echo
echo "Most common assignees:"
sqlite3 "$DB_PATH" <<SQL
.mode column
.headers on
SELECT
    COALESCE(assignee, '(null)') as assignee,
    COUNT(*) as count
FROM issues
WHERE status IN ('open', 'blocked', 'in_progress')
GROUP BY assignee
ORDER BY count DESC
LIMIT 10;
SQL

echo
echo "═══════════════════════════════════════════"
echo

if [[ "$DRY_RUN" == "true" ]]; then
    echo "🔍 Would clear assignee from:"
    sqlite3 "$DB_PATH" "SELECT COUNT(*) || ' closed issues' FROM issues WHERE status = 'closed' AND assignee IS NOT NULL;"
    sqlite3 "$DB_PATH" "SELECT COUNT(*) || ' open issues' FROM issues WHERE status = 'open' AND assignee IS NOT NULL;"
    sqlite3 "$DB_PATH" "SELECT COUNT(*) || ' blocked issues' FROM issues WHERE status = 'blocked' AND assignee IS NOT NULL;"
    echo
    echo "Run without --dry-run to execute cleanup"
    exit 0
fi

# Perform cleanup
echo "🧹 Clearing assignee field..."
echo

# Clear assignee from closed issues (should NEVER have assignee)
closed_count=$(sqlite3 "$DB_PATH" "SELECT COUNT(*) FROM issues WHERE status = 'closed' AND assignee IS NOT NULL;")
if [[ $closed_count -gt 0 ]]; then
    echo "  Clearing assignee from $closed_count closed issues..."
    sqlite3 "$DB_PATH" "UPDATE issues SET assignee = NULL WHERE status = 'closed' AND assignee IS NOT NULL;"
fi

# Clear assignee from open issues (should only be set when actively claimed)
open_count=$(sqlite3 "$DB_PATH" "SELECT COUNT(*) FROM issues WHERE status = 'open' AND assignee IS NOT NULL;")
if [[ $open_count -gt 0 ]]; then
    echo "  Clearing assignee from $open_count open issues..."
    sqlite3 "$DB_PATH" "UPDATE issues SET assignee = NULL WHERE status = 'open' AND assignee IS NOT NULL;"
fi

# Clear assignee from blocked issues (should only be set when actively claimed)
blocked_count=$(sqlite3 "$DB_PATH" "SELECT COUNT(*) FROM issues WHERE status = 'blocked' AND assignee IS NOT NULL;")
if [[ $blocked_count -gt 0 ]]; then
    echo "  Clearing assignee from $blocked_count blocked issues..."
    sqlite3 "$DB_PATH" "UPDATE issues SET assignee = NULL WHERE status = 'blocked' AND assignee IS NOT NULL;"
fi

echo
echo "✅ Cleanup complete!"
echo

# Show updated metrics
echo "📊 Updated Pollution Metrics:"
echo

sqlite3 "$DB_PATH" <<SQL
.mode column
.headers on
SELECT
    status,
    COUNT(*) as total_issues,
    COUNT(assignee) as with_assignee,
    PRINTF('%.1f%%', 100.0 * COUNT(assignee) / COUNT(*)) as pollution_pct
FROM issues
GROUP BY status
ORDER BY status;
SQL

echo
echo "═══════════════════════════════════════════"
echo "✨ Assignee field pollution cleanup complete!"
echo
echo "Note: Future issue creation will not set assignee at creation time."
echo "      Assignee will only be set when executor claims work."
