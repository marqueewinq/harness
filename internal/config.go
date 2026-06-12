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
	Require     MergeRequire `yaml:"require"`
	Strategy    string       `yaml:"strategy"`
	RerunChecks bool         `yaml:"rerun_checks"`
}

type ReviewConfig struct {
	Type   string `yaml:"type"`
	Cmd    string `yaml:"cmd"`
	Prompt string `yaml:"prompt"`
}

type Config struct {
	MainBranch          string                 `yaml:"main_branch"`
	WorktreeDir         string                 `yaml:"worktree_dir"`
	EpicFeatureBranches *bool                  `yaml:"epic_feature_branches,omitempty"`
	Checks              map[string]CheckConfig `yaml:"checks"`
	Merge               MergeConfig            `yaml:"merge"`
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

	return cfg, nil
}
