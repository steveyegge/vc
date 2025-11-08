package executor

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/steveyegge/vc/internal/ai"
	"github.com/steveyegge/vc/internal/events"
	"github.com/steveyegge/vc/internal/storage"
	"github.com/steveyegge/vc/internal/types"
)

// TestHandleIncompleteWork tests the incomplete work retry/escalation logic (vc-1ows, vc-rd1z)
func TestHandleIncompleteWork(t *testing.T) {
	tests := []struct {
		name                    string
		existingComments        []string          // Prior comments to set up history
		analysis                *ai.Analysis      // Analysis to pass to handleIncompleteWork
		expectedRetry           bool              // Should issue be left open for retry?
		expectedEscalation      bool              // Should issue be escalated?
		expectedCommentContains string            // Comment text to verify
		expectedLabel           string            // Label to check for
		expectedStatus          types.Status // Expected final status
		expectedEventType       events.EventType  // Expected event type
		expectedEventSeverity   events.EventSeverity
	}{
		{
			name:             "first incomplete attempt - retry",
			existingComments: []string{},
			analysis: &ai.Analysis{
				Completed: false,
				Summary:   "Agent read files but did not make required edits",
			},
			expectedRetry:           true,
			expectedEscalation:      false,
			expectedCommentContains: "**Incomplete Work Detected (Attempt #1)**",
			expectedLabel:           "",
			expectedStatus:          types.StatusInProgress, // Stays in_progress after releasing execution state
			expectedEventType:       events.EventTypeProgress,
			expectedEventSeverity:   events.SeverityWarning,
		},
		{
			name: "second incomplete attempt - escalation",
			existingComments: []string{
				"**Incomplete Work Detected (Attempt #1)**\n\nRetrying...",
			},
			analysis: &ai.Analysis{
				Completed: false,
				Summary:   "Agent still not completing work after retry",
			},
			expectedRetry:           false,
			expectedEscalation:      true,
			expectedCommentContains: "**Incomplete Work Escalated**",
			expectedLabel:           "needs-human-review",
			expectedStatus:          types.StatusBlocked,
			expectedEventType:       events.EventTypeProgress,
			expectedEventSeverity:   events.SeverityError,
		},
		{
			name: "third incomplete attempt - still escalated",
			existingComments: []string{
				"**Incomplete Work Detected (Attempt #1)**\n\nRetrying...",
				"**Incomplete Work Detected (Attempt #2)**\n\nRetrying...",
			},
			analysis: &ai.Analysis{
				Completed: false,
				Summary:   "Still incomplete after multiple attempts",
			},
			expectedRetry:           false,
			expectedEscalation:      true,
			expectedCommentContains: "**Incomplete Work Escalated**",
			expectedLabel:           "needs-human-review",
			expectedStatus:          types.StatusBlocked,
			expectedEventType:       events.EventTypeProgress,
			expectedEventSeverity:   events.SeverityError,
		},
		{
			name: "no history edge case - retry",
			existingComments: []string{
				"Some unrelated comment",
				"Another comment without the marker",
			},
			analysis: &ai.Analysis{
				Completed: false,
				Summary:   "First incomplete attempt with unrelated history",
			},
			expectedRetry:           true,
			expectedEscalation:      false,
			expectedCommentContains: "**Incomplete Work Detected (Attempt #1)**",
			expectedLabel:           "",
			expectedStatus:          types.StatusInProgress, // Stays in_progress after releasing execution state
			expectedEventType:       events.EventTypeProgress,
			expectedEventSeverity:   events.SeverityWarning,
		},
		{
			name: "mixed history with other failures - counts only incomplete",
			existingComments: []string{
				"Agent failed with exit code 1",
				"Quality gates failed",
				"**Incomplete Work Detected (Attempt #1)**\n\nRetrying...",
				"Some other comment",
			},
			analysis: &ai.Analysis{
				Completed: false,
				Summary:   "Second incomplete after other failures",
			},
			expectedRetry:           false,
			expectedEscalation:      true,
			expectedCommentContains: "**Incomplete Work Escalated**",
			expectedLabel:           "needs-human-review",
			expectedStatus:          types.StatusBlocked,
			expectedEventType:       events.EventTypeProgress,
			expectedEventSeverity:   events.SeverityError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup storage
			cfg := storage.DefaultConfig()
			cfg.Path = t.TempDir() + "/test.db"

			ctx := context.Background()
			store, err := storage.NewStorage(ctx, cfg)
			if err != nil {
				t.Fatalf("Failed to create storage: %v", err)
			}
			defer func() { _ = store.Close() }()

			// Create test issue
			issue := &types.Issue{
				Title:              "Test incomplete work",
				Description:        "This tests the incomplete work handling",
				IssueType:          types.TypeTask,
				Status:             types.StatusOpen,
				Priority:           1,
				AcceptanceCriteria: "Must complete all acceptance criteria:\n1. Fix the bug\n2. Add tests\n3. Update docs",
				CreatedAt:          time.Now(),
				UpdatedAt:          time.Now(),
			}

			if err := store.CreateIssue(ctx, issue, "test"); err != nil {
				t.Fatalf("Failed to create issue: %v", err)
			}

			// Add existing comments to establish history
			for _, comment := range tt.existingComments {
				if err := store.AddComment(ctx, issue.ID, "ai-supervisor", comment); err != nil {
					t.Fatalf("Failed to add existing comment: %v", err)
				}
			}

			// Register executor instance (required for ClaimIssue foreign key)
			executorID := "test-executor-001"
			instance := &types.ExecutorInstance{
				InstanceID:    executorID,
				Hostname:      "test-host",
				PID:           12345,
				Status:        types.ExecutorStatusRunning,
				StartedAt:     time.Now(),
				LastHeartbeat: time.Now(),
				Version:       "test",
				Metadata:      "{}",
			}
			if err := store.RegisterInstance(ctx, instance); err != nil {
				t.Fatalf("Failed to register executor: %v", err)
			}

			// Claim the issue to set up execution state
			if err := store.ClaimIssue(ctx, issue.ID, executorID); err != nil {
				t.Fatalf("Failed to claim issue: %v", err)
			}

			// Create results processor
			rpCfg := &ResultsProcessorConfig{
				Store:      store,
				WorkingDir: "/tmp/test",
				Actor:      "test-executor",
			}

			rp, err := NewResultsProcessor(rpCfg)
			if err != nil {
				t.Fatalf("Failed to create results processor: %v", err)
			}

			// Call handleIncompleteWork
			if err := rp.handleIncompleteWork(ctx, issue, tt.analysis); err != nil {
				t.Fatalf("handleIncompleteWork failed: %v", err)
			}

			// Verify comment was added
			issueEvents, err := store.GetEvents(ctx, issue.ID, 0)
			if err != nil {
				t.Fatalf("Failed to get events: %v", err)
			}

			// Find the comment added by handleIncompleteWork (last comment event, not label event)
			var lastComment string
			for i := len(issueEvents) - 1; i >= 0; i-- {
				if issueEvents[i].Comment != nil && issueEvents[i].EventType == types.EventCommented {
					lastComment = *issueEvents[i].Comment
					break
				}
			}

			if lastComment == "" {
				t.Error("Expected comment to be added, but none found")
			} else if !strings.Contains(lastComment, tt.expectedCommentContains) {
				t.Errorf("Expected comment to contain %q, got:\n%s", tt.expectedCommentContains, lastComment)
			}

			// Verify acceptance criteria is included in the comment (only for retry/escalation comments)
			if tt.expectedCommentContains != "" && !strings.Contains(lastComment, issue.AcceptanceCriteria) {
				t.Error("Expected comment to include acceptance criteria")
			}

			// Verify analysis summary is included
			if tt.expectedCommentContains != "" && !strings.Contains(lastComment, tt.analysis.Summary) {
				t.Error("Expected comment to include analysis summary")
			}

			// Verify label if expected
			if tt.expectedLabel != "" {
				issueLabels, err := store.GetLabels(ctx, issue.ID)
				if err != nil {
					t.Fatalf("Failed to get labels: %v", err)
				}

				hasLabel := false
				for _, label := range issueLabels {
					if label == tt.expectedLabel {
						hasLabel = true
						break
					}
				}
				if !hasLabel {
					t.Errorf("Expected label %q to be added, but it was not found. Labels: %v", tt.expectedLabel, issueLabels)
				}
			}

			// Verify issue status
			updatedIssue, err := store.GetIssue(ctx, issue.ID)
			if err != nil {
				t.Fatalf("Failed to get updated issue: %v", err)
			}

			if updatedIssue.Status != tt.expectedStatus {
				t.Errorf("Expected status %s, got %s", tt.expectedStatus, updatedIssue.Status)
			}

			// Verify execution state was released
			execState, err := store.GetExecutionState(ctx, issue.ID)
			if err != nil {
				t.Fatalf("Failed to get execution state: %v", err)
			}
			if execState != nil {
				t.Error("Expected execution state to be released, but it still exists")
			}

			// Verify event was emitted
			filter := events.EventFilter{
				IssueID: issue.ID,
				Type:    tt.expectedEventType,
			}
			progressEvents, err := store.GetAgentEvents(ctx, filter)
			if err != nil {
				t.Fatalf("Failed to get agent events: %v", err)
			}

			if len(progressEvents) == 0 {
				t.Errorf("Expected %s event to be emitted, but none found", tt.expectedEventType)
			} else {
				// Verify event severity
				event := progressEvents[len(progressEvents)-1] // Get the last event
				if event.Severity != tt.expectedEventSeverity {
					t.Errorf("Expected severity %s, got %s", tt.expectedEventSeverity, event.Severity)
				}

				// Verify event data contains expected fields
				if _, ok := event.Data["incomplete_attempts"]; !ok {
					t.Error("Expected event data to contain 'incomplete_attempts'")
				}

				if _, ok := event.Data["max_retries"]; !ok {
					t.Error("Expected event data to contain 'max_retries'")
				}

				if _, ok := event.Data["analysis_summary"]; !ok {
					t.Error("Expected event data to contain 'analysis_summary'")
				}

				// Verify escalation flag for escalation cases
				if tt.expectedEscalation {
					if escalated, ok := event.Data["escalated"].(bool); !ok || !escalated {
						t.Error("Expected event data to contain 'escalated: true' for escalation case")
					}
				}
			}
		})
	}
}

