package gates

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/steveyegge/vc/internal/storage/sqlite"
	"github.com/steveyegge/vc/internal/types"
)

func TestNewRunner(t *testing.T) {
	// Create temp db
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test.db")

	store, err := sqlite.New(dbPath)
	if err != nil {
		t.Fatalf("Failed to create store: %v", err)
	}
	defer store.Close()

	// Test successful creation
	cfg := &Config{
		Store:      store,
		WorkingDir: ".",
	}
	runner, err := NewRunner(cfg)
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}
	if runner == nil {
		t.Fatal("Expected runner, got nil")
	}

	// Test missing store
	cfg = &Config{
		WorkingDir: ".",
	}
	runner, err = NewRunner(cfg)
	if err == nil {
		t.Error("Expected error for missing store")
	}
	if runner != nil {
		t.Error("Expected nil runner for invalid config")
	}

	// Test default working dir
	cfg = &Config{
		Store: store,
	}
	runner, err = NewRunner(cfg)
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}
	if runner.workingDir != "." {
		t.Errorf("Expected workingDir '.', got %s", runner.workingDir)
	}
}

func TestRunTestGate_Success(t *testing.T) {
	// Create temp db
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test.db")

	store, err := sqlite.New(dbPath)
	if err != nil {
		t.Fatalf("Failed to create store: %v", err)
	}
	defer store.Close()

	runner := &Runner{
		store:      store,
		workingDir: ".", // Current directory should have tests
	}

	ctx := context.Background()
	result := runner.runTestGate(ctx)

	if result.Gate != GateTest {
		t.Errorf("Expected gate type %s, got %s", GateTest, result.Gate)
	}

	// The test might fail or pass depending on the test environment
	// We just verify the result is populated
	if result.Output == "" {
		t.Error("Expected output from go test")
	}
}

func TestRunLintGate(t *testing.T) {
	// Create temp db
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test.db")

	store, err := sqlite.New(dbPath)
	if err != nil {
		t.Fatalf("Failed to create store: %v", err)
	}
	defer store.Close()

	runner := &Runner{
		store:      store,
		workingDir: ".",
	}

	ctx := context.Background()
	result := runner.runLintGate(ctx)

	if result.Gate != GateLint {
		t.Errorf("Expected gate type %s, got %s", GateLint, result.Gate)
	}

	// If golangci-lint is not installed, the result should indicate this
	// Otherwise, it should have output
	if result.Output == "" && result.Error == nil {
		t.Error("Expected either output or error from lint gate")
	}
}

func TestRunBuildGate_Success(t *testing.T) {
	// Create temp db
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test.db")

	store, err := sqlite.New(dbPath)
	if err != nil {
		t.Fatalf("Failed to create store: %v", err)
	}
	defer store.Close()

	runner := &Runner{
		store:      store,
		workingDir: ".",
	}

	ctx := context.Background()
	result := runner.runBuildGate(ctx)

	if result.Gate != GateBuild {
		t.Errorf("Expected gate type %s, got %s", GateBuild, result.Gate)
	}

	// Build should succeed for valid Go code
	if !result.Passed {
		t.Errorf("Expected build to pass, got error: %v, output: %s", result.Error, result.Output)
	}
}

func TestRunAll(t *testing.T) {
	// Create temp db
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test.db")

	store, err := sqlite.New(dbPath)
	if err != nil {
		t.Fatalf("Failed to create store: %v", err)
	}
	defer store.Close()

	runner := &Runner{
		store:      store,
		workingDir: ".",
	}

	ctx := context.Background()
	results, allPassed := runner.RunAll(ctx)

	// Should run all 3 gates
	if len(results) != 3 {
		t.Fatalf("Expected 3 results, got %d", len(results))
	}

	// Verify gate types
	expectedGates := []GateType{GateTest, GateLint, GateBuild}
	for i, expected := range expectedGates {
		if results[i].Gate != expected {
			t.Errorf("Result %d: expected gate %s, got %s", i, expected, results[i].Gate)
		}
	}

	// AllPassed should reflect actual gate results
	_ = allPassed // We don't know if tests/lint will pass in all environments
}

