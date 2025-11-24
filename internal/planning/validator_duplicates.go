package planning

import (
	"context"
	"fmt"
	"strings"
)

// DuplicateWorkDetector identifies potentially duplicate tasks across phases.
// Duplicate work wastes effort and suggests poor phase organization.
type DuplicateWorkDetector struct{}

// Name returns the validator identifier.
func (d *DuplicateWorkDetector) Name() string {
	return "duplicate_work"
}

// Priority returns 10 (runs after structural checks).
func (d *DuplicateWorkDetector) Priority() int {
	return 10
}

// Validate checks for duplicate or highly similar tasks across different phases.
func (d *DuplicateWorkDetector) Validate(ctx context.Context, plan *MissionPlan, vctx *ValidationContext) ValidationResult {
	result := ValidationResult{
		Errors:   make([]ValidationError, 0),
		Warnings: make([]ValidationWarning, 0),
	}

	// Build a list of all tasks with their locations
	type taskInfo struct {
		task     Task
		phaseID  string
		phaseTitle string
	}
	allTasks := make([]taskInfo, 0)
	for _, phase := range plan.Phases {
		for _, task := range phase.Tasks {
			allTasks = append(allTasks, taskInfo{
				task:       task,
				phaseID:    phase.ID,
				phaseTitle: phase.Title,
			})
		}
	}

	// Compare each pair of tasks for similarity
	for i := 0; i < len(allTasks); i++ {
		for j := i + 1; j < len(allTasks); j++ {
			task1 := allTasks[i]
			task2 := allTasks[j]

			// Skip tasks in the same phase (those are intentional)
			if task1.phaseID == task2.phaseID {
				continue
			}

			similarity := d.calculateSimilarity(task1.task, task2.task)
			if similarity >= 0.8 { // 80% similarity threshold
				result.Warnings = append(result.Warnings, ValidationWarning{
					Code: "POTENTIAL_DUPLICATE",
					Message: fmt.Sprintf(
						"Tasks '%s' (in %s) and '%s' (in %s) appear very similar (%.0f%% match)",
						task1.task.Title, task1.phaseTitle,
						task2.task.Title, task2.phaseTitle,
						similarity*100,
					),
					Location: task1.task.ID,
					Severity: WarningSeverityHigh,
				})
			}
		}
	}

	return result
}

// calculateSimilarity computes a similarity score between two tasks (0.0 to 1.0).
// Uses a simple word-overlap heuristic on titles and descriptions.
func (d *DuplicateWorkDetector) calculateSimilarity(task1, task2 Task) float64 {
	// Normalize and tokenize titles
	words1 := d.tokenize(task1.Title)
	words2 := d.tokenize(task2.Title)

	// Calculate Jaccard similarity for titles (weighted heavily)
	titleSimilarity := d.jaccardSimilarity(words1, words2)

	// If titles are very different, tasks are probably different
	if titleSimilarity < 0.3 {
		return titleSimilarity
	}

	// For borderline cases, also check descriptions
	descWords1 := d.tokenize(task1.Description)
	descWords2 := d.tokenize(task2.Description)
	descSimilarity := d.jaccardSimilarity(descWords1, descWords2)

	// Weighted average: title matters more than description
	return 0.7*titleSimilarity + 0.3*descSimilarity
}

// tokenize converts text into a set of normalized words.
func (d *DuplicateWorkDetector) tokenize(text string) map[string]bool {
	// Convert to lowercase and split on whitespace/punctuation
	words := strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		// Keep only lowercase letters and digits
		return (r < 'a' || r > 'z') && (r < '0' || r > '9')
	})

	// Build word set, filtering out short words
	wordSet := make(map[string]bool)
	stopWords := map[string]bool{
		"the": true, "a": true, "an": true, "and": true, "or": true,
		"but": true, "in": true, "on": true, "at": true, "to": true,
		"for": true, "of": true, "with": true, "by": true, "from": true,
		"is": true, "are": true, "was": true, "were": true, "be": true,
		"been": true, "being": true, "have": true, "has": true, "had": true,
	}

	for _, word := range words {
		if len(word) > 2 && !stopWords[word] {
			wordSet[word] = true
		}
	}

	return wordSet
}

// jaccardSimilarity computes the Jaccard similarity coefficient between two word sets.
// Returns intersection size / union size.
func (d *DuplicateWorkDetector) jaccardSimilarity(set1, set2 map[string]bool) float64 {
	if len(set1) == 0 && len(set2) == 0 {
		return 1.0 // Both empty = identical
	}
	if len(set1) == 0 || len(set2) == 0 {
		return 0.0 // One empty, one not = completely different
	}

	// Count intersection
	intersection := 0
	for word := range set1 {
		if set2[word] {
			intersection++
		}
	}

	// Union size = |set1| + |set2| - |intersection|
	union := len(set1) + len(set2) - intersection

	return float64(intersection) / float64(union)
}
