package executor

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"sync"
	"time"

	"github.com/steveyegge/vc/internal/events"
	"github.com/steveyegge/vc/internal/gates"
	"github.com/steveyegge/vc/internal/storage/beads"
	"github.com/steveyegge/vc/internal/types"
)

// PreFlightChecker runs quality gates before claiming work to ensure baseline is clean
// vc-196: Pre-flight quality gates to prevent work on broken baseline
//
// Key innovation: Baseline cache keyed by git commit hash
// - Near-instant pre-flight for unchanged code (cache hit)
// - Only re-run gates when commit changes
// - 5 minute TTL for cache freshness
//
// Phase 1 (MVP): Basic caching with commit hash
// Phase 2 (future): Baseline comparison (only NEW failures block)
// Phase 3 (future): Sandbox reuse for unchanged baselines
type PreFlightChecker struct {
	storage     *beads.VCStorage
	gatesRunner gates.GateProvider
	config      *PreFlightConfig

	// In-memory cache for speed (keyed by commit hash)
	mu    sync.RWMutex
	cache map[string]*cachedBaseline
}

// cachedBaseline represents a baseline stored in memory with TTL
type cachedBaseline struct {
	baseline  *beads.GateBaseline
	expiresAt time.Time
}

// PreFlightConfig holds preflight checker configuration
type PreFlightConfig struct {
	Enabled      bool          // Enable preflight checks (default: true)
	CacheTTL     time.Duration // How long to cache baselines (default: 5 minutes)
	FailureMode  FailureMode   // What to do when baseline fails (default: block)
	WorkingDir   string        // Directory where gates are executed
	GatesTimeout time.Duration // Timeout for quality gate execution (default: 5 minutes)
}

// FailureMode determines how to handle baseline failures
type FailureMode string

const (
	FailureModeBlock  FailureMode = "block"  // Don't claim work (degraded mode)
	FailureModeWarn   FailureMode = "warn"   // Warn but continue claiming work
	FailureModeIgnore FailureMode = "ignore" // Ignore failures completely
)

// DefaultPreFlightConfig returns default preflight checker configuration
func DefaultPreFlightConfig() *PreFlightConfig {
	return &PreFlightConfig{
		Enabled:      true,
		CacheTTL:     5 * time.Minute,
		FailureMode:  FailureModeBlock,
		WorkingDir:   ".",
		GatesTimeout: 5 * time.Minute,
	}
}

// NewPreFlightChecker creates a new preflight checker
func NewPreFlightChecker(storage *beads.VCStorage, gatesRunner gates.GateProvider, config *PreFlightConfig) (*PreFlightChecker, error) {
	if storage == nil {
		return nil, fmt.Errorf("storage is required")
	}
	if gatesRunner == nil {
		return nil, fmt.Errorf("gates runner is required")
	}
	if config == nil {
		config = DefaultPreFlightConfig()
	}

	return &PreFlightChecker{
		storage:     storage,
		gatesRunner: gatesRunner,
		config:      config,
		cache:       make(map[string]*cachedBaseline),
	}, nil
}

