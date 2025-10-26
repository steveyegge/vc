package executor

import (
	"context"
	"fmt"
	"os"
	"time"
)

// watchdogLoop runs the watchdog monitoring in a background goroutine
// It periodically checks for anomalies and intervenes when necessary
func (e *Executor) watchdogLoop(ctx context.Context) {
	defer close(e.watchdogDoneCh)

	ticker := time.NewTicker(e.watchdogConfig.GetCheckInterval())
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-e.watchdogStopCh:
			return
		case <-ticker.C:
			// Check if we should stop before running potentially slow anomaly check (vc-113)
			select {
			case <-e.watchdogStopCh:
				return
			default:
			}

			// Run anomaly detection with cancellation support (vc-113)
			// Use a channel to make check interruptible
			done := make(chan error, 1)
			go func() {
				done <- e.checkForAnomalies(ctx)
			}()

			// Wait for either completion or stop signal
			select {
			case err := <-done:
				if err != nil {
					// Log error but continue monitoring
					fmt.Fprintf(os.Stderr, "watchdog: error checking for anomalies: %v\n", err)
				}
			case <-e.watchdogStopCh:
				// Stop signal received while checking - exit immediately
				// The goroutine will finish in the background
				return
			}
		}
	}
}

// checkForAnomalies performs one cycle of anomaly detection and intervention
func (e *Executor) checkForAnomalies(ctx context.Context) error {
	// Skip if no analyzer (watchdog disabled)
	if e.analyzer == nil {
		return nil
	}

	// Detect anomalies using AI analysis of telemetry
	report, err := e.analyzer.DetectAnomalies(ctx)
	if err != nil {
		return fmt.Errorf("anomaly detection failed: %w", err)
	}

	// If no anomaly detected, nothing to do
	if !report.Detected {
		return nil
	}

	// Check if this anomaly meets the threshold for intervention
	if !e.watchdogConfig.ShouldIntervene(report) {
		// Anomaly detected but below threshold - just log it
		if e.watchdogConfig.AIConfig.EnableAnomalyLogging {
			fmt.Printf("Watchdog: Anomaly detected but below threshold - type=%s, severity=%s, confidence=%.2f (threshold: confidence=%.2f, severity=%s)\n",
				report.AnomalyType, report.Severity, report.Confidence,
				e.watchdogConfig.AIConfig.MinConfidenceThreshold,
				e.watchdogConfig.AIConfig.MinSeverityLevel)
		}
		return nil
	}

	// Anomaly meets threshold - intervene
	fmt.Printf("Watchdog: Intervening - type=%s, severity=%s, confidence=%.2f, recommended_action=%s\n",
		report.AnomalyType, report.Severity, report.Confidence, report.RecommendedAction)

	// Use intervention controller to decide and execute intervention
	result, err := e.intervention.Intervene(ctx, report)
	if err != nil {
		return fmt.Errorf("intervention failed: %w", err)
	}

	fmt.Printf("Watchdog: Intervention completed - %s (escalation issue: %s)\n",
		result.Message, result.EscalationIssueID)

	return nil
}
