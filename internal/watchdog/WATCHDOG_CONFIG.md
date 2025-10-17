# Watchdog Configuration Guide

The VC Watchdog provides behavioral monitoring and anomaly detection for agent executions. This guide explains how to configure the watchdog system.

## Table of Contents

- [Overview](#overview)
- [Configuration Methods](#configuration-methods)
- [Configuration Options](#configuration-options)
- [Examples](#examples)
- [Runtime Reconfiguration](#runtime-reconfiguration)
- [Best Practices](#best-practices)

## Overview

The watchdog monitors agent behavior and can automatically intervene when anomalies are detected. It uses AI-driven analysis to detect problems like:

- Infinite loops (issue keeps re-executing without progress)
- Thrashing (rapid state changes without completion)
- Stuck states (execution stuck for too long)
- Regression patterns (failures after previous successes)
- Resource spikes or unusual patterns

## Configuration Methods

The watchdog can be configured in three ways (in order of precedence):

1. **Environment Variables** - Override defaults with `VC_WATCHDOG_*` environment variables
2. **Configuration File** - Load from JSON file using `LoadFromFile(path)`
3. **Default Values** - Safe, conservative defaults if no config is provided

## Configuration Options

### Overall Watchdog Settings

#### `enabled` (bool)
- **Default**: `true`
- **Environment**: `VC_WATCHDOG_ENABLED` (true/false, yes/no, 1/0, on/off)
- **Description**: Master switch to enable/disable the watchdog
- **Example**: `export VC_WATCHDOG_ENABLED=true`

#### `check_interval` (duration)
- **Default**: `30s`
- **Environment**: `VC_WATCHDOG_CHECK_INTERVAL` (Go duration format)
- **Valid Range**: 5s to 5m
- **Description**: How often to run anomaly detection
- **Example**: `export VC_WATCHDOG_CHECK_INTERVAL=45s`

#### `telemetry_window_size` (int)
- **Default**: `100`
- **Environment**: `VC_WATCHDOG_TELEMETRY_WINDOW`
- **Valid Range**: 1 to 10000
- **Description**: Number of recent executions to keep for analysis
- **Example**: `export VC_WATCHDOG_TELEMETRY_WINDOW=150`

#### `max_history_size` (int)
- **Default**: `100`
- **Environment**: `VC_WATCHDOG_MAX_HISTORY`
- **Valid Range**: 1 to 10000
- **Description**: Maximum number of interventions to keep in memory
- **Example**: `export VC_WATCHDOG_MAX_HISTORY=200`

### AI Sensitivity Settings

#### `ai_config.min_confidence_threshold` (float)
- **Default**: `0.75`
- **Environment**: `VC_WATCHDOG_MIN_CONFIDENCE`
- **Valid Range**: 0.0 to 1.0
- **Description**: Minimum confidence level required for anomaly detection to trigger intervention
- **Lower values**: More sensitive, may have false positives
- **Higher values**: Less sensitive, may miss some anomalies
- **Example**: `export VC_WATCHDOG_MIN_CONFIDENCE=0.80`

#### `ai_config.min_severity_level` (string)
- **Default**: `high`
- **Environment**: `VC_WATCHDOG_MIN_SEVERITY`
- **Valid Values**: `low`, `medium`, `high`, `critical`
- **Description**: Minimum severity level that triggers automatic intervention
- **Example**: `export VC_WATCHDOG_MIN_SEVERITY=medium`

Severity levels in order:
- `critical` - Requires immediate intervention
- `high` - Should be addressed soon (default threshold)
- `medium` - Notable but not urgent
- `low` - Informational

#### `ai_config.enable_anomaly_logging` (bool)
- **Default**: `true`
- **Environment**: `VC_WATCHDOG_LOG_ANOMALIES` (true/false, yes/no, 1/0)
- **Description**: Log all anomaly detections (even below threshold) for debugging
- **Example**: `export VC_WATCHDOG_LOG_ANOMALIES=true`

### Intervention Policies

#### `intervention_config.auto_kill_enabled` (bool)
- **Default**: `true`
- **Environment**: `VC_WATCHDOG_AUTO_KILL` (true/false, yes/no, 1/0)
- **Description**: Allow watchdog to automatically kill agents
- **If false**: Watchdog only creates escalation issues without intervention
- **Example**: `export VC_WATCHDOG_AUTO_KILL=true`

#### `intervention_config.max_retries` (int)
- **Default**: `3`
- **Environment**: `VC_WATCHDOG_MAX_RETRIES`
- **Valid Range**: 0 to 100
- **Description**: Number of times an issue can fail before being marked as blocked
- **0 = no retry limit** (not recommended)
- **Example**: `export VC_WATCHDOG_MAX_RETRIES=5`

#### `intervention_config.escalate_on_critical` (bool)
- **Default**: `true`
- **Environment**: `VC_WATCHDOG_ESCALATE_CRITICAL` (true/false, yes/no, 1/0)
- **Description**: Create escalation issues immediately for critical anomalies, even if intervention is disabled
- **Example**: `export VC_WATCHDOG_ESCALATE_CRITICAL=true`

#### `intervention_config.escalation_priority` (map)
- **Default**: `{critical: 0, high: 1, medium: 2, low: 3}`
- **Environment**: Not configurable via env vars (use config file)
- **Description**: Maps anomaly severity to escalation issue priority (P0-P3)

## Examples

### Example 1: Default Configuration

```go
// Use default configuration
cfg := watchdog.DefaultWatchdogConfig()

// All settings use safe, conservative defaults:
// - Enabled: true
// - Check interval: 30s
// - Min confidence: 0.75
// - Min severity: high
// - Auto-kill: enabled
// - Max retries: 3
```

### Example 2: Configuration File

Create a JSON file (`watchdog.json`):

```json
{
  "enabled": true,
  "check_interval": "45s",
  "telemetry_window_size": 150,
  "ai_config": {
    "min_confidence_threshold": 0.80,
    "min_severity_level": "medium",
    "enable_anomaly_logging": true
  },
  "intervention_config": {
    "auto_kill_enabled": true,
    "max_retries": 5,
    "escalate_on_critical": true,
    "escalation_priority": {
      "critical": 0,
      "high": 1,
      "medium": 2,
      "low": 3
    }
  },
  "max_history_size": 200
}
```

Load it:

```go
cfg, err := watchdog.LoadFromFile("watchdog.json")
if err != nil {
    log.Fatalf("Failed to load config: %v", err)
}
```

### Example 3: Environment Variables

```bash
# Enable watchdog with custom settings
export VC_WATCHDOG_ENABLED=true
export VC_WATCHDOG_CHECK_INTERVAL=1m
export VC_WATCHDOG_TELEMETRY_WINDOW=200
export VC_WATCHDOG_MIN_CONFIDENCE=0.85
export VC_WATCHDOG_MIN_SEVERITY=critical
export VC_WATCHDOG_AUTO_KILL=true
export VC_WATCHDOG_MAX_RETRIES=3

# Run your application
./vc
```

In code:

```go
// Load configuration from environment variables
cfg := watchdog.LoadFromEnv()
```

### Example 4: Validation

```go
cfg := watchdog.DefaultWatchdogConfig()

// Modify settings
cfg.CheckInterval = 45 * time.Second
cfg.AIConfig.MinConfidenceThreshold = 0.85

// Validate before using
if err := cfg.Validate(); err != nil {
    log.Fatalf("Invalid config: %v", err)
}
```

## Runtime Reconfiguration

The watchdog supports runtime reconfiguration without restart:

### Update AI Sensitivity

```go
newAIConfig := watchdog.AIConfig{
    MinConfidenceThreshold: 0.90,
    MinSeverityLevel:       watchdog.SeverityCritical,
    EnableAnomalyLogging:   true,
}

if err := cfg.UpdateAIConfig(newAIConfig); err != nil {
    log.Printf("Failed to update AI config: %v", err)
}
```

### Update Intervention Policies

```go
newInterventionConfig := watchdog.InterventionConfig{
    AutoKillEnabled:    false, // Disable auto-kill
    MaxRetries:         10,
    EscalateOnCritical: true,
    EscalationPriority: cfg.InterventionConfig.EscalationPriority,
}

if err := cfg.UpdateInterventionConfig(newInterventionConfig); err != nil {
    log.Printf("Failed to update intervention config: %v", err)
}
```

### Enable/Disable Watchdog

```go
// Disable watchdog temporarily
cfg.SetEnabled(false)

// Re-enable later
cfg.SetEnabled(true)
```

### Update Check Interval

```go
// Change check interval to 2 minutes
if err := cfg.SetCheckInterval(2 * time.Minute); err != nil {
    log.Printf("Failed to update check interval: %v", err)
}
```

### Save Configuration

```go
// Save current configuration to file
if err := cfg.SaveToFile("watchdog-current.json"); err != nil {
    log.Printf("Failed to save config: %v", err)
}
```

## Best Practices

### 1. Start Conservative

Use the default configuration initially. It's optimized for safety:
- High confidence threshold (0.75)
- Only intervenes on high/critical severity
- Limited retries (3)

```go
cfg := watchdog.DefaultWatchdogConfig()
```

### 2. Tune Based on Experience

Monitor watchdog behavior and adjust sensitivity:

**Too many false positives?**
```go
// Increase confidence threshold
cfg.AIConfig.MinConfidenceThreshold = 0.85

// Increase severity threshold
cfg.AIConfig.MinSeverityLevel = watchdog.SeverityCritical
```

**Missing real problems?**
```go
// Decrease confidence threshold
cfg.AIConfig.MinConfidenceThreshold = 0.70

// Lower severity threshold
cfg.AIConfig.MinSeverityLevel = watchdog.SeverityMedium
```

### 3. Environment-Specific Settings

**Development Environment:**
```bash
# More aggressive detection to catch issues early
export VC_WATCHDOG_MIN_CONFIDENCE=0.70
export VC_WATCHDOG_MIN_SEVERITY=medium
export VC_WATCHDOG_CHECK_INTERVAL=15s
```

**Production Environment:**
```bash
# Conservative detection to avoid false positives
export VC_WATCHDOG_MIN_CONFIDENCE=0.85
export VC_WATCHDOG_MIN_SEVERITY=high
export VC_WATCHDOG_CHECK_INTERVAL=1m
```

### 4. Enable Logging

Always enable anomaly logging for debugging:

```go
cfg.AIConfig.EnableAnomalyLogging = true
```

This logs all anomalies (even below threshold) so you can analyze patterns and tune thresholds.

### 5. Monitor Escalation Issues

Regularly review escalation issues created by the watchdog:

```bash
# Check for watchdog escalations
bd list --text "Watchdog:" --status open
```

Use these to understand:
- Are thresholds too aggressive or too lenient?
- What types of anomalies are most common?
- Are interventions effective?

### 6. Test Configuration Changes

Before deploying config changes to production:

1. Validate the configuration:
```go
if err := cfg.Validate(); err != nil {
    log.Fatalf("Invalid config: %v", err)
}
```

2. Test in development environment
3. Monitor behavior closely after changes
4. Revert if unexpected issues occur

### 7. Use Runtime Reconfiguration Carefully

Runtime updates are powerful but can cause unexpected behavior:

```go
// Clone config before modifying
backup := cfg.Clone()

// Try new settings
if err := cfg.UpdateAIConfig(newAIConfig); err != nil {
    log.Printf("Update failed: %v", err)
    // Could restore backup if needed
}
```

### 8. Document Custom Settings

If you deviate from defaults, document why:

```json
{
  "_comment": "Increased confidence threshold to 0.85 because we were seeing false positives on long-running refactoring tasks",
  "ai_config": {
    "min_confidence_threshold": 0.85
  }
}
```

## Configuration Validation

The watchdog validates all configuration values to prevent unsafe settings:

| Setting | Validation |
|---------|------------|
| `check_interval` | Must be between 5s and 5m |
| `telemetry_window_size` | Must be between 1 and 10000 |
| `min_confidence_threshold` | Must be between 0.0 and 1.0 |
| `min_severity_level` | Must be low, medium, high, or critical |
| `max_retries` | Must be between 0 and 100 |
| `max_history_size` | Must be between 1 and 10000 |

Invalid configurations will return an error from `Validate()`, `LoadFromFile()`, or `UpdateAIConfig()`/`UpdateInterventionConfig()`.

## Thread Safety

All runtime reconfiguration methods are thread-safe:

```go
// Safe to call concurrently
go func() {
    cfg.SetEnabled(false)
}()

go func() {
    interval := cfg.GetCheckInterval()
    fmt.Printf("Current interval: %v\n", interval)
}()
```

The configuration uses `sync.RWMutex` internally to coordinate access.

## Summary

The watchdog configuration system provides:

- **Safe defaults** - Conservative settings optimized for safety
- **Multiple configuration methods** - Environment vars, files, or programmatic
- **Runtime reconfiguration** - Tune without restart
- **Validation** - Prevents unsafe configurations
- **Thread-safe** - Safe for concurrent access

Start with defaults, monitor behavior, and tune based on your specific needs.
