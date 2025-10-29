package events

import "testing"

// TestIsValidFailureType tests the FailureType validation function (vc-228)
func TestIsValidFailureType(t *testing.T) {
	tests := []struct {
		name     string
		ft       string
		expected bool
	}{
		{"valid flaky", "flaky", true},
		{"valid real", "real", true},
		{"valid environmental", "environmental", true},
		{"valid unknown", "unknown", true},
		{"invalid type", "invalid", false},
		{"empty string", "", false},
		{"typo in flaky", "flakey", false},
		{"uppercase", "FLAKY", false},
		{"with space", "flaky ", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsValidFailureType(tt.ft)
			if result != tt.expected {
				t.Errorf("IsValidFailureType(%q) = %v, expected %v", tt.ft, result, tt.expected)
			}
		})
	}
}
