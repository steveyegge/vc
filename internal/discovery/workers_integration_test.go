package discovery

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/steveyegge/vc/internal/health"
)

// TestArchitectureWorker_Integration tests the ArchitectureWorker on the VC codebase.
func TestArchitectureWorker_Integration(t *testing.T) {
	// Get root of VC project (assuming test is run from project root or internal/discovery)
	rootPath, err := filepath.Abs("../..")
	if err != nil {
		t.Fatalf("Failed to get root path: %v", err)
	}

	// Build codebase context
	builder := NewContextBuilder(rootPath, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	codebaseCtx, err := builder.Build(ctx)
	if err != nil {
		t.Fatalf("Failed to build codebase context: %v", err)
	}

	// Verify context was built
	if codebaseCtx.RootPath == "" {
		t.Error("Expected RootPath to be set in CodebaseContext")
	}
	if codebaseCtx.TotalFiles == 0 {
		t.Error("Expected some files to be scanned")
	}

	// Create and run architecture worker
	worker := NewArchitectureWorker()
	result, err := worker.Analyze(ctx, codebaseCtx)
	if err != nil {
		t.Fatalf("Architecture worker failed: %v", err)
	}

	// Verify result structure
	if result == nil {
		t.Fatal("Expected non-nil result")
	}

	if result.Stats.FilesAnalyzed == 0 {
		t.Error("Expected some files to be analyzed")
	}

	// Log what was found (informational)
	t.Logf("Architecture Analysis Results:")
	t.Logf("  Files analyzed: %d", result.Stats.FilesAnalyzed)
	t.Logf("  Issues found: %d", result.Stats.IssuesFound)
	t.Logf("  Patterns found: %d", result.Stats.PatternsFound)
	t.Logf("  Duration: %v", result.Stats.Duration)

	// Log sample issues
	for i, issue := range result.IssuesDiscovered {
		if i >= 3 {
			break // Only show first 3
		}
		t.Logf("  Issue %d: %s (confidence: %.2f)", i+1, issue.Title, issue.Confidence)
	}

	// Verify issues have required fields
	for i, issue := range result.IssuesDiscovered {
		if issue.Title == "" {
			t.Errorf("Issue %d has empty title", i)
		}
		if issue.Description == "" {
			t.Errorf("Issue %d has empty description", i)
		}
		if issue.Category == "" {
			t.Errorf("Issue %d has empty category", i)
		}
		if issue.DiscoveredBy != worker.Name() {
			t.Errorf("Issue %d has wrong DiscoveredBy: got %q, want %q", i, issue.DiscoveredBy, worker.Name())
		}
		if issue.Confidence < 0 || issue.Confidence > 1 {
			t.Errorf("Issue %d has invalid confidence: %f", i, issue.Confidence)
		}
	}
}

// TestBugHunterWorker_Integration tests the BugHunterWorker on the VC codebase.
func TestBugHunterWorker_Integration(t *testing.T) {
	// Get root of VC project
	rootPath, err := filepath.Abs("../..")
	if err != nil {
		t.Fatalf("Failed to get root path: %v", err)
	}

	// Build codebase context
	builder := NewContextBuilder(rootPath, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	codebaseCtx, err := builder.Build(ctx)
	if err != nil {
		t.Fatalf("Failed to build codebase context: %v", err)
	}

	// Create and run bug hunter worker
	worker := NewBugHunterWorker()
	result, err := worker.Analyze(ctx, codebaseCtx)
	if err != nil {
		t.Fatalf("Bug hunter worker failed: %v", err)
	}

	// Verify result structure
	if result == nil {
		t.Fatal("Expected non-nil result")
	}

	if result.Stats.FilesAnalyzed == 0 {
		t.Error("Expected some files to be analyzed")
	}

	// Log what was found (informational)
	t.Logf("Bug Hunter Analysis Results:")
	t.Logf("  Files analyzed: %d", result.Stats.FilesAnalyzed)
	t.Logf("  Issues found: %d", result.Stats.IssuesFound)
	t.Logf("  Patterns found: %d", result.Stats.PatternsFound)
	t.Logf("  Duration: %v", result.Stats.Duration)
	t.Logf("  Errors ignored: %d", result.Stats.ErrorsIgnored)

	// Log sample issues
	for i, issue := range result.IssuesDiscovered {
		if i >= 5 {
			break // Only show first 5
		}
		t.Logf("  Issue %d: %s (P%d, confidence: %.2f)", i+1, issue.Title, issue.Priority, issue.Confidence)
	}

	// Verify issues have required fields
	for i, issue := range result.IssuesDiscovered {
		if issue.Title == "" {
			t.Errorf("Issue %d has empty title", i)
		}
		if issue.Description == "" {
			t.Errorf("Issue %d has empty description", i)
		}
		if issue.Category == "" {
			t.Errorf("Issue %d has empty category", i)
		}
		if issue.DiscoveredBy != worker.Name() {
			t.Errorf("Issue %d has wrong DiscoveredBy: got %q, want %q", i, issue.DiscoveredBy, worker.Name())
		}
		if issue.FilePath == "" {
			t.Errorf("Issue %d has no file path", i)
		}
		if issue.Confidence < 0 || issue.Confidence > 1 {
			t.Errorf("Issue %d has invalid confidence: %f", i, issue.Confidence)
		}
	}
}

// TestWorkerDependency_Integration tests that bug worker can use architecture worker results.
func TestWorkerDependency_Integration(t *testing.T) {
	// Get root of VC project
	rootPath, err := filepath.Abs("../..")
	if err != nil {
		t.Fatalf("Failed to get root path: %v", err)
	}

	// Build codebase context
	builder := NewContextBuilder(rootPath, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	codebaseCtx, err := builder.Build(ctx)
	if err != nil {
		t.Fatalf("Failed to build codebase context: %v", err)
	}

	// Verify dependency is declared correctly
	bugWorker := NewBugHunterWorker()
	deps := bugWorker.Dependencies()
	if len(deps) == 0 {
		t.Error("Expected bug worker to have dependencies")
	}

	found := false
	for _, dep := range deps {
		if dep == "architecture" {
			found = true
		}
	}
	if !found {
		t.Error("Expected bug worker to depend on architecture worker")
	}

	// Verify both workers run successfully
	archWorker := NewArchitectureWorker()
	archResult, err := archWorker.Analyze(ctx, codebaseCtx)
	if err != nil {
		t.Fatalf("Architecture worker failed: %v", err)
	}

	bugResult, err := bugWorker.Analyze(ctx, codebaseCtx)
	if err != nil {
		t.Fatalf("Bug worker failed: %v", err)
	}

	t.Logf("Workers run successfully in dependency order:")
	t.Logf("  Architecture: %d issues, %v", len(archResult.IssuesDiscovered), archResult.Stats.Duration)
	t.Logf("  Bug Hunter: %d issues, %v", len(bugResult.IssuesDiscovered), bugResult.Stats.Duration)
}

// TestWorkerRegistry_Integration tests that workers are registered correctly.
func TestWorkerRegistry_Integration(t *testing.T) {
	// Create registry
	registry := NewWorkerRegistry()

	// Register workers
	archWorker := NewArchitectureWorker()
	if err := registry.Register(archWorker); err != nil {
		t.Fatalf("Failed to register architecture worker: %v", err)
	}

	bugWorker := NewBugHunterWorker()
	if err := registry.Register(bugWorker); err != nil {
		t.Fatalf("Failed to register bug worker: %v", err)
	}

	// Verify workers are retrievable
	workers := registry.List()
	if len(workers) != 2 {
		t.Errorf("Expected 2 workers, got %d", len(workers))
	}

	// Verify workers can be resolved in dependency order
	resolved, err := registry.ResolveWorkers([]string{"bugs", "architecture"})
	if err != nil {
		t.Fatalf("Failed to resolve workers: %v", err)
	}

	if len(resolved) != 2 {
		t.Errorf("Expected 2 resolved workers, got %d", len(resolved))
	}

	// Architecture should come first (bug hunter depends on it)
	if resolved[0].Name() != "architecture" {
		t.Errorf("Expected architecture first, got %s", resolved[0].Name())
	}
	if resolved[1].Name() != "bugs" {
		t.Errorf("Expected bugs second, got %s", resolved[1].Name())
	}

	t.Log("Worker dependency resolution working correctly")
}

// TestHealthMonitorAdapter_Integration tests that health monitors can be used as discovery workers.
func TestHealthMonitorAdapter_Integration(t *testing.T) {
	// Get root of VC project
	rootPath, err := filepath.Abs("../..")
	if err != nil {
		t.Fatalf("Failed to get root path: %v", err)
	}

	// Create a file size monitor (without AI supervisor for interface testing)
	monitor, err := health.NewFileSizeMonitor(rootPath, nil)
	if err != nil {
		t.Fatalf("Failed to create file size monitor: %v", err)
	}

	// Adapt it to a discovery worker
	adapter := NewWorkerAdapter(monitor)

	// Verify it implements DiscoveryWorker interface
	if adapter.Name() == "" {
		t.Error("Adapter should have a name")
	}

	if adapter.Philosophy() == "" {
		t.Error("Adapter should have a philosophy")
	}

	if adapter.Cost().EstimatedDuration == 0 {
		t.Error("Adapter should have cost estimate")
	}

	// Verify no dependencies (health monitors don't depend on other workers)
	if len(adapter.Dependencies()) != 0 {
		t.Errorf("Expected no dependencies, got %v", adapter.Dependencies())
	}

	t.Logf("Health Monitor Adapter Interface:")
	t.Logf("  Name: %s", adapter.Name())
	t.Logf("  Philosophy: %s", adapter.Philosophy())
	t.Logf("  Scope: %s", adapter.Scope())
	t.Logf("  Cost: %v duration, %d AI calls", adapter.Cost().EstimatedDuration, adapter.Cost().AICallsEstimated)

	// Test that all 4 core health monitors can be adapted
	monitorNames := []string{"file_size", "cruft", "duplication", "zfc"}
	for _, name := range monitorNames {
		var testMonitor health.HealthMonitor
		var err error

		switch name {
		case "file_size":
			testMonitor, err = health.NewFileSizeMonitor(rootPath, nil)
		case "cruft":
			testMonitor, err = health.NewCruftDetector(rootPath, nil)
		case "duplication":
			testMonitor, err = health.NewDuplicationDetector(rootPath, nil)
		case "zfc":
			testMonitor, err = health.NewZFCDetector(rootPath, nil)
		}

		if err != nil {
			t.Errorf("Failed to create %s monitor: %v", name, err)
			continue
		}

		// Verify each can be adapted
		adapted := NewWorkerAdapter(testMonitor)
		if adapted.Name() == "" {
			t.Errorf("%s adapter has empty name", name)
		}
		if adapted.Philosophy() == "" {
			t.Errorf("%s adapter has empty philosophy", name)
		}

		t.Logf("  ✓ %s monitor successfully adapted", name)
	}

	t.Log("All 4 health monitors successfully implement DiscoveryWorker interface via adapter")
}

// TestDocAuditorWorker_Integration tests the DocAuditorWorker on the VC codebase.
func TestDocAuditorWorker_Integration(t *testing.T) {
	// Get root of VC project
	rootPath, err := filepath.Abs("../..")
	if err != nil {
		t.Fatalf("Failed to get root path: %v", err)
	}

	// Build codebase context
	builder := NewContextBuilder(rootPath, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	codebaseCtx, err := builder.Build(ctx)
	if err != nil {
		t.Fatalf("Failed to build codebase context: %v", err)
	}

	// Create and run doc auditor worker
	worker := NewDocAuditorWorker()
	result, err := worker.Analyze(ctx, codebaseCtx)
	if err != nil {
		t.Fatalf("Doc auditor worker failed: %v", err)
	}

	// Verify result structure
	if result == nil {
		t.Fatal("Expected non-nil result")
	}

	if result.Stats.FilesAnalyzed == 0 {
		t.Error("Expected some files to be analyzed")
	}

	// Log what was found (informational)
	t.Logf("Doc Auditor Analysis Results:")
	t.Logf("  Files analyzed: %d", result.Stats.FilesAnalyzed)
	t.Logf("  Issues found: %d", result.Stats.IssuesFound)
	t.Logf("  Patterns found: %d", result.Stats.PatternsFound)
	t.Logf("  Duration: %v", result.Stats.Duration)

	// Log sample issues
	for i, issue := range result.IssuesDiscovered {
		if i >= 5 {
			break
		}
		t.Logf("  Issue %d: %s (P%d, confidence: %.2f)", i+1, issue.Title, issue.Priority, issue.Confidence)
	}

	// Verify issues have required fields
	for i, issue := range result.IssuesDiscovered {
		if issue.Title == "" {
			t.Errorf("Issue %d has empty title", i)
		}
		if issue.Description == "" {
			t.Errorf("Issue %d has empty description", i)
		}
		if issue.Category != "documentation" {
			t.Errorf("Issue %d has wrong category: got %q, want %q", i, issue.Category, "documentation")
		}
		if issue.DiscoveredBy != worker.Name() {
			t.Errorf("Issue %d has wrong DiscoveredBy: got %q, want %q", i, issue.DiscoveredBy, worker.Name())
		}
		if issue.Confidence < 0 || issue.Confidence > 1 {
			t.Errorf("Issue %d has invalid confidence: %f", i, issue.Confidence)
		}
	}
}

// TestTestCoverageAnalyzerWorker_Integration tests the TestCoverageAnalyzerWorker on the VC codebase.
func TestTestCoverageAnalyzerWorker_Integration(t *testing.T) {
	// Get root of VC project
	rootPath, err := filepath.Abs("../..")
	if err != nil {
		t.Fatalf("Failed to get root path: %v", err)
	}

	// Build codebase context
	builder := NewContextBuilder(rootPath, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	codebaseCtx, err := builder.Build(ctx)
	if err != nil {
		t.Fatalf("Failed to build codebase context: %v", err)
	}

	// Create and run test coverage analyzer worker
	worker := NewTestCoverageAnalyzerWorker()
	result, err := worker.Analyze(ctx, codebaseCtx)
	if err != nil {
		t.Fatalf("Test coverage analyzer worker failed: %v", err)
	}

	// Verify result structure
	if result == nil {
		t.Fatal("Expected non-nil result")
	}

	if result.Stats.FilesAnalyzed == 0 {
		t.Error("Expected some files to be analyzed")
	}

	// Log what was found (informational)
	t.Logf("Test Coverage Analyzer Results:")
	t.Logf("  Files analyzed: %d", result.Stats.FilesAnalyzed)
	t.Logf("  Issues found: %d", result.Stats.IssuesFound)
	t.Logf("  Patterns found: %d", result.Stats.PatternsFound)
	t.Logf("  Duration: %v", result.Stats.Duration)

	// Log sample issues
	for i, issue := range result.IssuesDiscovered {
		if i >= 5 {
			break
		}
		t.Logf("  Issue %d: %s (P%d, confidence: %.2f)", i+1, issue.Title, issue.Priority, issue.Confidence)
	}

	// Verify issues have required fields
	for i, issue := range result.IssuesDiscovered {
		if issue.Title == "" {
			t.Errorf("Issue %d has empty title", i)
		}
		if issue.Description == "" {
			t.Errorf("Issue %d has empty description", i)
		}
		if issue.Category != "testing" {
			t.Errorf("Issue %d has wrong category: got %q, want %q", i, issue.Category, "testing")
		}
		if issue.DiscoveredBy != worker.Name() {
			t.Errorf("Issue %d has wrong DiscoveredBy: got %q, want %q", i, issue.DiscoveredBy, worker.Name())
		}
		if issue.Confidence < 0 || issue.Confidence > 1 {
			t.Errorf("Issue %d has invalid confidence: %f", i, issue.Confidence)
		}
	}
}

// TestSecurityScannerWorker_Integration tests the SecurityScannerWorker on the VC codebase.
func TestSecurityScannerWorker_Integration(t *testing.T) {
	// Get root of VC project
	rootPath, err := filepath.Abs("../..")
	if err != nil {
		t.Fatalf("Failed to get root path: %v", err)
	}

	// Build codebase context
	builder := NewContextBuilder(rootPath, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	codebaseCtx, err := builder.Build(ctx)
	if err != nil {
		t.Fatalf("Failed to build codebase context: %v", err)
	}

	// Create and run security scanner worker
	worker := NewSecurityScannerWorker()
	result, err := worker.Analyze(ctx, codebaseCtx)
	if err != nil {
		t.Fatalf("Security scanner worker failed: %v", err)
	}

	// Verify result structure
	if result == nil {
		t.Fatal("Expected non-nil result")
	}

	if result.Stats.FilesAnalyzed == 0 {
		t.Error("Expected some files to be analyzed")
	}

	// Log what was found (informational)
	t.Logf("Security Scanner Analysis Results:")
	t.Logf("  Files analyzed: %d", result.Stats.FilesAnalyzed)
	t.Logf("  Issues found: %d", result.Stats.IssuesFound)
	t.Logf("  Patterns found: %d", result.Stats.PatternsFound)
	t.Logf("  Duration: %v", result.Stats.Duration)

	// Log sample issues
	for i, issue := range result.IssuesDiscovered {
		if i >= 5 {
			break
		}
		t.Logf("  Issue %d: %s (P%d, confidence: %.2f)", i+1, issue.Title, issue.Priority, issue.Confidence)
	}

	// Verify issues have required fields
	for i, issue := range result.IssuesDiscovered {
		if issue.Title == "" {
			t.Errorf("Issue %d has empty title", i)
		}
		if issue.Description == "" {
			t.Errorf("Issue %d has empty description", i)
		}
		if issue.Category != "security" {
			t.Errorf("Issue %d has wrong category: got %q, want %q", i, issue.Category, "security")
		}
		if issue.DiscoveredBy != worker.Name() {
			t.Errorf("Issue %d has wrong DiscoveredBy: got %q, want %q", i, issue.DiscoveredBy, worker.Name())
		}
		if issue.FilePath == "" {
			t.Errorf("Issue %d has no file path", i)
		}
		if issue.Confidence < 0 || issue.Confidence > 1 {
			t.Errorf("Issue %d has invalid confidence: %f", i, issue.Confidence)
		}
	}
}

// TestAllNewWorkers_Registration tests that the new workers register correctly.
func TestAllNewWorkers_Registration(t *testing.T) {
	// Create registry
	registry := NewWorkerRegistry()

	// Register all new workers (vc-cq4l)
	docWorker := NewDocAuditorWorker()
	if err := registry.Register(docWorker); err != nil {
		t.Fatalf("Failed to register doc auditor worker: %v", err)
	}

	testWorker := NewTestCoverageAnalyzerWorker()
	if err := registry.Register(testWorker); err != nil {
		t.Fatalf("Failed to register test coverage analyzer worker: %v", err)
	}

	securityWorker := NewSecurityScannerWorker()
	if err := registry.Register(securityWorker); err != nil {
		t.Fatalf("Failed to register security scanner worker: %v", err)
	}

	// Verify workers are retrievable
	workers := registry.List()
	if len(workers) != 3 {
		t.Errorf("Expected 3 workers, got %d", len(workers))
	}

	// Verify each worker by name
	expectedWorkers := map[string]bool{
		"doc_auditor":             false,
		"test_coverage_analyzer":  false,
		"security_scanner":        false,
	}

	for _, name := range workers {
		if _, exists := expectedWorkers[name]; exists {
			expectedWorkers[name] = true
		}
	}

	for name, found := range expectedWorkers {
		if !found {
			t.Errorf("Worker %q not found in registry", name)
		}
	}

	t.Log("All three new workers (vc-cq4l) registered successfully")
}
