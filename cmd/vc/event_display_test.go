package main

import (
	"testing"
	"time"

	"github.com/steveyegge/vc/internal/events"
)

// TestExtractEventMetadata_AssessmentCompleted tests AssessmentCompleted event type
// with missing confidence/step_count/risk_count fields (vc-90dl)
func TestExtractEventMetadata_AssessmentCompleted(t *testing.T) {
	tests := []struct {
		name     string
		data     map[string]interface{}
		expected string
	}{
		{
			name: "all fields present",
			data: map[string]interface{}{
				"confidence": 0.85,
				"step_count": 5,
				"risk_count": 2,
			},
			expected: "85% | 5 steps | 2 risks",
		},
		{
			name:     "all fields missing",
			data:     map[string]interface{}{},
			expected: "0% | 0 steps | 0 risks",
		},
		{
			name: "confidence missing",
			data: map[string]interface{}{
				"step_count": 3,
				"risk_count": 1,
			},
			expected: "0% | 3 steps | 1 risks",
		},
		{
			name: "step_count missing",
			data: map[string]interface{}{
				"confidence": 0.92,
				"risk_count": 0,
			},
			expected: "92% | 0 steps | 0 risks",
		},
		{
			name: "risk_count missing",
			data: map[string]interface{}{
				"confidence": 0.75,
				"step_count": 4,
			},
			expected: "75% | 4 steps | 0 risks",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			event := &events.AgentEvent{
				Type:      events.EventTypeAssessmentCompleted,
				Data:      tt.data,
				Timestamp: time.Now(),
			}
			result := extractEventMetadata(event)
			if result != tt.expected {
				t.Errorf("extractEventMetadata() = %q, want %q", result, tt.expected)
			}
		})
	}
}

// TestExtractEventMetadata_QualityGatesCompleted tests QualityGatesCompleted event type
// with missing result/failing_gate/duration_ms fields (vc-90dl)
func TestExtractEventMetadata_QualityGatesCompleted(t *testing.T) {
	tests := []struct {
		name     string
		data     map[string]interface{}
		expected string
	}{
		{
			name: "all fields present - success",
			data: map[string]interface{}{
				"result":       "passed",
				"failing_gate": "none",
				"duration_ms":  2500,
			},
			expected: "passed | none | 2.5s",
		},
		{
			name: "all fields present - failure",
			data: map[string]interface{}{
				"result":       "failed",
				"failing_gate": "lint",
				"duration_ms":  1200,
			},
			expected: "failed | lint | 1.2s",
		},
		{
			name:     "all fields missing",
			data:     map[string]interface{}{},
			expected: "unknown | none | 0ms",
		},
		{
			name: "result missing",
			data: map[string]interface{}{
				"failing_gate": "test",
				"duration_ms":  3000,
			},
			expected: "unknown | test | 3.0s",
		},
		{
			name: "failing_gate missing",
			data: map[string]interface{}{
				"result":      "passed",
				"duration_ms": 500,
			},
			expected: "passed | none | 500ms",
		},
		{
			name: "duration_ms missing",
			data: map[string]interface{}{
				"result":       "failed",
				"failing_gate": "build",
			},
			expected: "failed | build | 0ms",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			event := &events.AgentEvent{
				Type:      events.EventTypeQualityGatesCompleted,
				Data:      tt.data,
				Timestamp: time.Now(),
			}
			result := extractEventMetadata(event)
			if result != tt.expected {
				t.Errorf("extractEventMetadata() = %q, want %q", result, tt.expected)
			}
		})
	}
}

// TestExtractEventMetadata_AgentCompleted tests AgentCompleted event type
// with missing duration_ms/tools_used/files_modified fields (vc-90dl)
func TestExtractEventMetadata_AgentCompleted(t *testing.T) {
	tests := []struct {
		name     string
		data     map[string]interface{}
		expected string
	}{
		{
			name: "all fields present",
			data: map[string]interface{}{
				"duration_ms":    45000,
				"tools_used":     12,
				"files_modified": 3,
			},
			expected: "45.0s | 12 tools | 3 files",
		},
		{
			name:     "all fields missing",
			data:     map[string]interface{}{},
			expected: "0ms | 0 tools | 0 files",
		},
		{
			name: "duration_ms missing",
			data: map[string]interface{}{
				"tools_used":     5,
				"files_modified": 2,
			},
			expected: "0ms | 5 tools | 2 files",
		},
		{
			name: "tools_used missing",
			data: map[string]interface{}{
				"duration_ms":    8500,
				"files_modified": 1,
			},
			expected: "8.5s | 0 tools | 1 files",
		},
		{
			name: "files_modified missing",
			data: map[string]interface{}{
				"duration_ms": 120000,
				"tools_used":  20,
			},
			expected: "2.0m | 20 tools | 0 files",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			event := &events.AgentEvent{
				Type:      events.EventTypeAgentCompleted,
				Data:      tt.data,
				Timestamp: time.Now(),
			}
			result := extractEventMetadata(event)
			if result != tt.expected {
				t.Errorf("extractEventMetadata() = %q, want %q", result, tt.expected)
			}
		})
	}
}

