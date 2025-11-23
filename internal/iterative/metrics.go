package iterative

import (
	"sort"
	"time"
)

// MetricsCollector provides instrumentation for iterative refinement.
// Implementations can track per-iteration and per-artifact metrics to measure
// quality improvement, cost, latency, and convergence behavior.
//
// This interface is optional - callers can pass nil to Converge() to disable
// metrics collection.
type MetricsCollector interface {
	// RecordIterationStart is called at the beginning of each refinement iteration
	RecordIterationStart(iteration int)

	// RecordIterationEnd is called when an iteration completes successfully
	RecordIterationEnd(iteration int, metrics *IterationMetrics)

	// RecordArtifactComplete is called when the entire refinement process finishes
	RecordArtifactComplete(result *ConvergenceResult, metrics *ArtifactMetrics)

	// RecordConvergenceCheckError is called when CheckConvergence fails
	// This tracks convergence detection failures for observability
	RecordConvergenceCheckError(iteration int, err error)

	// GetAggregateMetrics returns rolled-up statistics across all artifacts
	GetAggregateMetrics() *AggregateMetrics
}

// IterationMetrics captures metrics for a single refinement iteration.
type IterationMetrics struct {
	// Iteration is the iteration number (1-based)
	Iteration int

	// InputTokens is the number of tokens in the refinement prompt
	InputTokens int

	// OutputTokens is the number of tokens in the AI response
	OutputTokens int

	// DiffLines is the number of lines changed from the previous version
	DiffLines int

	// DiffPercent is the percentage of the artifact that changed
	DiffPercent float64

	// Duration is the time spent on this iteration
	Duration time.Duration

	// ConvergenceChecked indicates whether convergence was checked this iteration
	ConvergenceChecked bool

	// ConvergedThis indicates whether the convergence check passed (if checked)
	ConvergedThis bool

	// Confidence is the convergence detector's confidence (0.0-1.0), if checked
	Confidence float64

	// Strategy is the detector strategy used (e.g., "AI", "diff-based"), if checked
	Strategy string
}

// ArtifactMetrics captures metrics for an entire artifact refinement process.
type ArtifactMetrics struct {
	// ArtifactType identifies the type of artifact (e.g., "analysis", "assessment")
	ArtifactType string

	// Priority is the priority of the work (P0-P3), if applicable
	Priority string

	// Complexity is an optional complexity estimate (e.g., "simple", "medium", "complex")
	Complexity string

	// TotalIterations is the number of refinement iterations performed
	TotalIterations int

	// Converged indicates whether AI determined the artifact converged
	// (false means we hit MaxIterations)
	Converged bool

	// ConvergenceReason explains why refinement stopped
	// (e.g., "AI convergence", "max iterations", "timeout")
	ConvergenceReason string

	// TotalDuration is the total time spent refining this artifact
	TotalDuration time.Duration

	// TotalInputTokens is the sum of input tokens across all iterations
	TotalInputTokens int

	// TotalOutputTokens is the sum of output tokens across all iterations
	TotalOutputTokens int

	// QualityImprovement measures the quality delta from initial to final artifact
	// This is optional and domain-specific (e.g., number of issues caught,
	// quality gate failures avoided, human review score delta)
	QualityImprovement float64

	// IssuesDiscovered is the number of issues discovered through this artifact
	// (e.g., via analysis phase discovering follow-on work)
	IssuesDiscovered int

	// Iterations contains the per-iteration metrics
	Iterations []*IterationMetrics
}

// AggregateMetrics provides rolled-up statistics across multiple artifacts.
// This enables analysis of convergence behavior, cost, and quality trends.
type AggregateMetrics struct {
	// TotalArtifacts is the total number of artifacts processed
	TotalArtifacts int

	// ConvergedArtifacts is the count that reached AI-determined convergence
	ConvergedArtifacts int

	// MaxedOutArtifacts is the count that hit MaxIterations
	MaxedOutArtifacts int

	// TotalIterations is the sum of iterations across all artifacts
	TotalIterations int

	// MeanIterations is the average iterations per artifact
	MeanIterations float64

	// P50Iterations is the median iterations to convergence
	P50Iterations int

	// P95Iterations is the 95th percentile iterations to convergence
	P95Iterations int

	// TotalInputTokens is the sum of input tokens across all artifacts
	TotalInputTokens int64

	// TotalOutputTokens is the sum of output tokens across all artifacts
	TotalOutputTokens int64

	// EstimatedCostUSD is the estimated cost in USD (based on token counts)
	EstimatedCostUSD float64

	// TotalDuration is the sum of all artifact refinement durations
	TotalDuration time.Duration

	// ConvergenceCheckErrors is the total number of convergence check failures
	// When a convergence check fails, the system falls back to MaxIterations
	// High error rates indicate issues with the convergence detector
	ConvergenceCheckErrors int

	// ByType breaks down metrics by artifact type
	ByType map[string]*TypeMetrics

	// ByPriority breaks down metrics by priority (P0-P3)
	ByPriority map[string]*TypeMetrics

	// ByComplexity breaks down metrics by complexity
	ByComplexity map[string]*TypeMetrics
}

