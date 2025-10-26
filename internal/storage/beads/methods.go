package beads

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/steveyegge/beads"
	"github.com/steveyegge/vc/internal/types"
)

// ======================================================================
// ISSUE OPERATIONS (delegate to Beads + extension table lookups)
// ======================================================================

// GetIssue retrieves an issue by ID (Beads core + VC extensions)
func (s *VCStorage) GetIssue(ctx context.Context, id string) (*types.Issue, error) {
	// Get core issue from Beads
	beadsIssue, err := s.Storage.GetIssue(ctx, id)
	if err != nil {
		return nil, err
	}

	// Convert to VC type
	vcIssue := beadsIssueToVC(beadsIssue)

	// Look up subtype in VC extension table
	var subtype sql.NullString
	err = s.db.QueryRowContext(ctx,
		"SELECT subtype FROM vc_mission_state WHERE issue_id = ?", id,
	).Scan(&subtype)

	if err == nil && subtype.Valid {
		vcIssue.IssueSubtype = types.IssueSubtype(subtype.String)
	} else if err != nil && err != sql.ErrNoRows {
		// Real error (not just missing row)
		return nil, fmt.Errorf("failed to query mission state: %w", err)
	}

	return vcIssue, nil
}

// GetMission retrieves a mission issue with full metadata
func (s *VCStorage) GetMission(ctx context.Context, id string) (*types.Mission, error) {
	// Get base issue
	issue, err := s.GetIssue(ctx, id)
	if err != nil {
		return nil, err
	}

	// Query mission metadata from extension table
	var mission types.Mission
	mission.Issue = *issue

	var sandboxPath, branchName, gatesStatus, goal, context, approvedBy sql.NullString
	var approvedAt sql.NullTime
	var iterationCount sql.NullInt64

	err = s.db.QueryRowContext(ctx, `
		SELECT sandbox_path, branch_name, iteration_count, gates_status,
		       goal, context, phase_count, current_phase, approval_required, approved_at, approved_by
		FROM vc_mission_state
		WHERE issue_id = ? AND subtype IN ('mission', 'phase')
	`, id).Scan(
		&sandboxPath,
		&branchName,
		&iterationCount,
		&gatesStatus,
		&goal,
		&context,
		&mission.PhaseCount,
		&mission.CurrentPhase,
		&mission.ApprovalRequired,
		&approvedAt,
		&approvedBy,
	)

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("issue %s is not a mission", id)
		}
		return nil, fmt.Errorf("failed to query mission metadata: %w", err)
	}

	// Handle nullable fields
	if sandboxPath.Valid {
		mission.SandboxPath = sandboxPath.String
	}
	if branchName.Valid {
		mission.BranchName = branchName.String
	}
	if iterationCount.Valid {
		mission.IterationCount = int(iterationCount.Int64)
	}
	if gatesStatus.Valid {
		mission.GatesStatus = gatesStatus.String
	}
	if goal.Valid {
		mission.Goal = goal.String
	}
	if context.Valid {
		mission.Context = context.String
	}
	if approvedAt.Valid {
		mission.ApprovedAt = &approvedAt.Time
	}
	if approvedBy.Valid {
		mission.ApprovedBy = approvedBy.String
	}

	return &mission, nil
}

// CreateIssue creates an issue in Beads + VC extension table if needed
func (s *VCStorage) CreateIssue(ctx context.Context, issue *types.Issue, actor string) error {
	// Convert to Beads type
	beadsIssue := vcIssueToBeads(issue)

	// Create in Beads
	if err := s.Storage.CreateIssue(ctx, beadsIssue, actor); err != nil {
		return err
	}

	// Copy generated ID back
	issue.ID = beadsIssue.ID

	// If this is a mission/phase, store in extension table
	if issue.IssueSubtype != "" && issue.IssueSubtype != types.SubtypeNormal {
		_, err := s.db.ExecContext(ctx, `
			INSERT INTO vc_mission_state (issue_id, subtype, created_at, updated_at)
			VALUES (?, ?, ?, ?)
		`, issue.ID, issue.IssueSubtype, time.Now(), time.Now())

		if err != nil {
			return fmt.Errorf("failed to create mission state: %w", err)
		}
	}

	return nil
}