// TestExtractEventMetadata_AnalysisCompleted tests AnalysisCompleted event type
// with missing issues_discovered/confidence/duration_ms fields (vc-90dl)
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
				"confidence":        0.88,
				"duration_ms":       2200,
			},
			expected: "3 issues | 88% | 2.2s",
		},
		{
			name:     "all fields missing",
			data:     map[string]interface{}{},
			expected: "0 issues | 0% | 0ms",
		},
		{
			name: "issues_discovered missing",
			data: map[string]interface{}{
				"confidence":  0.95,
				"duration_ms": 1500,
			},
			expected: "0 issues | 95% | 1.5s",
		},
		{
			name: "confidence missing",
			data: map[string]interface{}{
				"issues_discovered": 2,
				"duration_ms":       3000,
			},
			expected: "2 issues | 0% | 3.0s",
		},
		{
			name: "duration_ms missing",
			data: map[string]interface{}{
				"issues_discovered": 1,
				"confidence":        0.70,
			},
			expected: "1 issues | 70% | 0ms",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			event := &events.AgentEvent{
				Type:      events.EventTypeAnalysisCompleted,
				Data:      tt.data,
				Timestamp: time.Now(),
			}
			result := extractEventMetadata(event)
			if result != tt.expected {
				t.Errorf("extractEventMetadata() = %q, want %q", result, tt.expected)
			}
		})
	}
}

// TestExtractEventMetadata_TestRun tests TestRun event type
// with missing passed/duration_ms/test_name fields (vc-90dl)
func TestExtractEventMetadata_TestRun(t *testing.T) {
	tests := []struct {
		name     string
		data     map[string]interface{}
		expected string
	}{
		{
			name: "all fields present - passed",
			data: map[string]interface{}{
				"passed":      true,
				"duration_ms": 1800,
				"test_name":   "TestFooBar",
			},
			expected: "✓ passed | 1.8s | TestFooBar",
		},
		{
			name: "all fields present - failed",
			data: map[string]interface{}{
				"passed":      false,
				"duration_ms": 500,
				"test_name":   "TestBazQux",
			},
			expected: "✗ failed | 500ms | TestBazQux",
		},
		{
			name:     "all fields missing",
			data:     map[string]interface{}{},
			expected: "✗ failed | 0ms",
		},
		{
			name: "passed missing (defaults to false)",
			data: map[string]interface{}{
				"duration_ms": 2000,
				"test_name":   "TestMissing",
			},
			expected: "✗ failed | 2.0s | TestMissing",
		},
		{
			name: "duration_ms missing",
			data: map[string]interface{}{
				"passed":    true,
				"test_name": "TestQuick",
			},
			expected: "✓ passed | 0ms | TestQuick",
		},
		{
			name: "test_name missing",
			data: map[string]interface{}{
				"passed":      true,
				"duration_ms": 3500,
			},
			expected: "✓ passed | 3.5s",
		},
		{
			name: "long test name truncation",
			data: map[string]interface{}{
				"passed":      true,
				"duration_ms": 1000,
				"test_name":   "TestVeryLongTestNameThatExceedsTheMaximumLengthAllowed",
			},
			expected: "✓ passed | 1.0s | TestVeryLongTestNameTh...",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			event := &events.AgentEvent{
				Type:      events.EventTypeTestRun,
				Data:      tt.data,
				Timestamp: time.Now(),
			}
			result := extractEventMetadata(event)
			if result != tt.expected {
				t.Errorf("extractEventMetadata() = %q, want %q", result, tt.expected)
			}
		})
	}
}

