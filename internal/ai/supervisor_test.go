package ai

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/steveyegge/vc/internal/events"
	"github.com/steveyegge/vc/internal/storage/sqlite"
	"github.com/steveyegge/vc/internal/types"
)

// mockStorage implements a minimal storage.Storage for testing
type mockStorage struct {
	issues       map[string]*types.Issue
	comments     []string
	dependencies []types.Dependency
	createError  error // Inject errors for testing
	depError     error
	createFunc   func(ctx context.Context, issue *types.Issue, actor string) error // Allow overriding
}

func newMockStorage() *mockStorage {
	return &mockStorage{
		issues:       make(map[string]*types.Issue),
		comments:     []string{},
		dependencies: []types.Dependency{},
	}
}

func (m *mockStorage) CreateIssue(ctx context.Context, issue *types.Issue, actor string) error {
	// Allow overriding for testing
	if m.createFunc != nil {
		return m.createFunc(ctx, issue, actor)
	}
	if m.createError != nil {
		return m.createError
	}
	// Generate a simple ID
	issue.ID = "test-" + issue.Title[:min(5, len(issue.Title))]
	m.issues[issue.ID] = issue
	return nil
}

func (m *mockStorage) AddComment(ctx context.Context, issueID, actor, comment string) error {
	m.comments = append(m.comments, comment)
	return nil
}

func (m *mockStorage) AddDependency(ctx context.Context, dep *types.Dependency, actor string) error {
	if m.depError != nil {
		return m.depError
	}
	m.dependencies = append(m.dependencies, *dep)
	return nil
}