// CreateMission creates a mission with full metadata
func (s *VCStorage) CreateMission(ctx context.Context, mission *types.Mission, actor string) error {
	// First create the base issue
	if err := s.CreateIssue(ctx, &mission.Issue, actor); err != nil {
		return err
	}

	// Update the mission state with all mission-specific fields
	_, err := s.db.ExecContext(ctx, `
		UPDATE vc_mission_state
		SET goal = ?, context = ?, phase_count = ?, current_phase = ?,
		    approval_required = ?, approved_at = ?, approved_by = ?,
		    updated_at = ?
		WHERE issue_id = ?
	`, mission.Goal, mission.Context, mission.PhaseCount, mission.CurrentPhase,
		mission.ApprovalRequired, mission.ApprovedAt, mission.ApprovedBy,
		time.Now(), mission.ID)

	if err != nil {
		return fmt.Errorf("failed to update mission metadata: %w", err)
	}

	return nil
}

// UpdateMission updates both base issue fields and mission-specific fields
func (s *VCStorage) UpdateMission(ctx context.Context, id string, updates map[string]interface{}, actor string) error {
	// Separate updates into base issue fields and mission-specific fields
	missionFields := map[string]interface{}{
		"approved_at": nil,
		"approved_by": nil,
		"goal":        nil,
		"context":     nil,
	}

	baseUpdates := make(map[string]interface{})
	missionUpdates := make(map[string]interface{})

	for key, value := range updates {
		if _, isMissionField := missionFields[key]; isMissionField {
			missionUpdates[key] = value
		} else {
			baseUpdates[key] = value
		}
	}

	// Update base issue fields if any
	if len(baseUpdates) > 0 {
		if err := s.Storage.UpdateIssue(ctx, id, baseUpdates, actor); err != nil {
			return fmt.Errorf("failed to update base issue fields: %w", err)
		}
	}

	// Update mission-specific fields if any
	if len(missionUpdates) > 0 {
		// Build dynamic UPDATE query
		setClauses := []string{}
		args := []interface{}{}

		for key, value := range missionUpdates {
			setClauses = append(setClauses, fmt.Sprintf("%s = ?", key))
			args = append(args, value)
		}

		setClauses = append(setClauses, "updated_at = ?")
		args = append(args, time.Now())
		args = append(args, id) // WHERE clause

		query := fmt.Sprintf(`
			UPDATE vc_mission_state
			SET %s
			WHERE issue_id = ?
		`, strings.Join(setClauses, ", "))

		if _, err := s.db.ExecContext(ctx, query, args...); err != nil {
			return fmt.Errorf("failed to update mission metadata: %w", err)
		}
	}

	return nil
}

// UpdateIssue updates issue fields in Beads
func (s *VCStorage) UpdateIssue(ctx context.Context, id string, updates map[string]interface{}, actor string) error {
	// Delegate to Beads (it handles all core issue fields)
	return s.Storage.UpdateIssue(ctx, id, updates, actor)
}

// CloseIssue closes an issue in Beads
func (s *VCStorage) CloseIssue(ctx context.Context, id string, reason string, actor string) error {
	return s.Storage.CloseIssue(ctx, id, reason, actor)
}

// SearchIssues searches issues in Beads
func (s *VCStorage) SearchIssues(ctx context.Context, query string, filter types.IssueFilter) ([]*types.Issue, error) {
	// Convert VC filter to Beads filter
	beadsFilter := beads.IssueFilter{
		Priority: filter.Priority,
		Assignee: filter.Assignee,
	}

	// Convert pointer fields if not nil
	if filter.Status != nil {
		beadsStatus := beads.Status(*filter.Status)
		beadsFilter.Status = &beadsStatus
	}
	if filter.Type != nil {
		beadsType := beads.IssueType(*filter.Type)
		beadsFilter.IssueType = &beadsType
	} else if filter.IssueType != nil {
		beadsType := beads.IssueType(*filter.IssueType)
		beadsFilter.IssueType = &beadsType
	}

	beadsIssues, err := s.Storage.SearchIssues(ctx, query, beadsFilter)
	if err != nil {
		return nil, err
	}

	// Convert back to VC types
	vcIssues := make([]*types.Issue, len(beadsIssues))
	for i, bi := range beadsIssues {
		vcIssues[i] = beadsIssueToVC(bi)
	}

	return vcIssues, nil
}

// ======================================================================
// DEPENDENCIES (delegate to Beads)
// ======================================================================

