package health

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestMissingUtilityDetector_Name(t *testing.T) {
	detector, err := NewMissingUtilityDetector("/tmp", nil)
	if err != nil {
		t.Fatalf("NewMissingUtilityDetector failed: %v", err)
	}

	if got := detector.Name(); got != "missing_utility_detector" {
		t.Errorf("Name() = %q, want %q", got, "missing_utility_detector")
	}
}

func TestMissingUtilityDetector_Philosophy(t *testing.T) {
	detector, err := NewMissingUtilityDetector("/tmp", nil)
	if err != nil {
		t.Fatalf("NewMissingUtilityDetector failed: %v", err)
	}

	philosophy := detector.Philosophy()
	if !strings.Contains(philosophy, "missing abstraction") {
		t.Errorf("Philosophy should mention missing abstraction, got: %s", philosophy)
	}
}

func TestMissingUtilityDetector_Schedule(t *testing.T) {
	detector, err := NewMissingUtilityDetector("/tmp", nil)
	if err != nil {
		t.Fatalf("NewMissingUtilityDetector failed: %v", err)
	}

	schedule := detector.Schedule()
	if schedule.Type != ScheduleEventBased {
		t.Errorf("Schedule.Type = %q, want %q", schedule.Type, ScheduleEventBased)
	}
	if schedule.EventTrigger != "every_50_issues" {
		t.Errorf("Schedule.EventTrigger = %q, want %q", schedule.EventTrigger, "every_50_issues")
	}
}

func TestMissingUtilityDetector_Cost(t *testing.T) {
	detector, err := NewMissingUtilityDetector("/tmp", nil)
	if err != nil {
		t.Fatalf("NewMissingUtilityDetector failed: %v", err)
	}

	cost := detector.Cost()
	if cost.Category != CostExpensive {
		t.Errorf("Cost.Category = %q, want %q", cost.Category, CostExpensive)
	}
	if cost.AICallsEstimated != 1 {
		t.Errorf("Cost.AICallsEstimated = %d, want 1", cost.AICallsEstimated)
	}
	if cost.RequiresFullScan {
		t.Errorf("Cost.RequiresFullScan = true, want false (uses sampling)")
	}
}

func TestMissingUtilityDetector_Check_NoSupervisor(t *testing.T) {
	tmpDir := t.TempDir()
	detector, err := NewMissingUtilityDetector(tmpDir, nil)
	if err != nil {
		t.Fatalf("NewMissingUtilityDetector failed: %v", err)
	}

	ctx := context.Background()
	codebase := CodebaseContext{RootPath: tmpDir}

	_, err = detector.Check(ctx, codebase)
	if err == nil {
		t.Error("Check() should fail when supervisor is nil")
	}
	if !strings.Contains(err.Error(), "AI supervisor is required") {
		t.Errorf("Check() error = %v, want error mentioning AI supervisor", err)
	}
}

func TestMissingUtilityDetector_Check_NoFiles(t *testing.T) {
	tmpDir := t.TempDir()

	mockSupervisor := &mockAISupervisor{}
	detector, err := NewMissingUtilityDetector(tmpDir, mockSupervisor)
	if err != nil {
		t.Fatalf("NewMissingUtilityDetector failed: %v", err)
	}

	ctx := context.Background()
	codebase := CodebaseContext{RootPath: tmpDir}

	result, err := detector.Check(ctx, codebase)
	if err != nil {
		t.Fatalf("Check() failed: %v", err)
	}

	if len(result.IssuesFound) != 0 {
		t.Errorf("Check() found %d issues, want 0 when no files exist", len(result.IssuesFound))
	}
	if !strings.Contains(result.Context, "No code files found") {
		t.Errorf("Check() context should mention no files found, got: %s", result.Context)
	}
}

