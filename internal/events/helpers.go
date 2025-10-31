package events

import (
	"encoding/json"
	"fmt"
)

// SetFileModifiedData sets the Data field with FileModifiedData in a type-safe way.
func (e *AgentEvent) SetFileModifiedData(data FileModifiedData) error {
	dataMap, err := structToMap(data)
	if err != nil {
		return fmt.Errorf("failed to convert FileModifiedData: %w", err)
	}
	e.Data = dataMap
	return nil
}

// GetFileModifiedData retrieves FileModifiedData from the Data field.
func (e *AgentEvent) GetFileModifiedData() (*FileModifiedData, error) {
	var data FileModifiedData
	if err := mapToStruct(e.Data, &data); err != nil {
		return nil, fmt.Errorf("failed to parse FileModifiedData: %w", err)
	}
	return &data, nil
}

// SetTestRunData sets the Data field with TestRunData in a type-safe way.
func (e *AgentEvent) SetTestRunData(data TestRunData) error {
	dataMap, err := structToMap(data)
	if err != nil {
		return fmt.Errorf("failed to convert TestRunData: %w", err)
	}
	e.Data = dataMap
	return nil
}

// GetTestRunData retrieves TestRunData from the Data field.
func (e *AgentEvent) GetTestRunData() (*TestRunData, error) {
	var data TestRunData
	if err := mapToStruct(e.Data, &data); err != nil {
		return nil, fmt.Errorf("failed to parse TestRunData: %w", err)
	}
	return &data, nil
}

// SetGitOperationData sets the Data field with GitOperationData in a type-safe way.
func (e *AgentEvent) SetGitOperationData(data GitOperationData) error {
	dataMap, err := structToMap(data)
	if err != nil {
		return fmt.Errorf("failed to convert GitOperationData: %w", err)
	}
	e.Data = dataMap
	return nil
}

// GetGitOperationData retrieves GitOperationData from the Data field.
func (e *AgentEvent) GetGitOperationData() (*GitOperationData, error) {
	var data GitOperationData
	if err := mapToStruct(e.Data, &data); err != nil {
		return nil, fmt.Errorf("failed to parse GitOperationData: %w", err)
	}
	return &data, nil
}

// structToMap converts a struct to map[string]interface{} using JSON marshaling.
func structToMap(data interface{}) (map[string]interface{}, error) {
	bytes, err := json.Marshal(data)
	if err != nil {
		return nil, err
	}
	var result map[string]interface{}
	if err := json.Unmarshal(bytes, &result); err != nil {
		return nil, err
	}
	return result, nil
}

// mapToStruct converts a map[string]interface{} to a struct using JSON unmarshaling.
func mapToStruct(dataMap map[string]interface{}, target interface{}) error {
	bytes, err := json.Marshal(dataMap)
	if err != nil {
		return err
	}
	return json.Unmarshal(bytes, target)
}

// SetDeduplicationBatchStartedData sets the Data field with DeduplicationBatchStartedData in a type-safe way (vc-151).
func (e *AgentEvent) SetDeduplicationBatchStartedData(data DeduplicationBatchStartedData) error {
	dataMap, err := structToMap(data)
	if err != nil {
		return fmt.Errorf("failed to convert DeduplicationBatchStartedData: %w", err)
	}
	e.Data = dataMap
	return nil
}

// GetDeduplicationBatchStartedData retrieves DeduplicationBatchStartedData from the Data field (vc-151).
func (e *AgentEvent) GetDeduplicationBatchStartedData() (*DeduplicationBatchStartedData, error) {
	var data DeduplicationBatchStartedData
	if err := mapToStruct(e.Data, &data); err != nil {
		return nil, fmt.Errorf("failed to parse DeduplicationBatchStartedData: %w", err)
	}
	return &data, nil
}

// SetDeduplicationBatchCompletedData sets the Data field with DeduplicationBatchCompletedData in a type-safe way (vc-151).
func (e *AgentEvent) SetDeduplicationBatchCompletedData(data DeduplicationBatchCompletedData) error {
	dataMap, err := structToMap(data)
	if err != nil {
		return fmt.Errorf("failed to convert DeduplicationBatchCompletedData: %w", err)
	}
	e.Data = dataMap
	return nil
}

