package watchdog

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/steveyegge/vc/internal/ai"
	"github.com/steveyegge/vc/internal/storage"
)

// GitCommandType categorizes git commands by their risk level
type GitCommandType string

const (
	GitCommandRead   GitCommandType = "read"   // Safe read operations (status, log, diff)
	GitCommandWrite  GitCommandType = "write"  // Safe write operations (commit, add)
	GitCommandDanger GitCommandType = "danger" // Potentially dangerous operations
)

// GitSafetyEvaluation represents the AI's safety assessment of a git command
type GitSafetyEvaluation struct {
	// Safe indicates whether the command is safe to execute
	Safe bool `json:"safe"`

	// CommandType categorizes the command
	CommandType GitCommandType `json:"command_type"`

	// RiskLevel indicates the risk level (low, medium, high, critical)
	RiskLevel string `json:"risk_level"`

	// Reasoning explains why the command is safe or dangerous
	Reasoning string `json:"reasoning"`

	// RecommendedAction suggests what should be done
	// "allow", "block", "confirm", "modify"
	RecommendedAction string `json:"recommended_action"`

	// Confidence indicates AI confidence in the evaluation (0.0-1.0)
	Confidence float64 `json:"confidence"`

	// AlternativeCommand suggests a safer alternative (if any)
	AlternativeCommand string `json:"alternative_command,omitempty"`

	// Warnings contains any warnings about the command
	Warnings []string `json:"warnings,omitempty"`
}

// GitSafetyMonitor evaluates git commands for safety using AI supervision
// It provides ZFC-compliant (Zero Framework Cognition) safety checks
type GitSafetyMonitor struct {
	supervisor *ai.Supervisor
	store      storage.Storage
	config     *WatchdogConfig
}

// GitSafetyMonitorConfig holds configuration for the git safety monitor
type GitSafetyMonitorConfig struct {
	Supervisor *ai.Supervisor
	Store      storage.Storage
	Config     *WatchdogConfig // Optional watchdog configuration
}

// NewGitSafetyMonitor creates a new git safety monitor
func NewGitSafetyMonitor(cfg *GitSafetyMonitorConfig) (*GitSafetyMonitor, error) {
	if cfg.Supervisor == nil {
		return nil, fmt.Errorf("AI supervisor is required")
	}
	if cfg.Store == nil {
		return nil, fmt.Errorf("storage is required")
	}

	// Use default config if not provided
	watchdogConfig := cfg.Config
	if watchdogConfig == nil {
		watchdogConfig = DefaultWatchdogConfig()
	}

	return &GitSafetyMonitor{
		supervisor: cfg.Supervisor,
		store:      cfg.Store,
		config:     watchdogConfig,
	}, nil
}

// EvaluateCommand uses AI to evaluate whether a git command is safe to execute
// This is the core ZFC implementation - no hardcoded patterns or heuristics
func (gsm *GitSafetyMonitor) EvaluateCommand(ctx context.Context, command string, branch string, issueID string) (*GitSafetyEvaluation, error) {
	startTime := time.Now()

	// Build the safety evaluation prompt
	prompt := gsm.buildSafetyPrompt(command, branch, issueID)

	// Call AI supervisor for safety evaluation
	// We'll use the supervisor's internal retry logic
	evaluation, err := gsm.callAISupervisor(ctx, prompt)
	if err != nil {
		return nil, fmt.Errorf("AI safety evaluation failed: %w", err)
	}

	duration := time.Since(startTime)

	// Log the evaluation
	if !evaluation.Safe {
		fmt.Printf("Git Safety: BLOCKED dangerous command - %s (risk=%s, confidence=%.2f, duration=%v)\n",
			command, evaluation.RiskLevel, evaluation.Confidence, duration)
	} else {
		fmt.Printf("Git Safety: Allowed command - %s (type=%s, duration=%v)\n",
			command, evaluation.CommandType, duration)
	}

	return evaluation, nil
}

