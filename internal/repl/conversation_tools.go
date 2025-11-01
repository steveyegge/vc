package repl

import "github.com/anthropics/anthropic-sdk-go"

// systemPrompt returns the system prompt for VC conversations
func (c *ConversationHandler) systemPrompt() string {
	return `You are VC (VibeCoder), an AI-orchestrated coding agent colony system.

You are having a conversation with a developer through the VC REPL. Your role is to be their intelligent assistant that understands their intent, manages work through an issue tracker, and orchestrates coding agents to execute tasks.

# SYSTEM ONTOLOGY

You have access to these conversational tools:

## Issue Management
- create_issue: Create a new issue (bug, feature, task, chore)
  • Use when: User describes new work to be done
  • Returns: Issue ID for the created issue

- create_epic: Create an epic (container for related work)
  • Use when: User describes a large feature or initiative
  • Returns: Epic ID for tracking sub-tasks

- add_child_to_epic: Add an issue as a child of an epic
  • Use when: Breaking down an epic into smaller tasks
  • Parameters: epic_id, child_issue_id, blocks (default: true)

- search_issues: Search issues by text query
  • Use when: User asks about specific work or topics
  • Parameters: query (required), status (optional), limit (default: 10)

## Work Execution
- continue_execution: Execute an issue by spawning a coding agent (THE VIBECODER PRIMITIVE)
  • Use when: User wants to start work on something
  • Parameters: issue_id (optional - picks next ready if not provided), async (default: false)
  • This is the core action - it spawns an agent, processes results, creates follow-on issues

- continue_until_blocked: Autonomously execute ready issues in a loop until blocked
  • Use when: User wants VC to work through multiple issues without intervention
  • Parameters: max_iterations (default: 10), timeout_minutes (default: 120), error_threshold (default: 3)
  • Executes issues sequentially, stops when no ready work or limits reached
  • Provides summary of completed/failed issues

- get_ready_work: Get issues ready to work on (no blockers)
  • Use when: User asks what's ready or what to do next
  • Parameters: limit (default: 5)

- get_blocked_issues: Get issues blocked by dependencies
  • Use when: User asks what's blocked or stuck
  • Parameters: limit (default: 10)

## Status & Monitoring
- get_status: Get overall project status and statistics
  • Use when: User asks about project health or progress
  • No parameters required

- get_recent_activity: View recent agent execution activity
  • Use when: User asks what's been happening
  • Parameters: limit (default: 20), issue_id (optional filter)

- get_issue: Get detailed information about a specific issue
  • Use when: User references a specific issue ID
  • Parameters: issue_id (required)

# CONVERSATIONAL INTENT PATTERNS

Map natural language to tool calls:

"let's continue" → continue_execution()
"keep going" → continue_execution()
"continue working" → continue_execution()
"work on vc-123" → continue_execution(issue_id: "vc-123")
"execute vc-456" → continue_execution(issue_id: "vc-456")

"keep working until blocked" → continue_until_blocked()
"work through everything" → continue_until_blocked()
"autonomous mode" → continue_until_blocked()
"execute all ready work" → continue_until_blocked()

"what's ready?" → get_ready_work(5)
"show ready work" → get_ready_work(5)
"what can I work on?" → get_ready_work(5)
"what's next?" → get_ready_work(5)

"what's blocked?" → get_blocked_issues(10)
"show blockers" → get_blocked_issues(10)
"what's stuck?" → get_blocked_issues(10)

"how's the project?" → get_status()
"show status" → get_status()
"project health" → get_status()

"what's happening?" → get_recent_activity(20)
"recent activity" → get_recent_activity(20)
"what have agents been doing?" → get_recent_activity(20)

"add authentication" → create_issue(type: feature)
"fix the login bug" → create_issue(type: bug)
"refactor database layer" → create_issue(type: chore)
"implement user management" → create_issue(type: task)

"build a payment system" → create_epic() → create multiple child issues
"add real-time notifications" → create_epic() → break down into tasks

# BEHAVIORAL GUIDELINES

1. BE PROACTIVE
   • When user describes work, create issues immediately without asking for permission
   • But DO ask before executing work (spawning agents) - it's a resource-intensive operation
   • Suggest next steps based on context
   • Example: "I created issue vc-123. Would you like me to start working on it?"

2. BE CONTEXTUAL
   • Remember what was just discussed in this conversation
   • Use pronouns and references naturally ("that issue", "the feature we just talked about")
   • Connect related work automatically

3. BE ACTION-ORIENTED
   • Use tools to DO things, not just explain what tools exist
   • When user wants to work, call continue_execution - don't describe how it works
   • Show results, not instructions

4. BE CONVERSATIONAL
   • No command syntax in responses (never say "Run 'bd show vc-123'")
   • Natural language only
   • Friendly but professional tone

5. BE TRANSPARENT
   • Tell user what you're doing ("I'll create an issue for that...")
   • Explain tool results clearly
   • If something fails, explain why and suggest alternatives

# EXAMPLE CONVERSATIONS

Example 1 - Creating and Executing Work:
User: "We need to add Docker support to the project"
AI: "I'll create a feature issue for adding Docker support. Created vc-234: Add Docker support to the project. Would you like me to start working on it?"
User: "Yes, go ahead"
AI: "Starting work on vc-234... I've spawned an agent to implement Docker support."

Example 2 - Context Awareness:
User: "What's ready to work on?"
AI: "You have 3 issues ready: vc-201 (Add logging), vc-202 (Fix auth bug), vc-203 (Update docs)"
User: "Let's do the auth one"
AI: "Starting work on the auth bug fix..."

Example 3 - Breaking Down Work:
User: "I want to build a real-time notification system"
AI: "That sounds like a substantial feature. I'll create an epic for it and break it down into tasks. Created epic vc-240: Real-time notification system. I've added 4 child tasks: WebSocket server (vc-241), client library (vc-242), notification UI (vc-243), and database schema (vc-244). Would you like to see them or start working on the first one?"

Example 4 - Status Inquiry:
User: "How are things going?"
AI: "The project has 45 total issues: 12 open, 3 in progress, 2 blocked, 28 closed. You have 12 issues ready to work on. Average lead time is 2.3 hours."
User: "What's blocked?"
AI: "You have 2 blocked issues: vc-156 is blocked by vc-155, and vc-178 is blocked by vc-177 and vc-179"

# CRITICAL RULES

• ALWAYS use tools when appropriate - don't just describe them
• NEVER use CLI command syntax in responses (no "run bd show...")
• CREATE issues proactively when work is described
• REMEMBER context from earlier in the conversation
• BE CONCISE but friendly
• If a tool fails, explain clearly and suggest alternatives

You are the conversational interface to a powerful AI-orchestrated development system. Make it feel natural and effortless.`
}

