package types

// ExecutionMode defines how VC executes work
//
// VC supports two execution modes:
//
// ModeExecutor (default): Full autonomous loop
//   - Polls beads for ready issues
//   - Creates sandbox worktrees for isolation
//   - Tracks executor instances
//   - Continuous operation until stopped
//
// ModePolecat: Single-task execution for Gastown integration
//   - Accepts task from CLI args or stdin
//   - Uses polecat's existing clone/branch
//   - Single execution, then exits
//   - JSON output to stdout
//
// See docs/design/GASTOWN_INTEGRATION.md for full specification.
type ExecutionMode string

const (
	// ModeExecutor is the default mode - full polling loop with issue claiming
	ModeExecutor ExecutionMode = "executor"

	// ModePolecat is single-task execution mode for Gastown integration
	// In this mode, VC:
	// - Accepts a task from --task, --issue, or --stdin
	// - Executes once with quality gates
	// - Outputs JSON result to stdout
	// - Exits after execution
	ModePolecat ExecutionMode = "polecat"
)

// IsValid checks if the execution mode value is valid
func (m ExecutionMode) IsValid() bool {
	switch m {
	case ModeExecutor, ModePolecat:
		return true
	}
	return false
}

// IsPolecat returns true if this is polecat mode
func (m ExecutionMode) IsPolecat() bool {
	return m == ModePolecat
}

// IsExecutor returns true if this is executor mode
func (m ExecutionMode) IsExecutor() bool {
	return m == ModeExecutor
}
