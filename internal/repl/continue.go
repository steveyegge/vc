package repl

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/fatih/color"
	"github.com/steveyegge/vc/internal/executor"
	"github.com/steveyegge/vc/internal/types"
)

// cmdContinue resumes execution by claiming ready work and spawning a worker
func (r *REPL) cmdContinue(args []string) error {
	ctx := r.ctx

	// Get one piece of ready work (highest priority)
	issues, err := r.store.GetReadyWork(ctx, types.WorkFilter{
		Status: types.StatusOpen,
		Limit:  1,
	})
	if err != nil {
		return fmt.Errorf("failed to get ready work: %w", err)
	}

	if len(issues) == 0 {
		yellow := color.New(color.FgYellow).SprintFunc()
		fmt.Printf("\n%s No ready work found.\n", yellow("ℹ"))
		fmt.Println("All issues are either blocked or already in progress.")
		fmt.Println()
		return nil
	}

	issue := issues[0]

	// Show what we're about to work on
	cyan := color.New(color.FgCyan, color.Bold).SprintFunc()
	green := color.New(color.FgGreen).SprintFunc()
	gray := color.New(color.FgHiBlack).SprintFunc()

	fmt.Printf("\n%s\n", cyan("Next Task"))
	fmt.Println()
	fmt.Printf("  ID: %s\n", green(issue.ID))
	fmt.Printf("  Priority: P%d\n", issue.Priority)
	fmt.Printf("  Title: %s\n", issue.Title)
	if issue.Description != "" {
		fmt.Printf("  Description: %s\n", gray(truncate(issue.Description, 80)))
	}
	fmt.Println()

	// Update issue status to in_progress and claim it
	updates := map[string]interface{}{
		"status": types.StatusInProgress,
	}
	if err := r.store.UpdateIssue(ctx, issue.ID, updates, r.actor); err != nil {
		return fmt.Errorf("failed to update issue status: %w", err)
	}
	issue.Status = types.StatusInProgress // Update local copy

	fmt.Printf("%s Claimed %s and starting worker...\n\n", green("✓"), issue.ID)

	// Get working directory (current directory)
	workingDir, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get working directory: %w", err)
	}

	// Spawn the worker agent
	result, err := r.spawnWorkerForIssue(ctx, issue, workingDir)
	if err != nil {
		// Worker spawn failed - update issue with error note
		errNote := fmt.Sprintf("\n\n[%s] Worker spawn failed: %v", time.Now().Format(time.RFC3339), err)
		noteUpdates := map[string]interface{}{
			"notes": issue.Notes + errNote,
		}
		if updateErr := r.store.UpdateIssue(ctx, issue.ID, noteUpdates, r.actor); updateErr != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to update issue notes: %v\n", updateErr)
		}
		return fmt.Errorf("worker spawn failed: %w", err)
	}

	fmt.Println()

	// Display results
	if result.Success {
		fmt.Printf("%s Worker completed successfully in %v\n", green("✓"), result.Duration.Round(time.Second))
		fmt.Printf("   Exit code: %d\n", result.ExitCode)
		fmt.Printf("   Output lines: %d\n", len(result.Output))
		if len(result.Errors) > 0 {
			fmt.Printf("   Error lines: %d\n", len(result.Errors))
		}
	} else {
		red := color.New(color.FgRed).SprintFunc()
		fmt.Printf("%s Worker failed after %v\n", red("✗"), result.Duration.Round(time.Second))
		fmt.Printf("   Exit code: %d\n", result.ExitCode)
		if len(result.Errors) > 0 {
			fmt.Printf("   Error lines: %d\n", len(result.Errors))
			fmt.Println()
			fmt.Println(red("Recent errors:"))
			// Show last 5 error lines
			start := len(result.Errors) - 5
			if start < 0 {
				start = 0
			}
			for _, line := range result.Errors[start:] {
				fmt.Printf("   %s\n", gray(line))
			}
		}
	}

	// Add result to issue notes
	resultNote := fmt.Sprintf("\n\n[%s] Worker execution:\n- Duration: %v\n- Exit code: %d\n- Success: %t",
		time.Now().Format(time.RFC3339),
		result.Duration.Round(time.Second),
		result.ExitCode,
		result.Success,
	)

	// Update issue with results
	// For MVP: keep status as in_progress - let user decide when to close
	// Future: AI analysis will determine if task is complete
	resultUpdates := map[string]interface{}{
		"notes": issue.Notes + resultNote,
	}
	if err := r.store.UpdateIssue(ctx, issue.ID, resultUpdates, r.actor); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to update issue with results: %v\n", err)
	}

	fmt.Println()
	yellow := color.New(color.FgYellow).SprintFunc()
	fmt.Printf("Issue %s remains %s\n", green(issue.ID), yellow("in progress"))
	fmt.Println("Use 'bd show <id>' to see full details and decide next steps")
	fmt.Println()

	return nil
}

// spawnWorkerForIssue spawns a Claude Code worker for the given issue
func (r *REPL) spawnWorkerForIssue(ctx context.Context, issue *types.Issue, workingDir string) (*executor.AgentResult, error) {
	// Configure the agent
	cfg := executor.AgentConfig{
		Type:       executor.AgentTypeClaudeCode,
		WorkingDir: workingDir,
		Issue:      issue,
		StreamJSON: false, // Don't need JSON parsing for MVP
		Timeout:    30 * time.Minute,
	}

	// Spawn the agent
	agent, err := executor.SpawnAgent(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to spawn agent: %w", err)
	}

	// Wait for completion (output is streamed in real-time by agent.go)
	result, err := agent.Wait(ctx)
	if err != nil {
		return nil, fmt.Errorf("agent execution failed: %w", err)
	}

	return result, nil
}

// truncate truncates a string to maxLen characters, adding "..." if truncated
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}
