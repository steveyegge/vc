package sandbox

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/steveyegge/vc/internal/storage"
	"github.com/steveyegge/vc/internal/types"
)

// setupTestDB creates a temporary beads database for testing
func setupTestDB(t *testing.T, repoPath string) (storage.Storage, func()) {
	t.Helper()

	// Create .beads directory in repo
	beadsDir := filepath.Join(repoPath, ".beads")
	if err := os.MkdirAll(beadsDir, 0755); err != nil {
		t.Fatalf("Failed to create .beads dir: %v", err)
	}

	dbPath := filepath.Join(beadsDir, "vc.db")
	cfg := &storage.Config{
		Path: dbPath,
	}

	store, err := storage.NewStorage(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Failed to create test database: %v", err)
	}

	cleanup := func() {
		store.Close()
	}

	return store, cleanup
}

func TestNewManager(t *testing.T) {
	repoPath, cleanupRepo := setupTestRepo(t)
	defer cleanupRepo()

	mainDB, cleanupDB := setupTestDB(t, repoPath)
	defer cleanupDB()

	sandboxRoot := filepath.Join(repoPath, "sandboxes")

	tests := []struct {
		name    string
		config  Config
		wantErr bool
	}{
		{
			name: "valid config",
			config: Config{
				SandboxRoot: sandboxRoot,
				ParentRepo:  repoPath,
				MainDB:      mainDB,
			},
			wantErr: false,
		},
		{
			name: "missing sandbox root",
			config: Config{
				ParentRepo: repoPath,
				MainDB:     mainDB,
			},
			wantErr: true,
		},
		{
			name: "missing parent repo",
			config: Config{
				SandboxRoot: sandboxRoot,
				MainDB:      mainDB,
			},
			wantErr: true,
		},
		{
			name: "missing main DB",
			config: Config{
				SandboxRoot: sandboxRoot,
				ParentRepo:  repoPath,
			},
			wantErr: true,
		},
		{
			name: "invalid parent repo",
			config: Config{
				SandboxRoot: sandboxRoot,
				ParentRepo:  "/nonexistent/path",
				MainDB:      mainDB,
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mgr, err := NewManager(tt.config)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewManager() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && mgr == nil {
				t.Error("NewManager() returned nil manager")
			}
		})
	}
}

func TestManager_Create(t *testing.T) {
	repoPath, cleanupRepo := setupTestRepo(t)
	defer cleanupRepo()

	mainDB, cleanupDB := setupTestDB(t, repoPath)
	defer cleanupDB()

	// Create a test mission in the main database
	ctx := context.Background()
	mission := &types.Issue{
		ID:          "vc-100",
		IssueType:   types.TypeTask,
		Status:      types.StatusOpen,
		Priority:    1,
		Title:       "Test Mission",
		Description: "Test mission description",
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}
	if err := mainDB.CreateIssue(ctx, mission, "test"); err != nil {
		t.Fatalf("Failed to create test mission: %v", err)
	}

	sandboxRoot := filepath.Join(repoPath, "sandboxes")

	mgr, err := NewManager(Config{
		SandboxRoot: sandboxRoot,
		ParentRepo:  repoPath,
		MainDB:      mainDB,
	})
	if err != nil {
		t.Fatalf("Failed to create manager: %v", err)
	}

	// Create sandbox
	sandboxCfg := SandboxConfig{
		MissionID:   "vc-100",
		ParentRepo:  repoPath,
		BaseBranch:  "main",
		SandboxRoot: sandboxRoot,
	}

	sandbox, err := mgr.Create(ctx, sandboxCfg)
	if err != nil {
		t.Fatalf("Create() failed: %v", err)
	}

	// Verify sandbox was created
	if sandbox == nil {
		t.Fatal("Create() returned nil sandbox")
	}

	if sandbox.ID == "" {
		t.Error("Sandbox ID is empty")
	}

	if sandbox.MissionID != "vc-100" {
		t.Errorf("Sandbox MissionID = %v, want %v", sandbox.MissionID, "vc-100")
	}

	if sandbox.Status != SandboxStatusActive {
		t.Errorf("Sandbox Status = %v, want %v", sandbox.Status, SandboxStatusActive)
	}

	// Verify worktree was created
	if _, err := os.Stat(sandbox.GitWorktree); os.IsNotExist(err) {
		t.Errorf("Worktree directory doesn't exist: %s", sandbox.GitWorktree)
	}

	// Verify .git exists in worktree
	gitPath := filepath.Join(sandbox.GitWorktree, ".git")
	if _, err := os.Stat(gitPath); os.IsNotExist(err) {
		t.Error("Worktree doesn't have .git")
	}

	// Verify beads database was created
	if _, err := os.Stat(sandbox.BeadsDB); os.IsNotExist(err) {
		t.Errorf("Beads database doesn't exist: %s", sandbox.BeadsDB)
	}

	// Verify mission was copied to sandbox DB
	sandboxDB, err := storage.NewStorage(ctx, &storage.Config{Path: sandbox.BeadsDB})
	if err != nil {
		t.Fatalf("Failed to open sandbox DB: %v", err)
	}
	defer sandboxDB.Close()

	sandboxMission, err := sandboxDB.GetIssue(ctx, "vc-100")
	if err != nil {
		t.Fatalf("Failed to get mission from sandbox DB: %v", err)
	}
	if sandboxMission == nil {
		t.Error("Mission was not copied to sandbox DB")
	}

	// Clean up sandbox
	if err := mgr.Cleanup(ctx, sandbox); err != nil {
		t.Errorf("Cleanup() failed: %v", err)
	}
}

