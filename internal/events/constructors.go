package events

import (
	"time"

	"github.com/google/uuid"
)

// NewFileModifiedEvent creates a new AgentEvent for a file modification with type-safe data.
func NewFileModifiedEvent(issueID, executorID, agentID string, severity EventSeverity, message string, data FileModifiedData) (*AgentEvent, error) {
	event := &AgentEvent{
		ID:         uuid.New().String(),
		Type:       EventTypeFileModified,
		Timestamp:  time.Now(),
		IssueID:    issueID,
		ExecutorID: executorID,
		AgentID:    agentID,
		Severity:   severity,
		Message:    message,
		SourceLine: 0,
	}
	if err := event.SetFileModifiedData(data); err != nil {
		return nil, err
	}
	return event, nil
}

// NewTestRunEvent creates a new AgentEvent for a test run with type-safe data.
func NewTestRunEvent(issueID, executorID, agentID string, severity EventSeverity, message string, data TestRunData) (*AgentEvent, error) {
	event := &AgentEvent{
		ID:         uuid.New().String(),
		Type:       EventTypeTestRun,
		Timestamp:  time.Now(),
		IssueID:    issueID,
		ExecutorID: executorID,
		AgentID:    agentID,
		Severity:   severity,
		Message:    message,
		SourceLine: 0,
	}
	if err := event.SetTestRunData(data); err != nil {
		return nil, err
	}
	return event, nil
}

// NewGitOperationEvent creates a new AgentEvent for a git operation with type-safe data.
func NewGitOperationEvent(issueID, executorID, agentID string, severity EventSeverity, message string, data GitOperationData) (*AgentEvent, error) {
	event := &AgentEvent{
		ID:         uuid.New().String(),
		Type:       EventTypeGitOperation,
		Timestamp:  time.Now(),
		IssueID:    issueID,
		ExecutorID: executorID,
		AgentID:    agentID,
		Severity:   severity,
		Message:    message,
		SourceLine: 0,
	}
	if err := event.SetGitOperationData(data); err != nil {
		return nil, err
	}
	return event, nil
}

// NewExecutorEvent creates a new AgentEvent for executor-level events (no specific data structure).
func NewExecutorEvent(eventType EventType, issueID, executorID, agentID string, severity EventSeverity, message string, data map[string]interface{}) *AgentEvent {
	if data == nil {
		data = make(map[string]interface{})
	}
	return &AgentEvent{
		ID:         uuid.New().String(),
		Type:       eventType,
		Timestamp:  time.Now(),
		IssueID:    issueID,
		ExecutorID: executorID,
		AgentID:    agentID,
		Severity:   severity,
		Message:    message,
		Data:       data,
		SourceLine: 0,
	}
}

// NewSimpleEvent creates a new AgentEvent with no structured data (for progress, errors, etc.).
func NewSimpleEvent(eventType EventType, issueID, executorID, agentID string, severity EventSeverity, message string) *AgentEvent {
	return &AgentEvent{
		ID:         uuid.New().String(),
		Type:       eventType,
		Timestamp:  time.Now(),
		IssueID:    issueID,
		ExecutorID: executorID,
		AgentID:    agentID,
		Severity:   severity,
		Message:    message,
		Data:       make(map[string]interface{}),
		SourceLine: 0,
	}
}

// NewDeduplicationBatchStartedEvent creates a new AgentEvent for deduplication batch start with type-safe data (vc-151).
func NewDeduplicationBatchStartedEvent(issueID, executorID, agentID string, severity EventSeverity, message string, data DeduplicationBatchStartedData) (*AgentEvent, error) {
	event := &AgentEvent{
		ID:         uuid.New().String(),
		Type:       EventTypeDeduplicationBatchStarted,
		Timestamp:  time.Now(),
		IssueID:    issueID,
		ExecutorID: executorID,
		AgentID:    agentID,
		Severity:   severity,
		Message:    message,
		SourceLine: 0,
	}
	if err := event.SetDeduplicationBatchStartedData(data); err != nil {
		return nil, err
	}
	return event, nil
}

// NewDeduplicationBatchCompletedEvent creates a new AgentEvent for deduplication batch completion with type-safe data (vc-151).
func NewDeduplicationBatchCompletedEvent(issueID, executorID, agentID string, severity EventSeverity, message string, data DeduplicationBatchCompletedData) (*AgentEvent, error) {
	event := &AgentEvent{
		ID:         uuid.New().String(),
		Type:       EventTypeDeduplicationBatchCompleted,
		Timestamp:  time.Now(),
		IssueID:    issueID,
		ExecutorID: executorID,
		AgentID:    agentID,
		Severity:   severity,
		Message:    message,
		SourceLine: 0,
	}
	if err := event.SetDeduplicationBatchCompletedData(data); err != nil {
		return nil, err
	}
	return event, nil
}

// NewDeduplicationDecisionEvent creates a new AgentEvent for an individual deduplication decision with type-safe data (vc-151).
func NewDeduplicationDecisionEvent(issueID, executorID, agentID string, severity EventSeverity, message string, data DeduplicationDecisionData) (*AgentEvent, error) {
	event := &AgentEvent{
		ID:         uuid.New().String(),
		Type:       EventTypeDeduplicationDecision,
		Timestamp:  time.Now(),
		IssueID:    issueID,
		ExecutorID: executorID,
		AgentID:    agentID,
		Severity:   severity,
		Message:    message,
		SourceLine: 0,
	}
	if err := event.SetDeduplicationDecisionData(data); err != nil {
		return nil, err
	}
	return event, nil
}
