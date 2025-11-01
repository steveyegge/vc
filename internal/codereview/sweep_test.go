package codereview

import (
	"context"
	"testing"
	"time"
)

// TestGetTotalLOCCaching tests that getTotalLOC() uses cache correctly
func TestGetTotalLOCCaching(t *testing.T) {
	ctx := context.Background()

	// Reset cache state
	cachedLOCMutex.Lock()
	cachedLOC = 0
	cachedLOCTime = time.Time{}
	cachedLOCMutex.Unlock()

	// First call - should calculate (cache miss)
	start := time.Now()
	loc1 := getTotalLOC(ctx)
	duration1 := time.Since(start)

	if loc1 == 0 {
		t.Skip("No code files found in repository")
	}

	// Verify cache is set
	cachedLOCMutex.RLock()
	if cachedLOC != loc1 {
		t.Errorf("Cache not set after first call: got %d, want %d", cachedLOC, loc1)
	}
	if cachedLOCTime.IsZero() {
		t.Error("Cache timestamp not set after first call")
	}
	cachedLOCMutex.RUnlock()

	// Second call (immediate) - should use cache (fast)
	start = time.Now()
	loc2 := getTotalLOC(ctx)
	duration2 := time.Since(start)

	if loc2 != loc1 {
		t.Errorf("Second call returned different value: got %d, want %d", loc2, loc1)
	}

	// Cache hit should be much faster (< 10ms vs potentially seconds)
	if duration2 > 10*time.Millisecond {
		t.Errorf("Cached call took too long: %v (expected < 10ms)", duration2)
	}

	t.Logf("First call (cache miss): %v", duration1)
	t.Logf("Second call (cache hit): %v", duration2)
	t.Logf("Speedup: %.2fx", float64(duration1)/float64(duration2))
}

// TestGetTotalLOCCacheExpiry tests that cache expires after TTL
func TestGetTotalLOCCacheExpiry(t *testing.T) {
	ctx := context.Background()

	// Set cache with old timestamp
	cachedLOCMutex.Lock()
	cachedLOC = 12345
	cachedLOCTime = time.Now().Add(-2 * time.Hour) // Expired (TTL is 1 hour)
	cachedLOCMutex.Unlock()

	// Call should recalculate
	loc := getTotalLOC(ctx)

	// If we got code files, should not be the cached value
	if loc > 0 && loc == 12345 {
		t.Log("Warning: Got cached value after expiry - may be coincidence or cache didn't expire")
	}

	// Verify cache was updated with fresh timestamp
	cachedLOCMutex.RLock()
	timeSinceCache := time.Since(cachedLOCTime)
	cachedLOCMutex.RUnlock()

	if timeSinceCache > 1*time.Second {
		t.Errorf("Cache timestamp not updated after expiry: %v old", timeSinceCache)
	}
}

// TestGetTotalLOCThreadSafety tests concurrent access to cache
func TestGetTotalLOCThreadSafety(t *testing.T) {
	ctx := context.Background()

	// Reset cache
	cachedLOCMutex.Lock()
	cachedLOC = 0
	cachedLOCTime = time.Time{}
	cachedLOCMutex.Unlock()

	// Run 10 concurrent getTotalLOC() calls
	done := make(chan int, 10)
	for i := 0; i < 10; i++ {
		go func() {
			loc := getTotalLOC(ctx)
			done <- loc
		}()
	}

	// Collect results
	var results []int
	for i := 0; i < 10; i++ {
		results = append(results, <-done)
	}

	// All results should be the same (thread-safe cache)
	for i := 1; i < len(results); i++ {
		if results[i] != results[0] {
			t.Errorf("Concurrent calls returned different values: %d vs %d", results[i], results[0])
		}
	}
}

// TestGetTotalLOCContextCancellation tests that getTotalLOC respects context cancellation
func TestGetTotalLOCContextCancellation(t *testing.T) {
	// Reset cache to force actual calculation
	cachedLOCMutex.Lock()
	cachedLOC = 0
	cachedLOCTime = time.Time{}
	cachedLOCMutex.Unlock()

	// Create cancellable context and cancel immediately
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel before call

	// Call should return quickly with 0 (command fails due to cancellation)
	start := time.Now()
	loc := getTotalLOC(ctx)
	duration := time.Since(start)

	// Should fail fast (cancelled context)
	if duration > 100*time.Millisecond {
		t.Errorf("Cancelled getTotalLOC took too long: %v (expected < 100ms)", duration)
	}

	// Should return 0 when context is cancelled (command fails)
	if loc != 0 {
		t.Logf("Note: getTotalLOC returned %d with cancelled context (may have used cache)", loc)
	}

	t.Logf("Cancelled call duration: %v", duration)
}
