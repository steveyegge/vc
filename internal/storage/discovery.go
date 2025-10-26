package storage

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// DiscoverDatabase looks for .beads/*.db in the current directory only.
// Returns the absolute path to the database file, or an error if not found.
//
// vc-240: Changed to only check current directory, not parent directories.
// This prevents accidentally using a parent project's database when VC
// is nested inside another project's directory structure.
//
// Example:
//   cd ~/src/vc && vc execute
//   → Finds ~/src/vc/.beads/vc.db (not ~/src/beads/.beads/bd.db)
//
// vc-235: Check VC_DB_PATH environment variable first to allow test isolation.
// If VC_DB_PATH is set, use it directly without discovery.
func DiscoverDatabase() (string, error) {
	// vc-235: Check environment variable first for test isolation
	if dbPath := os.Getenv("VC_DB_PATH"); dbPath != "" {
		// Allow special values like ":memory:" or explicit paths
		return dbPath, nil
	}

	// Start from current working directory
	dir, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("failed to get current directory: %w", err)
	}

	// vc-240: Only check current directory, do not walk up the tree
	return discoverDatabaseInDir(dir)
}

// discoverDatabaseInDir checks for .beads/*.db in the specified directory only.
// Does NOT walk up the directory tree (vc-240).
func discoverDatabaseInDir(dir string) (string, error) {
	// Check for .beads/*.db in the specified directory
	beadsDir := filepath.Join(dir, ".beads")

	// Check if .beads directory exists
	if info, err := os.Stat(beadsDir); err == nil && info.IsDir() {
		// Look for .db files in .beads/
		entries, err := os.ReadDir(beadsDir)
		if err == nil {
			for _, entry := range entries {
				if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".db") {
					dbPath := filepath.Join(beadsDir, entry.Name())
					// Return absolute path
					absPath, err := filepath.Abs(dbPath)
					if err != nil {
						return "", fmt.Errorf("failed to get absolute path: %w", err)
					}
					return absPath, nil
				}
			}
		}
	}

	return "", fmt.Errorf(
		"no .beads/*.db found in %s\n"+
			"  Run 'vc init' to initialize a VC tracker in this directory\n"+
			"  Or use --db flag to specify database path explicitly",
		dir)
}

// discoverDatabaseFromDir walks up from the given directory.
// DEPRECATED: Use discoverDatabaseInDir instead (vc-240).
// Kept for potential future use or reference.
//
//nolint:unused // DEPRECATED - kept for reference
func discoverDatabaseFromDir(startDir string) (string, error) {
	dir := startDir

	// Walk up directory tree
	for {
		// Check for .beads/*.db in current directory
		beadsDir := filepath.Join(dir, ".beads")

		// Check if .beads directory exists
		if info, err := os.Stat(beadsDir); err == nil && info.IsDir() {
			// Look for .db files in .beads/
			entries, err := os.ReadDir(beadsDir)
			if err == nil {
				for _, entry := range entries {
					if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".db") {
						dbPath := filepath.Join(beadsDir, entry.Name())
						// Return absolute path
						absPath, err := filepath.Abs(dbPath)
						if err != nil {
							return "", fmt.Errorf("failed to get absolute path: %w", err)
						}
						return absPath, nil
					}
				}
			}
		}

		// Move to parent directory
		parent := filepath.Dir(dir)
		if parent == dir {
			// Reached filesystem root without finding database
			break
		}
		dir = parent
	}

	return "", fmt.Errorf(
		"no .beads/*.db found in %s or parent directories\n"+
			"  Run 'vc init' to initialize a VC tracker in this project",
		startDir)
}

// GetProjectRoot returns the project root directory for a given database path.
// The project root is the directory containing the .beads/ directory.
//
// Example:
//   dbPath: /home/user/myproject/.beads/project.db
//   returns: /home/user/myproject
func GetProjectRoot(dbPath string) (string, error) {
	absPath, err := filepath.Abs(dbPath)
	if err != nil {
		return "", fmt.Errorf("failed to get absolute path: %w", err)
	}

	// Get directory containing the database file
	dbDir := filepath.Dir(absPath)

	// Check if this is in a .beads directory
	if filepath.Base(dbDir) != ".beads" {
		return "", fmt.Errorf(
			"database must be in a .beads/ directory, got: %s",
			dbPath)
	}

	// Project root is parent of .beads
	projectRoot := filepath.Dir(dbDir)
	return projectRoot, nil
}

// ValidateAlignment ensures database and working directory are in the same project.
// This prevents dangerous scenarios where VC reads issues from one project
// but spawns workers in a different project.
func ValidateAlignment(dbPath, workingDir string) error {
	projectRoot, err := GetProjectRoot(dbPath)
	if err != nil {
		return fmt.Errorf("invalid database path: %w", err)
	}

	absWorkingDir, err := filepath.Abs(workingDir)
	if err != nil {
		return fmt.Errorf("invalid working directory: %w", err)
	}

	// Working directory must be at or below project root
	// This allows running vc from subdirectories
	if !isAtOrBelow(absWorkingDir, projectRoot) {
		return fmt.Errorf(
			"database-working directory mismatch:\n"+
				"  database: %s\n"+
				"  project root: %s\n"+
				"  working directory: %s\n"+
				"\n"+
				"The database and working directory must be in the same project.\n"+
				"Either:\n"+
				"  - cd %s && vc ...\n"+
				"  - Use the correct --db flag for this directory",
			dbPath, projectRoot, absWorkingDir, projectRoot)
	}

	return nil
}

