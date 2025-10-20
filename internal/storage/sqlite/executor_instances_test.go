package sqlite

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/steveyegge/vc/internal/types"
)

func TestRegisterInstance(t *testing.T) {
	db := setupTestDB(t)
	defer func() { _ = db.Close() }()

	ctx := context.Background()
	now := time.Now()

	instance := &types.ExecutorInstance{
		InstanceID:    "test-instance-1",
		Hostname:      "test-host",
		PID:           12345,
		Status:        types.ExecutorStatusRunning,
		StartedAt:     now,
		LastHeartbeat: now,
		Version:       "0.1.0",
		Metadata:      `{"key":"value"}`,
	}

	// Register new instance
	err := db.RegisterInstance(ctx, instance)
	if err != nil {
		t.Fatalf("Failed to register instance: %v", err)
	}

	// Verify instance was registered
	instances, err := db.GetActiveInstances(ctx)
	if err != nil {
		t.Fatalf("Failed to get active instances: %v", err)
	}

	if len(instances) != 1 {
		t.Fatalf("Expected 1 active instance, got %d", len(instances))
	}

	if instances[0].InstanceID != instance.InstanceID {
		t.Errorf("Instance ID mismatch: got %s, want %s", instances[0].InstanceID, instance.InstanceID)
	}
	if instances[0].Hostname != instance.Hostname {
		t.Errorf("Hostname mismatch: got %s, want %s", instances[0].Hostname, instance.Hostname)
	}
	if instances[0].PID != instance.PID {
		t.Errorf("PID mismatch: got %d, want %d", instances[0].PID, instance.PID)
	}
}

func TestRegisterInstanceUpsert(t *testing.T) {
	db := setupTestDB(t)
	defer func() { _ = db.Close() }()

	ctx := context.Background()
	now := time.Now()

	instance := &types.ExecutorInstance{
		InstanceID:    "test-instance-1",
		Hostname:      "test-host-1",
		PID:           12345,
		Status:        types.ExecutorStatusRunning,
		StartedAt:     now,
		LastHeartbeat: now,
		Version:       "0.1.0",
		Metadata:      `{}`,
	}

	// Register instance first time
	err := db.RegisterInstance(ctx, instance)
	if err != nil {
		t.Fatalf("Failed to register instance: %v", err)
	}

	// Update instance (same ID, different hostname)
	instance.Hostname = "test-host-2"
	instance.PID = 67890
	err = db.RegisterInstance(ctx, instance)
	if err != nil {
		t.Fatalf("Failed to update instance: %v", err)
	}

	// Verify only one instance exists with updated data
	instances, err := db.GetActiveInstances(ctx)
	if err != nil {
		t.Fatalf("Failed to get active instances: %v", err)
	}

	if len(instances) != 1 {
		t.Fatalf("Expected 1 active instance after upsert, got %d", len(instances))
	}

	if instances[0].Hostname != "test-host-2" {
		t.Errorf("Hostname not updated: got %s, want test-host-2", instances[0].Hostname)
	}
	if instances[0].PID != 67890 {
		t.Errorf("PID not updated: got %d, want 67890", instances[0].PID)
	}
}

func TestUpdateHeartbeat(t *testing.T) {
	db := setupTestDB(t)
	defer func() { _ = db.Close() }()

	ctx := context.Background()
	now := time.Now()

	instance := &types.ExecutorInstance{
		InstanceID:    "test-instance-1",
		Hostname:      "test-host",
		PID:           12345,
		Status:        types.ExecutorStatusRunning,
		StartedAt:     now,
		LastHeartbeat: now.Add(-5 * time.Minute), // Old heartbeat
		Version:       "0.1.0",
		Metadata:      `{}`,
	}

	err := db.RegisterInstance(ctx, instance)
	if err != nil {
		t.Fatalf("Failed to register instance: %v", err)
	}

	// Sleep briefly to ensure timestamp changes
	time.Sleep(10 * time.Millisecond)

	// Update heartbeat
	err = db.UpdateHeartbeat(ctx, instance.InstanceID)
	if err != nil {
		t.Fatalf("Failed to update heartbeat: %v", err)
	}

	// Verify heartbeat was updated
	instances, err := db.GetActiveInstances(ctx)
	if err != nil {
		t.Fatalf("Failed to get active instances: %v", err)
	}

	if len(instances) != 1 {
		t.Fatalf("Expected 1 active instance, got %d", len(instances))
	}

	// New heartbeat should be more recent than old one
	if !instances[0].LastHeartbeat.After(instance.LastHeartbeat) {
		t.Errorf("Heartbeat not updated: old=%v, new=%v", instance.LastHeartbeat, instances[0].LastHeartbeat)
	}
}

