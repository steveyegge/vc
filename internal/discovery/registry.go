package discovery

import (
	"fmt"
	"sync"
	"time"

	"github.com/steveyegge/vc/internal/health"
)

// WorkerRegistry manages discovery worker registration and execution order.
// Unlike health monitor registry, this is stateless since discovery is one-time.
type WorkerRegistry struct {
	mu      sync.RWMutex
	workers map[string]DiscoveryWorker
}

// NewWorkerRegistry creates a new worker registry with default workers.
func NewWorkerRegistry() *WorkerRegistry {
	return &WorkerRegistry{
		workers: make(map[string]DiscoveryWorker),
	}
}

// Register adds a discovery worker to the registry.
func (r *WorkerRegistry) Register(worker DiscoveryWorker) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	name := worker.Name()
	if _, exists := r.workers[name]; exists {
		return fmt.Errorf("worker %q already registered", name)
	}

	r.workers[name] = worker
	return nil
}

// RegisterHealthMonitor registers a health monitor as a discovery worker.
// This allows reusing existing health monitors during discovery.
func (r *WorkerRegistry) RegisterHealthMonitor(monitor health.HealthMonitor) error {
	adapter := NewWorkerAdapter(monitor)
	return r.Register(adapter)
}

// Get returns a registered worker by name.
func (r *WorkerRegistry) Get(name string) (DiscoveryWorker, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	worker, exists := r.workers[name]
	return worker, exists
}

// List returns all registered worker names.
func (r *WorkerRegistry) List() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	names := make([]string, 0, len(r.workers))
	for name := range r.workers {
		names = append(names, name)
	}
	return names
}

// ResolveWorkers resolves a list of worker names to workers,
// sorted in topological order based on dependencies.
//
// For example, if "bugs" depends on "architecture", this returns
// ["architecture", "bugs"] so architecture runs first.
func (r *WorkerRegistry) ResolveWorkers(names []string) ([]DiscoveryWorker, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	// Validate all workers exist
	for _, name := range names {
		if _, exists := r.workers[name]; !exists {
			return nil, fmt.Errorf("worker %q not registered", name)
		}
	}

	// Build dependency graph
	graph := make(map[string][]string)
	inDegree := make(map[string]int)

	// Initialize graph nodes
	for _, name := range names {
		graph[name] = []string{}
		inDegree[name] = 0
	}

	// Build edges (dependencies)
	for _, name := range names {
		worker := r.workers[name]
		deps := worker.Dependencies()

		for _, dep := range deps {
			// Only include dependency if it's in the requested workers
			if contains(names, dep) {
				graph[dep] = append(graph[dep], name)
				inDegree[name]++
			}
		}
	}

	// Topological sort using Kahn's algorithm
	sorted := []string{}
	queue := []string{}

	// Find all nodes with no incoming edges
	for name, degree := range inDegree {
		if degree == 0 {
			queue = append(queue, name)
		}
	}

	for len(queue) > 0 {
		// Dequeue
		current := queue[0]
		queue = queue[1:]
		sorted = append(sorted, current)

		// Reduce in-degree for neighbors
		for _, neighbor := range graph[current] {
			inDegree[neighbor]--
			if inDegree[neighbor] == 0 {
				queue = append(queue, neighbor)
			}
		}
	}

	// Check for cycles
	if len(sorted) != len(names) {
		return nil, fmt.Errorf("circular dependency detected in workers")
	}

	// Convert names to workers
	workers := make([]DiscoveryWorker, len(sorted))
	for i, name := range sorted {
		workers[i] = r.workers[name]
	}

	return workers, nil
}

// GetPresetWorkers returns workers for a given preset,
// sorted in topological order.
func (r *WorkerRegistry) GetPresetWorkers(preset Preset) ([]DiscoveryWorker, error) {
	cfg := PresetConfig(preset)
	if len(cfg.Workers) == 0 {
		return nil, fmt.Errorf("preset %q has no workers defined", preset)
	}

	return r.ResolveWorkers(cfg.Workers)
}

// ValidateWorker checks if a worker's dependencies are registered.
func (r *WorkerRegistry) ValidateWorker(name string) error {
	r.mu.RLock()
	defer r.mu.RUnlock()

	worker, exists := r.workers[name]
	if !exists {
		return fmt.Errorf("worker %q not registered", name)
	}

	deps := worker.Dependencies()
	for _, dep := range deps {
		if _, exists := r.workers[dep]; !exists {
			return fmt.Errorf("worker %q depends on unregistered worker %q", name, dep)
		}
	}

	return nil
}

// GetWorkerCost returns the total estimated cost for a set of workers.
func (r *WorkerRegistry) GetWorkerCost(names []string) (health.CostEstimate, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	totalDuration := time.Duration(0)
	totalAICalls := 0
	requiresFullScan := false
	maxCategory := health.CostCheap

	for _, name := range names {
		worker, exists := r.workers[name]
		if !exists {
			return health.CostEstimate{}, fmt.Errorf("worker %q not registered", name)
		}

		cost := worker.Cost()
		totalDuration += cost.EstimatedDuration
		totalAICalls += cost.AICallsEstimated
		requiresFullScan = requiresFullScan || cost.RequiresFullScan

		// Track most expensive category
		if cost.Category == health.CostExpensive {
			maxCategory = health.CostExpensive
		} else if cost.Category == health.CostModerate && maxCategory != health.CostExpensive {
			maxCategory = health.CostModerate
		}
	}

	return health.CostEstimate{
		EstimatedDuration: totalDuration,
		AICallsEstimated:  totalAICalls,
		RequiresFullScan:  requiresFullScan,
		Category:          maxCategory,
	}, nil
}

// contains checks if a string is in a slice.
func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}

// DefaultRegistry creates a registry with all built-in workers registered.
// This includes health monitor adapters for existing monitors.
func DefaultRegistry(healthRegistry *health.MonitorRegistry) (*WorkerRegistry, error) {
	registry := NewWorkerRegistry()

	// Register health monitors as discovery workers
	// These can run during discovery to find issues
	if healthRegistry != nil {
		for _, monitorName := range healthRegistry.ListMonitors() {
			monitor, exists := healthRegistry.GetMonitor(monitorName)
			if exists {
				if err := registry.RegisterHealthMonitor(monitor); err != nil {
					return nil, fmt.Errorf("registering health monitor %q: %w", monitorName, err)
				}
			}
		}
	}

	// Register custom discovery workers
	// These workers provide deep analysis beyond health monitors

	// Architecture worker - package structure analysis
	archWorker := NewArchitectureWorker()
	if err := registry.Register(archWorker); err != nil {
		return nil, fmt.Errorf("registering architecture worker: %w", err)
	}

	// BugHunter worker - common bug pattern detection
	bugWorker := NewBugHunterWorker()
	if err := registry.Register(bugWorker); err != nil {
		return nil, fmt.Errorf("registering bug hunter worker: %w", err)
	}

	// TODO: Additional workers to implement in future epics
	// Note: Other workers (doc_auditor, dependency_auditor, test_coverage_analyzer, security_scanner)
	// are being implemented in parallel epic vc-cq4l

	return registry, nil
}
