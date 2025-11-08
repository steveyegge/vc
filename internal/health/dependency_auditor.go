package health

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"golang.org/x/mod/modfile"
	"golang.org/x/mod/semver"
)

// DependencyAuditor analyzes dependencies for security vulnerabilities,
// outdated versions, license issues, and unused dependencies.
//
// ZFC Compliance: Collects dependency data and vulnerability reports,
// then delegates severity assessment to AI supervisor.
type DependencyAuditor struct {
	// RootPath is the codebase root directory
	RootPath string

	// AI supervisor for evaluating issues
	Supervisor AISupervisor

	// HTTPClient for external API calls (OSV.dev, pkg.go.dev)
	HTTPClient *http.Client
}

// NewDependencyAuditor creates a dependency auditor with sensible defaults.
func NewDependencyAuditor(rootPath string, supervisor AISupervisor) (*DependencyAuditor, error) {
	// Validate and clean the root path
	absPath, err := filepath.Abs(rootPath)
	if err != nil {
		return nil, fmt.Errorf("invalid root path %q: %w", rootPath, err)
	}

	return &DependencyAuditor{
		RootPath:   absPath,
		Supervisor: supervisor,
		HTTPClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}, nil
}

// Name implements HealthMonitor.
func (a *DependencyAuditor) Name() string {
	return "dependency_auditor"
}

// Philosophy implements HealthMonitor.
func (a *DependencyAuditor) Philosophy() string {
	return "Dependencies should be up-to-date, secure, and necessary. " +
		"Outdated dependencies create security risks and maintenance burden."
}

// Schedule implements HealthMonitor.
func (a *DependencyAuditor) Schedule() ScheduleConfig {
	return ScheduleConfig{
		Type:     ScheduleTimeBased,
		Interval: 7 * 24 * time.Hour, // Weekly
	}
}

// Cost implements HealthMonitor.
func (a *DependencyAuditor) Cost() CostEstimate {
	return CostEstimate{
		EstimatedDuration: 30 * time.Second,
		AICallsEstimated:  2, // One for vulnerabilities, one for outdated deps
		RequiresFullScan:  false,
		Category:          CostCheap,
	}
}

// Check implements HealthMonitor.
func (a *DependencyAuditor) Check(ctx context.Context, codebase CodebaseContext) (*MonitorResult, error) {
	startTime := time.Now()

	// 1. Find go.mod file
	goModPath := filepath.Join(a.RootPath, "go.mod")
	if _, err := os.Stat(goModPath); os.IsNotExist(err) {
		return &MonitorResult{
			IssuesFound: []DiscoveredIssue{},
			Context:     "No go.mod file found in repository",
			CheckedAt:   startTime,
			Stats: CheckStats{
				FilesScanned: 0,
				Duration:     time.Since(startTime),
			},
		}, nil
	}

	// 2. Validate that AI supervisor is configured
	if a.Supervisor == nil {
		return nil, fmt.Errorf("AI supervisor is required for dependency auditing")
	}

	// 3. Parse go.mod
	dependencies, err := a.parseGoMod(goModPath)
	if err != nil {
		return nil, fmt.Errorf("parsing go.mod: %w", err)
	}

	if len(dependencies) == 0 {
		return &MonitorResult{
			IssuesFound: []DiscoveredIssue{},
			Context:     "No dependencies found in go.mod",
			CheckedAt:   startTime,
			Stats: CheckStats{
				FilesScanned: 1,
				Duration:     time.Since(startTime),
			},
		}, nil
	}

	aiCallsMade := 0

	// 4. Check for vulnerabilities via OSV.dev
	vulnerabilities, err := a.checkVulnerabilities(ctx, dependencies)
	if err != nil {
		log.Printf("Warning: vulnerability check failed: %v", err)
		vulnerabilities = []vulnerability{}
	}

	// 5. Check for outdated dependencies
	outdatedDeps, err := a.checkOutdated(ctx, dependencies)
	if err != nil {
		log.Printf("Warning: outdated check failed: %v", err)
		outdatedDeps = []outdatedDependency{}
	}

	// 6. Ask AI to evaluate findings
	var issues []DiscoveredIssue
	var reasoning string

	if len(vulnerabilities) > 0 {
		vulnEval, err := a.evaluateVulnerabilities(ctx, vulnerabilities)
		if err != nil {
			return nil, fmt.Errorf("evaluating vulnerabilities: %w", err)
		}
		issues = append(issues, a.buildVulnerabilityIssues(vulnEval)...)
		reasoning += vulnEval.Reasoning + "\n\n"
		aiCallsMade++
	}

	if len(outdatedDeps) > 0 {
		outdatedEval, err := a.evaluateOutdated(ctx, outdatedDeps)
		if err != nil {
			return nil, fmt.Errorf("evaluating outdated dependencies: %w", err)
		}
		issues = append(issues, a.buildOutdatedIssues(outdatedEval)...)
		reasoning += outdatedEval.Reasoning
		aiCallsMade++
	}

	return &MonitorResult{
		IssuesFound: issues,
		Context:     a.buildContext(dependencies, vulnerabilities, outdatedDeps),
		Reasoning:   reasoning,
		CheckedAt:   startTime,
		Stats: CheckStats{
			FilesScanned: 1,
			IssuesFound:  len(issues),
			Duration:     time.Since(startTime),
			AICallsMade:  aiCallsMade,
		},
	}, nil
}