// CheckBaseline checks if the current baseline is clean (all gates pass)
// Returns (allPassed bool, commitHash string, err error)
//
// Workflow:
// 1. Get current commit hash
// 2. Check in-memory cache (with TTL)
// 3. If cache miss, check database cache
// 4. If still miss, run gates and cache result
// 5. Emit events for monitoring
//
// Note: Detailed gate results are cached and can be retrieved via GetCachedResults()
func (p *PreFlightChecker) CheckBaseline(ctx context.Context, executorID string) (bool, string, error) {
	if !p.config.Enabled {
		return true, "", nil // Preflight disabled, always pass
	}

	// 1. Get current commit hash
	commitHash, branchName, err := p.getCurrentCommit(ctx)
	if err != nil {
		return false, "", fmt.Errorf("failed to get current commit: %w", err)
	}

	// Emit pre_flight_check_started event
	p.emitEvent(ctx, executorID, events.EventTypePreFlightCheckStarted, map[string]interface{}{
		"commit_hash": commitHash,
		"branch_name": branchName,
	})

	// 2. Check in-memory cache
	if baseline := p.getCachedBaseline(commitHash); baseline != nil {
		p.emitEvent(ctx, executorID, events.EventTypeBaselineCacheHit, map[string]interface{}{
			"commit_hash": commitHash,
			"all_passed":  baseline.AllPassed,
			"cache_type":  "memory",
			"age_seconds": time.Since(mustParseTime(baseline.Timestamp)).Seconds(),
		})

		p.emitEvent(ctx, executorID, events.EventTypePreFlightCheckCompleted, map[string]interface{}{
			"commit_hash": commitHash,
			"all_passed":  baseline.AllPassed,
			"cached":      true,
		})

		return baseline.AllPassed, commitHash, nil
	}

	// 3. Check database cache
	baseline, err := p.storage.GetGateBaseline(ctx, commitHash)
	if err != nil {
		return false, commitHash, fmt.Errorf("failed to query baseline cache: %w", err)
	}

	if baseline != nil {
		// Check if baseline is still fresh (within TTL)
		baselineTime := mustParseTime(baseline.Timestamp)
		if time.Since(baselineTime) < p.config.CacheTTL {
			// Cache hit - store in memory and return
			p.setCachedBaseline(commitHash, baseline)

			p.emitEvent(ctx, executorID, events.EventTypeBaselineCacheHit, map[string]interface{}{
				"commit_hash": commitHash,
				"all_passed":  baseline.AllPassed,
				"cache_type":  "database",
				"age_seconds": time.Since(baselineTime).Seconds(),
			})

			p.emitEvent(ctx, executorID, events.EventTypePreFlightCheckCompleted, map[string]interface{}{
				"commit_hash": commitHash,
				"all_passed":  baseline.AllPassed,
				"cached":      true,
			})

			return baseline.AllPassed, commitHash, nil
		}

		// Baseline is stale, invalidate it
		if err := p.storage.InvalidateGateBaseline(ctx, commitHash); err != nil {
			fmt.Printf("warning: failed to invalidate stale baseline: %v\n", err)
		}
		p.invalidateCachedBaseline(commitHash)
	}

	// 4. Cache miss - run gates
	p.emitEvent(ctx, executorID, events.EventTypeBaselineCacheMiss, map[string]interface{}{
		"commit_hash": commitHash,
	})

	return p.runGatesAndCache(ctx, executorID, commitHash, branchName)
}

// runGatesAndCache runs quality gates and caches the result
func (p *PreFlightChecker) runGatesAndCache(ctx context.Context, executorID, commitHash, branchName string) (bool, string, error) {
	startTime := time.Now()

	// Run gates with timeout
	gatesCtx, cancel := context.WithTimeout(ctx, p.config.GatesTimeout)
	defer cancel()

	results, allPassed := p.gatesRunner.RunAll(gatesCtx)
	duration := time.Since(startTime)

	// Convert gate results to storage format
	gateResults := make(map[string]*types.GateResult)
	for _, result := range results {
		errMsg := ""
		if result.Error != nil {
			errMsg = result.Error.Error()
		}
		gateResults[string(result.Gate)] = &types.GateResult{
			Gate:   string(result.Gate),
			Passed: result.Passed,
			Output: result.Output,
			Error:  errMsg,
		}
	}

	// Create baseline
	baseline := &beads.GateBaseline{
		CommitHash: commitHash,
		BranchName: branchName,
		Timestamp:  time.Now().Format(time.RFC3339),
		AllPassed:  allPassed,
		Results:    gateResults,
	}

	// Store in database
	if err := p.storage.SetGateBaseline(ctx, baseline); err != nil {
		fmt.Printf("warning: failed to cache baseline: %v\n", err)
	}

	// Store in memory
	p.setCachedBaseline(commitHash, baseline)

	// Emit completion event
	p.emitEvent(ctx, executorID, events.EventTypePreFlightCheckCompleted, map[string]interface{}{
		"commit_hash":       commitHash,
		"all_passed":        allPassed,
		"cached":            false,
		"duration_seconds":  duration.Seconds(),
		"failing_gates":     p.getFailingGates(results),
		"total_gates":       len(results),
	})

	return allPassed, commitHash, nil
}

