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

// TestIssueOptionalFields verifies that design, acceptance_criteria, and notes
// are truly optional and issues can be created without them
func TestIssueOptionalFields(t *testing.T) {
	now := time.Now()
	issue := Issue{
		ID:          "test-2",
		Title:       "Minimal Issue",
		Description: "Description only",
		Status:      StatusOpen,
		Priority:    0,
		IssueType:   TypeBug,
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

	// Verify validation passes without optional fields
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
