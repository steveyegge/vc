package main

import (
	"testing"
	"time"

	"github.com/steveyegge/vc/internal/events"
)

// TestExtractEventMetadata_AssessmentCompleted tests event metadata extraction for assessment completed events
func TestExtractEventMetadata_AssessmentCompleted(t *testing.T) {
	tests := []struct {
		name     string
		data     map[string]interface{}
		expected string
	}{
		{
			name: "all fields present",
			data: map[string]interface{}{
				"confidence":  0.85,
				"step_count":  5,
				"risk_count":  2,
			},
			expected: "85% | 5 steps | 2 risks",
		},
		{
			name: "missing confidence",
			data: map[string]interface{}{
				"step_count": 5,
				"risk_count": 2,
			},
			expected: "0% | 5 steps | 2 risks",
		},
		{
			name: "missing step_count",
			data: map[string]interface{}{
				"confidence": 0.85,
				"risk_count": 2,
			},
			expected: "85% | 0 steps | 2 risks",
		},
		{
			name: "missing risk_count",
			data: map[string]interface{}{
				"confidence":  0.85,
				"step_count":  5,
			},
			expected: "85% | 5 steps | 0 risks",
		},
		{
			name:     "all fields missing",
			data:     map[string]interface{}{},
			expected: "0% | 0 steps | 0 risks",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			event := &events.AgentEvent{
				Type:      events.EventTypeAssessmentCompleted,
				IssueID:   "vc-test",
				Message:   "Assessment complete",
				Severity:  events.SeverityInfo,
				Timestamp: time.Now(),
				Data:      tt.data,
			}
			got := extractEventMetadata(event)
			if got != tt.expected {
				t.Errorf("extractEventMetadata() = %q, expected %q", got, tt.expected)
			}
		})
	}
}

// TestExtractEventMetadata_QualityGatesCompleted tests event metadata extraction for quality gates events
func TestExtractEventMetadata_QualityGatesCompleted(t *testing.T) {
	tests := []struct {
		name     string
		data     map[string]interface{}
		expected string
	}{
		{
			name: "all fields present",
			data: map[string]interface{}{
				"result":       "passed",
				"failing_gate": "none",
				"duration_ms":  1500,
			},
			expected: "passed | none | 1.5s",
		},
		{
			name: "missing result",
			data: map[string]interface{}{
				"failing_gate": "test",
				"duration_ms":  1500,
			},
			expected: "unknown | test | 1.5s",
		},
		{
			name: "missing failing_gate",
			data: map[string]interface{}{
				"result":      "failed",
				"duration_ms": 1500,
			},
			expected: "failed | none | 1.5s",
		},
		{
			name: "missing duration_ms",
			data: map[string]interface{}{
				"result":       "passed",
				"failing_gate": "none",
			},
			expected: "passed | none | 0ms",
		},
		{
			name:     "all fields missing",
			data:     map[string]interface{}{},
			expected: "unknown | none | 0ms",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			event := &events.AgentEvent{
				Type:      events.EventTypeQualityGatesCompleted,
				IssueID:   "vc-test",
				Message:   "Quality gates complete",
				Severity:  events.SeverityInfo,
				Timestamp: time.Now(),
				Data:      tt.data,
			}
			got := extractEventMetadata(event)
			if got != tt.expected {
				t.Errorf("extractEventMetadata() = %q, expected %q", got, tt.expected)
			}
		})
	}
}

