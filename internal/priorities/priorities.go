package priorities

// CalculateDiscoveredPriority calculates the priority for a discovered issue
// based on the parent issue's priority and the discovery type label.
//
// Priority inheritance rules (vc-152):
// - Blockers: Escalate to at least P0 (prevents parent from completing)
// - Related: Inherit parent priority + 1 (capped at P3)
// - Background: Default to P2 (opportunistic discoveries)
// - Unknown: Inherit parent priority
func CalculateDiscoveredPriority(parentPriority int, discoveryType string) int {
	switch discoveryType {
	case "blocker":
		// Blockers should be high priority (at least P0)
		// If parent is P0, blocker is P0
		// If parent is P1+, blocker escalates to P0
		if parentPriority < 0 {
			return 0 // Cap at P0
		}
		return 0 // Blockers are always P0

	case "related":
		// Related work is slightly lower priority than parent
		// P0 parent -> P1 related
		// P1 parent -> P2 related
		// P2 parent -> P3 related
		// P3 parent -> P3 related (capped)
		newPriority := parentPriority + 1
		if newPriority > 3 {
			return 3 // Cap at P3 (lowest priority)
		}
		return newPriority

	case "background":
		// Background discoveries are opportunistic, default to P2
		return 2

	default:
		// No discovery type specified, inherit parent priority
		return parentPriority
	}
}
