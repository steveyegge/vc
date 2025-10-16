package repl

import (
	"context"
	"fmt"
	"io"
	"strings"
	"sync"

	"github.com/chzyer/readline"
	"github.com/fatih/color"
	"github.com/steveyegge/vc/internal/storage"
	"github.com/steveyegge/vc/internal/types"
)

// REPL represents the interactive shell
type REPL struct {
	store        storage.Storage
	rl           *readline.Instance
	ctx          context.Context
	actor        string
	conversation *ConversationHandler
	pendingPlans map[string]*types.MissionPlan // Mission plans awaiting approval
	plansMu      sync.RWMutex                   // Protects pendingPlans map
}

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
		store:        cfg.Store,
		actor:        actor,
		pendingPlans: make(map[string]*types.MissionPlan),
	}

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
	// Only intercept /quit and /exit - everything else goes to AI
	if line == "/quit" || line == "/exit" {
		return r.cmdExit(nil)
	}

	// Send everything else to AI conversation handler
	return r.processNaturalLanguage(line)
}

// printWelcome prints the welcome message
func (r *REPL) printWelcome() {
	cyan := color.New(color.FgCyan, color.Bold).SprintFunc()
	gray := color.New(color.FgHiBlack).SprintFunc()
	green := color.New(color.FgGreen).SprintFunc()
	fmt.Printf("\n%s\n", cyan("Welcome to VC - VibeCoder v2"))
	fmt.Println("AI-orchestrated coding agent colony")
	fmt.Println()
	fmt.Printf("%s No commands to memorize - just talk naturally!\n", green("✓"))
	fmt.Println()
	fmt.Println("Example interactions:")
	fmt.Printf("  %s\n", gray("• \"What's ready to work on?\""))
	fmt.Printf("  %s\n", gray("• \"Let's continue working\""))
	fmt.Printf("  %s\n", gray("• \"Add a login feature\""))
	fmt.Printf("  %s\n", gray("• \"Show me what's blocked\""))
	fmt.Printf("  %s\n", gray("• \"How's the project doing?\""))
	fmt.Printf("  %s\n", gray("• \"Create an epic for user auth\""))
	fmt.Println()
	fmt.Printf("Type %s or %s to exit\n", cyan("/quit"), cyan("/exit"))
	fmt.Println()
}

// cmdExit exits the REPL
func (r *REPL) cmdExit(args []string) error {
	green := color.New(color.FgGreen).SprintFunc()
	fmt.Printf("\n%s Goodbye!\n", green("✓"))
	r.rl.Close()
	return io.EOF // Signal to exit the loop
}
