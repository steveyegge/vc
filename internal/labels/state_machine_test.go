package labels

import (
	"context"
	"testing"
	"time"

	"github.com/steveyegge/vc/internal/events"
	"github.com/steveyegge/vc/internal/storage"
	"github.com/steveyegge/vc/internal/types"
)

// mockStorage implements the Storage interface for testing
type mockStorage struct {
	labels       map[string][]string // issueID -> list of labels
	events       []*events.AgentEvent
	addLabelErr  error
	remLabelErr  error
	getLabelsErr error
	storeEvtErr  error
}

func newMockStorage() *mockStorage {
	return &mockStorage{
		labels: make(map[string][]string),
		events: make([]*events.AgentEvent, 0),
	}
}

func (m *mockStorage) AddLabel(ctx context.Context, issueID, label, actor string) error {
	if m.addLabelErr != nil {
		return m.addLabelErr
	}
	if m.labels[issueID] == nil {
		m.labels[issueID] = make([]string, 0)
	}
	m.labels[issueID] = append(m.labels[issueID], label)
	return nil
}

func (m *mockStorage) RemoveLabel(ctx context.Context, issueID, label, actor string) error {
	if m.remLabelErr != nil {
		return m.remLabelErr
	}
	labels := m.labels[issueID]
	newLabels := make([]string, 0)
	for _, l := range labels {
		if l != label {
			newLabels = append(newLabels, l)
		}
	}
	m.labels[issueID] = newLabels
	return nil
}

func (m *mockStorage) GetLabels(ctx context.Context, issueID string) ([]string, error) {
	if m.getLabelsErr != nil {
		return nil, m.getLabelsErr
	}
	labels := m.labels[issueID]
	if labels == nil {
		return []string{}, nil
	}
	return labels, nil
}

func (m *mockStorage) StoreAgentEvent(ctx context.Context, event *events.AgentEvent) error {
	if m.storeEvtErr != nil {
		return m.storeEvtErr
	}
	m.events = append(m.events, event)
	return nil
}

// Status change logging (vc-n4lx)
func (m *mockStorage) LogStatusChange(ctx context.Context, issueID string, newStatus types.Status, actor, reason string) {
	// No-op for tests
}
func (m *mockStorage) LogStatusChangeFromUpdates(ctx context.Context, issueID string, updates map[string]interface{}, actor, reason string) {
	// No-op for tests
}

// Baseline Diagnostics methods (vc-9aa9) - Not used in labels package tests
func (m *mockStorage) StoreDiagnosis(ctx context.Context, issueID string, diagnosis *types.TestFailureDiagnosis) error {
	return nil
}
func (m *mockStorage) GetDiagnosis(ctx context.Context, issueID string) (*types.TestFailureDiagnosis, error) {
	return nil, nil
}

// Mission Plans (vc-un1o, vc-gxfn, vc-d295)
func (m *mockStorage) StorePlan(ctx context.Context, plan *types.MissionPlan, expectedIteration int) (int, error) {
	return 1, nil
}
func (m *mockStorage) GetPlan(ctx context.Context, missionID string) (*types.MissionPlan, int, error) {
	return nil, 0, nil
}
func (m *mockStorage) GetPlanHistory(ctx context.Context, missionID string) ([]*types.MissionPlan, error) {
	return nil, nil
}
func (m *mockStorage) DeletePlan(ctx context.Context, missionID string) error {
	return nil
}
func (m *mockStorage) ListDraftPlans(ctx context.Context) ([]*types.MissionPlan, error) {
	return nil, nil
}

func (m *mockStorage) RunInVCTransaction(ctx context.Context, fn func(tx *storage.VCTransaction) error) error {
	return nil // Mock does not support transactions
}

