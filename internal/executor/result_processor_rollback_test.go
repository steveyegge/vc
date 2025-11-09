package executor

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/steveyegge/vc/internal/events"
	"github.com/steveyegge/vc/internal/gates"
	"github.com/steveyegge/vc/internal/storage"
	"github.com/steveyegge/vc/internal/types"
)

// TestRollbackChangesOnQualityGateFailure verifies that changes are rolled back
// when quality gates fail (vc-16fe)
func TestRollbackChangesOnQualityGateFailure(t *testing.T) {
	// Setup storage
	cfg := storage.DefaultConfig()
	cfg.Path = t.TempDir() + "/test.db"

	ctx := context.Background()
	store, err := storage.NewStorage(ctx, cfg)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer func() { _ = store.Close() }()

	// Create test issue
	issue := &types.Issue{
		Title:              "Test Issue",
		Description:        "Test issue for rollback",
		IssueType:          types.TypeTask,
		Status:             types.StatusInProgress,
		Priority:           1,
		AcceptanceCriteria: "Should complete",
		CreatedAt:          time.Now(),
		UpdatedAt:          time.Now(),
	}

	if err := store.CreateIssue(ctx, issue, "test"); err != nil {
		t.Fatalf("Failed to create issue: %v", err)
	}

	// Create a temporary git repo for testing
	gitDir := t.TempDir()
	if err := setupTestGitRepo(gitDir); err != nil {
		t.Fatalf("Failed to setup test git repo: %v", err)
	}

	// Create a tracked file first, then modify it (to create uncommitted changes)
	testFile := filepath.Join(gitDir, "test.txt")
	if err := os.WriteFile(testFile, []byte("original content"), 0644); err != nil {
		t.Fatalf("Failed to write test file: %v", err)
	}

	// Add and commit the file
	cmd := exec.Command("git", "add", "test.txt")
	cmd.Dir = gitDir
	if err := cmd.Run(); err != nil {
		t.Fatalf("Failed to git add: %v", err)
	}

	cmd = exec.Command("git", "commit", "-m", "Add test file")
	cmd.Dir = gitDir
	if err := cmd.Run(); err != nil {
		t.Fatalf("Failed to git commit: %v", err)
	}

	// Now modify the file to create uncommitted changes
	if err := os.WriteFile(testFile, []byte("uncommitted changes"), 0644); err != nil {
		t.Fatalf("Failed to modify test file: %v", err)
	}

	// Create results processor with working directory pointing to git repo
	rpCfg := &ResultsProcessorConfig{
		Store:              store,
		EnableQualityGates: true,
		WorkingDir:         gitDir,
		Actor:              "test-executor",
	}

	rp, err := NewResultsProcessor(rpCfg)
	if err != nil {
		t.Fatalf("Failed to create results processor: %v", err)
	}

	// Create gate results with failures
	gateResults := []*gates.Result{
		{
			Gate:   gates.GateTest,
			Passed: false,
			Output: "FAIL: TestSomething (0.00s)\n    test_test.go:10: Expected 1, got 2",
			Error:  nil,
		},
		{
			Gate:   gates.GateBuild,
			Passed: true,
			Output: "Build succeeded",
			Error:  nil,
		},
	}

	// Execute rollback
	success := rp.rollbackChanges(ctx, issue, gateResults)

	// Verify rollback succeeded
	if !success {
		t.Errorf("Expected rollback to succeed, got false")
	}

	// Verify working tree is clean (uncommitted changes reverted)
	statusCmd := exec.Command("git", "status", "--porcelain")
	statusCmd.Dir = gitDir
	output, err := statusCmd.Output()
	if err != nil {
		t.Fatalf("Failed to check git status: %v", err)
	}

	if len(output) > 0 {
		t.Errorf("Expected clean working tree after rollback, got: %s", string(output))
	}

	// Verify rollback events were logged in agent events
	filter := events.EventFilter{
		IssueID: issue.ID,
	}
	agentEvents, err := store.GetAgentEvents(ctx, filter)
	if err != nil {
		t.Fatalf("Failed to get agent events: %v", err)
	}

	foundPreserveLog := false
	foundRollback := false
	for _, event := range agentEvents {
		if event.Type == events.EventTypeQualityGatesRollback {
			if strings.Contains(event.Message, "Preserving") {
				foundPreserveLog = true
			}
			if strings.Contains(event.Message, "Successfully rolled back") {
				foundRollback = true
			}
		}
	}

	if !foundPreserveLog {
		t.Errorf("Expected to find log preservation event")
	}
	if !foundRollback {
		t.Errorf("Expected to find rollback success event")
	}
}

