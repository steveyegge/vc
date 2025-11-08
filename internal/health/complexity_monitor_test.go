package health

import (
	"context"
	"strings"
	"testing"
)

// mockSupervisor is a mock AI supervisor for testing.
type mockComplexitySupervisor struct {
	response string
	err      error
}

func (m *mockComplexitySupervisor) CallAI(ctx context.Context, prompt string, operation string, model string, maxTokens int) (string, error) {
	if m.err != nil {
		return "", m.err
	}
	return m.response, nil
}

func TestComplexityMonitor_Name(t *testing.T) {
	monitor, err := NewComplexityMonitor(".", nil)
	if err != nil {
		t.Fatalf("NewComplexityMonitor failed: %v", err)
	}

	if got := monitor.Name(); got != "complexity" {
		t.Errorf("Name() = %q, want %q", got, "complexity")
	}
}

func TestComplexityMonitor_Philosophy(t *testing.T) {
	monitor, err := NewComplexityMonitor(".", nil)
	if err != nil {
		t.Fatalf("NewComplexityMonitor failed: %v", err)
	}

	philosophy := monitor.Philosophy()
	if philosophy == "" {
		t.Error("Philosophy() returned empty string")
	}

	// Should mention complexity
	if !strings.Contains(philosophy, "complex") && !strings.Contains(philosophy, "Complex") {
		t.Errorf("Philosophy doesn't mention complexity: %q", philosophy)
	}
}

func TestComplexityMonitor_Schedule(t *testing.T) {
	monitor, err := NewComplexityMonitor(".", nil)
	if err != nil {
		t.Fatalf("NewComplexityMonitor failed: %v", err)
	}

	schedule := monitor.Schedule()
	if schedule.Type != ScheduleEventBased {
		t.Errorf("Schedule type = %v, want %v", schedule.Type, ScheduleEventBased)
	}

	if schedule.EventTrigger != "every_20_issues" {
		t.Errorf("Event trigger = %q, want %q", schedule.EventTrigger, "every_20_issues")
	}
}

func TestComplexityMonitor_Cost(t *testing.T) {
	monitor, err := NewComplexityMonitor(".", nil)
	if err != nil {
		t.Fatalf("NewComplexityMonitor failed: %v", err)
	}

	cost := monitor.Cost()
	if cost.Category != CostExpensive {
		t.Errorf("Cost category = %v, want %v", cost.Category, CostExpensive)
	}

	if !cost.RequiresFullScan {
		t.Error("Cost should require full scan")
	}
}

func TestComplexityMonitor_parseGocycloOutput(t *testing.T) {
	monitor := &ComplexityMonitor{}

	tests := []struct {
		name    string
		output  string
		want    int
		wantErr bool
	}{
		{
			name: "single function",
			output: `45 executor executeIssue internal/executor/executor.go:123:1
`,
			want:    1,
			wantErr: false,
		},
		{
			name: "multiple functions",
			output: `80 beads TestGetIssues internal/storage/beads/integration_test.go:4092:1
76 beads TestMissionLifecycleEvents internal/storage/beads/integration_test.go:3493:1
63 beads TestGetReadyWorkWithMissionContext internal/storage/beads/integration_test.go:2822:1
`,
			want:    3,
			wantErr: false,
		},
		{
			name:    "empty output",
			output:  "",
			want:    0,
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			functions, err := monitor.parseGocycloOutput(tt.output)
			if (err != nil) != tt.wantErr {
				t.Errorf("parseGocycloOutput() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if len(functions) != tt.want {
				t.Errorf("parseGocycloOutput() returned %d functions, want %d", len(functions), tt.want)
			}

			// Verify first function if present
			if len(functions) > 0 {
				fn := functions[0]
				if fn.Complexity == 0 {
					t.Error("First function has zero complexity")
				}
				if fn.Package == "" {
					t.Error("First function has empty package")
				}
				if fn.Function == "" {
					t.Error("First function has empty function name")
				}
				if fn.FilePath == "" {
					t.Error("First function has empty file path")
				}
			}
		})
	}
}

func TestComplexityMonitor_extractFunctionBody(t *testing.T) {
	monitor := &ComplexityMonitor{
		MaxFunctionBodyLines: 10,
	}

	lines := []string{
		"func example() {",
		"    if x > 0 {",
		"        doSomething()",
		"    }",
		"}",
		"// next function",
	}

	body, err := monitor.extractFunctionBody(lines, 0)
	if err != nil {
		t.Fatalf("extractFunctionBody failed: %v", err)
	}

	expectedLines := 5 // Function definition through closing brace
	actualLines := len(strings.Split(body, "\n"))
	if actualLines != expectedLines {
		t.Errorf("extractFunctionBody returned %d lines, want %d", actualLines, expectedLines)
	}

	// Should not include "next function" comment
	if strings.Contains(body, "next function") {
		t.Error("extractFunctionBody included lines after function end")
	}
}

