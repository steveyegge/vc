package discovery

import (
	"context"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/steveyegge/vc/internal/health"
)

// ArchitectureWorker analyzes codebase architecture and structure.
// Philosophy: 'Good architecture has clear boundaries, minimal coupling, and cohesive modules'
//
// This worker performs static analysis to discover architectural issues:
// - Circular dependencies
// - High coupling
// - God packages (too many responsibilities)
// - Layer violations
// - Missing abstractions (repeated patterns)
//
// ZFC Compliance: Collects facts about structure. AI determines if they're problems.
type ArchitectureWorker struct {
	// No configuration - uses distribution-based detection
}

// NewArchitectureWorker creates a new architecture analysis worker.
func NewArchitectureWorker() *ArchitectureWorker {
	return &ArchitectureWorker{}
}

// Name implements DiscoveryWorker.
func (w *ArchitectureWorker) Name() string {
	return "architecture"
}

// Philosophy implements DiscoveryWorker.
func (w *ArchitectureWorker) Philosophy() string {
	return "Good architecture has clear boundaries, minimal coupling, and cohesive modules"
}

// Scope implements DiscoveryWorker.
func (w *ArchitectureWorker) Scope() string {
	return "Package/module structure, import graphs, circular dependencies, coupling metrics, god packages, missing abstractions (repeated structs/interfaces/functions across packages)"
}

// Cost implements DiscoveryWorker.
func (w *ArchitectureWorker) Cost() health.CostEstimate {
	return health.CostEstimate{
		EstimatedDuration: 30 * time.Second,
		AICallsEstimated:  3, // One for overall assessment, plus specific issues
		RequiresFullScan:  true,
		Category:          health.CostModerate,
	}
}

// Dependencies implements DiscoveryWorker.
func (w *ArchitectureWorker) Dependencies() []string {
	return nil // No dependencies - this is a foundational worker
}

