# Discovery Workers

## Overview

Discovery workers are specialized analyzers that examine a codebase to find actionable issues during initial bootstrap. They embody specific philosophies about code quality and use AI to distinguish real problems from acceptable patterns.

Discovery Mode enables `vc init --discover` to automatically analyze any codebase and file issues, making it easy to onboard VC to existing projects.

## Architecture

### Worker Interface

All discovery workers implement the `DiscoveryWorker` interface:

```go
type DiscoveryWorker interface {
    Name() string          // Unique identifier
    Philosophy() string    // Guiding principle
    Scope() string        // What this worker analyzes
    Cost() CostEstimate   // Resource usage estimate
    Dependencies() []string // Other workers to run first
    Analyze(ctx context.Context, codebase CodebaseContext) (*WorkerResult, error)
}
```

### CodebaseContext

Workers receive a `CodebaseContext` built once and shared by all workers for efficiency:

- **RootPath**: Root directory of the codebase
- **FileSizeDistribution**: Statistical distribution of file sizes
- **LanguageBreakdown**: File counts per programming language
- **TotalFiles**, **TotalLines**: Overall codebase statistics
- **Patterns**: Naming conventions and architectural patterns

### Worker Results

Workers return `WorkerResult` containing:

- **IssuesDiscovered**: List of potential issues with evidence
- **Context**: What was examined
- **Reasoning**: Why these might be problems (citing worker philosophy)
- **Stats**: Files analyzed, duration, AI calls, cost

### ZFC Compliance

Workers follow **Zero Framework Cognition (ZFC)** principles:

- **Collect facts, don't make judgments**: Workers gather objective patterns and statistics
- **AI makes decisions**: Discovered issues go to AI supervision for assessment
- **No hardcoded thresholds**: Use distributions and percentiles, not magic numbers
- **Cite philosophy**: Explain why a pattern might be a problem, let AI decide if it is

## Built-In Workers

### 1. ArchitectureScanner

**Philosophy**: *"Good architecture has clear boundaries, minimal coupling, and cohesive modules"*

**What it analyzes**:
- Package/module structure (import graphs)
- Circular dependencies (cycle detection)
- Coupling metrics (fan-in, fan-out)
- God packages (too many responsibilities)
- Layer violations
- Missing abstractions

**Algorithm**:

1. **Build Import Graph**
   - Parse all Go files using `go/parser`
   - Extract package names and import statements
   - Build bidirectional graph (imports and importedBy)
   - Count types per package

2. **Detect Circular Dependencies**
   - Use Tarjan's strongly connected components algorithm
   - Find all cycles in the import graph
   - Report each cycle as a potential issue

3. **Calculate Coupling Metrics**
   - Fan-in: Number of packages that import this package
   - Fan-out: Number of packages this package imports
   - Flag packages above 75th percentile in either metric

4. **Detect God Packages**
   - Count type declarations per package
   - Flag packages with >20 types (configurable threshold)
   - Compare against average types per package

**Cost Estimate**:
- Duration: ~30 seconds
- AI Calls: 3 (overall assessment + specific issues)
- Category: Moderate
- Requires full scan: Yes

**Dependencies**: None (foundational worker)

**Example Issues**:

```
Title: Circular dependency detected: auth → user → auth
Description: Circular import chain: auth → user → auth

Circular dependencies make code harder to understand, test, and maintain.
They often indicate unclear module boundaries or missing abstractions.

Evidence:
  cycle: ["auth", "user", "auth"]
  cycle_length: 2

Confidence: 0.9 (high - cycles are objective)
```

```
Title: God package detected: executor (45 types)
Description: Package executor contains 45 types, significantly more than
average (12).

God packages with too many responsibilities are harder to understand
and maintain. Consider splitting into focused sub-packages.

Evidence:
  type_count: 45
  avg_types: 12
  threshold: 20

Confidence: 0.7 (medium-high - needs AI to confirm if it's really a problem)
```

### 2. BugHunter

**Philosophy**: *"Common bug patterns indicate missing safeguards. Static analysis + AI catches more than linters alone"*

