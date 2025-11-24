package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/fatih/color"
	"github.com/spf13/cobra"
	"github.com/steveyegge/vc/internal/ai"
	"github.com/steveyegge/vc/internal/types"
)

var enhanceCmd = &cobra.Command{
	Use:   "enhance-ac [issue-id]",
	Short: "Enhance acceptance criteria to WHEN...THEN... format",
	Long: `Use AI to convert vague acceptance criteria to concrete WHEN...THEN... scenarios.

This command:
1. Reads the issue's current acceptance criteria
2. Checks if they already follow WHEN...THEN... format
3. If not, uses AI to enhance them with concrete scenarios
4. Updates the issue with improved acceptance criteria

The AI will preserve good criteria and only enhance vague ones.`,
	Args: cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		issueID := args[0]
		dryRun, _ := cmd.Flags().GetBool("dry-run")
		force, _ := cmd.Flags().GetBool("force")

		ctx := context.Background()

		// Fetch the issue
		issue, err := store.GetIssue(ctx, issueID)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		if issue == nil {
			fmt.Fprintf(os.Stderr, "Issue %s not found\n", issueID)
			os.Exit(1)
		}

		// Check if AC already exists
		if issue.AcceptanceCriteria == "" {
			red := color.New(color.FgRed).SprintFunc()
			fmt.Printf("%s Issue %s has no acceptance criteria to enhance\n", red("✗"), issueID)
			os.Exit(1)
		}

		// Check if AC already follows WHEN...THEN... format
		if !force && isWellFormed(issue.AcceptanceCriteria) {
			green := color.New(color.FgGreen).SprintFunc()
			fmt.Printf("%s Issue %s already has well-formed acceptance criteria (WHEN...THEN... format)\n", green("✓"), issueID)
			fmt.Println("\nCurrent acceptance criteria:")
			fmt.Println(issue.AcceptanceCriteria)
			fmt.Println("\nUse --force to enhance anyway")
			return
		}

		// Initialize AI supervisor
		apiKey := os.Getenv("ANTHROPIC_API_KEY")
		if apiKey == "" {
			red := color.New(color.FgRed).SprintFunc()
			fmt.Printf("%s ANTHROPIC_API_KEY not set\n", red("✗"))
			os.Exit(1)
		}

		supervisor, err := ai.NewSupervisor(&ai.Config{
			APIKey: apiKey,
			Store:  store,
		})
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error initializing AI supervisor: %v\n", err)
			os.Exit(1)
		}

		// Enhance the acceptance criteria using AI
		cyan := color.New(color.FgCyan).SprintFunc()
		fmt.Printf("%s Enhancing acceptance criteria for %s using AI...\n", cyan("🤖"), issueID)

		enhanced, err := enhanceAcceptanceCriteria(ctx, supervisor, issue)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error enhancing acceptance criteria: %v\n", err)
			os.Exit(1)
		}

		// Show the enhancement
		fmt.Println("\n" + strings.Repeat("=", 80))
		fmt.Println("ORIGINAL ACCEPTANCE CRITERIA:")
		fmt.Println(strings.Repeat("=", 80))
		fmt.Println(issue.AcceptanceCriteria)
		fmt.Println("\n" + strings.Repeat("=", 80))
		fmt.Println("ENHANCED ACCEPTANCE CRITERIA:")
		fmt.Println(strings.Repeat("=", 80))
		fmt.Println(enhanced)
		fmt.Println(strings.Repeat("=", 80))

		if dryRun {
			yellow := color.New(color.FgYellow).SprintFunc()
			fmt.Printf("\n%s Dry run mode - no changes written\n", yellow("⚠"))
			return
		}

		// Update the issue with enhanced AC
		updates := map[string]interface{}{
			"acceptance_criteria": enhanced,
		}

		if err := store.UpdateIssue(ctx, issueID, updates, actor); err != nil {
			fmt.Fprintf(os.Stderr, "Error updating issue: %v\n", err)
			os.Exit(1)
		}

		green := color.New(color.FgGreen).SprintFunc()
		fmt.Printf("\n%s Enhanced acceptance criteria for %s\n", green("✓"), issueID)
	},
}

func init() {
	enhanceCmd.Flags().BoolP("dry-run", "n", false, "Show enhancement without writing changes")
	enhanceCmd.Flags().BoolP("force", "f", false, "Enhance even if criteria already well-formed")
	rootCmd.AddCommand(enhanceCmd)
}

// isWellFormed checks if acceptance criteria follow WHEN...THEN... format
// Returns true if the criteria contains both "WHEN" and "THEN" keywords
func isWellFormed(ac string) bool {
	upper := strings.ToUpper(ac)
	hasWhen := strings.Contains(upper, "WHEN")
	hasThen := strings.Contains(upper, "THEN")
	return hasWhen && hasThen
}

// enhanceAcceptanceCriteria uses AI to convert vague acceptance criteria to WHEN...THEN... format
func enhanceAcceptanceCriteria(ctx context.Context, supervisor *ai.Supervisor, issue *types.Issue) (string, error) {
	prompt := fmt.Sprintf(`You are enhancing acceptance criteria for an issue to follow the WHEN...THEN... scenario format.

ISSUE DETAILS:
ID: %s
Title: %s
Type: %s
Priority: P%d
Description: %s

CURRENT ACCEPTANCE CRITERIA:
%s

YOUR TASK:
Transform the acceptance criteria into concrete, testable WHEN...THEN... scenarios.

GUIDELINES:
1. Each scenario should follow the format: "WHEN [condition] THEN [expected behavior]"
2. Be specific and concrete - avoid vague terms like "properly", "correctly", "thoroughly"
3. Cover the core functionality and important edge cases
4. Keep scenarios focused and atomic (one behavior per scenario)
5. If the current criteria are already well-formed, you may keep them (but improve clarity if needed)

GOOD EXAMPLES:
- WHEN creating an issue THEN it persists to SQLite database
- WHEN reading non-existent issue THEN NotFoundError is returned
- WHEN transaction fails THEN retry 3 times with exponential backoff
- WHEN executor shuts down gracefully THEN all in-progress work is checkpointed
- WHEN plan validation detects circular dependencies THEN it rejects the plan with clear error

BAD EXAMPLES (too vague):
- Test storage thoroughly
- Handle errors properly
- Make it robust

OUTPUT FORMAT:
Return ONLY the enhanced acceptance criteria as a bulleted list.
Do NOT include any explanations, commentary, or markdown code fences.
Just the criteria, one per line, starting with a dash and space.

ENHANCED ACCEPTANCE CRITERIA:`,
		issue.ID, issue.Title, issue.IssueType, issue.Priority,
		issue.Description, issue.AcceptanceCriteria)

	// Use Haiku model for this simple transformation task (cost-effective)
	model := ai.GetSimpleTaskModel()
	response, err := supervisor.CallAPI(ctx, prompt, model, 2048)
	if err != nil {
		return "", fmt.Errorf("AI call failed: %w", err)
	}

	// Extract text from response
	if len(response.Content) == 0 {
		return "", fmt.Errorf("empty response from AI")
	}

	var enhanced string
	for _, block := range response.Content {
		if block.Type == "text" {
			enhanced += block.Text
		}
	}

	if enhanced == "" {
		return "", fmt.Errorf("no text content in AI response")
	}

	// Clean up the response (trim whitespace)
	enhanced = strings.TrimSpace(enhanced)

	return enhanced, nil
}