func TestComplexityMonitor_extractFunctionBody_truncation(t *testing.T) {
	monitor := &ComplexityMonitor{
		MaxFunctionBodyLines: 3,
	}

	// Create a function longer than the limit
	lines := make([]string, 100)
	lines[0] = "func longFunction() {"
	for i := 1; i < 99; i++ {
		lines[i] = "    statement()"
	}
	lines[99] = "}"

	body, err := monitor.extractFunctionBody(lines, 0)
	if err != nil {
		t.Fatalf("extractFunctionBody failed: %v", err)
	}

	// Should be truncated
	if !strings.Contains(body, "truncated") {
		t.Error("extractFunctionBody should indicate truncation for long functions")
	}
}

func TestComplexityMonitor_parseAIResponse(t *testing.T) {
	monitor := &ComplexityMonitor{}

	aiResponse := `{
  "functions_to_refactor": [
    {
      "function": "executor.executeIssue",
      "file": "internal/executor/executor.go",
      "line": 123,
      "complexity": 45,
      "reason": "Too many responsibilities",
      "suggested_approach": "Extract helper functions"
    }
  ],
  "acceptable_complexity": [
    {
      "function": "parser.Parse",
      "file": "internal/parser/parser.go",
      "line": 456,
      "complexity": 35,
      "justification": "Inherent parsing complexity",
      "recommendation": "Add more tests"
    }
  ]
}`

	functions := []*FunctionComplexity{
		{
			Complexity: 45,
			Package:    "executor",
			Function:   "executeIssue",
			FilePath:   "internal/executor/executor.go",
			Line:       123,
		},
	}

	issues, err := monitor.parseAIResponse(aiResponse, functions)
	if err != nil {
		t.Fatalf("parseAIResponse failed: %v", err)
	}

	if len(issues) != 1 {
		t.Errorf("parseAIResponse returned %d issues, want 1", len(issues))
	}

	if len(issues) > 0 {
		issue := issues[0]
		if issue.Category != "complexity" {
			t.Errorf("Issue category = %q, want %q", issue.Category, "complexity")
		}
		if issue.FilePath != "internal/executor/executor.go" {
			t.Errorf("Issue file = %q, want %q", issue.FilePath, "internal/executor/executor.go")
		}
		if issue.Severity != "medium" {
			t.Errorf("Issue severity = %q, want %q (complexity 45 should be medium)", issue.Severity, "medium")
		}
	}
}

func TestComplexityMonitor_determineSeverity(t *testing.T) {
	tests := []struct {
		complexity int
		want       string
	}{
		{20, "low"},
		{29, "low"},
		{30, "medium"},
		{49, "medium"},
		{50, "high"},
		{100, "high"},
	}

	for _, tt := range tests {
		t.Run(string(rune(tt.complexity+'0')), func(t *testing.T) {
			got := determineSeverity(tt.complexity)
			if got != tt.want {
				t.Errorf("determineSeverity(%d) = %q, want %q", tt.complexity, got, tt.want)
			}
		})
	}
}

func TestComplexityMonitor_countUniqueFiles(t *testing.T) {
	functions := []*FunctionComplexity{
		{FilePath: "file1.go"},
		{FilePath: "file2.go"},
		{FilePath: "file1.go"}, // Duplicate
		{FilePath: "file3.go"},
	}

	count := countUniqueFiles(functions)
	if count != 3 {
		t.Errorf("countUniqueFiles() = %d, want 3", count)
	}
}

func TestComplexityMonitor_calculateComplexityDistribution(t *testing.T) {
	functions := []*FunctionComplexity{
		{Complexity: 10},
		{Complexity: 20},
		{Complexity: 30},
		{Complexity: 40},
		{Complexity: 50},
	}

	dist := calculateComplexityDistribution(functions)

	if dist.Count != 5 {
		t.Errorf("Distribution count = %d, want 5", dist.Count)
	}

	if dist.Mean != 30.0 {
		t.Errorf("Distribution mean = %f, want 30.0", dist.Mean)
	}

	if dist.Median != 30.0 {
		t.Errorf("Distribution median = %f, want 30.0", dist.Median)
	}

	if dist.Min != 10.0 {
		t.Errorf("Distribution min = %f, want 10.0", dist.Min)
	}

	if dist.Max != 50.0 {
		t.Errorf("Distribution max = %f, want 50.0", dist.Max)
	}
}
