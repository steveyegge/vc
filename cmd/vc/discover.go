package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/fatih/color"
	"github.com/spf13/cobra"
	"github.com/steveyegge/vc/internal/ai"
	"github.com/steveyegge/vc/internal/deduplication"
	"github.com/steveyegge/vc/internal/discovery"
	"github.com/steveyegge/vc/internal/health"
	"github.com/steveyegge/vc/internal/storage"
)

var (
	discoverPreset  string
	discoverWorkers string
	discoverDryRun  bool
	discoverList    bool
)

var discoverCmd = &cobra.Command{
	Use:   "discover",
	Short: "Run discovery workers to find issues in the codebase",
	Long: `Run discovery workers to analyze the codebase and file actionable issues.

Discovery workers scan the code for common problems like:
- Oversized files and functions
- Code duplication
- Architectural issues
- Missing tests and documentation
- Security vulnerabilities
- Common bug patterns

Workers can be run individually or via presets:
- quick:    Fast scan, minimal AI usage (~30s, $0.50)
- standard: Comprehensive scan (~5min, $2.00)
- thorough: Deep analysis, all workers (~15min, $10.00)

Examples:
  vc discover                              # Run standard preset
  vc discover --preset=quick               # Run quick preset
  vc discover --preset=thorough            # Run thorough preset
  vc discover --workers=filesize,cruft     # Run specific workers
  vc discover --dry-run                    # Preview without filing issues
  vc discover --list                       # List available workers`,
	Run: func(cmd *cobra.Command, args []string) {
		ctx := context.Background()

		// Handle --list flag
		if discoverList {
			listDiscoveryWorkers()
			return
		}

		// Get database path
		dbPath, err := storage.DiscoverDatabase()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}

		// Open storage
		db, err := storage.NewStorage(ctx, &storage.Config{Path: dbPath})
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: failed to open database: %v\n", err)
			os.Exit(1)
		}
		defer db.Close()

		// Get project root
		projectRoot, err := storage.GetProjectRoot(dbPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: failed to get project root: %v\n", err)
			os.Exit(1)
		}

		// Determine preset or workers
		preset := discovery.Preset(discoverPreset)
		if preset == "" {
			preset = discovery.PresetStandard
		}

		// Run discovery
		if err := runDiscovery(ctx, db, projectRoot, preset, discoverDryRun); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
	},
}

func init() {
	rootCmd.AddCommand(discoverCmd)
	discoverCmd.Flags().StringVar(&discoverPreset, "preset", "standard", "Preset: quick, standard, or thorough")
	discoverCmd.Flags().StringVar(&discoverWorkers, "workers", "", "Comma-separated worker names (overrides preset)")
	discoverCmd.Flags().BoolVar(&discoverDryRun, "dry-run", false, "Preview issues without filing them")
	discoverCmd.Flags().BoolVar(&discoverList, "list", false, "List available workers and exit")
}