// TestExtractEventMetadata_AgentCompleted tests event metadata extraction for agent completed events
func TestExtractEventMetadata_AgentCompleted(t *testing.T) {
	tests := []struct {
		name     string
		data     map[string]interface{}
		expected string
	}{
		{
			name: "all fields present",
			data: map[string]interface{}{
				"duration_ms":    120000,
				"tools_used":     15,
				"files_modified": 3,
			},
			expected: "2.0m | 15 tools | 3 files",
		},
		{
			name: "missing duration_ms",
			data: map[string]interface{}{
				"tools_used":     15,
				"files_modified": 3,
			},
			expected: "0ms | 15 tools | 3 files",
		},
		{
			name: "missing tools_used",
			data: map[string]interface{}{
				"duration_ms":    120000,
				"files_modified": 3,
			},
			expected: "2.0m | 0 tools | 3 files",
		},
		{
			name: "missing files_modified",
			data: map[string]interface{}{
				"duration_ms": 120000,
				"tools_used":  15,
			},
			expected: "2.0m | 15 tools | 0 files",
		},
		{
			name:     "all fields missing",
			data:     map[string]interface{}{},
			expected: "0ms | 0 tools | 0 files",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			event := &events.AgentEvent{
				Type:      events.EventTypeAgentCompleted,
				IssueID:   "vc-test",
				Message:   "Agent complete",
				Severity:  events.SeverityInfo,
				Timestamp: time.Now(),
				Data:      tt.data,
			}
			got := extractEventMetadata(event)
			if got != tt.expected {
				t.Errorf("extractEventMetadata() = %q, expected %q", got, tt.expected)
			}
		})
	}
}

// TestExtractEventMetadata_AnalysisCompleted tests event metadata extraction for analysis completed events
func TestExtractEventMetadata_AnalysisCompleted(t *testing.T) {
	tests := []struct {
		name     string
		data     map[string]interface{}
		expected string
	}{
		{
			name: "all fields present",
			data: map[string]interface{}{
				"issues_discovered": 3,
				"confidence":        0.90,
				"duration_ms":       2500,
			},
			expected: "3 issues | 90% | 2.5s",
		},
		{
			name: "missing issues_discovered",
			data: map[string]interface{}{
				"confidence":  0.90,
				"duration_ms": 2500,
			},
			expected: "0 issues | 90% | 2.5s",
		},
		{
			name: "missing confidence",
			data: map[string]interface{}{
				"issues_discovered": 3,
				"duration_ms":       2500,
			},
			expected: "3 issues | 0% | 2.5s",
		},
		{
			name: "missing duration_ms",
			data: map[string]interface{}{
				"issues_discovered": 3,
				"confidence":        0.90,
			},
			expected: "3 issues | 90% | 0ms",
		},
		{
			name:     "all fields missing",
			data:     map[string]interface{}{},
			expected: "0 issues | 0% | 0ms",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			event := &events.AgentEvent{
				Type:      events.EventTypeAnalysisCompleted,
				IssueID:   "vc-test",
				Message:   "Analysis complete",
				Severity:  events.SeverityInfo,
				Timestamp: time.Now(),
				Data:      tt.data,
			}
			got := extractEventMetadata(event)
			if got != tt.expected {
				t.Errorf("extractEventMetadata() = %q, expected %q", got, tt.expected)
			}
		})
	}
}

// TestExtractEventMetadata_TestRun tests event metadata extraction for test run events
func TestExtractEventMetadata_TestRun(t *testing.T) {
	tests := []struct {
		name     string
		data     map[string]interface{}
		expected string
	}{
		{
			name: "test passed with all fields",
			data: map[string]interface{}{
				"passed":      true,
				"duration_ms": 500,
				"test_name":   "TestFooBar",
			},
			expected: "✓ passed | 500ms | TestFooBar",
		},
		{
			name: "test failed with all fields",
			data: map[string]interface{}{
				"passed":      false,
				"duration_ms": 500,
				"test_name":   "TestFooBar",
			},
			expected: "✗ failed | 500ms | TestFooBar",
		},
		{
			name: "missing passed field defaults to false",
			data: map[string]interface{}{
				"duration_ms": 500,
				"test_name":   "TestFooBar",
			},
			expected: "✗ failed | 500ms | TestFooBar",
		},
		{
			name: "missing duration_ms",
			data: map[string]interface{}{
				"passed":    true,
				"test_name": "TestFooBar",
			},
			expected: "✓ passed | 0ms | TestFooBar",
		},
		{
			name: "missing test_name",
			data: map[string]interface{}{
				"passed":      true,
				"duration_ms": 500,
			},
			expected: "✓ passed | 500ms",
		},
		{
			name:     "all fields missing",
			data:     map[string]interface{}{},
			expected: "✗ failed | 0ms",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			event := &events.AgentEvent{
				Type:      events.EventTypeTestRun,
				IssueID:   "vc-test",
				Message:   "Test run",
				Severity:  events.SeverityInfo,
				Timestamp: time.Now(),
				Data:      tt.data,
			}
			got := extractEventMetadata(event)
			if got != tt.expected {
				t.Errorf("extractEventMetadata() = %q, expected %q", got, tt.expected)
			}
		})
	}
}

