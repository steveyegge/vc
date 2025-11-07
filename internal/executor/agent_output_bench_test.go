package executor

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/steveyegge/vc/internal/types"
)

// BenchmarkOutputCapture benchmarks the current mutex-per-line approach
func BenchmarkOutputCapture(b *testing.B) {
	scenarios := []struct {
		name       string
		linesCount int
		lineSize   int
	}{
		{"small_10_lines", 10, 50},
		{"medium_100_lines", 100, 50},
		{"large_1000_lines", 1000, 50},
		{"verbose_10000_lines", 10000, 50},
	}

	for _, sc := range scenarios {
		b.Run(sc.name, func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				benchmarkOutputCapture(b, sc.linesCount, sc.lineSize)
			}
		})
	}
}

func benchmarkOutputCapture(b *testing.B, lineCount, lineSize int) {
	// Create fake stdout/stderr pipes
	stdoutReader, stdoutWriter := io.Pipe()
	stderrReader, stderrWriter := io.Pipe()

	// Create agent with minimal config
	agent := &Agent{
		config: AgentConfig{
			Type:       AgentTypeAmp,
			WorkingDir: "/tmp/test",
			Issue:      &types.Issue{ID: "vc-test", Title: "Test"},
			StreamJSON: false,
		},
		stdout: stdoutReader,
		stderr: stderrReader,
		result: AgentResult{},
	}

	// Generate test data
	testLine := make([]byte, lineSize)
	for i := range testLine {
		testLine[i] = 'a'
	}

	// Start output capture
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		agent.captureOutput()
	}()

	// Write lines to both stdout and stderr
	wg.Add(2)
	go func() {
		defer wg.Done()
		defer stdoutWriter.Close()
		for i := 0; i < lineCount; i++ {
			fmt.Fprintln(stdoutWriter, string(testLine))
		}
	}()

	go func() {
		defer wg.Done()
		defer stderrWriter.Close()
		for i := 0; i < lineCount/2; i++ {
			fmt.Fprintln(stderrWriter, string(testLine))
		}
	}()

	wg.Wait()
}

// TestMutexContention measures actual mutex contention in output capture
func TestMutexContention(t *testing.T) {
	scenarios := []struct {
		name       string
		linesCount int
		lineSize   int
	}{
		{"low_frequency_100", 100, 50},
		{"medium_frequency_1000", 1000, 50},
		{"high_frequency_10000", 10000, 50},
	}

	for _, sc := range scenarios {
		t.Run(sc.name, func(t *testing.T) {
			stats := measureMutexContention(t, sc.linesCount, sc.lineSize)

			t.Logf("Mutex contention statistics for %s:", sc.name)
			t.Logf("  Total lock acquisitions: %d", stats.LockAcquisitions)
			t.Logf("  Total time in critical section: %v", stats.TotalLockedTime)
			t.Logf("  Average lock hold time: %v", stats.AvgLockHoldTime)
			t.Logf("  Max lock hold time: %v", stats.MaxLockHoldTime)
			t.Logf("  Total test duration: %v", stats.TotalDuration)
			t.Logf("  Lock contention ratio: %.2f%%", stats.ContentionRatio*100)

			// Warn if contention is high
			if stats.ContentionRatio > 0.05 {
				t.Logf("WARNING: Lock contention ratio %.2f%% exceeds 5%% threshold", stats.ContentionRatio*100)
			}
		})
	}
}

type MutexStats struct {
	LockAcquisitions int64
	TotalLockedTime  time.Duration
	AvgLockHoldTime  time.Duration
	MaxLockHoldTime  time.Duration
	TotalDuration    time.Duration
	ContentionRatio  float64
}

