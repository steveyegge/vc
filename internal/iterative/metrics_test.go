package iterative

import (
	"context"
	"testing"
	"time"
)

func TestInMemoryMetricsCollector_BasicCollection(t *testing.T) {
	collector := NewInMemoryMetricsCollector()

	// Simulate a simple refinement process
	collector.RecordIterationStart(1)
	collector.RecordIterationEnd(1, &IterationMetrics{
		Iteration:    1,
		InputTokens:  100,
		OutputTokens: 200,
		DiffLines:    10,
		DiffPercent:  5.0,
		Duration:     100 * time.Millisecond,
	})

	collector.RecordIterationStart(2)
	collector.RecordIterationEnd(2, &IterationMetrics{
		Iteration:          2,
		InputTokens:        110,
		OutputTokens:       220,
		DiffLines:          2,
		DiffPercent:        1.0,
		Duration:           90 * time.Millisecond,
		ConvergenceChecked: true,
		ConvergedThis:      true,
		Confidence:         0.85,
	})

	result := &ConvergenceResult{
		FinalArtifact: &Artifact{Type: "analysis", Content: "final"},
		Iterations:    2,
		Converged:     true,
		ElapsedTime:   200 * time.Millisecond,
	}

	artifactMetrics := &ArtifactMetrics{
		ArtifactType:      "analysis",
		Priority:          "P1",
		TotalIterations:   2,
		Converged:         true,
		ConvergenceReason: "AI convergence",
		TotalDuration:     200 * time.Millisecond,
		TotalInputTokens:  210,
		TotalOutputTokens: 420,
	}

	collector.RecordArtifactComplete(result, artifactMetrics)

	// Verify artifact was recorded
	artifacts := collector.GetArtifacts()
	if len(artifacts) != 1 {
		t.Fatalf("Expected 1 artifact, got %d", len(artifacts))
	}

	artifact := artifacts[0]
	if artifact.ArtifactType != "analysis" {
		t.Errorf("Expected type 'analysis', got %q", artifact.ArtifactType)
	}
	if artifact.TotalIterations != 2 {
		t.Errorf("Expected 2 iterations, got %d", artifact.TotalIterations)
	}
	if !artifact.Converged {
		t.Error("Expected converged=true")
	}

	// Verify iteration metrics were attached
	if len(artifact.Iterations) != 2 {
		t.Fatalf("Expected 2 iteration metrics, got %d", len(artifact.Iterations))
	}

	iter1 := artifact.Iterations[0]
	if iter1.InputTokens != 100 {
		t.Errorf("Expected 100 input tokens, got %d", iter1.InputTokens)
	}

	iter2 := artifact.Iterations[1]
	if !iter2.ConvergenceChecked {
		t.Error("Expected convergence checked on iteration 2")
	}
}

