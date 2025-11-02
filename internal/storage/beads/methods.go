package beads

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/steveyegge/beads"
	"github.com/steveyegge/vc/internal/events"
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

// GetIssues retrieves multiple issues by IDs in a single batch query (vc-58)
// Returns a map of issueID -> Issue for issues that exist
// Missing issues are omitted from the result map (not an error)
// vc-4573: Enforces a maximum batch size of 500 to prevent SQLite variable overflow
func (s *VCStorage) GetIssues(ctx context.Context, ids []string) (map[string]*types.Issue, error) {
	if len(ids) == 0 {
		return make(map[string]*types.Issue), nil
	}

	// vc-4573: SQLite has a limit of 999 variables per query (SQLITE_MAX_VARIABLE_NUMBER)
	// We use 2 queries (issues + subtypes), each with len(ids) variables, so max safe limit is ~499
	// Set limit to 500 for safety margin
	const maxBatchSize = 500
	if len(ids) > maxBatchSize {
		return nil, fmt.Errorf("batch size %d exceeds maximum of %d (SQLite variable limit)", len(ids), maxBatchSize)
	}

	// Build placeholders for IN clause
	placeholders := make([]string, len(ids))
	args := make([]interface{}, len(ids))
	for i, id := range ids {
		placeholders[i] = "?"
		args[i] = id
	}

	// Query 1: Get all core issues from Beads in one query
	query := fmt.Sprintf(`
		SELECT id, title, description, status, priority, issue_type, created_at, updated_at,
		       closed_at, assignee, estimated_minutes, notes, design, acceptance_criteria
		FROM issues
		WHERE id IN (%s)
	`, strings.Join(placeholders, ", "))

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query issues: %w", err)
	}
	defer rows.Close()

	// Build initial map from Beads data
	result := make(map[string]*types.Issue)
	for rows.Next() {
		var issue types.Issue
		var createdAt, updatedAt time.Time
		var closedAt sql.NullTime
		var assignee, notes, design, acceptanceCriteria sql.NullString
		var estimatedMinutes sql.NullInt64

		if err := rows.Scan(
			&issue.ID,
			&issue.Title,
			&issue.Description,
			&issue.Status,
			&issue.Priority,
			&issue.IssueType,
			&createdAt,
			&updatedAt,
			&closedAt,
			&assignee,
			&estimatedMinutes,
			&notes,
			&design,
			&acceptanceCriteria,
		); err != nil {
			return nil, fmt.Errorf("failed to scan issue: %w", err)
		}

		// Set timestamps
		issue.CreatedAt = createdAt
		issue.UpdatedAt = updatedAt
		if closedAt.Valid {
			issue.ClosedAt = &closedAt.Time
		}

		// Handle nullable fields
		if assignee.Valid {
			issue.Assignee = assignee.String
		}
		if estimatedMinutes.Valid {
			mins := int(estimatedMinutes.Int64)
			issue.EstimatedMinutes = &mins
		}
		if notes.Valid {
			issue.Notes = notes.String
		}
		if design.Valid {
			issue.Design = design.String
		}
		if acceptanceCriteria.Valid {
			issue.AcceptanceCriteria = acceptanceCriteria.String
		}

		result[issue.ID] = &issue
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating issue rows: %w", err)
	}

	// Query 2: Batch-load subtypes from VC extension table
	subtypeQuery := fmt.Sprintf(`
		SELECT issue_id, subtype
		FROM vc_mission_state
		WHERE issue_id IN (%s)
	`, strings.Join(placeholders, ", "))

	subtypeRows, err := s.db.QueryContext(ctx, subtypeQuery, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query subtypes: %w", err)
	}
	defer subtypeRows.Close()

	// Enrich issues with subtypes
	for subtypeRows.Next() {
		var issueID string
		var subtype string
		if err := subtypeRows.Scan(&issueID, &subtype); err != nil {
			return nil, fmt.Errorf("failed to scan subtype: %w", err)
		}

		if issue, exists := result[issueID]; exists {
			issue.IssueSubtype = types.IssueSubtype(subtype)
		}
	}

	if err := subtypeRows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating subtype rows: %w", err)
	}

	return result, nil
}