// TestHandleIncompleteWorkAttemptCounting verifies that attempt counting only counts incomplete attempts (vc-rd1z)
func TestHandleIncompleteWorkAttemptCounting(t *testing.T) {
	tests := []struct {
		name                 string
		comments             []string // Comments to add
		expectedAttemptCount int      // Expected incomplete attempt count
	}{
		{
			name:                 "no prior attempts",
			comments:             []string{},
			expectedAttemptCount: 1,
		},
		{
			name: "one prior incomplete attempt",
			comments: []string{
				"**Incomplete Work Detected (Attempt #1)**\n\nRetrying...",
			},
			expectedAttemptCount: 2,
		},
		{
			name: "two prior incomplete attempts",
			comments: []string{
				"**Incomplete Work Detected (Attempt #1)**\n\nRetrying...",
				"**Incomplete Work Detected (Attempt #2)**\n\nRetrying...",
			},
			expectedAttemptCount: 3,
		},
		{
			name: "ignores non-incomplete comments",
			comments: []string{
				"Agent failed with exit code 1",
				"Quality gates failed",
				"Some random comment",
				"**Incomplete Work Detected (Attempt #1)**\n\nRetrying...",
			},
			expectedAttemptCount: 2,
		},
		{
			name: "handles mixed history",
			comments: []string{
				"First execution attempt",
				"**Incomplete Work Detected (Attempt #1)**\n\nRetrying...",
				"Second execution failed",
				"Third execution passed but incomplete",
			},
			expectedAttemptCount: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup storage
			cfg := storage.DefaultConfig()
			cfg.Path = t.TempDir() + "/test.db"

			ctx := context.Background()
			store, err := storage.NewStorage(ctx, cfg)
			if err != nil {
				t.Fatalf("Failed to create storage: %v", err)
			}
			defer func() { _ = store.Close() }()

			// Create test issue
			issue := &types.Issue{
				Title:              "Test attempt counting",
				Description:        "This tests the attempt counting logic",
				IssueType:          types.TypeTask,
				Status:             types.StatusOpen,
				Priority:           1,
				AcceptanceCriteria: "Must complete",
				CreatedAt:          time.Now(),
				UpdatedAt:          time.Now(),
			}

			if err := store.CreateIssue(ctx, issue, "test"); err != nil {
				t.Fatalf("Failed to create issue: %v", err)
			}

			// Add comments to establish history
			for _, comment := range tt.comments {
				if err := store.AddComment(ctx, issue.ID, "ai-supervisor", comment); err != nil {
					t.Fatalf("Failed to add comment: %v", err)
				}
			}

			// Register executor instance (required for ClaimIssue foreign key)
			executorID := "test-executor-001"
			instance := &types.ExecutorInstance{
				InstanceID:    executorID,
				Hostname:      "test-host",
				PID:           12345,
				Status:        types.ExecutorStatusRunning,
				StartedAt:     time.Now(),
				LastHeartbeat: time.Now(),
				Version:       "test",
				Metadata:      "{}",
			}
			if err := store.RegisterInstance(ctx, instance); err != nil {
				t.Fatalf("Failed to register executor: %v", err)
			}

			// Claim the issue
			if err := store.ClaimIssue(ctx, issue.ID, executorID); err != nil {
				t.Fatalf("Failed to claim issue: %v", err)
			}

			// Create results processor
			rpCfg := &ResultsProcessorConfig{
				Store:      store,
				WorkingDir: "/tmp/test",
				Actor:      "test-executor",
			}

			rp, err := NewResultsProcessor(rpCfg)
			if err != nil {
				t.Fatalf("Failed to create results processor: %v", err)
			}

			// Call handleIncompleteWork
			analysis := &ai.Analysis{
				Completed: false,
				Summary:   "Test incomplete work",
			}

			if err := rp.handleIncompleteWork(ctx, issue, analysis); err != nil {
				t.Fatalf("handleIncompleteWork failed: %v", err)
			}

			// Verify the comment includes the correct attempt number
			issueEvents, err := store.GetEvents(ctx, issue.ID, 0)
			if err != nil {
				t.Fatalf("Failed to get events: %v", err)
			}

			// Find the last comment event (not label event)
			var lastComment string
			for i := len(issueEvents) - 1; i >= 0; i-- {
				if issueEvents[i].Comment != nil && issueEvents[i].EventType == types.EventCommented {
					lastComment = *issueEvents[i].Comment
					break
				}
			}

			// Check for retry or escalation message
			if tt.expectedAttemptCount <= 1 {
				// For first attempt, expect retry message with (Attempt #1)
				expectedAttemptString := fmt.Sprintf("(Attempt #%d)", tt.expectedAttemptCount)
				if !strings.Contains(lastComment, expectedAttemptString) {
					t.Errorf("Expected comment to contain %q, got:\n%s", expectedAttemptString, lastComment)
				}
			} else {
				// For second attempt and beyond, expect escalation message with "attempted N times"
				expectedAttemptString := fmt.Sprintf("attempted %d times", tt.expectedAttemptCount)
				if !strings.Contains(lastComment, expectedAttemptString) {
					t.Errorf("Expected comment to contain %q, got:\n%s", expectedAttemptString, lastComment)
				}
			}

			// Verify event data has correct attempt count
			filter := events.EventFilter{
				IssueID: issue.ID,
				Type:    events.EventTypeProgress,
			}
			progressEvents, err := store.GetAgentEvents(ctx, filter)
			if err != nil {
				t.Fatalf("Failed to get agent events: %v", err)
			}

			if len(progressEvents) > 0 {
				event := progressEvents[len(progressEvents)-1]
				// Check incomplete_attempts (may be int or float64 depending on JSON marshaling)
				attemptsValue := event.Data["incomplete_attempts"]
				var attempts int
				switch v := attemptsValue.(type) {
				case int:
					attempts = v
				case float64:
					attempts = int(v)
				case int64:
					attempts = int(v)
				default:
					t.Errorf("Expected incomplete_attempts to be numeric, got type %T value %v", attemptsValue, attemptsValue)
				}
				if attempts != tt.expectedAttemptCount {
					t.Errorf("Expected incomplete_attempts=%d in event data, got %d", tt.expectedAttemptCount, attempts)
				}
			}
		})
	}
}