func measureMutexContention(t *testing.T, lineCount, lineSize int) MutexStats {
	// Create instrumented agent
	stdoutReader, stdoutWriter := io.Pipe()
	stderrReader, stderrWriter := io.Pipe()

	var lockCount atomic.Int64
	var totalLockTime atomic.Int64
	var maxLockTime atomic.Int64

	agent := &instrumentedAgent{
		Agent: &Agent{
			config: AgentConfig{
				Type:       AgentTypeAmp,
				WorkingDir: "/tmp/test",
				Issue:      &types.Issue{ID: "vc-test", Title: "Test"},
				StreamJSON: false,
			},
			stdout: stdoutReader,
			stderr: stderrReader,
			result: AgentResult{},
		},
		lockCount:     &lockCount,
		totalLockTime: &totalLockTime,
		maxLockTime:   &maxLockTime,
	}

	// Generate test data
	testLine := make([]byte, lineSize)
	for i := range testLine {
		testLine[i] = 'a'
	}

	startTime := time.Now()

	// Start output capture
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		agent.captureOutputInstrumented()
	}()

	// Write lines
	wg.Add(2)
	go func() {
		defer wg.Done()
		defer stdoutWriter.Close()
		for i := 0; i < lineCount; i++ {
			fmt.Fprintln(stdoutWriter, string(testLine))
		}
	}()

	go func() {
		defer wg.Done()
		defer stderrWriter.Close()
		for i := 0; i < lineCount/2; i++ {
			fmt.Fprintln(stderrWriter, string(testLine))
		}
	}()

	wg.Wait()
	totalDuration := time.Since(startTime)

	locks := lockCount.Load()
	totalNs := totalLockTime.Load()
	maxNs := maxLockTime.Load()

	avgLockHold := time.Duration(0)
	if locks > 0 {
		avgLockHold = time.Duration(totalNs / locks)
	}

	return MutexStats{
		LockAcquisitions: locks,
		TotalLockedTime:  time.Duration(totalNs),
		AvgLockHoldTime:  avgLockHold,
		MaxLockHoldTime:  time.Duration(maxNs),
		TotalDuration:    totalDuration,
		ContentionRatio:  float64(totalNs) / float64(totalDuration.Nanoseconds()),
	}
}

// instrumentedAgent wraps Agent to measure mutex performance
type instrumentedAgent struct {
	*Agent
	lockCount     *atomic.Int64
	totalLockTime *atomic.Int64
	maxLockTime   *atomic.Int64
}

func (a *instrumentedAgent) captureOutputInstrumented() {
	var wg sync.WaitGroup
	wg.Add(2)

	// Capture stdout
	go func() {
		defer wg.Done()
		scanner := bufio.NewScanner(a.stdout)
		for scanner.Scan() {
			line := scanner.Text()

			lockStart := time.Now()
			a.mu.Lock()
			a.lockCount.Add(1)

			if len(a.result.Output) < maxOutputLines {
				a.result.Output = append(a.result.Output, line)
			} else if len(a.result.Output) == maxOutputLines {
				a.result.Output = append(a.result.Output, "[... output truncated: limit reached ...]")
			}

			if a.config.StreamJSON && len(a.result.ParsedJSON) < maxOutputLines {
				// Skip JSON parsing in benchmark
			}

			a.mu.Unlock()
			lockDuration := time.Since(lockStart).Nanoseconds()
			a.totalLockTime.Add(lockDuration)

			// Update max lock time
			for {
				currentMax := a.maxLockTime.Load()
				if lockDuration <= currentMax {
					break
				}
				if a.maxLockTime.CompareAndSwap(currentMax, lockDuration) {
					break
				}
			}
		}
	}()

	// Capture stderr
	go func() {
		defer wg.Done()
		scanner := bufio.NewScanner(a.stderr)
		for scanner.Scan() {
			line := scanner.Text()

			lockStart := time.Now()
			a.mu.Lock()
			a.lockCount.Add(1)

			if len(a.result.Errors) < maxOutputLines {
				a.result.Errors = append(a.result.Errors, line)
			} else if len(a.result.Errors) == maxOutputLines {
				a.result.Errors = append(a.result.Errors, "[... error output truncated: limit reached ...]")
			}

			a.mu.Unlock()
			lockDuration := time.Since(lockStart).Nanoseconds()
			a.totalLockTime.Add(lockDuration)

			// Update max lock time
			for {
				currentMax := a.maxLockTime.Load()
				if lockDuration <= currentMax {
					break
				}
				if a.maxLockTime.CompareAndSwap(currentMax, lockDuration) {
					break
				}
			}
		}
	}()

	wg.Wait()
}

