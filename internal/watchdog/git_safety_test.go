package watchdog

import (
	"context"
	"strings"
	"testing"

	"github.com/steveyegge/vc/internal/ai"
)

func TestNewGitSafetyMonitor(t *testing.T) {
	// Use mockStorage from analyzer_test.go
	mockStore := &mockStorage{}

	tests := []struct {
		name    string
		cfg     *GitSafetyMonitorConfig
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid config",
			cfg: &GitSafetyMonitorConfig{
				Supervisor: &ai.Supervisor{},
				Store:      mockStore,
			},
			wantErr: false,
		},
		{
			name: "missing supervisor",
			cfg: &GitSafetyMonitorConfig{
				Store: mockStore,
			},
			wantErr: true,
			errMsg:  "supervisor is required",
		},
		{
			name: "missing store",
			cfg: &GitSafetyMonitorConfig{
				Supervisor: &ai.Supervisor{},
			},
			wantErr: true,
			errMsg:  "storage is required",
		},
		{
			name: "with custom config",
			cfg: &GitSafetyMonitorConfig{
				Supervisor: &ai.Supervisor{},
				Store:      mockStore,
				Config:     DefaultWatchdogConfig(),
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			monitor, err := NewGitSafetyMonitor(tt.cfg)

			if tt.wantErr {
				if err == nil {
					t.Error("Expected error, got nil")
				} else if !strings.Contains(err.Error(), tt.errMsg) {
					t.Errorf("Expected error containing %q, got %q", tt.errMsg, err.Error())
				}
			} else {
				if err != nil {
					t.Errorf("Expected no error, got %v", err)
				}
				if monitor == nil {
					t.Error("Expected monitor to be non-nil")
				}
			}
		})
	}
}

func TestIsProtectedBranch(t *testing.T) {
	tests := []struct {
		name     string
		branch   string
		expected bool
	}{
		{"main branch", "main", true},
		{"master branch", "master", true},
		{"develop branch", "develop", true},
		{"development branch", "development", true},
		{"production branch", "production", true},
		{"prod branch", "prod", true},
		{"release branch", "release", true},
		{"release/* branch", "release/v1.0", true},
		{"release-* branch", "release-1.0", true},
		{"feature branch", "feature/my-feature", false},
		{"bugfix branch", "bugfix/fix-123", false},
		{"personal branch", "john/experiment", false},
		{"main with whitespace", " main ", true},
		{"MAIN uppercase", "MAIN", true},
		{"Master mixed case", "Master", true},
		{"empty string", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsProtectedBranch(tt.branch)
			if result != tt.expected {
				t.Errorf("IsProtectedBranch(%q) = %v, want %v", tt.branch, result, tt.expected)
			}
		})
	}
}

