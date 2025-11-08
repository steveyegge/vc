// Package discovery implements the Discovery Mode infrastructure for VC.
//
// Discovery Mode enables bootstrapping VC on any codebase by running specialized
// workers that scan, analyze, and file actionable issues. This provides an instant
// jumpstart with real work items instead of starting with an empty issue tracker.
//
// # Architecture
//
// Discovery Mode consists of several key components:
//
//  1. DiscoveryWorker - Interface for workers that analyze codebases
//  2. CodebaseContext - Shared context built once and used by all workers
//  3. WorkerRegistry - Manages worker registration and dependency resolution
//  4. Orchestrator - Coordinates worker execution with budget enforcement
//  5. Configuration - YAML-based configuration with preset support
//
// # Workers
//
// Discovery workers embody specific analysis philosophies and discover issues
// during initial codebase bootstrap. Unlike health monitors (which run on a schedule),
// discovery workers run once during initialization.
//
// Workers can be:
//   - Custom workers implementing DiscoveryWorker interface
//   - Adapted health monitors via WorkerAdapter
//
// Built-in workers include:
//   - filesize: Detects oversized files and functions
//   - cruft: Identifies commented-out code, TODOs, and dead code
//   - duplication: Finds duplicated code blocks
//   - zfc: Detects Zero Framework Cognition violations
//   - architecture: Analyzes package structure and boundaries (future)
//   - bugs: Scans for common bug patterns (future)
//   - documentation: Finds missing or outdated docs (future)
//   - tests: Identifies test coverage gaps (future)
//   - dependencies: Analyzes dependency health (future)
//   - security: Scans for vulnerabilities (future)
//
// # Presets
//
// Discovery provides three presets for common use cases:
//
//   quick:    Fast scan, minimal AI usage (~30s, $0.50)
//             Workers: filesize, cruft
//
//   standard: Comprehensive scan (~5min, $2.00)
//             Workers: filesize, cruft, duplication, architecture
//
//   thorough: Deep analysis, all workers (~15min, $10.00)
//             Workers: all available workers
//
// # Budget Enforcement
//
// Discovery respects budget constraints to prevent runaway costs:
//   - MaxCost: Maximum total cost in USD
//   - MaxDuration: Maximum total execution time
//   - MaxAICalls: Maximum AI API calls
//   - MaxIssues: Maximum issues to discover
//
// Workers stop when any limit is reached. Per-worker budgets can override global limits.
//
// # Deduplication
//
// Discovery uses AI-powered semantic deduplication to prevent filing duplicate issues.
// This compares newly discovered issues against:
//   - Recent open issues (configurable window, default 7 days)
//   - Other issues in the current discovery batch
//
// Deduplication considers:
//   - Title and description similarity
//   - File/line references
//   - Issue type and priority
//   - Parent issue context
//
// # Configuration
//
// Discovery can be configured via:
//   1. Presets (quick/standard/thorough)
//   2. CLI flags (--preset, --workers, --dry-run)
//   3. Config file (.vc/discovery.yaml)
//
// Config file example:
//
//	preset: standard
//	budget:
//	  max_cost: 2.00
//	  max_duration: 5m
//	  max_ai_calls: 100
//	workers:
//	  - filesize
//	  - cruft
//	  - duplication
//	deduplication:
//	  enabled: true
//	  window: 7d
//	issue_filing:
//	  auto_file: true
//	  default_priority: 2
//
// # Usage
//
// Discovery can be run via:
//
//	# During initialization
//	vc init --discover
//	vc init --discover --preset=quick
//
//	# Standalone
//	vc discover
//	vc discover --preset=thorough
//	vc discover --workers=filesize,cruft
//	vc discover --dry-run
//
// # ZFC Compliance
//
// Discovery Mode follows Zero Framework Cognition principles:
//   - Workers collect facts and patterns (file sizes, code patterns)
//   - AI makes judgments (what's too large, what's problematic)
//   - No hardcoded thresholds or heuristics in the orchestration layer
//   - Statistical distributions used for outlier detection
//
// # Worker Development
//
// To create a custom worker:
//
//	type MyWorker struct{}
//
//	func (w *MyWorker) Name() string { return "myworker" }
//
//	func (w *MyWorker) Philosophy() string {
//	    return "Code should follow principle X"
//	}
//
//	func (w *MyWorker) Scope() string {
//	    return "Analyzes aspect Y of the codebase"
//	}
//
//	func (w *MyWorker) Cost() health.CostEstimate {
//	    return health.CostEstimate{
//	        EstimatedDuration: 30 * time.Second,
//	        AICallsEstimated:  10,
//	        Category:          health.CostModerate,
//	    }
//	}
//
//	func (w *MyWorker) Dependencies() []string {
//	    return []string{"architecture"} // Run after architecture
//	}
//
//	func (w *MyWorker) Analyze(ctx context.Context, codebase health.CodebaseContext) (*WorkerResult, error) {
//	    // Analyze codebase and return discovered issues
//	    issues := []DiscoveredIssue{...}
//	    return &WorkerResult{
//	        IssuesDiscovered: issues,
//	        Context:          "What was examined",
//	        Reasoning:        "Why these are problems",
//	    }, nil
//	}
//
// Then register and run:
//
//	registry := discovery.NewWorkerRegistry()
//	registry.Register(&MyWorker{})
//	orchestrator := discovery.NewOrchestrator(registry, store, dedup, config)
//	result, err := orchestrator.Run(ctx, projectRoot)
//
// # Integration with VC
//
// Discovery Mode integrates seamlessly with VC's existing infrastructure:
//   - Uses Beads storage for issue filing
//   - Leverages AI supervisor for assessment and deduplication
//   - Reuses health monitors as workers (via adapter pattern)
//   - Respects the same labeling and priority conventions
//   - Follows the same ZFC principles
//
// Discovered issues are tagged with:
//   - discovered:discovery (general discovery label)
//   - discovered-by:<worker-name> (which worker found it)
//   - category:<category> (issue category)
//
// # Future Enhancements
//
// Planned improvements (vc-oxak, vc-cq4l, vc-c9an, vc-4noi):
//   - Architecture worker (package structure analysis)
//   - Bug pattern detection worker
//   - Documentation gap finder
//   - Test coverage analyzer
//   - Dependency health checker
//   - Security vulnerability scanner
//   - Build system analyzer
//   - CI/CD pipeline analyzer
//   - Custom worker plugin system
//   - Worker marketplace/sharing
//
// # References
//
// See also:
//   - internal/health: Health monitoring system (scheduled checks)
//   - internal/deduplication: AI-powered issue deduplication
//   - docs/FEATURES.md: Feature documentation
//   - vc-5239: Discovery Mode Orchestration epic
package discovery
