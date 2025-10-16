package repl

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/fatih/color"
	"github.com/steveyegge/vc/internal/ai"
	"github.com/steveyegge/vc/internal/executor"
	"github.com/steveyegge/vc/internal/types"
)

// timestamp returns a formatted timestamp for activity logging
func timestamp() string {
	gray := color.New(color.FgHiBlack).SprintFunc()
	return gray(time.Now().Format("[15:04:05]"))
}

// cmdContinue finds ready work and spawns a worker to execute it
// This implements vc-75 and uses vc-76's ResultsProcessor
func (r *REPL) cmdContinue(args []string) error {
	ctx := r.ctx

	cyan := color.New(color.FgCyan, color.Bold).SprintFunc()
	green := color.New(color.FgGreen).SprintFunc()
	yellow := color.New(color.FgYellow).SprintFunc()
	red := color.New(color.FgRed).SprintFunc()

	fmt.Printf("\n%s %s\n", timestamp(), cyan("Finding ready work..."))

	// Step 1: Get ready work (limit 1)
	issues, err := r.store.GetReadyWork(ctx, types.WorkFilter{
		Status: types.StatusOpen,
		Limit:  1,
	})
	if err != nil {
		return fmt.Errorf("failed to get ready work: %w", err)
	}

	if len(issues) == 0 {
		fmt.Printf("\n%s No ready work found. All issues are either completed or blocked.\n\n", yellow("ℹ"))
		return nil
	}

	issue := issues[0]

	// Step 2: Show user what will be executed
	fmt.Printf("\n%s\n", cyan("Next Issue:"))
	fmt.Printf("  ID: %s\n", green(issue.ID))
	fmt.Printf("  Title: %s\n", issue.Title)
	fmt.Printf("  Priority: P%d\n", issue.Priority)
	fmt.Printf("  Type: %s\n", issue.IssueType)

	if issue.Description != "" {
		fmt.Printf("\n  Description:\n")
		// Indent description
		for _, line := range splitLines(issue.Description, 70) {
			fmt.Printf("    %s\n", line)
		}
	}

	fmt.Println()

	// Step 3: Claim the issue
	// For REPL, we use a simple instance ID (could improve this later)
	instanceID := fmt.Sprintf("repl-%s", r.actor)
	if err := r.store.ClaimIssue(ctx, issue.ID, instanceID); err != nil {
		return fmt.Errorf("failed to claim issue %s: %w", issue.ID, err)
	}

	fmt.Printf("%s %s Claimed issue %s\n\n", timestamp(), green("✓"), issue.ID)

	// Update execution state to executing
	if err := r.store.UpdateExecutionState(ctx, issue.ID, types.ExecutionStateExecuting); err != nil {
		fmt.Fprintf(os.Stderr, "warning: failed to update execution state: %v\n", err)
	}

	// Step 4: Spawn Claude Code worker
	fmt.Printf("%s %s Spawning Claude Code worker...\n\n", timestamp(), cyan("⚙"))

	agentCfg := executor.AgentConfig{
		Type:       executor.AgentTypeClaudeCode,
		WorkingDir: ".",
		Issue:      issue,
		StreamJSON: false,
		Timeout:    30 * time.Minute,
	}

	agent, err := executor.SpawnAgent(ctx, agentCfg)
	if err != nil {
		r.releaseIssueWithError(ctx, issue.ID, instanceID, fmt.Sprintf("Failed to spawn agent: %v", err))
		return fmt.Errorf("failed to spawn agent: %w", err)
	}

	fmt.Printf("%s %s\n", timestamp(), cyan("=== Worker Output ==="))
	fmt.Println()

	// Step 5: Wait for completion (output is streamed in real-time by agent.go)
	result, err := agent.Wait(ctx)
	if err != nil {
		r.releaseIssueWithError(ctx, issue.ID, instanceID, fmt.Sprintf("Agent execution failed: %v", err))
		return fmt.Errorf("agent execution failed: %w", err)
	}

	fmt.Println()
	fmt.Printf("%s %s\n", timestamp(), cyan("=== Worker Completed ==="))
	fmt.Println()

	// Step 6: Process results using ResultsProcessor (vc-76 implementation)
	supervisor, err := ai.NewSupervisor(&ai.Config{
		Store: r.store,
	})
	if err != nil {
		// Continue without AI supervision
		fmt.Fprintf(os.Stderr, "Warning: AI supervisor not available: %v (continuing without AI analysis)\n", err)
		supervisor = nil
	}

	processor, err := executor.NewResultsProcessor(&executor.ResultsProcessorConfig{
		Store:              r.store,
		Supervisor:         supervisor,
		EnableQualityGates: true, // Enable quality gates for REPL execution
		WorkingDir:         ".",
		Actor:              instanceID,
	})
	if err != nil {
		r.releaseIssueWithError(ctx, issue.ID, instanceID, fmt.Sprintf("Failed to create results processor: %v", err))
		return fmt.Errorf("failed to create results processor: %w", err)
	}

	procResult, err := processor.ProcessAgentResult(ctx, issue, result)
	if err != nil {
		r.releaseIssueWithError(ctx, issue.ID, instanceID, fmt.Sprintf("Failed to process results: %v", err))
		return fmt.Errorf("failed to process results: %w", err)
	}

	// Step 7: Show summary to user
	fmt.Println()
	if procResult.Completed {
		fmt.Printf("%s %s Issue %s completed successfully!\n", timestamp(), green("✓"), issue.ID)
	} else if !procResult.GatesPassed {
		fmt.Printf("%s %s Issue %s blocked by quality gates\n", timestamp(), red("✗"), issue.ID)
	} else if !result.Success {
		fmt.Printf("%s %s Worker failed for issue %s\n", timestamp(), red("✗"), issue.ID)
	} else {
		fmt.Printf("%s %s Issue %s partially complete (left open)\n", timestamp(), yellow("⚡"), issue.ID)
	}

	if len(procResult.DiscoveredIssues) > 0 {
		fmt.Printf("%s %s Created %d follow-on issues: %v\n",
			timestamp(), green("✓"), len(procResult.DiscoveredIssues), procResult.DiscoveredIssues)
	}

	fmt.Println()

	return nil
}

// releaseIssueWithError releases an issue and adds an error comment
func (r *REPL) releaseIssueWithError(ctx context.Context, issueID, actor, errMsg string) {
	// Add error comment
	if err := r.store.AddComment(ctx, issueID, actor, errMsg); err != nil {
		fmt.Fprintf(os.Stderr, "warning: failed to add error comment: %v\n", err)
	}

	// Release the execution state
	if err := r.store.ReleaseIssue(ctx, issueID); err != nil {
		fmt.Fprintf(os.Stderr, "warning: failed to release issue: %v\n", err)
	}
}

// splitLines splits text into lines with max width
func splitLines(text string, maxWidth int) []string {
	// Simple implementation - can be improved
	var lines []string
	currentLine := ""

	words := []rune(text)
	for i := 0; i < len(words); i++ {
		if words[i] == '\n' {
			lines = append(lines, currentLine)
			currentLine = ""
			continue
		}

		currentLine += string(words[i])
		if len(currentLine) >= maxWidth {
			lines = append(lines, currentLine)
			currentLine = ""
		}
	}

	if currentLine != "" {
		lines = append(lines, currentLine)
	}

	return lines
}