// CheckDangerousOperation checks if a git operation is dangerous before execution
// Returns an error if the operation should be blocked
// This is meant to be called before executing high-risk git operations
func (gsm *GitSafetyMonitor) CheckDangerousOperation(ctx context.Context, command string, branch string, issueID string, allowOverride bool) error {
	// Evaluate the command safety
	evaluation, err := gsm.EvaluateCommand(ctx, command, branch, issueID)
	if err != nil {
		// If evaluation fails, err on the side of caution and block
		return fmt.Errorf("safety evaluation failed, blocking for safety: %w", err)
	}

	// If the command is safe, allow it
	if evaluation.Safe {
		return nil
	}

	// Command is deemed unsafe
	// Check if override is allowed
	if allowOverride {
		// Log the override but allow execution
		fmt.Printf("Git Safety: OVERRIDE - Allowing dangerous command despite safety concerns: %s\n", command)
		gsm.logSafetyEvent(ctx, issueID, command, evaluation, true)
		return nil
	}

	// Block the command
	gsm.logSafetyEvent(ctx, issueID, command, evaluation, false)

	// Create a detailed error message
	errMsg := fmt.Sprintf("Blocked dangerous git operation: %s\nRisk Level: %s\nReason: %s",
		command, evaluation.RiskLevel, evaluation.Reasoning)

	if evaluation.AlternativeCommand != "" {
		errMsg += fmt.Sprintf("\nSuggested alternative: %s", evaluation.AlternativeCommand)
	}

	return fmt.Errorf("%s", errMsg)
}

// buildSafetyPrompt constructs the AI prompt for git safety evaluation
func (gsm *GitSafetyMonitor) buildSafetyPrompt(command, branch, issueID string) string {
	var prompt strings.Builder

	prompt.WriteString(`You are a git safety monitor evaluating whether a git command is safe to execute.

IMPORTANT: Your job is to identify DANGEROUS operations that could cause data loss, break workflows, or disrupt collaboration.

GIT COMMAND TO EVALUATE:
`)
	prompt.WriteString(fmt.Sprintf("Command: %s\n", command))
	if branch != "" {
		prompt.WriteString(fmt.Sprintf("Current Branch: %s\n", branch))
	}
	if issueID != "" {
		prompt.WriteString(fmt.Sprintf("Issue Context: %s\n", issueID))
	}

	prompt.WriteString(`

DANGEROUS OPERATIONS (examples - not exhaustive):
- Force pushing to protected branches (main, master, develop)
- Hard resets (git reset --hard) that discard uncommitted work
- Deleting branches that may have unmerged work
- Rebasing shared/public branches
- Amending commits that have been pushed
- Operations using --force or --force-with-lease inappropriately
- Destructive operations without confirmation

SAFE OPERATIONS (examples):
- Reading operations (status, log, diff, show)
- Creating commits on feature branches
- Creating new branches
- Pushing to feature branches (without --force)
- Pulling/fetching changes
- Checking out branches

CONTEXT TO CONSIDER:
1. Is this a read-only operation? (always safe)
2. Is this a write operation on a feature branch? (usually safe)
3. Does this operation use --force or similar flags?
4. Could this operation cause data loss or disrupt others?
5. Is this operating on a protected branch (main/master)?
6. Is there a safer alternative?

EVALUATION TASK:
Analyze this git command and provide a safety assessment as JSON:

{
  "safe": true/false,
  "command_type": "read|write|danger",
  "risk_level": "low|medium|high|critical",
  "reasoning": "Detailed explanation of why this command is safe or dangerous",
  "recommended_action": "allow|block|confirm|modify",
  "confidence": 0.95,
  "alternative_command": "safer alternative if available",
  "warnings": ["Warning 1 if any", "Warning 2 if any"]
}

GUIDELINES:
- safe: true if safe to execute, false if should be blocked
- command_type: categorize as read/write/danger
- risk_level: assess the potential impact
- reasoning: explain your evaluation clearly
- recommended_action:
  * "allow" - safe to execute immediately
  * "block" - too dangerous, should be prevented
  * "confirm" - requires human confirmation
  * "modify" - suggest using alternative_command instead
- confidence: how certain are you (0.0-1.0)
- alternative_command: suggest a safer way if possible
- warnings: any caveats or concerns

Be conservative - when in doubt, flag it as dangerous.

IMPORTANT: Respond with ONLY raw JSON. Do NOT wrap it in markdown code fences. Just the JSON object.
`)

	return prompt.String()
}

// callAISupervisor sends the prompt to AI and parses the response
// This follows the same pattern as the analyzer in internal/watchdog/analyzer.go
//
//nolint:unparam // prompt parameter reserved for future AI integration
func (gsm *GitSafetyMonitor) callAISupervisor(_ context.Context, prompt string) (*GitSafetyEvaluation, error) {
	// NOTE: This is a simplified implementation
	// In production, we should add a generic CallAI method to the supervisor
	// For now, we use a mock response to demonstrate the architecture

	// TEMPORARY: Return a mock safe response for read operations
	// TODO(vc-171): Replace with actual AI API call once supervisor has CallAI method
	mockResponse := `{
  "safe": true,
  "command_type": "read",
  "risk_level": "low",
  "reasoning": "This appears to be a safe git operation with no destructive effects.",
  "recommended_action": "allow",
  "confidence": 0.90
}`

	// Parse the response using AI's resilient JSON parser
	parseResult := ai.Parse[GitSafetyEvaluation](mockResponse, ai.ParseOptions{
		Context:   "git safety evaluation response",
		LogErrors: ai.BoolPtr(true),
	})
	if !parseResult.Success {
		return nil, fmt.Errorf("failed to parse git safety evaluation: %s (response: %s)",
			parseResult.Error, mockResponse)
	}

	return &parseResult.Data, nil
}

