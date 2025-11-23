package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/fatih/color"
	"github.com/spf13/cobra"
	"github.com/steveyegge/vc/internal/control"
)

var pauseCmd = &cobra.Command{
	Use:   "pause <issue-id>",
	Short: "Pause a running task",
	Long: `Pause a running task gracefully, saving agent progress.

The executor will interrupt the current agent execution, save the agent's
context (todos, notes, progress), and mark the issue as 'open' with
interrupt metadata. The task can be resumed later with 'vc resume'.

Use cases:
  - Need to board a plane in 10 minutes
  - Cost budget approaching limit
  - Want to redirect executor to urgent issue
  - Debug agent state without losing progress`,
	Args: cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		issueID := args[0]
		reason, _ := cmd.Flags().GetString("reason")

		// Find executor control socket
		socketPath, err := findExecutorSocket()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			fmt.Fprintf(os.Stderr, "Hint: Is the executor running? Try 'vc status' to check.\n")
			os.Exit(1)
		}

		// Create client and send pause command
		client := control.NewClient(socketPath)
		resp, err := client.Pause(issueID, reason)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: failed to send pause command: %v\n", err)
			os.Exit(1)
		}

		if !resp.Success {
			red := color.New(color.FgRed).SprintFunc()
			fmt.Printf("%s Pause failed: %s\n", red("✗"), resp.Message)
			if resp.Error != "" {
				fmt.Printf("  Error: %s\n", resp.Error)
			}
			os.Exit(1)
		}

		green := color.New(color.FgGreen).SprintFunc()
		fmt.Printf("%s Task paused: %s\n", green("✓"), issueID)
		fmt.Printf("  %s\n", resp.Message)

		if resp.Data != nil {
			if savedContext, ok := resp.Data["saved_context"].(bool); ok && savedContext {
				fmt.Printf("  Agent context saved for resume\n")
			}
			if interruptTime, ok := resp.Data["interrupted_at"].(string); ok {
				fmt.Printf("  Interrupted at: %s\n", interruptTime)
			}
		}

		fmt.Printf("\nTo resume later: vc resume %s\n", issueID)
	},
}

func init() {
	pauseCmd.Flags().StringP("reason", "r", "", "Reason for pausing (optional)")
	rootCmd.AddCommand(pauseCmd)
}

// findExecutorSocket finds the control socket for a running executor
// Returns the socket path or error if not found
func findExecutorSocket() (string, error) {
	// Look for socket in standard locations:
	// 1. .vc/executor.sock (current directory)
	// 2. /tmp/vc-*.sock (system temp, for current user)

	// Try .vc/executor.sock first
	localSocket := filepath.Join(".vc", "executor.sock")
	if _, err := os.Stat(localSocket); err == nil {
		return localSocket, nil
	}

	// Try /tmp/vc-<user>.sock
	user := os.Getenv("USER")
	if user == "" {
		user = "unknown"
	}
	tmpSocket := filepath.Join("/tmp", fmt.Sprintf("vc-%s.sock", user))
	if _, err := os.Stat(tmpSocket); err == nil {
		return tmpSocket, nil
	}

	// Check for any vc-*.sock in /tmp (for other instances)
	tmpDir := "/tmp"
	entries, err := os.ReadDir(tmpDir)
	if err == nil {
		for _, entry := range entries {
			if filepath.Ext(entry.Name()) == ".sock" &&
			   len(entry.Name()) > 3 && entry.Name()[:3] == "vc-" {
				socketPath := filepath.Join(tmpDir, entry.Name())
				return socketPath, nil
			}
		}
	}

	return "", fmt.Errorf("no running executor found (no control socket)")
}
