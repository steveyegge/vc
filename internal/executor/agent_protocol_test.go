package executor

import (
	"encoding/json"
	"testing"
)

func TestParseAgentReport_WithMarkers(t *testing.T) {
	output := `
Agent is working on the task...
Some diagnostic output here

=== AGENT REPORT ===
{
  "status": "completed",
  "summary": "Task completed successfully",
  "tests_added": true
}
=== END AGENT REPORT ===

Additional output after report
`

	report, found := ParseAgentReport(output)
	if !found {
		t.Fatal("Expected to find agent report")
	}

	if report.Status != AgentStatusCompleted {
		t.Errorf("Expected status=completed, got %s", report.Status)
	}
	if report.Summary != "Task completed successfully" {
		t.Errorf("Unexpected summary: %s", report.Summary)
	}
	if !report.TestsAdded {
		t.Error("Expected tests_added=true")
	}
}

func TestParseAgentReport_WithCodeFence(t *testing.T) {
	output := `
Agent output...

` + "```agent-report" + `
{
  "status": "blocked",
  "summary": "Hit a blocker",
  "blockers": ["Missing API key", "Service unavailable"]
}
` + "```" + `

More output...
`

	report, found := ParseAgentReport(output)
	if !found {
		t.Fatal("Expected to find agent report")
	}

	if report.Status != AgentStatusBlocked {
		t.Errorf("Expected status=blocked, got %s", report.Status)
	}
	if len(report.Blockers) != 2 {
		t.Errorf("Expected 2 blockers, got %d", len(report.Blockers))
	}
}

func TestParseAgentReport_WithJSONCodeFence(t *testing.T) {
	output := `
Agent working...

` + "```json" + `
{
  "status": "partial",
  "summary": "Partial completion",
  "completed": ["Task 1", "Task 2"],
  "remaining": ["Task 3", "Task 4"]
}
` + "```" + `
`

	report, found := ParseAgentReport(output)
	if !found {
		t.Fatal("Expected to find agent report")
	}

	if report.Status != AgentStatusPartial {
		t.Errorf("Expected status=partial, got %s", report.Status)
	}
	if len(report.Completed) != 2 {
		t.Errorf("Expected 2 completed items, got %d", len(report.Completed))
	}
	if len(report.Remaining) != 2 {
		t.Errorf("Expected 2 remaining items, got %d", len(report.Remaining))
	}
}

func TestParseAgentReport_RawJSON(t *testing.T) {
	output := `
Lots of agent output here
Multiple lines
...

Final status:
{
  "status": "completed",
  "summary": "All done",
  "files_modified": ["file1.go", "file2.go"]
}
`

	report, found := ParseAgentReport(output)
	if !found {
		t.Fatal("Expected to find agent report")
	}

	if report.Status != AgentStatusCompleted {
		t.Errorf("Expected status=completed, got %s", report.Status)
	}
	if len(report.FilesModified) != 2 {
		t.Errorf("Expected 2 files modified, got %d", len(report.FilesModified))
	}
}

func TestParseAgentReport_Decomposed(t *testing.T) {
	output := `
=== AGENT REPORT ===
{
  "status": "decomposed",
  "reasoning": "Task too large, breaking into 3 subtasks",
  "summary": "Created breakdown",
  "epic": {
    "title": "User Management",
    "description": "Complete user auth system"
  },
  "children": [
    {
      "title": "Create User model",
      "description": "Data structure and validation",
      "type": "task",
      "priority": "P1"
    },
    {
      "title": "Add login endpoint",
      "description": "POST /login API",
      "type": "task",
      "priority": "P1"
    }
  ]
}
=== END AGENT REPORT ===
`

	report, found := ParseAgentReport(output)
	if !found {
		t.Fatal("Expected to find agent report")
	}

	if report.Status != AgentStatusDecomposed {
		t.Errorf("Expected status=decomposed, got %s", report.Status)
	}
	if report.Epic == nil {
		t.Fatal("Expected epic definition")
	}
	if report.Epic.Title != "User Management" {
		t.Errorf("Unexpected epic title: %s", report.Epic.Title)
	}
	if len(report.Children) != 2 {
		t.Errorf("Expected 2 children, got %d", len(report.Children))
	}
	if report.Children[0].Priority != "P1" {
		t.Errorf("Expected P1 priority, got %s", report.Children[0].Priority)
	}
}

func TestParseAgentReport_NotFound(t *testing.T) {
	output := `
Just regular agent output
No structured report here
Nothing to see
`

	_, found := ParseAgentReport(output)
	if found {
		t.Error("Should not have found agent report")
	}
}

