package executor

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/steveyegge/vc/internal/ai"
	"github.com/steveyegge/vc/internal/types"
)

// createCodeReviewIssue creates a blocking code review issue for the given commit
//
//nolint:unused // Reserved for future code review feature
func (rp *ResultsProcessor) createCodeReviewIssue(ctx context.Context, parentIssue *types.Issue, commitHash, reasoning string) (string, error) {
	// Create issue title
	title := fmt.Sprintf("Code review: %s", parentIssue.Title)

	// Build detailed description
	description := fmt.Sprintf(`Code review requested by AI supervisor for changes made in %s.

**Original Issue:** %s
**Commit:** %s

**AI Reasoning:**
%s

**Review Instructions:**
1. Review the changes in commit %s
2. Check for correctness, security issues, and code quality
3. Add review comments to this issue
4. Close this issue when review is complete

_This issue was automatically created by AI code review analysis._`,
		parentIssue.ID,
		parentIssue.ID,
		safeShortHash(commitHash),
		reasoning,
		safeShortHash(commitHash))

	// Create the code review issue
	// vc-dk3z: Set acceptance criteria for task issues
	acceptanceCriteria := `- Code review completed
- Review comments added to issue
- All concerns addressed or documented`

	reviewIssue := &types.Issue{
		Title:              title,
		Description:        description,
		IssueType:          types.TypeTask,
		Status:             types.StatusOpen,
		Priority:           1, // P1 - high priority
		AcceptanceCriteria: acceptanceCriteria,
	}

	err := rp.store.CreateIssue(ctx, reviewIssue, "ai-supervisor")
	if err != nil {
		return "", fmt.Errorf("failed to create code review issue: %w", err)
	}

	reviewIssueID := reviewIssue.ID

	// vc-d0r3: Add discovered:supervisor label to VC-filed code review issues
	if err := rp.store.AddLabel(ctx, reviewIssueID, types.LabelDiscoveredSupervisor, "ai-supervisor"); err != nil {
		fmt.Fprintf(os.Stderr, "warning: failed to add %s label to %s: %v\n", types.LabelDiscoveredSupervisor, reviewIssueID, err)
	}

	// Add blocking dependency: parent issue is blocked by review issue
	// This ensures the parent can't be considered "done" until review is complete
	dep := &types.Dependency{
		IssueID:     parentIssue.ID,          // Parent issue
		DependsOnID: reviewIssueID,           // Depends on review issue
		Type:        types.DepBlocks,         // Review blocks parent
	}
	if err := rp.store.AddDependency(ctx, dep, "ai-supervisor"); err != nil {
		// Log warning but don't fail - issue was created successfully
		fmt.Fprintf(os.Stderr, "warning: failed to add blocking dependency %s -> %s: %v\n",
			parentIssue.ID, reviewIssueID, err)
	}

	// Add comment to parent issue about code review
	reviewComment := fmt.Sprintf("Code review issue created: %s\n\nThis issue is now blocked pending code review.", reviewIssueID)
	if err := rp.store.AddComment(ctx, parentIssue.ID, "ai-supervisor", reviewComment); err != nil {
		fmt.Fprintf(os.Stderr, "warning: failed to add code review comment to parent: %v\n", err)
	}

	return reviewIssueID, nil
}