func TestManager_GetAndList(t *testing.T) {
	repoPath, cleanupRepo := setupTestRepo(t)
	defer cleanupRepo()

	mainDB, cleanupDB := setupTestDB(t, repoPath)
	defer cleanupDB()

	ctx := context.Background()

	// Create test missions
	for i := 1; i <= 3; i++ {
		missionID := fmt.Sprintf("vc-%d", 3000+i)
		mission := &types.Issue{
			ID:        missionID,
			IssueType: types.TypeTask,
			Status:    types.StatusOpen,
			Priority:  1,
			Title:     "Test Mission " + missionID,
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		}
		if err := mainDB.CreateIssue(ctx, mission, "test"); err != nil {
			t.Fatalf("Failed to create test mission: %v", err)
		}
	}

	sandboxRoot := filepath.Join(repoPath, "sandboxes")

	mgr, err := NewManager(Config{
		SandboxRoot: sandboxRoot,
		ParentRepo:  repoPath,
		MainDB:      mainDB,
	})
	if err != nil {
		t.Fatalf("Failed to create manager: %v", err)
	}

	// Create two sandboxes
	sandbox1, err := mgr.Create(ctx, SandboxConfig{
		MissionID:   "vc-3001",
		ParentRepo:  repoPath,
		BaseBranch:  "main",
		SandboxRoot: sandboxRoot,
	})
	if err != nil {
		t.Fatalf("Failed to create sandbox1: %v", err)
	}
	defer mgr.Cleanup(ctx, sandbox1)

	sandbox2, err := mgr.Create(ctx, SandboxConfig{
		MissionID:   "vc-3002",
		ParentRepo:  repoPath,
		BaseBranch:  "main",
		SandboxRoot: sandboxRoot,
	})
	if err != nil {
		t.Fatalf("Failed to create sandbox2: %v", err)
	}
	defer mgr.Cleanup(ctx, sandbox2)

	// Test Get
	t.Run("Get existing sandbox", func(t *testing.T) {
		got, err := mgr.Get(ctx, sandbox1.ID)
		if err != nil {
			t.Errorf("Get() error = %v", err)
		}
		if got == nil {
			t.Error("Get() returned nil")
		}
		if got != nil && got.ID != sandbox1.ID {
			t.Errorf("Get() returned wrong sandbox: got %v, want %v", got.ID, sandbox1.ID)
		}
	})

	t.Run("Get non-existent sandbox", func(t *testing.T) {
		got, err := mgr.Get(ctx, "nonexistent")
		if err != nil {
			t.Errorf("Get() error = %v", err)
		}
		if got != nil {
			t.Error("Get() should return nil for non-existent sandbox")
		}
	})

	// Test List
	t.Run("List sandboxes", func(t *testing.T) {
		sandboxes, err := mgr.List(ctx)
		if err != nil {
			t.Errorf("List() error = %v", err)
		}
		if len(sandboxes) != 2 {
			t.Errorf("List() returned %d sandboxes, want 2", len(sandboxes))
		}
	})
}

