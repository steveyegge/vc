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

	// JSON parse retry loop (max 2 retries for malformed JSON)
	const maxJSONRetries = 2
	var lastParseError string
	var response *anthropic.Message

	for jsonRetry := 0; jsonRetry <= maxJSONRetries; jsonRetry++ {
		// If this is a retry, add clarification to the prompt
		currentPrompt := prompt
		if jsonRetry > 0 {
			currentPrompt = fmt.Sprintf(`%s

IMPORTANT - Previous Response Had JSON Parse Error:
Your previous response failed to parse with error: %s

Please ensure your response is ONLY raw JSON (no markdown fences, no extra text).
The JSON must be valid and match the exact schema specified above.`, prompt, lastParseError)
			fmt.Printf("⚠️  JSON parse failed (attempt %d/%d), retrying with clarified prompt\n", jsonRetry, maxJSONRetries+1)
		}

		// Call Anthropic API with retry logic (for network/rate limit errors)
		err := s.retryWithBackoff(ctx, "planning", func(attemptCtx context.Context) error {
			resp, apiErr := s.client.Messages.New(attemptCtx, anthropic.MessageNewParams{
				Model:     anthropic.Model(s.model),
				MaxTokens: 8192, // Larger token limit for complex plans
				Messages: []anthropic.MessageParam{
					anthropic.NewUserMessage(anthropic.NewTextBlock(currentPrompt)),
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

		// If parse succeeded, continue with validation
		if parseResult.Success {
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
			if err := s.recordAIUsage(ctx, planningCtx.Mission.ID, "planning", response.Usage.InputTokens, response.Usage.OutputTokens, duration); err != nil {
				fmt.Fprintf(os.Stderr, "warning: failed to log AI usage: %v\n", err)
			}

			return &plan, nil
		}

		// Parse failed - save error for retry
		lastParseError = parseResult.Error
		fmt.Fprintf(os.Stderr, "JSON parse error (attempt %d/%d): %s\n", jsonRetry+1, maxJSONRetries+1, lastParseError)
		fmt.Fprintf(os.Stderr, "Response preview: %s\n", truncateString(responseText, 200))

		// If we've exhausted retries, fail
		if jsonRetry == maxJSONRetries {
			fmt.Fprintf(os.Stderr, "Full AI planning response (final attempt): %s\n", responseText)
			return nil, fmt.Errorf("failed to parse mission plan response after %d attempts: %s (response: %s)",
				maxJSONRetries+1, lastParseError, truncateString(responseText, 500))
		}

		// Brief pause before JSON retry (not exponential, just 1 second)
		time.Sleep(1 * time.Second)
	}

	// Should never reach here, but for safety
	return nil, fmt.Errorf("failed to generate valid plan after %d attempts", maxJSONRetries+1)
}

// RefinePhase breaks a planned epic down into granular tasks
// This is called when a child epic is ready to execute
func (s *Supervisor) RefinePhase(ctx context.Context, plannedEpic *types.PlannedPhase, missionCtx *types.PlanningContext) ([]types.PlannedTask, error) {
	// Add overall timeout to prevent indefinite retries
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()

	startTime := time.Now()

	// Validate inputs
	if plannedEpic == nil {
		return nil, fmt.Errorf("planned epic is required")
	}
	if err := plannedEpic.Validate(); err != nil {
		return nil, fmt.Errorf("invalid planned epic: %w", err)
	}
	if missionCtx != nil {
		if err := missionCtx.Validate(); err != nil {
			return nil, fmt.Errorf("invalid mission context: %w", err)
		}
	}

	// Build the refinement prompt
	prompt := s.buildRefinementPrompt(plannedEpic, missionCtx)

	// JSON parse retry loop (max 2 retries for malformed JSON)
	const maxJSONRetries = 2
	var lastParseError string
	var response *anthropic.Message

	for jsonRetry := 0; jsonRetry <= maxJSONRetries; jsonRetry++ {
		// If this is a retry, add clarification to the prompt
		currentPrompt := prompt
		if jsonRetry > 0 {
			currentPrompt = fmt.Sprintf(`%s

IMPORTANT - Previous Response Had JSON Parse Error:
Your previous response failed to parse with error: %s

Please ensure your response is ONLY raw JSON (no markdown fences, no extra text).
The JSON must be valid and match the exact schema specified above.`, prompt, lastParseError)
			fmt.Printf("⚠️  JSON parse failed (attempt %d/%d), retrying with clarified prompt\n", jsonRetry, maxJSONRetries+1)
		}

		// Call Anthropic API with retry logic (for network/rate limit errors)
		err := s.retryWithBackoff(ctx, "refinement", func(attemptCtx context.Context) error {
			resp, apiErr := s.client.Messages.New(attemptCtx, anthropic.MessageNewParams{
				Model:     anthropic.Model(s.model),
				MaxTokens: 8192,
				Messages: []anthropic.MessageParam{
					anthropic.NewUserMessage(anthropic.NewTextBlock(currentPrompt)),
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

		// If parse succeeded, continue with validation
		if parseResult.Success {
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
			fmt.Printf("AI Refinement for epic '%s': tasks=%d, duration=%v\n",
				plannedEpic.Title, len(tasks), duration)

			// Log AI usage
			if err := s.recordAIUsage(ctx, plannedEpic.Title, "refinement", response.Usage.InputTokens, response.Usage.OutputTokens, duration); err != nil {
				fmt.Fprintf(os.Stderr, "warning: failed to log AI usage: %v\n", err)
			}

			return tasks, nil
		}

		// Parse failed - save error for retry
		lastParseError = parseResult.Error
		fmt.Fprintf(os.Stderr, "JSON parse error (attempt %d/%d): %s\n", jsonRetry+1, maxJSONRetries+1, lastParseError)
		fmt.Fprintf(os.Stderr, "Response preview: %s\n", truncateString(responseText, 200))

		// If we've exhausted retries, fail
		if jsonRetry == maxJSONRetries {
			fmt.Fprintf(os.Stderr, "Full AI refinement response (final attempt): %s\n", responseText)
			return nil, fmt.Errorf("failed to parse refinement response after %d attempts: %s (response: %s)",
				maxJSONRetries+1, lastParseError, truncateString(responseText, 500))
		}

		// Brief pause before JSON retry (not exponential, just 1 second)
		time.Sleep(1 * time.Second)
	}

	// Should never reach here, but for safety
	return nil, fmt.Errorf("failed to refine phase after %d attempts", maxJSONRetries+1)
}

// ValidatePlan checks if a generated plan is valid and executable
func (s *Supervisor) ValidatePlan(ctx context.Context, plan *types.MissionPlan) error {
	// Basic validation already done by types.MissionPlan.Validate()
	if err := plan.Validate(); err != nil {
		return err
	}

	// Run validators with panic recovery and timeouts
	// Collect validation errors but allow all validators to run
	var validationErrors []string

	// Define validators as a slice of functions for easier iteration
	validators := []struct {
		name string
		fn   func(context.Context, *types.MissionPlan) error
	}{
		{"plan_size", s.validatePlanSize},
		{"circular_dependencies", s.validateCircularDependencies},
		{"dependency_references", s.validateDependencyReferences},
		{"task_counts", s.validateTaskCounts},
		{"phase_structure_ai", s.validatePhaseStructureWrapper},
	}

	// Run each validator with panic recovery and timeout
	for _, validator := range validators {
		if err := s.runValidatorSafely(ctx, validator.name, func(ctx context.Context) error {
			return validator.fn(ctx, plan)
		}); err != nil {
			validationErrors = append(validationErrors, fmt.Sprintf("%s: %s", validator.name, err.Error()))
		}
	}

	// If any validators failed, return combined error
	if len(validationErrors) > 0 {
		return fmt.Errorf("validation failed: %s", strings.Join(validationErrors, "; "))
	}

	return nil
}

// runValidatorSafely runs a validator with panic recovery and timeout
func (s *Supervisor) runValidatorSafely(ctx context.Context, name string, fn func(context.Context) error) (err error) {
	// Default timeout is 30 seconds per validator (configurable via environment)
	timeout := 30 * time.Second
	if envTimeout := os.Getenv("VC_VALIDATOR_TIMEOUT"); envTimeout != "" {
		if parsed, parseErr := time.ParseDuration(envTimeout); parseErr == nil {
			timeout = parsed
		}
	}

	// Create timeout context for this validator
	validatorCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// Channel for validator result
	done := make(chan error, 1)

	// Run validator in goroutine with panic recovery
	go func() {
		defer func() {
			if r := recover(); r != nil {
				done <- fmt.Errorf("validator panic: %v", r)
			}
		}()
		done <- fn(validatorCtx)
	}()

	// Wait for result or timeout
	select {
	case err = <-done:
		if err != nil {
			fmt.Fprintf(os.Stderr, "Validator %s failed: %v\n", name, err)
		}
		return err
	case <-validatorCtx.Done():
		err = fmt.Errorf("validator timeout after %v", timeout)
		fmt.Fprintf(os.Stderr, "Validator %s timed out after %v\n", name, timeout)
		return err
	}
}

// validatePlanSize enforces limits on plan size to prevent timeouts
// Checks: max phases, max tasks per phase, max total tasks, max dependency depth
func (s *Supervisor) validatePlanSize(ctx context.Context, plan *types.MissionPlan) error {
	// Get configurable limits from environment (with defaults)
	maxPhases := getEnvInt("VC_MAX_PLAN_PHASES", 20)
	maxPhaseTasks := getEnvInt("VC_MAX_PHASE_TASKS", 30)
	maxDependencyDepth := getEnvInt("VC_MAX_DEPENDENCY_DEPTH", 10)

	// Check phase count
	phaseCount := len(plan.Phases)
	if phaseCount > maxPhases {
		return fmt.Errorf("plan has too many phases (%d > %d limit); risk of timeout during validation",
			phaseCount, maxPhases)
	}

	// Check tasks per phase and count total tasks
	totalTasks := 0
	for _, phase := range plan.Phases {
		taskCount := len(phase.Tasks)
		totalTasks += taskCount
		if taskCount > maxPhaseTasks {
			return fmt.Errorf("phase %d (%s) has too many tasks (%d > %d limit); risk of timeout during refinement",
				phase.PhaseNumber, phase.Title, taskCount, maxPhaseTasks)
		}
	}

	// Check total tasks (max is computed from limits)
	maxTotalTasks := maxPhases * maxPhaseTasks
	if totalTasks > maxTotalTasks {
		return fmt.Errorf("plan has too many total tasks (%d > %d limit); risk of timeout during approval",
			totalTasks, maxTotalTasks)
	}

	// Check dependency depth using topological analysis
	depth := calculateDependencyDepth(plan.Phases)
	if depth > maxDependencyDepth {
		return fmt.Errorf("plan has excessive dependency depth (%d > %d limit); risk of pathological dependency graph",
			depth, maxDependencyDepth)
	}

	return nil
}

// CalculateDependencyDepthExported is a public wrapper for calculateDependencyDepth
// Exported for use in integration tests
func CalculateDependencyDepthExported(phases []types.PlannedPhase) int {
	return calculateDependencyDepth(phases)
}

// calculateDependencyDepth computes the maximum dependency depth in the phase graph
// Depth is the longest path from a phase with no dependencies to any phase
func calculateDependencyDepth(phases []types.PlannedPhase) int {
	// Build phase number to index map
	phaseIndex := make(map[int]int)
	for i, phase := range phases {
		phaseIndex[phase.PhaseNumber] = i
	}

	// Memoization for depth calculation
	depths := make(map[int]int)

	// Recursive depth calculation with memoization
	var computeDepth func(phaseNum int) int
	computeDepth = func(phaseNum int) int {
		// Check if already computed
		if d, ok := depths[phaseNum]; ok {
			return d
		}

		// Get phase
		idx, exists := phaseIndex[phaseNum]
		if !exists {
			return 0 // Invalid phase number, treat as depth 0
		}
		phase := phases[idx]

		// Base case: no dependencies
		if len(phase.Dependencies) == 0 {
			depths[phaseNum] = 1
			return 1
		}

		// Recursive case: 1 + max(depth of dependencies)
		maxDepth := 0
		for _, depNum := range phase.Dependencies {
			depDepth := computeDepth(depNum)
			if depDepth > maxDepth {
				maxDepth = depDepth
			}
		}

		depths[phaseNum] = maxDepth + 1
		return maxDepth + 1
	}

	// Find maximum depth across all phases
	maxDepth := 0
	for _, phase := range phases {
		depth := computeDepth(phase.PhaseNumber)
		if depth > maxDepth {
			maxDepth = depth
		}
	}

	return maxDepth
}

// getEnvInt gets an integer from environment variable with default fallback
func getEnvInt(key string, defaultValue int) int {
	if val := os.Getenv(key); val != "" {
		if parsed, err := fmt.Sscanf(val, "%d", &defaultValue); err == nil && parsed == 1 {
			return defaultValue
		}
		fmt.Fprintf(os.Stderr, "Warning: invalid integer for %s: %s (using default %d)\n", key, val, defaultValue)
	}
	return defaultValue
}

// validateCircularDependencies checks for circular dependencies in phases
func (s *Supervisor) validateCircularDependencies(ctx context.Context, plan *types.MissionPlan) error {
	if err := checkCircularDependencies(plan.Phases); err != nil {
		return fmt.Errorf("circular dependencies detected: %w", err)
	}
	return nil
}

// validateDependencyReferences checks that all dependency IDs reference existing phases/tasks
func (s *Supervisor) validateDependencyReferences(ctx context.Context, plan *types.MissionPlan) error {
	// Build set of valid phase numbers
	validPhaseNumbers := make(map[int]bool)
	for _, phase := range plan.Phases {
		validPhaseNumbers[phase.PhaseNumber] = true
	}

	// Check phase dependencies
	for _, phase := range plan.Phases {
		for _, depPhaseNum := range phase.Dependencies {
			if !validPhaseNumbers[depPhaseNum] {
				return fmt.Errorf("phase %d (%s) has invalid dependency: phase %d does not exist",
					phase.PhaseNumber, phase.Title, depPhaseNum)
			}
		}
	}

	return nil
}

// validateTaskCounts checks each phase has reasonable task count
func (s *Supervisor) validateTaskCounts(ctx context.Context, plan *types.MissionPlan) error {
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

// validatePhaseStructureWrapper wraps ValidatePhaseStructure to match validator interface
func (s *Supervisor) validatePhaseStructureWrapper(ctx context.Context, plan *types.MissionPlan) error {
	// Skip AI validation for single phase plans (optimization)
	if len(plan.Phases) == 1 {
		return nil
	}

	// Skip AI validation if supervisor is not initialized (e.g., in tests)
	if s == nil || s.client == nil {
		return nil
	}

	// AI validator might fail (network issues, API errors, etc.)
	// Log warning but don't block validation on AI failure
	if err := s.ValidatePhaseStructure(ctx, plan.Phases); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: AI phase structure validation failed: %v\n", err)
		fmt.Fprintf(os.Stderr, "Continuing validation (AI validation is advisory only)\n")
		// Return nil to continue validation - AI failures are non-blocking
		return nil
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
	if err := s.recordAIUsage(ctx, "phase-validation", "phase-validation", response.Usage.InputTokens, response.Usage.OutputTokens, duration); err != nil {
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

ACCEPTANCE CRITERIA FORMAT:
Use WHEN...THEN... scenarios for all acceptance criteria.

GOOD EXAMPLES:
- WHEN creating an issue THEN it persists to SQLite database
- WHEN reading non-existent issue THEN NotFoundError is returned
- WHEN transaction fails THEN retry 3 times with exponential backoff
- WHEN executor shuts down gracefully THEN all in-progress work is checkpointed
- WHEN plan validation detects circular dependencies THEN it rejects the plan with clear error

BAD EXAMPLES (too vague):
- Test storage thoroughly
- Handle errors properly
- Make it robust
- Add good test coverage

Each acceptance criterion should specify:
1. A triggering condition (WHEN...)
2. An observable outcome (THEN...)
3. Specific, measurable behavior (not vague goals)

IMPORTANT GUIDELINES:
- Generate 2-10 phases (prefer fewer, larger phases over many tiny ones)
- Phase numbers start at 1 and must be sequential
- Dependencies array contains phase numbers (must be earlier phases only)
- Each phase should have 3-8 high-level tasks
- Tasks are high-level descriptions, NOT granular implementation steps
- Estimated effort should be realistic: "3 days", "1 week", "2 weeks"
- Confidence should reflect uncertainty (0.0-1.0)
- Consider technical dependencies, logical ordering, and risk
- ALL acceptance criteria must use WHEN...THEN... format (as shown above)

IMPORTANT: Respond with ONLY raw JSON. Do NOT wrap it in markdown code fences (`+"```"+`). Just the JSON object.`,
		mission.ID, mission.Title, mission.Goal,
		mission.Description,
		mission.Context,
		contextSection,
		constraintsSection,
		failedAttemptsSection)
}

// buildRefinementPrompt builds the prompt for refining a planned epic into tasks
func (s *Supervisor) buildRefinementPrompt(plannedEpic *types.PlannedPhase, missionCtx *types.PlanningContext) string {
	// Build mission context if available
	missionSection := ""
	if missionCtx != nil && missionCtx.Mission != nil {
		missionSection = fmt.Sprintf(`
MISSION CONTEXT:
Mission: %s
Goal: %s
`, missionCtx.Mission.Title, missionCtx.Mission.Goal)
	}

	return fmt.Sprintf(`You are refining a child epic of a software development mission into granular, executable tasks.

%s
EPIC TO REFINE:
Title: %s
Strategy: %s

Description:
%s

YOUR TASK:
Break this epic down into 5-20 granular tasks. Each task should be:
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

ACCEPTANCE CRITERIA FORMAT:
Use WHEN...THEN... scenarios for all acceptance criteria.

GOOD EXAMPLES:
- WHEN calling Refine() THEN it stores new iteration with incremented number
- WHEN convergence detected (diff < 5%%) THEN CheckConvergence returns true
- WHEN test suite runs THEN all tests pass in under 5 seconds
- WHEN function receives nil input THEN it returns ErrInvalidInput

BAD EXAMPLES (too vague):
- Test thoroughly
- Handle edge cases
- Make it robust

Each criterion should specify a trigger (WHEN) and outcome (THEN).

GUIDELINES:
- Dependencies array contains task TITLES (not IDs) of tasks in this same list
- Priority: 0=P0 (critical), 1=P1 (high), 2=P2 (medium), 3=P3 (low)
- Type: "task", "bug", "feature", "chore"
- Estimated minutes should be realistic (15-120 minutes typical)
- Acceptance criteria MUST use WHEN...THEN... format (see examples above)
- Include tests as separate tasks
- Order tasks logically (dependencies should reference earlier tasks)

IMPORTANT: Respond with ONLY raw JSON. Do NOT wrap it in markdown code fences (`+"```"+`). Just the JSON object.`,
		missionSection,
		plannedEpic.Title,
		plannedEpic.Strategy,
		plannedEpic.Description)
}

// buildConvergencePrompt builds the prompt for assessing whether a mission plan has converged
func (s *Supervisor) buildConvergencePrompt(previous, current *types.MissionPlan) string {
	// Count total tasks
	prevTasks := countTotalTasks(previous)
	currTasks := countTotalTasks(current)

	// Serialize plans to JSON for comparison
	prevJSON := fmt.Sprintf(`{
  "phases": %d,
  "total_tasks": %d,
  "estimated_effort": "%s",
  "confidence": %.2f,
  "phase_titles": [%s]
}`, len(previous.Phases), prevTasks, previous.EstimatedEffort, previous.Confidence,
		formatPhaseTitles(previous.Phases))

	currJSON := fmt.Sprintf(`{
  "phases": %d,
  "total_tasks": %d,
  "estimated_effort": "%s",
  "confidence": %.2f,
  "phase_titles": [%s]
}`, len(current.Phases), currTasks, current.EstimatedEffort, current.Confidence,
		formatPhaseTitles(current.Phases))

	return fmt.Sprintf(`Check if this plan has converged (stabilized). Compare summaries:

PREVIOUS: %s
CURRENT: %s

CONVERGENCE = TRUE when structure is stable (same phase count, task count, similar effort).
Minor confidence changes (+/- 0.1) are normal polish, NOT major changes.

Return ONLY compact JSON:
{"converged":true/false,"confidence":0.9,"reasoning":"brief","diff_percentage":5.0,"major_changes":[]}`,
		prevJSON, currJSON)
}

// countTotalTasks counts all tasks across all phases
func countTotalTasks(plan *types.MissionPlan) int {
	total := 0
	for _, phase := range plan.Phases {
		total += len(phase.Tasks)
	}
	return total
}

// formatPhaseTitles formats phase titles for JSON display
func formatPhaseTitles(phases []types.PlannedPhase) string {
	if len(phases) == 0 {
		return ""
	}
	titles := make([]string, len(phases))
	for i, phase := range phases {
		titles[i] = fmt.Sprintf(`"%s"`, phase.Title)
	}
	return strings.Join(titles, ", ")
}

// ParseDescription parses a freeform text description into structured Goal and Constraints
// This is used for 'vc plan new' to extract planning context from natural language
func (s *Supervisor) ParseDescription(ctx context.Context, description string) (goal string, constraints []string, err error) {
	// Add timeout to prevent indefinite retries
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()

	startTime := time.Now()

	// Validate input
	description = strings.TrimSpace(description)
	if description == "" {
		return "", nil, fmt.Errorf("description cannot be empty")
	}

	// Build the parsing prompt
	prompt := s.buildDescriptionParsingPrompt(description)

	// JSON parse retry loop (max 2 retries for malformed JSON)
	const maxJSONRetries = 2
	var lastParseError string
	var response *anthropic.Message

	for jsonRetry := 0; jsonRetry <= maxJSONRetries; jsonRetry++ {
		// If this is a retry, add clarification to the prompt
		currentPrompt := prompt
		if jsonRetry > 0 {
			currentPrompt = fmt.Sprintf(`%s

IMPORTANT - Previous Response Had JSON Parse Error:
Your previous response failed to parse with error: %s

Please ensure your response is ONLY raw JSON (no markdown fences, no extra text).
The JSON must be valid and match the exact schema specified above.`, prompt, lastParseError)
			fmt.Printf("⚠️  JSON parse failed (attempt %d/%d), retrying with clarified prompt\n", jsonRetry, maxJSONRetries+1)
		}

		// Call Anthropic API with retry logic (for network/rate limit errors)
		err := s.retryWithBackoff(ctx, "description-parsing", func(attemptCtx context.Context) error {
			resp, apiErr := s.client.Messages.New(attemptCtx, anthropic.MessageNewParams{
				Model:     anthropic.Model(s.model),
				MaxTokens: 2048,
				Messages: []anthropic.MessageParam{
					anthropic.NewUserMessage(anthropic.NewTextBlock(currentPrompt)),
				},
			})
			if apiErr != nil {
				return apiErr
			}
			response = resp
			return nil
		})

		if err != nil {
			return "", nil, fmt.Errorf("anthropic API call failed: %w", err)
		}

		// Extract the text content from the response
		var responseText string
		for _, block := range response.Content {
			if block.Type == "text" {
				responseText += block.Text
			}
		}

		// Parse the response as JSON using resilient parser
		type parsedDescription struct {
			Goal        string   `json:"goal"`
			Constraints []string `json:"constraints"`
		}
		parseResult := Parse[parsedDescription](responseText, ParseOptions{
			Context:   "description parsing response",
			LogErrors: boolPtr(true),
		})

		// If parse succeeded, return the result
		if parseResult.Success {
			parsed := parseResult.Data

			// Validate we got a goal
			if strings.TrimSpace(parsed.Goal) == "" {
				return "", nil, fmt.Errorf("AI failed to extract a goal from the description")
			}

			// Log the parsing
			duration := time.Since(startTime)
			fmt.Printf("AI Description Parsing: goal extracted, %d constraints, duration=%v\n",
				len(parsed.Constraints), duration)

			// Log AI usage to events
			if err := s.recordAIUsage(ctx, "description-parsing", "description-parsing", response.Usage.InputTokens, response.Usage.OutputTokens, duration); err != nil {
				fmt.Fprintf(os.Stderr, "warning: failed to log AI usage: %v\n", err)
			}

			return parsed.Goal, parsed.Constraints, nil
		}

		// Parse failed - save error for retry
		lastParseError = parseResult.Error
		fmt.Fprintf(os.Stderr, "JSON parse error (attempt %d/%d): %s\n", jsonRetry+1, maxJSONRetries+1, lastParseError)
		fmt.Fprintf(os.Stderr, "Response preview: %s\n", truncateString(responseText, 200))

		// If we've exhausted retries, fail
		if jsonRetry == maxJSONRetries {
			fmt.Fprintf(os.Stderr, "Full AI description parsing response (final attempt): %s\n", responseText)
			return "", nil, fmt.Errorf("failed to parse description response after %d attempts: %s (response: %s)",
				maxJSONRetries+1, lastParseError, truncateString(responseText, 500))
		}

		// Brief pause before JSON retry (not exponential, just 1 second)
		time.Sleep(1 * time.Second)
	}

	// Should never reach here, but for safety
	return "", nil, fmt.Errorf("failed to parse description after %d attempts", maxJSONRetries+1)
}

// buildDescriptionParsingPrompt builds the prompt for parsing a freeform description
func (s *Supervisor) buildDescriptionParsingPrompt(description string) string {
	return fmt.Sprintf(`You are parsing a freeform mission description into structured planning components.

USER DESCRIPTION:
%s

YOUR TASK:
Extract the following from the description:
1. **Goal**: The high-level objective or outcome the user wants to achieve
2. **Constraints**: Any non-functional requirements, limitations, or quality criteria

GUIDELINES:
- Goal should be a clear, concise statement of what success looks like
- Constraints are things like: performance requirements, compatibility needs, test coverage targets, time limits, etc.
- If no explicit constraints are mentioned, return an empty array
- Be generous in extracting implicit constraints from context

EXAMPLES:

Input: "Improve test coverage from 46%% to 80%%. Must not slow down test suite beyond 5s baseline."
Output:
{
  "goal": "Improve test coverage from 46%% to 80%%",
  "constraints": ["Test suite runtime must stay under 5 seconds"]
}

Input: "Add user authentication with OAuth2. Need to support GitHub and Google providers."
Output:
{
  "goal": "Add user authentication with OAuth2",
  "constraints": ["Support GitHub OAuth provider", "Support Google OAuth provider"]
}

Input: "Refactor the database layer to use connection pooling."
Output:
{
  "goal": "Refactor the database layer to use connection pooling",
  "constraints": []
}

Return JSON in this format:
{
  "goal": "Clear statement of the objective",
  "constraints": ["Constraint 1", "Constraint 2"]
}

IMPORTANT: Respond with ONLY raw JSON. Do NOT wrap it in markdown code fences. Just the JSON object.`, description)
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