func TestTransitionState(t *testing.T) {
	tests := []struct {
		name        string
		issueID     string
		fromLabel   string
		toLabel     string
		trigger     string
		actor       string
		initLabels  []string
		wantLabels  []string
		wantEvents  int
		wantErr     bool
	}{
		{
			name:       "initial state transition",
			issueID:    "vc-100",
			fromLabel:  "",
			toLabel:    LabelTaskReady,
			trigger:    TriggerTaskCompleted,
			actor:      "test-executor",
			initLabels: []string{},
			wantLabels: []string{LabelTaskReady},
			wantEvents: 1,
			wantErr:    false,
		},
		{
			name:       "task ready to needs-quality-gates",
			issueID:    "vc-101",
			fromLabel:  LabelTaskReady,
			toLabel:    LabelNeedsQualityGates,
			trigger:    TriggerEpicCompleted,
			actor:      "test-executor",
			initLabels: []string{LabelTaskReady},
			wantLabels: []string{LabelNeedsQualityGates},
			wantEvents: 1,
			wantErr:    false,
		},
		{
			name:       "gates to review",
			issueID:    "vc-102",
			fromLabel:  LabelNeedsQualityGates,
			toLabel:    LabelNeedsReview,
			trigger:    TriggerGatesPassed,
			actor:      "test-executor",
			initLabels: []string{LabelNeedsQualityGates},
			wantLabels: []string{LabelNeedsReview},
			wantEvents: 1,
			wantErr:    false,
		},
		{
			name:       "review to human approval",
			issueID:    "vc-103",
			fromLabel:  LabelNeedsReview,
			toLabel:    LabelNeedsHumanApproval,
			trigger:    TriggerReviewCompleted,
			actor:      "arbiter",
			initLabels: []string{LabelNeedsReview},
			wantLabels: []string{LabelNeedsHumanApproval},
			wantEvents: 1,
			wantErr:    false,
		},
		{
			name:       "human approval to approved",
			issueID:    "vc-104",
			fromLabel:  LabelNeedsHumanApproval,
			toLabel:    LabelApproved,
			trigger:    TriggerHumanApproval,
			actor:      "user@example.com",
			initLabels: []string{LabelNeedsHumanApproval},
			wantLabels: []string{LabelApproved},
			wantEvents: 1,
			wantErr:    false,
		},
		{
			name:       "preserves other labels",
			issueID:    "vc-105",
			fromLabel:  LabelTaskReady,
			toLabel:    LabelNeedsQualityGates,
			trigger:    TriggerEpicCompleted,
			actor:      "test-executor",
			initLabels: []string{LabelTaskReady, "bug", "p1"},
			wantLabels: []string{"bug", "p1", LabelNeedsQualityGates},
			wantEvents: 1,
			wantErr:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			store := newMockStorage()

			// Initialize labels
			for _, label := range tt.initLabels {
				_ = store.AddLabel(ctx, tt.issueID, label, "init")
			}

			// Perform transition
			err := TransitionState(ctx, store, tt.issueID, tt.fromLabel, tt.toLabel, tt.trigger, tt.actor)

			// Check error
			if (err != nil) != tt.wantErr {
				t.Errorf("TransitionState() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			// Check labels
			labels, _ := store.GetLabels(ctx, tt.issueID)
			if len(labels) != len(tt.wantLabels) {
				t.Errorf("got %d labels, want %d: %v", len(labels), len(tt.wantLabels), labels)
				return
			}

			// Check each expected label is present
			labelMap := make(map[string]bool)
			for _, l := range labels {
				labelMap[l] = true
			}
			for _, wantLabel := range tt.wantLabels {
				if !labelMap[wantLabel] {
					t.Errorf("missing expected label %q in %v", wantLabel, labels)
				}
			}

			// Check events
			if len(store.events) != tt.wantEvents {
				t.Errorf("got %d events, want %d", len(store.events), tt.wantEvents)
				return
			}

			// Verify event data if events were created
			if len(store.events) > 0 {
				event := store.events[0]
				if event.Type != events.EventTypeLabelStateTransition {
					t.Errorf("event type = %v, want %v", event.Type, events.EventTypeLabelStateTransition)
				}
				if event.IssueID != tt.issueID {
					t.Errorf("event issueID = %v, want %v", event.IssueID, tt.issueID)
				}
				if event.Severity != events.SeverityInfo {
					t.Errorf("event severity = %v, want %v", event.Severity, events.SeverityInfo)
				}

				// Check event data fields
				if fromLabel, ok := event.Data["from_label"].(string); !ok || fromLabel != tt.fromLabel {
					t.Errorf("event data from_label = %v, want %v", fromLabel, tt.fromLabel)
				}
				if toLabel, ok := event.Data["to_label"].(string); !ok || toLabel != tt.toLabel {
					t.Errorf("event data to_label = %v, want %v", toLabel, tt.toLabel)
				}
				if trigger, ok := event.Data["trigger"].(string); !ok || trigger != tt.trigger {
					t.Errorf("event data trigger = %v, want %v", trigger, tt.trigger)
				}
				if actor, ok := event.Data["actor"].(string); !ok || actor != tt.actor {
					t.Errorf("event data actor = %v, want %v", actor, tt.actor)
				}
			}
		})
	}
}

func TestHasLabel(t *testing.T) {
	tests := []struct {
		name    string
		issueID string
		labels  []string
		check   string
		want    bool
	}{
		{
			name:    "label exists",
			issueID: "vc-200",
			labels:  []string{LabelTaskReady, "bug"},
			check:   LabelTaskReady,
			want:    true,
		},
		{
			name:    "label does not exist",
			issueID: "vc-201",
			labels:  []string{LabelTaskReady, "bug"},
			check:   LabelNeedsQualityGates,
			want:    false,
		},
		{
			name:    "no labels",
			issueID: "vc-202",
			labels:  []string{},
			check:   LabelTaskReady,
			want:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			store := newMockStorage()

			// Initialize labels
			for _, label := range tt.labels {
				_ = store.AddLabel(ctx, tt.issueID, label, "init")
			}

			got, err := HasLabel(ctx, store, tt.issueID, tt.check)
			if err != nil {
				t.Errorf("HasLabel() error = %v", err)
				return
			}
			if got != tt.want {
				t.Errorf("HasLabel() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGetStateLabel(t *testing.T) {
	tests := []struct {
		name    string
		issueID string
		labels  []string
		want    string
	}{
		{
			name:    "task-ready state",
			issueID: "vc-300",
			labels:  []string{LabelTaskReady, "bug"},
			want:    LabelTaskReady,
		},
		{
			name:    "needs-quality-gates state",
			issueID: "vc-301",
			labels:  []string{LabelNeedsQualityGates, "p1"},
			want:    LabelNeedsQualityGates,
		},
		{
			name:    "needs-review state",
			issueID: "vc-302",
			labels:  []string{LabelNeedsReview},
			want:    LabelNeedsReview,
		},
		{
			name:    "needs-human-approval state",
			issueID: "vc-303",
			labels:  []string{LabelNeedsHumanApproval},
			want:    LabelNeedsHumanApproval,
		},
		{
			name:    "approved state",
			issueID: "vc-304",
			labels:  []string{LabelApproved},
			want:    LabelApproved,
		},
		{
			name:    "no state label",
			issueID: "vc-305",
			labels:  []string{"bug", "p1"},
			want:    "",
		},
		{
			name:    "multiple state labels (returns highest priority)",
			issueID: "vc-306",
			labels:  []string{LabelTaskReady, LabelApproved, "bug"},
			want:    LabelApproved, // Approved has highest priority
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			store := newMockStorage()

			// Initialize labels
			for _, label := range tt.labels {
				_ = store.AddLabel(ctx, tt.issueID, label, "init")
			}

			got, err := GetStateLabel(ctx, store, tt.issueID)
			if err != nil {
				t.Errorf("GetStateLabel() error = %v", err)
				return
			}
			if got != tt.want {
				t.Errorf("GetStateLabel() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestTransitionStateEventLogging(t *testing.T) {
	ctx := context.Background()
	store := newMockStorage()

	// Perform a state transition
	issueID := "vc-400"
	fromLabel := LabelTaskReady
	toLabel := LabelNeedsQualityGates
	trigger := TriggerEpicCompleted
	actor := "test-executor-123"

	_ = store.AddLabel(ctx, issueID, fromLabel, actor)
	err := TransitionState(ctx, store, issueID, fromLabel, toLabel, trigger, actor)
	if err != nil {
		t.Fatalf("TransitionState() error = %v", err)
	}

	// Verify event was logged
	if len(store.events) != 1 {
		t.Fatalf("got %d events, want 1", len(store.events))
	}

	event := store.events[0]

	// Check event type
	if event.Type != events.EventTypeLabelStateTransition {
		t.Errorf("event.Type = %v, want %v", event.Type, events.EventTypeLabelStateTransition)
	}

	// Check timestamp is recent
	if time.Since(event.Timestamp) > time.Second {
		t.Errorf("event timestamp is too old: %v", event.Timestamp)
	}

	// Check issue ID
	if event.IssueID != issueID {
		t.Errorf("event.IssueID = %v, want %v", event.IssueID, issueID)
	}

	// Check severity
	if event.Severity != events.SeverityInfo {
		t.Errorf("event.Severity = %v, want %v", event.Severity, events.SeverityInfo)
	}

	// Check message format
	expectedMsg := "State transition: task-ready → needs-quality-gates (trigger: epic_completed)"
	if event.Message != expectedMsg {
		t.Errorf("event.Message = %q, want %q", event.Message, expectedMsg)
	}

	// Check data fields
	if event.Data["from_label"] != fromLabel {
		t.Errorf("event.Data[from_label] = %v, want %v", event.Data["from_label"], fromLabel)
	}
	if event.Data["to_label"] != toLabel {
		t.Errorf("event.Data[to_label] = %v, want %v", event.Data["to_label"], toLabel)
	}
	if event.Data["trigger"] != trigger {
		t.Errorf("event.Data[trigger] = %v, want %v", event.Data["trigger"], trigger)
	}
	if event.Data["actor"] != actor {
		t.Errorf("event.Data[actor] = %v, want %v", event.Data["actor"], actor)
	}
}
