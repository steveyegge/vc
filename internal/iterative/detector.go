package iterative

import (
	"context"
	"fmt"
	"math"
	"strings"

	"github.com/hexops/gotextdiff"
	"github.com/hexops/gotextdiff/myers"
	"github.com/hexops/gotextdiff/span"

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

// DiffBasedDetector is a fallback convergence detector that uses the Myers
// diff algorithm (same algorithm used by git and gopls) for accurate change
// detection. If changes are below threshold, assume converged.
//
// Advanced Features for Code Restructuring:
//
// The detector supports three optional modes to handle common AI refinement patterns:
//
// 1. IgnoreWhitespace: Normalizes whitespace before diffing to ignore pure formatting changes.
//    Use case: AI often reformats code (indentation, spacing) without semantic changes.
//    Tradeoff: May miss intentional formatting fixes. Enable when formatting is not critical.
//
// 2. IgnoreComments: Filters out comment-only changes from the diff count.
//    Use case: AI frequently adds/improves documentation without changing logic.
//    Tradeoff: May miss important documentation updates. Enable when doc changes shouldn't
//    prevent convergence.
//
// 3. SemanticRestructuring: Applies heuristics to detect code restructuring that preserves
//    semantics (refactoring, reordering). When a hunk has equal deletions and insertions
//    (70%+ overlap), it's weighted at 50% to reflect potential semantic equivalence.
//    Use case: AI often refactors code (extract function, reorder blocks) without changing behavior.
//    Tradeoff: May undercount real logic changes that happen to have balanced del/ins.
//    Enable when refactoring patterns are common.
//
// Recommendation: Start with default settings (all false). Enable options only if you observe
// convergence issues due to harmless reformatting/restructuring. The AI-based detector is
// better at true semantic understanding; this detector is a fast fallback.
type DiffBasedDetector struct {
	// MaxDiffPercent is the maximum percentage of changed lines to consider converged
	// Default: 5% (if less than 5% of lines changed, consider converged)
	MaxDiffPercent float64

	// IgnoreWhitespace controls whether pure whitespace changes are ignored
	// When true, lines that differ only in whitespace are not counted as changes
	IgnoreWhitespace bool

	// IgnoreComments controls whether comment-only changes are ignored
	// When true, lines that appear to be comment additions/removals are weighted less
	IgnoreComments bool

	// SemanticRestructuring controls whether to apply heuristics for detecting
	// code restructuring that preserves semantics (e.g., refactoring, reordering)
	// When true, certain structural changes are weighted less in diff calculations
	SemanticRestructuring bool
}

// NewDiffBasedDetector creates a diff-based convergence detector with default settings
func NewDiffBasedDetector(maxDiffPercent float64) *DiffBasedDetector {
	if maxDiffPercent <= 0 {
		maxDiffPercent = 5.0 // Default: 5% change threshold
	}
	return &DiffBasedDetector{
		MaxDiffPercent:        maxDiffPercent,
		IgnoreWhitespace:      false,
		IgnoreComments:        false,
		SemanticRestructuring: false,
	}
}

// NewDiffBasedDetectorWithOptions creates a diff-based convergence detector with custom options
func NewDiffBasedDetectorWithOptions(maxDiffPercent float64, ignoreWhitespace, ignoreComments, semanticRestructuring bool) *DiffBasedDetector {
	if maxDiffPercent <= 0 {
		maxDiffPercent = 5.0 // Default: 5% change threshold
	}
	return &DiffBasedDetector{
		MaxDiffPercent:        maxDiffPercent,
		IgnoreWhitespace:      ignoreWhitespace,
		IgnoreComments:        ignoreComments,
		SemanticRestructuring: semanticRestructuring,
	}
}

// CheckConvergence uses diff size to determine convergence
func (d *DiffBasedDetector) CheckConvergence(ctx context.Context, current, previous *Artifact) (*ConvergenceDecision, error) {
	opts := diffOptions{
		ignoreWhitespace:      d.IgnoreWhitespace,
		ignoreComments:        d.IgnoreComments,
		semanticRestructuring: d.SemanticRestructuring,
	}
	diffLines := countDiffLinesWithOptions(previous.Content, current.Content, opts)
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

	reasoning := buildReasoningString(changePercent, d.MaxDiffPercent, opts)

	return &ConvergenceDecision{
		Converged:  converged,
		Confidence: confidence,
		Reasoning:  reasoning,
		Strategy:   "diff-based",
	}, nil
}

func buildReasoningString(changePercent, threshold float64, opts diffOptions) string {
	base := fmt.Sprintf("%.1f%% of lines changed (threshold: %.1f%%)", changePercent, threshold)
	var flags []string
	if opts.ignoreWhitespace {
		flags = append(flags, "ignoring whitespace")
	}
	if opts.ignoreComments {
		flags = append(flags, "ignoring comments")
	}
	if opts.semanticRestructuring {
		flags = append(flags, "detecting semantic restructuring")
	}
	if len(flags) > 0 {
		return fmt.Sprintf("%s [%s]", base, strings.Join(flags, ", "))
	}
	return base
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

// diffOptions controls how diffs are computed and what changes to consider significant
type diffOptions struct {
	ignoreWhitespace      bool
	ignoreComments        bool
	semanticRestructuring bool
}

// countDiffLines is a backward-compatible wrapper that uses default options
func countDiffLines(prev, current string) int {
	return countDiffLinesWithOptions(prev, current, diffOptions{})
}

// countDiffLinesWithOptions computes the diff with customizable handling of different change types
func countDiffLinesWithOptions(prev, current string, opts diffOptions) int {
	// Use Myers diff algorithm (same algorithm used by git and gopls)
	// This properly handles line reordering, insertions, and deletions
	// without being fooled by simple position changes

	// Normalize trailing newlines to avoid spurious diffs
	// The diff algorithm is sensitive to whether the last line has a newline
	prevNorm := normalizeNewlines(prev)
	currentNorm := normalizeNewlines(current)

	// Apply whitespace normalization if requested
	if opts.ignoreWhitespace {
		prevNorm = normalizeWhitespace(prevNorm)
		currentNorm = normalizeWhitespace(currentNorm)
	}

	edits := myers.ComputeEdits(span.URIFromPath("prev"), prevNorm, currentNorm)
	unified := gotextdiff.ToUnified("prev", "current", prevNorm, edits)

	// Count "changed regions" from the unified diff
	// For each hunk, we count the max of deletions vs insertions
	// This gives us a count that matches intuitive understanding:
	// - Changed line: 1 delete + 1 insert = max(1,1) = 1
	// - Added lines: 0 deletes + N inserts = max(0,N) = N
	// - Removed lines: N deletes + 0 inserts = max(N,0) = N
	diffCount := 0
	for _, hunk := range unified.Hunks {
		deletions := 0
		insertions := 0
		commentOnlyDeletions := 0
		commentOnlyInsertions := 0

		for _, line := range hunk.Lines {
			switch line.Kind {
			case gotextdiff.Delete:
				deletions++
				if opts.ignoreComments && isCommentLine(line.Content) {
					commentOnlyDeletions++
				}
			case gotextdiff.Insert:
				insertions++
				if opts.ignoreComments && isCommentLine(line.Content) {
					commentOnlyInsertions++
				}
			}
		}

		// If ignoring comments, subtract comment-only changes
		if opts.ignoreComments {
			deletions -= commentOnlyDeletions
			insertions -= commentOnlyInsertions
		}

		// Apply semantic restructuring heuristics if enabled
		// If a hunk has equal deletions and insertions, it might be a refactoring
		// Weight it at 50% to reflect potential semantic equivalence
		hunkDiff := maxInt(deletions, insertions)
		if opts.semanticRestructuring && deletions > 0 && insertions > 0 {
			// Equal or near-equal del/ins suggests restructuring, not new logic
			ratio := float64(minInt(deletions, insertions)) / float64(maxInt(deletions, insertions))
			if ratio > 0.7 { // If 70%+ overlap, likely restructuring
				hunkDiff = int(float64(hunkDiff) * 0.5) // Weight at 50%
			}
		}

		diffCount += hunkDiff
	}

	return diffCount
}

// normalizeNewlines ensures consistent trailing newline handling
// Empty strings stay empty, non-empty strings get exactly one trailing newline
func normalizeNewlines(s string) string {
	if s == "" {
		return s
	}
	// Remove any trailing newlines, then add exactly one
	s = strings.TrimRight(s, "\n")
	return s + "\n"
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// normalizeWhitespace collapses all whitespace sequences to single spaces
// and trims leading/trailing whitespace on each line. This allows detection
// of pure indentation/formatting changes.
func normalizeWhitespace(text string) string {
	lines := strings.Split(text, "\n")
	var normalized []string
	for _, line := range lines {
		// Trim leading and trailing whitespace
		trimmed := strings.TrimSpace(line)
		// Collapse internal whitespace sequences to single spaces
		fields := strings.Fields(trimmed)
		normalized = append(normalized, strings.Join(fields, " "))
	}
	return strings.Join(normalized, "\n")
}

// isCommentLine attempts to detect if a line is a comment
// This is a simple heuristic that works for many languages:
// - Starts with // (C-style, Go, Rust, etc.)
// - Starts with # (Python, Ruby, Shell, etc.)
// - Starts with -- (SQL, Lua, etc.)
// - Contains /* or */ (C-style block comments)
// - Starts with * (continuation of block comment)
func isCommentLine(line string) bool {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" {
		return false
	}

	// Check common comment prefixes
	commentPrefixes := []string{"//", "#", "--", "/*", "*/", "*"}
	for _, prefix := range commentPrefixes {
		if strings.HasPrefix(trimmed, prefix) {
			return true
		}
	}

	return false
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