// GetMission retrieves a mission issue with full metadata
func (s *VCStorage) GetMission(ctx context.Context, id string) (*types.Mission, error) {
	// Get base issue
	issue, err := s.GetIssue(ctx, id)
	if err != nil {
		return nil, err
	}
	if issue == nil {
		return nil, fmt.Errorf("issue %s not found", id)
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
	// Handle gates_status: empty string -> NULL for database constraint
	var gatesStatus interface{} = mission.GatesStatus
	if mission.GatesStatus == "" {
		gatesStatus = nil
	}

	_, err := s.db.ExecContext(ctx, `
		UPDATE vc_mission_state
		SET goal = ?, context = ?, phase_count = ?, current_phase = ?,
		    approval_required = ?, approved_at = ?, approved_by = ?,
		    sandbox_path = ?, branch_name = ?,
		    iteration_count = ?, gates_status = ?,
		    updated_at = ?
		WHERE issue_id = ?
	`, mission.Goal, mission.Context, mission.PhaseCount, mission.CurrentPhase,
		mission.ApprovalRequired, mission.ApprovedAt, mission.ApprovedBy,
		mission.SandboxPath, mission.BranchName,
		mission.IterationCount, gatesStatus,
		time.Now(), mission.ID)

	if err != nil {
		return fmt.Errorf("failed to update mission metadata: %w", err)
	}

	// Emit mission_created event (vc-266)
	// Find parent epic if this mission has dependencies
	var parentEpicID string
	deps, err := s.GetDependencies(ctx, mission.ID)
	if err == nil && len(deps) > 0 {
		// Find first epic parent (if any)
		for _, dep := range deps {
			if dep.IssueType == types.TypeEpic {
				parentEpicID = dep.ID
				break
			}
		}
	}

	eventData := events.MissionCreatedData{
		MissionID:        mission.ID,
		ParentEpicID:     parentEpicID,
		Goal:             mission.Goal,
		PhaseCount:       mission.PhaseCount,
		ApprovalRequired: mission.ApprovalRequired,
		Actor:            actor,
	}

	event := &events.AgentEvent{
		ID:        uuid.New().String(),
		Type:      events.EventTypeMissionCreated,
		Timestamp: time.Now(),
		IssueID:   mission.ID,
		Severity:  events.SeverityInfo,
		Message:   fmt.Sprintf("Mission created: %s (goal: %s, phases: %d)", mission.ID, mission.Goal, mission.PhaseCount),
		Data: map[string]interface{}{
			"mission_id":        eventData.MissionID,
			"parent_epic_id":    eventData.ParentEpicID,
			"goal":              eventData.Goal,
			"phase_count":       eventData.PhaseCount,
			"approval_required": eventData.ApprovalRequired,
			"actor":             eventData.Actor,
		},
	}

	if err := s.StoreAgentEvent(ctx, event); err != nil {
		// Log warning but don't fail mission creation
		fmt.Fprintf(os.Stderr, "Warning: failed to store mission_created event for %s: %v\n", mission.ID, err)
	}

	return nil
}

// UpdateMission updates both base issue fields and mission-specific fields
func (s *VCStorage) UpdateMission(ctx context.Context, id string, updates map[string]interface{}, actor string) error {
	// Get old values for event tracking (vc-266)
	oldMission, err := s.GetMission(ctx, id)
	if err != nil {
		return fmt.Errorf("failed to get current mission state: %w", err)
	}

	// Separate updates into base issue fields and mission-specific fields
	missionFields := map[string]interface{}{
		"approved_at":       nil,
		"approved_by":       nil,
		"goal":              nil,
		"context":           nil,
		"sandbox_path":      nil,
		"branch_name":       nil,
		"phase_count":       nil,
		"current_phase":     nil,
		"approval_required": nil,
		"iteration_count":   nil,
		"gates_status":      nil,
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

	// Emit mission_metadata_updated event if there are changes (vc-266)
	if len(updates) > 0 {
		updatedFields := make([]string, 0, len(updates))
		changes := make(map[string]events.FieldChange)

		for key, newValue := range updates {
			updatedFields = append(updatedFields, key)

			// Get old value
			var oldValue interface{}
			switch key {
			case "approved_at":
				oldValue = oldMission.ApprovedAt
			case "approved_by":
				oldValue = oldMission.ApprovedBy
			case "goal":
				oldValue = oldMission.Goal
			case "context":
				oldValue = oldMission.Context
			case "sandbox_path":
				oldValue = oldMission.SandboxPath
			case "branch_name":
				oldValue = oldMission.BranchName
			case "status":
				oldValue = oldMission.Status
			case "priority":
				oldValue = oldMission.Priority
			case "phase_count":
				oldValue = oldMission.PhaseCount
			case "current_phase":
				oldValue = oldMission.CurrentPhase
			case "approval_required":
				oldValue = oldMission.ApprovalRequired
			case "iteration_count":
				oldValue = oldMission.IterationCount
			case "gates_status":
				oldValue = oldMission.GatesStatus
			}

			changes[key] = events.FieldChange{
				OldValue: oldValue,
				NewValue: newValue,
			}
		}

		eventData := events.MissionMetadataUpdatedData{
			MissionID:     id,
			UpdatedFields: updatedFields,
			Changes:       changes,
			Actor:         actor,
		}

		// Convert changes to map[string]interface{} for event storage
		changesMap := make(map[string]interface{})
		for k, v := range changes {
			changesMap[k] = map[string]interface{}{
				"old_value": v.OldValue,
				"new_value": v.NewValue,
			}
		}

		event := &events.AgentEvent{
			ID:        uuid.New().String(),
			Type:      events.EventTypeMissionMetadataUpdated,
			Timestamp: time.Now(),
			IssueID:   id,
			Severity:  events.SeverityInfo,
			Message:   fmt.Sprintf("Mission metadata updated: %s (fields: %v)", id, updatedFields),
			Data: map[string]interface{}{
				"mission_id":      eventData.MissionID,
				"updated_fields":  eventData.UpdatedFields,
				"changes":         changesMap,
				"actor":           eventData.Actor,
			},
		}

		if err := s.StoreAgentEvent(ctx, event); err != nil {
			// Log warning but don't fail mission update
			fmt.Fprintf(os.Stderr, "Warning: failed to store mission_metadata_updated event for %s: %v\n", id, err)
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
	beadsNodes, err := s.Storage.GetDependencyTree(ctx, issueID, maxDepth, false, false)
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

// GetReadyWork retrieves ready work from Beads with mission context (vc-234)
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

	// vc-203: Filter out epics - they are tracking/meta issues, not executable work
	// vc-185: Filter out blocked/in_progress issues - only return truly available work
	vcIssues := make([]*types.Issue, 0, len(beadsIssues))
	for _, bi := range beadsIssues {
		if bi.IssueType == beads.TypeEpic {
			continue // Skip epics
		}
		if bi.Status == beads.StatusBlocked {
			continue // Skip blocked issues (vc-185)
		}
		if bi.Status == beads.StatusInProgress {
			continue // Skip in_progress issues (vc-185)
		}
		vcIssues = append(vcIssues, beadsIssueToVC(bi))
	}

	// vc-4ec0: Batch-load labels for all issues to check for 'no-auto-claim' (avoid N+1)
	issueIDs := make(map[string]bool, len(vcIssues))
	for _, issue := range vcIssues {
		issueIDs[issue.ID] = true
	}

	issueLabels, err := s.batchLoadLabels(ctx, issueIDs)
	if err != nil {
		return nil, fmt.Errorf("failed to batch-load issue labels: %w", err)
	}

	// Filter out issues with 'no-auto-claim' label
	filteredIssues := make([]*types.Issue, 0, len(vcIssues))
	for _, issue := range vcIssues {
		labels := issueLabels[issue.ID]
		hasNoAutoClaim := false
		for _, label := range labels {
			if label == "no-auto-claim" {
				hasNoAutoClaim = true
				break
			}
		}
		if !hasNoAutoClaim {
			filteredIssues = append(filteredIssues, issue)
		}
	}

	// vc-234: Enrich with mission context and filter by mission active state
	return s.enrichWithMissionContext(ctx, filteredIssues)
}

// enrichWithMissionContext populates mission context for each issue and filters out
// issues from missions with needs-quality-gates label (vc-234, vc-239)
func (s *VCStorage) enrichWithMissionContext(ctx context.Context, issues []*types.Issue) ([]*types.Issue, error) {
	if len(issues) == 0 {
		return issues, nil
	}

	// Cache to avoid N+1 queries: map[missionID]missionContext
	missionCache := make(map[string]*types.MissionContext)
	// Track unique mission IDs for batch label loading
	uniqueMissionIDs := make(map[string]bool)

	// First pass: get mission context for all issues and collect unique mission IDs
	issuesWithMissions := make([]*types.Issue, 0, len(issues))
	for _, issue := range issues {
		// Try to get mission context (may fail if task is not part of a mission)
		missionCtx, err := s.getMissionForTaskCached(ctx, issue.ID, missionCache)
		if err != nil {
			// Task is not part of a mission - include it without mission context
			issuesWithMissions = append(issuesWithMissions, issue)
			continue
		}

		// Track this mission ID for batch loading
		uniqueMissionIDs[missionCtx.MissionID] = true

		// Attach mission context (will filter later based on labels)
		issue.MissionContext = missionCtx
		issuesWithMissions = append(issuesWithMissions, issue)
	}

	// Batch-load labels for all unique missions in one query (vc-239)
	missionLabels, err := s.batchLoadLabels(ctx, uniqueMissionIDs)
	if err != nil {
		return nil, fmt.Errorf("failed to batch-load mission labels: %w", err)
	}

	// Second pass: filter out issues from missions with needs-quality-gates label
	result := make([]*types.Issue, 0, len(issuesWithMissions))
	for _, issue := range issuesWithMissions {
		// If issue has no mission context, include it
		if issue.MissionContext == nil {
			result = append(result, issue)
			continue
		}

		// Check if mission has needs-quality-gates label
		labels := missionLabels[issue.MissionContext.MissionID]
		hasNeedsGates := false
		for _, label := range labels {
			if label == "needs-quality-gates" {
				hasNeedsGates = true
				break
			}
		}

		if !hasNeedsGates {
			// Mission doesn't have needs-quality-gates - include this task
			result = append(result, issue)
		}
		// Otherwise skip this task (mission is waiting for quality gates)
	}

	return result, nil
}

// batchLoadLabels loads labels for multiple issues in a single query (vc-239)
func (s *VCStorage) batchLoadLabels(ctx context.Context, issueIDs map[string]bool) (map[string][]string, error) {
	if len(issueIDs) == 0 {
		return make(map[string][]string), nil
	}

	// Build IN clause for SQL query
	ids := make([]string, 0, len(issueIDs))
	for id := range issueIDs {
		ids = append(ids, id)
	}

	// Build placeholders for IN clause
	placeholders := make([]string, len(ids))
	args := make([]interface{}, len(ids))
	for i, id := range ids {
		placeholders[i] = "?"
		args[i] = id
	}

	// Single query to get all labels for all missions
	query := fmt.Sprintf(`
		SELECT issue_id, label
		FROM labels
		WHERE issue_id IN (%s)
	`, strings.Join(placeholders, ", "))

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query labels: %w", err)
	}
	defer rows.Close()

	// Build map of issueID -> []label
	result := make(map[string][]string)
	for rows.Next() {
		var issueID, label string
		if err := rows.Scan(&issueID, &label); err != nil {
			return nil, fmt.Errorf("failed to scan label: %w", err)
		}
		result[issueID] = append(result[issueID], label)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating label rows: %w", err)
	}

	return result, nil
}

// getMissionForTaskCached gets mission context with caching to avoid N+1 queries
func (s *VCStorage) getMissionForTaskCached(ctx context.Context, taskID string, cache map[string]*types.MissionContext) (*types.MissionContext, error) {
	// Walk up the dependency tree to find mission
	missionCtx, err := s.GetMissionForTask(ctx, taskID)
	if err != nil {
		return nil, err
	}

	// Check if we've already loaded this mission's full context
	if cached, ok := cache[missionCtx.MissionID]; ok {
		return cached, nil
	}

	// Cache it for future lookups
	cache[missionCtx.MissionID] = missionCtx
	return missionCtx, nil
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
	// 3. Filters out epics (vc-203)
	// 4. LEFT JOINs to check for open blocking dependencies
	// 5. Returns only issues with NO open blockers (ready to execute)
	// 6. Orders by priority (lower = higher priority)
	query := `
		SELECT DISTINCT i.id, i.title, i.description, i.design, i.acceptance_criteria,
		       i.notes, i.status, i.priority, i.issue_type, i.assignee,
		       i.estimated_minutes, i.created_at, i.updated_at, i.closed_at
		FROM issues i
		INNER JOIN labels l ON i.id = l.issue_id
		WHERE l.label = 'discovered:blocker'
		  AND i.status = 'open'
		  AND i.issue_type != 'epic'
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

// ======================================================================
// EPIC COMPLETION (vc-232, vc-233)
// ======================================================================

// GetMissionForTask walks up the dependency tree to find the parent mission epic
// Uses recursive CTE to find mission in single query instead of N iterations (vc-238)
// Returns mission context with ID, sandbox path, and branch name
func (s *VCStorage) GetMissionForTask(ctx context.Context, taskID string) (*types.MissionContext, error) {
	// Use recursive CTE to walk parent-child dependencies upward
	// This replaces the iterative loop with a single database query
	query := `
		WITH RECURSIVE parent_chain AS (
		  -- Base case: start with the task's immediate parents
		  SELECT d.issue_id, d.depends_on_id, 1 as depth
		  FROM dependencies d
		  WHERE d.issue_id = ? AND d.type = ?

		  UNION ALL

		  -- Recursive case: walk up to parent's parents
		  SELECT d.issue_id, d.depends_on_id, p.depth + 1
		  FROM dependencies d
		  JOIN parent_chain p ON d.issue_id = p.depends_on_id
		  WHERE d.type = ? AND p.depth < 10  -- Prevent infinite loops
		)
		-- Find the first mission epic in the parent chain
		SELECT i.id, i.issue_type, COALESCE(m.subtype, '') as subtype,
		       COALESCE(m.sandbox_path, '') as sandbox_path,
		       COALESCE(m.branch_name, '') as branch_name
		FROM issues i
		JOIN parent_chain p ON i.id = p.depends_on_id
		LEFT JOIN vc_mission_state m ON i.id = m.issue_id
		WHERE i.issue_type = ?
		  AND m.subtype = ?
		ORDER BY p.depth ASC  -- Closest parent first
		LIMIT 1
	`

	var missionID, issueType, subtype, sandboxPath, branchName string
	err := s.db.QueryRowContext(ctx, query,
		taskID, types.DepParentChild, // Base case parameters
		types.DepParentChild, // Recursive case parameter
		types.TypeEpic, types.SubtypeMission, // WHERE clause parameters
	).Scan(&missionID, &issueType, &subtype, &sandboxPath, &branchName)

	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("task %s is not part of a mission (no parent-child dependency to mission epic)", taskID)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to query mission for task %s: %w", taskID, err)
	}

	return &types.MissionContext{
		MissionID:   missionID,
		SandboxPath: sandboxPath,
		BranchName:  branchName,
	}, nil
}

// IsEpicComplete checks if an epic is complete
// An epic is complete when:
// 1. All child issues (via parent-child dependencies) are closed
// 2. There are no open blocking dependencies
// Optimized to use single JOIN queries instead of N+1 loops (vc-237)
func (s *VCStorage) IsEpicComplete(ctx context.Context, epicID string) (bool, error) {
	// Check 1: Count children that are NOT closed
	var openChildrenCount int
	err := s.db.QueryRowContext(ctx, `
		SELECT COUNT(*) as open_children
		FROM dependencies d
		JOIN issues i ON d.issue_id = i.id
		WHERE d.depends_on_id = ?
		  AND d.type = ?
		  AND i.status != ?
	`, epicID, types.DepParentChild, types.StatusClosed).Scan(&openChildrenCount)
	if err != nil {
		return false, fmt.Errorf("failed to count open children for epic %s: %w", epicID, err)
	}

	// If any children are not closed, epic is not complete
	if openChildrenCount > 0 {
		return false, nil
	}

	// Check 2: Count blockers that are NOT closed
	var openBlockersCount int
	err = s.db.QueryRowContext(ctx, `
		SELECT COUNT(*) as open_blockers
		FROM dependencies d
		JOIN issues i ON d.depends_on_id = i.id
		WHERE d.issue_id = ?
		  AND d.type = ?
		  AND i.status != ?
	`, epicID, types.DepBlocks, types.StatusClosed).Scan(&openBlockersCount)
	if err != nil {
		return false, fmt.Errorf("failed to count open blockers for epic %s: %w", epicID, err)
	}

	// If any blockers are open, epic is not complete
	if openBlockersCount > 0 {
		return false, nil
	}

	// All children closed and no open blockers - epic is complete
	return true, nil
}

// ======================================================================
// QUALITY GATE WORKERS (vc-252)
// ======================================================================

// GetMissionsNeedingGates queries for missions with 'needs-quality-gates' label
// Used by QualityGateWorker to find missions ready for quality gate execution.
//
// Query logic:
// - SELECT missions (type=epic, subtype=mission)
// - WHERE has 'needs-quality-gates' label
// - AND does NOT have 'gates-running' label (already claimed)
// - ORDER BY priority ASC (P0 before P2, i.e., most urgent first), created_at (oldest first)
//
// Returns empty list if no missions need gates.
func (s *VCStorage) GetMissionsNeedingGates(ctx context.Context) ([]*types.Issue, error) {
	query := `
		SELECT DISTINCT i.id, i.title, i.description, i.issue_type, i.status,
		       i.priority, i.created_at, i.updated_at
		FROM issues i
		JOIN vc_mission_state m ON i.id = m.issue_id
		WHERE i.issue_type = ?
		  AND m.subtype = ?
		  AND EXISTS (
		    SELECT 1 FROM labels
		    WHERE issue_id = i.id AND label = 'needs-quality-gates'
		  )
		  AND NOT EXISTS (
		    SELECT 1 FROM labels
		    WHERE issue_id = i.id AND label = 'gates-running'
		  )
		ORDER BY i.priority ASC, i.created_at ASC
		LIMIT 10
	`

	rows, err := s.db.QueryContext(ctx, query, types.TypeEpic, types.SubtypeMission)
	if err != nil {
		return nil, fmt.Errorf("failed to query missions needing gates: %w", err)
	}
	defer rows.Close()

	var missions []*types.Issue
	for rows.Next() {
		var issue types.Issue
		err := rows.Scan(
			&issue.ID,
			&issue.Title,
			&issue.Description,
			&issue.IssueType,
			&issue.Status,
			&issue.Priority,
			&issue.CreatedAt,
			&issue.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan mission row: %w", err)
		}

		// Set subtype from join (we know it's mission from WHERE clause)
		issue.IssueSubtype = types.SubtypeMission

		missions = append(missions, &issue)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating mission rows: %w", err)
	}

	return missions, nil
}

// VacuumDatabase runs VACUUM on the database
func (s *VCStorage) VacuumDatabase(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, "VACUUM")
	return err
}

// ======================================================================
// CODE REVIEW CHECKPOINTS (vc-1)
// ======================================================================

// GetLastReviewCheckpoint retrieves the most recent code review checkpoint
func (s *VCStorage) GetLastReviewCheckpoint(ctx context.Context) (*types.ReviewCheckpoint, error) {
	var checkpoint types.ReviewCheckpoint
	err := s.db.QueryRowContext(ctx, `
		SELECT commit_sha, timestamp, review_scope
		FROM vc_review_checkpoints
		ORDER BY timestamp DESC
		LIMIT 1
	`).Scan(&checkpoint.CommitSHA, &checkpoint.Timestamp, &checkpoint.ReviewScope)

	if err == sql.ErrNoRows {
		return nil, nil // No checkpoint yet
	}
	if err != nil {
		return nil, fmt.Errorf("failed to query last review checkpoint: %w", err)
	}

	return &checkpoint, nil
}

// SaveReviewCheckpoint creates a new review checkpoint record
func (s *VCStorage) SaveReviewCheckpoint(ctx context.Context, checkpoint *types.ReviewCheckpoint, reviewIssueID string) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO vc_review_checkpoints (commit_sha, timestamp, review_scope, review_issue_id)
		VALUES (?, ?, ?, ?)
	`, checkpoint.CommitSHA, checkpoint.Timestamp, checkpoint.ReviewScope, reviewIssueID)

	if err != nil {
		return fmt.Errorf("failed to save review checkpoint: %w", err)
	}

	return nil
}