// Analyze implements DiscoveryWorker.
func (w *ArchitectureWorker) Analyze(ctx context.Context, codebase health.CodebaseContext) (*WorkerResult, error) {
	startTime := time.Now()

	result := &WorkerResult{
		IssuesDiscovered: []DiscoveredIssue{},
		AnalyzedAt:       startTime,
		Stats: AnalysisStats{
			FilesAnalyzed: 0,
			IssuesFound:   0,
		},
	}

	// Build package graph
	pkgGraph, err := w.buildPackageGraph(codebase.RootPath)
	if err != nil {
		return nil, fmt.Errorf("building package graph: %w", err)
	}

	result.Stats.FilesAnalyzed = pkgGraph.totalFiles

	// Detect circular dependencies
	cycles := w.detectCycles(pkgGraph)
	for _, cycle := range cycles {
		issue := DiscoveredIssue{
			Title:       fmt.Sprintf("Circular dependency detected: %s", strings.Join(cycle, " → ")),
			Description: fmt.Sprintf("Circular import chain: %s\n\nCircular dependencies make code harder to understand, test, and maintain. They often indicate unclear module boundaries or missing abstractions.", strings.Join(cycle, " → ")),
			Category:    "architecture",
			Type:        "task",
			Priority:    1, // P1 - architectural issues are important
			Tags:        []string{"circular-dependency", "coupling"},
			Evidence: map[string]interface{}{
				"cycle":        cycle,
				"cycle_length": len(cycle),
			},
			DiscoveredBy: w.Name(),
			DiscoveredAt: startTime,
			Confidence:   0.9, // High confidence - cycles are objective
		}
		result.IssuesDiscovered = append(result.IssuesDiscovered, issue)
	}

	// Detect god packages
	godPackages := w.detectGodPackages(pkgGraph)
	for _, pkg := range godPackages {
		issue := DiscoveredIssue{
			Title:       fmt.Sprintf("God package detected: %s (%d types)", pkg.name, pkg.typeCount),
			Description: fmt.Sprintf("Package %s contains %d types, significantly more than average (%d).\n\nGod packages with too many responsibilities are harder to understand and maintain. Consider splitting into focused sub-packages.", pkg.name, pkg.typeCount, pkgGraph.avgTypesPerPackage),
			Category:    "architecture",
			Type:        "task",
			Priority:    2, // P2 - quality improvement
			Tags:        []string{"god-package", "cohesion"},
			FilePath:    pkg.path,
			Evidence: map[string]interface{}{
				"type_count":    pkg.typeCount,
				"avg_types":     pkgGraph.avgTypesPerPackage,
				"threshold":     pkg.threshold,
				"package_name":  pkg.name,
				"package_path":  pkg.path,
			},
			DiscoveredBy: w.Name(),
			DiscoveredAt: startTime,
			Confidence:   0.7, // Medium-high confidence - needs AI to confirm if it's really a problem
		}
		result.IssuesDiscovered = append(result.IssuesDiscovered, issue)
	}

	// Calculate coupling metrics
	highCouplingPackages := w.detectHighCoupling(pkgGraph)
	for _, pkg := range highCouplingPackages {
		issue := DiscoveredIssue{
			Title:       fmt.Sprintf("High coupling detected: %s (fan-in: %d, fan-out: %d)", pkg.name, pkg.fanIn, pkg.fanOut),
			Description: fmt.Sprintf("Package %s has high coupling:\n- Imported by %d packages (fan-in)\n- Imports %d packages (fan-out)\n\nHigh coupling indicates tight dependencies that make code harder to change and test independently.", pkg.name, pkg.fanIn, pkg.fanOut),
			Category:    "architecture",
			Type:        "task",
			Priority:    2, // P2 - quality improvement
			Tags:        []string{"coupling", "dependencies"},
			FilePath:    pkg.path,
			Evidence: map[string]interface{}{
				"fan_in":       pkg.fanIn,
				"fan_out":      pkg.fanOut,
				"coupling":     pkg.fanIn + pkg.fanOut,
				"package_name": pkg.name,
				"package_path": pkg.path,
			},
			DiscoveredBy: w.Name(),
			DiscoveredAt: startTime,
			Confidence:   0.6, // Medium confidence - AI should assess if coupling is excessive
		}
		result.IssuesDiscovered = append(result.IssuesDiscovered, issue)
	}

	// Detect missing abstractions
	missingAbstractions := w.detectMissingAbstractions(pkgGraph)
	for _, abs := range missingAbstractions {
		issue := DiscoveredIssue{
			Title:       fmt.Sprintf("Missing abstraction: %d similar %s across %d packages", len(abs.instances), abs.patternType, len(abs.packages)),
			Description: fmt.Sprintf("Found %d similar %s definitions across packages: %s\n\nSimilar structures:\n%s\n\nRepeated patterns may indicate a missing abstraction. Consider extracting a shared type or interface if these represent the same concept.", len(abs.instances), abs.patternType, strings.Join(abs.packages, ", "), abs.exampleCode),
			Category:    "architecture",
			Type:        "task",
			Priority:    2, // P2 - quality improvement
			Tags:        []string{"duplication", "abstraction", "design"},
			Evidence: map[string]interface{}{
				"pattern_type":   abs.patternType,
				"instance_count": len(abs.instances),
				"packages":       abs.packages,
				"similarity":     abs.similarityScore,
				"instances":      abs.instances,
			},
			DiscoveredBy: w.Name(),
			DiscoveredAt: startTime,
			Confidence:   0.5, // Medium confidence - AI should determine if duplication is problematic
		}
		result.IssuesDiscovered = append(result.IssuesDiscovered, issue)
	}

	// Build context and reasoning for AI
	result.Context = fmt.Sprintf(
		"Analyzed %d packages with %d total files. Found %d circular dependencies, %d god packages, %d high-coupling packages, %d missing abstractions.",
		len(pkgGraph.packages),
		pkgGraph.totalFiles,
		len(cycles),
		len(godPackages),
		len(highCouplingPackages),
		len(missingAbstractions),
	)

	result.Reasoning = fmt.Sprintf(
		"Based on philosophy: '%s'\n\nPackage structure analysis revealed potential issues:\n"+
			"- Circular dependencies prevent clean layering and testability\n"+
			"- God packages suggest unclear responsibilities\n"+
			"- High coupling makes changes ripple across the codebase\n"+
			"- Missing abstractions indicate repeated patterns that could be unified\n\n"+
			"AI should evaluate: Are these issues worth addressing given the codebase context?",
		w.Philosophy(),
	)

	result.Stats.IssuesFound = len(result.IssuesDiscovered)
	result.Stats.Duration = time.Since(startTime)
	result.Stats.PatternsFound = len(cycles) + len(godPackages) + len(highCouplingPackages) + len(missingAbstractions)

	return result, nil
}

