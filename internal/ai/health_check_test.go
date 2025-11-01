package ai

import (
	"context"
	"errors"
	"testing"
)

func TestSupervisorHealthCheck(t *testing.T) {
	t.Run("passes when circuit breaker is closed", func(t *testing.T) {
		s := &Supervisor{
			retry:          DefaultRetryConfig(),
			circuitBreaker: NewCircuitBreaker(5, 2, DefaultRetryConfig().OpenTimeout),
		}

		err := s.HealthCheck(context.Background())
		if err != nil {
			t.Errorf("expected no error, got: %v", err)
		}
	})

	t.Run("passes when circuit breaker is half-open", func(t *testing.T) {
		s := &Supervisor{
			retry:          DefaultRetryConfig(),
			circuitBreaker: NewCircuitBreaker(2, 1, DefaultRetryConfig().OpenTimeout),
		}

		// Open the circuit
		s.circuitBreaker.RecordFailure()
		s.circuitBreaker.RecordFailure()
		
		// Transition to half-open by simulating timeout
		s.circuitBreaker.mu.Lock()
		s.circuitBreaker.transitionToHalfOpen()
		s.circuitBreaker.mu.Unlock()

		err := s.HealthCheck(context.Background())
		if err != nil {
			t.Errorf("expected no error in half-open state, got: %v", err)
		}
	})

	t.Run("fails when circuit breaker is open", func(t *testing.T) {
		s := &Supervisor{
			retry:          DefaultRetryConfig(),
			circuitBreaker: NewCircuitBreaker(2, 1, DefaultRetryConfig().OpenTimeout),
		}

		// Open the circuit
		s.circuitBreaker.RecordFailure()
		s.circuitBreaker.RecordFailure()

		err := s.HealthCheck(context.Background())
		if err == nil {
			t.Error("expected error when circuit is open, got nil")
		}

		if !errors.Is(err, ErrCircuitOpen) {
			t.Errorf("expected ErrCircuitOpen, got: %v", err)
		}
	})

	t.Run("passes when circuit breaker is disabled", func(t *testing.T) {
		s := &Supervisor{
			retry:          DefaultRetryConfig(),
			circuitBreaker: nil, // Disabled
		}

		err := s.HealthCheck(context.Background())
		if err != nil {
			t.Errorf("expected no error when circuit breaker disabled, got: %v", err)
		}
	})
}
