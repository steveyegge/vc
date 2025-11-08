// Package sdk provides tools and utilities for building custom VC discovery workers.
//
// The Custom Worker SDK makes it easy to extend VC's discovery capabilities with
// company-specific standards, language-specific patterns, or domain-specific checks.
//
// # Quick Start
//
// Here's a minimal custom worker:
//
//	type MyWorker struct {}
//
//	func (w *MyWorker) Name() string {
//		return "my_custom_worker"
//	}
//
//	func (w *MyWorker) Philosophy() string {
//		return "My guiding principle for code quality"
//	}
//
//	func (w *MyWorker) Scope() string {
//		return "What this worker analyzes"
//	}
//
//	func (w *MyWorker) Cost() health.CostEstimate {
//		return health.CostEstimate{
//			EstimatedDuration: 30 * time.Second,
//			AICallsEstimated:  5,
//			RequiresFullScan:  true,
//			Category:          health.CostModerate,
//		}
//	}
//
//	func (w *MyWorker) Dependencies() []string {
//		return nil
//	}
//
//	func (w *MyWorker) Analyze(ctx context.Context, codebase health.CodebaseContext) (*discovery.WorkerResult, error) {
//		// Use SDK helpers to build your analysis
//		return &discovery.WorkerResult{
//			IssuesDiscovered: []discovery.DiscoveredIssue{},
//		}, nil
//	}
//
// # SDK Components
//
// The SDK provides several helper packages:
//
//   - ast: Go AST parsing and traversal helpers
//   - pattern: Pattern matching and code search utilities
//   - ai: AI supervision integration (call Claude for assessments)
//   - issue: Issue builder with sensible defaults
//
// # Example: Simple Pattern Matching Worker
//
//	func (w *MyWorker) Analyze(ctx context.Context, codebase health.CodebaseContext) (*discovery.WorkerResult, error) {
//		result := sdk.NewWorkerResultBuilder(w.Name())
//
//		// Find all TODO comments
//		matches, _ := sdk.FindPattern(codebase.RootPath, `TODO:.*`, sdk.PatternOptions{
//			FilePattern: "*.go",
//			ExcludeDirs: []string{"vendor", ".git"},
//		})
//
//		for _, match := range matches {
//			result.AddIssue(sdk.NewIssue().
//				WithTitle(fmt.Sprintf("TODO found: %s", match.Text)).
//				WithFile(match.File, match.Line).
//				WithPriority(3).
//				Build())
//		}
//
//		return result.Build(), nil
//	}
//
// # Example: AST-Based Worker
//
//	func (w *MyWorker) Analyze(ctx context.Context, codebase health.CodebaseContext) (*discovery.WorkerResult, error) {
//		result := sdk.NewWorkerResultBuilder(w.Name())
//
//		// Find all functions longer than 100 lines
//		sdk.WalkGoFiles(codebase.RootPath, func(file *sdk.GoFile) error {
//			for _, fn := range file.Functions() {
//				if fn.LineCount() > 100 {
//					result.AddIssue(sdk.NewIssue().
//						WithTitle(fmt.Sprintf("Long function: %s (%d lines)", fn.Name, fn.LineCount())).
//						WithFile(file.Path, fn.StartLine).
//						WithPriority(2).
//						WithTag("complexity").
//						Build())
//				}
//			}
//			return nil
//		})
//
//		return result.Build(), nil
//	}
//
// # Example: AI-Powered Worker
//
//	func (w *MyWorker) Analyze(ctx context.Context, codebase health.CodebaseContext) (*discovery.WorkerResult, error) {
//		result := sdk.NewWorkerResultBuilder(w.Name())
//
//		// Find all API handlers
//		handlers := findAPIHandlers(codebase)
//
//		// Ask AI to assess security
//		assessment, _ := sdk.CallAI(ctx, sdk.AIRequest{
//			Prompt: fmt.Sprintf("Analyze these API handlers for security issues:\n%s", handlers),
//			Model:  "claude-sonnet-4-5-20250929",
//		})
//
//		// Parse AI response and create issues
//		result.AddIssue(sdk.NewIssue().
//			WithTitle("Security: " + assessment.Summary).
//			WithDescription(assessment.Details).
//			Build())
//
//		return result.Build(), nil
//	}
//
// # Testing Custom Workers
//
// The SDK provides test helpers:
//
//	func TestMyWorker(t *testing.T) {
//		worker := &MyWorker{}
//		result, err := sdk.TestWorker(t, worker, "testdata/sample_project")
//		require.NoError(t, err)
//		assert.Greater(t, len(result.IssuesDiscovered), 0)
//	}
//
// # Loading Custom Workers
//
// Workers can be loaded from:
//
//   - Project directory: .vc/workers/
//   - User directory: ~/.vc/workers/
//   - External plugins: Go .so files
//
// See the documentation for each package for detailed usage examples.
package sdk
