package examples

import (
	"context"
	"fmt"
	"strings"
	"time"
	"unicode"

	"github.com/steveyegge/vc/internal/discovery"
	"github.com/steveyegge/vc/internal/discovery/sdk"
	"github.com/steveyegge/vc/internal/health"
)

// NamingConventionWorker enforces company-specific naming conventions.
// This is an example custom worker showing pattern-based analysis.
//
// Philosophy: "Consistent naming improves code readability and maintainability"
//
// Example usage:
//
//	worker := examples.NewNamingConventionWorker(examples.NamingRules{
//		FunctionPrefix: "handle",  // API handlers must start with "handle"
//		TestSuffix:     "Test",    // Test functions must end with "Test"
//		ConstantCase:   "UPPER",   // Constants must be UPPER_CASE
//	})
type NamingConventionWorker struct {
	rules NamingRules
}

// NamingRules defines naming convention rules to enforce.
type NamingRules struct {
	// FunctionPrefix requires functions to start with this prefix
	FunctionPrefix string

	// TestSuffix requires test functions to end with this suffix
	TestSuffix string

	// ConstantCase enforces constant naming ("UPPER", "lower", "camelCase")
	ConstantCase string

	// TypePrefix requires types to start with this prefix
	TypePrefix string

	// InterfaceSuffix requires interfaces to end with this suffix (e.g., "er", "able")
	InterfaceSuffix string
}

// NewNamingConventionWorker creates a naming convention worker.
func NewNamingConventionWorker(rules NamingRules) *NamingConventionWorker {
	return &NamingConventionWorker{rules: rules}
}

// Name implements DiscoveryWorker.
func (w *NamingConventionWorker) Name() string {
	return "naming_conventions"
}

// Philosophy implements DiscoveryWorker.
func (w *NamingConventionWorker) Philosophy() string {
	return "Consistent naming improves code readability and maintainability"
}

// Scope implements DiscoveryWorker.
func (w *NamingConventionWorker) Scope() string {
	return "Function names, type names, constant names, interface names"
}

// Cost implements DiscoveryWorker.
func (w *NamingConventionWorker) Cost() health.CostEstimate {
	return health.CostEstimate{
		EstimatedDuration: 30 * time.Second,
		AICallsEstimated:  0, // Pure pattern matching, no AI needed
		RequiresFullScan:  true,
		Category:          health.CostCheap,
	}
}

// Dependencies implements DiscoveryWorker.
func (w *NamingConventionWorker) Dependencies() []string {
	return nil
}

// Analyze implements DiscoveryWorker.
func (w *NamingConventionWorker) Analyze(ctx context.Context, codebase health.CodebaseContext) (*discovery.WorkerResult, error) {
	result := sdk.NewWorkerResultBuilder(w.Name()).
		WithContext(fmt.Sprintf("Enforcing naming conventions in %s", codebase.RootPath)).
		WithReasoning(fmt.Sprintf("Based on philosophy: '%s'\n\nConsistent naming conventions help developers quickly understand code purpose and reduce cognitive load.", w.Philosophy()))

	// Walk all Go files
	err := sdk.WalkGoFiles(codebase.RootPath, func(file *sdk.GoFile) error {
		result.IncrementFilesAnalyzed()

		// Check function names
		if w.rules.FunctionPrefix != "" {
			w.checkFunctionPrefix(file, result)
		}

		// Check test function names
		if w.rules.TestSuffix != "" && strings.HasSuffix(file.Path, "_test.go") {
			w.checkTestSuffix(file, result)
		}

		// Check type names
		if w.rules.TypePrefix != "" || w.rules.InterfaceSuffix != "" {
			w.checkTypeNames(file, result)
		}

		return nil
	})

	if err != nil {
		return nil, err
	}

	return result.Build(), nil
}