func TestInMemoryMetricsCollector_AggregateMetrics(t *testing.T) {
	collector := NewInMemoryMetricsCollector()

	// Record multiple artifacts with varying characteristics
	artifacts := []*ArtifactMetrics{
		{
			ArtifactType:      "analysis",
			Priority:          "P1",
			TotalIterations:   3,
			Converged:         true,
			TotalInputTokens:  300,
			TotalOutputTokens: 600,
			TotalDuration:     300 * time.Millisecond,
		},
		{
			ArtifactType:      "analysis",
			Priority:          "P2",
			TotalIterations:   5,
			Converged:         true,
			TotalInputTokens:  500,
			TotalOutputTokens: 1000,
			TotalDuration:     500 * time.Millisecond,
		},
		{
			ArtifactType:      "assessment",
			Priority:          "P1",
			TotalIterations:   7,
			Converged:         false,
			TotalInputTokens:  700,
			TotalOutputTokens: 1400,
			TotalDuration:     700 * time.Millisecond,
		},
		{
			ArtifactType:      "analysis",
			Priority:          "P1",
			TotalIterations:   4,
			Converged:         true,
			TotalInputTokens:  400,
			TotalOutputTokens: 800,
			TotalDuration:     400 * time.Millisecond,
		},
	}

	for _, artifact := range artifacts {
		result := &ConvergenceResult{
			Iterations:  artifact.TotalIterations,
			Converged:   artifact.Converged,
			ElapsedTime: artifact.TotalDuration,
		}
		collector.RecordArtifactComplete(result, artifact)
	}

	agg := collector.GetAggregateMetrics()

	// Verify overall counts
	if agg.TotalArtifacts != 4 {
		t.Errorf("Expected 4 total artifacts, got %d", agg.TotalArtifacts)
	}
	if agg.ConvergedArtifacts != 3 {
		t.Errorf("Expected 3 converged artifacts, got %d", agg.ConvergedArtifacts)
	}
	if agg.MaxedOutArtifacts != 1 {
		t.Errorf("Expected 1 maxed out artifact, got %d", agg.MaxedOutArtifacts)
	}

	// Verify mean iterations
	expectedMean := float64(3+5+7+4) / 4.0 // = 4.75
	if agg.MeanIterations != expectedMean {
		t.Errorf("Expected mean iterations %.2f, got %.2f", expectedMean, agg.MeanIterations)
	}

	// Verify percentiles (sorted converged: [3, 4, 5])
	if agg.P50Iterations != 4 {
		t.Errorf("Expected P50=4, got %d", agg.P50Iterations)
	}
	if agg.P95Iterations != 5 {
		t.Errorf("Expected P95=5, got %d", agg.P95Iterations)
	}

	// Verify token counts
	expectedInput := int64(300 + 500 + 700 + 400)
	if agg.TotalInputTokens != expectedInput {
		t.Errorf("Expected %d input tokens, got %d", expectedInput, agg.TotalInputTokens)
	}

	expectedOutput := int64(600 + 1000 + 1400 + 800)
	if agg.TotalOutputTokens != expectedOutput {
		t.Errorf("Expected %d output tokens, got %d", expectedOutput, agg.TotalOutputTokens)
	}

	// Verify cost estimation (very rough check)
	if agg.EstimatedCostUSD <= 0 {
		t.Error("Expected positive cost estimate")
	}

	// Verify breakdown by type
	if len(agg.ByType) != 2 {
		t.Errorf("Expected 2 types, got %d", len(agg.ByType))
	}

	analysisMetrics := agg.ByType["analysis"]
	if analysisMetrics == nil {
		t.Fatal("Expected 'analysis' type metrics")
	}
	if analysisMetrics.Count != 3 {
		t.Errorf("Expected 3 analysis artifacts, got %d", analysisMetrics.Count)
	}
	if analysisMetrics.ConvergedCount != 3 {
		t.Errorf("Expected 3 converged analysis artifacts, got %d", analysisMetrics.ConvergedCount)
	}

	assessmentMetrics := agg.ByType["assessment"]
	if assessmentMetrics == nil {
		t.Fatal("Expected 'assessment' type metrics")
	}
	if assessmentMetrics.Count != 1 {
		t.Errorf("Expected 1 assessment artifact, got %d", assessmentMetrics.Count)
	}
	if assessmentMetrics.ConvergedCount != 0 {
		t.Errorf("Expected 0 converged assessment artifacts, got %d", assessmentMetrics.ConvergedCount)
	}

	// Verify breakdown by priority
	if len(agg.ByPriority) != 2 {
		t.Errorf("Expected 2 priorities, got %d", len(agg.ByPriority))
	}

	p1Metrics := agg.ByPriority["P1"]
	if p1Metrics == nil {
		t.Fatal("Expected 'P1' priority metrics")
	}
	if p1Metrics.Count != 3 {
		t.Errorf("Expected 3 P1 artifacts, got %d", p1Metrics.Count)
	}
}