**What it analyzes**:
- Resource leaks (files/connections not closed)
- Error handling gaps (errors ignored)
- Nil dereference risks (unchecked nil returns)
- Goroutine leaks (no cleanup mechanism)
- Race conditions (shared mutable state without locks)
- Off-by-one errors (loop bounds)

**Algorithm**:

1. **Resource Leak Detection**
   - Find calls to resource-opening functions (`Open`, `Create`, `Dial`, etc.)
   - Check for corresponding `defer Close()` in same function
   - Flag resources without visible cleanup

2. **Error Handling Gap Detection**
   - Find assignments where error is assigned to `_` (blank identifier)
   - Check if this is the error return value (last or second-to-last position)
   - Flag ignored errors with function name and location

3. **Nil Dereference Risk Detection**
   - Find pointer dereferences (`*ptr`)
   - Check for nil checks before dereference
   - Flag unchecked dereferences (high false positive rate)

4. **Goroutine Leak Detection**
   - Find `go` statements launching goroutines
   - Check if goroutine has `context.Context` parameter
   - Check if goroutine body has `select` with `ctx.Done()`
   - Flag goroutines without cleanup mechanism

**Cost Estimate**:
- Duration: ~2 minutes
- AI Calls: 10 (filter false positives)
- Category: Expensive
- Requires full scan: Yes

**Dependencies**: Architecture worker (uses package graph for context)

**Example Issues**:

```
Title: Error ignored: ReadFile returns error but it's assigned to _
Description: At config.go:42, error from ReadFile is assigned to blank identifier.

Ignoring errors can hide failures and make debugging difficult. Errors should
be checked or explicitly logged if intentionally ignored.

Evidence:
  function: ReadFile
  line: 42
  file: config.go

Confidence: 0.6 (medium - might be intentional)
```

```
Title: Potential goroutine leak: no visible cleanup mechanism
Description: At server.go:123, goroutine launched without context or cleanup.

Goroutines should either:
1) Have a context.Context parameter and select on ctx.Done(), or
2) Have explicit shutdown channel.
Otherwise they may leak when the program tries to exit.

Evidence:
  line: 123
  file: server.go

Confidence: 0.5 (medium - might have cleanup elsewhere)
```

## Using Discovery Workers

### Running Discovery

```bash
# Quick scan (< $0.50, < 1 min)
vc init --discover --preset=quick

# Standard scan (< $2.00, < 5 min)
vc init --discover --preset=standard

# Thorough scan (< $10.00, < 15 min)
vc init --discover --preset=thorough

# Custom workers
vc discover --workers=architecture,bugs --dry-run

# List available workers
vc discover --list-workers

# Show worker details
vc discover --show-worker=architecture
```

### Programmatic Usage

```go
import (
    "context"
    "github.com/steveyegge/vc/internal/discovery"
    "github.com/steveyegge/vc/internal/health"
)

// Create registry with workers
registry := NewWorkerRegistry()
registry.Register(NewArchitectureWorker())
registry.Register(NewBugHunterWorker())

// Build codebase context
builder := discovery.NewContextBuilder("/path/to/code", nil)
ctx := context.Background()
codebaseCtx, err := builder.Build(ctx)

// Run worker
worker := discovery.NewArchitectureWorker()
result, err := worker.Analyze(ctx, codebaseCtx)

// Process results
for _, issue := range result.IssuesDiscovered {
    fmt.Printf("%s: %s\n", issue.Title, issue.Description)
    fmt.Printf("  Confidence: %.2f\n", issue.Confidence)
    fmt.Printf("  Evidence: %+v\n", issue.Evidence)
}
```

### Preset Configurations

**Quick Preset** (filesize, cruft):
- Fast workers only
- Minimal AI usage
- Good for initial triage

**Standard Preset** (filesize, cruft, duplication, architecture):
- Core quality workers
- Moderate AI usage
- Best for regular discovery runs

**Thorough Preset** (filesize, cruft, duplication, zfc, architecture, bugs):
- All available workers
- Heavy AI usage
- Comprehensive analysis for new codebases

