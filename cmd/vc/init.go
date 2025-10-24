package main

import (
	"context"
	"fmt"
	"os"

	"github.com/fatih/color"
	"github.com/spf13/cobra"
	"github.com/steveyegge/vc/internal/storage"
)

var initCmd = &cobra.Command{
	Use:   "init [project-name]",
	Short: "Initialize a new VC tracker in the current directory",
	Long: `Initialize a new VC tracker by creating a .beads/ directory with a database.

This creates:
  - .beads/ directory
  - .beads/<project-name>.db (SQLite database)
  - .beads/issues.jsonl (empty JSONL file for git commits)

If no project name is provided, the current directory name is used.

Example:
  cd ~/myproject
  vc init           # Creates .beads/myproject.db
  vc init myapp     # Creates .beads/myapp.db`,
	Args: cobra.MaximumNArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		// Get project name from args or use current directory name
		projectName := ""
		if len(args) > 0 {
			projectName = args[0]
		}

		// Get current directory
		cwd, err := os.Getwd()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: failed to get current directory: %v\n", err)
			os.Exit(1)
		}

		// Initialize project
		dbPath, err := storage.InitProject(cwd, projectName)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}

		// Initialize the database schema by opening and closing it
		ctx := context.Background()
		db, err := storage.NewStorage(ctx, &storage.Config{Path: dbPath})
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: failed to initialize database: %v\n", err)
			os.Exit(1)
		}
		_ = db.Close() // Ignore close error during initialization

		green := color.New(color.FgGreen).SprintFunc()
		cyan := color.New(color.FgCyan).SprintFunc()
		gray := color.New(color.FgHiBlack).SprintFunc()

		fmt.Printf("\n%s Initialized VC tracker\n\n", green("✓"))
		fmt.Printf("  Database: %s\n", cyan(dbPath))
		fmt.Printf("  Project root: %s\n", cyan(cwd))
		fmt.Println()
		fmt.Printf("%s Next steps:\n", gray("→"))
		fmt.Printf("  %s\n", gray("vc create \"My first issue\" -t task"))
		fmt.Printf("  %s\n", gray("vc ready"))
		fmt.Printf("  %s\n", gray("vc execute"))
		fmt.Println()
	},
}

func init() {
	rootCmd.AddCommand(initCmd)
}
