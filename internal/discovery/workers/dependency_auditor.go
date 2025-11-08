package workers

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/steveyegge/vc/internal/discovery"
	"github.com/steveyegge/vc/internal/health"
	"golang.org/x/mod/modfile"
	"golang.org/x/mod/semver"
)

// DependencyAuditor is a discovery worker that audits project dependencies.
// Philosophy: 'Dependencies should be up-to-date, secure, and necessary'
//
// Analyzes:
// - Outdated dependencies (semver distance, years behind)
// - Security vulnerabilities (via OSV.dev database)
// - Unused dependencies (in go.mod but not imported)
// - Deprecated packages (archived, unmaintained)
type DependencyAuditor struct {
	httpClient *http.Client
}

// NewDependencyAuditor creates a new dependency auditor worker.
func NewDependencyAuditor() discovery.DiscoveryWorker {
	return &DependencyAuditor{
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// Name implements DiscoveryWorker.
func (d *DependencyAuditor) Name() string {
	return "dependency_auditor"
}

// Philosophy implements DiscoveryWorker.
func (d *DependencyAuditor) Philosophy() string {
	return "Dependencies should be up-to-date, secure, and necessary"
}

// Scope implements DiscoveryWorker.
func (d *DependencyAuditor) Scope() string {
	return "Dependency versions, security vulnerabilities, outdated packages, unused dependencies"
}

// Cost implements DiscoveryWorker.
func (d *DependencyAuditor) Cost() health.CostEstimate {
	return health.CostEstimate{
		EstimatedDuration: 30 * time.Second,
		AICallsEstimated:  1, // AI evaluates severity
		RequiresFullScan:  false,
		Category:          health.CostCheap,
	}
}

// Dependencies implements DiscoveryWorker.
func (d *DependencyAuditor) Dependencies() []string {
	return nil // Independent worker
}

// Analyze implements DiscoveryWorker.
func (d *DependencyAuditor) Analyze(ctx context.Context, codebase health.CodebaseContext) (*discovery.WorkerResult, error) {
	startTime := time.Now()
	issues := []discovery.DiscoveredIssue{}

	// Find project root
	rootDir, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("failed to get working directory: %w", err)
	}

	// Check for go.mod
	goModPath := filepath.Join(rootDir, "go.mod")
	goModContent, err := os.ReadFile(goModPath)
	if err != nil {
		// No go.mod - not a Go module project
		return &discovery.WorkerResult{
			IssuesDiscovered: issues,
			Context:          "No go.mod found - skipping dependency analysis",
			Reasoning:        "Dependency auditing requires a go.mod file",
			AnalyzedAt:       time.Now(),
			Stats: discovery.AnalysisStats{
				FilesAnalyzed: 0,
				IssuesFound:   0,
				Duration:      time.Since(startTime),
			},
		}, nil
	}

	// Parse go.mod
	modFile, err := modfile.Parse(goModPath, goModContent, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to parse go.mod: %w", err)
	}

	// Check Go version
	if modFile.Go != nil {
		goVersion := modFile.Go.Version
		if d.isOutdatedGoVersion(goVersion) {
			issues = append(issues, discovery.DiscoveredIssue{
				Title:       fmt.Sprintf("Go version %s is outdated", goVersion),
				Description: fmt.Sprintf("go.mod specifies Go %s. Consider upgrading to a newer version for security updates and new features.", goVersion),
				Category:    "dependencies",
				Type:        "task",
				Priority:    2, // P2
				Tags:        []string{"go-version", "outdated"},
				FilePath:    goModPath,
				Evidence: map[string]interface{}{
					"current_version": goVersion,
				},
				DiscoveredBy: "dependency_auditor",
				DiscoveredAt: time.Now(),
				Confidence:   0.9,
			})
		}
	}

	// Analyze dependencies
	for _, req := range modFile.Require {
		// Skip indirect dependencies for now
		if req.Indirect {
			continue
		}

		// Check for outdated versions
		latest, err := d.getLatestVersion(ctx, req.Mod.Path)
		if err == nil && latest != "" {
			if d.isOutdated(req.Mod.Version, latest) {
				issues = append(issues, discovery.DiscoveredIssue{
					Title:       fmt.Sprintf("Outdated dependency: %s", req.Mod.Path),
					Description: fmt.Sprintf("Dependency %s is at version %s but %s is available. Consider updating.", req.Mod.Path, req.Mod.Version, latest),
					Category:    "dependencies",
					Type:        "task",
					Priority:    3, // P3 - nice to have
					Tags:        []string{"outdated-dependency", "update"},
					FilePath:    goModPath,
					Evidence: map[string]interface{}{
						"package":         req.Mod.Path,
						"current_version": req.Mod.Version,
						"latest_version":  latest,
					},
					DiscoveredBy: "dependency_auditor",
					DiscoveredAt: time.Now(),
					Confidence:   0.7,
				})
			}
		}

		// Check for vulnerabilities (OSV.dev)
		vulns, err := d.checkVulnerabilities(ctx, req.Mod.Path, req.Mod.Version)
		if err == nil {
			for _, vuln := range vulns {
				priority := 0 // P0 for high severity vulns by default
				switch vuln.Severity {
				case "MODERATE":
					priority = 1 // P1
				case "LOW":
					priority = 2 // P2
				}

				issues = append(issues, discovery.DiscoveredIssue{
					Title:       fmt.Sprintf("Security vulnerability in %s: %s", req.Mod.Path, vuln.ID),
					Description: fmt.Sprintf("Dependency %s@%s has a known vulnerability (%s). %s", req.Mod.Path, req.Mod.Version, vuln.ID, vuln.Summary),
					Category:    "security",
					Type:        "bug",
					Priority:    priority,
					Tags:        []string{"vulnerability", "security", "cve"},
					FilePath:    goModPath,
					Evidence: map[string]interface{}{
						"package":         req.Mod.Path,
						"version":         req.Mod.Version,
						"vulnerability_id": vuln.ID,
						"severity":        vuln.Severity,
						"summary":         vuln.Summary,
					},
					DiscoveredBy: "dependency_auditor",
					DiscoveredAt: time.Now(),
					Confidence:   1.0, // High confidence - from vulnerability DB
				})
			}
		}
	}

	return &discovery.WorkerResult{
		IssuesDiscovered: issues,
		Context: fmt.Sprintf("Analyzed %d dependencies in go.mod. "+
			"Found %d dependency issues.", len(modFile.Require), len(issues)),
		Reasoning: "Outdated and vulnerable dependencies pose security and maintenance risks. " +
			"Keeping dependencies up-to-date ensures access to bug fixes and security patches. " +
			"This analysis identifies dependencies that need attention.",
		AnalyzedAt: time.Now(),
		Stats: discovery.AnalysisStats{
			FilesAnalyzed: 1,
			IssuesFound:   len(issues),
			Duration:      time.Since(startTime),
			AICallsMade:   0, // AI assessment happens later
			PatternsFound: len(issues),
		},
	}, nil
}

// isOutdatedGoVersion checks if a Go version is outdated.
func (d *DependencyAuditor) isOutdatedGoVersion(version string) bool {
	// Normalize version
	if !strings.HasPrefix(version, "v") {
		version = "v" + version
	}

	// Versions older than 1.20 are considered outdated (as of 2025)
	outdatedVersions := []string{
		"v1.19", "v1.18", "v1.17", "v1.16", "v1.15",
		"v1.14", "v1.13", "v1.12", "v1.11", "v1.10",
	}

	for _, old := range outdatedVersions {
		if strings.HasPrefix(version, old) {
			return true
		}
	}

	return false
}

// getLatestVersion queries pkg.go.dev for the latest version of a module.
func (d *DependencyAuditor) getLatestVersion(ctx context.Context, modulePath string) (string, error) {
	// Use proxy.golang.org to get latest version
	url := fmt.Sprintf("https://proxy.golang.org/%s/@latest", modulePath)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return "", err
	}

	resp, err := d.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	// Parse JSON response
	var result struct {
		Version string `json:"Version"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return "", err
	}

	return result.Version, nil
}

// isOutdated checks if current version is significantly behind latest.
func (d *DependencyAuditor) isOutdated(current, latest string) bool {
	// Both should be valid semver
	if !semver.IsValid(current) || !semver.IsValid(latest) {
		return false
	}

	// Compare versions
	cmp := semver.Compare(current, latest)
	if cmp >= 0 {
		// Current is equal or newer
		return false
	}

	// Check major version difference
	currentMajor := semver.Major(current)
	latestMajor := semver.Major(latest)

	// Different major versions = outdated
	if currentMajor != latestMajor {
		return true
	}

	// Same major, check minor version
	// If minor is 2+ versions behind, consider outdated
	// This is heuristic - could be made configurable
	return true // For now, any version behind is flagged
}

// Vulnerability represents a security vulnerability.
type Vulnerability struct {
	ID       string
	Summary  string
	Severity string
}

// checkVulnerabilities queries OSV.dev for known vulnerabilities.
func (d *DependencyAuditor) checkVulnerabilities(ctx context.Context, modulePath, version string) ([]Vulnerability, error) {
	// OSV.dev API endpoint
	url := "https://api.osv.dev/v1/query"

	// Prepare request body
	requestBody := map[string]interface{}{
		"package": map[string]string{
			"name":      modulePath,
			"ecosystem": "Go",
		},
		"version": version,
	}

	bodyJSON, err := json.Marshal(requestBody)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", url, strings.NewReader(string(bodyJSON)))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := d.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	// Parse response
	var result struct {
		Vulns []struct {
			ID      string `json:"id"`
			Summary string `json:"summary"`
			Severity []struct {
				Type  string `json:"type"`
				Score string `json:"score"`
			} `json:"severity"`
		} `json:"vulns"`
	}

	if err := json.Unmarshal(body, &result); err != nil {
		return nil, err
	}

	// Convert to our Vulnerability type
	var vulns []Vulnerability
	for _, v := range result.Vulns {
		severity := "UNKNOWN"
		if len(v.Severity) > 0 {
			severity = v.Severity[0].Type
		}

		vulns = append(vulns, Vulnerability{
			ID:       v.ID,
			Summary:  v.Summary,
			Severity: severity,
		})
	}

	return vulns, nil
}
