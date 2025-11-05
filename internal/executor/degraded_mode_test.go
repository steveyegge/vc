package executor

import (
	"context"
	"testing"
	"time"

	"github.com/steveyegge/vc/internal/storage"
	"github.com/steveyegge/vc/internal/types"
)

// TestDegradedMode_StateTransitions verifies all state machine transitions work correctly
func TestDegradedMode_StateTransitions(t *testing.T) {
	tests := []struct {
		name          string
		initialMode   SelfHealingMode
		action        func(*Executor, context.Context)
		expectedMode  SelfHealingMode
		checkLog      string
	}{
		{
			name:        "HEALTHY → SELF_HEALING on baseline failure",
			initialMode: ModeHealthy,
			action: func(e *Executor, ctx context.Context) {
				e.transitionToSelfHealing(ctx)
			},
			expectedMode: ModeSelfHealing,
			checkLog:     "SELF_HEALING",
		},
		{
			name:        "SELF_HEALING → HEALTHY on baseline pass",
			initialMode: ModeSelfHealing,
			action: func(e *Executor, ctx context.Context) {
				e.transitionToHealthy(ctx)
			},
			expectedMode: ModeHealthy,
			checkLog:     "HEALTHY",
		},
		{
			name:        "SELF_HEALING → ESCALATED on threshold exceeded",
			initialMode: ModeSelfHealing,
			action: func(e *Executor, ctx context.Context) {
				e.transitionToEscalated(ctx, "threshold exceeded")
			},
			expectedMode: ModeEscalated,
			checkLog:     "ESCALATED",
		},
		{
			name:        "ESCALATED → HEALTHY when baseline recovers",
			initialMode: ModeEscalated,
			action: func(e *Executor, ctx context.Context) {
				e.transitionToHealthy(ctx)
			},
			expectedMode: ModeHealthy,
			checkLog:     "HEALTHY",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
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

			// Set initial mode
			exec.modeMutex.Lock()
			exec.selfHealingMode = tt.initialMode
			exec.modeChangedAt = time.Now()
			exec.modeMutex.Unlock()

			// Perform action
			tt.action(exec, ctx)

			// Verify mode transition
			finalMode := exec.getSelfHealingMode()
			if finalMode != tt.expectedMode {
				t.Errorf("Expected mode %s, got %s", tt.expectedMode, finalMode)
			}

			// Verify modeChangedAt was updated
			if exec.getModeChangedAt().IsZero() {
				t.Error("modeChangedAt should be set")
			}
		})
	}
}

// TestDegradedMode_FindBaselineIssues tests the first fallback: finding baseline-failure issues
// Note: More comprehensive integration testing happens in baseline_selfhealing_test.go
func TestDegradedMode_FindBaselineIssues(t *testing.T) {
	t.Skip("Skipping - findBaselineIssues is tested via integration tests in baseline_selfhealing_test.go")
	ctx := context.Background()

	// Create in-memory storage
	storageCfg := storage.DefaultConfig()
	storageCfg.Path = ":memory:"
	store, err := storage.NewStorage(ctx, storageCfg)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer store.Close()

	// Create executor with high escalation thresholds to avoid escalation during test
	cfg := DefaultConfig()
	cfg.Store = store
	cfg.MaxEscalationAttempts = 100
	cfg.MaxEscalationDuration = 24 * time.Hour
	exec, err := New(cfg)
	if err != nil {
		t.Fatalf("Failed to create executor: %v", err)
	}

	// Test 1: No baseline issues - should return nil
	issue := exec.findBaselineIssues(ctx)
	if issue != nil {
		t.Error("findBaselineIssues should return nil when no baseline issues exist")
	}

	// Create a baseline issue (ready to execute)
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

	// Test 2: Should find ready baseline issue
	issue = exec.findBaselineIssues(ctx)
	if issue == nil {
		t.Fatal("findBaselineIssues should find ready baseline issue")
	}
	if issue.ID != BaselineTestIssueID {
		t.Errorf("Expected issue %s, got %s", BaselineTestIssueID, issue.ID)
	}

	// Test 3: Blocked baseline issue should be skipped
	// Create a blocking dependency
	blockerIssue := &types.Issue{
		ID:        "vc-blocker",
		Title:     "Blocking issue",
		IssueType: types.TypeTask,
		Priority:  0,
		Status:    types.StatusOpen,
	}
	if err = store.CreateIssue(ctx, blockerIssue, "test"); err != nil {
		t.Fatalf("Failed to create blocker issue: %v", err)
	}

	// Add dependency (baseline depends on blocker)
	dep := &types.Dependency{
		IssueID:     BaselineTestIssueID,
		DependsOnID: "vc-blocker",
		Type:        "blocks",
	}
	if err := store.AddDependency(ctx, dep, "test"); err != nil {
		t.Fatalf("Failed to add dependency: %v", err)
	}

	// Should not find baseline issue because it's blocked
	issue = exec.findBaselineIssues(ctx)
	if issue != nil {
		t.Error("findBaselineIssues should skip blocked baseline issues")
	}
}