func TestMissingUtilityDetector_Check_WithCodeSamples(t *testing.T) {
	tmpDir := t.TempDir()

	// Create sample Go files
	createTestFile(t, tmpDir, "file1.go", `package main

func main() {
	// Truncate string safely
	s := "hello world"
	runes := []rune(s)
	if len(runes) > 10 {
		s = string(runes[:10]) + "..."
	}
}
`)

	createTestFile(t, tmpDir, "file2.go", `package utils

func Process(input string) string {
	// Truncate string safely
	runes := []rune(input)
	if len(runes) > 10 {
		return string(runes[:10]) + "..."
	}
	return input
}
`)

	// Mock AI response
	mockResponse := mockMissingUtilityResponse{
		OverallAssessment: "Found repeated string truncation pattern",
		MissingUtilities: []missingUtility{
			{
				Pattern:           "String truncation with UTF-8 safety",
				Occurrences:       []string{"file1.go:4-8", "file2.go:4-8"},
				SuggestedName:     "SafeTruncateString",
				SuggestedLocation: "internal/utils/strings.go",
				Justification:     "Repeated 2 times, handles UTF-8 correctly",
				Priority:          "P2",
			},
		},
		AcceptableRepetition: []acceptablePattern{},
	}

	responseJSON, _ := json.Marshal(mockResponse)

	mockSupervisor := &mockAISupervisor{
		response: string(responseJSON),
	}

	detector, err := NewMissingUtilityDetector(tmpDir, mockSupervisor)
	if err != nil {
		t.Fatalf("NewMissingUtilityDetector failed: %v", err)
	}

	ctx := context.Background()
	codebase := CodebaseContext{RootPath: tmpDir}

	result, err := detector.Check(ctx, codebase)
	if err != nil {
		t.Fatalf("Check() failed: %v", err)
	}

	if len(result.IssuesFound) != 1 {
		t.Fatalf("Check() found %d issues, want 1", len(result.IssuesFound))
	}

	issue := result.IssuesFound[0]
	if issue.Category != "missing_utility" {
		t.Errorf("Issue.Category = %q, want %q", issue.Category, "missing_utility")
	}
	if issue.Severity != "medium" {
		t.Errorf("Issue.Severity = %q, want %q", issue.Severity, "medium")
	}
	if !strings.Contains(issue.Description, "SafeTruncateString") {
		t.Errorf("Issue.Description should mention SafeTruncateString, got: %s", issue.Description)
	}

	// Check evidence
	if pattern, ok := issue.Evidence["pattern"].(string); !ok || pattern != "String truncation with UTF-8 safety" {
		t.Errorf("Issue.Evidence[pattern] = %v, want %q", issue.Evidence["pattern"], "String truncation with UTF-8 safety")
	}

	// Check stats
	if result.Stats.AICallsMade != 1 {
		t.Errorf("Stats.AICallsMade = %d, want 1", result.Stats.AICallsMade)
	}
	if result.Stats.IssuesFound != 1 {
		t.Errorf("Stats.IssuesFound = %d, want 1", result.Stats.IssuesFound)
	}
}

func TestMissingUtilityDetector_ScanExistingUtilities(t *testing.T) {
	tmpDir := t.TempDir()

	// Create utility directory
	utilsDir := filepath.Join(tmpDir, "internal", "utils")
	if err := os.MkdirAll(utilsDir, 0755); err != nil {
		t.Fatalf("Failed to create utils dir: %v", err)
	}

	// Create utility file
	createTestFile(t, utilsDir, "strings.go", `package utils

func SafeTruncate(s string, maxLen int) string {
	runes := []rune(s)
	if len(runes) > maxLen {
		return string(runes[:maxLen]) + "..."
	}
	return s
}

func Capitalize(s string) string {
	// Implementation
	return s
}
`)

	detector, err := NewMissingUtilityDetector(tmpDir, nil)
	if err != nil {
		t.Fatalf("NewMissingUtilityDetector failed: %v", err)
	}

	ctx := context.Background()
	utils, err := detector.scanExistingUtilities(ctx)
	if err != nil {
		t.Fatalf("scanExistingUtilities() failed: %v", err)
	}

	if len(utils) != 2 {
		t.Fatalf("scanExistingUtilities() found %d utilities, want 2", len(utils))
	}

	// Check utility names
	names := make(map[string]bool)
	for _, util := range utils {
		names[util.Name] = true
	}

	expectedNames := []string{"SafeTruncate", "Capitalize"}
	for _, name := range expectedNames {
		if !names[name] {
			t.Errorf("scanExistingUtilities() missing utility %q", name)
		}
	}
}