// Stub implementations for other required methods
func (m *mockStorage) GetIssue(ctx context.Context, id string) (*types.Issue, error) {
	return m.issues[id], nil
}
func (m *mockStorage) GetMission(ctx context.Context, id string) (*types.Mission, error) {
	issue, err := m.GetIssue(ctx, id)
	if err != nil {
		return nil, err
	}
	if issue == nil {
		return nil, nil
	}
	return &types.Mission{Issue: *issue}, nil
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
func (m *mockStorage) DetectCycles(ctx context.Context) ([][]*types.Issue, error) {
	return nil, nil
}
func (m *mockStorage) AddLabel(ctx context.Context, issueID, label, actor string) error {
	return nil
}
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
func (m *mockStorage) GetEvents(ctx context.Context, issueID string, limit int) ([]*types.Event, error) {
	return nil, nil
}
func (m *mockStorage) GetStatistics(ctx context.Context) (*types.Statistics, error) {
	return nil, nil
}
func (m *mockStorage) RegisterInstance(ctx context.Context, instance *types.ExecutorInstance) error {
	return nil
}
func (m *mockStorage) MarkInstanceStopped(ctx context.Context, instanceID string) error {
	return nil
}
func (m *mockStorage) UpdateHeartbeat(ctx context.Context, instanceID string) error {
	return nil
}
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
func (m *mockStorage) ReleaseIssue(ctx context.Context, issueID string) error {
	return nil
}
func (m *mockStorage) ReleaseIssueAndReopen(ctx context.Context, issueID, actor, errorComment string) error {
	return nil
}
func (m *mockStorage) Close() error {
	return nil
}

// Execution History methods
func (m *mockStorage) GetExecutionHistory(ctx context.Context, issueID string) ([]*types.ExecutionAttempt, error) {
	return nil, nil
}
func (m *mockStorage) RecordExecutionAttempt(ctx context.Context, attempt *types.ExecutionAttempt) error {
	return nil
}

// Agent Events methods
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
func (m *mockStorage) GetConfig(ctx context.Context, key string) (string, error) {
	return "", nil
}
func (m *mockStorage) SetConfig(ctx context.Context, key, value string) error {
	return nil
}

func (m *mockStorage) CleanupEventsByAge(ctx context.Context, retentionDays, criticalRetentionDays, batchSize int) (int, error) {
	return 0, nil
}

func (m *mockStorage) CleanupEventsByGlobalLimit(ctx context.Context, globalLimit int, batchSize int) (int, error) {
	return 0, nil
}

func (m *mockStorage) CleanupEventsByIssueLimit(ctx context.Context, perIssueLimit int, batchSize int) (int, error) {
	return 0, nil
}

func (m *mockStorage) GetEventCounts(ctx context.Context) (*sqlite.EventCounts, error) {
	return &sqlite.EventCounts{}, nil
}

func (m *mockStorage) VacuumDatabase(ctx context.Context) error {
	return nil
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// TestBuildAssessmentPrompt tests prompt construction
func TestBuildAssessmentPrompt(t *testing.T) {
	store := newMockStorage()
	supervisor := &Supervisor{
		store: store,
		model: "test-model",
	}

	issue := &types.Issue{
		ID:                 "test-1",
		Title:              "Add feature X",
		Description:        "Implement feature X with Y",
		Design:             "Use pattern Z",
		AcceptanceCriteria: "Feature works",
		IssueType:          types.TypeTask,
		Priority:           1,
	}

	prompt := supervisor.buildAssessmentPrompt(issue)

	// Verify prompt contains key elements
	if !strings.Contains(prompt, "test-1") {
		t.Error("Prompt should contain issue ID")
	}
	if !strings.Contains(prompt, "Add feature X") {
		t.Error("Prompt should contain title")
	}
	if !strings.Contains(prompt, "Implement feature X with Y") {
		t.Error("Prompt should contain description")
	}
	if !strings.Contains(prompt, "Use pattern Z") {
		t.Error("Prompt should contain design")
	}
	if !strings.Contains(prompt, "Feature works") {
		t.Error("Prompt should contain acceptance criteria")
	}
	if !strings.Contains(prompt, "strategy") {
		t.Error("Prompt should request strategy")
	}
	if !strings.Contains(prompt, "confidence") {
		t.Error("Prompt should request confidence")
	}
}

// TestBuildAnalysisPrompt tests analysis prompt construction
func TestBuildAnalysisPrompt(t *testing.T) {
	store := newMockStorage()
	supervisor := &Supervisor{
		store: store,
		model: "test-model",
	}

	issue := &types.Issue{
		ID:                 "test-2",
		Title:              "Fix bug Y",
		Description:        "Bug in module Y",
		AcceptanceCriteria: "Bug is fixed",
	}

	agentOutput := "Fixed the bug by changing line 42"

	tests := []struct {
		name    string
		success bool
		want    string
	}{
		{"successful execution", true, "succeeded"},
		{"failed execution", false, "failed"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			prompt := supervisor.buildAnalysisPrompt(issue, agentOutput, tt.success)

			if !strings.Contains(prompt, tt.want) {
				t.Errorf("Prompt should contain status '%s'", tt.want)
			}
			if !strings.Contains(prompt, "test-2") {
				t.Error("Prompt should contain issue ID")
			}
			if !strings.Contains(prompt, "Fixed the bug") {
				t.Error("Prompt should contain agent output")
			}
			if !strings.Contains(prompt, "completed") {
				t.Error("Prompt should ask about completion status")
			}
			if !strings.Contains(prompt, "discovered_issues") {
				t.Error("Prompt should ask about discovered issues")
			}
		})
	}
}

// TestCreateDiscoveredIssues tests issue creation from AI analysis
func TestCreateDiscoveredIssues(t *testing.T) {
	store := newMockStorage()
	supervisor := &Supervisor{
		store: store,
		model: "test-model",
	}

	parentIssue := &types.Issue{
		ID:    "parent-1",
		Title: "Parent task",
	}

	tests := []struct {
		name          string
		discovered    []DiscoveredIssue
		wantCount     int
		wantTypes     []types.IssueType
		wantPriorities []int
	}{
		{
			name: "single bug",
			discovered: []DiscoveredIssue{
				{
					Title:       "Found a bug",
					Description: "Bug description",
					Type:        "bug",
					Priority:    "P0",
				},
			},
			wantCount:      1,
			wantTypes:      []types.IssueType{types.TypeBug},
			wantPriorities: []int{0},
		},
		{
			name: "multiple issues with different types",
			discovered: []DiscoveredIssue{
				{
					Title:       "Add test",
					Description: "Missing test",
					Type:        "task",
					Priority:    "P1",
				},
				{
					Title:       "Refactor code",
					Description: "Code needs cleanup",
					Type:        "enhancement",
					Priority:    "P2",
				},
				{
					Title:       "Fix typo",
					Description: "Documentation typo",
					Type:        "chore",
					Priority:    "P3",
				},
			},
			wantCount:      3,
			wantTypes:      []types.IssueType{types.TypeTask, types.TypeFeature, types.TypeChore},
			wantPriorities: []int{1, 2, 3},
		},
		{
			name: "default values when type/priority unknown",
			discovered: []DiscoveredIssue{
				{
					Title:       "Unknown thing",
					Description: "Something",
					Type:        "unknown-type",
					Priority:    "P999",
				},
			},
			wantCount:      1,
			wantTypes:      []types.IssueType{types.TypeTask}, // default
			wantPriorities: []int{2},                          // default P2
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Reset store
			store.issues = make(map[string]*types.Issue)
			store.dependencies = []types.Dependency{}

			ctx := context.Background()
			createdIDs, err := supervisor.CreateDiscoveredIssues(ctx, parentIssue, tt.discovered)

			if err != nil {
				t.Fatalf("CreateDiscoveredIssues failed: %v", err)
			}

			if len(createdIDs) != tt.wantCount {
				t.Errorf("Created %d issues, want %d", len(createdIDs), tt.wantCount)
			}

			// Verify created issues have correct types and priorities
			for i, id := range createdIDs {
				issue := store.issues[id]
				if issue == nil {
					t.Fatalf("Issue %s not found in store", id)
				}

				if issue.IssueType != tt.wantTypes[i] {
					t.Errorf("Issue %d: got type %s, want %s", i, issue.IssueType, tt.wantTypes[i])
				}

				if issue.Priority != tt.wantPriorities[i] {
					t.Errorf("Issue %d: got priority %d, want %d", i, issue.Priority, tt.wantPriorities[i])
				}

				if !strings.Contains(issue.Description, "parent-1") {
					t.Errorf("Issue %d: description should mention parent issue", i)
				}

				if issue.Assignee != "ai-supervisor" {
					t.Errorf("Issue %d: got assignee %s, want ai-supervisor", i, issue.Assignee)
				}
			}

			// Verify dependencies were created
			if len(store.dependencies) != tt.wantCount {
				t.Errorf("Created %d dependencies, want %d", len(store.dependencies), tt.wantCount)
			}

			for _, dep := range store.dependencies {
				if dep.DependsOnID != parentIssue.ID {
					t.Errorf("Dependency should reference parent issue %s, got %s", parentIssue.ID, dep.DependsOnID)
				}
				if dep.Type != types.DepDiscoveredFrom {
					t.Errorf("Dependency type should be %s, got %s", types.DepDiscoveredFrom, dep.Type)
				}
			}
		})
	}
}