func TestConverge_WithMetricsCollector(t *testing.T) {
	ctx := context.Background()
	collector := NewInMemoryMetricsCollector()

	// Create a simple refiner that converges after 3 iterations
	callCount := 0
	refiner := &mockRefiner{
		refineFunc: func(ctx context.Context, artifact *Artifact) (*Artifact, error) {
			callCount++
			time.Sleep(10 * time.Millisecond) // Simulate work
			return &Artifact{
				Type:    artifact.Type,
				Content: artifact.Content + " [refined]",
			}, nil
		},
		checkConvergenceFunc: func(ctx context.Context, current, previous *Artifact) (bool, error) {
			// Converge on 3rd iteration
			return callCount >= 3, nil
		},
	}

	initial := &Artifact{
		Type:    "analysis",
		Content: "initial",
		Context: "test context",
	}

	config := RefinementConfig{
		MinIterations: 2,
		MaxIterations: 5,
	}

	result, err := Converge(ctx, initial, refiner, config, collector)
	if err != nil {
		t.Fatalf("Converge failed: %v", err)
	}

	// Should converge at iteration 3
	if result.Iterations != 3 {
		t.Errorf("Expected 3 iterations, got %d", result.Iterations)
	}

	// Verify metrics were collected
	artifacts := collector.GetArtifacts()
	if len(artifacts) != 1 {
		t.Fatalf("Expected 1 artifact in metrics, got %d", len(artifacts))
	}

	artifactMetrics := artifacts[0]
	if artifactMetrics.TotalIterations != 3 {
		t.Errorf("Expected 3 iterations in metrics, got %d", artifactMetrics.TotalIterations)
	}
	if !artifactMetrics.Converged {
		t.Error("Expected converged=true in metrics")
	}
	if artifactMetrics.ConvergenceReason != "AI convergence" {
		t.Errorf("Expected 'AI convergence' reason, got %q", artifactMetrics.ConvergenceReason)
	}

	// Verify iteration metrics were collected
	if len(artifactMetrics.Iterations) != 3 {
		t.Fatalf("Expected 3 iteration metrics, got %d", len(artifactMetrics.Iterations))
	}

	// Verify each iteration has duration > 0
	for i, iter := range artifactMetrics.Iterations {
		if iter.Duration <= 0 {
			t.Errorf("Iteration %d has zero duration", i+1)
		}
		if iter.Iteration != i+1 {
			t.Errorf("Expected iteration %d, got %d", i+1, iter.Iteration)
		}
	}

	// Verify aggregate metrics
	agg := collector.GetAggregateMetrics()
	if agg.TotalArtifacts != 1 {
		t.Errorf("Expected 1 artifact in aggregate, got %d", agg.TotalArtifacts)
	}
	if agg.ConvergedArtifacts != 1 {
		t.Errorf("Expected 1 converged artifact, got %d", agg.ConvergedArtifacts)
	}
}

func TestConverge_WithMetricsCollector_MaxedOut(t *testing.T) {
	ctx := context.Background()
	collector := NewInMemoryMetricsCollector()

	// Refiner that never converges
	refiner := &mockRefiner{
		refineFunc: func(ctx context.Context, artifact *Artifact) (*Artifact, error) {
			return &Artifact{
				Type:    artifact.Type,
				Content: artifact.Content + " [refined]",
			}, nil
		},
		checkConvergenceFunc: func(ctx context.Context, current, previous *Artifact) (bool, error) {
			return false, nil // Never converge
		},
	}

	initial := &Artifact{Type: "test", Content: "initial"}
	config := RefinementConfig{MinIterations: 2, MaxIterations: 4}

	result, err := Converge(ctx, initial, refiner, config, collector)
	if err != nil {
		t.Fatalf("Converge failed: %v", err)
	}

	// Should hit max iterations
	if result.Iterations != 4 {
		t.Errorf("Expected 4 iterations, got %d", result.Iterations)
	}
	if result.Converged {
		t.Error("Expected converged=false (hit max iterations)")
	}

	// Verify metrics
	artifacts := collector.GetArtifacts()
	if len(artifacts) != 1 {
		t.Fatalf("Expected 1 artifact, got %d", len(artifacts))
	}

	artifactMetrics := artifacts[0]
	if artifactMetrics.Converged {
		t.Error("Expected converged=false in metrics")
	}
	if artifactMetrics.ConvergenceReason != "max iterations" {
		t.Errorf("Expected 'max iterations' reason, got %q", artifactMetrics.ConvergenceReason)
	}

	// Verify aggregate shows maxed out
	agg := collector.GetAggregateMetrics()
	if agg.MaxedOutArtifacts != 1 {
		t.Errorf("Expected 1 maxed out artifact, got %d", agg.MaxedOutArtifacts)
	}
	if agg.ConvergedArtifacts != 0 {
		t.Errorf("Expected 0 converged artifacts, got %d", agg.ConvergedArtifacts)
	}
}