func TestMissingUtilityDetector_SampleCodeFiles(t *testing.T) {
	tmpDir := t.TempDir()

	// Create multiple Go files
	for i := 1; i <= 5; i++ {
		content := "package main\n\nfunc main() {\n\t// Code here\n}\n"
		createTestFile(t, tmpDir, filepath.Join("file"+string(rune('0'+i))+".go"), content)
	}

	detector, err := NewMissingUtilityDetector(tmpDir, nil)
	if err != nil {
		t.Fatalf("NewMissingUtilityDetector failed: %v", err)
	}
	detector.MaxSamples = 3 // Limit samples

	ctx := context.Background()
	samples, err := detector.sampleCodeFiles(ctx)
	if err != nil {
		t.Fatalf("sampleCodeFiles() failed: %v", err)
	}

	if len(samples) > 3 {
		t.Errorf("sampleCodeFiles() returned %d samples, want <= 3", len(samples))
	}
	if len(samples) == 0 {
		t.Error("sampleCodeFiles() returned 0 samples, want > 0")
	}
}

func TestMissingUtilityDetector_BuildPrompt(t *testing.T) {
	detector, err := NewMissingUtilityDetector("/tmp", nil)
	if err != nil {
		t.Fatalf("NewMissingUtilityDetector failed: %v", err)
	}

	existingUtils := []existingUtility{
		{Name: "SafeTruncate", Location: "internal/utils/strings.go"},
	}

	samples := []codeSample{
		{
			FilePath:  "main.go",
			StartLine: 10,
			Lines:     []string{"func main() {", "\t// code", "}"},
		},
	}

	prompt := detector.buildPrompt(existingUtils, samples)

	// Check prompt contains key sections
	expectedSections := []string{
		"Missing Utility Detection",
		"Philosophy",
		"Guidance",
		"Existing Utilities",
		"SafeTruncate",
		"Code Samples",
		"main.go",
		"Response Format",
	}

	for _, section := range expectedSections {
		if !strings.Contains(prompt, section) {
			t.Errorf("buildPrompt() missing section %q", section)
		}
	}

	// Check year is current
	currentYear := time.Now().Year()
	if !strings.Contains(prompt, string(rune('0'+currentYear/1000))) {
		t.Error("buildPrompt() should include current year in guidance")
	}
}

func TestMissingUtilityDetector_BuildIssues(t *testing.T) {
	detector, err := NewMissingUtilityDetector("/tmp", nil)
	if err != nil {
		t.Fatalf("NewMissingUtilityDetector failed: %v", err)
	}

	analysis := &missingUtilityAnalysis{
		OverallAssessment: "Good code quality",
		MissingUtilities: []missingUtility{
			{
				Pattern:           "String truncation",
				Occurrences:       []string{"file1.go:10-15"},
				SuggestedName:     "SafeTruncate",
				SuggestedLocation: "internal/utils/strings.go",
				Justification:     "Repeated pattern",
				Priority:          "P2",
			},
			{
				Pattern:           "Error wrapping",
				Occurrences:       []string{"file2.go:20-25"},
				SuggestedName:     "WrapError",
				SuggestedLocation: "internal/utils/errors.go",
				Justification:     "Common error handling",
				Priority:          "P1",
			},
		},
	}

	issues := detector.buildIssues(analysis)

	if len(issues) != 2 {
		t.Fatalf("buildIssues() created %d issues, want 2", len(issues))
	}

	// Check first issue
	if issues[0].Category != "missing_utility" {
		t.Errorf("issues[0].Category = %q, want %q", issues[0].Category, "missing_utility")
	}
	if issues[0].Severity != "medium" {
		t.Errorf("issues[0].Severity = %q, want %q (P2 -> medium)", issues[0].Severity, "medium")
	}

	// Check second issue (P1 -> high severity)
	if issues[1].Severity != "high" {
		t.Errorf("issues[1].Severity = %q, want %q (P1 -> high)", issues[1].Severity, "high")
	}
}

