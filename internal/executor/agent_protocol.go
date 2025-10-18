package executor

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/steveyegge/vc/internal/ai"
)

// AgentReportStatus represents the status type declared by an agent
type AgentReportStatus string

const (
	// AgentStatusCompleted indicates task was fully completed
	AgentStatusCompleted AgentReportStatus = "completed"

	// AgentStatusBlocked indicates agent cannot proceed due to blockers
	AgentStatusBlocked AgentReportStatus = "blocked"

	// AgentStatusPartial indicates some work done, specific items remain
	AgentStatusPartial AgentReportStatus = "partial"

	// AgentStatusDecomposed indicates task was too large, agent broke it into epic + children
	AgentStatusDecomposed AgentReportStatus = "decomposed"
)

// IsValid checks if the status value is valid
func (s AgentReportStatus) IsValid() bool {
	switch s {
	case AgentStatusCompleted, AgentStatusBlocked, AgentStatusPartial, AgentStatusDecomposed:
		return true
	}
	return false
}

// AgentReport represents the structured output from an agent
// Agents should output this JSON at the end of their execution
type AgentReport struct {
	Status  AgentReportStatus `json:"status"`
	Summary string            `json:"summary"`

	// For status: blocked
	Blockers []string `json:"blockers,omitempty"`

	// For status: partial
	Completed []string `json:"completed,omitempty"`
	Remaining []string `json:"remaining,omitempty"`

	// For status: decomposed
	Reasoning string           `json:"reasoning,omitempty"` // Why task was decomposed
	Epic      *EpicDefinition  `json:"epic,omitempty"`
	Children  []ChildIssue     `json:"children,omitempty"`

	// Optional metadata
	TestsAdded    bool     `json:"tests_added,omitempty"`
	FilesModified []string `json:"files_modified,omitempty"`
}

// EpicDefinition defines an epic to be created from decomposition
type EpicDefinition struct {
	Title       string `json:"title"`
	Description string `json:"description"`
}

// ChildIssue defines a child issue to be created
type ChildIssue struct {
	Title       string `json:"title"`
	Description string `json:"description"`
	Type        string `json:"type"`     // bug, task, feature, chore
	Priority    string `json:"priority"` // P0, P1, P2, P3
}

// Validate checks if the agent report is valid
func (r *AgentReport) Validate() error {
	if !r.Status.IsValid() {
		return fmt.Errorf("invalid status: %s", r.Status)
	}

	if r.Summary == "" {
		return fmt.Errorf("summary is required")
	}

	switch r.Status {
	case AgentStatusBlocked:
		if len(r.Blockers) == 0 {
			return fmt.Errorf("status=blocked requires blockers list")
		}

	case AgentStatusPartial:
		if len(r.Remaining) == 0 {
			return fmt.Errorf("status=partial requires remaining work list")
		}

	case AgentStatusDecomposed:
		if r.Epic == nil {
			return fmt.Errorf("status=decomposed requires epic definition")
		}
		if len(r.Children) == 0 {
			return fmt.Errorf("status=decomposed requires children list")
		}
		if r.Reasoning == "" {
			return fmt.Errorf("status=decomposed requires reasoning")
		}
		// Validate epic
		if r.Epic.Title == "" {
			return fmt.Errorf("epic title is required")
		}
		// Validate children
		for i, child := range r.Children {
			if child.Title == "" {
				return fmt.Errorf("child %d: title is required", i)
			}
			if child.Type == "" {
				return fmt.Errorf("child %d: type is required", i)
			}
			if child.Priority == "" {
				return fmt.Errorf("child %d: priority is required", i)
			}
		}
	}

	return nil
}

// ParseAgentReport attempts to extract and parse a structured agent report from agent output
// Returns the report and true if found, or nil and false if not found/invalid
// This is a best-effort parser that looks for JSON blocks in the output
func ParseAgentReport(agentOutput string) (*AgentReport, bool) {
	// Look for JSON blocks that might contain agent reports
	// Try multiple strategies to find the JSON

	// Strategy 1: Look for explicit markers (recommended format)
	// Expected: === AGENT REPORT ===\n{...}\n=== END AGENT REPORT ===
	if report := extractBetweenMarkers(agentOutput, "=== AGENT REPORT ===", "=== END AGENT REPORT ==="); report != nil {
		return report, true
	}

	// Strategy 2: Look for JSON code fence with "agent-report" or "json" label
	// Expected: ```agent-report\n{...}\n```  or  ```json\n{...}\n```
	if report := extractFromCodeFence(agentOutput, "agent-report"); report != nil {
		return report, true
	}
	if report := extractFromCodeFence(agentOutput, "json"); report != nil {
		return report, true
	}

	// Strategy 3: Look for JSON object with "status" field near the end of output
	// This handles cases where agent outputs raw JSON without markers
	if report := extractLastJSONWithStatus(agentOutput); report != nil {
		return report, true
	}

	// No valid agent report found
	return nil, false
}

// extractBetweenMarkers extracts and parses JSON between start and end markers
func extractBetweenMarkers(text, startMarker, endMarker string) *AgentReport {
	startIdx := strings.Index(text, startMarker)
	if startIdx == -1 {
		return nil
	}

	// Start searching for end marker after start marker
	searchStart := startIdx + len(startMarker)
	endIdx := strings.Index(text[searchStart:], endMarker)
	if endIdx == -1 {
		return nil
	}

	// Extract JSON (between markers)
	jsonStr := strings.TrimSpace(text[searchStart : searchStart+endIdx])

	// Try to parse
	var report AgentReport
	if err := json.Unmarshal([]byte(jsonStr), &report); err != nil {
		return nil
	}

	// Validate
	if err := report.Validate(); err != nil {
		return nil
	}

	return &report
}

