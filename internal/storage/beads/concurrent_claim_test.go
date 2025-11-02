package beads

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/steveyegge/vc/internal/types"
)

// TestConcurrentGetReadyWork verifies that multiple executors can safely call
// GetReadyWork simultaneously without issues (vc-1db1)
func TestConcurrentGetReadyWork(t *testing.T) {
	ctx := context.Background()

	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")
	store, err := NewVCStorage(ctx, dbPath)
	if err != nil {
		t.Fatalf("Failed to create test storage: %v", err)
	}
	defer func() { _ = store.Close() }()

	// Create several open issues to work with
	numIssues := 10
	issueIDs := make([]string, numIssues)
	for i := 0; i < numIssues; i++ {
		issue := &types.Issue{
			Title:       "Concurrent test issue",
			Description: "Testing concurrent GetReadyWork",
			Status:      types.StatusOpen,
			Priority:    1,
			IssueType:   types.TypeTask,
		}
		err := store.CreateIssue(ctx, issue, "test")
		if err != nil {
			t.Fatalf("Failed to create issue %d: %v", i, err)
		}
		issueIDs[i] = issue.ID
	}

	// Spawn multiple goroutines that all call GetReadyWork simultaneously
	numExecutors := 5
	var wg sync.WaitGroup
	resultsChan := make(chan []*types.Issue, numExecutors)
	errorsChan := make(chan error, numExecutors)

	for i := 0; i < numExecutors; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results, err := store.GetReadyWork(ctx, types.WorkFilter{Limit: 100})
			if err != nil {
				errorsChan <- err
				return
			}
			resultsChan <- results
		}()
	}

	wg.Wait()
	close(resultsChan)
	close(errorsChan)

	// Check that no errors occurred
	for err := range errorsChan {
		t.Errorf("GetReadyWork error in concurrent execution: %v", err)
	}

	// Verify that all executors got consistent results
	var firstResults []*types.Issue
	allResultsCount := 0
	for results := range resultsChan {
		allResultsCount++
		if firstResults == nil {
			firstResults = results
		}
		// All executors should see the same ready work
		if len(results) != len(firstResults) {
			t.Errorf("Inconsistent result count: expected %d, got %d", len(firstResults), len(results))
		}
	}

	if allResultsCount != numExecutors {
		t.Errorf("Expected %d result sets, got %d", numExecutors, allResultsCount)
	}

	// Verify that all created issues are in the results
	if len(firstResults) != numIssues {
		t.Errorf("Expected %d issues in GetReadyWork results, got %d", numIssues, len(firstResults))
	}
}

// TestConcurrentClaimSameIssue verifies that when multiple executors try to claim
// the same issue simultaneously, only one succeeds and the others fail gracefully (vc-1db1)
func TestConcurrentClaimSameIssue(t *testing.T) {
	ctx := context.Background()

	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")
	store, err := NewVCStorage(ctx, dbPath)
	if err != nil {
		t.Fatalf("Failed to create test storage: %v", err)
	}
	defer func() { _ = store.Close() }()

	// Register multiple executor instances
	numExecutors := 5
	executorIDs := make([]string, numExecutors)
	for i := 0; i < numExecutors; i++ {
		instanceID := testInstanceID(i)
		executorIDs[i] = instanceID
		instance := &types.ExecutorInstance{
			InstanceID:    instanceID,
			Hostname:      "localhost",
			PID:           12345 + i,
			Version:       "test",
			StartedAt:     time.Now(),
			LastHeartbeat: time.Now(),
			Status:        "running",
		}
		err := store.RegisterInstance(ctx, instance)
		if err != nil {
			t.Fatalf("Failed to register executor %d: %v", i, err)
		}
	}

	// Create a single open issue
	issue := &types.Issue{
		Title:       "Race condition test issue",
		Description: "Testing concurrent claim attempts",
		Status:      types.StatusOpen,
		Priority:    1,
		IssueType:   types.TypeTask,
	}
	err = store.CreateIssue(ctx, issue, "test")
	if err != nil {
		t.Fatalf("Failed to create issue: %v", err)
	}

	// Spawn multiple goroutines that all try to claim the same issue simultaneously
	var wg sync.WaitGroup
	successChan := make(chan string, numExecutors)
	errorsChan := make(chan error, numExecutors)

	for i := 0; i < numExecutors; i++ {
		wg.Add(1)
		executorID := executorIDs[i]
		go func(execID string) {
			defer wg.Done()
			err := store.ClaimIssue(ctx, issue.ID, execID)
			if err != nil {
				errorsChan <- err
			} else {
				successChan <- execID
			}
		}(executorID)
	}

	wg.Wait()
	close(successChan)
	close(errorsChan)

	// Exactly one executor should succeed
	successCount := 0
	var winner string
	for execID := range successChan {
		successCount++
		winner = execID
	}

	if successCount != 1 {
		t.Errorf("Expected exactly 1 successful claim, got %d", successCount)
	}

	// The rest should fail with appropriate error
	errorCount := 0
	for err := range errorsChan {
		errorCount++
		errMsg := err.Error()
		// Error should indicate the issue is already claimed, or SQLite locking contention
		// SQLITE_BUSY is expected when multiple goroutines race to claim the same issue
		if !contains(errMsg, "already claimed") && !contains(errMsg, "not open") && !contains(errMsg, "database is locked") {
			t.Errorf("Unexpected error message: %v", err)
		}
	}

	if errorCount != numExecutors-1 {
		t.Errorf("Expected %d failed claims, got %d", numExecutors-1, errorCount)
	}

	// Verify the issue is now in_progress and claimed by the winner
	finalIssue, err := store.GetIssue(ctx, issue.ID)
	if err != nil {
		t.Fatalf("Failed to get final issue state: %v", err)
	}

	if finalIssue.Status != types.StatusInProgress {
		t.Errorf("Expected status 'in_progress', got: %s", finalIssue.Status)
	}

	// Verify the execution state shows the correct claimer
	execState, err := store.GetExecutionState(ctx, issue.ID)
	if err != nil {
		t.Fatalf("Failed to get execution state: %v", err)
	}

	if execState.ExecutorInstanceID != winner {
		t.Errorf("Expected executor %s to own the claim, got %s", winner, execState.ExecutorInstanceID)
	}

	if execState.State != types.ExecutionStateClaimed {
		t.Errorf("Expected execution state 'claimed', got: %s", execState.State)
	}
}

