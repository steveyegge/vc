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

⚠️ **CRITICAL**: Your job is to complete THIS SPECIFIC TASK ONLY. Do NOT work on related features, cleanup, or improvements unless explicitly mentioned in the acceptance criteria below.

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

**IMPORTANT**: These criteria define success. ALL criteria must be met. Do not add extra work beyond what's required.

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
{{if .IsBaselineIssue -}}
# BASELINE TEST FAILURE SELF-HEALING DIRECTIVE

**CRITICAL**: This is a baseline test failure. Your job is to FIX the failing test(s) to restore the baseline to a healthy state.

## Test Failure Analysis Framework

1. **Classify the Failure Type**:
   - **Flaky Test**: Passes sometimes, fails sometimes (race condition, timing, non-deterministic behavior)
   - **Real Failure**: Consistently fails due to actual bug in code
   - **Environmental**: External dependency issue (missing file, network, etc.)

2. **For Flaky Tests**:
   - Investigate race conditions (shared state, goroutines, channels)
   - Check for timing dependencies (hardcoded sleeps, timeouts)
   - Look for non-deterministic inputs (randomness, time-based logic, map iteration)
   - Fix by: adding synchronization, increasing timeouts, removing non-determinism
   - Verify: Run test 10+ times to ensure stability

3. **For Real Failures**:
   - Trace through the code to understand root cause
   - Identify what changed to break the test
   - Apply minimal fix to restore functionality
   - Verify: Ensure test passes and other tests still pass

4. **For Environmental Failures**:
   - Check for missing dependencies, files, or configuration
   - Verify external services are available
   - Fix by: adding setup steps, mocking external dependencies
   - Document any environment requirements

## Fix Verification Protocol

After applying a fix:
1. **Run the specific failing test(s)** to verify they pass
2. **Run the full test suite** to ensure no regressions
3. **For flaky tests**: Run the test 10+ times to verify stability
4. **Document your fix** with clear reasoning in commit message

## Commit Message Format

Use this format for test fix commits:

` + "```" + `
Fix: [test-name] - [brief-description]

Failure Type: [Flaky|Real|Environmental]
Root Cause: [explanation]
Fix Applied: [what you changed]
Verification: [how you verified it works]
` + "```" + `

## Rules for Baseline Test Fixes

1. **Minimize changes** - Only fix what's broken, don't refactor
2. **Preserve test intent** - Don't change what the test is testing
3. **Add comments** - Explain non-obvious fixes (especially for flaky tests)
4. **Verify thoroughly** - Don't commit until tests are stable
5. **Report blockers** - If test failure indicates a real bug in code, report it

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
{{end}}

---

# STRUCTURED OUTPUT PROTOCOL

**CRITICAL**: At the end of your execution, you MUST output a structured status report using this format:

## Output Format

Place your report between these markers:

=== AGENT REPORT ===
{
  "status": "completed|blocked|partial|decomposed",
  "summary": "Brief description of what happened"
}
=== END AGENT REPORT ===

## Status Types

### 1. COMPLETED - Task fully done
` + "```" + `json
{
  "status": "completed",
  "summary": "Implemented feature X, added tests, all acceptance criteria met",
  "tests_added": true,
  "files_modified": ["src/feature.go", "src/feature_test.go"]
}
` + "```" + `

### 2. BLOCKED - Cannot proceed due to technical blocker
` + "```" + `json
{
  "status": "blocked",
  "summary": "Attempted to implement API integration but hit blockers",
  "blockers": [
    "Missing API key - ANTHROPIC_API_KEY not set in environment",
    "Service endpoint unclear - documentation doesn't specify production URL"
  ]
}
` + "```" + `

### 3. PARTIAL - Some work done, specific items remain
` + "```" + `json
{
  "status": "partial",
  "summary": "Implemented core functionality, tests pending",
  "completed": [
    "Created data structures and validation logic",
    "Added database migration",
    "Implemented basic CRUD operations"
  ],
  "remaining": [
    "Add unit tests for edge cases",
    "Add integration tests with database",
    "Update API documentation"
  ]
}
` + "```" + `

### 4. DECOMPOSED - Task too large, broke into smaller pieces
` + "```" + `json
{
  "status": "decomposed",
  "reasoning": "Task scope too large - implementing full user management requires 6 distinct subtasks",
  "summary": "Analyzed requirements and created breakdown",
  "epic": {
    "title": "User Management System",
    "description": "Complete user authentication and authorization system"
  },
  "children": [
    {
      "title": "Implement User data model",
      "description": "Create User struct with validation, database schema",
      "type": "task",
      "priority": "P1"
    },
    {
      "title": "Add authentication endpoints",
      "description": "Login, logout, token validation APIs",
      "type": "task",
      "priority": "P1"
    },
    {
      "title": "Implement authorization middleware",
      "description": "Role-based access control for API endpoints",
      "type": "task",
      "priority": "P1"
    }
  ]
}
` + "```" + `

## When to Use Each Status

- **completed**: All acceptance criteria met, task is 100% done
- **blocked**: Hit a technical blocker (missing dependency, API key, external service issue)
- **partial**: Made significant progress but specific work remains (be DETAILED about what's left)
- **decomposed**: Task is too large/complex - you're breaking it down autonomously into an epic with children

## Rules

1. **ALWAYS output a report** - The system parses this to determine next steps
2. **Use valid JSON** - Escape quotes in strings, no trailing commas
3. **Be SPECIFIC** - Don't say "add tests", say "add unit tests for UserAuth.validateToken edge cases"
4. **Choose the right status** - Don't use "completed" if work remains
5. **For decomposed**: Create 3-8 focused children, each should be completable in one execution

The system will automatically:
- Create follow-on issues from your lists (blocked, partial, decomposed)
- Convert original task to epic if you use "decomposed"
- Close the issue if you report "completed" and tests pass`

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

	// Detect baseline issues (vc-210: Self-healing for baseline test failures)
	// vc-261: Use IsBaselineIssue() helper instead of duplicated map
	isBaselineIssue := IsBaselineIssue(ctx.Issue.ID)

	// Create a wrapper struct that exposes sandbox fields if available
	templateData := struct {
		*PromptContext
		Sandbox         *sandboxData
		IsBaselineIssue bool
	}{
		PromptContext:   ctx,
		IsBaselineIssue: isBaselineIssue,
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
