package storage

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestDiscoverDatabaseInDir_CurrentDirOnly verifies that discoverDatabaseInDir
// only checks the specified directory and does NOT walk up the tree (vc-240).
func TestDiscoverDatabaseInDir_CurrentDirOnly(t *testing.T) {
	// Create temporary directory structure:
	// tmpRoot/
	//   parent/
	//     .beads/
	//       parent.db
	//     child/
	//       (no .beads directory)
	tmpRoot := t.TempDir()
	parentDir := filepath.Join(tmpRoot, "parent")
	childDir := filepath.Join(parentDir, "child")

	// Create parent with database
	parentBeadsDir := filepath.Join(parentDir, ".beads")
	if err := os.MkdirAll(parentBeadsDir, 0755); err != nil {
		t.Fatalf("failed to create parent .beads dir: %v", err)
	}
	parentDB := filepath.Join(parentBeadsDir, "parent.db")
	if err := os.WriteFile(parentDB, []byte(""), 0644); err != nil {
		t.Fatalf("failed to create parent database: %v", err)
	}

	// Create child directory (no .beads)
	if err := os.MkdirAll(childDir, 0755); err != nil {
		t.Fatalf("failed to create child dir: %v", err)
	}

	// Test: discoverDatabaseInDir from child should NOT find parent database
	_, err := discoverDatabaseInDir(childDir)
	if err == nil {
		t.Error("Expected error when no database in current dir, but got success")
	}

	// Test: discoverDatabaseInDir from parent should find parent database
	dbPath, err := discoverDatabaseInDir(parentDir)
	if err != nil {
		t.Errorf("Expected to find database in parent dir, got error: %v", err)
	}
	if dbPath != parentDB {
		t.Errorf("Expected database path %s, got %s", parentDB, dbPath)
	}
}

// TestDiscoverDatabaseInDir_MultipleDBs verifies that when multiple .db files
// exist in .beads/, the function returns the first one found.
func TestDiscoverDatabaseInDir_MultipleDBs(t *testing.T) {
	tmpDir := t.TempDir()
	beadsDir := filepath.Join(tmpDir, ".beads")
	if err := os.MkdirAll(beadsDir, 0755); err != nil {
		t.Fatalf("failed to create .beads dir: %v", err)
	}

	// Create multiple database files
	db1 := filepath.Join(beadsDir, "aaa.db")
	db2 := filepath.Join(beadsDir, "zzz.db")
	if err := os.WriteFile(db1, []byte(""), 0644); err != nil {
		t.Fatalf("failed to create db1: %v", err)
	}
	if err := os.WriteFile(db2, []byte(""), 0644); err != nil {
		t.Fatalf("failed to create db2: %v", err)
	}

	// Should find one of them (order depends on filesystem)
	dbPath, err := discoverDatabaseInDir(tmpDir)
	if err != nil {
		t.Errorf("Expected to find database, got error: %v", err)
	}
	if dbPath != db1 && dbPath != db2 {
		t.Errorf("Expected to find one of the databases, got: %s", dbPath)
	}
}

// TestDiscoverDatabaseInDir_NoBeadsDir verifies error when .beads/ doesn't exist
func TestDiscoverDatabaseInDir_NoBeadsDir(t *testing.T) {
	tmpDir := t.TempDir()

	_, err := discoverDatabaseInDir(tmpDir)
	if err == nil {
		t.Error("Expected error when .beads directory doesn't exist, but got success")
	}
}

// TestDiscoverDatabaseInDir_EmptyBeadsDir verifies error when .beads/ exists but has no .db files
func TestDiscoverDatabaseInDir_EmptyBeadsDir(t *testing.T) {
	tmpDir := t.TempDir()
	beadsDir := filepath.Join(tmpDir, ".beads")
	if err := os.MkdirAll(beadsDir, 0755); err != nil {
		t.Fatalf("failed to create .beads dir: %v", err)
	}

	_, err := discoverDatabaseInDir(tmpDir)
	if err == nil {
		t.Error("Expected error when .beads/ is empty, but got success")
	}
}

// TestDiscoverDatabase_WithEnvVar verifies VC_DB_PATH takes precedence (vc-235)
func TestDiscoverDatabase_WithEnvVar(t *testing.T) {
	// Save and restore original env var
	origPath := os.Getenv("VC_DB_PATH")
	defer func() { _ = os.Setenv("VC_DB_PATH", origPath) }()

	testPath := "/tmp/custom.db"
	_ = os.Setenv("VC_DB_PATH", testPath)

	dbPath, err := DiscoverDatabase()
	if err != nil {
		t.Errorf("Expected success with env var set, got error: %v", err)
	}
	if dbPath != testPath {
		t.Errorf("Expected database path %s, got %s", testPath, dbPath)
	}
}