// TypeMetrics provides aggregate statistics for a specific artifact type,
// priority level, or complexity bucket.
type TypeMetrics struct {
	// Count is the number of artifacts in this category
	Count int

	// ConvergedCount is the number that reached convergence
	ConvergedCount int

	// MeanIterations is the average iterations for this category
	MeanIterations float64

	// P50Iterations is the median iterations
	P50Iterations int

	// P95Iterations is the 95th percentile
	P95Iterations int

	// TotalInputTokens is the sum of input tokens
	TotalInputTokens int64

	// TotalOutputTokens is the sum of output tokens
	TotalOutputTokens int64

	// MeanQualityImprovement is the average quality delta
	MeanQualityImprovement float64
}

// InMemoryMetricsCollector is a simple in-memory implementation of MetricsCollector.
// It stores all metrics in memory for analysis and testing.
type InMemoryMetricsCollector struct {
	// artifacts holds all artifact metrics
	artifacts []*ArtifactMetrics

	// currentArtifact tracks the artifact currently being refined
	currentArtifact *ArtifactMetrics

	// currentIterations tracks iterations for the current artifact
	currentIterations []*IterationMetrics

	// convergenceCheckErrors tracks convergence check failures
	convergenceCheckErrors int
}

// NewInMemoryMetricsCollector creates a new in-memory metrics collector
func NewInMemoryMetricsCollector() *InMemoryMetricsCollector {
	return &InMemoryMetricsCollector{
		artifacts: make([]*ArtifactMetrics, 0),
	}
}

// RecordIterationStart implements MetricsCollector
func (m *InMemoryMetricsCollector) RecordIterationStart(iteration int) {
	// Nothing to do - we record metrics at iteration end
	_ = iteration
}

// RecordIterationEnd implements MetricsCollector
func (m *InMemoryMetricsCollector) RecordIterationEnd(iteration int, metrics *IterationMetrics) {
	if metrics == nil {
		return
	}
	m.currentIterations = append(m.currentIterations, metrics)
}

// RecordConvergenceCheckError implements MetricsCollector
func (m *InMemoryMetricsCollector) RecordConvergenceCheckError(iteration int, err error) {
	m.convergenceCheckErrors++
	_ = iteration // Unused in this implementation, but available for future use
	_ = err        // Unused in this implementation, but available for future use
}

// RecordArtifactComplete implements MetricsCollector
func (m *InMemoryMetricsCollector) RecordArtifactComplete(result *ConvergenceResult, metrics *ArtifactMetrics) {
	if metrics == nil {
		return
	}

	// Attach iteration metrics to artifact metrics
	metrics.Iterations = m.currentIterations

	// Store the artifact metrics
	m.artifacts = append(m.artifacts, metrics)

	// Reset current tracking
	m.currentArtifact = nil
	m.currentIterations = nil
}