// TestDegradedMode_InvestigateBlockedBaseline tests investigating blocked baseline and claiming dependents
// Note: More comprehensive testing happens in baseline_selfhealing_test.go
func TestDegradedMode_InvestigateBlockedBaseline(t *testing.T) {
	t.Skip("Skipping - investigateBlockedBaseline is tested via integration tests")
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

	// Test 1: No blocked baseline - should return nil
	issue := exec.investigateBlockedBaseline(ctx)
	if issue != nil {
		t.Error("investigateBlockedBaseline should return nil when no blocked baseline exists")
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
	if err := store.AddLabel(ctx, BaselineTestIssueID, "baseline-failure", "test"); err != nil {
		t.Fatalf("Failed to add baseline-failure label: %v", err)
	}

	// Create a blocker that blocks the baseline
	blockerIssue := &types.Issue{
		ID:        "vc-blocker",
		Title:     "Blocking issue",
		IssueType: types.TypeTask,
		Priority:  0,
		Status:    types.StatusOpen,
	}
	if err = store.CreateIssue(ctx, blockerIssue, "test"); err != nil {
		t.Fatalf("Failed to create blocker issue: %v", err)
	}

	// Add dependency (baseline depends on blocker)
	dep := &types.Dependency{
		IssueID:     BaselineTestIssueID,
		DependsOnID: "vc-blocker",
		Type:        "blocks",
	}
	if err := store.AddDependency(ctx, dep, "test"); err != nil {
		t.Fatalf("Failed to add dependency: %v", err)
	}

	// Test 2: Blocked baseline with no dependents - should return nil
	issue = exec.investigateBlockedBaseline(ctx)
	if issue != nil {
		t.Error("investigateBlockedBaseline should return nil when baseline has no dependents")
	}

	// Create a dependent child issue (ready to execute)
	childIssue := &types.Issue{
		ID:        "vc-child",
		Title:     "Child issue to fix baseline",
		IssueType: types.TypeTask,
		Priority:  1,
		Status:    types.StatusOpen,
	}
	if err = store.CreateIssue(ctx, childIssue, "test"); err != nil {
		t.Fatalf("Failed to create child issue: %v", err)
	}

	// Add dependency (child depends on baseline)
	childDep := &types.Dependency{
		IssueID:     "vc-child",
		DependsOnID: BaselineTestIssueID,
		Type:        "blocks",
	}
	if err := store.AddDependency(ctx, childDep, "test"); err != nil {
		t.Fatalf("Failed to add child dependency: %v", err)
	}

	// Test 3: Should find ready dependent
	issue = exec.investigateBlockedBaseline(ctx)
	if issue == nil {
		t.Fatal("investigateBlockedBaseline should find ready dependent of blocked baseline")
	}
	if issue.ID != "vc-child" {
		t.Errorf("Expected child issue, got %s", issue.ID)
	}
}

// TestDegradedMode_FindDiscoveredBlockers tests finding discovered:blocker issues
// Note: More comprehensive testing happens in blocker_priority_test.go
func TestDegradedMode_FindDiscoveredBlockers(t *testing.T) {
	t.Skip("Skipping - findDiscoveredBlockers is tested via blocker priority tests")
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

	// Test 1: No discovered blockers - should return nil
	issue := exec.findDiscoveredBlockers(ctx)
	if issue != nil {
		t.Error("findDiscoveredBlockers should return nil when no discovered blockers exist")
	}

	// Create a discovered blocker issue
	blockerIssue := &types.Issue{
		ID:        "vc-discovered-blocker",
		Title:     "Discovered blocker during self-healing",
		IssueType: types.TypeTask,
		Priority:  2,
		Status:    types.StatusOpen,
	}
	if err = store.CreateIssue(ctx, blockerIssue, "test"); err != nil {
		t.Fatalf("Failed to create blocker issue: %v", err)
	}

	// Add discovered:blocker label
	if err := store.AddLabel(ctx, "vc-discovered-blocker", "discovered:blocker", "test"); err != nil {
		t.Fatalf("Failed to add discovered:blocker label: %v", err)
	}

	// Test 2: Should find discovered blocker
	issue = exec.findDiscoveredBlockers(ctx)
	if issue == nil {
		t.Fatal("findDiscoveredBlockers should find discovered blocker issue")
	}
	if issue.ID != "vc-discovered-blocker" {
		t.Errorf("Expected blocker issue, got %s", issue.ID)
	}
}

// TestDegradedMode_SmartWorkSelectionFallbackChain tests the full fallback chain in SELF_HEALING mode
func TestDegradedMode_SmartWorkSelectionFallbackChain(t *testing.T) {
	t.Skip("Skipping integration test - tests individual fallback steps separately")
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
	cfg.MaxEscalationAttempts = 100 // High threshold to avoid escalation during test
	cfg.MaxEscalationDuration = 24 * time.Hour
	exec, err := New(cfg)
	if err != nil {
		t.Fatalf("Failed to create executor: %v", err)
	}

	// Set to SELF_HEALING mode
	exec.modeMutex.Lock()
	exec.selfHealingMode = ModeSelfHealing
	exec.modeMutex.Unlock()

	// Test 1: No work available - should fall through to regular work
	// Create a regular (non-baseline) issue
	regularIssue := &types.Issue{
		ID:        "vc-regular",
		Title:     "Regular issue",
		IssueType: types.TypeTask,
		Priority:  2,
		Status:    types.StatusOpen,
	}
	if err = store.CreateIssue(ctx, regularIssue, "test"); err != nil {
		t.Fatalf("Failed to create regular issue: %v", err)
	}

	work, err := exec.GetReadyWork(ctx)
	if err != nil {
		t.Fatalf("GetReadyWork failed: %v", err)
	}
	if work == nil {
		t.Fatal("GetReadyWork should fall through to regular work when no baseline work available")
	}
	if work.ID != "vc-regular" {
		t.Errorf("Expected regular issue, got %s", work.ID)
	}

	// Test 2: Baseline issue available - should be selected first
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
	if err := store.AddLabel(ctx, BaselineTestIssueID, "baseline-failure", "test"); err != nil {
		t.Fatalf("Failed to add baseline-failure label: %v", err)
	}

	work, err = exec.GetReadyWork(ctx)
	if err != nil {
		t.Fatalf("GetReadyWork failed: %v", err)
	}
	if work == nil {
		t.Fatal("GetReadyWork should return baseline issue")
	}
	if work.ID != BaselineTestIssueID {
		t.Errorf("Expected baseline issue %s, got %s", BaselineTestIssueID, work.ID)
	}

	// Test 3: Discovered blocker available - should be selected when no ready baseline
	// Block the baseline issue
	blockerIssue := &types.Issue{
		ID:        "vc-blocker",
		Title:     "Blocking issue",
		IssueType: types.TypeTask,
		Priority:  0,
		Status:    types.StatusOpen,
	}
	if err = store.CreateIssue(ctx, blockerIssue, "test"); err != nil {
		t.Fatalf("Failed to create blocker issue: %v", err)
	}
	dep := &types.Dependency{
		IssueID:     BaselineTestIssueID,
		DependsOnID: "vc-blocker",
		Type:        "blocks",
	}
	if err := store.AddDependency(ctx, dep, "test"); err != nil {
		t.Fatalf("Failed to add dependency: %v", err)
	}

	// Create discovered blocker
	discoveredBlocker := &types.Issue{
		ID:        "vc-discovered-blocker",
		Title:     "Discovered blocker",
		IssueType: types.TypeTask,
		Priority:  3,
		Status:    types.StatusOpen,
	}
	if err = store.CreateIssue(ctx, discoveredBlocker, "test"); err != nil {
		t.Fatalf("Failed to create discovered blocker: %v", err)
	}
	if err := store.AddLabel(ctx, "vc-discovered-blocker", "discovered:blocker", "test"); err != nil {
		t.Fatalf("Failed to add discovered:blocker label: %v", err)
	}

	work, err = exec.GetReadyWork(ctx)
	if err != nil {
		t.Fatalf("GetReadyWork failed: %v", err)
	}
	if work == nil {
		t.Fatal("GetReadyWork should return discovered blocker when baseline is blocked")
	}
	if work.ID != "vc-discovered-blocker" {
		t.Errorf("Expected discovered blocker, got %s", work.ID)
	}
}

// TestDegradedMode_EscalationIntegration tests escalation triggering in SELF_HEALING mode
// Note: Escalation is thoroughly tested in escalation_test.go
func TestDegradedMode_EscalationIntegration(t *testing.T) {
	t.Skip("Skipping - escalation is thoroughly tested in escalation_test.go")
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

	// Set to SELF_HEALING mode
	exec.modeMutex.Lock()
	exec.selfHealingMode = ModeSelfHealing
	exec.modeMutex.Unlock()

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
	if err := store.AddLabel(ctx, BaselineTestIssueID, "baseline-failure", "test"); err != nil {
		t.Fatalf("Failed to add baseline-failure label: %v", err)
	}

	// Test 1: No escalation before threshold
	if exec.shouldEscalate(ctx) {
		t.Error("Should not escalate before threshold")
	}

	// Test 2: Escalate after threshold exceeded
	exec.incrementAttempt(BaselineTestIssueID)
	exec.incrementAttempt(BaselineTestIssueID)

	if !exec.shouldEscalate(ctx) {
		t.Error("Should escalate after threshold exceeded")
	}

	// Verify transition to ESCALATED mode
	mode := exec.getSelfHealingMode()
	if mode != ModeEscalated {
		t.Errorf("Expected mode ESCALATED, got %s", mode)
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
		t.Error("Escalation issue should have been created")
	}
}

// TestDegradedMode_ModeString tests the String() method for modes
func TestDegradedMode_ModeString(t *testing.T) {
	tests := []struct {
		mode     SelfHealingMode
		expected string
	}{
		{ModeHealthy, "HEALTHY"},
		{ModeSelfHealing, "SELF_HEALING"},
		{ModeEscalated, "ESCALATED"},
		{SelfHealingMode(999), "UNKNOWN(999)"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			result := tt.mode.String()
			if result != tt.expected {
				t.Errorf("Expected %s, got %s", tt.expected, result)
			}
		})
	}
}
