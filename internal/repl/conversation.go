package repl

import (
	"fmt"

	"github.com/fatih/color"
)

// processNaturalLanguage processes natural language input from the REPL
func (r *REPL) processNaturalLanguage(input string) error {
	// Initialize conversation handler if needed
	if r.conversation == nil {
		handler, err := NewConversationHandler(r.store, r.actor)
		if err != nil {
			yellow := color.New(color.FgYellow).SprintFunc()
			fmt.Printf("\n%s AI conversation requires ANTHROPIC_API_KEY environment variable.\n", yellow("Note:"))
			fmt.Println("Set your API key and restart the REPL to enable AI features.")
			fmt.Println()
			return nil
		}
		r.conversation = handler
	}

	// Show thinking indicator
	gray := color.New(color.FgHiBlack).SprintFunc()
	fmt.Printf("%s\n", gray("Thinking..."))

	// Send message to AI
	response, err := r.conversation.SendMessage(r.ctx, input)
	if err != nil {
		return fmt.Errorf("AI conversation failed: %w", err)
	}

	// Display response
	fmt.Println()
	fmt.Println(response)
	fmt.Println()

	return nil
}
