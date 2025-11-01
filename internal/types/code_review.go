package types

import "time"

// ReviewCheckpoint tracks when the last code review sweep occurred
// Used to calculate git diff metrics since last review
type ReviewCheckpoint struct {
	CommitSHA   string    // Last reviewed commit
	Timestamp   time.Time // When the review was performed
	ReviewScope string    // "quick" | "thorough" | "targeted:path/to/dir"
}

// ReviewDecisionRequest contains git metrics for AI to decide if review is needed
type ReviewDecisionRequest struct {
	// Git diff stats since last checkpoint
	LinesAdded      int
	LinesDeleted    int
	FilesChanged    int
	HeavyChurnAreas []string
	DaysSinceReview int

	// Codebase context
	TotalLOC int

	// Last review findings
	LastReviewSummary string
}

// ReviewMetricsResult contains both the metrics and the commit SHA used for calculation
// This prevents race conditions between metrics gathering and checkpoint saving
type ReviewMetricsResult struct {
	Metrics   *ReviewDecisionRequest
	CommitSHA string // The actual commit SHA (not "HEAD") used for diff calculations
}

// ReviewDecision represents AI decision about triggering a code review sweep
type ReviewDecision struct {
	ShouldReview    bool     `json:"should_review"`    // Should we trigger a code review now?
	Reasoning       string   `json:"reasoning"`        // Detailed reasoning for the decision
	Scope           string   `json:"scope"`            // "quick" | "thorough" | "targeted"
	TargetAreas     []string `json:"target_areas"`     // Specific directories/packages to review (null = broad)
	EstimatedFiles  int      `json:"estimated_files"`  // Estimated number of files to review (5-15)
	EstimatedCost   string   `json:"estimated_cost"`   // Estimated cost (e.g., "$1-5")
}

// FileReviewResult represents the AI review of a single file
type FileReviewResult struct {
	FilePath string              `json:"file_path"` // Path to reviewed file
	Issues   []FileReviewIssue   `json:"issues"`    // Issues found (0-3 per file)
}

// FileReviewIssue represents a single issue found during file review
type FileReviewIssue struct {
	Type        string `json:"type"`        // 'efficiency' | 'bug' | 'pattern' | 'best_practice' | 'other'
	Severity    string `json:"severity"`    // 'low' | 'medium' | 'high'
	Location    string `json:"location"`    // 'file.go:45-67'
	Title       string `json:"title"`       // Short description
	Description string `json:"description"` // Detailed explanation
	Suggestion  string `json:"suggestion"`  // How to fix
	Priority    string `json:"priority"`    // 'P0' | 'P1' | 'P2' | 'P3'
}