// TestCreateDiscoveredIssues_PartialFailure tests behavior when issue creation fails mid-way
func TestCreateDiscoveredIssues_PartialFailure(t *testing.T) {
	store := newMockStorage()
	supervisor := &Supervisor{
		store: store,
		model: "test-model",
	}

	parentIssue := &types.Issue{
		ID:    "parent-1",
		Title: "Parent task",
	}

	discovered := []DiscoveredIssue{
		{Title: "Issue 1", Description: "First", Type: "task", Priority: "P1"},
		{Title: "Issue 2", Description: "Second", Type: "bug", Priority: "P0"},
		{Title: "Issue 3", Description: "Third", Type: "task", Priority: "P2"},
	}

	// First two succeed, third fails
	callCount := 0
	store.createFunc = func(ctx context.Context, issue *types.Issue, actor string) error {
		callCount++
		if callCount > 2 {
			return fmt.Errorf("simulated creation failure")
		}
		// Default creation logic with unique ID
		issue.ID = fmt.Sprintf("test-%d", callCount)
		store.issues[issue.ID] = issue
		return nil
	}

	ctx := context.Background()
	createdIDs, err := supervisor.CreateDiscoveredIssues(ctx, parentIssue, discovered)

	// Should return error but include IDs of successfully created issues
	if err == nil {
		t.Error("Expected error when issue creation fails")
	}

	if len(createdIDs) != 2 {
		t.Errorf("Should have created 2 issues before failing, got %d", len(createdIDs))
	}

	// Verify the two successful issues exist
	if len(store.issues) != 2 {
		t.Errorf("Store should have 2 issues, got %d", len(store.issues))
	}
}

