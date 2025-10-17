package executor

import (
	"strings"
	"testing"
	"time"

	"github.com/steveyegge/vc/internal/gates"
	"github.com/steveyegge/vc/internal/types"
)

// TestNewPromptBuilder verifies that the PromptBuilder initializes correctly
func TestNewPromptBuilder(t *testing.T) {
	pb, err := NewPromptBuilder()
	if err != nil {
		t.Fatalf("NewPromptBuilder() failed: %v", err)
	}
	if pb == nil {
		t.Fatal("NewPromptBuilder() returned nil")
	}
	if pb.template == nil {
		t.Fatal("PromptBuilder.template is nil")
	}
}

// TestBuildPrompt_Minimal tests prompt generation with minimal context
func TestBuildPrompt_Minimal(t *testing.T) {
	pb, err := NewPromptBuilder()
	if err != nil {
		t.Fatalf("NewPromptBuilder() failed: %v", err)
	}

	ctx := &PromptContext{
		Issue: &types.Issue{
			ID:                 "vc-101",
			Title:              "Implement PromptBuilder",
			Description:        "Build comprehensive prompts from PromptContext",
			AcceptanceCriteria: "- PromptBuilder uses text/template\n- Template includes all context sections",
		},
	}

	prompt, err := pb.BuildPrompt(ctx)
	if err != nil {
		t.Fatalf("BuildPrompt() failed: %v", err)
	}

	// Verify key sections are present
	if !strings.Contains(prompt, "# YOUR TASK") {
		t.Error("Prompt missing 'YOUR TASK' section")
	}
	if !strings.Contains(prompt, "vc-101") {
		t.Error("Prompt missing issue ID")
	}
	if !strings.Contains(prompt, "Implement PromptBuilder") {
		t.Error("Prompt missing issue title")
	}
	if !strings.Contains(prompt, "## Description") {
		t.Error("Prompt missing description section")
	}
	if !strings.Contains(prompt, "## Acceptance Criteria") {
		t.Error("Prompt missing acceptance criteria section")
	}
}

// TestBuildPrompt_WithParentMission tests parent mission context rendering
func TestBuildPrompt_WithParentMission(t *testing.T) {
	pb, err := NewPromptBuilder()
	if err != nil {
		t.Fatalf("NewPromptBuilder() failed: %v", err)
	}

	ctx := &PromptContext{
		Issue: &types.Issue{
			ID:    "vc-101",
			Title: "Implement PromptBuilder",
		},
		ParentMission: &types.Issue{
			ID:          "vc-97",
			Title:       "Enhanced Context Management and Prompting",
			Description: "Enhance agent prompting with rich context",
		},
	}

	prompt, err := pb.BuildPrompt(ctx)
	if err != nil {
		t.Fatalf("BuildPrompt() failed: %v", err)
	}

	// Verify parent mission context
	if !strings.Contains(prompt, "# MISSION CONTEXT") {
		t.Error("Prompt missing 'MISSION CONTEXT' section")
	}
	if !strings.Contains(prompt, "vc-97") {
		t.Error("Prompt missing parent mission ID")
	}
	if !strings.Contains(prompt, "Enhanced Context Management") {
		t.Error("Prompt missing parent mission title")
	}
	if !strings.Contains(prompt, "Mission Goal:") {
		t.Error("Prompt missing mission goal")
	}
}

// TestBuildPrompt_WithRelatedIssues tests related issues rendering
func TestBuildPrompt_WithRelatedIssues(t *testing.T) {
	pb, err := NewPromptBuilder()
	if err != nil {
		t.Fatalf("NewPromptBuilder() failed: %v", err)
	}

	ctx := &PromptContext{
		Issue: &types.Issue{
			ID:    "vc-101",
			Title: "Implement PromptBuilder",
		},
		RelatedIssues: &RelatedIssues{
			Blockers: []*types.Issue{
				{ID: "vc-100", Title: "Implement ContextGatherer", Status: types.StatusClosed},
			},
			Dependents: []*types.Issue{
				{ID: "vc-102", Title: "Replace buildPrompt with PromptBuilder"},
			},
			Siblings: []*types.Issue{
				{ID: "vc-99", Title: "Implement execution history", Status: types.StatusClosed},
			},
		},
	}

	prompt, err := pb.BuildPrompt(ctx)
	if err != nil {
		t.Fatalf("BuildPrompt() failed: %v", err)
	}

	// Verify blockers section
	if !strings.Contains(prompt, "# BLOCKERS") {
		t.Error("Prompt missing 'BLOCKERS' section")
	}
	if !strings.Contains(prompt, "vc-100") {
		t.Error("Prompt missing blocker issue")
	}

	// Verify dependents section
	if !strings.Contains(prompt, "# DEPENDENT WORK") {
		t.Error("Prompt missing 'DEPENDENT WORK' section")
	}
	if !strings.Contains(prompt, "vc-102") {
		t.Error("Prompt missing dependent issue")
	}

	// Verify siblings section
	if !strings.Contains(prompt, "# SIBLING TASKS") {
		t.Error("Prompt missing 'SIBLING TASKS' section")
	}
	if !strings.Contains(prompt, "vc-99") {
		t.Error("Prompt missing sibling issue")
	}
}