// ValidateDatabaseFreshness checks if the database is in sync with the JSONL source of truth.
// Returns an error if .beads/issues.jsonl is newer than .beads/vc.db, indicating the database
// is stale and needs to be synced with 'bd import .beads/issues.jsonl'.
//
// This prevents bugs like vc-173 where the executor claimed closed issues because the database
// was out of sync with git after pulling updates.
//
// vc-173: Database staleness detection - prevents claiming closed issues when database is stale
func ValidateDatabaseFreshness(dbPath string) error {
	// Get project root to find issues.jsonl
	projectRoot, err := GetProjectRoot(dbPath)
	if err != nil {
		return fmt.Errorf("invalid database path: %w", err)
	}

	jsonlPath := filepath.Join(projectRoot, ".beads", "issues.jsonl")

	// Get modification times for both files
	dbInfo, err := os.Stat(dbPath)
	if err != nil {
		return fmt.Errorf("failed to stat database: %w", err)
	}

	jsonlInfo, err := os.Stat(jsonlPath)
	if err != nil {
		// If issues.jsonl doesn't exist, that's OK - database might be authoritative
		// This can happen in test environments or fresh clones
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("failed to stat issues.jsonl: %w", err)
	}

	// Compare modification times
	// vc-178: For SQLite WAL mode, use the newest timestamp among .db, .db-shm, and .db-wal
	// The main .db file may not be updated while WAL is active
	dbMtime := dbInfo.ModTime()

	// Check for WAL files and use the newest timestamp
	walPath := dbPath + "-wal"
	if walInfo, err := os.Stat(walPath); err == nil {
		if walInfo.ModTime().After(dbMtime) {
			dbMtime = walInfo.ModTime()
		}
	}

	shmPath := dbPath + "-shm"
	if shmInfo, err := os.Stat(shmPath); err == nil {
		if shmInfo.ModTime().After(dbMtime) {
			dbMtime = shmInfo.ModTime()
		}
	}

	jsonlMtime := jsonlInfo.ModTime()

	// vc-178: Add tolerance for filesystem timestamp precision
	// Filesystem timestamps can vary by platform (FAT32: 2s, ext4: 1ns, etc.)
	// Allow up to 1 second difference to avoid false positives
	const stalenessTolerance = 1 * time.Second
	staleness := jsonlMtime.Sub(dbMtime)

	// If JSONL is newer than database by more than tolerance, database is stale
	if staleness > stalenessTolerance {
		return fmt.Errorf(
			"database is out of sync with issues.jsonl:\n"+
				"  database: %s (modified: %s)\n"+
				"  issues.jsonl: %s (modified: %s)\n"+
				"\n"+
				"The database is stale by %v.\n"+
				"This can happen after pulling git updates that modified issues.jsonl.\n"+
				"\n"+
				"To fix this, sync your database with the canonical JSONL:\n"+
				"  bd import .beads/issues.jsonl\n"+
				"\n"+
				"Then run vc again",
			dbPath, dbMtime.Format("2006-01-02 15:04:05"),
			jsonlPath, jsonlMtime.Format("2006-01-02 15:04:05"),
			staleness)
	}

	return nil
}

// isAtOrBelow checks if path is at or below root in the directory tree
func isAtOrBelow(path, root string) bool {
	// Normalize paths
	path = filepath.Clean(path)
	root = filepath.Clean(root)

	// Path must start with root
	return path == root || strings.HasPrefix(path, root+string(filepath.Separator))
}

// InitProject creates a new .beads directory with an empty database.
// Returns the path to the created database.
func InitProject(projectDir, projectName string) (string, error) {
	// Ensure project directory exists
	if _, err := os.Stat(projectDir); os.IsNotExist(err) {
		return "", fmt.Errorf("project directory does not exist: %s", projectDir)
	}

	// Create .beads directory
	beadsDir := filepath.Join(projectDir, ".beads")
	if err := os.MkdirAll(beadsDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create .beads directory: %w", err)
	}

	// Determine database name
	dbName := projectName
	if dbName == "" {
		// Use directory name as default
		dbName = filepath.Base(projectDir)
	}
	if !strings.HasSuffix(dbName, ".db") {
		dbName += ".db"
	}

	dbPath := filepath.Join(beadsDir, dbName)

	// Check if database already exists
	if _, err := os.Stat(dbPath); err == nil {
		return "", fmt.Errorf("database already exists: %s", dbPath)
	}

	// Create empty issues.jsonl file
	jsonlPath := filepath.Join(beadsDir, "issues.jsonl")
	if err := os.WriteFile(jsonlPath, []byte(""), 0644); err != nil {
		return "", fmt.Errorf("failed to create issues.jsonl: %w", err)
	}

	// Database will be created on first connection
	// Return the path that should be used
	return dbPath, nil
}
