# Custom Worker SDK Tutorial

This tutorial will guide you through creating your first custom VC discovery worker in 5 minutes.

## What is a Custom Worker?

Custom workers extend VC's discovery capabilities with:
- **Company-specific standards** (naming conventions, code organization)
- **Language-specific patterns** (Python, Rust, TypeScript checks)
- **Domain-specific checks** (healthcare HIPAA, finance PCI compliance)
- **Integration with internal tools** (Jira, Datadog, custom linters)

## Quick Start: Your First Worker in 5 Minutes

### Step 1: Create the Worker File

Create `my_worker.go`:

```go
package main

import (
    "context"
    "time"

    "github.com/steveyegge/vc/internal/discovery"
    "github.com/steveyegge/vc/internal/discovery/sdk"
    "github.com/steveyegge/vc/internal/health"
)

type MyWorker struct{}

func NewMyWorker() *MyWorker {
    return &MyWorker{}
}

func (w *MyWorker) Name() string {
    return "my_custom_worker"
}

func (w *MyWorker) Philosophy() string {
    return "My philosophy for code quality"
}

func (w *MyWorker) Scope() string {
    return "What this worker analyzes"
}

func (w *MyWorker) Cost() health.CostEstimate {
    return health.CostEstimate{
        EstimatedDuration: 30 * time.Second,
        AICallsEstimated:  0,
        RequiresFullScan:  true,
        Category:          health.CostCheap,
    }
}

func (w *MyWorker) Dependencies() []string {
    return nil // No dependencies
}

func (w *MyWorker) Analyze(ctx context.Context, codebase health.CodebaseContext) (*discovery.WorkerResult, error) {
    result := sdk.NewWorkerResultBuilder(w.Name())

    // Your analysis logic here
    // For example, find TODO comments:
    matches, _ := sdk.FindPattern(codebase.RootPath, `TODO:.*`, sdk.PatternOptions{
        FilePattern: "*.go",
    })

    for _, match := range matches {
        result.AddIssue(sdk.NewIssue().
            WithTitle("TODO found: " + match.Text).
            WithFile(match.File, match.Line).
            WithPriority(3).
            Build())
    }

    return result.Build(), nil
}
```

### Step 2: Register the Worker

```go
// In your VC configuration or startup code
worker := NewMyWorker()
registry.Register(worker)
```

### Step 3: Run Discovery

```bash
vc discover --workers my_custom_worker
```

Done! Your worker will now run during discovery and find TODOs in your codebase.

## Tutorial 1: Pattern Matching Worker

Let's build a worker that enforces naming conventions.

```go
type NamingWorker struct{}

func (w *NamingWorker) Analyze(ctx context.Context, codebase health.CodebaseContext) (*discovery.WorkerResult, error) {
    result := sdk.NewWorkerResultBuilder(w.Name())

    // Find functions with snake_case names (Go uses camelCase)
    sdk.WalkGoFiles(codebase.RootPath, func(file *sdk.GoFile) error {
        for _, fn := range file.Functions() {
            if hasSnakeCase(fn.Name()) {
                result.AddIssue(sdk.NewIssue().
                    WithTitle("Function uses snake_case: " + fn.Name()).
                    WithDescription("Go convention is camelCase for function names").
                    WithFile(file.Path, fn.StartLine()).
                    WithPriority(3).
                    WithTag("naming").
                    Build())
            }
        }
        return nil
    })

    return result.Build(), nil
}

func hasSnakeCase(name string) bool {
    return strings.Contains(name, "_") && !strings.HasPrefix(name, "Test")
}
```

## Tutorial 2: AST-Based Analysis

Analyze code structure using the AST helpers:

```go
func (w *ComplexityWorker) Analyze(ctx context.Context, codebase health.CodebaseContext) (*discovery.WorkerResult, error) {
    result := sdk.NewWorkerResultBuilder(w.Name())

    sdk.WalkGoFiles(codebase.RootPath, func(file *sdk.GoFile) error {
        for _, fn := range file.Functions() {
            // Check function length
            if fn.LineCount() > 100 {
                result.AddIssue(sdk.NewIssue().
                    WithTitle(fmt.Sprintf("Long function: %s (%d lines)", fn.Name(), fn.LineCount())).
                    WithDescription("Consider breaking down functions longer than 100 lines").
                    WithFile(file.Path, fn.StartLine()).
                    WithPriority(2).
                    WithTag("complexity").
                    Build())
            }

            // Check parameter count
            if len(fn.Parameters()) > 5 {
                result.AddIssue(sdk.NewIssue().
                    WithTitle(fmt.Sprintf("Too many parameters: %s (%d params)", fn.Name(), len(fn.Parameters()))).
                    WithDescription("Functions with more than 5 parameters are hard to use").
                    WithFile(file.Path, fn.StartLine()).
                    WithPriority(3).
                    Build())
            }
        }
        return nil
    })

    return result.Build(), nil
}
```

## Tutorial 3: AI-Powered Analysis

Use AI to perform sophisticated analysis:

