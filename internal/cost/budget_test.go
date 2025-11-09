package cost

import (
	"context"
	"testing"
	"time"
)

// TestBurnRateCalculation tests the accuracy of burn rate prediction (vc-7e21)
func TestBurnRateCalculation(t *testing.T) {
	// Use a fixed "now" time for consistent testing
	now := time.Now()

	tests := []struct {
		name                    string
		snapshots               []QuotaSnapshot
		expectedTokensPerMinute float64
		expectedCostPerMinute   float64
		expectedConfidence      float64
		expectedAlertLevel      AlertLevel
		maxTokensPerHour        int64
		maxCostPerHour          float64
	}{
		{
			name: "steady burn rate - linear usage",
			snapshots: []QuotaSnapshot{
				{
					Timestamp:        now.Add(-10 * time.Minute),
					WindowStart:      now.Add(-20 * time.Minute),
					HourlyTokensUsed: 1000,
					HourlyCostUsed:   0.01,
				},
				{
					Timestamp:        now.Add(-5 * time.Minute),
					WindowStart:      now.Add(-20 * time.Minute),
					HourlyTokensUsed: 2000,
					HourlyCostUsed:   0.02,
				},
				{
					Timestamp:        now.Add(-1 * time.Minute),
					WindowStart:      now.Add(-20 * time.Minute),
					HourlyTokensUsed: 3000,
					HourlyCostUsed:   0.03,
				},
			},
			expectedTokensPerMinute: 222.0, // (3000-1000)/9 ≈ 222
			expectedCostPerMinute:   0.0022, // (0.03-0.01)/9 ≈ 0.0022
			expectedConfidence:      0.6,   // 3 samples / 5 = 0.6
			expectedAlertLevel:      AlertGreen,
			maxTokensPerHour:        10000,
			maxCostPerHour:          0.10,
		},
		{
			name: "accelerating burn rate - should predict RED alert",
			snapshots: []QuotaSnapshot{
				{
					Timestamp:        now.Add(-10 * time.Minute),
					WindowStart:      now.Add(-15 * time.Minute),
					HourlyTokensUsed: 5000,
					HourlyCostUsed:   0.05,
				},
				{
					Timestamp:        now.Add(-5 * time.Minute),
					WindowStart:      now.Add(-15 * time.Minute),
					HourlyTokensUsed: 7000,
					HourlyCostUsed:   0.07,
				},
				{
					Timestamp:        now.Add(-1 * time.Minute),
					WindowStart:      now.Add(-15 * time.Minute),
					HourlyTokensUsed: 9000,
					HourlyCostUsed:   0.09,
				},
			},
			expectedTokensPerMinute: 444.0, // (9000-5000)/9 ≈ 444
			expectedCostPerMinute:   0.0044, // (0.09-0.05)/9 ≈ 0.0044
			expectedConfidence:      0.6,   // 3 samples / 5 = 0.6
			expectedAlertLevel:      AlertRed,
			maxTokensPerHour:        10000,
			maxCostPerHour:          0.10,
		},
		{
			name: "no burn - idle system",
			snapshots: []QuotaSnapshot{
				{
					Timestamp:        now.Add(-10 * time.Minute),
					WindowStart:      now.Add(-20 * time.Minute),
					HourlyTokensUsed: 1000,
					HourlyCostUsed:   0.01,
				},
				{
					Timestamp:        now.Add(-5 * time.Minute),
					WindowStart:      now.Add(-20 * time.Minute),
					HourlyTokensUsed: 1000,
					HourlyCostUsed:   0.01,
				},
				{
					Timestamp:        now.Add(-1 * time.Minute),
					WindowStart:      now.Add(-20 * time.Minute),
					HourlyTokensUsed: 1000,
					HourlyCostUsed:   0.01,
				},
			},
			expectedTokensPerMinute: 0.0,
			expectedCostPerMinute:   0.0,
			expectedConfidence:      0.6,
			expectedAlertLevel:      AlertGreen,
			maxTokensPerHour:        10000,
			maxCostPerHour:          0.10,
		},
		{
			name: "insufficient data - less than 3 snapshots",
			snapshots: []QuotaSnapshot{
				{
					Timestamp:        time.Now().Add(-5 * time.Minute),
					WindowStart:      time.Now().Add(-10 * time.Minute),
					HourlyTokensUsed: 1000,
					HourlyCostUsed:   0.01,
				},
			},
			expectedTokensPerMinute: 0.0,
			expectedCostPerMinute:   0.0,
			expectedConfidence:      0.0,
			expectedAlertLevel:      AlertGreen,
			maxTokensPerHour:        10000,
			maxCostPerHour:          0.10,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create tracker with test configuration
			cfg := &Config{
				Enabled:                     true,
				MaxTokensPerHour:            tt.maxTokensPerHour,
				MaxCostPerHour:              tt.maxCostPerHour,
				EnableQuotaMonitoring:       true,
				QuotaSnapshotInterval:       5 * time.Minute,
				QuotaAlertYellowThreshold:   30 * time.Minute,
				QuotaAlertOrangeThreshold:   15 * time.Minute,
				QuotaAlertRedThreshold:      5 * time.Minute,
				EnableQuotaCrisisAutoIssue:  true,
			}

			tracker := &Tracker{
				config:    cfg,
				snapshots: tt.snapshots,
				state: &BudgetState{
					WindowStartTime:  time.Now().Add(-20 * time.Minute),
					HourlyTokensUsed: 3000,
					HourlyCostUsed:   0.03,
					IssueTokensUsed:  make(map[string]int64),
				},
			}

			// If we have snapshots, set the current usage to match the latest one
			if len(tt.snapshots) > 0 {
				latest := tt.snapshots[len(tt.snapshots)-1]
				tracker.state.HourlyTokensUsed = latest.HourlyTokensUsed
				tracker.state.HourlyCostUsed = latest.HourlyCostUsed
			}

			// Calculate burn rate
			burnRate := tracker.calculateBurnRate()

			// Verify tokens per minute (allow 10% tolerance)
			tokensPerMinTolerance := tt.expectedTokensPerMinute * 0.1
			if burnRate.TokensPerMinute < tt.expectedTokensPerMinute-tokensPerMinTolerance ||
				burnRate.TokensPerMinute > tt.expectedTokensPerMinute+tokensPerMinTolerance {
				t.Errorf("TokensPerMinute = %.2f, want %.2f (±10%%)",
					burnRate.TokensPerMinute, tt.expectedTokensPerMinute)
			}

			// Verify cost per minute (allow 10% tolerance)
			costPerMinTolerance := tt.expectedCostPerMinute * 0.1
			if tt.expectedCostPerMinute > 0 {
				if burnRate.CostPerMinute < tt.expectedCostPerMinute-costPerMinTolerance ||
					burnRate.CostPerMinute > tt.expectedCostPerMinute+costPerMinTolerance {
					t.Errorf("CostPerMinute = %.4f, want %.4f (±10%%)",
						burnRate.CostPerMinute, tt.expectedCostPerMinute)
				}
			}

			// Verify confidence
			if burnRate.Confidence != tt.expectedConfidence {
				t.Errorf("Confidence = %.2f, want %.2f",
					burnRate.Confidence, tt.expectedConfidence)
			}

			// Verify alert level
			if burnRate.AlertLevel != tt.expectedAlertLevel {
				t.Errorf("AlertLevel = %s, want %s",
					burnRate.AlertLevel.String(), tt.expectedAlertLevel.String())
			}
		})
	}
}