// TestExtractEventMetadata_DeduplicationBatchCompleted tests event metadata extraction for deduplication batch events
func TestExtractEventMetadata_DeduplicationBatchCompleted(t *testing.T) {
	tests := []struct {
		name     string
		data     map[string]interface{}
		expected string
	}{
		{
			name: "all fields present",
			data: map[string]interface{}{
				"unique_count":        10,
				"duplicate_count":     5,
				"comparisons_made":    45,
				"processing_time_ms":  2000,
			},
			expected: "10 unique | 5 dupes | 45 comps | 2.0s",
		},
		{
			name: "missing unique_count",
			data: map[string]interface{}{
				"duplicate_count":    5,
				"comparisons_made":   45,
				"processing_time_ms": 2000,
			},
			expected: "0 unique | 5 dupes | 45 comps | 2.0s",
		},
		{
			name: "missing duplicate_count",
			data: map[string]interface{}{
				"unique_count":       10,
				"comparisons_made":   45,
				"processing_time_ms": 2000,
			},
			expected: "10 unique | 0 dupes | 45 comps | 2.0s",
		},
		{
			name: "missing comparisons_made",
			data: map[string]interface{}{
				"unique_count":       10,
				"duplicate_count":    5,
				"processing_time_ms": 2000,
			},
			expected: "10 unique | 5 dupes | 0 comps | 2.0s",
		},
		{
			name: "missing processing_time_ms",
			data: map[string]interface{}{
				"unique_count":     10,
				"duplicate_count":  5,
				"comparisons_made": 45,
			},
			expected: "10 unique | 5 dupes | 45 comps | 0ms",
		},
		{
			name:     "all fields missing",
			data:     map[string]interface{}{},
			expected: "0 unique | 0 dupes | 0 comps | 0ms",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			event := &events.AgentEvent{
				Type:      events.EventTypeDeduplicationBatchCompleted,
				IssueID:   "vc-test",
				Message:   "Deduplication batch complete",
				Severity:  events.SeverityInfo,
				Timestamp: time.Now(),
				Data:      tt.data,
			}
			got := extractEventMetadata(event)
			if got != tt.expected {
				t.Errorf("extractEventMetadata() = %q, expected %q", got, tt.expected)
			}
		})
	}
}

// TestExtractEventMetadata_DeduplicationDecision tests event metadata extraction for deduplication decision events
func TestExtractEventMetadata_DeduplicationDecision(t *testing.T) {
	tests := []struct {
		name     string
		data     map[string]interface{}
		expected string
	}{
		{
			name: "is duplicate with all fields",
			data: map[string]interface{}{
				"is_duplicate": true,
				"confidence":   0.95,
				"duplicate_of": "vc-123",
			},
			expected: "duplicate | 95% | vc-123",
		},
		{
			name: "is unique with all fields",
			data: map[string]interface{}{
				"is_duplicate": false,
				"confidence":   0.95,
				"duplicate_of": "vc-123",
			},
			expected: "unique | 95% | vc-123",
		},
		{
			name: "missing is_duplicate defaults to false",
			data: map[string]interface{}{
				"confidence":   0.95,
				"duplicate_of": "vc-123",
			},
			expected: "unique | 95% | vc-123",
		},
		{
			name: "missing confidence",
			data: map[string]interface{}{
				"is_duplicate": true,
				"duplicate_of": "vc-123",
			},
			expected: "duplicate | 0% | vc-123",
		},
		{
			name: "missing duplicate_of",
			data: map[string]interface{}{
				"is_duplicate": true,
				"confidence":   0.95,
			},
			expected: "duplicate | 95% | n/a",
		},
		{
			name:     "all fields missing",
			data:     map[string]interface{}{},
			expected: "unique | 0% | n/a",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			event := &events.AgentEvent{
				Type:      events.EventTypeDeduplicationDecision,
				IssueID:   "vc-test",
				Message:   "Deduplication decision",
				Severity:  events.SeverityInfo,
				Timestamp: time.Now(),
				Data:      tt.data,
			}
			got := extractEventMetadata(event)
			if got != tt.expected {
				t.Errorf("extractEventMetadata() = %q, expected %q", got, tt.expected)
			}
		})
	}
}