func TestManager_InspectState(t *testing.T) {
	repoPath, cleanupRepo := setupTestRepo(t)
	defer cleanupRepo()

	mainDB, cleanupDB := setupTestDB(t, repoPath)
	defer cleanupDB()

	ctx := context.Background()

	// Create test mission
	mission := &types.Issue{
		ID:        "vc-4001",
		IssueType: types.TypeTask,
		Status:    types.StatusOpen,
		Priority:  1,
		Title:     "Test Mission",
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	if err := mainDB.CreateIssue(ctx, mission, "test"); err != nil {
		t.Fatalf("Failed to create test mission: %v", err)
	}

	sandboxRoot := filepath.Join(repoPath, "sandboxes")

	mgr, err := NewManager(Config{
		SandboxRoot: sandboxRoot,
		ParentRepo:  repoPath,
		MainDB:      mainDB,
	})
	if err != nil {
		t.Fatalf("Failed to create manager: %v", err)
	}

	sandbox, err := mgr.Create(ctx, SandboxConfig{
		MissionID:   "vc-4001",
		ParentRepo:  repoPath,
		BaseBranch:  "main",
		SandboxRoot: sandboxRoot,
	})
	if err != nil {
		t.Fatalf("Failed to create sandbox: %v", err)
	}
	defer mgr.Cleanup(ctx, sandbox)

	// Modify a file in the sandbox
	testFile := filepath.Join(sandbox.GitWorktree, "test.txt")
	if err := os.WriteFile(testFile, []byte("test content\n"), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	// Inspect state
	state, err := mgr.InspectState(ctx, sandbox)
	if err != nil {
		t.Fatalf("InspectState() failed: %v", err)
	}

	if state == nil {
		t.Fatal("InspectState() returned nil")
	}

	if state.Sandbox.ID != sandbox.ID {
		t.Errorf("State sandbox ID = %v, want %v", state.Sandbox.ID, sandbox.ID)
	}

	// Should have git status (file is untracked)
	if state.GitStatus == "" {
		t.Error("GitStatus is empty, expected untracked file status")
	}

	// Should have modified files
	if len(state.ModifiedFiles) == 0 {
		t.Error("ModifiedFiles is empty, expected at least one file")
	}

	if state.WorkState == nil {
		t.Error("WorkState is nil")
	}
}

func TestManager_Cleanup(t *testing.T) {
	repoPath, cleanupRepo := setupTestRepo(t)
	defer cleanupRepo()

	mainDB, cleanupDB := setupTestDB(t, repoPath)
	defer cleanupDB()

	ctx := context.Background()

	// Create test missions for both subtests
	mission1 := &types.Issue{
		ID:        "vc-1001",
		IssueType: types.TypeTask,
		Status:    types.StatusOpen,
		Priority:  1,
		Title:     "Test Mission 1",
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	if err := mainDB.CreateIssue(ctx, mission1, "test"); err != nil {
		t.Fatalf("Failed to create test mission 1: %v", err)
	}

	mission2 := &types.Issue{
		ID:        "vc-1002",
		IssueType: types.TypeTask,
		Status:    types.StatusOpen,
		Priority:  1,
		Title:     "Test Mission 2",
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	if err := mainDB.CreateIssue(ctx, mission2, "test"); err != nil {
		t.Fatalf("Failed to create test mission 2: %v", err)
	}

	sandboxRoot := filepath.Join(repoPath, "sandboxes")

	t.Run("cleanup removes worktree and directory", func(t *testing.T) {
		mgr, err := NewManager(Config{
			SandboxRoot:       sandboxRoot,
			ParentRepo:        repoPath,
			MainDB:            mainDB,
			PreserveOnFailure: false,
		})
		if err != nil {
			t.Fatalf("Failed to create manager: %v", err)
		}

		sandbox, err := mgr.Create(ctx, SandboxConfig{
			MissionID:   "vc-1001",
			ParentRepo:  repoPath,
			BaseBranch:  "main",
			SandboxRoot: sandboxRoot,
		})
		if err != nil {
			t.Fatalf("Failed to create sandbox: %v", err)
		}

		worktreePath := sandbox.GitWorktree

		// Cleanup
		if err := mgr.Cleanup(ctx, sandbox); err != nil {
			t.Fatalf("Cleanup() failed: %v", err)
		}

		// Verify worktree was removed
		if _, err := os.Stat(worktreePath); !os.IsNotExist(err) {
			t.Error("Worktree still exists after cleanup")
		}

		// Verify sandbox was removed from active map
		got, err := mgr.Get(ctx, sandbox.ID)
		if err != nil {
			t.Errorf("Get() error = %v", err)
		}
		if got != nil {
			t.Error("Sandbox still in active map after cleanup")
		}
	})

	t.Run("PreserveOnFailure keeps failed sandbox", func(t *testing.T) {
		mgr, err := NewManager(Config{
			SandboxRoot:       sandboxRoot + "-preserve",
			ParentRepo:        repoPath,
			MainDB:            mainDB,
			PreserveOnFailure: true,
		})
		if err != nil {
			t.Fatalf("Failed to create manager: %v", err)
		}

		sandbox, err := mgr.Create(ctx, SandboxConfig{
			MissionID:   "vc-1002",
			ParentRepo:  repoPath,
			BaseBranch:  "main",
			SandboxRoot: sandboxRoot + "-preserve",
		})
		if err != nil {
			t.Fatalf("Failed to create sandbox: %v", err)
		}

		worktreePath := sandbox.GitWorktree

		// Mark sandbox as failed
		sandbox.Status = SandboxStatusFailed

		// Cleanup
		if err := mgr.Cleanup(ctx, sandbox); err != nil {
			t.Fatalf("Cleanup() failed: %v", err)
		}

		// Verify worktree still exists (preserved)
		if _, err := os.Stat(worktreePath); os.IsNotExist(err) {
			t.Error("Worktree was removed despite PreserveOnFailure=true and Status=Failed")
		}

		// Clean up manually for next test
		removeWorktree(ctx, repoPath, worktreePath)
	})
}

func TestManager_CleanupAll(t *testing.T) {
	repoPath, cleanupRepo := setupTestRepo(t)
	defer cleanupRepo()

	mainDB, cleanupDB := setupTestDB(t, repoPath)
	defer cleanupDB()

	ctx := context.Background()

	// Create test missions
	for i := 1; i <= 3; i++ {
		missionID := fmt.Sprintf("vc-%d", 2000+i)
		mission := &types.Issue{
			ID:        missionID,
			IssueType: types.TypeTask,
			Status:    types.StatusOpen,
			Priority:  1,
			Title:     "Test Mission " + missionID,
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		}
		if err := mainDB.CreateIssue(ctx, mission, "test"); err != nil {
			t.Fatalf("Failed to create test mission: %v", err)
		}
	}

	sandboxRoot := filepath.Join(repoPath, "sandboxes-cleanupall")

	mgr, err := NewManager(Config{
		SandboxRoot: sandboxRoot,
		ParentRepo:  repoPath,
		MainDB:      mainDB,
	})
	if err != nil {
		t.Fatalf("Failed to create manager: %v", err)
	}

	// Create three sandboxes
	sandbox1, err := mgr.Create(ctx, SandboxConfig{
		MissionID:   "vc-2001",
		ParentRepo:  repoPath,
		BaseBranch:  "main",
		SandboxRoot: sandboxRoot,
	})
	if err != nil {
		t.Fatalf("Failed to create sandbox1: %v", err)
	}

	sandbox2, err := mgr.Create(ctx, SandboxConfig{
		MissionID:   "vc-2002",
		ParentRepo:  repoPath,
		BaseBranch:  "main",
		SandboxRoot: sandboxRoot,
	})
	if err != nil {
		t.Fatalf("Failed to create sandbox2: %v", err)
	}

	sandbox3, err := mgr.Create(ctx, SandboxConfig{
		MissionID:   "vc-2003",
		ParentRepo:  repoPath,
		BaseBranch:  "main",
		SandboxRoot: sandboxRoot,
	})
	if err != nil {
		t.Fatalf("Failed to create sandbox3: %v", err)
	}

	// Make sandbox1 and sandbox2 appear old by manipulating LastUsed
	// (In real usage, this would happen naturally over time)
	m := mgr.(*manager)
	m.mu.Lock()
	m.activeSandboxes[sandbox1.ID].LastUsed = time.Now().Add(-2 * time.Hour)
	m.activeSandboxes[sandbox2.ID].LastUsed = time.Now().Add(-2 * time.Hour)
	m.mu.Unlock()

	// Clean up sandboxes older than 1 hour
	if err := mgr.CleanupAll(ctx, 1*time.Hour); err != nil {
		t.Logf("CleanupAll() returned error: %v (may be expected)", err)
	}

	// Verify old sandboxes were cleaned up
	if got, _ := mgr.Get(ctx, sandbox1.ID); got != nil {
		t.Error("Old sandbox1 was not cleaned up")
	}
	if got, _ := mgr.Get(ctx, sandbox2.ID); got != nil {
		t.Error("Old sandbox2 was not cleaned up")
	}

	// Verify recent sandbox is still active
	if got, _ := mgr.Get(ctx, sandbox3.ID); got == nil {
		t.Error("Recent sandbox3 was incorrectly cleaned up")
	} else {
		// Clean up remaining sandbox
		mgr.Cleanup(ctx, sandbox3)
	}
}
