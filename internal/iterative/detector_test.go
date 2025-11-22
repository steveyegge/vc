package iterative

import (
	"context"
	"errors"
	"fmt"
	"testing"
)

// Note: AI convergence detector tests would require a real AI supervisor instance
// or extensive mocking infrastructure. For now, we test the other detectors
// and the prompt building logic. Integration tests with real AI can be added
// separately.

// TestDiffBasedDetector tests the diff-based convergence detector
func TestDiffBasedDetector(t *testing.T) {
	tests := []struct {
		name           string
		previous       string
		current        string
		maxDiffPercent float64
		wantConverged  bool
	}{
		{
			name:           "identical content",
			previous:       "line1\nline2\nline3",
			current:        "line1\nline2\nline3",
			maxDiffPercent: 5.0,
			wantConverged:  true, // 0% change
		},
		{
			name:           "small change below threshold",
			previous:       "line1\nline2\nline3\nline4\nline5\nline6\nline7\nline8\nline9\nline10",
			current:        "line1\nline2 modified\nline3\nline4\nline5\nline6\nline7\nline8\nline9\nline10",
			maxDiffPercent: 15.0, // 1 out of 10 lines = 10% change
			wantConverged:  true,
		},
		{
			name:           "large change above threshold",
			previous:       "line1\nline2\nline3\nline4\nline5",
			current:        "line1 mod\nline2 mod\nline3 mod\nline4\nline5",
			maxDiffPercent: 5.0,
			wantConverged:  false, // 3 out of 5 lines = 60% change
		},
		{
			name:           "empty to content",
			previous:       "",
			current:        "line1\nline2\nline3",
			maxDiffPercent: 5.0,
			wantConverged:  false, // 100% change (new content)
		},
		{
			name: "minimal refinement multiline",
			previous: `The quick brown fox jumps over the lazy dog.
This is line 2.
This is line 3.
This is line 4.
This is line 5.`,
			current: `The quick brown fox jumps over the lazy dog
This is line 2.
This is line 3.
This is line 4.
This is line 5.`,
			maxDiffPercent: 25.0, // 1 out of 5 lines = 20% change
			wantConverged:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			detector := NewDiffBasedDetector(tt.maxDiffPercent)
			ctx := context.Background()

			previous := &Artifact{Type: "test", Content: tt.previous}
			current := &Artifact{Type: "test", Content: tt.current}

			converged, confidence, err := detector.CheckConvergence(ctx, current, previous)
			if err != nil {
				t.Fatalf("CheckConvergence failed: %v", err)
			}

			if converged != tt.wantConverged {
				t.Errorf("Expected converged=%v, got %v (confidence=%.2f)", tt.wantConverged, converged, confidence)
			}

			// Confidence should be between 0 and 1
			if confidence < 0.0 || confidence > 1.0 {
				t.Errorf("Invalid confidence: %.2f (should be 0.0-1.0)", confidence)
			}
		})
	}
}

// TODO(vc-t9ls): AIConvergenceDetector tests have been moved to internal/ai/analysis_refiner_test.go
// The AI-specific convergence logic now lives in the ai package to avoid import cycles.
// This test is commented out to prevent build failures.
/*
func TestAIConvergenceDetector_PromptBuilding(t *testing.T) {
	// Use a mock supervisor to avoid nil check
	detector := &AIConvergenceDetector{
		supervisor:    nil, // We won't call CheckConvergence, just buildPrompt
		MinConfidence: 0.8,
	}

	previous := &Artifact{
		Type:    "analysis",
		Content: "Previous analysis content.\nMultiple lines.",
		Context: "Issue vc-123",
	}

	current := &Artifact{
		Type:    "analysis",
		Content: "Current analysis content.\nMore detail.",
		Context: "Issue vc-123",
	}

	prompt := detector.buildConvergencePrompt(current, previous)

	// Verify essential sections
	essentialSections := []string{
		"ARTIFACT TYPE:",
		"PREVIOUS VERSION:",
		"CURRENT VERSION:",
		"CONTEXT:",
		"converged",
		"confidence",
		"reasoning",
	}

	for _, section := range essentialSections {
		if !strings.Contains(prompt, section) {
			t.Errorf("Prompt missing: %s", section)
		}
	}
}
*/

