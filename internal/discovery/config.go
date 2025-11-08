package discovery

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"gopkg.in/yaml.v3"
)

// ConfigFile represents the structure of .vc/discovery.yaml
type ConfigFile struct {
	// Preset to use (quick/standard/thorough/custom)
	Preset string `yaml:"preset"`

	// Overall budget constraints
	Budget BudgetConfig `yaml:"budget"`

	// Workers to run (if empty, uses preset defaults)
	Workers []string `yaml:"workers"`

	// Path filters
	IncludePaths []string `yaml:"include_paths"`
	ExcludePaths []string `yaml:"exclude_paths"`

	// Per-worker configurations
	WorkerConfigs map[string]map[string]interface{} `yaml:"worker_configs"`

	// Deduplication settings
	Deduplication DeduplicationConfig `yaml:"deduplication"`

	// Assessment settings
	Assessment AssessmentConfig `yaml:"assessment"`

	// Issue filing settings
	IssueFiling IssueFilingConfig `yaml:"issue_filing"`
}

// BudgetConfig defines budget constraints in the config file.
type BudgetConfig struct {
	MaxCost     float64 `yaml:"max_cost"`
	MaxDuration string  `yaml:"max_duration"` // Duration string like "5m", "1h"
	MaxAICalls  int     `yaml:"max_ai_calls"`
	MaxIssues   int     `yaml:"max_issues"`

	// Per-worker budget overrides
	WorkerBudgets map[string]WorkerBudgetConfig `yaml:"worker_budgets"`
}

// WorkerBudgetConfig defines budget for a specific worker.
type WorkerBudgetConfig struct {
	MaxCost     float64 `yaml:"max_cost"`
	MaxDuration string  `yaml:"max_duration"`
	MaxAICalls  int     `yaml:"max_ai_calls"`
	MaxIssues   int     `yaml:"max_issues"`
}

// DeduplicationConfig defines deduplication settings in the config file.
type DeduplicationConfig struct {
	Enabled bool   `yaml:"enabled"`
	Window  string `yaml:"window"` // Duration string like "7d", "24h"
}

// AssessmentConfig defines AI assessment settings in the config file.
type AssessmentConfig struct {
	Enabled bool   `yaml:"enabled"`
	Model   string `yaml:"model"`
}

// IssueFilingConfig defines issue filing settings in the config file.
type IssueFilingConfig struct {
	AutoFile        bool     `yaml:"auto_file"`
	DefaultPriority int      `yaml:"default_priority"`
	DefaultLabels   []string `yaml:"default_labels"`
}

// LoadConfigFile loads configuration from .vc/discovery.yaml
func LoadConfigFile(projectRoot string) (*Config, error) {
	configPath := filepath.Join(projectRoot, ".vc", "discovery.yaml")

	// If file doesn't exist, return default config
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		return DefaultConfig(), nil
	}

	// Read file
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("reading config file: %w", err)
	}

	// Parse YAML
	var configFile ConfigFile
	if err := yaml.Unmarshal(data, &configFile); err != nil {
		return nil, fmt.Errorf("parsing config file: %w", err)
	}

	// Convert to Config
	return configFile.ToConfig()
}

// ToConfig converts a ConfigFile to a Config.
func (cf *ConfigFile) ToConfig() (*Config, error) {
	// Start with preset or default
	var config *Config
	if cf.Preset != "" {
		config = PresetConfig(Preset(cf.Preset))
	} else {
		config = DefaultConfig()
	}

	// Override with file settings
	if cf.Budget.MaxCost > 0 {
		config.Budget.MaxCost = cf.Budget.MaxCost
	}
	if cf.Budget.MaxDuration != "" {
		duration, err := parseDuration(cf.Budget.MaxDuration)
		if err != nil {
			return nil, fmt.Errorf("invalid max_duration: %w", err)
		}
		config.Budget.MaxDuration = duration
	}
	if cf.Budget.MaxAICalls > 0 {
		config.Budget.MaxAICalls = cf.Budget.MaxAICalls
	}
	if cf.Budget.MaxIssues > 0 {
		config.Budget.MaxIssues = cf.Budget.MaxIssues
	}

	// Worker budgets
	if len(cf.Budget.WorkerBudgets) > 0 {
		if config.Budget.WorkerBudgets == nil {
			config.Budget.WorkerBudgets = make(map[string]WorkerBudget)
		}
		for name, wbc := range cf.Budget.WorkerBudgets {
			wb := WorkerBudget{
				MaxCost:    wbc.MaxCost,
				MaxAICalls: wbc.MaxAICalls,
				MaxIssues:  wbc.MaxIssues,
			}
			if wbc.MaxDuration != "" {
				duration, err := parseDuration(wbc.MaxDuration)
				if err != nil {
					return nil, fmt.Errorf("invalid worker budget duration for %s: %w", name, err)
				}
				wb.MaxDuration = duration
			}
			config.Budget.WorkerBudgets[name] = wb
		}
	}

	// Workers list
	if len(cf.Workers) > 0 {
		config.Workers = cf.Workers
	}

	// Path filters
	if len(cf.IncludePaths) > 0 {
		config.IncludePaths = cf.IncludePaths
	}
	if len(cf.ExcludePaths) > 0 {
		config.ExcludePaths = cf.ExcludePaths
	}

	// Worker configs
	if len(cf.WorkerConfigs) > 0 {
		if config.WorkerConfigs == nil {
			config.WorkerConfigs = make(map[string]interface{})
		}
		for name, wcfg := range cf.WorkerConfigs {
			config.WorkerConfigs[name] = wcfg
		}
	}

	// Deduplication
	config.DeduplicationEnabled = cf.Deduplication.Enabled
	if cf.Deduplication.Window != "" {
		duration, err := parseDuration(cf.Deduplication.Window)
		if err != nil {
			return nil, fmt.Errorf("invalid deduplication window: %w", err)
		}
		config.DeduplicationWindow = duration
	}

	// Assessment
	config.AssessmentEnabled = cf.Assessment.Enabled
	if cf.Assessment.Model != "" {
		config.AssessmentModel = cf.Assessment.Model
	}

	// Issue filing
	config.AutoFileIssues = cf.IssueFiling.AutoFile
	if cf.IssueFiling.DefaultPriority >= 0 {
		config.DefaultPriority = cf.IssueFiling.DefaultPriority
	}
	if len(cf.IssueFiling.DefaultLabels) > 0 {
		config.DefaultLabels = cf.IssueFiling.DefaultLabels
	}

	return config, nil
}

