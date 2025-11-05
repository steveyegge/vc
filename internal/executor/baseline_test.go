package executor

import (
	"testing"
)

// TestParseTestFailures tests the test failure parsing function (vc-ebd9)
func TestParseTestFailures(t *testing.T) {
	tests := []struct {
		name           string
		output         string
		expectedCount  int
		expectedTests  []string
		expectedPkg    string
	}{
		{
			name: "single test failure",
			output: `
FAIL	github.com/steveyegge/vc/internal/executor	0.123s
--- FAIL: TestExample (0.00s)
    example_test.go:10: expected true, got false
`,
			expectedCount: 1,
			expectedTests: []string{"TestExample"},
			expectedPkg:   "github.com/steveyegge/vc/internal/executor",
		},
		{
			name: "multiple test failures",
			output: `
FAIL	github.com/steveyegge/vc/internal/gates	1.234s
--- FAIL: TestGateRunner (0.05s)
    gates_test.go:42: gate failed
--- FAIL: TestGateTimeout (0.10s)
    gates_test.go:89: timeout not triggered
`,
			expectedCount: 2,
			expectedTests: []string{"TestGateRunner", "TestGateTimeout"},
			expectedPkg:   "github.com/steveyegge/vc/internal/gates",
		},
		{
			name: "no failures",
			output: `
PASS
ok  	github.com/steveyegge/vc/internal/types	0.005s
`,
			expectedCount: 0,
			expectedTests: []string{},
			expectedPkg:   "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			failures := ParseTestFailures(tt.output)

			if len(failures) != tt.expectedCount {
				t.Errorf("ParseTestFailures() returned %d failures, expected %d", len(failures), tt.expectedCount)
			}

			for i, failure := range failures {
				if i >= len(tt.expectedTests) {
					t.Errorf("Unexpected extra failure: %s", failure.Test)
					continue
				}

				if failure.Test != tt.expectedTests[i] {
					t.Errorf("failure[%d].Test = %q, expected %q", i, failure.Test, tt.expectedTests[i])
				}

				if failure.Package != tt.expectedPkg {
					t.Errorf("failure[%d].Package = %q, expected %q", i, failure.Package, tt.expectedPkg)
				}

				if failure.Error == "" {
					t.Errorf("failure[%d].Error should not be empty", i)
				}
			}
		})
	}
}

// TestComputeFailureSignature tests signature computation (vc-ebd9)
func TestComputeFailureSignature(t *testing.T) {
	tests := []struct {
		name      string
		failure1  TestFailure
		failure2  TestFailure
		shouldMatch bool
	}{
		{
			name: "identical failures produce same signature",
			failure1: TestFailure{
				Package: "github.com/steveyegge/vc/internal/executor",
				Test:    "TestExample",
				Error:   "expected true, got false",
			},
			failure2: TestFailure{
				Package: "github.com/steveyegge/vc/internal/executor",
				Test:    "TestExample",
				Error:   "expected true, got false",
			},
			shouldMatch: true,
		},
		{
			name: "same failure with different line numbers produces same signature",
			failure1: TestFailure{
				Package: "github.com/steveyegge/vc/internal/executor",
				Test:    "TestExample",
				Error:   "example_test.go:10: expected true, got false",
			},
			failure2: TestFailure{
				Package: "github.com/steveyegge/vc/internal/executor",
				Test:    "TestExample",
				Error:   "example_test.go:42: expected true, got false",
			},
			shouldMatch: true,
		},
		{
			name: "different test names produce different signatures",
			failure1: TestFailure{
				Package: "github.com/steveyegge/vc/internal/executor",
				Test:    "TestExample1",
				Error:   "expected true, got false",
			},
			failure2: TestFailure{
				Package: "github.com/steveyegge/vc/internal/executor",
				Test:    "TestExample2",
				Error:   "expected true, got false",
			},
			shouldMatch: false,
		},
		{
			name: "different packages produce different signatures",
			failure1: TestFailure{
				Package: "github.com/steveyegge/vc/internal/executor",
				Test:    "TestExample",
				Error:   "expected true, got false",
			},
			failure2: TestFailure{
				Package: "github.com/steveyegge/vc/internal/gates",
				Test:    "TestExample",
				Error:   "expected true, got false",
			},
			shouldMatch: false,
		},
		{
			name: "same failure with different timestamps produces same signature",
			failure1: TestFailure{
				Package: "github.com/steveyegge/vc/internal/executor",
				Test:    "TestExample",
				Error:   "2024-11-04 12:34:56: timeout",
			},
			failure2: TestFailure{
				Package: "github.com/steveyegge/vc/internal/executor",
				Test:    "TestExample",
				Error:   "2024-11-04 18:22:33: timeout",
			},
			shouldMatch: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sig1 := ComputeFailureSignature(tt.failure1)
			sig2 := ComputeFailureSignature(tt.failure2)

			if tt.shouldMatch {
				if sig1 != sig2 {
					t.Errorf("Expected signatures to match:\n  sig1=%s\n  sig2=%s", sig1, sig2)
				}
			} else {
				if sig1 == sig2 {
					t.Errorf("Expected signatures to differ, but both are: %s", sig1)
				}
			}
		})
	}
}

// TestNormalizeError tests the error normalization function (vc-ebd9)
func TestNormalizeError(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "removes line numbers",
			input:    "example_test.go:123: error message",
			expected: "example_test.go:XXX: error message",
		},
		{
			name:     "removes timestamps",
			input:    "2024-11-04 12:34:56: timeout error",
			expected: "TIMESTAMP: timeout error",
		},
		{
			name:     "removes hex addresses",
			input:    "panic at 0x1a2b3c4d",
			expected: "panic at 0xXXXXXXXX",
		},
		{
			name:     "removes goroutine IDs",
			input:    "goroutine 123 [running]:",
			expected: "goroutine XXX [running]:",
		},
		{
			name:     "removes durations",
			input:    "test took 1.234s",
			expected: "test took X.XXXs",
		},
		{
			name:     "normalizes whitespace",
			input:    "error   with    extra   spaces",
			expected: "error with extra spaces",
		},
		{
			name:     "complex case with multiple patterns",
			input:    "goroutine 42 [running]: example_test.go:123: panic at 0xdeadbeef (took 2.5s)",
			expected: "goroutine XXX [running]: example_test.go:XXX: panic at 0xXXXXXXXX (took X.XXXs)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := normalizeError(tt.input)
			if result != tt.expected {
				t.Errorf("normalizeError() = %q, expected %q", result, tt.expected)
			}
		})
	}
}