// TestBuildPrompt_WithPreviousAttempts tests execution history rendering
func TestBuildPrompt_WithPreviousAttempts(t *testing.T) {
	pb, err := NewPromptBuilder()
	if err != nil {
		t.Fatalf("NewPromptBuilder() failed: %v", err)
	}

	startTime := time.Now().Add(-1 * time.Hour)
	completedTime := startTime.Add(30 * time.Minute)
	success := false
	exitCode := 1

	ctx := &PromptContext{
		Issue: &types.Issue{
			ID:    "vc-101",
			Title: "Implement PromptBuilder",
		},
		PreviousAttempts: []*types.ExecutionAttempt{
			{
				AttemptNumber: 1,
				StartedAt:     startTime,
				CompletedAt:   &completedTime,
				Success:       &success,
				ExitCode:      &exitCode,
				Summary:       "Template parsing failed",
				ErrorSample:   "parse error: unexpected EOF",
			},
		},
		ResumeHint: "Previous attempt #1 failed with exit code 1. Summary: Template parsing failed Please assess the current state and continue from where we left off.",
	}

	prompt, err := pb.BuildPrompt(ctx)
	if err != nil {
		t.Fatalf("BuildPrompt() failed: %v", err)
	}

	// Verify previous attempts section
	if !strings.Contains(prompt, "# PREVIOUS ATTEMPTS") {
		t.Error("Prompt missing 'PREVIOUS ATTEMPTS' section")
	}
	if !strings.Contains(prompt, "Attempt #1") {
		t.Error("Prompt missing attempt number")
	}
	if !strings.Contains(prompt, "✗ Failed") {
		t.Error("Prompt missing failure indicator")
	}
	if !strings.Contains(prompt, "Template parsing failed") {
		t.Error("Prompt missing attempt summary")
	}
	if !strings.Contains(prompt, "## Where We Left Off") {
		t.Error("Prompt missing resume hint section")
	}
	if !strings.Contains(prompt, "Continue from where the previous attempt left off") {
		t.Error("Prompt missing resume instruction")
	}
}

// TestBuildPrompt_WithQualityGates tests quality gate failure rendering
func TestBuildPrompt_WithQualityGates(t *testing.T) {
	pb, err := NewPromptBuilder()
	if err != nil {
		t.Fatalf("NewPromptBuilder() failed: %v", err)
	}

	ctx := &PromptContext{
		Issue: &types.Issue{
			ID:    "vc-101",
			Title: "Implement PromptBuilder",
		},
		QualityGateStatus: &GateStatus{
			Results: []*gates.Result{
				{
					Gate:   gates.GateTest,
					Passed: false,
					Output: "TestBuildPrompt failed: prompt missing section",
				},
				{
					Gate:   gates.GateLint,
					Passed: true,
				},
			},
			AllPassed: false,
		},
	}

	prompt, err := pb.BuildPrompt(ctx)
	if err != nil {
		t.Fatalf("BuildPrompt() failed: %v", err)
	}

	// Verify quality gates section
	if !strings.Contains(prompt, "# QUALITY GATES") {
		t.Error("Prompt missing 'QUALITY GATES' section")
	}
	if !strings.Contains(prompt, "⚠️") {
		t.Error("Prompt missing warning indicator")
	}
	if !strings.Contains(prompt, "test") {
		t.Error("Prompt missing failed gate")
	}
	if !strings.Contains(prompt, "TestBuildPrompt failed") {
		t.Error("Prompt missing gate output")
	}
	// Should not show passed gates
	if strings.Contains(prompt, "lint") {
		t.Error("Prompt should not show passed gates")
	}
}

