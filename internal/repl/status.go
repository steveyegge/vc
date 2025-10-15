package repl

import (
	"fmt"

	"github.com/fatih/color"
	"github.com/steveyegge/vc/internal/types"
)

// cmdStatus shows project overview
func (r *REPL) cmdStatus(args []string) error {
	ctx := r.ctx

	// Get ready work count
	readyIssues, err := r.store.GetReadyWork(ctx, types.WorkFilter{Limit: 100})
	if err != nil {
		return fmt.Errorf("failed to get ready work: %w", err)
	}

	// Get in-progress work
	inProgressStatus := types.StatusInProgress
	inProgressIssues, err := r.store.SearchIssues(ctx, "", types.IssueFilter{
		Status: &inProgressStatus,
		Limit:  100,
	})
	if err != nil {
		return fmt.Errorf("failed to get in-progress issues: %w", err)
	}

	// Get blocked issues (open but not ready)
	openStatus := types.StatusOpen
	allOpenIssues, err := r.store.SearchIssues(ctx, "", types.IssueFilter{
		Status: &openStatus,
		Limit:  1000,
	})
	if err != nil {
		return fmt.Errorf("failed to get open issues: %w", err)
	}

	// Calculate blocked (open issues that aren't ready)
	readyMap := make(map[string]bool)
	for _, issue := range readyIssues {
		readyMap[issue.ID] = true
	}
	blockedCount := 0
	for _, issue := range allOpenIssues {
		if !readyMap[issue.ID] {
			blockedCount++
		}
	}

	// Display status
	cyan := color.New(color.FgCyan, color.Bold).SprintFunc()
	green := color.New(color.FgGreen).SprintFunc()
	yellow := color.New(color.FgYellow).SprintFunc()
	red := color.New(color.FgRed).SprintFunc()

	fmt.Printf("\n%s\n", cyan("Project Status"))
	fmt.Println()
	fmt.Printf("  %s  %d issues\n", green("✓ Ready"), len(readyIssues))
	fmt.Printf("  %s  %d issues\n", yellow("⚡ In Progress"), len(inProgressIssues))
	fmt.Printf("  %s  %d issues\n", red("⊗ Blocked"), blockedCount)
	fmt.Println()

	if len(readyIssues) > 0 {
		fmt.Println("Use '/ready' to see ready work")
	}
	if blockedCount > 0 {
		fmt.Println("Use '/blocked' to see blocked issues")
	}
	fmt.Println()

	return nil
}

// cmdReady shows ready work
func (r *REPL) cmdReady(args []string) error {
	ctx := r.ctx

	limit := 10
	issues, err := r.store.GetReadyWork(ctx, types.WorkFilter{
		Status: types.StatusOpen,
		Limit:  limit,
	})
	if err != nil {
		return fmt.Errorf("failed to get ready work: %w", err)
	}

	if len(issues) == 0 {
		yellow := color.New(color.FgYellow).SprintFunc()
		fmt.Printf("\n%s No ready work found.\n\n", yellow("ℹ"))
		return nil
	}

	cyan := color.New(color.FgCyan, color.Bold).SprintFunc()
	green := color.New(color.FgGreen).SprintFunc()
	fmt.Printf("\n%s\n", cyan("Ready Work"))
	fmt.Println()

	for i, issue := range issues {
		priorityColor := color.New(color.FgGreen).SprintFunc()
		if issue.Priority == 0 {
			priorityColor = color.New(color.FgRed, color.Bold).SprintFunc()
		} else if issue.Priority == 1 {
			priorityColor = color.New(color.FgYellow).SprintFunc()
		}

		fmt.Printf("%2d. [%s] %s: %s\n",
			i+1,
			priorityColor(fmt.Sprintf("P%d", issue.Priority)),
			green(issue.ID),
			issue.Title,
		)
	}
	fmt.Println()

	return nil
}

// cmdBlocked shows blocked issues
func (r *REPL) cmdBlocked(args []string) error {
	ctx := r.ctx

	// Get all open issues
	openStatus := types.StatusOpen
	allOpenIssues, err := r.store.SearchIssues(ctx, "", types.IssueFilter{
		Status: &openStatus,
		Limit:  1000,
	})
	if err != nil {
		return fmt.Errorf("failed to get open issues: %w", err)
	}

	// Get ready work to determine which are blocked
	readyIssues, err := r.store.GetReadyWork(ctx, types.WorkFilter{Limit: 1000})
	if err != nil {
		return fmt.Errorf("failed to get ready work: %w", err)
	}

	readyMap := make(map[string]bool)
	for _, issue := range readyIssues {
		readyMap[issue.ID] = true
	}

	// Find blocked issues
	var blockedIssues []*types.Issue
	for _, issue := range allOpenIssues {
		if !readyMap[issue.ID] {
			blockedIssues = append(blockedIssues, issue)
		}
	}

	if len(blockedIssues) == 0 {
		green := color.New(color.FgGreen).SprintFunc()
		fmt.Printf("\n%s No blocked issues!\n\n", green("✓"))
		return nil
	}

	cyan := color.New(color.FgCyan, color.Bold).SprintFunc()
	red := color.New(color.FgRed).SprintFunc()
	fmt.Printf("\n%s\n", cyan("Blocked Issues"))
	fmt.Println()

	for i, issue := range blockedIssues {
		if i >= 10 {
			fmt.Printf("\n... and %d more\n", len(blockedIssues)-10)
			break
		}

		// Get dependencies to show what's blocking it
		deps, _ := r.store.GetDependencies(ctx, issue.ID)

		fmt.Printf("%2d. [P%d] %s: %s\n",
			i+1,
			issue.Priority,
			red(issue.ID),
			issue.Title,
		)

		if len(deps) > 0 {
			gray := color.New(color.FgHiBlack).SprintFunc()
			fmt.Printf("    %s Blocked by: ", gray("→"))
			for j, dep := range deps {
				if j > 0 {
					fmt.Print(", ")
				}
				fmt.Print(dep.ID)
				if j >= 2 {
					fmt.Printf(" (+%d more)", len(deps)-3)
					break
				}
			}
			fmt.Println()
		}
	}
	fmt.Println()

	return nil
}