// AddDependency adds a dependency in Beads
func (s *VCStorage) AddDependency(ctx context.Context, dep *types.Dependency, actor string) error {
	beadsDep := &beads.Dependency{
		IssueID:     dep.IssueID,
		DependsOnID: dep.DependsOnID,
		Type:        beads.DependencyType(dep.Type),
	}
	return s.Storage.AddDependency(ctx, beadsDep, actor)
}

// RemoveDependency removes a dependency from Beads
func (s *VCStorage) RemoveDependency(ctx context.Context, issueID, dependsOnID string, actor string) error {
	return s.Storage.RemoveDependency(ctx, issueID, dependsOnID, actor)
}

// GetDependencies retrieves dependencies from Beads
func (s *VCStorage) GetDependencies(ctx context.Context, issueID string) ([]*types.Issue, error) {
	beadsIssues, err := s.Storage.GetDependencies(ctx, issueID)
	if err != nil {
		return nil, err
	}

	vcIssues := make([]*types.Issue, len(beadsIssues))
	for i, bi := range beadsIssues {
		vcIssues[i] = beadsIssueToVC(bi)
	}
	return vcIssues, nil
}

// GetDependents retrieves dependents from Beads
func (s *VCStorage) GetDependents(ctx context.Context, issueID string) ([]*types.Issue, error) {
	beadsIssues, err := s.Storage.GetDependents(ctx, issueID)
	if err != nil {
		return nil, err
	}

	vcIssues := make([]*types.Issue, len(beadsIssues))
	for i, bi := range beadsIssues {
		vcIssues[i] = beadsIssueToVC(bi)
	}
	return vcIssues, nil
}

// GetDependencyRecords retrieves dependency records from Beads
func (s *VCStorage) GetDependencyRecords(ctx context.Context, issueID string) ([]*types.Dependency, error) {
	beadsDeps, err := s.Storage.GetDependencyRecords(ctx, issueID)
	if err != nil {
		return nil, err
	}

	vcDeps := make([]*types.Dependency, len(beadsDeps))
	for i, bd := range beadsDeps {
		vcDeps[i] = &types.Dependency{
			IssueID:     bd.IssueID,
			DependsOnID: bd.DependsOnID,
			Type:        types.DependencyType(bd.Type),
		}
	}
	return vcDeps, nil
}

// GetDependencyTree retrieves dependency tree from Beads
func (s *VCStorage) GetDependencyTree(ctx context.Context, issueID string, maxDepth int) ([]*types.TreeNode, error) {
	beadsNodes, err := s.Storage.GetDependencyTree(ctx, issueID, maxDepth, false)
	if err != nil {
		return nil, err
	}

	vcNodes := make([]*types.TreeNode, len(beadsNodes))
	for i, bn := range beadsNodes {
		vcNodes[i] = &types.TreeNode{
			Issue:     *beadsIssueToVC(&bn.Issue),
			Depth:     bn.Depth,
			Truncated: bn.Truncated,
		}
	}
	return vcNodes, nil
}

// DetectCycles detects dependency cycles in Beads
func (s *VCStorage) DetectCycles(ctx context.Context) ([][]*types.Issue, error) {
	beadsCycles, err := s.Storage.DetectCycles(ctx)
	if err != nil {
		return nil, err
	}

	vcCycles := make([][]*types.Issue, len(beadsCycles))
	for i, cycle := range beadsCycles {
		vcCycle := make([]*types.Issue, len(cycle))
		for j, bi := range cycle {
			vcCycle[j] = beadsIssueToVC(bi)
		}
		vcCycles[i] = vcCycle
	}
	return vcCycles, nil
}

// ======================================================================
// LABELS (delegate to Beads)
// ======================================================================

// AddLabel, RemoveLabel, GetLabels delegate to Beads
// (These methods are already available via embedded beads.Storage)

// GetIssuesByLabel retrieves issues by label from Beads and converts to VC types
func (s *VCStorage) GetIssuesByLabel(ctx context.Context, label string) ([]*types.Issue, error) {
	beadsIssues, err := s.Storage.GetIssuesByLabel(ctx, label)
	if err != nil {
		return nil, err
	}

	vcIssues := make([]*types.Issue, len(beadsIssues))
	for i, bi := range beadsIssues {
		vcIssues[i] = beadsIssueToVC(bi)
	}
	return vcIssues, nil
}