// TestBuildPrompt_WithGitState tests git state rendering
func TestBuildPrompt_WithGitState(t *testing.T) {
	pb, err := NewPromptBuilder()
	if err != nil {
		t.Fatalf("NewPromptBuilder() failed: %v", err)
	}

	ctx := &PromptContext{
		Issue: &types.Issue{
			ID:    "vc-101",
			Title: "Implement PromptBuilder",
		},
		GitState: &GitState{
			CurrentBranch:      "feature/prompt-builder",
			UncommittedChanges: true,
			ModifiedFiles:      []string{"internal/executor/prompt.go", "internal/executor/prompt_test.go"},
		},
	}

	prompt, err := pb.BuildPrompt(ctx)
	if err != nil {
		t.Fatalf("BuildPrompt() failed: %v", err)
	}

	// Verify git state section
	if !strings.Contains(prompt, "# GIT STATE") {
		t.Error("Prompt missing 'GIT STATE' section")
	}
	if !strings.Contains(prompt, "feature/prompt-builder") {
		t.Error("Prompt missing branch name")
	}
	if !strings.Contains(prompt, "Uncommitted changes") {
		t.Error("Prompt missing uncommitted changes indicator")
	}
	if !strings.Contains(prompt, "prompt.go") {
		t.Error("Prompt missing modified file")
	}
}

// TestBuildPrompt_WithNotes tests notes rendering
func TestBuildPrompt_WithNotes(t *testing.T) {
	pb, err := NewPromptBuilder()
	if err != nil {
		t.Fatalf("NewPromptBuilder() failed: %v", err)
	}

	ctx := &PromptContext{
		Issue: &types.Issue{
			ID:    "vc-101",
			Title: "Implement PromptBuilder",
			Notes: "Remember to use text/template for flexible formatting",
		},
	}

	prompt, err := pb.BuildPrompt(ctx)
	if err != nil {
		t.Fatalf("BuildPrompt() failed: %v", err)
	}

	// Verify notes section
	if !strings.Contains(prompt, "# NOTES") {
		t.Error("Prompt missing 'NOTES' section")
	}
	if !strings.Contains(prompt, "text/template") {
		t.Error("Prompt missing notes content")
	}
}

// TestBuildPrompt_NilContext tests error handling for nil context
func TestBuildPrompt_NilContext(t *testing.T) {
	pb, err := NewPromptBuilder()
	if err != nil {
		t.Fatalf("NewPromptBuilder() failed: %v", err)
	}

	_, err = pb.BuildPrompt(nil)
	if err == nil {
		t.Error("BuildPrompt() should fail with nil context")
	}
	if !strings.Contains(err.Error(), "context cannot be nil") {
		t.Errorf("Expected 'context cannot be nil' error, got: %v", err)
	}
}

// TestBuildPrompt_NilIssue tests error handling for nil issue
func TestBuildPrompt_NilIssue(t *testing.T) {
	pb, err := NewPromptBuilder()
	if err != nil {
		t.Fatalf("NewPromptBuilder() failed: %v", err)
	}

	ctx := &PromptContext{
		Issue: nil,
	}

	_, err = pb.BuildPrompt(ctx)
	if err == nil {
		t.Error("BuildPrompt() should fail with nil issue")
	}
	if !strings.Contains(err.Error(), "issue cannot be nil") {
		t.Errorf("Expected 'issue cannot be nil' error, got: %v", err)
	}
}

// TestBuildPrompt_EmptyContext tests graceful handling of empty optional fields
func TestBuildPrompt_EmptyContext(t *testing.T) {
	pb, err := NewPromptBuilder()
	if err != nil {
		t.Fatalf("NewPromptBuilder() failed: %v", err)
	}

	ctx := &PromptContext{
		Issue: &types.Issue{
			ID:    "vc-101",
			Title: "Minimal Issue",
			// All optional fields are empty
		},
		// All optional context fields are nil
	}

	prompt, err := pb.BuildPrompt(ctx)
	if err != nil {
		t.Fatalf("BuildPrompt() should handle empty context gracefully: %v", err)
	}

	// Should still have the task section
	if !strings.Contains(prompt, "# YOUR TASK") {
		t.Error("Prompt missing basic task section")
	}

	// Should not have optional sections
	if strings.Contains(prompt, "# MISSION CONTEXT") {
		t.Error("Prompt should not have mission context section")
	}
	if strings.Contains(prompt, "# BLOCKERS") {
		t.Error("Prompt should not have blockers section")
	}
	if strings.Contains(prompt, "# PREVIOUS ATTEMPTS") {
		t.Error("Prompt should not have previous attempts section")
	}
}

