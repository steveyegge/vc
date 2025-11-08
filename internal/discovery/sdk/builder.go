package sdk

import (
	"time"

	"github.com/steveyegge/vc/internal/discovery"
)

// WorkerResultBuilder provides a fluent API for building WorkerResult objects.
//
// Example:
//
//	result := sdk.NewWorkerResultBuilder("my_worker").
//		WithContext("Analyzed 50 files").
//		WithReasoning("Based on philosophy X").
//		AddIssue(issue1).
//		AddIssue(issue2).
//		Build()
type WorkerResultBuilder struct {
	workerName string
	issues     []discovery.DiscoveredIssue
	context    string
	reasoning  string
	stats      discovery.AnalysisStats
}

// NewWorkerResultBuilder creates a new result builder.
func NewWorkerResultBuilder(workerName string) *WorkerResultBuilder {
	return &WorkerResultBuilder{
		workerName: workerName,
		issues:     []discovery.DiscoveredIssue{},
		stats:      discovery.AnalysisStats{},
	}
}

// WithContext sets the context string (what was examined).
func (b *WorkerResultBuilder) WithContext(context string) *WorkerResultBuilder {
	b.context = context
	return b
}

// WithReasoning sets the reasoning string (why these are potential problems).
func (b *WorkerResultBuilder) WithReasoning(reasoning string) *WorkerResultBuilder {
	b.reasoning = reasoning
	return b
}

// AddIssue adds a discovered issue to the result.
func (b *WorkerResultBuilder) AddIssue(issue discovery.DiscoveredIssue) *WorkerResultBuilder {
	// Set worker name and timestamp if not already set
	if issue.DiscoveredBy == "" {
		issue.DiscoveredBy = b.workerName
	}
	if issue.DiscoveredAt.IsZero() {
		issue.DiscoveredAt = time.Now()
	}

	b.issues = append(b.issues, issue)
	return b
}

// AddIssues adds multiple discovered issues to the result.
func (b *WorkerResultBuilder) AddIssues(issues ...discovery.DiscoveredIssue) *WorkerResultBuilder {
	for _, issue := range issues {
		b.AddIssue(issue)
	}
	return b
}

// WithStats sets the analysis statistics.
func (b *WorkerResultBuilder) WithStats(stats discovery.AnalysisStats) *WorkerResultBuilder {
	b.stats = stats
	return b
}

// IncrementFilesAnalyzed increments the files analyzed counter.
func (b *WorkerResultBuilder) IncrementFilesAnalyzed() *WorkerResultBuilder {
	b.stats.FilesAnalyzed++
	return b
}

// IncrementPatternsFound increments the patterns found counter.
func (b *WorkerResultBuilder) IncrementPatternsFound() *WorkerResultBuilder {
	b.stats.PatternsFound++
	return b
}

// IncrementAICalls increments the AI calls counter.
func (b *WorkerResultBuilder) IncrementAICalls() *WorkerResultBuilder {
	b.stats.AICallsMade++
	return b
}

// AddTokensUsed adds tokens to the usage counter.
func (b *WorkerResultBuilder) AddTokensUsed(tokens int) *WorkerResultBuilder {
	b.stats.TokensUsed += tokens
	return b
}

// AddCost adds to the estimated cost in USD.
func (b *WorkerResultBuilder) AddCost(cost float64) *WorkerResultBuilder {
	b.stats.EstimatedCost += cost
	return b
}

// Build creates the final WorkerResult.
func (b *WorkerResultBuilder) Build() *discovery.WorkerResult {
	// Update final stats
	b.stats.IssuesFound = len(b.issues)

	return &discovery.WorkerResult{
		IssuesDiscovered: b.issues,
		Context:          b.context,
		Reasoning:        b.reasoning,
		AnalyzedAt:       time.Now(),
		Stats:            b.stats,
	}
}

// IssueBuilder provides a fluent API for building DiscoveredIssue objects.
//
// Example:
//
//	issue := sdk.NewIssue().
//		WithTitle("Function too long").
//		WithDescription("Function exceeds 100 lines").
//		WithFile("/path/to/file.go", 42).
//		WithPriority(2).
//		WithTag("complexity").
//		WithConfidence(0.8).
//		Build()
type IssueBuilder struct {
	issue discovery.DiscoveredIssue
}

// NewIssue creates a new issue builder with sensible defaults.
func NewIssue() *IssueBuilder {
	return &IssueBuilder{
		issue: discovery.DiscoveredIssue{
			Type:        "task",
			Priority:    2, // P2 default
			Tags:        []string{},
			Evidence:    make(map[string]interface{}),
			Confidence:  0.7, // Medium-high confidence default
			DiscoveredAt: time.Now(),
		},
	}
}

// WithTitle sets the issue title.
func (b *IssueBuilder) WithTitle(title string) *IssueBuilder {
	b.issue.Title = title
	return b
}

// WithDescription sets the issue description.
func (b *IssueBuilder) WithDescription(description string) *IssueBuilder {
	b.issue.Description = description
	return b
}

// WithCategory sets the issue category (e.g., "architecture", "bugs", "documentation").
func (b *IssueBuilder) WithCategory(category string) *IssueBuilder {
	b.issue.Category = category
	return b
}

// WithType sets the issue type ("bug", "task", "epic").
func (b *IssueBuilder) WithType(issueType string) *IssueBuilder {
	b.issue.Type = issueType
	return b
}

// WithPriority sets the issue priority (0-4 for P0-P4).
func (b *IssueBuilder) WithPriority(priority int) *IssueBuilder {
	b.issue.Priority = priority
	return b
}

// WithTag adds a tag to the issue.
func (b *IssueBuilder) WithTag(tag string) *IssueBuilder {
	b.issue.Tags = append(b.issue.Tags, tag)
	return b
}

// WithTags adds multiple tags to the issue.
func (b *IssueBuilder) WithTags(tags ...string) *IssueBuilder {
	b.issue.Tags = append(b.issue.Tags, tags...)
	return b
}

// WithFile sets the file path and optionally line range.
func (b *IssueBuilder) WithFile(filePath string, lines ...int) *IssueBuilder {
	b.issue.FilePath = filePath
	if len(lines) > 0 {
		b.issue.LineStart = lines[0]
		if len(lines) > 1 {
			b.issue.LineEnd = lines[1]
		} else {
			b.issue.LineEnd = lines[0]
		}
	}
	return b
}

// WithEvidence adds evidence data to the issue.
func (b *IssueBuilder) WithEvidence(key string, value interface{}) *IssueBuilder {
	if b.issue.Evidence == nil {
		b.issue.Evidence = make(map[string]interface{})
	}
	b.issue.Evidence[key] = value
	return b
}

// WithConfidence sets the confidence score (0.0-1.0).
func (b *IssueBuilder) WithConfidence(confidence float64) *IssueBuilder {
	b.issue.Confidence = confidence
	return b
}

// WithDiscoveredBy sets the worker name that discovered this issue.
func (b *IssueBuilder) WithDiscoveredBy(workerName string) *IssueBuilder {
	b.issue.DiscoveredBy = workerName
	return b
}

// Build creates the final DiscoveredIssue.
func (b *IssueBuilder) Build() discovery.DiscoveredIssue {
	return b.issue
}