// TestChainedDetector tests the fallback chain
func TestChainedDetector(t *testing.T) {
	tests := []struct {
		name           string
		detector1Err   error
		detector1Conf  float64
		detector1Conv  bool
		detector2Err   error
		detector2Conf  float64
		detector2Conv  bool
		minConfidence  float64
		wantConverged  bool
		wantConfidence float64
		wantErr        bool
	}{
		{
			name:           "first detector succeeds with high confidence",
			detector1Err:   nil,
			detector1Conf:  0.9,
			detector1Conv:  true,
			minConfidence:  0.7,
			wantConverged:  true,
			wantConfidence: 0.9,
			wantErr:        false,
		},
		{
			name:           "first detector low confidence, second succeeds",
			detector1Err:   nil,
			detector1Conf:  0.5, // Below threshold
			detector1Conv:  true,
			detector2Err:   nil,
			detector2Conf:  0.8, // Above threshold
			detector2Conv:  false,
			minConfidence:  0.7,
			wantConverged:  false,
			wantConfidence: 0.8,
			wantErr:        false,
		},
		{
			name:           "first detector fails, second succeeds",
			detector1Err:   errors.New("detector 1 failed"),
			detector2Err:   nil,
			detector2Conf:  0.9,
			detector2Conv:  true,
			minConfidence:  0.7,
			wantConverged:  true,
			wantConfidence: 0.9,
			wantErr:        false,
		},
		{
			name:           "both detectors fail",
			detector1Err:   errors.New("detector 1 failed"),
			detector2Err:   errors.New("detector 2 failed"),
			minConfidence:  0.7,
			wantErr:        true,
		},
		{
			name:           "both detectors low confidence",
			detector1Err:   nil,
			detector1Conf:  0.5,
			detector1Conv:  true,
			detector2Err:   nil,
			detector2Conf:  0.6,
			detector2Conv:  false,
			minConfidence:  0.7,
			wantConverged:  false,     // All had low confidence
			wantConfidence: 0.6,       // Last detector's confidence
			wantErr:        false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create mock detectors
			detector1 := &mockDetector{
				checkFunc: func(ctx context.Context, current, previous *Artifact) (bool, float64, error) {
					return tt.detector1Conv, tt.detector1Conf, tt.detector1Err
				},
			}

			detector2 := &mockDetector{
				checkFunc: func(ctx context.Context, current, previous *Artifact) (bool, float64, error) {
					return tt.detector2Conv, tt.detector2Conf, tt.detector2Err
				},
			}

			chained := NewChainedDetector(tt.minConfidence, detector1, detector2)

			ctx := context.Background()
			previous := &Artifact{Type: "test", Content: "prev"}
			current := &Artifact{Type: "test", Content: "curr"}

			converged, confidence, err := chained.CheckConvergence(ctx, current, previous)

			if (err != nil) != tt.wantErr {
				t.Fatalf("Expected error=%v, got: %v", tt.wantErr, err)
			}

			if tt.wantErr {
				return // Don't check other values if we expected an error
			}

			if converged != tt.wantConverged {
				t.Errorf("Expected converged=%v, got %v", tt.wantConverged, converged)
			}

			if confidence != tt.wantConfidence {
				t.Errorf("Expected confidence=%.2f, got %.2f", tt.wantConfidence, confidence)
			}
		})
	}
}

// mockDetector is a test implementation of ConvergenceDetector
type mockDetector struct {
	checkFunc func(ctx context.Context, current, previous *Artifact) (bool, float64, error)
}

func (m *mockDetector) CheckConvergence(ctx context.Context, current, previous *Artifact) (bool, float64, error) {
	if m.checkFunc != nil {
		return m.checkFunc(ctx, current, previous)
	}
	return false, 0.5, nil // Default: not converged, low confidence
}

