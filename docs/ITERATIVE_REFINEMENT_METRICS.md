# Iterative Refinement Metrics Guide

**Status:** vc-it8m (P1) - Metrics infrastructure complete, awaiting integration into analysis/assessment phases

This document explains how to collect, interpret, and act on iterative refinement metrics in VC.

---

## 📊 Overview

Iterative refinement metrics track the convergence behavior, cost, and quality improvement of AI-generated artifacts (assessments, analyses, issue breakdowns). These metrics validate the **4-5 iteration hypothesis** and enable data-driven tuning of refinement parameters.

**Key Questions Answered:**
- How many iterations does it take to reach convergence?
- What's the cost (tokens, USD) per artifact type?
- Is the AI convergence detector working correctly?
- Are we seeing quality improvement over iterations?
- Which artifact types benefit most from refinement?

---

## 🎯 The 4-5 Iteration Hypothesis

**Hypothesis:** Most AI artifacts converge to high quality in 4-5 refinement iterations, with diminishing returns beyond that point.

**Why it matters:**
- Too few iterations = premature convergence, missed issues
- Too many iterations = wasted cost, time with little quality gain
- Optimal range balances quality vs cost

**Validation via metrics:**
- Track actual mean/P50/P95 iterations to convergence
- Measure quality delta per iteration
- Compare cost of early-stop vs full refinement

**Expected results:**
- Mean iterations: 4-5
- P50: 4
- P95: 6-7
- Convergence rate: >80%

If actual data diverges significantly, adjust `MinIterations` and `MaxIterations` config.

---

## 🎯 Strategy Metrics

**What:** Which convergence detection strategy was used for each artifact.

**Available Strategies:**
- **`"AI"`** - AI-driven convergence detection (primary strategy for analysis/assessment)
- **`"diff-based"`** - Simple line-diff heuristics (fallback when AI fails)
- **`"chained"`** - Meta-strategy that chains multiple detectors with fallback