// TestBuildPrompt_Comprehensive generates a comprehensive prompt with all context
// This test also serves as a sample prompt generator for review
func TestBuildPrompt_Comprehensive(t *testing.T) {
	pb, err := NewPromptBuilder()
	if err != nil {
		t.Fatalf("NewPromptBuilder() failed: %v", err)
	}

	startTime := time.Now().Add(-2 * time.Hour)
	completedTime := startTime.Add(45 * time.Minute)
	success := false
	exitCode := 1

	ctx := &PromptContext{
		Issue: &types.Issue{
			ID:    "vc-101",
			Title: "Implement PromptBuilder with structured templates",
			Description: "Build comprehensive prompts from PromptContext using structured templates.\n\n" +
				"The PromptBuilder should use text/template to create well-formatted prompts that include all available context.",
			Design: "Create internal/executor/prompt.go with:\n" +
				"- PromptBuilder struct holding text/template\n" +
				"- NewPromptBuilder() constructor\n" +
				"- BuildPrompt(ctx *PromptContext) method",
			AcceptanceCriteria: "- PromptBuilder uses text/template\n" +
				"- Template includes all context sections\n" +
				"- Handles missing context gracefully\n" +
				"- Unit tests with various context combinations",
			Notes: "Focus on readability - the AI agent needs to parse this prompt easily",
		},
		ParentMission: &types.Issue{
			ID:          "vc-97",
			Title:       "Enhanced Context Management and Prompting",
			Description: "Enhance agent prompting with rich context including sandbox location, mission hierarchy, previous attempts, related issues, and quality gate failures.",
		},
		RelatedIssues: &RelatedIssues{
			Blockers: []*types.Issue{
				{ID: "vc-100", Title: "Implement ContextGatherer with all context sources", Status: types.StatusClosed},
			},
			Dependents: []*types.Issue{
				{ID: "vc-102", Title: "Replace buildPrompt with PromptBuilder in agent spawning", Status: types.StatusOpen},
			},
			Siblings: []*types.Issue{
				{ID: "vc-99", Title: "Implement execution attempt history tracking", Status: types.StatusClosed},
				{ID: "vc-98", Title: "Design PromptContext types and ContextGatherer interface", Status: types.StatusClosed},
			},
		},
		PreviousAttempts: []*types.ExecutionAttempt{
			{
				AttemptNumber: 1,
				StartedAt:     startTime,
				CompletedAt:   &completedTime,
				Success:       &success,
				ExitCode:      &exitCode,
				Summary:       "Created prompt.go but template had syntax errors",
				ErrorSample:   "template: prompt:12: unexpected EOF in quoted string",
			},
		},
		QualityGateStatus: &GateStatus{
			Results: []*gates.Result{
				{Gate: gates.GateTest, Passed: false, Output: "TestBuildPrompt_Minimal failed"},
				{Gate: gates.GateLint, Passed: true},
			},
			AllPassed: false,
		},
		GitState: &GitState{
			CurrentBranch:      "feature/vc-101-prompt-builder",
			UncommittedChanges: true,
			ModifiedFiles:      []string{"internal/executor/prompt.go", "internal/executor/prompt_test.go"},
		},
		ResumeHint: "Previous attempt #1 failed with exit code 1. Summary: Created prompt.go but template had syntax errors " +
			"Error: template: prompt:12: unexpected EOF in quoted string Please assess the current state and continue from where we left off.",
	}

	prompt, err := pb.BuildPrompt(ctx)
	if err != nil {
		t.Fatalf("BuildPrompt() failed: %v", err)
	}

	// Print the comprehensive sample prompt for review
	t.Logf("\n========== COMPREHENSIVE SAMPLE PROMPT ==========\n%s\n========== END SAMPLE PROMPT ==========\n", prompt)

	// Verify all major sections are present
	sections := []string{
		"# MISSION CONTEXT",
		"# YOUR TASK",
		"# GIT STATE",
		"# BLOCKERS",
		"# DEPENDENT WORK",
		"# SIBLING TASKS",
		"# PREVIOUS ATTEMPTS",
		"# QUALITY GATES",
		"# NOTES",
	}

	for _, section := range sections {
		if !strings.Contains(prompt, section) {
			t.Errorf("Comprehensive prompt missing section: %s", section)
		}
	}
}

// TestFormatTime tests the time formatting helper
func TestFormatTime(t *testing.T) {
	testTime := time.Date(2025, 10, 16, 14, 30, 0, 0, time.UTC)
	formatted := formatTime(testTime)

	expected := "2025-10-16 14:30"
	if formatted != expected {
		t.Errorf("formatTime() = %q, want %q", formatted, expected)
	}
}

// TestTruncate tests the truncate helper
func TestTruncate(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		maxLen int
		want   string
	}{
		{
			name:   "short string",
			input:  "hello",
			maxLen: 10,
			want:   "hello",
		},
		{
			name:   "exact length",
			input:  "hello",
			maxLen: 5,
			want:   "hello",
		},
		{
			name:   "needs truncation",
			input:  "hello world",
			maxLen: 5,
			want:   "hello...",
		},
		{
			name:   "long error message",
			input:  "template: prompt:12: unexpected EOF in quoted string at line 45",
			maxLen: 30,
			want:   "template: prompt:12: unexpecte...",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := truncate(tt.input, tt.maxLen)
			if got != tt.want {
				t.Errorf("truncate(%q, %d) = %q, want %q", tt.input, tt.maxLen, got, tt.want)
			}
		})
	}
}
