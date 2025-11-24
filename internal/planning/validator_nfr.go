package planning

import (
	"context"
	"fmt"
	"strings"
)

// NFRCoverageValidator checks that non-functional requirements are addressed in the plan.
// Performance, security, and scope constraints should have corresponding validation tasks.
type NFRCoverageValidator struct{}

// Name returns the validator identifier.
func (v *NFRCoverageValidator) Name() string {
	return "nfr_coverage"
}

// Priority returns 10 (runs after structural checks).
func (v *NFRCoverageValidator) Priority() int {
	return 10
}

// Validate checks that each NFR constraint has corresponding tasks or acceptance criteria.
func (v *NFRCoverageValidator) Validate(ctx context.Context, plan *MissionPlan, vctx *ValidationContext) ValidationResult {
	result := ValidationResult{
		Errors:   make([]ValidationError, 0),
		Warnings: make([]ValidationWarning, 0),
	}

	// Check each constraint from the mission
	for _, constraint := range plan.Constraints {
		if !v.isConstraintAddressed(plan, constraint) {
			// Determine severity based on constraint type
			severity := v.classifyConstraint(constraint)

			result.Warnings = append(result.Warnings, ValidationWarning{
				Code:     "NFR_NOT_ADDRESSED",
				Message:  fmt.Sprintf("Constraint '%s' is not addressed by any task or acceptance criteria", constraint),
				Location: "plan",
				Severity: severity,
			})
		}
	}

	return result
}

// isConstraintAddressed checks if a constraint is mentioned in task titles,
// descriptions, or acceptance criteria.
func (v *NFRCoverageValidator) isConstraintAddressed(plan *MissionPlan, constraint string) bool {
	// Extract key terms from the constraint (simple keyword matching)
	keywords := v.extractKeywords(constraint)

	// Search through all tasks
	for _, phase := range plan.Phases {
		for _, task := range phase.Tasks {
			// Check task title and description
			if v.containsAnyKeyword(task.Title, keywords) ||
				v.containsAnyKeyword(task.Description, keywords) {
				return true
			}

			// Check acceptance criteria
			for _, ac := range task.AcceptanceCriteria {
				if v.containsAnyKeyword(ac, keywords) {
					return true
				}
			}
		}
	}

	return false
}

// extractKeywords pulls out important words from a constraint.
// This is a simple heuristic - we focus on nouns and technical terms.
func (v *NFRCoverageValidator) extractKeywords(constraint string) []string {
	// Split on whitespace and common punctuation
	words := strings.FieldsFunc(constraint, func(r rune) bool {
		return r == ' ' || r == ',' || r == '.' || r == ':' || r == ';'
	})

	// Filter out common words and keep meaningful terms
	keywords := make([]string, 0, len(words))
	stopWords := map[string]bool{
		"the": true, "a": true, "an": true, "and": true, "or": true,
		"but": true, "in": true, "on": true, "at": true, "to": true,
		"for": true, "of": true, "with": true, "by": true, "from": true,
		"is": true, "are": true, "was": true, "were": true, "be": true,
		"been": true, "being": true, "have": true, "has": true, "had": true,
		"do": true, "does": true, "did": true, "will": true, "would": true,
		"should": true, "could": true, "may": true, "might": true, "must": true,
	}

	for _, word := range words {
		lower := strings.ToLower(word)
		// Keep words that are not stop words and have length > 2
		if !stopWords[lower] && len(word) > 2 {
			keywords = append(keywords, lower)
		}
	}

	return keywords
}

// containsAnyKeyword checks if text contains any of the keywords (case-insensitive).
func (v *NFRCoverageValidator) containsAnyKeyword(text string, keywords []string) bool {
	lower := strings.ToLower(text)
	for _, keyword := range keywords {
		if strings.Contains(lower, keyword) {
			return true
		}
	}
	return false
}

// classifyConstraint determines warning severity based on constraint content.
func (v *NFRCoverageValidator) classifyConstraint(constraint string) WarningSeverity {
	lower := strings.ToLower(constraint)

	// High priority: performance, security, correctness
	highPriority := []string{"performance", "security", "test", "validate", "verify", "check"}
	for _, term := range highPriority {
		if strings.Contains(lower, term) {
			return WarningSeverityHigh
		}
	}

	// Medium priority: compatibility, scope
	mediumPriority := []string{"backward", "compatible", "breaking", "scope", "limit"}
	for _, term := range mediumPriority {
		if strings.Contains(lower, term) {
			return WarningSeverityMedium
		}
	}

	// Default to low priority
	return WarningSeverityLow
}
