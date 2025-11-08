package workers

import (
	"github.com/steveyegge/vc/internal/discovery"
)

// RegisterAll registers all discovery workers with the given registry.
// This is called by the orchestrator to set up the full worker suite.
func RegisterAll(registry *discovery.WorkerRegistry) error {
	workers := []discovery.DiscoveryWorker{
		NewDocAuditor(),
		NewTestCoverageAnalyzer(),
		NewDependencyAuditor(),
		NewSecurityScanner(),
	}

	for _, worker := range workers {
		if err := registry.Register(worker); err != nil {
			return err
		}
	}

	return nil
}