// TestExtractEventMetadata_DeduplicationBatchCompleted tests DeduplicationBatchCompleted event type
// with missing unique_count/duplicate_count/comparisons_made/processing_time_ms fields (vc-90dl)
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
				"duplicate_count":     3,
				"comparisons_made":    45,
				"processing_time_ms":  1500,
			},
			expected: "10 unique | 3 dupes | 45 comps | 1.5s",
		},
		{
			name:     "all fields missing",
			data:     map[string]interface{}{},
			expected: "0 unique | 0 dupes | 0 comps | 0ms",
		},
		{
			name: "unique_count missing",
			data: map[string]interface{}{
				"duplicate_count":    2,
				"comparisons_made":   20,
				"processing_time_ms": 800,
			},
			expected: "0 unique | 2 dupes | 20 comps | 800ms",
		},
		{
			name: "duplicate_count missing",
			data: map[string]interface{}{
				"unique_count":       8,
				"comparisons_made":   28,
				"processing_time_ms": 1200,
			},
			expected: "8 unique | 0 dupes | 28 comps | 1.2s",
		},
		{
			name: "comparisons_made missing",
			data: map[string]interface{}{
				"unique_count":       5,
				"duplicate_count":    1,
				"processing_time_ms": 600,
			},
			expected: "5 unique | 1 dupes | 0 comps | 600ms",
		},
		{
			name: "processing_time_ms missing",
			data: map[string]interface{}{
				"unique_count":     12,
				"duplicate_count":  4,
				"comparisons_made": 66,
			},
			expected: "12 unique | 4 dupes | 66 comps | 0ms",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			event := &events.AgentEvent{
				Type:      events.EventTypeDeduplicationBatchCompleted,
				Data:      tt.data,
				Timestamp: time.Now(),
			}
			result := extractEventMetadata(event)
			if result != tt.expected {
				t.Errorf("extractEventMetadata() = %q, want %q", result, tt.expected)
			}
		})
	}
}

// TestExtractEventMetadata_DeduplicationDecision tests DeduplicationDecision event type
// with missing is_duplicate/confidence/duplicate_of fields (vc-90dl)
func TestExtractEventMetadata_DeduplicationDecision(t *testing.T) {
	tests := []struct {
		name     string
		data     map[string]interface{}
		expected string
	}{
		{
			name: "all fields present - duplicate",
			data: map[string]interface{}{
				"is_duplicate": true,
				"confidence":   0.95,
				"duplicate_of": "vc-123",
			},
			expected: "duplicate | 95% | vc-123",
		},
		{
			name: "all fields present - unique",
			data: map[string]interface{}{
				"is_duplicate": false,
				"confidence":   0.80,
				"duplicate_of": "",
			},
			expected: "unique | 80%",
		},
		{
			name:     "all fields missing",
			data:     map[string]interface{}{},
			expected: "unique | 0% | n/a",
		},
		{
			name: "is_duplicate missing (defaults to false)",
			data: map[string]interface{}{
				"confidence":   0.88,
				"duplicate_of": "vc-456",
			},
			expected: "unique | 88% | vc-456",
		},
		{
			name: "confidence missing",
			data: map[string]interface{}{
				"is_duplicate": true,
				"duplicate_of": "vc-789",
			},
			expected: "duplicate | 0% | vc-789",
		},
		{
			name: "duplicate_of missing",
			data: map[string]interface{}{
				"is_duplicate": true,
				"confidence":   0.92,
			},
			expected: "duplicate | 92% | n/a",
		},
		{
			name: "long duplicate_of truncation",
			data: map[string]interface{}{
				"is_duplicate": true,
				"confidence":   0.90,
				"duplicate_of": "vc-very-long-issue-id-that-needs-truncation",
			},
			expected: "duplicate | 90% | vc-very-long...",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			event := &events.AgentEvent{
				Type:      events.EventTypeDeduplicationDecision,
				Data:      tt.data,
				Timestamp: time.Now(),
			}
			result := extractEventMetadata(event)
			if result != tt.expected {
				t.Errorf("extractEventMetadata() = %q, want %q", result, tt.expected)
			}
		})
	}
}

