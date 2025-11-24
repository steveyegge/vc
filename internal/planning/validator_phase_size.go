package planning

import (
	"context"
	"fmt"
)

const (
	// MinPhaseTasks is the minimum recommended number of tasks per phase.
	MinPhaseTasks = 3

	// MaxPhaseTasks is the maximum recommended number of tasks per phase.
	MaxPhaseTasks = 15
)

// PhaseSizeValidator checks that phases have a reasonable number of tasks.
// Phases that are too small may indicate over-fragmentation.
// Phases that are too large may be hard to reason about and should be split.
type PhaseSizeValidator struct{}

// Name returns the validator identifier.
func (v *PhaseSizeValidator) Name() string {
	return "phase_size"
}

// Priority returns 10 (runs after structural checks).
func (v *PhaseSizeValidator) Priority() int {
	return 10
}

// Validate checks that each phase has between MinPhaseTasks and MaxPhaseTasks tasks.
func (v *PhaseSizeValidator) Validate(ctx context.Context, plan *MissionPlan, vctx *ValidationContext) ValidationResult {
	result := ValidationResult{
		Errors:   make([]ValidationError, 0),
		Warnings: make([]ValidationWarning, 0),
	}

	for _, phase := range plan.Phases {
		taskCount := len(phase.Tasks)

		if taskCount < MinPhaseTasks {
			result.Warnings = append(result.Warnings, ValidationWarning{
				Code:     "PHASE_TOO_SMALL",
				Message:  fmt.Sprintf("Phase '%s' has only %d task(s), recommended minimum is %d", phase.Title, taskCount, MinPhaseTasks),
				Location: phase.ID,
				Severity: WarningSeverityMedium,
			})
		} else if taskCount > MaxPhaseTasks {
			result.Warnings = append(result.Warnings, ValidationWarning{
				Code:     "PHASE_TOO_LARGE",
				Message:  fmt.Sprintf("Phase '%s' has %d tasks, recommended maximum is %d (consider splitting)", phase.Title, taskCount, MaxPhaseTasks),
				Location: phase.ID,
				Severity: WarningSeverityHigh,
			})
		}
	}

	return result
}
