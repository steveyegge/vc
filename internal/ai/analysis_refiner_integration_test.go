package ai

import (
	"context"
	"os"
	"testing"

	"github.com/steveyegge/vc/internal/iterative"
	"github.com/steveyegge/vc/internal/types"
)

// Integration tests for AnalysisRefiner.CheckConvergence with real AI API calls.
// These tests require ANTHROPIC_API_KEY to be set in the environment.
//
// Run with: go test -v -tags=integration ./internal/ai/

// skipIfNoAPIKey skips the test if ANTHROPIC_API_KEY is not set
func skipIfNoAPIKey(t *testing.T) {
	if os.Getenv("ANTHROPIC_API_KEY") == "" {
		t.Skip("Skipping integration test: ANTHROPIC_API_KEY not set")
	}
}

// TestCheckConvergence_IdenticalArtifacts tests convergence with identical content
func TestCheckConvergence_IdenticalArtifacts(t *testing.T) {
	skipIfNoAPIKey(t)

	supervisor, err := createTestSupervisor(t)
	if err != nil {
		t.Skipf("Failed to create supervisor: %v", err)
	}

	issue := &types.Issue{
		ID:                 "test-conv-1",
		Title:              "Test Issue",
		Description:        "Test description",
		AcceptanceCriteria: "Should work",
	}

	refiner, err := NewAnalysisRefiner(supervisor, issue, "test output", true)
	if err != nil {
		t.Fatalf("Failed to create refiner: %v", err)
	}

	// Create identical artifacts
	content := `Completed: true
Confidence: 0.95

Scope Validation:
  On Task: true
  Explanation: Agent completed the requested task

Acceptance Criteria:
  criterion_1: met=true (evidence: Work completed successfully)

Summary: All work completed successfully`

	previous := &iterative.Artifact{
		Type:    "analysis",
		Content: content,
		Context: "Initial analysis",
	}

	current := &iterative.Artifact{
		Type:    "analysis",
		Content: content,
		Context: "Second iteration found no new issues",
	}

	ctx := context.Background()
	decision, err := refiner.CheckConvergence(ctx, current, previous)
	if err != nil {
		t.Fatalf("CheckConvergence failed: %v", err)
	}

	// Identical content should converge
	if !decision.Converged {
		t.Errorf("Expected convergence for identical artifacts, got: %+v", decision)
	}
}

// TestCheckConvergence_MinimalChange tests convergence with minimal rewording
func TestCheckConvergence_MinimalChange(t *testing.T) {
	skipIfNoAPIKey(t)

	supervisor, err := createTestSupervisor(t)
	if err != nil {
		t.Skipf("Failed to create supervisor: %v", err)
	}

	issue := &types.Issue{
		ID:                 "test-conv-2",
		Title:              "Test Issue",
		Description:        "Test description",
		AcceptanceCriteria: "Should work",
	}

	refiner, err := NewAnalysisRefiner(supervisor, issue, "test output", true)
	if err != nil {
		t.Fatalf("Failed to create refiner: %v", err)
	}

	previous := &iterative.Artifact{
		Type: "analysis",
		Content: `Completed: true
Confidence: 0.95

Summary: The agent successfully completed the task`,
		Context: "Initial analysis",
	}

	current := &iterative.Artifact{
		Type: "analysis",
		Content: `Completed: true
Confidence: 0.95

Summary: The agent successfully finished the task`,
		Context: "Second iteration - minor rewording only",
	}

	ctx := context.Background()
	decision, err := refiner.CheckConvergence(ctx, current, previous)
	if err != nil {
		t.Fatalf("CheckConvergence failed: %v", err)
	}

	// Minimal rewording should converge
	if !decision.Converged {
		t.Error("Expected convergence for minimal rewording")
	}
}