// TestCountDiffLines tests the diff counting helper
func TestCountDiffLines(t *testing.T) {
	tests := []struct {
		name     string
		prev     string
		current  string
		wantDiff int
	}{
		{
			name:     "identical",
			prev:     "line1\nline2\nline3",
			current:  "line1\nline2\nline3",
			wantDiff: 0,
		},
		{
			name:     "one line changed",
			prev:     "line1\nline2\nline3",
			current:  "line1\nline2 modified\nline3",
			wantDiff: 1,
		},
		{
			name:     "all lines changed",
			prev:     "line1\nline2\nline3",
			current:  "new1\nnew2\nnew3",
			wantDiff: 3,
		},
		{
			name:     "lines added",
			prev:     "line1\nline2",
			current:  "line1\nline2\nline3\nline4",
			wantDiff: 2, // Two new lines
		},
		{
			name:     "lines removed",
			prev:     "line1\nline2\nline3\nline4",
			current:  "line1\nline2",
			wantDiff: 2, // Two missing lines
		},
		{
			name:     "empty to content",
			prev:     "",
			current:  "line1\nline2",
			wantDiff: 2,
		},
		{
			name:     "content to empty",
			prev:     "line1\nline2",
			current:  "",
			wantDiff: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			diff := countDiffLines(tt.prev, tt.current)
			if diff != tt.wantDiff {
				t.Errorf("Expected diff=%d, got %d", tt.wantDiff, diff)
			}
		})
	}
}

// TestCountLines tests the line counting helper
func TestCountLines(t *testing.T) {
	tests := []struct {
		name      string
		text      string
		wantLines int
	}{
		{name: "empty", text: "", wantLines: 0},
		{name: "single line no newline", text: "line1", wantLines: 1},
		{name: "single line with newline", text: "line1\n", wantLines: 2},
		{name: "three lines", text: "line1\nline2\nline3", wantLines: 3},
		{name: "three lines with trailing newline", text: "line1\nline2\nline3\n", wantLines: 4},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lines := countLines(tt.text)
			if lines != tt.wantLines {
				t.Errorf("Expected %d lines, got %d", tt.wantLines, lines)
			}
		})
	}
}


// TODO(vc-t9ls): TestNewAIConvergenceDetector has been moved to internal/ai package
// AI-specific convergence testing is now in internal/ai/analysis_refiner_test.go
/*
func TestNewAIConvergenceDetector(t *testing.T) {
	// Test with nil supervisor (should error)
	_, err := NewAIConvergenceDetector(nil, 0.8)
	if err == nil {
		t.Error("Expected error with nil supervisor, got nil")
	}
	if err != nil && !strings.Contains(err.Error(), "supervisor cannot be nil") {
		t.Errorf("Expected 'supervisor cannot be nil' error, got: %v", err)
	}

	// For valid tests with real supervisor and confidence defaults,
	// see integration tests (vc-kok4)
}
*/

// TestNewDiffBasedDetector tests constructor defaults
func TestNewDiffBasedDetector(t *testing.T) {
	// Test with explicit threshold
	detector1 := NewDiffBasedDetector(10.0)
	if detector1.MaxDiffPercent != 10.0 {
		t.Errorf("Expected MaxDiffPercent=10.0, got %.2f", detector1.MaxDiffPercent)
	}

	// Test with zero threshold (should use default)
	detector2 := NewDiffBasedDetector(0.0)
	if detector2.MaxDiffPercent != 5.0 {
		t.Errorf("Expected default MaxDiffPercent=5.0, got %.2f", detector2.MaxDiffPercent)
	}

	// Test with negative threshold (should use default)
	detector3 := NewDiffBasedDetector(-1.0)
	if detector3.MaxDiffPercent != 5.0 {
		t.Errorf("Expected default MaxDiffPercent=5.0, got %.2f", detector3.MaxDiffPercent)
	}
}

// TestNewChainedDetector tests constructor defaults
func TestNewChainedDetector(t *testing.T) {
	detector1 := &mockDetector{}
	detector2 := &mockDetector{}

	// Test with explicit confidence
	chained1 := NewChainedDetector(0.75, detector1, detector2)
	if chained1.MinConfidence != 0.75 {
		t.Errorf("Expected MinConfidence=0.75, got %.2f", chained1.MinConfidence)
	}
	if len(chained1.detectors) != 2 {
		t.Errorf("Expected 2 detectors, got %d", len(chained1.detectors))
	}

	// Test with zero confidence (should use default)
	chained2 := NewChainedDetector(0.0, detector1)
	if chained2.MinConfidence != 0.7 {
		t.Errorf("Expected default MinConfidence=0.7, got %.2f", chained2.MinConfidence)
	}
}

