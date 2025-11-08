//go:build !race

package executor

// raceEnabled is false when compiled without -race flag (default)
// This file is used when the race build tag is NOT set
