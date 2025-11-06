package types

import (
	"testing"
	"time"
)

// TestIssueFieldsExist verifies that design, acceptance_criteria, and notes fields
// can be set and retrieved correctly on Issue structs
func TestIssueFieldsExist(t *testing.T) {
	now := time.Now()
	issue := Issue{
		ID:                 "test-1",
		Title:              "Test Issue",
		Description:        "Test description",
		Design:             "Test design approach",
		AcceptanceCriteria: "Test acceptance criteria",
		Notes:              "Test notes",
		Status:             StatusOpen,
		Priority:           1,
		IssueType:          TypeTask,
		CreatedAt:          now,
		UpdatedAt:          now,
	}

	// Verify fields are set correctly
	if issue.Design != "Test design approach" {
		t.Errorf("Design field not set correctly: got %q", issue.Design)
	}
	if issue.AcceptanceCriteria != "Test acceptance criteria" {
		t.Errorf("AcceptanceCriteria field not set correctly: got %q", issue.AcceptanceCriteria)
	}
	if issue.Notes != "Test notes" {
		t.Errorf("Notes field not set correctly: got %q", issue.Notes)
	}

	// Verify validation passes with these fields
	if err := issue.Validate(); err != nil {
		t.Errorf("Issue validation failed with valid fields: %v", err)
	}
}

// TestIssueOptionalFields verifies that design and notes are optional fields
// Note: acceptance_criteria requirements depend on issue type (see vc-e3j2)
func TestIssueOptionalFields(t *testing.T) {
	now := time.Now()
	// Use TypeChore which doesn't require acceptance_criteria
	issue := Issue{
		ID:          "test-2",
		Title:       "Minimal Issue",
		Description: "Description only",
		Status:      StatusOpen,
		Priority:    0,
		IssueType:   TypeChore, // Changed from TypeBug - chores don't require acceptance_criteria
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	// Verify empty optional fields
	if issue.Design != "" {
		t.Errorf("Design should be empty by default, got %q", issue.Design)
	}
	if issue.AcceptanceCriteria != "" {
		t.Errorf("AcceptanceCriteria should be empty by default, got %q", issue.AcceptanceCriteria)
	}
	if issue.Notes != "" {
		t.Errorf("Notes should be empty by default, got %q", issue.Notes)
	}

	// Verify validation passes without optional fields (for chore type)
	if err := issue.Validate(); err != nil {
		t.Errorf("Issue validation failed without optional fields: %v", err)
	}
}

// TestDependencyTypeDiscoveredFrom tests the discovered-from dependency type
func TestDependencyTypeDiscoveredFrom(t *testing.T) {
	// Verify the constant exists and has correct value
	if DepDiscoveredFrom != "discovered-from" {
		t.Errorf("DepDiscoveredFrom has wrong value: got %q, want %q", DepDiscoveredFrom, "discovered-from")
	}

	// Verify IsValid() returns true for discovered-from
	if !DepDiscoveredFrom.IsValid() {
		t.Error("DepDiscoveredFrom should be valid but IsValid() returned false")
	}

	// Test creating a dependency with discovered-from type
	now := time.Now()
	dep := Dependency{
		IssueID:     "parent-1",
		DependsOnID: "discovered-1",
		Type:        DepDiscoveredFrom,
		CreatedAt:   now,
		CreatedBy:   "test-user",
	}

	if dep.Type != DepDiscoveredFrom {
		t.Errorf("Dependency type not set correctly: got %q, want %q", dep.Type, DepDiscoveredFrom)
	}

	if !dep.Type.IsValid() {
		t.Error("Dependency with DepDiscoveredFrom type should be valid")
	}
}

// TestDependencyTypeIsValid tests all dependency types pass validation
func TestDependencyTypeIsValid(t *testing.T) {
	tests := []struct {
		name     string
		depType  DependencyType
		expected bool
	}{
		{"blocks is valid", DepBlocks, true},
		{"related is valid", DepRelated, true},
		{"parent-child is valid", DepParentChild, true},
		{"discovered-from is valid", DepDiscoveredFrom, true},
		{"invalid type", DependencyType("invalid"), false},
		{"empty string", DependencyType(""), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.depType.IsValid()
			if result != tt.expected {
				t.Errorf("IsValid() = %v, want %v for type %q", result, tt.expected, tt.depType)
			}
		})
	}
}

// TestAllDependencyTypesValid ensures all defined constants pass validation
func TestAllDependencyTypesValid(t *testing.T) {
	types := []DependencyType{
		DepBlocks,
		DepRelated,
		DepParentChild,
		DepDiscoveredFrom,
	}

	for _, depType := range types {
		t.Run(string(depType), func(t *testing.T) {
			if !depType.IsValid() {
				t.Errorf("Defined constant %q should be valid", depType)
			}
		})
	}
}

