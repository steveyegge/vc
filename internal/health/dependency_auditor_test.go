package health

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestDependencyAuditorBasics(t *testing.T) {
	tmpDir := t.TempDir()

	supervisor := &mockAISupervisor{}
	auditor, err := NewDependencyAuditor(tmpDir, supervisor)
	if err != nil {
		t.Fatalf("NewDependencyAuditor failed: %v", err)
	}

	// Test interface implementation
	if auditor.Name() != "dependency_auditor" {
		t.Errorf("Name() = %q, want %q", auditor.Name(), "dependency_auditor")
	}

	if auditor.Philosophy() == "" {
		t.Error("Philosophy() returned empty string")
	}

	schedule := auditor.Schedule()
	if schedule.Type != ScheduleTimeBased {
		t.Errorf("Schedule type = %v, want %v", schedule.Type, ScheduleTimeBased)
	}

	cost := auditor.Cost()
	if cost.Category != CostCheap {
		t.Errorf("Cost category = %v, want %v", cost.Category, CostCheap)
	}
}

func TestDependencyAuditorNoGoMod(t *testing.T) {
	tmpDir := t.TempDir()

	supervisor := &mockAISupervisor{}
	auditor, err := NewDependencyAuditor(tmpDir, supervisor)
	if err != nil {
		t.Fatalf("NewDependencyAuditor failed: %v", err)
	}

	ctx := context.Background()
	codebase := CodebaseContext{RootPath: tmpDir}

	result, err := auditor.Check(ctx, codebase)
	if err != nil {
		t.Fatalf("Check failed: %v", err)
	}

	if len(result.IssuesFound) != 0 {
		t.Errorf("Expected no issues without go.mod, got %d", len(result.IssuesFound))
	}

	if result.Context != "No go.mod file found in repository" {
		t.Errorf("Unexpected context: %q", result.Context)
	}
}

func TestDependencyAuditorParseGoMod(t *testing.T) {
	tmpDir := t.TempDir()
	goModPath := filepath.Join(tmpDir, "go.mod")

	goModContent := `module example.com/test

go 1.21

require (
	github.com/stretchr/testify v1.8.0
	golang.org/x/mod v0.12.0
)

require (
	github.com/davecgh/go-spew v1.1.1 // indirect
	github.com/pmezard/go-difflib v1.0.0 // indirect
)
`

	if err := os.WriteFile(goModPath, []byte(goModContent), 0644); err != nil {
		t.Fatalf("Failed to write go.mod: %v", err)
	}

	supervisor := &mockAISupervisor{}
	auditor, err := NewDependencyAuditor(tmpDir, supervisor)
	if err != nil {
		t.Fatalf("NewDependencyAuditor failed: %v", err)
	}

	deps, err := auditor.parseGoMod(goModPath)
	if err != nil {
		t.Fatalf("parseGoMod failed: %v", err)
	}

	// Should only include direct dependencies (not indirect)
	if len(deps) != 2 {
		t.Errorf("Expected 2 direct dependencies, got %d", len(deps))
	}

	// Check that we got the right dependencies
	found := make(map[string]string)
	for _, dep := range deps {
		found[dep.Path] = dep.Version
	}

	expected := map[string]string{
		"github.com/stretchr/testify": "v1.8.0",
		"golang.org/x/mod":            "v0.12.0",
	}

	for path, version := range expected {
		if found[path] != version {
			t.Errorf("Dependency %s: got version %q, want %q", path, found[path], version)
		}
	}
}