// runDiscovery executes the discovery process and prints results.
// This is shared by both 'vc init --discover' and 'vc discover' commands.
func runDiscovery(
	ctx context.Context,
	db storage.Storage,
	projectRoot string,
	preset discovery.Preset,
	dryRun bool,
) error {
	green := color.New(color.FgGreen).SprintFunc()
	cyan := color.New(color.FgCyan).SprintFunc()
	yellow := color.New(color.FgYellow).SprintFunc()
	gray := color.New(color.FgHiBlack).SprintFunc()

	// Create health monitor registry (for adapter workers)
	healthRegistry, err := health.NewMonitorRegistry(".beads/health_state.json")
	if err != nil {
		return fmt.Errorf("creating health registry: %w", err)
	}

	// Create AI supervisor for health monitors and deduplication
	supervisor, err := ai.NewSupervisor(&ai.Config{
		Store: db,
	})
	if err != nil {
		return fmt.Errorf("creating AI supervisor: %w", err)
	}

	// Register health monitors
	fileSizeMonitor, err := health.NewFileSizeMonitor(projectRoot, supervisor)
	if err != nil {
		return fmt.Errorf("creating filesize monitor: %w", err)
	}
	healthRegistry.Register(fileSizeMonitor)

	cruftDetector, err := health.NewCruftDetector(projectRoot, supervisor)
	if err != nil {
		return fmt.Errorf("creating cruft detector: %w", err)
	}
	healthRegistry.Register(cruftDetector)

	duplicationDetector, err := health.NewDuplicationDetector(projectRoot, supervisor)
	if err != nil {
		return fmt.Errorf("creating duplication detector: %w", err)
	}
	healthRegistry.Register(duplicationDetector)

	zfcDetector, err := health.NewZFCDetector(projectRoot, supervisor)
	if err != nil {
		return fmt.Errorf("creating ZFC detector: %w", err)
	}
	healthRegistry.Register(zfcDetector)

	buildModernizer, err := health.NewBuildModernizer(projectRoot, supervisor)
	if err != nil {
		return fmt.Errorf("creating build modernizer: %w", err)
	}
	healthRegistry.Register(buildModernizer)

	cicdReviewer, err := health.NewCICDReviewer(projectRoot, supervisor)
	if err != nil {
		return fmt.Errorf("creating CI/CD reviewer: %w", err)
	}
	healthRegistry.Register(cicdReviewer)

	dependencyAuditor, err := health.NewDependencyAuditor(projectRoot, supervisor)
	if err != nil {
		return fmt.Errorf("creating dependency auditor: %w", err)
	}
	healthRegistry.Register(dependencyAuditor)

	// Create worker registry
	workerRegistry, err := discovery.DefaultRegistry(healthRegistry)
	if err != nil {
		return fmt.Errorf("creating worker registry: %w", err)
	}

	// Create deduplicator
	dedup, err := deduplication.NewAIDeduplicator(supervisor, db, deduplication.DefaultConfig())
	if err != nil {
		return fmt.Errorf("creating deduplicator: %w", err)
	}

	// Create discovery config
	config := discovery.PresetConfig(preset)
	if dryRun {
		config.AutoFileIssues = false
	}

	// Parse custom workers if specified
	if discoverWorkers != "" {
		config.Workers = strings.Split(discoverWorkers, ",")
		// Trim whitespace
		for i, w := range config.Workers {
			config.Workers[i] = strings.TrimSpace(w)
		}
	}

	// Create orchestrator
	orchestrator := discovery.NewOrchestrator(workerRegistry, db, dedup, config)

	// Run discovery
	fmt.Printf("%s Starting discovery with preset: %s\n", gray("→"), cyan(preset))
	if dryRun {
		fmt.Printf("%s Dry-run mode: issues will not be filed\n", yellow("⚠"))
	}
	fmt.Println()

	result, err := orchestrator.Run(ctx, projectRoot)
	if err != nil {
		return fmt.Errorf("running discovery: %w", err)
	}

	// Print summary
	fmt.Printf("\n%s Discovery complete!\n\n", green("✓"))

	// Print statistics
	fmt.Printf("  Workers run: %s\n", cyan(fmt.Sprintf("%d", result.Stats.WorkersRun)))
	fmt.Printf("  Duration: %s\n", cyan(result.Stats.TotalDuration.String()))
	fmt.Printf("  Issues discovered: %s\n", cyan(fmt.Sprintf("%d", result.Stats.TotalIssuesDiscovered)))
	if result.Config.DeduplicationEnabled {
		fmt.Printf("    Unique: %s\n", cyan(fmt.Sprintf("%d", result.Stats.UniqueIssues)))
		fmt.Printf("    Duplicates: %s\n", gray(fmt.Sprintf("%d", result.Stats.DuplicateIssues)))
	}
	if !dryRun {
		fmt.Printf("  Issues filed: %s\n", green(fmt.Sprintf("%d", result.Stats.IssuesFiled)))
	}
	fmt.Printf("  AI calls: %s\n", cyan(fmt.Sprintf("%d", result.Stats.TotalAICalls)))
	fmt.Printf("  Estimated cost: %s\n", cyan(fmt.Sprintf("$%.2f", result.Stats.TotalCost)))

	// Print budget status
	if result.BudgetExceeded {
		fmt.Printf("\n%s Budget limit reached: %s\n", yellow("⚠"), result.BudgetExceededReason)
	}

	// Print errors
	if len(result.Errors) > 0 {
		fmt.Printf("\n%s Errors encountered:\n", yellow("⚠"))
		for _, errMsg := range result.Errors {
			fmt.Printf("  - %s\n", gray(errMsg))
		}
	}

	// Print next steps
	if !dryRun && result.Stats.IssuesFiled > 0 {
		fmt.Printf("\n%s Next steps:\n", gray("→"))
		fmt.Printf("  %s\n", gray("vc ready         # See ready issues"))
		fmt.Printf("  %s\n", gray("vc execute       # Start working on them"))
	}

	fmt.Println()

	return nil
}

