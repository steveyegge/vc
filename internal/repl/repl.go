package repl

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
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
	rlClosed       bool       // Track if readline has been closed
	rlMu           sync.Mutex // Protects rl and rlClosed
	ctx            context.Context
	actor          string
	conversation   *ConversationHandler
	pendingPlans   map[string]*types.MissionPlan // Mission plans awaiting approval
	plansMu        sync.RWMutex                   // Protects pendingPlans map
	stopHeartbeat  chan struct{}                  // Signal to stop heartbeat goroutine
	stopCleanup    chan struct{}                  // Signal to stop cleanup goroutine
	instanceID     string                         // Executor instance ID for this REPL
	ctrlCCount     int                            // Track Ctrl+C presses to show hint only once
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

// closeReadline safely closes the readline instance (idempotent)
func (r *REPL) closeReadline() error {
	r.rlMu.Lock()
	defer r.rlMu.Unlock()

	if r.rlClosed || r.rl == nil {
		return nil
	}

	r.rlClosed = true
	return r.rl.Close()
}

// getHistoryPath returns the path to the history file
func getHistoryPath() (string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to get home directory: %w", err)
	}

	vcDir := filepath.Join(homeDir, ".vc")

	// Create .vc directory if it doesn't exist
	if err := os.MkdirAll(vcDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create .vc directory: %w", err)
	}

	return filepath.Join(vcDir, "repl_history"), nil
}

// dynamicCompleter implements readline.AutoCompleter interface for dynamic, context-aware completions
type dynamicCompleter struct {
	store         storage.Storage
	ctx           context.Context
	lastUpdate    time.Time
	cachedIssues  []string // Cached issue IDs from ready work
	historyPath   string
	cacheDuration time.Duration
}

// newDynamicCompleter creates a new dynamic auto-completer
func newDynamicCompleter(ctx context.Context, store storage.Storage, historyPath string) *dynamicCompleter {
	return &dynamicCompleter{
		store:         store,
		ctx:           ctx,
		historyPath:   historyPath,
		cacheDuration: 30 * time.Second, // Refresh ready work every 30 seconds
	}
}

// Do implements the AutoCompleter interface for dynamic tab completion
func (d *dynamicCompleter) Do(line []rune, pos int) (newLine [][]rune, length int) {
	lineStr := string(line[:pos])
	
	// Get all possible completions
	completions := d.getCompletions(lineStr)
	
	// Filter completions that match the current input
	var matches []string
	for _, completion := range completions {
		if strings.HasPrefix(completion, lineStr) && completion != lineStr {
			matches = append(matches, completion)
		}
	}
	
	// Convert matches to the format readline expects
	if len(matches) == 0 {
		return nil, 0
	}
	
	// Sort matches for consistent ordering
	sortCompletions(matches)
	
	// Convert to [][]rune
	var result [][]rune
	for _, match := range matches {
		result = append(result, []rune(match))
	}
	
	return result, pos
}

// getCompletions returns all possible completions based on context
func (d *dynamicCompleter) getCompletions(prefix string) []string {
	completions := make(map[string]bool)
	
	// 1. Slash commands (always available)
	slashCommands := []string{"/quit", "/exit", "/help"}
	for _, cmd := range slashCommands {
		completions[cmd] = true
	}
	
	// 2. Issue IDs from ready work (Phase 1)
	if time.Since(d.lastUpdate) > d.cacheDuration {
		d.refreshReadyWork()
	}
	for _, issueID := range d.cachedIssues {
		completions[issueID] = true
	}
	
	// 3. Common natural language starters (static baseline)
	naturalStarters := []string{
		"What's ",
		"What's ready to work on?",
		"What's blocked?",
		"Let's ",
		"Let's continue",
		"Show ",
		"Show me what's blocked",
		"Show ready work",
		"Create ",
		"Add ",
		"How ",
		"Continue",
		"Continue until blocked",
	}
	for _, starter := range naturalStarters {
		completions[starter] = true
	}
	
	// 4. History-based completions (Phase 2)
	historyCompletions := d.getHistoryBasedCompletions(prefix)
	for _, comp := range historyCompletions {
		completions[comp] = true
	}
	
	// 5. Context-aware suggestions (Phase 3)
	contextSuggestions := d.getContextAwareSuggestions(prefix)
	for _, sug := range contextSuggestions {
		completions[sug] = true
	}
	
	// 6. Fuzzy matching (Phase 4) - expand matches based on prefix patterns
	fuzzyMatches := d.getFuzzyMatches(prefix, completions)
	for _, match := range fuzzyMatches {
		completions[match] = true
	}
	
	// Convert map to slice
	var result []string
	for comp := range completions {
		result = append(result, comp)
	}
	
	return result
}

