-- VC Issue Tracker Analysis Queries
-- Run with: sqlite3 .beads/beads.db < analyze_issues.sql

.mode column
.headers on
.width 12 60 8 8

-- Summary statistics
SELECT '=== SUMMARY STATISTICS ===' as '';
SELECT
    COUNT(*) as total_issues,
    SUM(CASE WHEN status = 'open' THEN 1 ELSE 0 END) as open,
    SUM(CASE WHEN status = 'closed' THEN 1 ELSE 0 END) as closed,
    SUM(CASE WHEN status = 'in_progress' THEN 1 ELSE 0 END) as in_progress,
    SUM(CASE WHEN status = 'blocked' THEN 1 ELSE 0 END) as blocked
FROM issues;

-- Issues by type and status
SELECT '' as '';
SELECT '=== ISSUES BY TYPE AND STATUS ===' as '';
SELECT
    issue_type,
    status,
    COUNT(*) as count
FROM issues
GROUP BY issue_type, status
ORDER BY issue_type, status;

-- Priority distribution for open issues
SELECT '' as '';
SELECT '=== OPEN ISSUES BY PRIORITY ===' as '';
SELECT
    priority,
    COUNT(*) as count,
    ROUND(COUNT(*) * 100.0 / (SELECT COUNT(*) FROM issues WHERE status = 'open'), 1) as pct
FROM issues
WHERE status = 'open'
GROUP BY priority
ORDER BY priority;

-- Label distribution (open issues only)
SELECT '' as '';
SELECT '=== TOP LABELS (OPEN ISSUES) ===' as '';
SELECT
    l.label,
    COUNT(DISTINCT i.id) as issue_count
FROM labels l
JOIN issues i ON l.issue_id = i.id
WHERE i.status = 'open'
GROUP BY l.label
ORDER BY issue_count DESC
LIMIT 20;

-- Issues created per day (last 30 days)
SELECT '' as '';
SELECT '=== ISSUES CREATED PER DAY (RECENT) ===' as '';
SELECT
    DATE(created_at) as day,
    COUNT(*) as created,
    SUM(CASE WHEN status = 'closed' THEN 1 ELSE 0 END) as now_closed,
    SUM(CASE WHEN status = 'open' THEN 1 ELSE 0 END) as now_open
FROM issues
WHERE created_at > DATE('now', '-30 days')
GROUP BY DATE(created_at)
ORDER BY day DESC;

-- Potential duplicate titles (open issues)
SELECT '' as '';
SELECT '=== POTENTIAL DUPLICATE TITLES (OPEN) ===' as '';
SELECT
    title,
    COUNT(*) as occurrences,
    GROUP_CONCAT(id, ', ') as issue_ids
FROM issues
WHERE status = 'open'
GROUP BY title
HAVING COUNT(*) > 1
ORDER BY occurrences DESC;

-- Test-related issues by priority
SELECT '' as '';
SELECT '=== TEST-RELATED OPEN ISSUES BY PRIORITY ===' as '';
SELECT
    priority,
    COUNT(*) as count
FROM issues
WHERE status = 'open'
    AND (title LIKE '%test%' OR title LIKE '%Test%')
GROUP BY priority
ORDER BY priority;

-- Supervisor-discovered issues breakdown
SELECT '' as '';
SELECT '=== SUPERVISOR-DISCOVERED ISSUES BREAKDOWN ===' as '';
SELECT
    i.status,
    CASE
        WHEN i.title LIKE '%test%' OR i.title LIKE '%Test%' THEN 'test-related'
        WHEN i.title LIKE '%Code Review%' THEN 'code-review'
        ELSE 'other'
    END as category,
    COUNT(*) as count
FROM issues i
JOIN labels l ON i.id = l.issue_id
WHERE l.label = 'discovered:supervisor'
GROUP BY i.status, category
ORDER BY i.status, count DESC;

-- Oldest open issues (potential stale work)
SELECT '' as '';
SELECT '=== OLDEST OPEN ISSUES (TOP 20) ===' as '';
.width 12 70 10 12
SELECT
    id,
    title,
    priority,
    DATE(created_at) as created
FROM issues
WHERE status = 'open'
ORDER BY created_at ASC
LIMIT 20;

-- Issues with most dependencies (blockers)
SELECT '' as '';
SELECT '=== ISSUES WITH MOST BLOCKERS (TOP 10) ===' as '';
.width 12 60 8
SELECT
    i.id,
    i.title,
    COUNT(d.from_issue_id) as blocker_count
FROM issues i
JOIN dependencies d ON i.id = d.to_issue_id
WHERE i.status = 'open'
GROUP BY i.id
ORDER BY blocker_count DESC
LIMIT 10;

-- High priority issues that might be noise
SELECT '' as '';
SELECT '=== P0/P1 TEST ISSUES (POTENTIAL NOISE) ===' as '';
.width 12 70 8
SELECT
    id,
    title,
    priority
FROM issues
WHERE status = 'open'
    AND priority <= 1
    AND (title LIKE '%test%' OR title LIKE '%Test%')
ORDER BY priority, created_at
LIMIT 20;

-- Code review sweep analysis
SELECT '' as '';
SELECT '=== CODE REVIEW SWEEP ISSUES ===' as '';
SELECT
    id,
    title,
    status,
    DATE(created_at) as created
FROM issues
WHERE title LIKE '%Code Review Sweep%'
ORDER BY created_at;

-- Issues by week (trend analysis)
SELECT '' as '';
SELECT '=== ISSUE CREATION TREND (BY WEEK) ===' as '';
SELECT
    STRFTIME('%Y-W%W', created_at) as week,
    COUNT(*) as created,
    SUM(CASE WHEN status = 'closed' THEN 1 ELSE 0 END) as closed,
    SUM(CASE WHEN status = 'open' THEN 1 ELSE 0 END) as open
FROM issues
WHERE created_at > DATE('now', '-60 days')
GROUP BY STRFTIME('%Y-W%W', created_at)
ORDER BY week DESC;

-- Find similar titles (fuzzy matching via prefix)
SELECT '' as '';
SELECT '=== SIMILAR TITLE PATTERNS (POTENTIAL DUPLICATES) ===' as '';
.width 50 8
SELECT
    SUBSTR(title, 1, 50) as title_prefix,
    COUNT(*) as count
FROM issues
WHERE status = 'open'
    AND LENGTH(title) > 20
GROUP BY SUBSTR(title, 1, 50)
HAVING COUNT(*) > 1
ORDER BY count DESC
LIMIT 20;