// TestTruncateString tests the truncation utility
func TestTruncateString(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		maxLen int
		want   string
	}{
		{
			name:   "string shorter than max",
			input:  "short",
			maxLen: 10,
			want:   "short",
		},
		{
			name:   "string exactly max length",
			input:  "exact",
			maxLen: 5,
			want:   "exact",
		},
		{
			name:   "string longer than max - takes last N chars",
			input:  "This is a long string",
			maxLen: 10,
			want:   "ong string",
		},
		{
			name:   "empty string",
			input:  "",
			maxLen: 10,
			want:   "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := truncateString(tt.input, tt.maxLen)
			if got != tt.want {
				t.Errorf("truncateString() = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestPriorityMapping tests all priority mappings
func TestPriorityMapping(t *testing.T) {
	tests := []struct {
		input string
		want  int
	}{
		{"P0", 0},
		{"P1", 1},
		{"P2", 2},
		{"P3", 3},
		{"P4", 2},       // unknown, defaults to P2
		{"invalid", 2},  // unknown, defaults to P2
		{"", 2},         // empty, defaults to P2
	}

	store := newMockStorage()
	supervisor := &Supervisor{
		store: store,
		model: "test-model",
	}

	parentIssue := &types.Issue{ID: "parent", Title: "Parent"}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			store.issues = make(map[string]*types.Issue)

			discovered := []DiscoveredIssue{
				{
					Title:       "Test",
					Description: "Test",
					Type:        "task",
					Priority:    tt.input,
				},
			}

			ctx := context.Background()
			createdIDs, err := supervisor.CreateDiscoveredIssues(ctx, parentIssue, discovered)

			if err != nil {
				t.Fatalf("CreateDiscoveredIssues failed: %v", err)
			}

			if len(createdIDs) != 1 {
				t.Fatal("Should create 1 issue")
			}

			issue := store.issues[createdIDs[0]]
			if issue.Priority != tt.want {
				t.Errorf("Priority %s mapped to %d, want %d", tt.input, issue.Priority, tt.want)
			}
		})
	}
}

// TestTypeMapping tests all type mappings
func TestTypeMapping(t *testing.T) {
	tests := []struct {
		input string
		want  types.IssueType
	}{
		{"bug", types.TypeBug},
		{"task", types.TypeTask},
		{"feature", types.TypeFeature},
		{"enhancement", types.TypeFeature}, // maps to feature
		{"epic", types.TypeEpic},
		{"chore", types.TypeChore},
		{"unknown", types.TypeTask},  // unknown, defaults to task
		{"", types.TypeTask},         // empty, defaults to task
	}

	store := newMockStorage()
	supervisor := &Supervisor{
		store: store,
		model: "test-model",
	}

	parentIssue := &types.Issue{ID: "parent", Title: "Parent"}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			store.issues = make(map[string]*types.Issue)

			discovered := []DiscoveredIssue{
				{
					Title:       "Test",
					Description: "Test",
					Type:        tt.input,
					Priority:    "P1",
				},
			}

			ctx := context.Background()
			createdIDs, err := supervisor.CreateDiscoveredIssues(ctx, parentIssue, discovered)

			if err != nil {
				t.Fatalf("CreateDiscoveredIssues failed: %v", err)
			}

			if len(createdIDs) != 1 {
				t.Fatal("Should create 1 issue")
			}

			issue := store.issues[createdIDs[0]]
			if issue.IssueType != tt.want {
				t.Errorf("Type %s mapped to %s, want %s", tt.input, issue.IssueType, tt.want)
			}
		})
	}
}