// TestValidateDatabaseFreshness_FreshDatabase verifies no error when database is up to date (vc-173)
func TestValidateDatabaseFreshness_FreshDatabase(t *testing.T) {
	// Create test directory structure
	tmpDir := t.TempDir()
	beadsDir := filepath.Join(tmpDir, ".beads")
	if err := os.MkdirAll(beadsDir, 0755); err != nil {
		t.Fatalf("failed to create .beads dir: %v", err)
	}

	dbPath := filepath.Join(beadsDir, "test.db")
	jsonlPath := filepath.Join(beadsDir, "issues.jsonl")

	// Create database file first (older)
	if err := os.WriteFile(dbPath, []byte("db content"), 0644); err != nil {
		t.Fatalf("failed to create database: %v", err)
	}

	// Wait a bit to ensure different timestamps
	time.Sleep(10 * time.Millisecond)

	// Touch database to make it newer than jsonl (if jsonl exists at all)
	now := time.Now()
	if err := os.Chtimes(dbPath, now, now); err != nil {
		t.Fatalf("failed to update database timestamp: %v", err)
	}

	// Create JSONL file after database (newer would fail, but we touch db above)
	if err := os.WriteFile(jsonlPath, []byte("{}"), 0644); err != nil {
		t.Fatalf("failed to create issues.jsonl: %v", err)
	}

	// Make JSONL older than database
	oldTime := now.Add(-1 * time.Minute)
	if err := os.Chtimes(jsonlPath, oldTime, oldTime); err != nil {
		t.Fatalf("failed to update jsonl timestamp: %v", err)
	}

	// Database is newer than JSONL - should pass
	err := ValidateDatabaseFreshness(dbPath)
	if err != nil {
		t.Errorf("Expected no error for fresh database, got: %v", err)
	}
}

// TestValidateDatabaseFreshness_StaleDatabase verifies error when JSONL is newer (vc-173)
func TestValidateDatabaseFreshness_StaleDatabase(t *testing.T) {
	// Create test directory structure
	tmpDir := t.TempDir()
	beadsDir := filepath.Join(tmpDir, ".beads")
	if err := os.MkdirAll(beadsDir, 0755); err != nil {
		t.Fatalf("failed to create .beads dir: %v", err)
	}

	dbPath := filepath.Join(beadsDir, "test.db")
	jsonlPath := filepath.Join(beadsDir, "issues.jsonl")

	// Create database file first
	if err := os.WriteFile(dbPath, []byte("db content"), 0644); err != nil {
		t.Fatalf("failed to create database: %v", err)
	}

	// Make database old
	oldTime := time.Now().Add(-1 * time.Hour)
	if err := os.Chtimes(dbPath, oldTime, oldTime); err != nil {
		t.Fatalf("failed to update database timestamp: %v", err)
	}

	// Wait to ensure different timestamps
	time.Sleep(10 * time.Millisecond)

	// Create JSONL file after database (simulates git pull updating JSONL)
	if err := os.WriteFile(jsonlPath, []byte("{}"), 0644); err != nil {
		t.Fatalf("failed to create issues.jsonl: %v", err)
	}

	// JSONL is newer than database - should fail
	err := ValidateDatabaseFreshness(dbPath)
	if err == nil {
		t.Error("Expected error for stale database, got nil")
	}

	// Error message should mention staleness
	if err != nil {
		errMsg := err.Error()
		if !contains(errMsg, "out of sync") && !contains(errMsg, "stale") {
			t.Errorf("Expected error message to mention staleness, got: %s", errMsg)
		}
		if !contains(errMsg, "bd import") {
			t.Errorf("Expected error message to suggest 'bd import', got: %s", errMsg)
		}
	}
}

// TestValidateDatabaseFreshness_NoJSONL verifies no error when issues.jsonl doesn't exist (vc-173)
func TestValidateDatabaseFreshness_NoJSONL(t *testing.T) {
	// Create test directory structure
	tmpDir := t.TempDir()
	beadsDir := filepath.Join(tmpDir, ".beads")
	if err := os.MkdirAll(beadsDir, 0755); err != nil {
		t.Fatalf("failed to create .beads dir: %v", err)
	}

	dbPath := filepath.Join(beadsDir, "test.db")

	// Create database file only (no issues.jsonl)
	if err := os.WriteFile(dbPath, []byte("db content"), 0644); err != nil {
		t.Fatalf("failed to create database: %v", err)
	}

	// Should pass - database is authoritative when JSONL doesn't exist
	err := ValidateDatabaseFreshness(dbPath)
	if err != nil {
		t.Errorf("Expected no error when issues.jsonl doesn't exist, got: %v", err)
	}
}

// Helper function to check if string contains substring
func contains(s, substr string) bool {
	return len(s) >= len(substr) && findSubstring(s, substr)
}

func findSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