// TestAcceptanceCriteriaValidationPolicy validates that acceptance_criteria
// requirements are consistently enforced based on issue type (vc-e3j2)
func TestAcceptanceCriteriaValidationPolicy(t *testing.T) {
	now := time.Now()

	tests := []struct {
		name               string
		issueType          IssueType
		acceptanceCriteria string
		shouldPass         bool
		errorContains      string
	}{
		// Task type - REQUIRES acceptance criteria
		{
			name:               "task with valid acceptance_criteria should pass",
			issueType:          TypeTask,
			acceptanceCriteria: "Valid acceptance criteria",
			shouldPass:         true,
		},
		{
			name:               "task with empty acceptance_criteria should fail",
			issueType:          TypeTask,
			acceptanceCriteria: "",
			shouldPass:         false,
			errorContains:      "acceptance_criteria is required for task issues",
		},
		{
			name:               "task with whitespace-only acceptance_criteria should fail",
			issueType:          TypeTask,
			acceptanceCriteria: "   \n\t  ",
			shouldPass:         false,
			errorContains:      "acceptance_criteria is required for task issues",
		},

		// Bug type - REQUIRES acceptance criteria
		{
			name:               "bug with valid acceptance_criteria should pass",
			issueType:          TypeBug,
			acceptanceCriteria: "Bug is fixed when X works",
			shouldPass:         true,
		},
		{
			name:               "bug with empty acceptance_criteria should fail",
			issueType:          TypeBug,
			acceptanceCriteria: "",
			shouldPass:         false,
			errorContains:      "acceptance_criteria is required for bug issues",
		},
		{
			name:               "bug with whitespace-only acceptance_criteria should fail",
			issueType:          TypeBug,
			acceptanceCriteria: "  \n  ",
			shouldPass:         false,
			errorContains:      "acceptance_criteria is required for bug issues",
		},

		// Feature type - REQUIRES acceptance criteria
		{
			name:               "feature with valid acceptance_criteria should pass",
			issueType:          TypeFeature,
			acceptanceCriteria: "Feature complete when users can do X",
			shouldPass:         true,
		},
		{
			name:               "feature with empty acceptance_criteria should fail",
			issueType:          TypeFeature,
			acceptanceCriteria: "",
			shouldPass:         false,
			errorContains:      "acceptance_criteria is required for feature issues",
		},
		{
			name:               "feature with whitespace-only acceptance_criteria should fail",
			issueType:          TypeFeature,
			acceptanceCriteria: "\t\n",
			shouldPass:         false,
			errorContains:      "acceptance_criteria is required for feature issues",
		},

		// Epic type - does NOT require acceptance criteria
		{
			name:               "epic with empty acceptance_criteria should pass",
			issueType:          TypeEpic,
			acceptanceCriteria: "",
			shouldPass:         true,
		},
		{
			name:               "epic with valid acceptance_criteria should pass",
			issueType:          TypeEpic,
			acceptanceCriteria: "Optional criteria for epic",
			shouldPass:         true,
		},

		// Chore type - does NOT require acceptance criteria
		{
			name:               "chore with empty acceptance_criteria should pass",
			issueType:          TypeChore,
			acceptanceCriteria: "",
			shouldPass:         true,
		},
		{
			name:               "chore with valid acceptance_criteria should pass",
			issueType:          TypeChore,
			acceptanceCriteria: "Optional criteria for chore",
			shouldPass:         true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			issue := Issue{
				ID:                 "test-id",
				Title:              "Test Issue",
				Description:        "Test description",
				Status:             StatusOpen,
				Priority:           1,
				IssueType:          tt.issueType,
				AcceptanceCriteria: tt.acceptanceCriteria,
				CreatedAt:          now,
				UpdatedAt:          now,
			}

			err := issue.Validate()

			if tt.shouldPass {
				if err != nil {
					t.Errorf("Expected validation to pass, but got error: %v", err)
				}
			} else {
				if err == nil {
					t.Errorf("Expected validation to fail, but got no error")
				} else if tt.errorContains != "" && !contains(err.Error(), tt.errorContains) {
					t.Errorf("Expected error to contain %q, but got: %v", tt.errorContains, err)
				}
			}
		})
	}
}

// TestAcceptanceCriteriaRequiredTypes verifies which issue types require
// acceptance criteria (vc-e3j2)
func TestAcceptanceCriteriaRequiredTypes(t *testing.T) {
	now := time.Now()

	// Issue types that REQUIRE acceptance criteria
	requiredTypes := []IssueType{TypeTask, TypeBug, TypeFeature}

	for _, issueType := range requiredTypes {
		t.Run(string(issueType)+" requires acceptance_criteria", func(t *testing.T) {
			issue := Issue{
				ID:                 "test-id",
				Title:              "Test Issue",
				Description:        "Test description",
				Status:             StatusOpen,
				Priority:           1,
				IssueType:          issueType,
				AcceptanceCriteria: "", // Empty
				CreatedAt:          now,
				UpdatedAt:          now,
			}

			err := issue.Validate()
			if err == nil {
				t.Errorf("Expected validation to fail for %s with empty acceptance_criteria", issueType)
			}
			if !contains(err.Error(), "acceptance_criteria is required") {
				t.Errorf("Expected error about required acceptance_criteria, got: %v", err)
			}
		})
	}

	// Issue types that do NOT require acceptance criteria
	optionalTypes := []IssueType{TypeEpic, TypeChore}

	for _, issueType := range optionalTypes {
		t.Run(string(issueType)+" does not require acceptance_criteria", func(t *testing.T) {
			issue := Issue{
				ID:                 "test-id",
				Title:              "Test Issue",
				Description:        "Test description",
				Status:             StatusOpen,
				Priority:           1,
				IssueType:          issueType,
				AcceptanceCriteria: "", // Empty is OK
				CreatedAt:          now,
				UpdatedAt:          now,
			}

			err := issue.Validate()
			if err != nil {
				t.Errorf("Expected validation to pass for %s with empty acceptance_criteria, got: %v", issueType, err)
			}
		})
	}
}

// contains is a helper to check if a string contains a substring
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 || indexOfSubstring(s, substr) >= 0)
}

// indexOfSubstring finds the index of substr in s, or -1 if not found
func indexOfSubstring(s, substr string) int {
	if len(substr) == 0 {
		return 0
	}
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}
