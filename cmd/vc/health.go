package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/fatih/color"
	"github.com/spf13/cobra"
	"github.com/steveyegge/vc/internal/ai"
	"github.com/steveyegge/vc/internal/health"
	"github.com/steveyegge/vc/internal/storage"
	"github.com/steveyegge/vc/internal/types"
)

var healthCmd = &cobra.Command{
	Use:   "health",
	Short: "Code health monitoring commands",
	Long: `Run code health monitors to detect technical debt and code quality issues.

Health monitors use AI to detect issues like:
- Oversized files that should be split
- Cruft files (backups, temp files, etc.)
- Gitignore violations (secrets, build artifacts, OS files in git)
- ZFC violations (hardcoded thresholds, regex for semantic parsing, etc.)
- Code duplication
- High complexity
- Missing tests

All monitors are ZFC-compliant: they collect facts and defer judgment to AI.`,
}

var healthCheckCmd = &cobra.Command{
	Use:   "check",
	Short: "Run health monitors and report findings",
	Long: `Run health monitors to check code quality and file issues.

Examples:
  # Run all monitors
  vc health check

  # Run specific monitor
  vc health check --monitor file-size
  vc health check --monitor cruft
  vc health check --monitor gitignore
  vc health check --monitor zfc
  vc health check --monitor duplication
  vc health check --monitor complexity

  # Dry run (show issues without filing)
  vc health check --dry-run

  # Verbose output
  vc health check --verbose`,
	Run: func(cmd *cobra.Command, args []string) {
		monitorName, _ := cmd.Flags().GetString("monitor")
		dryRun, _ := cmd.Flags().GetBool("dry-run")
		verbose, _ := cmd.Flags().GetBool("verbose")

		ctx := context.Background()

		// Determine project root from database location
		projectRoot, err := storage.GetProjectRoot(dbPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}

		// Create AI supervisor for health monitors
		supervisor, err := ai.NewSupervisor(&ai.Config{
			Store: store,
		})
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: failed to create AI supervisor: %v\n", err)
			fmt.Fprintf(os.Stderr, "Make sure ANTHROPIC_API_KEY is set in your environment\n")
			os.Exit(1)
		}

		// Build list of monitors to run
		monitors, err := createMonitors(projectRoot, supervisor, monitorName)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}

		// Run monitors
		green := color.New(color.FgGreen).SprintFunc()
		yellow := color.New(color.FgYellow).SprintFunc()
		cyan := color.New(color.FgCyan).SprintFunc()
		red := color.New(color.FgRed).SprintFunc()

		fmt.Printf("\n%s Running health monitors...\n\n", cyan("⚕"))

		totalIssuesFound := 0
		totalIssuesFiled := 0

		for _, monitor := range monitors {
			fmt.Printf("%s %s\n", cyan("▶"), monitor.Name())

			// Build codebase context (currently unused by monitors, but part of interface)
			codebaseCtx := health.CodebaseContext{}

			// Run the monitor
			result, err := monitor.Check(ctx, codebaseCtx)
			if err != nil {
				fmt.Printf("  %s Error: %v\n", red("✗"), err)
				continue
			}

			// Report results
			if verbose {
				fmt.Printf("  %s\n", result.Context)
				if result.Reasoning != "" {
					fmt.Printf("  %s\n", result.Reasoning)
				}
			}

			if len(result.IssuesFound) == 0 {
				fmt.Printf("  %s No issues found\n", green("✓"))
			} else {
				totalIssuesFound += len(result.IssuesFound)
				fmt.Printf("  %s Found %d issue(s)\n", yellow("!"), len(result.IssuesFound))

				// Show discovered issues
				for i, issue := range result.IssuesFound {
					if verbose {
						fmt.Printf("  %d. %s [%s]\n", i+1, issue.Description, issue.Severity)
						if issue.FilePath != "" {
							fmt.Printf("     File: %s\n", issue.FilePath)
						}
					} else {
						fmt.Printf("  - %s\n", issue.Description)
					}
				}

				// File issues if not dry run
				if !dryRun {
					for _, issue := range result.IssuesFound {
						issueID, err := fileHealthIssue(ctx, store, monitor, issue)
						if err != nil {
							fmt.Printf("  %s Failed to file issue: %v\n", red("✗"), err)
							continue
						}
						totalIssuesFiled++
						if verbose {
							fmt.Printf("  %s Filed issue: %s\n", green("✓"), issueID)
						}
					}
					if !verbose && len(result.IssuesFound) > 0 {
						fmt.Printf("  %s Filed %d issue(s)\n", green("✓"), len(result.IssuesFound))
					}
				}
			}

			// Show statistics
			if verbose {
				fmt.Printf("  Stats: %d files scanned, %d AI calls, duration: %v\n",
					result.Stats.FilesScanned,
					result.Stats.AICallsMade,
					result.Stats.Duration)
			}

			fmt.Println()
		}

		// Summary
		fmt.Println(strings.Repeat("─", 60))
		if dryRun {
			fmt.Printf("%s Dry run: found %d issue(s) (not filed)\n",
				yellow("ⓘ"), totalIssuesFound)
			if totalIssuesFound > 0 {
				os.Exit(1) // Exit with error code when issues found
			}
		} else {
			if totalIssuesFiled > 0 {
				fmt.Printf("%s Filed %d issue(s)\n", green("✓"), totalIssuesFiled)
				os.Exit(1) // Exit with error code when issues filed
			} else {
				fmt.Printf("%s No issues found\n", green("✓"))
			}
		}
	},
}

