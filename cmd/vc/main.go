package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/fatih/color"
	"github.com/spf13/cobra"
	"github.com/steveyegge/vc/internal/storage"
	"github.com/steveyegge/vc/internal/storage/beads"
	"github.com/steveyegge/vc/internal/types"
)

var (
	dbPath string
	actor  string
	store  storage.Storage
)

var rootCmd = &cobra.Command{
	Use:   "vc",
	Short: "VC - AI-orchestrated coding agent colony",
	Long:  `VibeCoder v2: Orchestrate coding agents to work on small, well-defined tasks with AI supervision.`,
	PersistentPreRun: func(cmd *cobra.Command, args []string) {
		// Skip database initialization for init command
		if cmd.Name() == "init" {
			return
		}

		// Initialize storage
		var err error
		if dbPath == "" {
			// Auto-discover database by walking up directory tree
			dbPath, err = storage.DiscoverDatabase()
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				os.Exit(1)
			}
		} else {
			// Make path absolute if relative was provided
			dbPath, err = filepath.Abs(dbPath)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error: invalid database path: %v\n", err)
				os.Exit(1)
			}
		}

		ctx := context.Background()
		store, err = beads.NewVCStorage(ctx, dbPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: failed to open database: %v\n", err)
			os.Exit(1)
		}

		// Set actor from env or default
		if actor == "" {
			actor = os.Getenv("USER")
			if actor == "" {
				actor = "unknown"
			}
		}
	},
	PersistentPostRun: func(cmd *cobra.Command, args []string) {
		if store != nil {
			_ = store.Close() // Ignore close error on cleanup
		}
	},
}

func init() {
	rootCmd.PersistentFlags().StringVar(&dbPath, "db", "", "Database path (default: ~/.vc/vc.db)")
	rootCmd.PersistentFlags().StringVar(&actor, "actor", "", "Actor name for audit trail (default: $USER)")
}

var createCmd = &cobra.Command{
	Use:   "create [title]",
	Short: "Create a new issue",
	Args:  cobra.MinimumNArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		title := args[0]
		description, _ := cmd.Flags().GetString("description")
		design, _ := cmd.Flags().GetString("design")
		acceptance, _ := cmd.Flags().GetString("acceptance")
		priority, _ := cmd.Flags().GetInt("priority")
		issueType, _ := cmd.Flags().GetString("type")
		assignee, _ := cmd.Flags().GetString("assignee")
		labels, _ := cmd.Flags().GetStringSlice("labels")

		issue := &types.Issue{
			Title:              title,
			Description:        description,
			Design:             design,
			AcceptanceCriteria: acceptance,
			Status:             types.StatusOpen,
			Priority:           priority,
			IssueType:          types.IssueType(issueType),
			Assignee:           assignee,
		}

		ctx := context.Background()
		if err := store.CreateIssue(ctx, issue, actor); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}

		// Add labels if specified
		for _, label := range labels {
			if err := store.AddLabel(ctx, issue.ID, label, actor); err != nil {
				fmt.Fprintf(os.Stderr, "Warning: failed to add label %s: %v\n", label, err)
			}
		}

		green := color.New(color.FgGreen).SprintFunc()
		fmt.Printf("%s Created issue: %s\n", green("✓"), issue.ID)
		fmt.Printf("  Title: %s\n", issue.Title)
		fmt.Printf("  Priority: P%d\n", issue.Priority)
		fmt.Printf("  Status: %s\n", issue.Status)
	},
}

func init() {
	createCmd.Flags().StringP("description", "d", "", "Issue description")
	createCmd.Flags().String("design", "", "Design notes")
	createCmd.Flags().String("acceptance", "", "Acceptance criteria")
	createCmd.Flags().IntP("priority", "p", 2, "Priority (0-4, 0=highest)")
	createCmd.Flags().StringP("type", "t", "task", "Issue type (bug|feature|task|epic|chore)")
	createCmd.Flags().StringP("assignee", "a", "", "Assignee")
	createCmd.Flags().StringSliceP("labels", "l", []string{}, "Labels (comma-separated)")
	rootCmd.AddCommand(createCmd)
}

var showCmd = &cobra.Command{
	Use:   "show [id]",
	Short: "Show issue details",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		ctx := context.Background()
		issue, err := store.GetIssue(ctx, args[0])
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		if issue == nil {
			fmt.Fprintf(os.Stderr, "Issue %s not found\n", args[0])
			os.Exit(1)
		}

		cyan := color.New(color.FgCyan).SprintFunc()
		fmt.Printf("\n%s: %s\n", cyan(issue.ID), issue.Title)
		fmt.Printf("Status: %s\n", issue.Status)
		fmt.Printf("Priority: P%d\n", issue.Priority)
		fmt.Printf("Type: %s\n", issue.IssueType)
		if issue.Assignee != "" {
			fmt.Printf("Assignee: %s\n", issue.Assignee)
		}
		if issue.EstimatedMinutes != nil {
			fmt.Printf("Estimated: %d minutes\n", *issue.EstimatedMinutes)
		}
		fmt.Printf("Created: %s\n", issue.CreatedAt.Format("2006-01-02 15:04"))
		fmt.Printf("Updated: %s\n", issue.UpdatedAt.Format("2006-01-02 15:04"))

		if issue.Description != "" {
			fmt.Printf("\nDescription:\n%s\n", issue.Description)
		}
		if issue.Design != "" {
			fmt.Printf("\nDesign:\n%s\n", issue.Design)
		}
		if issue.AcceptanceCriteria != "" {
			fmt.Printf("\nAcceptance Criteria:\n%s\n", issue.AcceptanceCriteria)
		}

		// Show labels
		labels, _ := store.GetLabels(ctx, issue.ID)
		if len(labels) > 0 {
			fmt.Printf("\nLabels: %v\n", labels)
		}

		// Show dependencies
		deps, _ := store.GetDependencies(ctx, issue.ID)
		if len(deps) > 0 {
			fmt.Printf("\nDepends on (%d):\n", len(deps))
			for _, dep := range deps {
				fmt.Printf("  → %s: %s [P%d]\n", dep.ID, dep.Title, dep.Priority)
			}
		}

		// Show dependents
		dependents, _ := store.GetDependents(ctx, issue.ID)
		if len(dependents) > 0 {
			fmt.Printf("\nBlocks (%d):\n", len(dependents))
			for _, dep := range dependents {
				fmt.Printf("  ← %s: %s [P%d]\n", dep.ID, dep.Title, dep.Priority)
			}
		}

		fmt.Println()
	},
}