// GetAggregateMetrics implements MetricsCollector
func (m *InMemoryMetricsCollector) GetAggregateMetrics() *AggregateMetrics {
	if len(m.artifacts) == 0 {
		return &AggregateMetrics{
			ByType:       make(map[string]*TypeMetrics),
			ByPriority:   make(map[string]*TypeMetrics),
			ByComplexity: make(map[string]*TypeMetrics),
		}
	}

	agg := &AggregateMetrics{
		ByType:       make(map[string]*TypeMetrics),
		ByPriority:   make(map[string]*TypeMetrics),
		ByComplexity: make(map[string]*TypeMetrics),
	}

	// Collect iteration counts for percentile calculation
	var iterationCounts []int

	// Aggregate across all artifacts
	for _, artifact := range m.artifacts {
		agg.TotalArtifacts++
		agg.TotalIterations += artifact.TotalIterations
		agg.TotalInputTokens += int64(artifact.TotalInputTokens)
		agg.TotalOutputTokens += int64(artifact.TotalOutputTokens)
		agg.TotalDuration += artifact.TotalDuration

		if artifact.Converged {
			agg.ConvergedArtifacts++
			iterationCounts = append(iterationCounts, artifact.TotalIterations)
		} else {
			agg.MaxedOutArtifacts++
		}

		// Aggregate by type
		updateTypeMetrics(agg.ByType, artifact.ArtifactType, artifact)

		// Aggregate by priority
		if artifact.Priority != "" {
			updateTypeMetrics(agg.ByPriority, artifact.Priority, artifact)
		}

		// Aggregate by complexity
		if artifact.Complexity != "" {
			updateTypeMetrics(agg.ByComplexity, artifact.Complexity, artifact)
		}
	}

	// Calculate overall statistics
	if agg.TotalArtifacts > 0 {
		agg.MeanIterations = float64(agg.TotalIterations) / float64(agg.TotalArtifacts)
	}

	// Calculate percentiles
	if len(iterationCounts) > 0 {
		sort.Ints(iterationCounts)
		agg.P50Iterations = percentile(iterationCounts, 50)
		agg.P95Iterations = percentile(iterationCounts, 95)
	}

	// Include convergence check errors
	agg.ConvergenceCheckErrors = m.convergenceCheckErrors

	// Estimate cost (Claude Sonnet 4.5 pricing as of Jan 2025: $3/MTok input, $15/MTok output)
	inputCostPerMToken := 3.0
	outputCostPerMToken := 15.0
	agg.EstimatedCostUSD = (float64(agg.TotalInputTokens)/1_000_000)*inputCostPerMToken +
		(float64(agg.TotalOutputTokens)/1_000_000)*outputCostPerMToken

	return agg
}

// GetArtifacts returns all collected artifact metrics (useful for analysis)
func (m *InMemoryMetricsCollector) GetArtifacts() []*ArtifactMetrics {
	return m.artifacts
}

// Helper: updateTypeMetrics aggregates metrics for a specific type/priority/complexity
func updateTypeMetrics(metrics map[string]*TypeMetrics, key string, artifact *ArtifactMetrics) {
	tm := metrics[key]
	if tm == nil {
		tm = &TypeMetrics{}
		metrics[key] = tm
	}

	tm.Count++
	tm.TotalInputTokens += int64(artifact.TotalInputTokens)
	tm.TotalOutputTokens += int64(artifact.TotalOutputTokens)

	if artifact.Converged {
		tm.ConvergedCount++
	}

	// Quality improvement (if tracked)
	if artifact.QualityImprovement > 0 {
		// Incremental mean update
		oldMean := tm.MeanQualityImprovement
		tm.MeanQualityImprovement = oldMean + (artifact.QualityImprovement-oldMean)/float64(tm.Count)
	}
}

// Helper: percentile calculates the Nth percentile from a sorted slice
func percentile(sorted []int, p int) int {
	if len(sorted) == 0 {
		return 0
	}
	index := (len(sorted) * p) / 100
	if index >= len(sorted) {
		index = len(sorted) - 1
	}
	return sorted[index]
}

// ComputeTypeMetricsStats finalizes type metrics after all artifacts are processed
// This calculates percentiles and mean iterations for each type/priority/complexity
func ComputeTypeMetricsStats(artifacts []*ArtifactMetrics, byType map[string]*TypeMetrics) {
	// Group iterations by type
	iterationsByType := make(map[string][]int)

	for _, artifact := range artifacts {
		if artifact.Converged {
			iterationsByType[artifact.ArtifactType] = append(
				iterationsByType[artifact.ArtifactType],
				artifact.TotalIterations,
			)
		}
	}

	// Calculate stats for each type
	for key, tm := range byType {
		iterations := iterationsByType[key]
		if len(iterations) > 0 {
			// Mean
			sum := 0
			for _, it := range iterations {
				sum += it
			}
			tm.MeanIterations = float64(sum) / float64(len(iterations))

			// Percentiles
			sort.Ints(iterations)
			tm.P50Iterations = percentile(iterations, 50)
			tm.P95Iterations = percentile(iterations, 95)
		}
	}
}