func TestMissingUtilityDetector_ExcludePatterns(t *testing.T) {
	tmpDir := t.TempDir()

	// Create files that should be excluded
	createTestFile(t, tmpDir, "main.go", "package main\n")
	createTestFile(t, tmpDir, "main_test.go", "package main\n") // Should be excluded

	vendorDir := filepath.Join(tmpDir, "vendor", "lib")
	if err := os.MkdirAll(vendorDir, 0755); err != nil {
		t.Fatalf("Failed to create vendor dir: %v", err)
	}
	createTestFile(t, vendorDir, "vendor.go", "package lib\n") // Should be excluded

	detector, err := NewMissingUtilityDetector(tmpDir, nil)
	if err != nil {
		t.Fatalf("NewMissingUtilityDetector failed: %v", err)
	}

	ctx := context.Background()
	samples, err := detector.sampleCodeFiles(ctx)
	if err != nil {
		t.Fatalf("sampleCodeFiles() failed: %v", err)
	}

	// Should only find main.go
	if len(samples) != 1 {
		t.Errorf("sampleCodeFiles() returned %d samples, want 1 (main_test.go and vendor should be excluded)", len(samples))
	}
	if len(samples) > 0 && !strings.Contains(samples[0].FilePath, "main.go") {
		t.Errorf("sampleCodeFiles() should return main.go, got: %s", samples[0].FilePath)
	}
}

func TestMissingUtilityDetector_ExtractSnippet(t *testing.T) {
	tmpDir := t.TempDir()

	// Small file - should take all lines
	smallFile := filepath.Join(tmpDir, "small.go")
	smallContent := "package main\n\nfunc main() {\n\t// Small file\n}"
	if err := os.WriteFile(smallFile, []byte(smallContent), 0644); err != nil {
		t.Fatalf("Failed to write small file: %v", err)
	}

	detector, err := NewMissingUtilityDetector(tmpDir, nil)
	if err != nil {
		t.Fatalf("NewMissingUtilityDetector failed: %v", err)
	}
	detector.SnippetLines = 50

	snippet, err := detector.extractSnippet(smallFile, "small.go")
	if err != nil {
		t.Fatalf("extractSnippet() failed: %v", err)
	}

	if snippet == nil {
		t.Fatal("extractSnippet() returned nil for small file")
	}

	// Split on newlines creates N+1 entries for N lines with trailing newline
	expectedLines := len(strings.Split(smallContent, "\n"))
	if len(snippet.Lines) != expectedLines {
		t.Errorf("extractSnippet() returned %d lines, want %d (entire file)", len(snippet.Lines), expectedLines)
	}

	// Large file - should take a snippet
	largeFile := filepath.Join(tmpDir, "large.go")
	var largeLines []string
	for i := 0; i < 200; i++ {
		largeLines = append(largeLines, "// Line "+string(rune('0'+i%10)))
	}
	largeContent := strings.Join(largeLines, "\n")
	if err := os.WriteFile(largeFile, []byte(largeContent), 0644); err != nil {
		t.Fatalf("Failed to write large file: %v", err)
	}

	snippet, err = detector.extractSnippet(largeFile, "large.go")
	if err != nil {
		t.Fatalf("extractSnippet() failed for large file: %v", err)
	}

	if snippet == nil {
		t.Fatal("extractSnippet() returned nil for large file")
	}
	if len(snippet.Lines) != 50 {
		t.Errorf("extractSnippet() returned %d lines, want 50 (snippet)", len(snippet.Lines))
	}
}

// Helper functions

func createTestFile(t *testing.T, baseDir, relPath, content string) {
	t.Helper()

	fullPath := filepath.Join(baseDir, relPath)
	dir := filepath.Dir(fullPath)

	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("Failed to create directory %s: %v", dir, err)
	}

	if err := os.WriteFile(fullPath, []byte(content), 0644); err != nil {
		t.Fatalf("Failed to write file %s: %v", fullPath, err)
	}
}

// Mock types for testing

type mockMissingUtilityResponse struct {
	OverallAssessment    string               `json:"overall_assessment"`
	MissingUtilities     []missingUtility     `json:"missing_utilities"`
	AcceptableRepetition []acceptablePattern  `json:"acceptable_repetition"`
}