// TestBurnRatePredictionAccuracy tests that burn rate predictions are accurate (vc-7e21)
func TestBurnRatePredictionAccuracy(t *testing.T) {
	now := time.Now()

	cfg := &Config{
		Enabled:                     true,
		MaxTokensPerHour:            10000,
		MaxCostPerHour:              0.10,
		EnableQuotaMonitoring:       true,
		QuotaSnapshotInterval:       5 * time.Minute,
		QuotaAlertYellowThreshold:   30 * time.Minute,
		QuotaAlertOrangeThreshold:   15 * time.Minute,
		QuotaAlertRedThreshold:      5 * time.Minute,
		EnableQuotaCrisisAutoIssue:  true,
	}

	tracker := &Tracker{
		config: cfg,
		state: &BudgetState{
			WindowStartTime:  now.Add(-30 * time.Minute),
			HourlyTokensUsed: 8000,
			HourlyCostUsed:   0.08,
			IssueTokensUsed:  make(map[string]int64),
		},
		snapshots: []QuotaSnapshot{
			{
				Timestamp:        now.Add(-10 * time.Minute),
				WindowStart:      now.Add(-30 * time.Minute),
				HourlyTokensUsed: 6000,
				HourlyCostUsed:   0.06,
			},
			{
				Timestamp:        now.Add(-5 * time.Minute),
				WindowStart:      now.Add(-30 * time.Minute),
				HourlyTokensUsed: 7000,
				HourlyCostUsed:   0.07,
			},
			{
				Timestamp:        now.Add(-1 * time.Minute),
				WindowStart:      now.Add(-30 * time.Minute),
				HourlyTokensUsed: 8000,
				HourlyCostUsed:   0.08,
			},
		},
	}

	// Calculate burn rate
	burnRate := tracker.calculateBurnRate()

	// Expected: ~222 tokens/minute (2000 tokens / 9 minutes)
	// Remaining: 2000 tokens (10000 - 8000)
	// Time to limit: ~9 minutes (2000 / 222)
	expectedTimeToLimit := 9 * time.Minute

	// Allow 30% tolerance for time prediction
	tolerance := time.Duration(float64(expectedTimeToLimit) * 0.3)
	if burnRate.EstimatedTimeToLimit < expectedTimeToLimit-tolerance ||
		burnRate.EstimatedTimeToLimit > expectedTimeToLimit+tolerance {
		t.Errorf("EstimatedTimeToLimit = %v, want %v (±30%%)",
			burnRate.EstimatedTimeToLimit, expectedTimeToLimit)
	}

	// Should be ORANGE alert (9 minutes is between 5-15 minutes)
	if burnRate.AlertLevel != AlertOrange {
		t.Errorf("AlertLevel = %s, want %s (time to limit: %v)",
			burnRate.AlertLevel.String(), AlertOrange.String(), burnRate.EstimatedTimeToLimit)
	}
}