// extractFromCodeFence extracts and parses JSON from markdown code fence
func extractFromCodeFence(text, language string) *AgentReport {
	// Look for ```language\n...\n```
	marker := "```" + language
	startIdx := strings.Index(text, marker)
	if startIdx == -1 {
		return nil
	}

	// Skip past the marker and newline
	jsonStart := startIdx + len(marker)
	// Find the next newline
	if jsonStart < len(text) && text[jsonStart] == '\n' {
		jsonStart++
	}

	// Find the closing ```
	endIdx := strings.Index(text[jsonStart:], "```")
	if endIdx == -1 {
		return nil
	}

	// Extract JSON
	jsonStr := strings.TrimSpace(text[jsonStart : jsonStart+endIdx])

	// Try to parse
	var report AgentReport
	if err := json.Unmarshal([]byte(jsonStr), &report); err != nil {
		return nil
	}

	// Validate
	if err := report.Validate(); err != nil {
		return nil
	}

	return &report
}

// extractLastJSONWithStatus finds the last JSON object containing a "status" field
func extractLastJSONWithStatus(text string) *AgentReport {
	// Look for the last occurrence of a JSON object with "status" field
	// This is a simple heuristic: find all { ... } blocks and try to parse them

	// Find all potential JSON objects (last 10KB of output to avoid parsing huge logs)
	const maxSearchSize = 10000
	searchText := text
	if len(text) > maxSearchSize {
		searchText = text[len(text)-maxSearchSize:]
	}

	// Find the last opening brace
	lastOpen := strings.LastIndex(searchText, "{")
	if lastOpen == -1 {
		return nil
	}

	// Find the matching closing brace
	// Simple approach: find the last closing brace after the opening brace
	remaining := searchText[lastOpen:]
	lastClose := strings.LastIndex(remaining, "}")
	if lastClose == -1 {
		return nil
	}

	// Extract potential JSON
	jsonStr := strings.TrimSpace(remaining[:lastClose+1])

	// Try to parse
	var report AgentReport
	if err := json.Unmarshal([]byte(jsonStr), &report); err != nil {
		return nil
	}

	// Must have a status field to be considered an agent report
	if report.Status == "" {
		return nil
	}

	// Validate
	if err := report.Validate(); err != nil {
		return nil
	}

	return &report
}

// ConvertToDiscoveredIssues converts child issues from agent report to AI DiscoveredIssue format
// This allows reusing the existing CreateDiscoveredIssues infrastructure
func ConvertToDiscoveredIssues(children []ChildIssue) []ai.DiscoveredIssue {
	discovered := make([]ai.DiscoveredIssue, len(children))
	for i, child := range children {
		discovered[i] = ai.DiscoveredIssue{
			Title:       child.Title,
			Description: child.Description,
			Type:        child.Type,
			Priority:    child.Priority,
		}
	}
	return discovered
}

// GetPromptInstructions returns the markdown instructions to include in agent prompts
// explaining the structured output protocol
func GetPromptInstructions() string {
	return `
# STRUCTURED OUTPUT PROTOCOL

At the end of your execution, you MUST output a structured status report in one of these formats:

## Format 1: Using Markers (Recommended)

=== AGENT REPORT ===
{
  "status": "completed",
  "summary": "Implemented feature X and added tests",
  "tests_added": true,
  "files_modified": ["file1.go", "file2.go"]
}
=== END AGENT REPORT ===

## Format 2: Using Code Fence

` + "```agent-report" + `
{
  "status": "completed",
  "summary": "Implemented feature X and added tests"
}
` + "```" + `

## Status Types

Choose ONE of these status types:

### 1. COMPLETED - Task fully done
{
  "status": "completed",
  "summary": "What was accomplished",
  "tests_added": true,  // optional
  "files_modified": ["list", "of", "files"]  // optional
}

### 2. BLOCKED - Cannot proceed
{
  "status": "blocked",
  "summary": "Brief summary of what was attempted",
  "blockers": [
    "Missing API key for service X",
    "Dependency Y is not installed",
    "Unclear requirement: how should edge case Z be handled?"
  ]
}

### 3. PARTIAL - Some work done, specific items remain
{
  "status": "partial",
  "summary": "What was accomplished so far",
  "completed": [
    "Implemented core data structure",
    "Added unit tests for happy path"
  ],
  "remaining": [
    "Add error handling for edge cases",
    "Add integration tests",
    "Update documentation"
  ]
}

### 4. DECOMPOSED - Task too large, broke into epic + children
{
  "status": "decomposed",
  "reasoning": "Task scope is too large for single execution. Breaking into 5 focused subtasks.",
  "summary": "Analyzed task and created implementation plan",
  "epic": {
    "title": "Original task as epic",
    "description": "Overall goal and context"
  },
  "children": [
    {
      "title": "Subtask 1: Implement core data structure",
      "description": "Create the User struct with validation",
      "type": "task",
      "priority": "P1"
    },
    {
      "title": "Subtask 2: Add database layer",
      "description": "Implement storage for User in SQLite",
      "type": "task",
      "priority": "P1"
    }
  ]
}

## When to Use Each Status

- **completed**: You finished all acceptance criteria
- **blocked**: You hit a technical blocker (missing dependency, unclear requirement, external service down)
- **partial**: You made progress but ran out of time or scope. Be SPECIFIC about what remains.
- **decomposed**: The task is too large/complex for one execution. Break it down autonomously.

## Important Notes

1. You MUST output this report - it's how the system knows your status
2. The JSON must be valid (use proper escaping for quotes in strings)
3. Be SPECIFIC in all lists - no vague items like "finish remaining work"
4. For "decomposed", aim for 3-8 children (focused, achievable subtasks)
5. The system will automatically create follow-on issues from your report
`
}
