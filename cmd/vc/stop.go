package main

import (
	"context"
	"fmt"
	"os"
	"syscall"
	"time"

	"github.com/fatih/color"
	"github.com/spf13/cobra"
	"github.com/steveyegge/vc/internal/types"
)

var stopCmd = &cobra.Command{
	Use:   "stop",
	Short: "Gracefully stop running executor",
	Long: `Stop the running executor process gracefully.

This command will:
1. Find the running executor process from the database
2. Send SIGINT for graceful shutdown
3. Wait for the executor to shut down cleanly
4. Send SIGKILL if shutdown takes longer than 30 seconds
5. Update database state as needed

Example:
  $ vc stop
  Found running executor (PID 965, started 5m ago)
  Sending shutdown signal...
  ✓ Executor stopped gracefully`,
	Run: func(cmd *cobra.Command, args []string) {
		timeout, _ := cmd.Flags().GetDuration("timeout")
		force, _ := cmd.Flags().GetBool("force")

		if err := stopExecutor(timeout, force); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
	},
}

func init() {
	stopCmd.Flags().Duration("timeout", 30*time.Second, "Timeout for graceful shutdown before force kill")
	stopCmd.Flags().Bool("force", false, "Immediately send SIGKILL instead of graceful SIGINT")
	rootCmd.AddCommand(stopCmd)
}

// stopExecutor finds and stops the running executor
func stopExecutor(timeout time.Duration, force bool) error {
	ctx := context.Background()

	// Find active executor instances
	instances, err := store.GetActiveInstances(ctx)
	if err != nil {
		return fmt.Errorf("failed to query active instances: %w", err)
	}

	if len(instances) == 0 {
		yellow := color.New(color.FgYellow).SprintFunc()
		fmt.Printf("%s No running executor found\n", yellow("ℹ"))
		return nil
	}

	// Handle multiple instances (shouldn't happen in normal operation)
	if len(instances) > 1 {
		yellow := color.New(color.FgYellow).SprintFunc()
		fmt.Printf("%s Found %d running executors (unusual, will stop all):\n", yellow("⚠"), len(instances))
		for _, inst := range instances {
			fmt.Printf("  - PID %d on %s (started %s)\n",
				inst.PID, inst.Hostname, formatDuration(time.Since(inst.StartedAt)))
		}
	}

	// Stop each instance
	for _, inst := range instances {
		if err := stopInstance(ctx, inst, timeout, force); err != nil {
			return fmt.Errorf("failed to stop instance %s: %w", inst.InstanceID, err)
		}
	}

	return nil
}

// stopInstance stops a single executor instance
func stopInstance(ctx context.Context, inst *types.ExecutorInstance, timeout time.Duration, force bool) error {
	cyan := color.New(color.FgCyan).SprintFunc()
	green := color.New(color.FgGreen).SprintFunc()
	yellow := color.New(color.FgYellow).SprintFunc()

	fmt.Printf("\n%s Executor instance %s\n", cyan("→"), inst.InstanceID[:8])
	fmt.Printf("  PID: %d\n", inst.PID)
	fmt.Printf("  Host: %s\n", inst.Hostname)
	fmt.Printf("  Started: %s ago\n", formatDuration(time.Since(inst.StartedAt)))

	// Check if process exists
	if !processExists(inst.PID) {
		fmt.Printf("%s Process not running (stale database entry)\n", yellow("⚠"))
		if err := store.MarkInstanceStopped(ctx, inst.InstanceID); err != nil {
			return fmt.Errorf("failed to mark instance as stopped: %w", err)
		}
		fmt.Printf("%s Database state cleaned up\n", green("✓"))
		return nil
	}

	// Send shutdown signal
	if force {
		fmt.Printf("Sending force kill signal (SIGKILL)...\n")
		if err := syscall.Kill(inst.PID, syscall.SIGKILL); err != nil {
			return fmt.Errorf("failed to send SIGKILL: %w", err)
		}
	} else {
		fmt.Printf("Sending graceful shutdown signal (SIGINT)...\n")
		if err := syscall.Kill(inst.PID, syscall.SIGINT); err != nil {
			return fmt.Errorf("failed to send SIGINT: %w", err)
		}
	}

	// Wait for process to exit
	if err := waitForProcessExit(inst.PID, timeout); err != nil {
		// Timeout - send SIGKILL
		fmt.Printf("%s Graceful shutdown timeout after %s\n", yellow("⚠"), timeout)
		fmt.Printf("Sending force kill signal (SIGKILL)...\n")
		if killErr := syscall.Kill(inst.PID, syscall.SIGKILL); killErr != nil {
			return fmt.Errorf("failed to send SIGKILL after timeout: %w", killErr)
		}

		// Wait a bit more for SIGKILL to take effect
		if killWaitErr := waitForProcessExit(inst.PID, 5*time.Second); killWaitErr != nil {
			return fmt.Errorf("process did not exit even after SIGKILL: %w", killWaitErr)
		}
	}

	// Update database state
	if err := store.MarkInstanceStopped(ctx, inst.InstanceID); err != nil {
		return fmt.Errorf("failed to mark instance as stopped: %w", err)
	}

	fmt.Printf("%s Executor stopped successfully\n", green("✓"))
	return nil
}

// processExists checks if a process with the given PID exists
func processExists(pid int) bool {
	// Send signal 0 to check if process exists without actually sending a signal
	err := syscall.Kill(pid, syscall.Signal(0))
	return err == nil
}

// waitForProcessExit waits for a process to exit, with timeout
func waitForProcessExit(pid int, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	pollInterval := 100 * time.Millisecond

	for time.Now().Before(deadline) {
		if !processExists(pid) {
			return nil
		}
		time.Sleep(pollInterval)
	}

	return fmt.Errorf("timeout waiting for process %d to exit", pid)
}