// createQualityIssues creates blocking quality fix issues from automated code analysis (vc-216)
// Each issue represents a specific fix that should be addressed.
// If individual issue creation fails, continues creating remaining issues and collects all errors.
func (rp *ResultsProcessor) createQualityIssues(ctx context.Context, parentIssue *types.Issue, commitHash string, qualityIssues []ai.DiscoveredIssue) ([]string, error) {
	var createdIssues []string
	var errors []error

	for i, qualityIssue := range qualityIssues {
		// Create issue title with commit reference
		title := qualityIssue.Title

		// Build detailed description with commit context
		description := fmt.Sprintf(`Code quality issue identified by automated analysis.

**Original Issue:** %s
**Commit:** %s

%s

_This issue was automatically created by AI code quality analysis (vc-216)._`,
			parentIssue.ID,
			safeShortHash(commitHash),
			qualityIssue.Description)

		// Map priority string to int
		priority := 2 // default P2
		switch qualityIssue.Priority {
		case "P0":
			priority = 0
		case "P1":
			priority = 1
		case "P2":
			priority = 2
		case "P3":
			priority = 3
		}

		// Map type string to types.IssueType
		issueType := types.TypeTask // default
		switch qualityIssue.Type {
		case "bug":
			issueType = types.TypeBug
		case "task":
			issueType = types.TypeTask
		case "chore":
			issueType = types.TypeChore
		case "feature", "enhancement":
			issueType = types.TypeFeature
		}

		// Create the quality fix issue
		// vc-dk3z: Set acceptance criteria from AI-provided criteria, or generate default
		acceptanceCriteria := qualityIssue.AcceptanceCriteria
		if acceptanceCriteria == "" && issueType == types.TypeTask {
			// Generate default acceptance criteria for quality issues if not provided by AI
			acceptanceCriteria = "- Code quality issue fixed\n- Tests updated if needed\n- All quality gates passing"
		}

		fixIssue := &types.Issue{
			Title:              title,
			Description:        description,
			IssueType:          issueType,
			Status:             types.StatusOpen,
			Priority:           priority,
			AcceptanceCriteria: acceptanceCriteria,
		}

		err := rp.store.CreateIssue(ctx, fixIssue, "ai-supervisor")
		if err != nil {
			// Collect error but continue creating remaining issues
			errors = append(errors, fmt.Errorf("failed to create quality fix issue %d (%s): %w", i+1, title, err))
			fmt.Fprintf(os.Stderr, "warning: failed to create quality fix issue %d (%s): %v (continuing with remaining issues)\n", i+1, title, err)
			continue
		}

		fixIssueID := fixIssue.ID
		createdIssues = append(createdIssues, fixIssueID)

		// vc-d0r3: Add discovered:supervisor label to VC-filed quality issues
		if err := rp.store.AddLabel(ctx, fixIssueID, types.LabelDiscoveredSupervisor, "ai-supervisor"); err != nil {
			fmt.Fprintf(os.Stderr, "warning: failed to add %s label to %s: %v\n", types.LabelDiscoveredSupervisor, fixIssueID, err)
		}

		// Add blocking dependency: parent issue is blocked by this fix issue
		// This ensures the parent can't be considered "done" until quality issues are addressed
		dep := &types.Dependency{
			IssueID:     parentIssue.ID,    // Parent issue
			DependsOnID: fixIssueID,        // Depends on fix issue
			Type:        types.DepBlocks,   // Fix blocks parent
		}
		if err := rp.store.AddDependency(ctx, dep, "ai-supervisor"); err != nil {
			// Log warning but don't fail - issue was created successfully
			fmt.Fprintf(os.Stderr, "warning: failed to add blocking dependency %s -> %s: %v\n",
				parentIssue.ID, fixIssueID, err)
		}

		fmt.Printf("  ✓ Created %s (%s, P%d): %s\n", fixIssueID, issueType, priority, title)
	}

	// Add comment to parent issue about quality issues
	if len(createdIssues) > 0 {
		qualityComment := fmt.Sprintf("Automated code quality analysis found %d issues:\n%v\n\nThis issue is now blocked pending quality fixes.",
			len(createdIssues), createdIssues)
		if err := rp.store.AddComment(ctx, parentIssue.ID, "ai-supervisor", qualityComment); err != nil {
			fmt.Fprintf(os.Stderr, "warning: failed to add quality issues comment to parent: %v\n", err)
		}
	}

	// Return any errors that occurred during issue creation
	if len(errors) > 0 {
		// Create a combined error message
		var errMsg strings.Builder
		errMsg.WriteString(fmt.Sprintf("encountered %d errors while creating quality issues:", len(errors)))
		for _, err := range errors {
			errMsg.WriteString(fmt.Sprintf("\n  - %v", err))
		}
		return createdIssues, fmt.Errorf("%s", errMsg.String())
	}

	return createdIssues, nil
}