// ============================================================================
// Circuit Breaker Tests
// ============================================================================

// TestCircuitBreakerInitialization tests circuit breaker creation and defaults
func TestCircuitBreakerInitialization(t *testing.T) {
	t.Run("default config enables circuit breaker", func(t *testing.T) {
		cfg := DefaultRetryConfig()

		if !cfg.CircuitBreakerEnabled {
			t.Error("Circuit breaker should be enabled by default")
		}
		if cfg.FailureThreshold != 5 {
			t.Errorf("Default failure threshold should be 5, got %d", cfg.FailureThreshold)
		}
		if cfg.SuccessThreshold != 2 {
			t.Errorf("Default success threshold should be 2, got %d", cfg.SuccessThreshold)
		}
		if cfg.OpenTimeout != 30*time.Second {
			t.Errorf("Default open timeout should be 30s, got %v", cfg.OpenTimeout)
		}
	})

	t.Run("new circuit breaker starts in closed state", func(t *testing.T) {
		cb := NewCircuitBreaker(5, 2, 30*time.Second)

		if cb.GetState() != CircuitClosed {
			t.Errorf("New circuit breaker should start in CLOSED state, got %s", cb.GetState())
		}

		state, failures, successes := cb.GetMetrics()
		if state != CircuitClosed {
			t.Errorf("Expected CLOSED state, got %s", state)
		}
		if failures != 0 {
			t.Errorf("Expected 0 failures, got %d", failures)
		}
		if successes != 0 {
			t.Errorf("Expected 0 successes, got %d", successes)
		}
	})
}

// TestCircuitBreakerClosedState tests normal operation when circuit is closed
func TestCircuitBreakerClosedState(t *testing.T) {
	t.Run("allows requests in closed state", func(t *testing.T) {
		cb := NewCircuitBreaker(5, 2, 30*time.Second)

		// Should allow multiple requests
		for i := 0; i < 10; i++ {
			if err := cb.Allow(); err != nil {
				t.Errorf("Request %d should be allowed in CLOSED state, got error: %v", i, err)
			}
		}
	})

	t.Run("resets failure count on success", func(t *testing.T) {
		cb := NewCircuitBreaker(5, 2, 30*time.Second)

		// Record some failures
		cb.RecordFailure()
		cb.RecordFailure()
		_, failures, _ := cb.GetMetrics()
		if failures != 2 {
			t.Errorf("Expected 2 failures, got %d", failures)
		}

		// Record success should reset failure count
		cb.RecordSuccess()
		_, failures, _ = cb.GetMetrics()
		if failures != 0 {
			t.Errorf("Failure count should be reset to 0 after success, got %d", failures)
		}
	})

	t.Run("transitions to open after threshold failures", func(t *testing.T) {
		cb := NewCircuitBreaker(3, 2, 30*time.Second)

		// Record failures up to threshold
		for i := 0; i < 3; i++ {
			cb.RecordFailure()
		}

		// Should now be open
		if cb.GetState() != CircuitOpen {
			t.Errorf("Circuit should be OPEN after %d failures, got %s", 3, cb.GetState())
		}
	})
}