func TestParseAgentReport_InvalidJSON(t *testing.T) {
	output := `
=== AGENT REPORT ===
{
  "status": "completed"
  "summary": "Missing comma"
}
=== END AGENT REPORT ===
`

	_, found := ParseAgentReport(output)
	if found {
		t.Error("Should not parse invalid JSON")
	}
}

func TestAgentReportValidation_Completed(t *testing.T) {
	report := &AgentReport{
		Status:  AgentStatusCompleted,
		Summary: "All done",
	}

	if err := report.Validate(); err != nil {
		t.Errorf("Valid completed report failed validation: %v", err)
	}
}

func TestAgentReportValidation_BlockedMissingBlockers(t *testing.T) {
	report := &AgentReport{
		Status:  AgentStatusBlocked,
		Summary: "Blocked",
		// Missing blockers list
	}

	if err := report.Validate(); err == nil {
		t.Error("Should fail validation: blocked status requires blockers")
	}
}

func TestAgentReportValidation_PartialMissingRemaining(t *testing.T) {
	report := &AgentReport{
		Status:    AgentStatusPartial,
		Summary:   "Partial work",
		Completed: []string{"Task 1"},
		// Missing remaining list
	}

	if err := report.Validate(); err == nil {
		t.Error("Should fail validation: partial status requires remaining work")
	}
}

func TestAgentReportValidation_DecomposedMissingEpic(t *testing.T) {
	report := &AgentReport{
		Status:  AgentStatusDecomposed,
		Summary: "Decomposed",
		// Missing epic
		Children: []ChildIssue{
			{Title: "Child 1", Description: "Desc", Type: "task", Priority: "P1"},
		},
	}

	if err := report.Validate(); err == nil {
		t.Error("Should fail validation: decomposed status requires epic")
	}
}

func TestAgentReportValidation_DecomposedMissingChildren(t *testing.T) {
	report := &AgentReport{
		Status:  AgentStatusDecomposed,
		Summary: "Decomposed",
		Epic: &EpicDefinition{
			Title:       "Epic",
			Description: "Desc",
		},
		// Missing children
	}

	if err := report.Validate(); err == nil {
		t.Error("Should fail validation: decomposed status requires children")
	}
}

func TestAgentReportSerialization(t *testing.T) {
	report := &AgentReport{
		Status:     AgentStatusCompleted,
		Summary:    "Task done",
		TestsAdded: true,
		FilesModified: []string{"file1.go", "file2_test.go"},
	}

	// Serialize
	data, err := json.Marshal(report)
	if err != nil {
		t.Fatalf("Failed to marshal: %v", err)
	}

	// Deserialize
	var decoded AgentReport
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Failed to unmarshal: %v", err)
	}

	// Verify
	if decoded.Status != report.Status {
		t.Errorf("Status mismatch after round-trip")
	}
	if decoded.Summary != report.Summary {
		t.Errorf("Summary mismatch after round-trip")
	}
	if decoded.TestsAdded != report.TestsAdded {
		t.Errorf("TestsAdded mismatch after round-trip")
	}
}

func TestConvertToDiscoveredIssues(t *testing.T) {
	children := []ChildIssue{
		{
			Title:       "Fix bug X",
			Description: "Bug description",
			Type:        "bug",
			Priority:    "P0",
		},
		{
			Title:       "Add feature Y",
			Description: "Feature description",
			Type:        "feature",
			Priority:    "P2",
		},
	}

	discovered := ConvertToDiscoveredIssues(children)

	if len(discovered) != 2 {
		t.Errorf("Expected 2 discovered issues, got %d", len(discovered))
	}
	if discovered[0].Title != "Fix bug X" {
		t.Errorf("Unexpected title: %s", discovered[0].Title)
	}
	if discovered[0].Type != "bug" {
		t.Errorf("Unexpected type: %s", discovered[0].Type)
	}
	if discovered[1].Priority != "P2" {
		t.Errorf("Unexpected priority: %s", discovered[1].Priority)
	}
}

func TestAgentReportStatus_IsValid(t *testing.T) {
	tests := []struct {
		status AgentReportStatus
		valid  bool
	}{
		{AgentStatusCompleted, true},
		{AgentStatusBlocked, true},
		{AgentStatusPartial, true},
		{AgentStatusDecomposed, true},
		{"invalid", false},
		{"", false},
	}

	for _, tt := range tests {
		if got := tt.status.IsValid(); got != tt.valid {
			t.Errorf("IsValid(%s) = %v, want %v", tt.status, got, tt.valid)
		}
	}
}