func TestUpdateHeartbeatNonExistent(t *testing.T) {
	db := setupTestDB(t)
	defer func() { _ = db.Close() }()

	ctx := context.Background()

	// Try to update heartbeat for non-existent instance
	err := db.UpdateHeartbeat(ctx, "non-existent-id")
	if err == nil {
		t.Error("Expected error when updating heartbeat for non-existent instance")
	}
}

func TestGetActiveInstances(t *testing.T) {
	db := setupTestDB(t)
	defer func() { _ = db.Close() }()

	ctx := context.Background()
	now := time.Now()

	// Register multiple instances
	instances := []*types.ExecutorInstance{
		{
			InstanceID:    "instance-1",
			Hostname:      "host-1",
			PID:           100,
			Status:        types.ExecutorStatusRunning,
			StartedAt:     now,
			LastHeartbeat: now,
			Version:       "0.1.0",
			Metadata:      `{}`,
		},
		{
			InstanceID:    "instance-2",
			Hostname:      "host-2",
			PID:           200,
			Status:        types.ExecutorStatusRunning,
			StartedAt:     now,
			LastHeartbeat: now,
			Version:       "0.1.0",
			Metadata:      `{}`,
		},
		{
			InstanceID:    "instance-3",
			Hostname:      "host-3",
			PID:           300,
			Status:        types.ExecutorStatusStopped, // Not running
			StartedAt:     now,
			LastHeartbeat: now,
			Version:       "0.1.0",
			Metadata:      `{}`,
		},
	}

	for _, instance := range instances {
		err := db.RegisterInstance(ctx, instance)
		if err != nil {
			t.Fatalf("Failed to register instance %s: %v", instance.InstanceID, err)
		}
	}

	// Get active instances (should only return running ones)
	active, err := db.GetActiveInstances(ctx)
	if err != nil {
		t.Fatalf("Failed to get active instances: %v", err)
	}

	if len(active) != 2 {
		t.Errorf("Expected 2 active instances, got %d", len(active))
	}

	// Verify stopped instance is not in results
	for _, inst := range active {
		if inst.Status != "running" {
			t.Errorf("Got non-running instance in active results: %s (status=%s)", inst.InstanceID, inst.Status)
		}
	}
}

func TestCleanupStaleInstances(t *testing.T) {
	db := setupTestDB(t)
	defer func() { _ = db.Close() }()

	ctx := context.Background()
	now := time.Now()

	// Register instances with different heartbeat times
	instances := []*types.ExecutorInstance{
		{
			InstanceID:    "fresh-instance",
			Hostname:      "host-1",
			PID:           100,
			Status:        types.ExecutorStatusRunning,
			StartedAt:     now,
			LastHeartbeat: now, // Fresh
			Version:       "0.1.0",
			Metadata:      `{}`,
		},
		{
			InstanceID:    "stale-instance",
			Hostname:      "host-2",
			PID:           200,
			Status:        types.ExecutorStatusRunning,
			StartedAt:     now.Add(-10 * time.Minute),
			LastHeartbeat: now.Add(-10 * time.Minute), // Stale (10 minutes old)
			Version:       "0.1.0",
			Metadata:      `{}`,
		},
	}

	for _, instance := range instances {
		err := db.RegisterInstance(ctx, instance)
		if err != nil {
			t.Fatalf("Failed to register instance %s: %v", instance.InstanceID, err)
		}
	}

	// Cleanup instances stale by more than 5 minutes
	cleaned, err := db.CleanupStaleInstances(ctx, 300) // 300 seconds = 5 minutes
	if err != nil {
		t.Fatalf("Failed to cleanup stale instances: %v", err)
	}

	if cleaned != 1 {
		t.Errorf("Expected to cleanup 1 stale instance, cleaned %d", cleaned)
	}

	// Verify only fresh instance is still active
	active, err := db.GetActiveInstances(ctx)
	if err != nil {
		t.Fatalf("Failed to get active instances: %v", err)
	}

	if len(active) != 1 {
		t.Fatalf("Expected 1 active instance after cleanup, got %d", len(active))
	}

	if active[0].InstanceID != "fresh-instance" {
		t.Errorf("Wrong instance active after cleanup: got %s, want fresh-instance", active[0].InstanceID)
	}
}

