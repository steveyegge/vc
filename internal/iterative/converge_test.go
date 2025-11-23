package iterative

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

// mockRefiner is a test implementation of the Refiner interface
type mockRefiner struct {
	refineFunc            func(ctx context.Context, artifact *Artifact) (*Artifact, error)
	checkConvergenceFunc  func(ctx context.Context, current, previous *Artifact) (*ConvergenceDecision, error)
	refineCalls           int
	convergenceCheckCalls int
}

func (m *mockRefiner) Refine(ctx context.Context, artifact *Artifact) (*Artifact, error) {
	m.refineCalls++
	if m.refineFunc != nil {
		return m.refineFunc(ctx, artifact)
	}
	// Default: append iteration marker
	return &Artifact{
		Type:    artifact.Type,
		Content: fmt.Sprintf("%s [refined-%d]", artifact.Content, m.refineCalls),
		Context: artifact.Context,
	}, nil
}

func (m *mockRefiner) CheckConvergence(ctx context.Context, current, previous *Artifact) (*ConvergenceDecision, error) {
	m.convergenceCheckCalls++
	if m.checkConvergenceFunc != nil {
		return m.checkConvergenceFunc(ctx, current, previous)
	}
	// Default: never converge (test MaxIterations)
	return &ConvergenceDecision{
		Converged:  false,
		Confidence: 0.5,
		Reasoning:  "Mock default: not converged",
		Strategy:   "mock",
	}, nil
}

func TestConverge_BasicIteration(t *testing.T) {
	ctx := context.Background()
	refiner := &mockRefiner{}

	initial := &Artifact{
		Type:    "test",
		Content: "initial",
		Context: "test context",
	}

	config := RefinementConfig{
		MinIterations: 2,
		MaxIterations: 5,
	}

	result, err := Converge(ctx, initial, refiner, config, nil)
	if err != nil {
		t.Fatalf("Converge failed: %v", err)
	}

	// Should hit MaxIterations since mockRefiner never converges
	if result.Iterations != 5 {
		t.Errorf("Expected 5 iterations, got %d", result.Iterations)
	}

	if result.Converged {
		t.Error("Expected not converged (hit MaxIterations)")
	}

	// Should have refined 5 times
	if refiner.refineCalls != 5 {
		t.Errorf("Expected 5 refine calls, got %d", refiner.refineCalls)
	}

	// Should check convergence after MinIterations (iterations 2-5 = 4 checks)
	if refiner.convergenceCheckCalls != 4 {
		t.Errorf("Expected 4 convergence checks, got %d", refiner.convergenceCheckCalls)
	}

	// Final content should show all iterations
	expected := "initial [refined-1] [refined-2] [refined-3] [refined-4] [refined-5]"
	if result.FinalArtifact.Content != expected {
		t.Errorf("Expected content %q, got %q", expected, result.FinalArtifact.Content)
	}
}

func TestConverge_ConvergesEarly(t *testing.T) {
	ctx := context.Background()

	// Converge after 3 iterations
	refiner := &mockRefiner{
		checkConvergenceFunc: func(ctx context.Context, current, previous *Artifact) (*ConvergenceDecision, error) {
			// Count refinement iterations by looking at content
			iterations := strings.Count(current.Content, "refined")
			converged := iterations >= 3
			return &ConvergenceDecision{
				Converged:  converged,
				Confidence: 0.9,
				Reasoning:  fmt.Sprintf("Iteration %d/3", iterations),
				Strategy:   "mock-counter",
			}, nil
		},
	}

	initial := &Artifact{
		Type:    "test",
		Content: "initial",
	}

	config := RefinementConfig{
		MinIterations: 2,
		MaxIterations: 10,
	}

	result, err := Converge(ctx, initial, refiner, config, nil)
	if err != nil {
		t.Fatalf("Converge failed: %v", err)
	}

	// Should converge at iteration 3
	if result.Iterations != 3 {
		t.Errorf("Expected 3 iterations, got %d", result.Iterations)
	}

	if !result.Converged {
		t.Error("Expected converged=true")
	}

	if refiner.refineCalls != 3 {
		t.Errorf("Expected 3 refine calls, got %d", refiner.refineCalls)
	}

	// Convergence checks start after MinIterations (2)
	// Checks happen at iterations 2 and 3 (converges on iteration 3)
	if refiner.convergenceCheckCalls != 2 {
		t.Errorf("Expected 2 convergence checks, got %d", refiner.convergenceCheckCalls)
	}
}

