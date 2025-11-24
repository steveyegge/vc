package iterative

import (
	"context"
	"fmt"
	"math"
	"strings"

	// TODO(vc-t9ls): The AIConvergenceDetector has been moved to the ai package
	// to avoid import cycles. The general-purpose iterative package should not
	// depend on the specific ai implementation.
	// "github.com/steveyegge/vc/internal/ai"
)

// ConvergenceDetector determines whether an artifact has converged to a stable,
// high-quality state. Implementations use different strategies (AI-driven,
// diff-based, semantic similarity) to judge convergence.
type ConvergenceDetector interface {
	// CheckConvergence determines if the artifact has stabilized.
	// Returns a ConvergenceDecision containing the convergence judgment,
	// confidence level, reasoning, and strategy used.
	CheckConvergence(ctx context.Context, current, previous *Artifact) (*ConvergenceDecision, error)
}

// ConvergenceDecision captures the outcome of a single convergence check
type ConvergenceDecision struct {
	Converged  bool    // Whether the artifact has converged
	Confidence float64 // Confidence in the judgment (0.0-1.0)
	Reasoning  string  // Explanation for the decision
	Strategy   string  // Which detection strategy was used
}

// TODO(vc-t9ls): AIConvergenceDetector has been moved to the ai package to avoid import cycles.
// The general-purpose iterative package should not depend on the specific ai implementation.
// See internal/ai/analysis_refiner.go for the AI-based convergence implementation.

// DiffBasedDetector is a fallback convergence detector that uses simple
// diff size heuristics. If changes are below threshold, assume converged.
type DiffBasedDetector struct {
	// MaxDiffPercent is the maximum percentage of changed lines to consider converged
	// Default: 5% (if less than 5% of lines changed, consider converged)
	MaxDiffPercent float64
}

// NewDiffBasedDetector creates a diff-based convergence detector
func NewDiffBasedDetector(maxDiffPercent float64) *DiffBasedDetector {
	if maxDiffPercent <= 0 {
		maxDiffPercent = 5.0 // Default: 5% change threshold
	}
	return &DiffBasedDetector{
		MaxDiffPercent: maxDiffPercent,
	}
}

// CheckConvergence uses diff size to determine convergence
func (d *DiffBasedDetector) CheckConvergence(ctx context.Context, current, previous *Artifact) (*ConvergenceDecision, error) {
	diffLines := countDiffLines(previous.Content, current.Content)
	totalLines := countLines(current.Content)

	if totalLines == 0 {
		// Empty artifact - not converged
		return &ConvergenceDecision{
			Converged:  false,
			Confidence: 1.0,
			Reasoning:  "Empty artifact",
			Strategy:   "diff-based",
		}, nil
	}

	changePercent := (float64(diffLines) / float64(totalLines)) * 100

	// Simple heuristic: if change is small, assume converged
	converged := changePercent < d.MaxDiffPercent

	// Confidence is inverse of how close we are to threshold
	// Far below threshold = high confidence converged
	// Far above threshold = high confidence NOT converged
	// Near threshold = low confidence
	distanceFromThreshold := math.Abs(changePercent - d.MaxDiffPercent)
	confidence := math.Min(1.0, distanceFromThreshold/d.MaxDiffPercent)

	reasoning := fmt.Sprintf("%.1f%% of lines changed (threshold: %.1f%%)", changePercent, d.MaxDiffPercent)

	return &ConvergenceDecision{
		Converged:  converged,
		Confidence: confidence,
		Reasoning:  reasoning,
		Strategy:   "diff-based",
	}, nil
}

// ChainedDetector chains multiple detectors with fallback logic.
// It tries the primary detector first. If it fails or has low confidence,
// it falls back to the next detector in the chain.
type ChainedDetector struct {
	detectors []ConvergenceDetector
	// MinConfidence is the minimum confidence threshold to accept a result
	// If a detector returns confidence below this, try the next detector
	MinConfidence float64
}

// NewChainedDetector creates a chained convergence detector
func NewChainedDetector(minConfidence float64, detectors ...ConvergenceDetector) *ChainedDetector {
	if minConfidence <= 0 {
		minConfidence = 0.7 // Default: require reasonably high confidence
	}
	return &ChainedDetector{
		detectors:     detectors,
		MinConfidence: minConfidence,
	}
}

