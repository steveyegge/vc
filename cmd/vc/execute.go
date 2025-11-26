package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/fatih/color"
	"github.com/spf13/cobra"
	"github.com/steveyegge/vc/internal/config"
	"github.com/steveyegge/vc/internal/deduplication"
	"github.com/steveyegge/vc/internal/executor"
	"github.com/steveyegge/vc/internal/storage"
	"github.com/steveyegge/vc/internal/types"
)

var executeCmd = &cobra.Command{
	Use:   "execute",
	Short: "Start the issue processor event loop",
	Long: `Start the executor that continuously claims and processes ready issues.

The executor will:
1. Register as an active executor instance
2. Poll for ready work (issues with no open blockers)
3. Atomically claim available issues
4. Spawn coding agents (Claude Code) to execute the work
5. Update issue status based on agent results
6. Continue until stopped with Ctrl+C

Polecat Mode (--polecat-mode):
When running inside a Gastown polecat, use --polecat-mode for single-task execution.
In this mode, VC accepts a task via --task, --issue, or --stdin, executes once
with quality gates, outputs JSON to stdout, and exits. This mode skips issue
claiming and polling since Gastown handles coordination.

See docs/design/GASTOWN_INTEGRATION.md for details.`,
	Run: func(cmd *cobra.Command, args []string) {
		if err := runExecutor(cmd); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
	},
}

