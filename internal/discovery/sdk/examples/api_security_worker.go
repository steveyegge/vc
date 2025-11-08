package examples

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/steveyegge/vc/internal/discovery"
	"github.com/steveyegge/vc/internal/discovery/sdk"
	"github.com/steveyegge/vc/internal/health"
)

// APISecurityWorker uses AI to analyze API handlers for security issues.
// This is an example custom worker showing AI-powered analysis.
//
// Philosophy: "AI-powered security analysis catches subtle vulnerabilities that static analysis misses"
//
// Example usage:
//
//	worker := examples.NewAPISecurityWorker()
//	result, err := worker.Analyze(ctx, codebase)
type APISecurityWorker struct {
	// MaxHandlersToAnalyze limits how many handlers to check (cost control)
	MaxHandlersToAnalyze int

	// SecurityFocus specifies what to look for (e.g., "SQL injection", "XSS")
	SecurityFocus []string
}

// NewAPISecurityWorker creates an API security worker.
func NewAPISecurityWorker() *APISecurityWorker {
	return &APISecurityWorker{
		MaxHandlersToAnalyze: 20, // Limit to control costs
		SecurityFocus: []string{
			"SQL injection",
			"XSS (cross-site scripting)",
			"Authentication bypass",
			"Authorization issues",
			"Input validation",
			"Sensitive data exposure",
		},
	}
}

// Name implements DiscoveryWorker.
func (w *APISecurityWorker) Name() string {
	return "api_security"
}

// Philosophy implements DiscoveryWorker.
func (w *APISecurityWorker) Philosophy() string {
	return "AI-powered security analysis catches subtle vulnerabilities that static analysis misses"
}

// Scope implements DiscoveryWorker.
func (w *APISecurityWorker) Scope() string {
	return "HTTP handlers, API endpoints, authentication, authorization, input validation"
}

// Cost implements DiscoveryWorker.
func (w *APISecurityWorker) Cost() health.CostEstimate {
	return health.CostEstimate{
		EstimatedDuration: 2 * time.Minute,
		AICallsEstimated:  w.MaxHandlersToAnalyze / 5, // Batch 5 handlers per AI call
		RequiresFullScan:  true,
		Category:          health.CostExpensive,
	}
}

// Dependencies implements DiscoveryWorker.
func (w *APISecurityWorker) Dependencies() []string {
	return nil
}

// Analyze implements DiscoveryWorker.
func (w *APISecurityWorker) Analyze(ctx context.Context, codebase health.CodebaseContext) (*discovery.WorkerResult, error) {
	result := sdk.NewWorkerResultBuilder(w.Name()).
		WithContext(fmt.Sprintf("Analyzing API handlers for security vulnerabilities in %s", codebase.RootPath)).
		WithReasoning(fmt.Sprintf("Based on philosophy: '%s'\n\nSecurity vulnerabilities in API handlers can lead to data breaches, unauthorized access, and system compromise.", w.Philosophy()))

	// Find API handler functions
	handlers := w.findAPIHandlers(codebase)

	// Limit to max handlers
	if len(handlers) > w.MaxHandlersToAnalyze {
		handlers = handlers[:w.MaxHandlersToAnalyze]
	}

	// Batch handlers for efficient AI analysis
	batchSize := 5
	for i := 0; i < len(handlers); i += batchSize {
		end := i + batchSize
		if end > len(handlers) {
			end = len(handlers)
		}

		batch := handlers[i:end]
		w.analyzeBatch(ctx, batch, result)
	}

	return result.Build(), nil
}

// handlerInfo contains information about an API handler.
type handlerInfo struct {
	Name     string
	File     string
	Line     int
	Code     string
	Route    string // HTTP route (if detected)
	Method   string // HTTP method (if detected)
}