// getCurrentCommit gets the current git commit hash and branch name
func (p *PreFlightChecker) getCurrentCommit(ctx context.Context) (commitHash string, branchName string, err error) {
	// Get commit hash
	cmd := exec.CommandContext(ctx, "git", "rev-parse", "HEAD")
	cmd.Dir = p.config.WorkingDir
	output, err := cmd.Output()
	if err != nil {
		return "", "", fmt.Errorf("failed to get commit hash: %w", err)
	}
	commitHash = string(output[:40]) // First 40 chars (full SHA)

	// Get branch name
	cmd = exec.CommandContext(ctx, "git", "rev-parse", "--abbrev-ref", "HEAD")
	cmd.Dir = p.config.WorkingDir
	output, err = cmd.Output()
	if err != nil {
		return "", "", fmt.Errorf("failed to get branch name: %w", err)
	}
	branchName = string(output)
	if len(branchName) > 0 && branchName[len(branchName)-1] == '\n' {
		branchName = branchName[:len(branchName)-1]
	}

	return commitHash, branchName, nil
}

// getCachedBaseline retrieves a baseline from in-memory cache (with TTL check)
func (p *PreFlightChecker) getCachedBaseline(commitHash string) *beads.GateBaseline {
	p.mu.RLock()
	defer p.mu.RUnlock()

	cached, ok := p.cache[commitHash]
	if !ok {
		return nil
	}

	// Check TTL
	if time.Now().After(cached.expiresAt) {
		return nil
	}

	return cached.baseline
}

// setCachedBaseline stores a baseline in in-memory cache
func (p *PreFlightChecker) setCachedBaseline(commitHash string, baseline *beads.GateBaseline) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.cache[commitHash] = &cachedBaseline{
		baseline:  baseline,
		expiresAt: time.Now().Add(p.config.CacheTTL),
	}
}

// invalidateCachedBaseline removes a baseline from in-memory cache
func (p *PreFlightChecker) invalidateCachedBaseline(commitHash string) {
	p.mu.Lock()
	defer p.mu.Unlock()

	delete(p.cache, commitHash)
}

// GetCachedResults retrieves gate results from the cached baseline for a commit hash
// Returns nil if no cached baseline exists or if the baseline has expired
func (p *PreFlightChecker) GetCachedResults(ctx context.Context, commitHash string) ([]*gates.Result, error) {
	// Try in-memory cache first
	if baseline := p.getCachedBaseline(commitHash); baseline != nil {
		return convertBaselineToResults(baseline), nil
	}

	// Try database cache
	baseline, err := p.storage.GetGateBaseline(ctx, commitHash)
	if err != nil {
		return nil, fmt.Errorf("failed to query baseline cache: %w", err)
	}

	if baseline == nil {
		return nil, nil // No cached baseline
	}

	// Check if baseline is still fresh (within TTL)
	baselineTime := mustParseTime(baseline.Timestamp)
	if time.Since(baselineTime) >= p.config.CacheTTL {
		return nil, nil // Baseline is stale
	}

	return convertBaselineToResults(baseline), nil
}

// convertBaselineToResults converts a cached baseline to gate results
func convertBaselineToResults(baseline *beads.GateBaseline) []*gates.Result {
	var results []*gates.Result
	for gateName, gateResult := range baseline.Results {
		var err error
		if gateResult.Error != "" {
			err = fmt.Errorf("%s", gateResult.Error)
		}
		results = append(results, &gates.Result{
			Gate:   gates.GateType(gateName),
			Passed: gateResult.Passed,
			Output: gateResult.Output,
			Error:  err,
		})
	}
	return results
}

// getFailingGates returns a list of gate names that failed
func (p *PreFlightChecker) getFailingGates(results []*gates.Result) []string {
	var failing []string
	for _, result := range results {
		if !result.Passed {
			failing = append(failing, string(result.Gate))
		}
	}
	return failing
}

// emitEvent emits a preflight event to the activity feed
func (p *PreFlightChecker) emitEvent(ctx context.Context, executorID string, eventType events.EventType, data map[string]interface{}) {
	event := &events.AgentEvent{
		Timestamp:  time.Now(),
		ExecutorID: executorID,
		Type:       eventType,
		Severity:   events.SeverityInfo,
		Message:    fmt.Sprintf("Preflight: %s", eventType),
		Data:       data,
	}

	if err := p.storage.StoreAgentEvent(ctx, event); err != nil {
		fmt.Printf("warning: failed to store preflight event: %v\n", err)
	}
}

