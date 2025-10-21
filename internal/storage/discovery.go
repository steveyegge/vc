package storage

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// DiscoverDatabase walks up the directory tree from cwd looking for .beads/*.db
// Returns the absolute path to the database file, or an error if not found.
//
// This implements git-like discovery:
//   cd ~/myproject && vc execute
//   → Finds ~/myproject/.beads/project.db
//   → Sets WorkingDir to ~/myproject
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

	return discoverDatabaseFromDir(dir)
}

// discoverDatabaseFromDir walks up from the given directory
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
