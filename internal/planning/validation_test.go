package planning

import (
	"context"
	"testing"

	"github.com/steveyegge/vc/internal/types"
)

// mockValidator is a simple test validator.
type mockValidator struct {
	name     string
	priority int
	errors   []ValidationError
	warnings []ValidationWarning
}

func (m *mockValidator) Name() string { return m.name }
func (m *mockValidator) Priority() int { return m.priority }
func (m *mockValidator) Validate(ctx context.Context, plan *MissionPlan, vctx *ValidationContext) ValidationResult {
	return ValidationResult{
		Errors:   m.errors,
		Warnings: m.warnings,
	}
}

func TestValidatorRegistry_Register(t *testing.T) {
	registry := NewValidatorRegistry()

	// Register validators in random order
	v3 := &mockValidator{name: "third", priority: 100}
	v1 := &mockValidator{name: "first", priority: 1}
	v2 := &mockValidator{name: "second", priority: 10}

	registry.Register(v3)
	registry.Register(v1)
	registry.Register(v2)

	// Verify validators are sorted by priority
	if len(registry.validators) != 3 {
		t.Fatalf("expected 3 validators, got %d", len(registry.validators))
	}

	if registry.validators[0].Name() != "first" {
		t.Errorf("expected first validator to be 'first', got '%s'", registry.validators[0].Name())
	}
	if registry.validators[1].Name() != "second" {
		t.Errorf("expected second validator to be 'second', got '%s'", registry.validators[1].Name())
	}
	if registry.validators[2].Name() != "third" {
		t.Errorf("expected third validator to be 'third', got '%s'", registry.validators[2].Name())
	}
}

func TestValidatorRegistry_ValidateAll(t *testing.T) {
	registry := NewValidatorRegistry()

	// Create validators with different results
	v1 := &mockValidator{
		name:     "v1",
		priority: 1,
		errors: []ValidationError{
			{Code: "E1", Message: "Error from v1"},
		},
	}
	v2 := &mockValidator{
		name:     "v2",
		priority: 2,
		warnings: []ValidationWarning{
			{Code: "W1", Message: "Warning from v2"},
		},
	}
	v3 := &mockValidator{
		name:     "v3",
		priority: 3,
		errors: []ValidationError{
			{Code: "E2", Message: "Error from v3"},
		},
		warnings: []ValidationWarning{
			{Code: "W2", Message: "Warning from v3"},
		},
	}

	registry.Register(v1)
	registry.Register(v2)
	registry.Register(v3)

	// Run validation
	plan := &MissionPlan{}
	vctx := &ValidationContext{}
	result := registry.ValidateAll(context.Background(), plan, vctx)

	// Verify all errors and warnings are collected
	if len(result.Errors) != 2 {
		t.Errorf("expected 2 errors, got %d", len(result.Errors))
	}
	if len(result.Warnings) != 2 {
		t.Errorf("expected 2 warnings, got %d", len(result.Warnings))
	}

	// Verify HasErrors and HasWarnings
	if !result.HasErrors() {
		t.Error("expected HasErrors() to be true")
	}
	if !result.HasWarnings() {
		t.Error("expected HasWarnings() to be true")
	}
	if result.IsValid() {
		t.Error("expected IsValid() to be false when errors present")
	}
}

func TestValidationResult_Helpers(t *testing.T) {
	tests := []struct {
		name         string
		errors       []ValidationError
		warnings     []ValidationWarning
		wantHasErrs  bool
		wantHasWarns bool
		wantValid    bool
	}{
		{
			name:         "empty result",
			errors:       nil,
			warnings:     nil,
			wantHasErrs:  false,
			wantHasWarns: false,
			wantValid:    true,
		},
		{
			name: "only errors",
			errors: []ValidationError{
				{Code: "E1"},
			},
			warnings:     nil,
			wantHasErrs:  true,
			wantHasWarns: false,
			wantValid:    false,
		},
		{
			name:   "only warnings",
			errors: nil,
			warnings: []ValidationWarning{
				{Code: "W1"},
			},
			wantHasErrs:  false,
			wantHasWarns: true,
			wantValid:    true, // Warnings don't invalidate
		},
		{
			name: "errors and warnings",
			errors: []ValidationError{
				{Code: "E1"},
			},
			warnings: []ValidationWarning{
				{Code: "W1"},
			},
			wantHasErrs:  true,
			wantHasWarns: true,
			wantValid:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ValidationResult{
				Errors:   tt.errors,
				Warnings: tt.warnings,
			}

			if got := result.HasErrors(); got != tt.wantHasErrs {
				t.Errorf("HasErrors() = %v, want %v", got, tt.wantHasErrs)
			}
			if got := result.HasWarnings(); got != tt.wantHasWarns {
				t.Errorf("HasWarnings() = %v, want %v", got, tt.wantHasWarns)
			}
			if got := result.IsValid(); got != tt.wantValid {
				t.Errorf("IsValid() = %v, want %v", got, tt.wantValid)
			}
		})
	}
}

func TestWarningSeverity_String(t *testing.T) {
	tests := []struct {
		severity WarningSeverity
		want     string
	}{
		{WarningSeverityLow, "LOW"},
		{WarningSeverityMedium, "MEDIUM"},
		{WarningSeverityHigh, "HIGH"},
		{WarningSeverity(999), "UNKNOWN"},
	}

	for _, tt := range tests {
		if got := tt.severity.String(); got != tt.want {
			t.Errorf("WarningSeverity(%d).String() = %q, want %q", tt.severity, got, tt.want)
		}
	}
}

func TestValidationContext(t *testing.T) {
	// Test that ValidationContext can be created with various fields
	issue := &types.Issue{
		ID:    "vc-test",
		Title: "Test Mission",
	}

	vctx := &ValidationContext{
		OriginalIssue: issue,
		Constraints:   []string{"test < 5s", "no breaking changes"},
		Goals:         []string{"improve performance"},
	}

	if vctx.OriginalIssue.ID != "vc-test" {
		t.Errorf("expected issue ID 'vc-test', got '%s'", vctx.OriginalIssue.ID)
	}
	if len(vctx.Constraints) != 2 {
		t.Errorf("expected 2 constraints, got %d", len(vctx.Constraints))
	}
	if len(vctx.Goals) != 1 {
		t.Errorf("expected 1 goal, got %d", len(vctx.Goals))
	}
}
