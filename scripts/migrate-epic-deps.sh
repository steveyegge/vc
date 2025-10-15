#!/bin/bash
# Migrate epic-child dependencies from old (epic, child) to new (child, epic) direction
# This fixes inconsistency where old epics used (parent, child) instead of (child, parent)

set -euo pipefail

# Default database path
DB_PATH="${VC_DB_PATH:-.beads/vc.db}"

if [ ! -f "$DB_PATH" ]; then
    echo "Error: Database not found at $DB_PATH"
    echo "Set VC_DB_PATH environment variable if using non-default location"
    exit 1
fi

echo "Migrating epic-child dependencies in $DB_PATH"
echo ""

# Step 1: Identify old-style dependencies
echo "Step 1: Finding old-style (epic, child) dependencies..."
OLD_DEPS=$(sqlite3 "$DB_PATH" <<EOF
SELECT COUNT(*) FROM dependencies d
JOIN issues i1 ON d.issue_id = i1.id
WHERE d.type = 'parent-child'
  AND i1.issue_type = 'epic';
EOF
)

if [ "$OLD_DEPS" -eq 0 ]; then
    echo "No old-style dependencies found. Migration not needed."
    exit 0
fi

echo "Found $OLD_DEPS dependencies to migrate:"
sqlite3 "$DB_PATH" <<EOF
.mode column
.headers on
SELECT d.issue_id as epic, d.depends_on_id as child
FROM dependencies d
JOIN issues i1 ON d.issue_id = i1.id
WHERE d.type = 'parent-child'
  AND i1.issue_type = 'epic'
ORDER BY d.issue_id, d.depends_on_id;
EOF

echo ""
echo "Step 2: Performing migration..."

# Step 2: Migrate dependencies in a transaction
sqlite3 "$DB_PATH" <<EOF
BEGIN TRANSACTION;

-- Create temporary table to hold old dependencies
CREATE TEMP TABLE old_epic_deps AS
SELECT d.issue_id as epic_id, d.depends_on_id as child_id, d.created_at, d.created_by
FROM dependencies d
JOIN issues i1 ON d.issue_id = i1.id
WHERE d.type = 'parent-child'
  AND i1.issue_type = 'epic';

-- Delete old-style dependencies
DELETE FROM dependencies
WHERE (issue_id, depends_on_id) IN (
    SELECT epic_id, child_id FROM old_epic_deps
);

-- Insert new-style dependencies (reversed direction)
INSERT INTO dependencies (issue_id, depends_on_id, type, created_at, created_by)
SELECT child_id, epic_id, 'parent-child', created_at, created_by
FROM old_epic_deps;

COMMIT;
EOF

echo "Migration completed successfully!"
echo ""

# Step 3: Verify migration
echo "Step 3: Verifying migration..."

# Check that no old-style dependencies remain
REMAINING_OLD=$(sqlite3 "$DB_PATH" <<EOF
SELECT COUNT(*) FROM dependencies d
JOIN issues i1 ON d.issue_id = i1.id
WHERE d.type = 'parent-child'
  AND i1.issue_type = 'epic';
EOF
)

if [ "$REMAINING_OLD" -eq 0 ]; then
    echo "✓ Verification passed:"
    echo "  - No old-style (epic, child) dependencies remaining"
    echo "  - All $OLD_DEPS dependencies successfully migrated to (child, epic) direction"
    echo ""
    echo "Example: Check vc-5 children with:"
    echo "  bd show vc-5"
else
    echo "✗ Verification failed:"
    echo "  - Old-style dependencies remaining: $REMAINING_OLD (expected 0)"
    exit 1
fi

echo ""
echo "Migration complete. Remember to export issues to JSONL:"
echo "  bd export -o .beads/issues.jsonl"
