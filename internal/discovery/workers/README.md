# Discovery Workers

This package provides specialized workers for deep codebase analysis during Discovery Mode.

## Available Workers

### 1. DocAuditor

**Philosophy**: "Code should be self-documenting, but critical APIs need explicit documentation"

**Analyzes**:
- Missing package documentation
- Undocumented exported functions and types
- README quality (installation, usage, examples)
- API documentation coverage

**Cost**: Moderate (2-5 AI calls, ~1 minute)

**Example Issues**:
- "Package storage missing documentation"
- "README missing installation instructions"
- "Exported type Config lacks documentation"

### 2. TestCoverageAnalyzer

**Philosophy**: "Tests should cover critical paths, edge cases, and error conditions"

**Analyzes**:
- Untested source files (no corresponding test files)
- Packages with no tests
- Weak test suites (< 3 tests)
- Missing integration tests
- Test quality (suggests table-driven tests)

**Cost**: Expensive (10+ AI calls, ~3 minutes)

**Example Issues**:
- "No tests for internal/storage/store.go"
- "Test file config_test.go has only 2 tests"
- "Missing integration tests"

### 3. DependencyAuditor

**Philosophy**: "Dependencies should be up-to-date, secure, and necessary"

**Analyzes**:
- Outdated Go version
- Outdated dependencies (checks latest versions via proxy.golang.org)
- Security vulnerabilities (via OSV.dev API)
- Direct dependencies only (ignores indirect)

**Cost**: Cheap (API calls only, 1-2 AI calls, ~30 seconds)

**External APIs**:
- `proxy.golang.org` - Latest version lookup
- `api.osv.dev` - Vulnerability database

**Example Issues**:
- "Go version 1.18 is outdated"
- "Security vulnerability in github.com/foo/bar: CVE-2024-1234"
- "Outdated dependency: github.com/x/y is at v1.2.0 but v2.0.0 is available"

### 4. SecurityScanner

**Philosophy**: "Security vulnerabilities should be caught before production"

**Analyzes**:
- Credential leaks (API keys, passwords, tokens via regex)
- SQL injection (string concatenation in queries)
- Command injection (string concat in exec.Command)
- Weak cryptography (MD5, SHA1, DES usage)
- OWASP Top 10 patterns

**Cost**: Expensive (5-10 AI calls, ~2 minutes)

**Example Issues**:
- "Potential credential leak in config.go:42"
- "Potential SQL injection in database.go:156"
- "Weak cryptography in crypto.go:89 (uses MD5)"

## Usage

### Registering Workers

```go
import (
	"github.com/steveyegge/vc/internal/discovery"
	"github.com/steveyegge/vc/internal/discovery/workers"
)

// Create registry
registry := discovery.NewWorkerRegistry()

// Register all workers
workers.RegisterAll(registry)

// Or register individually
registry.Register(workers.NewDocAuditor())
registry.Register(workers.NewTestCoverageAnalyzer())
registry.Register(workers.NewDependencyAuditor())
registry.Register(workers.NewSecurityScanner())
```

### Running Discovery

```go
// Get workers for a preset
workers, err := registry.GetPresetWorkers(discovery.PresetStandard)
if err != nil {
	log.Fatal(err)
}

// Run orchestrator
orchestrator := discovery.NewOrchestrator(registry, config)
result, err := orchestrator.Run(ctx)
```

## Preset Configurations

### Quick ($0.50, 1 minute)
- `file_size_monitor`
- `cruft_detector`
- `dependency_auditor`

### Standard ($2.00, 5 minutes)
- `file_size_monitor`
- `cruft_detector`
- `duplication_detector`
- `doc_auditor`
- `dependency_auditor`

### Thorough ($10.00, 15 minutes)
- `file_size_monitor`
- `cruft_detector`
- `duplication_detector`
- `zfc_detector`
- `doc_auditor`
- `test_coverage_analyzer`
- `dependency_auditor`
- `security_scanner`

## Design Principles

### Zero Framework Cognition (ZFC)

Workers **collect facts and patterns**. AI supervision **interprets them**.

**Good** (ZFC-compliant):
```go
// Worker finds the pattern
issue := DiscoveredIssue{
	Title: "No tests for storage.go",
	Evidence: map[string]interface{}{
		"expected_test_file": "storage_test.go",
		"package_has_tests": false,
	},
}
```

**Bad** (framework cognition):
```go
// Don't hardcode judgments
if fileSize > 1000 {  // ❌ Arbitrary threshold
	return "File too large"
}
```

### Composability

Workers run independently and share `CodebaseContext`:

```go
// Built once, shared by all workers
context := builder.Build(ctx)

// Each worker uses same context
result1 := docWorker.Analyze(ctx, context)
result2 := testWorker.Analyze(ctx, context)
```

### Cost Awareness

Workers declare cost estimates for budget enforcement:

```go
func (w *Worker) Cost() health.CostEstimate {
	return health.CostEstimate{
		EstimatedDuration: 1 * time.Minute,
		AICallsEstimated: 5,
		Category: health.CostModerate,
	}
}
```

## Implementation Notes

### External API Integration

**DependencyAuditor** uses external APIs:
- Rate limiting: Built into `http.Client` timeout
- Error handling: Silently skips failed API calls
- Caching: Not implemented (queries are cheap)

**SecurityScanner** uses regex patterns:
- Compiled once at initialization
- Patterns tuned to minimize false positives
- AI assessment filters false positives later

### AST Analysis

Workers parse Go code using `go/ast`:

```go
fset := token.NewFileSet()
node, _ := parser.ParseFile(fset, path, nil, parser.ParseComments)

ast.Inspect(node, func(n ast.Node) bool {
	// Analyze AST nodes
	return true
})
```

### Issue Evidence

Workers provide rich evidence for AI assessment:

```go
Evidence: map[string]interface{}{
	"package_name": "storage",
	"file_path": "internal/storage/store.go",
	"has_doc": false,
	"line_count": 450,
}
```

## Future Enhancements

Potential additions (see vc-c9an for infrastructure workers):
- **BuildModernizer**: Makefile, go.mod quality
- **CICDReviewer**: GitHub Actions, CI/CD analysis
- **ArchitectureScanner**: Package structure, layering
- **BugDetector**: Common Go bug patterns

## Testing

```bash
# Build workers
go build ./internal/discovery/workers/...

# Run tests (when added)
go test ./internal/discovery/workers/...

# Lint
golangci-lint run ./internal/discovery/workers/...
```

## See Also

- [../../docs/FEATURES.md](../../docs/FEATURES.md) - Discovery Mode overview
- [../types.go](../types.go) - DiscoveryWorker interface
- [../orchestrator.go](../orchestrator.go) - Worker execution