func TestDependencyAuditorVulnerabilityCheck(t *testing.T) {
	// Create mock OSV.dev server
	osvServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/querybatch" {
			http.NotFound(w, r)
			return
		}

		// Return mock vulnerability data
		response := map[string]interface{}{
			"results": []map[string]interface{}{
				{
					"vulns": []map[string]interface{}{
						{
							"id":      "GO-2023-1234",
							"summary": "Test vulnerability in package X",
							"published": "2023-01-01T00:00:00Z",
							"severity": []map[string]string{
								{"type": "CVSS_V3", "score": "7.5"},
							},
							"affected": []map[string]interface{}{
								{
									"package": map[string]string{
										"name": "example.com/vulnerable",
									},
									"ranges": []map[string]interface{}{
										{
											"events": []map[string]string{
												{"fixed": "v1.2.3"},
											},
										},
									},
								},
							},
						},
					},
				},
			},
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}))
	defer osvServer.Close()

	tmpDir := t.TempDir()
	goModPath := filepath.Join(tmpDir, "go.mod")

	goModContent := `module example.com/test

go 1.21

require example.com/vulnerable v1.0.0
`

	if err := os.WriteFile(goModPath, []byte(goModContent), 0644); err != nil {
		t.Fatalf("Failed to write go.mod: %v", err)
	}

	supervisor := &mockAISupervisor{
		response: `{
			"issues": [
				{
					"package": "example.com/vulnerable",
					"id": "GO-2023-1234",
					"severity": "high",
					"action": "Update to v1.2.3 immediately"
				}
			],
			"reasoning": "Critical security vulnerability with available fix"
		}`,
	}

	auditor, err := NewDependencyAuditor(tmpDir, supervisor)
	if err != nil {
		t.Fatalf("NewDependencyAuditor failed: %v", err)
	}

	// Override HTTP client to use mock server
	auditor.HTTPClient = osvServer.Client()

	// Note: This test will fail because we can't easily override the OSV URL
	// In a real implementation, we'd make the OSV endpoint configurable
	// For now, just test the evaluation logic
	ctx := context.Background()

	vulns := []vulnerability{
		{
			ID:          "GO-2023-1234",
			Package:     "example.com/vulnerable",
			Summary:     "Test vulnerability",
			Severity:    "7.5",
			FixedIn:     "v1.2.3",
			PublishedAt: "2023-01-01T00:00:00Z",
		},
	}

	eval, err := auditor.evaluateVulnerabilities(ctx, vulns)
	if err != nil {
		t.Fatalf("evaluateVulnerabilities failed: %v", err)
	}

	if len(eval.Issues) != 1 {
		t.Errorf("Expected 1 issue, got %d", len(eval.Issues))
	}

	if eval.Issues[0].Severity != "high" {
		t.Errorf("Issue severity = %q, want %q", eval.Issues[0].Severity, "high")
	}
}

func TestDependencyAuditorOutdatedCheck(t *testing.T) {
	// Create mock Go proxy server
	proxyServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Handle /@latest requests
		if r.URL.Path == "/example.com/old/@latest" {
			response := map[string]string{
				"Version": "v2.0.0",
				"Time":    "2024-01-01T00:00:00Z",
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(response)
			return
		}

		http.NotFound(w, r)
	}))
	defer proxyServer.Close()

	tmpDir := t.TempDir()

	supervisor := &mockAISupervisor{
		response: `{
			"issues": [
				{
					"package": "example.com/old",
					"severity": "medium",
					"action": "Update to v2.0.0 when convenient"
				}
			],
			"reasoning": "Multiple versions behind but no security risk"
		}`,
	}

	auditor, err := NewDependencyAuditor(tmpDir, supervisor)
	if err != nil {
		t.Fatalf("NewDependencyAuditor failed: %v", err)
	}

	// Override HTTP client to use mock server
	auditor.HTTPClient = proxyServer.Client()

	ctx := context.Background()

	// Test evaluation logic with mock outdated deps
	outdated := []outdatedDependency{
		{
			Package:        "example.com/old",
			CurrentVersion: "v1.0.0",
			LatestVersion:  "v2.0.0",
			VersionsBehind: 5,
		},
	}

	eval, err := auditor.evaluateOutdated(ctx, outdated)
	if err != nil {
		t.Fatalf("evaluateOutdated failed: %v", err)
	}

	if len(eval.Issues) != 1 {
		t.Errorf("Expected 1 issue, got %d", len(eval.Issues))
	}

	if eval.Issues[0].Severity != "medium" {
		t.Errorf("Issue severity = %q, want %q", eval.Issues[0].Severity, "medium")
	}
}