func TestConverge_MinIterationsEnforced(t *testing.T) {
	ctx := context.Background()

	// Try to converge immediately (but MinIterations should prevent it)
	refiner := &mockRefiner{
		checkConvergenceFunc: func(ctx context.Context, current, previous *Artifact) (*ConvergenceDecision, error) {
			return &ConvergenceDecision{
				Converged:  true,
				Confidence: 1.0,
				Reasoning:  "Always converged",
				Strategy:   "mock-always",
			}, nil
		},
	}

	initial := &Artifact{
		Type:    "test",
		Content: "initial",
	}

	config := RefinementConfig{
		MinIterations: 4,
		MaxIterations: 10,
	}

	result, err := Converge(ctx, initial, refiner, config, nil)
	if err != nil {
		t.Fatalf("Converge failed: %v", err)
	}

	// Should do at least MinIterations, then converge immediately
	if result.Iterations != 4 {
		t.Errorf("Expected 4 iterations (MinIterations), got %d", result.Iterations)
	}

	if !result.Converged {
		t.Error("Expected converged=true")
	}
}

func TestConverge_RefineError(t *testing.T) {
	ctx := context.Background()

	expectedErr := errors.New("refine error")
	refiner := &mockRefiner{
		refineFunc: func(ctx context.Context, artifact *Artifact) (*Artifact, error) {
			return nil, expectedErr
		},
	}

	initial := &Artifact{Type: "test", Content: "initial"}
	config := RefinementConfig{MinIterations: 2, MaxIterations: 5}

	_, err := Converge(ctx, initial, refiner, config, nil)
	if err == nil {
		t.Fatal("Expected error from Refine, got nil")
	}

	if !strings.Contains(err.Error(), "refinement failed at iteration 1") {
		t.Errorf("Expected error message about iteration 1, got: %v", err)
	}
}

func TestConverge_ContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	refiner := &mockRefiner{
		refineFunc: func(ctx context.Context, artifact *Artifact) (*Artifact, error) {
			// Cancel context on second iteration
			if strings.Count(artifact.Content, "refined") == 1 {
				cancel()
			}
			return &Artifact{
				Type:    artifact.Type,
				Content: artifact.Content + " [refined]",
			}, nil
		},
	}

	initial := &Artifact{Type: "test", Content: "initial"}
	config := RefinementConfig{MinIterations: 2, MaxIterations: 10}

	_, err := Converge(ctx, initial, refiner, config, nil)
	if err == nil {
		t.Fatal("Expected cancellation error, got nil")
	}

	if !strings.Contains(err.Error(), "refinement canceled") {
		t.Errorf("Expected cancellation error, got: %v", err)
	}
}

func TestConverge_Timeout(t *testing.T) {
	ctx := context.Background()

	refiner := &mockRefiner{
		refineFunc: func(ctx context.Context, artifact *Artifact) (*Artifact, error) {
			// Simulate slow refinement
			time.Sleep(100 * time.Millisecond)
			return &Artifact{
				Type:    artifact.Type,
				Content: artifact.Content + " [refined]",
			}, nil
		},
	}

	initial := &Artifact{Type: "test", Content: "initial"}
	config := RefinementConfig{
		MinIterations: 2,
		MaxIterations: 10,
		Timeout:       150 * time.Millisecond, // Only allow ~1 iteration
	}

	_, err := Converge(ctx, initial, refiner, config, nil)
	if err == nil {
		t.Fatal("Expected timeout error, got nil")
	}

	if !strings.Contains(err.Error(), "refinement canceled") {
		t.Errorf("Expected timeout/cancellation error, got: %v", err)
	}
}

func TestConverge_ConvergenceCheckError(t *testing.T) {
	ctx := context.Background()

	convergenceErr := errors.New("convergence check failed")
	refiner := &mockRefiner{
		checkConvergenceFunc: func(ctx context.Context, current, previous *Artifact) (*ConvergenceDecision, error) {
			return nil, convergenceErr
		},
	}

	initial := &Artifact{Type: "test", Content: "initial"}
	config := RefinementConfig{MinIterations: 2, MaxIterations: 3}

	result, err := Converge(ctx, initial, refiner, config, nil)
	// Convergence errors should be logged but not fail the refinement
	if err != nil {
		t.Fatalf("Converge should not fail on convergence check error: %v", err)
	}

	// Should complete all MaxIterations (fallback when convergence check fails)
	if result.Iterations != 3 {
		t.Errorf("Expected 3 iterations, got %d", result.Iterations)
	}
}

