package internal

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

//go:embed skills/harness-workflow.md
var harnessWorkflowSkill string

//go:embed skills/harness-planning.md
var harnessPlanningSkill string

type opencodePermissionBash struct {
	Wildcard     string `json:"*"`
	GitStatus    string `json:"git status*"`
	GitLog       string `json:"git log*"`
	GitDiff      string `json:"git diff*"`
	GitBranch    string `json:"git branch*"`
	GitAdd       string `json:"git add*"`
	GitCommit    string `json:"git commit*"`
	GitPush      string `json:"git push*"`
	GitMerge     string `json:"git merge*"`
	GitRebase    string `json:"git rebase*"`
	GitCheckout  string `json:"git checkout*"`
	GitStash     string `json:"git stash*"`
	HarnessAll   string `json:"harness *"`
}

type opencodePermission struct {
	Bash opencodePermissionBash `json:"bash"`
}

type opencodeConfig struct {
	Schema     string             `json:"$schema"`
	Permission opencodePermission `json:"permission"`
}

func RunSetupOpencode(dir string) error {
	configPath := filepath.Join(dir, ".harness.yaml")
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		return fmt.Errorf("no .harness.yaml found — run 'harness init' first")
	}

	ocPath := filepath.Join(dir, "opencode.json")

	cfg := opencodeConfig{
		Schema: "https://opencode.ai/config.json",
		Permission: opencodePermission{
			Bash: opencodePermissionBash{
				Wildcard:    "allow",
				GitStatus:   "allow",
				GitLog:      "allow",
				GitDiff:     "allow",
				GitBranch:   "allow",
				GitAdd:      "allow",
				GitCommit:   "allow",
				GitPush:     "deny",
				GitMerge:    "deny",
				GitRebase:   "deny",
				GitCheckout: "deny",
				GitStash:    "deny",
				HarnessAll:  "allow",
			},
		},
	}

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling config: %w", err)
	}

	if err := os.WriteFile(ocPath, append(data, '\n'), 0644); err != nil {
		return fmt.Errorf("writing opencode.json: %w", err)
	}

	fmt.Printf("Created %s\n", ocPath)
	fmt.Println()
	fmt.Println("Permissions configured:")
	fmt.Println("  ALLOW: all commands by default")
	fmt.Println("  DENY:  git push, merge, rebase, checkout, stash (use harness instead)")
	fmt.Println()
	fmt.Println("The agent must use 'harness' for all git-mutating operations.")

	// Install skills
	if err := installOpenCodeSkill(); err != nil {
		return fmt.Errorf("installing skill: %w", err)
	}
	if err := installPlanningSkill(); err != nil {
		return fmt.Errorf("installing planning skill: %w", err)
	}

	return nil
}

func installOpenCodeSkill() error {
	// OpenCode reads skills from ~/.claude/skills/ (Claude Code compatibility)
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}

	skillDir := filepath.Join(home, ".claude", "skills", "harness-workflow")
	if err := os.MkdirAll(skillDir, 0755); err != nil {
		return err
	}

	skillPath := filepath.Join(skillDir, "SKILL.md")

	if err := os.WriteFile(skillPath, []byte(harnessWorkflowSkill), 0644); err != nil {
		return err
	}

	fmt.Printf("\nInstalled skill: %s\n", skillPath)
	fmt.Println("The agent will load the harness-workflow skill automatically.")

	return nil
}

func installPlanningSkill() error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	skillDir := filepath.Join(home, ".claude", "skills", "harness-planning")
	if err := os.MkdirAll(skillDir, 0755); err != nil {
		return err
	}
	skillPath := filepath.Join(skillDir, "SKILL.md")
	if err := os.WriteFile(skillPath, []byte(harnessPlanningSkill), 0644); err != nil {
		return err
	}
	fmt.Printf("Installed skill: %s\n", skillPath)
	return nil
}
