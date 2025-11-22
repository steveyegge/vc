// Package iterative provides a framework for convergent iterative refinement
// of AI-generated artifacts.
//
// # Overview
//
// Research shows that LLM-generated work converges to "outstandingly good"
// quality after approximately 4-5 refinement iterations, across diverse tasks
// (design, planning, implementation, review). The iterative package provides
// a simple, composable framework for applying this pattern to any AI-generated
// artifact.
//
// The key insight: LLMs have strong breadth-first generation but limited
// critique depth in a single pass. Multiple passes enable fresh perspective,
// recursive refinement, and breadth→depth transition.
//
// # Architecture
//
// The framework follows Zero Framework Cognition (ZFC) principles:
//   - Framework handles iteration mechanics (loop, count, timeout)
//   - AI handles convergence judgment (via Refiner.CheckConvergence)
//   - Pluggable refiners for different artifact types
//   - Config-driven min/max iterations with safeguards
//
// # Core Types
//
// Artifact represents an AI-generated artifact that can be refined. It carries
// the artifact type, current content, and context for refinement.
//
// RefinementConfig controls iteration behavior (min/max iterations, timeout,
// skip simple tasks).
//
// Refiner is the interface for pluggable refinement strategies. Implementations
// provide domain-specific refinement logic while the framework handles mechanics.
//
// # Usage Example
//
//	// Define a refiner for analysis artifacts
//	type AnalysisRefiner struct {
//	    supervisor ai.Supervisor
//	}
//
//	func (r *AnalysisRefiner) Refine(ctx context.Context, artifact *Artifact) (*Artifact, error) {
//	    // Use AI supervisor to refine the analysis
//	    prompt := fmt.Sprintf("Refine this analysis:\n\n%s\n\nContext: %s",
//	        artifact.Content, artifact.Context)
//	    refined, err := r.supervisor.Analyze(ctx, prompt)
//	    if err != nil {
//	        return nil, err
//	    }
//	    return &Artifact{
//	        Type:    artifact.Type,
//	        Content: refined,
//	        Context: artifact.Context,
//	    }, nil
//	}
//
//	func (r *AnalysisRefiner) CheckConvergence(ctx context.Context, current, previous *Artifact) (bool, error) {
//	    // Use AI to judge convergence
//	    prompt := fmt.Sprintf(`Has this artifact converged?
//
//	CURRENT: %s
//	PREVIOUS: %s
//
//	Consider:
//	1. Diff size: Minimal/superficial changes?
//	2. Completeness: All key concerns addressed?
//	3. Gaps: Obvious missing elements?
//	4. Marginal value: Would another iteration help?
//
//	Respond JSON: {converged: bool, reasoning: string}`,
//	        current.Content, previous.Content)
//
//	    response, err := r.supervisor.Analyze(ctx, prompt)
//	    if err != nil {
//	        return false, err
//	    }
//	    // Parse response and return convergence judgment
//	    // ... (implementation details omitted)
//	}
//
//	// Use the framework
//	func refineAnalysis(ctx context.Context, rawAnalysis string, supervisor ai.Supervisor) (*Artifact, error) {
//	    refiner := &AnalysisRefiner{supervisor: supervisor}
//	    config := RefinementConfig{
//	        MinIterations: 3,
//	        MaxIterations: 7,
//	        Timeout:       5 * time.Minute,
//	    }
//
//	    initial := &Artifact{
//	        Type:    "analysis",
//	        Content: rawAnalysis,
//	        Context: "Analysis of task completion",
//	    }
//
//	    // Optional: collect metrics
//	    collector := NewInMemoryMetricsCollector()
//
//	    result, err := Converge(ctx, initial, refiner, config, collector)
//	    if err != nil {
//	        return nil, err
//	    }
//
//	    log.Printf("Analysis converged after %d iterations (converged=%v, elapsed=%v)",
//	        result.Iterations, result.Converged, result.ElapsedTime)
//
//	    return result.FinalArtifact, nil
//	}
//
// # Convergence Strategies
//
// The framework supports multiple convergence detection strategies via the
// ConvergenceDetector interface:
//
// 1. **AI-driven (primary)**: AIConvergenceDetector uses AI supervision to judge
//    convergence by analyzing diff size, completeness, gaps, and marginal value
//    of further iteration. Returns confidence score (0.0-1.0).
//
// 2. **Diff-based (fallback)**: DiffBasedDetector uses simple heuristics - if
//    changed lines are below a threshold percentage (default 5%), assumes convergence.
//
// 3. **Chained (recommended)**: ChainedDetector chains multiple detectors with
//    fallback logic. Tries AI first, falls back to diff-based if AI fails or
//    returns low confidence.
//
// 4. **Timeout safeguard**: MaxIterations config provides hard limit regardless
//    of detector strategy.
//
// Example chained detector setup:
//
//	aiDetector, err := iterative.NewAIConvergenceDetector(supervisor, 0.8)
//	if err != nil {
//	    return err
//	}
//	diffDetector := iterative.NewDiffBasedDetector(5.0)
//	chainedDetector := iterative.NewChainedDetector(0.7, aiDetector, diffDetector)
//
// The chained approach provides robust convergence detection: AI for intelligent
// judgment with diff-based fallback for reliability.
//
// # Metrics (vc-it8m)
//
// The framework provides comprehensive metrics collection via the MetricsCollector interface:
//
//   - **Per-iteration metrics**: Tokens, diff size, duration, convergence checks
//   - **Per-artifact metrics**: Total iterations, cost, quality improvement
//   - **Aggregate metrics**: Convergence rates, percentiles, cost analysis
//
// Enable metrics collection by passing a collector to Converge():
//
//	collector := iterative.NewInMemoryMetricsCollector()
//	result, err := Converge(ctx, initial, refiner, config, collector)
//
//	// Access metrics
//	agg := collector.GetAggregateMetrics()
//	fmt.Printf("Convergence rate: %.2f%%\n", agg.ConvergenceRate())
//	fmt.Printf("Mean iterations: %.2f\n", agg.MeanIterations)
//	fmt.Printf("Estimated cost: $%.4f\n", agg.EstimatedCostUSD)
//
// Metrics enable data-driven tuning of refinement parameters and validation
// of the 4-5 iteration hypothesis. See docs/ITERATIVE_REFINEMENT_METRICS.md
// for detailed guidance on interpreting and using metrics.
//
// Metrics collection is optional - pass nil to disable.
//
// # Design Principles
//
// 1. Simple, composable: Core abstraction is ~100 lines of code
// 2. ZFC compliance: Framework provides mechanics, AI provides judgment
// 3. Pluggable: Refiner interface allows domain-specific strategies
// 4. Safe: Built-in safeguards (max iterations, timeout, cancellation)
// 5. Observable: Returns iteration count and timing metrics
//
// # Integration Points
//
// This framework can be integrated into various VC workflow phases:
//
//   - Tier 1 (High Value): Analysis phase, Issue planning/decomposition
//   - Tier 2 (Medium Value): Assessment (selective), Pre-flight review
//   - Tier 3 (Lower Priority): Issue description refinement
//
// See the parent epic (vc-x1t4) for detailed integration guidance.
//
// # Cost Considerations
//
// Typical cost: ~$0.14 per artifact (5 iterations @ ~28K tokens).
// Latency: 10-25s per artifact.
// This is negligible compared to agent execution costs (~$1-10 per issue).
//
// # Error Handling
//
// The framework provides robust error handling:
//
//   - Refiner errors are propagated immediately (fail-fast)
//   - Convergence check errors are logged but don't fail refinement (fallback to MaxIterations)
//   - Context cancellation is respected at iteration boundaries
//   - Timeout (if configured) triggers graceful cancellation
//   - Config validation catches invalid settings before iteration starts
package iterative
