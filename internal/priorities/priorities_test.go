package priorities

import "testing"

// TestCalculateDiscoveredPriority tests priority calculation for discovered issues (vc-152)
func TestCalculateDiscoveredPriority(t *testing.T) {
	tests := []struct {
		name          string
		parentPriority int
		discoveryType  string
		wantPriority   int
	}{
		// Blocker discovery type - always escalates to P0
		{
			name:          "blocker from P0 parent stays P0",
			parentPriority: 0,
			discoveryType:  "blocker",
			wantPriority:   0,
		},
		{
			name:          "blocker from P1 parent escalates to P0",
			parentPriority: 1,
			discoveryType:  "blocker",
			wantPriority:   0,
		},
		{
			name:          "blocker from P2 parent escalates to P0",
			parentPriority: 2,
			discoveryType:  "blocker",
			wantPriority:   0,
		},
		{
			name:          "blocker from P3 parent escalates to P0",
			parentPriority: 3,
			discoveryType:  "blocker",
			wantPriority:   0,
		},

		// Related discovery type - parent priority + 1, capped at P3
		{
			name:          "related from P0 parent becomes P1",
			parentPriority: 0,
			discoveryType:  "related",
			wantPriority:   1,
		},
		{
			name:          "related from P1 parent becomes P2",
			parentPriority: 1,
			discoveryType:  "related",
			wantPriority:   2,
		},
		{
			name:          "related from P2 parent becomes P3",
			parentPriority: 2,
			discoveryType:  "related",
			wantPriority:   3,
		},
		{
			name:          "related from P3 parent stays P3 (capped)",
			parentPriority: 3,
			discoveryType:  "related",
			wantPriority:   3,
		},

		// Background discovery type - always P2
		{
			name:          "background from P0 parent becomes P2",
			parentPriority: 0,
			discoveryType:  "background",
			wantPriority:   2,
		},
		{
			name:          "background from P1 parent becomes P2",
			parentPriority: 1,
			discoveryType:  "background",
			wantPriority:   2,
		},
		{
			name:          "background from P2 parent stays P2",
			parentPriority: 2,
			discoveryType:  "background",
			wantPriority:   2,
		},
		{
			name:          "background from P3 parent becomes P2 (escalated)",
			parentPriority: 3,
			discoveryType:  "background",
			wantPriority:   2,
		},

		// Unknown/empty discovery type - inherit parent priority
		{
			name:          "unknown type from P0 parent inherits P0",
			parentPriority: 0,
			discoveryType:  "unknown",
			wantPriority:   0,
		},
		{
			name:          "unknown type from P1 parent inherits P1",
			parentPriority: 1,
			discoveryType:  "unknown",
			wantPriority:   1,
		},
		{
			name:          "empty type from P2 parent inherits P2",
			parentPriority: 2,
			discoveryType:  "",
			wantPriority:   2,
		},
		{
			name:          "unknown type from P3 parent inherits P3",
			parentPriority: 3,
			discoveryType:  "unknown",
			wantPriority:   3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CalculateDiscoveredPriority(tt.parentPriority, tt.discoveryType)
			if got != tt.wantPriority {
				t.Errorf("CalculateDiscoveredPriority(%d, %q) = %d, want %d",
					tt.parentPriority, tt.discoveryType, got, tt.wantPriority)
			}
		})
	}
}

// TestCalculateDiscoveredPriority_EdgeCases tests edge cases
func TestCalculateDiscoveredPriority_EdgeCases(t *testing.T) {
	tests := []struct {
		name          string
		parentPriority int
		discoveryType  string
		wantPriority   int
	}{
		{
			name:          "negative parent priority with blocker",
			parentPriority: -1,
			discoveryType:  "blocker",
			wantPriority:   0, // Capped at P0
		},
		{
			name:          "very high parent priority with related",
			parentPriority: 10,
			discoveryType:  "related",
			wantPriority:   3, // Capped at P3
		},
		{
			name:          "case sensitive discovery type",
			parentPriority: 1,
			discoveryType:  "BLOCKER", // Should not match
			wantPriority:   1,         // Falls through to default (inherit)
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CalculateDiscoveredPriority(tt.parentPriority, tt.discoveryType)
			if got != tt.wantPriority {
				t.Errorf("CalculateDiscoveredPriority(%d, %q) = %d, want %d",
					tt.parentPriority, tt.discoveryType, got, tt.wantPriority)
			}
		})
	}
}