// TestClaimedIssueNotInGetReadyWork verifies that once an issue is claimed by one
// executor, it no longer appears in GetReadyWork results for other executors (vc-1db1)
func TestClaimedIssueNotInGetReadyWork(t *testing.T) {
	ctx := context.Background()

	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")
	store, err := NewVCStorage(ctx, dbPath)
	if err != nil {
		t.Fatalf("Failed to create test storage: %v", err)
	}
	defer func() { _ = store.Close() }()

	// Register two executor instances
	executor1 := &types.ExecutorInstance{
		InstanceID:    "executor-1",
		Hostname:      "localhost",
		PID:           12345,
		Version:       "test",
		StartedAt:     time.Now(),
		LastHeartbeat: time.Now(),
		Status:        "running",
	}
	err = store.RegisterInstance(ctx, executor1)
	if err != nil {
		t.Fatalf("Failed to register executor 1: %v", err)
	}

	executor2 := &types.ExecutorInstance{
		InstanceID:    "executor-2",
		Hostname:      "localhost",
		PID:           12346,
		Version:       "test",
		StartedAt:     time.Now(),
		LastHeartbeat: time.Now(),
		Status:        "running",
	}
	err = store.RegisterInstance(ctx, executor2)
	if err != nil {
		t.Fatalf("Failed to register executor 2: %v", err)
	}

	// Create two open issues
	issue1 := &types.Issue{
		Title:       "Issue to be claimed",
		Description: "This will be claimed",
		Status:      types.StatusOpen,
		Priority:    1,
		IssueType:   types.TypeTask,
	}
	err = store.CreateIssue(ctx, issue1, "test")
	if err != nil {
		t.Fatalf("Failed to create issue 1: %v", err)
	}

	issue2 := &types.Issue{
		Title:       "Issue to remain open",
		Description: "This will stay open",
		Status:      types.StatusOpen,
		Priority:    1,
		IssueType:   types.TypeTask,
	}
	err = store.CreateIssue(ctx, issue2, "test")
	if err != nil {
		t.Fatalf("Failed to create issue 2: %v", err)
	}

	// Both executors should see both issues initially
	readyWork, err := store.GetReadyWork(ctx, types.WorkFilter{Limit: 100})
	if err != nil {
		t.Fatalf("Failed to get ready work: %v", err)
	}

	if len(readyWork) != 2 {
		t.Fatalf("Expected 2 ready issues initially, got %d", len(readyWork))
	}

	// Executor 1 claims issue 1
	err = store.ClaimIssue(ctx, issue1.ID, "executor-1")
	if err != nil {
		t.Fatalf("Failed to claim issue 1: %v", err)
	}

	// Now GetReadyWork should only return issue 2
	readyWork, err = store.GetReadyWork(ctx, types.WorkFilter{Limit: 100})
	if err != nil {
		t.Fatalf("Failed to get ready work after claim: %v", err)
	}

	if len(readyWork) != 1 {
		t.Fatalf("Expected 1 ready issue after claim, got %d", len(readyWork))
	}

	if readyWork[0].ID != issue2.ID {
		t.Errorf("Expected issue2 (%s) in ready work, got %s", issue2.ID, readyWork[0].ID)
	}

	// Verify issue1 is not in ready work
	for _, issue := range readyWork {
		if issue.ID == issue1.ID {
			t.Errorf("Claimed issue %s should not appear in GetReadyWork", issue1.ID)
		}
	}
}

