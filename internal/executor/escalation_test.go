package executor

import (
	"context"
	"testing"
	"time"

	"github.com/steveyegge/vc/internal/storage"
	"github.com/steveyegge/vc/internal/types"
)

func TestEscalationTracker_AttemptTracking(t *testing.T) {
	ctx := context.Background()

	// Create in-memory storage
	storageCfg := storage.DefaultConfig()
	storageCfg.Path = ":memory:"
	store, err := storage.NewStorage(ctx, storageCfg)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer store.Close()

	// Create executor with short escalation thresholds for testing
	cfg := DefaultConfig()
	cfg.Store = store
	cfg.MaxEscalationAttempts = 3
	cfg.MaxEscalationDuration = 5 * time.Minute

	exec, err := New(cfg)
	if err != nil {
		t.Fatalf("Failed to create executor: %v", err)
	}

	issueID := BaselineTestIssueID

	// Test 1: Initial tracker creation
	tracker := exec.getOrCreateTracker(issueID)
	if tracker.IssueID != issueID {
		t.Errorf("tracker.IssueID = %s, want %s", tracker.IssueID, issueID)
	}
	if tracker.AttemptCount != 0 {
		t.Errorf("tracker.AttemptCount = %d, want 0", tracker.AttemptCount)
	}

	// Test 2: Increment attempts
	exec.incrementAttempt(issueID)
	tracker = exec.getOrCreateTracker(issueID)
	if tracker.AttemptCount != 1 {
		t.Errorf("After increment: AttemptCount = %d, want 1", tracker.AttemptCount)
	}

	exec.incrementAttempt(issueID)
	exec.incrementAttempt(issueID)
	tracker = exec.getOrCreateTracker(issueID)
	if tracker.AttemptCount != 3 {
		t.Errorf("After 3 increments: AttemptCount = %d, want 3", tracker.AttemptCount)
	}

	// Test 3: Check escalation threshold (should trigger on attempts)
	escalatedIssueID, reason := exec.checkEscalationThresholds(ctx)
	if escalatedIssueID != issueID {
		t.Errorf("checkEscalationThresholds issueID = %s, want %s", escalatedIssueID, issueID)
	}
	if reason == "" {
		t.Error("Expected non-empty escalation reason")
	}
	t.Logf("Escalation reason: %s", reason)

	// Test 4: Clear tracker
	exec.clearTracker(issueID)
	tracker = exec.getOrCreateTracker(issueID)
	if tracker.AttemptCount != 0 {
		t.Errorf("After clear: AttemptCount = %d, want 0", tracker.AttemptCount)
	}
}

func TestEscalationTracker_DurationThreshold(t *testing.T) {
	ctx := context.Background()

	// Create in-memory storage
	storageCfg := storage.DefaultConfig()
	storageCfg.Path = ":memory:"
	store, err := storage.NewStorage(ctx, storageCfg)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer store.Close()

	// Create executor with short duration threshold for testing
	cfg := DefaultConfig()
	cfg.Store = store
	cfg.MaxEscalationAttempts = 100 // High threshold so attempts don't trigger
	cfg.MaxEscalationDuration = 100 * time.Millisecond

	exec, err := New(cfg)
	if err != nil {
		t.Fatalf("Failed to create executor: %v", err)
	}

	issueID := BaselineLintIssueID

	// Create tracker and backdate FirstSeen to simulate duration
	exec.escalationMutex.Lock()
	exec.escalationTrackers[issueID] = &escalationTracker{
		IssueID:       issueID,
		AttemptCount:  1,
		FirstSeen:     time.Now().Add(-200 * time.Millisecond), // 200ms ago
		LastAttempted: time.Now(),
	}
	exec.escalationMutex.Unlock()

	// Check escalation threshold (should trigger on duration)
	escalatedIssueID, reason := exec.checkEscalationThresholds(ctx)
	if escalatedIssueID != issueID {
		t.Errorf("checkEscalationThresholds issueID = %s, want %s", escalatedIssueID, issueID)
	}
	if reason == "" {
		t.Error("Expected non-empty escalation reason")
	}
	t.Logf("Escalation reason: %s", reason)
}