// TestCircuitBreakerOpenState tests fail-fast behavior when circuit is open
func TestCircuitBreakerOpenState(t *testing.T) {
	t.Run("blocks requests when open", func(t *testing.T) {
		cb := NewCircuitBreaker(3, 2, 30*time.Second)

		// Trip the circuit
		for i := 0; i < 3; i++ {
			cb.RecordFailure()
		}

		// Should block requests
		err := cb.Allow()
		if err == nil {
			t.Error("Allow() should return error when circuit is OPEN")
		}
		if !errors.Is(err, ErrCircuitOpen) {
			t.Errorf("Expected ErrCircuitOpen, got %v", err)
		}
	})

	t.Run("transitions to half-open after timeout", func(t *testing.T) {
		cb := NewCircuitBreaker(3, 2, 100*time.Millisecond) // Short timeout for testing

		// Trip the circuit
		for i := 0; i < 3; i++ {
			cb.RecordFailure()
		}

		if cb.GetState() != CircuitOpen {
			t.Fatal("Circuit should be OPEN")
		}

		// Should still be blocked immediately
		if err := cb.Allow(); !errors.Is(err, ErrCircuitOpen) {
			t.Error("Should be blocked immediately after opening")
		}

		// Wait for timeout
		time.Sleep(150 * time.Millisecond)

		// Should transition to half-open and allow request
		if err := cb.Allow(); err != nil {
			t.Errorf("Should allow request after timeout, got error: %v", err)
		}

		if cb.GetState() != CircuitHalfOpen {
			t.Errorf("Circuit should be HALF_OPEN after timeout, got %s", cb.GetState())
		}
	})
}

// TestCircuitBreakerHalfOpenState tests recovery probing in half-open state
func TestCircuitBreakerHalfOpenState(t *testing.T) {
	t.Run("allows probe requests in half-open", func(t *testing.T) {
		cb := NewCircuitBreaker(3, 2, 50*time.Millisecond)

		// Trip and wait for half-open
		for i := 0; i < 3; i++ {
			cb.RecordFailure()
		}
		time.Sleep(60 * time.Millisecond)

		// Transition to half-open
		_ = cb.Allow() // Intentionally ignore error to transition state

		// Should allow requests in half-open
		if err := cb.Allow(); err != nil {
			t.Errorf("Should allow probe requests in HALF_OPEN, got error: %v", err)
		}
	})

	t.Run("transitions to closed after success threshold", func(t *testing.T) {
		cb := NewCircuitBreaker(3, 2, 50*time.Millisecond)

		// Trip and transition to half-open
		for i := 0; i < 3; i++ {
			cb.RecordFailure()
		}
		time.Sleep(60 * time.Millisecond)
		_ = cb.Allow() // Intentionally ignore error to transition state

		if cb.GetState() != CircuitHalfOpen {
			t.Fatal("Should be in HALF_OPEN state")
		}

		// Record successful probes
		cb.RecordSuccess()
		if cb.GetState() != CircuitHalfOpen {
			t.Error("Should still be HALF_OPEN after 1 success")
		}

		cb.RecordSuccess()
		if cb.GetState() != CircuitClosed {
			t.Errorf("Should transition to CLOSED after 2 successes, got %s", cb.GetState())
		}

		// Verify failure count is reset
		_, failures, _ := cb.GetMetrics()
		if failures != 0 {
			t.Errorf("Failure count should be reset to 0, got %d", failures)
		}
	})

	t.Run("reopens immediately on failure in half-open", func(t *testing.T) {
		cb := NewCircuitBreaker(3, 2, 50*time.Millisecond)

		// Trip and transition to half-open
		for i := 0; i < 3; i++ {
			cb.RecordFailure()
		}
		time.Sleep(60 * time.Millisecond)
		_ = cb.Allow() // Intentionally ignore error to transition state

		if cb.GetState() != CircuitHalfOpen {
			t.Fatal("Should be in HALF_OPEN state")
		}

		// One success, then failure
		cb.RecordSuccess()
		cb.RecordFailure()

		// Should immediately reopen
		if cb.GetState() != CircuitOpen {
			t.Errorf("Should immediately transition to OPEN on failure in HALF_OPEN, got %s", cb.GetState())
		}
	})
}

