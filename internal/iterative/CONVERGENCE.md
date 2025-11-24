# Convergence Detection Strategies

This document describes the convergence detection strategies implemented for iterative refinement (vc-b32j).

## Overview

The `ConvergenceDetector` interface allows pluggable strategies for determining when an AI-generated artifact has stabilized and reached high quality. The framework provides three implementations:

1. **AIConvergenceDetector** - AI-driven meta-cognition (primary)
2. **DiffBasedDetector** - Simple diff heuristics (fallback)
3. **ChainedDetector** - Chains multiple detectors with fallback logic

## Design Philosophy

Following Zero Framework Cognition (ZFC) principles, convergence detection delegates the judgment to AI while providing fallback strategies for reliability:

- **AI-driven primary strategy**: Let the AI judge convergence based on semantic understanding
- **Heuristic fallbacks**: Simple diff-based rules when AI is unavailable or uncertain
- **Confidence thresholds**: Only accept detector results with sufficient confidence
- **Timeout safeguards**: Hard limits (MaxIterations) prevent runaway iteration

## AIConvergenceDetector

The primary convergence strategy uses AI supervision to make intelligent convergence judgments.

### How It Works

The AI receives a structured prompt containing:
- Previous version of the artifact
- Current (refined) version
- Artifact context
- Diff metrics (changed lines, change percentage)

The AI considers:
1. **Diff size**: Are changes minimal/superficial or substantive?
2. **Completeness**: Are all key concerns addressed?
3. **Gaps**: Are there obvious missing elements?
4. **Marginal value**: Would another iteration yield meaningful improvement?

### Response Format

The AI returns structured JSON:

```json
{
  "converged": true/false,
  "confidence": 0.0-1.0,
  "reasoning": "Brief explanation",
  "diff_size": "minimal|small|moderate|large",
  "marginal": "none|low|medium|high"
}
```

### Configuration

```go
detector := iterative.NewAIConvergenceDetector(supervisor, minConfidence)
```

Parameters:
- `supervisor`: AI supervisor instance
- `minConfidence`: Minimum confidence threshold (0.0-1.0, default 0.8)

If the AI's confidence is below `minConfidence`, the detector treats it as "not converged" to avoid premature convergence on uncertain judgments.

### Example Usage

```go
aiDetector := iterative.NewAIConvergenceDetector(supervisor, 0.8)

converged, confidence, err := aiDetector.CheckConvergence(ctx, current, previous)
if err != nil {
    // Handle error
}

if converged && confidence >= 0.8 {
    fmt.Println("Artifact has converged with high confidence")
}
```

## DiffBasedDetector

A fallback strategy that uses Myers diff algorithm (same algorithm used by git and gopls) for accurate change detection.

### How It Works