// createTestIssues creates test improvement issues from test coverage analysis (vc-217)
func (rp *ResultsProcessor) createTestIssues(ctx context.Context, parentIssue *types.Issue, testIssues []ai.DiscoveredIssue) ([]string, error) {
	var createdIssues []string
	var errors []error

	for i, testIssue := range testIssues {
		title := testIssue.Title

		// Build description with parent issue context
		description := fmt.Sprintf(`Test coverage gap identified by automated analysis (vc-217).

**Original Issue:** %s

%s

_This issue was automatically created by AI test coverage analysis._`,
			parentIssue.ID,
			testIssue.Description)

		// Map priority string to int
		priority := 2 // default P2
		switch testIssue.Priority {
		case "P0":
			priority = 0
		case "P1":
			priority = 1
		case "P2":
			priority = 2
		case "P3":
			priority = 3
		}

		// Map type string to types.IssueType
		issueType := types.TypeTask // default for tests
		switch testIssue.Type {
		case "bug":
			issueType = types.TypeBug
		case "task":
			issueType = types.TypeTask
		case "chore":
			issueType = types.TypeChore
		}

		// Create the test improvement issue
		// vc-dk3z: Set acceptance criteria from AI-provided criteria, or generate default
		acceptanceCriteria := testIssue.AcceptanceCriteria
		if acceptanceCriteria == "" && issueType == types.TypeTask {
			// Generate default acceptance criteria for test issues if not provided by AI
			acceptanceCriteria = "- Tests written for uncovered functionality\n- Test coverage verified\n- All tests passing"
		}

		newIssue := &types.Issue{
			Title:              title,
			Description:        description,
			IssueType:          issueType,
			Status:             types.StatusOpen,
			Priority:           priority,
			AcceptanceCriteria: acceptanceCriteria,
		}

		err := rp.store.CreateIssue(ctx, newIssue, "ai-supervisor")
		if err != nil {
			errors = append(errors, fmt.Errorf("failed to create test issue %d (%s): %w", i+1, title, err))
			fmt.Fprintf(os.Stderr, "warning: failed to create test issue %d (%s): %v\n", i+1, title, err)
			continue
		}

		createdIssues = append(createdIssues, newIssue.ID)

		// vc-d0r3: Add discovered:supervisor label to VC-filed test issues
		if err := rp.store.AddLabel(ctx, newIssue.ID, types.LabelDiscoveredSupervisor, "ai-supervisor"); err != nil {
			fmt.Fprintf(os.Stderr, "warning: failed to add %s label to %s: %v\n", types.LabelDiscoveredSupervisor, newIssue.ID, err)
		}

		// Add related dependency (not blocking - these are follow-on improvements)
		dep := &types.Dependency{
			IssueID:     newIssue.ID,             // Test issue
			DependsOnID: parentIssue.ID,          // Related to parent
			Type:        types.DepDiscoveredFrom, // Discovered from parent work
		}
		if err := rp.store.AddDependency(ctx, dep, "ai-supervisor"); err != nil {
			fmt.Fprintf(os.Stderr, "warning: failed to add dependency %s -> %s: %v\n",
				newIssue.ID, parentIssue.ID, err)
		}

		fmt.Printf("  ✓ Created %s (%s, P%d): %s\n", newIssue.ID, issueType, priority, title)
	}

	// Add comment to parent issue about test issues
	if len(createdIssues) > 0 {
		testComment := fmt.Sprintf("Test coverage analysis found %d test gaps and created issues:\n%v",
			len(createdIssues), createdIssues)
		if err := rp.store.AddComment(ctx, parentIssue.ID, "ai-supervisor", testComment); err != nil {
			fmt.Fprintf(os.Stderr, "warning: failed to add test issues comment to parent: %v\n", err)
		}
	}

	if len(errors) > 0 {
		var errMsg strings.Builder
		errMsg.WriteString(fmt.Sprintf("encountered %d errors while creating test issues:", len(errors)))
		for _, err := range errors {
			errMsg.WriteString(fmt.Sprintf("\n  - %v", err))
		}
		return createdIssues, fmt.Errorf("%s", errMsg.String())
	}

	return createdIssues, nil
}