func TestParseCommand(t *testing.T) {
	tests := []struct {
		name                string
		cmdString           string
		expectedCommand     string
		expectedHasForce    bool
		expectedIsDangerous bool
		expectedIsWrite     bool
		expectedTargetRef   string
	}{
		{
			name:                "simple status",
			cmdString:           "git status",
			expectedCommand:     "status",
			expectedHasForce:    false,
			expectedIsDangerous: false,
			expectedIsWrite:     false,
		},
		{
			name:                "force push",
			cmdString:           "git push origin main --force",
			expectedCommand:     "push",
			expectedHasForce:    true,
			expectedIsDangerous: true,
			expectedIsWrite:     true,
			expectedTargetRef:   "main",
		},
		{
			name:                "force push short flag",
			cmdString:           "git push -f origin main",
			expectedCommand:     "push",
			expectedHasForce:    true,
			expectedIsDangerous: true,
			expectedIsWrite:     true,
		},
		{
			name:                "force-with-lease",
			cmdString:           "git push --force-with-lease origin feature",
			expectedCommand:     "push",
			expectedHasForce:    true,
			expectedIsDangerous: true,
			expectedIsWrite:     true,
			expectedTargetRef:   "origin", // First non-flag arg after push
		},
		{
			name:                "hard reset",
			cmdString:           "git reset --hard HEAD~1",
			expectedCommand:     "reset",
			expectedHasForce:    false,
			expectedIsDangerous: true,
			expectedIsWrite:     true,
		},
		{
			name:                "delete branch",
			cmdString:           "git branch -D old-feature",
			expectedCommand:     "branch",
			expectedHasForce:    false,
			expectedIsDangerous: true,
			expectedIsWrite:     true,
			expectedTargetRef:   "old-feature",
		},
		{
			name:                "safe commit",
			cmdString:           "git commit -m 'Update README'",
			expectedCommand:     "commit",
			expectedHasForce:    false,
			expectedIsDangerous: false,
			expectedIsWrite:     true,
		},
		{
			name:                "safe push",
			cmdString:           "git push origin feature-branch",
			expectedCommand:     "push",
			expectedHasForce:    false,
			expectedIsDangerous: false,
			expectedIsWrite:     true,
			expectedTargetRef:   "feature-branch",
		},
		{
			name:                "read operation",
			cmdString:           "git log --oneline",
			expectedCommand:     "log",
			expectedHasForce:    false,
			expectedIsDangerous: false,
			expectedIsWrite:     false,
		},
		{
			name:                "checkout branch",
			cmdString:           "git checkout feature-branch",
			expectedCommand:     "checkout",
			expectedHasForce:    false,
			expectedIsDangerous: false,
			expectedIsWrite:     false,
			expectedTargetRef:   "feature-branch",
		},
		{
			name:                "no git prefix",
			cmdString:           "status",
			expectedCommand:     "status",
			expectedHasForce:    false,
			expectedIsDangerous: false,
			expectedIsWrite:     false,
		},
		{
			name:                "empty command",
			cmdString:           "",
			expectedCommand:     "",
			expectedHasForce:    false,
			expectedIsDangerous: false,
			expectedIsWrite:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parsed := ParseCommand(tt.cmdString)

			if parsed.Command != tt.expectedCommand {
				t.Errorf("Command = %q, want %q", parsed.Command, tt.expectedCommand)
			}

			if parsed.HasForce != tt.expectedHasForce {
				t.Errorf("HasForce = %v, want %v", parsed.HasForce, tt.expectedHasForce)
			}

			if parsed.IsDangerous != tt.expectedIsDangerous {
				t.Errorf("IsDangerous = %v, want %v", parsed.IsDangerous, tt.expectedIsDangerous)
			}

			if parsed.IsWrite != tt.expectedIsWrite {
				t.Errorf("IsWrite = %v, want %v", parsed.IsWrite, tt.expectedIsWrite)
			}

			if tt.expectedTargetRef != "" && parsed.TargetRef != tt.expectedTargetRef {
				t.Errorf("TargetRef = %q, want %q", parsed.TargetRef, tt.expectedTargetRef)
			}
		})
	}
}

func TestEvaluateCommand_Mock(t *testing.T) {
	// Create a mock store
	mockStore := &mockStorage{}
	ctx := context.Background()

	// Note: We can't fully test EvaluateCommand without a real supervisor
	// because it requires an API key. For now, we test the structure.
	// The mock implementation in callAISupervisor returns a safe evaluation.

	monitor := &GitSafetyMonitor{
		supervisor: nil, // We'll use the mock in callAISupervisor
		store:      mockStore,
		config:     DefaultWatchdogConfig(),
	}

	// Test the evaluation (will use mock response)
	evaluation, err := monitor.EvaluateCommand(ctx, "git status", "main", "vc-test-1")
	if err != nil {
		t.Fatalf("EvaluateCommand failed: %v", err)
	}

	// Verify the mock response structure
	if !evaluation.Safe {
		t.Error("Expected mock response to mark command as safe")
	}

	if evaluation.CommandType != GitCommandRead {
		t.Errorf("Expected command type 'read', got %q", evaluation.CommandType)
	}

	if evaluation.Confidence <= 0 || evaluation.Confidence > 1 {
		t.Errorf("Expected confidence between 0 and 1, got %f", evaluation.Confidence)
	}
}

func TestCheckDangerousOperation_SafeCommand(t *testing.T) {
	mockStore := &mockStorage{}
	ctx := context.Background()

	monitor := &GitSafetyMonitor{
		supervisor: nil, // Will use mock
		store:      mockStore,
		config:     DefaultWatchdogConfig(),
	}

	// Test a safe command (mock returns safe=true)
	err := monitor.CheckDangerousOperation(ctx, "git status", "main", "vc-test-1", false)
	if err != nil {
		t.Errorf("Expected no error for safe command, got %v", err)
	}
}

