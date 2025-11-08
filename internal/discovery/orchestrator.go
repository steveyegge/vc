package discovery

import (
	"context"
	"fmt"
	"time"

	"github.com/steveyegge/vc/internal/deduplication"
	"github.com/steveyegge/vc/internal/health"
	"github.com/steveyegge/vc/internal/storage"
	"github.com/steveyegge/vc/internal/types"
)

// Orchestrator coordinates the discovery process:
// - Builds codebase context once (shared by all workers)
// - Runs workers in dependency order
// - Enforces budget constraints
// - Deduplicates discovered issues
// - Files issues in Beads
type Orchestrator struct {
	registry *WorkerRegistry
	store    storage.Storage
	dedup    deduplication.Deduplicator
	config   *Config
}

// NewOrchestrator creates a new discovery orchestrator.
func NewOrchestrator(
	registry *WorkerRegistry,
	store storage.Storage,
	dedup deduplication.Deduplicator,
	config *Config,
) *Orchestrator {
	if config == nil {
		config = DefaultConfig()
	}

	return &Orchestrator{
		registry: registry,
		store:    store,
		dedup:    dedup,
		config:   config,
	}
}

// Run executes the discovery process and returns the results.
func (o *Orchestrator) Run(ctx context.Context, rootDir string) (*DiscoveryResult, error) {
	result := &DiscoveryResult{
		StartedAt: time.Now(),
		Config:    o.config,
	}

	// Build codebase context once (shared by all workers)
	contextBuilder := NewContextBuilder(rootDir, o.config.ExcludePaths)
	codebaseCtx, err := contextBuilder.Build(ctx)
	if err != nil {
		return nil, fmt.Errorf("building codebase context: %w", err)
	}

	// Resolve workers to run
	workers, err := o.resolveWorkers()
	if err != nil {
		return nil, fmt.Errorf("resolving workers: %w", err)
	}

	// Run workers in order
	allIssues := []DiscoveredIssue{}
	workerResults := make(map[string]*WorkerResult)

	for _, worker := range workers {
		// Check budget before running
		if err := o.checkBudget(result); err != nil {
			result.BudgetExceeded = true
			result.BudgetExceededReason = err.Error()
			break
		}

		// Run worker
		workerResult, err := o.runWorker(ctx, worker, codebaseCtx)
		if err != nil {
			// Log error but continue with other workers
			result.Errors = append(result.Errors, fmt.Sprintf("worker %s failed: %v", worker.Name(), err))
			continue
		}

		// Record results
		workerResults[worker.Name()] = workerResult
		allIssues = append(allIssues, workerResult.IssuesDiscovered...)

		// Update running totals
		result.Stats.TotalDuration += workerResult.Stats.Duration
		result.Stats.TotalAICalls += workerResult.Stats.AICallsMade
		result.Stats.TotalCost += workerResult.Stats.EstimatedCost
		result.Stats.WorkersRun++
	}

	result.WorkerResults = workerResults
	result.Stats.TotalIssuesDiscovered = len(allIssues)

	// Deduplicate issues if enabled
	if o.config.DeduplicationEnabled && len(allIssues) > 0 {
		uniqueIssues, duplicates, err := o.deduplicateIssues(ctx, allIssues)
		if err != nil {
			return nil, fmt.Errorf("deduplicating issues: %w", err)
		}

		result.UniqueIssues = uniqueIssues
		result.DuplicateIssues = duplicates
		result.Stats.UniqueIssues = len(uniqueIssues)
		result.Stats.DuplicateIssues = len(duplicates)
	} else {
		// No deduplication, all issues are unique
		result.UniqueIssues = allIssues
		result.Stats.UniqueIssues = len(allIssues)
	}

	// File issues if enabled
	if o.config.AutoFileIssues && len(result.UniqueIssues) > 0 {
		filedIDs, err := o.fileIssues(ctx, result.UniqueIssues)
		if err != nil {
			return nil, fmt.Errorf("filing issues: %w", err)
		}

		result.FiledIssueIDs = filedIDs
		result.Stats.IssuesFiled = len(filedIDs)
	}

	result.CompletedAt = time.Now()
	result.Stats.TotalDuration = result.CompletedAt.Sub(result.StartedAt)

	return result, nil
}

