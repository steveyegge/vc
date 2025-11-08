package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/steveyegge/vc/internal/discovery"
	"github.com/steveyegge/vc/internal/health"
)

func main() {
	if len(os.Args) < 3 {
		fmt.Fprintf(os.Stderr, "Usage: %s <project-path> <worker-name>\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "Example: %s ~/src/go/hugo architecture\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "Workers: architecture, bugs\n")
		os.Exit(1)
	}

	projectPath := os.Args[1]
	workerName := os.Args[2]

	// Expand ~ to home directory
	if projectPath[:2] == "~/" {
		home, _ := os.UserHomeDir()
		projectPath = filepath.Join(home, projectPath[2:])
	}

	// Verify project exists
	if _, err := os.Stat(projectPath); err != nil {
		fmt.Fprintf(os.Stderr, "Error: project path does not exist: %s\n", projectPath)
		os.Exit(1)
	}

	fmt.Printf("Testing discovery worker on external project\n")
	fmt.Printf("Project: %s\n", projectPath)
	fmt.Printf("Worker: %s\n", workerName)
	fmt.Println()

	ctx := context.Background()

	// Create worker
	var worker discovery.DiscoveryWorker
	switch workerName {
	case "architecture":
		worker = discovery.NewArchitectureWorker()
	case "bugs":
		worker = discovery.NewBugHunterWorker()
	default:
		fmt.Fprintf(os.Stderr, "Unknown worker: %s\n", workerName)
		os.Exit(1)
	}

	fmt.Printf("Worker Info:\n")
	fmt.Printf("  Name: %s\n", worker.Name())
	fmt.Printf("  Philosophy: %s\n", worker.Philosophy())
	fmt.Printf("  Scope: %s\n", worker.Scope())
	fmt.Println()

	// Build codebase context
	fmt.Println("Building codebase context...")
	codebaseCtx := health.CodebaseContext{
		RootPath: projectPath,
	}

	// Run analysis
	fmt.Printf("Running %s worker...\n", worker.Name())
	startTime := time.Now()

	result, err := worker.Analyze(ctx, codebaseCtx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	elapsed := time.Since(startTime)

	// Print summary
	fmt.Println()
	fmt.Printf("=== RESULTS ===\n")
	fmt.Printf("Duration: %v\n", elapsed)
	fmt.Printf("Files analyzed: %d\n", result.Stats.FilesAnalyzed)
	fmt.Printf("Issues discovered: %d\n", len(result.IssuesDiscovered))
	fmt.Printf("Patterns found: %d\n", result.Stats.PatternsFound)
	if result.Stats.ErrorsIgnored > 0 {
		fmt.Printf("Errors ignored: %d\n", result.Stats.ErrorsIgnored)
	}
	fmt.Println()

	// Print issue breakdown by category
	categoryCount := make(map[string]int)
	for _, issue := range result.IssuesDiscovered {
		categoryCount[issue.Category]++
	}

	fmt.Printf("Issues by category:\n")
	for category, count := range categoryCount {
		fmt.Printf("  %s: %d\n", category, count)
	}
	fmt.Println()

	// Print first 10 issues
	fmt.Printf("Sample issues (first 10):\n")
	for i, issue := range result.IssuesDiscovered {
		if i >= 10 {
			break
		}
		fmt.Printf("\n[%d] %s\n", i+1, issue.Title)
		fmt.Printf("    Category: %s | Type: %s | Priority: P%d | Confidence: %.1f\n",
			issue.Category, issue.Type, issue.Priority, issue.Confidence)
		if issue.FilePath != "" {
			relPath, _ := filepath.Rel(projectPath, issue.FilePath)
			if relPath != "" {
				fmt.Printf("    File: %s", relPath)
				if issue.LineStart > 0 {
					fmt.Printf(":%d", issue.LineStart)
				}
				fmt.Println()
			}
		}
		// Print first 150 chars of description
		desc := issue.Description
		if len(desc) > 150 {
			desc = desc[:150] + "..."
		}
		fmt.Printf("    %s\n", desc)
	}

	// Save full results to JSON file
	outputFile := fmt.Sprintf("discovery_%s_%s.json", filepath.Base(projectPath), worker.Name())
	jsonData, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error marshaling JSON: %v\n", err)
		os.Exit(1)
	}

	if err := os.WriteFile(outputFile, jsonData, 0644); err != nil {
		fmt.Fprintf(os.Stderr, "Error writing output file: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("\n✓ Full results saved to: %s\n", outputFile)
}