// mustParseTime parses RFC3339 time or panics (used for cached baseline timestamps)
func mustParseTime(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		// This should never happen with timestamps we generate
		return time.Time{}
	}
	return t
}

// HandleBaselineFailure handles degraded mode when baseline fails
// vc-200: Degraded mode - create system blocking issues for failing gates
//
// This creates system-level blocking issues with IDs like:
// - vc-baseline-test (test gate failure)
// - vc-baseline-lint (lint gate failure)
// - vc-baseline-build (build gate failure)
//
// These issues block the executor from claiming work until resolved.
// The executor continues polling but won't claim work while these issues exist.
func (p *PreFlightChecker) HandleBaselineFailure(ctx context.Context, executorID, commitHash string, results []*gates.Result) error {
	var failingGates []string
	for _, result := range results {
		if !result.Passed {
			failingGates = append(failingGates, string(result.Gate))
			if err := p.createBaselineBlockingIssue(ctx, result); err != nil {
				return fmt.Errorf("failed to create baseline blocking issue: %w", err)
			}
		}
	}

	// Emit degraded mode event
	p.emitEvent(ctx, executorID, events.EventTypeExecutorDegradedMode, map[string]interface{}{
		"commit_hash":   commitHash,
		"failing_gates": failingGates,
		"total_gates":   len(results),
		"severity":      "critical",
	})

	fmt.Printf("\n⚠️  DEGRADED MODE: Baseline quality gates failed\n")
	fmt.Printf("   Commit: %s\n", commitHash)
	fmt.Printf("   Failing gates: %v\n", failingGates)
	fmt.Printf("   Executor will not claim work until baseline is fixed\n\n")

	return nil
}

// createBaselineBlockingIssue creates a system-level blocking issue for a gate failure
func (p *PreFlightChecker) createBaselineBlockingIssue(ctx context.Context, result *gates.Result) error {
	issueID := fmt.Sprintf("vc-baseline-%s", result.Gate)

	// Check if issue already exists
	existingIssue, err := p.storage.GetIssue(ctx, issueID)
	if err != nil {
		return fmt.Errorf("failed to check for existing issue: %w", err)
	}

	if existingIssue != nil {
		// Issue already exists
		if existingIssue.Status != types.StatusClosed {
			// Issue is still open - don't create duplicate
			fmt.Printf("   Issue %s already exists (status: %s)\n", issueID, existingIssue.Status)
			return nil
		}
		// Issue exists but is closed - reopen it with updated information
		fmt.Printf("   Reopening closed baseline issue: %s\n", issueID)

		// Prepare new failure information
		errMsg := ""
		if result.Error != nil {
			errMsg = result.Error.Error()
		}
		output := result.Output
		if len(output) > 2000 {
			output = output[:2000] + "\n... (truncated, see full output in logs)"
		}
		newNotes := fmt.Sprintf("Gate failed again. Error: %s\n\nOutput:\n```\n%s\n```", errMsg, output)

		// Reopen by updating status and notes
		if err := p.storage.UpdateIssue(ctx, issueID, map[string]interface{}{
			"status": string(types.StatusOpen),
			"notes":  newNotes,
		}, "preflight-degraded-mode"); err != nil {
			return fmt.Errorf("failed to reopen baseline issue: %w", err)
		}
		return nil
	}

	// Truncate output for issue description
	output := result.Output
	if len(output) > 2000 {
		output = output[:2000] + "\n... (truncated, see full output in logs)"
	}

	errMsg := ""
	if result.Error != nil {
		errMsg = result.Error.Error()
	}

	issue := &types.Issue{
		ID:          issueID,
		Title:       fmt.Sprintf("Baseline quality gate failure: %s", result.Gate),
		Description: fmt.Sprintf("The %s quality gate is failing on the baseline (main branch).\n\n"+
			"This blocks the executor from claiming work until fixed.\n\n"+
			"Error: %s\n\n"+
			"Output:\n```\n%s\n```",
			result.Gate, errMsg, output),
		Status:      types.StatusOpen,
		Priority:    1, // P1 - critical
		IssueType:   types.TypeBug,
		Design:      fmt.Sprintf("Fix the %s gate failures reported above.", result.Gate),
		AcceptanceCriteria: fmt.Sprintf("- %s gate passes on main branch\n"+
			"- Preflight check succeeds\n"+
			"- Executor can resume claiming work",
			result.Gate),
	}

	if err := p.storage.CreateIssue(ctx, issue, "preflight-degraded-mode"); err != nil {
		return fmt.Errorf("failed to create baseline issue: %w", err)
	}

	// Add labels
	if err := p.storage.AddLabel(ctx, issueID, fmt.Sprintf("gate:%s", result.Gate), "preflight-degraded-mode"); err != nil {
		fmt.Printf("warning: failed to add gate label: %v\n", err)
	}
	if err := p.storage.AddLabel(ctx, issueID, "baseline-failure", "preflight-degraded-mode"); err != nil {
		fmt.Printf("warning: failed to add baseline-failure label: %v\n", err)
	}
	if err := p.storage.AddLabel(ctx, issueID, "system", "preflight-degraded-mode"); err != nil {
		fmt.Printf("warning: failed to add system label: %v\n", err)
	}

	fmt.Printf("   Created baseline blocking issue: %s\n", issueID)
	return nil
}