func init() {
	healthCheckCmd.Flags().StringP("monitor", "m", "", "Run specific monitor (file-size, cruft, gitignore, zfc, duplication, complexity)")
	healthCheckCmd.Flags().Bool("dry-run", false, "Show issues without filing")
	healthCheckCmd.Flags().BoolP("verbose", "v", false, "Verbose output")

	healthCmd.AddCommand(healthCheckCmd)
	rootCmd.AddCommand(healthCmd)
}

// createMonitors builds the list of monitors to run based on the monitor flag.
func createMonitors(projectRoot string, supervisor *ai.Supervisor, monitorName string) ([]health.HealthMonitor, error) {
	var monitors []health.HealthMonitor

	// Available monitors
	allMonitors := map[string]func() (health.HealthMonitor, error){
		"file-size": func() (health.HealthMonitor, error) {
			return health.NewFileSizeMonitor(projectRoot, supervisor)
		},
		"cruft": func() (health.HealthMonitor, error) {
			return health.NewCruftDetector(projectRoot, supervisor)
		},
		"gitignore": func() (health.HealthMonitor, error) {
			return health.NewGitignoreDetector(projectRoot, supervisor)
		},
		"zfc": func() (health.HealthMonitor, error) {
			return health.NewZFCDetector(projectRoot, supervisor)
		},
		"duplication": func() (health.HealthMonitor, error) {
			return health.NewDuplicationDetector(projectRoot, supervisor)
		},
		"complexity": func() (health.HealthMonitor, error) {
			return health.NewComplexityMonitor(projectRoot, supervisor)
		},
	}

	// If specific monitor requested, only create that one
	if monitorName != "" {
		createFn, ok := allMonitors[monitorName]
		if !ok {
			validNames := make([]string, 0, len(allMonitors))
			for name := range allMonitors {
				validNames = append(validNames, name)
			}
			return nil, fmt.Errorf("unknown monitor %q. Valid monitors: %s",
				monitorName, strings.Join(validNames, ", "))
		}

		monitor, err := createFn()
		if err != nil {
			return nil, fmt.Errorf("failed to create monitor %q: %w", monitorName, err)
		}
		monitors = append(monitors, monitor)
	} else {
		// Create all monitors
		// Order matters: run cheaper checks first, expensive checks last
		monitorOrder := []string{"file-size", "cruft", "gitignore", "zfc", "duplication", "complexity"}

		for _, name := range monitorOrder {
			createFn := allMonitors[name]
			monitor, err := createFn()
			if err != nil {
				return nil, fmt.Errorf("failed to create monitor %q: %w", name, err)
			}
			monitors = append(monitors, monitor)
		}
	}

	return monitors, nil
}

// fileHealthIssue creates an issue in the tracker for a discovered health problem.
func fileHealthIssue(ctx context.Context, store storage.Storage, monitor health.HealthMonitor, discovered health.DiscoveredIssue) (string, error) {
	// Build issue title and description
	title := buildIssueTitle(monitor, discovered)
	description := buildIssueDescription(monitor, discovered)

	// Determine priority based on severity
	priority := 3 // Default: P3 (low)
	switch discovered.Severity {
	case "high":
		priority = 1 // P1
	case "medium":
		priority = 2 // P2
	}

	// Create the issue
	issue := &types.Issue{
		Title:       title,
		Description: description,
		Status:      types.StatusOpen,
		Priority:    priority,
		IssueType:   types.TypeTask,
	}

	// Add health monitor label
	err := store.CreateIssue(ctx, issue, "vc-health-monitor")
	if err != nil {
		return "", fmt.Errorf("creating issue: %w", err)
	}

	// Add labels
	labels := []string{
		"health",
		discovered.Category,
		fmt.Sprintf("severity:%s", discovered.Severity),
	}

	for _, label := range labels {
		if err := store.AddLabel(ctx, issue.ID, label, "vc-health-monitor"); err != nil {
			// Log but don't fail on label errors
			fmt.Fprintf(os.Stderr, "Warning: failed to add label %q to %s: %v\n", label, issue.ID, err)
		}
	}

	return issue.ID, nil
}

