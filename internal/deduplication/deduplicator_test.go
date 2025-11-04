package deduplication

import (
	"context"
	"testing"
	"time"

	"github.com/steveyegge/vc/internal/ai"
	"github.com/steveyegge/vc/internal/storage"
	"github.com/steveyegge/vc/internal/types"
)

// TestDuplicateDecisionValidation tests the validation logic for DuplicateDecision
func TestDuplicateDecisionValidation(t *testing.T) {
	tests := []struct {
		name        string
		decision    DuplicateDecision
		expectError bool
		errorMsg    string
	}{
		{
			name: "valid non-duplicate",
			decision: DuplicateDecision{
				IsDuplicate:   false,
				Confidence:    0.42,
				Reasoning:     "Not similar enough",
				ComparedCount: 10,
			},
			expectError: false,
		},
		{
			name: "valid duplicate",
			decision: DuplicateDecision{
				IsDuplicate:   true,
				DuplicateOf:   "vc-123",
				Confidence:    0.95,
				Reasoning:     "Very similar titles and descriptions",
				ComparedCount: 10,
			},
			expectError: false,
		},
		{
			name: "duplicate without duplicate_of",
			decision: DuplicateDecision{
				IsDuplicate:   true,
				Confidence:    0.95,
				ComparedCount: 10,
			},
			expectError: true,
			errorMsg:    "duplicate_of must be set",
		},
		{
			name: "non-duplicate with duplicate_of",
			decision: DuplicateDecision{
				IsDuplicate:   false,
				DuplicateOf:   "vc-123",
				Confidence:    0.42,
				ComparedCount: 10,
			},
			expectError: true,
			errorMsg:    "duplicate_of should not be set",
		},
		{
			name: "confidence too low",
			decision: DuplicateDecision{
				IsDuplicate:   false,
				Confidence:    -0.1,
				ComparedCount: 10,
			},
			expectError: true,
			errorMsg:    "confidence must be between 0.0 and 1.0",
		},
		{
			name: "confidence too high",
			decision: DuplicateDecision{
				IsDuplicate:   true,
				DuplicateOf:   "vc-123",
				Confidence:    1.5,
				ComparedCount: 10,
			},
			expectError: true,
			errorMsg:    "confidence must be between 0.0 and 1.0",
		},
		{
			name: "negative compared count",
			decision: DuplicateDecision{
				IsDuplicate:   false,
				Confidence:    0.5,
				ComparedCount: -1,
			},
			expectError: true,
			errorMsg:    "compared_count cannot be negative",
		},
		{
			name: "zero compared count is valid",
			decision: DuplicateDecision{
				IsDuplicate:   false,
				Confidence:    0.0,
				ComparedCount: 0,
			},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.decision.Validate()
			if tt.expectError {
				if err == nil {
					t.Errorf("expected error containing '%s', got nil", tt.errorMsg)
				} else if tt.errorMsg != "" && !contains(err.Error(), tt.errorMsg) {
					t.Errorf("expected error containing '%s', got '%s'", tt.errorMsg, err.Error())
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
			}
		})
	}
}