func init() {
	rootCmd.AddCommand(showCmd)
}

var listCmd = &cobra.Command{
	Use:   "list",
	Short: "List issues",
	Run: func(cmd *cobra.Command, args []string) {
		status, _ := cmd.Flags().GetString("status")
		assignee, _ := cmd.Flags().GetString("assignee")
		issueType, _ := cmd.Flags().GetString("type")
		limit, _ := cmd.Flags().GetInt("limit")

		filter := types.IssueFilter{
			Limit: limit,
		}
		if status != "" {
			s := types.Status(status)
			filter.Status = &s
		}
		// Use Changed() to properly handle P0 (priority=0)
		if cmd.Flags().Changed("priority") {
			priority, _ := cmd.Flags().GetInt("priority")
			filter.Priority = &priority
		}
		if assignee != "" {
			filter.Assignee = &assignee
		}
		if issueType != "" {
			t := types.IssueType(issueType)
			filter.IssueType = &t
		}

		ctx := context.Background()
		issues, err := store.SearchIssues(ctx, "", filter)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}

		fmt.Printf("\nFound %d issues:\n\n", len(issues))
		for _, issue := range issues {
			fmt.Printf("%s [P%d] %s\n", issue.ID, issue.Priority, issue.Status)
			fmt.Printf("  %s\n", issue.Title)
			if issue.Assignee != "" {
				fmt.Printf("  Assignee: %s\n", issue.Assignee)
			}
			fmt.Println()
		}
	},
}

func init() {
	listCmd.Flags().StringP("status", "s", "", "Filter by status")
	listCmd.Flags().IntP("priority", "p", 0, "Filter by priority")
	listCmd.Flags().StringP("assignee", "a", "", "Filter by assignee")
	listCmd.Flags().StringP("type", "t", "", "Filter by type")
	listCmd.Flags().IntP("limit", "n", 0, "Limit results")
	rootCmd.AddCommand(listCmd)
}

var updateCmd = &cobra.Command{
	Use:   "update [id]",
	Short: "Update an issue",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		updates := make(map[string]interface{})

		if cmd.Flags().Changed("status") {
			status, _ := cmd.Flags().GetString("status")
			updates["status"] = status
		}
		if cmd.Flags().Changed("priority") {
			priority, _ := cmd.Flags().GetInt("priority")
			updates["priority"] = priority
		}
		if cmd.Flags().Changed("title") {
			title, _ := cmd.Flags().GetString("title")
			updates["title"] = title
		}
		if cmd.Flags().Changed("assignee") {
			assignee, _ := cmd.Flags().GetString("assignee")
			updates["assignee"] = assignee
		}

		if len(updates) == 0 {
			fmt.Println("No updates specified")
			return
		}

		ctx := context.Background()
		if err := store.UpdateIssue(ctx, args[0], updates, actor); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}

		green := color.New(color.FgGreen).SprintFunc()
		fmt.Printf("%s Updated issue: %s\n", green("✓"), args[0])
	},
}

func init() {
	updateCmd.Flags().StringP("status", "s", "", "New status")
	updateCmd.Flags().IntP("priority", "p", 0, "New priority")
	updateCmd.Flags().String("title", "", "New title")
	updateCmd.Flags().StringP("assignee", "a", "", "New assignee")
	rootCmd.AddCommand(updateCmd)
}

var closeCmd = &cobra.Command{
	Use:   "close [id...]",
	Short: "Close one or more issues",
	Args:  cobra.MinimumNArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		reason, _ := cmd.Flags().GetString("reason")
		if reason == "" {
			reason = "Closed"
		}

		ctx := context.Background()
		for _, id := range args {
			if err := store.CloseIssue(ctx, id, reason, actor); err != nil {
				fmt.Fprintf(os.Stderr, "Error closing %s: %v\n", id, err)
				continue
			}
			green := color.New(color.FgGreen).SprintFunc()
			fmt.Printf("%s Closed %s: %s\n", green("✓"), id, reason)
		}
	},
}

func init() {
	closeCmd.Flags().StringP("reason", "r", "", "Reason for closing")
	rootCmd.AddCommand(closeCmd)
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}
