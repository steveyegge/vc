#!/bin/bash
# Aggressive mass cleanup script for VC issue tracker
# Target: Remove at least 200 obsolete/redundant issues

set -e

DB_PATH="${VC_DB_PATH:-.beads/vc.db}"
BACKUP_PATH=".beads/vc.db.backup.$(date +%Y%m%d_%H%M%S)"

echo "🗄️  Creating backup: $BACKUP_PATH"
cp "$DB_PATH" "$BACKUP_PATH"

echo "📊 Current issue count:"
sqlite3 "$DB_PATH" "SELECT status, COUNT(*) FROM issues GROUP BY status"
TOTAL_BEFORE=$(sqlite3 "$DB_PATH" "SELECT COUNT(*) FROM issues")
echo "Total: $TOTAL_BEFORE"

# Collect issue IDs to delete
TO_DELETE_FILE=$(mktemp)

echo ""
echo "🔍 Finding issues to delete..."

# 1. ALL closed discovered:blocker issues (these are tactical fixes that are done)
echo "  - ALL closed discovered:blocker issues..."
sqlite3 "$DB_PATH" "SELECT DISTINCT i.id FROM issues i JOIN labels l ON i.id = l.issue_id WHERE i.status = 'closed' AND l.label = 'discovered:blocker'" >> "$TO_DELETE_FILE"

# 2. ALL closed issues with "Add test" or "test coverage" in title
echo "  - ALL closed test-related issues..."
sqlite3 "$DB_PATH" "SELECT id FROM issues WHERE status = 'closed' AND (title LIKE '%Add test%' OR title LIKE '%test coverage%' OR title LIKE '%Add integration test%' OR title LIKE '%Add unit test%' OR title LIKE '%Add validation test%')" >> "$TO_DELETE_FILE"

# 3. ALL closed lint/staticcheck issues
echo "  - ALL closed lint issues..."
sqlite3 "$DB_PATH" "SELECT id FROM issues WHERE status = 'closed' AND (title LIKE '%lint%' OR title LIKE '%staticcheck%' OR title LIKE '%unparam%' OR title LIKE '%errcheck%' OR title LIKE '%golangci%')" >> "$TO_DELETE_FILE"

# 4. ALL closed "Fix" issues that are small tactical fixes
echo "  - ALL closed tactical 'Fix' issues..."
sqlite3 "$DB_PATH" "SELECT id FROM issues WHERE status = 'closed' AND title LIKE 'Fix %' AND (title LIKE '%error%' OR title LIKE '%warning%' OR title LIKE '%failure%' OR title LIKE '%flaky%')" >> "$TO_DELETE_FILE"

# 5. Closed acceptance criteria meta-issues
echo "  - Closed acceptance criteria meta-issues..."
sqlite3 "$DB_PATH" "SELECT id FROM issues WHERE status = 'closed' AND (title LIKE '%acceptance criteria%' OR title LIKE '%needs acceptance criteria%')" >> "$TO_DELETE_FILE"

# 6. Closed investigation/clarify issues
echo "  - Closed investigation issues..."
sqlite3 "$DB_PATH" "SELECT id FROM issues WHERE status = 'closed' AND (title LIKE 'Investigate%' OR title LIKE 'Clarify%' OR title LIKE 'Verify%')" >> "$TO_DELETE_FILE"

# 7. Closed "regression test" issues
echo "  - Closed regression test issues..."
sqlite3 "$DB_PATH" "SELECT id FROM issues WHERE status = 'closed' AND title LIKE '%regression test%'" >> "$TO_DELETE_FILE"

# 8. Closed issues from discovered work that completed
echo "  - Closed discovered:related work..."
sqlite3 "$DB_PATH" "SELECT DISTINCT i.id FROM issues i JOIN labels l ON i.id = l.issue_id WHERE i.status = 'closed' AND l.label = 'discovered:related'" >> "$TO_DELETE_FILE"

# 9. Closed WIP and prototype issues
echo "  - Closed WIP/prototype issues..."
sqlite3 "$DB_PATH" "SELECT id FROM issues WHERE status = 'closed' AND (title LIKE 'WIP:%' OR title LIKE '%prototype%')" >> "$TO_DELETE_FILE"

# 10. Closed duplicate issues
echo "  - Closed duplicate detection issues..."
sqlite3 "$DB_PATH" "SELECT id FROM issues WHERE status = 'closed' AND (title LIKE '%duplicate%' OR description LIKE '%This is a duplicate%')" >> "$TO_DELETE_FILE"

# Deduplicate
sort -u "$TO_DELETE_FILE" > "${TO_DELETE_FILE}.uniq"
mv "${TO_DELETE_FILE}.uniq" "$TO_DELETE_FILE"

COUNT=$(wc -l < "$TO_DELETE_FILE")
echo ""
echo "📝 Found $COUNT issues to delete"

if [ "$COUNT" -eq 0 ]; then
    echo "❌ No issues found to delete. Exiting."
    rm "$TO_DELETE_FILE"
    exit 0
fi

# Show sample
echo ""
echo "Sample of issues to be deleted (first 20):"
head -20 "$TO_DELETE_FILE" | while read id; do
    sqlite3 "$DB_PATH" "SELECT id, title, status FROM issues WHERE id = '$id'"
done

echo ""
echo "🗑️  Deleting $COUNT issues (auto-confirmed)..."

# Delete issues
while read id; do
    # Delete from all related tables
    sqlite3 "$DB_PATH" "DELETE FROM labels WHERE issue_id = '$id'" 2>/dev/null || true
    sqlite3 "$DB_PATH" "DELETE FROM dependencies WHERE child_issue_id = '$id' OR parent_issue_id = '$id'" 2>/dev/null || true
    sqlite3 "$DB_PATH" "DELETE FROM issues WHERE id = '$id'"
done < "$TO_DELETE_FILE"

rm "$TO_DELETE_FILE"

echo "✅ Deleted $COUNT issues"

echo ""
echo "📊 New issue count:"
sqlite3 "$DB_PATH" "SELECT status, COUNT(*) FROM issues GROUP BY status"
TOTAL_AFTER=$(sqlite3 "$DB_PATH" "SELECT COUNT(*) FROM issues")
echo "Total: $TOTAL_AFTER (was $TOTAL_BEFORE, removed $((TOTAL_BEFORE - TOTAL_AFTER)))"

echo ""
echo "💾 Exporting to JSONL..."
bd export -o .beads/issues.jsonl

echo ""
echo "✅ Cleanup complete!"
echo "   Backup: $BACKUP_PATH"
echo "   Deleted: $COUNT issues"
echo "   Before: $TOTAL_BEFORE"
echo "   After: $TOTAL_AFTER"
