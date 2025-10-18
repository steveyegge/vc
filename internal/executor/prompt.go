package executor

import (
	"bytes"
	"fmt"
	"text/template"
	"time"
)

// PromptBuilder constructs comprehensive prompts from PromptContext using structured templates.
// It aggregates all available context (issue details, dependencies, history, sandbox state)
// into a well-formatted prompt for AI agents.
type PromptBuilder struct {
	template *template.Template
}

// promptTemplate defines the structure for agent prompts
// It includes all context sections with conditional rendering
const promptTemplate = `{{if .ParentMission -}}
# MISSION CONTEXT

You are working on a subtask of a larger mission:

**Mission**: {{.ParentMission.ID}} - {{.ParentMission.Title}}

{{if .ParentMission.Description -}}
Mission Goal:
{{.ParentMission.Description}}
{{end}}
{{end}}
# YOUR TASK

**Issue**: {{.Issue.ID}} - {{.Issue.Title}}

{{if .Issue.Description -}}
## Description
{{.Issue.Description}}

{{end}}
{{if .Issue.Design -}}
## Design
{{.Issue.Design}}

{{end}}
{{if .Issue.AcceptanceCriteria -}}
## Acceptance Criteria
{{.Issue.AcceptanceCriteria}}

{{end}}
{{if .Sandbox -}}
# ENVIRONMENT

You are working in an isolated sandbox:
{{if .Sandbox.Path -}}
- **Path**: {{.Sandbox.Path}}
{{end}}
{{if .Sandbox.GitBranch -}}
- **Branch**: {{.Sandbox.GitBranch}}
{{end}}
{{if .Sandbox.BeadsDB -}}
- **Database**: {{.Sandbox.BeadsDB}}
{{end}}
{{if .Sandbox.ModifiedFiles -}}

Modified files ({{len .Sandbox.ModifiedFiles}}):
{{range .Sandbox.ModifiedFiles -}}
- {{.}}
{{end}}
{{end}}
{{end}}
{{if .GitState -}}
{{if .GitState.CurrentBranch -}}
# GIT STATE

- **Branch**: {{.GitState.CurrentBranch}}
{{if .GitState.UncommittedChanges -}}
- **Uncommitted changes**: Yes
{{end}}
{{if .GitState.ModifiedFiles -}}

Modified files:
{{range .GitState.ModifiedFiles -}}
- {{.}}
{{end}}
{{end}}
{{end}}
{{end}}
{{if .RelatedIssues -}}
{{if .RelatedIssues.Blockers -}}
# BLOCKERS

This task depends on:
{{range .RelatedIssues.Blockers -}}
- {{.ID}}: {{.Title}} [{{.Status}}]
{{end}}

{{end}}
{{if .RelatedIssues.Dependents -}}
# DEPENDENT WORK

The following tasks are waiting for this:
{{range .RelatedIssues.Dependents -}}
- {{.ID}}: {{.Title}}
{{end}}

{{end}}
{{if .RelatedIssues.Siblings -}}
# SIBLING TASKS

Other tasks in the same mission:
{{range .RelatedIssues.Siblings -}}
- {{.ID}}: {{.Title}} [{{.Status}}]
{{end}}

{{end}}
{{end}}
{{if .PreviousAttempts -}}
# PREVIOUS ATTEMPTS

This task has been attempted {{len .PreviousAttempts}} time(s) before:
{{range .PreviousAttempts -}}

## Attempt #{{.AttemptNumber}} ({{formatTime .StartedAt}})
{{if .CompletedAt -}}
- Completed: {{formatTime .CompletedAt}}
{{else -}}
- Status: Incomplete (may have crashed)
{{end}}
{{if not (isNil .Success) -}}
{{if deref .Success -}}
- Result: ✓ Success
{{else -}}
- Result: ✗ Failed{{if .ExitCode}} (exit code {{derefInt .ExitCode}}){{end}}
{{end}}
{{end}}
{{if .Summary -}}
- Summary: {{.Summary}}
{{end}}
{{if .ErrorSample -}}
- Error: {{truncate .ErrorSample 200}}
{{end}}
{{end}}
{{if .ResumeHint -}}

## Where We Left Off
{{.ResumeHint}}
{{end}}
{{end}}
{{if .QualityGateStatus -}}
{{if not .QualityGateStatus.AllPassed -}}
# QUALITY GATES

⚠️  The following quality gates failed:
{{range .QualityGateStatus.Results -}}
{{if not .Passed -}}
- **{{.Gate}}**: Failed
{{if .Output -}}
  Output: {{truncate .Output 200}}
{{end}}
{{if .Error -}}
  Error: {{.Error}}
{{end}}
{{end}}
{{end}}

{{end}}
{{end}}
{{if .Issue.Notes -}}
# NOTES
{{.Issue.Notes}}

{{end}}
---

# EXECUTION DIRECTIVE

You are operating in **autonomous mode**. Your job is to:

1. **Implement the required changes** - Make the necessary code modifications to complete this task
2. **Do not ask for permission** - Proceed directly with implementation
3. **Only stop if technically blocked** - Don't stop for permission or policy concerns

## Examples of Autonomous Behavior

✓ **Good**: You identify a bug in the code → You implement the fix → You verify it works
✗ **Bad**: You identify a bug → You describe the fix → You ask permission to implement it

✓ **Good**: Requirements are clear → You implement the solution → You test it
✗ **Bad**: Requirements are clear → You ask if you should implement it

✓ **Good**: You encounter a technical blocker (missing dependency, broken API) → You report it and stop
✗ **Bad**: You know exactly what to do → You ask permission to proceed

## When to Ask Questions

Only ask clarifying questions if:
- Requirements are genuinely ambiguous (not just complex)
- Multiple valid approaches exist with different trade-offs that need user input
- You discover a technical blocker that makes the task impossible

**Do not ask for permission to make code changes** - that is your job.

{{if .Sandbox -}}
{{if .Sandbox.Path -}}
Work in the sandbox at: {{.Sandbox.Path}}
{{end}}
{{end}}
{{if .ResumeHint -}}
Continue from where the previous attempt left off.
{{else -}}
Begin implementation now.
{{end}}`

