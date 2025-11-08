package sdk

import (
	"fmt"
	"os"
	"path/filepath"
	"plugin"

	"github.com/steveyegge/vc/internal/discovery"
)

// PluginLoader loads custom workers from external .so files.
//
// Workers can be distributed as compiled Go plugins and loaded at runtime.
// This allows extending VC without modifying its source code.
//
// Example plugin structure:
//
//	// my_worker.go
//	package main
//
//	import (
//		"github.com/steveyegge/vc/internal/discovery"
//		"github.com/steveyegge/vc/internal/discovery/sdk"
//	)
//
//	type MyWorker struct {}
//
//	func (w *MyWorker) Name() string { return "my_worker" }
//	// ... implement other DiscoveryWorker methods
//
//	// Export a constructor function
//	var Worker discovery.DiscoveryWorker = &MyWorker{}
//
// Build as plugin:
//
//	go build -buildmode=plugin -o my_worker.so my_worker.go
//
// Load at runtime:
//
//	worker, err := sdk.LoadPlugin("my_worker.so")
type PluginLoader struct {
	// Loaded plugins (path -> plugin)
	plugins map[string]*plugin.Plugin

	// Loaded workers (name -> worker)
	workers map[string]discovery.DiscoveryWorker
}

// NewPluginLoader creates a new plugin loader.
func NewPluginLoader() *PluginLoader {
	return &PluginLoader{
		plugins: make(map[string]*plugin.Plugin),
		workers: make(map[string]discovery.DiscoveryWorker),
	}
}

// LoadPlugin loads a worker from a .so file.
//
// The plugin must export a symbol named "Worker" of type discovery.DiscoveryWorker.
//
// Example:
//
//	loader := sdk.NewPluginLoader()
//	worker, err := loader.LoadPlugin("./workers/my_worker.so")
func (l *PluginLoader) LoadPlugin(path string) (discovery.DiscoveryWorker, error) {
	// Open plugin
	p, err := plugin.Open(path)
	if err != nil {
		return nil, fmt.Errorf("opening plugin %s: %w", path, err)
	}

	l.plugins[path] = p

	// Look up the Worker symbol
	sym, err := p.Lookup("Worker")
	if err != nil {
		return nil, fmt.Errorf("plugin %s must export 'Worker' symbol: %w", path, err)
	}

	// Type assert to DiscoveryWorker
	worker, ok := sym.(discovery.DiscoveryWorker)
	if !ok {
		return nil, fmt.Errorf("plugin %s: 'Worker' symbol is not a DiscoveryWorker", path)
	}

	// Validate worker
	if err := validateWorker(worker); err != nil {
		return nil, fmt.Errorf("plugin %s: invalid worker: %w", path, err)
	}

	// Store worker
	l.workers[worker.Name()] = worker

	return worker, nil
}

// LoadPluginsFromDir loads all .so files from a directory.
//
// Example:
//
//	loader := sdk.NewPluginLoader()
//	workers, err := loader.LoadPluginsFromDir("~/.vc/workers/")
func (l *PluginLoader) LoadPluginsFromDir(dir string) ([]discovery.DiscoveryWorker, error) {
	var workers []discovery.DiscoveryWorker

	// Expand home directory
	if dir[:2] == "~/" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("getting home directory: %w", err)
		}
		dir = filepath.Join(home, dir[2:])
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return workers, nil // Directory doesn't exist, return empty list
		}
		return nil, err
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		// Only load .so files
		name := entry.Name()
		if filepath.Ext(name) != ".so" {
			continue
		}

		path := filepath.Join(dir, name)
		worker, err := l.LoadPlugin(path)
		if err != nil {
			// Log error but continue loading other plugins
			// In production, you'd use proper logging
			fmt.Fprintf(os.Stderr, "Warning: Failed to load plugin %s: %v\n", path, err)
			continue
		}

		workers = append(workers, worker)
	}

	return workers, nil
}

