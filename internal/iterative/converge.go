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
// Metrics collection (optional):
// - Pass a MetricsCollector to collect iteration and artifact metrics
// - Pass nil to disable metrics collection
//
// Returns ConvergenceResult with the final artifact and metadata, or error if
// refinement failed.
func Converge(ctx context.Context, initial *Artifact, refiner Refiner, config RefinementConfig, collector MetricsCollector) (*ConvergenceResult, error) {
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

		// Notify metrics collector that iteration is starting
		if collector != nil {
			collector.RecordIterationStart(i)
		}

		iterationStart := time.Now()

		// Perform refinement pass
		refined, err := refiner.Refine(ctx, current)
		if err != nil {
			return nil, fmt.Errorf("refinement failed at iteration %d: %w", i, err)
		}

		iterationDuration := time.Since(iterationStart)

		// Update previous/current
		previous = current
		current = refined

		// Check for skip signal (same object returned on first iteration)
		if i == 1 && config.SkipSimple && current == previous {
			result := &ConvergenceResult{
				FinalArtifact: current,
				Iterations:    0,
				Converged:     true,
				ElapsedTime:   time.Since(startTime),
			}
			// Record artifact metrics (if collector provided)
			if collector != nil {
				artifactMetrics := &ArtifactMetrics{
					ArtifactType:      initial.Type,
					TotalIterations:   0,
					Converged:         true,
					ConvergenceReason: "skipped (simple)",
					TotalDuration:     result.ElapsedTime,
				}
				collector.RecordArtifactComplete(result, artifactMetrics)
			}
			return result, nil
		}

		// Calculate diff metrics for this iteration
		diffLines := 0
		diffPercent := 0.0
		if previous != nil {
			diffLines = countDiffLines(previous.Content, current.Content)
			totalLines := countLines(current.Content)
			if totalLines > 0 {
				diffPercent = (float64(diffLines) / float64(totalLines)) * 100
			}
		}

		// After MinIterations, check for convergence
		var convergedNow bool
		var convergenceConfidence float64
		var convergenceStrategy string
		convergenceChecked := false

		if i >= config.MinIterations {
			decision, err := refiner.CheckConvergence(ctx, current, previous)
			convergenceChecked = true

			if err != nil {
				// Log error but continue - convergence check is advisory
				// We'll rely on MaxIterations as fallback
				// TODO: Add logging when logging infrastructure is available
				_ = err // Suppress unused variable warning for now
			} else if decision != nil {
				convergedNow = decision.Converged
				convergenceConfidence = decision.Confidence
				convergenceStrategy = decision.Strategy

				if convergedNow {
					result := &ConvergenceResult{
						FinalArtifact: current,
						Iterations:    i,
						Converged:     true,
						ElapsedTime:   time.Since(startTime),
					}

					// Record final iteration metrics
					if collector != nil {
						iterMetrics := &IterationMetrics{
							Iteration:          i,
							DiffLines:          diffLines,
							DiffPercent:        diffPercent,
							Duration:           iterationDuration,
							ConvergenceChecked: convergenceChecked,
							ConvergedThis:      convergedNow,
							Confidence:         convergenceConfidence,
							Strategy:           convergenceStrategy,
						}
						collector.RecordIterationEnd(i, iterMetrics)

						// Record artifact completion
						artifactMetrics := &ArtifactMetrics{
							ArtifactType:      initial.Type,
							TotalIterations:   i,
							Converged:         true,
							ConvergenceReason: "AI convergence",
							TotalDuration:     result.ElapsedTime,
						}
						collector.RecordArtifactComplete(result, artifactMetrics)
					}

					return result, nil
				}
			}
		}

		// Record iteration metrics (if collector provided)
		if collector != nil {
			iterMetrics := &IterationMetrics{
				Iteration:          i,
				DiffLines:          diffLines,
				DiffPercent:        diffPercent,
				Duration:           iterationDuration,
				ConvergenceChecked: convergenceChecked,
				ConvergedThis:      convergedNow,
				Confidence:         convergenceConfidence,
				Strategy:           convergenceStrategy,
			}
			collector.RecordIterationEnd(i, iterMetrics)
		}
	}

	// Reached MaxIterations without AI convergence
	result := &ConvergenceResult{
		FinalArtifact: current,
		Iterations:    config.MaxIterations,
		Converged:     false,
		ElapsedTime:   time.Since(startTime),
	}

	// Record artifact metrics (if collector provided)
	if collector != nil {
		artifactMetrics := &ArtifactMetrics{
			ArtifactType:      initial.Type,
			TotalIterations:   config.MaxIterations,
			Converged:         false,
			ConvergenceReason: "max iterations",
			TotalDuration:     result.ElapsedTime,
		}
		collector.RecordArtifactComplete(result, artifactMetrics)
	}

	return result, nil
}