// packageGraph represents the import graph of a codebase.
type packageGraph struct {
	packages           map[string]*packageInfo
	totalFiles         int
	avgTypesPerPackage int
}

// packageInfo contains metadata about a package.
type packageInfo struct {
	name      string
	path      string
	imports   []string // Packages this package imports
	importedBy []string // Packages that import this package
	typeCount int      // Number of types defined
	files     []string // Go files in this package
}

// getModuleName extracts the module name from go.mod file.
func getModuleName(rootPath string) (string, error) {
	goModPath := filepath.Join(rootPath, "go.mod")
	data, err := os.ReadFile(goModPath)
	if err != nil {
		return "", fmt.Errorf("reading go.mod: %w", err)
	}

	// Parse module line (first non-comment line starting with "module")
	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "module ") {
			return strings.TrimSpace(strings.TrimPrefix(line, "module")), nil
		}
	}

	return "", fmt.Errorf("module name not found in go.mod")
}

// buildPackageGraph constructs the import graph for a codebase.
func (w *ArchitectureWorker) buildPackageGraph(rootPath string) (*packageGraph, error) {
	// Extract module name from go.mod
	moduleName, err := getModuleName(rootPath)
	if err != nil {
		return nil, fmt.Errorf("getting module name: %w", err)
	}

	graph := &packageGraph{
		packages: make(map[string]*packageInfo),
	}

	// Walk the directory tree and parse Go files
	err = filepath.Walk(rootPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Skip vendor and hidden directories (vc-dkho fix: check IsDir first and return SkipDir)
		if info.IsDir() {
			if strings.Contains(path, "/vendor/") || strings.Contains(path, "/.") {
				return filepath.SkipDir
			}
			return nil // Skip other directories but continue traversing
		}

		// Skip non-Go files
		if !strings.HasSuffix(path, ".go") {
			return nil
		}

		// Skip test files and generated files
		if strings.HasSuffix(path, "_test.go") || strings.Contains(path, "_generated.go") {
			return nil
		}

		graph.totalFiles++

		// Parse the file to extract package and imports
		fset := token.NewFileSet()
		node, err := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
		if err != nil {
			// Skip files that fail to parse
			return nil
		}

		pkgName := node.Name.Name
		pkgPath := filepath.Dir(path)

		// Get or create package info
		pkg, exists := graph.packages[pkgPath]
		if !exists {
			pkg = &packageInfo{
				name:    pkgName,
				path:    pkgPath,
				imports: []string{},
				files:   []string{},
			}
			graph.packages[pkgPath] = pkg
		}

		pkg.files = append(pkg.files, path)

		// Extract imports
		for _, imp := range node.Imports {
			importPath := strings.Trim(imp.Path.Value, "\"")
			// Only track internal imports (within the project)
			// Internal imports start with the module name (e.g., "github.com/steveyegge/vc/...")
			if strings.HasPrefix(importPath, moduleName) {
				if !contains(pkg.imports, importPath) {
					pkg.imports = append(pkg.imports, importPath)
				}
			}
		}

		return nil
	})

	if err != nil {
		return nil, err
	}

	// Build reverse dependency map (importedBy)
	for pkgPath, pkg := range graph.packages {
		for _, importPath := range pkg.imports {
			if importedPkg, exists := graph.packages[importPath]; exists {
				importedPkg.importedBy = append(importedPkg.importedBy, pkgPath)
			}
		}
	}

	// Count types in each package
	totalTypes := 0
	for _, pkg := range graph.packages {
		typeCount := w.countTypes(pkg.files)
		pkg.typeCount = typeCount
		totalTypes += typeCount
	}

	if len(graph.packages) > 0 {
		graph.avgTypesPerPackage = totalTypes / len(graph.packages)
	}

	return graph, nil
}

// countTypes counts the number of type declarations in a set of files.
func (w *ArchitectureWorker) countTypes(files []string) int {
	count := 0
	for _, file := range files {
		fset := token.NewFileSet()
		node, err := parser.ParseFile(fset, file, nil, 0)
		if err != nil {
			continue
		}

		for _, decl := range node.Decls {
			if _, ok := decl.(*ast.GenDecl); ok {
				genDecl := decl.(*ast.GenDecl)
				if genDecl.Tok == token.TYPE {
					count += len(genDecl.Specs)
				}
			}
		}
	}
	return count
}