// TestExtractEventMetadata_BaselineTestFixCompleted tests BaselineTestFixCompleted event type
// with missing fix_type/success/tests_fixed/processing_time_ms fields (vc-90dl)
func TestExtractEventMetadata_BaselineTestFixCompleted(t *testing.T) {
	tests := []struct {
		name     string
		data     map[string]interface{}
		expected string
	}{
		{
			name: "all fields present - success",
			data: map[string]interface{}{
				"fix_type":            "import",
				"success":             true,
				"tests_fixed":         2,
				"processing_time_ms":  3500,
			},
			expected: "import | ✓ | 2 tests | 3.5s",
		},
		{
			name: "all fields present - failure",
			data: map[string]interface{}{
				"fix_type":            "dependency",
				"success":             false,
				"tests_fixed":         0,
				"processing_time_ms":  1200,
			},
			expected: "dependency | ✗ | 0 tests | 1.2s",
		},
		{
			name:     "all fields missing",
			data:     map[string]interface{}{},
			expected: "unknown | ✗ | 0 tests | 0ms",
		},
		{
			name: "fix_type missing",
			data: map[string]interface{}{
				"success":            true,
				"tests_fixed":        1,
				"processing_time_ms": 2000,
			},
			expected: "unknown | ✓ | 1 tests | 2.0s",
		},
		{
			name: "success missing (defaults to false)",
			data: map[string]interface{}{
				"fix_type":           "syntax",
				"tests_fixed":        3,
				"processing_time_ms": 4000,
			},
			expected: "syntax | ✗ | 3 tests | 4.0s",
		},
		{
			name: "tests_fixed missing",
			data: map[string]interface{}{
				"fix_type":           "type",
				"success":            true,
				"processing_time_ms": 2500,
			},
			expected: "type | ✓ | 0 tests | 2.5s",
		},
		{
			name: "processing_time_ms missing",
			data: map[string]interface{}{
				"fix_type":    "refactor",
				"success":     true,
				"tests_fixed": 5,
			},
			expected: "refactor | ✓ | 5 tests | 0ms",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			event := &events.AgentEvent{
				Type:      events.EventTypeBaselineTestFixCompleted,
				Data:      tt.data,
				Timestamp: time.Now(),
			}
			result := extractEventMetadata(event)
			if result != tt.expected {
				t.Errorf("extractEventMetadata() = %q, want %q", result, tt.expected)
			}
		})
	}
}

// TestExtractEventMetadata_TestFailureDiagnosis tests TestFailureDiagnosis event type
// with missing failure_type/confidence/root_cause fields (vc-90dl)
func TestExtractEventMetadata_TestFailureDiagnosis(t *testing.T) {
	tests := []struct {
		name     string
		data     map[string]interface{}
		expected string
	}{
		{
			name: "all fields present",
			data: map[string]interface{}{
				"failure_type": "assertion",
				"confidence":   0.90,
				"root_cause":   "nil pointer dereference in handler",
			},
			expected: "assertion | 90% | nil pointer dereference in ...",
		},
		{
			name:     "all fields missing",
			data:     map[string]interface{}{},
			expected: "unknown | 0%",
		},
		{
			name: "failure_type missing",
			data: map[string]interface{}{
				"confidence": 0.85,
				"root_cause": "timeout",
			},
			expected: "unknown | 85% | timeout",
		},
		{
			name: "confidence missing",
			data: map[string]interface{}{
				"failure_type": "panic",
				"root_cause":   "index out of bounds",
			},
			expected: "panic | 0% | index out of bounds",
		},
		{
			name: "root_cause missing",
			data: map[string]interface{}{
				"failure_type": "compilation",
				"confidence":   0.95,
			},
			expected: "compilation | 95%",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			event := &events.AgentEvent{
				Type:      events.EventTypeTestFailureDiagnosis,
				Data:      tt.data,
				Timestamp: time.Now(),
			}
			result := extractEventMetadata(event)
			if result != tt.expected {
				t.Errorf("extractEventMetadata() = %q, want %q", result, tt.expected)
			}
		})
	}
}

