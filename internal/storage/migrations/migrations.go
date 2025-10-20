package migrations

import (
	"database/sql"
	"fmt"
	"sort"
	"time"
)

// Migration represents a single database migration
type Migration struct {
	Version     int
	Description string
	Up          string // SQL to apply the migration
	Down        string // SQL to revert the migration
}

// Manager handles database migrations
type Manager struct {
	migrations []Migration
}

// NewManager creates a new migration manager
func NewManager() *Manager {
	return &Manager{
		migrations: []Migration{},
	}
}

// Register adds a migration to the manager
func (m *Manager) Register(migration Migration) {
	m.migrations = append(m.migrations, migration)
}

// sortMigrations sorts migrations by version
func (m *Manager) sortMigrations() {
	sort.Slice(m.migrations, func(i, j int) bool {
		return m.migrations[i].Version < m.migrations[j].Version
	})
}

// SQLite migration methods

// ApplySQLite applies all pending migrations to a SQLite database
func (m *Manager) ApplySQLite(db *sql.DB) error {
	// Create schema_version table if it doesn't exist
	if err := createSQLiteVersionTable(db); err != nil {
		return fmt.Errorf("failed to create version table: %w", err)
	}

	// Get current version
	currentVersion, err := getSQLiteVersion(db)
	if err != nil {
		return fmt.Errorf("failed to get current version: %w", err)
	}

	// Sort migrations
	m.sortMigrations()

	// Apply pending migrations
	for _, migration := range m.migrations {
		if migration.Version <= currentVersion {
			continue
		}

		if err := applySQLiteMigration(db, migration); err != nil {
			return fmt.Errorf("failed to apply migration %d: %w", migration.Version, err)
		}
	}

	return nil
}

// RollbackSQLite rolls back the last migration from a SQLite database
func (m *Manager) RollbackSQLite(db *sql.DB) error {
	// Get current version
	currentVersion, err := getSQLiteVersion(db)
	if err != nil {
		return fmt.Errorf("failed to get current version: %w", err)
	}

	if currentVersion == 0 {
		return fmt.Errorf("no migrations to rollback")
	}

	// Find the migration to rollback
	m.sortMigrations()
	for _, migration := range m.migrations {
		if migration.Version == currentVersion {
			if err := rollbackSQLiteMigration(db, migration); err != nil {
				return fmt.Errorf("failed to rollback migration %d: %w", migration.Version, err)
			}
			return nil
		}
	}

	return fmt.Errorf("migration %d not found", currentVersion)
}

// SQLite helper functions

func createSQLiteVersionTable(db *sql.DB) error {
	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS schema_version (
			version INTEGER PRIMARY KEY,
			description TEXT NOT NULL,
			applied_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		)
	`)
	return err
}

func getSQLiteVersion(db *sql.DB) (int, error) {
	var version int
	err := db.QueryRow("SELECT COALESCE(MAX(version), 0) FROM schema_version").Scan(&version)
	if err != nil {
		return 0, err
	}
	return version, nil
}

func applySQLiteMigration(db *sql.DB, migration Migration) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	// Execute migration SQL
	if _, err := tx.Exec(migration.Up); err != nil {
		return fmt.Errorf("failed to execute migration SQL: %w", err)
	}

	// Record migration
	if _, err := tx.Exec(
		"INSERT INTO schema_version (version, description, applied_at) VALUES (?, ?, ?)",
		migration.Version, migration.Description, time.Now(),
	); err != nil {
		return fmt.Errorf("failed to record migration: %w", err)
	}

	return tx.Commit()
}

func rollbackSQLiteMigration(db *sql.DB, migration Migration) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	// Execute rollback SQL
	if _, err := tx.Exec(migration.Down); err != nil {
		return fmt.Errorf("failed to execute rollback SQL: %w", err)
	}

	// Remove migration record
	if _, err := tx.Exec(
		"DELETE FROM schema_version WHERE version = ?",
		migration.Version,
	); err != nil {
		return fmt.Errorf("failed to remove migration record: %w", err)
	}

	return tx.Commit()
}
