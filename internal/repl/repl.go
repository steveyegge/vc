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
	store        storage.Storage
	rl           *readline.Instance
	ctx          context.Context
	actor        string
	commands     map[string]CommandHandler
	conversation *ConversationHandler
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
	// Check if it's a slash command
	if strings.HasPrefix(line, "/") {
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

		// Unknown slash command
		red := color.New(color.FgRed).SprintFunc()
		return fmt.Errorf("%s: unknown command. Type /help for available commands", red(command))
	}

	// Not a slash command - send to AI conversation handler
	return r.processNaturalLanguage(line)
}

// registerCommands registers all built-in commands
func (r *REPL) registerCommands() {
	r.commands["/help"] = r.cmdHelp
	r.commands["/?"] = r.cmdHelp
	r.commands["/exit"] = r.cmdExit
	r.commands["/quit"] = r.cmdExit
	r.commands["/status"] = r.cmdStatus
	r.commands["/ready"] = r.cmdReady
	r.commands["/blocked"] = r.cmdBlocked
	r.commands["/continue"] = r.cmdContinue
}

// printWelcome prints the welcome message
func (r *REPL) printWelcome() {
	cyan := color.New(color.FgCyan, color.Bold).SprintFunc()
	gray := color.New(color.FgHiBlack).SprintFunc()
	fmt.Printf("\n%s\n", cyan("Welcome to VC - VibeCoder v2"))
	fmt.Println("AI-orchestrated coding agent colony")
	fmt.Println()
	fmt.Printf("Slash commands start with %s (type %s for list)\n", gray("/"), cyan("/help"))
	fmt.Println("Everything else is sent to AI for natural language processing")
	fmt.Println()
}

// cmdHelp shows help information
func (r *REPL) cmdHelp(args []string) error {
	cyan := color.New(color.FgCyan, color.Bold).SprintFunc()
	green := color.New(color.FgGreen).SprintFunc()
	gray := color.New(color.FgHiBlack).SprintFunc()

	fmt.Printf("\n%s\n", cyan("VC REPL Commands"))
	fmt.Println()
	fmt.Printf("%s\n", cyan("Slash Commands:"))
	fmt.Printf("  %s          Show this help\n", green("/help"))
	fmt.Printf("  %s        Show project status\n", green("/status"))
	fmt.Printf("  %s         Show ready work\n", green("/ready"))
	fmt.Printf("  %s       Show blocked issues\n", green("/blocked"))
	fmt.Printf("  %s      Resume execution - claim and execute ready work\n", green("/continue"))
	fmt.Printf("  %s          Exit the REPL\n", green("/exit"))
	fmt.Println()

	fmt.Printf("%s\n", cyan("Natural Language:"))
	fmt.Println("  Type anything without a / to talk to the AI")
	fmt.Printf("  %s\n", gray("Examples:"))
	fmt.Printf("    %s\n", gray("Add a login page"))
	fmt.Printf("    %s\n", gray("What issues are blocked?"))
	fmt.Printf("    %s\n", gray("Fix the authentication bug"))
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
