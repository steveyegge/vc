package ai

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/steveyegge/vc/internal/types"
)

// TestValidatorPanicRecovery verifies that panics in validators are caught and handled
func TestValidatorPanicRecovery(t *testing.T) {
	s := &Supervisor{}

	// Create a custom validator that panics
	panicValidator := func(ctx context.Context) error {
		panic("intentional panic for testing")
	}

	err := s.runValidatorSafely(context.Background(), "panic_test", panicValidator)

	if err == nil {
		t.Fatal("Expected error from panic recovery, got nil")
	}

	if !strings.Contains(err.Error(), "validator panic") {
		t.Errorf("Error should mention panic, got: %v", err)
	}

	if !strings.Contains(err.Error(), "intentional panic") {
		t.Errorf("Error should include panic message, got: %v", err)
	}
}

// TestValidatorTimeout verifies that validators time out correctly
func TestValidatorTimeout(t *testing.T) {
	s := &Supervisor{}

	// Create a validator that hangs
	hangingValidator := func(ctx context.Context) error {
		// This will hang until context is cancelled
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(1 * time.Hour):
			return nil
		}
	}

	// Set a very short timeout for testing
	t.Setenv("VC_VALIDATOR_TIMEOUT", "100ms")

	start := time.Now()
	err := s.runValidatorSafely(context.Background(), "timeout_test", hangingValidator)
	duration := time.Since(start)

	if err == nil {
		t.Fatal("Expected timeout error, got nil")
	}

	if !strings.Contains(err.Error(), "timeout") {
		t.Errorf("Error should mention timeout, got: %v", err)
	}

	// Should timeout within a reasonable time (not hang for an hour)
	if duration > 1*time.Second {
		t.Errorf("Timeout took too long: %v", duration)
	}
}

// TestValidatorContextCancellation verifies validators respect parent context canceled
func TestValidatorContextCancellation(t *testing.T) {
	s := &Supervisor{}

	ctx, cancel := context.WithCancel(context.Background())

	// Create a validator that checks context
	contextValidator := func(ctx context.Context) error {
		<-ctx.Done()
		return ctx.Err()
	}

	// Cancel context immediately
	cancel()

	err := s.runValidatorSafely(ctx, "context_test", contextValidator)

	// Should get context canceled error
	if err == nil {
		t.Fatal("Expected context error, got nil")
	}
}

// TestValidatorSuccessPath verifies normal validators still work
func TestValidatorSuccessPath(t *testing.T) {
	s := &Supervisor{}

	successValidator := func(ctx context.Context) error {
		return nil
	}

	err := s.runValidatorSafely(context.Background(), "success_test", successValidator)

	if err != nil {
		t.Errorf("Expected nil error, got: %v", err)
	}
}

// TestValidatorErrorPropagation verifies errors are properly propagated
func TestValidatorErrorPropagation(t *testing.T) {
	s := &Supervisor{}

	testErr := errors.New("test validation error")
	errorValidator := func(ctx context.Context) error {
		return testErr
	}

	err := s.runValidatorSafely(context.Background(), "error_test", errorValidator)

	if err == nil {
		t.Fatal("Expected error, got nil")
	}

	if !strings.Contains(err.Error(), "test validation error") {
		t.Errorf("Expected test validation error, got: %v", err)
	}
}

// TestMultipleValidatorFailures verifies all validators run despite failures
func TestMultipleValidatorFailures(t *testing.T) {
	s := &Supervisor{}

	// Plan that will fail multiple validators
	// Use makePhases to create too many phases AND each phase has too many tasks
	phases := makePhases(20) // Too many phases (>15)
	for i := range phases {
		phases[i].Tasks = makeTasks(60) // Too many tasks per phase (>50)
	}

	plan := &types.MissionPlan{
		MissionID:       "vc-test",
		Phases:          phases,
		Strategy:        "Test",
		EstimatedEffort: "1 year",
		Confidence:      0.5,
	}

	err := s.ValidatePlan(context.Background(), plan)

	if err == nil {
		t.Fatal("Expected validation error, got nil")
	}

	// Should mention both validator failures
	if !strings.Contains(err.Error(), "phase_count") {
		t.Errorf("Error should mention phase_count validator, got: %v", err)
	}
	if !strings.Contains(err.Error(), "task_counts") {
		t.Errorf("Error should mention task_counts validator, got: %v", err)
	}
}

// TestCycleDetectorPathological verifies cycle detector doesn't hang on complex graphs
func TestCycleDetectorPathological(t *testing.T) {
	s := &Supervisor{}

	// Create a complex but valid dependency graph
	phases := make([]types.PlannedPhase, 15)
	for i := 0; i < 15; i++ {
		deps := []int{}
		// Each phase depends on all previous phases (valid but complex)
		for j := 0; j < i; j++ {
			deps = append(deps, j+1)
		}

		phases[i] = types.PlannedPhase{
			PhaseNumber:     i + 1,
			Title:           string(rune('A' + i)),
			Description:     "Test",
			Strategy:        "Test",
			Tasks:           []string{"Task"},
			Dependencies:    deps,
			EstimatedEffort: "1 week",
		}
	}

	plan := &types.MissionPlan{
		MissionID:       "vc-test",
		Phases:          phases,
		Strategy:        "Test",
		EstimatedEffort: "15 weeks",
		Confidence:      0.8,
	}

	// Should complete quickly even with complex graph
	start := time.Now()
	err := s.ValidatePlan(context.Background(), plan)
	duration := time.Since(start)

	if err != nil {
		t.Errorf("Valid complex graph should pass: %v", err)
	}

	// Should not take more than a few seconds
	if duration > 5*time.Second {
		t.Errorf("Validation took too long for complex graph: %v", duration)
	}
}

// TestValidatorTimeoutConfiguration verifies timeout can be configured
func TestValidatorTimeoutConfiguration(t *testing.T) {
	s := &Supervisor{}

	slowValidator := func(ctx context.Context) error {
		time.Sleep(200 * time.Millisecond)
		return nil
	}

	t.Run("short timeout fails", func(t *testing.T) {
		t.Setenv("VC_VALIDATOR_TIMEOUT", "50ms")
		err := s.runValidatorSafely(context.Background(), "slow_test", slowValidator)
		if err == nil || !strings.Contains(err.Error(), "timeout") {
			t.Errorf("Expected timeout error, got: %v", err)
		}
	})

	t.Run("long timeout succeeds", func(t *testing.T) {
		t.Setenv("VC_VALIDATOR_TIMEOUT", "500ms")
		err := s.runValidatorSafely(context.Background(), "slow_test", slowValidator)
		if err != nil {
			t.Errorf("Expected success with long timeout, got: %v", err)
		}
	})
}
