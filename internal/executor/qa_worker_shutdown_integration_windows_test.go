//go:build windows
// +build windows

package executor

import (
	"testing"
)

// TestConcurrentQAWorkerAndExecutorShutdown is disabled on Windows
// because the process counting logic relies on Unix-specific tools (pgrep)
func TestConcurrentQAWorkerAndExecutorShutdown(t *testing.T) {
	t.Skip("Process counting tests not supported on Windows (requires pgrep)")
}

// TestQAWorkerShutdownWithSlowGates is disabled on Windows
// because the process counting logic relies on Unix-specific tools (pgrep)
func TestQAWorkerShutdownWithSlowGates(t *testing.T) {
	t.Skip("Process counting tests not supported on Windows (requires pgrep)")
}