// detectCycles finds circular dependencies using Tarjan's algorithm.
func (w *ArchitectureWorker) detectCycles(graph *packageGraph) [][]string {
	var cycles [][]string

	// Tarjan's algorithm state
	index := 0
	stack := []string{}
	indices := make(map[string]int)
	lowlinks := make(map[string]int)
	onStack := make(map[string]bool)

	var strongConnect func(string)
	strongConnect = func(v string) {
		indices[v] = index
		lowlinks[v] = index
		index++
		stack = append(stack, v)
		onStack[v] = true

		pkg := graph.packages[v]
		for _, w := range pkg.imports {
			if _, exists := graph.packages[w]; !exists {
				continue
			}

			if _, visited := indices[w]; !visited {
				strongConnect(w)
				if lowlinks[w] < lowlinks[v] {
					lowlinks[v] = lowlinks[w]
				}
			} else if onStack[w] {
				if indices[w] < lowlinks[v] {
					lowlinks[v] = indices[w]
				}
			}
		}

		if lowlinks[v] == indices[v] {
			var cycle []string
			for {
				w := stack[len(stack)-1]
				stack = stack[:len(stack)-1]
				onStack[w] = false
				cycle = append(cycle, graph.packages[w].name)
				if w == v {
					break
				}
			}
			if len(cycle) > 1 {
				cycles = append(cycles, cycle)
			}
		}
	}

	for pkgPath := range graph.packages {
		if _, visited := indices[pkgPath]; !visited {
			strongConnect(pkgPath)
		}
	}

	return cycles
}

// godPackageCandidate represents a package that might be a "god package".
type godPackageCandidate struct {
	name      string
	path      string
	typeCount int
	threshold float64 // Calculated threshold (mean + 2*stddev)
}

// detectGodPackages finds packages with too many types using distribution-based detection.
// ZFC Compliance: Uses statistical analysis instead of hardcoded thresholds.
func (w *ArchitectureWorker) detectGodPackages(graph *packageGraph) []godPackageCandidate {
	var candidates []godPackageCandidate

	// Collect all type counts
	var typeCounts []int
	for _, pkg := range graph.packages {
		typeCounts = append(typeCounts, pkg.typeCount)
	}

	// Need at least 2 packages to calculate meaningful statistics
	if len(typeCounts) < 2 {
		return candidates
	}

	// Calculate mean
	sum := 0
	for _, count := range typeCounts {
		sum += count
	}
	mean := float64(sum) / float64(len(typeCounts))

	// Calculate standard deviation
	varianceSum := 0.0
	for _, count := range typeCounts {
		diff := float64(count) - mean
		varianceSum += diff * diff
	}
	stdDev := math.Sqrt(varianceSum / float64(len(typeCounts)))

	// Use 2 standard deviations above mean as threshold (captures ~95% of normal distribution)
	// This makes detection adaptive to the codebase's actual distribution
	threshold := mean + (2.0 * stdDev)

	// Find packages that exceed the threshold
	for _, pkg := range graph.packages {
		if float64(pkg.typeCount) > threshold {
			candidates = append(candidates, godPackageCandidate{
				name:      pkg.name,
				path:      pkg.path,
				typeCount: pkg.typeCount,
				threshold: threshold,
			})
		}
	}

	return candidates
}

// couplingCandidate represents a package with high coupling.
type couplingCandidate struct {
	name   string
	path   string
	fanIn  int // Number of packages that import this package
	fanOut int // Number of packages this package imports
}

// detectHighCoupling finds packages with high fan-in or fan-out.
func (w *ArchitectureWorker) detectHighCoupling(graph *packageGraph) []couplingCandidate {
	var candidates []couplingCandidate

	// Calculate thresholds (packages above 75th percentile)
	var fanIns []int
	var fanOuts []int
	for _, pkg := range graph.packages {
		fanIns = append(fanIns, len(pkg.importedBy))
		fanOuts = append(fanOuts, len(pkg.imports))
	}

	fanInThreshold := percentile(fanIns, 0.75)
	fanOutThreshold := percentile(fanOuts, 0.75)

	for _, pkg := range graph.packages {
		fanIn := len(pkg.importedBy)
		fanOut := len(pkg.imports)

		if fanIn > fanInThreshold || fanOut > fanOutThreshold {
			candidates = append(candidates, couplingCandidate{
				name:   pkg.name,
				path:   pkg.path,
				fanIn:  fanIn,
				fanOut: fanOut,
			})
		}
	}

	return candidates
}