// listDiscoveryWorkers prints all available discovery workers.
func listDiscoveryWorkers() {
	ctx := context.Background()
	cyan := color.New(color.FgCyan).SprintFunc()
	gray := color.New(color.FgHiBlack).SprintFunc()

	// Get database path
	dbPath, err := storage.DiscoverDatabase()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	// Open storage
	db, err := storage.NewStorage(ctx, &storage.Config{Path: dbPath})
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: failed to open database: %v\n", err)
		os.Exit(1)
	}
	defer db.Close()

	// Get project root
	projectRoot, err := storage.GetProjectRoot(dbPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: failed to get project root: %v\n", err)
		os.Exit(1)
	}

	// Create registries
	healthRegistry, err := health.NewMonitorRegistry(".beads/health_state.json")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	// Create AI supervisor
	supervisor, err := ai.NewSupervisor(&ai.Config{
		Store: db,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	// Register health monitors
	fileSizeMonitor, _ := health.NewFileSizeMonitor(projectRoot, supervisor)
	if fileSizeMonitor != nil {
		healthRegistry.Register(fileSizeMonitor)
	}

	cruftDetector, _ := health.NewCruftDetector(projectRoot, supervisor)
	if cruftDetector != nil {
		healthRegistry.Register(cruftDetector)
	}

	duplicationDetector, _ := health.NewDuplicationDetector(projectRoot, supervisor)
	if duplicationDetector != nil {
		healthRegistry.Register(duplicationDetector)
	}

	zfcDetector, _ := health.NewZFCDetector(projectRoot, supervisor)
	if zfcDetector != nil {
		healthRegistry.Register(zfcDetector)
	}

	buildModernizer, _ := health.NewBuildModernizer(projectRoot, supervisor)
	if buildModernizer != nil {
		healthRegistry.Register(buildModernizer)
	}

	cicdReviewer, _ := health.NewCICDReviewer(projectRoot, supervisor)
	if cicdReviewer != nil {
		healthRegistry.Register(cicdReviewer)
	}

	dependencyAuditor, _ := health.NewDependencyAuditor(projectRoot, supervisor)
	if dependencyAuditor != nil {
		healthRegistry.Register(dependencyAuditor)
	}

	workerRegistry, err := discovery.DefaultRegistry(healthRegistry)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("\n%s Available Discovery Workers:\n\n", cyan("Discovery Workers"))

	for _, name := range workerRegistry.List() {
		worker, _ := workerRegistry.Get(name)
		cost := worker.Cost()

		fmt.Printf("  %s\n", cyan(name))
		fmt.Printf("    Philosophy: %s\n", worker.Philosophy())
		fmt.Printf("    Scope: %s\n", worker.Scope())
		fmt.Printf("    Cost: %s (~%v, %d AI calls)\n",
			gray(string(cost.Category)),
			cost.EstimatedDuration,
			cost.AICallsEstimated)
		if deps := worker.Dependencies(); len(deps) > 0 {
			fmt.Printf("    Dependencies: %s\n", gray(strings.Join(deps, ", ")))
		}
		fmt.Println()
	}

	// Print presets
	fmt.Printf("%s Presets:\n\n", cyan("Discovery Presets"))

	presets := []discovery.Preset{
		discovery.PresetQuick,
		discovery.PresetStandard,
		discovery.PresetThorough,
	}

	for _, preset := range presets {
		config := discovery.PresetConfig(preset)
		fmt.Printf("  %s\n", cyan(preset))
		fmt.Printf("    Workers: %s\n", gray(strings.Join(config.Workers, ", ")))
		fmt.Printf("    Budget: $%.2f, %v, %d AI calls, %d max issues\n",
			config.Budget.MaxCost,
			config.Budget.MaxDuration,
			config.Budget.MaxAICalls,
			config.Budget.MaxIssues)
		fmt.Println()
	}
}
