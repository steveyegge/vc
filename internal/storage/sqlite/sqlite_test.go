package sqlite

import (
	"context"
	"database/sql"
	"os"
	"testing"

	_ "github.com/mattn/go-sqlite3"
)

// TestGetNextIDWithEmptyTable verifies getNextID returns 1 for empty table
func TestGetNextIDWithEmptyTable(t *testing.T) {
	// Create temp database
	tmpfile, err := os.CreateTemp("", "test-*.db")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer os.Remove(tmpfile.Name())
	tmpfile.Close()

	db, err := sql.Open("sqlite3", tmpfile.Name())
	if err != nil {
		t.Fatalf("Failed to open database: %v", err)
	}
	defer db.Close()

	// Create issues table
	_, err = db.Exec(`
		CREATE TABLE issues (
			id TEXT PRIMARY KEY,
			title TEXT NOT NULL,
			description TEXT,
			design TEXT,
			acceptance_criteria TEXT,
			notes TEXT,
			status TEXT NOT NULL,
			priority INTEGER NOT NULL,
			issue_type TEXT NOT NULL,
			assignee TEXT,
			estimated_minutes INTEGER,
			created_at DATETIME NOT NULL,
			updated_at DATETIME NOT NULL,
			closed_at DATETIME,
			approved_at DATETIME,
			approved_by TEXT
		)
	`)
	if err != nil {
		t.Fatalf("Failed to create table: %v", err)
	}

	// Test with empty table
	nextID, err := getNextID(db)
	if err != nil {
		t.Errorf("getNextID failed: %v", err)
	}
	if nextID != 1 {
		t.Errorf("Expected nextID=1 for empty table, got %d", nextID)
	}
}

// TestGetNextIDWithExistingIssues verifies getNextID increments correctly
func TestGetNextIDWithExistingIssues(t *testing.T) {
	// Create temp database
	tmpfile, err := os.CreateTemp("", "test-*.db")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer os.Remove(tmpfile.Name())
	tmpfile.Close()

	db, err := sql.Open("sqlite3", tmpfile.Name())
	if err != nil {
		t.Fatalf("Failed to open database: %v", err)
	}
	defer db.Close()

	// Create issues table
	_, err = db.Exec(`
		CREATE TABLE issues (
			id TEXT PRIMARY KEY,
			title TEXT NOT NULL,
			description TEXT,
			design TEXT,
			acceptance_criteria TEXT,
			notes TEXT,
			status TEXT NOT NULL,
			priority INTEGER NOT NULL,
			issue_type TEXT NOT NULL,
			assignee TEXT,
			estimated_minutes INTEGER,
			created_at DATETIME NOT NULL,
			updated_at DATETIME NOT NULL,
			closed_at DATETIME,
			approved_at DATETIME,
			approved_by TEXT
		)
	`)
	if err != nil {
		t.Fatalf("Failed to create table: %v", err)
	}

	// Insert test issues
	testCases := []struct {
		id       string
		expected int
	}{
		{"vc-5", 6},
		{"vc-42", 43},
		{"bd-99", 100},
	}

	for _, tc := range testCases {
		// Clear table
		_, err = db.Exec("DELETE FROM issues")
		if err != nil {
			t.Fatalf("Failed to clear table: %v", err)
		}

		// Insert issue
		_, err = db.Exec(`
			INSERT INTO issues (id, title, status, priority, issue_type, created_at, updated_at)
			VALUES (?, 'Test', 'open', 1, 'task', datetime('now'), datetime('now'))
		`, tc.id)
		if err != nil {
			t.Fatalf("Failed to insert issue: %v", err)
		}

		// Test getNextID
		nextID, err := getNextID(db)
		if err != nil {
			t.Errorf("getNextID failed for ID %s: %v", tc.id, err)
		}
		if nextID != tc.expected {
			t.Errorf("For ID %s, expected nextID=%d, got %d", tc.id, tc.expected, nextID)
		}
	}
}