// percentile calculates the nth percentile of a slice of integers using linear interpolation.
// Uses the "R-7" method (Excel PERCENTILE function) for compatibility.
func percentile(values []int, p float64) int {
	if len(values) == 0 {
		return 0
	}
	if len(values) == 1 {
		return values[0]
	}

	// Sort values using stdlib for efficiency
	sorted := make([]int, len(values))
	copy(sorted, values)
	sort.Ints(sorted)

	// Handle boundary cases
	if p <= 0 {
		return sorted[0]
	}
	if p >= 1 {
		return sorted[len(sorted)-1]
	}

	// Calculate position using linear interpolation (R-7 method)
	// Position h = (n-1)*p, where n is the number of elements
	h := float64(len(sorted)-1) * p

	// Lower and upper indices for interpolation
	lower := int(math.Floor(h))
	upper := int(math.Ceil(h))

	// If h is exactly on an index, return that value
	if lower == upper {
		return sorted[lower]
	}

	// Linear interpolation between lower and upper values
	fraction := h - float64(lower)
	result := float64(sorted[lower]) + fraction*float64(sorted[upper]-sorted[lower])

	return int(math.Round(result))
}

// missingAbstraction represents a pattern of repeated code structures.
type missingAbstraction struct {
	patternType     string                 // "struct", "interface", "function signature"
	instances       []abstractionInstance  // All occurrences of this pattern
	packages        []string               // Unique packages where pattern appears
	similarityScore float64                // 0.0-1.0 similarity score
	exampleCode     string                 // Sample code showing the duplication
}

// abstractionInstance represents one occurrence of a repeated pattern.
type abstractionInstance struct {
	name     string            // Type or function name
	pkg      string            // Package name
	file     string            // File path
	line     int               // Line number
	fields   []string          // For structs: field names and types
	params   []string          // For functions: parameter types
	returns  []string          // For functions: return types
	metadata map[string]string // Additional context
}

// detectMissingAbstractions finds repeated struct/interface/function patterns across packages.
func (w *ArchitectureWorker) detectMissingAbstractions(graph *packageGraph) []missingAbstraction {
	var abstractions []missingAbstraction

	// Extract all type definitions and function signatures
	structs := make(map[string][]abstractionInstance)
	interfaces := make(map[string][]abstractionInstance)
	functionSigs := make(map[string][]abstractionInstance)

	for _, pkg := range graph.packages {
		for _, file := range pkg.files {
			fset := token.NewFileSet()
			node, err := parser.ParseFile(fset, file, nil, 0)
			if err != nil {
				continue
			}

			// Extract type definitions
			for _, decl := range node.Decls {
				genDecl, ok := decl.(*ast.GenDecl)
				if !ok || genDecl.Tok != token.TYPE {
					continue
				}

				for _, spec := range genDecl.Specs {
					typeSpec, ok := spec.(*ast.TypeSpec)
					if !ok {
						continue
					}

					pos := fset.Position(typeSpec.Pos())

					// Handle struct types
					if structType, ok := typeSpec.Type.(*ast.StructType); ok {
						fields := w.extractStructFields(structType)
						signature := w.structSignature(fields)

						instance := abstractionInstance{
							name:   typeSpec.Name.Name,
							pkg:    pkg.name,
							file:   file,
							line:   pos.Line,
							fields: fields,
						}
						structs[signature] = append(structs[signature], instance)
					}

					// Handle interface types
					if interfaceType, ok := typeSpec.Type.(*ast.InterfaceType); ok {
						methods := w.extractInterfaceMethods(interfaceType)
						signature := w.interfaceSignature(methods)

						instance := abstractionInstance{
							name:     typeSpec.Name.Name,
							pkg:      pkg.name,
							file:     file,
							line:     pos.Line,
							params:   methods, // Store methods in params field
						}
						interfaces[signature] = append(interfaces[signature], instance)
					}
				}
			}

			// Extract function signatures
			for _, decl := range node.Decls {
				funcDecl, ok := decl.(*ast.FuncDecl)
				if !ok || funcDecl.Recv != nil { // Skip methods
					continue
				}

				pos := fset.Position(funcDecl.Pos())
				params := w.extractFunctionParams(funcDecl.Type.Params)
				returns := w.extractFunctionReturns(funcDecl.Type.Results)
				signature := w.functionSignature(params, returns)

				instance := abstractionInstance{
					name:    funcDecl.Name.Name,
					pkg:     pkg.name,
					file:    file,
					line:    pos.Line,
					params:  params,
					returns: returns,
				}
				functionSigs[signature] = append(functionSigs[signature], instance)
			}
		}
	}

	// Analyze struct patterns
	for _, instances := range structs {
		if len(instances) >= 2 && w.spansMultiplePackages(instances) {
			pkgs := w.uniquePackages(instances)
			exampleCode := w.generateStructExample(instances)

			abstractions = append(abstractions, missingAbstraction{
				patternType:     "struct",
				instances:       instances,
				packages:        pkgs,
				similarityScore: 1.0, // Exact signature match
				exampleCode:     exampleCode,
			})
		}
	}

	// Analyze interface patterns
	for _, instances := range interfaces {
		if len(instances) >= 2 && w.spansMultiplePackages(instances) {
			pkgs := w.uniquePackages(instances)
			exampleCode := w.generateInterfaceExample(instances)

			abstractions = append(abstractions, missingAbstraction{
				patternType:     "interface",
				instances:       instances,
				packages:        pkgs,
				similarityScore: 1.0,
				exampleCode:     exampleCode,
			})
		}
	}

	// Analyze function signature patterns
	for _, instances := range functionSigs {
		if len(instances) >= 3 && w.spansMultiplePackages(instances) {
			pkgs := w.uniquePackages(instances)
			exampleCode := w.generateFunctionExample(instances)

			abstractions = append(abstractions, missingAbstraction{
				patternType:     "function",
				instances:       instances,
				packages:        pkgs,
				similarityScore: 1.0,
				exampleCode:     exampleCode,
			})
		}
	}

	return abstractions
}