// buildIssueTitle creates a concise title for the health issue.
func buildIssueTitle(_ health.HealthMonitor, discovered health.DiscoveredIssue) string {
	// Extract the first sentence or first 80 chars of description
	desc := discovered.Description
	if idx := strings.Index(desc, "."); idx > 0 && idx < 80 {
		desc = desc[:idx]
	} else if len(desc) > 80 {
		desc = desc[:77] + "..."
	}

	return desc
}

// buildIssueDescription creates a detailed description for the health issue.
func buildIssueDescription(monitor health.HealthMonitor, discovered health.DiscoveredIssue) string {
	var sb strings.Builder

	sb.WriteString("## Health Monitor Finding\n\n")
	sb.WriteString(fmt.Sprintf("**Monitor:** %s\n", monitor.Name()))
	sb.WriteString(fmt.Sprintf("**Category:** %s\n", discovered.Category))
	sb.WriteString(fmt.Sprintf("**Severity:** %s\n\n", discovered.Severity))

	sb.WriteString("## Issue\n\n")
	sb.WriteString(discovered.Description)
	sb.WriteString("\n\n")

	if discovered.FilePath != "" {
		sb.WriteString("## Location\n\n")
		sb.WriteString(fmt.Sprintf("File: `%s`\n", discovered.FilePath))
		if discovered.LineStart > 0 {
			if discovered.LineEnd > 0 && discovered.LineEnd != discovered.LineStart {
				sb.WriteString(fmt.Sprintf("Lines: %d-%d\n", discovered.LineStart, discovered.LineEnd))
			} else {
				sb.WriteString(fmt.Sprintf("Line: %d\n", discovered.LineStart))
			}
		}
		sb.WriteString("\n")
	}

	// Add evidence if available
	if len(discovered.Evidence) > 0 {
		sb.WriteString("## Evidence\n\n")

		// Format common evidence fields
		if files, ok := discovered.Evidence["files_to_delete"].(int); ok {
			sb.WriteString(fmt.Sprintf("- Files to delete: %d\n", files))
		}
		if patterns, ok := discovered.Evidence["patterns_to_add"].(int); ok {
			sb.WriteString(fmt.Sprintf("- Patterns to add to .gitignore: %d\n", patterns))
		}
		if lines, ok := discovered.Evidence["lines"].(int); ok {
			sb.WriteString(fmt.Sprintf("- Line count: %d\n", lines))
		}
		if stdDevs, ok := discovered.Evidence["std_devs_above"].(float64); ok {
			sb.WriteString(fmt.Sprintf("- Standard deviations above mean: %.1f\n", stdDevs))
		}
		if issue, ok := discovered.Evidence["issue"].(string); ok && issue != "" {
			sb.WriteString(fmt.Sprintf("- Issue: %s\n", issue))
		}
		if split, ok := discovered.Evidence["suggested_split"].(string); ok && split != "" {
			sb.WriteString(fmt.Sprintf("- Suggested split: %s\n", split))
		}

		// Include cruft details if present
		if cruftToDelete, ok := discovered.Evidence["cruft_to_delete"].([]interface{}); ok && len(cruftToDelete) > 0 {
			sb.WriteString("\n### Files to Delete\n\n")
			for _, item := range cruftToDelete {
				if fileAction, ok := item.(map[string]interface{}); ok {
					file := fileAction["file"]
					reason := fileAction["reason"]
					sb.WriteString(fmt.Sprintf("- `%v`: %v\n", file, reason))
				}
			}
		}

		if patternsToIgnore, ok := discovered.Evidence["patterns_to_ignore"].([]interface{}); ok && len(patternsToIgnore) > 0 {
			sb.WriteString("\n### Patterns to Add to .gitignore\n\n")
			for _, pattern := range patternsToIgnore {
				sb.WriteString(fmt.Sprintf("- `%v`\n", pattern))
			}
		}

		sb.WriteString("\n")
	}

	sb.WriteString("## Philosophy\n\n")
	sb.WriteString(monitor.Philosophy())
	sb.WriteString("\n")

	return sb.String()
}