// ======================================================================
// READY WORK & BLOCKING (delegate to Beads)
// ======================================================================

// GetReadyWork retrieves ready work from Beads
func (s *VCStorage) GetReadyWork(ctx context.Context, filter types.WorkFilter) ([]*types.Issue, error) {
	beadsFilter := beads.WorkFilter{
		Status:     beads.Status(filter.Status),
		Priority:   filter.Priority,
		Limit:      filter.Limit,
		SortPolicy: beads.SortPolicy(filter.SortPolicy), // Pass through sort policy (vc-190)
	}

	beadsIssues, err := s.Storage.GetReadyWork(ctx, beadsFilter)
	if err != nil {
		return nil, err
	}

	vcIssues := make([]*types.Issue, len(beadsIssues))
	for i, bi := range beadsIssues {
		vcIssues[i] = beadsIssueToVC(bi)
	}
	return vcIssues, nil
}

// GetBlockedIssues retrieves blocked issues from Beads
func (s *VCStorage) GetBlockedIssues(ctx context.Context) ([]*types.BlockedIssue, error) {
	beadsBlocked, err := s.Storage.GetBlockedIssues(ctx)
	if err != nil {
		return nil, err
	}

	vcBlocked := make([]*types.BlockedIssue, len(beadsBlocked))
	for i, bb := range beadsBlocked {
		vcBlocked[i] = &types.BlockedIssue{
			Issue:          *beadsIssueToVC(&bb.Issue),
			BlockedByCount: bb.BlockedByCount,
			BlockedBy:      bb.BlockedBy,
		}
	}
	return vcBlocked, nil
}

// GetReadyBlockers retrieves blocker issues that are ready to execute
// This is an optimized query that filters for label='discovered:blocker' AND status='open'
// and checks for open blocking dependencies in a single SQL query (vc-156)
func (s *VCStorage) GetReadyBlockers(ctx context.Context, limit int) ([]*types.Issue, error) {
	// Optimized single SQL query that:
	// 1. Filters for issues with discovered:blocker label
	// 2. Filters for status='open'
	// 3. LEFT JOINs to check for open blocking dependencies
	// 4. Returns only issues with NO open blockers (ready to execute)
	// 5. Orders by priority (lower = higher priority)
	query := `
		SELECT DISTINCT i.id, i.title, i.description, i.design, i.acceptance_criteria,
		       i.notes, i.status, i.priority, i.issue_type, i.assignee,
		       i.estimated_minutes, i.created_at, i.updated_at, i.closed_at
		FROM issues i
		INNER JOIN labels l ON i.id = l.issue_id
		WHERE l.label = 'discovered:blocker'
		  AND i.status = 'open'
		  AND NOT EXISTS (
		    -- Check if this issue has any open blocking dependencies (vc-157)
		    -- Only check type='blocks', not related/parent-child/discovered-from
		    SELECT 1 FROM dependencies d
		    INNER JOIN issues dep_issue ON d.depends_on_id = dep_issue.id
		    WHERE d.issue_id = i.id
		      AND d.type = 'blocks'
		      AND dep_issue.status != 'closed'
		  )
		ORDER BY i.priority ASC
		LIMIT ?
	`

	rows, err := s.db.QueryContext(ctx, query, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to query ready blockers: %w", err)
	}
	defer rows.Close()

	var issues []*types.Issue
	for rows.Next() {
		var issue types.Issue
		var closedAt sql.NullTime
		var assignee sql.NullString
		var estimatedMinutes sql.NullInt64

		err := rows.Scan(
			&issue.ID,
			&issue.Title,
			&issue.Description,
			&issue.Design,
			&issue.AcceptanceCriteria,
			&issue.Notes,
			&issue.Status,
			&issue.Priority,
			&issue.IssueType,
			&assignee,
			&estimatedMinutes,
			&issue.CreatedAt,
			&issue.UpdatedAt,
			&closedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan issue: %w", err)
		}

		// Handle nullable fields
		if closedAt.Valid {
			issue.ClosedAt = &closedAt.Time
		}
		if assignee.Valid {
			issue.Assignee = assignee.String
		}
		if estimatedMinutes.Valid {
			val := int(estimatedMinutes.Int64)
			issue.EstimatedMinutes = &val
		}

		issues = append(issues, &issue)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating rows: %w", err)
	}

	return issues, nil
}