// TestCheckConvergence_NewIssuesDiscovered tests non-convergence when new issues found
func TestCheckConvergence_NewIssuesDiscovered(t *testing.T) {
	skipIfNoAPIKey(t)

	supervisor, err := createTestSupervisor(t)
	if err != nil {
		t.Skipf("Failed to create supervisor: %v", err)
	}

	issue := &types.Issue{
		ID:                 "test-conv-3",
		Title:              "Test Issue",
		Description:        "Test description",
		AcceptanceCriteria: "Should work",
	}

	refiner, err := NewAnalysisRefiner(supervisor, issue, "test output", true)
	if err != nil {
		t.Fatalf("Failed to create refiner: %v", err)
	}

	previous := &iterative.Artifact{
		Type: "analysis",
		Content: `Completed: true
Confidence: 0.95

Discovered Issues (1):
  1. Add missing tests
     Type: task, Priority: P2, Discovery: related
     Description: Tests are missing for the new feature

Summary: Work completed but missing tests`,
		Context: "Initial analysis found 1 issue",
	}

	current := &iterative.Artifact{
		Type: "analysis",
		Content: `Completed: true
Confidence: 0.95

Discovered Issues (3):
  1. Add missing tests
     Type: task, Priority: P2, Discovery: related
     Description: Tests are missing for the new feature
  2. Fix error handling
     Type: bug, Priority: P1, Discovery: blocker
     Description: Error handling is incomplete in edge cases
  3. Add documentation
     Type: task, Priority: P3, Discovery: related
     Description: Public API lacks documentation

Summary: Work completed but found additional issues in review`,
		Context: "Second iteration found 2 new issues",
	}

	ctx := context.Background()
	decision, err := refiner.CheckConvergence(ctx, current, previous)
	if err != nil {
		t.Fatalf("CheckConvergence failed: %v", err)
	}

	// Finding new issues should NOT converge
	if decision.Converged {
		t.Error("Expected non-convergence when new issues are discovered")
	}
}

// TestCheckConvergence_EmptyDiff tests convergence with completely empty diffs
func TestCheckConvergence_EmptyDiff(t *testing.T) {
	skipIfNoAPIKey(t)

	supervisor, err := createTestSupervisor(t)
	if err != nil {
		t.Skipf("Failed to create supervisor: %v", err)
	}

	issue := &types.Issue{
		ID:                 "test-conv-4",
		Title:              "Test Issue",
		Description:        "Test description",
		AcceptanceCriteria: "Should work",
	}

	refiner, err := NewAnalysisRefiner(supervisor, issue, "test output", true)
	if err != nil {
		t.Fatalf("Failed to create refiner: %v", err)
	}

	// Completely empty artifacts
	previous := &iterative.Artifact{
		Type:    "analysis",
		Content: "",
		Context: "",
	}

	current := &iterative.Artifact{
		Type:    "analysis",
		Content: "",
		Context: "",
	}

	ctx := context.Background()
	decision, err := refiner.CheckConvergence(ctx, current, previous)
	if err != nil {
		t.Fatalf("CheckConvergence failed: %v", err)
	}

	// Empty diffs should converge (nothing changing)
	if !decision.Converged {
		t.Error("Expected convergence for empty diffs")
	}
}

