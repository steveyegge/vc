package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/fatih/color"
	"github.com/spf13/cobra"
	"github.com/steveyegge/vc/internal/deduplication"
	"github.com/steveyegge/vc/internal/executor"
	"github.com/steveyegge/vc/internal/storage"
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
6. Continue until stopped with Ctrl+C`,
	Run: func(cmd *cobra.Command, args []string) {
		version, _ := cmd.Flags().GetString("version")
		pollSeconds, _ := cmd.Flags().GetInt("poll-interval")
		disableSandboxes, _ := cmd.Flags().GetBool("disable-sandboxes")
		sandboxRoot, _ := cmd.Flags().GetString("sandbox-root")
		parentRepo, _ := cmd.Flags().GetString("parent-repo")
		enableAutoCommit, _ := cmd.Flags().GetBool("enable-auto-commit")

		// Check environment variable as fallback for auto-commit (vc-142)
		if !enableAutoCommit {
			enableAutoCommit = os.Getenv("VC_ENABLE_AUTO_COMMIT") == "true"
		}

		// Derive working directory from database location
		// This ensures database and code are in the same project
		projectRoot, err := storage.GetProjectRoot(dbPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}

		// Validate alignment between database and working directory
		cwd, _ := os.Getwd()
		if err := storage.ValidateAlignment(dbPath, cwd); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}

		// vc-195: Acquire exclusive lock to prevent bd daemon interference
		// This implements the VC Daemon Exclusion Protocol
		// bd daemon will check for .beads/.exclusive-lock and skip this database
		lockPath, err := storage.AcquireExclusiveLock(dbPath, version)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		// Ensure lock is released on exit
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
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}

		// Load deduplication configuration from environment
		dedupConfig, err := deduplication.ConfigFromEnv()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: invalid deduplication configuration: %v\n", err)
			os.Exit(1)
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
		cfg.EnableAutoCommit = enableAutoCommit // vc-142: expose auto-commit configuration
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
			fmt.Fprintf(os.Stderr, "Error: failed to create executor: %v\n", err)
			os.Exit(1)
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
			fmt.Fprintf(os.Stderr, "Error: failed to start executor: %v\n", err)
			os.Exit(1)
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
	},
}

func init() {
	executeCmd.Flags().String("version", "0.1.0", "Executor version")
	executeCmd.Flags().IntP("poll-interval", "i", 5, "Poll interval in seconds")
	executeCmd.Flags().Bool("disable-sandboxes", false, "Disable sandbox isolation (DANGEROUS: for development/testing only)")
	executeCmd.Flags().String("sandbox-root", ".sandboxes", "Root directory for sandboxes")
	executeCmd.Flags().String("parent-repo", ".", "Parent repository path")
	executeCmd.Flags().Bool("enable-auto-commit", false, "Enable automatic git commits after successful execution (can also use VC_ENABLE_AUTO_COMMIT=true)")
	rootCmd.AddCommand(executeCmd)
}