// extractStructFields extracts field names and types from a struct.
func (w *ArchitectureWorker) extractStructFields(structType *ast.StructType) []string {
	var fields []string
	if structType.Fields == nil {
		return fields
	}

	for _, field := range structType.Fields.List {
		typeStr := w.typeToString(field.Type)
		if len(field.Names) == 0 {
			// Embedded field
			fields = append(fields, typeStr)
		} else {
			for _, name := range field.Names {
				fields = append(fields, fmt.Sprintf("%s %s", name.Name, typeStr))
			}
		}
	}
	return fields
}

// extractInterfaceMethods extracts method signatures from an interface.
func (w *ArchitectureWorker) extractInterfaceMethods(interfaceType *ast.InterfaceType) []string {
	var methods []string
	if interfaceType.Methods == nil {
		return methods
	}

	for _, method := range interfaceType.Methods.List {
		if len(method.Names) == 0 {
			// Embedded interface
			methods = append(methods, w.typeToString(method.Type))
			continue
		}

		if funcType, ok := method.Type.(*ast.FuncType); ok {
			for _, name := range method.Names {
				params := w.extractFunctionParams(funcType.Params)
				returns := w.extractFunctionReturns(funcType.Results)
				sig := fmt.Sprintf("%s(%s) %s", name.Name, strings.Join(params, ", "), strings.Join(returns, ", "))
				methods = append(methods, sig)
			}
		}
	}
	return methods
}

// extractFunctionParams extracts parameter types from a function.
func (w *ArchitectureWorker) extractFunctionParams(params *ast.FieldList) []string {
	if params == nil {
		return []string{}
	}

	var result []string
	for _, param := range params.List {
		typeStr := w.typeToString(param.Type)
		if len(param.Names) == 0 {
			result = append(result, typeStr)
		} else {
			for range param.Names {
				result = append(result, typeStr)
			}
		}
	}
	return result
}

// extractFunctionReturns extracts return types from a function.
func (w *ArchitectureWorker) extractFunctionReturns(results *ast.FieldList) []string {
	if results == nil {
		return []string{}
	}

	var result []string
	for _, ret := range results.List {
		typeStr := w.typeToString(ret.Type)
		if len(ret.Names) == 0 {
			result = append(result, typeStr)
		} else {
			for range ret.Names {
				result = append(result, typeStr)
			}
		}
	}
	return result
}

