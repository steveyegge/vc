package main

import (
	"context"
	"fmt"
	"os"

	"github.com/fatih/color"
	"github.com/spf13/cobra"
	"github.com/steveyegge/vc/internal/discovery"
	"github.com/steveyegge/vc/internal/storage"
)

var (
	initDiscover bool
	initPreset   string
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

With --discover flag, also runs discovery workers to bootstrap the issue tracker
with actionable issues found in the codebase.

Example:
  cd ~/myproject
  vc init                          # Creates .beads/myproject.db
  vc init myapp                    # Creates .beads/myapp.db
  vc init --discover               # Initialize and run discovery (standard preset)
  vc init --discover --preset=quick  # Initialize and run quick discovery`,
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

		// Run discovery if requested
		if initDiscover {
			fmt.Printf("%s Running discovery workers...\n\n", gray("→"))

			// Get preset
			preset := discovery.Preset(initPreset)
			if preset == "" {
				preset = discovery.PresetStandard
			}

			// Run discovery
			if err := runDiscovery(ctx, db, cwd, preset, false); err != nil {
				fmt.Fprintf(os.Stderr, "Warning: discovery failed: %v\n", err)
				fmt.Fprintf(os.Stderr, "The tracker was initialized successfully, but discovery encountered errors.\n")
			}

			fmt.Println()
		}

		fmt.Printf("%s Next steps:\n", gray("→"))
		if !initDiscover {
			fmt.Printf("  %s\n", gray("vc discover --preset=quick  # Discover issues in codebase"))
		}
		fmt.Printf("  %s\n", gray("vc ready"))
		fmt.Printf("  %s\n", gray("vc execute"))
		fmt.Println()
	},
}

func init() {
	rootCmd.AddCommand(initCmd)
	initCmd.Flags().BoolVar(&initDiscover, "discover", false, "Run discovery workers after initialization")
	initCmd.Flags().StringVar(&initPreset, "preset", "standard", "Discovery preset: quick, standard, or thorough")
}
