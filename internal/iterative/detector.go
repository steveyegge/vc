package iterative

import (
	"context"
	"fmt"
	"math"
	"strings"

	"github.com/steveyegge/vc/internal/ai"
)

// ConvergenceDetector determines whether an artifact has converged to a stable,
// high-quality state. Implementations use different strategies (AI-driven,
// diff-based, semantic similarity) to judge convergence.
type ConvergenceDetector interface {
	// CheckConvergence determines if the artifact has stabilized.
	// Returns true if converged, false if more iteration would help.
	// confidence is a value between 0.0 and 1.0 indicating detector confidence.
	CheckConvergence(ctx context.Context, current, previous *Artifact) (converged bool, confidence float64, err error)
}

// ConvergenceDecision captures the outcome of a single convergence check
type ConvergenceDecision struct {
	Converged  bool    // Whether the artifact has converged
	Confidence float64 // Confidence in the judgment (0.0-1.0)
	Reasoning  string  // Explanation for the decision
	Strategy   string  // Which detection strategy was used
}

// AIConvergenceDetector uses AI supervision to judge convergence.
// This is the primary strategy - AI considers diff size, completeness,
// gaps, and marginal value of further iteration.
type AIConvergenceDetector struct {
	supervisor *ai.Supervisor
	// MinConfidence is the minimum confidence threshold (0.0-1.0)
	// If AI confidence is below this, we treat it as "not converged"
	MinConfidence float64
}

// NewAIConvergenceDetector creates an AI-driven convergence detector
func NewAIConvergenceDetector(supervisor *ai.Supervisor, minConfidence float64) (*AIConvergenceDetector, error) {
	if supervisor == nil {
		return nil, fmt.Errorf("supervisor cannot be nil")
	}
	if minConfidence <= 0 {
		minConfidence = 0.8 // Default: require high confidence
	}
	return &AIConvergenceDetector{
		supervisor:    supervisor,
		MinConfidence: minConfidence,
	}, nil
}

// aiConvergenceResponse is the structured response from the AI
type aiConvergenceResponse struct {
	Converged  bool    `json:"converged"`            // Has the artifact converged?
	Confidence float64 `json:"confidence"`           // Confidence in judgment (0.0-1.0)
	Reasoning  string  `json:"reasoning"`            // Explanation
	DiffSize   string  `json:"diff_size,omitempty"`  // "minimal", "small", "moderate", "large"
	Marginal   string  `json:"marginal,omitempty"`   // "none", "low", "medium", "high"
}

// CheckConvergence uses AI to determine if the artifact has converged
func (d *AIConvergenceDetector) CheckConvergence(ctx context.Context, current, previous *Artifact) (bool, float64, error) {
	prompt := d.buildConvergencePrompt(current, previous)

	// Use the AI supervisor's raw message API to get structured response
	response, err := d.supervisor.CallAPI(ctx, prompt, ai.ModelSonnet, 2048)
	if err != nil {
		return false, 0.0, fmt.Errorf("AI convergence check failed: %w", err)
	}

	// Extract response text
	var responseText string
	for _, block := range response.Content {
		if block.Type == "text" {
			responseText += block.Text
		}
	}

	// Parse the structured response
	parseResult := ai.Parse[aiConvergenceResponse](responseText, ai.ParseOptions{
		Context:   "convergence check",
		LogErrors: boolPtr(true),
	})
	if !parseResult.Success {
		return false, 0.0, fmt.Errorf("failed to parse AI convergence response: %s", parseResult.Error)
	}

	aiResponse := parseResult.Data

	// Apply confidence threshold
	if aiResponse.Confidence < d.MinConfidence {
		// AI is not confident enough - treat as not converged
		return false, aiResponse.Confidence, nil
	}

	return aiResponse.Converged, aiResponse.Confidence, nil
}

// buildConvergencePrompt constructs the AI convergence judgment prompt
func (d *AIConvergenceDetector) buildConvergencePrompt(current, previous *Artifact) string {
	// Calculate simple diff metrics for AI context
	diffLines := countDiffLines(previous.Content, current.Content)
	totalLines := countLines(current.Content)

	return fmt.Sprintf(`You are judging whether an AI-generated artifact has converged to a stable, high-quality state through iterative refinement.

ARTIFACT TYPE: %s

PREVIOUS VERSION:
%s

CURRENT VERSION:
%s

CONTEXT:
%s

DIFF METRICS:
- Changed lines: ~%d (out of %d total)
- Change percentage: ~%.1f%%

YOUR TASK:
Determine if this artifact has converged (reached stability and high quality) or if another refinement iteration would meaningfully improve it.

Consider these factors:
1. **Diff size**: Are changes minimal/superficial, or substantive?
2. **Completeness**: Are all key concerns addressed?
3. **Gaps**: Are there obvious missing elements or improvements?
4. **Marginal value**: Would another iteration yield meaningful improvement, or are we at diminishing returns?

IMPORTANT GUIDELINES:
- Minimal diff + high completeness = likely converged
- Large substantive changes = NOT converged (artifact still evolving)
- Small refinements of already-good content = likely converged
- New sections or restructuring = NOT converged
- If changes are just stylistic polish = likely converged
- If changes fix actual gaps or errors = may not be converged yet

Respond with JSON:
{
  "converged": true/false,
  "confidence": 0.0-1.0,
  "reasoning": "Brief explanation of your judgment",
  "diff_size": "minimal|small|moderate|large",
  "marginal": "none|low|medium|high"
}

Where:
- converged: true if artifact is stable and high-quality
- confidence: 0.0-1.0 (how confident are you in this judgment?)
- reasoning: 1-2 sentences explaining why
- diff_size: how much changed between versions
- marginal: expected value of another iteration (none=converged, high=needs more work)`,
		current.Type,
		truncateForPrompt(previous.Content, 3000),
		truncateForPrompt(current.Content, 3000),
		truncateForPrompt(current.Context, 1000),
		diffLines,
		totalLines,
		float64(diffLines)/float64(maxInt(totalLines, 1))*100,
	)
}

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
func (d *DiffBasedDetector) CheckConvergence(ctx context.Context, current, previous *Artifact) (bool, float64, error) {
	diffLines := countDiffLines(previous.Content, current.Content)
	totalLines := countLines(current.Content)

	if totalLines == 0 {
		// Empty artifact - not converged
		return false, 1.0, nil
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

	return converged, confidence, nil
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
func (d *ChainedDetector) CheckConvergence(ctx context.Context, current, previous *Artifact) (bool, float64, error) {
	var lastErr error
	var lastConfidence float64

	for i, detector := range d.detectors {
		converged, confidence, err := detector.CheckConvergence(ctx, current, previous)

		// If this detector succeeded and has sufficient confidence, use it
		if err == nil && confidence >= d.MinConfidence {
			return converged, confidence, nil
		}

		// Track last error and confidence for fallback
		lastErr = err
		lastConfidence = confidence

		// If this was the last detector and it failed, return the error
		if i == len(d.detectors)-1 && err != nil {
			return false, 0.0, fmt.Errorf("all convergence detectors failed, last error: %w", lastErr)
		}

		// Otherwise, try the next detector
	}

	// All detectors had low confidence - return the last result
	// (This handles the case where all detectors succeed but with low confidence)
	return false, lastConfidence, nil
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

func truncateForPrompt(text string, maxChars int) string {
	if len(text) <= maxChars {
		return text
	}
	return text[:maxChars] + "... (truncated)"
}

func boolPtr(b bool) *bool {
	return &b
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
