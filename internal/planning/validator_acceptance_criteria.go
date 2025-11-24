package planning

import (
	"context"
	"fmt"
	"strings"
)

// AcceptanceCriteriaValidator checks that all tasks have well-formed acceptance criteria.
// Tasks without acceptance criteria are hard to verify and may lead to incomplete work.
type AcceptanceCriteriaValidator struct{}

// Name returns the validator identifier.
func (v *AcceptanceCriteriaValidator) Name() string {
	return "acceptance_criteria"
}

// Priority returns 10 (runs after structural checks).
func (v *AcceptanceCriteriaValidator) Priority() int {
	return 10
}

// Validate checks that all tasks have acceptance criteria in WHEN...THEN... format.
func (v *AcceptanceCriteriaValidator) Validate(ctx context.Context, plan *MissionPlan, vctx *ValidationContext) ValidationResult {
	result := ValidationResult{
		Errors:   make([]ValidationError, 0),
		Warnings: make([]ValidationWarning, 0),
	}

	for _, phase := range plan.Phases {
		for _, task := range phase.Tasks {
			// Error: completely missing acceptance criteria
			if len(task.AcceptanceCriteria) == 0 {
				result.Errors = append(result.Errors, ValidationError{
					Code:     "MISSING_ACCEPTANCE_CRITERIA",
					Message:  fmt.Sprintf("Task '%s' has no acceptance criteria", task.Title),
					Location: task.ID,
					Details: map[string]interface{}{
						"phase":      phase.ID,
						"task":       task.ID,
						"task_title": task.Title,
					},
				})
				continue
			}

			// Warning: acceptance criteria don't follow WHEN...THEN... format
			for i, ac := range task.AcceptanceCriteria {
				if !v.isWellFormed(ac) {
					result.Warnings = append(result.Warnings, ValidationWarning{
						Code:     "VAGUE_ACCEPTANCE_CRITERIA",
						Message:  fmt.Sprintf("Task '%s' has vague acceptance criteria (not in WHEN...THEN... format): '%s'", task.Title, ac),
						Location: task.ID,
						Severity: WarningSeverityMedium,
					})
					_ = i // Keep track of which criterion is problematic
				}
			}
		}
	}

	return result
}

// isWellFormed checks if an acceptance criterion follows WHEN...THEN... format.
// Returns true if the criterion contains both "WHEN" and "THEN" keywords.
func (v *AcceptanceCriteriaValidator) isWellFormed(ac string) bool {
	upper := strings.ToUpper(ac)
	hasWhen := strings.Contains(upper, "WHEN")
	hasThen := strings.Contains(upper, "THEN")
	return hasWhen && hasThen
}