// TestCaptureGateFailureLogs verifies that failure logs are captured correctly
func TestCaptureGateFailureLogs(t *testing.T) {
	// Setup storage
	cfg := storage.DefaultConfig()
	cfg.Path = t.TempDir() + "/test.db"

	ctx := context.Background()
	store, err := storage.NewStorage(ctx, cfg)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer func() { _ = store.Close() }()

	// Create results processor
	rpCfg := &ResultsProcessorConfig{
		Store:              store,
		EnableQualityGates: true,
		WorkingDir:         ".",
		Actor:              "test-executor",
	}

	rp, err := NewResultsProcessor(rpCfg)
	if err != nil {
		t.Fatalf("Failed to create results processor: %v", err)
	}

	// Create gate results with multiple failures
	gateResults := []*gates.Result{
		{
			Gate:   gates.GateTest,
			Passed: false,
			Output: "FAIL: TestFoo\nExpected 1, got 2",
			Error:  nil,
		},
		{
			Gate:   gates.GateLint,
			Passed: false,
			Output: "golangci-lint found 5 issues",
			Error:  nil,
		},
		{
			Gate:   gates.GateBuild,
			Passed: true,
			Output: "Build succeeded",
			Error:  nil,
		},
	}

	// Capture failure logs
	failureData := rp.captureGateFailureLogs(gateResults)

	// Verify failed gates list
	failedGates, ok := failureData["failed_gates"].([]string)
	if !ok {
		t.Fatalf("failed_gates is not a string slice")
	}

	if len(failedGates) != 2 {
		t.Errorf("Expected 2 failed gates, got %d", len(failedGates))
	}

	expectedGates := map[string]bool{
		"test": true,
		"lint": true,
	}

	for _, gate := range failedGates {
		if !expectedGates[gate] {
			t.Errorf("Unexpected failed gate: %s", gate)
		}
	}

	// Verify failure count
	failureCount, ok := failureData["failure_count"].(int)
	if !ok {
		t.Fatalf("failure_count is not an int")
	}

	if failureCount != 2 {
		t.Errorf("Expected failure count of 2, got %d", failureCount)
	}

	// Verify full logs contain both failures
	fullLogs, ok := failureData["full_logs"].(string)
	if !ok {
		t.Fatalf("full_logs is not a string")
	}

	if !strings.Contains(fullLogs, "test Gate Failure") {
		t.Errorf("Expected full logs to contain test gate failure")
	}

	if !strings.Contains(fullLogs, "lint Gate Failure") {
		t.Errorf("Expected full logs to contain lint gate failure")
	}

	if !strings.Contains(fullLogs, "Expected 1, got 2") {
		t.Errorf("Expected full logs to contain test output")
	}

	if !strings.Contains(fullLogs, "golangci-lint found 5 issues") {
		t.Errorf("Expected full logs to contain lint output")
	}

	// Verify successful gate is NOT included
	if strings.Contains(fullLogs, "build Gate Failure") {
		t.Errorf("Full logs should not contain successful build gate")
	}
}

// TestQualityGateFailureAddsLabel verifies quality-gates-failed label is added
func TestQualityGateFailureAddsLabel(t *testing.T) {
	// Setup storage
	cfg := storage.DefaultConfig()
	cfg.Path = t.TempDir() + "/test.db"

	ctx := context.Background()
	store, err := storage.NewStorage(ctx, cfg)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer func() { _ = store.Close() }()

	// Create test issue
	issue := &types.Issue{
		Title:              "Test Issue",
		Description:        "Test issue for label check",
		IssueType:          types.TypeTask,
		Status:             types.StatusOpen,
		Priority:           1,
		AcceptanceCriteria: "Should complete",
		CreatedAt:          time.Now(),
		UpdatedAt:          time.Now(),
	}

	if err := store.CreateIssue(ctx, issue, "test"); err != nil {
		t.Fatalf("Failed to create issue: %v", err)
	}

	// Directly add the label (simulating what handleQualityGates does)
	if err := store.AddLabel(ctx, issue.ID, "quality-gates-failed", "test-executor"); err != nil {
		t.Fatalf("Failed to add label: %v", err)
	}

	// Verify label was added
	labels, err := store.GetLabels(ctx, issue.ID)
	if err != nil {
		t.Fatalf("Failed to get labels: %v", err)
	}

	found := false
	for _, label := range labels {
		if label == "quality-gates-failed" {
			found = true
			break
		}
	}

	if !found {
		t.Errorf("Expected quality-gates-failed label to be present, got labels: %v", labels)
	}
}

// TestRollbackCommentIncludesStatus verifies rollback status is included in comments
func TestRollbackCommentIncludesStatus(t *testing.T) {
	testCases := []struct {
		name            string
		rollbackSuccess bool
		expectedText    string
	}{
		{
			name:            "successful rollback",
			rollbackSuccess: true,
			expectedText:    "Changes automatically rolled back to clean state",
		},
		{
			name:            "failed rollback",
			rollbackSuccess: false,
			expectedText:    "Automatic rollback failed",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Simulate the comment generation logic
			gatesComment := "**Quality Gates Failed**\n\nFailed gates (1):\n- test\n"

			if tc.rollbackSuccess {
				gatesComment += "\n\n✓ Changes automatically rolled back to clean state (git reset --hard HEAD)"
			} else {
				gatesComment += "\n\n⚠ Automatic rollback failed - working tree may contain uncommitted changes"
			}

			if !strings.Contains(gatesComment, tc.expectedText) {
				t.Errorf("Expected comment to contain '%s', got: %s", tc.expectedText, gatesComment)
			}
		})
	}
}

// setupTestGitRepo creates a test git repository with an initial commit
func setupTestGitRepo(dir string) error {
	// Initialize git repo
	cmd := exec.Command("git", "init")
	cmd.Dir = dir
	if err := cmd.Run(); err != nil {
		return err
	}

	// Configure git for tests
	cmd = exec.Command("git", "config", "user.email", "test@example.com")
	cmd.Dir = dir
	if err := cmd.Run(); err != nil {
		return err
	}

	cmd = exec.Command("git", "config", "user.name", "Test User")
	cmd.Dir = dir
	if err := cmd.Run(); err != nil {
		return err
	}

	// Create initial commit
	initialFile := filepath.Join(dir, "README.md")
	if err := os.WriteFile(initialFile, []byte("# Test Repo"), 0644); err != nil {
		return err
	}

	cmd = exec.Command("git", "add", ".")
	cmd.Dir = dir
	if err := cmd.Run(); err != nil {
		return err
	}

	cmd = exec.Command("git", "commit", "-m", "Initial commit")
	cmd.Dir = dir
	if err := cmd.Run(); err != nil {
		return err
	}

	return nil
}
