// Package workers provides discovery workers for analyzing codebases.
//
// Discovery workers implement the DiscoveryWorker interface and perform
// specific types of codebase analysis during initial bootstrap.
//
// Available Workers:
//
//   - DocAuditor: Analyzes documentation quality (package docs, README, API docs)
//   - TestCoverageAnalyzer: Finds test coverage gaps and weak tests
//   - DependencyAuditor: Audits dependencies (outdated, vulnerabilities, licenses)
//   - SecurityScanner: Scans for security issues (credentials, SQL injection, weak crypto)
//
// Each worker follows the Zero Framework Cognition (ZFC) principle:
// Workers collect facts and patterns, AI supervision interprets them.
//
// Usage:
//
//	// Create workers
//	docWorker := workers.NewDocAuditor()
//	testWorker := workers.NewTestCoverageAnalyzer()
//	depWorker := workers.NewDependencyAuditor()
//	secWorker := workers.NewSecurityScanner()
//
//	// Register with discovery system
//	registry := discovery.NewWorkerRegistry()
//	registry.Register(docWorker)
//	registry.Register(testWorker)
//	registry.Register(depWorker)
//	registry.Register(secWorker)
//
// Workers are designed to be:
//   - Composable: Can run independently or in sequence
//   - Efficient: Share CodebaseContext to avoid redundant work
//   - Cost-aware: Provide estimates for budget enforcement
//   - Dependency-aware: Declare dependencies for correct execution order
package workers