// TestCircuitBreakerThreadSafety tests concurrent access to circuit breaker
func TestCircuitBreakerThreadSafety(t *testing.T) {
	cb := NewCircuitBreaker(10, 2, 100*time.Millisecond)

	// Run many concurrent operations
	var wg sync.WaitGroup
	numGoroutines := 50
	operationsPerGoroutine := 100

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < operationsPerGoroutine; j++ {
				// Mix of operations
				_ = cb.Allow() // Intentionally ignore error in concurrency test
				if j%3 == 0 {
					cb.RecordSuccess()
				} else if j%7 == 0 {
					cb.RecordFailure()
				}
				cb.GetState()
				cb.GetMetrics()
			}
		}(i)
	}

	// Should complete without deadlock or panic
	wg.Wait()

	// Circuit breaker should still be functional
	state := cb.GetState()
	if state != CircuitClosed && state != CircuitOpen && state != CircuitHalfOpen {
		t.Errorf("Circuit breaker in invalid state: %v", state)
	}
}

// TestCircuitBreakerWithRetry tests integration with retryWithBackoff
func TestCircuitBreakerWithRetry(t *testing.T) {
	t.Run("circuit breaker blocks retries when open", func(t *testing.T) {
		store := newMockStorage()
		cfg := &Config{
			Store: store,
			Retry: RetryConfig{
				MaxRetries:            3,
				InitialBackoff:        10 * time.Millisecond,
				MaxBackoff:            100 * time.Millisecond,
				BackoffMultiplier:     2.0,
				Timeout:               100 * time.Millisecond,
				CircuitBreakerEnabled: true,
				FailureThreshold:      2,
				SuccessThreshold:      1,
				OpenTimeout:           200 * time.Millisecond,
			},
		}

		supervisor, err := NewSupervisor(cfg)
		if err != nil {
			t.Fatalf("Failed to create supervisor: %v", err)
		}

		// Trip the circuit by causing failures
		callCount := 0
		ctx := context.Background()
		retriableError := errors.New("503 service unavailable")

		// First attempt - causes 2 failures and opens circuit
		err = supervisor.retryWithBackoff(ctx, "test", func(ctx context.Context) error {
			callCount++
			return retriableError
		})
		if err == nil {
			t.Error("Expected error from retryWithBackoff")
		}

		// Circuit should be open now
		if supervisor.circuitBreaker.GetState() != CircuitOpen {
			t.Errorf("Circuit should be OPEN, got %s", supervisor.circuitBreaker.GetState())
		}

		// Second attempt should fail immediately without retry
		callCountBefore := callCount
		err = supervisor.retryWithBackoff(ctx, "test", func(ctx context.Context) error {
			callCount++
			return retriableError
		})

		// Should fail fast without calling the function
		if callCount != callCountBefore {
			t.Error("Circuit breaker should block request without calling function")
		}

		// Error should mention circuit breaker
		if !strings.Contains(err.Error(), "circuit breaker") {
			t.Errorf("Error should mention circuit breaker, got: %v", err)
		}
	})

	t.Run("successful request records success with circuit breaker", func(t *testing.T) {
		store := newMockStorage()
		cfg := &Config{
			Store: store,
			Retry: RetryConfig{
				MaxRetries:            3,
				InitialBackoff:        10 * time.Millisecond,
				MaxBackoff:            100 * time.Millisecond,
				BackoffMultiplier:     2.0,
				Timeout:               100 * time.Millisecond,
				CircuitBreakerEnabled: true,
				FailureThreshold:      5,
				SuccessThreshold:      2,
				OpenTimeout:           30 * time.Second,
			},
		}

		supervisor, err := NewSupervisor(cfg)
		if err != nil {
			t.Fatalf("Failed to create supervisor: %v", err)
		}

		ctx := context.Background()

		// Record some failures
		supervisor.circuitBreaker.RecordFailure()
		supervisor.circuitBreaker.RecordFailure()
		_, failures, _ := supervisor.circuitBreaker.GetMetrics()
		if failures != 2 {
			t.Errorf("Expected 2 failures, got %d", failures)
		}

		// Successful request should reset failure count
		err = supervisor.retryWithBackoff(ctx, "test", func(ctx context.Context) error {
			return nil // Success
		})
		if err != nil {
			t.Errorf("Unexpected error: %v", err)
		}

		_, failures, _ = supervisor.circuitBreaker.GetMetrics()
		if failures != 0 {
			t.Errorf("Failure count should be reset after success, got %d", failures)
		}
	})

	t.Run("non-retriable errors don't affect circuit breaker", func(t *testing.T) {
		store := newMockStorage()
		cfg := &Config{
			Store: store,
			Retry: RetryConfig{
				MaxRetries:            3,
				InitialBackoff:        10 * time.Millisecond,
				MaxBackoff:            100 * time.Millisecond,
				BackoffMultiplier:     2.0,
				Timeout:               100 * time.Millisecond,
				CircuitBreakerEnabled: true,
				FailureThreshold:      2,
				SuccessThreshold:      1,
				OpenTimeout:           30 * time.Second,
			},
		}

		supervisor, err := NewSupervisor(cfg)
		if err != nil {
			t.Fatalf("Failed to create supervisor: %v", err)
		}

		ctx := context.Background()
		nonRetriableError := errors.New("401 unauthorized")

		// Non-retriable error shouldn't affect circuit breaker
		err = supervisor.retryWithBackoff(ctx, "test", func(ctx context.Context) error {
			return nonRetriableError
		})
		if err == nil {
			t.Error("Expected error from retryWithBackoff")
		}

		// Circuit should still be closed
		if supervisor.circuitBreaker.GetState() != CircuitClosed {
			t.Errorf("Circuit should remain CLOSED for non-retriable errors, got %s", supervisor.circuitBreaker.GetState())
		}

		_, failures, _ := supervisor.circuitBreaker.GetMetrics()
		if failures != 0 {
			t.Errorf("Non-retriable errors shouldn't count as failures, got %d", failures)
		}
	})
}