// TestGetNextIDWithInvalidFormat verifies error handling for malformed IDs
func TestGetNextIDWithInvalidFormat(t *testing.T) {
	// Create temp database
	tmpfile, err := os.CreateTemp("", "test-*.db")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer os.Remove(tmpfile.Name())
	tmpfile.Close()

	db, err := sql.Open("sqlite3", tmpfile.Name())
	if err != nil {
		t.Fatalf("Failed to open database: %v", err)
	}
	defer db.Close()

	// Create issues table
	_, err = db.Exec(`
		CREATE TABLE issues (
			id TEXT PRIMARY KEY,
			title TEXT NOT NULL,
			description TEXT,
			design TEXT,
			acceptance_criteria TEXT,
			notes TEXT,
			status TEXT NOT NULL,
			priority INTEGER NOT NULL,
			issue_type TEXT NOT NULL,
			assignee TEXT,
			estimated_minutes INTEGER,
			created_at DATETIME NOT NULL,
			updated_at DATETIME NOT NULL,
			closed_at DATETIME,
			approved_at DATETIME,
			approved_by TEXT
		)
	`)
	if err != nil {
		t.Fatalf("Failed to create table: %v", err)
	}

	testCases := []struct {
		name     string
		id       string
		wantErr  bool
		errMatch string
	}{
		{
			name:     "no hyphen",
			id:       "bd123",
			wantErr:  true,
			errMatch: "invalid issue ID format",
		},
		{
			name:     "multiple hyphens",
			id:       "vc-123-extra",
			wantErr:  true,
			errMatch: "invalid issue ID format",
		},
		{
			name:     "non-numeric suffix",
			id:       "vc-abc",
			wantErr:  true,
			errMatch: "failed to parse issue number",
		},
		{
			name:     "empty suffix",
			id:       "vc-",
			wantErr:  true,
			errMatch: "failed to parse issue number",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Clear and insert test issue
			_, err = db.Exec("DELETE FROM issues")
			if err != nil {
				t.Fatalf("Failed to clear table: %v", err)
			}

			_, err = db.Exec(`
				INSERT INTO issues (id, title, status, priority, issue_type, created_at, updated_at)
				VALUES (?, 'Test', 'open', 1, 'task', datetime('now'), datetime('now'))
			`, tc.id)
			if err != nil {
				t.Fatalf("Failed to insert issue: %v", err)
			}

			// Test getNextID
			_, err = getNextID(db)
			if tc.wantErr {
				if err == nil {
					t.Errorf("Expected error containing %q, got nil", tc.errMatch)
				} else if tc.errMatch != "" && !contains(err.Error(), tc.errMatch) {
					t.Errorf("Expected error containing %q, got %q", tc.errMatch, err.Error())
				}
			} else {
				if err != nil {
					t.Errorf("Expected no error, got %v", err)
				}
			}
		})
	}
}

// TestGetNextIDWithDatabaseError verifies database errors are propagated
func TestGetNextIDWithDatabaseError(t *testing.T) {
	// Open database that will fail queries
	tmpfile, err := os.CreateTemp("", "test-*.db")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	tmpfile.Close()
	os.Remove(tmpfile.Name()) // Remove file so it doesn't exist

	db, err := sql.Open("sqlite3", tmpfile.Name())
	if err != nil {
		t.Fatalf("Failed to open database: %v", err)
	}
	defer db.Close()

	// Attempt getNextID on non-existent table - should propagate error
	_, err = getNextID(db)
	if err == nil {
		t.Error("Expected error when querying non-existent table, got nil")
	}
	if !contains(err.Error(), "failed to query max issue ID") {
		t.Errorf("Expected error to mention 'failed to query max issue ID', got: %v", err)
	}
}

// TestNewWithCorruptDatabase verifies New() handles getNextID errors
func TestNewWithCorruptDatabase(t *testing.T) {
	// Create temp database
	tmpfile, err := os.CreateTemp("", "test-*.db")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer os.Remove(tmpfile.Name())
	tmpfile.Close()

	// Create database with schema
	db, err := sql.Open("sqlite3", tmpfile.Name())
	if err != nil {
		t.Fatalf("Failed to open database: %v", err)
	}

	// Create table with valid schema
	_, err = db.Exec(`
		CREATE TABLE issues (
			id TEXT PRIMARY KEY,
			title TEXT NOT NULL,
			description TEXT,
			design TEXT,
			acceptance_criteria TEXT,
			notes TEXT,
			status TEXT NOT NULL,
			priority INTEGER NOT NULL,
			issue_type TEXT NOT NULL,
			assignee TEXT,
			estimated_minutes INTEGER,
			created_at DATETIME NOT NULL,
			updated_at DATETIME NOT NULL,
			closed_at DATETIME,
			approved_at DATETIME,
			approved_by TEXT
		)
	`)
	if err != nil {
		t.Fatalf("Failed to create table: %v", err)
	}

	// Insert issue with malformed ID
	_, err = db.Exec(`
		INSERT INTO issues (id, title, status, priority, issue_type, created_at, updated_at)
		VALUES ('malformed-id-without-number', 'Test', 'open', 1, 'task', datetime('now'), datetime('now'))
	`)
	if err != nil {
		t.Fatalf("Failed to insert malformed issue: %v", err)
	}
	db.Close()

	// Now try to open with New() - should fail during getNextID
	_, err = New(tmpfile.Name())
	if err == nil {
		t.Error("Expected New() to fail with malformed issue ID in database")
	}
	// Could be either format error or parse error depending on the exact format
	if !contains(err.Error(), "invalid issue ID format") && !contains(err.Error(), "failed to parse issue number") {
		t.Errorf("Expected error about invalid ID format or parsing, got: %v", err)
	}
}

// TestForeignKeysEnabled verifies that foreign keys are enabled (vc-116)
func TestForeignKeysEnabled(t *testing.T) {
	// Create temp database
	tmpfile, err := os.CreateTemp("", "test-*.db")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer os.Remove(tmpfile.Name())
	tmpfile.Close()

	// Create new storage (which should enable foreign keys)
	storage, err := New(tmpfile.Name())
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer storage.Close()

	// Check that foreign keys are enabled
	var fkEnabled int
	err = storage.db.QueryRow("PRAGMA foreign_keys").Scan(&fkEnabled)
	if err != nil {
		t.Fatalf("Failed to check foreign keys: %v", err)
	}

	if fkEnabled != 1 {
		t.Errorf("Expected foreign keys to be enabled (1), got %d", fkEnabled)
	}
}

