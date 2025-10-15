package repl

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/chzyer/readline"
	"github.com/fatih/color"
	"github.com/steveyegge/vc/internal/storage"
)

// REPL represents the interactive shell
type REPL struct {
	store    storage.Storage
	rl       *readline.Instance
	ctx      context.Context
	actor    string
	commands map[string]CommandHandler
}

// CommandHandler handles a specific command
type CommandHandler func(args []string) error

// Config holds REPL configuration
type Config struct {
	Store storage.Storage
	Actor string
}

// New creates a new REPL instance
func New(cfg *Config) (*REPL, error) {
	if cfg.Store == nil {
		return nil, fmt.Errorf("storage is required")
	}

	actor := cfg.Actor
	if actor == "" {
		actor = "user"
	}

	r := &REPL{
		store:    cfg.Store,
		actor:    actor,
		commands: make(map[string]CommandHandler),
	}

	// Register built-in commands
	r.registerCommands()

	return r, nil
}

// Run starts the REPL loop
func (r *REPL) Run(ctx context.Context) error {
	r.ctx = ctx

	// Create readline instance
	cyan := color.New(color.FgCyan).SprintFunc()
	prompt := cyan("vc> ")

	rl, err := readline.NewEx(&readline.Config{
		Prompt:                 prompt,
		HistoryFile:            "", // In-memory for now, will persist later
		AutoComplete:           nil, // Will add tab completion later
		InterruptPrompt:        "^C",
		EOFPrompt:              "exit",
		HistorySearchFold:      true,
		FuncFilterInputRune:    nil,
		ForceUseInteractive:    false,
		DisableAutoSaveHistory: false,
	})
	if err != nil {
		return fmt.Errorf("failed to create readline: %w", err)
	}
	defer rl.Close()

	r.rl = rl

	// Print welcome message
	r.printWelcome()

	// Main loop
	for {
		line, err := rl.Readline()
		if err != nil {
			if err == readline.ErrInterrupt {
				// Ctrl+C - just show prompt again
				continue
			} else if err == io.EOF {
				// Ctrl+D - exit
				fmt.Println("\nGoodbye!")
				return nil
			}
			return err
		}

		// Process the input
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		if err := r.processInput(line); err != nil {
			if err == io.EOF {
				// Exit command - graceful shutdown
				return nil
			}
			red := color.New(color.FgRed).SprintFunc()
			fmt.Printf("%s %v\n", red("Error:"), err)
		}
	}
}

// processInput processes a single line of input
func (r *REPL) processInput(line string) error {
	parts := strings.Fields(line)
	if len(parts) == 0 {
		return nil
	}

	command := parts[0]
	args := parts[1:]

	// Check if it's a registered command
	if handler, ok := r.commands[command]; ok {
		return handler(args)
	}

	// If not a command, treat as natural language (to be implemented later)
	yellow := color.New(color.FgYellow).SprintFunc()
	fmt.Printf("%s Natural language processing not yet implemented. Use 'help' for available commands.\n", yellow("Note:"))
	return nil
}

// registerCommands registers all built-in commands
func (r *REPL) registerCommands() {
	r.commands["help"] = r.cmdHelp
	r.commands["?"] = r.cmdHelp
	r.commands["exit"] = r.cmdExit
	r.commands["quit"] = r.cmdExit
}

// printWelcome prints the welcome message
func (r *REPL) printWelcome() {
	cyan := color.New(color.FgCyan, color.Bold).SprintFunc()
	fmt.Printf("\n%s\n", cyan("Welcome to VC - VibeCoder v2"))
	fmt.Println("AI-orchestrated coding agent colony")
	fmt.Println()
	fmt.Println("Type 'help' for available commands, 'exit' to quit")
	fmt.Println()
}

// cmdHelp shows help information
func (r *REPL) cmdHelp(args []string) error {
	cyan := color.New(color.FgCyan, color.Bold).SprintFunc()
	fmt.Printf("\n%s\n", cyan("Available Commands:"))
	fmt.Println()

	commands := []struct {
		name string
		desc string
	}{
		{"help, ?", "Show this help message"},
		{"exit, quit", "Exit the REPL"},
		{"", ""},
		{"Coming soon:", ""},
		{"  continue", "Resume execution from tracker state"},
		{"  status", "Show project status (ready/blocked/in-progress)"},
		{"  ready", "Show ready work"},
		{"  blocked", "Show blocked issues"},
	}

	for _, cmd := range commands {
		if cmd.name == "" {
			fmt.Println(cmd.desc)
		} else if strings.HasPrefix(cmd.desc, "Coming soon") {
			yellow := color.New(color.FgYellow).SprintFunc()
			fmt.Printf("  %s\n", yellow(cmd.desc))
		} else if strings.HasPrefix(cmd.name, " ") {
			fmt.Printf("  %s  %s\n", cmd.name, cmd.desc)
		} else {
			green := color.New(color.FgGreen).SprintFunc()
			fmt.Printf("  %s  %s\n", green(cmd.name), cmd.desc)
		}
	}

	fmt.Println()
	fmt.Println("Natural language input (coming soon):")
	fmt.Println("  'Add a login page'")
	fmt.Println("  'Fix the bug in auth.go'")
	fmt.Println("  'Build authentication system'")
	fmt.Println()

	return nil
}

// cmdExit exits the REPL
func (r *REPL) cmdExit(args []string) error {
	green := color.New(color.FgGreen).SprintFunc()
	fmt.Printf("\n%s Goodbye!\n", green("✓"))
	r.rl.Close()
	return io.EOF // Signal to exit the loop
}