// NewPromptBuilder creates a new PromptBuilder with the default template
func NewPromptBuilder() (*PromptBuilder, error) {
	// Create template with helper functions
	tmpl := template.New("prompt").Funcs(template.FuncMap{
		"formatTime": formatTime,
		"truncate":   truncate,
		"isNil":      isNil,
		"deref":      deref,
		"derefInt":   derefInt,
	})

	// Parse the template
	tmpl, err := tmpl.Parse(promptTemplate)
	if err != nil {
		return nil, fmt.Errorf("failed to parse prompt template: %w", err)
	}

	return &PromptBuilder{
		template: tmpl,
	}, nil
}

// BuildPrompt generates a comprehensive prompt from the given context
// It handles missing or nil context fields gracefully through template conditionals
func (pb *PromptBuilder) BuildPrompt(ctx *PromptContext) (string, error) {
	if ctx == nil {
		return "", fmt.Errorf("prompt context cannot be nil")
	}
	if ctx.Issue == nil {
		return "", fmt.Errorf("issue cannot be nil in prompt context")
	}

	// Prepare the context data for template execution
	// We need to handle the sandbox interface{} type
	type sandboxData struct {
		Path          string
		GitBranch     string
		BeadsDB       string
		ModifiedFiles []string
	}

	// Create a wrapper struct that exposes sandbox fields if available
	templateData := struct {
		*PromptContext
		Sandbox *sandboxData
	}{
		PromptContext: ctx,
	}

	// Extract sandbox data if available
	// TODO: When sandbox package is implemented, use proper type assertion
	// For now, we handle it as generic interface
	if ctx.Sandbox != nil {
		// This will be replaced when sandbox.SandboxContext is available
		// For now, just set to nil to avoid panic
		templateData.Sandbox = nil
	}

	var buf bytes.Buffer
	if err := pb.template.Execute(&buf, templateData); err != nil {
		return "", fmt.Errorf("failed to execute prompt template: %w", err)
	}

	return buf.String(), nil
}

// formatTime formats a time value for display in prompts
func formatTime(t time.Time) string {
	return t.Format("2006-01-02 15:04")
}

// truncate truncates a string to the specified length, adding ellipsis if needed
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

// isNil checks if a pointer is nil
func isNil(v interface{}) bool {
	return v == nil
}

// deref dereferences a bool pointer, returning false if nil
func deref(v *bool) bool {
	if v == nil {
		return false
	}
	return *v
}

// derefInt dereferences an int pointer, returning 0 if nil
func derefInt(v *int) int {
	if v == nil {
		return 0
	}
	return *v
}
