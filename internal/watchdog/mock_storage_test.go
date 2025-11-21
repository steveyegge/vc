package watchdog

import (
	"context"

	"github.com/steveyegge/vc/internal/events"
	"github.com/steveyegge/vc/internal/types"
)

// mockStorage provides a mock storage implementation for testing
type mockStorage struct{}

// Status change logging (vc-n4lx)
func (m *mockStorage) LogStatusChange(ctx context.Context, issueID string, newStatus types.Status, actor, reason string) {
}
func (m *mockStorage) LogStatusChangeFromUpdates(ctx context.Context, issueID string, updates map[string]interface{}, actor, reason string) {
}

func (m *mockStorage) Close() error { return nil }
func (m *mockStorage) StoreAgentEvent(ctx context.Context, event *events.AgentEvent) error {
	return nil
}
func (m *mockStorage) GetAgentEvents(ctx context.Context, filter events.EventFilter) ([]*events.AgentEvent, error) {
	return nil, nil
}
func (m *mockStorage) GetAgentEventsByIssue(ctx context.Context, issueID string) ([]*events.AgentEvent, error) {
	return nil, nil
}
func (m *mockStorage) GetRecentAgentEvents(ctx context.Context, limit int) ([]*events.AgentEvent, error) {
	return nil, nil
}
func (m *mockStorage) CreateIssue(ctx context.Context, issue *types.Issue, actor string) error {
	return nil
}
func (m *mockStorage) CreateMission(ctx context.Context, mission *types.Mission, actor string) error {
	return nil
}
func (m *mockStorage) GetIssue(ctx context.Context, id string) (*types.Issue, error) { return nil, nil }
func (m *mockStorage) GetIssues(ctx context.Context, ids []string) (map[string]*types.Issue, error) {
	return make(map[string]*types.Issue), nil
}
func (m *mockStorage) GetMission(ctx context.Context, id string) (*types.Mission, error) {
	return nil, nil
}

