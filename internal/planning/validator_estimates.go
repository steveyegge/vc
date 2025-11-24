package planning

import (
	"context"
	"fmt"
)

const (
	// MaxPhaseHours is the maximum reasonable time for a single phase.
	// Phases longer than this should likely be split.
	MaxPhaseHours = 20.0

	// MaxTaskMinutes is the maximum reasonable time for a single task.
	// Tasks longer than this should likely be broken down.
	MaxTaskMinutes = 240 // 4 hours
)

// EstimateValidator checks that time estimates are reasonable.
// Unrealistic estimates suggest poor planning or overly complex tasks.
type EstimateValidator struct{}

// Name returns the validator identifier.
func (v *EstimateValidator) Name() string {
	return "estimate_reasonableness"
}

// Priority returns 10 (runs after structural checks).
func (v *EstimateValidator) Priority() int {
	return 10
}

// Validate checks that phase and task estimates are reasonable.
func (v *EstimateValidator) Validate(ctx context.Context, plan *MissionPlan, vctx *ValidationContext) ValidationResult {
	result := ValidationResult{
		Errors:   make([]ValidationError, 0),
		Warnings: make([]ValidationWarning, 0),
	}

	// Check phase estimates
	for _, phase := range plan.Phases {
		if phase.EstimatedHours > MaxPhaseHours {
			result.Warnings = append(result.Warnings, ValidationWarning{
				Code:     "PHASE_ESTIMATE_TOO_HIGH",
				Message:  fmt.Sprintf("Phase '%s' estimates %.1f hours, recommended maximum is %.1f (consider splitting)", phase.Title, phase.EstimatedHours, MaxPhaseHours),
				Location: phase.ID,
				Severity: WarningSeverityHigh,
			})
		}

		// Check task estimates within phase
		for _, task := range phase.Tasks {
			if task.EstimatedMinutes > MaxTaskMinutes {
				result.Warnings = append(result.Warnings, ValidationWarning{
					Code:     "TASK_ESTIMATE_TOO_HIGH",
					Message:  fmt.Sprintf("Task '%s' estimates %d minutes, recommended maximum is %d (consider breaking down)", task.Title, task.EstimatedMinutes, MaxTaskMinutes),
					Location: task.ID,
					Severity: WarningSeverityMedium,
				})
			}

			// Zero or negative estimates are suspicious
			if task.EstimatedMinutes <= 0 {
				result.Warnings = append(result.Warnings, ValidationWarning{
					Code:     "TASK_ESTIMATE_INVALID",
					Message:  fmt.Sprintf("Task '%s' has invalid estimate: %d minutes", task.Title, task.EstimatedMinutes),
					Location: task.ID,
					Severity: WarningSeverityLow,
				})
			}
		}
	}

	// Check total mission estimate consistency
	var sumPhaseHours float64
	for _, phase := range plan.Phases {
		sumPhaseHours += phase.EstimatedHours
	}

	// Allow 10% tolerance for rounding differences
	tolerance := 0.1 * sumPhaseHours
	if plan.EstimatedHours < sumPhaseHours-tolerance || plan.EstimatedHours > sumPhaseHours+tolerance {
		result.Warnings = append(result.Warnings, ValidationWarning{
			Code:     "ESTIMATE_MISMATCH",
			Message:  fmt.Sprintf("Mission total estimate (%.1fh) doesn't match sum of phases (%.1fh)", plan.EstimatedHours, sumPhaseHours),
			Location: "plan",
			Severity: WarningSeverityLow,
		})
	}

	return result
}