// TestHandleIncompleteWorkEventEmission verifies that events are emitted correctly (vc-1ows)
func TestHandleIncompleteWorkEventEmission(t *testing.T) {
	tests := []struct {
		name              string
		priorAttempts     int // Number of prior incomplete attempts
		expectedEventData map[string]interface{}
	}{
		{
			name:          "first attempt - retry event",
			priorAttempts: 0,
			expectedEventData: map[string]interface{}{
				"incomplete_attempts": 1,
				"max_retries":         1,
				"escalated":           nil, // Should not be present
			},
		},
		{
			name:          "second attempt - escalation event",
			priorAttempts: 1,
			expectedEventData: map[string]interface{}{
				"incomplete_attempts": 2,
				"max_retries":         1,
				"escalated":           true,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup storage
			cfg := storage.DefaultConfig()
			cfg.Path = t.TempDir() + "/test.db"

			ctx := context.Background()
			store, err := storage.NewStorage(ctx, cfg)
			if err != nil {
				t.Fatalf("Failed to create storage: %v", err)
			}
			defer func() { _ = store.Close() }()

			// Create test issue
			issue := &types.Issue{
				Title:              "Test event emission",
				Description:        "This tests event emission",
				IssueType:          types.TypeTask,
				Status:             types.StatusOpen,
				Priority:           1,
				AcceptanceCriteria: "Must complete",
				CreatedAt:          time.Now(),
				UpdatedAt:          time.Now(),
			}

			if err := store.CreateIssue(ctx, issue, "test"); err != nil {
				t.Fatalf("Failed to create issue: %v", err)
			}

			// Add prior incomplete comments
			for i := 0; i < tt.priorAttempts; i++ {
				comment := fmt.Sprintf("**Incomplete Work Detected (Attempt #%d)**\n\nRetrying...", i+1)
				if err := store.AddComment(ctx, issue.ID, "ai-supervisor", comment); err != nil {
					t.Fatalf("Failed to add comment: %v", err)
				}
			}

			// Register executor instance (required for ClaimIssue foreign key)
			executorID := "test-executor-001"
			instance := &types.ExecutorInstance{
				InstanceID:    executorID,
				Hostname:      "test-host",
				PID:           12345,
				Status:        types.ExecutorStatusRunning,
				StartedAt:     time.Now(),
				LastHeartbeat: time.Now(),
				Version:       "test",
				Metadata:      "{}",
			}
			if err := store.RegisterInstance(ctx, instance); err != nil {
				t.Fatalf("Failed to register executor: %v", err)
			}

			// Claim the issue
			if err := store.ClaimIssue(ctx, issue.ID, executorID); err != nil {
				t.Fatalf("Failed to claim issue: %v", err)
			}

			// Create results processor
			rpCfg := &ResultsProcessorConfig{
				Store:      store,
				WorkingDir: "/tmp/test",
				Actor:      "test-executor",
			}

			rp, err := NewResultsProcessor(rpCfg)
			if err != nil {
				t.Fatalf("Failed to create results processor: %v", err)
			}

			// Call handleIncompleteWork
			analysis := &ai.Analysis{
				Completed: false,
				Summary:   "Test event emission",
			}

			if err := rp.handleIncompleteWork(ctx, issue, analysis); err != nil {
				t.Fatalf("handleIncompleteWork failed: %v", err)
			}

			// Verify event was emitted with correct data
			filter := events.EventFilter{
				IssueID: issue.ID,
				Type:    events.EventTypeProgress,
			}
			progressEvents, err := store.GetAgentEvents(ctx, filter)
			if err != nil {
				t.Fatalf("Failed to get agent events: %v", err)
			}

			if len(progressEvents) == 0 {
				t.Fatal("Expected progress event to be emitted, but none found")
			}

			event := progressEvents[len(progressEvents)-1]

			// Verify expected data fields
			for key, expectedValue := range tt.expectedEventData {
				if expectedValue == nil {
					// Should not be present
					if _, ok := event.Data[key]; ok {
						t.Errorf("Expected %q to NOT be present in event data, but it was: %v", key, event.Data[key])
					}
				} else {
					actualValue, ok := event.Data[key]
					if !ok {
						t.Errorf("Expected %q in event data, but it was not present", key)
					} else {
						// Handle type conversions for numeric values (may be int, int64, or float64)
						if expectedInt, ok := expectedValue.(int); ok {
							switch v := actualValue.(type) {
							case int:
								if v != expectedInt {
									t.Errorf("Expected event data[%q]=%d, got %d", key, expectedInt, v)
								}
							case int64:
								if int(v) != expectedInt {
									t.Errorf("Expected event data[%q]=%d, got %d", key, expectedInt, v)
								}
							case float64:
								if int(v) != expectedInt {
									t.Errorf("Expected event data[%q]=%d, got %f", key, expectedInt, v)
								}
							default:
								t.Errorf("Expected event data[%q]=%d (int), got %v (type %T)", key, expectedInt, actualValue, actualValue)
							}
						} else if actualValue != expectedValue {
							// Non-numeric comparison
							t.Errorf("Expected event data[%q]=%v, got %v", key, expectedValue, actualValue)
						}
					}
				}
			}

			// Verify analysis_summary is always present
			if _, ok := event.Data["analysis_summary"]; !ok {
				t.Error("Expected 'analysis_summary' in event data")
			}
		})
	}
}
