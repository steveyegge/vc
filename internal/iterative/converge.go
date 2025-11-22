package iterative

import (
	"context"
	"fmt"
	"time"
)

// Converge iteratively refines an artifact until AI determines convergence or
// max iterations is reached. The function handles iteration mechanics (loop,
// count, timeout) while delegating convergence judgment to the refiner (ZFC).
//
// The refinement process:
// 1. Performs MinIterations refinement passes unconditionally
// 2. After MinIterations, checks convergence after each pass
// 3. Stops when AI determines convergence or MaxIterations is reached
// 4. Returns the final artifact, iteration count, and any error
//
// Safeguards:
// - MaxIterations prevents runaway iteration
// - Context cancellation allows graceful shutdown
// - Timeout (if configured) limits total refinement duration
// - Errors from refiner are propagated immediately
//
// Returns ConvergenceResult with the final artifact and metadata, or error if
// refinement failed.
func Converge(ctx context.Context, initial *Artifact, refiner Refiner, config RefinementConfig) (*ConvergenceResult, error) {
	startTime := time.Now()

	// Validate config
	if config.MinIterations < 0 {
		return nil, fmt.Errorf("MinIterations cannot be negative: %d", config.MinIterations)
	}
	if config.MaxIterations < config.MinIterations {
		return nil, fmt.Errorf("MaxIterations (%d) must be >= MinIterations (%d)",
			config.MaxIterations, config.MinIterations)
	}
	if config.MaxIterations == 0 {
		return nil, fmt.Errorf("MaxIterations cannot be zero (prevents infinite loops)")
	}

	// Set up timeout context if configured
	if config.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, config.Timeout)
		defer cancel()
	}

	// Note: SkipSimple is checked below in the iteration loop
	// Refiner can indicate skip by returning the same artifact in first pass

	current := initial
	var previous *Artifact

	// Iteration loop
	for i := 1; i <= config.MaxIterations; i++ {
		// Check context cancellation
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("refinement canceled after %d iterations: %w", i-1, err)
		}

		// Perform refinement pass
		refined, err := refiner.Refine(ctx, current)
		if err != nil {
			return nil, fmt.Errorf("refinement failed at iteration %d: %w", i, err)
		}

		// Update previous/current
		previous = current
		current = refined

		// Check for skip signal (same object returned on first iteration)
		if i == 1 && config.SkipSimple && current == previous {
			return &ConvergenceResult{
				FinalArtifact: current,
				Iterations:    0,
				Converged:     true,
				ElapsedTime:   time.Since(startTime),
			}, nil
		}

		// After MinIterations, check for convergence
		if i >= config.MinIterations {
			convergedNow, err := refiner.CheckConvergence(ctx, current, previous)
			if err != nil {
				// Log error but continue - convergence check is advisory
				// We'll rely on MaxIterations as fallback
				// TODO: Add logging when logging infrastructure is available
				_ = err // Suppress unused variable warning for now
			} else if convergedNow {
				return &ConvergenceResult{
					FinalArtifact: current,
					Iterations:    i,
					Converged:     true,
					ElapsedTime:   time.Since(startTime),
				}, nil
			}
		}
	}

	// Reached MaxIterations without AI convergence
	return &ConvergenceResult{
		FinalArtifact: current,
		Iterations:    config.MaxIterations,
		Converged:     false,
		ElapsedTime:   time.Since(startTime),
	}, nil
}