// GetWorker returns a loaded worker by name.
func (l *PluginLoader) GetWorker(name string) (discovery.DiscoveryWorker, bool) {
	worker, ok := l.workers[name]
	return worker, ok
}

// ListWorkers returns names of all loaded workers.
func (l *PluginLoader) ListWorkers() []string {
	names := make([]string, 0, len(l.workers))
	for name := range l.workers {
		names = append(names, name)
	}
	return names
}

// validateWorker performs basic validation on a worker.
func validateWorker(worker discovery.DiscoveryWorker) error {
	if worker.Name() == "" {
		return fmt.Errorf("worker name cannot be empty")
	}
	if worker.Philosophy() == "" {
		return fmt.Errorf("worker philosophy cannot be empty")
	}
	if worker.Scope() == "" {
		return fmt.Errorf("worker scope cannot be empty")
	}

	// Validate dependencies (if any)
	for _, dep := range worker.Dependencies() {
		if dep == "" {
			return fmt.Errorf("empty dependency name")
		}
	}

	return nil
}

// DiscoverWorkers discovers and loads workers from standard locations:
// 1. Project-local: .vc/workers/
// 2. User-global: ~/.vc/workers/
//
// Returns both Go plugin workers and YAML-defined workers.
//
// Example:
//
//	workers, err := sdk.DiscoverWorkers("/path/to/project")
func DiscoverWorkers(projectRoot string) ([]discovery.DiscoveryWorker, error) {
	var allWorkers []discovery.DiscoveryWorker
	loader := NewPluginLoader()

	// 1. Load project-local YAML workers
	projectYAMLDir := filepath.Join(projectRoot, ".vc", "workers")
	yamlWorkers, err := LoadYAMLWorkersFromDir(projectYAMLDir)
	if err != nil {
		// Log but don't fail
		fmt.Fprintf(os.Stderr, "Warning: Failed to load YAML workers from %s: %v\n", projectYAMLDir, err)
	} else {
		for _, w := range yamlWorkers {
			allWorkers = append(allWorkers, w)
		}
	}

	// 2. Load project-local plugin workers
	projectPluginDir := filepath.Join(projectRoot, ".vc", "workers")
	pluginWorkers, err := loader.LoadPluginsFromDir(projectPluginDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: Failed to load plugins from %s: %v\n", projectPluginDir, err)
	} else {
		allWorkers = append(allWorkers, pluginWorkers...)
	}

	// 3. Load user-global YAML workers
	home, err := os.UserHomeDir()
	if err == nil {
		userYAMLDir := filepath.Join(home, ".vc", "workers")
		yamlWorkers, err := LoadYAMLWorkersFromDir(userYAMLDir)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: Failed to load YAML workers from %s: %v\n", userYAMLDir, err)
		} else {
			for _, w := range yamlWorkers {
				allWorkers = append(allWorkers, w)
			}
		}

		// 4. Load user-global plugin workers
		userPluginDir := filepath.Join(home, ".vc", "workers")
		pluginWorkers, err := loader.LoadPluginsFromDir(userPluginDir)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: Failed to load plugins from %s: %v\n", userPluginDir, err)
		} else {
			allWorkers = append(allWorkers, pluginWorkers...)
		}
	}

	return allWorkers, nil
}

// RegisterCustomWorkers registers discovered custom workers with a WorkerRegistry.
//
// Example:
//
//	registry := discovery.NewWorkerRegistry()
//	if err := sdk.RegisterCustomWorkers(registry, "/path/to/project"); err != nil {
//		return err
//	}
func RegisterCustomWorkers(registry interface{}, projectRoot string) error {
	// Discover workers
	workers, err := DiscoverWorkers(projectRoot)
	if err != nil {
		return fmt.Errorf("discovering workers: %w", err)
	}

	// Register each worker
	// Note: This assumes registry has a Register method
	// In real implementation, you'd type assert to *discovery.WorkerRegistry
	for _, worker := range workers {
		// registry.Register(worker) - implementation depends on WorkerRegistry interface
		_ = worker // Placeholder
	}

	return nil
}