// SaveConfigFile saves a Config to .vc/discovery.yaml
func SaveConfigFile(projectRoot string, config *Config) error {
	configPath := filepath.Join(projectRoot, ".vc", "discovery.yaml")

	// Ensure .vc directory exists
	if err := os.MkdirAll(filepath.Dir(configPath), 0755); err != nil {
		return fmt.Errorf("creating .vc directory: %w", err)
	}

	// Convert Config to ConfigFile
	configFile := ConfigFile{
		Preset:  string(config.Preset),
		Workers: config.Workers,
		Budget: BudgetConfig{
			MaxCost:    config.Budget.MaxCost,
			MaxDuration: config.Budget.MaxDuration.String(),
			MaxAICalls: config.Budget.MaxAICalls,
			MaxIssues:  config.Budget.MaxIssues,
		},
		IncludePaths: config.IncludePaths,
		ExcludePaths: config.ExcludePaths,
		WorkerConfigs: make(map[string]map[string]interface{}),
		Deduplication: DeduplicationConfig{
			Enabled: config.DeduplicationEnabled,
			Window:  config.DeduplicationWindow.String(),
		},
		Assessment: AssessmentConfig{
			Enabled: config.AssessmentEnabled,
			Model:   config.AssessmentModel,
		},
		IssueFiling: IssueFilingConfig{
			AutoFile:        config.AutoFileIssues,
			DefaultPriority: config.DefaultPriority,
			DefaultLabels:   config.DefaultLabels,
		},
	}

	// Convert worker configs
	for name, wcfg := range config.WorkerConfigs {
		if m, ok := wcfg.(map[string]interface{}); ok {
			configFile.WorkerConfigs[name] = m
		}
	}

	// Marshal to YAML
	data, err := yaml.Marshal(&configFile)
	if err != nil {
		return fmt.Errorf("marshaling config: %w", err)
	}

	// Write file
	if err := os.WriteFile(configPath, data, 0644); err != nil {
		return fmt.Errorf("writing config file: %w", err)
	}

	return nil
}

// ExampleConfigFile returns an example configuration file content.
func ExampleConfigFile() string {
	return `# Discovery Mode Configuration
# See: https://docs.vc.dev/discovery

# Preset to use (quick/standard/thorough/custom)
preset: standard

# Overall budget constraints
budget:
  max_cost: 2.00          # Maximum total cost in USD
  max_duration: 5m        # Maximum total duration
  max_ai_calls: 100       # Maximum AI API calls
  max_issues: 50          # Maximum issues to discover

  # Per-worker budget overrides
  worker_budgets:
    architecture:
      max_cost: 1.00
      max_duration: 2m

# Workers to run (overrides preset)
# Leave empty to use preset defaults
workers:
  - filesize
  - cruft
  - duplication
  - architecture

# Path filters
include_paths: []
exclude_paths:
  - vendor/
  - node_modules/
  - "*.pb.go"
  - "*_generated.go"

# Worker-specific configuration
worker_configs:
  filesize:
    threshold_bytes: 10000

# Deduplication settings
deduplication:
  enabled: true
  window: 7d              # Look back 7 days for duplicates

# AI assessment settings
assessment:
  enabled: true
  model: claude-sonnet-4-5-20250929

# Issue filing settings
issue_filing:
  auto_file: true
  default_priority: 2
  default_labels:
    - discovered:discovery
`
}

// parseDuration parses duration strings like "5m", "1h", "7d"
func parseDuration(s string) (time.Duration, error) {
	// Handle day suffix
	if len(s) > 1 && s[len(s)-1] == 'd' {
		days := s[:len(s)-1]
		var d int
		if _, err := fmt.Sscanf(days, "%d", &d); err != nil {
			return 0, fmt.Errorf("invalid duration: %s", s)
		}
		return time.Duration(d) * 24 * time.Hour, nil
	}

	// Use standard time.ParseDuration for other formats
	return time.ParseDuration(s)
}
