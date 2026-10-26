package internal

import (
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

const DefaultCheckTimeout = 30 * time.Minute

type CheckConfig struct {
	Run     string   `yaml:"run"`
	Needs   []string `yaml:"needs,omitempty"`
	Timeout string   `yaml:"timeout,omitempty"` // Go duration, e.g. "10m"; default 30m
}

func (c CheckConfig) TimeoutDuration() time.Duration {
	if d, err := time.ParseDuration(c.Timeout); err == nil && d > 0 {
		return d
	}
	return DefaultCheckTimeout
}

type MergeRequire struct {
	Checks []string `yaml:"checks"`
	Review string   `yaml:"review"`
}

type MergeConfig struct {
	Require      MergeRequire `yaml:"require"`
	Strategy     string       `yaml:"strategy"`
	RerunChecks  bool         `yaml:"rerun_checks"`
	QueueTimeout int          `yaml:"queue_timeout"` // seconds, 0 = default 300
}

type ReviewConfig struct {
	Type   string `yaml:"type"`
	Cmd    string `yaml:"cmd"`
	Prompt string `yaml:"prompt"`
}

type CommitConfig struct {
	Message string `yaml:"message"`
}

type Config struct {
	MainBranch          string                 `yaml:"main_branch"`
	WorktreeDir         string                 `yaml:"worktree_dir"`
	EpicFeatureBranches *bool                  `yaml:"epic_feature_branches,omitempty"`
	Checks              map[string]CheckConfig `yaml:"checks"`
	Merge               MergeConfig            `yaml:"merge"`
	Commit              CommitConfig           `yaml:"commit"`
	Review              ReviewConfig           `yaml:"review"`
}

// EpicFeatureBranchesEnabled returns true (default) unless explicitly set to false.
func (c *Config) EpicFeatureBranchesEnabled() bool {
	if c.EpicFeatureBranches == nil {
		return true
	}
	return *c.EpicFeatureBranches
}

func LoadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config: %w", err)
	}

	return ParseConfig(data)
}

func ParseConfig(data []byte) (*Config, error) {
	cfg := &Config{}
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parsing config: %w", err)
	}

	// MainBranch default is resolved at LoadApp time via git detection
	if cfg.WorktreeDir == "" {
		cfg.WorktreeDir = ".worktrees"
	}
	if cfg.Merge.Strategy == "" {
		cfg.Merge.Strategy = "merge"
	}
	if cfg.Commit.Message == "" {
		cfg.Commit.Message = "{{message}}"
	}
	if cfg.Merge.QueueTimeout == 0 {
		cfg.Merge.QueueTimeout = 300
	}
	for name, check := range cfg.Checks {
		if check.Timeout == "" {
			continue
		}
		if d, err := time.ParseDuration(check.Timeout); err != nil || d <= 0 {
			return nil, fmt.Errorf("checks.%s.timeout: invalid duration %q", name, check.Timeout)
		}
	}

	return cfg, nil
}