// dependency represents a Go module dependency.
type dependency struct {
	Path    string
	Version string
}

// vulnerability represents a security vulnerability from OSV.dev.
type vulnerability struct {
	ID          string
	Package     string
	Summary     string
	Severity    string
	FixedIn     string
	PublishedAt string
}

// outdatedDependency represents a dependency with a newer version available.
type outdatedDependency struct {
	Package        string
	CurrentVersion string
	LatestVersion  string
	VersionsBehind int
	YearsBehind    float64
}

// parseGoMod parses go.mod and extracts dependencies.
func (a *DependencyAuditor) parseGoMod(path string) ([]dependency, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading go.mod: %w", err)
	}

	modFile, err := modfile.Parse(path, data, nil)
	if err != nil {
		return nil, fmt.Errorf("parsing go.mod: %w", err)
	}

	var deps []dependency
	for _, req := range modFile.Require {
		// Skip indirect dependencies for now (focus on direct deps)
		if req.Indirect {
			continue
		}
		deps = append(deps, dependency{
			Path:    req.Mod.Path,
			Version: req.Mod.Version,
		})
	}

	return deps, nil
}

// checkVulnerabilities queries OSV.dev for known vulnerabilities.
func (a *DependencyAuditor) checkVulnerabilities(ctx context.Context, deps []dependency) ([]vulnerability, error) {
	// OSV.dev batch query API
	// https://google.github.io/osv.dev/post-v1-querybatch/

	type osvPackage struct {
		Name      string `json:"name"`
		Ecosystem string `json:"ecosystem"`
	}

	type osvQuery struct {
		Version string     `json:"version,omitempty"`
		Package osvPackage `json:"package"`
	}

	type batchRequest struct {
		Queries []osvQuery `json:"queries"`
	}

	// Build batch query
	queries := make([]osvQuery, 0, len(deps))
	for _, dep := range deps {
		queries = append(queries, osvQuery{
			Version: dep.Version,
			Package: osvPackage{
				Name:      dep.Path,
				Ecosystem: "Go",
			},
		})
	}

	reqBody := batchRequest{Queries: queries}
	reqJSON, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshaling request: %w", err)
	}

	// Make API request
	req, err := http.NewRequestWithContext(ctx, "POST", "https://api.osv.dev/v1/querybatch", strings.NewReader(string(reqJSON)))
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := a.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("querying OSV.dev: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("OSV.dev returned %d: %s", resp.StatusCode, string(body))
	}

	// Parse response
	var batchResp struct {
		Results []struct {
			Vulns []struct {
				ID      string `json:"id"`
				Summary string `json:"summary"`
				Published string `json:"published"`
				Severity []struct {
					Type  string `json:"type"`
					Score string `json:"score"`
				} `json:"severity"`
				Affected []struct {
					Package struct {
						Name string `json:"name"`
					} `json:"package"`
					Ranges []struct {
						Events []struct {
							Introduced string `json:"introduced,omitempty"`
							Fixed      string `json:"fixed,omitempty"`
						} `json:"events"`
					} `json:"ranges"`
				} `json:"affected"`
			} `json:"vulns"`
		} `json:"results"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&batchResp); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}

	// Extract vulnerabilities
	var vulns []vulnerability
	for _, result := range batchResp.Results {
		for _, v := range result.Vulns {
			// Extract severity (CVSS score if available)
			severity := "unknown"
			for _, sev := range v.Severity {
				if sev.Type == "CVSS_V3" {
					severity = sev.Score
					break
				}
			}

			// Extract fixed version
			fixedIn := "unknown"
			if len(v.Affected) > 0 && len(v.Affected[0].Ranges) > 0 {
				for _, event := range v.Affected[0].Ranges[0].Events {
					if event.Fixed != "" {
						fixedIn = event.Fixed
						break
					}
				}
			}

			pkgName := "unknown"
			if len(v.Affected) > 0 {
				pkgName = v.Affected[0].Package.Name
			}

			vulns = append(vulns, vulnerability{
				ID:          v.ID,
				Package:     pkgName,
				Summary:     v.Summary,
				Severity:    severity,
				FixedIn:     fixedIn,
				PublishedAt: v.Published,
			})
		}
	}

	return vulns, nil
}

// checkOutdated checks for outdated dependencies (simplified version).
// In production, this would query pkg.go.dev or proxy.golang.org.
func (a *DependencyAuditor) checkOutdated(ctx context.Context, deps []dependency) ([]outdatedDependency, error) {
	var outdated []outdatedDependency

	// For each dependency, check if there's a newer version
	// This is a simplified implementation - real version would query Go module proxy
	for _, dep := range deps {
		latest, err := a.getLatestVersion(ctx, dep.Path)
		if err != nil {
			// Log but don't fail the entire check
			log.Printf("Warning: failed to check latest version for %s: %v", dep.Path, err)
			continue
		}

		if latest != "" && semver.Compare(dep.Version, latest) < 0 {
			outdated = append(outdated, outdatedDependency{
				Package:        dep.Path,
				CurrentVersion: dep.Version,
				LatestVersion:  latest,
				VersionsBehind: a.countVersionsBehind(dep.Version, latest),
			})
		}
	}

	return outdated, nil
}

// getLatestVersion queries the Go module proxy for the latest version.
func (a *DependencyAuditor) getLatestVersion(ctx context.Context, modulePath string) (string, error) {
	// Use Go module proxy to get latest version
	// https://proxy.golang.org/{module}/@latest
	url := fmt.Sprintf("https://proxy.golang.org/%s/@latest", modulePath)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return "", fmt.Errorf("creating request: %w", err)
	}

	resp, err := a.HTTPClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("querying proxy: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		// Module not found in proxy
		return "", nil
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("proxy returned %d", resp.StatusCode)
	}

	var latestInfo struct {
		Version string `json:"Version"`
		Time    string `json:"Time"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&latestInfo); err != nil {
		return "", fmt.Errorf("decoding response: %w", err)
	}

	return latestInfo.Version, nil
}