// TestExtractEventMetadata_BaselineTestFixCompleted tests event metadata extraction for baseline test fix events
func TestExtractEventMetadata_BaselineTestFixCompleted(t *testing.T) {
	tests := []struct {
		name     string
		data     map[string]interface{}
		expected string
	}{
		{
			name: "successful fix with all fields",
			data: map[string]interface{}{
				"fix_type":            "import",
				"success":             true,
				"tests_fixed":         2,
				"processing_time_ms":  3000,
			},
			expected: "import | ✓ | 2 tests | 3.0s",
		},
		{
			name: "failed fix with all fields",
			data: map[string]interface{}{
				"fix_type":            "import",
				"success":             false,
				"tests_fixed":         0,
				"processing_time_ms":  3000,
			},
			expected: "import | ✗ | 0 tests | 3.0s",
		},
		{
			name: "missing fix_type",
			data: map[string]interface{}{
				"success":            true,
				"tests_fixed":        2,
				"processing_time_ms": 3000,
			},
			expected: "unknown | ✓ | 2 tests | 3.0s",
		},
		{
			name: "missing success defaults to false",
			data: map[string]interface{}{
				"fix_type":           "import",
				"tests_fixed":        2,
				"processing_time_ms": 3000,
			},
			expected: "import | ✗ | 2 tests | 3.0s",
		},
		{
			name: "missing tests_fixed",
			data: map[string]interface{}{
				"fix_type":           "import",
				"success":            true,
				"processing_time_ms": 3000,
			},
			expected: "import | ✓ | 0 tests | 3.0s",
		},
		{
			name: "missing processing_time_ms",
			data: map[string]interface{}{
				"fix_type":    "import",
				"success":     true,
				"tests_fixed": 2,
			},
			expected: "import | ✓ | 2 tests | 0ms",
		},
		{
			name:     "all fields missing",
			data:     map[string]interface{}{},
			expected: "unknown | ✗ | 0 tests | 0ms",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			event := &events.AgentEvent{
				Type:      events.EventTypeBaselineTestFixCompleted,
				IssueID:   "vc-test",
				Message:   "Baseline test fix complete",
				Severity:  events.SeverityInfo,
				Timestamp: time.Now(),
				Data:      tt.data,
			}
			got := extractEventMetadata(event)
			if got != tt.expected {
				t.Errorf("extractEventMetadata() = %q, expected %q", got, tt.expected)
			}
		})
	}
}

// TestExtractEventMetadata_TestFailureDiagnosis tests event metadata extraction for test failure diagnosis events
func TestExtractEventMetadata_TestFailureDiagnosis(t *testing.T) {
	tests := []struct {
		name     string
		data     map[string]interface{}
		expected string
	}{
		{
			name: "all fields present",
			data: map[string]interface{}{
				"failure_type": "import_error",
				"confidence":   0.85,
				"root_cause":   "Missing import statement",
			},
			expected: "import_error | 85% | Missing import statement",
		},
		{
			name: "missing failure_type",
			data: map[string]interface{}{
				"confidence": 0.85,
				"root_cause": "Missing import statement",
			},
			expected: "unknown | 85% | Missing import statement",
		},
		{
			name: "missing confidence",
			data: map[string]interface{}{
				"failure_type": "import_error",
				"root_cause":   "Missing import statement",
			},
			expected: "import_error | 0% | Missing import statement",
		},
		{
			name: "missing root_cause",
			data: map[string]interface{}{
				"failure_type": "import_error",
				"confidence":   0.85,
			},
			expected: "import_error | 85%",
		},
		{
			name:     "all fields missing",
			data:     map[string]interface{}{},
			expected: "unknown | 0%",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			event := &events.AgentEvent{
				Type:      events.EventTypeTestFailureDiagnosis,
				IssueID:   "vc-test",
				Message:   "Test failure diagnosis",
				Severity:  events.SeverityInfo,
				Timestamp: time.Now(),
				Data:      tt.data,
			}
			got := extractEventMetadata(event)
			if got != tt.expected {
				t.Errorf("extractEventMetadata() = %q, expected %q", got, tt.expected)
			}
		})
	}
}

