package planning

import (
	"context"
	"fmt"
	"strings"
)

// CycleDetector validates that plan dependencies form a directed acyclic graph (DAG).
// Circular dependencies would prevent the plan from being executable.
type CycleDetector struct{}

// Name returns the validator identifier.
func (d *CycleDetector) Name() string {
	return "cycle_detector"
}

// Priority returns 1 (runs first, as this is a critical structural check).
func (d *CycleDetector) Priority() int {
	return 1
}

// Validate checks for circular dependencies in both phase and task dependencies.
func (d *CycleDetector) Validate(ctx context.Context, plan *MissionPlan, vctx *ValidationContext) ValidationResult {
	result := ValidationResult{
		Errors:   make([]ValidationError, 0),
		Warnings: make([]ValidationWarning, 0),
	}

	// Check phase dependencies for cycles
	if cycle := d.detectPhaseCycle(plan); len(cycle) > 0 {
		result.Errors = append(result.Errors, ValidationError{
			Code:     "PHASE_CYCLE_DETECTED",
			Message:  fmt.Sprintf("Circular phase dependency detected: %s", strings.Join(cycle, " → ")),
			Location: "phases",
			Details: map[string]interface{}{
				"cycle": cycle,
			},
		})
	}

	// Check task dependencies within each phase for cycles
	for _, phase := range plan.Phases {
		if cycle := d.detectTaskCycle(phase); len(cycle) > 0 {
			result.Errors = append(result.Errors, ValidationError{
				Code:     "TASK_CYCLE_DETECTED",
				Message:  fmt.Sprintf("Circular task dependency detected in %s: %s", phase.ID, strings.Join(cycle, " → ")),
				Location: phase.ID,
				Details: map[string]interface{}{
					"cycle":   cycle,
					"phase":   phase.ID,
					"phase_title": phase.Title,
				},
			})
		}
	}

	return result
}

// detectPhaseCycle uses DFS to detect cycles in phase dependencies.
// Returns the cycle path if found, empty slice otherwise.
func (d *CycleDetector) detectPhaseCycle(plan *MissionPlan) []string {
	// Build phase adjacency list
	graph := make(map[string][]string)
	for _, phase := range plan.Phases {
		graph[phase.ID] = phase.Dependencies
	}

	// Track visited nodes and current DFS path
	visited := make(map[string]bool)
	recStack := make(map[string]bool)
	var path []string

	var dfs func(string) bool
	dfs = func(node string) bool {
		visited[node] = true
		recStack[node] = true
		path = append(path, node)

		for _, neighbor := range graph[node] {
			if !visited[neighbor] {
				if dfs(neighbor) {
					return true
				}
			} else if recStack[neighbor] {
				// Found a cycle - extract the cycle path
				cycleStart := 0
				for i, p := range path {
					if p == neighbor {
						cycleStart = i
						break
					}
				}
				path = append(path[cycleStart:], neighbor) // Close the cycle
				return true
			}
		}

		recStack[node] = false
		path = path[:len(path)-1] // Backtrack
		return false
	}

	// Check all phases for cycles
	for _, phase := range plan.Phases {
		if !visited[phase.ID] {
			path = make([]string, 0)
			if dfs(phase.ID) {
				return path
			}
		}
	}

	return nil
}

// detectTaskCycle uses DFS to detect cycles in task dependencies within a phase.
// Returns the cycle path if found, empty slice otherwise.
func (d *CycleDetector) detectTaskCycle(phase Phase) []string {
	// Build task adjacency list
	graph := make(map[string][]string)
	for _, task := range phase.Tasks {
		graph[task.ID] = task.Dependencies
	}

	// Track visited nodes and current DFS path
	visited := make(map[string]bool)
	recStack := make(map[string]bool)
	var path []string

	var dfs func(string) bool
	dfs = func(node string) bool {
		visited[node] = true
		recStack[node] = true
		path = append(path, node)

		for _, neighbor := range graph[node] {
			if !visited[neighbor] {
				if dfs(neighbor) {
					return true
				}
			} else if recStack[neighbor] {
				// Found a cycle - extract the cycle path
				cycleStart := 0
				for i, p := range path {
					if p == neighbor {
						cycleStart = i
						break
					}
				}
				path = append(path[cycleStart:], neighbor) // Close the cycle
				return true
			}
		}

		recStack[node] = false
		path = path[:len(path)-1] // Backtrack
		return false
	}

	// Check all tasks for cycles
	for _, task := range phase.Tasks {
		if !visited[task.ID] {
			path = make([]string, 0)
			if dfs(task.ID) {
				return path
			}
		}
	}

	return nil
}