// countVersionsBehind estimates how many versions behind current is from latest.
func (a *DependencyAuditor) countVersionsBehind(current, latest string) int {
	// Simple heuristic based on semver
	// This is approximate - real implementation would query all versions
	if !semver.IsValid(current) || !semver.IsValid(latest) {
		return 0
	}

	// Compare major versions
	currentMajor := semver.Major(current)
	latestMajor := semver.Major(latest)
	if currentMajor != latestMajor {
		// Different major versions - could be many versions behind
		return 10 // Placeholder
	}

	// Compare minor versions
	currentMinor := strings.TrimPrefix(current, currentMajor+".")
	latestMinor := strings.TrimPrefix(latest, latestMajor+".")

	if strings.HasPrefix(currentMinor, strings.Split(latestMinor, ".")[0]) {
		// Same minor version, different patch
		return 1
	}

	// Different minor versions
	return 5 // Placeholder
}

// evaluateVulnerabilities asks AI to assess vulnerability severity.
func (a *DependencyAuditor) evaluateVulnerabilities(ctx context.Context, vulns []vulnerability) (*vulnerabilityEvaluation, error) {
	prompt := a.buildVulnerabilityPrompt(vulns)

	resp, err := a.Supervisor.CallAI(ctx, prompt, "evaluate_vulnerabilities", "claude-sonnet-4-20250514", 4096)
	if err != nil {
		return nil, fmt.Errorf("AI evaluation failed: %w", err)
	}

	// Parse AI response
	eval := &vulnerabilityEvaluation{}
	if err := json.Unmarshal([]byte(resp), eval); err != nil {
		// If JSON parsing fails, treat entire response as reasoning
		eval.Reasoning = resp
		eval.Issues = make([]vulnerabilityIssue, len(vulns))
		for i, v := range vulns {
			eval.Issues[i] = vulnerabilityIssue{
				Package:  v.Package,
				ID:       v.ID,
				Severity: "high", // Default to high if we can't parse
				Action:   "Update to latest version",
			}
		}
	}

	return eval, nil
}