// TestCircuitBreakerStateTransitions tests all state transition logging
func TestCircuitBreakerStateTransitions(t *testing.T) {
	t.Run("logs all state transitions", func(t *testing.T) {
		cb := NewCircuitBreaker(2, 1, 50*time.Millisecond)

		// CLOSED -> OPEN
		cb.RecordFailure()
		cb.RecordFailure() // Should log transition

		// Wait for timeout and transition to HALF_OPEN
		time.Sleep(60 * time.Millisecond)
		_ = cb.Allow() // Should log transition, intentionally ignore error

		// HALF_OPEN -> CLOSED
		cb.RecordSuccess() // Should log transition

		// CLOSED -> OPEN again
		cb.RecordFailure()
		cb.RecordFailure() // Should log transition

		// Wait and OPEN -> HALF_OPEN
		time.Sleep(60 * time.Millisecond)
		_ = cb.Allow() // Intentionally ignore error to transition state

		// HALF_OPEN -> OPEN (failure in half-open)
		cb.RecordFailure() // Should log transition

		// Verify we end in OPEN state
		if cb.GetState() != CircuitOpen {
			t.Errorf("Expected final state to be OPEN, got %s", cb.GetState())
		}
	})
}

// TestCircuitBreakerDisabled tests behavior when circuit breaker is disabled
func TestCircuitBreakerDisabled(t *testing.T) {
	store := newMockStorage()
	cfg := &Config{
		Store: store,
		Retry: RetryConfig{
			MaxRetries:            3,
			InitialBackoff:        10 * time.Millisecond,
			MaxBackoff:            100 * time.Millisecond,
			BackoffMultiplier:     2.0,
			Timeout:               100 * time.Millisecond,
			CircuitBreakerEnabled: false, // Disabled
		},
	}

	supervisor, err := NewSupervisor(cfg)
	if err != nil {
		t.Fatalf("Failed to create supervisor: %v", err)
	}

	// Circuit breaker should be nil
	if supervisor.circuitBreaker != nil {
		t.Error("Circuit breaker should be nil when disabled")
	}

	// Retry logic should still work
	ctx := context.Background()
	callCount := 0
	err = supervisor.retryWithBackoff(ctx, "test", func(ctx context.Context) error {
		callCount++
		if callCount < 3 {
			return errors.New("503 service unavailable")
		}
		return nil
	})

	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}

	if callCount != 3 {
		t.Errorf("Expected 3 calls (2 retries + 1 success), got %d", callCount)
	}
}
