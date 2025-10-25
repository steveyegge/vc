package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/fatih/color"
	"github.com/spf13/cobra"
	"github.com/steveyegge/vc/internal/storage"
	"github.com/steveyegge/vc/internal/types"
)

var doctorCmd = &cobra.Command{
	Use:   "doctor",
	Short: "Check VC installation and environment health",
	Long: `Run health checks to diagnose common VC configuration and environment issues.

This command checks for:
- Database existence and accessibility
- Database staleness (sync with issues.jsonl)
- WAL mode timestamp sync issues
- Beads daemon conflicts
- Required environment variables
- Git repository status
- Sandbox directory permissions

Exit codes:
  0 - All checks passed
  1 - One or more checks failed (but not critical)
  2 - Critical failures that prevent VC from running`,
	Run: func(cmd *cobra.Command, args []string) {
		verbose, _ := cmd.Flags().GetBool("verbose")
		fixIssues, _ := cmd.Flags().GetBool("fix")

		green := color.New(color.FgGreen).SprintFunc()
		red := color.New(color.FgRed).SprintFunc()
		yellow := color.New(color.FgYellow).SprintFunc()
		cyan := color.New(color.FgCyan).SprintFunc()

		fmt.Printf("Running VC health checks...\n\n")

		var failures []string
		var warnings []string
		var criticalFailures []string

		// Check 1: Database discovery
		fmt.Printf("%s Database discovery\n", cyan("→"))
		if dbPath == "" {
			if discoveredPath, err := storage.DiscoverDatabase(); err != nil {
				criticalFailures = append(criticalFailures, fmt.Sprintf("No database found: %v", err))
				fmt.Printf("  %s No database found\n", red("✗"))
				if verbose {
					fmt.Printf("    Error: %v\n", err)
				}
			} else {
				dbPath = discoveredPath
				fmt.Printf("  %s Found database: %s\n", green("✓"), dbPath)
			}
		} else {
			fmt.Printf("  %s Using explicit database: %s\n", green("✓"), dbPath)
		}

		if dbPath == "" {
			fmt.Printf("\n%s Critical failures prevent VC from running\n", red("✗"))
			os.Exit(2)
		}

		// Check 2: Database file accessibility
		fmt.Printf("%s Database file access\n", cyan("→"))
		if info, err := os.Stat(dbPath); err != nil {
			criticalFailures = append(criticalFailures, fmt.Sprintf("Cannot access database: %v", err))
			fmt.Printf("  %s Cannot access database file\n", red("✗"))
			if verbose {
				fmt.Printf("    Error: %v\n", err)
			}
		} else {
			fmt.Printf("  %s Database file accessible (%d bytes)\n", green("✓"), info.Size())
			if info.Size() == 0 {
				warnings = append(warnings, "Database file is empty (0 bytes)")
				fmt.Printf("  %s WARNING: Database is empty\n", yellow("⚠"))
			}
		}

		// Check 3: Project root and alignment
		fmt.Printf("%s Project structure\n", cyan("→"))
		projectRoot, err := storage.GetProjectRoot(dbPath)
		if err != nil {
			failures = append(failures, fmt.Sprintf("Invalid project structure: %v", err))
			fmt.Printf("  %s Invalid project structure\n", red("✗"))
			if verbose {
				fmt.Printf("    Error: %v\n", err)
			}
		} else {
			fmt.Printf("  %s Project root: %s\n", green("✓"), projectRoot)

			cwd, _ := os.Getwd()
			if err := storage.ValidateAlignment(dbPath, cwd); err != nil {
				failures = append(failures, "Database-working directory mismatch")
				fmt.Printf("  %s Working directory not aligned with database\n", yellow("⚠"))
				if verbose {
					fmt.Printf("    Error: %v\n", err)
				}
			} else {
				fmt.Printf("  %s Working directory aligned\n", green("✓"))
			}
		}

		// Check 4: Database staleness (issues.jsonl sync)
		fmt.Printf("%s Database freshness\n", cyan("→"))
		if err := storage.ValidateDatabaseFreshness(dbPath); err != nil {
			if strings.Contains(err.Error(), "stale by") {
				failures = append(failures, "Database is stale (needs bd import)")
				fmt.Printf("  %s Database is out of sync with issues.jsonl\n", red("✗"))
				if verbose {
					fmt.Printf("    %v\n", err)
				}
				if fixIssues {
					fmt.Printf("  %s Running bd import to sync database...\n", cyan("→"))
					if err := runBdImport(projectRoot); err != nil {
						fmt.Printf("    %s Failed to import: %v\n", red("✗"), err)
					} else {
						fmt.Printf("    %s Database synced successfully\n", green("✓"))
						// Remove from failures list since we fixed it
						failures = failures[:len(failures)-1]
					}
				}
			} else {
				fmt.Printf("  %s Cannot check freshness: %v\n", yellow("⚠"), err)
			}
		} else {
			fmt.Printf("  %s Database is in sync with issues.jsonl\n", green("✓"))
		}

		// Check 5: WAL mode timestamp sync
		fmt.Printf("%s WAL mode status\n", cyan("→"))
		walPath := dbPath + "-wal"
		if walInfo, err := os.Stat(walPath); err == nil {
			dbInfo, _ := os.Stat(dbPath)
			walAge := time.Since(walInfo.ModTime())
			dbAge := time.Since(dbInfo.ModTime())

			fmt.Printf("  %s WAL mode detected\n", green("✓"))
			if verbose {
				fmt.Printf("    Main DB age: %v\n", dbAge.Round(time.Second))
				fmt.Printf("    WAL file age: %v\n", walAge.Round(time.Second))
			}

			// Check if WAL is much newer than main DB (indicates checkpoint needed)
			if walInfo.ModTime().Sub(dbInfo.ModTime()) > 5*time.Minute {
				warnings = append(warnings, "WAL file significantly newer than main DB (consider PRAGMA wal_checkpoint)")
				fmt.Printf("  %s WAL file significantly newer than main DB\n", yellow("⚠"))
			}
		} else {
			fmt.Printf("  %s WAL mode not active (using rollback journal)\n", green("✓"))
		}

		// Check 6: Beads daemon conflicts
		fmt.Printf("%s Beads daemon status\n", cyan("→"))
		if isBeadsDaemonRunning() {
			warnings = append(warnings, "Beads daemon is running (may conflict with VC)")
			fmt.Printf("  %s Beads daemon detected (may cause conflicts)\n", yellow("⚠"))
			fmt.Printf("    Recommendation: Stop bd daemon before running VC\n")
			fmt.Printf("    Command: pkill -f 'bd daemon'\n")
		} else {
			fmt.Printf("  %s No beads daemon detected\n", green("✓"))
		}

		// Check 7: Required environment variables
		fmt.Printf("%s Environment variables\n", cyan("→"))
		if apiKey := os.Getenv("ANTHROPIC_API_KEY"); apiKey == "" {
			failures = append(failures, "ANTHROPIC_API_KEY not set")
			fmt.Printf("  %s ANTHROPIC_API_KEY not set\n", red("✗"))
			fmt.Printf("    AI supervision features will not work\n")
		} else {
			fmt.Printf("  %s ANTHROPIC_API_KEY is set\n", green("✓"))
			if verbose {
				fmt.Printf("    Key: %s...%s\n", apiKey[:10], apiKey[len(apiKey)-4:])
			}
		}

		// Check 8: Git repository status
		fmt.Printf("%s Git repository\n", cyan("→"))
		if projectRoot != "" {
			gitDir := filepath.Join(projectRoot, ".git")
			if _, err := os.Stat(gitDir); err != nil {
				warnings = append(warnings, "Not a git repository")
				fmt.Printf("  %s Not a git repository\n", yellow("⚠"))
			} else {
				fmt.Printf("  %s Git repository detected\n", green("✓"))

				// Check for uncommitted changes
				cmd := exec.Command("git", "status", "--porcelain")
				cmd.Dir = projectRoot
				if output, err := cmd.Output(); err == nil {
					if len(output) > 0 {
						lines := strings.Split(strings.TrimSpace(string(output)), "\n")
						fmt.Printf("  %s Uncommitted changes detected (%d files)\n", yellow("⚠"), len(lines))
						if verbose {
							for i, line := range lines {
								if i >= 5 {
									fmt.Printf("    ... and %d more\n", len(lines)-5)
									break
								}
								fmt.Printf("    %s\n", line)
							}
						}
					} else {
						fmt.Printf("  %s Working directory clean\n", green("✓"))
					}
				}
			}
		}

		// Check 9: Sandbox directory
		fmt.Printf("%s Sandbox directory\n", cyan("→"))
		if projectRoot != "" {
			sandboxRoot := filepath.Join(projectRoot, ".sandboxes")
			if info, err := os.Stat(sandboxRoot); err == nil {
				if !info.IsDir() {
					warnings = append(warnings, ".sandboxes exists but is not a directory")
					fmt.Printf("  %s .sandboxes exists but is not a directory\n", yellow("⚠"))
				} else {
					// Check permissions
					if info.Mode().Perm()&0700 == 0700 {
						fmt.Printf("  %s Sandbox directory exists with correct permissions\n", green("✓"))
					} else {
						warnings = append(warnings, ".sandboxes has incorrect permissions")
						fmt.Printf("  %s Sandbox directory has incorrect permissions\n", yellow("⚠"))
						fmt.Printf("    Expected: drwx------ (0700), got: %v\n", info.Mode().Perm())
					}

					// Count existing sandboxes
					entries, _ := os.ReadDir(sandboxRoot)
					sandboxCount := 0
					for _, entry := range entries {
						if entry.IsDir() && strings.HasPrefix(entry.Name(), "mission-") {
							sandboxCount++
						}
					}
					if sandboxCount > 0 {
						fmt.Printf("  %s Found %d existing sandbox(es)\n", green("✓"), sandboxCount)
					}
				}
			} else {
				fmt.Printf("  %s Sandbox directory does not exist (will be created on first execution)\n", green("✓"))
			}
		}

		// Check 10: Database issue count
		fmt.Printf("%s Database statistics\n", cyan("→"))
		if projectRoot != "" {
			// Try to connect and get basic stats
			cfg := storage.DefaultConfig()
			cfg.Path = dbPath
			ctx := context.Background()
			if store, err := storage.NewStorage(ctx, cfg); err == nil {
				// Get issue count using SearchIssues with empty query
				issues, err := store.SearchIssues(ctx, "", types.IssueFilter{})
				if err != nil {
					warnings = append(warnings, fmt.Sprintf("Cannot query issues: %v", err))
					fmt.Printf("  %s Cannot query database\n", yellow("⚠"))
				} else {
					fmt.Printf("  %s Database contains %d issue(s)\n", green("✓"), len(issues))

					// Count by status
					statusCounts := make(map[string]int)
					for _, issue := range issues {
						statusCounts[string(issue.Status)]++
					}
					if verbose && len(issues) > 0 {
						for status, count := range statusCounts {
							fmt.Printf("    %s: %d\n", status, count)
						}
					}
				}
				store.Close()
			} else {
				failures = append(failures, fmt.Sprintf("Cannot connect to database: %v", err))
				fmt.Printf("  %s Cannot connect to database\n", red("✗"))
				if verbose {
					fmt.Printf("    Error: %v\n", err)
				}
			}
		}

		// Summary
		fmt.Printf("\n%s\n", strings.Repeat("─", 60))

		totalIssues := len(criticalFailures) + len(failures) + len(warnings)
		if totalIssues == 0 {
			fmt.Printf("%s All checks passed! VC is ready to run.\n", green("✓"))
			os.Exit(0)
		}

		if len(criticalFailures) > 0 {
			fmt.Printf("\n%s Critical failures (%d):\n", red("✗"), len(criticalFailures))
			for _, failure := range criticalFailures {
				fmt.Printf("  • %s\n", failure)
			}
		}

		if len(failures) > 0 {
			fmt.Printf("\n%s Failures (%d):\n", red("✗"), len(failures))
			for _, failure := range failures {
				fmt.Printf("  • %s\n", failure)
			}
		}

		if len(warnings) > 0 {
			fmt.Printf("\n%s Warnings (%d):\n", yellow("⚠"), len(warnings))
			for _, warning := range warnings {
				fmt.Printf("  • %s\n", warning)
			}
		}

		if len(criticalFailures) > 0 {
			fmt.Printf("\n%s VC cannot run until critical issues are resolved.\n", red("✗"))
			os.Exit(2)
		}

		if len(failures) > 0 {
			fmt.Printf("\n%s VC may not work correctly. Please address the failures above.\n", yellow("⚠"))
			os.Exit(1)
		}

		fmt.Printf("\n%s VC should work, but some warnings were detected.\n", green("✓"))
		os.Exit(0)
	},
}

func init() {
	doctorCmd.Flags().BoolP("verbose", "v", false, "Show detailed diagnostic information")
	doctorCmd.Flags().Bool("fix", false, "Attempt to automatically fix common issues")
	rootCmd.AddCommand(doctorCmd)
}

// isBeadsDaemonRunning checks if any bd daemon processes are running
func isBeadsDaemonRunning() bool {
	cmd := exec.Command("pgrep", "-f", "bd daemon")
	err := cmd.Run()
	return err == nil // pgrep returns 0 if processes found
}

// runBdImport runs bd import to sync the database with issues.jsonl
func runBdImport(projectRoot string) error {
	jsonlPath := filepath.Join(projectRoot, ".beads", "issues.jsonl")
	cmd := exec.Command("bd", "--no-daemon", "--db", dbPath, "import", jsonlPath)
	cmd.Dir = projectRoot
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%w: %s", err, string(output))
	}
	return nil
}