// TestOutputOrdering verifies that output ordering is maintained under high frequency
func TestOutputOrdering(t *testing.T) {
	stdoutReader, stdoutWriter := io.Pipe()
	stderrReader, stderrWriter := io.Pipe()

	agent := &Agent{
		config: AgentConfig{
			Type:       AgentTypeAmp,
			WorkingDir: "/tmp/test",
			Issue:      &types.Issue{ID: "vc-test", Title: "Test"},
			StreamJSON: false,
		},
		stdout: stdoutReader,
		stderr: stderrReader,
		result: AgentResult{},
	}

	lineCount := 1000
	var wg sync.WaitGroup

	// Start capture
	wg.Add(1)
	go func() {
		defer wg.Done()
		agent.captureOutput()
	}()

	// Write numbered lines to stdout
	wg.Add(1)
	go func() {
		defer wg.Done()
		defer stdoutWriter.Close()
		for i := 0; i < lineCount; i++ {
			fmt.Fprintf(stdoutWriter, "stdout-%d\n", i)
		}
	}()

	// Write numbered lines to stderr
	wg.Add(1)
	go func() {
		defer wg.Done()
		defer stderrWriter.Close()
		for i := 0; i < lineCount/2; i++ {
			fmt.Fprintf(stderrWriter, "stderr-%d\n", i)
		}
	}()

	wg.Wait()

	// Verify stdout ordering
	for i := 0; i < lineCount && i < len(agent.result.Output); i++ {
		expected := fmt.Sprintf("stdout-%d", i)
		if agent.result.Output[i] != expected {
			t.Errorf("stdout ordering broken at index %d: expected %s, got %s", i, expected, agent.result.Output[i])
			break
		}
	}

	// Verify stderr ordering
	for i := 0; i < lineCount/2 && i < len(agent.result.Errors); i++ {
		expected := fmt.Sprintf("stderr-%d", i)
		if agent.result.Errors[i] != expected {
			t.Errorf("stderr ordering broken at index %d: expected %s, got %s", i, expected, agent.result.Errors[i])
			break
		}
	}

	t.Logf("Successfully verified ordering for %d stdout lines and %d stderr lines",
		len(agent.result.Output), len(agent.result.Errors))
}

// BenchmarkBatchedOutput benchmarks a hypothetical batched approach
func BenchmarkBatchedOutput(b *testing.B) {
	scenarios := []struct {
		name       string
		linesCount int
		lineSize   int
		batchSize  int
	}{
		{"batch_10_lines_1000", 1000, 50, 10},
		{"batch_50_lines_1000", 1000, 50, 50},
		{"batch_100_lines_1000", 1000, 50, 100},
		{"batch_10_lines_10000", 10000, 50, 10},
		{"batch_50_lines_10000", 10000, 50, 50},
		{"batch_100_lines_10000", 10000, 50, 100},
	}

	for _, sc := range scenarios {
		b.Run(sc.name, func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				benchmarkBatchedOutput(b, sc.linesCount, sc.lineSize, sc.batchSize)
			}
		})
	}
}

func benchmarkBatchedOutput(b *testing.B, lineCount, lineSize, batchSize int) {
	var mu sync.Mutex
	var output []string
	var errors []string

	// Generate test data
	var buf bytes.Buffer
	testLine := make([]byte, lineSize)
	for i := range testLine {
		testLine[i] = 'a'
	}
	for i := 0; i < lineCount; i++ {
		buf.WriteString(string(testLine))
		buf.WriteByte('\n')
	}

	stdoutData := buf.Bytes()
	stderrData := buf.Bytes()[:len(buf.Bytes())/2]

	var wg sync.WaitGroup
	wg.Add(2)

	// Batched stdout capture
	go func() {
		defer wg.Done()
		scanner := bufio.NewScanner(bytes.NewReader(stdoutData))
		batch := make([]string, 0, batchSize)

		for scanner.Scan() {
			batch = append(batch, scanner.Text())

			if len(batch) >= batchSize {
				mu.Lock()
				output = append(output, batch...)
				mu.Unlock()
				batch = batch[:0]
			}
		}

		// Flush remaining
		if len(batch) > 0 {
			mu.Lock()
			output = append(output, batch...)
			mu.Unlock()
		}
	}()

	// Batched stderr capture
	go func() {
		defer wg.Done()
		scanner := bufio.NewScanner(bytes.NewReader(stderrData))
		batch := make([]string, 0, batchSize)

		for scanner.Scan() {
			batch = append(batch, scanner.Text())

			if len(batch) >= batchSize {
				mu.Lock()
				errors = append(errors, batch...)
				mu.Unlock()
				batch = batch[:0]
			}
		}

		// Flush remaining
		if len(batch) > 0 {
			mu.Lock()
			errors = append(errors, batch...)
			mu.Unlock()
		}
	}()

	wg.Wait()
}