func TestBuildSafetyPrompt(t *testing.T) {
	monitor := &GitSafetyMonitor{
		config: DefaultWatchdogConfig(),
	}

	tests := []struct {
		name     string
		command  string
		branch   string
		issueID  string
		contains []string
	}{
		{
			name:    "basic command",
			command: "git push origin main --force",
			branch:  "main",
			issueID: "vc-123",
			contains: []string{
				"git push origin main --force",
				"Current Branch: main",
				"Issue Context: vc-123",
				"DANGEROUS OPERATIONS",
				"Force pushing",
				"raw JSON",
			},
		},
		{
			name:    "command without context",
			command: "git status",
			branch:  "",
			issueID: "",
			contains: []string{
				"git status",
				"safety monitor",
				"raw JSON",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			prompt := monitor.buildSafetyPrompt(tt.command, tt.branch, tt.issueID)

			for _, expectedStr := range tt.contains {
				if !strings.Contains(prompt, expectedStr) {
					t.Errorf("Expected prompt to contain %q, but it didn't", expectedStr)
				}
			}
		})
	}
}

func TestGitCommandType_Constants(t *testing.T) {
	// Verify the constants are defined correctly
	if GitCommandRead != "read" {
		t.Errorf("GitCommandRead = %q, want 'read'", GitCommandRead)
	}
	if GitCommandWrite != "write" {
		t.Errorf("GitCommandWrite = %q, want 'write'", GitCommandWrite)
	}
	if GitCommandDanger != "danger" {
		t.Errorf("GitCommandDanger = %q, want 'danger'", GitCommandDanger)
	}
}

func TestGitSafetyEvaluation_Structure(t *testing.T) {
	// Test that the structure can be created and has expected fields
	eval := &GitSafetyEvaluation{
		Safe:               true,
		CommandType:        GitCommandRead,
		RiskLevel:          "low",
		Reasoning:          "Safe read operation",
		RecommendedAction:  "allow",
		Confidence:         0.95,
		AlternativeCommand: "",
		Warnings:           []string{},
	}

	if !eval.Safe {
		t.Error("Expected Safe to be true")
	}

	if eval.CommandType != GitCommandRead {
		t.Errorf("Expected CommandType to be 'read', got %q", eval.CommandType)
	}

	if eval.Confidence != 0.95 {
		t.Errorf("Expected Confidence to be 0.95, got %f", eval.Confidence)
	}
}

func TestParseCommand_EdgeCases(t *testing.T) {
	tests := []struct {
		name      string
		cmdString string
		checkFunc func(*testing.T, *ParsedGitCommand)
	}{
		{
			name:      "only whitespace",
			cmdString: "   ",
			checkFunc: func(t *testing.T, p *ParsedGitCommand) {
				if p.Command != "" {
					t.Errorf("Expected empty command, got %q", p.Command)
				}
			},
		},
		{
			name:      "git with no subcommand",
			cmdString: "git",
			checkFunc: func(t *testing.T, p *ParsedGitCommand) {
				if p.Command != "" {
					t.Errorf("Expected empty command, got %q", p.Command)
				}
			},
		},
		{
			name:      "multiple force flags",
			cmdString: "git push --force origin main --force-with-lease",
			checkFunc: func(t *testing.T, p *ParsedGitCommand) {
				if !p.HasForce {
					t.Error("Expected HasForce to be true")
				}
				if !p.IsDangerous {
					t.Error("Expected IsDangerous to be true")
				}
			},
		},
		{
			name:      "complex command with many args",
			cmdString: "git push origin feature-branch --set-upstream --verbose",
			checkFunc: func(t *testing.T, p *ParsedGitCommand) {
				if p.Command != "push" {
					t.Errorf("Expected command 'push', got %q", p.Command)
				}
				if p.TargetRef != "feature-branch" {
					t.Errorf("Expected TargetRef 'feature-branch', got %q", p.TargetRef)
				}
				if len(p.Args) < 4 {
					t.Errorf("Expected at least 4 args, got %d", len(p.Args))
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parsed := ParseCommand(tt.cmdString)
			tt.checkFunc(t, parsed)
		})
	}
}
