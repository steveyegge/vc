package types

// Discovery label constants for issues created during analysis.
// These labels categorize discovered work by urgency and relationship to the mission.
const (
	// LabelDiscoveredBlocker marks issues that block mission progress.
	// These are selected before regular ready work.
	LabelDiscoveredBlocker = "discovered:blocker"

	// LabelDiscoveredRelated marks issues related to the mission but not blocking.
	// These are selected after regular ready work.
	LabelDiscoveredRelated = "discovered:related"

	// LabelDiscoveredBackground marks issues unrelated to the mission.
	// These are lower priority than discovered:related.
	LabelDiscoveredBackground = "discovered:background"
)
