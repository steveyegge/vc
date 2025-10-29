package sandbox

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/steveyegge/vc/internal/types"
)

func TestSlugify(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "basic title",
			input: "User Authentication",
			want:  "user-authentication",
		},
		{
			name:  "title with special characters",
			input: "Fix bug #123",
			want:  "fix-bug-123",
		},
		{
			name:  "title with version numbers",
			input: "Add support for OAuth2.0",
			want:  "add-support-for-oauth2-0",
		},
		{
			name:  "title with multiple spaces",
			input: "This  has   multiple    spaces",
			want:  "this-has-multiple-spaces",
		},
		{
			name:  "title with leading/trailing spaces",
			input: "  Leading and trailing  ",
			want:  "leading-and-trailing",
		},
		{
			name:  "very long title",
			input: "This is a very long title that should be truncated to ensure branch names stay reasonable length",
			want:  "this-is-a-very-long-title-that-should-be-truncated",
		},
		{
			name:  "empty string",
			input: "",
			want:  "",
		},
		{
			name:  "only special characters",
			input: "###!!!@@@",
			want:  "",
		},
		{
			name:  "mixed case",
			input: "MixedCaseTitle",
			want:  "mixedcasetitle",
		},
		{
			name:  "with parentheses",
			input: "Implement feature (Phase 2)",
			want:  "implement-feature-phase-2",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := slugify(tt.input)
			if got != tt.want {
				t.Errorf("slugify(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestCreateMissionSandbox(t *testing.T) {
	// Setup test repository
	repoPath, cleanupRepo := setupTestRepo(t)
	defer cleanupRepo()

	// Setup test database
	mainDB, cleanupDB := setupTestDB(t, repoPath)
	defer cleanupDB()

	// Create sandbox manager
	sandboxRoot := filepath.Join(repoPath, ".sandboxes")
	cfg := Config{
		SandboxRoot: sandboxRoot,
		ParentRepo:  repoPath,
		MainDB:      mainDB,
	}

	manager, err := NewManager(cfg)
	if err != nil {
		t.Fatalf("Failed to create manager: %v", err)
	}

	ctx := context.Background()

	// Create a test mission
	mission := &types.Mission{
		Issue: types.Issue{
			Title:       "User Authentication System",
			Description: "Implement OAuth2 login",
			IssueType:   types.TypeEpic,
			IssueSubtype: types.SubtypeMission,
			Status:      types.StatusOpen,
			Priority:    1,
		},
		Goal:             "Build authentication",
		ApprovalRequired: true,
	}

	if err := mainDB.CreateMission(ctx, mission, "test"); err != nil {
		t.Fatalf("Failed to create test mission: %v", err)
	}

	t.Run("creates new sandbox with stable paths", func(t *testing.T) {
		sandbox, err := CreateMissionSandbox(ctx, manager, mainDB, mission.ID)
		if err != nil {
			t.Fatalf("CreateMissionSandbox() failed: %v", err)
		}

		// Verify sandbox was created
		if sandbox == nil {
			t.Fatal("Expected sandbox, got nil")
		}

		// Verify sandbox has correct mission ID
		if sandbox.MissionID != mission.ID {
			t.Errorf("Expected MissionID %s, got %s", mission.ID, sandbox.MissionID)
		}

		// Verify stable paths (no timestamp)
		expectedID := "mission-" + mission.ID
		if sandbox.ID != expectedID {
			t.Errorf("Expected sandbox ID %s, got %s", expectedID, sandbox.ID)
		}

		// Verify branch name includes slugified title
		expectedBranchPrefix := "mission/" + mission.ID + "-"
		if len(sandbox.GitBranch) <= len(expectedBranchPrefix) {
			t.Errorf("Expected branch name to start with %s, got %s", expectedBranchPrefix, sandbox.GitBranch)
		}

		// Verify metadata stored in vc_mission_state
		updatedMission, err := mainDB.GetMission(ctx, mission.ID)
		if err != nil {
			t.Fatalf("Failed to retrieve mission: %v", err)
		}

		if updatedMission.SandboxPath == "" {
			t.Error("Expected sandbox_path to be set in vc_mission_state")
		}
		if updatedMission.BranchName == "" {
			t.Error("Expected branch_name to be set in vc_mission_state")
		}

		// Verify sandbox path matches stored metadata
		if sandbox.Path != updatedMission.SandboxPath {
			t.Errorf("Sandbox path %s doesn't match stored metadata %s", sandbox.Path, updatedMission.SandboxPath)
		}
		if sandbox.GitBranch != updatedMission.BranchName {
			t.Errorf("Branch name %s doesn't match stored metadata %s", sandbox.GitBranch, updatedMission.BranchName)
		}
	})

	t.Run("idempotent - returns existing sandbox", func(t *testing.T) {
		// Call CreateMissionSandbox again for the same mission
		sandbox1, err := CreateMissionSandbox(ctx, manager, mainDB, mission.ID)
		if err != nil {
			t.Fatalf("First call failed: %v", err)
		}

		sandbox2, err := CreateMissionSandbox(ctx, manager, mainDB, mission.ID)
		if err != nil {
			t.Fatalf("Second call failed: %v", err)
		}

		// Should return the same sandbox
		if sandbox1.ID != sandbox2.ID {
			t.Errorf("Expected same sandbox ID, got %s and %s", sandbox1.ID, sandbox2.ID)
		}
		if sandbox1.GitBranch != sandbox2.GitBranch {
			t.Errorf("Expected same branch name, got %s and %s", sandbox1.GitBranch, sandbox2.GitBranch)
		}
	})

	t.Run("fails for non-existent mission", func(t *testing.T) {
		_, err := CreateMissionSandbox(ctx, manager, mainDB, "vc-nonexistent")
		if err == nil {
			t.Error("Expected error for non-existent mission, got nil")
		}
	})
}

func TestGetMissionSandbox(t *testing.T) {
	// Setup test repository
	repoPath, cleanupRepo := setupTestRepo(t)
	defer cleanupRepo()

	// Setup test database
	mainDB, cleanupDB := setupTestDB(t, repoPath)
	defer cleanupDB()

	// Create sandbox manager
	sandboxRoot := filepath.Join(repoPath, ".sandboxes")
	cfg := Config{
		SandboxRoot: sandboxRoot,
		ParentRepo:  repoPath,
		MainDB:      mainDB,
	}

	manager, err := NewManager(cfg)
	if err != nil {
		t.Fatalf("Failed to create manager: %v", err)
	}

	ctx := context.Background()

	// Create test missions
	mission1 := &types.Mission{
		Issue: types.Issue{
			Title:        "Mission with sandbox",
			Description:  "Has a sandbox",
			IssueType:    types.TypeEpic,
			IssueSubtype: types.SubtypeMission,
			Status:       types.StatusOpen,
			Priority:     1,
		},
	}

	mission2 := &types.Mission{
		Issue: types.Issue{
			Title:        "Mission without sandbox",
			Description:  "No sandbox yet",
			IssueType:    types.TypeEpic,
			IssueSubtype: types.SubtypeMission,
			Status:       types.StatusOpen,
			Priority:     1,
		},
	}

	if err := mainDB.CreateMission(ctx, mission1, "test"); err != nil {
		t.Fatalf("Failed to create mission1: %v", err)
	}
	if err := mainDB.CreateMission(ctx, mission2, "test"); err != nil {
		t.Fatalf("Failed to create mission2: %v", err)
	}

	// Create sandbox for mission1
	createdSandbox, err := CreateMissionSandbox(ctx, manager, mainDB, mission1.ID)
	if err != nil {
		t.Fatalf("Failed to create sandbox: %v", err)
	}

	t.Run("retrieves existing sandbox", func(t *testing.T) {
		sandbox, err := GetMissionSandbox(ctx, manager, mainDB, mission1.ID)
		if err != nil {
			t.Fatalf("GetMissionSandbox() failed: %v", err)
		}

		if sandbox == nil {
			t.Fatal("Expected sandbox, got nil")
		}

		if sandbox.ID != createdSandbox.ID {
			t.Errorf("Expected sandbox ID %s, got %s", createdSandbox.ID, sandbox.ID)
		}
	})

	t.Run("returns nil for mission without sandbox", func(t *testing.T) {
		sandbox, err := GetMissionSandbox(ctx, manager, mainDB, mission2.ID)
		if err != nil {
			t.Fatalf("GetMissionSandbox() failed: %v", err)
		}

		if sandbox != nil {
			t.Errorf("Expected nil for mission without sandbox, got %v", sandbox)
		}
	})

	t.Run("fails for non-existent mission", func(t *testing.T) {
		_, err := GetMissionSandbox(ctx, manager, mainDB, "vc-nonexistent")
		if err == nil {
			t.Error("Expected error for non-existent mission, got nil")
		}
	})
}

func TestCleanupMissionSandbox(t *testing.T) {
	// Setup test repository
	repoPath, cleanupRepo := setupTestRepo(t)
	defer cleanupRepo()

	// Setup test database
	mainDB, cleanupDB := setupTestDB(t, repoPath)
	defer cleanupDB()

	// Create sandbox manager
	sandboxRoot := filepath.Join(repoPath, ".sandboxes")
	cfg := Config{
		SandboxRoot: sandboxRoot,
		ParentRepo:  repoPath,
		MainDB:      mainDB,
	}

	manager, err := NewManager(cfg)
	if err != nil {
		t.Fatalf("Failed to create manager: %v", err)
	}

	ctx := context.Background()

	// Create test mission
	mission := &types.Mission{
		Issue: types.Issue{
			Title:        "Mission to cleanup",
			Description:  "Test cleanup",
			IssueType:    types.TypeEpic,
			IssueSubtype: types.SubtypeMission,
			Status:       types.StatusOpen,
			Priority:     1,
		},
	}

	if err := mainDB.CreateMission(ctx, mission, "test"); err != nil {
		t.Fatalf("Failed to create mission: %v", err)
	}

	// Create sandbox
	_, err = CreateMissionSandbox(ctx, manager, mainDB, mission.ID)
	if err != nil {
		t.Fatalf("Failed to create sandbox: %v", err)
	}

	t.Run("cleans up sandbox and clears metadata", func(t *testing.T) {
		// Verify sandbox exists before cleanup
		sandboxBefore, err := GetMissionSandbox(ctx, manager, mainDB, mission.ID)
		if err != nil {
			t.Fatalf("Failed to get sandbox before cleanup: %v", err)
		}
		if sandboxBefore == nil {
			t.Fatal("Expected sandbox before cleanup, got nil")
		}

		// Cleanup
		err = CleanupMissionSandbox(ctx, manager, mainDB, mission.ID)
		if err != nil {
			t.Fatalf("CleanupMissionSandbox() failed: %v", err)
		}

		// Verify metadata cleared
		updatedMission, err := mainDB.GetMission(ctx, mission.ID)
		if err != nil {
			t.Fatalf("Failed to get mission after cleanup: %v", err)
		}

		if updatedMission.SandboxPath != "" {
			t.Errorf("Expected empty sandbox_path after cleanup, got %s", updatedMission.SandboxPath)
		}
		if updatedMission.BranchName != "" {
			t.Errorf("Expected empty branch_name after cleanup, got %s", updatedMission.BranchName)
		}

		// Verify sandbox no longer in manager's list
		sandboxAfter, err := GetMissionSandbox(ctx, manager, mainDB, mission.ID)
		if err != nil {
			t.Fatalf("GetMissionSandbox() after cleanup failed: %v", err)
		}
		if sandboxAfter != nil {
			t.Errorf("Expected nil after cleanup, got %v", sandboxAfter)
		}
	})

	t.Run("succeeds for mission without sandbox (idempotent)", func(t *testing.T) {
		// Create new mission without sandbox
		mission2 := &types.Mission{
			Issue: types.Issue{
				Title:        "Mission without sandbox",
				Description:  "No sandbox",
				IssueType:    types.TypeEpic,
				IssueSubtype: types.SubtypeMission,
				Status:       types.StatusOpen,
				Priority:     1,
			},
		}

		if err := mainDB.CreateMission(ctx, mission2, "test"); err != nil {
			t.Fatalf("Failed to create mission2: %v", err)
		}

		// Cleanup should succeed without error
		err := CleanupMissionSandbox(ctx, manager, mainDB, mission2.ID)
		if err != nil {
			t.Errorf("Expected no error for mission without sandbox, got %v", err)
		}
	})
}
