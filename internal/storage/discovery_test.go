package storage

import (
	"os"
	"path/filepath"
	"testing"
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
	defer os.Setenv("VC_DB_PATH", origPath)

	testPath := "/tmp/custom.db"
	os.Setenv("VC_DB_PATH", testPath)

	dbPath, err := DiscoverDatabase()
	if err != nil {
		t.Errorf("Expected success with env var set, got error: %v", err)
	}
	if dbPath != testPath {
		t.Errorf("Expected database path %s, got %s", testPath, dbPath)
	}
}