func TestConverge_ConvergenceCheckErrorWithMetrics(t *testing.T) {
	ctx := context.Background()

	convergenceErr := errors.New("convergence check failed")
	refiner := &mockRefiner{
		checkConvergenceFunc: func(ctx context.Context, current, previous *Artifact) (*ConvergenceDecision, error) {
			return nil, convergenceErr
		},
	}

	initial := &Artifact{Type: "test", Content: "initial"}
	config := RefinementConfig{MinIterations: 2, MaxIterations: 3}
	collector := NewInMemoryMetricsCollector()

	result, err := Converge(ctx, initial, refiner, config, collector)
	// Convergence errors should be logged but not fail the refinement
	if err != nil {
		t.Fatalf("Converge should not fail on convergence check error: %v", err)
	}

	// Should complete all MaxIterations (fallback when convergence check fails)
	if result.Iterations != 3 {
		t.Errorf("Expected 3 iterations, got %d", result.Iterations)
	}

	// Verify convergence check errors were tracked
	agg := collector.GetAggregateMetrics()
	// MinIterations=2, MaxIterations=3, so convergence is checked at iterations 2 and 3
	// Both checks fail, so we expect 2 errors
	expectedErrors := 2
	if agg.ConvergenceCheckErrors != expectedErrors {
		t.Errorf("Expected %d convergence check errors, got %d", expectedErrors, agg.ConvergenceCheckErrors)
	}
}

func TestConverge_InvalidConfig(t *testing.T) {
	ctx := context.Background()
	refiner := &mockRefiner{}
	initial := &Artifact{Type: "test", Content: "initial"}

	tests := []struct {
		name   string
		config RefinementConfig
		errMsg string
	}{
		{
			name:   "negative MinIterations",
			config: RefinementConfig{MinIterations: -1, MaxIterations: 5},
			errMsg: "MinIterations cannot be negative",
		},
		{
			name:   "MaxIterations < MinIterations",
			config: RefinementConfig{MinIterations: 5, MaxIterations: 3},
			errMsg: "MaxIterations (3) must be >= MinIterations (5)",
		},
		{
			name:   "MaxIterations zero",
			config: RefinementConfig{MinIterations: 0, MaxIterations: 0},
			errMsg: "MaxIterations cannot be zero",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Converge(ctx, initial, refiner, tt.config, nil)
			if err == nil {
				t.Fatal("Expected validation error, got nil")
			}
			if !strings.Contains(err.Error(), tt.errMsg) {
				t.Errorf("Expected error containing %q, got: %v", tt.errMsg, err)
			}
		})
	}
}

func TestConverge_SkipSimple(t *testing.T) {
	ctx := context.Background()

	// Refiner signals skip by returning same artifact on first iteration
	refiner := &mockRefiner{
		refineFunc: func(ctx context.Context, artifact *Artifact) (*Artifact, error) {
			// Return same artifact to signal skip
			return artifact, nil
		},
	}

	initial := &Artifact{Type: "test", Content: "simple task"}
	config := RefinementConfig{
		MinIterations: 2,
		MaxIterations: 5,
		SkipSimple:    true,
	}

	result, err := Converge(ctx, initial, refiner, config, nil)
	if err != nil {
		t.Fatalf("Converge failed: %v", err)
	}

	// Should skip (0 iterations)
	if result.Iterations != 0 {
		t.Errorf("Expected 0 iterations (skipped), got %d", result.Iterations)
	}

	if !result.Converged {
		t.Error("Expected converged=true for skipped task")
	}

	if refiner.refineCalls != 1 {
		t.Errorf("Expected 1 refine call (to check skip), got %d", refiner.refineCalls)
	}

	// Should not check convergence if skipped
	if refiner.convergenceCheckCalls != 0 {
		t.Errorf("Expected 0 convergence checks (skipped), got %d", refiner.convergenceCheckCalls)
	}
}

func TestConverge_ElapsedTime(t *testing.T) {
	ctx := context.Background()

	refiner := &mockRefiner{
		refineFunc: func(ctx context.Context, artifact *Artifact) (*Artifact, error) {
			time.Sleep(10 * time.Millisecond)
			return &Artifact{
				Type:    artifact.Type,
				Content: artifact.Content + " [refined]",
			}, nil
		},
	}

	initial := &Artifact{Type: "test", Content: "initial"}
	config := RefinementConfig{MinIterations: 2, MaxIterations: 2}

	result, err := Converge(ctx, initial, refiner, config, nil)
	if err != nil {
		t.Fatalf("Converge failed: %v", err)
	}

	// Should have some elapsed time (at least 20ms for 2 iterations)
	if result.ElapsedTime < 20*time.Millisecond {
		t.Errorf("Expected elapsed time >= 20ms, got %v", result.ElapsedTime)
	}
}
