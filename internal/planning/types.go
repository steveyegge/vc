// Package planning provides types and functionality for mission planning.
package planning

import (
	"time"
)

// PlanStatus represents the lifecycle state of a mission plan.
type PlanStatus string

const (
	// PlanStatusDraft is the initial state when a plan is first created.
	PlanStatusDraft PlanStatus = "draft"

	// PlanStatusRefining indicates the plan is undergoing iterative refinement.
	PlanStatusRefining PlanStatus = "refining"

	// PlanStatusValidated indicates the plan has passed validation checks.
	PlanStatusValidated PlanStatus = "validated"

	// PlanStatusApproved indicates the plan has been approved and is ready for execution.
	PlanStatusApproved PlanStatus = "approved"
)

// MissionPlan represents a complete mission plan with phases and tasks.
type MissionPlan struct {
	// MissionID is the Beads issue ID for the parent mission (e.g., "vc-x7y2").
	MissionID string

	// MissionTitle is the human-readable title of the mission.
	MissionTitle string

	// Goal describes the overall objective of this mission.
	Goal string

	// Constraints lists the non-functional requirements (NFRs) that must be satisfied.
	// Examples: "Tests must pass in <5s", "Zero breaking changes", "Maintain backward compatibility"
	Constraints []string

	// Phases contains the breakdown of work into sequential or parallel phases.
	Phases []Phase

	// TotalTasks is the total number of tasks across all phases.
	TotalTasks int

	// EstimatedHours is the estimated total time to complete the mission.
	EstimatedHours float64

	// Iteration tracks how many refinement cycles this plan has undergone.
	// Starts at 0 for initial generation, increments with each refinement.
	Iteration int

	// Status indicates the current lifecycle state of the plan.
	Status PlanStatus

	// CreatedAt is when the plan was first generated.
	CreatedAt time.Time

	// UpdatedAt is when the plan was last modified.
	UpdatedAt time.Time
}

// Phase represents a logical grouping of related tasks within a mission.
type Phase struct {
	// ID is a unique identifier for this phase within the plan (e.g., "phase-1", "phase-2").
	ID string

	// Title is a short, descriptive name for this phase.
	Title string

	// Description explains the purpose and scope of this phase.
	Description string

	// Strategy describes the approach for completing this phase.
	// Example: "Bottom-up implementation starting with storage layer"
	Strategy string

	// Tasks contains all tasks that belong to this phase.
	Tasks []Task

	// Dependencies lists phase IDs that must be completed before this phase can start.
	// Empty slice means no dependencies (can start immediately).
	Dependencies []string

	// EstimatedHours is the estimated time to complete all tasks in this phase.
	EstimatedHours float64

	// Priority indicates the relative importance of this phase (lower number = higher priority).
	Priority int
}

// Task represents a single unit of work within a phase.
type Task struct {
	// ID is a unique identifier for this task within the plan (e.g., "task-1-1", "task-2-3").
	ID string

	// Title is a short, actionable description of the task.
	Title string

	// Description provides additional context and implementation guidance.
	Description string

	// AcceptanceCriteria defines success criteria in WHEN...THEN... format.
	// Example: "WHEN storage initialized THEN all tables created and indexes in place"
	AcceptanceCriteria []string

	// Dependencies lists task IDs that must be completed before this task can start.
	// Empty slice means no dependencies (can start immediately after phase starts).
	Dependencies []string

	// EstimatedMinutes is the estimated time to complete this task.
	EstimatedMinutes int

	// Priority indicates the relative importance of this task (lower number = higher priority).
	Priority int
}