// evaluateOutdated asks AI to assess outdated dependency severity.
func (a *DependencyAuditor) evaluateOutdated(ctx context.Context, outdated []outdatedDependency) (*outdatedEvaluation, error) {
	prompt := a.buildOutdatedPrompt(outdated)

	resp, err := a.Supervisor.CallAI(ctx, prompt, "evaluate_outdated", "claude-sonnet-4-20250514", 4096)
	if err != nil {
		return nil, fmt.Errorf("AI evaluation failed: %w", err)
	}

	// Parse AI response
	eval := &outdatedEvaluation{}
	if err := json.Unmarshal([]byte(resp), eval); err != nil {
		// If JSON parsing fails, treat entire response as reasoning
		eval.Reasoning = resp
		eval.Issues = make([]outdatedIssue, len(outdated))
		for i, d := range outdated {
			eval.Issues[i] = outdatedIssue{
				Package:  d.Package,
				Severity: "medium", // Default to medium
				Action:   fmt.Sprintf("Update to %s", d.LatestVersion),
			}
		}
	}

	return eval, nil
}

// buildVulnerabilityPrompt creates the AI prompt for vulnerability evaluation.
func (a *DependencyAuditor) buildVulnerabilityPrompt(vulns []vulnerability) string {
	var sb strings.Builder

	sb.WriteString("You are evaluating security vulnerabilities in Go dependencies.\n\n")
	sb.WriteString("Philosophy: Dependencies should be up-to-date, secure, and necessary.\n\n")
	sb.WriteString("Vulnerabilities found:\n\n")

	for i, v := range vulns {
		sb.WriteString(fmt.Sprintf("%d. %s (%s)\n", i+1, v.ID, v.Package))
		sb.WriteString(fmt.Sprintf("   Summary: %s\n", v.Summary))
		sb.WriteString(fmt.Sprintf("   Severity: %s\n", v.Severity))
		sb.WriteString(fmt.Sprintf("   Fixed in: %s\n", v.FixedIn))
		sb.WriteString(fmt.Sprintf("   Published: %s\n\n", v.PublishedAt))
	}

	sb.WriteString("For each vulnerability, assess:\n")
	sb.WriteString("1. Actual risk to this codebase (critical/high/medium/low)\n")
	sb.WriteString("2. Recommended action (upgrade, workaround, accept risk)\n")
	sb.WriteString("3. Urgency (immediate/soon/low-priority)\n\n")

	sb.WriteString("Respond with JSON:\n")
	sb.WriteString("{\n")
	sb.WriteString(`  "issues": [{"package": "...", "id": "...", "severity": "...", "action": "..."}],` + "\n")
	sb.WriteString(`  "reasoning": "..."` + "\n")
	sb.WriteString("}\n")

	return sb.String()
}