Uses the Myers diff algorithm to compute differences:
- Computes optimal edit sequence between previous and current versions
- Counts changed lines from the unified diff output
- Handles line reordering intelligently (doesn't count moved lines as multiple changes)
- Calculates change percentage relative to total lines
- If change % < threshold, assumes converged

**Key improvement over naive line-by-line comparison:**
- Line reordering is detected accurately (moved lines don't inflate diff count)
- Structural changes (like refactoring) are measured by actual content changes
- Whitespace-only changes are counted precisely

### Configuration

```go
detector := iterative.NewDiffBasedDetector(maxDiffPercent)
```

Parameters:
- `maxDiffPercent`: Maximum percentage of changed lines (default 5.0)

### Confidence Calculation

Confidence is based on distance from the threshold:
- Far below threshold → high confidence (converged)
- Far above threshold → high confidence (not converged)
- Near threshold → low confidence

This allows `ChainedDetector` to fall back when diff-based confidence is low.

### Example Usage

```go
diffDetector := iterative.NewDiffBasedDetector(5.0) // 5% change threshold

converged, confidence, err := diffDetector.CheckConvergence(ctx, current, previous)
if err != nil {
    // Handle error
}

fmt.Printf("Converged: %v, Confidence: %.2f\n", converged, confidence)
```

## ChainedDetector

Chains multiple detectors with intelligent fallback logic.

### How It Works

Tries each detector in sequence:
1. Call first detector (typically AI-driven)
2. If error or confidence < threshold, try next detector
3. If detector succeeds with confidence ≥ threshold, use that result
4. If all detectors fail, return error
5. If all detectors have low confidence, return last result

### Configuration

```go
detector := iterative.NewChainedDetector(minConfidence, detector1, detector2, ...)
```

Parameters:
- `minConfidence`: Minimum confidence threshold (default 0.7)
- `detectors`: Variable number of detectors to chain

### Recommended Setup

```go
// AI primary, diff-based fallback
aiDetector := iterative.NewAIConvergenceDetector(supervisor, 0.8)
diffDetector := iterative.NewDiffBasedDetector(5.0)
chainedDetector := iterative.NewChainedDetector(0.7, aiDetector, diffDetector)
```

This setup:
- Uses AI for intelligent convergence judgment (requires confidence ≥ 0.8)
- Falls back to diff heuristics if AI fails or has low confidence
- Only accepts results with overall confidence ≥ 0.7

### Example Usage

```go
chained := iterative.NewChainedDetector(0.7,
    iterative.NewAIConvergenceDetector(supervisor, 0.8),
    iterative.NewDiffBasedDetector(5.0),
)

converged, confidence, err := chained.CheckConvergence(ctx, current, previous)
if err != nil {
    // All detectors failed - handle error
}
```

## Integration with Refiner

The `Refiner` interface delegates to `ConvergenceDetector`:

```go
type MyRefiner struct {
    supervisor *ai.Supervisor
    detector   iterative.ConvergenceDetector
}

func NewMyRefiner(supervisor *ai.Supervisor) *MyRefiner {
    // Set up chained detector
    aiDetector := iterative.NewAIConvergenceDetector(supervisor, 0.8)
    diffDetector := iterative.NewDiffBasedDetector(5.0)
    detector := iterative.NewChainedDetector(0.7, aiDetector, diffDetector)

    return &MyRefiner{
        supervisor: supervisor,
        detector:   detector,
    }
}

func (r *MyRefiner) CheckConvergence(ctx context.Context, current, previous *iterative.Artifact) (bool, error) {
    converged, confidence, err := r.detector.CheckConvergence(ctx, current, previous)
    if err != nil {
        return false, err
    }

    // Log convergence decision
    fmt.Printf("Convergence check: converged=%v, confidence=%.2f\n", converged, confidence)

    return converged, nil
}
```

## Metrics Tracking

Future work (see acceptance criteria in vc-b32j):

- **Convergence rate**: % of artifacts that reach convergence before MaxIterations
- **False positives**: Artifacts marked converged that needed more iteration
- **Mean iterations**: Average number of iterations to convergence
- **Detector effectiveness**: Which detector is used most often in chained setup

These metrics will help tune confidence thresholds and diff percentages.

## Cost Considerations

**AI Convergence Detection Cost:**
- ~1 API call per iteration (after MinIterations)
- ~2K input tokens (artifact diffs + prompt)
- ~100 output tokens (JSON response)
- ~$0.006 per check with Sonnet 4.5
- For 5 iterations with 3 convergence checks: ~$0.018

**Total cost is negligible** compared to agent execution costs (~$1-10 per issue).

## Error Handling

All detectors follow consistent error handling:

- **API errors** (AI detector): Propagated immediately to caller
- **Parse errors** (AI detector): Wrapped with context, propagated to caller
- **Detector failures** (Chained): Try next detector in chain
- **All detectors fail** (Chained): Return error with context

The `Converge` function handles convergence check errors gracefully:
- Logs error but doesn't fail refinement
- Falls back to MaxIterations as safety limit

## Testing

See `detector_test.go` for comprehensive tests:

- **DiffBasedDetector**: Line diff calculations, confidence, edge cases
- **AIConvergenceDetector**: Prompt building (integration tests with real AI TBD)
- **ChainedDetector**: Fallback logic, confidence thresholds
- **Helper functions**: countLines, countDiffLines, truncation

## Future Enhancements

Potential improvements (out of scope for vc-b32j):

1. **Semantic similarity**: Compare embeddings of current vs. previous
2. **Learned thresholds**: Adjust confidence thresholds based on historical data
3. **Domain-specific detectors**: Different strategies for different artifact types
4. **Convergence metrics dashboard**: Real-time monitoring of convergence patterns

## References

- Parent epic: vc-x1t4 (Iterative Refinement for AI-Generated Work)
- Core framework: vc-43no (Core iterative refinement framework)
- Integration: vc-t9ls (Analysis phase), vc-43kd (Assessment phase)