func TestCleanupStaleInstancesNoneStale(t *testing.T) {
	db := setupTestDB(t)
	defer func() { _ = db.Close() }()

	ctx := context.Background()
	now := time.Now()

	instance := &types.ExecutorInstance{
		InstanceID:    "fresh-instance",
		Hostname:      "host-1",
		PID:           100,
		Status:        types.ExecutorStatusRunning,
		StartedAt:     now,
		LastHeartbeat: now,
		Version:       "0.1.0",
		Metadata:      `{}`,
	}

	err := db.RegisterInstance(ctx, instance)
	if err != nil {
		t.Fatalf("Failed to register instance: %v", err)
	}

	// Cleanup with aggressive threshold - should find nothing
	cleaned, err := db.CleanupStaleInstances(ctx, 1) // 1 second
	if err != nil {
		t.Fatalf("Failed to cleanup stale instances: %v", err)
	}

	if cleaned != 0 {
		t.Errorf("Expected to cleanup 0 instances, cleaned %d", cleaned)
	}
}

func TestCleanupStaleInstancesReleasesClaimedIssues(t *testing.T) {
	db := setupTestDB(t)
	defer func() { _ = db.Close() }()

	ctx := context.Background()
	now := time.Now()

	// Create a stale executor instance
	staleInstance := &types.ExecutorInstance{
		InstanceID:    "stale-instance",
		Hostname:      "host-1",
		PID:           100,
		Status:        types.ExecutorStatusRunning,
		StartedAt:     now.Add(-10 * time.Minute),
		LastHeartbeat: now.Add(-10 * time.Minute), // Stale (10 minutes old)
		Version:       "0.1.0",
		Metadata:      `{}`,
	}

	err := db.RegisterInstance(ctx, staleInstance)
	if err != nil {
		t.Fatalf("Failed to register stale instance: %v", err)
	}

	// Create a test issue
	issue := &types.Issue{
		ID:          "vc-test-1",
		Title:       "Test Issue",
		Description: "Test Description",
		Status:      types.StatusOpen,
		IssueType:   types.TypeTask,
		Priority:    1,
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	err = db.CreateIssue(ctx, issue, "test-actor")
	if err != nil {
		t.Fatalf("Failed to create test issue: %v", err)
	}

	// Have the stale instance claim the issue
	err = db.ClaimIssue(ctx, issue.ID, staleInstance.InstanceID)
	if err != nil {
		t.Fatalf("Failed to claim issue: %v", err)
	}

	// Verify issue is claimed
	execState, err := db.GetExecutionState(ctx, issue.ID)
	if err != nil {
		t.Fatalf("Failed to get execution state: %v", err)
	}
	if execState == nil {
		t.Fatal("Expected execution state to exist after claiming")
	}
	if execState.ExecutorInstanceID != staleInstance.InstanceID {
		t.Errorf("Wrong executor claimed issue: got %s, want %s", execState.ExecutorInstanceID, staleInstance.InstanceID)
	}

	// Verify issue status is in_progress
	claimedIssue, err := db.GetIssue(ctx, issue.ID)
	if err != nil {
		t.Fatalf("Failed to get issue: %v", err)
	}
	if claimedIssue.Status != types.StatusInProgress {
		t.Errorf("Issue status should be in_progress after claiming, got %s", claimedIssue.Status)
	}

	// Cleanup stale instances (should release the claimed issue)
	cleaned, err := db.CleanupStaleInstances(ctx, 300) // 5 minutes
	if err != nil {
		t.Fatalf("Failed to cleanup stale instances: %v", err)
	}

	if cleaned != 1 {
		t.Errorf("Expected to cleanup 1 stale instance, cleaned %d", cleaned)
	}

	// Verify execution state was deleted
	execState, err = db.GetExecutionState(ctx, issue.ID)
	if err != nil {
		t.Fatalf("Failed to get execution state after cleanup: %v", err)
	}
	if execState != nil {
		t.Error("Expected execution state to be deleted after cleanup")
	}

	// Verify issue status was reset to 'open'
	releasedIssue, err := db.GetIssue(ctx, issue.ID)
	if err != nil {
		t.Fatalf("Failed to get issue after cleanup: %v", err)
	}
	if releasedIssue.Status != types.StatusOpen {
		t.Errorf("Issue status should be open after cleanup, got %s", releasedIssue.Status)
	}

	// Verify a comment was added explaining the release
	events, err := db.GetEvents(ctx, issue.ID, 10)
	if err != nil {
		t.Fatalf("Failed to get events: %v", err)
	}

	foundReleaseComment := false
	for _, event := range events {
		if event.Comment != nil && event.Actor == "system" && strings.Contains(*event.Comment, "automatically released") {
			foundReleaseComment = true
			if !strings.Contains(*event.Comment, staleInstance.InstanceID) {
				t.Errorf("Release comment should mention instance ID, got: %s", *event.Comment)
			}
			break
		}
	}

	if !foundReleaseComment {
		t.Error("Expected to find a system comment explaining the release")
	}
}

func TestRegisterInstanceValidation(t *testing.T) {
	db := setupTestDB(t)
	defer func() { _ = db.Close() }()

	ctx := context.Background()
	now := time.Now()

	tests := []struct {
		name     string
		instance *types.ExecutorInstance
		wantErr  bool
		errMsg   string
	}{
		{
			name: "empty instance_id",
			instance: &types.ExecutorInstance{
				InstanceID:    "",
				Hostname:      "test-host",
				PID:           12345,
				Status:        types.ExecutorStatusRunning,
				StartedAt:     now,
				LastHeartbeat: now,
			},
			wantErr: true,
			errMsg:  "instance_id is required",
		},
		{
			name: "empty hostname",
			instance: &types.ExecutorInstance{
				InstanceID:    "test-1",
				Hostname:      "",
				PID:           12345,
				Status:        types.ExecutorStatusRunning,
				StartedAt:     now,
				LastHeartbeat: now,
			},
			wantErr: true,
			errMsg:  "hostname is required",
		},
		{
			name: "negative PID",
			instance: &types.ExecutorInstance{
				InstanceID:    "test-1",
				Hostname:      "test-host",
				PID:           -1,
				Status:        types.ExecutorStatusRunning,
				StartedAt:     now,
				LastHeartbeat: now,
			},
			wantErr: true,
			errMsg:  "pid must be positive",
		},
		{
			name: "zero PID",
			instance: &types.ExecutorInstance{
				InstanceID:    "test-1",
				Hostname:      "test-host",
				PID:           0,
				Status:        types.ExecutorStatusRunning,
				StartedAt:     now,
				LastHeartbeat: now,
			},
			wantErr: true,
			errMsg:  "pid must be positive",
		},
		{
			name: "invalid status",
			instance: &types.ExecutorInstance{
				InstanceID:    "test-1",
				Hostname:      "test-host",
				PID:           12345,
				Status:        types.ExecutorStatus("invalid"),
				StartedAt:     now,
				LastHeartbeat: now,
			},
			wantErr: true,
			errMsg:  "invalid status",
		},
		{
			name: "invalid JSON metadata",
			instance: &types.ExecutorInstance{
				InstanceID:    "test-1",
				Hostname:      "test-host",
				PID:           12345,
				Status:        types.ExecutorStatusRunning,
				StartedAt:     now,
				LastHeartbeat: now,
				Metadata:      `{invalid json}`,
			},
			wantErr: true,
			errMsg:  "metadata must be valid JSON",
		},
		{
			name: "valid instance",
			instance: &types.ExecutorInstance{
				InstanceID:    "test-1",
				Hostname:      "test-host",
				PID:           12345,
				Status:        types.ExecutorStatusRunning,
				StartedAt:     now,
				LastHeartbeat: now,
				Version:       "0.1.0",
				Metadata:      `{"key":"value"}`,
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := db.RegisterInstance(ctx, tt.instance)
			if tt.wantErr {
				if err == nil {
					t.Errorf("Expected error containing %q, got nil", tt.errMsg)
				} else if tt.errMsg != "" && !strings.Contains(err.Error(), tt.errMsg) {
					t.Errorf("Expected error containing %q, got %q", tt.errMsg, err.Error())
				}
			} else {
				if err != nil {
					t.Errorf("Expected no error, got %v", err)
				}
			}
		})
	}
}

// setupTestDB creates a temporary test database
func setupTestDB(t *testing.T) *SQLiteStorage {
	t.Helper()

	// Create temp file
	tmpfile, err := os.CreateTemp("", "test-*.db")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	_ = tmpfile.Close()

	// Create storage
	storage, err := New(tmpfile.Name())
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}

	// Cleanup function
	t.Cleanup(func() {
		_ = storage.Close()
		_ = os.Remove(tmpfile.Name())
	})

	return storage
}