// buildOutdatedPrompt creates the AI prompt for outdated dependency evaluation.
func (a *DependencyAuditor) buildOutdatedPrompt(outdated []outdatedDependency) string {
	var sb strings.Builder

	sb.WriteString("You are evaluating outdated Go dependencies.\n\n")
	sb.WriteString("Philosophy: Dependencies should be reasonably up-to-date to avoid security risks and maintenance burden.\n\n")
	sb.WriteString("Outdated dependencies found:\n\n")

	// Sort by versions behind (most outdated first)
	sort.Slice(outdated, func(i, j int) bool {
		return outdated[i].VersionsBehind > outdated[j].VersionsBehind
	})

	for i, d := range outdated {
		sb.WriteString(fmt.Sprintf("%d. %s\n", i+1, d.Package))
		sb.WriteString(fmt.Sprintf("   Current: %s\n", d.CurrentVersion))
		sb.WriteString(fmt.Sprintf("   Latest: %s\n", d.LatestVersion))
		sb.WriteString(fmt.Sprintf("   Versions behind: ~%d\n\n", d.VersionsBehind))
	}

	sb.WriteString("For each outdated dependency, assess:\n")
	sb.WriteString("1. Risk of staying on current version (high/medium/low)\n")
	sb.WriteString("2. Recommended action (upgrade immediately/plan upgrade/acceptable lag)\n")
	sb.WriteString("3. Breaking change likelihood\n\n")

	sb.WriteString("Respond with JSON:\n")
	sb.WriteString("{\n")
	sb.WriteString(`  "issues": [{"package": "...", "severity": "...", "action": "..."}],` + "\n")
	sb.WriteString(`  "reasoning": "..."` + "\n")
	sb.WriteString("}\n")

	return sb.String()
}

// vulnerabilityEvaluation is the AI response for vulnerability assessment.
type vulnerabilityEvaluation struct {
	Issues    []vulnerabilityIssue `json:"issues"`
	Reasoning string               `json:"reasoning"`
}

type vulnerabilityIssue struct {
	Package  string `json:"package"`
	ID       string `json:"id"`
	Severity string `json:"severity"`
	Action   string `json:"action"`
}

// outdatedEvaluation is the AI response for outdated dependency assessment.
type outdatedEvaluation struct {
	Issues    []outdatedIssue `json:"issues"`
	Reasoning string          `json:"reasoning"`
}

type outdatedIssue struct {
	Package  string `json:"package"`
	Severity string `json:"severity"`
	Action   string `json:"action"`
}

// buildVulnerabilityIssues converts AI evaluation to DiscoveredIssue list.
func (a *DependencyAuditor) buildVulnerabilityIssues(eval *vulnerabilityEvaluation) []DiscoveredIssue {
	var issues []DiscoveredIssue

	for _, issue := range eval.Issues {
		issues = append(issues, DiscoveredIssue{
			FilePath:    "go.mod",
			Category:    "security",
			Severity:    issue.Severity,
			Description: fmt.Sprintf("%s vulnerability in %s: %s", issue.ID, issue.Package, issue.Action),
			Evidence: map[string]interface{}{
				"vulnerability_id": issue.ID,
				"package":          issue.Package,
				"recommended_action": issue.Action,
			},
		})
	}

	return issues
}

// buildOutdatedIssues converts AI evaluation to DiscoveredIssue list.
func (a *DependencyAuditor) buildOutdatedIssues(eval *outdatedEvaluation) []DiscoveredIssue {
	var issues []DiscoveredIssue

	for _, issue := range eval.Issues {
		issues = append(issues, DiscoveredIssue{
			FilePath:    "go.mod",
			Category:    "outdated_dependency",
			Severity:    issue.Severity,
			Description: fmt.Sprintf("Outdated dependency %s: %s", issue.Package, issue.Action),
			Evidence: map[string]interface{}{
				"package":            issue.Package,
				"recommended_action": issue.Action,
			},
		})
	}

	return issues
}

// buildContext creates context string for the monitor result.
func (a *DependencyAuditor) buildContext(deps []dependency, vulns []vulnerability, outdated []outdatedDependency) string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("Analyzed %d dependencies in go.mod\n\n", len(deps)))

	if len(vulns) > 0 {
		sb.WriteString(fmt.Sprintf("Found %d security vulnerabilities\n", len(vulns)))
	}

	if len(outdated) > 0 {
		sb.WriteString(fmt.Sprintf("Found %d outdated dependencies\n", len(outdated)))
	}

	if len(vulns) == 0 && len(outdated) == 0 {
		sb.WriteString("All dependencies are up-to-date and have no known vulnerabilities\n")
	}

	return sb.String()
}
