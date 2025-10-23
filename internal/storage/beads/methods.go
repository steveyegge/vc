package beads

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	beadsTypes "github.com/steveyegge/beads/internal/types"
	"github.com/steveyegge/vc/internal/storage/sqlite"
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

	err = s.db.QueryRowContext(ctx, `
		SELECT sandbox_path, branch_name, iteration_count, gates_status
		FROM vc_mission_state
		WHERE issue_id = ? AND subtype IN ('mission', 'phase')
	`, id).Scan(
		&mission.SandboxPath,
		&mission.BranchName,
		&mission.IterationCount,
		&mission.GatesStatus,
	)

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("issue %s is not a mission", id)
		}
		return nil, fmt.Errorf("failed to query mission metadata: %w", err)
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
	beadsFilter := beadsTypes.IssueFilter{
		Status:   beadsTypes.Status(filter.Status),
		Type:     beadsTypes.IssueType(filter.Type),
		Priority: filter.Priority,
		Assignee: filter.Assignee,
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
	beadsDep := &beadsTypes.Dependency{
		IssueID:     dep.IssueID,
		DependsOnID: dep.DependsOnID,
		Type:        beadsTypes.DependencyType(dep.Type),
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
			Issue:    *beadsIssueToVC(bn.Issue),
			Children: nil, // TODO: convert children recursively if needed
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

// AddLabel, RemoveLabel, GetLabels, GetIssuesByLabel all delegate to Beads
// (These methods are already available via embedded beads.Storage)

// ======================================================================
// READY WORK & BLOCKING (delegate to Beads)
// ======================================================================

// GetReadyWork retrieves ready work from Beads
func (s *VCStorage) GetReadyWork(ctx context.Context, filter types.WorkFilter) ([]*types.Issue, error) {
	beadsFilter := beadsTypes.WorkFilter{
		Status:   beadsTypes.Status(filter.Status),
		Type:     beadsTypes.IssueType(filter.Type),
		Priority: filter.Priority,
		Limit:    filter.Limit,
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
			Issue:    *beadsIssueToVC(bb.Issue),
			Blockers: nil, // TODO: convert blockers if needed
		}
	}
	return vcBlocked, nil
}

// ======================================================================
// EVENTS & COMMENTS (delegate to Beads)
// ======================================================================

// AddComment, GetEvents delegate to Beads
// (Already available via embedded beads.Storage)

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
	}, nil
}

// ======================================================================
// EVENT CLEANUP (VC extension methods)
// ======================================================================

// CleanupEventsByAge cleans up old events from vc_agent_events table
func (s *VCStorage) CleanupEventsByAge(ctx context.Context, retentionDays, criticalRetentionDays, batchSize int) (int, error) {
	// TODO: Implement event cleanup logic
	// For now, return 0 (no events deleted)
	return 0, nil
}

// CleanupEventsByIssueLimit limits events per issue
func (s *VCStorage) CleanupEventsByIssueLimit(ctx context.Context, perIssueLimit, batchSize int) (int, error) {
	// TODO: Implement per-issue limit cleanup
	return 0, nil
}

// CleanupEventsByGlobalLimit enforces global event limit
func (s *VCStorage) CleanupEventsByGlobalLimit(ctx context.Context, globalLimit, batchSize int) (int, error) {
	// TODO: Implement global limit cleanup
	return 0, nil
}

// GetEventCounts returns event statistics
func (s *VCStorage) GetEventCounts(ctx context.Context) (*sqlite.EventCounts, error) {
	// TODO: Implement event counts query
	return &sqlite.EventCounts{}, nil
}

// VacuumDatabase runs VACUUM on the database
func (s *VCStorage) VacuumDatabase(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, "VACUUM")
	return err
}