// TestCheckConvergence_LargeDiff tests convergence behavior with large changes
func TestCheckConvergence_LargeDiff(t *testing.T) {
	skipIfNoAPIKey(t)

	supervisor, err := createTestSupervisor(t)
	if err != nil {
		t.Skipf("Failed to create supervisor: %v", err)
	}

	issue := &types.Issue{
		ID:                 "test-conv-5",
		Title:              "Test Issue",
		Description:        "Test description",
		AcceptanceCriteria: "Should work",
	}

	refiner, err := NewAnalysisRefiner(supervisor, issue, "test output", true)
	if err != nil {
		t.Fatalf("Failed to create refiner: %v", err)
	}

	previous := &iterative.Artifact{
		Type: "analysis",
		Content: `Completed: false
Confidence: 0.50

Summary: Initial incomplete analysis`,
		Context: "First iteration",
	}

	current := &iterative.Artifact{
		Type: "analysis",
		Content: `Completed: true
Confidence: 0.95

Scope Validation:
  On Task: true
  Explanation: Agent completed all requested work

Acceptance Criteria:
  criterion_1: met=true (evidence: Feature implemented)
  criterion_2: met=true (evidence: Tests passing)
  criterion_3: met=true (evidence: Documentation added)

Discovered Issues (5):
  1. Optimize database queries
     Type: enhancement, Priority: P2, Discovery: related
     Description: Database queries could be more efficient
  2. Add caching layer
     Type: enhancement, Priority: P3, Discovery: background
     Description: Caching would improve performance
  3. Refactor error handling
     Type: task, Priority: P2, Discovery: related
     Description: Error handling could be more consistent
  4. Add integration tests
     Type: task, Priority: P1, Discovery: blocker
     Description: Integration tests are missing
  5. Update API documentation
     Type: task, Priority: P3, Discovery: related
     Description: API docs need updating

Quality Issues (3):
  1. Missing test coverage in module X
  2. Lint warning in file Y
  3. Missing godoc comments

Summary: Comprehensive analysis found significant new work items`,
		Context: "Second iteration - major expansion of analysis",
	}

	ctx := context.Background()
	decision, err := refiner.CheckConvergence(ctx, current, previous)
	if err != nil {
		t.Fatalf("CheckConvergence failed: %v", err)
	}

	// Large diff with many new issues should NOT converge
	if decision.Converged {
		t.Error("Expected non-convergence for large diff with many new issues")
	}
}

// TestCheckConvergence_SemanticOnlyChange tests convergence with semantic but not syntactic changes
func TestCheckConvergence_SemanticOnlyChange(t *testing.T) {
	skipIfNoAPIKey(t)

	supervisor, err := createTestSupervisor(t)
	if err != nil {
		t.Skipf("Failed to create supervisor: %v", err)
	}

	issue := &types.Issue{
		ID:                 "test-conv-6",
		Title:              "Test Issue",
		Description:        "Test description",
		AcceptanceCriteria: "Should work",
	}

	refiner, err := NewAnalysisRefiner(supervisor, issue, "test output", true)
	if err != nil {
		t.Fatalf("Failed to create refiner: %v", err)
	}

	previous := &iterative.Artifact{
		Type: "analysis",
		Content: `Completed: true
Confidence: 0.95

Discovered Issues (2):
  1. Add unit tests for feature X
     Type: task, Priority: P2, Discovery: related
     Description: Feature X lacks test coverage
  2. Fix logging in module Y
     Type: bug, Priority: P3, Discovery: related
     Description: Logging is too verbose

Summary: Work completed with 2 follow-on tasks`,
		Context: "Initial analysis",
	}

	current := &iterative.Artifact{
		Type: "analysis",
		Content: `Completed: true
Confidence: 0.95

Discovered Issues (2):
  1. Improve test coverage for feature X
     Type: task, Priority: P2, Discovery: related
     Description: Feature X needs additional unit tests
  2. Adjust logging verbosity in module Y
     Type: bug, Priority: P3, Discovery: related
     Description: Module Y logs too much information

Summary: Task completed successfully with 2 related follow-up items`,
		Context: "Second iteration - same issues, different wording",
	}

	ctx := context.Background()
	decision, err := refiner.CheckConvergence(ctx, current, previous)
	if err != nil {
		t.Fatalf("CheckConvergence failed: %v", err)
	}

	// Same semantic content (2 issues, same priorities) should converge
	if !decision.Converged {
		t.Error("Expected convergence for semantic-only changes (rewording same issues)")
	}
}