```go
func (w *SecurityWorker) Analyze(ctx context.Context, codebase health.CodebaseContext) (*discovery.WorkerResult, error) {
    result := sdk.NewWorkerResultBuilder(w.Name())

    // Find all API handlers
    var handlers []sdk.CodeSnippet
    sdk.WalkGoFiles(codebase.RootPath, func(file *sdk.GoFile) error {
        for _, fn := range file.Functions() {
            if strings.Contains(fn.Name(), "Handler") {
                // Extract function code (simplified)
                handlers = append(handlers, sdk.CodeSnippet{
                    ID:      fn.Name(),
                    Code:    "/* function code */",
                    Context: fmt.Sprintf("%s:%d", file.Path, fn.StartLine()),
                })
            }
        }
        return nil
    })

    // Batch analyze with AI
    assessments, err := sdk.BatchAssessCode(ctx, handlers, "security", sdk.AssessmentOptions{
        Focus: "Look for SQL injection, XSS, and authentication bypass",
    })

    if err != nil {
        return nil, err
    }

    // Create issues from AI findings
    for id, assessment := range assessments {
        if len(assessment.Issues) > 0 {
            result.AddIssue(sdk.NewIssue().
                WithTitle("Security issue in " + id).
                WithDescription(assessment.Summary).
                WithPriority(1).
                WithTag("security").
                WithTag("ai-detected").
                Build())

            result.IncrementAICalls()
            result.AddTokensUsed(assessment.TokensUsed)
        }
    }

    return result.Build(), nil
}
```

## Tutorial 4: YAML Workers (No Code Required)

For simple pattern matching, use YAML instead of Go:

Create `.vc/workers/my-checks.yaml`:

```yaml
name: my_custom_checks
philosophy: "My philosophy"
scope: "What I check"

cost:
  duration: "30s"
  ai_calls: 0
  category: cheap

patterns:
  - name: "hardcoded_passwords"
    regex: 'password\s*=\s*"[^"]+"'
    file_pattern: "*.go"
    title: "Hardcoded password detected"
    description: "Passwords should not be hardcoded in source code"
    priority: 0
    category: "security"
    tags:
      - "security"
      - "credentials"
    confidence: 0.9

missing_files:
  - path: "SECURITY.md"
    title: "Missing SECURITY.md"
    description: "Projects should document security disclosure policies"
    priority: 2
    category: "security"
```

VC automatically discovers and runs YAML workers from:
- `.vc/workers/` (project-local)
- `~/.vc/workers/` (user-global)

## Tutorial 5: Building Plugins

Share workers as compiled plugins:

1. **Create the plugin:**

```go
// my_worker_plugin.go
package main

import (
    "github.com/steveyegge/vc/internal/discovery"
)

type MyPluginWorker struct{}

func (w *MyPluginWorker) Name() string { return "my_plugin" }
// ... implement other methods

// Export the worker
var Worker discovery.DiscoveryWorker = &MyPluginWorker{}
```

2. **Build as plugin:**

```bash
go build -buildmode=plugin -o my_worker.so my_worker_plugin.go
```

3. **Install:**

```bash
# Project-local
cp my_worker.so /path/to/project/.vc/workers/

# User-global
cp my_worker.so ~/.vc/workers/
```

4. **Use:**

VC automatically discovers and loads plugins from these directories.

## Best Practices

### 1. Keep Workers Focused

Each worker should have one clear purpose:
- ✅ Good: "naming_conventions" checks naming
- ❌ Bad: "code_quality" checks everything

### 2. Provide Clear Evidence

Include context in issue descriptions:

```go
result.AddIssue(sdk.NewIssue().
    WithTitle("Function too long: processOrder").
    WithDescription(fmt.Sprintf(
        "Function processOrder at %s:%d is %d lines long.\n\n"+
        "Functions over 100 lines are harder to understand and maintain. "+
        "Consider breaking it into smaller, focused functions.",
        file, line, lineCount)).
    WithEvidence("line_count", lineCount).
    WithEvidence("recommendation", "Break into processPayment, validateOrder, sendNotification").
    Build())
```

### 3. Control AI Costs

Limit AI calls to avoid excessive costs:

```go
const maxAIChecks = 20

if aiCallCount >= maxAIChecks {
    // Stop making AI calls
    return result.Build(), nil
}
```

### 4. Use Appropriate Confidence

Set confidence based on certainty:
- `1.0` - Exact pattern match (regex, missing files)
- `0.8-0.9` - High confidence (obvious patterns)
- `0.6-0.7` - Medium confidence (AI analysis, heuristics)
- `0.4-0.5` - Low confidence (might be false positive)

### 5. Test Your Workers

```go
func TestMyWorker(t *testing.T) {
    worker := NewMyWorker()

    // Create test codebase
    codebase := health.CodebaseContext{
        RootPath: "testdata/sample_project",
    }

    result, err := worker.Analyze(context.Background(), codebase)
    require.NoError(t, err)

    assert.Greater(t, len(result.IssuesDiscovered), 0)
    assert.Equal(t, "my_custom_worker", result.IssuesDiscovered[0].DiscoveredBy)
}
```

## SDK Reference

See the [SDK package documentation](https://pkg.go.dev/github.com/steveyegge/vc/internal/discovery/sdk) for complete API reference.

### Key Packages

- **sdk** - Core builders and utilities
- **sdk/ast** - Go AST parsing helpers
- **sdk/pattern** - Pattern matching and search
- **sdk/ai** - AI integration

### Helper Functions

- `NewWorkerResultBuilder()` - Build worker results
- `NewIssue()` - Build discovered issues
- `WalkGoFiles()` - Traverse Go source files
- `FindPattern()` - Regex pattern search
- `CallAI()` - AI supervision integration

## Next Steps

- Browse [example workers](./examples/) for inspiration
- Read the [YAML worker spec](./yaml_worker.go) for declarative workers
- See [plugin system](./plugin.go) for distribution
- Join the [VC community](https://github.com/steveyegge/vc) to share workers

## Getting Help

- GitHub Issues: https://github.com/steveyegge/vc/issues
- Documentation: https://vc.dev/docs
- Examples: `internal/discovery/sdk/examples/`
