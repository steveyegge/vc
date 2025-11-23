package executor

import "time"

// getExecutorStatus returns the current executor status for the status command
func (e *Executor) getExecutorStatus() map[string]interface{} {
	e.mu.RLock()
	running := e.running
	e.mu.RUnlock()

	status := map[string]interface{}{
		"instance_id":       e.instanceID,
		"hostname":          e.hostname,
		"pid":               e.pid,
		"version":           e.version,
		"running":           running,
		"self_healing_mode": e.getSelfHealingMode().String(),
	}

	// Add current issue if any
	if e.interruptMgr != nil {
		currentIssue := e.interruptMgr.GetCurrentIssue()
		if currentIssue != nil {
			status["current_issue"] = map[string]interface{}{
				"id":    currentIssue.ID,
				"title": currentIssue.Title,
			}
		}
	}

	// Add budget info if available
	if e.costTracker != nil {
		budgetStats := e.costTracker.GetStats()
		status["budget"] = map[string]interface{}{
			"status":             budgetStats.Status.String(),
			"hourly_tokens_used": budgetStats.HourlyTokensUsed,
			"hourly_cost_used":   budgetStats.HourlyCostUsed,
		}
	}

	status["timestamp"] = time.Now().Format(time.RFC3339)

	return status
}