// runExecutor contains the main executor logic, extracted to allow proper defer cleanup.
// This function returns errors instead of calling os.Exit(), which ensures that defer
// statements (like lock cleanup) run properly on all error paths.
func runExecutor(cmd *cobra.Command) error {
	version, _ := cmd.Flags().GetString("version")
	pollSeconds, _ := cmd.Flags().GetInt("poll-interval")
	disableSandboxes, _ := cmd.Flags().GetBool("disable-sandboxes")
	sandboxRoot, _ := cmd.Flags().GetString("sandbox-root")
	parentRepo, _ := cmd.Flags().GetString("parent-repo")
	enableAutoCommit, _ := cmd.Flags().GetBool("enable-auto-commit")
	enableAutoPR, _ := cmd.Flags().GetBool("enable-auto-pr")
	polecatMode, _ := cmd.Flags().GetBool("polecat-mode")
	taskDesc, _ := cmd.Flags().GetString("task")
	issueID, _ := cmd.Flags().GetString("issue")

	// Determine execution mode
	var mode types.ExecutionMode
	if polecatMode {
		mode = types.ModePolecat
	} else {
		mode = types.ModeExecutor
	}

	// Validate polecat mode input requirements (vc-plr3, vc-5fxi)
	// In polecat mode, one of --task, --issue, or --stdin must be provided
	if mode.IsPolecat() {
		if taskDesc == "" && issueID == "" {
			// TODO: Also check --stdin when implemented
			return fmt.Errorf("polecat mode requires --task or --issue (or --stdin when implemented)")
		}

		// Validate mutually exclusive inputs
		if taskDesc != "" && issueID != "" {
			return fmt.Errorf("--task and --issue are mutually exclusive")
		}

		// Build polecat task from input source
		var task *types.PolecatTask
		if taskDesc != "" {
			// Task from CLI argument
			task = &types.PolecatTask{
				Description: taskDesc,
				Source:      types.TaskSourceCLI,
			}
		} else if issueID != "" {
			// Task from beads issue - load issue details
			issue, err := store.GetIssue(context.Background(), issueID)
			if err != nil {
				return fmt.Errorf("failed to load issue %s: %w", issueID, err)
			}
			if issue == nil {
				return fmt.Errorf("issue %s not found", issueID)
			}

			// Construct task description from issue fields
			description := formatIssueAsTask(issue)
			task = &types.PolecatTask{
				Description:        description,
				Source:             types.TaskSourceIssue,
				IssueID:            issueID,
				AcceptanceCriteria: issue.AcceptanceCriteria,
			}
		}

		// Run polecat execution (vc-k0c7)
		return runPolecatExecution(task)
	}

	// Check environment variable as fallback for auto-commit (vc-142)
	if !enableAutoCommit {
		enableAutoCommit = os.Getenv("VC_ENABLE_AUTO_COMMIT") == "true"
	}

	// Check environment variable as fallback for auto-PR (vc-389e)
	if !enableAutoPR {
		enableAutoPR = os.Getenv("VC_ENABLE_AUTO_PR") == "true"
	}

	// Validate auto-PR requires auto-commit (vc-389e)
	if enableAutoPR && !enableAutoCommit {
		return fmt.Errorf("--enable-auto-pr requires --enable-auto-commit to be enabled")
	}

	// Derive working directory from database location
	// This ensures database and code are in the same project
	projectRoot, err := storage.GetProjectRoot(dbPath)
	if err != nil {
		return err
	}

	// Validate alignment between database and working directory
	cwd, _ := os.Getwd()
	if err := storage.ValidateAlignment(dbPath, cwd); err != nil {
		return err
	}

	// vc-195: Acquire exclusive lock to prevent bd daemon interference
	// This implements the VC Daemon Exclusion Protocol
	// bd daemon will check for .beads/.exclusive-lock and skip this database
	lockPath, err := storage.AcquireExclusiveLock(dbPath, version)
	if err != nil {
		return err
	}
	// Ensure lock is released on exit (vc-206: now runs on all error paths)
	defer func() {
		if err := storage.ReleaseExclusiveLock(lockPath); err != nil {
			fmt.Fprintf(os.Stderr, "warning: failed to release exclusive lock: %v\n", err)
		}
	}()
	green := color.New(color.FgGreen).SprintFunc()
	fmt.Fprintf(os.Stderr, "%s Acquired exclusive lock (bd daemon will skip this database)\n", green("✓"))

	// vc-173: Validate database is in sync with issues.jsonl
	// vc-195: Now that we control sync via exclusive lock, this check works reliably
	if err := storage.ValidateDatabaseFreshness(dbPath); err != nil {
		return err
	}

	// Load deduplication configuration from environment
	dedupConfig, err := deduplication.ConfigFromEnv()
	if err != nil {
		return fmt.Errorf("invalid deduplication configuration: %w", err)
	}

	// Load instance cleanup configuration from environment (vc-33)
	instanceCleanupConfig, err := config.InstanceCleanupConfigFromEnv()
	if err != nil {
		return fmt.Errorf("invalid instance cleanup configuration: %w", err)
	}

	// Create executor configuration
	cfg := executor.DefaultConfig()
	cfg.Store = store
	cfg.Version = version
	cfg.WorkingDir = projectRoot // Use project root, not cwd
	cfg.EnableSandboxes = !disableSandboxes // Sandboxes enabled by default (vc-144)
	cfg.SandboxRoot = sandboxRoot
	cfg.ParentRepo = parentRepo
	cfg.DeduplicationConfig = &dedupConfig
	cfg.InstanceCleanupAge = instanceCleanupConfig.CleanupAge() // vc-33: from environment
	cfg.InstanceCleanupKeep = instanceCleanupConfig.CleanupKeep  // vc-33: from environment
	cfg.EnableAutoCommit = enableAutoCommit // vc-142: expose auto-commit configuration
	cfg.EnableAutoPR = enableAutoPR         // vc-389e: expose auto-PR configuration
	if pollSeconds > 0 {
		cfg.PollInterval = time.Duration(pollSeconds) * time.Second
	}

	// Warn if sandboxes are disabled (vc-144)
	if disableSandboxes {
		fmt.Fprintf(os.Stderr, "\n⚠️  WARNING: Sandboxes are disabled!\n")
		fmt.Fprintf(os.Stderr, "   Agents will work directly in your main workspace.\n")
		fmt.Fprintf(os.Stderr, "   Failed executions may leave your repository in a dirty state.\n")
		fmt.Fprintf(os.Stderr, "   This mode is intended for development/testing only.\n\n")
	}

	// Create executor instance
	exec, err := executor.New(cfg)
	if err != nil {
		return fmt.Errorf("failed to create executor: %w", err)
	}

	// Ensure instance is marked as stopped on exit (vc-192)
	// This handles abnormal exits (panics, os.Exit, etc.) in addition to graceful shutdown
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := exec.MarkInstanceStoppedOnExit(shutdownCtx); err != nil {
			fmt.Fprintf(os.Stderr, "warning: failed to mark instance as stopped: %v\n", err)
		}
	}()

	// Set up context with cancellation
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle signals for graceful shutdown
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)

	// Start executor in background
	if err := exec.Start(ctx); err != nil {
		return fmt.Errorf("failed to start executor: %w", err)
	}

	cyan := color.New(color.FgCyan).SprintFunc()
	fmt.Printf("%s Executor started (version %s)\n", green("✓"), cyan(version))
	fmt.Printf("  Polling for ready work every %v\n", cfg.PollInterval)
	if cfg.EnableSandboxes {
		fmt.Printf("  Sandboxes: %s (root: %s)\n", green("enabled"), cfg.SandboxRoot)
	} else {
		fmt.Printf("  Sandboxes: disabled\n")
	}
	fmt.Printf("  Press Ctrl+C to stop\n\n")

	// Wait for shutdown signal
	<-sigCh
	fmt.Println("\n\nShutting down executor...")

	// Stop the executor gracefully
	// Use a fresh context for shutdown since main context is being canceled
	cancel()
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer shutdownCancel()

	if err := exec.Stop(shutdownCtx); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: error during shutdown: %v\n", err)
	}

	fmt.Printf("%s Executor stopped\n", green("✓"))
	return nil
}