// refreshReadyWork updates the cached list of ready work issue IDs
func (d *dynamicCompleter) refreshReadyWork() {
	ctx, cancel := context.WithTimeout(d.ctx, 50*time.Millisecond)
	defer cancel()
	
	filter := types.WorkFilter{
		Status: types.StatusOpen,
		Limit:  20, // Keep top 20 ready issues for completion
	}
	
	issues, err := d.store.GetReadyWork(ctx, filter)
	if err != nil {
		// Silently fail - don't disrupt tab completion
		return
	}
	
	d.cachedIssues = nil
	for _, issue := range issues {
		d.cachedIssues = append(d.cachedIssues, issue.ID)
	}
	d.lastUpdate = time.Now()
}

// getHistoryBasedCompletions analyzes command history for common patterns
func (d *dynamicCompleter) getHistoryBasedCompletions(prefix string) []string {
	if d.historyPath == "" {
		return nil
	}
	
	// Read history file
	data, err := os.ReadFile(d.historyPath)
	if err != nil {
		return nil
	}
	
	lines := strings.Split(string(data), "\n")
	
	// Count frequency of commands
	frequency := make(map[string]int)
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "/") {
			continue // Skip empty lines and slash commands
		}
		if prefix != "" && !strings.HasPrefix(line, prefix) {
			continue // Filter by prefix if provided
		}
		frequency[line]++
	}
	
	// Return top 10 most frequent commands
	type freqPair struct {
		cmd   string
		count int
	}
	var pairs []freqPair
	for cmd, count := range frequency {
		if count >= 2 { // Only suggest commands used at least twice
			pairs = append(pairs, freqPair{cmd, count})
		}
	}
	
	// Sort by frequency
	for i := 0; i < len(pairs); i++ {
		for j := i + 1; j < len(pairs); j++ {
			if pairs[j].count > pairs[i].count {
				pairs[i], pairs[j] = pairs[j], pairs[i]
			}
		}
	}
	
	// Take top 10
	var result []string
	for i := 0; i < len(pairs) && i < 10; i++ {
		result = append(result, pairs[i].cmd)
	}
	
	return result
}

// getContextAwareSuggestions provides suggestions based on conversation context
func (d *dynamicCompleter) getContextAwareSuggestions(prefix string) []string {
	// For now, provide intelligent follow-up suggestions based on common workflows
	suggestions := []string{
		"start working on it",
		"what dependencies does it have?",
		"show me the design",
		"add a task",
		"mark it as blocked",
		"close it",
		"what's the status?",
	}
	
	// TODO: In future, track actual conversation state to provide smarter suggestions
	// For example, if last query was "What's ready?", suggest "Let's work on vc-XXX"
	
	return suggestions
}

// getFuzzyMatches performs fuzzy/smart prefix matching
func (d *dynamicCompleter) getFuzzyMatches(prefix string, existing map[string]bool) []string {
	if len(prefix) < 2 {
		return nil
	}
	
	lowerPrefix := strings.ToLower(prefix)
	var matches []string
	
	// Fuzzy match patterns
	fuzzyMappings := map[string][]string{
		"cont": {"Continue", "Continue until blocked", "Let's continue"},
		"show": {"Show ready work", "Show me what's blocked"},
		"what": {"What's ready to work on?", "What's blocked?"},
		"read": {"What's ready to work on?"},
		"bloc": {"What's blocked?", "Show me what's blocked"},
		"creat": {"Create a feature", "Create a bug"},
	}
	
	// Check if prefix matches any fuzzy pattern
	for pattern, expansions := range fuzzyMappings {
		if strings.HasPrefix(pattern, lowerPrefix) || strings.HasPrefix(lowerPrefix, pattern) {
			for _, expansion := range expansions {
				if !existing[expansion] {
					matches = append(matches, expansion)
				}
			}
		}
	}
	
	return matches
}