// resolveWorkers resolves the workers to run based on configuration.
func (o *Orchestrator) resolveWorkers() ([]DiscoveryWorker, error) {
	// If workers explicitly specified, use those
	if len(o.config.Workers) > 0 {
		return o.registry.ResolveWorkers(o.config.Workers)
	}

	// Otherwise, use preset
	return o.registry.GetPresetWorkers(o.config.Preset)
}

// checkBudget checks if any budget limits have been exceeded.
func (o *Orchestrator) checkBudget(result *DiscoveryResult) error {
	budget := o.config.Budget

	if budget.MaxCost > 0 && result.Stats.TotalCost >= budget.MaxCost {
		return fmt.Errorf("max cost exceeded: $%.2f >= $%.2f", result.Stats.TotalCost, budget.MaxCost)
	}

	if budget.MaxDuration > 0 && result.Stats.TotalDuration >= budget.MaxDuration {
		return fmt.Errorf("max duration exceeded: %v >= %v", result.Stats.TotalDuration, budget.MaxDuration)
	}

	if budget.MaxAICalls > 0 && result.Stats.TotalAICalls >= budget.MaxAICalls {
		return fmt.Errorf("max AI calls exceeded: %d >= %d", result.Stats.TotalAICalls, budget.MaxAICalls)
	}

	if budget.MaxIssues > 0 && result.Stats.TotalIssuesDiscovered >= budget.MaxIssues {
		return fmt.Errorf("max issues exceeded: %d >= %d", result.Stats.TotalIssuesDiscovered, budget.MaxIssues)
	}

	return nil
}

// runWorker executes a single worker and returns the result.
func (o *Orchestrator) runWorker(
	ctx context.Context,
	worker DiscoveryWorker,
	codebaseCtx health.CodebaseContext,
) (*WorkerResult, error) {
	startTime := time.Now()

	// Run worker
	result, err := worker.Analyze(ctx, codebaseCtx)
	if err != nil {
		return nil, err
	}

	// Update stats
	result.Stats.Duration = time.Since(startTime)

	return result, nil
}

// deduplicateIssues removes duplicate issues using AI-powered deduplication.
func (o *Orchestrator) deduplicateIssues(
	ctx context.Context,
	issues []DiscoveredIssue,
) (unique []DiscoveredIssue, duplicates []DiscoveredIssue, err error) {
	if o.dedup == nil {
		// No deduplicator, return all as unique
		return issues, nil, nil
	}

	// Convert DiscoveredIssues to types.Issue for deduplication
	candidates := make([]*types.Issue, len(issues))
	for i, issue := range issues {
		// Convert string type to IssueType
		issueType := types.TypeTask
		switch issue.Type {
		case "bug":
			issueType = types.TypeBug
		case "feature":
			issueType = types.TypeFeature
		case "epic":
			issueType = types.TypeEpic
		case "chore":
			issueType = types.TypeChore
		}

		candidates[i] = &types.Issue{
			Title:              issue.Title,
			Description:        issue.Description,
			IssueType:          issueType,
			Priority:           issue.Priority,
			Status:             types.StatusOpen,
			AcceptanceCriteria: "Issue resolved and verified",
			// Note: ID will be empty for new issues
		}
	}

	// Run batch deduplication
	dedupResult, err := o.dedup.DeduplicateBatch(ctx, candidates)
	if err != nil {
		return nil, nil, fmt.Errorf("batch deduplication failed: %w", err)
	}

	// Map unique issues back to DiscoveredIssues
	uniqueMap := make(map[*types.Issue]bool)
	for _, issue := range dedupResult.UniqueIssues {
		uniqueMap[issue] = true
	}

	for i, candidate := range candidates {
		if uniqueMap[candidate] {
			unique = append(unique, issues[i])
		} else {
			duplicates = append(duplicates, issues[i])
		}
	}

	return unique, duplicates, nil
}