// TestExtractEventMetadata_SandboxCreationCompleted tests SandboxCreationCompleted event type
// with missing branch_name/duration_ms/success fields (vc-90dl)
func TestExtractEventMetadata_SandboxCreationCompleted(t *testing.T) {
	tests := []struct {
		name     string
		data     map[string]interface{}
		expected string
	}{
		{
			name: "all fields present - success",
			data: map[string]interface{}{
				"branch_name": "sandbox/vc-123-feature",
				"duration_ms": 2500,
				"success":     true,
			},
			expected: "sandbox/vc-123-feature | 2.5s | ✓",
		},
		{
			name: "all fields present - failure",
			data: map[string]interface{}{
				"branch_name": "sandbox/vc-456-bugfix",
				"duration_ms": 1500,
				"success":     false,
			},
			expected: "sandbox/vc-456-bugfix | 1.5s | ✗",
		},
		{
			name:     "all fields missing",
			data:     map[string]interface{}{},
			expected: "0ms | ✓",
		},
		{
			name: "branch_name missing",
			data: map[string]interface{}{
				"duration_ms": 3000,
				"success":     true,
			},
			expected: "3.0s | ✓",
		},
		{
			name: "duration_ms missing",
			data: map[string]interface{}{
				"branch_name": "sandbox/test",
				"success":     true,
			},
			expected: "sandbox/test | 0ms | ✓",
		},
		{
			name: "success missing (defaults to true)",
			data: map[string]interface{}{
				"branch_name": "sandbox/default",
				"duration_ms": 1800,
			},
			expected: "sandbox/default | 1.8s | ✓",
		},
		{
			name: "long branch name truncation",
			data: map[string]interface{}{
				"branch_name": "sandbox/vc-789-very-long-branch-name-that-exceeds-limit",
				"duration_ms": 2000,
				"success":     true,
			},
			expected: "sandbox/vc-789-very-lo... | 2.0s | ✓",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			event := &events.AgentEvent{
				Type:      events.EventTypeSandboxCreationCompleted,
				Data:      tt.data,
				Timestamp: time.Now(),
			}
			result := extractEventMetadata(event)
			if result != tt.expected {
				t.Errorf("extractEventMetadata() = %q, want %q", result, tt.expected)
			}
		})
	}
}

// TestExtractEventMetadata_MissionCreated tests MissionCreated event type
// with missing phase_count/approval_required/actor fields (vc-90dl)
func TestExtractEventMetadata_MissionCreated(t *testing.T) {
	tests := []struct {
		name     string
		data     map[string]interface{}
		expected string
	}{
		{
			name: "all fields present - approval required",
			data: map[string]interface{}{
				"phase_count":       3,
				"approval_required": true,
				"actor":             "user@example.com",
			},
			expected: "3 phases | approval needed | user@example.com",
		},
		{
			name: "all fields present - no approval",
			data: map[string]interface{}{
				"phase_count":       5,
				"approval_required": false,
				"actor":             "ai-supervisor",
			},
			expected: "5 phases | no approval | ai-supervisor",
		},
		{
			name:     "all fields missing",
			data:     map[string]interface{}{},
			expected: "0 phases | no approval",
		},
		{
			name: "phase_count missing",
			data: map[string]interface{}{
				"approval_required": true,
				"actor":             "admin",
			},
			expected: "0 phases | approval needed | admin",
		},
		{
			name: "approval_required missing (defaults to false)",
			data: map[string]interface{}{
				"phase_count": 2,
				"actor":       "bot",
			},
			expected: "2 phases | no approval | bot",
		},
		{
			name: "actor missing",
			data: map[string]interface{}{
				"phase_count":       4,
				"approval_required": true,
			},
			expected: "4 phases | approval needed",
		},
		{
			name: "long actor truncation",
			data: map[string]interface{}{
				"phase_count":       1,
				"approval_required": false,
				"actor":             "very-long-actor-name-that-exceeds-the-maximum-allowed-length",
			},
			expected: "1 phases | no approval | very-long-actor-n...",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			event := &events.AgentEvent{
				Type:      events.EventTypeMissionCreated,
				Data:      tt.data,
				Timestamp: time.Now(),
			}
			result := extractEventMetadata(event)
			if result != tt.expected {
				t.Errorf("extractEventMetadata() = %q, want %q", result, tt.expected)
			}
		})
	}
}

// TestExtractEventMetadata_EpicCompleted tests EpicCompleted event type
// with missing children_completed/completion_method/confidence fields (vc-90dl)
func TestExtractEventMetadata_EpicCompleted(t *testing.T) {
	tests := []struct {
		name     string
		data     map[string]interface{}
		expected string
	}{
		{
			name: "all fields present",
			data: map[string]interface{}{
				"children_completed": 8,
				"completion_method":  "all_complete",
				"confidence":         0.95,
			},
			expected: "8 children | all_complete | 95%",
		},
		{
			name:     "all fields missing",
			data:     map[string]interface{}{},
			expected: "0 children | unknown | 0%",
		},
		{
			name: "children_completed missing",
			data: map[string]interface{}{
				"completion_method": "partial",
				"confidence":        0.80,
			},
			expected: "0 children | partial | 80%",
		},
		{
			name: "completion_method missing",
			data: map[string]interface{}{
				"children_completed": 5,
				"confidence":         0.88,
			},
			expected: "5 children | unknown | 88%",
		},
		{
			name: "confidence missing",
			data: map[string]interface{}{
				"children_completed": 3,
				"completion_method":  "manual",
			},
			expected: "3 children | manual | 0%",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			event := &events.AgentEvent{
				Type:      events.EventTypeEpicCompleted,
				Data:      tt.data,
				Timestamp: time.Now(),
			}
			result := extractEventMetadata(event)
			if result != tt.expected {
				t.Errorf("extractEventMetadata() = %q, want %q", result, tt.expected)
			}
		})
	}
}

