package repl

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/chzyer/readline"
	"github.com/fatih/color"
	"github.com/steveyegge/vc/internal/storage"
	"github.com/steveyegge/vc/internal/types"
)

// REPL represents the interactive shell
type REPL struct {
	store          storage.Storage
	rl             *readline.Instance
	ctx            context.Context
	actor          string
	conversation   *ConversationHandler
	pendingPlans   map[string]*types.MissionPlan // Mission plans awaiting approval
	plansMu        sync.RWMutex                   // Protects pendingPlans map
	stopHeartbeat  chan struct{}                  // Signal to stop heartbeat goroutine
	stopCleanup    chan struct{}                  // Signal to stop cleanup goroutine
	instanceID     string                         // Executor instance ID for this REPL
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
		store:         cfg.Store,
		actor:         actor,
		pendingPlans:  make(map[string]*types.MissionPlan),
		stopHeartbeat: make(chan struct{}),
		stopCleanup:   make(chan struct{}),
	}

	return r, nil
}

// Run starts the REPL loop
func (r *REPL) Run(ctx context.Context) error {
	r.ctx = ctx

	// Register this REPL as an executor instance
	if err := r.registerExecutorInstance(ctx); err != nil {
		return fmt.Errorf("failed to register executor instance: %w", err)
	}

	// Start heartbeat goroutine to keep executor instance alive
	go r.heartbeatLoop(ctx)
	defer func() {
		close(r.stopHeartbeat) // Signal heartbeat to stop
	}()

	// Start cleanup goroutine to clean up stale executor instances
	go r.cleanupLoop(ctx)
	defer func() {
		close(r.stopCleanup) // Signal cleanup to stop
	}()

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
	defer func() {
		if err := rl.Close(); err != nil {
			fmt.Fprintf(os.Stderr, "warning: failed to close readline: %v\n", err)
		}
	}()

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
func (r *REPL) cmdExit(_ []string) error {
	green := color.New(color.FgGreen).SprintFunc()
	fmt.Printf("\n%s Goodbye!\n", green("✓"))
	if err := r.rl.Close(); err != nil {
		fmt.Fprintf(os.Stderr, "warning: failed to close readline: %v\n", err)
	}
	return io.EOF // Signal to exit the loop
}

// registerExecutorInstance registers the REPL as an executor instance in the database
func (r *REPL) registerExecutorInstance(ctx context.Context) error {
	hostname, err := os.Hostname()
	if err != nil {
		hostname = "unknown"
	}

	r.instanceID = fmt.Sprintf("conversation-%s", r.actor)

	instance := &types.ExecutorInstance{
		InstanceID:    r.instanceID,
		Hostname:      hostname,
		PID:           os.Getpid(),
		Status:        types.ExecutorStatusRunning,
		StartedAt:     time.Now(),
		LastHeartbeat: time.Now(),
		Version:       "v2",
		Metadata:      `{"type":"repl","mode":"interactive"}`,
	}

	return r.store.RegisterInstance(ctx, instance)
}

// heartbeatLoop periodically updates the executor instance heartbeat
func (r *REPL) heartbeatLoop(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-r.stopHeartbeat:
			return
		case <-ticker.C:
			if err := r.store.UpdateHeartbeat(ctx, r.instanceID); err != nil {
				fmt.Fprintf(os.Stderr, "warning: failed to update heartbeat: %v\n", err)
			}
		}
	}
}

// cleanupLoop periodically cleans up stale executor instances
// Runs every 5 minutes and releases issues claimed by dead executors
func (r *REPL) cleanupLoop(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	staleThreshold := int((5 * time.Minute).Seconds()) // 5 minutes = 300 seconds

	for {
		select {
		case <-ctx.Done():
			return
		case <-r.stopCleanup:
			return
		case <-ticker.C:
			cleaned, err := r.store.CleanupStaleInstances(ctx, staleThreshold)
			if err != nil {
				fmt.Fprintf(os.Stderr, "warning: failed to cleanup stale instances: %v\n", err)
			} else if cleaned > 0 {
				fmt.Printf("Cleanup: Cleaned up %d stale executor instance(s)\n", cleaned)
			}
		}
	}
}