For detailed explanation of each strategy, see [ITERATIVE_REFINEMENT.md - Convergence Detection Strategies](ITERATIVE_REFINEMENT.md#convergence-detection-strategies).

**Metrics:**
- **Strategy distribution** - Count of each strategy used across all convergence checks
- **Average confidence by strategy** - Typical confidence level for each strategy
- **Fallback rate** - % of times diff-based strategy was used (indicates AI reliability)

**SQL query:**
```sql
SELECT
  json_extract(data, '$.strategy') as strategy,
  COUNT(*) as count,
  ROUND(AVG(json_extract(data, '$.confidence')), 2) as avg_confidence,
  ROUND(100.0 * COUNT(*) / (SELECT COUNT(*) FROM agent_events WHERE type = 'convergence_check'), 2) as percentage
FROM agent_events
WHERE type = 'convergence_check'
GROUP BY strategy
ORDER BY count DESC;
```

**Expected healthy distribution:**
```
strategy     | count | avg_confidence | percentage
-------------|-------|----------------|------------
AI           | 234   | 0.87           | 95.12
diff-based   | 12    | 0.72           | 4.88
chained      | 0     | 0.00           | 0.00
```

**Interpreting strategy patterns:**

| Pattern | Meaning | Action |
|---------|---------|--------|
| AI >90% | AI detection working well | ✅ No action needed |
| diff-based 5-10% | Occasional API issues or low confidence | ⚠️ Monitor, acceptable fallback rate |
| diff-based >20% | Frequent AI failures | 🚨 Investigate API reliability or lower MinConfidence |
| chained is common | Using ChainedDetector | ℹ️ Check if fallback logic is working as intended |
| Strategy shift over time | Detector behavior changing | 📊 Monitor for regressions or improvements |

**Tracking strategy over time:**
```sql
-- Strategy distribution by day
SELECT
  date(timestamp) as day,
  json_extract(data, '$.strategy') as strategy,
  COUNT(*) as count
FROM agent_events
WHERE type = 'convergence_check'
GROUP BY day, strategy
ORDER BY day DESC, count DESC;
```

**Cost implications by strategy:**
- **AI strategy**: ~$0.001-0.002 per convergence check (requires API call)
- **diff-based strategy**: $0.000 per check (local computation only)
- **chained strategy**: Variable (depends on which detector succeeds)

If diff-based fallback is triggered frequently (>20%), you're saving on AI costs but may be getting lower-quality convergence detection.

## 📈 Core Metrics

### Convergence Metrics

**What:** How often and how quickly artifacts reach AI-determined convergence.

**Metrics:**
- **Convergence rate** - % of artifacts that reached AI convergence (vs hitting MaxIterations)
- **Mean iterations** - Average iterations across all artifacts
- **P50/P95 iterations** - Median and 95th percentile (shows distribution)
- **Artifacts maxed out** - Count that hit MaxIterations without converging
- **Strategy distribution** - Breakdown of which convergence detector was used (AI, diff-based, chained)

**Good convergence:**
- Convergence rate: >80% (most artifacts converge naturally)
- Mean iterations: 4-5 (hypothesis validated)
- P95 < 8 (few outliers)
- AI strategy dominates: >90% (AI-based detection working well)

**Bad convergence (investigate):**
- Convergence rate: <60% (AI detector may be too strict, or MaxIterations too low)
- Mean iterations: >7 (wasting cost on diminishing returns)
- P95 > 10 (some artifacts never stabilize - check for bugs or complexity issues)
- Diff-based strategy frequent: >20% (AI detector failing or low confidence - investigate API issues)

### Cost Metrics

**What:** Token usage and estimated USD cost per artifact type.

**Metrics:**
- **Total input tokens** - Cumulative tokens in refinement prompts
- **Total output tokens** - Cumulative tokens in AI responses
- **Estimated cost USD** - Based on Claude Sonnet 4.5 pricing ($3/MTok input, $15/MTok output)
- **Cost per artifact** - Average cost to refine one artifact

**Interpreting cost:**
- Analysis artifacts are typically most expensive (largest content, deepest reasoning)
- Assessment artifacts are cheaper (shorter, more structured)
- Issue breakdowns are cheapest (simple decomposition)

**Cost targets:**
- Analysis: $0.10 - $0.50 per artifact
- Assessment: $0.05 - $0.20 per artifact
- Issue breakdown: $0.02 - $0.10 per artifact

If costs exceed these ranges, consider:
- Reducing `MaxIterations`
- Using smaller prompts (truncate context)
- Selective refinement (skip simple artifacts)

### Quality Improvement Metrics

**What:** Measured quality delta from initial to final artifact.

**Metrics:**
- **Quality improvement** - Domain-specific quality score delta
  - For analysis: Number of issues discovered in later iterations
  - For assessment: Confidence score improvement
  - For issue breakdown: Completeness/clarity improvement
- **Issues discovered** - Follow-on work found via this artifact

**Interpreting quality:**
- Positive quality delta = refinement is working
- Zero/negative delta = premature convergence or over-refinement
- High issues discovered = artifact uncovered hidden complexity

**Quality targets:**
- Mean quality improvement: >10% per artifact
- Issues discovered: 1-3 per analysis artifact
- False convergence rate: <5% (converged but missed issues)

### Latency Metrics

**What:** Time spent refining artifacts (wall-clock duration).

**Metrics:**
- **Total duration** - Time from refinement start to completion
- **Duration per iteration** - Average iteration time
- **Total duration vs iterations** - Correlation (more iterations = longer time)

**Interpreting latency:**
- Iteration time: 5-30 seconds (depends on artifact size, model speed)
- Total duration: 20-150 seconds (for 4-5 iterations)
- Outliers: >3 minutes total (investigate slow API calls or large artifacts)

**Latency is secondary to quality** - don't sacrifice convergence for speed.

---

## 🔍 How to Use the Metrics

### Step 1: Collect Metrics

**Enable metrics collection:**

```go
import "github.com/steveyegge/vc/internal/iterative"

// Create a metrics collector
collector := iterative.NewInMemoryMetricsCollector()

// During refinement
result, err := iterative.Converge(ctx, initial, refiner, config, collector)

// After refinement session
agg := collector.GetAggregateMetrics()
fmt.Printf("Convergence rate: %.2f%%\n", agg.ConvergenceRate())
fmt.Printf("Mean iterations: %.2f\n", agg.MeanIterations)
fmt.Printf("Estimated cost: $%.4f\n", agg.EstimatedCostUSD)
```

**Metrics are collected automatically** when you pass a non-nil collector to `Converge()`.

### Step 2: Analyze with SQL Queries

See [docs/QUERIES.md](QUERIES.md#-iterative-refinement-metrics-queries-vc-it8m) for comprehensive SQL queries.

**Quick health check:**

```sql
-- Overall convergence rate
SELECT
  COUNT(*) as total,
  SUM(CASE WHEN json_extract(data, '$.converged') = 1 THEN 1 ELSE 0 END) as converged,
  ROUND(AVG(json_extract(data, '$.total_iterations')), 2) as avg_iterations
FROM agent_events
WHERE type = 'refinement_completed';
```

**Cost analysis:**

```sql
SELECT
  json_extract(data, '$.artifact_type') as type,
  COUNT(*) as count,
  ROUND(SUM(json_extract(data, '$.total_input_tokens') + json_extract(data, '$.total_output_tokens')) * 3.0 / 1000000, 4) as cost_usd
FROM agent_events
WHERE type = 'refinement_completed'
GROUP BY type;
```

### Step 3: Interpret Results

**Scenario: Low convergence rate (50%)**

```
Problem: Most artifacts hit MaxIterations without converging
Possible causes:
  - AI convergence detector is too strict (MinConfidence too high)
  - MaxIterations is too low (artifacts need more iterations)
  - Artifacts are too complex (break into smaller pieces)

Actions:
  - Lower MinConfidence from 0.8 to 0.7
  - Increase MaxIterations from 7 to 10
  - Check for artifacts with >10 iterations (outliers)
```

**Scenario: Mean iterations = 2.5**

```
Problem: Artifacts converge too quickly
Possible causes:
  - AI convergence detector is too lenient
  - MinIterations is too low (not enough forced refinement)
  - Artifacts are too simple (may not need refinement)

Actions:
  - Increase MinConfidence from 0.7 to 0.85
  - Increase MinIterations from 2 to 3
  - Enable SkipSimple for trivial artifacts
```

**Scenario: High cost ($0.80 per analysis)**

```
Problem: Refinement is expensive
Possible causes:
  - Too many iterations (mean > 6)
  - Large artifacts (10K+ tokens)
  - Using expensive model (Opus instead of Sonnet)

Actions:
  - Lower MaxIterations from 10 to 7
  - Truncate artifact content in refinement prompts
  - Use Sonnet instead of Opus for refinement
```

### Step 4: Tune Configuration

**Default configuration (starting point):**

```go
config := iterative.RefinementConfig{
    MinIterations: 3,  // Force at least 3 passes
    MaxIterations: 7,  // Safety cap at 7
    SkipSimple:    true, // Skip trivial artifacts
    Timeout:       5 * time.Minute, // Bail after 5 min
}

detector := iterative.NewAIConvergenceDetector(supervisor, 0.8) // 80% confidence threshold
```

**Tuning based on metrics:**

| Metric Observed | Config Adjustment |
|----------------|-------------------|
| Convergence rate < 60% | Lower MinConfidence, increase MaxIterations |
| Mean iterations > 6 | Lower MaxIterations, increase MinConfidence |
| Mean iterations < 3 | Increase MinIterations, lower MinConfidence |
| P95 > 10 | Investigate outliers, add artifact type filtering |
| Cost > $1 per artifact | Lower MaxIterations, use cheaper model |
| Quality improvement < 5% | Increase MinIterations, check refiner logic |

---

## 🎛️ Configuration Parameters

### RefinementConfig

**MinIterations** (default: 3)
- Prevents premature convergence
- Higher = more forced refinement (better quality, higher cost)
- Lower = faster convergence (lower cost, risk of premature stop)

**MaxIterations** (default: 7)
- Safety cap to prevent runaway iteration
- Higher = more chances to converge (higher cost, longer time)
- Lower = faster bailout (lower cost, more maxed-out artifacts)

**SkipSimple** (default: true)
- Skip refinement for trivial artifacts (same content returned on first pass)
- Saves cost on simple tasks
- Disable if you want all artifacts refined

**Timeout** (default: 5 minutes)
- Wall-clock duration limit
- Prevents slow API calls from blocking
- Set based on your latency tolerance

### AIConvergenceDetector

**MinConfidence** (default: 0.8)
- Minimum AI confidence to accept convergence judgment
- Higher = stricter convergence (more iterations, higher quality)
- Lower = lenient convergence (fewer iterations, risk of false convergence)

**Typical values:**
- 0.7 - Lenient (faster convergence, accept some uncertainty)
- 0.8 - Balanced (default)
- 0.9 - Strict (high confidence required, more iterations)

---

## 📊 Example Metrics Analysis

**Sample aggregate metrics:**

```
Total artifacts: 142
Converged: 118 (83%)
Maxed out: 24 (17%)

Mean iterations: 4.3
P50 iterations: 4
P95 iterations: 7

Total input tokens: 1,247,893
Total output tokens: 2,834,221
Estimated cost: $46.25

By artifact type:
  - Analysis: 67 artifacts, 5.1 avg iterations, $28.40
  - Assessment: 52 artifacts, 3.8 avg iterations, $14.32
  - Issue breakdown: 23 artifacts, 3.2 avg iterations, $3.53
```

**Interpretation:**
- ✅ Convergence rate (83%) is healthy
- ✅ Mean iterations (4.3) validates 4-5 hypothesis
- ✅ P50 (4) and P95 (7) show good distribution
- ⚠️ Analysis artifacts take slightly more iterations (5.1) - consider if this is acceptable
- ⚠️ 17% maxed out - acceptable, but could increase MaxIterations if concerned
- 💰 Cost ($46.25 / 142 = $0.33 per artifact) is reasonable

**Actions:**
- No immediate tuning needed
- Monitor maxed-out artifacts for patterns
- Consider raising MaxIterations to 8 if analysis quality suffers

---

## 🧪 Validation Queries

**Check if refinement is working (quality improvement):**

```sql
-- Artifacts should get smaller diffs over iterations
SELECT
  json_extract(data, '$.iteration') as iteration,
  ROUND(AVG(json_extract(data, '$.diff_percent')), 2) as avg_diff_pct
FROM agent_events
WHERE type = 'refinement_iteration'
GROUP BY iteration
ORDER BY iteration;
```

**Expected:** Diff percentage should decrease over iterations (e.g., 25% → 15% → 8% → 3%).

**Check for false convergence:**

```sql
-- Artifacts that converged but later spawned issues
SELECT
  issue_id,
  json_extract(data, '$.total_iterations') as iterations,
  json_extract(data, '$.issues_discovered') as issues_found
FROM agent_events
WHERE type = 'refinement_completed'
  AND json_extract(data, '$.converged') = 1
  AND json_extract(data, '$.total_iterations') < 4
  AND json_extract(data, '$.issues_discovered') > 2;
```

**Expected:** Very few results (false convergence rate < 5%).

---

## 🚀 Next Steps

1. **Integrate into workflows** (vc-t9ls, vc-43kd)
   - Add metrics collection to analysis phase
   - Add metrics collection to assessment phase
   - Emit events to activity feed

2. **Build monitoring dashboard** (future)
   - Real-time convergence rate
   - Cost burn rate alerts
   - Quality improvement trends

3. **Automated tuning** (future)
   - Use metrics to auto-adjust MinIterations/MaxIterations
   - Adaptive convergence thresholds based on artifact type

---

## 📚 Related Docs

- [docs/QUERIES.md](QUERIES.md#-iterative-refinement-metrics-queries-vc-it8m) - SQL queries for metrics analysis
- [docs/FEATURES.md](FEATURES.md) - Deep dive on iterative refinement feature
- [internal/iterative/metrics.go](../internal/iterative/metrics.go) - Metrics collector implementation
- [internal/iterative/detector.go](../internal/iterative/detector.go) - Convergence detection

---

**Status:** Metrics infrastructure is complete and ready for integration. Next: vc-t9ls (integrate into analysis phase).