// ======================================================================
// EVENTS & COMMENTS (delegate to Beads)
// ======================================================================

// AddComment delegates to Beads (already available via embedded beads.Storage)

// GetEvents retrieves events from Beads and converts to VC types
func (s *VCStorage) GetEvents(ctx context.Context, issueID string, limit int) ([]*types.Event, error) {
	beadsEvents, err := s.Storage.GetEvents(ctx, issueID, limit)
	if err != nil {
		return nil, err
	}

	vcEvents := make([]*types.Event, len(beadsEvents))
	for i, be := range beadsEvents {
		vcEvents[i] = &types.Event{
			ID:        be.ID,
			IssueID:   be.IssueID,
			EventType: types.EventType(be.EventType),
			Actor:     be.Actor,
			OldValue:  be.OldValue,
			NewValue:  be.NewValue,
			Comment:   be.Comment,
			CreatedAt: be.CreatedAt,
		}
	}
	return vcEvents, nil
}

// ======================================================================
// STATISTICS (delegate to Beads)
// ======================================================================

// GetStatistics retrieves statistics from Beads
func (s *VCStorage) GetStatistics(ctx context.Context) (*types.Statistics, error) {
	beadsStats, err := s.Storage.GetStatistics(ctx)
	if err != nil {
		return nil, err
	}

	return &types.Statistics{
		TotalIssues:      beadsStats.TotalIssues,
		OpenIssues:       beadsStats.OpenIssues,
		InProgressIssues: beadsStats.InProgressIssues,
		ClosedIssues:     beadsStats.ClosedIssues,
		BlockedIssues:    beadsStats.BlockedIssues,
		ReadyIssues:      beadsStats.ReadyIssues, // vc-166: Include ready issues count
	}, nil
}

// ======================================================================
// EVENT CLEANUP (VC extension methods)
// ======================================================================

// CleanupEventsByAge cleans up old events from vc_agent_events table
func (s *VCStorage) CleanupEventsByAge(ctx context.Context, retentionDays, criticalRetentionDays, batchSize int) (int, error) {
	if retentionDays < 0 || criticalRetentionDays < 0 {
		return 0, fmt.Errorf("retention days cannot be negative")
	}
	if batchSize < 1 {
		return 0, fmt.Errorf("batch size must be at least 1")
	}

	totalDeleted := 0

	// Step 1: Delete old regular events (severity = info or warning)
	regularCutoff := time.Now().AddDate(0, 0, -retentionDays)
	deleted, err := s.deleteOldEventsBatch(ctx, regularCutoff, []string{"info", "warning"}, batchSize)
	if err != nil {
		return totalDeleted, fmt.Errorf("failed to delete old regular events: %w", err)
	}
	totalDeleted += deleted

	// Step 2: Delete old critical events (severity = error or critical)
	// Only if critical retention is different from regular retention
	if criticalRetentionDays != retentionDays {
		criticalCutoff := time.Now().AddDate(0, 0, -criticalRetentionDays)
		deleted, err = s.deleteOldEventsBatch(ctx, criticalCutoff, []string{"error", "critical"}, batchSize)
		if err != nil {
			return totalDeleted, fmt.Errorf("failed to delete old critical events: %w", err)
		}
		totalDeleted += deleted
	}

	return totalDeleted, nil
}

// deleteOldEventsBatch deletes events older than cutoff with specified severities in batches
func (s *VCStorage) deleteOldEventsBatch(ctx context.Context, cutoff time.Time, severities []string, batchSize int) (int, error) {
	totalDeleted := 0

	for {
		// Check context cancellation
		select {
		case <-ctx.Done():
			return totalDeleted, ctx.Err()
		default:
		}

		// Build severity IN clause
		severityPlaceholders := ""
		args := []interface{}{cutoff}
		for i, sev := range severities {
			if i > 0 {
				severityPlaceholders += ", "
			}
			severityPlaceholders += "?"
			args = append(args, sev)
		}
		args = append(args, batchSize)

		// Delete a batch
		query := fmt.Sprintf(`
			DELETE FROM vc_agent_events
			WHERE id IN (
				SELECT id FROM vc_agent_events
				WHERE timestamp < ?
				AND severity IN (%s)
				ORDER BY timestamp ASC
				LIMIT ?
			)
		`, severityPlaceholders)

		result, err := s.db.ExecContext(ctx, query, args...)
		if err != nil {
			return totalDeleted, fmt.Errorf("failed to execute delete: %w", err)
		}

		rowsAffected, err := result.RowsAffected()
		if err != nil {
			return totalDeleted, fmt.Errorf("failed to get rows affected: %w", err)
		}

		totalDeleted += int(rowsAffected)

		// If we deleted fewer than batchSize, we're done
		if rowsAffected < int64(batchSize) {
			break
		}
	}

	return totalDeleted, nil
}

