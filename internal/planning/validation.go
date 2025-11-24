package planning

import (
	"context"
	"sort"

	"github.com/steveyegge/vc/internal/types"
)

// Validator is the interface for pluggable plan validation.
type Validator interface {
	// Name returns a unique identifier for this validator.
	Name() string

	// Priority determines execution order (lower values run first).
	// Suggested priorities:
	//   1-9:   Critical structural checks (cycle detection)
	//   10-99: Content quality checks (phase size, acceptance criteria)
	//   100+:  AI-driven analysis (gap detection)
	Priority() int

	// Validate checks the plan and returns any errors or warnings found.
	Validate(ctx context.Context, plan *MissionPlan, vctx *ValidationContext) ValidationResult
}

// ValidationContext provides mission context to validators.
type ValidationContext struct {
	// OriginalIssue is the parent mission issue from Beads.
	OriginalIssue *types.Issue

	// Constraints are the non-functional requirements from the mission.
	Constraints []string

	// Goals are the mission objectives.
	Goals []string
}

// ValidationResult contains errors and warnings from validation.
type ValidationResult struct {
	// Errors are blocking issues that must be fixed before approval.
	Errors []ValidationError

	// Warnings are recommended fixes that can be overridden with --force.
	Warnings []ValidationWarning
}

// ValidationError represents a blocking validation failure.
type ValidationError struct {
	// Code is a machine-readable error identifier (e.g., "CYCLE_DETECTED").
	Code string

	// Message is a human-readable error description.
	Message string

	// Location indicates where in the plan the error occurs (e.g., "phase-2", "task-3-1").
	Location string

	// Details provides additional context as key-value pairs.
	Details map[string]interface{}
}

// ValidationWarning represents a recommended fix that doesn't block approval.
type ValidationWarning struct {
	// Code is a machine-readable warning identifier (e.g., "VAGUE_AC").
	Code string

	// Message is a human-readable warning description.
	Message string

	// Location indicates where in the plan the warning occurs.
	Location string

	// Severity indicates how important this warning is.
	Severity WarningSeverity
}

// WarningSeverity indicates the importance of a warning.
type WarningSeverity int

const (
	// WarningSeverityLow indicates minor issues that are nice to fix.
	WarningSeverityLow WarningSeverity = iota

	// WarningSeverityMedium indicates issues that should be addressed.
	WarningSeverityMedium

	// WarningSeverityHigh indicates serious issues that strongly suggest plan revision.
	WarningSeverityHigh
)

// String returns the string representation of the severity.
func (s WarningSeverity) String() string {
	switch s {
	case WarningSeverityLow:
		return "LOW"
	case WarningSeverityMedium:
		return "MEDIUM"
	case WarningSeverityHigh:
		return "HIGH"
	default:
		return "UNKNOWN"
	}
}

// ValidatorRegistry manages a collection of validators and orchestrates validation.
type ValidatorRegistry struct {
	validators []Validator
}

// NewValidatorRegistry creates a new empty registry.
func NewValidatorRegistry() *ValidatorRegistry {
	return &ValidatorRegistry{
		validators: make([]Validator, 0),
	}
}

// Register adds a validator to the registry.
// Validators are automatically sorted by priority after registration.
func (r *ValidatorRegistry) Register(v Validator) {
	r.validators = append(r.validators, v)
	// Sort by priority (lower values first)
	sort.Slice(r.validators, func(i, j int) bool {
		return r.validators[i].Priority() < r.validators[j].Priority()
	})
}

// ValidateAll runs all registered validators against the plan.
// Validators run in priority order (lowest first).
// All validators run even if earlier ones fail (collect all issues).
func (r *ValidatorRegistry) ValidateAll(ctx context.Context, plan *MissionPlan, vctx *ValidationContext) ValidationResult {
	result := ValidationResult{
		Errors:   make([]ValidationError, 0),
		Warnings: make([]ValidationWarning, 0),
	}

	for _, v := range r.validators {
		vr := v.Validate(ctx, plan, vctx)
		result.Errors = append(result.Errors, vr.Errors...)
		result.Warnings = append(result.Warnings, vr.Warnings...)
	}

	return result
}

// HasErrors returns true if the validation result contains any errors.
func (r ValidationResult) HasErrors() bool {
	return len(r.Errors) > 0
}

// HasWarnings returns true if the validation result contains any warnings.
func (r ValidationResult) HasWarnings() bool {
	return len(r.Warnings) > 0
}

// IsValid returns true if there are no errors (warnings are acceptable).
func (r ValidationResult) IsValid() bool {
	return !r.HasErrors()
}