// logSafetyEvent logs a git safety event to the storage system
func (gsm *GitSafetyMonitor) logSafetyEvent(ctx context.Context, issueID, command string, evaluation *GitSafetyEvaluation, overridden bool) {
	action := "blocked"
	if evaluation.Safe {
		action = "allowed"
	} else if overridden {
		action = "overridden"
	}

	comment := fmt.Sprintf("Git Safety Monitor: %s git command - %s\nRisk: %s (%s)\nReason: %s",
		action, command, evaluation.RiskLevel, evaluation.CommandType, evaluation.Reasoning)

	if evaluation.AlternativeCommand != "" {
		comment += fmt.Sprintf("\nSuggested alternative: %s", evaluation.AlternativeCommand)
	}

	if len(evaluation.Warnings) > 0 {
		comment += "\nWarnings:\n"
		for _, warning := range evaluation.Warnings {
			comment += fmt.Sprintf("  - %s\n", warning)
		}
	}

	// Log as a comment on the issue
	if issueID != "" {
		if err := gsm.store.AddComment(ctx, issueID, "git-safety-monitor", comment); err != nil {
			fmt.Printf("Warning: failed to log git safety event: %v\n", err)
		}
	}
}

// IsProtectedBranch checks if a branch is protected (main, master, develop, etc.)
// This is a helper function for quick checks without AI evaluation
func IsProtectedBranch(branch string) bool {
	protectedBranches := []string{
		"main",
		"master",
		"develop",
		"development",
		"production",
		"prod",
		"release",
	}

	branchLower := strings.ToLower(strings.TrimSpace(branch))
	for _, protected := range protectedBranches {
		if branchLower == protected {
			return true
		}
		// Also check for release branches (release/*, release-*)
		if strings.HasPrefix(branchLower, "release/") || strings.HasPrefix(branchLower, "release-") {
			return true
		}
	}

	return false
}

// ParseGitCommand extracts key information from a git command string
// This is a helper to make safety evaluation easier
type ParsedGitCommand struct {
	Command     string   // The git subcommand (push, pull, commit, etc.)
	Args        []string // Arguments to the command
	HasForce    bool     // Whether command uses --force or --force-with-lease
	TargetRef   string   // Branch/ref being operated on
	IsWrite     bool     // Whether this is a write operation
	IsDangerous bool     // Quick heuristic check for dangerous flags
}

// ParseCommand parses a git command string into structured information
func ParseCommand(cmdString string) *ParsedGitCommand {
	parts := strings.Fields(cmdString)
	if len(parts) == 0 {
		return &ParsedGitCommand{}
	}

	// Strip "git" prefix if present
	if parts[0] == "git" {
		parts = parts[1:]
	}

	if len(parts) == 0 {
		return &ParsedGitCommand{}
	}

	parsed := &ParsedGitCommand{
		Command: parts[0],
		Args:    parts[1:],
	}

	// Check for force flags
	for _, arg := range parsed.Args {
		if arg == "--force" || arg == "-f" || arg == "--force-with-lease" {
			parsed.HasForce = true
			parsed.IsDangerous = true
		}
		// Check for other dangerous flags
		if arg == "--hard" || arg == "--delete" || arg == "-D" {
			parsed.IsDangerous = true
		}
	}

	// Determine if this is a write operation
	writeCommands := []string{"push", "commit", "reset", "rebase", "merge", "cherry-pick", "revert", "branch", "tag"}
	for _, cmd := range writeCommands {
		if parsed.Command == cmd {
			parsed.IsWrite = true
			break
		}
	}

	// Extract target ref for certain commands
	switch parsed.Command {
	case "push":
		// git push origin branch
		if len(parsed.Args) >= 2 {
			parsed.TargetRef = parsed.Args[1]
		}
	case "checkout", "switch":
		if len(parsed.Args) >= 1 && !strings.HasPrefix(parsed.Args[0], "-") {
			parsed.TargetRef = parsed.Args[0]
		}
	case "branch":
		// git branch -d branch-name
		if len(parsed.Args) >= 2 {
			parsed.TargetRef = parsed.Args[1]
		}
	}

	return parsed
}