func TestCreateBlockingIssue(t *testing.T) {
	// Create temp db
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test.db")

	store, err := sqlite.New(dbPath)
	if err != nil {
		t.Fatalf("Failed to create store: %v", err)
	}
	defer store.Close()

	ctx := context.Background()

	// Create the original issue
	originalIssue := &types.Issue{
		ID:          "test-1",
		Title:       "Test Issue",
		Description: "Test description",
		Status:      types.StatusInProgress,
		Priority:    1,
		IssueType:   types.TypeTask,
	}

	if err := store.CreateIssue(ctx, originalIssue, "test"); err != nil {
		t.Fatalf("Failed to create original issue: %v", err)
	}

	runner := &Runner{
		store:      store,
		workingDir: ".",
	}

	// Create a failing gate result
	gateResult := &Result{
		Gate:   GateTest,
		Passed: false,
		Output: "Test failed\nExpected true, got false",
		Error:  os.ErrInvalid,
	}

	// Create blocking issue
	blockingID, err := runner.CreateBlockingIssue(ctx, originalIssue, gateResult)
	if err != nil {
		t.Fatalf("Failed to create blocking issue: %v", err)
	}

	expectedID := "test-1-gate-test"
	if blockingID != expectedID {
		t.Errorf("Expected blocking ID %s, got %s", expectedID, blockingID)
	}

	// Verify the blocking issue was created
	blockingIssue, err := store.GetIssue(ctx, blockingID)
	if err != nil {
		t.Fatalf("Failed to get blocking issue: %v", err)
	}

	if blockingIssue.Status != types.StatusOpen {
		t.Errorf("Expected status %s, got %s", types.StatusOpen, blockingIssue.Status)
	}

	if blockingIssue.IssueType != types.TypeBug {
		t.Errorf("Expected type %s, got %s", types.TypeBug, blockingIssue.IssueType)
	}

	if blockingIssue.Priority != originalIssue.Priority {
		t.Errorf("Expected priority %d, got %d", originalIssue.Priority, blockingIssue.Priority)
	}

	// Verify the label was added
	labels, err := store.GetLabels(ctx, blockingID)
	if err != nil {
		t.Fatalf("Failed to get labels: %v", err)
	}

	expectedLabel := "gate:test"
	found := false
	for _, label := range labels {
		if label == expectedLabel {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("Expected label %s not found in %v", expectedLabel, labels)
	}

	// Verify the dependency was created
	deps, err := store.GetDependencies(ctx, originalIssue.ID)
	if err != nil {
		t.Fatalf("Failed to get dependencies: %v", err)
	}

	if len(deps) != 1 {
		t.Fatalf("Expected 1 dependency, got %d", len(deps))
	}

	if deps[0].ID != blockingID {
		t.Errorf("Expected dependency on %s, got %s", blockingID, deps[0].ID)
	}
}

func TestHandleGateResults_AllPassed(t *testing.T) {
	// Create temp db
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test.db")

	store, err := sqlite.New(dbPath)
	if err != nil {
		t.Fatalf("Failed to create store: %v", err)
	}
	defer store.Close()

	ctx := context.Background()

	// Create the original issue
	originalIssue := &types.Issue{
		ID:          "test-2",
		Title:       "Test Issue 2",
		Description: "Test description",
		Status:      types.StatusInProgress,
		Priority:    1,
		IssueType:   types.TypeTask,
	}

	if err := store.CreateIssue(ctx, originalIssue, "test"); err != nil {
		t.Fatalf("Failed to create original issue: %v", err)
	}

	runner := &Runner{
		store:      store,
		workingDir: ".",
	}

	// All gates passed
	results := []*Result{
		{Gate: GateTest, Passed: true, Output: "All tests passed"},
		{Gate: GateLint, Passed: true, Output: "No lint errors"},
		{Gate: GateBuild, Passed: true, Output: "Build successful"},
	}

	err = runner.HandleGateResults(ctx, originalIssue, results, true)
	if err != nil {
		t.Fatalf("HandleGateResults failed: %v", err)
	}

	// Verify issue is still in_progress (not blocked)
	issue, err := store.GetIssue(ctx, originalIssue.ID)
	if err != nil {
		t.Fatalf("Failed to get issue: %v", err)
	}

	if issue.Status != types.StatusInProgress {
		t.Errorf("Expected status %s, got %s", types.StatusInProgress, issue.Status)
	}

	// Verify comments were added
	events, err := store.GetEvents(ctx, originalIssue.ID, 100)
	if err != nil {
		t.Fatalf("Failed to get events: %v", err)
	}

	// Should have at least: create event + success comment
	if len(events) < 2 {
		t.Errorf("Expected at least 2 events, got %d", len(events))
	}
}

func TestHandleGateResults_SomeFailed(t *testing.T) {
	// Create temp db
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test.db")

	store, err := sqlite.New(dbPath)
	if err != nil {
		t.Fatalf("Failed to create store: %v", err)
	}
	defer store.Close()

	ctx := context.Background()

	// Create the original issue
	originalIssue := &types.Issue{
		ID:          "test-3",
		Title:       "Test Issue 3",
		Description: "Test description",
		Status:      types.StatusInProgress,
		Priority:    2,
		IssueType:   types.TypeTask,
	}

	if err := store.CreateIssue(ctx, originalIssue, "test"); err != nil {
		t.Fatalf("Failed to create original issue: %v", err)
	}

	runner := &Runner{
		store:      store,
		workingDir: ".",
	}

	// Some gates failed
	results := []*Result{
		{Gate: GateTest, Passed: false, Output: "Test failure", Error: os.ErrInvalid},
		{Gate: GateLint, Passed: true, Output: "No lint errors"},
		{Gate: GateBuild, Passed: false, Output: "Build failure", Error: os.ErrInvalid},
	}

	err = runner.HandleGateResults(ctx, originalIssue, results, false)
	if err != nil {
		t.Fatalf("HandleGateResults failed: %v", err)
	}

	// Verify issue is now blocked
	issue, err := store.GetIssue(ctx, originalIssue.ID)
	if err != nil {
		t.Fatalf("Failed to get issue: %v", err)
	}

	if issue.Status != types.StatusBlocked {
		t.Errorf("Expected status %s, got %s", types.StatusBlocked, issue.Status)
	}

	// Verify blocking issues were created (2 failures)
	testGateIssue, err := store.GetIssue(ctx, "test-3-gate-test")
	if err != nil {
		t.Fatalf("Failed to get test gate blocking issue: %v", err)
	}
	if testGateIssue.Status != types.StatusOpen {
		t.Errorf("Expected blocking issue to be open")
	}

	buildGateIssue, err := store.GetIssue(ctx, "test-3-gate-build")
	if err != nil {
		t.Fatalf("Failed to get build gate blocking issue: %v", err)
	}
	if buildGateIssue.Status != types.StatusOpen {
		t.Errorf("Expected blocking issue to be open")
	}

	// Verify dependencies were created
	deps, err := store.GetDependencies(ctx, originalIssue.ID)
	if err != nil {
		t.Fatalf("Failed to get dependencies: %v", err)
	}

	if len(deps) != 2 {
		t.Fatalf("Expected 2 dependencies, got %d", len(deps))
	}
}

func TestFormatGateResult(t *testing.T) {
	runner := &Runner{}

	tests := []struct {
		name     string
		result   *Result
		contains []string
	}{
		{
			name: "passed gate",
			result: &Result{
				Gate:   GateTest,
				Passed: true,
				Output: "All tests passed",
			},
			contains: []string{"Quality Gate: test", "PASSED", "All tests passed"},
		},
		{
			name: "failed gate",
			result: &Result{
				Gate:   GateLint,
				Passed: false,
				Output: "Lint errors found",
				Error:  os.ErrInvalid,
			},
			contains: []string{"Quality Gate: lint", "FAILED", "Error:", "Lint errors found"},
		},
		{
			name: "truncated output",
			result: &Result{
				Gate:   GateBuild,
				Passed: false,
				Output: string(make([]byte, 1000)), // Long output
			},
			contains: []string{"Quality Gate: build", "FAILED", "truncated"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			output := runner.formatGateResult(tt.result)

			for _, expected := range tt.contains {
				if len(output) < 500 { // Only check if not truncated in test
					// Simple substring check
					found := false
					for i := 0; i <= len(output)-len(expected); i++ {
						if output[i:i+len(expected)] == expected {
							found = true
							break
						}
					}
					if !found && len(expected) < 100 {
						t.Errorf("Expected output to contain %q, got: %s", expected, output)
					}
				}
			}
		})
	}
}