// TestExtractEventMetadata_SandboxCreationCompleted tests event metadata extraction for sandbox creation events
func TestExtractEventMetadata_SandboxCreationCompleted(t *testing.T) {
	tests := []struct {
		name     string
		data     map[string]interface{}
		expected string
	}{
		{
			name: "successful creation with all fields",
			data: map[string]interface{}{
				"branch_name": "vc-test-branch",
				"duration_ms": 1500,
				"success":     true,
			},
			expected: "vc-test-branch | 1.5s | ✓",
		},
		{
			name: "failed creation with all fields",
			data: map[string]interface{}{
				"branch_name": "vc-test-branch",
				"duration_ms": 1500,
				"success":     false,
			},
			expected: "vc-test-branch | 1.5s | ✗",
		},
		{
			name: "missing branch_name",
			data: map[string]interface{}{
				"duration_ms": 1500,
				"success":     true,
			},
			expected: "1.5s | ✓",
		},
		{
			name: "missing duration_ms",
			data: map[string]interface{}{
				"branch_name": "vc-test-branch",
				"success":     true,
			},
			expected: "vc-test-branch | 0ms | ✓",
		},
		{
			name: "missing success defaults to true",
			data: map[string]interface{}{
				"branch_name": "vc-test-branch",
				"duration_ms": 1500,
			},
			expected: "vc-test-branch | 1.5s | ✓",
		},
		{
			name:     "all fields missing",
			data:     map[string]interface{}{},
			expected: "0ms | ✓",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			event := &events.AgentEvent{
				Type:      events.EventTypeSandboxCreationCompleted,
				IssueID:   "vc-test",
				Message:   "Sandbox creation complete",
				Severity:  events.SeverityInfo,
				Timestamp: time.Now(),
				Data:      tt.data,
			}
			got := extractEventMetadata(event)
			if got != tt.expected {
				t.Errorf("extractEventMetadata() = %q, expected %q", got, tt.expected)
			}
		})
	}
}

// TestExtractEventMetadata_MissionCreated tests event metadata extraction for mission created events
func TestExtractEventMetadata_MissionCreated(t *testing.T) {
	tests := []struct {
		name     string
		data     map[string]interface{}
		expected string
	}{
		{
			name: "all fields present with approval",
			data: map[string]interface{}{
				"goal":              "Build user authentication",
				"approval_required": true,
				"actor":             "user@example.com",
			},
			expected: "Build user authentication | approval needed | user@example.com",
		},
		{
			name: "all fields present without approval",
			data: map[string]interface{}{
				"goal":              "Build user authentication",
				"approval_required": false,
				"actor":             "user@example.com",
			},
			expected: "Build user authentication | no approval | user@example.com",
		},
		{
			data: map[string]interface{}{
				"goal":              "",
				"approval_required": true,
				"actor":             "user@example.com",
			},
			expected: "approval needed | user@example.com",
		},
		{
			name: "missing approval_required defaults to false",
			data: map[string]interface{}{
				"goal":  "Build user authentication",
				"actor": "user@example.com",
			},
			expected: "Build user authentication | no approval | user@example.com",
		},
		{
			name: "missing actor",
			data: map[string]interface{}{
				"goal":              "Build user authentication",
				"approval_required": true,
			},
			expected: "Build user authentication | approval needed",
		},
		{
			name:     "all fields missing",
			data:     map[string]interface{}{},
			expected: "no approval",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			event := &events.AgentEvent{
				Type:      events.EventTypeMissionCreated,
				IssueID:   "vc-test",
				Message:   "Mission created",
				Severity:  events.SeverityInfo,
				Timestamp: time.Now(),
				Data:      tt.data,
			}
			got := extractEventMetadata(event)
			if got != tt.expected {
				t.Errorf("extractEventMetadata() = %q, expected %q", got, tt.expected)
			}
		})
	}
}