// fileIssues creates Beads issues for discovered problems.
func (o *Orchestrator) fileIssues(ctx context.Context, issues []DiscoveredIssue) ([]string, error) {
	filedIDs := []string{}
	actor := "discovery"

	for _, issue := range issues {
		// Convert string type to IssueType
		issueType := types.TypeTask
		switch issue.Type {
		case "bug":
			issueType = types.TypeBug
		case "feature":
			issueType = types.TypeFeature
		case "epic":
			issueType = types.TypeEpic
		case "chore":
			issueType = types.TypeChore
		}

		// Create issue in storage
		// Note: ID will be auto-generated by Beads
		beadsIssue := &types.Issue{
			Title:              issue.Title,
			Description:        issue.Description,
			IssueType:          issueType,
			Priority:           issue.Priority,
			Status:             types.StatusOpen,
			CreatedAt:          time.Now(),
			UpdatedAt:          time.Now(),
			AcceptanceCriteria: "Issue resolved and verified",
		}

		// Create issue (Beads will generate ID)
		if err := o.store.CreateIssue(ctx, beadsIssue, actor); err != nil {
			return filedIDs, fmt.Errorf("creating issue %q: %w", issue.Title, err)
		}

		// Get the generated ID from the issue
		id := beadsIssue.ID

		// Add default labels
		labels := append([]string{}, o.config.DefaultLabels...)
		labels = append(labels, fmt.Sprintf("discovered-by:%s", issue.DiscoveredBy))
		if issue.Category != "" {
			labels = append(labels, fmt.Sprintf("category:%s", issue.Category))
		}

		// Add labels
		for _, label := range labels {
			if err := o.store.AddLabel(ctx, id, label, actor); err != nil {
				// Log but don't fail - issue was created successfully
				continue
			}
		}

		filedIDs = append(filedIDs, id)
	}

	return filedIDs, nil
}

// DiscoveryResult contains the complete results of a discovery run.
type DiscoveryResult struct {
	// Timing
	StartedAt   time.Time
	CompletedAt time.Time

	// Configuration used
	Config *Config

	// Results by worker
	WorkerResults map[string]*WorkerResult

	// Deduplication results
	UniqueIssues    []DiscoveredIssue
	DuplicateIssues []DiscoveredIssue

	// Filed issues
	FiledIssueIDs []string

	// Budget tracking
	BudgetExceeded       bool
	BudgetExceededReason string

	// Errors encountered (non-fatal)
	Errors []string

	// Overall statistics
	Stats DiscoveryStats
}

// DiscoveryStats tracks aggregate statistics for a discovery run.
type DiscoveryStats struct {
	TotalDuration         time.Duration
	TotalAICalls          int
	TotalCost             float64
	TotalIssuesDiscovered int
	UniqueIssues          int
	DuplicateIssues       int
	IssuesFiled           int
	WorkersRun            int
}

// Summary returns a human-readable summary of the discovery results.
func (r *DiscoveryResult) Summary() string {
	return fmt.Sprintf(
		"Discovery completed in %v\n"+
			"Workers run: %d\n"+
			"Issues discovered: %d (unique: %d, duplicates: %d)\n"+
			"Issues filed: %d\n"+
			"AI calls: %d\n"+
			"Estimated cost: $%.2f",
		r.Stats.TotalDuration,
		r.Stats.WorkersRun,
		r.Stats.TotalIssuesDiscovered,
		r.Stats.UniqueIssues,
		r.Stats.DuplicateIssues,
		r.Stats.IssuesFiled,
		r.Stats.TotalAICalls,
		r.Stats.TotalCost,
	)
}