func TestEscalationTracker_ClearAllTrackers(t *testing.T) {
	ctx := context.Background()

	// Create in-memory storage
	storageCfg := storage.DefaultConfig()
	storageCfg.Path = ":memory:"
	store, err := storage.NewStorage(ctx, storageCfg)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer store.Close()

	// Create executor
	cfg := DefaultConfig()
	cfg.Store = store
	exec, err := New(cfg)
	if err != nil {
		t.Fatalf("Failed to create executor: %v", err)
	}

	// Create multiple trackers
	exec.incrementAttempt(BaselineTestIssueID)
	exec.incrementAttempt(BaselineLintIssueID)
	exec.incrementAttempt(BaselineBuildIssueID)

	// Verify trackers exist
	exec.escalationMutex.RLock()
	count := len(exec.escalationTrackers)
	exec.escalationMutex.RUnlock()

	if count != 3 {
		t.Errorf("Before clear: tracker count = %d, want 3", count)
	}

	// Clear all trackers
	exec.clearAllTrackers()

	// Verify all trackers cleared
	exec.escalationMutex.RLock()
	count = len(exec.escalationTrackers)
	exec.escalationMutex.RUnlock()

	if count != 0 {
		t.Errorf("After clearAllTrackers: tracker count = %d, want 0", count)
	}
}

func TestEscalateBaseline_CreatesEscalationIssue(t *testing.T) {
	ctx := context.Background()

	// Create in-memory storage
	storageCfg := storage.DefaultConfig()
	storageCfg.Path = ":memory:"
	store, err := storage.NewStorage(ctx, storageCfg)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer store.Close()

	// Create executor
	cfg := DefaultConfig()
	cfg.Store = store
	cfg.MaxEscalationAttempts = 5
	cfg.MaxEscalationDuration = 24 * time.Hour

	exec, err := New(cfg)
	if err != nil {
		t.Fatalf("Failed to create executor: %v", err)
	}

	// Create a baseline issue
	baselineIssue := &types.Issue{
		ID:          BaselineTestIssueID,
		Title:       "Baseline test gate failing",
		IssueType:   types.TypeTask,
		Priority:    0,
		Status:      types.StatusOpen,
		Description: "Test gate is failing on main branch",
	}
	if err = store.CreateIssue(ctx, baselineIssue, "test"); err != nil {
		t.Fatalf("Failed to create baseline issue: %v", err)
	}

	// Add baseline-failure label
	if err := store.AddLabel(ctx, BaselineTestIssueID, "baseline-failure", "test"); err != nil {
		t.Fatalf("Failed to add baseline-failure label: %v", err)
	}

	// Simulate multiple attempts
	exec.incrementAttempt(BaselineTestIssueID)
	exec.incrementAttempt(BaselineTestIssueID)
	exec.incrementAttempt(BaselineTestIssueID)
	exec.incrementAttempt(BaselineTestIssueID)
	exec.incrementAttempt(BaselineTestIssueID)

	// Escalate the baseline issue
	reason := "Test escalation"
	if err := exec.escalateBaseline(ctx, BaselineTestIssueID, reason); err != nil {
		t.Fatalf("escalateBaseline failed: %v", err)
	}

	// Verify no-auto-claim label was added to baseline issue
	baselineLabels, err := store.GetLabels(ctx, BaselineTestIssueID)
	if err != nil {
		t.Fatalf("Failed to get baseline labels: %v", err)
	}
	hasNoAutoClaim := false
	for _, label := range baselineLabels {
		if label == "no-auto-claim" {
			hasNoAutoClaim = true
			break
		}
	}
	if !hasNoAutoClaim {
		t.Error("Baseline issue should have no-auto-claim label after escalation")
	}

	// Find the escalation issue (should have escalation label)
	filter := types.WorkFilter{
		Status: types.StatusOpen,
		Limit:  100,
	}
	issues, err := store.GetReadyWork(ctx, filter)
	if err != nil {
		t.Fatalf("Failed to get issues: %v", err)
	}

	var escalationIssue *types.Issue
	for _, issue := range issues {
		labels, err := store.GetLabels(ctx, issue.ID)
		if err != nil {
			continue
		}
		for _, label := range labels {
			if label == "escalation" {
				escalationIssue = issue
				break
			}
		}
		if escalationIssue != nil {
			break
		}
	}

	if escalationIssue == nil {
		t.Fatal("Expected escalation issue to be created")
	}

	t.Logf("Escalation issue created: %s - %s", escalationIssue.ID, escalationIssue.Title)

	// Verify escalation issue properties
	if escalationIssue.Priority != 0 {
		t.Errorf("Escalation issue priority = %d, want 0 (P0)", escalationIssue.Priority)
	}
	if escalationIssue.Status != types.StatusOpen {
		t.Errorf("Escalation issue status = %s, want %s", escalationIssue.Status, types.StatusOpen)
	}

	// Verify escalation issue has no-auto-claim label
	escalationLabels, err := store.GetLabels(ctx, escalationIssue.ID)
	if err != nil {
		t.Fatalf("Failed to get escalation labels: %v", err)
	}
	hasNoAutoClaim = false
	for _, label := range escalationLabels {
		if label == "no-auto-claim" {
			hasNoAutoClaim = true
			break
		}
	}
	if !hasNoAutoClaim {
		t.Error("Escalation issue should have no-auto-claim label")
	}

	// Verify discovered-from dependency
	deps, err := store.GetDependencyRecords(ctx, escalationIssue.ID)
	if err != nil {
		t.Fatalf("Failed to get dependencies: %v", err)
	}
	hasDiscoveredFrom := false
	for _, dep := range deps {
		if dep.Type == "discovered-from" && dep.DependsOnID == BaselineTestIssueID {
			hasDiscoveredFrom = true
			break
		}
	}
	if !hasDiscoveredFrom {
		t.Error("Escalation issue should have discovered-from dependency to baseline issue")
	}
}