// TestConcurrentClaimDifferentIssues verifies that multiple executors can
// simultaneously claim different issues. This may trigger database locking with
// SQLite, which is expected behavior - the important part is that the system
// handles it gracefully and eventually all claims succeed (vc-1db1)
func TestConcurrentClaimDifferentIssues(t *testing.T) {
	ctx := context.Background()

	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")
	store, err := NewVCStorage(ctx, dbPath)
	if err != nil {
		t.Fatalf("Failed to create test storage: %v", err)
	}
	defer func() { _ = store.Close() }()

	// Register multiple executor instances
	numExecutors := 5
	executorIDs := make([]string, numExecutors)
	for i := 0; i < numExecutors; i++ {
		instanceID := testInstanceID(i)
		executorIDs[i] = instanceID
		instance := &types.ExecutorInstance{
			InstanceID:    instanceID,
			Hostname:      "localhost",
			PID:           12345 + i,
			Version:       "test",
			StartedAt:     time.Now(),
			LastHeartbeat: time.Now(),
			Status:        "running",
		}
		err := store.RegisterInstance(ctx, instance)
		if err != nil {
			t.Fatalf("Failed to register executor %d: %v", i, err)
		}
	}

	// Create one issue per executor
	issueIDs := make([]string, numExecutors)
	for i := 0; i < numExecutors; i++ {
		issue := &types.Issue{
			Title:       "Parallel claim test issue",
			Description: "Each executor gets one",
			Status:      types.StatusOpen,
			Priority:    1,
			IssueType:   types.TypeTask,
		}
		err := store.CreateIssue(ctx, issue, "test")
		if err != nil {
			t.Fatalf("Failed to create issue %d: %v", i, err)
		}
		issueIDs[i] = issue.ID
	}

	// Each executor claims a different issue concurrently with retry logic
	// to handle SQLite database locking (which is expected in concurrent scenarios)
	var wg sync.WaitGroup
	successChan := make(chan int, numExecutors)

	for i := 0; i < numExecutors; i++ {
		wg.Add(1)
		executorID := executorIDs[i]
		issueID := issueIDs[i]
		executorIndex := i
		go func(execID, issID string, idx int) {
			defer wg.Done()
			// Retry up to 5 times to handle database locking
			var lastErr error
			for attempt := 0; attempt < 5; attempt++ {
				err := store.ClaimIssue(ctx, issID, execID)
				if err == nil {
					successChan <- idx
					return
				}
				lastErr = err
				// Brief sleep before retry to reduce contention
				time.Sleep(time.Duration(attempt*10) * time.Millisecond)
			}
			t.Errorf("Executor %d failed to claim issue after retries: %v", idx, lastErr)
		}(executorID, issueID, executorIndex)
	}

	wg.Wait()
	close(successChan)

	// Count successful claims
	successCount := 0
	for range successChan {
		successCount++
	}

	if successCount != numExecutors {
		t.Errorf("Expected %d successful claims, got %d", numExecutors, successCount)
	}

	// Verify all issues are now in_progress and claimed by the correct executor
	for i := 0; i < numExecutors; i++ {
		issue, err := store.GetIssue(ctx, issueIDs[i])
		if err != nil {
			t.Fatalf("Failed to get issue %d: %v", i, err)
		}

		if issue.Status != types.StatusInProgress {
			t.Errorf("Issue %d: expected status 'in_progress', got: %s", i, issue.Status)
		}

		execState, err := store.GetExecutionState(ctx, issueIDs[i])
		if err != nil {
			t.Fatalf("Failed to get execution state for issue %d: %v", i, err)
		}

		if execState.ExecutorInstanceID != executorIDs[i] {
			t.Errorf("Issue %d: expected executor %s, got %s", i, executorIDs[i], execState.ExecutorInstanceID)
		}
	}

	// GetReadyWork should now return 0 issues
	readyWork, err := store.GetReadyWork(ctx, types.WorkFilter{Limit: 100})
	if err != nil {
		t.Fatalf("Failed to get ready work: %v", err)
	}

	if len(readyWork) != 0 {
		t.Errorf("Expected 0 ready issues after all claimed, got %d", len(readyWork))
	}
}

// testInstanceID generates a unique test instance ID
func testInstanceID(i int) string {
	return fmt.Sprintf("test-executor-%d", i)
}
