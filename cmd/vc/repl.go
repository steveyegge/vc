package main

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/steveyegge/vc/internal/repl"
	"github.com/steveyegge/vc/internal/storage"
)

var replCmd = &cobra.Command{
	Use:   "repl",
	Short: "Start interactive REPL shell",
	Long: `Start an interactive REPL (Read-Eval-Print Loop) shell for VC.

The REPL provides a natural language interface for:
- Creating issues from plain English
- Viewing project status
- Resuming execution with 'let's continue'
- Managing the issue tracker

Type 'help' in the REPL for available commands.`,
	Run: func(cmd *cobra.Command, args []string) {
		// Validate alignment between database and working directory
		cwd, _ := os.Getwd()
		if err := storage.ValidateAlignment(dbPath, cwd); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}

		// Create REPL configuration
		cfg := &repl.Config{
			Store: store,
			Actor: actor,
		}

		// Create REPL instance
		r, err := repl.New(cfg)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: failed to create REPL: %v\n", err)
			os.Exit(1)
		}

		// Run the REPL
		ctx := context.Background()
		if err := r.Run(ctx); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
	},
}

func init() {
	rootCmd.AddCommand(replCmd)
}