// checkFunctionPrefix checks if functions have the required prefix.
func (w *NamingConventionWorker) checkFunctionPrefix(file *sdk.GoFile, result *sdk.WorkerResultBuilder) {
	for _, fn := range file.Functions() {
		// Skip methods and test functions
		if fn.IsMethod() || strings.HasPrefix(fn.Name(), "Test") {
			continue
		}

		// Check if exported function has the required prefix
		if unicode.IsUpper(rune(fn.Name()[0])) && !strings.HasPrefix(fn.Name(), w.rules.FunctionPrefix) {
			result.AddIssue(sdk.NewIssue().
				WithTitle(fmt.Sprintf("Function naming: '%s' should start with '%s'", fn.Name(), w.rules.FunctionPrefix)).
				WithDescription(fmt.Sprintf("Exported function '%s' at %s:%d doesn't follow naming convention.\n\nExpected: Functions should start with '%s' (e.g., %s%s)", fn.Name(), file.Path, fn.StartLine(), w.rules.FunctionPrefix, w.rules.FunctionPrefix, strings.TrimPrefix(fn.Name(), strings.ToLower(fn.Name()[:1])))).
				WithCategory("naming").
				WithFile(file.Path, fn.StartLine()).
				WithPriority(3). // P3 - stylistic
				WithTag("naming-convention").
				WithConfidence(0.9).
				Build())

			result.IncrementPatternsFound()
		}
	}
}

// checkTestSuffix checks if test functions have the required suffix.
func (w *NamingConventionWorker) checkTestSuffix(file *sdk.GoFile, result *sdk.WorkerResultBuilder) {
	for _, fn := range file.Functions() {
		// Only check test functions
		if !strings.HasPrefix(fn.Name(), "Test") {
			continue
		}

		if !strings.HasSuffix(fn.Name(), w.rules.TestSuffix) && fn.Name() != "Test"+w.rules.TestSuffix {
			result.AddIssue(sdk.NewIssue().
				WithTitle(fmt.Sprintf("Test naming: '%s' should end with '%s'", fn.Name(), w.rules.TestSuffix)).
				WithDescription(fmt.Sprintf("Test function '%s' at %s:%d doesn't follow naming convention.\n\nExpected: Test functions should end with '%s'", fn.Name(), file.Path, fn.StartLine(), w.rules.TestSuffix)).
				WithCategory("naming").
				WithFile(file.Path, fn.StartLine()).
				WithPriority(3).
				WithTag("naming-convention").
				WithTag("testing").
				WithConfidence(0.9).
				Build())

			result.IncrementPatternsFound()
		}
	}
}

// checkTypeNames checks if types follow naming conventions.
func (w *NamingConventionWorker) checkTypeNames(file *sdk.GoFile, result *sdk.WorkerResultBuilder) {
	for _, typ := range file.Types() {
		// Check type prefix
		if w.rules.TypePrefix != "" && !strings.HasPrefix(typ.Name(), w.rules.TypePrefix) {
			result.AddIssue(sdk.NewIssue().
				WithTitle(fmt.Sprintf("Type naming: '%s' should start with '%s'", typ.Name(), w.rules.TypePrefix)).
				WithDescription(fmt.Sprintf("Type '%s' at %s doesn't follow naming convention.\n\nExpected: Types should start with '%s'", typ.Name(), file.Path, w.rules.TypePrefix)).
				WithCategory("naming").
				WithFile(file.Path).
				WithPriority(3).
				WithTag("naming-convention").
				WithConfidence(0.9).
				Build())

			result.IncrementPatternsFound()
		}

		// Check interface suffix
		if w.rules.InterfaceSuffix != "" && typ.IsInterface() && !strings.HasSuffix(typ.Name(), w.rules.InterfaceSuffix) {
			result.AddIssue(sdk.NewIssue().
				WithTitle(fmt.Sprintf("Interface naming: '%s' should end with '%s'", typ.Name(), w.rules.InterfaceSuffix)).
				WithDescription(fmt.Sprintf("Interface '%s' at %s doesn't follow naming convention.\n\nExpected: Interfaces should end with '%s' (e.g., Reader, Writer, Closer)", typ.Name(), file.Path, w.rules.InterfaceSuffix)).
				WithCategory("naming").
				WithFile(file.Path).
				WithPriority(3).
				WithTag("naming-convention").
				WithTag("interface").
				WithConfidence(0.8).
				Build())

			result.IncrementPatternsFound()
		}
	}
}
