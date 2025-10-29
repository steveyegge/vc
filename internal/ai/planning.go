package ai

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/steveyegge/vc/internal/types"
)

// GeneratePlan generates a mission plan from the planning context
func (s *Supervisor) GeneratePlan(ctx context.Context, planningCtx *types.PlanningContext) (*types.MissionPlan, error) {
	// Add overall timeout to prevent indefinite retries
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()

	startTime := time.Now()

	// Validate input
	if err := planningCtx.Validate(); err != nil {
		return nil, fmt.Errorf("invalid planning context: %w", err)
	}

	// Build the planning prompt
	prompt := s.buildPlanningPrompt(planningCtx)

	// Call Anthropic API with retry logic
	var response *anthropic.Message
	err := s.retryWithBackoff(ctx, "planning", func(attemptCtx context.Context) error {
		resp, apiErr := s.client.Messages.New(attemptCtx, anthropic.MessageNewParams{
			Model:     anthropic.Model(s.model),
			MaxTokens: 8192, // Larger token limit for complex plans
			Messages: []anthropic.MessageParam{
				anthropic.NewUserMessage(anthropic.NewTextBlock(prompt)),
			},
		})
		if apiErr != nil {
			return apiErr
		}
		response = resp
		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("anthropic API call failed: %w", err)
	}

	// Extract the text content from the response
	var responseText string
	for _, block := range response.Content {
		if block.Type == "text" {
			responseText += block.Text
		}
	}

	// Parse the response as JSON using resilient parser
	parseResult := Parse[types.MissionPlan](responseText, ParseOptions{
		Context:   "mission plan response",
		LogErrors: boolPtr(true),
	})
	if !parseResult.Success {
		// Log full response for debugging, but truncate in error message
		fmt.Fprintf(os.Stderr, "Full AI planning response: %s\n", responseText)
		return nil, fmt.Errorf("failed to parse mission plan response: %s (response: %s)", parseResult.Error, truncateString(responseText, 500))
	}
	plan := parseResult.Data

	// Set metadata
	plan.MissionID = planningCtx.Mission.ID
	plan.GeneratedAt = time.Now()
	plan.GeneratedBy = "ai-planner"

	// Validate the generated plan
	if err := s.ValidatePlan(ctx, &plan); err != nil {
		return nil, fmt.Errorf("generated plan failed validation: %w", err)
	}

	// Log the plan generation
	duration := time.Since(startTime)
	fmt.Printf("AI Planning for %s: phases=%d, confidence=%.2f, effort=%s, duration=%v\n",
		planningCtx.Mission.ID, len(plan.Phases), plan.Confidence, plan.EstimatedEffort, duration)

	// Log AI usage to events
	if err := s.logAIUsage(ctx, planningCtx.Mission.ID, "planning", response.Usage.InputTokens, response.Usage.OutputTokens, duration); err != nil {
		fmt.Fprintf(os.Stderr, "warning: failed to log AI usage: %v\n", err)
	}

	return &plan, nil
}

// RefinePhase breaks a phase down into granular tasks
// This is called when a phase is ready to execute
func (s *Supervisor) RefinePhase(ctx context.Context, phase *types.Phase, missionCtx *types.PlanningContext) ([]types.PlannedTask, error) {
	// Add overall timeout to prevent indefinite retries
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()

	startTime := time.Now()

	// Validate inputs
	if phase == nil {
		return nil, fmt.Errorf("phase is required")
	}
	if err := phase.Validate(); err != nil {
		return nil, fmt.Errorf("invalid phase: %w", err)
	}
	if missionCtx != nil {
		if err := missionCtx.Validate(); err != nil {
			return nil, fmt.Errorf("invalid mission context: %w", err)
		}
	}

	// Build the refinement prompt
	prompt := s.buildRefinementPrompt(phase, missionCtx)

	// Call Anthropic API with retry logic
	var response *anthropic.Message
	err := s.retryWithBackoff(ctx, "refinement", func(attemptCtx context.Context) error {
		resp, apiErr := s.client.Messages.New(attemptCtx, anthropic.MessageNewParams{
			Model:     anthropic.Model(s.model),
			MaxTokens: 8192,
			Messages: []anthropic.MessageParam{
				anthropic.NewUserMessage(anthropic.NewTextBlock(prompt)),
			},
		})
		if apiErr != nil {
			return apiErr
		}
		response = resp
		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("anthropic API call failed: %w", err)
	}

	// Extract the text content from the response
	var responseText string
	for _, block := range response.Content {
		if block.Type == "text" {
			responseText += block.Text
		}
	}

	// Parse the response - expecting {"tasks": [...]}
	type refinementResponse struct {
		Tasks []types.PlannedTask `json:"tasks"`
	}
	parseResult := Parse[refinementResponse](responseText, ParseOptions{
		Context:   "phase refinement response",
		LogErrors: boolPtr(true),
	})
	if !parseResult.Success {
		// Log full response for debugging, but truncate in error message
		fmt.Fprintf(os.Stderr, "Full AI refinement response: %s\n", responseText)
		return nil, fmt.Errorf("failed to parse refinement response: %s (response: %s)", parseResult.Error, truncateString(responseText, 500))
	}
	tasks := parseResult.Data.Tasks

	// Validate tasks
	if len(tasks) == 0 {
		return nil, fmt.Errorf("refinement produced no tasks")
	}
	for i, task := range tasks {
		if err := task.Validate(); err != nil {
			return nil, fmt.Errorf("task %d invalid: %w", i+1, err)
		}
	}

	// Log the refinement
	duration := time.Since(startTime)
	fmt.Printf("AI Refinement for phase %s: tasks=%d, duration=%v\n",
		phase.ID, len(tasks), duration)

	// Log AI usage
	if err := s.logAIUsage(ctx, phase.ID, "refinement", response.Usage.InputTokens, response.Usage.OutputTokens, duration); err != nil {
		fmt.Fprintf(os.Stderr, "warning: failed to log AI usage: %v\n", err)
	}

	return tasks, nil
}

// ValidatePlan checks if a generated plan is valid and executable
func (s *Supervisor) ValidatePlan(ctx context.Context, plan *types.MissionPlan) error {
	// Basic validation already done by types.MissionPlan.Validate()
	if err := plan.Validate(); err != nil {
		return err
	}

	// Additional validation rules
	phaseCount := len(plan.Phases)
	if phaseCount < 1 {
		return fmt.Errorf("plan must have at least 1 phase (got %d)", phaseCount)
	}
	if phaseCount > 15 {
		return fmt.Errorf("plan has too many phases (%d); consider breaking into multiple missions", phaseCount)
	}

	// Check for circular dependencies
	if err := checkCircularDependencies(plan.Phases); err != nil {
		return fmt.Errorf("circular dependencies detected: %w", err)
	}

	// Validate each phase has reasonable task count
	for i, phase := range plan.Phases {
		taskCount := len(phase.Tasks)
		if taskCount == 0 {
			return fmt.Errorf("phase %d (%s) has no tasks", i+1, phase.Title)
		}
		if taskCount > 50 {
			return fmt.Errorf("phase %d (%s) has too many tasks (%d); break it down further", i+1, phase.Title, taskCount)
		}
	}

	return nil
}

// ValidatePhaseStructure validates phase dependencies and ordering using AI
// This replaces hardcoded validation rules (like "phases can only depend on earlier phases")
// with AI-driven validation that can be more flexible and context-aware
func (s *Supervisor) ValidatePhaseStructure(ctx context.Context, phases []types.PlannedPhase) error {
	startTime := time.Now()

	// For very simple cases (single phase, no dependencies), skip AI validation
	if len(phases) == 1 {
		return nil
	}

	// Build validation prompt
	prompt := s.buildPhaseValidationPrompt(phases)

	// Call Anthropic API with retry logic
	var response *anthropic.Message
	err := s.retryWithBackoff(ctx, "phase-validation", func(attemptCtx context.Context) error {
		resp, apiErr := s.client.Messages.New(attemptCtx, anthropic.MessageNewParams{
			Model:     anthropic.Model(s.model),
			MaxTokens: 2048,
			Messages: []anthropic.MessageParam{
				anthropic.NewUserMessage(anthropic.NewTextBlock(prompt)),
			},
		})
		if apiErr != nil {
			return apiErr
		}
		response = resp
		return nil
	})

	if err != nil {
		return fmt.Errorf("anthropic API call failed: %w", err)
	}

	// Extract the text content from the response
	var responseText string
	for _, block := range response.Content {
		if block.Type == "text" {
			responseText += block.Text
		}
	}

	// Parse the response
	type validationResult struct {
		Valid     bool     `json:"valid"`
		Errors    []string `json:"errors"`
		Warnings  []string `json:"warnings"`
		Reasoning string   `json:"reasoning"`
	}

	parseResult := Parse[validationResult](responseText, ParseOptions{
		Context:   "phase validation response",
		LogErrors: boolPtr(true),
	})
	if !parseResult.Success {
		// vc-227: Truncate AI response to prevent log spam
		return fmt.Errorf("failed to parse phase validation response: %s (response: %s)", parseResult.Error, truncateString(responseText, 200))
	}
	result := parseResult.Data

	// Log the validation
	duration := time.Since(startTime)
	fmt.Printf("AI Phase Validation: valid=%v, errors=%d, warnings=%d, duration=%v\n",
		result.Valid, len(result.Errors), len(result.Warnings), duration)

	// Log AI usage (use a dummy issue ID for now since we don't have one in this context)
	if err := s.logAIUsage(ctx, "phase-validation", "phase-validation", response.Usage.InputTokens, response.Usage.OutputTokens, duration); err != nil {
		fmt.Fprintf(os.Stderr, "warning: failed to log AI usage: %v\n", err)
	}

	// If invalid, return the errors
	if !result.Valid {
		return fmt.Errorf("phase structure validation failed: %s (errors: %v)", result.Reasoning, result.Errors)
	}

	// Log warnings if any
	for _, warning := range result.Warnings {
		fmt.Printf("Phase validation warning: %s\n", warning)
	}

	return nil
}

// buildPhaseValidationPrompt builds the prompt for validating phase structure
func (s *Supervisor) buildPhaseValidationPrompt(phases []types.PlannedPhase) string {
	// Build phase summary
	var phaseSummary strings.Builder
	for _, phase := range phases {
		phaseSummary.WriteString(fmt.Sprintf("\nPhase %d: %s\n", phase.PhaseNumber, phase.Title))
		phaseSummary.WriteString(fmt.Sprintf("  Description: %s\n", phase.Description))
		phaseSummary.WriteString(fmt.Sprintf("  Dependencies: %v\n", phase.Dependencies))
		phaseSummary.WriteString(fmt.Sprintf("  Estimated Effort: %s\n", phase.EstimatedEffort))
	}

	return fmt.Sprintf(`You are validating the structure and dependencies of a multi-phase implementation plan.

PHASES TO VALIDATE:
%s

VALIDATION TASK:
Check if this phase structure makes logical sense. Consider:

1. **Dependency Validity**: Are dependencies sensible?
   - Typically phases depend on earlier phases, but forward dependencies MAY be valid in special cases
   - Example: Phase 3 depending on Phase 5 might be valid if Phase 5 is foundational infrastructure

2. **Circular Dependencies**: Are there any circular dependency chains?
   - Phase A → Phase B → Phase A is always invalid

3. **Missing Dependencies**: Are there obvious missing dependencies?
   - If Phase 3 builds on Phase 2's work, it should depend on Phase 2

4. **Logical Ordering**: Does the phase sequence make sense?
   - Foundation before features
   - Core before polish
   - Setup before execution

IMPORTANT: Be flexible. Not all plans follow strict "earlier phases only" rules. Consider the context.

Provide your validation as a JSON object:
{
  "valid": true/false,
  "errors": ["Critical error 1", "Critical error 2"],
  "warnings": ["Concern 1", "Concern 2"],
  "reasoning": "Detailed explanation of the assessment"
}

Guidelines:
- errors: Critical issues that MUST be fixed (invalid structure, circular deps)
- warnings: Concerns that should be reviewed but might be intentional
- reasoning: Explain your assessment clearly
- Be pragmatic: unusual structures might be valid if there's good reason

IMPORTANT: Respond with ONLY raw JSON. Do NOT wrap it in markdown code fences (`+"```"+`). Just the JSON object.`,
		phaseSummary.String())
}

// buildPlanningPrompt builds the prompt for generating a mission plan
func (s *Supervisor) buildPlanningPrompt(ctx *types.PlanningContext) string {
	mission := ctx.Mission

	// Build constraints section
	constraintsSection := ""
	if len(ctx.Constraints) > 0 {
		constraintsSection = "\n\nConstraints:\n"
		for _, constraint := range ctx.Constraints {
			constraintsSection += fmt.Sprintf("- %s\n", constraint)
		}
	}

	// Build context section
	contextSection := ""
	if ctx.CodebaseInfo != "" {
		contextSection = fmt.Sprintf("\n\nCodebase Context:\n%s", ctx.CodebaseInfo)
	}

	// Build failed attempts section
	failedAttemptsSection := ""
	if ctx.FailedAttempts > 0 {
		failedAttemptsSection = fmt.Sprintf("\n\nNote: This is attempt %d at planning. Previous plans had issues. Please try a different approach.", ctx.FailedAttempts+1)
	}

	return fmt.Sprintf(`You are an AI mission planner helping break down a large software development mission into executable phases.

MISSION OVERVIEW:
Mission ID: %s
Title: %s
Goal: %s

Description:
%s

Context:
%s%s%s%s

THREE-TIER WORKFLOW:
This system uses a three-tier workflow:
1. OUTER LOOP (Mission): High-level goal (what you're planning now)
2. MIDDLE LOOP (Phases): Implementation stages (what you'll generate)
3. INNER LOOP (Tasks): Granular work items (generated later when each phase executes)

YOUR TASK:
Generate a phased implementation plan. Each phase should be:
- A major milestone that takes 1-2 weeks to complete
- Focused on a specific aspect or stage of the work
- Independently valuable (produces working functionality)
- Ordered logically with clear dependencies

GENERATE A JSON PLAN WITH THIS STRUCTURE:
{
  "phases": [
    {
      "phase_number": 1,
      "title": "Phase 1: Foundation",
      "description": "Detailed description of what this phase accomplishes",
      "strategy": "High-level approach for this phase",
      "tasks": [
        "High-level task 1 (will be refined later into granular tasks)",
        "High-level task 2",
        "High-level task 3"
      ],
      "dependencies": [],
      "estimated_effort": "1 week"
    },
    {
      "phase_number": 2,
      "title": "Phase 2: Core Features",
      "description": "...",
      "strategy": "...",
      "tasks": ["..."],
      "dependencies": [1],
      "estimated_effort": "2 weeks"
    }
  ],
  "strategy": "Overall implementation strategy across all phases",
  "risks": [
    "Potential risk or challenge 1",
    "Potential risk or challenge 2"
  ],
  "estimated_effort": "6 weeks",
  "confidence": 0.85
}

IMPORTANT GUIDELINES:
- Generate 2-10 phases (prefer fewer, larger phases over many tiny ones)
- Phase numbers start at 1 and must be sequential
- Dependencies array contains phase numbers (must be earlier phases only)
- Each phase should have 3-8 high-level tasks
- Tasks are high-level descriptions, NOT granular implementation steps
- Estimated effort should be realistic: "3 days", "1 week", "2 weeks"
- Confidence should reflect uncertainty (0.0-1.0)
- Consider technical dependencies, logical ordering, and risk

IMPORTANT: Respond with ONLY raw JSON. Do NOT wrap it in markdown code fences (`+"```"+`). Just the JSON object.`,
		mission.ID, mission.Title, mission.Goal,
		mission.Description,
		mission.Context,
		contextSection,
		constraintsSection,
		failedAttemptsSection)
}

// buildRefinementPrompt builds the prompt for refining a phase into tasks
func (s *Supervisor) buildRefinementPrompt(phase *types.Phase, missionCtx *types.PlanningContext) string {
	// Build mission context if available
	missionSection := ""
	if missionCtx != nil && missionCtx.Mission != nil {
		missionSection = fmt.Sprintf(`
MISSION CONTEXT:
Mission: %s
Goal: %s
`, missionCtx.Mission.Title, missionCtx.Mission.Goal)
	}

	return fmt.Sprintf(`You are refining a phase of a software development mission into granular, executable tasks.

%s
PHASE TO REFINE:
Phase: %s
Strategy: %s

Description:
%s

YOUR TASK:
Break this phase down into 5-20 granular tasks. Each task should be:
- Small enough to complete in 30 minutes to 2 hours
- Concrete and actionable (not vague)
- Testable with clear acceptance criteria
- Ordered logically

GENERATE A JSON RESPONSE WITH THIS STRUCTURE:
{
  "tasks": [
    {
      "title": "Implement X data structure",
      "description": "Detailed description of what needs to be done",
      "acceptance_criteria": "Specific criteria for completion",
      "dependencies": [],
      "estimated_minutes": 60,
      "priority": 0,
      "type": "task"
    },
    {
      "title": "Add unit tests for X",
      "description": "...",
      "acceptance_criteria": "All tests pass, coverage > 80%%",
      "dependencies": ["Implement X data structure"],
      "estimated_minutes": 45,
      "priority": 1,
      "type": "task"
    }
  ]
}

GUIDELINES:
- Dependencies array contains task TITLES (not IDs) of tasks in this same list
- Priority: 0=P0 (critical), 1=P1 (high), 2=P2 (medium), 3=P3 (low)
- Type: "task", "bug", "feature", "chore"
- Estimated minutes should be realistic (15-120 minutes typical)
- Acceptance criteria should be specific and measurable
- Include tests as separate tasks
- Order tasks logically (dependencies should reference earlier tasks)

IMPORTANT: Respond with ONLY raw JSON. Do NOT wrap it in markdown code fences (`+"```"+`). Just the JSON object.`,
		missionSection,
		phase.Title,
		phase.Strategy,
		phase.Description)
}

// checkCircularDependencies detects circular dependencies in phases
func checkCircularDependencies(phases []types.PlannedPhase) error {
	// Build adjacency list
	graph := make(map[int][]int)
	for _, phase := range phases {
		graph[phase.PhaseNumber] = phase.Dependencies
	}

	// Check each phase for circular dependencies using DFS
	for _, phase := range phases {
		visited := make(map[int]bool)
		if hasCycle(graph, phase.PhaseNumber, visited, make(map[int]bool)) {
			return fmt.Errorf("phase %d (%s) has circular dependencies", phase.PhaseNumber, phase.Title)
		}
	}

	return nil
}

// hasCycle performs DFS to detect cycles
func hasCycle(graph map[int][]int, node int, visited, recStack map[int]bool) bool {
	visited[node] = true
	recStack[node] = true

	for _, neighbor := range graph[node] {
		if !visited[neighbor] {
			if hasCycle(graph, neighbor, visited, recStack) {
				return true
			}
		} else if recStack[neighbor] {
			return true
		}
	}

	recStack[node] = false
	return false
}