// TestExtractEventMetadata_EpicCompleted tests event metadata extraction for epic completed events
func TestExtractEventMetadata_EpicCompleted(t *testing.T) {
	tests := []struct {
		name     string
		data     map[string]interface{}
		expected string
	}{
		{
			name: "all fields present",
			data: map[string]interface{}{
				"children_completed": 5,
				"completion_method":  "all_done",
				"confidence":         0.95,
			},
			expected: "5 children | all_done | 95%",
		},
		{
			name: "missing children_completed",
			data: map[string]interface{}{
				"completion_method": "all_done",
				"confidence":        0.95,
			},
			expected: "0 children | all_done | 95%",
		},
		{
			name: "missing completion_method",
			data: map[string]interface{}{
				"children_completed": 5,
				"confidence":         0.95,
			},
			expected: "5 children | unknown | 95%",
		},
		{
			name: "missing confidence",
			data: map[string]interface{}{
				"children_completed": 5,
				"completion_method":  "all_done",
			},
			expected: "5 children | all_done | 0%",
		},
		{
			name:     "all fields missing",
			data:     map[string]interface{}{},
			expected: "0 children | unknown | 0%",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			event := &events.AgentEvent{
				Type:      events.EventTypeEpicCompleted,
				IssueID:   "vc-test",
				Message:   "Epic complete",
				Severity:  events.SeverityInfo,
				Timestamp: time.Now(),
				Data:      tt.data,
			}
			got := extractEventMetadata(event)
			if got != tt.expected {
				t.Errorf("extractEventMetadata() = %q, expected %q", got, tt.expected)
			}
		})
	}
}

// TestExtractEventMetadata_DefaultCase tests event metadata extraction for unknown event types
func TestExtractEventMetadata_DefaultCase(t *testing.T) {
	tests := []struct {
		name     string
		data     map[string]interface{}
		expected string
	}{
		{
			name: "error field present",
			data: map[string]interface{}{
				"error": "Something went wrong",
			},
			expected: "Something went wrong",
		},
		{
			name: "duration_ms field present",
			data: map[string]interface{}{
				"duration_ms": 5000,
			},
			expected: "5.0s",
		},
		{
			name: "confidence field present",
			data: map[string]interface{}{
				"confidence": 0.75,
			},
			expected: "75%",
		},
		{
			name: "all generic fields present",
			data: map[string]interface{}{
				"error":       "Something went wrong",
				"duration_ms": 5000,
				"confidence":  0.75,
			},
			expected: "Something went wrong | 5.0s | 75%",
		},
		{
			name: "confidence -1 is not included",
			data: map[string]interface{}{
				"error":       "Something went wrong",
				"duration_ms": 5000,
				"confidence":  -1.0,
			},
			expected: "Something went wrong | 5.0s",
		},
		{
			name:     "no fields present",
			data:     map[string]interface{}{},
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			event := &events.AgentEvent{
				Type:      events.EventType("unknown_event_type"),
				IssueID:   "vc-test",
				Message:   "Unknown event",
				Severity:  events.SeverityInfo,
				Timestamp: time.Now(),
				Data:      tt.data,
			}
			got := extractEventMetadata(event)
			if got != tt.expected {
				t.Errorf("extractEventMetadata() = %q, expected %q", got, tt.expected)
			}
		})
	}
}

// TestExtractEventMetadata_NoPanic tests that extractEventMetadata doesn't panic with nil or malformed data
func TestExtractEventMetadata_NoPanic(t *testing.T) {
	tests := []struct {
		name  string
		event *events.AgentEvent
	}{
		{
			name: "nil data map",
			event: &events.AgentEvent{
				Type:      events.EventTypeAssessmentCompleted,
				IssueID:   "vc-test",
				Message:   "Test",
				Severity:  events.SeverityInfo,
				Timestamp: time.Now(),
				Data:      nil,
			},
		},
		{
			name: "empty data map",
			event: &events.AgentEvent{
				Type:      events.EventTypeAssessmentCompleted,
				IssueID:   "vc-test",
				Message:   "Test",
				Severity:  events.SeverityInfo,
				Timestamp: time.Now(),
				Data:      map[string]interface{}{},
			},
		},
		{
			name: "wrong type in data",
			event: &events.AgentEvent{
				Type:      events.EventTypeAssessmentCompleted,
				IssueID:   "vc-test",
				Message:   "Test",
				Severity:  events.SeverityInfo,
				Timestamp: time.Now(),
				Data: map[string]interface{}{
					"confidence":  "should be float",
					"step_count":  "should be int",
					"risk_count":  []string{"should", "be", "int"},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("extractEventMetadata() panicked: %v", r)
				}
			}()
			// Just call it and make sure it doesn't panic
			_ = extractEventMetadata(tt.event)
		})
	}
}