// TestCascadeDeleteWorks verifies ON DELETE CASCADE works (vc-116)
func TestCascadeDeleteWorks(t *testing.T) {
	// Create temp database
	tmpfile, err := os.CreateTemp("", "test-*.db")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer os.Remove(tmpfile.Name())
	tmpfile.Close()

	// Create new storage
	storage, err := New(tmpfile.Name())
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer storage.Close()

	// Insert a parent issue
	_, err = storage.db.Exec(`
		INSERT INTO issues (id, title, status, priority, issue_type, created_at, updated_at)
		VALUES ('vc-100', 'Parent Issue', 'open', 1, 'task', datetime('now'), datetime('now'))
	`)
	if err != nil {
		t.Fatalf("Failed to insert parent issue: %v", err)
	}

	// Insert child records that should cascade delete
	_, err = storage.db.Exec(`
		INSERT INTO events (issue_id, event_type, actor, comment)
		VALUES ('vc-100', 'created', 'test', 'test event')
	`)
	if err != nil {
		t.Fatalf("Failed to insert event: %v", err)
	}

	_, err = storage.db.Exec(`
		INSERT INTO labels (issue_id, label)
		VALUES ('vc-100', 'test-label')
	`)
	if err != nil {
		t.Fatalf("Failed to insert label: %v", err)
	}

	// Verify records exist
	var eventCount, labelCount int
	storage.db.QueryRow("SELECT COUNT(*) FROM events WHERE issue_id = 'vc-100'").Scan(&eventCount)
	storage.db.QueryRow("SELECT COUNT(*) FROM labels WHERE issue_id = 'vc-100'").Scan(&labelCount)

	if eventCount != 1 {
		t.Errorf("Expected 1 event, got %d", eventCount)
	}
	if labelCount != 1 {
		t.Errorf("Expected 1 label, got %d", labelCount)
	}

	// Delete the parent issue
	_, err = storage.db.Exec("DELETE FROM issues WHERE id = 'vc-100'")
	if err != nil {
		t.Fatalf("Failed to delete issue: %v", err)
	}

	// Verify child records were cascade deleted
	storage.db.QueryRow("SELECT COUNT(*) FROM events WHERE issue_id = 'vc-100'").Scan(&eventCount)
	storage.db.QueryRow("SELECT COUNT(*) FROM labels WHERE issue_id = 'vc-100'").Scan(&labelCount)

	if eventCount != 0 {
		t.Errorf("Expected events to be cascade deleted, but found %d", eventCount)
	}
	if labelCount != 0 {
		t.Errorf("Expected labels to be cascade deleted, but found %d", labelCount)
	}
}

// TestGetStatisticsWithEmptyDatabase verifies GetStatistics handles empty database (vc-112)
// Bug: SUM() returns NULL when table is empty, causing "converting NULL to int" error
// Fix: Use COALESCE(SUM(...), 0) to convert NULL to 0
func TestGetStatisticsWithEmptyDatabase(t *testing.T) {
	// Create temp database
	tmpfile, err := os.CreateTemp("", "test-*.db")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer os.Remove(tmpfile.Name())
	tmpfile.Close()

	// Create new storage (will initialize schema with empty tables)
	storage, err := New(tmpfile.Name())
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer storage.Close()

	// Get statistics on empty database - should not error
	ctx := context.Background()
	stats, err := storage.GetStatistics(ctx)
	if err != nil {
		t.Fatalf("GetStatistics failed on empty database: %v", err)
	}

	// Verify all counts are zero
	if stats.TotalIssues != 0 {
		t.Errorf("Expected TotalIssues=0, got %d", stats.TotalIssues)
	}
	if stats.OpenIssues != 0 {
		t.Errorf("Expected OpenIssues=0, got %d", stats.OpenIssues)
	}
	if stats.InProgressIssues != 0 {
		t.Errorf("Expected InProgressIssues=0, got %d", stats.InProgressIssues)
	}
	if stats.ClosedIssues != 0 {
		t.Errorf("Expected ClosedIssues=0, got %d", stats.ClosedIssues)
	}
	if stats.BlockedIssues != 0 {
		t.Errorf("Expected BlockedIssues=0, got %d", stats.BlockedIssues)
	}
	if stats.ReadyIssues != 0 {
		t.Errorf("Expected ReadyIssues=0, got %d", stats.ReadyIssues)
	}
	if stats.AverageLeadTime != 0 {
		t.Errorf("Expected AverageLeadTime=0, got %f", stats.AverageLeadTime)
	}
}

// contains checks if a string contains a substring
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 ||
		(len(s) > 0 && len(substr) > 0 && containsHelper(s, substr)))
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
