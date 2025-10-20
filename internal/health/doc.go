// Package health provides code health monitoring for the VC agent system.
//
// # Philosophy: Zero Framework Cognition (ZFC) for Code Health
//
// Traditional code quality tools use rigid rules and thresholds:
//   - "Cyclomatic complexity must be < 10"
//   - "Functions must be < 50 lines"
//   - "No duplicate code blocks > 6 lines"
//
// These rules become outdated, don't adapt to context, and often
// produce false positives. VC takes a different approach.
//
// # ZFC-Compliant Health Monitoring
//
// Health monitors in VC:
//
//  1. Collect facts and patterns (not enforce rules)
//  2. Express timeless principles (not specific thresholds)
//  3. Defer judgment to AI supervision (not hardcoded logic)
//  4. Detect statistical outliers (not absolute violations)
//  5. Provide context for AI interpretation (not binary pass/fail)
//
// # Example: File Size Monitor
//
// BAD (traditional):
//
//	if fileLines > 500 {
//	    return Error("File too large")
//	}
//
// GOOD (ZFC-compliant):
//
//	philosophy := "Large files are harder to understand and modify"
//	if distribution.IsUpperOutlier(fileLines, 2.0) {
//	    context := fmt.Sprintf("File has %d lines (mean: %.0f, stddev: %.0f)",
//	        fileLines, distribution.Mean, distribution.StdDev)
//	    reasoning := "This file is a statistical outlier (>2σ from mean). " +
//	                 "Consider whether it violates the principle: " + philosophy
//	    // AI decides if this is actually a problem
//	}
//
// # Integration with AI Supervision
//
// Health monitors produce DiscoveredIssue structs that feed into
// the AI supervision system. The AI:
//
//  1. Reviews the context and reasoning from the monitor
//  2. Examines the actual code and evidence
//  3. Applies its understanding of late-2025 best practices
//  4. Decides whether to file an issue (or not)
//  5. Suggests appropriate fixes in context
//
// This allows monitors to be simple fact-collectors while the AI
// handles the nuanced judgment calls.
//
// # Monitor Lifecycle
//
//	// 1. Define a monitor
//	type FileSizeMonitor struct { }
//
//	func (m *FileSizeMonitor) Philosophy() string {
//	    return "Large files are harder to understand and modify"
//	}
//
//	// 2. Check codebase
//	result, err := monitor.Check(ctx, codebaseContext)
//
//	// 3. AI supervision reviews result
//	for _, issue := range result.IssuesFound {
//	    // AI analyzes issue.Evidence, result.Context, result.Reasoning
//	    decision := aiSupervisor.ReviewHealthIssue(issue)
//	    if decision.ShouldFile {
//	        // File issue in tracker
//	    }
//	}
//
// # Available Monitors
//
// Current monitors (late 2025):
//   - FileSizeMonitor: Detects files that are statistical outliers in size
//   - CruftDetector: Finds unused/commented code, TODOs, dead imports
//   - ComplexityMonitor: Identifies functions with high cyclomatic complexity
//   - DuplicationMonitor: Discovers repeated code patterns
//
// # Scheduling
//
// Monitors can run on different schedules:
//
//	Time-based: Every 24 hours, every week
//	Event-based: After every 10 issues closed, after git push
//	Hybrid: At least daily, but also after major changes
//	Manual: Only when 'vc health check' is run
//
// # Cost Awareness
//
// Monitors estimate their resource usage:
//
//	Cheap: Quick static analysis, no AI calls
//	Moderate: Limited AI calls, focused scans
//	Expensive: Full codebase AI analysis
//
// The executor can skip expensive monitors during peak activity
// and run them during idle periods.
//
// # Future: Self-Improving Health Checks
//
// As the AI learns codebase conventions, it can:
//   - Suggest new monitors for project-specific patterns
//   - Adjust outlier thresholds based on codebase evolution
//   - Detect emerging anti-patterns early
//   - Recommend architectural improvements
//
// This creates a feedback loop where health monitoring improves
// over time without manual tuning.
package health