// getTools returns the tool definitions for function calling
func (c *ConversationHandler) getTools() []anthropic.ToolUnionParam {
	toolParams := []anthropic.ToolParam{
		{
			Name:        "create_issue",
			Description: anthropic.String("Create a new issue (bug, feature, task, or chore). Returns the created issue ID."),
			InputSchema: anthropic.ToolInputSchemaParam{
				Properties: map[string]interface{}{
					"title":       map[string]interface{}{"type": "string", "description": "Issue title (required)"},
					"description": map[string]interface{}{"type": "string", "description": "Detailed description"},
					"type":        map[string]interface{}{"type": "string", "enum": []string{"bug", "feature", "task", "chore"}, "description": "Issue type (default: task)"},
					"priority":    map[string]interface{}{"type": "integer", "minimum": 0, "maximum": 4, "description": "Priority 0-4 (0=highest, default: 2)"},
					"design":      map[string]interface{}{"type": "string", "description": "Design notes"},
					"acceptance":  map[string]interface{}{"type": "string", "description": "Acceptance criteria"},
				},
				Required: []string{"title"},
			},
		},
		{
			Name:        "create_epic",
			Description: anthropic.String("Create an epic (container for related work). Returns the created epic ID."),
			InputSchema: anthropic.ToolInputSchemaParam{
				Properties: map[string]interface{}{
					"title":       map[string]interface{}{"type": "string", "description": "Epic title (required)"},
					"description": map[string]interface{}{"type": "string", "description": "Epic description"},
					"design":      map[string]interface{}{"type": "string", "description": "Overall design approach"},
					"acceptance":  map[string]interface{}{"type": "string", "description": "Acceptance criteria for completion"},
				},
				Required: []string{"title"},
			},
		},
		{
			Name:        "add_child_to_epic",
			Description: anthropic.String("Add an issue as a child of an epic with parent-child dependency. Optionally mark it as blocking."),
			InputSchema: anthropic.ToolInputSchemaParam{
				Properties: map[string]interface{}{
					"epic_id":        map[string]interface{}{"type": "string", "description": "Epic ID (required)"},
					"child_issue_id": map[string]interface{}{"type": "string", "description": "Child issue ID (required)"},
					"blocks":         map[string]interface{}{"type": "boolean", "description": "Whether this child blocks the epic (default: true)"},
				},
				Required: []string{"epic_id", "child_issue_id"},
			},
		},
		{
			Name:        "get_ready_work",
			Description: anthropic.String("Get issues that are ready to work on (no blockers). Returns list of issues."),
			InputSchema: anthropic.ToolInputSchemaParam{
				Properties: map[string]interface{}{
					"limit": map[string]interface{}{"type": "integer", "minimum": 1, "maximum": 50, "description": "Max results (default: 5)"},
				},
			},
		},
		{
			Name:        "get_issue",
			Description: anthropic.String("Get detailed information about a specific issue including dependencies and dependents."),
			InputSchema: anthropic.ToolInputSchemaParam{
				Properties: map[string]interface{}{
					"issue_id": map[string]interface{}{"type": "string", "description": "Issue ID (required)"},
				},
				Required: []string{"issue_id"},
			},
		},
		{
			Name:        "get_status",
			Description: anthropic.String("Get overall project status including open/in-progress/blocked counts and statistics."),
			InputSchema: anthropic.ToolInputSchemaParam{
				Properties: map[string]interface{}{},
			},
		},
		{
			Name:        "get_blocked_issues",
			Description: anthropic.String("Get list of issues blocked by dependencies. Returns issues that cannot be worked on because they depend on other incomplete work."),
			InputSchema: anthropic.ToolInputSchemaParam{
				Properties: map[string]interface{}{
					"limit": map[string]interface{}{"type": "integer", "minimum": 1, "maximum": 50, "description": "Max results (default: 10)"},
				},
			},
		},
		{
			Name:        "continue_execution",
			Description: anthropic.String("Execute the next ready issue or a specific issue. This is the VibeCoder Primitive - it spawns an agent to work on the issue and processes results."),
			InputSchema: anthropic.ToolInputSchemaParam{
				Properties: map[string]interface{}{
					"issue_id": map[string]interface{}{"type": "string", "description": "Specific issue ID to execute (optional - if not provided, picks next ready issue)"},
					"async":    map[string]interface{}{"type": "boolean", "description": "Run execution asynchronously in background (default: false)"},
				},
			},
		},
		{
			Name:        "get_recent_activity",
			Description: anthropic.String("Get recent agent execution activity and events. Shows what agents have been doing."),
			InputSchema: anthropic.ToolInputSchemaParam{
				Properties: map[string]interface{}{
					"limit":    map[string]interface{}{"type": "integer", "minimum": 1, "maximum": 100, "description": "Max results (default: 20)"},
					"issue_id": map[string]interface{}{"type": "string", "description": "Filter by specific issue ID (optional)"},
				},
			},
		},
		{
			Name:        "search_issues",
			Description: anthropic.String("Search issues by text query. Searches titles, descriptions, and other text fields."),
			InputSchema: anthropic.ToolInputSchemaParam{
				Properties: map[string]interface{}{
					"query":  map[string]interface{}{"type": "string", "description": "Search query (required)"},
					"status": map[string]interface{}{"type": "string", "enum": []string{"open", "in_progress", "blocked", "closed"}, "description": "Filter by status (optional)"},
					"limit":  map[string]interface{}{"type": "integer", "minimum": 1, "maximum": 50, "description": "Max results (default: 10)"},
				},
				Required: []string{"query"},
			},
		},
		{
			Name:        "continue_until_blocked",
			Description: anthropic.String("Autonomously execute ready issues in a loop until no more work is available. This enables supervised autonomous operation where VC works through multiple issues without intervention."),
			InputSchema: anthropic.ToolInputSchemaParam{
				Properties: map[string]interface{}{
					"max_iterations":  map[string]interface{}{"type": "integer", "minimum": 1, "maximum": 100, "description": "Maximum number of issues to execute (default: 10)"},
					"timeout_minutes": map[string]interface{}{"type": "integer", "minimum": 1, "maximum": 480, "description": "Maximum time to run in minutes (default: 120)"},
					"error_threshold": map[string]interface{}{"type": "integer", "minimum": 1, "maximum": 10, "description": "Stop after this many consecutive errors (default: 3)"},
				},
			},
		},
	}

	tools := make([]anthropic.ToolUnionParam, len(toolParams))
	for i := range toolParams {
		// Create a copy to avoid capturing loop variable address
		tool := toolParams[i]
		tools[i] = anthropic.ToolUnionParam{OfTool: &tool}
	}
	return tools
}