// sortCompletions sorts completions in a smart order:
// 1. Slash commands first
// 2. Issue IDs (vc-xxx)
// 3. Natural language (alphabetically)
func sortCompletions(completions []string) {
	// Simple bubble sort with custom comparison
	for i := 0; i < len(completions); i++ {
		for j := i + 1; j < len(completions); j++ {
			if shouldSwap(completions[i], completions[j]) {
				completions[i], completions[j] = completions[j], completions[i]
			}
		}
	}
}

// shouldSwap returns true if a should come after b
func shouldSwap(a, b string) bool {
	// Slash commands first
	aIsSlash := strings.HasPrefix(a, "/")
	bIsSlash := strings.HasPrefix(b, "/")
	if aIsSlash && !bIsSlash {
		return false
	}
	if !aIsSlash && bIsSlash {
		return true
	}
	
	// Issue IDs second (vc-xxx pattern)
	aIsIssue := strings.HasPrefix(a, "vc-")
	bIsIssue := strings.HasPrefix(b, "vc-")
	if aIsIssue && !bIsIssue {
		return false
	}
	if !aIsIssue && bIsIssue {
		return true
	}
	
	// Otherwise alphabetical
	return a > b
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

	// Set up signal handler for graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGTERM, syscall.SIGINT)
	go func() {
		sig := <-sigChan
		green := color.New(color.FgGreen).SprintFunc()
		fmt.Printf("\n%s Received signal %v, shutting down gracefully...\n", green("✓"), sig)

		// Close readline to interrupt the Readline() call
		if err := r.closeReadline(); err != nil {
			fmt.Fprintf(os.Stderr, "warning: failed to close readline: %v\n", err)
		}

		// Mark executor instance as stopped
		if err := r.store.MarkInstanceStopped(context.Background(), r.instanceID); err != nil {
			fmt.Fprintf(os.Stderr, "warning: failed to mark instance as stopped: %v\n", err)
		}

		os.Exit(0)
	}()

	// Get history file path
	historyPath, err := getHistoryPath()
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: failed to get history path: %v\n", err)
		historyPath = "" // Fall back to in-memory history
	}

	// Create dynamic auto-completer
	completer := newDynamicCompleter(ctx, r.store, historyPath)

	// Create readline instance
	cyan := color.New(color.FgCyan).SprintFunc()
	prompt := cyan("vc> ")

	rl, err := readline.NewEx(&readline.Config{
		Prompt:                 prompt,
		HistoryFile:            historyPath,
		HistoryLimit:           1000, // Keep last 1000 commands to prevent unbounded growth
		AutoComplete:           completer,
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
		if err := r.closeReadline(); err != nil {
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
				// Ctrl+C - show hint only on first press to reduce noise
				r.ctrlCCount++
				if r.ctrlCCount == 1 {
					gray := color.New(color.FgHiBlack).SprintFunc()
					fmt.Printf("%s (use /quit or /exit to leave)\n", gray("^C"))
				}
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
	// Intercept slash commands
	switch line {
	case "/quit", "/exit":
		return r.cmdExit(nil)
	case "/help":
		r.printHelp()
		return nil
	}

	// Send everything else to AI conversation handler
	return r.processNaturalLanguage(line)
}

// printWelcome prints the welcome message
func (r *REPL) printWelcome() {
	cyan := color.New(color.FgCyan, color.Bold).SprintFunc()
	gray := color.New(color.FgHiBlack).SprintFunc()
	green := color.New(color.FgGreen).SprintFunc()
	yellow := color.New(color.FgYellow).SprintFunc()

	fmt.Println()
	fmt.Printf("%s\n", cyan("╔══════════════════════════════════════════════════════════╗"))
	fmt.Printf("%s\n", cyan("║          Welcome to VC - VibeCoder v2                    ║"))
	fmt.Printf("%s\n", cyan("║      AI-orchestrated coding agent colony                 ║"))
	fmt.Printf("%s\n", cyan("╚══════════════════════════════════════════════════════════╝"))
	fmt.Println()

	fmt.Printf("%s No commands to memorize - just talk naturally!\n", green("✓"))
	fmt.Println()

	fmt.Printf("%s\n", yellow("Quick Start:"))
	fmt.Printf("  %s\n", gray("• \"What's ready to work on?\"    - See available issues"))
	fmt.Printf("  %s\n", gray("• \"Let's continue\"              - Resume execution"))
	fmt.Printf("  %s\n", gray("• \"Continue until blocked\"      - Run until no ready work"))
	fmt.Printf("  %s\n", gray("• \"Show me what's blocked\"      - View blocked issues"))
	fmt.Printf("  %s\n", gray("• \"Create a feature for X\"      - Add new work"))
	fmt.Println()

	fmt.Printf("%s\n", yellow("Features:"))
	fmt.Printf("  %s Smart tab completion (issue IDs, commands, history)\n", green("✓"))
	fmt.Printf("  %s Persistent command history across sessions\n", green("✓"))
	fmt.Printf("  %s Press Ctrl+C to cancel current input (not exit)\n", green("✓"))
	fmt.Printf("  %s Use Up/Down arrows to navigate command history\n", green("✓"))
	fmt.Println()

	fmt.Printf("%s Type %s or %s to exit • Press %s for help\n",
		gray("━"), cyan("/quit"), cyan("/exit"), cyan("Tab"))
	fmt.Println()
}

// printHelp prints the help message
func (r *REPL) printHelp() {
	cyan := color.New(color.FgCyan, color.Bold).SprintFunc()
	gray := color.New(color.FgHiBlack).SprintFunc()
	green := color.New(color.FgGreen).SprintFunc()
	yellow := color.New(color.FgYellow).SprintFunc()

	fmt.Println()
	fmt.Printf("%s\n", cyan("VC REPL Help"))
	fmt.Println(strings.Repeat("─", 60))
	fmt.Println()

	fmt.Printf("%s\n", yellow("Slash Commands:"))
	fmt.Printf("  %s  - Display this help message\n", cyan("/help"))
	fmt.Printf("  %s  - Exit the REPL\n", cyan("/quit"))
	fmt.Printf("  %s  - Exit the REPL\n", cyan("/exit"))
	fmt.Println()

	fmt.Printf("%s\n", yellow("Natural Language Queries:"))
	fmt.Printf("  %s\n", gray("• \"What's ready to work on?\""))
	fmt.Printf("  %s\n", gray("• \"Let's continue\""))
	fmt.Printf("  %s\n", gray("• \"Continue until blocked\""))
	fmt.Printf("  %s\n", gray("• \"Show me what's blocked\""))
	fmt.Printf("  %s\n", gray("• \"Create a feature for X\""))
	fmt.Println()

	fmt.Printf("%s\n", yellow("Session Information:"))
	fmt.Printf("  Actor:    %s\n", green(r.actor))
	fmt.Printf("  Instance: %s\n", gray(r.instanceID))
	fmt.Println()

	fmt.Printf("%s\n", yellow("Tips:"))
	fmt.Printf("  %s Press %s for auto-completion of commands and phrases\n", green("✓"), cyan("Tab"))
	fmt.Printf("  %s Use %s and %s to navigate command history\n", green("✓"), cyan("Up"), cyan("Down"))
	fmt.Printf("  %s Press %s to cancel current input (not exit)\n", green("✓"), cyan("Ctrl+C"))
	fmt.Printf("  %s Press %s or type %s to exit\n", green("✓"), cyan("Ctrl+D"), cyan("/quit"))
	fmt.Println()
}

// cmdExit exits the REPL
func (r *REPL) cmdExit(_ []string) error {
	green := color.New(color.FgGreen).SprintFunc()
	fmt.Printf("\n%s Goodbye!\n", green("✓"))
	// Don't close readline here - the defer in Run() will handle it
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
			} else if cleaned > 3 {
				// Only show cleanup messages for significant counts (> 3)
				// Use subdued gray color with timestamp to avoid disrupting workflow
				gray := color.New(color.FgHiBlack).SprintFunc()
				timestamp := time.Now().Format("15:04:05")
				fmt.Printf("%s [%s] Cleanup: %d stale instances\n", gray("•"), timestamp, cleaned)
			}
		}
	}
}