// TestFormatDurationMs tests the duration formatting helper
func TestFormatDurationMs(t *testing.T) {
	tests := []struct {
		ms       int
		expected string
	}{
		{0, "0ms"},
		{100, "100ms"},
		{999, "999ms"},
		{1000, "1.0s"},
		{1500, "1.5s"},
		{59999, "60.0s"},
		{60000, "1.0m"},
		{90000, "1.5m"},
		{120000, "2.0m"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			got := formatDurationMs(tt.ms)
			if got != tt.expected {
				t.Errorf("formatDurationMs(%d) = %q, expected %q", tt.ms, got, tt.expected)
			}
		})
	}
}

// TestBuildDisplayMessage tests the new single-line display format (vc-9lvs)
func TestBuildDisplayMessage(t *testing.T) {
	tests := []struct {
		name     string
		event    *events.AgentEvent
		expected string
	}{
		{
			name: "agent_tool_use with file",
			event: &events.AgentEvent{
				Type:    events.EventTypeAgentToolUse,
				Message: "tool:Read internal/executor/agent.go",
				Data: map[string]interface{}{
					"tool_name":   "Read",
					"target_file": "internal/executor/agent.go",
				},
			},
			expected: "tool:Read internal/executor/agent.go",
		},
		{
			name: "agent_tool_use with bash command",
			event: &events.AgentEvent{
				Type:    events.EventTypeAgentToolUse,
				Message: "tool:Bash 'go test ./...'",
				Data: map[string]interface{}{
					"tool_name": "Bash",
					"command":   "go test ./...",
				},
			},
			expected: "tool:Bash 'go test ./...'",
		},
		{
			name: "agent_tool_use with long bash command (truncated)",
			event: &events.AgentEvent{
				Type:    events.EventTypeAgentToolUse,
				Message: "tool:Bash command",
				Data: map[string]interface{}{
					"tool_name": "Bash",
					"command":   "go test ./internal/executor -v -count=1 -timeout=30s -run TestAgentSpawn",
				},
			},
			expected: "tool:Bash 'go test ./internal/executor -v -count=1 -timeout=30s -run...'",
		},
		{
			name: "assessment_completed",
			event: &events.AgentEvent{
				Type:    events.EventTypeAssessmentCompleted,
				Message: "AI assessment completed",
				Data: map[string]interface{}{
					"confidence":  0.92,
					"step_count":  7,
					"risk_count":  3,
				},
			},
			expected: "Assessment complete: 92% confidence, 7 steps, 3 risks",
		},
		{
			name: "analysis_completed with issues",
			event: &events.AgentEvent{
				Type:    events.EventTypeAnalysisCompleted,
				Message: "Analysis complete",
				Data: map[string]interface{}{
					"issues_discovered": 2,
					"confidence":        0.88,
					"duration_ms":       1200,
				},
			},
			expected: "Analysis complete: 2 issues discovered (88% confidence)",
		},
		{
			name: "analysis_completed no issues",
			event: &events.AgentEvent{
				Type:    events.EventTypeAnalysisCompleted,
				Message: "Analysis complete",
				Data: map[string]interface{}{
					"issues_discovered": 0,
					"confidence":        0.95,
					"duration_ms":       800,
				},
			},
			expected: "Analysis complete: no issues discovered (95% confidence)",
		},
		{
			name: "agent_completed",
			event: &events.AgentEvent{
				Type:    events.EventTypeAgentCompleted,
				Message: "Agent completed",
				Data: map[string]interface{}{
					"duration_ms":    12500,
					"tools_used":     15,
					"files_modified": 3,
				},
			},
			expected: "Agent completed: 15 tools, 3 files modified, 12.5s",
		},
		{
			name: "quality_gates_passed",
			event: &events.AgentEvent{
				Type:    events.EventTypeQualityGatePass,
				Message: "Quality gates passed",
				Data: map[string]interface{}{
					"duration_ms": 8400,
				},
			},
			expected: "Quality gates passed (8.4s)",
		},
		{
			name: "quality_gate_failed",
			event: &events.AgentEvent{
				Type:    events.EventTypeQualityGateFail,
				Message: "Quality gate failed",
				Data: map[string]interface{}{
					"failing_gate": "test",
				},
			},
			expected: "Quality gate failed: test",
		},
		{
			name: "test_run passed",
			event: &events.AgentEvent{
				Type:    events.EventTypeTestRun,
				Message: "Test passed",
				Data: map[string]interface{}{
					"passed":      true,
					"test_name":   "TestAgentSpawn",
					"duration_ms": 150,
				},
			},
			expected: "Test ✓ passed: TestAgentSpawn (150ms)",
		},
		{
			name: "test_run failed",
			event: &events.AgentEvent{
				Type:    events.EventTypeTestRun,
				Message: "Test failed",
				Data: map[string]interface{}{
					"passed":      false,
					"test_name":   "TestCircuitBreaker",
					"duration_ms": 200,
				},
			},
			expected: "Test ✗ failed: TestCircuitBreaker (200ms)",
		},
		{
			name: "git_operation success",
			event: &events.AgentEvent{
				Type:    events.EventTypeGitOperation,
				Message: "Git command executed",
				Data: map[string]interface{}{
					"command": "commit",
					"args":    "-m 'Fix bug in agent.go'",
					"success": true,
				},
			},
			expected: "Git ✓: commit -m 'Fix bug in agent.go'",
		},
		{
			name: "deduplication_batch_completed",
			event: &events.AgentEvent{
				Type:    events.EventTypeDeduplicationBatchCompleted,
				Message: "Deduplication completed",
				Data: map[string]interface{}{
					"unique_count":    5,
					"duplicate_count": 2,
				},
			},
			expected: "Deduplication: 5 unique, 2 duplicates",
		},
		{
			name: "baseline_test_fix success",
			event: &events.AgentEvent{
				Type:    events.EventTypeBaselineTestFixCompleted,
				Message: "Baseline fix completed",
				Data: map[string]interface{}{
					"success":     true,
					"fix_type":    "flaky",
					"tests_fixed": 3,
				},
			},
			expected: "Baseline fix ✓: flaky (3 tests)",
		},
		{
			name: "sandbox_creation success",
			event: &events.AgentEvent{
				Type:    events.EventTypeSandboxCreationCompleted,
				Message: "Sandbox created",
				Data: map[string]interface{}{
					"branch_name": "mission/vc-123",
					"success":     true,
				},
			},
			expected: "Sandbox created: mission/vc-123",
		},
		{
			name: "mission_created",
			event: &events.AgentEvent{
				Type:    events.EventTypeMissionCreated,
				Message: "Mission created",
				Data: map[string]interface{}{
					"goal": "Build user authentication system",
				},
			},
			expected: "Mission created: Build user authentication system",
		},
		{
			name: "epic_completed",
			event: &events.AgentEvent{
				Type:    events.EventTypeEpicCompleted,
				Message: "Epic completed",
				Data: map[string]interface{}{
					"epic_title":          "Implement agent progress tracking",
					"children_completed":  12,
				},
			},
			expected: "Epic completed: Implement agent progress tracking (12 children)",
		},
		{
			name: "default event (fallback)",
			event: &events.AgentEvent{
				Type:    events.EventTypeIssueClaimed,
				Message: "Issue claimed for processing",
				Data:    map[string]interface{}{},
			},
			expected: "Issue claimed for processing",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildDisplayMessage(tt.event)
			if got != tt.expected {
				t.Errorf("buildDisplayMessage() = %q, expected %q", got, tt.expected)
			}
		})
	}
}