func TestConverge_WithMetricsCollector_Skipped(t *testing.T) {
	ctx := context.Background()
	collector := NewInMemoryMetricsCollector()

	// Refiner that signals skip by returning same artifact
	refiner := &mockRefiner{
		refineFunc: func(ctx context.Context, artifact *Artifact) (*Artifact, error) {
			return artifact, nil // Return same artifact
		},
	}

	initial := &Artifact{Type: "test", Content: "simple"}
	config := RefinementConfig{
		MinIterations: 2,
		MaxIterations: 5,
		SkipSimple:    true,
	}

	result, err := Converge(ctx, initial, refiner, config, collector)
	if err != nil {
		t.Fatalf("Converge failed: %v", err)
	}

	// Should skip (0 iterations)
	if result.Iterations != 0 {
		t.Errorf("Expected 0 iterations, got %d", result.Iterations)
	}

	// Verify metrics
	artifacts := collector.GetArtifacts()
	if len(artifacts) != 1 {
		t.Fatalf("Expected 1 artifact, got %d", len(artifacts))
	}

	artifactMetrics := artifacts[0]
	if artifactMetrics.TotalIterations != 0 {
		t.Errorf("Expected 0 iterations in metrics, got %d", artifactMetrics.TotalIterations)
	}
	if artifactMetrics.ConvergenceReason != "skipped (simple)" {
		t.Errorf("Expected 'skipped (simple)' reason, got %q", artifactMetrics.ConvergenceReason)
	}

	// No iteration metrics should be recorded for skipped artifacts
	if len(artifactMetrics.Iterations) != 0 {
		t.Errorf("Expected 0 iteration metrics for skipped artifact, got %d", len(artifactMetrics.Iterations))
	}
}

func TestPercentile(t *testing.T) {
	tests := []struct {
		name     string
		values   []int
		p        int
		expected int
	}{
		{
			name:     "empty slice",
			values:   []int{},
			p:        50,
			expected: 0,
		},
		{
			name:     "single value",
			values:   []int{42},
			p:        50,
			expected: 42,
		},
		{
			name:     "p50 of sorted values",
			values:   []int{1, 2, 3, 4, 5},
			p:        50,
			expected: 3,
		},
		{
			name:     "p95 of sorted values",
			values:   []int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10},
			p:        95,
			expected: 10,
		},
		{
			name:     "p0 (minimum)",
			values:   []int{5, 10, 15, 20},
			p:        0,
			expected: 5,
		},
		{
			name:     "p100 (maximum)",
			values:   []int{5, 10, 15, 20},
			p:        100,
			expected: 20,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := percentile(tt.values, tt.p)
			if result != tt.expected {
				t.Errorf("percentile(%v, %d) = %d, expected %d", tt.values, tt.p, result, tt.expected)
			}
		})
	}
}

func TestComputeTypeMetricsStats(t *testing.T) {
	artifacts := []*ArtifactMetrics{
		{ArtifactType: "analysis", TotalIterations: 3, Converged: true},
		{ArtifactType: "analysis", TotalIterations: 5, Converged: true},
		{ArtifactType: "analysis", TotalIterations: 4, Converged: true},
		{ArtifactType: "assessment", TotalIterations: 7, Converged: false}, // Not included in stats
		{ArtifactType: "assessment", TotalIterations: 2, Converged: true},
	}

	byType := map[string]*TypeMetrics{
		"analysis":   {Count: 3},
		"assessment": {Count: 2},
	}

	ComputeTypeMetricsStats(artifacts, byType)

	// Verify analysis stats (3, 4, 5)
	analysisMetrics := byType["analysis"]
	if analysisMetrics.MeanIterations != 4.0 {
		t.Errorf("Expected mean=4.0 for analysis, got %.2f", analysisMetrics.MeanIterations)
	}
	if analysisMetrics.P50Iterations != 4 {
		t.Errorf("Expected P50=4 for analysis, got %d", analysisMetrics.P50Iterations)
	}
	if analysisMetrics.P95Iterations != 5 {
		t.Errorf("Expected P95=5 for analysis, got %d", analysisMetrics.P95Iterations)
	}

	// Verify assessment stats (only 2, since 7 didn't converge)
	assessmentMetrics := byType["assessment"]
	if assessmentMetrics.MeanIterations != 2.0 {
		t.Errorf("Expected mean=2.0 for assessment, got %.2f", assessmentMetrics.MeanIterations)
	}
	if assessmentMetrics.P50Iterations != 2 {
		t.Errorf("Expected P50=2 for assessment, got %d", assessmentMetrics.P50Iterations)
	}
}