// typeToString converts an AST type to a string representation.
func (w *ArchitectureWorker) typeToString(expr ast.Expr) string {
	switch t := expr.(type) {
	case *ast.Ident:
		return t.Name
	case *ast.SelectorExpr:
		return fmt.Sprintf("%s.%s", w.typeToString(t.X), t.Sel.Name)
	case *ast.StarExpr:
		return "*" + w.typeToString(t.X)
	case *ast.ArrayType:
		return "[]" + w.typeToString(t.Elt)
	case *ast.MapType:
		return fmt.Sprintf("map[%s]%s", w.typeToString(t.Key), w.typeToString(t.Value))
	case *ast.InterfaceType:
		return "interface{}"
	case *ast.FuncType:
		return "func"
	case *ast.ChanType:
		return "chan " + w.typeToString(t.Value)
	default:
		return "unknown"
	}
}

// structSignature generates a canonical signature for a struct (based on field types).
func (w *ArchitectureWorker) structSignature(fields []string) string {
	// Normalize by removing field names, keeping only types
	var types []string
	for _, field := range fields {
		parts := strings.Fields(field)
		if len(parts) > 1 {
			types = append(types, parts[len(parts)-1]) // Last part is the type
		} else {
			types = append(types, field)
		}
	}
	sort.Strings(types)
	return "struct{" + strings.Join(types, ";") + "}"
}

// interfaceSignature generates a canonical signature for an interface.
func (w *ArchitectureWorker) interfaceSignature(methods []string) string {
	sorted := make([]string, len(methods))
	copy(sorted, methods)
	sort.Strings(sorted)
	return "interface{" + strings.Join(sorted, ";") + "}"
}

// functionSignature generates a canonical signature for a function.
func (w *ArchitectureWorker) functionSignature(params, returns []string) string {
	return fmt.Sprintf("func(%s)(%s)", strings.Join(params, ","), strings.Join(returns, ","))
}

// spansMultiplePackages checks if instances appear in different packages.
func (w *ArchitectureWorker) spansMultiplePackages(instances []abstractionInstance) bool {
	pkgs := make(map[string]bool)
	for _, inst := range instances {
		pkgs[inst.pkg] = true
	}
	return len(pkgs) > 1
}

// uniquePackages extracts unique package names from instances.
func (w *ArchitectureWorker) uniquePackages(instances []abstractionInstance) []string {
	pkgs := make(map[string]bool)
	for _, inst := range instances {
		pkgs[inst.pkg] = true
	}

	var result []string
	for pkg := range pkgs {
		result = append(result, pkg)
	}
	sort.Strings(result)
	return result
}

// generateStructExample creates example code showing struct duplication.
func (w *ArchitectureWorker) generateStructExample(instances []abstractionInstance) string {
	if len(instances) == 0 {
		return ""
	}

	var examples []string
	for i, inst := range instances {
		if i >= 3 { // Show at most 3 examples
			break
		}
		examples = append(examples, fmt.Sprintf("// %s.%s (%s:%d)\ntype %s struct {\n  %s\n}",
			inst.pkg, inst.name, filepath.Base(inst.file), inst.line, inst.name, strings.Join(inst.fields, "\n  ")))
	}
	return strings.Join(examples, "\n\n")
}

// generateInterfaceExample creates example code showing interface duplication.
func (w *ArchitectureWorker) generateInterfaceExample(instances []abstractionInstance) string {
	if len(instances) == 0 {
		return ""
	}

	var examples []string
	for i, inst := range instances {
		if i >= 3 {
			break
		}
		examples = append(examples, fmt.Sprintf("// %s.%s (%s:%d)\ntype %s interface {\n  %s\n}",
			inst.pkg, inst.name, filepath.Base(inst.file), inst.line, inst.name, strings.Join(inst.params, "\n  ")))
	}
	return strings.Join(examples, "\n\n")
}

// generateFunctionExample creates example code showing function signature duplication.
func (w *ArchitectureWorker) generateFunctionExample(instances []abstractionInstance) string {
	if len(instances) == 0 {
		return ""
	}

	var examples []string
	for i, inst := range instances {
		if i >= 3 {
			break
		}
		examples = append(examples, fmt.Sprintf("// %s.%s (%s:%d)\nfunc %s(%s) (%s)",
			inst.pkg, inst.name, filepath.Base(inst.file), inst.line, inst.name,
			strings.Join(inst.params, ", "), strings.Join(inst.returns, ", ")))
	}
	return strings.Join(examples, "\n\n")
}
