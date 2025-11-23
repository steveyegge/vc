// Package iterative provides a framework for convergent iterative refinement
// of AI-generated artifacts. The framework handles iteration mechanics (loop,
// count, timeout) while delegating convergence detection to AI (ZFC compliance).
//
// The core abstraction is the Converge function, which takes an initial artifact
// and iteratively refines it using a pluggable Refiner until AI determines the
// artifact has converged to a stable, high-quality state.
//
// Example usage:
//
//	refiner := &AnalysisRefiner{supervisor: supervisor}
//	config := RefinementConfig{
//	    MinIterations: 3,
//	    MaxIterations: 7,
//	}
//
//	initial := &Artifact{
//	    Type:    "analysis",
//	    Content: rawAnalysis,
//	    Context: issueContext,
//	}
//
//	final, iterations, err := Converge(ctx, initial, refiner, config)
package iterative

import (
	"context"
	"time"
)

// Artifact represents an AI-generated artifact that can be iteratively refined.
// It carries the artifact type, current content, and any additional context
// needed for refinement.
type Artifact struct {
	// Type identifies the artifact type (e.g., 'assessment', 'analysis', 'issue_breakdown')
	Type string

	// Content is the current version of the artifact
	Content string

	// Context provides additional context for refinement (e.g., issue description,
	// previous iteration feedback, discovered issues from prior passes)
	Context string
}

// RefinementConfig controls the refinement iteration behavior.
type RefinementConfig struct {
	// MinIterations ensures at least N refinement passes, even if AI thinks
	// convergence is reached earlier. This prevents premature convergence.
	// Default: 2-3 for most use cases.
	MinIterations int

	// MaxIterations sets a safety limit to prevent runaway iteration.
	// Default: 8-10 for most use cases.
	MaxIterations int

	// SkipSimple allows bypassing refinement for trivial tasks.
	// When true, the refiner can return immediately without iteration.
	SkipSimple bool

	// Timeout sets a maximum duration for the entire refinement process.
	// Zero means no timeout (rely on MaxIterations instead).
	Timeout time.Duration
}

// Refiner defines the interface for pluggable refinement strategies.
// Implementations provide domain-specific refinement logic (e.g., AnalysisRefiner,
// AssessmentRefiner) while the framework handles iteration mechanics.
type Refiner interface {
	// Refine performs one refinement pass on the artifact, returning an
	// improved version. The refiner should incorporate feedback from previous
	// iterations via the artifact's Context field.
	Refine(ctx context.Context, artifact *Artifact) (*Artifact, error)

	// CheckConvergence determines if the artifact has stabilized to a high-quality
	// state. This is AI-driven (ZFC compliance) - the AI judges convergence based
	// on the diff between current and previous versions, completeness, and
	// marginal value of further iteration.
	//
	// Returns a ConvergenceDecision containing the convergence judgment,
	// confidence level, reasoning, and strategy used. This aligns with the
	// ConvergenceDetector interface for consistency.
	CheckConvergence(ctx context.Context, current, previous *Artifact) (*ConvergenceDecision, error)
}

// ConvergenceResult captures the outcome of a refinement process.
type ConvergenceResult struct {
	// FinalArtifact is the converged artifact
	FinalArtifact *Artifact

	// Iterations is the number of refinement passes performed
	Iterations int

	// Converged indicates whether AI determined convergence (true) or
	// we hit max iterations (false)
	Converged bool

	// ElapsedTime is the total duration of the refinement process
	ElapsedTime time.Duration
}
