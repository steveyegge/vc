package storage

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

// ExclusiveLock represents the lock file format for VC to claim exclusive management
// of the beads database. When this lock is present, bd daemon will skip the database.
// This follows the protocol defined in VC_DAEMON_EXCLUSION_PROTOCOL.md
type ExclusiveLock struct {
	Holder    string    `json:"holder"`
	PID       int       `json:"pid"`
	Hostname  string    `json:"hostname"`
	StartedAt time.Time `json:"started_at"`
	Version   string    `json:"version"`
}

// AcquireExclusiveLock creates an exclusive lock file in the .beads directory.
// This prevents bd daemon from managing the database while VC is running.
// Returns the lock file path for cleanup on shutdown.
func AcquireExclusiveLock(dbPath, version string) (lockPath string, err error) {
	// Get project root to find .beads directory
	projectRoot, err := GetProjectRoot(dbPath)
	if err != nil {
		return "", fmt.Errorf("invalid database path: %w", err)
	}

	lockPath = filepath.Join(projectRoot, ".beads", ".exclusive-lock")

	// Check for existing lock
	if data, err := os.ReadFile(lockPath); err == nil {
		var existingLock ExclusiveLock
		if json.Unmarshal(data, &existingLock) == nil {
			// Check if stale (process no longer exists)
			if isProcessAlive(existingLock.PID, existingLock.Hostname) {
				return "", fmt.Errorf("another VC executor is already running (PID %d on %s, started %s)",
					existingLock.PID, existingLock.Hostname, existingLock.StartedAt.Format(time.RFC3339))
			}
			// Stale lock - will overwrite
		}
	}

	// Create lock
	hostname, err := os.Hostname()
	if err != nil {
		return "", fmt.Errorf("failed to get hostname: %w", err)
	}

	lock := ExclusiveLock{
		Holder:    "vc-executor",
		PID:       os.Getpid(),
		Hostname:  hostname,
		StartedAt: time.Now(),
		Version:   version,
	}

	data, err := json.MarshalIndent(lock, "", "  ")
	if err != nil {
		return "", fmt.Errorf("failed to marshal lock: %w", err)
	}

	if err := os.WriteFile(lockPath, data, 0644); err != nil {
		return "", fmt.Errorf("failed to create exclusive lock: %w", err)
	}

	return lockPath, nil
}

// ReleaseExclusiveLock removes the exclusive lock file.
// Should be called on executor shutdown (use defer).
func ReleaseExclusiveLock(lockPath string) error {
	if lockPath == "" {
		return nil
	}

	if err := os.Remove(lockPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove exclusive lock: %w", err)
	}

	return nil
}

// isProcessAlive checks if a process with the given PID exists on the given hostname.
// Returns true if the process is alive, false otherwise.
// This is a simplified version of the Beads implementation.
func isProcessAlive(pid int, hostname string) bool {
	// Check if this is localhost
	currentHost, err := os.Hostname()
	if err != nil {
		// Can't check hostname, assume remote/alive
		return true
	}

	// Case-insensitive hostname comparison (following Beads implementation)
	if !strings.EqualFold(hostname, currentHost) {
		// Remote host - can't check, assume alive
		return true
	}

	// Check if PID exists on localhost (Unix: kill -0)
	process, err := os.FindProcess(pid)
	if err != nil {
		return false
	}

	// Send signal 0 to check if process exists
	err = process.Signal(syscall.Signal(0))
	if err == nil {
		return true // Process exists
	}

	// Check for EPERM (process exists but we don't have permission)
	// This is a fail-safe: if we can't verify, assume alive
	if err == syscall.EPERM {
		return true
	}

	return false // Process doesn't exist
}