// TestCheckConvergence_ConfidenceThreshold tests that low confidence results are handled
func TestCheckConvergence_ConfidenceThreshold(t *testing.T) {
	skipIfNoAPIKey(t)

	supervisor, err := createTestSupervisor(t)
	if err != nil {
		t.Skipf("Failed to create supervisor: %v", err)
	}

	issue := &types.Issue{
		ID:                 "test-conv-7",
		Title:              "Ambiguous Issue",
		Description:        "This is a very ambiguous task description",
		AcceptanceCriteria: "Unclear criteria",
	}

	refiner, err := NewAnalysisRefiner(supervisor, issue, "unclear output", true)
	if err != nil {
		t.Fatalf("Failed to create refiner: %v", err)
	}

	// Create artifacts with very little difference - edge case for convergence
	previous := &iterative.Artifact{
		Type: "analysis",
		Content: `Completed: maybe
Confidence: 0.60

Summary: Unclear if work is complete`,
		Context: "Ambiguous first iteration",
	}

	current := &iterative.Artifact{
		Type: "analysis",
		Content: `Completed: probably
Confidence: 0.65

Summary: Still unclear if work is complete`,
		Context: "Ambiguous second iteration",
	}

	ctx := context.Background()
	decision, err := refiner.CheckConvergence(ctx, current, previous)
	if err != nil {
		t.Fatalf("CheckConvergence failed: %v", err)
	}

	// This test validates that the AI can make a judgment even in ambiguous cases.
	// We don't assert the result (could go either way), but we verify no error.
	t.Logf("Convergence judgment for ambiguous case: %+v", decision)
}

// TestCheckConvergence_JSONParsing tests that AI returns valid JSON
func TestCheckConvergence_JSONParsing(t *testing.T) {
	skipIfNoAPIKey(t)

	supervisor, err := createTestSupervisor(t)
	if err != nil {
		t.Skipf("Failed to create supervisor: %v", err)
	}

	issue := &types.Issue{
		ID:                 "test-conv-8",
		Title:              "Test Issue",
		Description:        "Test description",
		AcceptanceCriteria: "Should work",
	}

	refiner, err := NewAnalysisRefiner(supervisor, issue, "test output", true)
	if err != nil {
		t.Fatalf("Failed to create refiner: %v", err)
	}

	previous := &iterative.Artifact{
		Type:    "analysis",
		Content: "Previous content",
		Context: "Context",
	}

	current := &iterative.Artifact{
		Type:    "analysis",
		Content: "Current content",
		Context: "Context",
	}

	ctx := context.Background()
	_, err = refiner.CheckConvergence(ctx, current, previous)

	// The key test: We should NOT get a JSON parsing error
	if err != nil {
		t.Errorf("CheckConvergence should return valid parseable JSON, got error: %v", err)
	}
}

// TestCheckConvergence_PromptQuality tests that the convergence prompt produces good AI responses
func TestCheckConvergence_PromptQuality(t *testing.T) {
	skipIfNoAPIKey(t)

	supervisor, err := createTestSupervisor(t)
	if err != nil {
		t.Skipf("Failed to create supervisor: %v", err)
	}

	issue := &types.Issue{
		ID:                 "test-conv-9",
		Title:              "Complex Issue",
		Description:        "A complex task with multiple parts",
		AcceptanceCriteria: "1. Part A\n2. Part B\n3. Part C",
	}

	refiner, err := NewAnalysisRefiner(supervisor, issue, "complex agent output with multiple findings", true)
	if err != nil {
		t.Fatalf("Failed to create refiner: %v", err)
	}

	previous := &iterative.Artifact{
		Type: "analysis",
		Content: `Completed: true
Confidence: 0.90

Discovered Issues (2):
  1. Issue A
  2. Issue B

Summary: Found 2 issues`,
		Context: "First pass",
	}

	current := &iterative.Artifact{
		Type: "analysis",
		Content: `Completed: true
Confidence: 0.95

Discovered Issues (4):
  1. Issue A
  2. Issue B
  3. Issue C
  4. Issue D

Summary: Found 2 additional issues on review`,
		Context: "Second pass found more issues",
	}

	ctx := context.Background()
	decision, err := refiner.CheckConvergence(ctx, current, previous)
	if err != nil {
		t.Fatalf("CheckConvergence failed: %v", err)
	}

	// When we find 2 new issues (C and D), AI should recognize non-convergence
	if decision.Converged {
		t.Error("Expected non-convergence when 2 new issues were discovered")
	}
}