// GetDeduplicationBatchCompletedData retrieves DeduplicationBatchCompletedData from the Data field (vc-151).
func (e *AgentEvent) GetDeduplicationBatchCompletedData() (*DeduplicationBatchCompletedData, error) {
	var data DeduplicationBatchCompletedData
	if err := mapToStruct(e.Data, &data); err != nil {
		return nil, fmt.Errorf("failed to parse DeduplicationBatchCompletedData: %w", err)
	}
	return &data, nil
}

// SetDeduplicationDecisionData sets the Data field with DeduplicationDecisionData in a type-safe way (vc-151).
func (e *AgentEvent) SetDeduplicationDecisionData(data DeduplicationDecisionData) error {
	dataMap, err := structToMap(data)
	if err != nil {
		return fmt.Errorf("failed to convert DeduplicationDecisionData: %w", err)
	}
	e.Data = dataMap
	return nil
}

// GetDeduplicationDecisionData retrieves DeduplicationDecisionData from the Data field (vc-151).
func (e *AgentEvent) GetDeduplicationDecisionData() (*DeduplicationDecisionData, error) {
	var data DeduplicationDecisionData
	if err := mapToStruct(e.Data, &data); err != nil {
		return nil, fmt.Errorf("failed to parse DeduplicationDecisionData: %w", err)
	}
	return &data, nil
}

// SetAgentToolUseData sets the Data field with AgentToolUseData in a type-safe way (vc-129).
func (e *AgentEvent) SetAgentToolUseData(data AgentToolUseData) error {
	dataMap, err := structToMap(data)
	if err != nil {
		return fmt.Errorf("failed to convert AgentToolUseData: %w", err)
	}
	e.Data = dataMap
	return nil
}

// GetAgentToolUseData retrieves AgentToolUseData from the Data field (vc-129).
func (e *AgentEvent) GetAgentToolUseData() (*AgentToolUseData, error) {
	var data AgentToolUseData
	if err := mapToStruct(e.Data, &data); err != nil {
		return nil, fmt.Errorf("failed to parse AgentToolUseData: %w", err)
	}
	return &data, nil
}

// SetAgentHeartbeatData sets the Data field with AgentHeartbeatData in a type-safe way (vc-129).
func (e *AgentEvent) SetAgentHeartbeatData(data AgentHeartbeatData) error {
	dataMap, err := structToMap(data)
	if err != nil {
		return fmt.Errorf("failed to convert AgentHeartbeatData: %w", err)
	}
	e.Data = dataMap
	return nil
}

// GetAgentHeartbeatData retrieves AgentHeartbeatData from the Data field (vc-129).
func (e *AgentEvent) GetAgentHeartbeatData() (*AgentHeartbeatData, error) {
	var data AgentHeartbeatData
	if err := mapToStruct(e.Data, &data); err != nil {
		return nil, fmt.Errorf("failed to parse AgentHeartbeatData: %w", err)
	}
	return &data, nil
}

// SetAgentStateChangeData sets the Data field with AgentStateChangeData in a type-safe way (vc-129).
func (e *AgentEvent) SetAgentStateChangeData(data AgentStateChangeData) error {
	dataMap, err := structToMap(data)
	if err != nil {
		return fmt.Errorf("failed to convert AgentStateChangeData: %w", err)
	}
	e.Data = dataMap
	return nil
}

// GetAgentStateChangeData retrieves AgentStateChangeData from the Data field (vc-129).
func (e *AgentEvent) GetAgentStateChangeData() (*AgentStateChangeData, error) {
	var data AgentStateChangeData
	if err := mapToStruct(e.Data, &data); err != nil {
		return nil, fmt.Errorf("failed to parse AgentStateChangeData: %w", err)
	}
	return &data, nil
}

// SetQualityGatesProgressData sets the Data field with QualityGatesProgressData in a type-safe way (vc-273).
func (e *AgentEvent) SetQualityGatesProgressData(data QualityGatesProgressData) error {
	dataMap, err := structToMap(data)
	if err != nil {
		return fmt.Errorf("failed to convert QualityGatesProgressData: %w", err)
	}
	e.Data = dataMap
	return nil
}

// GetQualityGatesProgressData retrieves QualityGatesProgressData from the Data field (vc-273).
func (e *AgentEvent) GetQualityGatesProgressData() (*QualityGatesProgressData, error) {
	var data QualityGatesProgressData
	if err := mapToStruct(e.Data, &data); err != nil {
		return nil, fmt.Errorf("failed to parse QualityGatesProgressData: %w", err)
	}
	return &data, nil
}