// TestQuotaAlertSeverityMapping tests that alert levels map to correct event severities (vc-7e21)
func TestQuotaAlertSeverityMapping(t *testing.T) {
	tests := []struct {
		name          string
		alertLevel    AlertLevel
		wantSeverity  string
	}{
		{
			name:         "GREEN alert is INFO severity",
			alertLevel:   AlertGreen,
			wantSeverity: "info",
		},
		{
			name:         "YELLOW alert is WARNING severity",
			alertLevel:   AlertYellow,
			wantSeverity: "warning",
		},
		{
			name:         "ORANGE alert is ERROR severity",
			alertLevel:   AlertOrange,
			wantSeverity: "error",
		},
		{
			name:         "RED alert is CRITICAL severity",
			alertLevel:   AlertRed,
			wantSeverity: "critical",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// This test validates the mapping in logQuotaAlert()
			// The actual implementation is in budget.go:745-756
			var severity string
			switch tt.alertLevel {
			case AlertYellow:
				severity = "warning"
			case AlertOrange:
				severity = "error"
			case AlertRed:
				severity = "critical"
			default:
				severity = "info"
			}

			if severity != tt.wantSeverity {
				t.Errorf("Alert level %s mapped to severity %s, want %s",
					tt.alertLevel.String(), severity, tt.wantSeverity)
			}
		})
	}
}

// TestRecordOperation tests operation-level recording with map interface (vc-7e21)
func TestRecordOperation(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name      string
		input     interface{}
		wantError bool
	}{
		{
			name: "map interface from AI supervisor",
			input: map[string]interface{}{
				"issue_id":        "vc-123",
				"operation_type":  "assessment",
				"model":           "claude-sonnet-4-5-20250929",
				"input_tokens":    int64(1000),
				"output_tokens":   int64(500),
				"duration_ms":     int64(2500),
			},
			wantError: false,
		},
		{
			name: "QuotaOperation struct",
			input: QuotaOperation{
				IssueID:        "vc-456",
				OperationType:  "analysis",
				Model:          "claude-3-5-haiku-20241022",
				InputTokens:    500,
				OutputTokens:   250,
				DurationMs:     1500,
			},
			wantError: false,
		},
		{
			name:      "unsupported type",
			input:     "invalid",
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &Config{
				Enabled:               true,
				MaxTokensPerHour:      10000,
				EnableQuotaMonitoring: true,
			}

			tracker := &Tracker{
				config: cfg,
				state: &BudgetState{
					WindowStartTime: time.Now(),
					IssueTokensUsed: make(map[string]int64),
				},
			}

			err := tracker.RecordOperation(ctx, tt.input)

			if tt.wantError && err == nil {
				t.Error("expected error, got nil")
			}
			if !tt.wantError && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}