// PreFlightConfigFromEnv creates a PreFlightConfig from environment variables
// vc-201: Configuration and events for pre-flight
//
// Environment variables:
// - VC_PREFLIGHT_ENABLED: Enable preflight checks (default: true)
// - VC_PREFLIGHT_CACHE_TTL: Cache TTL duration (default: 5m)
// - VC_PREFLIGHT_FAILURE_MODE: Failure mode (block/warn/ignore, default: block)
// - VC_PREFLIGHT_GATES_TIMEOUT: Timeout for gate execution (default: 5m)
func PreFlightConfigFromEnv() (*PreFlightConfig, error) {
	cfg := DefaultPreFlightConfig()

	// VC_PREFLIGHT_ENABLED
	if val := os.Getenv("VC_PREFLIGHT_ENABLED"); val != "" {
		enabled, err := strconv.ParseBool(val)
		if err != nil {
			return nil, fmt.Errorf("invalid VC_PREFLIGHT_ENABLED: %w", err)
		}
		cfg.Enabled = enabled
	}

	// VC_PREFLIGHT_CACHE_TTL
	if val := os.Getenv("VC_PREFLIGHT_CACHE_TTL"); val != "" {
		ttl, err := time.ParseDuration(val)
		if err != nil {
			return nil, fmt.Errorf("invalid VC_PREFLIGHT_CACHE_TTL: %w", err)
		}
		if ttl <= 0 {
			return nil, fmt.Errorf("VC_PREFLIGHT_CACHE_TTL must be positive (got %v)", ttl)
		}
		cfg.CacheTTL = ttl
	}

	// VC_PREFLIGHT_FAILURE_MODE
	if val := os.Getenv("VC_PREFLIGHT_FAILURE_MODE"); val != "" {
		switch val {
		case "block":
			cfg.FailureMode = FailureModeBlock
		case "warn":
			cfg.FailureMode = FailureModeWarn
		case "ignore":
			cfg.FailureMode = FailureModeIgnore
		default:
			return nil, fmt.Errorf("invalid VC_PREFLIGHT_FAILURE_MODE: must be block, warn, or ignore (got %s)", val)
		}
	}

	// VC_PREFLIGHT_GATES_TIMEOUT
	if val := os.Getenv("VC_PREFLIGHT_GATES_TIMEOUT"); val != "" {
		timeout, err := time.ParseDuration(val)
		if err != nil {
			return nil, fmt.Errorf("invalid VC_PREFLIGHT_GATES_TIMEOUT: %w", err)
		}
		if timeout <= 0 {
			return nil, fmt.Errorf("VC_PREFLIGHT_GATES_TIMEOUT must be positive (got %v)", timeout)
		}
		cfg.GatesTimeout = timeout
	}

	return cfg, nil
}