func TestShouldEscalate_Integration(t *testing.T) {
	ctx := context.Background()

	// Create in-memory storage
	storageCfg := storage.DefaultConfig()
	storageCfg.Path = ":memory:"
	store, err := storage.NewStorage(ctx, storageCfg)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer store.Close()

	// Create executor with low thresholds for testing
	cfg := DefaultConfig()
	cfg.Store = store
	cfg.MaxEscalationAttempts = 2
	cfg.MaxEscalationDuration = 100 * time.Millisecond

	exec, err := New(cfg)
	if err != nil {
		t.Fatalf("Failed to create executor: %v", err)
	}

	// Create a baseline issue
	baselineIssue := &types.Issue{
		ID:          BaselineBuildIssueID,
		Title:       "Baseline build gate failing",
		IssueType:   types.TypeTask,
		Priority:    0,
		Status:      types.StatusOpen,
		Description: "Build gate is failing on main branch",
	}
	if err = store.CreateIssue(ctx, baselineIssue, "test"); err != nil {
		t.Fatalf("Failed to create baseline issue: %v", err)
	}

	// Add baseline-failure label
	if err := store.AddLabel(ctx, BaselineBuildIssueID, "baseline-failure", "test"); err != nil {
		t.Fatalf("Failed to add baseline-failure label: %v", err)
	}

	// Test 1: Should not escalate initially
	if exec.shouldEscalate(ctx) {
		t.Error("Should not escalate before any attempts")
	}

	// Test 2: Increment attempts to threshold
	exec.incrementAttempt(BaselineBuildIssueID)
	if exec.shouldEscalate(ctx) {
		t.Error("Should not escalate after 1 attempt (threshold: 2)")
	}

	exec.incrementAttempt(BaselineBuildIssueID)
	// Test 3: Should escalate after reaching attempt threshold
	if !exec.shouldEscalate(ctx) {
		t.Error("Should escalate after 2 attempts (threshold: 2)")
	}

	// Verify escalation issue was created
	filter := types.WorkFilter{
		Status: types.StatusOpen,
		Limit:  100,
	}
	issues, err := store.GetReadyWork(ctx, filter)
	if err != nil {
		t.Fatalf("Failed to get issues: %v", err)
	}

	foundEscalation := false
	for _, issue := range issues {
		labels, _ := store.GetLabels(ctx, issue.ID)
		for _, label := range labels {
			if label == "escalation" {
				foundEscalation = true
				t.Logf("Found escalation issue: %s - %s", issue.ID, issue.Title)
				break
			}
		}
		if foundEscalation {
			break
		}
	}

	if !foundEscalation {
		t.Error("Escalation issue should have been created by shouldEscalate()")
	}
}