// findAPIHandlers identifies functions that look like HTTP handlers.
func (w *APISecurityWorker) findAPIHandlers(codebase health.CodebaseContext) []handlerInfo {
	var handlers []handlerInfo

	// Walk Go files looking for HTTP handler patterns
	sdk.WalkGoFiles(codebase.RootPath, func(file *sdk.GoFile) error {
		for _, fn := range file.Functions() {
			// Check if function signature matches http.HandlerFunc
			// func(w http.ResponseWriter, r *http.Request)
			params := fn.Parameters()

			if len(params) == 2 &&
				strings.Contains(params[0].Type, "ResponseWriter") &&
				strings.Contains(params[1].Type, "Request") {

				// Extract function code (simplified - would need more robust extraction)
				handlers = append(handlers, handlerInfo{
					Name: fn.Name(),
					File: file.Path,
					Line: fn.StartLine(),
					Code: fmt.Sprintf("// Handler at %s:%d\nfunc %s(...) { ... }", file.Path, fn.StartLine(), fn.Name()),
				})
			}

			// Also check for functions with "Handler" in the name
			if strings.Contains(fn.Name(), "Handler") || strings.Contains(fn.Name(), "handle") {
				handlers = append(handlers, handlerInfo{
					Name: fn.Name(),
					File: file.Path,
					Line: fn.StartLine(),
					Code: fmt.Sprintf("// Handler at %s:%d\nfunc %s(...) { ... }", file.Path, fn.StartLine(), fn.Name()),
				})
			}
		}
		return nil
	})

	return handlers
}

// analyzeBatch analyzes a batch of handlers using AI.
func (w *APISecurityWorker) analyzeBatch(ctx context.Context, handlers []handlerInfo, result *sdk.WorkerResultBuilder) {
	// Build code snippets for AI
	snippets := make([]sdk.CodeSnippet, len(handlers))
	for i, handler := range handlers {
		snippets[i] = sdk.CodeSnippet{
			ID:      handler.Name,
			Code:    handler.Code,
			Context: fmt.Sprintf("HTTP handler at %s:%d", handler.File, handler.Line),
		}
	}

	// Call AI for security assessment
	assessments, err := sdk.BatchAssessCode(ctx, snippets, "security", sdk.AssessmentOptions{
		Focus: fmt.Sprintf("Look for: %s", strings.Join(w.SecurityFocus, ", ")),
		Context: "These are HTTP handlers that process user input. Pay special attention to input validation, authentication checks, and database queries.",
	})

	if err != nil {
		// If AI call fails, log and continue
		// (In production, you might want to track this failure)
		return
	}

	// Process AI assessments
	for _, handler := range handlers {
		assessment, ok := assessments[handler.Name]
		if !ok {
			continue
		}

		result.IncrementAICalls()
		result.AddTokensUsed(assessment.TokensUsed)
		result.AddCost(assessment.EstimatedCost)

		// If AI found issues, create discovered issues
		if strings.Contains(strings.ToLower(assessment.Summary), "issue") ||
			strings.Contains(strings.ToLower(assessment.Summary), "vulnerability") ||
			strings.Contains(strings.ToLower(assessment.Summary), "concern") {

			result.AddIssue(sdk.NewIssue().
				WithTitle(fmt.Sprintf("Security review: %s", handler.Name)).
				WithDescription(fmt.Sprintf("AI security analysis of handler %s at %s:%d\n\n%s\n\nRecommendations:\n%s",
					handler.Name, handler.File, handler.Line, assessment.Summary, strings.Join(assessment.Recommendations, "\n"))).
				WithCategory("security").
				WithFile(handler.File, handler.Line).
				WithPriority(1). // P1 - security issues are high priority
				WithTag("security").
				WithTag("api").
				WithTag("ai-detected").
				WithEvidence("ai_summary", assessment.Summary).
				WithEvidence("handler_name", handler.Name).
				WithConfidence(assessment.Confidence).
				Build())

			result.IncrementPatternsFound()
		}
	}
}