func (m *mockStorage) UpdateMission(ctx context.Context, id string, updates map[string]interface{}, actor string) error {
	return nil
}
func (m *mockStorage) UpdateIssue(ctx context.Context, id string, updates map[string]interface{}, actor string) error {
	return nil
}
func (m *mockStorage) CloseIssue(ctx context.Context, id string, reason string, actor string) error {
	return nil
}
func (m *mockStorage) SearchIssues(ctx context.Context, query string, filter types.IssueFilter) ([]*types.Issue, error) {
	return nil, nil
}
func (m *mockStorage) AddDependency(ctx context.Context, dep *types.Dependency, actor string) error {
	return nil
}
func (m *mockStorage) RemoveDependency(ctx context.Context, issueID, dependsOnID string, actor string) error {
	return nil
}
func (m *mockStorage) GetDependencies(ctx context.Context, issueID string) ([]*types.Issue, error) {
	return nil, nil
}
func (m *mockStorage) GetDependencyRecords(ctx context.Context, issueID string) ([]*types.Dependency, error) {
	return nil, nil
}
func (m *mockStorage) GetDependents(ctx context.Context, issueID string) ([]*types.Issue, error) {
	return nil, nil
}
func (m *mockStorage) GetDependencyTree(ctx context.Context, issueID string, maxDepth int) ([]*types.TreeNode, error) {
	return nil, nil
}
func (m *mockStorage) DetectCycles(ctx context.Context) ([][]*types.Issue, error)       { return nil, nil }
func (m *mockStorage) AddLabel(ctx context.Context, issueID, label, actor string) error { return nil }
func (m *mockStorage) RemoveLabel(ctx context.Context, issueID, label, actor string) error {
	return nil
}
func (m *mockStorage) GetLabels(ctx context.Context, issueID string) ([]string, error) {
	return nil, nil
}
func (m *mockStorage) GetIssuesByLabel(ctx context.Context, label string) ([]*types.Issue, error) {
	return nil, nil
}
func (m *mockStorage) GetReadyWork(ctx context.Context, filter types.WorkFilter) ([]*types.Issue, error) {
	return nil, nil
}
func (m *mockStorage) GetBlockedIssues(ctx context.Context) ([]*types.BlockedIssue, error) {
	return nil, nil
}
func (m *mockStorage) GetReadyBlockers(ctx context.Context, limit int) ([]*types.Issue, error) {
	return nil, nil
}
func (m *mockStorage) GetReadyBaselineIssues(ctx context.Context, limit int) ([]*types.Issue, error) {
	return nil, nil
}
func (m *mockStorage) GetReadyDependentsOfBlockedBaselines(ctx context.Context, limit int) ([]*types.Issue, map[string]string, error) {
	return nil, nil, nil
}
func (m *mockStorage) IsEpicComplete(ctx context.Context, epicID string) (bool, error) {
	return false, nil
}
func (m *mockStorage) GetMissionForTask(ctx context.Context, taskID string) (*types.MissionContext, error) {
	return nil, nil
}
func (m *mockStorage) GetMissionsNeedingGates(ctx context.Context) ([]*types.Issue, error) {
	return nil, nil
}
func (m *mockStorage) AddComment(ctx context.Context, issueID, actor, comment string) error {
	return nil
}
func (m *mockStorage) GetEvents(ctx context.Context, issueID string, limit int) ([]*types.Event, error) {
	return nil, nil
}
func (m *mockStorage) GetStatistics(ctx context.Context) (*types.Statistics, error) { return nil, nil }
func (m *mockStorage) RegisterInstance(ctx context.Context, instance *types.ExecutorInstance) error {
	return nil
}
func (m *mockStorage) MarkInstanceStopped(ctx context.Context, instanceID string) error { return nil }
func (m *mockStorage) UpdateHeartbeat(ctx context.Context, instanceID string) error     { return nil }
func (m *mockStorage) GetActiveInstances(ctx context.Context) ([]*types.ExecutorInstance, error) {
	return nil, nil
}
func (m *mockStorage) CleanupStaleInstances(ctx context.Context, staleThreshold int) (int, error) {
	return 0, nil
}
func (m *mockStorage) DeleteOldStoppedInstances(ctx context.Context, olderThanSeconds int, maxToKeep int) (int, error) {
	return 0, nil
}
func (m *mockStorage) ClaimIssue(ctx context.Context, issueID, executorInstanceID string) error {
	return nil
}
func (m *mockStorage) GetExecutionState(ctx context.Context, issueID string) (*types.IssueExecutionState, error) {
	return nil, nil
}
func (m *mockStorage) UpdateExecutionState(ctx context.Context, issueID string, state types.ExecutionState) error {
	return nil
}
func (m *mockStorage) SaveCheckpoint(ctx context.Context, issueID string, checkpointData interface{}) error {
	return nil
}
func (m *mockStorage) GetCheckpoint(ctx context.Context, issueID string) (string, error) {
	return "", nil
}
func (m *mockStorage) ReleaseIssue(ctx context.Context, issueID string) error { return nil }
func (m *mockStorage) ReleaseIssueAndReopen(ctx context.Context, issueID, actor, errorComment string) error {
	return nil
}
func (m *mockStorage) GetExecutionHistory(ctx context.Context, issueID string) ([]*types.ExecutionAttempt, error) {
	return nil, nil
}
func (m *mockStorage) GetExecutionHistoryPaginated(ctx context.Context, issueID string, limit, offset int) ([]*types.ExecutionAttempt, error) {
	return nil, nil
}
func (m *mockStorage) RecordExecutionAttempt(ctx context.Context, attempt *types.ExecutionAttempt) error {
	return nil
}
func (m *mockStorage) GetConfig(ctx context.Context, key string) (string, error) { return "", nil }
func (m *mockStorage) SetConfig(ctx context.Context, key, value string) error    { return nil }
func (m *mockStorage) GetIssuePrefix(ctx context.Context) (string, error)        { return "vc", nil } // vc-0bt1
func (m *mockStorage) CleanupEventsByAge(ctx context.Context, retentionDays, criticalRetentionDays, batchSize int) (int, error) {
	return 0, nil
}
func (m *mockStorage) CleanupEventsByIssueLimit(ctx context.Context, perIssueLimit, batchSize int) (int, error) {
	return 0, nil
}
func (m *mockStorage) CleanupEventsByGlobalLimit(ctx context.Context, globalLimit, batchSize int) (int, error) {
	return 0, nil
}
func (m *mockStorage) GetEventCounts(ctx context.Context) (*types.EventCounts, error) {
	return &types.EventCounts{}, nil
}
func (m *mockStorage) VacuumDatabase(ctx context.Context) error { return nil }
func (m *mockStorage) RecordWatchdogIntervention(ctx context.Context, issueID string) error {
	return nil
}

// Baseline Diagnostics methods (vc-9aa9)
func (m *mockStorage) StoreDiagnosis(ctx context.Context, issueID string, diagnosis *types.TestFailureDiagnosis) error {
	return nil
}
func (m *mockStorage) GetDiagnosis(ctx context.Context, issueID string) (*types.TestFailureDiagnosis, error) {
	return nil, nil
}

// Self-healing mode (vc-556f)
func (m *mockStorage) UpdateSelfHealingMode(ctx context.Context, instanceID string, mode string) error {
	return nil
}