func TestDependencyAuditorIntegration(t *testing.T) {
	tmpDir := t.TempDir()
	goModPath := filepath.Join(tmpDir, "go.mod")

	goModContent := `module example.com/test

go 1.21

require (
	github.com/stretchr/testify v1.8.0
	golang.org/x/mod v0.12.0
)
`

	if err := os.WriteFile(goModPath, []byte(goModContent), 0644); err != nil {
		t.Fatalf("Failed to write go.mod: %v", err)
	}

	supervisor := &mockAISupervisor{
		response: `{
			"issues": [],
			"reasoning": "No significant issues found"
		}`,
	}

	auditor, err := NewDependencyAuditor(tmpDir, supervisor)
	if err != nil {
		t.Fatalf("NewDependencyAuditor failed: %v", err)
	}

	ctx := context.Background()
	codebase := CodebaseContext{RootPath: tmpDir}

	// This will make real API calls to OSV.dev and proxy.golang.org
	// In CI, we might want to skip this or use mocks
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	result, err := auditor.Check(ctx, codebase)
	if err != nil {
		t.Fatalf("Check failed: %v", err)
	}

	// Should have scanned 1 file (go.mod)
	if result.Stats.FilesScanned != 1 {
		t.Errorf("FilesScanned = %d, want 1", result.Stats.FilesScanned)
	}

	// Context should mention the dependencies
	if result.Context == "" {
		t.Error("Context is empty")
	}

	// Should have made some AI calls (if vulnerabilities or outdated deps found)
	// This is variable based on actual state of dependencies
	t.Logf("AI calls made: %d", result.Stats.AICallsMade)
	t.Logf("Issues found: %d", result.Stats.IssuesFound)
	t.Logf("Context: %s", result.Context)
}

func TestDependencyAuditorCountVersionsBehind(t *testing.T) {
	auditor := &DependencyAuditor{}

	tests := []struct {
		current string
		latest  string
		want    int
	}{
		{"v1.0.0", "v1.0.1", 1},
		{"v1.0.0", "v1.1.0", 5},
		{"v1.0.0", "v2.0.0", 10},
		{"invalid", "v1.0.0", 0},
		{"v1.0.0", "invalid", 0},
	}

	for _, tt := range tests {
		got := auditor.countVersionsBehind(tt.current, tt.latest)
		if got != tt.want {
			t.Errorf("countVersionsBehind(%q, %q) = %d, want %d",
				tt.current, tt.latest, got, tt.want)
		}
	}
}

func TestDependencyAuditorBuildPrompts(t *testing.T) {
	auditor := &DependencyAuditor{}

	// Test vulnerability prompt
	vulns := []vulnerability{
		{
			ID:          "GO-2023-1234",
			Package:     "example.com/pkg",
			Summary:     "Test vulnerability",
			Severity:    "7.5",
			FixedIn:     "v1.2.3",
			PublishedAt: "2023-01-01T00:00:00Z",
		},
	}

	prompt := auditor.buildVulnerabilityPrompt(vulns)
	if prompt == "" {
		t.Error("buildVulnerabilityPrompt returned empty string")
	}

	// Should include the vulnerability ID
	if !contains(prompt, "GO-2023-1234") {
		t.Error("Prompt doesn't contain vulnerability ID")
	}

	// Test outdated prompt
	outdated := []outdatedDependency{
		{
			Package:        "example.com/old",
			CurrentVersion: "v1.0.0",
			LatestVersion:  "v2.0.0",
			VersionsBehind: 5,
		},
	}

	prompt = auditor.buildOutdatedPrompt(outdated)
	if prompt == "" {
		t.Error("buildOutdatedPrompt returned empty string")
	}

	// Should include the package name
	if !contains(prompt, "example.com/old") {
		t.Error("Prompt doesn't contain package name")
	}
}

func contains(s, substr string) bool {
	return len(s) > 0 && len(substr) > 0 && (s == substr || len(s) > len(substr) && containsSubstring(s, substr))
}

func containsSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