// CleanupEventsByIssueLimit limits events per issue
func (s *VCStorage) CleanupEventsByIssueLimit(ctx context.Context, perIssueLimit, batchSize int) (int, error) {
	if perIssueLimit < 0 {
		return 0, fmt.Errorf("per-issue limit cannot be negative")
	}
	if perIssueLimit == 0 {
		// 0 means unlimited
		return 0, nil
	}
	if batchSize < 1 {
		return 0, fmt.Errorf("batch size must be at least 1")
	}

	totalDeleted := 0

	// Find issues exceeding the limit
	query := `
		SELECT issue_id, COUNT(*) as event_count
		FROM vc_agent_events
		GROUP BY issue_id
		HAVING event_count > ?
	`

	rows, err := s.db.QueryContext(ctx, query, perIssueLimit)
	if err != nil {
		return 0, fmt.Errorf("failed to query issue event counts: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var issues []struct {
		issueID    string
		eventCount int
	}

	for rows.Next() {
		var issueID string
		var count int
		if err := rows.Scan(&issueID, &count); err != nil {
			return totalDeleted, fmt.Errorf("failed to scan issue count: %w", err)
		}
		issues = append(issues, struct {
			issueID    string
			eventCount int
		}{issueID, count})
	}

	if err := rows.Err(); err != nil {
		return totalDeleted, fmt.Errorf("error iterating issue counts: %w", err)
	}

	// For each issue exceeding the limit, delete oldest non-critical events
	for _, issue := range issues {
		// Check context cancellation
		select {
		case <-ctx.Done():
			return totalDeleted, ctx.Err()
		default:
		}

		eventsToDelete := issue.eventCount - perIssueLimit
		if eventsToDelete <= 0 {
			continue
		}

		deleted, err := s.deleteOldestEventsForIssue(ctx, issue.issueID, eventsToDelete, batchSize)
		if err != nil {
			return totalDeleted, fmt.Errorf("failed to delete events for issue %s: %w", issue.issueID, err)
		}
		totalDeleted += deleted
	}

	return totalDeleted, nil
}

// deleteOldestEventsForIssue deletes the oldest non-critical events for a specific issue
func (s *VCStorage) deleteOldestEventsForIssue(ctx context.Context, issueID string, count, batchSize int) (int, error) {
	totalDeleted := 0
	remaining := count

	for remaining > 0 {
		// Check context cancellation
		select {
		case <-ctx.Done():
			return totalDeleted, ctx.Err()
		default:
		}

		// Delete up to batchSize events
		limitThisBatch := batchSize
		if remaining < batchSize {
			limitThisBatch = remaining
		}

		query := `
			DELETE FROM vc_agent_events
			WHERE id IN (
				SELECT id FROM vc_agent_events
				WHERE issue_id = ?
				AND severity NOT IN ('error', 'critical')
				ORDER BY timestamp ASC
				LIMIT ?
			)
		`

		result, err := s.db.ExecContext(ctx, query, issueID, limitThisBatch)
		if err != nil {
			return totalDeleted, fmt.Errorf("failed to execute delete: %w", err)
		}

		rowsAffected, err := result.RowsAffected()
		if err != nil {
			return totalDeleted, fmt.Errorf("failed to get rows affected: %w", err)
		}

		totalDeleted += int(rowsAffected)
		remaining -= int(rowsAffected)

		// If we deleted fewer than requested, no more non-critical events to delete
		if rowsAffected < int64(limitThisBatch) {
			break
		}
	}

	return totalDeleted, nil
}

// CleanupEventsByGlobalLimit enforces global event limit
func (s *VCStorage) CleanupEventsByGlobalLimit(ctx context.Context, globalLimit, batchSize int) (int, error) {
	if globalLimit < 1 {
		return 0, fmt.Errorf("global limit must be at least 1")
	}
	if batchSize < 1 {
		return 0, fmt.Errorf("batch size must be at least 1")
	}

	// Get current event count
	var currentCount int
	err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM vc_agent_events").Scan(&currentCount)
	if err != nil {
		return 0, fmt.Errorf("failed to get event count: %w", err)
	}

	// If under the limit, nothing to do
	if currentCount <= globalLimit {
		return 0, nil
	}

	eventsToDelete := currentCount - globalLimit
	totalDeleted := 0

	// Delete oldest non-critical events in batches
	for eventsToDelete > 0 {
		// Check context cancellation
		select {
		case <-ctx.Done():
			return totalDeleted, ctx.Err()
		default:
		}

		// Delete up to batchSize events
		limitThisBatch := batchSize
		if eventsToDelete < batchSize {
			limitThisBatch = eventsToDelete
		}

		query := `
			DELETE FROM vc_agent_events
			WHERE id IN (
				SELECT id FROM vc_agent_events
				WHERE severity NOT IN ('error', 'critical')
				ORDER BY timestamp ASC
				LIMIT ?
			)
		`

		result, err := s.db.ExecContext(ctx, query, limitThisBatch)
		if err != nil {
			return totalDeleted, fmt.Errorf("failed to execute delete: %w", err)
		}

		rowsAffected, err := result.RowsAffected()
		if err != nil {
			return totalDeleted, fmt.Errorf("failed to get rows affected: %w", err)
		}

		totalDeleted += int(rowsAffected)
		eventsToDelete -= int(rowsAffected)

		// If we deleted fewer than requested, no more non-critical events to delete
		if rowsAffected < int64(limitThisBatch) {
			break
		}
	}

	return totalDeleted, nil
}

// GetEventCounts returns event statistics
func (s *VCStorage) GetEventCounts(ctx context.Context) (*types.EventCounts, error) {
	counts := &types.EventCounts{
		EventsByIssue:    make(map[string]int),
		EventsBySeverity: make(map[string]int),
		EventsByType:     make(map[string]int),
	}

	// Total events
	err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM vc_agent_events").Scan(&counts.TotalEvents)
	if err != nil {
		return nil, fmt.Errorf("failed to get total event count: %w", err)
	}

	// Events by issue
	rows, err := s.db.QueryContext(ctx, `
		SELECT issue_id, COUNT(*)
		FROM vc_agent_events
		GROUP BY issue_id
	`)
	if err != nil {
		return nil, fmt.Errorf("failed to query events by issue: %w", err)
	}
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		var issueID sql.NullString
		var count int
		if err := rows.Scan(&issueID, &count); err != nil {
			return nil, fmt.Errorf("failed to scan issue count: %w", err)
		}
		// Use empty string for NULL issue_id (system events)
		key := ""
		if issueID.Valid {
			key = issueID.String
		}
		counts.EventsByIssue[key] = count
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating issue counts: %w", err)
	}

	// Events by severity
	// vc-127: Use COALESCE to handle NULL severity values
	rows, err = s.db.QueryContext(ctx, `
		SELECT COALESCE(severity, 'unknown'), COUNT(*)
		FROM vc_agent_events
		GROUP BY severity
	`)
	if err != nil {
		return nil, fmt.Errorf("failed to query events by severity: %w", err)
	}
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		var severity string
		var count int
		if err := rows.Scan(&severity, &count); err != nil {
			return nil, fmt.Errorf("failed to scan severity count: %w", err)
		}
		counts.EventsBySeverity[severity] = count
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating severity counts: %w", err)
	}

	// Events by type
	rows, err = s.db.QueryContext(ctx, `
		SELECT type, COUNT(*)
		FROM vc_agent_events
		GROUP BY type
	`)
	if err != nil {
		return nil, fmt.Errorf("failed to query events by type: %w", err)
	}
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		var eventType string
		var count int
		if err := rows.Scan(&eventType, &count); err != nil {
			return nil, fmt.Errorf("failed to scan type count: %w", err)
		}
		counts.EventsByType[eventType] = count
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating type counts: %w", err)
	}

	return counts, nil
}

// VacuumDatabase runs VACUUM on the database
func (s *VCStorage) VacuumDatabase(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, "VACUUM")
	return err
}