// Example usage demonstrating integration with Refiner
func ExampleNewChainedDetector() {
	// Create a chained detector with AI primary and diff-based fallback
	// Note: In real usage, pass a real supervisor instead of nil
	// aiDetector, _ := NewAIConvergenceDetector(supervisor, 0.8)
	diffDetector := NewDiffBasedDetector(5.0)

	// For example purposes, create chain with just diff detector
	chainedDetector := NewChainedDetector(0.7, diffDetector)

	fmt.Printf("Chained detector configured with min confidence: %.1f\n", chainedDetector.MinConfidence)
	// Output: Chained detector configured with min confidence: 0.7
}

// TestConvergenceMetrics tests metrics tracking
func TestConvergenceMetrics(t *testing.T) {
	metrics := NewConvergenceMetrics()

	// Record some checks
	metrics.RecordCheck(false, "AI")
	metrics.RecordCheck(false, "AI")
	metrics.RecordCheck(true, "AI")

	if metrics.TotalChecks != 3 {
		t.Errorf("Expected TotalChecks=3, got %d", metrics.TotalChecks)
	}

	if metrics.ConvergedChecks != 1 {
		t.Errorf("Expected ConvergedChecks=1, got %d", metrics.ConvergedChecks)
	}

	if metrics.DetectorStrategyUsed["AI"] != 3 {
		t.Errorf("Expected AI strategy used 3 times, got %d", metrics.DetectorStrategyUsed["AI"])
	}

	// Record some artifact results
	metrics.RecordArtifact(&ConvergenceResult{
		FinalArtifact: nil,
		Iterations:    3,
		Converged:     true,
		ElapsedTime:   0,
	})

	metrics.RecordArtifact(&ConvergenceResult{
		FinalArtifact: nil,
		Iterations:    5,
		Converged:     true,
		ElapsedTime:   0,
	})

	metrics.RecordArtifact(&ConvergenceResult{
		FinalArtifact: nil,
		Iterations:    10,
		Converged:     false, // Hit MaxIterations
		ElapsedTime:   0,
	})

	// Verify artifact metrics
	if metrics.ArtifactsConverged != 2 {
		t.Errorf("Expected ArtifactsConverged=2, got %d", metrics.ArtifactsConverged)
	}

	if metrics.ArtifactsMaxedOut != 1 {
		t.Errorf("Expected ArtifactsMaxedOut=1, got %d", metrics.ArtifactsMaxedOut)
	}

	if metrics.TotalIterations != 18 {
		t.Errorf("Expected TotalIterations=18, got %d", metrics.TotalIterations)
	}

	if metrics.ConvergedIterations != 8 {
		t.Errorf("Expected ConvergedIterations=8, got %d", metrics.ConvergedIterations)
	}

	// Verify calculated metrics
	convergenceRate := metrics.ConvergenceRate()
	if convergenceRate != 66.66666666666666 {
		t.Errorf("Expected ConvergenceRate=66.67%%, got %.2f%%", convergenceRate)
	}

	meanIterations := metrics.MeanIterations()
	if meanIterations != 6.0 {
		t.Errorf("Expected MeanIterations=6.0, got %.2f", meanIterations)
	}

	meanToConvergence := metrics.MeanIterationsToConvergence()
	if meanToConvergence != 4.0 {
		t.Errorf("Expected MeanIterationsToConvergence=4.0, got %.2f", meanToConvergence)
	}
}

// TestConvergenceMetrics_EdgeCases tests edge cases
func TestConvergenceMetrics_EdgeCases(t *testing.T) {
	metrics := NewConvergenceMetrics()

	// Empty metrics should return 0.0 for all calculated values
	if metrics.ConvergenceRate() != 0.0 {
		t.Errorf("Expected ConvergenceRate=0.0 for empty metrics, got %.2f", metrics.ConvergenceRate())
	}

	if metrics.MeanIterations() != 0.0 {
		t.Errorf("Expected MeanIterations=0.0 for empty metrics, got %.2f", metrics.MeanIterations())
	}

	if metrics.MeanIterationsToConvergence() != 0.0 {
		t.Errorf("Expected MeanIterationsToConvergence=0.0 for empty metrics, got %.2f", metrics.MeanIterationsToConvergence())
	}
}