// formatIssueAsTask formats a beads issue as a natural language task description
// for the polecat mode executor (vc-5fxi)
func formatIssueAsTask(issue *types.Issue) string {
	var parts []string

	// Start with the title as the main task
	parts = append(parts, fmt.Sprintf("Task: %s", issue.Title))

	// Add description if present
	if issue.Description != "" {
		parts = append(parts, fmt.Sprintf("\nDescription:\n%s", issue.Description))
	}

	// Add design notes if present
	if issue.Design != "" {
		parts = append(parts, fmt.Sprintf("\nDesign:\n%s", issue.Design))
	}

	// Add acceptance criteria if present
	if issue.AcceptanceCriteria != "" {
		parts = append(parts, fmt.Sprintf("\nAcceptance Criteria:\n%s", issue.AcceptanceCriteria))
	}

	// Add any notes
	if issue.Notes != "" {
		parts = append(parts, fmt.Sprintf("\nNotes:\n%s", issue.Notes))
	}

	return strings.Join(parts, "\n")
}

// runPolecatExecution executes a single task in polecat mode (vc-k0c7)
// This is the main entry point for Gastown integration
func runPolecatExecution(task *types.PolecatTask) error {
	// Get current working directory
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get working directory: %w", err)
	}

	// Create polecat executor with current directory as working dir
	// Store is optional in polecat mode - we pass nil since we're not writing to beads
	polecatConfig := executor.DefaultPolecatConfig()
	polecatConfig.WorkingDir = cwd
	polecatConfig.Store = nil // No database writes in polecat mode

	pe, err := executor.NewPolecatExecutor(polecatConfig)
	if err != nil {
		return fmt.Errorf("failed to create polecat executor: %w", err)
	}

	// Execute the task
	ctx := context.Background()
	result := pe.Execute(ctx, task)

	// Output JSON result to stdout
	if err := pe.OutputJSON(result); err != nil {
		return fmt.Errorf("failed to write JSON output: %w", err)
	}

	// Return error if execution failed (non-zero exit code)
	if !result.Success {
		// Don't return error - JSON output already contains the failure details
		// Return nil so caller can parse JSON for details
		// But set exit code based on status
		if result.Status == types.PolecatStatusFailed {
			os.Exit(1)
		}
	}

	return nil
}

func init() {
	executeCmd.Flags().String("version", "0.1.0", "Executor version")
	executeCmd.Flags().IntP("poll-interval", "i", 5, "Poll interval in seconds")
	executeCmd.Flags().Bool("disable-sandboxes", false, "Disable sandbox isolation (DANGEROUS: for development/testing only)")
	executeCmd.Flags().String("sandbox-root", ".sandboxes", "Root directory for sandboxes")
	executeCmd.Flags().String("parent-repo", ".", "Parent repository path")
	executeCmd.Flags().Bool("enable-auto-commit", false, "Enable automatic git commits after successful execution (can also use VC_ENABLE_AUTO_COMMIT=true)")
	executeCmd.Flags().Bool("enable-auto-pr", false, "Enable automatic PR creation after successful commit (requires --enable-auto-commit, can also use VC_ENABLE_AUTO_PR=true)")

	// Polecat mode flags (vc-m5qr, vc-plr3, vc-5fxi: Gastown integration)
	executeCmd.Flags().Bool("polecat-mode", false, "Enable polecat mode for single-task execution inside Gastown")
	executeCmd.Flags().String("task", "", "Task description for polecat mode (natural language)")
	executeCmd.Flags().String("issue", "", "Beads issue ID to load as task for polecat mode")

	rootCmd.AddCommand(executeCmd)
}
