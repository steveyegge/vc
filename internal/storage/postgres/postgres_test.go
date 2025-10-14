package postgres

import (
	"context"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/steveyegge/vc/internal/types"
)

// getTestConfig returns a config for testing based on environment variables
func getTestConfig() *Config {
	cfg := DefaultConfig()

	// Allow overriding via environment variables
	if host := os.Getenv("VC_TEST_PG_HOST"); host != "" {
		cfg.Host = host
	}
	if db := os.Getenv("VC_TEST_PG_DATABASE"); db != "" {
		cfg.Database = db
	}
	if user := os.Getenv("VC_TEST_PG_USER"); user != "" {
		cfg.User = user
	}
	if pass := os.Getenv("VC_TEST_PG_PASSWORD"); pass != "" {
		cfg.Password = pass
	}

	return cfg
}

// setupTestStorage creates a test storage and cleans up the database
func setupTestStorage(t *testing.T) *PostgresStorage {
	ctx := context.Background()

	cfg := getTestConfig()
	storage, err := New(ctx, cfg)
	if err != nil {
		t.Skipf("Skipping PostgreSQL test (database not available): %v", err)
	}

	// Clean up all tables
	_, err = storage.pool.Exec(ctx, `
		TRUNCATE TABLE issue_execution_state, executor_instances, events, labels, dependencies, issues CASCADE;
		ALTER SEQUENCE IF EXISTS issue_id_seq RESTART WITH 1;
	`)
	if err != nil {
		t.Fatalf("Failed to clean up test database: %v", err)
	}

	return storage
}

// TestConcurrentIDGeneration verifies that multiple goroutines can create issues
// concurrently without ID collisions
func TestConcurrentIDGeneration(t *testing.T) {
	storage := setupTestStorage(t)
	defer storage.Close()

	ctx := context.Background()
	numGoroutines := 10
	issuesPerGoroutine := 10

	var wg sync.WaitGroup
	errChan := make(chan error, numGoroutines*issuesPerGoroutine)
	idChan := make(chan string, numGoroutines*issuesPerGoroutine)

	// Launch multiple goroutines creating issues concurrently
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(goroutineID int) {
			defer wg.Done()

			for j := 0; j < issuesPerGoroutine; j++ {
				issue := &types.Issue{
					Title:      fmt.Sprintf("Test Issue G%d-I%d", goroutineID, j),
					Status:     types.StatusOpen,
					Priority:   2,
					IssueType:  types.TypeTask,
				}

				err := storage.CreateIssue(ctx, issue, fmt.Sprintf("goroutine-%d", goroutineID))
				if err != nil {
					errChan <- fmt.Errorf("goroutine %d, issue %d: %w", goroutineID, j, err)
					return
				}

				idChan <- issue.ID
			}
		}(i)
	}

	wg.Wait()
	close(errChan)
	close(idChan)

	// Check for errors
	for err := range errChan {
		t.Errorf("Error during concurrent creation: %v", err)
	}

	// Collect all IDs and verify uniqueness
	seenIDs := make(map[string]bool)
	for id := range idChan {
		if seenIDs[id] {
			t.Errorf("Duplicate ID generated: %s", id)
		}
		seenIDs[id] = true
	}

	expectedCount := numGoroutines * issuesPerGoroutine
	if len(seenIDs) != expectedCount {
		t.Errorf("Expected %d unique IDs, got %d", expectedCount, len(seenIDs))
	}

	// Verify all IDs have the correct prefix
	for id := range seenIDs {
		if len(id) < 4 || id[:3] != "vc-" {
			t.Errorf("ID has incorrect prefix: %s (expected 'vc-')", id)
		}
	}

	t.Logf("Successfully created %d issues with unique IDs across %d concurrent goroutines", len(seenIDs), numGoroutines)
}

// TestIDGenerationPerformance measures the time taken to generate IDs
func TestIDGenerationPerformance(t *testing.T) {
	storage := setupTestStorage(t)
	defer storage.Close()

	ctx := context.Background()
	numIssues := 100

	start := time.Now()

	for i := 0; i < numIssues; i++ {
		issue := &types.Issue{
			Title:     fmt.Sprintf("Performance Test Issue %d", i),
			Status:    types.StatusOpen,
			Priority:  2,
			IssueType: types.TypeTask,
		}

		err := storage.CreateIssue(ctx, issue, "test")
		if err != nil {
			t.Fatalf("Failed to create issue: %v", err)
		}
	}

	elapsed := time.Since(start)
	avgTime := elapsed / time.Duration(numIssues)

	t.Logf("Created %d issues in %v (avg: %v per issue)", numIssues, elapsed, avgTime)

	// Acceptance criteria: < 5ms per ID
	maxTime := 5 * time.Millisecond
	if avgTime > maxTime {
		t.Errorf("Average ID generation time %v exceeds threshold of %v", avgTime, maxTime)
	}
}

// TestSequenceStartsAtOne verifies that the sequence starts at 1
func TestSequenceStartsAtOne(t *testing.T) {
	storage := setupTestStorage(t)
	defer storage.Close()

	ctx := context.Background()

	issue := &types.Issue{
		Title:     "First Issue",
		Status:    types.StatusOpen,
		Priority:  2,
		IssueType: types.TypeTask,
	}

	err := storage.CreateIssue(ctx, issue, "test")
	if err != nil {
		t.Fatalf("Failed to create issue: %v", err)
	}

	if issue.ID != "vc-1" {
		t.Errorf("Expected first issue ID to be 'vc-1', got '%s'", issue.ID)
	}
}

// TestCustomIDNotOverwritten verifies that pre-set IDs are not overwritten
func TestCustomIDNotOverwritten(t *testing.T) {
	storage := setupTestStorage(t)
	defer storage.Close()

	ctx := context.Background()

	issue := &types.Issue{
		ID:        "custom-123",
		Title:     "Custom ID Issue",
		Status:    types.StatusOpen,
		Priority:  2,
		IssueType: types.TypeTask,
	}

	err := storage.CreateIssue(ctx, issue, "test")
	if err != nil {
		t.Fatalf("Failed to create issue with custom ID: %v", err)
	}

	if issue.ID != "custom-123" {
		t.Errorf("Custom ID was overwritten: expected 'custom-123', got '%s'", issue.ID)
	}
}
