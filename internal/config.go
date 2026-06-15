package internal

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

type CheckConfig struct {
	Run   string   `yaml:"run"`
	Needs []string `yaml:"needs,omitempty"`
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

	return cfg, nil
}