// TestExtractEventMetadata_DefaultCase tests the default case fallback
// with missing error/duration_ms/confidence fields (vc-90dl)
func TestExtractEventMetadata_DefaultCase(t *testing.T) {
	tests := []struct {
		name     string
		eventType events.EventType
		data     map[string]interface{}
		expected string
	}{
		{
			name:      "all fields present",
			eventType: "custom_event_type",
			data: map[string]interface{}{
				"error":       "something went wrong",
				"duration_ms": 5000,
				"confidence":  0.75,
			},
			expected: "something went wrong | 5.0s | 75%",
		},
		{
			name:      "all fields missing",
			eventType: "unknown_event_type",
			data:      map[string]interface{}{},
			expected:  "",
		},
		{
			name:      "only error present",
			eventType: "error_event",
			data: map[string]interface{}{
				"error": "file not found",
			},
			expected: "file not found",
		},
		{
			name:      "only duration_ms present",
			eventType: "timed_event",
			data: map[string]interface{}{
				"duration_ms": 3500,
			},
			expected: "3.5s",
		},
		{
			name:      "only confidence present",
			eventType: "confidence_event",
			data: map[string]interface{}{
				"confidence": 0.92,
			},
			expected: "92%",
		},
		{
			name:      "error and duration_ms",
			eventType: "partial_event",
			data: map[string]interface{}{
				"error":       "timeout",
				"duration_ms": 10000,
			},
			expected: "timeout | 10.0s",
		},
		{
			name:      "long error truncation",
			eventType: "long_error_event",
			data: map[string]interface{}{
				"error": "this is a very long error message that definitely exceeds the maximum allowed length for display",
			},
			expected: "this is a very long error message that definite...",
		},
		{
			name:      "negative confidence ignored",
			eventType: "negative_confidence_event",
			data: map[string]interface{}{
				"confidence": -1.0,
			},
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			event := &events.AgentEvent{
				Type:      tt.eventType,
				Data:      tt.data,
				Timestamp: time.Now(),
			}
			result := extractEventMetadata(event)
			if result != tt.expected {
				t.Errorf("extractEventMetadata() = %q, want %q", result, tt.expected)
			}
		})
	}
}

// TestExtractEventMetadata_EmptyData tests that empty data maps don't cause panics
func TestExtractEventMetadata_EmptyData(t *testing.T) {
	eventTypes := []events.EventType{
		events.EventTypeAssessmentCompleted,
		events.EventTypeQualityGatesCompleted,
		events.EventTypeAgentCompleted,
		events.EventTypeAnalysisCompleted,
		events.EventTypeTestRun,
		events.EventTypeDeduplicationBatchCompleted,
		events.EventTypeDeduplicationDecision,
		events.EventTypeBaselineTestFixCompleted,
		events.EventTypeTestFailureDiagnosis,
		events.EventTypeSandboxCreationCompleted,
		events.EventTypeMissionCreated,
		events.EventTypeEpicCompleted,
	}

	for _, eventType := range eventTypes {
		t.Run(string(eventType), func(t *testing.T) {
			event := &events.AgentEvent{
				Type:      eventType,
				Data:      map[string]interface{}{},
				Timestamp: time.Now(),
			}
			// Should not panic
			result := extractEventMetadata(event)
			// Result should be non-empty for most event types (default values)
			// We just care that it doesn't panic
			_ = result
		})
	}
}

// TestExtractEventMetadata_NilData tests that nil data maps don't cause panics
func TestExtractEventMetadata_NilData(t *testing.T) {
	event := &events.AgentEvent{
		Type:      events.EventTypeAssessmentCompleted,
		Data:      nil,
		Timestamp: time.Now(),
	}
	// Should not panic - the helper functions check for nil
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("extractEventMetadata panicked with nil data: %v", r)
		}
	}()
	_ = extractEventMetadata(event)
}