// CheckConvergence tries each detector in sequence until one succeeds with sufficient confidence
func (d *ChainedDetector) CheckConvergence(ctx context.Context, current, previous *Artifact) (*ConvergenceDecision, error) {
	var lastErr error
	var lastDecision *ConvergenceDecision

	for i, detector := range d.detectors {
		decision, err := detector.CheckConvergence(ctx, current, previous)

		// If this detector succeeded and has sufficient confidence, use it
		if err == nil && decision.Confidence >= d.MinConfidence {
			return decision, nil
		}

		// Track last error and decision for fallback
		lastErr = err
		lastDecision = decision

		// If this was the last detector and it failed, return the error
		if i == len(d.detectors)-1 && err != nil {
			return nil, fmt.Errorf("all convergence detectors failed, last error: %w", lastErr)
		}

		// Otherwise, try the next detector
	}

	// All detectors had low confidence - return the last result
	// (This handles the case where all detectors succeed but with low confidence)
	if lastDecision != nil {
		return lastDecision, nil
	}

	// Shouldn't reach here, but handle gracefully
	return &ConvergenceDecision{
		Converged:  false,
		Confidence: 0.0,
		Reasoning:  "All detectors had low confidence",
		Strategy:   "chained",
	}, nil
}

// Helper functions

func countLines(text string) int {
	if text == "" {
		return 0
	}
	return strings.Count(text, "\n") + 1
}

func countDiffLines(prev, current string) int {
	// Simple line-by-line comparison
	// A real implementation would use a proper diff algorithm,
	// but this is good enough for convergence heuristics
	prevLines := strings.Split(prev, "\n")
	currentLines := strings.Split(current, "\n")

	// Count lines that differ
	maxLines := maxInt(len(prevLines), len(currentLines))
	diffCount := 0

	for i := 0; i < maxLines; i++ {
		prevLine := ""
		currentLine := ""
		if i < len(prevLines) {
			prevLine = strings.TrimSpace(prevLines[i])
		}
		if i < len(currentLines) {
			currentLine = strings.TrimSpace(currentLines[i])
		}
		if prevLine != currentLine {
			diffCount++
		}
	}

	return diffCount
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// ConvergenceMetrics tracks convergence detection performance for analysis
// and tuning. Callers can optionally record metrics during refinement.
type ConvergenceMetrics struct {
	// TotalChecks is the number of convergence checks performed
	TotalChecks int

	// ConvergedChecks is the number of checks that returned converged=true
	ConvergedChecks int

	// TotalIterations is the total number of refinement iterations across all artifacts
	TotalIterations int

	// ConvergedIterations is the total iterations for artifacts that reached convergence
	// (vs hitting MaxIterations)
	ConvergedIterations int

	// ArtifactsConverged is the count of artifacts that reached AI-determined convergence
	ArtifactsConverged int

	// ArtifactsMaxedOut is the count of artifacts that hit MaxIterations
	ArtifactsMaxedOut int

	// DetectorStrategyUsed tracks which detector made the final decision (for ChainedDetector)
	// Key: detector type (e.g., "AI", "diff-based"), Value: count
	DetectorStrategyUsed map[string]int
}

// NewConvergenceMetrics creates a new metrics tracker
func NewConvergenceMetrics() *ConvergenceMetrics {
	return &ConvergenceMetrics{
		DetectorStrategyUsed: make(map[string]int),
	}
}

// RecordCheck records a convergence check
func (m *ConvergenceMetrics) RecordCheck(converged bool, strategy string) {
	m.TotalChecks++
	if converged {
		m.ConvergedChecks++
	}
	if strategy != "" {
		m.DetectorStrategyUsed[strategy]++
	}
}

// RecordArtifact records the completion of an artifact refinement
func (m *ConvergenceMetrics) RecordArtifact(result *ConvergenceResult) {
	m.TotalIterations += result.Iterations
	if result.Converged {
		m.ArtifactsConverged++
		m.ConvergedIterations += result.Iterations
	} else {
		m.ArtifactsMaxedOut++
	}
}

// ConvergenceRate returns the percentage of artifacts that reached convergence
func (m *ConvergenceMetrics) ConvergenceRate() float64 {
	total := m.ArtifactsConverged + m.ArtifactsMaxedOut
	if total == 0 {
		return 0.0
	}
	return float64(m.ArtifactsConverged) / float64(total) * 100
}

// MeanIterations returns the average number of iterations across all artifacts
func (m *ConvergenceMetrics) MeanIterations() float64 {
	total := m.ArtifactsConverged + m.ArtifactsMaxedOut
	if total == 0 {
		return 0.0
	}
	return float64(m.TotalIterations) / float64(total)
}

// MeanIterationsToConvergence returns the average iterations for converged artifacts only
func (m *ConvergenceMetrics) MeanIterationsToConvergence() float64 {
	if m.ArtifactsConverged == 0 {
		return 0.0
	}
	return float64(m.ConvergedIterations) / float64(m.ArtifactsConverged)
}
