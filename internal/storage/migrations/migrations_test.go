package migrations

import (
	"database/sql"
	"testing"

	_ "github.com/mattn/go-sqlite3"
)

// Example migration for testing
var exampleMigration = Migration{
	Version:     1,
	Description: "Add example test table",
	Up: `
		CREATE TABLE IF NOT EXISTS test_table (
			id INTEGER PRIMARY KEY,
			name TEXT NOT NULL
		)
	`,
	Down: `
		DROP TABLE IF EXISTS test_table
	`,
}

func TestSQLiteMigrations(t *testing.T) {
	// Create in-memory database
	db, err := sql.Open("sqlite3", ":memory:?_foreign_keys=ON")
	if err != nil {
		t.Fatalf("failed to open database: %v", err)
	}
	defer func() { _ = db.Close() }()

	// Create migration manager
	manager := NewManager()
	manager.Register(exampleMigration)

	// Apply migrations
	if err := manager.ApplySQLite(db); err != nil {
		t.Fatalf("failed to apply migrations: %v", err)
	}

	// Verify version table exists
	var version int
	err = db.QueryRow("SELECT version FROM schema_version WHERE version = 1").Scan(&version)
	if err != nil {
		t.Fatalf("version record not found: %v", err)
	}
	if version != 1 {
		t.Errorf("expected version 1, got %d", version)
	}

	// Verify test table exists
	_, err = db.Exec("INSERT INTO test_table (id, name) VALUES (1, 'test')")
	if err != nil {
		t.Fatalf("test table not created: %v", err)
	}

	// Test rollback
	if err := manager.RollbackSQLite(db); err != nil {
		t.Fatalf("failed to rollback migration: %v", err)
	}

	// Verify version table is empty
	err = db.QueryRow("SELECT version FROM schema_version WHERE version = 1").Scan(&version)
	if err != sql.ErrNoRows {
		t.Errorf("expected version record to be removed")
	}

	// Verify test table is dropped
	_, err = db.Exec("INSERT INTO test_table (id, name) VALUES (1, 'test')")
	if err == nil {
		t.Error("test table should have been dropped")
	}
}

func TestMigrationOrdering(t *testing.T) {
	manager := NewManager()

	// Register migrations out of order
	manager.Register(Migration{Version: 3, Description: "Third"})
	manager.Register(Migration{Version: 1, Description: "First"})
	manager.Register(Migration{Version: 2, Description: "Second"})

	// Sort migrations
	manager.sortMigrations()

	// Verify order
	if len(manager.migrations) != 3 {
		t.Fatalf("expected 3 migrations, got %d", len(manager.migrations))
	}
	if manager.migrations[0].Version != 1 {
		t.Errorf("expected first migration version 1, got %d", manager.migrations[0].Version)
	}
	if manager.migrations[1].Version != 2 {
		t.Errorf("expected second migration version 2, got %d", manager.migrations[1].Version)
	}
	if manager.migrations[2].Version != 3 {
		t.Errorf("expected third migration version 3, got %d", manager.migrations[2].Version)
	}
}
