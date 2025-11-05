#!/bin/bash
# Mass cleanup script for VC issue tracker
# Target: Remove at least 200 obsolete/redundant issues

set -e

DB_PATH="${VC_DB_PATH:-.beads/vc.db}"
BACKUP_PATH=".beads/vc.db.backup.$(date +%Y%m%d_%H%M%S)"

echo "🗄️  Creating backup: $BACKUP_PATH"
cp "$DB_PATH" "$BACKUP_PATH"

echo "📊 Current issue count:"
sqlite3 "$DB_PATH" "SELECT status, COUNT(*) FROM issues GROUP BY status"

# List of issues to delete (will be populated by queries below)
TO_DELETE=()

echo ""
echo "🔍 Finding issues to delete..."

# 1. Find closed meta-issues about acceptance criteria
echo "  - Closed acceptance criteria meta-issues..."
while IFS= read -r id; do
    TO_DELETE+=("$id")
done < <(sqlite3 "$DB_PATH" "SELECT id FROM issues WHERE status = 'closed' AND (title LIKE '%acceptance criteria%' OR title LIKE '%needs acceptance criteria%')")

# 2. Find closed "Add test" issues that were automated discoveries
echo "  - Closed 'Add test' automated issues..."
while IFS= read -r id; do
    TO_DELETE+=("$id")
done < <(sqlite3 "$DB_PATH" "SELECT id FROM issues WHERE status = 'closed' AND title LIKE 'Add test%' AND description LIKE '%automatically created%'")

# 3. Find closed lint fix issues (small tactical fixes)
echo "  - Closed lint fix issues..."
while IFS= read -r id; do
    TO_DELETE+=("$id")
done < <(sqlite3 "$DB_PATH" "SELECT id FROM issues WHERE status = 'closed' AND (title LIKE 'Fix%lint%' OR title LIKE 'Fix staticcheck%' OR title LIKE 'Fix unparam%')")

# 4. Find closed "investigate" issues that were false alarms
echo "  - Closed investigation false alarms..."
while IFS= read -r id; do
    TO_DELETE+=("$id")
done < <(sqlite3 "$DB_PATH" "SELECT id FROM issues WHERE status = 'closed' AND title LIKE '%Investigate%' AND (notes LIKE '%false alarm%' OR notes LIKE '%already fixed%' OR notes LIKE '%no longer relevant%')")

# 5. Find validation/test issues that are closed
echo "  - Closed validation test issues..."
while IFS= read -r id; do
    TO_DELETE+=("$id")
done < <(sqlite3 "$DB_PATH" "SELECT id FROM issues WHERE status = 'closed' AND (title LIKE 'Add validation test%' OR title LIKE 'Add unit test%' OR title LIKE 'Add integration test%')")

# 6. Find closed "Fix test failure" discovered:blocker issues
echo "  - Closed test failure blocker issues..."
while IFS= read -r id; do
    TO_DELETE+=("$id")
done < <(sqlite3 "$DB_PATH" "SELECT DISTINCT i.id FROM issues i JOIN labels l ON i.id = l.issue_id WHERE i.status = 'closed' AND l.label = 'discovered:blocker' AND i.title LIKE '%test%fail%'")

# 7. Find closed issues from old bootstrap phases (before current architecture)
echo "  - Old bootstrap phase issues..."
while IFS= read -r id; do
    TO_DELETE+=("$id")
done < <(sqlite3 "$DB_PATH" "SELECT id FROM issues WHERE status = 'closed' AND created_at < '2025-11-01' AND (description LIKE '%bootstrap%' OR title LIKE '%WIP:%')")

# Deduplicate the list
TO_DELETE=($(printf '%s\n' "${TO_DELETE[@]}" | sort -u))

echo ""
echo "📝 Found ${#TO_DELETE[@]} issues to delete"

if [ ${#TO_DELETE[@]} -eq 0 ]; then
    echo "❌ No issues found to delete. Exiting."
    exit 0
fi

# Show sample of what will be deleted
echo ""
echo "Sample of issues to be deleted (first 10):"
for id in "${TO_DELETE[@]:0:10}"; do
    sqlite3 "$DB_PATH" "SELECT id, title, status FROM issues WHERE id = '$id'"
done

echo ""
read -p "🗑️  Delete ${#TO_DELETE[@]} issues? (yes/no): " CONFIRM

if [ "$CONFIRM" != "yes" ]; then
    echo "❌ Cancelled. Backup preserved at: $BACKUP_PATH"
    exit 0
fi

# Delete issues
echo ""
echo "🗑️  Deleting issues..."

for id in "${TO_DELETE[@]}"; do
    # Delete from all related tables (cascade should handle most, but be explicit)
    sqlite3 "$DB_PATH" "DELETE FROM labels WHERE issue_id = '$id'"
    sqlite3 "$DB_PATH" "DELETE FROM dependencies WHERE child_issue_id = '$id' OR parent_issue_id = '$id'"
    sqlite3 "$DB_PATH" "DELETE FROM issues WHERE id = '$id'"
done

echo "✅ Deleted ${#TO_DELETE[@]} issues"

echo ""
echo "📊 New issue count:"
sqlite3 "$DB_PATH" "SELECT status, COUNT(*) FROM issues GROUP BY status"

echo ""
echo "💾 Exporting to JSONL..."
bd export -o .beads/issues.jsonl

echo ""
echo "✅ Cleanup complete!"
echo "   Backup: $BACKUP_PATH"
echo "   Deleted: ${#TO_DELETE[@]} issues"
echo ""
echo "Next steps:"
echo "  1. Review changes: git diff .beads/issues.jsonl"
echo "  2. Commit: git add .beads/issues.jsonl && git commit -m 'Mass cleanup: removed ${#TO_DELETE[@]} obsolete issues'"
