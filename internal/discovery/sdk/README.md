# VC Custom Worker SDK

Build custom discovery workers to extend VC with company-specific standards, language-specific patterns, or domain-specific checks.

## Quick Links

- **[Tutorial](./TUTORIAL.md)** - Build your first worker in 5 minutes
- **[Examples](./examples/)** - Sample workers to learn from
- **[API Reference](https://pkg.go.dev/github.com/steveyegge/vc/internal/discovery/sdk)** - Complete SDK documentation

## Why Custom Workers?

VC ships with built-in discovery workers for common patterns (architecture, bugs, documentation). But every organization has unique needs:

- **Company Standards**: Enforce your naming conventions, code organization, or API design
- **Language-Specific**: Add checks for Python, Rust, TypeScript, or any language
- **Domain-Specific**: Healthcare HIPAA compliance, financial PCI requirements
- **Integration**: Connect with Jira, Datadog, SonarQube, or internal tools

Custom workers give you this extensibility **without forking VC**.

## Three Ways to Build Workers

### 1. Go SDK (Most Powerful)

Write workers in Go for maximum flexibility:

```go
type MyWorker struct{}

func (w *MyWorker) Analyze(ctx context.Context, codebase health.CodebaseContext) (*discovery.WorkerResult, error) {
    result := sdk.NewWorkerResultBuilder(w.Name())

    // AST-based analysis
    sdk.WalkGoFiles(codebase.RootPath, func(file *sdk.GoFile) error {
        for _, fn := range file.Functions() {
            if fn.LineCount() > 100 {
                result.AddIssue(sdk.NewIssue().
                    WithTitle("Long function: " + fn.Name()).
                    WithFile(file.Path, fn.StartLine()).
                    Build())
            }
        }
        return nil
    })

    return result.Build(), nil
}
```

**Best for:**
- Complex AST analysis
- Multi-step checks
- Custom integrations

### 2. YAML (Simplest)

Define workers declaratively for simple pattern matching:

```yaml
name: security_basics
philosophy: "Catch common vulnerabilities"
scope: "Hardcoded secrets, SQL injection patterns"

patterns:
  - name: "hardcoded_api_keys"
    regex: 'api_key\s*=\s*"[^"]+"'
    file_pattern: "*.go"
    title: "Hardcoded API key detected"
    priority: 0
    category: "security"
```

**Best for:**
- Simple regex patterns
- Missing file checks
- Quick prototyping

### 3. Plugins (Most Portable)

Compile workers as `.so` files for easy distribution:

```bash
go build -buildmode=plugin -o my_worker.so my_worker.go
```

Install in `.vc/workers/` or `~/.vc/workers/` and VC automatically loads them.

**Best for:**
- Sharing across teams
- Proprietary workers
- Third-party extensions

## SDK Components

### Core Builders

- **WorkerResultBuilder** - Fluent API for building results
- **IssueBuilder** - Fluent API for creating issues

```go
result := sdk.NewWorkerResultBuilder("my_worker").
    WithContext("Analyzed 50 files").
    AddIssue(sdk.NewIssue().
        WithTitle("Issue found").
        WithPriority(2).
        Build()).
    Build()
```

### AST Helpers

Parse and traverse Go source code:

```go
sdk.WalkGoFiles(rootPath, func(file *sdk.GoFile) error {
    for _, fn := range file.Functions() {
        fmt.Println(fn.Name(), fn.LineCount())
    }
    return nil
})
```

Available helpers:
- `GoFile.Functions()` - All function declarations
- `GoFile.Types()` - All type declarations
- `Function.Parameters()` - Function parameters
- `Function.CallsFunction()` - Check if function calls another
- `TypeDecl.Fields()` - Struct fields
- `TypeDecl.Methods()` - Interface methods

### Pattern Matching

Search for patterns in code:

```go
matches, err := sdk.FindPattern(rootPath, `TODO:.*`, sdk.PatternOptions{
    FilePattern: "*.go",
    ExcludeDirs: []string{"vendor", ".git"},
})

for _, match := range matches {
    fmt.Printf("%s:%d: %s\n", match.File, match.Line, match.Text)
}
```

Utilities:
- `FindPattern()` - Regex search across files
- `FindMissingFiles()` - Check for required files
- `FindLargeFiles()` - Find files exceeding size limits

### AI Integration

Leverage AI for sophisticated analysis:

```go
response, err := sdk.CallAI(ctx, sdk.AIRequest{
    Prompt: "Is this code secure?\n" + code,
})

assessment, err := sdk.AssessCode(ctx, code, "security", sdk.AssessmentOptions{
    Focus: "Look for SQL injection and XSS",
})

// Batch multiple snippets for efficiency
assessments, err := sdk.BatchAssessCode(ctx, snippets, "security", opts)
```

## Discovery Locations

VC automatically discovers workers from:

1. **Project workers**: `.vc/workers/` (checked into git)
2. **User workers**: `~/.vc/workers/` (personal customizations)
3. **External workers**: Via plugin paths in config

Priority order: project → user → external

## Examples

### Naming Conventions

```go
// examples/naming_convention_worker.go
type NamingConventionWorker struct {
    rules NamingRules
}

// Enforce company naming standards
// - Functions must start with "handle" prefix
// - Interfaces must end with "er" suffix
// - Constants must be UPPER_CASE
```

### TODO Tracker

```go
// examples/todo_tracker_worker.go
type TODOTrackerWorker struct{}

// Find and track TODO/FIXME/HACK comments
// Converts hidden technical debt into tracked issues
```

### API Security

```go
// examples/api_security_worker.go
type APISecurityWorker struct{}

// AI-powered security analysis of HTTP handlers
// Detects SQL injection, XSS, auth bypass
```

### YAML Examples

```yaml
# examples/yaml/naming-conventions.yaml
# Simple pattern-based naming checks

# examples/yaml/security-basics.yaml
# Common security patterns (no AI needed)
```

## Worker Lifecycle

1. **Discovery**: VC finds workers in standard locations
2. **Registration**: Workers register with WorkerRegistry
3. **Dependency Resolution**: Workers sorted by dependencies
4. **Execution**: Each worker analyzes the codebase
5. **Deduplication**: Similar issues are merged
6. **Filing**: Issues created in Beads tracker

## Cost Control

Workers declare cost estimates:

```go
func (w *MyWorker) Cost() health.CostEstimate {
    return health.CostEstimate{
        EstimatedDuration: 2 * time.Minute,
        AICallsEstimated:  20,
        Category:          health.CostExpensive,
    }
}
```

VC enforces budgets:
- `--preset quick` - Cheap workers only, $0.50 max
- `--preset standard` - Core workers, $2.00 max
- `--preset thorough` - All workers, $10.00 max

## Testing Workers

Use the test helpers:

```go
func TestMyWorker(t *testing.T) {
    worker := &MyWorker{}

    codebase := health.CodebaseContext{
        RootPath: "testdata/sample_project",
    }

    result, err := worker.Analyze(context.Background(), codebase)
    require.NoError(t, err)

    assert.Greater(t, len(result.IssuesDiscovered), 0)
    assert.Contains(t, result.Context, "Analyzed")
}
```

## Best Practices

### 1. Philosophy Over Rules

Good workers have clear philosophy:
- ✅ "Consistent naming improves readability"
- ❌ "Check if functions use snake_case"

Philosophy guides AI assessment.

### 2. ZFC Compliance

Workers collect facts. AI makes judgments.

```go
// ✅ Good: Collect facts
result.AddIssue(sdk.NewIssue().
    WithTitle("Function is 250 lines").
    WithEvidence("line_count", 250).
    WithConfidence(0.7)) // Let AI decide if it's a problem

// ❌ Bad: Make judgments
result.AddIssue(sdk.NewIssue().
    WithTitle("Function is too long")) // Who decided "too long"?
```

### 3. Actionable Issues

Issues should be specific and actionable:

```go
// ✅ Good
"Function processOrder at order.go:42 is 250 lines. "+
"Consider extracting payment processing (lines 100-150) and "+
"notification logic (lines 180-220) into separate functions."

// ❌ Bad
"This function is too long"
```

### 4. Evidence Over Assertions

Provide evidence for AI assessment:

```go
result.AddIssue(sdk.NewIssue().
    WithEvidence("complexity", cyclomaticComplexity).
    WithEvidence("nesting_depth", maxNestingDepth).
    WithEvidence("line_count", lineCount).
    Build())
```

### 5. Cost Awareness

Limit AI calls to control costs:

```go
const maxAIChecks = 20
for i, handler := range handlers {
    if i >= maxAIChecks {
        break
    }
    // AI analysis...
}
```

## Roadmap

Future enhancements:

- **Worker Marketplace** - Public registry of community workers
  ```bash
  vc worker install security/owasp-go
  ```

- **Version Management** - Worker dependencies and updates

- **Safety Review** - Malicious worker detection

- **Multi-Language** - SDK in Python, Rust, TypeScript

- **Streaming Results** - Real-time issue discovery

## Contributing

Have a great worker? Share it!

1. Add to `examples/` directory
2. Write tests
3. Document in TUTORIAL.md
4. Submit PR

See [CONTRIBUTING.md](../../../../CONTRIBUTING.md) for guidelines.

## Support

- **Issues**: https://github.com/steveyegge/vc/issues
- **Discussions**: https://github.com/steveyegge/vc/discussions
- **Discord**: https://discord.gg/vc (coming soon)

## License

MIT License - See [LICENSE](../../../../LICENSE)