## Implementing Custom Workers

### Step 1: Implement the Interface

```go
type MyWorker struct {
    // Configuration fields
}

func (w *MyWorker) Name() string {
    return "my_worker"
}

func (w *MyWorker) Philosophy() string {
    return "State your principle - NOT a threshold"
}

func (w *MyWorker) Scope() string {
    return "What you analyze"
}

func (w *MyWorker) Cost() health.CostEstimate {
    return health.CostEstimate{
        EstimatedDuration: 30 * time.Second,
        AICallsEstimated:  2,
        RequiresFullScan:  true,
        Category:          health.CostModerate,
    }
}

func (w *MyWorker) Dependencies() []string {
    return []string{"architecture"} // Or nil
}

func (w *MyWorker) Analyze(ctx context.Context, codebase health.CodebaseContext) (*discovery.WorkerResult, error) {
    // 1. Collect facts and patterns
    // 2. Build evidence
    // 3. Return issues with context and reasoning
}
```

### Step 2: Register the Worker

```go
// In registry.go DefaultRegistry()
myWorker := NewMyWorker()
if err := registry.Register(myWorker); err != nil {
    return nil, err
}
```

### Step 3: Add to Presets

```go
// In types.go PresetConfig()
case PresetThorough:
    cfg.Workers = []string{
        // ... existing workers
        "my_worker",
    }
```

## Best Practices

### DO

✅ **State principles, not thresholds**
- Good: "Functions should have a single responsibility"
- Bad: "Functions should be under 50 lines"

✅ **Provide rich evidence**
- Include file paths, line numbers, metrics
- Give AI context to make informed decisions

✅ **Use confidence scores**
- High (0.8-1.0): Objective facts (cycles, leaks)
- Medium (0.5-0.7): Patterns that often indicate problems
- Low (0.3-0.5): Heuristics with high false positive rate

✅ **Cite your philosophy in reasoning**
- Explain *why* this pattern might be a problem
- Reference industry best practices

✅ **Handle errors gracefully**
- Skip files that fail to parse
- Continue analysis despite errors
- Track errors in Stats.ErrorsIgnored

### DON'T

❌ **Don't make judgments**
- Don't decide if an issue is "real" - that's AI's job
- Don't filter based on thresholds - report patterns

❌ **Don't use magic numbers**
- Bad: `if lines > 500`
- Good: `if lines > distribution.P95`

❌ **Don't assume AI context**
- Provide full descriptions in discovered issues
- Don't rely on AI "knowing" about the codebase

❌ **Don't block on non-critical errors**
- Parse errors? Skip file and continue
- Missing dependency? Report and continue

## Future Workers

Potential workers to implement:

- **DocumentationWorker**: Find missing docs (public APIs without comments)
- **TestWorker**: Find coverage gaps (files without test files)
- **DependencyWorker**: Analyze external dependencies (outdated, vulnerable)
- **SecurityWorker**: Find security issues (SQL injection, XSS risks)
- **PerformanceWorker**: Find performance issues (N+1 queries, inefficient algorithms)
- **AccessibilityWorker**: Find accessibility issues (missing alt text, ARIA labels)

## Troubleshooting

### Worker takes too long

- Reduce `MaxDuration` in budget
- Use `Quick` preset instead of `Thorough`
- Exclude large directories (vendor/, node_modules/)

### Too many false positives

- Lower confidence threshold when filing issues
- Add AI assessment to filter patterns
- Refine detection heuristics

### Worker fails to parse files

- Check file encoding (should be UTF-8)
- Verify Go syntax is valid
- Check for generated files (excluded by default)

### High cost / too many AI calls

- Reduce `MaxAICalls` in budget
- Use simpler workers (Quick preset)
- Batch issues for AI assessment

## References

- **ZFC Philosophy**: docs/ZFC.md
- **Discovery Orchestration**: internal/discovery/orchestrator.go
- **Worker Examples**: internal/discovery/*_worker.go
- **Integration Tests**: internal/discovery/workers_integration_test.go
