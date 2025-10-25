package executor

import (
	"context"
	"fmt"

	"github.com/steveyegge/vc/internal/storage"
	"github.com/steveyegge/vc/internal/types"
)

// GetMissionRoot walks the discovered-from chain backwards to find the root issue.
// A mission root is the issue that started the discovery chain (has no discovered-from parent).
//
// Example:
//
//	vc-100 (root) ← discovered vc-101 ← discovered vc-102
//	GetMissionRoot(vc-102) → vc-100
func GetMissionRoot(ctx context.Context, store storage.Storage, issueID string) (*types.Issue, error) {
	currentID := issueID
	visited := make(map[string]bool) // Cycle detection

	for {
		// Prevent infinite loops
		if visited[currentID] {
			return nil, fmt.Errorf("cycle detected in discovered-from chain at %s", currentID)
		}
		visited[currentID] = true

		// Get current issue
		issue, err := store.GetIssue(ctx, currentID)
		if err != nil {
			return nil, fmt.Errorf("failed to get issue %s: %w", currentID, err)
		}

		// Get all dependencies for this issue
		deps, err := store.GetDependencyRecords(ctx, currentID)
		if err != nil {
			return nil, fmt.Errorf("failed to get dependencies for %s: %w", currentID, err)
		}

		// Look for discovered-from dependency
		var discoveredFromID string
		for _, dep := range deps {
			if dep.Type == types.DepDiscoveredFrom {
				discoveredFromID = dep.DependsOnID
				break
			}
		}

		// If no discovered-from parent, this is the root
		if discoveredFromID == "" {
			return issue, nil
		}

		// Continue walking up the chain
		currentID = discoveredFromID
	}
}

// GetMissionDiscoveries recursively gets all issues discovered from a mission root.
// This includes direct discoveries and transitive discoveries (discoveries of discoveries).
//
// Example:
//
//	vc-100 (mission) → discovered vc-101 → discovered vc-102
//	                → discovered vc-103
//	GetMissionDiscoveries(vc-100) → [vc-101, vc-102, vc-103]
func GetMissionDiscoveries(ctx context.Context, store storage.Storage, missionID string) ([]*types.Issue, error) {
	discoveries := make([]*types.Issue, 0)
	visited := make(map[string]bool) // Prevent infinite loops

	// Helper function for recursive traversal
	var collectDiscoveries func(string) error
	collectDiscoveries = func(parentID string) error {
		// Prevent infinite loops
		if visited[parentID] {
			return nil
		}
		visited[parentID] = true

		// Get all issues that depend on this parent
		dependents, err := store.GetDependents(ctx, parentID)
		if err != nil {
			return fmt.Errorf("failed to get dependents for %s: %w", parentID, err)
		}

		// For each dependent, check if it's a discovered-from relationship
		for _, dependent := range dependents {
			// Get dependency records to check the type
			deps, err := store.GetDependencyRecords(ctx, dependent.ID)
			if err != nil {
				return fmt.Errorf("failed to get dependencies for %s: %w", dependent.ID, err)
			}

			// Check if this dependent has a discovered-from relationship to the parent
			hasDiscoveredFrom := false
			for _, dep := range deps {
				if dep.Type == types.DepDiscoveredFrom && dep.DependsOnID == parentID {
					hasDiscoveredFrom = true
					break
				}
			}

			if hasDiscoveredFrom {
				// Add to discoveries list
				discoveries = append(discoveries, dependent)

				// Recursively collect discoveries from this issue
				if err := collectDiscoveries(dependent.ID); err != nil {
					return err
				}
			}
		}

		return nil
	}

	// Start recursive collection from mission root
	if err := collectDiscoveries(missionID); err != nil {
		return nil, err
	}

	return discoveries, nil
}

// HasMissionConverged checks if all discovered issues in a mission are closed.
// Returns true only when all discoveries (and their transitive discoveries) are closed.
//
// Example:
//
//	vc-100 → discovered vc-101 (closed) → discovered vc-102 (closed)
//	HasMissionConverged(vc-100) → true
//
//	vc-100 → discovered vc-101 (closed) → discovered vc-102 (open)
//	HasMissionConverged(vc-100) → false
func HasMissionConverged(ctx context.Context, store storage.Storage, missionID string) (bool, error) {
	discoveries, err := GetMissionDiscoveries(ctx, store, missionID)
	if err != nil {
		return false, fmt.Errorf("failed to get mission discoveries: %w", err)
	}

	// If there are no discoveries yet, mission hasn't converged
	// (Nothing has been discovered, so there's no work to converge)
	if len(discoveries) == 0 {
		return false, nil
	}

	// Check if all discoveries are closed
	for _, issue := range discoveries {
		if issue.Status != types.StatusClosed {
			return false, nil
		}
	}

	return true, nil
}

// CheckMissionExplosion detects runaway discovery when a mission has discovered too many issues.
// Returns true if the mission has more than 20 discoveries (including transitive discoveries).
//
// Example:
//
//	vc-100 → discovered 25 issues
//	CheckMissionExplosion(vc-100) → true (needs human intervention)
//
//	vc-100 → discovered 15 issues
//	CheckMissionExplosion(vc-100) → false (within reasonable bounds)
func CheckMissionExplosion(ctx context.Context, store storage.Storage, missionID string) (bool, error) {
	discoveries, err := GetMissionDiscoveries(ctx, store, missionID)
	if err != nil {
		return false, fmt.Errorf("failed to get mission discoveries: %w", err)
	}

	const explosionThreshold = 20
	return len(discoveries) > explosionThreshold, nil
}