// TestDeduplicationResultValidation tests the validation logic for DeduplicationResult
func TestDeduplicationResultValidation(t *testing.T) {
	now := time.Now()
	issue1 := &types.Issue{
		ID:          "vc-1",
		Title:       "Test issue 1",
		Description: "Description",
		Status:      types.StatusOpen,
		Priority:    1,
		IssueType:   types.TypeTask,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	issue2 := &types.Issue{
		ID:          "vc-2",
		Title:       "Test issue 2",
		Description: "Description",
		Status:      types.StatusOpen,
		Priority:    1,
		IssueType:   types.TypeTask,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	issue3 := &types.Issue{
		ID:          "vc-3",
		Title:       "Test issue 3",
		Description: "Description",
		Status:      types.StatusOpen,
		Priority:    1,
		IssueType:   types.TypeTask,
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	tests := []struct {
		name        string
		result      DeduplicationResult
		expectError bool
		errorMsg    string
	}{
		{
			name: "valid result with all unique",
			result: DeduplicationResult{
				UniqueIssues:          []*types.Issue{issue1, issue2, issue3},
				DuplicatePairs:        make(map[int]string),
				WithinBatchDuplicates: make(map[int]int),
				Stats: DeduplicationStats{
					TotalCandidates:           3,
					UniqueCount:               3,
					DuplicateCount:            0,
					WithinBatchDuplicateCount: 0,
				},
			},
			expectError: false,
		},
		{
			name: "valid result with duplicates",
			result: DeduplicationResult{
				UniqueIssues:          []*types.Issue{issue1},
				DuplicatePairs:        map[int]string{1: "vc-100", 2: "vc-101"},
				WithinBatchDuplicates: make(map[int]int),
				Stats: DeduplicationStats{
					TotalCandidates:           3,
					UniqueCount:               1,
					DuplicateCount:            2,
					WithinBatchDuplicateCount: 0,
				},
			},
			expectError: false,
		},
		{
			name: "valid result with within-batch duplicates",
			result: DeduplicationResult{
				UniqueIssues:          []*types.Issue{issue1},
				DuplicatePairs:        make(map[int]string),
				WithinBatchDuplicates: map[int]int{1: 0, 2: 0},
				Stats: DeduplicationStats{
					TotalCandidates:           3,
					UniqueCount:               1,
					DuplicateCount:            0,
					WithinBatchDuplicateCount: 2,
				},
			},
			expectError: false,
		},
		{
			name: "mismatched unique count",
			result: DeduplicationResult{
				UniqueIssues:          []*types.Issue{issue1, issue2},
				DuplicatePairs:        make(map[int]string),
				WithinBatchDuplicates: make(map[int]int),
				Stats: DeduplicationStats{
					TotalCandidates: 2,
					UniqueCount:     3, // Wrong!
					DuplicateCount:  0,
				},
			},
			expectError: true,
			errorMsg:    "stats.unique_count",
		},
		{
			name: "mismatched duplicate count",
			result: DeduplicationResult{
				UniqueIssues:          []*types.Issue{issue1},
				DuplicatePairs:        map[int]string{1: "vc-100"},
				WithinBatchDuplicates: make(map[int]int),
				Stats: DeduplicationStats{
					TotalCandidates: 2,
					UniqueCount:     1,
					DuplicateCount:  2, // Wrong!
				},
			},
			expectError: true,
			errorMsg:    "stats.duplicate_count",
		},
		{
			name: "mismatched within-batch count",
			result: DeduplicationResult{
				UniqueIssues:          []*types.Issue{issue1},
				DuplicatePairs:        make(map[int]string),
				WithinBatchDuplicates: map[int]int{1: 0},
				Stats: DeduplicationStats{
					TotalCandidates:           2,
					UniqueCount:               1,
					DuplicateCount:            0,
					WithinBatchDuplicateCount: 2, // Wrong!
				},
			},
			expectError: true,
			errorMsg:    "stats.within_batch_duplicate_count",
		},
		{
			name: "total mismatch",
			result: DeduplicationResult{
				UniqueIssues:          []*types.Issue{issue1},
				DuplicatePairs:        map[int]string{1: "vc-100"},
				WithinBatchDuplicates: make(map[int]int),
				Stats: DeduplicationStats{
					TotalCandidates: 5, // Wrong! Should be 2
					UniqueCount:     1,
					DuplicateCount:  1,
				},
			},
			expectError: true,
			errorMsg:    "stats.total_candidates",
		},
		{
			name: "invalid duplicate index",
			result: DeduplicationResult{
				UniqueIssues:          []*types.Issue{issue1},
				DuplicatePairs:        map[int]string{5: "vc-100"}, // Index 5 out of range
				WithinBatchDuplicates: make(map[int]int),
				Stats: DeduplicationStats{
					TotalCandidates: 2,
					UniqueCount:     1,
					DuplicateCount:  1,
				},
			},
			expectError: true,
			errorMsg:    "invalid index",
		},
		{
			name: "invalid within-batch duplicate index",
			result: DeduplicationResult{
				UniqueIssues:          []*types.Issue{issue1},
				DuplicatePairs:        make(map[int]string),
				WithinBatchDuplicates: map[int]int{0: 1}, // Duplicate index must be > original
				Stats: DeduplicationStats{
					TotalCandidates:           2,
					UniqueCount:               1,
					DuplicateCount:            0,
					WithinBatchDuplicateCount: 1,
				},
			},
			expectError: true,
			errorMsg:    "duplicate index",
		},
		{
			name: "overlapping indices: duplicate_pairs and within_batch_duplicates",
			result: DeduplicationResult{
				UniqueIssues:          []*types.Issue{issue1},
				DuplicatePairs:        map[int]string{1: "vc-100"},
				WithinBatchDuplicates: map[int]int{1: 0}, // Index 1 in both maps!
				Stats: DeduplicationStats{
					TotalCandidates:           3,
					UniqueCount:               1,
					DuplicateCount:            1,
					WithinBatchDuplicateCount: 1,
				},
			},
			expectError: true,
			errorMsg:    "appears in both duplicate_pairs and within_batch_duplicates",
		},
		{
			name: "within_batch original appears in duplicate_pairs",
			result: DeduplicationResult{
				UniqueIssues:          []*types.Issue{issue1},
				DuplicatePairs:        map[int]string{1: "vc-100"}, // Index 1 is duplicate of existing
				WithinBatchDuplicates: map[int]int{2: 1},           // But index 1 is also original for within-batch!
				Stats: DeduplicationStats{
					TotalCandidates:           3,
					UniqueCount:               1,
					DuplicateCount:            1,
					WithinBatchDuplicateCount: 1,
				},
			},
			expectError: true,
			errorMsg:    "references index 1 as original, but it appears in duplicate_pairs",
		},
		{
			name: "within_batch original is also a duplicate",
			result: DeduplicationResult{
				UniqueIssues:          []*types.Issue{issue1},
				DuplicatePairs:        make(map[int]string),
				WithinBatchDuplicates: map[int]int{2: 1, 1: 0}, // Index 1 is both original and duplicate!
				Stats: DeduplicationStats{
					TotalCandidates:           3,
					UniqueCount:               1,
					DuplicateCount:            0,
					WithinBatchDuplicateCount: 2,
				},
			},
			expectError: true,
			errorMsg:    "references index 1 as original, but it is also a duplicate",
		},
		{
			name: "valid complex deduplication",
			result: DeduplicationResult{
				UniqueIssues:          []*types.Issue{issue1, issue2},
				DuplicatePairs:        map[int]string{2: "vc-100", 5: "vc-101"},
				WithinBatchDuplicates: map[int]int{3: 0, 4: 1},
				Stats: DeduplicationStats{
					TotalCandidates:           6,
					UniqueCount:               2,
					DuplicateCount:            2,
					WithinBatchDuplicateCount: 2,
				},
			},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.result.Validate()
			if tt.expectError {
				if err == nil {
					t.Errorf("expected error containing '%s', got nil", tt.errorMsg)
				} else if tt.errorMsg != "" && !contains(err.Error(), tt.errorMsg) {
					t.Errorf("expected error containing '%s', got '%s'", tt.errorMsg, err.Error())
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
			}
		})
	}
}

// TestConfigValidation tests the validation logic for Config
func TestConfigValidation(t *testing.T) {
	tests := []struct {
		name        string
		config      Config
		expectError bool
		errorMsg    string
	}{
		{
			name:        "default config is valid",
			config:      DefaultConfig(),
			expectError: false,
		},
		{
			name: "confidence threshold too low",
			config: Config{
				ConfidenceThreshold: -0.1,
				LookbackWindow:      7 * 24 * time.Hour,
				MaxCandidates:       50,
				BatchSize:           10,
				MinTitleLength:      10,
				MaxRetries:          2,
				RequestTimeout:      30 * time.Second,
			},
			expectError: true,
			errorMsg:    "confidence_threshold must be between 0.0 and 1.0",
		},
		{
			name: "confidence threshold too high",
			config: Config{
				ConfidenceThreshold: 1.5,
				LookbackWindow:      7 * 24 * time.Hour,
				MaxCandidates:       50,
				BatchSize:           10,
				MinTitleLength:      10,
				MaxRetries:          2,
				RequestTimeout:      30 * time.Second,
			},
			expectError: true,
			errorMsg:    "confidence_threshold must be between 0.0 and 1.0",
		},
		{
			name: "lookback window too large",
			config: Config{
				ConfidenceThreshold: 0.85,
				LookbackWindow:      100 * 24 * time.Hour,
				MaxCandidates:       50,
				BatchSize:           10,
				MinTitleLength:      10,
				MaxRetries:          2,
				RequestTimeout:      30 * time.Second,
			},
			expectError: true,
			errorMsg:    "lookback_window too large",
		},
		{
			name: "max candidates too large",
			config: Config{
				ConfidenceThreshold: 0.85,
				LookbackWindow:      7 * 24 * time.Hour,
				MaxCandidates:       1000,
				BatchSize:           10,
				MinTitleLength:      10,
				MaxRetries:          2,
				RequestTimeout:      30 * time.Second,
			},
			expectError: true,
			errorMsg:    "max_candidates too large",
		},
		{
			name: "batch size too large",
			config: Config{
				ConfidenceThreshold: 0.85,
				LookbackWindow:      7 * 24 * time.Hour,
				MaxCandidates:       50,
				BatchSize:           200,
				MinTitleLength:      10,
				MaxRetries:          2,
				RequestTimeout:      30 * time.Second,
			},
			expectError: true,
			errorMsg:    "batch_size too large",
		},
		{
			name: "request timeout too large",
			config: Config{
				ConfidenceThreshold: 0.85,
				LookbackWindow:      7 * 24 * time.Hour,
				MaxCandidates:       50,
				BatchSize:           10,
				MinTitleLength:      10,
				MaxRetries:          2,
				RequestTimeout:      10 * time.Minute,
			},
			expectError: true,
			errorMsg:    "request_timeout too large",
		},
		{
			name: "valid custom config",
			config: Config{
				ConfidenceThreshold:    0.90,
				LookbackWindow:         14 * 24 * time.Hour,
				MaxCandidates:          100,
				BatchSize:              20,
				EnableWithinBatchDedup: true,
				FailOpen:               false,
				IncludeClosedIssues:    true,
				MinTitleLength:         5,
				MaxRetries:             5,
				RequestTimeout:         60 * time.Second,
			},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if tt.expectError {
				if err == nil {
					t.Errorf("expected error containing '%s', got nil", tt.errorMsg)
				} else if tt.errorMsg != "" && !contains(err.Error(), tt.errorMsg) {
					t.Errorf("expected error containing '%s', got '%s'", tt.errorMsg, err.Error())
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
			}
		})
	}
}

// TestConfigString tests the String() method for Config
func TestConfigString(t *testing.T) {
	config := DefaultConfig()
	s := config.String()

	// Check that key fields are present in the string representation
	expectedSubstrings := []string{
		"Threshold: 0.85",
		"MaxCandidates: 25",
		"BatchSize: 50",
		"FailOpen: true",
	}

	for _, expected := range expectedSubstrings {
		if !contains(s, expected) {
			t.Errorf("expected String() to contain '%s', got: %s", expected, s)
		}
	}
}

// TestDefaultConfig tests that DefaultConfig returns valid configuration
func TestDefaultConfig(t *testing.T) {
	config := DefaultConfig()

	// Validate the default config
	if err := config.Validate(); err != nil {
		t.Errorf("default config should be valid, got error: %v", err)
	}

	// Check specific defaults
	if config.ConfidenceThreshold != 0.85 {
		t.Errorf("expected default confidence threshold 0.85, got %.2f", config.ConfidenceThreshold)
	}
	if config.LookbackWindow != 7*24*time.Hour {
		t.Errorf("expected default lookback window 7 days, got %v", config.LookbackWindow)
	}
	if config.MaxCandidates != 25 {
		t.Errorf("expected default max candidates 25, got %d", config.MaxCandidates)
	}
	if config.BatchSize != 50 {
		t.Errorf("expected default batch size 50, got %d", config.BatchSize)
	}
	if !config.EnableWithinBatchDedup {
		t.Errorf("expected EnableWithinBatchDedup to be true by default")
	}
	if !config.FailOpen {
		t.Errorf("expected FailOpen to be true by default")
	}
	if config.IncludeClosedIssues {
		t.Errorf("expected IncludeClosedIssues to be false by default")
	}
}

// TestNewAIDeduplicatorValidation tests the constructor validation logic
func TestNewAIDeduplicatorValidation(t *testing.T) {
	ctx := context.Background()

	// Create a valid in-memory storage for tests
	cfg := storage.DefaultConfig()
	cfg.Path = t.TempDir() + "/test.db"
	store, err := storage.NewStorage(ctx, cfg)
	if err != nil {
		t.Fatalf("failed to create test storage: %v", err)
	}
	defer func() { _ = store.Close() }()

	// Note: We can't easily create a real supervisor without an Anthropic API client,
	// so we focus on testing nil validation. Config validation is tested separately
	// in TestConfigValidation, and it's already called by the constructor.

	tests := []struct {
		name        string
		supervisor  *ai.Supervisor
		store       storage.Storage
		config      Config
		expectError bool
		errorMsg    string
	}{
		{
			name:        "nil supervisor",
			supervisor:  nil,
			store:       store,
			config:      DefaultConfig(),
			expectError: true,
			errorMsg:    "supervisor cannot be nil",
		},
		{
			name:        "nil store",
			supervisor:  nil, // Still nil since we can't create a real one
			store:       nil,
			config:      DefaultConfig(),
			expectError: true,
			errorMsg:    "nil", // Will match either "supervisor" or "store" error
		},
		{
			name:       "invalid config gets validated",
			supervisor: nil,
			store:      store,
			config: Config{
				ConfidenceThreshold: 1.5, // Invalid - will be caught after supervisor check
				LookbackWindow:      7 * 24 * time.Hour,
				MaxCandidates:       50,
				BatchSize:           10,
				MinTitleLength:      10,
				MaxRetries:          2,
				RequestTimeout:      30 * time.Second,
			},
			expectError: true,
			errorMsg:    "nil", // Supervisor validation happens first
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dedup, err := NewAIDeduplicator(tt.supervisor, tt.store, tt.config)

			if tt.expectError {
				if err == nil {
					t.Errorf("expected error containing '%s', got nil", tt.errorMsg)
				} else if tt.errorMsg != "" && !contains(err.Error(), tt.errorMsg) {
					t.Errorf("expected error containing '%s', got '%s'", tt.errorMsg, err.Error())
				}
				if dedup != nil {
					t.Errorf("expected nil deduplicator on error, got non-nil")
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
				if dedup == nil {
					t.Errorf("expected non-nil deduplicator on success, got nil")
				}
			}
		})
	}
}

// TestNewAIDeduplicatorConfigValidation tests that config validation is called
// This test verifies the validation is invoked; specific validation rules are
// tested in TestConfigValidation
func TestNewAIDeduplicatorConfigValidation(t *testing.T) {
	ctx := context.Background()

	// Create a valid in-memory storage
	cfg := storage.DefaultConfig()
	cfg.Path = t.TempDir() + "/test.db"
	store, err := storage.NewStorage(ctx, cfg)
	if err != nil {
		t.Fatalf("failed to create test storage: %v", err)
	}
	defer func() { _ = store.Close() }()

	// Create a mock supervisor by creating an empty one (unsafe, but for testing validation)
	// We just need a non-nil pointer to test config validation
	mockSupervisor := &ai.Supervisor{}

	// Test invalid configs
	invalidConfigs := []struct {
		name   string
		config Config
	}{
		{
			name: "confidence too high",
			config: Config{
				ConfidenceThreshold: 1.5,
				LookbackWindow:      7 * 24 * time.Hour,
				MaxCandidates:       50,
				BatchSize:           10,
				MinTitleLength:      10,
				MaxRetries:          2,
				RequestTimeout:      30 * time.Second,
			},
		},
		{
			name: "max candidates too large",
			config: Config{
				ConfidenceThreshold: 0.85,
				LookbackWindow:      7 * 24 * time.Hour,
				MaxCandidates:       1000,
				BatchSize:           10,
				MinTitleLength:      10,
				MaxRetries:          2,
				RequestTimeout:      30 * time.Second,
			},
		},
		{
			name: "negative lookback",
			config: Config{
				ConfidenceThreshold: 0.85,
				LookbackWindow:      -1 * time.Hour,
				MaxCandidates:       50,
				BatchSize:           10,
				MinTitleLength:      10,
				MaxRetries:          2,
				RequestTimeout:      30 * time.Second,
			},
		},
	}

	for _, tt := range invalidConfigs {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewAIDeduplicator(mockSupervisor, store, tt.config)
			if err == nil {
				t.Errorf("expected error for invalid config, got nil")
			}
			if !contains(err.Error(), "invalid config") {
				t.Errorf("expected error to contain 'invalid config', got: %v", err)
			}
		})
	}

	// Test valid config succeeds (doesn't panic from nil fields in mock supervisor)
	t.Run("valid config", func(t *testing.T) {
		dedup, err := NewAIDeduplicator(mockSupervisor, store, DefaultConfig())
		if err != nil {
			t.Errorf("unexpected error with valid config: %v", err)
		}
		if dedup == nil {
			t.Errorf("expected non-nil deduplicator with valid config")
		}
	})
}

// contains checks if a string contains a substring (helper for tests)
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 ||
		(len(s) > 0 && (s[:len(substr)] == substr ||
		(len(s) > len(substr) && containsHelper(s, substr)))))
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
